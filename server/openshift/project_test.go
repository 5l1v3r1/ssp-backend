package openshift

import (
	"fmt"
	"testing"

	"github.com/Jeffail/gabs/v2"
	"github.com/SchweizerischeBundesbahnen/ssp-backend/server/config"
)

func TestProjectFilter(t *testing.T) {
	projects, err := gabs.ParseJSON([]byte(`[
		{
			"metadata": {
				"annotations": {
					"openshift.io/kontierung-element": "5678",
					"openshift.io/MEGAID": "1234"
				}
			}
		},
		{
			"metadata": {
				"annotations": {
					"openshift.io/kontierung-element": "5678",
					"openshift.io/MEGAID": "8080"
				}
			}
		},
		{
			"metadata": {
				"annotations": {
					"openshift.io/kontierung-element": "8888",
					"openshift.io/MEGAID": ""
				}
			}
		},
		{
			"metadata": {
				"annotations": {
					"openshift.io/kontierung-element": "8888"
				}
			}
		},
		{
			"metadata": {
				"annotations": {
					"openshift.io/kontierung-element": "5678",
					"openshift.io/MEGAID": "1235"
				}
			}
		},
		{
			"metadata": {
				"annotations": {
					"openshift.io/kontierung-element": "",
					"openshift.io/MEGAID": "9999"
				}
			}
		},
		{
			"metadata": {
				"annotations": {
					"openshift.io/MEGAID": "9999"
				}
			}
		},
		{
			"metadata": {
				"annotations": {
					"openshift.io/kontierung-element": "5050",
					"openshift.io/MEGAID": "5678"
				}
			}
		}
	]`))
	if err != nil {
		t.Error("Invalid JSON!")
		return

	}
	var searchsets = []struct {
		inAccountingNumber string
		inMegaId           string
		numberOfResults    int
	}{
		{"1234", "5678", 0},
		{"5678", "1234", 1},
		{"8888", "", 2},
		{"", "9999", 2},
	}

	for _, set := range searchsets {
		t.Run(fmt.Sprintf("accountingNumber=%s megaId=%s", set.inAccountingNumber, set.inMegaId), func(t *testing.T) {
			filteredProjects := filterProjects(projects, set.inAccountingNumber, set.inMegaId)
			if len(filteredProjects.Children()) != set.numberOfResults {
				t.Errorf("ERROR: number of filtered projects should be %v, but is: %v", set.numberOfResults, len(filteredProjects.Children()))
			}
		})
	}
}

func TestValidateProjectPermissions(t *testing.T) {
	// testing empty Cluster ID
	err := validateProjectPermissions("", "faccount", "project")
	if err.Error() != "Cluster must be provided" {
		t.Error("ERROR! function \"validateProjectPermissions\" not throwing the right error on empty Cluster!")
	}
	// testing empty Project name
	err = validateProjectPermissions("clusterId", "faccount", "")
	if err.Error() != "Project name must be provided" {
		t.Error("ERROR! function \"validateProjectPermissions\" not throwing the right error on empty Project!")
	}
	// "mocking" the configuration for the next test
	config.Init("bla")
	config.Config().Set("openshift_functional_account", "faccount")
	// testing the functional account
	newErr := validateProjectPermissions("cluster", "faccount", "project")
	if newErr != nil {
		t.Error("ERROR! function \"validateProjectPermissions\" not allowing the functional account")
	}
}
