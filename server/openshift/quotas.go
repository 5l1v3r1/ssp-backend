package openshift

import (
	"bytes"
	"errors"
	"io/ioutil"
	"log"
	"net/http"
	"os"

	"fmt"
	"strconv"
	"strings"

	"github.com/Jeffail/gabs"
	"github.com/SchweizerischeBundesbahnen/ssp-backend/server/common"
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

func validateEditQuotas(username string, project string, cpu string, memory string) error {
	maxCPU := os.Getenv("MAX_QUOTA_CPU")
	maxMemory := os.Getenv("MAX_QUOTA_MEMORY")

	if len(maxCPU) == 0 || len(maxMemory) == 0 {
		log.Fatal("Env variables 'MAX_QUOTA_MEMORY' and 'MAX_QUOTA_CPU' must be specified")
	}

	// Validate user input
	if len(project) == 0 {
		return errors.New("Projekt muss angegeben werden")
	}
	if err := common.ValidateIntInput(maxCPU, cpu); err != nil {
		return err
	}
	if err := common.ValidateIntInput(maxMemory, memory); err != nil {
		return err
	}

	// Validate permissions
	resp := checkAdminPermissions(username, project)
	return resp
}

func GetQuotas(project string) (int, int) {
	resp, err := getOseHTTPClient("GET", "api/v1/namespaces/"+project+"/resourcequotas", nil)
	if err != nil {
		log.Fatalf(getQuotasApiError, err.Error())
	}
	defer resp.Body.Close()

	json, err := gabs.ParseJSONBuffer(resp.Body)
	if err != nil {
		log.Fatalf(jsonDecodingError, err)
	}

	firstQuota := json.S("items").Index(0)

	cpu := firstQuota.Path("spec.hard.cpu").String()
	mem := strings.Replace(firstQuota.Path("spec.hard.memory").String(), "Gi", "", 1)

	cpuInt, err := strconv.Atoi(cpu)
	if err != nil {
		log.Fatalf("Error parsing cpu quota. value: %v, err: %v", cpu, err.Error())
	}
	memInt, err := strconv.Atoi(mem)
	if err != nil {
		log.Fatalf("Error parsing memory quota. value: %v, err: %v", mem, err.Error())
	}

	return cpuInt, memInt
}

func updateQuotas(username string, project string, cpu string, memory string) error {
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
	firstQuota.SetP(memory+"Gi", "spec.hard.memory")

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
