package cmd

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/dstockto/fil/api"
	"github.com/dstockto/fil/models"
	"github.com/fatih/color"
	"github.com/spf13/cobra"
)

var planStatusCmd = &cobra.Command{
	Use:     "status",
	Aliases: []string{"s"},
	Short:   "Show what is printing on each printer",
	RunE: func(cmd *cobra.Command, args []string) error {
		if Cfg == nil || len(Cfg.Printers) == 0 {
			fmt.Println("No printers configured.")
			return nil
		}

		watch, _ := cmd.Flags().GetBool("watch")

		if !watch {
			return printStatus()
		}

		// Watch mode: refresh every 5 seconds until interrupted
		ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
		defer stop()

		ticker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()

		// Print immediately
		fmt.Print("\033[2J\033[H") // clear screen, cursor home
		if err := printStatus(); err != nil {
			return err
		}
		fmt.Printf("\n%s  Refreshing every 5s (Ctrl+C to stop)", color.New(color.FgHiBlack).Sprint("⏱"))

		for {
			select {
			case <-ctx.Done():
				fmt.Println()
				return nil
			case <-ticker.C:
				fmt.Print("\033[2J\033[H")
				if err := printStatus(); err != nil {
					return err
				}
				fmt.Printf("\n%s  Refreshing every 5s (Ctrl+C to stop)", color.New(color.FgHiBlack).Sprint("⏱"))
			}
		}
	},
}

func printStatus() error {
	plans, err := discoverPlans()
	if err != nil {
		return err
	}

	// Build map of printer → (project name, plate name, time info)
	type printingInfo struct {
		Project           string
		Plate             string
		StartedAt         string
		EstimatedDuration string
	}
	printerMap := make(map[string][]printingInfo)

	for _, p := range plans {
		for _, proj := range p.Plan.Projects {
			for _, plate := range proj.Plates {
				if plate.Status == "in-progress" && plate.Printer != "" {
					printerMap[plate.Printer] = append(printerMap[plate.Printer], printingInfo{
						Project:           proj.Name,
						Plate:             plate.Name,
						StartedAt:         plate.StartedAt,
						EstimatedDuration: plate.EstimatedDuration,
					})
				}
			}
		}
	}

	// Fetch live printer status from server if available
	liveStatus := make(map[string]api.PrinterStatus)
	var liveStatuses []api.PrinterStatus
	if Cfg.PlansServer != "" {
		client := api.NewPlanServerClient(Cfg.PlansServer, version, Cfg.TLSSkipVerify)
		if statuses, err := client.GetPrinterStatus(context.Background()); err == nil {
			liveStatuses = statuses
			for _, s := range statuses {
				liveStatus[s.Name] = s
			}
		}
	}

	// Split into active and idle, each sorted alphabetically
	var active, idle []string
	for name := range Cfg.Printers {
		if infos, ok := printerMap[name]; ok && len(infos) > 0 {
			active = append(active, name)
		} else {
			idle = append(idle, name)
		}
	}
	sort.Strings(active)
	sort.Strings(idle)

	for _, name := range active {
		infos := printerMap[name]
		// Calculate the visible prefix length: swatch (if any) + "PrinterName: "
		// so subsequent lines can align the project/plate column.
		prefixLen := len(name) + 2 // "Name: "
		if live, ok := liveStatus[name]; ok {
			if activeTrayColorSwatch(live) != "" {
				prefixLen += 3 // "██ " is 2 visible chars + 1 space, but ██ is 2 runes
			}
		}

		for i, info := range infos {
			line := ""

			if i == 0 {
				// Show color swatch if live data has active tray color
				if live, ok := liveStatus[name]; ok {
					if swatch := activeTrayColorSwatch(live); swatch != "" {
						line += swatch + " "
					}
				}
				line += fmt.Sprintf("%s: %s / %s", name, models.Sanitize(info.Project), models.Sanitize(info.Plate))
			} else {
				line += fmt.Sprintf("%s%s / %s", strings.Repeat(" ", prefixLen), models.Sanitize(info.Project), models.Sanitize(info.Plate))
			}

			// Prefer live printer data over fil's time estimate (only on first line)
			if i == 0 {
				if live, ok := liveStatus[name]; ok && live.State == "printing" {
					line += formatLiveStatus(live)
				} else if live, ok := liveStatus[name]; ok && live.State != "idle" && live.State != "offline" {
					line += fmt.Sprintf(" (%s)", live.State)
				} else {
					line += formatTimeInfo(info.StartedAt, info.EstimatedDuration)
				}
			} else {
				line += formatTimeInfo(info.StartedAt, info.EstimatedDuration)
			}
			fmt.Println(line)
		}
	}

	for _, name := range idle {
		// Check if printer reports a non-idle state even though fil has no plate tracked
		if live, ok := liveStatus[name]; ok && live.State != "idle" && live.State != "offline" {
			fmt.Printf("%s: (%s)\n", name, live.State)
		} else {
			fmt.Printf("%s: (idle)\n", name)
		}
	}

	// Check for tray mismatches if we have live data
	if len(liveStatuses) > 0 {
		mismatches := detectMismatches(context.Background(), liveStatuses)
		if len(mismatches) > 0 {
			fmt.Println()
			warn := color.New(color.FgYellow).SprintFunc()
			fmt.Printf("%s %d tray mismatch(es):\n", warn("⚠"), len(mismatches))
			for _, m := range mismatches {
				fmt.Println(m)
			}
		}
	}

	return nil
}

