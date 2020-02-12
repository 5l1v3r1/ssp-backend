package openshift

import "testing"
import "fmt"
import "strings"

func TestWeightedAverage(t *testing.T) {
	tests := []struct {
		input  []float64
		output float64
	}{
		{[]float64{0.9, 0.2, 0.4}, 0.84},
	}
	for _, tt := range tests {
		t.Run(fString(tt.input), func(t *testing.T) {
			avg := weightedAverage(tt.input...)
			if avg != tt.output {
				t.Fatalf("Weighted average is incorrect. got: %v want: %v", avg, tt.output)
			}
		})
	}
}

func fString(a []float64) string {
	s := fmt.Sprintf("%v", a)
	return strings.ReplaceAll(s, " ", ",")
}
