package otc

import (
	"fmt"
	"github.com/SchweizerischeBundesbahnen/ssp-backend/server/common"
	"github.com/gin-gonic/gin"
	"github.com/gophercloud/gophercloud"
	"github.com/gophercloud/gophercloud/openstack/compute/v2/flavors"
	"github.com/gophercloud/gophercloud/openstack/compute/v2/servers"
	"github.com/gophercloud/gophercloud/openstack/imageservice/v2/images"
	"github.com/pkg/errors"
	"net/http"
	"os"
	"strings"
)

func newECSHandler(c *gin.Context) {
	username := common.GetUserName(c)

	fmt.Println(username + " creates new ECS @ OTC.")

	var data common.NewECSCommand
	err := c.BindJSON(&data)

	if err != nil {
		fmt.Println("Binding request to Go struct failed.", err.Error())
		c.JSON(http.StatusBadRequest, common.ApiResponse{Message: wrongAPIUsageError})
		return
	}

	client, err := getComputeClient()

	if err != nil {
		fmt.Println("Error getting compute client.", err.Error())
		if err != nil {
			c.JSON(http.StatusBadRequest, common.ApiResponse{Message: genericOTCAPIError})
			return
		}
	}

	serverName, err := generateECSName(data.ECSName)
	if err != nil {
		fmt.Println("Error generating server name.", err.Error())
		if err != nil {
			c.JSON(http.StatusBadRequest, common.ApiResponse{Message: genericOTCAPIError})
			return
		}
	}

	networkId := os.Getenv("OTC_NETWORK_UUID")

	if len(networkId) < 1 {
		fmt.Println("Environment variable OTC_NETWORK_UUID must be set.")
		c.JSON(http.StatusBadRequest, common.ApiResponse{Message: genericOTCAPIError})
		return
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
		fmt.Println("Creating server failed.", err.Error())
		c.JSON(http.StatusBadRequest, common.ApiResponse{Message: "Server konnte nicht erstellt werden."})
		return
	} else {
		fmt.Println("Creating server succeeded.")
		c.JSON(http.StatusOK, common.ApiResponse{Message: "Server erstellt."})
		return
	}
}

func listECSHandler(c *gin.Context) {
	username := common.GetUserName(c)

	fmt.Println(username + " lists ECS instances @ OTC.")

	client, err := getComputeClient()

	if err != nil {
		fmt.Println("Error getting compute client.", err.Error())
		if err != nil {
			c.JSON(http.StatusBadRequest, common.ApiResponse{Message: genericOTCAPIError})
			return
		}
	}

	allServers, err := getECServersByUsername(client, common.GetUserName(c))

	if err != nil {
		fmt.Println("Error getting ECS servers.", err.Error())
		if err != nil {
			c.JSON(http.StatusBadRequest, common.ApiResponse{Message: genericOTCAPIError})
			return
		}
	}

	c.JSON(http.StatusOK, allServers)
	return

}

func listFlavorsHandler(c *gin.Context) {
	fmt.Println("Querying flavors @ OTC.")

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
		fmt.Println("Error getting flavors.", err.Error())
		if err != nil {
			c.JSON(http.StatusBadRequest, common.ApiResponse{Message: genericOTCAPIError})
			return
		}
	}

	c.JSON(http.StatusOK, allFlavors)
	return
}

func listImagesHandler(c *gin.Context) {
	fmt.Println("Querying images @ OTC.")

	client, err := getComputeClient()

	if err != nil {
		fmt.Println("Error getting compute client.", err.Error())
		if err != nil {
			c.JSON(http.StatusBadRequest, common.ApiResponse{Message: genericOTCAPIError})
			return
		}
	}

	allImages, err := getImages(client)

	if err != nil {
		fmt.Println("Error getting images.", err.Error())
		if err != nil {
			c.JSON(http.StatusBadRequest, common.ApiResponse{Message: genericOTCAPIError})
			return
		}
	}

	c.JSON(http.StatusOK, allImages)
	return
}

func getECServersByUsername(client *gophercloud.ServiceClient, username string) (*common.ECServerListResponse, error) {
	result := common.ECServerListResponse{
		ECServers: []common.ECServer{},
	}

	opts := servers.ListOpts{}

	allPages, err := servers.List(client, opts).AllPages()

	if err != nil {
		fmt.Println("Error while listing servers.", err.Error())
		return nil, err
	}

	allServers, err := servers.ExtractServers(allPages)

	if err != nil {
		fmt.Println("Error while extracting servers.", err.Error())
		return nil, err
	}

	imageClient, err := getImageClient()

	if err != nil {
		fmt.Println("Error getting image service client.", err.Error())
		return nil, err
	}

	for _, server := range allServers {

		if strings.ToLower(server.Metadata["Owner"]) != strings.ToLower(username) {
			continue
		}

		flavor, err := flavors.Get(client, server.Flavor["id"].(string)).Extract()

		if err != nil {
			fmt.Println("Error getting flavor for a server.", err.Error())
			return nil, err
		}

		image, err := images.Get(imageClient, server.Image["id"].(string)).Extract()

		if err != nil {
			fmt.Println("Error getting image for a server.", err.Error())
			return nil, err
		}

		result.ECServers = append(result.ECServers,
			common.ECServer{
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
		fmt.Println("Error while listing flavors.", err.Error())
		return nil, err
	}

	allFlavors, err := flavors.ExtractFlavors(allPages)

	if err != nil {
		fmt.Println("Error while extracting flavors.", err.Error())
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
		fmt.Println("Error while listing images.", err.Error())
		return nil, err
	}

	allImages, err := images.ExtractImages(allPages)

	if err != nil {
		fmt.Println("Error while extracting images.", err.Error())
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
