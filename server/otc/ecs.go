package otc

import (
	"fmt"
	"github.com/SchweizerischeBundesbahnen/ssp-backend/server/common"
	"github.com/gin-gonic/gin"
	"github.com/gofrs/uuid"
	"github.com/gophercloud/gophercloud"
	"github.com/gophercloud/gophercloud/openstack/blockstorage/v1/volumetypes"
	"github.com/gophercloud/gophercloud/openstack/blockstorage/v3/volumes"
	"github.com/gophercloud/gophercloud/openstack/compute/v2/extensions/availabilityzones"
	"github.com/gophercloud/gophercloud/openstack/compute/v2/extensions/bootfromvolume"
	"github.com/gophercloud/gophercloud/openstack/compute/v2/extensions/keypairs"
	"github.com/gophercloud/gophercloud/openstack/compute/v2/extensions/startstop"
	"github.com/gophercloud/gophercloud/openstack/compute/v2/flavors"
	"github.com/gophercloud/gophercloud/openstack/compute/v2/servers"
	"github.com/gophercloud/gophercloud/openstack/imageservice/v2/images"
	"github.com/pkg/errors"
	"golang.org/x/crypto/ssh"
	"log"
	"net/http"
	"os"
	"strings"
)

func validateUserInput(data NewECSCommand) error {
	log.Println("Validating user input for ECS creation.")

	_, _, _, _, err := ssh.ParseAuthorizedKey([]byte(data.PublicKey))

	if err != nil {
		log.Println("Can't parse public key.", err.Error())
		if err != nil {
			return errors.New("Der SSH Public Key kann nicht geparst werden.")
		}
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

	// data.systemDataDiskSize
	imageClient, err := getImageClient()

	if err != nil {
		log.Println("Error getting compute client.", err.Error())
		if err != nil {
			return errors.New(genericOTCAPIError)
		}
	}

	image, err := images.Get(imageClient, data.ImageId).Extract()

	if err != nil {
		log.Println("Error while extracting image.", err.Error())
		if err != nil {
			return errors.New(genericOTCAPIError)
		}
	}

	if image.MinDiskGigabytes > data.SystemDiskSize {
		return errors.New(fmt.Sprintf("Das gewählte Image benötigt eine mindestens %vGB grosse System Disk.", image.MinDiskGigabytes))
	}

	computeClient, err := getComputeClient()

	if err != nil {
		log.Println("Error getting compute client.", err.Error())
		if err != nil {
			return errors.New(genericOTCAPIError)
		}
	}

	flavor, err := flavors.Get(computeClient, data.FlavorName).Extract()

	if err != nil {
		log.Println("Error while extracting flavor.", err.Error())
		if err != nil {
			return errors.New(genericOTCAPIError)
		}
	}

	if image.MinRAMMegabytes > flavor.RAM {
		return errors.New(fmt.Sprintf("Das gewählte Image benötigt mindestens %vGB RAM.", image.MinRAMMegabytes/1024))
	}

	if len(data.SystemVolumeTypeId) == 0 {
		return errors.New("System Disk Typ muss angegeben werden.")
	}

	return nil
}

func newECSHandler(c *gin.Context) {
	networkId := os.Getenv("OTC_NETWORK_UUID")

	if len(networkId) < 1 {
		log.Println("Environment variable OTC_NETWORK_UUID must be set.")
		c.JSON(http.StatusBadRequest, common.ApiResponse{Message: genericOTCAPIError})
		return
	}

	username := common.GetUserName(c)
	log.Printf("%v creates new ECS @ OTC.", username)

	var data NewECSCommand
	err := c.BindJSON(&data)

	if err != nil {
		log.Println("Binding request to Go struct failed.", err.Error())
		c.JSON(http.StatusBadRequest, common.ApiResponse{Message: wrongAPIUsageError})
		return
	}

	err = validateUserInput(data)

	if err != nil {
		log.Println("User input validation failed.", err.Error())
		c.JSON(http.StatusBadRequest, common.ApiResponse{Message: err.Error()})
		return
	}

	client, err := getComputeClient()

	if err != nil {
		log.Println("Error getting compute client.", err.Error())
		c.JSON(http.StatusBadRequest, common.ApiResponse{Message: genericOTCAPIError})
		return
	}

	serverName, err := generateECSName(data.ECSName)

	if err != nil {
		log.Println("Error generating server name.", err.Error())
		c.JSON(http.StatusBadRequest, common.ApiResponse{Message: genericOTCAPIError})
		return
	}

	uniqueId, err := uuid.NewV4()
	if err != nil {
		log.Println("Error getting UUID. That's incredible.", err.Error())
		c.JSON(http.StatusBadRequest, common.ApiResponse{Message: genericOTCAPIError})
		return
	}

	blockDevices, err := createECSDisks(data, serverName, uniqueId.String(), username)
	if err != nil {
		log.Println("Error creating disks for ECS.", err.Error())
		c.JSON(http.StatusBadRequest, common.ApiResponse{Message: genericOTCAPIError})
		return
	}

	keyPair, err := createKeyPair(client, serverName+"-"+username+"-"+uniqueId.String(), data.PublicKey)
	if err != nil {
		log.Println("Error getting key.", err.Error())
		c.JSON(http.StatusBadRequest, common.ApiResponse{Message: genericOTCAPIError})
		return
	}

	serverCreateOpts := servers.CreateOpts{
		Name:             serverName,
		AvailabilityZone: data.AvailabilityZone,
		FlavorRef:        data.FlavorName,
		Networks: []servers.Network{
			{
				UUID: networkId,
			},
		},
		Metadata: map[string]string{
			"Owner":   username,
			"Billing": data.Billing,
			"Mega ID": data.MegaId,
		},
	}

	keyCreateOpts := keypairs.CreateOptsExt{
		CreateOptsBuilder: serverCreateOpts,
		KeyName:           keyPair.Name,
	}

	_, err = bootfromvolume.Create(client, bootfromvolume.CreateOptsExt{
		CreateOptsBuilder: keyCreateOpts,
		BlockDevice:       blockDevices,
	}).Extract()

	if err != nil {
		log.Println("Creating server failed.", err.Error())
		c.JSON(http.StatusBadRequest, common.ApiResponse{Message: "Server konnte nicht erstellt werden."})
		return
	} else {
		log.Println("Creating server succeeded.")
		c.JSON(http.StatusOK, common.ApiResponse{Message: "Server erstellt."})
		return
	}
}

func createECSDisks(data NewECSCommand, serverName string, uniqueId string, username string) ([]bootfromvolume.BlockDevice, error) {
	log.Println("Creating system and data disks.")

	blockStorageClient, err := getBlockStorageClient()
	if err != nil {
		log.Println("Error getting block storage client.", err.Error())
		return nil, err
	}

	// create system disk volume
	volOpts := volumes.CreateOpts{
		Size:             data.SystemDiskSize,
		Name:             serverName + "-" + uniqueId,
		Description:      serverName + "-" + uniqueId,
		VolumeType:       data.SystemVolumeTypeId,
		ImageID:          data.ImageId,
		AvailabilityZone: data.AvailabilityZone,
		Metadata: map[string]string{
			"Owner":   username,
			"Billing": data.Billing,
			"Mega ID": data.MegaId,
		},
	}

	vol, err := volumes.Create(blockStorageClient, volOpts).Extract()
	if err != nil {
		log.Println("Creating system disk volume failed.", err.Error())
		return nil, err
	}

	err = volumes.WaitForStatus(blockStorageClient, vol.ID, "available", 60)
	if err != nil {
		log.Println("Error while waiting on system volume.", err.Error())
		return nil, err
	}

	var blockDevices []bootfromvolume.BlockDevice

	// add system disk to the list
	blockDevices = append(blockDevices, bootfromvolume.BlockDevice{SourceType: bootfromvolume.SourceVolume, DestinationType: bootfromvolume.DestinationVolume, UUID: vol.ID})

	// data disks
	for _, disk := range data.DataDisks {
		volOpts := volumes.CreateOpts{
			Size:             disk.DiskSize,
			Name:             serverName + "-" + uniqueId,
			Description:      serverName + "-" + uniqueId,
			VolumeType:       disk.VolumeTypeId,
			AvailabilityZone: data.AvailabilityZone,
			Metadata: map[string]string{
				"Owner":   username,
				"Billing": data.Billing,
				"Mega ID": data.MegaId,
			},
		}

		vol, err := volumes.Create(blockStorageClient, volOpts).Extract()
		if err != nil {
			log.Println("Creating data disk volume failed.", err.Error())
			return nil, err
		}
		blockDevices = append(blockDevices, bootfromvolume.BlockDevice{SourceType: bootfromvolume.SourceVolume, DestinationType: bootfromvolume.DestinationVolume, UUID: vol.ID, BootIndex: -1})
	}

	for _, blockDevice := range blockDevices {
		err = volumes.WaitForStatus(blockStorageClient, blockDevice.UUID, "available", 60)
		if err != nil {
			log.Println("Error while waiting on data disk volume.", err.Error())
			return nil, err
		}
	}

	return blockDevices, nil
}

func listECSHandler(c *gin.Context) {
	username := common.GetUserName(c)

	log.Printf("%v lists ECS instances @ OTC.", username)

	client, err := getComputeClient()

	if err != nil {
		log.Println("Error getting compute client.", err.Error())
		if err != nil {
			c.JSON(http.StatusBadRequest, common.ApiResponse{Message: genericOTCAPIError})
			return
		}
	}

	allServers, err := getECServersByUsername(client, common.GetUserName(c))

	if err != nil {
		log.Println("Error getting ECS servers.", err.Error())
		if err != nil {
			c.JSON(http.StatusBadRequest, common.ApiResponse{Message: genericOTCAPIError})
			return
		}
	}

	c.JSON(http.StatusOK, allServers)
	return

}

func listFlavorsHandler(c *gin.Context) {
	log.Println("Querying flavors @ OTC.")

	client, err := getComputeClient()

	if err != nil {
		fmt.Println("Error getting compute client.", err.Error())
		if err != nil {
			c.JSON(http.StatusBadRequest, common.ApiResponse{Message: genericOTCAPIError})
			return
		}
	}

	allFlavors, err := getFlavors(client)

	if err != nil {
		log.Println("Error getting flavors.", err.Error())
		if err != nil {
			c.JSON(http.StatusBadRequest, common.ApiResponse{Message: genericOTCAPIError})
			return
		}
	}

	c.JSON(http.StatusOK, allFlavors)
	return
}

func listImagesHandler(c *gin.Context) {
	log.Println("Querying images @ OTC.")

	client, err := getImageClient()

	if err != nil {
		log.Println("Error getting compute client.", err.Error())
		if err != nil {
			c.JSON(http.StatusBadRequest, common.ApiResponse{Message: genericOTCAPIError})
			return
		}
	}

	allImages, err := getImages(client)

	if err != nil {
		log.Println("Error getting images.", err.Error())
		if err != nil {
			c.JSON(http.StatusBadRequest, common.ApiResponse{Message: genericOTCAPIError})
			return
		}
	}

	c.JSON(http.StatusOK, allImages)
	return
}

func listAvailabilityZonesHandler(c *gin.Context) {
	log.Println("Querying availability zones @ OTC.")

	client, err := getComputeClient()

	if err != nil {
		fmt.Println("Error getting compute client.", err.Error())
		if err != nil {
			c.JSON(http.StatusBadRequest, common.ApiResponse{Message: genericOTCAPIError})
			return
		}
	}

	allAvailabilityZones, err := getAvailabilityZones(client)

	if err != nil {
		log.Println("Error getting availability zones.", err.Error())
		if err != nil {
			c.JSON(http.StatusBadRequest, common.ApiResponse{Message: genericOTCAPIError})
			return
		}
	}

	c.JSON(http.StatusOK, allAvailabilityZones)
	return
}

func listVolumeTypesHandler(c *gin.Context) {
	log.Println("Querying volume types @ OTC.")

	client, err := getBlockStorageClient()

	if err != nil {
		fmt.Println("Error getting compute client.", err.Error())
		if err != nil {
			c.JSON(http.StatusBadRequest, common.ApiResponse{Message: genericOTCAPIError})
			return
		}
	}

	allVolumeTypes, err := getVolumeTypes(client)

	if err != nil {
		log.Println("Error getting volume types.", err.Error())
		if err != nil {
			c.JSON(http.StatusBadRequest, common.ApiResponse{Message: genericOTCAPIError})
			return
		}
	}

	c.JSON(http.StatusOK, allVolumeTypes)
	return
}

func stopECSHandler(c *gin.Context) {
	log.Println("Stopping ECS @ OTC.")

	client, err := getComputeClient()

	if err != nil {
		log.Println("Error getting compute client.", err.Error())
		if err != nil {
			c.JSON(http.StatusBadRequest, common.ApiResponse{Message: genericOTCAPIError})
			return
		}
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
		if err != nil {
			c.JSON(http.StatusBadRequest, common.ApiResponse{Message: genericOTCAPIError})
			return
		}
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
		if err != nil {
			c.JSON(http.StatusBadRequest, common.ApiResponse{Message: genericOTCAPIError})
			return
		}
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

func deleteECSHandler(c *gin.Context) {
	log.Println("Deleting ECS @ OTC.")

	client, err := getComputeClient()

	if err != nil {
		log.Println("Error getting compute client.", err.Error())
		if err != nil {
			c.JSON(http.StatusBadRequest, common.ApiResponse{Message: genericOTCAPIError})
			return
		}
	}

	var data ECServerListResponse
	err = c.BindJSON(&data)

	if err != nil {
		log.Println("Binding request to Go struct failed.", err.Error())
		c.JSON(http.StatusBadRequest, common.ApiResponse{Message: wrongAPIUsageError})
		return
	}

	for _, server := range data.ECServers {
		deleteResult := servers.Delete(client, server.Id)

		if deleteResult.Err != nil {
			log.Println("Error while deleting server.", err.Error())
			c.JSON(http.StatusBadRequest, common.ApiResponse{Message: "Mindestens ein server konnte nicht gelöscht werden."})
			return
		}
	}

	c.JSON(http.StatusOK, common.ApiResponse{Message: "Löschung wurde initiert."})
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
	log.Printf("Getting EC servers for user %v.", username)

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

	for _, server := range allServers {

		if strings.ToLower(server.Metadata["Owner"]) != strings.ToLower(username) {
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
				Billing:   server.Metadata["Billing"],
				Owner:     server.Metadata["Owner"],
				MegaId:    server.Metadata["Mega ID"]})
	}

	return &result, nil

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

	for _, image := range allImages {
		result.Images = append(result.Images, Image{Name: image.Name, Id: image.ID, MinDiskGigabytes: image.MinDiskGigabytes, MinRAMMegabytes: image.MinRAMMegabytes})
	}

	return &result, nil
}

func generateECSName(ecsName string) (string, error) {
	log.Println("Generating ECS name.")

	// <prefix>-<ecsName>
	ecsPrefix := os.Getenv("OTC_ECS_PREFIX")

	if len(ecsPrefix) < 1 {
		return "", errors.New("Environment variable OTC_ECS_PREFIX must be set.")
	}

	return strings.ToLower(ecsPrefix + "-" + ecsName), nil
}
