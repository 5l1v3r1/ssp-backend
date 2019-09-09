package tower

import (
	"bytes"
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
	genericAPIError    = "Fehler beim Aufruf vom Ansible-Tower-API. Bitte erstelle ein Ticket"
	wrongAPIUsageError = "Invalid api call - parameters did not match to method definition"
)

func RegisterRoutes(r *gin.RouterGroup) {
	//r.POST("/tower/template", startTemplate)
	r.GET("/tower/jobs", getJobsHandler)
	r.POST("/tower/job_templates/launch", postJobTemplateLaunchHandler)
}

func startTemplate() {

}

func postJobTemplateLaunchHandler(c *gin.Context) {
	username := common.GetUserName(c)
	//mail := common.GetUserMail(c)

	request, err := ioutil.ReadAll(c.Request.Body)
	if err != nil {
		log.Printf("%v", err)
		c.JSON(http.StatusOK, "ERROR")
	}
	json, err := gabs.ParseJSON(request)
	if err != nil {
		log.Printf("%v", err)
		c.JSON(http.StatusOK, "ERROR")
	}
	json.SetP(username, "extra_vars.provision_otc_owner_tag")
	log.Printf("%v", json)

	resp, err := getTowerHTTPClient("POST", "job_templates/19296/launch/", bytes.NewReader(json.Bytes()))
	if err != nil {
		log.Printf("%v", err)
		c.JSON(http.StatusOK, "ERROR")
	}
	defer resp.Body.Close()
	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		log.Printf("%v", err)
		c.JSON(http.StatusOK, "ERROR")
	}
	json, err = gabs.ParseJSON(body)
	if err != nil {
		c.JSON(http.StatusBadRequest, common.ApiResponse{Message: "Something went wrong"})
		return
	}
	if resp.StatusCode == http.StatusBadRequest {
		// TODO: cleanup code
		errs := "Fehler vom Ansible Tower (bitte Ticket erstellen):<br><br>"
		for _, err := range json.Path("variables_needed_to_start").Children() {
			errs += "<br>" + err.Data().(string)
		}
		c.JSON(http.StatusBadRequest, common.ApiResponse{Message: string(errs)})
		return
	}
	log.Printf("%+v", string(body))
	c.JSON(http.StatusOK, "test")
}

func getJobsHandler(c *gin.Context) {
	resp, err := getTowerHTTPClient("GET", "jobs/", nil)
	if err != nil {
		log.Printf("%v", err)
		c.JSON(http.StatusOK, "ERROR")
	}
	defer resp.Body.Close()
	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		log.Printf("%v", err)
		c.JSON(http.StatusOK, "ERROR")
	}
	log.Printf("%+v", string(body))

	c.JSON(http.StatusOK, "test")
}

func getTowerHTTPClient(method string, urlPart string, body io.Reader) (*http.Response, error) {
	cfg := config.Config()
	baseUrl := cfg.GetString("tower_base_url")
	if baseUrl == "" {
		log.Fatal("Env variables 'TOWER_BASE_URL' must be specified")
	}

	username := cfg.GetString("tower_username")
	password := cfg.GetString("tower_password")
	if username == "" || password == "" {
		log.Fatal("Env variables 'TOWER_USERNAME' and 'TOWER_PASSWORD' must be specified")
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
