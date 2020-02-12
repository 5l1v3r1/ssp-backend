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
		{[]float64{0.6, 0.6, 0.6}, 0.6},
		{[]float64{0.5, 0.6, 0.7}, 0.65},
		{[]float64{0.2, 0.3, 0.4}, 0.31},
		{[]float64{0.2, 0.3, 0.8}, 0.69},
		{[]float64{0.6, 1, 0.8}, 1},
		{[]float64{0, 0, 1}, 1},
		{[]float64{0, 0.8, 0}, 0.64},
		{[]float64{0.9, 0.8, 0}, 0.89},
		{[]float64{0.3, 0.2}, 0.23},
		{[]float64{0.7, 0.8, 0.9, 0.9}, 0.89},
		{[]float64{0.1, 0.1, 0.1, 0.4}, 0.22},
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
