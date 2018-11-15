package openshift

import (
	"errors"
	"net/http"

	"bytes"
	"fmt"
	"io/ioutil"
	"log"
	"strings"
	"time"

	"encoding/json"

	"os"
	"strconv"

	"github.com/Jeffail/gabs"
	"github.com/SchweizerischeBundesbahnen/ssp-backend/glusterapi/models"
	"github.com/SchweizerischeBundesbahnen/ssp-backend/server/common"
	"github.com/gin-gonic/gin"
)

const (
	wrongSizeFormatError    = "Ungültige Grösse. Format muss Zahl gefolgt von M/G sein (z.B. 500M)."
	wrongSizeNFSFormatError = "Ungültige Grösse. Format muss Zahl gefolgt von G sein (z.B. 1G)."
	wrongSizeLimitError     = "Grösse nicht erlaubt. Mindestgrösse: 500M (1G für NFS). Maximale Grössen sind: M: %v, G: %v"
	apiCreateWorkflowUuid   = "64b3b95b-0d79-4563-8b88-f8c4486b40a0"
	apiChangeWorkflowUuid   = "186b1295-1b82-42e4-b04d-477da967e1d4"
	apiDeleteWorkflowUuid   = "06090103-2313-4ad5-8e89-36d872349eaa"
)

func newVolumeHandler(c *gin.Context) {
	username := common.GetUserName(c)

	var data common.NewVolumeCommand
	if c.BindJSON(&data) == nil {
		if err := validateNewVolume(data.Project, data.Size, data.PvcName, data.Mode, data.Technology, username); err != nil {
			c.JSON(http.StatusBadRequest, common.ApiResponse{Message: err.Error()})
			return
		}

		newVolumeResponse, err := createNewVolume(data.Project, data.Size, data.PvcName, data.Mode, data.Technology, username)
		if err != nil {
			c.JSON(http.StatusBadRequest, common.ApiResponse{Message: err.Error()})
			return
		}
		if data.Technology == "nfs" {
			// Don't send a message because this only starts a job
			// and the client polls the server to get the current progress
			c.JSON(http.StatusOK, common.NewVolumeApiResponse{
				Data: *newVolumeResponse,
			})
		} else {
			c.JSON(http.StatusOK, common.NewVolumeApiResponse{
				Message: "Das Volume wurde erstellt. Deinem Projekt wurde das PVC, und der Gluster Service & Endpunkte hinzugefügt.",
				Data:    *newVolumeResponse,
			})
		}
	} else {
		c.JSON(http.StatusBadRequest, common.ApiResponse{Message: wrongAPIUsageError})
	}
}

func jobStatusHandler(c *gin.Context) {
	jobId, err := strconv.Atoi(c.Param("job"))
	if err != nil {
		c.JSON(http.StatusBadRequest, common.ApiResponse{Message: genericAPIError})
		return
	}
	job, err := getJob(jobId)
	if err != nil {
		c.JSON(http.StatusBadRequest, common.ApiResponse{Message: err.Error()})
		return
	}
	progress := getJobProgress(*job)

	c.JSON(http.StatusOK, progress)
}

func fixVolumeHandler(c *gin.Context) {
	username := common.GetUserName(c)

	var data common.FixVolumeCommand
	if c.BindJSON(&data) == nil {
		if err := validateFixVolume(data.Project, username); err != nil {
			c.JSON(http.StatusBadRequest, common.ApiResponse{Message: err.Error()})
			return
		}

		if err := recreateGlusterObjects(data.Project, username); err != nil {
			c.JSON(http.StatusBadRequest, common.ApiResponse{Message: err.Error()})
		} else {
			c.JSON(http.StatusOK, common.ApiResponse{
				Message: "Die Gluster-Objekte wurden in deinem Projekt erzeugt.",
			})
		}

	} else {
		c.JSON(http.StatusBadRequest, common.ApiResponse{Message: wrongAPIUsageError})
	}
}

func growVolumeHandler(c *gin.Context) {
	username := common.GetUserName(c)

	var data common.GrowVolumeCommand
	if c.BindJSON(&data) != nil {
		c.JSON(http.StatusBadRequest, common.ApiResponse{Message: wrongAPIUsageError})
		return
	}
	if err := validateGrowVolume(data.Project, data.NewSize, data.PvName, username); err != nil {
		c.JSON(http.StatusBadRequest, common.ApiResponse{Message: err.Error()})
		return
	}
	pv, err := getOpenshiftPV(data.PvName)
	if err != nil {
		c.JSON(http.StatusBadRequest, common.ApiResponse{Message: err.Error()})
		return
	}
	if err := growExistingVolume(pv, data.NewSize, username); err != nil {
		c.JSON(http.StatusBadRequest, common.ApiResponse{Message: err.Error()})
		return
	}

	c.JSON(http.StatusOK, common.ApiResponse{Message: "Das Volume wurde vergrössert."})
}

