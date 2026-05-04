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

// Resolve POSTs a ResolveRequest to /api/v1/plans/{name}/resolve on the
// plan-server. Empty Resolutions short-circuits before any HTTP call.
func (r *RemotePlanOps) Resolve(ctx context.Context, req ResolveRequest) error {
	if req.Plan == "" {
		return fmt.Errorf("plan name is required")
	}
	if len(req.Resolutions) == 0 {
		return nil
	}
	endpoint := fmt.Sprintf("%s/api/v1/plans/%s/resolve", r.base, url.PathEscape(req.Plan))
	body, err := json.Marshal(req)
	if err != nil {
		return fmt.Errorf("marshal resolve request: %w", err)
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("build resolve request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if r.version != "" {
		httpReq.Header.Set("X-Fil-Version", r.version)
	}

	resp, err := r.httpClient.Do(httpReq)
	if err != nil {
		return fmt.Errorf("resolve request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusNoContent {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("resolve failed: status %d: %s", resp.StatusCode, strings.TrimSpace(string(b)))
	}
	return nil
}
