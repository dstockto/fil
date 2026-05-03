package server

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/dstockto/fil/models"
	"github.com/dstockto/fil/plan"
	"gopkg.in/yaml.v3"
)

const versionHeader = "X-Fil-Version"

// PlanServer handles HTTP requests for plan CRUD and lifecycle operations.
type PlanServer struct {
	PlansDir        string
	PauseDir        string
	ArchiveDir      string
	ConfigDir       string
	AssembliesDir   string
	Version         string
	ApiBase         string // Spoolman base URL, used by health checks
	ApiBaseInternal string // optional alternate URL for server-side probes (e.g. http://localhost:8000)
	TLSSkipVerify   bool   // passed to health-check HTTP clients
	StartedAt       time.Time
	Watcher         *ETAWatcher
	Printers        *PrinterManager
	Notifier        *Notifier
	// PlanOps runs verbs that mutate Plan state (currently only Fail; more
	// verbs migrate from cmd/plan_*.go in subsequent PRs). The server uses a
	// LocalPlanOps under the hood — Remote-Mode CLIs delegate here.
	PlanOps plan.PlanOperations
}

// PlanSummary is the JSON representation returned by the list endpoint.
type PlanSummary struct {
	Name        string `json:"name"`
	Projects    int    `json:"projects"`
	PlatesTodo  int    `json:"plates_todo"`
	HasAssembly bool   `json:"has_assembly"`
}

// Routes registers all plan API routes on a new ServeMux using Go 1.22+ method routing.
func (s *PlanServer) Routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/plans", s.handleListPlans)
	mux.HandleFunc("GET /api/v1/plans/{name}", s.handleGetPlan)
	mux.HandleFunc("PUT /api/v1/plans/{name}", s.handlePutPlan)
	mux.HandleFunc("DELETE /api/v1/plans/{name}", s.handleDeletePlan)
	mux.HandleFunc("POST /api/v1/plans/{name}/pause", s.handlePausePlan)
	mux.HandleFunc("POST /api/v1/plans/{name}/resume", s.handleResumePlan)
	mux.HandleFunc("POST /api/v1/plans/{name}/archive", s.handleArchivePlan)
	mux.HandleFunc("POST /api/v1/plans/{name}/unarchive", s.handleUnarchivePlan)
	mux.HandleFunc("PUT /api/v1/plans/{name}/assembly", s.handlePutAssembly)
	mux.HandleFunc("GET /api/v1/plans/{name}/assembly", s.handleGetAssembly)
	mux.HandleFunc("DELETE /api/v1/plans/{name}/assembly", s.handleDeleteAssembly)
	mux.HandleFunc("GET /api/v1/config", s.handleGetConfig)
	mux.HandleFunc("PUT /api/v1/config", s.handlePutConfig)
	mux.HandleFunc("POST /api/v1/plans/clean-assemblies", s.handleCleanAssemblies)
	mux.HandleFunc("GET /api/v1/history", s.handleHistory)
	mux.HandleFunc("POST /api/v1/plan-fail", s.handlePlanFail)
	mux.HandleFunc("POST /api/v1/plans/{name}/complete", s.handlePlanComplete)
	mux.HandleFunc("POST /api/v1/scan-history", s.handleScanHistoryPost)
	mux.HandleFunc("GET /api/v1/scan-history", s.handleScanHistoryGet)
	mux.HandleFunc("GET /api/v1/printers", s.handleListPrinters)
	mux.HandleFunc("POST /api/v1/printers/{name}/push-tray", s.handlePushTray)
	mux.HandleFunc("GET /api/v1/version", s.handleVersion)
	mux.HandleFunc("GET /api/v1/doctor", s.handleHealth)
	mux.HandleFunc("POST /api/v1/notify/test", s.handleNotifyTest)
	mux.HandleFunc("GET /api/v1/say", s.handleSay)
	return s.versionMiddleware(mux)
}

// versionMiddleware adds the server's version header to every response.
func (s *PlanServer) versionMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.Version != "" {
			w.Header().Set(versionHeader, s.Version)
		}
		next.ServeHTTP(w, r)
	})
}

