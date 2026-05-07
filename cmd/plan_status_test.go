package cmd

import (
	"strings"
	"testing"
	"time"
)

// TestFormatTimeInfoRendersInLocalZone is a regression test for the bug where
// plan YAML files mix UTC ("Z") and local-offset RFC3339 stamps for
// plate.started_at. formatTimeInfo must render both in the user's local zone
// so the clock value matches wall-clock time regardless of how the writer
// stored it.
func TestFormatTimeInfoRendersInLocalZone(t *testing.T) {
	loc, err := time.LoadLocation("America/Denver")
	if err != nil {
		t.Skipf("test needs America/Denver tzdata: %v", err)
	}

	// 2026-05-07 11:27:13 UTC = 2026-05-07 05:27:13 MDT (UTC-6 in May).
	utcStamp := "2026-05-07T11:27:13Z"
	localStamp := "2026-05-07T05:27:13-06:00"

	prevLocal := time.Local
	time.Local = loc
	defer func() { time.Local = prevLocal }()

	got := formatTimeInfo(utcStamp, "")
	if !strings.Contains(got, "5:27am") {
		t.Errorf("UTC stamp displayed wrong: got %q, want contains 5:27am (local)", got)
	}

	got = formatTimeInfo(localStamp, "")
	if !strings.Contains(got, "5:27am") {
		t.Errorf("local-offset stamp displayed wrong: got %q, want contains 5:27am (local)", got)
	}
}

// TestFormatTimeInfoETAUsesLocalZone confirms that the "done ~X" ETA also
// renders in local time. A UTC start + 2h duration must show the local-zone
// finish time, not the UTC clock value.
func TestFormatTimeInfoETAUsesLocalZone(t *testing.T) {
	loc, err := time.LoadLocation("America/Denver")
	if err != nil {
		t.Skipf("test needs America/Denver tzdata: %v", err)
	}
	prevLocal := time.Local
	time.Local = loc
	defer func() { time.Local = prevLocal }()

	// UTC start at 11:00, +2h = 13:00 UTC = 7:00 MDT.
	got := formatTimeInfo("2026-05-07T11:00:00Z", "2h")
	if !strings.Contains(got, "started 5:00am") {
		t.Errorf("got %q, want contains 'started 5:00am'", got)
	}
	if !strings.Contains(got, "done ~7:00am") {
		t.Errorf("got %q, want contains 'done ~7:00am'", got)
	}
}
