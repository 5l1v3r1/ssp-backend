package kafka

type Features struct {
	Enabled bool `json:"enabled"`
}

func GetFeatures() Features {
	kafkaBackends := getAllKafkaBackendsFromConfig()

	return Features{
		Enabled: len(kafkaBackends) > 0,
	}
}
