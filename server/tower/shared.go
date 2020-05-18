package tower

import (
	"bytes"
	"errors"
	"fmt"
	"github.com/Jeffail/gabs/v2"
	"github.com/SchweizerischeBundesbahnen/ssp-backend/server/common"
	"github.com/SchweizerischeBundesbahnen/ssp-backend/server/config"
	"github.com/SchweizerischeBundesbahnen/ssp-backend/server/otc"
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
	job, err := launchJobTemplate(job_template, json, username)
	if err != nil {
		log.Errorf("%v", err)
		c.JSON(http.StatusBadRequest, common.ApiResponse{Message: genericAPIError})
		return
	}
	c.JSON(http.StatusOK, job)
}

func launchJobTemplate(job_template string, json *gabs.Container, username string) (string, error) {
	// Check if the user is allowed to execute this job_template.
	// This also checks if the job_template is whitelisted (see sample config)
	if err := checkPermissions(job_template, json, username); err != nil {
		return "", err
	}

	// Remove extra_vars that the user is not allowed to set.
	json = removeBlacklistedParameters(json)

	// Overwrite/set the username, this is mostly used for email notifications and
	// for filtering jobs in the SSP (list all jobs with one username)
	json.SetP(username, "extra_vars.custom_tower_user_name")
	log.Printf("%+v", json)

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
		errs := "Fehler vom Ansible Tower:"
		for _, err := range json.Path("variables_needed_to_start").Children() {
			errs += ", " + err.Data().(string)
		}
		return "", fmt.Errorf(string(errs))
	}
	return string(body), nil
}

type jobTemplatePermission struct {
	ID       string
	Validate string
}

func removeBlacklistedParameters(json *gabs.Container) *gabs.Container {
	cfg := config.Config()
	var blacklist []string
	if err := cfg.UnmarshalKey("tower.parameter_blacklist", &blacklist); err != nil {
		log.Warn("No Ansible-Tower parameter blacklist found")
	}
	for _, p := range blacklist {
		if json.Exists("extra_vars", p) {
			json.Delete("extra_vars", p)
			log.WithFields(log.Fields{
				"parameter": p,
			}).Warn("Removed blacklisted parameter!")
		}
	}
	return json
}

func checkPermissions(job_template string, json *gabs.Container, username string) error {
	cfg := config.Config()

	job_templates := []jobTemplatePermission{}
	if err := cfg.UnmarshalKey("tower.job_templates", &job_templates); err != nil {
		return err
	}
	// Check if the template id is whitelisted in the config file (see sample config)
	for _, template := range job_templates {
		if template.ID != job_template {
			continue
		}
		// This is an optional setting in the configfile (see sample config)
		// It means that additional checks are needed. This is mostly done
		// by calling an external service/package.
		if template.Validate != "" {
			if err := checkServicePermissions(template, json, username); err != nil {
				return err
			}
		}
		log.Printf("Job template %v allowed for %v", job_template, username)
		return nil
	}
	return fmt.Errorf("Username %v tried to launch job template %v. Not in allowed job_templates", username, job_template)
}

// This function is only executed if "validate" is specified in the configfile
// There can be multiple validations (see below), if the specified validation
// doesn't exist in the below code, then the check will fail.
func checkServicePermissions(template jobTemplatePermission, json *gabs.Container, username string) error {
	// Validate the uos_group metadata on the server, that is being modified/deleted.
	// Permission only has to be checked if the server already exists.
	if template.Validate == "metadata.uos_group" {
		// To check the "metadata.uos_group" field we need to get the server from OTC
		// We mostly do this by hostname, because the ID is not human readable and
		// this data mostly comes from the Tower Survey or SSP.
		//
		// **Notes for future contributors:**
		// At the moment the tenant is evaluated with the hostname and there is only the managed project.
		// When there are more tenants/projects it might be necessary to somehow evaluate
		// which tenant/project the server hostname belongs to. This could be achieved by parsing the
		// job templates name (from Tower), if these are consistent. Another possibility would be to
		// add tenant and project fields to every job_template in the config file (see jobTemplatePermission struct).
		servername := json.Path("extra_vars.unifiedos_hostname").Data().(string)
		// this function gets the server data and validates the groups of username against the metadata
		if err := otc.ValidatePermissionsByHostname(servername, username); err != nil {
			return err
		}
		// If there is no error, then the user has permission
		return nil
	}
	// Fail if the validation is not defined above or there is a typo in the configuration
	return fmt.Errorf("No existing validation matches: %v Check the configuration", template.Validate)
}

