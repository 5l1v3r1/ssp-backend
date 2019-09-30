package tower

import (
	"bytes"
	"errors"
	"fmt"
	"github.com/Jeffail/gabs/v2"
	"github.com/SchweizerischeBundesbahnen/ssp-backend/server/common"
	"github.com/SchweizerischeBundesbahnen/ssp-backend/server/config"
	"github.com/gin-gonic/gin"
	log "github.com/sirupsen/logrus"
	"io"
	"io/ioutil"
	"net/http"
	"strings"
)

const (
	wrongAPIUsageError = "Ungültiger API-Aufruf: Die Argumente stimmen nicht mit der definition überein. Bitte erstelle ein Ticket"
	genericAPIError    = "Fehler beim Aufruf der Ansible Tower API. Bitte erstelle ein Ticket"
)

func RegisterRoutes(r *gin.RouterGroup) {
	r.GET("/tower/jobs/:job/stdout", getJobOutputHandler)
	r.GET("/tower/jobs/:job", getJobHandler)
	r.GET("/tower/jobs", getJobsHandler)
	r.POST("/tower/job_templates/:job_template/launch", postJobTemplateLaunchHandler)
}

func postJobTemplateLaunchHandler(c *gin.Context) {
	username := common.GetUserName(c)
	job_template := c.Param("job_template")

	request, err := ioutil.ReadAll(c.Request.Body)
	if err != nil {
		log.Errorf("%v", err)
		c.JSON(http.StatusBadRequest, common.ApiResponse{Message: genericAPIError})
		return
	}
	json, err := gabs.ParseJSON(request)
	if err != nil {
		log.Errorf("%v", err)
		c.JSON(http.StatusBadRequest, common.ApiResponse{Message: genericAPIError})
		return
	}
	json.SetP(username, "extra_vars.provision_otc_owner_tag")
	job, err := launchJobTemplate(job_template, json, username)
	if err != nil {
		log.Errorf("%v", err)
		c.JSON(http.StatusBadRequest, common.ApiResponse{Message: genericAPIError})
		return
	}
	c.JSON(http.StatusOK, job)
}

func launchJobTemplate(job_template string, json *gabs.Container, username string) (string, error) {
	if err := checkPermissions(job_template, username); err != nil {
		return "", err
	}

	json.SetP(username, "extra_vars.custom_tower_user_name")

	resp, err := getTowerHTTPClient("POST", "job_templates/"+job_template+"/launch/", bytes.NewReader(json.Bytes()))
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	json, err = gabs.ParseJSON(body)
	if err != nil {
		return "", err
	}
	if resp.StatusCode == http.StatusBadRequest {
		// Should never happen. This means the SSP and Tower send/expect different variables
		errs := "Fehler vom Ansible Tower (bitte Ticket erstellen):<br><br>"
		for _, err := range json.Path("variables_needed_to_start").Children() {
			errs += "<br>" + err.Data().(string)
		}
		return "", fmt.Errorf(string(errs))
	}
	return string(body), nil
}

type jobTemplatePermission struct {
	ID string
}

func checkPermissions(job_template, username string) error {
	cfg := config.Config()

	job_templates := []jobTemplatePermission{}
	err := cfg.UnmarshalKey("tower.job_templates", &job_templates)
	if err != nil {
		return err
	}
	for _, template := range job_templates {
		if template.ID == job_template {
			log.Printf("Job template %v allowed", job_template)
			return nil
		}

	}
	return fmt.Errorf("Username %v tried to launch job template %v. Not in allowed job_templates", username, job_template)
}

func getJobOutputHandler(c *gin.Context) {
	job := c.Param("job")
	resp, err := getTowerHTTPClient("GET", "jobs/"+job+"/stdout/?format=html", nil)
	if err != nil {
		log.Errorf("%v", err)
		c.JSON(http.StatusBadRequest, common.ApiResponse{Message: genericAPIError})
	}
	defer resp.Body.Close()
	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		log.Errorf("%v", err)
		c.JSON(http.StatusBadRequest, common.ApiResponse{Message: genericAPIError})
	}

	c.JSON(http.StatusOK, string(body))
}

func getJobHandler(c *gin.Context) {
	job := c.Param("job")
	resp, err := getTowerHTTPClient("GET", "jobs/"+job, nil)
	if err != nil {
		log.Errorf("%v", err)
		c.JSON(http.StatusBadRequest, common.ApiResponse{Message: genericAPIError})
	}
	defer resp.Body.Close()
	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		log.Errorf("%v", err)
		c.JSON(http.StatusBadRequest, common.ApiResponse{Message: genericAPIError})
	}

	c.JSON(http.StatusOK, string(body))
}

func getJobsHandler(c *gin.Context) {
	resp, err := getTowerHTTPClient("GET", "jobs/?or__finished__isnull=true&or__artifacts__contains=gs-gdi-otc2-t02&not__launch_type=scheduled", nil)
	if err != nil {
		log.Errorf("%v", err)
		c.JSON(http.StatusBadRequest, common.ApiResponse{Message: genericAPIError})
	}
	defer resp.Body.Close()
	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		log.Errorf("%v", err)
		c.JSON(http.StatusBadRequest, common.ApiResponse{Message: genericAPIError})
	}

	c.JSON(http.StatusOK, string(body))
}

func getTowerHTTPClient(method string, urlPart string, body io.Reader) (*http.Response, error) {
	cfg := config.Config()
	baseUrl := cfg.GetString("tower.base_url")
	if baseUrl == "" {
		log.Error("Env variables 'TOWER_BASE_URL' must be specified")
		return nil, errors.New(common.ConfigNotSetError)
	}

	username := cfg.GetString("tower.username")
	password := cfg.GetString("tower.password")
	if username == "" || password == "" {
		log.Error("Env variables 'TOWER_USERNAME' and 'TOWER_PASSWORD' must be specified")
		return nil, errors.New(common.ConfigNotSetError)
	}

	if !strings.HasSuffix(baseUrl, "/") {
		baseUrl += "/"
	}

	client := &http.Client{}
	req, _ := http.NewRequest(method, baseUrl+urlPart, body)
	req.SetBasicAuth(username, password)

	log.Debugf("Calling %v", req.URL.String())

	req.Header.Add("Content-Type", "application/json")

	return client.Do(req)
}
