package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/dstockto/fil/api"
	"github.com/dstockto/fil/devices"
	"github.com/fatih/color"
	"github.com/spf13/cobra"
)

var doctorCmd = &cobra.Command{
	Use:   "doctor",
	Short: "Run a health check across fil, Spoolman, the plan server, and printers",
	Long: `Runs a series of checks to verify that fil is set up correctly and all
dependencies are reachable. Reports ok/warn/fail for each check and exits
non-zero if anything is broken.

Exit codes:
  0  all checks passed
  1  at least one warning
  2  at least one failure`,
	RunE: func(cmd *cobra.Command, args []string) error {
		jsonOut, _ := cmd.Flags().GetBool("json")
		verbose, _ := cmd.Flags().GetBool("verbose")
		skipRaw, _ := cmd.Flags().GetString("skip")
		timeout, _ := cmd.Flags().GetDuration("timeout")

		skip := parseSkipList(skipRaw)

		// Use a top-level context with the requested per-check timeout budget.
		// Most checks are fast; we multiply by a small factor to allow several
		// to run in series without starving one another.
		ctx, cancel := context.WithTimeout(cmd.Context(), timeout*4)
		defer cancel()

		report := runDoctor(ctx, skip, timeout)

		if jsonOut {
			return writeJSON(cmd.OutOrStdout(), report)
		}
		writeHuman(cmd.OutOrStdout(), report, verbose)

		switch {
		case report.Summary.Fail > 0:
			os.Exit(2)
		case report.Summary.Warn > 0:
			os.Exit(1)
		}
		return nil
	},
}

//nolint:gochecknoinits
func init() {
	rootCmd.AddCommand(doctorCmd)
	doctorCmd.Flags().Bool("json", false, "output machine-readable JSON")
	doctorCmd.Flags().BoolP("verbose", "v", false, "show details on passing checks")
	doctorCmd.Flags().String("skip", "", "comma-separated list of check groups to skip (e.g. printers,notifications)")
	doctorCmd.Flags().Duration("timeout", 5*time.Second, "per-check network timeout")
}

// parseSkipList turns a comma-separated flag value into a lookup set.
func parseSkipList(raw string) map[string]bool {
	out := map[string]bool{}
	for _, part := range strings.Split(raw, ",") {
		p := strings.TrimSpace(strings.ToLower(part))
		if p != "" {
			out[p] = true
		}
	}
	return out
}

// runDoctor executes client-side checks, fetches the server health report, and merges them.
func runDoctor(ctx context.Context, skip map[string]bool, perCheckTimeout time.Duration) *api.HealthReport {
	report := &api.HealthReport{
		CheckedAt: time.Now().UTC(),
	}

	var checks []api.Check

	// Runtime info always goes first — cheap and useful in bug reports.
	if !skip["runtime"] {
		checks = append(checks, runtimeChecks()...)
	}

	// Config — must run first since other checks depend on having Cfg loaded.
	if !skip["config"] {
		checks = append(checks, clientConfigChecks()...)
	}

	// Server reachability + version match. Also fetches the server health report.
	var serverReport *api.HealthReport
	if !skip["server"] && Cfg != nil && Cfg.PlansServer != "" {
		srvChecks, srvReport := serverChecks(ctx, perCheckTimeout)
		checks = append(checks, srvChecks...)
		serverReport = srvReport
	}

	// Shared config divergence — needs the plan server to be reachable.
	if !skip["config"] && Cfg != nil && Cfg.PlansServer != "" {
		checks = append(checks, sharedConfigDivergenceCheck(ctx, perCheckTimeout))
	}

	// Spoolman from client's perspective (separate from server's check).
	if !skip["spoolman"] && Cfg != nil && Cfg.ApiBase != "" {
		checks = append(checks, clientSpoolmanCheck(ctx, perCheckTimeout))
	}

	// Printer mismatches (client-side cross-check using /api/v1/printers + Spoolman).
	if !skip["printers"] && Cfg != nil && Cfg.PlansServer != "" && Cfg.ApiBase != "" {
		checks = append(checks, mismatchCheck(ctx, perCheckTimeout))
	}

	// USB devices (TD-1 color scanner).
	if !skip["devices"] {
		checks = append(checks, td1DetectCheck())
	}

	// Merge in server-side checks, tagging so we can tell them apart.
	if serverReport != nil {
		for _, c := range serverReport.Checks {
			if skip[c.Group] {
				continue
			}
			checks = append(checks, c)
		}
		report.Version = serverReport.Version
		report.UptimeSeconds = serverReport.UptimeSeconds
	}

	sortDoctorChecks(checks)
	report.Checks = checks
	report.Tally()
	return report
}

