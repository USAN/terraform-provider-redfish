package redfish

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log"
	"os"
	"strings"

	"github.com/hashicorp/terraform-plugin-sdk/v2/diag"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
	"github.com/stmcginnis/gofish"
	"github.com/stmcginnis/gofish/redfish"
)

const (
	nameName              string = "name"
	versionName           string = "versionName"
	localFileName         string = "local_file"
	signatureFileName     string = "signature_file"
	updateRecoverySetName string = "update_recovery_set"
	taskURIName           string = "task_uri"
)

func resourceRedfishFirmware() *schema.Resource {
	return &schema.Resource{
		CreateContext: resourceRedfishFirmwareUpdate,
		ReadContext:   resourceRedfishFirmwareRead,
		UpdateContext: resourceRedfishFirmwareUpdate,
		DeleteContext: resourceRedfishFirmwareDelete,
		Schema: map[string]*schema.Schema{
			nameName: {
				Type:        schema.TypeString,
				Required:    true,
				Description: "The name of the firmware to be updated.",
			},

			versionName: {
				Type:        schema.TypeString,
				Required:    true,
				Description: "The desired firmware versionName. Should match the versionName of the referenced update.",
			},

			localFileName: {
				Type:        schema.TypeString,
				Required:    true,
				Description: "The path to a local file that contains the firmware update.",
				//DiffSuppressFunc: func(k, old, new string, d *schema.ResourceData) bool { return true },
			},

			signatureFileName: {
				Type:        schema.TypeString,
				Optional:    true,
				Default:     "",
				Description: "The path to a signature file corresponding to the firmware update.",
				//DiffSuppressFunc: func(k, old, new string, d *schema.ResourceData) bool { return true },
			},

			updateRecoverySetName: {
				Type:        schema.TypeBool,
				Optional:    true,
				Default:     false,
				Description: "Whether to update the recovery set with this update. Default is 'false'.",
				//DiffSuppressFunc: func(k, old, new string, d *schema.ResourceData) bool { return true },
			},

			taskURIName: {
				Type:        schema.TypeString,
				Description: "Firmware update task uri",
				Computed:    true,
			},
		},
	}
}

func resourceRedfishFirmwareUpdate(ctx context.Context, d *schema.ResourceData, m interface{}) diag.Diagnostics {

	log.Printf("[DEBUG] Beginning update")
	var diags diag.Diagnostics

	conn := m.(*gofish.APIClient)

	conn, err := conn.CloneWithSession()
	if err == nil {
		defer conn.Logout()
	}

	service := conn.Service
	update, err := service.UpdateService()
	if err != nil {
		return diag.Errorf("error fetching update service: %s", err)
	}

	inventory, err := update.FirmwareInventory()
	if err != nil {
		return diag.Errorf("error fetching firmware inventory: %s", err)
	}

	name := d.Get(nameName)
	version := d.Get(versionName)
	localFile := d.Get(localFileName)
	signatureFile, ok := d.GetOk(signatureFileName)
	if !ok {
		signatureFile = ""
		d.Set(signatureFileName, "")
	}
	updateRecoverySet, ok := d.GetOk(updateRecoverySetName)
	if !ok {
		updateRecoverySet = false
		d.Set(updateRecoverySetName, updateRecoverySet)
	}

	d.Set(taskURIName, "")

	firmwares, err := inventory.Members()
	if err != nil {
		return diag.Errorf("error fetching firmware details: %s", err)
	}

	var firmware *redfish.SoftwareInventory
	for _, f := range firmwares {
		if f.Name == name {
			firmware = f
			break
		}
	}

	if firmware == nil || firmware.Version != version {
		session, err := conn.GetSession()
		if err != nil {
			return diag.Errorf("Error fetching session token: %s", err)
		}

		localFileReader, err := os.Open(localFile.(string))
		if err != nil {
			return diag.Errorf("Error opening local firmware file: %s", err)
		}
		defer localFileReader.Close()

		updateURL := update.HTTPPushURI

		parameters := map[string]interface{}{
			"UpdateRepository": true,
			"UpdateTarget":     true,
			"ETag":             "sometag",
			"Section":          0,
		}

		parameterBytes, err := json.Marshal(parameters)
		if err != nil {
			return diag.Errorf("Error creating parameters: %s", err)
		}
		payloadBuffer := bytes.NewReader(parameterBytes)

		values := map[string]io.Reader{
			"sessionKey": strings.NewReader(session.Token),
			"parameters": payloadBuffer,
			"file":       localFileReader,
		}

		if signatureFile != "" {
			sigFileReader, err := os.Open(signatureFile.(string))
			if err != nil {
				return diag.Errorf("Error opening signature file: %s", err)
			}
			defer sigFileReader.Close()
			values["compsig"] = sigFileReader
		}

		response, err := conn.PostMultipart(updateURL, values)
		if err != nil {
			return diag.Errorf("Error posting firmware: %s", err)
		}
		defer response.Body.Close()
	}

	if firmware != nil {
		d.SetId(firmware.ODataID)
	}

	log.Printf("[DEBUG] %s: Update finished successfully", d.Id())
	return diags
}

func resourceRedfishFirmwareRead(ctx context.Context, d *schema.ResourceData, m interface{}) diag.Diagnostics {
	log.Printf("[DEBUG] %s: Beginning read", d.Id())
	var diags diag.Diagnostics

	conn := m.(*gofish.APIClient)

	service := conn.Service
	update, err := service.UpdateService()
	if err != nil {
		return diag.Errorf("error fetching update service: %s", err)
	}

	inventory, err := update.FirmwareInventory()
	if err != nil {
		return diag.Errorf("error fetching firmware inventory: %s", err)
	}

	name := d.Get(nameName)

	firmwares, err := inventory.Members()
	if err != nil {
		return diag.Errorf("error fetching firmware details: %s", err)
	}

	var firmware *redfish.SoftwareInventory
	for _, f := range firmwares {
		if f.Name == name {
			firmware = f
			break
		}
	}

	if firmware == nil {
		log.Printf("[DEBUG] %s: Read finished not found", name)
		return diags
	}

	d.Set(versionName, firmware.Version)
	d.SetId(firmware.ODataID)

	log.Printf("[DEBUG] %s: Read finished successfully", d.Id())

	return diags
}

func resourceRedfishFirmwareDelete(ctx context.Context, d *schema.ResourceData, m interface{}) diag.Diagnostics {

	var diags diag.Diagnostics

	d.SetId("")

	return diags
}
