package otc

import (
	"github.com/SchweizerischeBundesbahnen/ssp-backend/server/config"
	"github.com/gophercloud/gophercloud"
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