func validateNewVolume(project string, size string, pvcName string, mode string, technology string, username string) error {
	// Required fields
	if len(project) == 0 || len(pvcName) == 0 || len(size) == 0 || len(mode) == 0 {
		return errors.New("Es müssen alle Felder ausgefüllt werden")
	}

	if err := validateSizeFormat(size, technology); err != nil {
		return err
	}

	if err := validateSize(size); err != nil {
		return err
	}

	// Permissions on project
	if err := checkAdminPermissions(username, project); err != nil {
		return err
	}

	// Check if pvc name already taken
	if err := checkPvcName(project, pvcName); err != nil {
		return err
	}

	// Check if technology is nfs or gluster
	if err := checkTechnology(technology); err != nil {
		return err
	}

	return nil
}

func validateGrowVolume(project string, newSize string, pvName string, username string) error {
	// Required fields
	if len(project) == 0 || len(pvName) == 0 || len(newSize) == 0 {
		return errors.New("Es müssen alle Felder ausgefüllt werden")
	}

	// The technology (nfs, gluster) isn't important. Size can only be bigger
	if err := validateSizeFormat(newSize, "any"); err != nil {
		return err
	}

	if err := validateSize(newSize); err != nil {
		return err
	}

	// Permissions on project
	if err := checkAdminPermissions(username, project); err != nil {
		return err
	}

	return nil
}

func validateFixVolume(project string, username string) error {
	if len(project) == 0 {
		return errors.New("Projekt muss angegeben werden")
	}

	// Permissions on project
	if err := checkAdminPermissions(username, project); err != nil {
		return err
	}

	return nil
}

func validateSizeFormat(size string, technology string) error {
	// only allow Gigabytes for nfs
	if technology == "nfs" {
		if strings.HasSuffix(size, "G") {
			return nil
		}
		return errors.New(wrongSizeNFSFormatError)
	}
	if strings.HasSuffix(size, "M") || strings.HasSuffix(size, "G") {
		return nil
	}
	return errors.New(wrongSizeFormatError)
}

func validateSize(size string) error {
	minMB := 500
	maxMB := 1024
	maxGB := os.Getenv("MAX_VOLUME_GB")

	maxGBInt, errGB := strconv.Atoi(maxGB)
	if errGB != nil || maxGBInt <= 0 {
		log.Fatal("Env variable 'MAX_VOLUME_GB' must be specified and a valid integer")
	}

	// Size limits
	if strings.HasSuffix(size, "M") {
		sizeInt, err := strconv.Atoi(strings.Replace(size, "M", "", 1))
		if err != nil {
			return errors.New(wrongSizeFormatError)
		}

		if sizeInt < minMB {
			return fmt.Errorf(wrongSizeLimitError, maxMB, maxGB)
		}
		if sizeInt > maxMB {
			return errors.New("Deine Angaben sind zu gross für 'M'. Bitte gib die Grösse als Ganzzahl in 'G' an")
		}
	}
	if strings.HasSuffix(size, "G") {
		sizeInt, err := strconv.Atoi(strings.Replace(size, "G", "", 1))
		if err != nil {
			return errors.New(wrongSizeFormatError)
		}

		if sizeInt > maxGBInt {
			return fmt.Errorf(wrongSizeLimitError, maxMB, maxGB)
		}
	}

	return nil
}

func checkPvcName(project string, pvcName string) error {
	client, req := getOseHTTPClient("GET", fmt.Sprintf("api/v1/namespaces/%v/persistentvolumeclaims", project), nil)

	resp, err := client.Do(req)
	if err != nil {
		log.Println("Error from server while getting pvc-list: ", err.Error())
		return errors.New(genericAPIError)
	}
	defer resp.Body.Close()

	json, err := gabs.ParseJSONBuffer(resp.Body)
	if err != nil {
		log.Println("error parsing body of response:", err)
		return errors.New(genericAPIError)
	}

	// Check if pvc name is not already used
	children, err := json.S("items").Children()
	if err != nil {
		log.Println("Unable to parse pvc list", err.Error())
		return errors.New(genericAPIError)
	}
	for _, v := range children {
		if v.Path("metadata.name").Data().(string) == pvcName {
			return fmt.Errorf("Der gewünschte PVC-Name %v existiert bereits.", pvcName)
		}
	}

	return nil
}

