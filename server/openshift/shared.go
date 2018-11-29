package openshift

import (
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"strings"

	"github.com/Jeffail/gabs"
	"github.com/SchweizerischeBundesbahnen/ssp-backend/server/common"
	"github.com/gin-gonic/gin"
)

const (
	genericAPIError         = "Fehler beim Aufruf der OpenShift-API. Bitte erstelle ein Ticket"
	wrongAPIUsageError      = "Invalid api call - parameters did not match to method definition"
	testProjectDeletionDays = "30"
)

// RegisterRoutes registers the routes for OpenShift
func RegisterRoutes(r *gin.RouterGroup) {
	// OpenShift
	r.POST("/ose/project", newProjectHandler)
	r.GET("/ose/project/:project/admins", getProjectAdminsHandler)
	r.POST("/ose/testproject", newTestProjectHandler)
	r.POST("/ose/serviceaccount", newServiceAccountHandler)
	r.GET("/ose/billing/:project", getBillingHandler)
	r.POST("/ose/billing", updateBillingHandler)
	r.POST("/ose/quotas", editQuotasHandler)
	r.POST("/ose/chargeback", chargebackHandler)

	// Volumes (Gluster and NFS)
	r.POST("/ose/volume", newVolumeHandler)
	r.POST("/ose/volume/grow", growVolumeHandler)
	r.POST("/ose/volume/gluster/fix", fixVolumeHandler)
	// Get job status for NFS volumes because it takes a while
	r.GET("/ose/volume/jobs/:job", jobStatusHandler)
}

func RegisterSecRoutes(r *gin.RouterGroup) {
	r.POST("/gluster/volume/fix", fixVolumeHandler)
}

func getProjectAdminsAndOperators(project string) ([]string, []string, error) {
	adminRoleBinding, err := getAdminRoleBinding(project)
	if err != nil {
		return nil, nil, err
	}

	var admins []string
	hasOperatorGroup := false
	groups, err := adminRoleBinding.Path("groupNames").Children()
	if err == nil {
		for _, g := range groups {
			if strings.ToLower(g.Data().(string)) == "operator" {
				hasOperatorGroup = true
			}
		}
	}
	usernames, err := adminRoleBinding.Path("userNames").Children()
	if err != nil {
		log.Println("Unable to parse roleBinding", err.Error())
		return nil, nil, errors.New(genericAPIError)
	}
	for _, u := range usernames {
		admins = append(admins, strings.ToLower(u.Data().(string)))
	}

	var operators []string
	if hasOperatorGroup {
		// Going to add the operator group to the admins
		json, err := getOperatorGroup()
		if err != nil {
			return nil, nil, err
		}

		users, err := json.Path("users").Children()
		if err != nil {
			log.Println("Could not parse operator group:", json, err.Error())
			return nil, nil, errors.New(genericAPIError)
		}

		for _, u := range users {
			operators = append(operators, strings.ToLower(u.Data().(string)))
		}
	}
	// remove duplicates because admins are added two times:
	// lowercase and uppercase
	return common.RemoveDuplicates(admins), operators, nil
}

func checkAdminPermissions(username string, project string) error {
	// Check if user has admin-access
	hasAccess := false
	admins, operators, err := getProjectAdminsAndOperators(project)
	if err != nil {
		return err
	}

	username = strings.ToLower(username)

	// allow full access via basic auth
	if username == "sec_api" {
		return nil
	}

	// Access for admins
	for _, a := range admins {
		if username == a {
			hasAccess = true
		}
	}

	// Access for operators
	for _, o := range operators {
		if username == o {
			hasAccess = true
		}
	}

	if hasAccess {
		return nil
	}

	return fmt.Errorf("Du hast keine Admin Rechte auf dem Projekt. Bestehende Admins sind folgende Benutzer: %v", strings.Join(admins, ", "))
}

func getOperatorGroup() (*gabs.Container, error) {
	client, req := getOseHTTPClient("GET", "oapi/v1/groups/operator", nil)
	resp, err := client.Do(req)

	if err != nil {
		log.Println("Error from OpenShift API: ", err.Error())
		return nil, errors.New(genericAPIError)
	}

	defer resp.Body.Close()

	json, err := gabs.ParseJSONBuffer(resp.Body)
	if err != nil {
		log.Println("error parsing body of response:", err)
		return nil, errors.New(genericAPIError)
	}

	return json, nil
}

