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

	"strconv"

	"github.com/Jeffail/gabs/v2"
	"github.com/SchweizerischeBundesbahnen/ssp-backend/glusterapi/models"
	"github.com/SchweizerischeBundesbahnen/ssp-backend/server/common"
	"github.com/gin-gonic/gin"
)

const (
	wrongSizeFormatError    = "Invalid size. Format: Digits followed by M/G (e.g. 500M)."
	wrongSizeNFSFormatError = "Invalid size. Format: Digits followed by G (e.g. 1G)."
	wrongSizeLimitError     = "This size is not allowed. Minimal size: 500M (1G for NFS). Maximal size: M: %v, G: %v"
	apiCreateWorkflowUuid   = "cf8017d2-061b-4ce4-b25f-9ef7e38a8db9"
	apiChangeWorkflowUuid   = "186b1295-1b82-42e4-b04d-477da967e1d4"
)

func (p Plugin) newVolumeHandler(c *gin.Context) {
	username := common.GetUserName(c)

	var data common.NewVolumeCommand
	if c.BindJSON(&data) == nil {
		if err := p.validateNewVolume(data.ClusterId, data.Project, data.Size, data.PvcName, data.Mode, data.Technology, username); err != nil {
			c.JSON(http.StatusBadRequest, common.ApiResponse{Message: err.Error()})
			return
		}

		// try to get storageclass
		storageclass, err := p.getStorageClass(data.ClusterId, data.Technology)
		if err != nil {
			c.JSON(http.StatusBadRequest, common.ApiResponse{Message: err.Error()})
			return
		}

		newVolumeResponse, err := p.createNewVolume(data.ClusterId, data.Project, data.Size, data.PvcName, data.Mode, data.Technology, username, storageclass)
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
				Message: "The volume has been successfully created.",
				Data:    *newVolumeResponse,
			})
		}
	} else {
		c.JSON(http.StatusBadRequest, common.ApiResponse{Message: wrongAPIUsageError})
	}
}

func (p Plugin) jobStatusHandler(c *gin.Context) {
	params := c.Request.URL.Query()
	clusterId := params.Get("clusterid")
	jobIdStr := params.Get("job")

	jobId, err := strconv.Atoi(jobIdStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, common.ApiResponse{Message: genericAPIError})
		return
	}
	job, err := p.getJob(clusterId, jobId)
	if err != nil {
		c.JSON(http.StatusBadRequest, common.ApiResponse{Message: err.Error()})
		return
	}
	progress := getJobProgress(*job)

	c.JSON(http.StatusOK, progress)
}

func (p Plugin) fixVolumeHandler(c *gin.Context) {
	username := common.GetUserName(c)

	var data common.FixVolumeCommand
	if c.BindJSON(&data) == nil {
		if err := p.validateFixVolume(data.ClusterId, data.Project, username); err != nil {
			c.JSON(http.StatusBadRequest, common.ApiResponse{Message: err.Error()})
			return
		}

		if err := p.recreateGlusterObjects(data.ClusterId, data.Project, username); err != nil {
			c.JSON(http.StatusBadRequest, common.ApiResponse{Message: err.Error()})
		} else {
			c.JSON(http.StatusOK, common.ApiResponse{
				Message: "The GlusterFS objects have been created in the project.",
			})
		}

	} else {
		c.JSON(http.StatusBadRequest, common.ApiResponse{Message: wrongAPIUsageError})
	}
}

func (p Plugin) growVolumeHandler(c *gin.Context) {
	username := common.GetUserName(c)

	var data common.GrowVolumeCommand
	if c.BindJSON(&data) != nil {
		c.JSON(http.StatusBadRequest, common.ApiResponse{Message: wrongAPIUsageError})
		return
	}
	pv, err := p.getOpenshiftPV(data.ClusterId, data.PvName)
	if err != nil {
		c.JSON(http.StatusBadRequest, common.ApiResponse{Message: err.Error()})
		return
	}
	if err := p.validateGrowVolume(data.ClusterId, pv, data.NewSize, username); err != nil {
		c.JSON(http.StatusBadRequest, common.ApiResponse{Message: err.Error()})
		return
	}
	if err := p.growExistingVolume(data.ClusterId, pv, data.NewSize, username); err != nil {
		c.JSON(http.StatusBadRequest, common.ApiResponse{Message: err.Error()})
		return
	}

	c.JSON(http.StatusOK, common.ApiResponse{Message: "Volume has been expanded."})
}