func checkTechnology(technology string) error {
	switch technology {
	case
		"nfs",
		"gluster":
		return nil
	}
	return errors.New("Invalid technology. Must be either nfs or gluster")
}

func createNewVolume(project string, size string, pvcName string, mode string, technology string, username string) (*common.NewVolumeResponse, error) {
	var newVolumeResponse *common.NewVolumeResponse
	var err error
	if technology == "nfs" {
		newVolumeResponse, err = createNfsVolume(project, pvcName, size, username)
		if err != nil {
			return nil, err
		}
	} else {
		newVolumeResponse, err = createGlusterVolume(project, size, username)
		if err != nil {
			return nil, err
		}

		// Create Gluster Service & Endpoints in user project
		if err := createOpenShiftGlusterService(project, username); err != nil {
			return nil, err
		}

		if err := createOpenShiftGlusterEndpoint(project, username); err != nil {
			return nil, err
		}
	}

	if err := createOpenShiftPV(size, newVolumeResponse.PvName, newVolumeResponse.Server, newVolumeResponse.Path, mode, technology, username); err != nil {
		return nil, err
	}

	if err := createOpenShiftPVC(project, size, pvcName, mode, username); err != nil {
		return nil, err
	}

	return newVolumeResponse, nil
}

func createGlusterVolume(project string, size string, username string) (*common.NewVolumeResponse, error) {
	cmd := models.CreateVolumeCommand{
		Project: project,
		Size:    size,
	}

	b := new(bytes.Buffer)
	if err := json.NewEncoder(b).Encode(cmd); err != nil {
		log.Println(err.Error())
		return nil, errors.New(genericAPIError)
	}

	client, req := getGlusterHTTPClient("sec/volume", b)

	resp, err := client.Do(req)
	if err != nil {
		log.Println("Error calling gluster-api", err.Error())
		return nil, errors.New(genericAPIError)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		errMsg, _ := ioutil.ReadAll(resp.Body)
		log.Printf("Error creating gluster volume: %v %v", resp.StatusCode, string(errMsg))
		return nil, fmt.Errorf("Fehlerhafte Antwort vom Gluster-API: %v", string(errMsg))
	}

	log.Printf("%v created a gluster volume. Project: %v, size: %v", username, project, size)

	respJson, err := gabs.ParseJSONBuffer(resp.Body)
	if err != nil {
		log.Println("Error parsing respJson from gluster-api response", err.Error())
		return nil, errors.New(genericAPIError)
	}
	message := respJson.Path("message").Data().(string)

	return &common.NewVolumeResponse{
		// Add gl- to pvName because of conflicting PVs on other storage technology
		// The Volume will use _ in the name, OpenShift can't, so we change it to -
		PvName: fmt.Sprintf("gl-%v", strings.Replace(message, "_", "-", 1)),
		Path:   fmt.Sprintf("vol_%v", message),
	}, nil
}

