package otc

import (
	"fmt"
	"github.com/SchweizerischeBundesbahnen/ssp-backend/server/common"
	"github.com/SchweizerischeBundesbahnen/ssp-backend/server/config"
	"github.com/SchweizerischeBundesbahnen/ssp-backend/server/ldap"
	"github.com/gin-gonic/gin"
	"github.com/gophercloud/gophercloud"
	"github.com/gophercloud/gophercloud/openstack/blockstorage/v1/volumetypes"
	"github.com/gophercloud/gophercloud/openstack/blockstorage/v3/volumes"
	"github.com/gophercloud/gophercloud/openstack/compute/v2/extensions/availabilityzones"
	"github.com/gophercloud/gophercloud/openstack/compute/v2/extensions/keypairs"
	"github.com/gophercloud/gophercloud/openstack/compute/v2/extensions/startstop"
	"github.com/gophercloud/gophercloud/openstack/compute/v2/flavors"
	"github.com/gophercloud/gophercloud/openstack/compute/v2/servers"
	"github.com/gophercloud/gophercloud/openstack/imageservice/v2/images"
	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"
	"golang.org/x/crypto/ssh"
	"net/http"
	"strings"
)

func validateUserInput(data NewECSCommand) error {
	log.Println("Validating user input for ECS creation.")

	_, _, _, _, err := ssh.ParseAuthorizedKey([]byte(data.PublicKey))

	if err != nil {
		log.Println("Can't parse public key.", err.Error())
		return errors.New("Der SSH Public Key kann nicht geparst werden.")
	}

	if len(data.ECSName) == 0 {
		return errors.New("Der ECS Name muss angegeben werden.")
	}

	if len(data.Billing) == 0 {
		return errors.New("Kontierungsnummer muss angegeben werden.")
	}

	if len(data.MegaId) == 0 {
		return errors.New("Mega ID muss angegeben werden.")
	}

	if len(data.AvailabilityZone) == 0 {
		return errors.New("Availability Zone muss angegeben werden.")
	}

	if len(data.FlavorName) == 0 {
		return errors.New("Flavor muss angegeben werden.")
	}

	if len(data.ImageId) == 0 {
		return errors.New("Flavor muss angegeben werden.")
	}

	imageClient, err := getImageClient()

	if err != nil {
		log.Println("Error getting compute client.", err.Error())
		return errors.New(genericOTCAPIError)
	}

	image, err := images.Get(imageClient, data.ImageId).Extract()

	if err != nil {
		log.Println("Error while extracting image.", err.Error())
		return errors.New(genericOTCAPIError)
	}

	if image.MinDiskGigabytes > data.RootDiskSize {
		return errors.New(fmt.Sprintf("Das gewählte Image benötigt eine mindestens %vGB grosse System Disk.", image.MinDiskGigabytes))
	}

	computeClient, err := getComputeClient()

	if err != nil {
		log.Println("Error getting compute client.", err.Error())
		return errors.New(genericOTCAPIError)
	}

	flavor, err := flavors.Get(computeClient, data.FlavorName).Extract()

	if err != nil {
		log.Println("Error while extracting flavor.", err.Error())
		return errors.New(genericOTCAPIError)
	}

	if image.MinRAMMegabytes > flavor.RAM {
		return errors.New(fmt.Sprintf("Das gewählte Image benötigt mindestens %vGB RAM.", image.MinRAMMegabytes/1024))
	}

	if len(data.SystemVolumeTypeId) == 0 {
		return errors.New("System Disk Typ muss angegeben werden.")
	}

	return nil
}

func listECSHandler(c *gin.Context) {
	username := common.GetUserName(c)

	log.Printf("%v lists ECS instances @ OTC.", username)

	client, err := getComputeClient()

	if err != nil {
		log.Println("Error getting compute client.", err.Error())
		c.JSON(http.StatusBadRequest, common.ApiResponse{Message: genericOTCAPIError})
		return
	}

	allServers, err := getECServersByUsername(client, common.GetUserName(c))

	if err != nil {
		log.Println("Error getting ECS servers.", err.Error())
		c.JSON(http.StatusBadRequest, common.ApiResponse{Message: genericOTCAPIError})
		return
	}

	c.JSON(http.StatusOK, allServers)
	return
}

