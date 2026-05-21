package api

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestCompareSemver(t *testing.T) {
	tests := []struct {
		a, b string
		want int // >0 means a>b, <0 means a<b, 0 means equal
	}{
		{"v1.0.0", "v1.0.0", 0},
		{"v1.2.3", "v1.2.3", 0},
		{"v2.0.0", "v1.0.0", 1},
		{"v1.0.0", "v2.0.0", -1},
		{"v10.0.0", "v2.0.0", 1},  // the bug this fixes
		{"v2.0.0", "v10.0.0", -1}, // the bug this fixes
		{"v1.10.0", "v1.2.0", 1},
		{"v1.2.0", "v1.10.0", -1},
		{"v1.0.10", "v1.0.2", 1},
		{"v1.0.2", "v1.0.10", -1},
		{"v1.2.0", "v1.1.9", 1},
		{"1.2.3", "1.2.3", 0},  // no v prefix
		{"v1.2.3", "1.2.3", 0}, // mixed prefix
		{"v1.0", "v1.0.0", 0},  // missing patch
		{"v1", "v1.0.0", 0},    // major only
		{"dev", "v1.0.0", -1},  // non-numeric treated as 0
	}

	for _, tt := range tests {
		got := compareSemver(tt.a, tt.b)
		// Normalize to sign
		switch {
		case tt.want > 0 && got <= 0:
			t.Errorf("compareSemver(%q, %q) = %d, want >0", tt.a, tt.b, got)
		case tt.want < 0 && got >= 0:
			t.Errorf("compareSemver(%q, %q) = %d, want <0", tt.a, tt.b, got)
		case tt.want == 0 && got != 0:
			t.Errorf("compareSemver(%q, %q) = %d, want 0", tt.a, tt.b, got)
		}
	}
}

func TestPostScanEvent_Success(t *testing.T) {
	var got ScanEvent
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if r.URL.Path != "/api/fil/scan-history" {
			t.Errorf("expected /api/fil/scan-history, got %s", r.URL.Path)
		}
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &got)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	c := NewPlanServerClient(srv.URL, "test", false)
	ev := ScanEvent{
		Timestamp:  time.Now().UTC(),
		SpoolID:    127,
		FilamentID: 42,
		ScannedHex: "#ead9d4",
		Action:     "commit",
	}
	if err := c.PostScanEvent(context.Background(), ev); err != nil {
		t.Fatalf("PostScanEvent: %v", err)
	}
	if got.Action != "commit" || got.SpoolID != 127 {
		t.Errorf("server received unexpected event: %+v", got)
	}
}

func TestPostScanEvent_ServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer srv.Close()

	c := NewPlanServerClient(srv.URL, "test", false)
	err := c.PostScanEvent(context.Background(), ScanEvent{Action: "commit"})
	if err == nil {
		t.Fatal("expected error on 500, got nil")
	}
}

// TestPlanServerClientUsesFilPrefix is the regression guard for the
// /api/v1 → /api/fil migration. The server has dual-routed both prefixes
// since PR-1 and Caddy now wildcards /api/fil/* to the plan server, so
// the client side must call /api/fil. If a future change accidentally
// reintroduces /api/v1 in a client URL, this test catches it.
func TestPlanServerClientUsesFilPrefix(t *testing.T) {
	var hits []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits = append(hits, r.URL.Path)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	c := NewPlanServerClient(srv.URL, "test", false)
	if err := c.PostScanEvent(context.Background(), ScanEvent{Action: "commit"}); err != nil {
		t.Fatalf("PostScanEvent: %v", err)
	}

	if len(hits) == 0 {
		t.Fatal("expected at least one hit, got none")
	}
	for _, p := range hits {
		if !strings.HasPrefix(p, "/api/fil/") {
			t.Errorf("client hit %q; expected /api/fil/ prefix", p)
		}
	}
}
