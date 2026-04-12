package server

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"syscall"
	"time"

	"github.com/dstockto/fil/api"
)

// StalePrinterThreshold is how long a printer's last update can be before a
// warn is raised. Bambu MQTT updates every few seconds; Prusa polls every 30s.
const StalePrinterThreshold = 2 * time.Minute

// LowDiskWarn is the free-bytes threshold below which a filesystem check warns.
const LowDiskWarn int64 = 1 << 30 // 1 GiB

// LowDiskFail is the free-bytes threshold below which a filesystem check fails.
const LowDiskFail int64 = 100 << 20 // 100 MiB

// RunHealthChecks executes all server-side health checks and returns a report.
func (s *PlanServer) RunHealthChecks(ctx context.Context) *api.HealthReport {
	report := &api.HealthReport{
		Version:   s.Version,
		CheckedAt: time.Now().UTC(),
	}
	if !s.StartedAt.IsZero() {
		report.UptimeSeconds = int64(time.Since(s.StartedAt).Seconds())
	}

	var (
		mu     sync.Mutex
		wg     sync.WaitGroup
		checks []api.Check
	)
	add := func(cs ...api.Check) {
		mu.Lock()
		checks = append(checks, cs...)
		mu.Unlock()
	}

	wg.Add(1)
	go func() {
		defer wg.Done()
		add(s.fsChecks()...)
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		add(s.spoolmanCheck(ctx))
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		add(s.printerChecks()...)
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		add(s.historyCheck())
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		add(s.notificationCheck())
	}()

	wg.Wait()

	sortChecks(checks)
	report.Checks = checks
	report.Tally()
	return report
}

// groupOrder defines the preferred ordering of check groups in the report.
var groupOrder = map[string]int{
	"filesystem":    0,
	"spoolman":      1,
	"history":       2,
	"printers":      3,
	"notifications": 4,
}

func sortChecks(checks []api.Check) {
	sort.SliceStable(checks, func(i, j int) bool {
		gi, ok := groupOrder[checks[i].Group]
		if !ok {
			gi = 99
		}
		gj, ok := groupOrder[checks[j].Group]
		if !ok {
			gj = 99
		}
		if gi != gj {
			return gi < gj
		}
		return checks[i].Name < checks[j].Name
	})
}

// fsChecks verifies each configured directory exists, is writable, and has free space.
func (s *PlanServer) fsChecks() []api.Check {
	type dirEntry struct {
		name string
		path string
	}
	dirs := []dirEntry{
		{"plans_dir", s.PlansDir},
		{"pause_dir", s.PauseDir},
		{"archive_dir", s.ArchiveDir},
		{"assemblies_dir", s.AssembliesDir},
		{"config_dir", s.ConfigDir},
	}

	var out []api.Check
	for _, d := range dirs {
		out = append(out, checkDir(d.name, d.path))
	}
	return out
}

func checkDir(name, path string) api.Check {
	start := time.Now()
	c := api.Check{
		Group: "filesystem",
		Name:  name,
	}
	defer func() {
		c.DurationMs = time.Since(start).Milliseconds()
	}()

	if path == "" {
		c.Status = api.StatusSkip
		c.Message = "not configured"
		return c
	}

	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			c.Status = api.StatusFail
			c.Message = fmt.Sprintf("%s: does not exist", path)
		} else {
			c.Status = api.StatusFail
			c.Message = fmt.Sprintf("%s: %v", path, err)
		}
		c.Detail = rawJSON(map[string]any{"path": path})
		return c
	}
	if !info.IsDir() {
		c.Status = api.StatusFail
		c.Message = fmt.Sprintf("%s: not a directory", path)
		c.Detail = rawJSON(map[string]any{"path": path})
		return c
	}

	writable := isWritable(path)
	freeBytes, freeErr := diskFree(path)

	detail := map[string]any{
		"path":     path,
		"writable": writable,
	}
	if freeErr == nil {
		detail["free_bytes"] = freeBytes
	}
	c.Detail = rawJSON(detail)

	switch {
	case !writable:
		c.Status = api.StatusFail
		c.Message = fmt.Sprintf("%s: not writable", path)
	case freeErr != nil:
		c.Status = api.StatusWarn
		c.Message = fmt.Sprintf("%s: free space unknown (%v)", path, freeErr)
	case freeBytes < LowDiskFail:
		c.Status = api.StatusFail
		c.Message = fmt.Sprintf("%s: only %s free", path, humanBytes(freeBytes))
	case freeBytes < LowDiskWarn:
		c.Status = api.StatusWarn
		c.Message = fmt.Sprintf("%s: %s free", path, humanBytes(freeBytes))
	default:
		c.Status = api.StatusOK
		c.Message = fmt.Sprintf("%s (%s free)", path, humanBytes(freeBytes))
	}
	return c
}

