package plan

import (
	"crypto/tls"
	"net/http"
	"strings"
	"time"
)

// RemotePlanOps is the adapter used in Remote Mode: every verb is an HTTP
// call to a plan-server, which runs LocalPlanOps internally to do the actual
// work. The CLI in Remote Mode never touches Spoolman directly.
type RemotePlanOps struct {
	base       string
	version    string
	httpClient *http.Client
}

// NewRemote builds a RemotePlanOps pointing at the plan-server URL. The
// version string is sent in X-Fil-Version so the server can warn on
// version-skew (matching api.PlanServerClient's existing behaviour).
func NewRemote(base, version string, tlsSkipVerify bool) *RemotePlanOps {
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
	}
}
