package openshift

import (
	"bytes"
	"errors"
	"io/ioutil"
	"log"
	"net/http"
	"strings"

	"fmt"

	"github.com/Jeffail/gabs"
	"github.com/SchweizerischeBundesbahnen/ssp-backend/server/common"
	"github.com/gin-gonic/gin"
)

func newProjectHandler(c *gin.Context) {
	username := common.GetUserName(c)

	var data common.NewProjectCommand
	if c.BindJSON(&data) == nil {
		if err := validateNewProject(data.Project, data.Billing, false); err != nil {
			c.JSON(http.StatusBadRequest, common.ApiResponse{Message: err.Error()})
			return
		}

		if err := createNewProject(data.ClusterId, data.Project, username, data.Billing, data.MegaId, false); err != nil {
			c.JSON(http.StatusBadRequest, common.ApiResponse{Message: err.Error()})
		} else {
			c.JSON(http.StatusOK, common.ApiResponse{
				Message: fmt.Sprintf("Das Projekt %v wurde erstellt", data.Project),
			})
		}
	} else {
		c.JSON(http.StatusBadRequest, common.ApiResponse{Message: wrongAPIUsageError})
	}
}

func newTestProjectHandler(c *gin.Context) {
	username := common.GetUserName(c)

	var data common.NewTestProjectCommand
	if c.BindJSON(&data) == nil {
		// Special values for a test project
		billing := "keine-verrechnung"
		data.Project = username + "-" + data.Project

		if err := validateNewProject(data.Project, billing, true); err != nil {
			c.JSON(http.StatusBadRequest, common.ApiResponse{Message: err.Error()})
			return
		}

		if err := createNewProject(data.ClusterId, data.Project, username, billing, "", true); err != nil {
			c.JSON(http.StatusBadRequest, common.ApiResponse{Message: err.Error()})
		} else {
			c.JSON(http.StatusOK, common.ApiResponse{
				Message: fmt.Sprintf("Das Test-Projekt %v wurde erstellt", data.Project),
			})
		}
	} else {
		c.JSON(http.StatusBadRequest, common.ApiResponse{Message: wrongAPIUsageError})
	}
}

func getProjectAdminsHandler(c *gin.Context) {
	username := common.GetUserName(c)
	project := c.Param("project")
	clusterId := c.Param("clusterid")

	if project == "" || clusterId == "" {
		c.JSON(http.StatusBadRequest, common.ApiResponse{Message: wrongAPIUsageError})
		return
	}

	log.Printf("%v has queried all the admins of project %v", username, project)

	if admins, _, err := getProjectAdminsAndOperators(clusterId, project); err != nil {
		c.JSON(http.StatusBadRequest, common.ApiResponse{Message: err.Error()})
	} else {
		c.JSON(http.StatusOK, common.AdminList{
			Admins: admins,
		})
	}
}

func getBillingHandler(c *gin.Context) {
	username := common.GetUserName(c)
	project := c.Param("project")
	clusterId := c.Param("clusterid")

	if project == "" || clusterId == "" {
		c.JSON(http.StatusBadRequest, common.ApiResponse{Message: wrongAPIUsageError})
		return
	}

	if err := validateAdminAccess(clusterId, username, project); err != nil {
		c.JSON(http.StatusBadRequest, common.ApiResponse{Message: err.Error()})
		return
	}

	if billingData, err := getProjectBillingInformation(clusterId, project); err != nil {
		c.JSON(http.StatusBadRequest, common.ApiResponse{Message: err.Error()})
	} else {
		c.JSON(http.StatusOK, common.ApiResponse{
			Message: fmt.Sprintf("Aktuelle Verrechnungsdaten für Projekt %v: %v", project, billingData),
		})
	}
}

func updateBillingHandler(c *gin.Context) {
	username := common.GetUserName(c)

	var data common.EditBillingDataCommand
	if c.BindJSON(&data) == nil {
		if err := validateBillingInformation(data.ClusterId, data.Project, data.Billing, username); err != nil {
			c.JSON(http.StatusBadRequest, common.ApiResponse{Message: err.Error()})
			return
		}

		if err := createOrUpdateMetadata(data.ClusterId, data.Project, data.Billing, "", username, false); err != nil {
			c.JSON(http.StatusBadRequest, common.ApiResponse{Message: err.Error()})
		} else {
			c.JSON(http.StatusOK, common.ApiResponse{
				Message: fmt.Sprintf("Die Verrechnungsdaten wurden gespeichert: %v", data.Billing),
			})
		}
	} else {
		c.JSON(http.StatusBadRequest, common.ApiResponse{Message: wrongAPIUsageError})
	}
}

func validateNewProject(project string, billing string, testProject bool) error {
	if len(project) == 0 {
		return errors.New("Projektname muss angegeben werden")
	}

	if !testProject && len(billing) == 0 {
		return errors.New("Kontierungsnummer muss angegeben werden")
	}

	return nil
}

