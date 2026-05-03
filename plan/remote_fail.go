package plan

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// Fail POSTs the FailRequest to the plan-server and returns an empty result on
// success. The server runs its own LocalPlanOps under the hood; the CLI does
// not see per-spool allocations in Remote Mode (server returns 204).
func (r *RemotePlanOps) Fail(ctx context.Context, req FailRequest) (FailResult, error) {
	endpoint := r.base + "/api/v1/plan-fail"
	body, err := json.Marshal(req)
	if err != nil {
		return FailResult{}, fmt.Errorf("marshal fail request: %w", err)
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return FailResult{}, fmt.Errorf("build fail request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if r.version != "" {
		httpReq.Header.Set("X-Fil-Version", r.version)
	}

	resp, err := r.httpClient.Do(httpReq)
	if err != nil {
		return FailResult{}, fmt.Errorf("plan-fail request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusNoContent {
		b, _ := io.ReadAll(resp.Body)
		return FailResult{}, fmt.Errorf("plan-fail failed: status %d: %s", resp.StatusCode, strings.TrimSpace(string(b)))
	}
	return FailResult{}, nil
}
