package inventory

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/dstockto/fil/api"
	"github.com/dstockto/fil/models"
)

// Spoolman is the slice of the Spoolman API a move needs. *api.Client
// satisfies it.
type Spoolman interface {
	SettingsReader
	PostSettingObject(ctx context.Context, key string, obj any) error
	FindSpoolsById(ctx context.Context, id int) (*models.FindSpool, error)
	MoveSpool(ctx context.Context, spoolId int, to string) error
}

// ErrBadRequest marks a move request that can't be carried out as asked.
var ErrBadRequest = errors.New("bad move request")

// MoveRequest moves one spool. Slot is 1-based; 0 means no slot.
type MoveRequest struct {
	SpoolID  int
	Location string
	Slot     int
}

// MoveResult reports what a move did.
type MoveResult struct {
	Spool     models.FindSpool
	From      string
	To        string
	Slot      int // final 1-based position in To's order list; 0 if not listed
	Displaced int // spool bumped from the requested printer slot, or 0
}

// Move moves one spool the same way `fil move <id> <location>[:slot]` does:
// it updates locations_spoolorders first (so Spoolman's UI order reflects the
// move immediately), then the spool's location.
//
// The read-modify-write of locations_spoolorders is not atomic against other
// writers (another fil CLI, the Spoolman UI); callers in one process should
// serialize their moves.
func Move(ctx context.Context, sm Spoolman, l Layout, req MoveRequest) (MoveResult, error) {
	if req.SpoolID <= 0 {
		return MoveResult{}, fmt.Errorf("%w: spool id must be positive", ErrBadRequest)
	}
	if strings.TrimSpace(req.Location) == "" {
		return MoveResult{}, fmt.Errorf("%w: location is required", ErrBadRequest)
	}
	if req.Slot < 0 {
		return MoveResult{}, fmt.Errorf("%w: slot must be 1 or more", ErrBadRequest)
	}

	spool, err := sm.FindSpoolsById(ctx, req.SpoolID)
	if err != nil {
		return MoveResult{}, err
	}

	orders, err := LoadLocationOrders(ctx, sm)
	if err != nil {
		return MoveResult{}, err
	}
	for loc := range orders {
		orders[loc] = l.PadToCapacity(loc, orders[loc])
	}
	orders[req.Location] = l.PadToCapacity(req.Location, orders[req.Location])

	orders = l.RemoveFromAllOrders(orders, req.SpoolID)
	displaced := l.Place(orders, req.SpoolID, req.Location, req.Slot)

	if err := sm.PostSettingObject(ctx, "locations_spoolorders", orders); err != nil {
		return MoveResult{}, fmt.Errorf("failed to update locations_spoolorders: %w", err)
	}
	if err := sm.MoveSpool(ctx, req.SpoolID, req.Location); err != nil {
		return MoveResult{}, fmt.Errorf("error moving spool #%d: %w", req.SpoolID, err)
	}

	slot := 0
	for i, id := range orders[req.Location] {
		if id == req.SpoolID {
			slot = i + 1
			break
		}
	}
	return MoveResult{Spool: *spool, From: spool.Location, To: req.Location, Slot: slot, Displaced: displaced}, nil
}

// TrayPush is the filament metadata pushed to a printer tray after a move.
type TrayPush struct {
	Color   string // RRGGBBAA, upper case
	Type    string
	TempMin int
	TempMax int
	InfoIdx string
}

// TrayPushFor builds the tray metadata for a spool, using the Bambu filament
// profile table when the spool matches one.
func TrayPushFor(spool models.FindSpool) TrayPush {
	colorHex := strings.TrimPrefix(spool.Filament.ColorHex, "#")
	if len(colorHex) == 6 {
		colorHex += "FF"
	}
	push := TrayPush{Color: strings.ToUpper(colorHex), Type: spool.Filament.Material, TempMin: 190, TempMax: 240}
	if profile := LookupFilamentProfile(spool.Filament.Vendor.Name, spool.Filament.Name, spool.Filament.Material); profile != nil {
		push.Type = profile.TrayType
		push.InfoIdx = profile.InfoIdx
	}
	return push
}

var _ Spoolman = (*api.Client)(nil)
