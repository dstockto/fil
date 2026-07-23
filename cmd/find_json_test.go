package cmd

import (
	"encoding/json"
	"testing"

	"github.com/dstockto/fil/models"
)

// newExportSpool builds a FindSpool with the nested filament fields set,
// without having to spell out the anonymous Filament struct literal.
//
// location is the RAW Spoolman location ("AMS B"), never the "AMS B:4" label
// the text renderer prints — Spoolman has no slot model, so a fixture carrying
// a slot suffix would be asserting a shape the API cannot produce.
func newExportSpool(name, vendor, material, hex, location string, remaining float64) models.FindSpool {
	var s models.FindSpool
	s.Filament.Name = name
	s.Filament.Vendor.Name = vendor
	s.Filament.Material = material
	s.Filament.ColorHex = hex
	s.Location = location
	s.RemainingWeight = remaining
	return s
}

func TestToExport(t *testing.T) {
	tests := []struct {
		name string
		in   models.FindSpool
		slot int
		want spoolExport
	}{
		{
			"hex without # gets normalized",
			newExportSpool("PolyTerra™ Cotton White", "Polymaker", "Matte PLA", "e6dddb", "AMS B", 419.9),
			4,
			spoolExport{Name: "PolyTerra™ Cotton White", Vendor: "Polymaker", Material: "Matte PLA", ColorHex: "#e6dddb", Location: "AMS B", Slot: 4, RemainingG: 419.9},
		},
		{
			"hex with # is left alone",
			newExportSpool("Some Red", "Acme", "PLA", "#FF0000", "Shelf 1", 100),
			0,
			spoolExport{Name: "Some Red", Vendor: "Acme", Material: "PLA", ColorHex: "#FF0000", Location: "Shelf 1", RemainingG: 100},
		},
		{
			"empty hex stays empty",
			newExportSpool("Mystery", "Acme", "PLA", "", "Shelf 2", 0),
			0,
			spoolExport{Name: "Mystery", Vendor: "Acme", Material: "PLA", ColorHex: "", Location: "Shelf 2", RemainingG: 0},
		},
		{
			"control chars stripped from location",
			newExportSpool("X", "Y", "PLA", "abcdef", "AMS\x1b A", 5),
			0,
			spoolExport{Name: "X", Vendor: "Y", Material: "PLA", ColorHex: "#abcdef", Location: "AMS A", RemainingG: 5},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := toExport(tt.in, tt.slot)
			if got != tt.want {
				t.Errorf("toExport() = %+v; want %+v", got, tt.want)
			}
		})
	}
}

// TestSpoolExportJSONShape locks the JSON field names so downstream consumers
// (e.g. read3mf -make-mapping) don't break on a rename.
func TestSpoolExportJSONShape(t *testing.T) {
	b, err := json.Marshal(spoolExport{
		ID: 262, Name: "PolyTerra™ Cotton White", Vendor: "Polymaker",
		Material: "Matte PLA", ColorHex: "#e6dddb", Location: "AMS B", Slot: 4, RemainingG: 419.9,
	})
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"id", "name", "vendor", "material", "color_hex", "location", "slot", "remaining_g"} {
		if _, ok := m[key]; !ok {
			t.Errorf("expected JSON key %q missing; got %v", key, m)
		}
	}
	if m["color_hex"] != "#e6dddb" {
		t.Errorf("color_hex = %v; want #e6dddb", m["color_hex"])
	}
	if m["location"] != "AMS B" {
		t.Errorf("location = %v; want the raw Spoolman value AMS B", m["location"])
	}
}

// TestSpoolExportOmitsSlotOutsidePrinters pins the omitempty on Slot: a spool on
// a shelf has no slot concept, and emitting "slot": 0 would invite a consumer to
// treat 0 as a real slot number when slots are 1-based.
func TestSpoolExportOmitsSlotOutsidePrinters(t *testing.T) {
	b, err := json.Marshal(spoolExport{ID: 1, Location: "Shelf 1", RemainingG: 100})
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	if _, ok := m["slot"]; ok {
		t.Errorf("slot should be omitted for a non-printer location; got %v", m)
	}
}