// clientGroupOrder controls the grouping order in the human-readable output.
var clientGroupOrder = map[string]int{
	"runtime":       0,
	"config":        1,
	"server":        2,
	"spoolman":      3,
	"filesystem":    4,
	"history":       5,
	"printers":      6,
	"devices":       7,
	"notifications": 8,
}

func sortDoctorChecks(checks []api.Check) {
	sort.SliceStable(checks, func(i, j int) bool {
		gi, ok := clientGroupOrder[checks[i].Group]
		if !ok {
			gi = 99
		}
		gj, ok := clientGroupOrder[checks[j].Group]
		if !ok {
			gj = 99
		}
		if gi != gj {
			return gi < gj
		}
		return checks[i].Name < checks[j].Name
	})
}

// runtimeChecks records client binary and Go runtime info.
func runtimeChecks() []api.Check {
	cwd, _ := os.Getwd()
	detail, _ := json.Marshal(map[string]any{
		"client_version": version,
		"go_version":     runtime.Version(),
		"os":             runtime.GOOS,
		"arch":           runtime.GOARCH,
		"cwd":            cwd,
	})
	return []api.Check{
		{
			Group:   "runtime",
			Name:    "client",
			Status:  api.StatusOK,
			Message: fmt.Sprintf("fil %s (%s/%s)", version, runtime.GOOS, runtime.GOARCH),
			Detail:  detail,
		},
	}
}

// clientConfigChecks validates the loaded Config for obvious problems.
func clientConfigChecks() []api.Check {
	var out []api.Check

	// config files loaded
	paths := discoverConfigPaths()
	if len(paths) == 0 {
		out = append(out, api.Check{
			Group:   "config",
			Name:    "files_loaded",
			Status:  api.StatusWarn,
			Message: "no config files found in standard locations",
		})
	} else {
		detail, _ := json.Marshal(map[string]any{"paths": paths})
		out = append(out, api.Check{
			Group:   "config",
			Name:    "files_loaded",
			Status:  api.StatusOK,
			Message: fmt.Sprintf("%d file(s) loaded", len(paths)),
			Detail:  detail,
		})
	}

	if Cfg == nil {
		out = append(out, api.Check{
			Group:   "config",
			Name:    "loaded",
			Status:  api.StatusFail,
			Message: "no config loaded",
		})
		return out
	}

	// api_base
	if Cfg.ApiBase == "" {
		out = append(out, api.Check{
			Group:   "config",
			Name:    "api_base",
			Status:  api.StatusFail,
			Message: "api_base is not set",
		})
	} else {
		out = append(out, api.Check{
			Group:   "config",
			Name:    "api_base",
			Status:  api.StatusOK,
			Message: Cfg.ApiBase,
		})
	}

	// plans_server
	if Cfg.PlansServer == "" {
		out = append(out, api.Check{
			Group:   "config",
			Name:    "plans_server",
			Status:  api.StatusWarn,
			Message: "plans_server is not set (standalone mode)",
		})
	} else {
		out = append(out, api.Check{
			Group:   "config",
			Name:    "plans_server",
			Status:  api.StatusOK,
			Message: Cfg.PlansServer,
		})
	}

	// per-printer schema lint
	out = append(out, printerSchemaChecks()...)

	return out
}

// printerSchemaChecks returns one Check per configured printer validating
// required fields for its type.
func printerSchemaChecks() []api.Check {
	if Cfg == nil || len(Cfg.Printers) == 0 {
		return nil
	}
	var names []string
	for n := range Cfg.Printers {
		names = append(names, n)
	}
	sort.Strings(names)

	var out []api.Check
	for _, name := range names {
		p := Cfg.Printers[name]
		c := api.Check{
			Group: "config",
			Name:  "printer:" + name,
		}
		var missing []string
		if p.Type == "" {
			missing = append(missing, "type")
		}
		if p.IP == "" {
			missing = append(missing, "ip")
		}
		switch p.Type {
		case "bambu":
			if p.Serial == "" {
				missing = append(missing, "serial")
			}
			if p.AccessCode == "" {
				missing = append(missing, "access_code")
			}
		case "prusa":
			if p.Username == "" {
				missing = append(missing, "username")
			}
			if p.Password == "" {
				missing = append(missing, "password")
			}
		case "":
			// already flagged by missing type
		default:
			c.Status = api.StatusWarn
			c.Message = fmt.Sprintf("unknown type %q", p.Type)
			out = append(out, c)
			continue
		}
		if len(missing) > 0 {
			c.Status = api.StatusFail
			c.Message = "missing: " + strings.Join(missing, ", ")
		} else {
			c.Status = api.StatusOK
			c.Message = fmt.Sprintf("%s at %s", p.Type, p.IP)
		}
		out = append(out, c)
	}
	return out
}

