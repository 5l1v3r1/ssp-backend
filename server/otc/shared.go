package otc

import (
	"errors"
	"fmt"
	"github.com/SchweizerischeBundesbahnen/ssp-backend/server/config"
	"github.com/gin-gonic/gin"
	"github.com/gophercloud/gophercloud"
	"github.com/gophercloud/gophercloud/openstack"
)

const (
	genericOTCAPIError = "Fehler beim Aufruf der OTC API. Bitte erstelle ein Ticket."
	wrongAPIUsageError = "Ungültiger API-Aufruf: Die Argumente stimmen nicht mit der definition überein. Bitte erstelle eine Ticket."
)

func RegisterRoutes(r *gin.RouterGroup) {
	r.GET("/otc/ecs", listECSHandler)
	r.POST("/otc/ecs", newECSHandler)
	r.POST("/otc/stopecs", stopECSHandler)
	r.POST("/otc/startecs", startECSHandler)
	r.POST("/otc/rebootecs", rebootECSHandler)
	r.POST("/otc/deleteecs", deleteECSHandler)
	r.GET("/otc/flavors", listFlavorsHandler)
	r.GET("/otc/images", listImagesHandler)
	r.GET("/otc/availabilityzones", listAvailabilityZonesHandler)
	r.GET("/otc/volumetypes", listVolumeTypesHandler)
}

var (
	ErrNoAuthURL  = fmt.Errorf("Environment variable OS_AUTH_URL needs to be set.")
	ErrNoUsername = fmt.Errorf("Environment variable OS_USERNAME, OS_USERID, or OS_TOKEN needs to be set.")
	ErrNoPassword = fmt.Errorf("Environment variable OS_PASSWORD or OS_TOKEN needs to be set.")
)
var nilOptions = gophercloud.AuthOptions{}

// From https://github.com/gophercloud/gophercloud/blob/master/openstack/auth_env.go
// Updated to use viper and fallback to env
func authOptionsFromEnv() (gophercloud.AuthOptions, error) {
	cfg := config.Config()

	authURL := cfg.GetString("os_auth_url")
	username := cfg.GetString("os_username")
	userID := cfg.GetString("os_userid")
	password := cfg.GetString("os_password")
	tenantID := cfg.GetString("os_tenant_id")
	tenantName := cfg.GetString("os_tenant_name")
	domainID := cfg.GetString("os_domain_id")
	domainName := cfg.GetString("os_domain_name")
	applicationCredentialID := cfg.GetString("os_application_credential_id")
	applicationCredentialName := cfg.GetString("os_application_credential_name")
	applicationCredentialSecret := cfg.GetString("os_application_credential_secret")

	// If OS_PROJECT_ID is set, overwrite tenantID with the value.
	if v := cfg.GetString("os_project_id"); v != "" {
		tenantID = v
	}

	// If OS_PROJECT_NAME is set, overwrite tenantName with the value.
	if v := cfg.GetString("os_project_name"); v != "" {
		tenantName = v
	}

	// end custom part

	if authURL == "" {
		err := gophercloud.ErrMissingEnvironmentVariable{
			EnvironmentVariable: "OS_AUTH_URL",
		}
		return nilOptions, err
	}

	if userID == "" && username == "" {
		// Empty username and userID could be ignored, when applicationCredentialID and applicationCredentialSecret are set
		if applicationCredentialID == "" && applicationCredentialSecret == "" {
			err := gophercloud.ErrMissingAnyoneOfEnvironmentVariables{
				EnvironmentVariables: []string{"OS_USERID", "OS_USERNAME"},
			}
			return nilOptions, err
		}
	}

	if password == "" && applicationCredentialID == "" && applicationCredentialName == "" {
		err := gophercloud.ErrMissingEnvironmentVariable{
			EnvironmentVariable: "OS_PASSWORD",
		}
		return nilOptions, err
	}

	if (applicationCredentialID != "" || applicationCredentialName != "") && applicationCredentialSecret == "" {
		err := gophercloud.ErrMissingEnvironmentVariable{
			EnvironmentVariable: "OS_APPLICATION_CREDENTIAL_SECRET",
		}
		return nilOptions, err
	}

	if applicationCredentialID == "" && applicationCredentialName != "" && applicationCredentialSecret != "" {
		if userID == "" && username == "" {
			return nilOptions, gophercloud.ErrMissingAnyoneOfEnvironmentVariables{
				EnvironmentVariables: []string{"OS_USERID", "OS_USERNAME"},
			}
		}
		if username != "" && domainID == "" && domainName == "" {
			return nilOptions, gophercloud.ErrMissingAnyoneOfEnvironmentVariables{
				EnvironmentVariables: []string{"OS_DOMAIN_ID", "OS_DOMAIN_NAME"},
			}
		}
	}

	ao := gophercloud.AuthOptions{
		IdentityEndpoint:            authURL,
		UserID:                      userID,
		Username:                    username,
		Password:                    password,
		TenantID:                    tenantID,
		TenantName:                  tenantName,
		DomainID:                    domainID,
		DomainName:                  domainName,
		ApplicationCredentialID:     applicationCredentialID,
		ApplicationCredentialName:   applicationCredentialName,
		ApplicationCredentialSecret: applicationCredentialSecret,
	}

	return ao, nil
}

func getProvider() (*gophercloud.ProviderClient, error) {
	opts, err := authOptionsFromEnv()
	if err != nil {
		return nil, err
	}

	provider, err := openstack.AuthenticatedClient(opts)
	if err != nil {
		return nil, err
	}
	return provider, nil
}

func getComputeClient() (*gophercloud.ServiceClient, error) {
	provider, err := getProvider()
	if err != nil {
		fmt.Println("Error while authenticating.", err.Error())
		return nil, errors.New(genericOTCAPIError)
	}

	client, err := openstack.NewComputeV2(provider, gophercloud.EndpointOpts{
		Region: "eu-ch",
	})

	if err != nil {
		fmt.Println("Error getting client.", err.Error())
		return nil, errors.New(genericOTCAPIError)
	}

	return client, nil
}

func getImageClient() (*gophercloud.ServiceClient, error) {
	provider, err := getProvider()
	if err != nil {
		fmt.Println("Error while authenticating.", err.Error())
		return nil, errors.New(genericOTCAPIError)
	}

	client, err := openstack.NewImageServiceV2(provider, gophercloud.EndpointOpts{
		Region: "eu-ch",
	})

	if err != nil {
		fmt.Println("Error getting client.", err.Error())
		return nil, errors.New(genericOTCAPIError)
	}

	return client, nil
}

func getBlockStorageClient() (*gophercloud.ServiceClient, error) {
	provider, err := getProvider()
	if err != nil {
		fmt.Println("Error while authenticating.", err.Error())
		return nil, errors.New(genericOTCAPIError)
	}

	client, err := openstack.NewBlockStorageV3(provider, gophercloud.EndpointOpts{
		Region: "eu-ch",
	})

	if err != nil {
		fmt.Println("Error getting client.", err.Error())
		return nil, errors.New(genericOTCAPIError)
	}

	return client, nil
}
