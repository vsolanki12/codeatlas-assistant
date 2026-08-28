package metrics

import "testing"

func TestMeasure(t *testing.T) {
	got := Measure("abcdé")
	if got.Bytes != 6 || got.Characters != 5 || got.EstimatedTokens != 2 {
		t.Fatalf("Measure() = %+v, want bytes=6 characters=5 estimatedTokens=2", got)
	}
}

func TestReductionPercent(t *testing.T) {
	if got := ReductionPercent(100, 25); got != 75 {
		t.Fatalf("ReductionPercent() = %v, want 75", got)
	}
	if got := ReductionPercent(0, 0); got != 0 {
		t.Fatalf("ReductionPercent(0, 0) = %v, want 0", got)
	}
}
