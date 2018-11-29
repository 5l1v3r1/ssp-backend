package openshift

import (
	"bytes"
	"errors"
	"io/ioutil"
	"log"
	"net/http"

	"fmt"

	"github.com/Jeffail/gabs"
	"github.com/SchweizerischeBundesbahnen/ssp-backend/server/common"
	"github.com/SchweizerischeBundesbahnen/ssp-backend/server/config"
	"github.com/gin-gonic/gin"
)

const (
	getQuotasApiError = "Error getting quotas from ose-api: %v"
	jsonDecodingError = "Error decoding json from ose api: %v"
)

func editQuotasHandler(c *gin.Context) {
	username := common.GetUserName(c)

	var data common.EditQuotasCommand
	if c.BindJSON(&data) == nil {
		if err := validateEditQuotas(username, data.Project, data.CPU, data.Memory); err != nil {
			c.JSON(http.StatusBadRequest, common.ApiResponse{Message: err.Error()})
			return
		}

		if err := updateQuotas(username, data.Project, data.CPU, data.Memory); err != nil {
			c.JSON(http.StatusBadRequest, common.ApiResponse{Message: err.Error()})
		} else {
			c.JSON(http.StatusOK, common.ApiResponse{
				Message: fmt.Sprintf("Die neuen Quotas wurden gespeichert: Projekt %v, CPU: %v, Memory: %v",
					data.Project, data.CPU, data.Memory),
			})
		}
	} else {
		c.JSON(http.StatusBadRequest, common.ApiResponse{Message: wrongAPIUsageError})
	}
}

func validateEditQuotas(username string, project string, cpu int, memory int) error {
	cfg := config.Config()
	maxCPU := cfg.GetInt("max_quota_cpu")
	maxMemory := cfg.GetInt("max_quota_memory")

	if maxCPU == 0 || maxMemory == 0 {
		log.Println("WARNING: Env variables 'MAX_QUOTA_MEMORY' and 'MAX_QUOTA_CPU' must be specified and valid integers")
		return errors.New(common.ConfigNotSetError)
	}

	// Validate user input
	if project == "" {
		return errors.New("Projekt muss angegeben werden")
	}

	if cpu > maxCPU {
		return fmt.Errorf("Der Maximalwert für CPU ist: %v", maxCPU)
	}

	if memory > maxMemory {
		return fmt.Errorf("Der Maximalwert für Memory ist: %v", maxMemory)
	}

	// Validate permissions
	resp := checkAdminPermissions(username, project)
	return resp
}

func updateQuotas(username string, project string, cpu int, memory int) error {
  resp, err := getOseHTTPClient("GET", "api/v1/namespaces/"+project+"/resourcequotas", nil)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	json, err := gabs.ParseJSONBuffer(resp.Body)
	if err != nil {
		log.Printf(jsonDecodingError, err)
		return errors.New(genericAPIError)
	}

	firstQuota := json.S("items").Index(0)

	firstQuota.SetP(cpu, "spec.hard.cpu")
	firstQuota.SetP(fmt.Sprintf("%vGi", memory), "spec.hard.memory")

	resp, err = getOseHTTPClient("PUT",
		"api/v1/namespaces/"+project+"/resourcequotas/"+firstQuota.Path("metadata.name").Data().(string),
		bytes.NewReader(firstQuota.Bytes()))
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		errMsg, _ := ioutil.ReadAll(resp.Body)
		log.Println("Error updating resourceQuota:", resp.StatusCode, string(errMsg))
		return errors.New(genericAPIError)
	}
	log.Println("User "+username+" changed quotas for the project "+project+". CPU: "+cpu, ", Mem: "+memory)
	return nil
}
