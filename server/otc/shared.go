package otc

import (
	"errors"
	"fmt"
	"github.com/gin-gonic/gin"
	"github.com/gophercloud/gophercloud"
	"github.com/gophercloud/gophercloud/openstack"
)

const (
	genericOTCAPIError = "Fehler beim Aufruf der OTC API. Bitte erstelle ein Ticket."
	wrongAPIUsageError = "Ungültiger API-Aufruf: Die Argumente stimmen nicht mit der definition überein. Bitte erstelle eine Ticket."
)

func RegisterRoutes(r *gin.RouterGroup) {
	r.GET("/otc/ecs", listECSHandler)
	r.POST("/otc/ecs", newECSHandler)
	r.GET("/otc/flavors", listFlavorsHandler)
	r.GET("/otc/images", listImagesHandler)
}

func getProviderClient() (*gophercloud.ProviderClient, error) {
	opts, err := openstack.AuthOptionsFromEnv()

	if err != nil {
		fmt.Println("Error while getting auth options from environment.", err.Error())
		if err != nil {
			return nil, errors.New(genericOTCAPIError)
		}
	}

	provider, err := openstack.AuthenticatedClient(opts)

	if err != nil {
		fmt.Println("Error while authenticating.", err.Error())
		if err != nil {
			return nil, errors.New(genericOTCAPIError)
		}
	}

	return provider, nil
}
