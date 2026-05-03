package server

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/dstockto/fil/plan"
)

// validCauses is the closed enum of failure causes accepted by /api/v1/plan-fail.
var validCauses = map[string]struct{}{
	"bed_adhesion":    {},
	"spaghetti":       {},
	"layer_shift":     {},
	"blob_of_death":   {},
	"bad_first_layer": {},
	"warping":         {},
	"other":           {},
}

// handlePlanFail validates the request and delegates the actual fail flow to
// the configured PlanOps (a plan.LocalPlanOps in production). LocalPlanOps
// owns Spoolman deduction, history append, and notification — see plan/.
func (s *PlanServer) handlePlanFail(w http.ResponseWriter, r *http.Request) {
	var req plan.FailRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid JSON body", http.StatusBadRequest)
		return
	}
	if req.Cause == "" {
		http.Error(w, "cause is required", http.StatusBadRequest)
		return
	}
	if _, ok := validCauses[req.Cause]; !ok {
		http.Error(w, fmt.Sprintf("invalid cause %q", req.Cause), http.StatusBadRequest)
		return
	}
	if len(req.Plates) == 0 {
		http.Error(w, "plates is required", http.StatusBadRequest)
		return
	}
	if s.PlanOps == nil {
		http.Error(w, "plan ops not configured", http.StatusInternalServerError)
		return
	}
	if _, err := s.PlanOps.Fail(r.Context(), req); err != nil {
		http.Error(w, fmt.Sprintf("fail: %v", err), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
