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

// Next POSTs a NextRequest to /api/v1/plans/{name}/next on the plan-server.
func (r *RemotePlanOps) Next(ctx context.Context, req NextRequest) (NextResult, error) {
	if req.Plan == "" {
		return NextResult{}, fmt.Errorf("plan name is required")
	}
	endpoint := fmt.Sprintf("%s/api/v1/plans/%s/next", r.base, url.PathEscape(req.Plan))
	body, err := json.Marshal(req)
	if err != nil {
		return NextResult{}, fmt.Errorf("marshal next request: %w", err)
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return NextResult{}, fmt.Errorf("build next request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if r.version != "" {
		httpReq.Header.Set("X-Fil-Version", r.version)
	}

	resp, err := r.httpClient.Do(httpReq)
	if err != nil {
		return NextResult{}, fmt.Errorf("next request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent {
		b, _ := io.ReadAll(resp.Body)
		return NextResult{}, fmt.Errorf("next failed: status %d: %s", resp.StatusCode, strings.TrimSpace(string(b)))
	}
	if resp.StatusCode == http.StatusNoContent {
		return NextResult{}, nil
	}
	var result NextResult
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return NextResult{}, nil
	}
	return result, nil
}