func listFlavorsHandler(c *gin.Context) {
	log.Println("Querying flavors @ OTC.")

	client, err := getComputeClient()

	if err != nil {
		fmt.Println("Error getting compute client.", err.Error())
		c.JSON(http.StatusBadRequest, common.ApiResponse{Message: genericOTCAPIError})
		return
	}

	allFlavors, err := getFlavors(client)

	if err != nil {
		log.Println("Error getting flavors.", err.Error())
		c.JSON(http.StatusBadRequest, common.ApiResponse{Message: genericOTCAPIError})
		return
	}

	c.JSON(http.StatusOK, allFlavors)
	return
}

func listImagesHandler(c *gin.Context) {
	log.Println("Querying images @ OTC.")

	client, err := getImageClient()
	if err != nil {
		log.Println("Error getting compute client.", err.Error())
		c.JSON(http.StatusBadRequest, common.ApiResponse{Message: genericOTCAPIError})
		return
	}

	allImages, err := getImages(client)

	if err != nil {
		log.Println("Error getting images.", err.Error())
		c.JSON(http.StatusBadRequest, common.ApiResponse{Message: genericOTCAPIError})
		return
	}

	c.JSON(http.StatusOK, allImages)
	return
}

func listAvailabilityZonesHandler(c *gin.Context) {
	log.Println("Querying availability zones @ OTC.")

	client, err := getComputeClient()

	if err != nil {
		fmt.Println("Error getting compute client.", err.Error())
		c.JSON(http.StatusBadRequest, common.ApiResponse{Message: genericOTCAPIError})
		return
	}

	allAvailabilityZones, err := getAvailabilityZones(client)

	if err != nil {
		log.Println("Error getting availability zones.", err.Error())
		c.JSON(http.StatusBadRequest, common.ApiResponse{Message: genericOTCAPIError})
		return
	}

	c.JSON(http.StatusOK, allAvailabilityZones)
	return
}

func listVolumeTypesHandler(c *gin.Context) {
	log.Println("Querying volume types @ OTC.")

	client, err := getBlockStorageClient()

	if err != nil {
		fmt.Println("Error getting compute client.", err.Error())
		c.JSON(http.StatusBadRequest, common.ApiResponse{Message: genericOTCAPIError})
		return
	}

	allVolumeTypes, err := getVolumeTypes(client)

	if err != nil {
		log.Println("Error getting volume types.", err.Error())
		c.JSON(http.StatusBadRequest, common.ApiResponse{Message: genericOTCAPIError})
		return
	}

	c.JSON(http.StatusOK, allVolumeTypes)
	return
}

func stopECSHandler(c *gin.Context) {
	log.Println("Stopping ECS @ OTC.")

	client, err := getComputeClient()

	if err != nil {
		log.Println("Error getting compute client.", err.Error())
		c.JSON(http.StatusBadRequest, common.ApiResponse{Message: genericOTCAPIError})
		return
	}

	var data ECServerListResponse
	err = c.BindJSON(&data)

	if err != nil {
		log.Println("Binding request to Go struct failed.", err.Error())
		c.JSON(http.StatusBadRequest, common.ApiResponse{Message: wrongAPIUsageError})
		return
	}

	for _, server := range data.ECServers {
		stopResult := startstop.Stop(client, server.Id)

		if stopResult.Err != nil {
			log.Println("Error while stopping server.", err.Error())
			c.JSON(http.StatusBadRequest, common.ApiResponse{Message: "Mindestens ein server konnte nicht gestoppt werden."})
			return
		}
	}

	c.JSON(http.StatusOK, common.ApiResponse{Message: "Serverstopp initiert."})
	return
}

func startECSHandler(c *gin.Context) {
	log.Println("Starting ECS @ OTC.")

	client, err := getComputeClient()

	if err != nil {
		log.Println("Error getting compute client.", err.Error())
		c.JSON(http.StatusBadRequest, common.ApiResponse{Message: genericOTCAPIError})
		return
	}

	var data ECServerListResponse
	err = c.BindJSON(&data)

	if err != nil {
		log.Println("Binding request to Go struct failed.", err.Error())
		c.JSON(http.StatusBadRequest, common.ApiResponse{Message: wrongAPIUsageError})
		return
	}

	for _, server := range data.ECServers {
		stopResult := startstop.Start(client, server.Id)

		if stopResult.Err != nil {
			log.Println("Error while starting server.", err.Error())
			c.JSON(http.StatusBadRequest, common.ApiResponse{Message: "Mindestens ein server konnte nicht gestartet werden."})
			return
		}
	}

	c.JSON(http.StatusOK, common.ApiResponse{Message: "Serverstart initiert."})
	return
}

