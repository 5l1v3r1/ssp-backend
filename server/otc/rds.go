package otc

import (
	"github.com/SchweizerischeBundesbahnen/ssp-backend/server/common"
	"github.com/gin-gonic/gin"
	"github.com/gophercloud/gophercloud/openstack/rds/v3/datastores"
	"github.com/gophercloud/gophercloud/openstack/rds/v3/flavors"
	"github.com/gophercloud/gophercloud/openstack/rds/v3/instances"
	"log"
	"net/http"
)

func listRDSFlavorsHandler(c *gin.Context) {
	client, err := getRDSClient()
	if err != nil {
		log.Println("Error getting rds client.", err.Error())
		c.JSON(http.StatusBadRequest, common.ApiResponse{Message: genericOTCAPIError})
		return
	}

	version := c.Request.URL.Query().Get("version_name")
	if version == "" {
		c.JSON(http.StatusBadRequest, common.ApiResponse{Message: "Wrong API usage"})
		return
	}
	dbFlavorsOpts := flavors.DbFlavorsOpts{
		Versionname: version,
	}

	allPages, err := flavors.List(client, dbFlavorsOpts, "postgresql").AllPages()
	if err != nil {
		log.Println("Error while listing flavors.", err.Error())
		c.JSON(http.StatusBadRequest, common.ApiResponse{Message: "There was a problem getting the available database flavors"})
		return
	}

	flavors, err := flavors.ExtractDbFlavors(allPages)
	if err != nil {
		log.Println("Error while extracting flavors.", err.Error())
		c.JSON(http.StatusBadRequest, common.ApiResponse{Message: "There was a problem getting the available database flavors"})
		return
	}

	c.JSON(http.StatusOK, flavors)
	return
}

func listRDSVersionsHandler(c *gin.Context) {
	client, err := getRDSClient()
	if err != nil {
		log.Println("Error getting rds client.", err.Error())
		c.JSON(http.StatusBadRequest, common.ApiResponse{Message: genericOTCAPIError})
		return
	}

	allPages, err := datastores.List(client, "postgresql").AllPages()
	if err != nil {
		log.Println("Error while listing datastores.", err.Error())
		c.JSON(http.StatusBadRequest, common.ApiResponse{Message: "There was a problem getting the available database versions"})
		return
	}

	datastores, err := datastores.ExtractDataStores(allPages)
	if err != nil {
		log.Println("Error while extracting datastores.", err.Error())
		c.JSON(http.StatusBadRequest, common.ApiResponse{Message: "There was a problem getting the available database versions"})
		return
	}

	versions := make([]string, len(datastores.DataStores))

	for i, d := range datastores.DataStores {
		versions[i] = d.Name
	}

	c.JSON(http.StatusOK, versions)
	return
}

func listRDSInstancesHandler(c *gin.Context) {
	client, err := getRDSClient()
	if err != nil {
		log.Println("Error getting rds client.", err.Error())
		c.JSON(http.StatusBadRequest, common.ApiResponse{Message: genericOTCAPIError})
		return
	}

	allPages, err := instances.List(client, nil).AllPages()
	if err != nil {
		log.Println("Error while listing instances.", err.Error())
		c.JSON(http.StatusBadRequest, common.ApiResponse{Message: "There was a problem getting the databases"})
		return
	}

	instances, err := instances.ExtractRdsInstances(allPages)
	if err != nil {
		log.Println("Error while extracting instances.", err.Error())
		c.JSON(http.StatusBadRequest, common.ApiResponse{Message: "There was a problem getting the databases"})
		return
	}

	log.Printf("%+v", instances)
	versions := make([]string, 5)

	c.JSON(http.StatusOK, versions)
	return
}
