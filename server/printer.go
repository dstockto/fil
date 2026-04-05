package server

import "time"

// PrinterState represents the current state of a printer.
type PrinterState struct {
	Name          string     `json:"name"`
	Type          string     `json:"type"`                     // "bambu" or "prusa"
	State         string     `json:"state"`                    // "idle", "printing", "paused", "finished", "failed", "offline"
	Progress      int        `json:"progress,omitempty"`       // 0-100
	RemainingMins int        `json:"remaining_mins,omitempty"` // minutes remaining
	CurrentFile   string     `json:"current_file,omitempty"`
	Layer         int        `json:"layer,omitempty"`
	TotalLayers   int        `json:"total_layers,omitempty"`
	ActiveTray    int        `json:"active_tray,omitempty"`    // active AMS tray index (-1 if unknown)
	LastUpdated   time.Time  `json:"last_updated"`
	Trays         []TrayInfo `json:"trays,omitempty"`
}

// TrayInfo represents the state of a single filament tray/slot as reported by the printer.
type TrayInfo struct {
	AmsID        int    `json:"ams_id"`
	TrayID       int    `json:"tray_id"`
	Color        string `json:"color,omitempty"`         // hex RRGGBB (no alpha)
	Type         string `json:"type,omitempty"`          // e.g. "PLA", "Matte PLA"
	TempMin      int    `json:"temp_min,omitempty"`
	TempMax      int    `json:"temp_max,omitempty"`
	InfoIdx      string `json:"info_idx,omitempty"`      // Bambu filament profile ID
}

// TrayUpdate contains the fields to push to a printer tray.
type TrayUpdate struct {
	AmsID   int    `json:"ams_id"`
	TrayID  int    `json:"tray_id"`
	Color   string `json:"color"`    // hex RRGGBBAA
	Type    string `json:"type"`     // e.g. "PLA", "Matte PLA"
	TempMin int    `json:"temp_min"`
	TempMax int    `json:"temp_max"`
}

// HMSCode represents a Bambu Health Management System code.
type HMSCode struct {
	Attr int `json:"attr"`
	Code int `json:"code"`
}

// StateChangeEvent carries context about a printer state transition.
type StateChangeEvent struct {
	OldState     string
	NewState     string
	HMSCodes     []HMSCode // current HMS codes at time of change
	PrevHMSCodes []HMSCode // HMS codes before the change
}

// IsLikelyUserPause returns true if the pause appears to be user-initiated
// (no new HMS codes appeared with the state change).
func (e StateChangeEvent) IsLikelyUserPause() bool {
	if e.NewState != "paused" {
		return false
	}
	// If new HMS codes appeared that weren't in the previous set, it's printer-caused
	prevSet := make(map[int]bool)
	for _, h := range e.PrevHMSCodes {
		prevSet[h.Code] = true
	}
	for _, h := range e.HMSCodes {
		if !prevSet[h.Code] {
			return false // new HMS code appeared — printer-caused
		}
	}
	return true
}

// PrinterAdapter defines the interface for communicating with a printer.
// Each printer type (Bambu, Prusa) implements this interface.
type PrinterAdapter interface {
	// Connect establishes a connection to the printer.
	Connect() error

	// Close cleanly shuts down the connection.
	Close() error

	// Status returns the current printer state.
	Status() PrinterState

	// PushTray updates the filament metadata for a specific tray.
	// Returns an error if the printer doesn't support writes (e.g. Prusa).
	PushTray(update TrayUpdate) error

	// OnStateChange registers a callback for state transitions.
	OnStateChange(func(event StateChangeEvent))
}
