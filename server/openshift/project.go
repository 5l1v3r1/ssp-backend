package openshift

import (
	"bytes"
	"errors"
	"io/ioutil"
	"log"
	"net/http"
	"strings"

	"fmt"

	"crypto/tls"
	"os"

	"github.com/Jeffail/gabs/v2"
	"github.com/SchweizerischeBundesbahnen/ssp-backend/server/common"
	"github.com/gin-gonic/gin"
	"gopkg.in/gomail.v2"
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
			err := sendNewProjectMail(data.ClusterId, data.Project, username, data.MegaId)
			if err != nil {
				log.Printf("Can't send e-mail about new project (%v) on cluster %v.", err, data.ClusterId)
			}

			c.JSON(http.StatusOK, common.ApiResponse{
				Message: fmt.Sprintf("Das Projekt %v wurde erstellt auf Cluster %v", data.Project, data.ClusterId),
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
				Message: fmt.Sprintf("Das Test-Projekt %v wurde erstellt auf Cluster %v", data.Project, data.ClusterId),
			})
		}
	} else {
		c.JSON(http.StatusBadRequest, common.ApiResponse{Message: wrongAPIUsageError})
	}
}

func getProjectsHandler(c *gin.Context) {
	username := common.GetUserName(c)
	params := c.Request.URL.Query()
	clusterId := params.Get("clusterid")
	accountingNumber := params.Get("sbb_accounting_number")
	megaID := params.Get("sbb_mega_id")
	if clusterId == "" {
		c.JSON(http.StatusBadRequest, common.ApiResponse{Message: wrongAPIUsageError})
		return
	}
	log.Printf("%v has queried all his projects in clusterid: %v", username, clusterId)
	projects, err := getProjects(clusterId, username)
	if err != nil {
		c.JSON(http.StatusBadRequest, common.ApiResponse{Message: err.Error()})
		return
	}
	// only return projects that have accountingNumber OR megaID
	// if both are empty (no filtering), then all projects are returned
	filteredProjects := filterProjects(projects, accountingNumber, megaID)
	c.JSON(http.StatusOK, getProjectNames(filteredProjects))
}

// filter projects by accountingNumber AND megaID
// this is used by ESTA
func filterProjects(projects *gabs.Container, accountingNumber, megaID string) *gabs.Container {
	if accountingNumber == "" && megaID == "" {
		return projects
	}
	filtered := gabs.New()
	for _, project := range projects.Children() {
		m, ok := project.Search("metadata", "annotations", "openshift.io/MEGAID").Data().(string)
		if !ok {
			continue
		}
		a, ok := project.Search("metadata", "annotations", "openshift.io/kontierung-element").Data().(string)
		if !ok {
			continue
		}
		if m == megaID && a == accountingNumber {
			filtered.ArrayAppend(project.Data())
		}
	}
	return filtered
}

func getProjectNames(projects *gabs.Container) []string {
	projectNames := []string{}
	for _, project := range projects.Children() {
		name, ok := project.Path("metadata.name").Data().(string)
		if ok {
			projectNames = append(projectNames, name)
		}
	}
	return projectNames
}

func getProjects(clusterid, username string) (*gabs.Container, error) {
	resp, err := getOseHTTPClient("GET", clusterid, "apis/project.openshift.io/v1/projects", nil)
	if err != nil {
		return nil, err
	}

	defer resp.Body.Close()

	json, err := gabs.ParseJSONBuffer(resp.Body)
	if err != nil {
		log.Println("error decoding json:", err, resp.StatusCode)
		return nil, errors.New(genericAPIError)
	}
	projects := json.Search("items")
	return projects, nil
}

func getProjectAdminsHandler(c *gin.Context) {
	username := common.GetUserName(c)

	params := c.Request.URL.Query()
	clusterId := params.Get("clusterid")
	project := params.Get("project")

	if clusterId == "" || project == "" {
		c.JSON(http.StatusBadRequest, common.ApiResponse{Message: wrongAPIUsageError})
		return
	}

	log.Printf("%v has queried all the admins of project %v on cluster %v", username, project, clusterId)

	if admins, _, err := getProjectAdminsAndOperators(clusterId, project); err != nil {
		c.JSON(http.StatusBadRequest, common.ApiResponse{Message: err.Error()})
	} else {
		c.JSON(http.StatusOK, common.AdminList{
			Admins: admins,
		})
	}
}

