package monitor

import (
	"kami/internal/uptime"
	"testing"
)

func TestSamplesRateAProjectByItsWorstService(t *testing.T) {
	s := &Status{Resources: []Resource{
		{Name: "noctis", Group: "noctis", State: StateUp},
		{Name: "noctis-postgres", Group: "noctis", State: StateUnresponsive},
		{Name: "augur", Group: "augur", State: StateDegraded},
		{Name: "augur-cleanup", Group: "augur", Type: "Job", State: StateFailed},
		{Name: "luna", Group: "luna", State: StateOff},
	}}

	got := make(map[string]uptime.Level)
	for _, sample := range s.Samples() {
		got[sample.Key] = sample.Level
	}
	want := map[string]uptime.Level{
		"noctis":                  uptime.Up,
		"noctis-postgres":         uptime.Down,
		uptime.GroupKey("noctis"): uptime.Down,
		"augur":                   uptime.Degraded,
		"augur-cleanup":           uptime.Down,
		uptime.GroupKey("augur"):  uptime.Degraded, // the failed job run doesn't count
	}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v (scaled-to-zero luna left out)", got, want)
	}
	for key, level := range want {
		if got[key] != level {
			t.Errorf("%s: got %v, want %v", key, got[key], level)
		}
	}
}