func validateAdminAccess(clusterId, username, project string) error {
	if len(project) == 0 {
		return errors.New("Projektname muss angegeben werden")
	}

	// Validate permissions
	if err := checkAdminPermissions(clusterId, username, project); err != nil {
		return err
	}

	return nil
}

func validateBillingInformation(clusterId, project, billing, username string) error {
	if len(project) == 0 {
		return errors.New("Projektname muss angegeben werden")
	}

	if len(billing) == 0 {
		return errors.New("Kontierungsnummer muss angegeben werden")
	}

	// Validate permissions
	if err := checkAdminPermissions(clusterId, username, project); err != nil {
		return err
	}

	return nil
}

func createNewProject(clusterId string, project string, username string, billing string, megaid string, testProject bool) error {
	project = strings.ToLower(project)
	p := newObjectRequest("ProjectRequest", project)

	resp, err := getOseHTTPClient("POST", clusterId, "oapi/v1/projectrequests", bytes.NewReader(p.Bytes()))
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusCreated {
		log.Printf("%v created a new project: %v", username, project)

		if err := changeProjectPermission(clusterId, project, username); err != nil {
			return err
		}

		if err := createOrUpdateMetadata(clusterId, project, billing, megaid, username, testProject); err != nil {
			return err
		}
		return nil
	}
	if resp.StatusCode == http.StatusConflict {
		return errors.New("Das Projekt existiert bereits")
	}

	errMsg, _ := ioutil.ReadAll(resp.Body)
	log.Println("Error creating new project:", err, resp.StatusCode, string(errMsg))

	return errors.New(genericAPIError)
}

func changeProjectPermission(clusterId string, project string, username string) error {
	adminRoleBinding, err := getAdminRoleBinding(clusterId, project)
	if err != nil {
		return err
	}

	adminRoleBinding.ArrayAppend(strings.ToLower(username), "userNames")
	adminRoleBinding.ArrayAppend(strings.ToUpper(username), "userNames")

	// Update the policyBindings on the api
	resp, err := getOseHTTPClient("PUT",
		clusterId,
		"oapi/v1/namespaces/"+project+"/rolebindings/admin",
		bytes.NewReader(adminRoleBinding.Bytes()))
	if err != nil {
		return err
	}

	defer resp.Body.Close()

	if resp.StatusCode == http.StatusOK {
		log.Print(username + " is now admin of " + project)
		return nil
	}

	errMsg, _ := ioutil.ReadAll(resp.Body)
	log.Println("Error updating project permissions:", err, resp.StatusCode, string(errMsg))
	return errors.New(genericAPIError)
}

func getProjectBillingInformation(clusterId, project string) (string, error) {
	resp, err := getOseHTTPClient("GET", clusterId, "api/v1/namespaces/"+project, nil)
	if err != nil {
		return "", err
	}

	defer resp.Body.Close()

	json, err := gabs.ParseJSONBuffer(resp.Body)
	if err != nil {
		log.Println("error decoding json:", err, resp.StatusCode)
		return "", errors.New(genericAPIError)
	}

	billing := json.Path("metadata.annotations").S("openshift.io/kontierung-element").Data()
	if billing != nil {
		return billing.(string), nil
	} else {
		return "Keine Daten hinterlegt", nil
	}
}

func createOrUpdateMetadata(clusterId, project string, billing string, megaid string, username string, testProject bool) error {
	resp, err := getOseHTTPClient("GET", clusterId, "api/v1/namespaces/"+project, nil)
	if err != nil {
		return err
	}

	defer resp.Body.Close()

	json, err := gabs.ParseJSONBuffer(resp.Body)
	if err != nil {
		log.Println("error decoding json:", err, resp.StatusCode)
		return errors.New(genericAPIError)
	}

	annotations := json.Path("metadata.annotations")
	annotations.Set(billing, "openshift.io/kontierung-element")
	annotations.Set(username, "openshift.io/requester")

	if testProject {
		annotations.Set(testProjectDeletionDays, "openshift.io/testproject-daystodeletion")
		annotations.Set(fmt.Sprintf("Dieses Testprojekt wird in %v Tagen automatisch gelöscht!", testProjectDeletionDays), "openshift.io/description")
	}

	if len(megaid) > 0 {
		annotations.Set(megaid, "openshift.io/MEGAID")
	}

	resp, err = getOseHTTPClient("PUT", clusterId, "api/v1/namespaces/"+project, bytes.NewReader(json.Bytes()))
	if err != nil {
		return err
	}

	if resp.StatusCode == http.StatusOK {
		resp.Body.Close()
		log.Println("User "+username+" changed config of project "+project+". Kontierungsnummer: "+billing, ", MegaID: "+megaid)
		return nil
	}

	errMsg, _ := ioutil.ReadAll(resp.Body)
	log.Println("Error updating project config:", err, resp.StatusCode, string(errMsg))

	return errors.New(genericAPIError)
}
