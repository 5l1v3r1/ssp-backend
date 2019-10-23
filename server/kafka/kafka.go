package kafka

import (
	"github.com/SchweizerischeBundesbahnen/ssp-backend/server/config"
	"github.com/gin-gonic/gin"
	"log"
	"net/http"
)

type KafkaBackend struct {
	Name string `json:"name"`
	Url  string `json:"url"`
}

func getAllKafkaBackendsFromConfig() []KafkaBackend {
	kafka_backends := []KafkaBackend{}
	err := config.Config().UnmarshalKey("kafka_backends", &kafka_backends)

	if err != nil {
		log.Println("Error unmarshalling kafka config.", err.Error())
	}

	return kafka_backends
}

func RegisterRoutes(r *gin.RouterGroup) {
	r.GET("/kafka/backends", listKafkaBackends)
}

func listKafkaBackends(c *gin.Context) {
	clusters := getAllKafkaBackendsFromConfig()

	c.JSON(http.StatusOK, clusters)
	return
}
