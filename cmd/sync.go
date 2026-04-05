package cmd

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/dstockto/fil/api"
	"github.com/dstockto/fil/models"
	"github.com/manifoldco/promptui"
	"github.com/spf13/cobra"
)

var syncCmd = &cobra.Command{
	Use:   "sync",
	Short: "Push filament info from Spoolman to all connected printers",
	Long:  "Reads current spool locations from Spoolman and pushes color, material type, and temperature settings to each printer's AMS trays via the plan server.",
	RunE: func(cmd *cobra.Command, args []string) error {
		if Cfg == nil || Cfg.ApiBase == "" {
			return fmt.Errorf("api_base must be configured")
		}
		if Cfg.PlansServer == "" {
			return fmt.Errorf("plans_server must be configured for sync")
		}

		dryRun, _ := cmd.Flags().GetBool("dry-run")
		printerFilter, _ := cmd.Flags().GetString("printer")
		ctx := cmd.Context()

		spoolmanClient := api.NewClient(Cfg.ApiBase, Cfg.TLSSkipVerify)
		planClient := api.NewPlanServerClient(Cfg.PlansServer, version, Cfg.TLSSkipVerify)

		// Load location orders to know slot positions
		orders, err := LoadLocationOrders(ctx, spoolmanClient)
		if err != nil {
			return fmt.Errorf("failed to load location orders: %w", err)
		}

		// Pre-fetch all spools so we can look up by ID
		allSpools, err := spoolmanClient.FindSpoolsByName(ctx, "*", nil, nil)
		if err != nil {
			return fmt.Errorf("failed to fetch spools: %w", err)
		}
		spoolByID := make(map[int]*models.FindSpool)
		for i := range allSpools {
			spoolByID[allSpools[i].Id] = &allSpools[i]
		}

		// Determine which printers to sync
		// Only consider printers that support tray sync
		var syncablePrinters []string
		for name, pCfg := range Cfg.Printers {
			if pCfg.Type == "bambu" && pCfg.IP != "" {
				syncablePrinters = append(syncablePrinters, name)
			}
		}
		sort.Strings(syncablePrinters)

		if len(syncablePrinters) == 0 {
			fmt.Println("No printers support tray sync.")
			return nil
		}

		if printerFilter == "" && len(syncablePrinters) > 1 {
			// Interactive selection: all or pick one
			printerNames := syncablePrinters
			sort.Strings(printerNames)
			items := append([]string{"All printers"}, printerNames...)
			prompt := promptui.Select{
				Label:  "Which printer to sync?",
				Items:  items,
				Stdout: NoBellStdout,
			}
			idx, _, err := prompt.Run()
			if err != nil {
				return err
			}
			if idx > 0 {
				printerFilter = items[idx]
			}
		}

		pushed := 0
		skipped := 0

		for printerName, pCfg := range Cfg.Printers {
			if pCfg.Type == "" || pCfg.IP == "" {
				continue
			}
			if printerFilter != "" && printerName != printerFilter {
				continue
			}
			if pCfg.Type != "bambu" {
				fmt.Printf("%s: skipped (tray sync not supported for %s printers)\n", printerName, pCfg.Type)
				continue
			}

			fmt.Printf("%s:\n", printerName)

			for locIdx, loc := range pCfg.Locations {
				ids, ok := orders[loc]
				if !ok {
					continue
				}

				for trayIdx, spoolID := range ids {
					if spoolID == EmptySlot {
						continue
					}

					spool, ok := spoolByID[spoolID]
					if !ok {
						continue
					}

					colorHex := strings.TrimPrefix(spool.Filament.ColorHex, "#")
					if len(colorHex) == 6 {
						colorHex += "FF"
					}

					slotLabel := fmt.Sprintf("%s:%d", loc, trayIdx+1)

					if dryRun {
						fmt.Printf("  %s — #%d %s %s (%s)\n", slotLabel, spool.Id, spool.Filament.Vendor.Name, spool.Filament.Name, spool.Filament.Material)
						pushed++
						continue
					}

					trayType := spool.Filament.Material
					infoIdx := ""
					if profile := LookupFilamentProfile(spool.Filament.Vendor.Name, spool.Filament.Name, spool.Filament.Material); profile != nil {
						trayType = profile.TrayType
						infoIdx = profile.InfoIdx
					}

					err := planClient.PushTray(context.Background(), printerName, api.TrayPushRequest{
						AmsID:   locIdx,
						TrayID:  trayIdx,
						Color:   strings.ToUpper(colorHex),
						Type:    trayType,
						TempMin: 190,
						TempMax: 240,
						InfoIdx: infoIdx,
					})
					if err != nil {
						fmt.Printf("  %s — error: %v\n", slotLabel, err)
						skipped++
					} else {
						fmt.Printf("  %s — #%d %s (%s #%s)\n", slotLabel, spool.Id, spool.Filament.Name, spool.Filament.Material, colorHex[:6])
						pushed++
					}
				}
			}
			fmt.Println()
		}

		if dryRun {
			fmt.Printf("Dry run: would push %d trays\n", pushed)
		} else {
			fmt.Printf("Synced %d trays", pushed)
			if skipped > 0 {
				fmt.Printf(" (%d errors)", skipped)
			}
			fmt.Println()
		}

		return nil
	},
}

func init() {
	rootCmd.AddCommand(syncCmd)
	syncCmd.Flags().Bool("dry-run", false, "show what would be pushed without pushing")
	syncCmd.Flags().StringP("printer", "p", "", "sync only the specified printer")
}