func (s *PlanServer) handleListPrinters(w http.ResponseWriter, r *http.Request) {
	if s.Printers == nil {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte("[]"))
		return
	}

	states := s.Printers.AllStatus()
	if states == nil {
		states = []PrinterState{}
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(states)
}

func (s *PlanServer) handlePushTray(w http.ResponseWriter, r *http.Request) {
	if s.Printers == nil {
		http.Error(w, "no printer connections configured", http.StatusBadRequest)
		return
	}

	name := r.PathValue("name")
	if name == "" {
		http.Error(w, "printer name required", http.StatusBadRequest)
		return
	}

	var update TrayUpdate
	if err := json.NewDecoder(r.Body).Decode(&update); err != nil {
		http.Error(w, fmt.Sprintf("invalid request body: %v", err), http.StatusBadRequest)
		return
	}

	if err := s.Printers.PushTray(name, update); err != nil {
		http.Error(w, fmt.Sprintf("failed to push tray: %v", err), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (s *PlanServer) handleVersion(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"version": s.Version})
}

func (s *PlanServer) handleHealth(w http.ResponseWriter, r *http.Request) {
	report := s.RunHealthChecks(r.Context())
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(report)
}

func (s *PlanServer) handleListPlans(w http.ResponseWriter, r *http.Request) {
	status := r.URL.Query().Get("status")

	var dir string
	switch status {
	case "paused":
		if s.PauseDir == "" {
			http.Error(w, "pause directory not configured", http.StatusBadRequest)
			return
		}
		dir = s.PauseDir
	case "archived":
		if s.ArchiveDir == "" {
			http.Error(w, "archive directory not configured", http.StatusBadRequest)
			return
		}
		dir = s.ArchiveDir
	default:
		dir = s.PlansDir
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte("[]"))
			return
		}
		http.Error(w, fmt.Sprintf("failed to read directory: %v", err), http.StatusInternalServerError)
		return
	}

	var summaries []PlanSummary
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		ext := strings.ToLower(filepath.Ext(e.Name()))
		if ext != ".yaml" && ext != ".yml" {
			continue
		}

		data, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			continue
		}
		var plan models.PlanFile
		if err := yaml.Unmarshal(data, &plan); err != nil {
			continue
		}
		plan.DefaultStatus()

		platesTodo := 0
		for _, proj := range plan.Projects {
			if proj.Status == "completed" {
				continue
			}
			for _, plate := range proj.Plates {
				if plate.Status != "completed" {
					platesTodo++
				}
			}
		}

		summaries = append(summaries, PlanSummary{
			Name:        e.Name(),
			Projects:    len(plan.Projects),
			PlatesTodo:  platesTodo,
			HasAssembly: plan.Assembly != "",
		})
	}

	if summaries == nil {
		summaries = []PlanSummary{}
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(summaries)
}

func (s *PlanServer) handleGetPlan(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if name == "" {
		http.Error(w, "plan name required", http.StatusBadRequest)
		return
	}

	dir := s.PlansDir
	switch r.URL.Query().Get("status") {
	case "paused":
		if s.PauseDir == "" {
			http.Error(w, "pause directory not configured", http.StatusBadRequest)
			return
		}
		dir = s.PauseDir
	case "archived":
		if s.ArchiveDir == "" {
			http.Error(w, "archive directory not configured", http.StatusBadRequest)
			return
		}
		dir = s.ArchiveDir
	}

	path := filepath.Join(dir, filepath.Base(name))
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			http.Error(w, "plan not found", http.StatusNotFound)
			return
		}
		http.Error(w, fmt.Sprintf("failed to read plan: %v", err), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/x-yaml")
	_, _ = w.Write(data)
}

