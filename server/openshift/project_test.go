package openshift

import (
	"testing"

	"github.com/Jeffail/gabs/v2"
)

func TestProjectFilter(t *testing.T) {
	projects, _ := gabs.ParseJSON([]byte(`[
		{
			"metadata": {
				"annotations": {
					"openshift.io/MEGAID": "1234",
					"openshift.io/kontierung-element": "5678"
				}
			}
		},
		{
			"metadata": {
				"annotations": {
					"openshift.io/MEGAID": "5678",
					"openshift.io/kontierung-element": "1234"
				}
			}
		}
	]`))
	filteredProjects := filterProjects(projects, "1234", "5678")
	if len(filteredProjects.Children()) != 1 {
		t.Errorf("ERROR: number of filtered projects should be 1, but is: %v", len(filteredProjects.Children()))
	}
}