func createNfsVolume(project string, pvcName string, size string, username string) (*common.NewVolumeResponse, error) {
	cmd := common.WorkflowCommand{
		UserInputValues: []common.WorkflowKeyValue{
			{
				Key:   "Projectname",
				Value: fmt.Sprintf("%v-%v", project, pvcName),
			},
			{
				Key:   "Projectsize",
				Value: strings.Replace(size, "G", "", 1),
			},
		},
	}

	body := new(bytes.Buffer)
	if err := json.NewEncoder(body).Encode(cmd); err != nil {
		log.Println(err.Error())
		return nil, errors.New(genericAPIError)
	}

	client, req := getNfsHTTPClient("POST", fmt.Sprintf("workflows/%v/jobs", apiCreateWorkflowUuid), body)

	resp, err := client.Do(req)
	if err != nil {
		log.Println("Error calling nfs-api", err.Error())
		return nil, errors.New(genericAPIError)
	}
	defer resp.Body.Close()

	job := &common.WorkflowJob{}

	if resp.StatusCode != http.StatusCreated {
		errMsg, _ := ioutil.ReadAll(resp.Body)
		log.Printf("Error creating nfs volume: %v %v", resp.StatusCode, string(errMsg))
		return nil, errors.New(genericAPIError)
	}

	log.Printf("%v is creating an nfs volume. Project: %v, size: %v", username, project, size)
	bodyBytes, _ := ioutil.ReadAll(resp.Body)

	if err := json.Unmarshal(bodyBytes, job); err != nil {
		log.Println("Error unmarshalling workflow job", err.Error())
		return nil, errors.New(genericAPIError)
	}

	// wait until job is executing
	for {
		job, err = getJob(job.JobId)
		if err != nil {
			log.Println("Error unmarshalling workflow job", err.Error())
			return nil, errors.New(genericAPIError)
		}
		if job.JobStatus.JobStatus == "EXECUTING" {
			break
		}
		time.Sleep(time.Second)
	}

	server := ""
	path := ""
	for _, parameter := range job.JobStatus.ReturnParameters {
		if parameter.Key == "'Server' + $Projectname" {
			s := strings.Split(parameter.Value, ":")
			server, path = s[0], s[1]
			break
		}
	}
	if server == "" || path == "" {
		log.Println("Couldn't parse nfs server or path")
		return nil, errors.New(genericAPIError)
	}

	// Add nfs_ to pvName because of conflicting PVs on other storage technology
	return &common.NewVolumeResponse{
		PvName: fmt.Sprintf("nfs-%v-%v", project, pvcName),
		Server: server,
		Path:   path,
		JobId:  job.JobId,
	}, nil
}

func getOpenshiftPV(pvName string) (*gabs.Container, error) {
	client, req := getOseHTTPClient("GET", fmt.Sprintf("api/v1/persistentvolumes/%v", pvName), nil)

	resp, err := client.Do(req)
	if err != nil {
		log.Printf("Error from server while getting pv: %v", err.Error())
		return nil, errors.New(genericAPIError)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		errMsg, _ := ioutil.ReadAll(resp.Body)
		log.Printf("Error getting openshift pv: %v %v", resp.StatusCode, string(errMsg))
		return nil, errors.New(genericAPIError)
	}

	json, err := gabs.ParseJSONBuffer(resp.Body)
	if err != nil {
		log.Printf("Error parsing body of response in getOpenshiftPV(): %v", err.Error())
		return nil, errors.New(genericAPIError)
	}
	return json, nil
}

func getJob(jobId int) (*common.WorkflowJob, error) {
	client, req := getNfsHTTPClient("GET", fmt.Sprintf("workflows/jobs/%v", jobId), nil)

	resp, err := client.Do(req)
	if err != nil {
		log.Println("Error calling nfs-api", err.Error())
		return nil, errors.New(genericAPIError)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		errMsg, _ := ioutil.ReadAll(resp.Body)
		log.Printf("Error getting job: %v %v", resp.StatusCode, string(errMsg))
		return nil, errors.New(genericAPIError)
	}

	var body common.WorkflowJob
	bodyBytes, _ := ioutil.ReadAll(resp.Body)
	if err := json.Unmarshal(bodyBytes, &body); err != nil {
		log.Println("Error unmarshalling workflow job", err.Error())
		return nil, errors.New(genericAPIError)
	}
	if body.JobStatus.JobStatus == "FAILED" {
		log.Println("Workflow job failed: ", body.JobStatus.ErrorMessage)
		return nil, errors.New(genericAPIError)
	}
	return &body, nil
}

func getJobProgress(job common.WorkflowJob) float64 {
	currentProgress := job.JobStatus.WorkflowExecutionProgress.CurrentCommandIndex
	maxProgress := job.JobStatus.WorkflowExecutionProgress.CommandsNumber
	if maxProgress*currentProgress == 0 {
		return 0
	}
	return 100.0 / maxProgress * currentProgress
}

func growExistingVolume(pv *gabs.Container, newSize string, username string) error {
	if pv.ExistsP("spec.glusterfs") {
		if err := growGlusterVolume(pv, newSize, username); err != nil {
			return err
		}
		return nil
	}
	if pv.ExistsP("spec.nfs") {
		if err := growNfsVolume(pv, newSize, username); err != nil {
			return err
		}
		return nil
	}
	return errors.New("Wrong pv name")
}