// isWritable tests writability by creating and removing a temp file in the dir.
func isWritable(dir string) bool {
	f, err := os.CreateTemp(dir, ".fil-doctor-*")
	if err != nil {
		return false
	}
	_ = f.Close()
	_ = os.Remove(f.Name())
	return true
}

// diskFree returns bytes free on the filesystem containing path.
func diskFree(path string) (int64, error) {
	var st syscall.Statfs_t
	if err := syscall.Statfs(path, &st); err != nil {
		return 0, err
	}
	//nolint:unconvert // Bavail type varies by platform
	return int64(st.Bavail) * int64(st.Bsize), nil
}

// spoolmanCheck probes the Spoolman /info endpoint from the server.
func (s *PlanServer) spoolmanCheck(ctx context.Context) api.Check {
	start := time.Now()
	c := api.Check{
		Group: "spoolman",
		Name:  "reachable",
	}

	if s.ApiBase == "" {
		c.Status = api.StatusSkip
		c.Message = "api_base not configured"
		c.DurationMs = time.Since(start).Milliseconds()
		return c
	}

	client := api.NewClient(s.ApiBase, s.TLSSkipVerify)
	info, err := client.GetInfo(ctx)
	c.DurationMs = time.Since(start).Milliseconds()

	if err != nil {
		c.Status = api.StatusFail
		c.Message = fmt.Sprintf("%s: %v", s.ApiBase, err)
		c.Detail = rawJSON(map[string]any{"url": s.ApiBase})
		return c
	}

	c.Status = api.StatusOK
	c.Message = fmt.Sprintf("version %s", info.Version)
	c.Detail = rawJSON(map[string]any{
		"url":     s.ApiBase,
		"version": info.Version,
	})
	return c
}

// printerChecks reports each printer's connection state and last-update age.
func (s *PlanServer) printerChecks() []api.Check {
	if s.Printers == nil {
		return nil
	}

	start := time.Now()
	states := s.Printers.AllStatus()
	sharedDuration := time.Since(start).Milliseconds()

	var out []api.Check
	for _, st := range states {
		c := api.Check{
			Group:      "printers",
			Name:       st.Name,
			DurationMs: sharedDuration,
		}

		age := time.Duration(0)
		if !st.LastUpdated.IsZero() {
			age = time.Since(st.LastUpdated)
		}
		ageSecs := int64(age.Seconds())

		detail := map[string]any{
			"type":                st.Type,
			"state":               st.State,
			"last_update_seconds": ageSecs,
		}
		if len(st.Trays) > 0 {
			detail["tray_count"] = len(st.Trays)
		}

		switch {
		case st.LastUpdated.IsZero():
			c.Status = api.StatusFail
			c.Message = "never reported"
		case st.State == "offline":
			c.Status = api.StatusFail
			c.Message = "offline"
		case age > StalePrinterThreshold:
			c.Status = api.StatusWarn
			c.Message = fmt.Sprintf("last update %s ago", humanDuration(age))
		default:
			c.Status = api.StatusOK
			msg := fmt.Sprintf("%s, last update %s ago", st.State, humanDuration(age))
			if len(st.Trays) > 0 {
				msg = fmt.Sprintf("%s, %d trays", msg, len(st.Trays))
			}
			c.Message = msg
		}
		c.Detail = rawJSON(detail)
		out = append(out, c)
	}
	return out
}

