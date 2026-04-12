package cmd

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/dstockto/fil/api"
)

func TestDiffJSON_NoChanges(t *testing.T) {
	a := []byte(`{"api_base":"http://x","low_ignore":["foo","bar"]}`)
	b := []byte(`{"api_base":"http://x","low_ignore":["foo","bar"]}`)

	diffs, err := diffJSON(a, b)
	if err != nil {
		t.Fatal(err)
	}
	if len(diffs) != 0 {
		t.Errorf("expected 0 diffs, got %d: %+v", len(diffs), diffs)
	}
}

func TestDiffJSON_ScalarChange(t *testing.T) {
	a := []byte(`{"api_base":"http://old"}`)
	b := []byte(`{"api_base":"http://new"}`)

	diffs, err := diffJSON(a, b)
	if err != nil {
		t.Fatal(err)
	}
	if len(diffs) != 1 {
		t.Fatalf("expected 1 diff, got %d: %+v", len(diffs), diffs)
	}
	if diffs[0].Path != "api_base" {
		t.Errorf("unexpected path: %s", diffs[0].Path)
	}
	if diffs[0].Local != "http://old" || diffs[0].Server != "http://new" {
		t.Errorf("unexpected diff values: %+v", diffs[0])
	}
}

func TestDiffJSON_NestedAddition(t *testing.T) {
	a := []byte(`{"printers":{"Bambu X1C":{"ip":"192.168.1.190"}}}`)
	b := []byte(`{"printers":{"Bambu X1C":{"ip":"192.168.1.190","serial":"NEW"}}}`)

	diffs, err := diffJSON(a, b)
	if err != nil {
		t.Fatal(err)
	}
	if len(diffs) != 1 {
		t.Fatalf("expected 1 diff, got %d: %+v", len(diffs), diffs)
	}
	if diffs[0].Path != "printers.Bambu X1C.serial" {
		t.Errorf("unexpected path: %s", diffs[0].Path)
	}
	if diffs[0].Local != nil {
		t.Errorf("expected local=nil, got %v", diffs[0].Local)
	}
	if diffs[0].Server != "NEW" {
		t.Errorf("expected server='NEW', got %v", diffs[0].Server)
	}
}

func TestDiffJSON_NestedRemoval(t *testing.T) {
	a := []byte(`{"notifications":{"quiet_start":"22:00","quiet_end":"07:00"}}`)
	b := []byte(`{"notifications":{"quiet_start":"22:00"}}`)

	diffs, err := diffJSON(a, b)
	if err != nil {
		t.Fatal(err)
	}
	if len(diffs) != 1 {
		t.Fatalf("expected 1 diff, got %d", len(diffs))
	}
	if diffs[0].Path != "notifications.quiet_end" {
		t.Errorf("unexpected path: %s", diffs[0].Path)
	}
	if diffs[0].Server != nil {
		t.Errorf("expected server=nil, got %v", diffs[0].Server)
	}
}

func TestDiffJSON_ArrayLengthDifference(t *testing.T) {
	a := []byte(`{"low_ignore":["foo","bar"]}`)
	b := []byte(`{"low_ignore":["foo","bar","baz"]}`)

	diffs, err := diffJSON(a, b)
	if err != nil {
		t.Fatal(err)
	}
	if len(diffs) != 1 {
		t.Fatalf("expected 1 diff, got %d: %+v", len(diffs), diffs)
	}
	if diffs[0].Path != "low_ignore" {
		t.Errorf("unexpected path: %s", diffs[0].Path)
	}
}

func TestDiffJSON_ArrayElementChange(t *testing.T) {
	a := []byte(`{"low_ignore":["foo","bar"]}`)
	b := []byte(`{"low_ignore":["foo","baz"]}`)

	diffs, err := diffJSON(a, b)
	if err != nil {
		t.Fatal(err)
	}
	if len(diffs) != 1 {
		t.Fatalf("expected 1 diff, got %d: %+v", len(diffs), diffs)
	}
	if diffs[0].Path != "low_ignore[1]" {
		t.Errorf("unexpected path: %s", diffs[0].Path)
	}
}

func TestDiffJSON_NumericAndMapComparison(t *testing.T) {
	a := []byte(`{"low_thresholds":{"PLA":100,"PETG":150}}`)
	b := []byte(`{"low_thresholds":{"PLA":100,"PETG":200}}`)

	diffs, err := diffJSON(a, b)
	if err != nil {
		t.Fatal(err)
	}
	if len(diffs) != 1 {
		t.Fatalf("expected 1 diff, got %d: %+v", len(diffs), diffs)
	}
	if diffs[0].Path != "low_thresholds.PETG" {
		t.Errorf("unexpected path: %s", diffs[0].Path)
	}
}

func TestParseSkipList(t *testing.T) {
	tests := []struct {
		in   string
		want []string
	}{
		{"", nil},
		{"printers", []string{"printers"}},
		{"printers,notifications", []string{"printers", "notifications"}},
		{" printers , Notifications ", []string{"printers", "notifications"}},
	}
	for _, tc := range tests {
		got := parseSkipList(tc.in)
		if len(got) != len(tc.want) {
			t.Errorf("parseSkipList(%q) size = %d, want %d", tc.in, len(got), len(tc.want))
			continue
		}
		for _, k := range tc.want {
			if !got[k] {
				t.Errorf("parseSkipList(%q) missing %q", tc.in, k)
			}
		}
	}
}

func TestWriteJSON_MatchesReport(t *testing.T) {
	report := &api.HealthReport{
		Version: "test",
		Checks: []api.Check{
			{Group: "config", Name: "api_base", Status: api.StatusOK, Message: "set"},
		},
		Summary: api.HealthSummary{OK: 1},
	}

	var buf bytes.Buffer
	if err := writeJSON(&buf, report); err != nil {
		t.Fatal(err)
	}

	var decoded api.HealthReport
	if err := json.Unmarshal(buf.Bytes(), &decoded); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if decoded.Version != "test" || len(decoded.Checks) != 1 {
		t.Errorf("roundtrip mismatch: %+v", decoded)
	}
}

func TestWriteHuman_IncludesStatusLabels(t *testing.T) {
	report := &api.HealthReport{
		Checks: []api.Check{
			{Group: "config", Name: "api_base", Status: api.StatusOK, Message: "https://x"},
			{Group: "config", Name: "plans_server", Status: api.StatusWarn, Message: "not set"},
			{Group: "spoolman", Name: "reachable", Status: api.StatusFail, Message: "down"},
		},
		Summary: api.HealthSummary{OK: 1, Warn: 1, Fail: 1},
	}

	var buf bytes.Buffer
	writeHuman(&buf, report, true)
	out := buf.String()

	for _, want := range []string{"api_base", "plans_server", "reachable", "Summary"} {
		if !bytes.Contains([]byte(out), []byte(want)) {
			t.Errorf("human output missing %q\n---\n%s", want, out)
		}
	}
}
