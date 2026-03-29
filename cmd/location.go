/*
Copyright © 2025 David Stockton <dave@davidstockton.com>
*/
package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/dstockto/fil/api"
	"github.com/fatih/color"
	"github.com/spf13/cobra"
)

var locationCmd = &cobra.Command{
	Use:     "location",
	Short:   "Manage locations",
	Long:    `View and manage location settings such as capacity.`,
	Aliases: []string{"loc", "lo", "l"},
}

var locationCapacityCmd = &cobra.Command{
	Use:     "capacity",
	Short:   "Manage location capacity",
	Long:    `View and set capacity for locations.`,
	Aliases: []string{"cap", "ca", "c"},
}

var locationCapacitySetCmd = &cobra.Command{
	Use:   "set <location> [capacity]",
	Short: "Set a location's capacity",
	Long: `Set the capacity for a location. If capacity is not provided, uses the
current spool count. Use --full to skip the confirmation prompt when
setting from current count.`,
	Args:    cobra.RangeArgs(1, 2),
	RunE:    runLocationCapacitySet,
}

var locationCapacityShowCmd = &cobra.Command{
	Use:   "show [location...]",
	Short: "Show location capacity and usage",
	Long:  `Display capacity and current spool count for specified locations, or all if none given.`,
	RunE:  runLocationCapacityShow,
}

func runLocationCapacitySet(cmd *cobra.Command, args []string) error {
	if Cfg == nil {
		return fmt.Errorf("configuration not loaded")
	}

	location := MapToAlias(args[0])
	full, _ := cmd.Flags().GetBool("full")
	dryRun, _ := cmd.Flags().GetBool("dry-run")

	var capacity int

	if len(args) == 2 {
		// Explicit capacity provided
		c, err := strconv.Atoi(args[1])
		if err != nil || c < 1 {
			return fmt.Errorf("capacity must be a positive integer, got %q", args[1])
		}
		capacity = c
	} else {
		// Derive from current spool count
		if Cfg.ApiBase == "" {
			return fmt.Errorf("api_base must be configured to query current spool count")
		}
		apiClient := api.NewClient(Cfg.ApiBase, Cfg.TLSSkipVerify)
		ctx := cmd.Context()

		orders, err := LoadLocationOrders(ctx, apiClient)
		if err != nil {
			return fmt.Errorf("failed to load location orders: %w", err)
		}
		ids := orders[location]
		count := CountSpools(ids)

		if count == 0 {
			return fmt.Errorf("no spools found in %q; specify a capacity manually", location)
		}

		if full {
			capacity = count
		} else {
			fmt.Printf("%s currently has %d spool(s). Set capacity to %d? [Y/n] ", location, count, count)
			var input string
			_, _ = fmt.Scanln(&input)
			input = strings.TrimSpace(strings.ToLower(input))
			if input == "" || input == "y" || input == "yes" {
				capacity = count
			} else {
				fmt.Printf("Enter capacity for %s: ", location)
				var capInput string
				_, _ = fmt.Scanln(&capInput)
				c, err := strconv.Atoi(strings.TrimSpace(capInput))
				if err != nil || c < 1 {
					return fmt.Errorf("capacity must be a positive integer, got %q", capInput)
				}
				capacity = c
			}
		}
	}

	if dryRun {
		fmt.Printf("Would set capacity for %s to %d\n", location, capacity)
		return nil
	}

	// Pull → update → push flow
	if err := pullUpdatePushCapacity(cmd.Context(), location, capacity); err != nil {
		return err
	}

	fmt.Printf("Set capacity for %s to %d\n", location, capacity)
	return nil
}

