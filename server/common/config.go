package common

import (
	"net/http"
	"os"

	"github.com/gin-gonic/gin"
)

func ConfigHandler(c *gin.Context) {
	glusterApi := os.Getenv("GLUSTER_API_URL")
	nfsApi := os.Getenv("NFS_API_URL")
	ddcApi := os.Getenv("DDC_API")
	otcApi := os.Getenv("OS_AUTH_URL")

	c.JSON(http.StatusOK, FeatureToggleResponse{
		DDC:     ddcApi != "",
		Gluster: glusterApi != "",
		Nfs:     nfsApi != "",
		OTC:     otcApi != "",
	})
}
