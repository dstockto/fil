package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/dstockto/fil/api"
	"github.com/dstockto/fil/inventory"
)

// SpoolmanAPI is the Spoolman access the spool endpoints need: moves and
// archives. *api.Client satisfies it.
type SpoolmanAPI interface {
	inventory.Spoolman
	inventory.Archiver
}

// archiveResponse reports a completed archive.
type archiveResponse struct {
	SpoolID int    `json:"spool_id"`
	From    string `json:"from"` // location the spool was in, "" if none
	// RemainingG is what Spoolman showed left on the spool when it was
	// archived; archiving doesn't change it.
	RemainingG float64 `json:"remaining_g"`
}

// handleArchiveSpool archives one spool the way `fil archive <id>` does:
// removed from every location's order list (trimming an emptied overflow
// position), then marked archived with no location. It doesn't refuse a
// spool that still has filament — the CLI's confirmation is the caller's job.
func (s *PlanServer) handleArchiveSpool(w http.ResponseWriter, r *http.Request) {
	if s.Spoolman == nil {
		http.Error(w, "spoolman not configured", http.StatusServiceUnavailable)
		return
	}
	id, err := strconv.Atoi(r.PathValue("id"))
	if err != nil || id <= 0 {
		http.Error(w, "invalid spool id", http.StatusBadRequest)
		return
	}

	// Same lock and WithoutCancel as moves: both rewrite locations_spoolorders.
	s.moveMu.Lock()
	spool, err := inventory.Archive(context.WithoutCancel(r.Context()), s.Spoolman, s.Layout, id)
	s.moveMu.Unlock()
	switch {
	case errors.Is(err, inventory.ErrBadRequest):
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	case errors.Is(err, api.ErrSpoolNotFound):
		http.Error(w, "spool not found", http.StatusNotFound)
		return
	case err != nil:
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(archiveResponse{SpoolID: id, From: spool.Location, RemainingG: spool.RemainingWeight})
}

var _ SpoolmanAPI = (*api.Client)(nil)
