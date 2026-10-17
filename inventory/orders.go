// Package inventory holds the spool-placement rules shared by the fil CLI and
// fil serve: how Spoolman's locations_spoolorders setting changes when a
// spool moves, and how a printer slot maps to a printer tray.
package inventory

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/dstockto/fil/api"
)

// EmptySlot is the sentinel value used in locations_spoolorders to represent
// an unoccupied slot in a printer location (AMS unit, etc.).
const EmptySlot = -1

// Printer is one configured printer and the locations it feeds from, in
// order (the index is the AMS unit for Bambu printers).
type Printer struct {
	Name      string
	Type      string // "bambu", "prusa", or "" when unconfigured
	Locations []string
}

// Layout is the location configuration the placement rules need. It is built
// from fil's config by each caller.
type Layout struct {
	Printers []Printer
	Capacity map[string]int // location -> slot count
}

// IsPrinterLocation reports whether location belongs to any printer.
func (l Layout) IsPrinterLocation(location string) bool {
	for _, p := range l.Printers {
		for _, loc := range p.Locations {
			if loc == location {
				return true
			}
		}
	}
	return false
}

// PadToCapacity ensures a printer location's spool order array is at least
// as long as its configured capacity, padding with EmptySlot (-1) sentinels.
// Non-printer locations and locations without capacity config are returned as-is.
func (l Layout) PadToCapacity(location string, ids []int) []int {
	if !l.IsPrinterLocation(location) {
		return ids
	}
	capacity, ok := l.Capacity[location]
	if !ok {
		return ids
	}
	for len(ids) < capacity {
		ids = append(ids, EmptySlot)
	}
	return ids
}

// RemoveFromAllOrders removes id from every Location list to avoid duplicates.
// For printer locations, the id is replaced with EmptySlot (-1) to preserve
// positional slot tracking. For non-printer locations, the id is removed and
// the array collapses.
func (l Layout) RemoveFromAllOrders(orders map[string][]int, id int) map[string][]int {
	for loc, ids := range orders {
		if l.IsPrinterLocation(loc) {
			for i, v := range ids {
				if v == id {
					ids[i] = EmptySlot
				}
			}
			orders[loc] = ids
		} else {
			kept := make([]int, 0, len(ids))
			for _, v := range ids {
				if v != id {
					kept = append(kept, v)
				}
			}
			orders[loc] = kept
		}
	}
	return orders
}

// Place puts id into dest's order list, which must already have id removed
// from every location (RemoveFromAllOrders). pos is a 1-based slot; 0 means
// no slot was requested. It returns the spool id displaced from an occupied
// printer slot, or 0.
//
// Printer locations: a requested slot is taken even when occupied — the
// occupant is appended after the last slot instead of being moved out, so a
// later plan complete can still charge the print to both spools (a spool that
// runs out mid-print is replaced without being unloaded). Without a slot the
// spool takes the first empty slot, or is appended past capacity.
//
// Other locations are plain ordered lists: insert at pos (clamped) or append.
func (l Layout) Place(orders map[string][]int, id int, dest string, pos int) (displaced int) {
	list := orders[dest]
	if l.IsPrinterLocation(dest) {
		switch {
		case pos > 0:
			idx := pos - 1
			for len(list) <= idx {
				list = append(list, EmptySlot)
			}
			if occupant := list[idx]; occupant != EmptySlot {
				displaced = occupant
				list = append(list, occupant)
			}
			list[idx] = id
		default:
			if i := FirstEmptySlot(list); i >= 0 {
				list[i] = id
			} else {
				list = append(list, id)
			}
		}
	} else {
		if pos > 0 {
			list = InsertAt(list, min(pos, len(list)+1)-1, id)
		} else {
			list = append(list, id)
		}
	}
	orders[dest] = list
	return displaced
}

// TrayMapping describes how a location+slot maps to a printer's tray.
type TrayMapping struct {
	PrinterName string
	PrinterType string
	AmsID       int // index of the location within the printer's locations list
	TrayID      int // 0-based slot index within the location
}

// SupportsTrayPush reports whether this printer type accepts pushed tray
// metadata updates (filament color/type/etc). Currently only Bambu printers
// support this; Prusa and any unknown types do not. Update this single
// function when adding a new printer type that supports tray pushes.
func (m *TrayMapping) SupportsTrayPush() bool {
	if m == nil {
		return false
	}
	return m.PrinterType == "bambu"
}

// MapLocationToTray maps a location name and 1-based slot position to a
// printer tray. Returns nil if the location is not a printer location or the
// printer has no type configured.
func (l Layout) MapLocationToTray(location string, slotPos int) *TrayMapping {
	for _, p := range l.Printers {
		if p.Type == "" {
			continue
		}
		for locIdx, loc := range p.Locations {
			if loc == location {
				return &TrayMapping{PrinterName: p.Name, PrinterType: p.Type, AmsID: locIdx, TrayID: slotPos - 1}
			}
		}
	}
	return nil
}

// TrayFor returns the tray to push filament info to after a spool lands in
// location at slotPos, or nil when there is none: not a printer location, a
// printer that doesn't accept tray pushes, or a slot past the location's
// capacity (a spool appended to a full AMS without a slot is in no tray).
func (l Layout) TrayFor(location string, slotPos int) *TrayMapping {
	if slotPos < 1 {
		return nil
	}
	if capacity, ok := l.Capacity[location]; ok && slotPos > capacity {
		return nil
	}
	m := l.MapLocationToTray(location, slotPos)
	if !m.SupportsTrayPush() {
		return nil
	}
	return m
}

// InsertAt inserts val at index i (0-based) into slice s, shifting elements to the right.
func InsertAt(s []int, i int, val int) []int {
	if i < 0 {
		i = 0
	}
	if i > len(s) {
		i = len(s)
	}
	s = append(s, 0)
	copy(s[i+1:], s[i:])
	s[i] = val
	return s
}

// FirstEmptySlot returns the 0-based index of the first EmptySlot sentinel in
// the slice, or -1 if there are no empty slots.
func FirstEmptySlot(ids []int) int {
	for i, id := range ids {
		if id == EmptySlot {
			return i
		}
	}
	return -1
}

// CountSpools returns the number of actual spool IDs in a location order list,
// excluding EmptySlot sentinels.
func CountSpools(ids []int) int {
	count := 0
	for _, id := range ids {
		if id != EmptySlot {
			count++
		}
	}
	return count
}

// SettingsReader is the Spoolman call LoadLocationOrders needs.
type SettingsReader interface {
	GetSettings(ctx context.Context) (map[string]api.SettingEntry, error)
}

// LoadLocationOrders reads the settings entry 'locations_spoolorders' and
// returns it as a map[Location][]spoolID. If not set or empty, returns an
// empty map.
func LoadLocationOrders(ctx context.Context, sm SettingsReader) (map[string][]int, error) {
	settings, err := sm.GetSettings(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch settings: %w", err)
	}
	entry, ok := settings["locations_spoolorders"]
	if !ok {
		return map[string][]int{}, nil
	}

	var rawString string
	if err := json.Unmarshal(entry.Value, &rawString); err != nil {
		return nil, fmt.Errorf("failed to decode settings value wrapper: %w", err)
	}
	if rawString == "" {
		return map[string][]int{}, nil
	}
	var orders map[string][]int
	if err := json.Unmarshal([]byte(rawString), &orders); err != nil {
		return nil, fmt.Errorf("failed to parse locations_spoolorders JSON: %w", err)
	}
	if orders == nil {
		orders = map[string][]int{}
	}
	return orders, nil
}
