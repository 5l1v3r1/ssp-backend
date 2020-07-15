package openshift

import (
	"fmt"
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
	var searchsets = []struct {
		inAccountingNumber string
		inMegaId           string
		numberOfResults    int
	}{
		{"1234", "5678", 1},
		{"5678", "1234", 1},
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
