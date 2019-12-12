package kafka

type Features struct {
	Enabled bool `json:"enabled"`
}

func GetFeatures() Features {
	kafkaBackend := getAllKafkaBackendsFromConfig()

	return Features{
		Enabled: kafkaBackend.Url != "",
	}
}
