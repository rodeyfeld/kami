package health

import "testing"

func TestPercentNeverRoundsDowntimeUpTo100(t *testing.T) {
	cases := map[float64]string{1: "100%", 0.99999: "99.99%", 0.9971: "99.71%", 0.5: "50.00%"}
	for p, want := range cases {
		if got := formatPercent(p); got != want {
			t.Errorf("formatPercent(%v) = %s, want %s", p, got, want)
		}
	}
	if whole, frac, _ := splitPercent(0.9997, true); whole != "99" || frac != ".97%" {
		t.Errorf("split: %s %s", whole, frac)
	}
	if whole, frac, _ := splitPercent(1, true); whole != "100" || frac != "%" {
		t.Errorf("split 100: %s %s", whole, frac)
	}
}
