package uptime

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

func openTest(t *testing.T, now *time.Time) *Store {
	t.Helper()
	s, err := Open(filepath.Join(t.TempDir(), "kami.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	s.now = func() time.Time { return *now }
	return s
}

func TestHistoryAddsUpSamplesIntoAlignedDays(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 10, 9, 23, 59, 0, 0, time.UTC)
	s := openTest(t, &now)

	if err := s.Record(ctx, []Sample{{"augur", Up}, {"noctis", Down}}, 60); err != nil {
		t.Fatal(err)
	}
	now = now.Add(2 * time.Minute) // past midnight: a new bucket
	for _, level := range []Level{Up, Degraded, Down} {
		if err := s.Record(ctx, []Sample{{"augur", level}}, 60); err != nil {
			t.Fatal(err)
		}
	}

	h, err := s.History(ctx, 3)
	if err != nil {
		t.Fatal(err)
	}
	augur := h["augur"]
	if len(augur) != 3 || !augur[2].Date.Equal(time.Date(2026, 10, 10, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("days not aligned to today: %+v", augur)
	}
	if augur[0].Total() != 0 || augur[1] != (Day{Date: augur[1].Date, Up: 60}) {
		t.Fatalf("yesterday: %+v", augur[:2])
	}
	if got := augur[2]; got.Up != 60 || got.Degraded != 60 || got.Down != 60 {
		t.Fatalf("today: %+v", got)
	}
	// Degraded counts as up; only down time comes off
	if u := augur[2].Uptime(); u < 0.66 || u > 0.67 {
		t.Fatalf("uptime %v", u)
	}
	if h["noctis"][1].Down != 60 || h["noctis"][2].Total() != 0 {
		t.Fatalf("noctis: %+v", h["noctis"])
	}

	since, err := s.Since(ctx)
	if err != nil || !since.Equal(time.Date(2026, 10, 9, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("since %v %v", since, err)
	}
}

func TestRecordForgetsDaysPastTheRetention(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	s := openTest(t, &now)

	if err := s.Record(ctx, []Sample{{"augur", Up}}, 60); err != nil {
		t.Fatal(err)
	}
	now = now.AddDate(0, 0, keepDays+1)
	if err := s.Record(ctx, []Sample{{"augur", Up}}, 60); err != nil {
		t.Fatal(err)
	}
	since, err := s.Since(ctx)
	if err != nil || !since.Equal(now.Truncate(24*time.Hour)) {
		t.Fatalf("old day kept: since %v %v", since, err)
	}
}