func (s *PlanServer) handlePutPlan(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if name == "" {
		http.Error(w, "plan name required", http.StatusBadRequest)
		return
	}

	data, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, fmt.Sprintf("failed to read request body: %v", err), http.StatusBadRequest)
		return
	}

	// Validate that the body is valid YAML
	var plan models.PlanFile
	if err := yaml.Unmarshal(data, &plan); err != nil {
		http.Error(w, fmt.Sprintf("invalid YAML: %v", err), http.StatusBadRequest)
		return
	}

	path := filepath.Join(s.PlansDir, filepath.Base(name))

	// Read old plan for completion diffing before overwriting
	var oldPlan *models.PlanFile
	if oldData, err := os.ReadFile(path); err == nil {
		var op models.PlanFile
		if yaml.Unmarshal(oldData, &op) == nil {
			op.DefaultStatus()
			oldPlan = &op
		}
	}

	if err := os.WriteFile(path, data, 0644); err != nil {
		http.Error(w, fmt.Sprintf("failed to write plan: %v", err), http.StatusInternalServerError)
		return
	}

	// Log any plates that transitioned to completed
	plan.DefaultStatus()
	s.logCompletions(name, oldPlan, &plan)

	if s.Watcher != nil {
		s.Watcher.Reschedule()
	}

	w.WriteHeader(http.StatusNoContent)
}

func (s *PlanServer) handleDeletePlan(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if name == "" {
		http.Error(w, "plan name required", http.StatusBadRequest)
		return
	}

	path := filepath.Join(s.PlansDir, filepath.Base(name))

	// Read plan before deleting to find assembly reference for cleanup
	var assemblyFile string
	if s.AssembliesDir != "" {
		if planData, readErr := os.ReadFile(path); readErr == nil {
			var plan models.PlanFile
			if yamlErr := yaml.Unmarshal(planData, &plan); yamlErr == nil && plan.Assembly != "" {
				assemblyFile = plan.Assembly
			}
		}
	}

	if err := os.Remove(path); err != nil {
		if os.IsNotExist(err) {
			http.Error(w, "plan not found", http.StatusNotFound)
			return
		}
		http.Error(w, fmt.Sprintf("failed to delete plan: %v", err), http.StatusInternalServerError)
		return
	}

	// Only delete assembly PDF if no other plan references it
	if assemblyFile != "" {
		if refs := s.plansReferencingAssembly(assemblyFile); len(refs) == 0 {
			_ = os.Remove(filepath.Join(s.AssembliesDir, filepath.Base(assemblyFile)))
		}
	}

	w.WriteHeader(http.StatusNoContent)
}

