package cmd

import "github.com/dstockto/fil/inventory"

// The Bambu filament profile table lives in package inventory so fil serve
// can push tray info after a move; these keep the cmd call sites unchanged.

// FilamentProfile represents the Bambu/OrcaSlicer profile for a filament.
type FilamentProfile = inventory.FilamentProfile

// LookupFilamentProfile finds the best matching Bambu/OrcaSlicer profile for a filament.
func LookupFilamentProfile(vendor, name, material string) *FilamentProfile {
	return inventory.LookupFilamentProfile(vendor, name, material)
}

// ProfileName returns the OrcaSlicer profile name for a tray_info_idx code.
func ProfileName(infoIdx string) string { return inventory.ProfileName(infoIdx) }