func (p Plugin) validateNewVolume(clusterId, project, size, pvcName, mode, technology, username string) error {
	// Required fields
	if len(project) == 0 || len(pvcName) == 0 || len(size) == 0 || len(mode) == 0 {
		return errors.New("All fields must be filled out.")
	}

	if err := validateSizeFormat(size, technology); err != nil {
		return err
	}

	if err := p.validateSize(size); err != nil {
		return err
	}

	// Permissions on project
	if err := p.checkAdminPermissions(clusterId, username, project); err != nil {
		return err
	}

	// Check if pvc name already taken
	if err := p.checkPvcName(clusterId, project, pvcName); err != nil {
		return err
	}

	// Check if technology is nfs or gluster
	if err := checkTechnology(technology); err != nil {
		return err
	}

	return nil
}

func (p Plugin) validateGrowVolume(clusterId string, pv *gabs.Container, newSize string, username string) error {
	// Required fields
	if len(newSize) == 0 {
		return errors.New("All fields must be filled out.")
	}

	// The technology (nfs, gluster) isn't important. Size can only be bigger
	if err := validateSizeFormat(newSize, "any"); err != nil {
		return err
	}

	if err := p.validateSize(newSize); err != nil {
		return err
	}

	// Permissions on project
	project, ok := pv.Path("spec.claimRef.namespace").Data().(string)
	if !ok {
		log.Println("metadata.claimRef.namespace not found in pv: validateGrowVolume()")
		return errors.New(genericAPIError)
	}
	if err := p.checkAdminPermissions(clusterId, username, project); err != nil {
		return err
	}

	return nil
}

