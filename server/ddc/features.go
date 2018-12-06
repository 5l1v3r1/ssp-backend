package ddc

import (
	"github.com/SchweizerischeBundesbahnen/ssp-backend/server/config"
)

type Features struct {
	Enabled bool `json:"enabled"`
}

func GetFeatures() Features {
	cfg := config.Config()
	ddcApi := cfg.GetString("ddc_api")

	return Features{
		Enabled: ddcApi != "",
	}
}
