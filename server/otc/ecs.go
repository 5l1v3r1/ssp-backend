package otc

import (
	"fmt"
	"github.com/SchweizerischeBundesbahnen/ssp-backend/server/common"
	"github.com/SchweizerischeBundesbahnen/ssp-backend/server/config"
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
	"strings"
)

func validateUserInput(data NewECSCommand) error {
	log.Println("Validating user input for ECS creation.")

	_, _, _, _, err := ssh.ParseAuthorizedKey([]byte(data.PublicKey))

	if err != nil {
		log.Println("Can't parse public key.", err.Error())
		if err != nil {
			return errors.New("SSH public key can't be parsed")
		}
	}

	if len(data.ECSName) == 0 {
		return errors.New("ECS name must be provided.")
	}

	if len(data.Billing) == 0 {
		return errors.New("Accounting number must be provided.")
	}

	if len(data.MegaId) == 0 {
		return errors.New("Mega ID must be provided.")
	}

	if len(data.AvailabilityZone) == 0 {
		return errors.New("Availability Zone must be provided.")
	}

	if len(data.FlavorName) == 0 {
		return errors.New("Flavor must be provided.")
	}

	if len(data.ImageId) == 0 {
		return errors.New("Flavor must be provided.")
	}

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

	if image.MinDiskGigabytes > data.RootDiskSize {
		return errors.New(fmt.Sprintf("The chosen image requires a minimal system disk size of  %vGB .", image.MinDiskGigabytes))
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
		return errors.New(fmt.Sprintf("The chosen image requires a minimal RAM size of %vGB.", image.MinRAMMegabytes/1024))
	}

	if len(data.SystemVolumeTypeId) == 0 {
		return errors.New("System disk type must be provided")
	}

	return nil
}

