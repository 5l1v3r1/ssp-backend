package otc

import (
	"errors"
	"fmt"
	"github.com/gin-gonic/gin"
	"github.com/gophercloud/gophercloud"
	"github.com/gophercloud/gophercloud/openstack"
)

const (
	genericOTCAPIError = "Error when calling the OTC API. Please open a Jira issue"
	wrongAPIUsageError = "Invalid API request: Argument doesn't match definition. Please open a Jira issue."
)

func RegisterRoutes(r *gin.RouterGroup) {
	r.GET("/otc/ecs", listECSHandler)
	r.POST("/otc/ecs", newECSHandler)
	r.POST("/otc/stopecs", stopECSHandler)
	r.POST("/otc/startecs", startECSHandler)
	r.POST("/otc/rebootecs", rebootECSHandler)
	r.POST("/otc/deleteecs", deleteECSHandler)
	r.GET("/otc/flavors", listFlavorsHandler)
	r.GET("/otc/images", listImagesHandler)
	r.GET("/otc/availabilityzones", listAvailabilityZonesHandler)
	r.GET("/otc/volumetypes", listVolumeTypesHandler)
}

func getProvider() (*gophercloud.ProviderClient, error) {
	opts, err := authOptionsFromEnv()
	if err != nil {
		return nil, err
	}

	provider, err := openstack.AuthenticatedClient(opts)
	if err != nil {
		return nil, err
	}
	return provider, nil
}

func getComputeClient() (*gophercloud.ServiceClient, error) {
	provider, err := getProvider()
	if err != nil {
		fmt.Println("Error while authenticating.", err.Error())
		return nil, errors.New(genericOTCAPIError)
	}

	client, err := openstack.NewComputeV2(provider, gophercloud.EndpointOpts{
		Region: "eu-ch",
	})

	if err != nil {
		fmt.Println("Error getting client.", err.Error())
		return nil, errors.New(genericOTCAPIError)
	}

	return client, nil
}

func getImageClient() (*gophercloud.ServiceClient, error) {
	provider, err := getProvider()
	if err != nil {
		fmt.Println("Error while authenticating.", err.Error())
		return nil, errors.New(genericOTCAPIError)
	}

	client, err := openstack.NewImageServiceV2(provider, gophercloud.EndpointOpts{
		Region: "eu-ch",
	})

	if err != nil {
		fmt.Println("Error getting client.", err.Error())
		return nil, errors.New(genericOTCAPIError)
	}

	return client, nil
}

func getBlockStorageClient() (*gophercloud.ServiceClient, error) {
	provider, err := getProvider()
	if err != nil {
		fmt.Println("Error while authenticating.", err.Error())
		return nil, errors.New(genericOTCAPIError)
	}

	client, err := openstack.NewBlockStorageV3(provider, gophercloud.EndpointOpts{
		Region: "eu-ch",
	})

	if err != nil {
		fmt.Println("Error getting client.", err.Error())
		return nil, errors.New(genericOTCAPIError)
	}

	return client, nil
}