// serverChecks verifies plan server reachability, version match, and fetches
// the server-side health report. Returns the client-side checks and the
// fetched report (or nil on fetch failure).
func serverChecks(ctx context.Context, perCheckTimeout time.Duration) ([]api.Check, *api.HealthReport) {
	var out []api.Check

	client := api.NewPlanServerClient(Cfg.PlansServer, version, Cfg.TLSSkipVerify)

	start := time.Now()
	rctx, cancel := context.WithTimeout(ctx, perCheckTimeout)
	defer cancel()

	report, err := client.GetHealth(rctx)
	dur := time.Since(start).Milliseconds()

	if err != nil {
		out = append(out, api.Check{
			Group:      "server",
			Name:       "reachable",
			Status:     api.StatusFail,
			DurationMs: dur,
			Message:    err.Error(),
		})
		return out, nil
	}

	out = append(out, api.Check{
		Group:      "server",
		Name:       "reachable",
		Status:     api.StatusOK,
		DurationMs: dur,
		Message:    Cfg.PlansServer,
	})

	// Version match
	if report.Version != "" && version != "" && version != "dev" && report.Version != version {
		out = append(out, api.Check{
			Group:   "server",
			Name:    "version_match",
			Status:  api.StatusWarn,
			Message: fmt.Sprintf("server=%s client=%s", report.Version, version),
		})
	} else if report.Version != "" {
		out = append(out, api.Check{
			Group:   "server",
			Name:    "version_match",
			Status:  api.StatusOK,
			Message: report.Version,
		})
	}

	return out, report
}

// clientSpoolmanCheck probes Spoolman from the client's own network.
func clientSpoolmanCheck(ctx context.Context, perCheckTimeout time.Duration) api.Check {
	start := time.Now()
	rctx, cancel := context.WithTimeout(ctx, perCheckTimeout)
	defer cancel()

	client := api.NewClient(Cfg.ApiBase, Cfg.TLSSkipVerify)
	info, err := client.GetInfo(rctx)
	dur := time.Since(start).Milliseconds()

	c := api.Check{
		Group:      "spoolman",
		Name:       "client_reachable",
		DurationMs: dur,
	}
	if err != nil {
		c.Status = api.StatusFail
		c.Message = err.Error()
		return c
	}
	c.Status = api.StatusOK
	if info.Version != "" {
		c.Message = fmt.Sprintf("version %s", info.Version)
	} else {
		c.Message = "reachable"
	}
	detail, _ := json.Marshal(map[string]any{
		"url":     Cfg.ApiBase,
		"version": info.Version,
	})
	c.Detail = detail
	return c
}

// mismatchCheck fetches printer status from the plan server and counts mismatches.
func mismatchCheck(ctx context.Context, perCheckTimeout time.Duration) api.Check {
	start := time.Now()
	rctx, cancel := context.WithTimeout(ctx, perCheckTimeout*2)
	defer cancel()

	c := api.Check{
		Group: "printers",
		Name:  "mismatches",
	}
	defer func() { c.DurationMs = time.Since(start).Milliseconds() }()

	planClient := api.NewPlanServerClient(Cfg.PlansServer, version, Cfg.TLSSkipVerify)
	statuses, err := planClient.GetPrinterStatus(rctx)
	if err != nil {
		c.Status = api.StatusWarn
		c.Message = fmt.Sprintf("could not fetch printer status: %v", err)
		return c
	}

	mismatches := detectMismatches(rctx, statuses)
	if len(mismatches) == 0 {
		c.Status = api.StatusOK
		c.Message = "no mismatches"
		return c
	}

	c.Status = api.StatusWarn
	c.Message = fmt.Sprintf("%d mismatch(es) — run: fil verify", len(mismatches))
	return c
}

