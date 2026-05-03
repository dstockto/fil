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

// Stop POSTs a StopRequest to /api/v1/plans/{name}/stop on the plan-server.
func (r *RemotePlanOps) Stop(ctx context.Context, req StopRequest) error {
	if req.Plan == "" {
		return fmt.Errorf("plan name is required")
	}
	endpoint := fmt.Sprintf("%s/api/v1/plans/%s/stop", r.base, url.PathEscape(req.Plan))
	body, err := json.Marshal(req)
	if err != nil {
		return fmt.Errorf("marshal stop request: %w", err)
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("build stop request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if r.version != "" {
		httpReq.Header.Set("X-Fil-Version", r.version)
	}

	resp, err := r.httpClient.Do(httpReq)
	if err != nil {
		return fmt.Errorf("stop request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusNoContent {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("stop failed: status %d: %s", resp.StatusCode, strings.TrimSpace(string(b)))
	}
	return nil
}
