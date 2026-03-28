package api

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/fatih/color"
)

const versionHeader = "X-Fil-Version"

// PlanSummary represents a plan as returned by the plan server list endpoint.
type PlanSummary struct {
	Name        string `json:"name"`
	Projects    int    `json:"projects"`
	PlatesTodo  int    `json:"plates_todo"`
	HasAssembly bool   `json:"has_assembly"`
}

// PlanServerClient communicates with the fil plan storage server.
type PlanServerClient struct {
	base       string
	version    string
	httpClient http.Client
	warnOnce   sync.Once
}

// NewPlanServerClient creates a new client for the plan server API.
func NewPlanServerClient(base string, version string, tlsSkipVerify bool) *PlanServerClient {
	client := http.Client{
		Timeout: 10 * time.Second,
	}
	if tlsSkipVerify {
		client.Transport = &http.Transport{
			TLSClientConfig: &tls.Config{
				InsecureSkipVerify: true, //nolint:gosec // user-configured for local CA trust issues
			},
		}
	}
	return &PlanServerClient{
		base:       strings.TrimRight(base, "/"),
		version:    version,
		httpClient: client,
	}
}

// setVersionHeader adds the client version header to an outgoing request.
func (c *PlanServerClient) setVersionHeader(req *http.Request) {
	if c.version != "" {
		req.Header.Set(versionHeader, c.version)
	}
}

// checkVersionMismatch reads the server's version header and warns once if it differs.
func (c *PlanServerClient) checkVersionMismatch(resp *http.Response) {
	serverVersion := resp.Header.Get(versionHeader)
	if serverVersion == "" || serverVersion == c.version {
		return
	}
	c.warnOnce.Do(func() {
		warn := color.New(color.FgRed, color.Bold).FprintfFunc()
		if compareSemver(serverVersion, c.version) > 0 {
			warn(os.Stderr, "Note: server is running fil %s (you have %s). Consider updating your client.\n", serverVersion, c.version)
		} else {
			warn(os.Stderr, "Note: server is running fil %s (you have %s). The server may need updating.\n", serverVersion, c.version)
		}
	})
}

// do executes a request, injecting the version header and checking for version mismatch on the response.
func (c *PlanServerClient) do(req *http.Request) (*http.Response, error) {
	c.setVersionHeader(req)
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	c.checkVersionMismatch(resp)
	return resp, nil
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

	resp, err := c.do(req)
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

	resp, err := c.do(req)
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

	resp, err := c.do(req)
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

	resp, err := c.do(req)
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

// UnarchivePlan moves a plan from the archive back to active on the server.
func (c *PlanServerClient) UnarchivePlan(ctx context.Context, name string) error {
	return c.planAction(ctx, name, "unarchive")
}

// GetSharedConfig fetches the shared configuration from the server.
func (c *PlanServerClient) GetSharedConfig(ctx context.Context) ([]byte, error) {
	endpoint := c.base + "/api/v1/config"

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
		return nil, fmt.Errorf("plan server error: status %d: %s", resp.StatusCode, strings.TrimSpace(string(b)))
	}

	return io.ReadAll(resp.Body)
}

// PutSharedConfig uploads a shared configuration to the server.
func (c *PlanServerClient) PutSharedConfig(ctx context.Context, data []byte) error {
	endpoint := c.base + "/api/v1/config"

	req, err := http.NewRequestWithContext(ctx, http.MethodPut, endpoint, bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.do(req)
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

// compareSemver compares two version strings numerically.
// Returns >0 if a > b, <0 if a < b, 0 if equal.
// Strips a leading "v" prefix and compares up to three numeric parts (major.minor.patch).
// Non-numeric parts are treated as 0.
func compareSemver(a, b string) int {
	parse := func(v string) [3]int {
		v = strings.TrimPrefix(v, "v")
		parts := strings.SplitN(v, ".", 3)
		var nums [3]int
		for i := 0; i < len(parts) && i < 3; i++ {
			nums[i], _ = strconv.Atoi(parts[i])
		}
		return nums
	}
	av, bv := parse(a), parse(b)
	for i := 0; i < 3; i++ {
		if av[i] != bv[i] {
			return av[i] - bv[i]
		}
	}
	return 0
}

// PutAssembly uploads a PDF assembly document for the given plan.
// Returns the server-side filename that should be stored in the plan YAML's assembly field.
func (c *PlanServerClient) PutAssembly(ctx context.Context, planName string, pdfData []byte) (string, error) {
	endpoint := fmt.Sprintf("%s/api/v1/plans/%s/assembly", c.base, planName)

	req, err := http.NewRequestWithContext(ctx, http.MethodPut, endpoint, bytes.NewReader(pdfData))
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/pdf")

	resp, err := c.do(req)
	if err != nil {
		return "", fmt.Errorf("plan server request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusCreated {
		b, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("plan server error: status %d: %s", resp.StatusCode, strings.TrimSpace(string(b)))
	}

	var result struct {
		Filename string `json:"filename"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("failed to decode response: %w", err)
	}

	return result.Filename, nil
}

// GetAssembly downloads the assembly PDF for a plan. Returns the PDF bytes and the filename from Content-Disposition.
func (c *PlanServerClient) GetAssembly(ctx context.Context, planName string) ([]byte, string, error) {
	endpoint := fmt.Sprintf("%s/api/v1/plans/%s/assembly", c.base, planName)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, "", fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := c.do(req)
	if err != nil {
		return nil, "", fmt.Errorf("plan server request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return nil, "", fmt.Errorf("plan server error: status %d: %s", resp.StatusCode, strings.TrimSpace(string(b)))
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, "", fmt.Errorf("failed to read response body: %w", err)
	}

	// Extract filename from Content-Disposition header
	filename := ""
	if cd := resp.Header.Get("Content-Disposition"); cd != "" {
		// Parse "attachment; filename=\"foo.pdf\""
		for _, part := range strings.Split(cd, ";") {
			part = strings.TrimSpace(part)
			if strings.HasPrefix(part, "filename=") {
				filename = strings.Trim(strings.TrimPrefix(part, "filename="), "\"")
			}
		}
	}

	return data, filename, nil
}

// DeleteAssembly removes the assembly PDF for a plan.
func (c *PlanServerClient) DeleteAssembly(ctx context.Context, planName string) error {
	endpoint := fmt.Sprintf("%s/api/v1/plans/%s/assembly", c.base, planName)

	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, endpoint, nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := c.do(req)
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

// CleanAssembliesResult is the response from the clean-assemblies endpoint.
type CleanAssembliesResult struct {
	Orphans []string `json:"orphans"`
	DryRun  bool     `json:"dry_run"`
}

func (c *PlanServerClient) CleanAssemblies(ctx context.Context, dryRun bool) (*CleanAssembliesResult, error) {
	endpoint := fmt.Sprintf("%s/api/v1/plans/clean-assemblies?dry_run=%v", c.base, dryRun)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, nil)
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
		return nil, fmt.Errorf("plan server error: status %d: %s", resp.StatusCode, strings.TrimSpace(string(b)))
	}

	var result CleanAssembliesResult
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return &result, nil
}

func (c *PlanServerClient) planAction(ctx context.Context, name, action string) error {
	endpoint := fmt.Sprintf("%s/api/v1/plans/%s/%s", c.base, name, action)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := c.do(req)
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