// td1DetectCheck probes for an attached TD-1 color/transmission scanner.
// The device is optional hardware, so absence is a warn, not a fail.
func td1DetectCheck() api.Check {
	start := time.Now()
	c := api.Check{Group: "devices", Name: "td1_detected"}
	defer func() { c.DurationMs = time.Since(start).Milliseconds() }()

	info, err := devices.Probe(nil)
	if err != nil {
		if errors.Is(err, devices.ErrNoDevice) {
			c.Status = api.StatusWarn
			c.Message = "no TD-1 attached (optional)"
			return c
		}
		c.Status = api.StatusFail
		c.Message = err.Error()
		return c
	}
	c.Status = api.StatusOK
	c.Message = fmt.Sprintf("TD-1 on %s", info.Path)
	detail, _ := json.Marshal(map[string]any{
		"path":   info.Path,
		"vid":    info.VID,
		"pid":    info.PID,
		"serial": info.Serial,
	})
	c.Detail = detail
	return c
}

// sharedConfigDivergenceCheck fetches the server's shared config and diffs it
// against the local ~/.config/fil/shared-config.json file.
func sharedConfigDivergenceCheck(ctx context.Context, perCheckTimeout time.Duration) api.Check {
	start := time.Now()
	c := api.Check{
		Group: "config",
		Name:  "shared_config_divergence",
	}
	defer func() { c.DurationMs = time.Since(start).Milliseconds() }()

	home, err := os.UserHomeDir()
	if err != nil {
		c.Status = api.StatusWarn
		c.Message = "cannot determine home directory"
		return c
	}
	localPath := filepath.Join(home, ".config", "fil", "shared-config.json")
	localData, err := os.ReadFile(localPath)
	if err != nil {
		if os.IsNotExist(err) {
			c.Status = api.StatusWarn
			c.Message = "no local shared-config.json (run: fil config pull)"
			return c
		}
		c.Status = api.StatusFail
		c.Message = err.Error()
		return c
	}

	rctx, cancel := context.WithTimeout(ctx, perCheckTimeout)
	defer cancel()
	client := api.NewPlanServerClient(Cfg.PlansServer, version, Cfg.TLSSkipVerify)
	serverData, err := client.GetSharedConfig(rctx)
	if err != nil {
		c.Status = api.StatusWarn
		c.Message = fmt.Sprintf("cannot fetch server config: %v", err)
		return c
	}

	diffs, err := diffJSON(localData, serverData)
	if err != nil {
		c.Status = api.StatusWarn
		c.Message = fmt.Sprintf("could not compare configs: %v", err)
		return c
	}

	if len(diffs) == 0 {
		c.Status = api.StatusOK
		c.Message = "in sync with server"
		return c
	}

	c.Status = api.StatusWarn
	c.Message = fmt.Sprintf("%d difference(s) — run: fil config pull", len(diffs))
	detail, _ := json.Marshal(map[string]any{"differences": diffs})
	c.Detail = detail
	return c
}

// JSONDiff is a single difference between two JSON values.
type JSONDiff struct {
	Path   string `json:"path"`
	Local  any    `json:"local,omitempty"`
	Server any    `json:"server,omitempty"`
}

// diffJSON unmarshals two JSON blobs as generic maps and reports differences.
// Paths are dotted (e.g. "printers.Bambu X1C.ip"). Arrays are compared
// element-by-element using numeric indices (e.g. "low_ignore[0]").
func diffJSON(localRaw, serverRaw []byte) ([]JSONDiff, error) {
	var local, server any
	if len(localRaw) > 0 {
		if err := json.Unmarshal(localRaw, &local); err != nil {
			return nil, fmt.Errorf("local: %w", err)
		}
	}
	if len(serverRaw) > 0 {
		if err := json.Unmarshal(serverRaw, &server); err != nil {
			return nil, fmt.Errorf("server: %w", err)
		}
	}
	var diffs []JSONDiff
	walkDiff("", local, server, &diffs)
	sort.Slice(diffs, func(i, j int) bool { return diffs[i].Path < diffs[j].Path })
	return diffs, nil
}

func walkDiff(path string, local, server any, out *[]JSONDiff) {
	lm, lIsMap := local.(map[string]any)
	sm, sIsMap := server.(map[string]any)
	if lIsMap && sIsMap {
		keys := map[string]struct{}{}
		for k := range lm {
			keys[k] = struct{}{}
		}
		for k := range sm {
			keys[k] = struct{}{}
		}
		var sorted []string
		for k := range keys {
			sorted = append(sorted, k)
		}
		sort.Strings(sorted)
		for _, k := range sorted {
			walkDiff(joinPath(path, k), lm[k], sm[k], out)
		}
		return
	}

	la, lIsArr := local.([]any)
	sa, sIsArr := server.([]any)
	if lIsArr && sIsArr {
		if len(la) != len(sa) {
			*out = append(*out, JSONDiff{Path: path, Local: la, Server: sa})
			return
		}
		for i := range la {
			walkDiff(fmt.Sprintf("%s[%d]", path, i), la[i], sa[i], out)
		}
		return
	}

	if !jsonEqual(local, server) {
		*out = append(*out, JSONDiff{Path: path, Local: local, Server: server})
	}
}