func activeTrayColorSwatch(status api.PrinterStatus) string {
	if color.NoColor || len(status.Trays) == 0 || status.ActiveTray < 0 {
		return ""
	}

	// Find the active tray — active_tray is a global index across AMS units
	// AMS 0 trays 0-3 = indices 0-3, AMS 1 trays 0-3 = indices 4-7, etc.
	amsID := status.ActiveTray / 4
	trayID := status.ActiveTray % 4

	for _, tray := range status.Trays {
		if tray.AmsID == amsID && tray.TrayID == trayID {
			if tray.Color == "" {
				return ""
			}
			hex := strings.TrimPrefix(tray.Color, "#")
			if len(hex) < 6 {
				return ""
			}
			r, _ := strconv.ParseInt(hex[0:2], 16, 16)
			g, _ := strconv.ParseInt(hex[2:4], 16, 16)
			b, _ := strconv.ParseInt(hex[4:6], 16, 16)
			return color.RGB(int(r), int(g), int(b)).Sprintf("██")
		}
	}
	return ""
}

func formatLiveStatus(status api.PrinterStatus) string {
	parts := []string{fmt.Sprintf("%d%%", status.Progress)}

	if status.Layer > 0 && status.TotalLayers > 0 {
		parts = append(parts, fmt.Sprintf("layer %d/%d", status.Layer, status.TotalLayers))
	}

	if status.RemainingMins > 0 {
		eta := time.Now().Add(time.Duration(status.RemainingMins) * time.Minute)
		parts = append(parts, fmt.Sprintf("done ~%s", eta.Format("3:04pm")))
	}

	return fmt.Sprintf(" (%s)", strings.Join(parts, ", "))
}

func formatTimeInfo(startedAt, estimatedDuration string) string {
	if startedAt == "" {
		return ""
	}

	started, err := time.Parse(time.RFC3339, startedAt)
	if err != nil {
		return ""
	}

	startedStr := started.Format("3:04pm")

	if estimatedDuration == "" {
		return fmt.Sprintf(" (started %s)", startedStr)
	}

	dur, err := time.ParseDuration(estimatedDuration)
	if err != nil {
		return fmt.Sprintf(" (started %s)", startedStr)
	}

	eta := started.Add(dur)
	return fmt.Sprintf(" (started %s, done ~%s)", startedStr, eta.Format("3:04pm"))
}

func init() {
	planCmd.AddCommand(planStatusCmd)
	planStatusCmd.Flags().BoolP("watch", "w", false, "continuously refresh status every 5 seconds")
}
