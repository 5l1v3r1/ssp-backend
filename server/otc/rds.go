package otc

import (
	"github.com/SchweizerischeBundesbahnen/ssp-backend/server/common"
	"github.com/SchweizerischeBundesbahnen/ssp-backend/server/ldap"
	"github.com/gin-gonic/gin"
	"github.com/gophercloud/gophercloud"
	"github.com/gophercloud/gophercloud/openstack/rds/v1/tags"
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
	username := common.GetUserName(c)

	client, err := getRDSClient()
	if err != nil {
		log.Println("Error getting rds client.", err.Error())
		c.JSON(http.StatusBadRequest, common.ApiResponse{Message: genericOTCAPIError})
		return
	}

	instances, err := getRDSInstancesByUsername(client, username)
	if err != nil {
		c.JSON(http.StatusBadRequest, common.ApiResponse{Message: genericOTCAPIError})
		return
	}
	c.JSON(http.StatusOK, instances)
	return
}

type rdsInstance struct {
	instances.RdsInstanceResponse
	Tags map[string]string
}

func getRDSInstancesByUsername(client *gophercloud.ServiceClient, username string) ([]rdsInstance, error) {
	// Use make because of the following behaviour:
	// https://github.com/gin-gonic/gin/issues/125
	filteredInstances := make([]rdsInstance, 0)
	l, err := ldap.New()
	if err != nil {
		return nil, err
	}
	defer l.Close()

	groups, err := l.GetGroupsOfUser(username)
	if err != nil {
		return nil, err
	}
	log.Printf("%v", groups)

	instances, err := getRDSInstances(client)
	if err != nil {
		log.Println("Error getting rds client.", err.Error())
		return nil, err
	}

	log.Printf("%+v", instances)
	log.Printf("%v", len(instances))

	for _, instance := range instances {
		if instance.Type == "slave" {
			continue
		}
		t, err := getRDSTags(client, instance.Id)
		if err != nil {
			log.Printf("%+v", instance)
		}
		if t["Group"] == "" {
			continue
		}
		if !common.ContainsStringI(groups, t["Group"]) {
			continue
		}
		filteredInstances = append(filteredInstances, rdsInstance{instance, t})
		log.Printf("ALLOWED %v %v", username, instance.Id)
	}
	return filteredInstances, nil
}

func getRDSInstances(client *gophercloud.ServiceClient) ([]instances.RdsInstanceResponse, error) {

	allPages, err := instances.List(client, nil).AllPages()
	if err != nil {
		return nil, err
	}

	instances, err := instances.ExtractRdsInstances(allPages)
	if err != nil {
		return nil, err
	}

	return instances.Instances, nil
}

func getRDSTags(client *gophercloud.ServiceClient, id string) (map[string]string, error) {
	t, err := tags.GetTags(client, id).Extract()
	if err != nil {
		log.Printf("Error while listing tags for instance: %v. %v", id, err.Error())
		return nil, err
	}
	return t, nil
}