func getAdminRoleBinding(project string) (*gabs.Container, error) {
	client, req := getOseHTTPClient("GET", "oapi/v1/namespaces/"+project+"/rolebindings/admin", nil)
	resp, err := client.Do(req)

	if err != nil {
		log.Println("Error from OpenShift API: ", err.Error())
		return nil, errors.New(genericAPIError)
	}

	defer resp.Body.Close()

	if resp.StatusCode == 404 {
		log.Println("Project was not found", project)
		return nil, errors.New("Das Projekt existiert nicht")
	}
	if resp.StatusCode == 403 {
		log.Println("Cannot list RoleBindings: Forbidden")
		return nil, errors.New(genericAPIError)
	}
	json, err := gabs.ParseJSONBuffer(resp.Body)
	if err != nil {
		log.Println("error parsing body of response:", err)
		return nil, errors.New(genericAPIError)
	}

	return json, nil
}

func getOseAddress(end string) string {
	base := os.Getenv("OPENSHIFT_API")

	if len(base) == 0 {
		log.Fatal("Env variable 'OPENSHIFT_API' must be specified")
	}

	return base + "/" + end
}

func getOseHTTPClient(method string, endURL string, body io.Reader) (*http.Client, *http.Request) {
	token := os.Getenv("OPENSHIFT_TOKEN")
	if len(token) == 0 {
		log.Fatal("Env variable 'OPENSHIFT_TOKEN' must be specified")
	}

	tr := &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
	}
	client := &http.Client{Transport: tr}

	req, _ := http.NewRequest(method, getOseAddress(endURL), body)

	if common.DebugMode() {
		log.Print("Calling ", req.URL.String())
	}

	req.Header.Add("Authorization", "Bearer "+token)

	return client, req
}

func getWZUBackendClient(method string, endUrl string, body io.Reader) (*http.Client, *http.Request) {
	wzuBackendUrl := os.Getenv("WZUBACKEND_URL")
	wzuBackendSecret := os.Getenv("WZUBACKEND_SECRET")
	if len(wzuBackendUrl) == 0 || len(wzuBackendSecret) == 0 {
		log.Fatal("Env variable 'wzuBackendUrl' and 'WZUBACKEND_SECRET' must be specified")
	}

	tr := &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
	}
	client := &http.Client{Transport: tr}
	req, _ := http.NewRequest(method, wzuBackendUrl+"/"+endUrl, body)

	if common.DebugMode() {
		log.Print("Calling ", req.URL.String())
	}

	req.SetBasicAuth("CLOUD_SSP", wzuBackendSecret)

	return client, req
}

func getGlusterHTTPClient(url string, body io.Reader) (*http.Client, *http.Request) {
	apiUrl := os.Getenv("GLUSTER_API_URL")
	apiSecret := os.Getenv("GLUSTER_SECRET")

	if len(apiUrl) == 0 || len(apiSecret) == 0 {
		log.Fatal("Env variables 'GLUSTER_API_URL' and 'GLUSTER_SECRET' must be specified")
	}

	client := &http.Client{}
	req, _ := http.NewRequest("POST", fmt.Sprintf("%v/%v", apiUrl, url), body)

	if common.DebugMode() {
		log.Printf("Calling %v", req.URL.String())
	}

	req.SetBasicAuth("GLUSTER_API", apiSecret)

	return client, req
}

func getNfsHTTPClient(method string, apiPath string, body io.Reader) (*http.Client, *http.Request) {
	apiUrl := os.Getenv("NFS_API_URL")
	apiSecret := os.Getenv("NFS_API_SECRET")
	nfsProxy := os.Getenv("NFS_PROXY")

	if len(apiUrl) == 0 || len(apiSecret) == 0 || len(nfsProxy) == 0 {
		log.Fatal("Env variables 'NFS_PROXY', 'NFS_API_URL' and 'NFS_API_SECRET' must be specified")
	}

	// Create http client with proxy:
	// https://blog.abhi.host/blog/2016/02/27/golang-creating-https-connection-via/
	proxyURL, err := url.Parse(nfsProxy)
	if err != nil {
		log.Printf(err.Error())
	}

	transport := http.Transport{
		Proxy:           http.ProxyURL(proxyURL),
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
	}

	client := &http.Client{Transport: &transport}
	req, err := http.NewRequest(method, fmt.Sprintf("%v/%v", apiUrl, apiPath), body)
	if err != nil {
		log.Printf(err.Error())
	}

	if common.DebugMode() {
		log.Printf("Calling %v", req.URL.String())
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.SetBasicAuth("sbb_openshift", apiSecret)

	return client, req
}

func newObjectRequest(kind string, name string) *gabs.Container {
	json := gabs.New()

	json.Set(kind, "kind")
	json.Set("v1", "apiVersion")
	json.SetP(name, "metadata.name")

	return json
}
