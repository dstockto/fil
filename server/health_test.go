package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/dstockto/fil/api"
)

func TestRunHealthChecks_FilesystemReportsOK(t *testing.T) {
	s, _ := setupTestServer(t)
	s.Version = "test-version"
	s.StartedAt = time.Now().Add(-90 * time.Second)

	report := s.RunHealthChecks(context.Background())

	if report.Version != "test-version" {
		t.Errorf("expected version in report, got %q", report.Version)
	}
	if report.UptimeSeconds < 85 || report.UptimeSeconds > 120 {
		t.Errorf("expected uptime near 90s, got %d", report.UptimeSeconds)
	}

	got := map[string]api.Check{}
	for _, c := range report.Checks {
		got[c.Group+"/"+c.Name] = c
	}

	// Every configured directory should produce an OK filesystem check.
	for _, name := range []string{"plans_dir", "pause_dir", "archive_dir", "assemblies_dir", "config_dir"} {
		key := "filesystem/" + name
		c, ok := got[key]
		if !ok {
			t.Errorf("missing check %s", key)
			continue
		}
		if c.Status != api.StatusOK {
			t.Errorf("%s: expected ok, got %s (%s)", key, c.Status, c.Message)
		}
	}
}

func TestRunHealthChecks_SpoolmanReachable(t *testing.T) {
	// Spin up a fake Spoolman with an /api/v1/info endpoint.
	spoolman := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/info" {
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]string{"version": "0.99.9"})
	}))
	defer spoolman.Close()

	s, _ := setupTestServer(t)
	s.ApiBase = spoolman.URL

	report := s.RunHealthChecks(context.Background())

	var found *api.Check
	for i := range report.Checks {
		if report.Checks[i].Group == "spoolman" && report.Checks[i].Name == "reachable" {
			found = &report.Checks[i]
			break
		}
	}
	if found == nil {
		t.Fatal("spoolman/reachable check missing")
	}
	if found.Status != api.StatusOK {
		t.Errorf("expected ok, got %s: %s", found.Status, found.Message)
	}
	if found.Message != "version 0.99.9" {
		t.Errorf("unexpected message: %s", found.Message)
	}
}

func TestHandleHealth_ReturnsJSON(t *testing.T) {
	s, _ := setupTestServer(t)
	s.Version = "test"
	s.StartedAt = time.Now()

	srv := httptest.NewServer(s.Routes())
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/api/v1/doctor")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "application/json" {
		t.Errorf("unexpected content type: %s", ct)
	}

	var report api.HealthReport
	if err := json.NewDecoder(resp.Body).Decode(&report); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if report.Version != "test" {
		t.Errorf("expected version 'test', got %q", report.Version)
	}
	if len(report.Checks) == 0 {
		t.Error("expected checks in report, got none")
	}
	// Summary should tally to the same count as checks.
	total := report.Summary.OK + report.Summary.Warn + report.Summary.Fail + report.Summary.Skip
	if total != len(report.Checks) {
		t.Errorf("summary total %d != checks %d", total, len(report.Checks))
	}
}

func TestSpoolmanCheck_UnreachableFails(t *testing.T) {
	s, _ := setupTestServer(t)
	// Use a port where nothing is listening.
	s.ApiBase = "http://127.0.0.1:1"

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	c := s.spoolmanCheck(ctx)

	if c.Status != api.StatusFail {
		t.Errorf("expected fail, got %s: %s", c.Status, c.Message)
	}
}

func TestHumanBytes(t *testing.T) {
	tests := []struct {
		in   int64
		want string
	}{
		{0, "0 B"},
		{1023, "1023 B"},
		{1024, "1.0 KB"},
		{1536, "1.5 KB"},
		{1 << 20, "1.0 MB"},
		{int64(1 << 30), "1.0 GB"},
	}
	for _, tc := range tests {
		if got := humanBytes(tc.in); got != tc.want {
			t.Errorf("humanBytes(%d) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestHumanDuration(t *testing.T) {
	tests := []struct {
		in   time.Duration
		want string
	}{
		{500 * time.Millisecond, "<1s"},
		{2 * time.Second, "2s"},
		{45 * time.Second, "45s"},
		{90 * time.Second, "1m"},
		{30 * time.Minute, "30m"},
		{2 * time.Hour, "2.0h"},
	}
	for _, tc := range tests {
		if got := humanDuration(tc.in); got != tc.want {
			t.Errorf("humanDuration(%v) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
