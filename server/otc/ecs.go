package otc

import (
	"fmt"
	"github.com/SchweizerischeBundesbahnen/ssp-backend/server/common"
	"github.com/gin-gonic/gin"
	"github.com/gophercloud/gophercloud"
	"github.com/gophercloud/gophercloud/openstack"
	"github.com/gophercloud/gophercloud/openstack/compute/v2/flavors"
	"github.com/gophercloud/gophercloud/openstack/compute/v2/servers"
	"github.com/gophercloud/gophercloud/openstack/imageservice/v2/images"
	"github.com/gophercloud/gophercloud/pagination"
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

	provider, err := getProviderClient()

	if err != nil {
		if err != nil {
			c.JSON(http.StatusBadRequest, err.Error())
			return
		}
	}

	client, err := openstack.NewComputeV2(provider, gophercloud.EndpointOpts{
		Region: "eu-ch",
	})

	if err != nil {
		fmt.Println("Error getting client.", err.Error())
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
		c.JSON(http.StatusBadRequest, common.ApiResponse{Message: "Unable to create server."})
		return
	} else {
		fmt.Println("Creating server succeeded.")
		c.JSON(http.StatusOK, common.ApiResponse{Message: "Server created."})
		return
	}
}

func listECSHandler(c *gin.Context) {
	username := common.GetUserName(c)

	fmt.Println(username + " lists ECS instances @ OTC.")

	provider, err := getProviderClient()

	if err != nil {
		if err != nil {
			c.JSON(http.StatusBadRequest, err.Error())
			return
		}
	}

	client, err := openstack.NewComputeV2(provider, gophercloud.EndpointOpts{
		Region: "eu-ch",
	})

	if err != nil {
		fmt.Println("Error getting client.", err.Error())
		if err != nil {
			c.JSON(http.StatusBadRequest, common.ApiResponse{Message: genericOTCAPIError})
			return
		}
	}

	opts := servers.ListOpts{}

	pager := servers.List(client, opts)

	pager.EachPage(func(page pagination.Page) (bool, error) {
		serverList, _ := servers.ExtractServers(page)

		for _, s := range serverList {
			if s.Metadata["Owner"] == username {
				fmt.Println(s.Name)
			}
		}

		return false, nil
	})

	c.JSON(http.StatusOK, "Listing OK.")
	return

}

func listFlavorsHandler(c *gin.Context) {
	fmt.Println("Querying flavors @ OTC.")

	provider, err := getProviderClient()

	if err != nil {
		if err != nil {
			c.JSON(http.StatusBadRequest, err.Error())
			return
		}
	}

	client, err := openstack.NewComputeV2(provider, gophercloud.EndpointOpts{
		Region: "eu-ch",
	})

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

	provider, err := getProviderClient()

	if err != nil {
		if err != nil {
			c.JSON(http.StatusBadRequest, err.Error())
			return
		}
	}

	client, err := openstack.NewComputeV2(provider, gophercloud.EndpointOpts{
		Region: "eu-ch",
	})

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
