package cmd

import (
	"context"
	"fmt"
	"strconv"
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
	Long:  "Compares fil's filament data against what each printer reports. Use -v to show all trays side by side.",
	RunE: func(cmd *cobra.Command, args []string) error {
		if Cfg == nil || Cfg.PlansServer == "" {
			return fmt.Errorf("plans_server must be configured")
		}

		verbose, _ := cmd.Flags().GetBool("verbose")
		profiles, _ := cmd.Flags().GetBool("profiles")

		if profiles {
			return printProfileMappings(cmd.Context())
		}

		if Cfg.PlansServer == "" {
			return fmt.Errorf("plans_server must be configured")
		}

		ctx := cmd.Context()
		planClient := api.NewPlanServerClient(Cfg.PlansServer, version, Cfg.TLSSkipVerify)

		statuses, err := planClient.GetPrinterStatus(ctx)
		if err != nil {
			return fmt.Errorf("failed to fetch printer status: %w", err)
		}

		if verbose {
			return printFullTrayView(ctx, statuses)
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

func printFullTrayView(ctx context.Context, printerStatuses []api.PrinterStatus) error {
	if Cfg == nil || Cfg.ApiBase == "" {
		return fmt.Errorf("api_base must be configured")
	}

	spoolmanClient := api.NewClient(Cfg.ApiBase, Cfg.TLSSkipVerify)

	orders, err := LoadLocationOrders(ctx, spoolmanClient)
	if err != nil {
		return fmt.Errorf("failed to load location orders: %w", err)
	}

	allSpools, err := spoolmanClient.FindSpoolsByName(ctx, "*", nil, nil)
	if err != nil {
		return fmt.Errorf("failed to fetch spools: %w", err)
	}
	spoolByID := make(map[int]*models.FindSpool)
	for i := range allSpools {
		spoolByID[allSpools[i].Id] = &allSpools[i]
	}

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

	good := color.New(color.FgGreen).SprintFunc()
	warn := color.New(color.FgYellow).SprintFunc()
	dim := color.New(color.FgHiBlack).SprintFunc()

	for printerName, pCfg := range Cfg.Printers {
		if pCfg.Type == "" {
			continue
		}
		trays, hasTrayData := printerTrays[printerName]

		fmt.Printf("%s:\n", printerName)

		if !hasTrayData {
			fmt.Println("  (no live data available)")
			fmt.Println()
			continue
		}

		// First pass: collect row data and find max widths
		type rowData struct {
			match        string
			slotLabel    string
			filSwatch    string
			filInfo      string
			printerSwatch string
			printerInfo  string
		}
		var rows []rowData
		maxSlot := 0
		maxFil := 0

		for locIdx, loc := range pCfg.Locations {
			ids := orders[loc]

			for trayIdx := range ids {
				spoolID := ids[trayIdx]
				slotLabel := fmt.Sprintf("%s:%d", loc, trayIdx+1)
				printerTray, hasPrinter := trays[trayKey{locIdx, trayIdx}]

				filInfo := "(empty)"
				filColor := ""
				if spoolID != EmptySlot {
					if spool, ok := spoolByID[spoolID]; ok {
						filColor = normalizeHex(spool.Filament.ColorHex)
						filInfo = fmt.Sprintf("#%d %s (%s #%s)", spool.Id, spool.Filament.Name, spool.Filament.Material, filColor)
					}
				}

				printerInfo := "(no data)"
				printerColor := ""
				if hasPrinter {
					printerColor = normalizeHex(printerTray.Color)
					if printerTray.Color != "" {
						printerInfo = fmt.Sprintf("%s #%s", printerTray.Type, printerColor)
					} else {
						printerInfo = "(empty)"
					}
				}

				match := " "
				if spoolID == EmptySlot {
					match = dim("·")
				} else if filColor != "" && printerColor != "" {
					if colorsMatch(filColor, printerColor) {
						match = good("✓")
					} else {
						match = warn("✗")
					}
				}

				filSwatch := "  "
				printerSwatch := "  "
				if !color.NoColor {
					if filColor != "" {
						filSwatch = hexSwatch(filColor)
					}
					if printerColor != "" {
						printerSwatch = hexSwatch(printerColor)
					}
				}

				if len(slotLabel) > maxSlot {
					maxSlot = len(slotLabel)
				}
				if len(filInfo) > maxFil {
					maxFil = len(filInfo)
				}

				rows = append(rows, rowData{match, slotLabel, filSwatch, filInfo, printerSwatch, printerInfo})
			}
		}

		// Second pass: print with consistent alignment
		filFmt := fmt.Sprintf("%%-%ds", maxFil)
		slotFmt := fmt.Sprintf("%%-%ds", maxSlot)
		for _, r := range rows {
			fmt.Printf("  %s "+slotFmt+"  %s "+filFmt+"  %s %s %s\n",
				r.match, r.slotLabel,
				r.filSwatch, r.filInfo,
				dim("│"),
				r.printerSwatch, r.printerInfo,
			)
		}
		fmt.Println()
	}

	return nil
}

func hexSwatch(hex string) string {
	hex = strings.TrimPrefix(hex, "#")
	if len(hex) < 6 {
		return "  "
	}
	r, _ := strconv.ParseInt(hex[0:2], 16, 16)
	g, _ := strconv.ParseInt(hex[2:4], 16, 16)
	b, _ := strconv.ParseInt(hex[4:6], 16, 16)
	return color.RGB(int(r), int(g), int(b)).Sprintf("██")
}

func printProfileMappings(ctx context.Context) error {
	if Cfg == nil || Cfg.ApiBase == "" {
		return fmt.Errorf("api_base must be configured")
	}

	spoolmanClient := api.NewClient(Cfg.ApiBase, Cfg.TLSSkipVerify)
	allSpools, err := spoolmanClient.FindSpoolsByName(ctx, "*", nil, nil)
	if err != nil {
		return fmt.Errorf("failed to fetch spools: %w", err)
	}

	// Deduplicate by filament ID (many spools share the same filament)
	type filamentKey struct {
		vendor   string
		name     string
		material string
	}
	seen := make(map[filamentKey]bool)

	good := color.New(color.FgGreen).SprintFunc()
	warn := color.New(color.FgYellow).SprintFunc()

	matched := 0
	unmatched := 0

	for _, spool := range allSpools {
		key := filamentKey{spool.Filament.Vendor.Name, spool.Filament.Name, spool.Filament.Material}
		if seen[key] {
			continue
		}
		seen[key] = true

		profile := LookupFilamentProfile(spool.Filament.Vendor.Name, spool.Filament.Name, spool.Filament.Material)
		if profile != nil {
			pName := ProfileName(profile.InfoIdx)
			if pName == "" {
				pName = profile.TrayType
			}
			fmt.Printf("  %s  %-20s %-40s %-15s → %-10s %s\n",
				good("✓"),
				spool.Filament.Vendor.Name,
				spool.Filament.Name,
				spool.Filament.Material,
				profile.InfoIdx,
				pName,
			)
			matched++
		} else {
			fmt.Printf("  %s  %-20s %-40s %-15s → (no match)\n",
				warn("?"),
				spool.Filament.Vendor.Name,
				spool.Filament.Name,
				spool.Filament.Material,
			)
			unmatched++
		}
	}

	fmt.Printf("\n%d matched, %d unmatched\n", matched, unmatched)
	return nil
}

func init() {
	rootCmd.AddCommand(verifyCmd)
	verifyCmd.Flags().BoolP("verbose", "v", false, "show full tray comparison for all printers")
	verifyCmd.Flags().Bool("profiles", false, "show filament profile mapping for all filaments")
}
