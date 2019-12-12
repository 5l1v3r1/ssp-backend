package kafka

import (
	"github.com/SchweizerischeBundesbahnen/ssp-backend/server/config"
	"github.com/gin-gonic/gin"
	"log"
	"net/http"
)

type KafkaBackend struct {
	Url string `json:"url"`
}

func getAllKafkaBackendsFromConfig() KafkaBackend {
	kafka_backend := KafkaBackend{}
	err := config.Config().UnmarshalKey("kafka_backend", &kafka_backend)

	if err != nil {
		log.Println("Error unmarshalling kafka config.", err.Error())
	}

	return kafka_backend
}

func RegisterRoutes(r *gin.RouterGroup) {
	r.GET("/kafka/backend", listKafkaBackends)
}

func listKafkaBackends(c *gin.Context) {
	kafkaBackend := getAllKafkaBackendsFromConfig()

	c.JSON(http.StatusOK, kafkaBackend)
	return
}
