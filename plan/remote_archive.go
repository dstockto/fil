package plan

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

// Archive hits /api/v1/plans/{name}/archive — same lifecycleAction shape as
// Pause/Resume.
func (r *RemotePlanOps) Archive(ctx context.Context, name string) error {
	return r.lifecycleAction(ctx, name, "archive")
}

// Unarchive hits /api/v1/plans/{name}/unarchive.
func (r *RemotePlanOps) Unarchive(ctx context.Context, name string) error {
	return r.lifecycleAction(ctx, name, "unarchive")
}

// Delete hits DELETE /api/v1/plans/{name}. Treats 204 and 404 as success
// (the plan is gone either way), matching the previous api.PlanServerClient
// semantics.
func (r *RemotePlanOps) Delete(ctx context.Context, name string) error {
	if name == "" {
		return fmt.Errorf("plan name is required")
	}
	endpoint := fmt.Sprintf("%s/api/v1/plans/%s", r.base, url.PathEscape(name))
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, endpoint, nil)
	if err != nil {
		return fmt.Errorf("build delete request: %w", err)
	}
	if r.version != "" {
		req.Header.Set("X-Fil-Version", r.version)
	}

	resp, err := r.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("delete request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusNoContent && resp.StatusCode != http.StatusNotFound {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("delete failed: status %d: %s", resp.StatusCode, strings.TrimSpace(string(b)))
	}
	return nil
}
