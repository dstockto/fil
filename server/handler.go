package server

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/dstockto/fil/models"
	"gopkg.in/yaml.v3"
)

const versionHeader = "X-Fil-Version"

// PlanServer handles HTTP requests for plan CRUD and lifecycle operations.
type PlanServer struct {
	PlansDir   string
	PauseDir   string
	ArchiveDir string
	ConfigDir  string
	Version    string
}

// PlanSummary is the JSON representation returned by the list endpoint.
type PlanSummary struct {
	Name       string `json:"name"`
	Projects   int    `json:"projects"`
	PlatesTodo int    `json:"plates_todo"`
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
	mux.HandleFunc("GET /api/v1/config", s.handleGetConfig)
	mux.HandleFunc("PUT /api/v1/config", s.handlePutConfig)
	mux.HandleFunc("GET /api/v1/version", s.handleVersion)
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

func (s *PlanServer) handleVersion(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"version": s.Version})
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
			Name:       e.Name(),
			Projects:   len(plan.Projects),
			PlatesTodo: platesTodo,
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
	if err := os.WriteFile(path, data, 0644); err != nil {
		http.Error(w, fmt.Sprintf("failed to write plan: %v", err), http.StatusInternalServerError)
		return
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
	if err := os.Remove(path); err != nil {
		if os.IsNotExist(err) {
			http.Error(w, "plan not found", http.StatusNotFound)
			return
		}
		http.Error(w, fmt.Sprintf("failed to delete plan: %v", err), http.StatusInternalServerError)
		return
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
