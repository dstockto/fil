package plan

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

// Pause hits the existing /api/v1/plans/{name}/pause endpoint on the
// plan-server.
func (r *RemotePlanOps) Pause(ctx context.Context, name string) error {
	return r.lifecycleAction(ctx, name, "pause")
}

// Resume hits /api/v1/plans/{name}/resume.
func (r *RemotePlanOps) Resume(ctx context.Context, name string) error {
	return r.lifecycleAction(ctx, name, "resume")
}

// lifecycleAction is the shared HTTP shape for verbs whose entire body is in
// the URL path: pause, resume, archive, unarchive, ... POST with no body,
// expect 204.
func (r *RemotePlanOps) lifecycleAction(ctx context.Context, name, action string) error {
	if name == "" {
		return fmt.Errorf("plan name is required")
	}
	endpoint := fmt.Sprintf("%s/api/v1/plans/%s/%s", r.base, url.PathEscape(name), action)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, nil)
	if err != nil {
		return fmt.Errorf("build %s request: %w", action, err)
	}
	if r.version != "" {
		req.Header.Set("X-Fil-Version", r.version)
	}

	resp, err := r.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("%s request failed: %w", action, err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusNoContent {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("%s failed: status %d: %s", action, resp.StatusCode, strings.TrimSpace(string(b)))
	}
	return nil
}
