package server

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/dstockto/fil/plan"
)

// handlePlanResolve decodes a ResolveRequest and delegates to PlanOps.Resolve.
// The interactive disambiguation against Spoolman happens client-side; the
// request body is the resolved (Project, Plate, NeedIndex, FilamentID, Name,
// Material) tuples.
func (s *PlanServer) handlePlanResolve(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if name == "" {
		http.Error(w, "plan name is required", http.StatusBadRequest)
		return
	}

	var req plan.ResolveRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid JSON body", http.StatusBadRequest)
		return
	}
	req.Plan = name

	if s.PlanOps == nil {
		http.Error(w, "plan ops not configured", http.StatusInternalServerError)
		return
	}
	if err := s.PlanOps.Resolve(r.Context(), req); err != nil {
		http.Error(w, fmt.Sprintf("resolve: %v", err), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
