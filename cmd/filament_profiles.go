package cmd

import (
	"strings"
)

// FilamentProfile represents the Bambu/OrcaSlicer profile for a filament.
type FilamentProfile struct {
	InfoIdx  string // tray_info_idx (e.g. "GFL01")
	TrayType string // tray_type (e.g. "PLA", "PETG")
}

// filamentProfileRules defines matching rules ordered from most specific to least.
// Each rule has a match function and the resulting profile.
var filamentProfileRules = []struct {
	match   func(vendor, name, material string) bool
	profile FilamentProfile
}{
	// === Polymaker ===

	// PolyTerra (Matte PLA)
	{match: vendorAndNameContains("Polymaker", "PolyTerra"), profile: FilamentProfile{"GFL01", "PLA"}},

	// Panchroma Matte (also formerly PolyTerra)
	{match: vendorAndNameContains("Polymaker", "Panchroma Matte", "Panchroma™ Matte", "PanChroma™ Matte"), profile: FilamentProfile{"GFPM002", "PLA"}},

	// Panchroma specific types (must be before generic Panchroma)
	{match: vendorAndNameContains("Polymaker", "Panchroma Metallic"), profile: FilamentProfile{"GFPM012", "PLA"}},
	{match: vendorAndNameContains("Polymaker", "Panchroma Silk"), profile: FilamentProfile{"GFPM004", "PLA"}},
	{match: vendorAndNameContains("Polymaker", "Panchroma Glow"), profile: FilamentProfile{"GFPM010", "PLA"}},
	{match: vendorAndNameContains("Polymaker", "Panchroma Marble"), profile: FilamentProfile{"GFPM003", "PLA"}},
	{match: vendorAndNameContains("Polymaker", "Panchroma Starlight", "PanChroma™ Starlight"), profile: FilamentProfile{"GFPM009", "PLA"}},
	{match: vendorAndNameContains("Polymaker", "Panchroma Celestial"), profile: FilamentProfile{"GFPM008", "PLA"}},
	{match: vendorAndNameContains("Polymaker", "Panchroma Galaxy"), profile: FilamentProfile{"GFPM007", "PLA"}},
	{match: vendorAndNameContains("Polymaker", "Panchroma Luminous"), profile: FilamentProfile{"GFPM011", "PLA"}},
	{match: vendorAndNameContains("Polymaker", "Panchroma Neon"), profile: FilamentProfile{"GFPM013", "PLA"}},
	{match: vendorAndNameContains("Polymaker", "Panchroma Translucent"), profile: FilamentProfile{"GFPM006", "PLA"}},
	{match: vendorAndNameContains("Polymaker", "Translucent"), profile: FilamentProfile{"GFPM006", "PLA"}},
	{match: vendorAndNameContains("Polymaker", "Panchroma Stain"), profile: FilamentProfile{"GFPM005", "PLA"}},
	{match: vendorAndNameContains("Polymaker", "Panchroma UV Shift"), profile: FilamentProfile{"GFPM014", "PLA"}},
	{match: vendorAndNameContains("Polymaker", "Panchroma Temp Shift"), profile: FilamentProfile{"GFPM015", "PLA"}},

	// Panchroma generic (regular PLA)
	{match: vendorAndNameContains("Polymaker", "Panchroma"), profile: FilamentProfile{"GFPM001", "PLA"}},

	// Polymaker Marble (older naming)
	{match: vendorAndNameContains("Polymaker", "Marble"), profile: FilamentProfile{"GFPM003", "PLA"}},

	// PolyLite
	{match: vendorAndNameContains("Polymaker", "Polylite PLA Pro", "PolyLite PLA Pro"), profile: FilamentProfile{"GFPM019", "PLA"}},
	{match: vendorAndNameContains("Polymaker", "PolyLite™ Silk", "PolyLite Silk"), profile: FilamentProfile{"GFPM004", "PLA"}},
	{match: vendorAndNameContains("Polymaker", "PolyLite™ Cream", "PolyLite Cream", "PolyLite™", "PolyLite PLA"), profile: FilamentProfile{"GFL00", "PLA"}},
	{match: vendorAndNameContains("Polymaker", "PolyLite ASA"), profile: FilamentProfile{"GFB61", "ASA"}},
	{match: vendorAndNameContains("Polymaker", "PolyLite ABS"), profile: FilamentProfile{"GFB60", "ABS"}},
	{match: vendorAndNameContains("Polymaker", "PolyLite PETG"), profile: FilamentProfile{"GFG60", "PETG"}},

	// Polymaker specialty
	{match: vendorAndNameContains("Polymaker", "PolyDissolve", "PVA"), profile: FilamentProfile{"GFS04", "PVA"}},
	{match: vendorAndNameContains("Polymaker", "Polyflex", "TPU"), profile: FilamentProfile{"GFU99", "TPU"}},
	{match: vendorAndNameContains("Polymaker", "PolySupport"), profile: FilamentProfile{"GFL99", "PLA"}},
	{match: vendorAndNameContains("Polymaker", "HT-PLA-GF"), profile: FilamentProfile{"GFPM018", "PLA"}},
	{match: vendorAndNameContains("Polymaker", "HT-PLA"), profile: FilamentProfile{"GFPM017", "PLA"}},

	// === Bambu (brand-specific profiles) ===
	{match: vendorAndNameContains("Bambu", "Support for PLA"), profile: FilamentProfile{"GFS02", "PLA"}},
	{match: vendorAndNameContains("Bambu", "Silk"), profile: FilamentProfile{"GFA05", "PLA"}},
	{match: vendorAndNameContains("Bambu", "Sparkle"), profile: FilamentProfile{"GFA08", "PLA"}},
	{match: vendorAndMaterial("Bambu", "Matte PLA"), profile: FilamentProfile{"GFA01", "PLA"}},
	{match: vendorAndMaterial("Bambu", "PLA-CF"), profile: FilamentProfile{"GFA50", "PLA-CF"}},
	{match: vendorAndMaterial("Bambu", "PLA", "PLA Basic"), profile: FilamentProfile{"GFA00", "PLA"}},
	{match: vendorAndMaterial("Bambu", "ABS"), profile: FilamentProfile{"GFB00", "ABS"}},
	{match: vendorAndMaterial("Bambu", "ASA"), profile: FilamentProfile{"GFB01", "ASA"}},
	{match: vendorAndMaterial("Bambu", "PETG"), profile: FilamentProfile{"GFG00", "PETG"}},

	// === Vendors with specific OrcaSlicer profiles ===
	{match: vendorAndMaterial("eSun", "PLA+", "PLA Plus"), profile: FilamentProfile{"GFL03", "PLA"}},
	{match: vendorAndNameContains("Overture", "Matte"), profile: FilamentProfile{"GFL05", "PLA"}},
	{match: vendorAndMaterial("Overture", "PLA"), profile: FilamentProfile{"GFL04", "PLA"}},

	// === Silk PLA from any vendor ===
	{match: nameContains("Silk"), profile: FilamentProfile{"GFL96", "PLA"}},

	// === Generic material fallbacks ===
	{match: materialIs("Silk PLA"), profile: FilamentProfile{"GFL96", "PLA"}},
	{match: materialIs("Matte PLA"), profile: FilamentProfile{"GFL99", "PLA"}},
	{match: materialIs("PLA Basic"), profile: FilamentProfile{"GFL99", "PLA"}},
	{match: materialIs("PLA+", "PLA Plus"), profile: FilamentProfile{"GFL99", "PLA"}},
	{match: materialIs("PLA PRO", "PLA Pro"), profile: FilamentProfile{"GFL99", "PLA"}},
	{match: materialIs("PLA PRO PLUS"), profile: FilamentProfile{"GFL99", "PLA"}},
	{match: materialIs("PLA Wood"), profile: FilamentProfile{"GFL99", "PLA"}},
	{match: materialIs("PLA Matte"), profile: FilamentProfile{"GFL99", "PLA"}},
	{match: materialIs("Iridescent PLA"), profile: FilamentProfile{"GFL99", "PLA"}},
	{match: materialIs("Tough PLA"), profile: FilamentProfile{"GFL99", "PLA"}},
	{match: materialIs("PLA-CF"), profile: FilamentProfile{"GFL98", "PLA-CF"}},
	{match: materialIs("PLA"), profile: FilamentProfile{"GFL99", "PLA"}},
	{match: materialIs("PETG-CF"), profile: FilamentProfile{"GFG98", "PETG-CF"}},
	{match: materialIs("PETG"), profile: FilamentProfile{"GFG99", "PETG"}},
	{match: materialIs("ABS"), profile: FilamentProfile{"GFB99", "ABS"}},
	{match: materialIs("ASA"), profile: FilamentProfile{"GFB98", "ASA"}},
	{match: materialIs("PC", "PC-CF"), profile: FilamentProfile{"GFC99", "PC"}},
	{match: materialIs("TPU"), profile: FilamentProfile{"GFU99", "TPU"}},
	{match: materialIs("PVA"), profile: FilamentProfile{"GFS99", "PVA"}},
	{match: materialIs("PolySupport"), profile: FilamentProfile{"GFL99", "PLA"}},
	{match: materialIs("HIPS"), profile: FilamentProfile{"GFS98", "HIPS"}},
}

