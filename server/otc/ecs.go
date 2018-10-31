package otc

import (
	"fmt"
	"github.com/SchweizerischeBundesbahnen/ssp-backend/server/common"
	"github.com/gin-gonic/gin"
	"github.com/gophercloud/gophercloud"
	"github.com/gophercloud/gophercloud/openstack/compute/v2/extensions/startstop"
	"github.com/gophercloud/gophercloud/openstack/compute/v2/flavors"
	"github.com/gophercloud/gophercloud/openstack/compute/v2/servers"
	"github.com/gophercloud/gophercloud/openstack/imageservice/v2/images"
	"github.com/pkg/errors"
	"log"
	"net/http"
	"os"
	"strings"
)

func newECSHandler(c *gin.Context) {
	networkId := os.Getenv("OTC_NETWORK_UUID")

	if len(networkId) < 1 {
		log.Println("Environment variable OTC_NETWORK_UUID must be set.")
		c.JSON(http.StatusBadRequest, common.ApiResponse{Message: genericOTCAPIError})
		return
	}

	username := common.GetUserName(c)
	log.Printf("%v creates new ECS @ OTC.", username)

	var data common.NewECSCommand
	err := c.BindJSON(&data)

	if err != nil {
		log.Println("Binding request to Go struct failed.", err.Error())
		c.JSON(http.StatusBadRequest, common.ApiResponse{Message: wrongAPIUsageError})
		return
	}

	client, err := getComputeClient()

	if err != nil {
		log.Println("Error getting compute client.", err.Error())
		if err != nil {
			c.JSON(http.StatusBadRequest, common.ApiResponse{Message: genericOTCAPIError})
			return
		}
	}

	serverName, err := generateECSName(data.ECSName)
	if err != nil {
		log.Println("Error generating server name.", err.Error())
		if err != nil {
			c.JSON(http.StatusBadRequest, common.ApiResponse{Message: genericOTCAPIError})
			return
		}
	}

	_, err = servers.Create(client, servers.CreateOpts{
		Name:      serverName,
		FlavorRef: data.FlavorName,
		ImageRef:  data.ImageId,
		Networks: []servers.Network{
			{
				UUID: networkId,
			},
		},
		Metadata: map[string]string{
			"Owner":   username,
			"Billing": data.Billing,
		},
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

	client, err := getComputeClient()

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

	var data common.ECServerListResponse
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

	var data common.ECServerListResponse
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

	var data common.ECServerListResponse
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

	var data common.ECServerListResponse
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

func getECServersByUsername(client *gophercloud.ServiceClient, username string) (*common.ECServerListResponse, error) {
	result := common.ECServerListResponse{
		ECServers: []common.ECServer{},
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

		result.ECServers = append(result.ECServers,
			common.ECServer{
				Id:        server.ID,
				Name:      server.Name,
				Created:   server.Created,
				VCPUs:     flavor.VCPUs,
				RAM:       flavor.RAM,
				ImageName: image.Name,
				Status:    server.Status,
				Billing:   server.Metadata["Billing"],
				Owner:     server.Metadata["Owner"]})
	}

	return &result, nil

}

func getFlavors(client *gophercloud.ServiceClient) (*common.FlavorListResponse, error) {
	result := common.FlavorListResponse{
		Flavors: []common.Flavor{},
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
		result.Flavors = append(result.Flavors, common.Flavor{flavor.Name, flavor.VCPUs, flavor.RAM})
	}

	return &result, nil
}

func getImages(client *gophercloud.ServiceClient) (*common.ImageListResponse, error) {
	result := common.ImageListResponse{
		Images: []common.Image{},
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
		result.Images = append(result.Images, common.Image{image.Name, image.ID})
	}

	return &result, nil
}

func generateECSName(ecsName string) (string, error) {
	// <prefix>-<ecsName>
	ecsPrefix := os.Getenv("OTC_ECS_PREFIX")

	if len(ecsPrefix) < 1 {
		return "", errors.New("Environment variable OTC_ECS_PREFIX must be set.")
	}

	return strings.ToLower(ecsPrefix + "-" + ecsName), nil
}
