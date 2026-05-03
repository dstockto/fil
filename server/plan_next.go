package server

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/dstockto/fil/plan"
)

// handlePlanNext decodes a NextRequest from the body, fills in the plan name
// from the URL path, and delegates to PlanOps.Next.
func (s *PlanServer) handlePlanNext(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if name == "" {
		http.Error(w, "plan name is required", http.StatusBadRequest)
		return
	}

	var req plan.NextRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid JSON body", http.StatusBadRequest)
		return
	}
	req.Plan = name

	if req.Project == "" || req.Plate == "" || req.Printer == "" {
		http.Error(w, "project, plate, and printer are required", http.StatusBadRequest)
		return
	}
	if s.PlanOps == nil {
		http.Error(w, "plan ops not configured", http.StatusInternalServerError)
		return
	}

	result, err := s.PlanOps.Next(r.Context(), req)
	if err != nil {
		http.Error(w, fmt.Sprintf("next: %v", err), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(result)
}