// pullUpdatePushCapacity pulls the shared config from the server, updates the
// location capacity, pushes back, and writes the local shared config file.
func pullUpdatePushCapacity(ctx context.Context, location string, capacity int) error {
	if Cfg == nil || Cfg.PlansServer == "" {
		// No plan server — update local config only
		return updateLocalCapacity(location, capacity)
	}

	client := api.NewPlanServerClient(Cfg.PlansServer, version, Cfg.TLSSkipVerify)

	// Pull current shared config from server
	data, err := client.GetSharedConfig(ctx)
	if err != nil {
		return fmt.Errorf("failed to pull shared config: %w", err)
	}

	var shared SharedConfig
	if err := json.Unmarshal(data, &shared); err != nil {
		return fmt.Errorf("failed to parse shared config: %w", err)
	}

	// Update capacity
	if shared.LocationCapacity == nil {
		shared.LocationCapacity = map[string]LocationCapacity{}
	}
	shared.LocationCapacity[location] = LocationCapacity{Capacity: capacity}

	// Push updated config back to server
	updatedData, err := json.MarshalIndent(shared, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal shared config: %w", err)
	}
	if err := client.PutSharedConfig(ctx, updatedData); err != nil {
		return fmt.Errorf("failed to push shared config: %w", err)
	}

	// Write locally too
	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("failed to determine home directory: %w", err)
	}
	destPath := filepath.Join(home, ".config", "fil", "shared-config.json")
	if err := os.MkdirAll(filepath.Dir(destPath), 0755); err != nil {
		return fmt.Errorf("failed to create config directory: %w", err)
	}
	if err := os.WriteFile(destPath, updatedData, 0644); err != nil {
		return fmt.Errorf("failed to write local shared config: %w", err)
	}

	// Update in-memory config
	if Cfg.LocationCapacity == nil {
		Cfg.LocationCapacity = map[string]LocationCapacity{}
	}
	Cfg.LocationCapacity[location] = LocationCapacity{Capacity: capacity}

	return nil
}

// updateLocalCapacity updates the local shared config file only (no server).
func updateLocalCapacity(location string, capacity int) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("failed to determine home directory: %w", err)
	}
	destPath := filepath.Join(home, ".config", "fil", "shared-config.json")

	var shared SharedConfig
	if data, err := os.ReadFile(destPath); err == nil {
		_ = json.Unmarshal(data, &shared)
	}

	if shared.LocationCapacity == nil {
		shared.LocationCapacity = map[string]LocationCapacity{}
	}
	shared.LocationCapacity[location] = LocationCapacity{Capacity: capacity}

	updatedData, err := json.MarshalIndent(shared, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal shared config: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(destPath), 0755); err != nil {
		return fmt.Errorf("failed to create config directory: %w", err)
	}
	if err := os.WriteFile(destPath, updatedData, 0644); err != nil {
		return fmt.Errorf("failed to write local shared config: %w", err)
	}

	// Update in-memory config
	if Cfg.LocationCapacity == nil {
		Cfg.LocationCapacity = map[string]LocationCapacity{}
	}
	Cfg.LocationCapacity[location] = LocationCapacity{Capacity: capacity}

	return nil
}

func runLocationCapacityShow(cmd *cobra.Command, args []string) error {
	if Cfg == nil {
		return fmt.Errorf("configuration not loaded")
	}
	if Cfg.ApiBase == "" {
		return fmt.Errorf("api_base must be configured")
	}

	apiClient := api.NewClient(Cfg.ApiBase, Cfg.TLSSkipVerify)
	ctx := cmd.Context()

	orders, err := LoadLocationOrders(ctx, apiClient)
	if err != nil {
		return fmt.Errorf("failed to load location orders: %w", err)
	}

	// Build partial-match filters from args (OR logic)
	filters := make([]string, len(args))
	for i, a := range args {
		filters[i] = strings.ToLower(strings.TrimSpace(a))
	}

	matchesFilter := func(loc string) bool {
		if len(filters) == 0 {
			return true
		}
		lower := strings.ToLower(loc)
		for _, f := range filters {
			if strings.Contains(lower, f) {
				return true
			}
		}
		return false
	}

	// Show all locations that have spools or capacity configured
	type locInfo struct {
		name     string
		count    int
		capacity int // 0 means not set
	}
	var locs []locInfo

	// Gather from orders
	seen := map[string]struct{}{}
	for loc, ids := range orders {
		if loc == "" {
			continue
		}
		if !matchesFilter(loc) {
			continue
		}
		count := CountSpools(ids)
		cap := 0
		if capInfo, ok := Cfg.LocationCapacity[loc]; ok {
			cap = capInfo.Capacity
		}
		locs = append(locs, locInfo{name: loc, count: count, capacity: cap})
		seen[loc] = struct{}{}
	}

	// Add capacity-configured locations not in orders
	if Cfg.LocationCapacity != nil {
		for loc, capInfo := range Cfg.LocationCapacity {
			if _, ok := seen[loc]; ok {
				continue
			}
			if !matchesFilter(loc) {
				continue
			}
			locs = append(locs, locInfo{name: loc, count: 0, capacity: capInfo.Capacity})
		}
	}

	// Sort by name
	sort.Slice(locs, func(i, j int) bool {
		return locs[i].name < locs[j].name
	})

	// Print
	for _, l := range locs {
		printLocationCapacity(l.name, l.count)
	}

	return nil
}