func growNfsVolume(pv *gabs.Container, newSize string, username string) error {
	nfsPath, ok := pv.Path("spec.nfs.path").Data().(string)
	if !ok {
		log.Println("spec.nfs.path not found in pv: growNfsVolume()")
		return errors.New(genericAPIError)
	}
	pvName, ok := pv.Path("metadata.name").Data().(string)
	if !ok {
		log.Println("metadata.name not found in pv: growNfsVolume()")
		return errors.New(genericAPIError)
	}
	cmd := common.WorkflowCommand{
		UserInputValues: []common.WorkflowKeyValue{
			{
				Key:   "Projectname",
				Value: strings.Replace(nfsPath, "/v004_0/", "", 1),
			},
			{
				Key:   "newSize",
				Value: strings.Replace(newSize, "G", "", 1),
			},
		},
	}

	body := new(bytes.Buffer)
	if err := json.NewEncoder(body).Encode(cmd); err != nil {
		log.Println(err.Error())
		return errors.New(genericAPIError)
	}

	client, req := getNfsHTTPClient("POST", fmt.Sprintf("workflows/%v/jobs", apiChangeWorkflowUuid), body)

	resp, err := client.Do(req)
	if err != nil {
		log.Println("Error calling nfs-api", err.Error())
		return errors.New(genericAPIError)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		errMsg, _ := ioutil.ReadAll(resp.Body)
		log.Printf("Error getting job: %v %v", resp.StatusCode, string(errMsg))
		return errors.New(genericAPIError)
	}

	job := &common.WorkflowJob{}
	log.Printf("%v grew nfs volume. pv: %v, size: %v", username, pvName, newSize)
	bodyBytes, _ := ioutil.ReadAll(resp.Body)

	if err := json.Unmarshal(bodyBytes, job); err != nil {
		log.Println("Error unmarshalling workflow job", err.Error())
		return errors.New(genericAPIError)
	}

	// wait until job is executing
	for {
		job, err = getJob(job.JobId)
		if err != nil {
			log.Println("Error unmarshalling workflow job", err.Error())
			return errors.New(genericAPIError)
		}
		if job.JobStatus.JobStatus == "COMPLETED" {
			break
		}
		time.Sleep(time.Second)
	}
    return nil
}

func growGlusterVolume(pv *gabs.Container, newSize string, username string) error {
	glusterfsPath, ok := pv.Path("spec.glusterfs.path").Data().(string)
	if !ok {
		log.Println("spec.glusterfs.path not found in pv: growGlusterVolume()")
		return errors.New(genericAPIError)
	}
	pvName, ok := pv.Path("metadata.name").Data().(string)
	if !ok {
		log.Println("metadata.name not found in pv: growGlusterVolume()")
		return errors.New(genericAPIError)
	}
	cmd := models.GrowVolumeCommand{
		PvName:  strings.Replace(glusterfsPath, "vol_", "", 1),
		NewSize: newSize,
	}

	b := new(bytes.Buffer)
	if err := json.NewEncoder(b).Encode(cmd); err != nil {
		log.Println(err.Error())
		return errors.New(genericAPIError)
	}

	client, req := getGlusterHTTPClient("sec/volume/grow", b)

	resp, err := client.Do(req)
	if err != nil {
		log.Println("Error calling gluster-api", err.Error())
		return errors.New(genericAPIError)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		errMsg, _ := ioutil.ReadAll(resp.Body)
		log.Printf("Error growing gluster volume: %v %v", resp.StatusCode, string(errMsg))
		return fmt.Errorf("Fehlerhafte Antwort vom Gluster-API: %v", string(errMsg))
	}

	log.Printf("%v grew gluster volume. pv: %v, newSize: %v", username, pvName, newSize)
	return nil
}

func createOpenShiftPV(size string, pvName string, server string, path string, mode string, technology string, username string) error {
	p := newObjectRequest("PersistentVolume", pvName)
	p.SetP(size, "spec.capacity.storage")

	if technology == "nfs" {
		p.SetP(path, "spec.nfs.path")
		p.SetP(server, "spec.nfs.server")
	} else {
		p.SetP("glusterfs-cluster", "spec.glusterfs.endpoints")
		p.SetP(path, "spec.glusterfs.path")
		p.SetP(false, "spec.glusterfs.readOnly")
	}

	p.SetP("Retain", "spec.persistentVolumeReclaimPolicy")
	p.ArrayP("spec.accessModes")
	p.ArrayAppend(mode, "spec", "accessModes")

	client, req := getOseHTTPClient("POST",
		"api/v1/persistentvolumes",
		bytes.NewReader(p.Bytes()))

	resp, err := client.Do(req)
	if err != nil {
		log.Println("Error creating Openshift PV", err.Error())
		return errors.New(genericAPIError)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		errMsg, _ := ioutil.ReadAll(resp.Body)
		log.Printf("Error creating new PV: %v %v", resp.StatusCode, string(errMsg))
		return errors.New(genericAPIError)
	}

	log.Printf("Created the pv %v based on the request of %v", pvName, username)
	return nil
}

