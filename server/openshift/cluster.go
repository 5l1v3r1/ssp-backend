package openshift

import (
	"errors"
	"log"
	"net/http"

	"github.com/SchweizerischeBundesbahnen/ssp-backend/server/config"
	"github.com/gin-gonic/gin"
)

type OpenshiftCluster struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	// exclude token from json marshal
	Token string `json:"-"`
	URL   string `json:"url"`
}

func clustersHandler(c *gin.Context) {
	//username := common.GetUserName(c)
	clusters := getOpenshiftClusters()
	c.JSON(http.StatusOK, clusters)
}

func getOpenshiftClusters() []OpenshiftCluster {
	clusters := []OpenshiftCluster{}
	config.Config().UnmarshalKey("openshift", &clusters)
	return clusters
}

func getOpenshiftCluster(clusterId string) (OpenshiftCluster, error) {
	if clusterId == "" {
		log.Println("WARNING: ClusterId is empty")
		return OpenshiftCluster{}, errors.New(genericAPIError)
	}
	clusters := getOpenshiftClusters()
	for _, cluster := range clusters {
		log.Println(cluster.ID)
		if cluster.ID == clusterId {
			return cluster, nil
		}
	}
	log.Printf("WARNING: Cluster %v not found", clusterId)
	return OpenshiftCluster{}, errors.New(genericAPIError)
}