func getJobOutputHandler(c *gin.Context) {
	job := c.Param("job")
	resp, err := getTowerHTTPClient("GET", "jobs/"+job+"/stdout/?format=html", nil)
	if err != nil {
		log.Errorf("%v", err)
		c.JSON(http.StatusBadRequest, common.ApiResponse{Message: genericAPIError})
		return
	}
	defer resp.Body.Close()
	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		log.Errorf("%v", err)
		c.JSON(http.StatusBadRequest, common.ApiResponse{Message: genericAPIError})
		return
	}

	c.JSON(http.StatusOK, string(body))
}

func getJobHandler(c *gin.Context) {
	job := c.Param("job")
	resp, err := getTowerHTTPClient("GET", "jobs/"+job, nil)
	if err != nil {
		log.Errorf("%v", err)
		c.JSON(http.StatusBadRequest, common.ApiResponse{Message: genericAPIError})
		return
	}
	defer resp.Body.Close()
	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		log.Errorf("%v", err)
		c.JSON(http.StatusBadRequest, common.ApiResponse{Message: genericAPIError})
		return
	}

	c.JSON(http.StatusOK, string(body))
}

func getJobsHandler(c *gin.Context) {
	username := common.GetUserName(c)
	// We need to first get the finished jobs and then the failed/running jobs, because the Tower-API
	// doesn't allow filtering by extra_vars (as far as I know).
	finishedJobs, err := getFinishedJobs(username)
	if err != nil {
		log.Errorf("%v", err)
		c.JSON(http.StatusBadRequest, common.ApiResponse{Message: genericAPIError})
		return
	}
	failedOrRunningJobs, err := getFailedOrRunningJobs(username)
	if err != nil {
		log.Errorf("%v", err)
		c.JSON(http.StatusBadRequest, common.ApiResponse{Message: genericAPIError})
		return
	}
	finishedJobs.Merge(failedOrRunningJobs)

	c.JSON(http.StatusOK, finishedJobs.S("results").String())
}

func getFinishedJobs(username string) (*gabs.Container, error) {
	// Get all the jobs that have artifacts which contain the username. This could produce a few
	// false-positives in the future.
	resp, err := getTowerHTTPClient("GET", "jobs/?order_by=-created&artifacts__contains="+username, nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	return gabs.ParseJSON(body)
}

func getFailedOrRunningJobs(username string) (*gabs.Container, error) {
	// Get all the failed/running jobs (of all users, because we cannot filter by extra_vars
	// and artifacts are not available yet) and then loop through and only keep
	// if custom_tower_user_name is set.
	resp, err := getTowerHTTPClient("GET", "jobs/?order_by=-created&or__status=failed&or__finished__isnull=true", nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	jobs, err := gabs.ParseJSON(body)
	if err != nil {
		return nil, err
	}
	jsonObj := gabs.New()
	// Ugly hack to filter on extra_vars.custom_tower_user_name
	// Because the tower api doesn't allow filtering on custom_vars
	// custom_vars is an escaped json string
	for _, job := range jobs.S("results").Children() {
		extra_vars, err := gabs.ParseJSON([]byte(job.S("extra_vars").Data().(string)))
		if err != nil {
			log.Error(err)
			continue
		}
		// Can be nil, if the value doesn't exist
		ctun := extra_vars.S("custom_tower_user_name").Data()
		if ctun != nil && ctun.(string) == username {
			jsonObj.ArrayAppend(job.Data(), "results")
		}
	}
	return jsonObj, nil
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
