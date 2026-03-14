package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// PlanSummary represents a plan as returned by the plan server list endpoint.
type PlanSummary struct {
	Name       string `json:"name"`
	Projects   int    `json:"projects"`
	PlatesTodo int    `json:"plates_todo"`
}

// PlanServerClient communicates with the fil plan storage server.
type PlanServerClient struct {
	base       string
	httpClient http.Client
}

// NewPlanServerClient creates a new client for the plan server API.
func NewPlanServerClient(base string) *PlanServerClient {
	return &PlanServerClient{
		base: strings.TrimRight(base, "/"),
		httpClient: http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

// ListPlans returns plan summaries. status can be "" (active), "paused", or "archived".
func (c *PlanServerClient) ListPlans(ctx context.Context, status string) ([]PlanSummary, error) {
	endpoint := c.base + "/api/v1/plans"
	if status != "" {
		endpoint += "?status=" + status
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("plan server request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("plan server error: status %d: %s", resp.StatusCode, strings.TrimSpace(string(b)))
	}

	var summaries []PlanSummary
	if err := json.NewDecoder(resp.Body).Decode(&summaries); err != nil {
		return nil, fmt.Errorf("failed to decode plan list: %w", err)
	}

	return summaries, nil
}

// GetPlan fetches the raw YAML content of a plan by name.
// An optional status ("paused", "archived") can be passed to fetch from the
// corresponding directory on the server. Pass "" for active plans.
func (c *PlanServerClient) GetPlan(ctx context.Context, name string, status ...string) ([]byte, error) {
	endpoint := c.base + "/api/v1/plans/" + name
	if len(status) > 0 && status[0] != "" {
		endpoint += "?status=" + status[0]
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("plan server request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("plan server error: status %d: %s", resp.StatusCode, strings.TrimSpace(string(b)))
	}

	return io.ReadAll(resp.Body)
}

// PutPlan creates or updates a plan on the server.
func (c *PlanServerClient) PutPlan(ctx context.Context, name string, yamlData []byte) error {
	endpoint := c.base + "/api/v1/plans/" + name

	req, err := http.NewRequestWithContext(ctx, http.MethodPut, endpoint, bytes.NewReader(yamlData))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("plan server request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusNoContent {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("plan server error: status %d: %s", resp.StatusCode, strings.TrimSpace(string(b)))
	}

	return nil
}

// DeletePlan removes a plan from the server.
func (c *PlanServerClient) DeletePlan(ctx context.Context, name string) error {
	endpoint := c.base + "/api/v1/plans/" + name

	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, endpoint, nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("plan server request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusNoContent && resp.StatusCode != http.StatusNotFound {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("plan server error: status %d: %s", resp.StatusCode, strings.TrimSpace(string(b)))
	}

	return nil
}

// PausePlan moves a plan to the paused state on the server.
func (c *PlanServerClient) PausePlan(ctx context.Context, name string) error {
	return c.planAction(ctx, name, "pause")
}

// ResumePlan moves a plan from paused back to active on the server.
func (c *PlanServerClient) ResumePlan(ctx context.Context, name string) error {
	return c.planAction(ctx, name, "resume")
}

// ArchivePlan moves a plan to the archive on the server.
func (c *PlanServerClient) ArchivePlan(ctx context.Context, name string) error {
	return c.planAction(ctx, name, "archive")
}

func (c *PlanServerClient) planAction(ctx context.Context, name, action string) error {
	endpoint := fmt.Sprintf("%s/api/v1/plans/%s/%s", c.base, name, action)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("plan server request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusNoContent {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("plan server error: status %d: %s", resp.StatusCode, strings.TrimSpace(string(b)))
	}

	return nil
}
