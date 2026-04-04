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
	AmsID   int
	TrayID  int
	Color   string // hex RRGGBBAA
	Type    string // e.g. "PLA", "Matte PLA"
	TempMin int
	TempMax int
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
	// The callback receives the old and new state strings.
	OnStateChange(func(oldState, newState string))
}