func (s *PlanServer) handlePausePlan(w http.ResponseWriter, r *http.Request) {
	if s.PauseDir == "" {
		http.Error(w, "pause directory not configured", http.StatusBadRequest)
		return
	}

	name := r.PathValue("name")
	if name == "" {
		http.Error(w, "plan name required", http.StatusBadRequest)
		return
	}

	src := filepath.Join(s.PlansDir, filepath.Base(name))
	if _, err := os.Stat(src); os.IsNotExist(err) {
		http.Error(w, "plan not found", http.StatusNotFound)
		return
	}

	if err := os.MkdirAll(s.PauseDir, 0755); err != nil {
		http.Error(w, fmt.Sprintf("failed to create pause directory: %v", err), http.StatusInternalServerError)
		return
	}

	dest := filepath.Join(s.PauseDir, filepath.Base(name))
	if err := os.Rename(src, dest); err != nil {
		http.Error(w, fmt.Sprintf("failed to pause plan: %v", err), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (s *PlanServer) handleResumePlan(w http.ResponseWriter, r *http.Request) {
	if s.PauseDir == "" {
		http.Error(w, "pause directory not configured", http.StatusBadRequest)
		return
	}

	name := r.PathValue("name")
	if name == "" {
		http.Error(w, "plan name required", http.StatusBadRequest)
		return
	}

	src := filepath.Join(s.PauseDir, filepath.Base(name))
	if _, err := os.Stat(src); os.IsNotExist(err) {
		http.Error(w, "plan not found in pause directory", http.StatusNotFound)
		return
	}

	dest := filepath.Join(s.PlansDir, filepath.Base(name))
	if err := os.Rename(src, dest); err != nil {
		http.Error(w, fmt.Sprintf("failed to resume plan: %v", err), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (s *PlanServer) handleArchivePlan(w http.ResponseWriter, r *http.Request) {
	if s.ArchiveDir == "" {
		http.Error(w, "archive directory not configured", http.StatusBadRequest)
		return
	}

	name := r.PathValue("name")
	if name == "" {
		http.Error(w, "plan name required", http.StatusBadRequest)
		return
	}

	src := filepath.Join(s.PlansDir, filepath.Base(name))
	if _, err := os.Stat(src); os.IsNotExist(err) {
		http.Error(w, "plan not found", http.StatusNotFound)
		return
	}

	if err := os.MkdirAll(s.ArchiveDir, 0755); err != nil {
		http.Error(w, fmt.Sprintf("failed to create archive directory: %v", err), http.StatusInternalServerError)
		return
	}

	ext := filepath.Ext(name)
	base := strings.TrimSuffix(filepath.Base(name), ext)
	timestamp := time.Now().Format("20060102150405")
	newFilename := fmt.Sprintf("%s-%s%s", base, timestamp, ext)

	dest := filepath.Join(s.ArchiveDir, newFilename)
	if err := os.Rename(src, dest); err != nil {
		http.Error(w, fmt.Sprintf("failed to archive plan: %v", err), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// archiveTimestampRe matches the -YYYYMMDDHHMMSS suffix added during archiving.
var archiveTimestampRe = regexp.MustCompile(`-\d{14}$`)

func (s *PlanServer) handleUnarchivePlan(w http.ResponseWriter, r *http.Request) {
	if s.ArchiveDir == "" {
		http.Error(w, "archive directory not configured", http.StatusBadRequest)
		return
	}

	name := r.PathValue("name")
	if name == "" {
		http.Error(w, "plan name required", http.StatusBadRequest)
		return
	}

	src := filepath.Join(s.ArchiveDir, filepath.Base(name))
	if _, err := os.Stat(src); os.IsNotExist(err) {
		http.Error(w, "plan not found in archive directory", http.StatusNotFound)
		return
	}

	// Strip the archive timestamp to restore the original filename.
	ext := filepath.Ext(name)
	base := strings.TrimSuffix(filepath.Base(name), ext)
	restored := archiveTimestampRe.ReplaceAllString(base, "") + ext

	dest := filepath.Join(s.PlansDir, restored)
	if err := os.Rename(src, dest); err != nil {
		http.Error(w, fmt.Sprintf("failed to unarchive plan: %v", err), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (s *PlanServer) handleGetConfig(w http.ResponseWriter, r *http.Request) {
	if s.ConfigDir == "" {
		http.Error(w, "config directory not configured", http.StatusBadRequest)
		return
	}

	path := filepath.Join(s.ConfigDir, "shared-config.json")
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte("{}"))
			return
		}
		http.Error(w, fmt.Sprintf("failed to read config: %v", err), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(data)
}

func (s *PlanServer) handlePutConfig(w http.ResponseWriter, r *http.Request) {
	if s.ConfigDir == "" {
		http.Error(w, "config directory not configured", http.StatusBadRequest)
		return
	}

	data, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, fmt.Sprintf("failed to read request body: %v", err), http.StatusBadRequest)
		return
	}

	// Validate that the body is valid JSON
	var obj map[string]interface{}
	if err := json.Unmarshal(data, &obj); err != nil {
		http.Error(w, fmt.Sprintf("invalid JSON: %v", err), http.StatusBadRequest)
		return
	}

	if err := os.MkdirAll(s.ConfigDir, 0755); err != nil {
		http.Error(w, fmt.Sprintf("failed to create config directory: %v", err), http.StatusInternalServerError)
		return
	}

	path := filepath.Join(s.ConfigDir, "shared-config.json")
	if err := os.WriteFile(path, data, 0644); err != nil {
		http.Error(w, fmt.Sprintf("failed to write config: %v", err), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (s *PlanServer) handlePutAssembly(w http.ResponseWriter, r *http.Request) {
	if s.AssembliesDir == "" {
		http.Error(w, "assemblies directory not configured", http.StatusBadRequest)
		return
	}

	name := r.PathValue("name")
	if name == "" {
		http.Error(w, "plan name required", http.StatusBadRequest)
		return
	}

	// Limit upload to 100MB
	r.Body = http.MaxBytesReader(w, r.Body, 100<<20)
	data, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "failed to read request body (max 100MB)", http.StatusBadRequest)
		return
	}

	// Validate PDF magic bytes
	if len(data) < 4 || string(data[:4]) != "%PDF" {
		http.Error(w, "uploaded file is not a valid PDF", http.StatusBadRequest)
		return
	}

	if err := os.MkdirAll(s.AssembliesDir, 0755); err != nil {
		http.Error(w, fmt.Sprintf("failed to create assemblies directory: %v", err), http.StatusInternalServerError)
		return
	}

	// Generate a timestamped filename to avoid conflicts across archive/reprint cycles
	base := strings.TrimSuffix(filepath.Base(name), filepath.Ext(name))
	timestamp := time.Now().Format("20060102150405")
	pdfFilename := fmt.Sprintf("%s-%s.pdf", base, timestamp)

	path := filepath.Join(s.AssembliesDir, pdfFilename)
	if err := os.WriteFile(path, data, 0644); err != nil {
		http.Error(w, fmt.Sprintf("failed to write assembly PDF: %v", err), http.StatusInternalServerError)
		return
	}

	// Return the server-side filename so the client can store it in the plan YAML
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(map[string]string{"filename": pdfFilename})
}

func (s *PlanServer) handleGetAssembly(w http.ResponseWriter, r *http.Request) {
	if s.AssembliesDir == "" {
		http.Error(w, "assemblies directory not configured", http.StatusBadRequest)
		return
	}

	name := r.PathValue("name")
	if name == "" {
		http.Error(w, "plan name required", http.StatusBadRequest)
		return
	}

	// Read the plan YAML to find which assembly file it references
	planPath := filepath.Join(s.PlansDir, filepath.Base(name))
	planData, err := os.ReadFile(planPath)
	if err != nil {
		if os.IsNotExist(err) {
			http.Error(w, "plan not found", http.StatusNotFound)
			return
		}
		http.Error(w, fmt.Sprintf("failed to read plan: %v", err), http.StatusInternalServerError)
		return
	}

	var plan models.PlanFile
	if err := yaml.Unmarshal(planData, &plan); err != nil {
		http.Error(w, fmt.Sprintf("failed to parse plan: %v", err), http.StatusInternalServerError)
		return
	}

	if plan.Assembly == "" {
		http.Error(w, "plan has no assembly", http.StatusNotFound)
		return
	}

	pdfPath := filepath.Join(s.AssembliesDir, filepath.Base(plan.Assembly))
	data, err := os.ReadFile(pdfPath)
	if err != nil {
		if os.IsNotExist(err) {
			http.Error(w, "assembly file not found", http.StatusNotFound)
			return
		}
		http.Error(w, fmt.Sprintf("failed to read assembly: %v", err), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/pdf")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", filepath.Base(plan.Assembly)))
	_, _ = w.Write(data)
}

func (s *PlanServer) handleDeleteAssembly(w http.ResponseWriter, r *http.Request) {
	if s.AssembliesDir == "" {
		http.Error(w, "assemblies directory not configured", http.StatusBadRequest)
		return
	}

	name := r.PathValue("name")
	if name == "" {
		http.Error(w, "plan name required", http.StatusBadRequest)
		return
	}

	// Read the plan YAML to find which assembly file to delete
	planPath := filepath.Join(s.PlansDir, filepath.Base(name))
	planData, err := os.ReadFile(planPath)
	if err != nil {
		if os.IsNotExist(err) {
			http.Error(w, "plan not found", http.StatusNotFound)
			return
		}
		http.Error(w, fmt.Sprintf("failed to read plan: %v", err), http.StatusInternalServerError)
		return
	}

	var plan models.PlanFile
	if err := yaml.Unmarshal(planData, &plan); err != nil {
		http.Error(w, fmt.Sprintf("failed to parse plan: %v", err), http.StatusInternalServerError)
		return
	}

	if plan.Assembly == "" {
		http.Error(w, "plan has no assembly", http.StatusNotFound)
		return
	}

	assemblyFile := plan.Assembly
	pdfPath := filepath.Join(s.AssembliesDir, filepath.Base(assemblyFile))
	_ = os.Remove(pdfPath) // best-effort delete of the file

	// Clear the assembly field in this plan and all other plans referencing the same PDF
	for _, ref := range s.plansReferencingAssembly(assemblyFile) {
		s.clearAssemblyField(ref)
	}

	// Clear the requesting plan too (already deleted from plansDir so won't appear in refs)
	plan.Assembly = ""
	updatedData, err := yaml.Marshal(plan)
	if err == nil {
		_ = os.WriteFile(planPath, updatedData, 0644)
	}

	w.WriteHeader(http.StatusNoContent)
}

func (s *PlanServer) handleCleanAssemblies(w http.ResponseWriter, r *http.Request) {
	if s.AssembliesDir == "" {
		http.Error(w, "assemblies directory not configured", http.StatusBadRequest)
		return
	}

	dryRun := r.URL.Query().Get("dry_run") == "true"

	// Collect all assembly filenames referenced by any plan
	referenced := s.allReferencedAssemblies()

	// Scan assemblies directory for files on disk
	entries, err := os.ReadDir(s.AssembliesDir)
	if err != nil {
		http.Error(w, fmt.Sprintf("failed to read assemblies directory: %v", err), http.StatusInternalServerError)
		return
	}

	var orphans []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if _, ok := referenced[e.Name()]; !ok {
			orphans = append(orphans, e.Name())
		}
	}

	if !dryRun {
		for _, name := range orphans {
			_ = os.Remove(filepath.Join(s.AssembliesDir, name))
		}
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"orphans": orphans,
		"dry_run": dryRun,
	})
}

// allReferencedAssemblies returns a set of assembly filenames referenced by any
// plan YAML across active, paused, and archived directories.
func (s *PlanServer) allReferencedAssemblies() map[string]struct{} {
	refs := map[string]struct{}{}
	for _, dir := range []string{s.PlansDir, s.PauseDir, s.ArchiveDir} {
		if dir == "" {
			continue
		}
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if e.IsDir() {
				continue
			}
			ext := strings.ToLower(filepath.Ext(e.Name()))
			if ext != ".yaml" && ext != ".yml" {
				continue
			}
			data, err := os.ReadFile(filepath.Join(dir, e.Name()))
			if err != nil {
				continue
			}
			var plan models.PlanFile
			if err := yaml.Unmarshal(data, &plan); err != nil {
				continue
			}
			if plan.Assembly != "" {
				refs[filepath.Base(plan.Assembly)] = struct{}{}
			}
		}
	}
	return refs
}

// plansReferencingAssembly returns paths of all plan YAML files (across active,
// paused, and archived directories) whose Assembly field matches the given filename.
func (s *PlanServer) plansReferencingAssembly(assemblyFile string) []string {
	var dirs []string
	for _, d := range []string{s.PlansDir, s.PauseDir, s.ArchiveDir} {
		if d != "" {
			dirs = append(dirs, d)
		}
	}

	target := filepath.Base(assemblyFile)
	var refs []string
	for _, dir := range dirs {
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if e.IsDir() {
				continue
			}
			ext := strings.ToLower(filepath.Ext(e.Name()))
			if ext != ".yaml" && ext != ".yml" {
				continue
			}
			path := filepath.Join(dir, e.Name())
			data, err := os.ReadFile(path)
			if err != nil {
				continue
			}
			var plan models.PlanFile
			if err := yaml.Unmarshal(data, &plan); err != nil {
				continue
			}
			if filepath.Base(plan.Assembly) == target {
				refs = append(refs, path)
			}
		}
	}
	return refs
}

// clearAssemblyField removes the assembly reference from a plan YAML file.
func (s *PlanServer) clearAssemblyField(planPath string) {
	data, err := os.ReadFile(planPath)
	if err != nil {
		return
	}
	var plan models.PlanFile
	if err := yaml.Unmarshal(data, &plan); err != nil {
		return
	}
	plan.Assembly = ""
	updatedData, err := yaml.Marshal(plan)
	if err != nil {
		return
	}
	_ = os.WriteFile(planPath, updatedData, 0644)
}
