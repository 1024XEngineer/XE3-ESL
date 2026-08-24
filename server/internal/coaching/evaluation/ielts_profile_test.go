package evaluation

import "testing"

func TestIELTSProfileDimensionValidatesHalfBands(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		low  float64
		high float64
		want bool
	}{
		{name: "whole bands", low: 6, high: 7, want: true},
		{name: "half bands", low: 6.5, high: 7.5, want: true},
		{name: "non half low", low: 6.3, high: 7.5, want: false},
		{name: "non half high", low: 6.5, high: 7.3, want: false},
		{name: "below range", low: -0.5, high: 1, want: false},
		{name: "above range", low: 8.5, high: 9.5, want: false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			dimension := IELTSProfileDimension{
				Key: "FLUENCY_COHERENCE", ProvisionalBandLow: test.low,
				ProvisionalBandHigh: test.high, Coverage: 0.5, Confidence: 0.5,
				Observations: []IELTSProfileObservation{},
			}
			if got := dimension.Valid(); got != test.want {
				t.Fatalf("Valid() = %t, want %t", got, test.want)
			}
		})
	}
}