func printLocationCapacity(location string, count int) {
	if capInfo, ok := Cfg.LocationCapacity[location]; ok {
		avail := capInfo.Capacity - count
		status := ""
		if avail > 0 {
			status = color.GreenString(" (%d available)", avail)
		} else if avail == 0 {
			status = color.YellowString(" (full)")
		} else {
			status = color.RedString(" (%d over capacity)", -avail)
		}
		fmt.Printf("%-20s %d/%d%s\n", location, count, capInfo.Capacity, status)
	} else {
		fmt.Printf("%-20s %d spools\n", location, count)
	}
}

var locationDeleteCmd = &cobra.Command{
	Use:   "delete <location...>",
	Short: "Delete a location from tracking",
	Long: `Remove one or more locations from locations_spoolorders and location_capacity.
Refuses to delete locations that still have spools assigned to them.`,
	Args:    cobra.MinimumNArgs(1),
	RunE:    runLocationDelete,
	Aliases: []string{"del", "rm"},
}

func runLocationDelete(cmd *cobra.Command, args []string) error {
	if Cfg == nil || Cfg.ApiBase == "" {
		return fmt.Errorf("api_base must be configured")
	}

	apiClient := api.NewClient(Cfg.ApiBase, Cfg.TLSSkipVerify)
	ctx := cmd.Context()
	dryRun, _ := cmd.Flags().GetBool("dry-run")

	// Load current orders
	orders, err := LoadLocationOrders(ctx, apiClient)
	if err != nil {
		return fmt.Errorf("failed to load location orders: %w", err)
	}

	// Fetch all spools to check for occupants
	spools, err := apiClient.FindSpoolsByName(ctx, "*", nil, map[string]string{"allow_archived": "true"})
	if err != nil {
		return fmt.Errorf("failed to fetch spools: %w", err)
	}
	spoolsByLoc := map[string]int{}
	for _, s := range spools {
		if s.Location != "" {
			spoolsByLoc[s.Location]++
		}
	}

	deleted := 0
	capacityRemoved := 0
	for _, arg := range args {
		location := MapToAlias(arg)

		// Check if spools are assigned to this location
		if count := spoolsByLoc[location]; count > 0 {
			color.Red("Cannot delete %s: %d spool(s) still assigned\n", location, count)
			continue
		}

		// Check it exists in orders or capacity
		_, inOrders := orders[location]
		inCapacity := false
		if Cfg.LocationCapacity != nil {
			_, inCapacity = Cfg.LocationCapacity[location]
		}

		if !inOrders && !inCapacity {
			fmt.Printf("%s: not found in orders or capacity config\n", location)
			continue
		}

		if dryRun {
			parts := []string{}
			if inOrders {
				parts = append(parts, "locations_spoolorders")
			}
			if inCapacity {
				parts = append(parts, "location_capacity")
			}
			fmt.Printf("Would delete %s from %s\n", location, strings.Join(parts, " and "))
			continue
		}

		if inOrders {
			delete(orders, location)
			deleted++
			fmt.Printf("Removed %s from locations_spoolorders\n", location)
		}

		if inCapacity {
			capacityRemoved++
		}
	}

	if dryRun {
		return nil
	}

	// Persist orders if any were deleted
	if deleted > 0 {
		if err := apiClient.PostSettingObject(ctx, "locations_spoolorders", orders); err != nil {
			return fmt.Errorf("failed to update locations_spoolorders: %w", err)
		}
	}

	// Remove from capacity config via pull→update→push
	if capacityRemoved > 0 {
		locsToRemove := []string{}
		for _, arg := range args {
			location := MapToAlias(arg)
			if Cfg.LocationCapacity != nil {
				if _, ok := Cfg.LocationCapacity[location]; ok {
					locsToRemove = append(locsToRemove, location)
				}
			}
		}
		if err := pullRemoveCapacity(ctx, locsToRemove); err != nil {
			return fmt.Errorf("failed to update capacity config: %w", err)
		}
		for _, loc := range locsToRemove {
			fmt.Printf("Removed %s from location_capacity\n", loc)
		}
	}

	return nil
}