// LookupFilamentProfile finds the best matching Bambu/OrcaSlicer profile for a filament.
func LookupFilamentProfile(vendor, name, material string) *FilamentProfile {
	for _, rule := range filamentProfileRules {
		if rule.match(vendor, name, material) {
			p := rule.profile
			return &p
		}
	}
	return nil
}

// vendorAndNameContains returns a matcher that checks if the vendor matches
// and the filament name contains any of the given substrings (case-insensitive).
func vendorAndNameContains(vendor string, substrs ...string) func(string, string, string) bool {
	return func(v, n, m string) bool {
		if !strings.EqualFold(v, vendor) && !strings.Contains(strings.ToLower(v), strings.ToLower(vendor)) {
			return false
		}
		lower := strings.ToLower(n)
		for _, s := range substrs {
			if strings.Contains(lower, strings.ToLower(s)) {
				return true
			}
		}
		return false
	}
}

// vendorAndMaterial returns a matcher that checks vendor and material type.
func vendorAndMaterial(vendor string, materials ...string) func(string, string, string) bool {
	return func(v, n, m string) bool {
		if !strings.EqualFold(v, vendor) && !strings.Contains(strings.ToLower(v), strings.ToLower(vendor)) {
			return false
		}
		for _, mat := range materials {
			if strings.EqualFold(m, mat) {
				return true
			}
		}
		return false
	}
}

