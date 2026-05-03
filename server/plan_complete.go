package server

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/dstockto/fil/plan"
)

// handlePlanComplete decodes a CompleteRequest from the body, fills in the
// plan name from the URL path, and delegates to PlanOps.Complete. The actual
// YAML mutation, Spoolman deduction, history append, and notification all
// live in plan.LocalPlanOps.
func (s *PlanServer) handlePlanComplete(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if name == "" {
		http.Error(w, "plan name is required", http.StatusBadRequest)
		return
	}

	var req plan.CompleteRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid JSON body", http.StatusBadRequest)
		return
	}
	// Trust the path, not the body — name in the URL wins so a body
	// containing a stale or wrong plan can't accidentally hit a different
	// file on disk.
	req.Plan = name

	if req.Project == "" || req.Plate == "" {
		http.Error(w, "project and plate are required", http.StatusBadRequest)
		return
	}
	if s.PlanOps == nil {
		http.Error(w, "plan ops not configured", http.StatusInternalServerError)
		return
	}

	result, err := s.PlanOps.Complete(r.Context(), req)
	if err != nil {
		http.Error(w, fmt.Sprintf("complete: %v", err), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(result)
}
