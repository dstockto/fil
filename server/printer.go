package server

import (
	"fmt"
	"time"
)

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

// IsLikelyUserPause returns true if the pause appears to be user-initiated.
// If any HMS codes are present at all, it's likely printer-caused.
func (e StateChangeEvent) IsLikelyUserPause() bool {
	if e.NewState != "paused" {
		return false
	}
	return len(e.HMSCodes) == 0
}

// HMSCodeString returns a human-readable hex representation of an HMS code,
// formatted to match the Bambu wiki convention (e.g. 0C00-0300-0003-0008).
func (h HMSCode) HMSCodeString() string {
	return fmt.Sprintf("%04X-%04X-%04X-%04X",
		(h.Attr>>16)&0xFFFF, h.Attr&0xFFFF,
		(h.Code>>16)&0xFFFF, h.Code&0xFFFF)
}

// hmsDescriptions maps known HMS codes to human-friendly descriptions.
// Verified against https://wiki.bambulab.com/en/hms/home
// Add new codes as they are encountered in practice.
var hmsDescriptions = map[string]string{
	// Spaghetti / First layer
	"0C00-0300-0003-0007": "possible first layer defects",
	"0C00-0300-0003-0008": "possible spaghetti defects",

	// Lidar
	"0C00-0100-0001-0004": "micro lidar lens dirty",

	// Toolhead
	"0300-1200-0002-0001": "toolhead front cover fell off",

	// Build plate
	"0300-0D00-0001-0003": "build plate not properly placed",

	// AMS filament ran out (07XX-7000-0002-0007, XX = AMS number)
	"0700-7000-0002-0007": "AMS 1 filament ran out",
	"0701-7000-0002-0007": "AMS 2 filament ran out",
	"0702-7000-0002-0007": "AMS 3 filament ran out",
	"0703-7000-0002-0007": "AMS 4 filament ran out",
}

// HMSDescription returns a human-friendly description for an HMS code,
// or an empty string if the code is not recognized.
func (h HMSCode) HMSDescription() string {
	return hmsDescriptions[h.HMSCodeString()]
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