// historyCheck reports on the print-history.jsonl file.
func (s *PlanServer) historyCheck() api.Check {
	start := time.Now()
	c := api.Check{
		Group: "history",
		Name:  "print_history",
	}
	defer func() {
		c.DurationMs = time.Since(start).Milliseconds()
	}()

	if s.PlansDir == "" {
		c.Status = api.StatusSkip
		c.Message = "plans_dir not configured"
		return c
	}

	path := filepath.Join(s.PlansDir, "print-history.jsonl")
	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			c.Status = api.StatusWarn
			c.Message = "no history file yet"
			c.Detail = rawJSON(map[string]any{"path": path})
			return c
		}
		c.Status = api.StatusFail
		c.Message = err.Error()
		return c
	}

	lastTS, entryCount := lastHistoryTimestamp(path)
	detail := map[string]any{
		"path":       path,
		"size_bytes": info.Size(),
	}
	if entryCount >= 0 {
		detail["entries"] = entryCount
	}
	if lastTS != "" {
		detail["last_entry"] = lastTS
	}

	c.Status = api.StatusOK
	c.Message = fmt.Sprintf("%s, %s", humanBytes(info.Size()), pluralize(entryCount, "entry", "entries"))
	c.Detail = rawJSON(detail)
	return c
}

// lastHistoryTimestamp scans a jsonl history file for line count and last timestamp.
// Returns "" if none found. Returns -1 for count if the file can't be read.
func lastHistoryTimestamp(path string) (string, int) {
	f, err := os.Open(path)
	if err != nil {
		return "", -1
	}
	defer func() { _ = f.Close() }()

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	count := 0
	last := ""
	for scanner.Scan() {
		var e struct {
			Timestamp string `json:"timestamp"`
		}
		if err := json.Unmarshal(scanner.Bytes(), &e); err == nil && e.Timestamp != "" {
			last = e.Timestamp
		}
		count++
	}
	return last, count
}

// notificationCheck validates Pushover credentials if configured.
func (s *PlanServer) notificationCheck() api.Check {
	start := time.Now()
	c := api.Check{
		Group: "notifications",
		Name:  "pushover",
	}
	defer func() {
		c.DurationMs = time.Since(start).Milliseconds()
	}()

	if s.Notifier == nil {
		c.Status = api.StatusSkip
		c.Message = "not configured"
		return c
	}

	if err := s.Notifier.ValidatePushover(); err != nil {
		// Distinguish "not configured" from "rejected"
		if err.Error() == "pushover credentials not configured" {
			c.Status = api.StatusSkip
			c.Message = "not configured"
			return c
		}
		c.Status = api.StatusFail
		c.Message = err.Error()
		return c
	}

	c.Status = api.StatusOK
	c.Message = "credentials valid"
	return c
}

// rawJSON marshals a value to json.RawMessage, returning nil on error.
func rawJSON(v any) json.RawMessage {
	b, err := json.Marshal(v)
	if err != nil {
		return nil
	}
	return b
}

func humanBytes(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for m := n / unit; m >= unit; m /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(n)/float64(div), "KMGTPE"[exp])
}

func humanDuration(d time.Duration) string {
	if d < time.Second {
		return "<1s"
	}
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	if d < time.Hour {
		return fmt.Sprintf("%dm", int(d.Minutes()))
	}
	return fmt.Sprintf("%.1fh", d.Hours())
}

func pluralize(n int, one, many string) string {
	if n == 1 {
		return fmt.Sprintf("%d %s", n, one)
	}
	return fmt.Sprintf("%d %s", n, many)
}
