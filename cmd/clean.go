package cmd

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/dstockto/fil/api"
	"github.com/spf13/cobra"
)

// cleanCmd cleans up locations_spoolorders by removing spool IDs that are no longer at that Location.
var cleanCmd = &cobra.Command{
	Use:   "clean-orders",
	Short: "Clean locations_spoolorders by removing stale spool IDs",
	Long:  "Cleans the Spoolman setting 'locations_spoolorders' by removing spool IDs that are no longer in those locations.",
	RunE:  runClean,
	Args:  cobra.NoArgs,
}

func runClean(cmd *cobra.Command, _ []string) error {
	if Cfg == nil || Cfg.ApiBase == "" {
		return errors.New("apiClient endpoint not configured")
	}

	apiClient := api.NewClient(Cfg.ApiBase, Cfg.TLSSkipVerify)
	ctx := cmd.Context()

	write, err := cmd.Flags().GetBool("write")
	if err != nil {
		return err
	}
	addMissing, err := cmd.Flags().GetBool("add-missing")
	if err != nil {
		return err
	}

	// 1) Fetch all spools (allow archived) to get their current locations
	spools, err := apiClient.FindSpoolsByName(ctx, "*", nil, map[string]string{"allow_archived": "true"})
	if err != nil {
		return fmt.Errorf("failed to fetch spools: %w", err)
	}

	current := map[string]map[int]struct{}{}
	for _, s := range spools {
		loc := s.Location
		if _, ok := current[loc]; !ok {
			current[loc] = map[int]struct{}{}
		}
		current[loc][s.Id] = struct{}{}
	}

	// 2) Fetch settings and parse locations_spoolorders
	settings, err := apiClient.GetSettings(ctx)
	if err != nil {
		return fmt.Errorf("failed to fetch settings: %w", err)
	}

	entry, ok := settings["locations_spoolorders"]
	if !ok {
		return fmt.Errorf("settings did not include 'locations_spoolorders'")
	}

	var rawString string
	if err := json.Unmarshal(entry.Value, &rawString); err != nil {
		return fmt.Errorf("failed to decode settings value wrapper: %w", err)
	}

	var orders map[string][]int
	if rawString == "" {
		orders = map[string][]int{}
	} else if err := json.Unmarshal([]byte(rawString), &orders); err != nil {
		return fmt.Errorf("failed to parse locations_spoolorders JSON: %w", err)
	}

	// 3) Clean: keep only IDs currently at the same Location. Optionally add missing IDs.
	cleaned := make(map[string][]int, len(orders))
	removedTotal := 0
	addedTotal := 0

	// Process locations already present in orders
	for loc, ids := range orders {
		set := current[loc] // nil map is fine; membership will be false
		isPrinter := IsPrinterLocation(loc)
		kept := make([]int, 0, len(ids))
		removed := make([]int, 0)
		for _, id := range ids {
			if id == EmptySlot && isPrinter {
				// Preserve empty slot sentinels in printer locations
				kept = append(kept, id)
			} else if _, ok := set[id]; ok {
				kept = append(kept, id)
			} else {
				removed = append(removed, id)
			}
		}
		removedTotal += len(removed)
		if len(removed) > 0 {
			fmt.Printf("%s: removing %d stale id(s): %v\n", locLabel(loc), len(removed), removed)
		}

		// Optionally append missing IDs (those present in current but not listed)
		added := make([]int, 0)
		if addMissing && len(set) > 0 && loc != "" {
			present := make(map[int]struct{}, len(kept))
			for _, id := range kept {
				present[id] = struct{}{}
			}
			for id := range set {
				if _, ok := present[id]; !ok {
					added = append(added, id)
				}
			}
			if len(added) > 0 {
				sort.Ints(added)
				kept = append(kept, added...)
				addedTotal += len(added)
				fmt.Printf("%s: adding %d missing id(s): %v\n", locLabel(loc), len(added), added)
			}
		}

		// preserve relative order of existing IDs; newly added go to the end
		cleaned[loc] = kept
	}

	// Also handle locations that exist in current but are absent from orders
	if addMissing {
		for loc, set := range current {
			if loc == "" {
				// Do not add missing spools to the <empty> Location
				continue
			}
			if _, seen := orders[loc]; seen {
				continue
			}
			if len(set) == 0 {
				continue
			}
			ids := make([]int, 0, len(set))
			for id := range set {
				ids = append(ids, id)
			}
			sort.Ints(ids)
			cleaned[loc] = ids
			addedTotal += len(ids)
			fmt.Printf("%s: adding %d missing id(s): %v\n", locLabel(loc), len(ids), ids)
		}
	}

	// Pad printer locations to their configured capacity with EmptySlot sentinels
	paddedTotal := 0
	for loc, ids := range cleaned {
		if !IsPrinterLocation(loc) {
			continue
		}
		padded := PadToCapacity(loc, ids)
		if len(padded) != len(ids) {
			paddedTotal += len(padded) - len(ids)
			fmt.Printf("%s: padding %d empty slot(s) to capacity\n", locLabel(loc), len(padded)-len(ids))
			cleaned[loc] = padded
		}
	}

	if removedTotal == 0 && addedTotal == 0 && paddedTotal == 0 {
		fmt.Println("No changes needed; nothing to clean or add.")
		return nil
	}

	if !write {
		parts := []string{}
		if removedTotal > 0 {
			parts = append(parts, fmt.Sprintf("remove %d stale id(s)", removedTotal))
		}
		if addedTotal > 0 {
			parts = append(parts, fmt.Sprintf("add %d missing id(s)", addedTotal))
		}
		if paddedTotal > 0 {
			parts = append(parts, fmt.Sprintf("pad %d empty slot(s)", paddedTotal))
		}
		fmt.Printf("Dry run: would %s. Use --write to apply changes.\n", strings.Join(parts, ", "))
		return nil
	}

	// 4) Write back cleaned map via POST /api/v1/setting/locations_spoolorders
	if err := apiClient.PostSettingObject(ctx, "locations_spoolorders", cleaned); err != nil {
		return fmt.Errorf("failed to update settings: %w", err)
	}

	parts := []string{}
	if removedTotal > 0 {
		parts = append(parts, fmt.Sprintf("removed %d stale id(s)", removedTotal))
	}
	if addedTotal > 0 {
		parts = append(parts, fmt.Sprintf("added %d missing id(s)", addedTotal))
	}
	if paddedTotal > 0 {
		parts = append(parts, fmt.Sprintf("padded %d empty slot(s)", paddedTotal))
	}
	fmt.Printf("Updated locations_spoolorders; %s.\n", strings.Join(parts, ", "))
	return nil
}

func locLabel(loc string) string {
	if loc == "" {
		return "<empty>"
	}
	return loc
}

func init() { //nolint:gochecknoinits
	cleanCmd.Flags().Bool("write", false, "apply changes (by default runs as a dry run)")
	cleanCmd.Flags().Bool("add-missing", false, "also add missing spool IDs to each Location based on current data")
	rootCmd.AddCommand(cleanCmd)
}
