package openshift

import (
	"github.com/gin-gonic/gin"
	"net/http"
)

type Features struct {
	Nfs     bool `json:"nfs"`
	Gluster bool `json:"gluster"`
}

func (p Plugin) featuresHandler(c *gin.Context) {
	clusterId, _ := c.GetQuery("clusterid")
	cluster, _ := p.getOpenshiftCluster(clusterId)
	f := Features{
		Gluster: cluster.GlusterApi != nil,
		Nfs:     cluster.NfsApi != nil,
	}
	c.JSON(http.StatusOK, f)
}
