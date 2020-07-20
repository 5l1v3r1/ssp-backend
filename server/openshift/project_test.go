package openshift

import (
	"fmt"
	"testing"

	"github.com/Jeffail/gabs/v2"
	"github.com/SchweizerischeBundesbahnen/ssp-backend/server/config"
)

func TestFilterProjects(t *testing.T) {
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

func TestValidateNewProject(t *testing.T) {

	// testing empty project
	err := validateNewProject("", "billing", true)
	if err.Error() != "Project name has to be provided" {
		t.Error("ERROR! function \"validateNewProject\" not throwing the right error on empty Project!")
	}
	// testing empty accounting number (and not a testing project)
	err = validateNewProject("project", "", false)
	if err.Error() != "Accounting number must be provided" {
		t.Error("ERROR! function \"validateNewProject\" not throwing the right error on empty Accounting Number!")
	}
	// testing empty accounting number, but for a testing project
	err = validateNewProject("project", "", true)
	if err != nil {
		t.Error("ERROR! function \"validateNewProject\" not skipping Accounting Number validation on a test project!")
	}
	// testing when none of the inputs is empty
	err = validateNewProject("project", "billing", false)
	if err != nil {
		t.Error("ERROR! function \"validateNewProject\" still returning error on non-empty project + accounting number!")
	}
}

func TestValidateAdminAccess(t *testing.T) {
	err := validateAdminAccess("", "user", "project")
	if err.Error() != "Cluster must be provided" {
		t.Error("ERROR! function \"validateAdminAccess\" not throwing the right error on empty Cluster!")
	}
	err = validateAdminAccess("cluster", "user", "")
	if err.Error() != "Project name must be provided" {
		t.Error("ERROR! function \"validateAdminAccess\" not throwing the right error on empty Project!")
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
	// (testing the functional account when it's not set requires mocking
	// of the Openshift API. for the moment won't be done)
	// setting the functional account (a.k.a. "additional project admin account")
	config.Config().Set("openshift_additional_project_admin_account", "faccount")
	// testing the functional account (when set)
	err = validateProjectPermissions("cluster", "faccount", "project")
	if err != nil {
		t.Error("ERROR! function \"validateProjectPermissions\" not checking the functional account")
	}
}
