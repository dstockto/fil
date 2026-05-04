package plan

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/dstockto/fil/models"
	"gopkg.in/yaml.v3"
)

// SaveAll runs the SaveAll-time color backfill (best-effort, requires the
// optional Spoolman dep), marshals the plan to YAML, and PUTs it to
// /api/v1/plans/{name}.
func (r *RemotePlanOps) SaveAll(ctx context.Context, name string, plan models.PlanFile) error {
	if name == "" {
		return fmt.Errorf("plan name is required")
	}
	applyColorBackfill(ctx, r.spoolman, &plan)

	out, err := yaml.Marshal(plan)
	if err != nil {
		return fmt.Errorf("marshal plan: %w", err)
	}
	return r.putPlanBytes(ctx, name, out)
}

// SaveBytes PUTs raw bytes to /api/v1/plans/{name}, bypassing marshal +
// backfill so $EDITOR-driven flows preserve formatting exactly.
func (r *RemotePlanOps) SaveBytes(ctx context.Context, name string, data []byte) error {
	if name == "" {
		return fmt.Errorf("plan name is required")
	}
	return r.putPlanBytes(ctx, name, data)
}

// putPlanBytes is the shared HTTP shape for both SaveAll and SaveBytes.
func (r *RemotePlanOps) putPlanBytes(ctx context.Context, name string, data []byte) error {
	endpoint := fmt.Sprintf("%s/api/v1/plans/%s", r.base, url.PathEscape(name))
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, endpoint, bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("build save request: %w", err)
	}
	if r.version != "" {
		req.Header.Set("X-Fil-Version", r.version)
	}

	resp, err := r.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("save request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusNoContent {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("save failed: status %d: %s", resp.StatusCode, strings.TrimSpace(string(b)))
	}
	return nil
}
