package common

import (
	"github.com/SchweizerischeBundesbahnen/ssp-backend/server/config"
	"net/http"

	"github.com/gin-gonic/gin"
)

func ConfigHandler(c *gin.Context) {
	cfg := config.Config()
	glusterApi := cfg.GetString("gluster_api_url")
	nfsApi := cfg.GetString("nfs_api_url")
	ddcApi := cfg.GetString("ddc_api")

	c.JSON(http.StatusOK, FeatureToggleResponse{
		DDC:     ddcApi != "",
		Gluster: glusterApi != "",
		Nfs:     nfsApi != "",
	})
}
