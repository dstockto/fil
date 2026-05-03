package plan

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

// Complete POSTs the CompleteRequest to /api/v1/plans/{name}/complete on the
// plan-server, which runs its own LocalPlanOps under the hood.
func (r *RemotePlanOps) Complete(ctx context.Context, req CompleteRequest) (CompleteResult, error) {
	if req.Plan == "" {
		return CompleteResult{}, fmt.Errorf("plan name is required")
	}
	endpoint := fmt.Sprintf("%s/api/v1/plans/%s/complete", r.base, url.PathEscape(req.Plan))
	body, err := json.Marshal(req)
	if err != nil {
		return CompleteResult{}, fmt.Errorf("marshal complete request: %w", err)
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return CompleteResult{}, fmt.Errorf("build complete request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if r.version != "" {
		httpReq.Header.Set("X-Fil-Version", r.version)
	}

	resp, err := r.httpClient.Do(httpReq)
	if err != nil {
		return CompleteResult{}, fmt.Errorf("complete request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent {
		b, _ := io.ReadAll(resp.Body)
		return CompleteResult{}, fmt.Errorf("complete failed: status %d: %s", resp.StatusCode, strings.TrimSpace(string(b)))
	}
	if resp.StatusCode == http.StatusNoContent {
		return CompleteResult{}, nil
	}
	var result CompleteResult
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		// Body may be empty on server versions that always 204 — not fatal.
		return CompleteResult{}, nil
	}
	return result, nil
}
