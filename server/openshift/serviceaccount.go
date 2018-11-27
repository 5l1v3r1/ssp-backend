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
	"github.com/gin-gonic/gin"
	"os"
	"strings"
	"time"
)

var jenkinsUrl string

type newJenkinsCredentialsCommand struct {
	OrganizationKey string `json:"organizationKey"`
	Secret          string `json:"secret"`
	Description     string `json:"description"`
}

func init() {
	jenkinsUrl = os.Getenv("JENKINS_URL")

	if len(jenkinsUrl) == 0 {
		log.Fatal("Env variable 'JENKINS_URL' must be specified")
	}
}

func newServiceAccountHandler(c *gin.Context) {
	username := common.GetUserName(c)

	var data common.NewServiceAccountCommand
	if c.BindJSON(&data) == nil {
		if err := validateNewServiceAccount(username, data.Project, data.ServiceAccount); err != nil {
			c.JSON(http.StatusBadRequest, common.ApiResponse{Message: err.Error()})
			return
		}

		if err := createNewServiceAccount(username, data.Project, data.ServiceAccount, data.OrganizationKey); err != nil {
			c.JSON(http.StatusBadRequest, common.ApiResponse{Message: err.Error()})
		} else {

			if len(data.OrganizationKey) > 0 {
				c.JSON(http.StatusOK, common.ApiResponse{
					Message: fmt.Sprintf(`Der Service Account %v wurde angelegt und im Jenkins hinterlegt. Du findest das Credential & die CredentialId im Jenkins hier: <a href='%v' target='_blank'>Jenkins</a>`,
						data.ServiceAccount, jenkinsUrl+"/job/"+data.OrganizationKey+"/credentials")})
			} else {
				c.JSON(http.StatusOK, common.ApiResponse{
					Message: fmt.Sprintf("Der Service Account %v wurde angelegt", data.ServiceAccount),
				})
			}
		}
	} else {
		c.JSON(http.StatusBadRequest, common.ApiResponse{Message: wrongAPIUsageError})
	}
}

func validateNewServiceAccount(username string, project string, serviceAccountName string) error {
	if len(serviceAccountName) == 0 {
		return errors.New("Service Account muss angegeben werden")
	}

	// Validate permissions
	if err := checkAdminPermissions(username, project); err != nil {
		return err
	}

	return nil
}

func createNewServiceAccount(username string, project string, serviceaccount string, organizationKey string) error {
	p := newObjectRequest("ServiceAccount", serviceaccount)

	resp, err := getOseHTTPClient("POST",
		"api/v1/namespaces/"+project+"/serviceaccounts",
		bytes.NewReader(p.Bytes()))
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusCreated {
		log.Print(username + " created a new service account: " + serviceaccount + " on project " + project)

		if len(organizationKey) > 0 {
			if err = createJenkinsCredential(project, serviceaccount, organizationKey); err != nil {
				log.Println("error creating jenkins credential for service-account", err.Error())
				return err
			}
		}

		return nil
	}

	if resp.StatusCode == http.StatusConflict {
		return errors.New("Der Service-Account existiert bereits.")
	}

	errMsg, _ := ioutil.ReadAll(resp.Body)
	log.Println("Error creating new project:", err, resp.StatusCode, string(errMsg))
	return errors.New(genericAPIError)
}

func getServiceAccount(namespace string, serviceaccount string) (*gabs.Container, error) {
	url := fmt.Sprintf("api/v1/namespaces/%v/serviceaccounts/%v", namespace, serviceaccount)
	resp, err := getOseHTTPClient("GET", url, nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	json, err := gabs.ParseJSONBuffer(resp.Body)
	if err != nil {
		log.Println(err.Error())
		return nil, errors.New(genericAPIError)
	}
	return json, nil
}

func getSecret(namespace string, secret string) (*gabs.Container, error) {
	url := fmt.Sprintf("api/v1/namespaces/%v/secrets/%v", namespace, secret)
	resp, err := getOseHTTPClient("GET", url, nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

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

func createJenkinsCredential(project string, serviceaccount string, organizationKey string) error {
	//Sleep which ensures that the serviceaccount is created completely before we take the Secret out of it.
	time.Sleep(400 * time.Millisecond)

	saJson, err := getServiceAccount(project, serviceaccount)
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

	secretJson, err := getSecret(project, secretName)
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
