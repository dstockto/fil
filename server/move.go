package server

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/dstockto/fil/api"
	"github.com/dstockto/fil/inventory"
)

// moveRequest is the body of POST /spools/{id}/move.
type moveRequest struct {
	Location string `json:"location"`
	Slot     int    `json:"slot,omitempty"` // 1-based; omit or 0 for none
}

// moveResponse reports a completed move.
type moveResponse struct {
	SpoolID   int    `json:"spool_id"`
	From      string `json:"from"`
	To        string `json:"to"`
	Slot      int    `json:"slot,omitempty"`
	Displaced int    `json:"displaced,omitempty"` // spool bumped from the requested printer slot
	// TrayPushed is true when the destination printer's tray was updated;
	// TrayError says why a push that applied failed. The move itself stands.
	TrayPushed bool   `json:"tray_pushed,omitempty"`
	TrayError  string `json:"tray_error,omitempty"`
}

// handleMoveSpool moves one spool with the same rules as `fil move`, then
// pushes tray info when the destination is a Bambu AMS slot (what the CLI does
// through POST /printers/{name}/push-tray).
func (s *PlanServer) handleMoveSpool(w http.ResponseWriter, r *http.Request) {
	if s.Spoolman == nil {
		http.Error(w, "spoolman not configured", http.StatusServiceUnavailable)
		return
	}
	id, err := strconv.Atoi(r.PathValue("id"))
	if err != nil || id <= 0 {
		http.Error(w, "invalid spool id", http.StatusBadRequest)
		return
	}
	var body moveRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid JSON body", http.StatusBadRequest)
		return
	}

	// Moves rewrite the whole locations_spoolorders setting; serialize ours.
	s.moveMu.Lock()
	res, err := inventory.Move(r.Context(), s.Spoolman, s.Layout, inventory.MoveRequest{SpoolID: id, Location: body.Location, Slot: body.Slot})
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

	resp := moveResponse{SpoolID: id, From: res.From, To: res.To, Slot: res.Slot, Displaced: res.Displaced}
	if mapping := s.Layout.TrayFor(res.To, res.Slot); mapping != nil && s.Printers != nil {
		tray := inventory.TrayPushFor(res.Spool)
		err := s.Printers.PushTray(mapping.PrinterName, TrayUpdate{
			AmsID: mapping.AmsID, TrayID: mapping.TrayID,
			Color: tray.Color, Type: tray.Type, TempMin: tray.TempMin, TempMax: tray.TempMax, InfoIdx: tray.InfoIdx,
		})
		if err != nil {
			resp.TrayError = err.Error()
		} else {
			resp.TrayPushed = true
		}
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}