func getProjectInformationHandler(c *gin.Context) {
	username := common.GetUserName(c)

	params := c.Request.URL.Query()
	clusterId := params.Get("clusterid")
	project := params.Get("project")

	if err := validateAdminAccess(clusterId, username, project); err != nil {
		c.JSON(http.StatusBadRequest, common.ApiResponse{Message: err.Error()})
		return
	}

	pi, err := getProjectInformation(clusterId, project)
	if err != nil {
		c.JSON(http.StatusBadRequest, common.ApiResponse{Message: err.Error()})
	}

	c.JSON(http.StatusOK, pi)
}

func updateProjectInformationHandler(c *gin.Context) {
	username := common.GetUserName(c)

	var data common.UpdateProjectInformationCommand
	if c.BindJSON(&data) == nil {
		if err := validateProjectInformation(data, username); err != nil {
			c.JSON(http.StatusBadRequest, common.ApiResponse{Message: err.Error()})
			return
		}

		if err := createOrUpdateMetadata(data.ClusterId, data.Project, data.Billing, data.MegaID, username, false); err != nil {
			c.JSON(http.StatusBadRequest, common.ApiResponse{Message: err.Error()})
		} else {
			c.JSON(http.StatusOK, common.ApiResponse{
				Message: fmt.Sprintf("The details for project %v on cluster %v has been saved", data.Project, data.ClusterId),
			})
		}
	} else {
		c.JSON(http.StatusBadRequest, common.ApiResponse{Message: wrongAPIUsageError})
	}
}

// Used by ESTA frontend
func addProjectAdminHandler(c *gin.Context) {
	username := common.GetUserName(c)

	var data common.AddProjectAdminCommand
	if c.BindJSON(&data) != nil {
		c.JSON(http.StatusBadRequest, common.ApiResponse{Message: wrongAPIUsageError})
	}

	if data.ClusterId == "" {
		c.JSON(http.StatusBadRequest, common.ApiResponse{Message: "ClusterId must be provided"})
		return
	}

	if data.Project == "" {
		c.JSON(http.StatusBadRequest, common.ApiResponse{Message: "Project must be provided"})
		return
	}

	if data.Username == "" {
		c.JSON(http.StatusBadRequest, common.ApiResponse{Message: "Username must be provided"})
		return
	}

	// Validate permissions
	if err := checkAdminPermissions(data.ClusterId, username, data.Project); err != nil {
		c.JSON(http.StatusBadRequest, common.ApiResponse{Message: err.Error()})
		return
	}

	if err := changeProjectPermission(data.ClusterId, data.Project, data.Username); err != nil {
		c.JSON(http.StatusBadRequest, common.ApiResponse{Message: err.Error()})
		return
	}
	c.JSON(http.StatusOK, common.ApiResponse{
		Message: fmt.Sprintf("The user %v has been sucessfully added to the %v project", data.Username, data.Project),
	})
}

func validateNewProject(project string, billing string, testProject bool) error {
	if len(project) == 0 {
		return errors.New("Project name has to be provided")
	}

	if !testProject && len(billing) == 0 {
		return errors.New("Accounting number must be provided")
	}

	return nil
}

func validateAdminAccess(clusterId, username, project string) error {
	if clusterId == "" {
		return errors.New("Cluster must be provided")
	}

	if project == "" {
		return errors.New("Project name must be provided")
	}

	// Validate permissions
	if err := checkAdminPermissions(clusterId, username, project); err != nil {
		return err
	}

	return nil
}

func validateProjectPermissions(clusterId, username, project string) error {
	if clusterId == "" {
		return errors.New("Cluster must be provided")
	}

	if project == "" {
		return errors.New("Project name must be provided")
	}

	// Allow functional account
	if username == "fssnow1" {
		return nil
	}

	// Validate permissions
	if err := checkAdminPermissions(clusterId, username, project); err != nil {
		return err
	}

	return nil
}

func validateProjectInformation(data common.UpdateProjectInformationCommand, username string) error {
	if data.ClusterId == "" {
		return errors.New("Cluster must be provided")
	}

	if data.Project == "" {
		return errors.New("Project name must be provided")
	}

	if data.Billing == "" {
		return errors.New("Accounting number must be provided")
	}

	// Validate permissions
	if err := validateProjectPermissions(data.ClusterId, username, data.Project); err != nil {
		return err
	}

	return nil
}

