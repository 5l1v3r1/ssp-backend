package otc

import (
	"github.com/SchweizerischeBundesbahnen/ssp-backend/server/config"
)

type Features struct {
	Enabled bool `json:"enabled"`
}

func GetFeatures() Features {
	cfg := config.Config()
	otcApi := cfg.GetString("otc_api")

	return Features{
		Enabled: otcApi != "",
	}
}