// pullRemoveCapacity pulls the shared config, removes the given locations from
// location_capacity, pushes back, and writes the local shared config file.
func pullRemoveCapacity(ctx context.Context, locations []string) error {
	if Cfg == nil || Cfg.PlansServer == "" {
		return removeLocalCapacity(locations)
	}

	client := api.NewPlanServerClient(Cfg.PlansServer, version, Cfg.TLSSkipVerify)

	data, err := client.GetSharedConfig(ctx)
	if err != nil {
		return fmt.Errorf("failed to pull shared config: %w", err)
	}

	var shared SharedConfig
	if err := json.Unmarshal(data, &shared); err != nil {
		return fmt.Errorf("failed to parse shared config: %w", err)
	}

	for _, loc := range locations {
		delete(shared.LocationCapacity, loc)
		delete(Cfg.LocationCapacity, loc)
	}

	updatedData, err := json.MarshalIndent(shared, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal shared config: %w", err)
	}
	if err := client.PutSharedConfig(ctx, updatedData); err != nil {
		return fmt.Errorf("failed to push shared config: %w", err)
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("failed to determine home directory: %w", err)
	}
	destPath := filepath.Join(home, ".config", "fil", "shared-config.json")
	if err := os.MkdirAll(filepath.Dir(destPath), 0755); err != nil {
		return fmt.Errorf("failed to create config directory: %w", err)
	}
	return os.WriteFile(destPath, updatedData, 0644)
}

func removeLocalCapacity(locations []string) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("failed to determine home directory: %w", err)
	}
	destPath := filepath.Join(home, ".config", "fil", "shared-config.json")

	var shared SharedConfig
	if data, err := os.ReadFile(destPath); err == nil {
		_ = json.Unmarshal(data, &shared)
	}

	for _, loc := range locations {
		delete(shared.LocationCapacity, loc)
		if Cfg != nil {
			delete(Cfg.LocationCapacity, loc)
		}
	}

	updatedData, err := json.MarshalIndent(shared, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal shared config: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(destPath), 0755); err != nil {
		return fmt.Errorf("failed to create config directory: %w", err)
	}
	return os.WriteFile(destPath, updatedData, 0644)
}

//nolint:gochecknoinits
func init() {
	rootCmd.AddCommand(locationCmd)
	locationCmd.AddCommand(locationCapacityCmd)
	locationCmd.AddCommand(locationDeleteCmd)
	locationCapacityCmd.AddCommand(locationCapacitySetCmd)
	locationCapacityCmd.AddCommand(locationCapacityShowCmd)

	locationCapacitySetCmd.Flags().Bool("full", false, "set capacity from current spool count without prompting")
	locationCapacitySetCmd.Flags().Bool("dry-run", false, "show what would be set without making changes")

	locationDeleteCmd.Flags().Bool("dry-run", false, "show what would be deleted without making changes")
}
