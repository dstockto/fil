package cmd

import (
	"encoding/json"
	"testing"

	"github.com/dstockto/fil/models"
)

// newExportSpool builds a FindSpool with the nested filament fields set,
// without having to spell out the anonymous Filament struct literal.
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
		want spoolExport
	}{
		{
			"hex without # gets normalized",
			newExportSpool("PolyTerra™ Cotton White", "Polymaker", "Matte PLA", "e6dddb", "AMS B:4", 419.9),
			spoolExport{Name: "PolyTerra™ Cotton White", Vendor: "Polymaker", Material: "Matte PLA", ColorHex: "#e6dddb", Location: "AMS B:4", RemainingG: 419.9},
		},
		{
			"hex with # is left alone",
			newExportSpool("Some Red", "Acme", "PLA", "#FF0000", "Shelf 1", 100),
			spoolExport{Name: "Some Red", Vendor: "Acme", Material: "PLA", ColorHex: "#FF0000", Location: "Shelf 1", RemainingG: 100},
		},
		{
			"empty hex stays empty",
			newExportSpool("Mystery", "Acme", "PLA", "", "Shelf 2", 0),
			spoolExport{Name: "Mystery", Vendor: "Acme", Material: "PLA", ColorHex: "", Location: "Shelf 2", RemainingG: 0},
		},
		{
			"control chars stripped from location",
			newExportSpool("X", "Y", "PLA", "abcdef", "AMS\x1b A", 5),
			spoolExport{Name: "X", Vendor: "Y", Material: "PLA", ColorHex: "#abcdef", Location: "AMS A", RemainingG: 5},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := toExport(tt.in)
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
		Material: "Matte PLA", ColorHex: "#e6dddb", Location: "AMS B:4", RemainingG: 419.9,
	})
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"id", "name", "vendor", "material", "color_hex", "location", "remaining_g"} {
		if _, ok := m[key]; !ok {
			t.Errorf("expected JSON key %q missing; got %v", key, m)
		}
	}
	if m["color_hex"] != "#e6dddb" {
		t.Errorf("color_hex = %v; want #e6dddb", m["color_hex"])
	}
}
