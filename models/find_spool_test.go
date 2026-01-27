package models

import (
	"strings"
	"testing"
	"time"

	"github.com/fatih/color"
)

func TestConvertFromHex(t *testing.T) {
	tests := []struct {
		hex   string
		wantR int
		wantG int
		wantB int
	}{
		{"000000", 0, 0, 0},
		{"FFFFFF", 255, 255, 255},
		{"FF0000", 255, 0, 0},
		{"00FF00", 0, 255, 0},
		{"0000FF", 0, 0, 255},
		{"123456", 18, 52, 86},
	}

	for _, tt := range tests {
		r, g, b := convertFromHex(tt.hex)
		if r != tt.wantR || g != tt.wantG || b != tt.wantB {
			t.Errorf("convertFromHex(%q) = (%d, %d, %d), want (%d, %d, %d)", tt.hex, r, g, b, tt.wantR, tt.wantG, tt.wantB)
		}
	}
}

func TestGetColorBlock(t *testing.T) {
	// Disable color to test plain output or enable it and check for escape sequences
	color.NoColor = false

	t.Run("single color", func(t *testing.T) {
		got := GetColorBlock("FF0000", "")
		if !strings.Contains(got, "████") {
			t.Errorf("GetColorBlock should contain block characters, got %q", got)
		}
	})

	t.Run("semi-transparent", func(t *testing.T) {
		got := GetColorBlock("FF0000AA", "")
		if !strings.Contains(got, "▓▓▓▓") {
			t.Errorf("GetColorBlock for semi-transparent should contain different block characters, got %q", got)
		}
	})

	t.Run("multi-color", func(t *testing.T) {
		got := GetColorBlock("", "FF0000,00FF00")
		if !strings.Contains(got, "██") { // multi-color uses "██" for each color
			t.Errorf("GetColorBlock multi-color should contain block characters, got %q", got)
		}
	})

	t.Run("no color", func(t *testing.T) {
		color.NoColor = true
		defer func() { color.NoColor = false }()
		got := GetColorBlock("FF0000", "")
		if got != "" {
			t.Errorf("GetColorBlock should be empty when NoColor is true, got %q", got)
		}
	})
}

func TestFindSpool_String(t *testing.T) {
	color.NoColor = true
	defer func() { color.NoColor = false }()

	now := time.Now()
	spool := FindSpool{
		Id: 123,
		Filament: struct {
			Id         int       `json:"id"`
			Registered time.Time `json:"registered"`
			Name       string    `json:"name"`
			Vendor     struct {
				Id         int       `json:"id"`
				Registered time.Time `json:"registered"`
				Name       string    `json:"name"`
				Extra      struct {
				} `json:"extra"`
			} `json:"vendor"`
			Material            string  `json:"material"`
			Price               float64 `json:"price"`
			Density             float64 `json:"density"`
			Diameter            float64 `json:"diameter"`
			Weight              float64 `json:"weight"`
			SpoolWeight         float64 `json:"spool_weight"`
			ColorHex            string  `json:"color_hex"`
			MultiColorHexes     string  `json:"multi_color_hexes"`
			MultiColorDirection string  `json:"multi_color_direction"`
			Extra               struct {
			} `json:"extra"`
		}{
			Name: "PLA Basic",
			Vendor: struct {
				Id         int       `json:"id"`
				Registered time.Time `json:"registered"`
				Name       string    `json:"name"`
				Extra      struct {
				} `json:"extra"`
			}{Name: "Bambu Lab"},
			Material: "PLA",
			Diameter: 1.75,
			ColorHex: "FFFFFF",
		},
		RemainingWeight: 500.5,
		Location:        "Shelf A",
		LastUsed:        now.Add(-2 * 24 * time.Hour),
		Archived:        false,
	}

	got := spool.String()
	// Example: "Shelf A - #123 Bambu Lab PLA Basic (PLA #FFFFFF) - 500.5g remaining, last used 2 days ago"
	expectedParts := []string{"Shelf A", "#123", "Bambu Lab", "PLA Basic", "500.5g remaining", "2 days ago"}
	for _, part := range expectedParts {
		if !strings.Contains(got, part) {
			t.Errorf("spool.String() missing expected part %q, got %q", part, got)
		}
	}

	t.Run("archived", func(t *testing.T) {
		spool.Archived = true
		gotArchived := spool.String()
		if !strings.Contains(gotArchived, "(archived)") {
			t.Errorf("spool.String() missing (archived), got %q", gotArchived)
		}
	})

	t.Run("diameter 2.85", func(t *testing.T) {
		spool.Filament.Diameter = 2.85
		gotDiameter := spool.String()
		if !strings.Contains(gotDiameter, "2.85mm") {
			t.Errorf("spool.String() missing 2.85mm, got %q", gotDiameter)
		}
	})
}