func newECSHandler(c *gin.Context) {
	networkId := config.Config().GetString("otc_network_uuid")
	if networkId == "" {
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

	userdata := []byte(fmt.Sprintf(`
#cloud-config
fqdn: %v
hostname: %v
manage_etc_hosts: false
bootcmd:
  - [ cloud-init-per, once, vgsystem, sh, -c, 'ret=1; i=0; while [[ $i -lt 120 ]]; do test -b /dev/vdb && lvm vgcreate vg_system /dev/vdb && ret=0 && break; i=$((i+5)); sleep 5; done; exit $ret' ]
  - [ cloud-init-per, once, mkswap, sh, -c, 'lvm vgs vg_system && lvm lvcreate -nswap -L4G vg_system && mkswap /dev/vg_system/swap' ]
  - [ cloud-init-per, once, mktmp, sh, -c, 'lvm vgs vg_system && lvm lvcreate -ntmp -L2G vg_system && mkfs.xfs /dev/vg_system/tmp && sed -i "$ a /dev/vg_system/tmp /tmp xfs nodev,nosuid 0 0" /etc/fstab && mount /tmp' ]
  - [ cloud-init-per, once, mklog, sh, -c, 'lvm vgs vg_system && lvm lvcreate -nlog -L1G vg_system && mkfs.xfs /dev/vg_system/log && sed -i "$ a /dev/vg_system/log /var/log xfs nodev,nosuid,noexec 0 0" /etc/fstab && logdirs=$(find /var/log -mindepth 1 -maxdepth 1 -type d) && mount /var/log && mkdir $logdirs && mkdir /var/log/journal && ( type restorecon && restorecon -rv /var/log || true )' ]
  - [ cloud-init-per, once, vgdata, sh, -c, 'ret=1; i=0; while [[ $i -lt 120 ]]; do test -b /dev/vdc && lvm vgcreate vg_data /dev/vdc && ret=0 && break; i=$((i+5)); sleep 5; done; exit $ret' ]
  - [ cloud-init-per, once, mkhome, sh, -c, 'lvm vgs vg_data && lvm lvcreate -nhome -L4G vg_data && mkfs.xfs /dev/vg_data/home && sed -i "$ a /dev/vg_data/home /home xfs nodev,nosuid 0 0" /etc/fstab && mount /home' ]
runcmd:
  - [ cloud-init-per, once, chownhome, sh, -c, 'chown -R 1000:1000 /home/*' ]`, serverName, serverName))

	serverCreateOpts := servers.CreateOpts{
		Name:             serverName,
		AvailabilityZone: data.AvailabilityZone,
		FlavorRef:        data.FlavorName,
		UserData:         userdata,
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
		c.JSON(http.StatusBadRequest, common.ApiResponse{Message: "Failed to create server."})
		return
	} else {
		log.Println("Creating server succeeded.")
		c.JSON(http.StatusOK, common.ApiResponse{Message: "Server created."})
		return
	}
}

func createECSDisks(data NewECSCommand, serverName string, uniqueId string, username string) ([]bootfromvolume.BlockDevice, error) {
	log.Println("Creating root, system and data disk.")

	blockStorageClient, err := getBlockStorageClient()
	if err != nil {
		log.Println("Error getting block storage client.", err.Error())
		return nil, err
	}

	metadata := map[string]string{
		"Owner":   username,
		"Billing": data.Billing,
		"Mega ID": data.MegaId,
	}

	var blockDevices []bootfromvolume.BlockDevice

	// create root disk volume
	volOpts := []volumes.CreateOpts{
		{
			Name:             serverName + "-" + uniqueId,
			Description:      serverName + "-" + uniqueId,
			Size:             data.RootDiskSize,
			VolumeType:       data.RootVolumeTypeId,
			ImageID:          data.ImageId,
			AvailabilityZone: data.AvailabilityZone,
			Metadata:         metadata,
		},
		{
			Name:             serverName + "-" + uniqueId,
			Description:      serverName + "-" + uniqueId,
			Size:             data.SystemDiskSize,
			VolumeType:       data.SystemVolumeTypeId,
			AvailabilityZone: data.AvailabilityZone,
			Metadata:         metadata,
		},
		{
			Name:             serverName + "-" + uniqueId,
			Description:      serverName + "-" + uniqueId,
			Size:             data.DataDiskSize,
			VolumeType:       data.DataVolumeTypeId,
			AvailabilityZone: data.AvailabilityZone,
			Metadata:         metadata,
		},
	}

	for _, volOpt := range volOpts {
		vol, err := volumes.Create(blockStorageClient, volOpt).Extract()
		if err != nil {
			log.Println("Creating volume failed.", err.Error())
			return nil, err
		}

		// bootIndex should be 0 for root volume
		var bootIndex int
		if volOpt.ImageID == "" {
			bootIndex = -1
		}

		blockDevices = append(blockDevices, bootfromvolume.BlockDevice{
			SourceType:      bootfromvolume.SourceVolume,
			DestinationType: bootfromvolume.DestinationVolume,
			UUID:            vol.ID,
			BootIndex:       bootIndex,
		})

		err = volumes.WaitForStatus(blockStorageClient, vol.ID, "available", 300)
		if err != nil {
			log.Println("Error while waiting on volume.", err.Error())
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
			c.JSON(http.StatusBadRequest, common.ApiResponse{Message: "At least one server couldn't be stopped."})
			return
		}
	}

	c.JSON(http.StatusOK, common.ApiResponse{Message: "Server stop initiated."})
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
			c.JSON(http.StatusBadRequest, common.ApiResponse{Message: "At least one server couldn't be started."})
			return
		}
	}

	c.JSON(http.StatusOK, common.ApiResponse{Message: "Server start initiated."})
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
			c.JSON(http.StatusBadRequest, common.ApiResponse{Message: "At least one server couldn't be rebooted."})
			return
		}
	}

	c.JSON(http.StatusOK, common.ApiResponse{Message: "Reboot initiated."})
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
			c.JSON(http.StatusBadRequest, common.ApiResponse{Message: "At least one server couldn't be deleted."})
			return
		}
	}

	c.JSON(http.StatusOK, common.ApiResponse{Message: "Deletion initiated"})
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

	volumeClient, err := getBlockStorageClient()

	if err != nil {
		log.Println("Error getting block storage client.", err.Error())
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
				Billing:   server.Metadata["Billing"],
				Owner:     server.Metadata["Owner"],
				MegaId:    server.Metadata["Mega ID"],
				Volumes:   serverVolumes})
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
			Name:             strings.TrimPrefix(image.Name, imagePrefix),
			Id:               image.ID,
			MinDiskGigabytes: image.MinDiskGigabytes,
			MinRAMMegabytes:  image.MinRAMMegabytes,
		})
	}

	return &result, nil
}

func generateECSName(ecsName string) (string, error) {
	log.Println("Generating ECS name.")

	// <prefix>-<ecsName>
	ecsPrefix := config.Config().GetString("otc_ecs_prefix")

	if len(ecsPrefix) < 1 {
		return "", errors.New("Environment variable OTC_ECS_PREFIX must be set.")
	}

	return strings.ToLower(ecsPrefix + "-" + ecsName), nil
}
