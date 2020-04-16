package otc

import (
	"fmt"
	"github.com/SchweizerischeBundesbahnen/ssp-backend/server/common"
	"github.com/SchweizerischeBundesbahnen/ssp-backend/server/config"
	"github.com/SchweizerischeBundesbahnen/ssp-backend/server/ldap"
	"github.com/gin-gonic/gin"
	"github.com/google/go-cmp/cmp"
	"github.com/gophercloud/gophercloud"
	"github.com/gophercloud/gophercloud/openstack/blockstorage/v1/volumetypes"
	"github.com/gophercloud/gophercloud/openstack/blockstorage/v3/volumes"
	"github.com/gophercloud/gophercloud/openstack/compute/v2/extensions/availabilityzones"
	"github.com/gophercloud/gophercloud/openstack/compute/v2/extensions/keypairs"
	"github.com/gophercloud/gophercloud/openstack/compute/v2/extensions/startstop"
	"github.com/gophercloud/gophercloud/openstack/compute/v2/flavors"
	"github.com/gophercloud/gophercloud/openstack/compute/v2/servers"
	"github.com/gophercloud/gophercloud/openstack/imageservice/v2/images"
	log "github.com/sirupsen/logrus"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"
)

func listECSHandler(c *gin.Context) {
	username := common.GetUserName(c)

	log.Printf("%v lists ECS instances @ OTC.", username)

	params := c.Request.URL.Query()
	showall, err := strconv.ParseBool(params.Get("showall"))
	if err != nil {
		log.Printf("Error parsing showall: %v", err)
		c.JSON(http.StatusBadRequest, common.ApiResponse{Message: genericOTCAPIError})
		return
	}
	var allServers []servers.Server
	clients, err := getComputeClients()
	if err != nil {
		log.Printf("Error getting compute clients: %v", err)
		c.JSON(http.StatusBadRequest, common.ApiResponse{Message: genericOTCAPIError})
		return
	}
	for _, client := range clients {
		serversInTenant, err := getServersByUsername(client, common.GetUserName(c), showall)
		if err != nil {
			log.Println("Error getting ECS servers.", err.Error())
			c.JSON(http.StatusBadRequest, common.ApiResponse{Message: genericOTCAPIError})
			return
		}
		allServers = append(allServers, serversInTenant...)
	}

	if allServers == nil {
		allServers = []servers.Server{}
	}

	c.JSON(http.StatusOK, ECServerListResponse{Servers: allServers})
	return
}

