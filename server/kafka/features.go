package kafka

type Features struct {
	Enabled bool `json:"enabled"`
}

func GetFeatures() Features {
	kafkaBackend := getKafkaConfig()

	return Features{
		Enabled: kafkaConfig.BackendUrl != "",
	}
}