func createOpenShiftPVC(project string, size string, pvcName string, mode string, username string) error {
	p := newObjectRequest("PersistentVolumeClaim", pvcName)

	p.SetP(size, "spec.resources.requests.storage")
	p.ArrayP("spec.accessModes")
	p.ArrayAppend(mode, "spec", "accessModes")

	client, req := getOseHTTPClient("POST",
		"api/v1/namespaces/"+project+"/persistentvolumeclaims",
		bytes.NewReader(p.Bytes()))

	resp, err := client.Do(req)
	if err != nil {
		log.Println("Error creating Openshift PVC", err.Error())
		return errors.New(genericAPIError)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		errMsg, _ := ioutil.ReadAll(resp.Body)
		log.Printf("Error creating new PVC: %v %v", resp.StatusCode, string(errMsg))
		return errors.New(genericAPIError)
	}

	log.Printf("Created the pvc %v based on the request of %v", pvcName, username)
	return nil
}

func recreateGlusterObjects(project string, username string) error {
	if err := createOpenShiftGlusterService(project, username); err != nil {
		return err
	}

	if err := createOpenShiftGlusterEndpoint(project, username); err != nil {
		return err
	}

	return nil
}

func createOpenShiftGlusterService(project string, username string) error {
	p := newObjectRequest("Service", "glusterfs-cluster")

	port := gabs.New()
	port.Set(1, "port")

	p.ArrayP("spec.ports")
	p.ArrayAppendP(port.Data(), "spec.ports")

	client, req := getOseHTTPClient("POST",
		"api/v1/namespaces/"+project+"/services",
		bytes.NewReader(p.Bytes()))

	resp, err := client.Do(req)
	if err != nil {
		log.Println("Error creating Openshift Gluster service", err.Error())
		return errors.New(genericAPIError)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusConflict {
		log.Println("Gluster service already existed, skipping")
		return nil
	}

	if resp.StatusCode != http.StatusCreated {
		errMsg, _ := ioutil.ReadAll(resp.Body)
		log.Printf("Error creating gluster service: %v %v", resp.StatusCode, string(errMsg))
		return errors.New(genericAPIError)
	}

	log.Printf("Created the gluster service based on the request of %v", username)
	return nil
}

func createOpenShiftGlusterEndpoint(project string, username string) error {
	p, err := getGlusterEndpointsContainer()
	if err != nil {
		return err
	}

	client, req := getOseHTTPClient("POST",
		"api/v1/namespaces/"+project+"/endpoints",
		bytes.NewReader(p.Bytes()))

	resp, err := client.Do(req)
	if err != nil {
		log.Println("Error creating Openshift Gluster endpoint", err.Error())
		return errors.New(genericAPIError)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusConflict {
		log.Println("Gluster endpoints already existed, skipping")
		return nil
	}

	if resp.StatusCode != http.StatusCreated {
		errMsg, _ := ioutil.ReadAll(resp.Body)
		log.Printf("Error creating gluster endpoints: %v %v", resp.StatusCode, string(errMsg))
		return errors.New(genericAPIError)
	}

	log.Printf("Created the gluster endpoints based on the request of %v", username)
	return nil
}

func getGlusterEndpointsContainer() (*gabs.Container, error) {
	p := newObjectRequest("Endpoints", "glusterfs-cluster")
	p.Array("subsets")

	// Add gluster endpoints
	glusterIPs := os.Getenv("GLUSTER_IPS")
	if len(glusterIPs) == 0 {
		log.Println("Wrong configuration. Missing env variable 'GLUSTER_IPS'")
		return nil, errors.New(genericAPIError)
	}

	addresses := gabs.New()
	addresses.Array("addresses")
	addresses.Array("ports")
	for _, ip := range strings.Split(glusterIPs, ",") {
		address := gabs.New()
		address.Set(ip, "ip")

		addresses.ArrayAppend(address.Data(), "addresses")
	}

	port := gabs.New()
	port.Set(1, "port")
	addresses.ArrayAppend(port.Data(), "ports")

	p.ArrayAppend(addresses.Data(), "subsets")

	return p, nil
}