func rebootECSHandler(c *gin.Context) {
	log.Println("Rebooting ECS @ OTC.")

	client, err := getComputeClient()

	if err != nil {
		log.Println("Error getting compute client.", err.Error())
		c.JSON(http.StatusBadRequest, common.ApiResponse{Message: genericOTCAPIError})
		return
	}

	var data ECServerListResponse
	err = c.BindJSON(&data)

	if err != nil {
		log.Println("Binding request to Go struct failed.", err.Error())
		c.JSON(http.StatusBadRequest, common.ApiResponse{Message: wrongAPIUsageError})
		return
	}

	rebootOpts := servers.RebootOpts{
		Type: servers.SoftReboot,
	}

	for _, server := range data.ECServers {
		rebootResult := servers.Reboot(client, server.Id, rebootOpts)

		if rebootResult.Err != nil {
			log.Println("Error while rebooting server.", err.Error())
			c.JSON(http.StatusBadRequest, common.ApiResponse{Message: "Mindestens ein server konnte nicht rebootet werden."})
			return
		}
	}

	c.JSON(http.StatusOK, common.ApiResponse{Message: "Neustart initiert."})
	return
}

func createKeyPair(client *gophercloud.ServiceClient, publicKeyName string, publicKey string) (*keypairs.KeyPair, error) {
	log.Printf("Creating public key with name %v.", publicKeyName)

	createOpts := keypairs.CreateOpts{
		Name:      publicKeyName,
		PublicKey: publicKey,
	}

	keyPair, err := keypairs.Create(client, createOpts).Extract()

	if err != nil {
		log.Println("Error while creating key pair.", err.Error())
		return nil, err
	}

	return keyPair, nil
}

func getECServersByUsername(client *gophercloud.ServiceClient, username string) (*ECServerListResponse, error) {
	log.WithFields(log.Fields{
		"username": username,
	}).Debug("Getting EC Servers.")

	l, err := ldap.New()
	if err != nil {
		return nil, err
	}
	defer l.Close()

	groups, err := l.GetGroupsOfUser(username)
	if err != nil {
		return nil, err
	}
	log.WithFields(log.Fields{
		"groups":   groups,
		"username": username,
	}).Debug("LDAP groups")

	result := ECServerListResponse{
		ECServers: []ECServer{},
	}

	opts := servers.ListOpts{}

	allPages, err := servers.List(client, opts).AllPages()

	if err != nil {
		log.Println("Error while listing servers.", err.Error())
		return nil, err
	}

	allServers, err := servers.ExtractServers(allPages)

	if err != nil {
		log.Println("Error while extracting servers.", err.Error())
		return nil, err
	}

	imageClient, err := getImageClient()

	if err != nil {
		log.Println("Error getting image service client.", err.Error())
		return nil, err
	}

	volumeClient, err := getBlockStorageClient()

	if err != nil {
		log.Println("Error getting block storage client.", err.Error())
		return nil, err
	}

	for _, server := range allServers {
		if !common.ContainsStringI(groups, server.Metadata["Group"]) {
			continue
		}

		flavor, err := flavors.Get(client, server.Flavor["id"].(string)).Extract()

		if err != nil {
			log.Println("Error getting flavor for a server.", err.Error())
			return nil, err
		}

		image, err := images.Get(imageClient, server.Image["id"].(string)).Extract()

		if err != nil {
			log.Println("Error getting image for a server.", err.Error())
			return nil, err
		}

		var ipAddresses []string

		for _, v := range server.Addresses {
			for _, element := range v.([]interface{}) {
				for key, value := range element.(map[string]interface{}) {
					if key == "addr" {
						ipAddresses = append(ipAddresses, value.(string))
					}
				}
			}
		}

		serverVolumes, err := getVolumesByServerID(volumeClient, server.ID)

		if err != nil {
			log.Println("Error getting volumes for a server.", err.Error())
			return nil, err
		}

		result.ECServers = append(result.ECServers,
			ECServer{
				Id:        server.ID,
				IPv4:      ipAddresses,
				Name:      server.Name,
				Created:   server.Created,
				VCPUs:     flavor.VCPUs,
				RAM:       flavor.RAM,
				ImageName: image.Name,
				Status:    server.Status,
				Metadata:  server.Metadata,
				Volumes:   serverVolumes,
			})
	}

	return &result, nil

}

