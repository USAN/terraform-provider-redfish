package redfish

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log"
	"mime/multipart"
	"net/http"
	"os"
	"strings"

	"github.com/hashicorp/terraform-plugin-sdk/v2/diag"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
	"github.com/stmcginnis/gofish"
	"github.com/stmcginnis/gofish/common"
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

	inventory, err := GetFirmwareInventory(conn)
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

	firmwares, err := inventory.Firmwares()
	if err != nil {
		return diag.Errorf("error fetching firmware details: %s", err)
	}

	var firmware *Firmware
	for _, f := range firmwares {
		if f.Name == name {
			firmware = f
			break
		}
	}

	if firmware == nil || firmware.Version != version {
		service := conn.Service
		update, _ := service.UpdateService()

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

	inventory, err := GetFirmwareInventory(conn)
	if err != nil {
		return diag.Errorf("error fetching firmware inventory: %s", err)
	}

	name := d.Get(nameName)

	firmwares, err := inventory.Firmwares()
	if err != nil {
		return diag.Errorf("error fetching firmware details: %s", err)
	}

	var firmware *Firmware
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

type Firmware struct {
	common.Entity

	Description string
	Name        string
	Version     string
	rawData     []byte
}

type FirmwareInventory struct {
	common.Entity

	Name      string
	firmwares []string
	rawData   []byte
}

func (firmware *Firmware) UnmarshalJSON(b []byte) error {
	type temp Firmware
	var t struct {
		temp
	}

	err := json.Unmarshal(b, &t)
	if err != nil {
		return err
	}

	// Extract the links to other entities for later
	*firmware = Firmware(t.temp)
	firmware.rawData = b
	return nil
}

func (firmware *FirmwareInventory) UnmarshalJSON(b []byte) error {
	type temp FirmwareInventory
	var t struct {
		temp
		Members common.Links
	}

	err := json.Unmarshal(b, &t)
	if err != nil {
		return err
	}

	// Extract the links to other entities for later
	*firmware = FirmwareInventory(t.temp)
	firmware.rawData = b
	firmware.firmwares = t.Members.ToStrings()
	return nil
}

func (firmware *FirmwareInventory) Firmwares() ([]*Firmware, error) {
	var result []*Firmware
	for _, firmwareLink := range firmware.firmwares {
		firmware, err := GetFirmware(firmware.Client, firmwareLink)
		if err != nil {
			return result, nil
		}
		result = append(result, firmware)
	}
	return result, nil
}

func GetFirmware(conn common.Client, uri string) (*Firmware, error) {
	resp, err := conn.Get(uri)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var firmware Firmware
	err = json.NewDecoder(resp.Body).Decode(&firmware)
	if err != nil {
		return nil, err
	}
	firmware.SetClient(conn)
	return &firmware, nil
}

func GetFirmwareInventory(conn *gofish.APIClient) (*FirmwareInventory, error) {

	service := conn.Service
	update, err := service.UpdateService()
	if err != nil {
		return nil, err
	}

	resp, err := conn.Get(update.FirmwareInventory)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var inventory FirmwareInventory
	err = json.NewDecoder(resp.Body).Decode(&inventory)
	if err != nil {
		return nil, err
	}
	inventory.SetClient(conn)
	return &inventory, nil
}
