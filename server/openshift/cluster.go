package openshift

import (
	"errors"
	"log"
	"net/http"

	"github.com/gin-gonic/gin"
)

type OpenshiftCluster struct {
	ID       string   `json:"id"`
	Name     string   `json:"name"`
	Optgroup string   `json:"optgroup"`
	Features []string `json:"features"`
	// exclude token from json marshal
	Token      string      `json:"-"`
	URL        string      `json:"url"`
	GlusterApi *GlusterApi `json:"-"`
	NfsApi     *NfsApi     `json:"-"`
}

type GlusterApi struct {
	URL          string `json:"url"`
	Secret       string `json:"-"`
	IPs          string `json:"-"`
	StorageClass string `json:"-"`
}

type NfsApi struct {
	URL          string `json:"url"`
	Secret       string `json:"-"`
	Proxy        string `json:"-"`
	StorageClass string `json:"-"`
}

func (p Plugin) clustersHandler(c *gin.Context) {
	//username := common.GetUserName(c)
	clusters := p.getOpenshiftClusters(c.Query("feature"))
	c.JSON(http.StatusOK, clusters)
}

func (p Plugin) getOpenshiftClusters(feature string) []OpenshiftCluster {
	log.Printf("Looking up clusters with the following features %v", feature)
	clusters := []OpenshiftCluster{}
	p.config.UnmarshalKey("clusters", &clusters)
	if feature != "" {
		tmp := []OpenshiftCluster{}
		for _, p := range clusters {
			if contains(p.Features, feature) {
				tmp = append(tmp, p)
			}
		}
		return tmp
	}
	return clusters
}

func contains(list []string, search string) bool {
	for _, element := range list {
		if element == search {
			return true
		}
	}
	return false
}

func (p Plugin) getOpenshiftCluster(clusterId string) (OpenshiftCluster, error) {
	if clusterId == "" {
		log.Printf("WARNING: clusterId missing!")
		return OpenshiftCluster{}, errors.New(genericAPIError)
	}
	clusters := p.getOpenshiftClusters("")
	for _, cluster := range clusters {
		if cluster.ID == clusterId {
			return cluster, nil
		}
	}
	log.Printf("WARNING: Cluster %v not found", clusterId)
	return OpenshiftCluster{}, errors.New(genericAPIError)
}

func (p Plugin) getStorageClass(clusterId, technology string) (string, error) {

	cluster, err := p.getOpenshiftCluster(clusterId)
	if err != nil {
		return "", err
	}
	var storageclass string

	if technology == "nfs" {
		if cluster.NfsApi == nil {
			log.Printf("WARNING: NfsApi is not configured for cluster %v", clusterId)
			return "", nil
		}
		storageclass = cluster.NfsApi.StorageClass

	} else {
		if cluster.GlusterApi == nil {
			log.Printf("WARNING: GlusterApi is not configured for cluster %v", clusterId)
			return "", nil
		}

		storageclass = cluster.GlusterApi.StorageClass
	}
	return storageclass, nil
}
