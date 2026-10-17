package cmd

import "github.com/dstockto/fil/inventory"

// The Bambu filament profile table lives in package inventory so fil serve
// can push tray info after a move; these keep the cmd call sites unchanged.

type FilamentProfile = inventory.FilamentProfile

func LookupFilamentProfile(vendor, name, material string) *FilamentProfile {
	return inventory.LookupFilamentProfile(vendor, name, material)
}

func ProfileName(infoIdx string) string { return inventory.ProfileName(infoIdx) }
