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
	"strconv"
	"strings"

	"github.com/dstockto/fil/api"
	"github.com/fatih/color"
	"github.com/spf13/cobra"
)

var locationCmd = &cobra.Command{
	Use:   "location",
	Short: "Manage locations",
	Long:  `View and manage location settings such as capacity.`,
}

var locationCapacityCmd = &cobra.Command{
	Use:   "capacity",
	Short: "Manage location capacity",
	Long:  `View and set capacity for locations.`,
}

var locationCapacitySetCmd = &cobra.Command{
	Use:   "set <location> [capacity]",
	Short: "Set a location's capacity",
	Long: `Set the capacity for a location. If capacity is not provided, uses the
current spool count. Use --full to skip the confirmation prompt when
setting from current count.`,
	Args: cobra.RangeArgs(1, 2),
	RunE: runLocationCapacitySet,
}

var locationCapacityShowCmd = &cobra.Command{
	Use:   "show [location]",
	Short: "Show location capacity and usage",
	Long:  `Display capacity and current spool count for one or all locations.`,
	Args:  cobra.MaximumNArgs(1),
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

	// If a specific location is requested, show just that one
	if len(args) == 1 {
		location := MapToAlias(args[0])
		ids := orders[location]
		count := CountSpools(ids)
		if capInfo, ok := Cfg.LocationCapacity[location]; ok {
			avail := capInfo.Capacity - count
			fmt.Printf("%s: %d/%d", location, count, capInfo.Capacity)
			if avail > 0 {
				color.Green(" (%d available)", avail)
			} else if avail == 0 {
				color.Yellow(" (full)")
			} else {
				color.Red(" (%d over capacity)", -avail)
			}
			fmt.Println()
		} else {
			fmt.Printf("%s: %d spools (no capacity set)\n", location, count)
		}
		return nil
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
			locs = append(locs, locInfo{name: loc, count: 0, capacity: capInfo.Capacity})
		}
	}

	// Print
	for _, l := range locs {
		if l.capacity > 0 {
			avail := l.capacity - l.count
			fmt.Printf("%-20s %d/%d", l.name, l.count, l.capacity)
			if avail > 0 {
				color.Green(" (%d available)", avail)
			} else if avail == 0 {
				color.Yellow(" (full)")
			} else {
				color.Red(" (%d over capacity)", -avail)
			}
			fmt.Println()
		} else {
			fmt.Printf("%-20s %d spools\n", l.name, l.count)
		}
	}

	return nil
}

//nolint:gochecknoinits
func init() {
	rootCmd.AddCommand(locationCmd)
	locationCmd.AddCommand(locationCapacityCmd)
	locationCapacityCmd.AddCommand(locationCapacitySetCmd)
	locationCapacityCmd.AddCommand(locationCapacityShowCmd)

	locationCapacitySetCmd.Flags().Bool("full", false, "set capacity from current spool count without prompting")
	locationCapacitySetCmd.Flags().Bool("dry-run", false, "show what would be set without making changes")
}
