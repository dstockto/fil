package server

import (
	"encoding/json"
	"net/http"
	"time"
)

// NotifyTestResult reports the outcome of a test notification request per channel.
// Used by `fil notify test` to verify the full wiring (config → server → channels).
type NotifyTestResult struct {
	Message    string            `json:"message"`
	QuietHours bool              `json:"quiet_hours"`
	Forced     bool              `json:"forced"`
	Channels   map[string]string `json:"channels"` // channel name -> "sent" | "skipped: <reason>" | "error: <msg>"
}

type notifyTestRequest struct {
	Message string `json:"message,omitempty"`
	Force   bool   `json:"force,omitempty"`
}

func (s *PlanServer) handleNotifyTest(w http.ResponseWriter, r *http.Request) {
	var req notifyTestRequest
	if r.ContentLength > 0 {
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid json: "+err.Error(), http.StatusBadRequest)
			return
		}
	}

	message := req.Message
	if message == "" {
		message = "Fil notification test"
	}

	result := NotifyTestResult{
		Message:  message,
		Forced:   req.Force,
		Channels: map[string]string{},
	}

	if s.Notifier == nil || !s.Notifier.Enabled() {
		result.Channels["_"] = "skipped: no channels configured"
		writeJSON(w, result)
		return
	}

	result.QuietHours = s.Notifier.IsQuietHours(time.Now())
	if result.QuietHours && !req.Force {
		result.Channels["_"] = "skipped: quiet hours (use force=true to override)"
		writeJSON(w, result)
		return
	}

	s.Notifier.TestAll(message, result.Channels)

	writeJSON(w, result)
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}
