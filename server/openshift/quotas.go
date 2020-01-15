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
	"github.com/gin-gonic/gin"
)

const (
	getQuotasApiError = "Error getting quotas from ose-api: %v"
	jsonDecodingError = "Error decoding json from ose api: %v"
)

func (p Plugin) editQuotasHandler(c *gin.Context) {
	username := common.GetUserName(c)

	var data common.EditQuotasCommand
	if c.BindJSON(&data) == nil {
		if err := p.validateEditQuotas(data.ClusterId, username, data.Project, data.CPU, data.Memory); err != nil {
			c.JSON(http.StatusBadRequest, common.ApiResponse{Message: err.Error()})
			return
		}

		if err := p.updateQuotas(data.ClusterId, username, data.Project, data.CPU, data.Memory); err != nil {
			c.JSON(http.StatusBadRequest, common.ApiResponse{Message: err.Error()})
		} else {
			c.JSON(http.StatusOK, common.ApiResponse{
				Message: fmt.Sprintf("The new quotas have been saved: Cluster %v, Project %v, CPU: %v, Memory: %v",
					data.ClusterId, data.Project, data.CPU, data.Memory),
			})
		}
	} else {
		c.JSON(http.StatusBadRequest, common.ApiResponse{Message: wrongAPIUsageError})
	}
}

func (p Plugin) validateEditQuotas(clusterId, username, project string, cpu int, memory int) error {
	cfg := p.config
	maxCPU := cfg.GetInt("max_quota_cpu")
	maxMemory := cfg.GetInt("max_quota_memory")

	if maxCPU == 0 || maxMemory == 0 {
		log.Println("WARNING: Env variables 'MAX_QUOTA_MEMORY' and 'MAX_QUOTA_CPU' must be specified and valid integers")
		return errors.New(common.ConfigNotSetError)
	}

	// Validate user input
	if clusterId == "" {
		return errors.New("Cluster must be provided")
	}

	if project == "" {
		return errors.New("Project must be provided")
	}

	if cpu > maxCPU {
		return fmt.Errorf("The maximal value for CPU cores: %v", maxCPU)
	}

	if memory > maxMemory {
		return fmt.Errorf("The maximal value for memory: %v", maxMemory)
	}

	// Validate permissions
	resp := p.checkAdminPermissions(clusterId, username, project)
	return resp
}

func (p Plugin) updateQuotas(clusterId, username, project string, cpu int, memory int) error {
	resp, err := p.getOseHTTPClient("GET", clusterId, "api/v1/namespaces/"+project+"/resourcequotas", nil)
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

	resp, err = p.getOseHTTPClient("PUT",
		clusterId,
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
	log.Printf("User %v changed quotas for the project %v on cluster %v. CPU: %v Mem: %v", username, clusterId, project, cpu, memory)
	return nil
}