func (p Plugin) validateFixVolume(clusterId, project string, username string) error {
	if len(project) == 0 {
		return errors.New("Project name must be provided")
	}

	// Permissions on project
	if err := p.checkAdminPermissions(clusterId, username, project); err != nil {
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

func (p Plugin) validateSize(size string) error {
	minMB := 500
	maxMB := 1024
	maxGB := p.config.GetInt("max_volume_gb")
	if maxGB <= 0 {
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
			return errors.New("Your value in Megabytes is too big. Please provide the size in Gigabytes")
		}
	}
	if strings.HasSuffix(size, "G") {
		sizeInt, err := strconv.Atoi(strings.Replace(size, "G", "", 1))
		if err != nil {
			return errors.New(wrongSizeFormatError)
		}

		if sizeInt > maxGB {
			return fmt.Errorf(wrongSizeLimitError, maxMB, maxGB)
		}
	}

	return nil
}

func (p Plugin) checkPvcName(clusterId, project, pvcName string) error {
	resp, err := p.getOseHTTPClient("GET", clusterId, fmt.Sprintf("api/v1/namespaces/%v/persistentvolumeclaims", project), nil)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	json, err := gabs.ParseJSONBuffer(resp.Body)
	if err != nil {
		log.Println("error parsing body of response:", err)
		return errors.New(genericAPIError)
	}

	for _, v := range json.S("items").Children() {
		if v.Path("metadata.name").Data().(string) == pvcName {
			return fmt.Errorf("The requested persistent volume claim(PVC) name %v already exists.", pvcName)
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

func (p Plugin) createNewVolume(clusterId, project, size, pvcName, mode, technology, username, storageclass string) (*common.NewVolumeResponse, error) {
	var newVolumeResponse *common.NewVolumeResponse
	var err error
	if technology == "nfs" {
		newVolumeResponse, err = p.createNfsVolume(clusterId, project, pvcName, size, username)
		if err != nil {
			return nil, err
		}
	} else {
		newVolumeResponse, err = p.createGlusterVolume(clusterId, project, size, username)
		if err != nil {
			return nil, err
		}

		// Create Gluster Service & Endpoints in user project
		if err := p.createOpenShiftGlusterService(clusterId, project, username); err != nil {
			return nil, err
		}

		if err := p.createOpenShiftGlusterEndpoint(clusterId, project, username); err != nil {
			return nil, err
		}
	}

	if err := p.createOpenShiftPV(clusterId, size, newVolumeResponse.PvName, newVolumeResponse.Server, newVolumeResponse.Path, mode, technology, username, storageclass); err != nil {
		return nil, err
	}

	if err := p.createOpenShiftPVC(clusterId, project, size, pvcName, mode, username, storageclass); err != nil {
		return nil, err
	}

	return newVolumeResponse, nil
}

func (p Plugin) createGlusterVolume(clusterId, project string, size string, username string) (*common.NewVolumeResponse, error) {
	cmd := models.CreateVolumeCommand{
		Project: project,
		Size:    size,
	}

	b := new(bytes.Buffer)
	if err := json.NewEncoder(b).Encode(cmd); err != nil {
		log.Println(err.Error())
		return nil, errors.New(genericAPIError)
	}

	resp, err := p.getGlusterHTTPClient(clusterId, "sec/volume", b)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		errMsg, _ := ioutil.ReadAll(resp.Body)
		log.Printf("Error creating gluster volume: %v %v", resp.StatusCode, string(errMsg))
		return nil, fmt.Errorf("Fehlerhafte Antwort vom Gluster-API: %v", string(errMsg))
	}

	log.Printf("%v created a gluster volume. Cluster: %v, Project: %v, size: %v", username, clusterId, project, size)

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

func (p Plugin) createNfsVolume(clusterId, project, pvcName, size, username string) (*common.NewVolumeResponse, error) {
	ID := generateID()
	pvName := fmt.Sprintf("%v-%v", project, ID)
	cmd := common.WorkflowCommand{
		UserInputValues: []common.WorkflowKeyValue{
			{
				Key:   "Projectname",
				Value: pvName,
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

	resp, err := p.getNfsHTTPClient("POST", clusterId, fmt.Sprintf("workflows/%v/jobs", apiCreateWorkflowUuid), body)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	job := &common.WorkflowJob{}

	if resp.StatusCode != http.StatusCreated {
		errMsg, _ := ioutil.ReadAll(resp.Body)
		log.Printf("Error creating nfs volume: %v %v", resp.StatusCode, string(errMsg))
		return nil, errors.New(genericAPIError)
	}

	log.Printf("%v is creating an nfs volume. CLuster: %v, Project: %v, size: %v", username, clusterId, project, size)
	bodyBytes, _ := ioutil.ReadAll(resp.Body)

	if err := json.Unmarshal(bodyBytes, job); err != nil {
		log.Println("Error unmarshalling workflow job", err.Error())
		return nil, errors.New(genericAPIError)
	}

	// wait until job is executing
	for {
		job, err = p.getJob(clusterId, job.JobId)
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
		PvName: fmt.Sprintf("nfs-%v", pvName),
		Server: server,
		Path:   path,
		JobId:  job.JobId,
	}, nil
}

func (p Plugin) getOpenshiftPV(clusterId, pvName string) (*gabs.Container, error) {
	if len(pvName) == 0 {
		return nil, errors.New(genericAPIError)
	}
	resp, err := p.getOseHTTPClient("GET", clusterId, fmt.Sprintf("api/v1/persistentvolumes/%v", pvName), nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return nil, errors.New("Persistent Volume not found")
	}
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

func (p Plugin) getJob(clusterId string, jobId int) (*common.WorkflowJob, error) {
	resp, err := p.getNfsHTTPClient("GET", clusterId, fmt.Sprintf("workflows/jobs/%v", jobId), nil)
	if err != nil {
		return nil, err
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

func (p Plugin) growExistingVolume(clusterId string, pv *gabs.Container, newSize string, username string) error {
	if pv.ExistsP("spec.glusterfs") {
		if err := p.growGlusterVolume(clusterId, pv, newSize, username); err != nil {
			return err
		}
		return nil
	}
	if pv.ExistsP("spec.nfs") {
		if err := p.growNfsVolume(clusterId, pv, newSize, username); err != nil {
			return err
		}
		return nil
	}
	return errors.New("Wrong pv name")
}

func (p Plugin) growNfsVolume(clusterId string, pv *gabs.Container, newSize string, username string) error {
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

	resp, err := p.getNfsHTTPClient("POST", clusterId, fmt.Sprintf("workflows/%v/jobs", apiChangeWorkflowUuid), body)
	if err != nil {
		return err
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
		job, err = p.getJob(clusterId, job.JobId)
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

func (p Plugin) growGlusterVolume(clusterId string, pv *gabs.Container, newSize string, username string) error {
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

	resp, err := p.getGlusterHTTPClient(clusterId, "sec/volume/grow", b)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		errMsg, _ := ioutil.ReadAll(resp.Body)
		log.Printf("Error growing gluster volume: %v %v", resp.StatusCode, string(errMsg))
		return fmt.Errorf("Error message from GlusterFS API: %v", string(errMsg))
	}

	log.Printf("%v grew gluster volume. pv: %v, newSize: %v", username, pvName, newSize)
	return nil
}

func (p Plugin) createOpenShiftPV(clusterId, size, pvName, server, path, mode, technology, username, storageclass string) error {
	pvObject := newObjectRequest("PersistentVolume", pvName)
	pvObject.SetP(size, "spec.capacity.storage")

	if technology == "nfs" {
		pvObject.SetP(path, "spec.nfs.path")
		pvObject.SetP(server, "spec.nfs.server")
	} else {
		pvObject.SetP("glusterfs-cluster", "spec.glusterfs.endpoints")
		pvObject.SetP(path, "spec.glusterfs.path")
		pvObject.SetP(false, "spec.glusterfs.readOnly")
	}

	pvObject.SetP("Retain", "spec.persistentVolumeReclaimPolicy")
	if storageclass != "" {
		pvObject.SetP(storageclass, "spec.storageClassName")
	}

	pvObject.ArrayP("spec.accessModes")
	pvObject.ArrayAppend(mode, "spec", "accessModes")

	resp, err := p.getOseHTTPClient("POST",
		clusterId,
		"api/v1/persistentvolumes",
		bytes.NewReader(pvObject.Bytes()))
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		errMsg, _ := ioutil.ReadAll(resp.Body)
		log.Printf("Error creating new PV: %v %v", resp.StatusCode, string(errMsg))
		return errors.New(genericAPIError)
	}

	log.Printf("Created the pv %v based on the request of %v on cluster %v", pvName, username, clusterId)
	return nil
}

func (p Plugin) createOpenShiftPVC(clusterId, project, size, pvcName, mode, username, storageclass string) error {
	pvcObject := newObjectRequest("PersistentVolumeClaim", pvcName)

	pvcObject.SetP(size, "spec.resources.requests.storage")
	pvcObject.ArrayP("spec.accessModes")
	pvcObject.ArrayAppend(mode, "spec", "accessModes")
	if storageclass != "" {
		pvcObject.SetP(storageclass, "spec.storageClassName")
	}

	resp, err := p.getOseHTTPClient("POST",
		clusterId,
		"api/v1/namespaces/"+project+"/persistentvolumeclaims",
		bytes.NewReader(pvcObject.Bytes()))
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		errMsg, _ := ioutil.ReadAll(resp.Body)
		log.Printf("Error creating new PVC: %v %v", resp.StatusCode, string(errMsg))
		return errors.New(genericAPIError)
	}

	log.Printf("Created the pvc %v based on the request of %v on cluster %v", pvcName, username, clusterId)
	return nil
}

func (p Plugin) recreateGlusterObjects(clusterId, project, username string) error {
	if err := p.createOpenShiftGlusterService(clusterId, project, username); err != nil {
		return err
	}

	if err := p.createOpenShiftGlusterEndpoint(clusterId, project, username); err != nil {
		return err
	}

	return nil
}

func (p Plugin) createOpenShiftGlusterService(clusterId, project string, username string) error {
	serviceObject := newObjectRequest("Service", "glusterfs-cluster")

	port := gabs.New()
	port.Set(1, "port")

	serviceObject.ArrayP("spec.ports")
	serviceObject.ArrayAppendP(port.Data(), "spec.ports")

	resp, err := p.getOseHTTPClient("POST",
		clusterId,
		"api/v1/namespaces/"+project+"/services",
		bytes.NewReader(serviceObject.Bytes()))
	if err != nil {
		return err
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

	log.Printf("Created the gluster service based on the request of %v on cluster %v", username, clusterId)
	return nil
}

func (p Plugin) createOpenShiftGlusterEndpoint(clusterId, project, username string) error {
	glusterEndpoints, err := p.getGlusterEndpointsContainer(clusterId)
	if err != nil {
		return err
	}

	resp, err := p.getOseHTTPClient("POST",
		clusterId,
		"api/v1/namespaces/"+project+"/endpoints",
		bytes.NewReader(glusterEndpoints.Bytes()))
	if err != nil {
		return err
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

	log.Printf("Created the gluster endpoints based on the request of %v on cluster %v", username, clusterId)
	return nil
}

func (p Plugin) getGlusterEndpointsContainer(clusterId string) (*gabs.Container, error) {
	cluster, err := p.getOpenshiftCluster(clusterId)
	if err != nil {
		return nil, err
	}

	glusterIPs := cluster.GlusterApi.IPs
	if glusterIPs == "" {
		log.Printf("WARNING: Glusterapi ips not found. Please see README for more details. ClusterId: %v", clusterId)
		return nil, errors.New(common.ConfigNotSetError)
	}

	endpointsObject := newObjectRequest("Endpoints", "glusterfs-cluster")
	endpointsObject.Array("subsets")

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

	endpointsObject.ArrayAppend(addresses.Data(), "subsets")

	return endpointsObject, nil
}