// nameContains returns a matcher that checks if the filament name contains any
// of the given substrings (case-insensitive), regardless of vendor.
func nameContains(substrs ...string) func(string, string, string) bool {
	return func(v, n, m string) bool {
		lower := strings.ToLower(n)
		for _, s := range substrs {
			if strings.Contains(lower, strings.ToLower(s)) {
				return true
			}
		}
		return false
	}
}

// materialIs returns a matcher that checks only the material type.
func materialIs(materials ...string) func(string, string, string) bool {
	return func(v, n, m string) bool {
		for _, mat := range materials {
			if strings.EqualFold(m, mat) {
				return true
			}
		}
		return false
	}
}

// profileNames maps tray_info_idx codes to OrcaSlicer profile names.
// Sourced from OrcaSlicer's BBL filament profiles.
var profileNames = map[string]string{
	"GFA00":    "Bambu PLA Basic",
	"GFA01":    "Bambu PLA Matte",
	"GFA02":    "Bambu PLA Metal",
	"GFA03":    "Bambu PLA Impact",
	"GFA05":    "Bambu PLA Silk",
	"GFA06":    "Bambu PLA Silk+",
	"GFA07":    "Bambu PLA Marble",
	"GFA08":    "Bambu PLA Sparkle",
	"GFA09":    "Bambu PLA Tough",
	"GFA11":    "Bambu PLA Aero",
	"GFA12":    "Bambu PLA Glow",
	"GFA13":    "Bambu PLA Dynamic",
	"GFA15":    "Bambu PLA Galaxy",
	"GFA16":    "Bambu PLA Wood",
	"GFA50":    "Bambu PLA-CF",
	"GFB00":    "Bambu ABS",
	"GFB01":    "Bambu ASA",
	"GFB02":    "Bambu ASA-Aero",
	"GFB50":    "Bambu ABS-GF",
	"GFB51":    "Bambu ASA-CF",
	"GFB60":    "PolyLite ABS",
	"GFB61":    "PolyLite ASA",
	"GFB98":    "Generic ASA",
	"GFB99":    "Generic ABS",
	"GFC00":    "Bambu PC",
	"GFC01":    "Bambu PC FR",
	"GFC99":    "Generic PC",
	"GFG00":    "Bambu PETG Basic",
	"GFG01":    "Bambu PETG Translucent",
	"GFG02":    "Bambu PETG HF",
	"GFG50":    "Bambu PETG-CF",
	"GFG60":    "PolyLite PETG",
	"GFG96":    "Generic PETG HF",
	"GFG97":    "Generic PCTG",
	"GFG98":    "Generic PETG-CF",
	"GFG99":    "Generic PETG",
	"GFL00":    "PolyLite PLA",
	"GFL01":    "PolyTerra PLA",
	"GFL03":    "eSUN PLA+",
	"GFL04":    "Overture PLA",
	"GFL05":    "Overture Matte PLA",
	"GFL06":    "Fiberon PETG-ESD",
	"GFL50":    "Fiberon PA6-CF",
	"GFL51":    "Fiberon PA6-GF",
	"GFL52":    "Fiberon PA12-CF",
	"GFL53":    "Fiberon PA612-CF",
	"GFL54":    "Fiberon PET-CF",
	"GFL55":    "Fiberon PETG-rCF",
	"GFL95":    "Generic PLA High Speed",
	"GFL96":    "Generic PLA Silk",
	"GFL98":    "Generic PLA-CF",
	"GFL99":    "Generic PLA",
	"GFN03":    "Bambu PA-CF",
	"GFN04":    "Bambu PAHT-CF",
	"GFN05":    "Bambu PA6-CF",
	"GFN06":    "Bambu PPA-CF",
	"GFN08":    "Bambu PA6-GF",
	"GFS00":    "Bambu Support W",
	"GFS01":    "Bambu Support G",
	"GFS02":    "Bambu Support For PLA",
	"GFS03":    "Bambu Support For PA/PET",
	"GFS04":    "Bambu PVA",
	"GFS05":    "Bambu Support For PLA-PETG",
	"GFS06":    "Bambu Support for ABS",
	"GFS98":    "Generic HIPS",
	"GFS99":    "Generic PVA",
	"GFT01":    "Bambu PET-CF",
	"GFT02":    "Bambu PPS-CF",
	"GFU00":    "Bambu TPU 95A HF",
	"GFU01":    "Bambu TPU 95A",
	"GFU02":    "Bambu TPU for AMS",
	"GFU98":    "Generic TPU for AMS",
	"GFU99":    "Generic TPU",
	"GFR00":    "FusRock ABS-GF",
	"GFOT001":  "Overture PLA Pro",
	"GFPM001":  "Panchroma PLA",
	"GFPM002":  "Panchroma PLA Matte",
	"GFPM003":  "Panchroma PLA Marble",
	"GFPM004":  "Panchroma PLA Silk",
	"GFPM005":  "Panchroma PLA Stain",
	"GFPM006":  "Panchroma PLA Translucent",
	"GFPM007":  "Panchroma PLA Galaxy",
	"GFPM008":  "Panchroma PLA Celestial",
	"GFPM009":  "Panchroma PLA Starlight",
	"GFPM010":  "Panchroma PLA Glow",
	"GFPM011":  "Panchroma PLA Luminous",
	"GFPM012":  "Panchroma PLA Metallic",
	"GFPM013":  "Panchroma PLA Neon",
	"GFPM014":  "Panchroma PLA UV Shift",
	"GFPM015":  "Panchroma PLA Temp Shift",
	"GFPM016":  "Panchroma CoPE",
	"GFPM017":  "Polymaker HT-PLA",
	"GFPM018":  "Polymaker HT-PLA-GF",
	"GFPM019":  "PolyLite PLA Pro",
}

// ProfileName returns the OrcaSlicer profile name for a tray_info_idx code.
func ProfileName(infoIdx string) string {
	if name, ok := profileNames[infoIdx]; ok {
		return name
	}
	return ""
}