func joinPath(prefix, key string) string {
	if prefix == "" {
		return key
	}
	return prefix + "." + key
}

func jsonEqual(a, b any) bool {
	ab, errA := json.Marshal(a)
	bb, errB := json.Marshal(b)
	if errA != nil || errB != nil {
		return false
	}
	return string(ab) == string(bb)
}

// writeJSON emits the report as indented JSON.
func writeJSON(w io.Writer, report *api.HealthReport) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(report)
}

// writeHuman prints the report in grouped, colorized human-readable form.
func writeHuman(w io.Writer, report *api.HealthReport, verbose bool) {
	groups := make(map[string][]api.Check)
	var groupOrder []string
	for _, c := range report.Checks {
		if _, ok := groups[c.Group]; !ok {
			groupOrder = append(groupOrder, c.Group)
		}
		groups[c.Group] = append(groups[c.Group], c)
	}

	okC := color.New(color.FgGreen).SprintFunc()
	warnC := color.New(color.FgYellow).SprintFunc()
	failC := color.New(color.FgRed, color.Bold).SprintFunc()
	skipC := color.New(color.FgHiBlack).SprintFunc()
	bold := color.New(color.Bold).SprintFunc()

	status := func(s api.CheckStatus) string {
		switch s {
		case api.StatusOK:
			return okC("ok  ")
		case api.StatusWarn:
			return warnC("warn")
		case api.StatusFail:
			return failC("fail")
		case api.StatusSkip:
			return skipC("skip")
		}
		return string(s)
	}

	for _, g := range groupOrder {
		_, _ = fmt.Fprintln(w, bold(groupLabel(g)))
		for _, c := range groups[g] {
			if c.Status == api.StatusOK && !verbose {
				_, _ = fmt.Fprintf(w, "  %s  %-28s %s\n", status(c.Status), c.Name, dim(c.Message))
			} else {
				_, _ = fmt.Fprintf(w, "  %s  %-28s %s\n", status(c.Status), c.Name, c.Message)
			}
			// Render divergence detail inline in non-json mode.
			if c.Name == "shared_config_divergence" && c.Status == api.StatusWarn && len(c.Detail) > 0 {
				renderDivergenceDetail(w, c.Detail)
			}
		}
		_, _ = fmt.Fprintln(w)
	}

	_, _ = fmt.Fprintf(w, "Summary: %s, %s, %s",
		okC(fmt.Sprintf("%d ok", report.Summary.OK)),
		warnC(fmt.Sprintf("%d warn", report.Summary.Warn)),
		failC(fmt.Sprintf("%d fail", report.Summary.Fail)),
	)
	if report.Summary.Skip > 0 {
		_, _ = fmt.Fprintf(w, ", %s", skipC(fmt.Sprintf("%d skipped", report.Summary.Skip)))
	}
	_, _ = fmt.Fprintln(w)
}

func groupLabel(g string) string {
	switch g {
	case "config":
		return "Config"
	case "runtime":
		return "Runtime"
	case "server":
		return "Plan server"
	case "spoolman":
		return "Spoolman"
	case "filesystem":
		return "Filesystem (server)"
	case "history":
		return "History (server)"
	case "printers":
		return "Printers"
	case "notifications":
		return "Notifications (server)"
	}
	return strings.Title(g) //nolint:staticcheck // SA1019: acceptable for simple labels
}

func dim(s string) string {
	return color.New(color.FgHiBlack).Sprint(s)
}

func renderDivergenceDetail(w io.Writer, raw json.RawMessage) {
	var d struct {
		Differences []JSONDiff `json:"differences"`
	}
	if err := json.Unmarshal(raw, &d); err != nil {
		return
	}
	for _, diff := range d.Differences {
		_, _ = fmt.Fprintf(w, "      %s: local=%s server=%s\n", diff.Path, formatValue(diff.Local), formatValue(diff.Server))
	}
}

func formatValue(v any) string {
	if v == nil {
		return "(missing)"
	}
	b, _ := json.Marshal(v)
	return string(b)
}
