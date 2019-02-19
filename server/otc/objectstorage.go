package otc

import (
	"github.com/SchweizerischeBundesbahnen/ssp-backend/server/common"
	"github.com/gin-gonic/gin"
	"github.com/gophercloud/gophercloud/openstack/objectstorage/v1/containers"
	"log"
	"net/http"
)

func newContainerHandler(c *gin.Context) {
	client, err := getObjectStorageClient()

	if err != nil {
		log.Println("Error getting object storage client.", err.Error())
		c.JSON(http.StatusBadRequest, common.ApiResponse{Message: genericOTCAPIError})
		return
	}

	result := containers.Create(client, "zoom", nil)

	if result.Err != nil {
		log.Println("Error creating container.", result.Err.Error())
		c.JSON(http.StatusBadRequest, common.ApiResponse{Message: genericOTCAPIError})
		return
	}
}
