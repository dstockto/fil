package cmd

import (
	"context"
	"fmt"
	"strings"

	"github.com/dstockto/fil/api"
	"github.com/dstockto/fil/models"
	"github.com/fatih/color"
	"github.com/spf13/cobra"
)

// TrayMismatch describes a mismatch between what fil thinks and what the printer reports.
type TrayMismatch struct {
	PrinterName string
	Location    string
	SlotPos     int // 1-based
	FilColor    string
	FilType     string
	FilName     string
	PrinterColor string
	PrinterType  string
	SpoolID      int
}

func (m TrayMismatch) String() string {
	warn := color.New(color.FgYellow).SprintFunc()
	slot := fmt.Sprintf("%s:%d", m.Location, m.SlotPos)

	parts := []string{}
	if !colorsMatch(m.FilColor, m.PrinterColor) {
		parts = append(parts, fmt.Sprintf("color: fil=#%s printer=#%s", normalizeHex(m.FilColor), normalizeHex(m.PrinterColor)))
	}
	if !typesMatch(m.FilType, m.PrinterType) {
		parts = append(parts, fmt.Sprintf("type: fil=%s printer=%s", m.FilType, m.PrinterType))
	}

	return warn(fmt.Sprintf("  %s #%d %s — %s", slot, m.SpoolID, m.FilName, strings.Join(parts, ", ")))
}

// detectMismatches compares printer tray data against Spoolman spool data.
func detectMismatches(ctx context.Context, printerStatuses []api.PrinterStatus) []TrayMismatch {
	if Cfg == nil || Cfg.ApiBase == "" {
		return nil
	}

	spoolmanClient := api.NewClient(Cfg.ApiBase, Cfg.TLSSkipVerify)

	// Load location orders
	orders, err := LoadLocationOrders(ctx, spoolmanClient)
	if err != nil {
		return nil
	}

	// Fetch all spools
	allSpools, err := spoolmanClient.FindSpoolsByName(ctx, "*", nil, nil)
	if err != nil {
		return nil
	}
	spoolByID := make(map[int]*models.FindSpool)
	for i := range allSpools {
		spoolByID[allSpools[i].Id] = &allSpools[i]
	}

	// Build printer tray lookup: printerName -> (amsID, trayID) -> tray
	type trayKey struct {
		amsID  int
		trayID int
	}
	printerTrays := make(map[string]map[trayKey]api.PrinterTrayStatus)
	for _, ps := range printerStatuses {
		trays := make(map[trayKey]api.PrinterTrayStatus)
		for _, t := range ps.Trays {
			trays[trayKey{t.AmsID, t.TrayID}] = t
		}
		printerTrays[ps.Name] = trays
	}

	var mismatches []TrayMismatch

	for printerName, pCfg := range Cfg.Printers {
		if pCfg.Type == "" {
			continue
		}
		trays, ok := printerTrays[printerName]
		if !ok {
			continue
		}

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

				printerTray, ok := trays[trayKey{locIdx, trayIdx}]
				if !ok {
					continue
				}

				filColor := spool.Filament.ColorHex
				filType := spool.Filament.Material

				if !colorsMatch(filColor, printerTray.Color) {
					mismatches = append(mismatches, TrayMismatch{
						PrinterName:  printerName,
						Location:     loc,
						SlotPos:      trayIdx + 1,
						FilColor:     filColor,
						FilType:      filType,
						FilName:      spool.Filament.Name,
						PrinterColor: printerTray.Color,
						PrinterType:  printerTray.Type,
						SpoolID:      spoolID,
					})
				}
			}
		}
	}

	return mismatches
}

// normalizeHex strips # prefix and converts to uppercase 6-char hex.
func normalizeHex(hex string) string {
	hex = strings.TrimPrefix(hex, "#")
	if len(hex) >= 6 {
		hex = hex[:6]
	}
	return strings.ToUpper(hex)
}

func colorsMatch(a, b string) bool {
	return normalizeHex(a) == normalizeHex(b)
}

func typesMatch(a, b string) bool {
	return strings.EqualFold(a, b)
}

var verifyCmd = &cobra.Command{
	Use:   "verify",
	Short: "Check for mismatches between fil and printer tray data",
	RunE: func(cmd *cobra.Command, args []string) error {
		if Cfg == nil || Cfg.PlansServer == "" {
			return fmt.Errorf("plans_server must be configured")
		}

		ctx := cmd.Context()
		planClient := api.NewPlanServerClient(Cfg.PlansServer, version, Cfg.TLSSkipVerify)

		statuses, err := planClient.GetPrinterStatus(ctx)
		if err != nil {
			return fmt.Errorf("failed to fetch printer status: %w", err)
		}

		mismatches := detectMismatches(ctx, statuses)

		if len(mismatches) == 0 {
			fmt.Println("All printer trays match fil data.")
			return nil
		}

		fmt.Printf("Found %d mismatch(es):\n", len(mismatches))
		currentPrinter := ""
		for _, m := range mismatches {
			if m.PrinterName != currentPrinter {
				fmt.Printf("%s:\n", m.PrinterName)
				currentPrinter = m.PrinterName
			}
			fmt.Println(m)
		}

		return nil
	},
}

func init() {
	rootCmd.AddCommand(verifyCmd)
}
