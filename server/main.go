package main

import (
	"github.com/SchweizerischeBundesbahnen/ssp-backend/server/aws"
	legacyConfig "github.com/SchweizerischeBundesbahnen/ssp-backend/server/config"
	"github.com/SchweizerischeBundesbahnen/ssp-backend/server/kafka"
	"github.com/SchweizerischeBundesbahnen/ssp-backend/server/keycloak"
	"github.com/SchweizerischeBundesbahnen/ssp-backend/server/ldap"
	"github.com/SchweizerischeBundesbahnen/ssp-backend/server/openshift"
	"github.com/SchweizerischeBundesbahnen/ssp-backend/server/otc"
	"github.com/SchweizerischeBundesbahnen/ssp-backend/server/sematext"
	"github.com/SchweizerischeBundesbahnen/ssp-backend/server/tower"
	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
	"github.com/sirupsen/logrus"
	"github.com/spf13/viper"

	"net/http"
	"os"
	"strings"
)

type Plugin interface {
	RegisterRoutes()
}

func config() *viper.Viper {
	c := viper.New()
	c.SetConfigType("yaml")
	c.SetConfigName("config")
	c.AddConfigPath(".")
	c.AddConfigPath("/etc/")
	c.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	c.AutomaticEnv()
	if err := c.ReadInConfig(); err != nil {
		logrus.Println("WARNING: could not load configuration file. Using ENV variables")
	}
	return c
}

func NewLogger() *logrus.Logger {
	logger := &logrus.Logger{
		Out:       os.Stdout,
		Level:     logrus.InfoLevel,
		Formatter: new(logrus.TextFormatter),
	}
	//logger.SetReportCaller(true)
	return logger
}

func main() {
	log := NewLogger()
	cfg := config()

	// Backwards compatibility
	legacyConfig.Init("")

	if !cfg.GetBool("debug") {
		gin.SetMode(gin.ReleaseMode)
	}

	gin.DefaultWriter = log.Writer()
	gin.DefaultErrorWriter = log.WriterLevel(logrus.ErrorLevel)

	router := gin.New()
	router.Use(gin.Recovery())

	// Allow cors
	corsConfig := cors.DefaultConfig()
	corsConfig.AllowAllOrigins = true
	corsConfig.AddAllowHeaders("authorization", "*")
	corsConfig.AddAllowMethods("DELETE")
	router.Use(cors.New(corsConfig))

	// Public routes
	router.GET("/features", featuresHandler)

	// Protected routes
	api := router.Group("/api/")
	api.Use(keycloak.Auth(keycloak.LoggedInCheck()))
	{
		// AWS routes
		aws.RegisterRoutes(api)

		// OTC routes
		otc.RegisterRoutes(api)

		// Sematext routes
		sematext.RegisterRoutes(api)

		// Ansible Tower
		tower.RegisterRoutes(api)

		// Kafka routes
		kafka.RegisterRoutes(api)

		// LDAP routes
		ldap.RegisterRoutes(api)
	}

	plugins := []Plugin{
		openshift.New(api.Group("/ose/"), cfg.Sub("openshift"), log),
	}

	for _, plugin := range plugins {
		plugin.RegisterRoutes()
	}

	log.Println("Cloud SSP is running")

	port := cfg.GetString("port")
	if port == "" {
		port = "8000"
	}
	err := router.Run(":" + port)
	if err != nil {
		log.Println(err)
	}
}

// not in common package, because that generates an import loop
type featureToggleResponse struct {
	OTC   otc.Features   `json:"otc"`
	Kafka kafka.Features `json:"kafka"`
}

func featuresHandler(c *gin.Context) {
	c.JSON(http.StatusOK, featureToggleResponse{
		OTC:   otc.GetFeatures(),
		Kafka: kafka.GetFeatures(),
	})
}
