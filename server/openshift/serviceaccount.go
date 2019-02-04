package openshift

import (
	"bytes"
	"errors"
	"io/ioutil"
	"log"
	"net/http"

	"fmt"

	"encoding/base64"
	"encoding/json"
	"github.com/Jeffail/gabs"
	"github.com/SchweizerischeBundesbahnen/ssp-backend/server/common"
	"github.com/SchweizerischeBundesbahnen/ssp-backend/server/config"
	"github.com/gin-gonic/gin"
	"strings"
	"time"
)

type newJenkinsCredentialsCommand struct {
	OrganizationKey string `json:"organizationKey"`
	Secret          string `json:"secret"`
	Description     string `json:"description"`
}

func newServiceAccountHandler(c *gin.Context) {
	jenkinsUrl := config.Config().GetString("jenkins_url")
	if jenkinsUrl == "" {
		log.Fatal("Env variable 'JENKINS_URL' must be specified")
	}

	username := common.GetUserName(c)

	var data common.NewServiceAccountCommand
	if c.BindJSON(&data) != nil {
		c.JSON(http.StatusBadRequest, common.ApiResponse{Message: wrongAPIUsageError})
		return
	}

	if err := validateNewServiceAccount(data.ClusterId, username, data.Project, data.ServiceAccount); err != nil {
		c.JSON(http.StatusBadRequest, common.ApiResponse{Message: err.Error()})
		return
	}

	if err := createNewServiceAccount(data.ClusterId, username, data.Project, data.ServiceAccount); err != nil {
		c.JSON(http.StatusBadRequest, common.ApiResponse{Message: err.Error()})
		return
	}

	if len(data.OrganizationKey) > 0 {

		if err := createJenkinsCredential(data.ClusterId, data.Project, data.ServiceAccount, data.OrganizationKey); err != nil {
			c.JSON(http.StatusBadRequest, common.ApiResponse{Message: err.Error()})
			return
		}
		c.JSON(http.StatusOK, common.ApiResponse{
			Message: fmt.Sprintf(`Der Service Account %v wurde angelegt und im Jenkins hinterlegt. Du findest das Credential & die CredentialId im Jenkins hier: <a href='%v' target='_blank'>Jenkins</a>`,
				data.ServiceAccount, jenkinsUrl+"/job/"+data.OrganizationKey+"/credentials"),
		})

	} else {
		c.JSON(http.StatusOK, common.ApiResponse{
			Message: fmt.Sprintf("Der Service Account %v wurde angelegt", data.ServiceAccount),
		})
	}
}

func validateNewServiceAccount(clusterId, username string, project string, serviceAccountName string) error {
	if len(serviceAccountName) == 0 {
		return errors.New("Service Account muss angegeben werden")
	}

	// Validate permissions
	if err := checkAdminPermissions(clusterId, username, project); err != nil {
		return err
	}

	return nil
}

func createNewServiceAccount(clusterId, username, project, serviceaccount string) error {
	p := newObjectRequest("ServiceAccount", serviceaccount)

	resp, err := getOseHTTPClient("POST", clusterId, "api/v1/namespaces/"+project+"/serviceaccounts", bytes.NewReader(p.Bytes()))
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusConflict {
		return errors.New("Der Service-Account existiert bereits.")
	}

	if resp.StatusCode != http.StatusCreated {
		errMsg, _ := ioutil.ReadAll(resp.Body)
		log.Printf("Error creating service account: StatusCode: %v, Nachricht: %v", resp.StatusCode, string(errMsg))
		return errors.New(genericAPIError)
	}

	log.Print(username + " created a new service account: " + serviceaccount + " on project " + project)

	return nil
}

func getServiceAccount(clusterId, namespace, serviceaccount string) (*gabs.Container, error) {
	url := fmt.Sprintf("api/v1/namespaces/%v/serviceaccounts/%v", namespace, serviceaccount)
	resp, err := getOseHTTPClient("GET", clusterId, url, nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusForbidden {
		bodyBytes, _ := ioutil.ReadAll(resp.Body)
		log.Printf("Error getting service account: StatusCode: %v, Nachricht: %v", resp.StatusCode, string(bodyBytes))
		return nil, errors.New(genericAPIError)
	}

	json, err := gabs.ParseJSONBuffer(resp.Body)
	if err != nil {
		log.Println(err.Error())
		return nil, errors.New(genericAPIError)
	}
	return json, nil
}

func getSecret(clusterId, namespace, secret string) (*gabs.Container, error) {
	url := fmt.Sprintf("api/v1/namespaces/%v/secrets/%v", namespace, secret)
	resp, err := getOseHTTPClient("GET", clusterId, url, nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusForbidden {
		bodyBytes, _ := ioutil.ReadAll(resp.Body)
		log.Printf("Error getting secret: StatusCode: %v, Nachricht: %v", resp.StatusCode, string(bodyBytes))
		return nil, errors.New(genericAPIError)
	}

	json, err := gabs.ParseJSONBuffer(resp.Body)
	if err != nil {
		log.Println(err.Error())
		return nil, errors.New(genericAPIError)
	}
	return json, nil
}

func callWZUBackend(command newJenkinsCredentialsCommand) error {
	byteJson, err := json.Marshal(command)
	if err != nil {
		log.Println(err.Error())
		return errors.New(genericAPIError)
	}

	resp, err := getWZUBackendClient("POST", "sec/jenkins/credentials", bytes.NewReader(byteJson))
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := ioutil.ReadAll(resp.Body)
		return fmt.Errorf("Fehler vom WZU-Backend: StatusCode: %v, Nachricht: %v", resp.StatusCode, string(bodyBytes))
	}
	return nil
}

func createJenkinsCredential(clusterId, project, serviceaccount, organizationKey string) error {
	//Sleep which ensures that the serviceaccount is created completely before we take the Secret out of it.
	time.Sleep(400 * time.Millisecond)

	saJson, err := getServiceAccount(clusterId, project, serviceaccount)
	if err != nil {
		return err
	}

	secret := saJson.S("secrets").Index(0)
	secretName := strings.Trim(secret.Path("name").String(), "\"")

	// The secret with dockercfg is the wrong one. In some rare cases this is the first secret returned
	if strings.Contains(secretName, "dockercfg") {
		secret = saJson.S("secrets").Index(1)
		secretName = strings.Trim(secret.Path("name").String(), "\"")
	}

	secretJson, err := getSecret(clusterId, project, secretName)
	if err != nil {
		return err
	}

	tokenEncoded := strings.Trim(secretJson.Path("data.token").String(), "\"")
	encodedTokenData, err := base64.StdEncoding.DecodeString(tokenEncoded)

	if err != nil {
		log.Println(err.Error())
		return errors.New(genericAPIError)
	}

	// Call the WZU backend
	command := newJenkinsCredentialsCommand{
		OrganizationKey: organizationKey,
		Description:     fmt.Sprintf("OpenShift Deployer - project: %v, service-account: %v", project, serviceaccount),
		Secret:          string(encodedTokenData),
	}
	if err := callWZUBackend(command); err != nil {
		return err
	}

	return nil
}
