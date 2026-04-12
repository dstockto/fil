package api

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// CheckStatus represents the pass/warn/fail state of a single health check.
type CheckStatus string

const (
	StatusOK   CheckStatus = "ok"
	StatusWarn CheckStatus = "warn"
	StatusFail CheckStatus = "fail"
	StatusSkip CheckStatus = "skip"
)

// Check is a single health check result. Both client and server produce Checks.
// Detail is free-form per-check data surfaced in --json mode.
type Check struct {
	Group      string          `json:"group"`
	Name       string          `json:"name"`
	Status     CheckStatus     `json:"status"`
	DurationMs int64           `json:"duration_ms"`
	Message    string          `json:"message,omitempty"`
	Detail     json.RawMessage `json:"detail,omitempty"`
}

// HealthSummary aggregates per-status counts.
type HealthSummary struct {
	OK   int `json:"ok"`
	Warn int `json:"warn"`
	Fail int `json:"fail"`
	Skip int `json:"skip"`
}

// HealthReport is what /api/v1/health returns (and what fil doctor emits in --json).
type HealthReport struct {
	Version       string        `json:"version,omitempty"`
	UptimeSeconds int64         `json:"uptime_seconds,omitempty"`
	CheckedAt     time.Time     `json:"checked_at"`
	Checks        []Check       `json:"checks"`
	Summary       HealthSummary `json:"summary"`
}

// Tally recomputes the summary counts from the checks list.
func (r *HealthReport) Tally() {
	r.Summary = HealthSummary{}
	for _, c := range r.Checks {
		switch c.Status {
		case StatusOK:
			r.Summary.OK++
		case StatusWarn:
			r.Summary.Warn++
		case StatusFail:
			r.Summary.Fail++
		case StatusSkip:
			r.Summary.Skip++
		}
	}
}

// GetHealth fetches the server-side health report from /api/v1/doctor.
// Path is /api/v1/doctor (not /health) to avoid collision with generic
// reverse-proxy health endpoints.
func (c *PlanServerClient) GetHealth(ctx context.Context) (*HealthReport, error) {
	endpoint := c.base + "/api/v1/doctor"

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := c.do(req)
	if err != nil {
		return nil, fmt.Errorf("plan server request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("plan server error: status %d: %s", resp.StatusCode, string(b))
	}

	var report HealthReport
	if err := json.NewDecoder(resp.Body).Decode(&report); err != nil {
		return nil, fmt.Errorf("failed to decode health report: %w", err)
	}
	return &report, nil
}
