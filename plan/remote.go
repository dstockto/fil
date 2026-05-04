package plan

import (
	"crypto/tls"
	"net/http"
	"strings"
	"time"
)

// RemotePlanOps is the adapter used in Remote Mode: every verb is an HTTP
// call to a plan-server, which runs LocalPlanOps internally to do the actual
// work. Writes to Spoolman happen on the server side; the CLI in Remote Mode
// only does Spoolman *reads* (for interactive flows + SaveAll color
// backfill).
type RemotePlanOps struct {
	base       string
	version    string
	httpClient *http.Client
	spoolman   Spoolman // optional, used by SaveAll for color backfill before PUT
}

// NewRemote builds a RemotePlanOps pointing at the plan-server URL. The
// version string is sent in X-Fil-Version so the server can warn on
// version-skew (matching api.PlanServerClient's existing behaviour). The
// spoolman parameter may be nil — in that case SaveAll skips color backfill.
func NewRemote(base, version string, tlsSkipVerify bool, spoolman Spoolman) *RemotePlanOps {
	client := &http.Client{Timeout: 30 * time.Second}
	if tlsSkipVerify {
		client.Transport = &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, //nolint:gosec
		}
	}
	return &RemotePlanOps{
		base:       strings.TrimRight(base, "/"),
		version:    version,
		httpClient: client,
		spoolman:   spoolman,
	}
}
