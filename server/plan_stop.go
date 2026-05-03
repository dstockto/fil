package server

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/dstockto/fil/plan"
)

func (s *PlanServer) handlePlanStop(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if name == "" {
		http.Error(w, "plan name is required", http.StatusBadRequest)
		return
	}

	var req plan.StopRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid JSON body", http.StatusBadRequest)
		return
	}
	req.Plan = name

	if req.Project == "" || req.Plate == "" {
		http.Error(w, "project and plate are required", http.StatusBadRequest)
		return
	}
	if s.PlanOps == nil {
		http.Error(w, "plan ops not configured", http.StatusInternalServerError)
		return
	}
	if err := s.PlanOps.Stop(r.Context(), req); err != nil {
		http.Error(w, fmt.Sprintf("stop: %v", err), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
