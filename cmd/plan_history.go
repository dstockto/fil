package cmd

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/dstockto/fil/api"
	"github.com/dstockto/fil/models"
	"github.com/spf13/cobra"
)

var planHistoryCmd = &cobra.Command{
	Use:     "history",
	Aliases: []string{"hist", "h"},
	Short:   "Show print completion history",
	Long:    "Shows completed prints from the server's history log. Default view is a daily summary; use --detail for per-plate output.",
	RunE: func(cmd *cobra.Command, args []string) error {
		if Cfg == nil || Cfg.PlansServer == "" {
			return fmt.Errorf("plans_server must be configured")
		}

		since, _ := cmd.Flags().GetString("since")
		until, _ := cmd.Flags().GetString("until")
		printer, _ := cmd.Flags().GetString("printer")
		filament, _ := cmd.Flags().GetString("filament")
		limit, _ := cmd.Flags().GetInt("limit")
		detail, _ := cmd.Flags().GetBool("detail")

		ctx := cmd.Context()
		client := api.NewPlanServerClient(Cfg.PlansServer, version, Cfg.TLSSkipVerify)

		entries, err := client.GetHistory(ctx, since, until, printer, limit)
		if err != nil {
			return fmt.Errorf("failed to fetch history: %w", err)
		}

		// Client-side filament filter (server doesn't filter by filament)
		if filament != "" {
			var filtered []api.HistoryEntry
			for _, e := range entries {
				for _, f := range e.Filament {
					if strings.Contains(strings.ToLower(f.Name), strings.ToLower(filament)) ||
						strings.Contains(strings.ToLower(f.Material), strings.ToLower(filament)) {
						filtered = append(filtered, e)
						break
					}
				}
			}
			entries = filtered
		}

		if len(entries) == 0 {
			fmt.Println("No print history found.")
			return nil
		}

		verbose, _ := cmd.Flags().GetBool("verbose")

		if detail || verbose {
			printDetailedHistory(entries, verbose)
		} else {
			printDailySummary(entries)
		}

		return nil
	},
}

func printDetailedHistory(entries []api.HistoryEntry, verbose bool) {
	totalPrints := 0
	var totalDuration time.Duration
	totalFilament := 0.0

	for _, e := range entries {
		ts, _ := completionTime(e)
		date := ts.Format("2006-01-02")

		dur := calcDuration(e)
		durStr := formatDuration(dur)

		filGrams := 0.0
		for _, f := range e.Filament {
			filGrams += f.Amount
		}

		filSummary := formatFilamentSummary(e.Filament)

		printerName := e.Printer
		if printerName == "" {
			printerName = "—"
		}

		fmt.Printf("  %s  %-12s %s / %s  %6s  %s\n",
			date,
			models.Sanitize(printerName),
			models.Sanitize(e.Project),
			models.Sanitize(e.Plate),
			durStr,
			filSummary,
		)

		if verbose && len(e.Filament) > 0 {
			for _, f := range e.Filament {
				name := f.Name
				if name == "" {
					name = f.Material
				}
				fmt.Printf("             %.0fg %s\n", f.Amount, name)
			}
		}

		totalPrints++
		totalDuration += dur
		totalFilament += filGrams
	}

	fmt.Printf("\nTotal: %d prints, %s, %.0fg filament\n", totalPrints, formatDuration(totalDuration), totalFilament)
}

type daySummary struct {
	date     string
	prints   int
	duration time.Duration
	filament float64
}

func printDailySummary(entries []api.HistoryEntry) {
	dayMap := map[string]*daySummary{}
	getDay := func(date string) *daySummary {
		if d, ok := dayMap[date]; ok {
			return d
		}
		d := &daySummary{date: date}
		dayMap[date] = d
		return d
	}

	// Multiple history entries (e.g. a multi-plate batch print) often share
	// the same wall-clock window on the same printer; merge per-printer
	// intervals so we don't double-count physical printer time.
	perPrinter := map[string][]interval{}
	var unknownPrinter []interval

	for _, e := range entries {
		completed, err := completionTime(e)
		if err != nil {
			continue
		}
		completionDay := getDay(completed.Format("2006-01-02"))
		completionDay.prints++
		for _, f := range e.Filament {
			completionDay.filament += f.Amount
		}

		started, serr := time.Parse(time.RFC3339, e.StartedAt)
		if e.StartedAt == "" || serr != nil || !completed.After(started) {
			continue
		}
		iv := interval{start: started, end: completed}
		if e.Printer == "" {
			unknownPrinter = append(unknownPrinter, iv)
		} else {
			perPrinter[e.Printer] = append(perPrinter[e.Printer], iv)
		}
	}

	distribute := func(ivs []interval) {
		for _, iv := range ivs {
			for date, dur := range splitDurationByDay(iv.start, iv.end) {
				getDay(date).duration += dur
			}
		}
	}
	for _, ivs := range perPrinter {
		distribute(mergeIntervals(ivs))
	}
	distribute(unknownPrinter)

	dates := make([]string, 0, len(dayMap))
	for d := range dayMap {
		dates = append(dates, d)
	}
	sort.Strings(dates)

	totalPrints := 0
	var totalDuration time.Duration
	totalFilament := 0.0

	for _, date := range dates {
		d := dayMap[date]
		fmt.Printf("  %s  %d print(s), %s, %.0fg filament\n",
			d.date,
			d.prints,
			formatDuration(d.duration),
			d.filament,
		)
		totalPrints += d.prints
		totalDuration += d.duration
		totalFilament += d.filament
	}

	fmt.Printf("\nTotal: %d prints, %s, %.0fg filament\n", totalPrints, formatDuration(totalDuration), totalFilament)
}