// TestBuildSlotIndex covers the derivation itself, including the EmptySlot
// sentinel and the requirement that non-printer locations contribute nothing.
func TestBuildSlotIndex(t *testing.T) {
	oldCfg := Cfg
	t.Cleanup(func() { Cfg = oldCfg })
	Cfg = &Config{Printers: map[string]PrinterConfig{
		"bambu": {Locations: []string{"AMS A", "AMS B"}},
	}}

	got := buildSlotIndex(map[string][]int{
		"AMS A":   {10, EmptySlot, 12},
		"AMS B":   {EmptySlot, EmptySlot, EmptySlot, 262},
		"Shelf 1": {99, 98},
	})

	want := map[int]int{10: 1, 12: 3, 262: 4}
	for id, slot := range want {
		if got.slotOf(locOf(id), id) != slot {
			t.Errorf("spool %d slot = %d; want %d", id, got.slotOf(locOf(id), id), slot)
		}
	}
	// Shelf 1 is not a printer location, so its occupants must not get slots.
	for _, id := range []int{99, 98} {
		if s := got.slotOf("Shelf 1", id); s != 0 {
			t.Errorf("spool %d is on a shelf and must not have a slot; got %d", id, s)
		}
	}
}

// locOf maps the TestBuildSlotIndex fixture IDs back to their location.
func locOf(id int) string {
	if id == 262 {
		return "AMS B"
	}
	return "AMS A"
}

// TestBuildSlotIndexIgnoresDriftedEntries covers the locations_spoolorders drift
// Spoolman is known for: it does not reliably remove a spool's ID from its old
// location's list when the spool moves. A global ID -> slot map would let the
// stale entry win at random, because Go randomizes map iteration order. The slot
// must come from the list belonging to the spool's ACTUAL location, which is the
// same cross-check the text renderer performs.
func TestBuildSlotIndexIgnoresDriftedEntries(t *testing.T) {
	oldCfg := Cfg
	t.Cleanup(func() { Cfg = oldCfg })
	Cfg = &Config{Printers: map[string]PrinterConfig{
		"bambu": {Locations: []string{"AMS A", "AMS B"}},
	}}

	// Spool 262 really lives in AMS B slot 4, but a stale entry still lists it
	// as AMS A slot 1.
	idx := buildSlotIndex(map[string][]int{
		"AMS A": {262, EmptySlot},
		"AMS B": {EmptySlot, EmptySlot, EmptySlot, 262},
	})

	// Run repeatedly: a map-iteration-order dependency shows up as flakiness,
	// so a single call proves little.
	for i := 0; i < 50; i++ {
		if got := idx.slotOf("AMS B", 262); got != 4 {
			t.Fatalf("iteration %d: slot for the real location = %d; want 4", i, got)
		}
		if got := idx.slotOf("AMS A", 262); got != 1 {
			t.Fatalf("iteration %d: stale list should still report its own position, got %d", i, got)
		}
	}
}

// TestBuildSlotIndexDuplicateWithinOneList pins the tie-break when one list
// names the same spool twice — also drift, and also a place where iteration
// order must not decide the answer.
func TestBuildSlotIndexDuplicateWithinOneList(t *testing.T) {
	oldCfg := Cfg
	t.Cleanup(func() { Cfg = oldCfg })
	Cfg = &Config{Printers: map[string]PrinterConfig{"bambu": {Locations: []string{"AMS A"}}}}

	idx := buildSlotIndex(map[string][]int{"AMS A": {7, EmptySlot, 7}})
	if got := idx.slotOf("AMS A", 7); got != 1 {
		t.Errorf("slot = %d; want 1 (first position wins, deterministically)", got)
	}
}