func getVolumesByServerID(client *gophercloud.ServiceClient, serverId string) ([]volumes.Volume, error) {
	var result []volumes.Volume

	opts := volumes.ListOpts{}

	allPages, err := volumes.List(client, opts).AllPages()

	if err != nil {
		log.Println("Error while listing volumes.", err.Error())
		return nil, err
	}

	allVolumes, err := volumes.ExtractVolumes(allPages)
	if err != nil {
		log.Println("Error while extracting volumes.", err.Error())
		return nil, err
	}

	for _, volume := range allVolumes {
		for _, attachment := range volume.Attachments {
			if attachment.ServerID == serverId {
				result = append(result, volume)
				continue
			}
		}
	}

	return result, err
}

func getVolumeTypes(client *gophercloud.ServiceClient) (*VolumeTypesListResponse, error) {
	log.Println("Getting volume types @ OTC.")

	result := VolumeTypesListResponse{
		VolumeTypes: []VolumeType{},
	}

	allPages, err := volumetypes.List(client).AllPages()

	if err != nil {
		log.Println("Error while listing volume types.", err.Error())
		return nil, err
	}

	allVolumeTypes, err := volumetypes.ExtractVolumeTypes(allPages)

	if err != nil {
		log.Println("Error while extracting volume types.", err.Error())
		return nil, err
	}

	for _, volumeType := range allVolumeTypes {
		result.VolumeTypes = append(result.VolumeTypes, VolumeType{Name: volumeType.Name, Id: volumeType.ID})
	}

	return &result, nil
}

func getFlavors(client *gophercloud.ServiceClient) (*FlavorListResponse, error) {
	log.Println("Getting flavors @ OTC.")

	result := FlavorListResponse{
		Flavors: []Flavor{},
	}

	opts := flavors.ListOpts{}

	allPages, err := flavors.ListDetail(client, opts).AllPages()

	if err != nil {
		log.Println("Error while listing flavors.", err.Error())
		return nil, err
	}

	allFlavors, err := flavors.ExtractFlavors(allPages)

	if err != nil {
		log.Println("Error while extracting flavors.", err.Error())
		return nil, err
	}

	for _, flavor := range allFlavors {
		result.Flavors = append(result.Flavors, Flavor{Name: flavor.Name, VCPUs: flavor.VCPUs, RAM: flavor.RAM})
	}

	return &result, nil
}

func getAvailabilityZones(client *gophercloud.ServiceClient) (*AvailabilityZoneListResponse, error) {
	log.Println("Getting availability zones @ OTC.")

	result := AvailabilityZoneListResponse{}

	allPages, err := availabilityzones.List(client).AllPages()

	if err != nil {
		log.Println("Error while listing availability zones.", err.Error())
		return nil, err
	}

	allAvailabilityZones, err := availabilityzones.ExtractAvailabilityZones(allPages)

	if err != nil {
		log.Println("Error while extracting availability zones.", err.Error())
		return nil, err
	}

	for _, az := range allAvailabilityZones {
		result.AvailabilityZones = append(result.AvailabilityZones, az.ZoneName)
	}

	return &result, nil
}

func getImages(client *gophercloud.ServiceClient) (*ImageListResponse, error) {
	log.Println("Getting images @ OTC.")

	result := ImageListResponse{
		Images: []Image{},
	}

	opts := images.ListOpts{}

	allPages, err := images.List(client, opts).AllPages()

	if err != nil {
		log.Println("Error while listing images.", err.Error())
		return nil, err
	}

	allImages, err := images.ExtractImages(allPages)

	if err != nil {
		log.Println("Error while extracting images.", err.Error())
		return nil, err
	}

	imagePrefix := config.Config().GetString("otc_image_prefix")
	if imagePrefix == "" {
		imagePrefix = "SBB-Managed-OS_"
	}

	for _, image := range allImages {
		if !strings.HasPrefix(image.Name, imagePrefix) {
			continue
		}
		result.Images = append(result.Images, Image{
			TrimmedName:      strings.TrimPrefix(image.Name, imagePrefix),
			Name:             image.Name,
			Id:               image.ID,
			MinDiskGigabytes: image.MinDiskGigabytes,
			MinRAMMegabytes:  image.MinRAMMegabytes,
		})
	}

	return &result, nil
}