type interval struct {
	start, end time.Time
}

// mergeIntervals collapses overlapping or touching intervals into their union.
// The input slice is not mutated. Intervals are assumed to have end >= start.
func mergeIntervals(ivs []interval) []interval {
	if len(ivs) == 0 {
		return nil
	}
	sorted := make([]interval, len(ivs))
	copy(sorted, ivs)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].start.Before(sorted[j].start)
	})
	merged := []interval{sorted[0]}
	for _, cur := range sorted[1:] {
		last := &merged[len(merged)-1]
		if !cur.start.After(last.end) {
			if cur.end.After(last.end) {
				last.end = cur.end
			}
		} else {
			merged = append(merged, cur)
		}
	}
	return merged
}

// splitDurationByDay distributes the interval [start, end) across local-date
// buckets, returning the duration that fell within each calendar day. Dates
// use the start's timezone so the keys align with how entries are bucketed
// elsewhere in this file.
func splitDurationByDay(start, end time.Time) map[string]time.Duration {
	result := map[string]time.Duration{}
	if !end.After(start) {
		return result
	}
	cursor := start
	for cursor.Before(end) {
		y, m, d := cursor.Date()
		nextMidnight := time.Date(y, m, d, 0, 0, 0, 0, cursor.Location()).AddDate(0, 0, 1)
		segEnd := nextMidnight
		if end.Before(segEnd) {
			segEnd = end
		}
		result[cursor.Format("2006-01-02")] += segEnd.Sub(cursor)
		cursor = segEnd
	}
	return result
}

// completionTime returns the time the print actually finished. Prefers the
// printer-reported FinishedAt (recorded by the live printer connection on
// FINISH transition) and falls back to Timestamp (the moment fil saved the
// entry) for entries that predate that field or were logged with no live
// printer data available.
func completionTime(e api.HistoryEntry) (time.Time, error) {
	if e.FinishedAt != "" {
		return time.Parse(time.RFC3339, e.FinishedAt)
	}
	return time.Parse(time.RFC3339, e.Timestamp)
}

func calcDuration(e api.HistoryEntry) time.Duration {
	if e.StartedAt == "" {
		return 0
	}
	started, err := time.Parse(time.RFC3339, e.StartedAt)
	if err != nil {
		return 0
	}
	completed, err := completionTime(e)
	if err != nil {
		return 0
	}
	d := completed.Sub(started)
	if d < 0 {
		return 0
	}
	return d
}

func formatDuration(d time.Duration) string {
	if d == 0 {
		return "—"
	}
	h := int(d.Hours())
	m := int(d.Minutes()) % 60
	if h > 0 {
		return fmt.Sprintf("%dh%02dm", h, m)
	}
	return fmt.Sprintf("%dm", m)
}

func formatFilamentSummary(filament []api.HistoryFilament) string {
	if len(filament) == 0 {
		return ""
	}
	total := 0.0
	for _, f := range filament {
		total += f.Amount
	}
	return fmt.Sprintf("%.0fg", total)
}

func init() {
	planCmd.AddCommand(planHistoryCmd)
	planHistoryCmd.Flags().String("since", "", "show history from this date (YYYY-MM-DD)")
	planHistoryCmd.Flags().String("until", "", "show history up to this date (YYYY-MM-DD)")
	planHistoryCmd.Flags().StringP("printer", "p", "", "filter by printer name")
	planHistoryCmd.Flags().StringP("filament", "f", "", "filter by filament name or material")
	planHistoryCmd.Flags().IntP("limit", "l", 0, "show only the last N entries")
	planHistoryCmd.Flags().BoolP("detail", "d", false, "show per-plate detail instead of daily summary")
	planHistoryCmd.Flags().BoolP("verbose", "v", false, "show per-plate detail with filament breakdown")
}
