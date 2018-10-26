package main

import (
	"log"
	"os"

	"github.com/SchweizerischeBundesbahnen/ssp-backend/server/aws"
	"github.com/SchweizerischeBundesbahnen/ssp-backend/server/common"
	"github.com/SchweizerischeBundesbahnen/ssp-backend/server/ddc"
	"github.com/SchweizerischeBundesbahnen/ssp-backend/server/openshift"
	"github.com/SchweizerischeBundesbahnen/ssp-backend/server/otc"
	"github.com/SchweizerischeBundesbahnen/ssp-backend/server/sematext"
	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
)

func main() {
	router := gin.New()
	router.Use(gin.Recovery())

	// Allow cors
	corsConfig := cors.DefaultConfig()
	corsConfig.AllowAllOrigins = true
	corsConfig.AddAllowHeaders("authorization", "*")
	corsConfig.AddAllowMethods("DELETE")
	router.Use(cors.New(corsConfig))

	// Public routes
	authMiddleware := common.GetAuthMiddleware()
	router.POST("/login", authMiddleware.LoginHandler)
	router.GET("/config", common.ConfigHandler)

	// Protected routes
	auth := router.Group("/api/")
	auth.Use(authMiddleware.MiddlewareFunc())
	{
		// Openshift routes
		openshift.RegisterRoutes(auth)

		// DDC routes
		ddc.RegisterRoutes(auth)

		// AWS routes
		aws.RegisterRoutes(auth)

		// OTC routes
		otc.RegisterRoutes(auth)

		// Sematext routes
		sematext.RegisterRoutes(auth)
	}

	secApiPassword, ok := os.LookupEnv("SEC_API_PASSWORD")
	if ok {
		log.Println("Activating secure api (basic auth)")
		sec := router.Group("/sec", gin.BasicAuth(gin.Accounts{"SEC_API": secApiPassword}))
		openshift.RegisterSecRoutes(sec)
	} else {
		log.Println("Secure api (basic auth) won't be activated, because SEC_API_PASSWORD isn't set")
	}

	log.Println("Cloud SSP is running")
	router.Run()
}