func listFlavorsHandler(c *gin.Context) {
	log.Println("Querying flavors @ OTC.")
	stage := c.Request.URL.Query().Get("stage")
	if stage == "" {
		c.JSON(http.StatusBadRequest, common.ApiResponse{Message: "Wrong API usage. Missing parameter stage"})
		return
	}
	if stage != "p" && stage != "t" {
		c.JSON(http.StatusBadRequest, common.ApiResponse{Message: fmt.Sprintf("Wrong API usage. Parameter stage is: %v. Should be p or t", stage)})
		return
	}
	tenant := fmt.Sprintf("SBB_RZ_%v_001", strings.ToUpper(stage))
	client, err := getComputeClient(tenant)

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

func getComputeClients() (map[string]*gophercloud.ServiceClient, error) {
	tenants := []string{
		"SBB_RZ_T_001",
		"SBB_RZ_P_001",
	}
	clients := make(map[string]*gophercloud.ServiceClient)
	var err error
	for _, tenant := range tenants {
		clients[tenant], err = getComputeClient(tenant)
		if err != nil {
			return clients, err
		}
	}
	return clients, nil
}

func getTenantName(servername string) string {
	pattern := regexp.MustCompile(`(.)\d{2}\.sbb\.ch`)
	matches := pattern.FindStringSubmatch(servername)
	if len(matches) == 2 {
		stage := matches[1]
		return fmt.Sprintf("SBB_RZ_%v_001", strings.ToUpper(stage))
	}
	return ""
}

func stopECSHandler(c *gin.Context) {
	log.Println("Stopping ECS @ OTC.")
	username := common.GetUserName(c)

	clients, err := getComputeClients()
	if err != nil {
		log.Printf("Error getting compute client: %v", err)
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
	if err := validatePermissions(clients, data.Servers, username); err != nil {
		c.JSON(http.StatusForbidden, common.ApiResponse{Message: err.Error()})
		return
	}

	for _, server := range data.Servers {
		tenant := getTenantName(server.Name)
		stopResult := startstop.Stop(clients[tenant], server.ID)

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
	username := common.GetUserName(c)

	clients, err := getComputeClients()
	if err != nil {
		log.Printf("Error getting compute clients: %v", err)
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
	if err := validatePermissions(clients, data.Servers, username); err != nil {
		c.JSON(http.StatusForbidden, common.ApiResponse{Message: err.Error()})
		return
	}
	for _, server := range data.Servers {
		tenant := getTenantName(server.Name)
		stopResult := startstop.Start(clients[tenant], server.ID)

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
	username := common.GetUserName(c)

	clients, err := getComputeClients()
	if err != nil {
		log.Printf("Error getting compute client: %v", err)
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

	if err := validatePermissions(clients, data.Servers, username); err != nil {
		c.JSON(http.StatusForbidden, common.ApiResponse{Message: err.Error()})
		return
	}
	for _, server := range data.Servers {
		tenant := getTenantName(server.Name)
		rebootResult := servers.Reboot(clients[tenant], server.ID, &rebootOpts)

		if rebootResult.Err != nil {
			log.Printf("Error while rebooting server: %v", rebootResult.Err)
			c.JSON(http.StatusBadRequest, common.ApiResponse{Message: "At least one server couldn't be rebooted."})
			return
		}
	}
	c.JSON(http.StatusOK, common.ApiResponse{Message: "Reboot initiated."})
	return
}

func validatePermissions(clients map[string]*gophercloud.ServiceClient, untrustedServers []servers.Server, username string) error {
	groups, err := getGroups(username)
	if err != nil {
		return err
	}
	if common.ContainsStringI(groups, "DG_RBT_UOS_ADMINS") {
		// skip checks
		return nil
	}
	var allServers []servers.Server
	for _, client := range clients {
		serversInTenant, err := getServersByUsername(client, username, false)
		if err != nil {
			return err
		}
		allServers = append(allServers, serversInTenant...)
	}
	for _, untrustedServer := range untrustedServers {
		// do not trust user data, because the metadata could have been modified
		var server *servers.Server
		for _, s := range allServers {
			if untrustedServer.ID == s.ID {
				if !cmp.Equal(untrustedServer.Metadata, s.Metadata) {
					log.WithFields(log.Fields{
						"username": username,
						"server":   s.ID,
						"received": untrustedServer.Metadata,
						"metadata": s.Metadata,
					}).Error("Server metadata doesn't match received metadata")
					return fmt.Errorf(genericOTCAPIError)
				}
				server = &s
				break
			}
		}
		if server == nil {
			log.WithFields(log.Fields{
				"username": username,
				"server":   untrustedServer.ID,
			}).Error("Server not found")
			return fmt.Errorf(genericOTCAPIError)
		}
		group := server.Metadata["uos_group"]
		if group == "" {
			log.WithFields(log.Fields{
				"username": username,
				"server":   server.ID,
				"metadata": server.Metadata,
			}).Error("uos_group not found in metadata")
			return fmt.Errorf(genericOTCAPIError)
		}
		if !common.ContainsStringI(groups, group) {
			log.WithFields(log.Fields{
				"username": username,
				"groups":   groups,
				"server":   server.ID,
				"metadata": server.Metadata,
			}).Error("uos_group not found in user groups")
			return fmt.Errorf(genericOTCAPIError)
		}
	}
	return nil
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

type otcTenantCache struct {
	LastRunTimestamp string
	Servers          []servers.Server
}

// Cache for all tenants
var otcCache map[string]otcTenantCache

func getServersByUsername(client *gophercloud.ServiceClient, username string, showall bool) ([]servers.Server, error) {
	if otcCache == nil {
		otcCache = make(map[string]otcTenantCache)
	}
	log.WithFields(log.Fields{
		"username": username,
	}).Debug("Getting EC Servers.")

	groups, err := getGroups(username)
	if err != nil {
		return nil, err
	}
	log.WithFields(log.Fields{
		"groups":   groups,
		"username": username,
	}).Debug("LDAP groups")

	cacheKey := client.Endpoint
	// this seems to work even if lastRunTimestamp is empty
	opts := servers.ListOpts{
		ChangesSince: otcCache[cacheKey].LastRunTimestamp,
	}

	allPages, err := servers.List(client, opts).AllPages()
	if err != nil {
		log.Println("Error while listing servers.", err.Error())
		return nil, err
	}

	newServers, err := servers.ExtractServers(allPages)
	if err != nil {
		log.Println("Error while extracting servers.", err.Error())
		return nil, err
	}

	otcCache[cacheKey] = otcTenantCache{
		LastRunTimestamp: time.Now().Format(time.RFC3339),
		Servers:          mergeServers(otcCache[cacheKey].Servers, newServers),
	}

	if showall && common.ContainsStringI(groups, "DG_RBT_UOS_ADMINS") {
		return otcCache[cacheKey].Servers, nil
	}

	var filteredServers []servers.Server
	for _, server := range otcCache[cacheKey].Servers {
		if common.ContainsStringI(groups, server.Metadata["Group"]) {
			filteredServers = append(filteredServers, server)
		}
	}
	return filteredServers, nil
}

func getGroups(username string) ([]string, error) {
	l, err := ldap.New()
	if err != nil {
		return nil, err
	}
	defer l.Close()

	groups, err := l.GetGroupsOfUser(username)
	if err != nil {
		return nil, err
	}
	return groups, nil
}

func mergeServers(cachedServers, newServers []servers.Server) []servers.Server {
	unique := make(map[string]servers.Server)

	for _, s := range cachedServers {
		unique[s.ID] = s
	}
	for _, s := range newServers {
		unique[s.ID] = s
	}
	final := make([]servers.Server, 0)
	for _, s := range unique {
		final = append(final, s)
	}
	return final
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
		imagePrefix = "SBB-UnifiedOS_"
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
