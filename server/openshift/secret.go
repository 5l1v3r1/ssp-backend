package openshift

import (
	"bytes"
	"errors"
	"io/ioutil"
	"log"
	"net/http"

	"fmt"

	"encoding/json"
	"github.com/Jeffail/gabs"
	"github.com/SchweizerischeBundesbahnen/ssp-backend/server/common"
	"github.com/gin-gonic/gin"
)

type DockerConfig struct {
	Auths map[string]*Auth `json:"auths"`
}
type Auth struct {
	// byte arrays are marshalled to base64
	Auth []byte `json:"auth"`
}

func newPullSecretHandler(c *gin.Context) {
	username := common.GetUserName(c)

	var data common.NewPullSecretCommand
	if c.BindJSON(&data) != nil {
		c.JSON(http.StatusBadRequest, common.ApiResponse{Message: wrongAPIUsageError})
		return
	}
	secret := newObjectRequest("Secret", "external-registry")
	config := DockerConfig{
		Auths: make(map[string]*Auth),
	}
	auth := Auth{
		Auth: []byte(fmt.Sprintf("%v:%v", data.Username, data.Password)),
	}
	config.Auths[data.Repository] = &auth
	secretData, _ := json.Marshal(config)

	secret.Set(secretData, "data", ".dockerconfigjson")
	secret.Set("kubernetes.io/dockerconfigjson", "type")
	if err := createSecret(data.Project, secret); err != nil {
		c.JSON(http.StatusBadRequest, common.ApiResponse{Message: err.Error()})
		return
	}
	if err := addPullSecretToServiceaccount(data.Project, "default"); err != nil {
		c.JSON(http.StatusBadRequest, common.ApiResponse{Message: err.Error()})
		return
	}
	log.Printf("%v created a new pull secret to default serviceaccount on project %v", username, data.Project)
	c.JSON(http.StatusOK, common.ApiResponse{Message: "Das Pull-Secret wurde angelegt"})
}

func addPullSecretToServiceaccount(namespace string, serviceaccount string) error {
	url := fmt.Sprintf("api/v1/namespaces/%v/serviceaccounts/%v", namespace, serviceaccount)
	patch := []common.JsonPatch{
		{
			Operation: "add",
			Path:      "/imagePullSecrets/0",
			Value: struct {
				Name string `json:"name"`
			}{
				Name: "external-registry",
			},
		},
	}

	patchBytes, err := json.Marshal(patch)
	if err != nil {
		log.Printf("Error marshalling patch: %v", err)
		return errors.New(genericAPIError)
	}

	resp, err := getOseHTTPClient("PATCH", url, bytes.NewBuffer(patchBytes))
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := ioutil.ReadAll(resp.Body)
		log.Printf("Error adding pull secret to service account: StatusCode: %v, Nachricht: %v", resp.StatusCode, string(bodyBytes))
		return errors.New(genericAPIError)
	}

	return nil

}

func createSecret(namespace string, secret *gabs.Container) error {
	url := fmt.Sprintf("api/v1/namespaces/%v/secrets", namespace)

	resp, err := getOseHTTPClient("POST", url, bytes.NewReader(secret.Bytes()))
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusForbidden {
		bodyBytes, _ := ioutil.ReadAll(resp.Body)
		log.Printf("Error creating secret: StatusCode: %v, Nachricht: %v", resp.StatusCode, string(bodyBytes))
		return errors.New(genericAPIError)
	}

	if resp.StatusCode == http.StatusConflict {
		return errors.New("Das Secret existiert bereits")
	}

	return nil
}