func sendNewProjectMail(clusterId string, projectName string, userName string, megaID string) error {

	mailServer, ok := os.LookupEnv("MAIL_SERVER")
	if !ok {
		return errors.New("Error looking up MAIL_SERVER from environment.")
	}

	fromMail, ok := os.LookupEnv("MAIL_ADMIN_SENDER")
	if !ok {
		return errors.New("Error looking up MAIL_ADMIN_SENDER from environment.")
	}

	newProjectMail, ok := os.LookupEnv("MAIL_NEW_PROJECT_RECIPIENT")
	if !ok {
		return errors.New("Error looking up MAIL_NEW_PROJECT_RECIPIENT from environment.")
	}

	m := gomail.NewMessage()
	m.SetHeader("From", fromMail)

	m.SetHeader("To", newProjectMail)
	m.SetHeader("Subject", fmt.Sprintf("New Project '%v' on OpenShift", projectName))

	m.SetBody("text/html", fmt.Sprintf(`
	Dear Ladys and Gentleman,
	<br><br>
	The following project has been created on:
	<br><br>
	Cluster: %v<br>
	Project name:	%v<br>
	Creator:		%v<br>
	Mega ID:		%v
	<br><br>
	Kind regards<br>
	Your Cloud Team<br>
	IT-OM-SDL-CLP
	`, clusterId, projectName, userName, megaID))

	d := gomail.Dialer{Host: mailServer, Port: 25}
	d.TLSConfig = &tls.Config{InsecureSkipVerify: true}
	err := d.DialAndSend(m)

	if err != nil {
		return err
	}

	return nil
}

func createNewProject(clusterId string, project string, username string, billing string, megaid string, testProject bool) error {
	project = strings.ToLower(project)
	p := newObjectRequest("ProjectRequest", project, "project.openshift.io/v1")

	resp, err := getOseHTTPClient("POST", clusterId, "apis/project.openshift.io/v1/projectrequests", bytes.NewReader(p.Bytes()))
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusCreated {
		log.Printf("%v created a new project: %v on cluster %v", username, project, clusterId)

		if err := changeProjectPermission(clusterId, project, username); err != nil {
			return err
		}

		if err := createOrUpdateMetadata(clusterId, project, billing, megaid, username, testProject); err != nil {
			return err
		}
		return nil
	}
	if resp.StatusCode == http.StatusConflict {
		return errors.New("The project already exists")
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

	current_user_low := OpenshiftSubject{
		ApiGroup: "rbac.authorization.k8s.io",
		Kind:     "User",
		Name:     strings.ToLower(username),
	}
	current_user_up := OpenshiftSubject{
		ApiGroup: "rbac.authorization.k8s.io",
		Kind:     "User",
		Name:     strings.ToUpper(username),
	}

	adminRoleBinding.ArrayAppend(current_user_low, "subjects")
	adminRoleBinding.ArrayAppend(current_user_up, "subjects")

	// Update the policyBindings on the api
	resp, err := getOseHTTPClient("PUT",
		clusterId,
		"apis/rbac.authorization.k8s.io/v1/namespaces/"+project+"/rolebindings/admin",
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

type ProjectInformation struct {
	Kontierungsnummer string `json:"kontierungsnummer"`
	MegaID            string `json:"megaid"`
}

func getProjectInformation(clusterId, project string) (*ProjectInformation, error) {
	resp, err := getOseHTTPClient("GET", clusterId, "api/v1/namespaces/"+project, nil)
	if err != nil {
		return nil, err
	}

	defer resp.Body.Close()

	json, err := gabs.ParseJSONBuffer(resp.Body)
	if err != nil {
		log.Println("error decoding json:", err, resp.StatusCode)
		return nil, errors.New(genericAPIError)
	}

	billing := json.Path("metadata.annotations").S("openshift.io/kontierung-element").Data()
	if billing == nil {
		billing = ""
	}
	megaid := json.Path("metadata.annotations").S("openshift.io/MEGAID").Data()
	if megaid == nil {
		megaid = ""
	}
	return &ProjectInformation{
		Kontierungsnummer: billing.(string),
		MegaID:            megaid.(string),
	}, nil
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
		log.Println("User "+username+" changed config of project "+project+" on cluster "+clusterId+". Kontierungsnummer: "+billing, ", MegaID: "+megaid)
		return nil
	}

	errMsg, _ := ioutil.ReadAll(resp.Body)
	log.Println("Error updating project config:", err, resp.StatusCode, string(errMsg))

	return errors.New(genericAPIError)
}
