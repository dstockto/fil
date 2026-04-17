package cmd

import (
	"math"
	"testing"

	"github.com/dstockto/fil/models"
)

func TestParseHexColor(t *testing.T) {
	tests := []struct {
		name   string
		input  string
		want   bool
		expR   float64
		expG   float64
		expB   float64
	}{
		{"six-digit with hash", "#ff0000", true, 1, 0, 0},
		{"six-digit no hash", "00ff00", true, 0, 1, 0},
		{"mixed case", "#AaBbCc", true, 0xaa / 255.0, 0xbb / 255.0, 0xcc / 255.0},
		{"three-digit shorthand", "#f0a", true, 1, 0, 0xaa / 255.0},
		{"with alpha suffix", "#ff550080", true, 1, 0x55 / 255.0, 0},
		{"whitespace", "  #123456  ", true, 0x12 / 255.0, 0x34 / 255.0, 0x56 / 255.0},
		{"empty", "", false, 0, 0, 0},
		{"too short", "#f", false, 0, 0, 0},
		{"garbage", "notacolor", false, 0, 0, 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, ok := parseHexColor(tt.input)
			if ok != tt.want {
				t.Fatalf("parseHexColor(%q) ok=%v, want %v", tt.input, ok, tt.want)
			}
			if !ok {
				return
			}
			// Allow tiny float rounding
			const eps = 1e-9
			if math.Abs(c.R-tt.expR) > eps || math.Abs(c.G-tt.expG) > eps || math.Abs(c.B-tt.expB) > eps {
				t.Fatalf("parseHexColor(%q) = (%v,%v,%v), want (%v,%v,%v)", tt.input, c.R, c.G, c.B, tt.expR, tt.expG, tt.expB)
			}
		})
	}
}

func makeSpool(primary, multi string) models.FindSpool {
	var s models.FindSpool
	s.Filament.ColorHex = primary
	s.Filament.MultiColorHexes = multi
	return s
}

func TestSpoolColorDistance(t *testing.T) {
	target, ok := parseHexColor("#ff0000")
	if !ok {
		t.Fatal("failed to parse target")
	}

	t.Run("exact primary match is zero", func(t *testing.T) {
		d := spoolColorDistance(makeSpool("ff0000", ""), target)
		if d > 0.01 {
			t.Fatalf("expected ~0, got %v", d)
		}
	})

	t.Run("no color returns +Inf", func(t *testing.T) {
		d := spoolColorDistance(makeSpool("", ""), target)
		if !math.IsInf(d, 1) {
			t.Fatalf("expected +Inf, got %v", d)
		}
	})

	t.Run("unparseable color returns +Inf", func(t *testing.T) {
		d := spoolColorDistance(makeSpool("notahex", ""), target)
		if !math.IsInf(d, 1) {
			t.Fatalf("expected +Inf, got %v", d)
		}
	})

	t.Run("multi-color picks minimum", func(t *testing.T) {
		// primary is blue (far from red), multi contains red (exact match)
		d := spoolColorDistance(makeSpool("0000ff", "00ff00,ff0000"), target)
		if d > 0.01 {
			t.Fatalf("expected near-zero via multi-color match, got %v", d)
		}
	})

	t.Run("closer primary beats further multi", func(t *testing.T) {
		// primary is near-red, multi is blue — primary should win
		near := spoolColorDistance(makeSpool("ff1010", "0000ff"), target)
		far := spoolColorDistance(makeSpool("0000ff", ""), target)
		if near >= far {
			t.Fatalf("expected near-red primary (%v) to beat blue-only (%v)", near, far)
		}
	})

	t.Run("scale is 0-100 ΔE", func(t *testing.T) {
		// Known pairs from the CIEDE2000 reference scale: black vs white
		// should land in the 40s on the standard 0–100 range.
		black, _ := parseHexColor("#000000")
		white, _ := parseHexColor("#ffffff")
		d := black.DistanceCIEDE2000(white) * 100
		if d < 40 || d > 120 {
			t.Fatalf("expected black↔white ΔE in 40-120 range, got %v", d)
		}
		// An exact match must stay at ~zero regardless of scaling.
		if e := spoolColorDistance(makeSpool("ff0000", ""), target); math.Abs(e) > 1e-9 {
			t.Fatalf("expected exact match ≈ 0, got %v", e)
		}
	})

	t.Run("ordering matches color intuition", func(t *testing.T) {
		// Relative to target #ff0000: pure red < orange-red < blue.
		ff0000 := spoolColorDistance(makeSpool("ff0000", ""), target)
		ff5500 := spoolColorDistance(makeSpool("ff5500", ""), target)
		blue := spoolColorDistance(makeSpool("0000ff", ""), target)
		if !(ff0000 < ff5500 && ff5500 < blue) {
			t.Fatalf("expected 0 < %v < %v, got ordering violated", ff5500, blue)
		}
	})
}
