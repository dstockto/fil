package cmd

import (
	"context"
	"fmt"

	"github.com/dstockto/fil/api"
	"github.com/spf13/cobra"
)

var planCleanCmd = &cobra.Command{
	Use:   "clean",
	Short: "Remove orphaned assembly PDFs from the server",
	Long:  "Scans the server's assemblies directory for PDF files not referenced by any plan (active, paused, or archived) and removes them.",
	RunE: func(cmd *cobra.Command, args []string) error {
		if Cfg == nil || Cfg.PlansServer == "" {
			return fmt.Errorf("plans_server must be configured to clean assemblies")
		}

		dryRun, _ := cmd.Flags().GetBool("dry-run")
		client := api.NewPlanServerClient(Cfg.PlansServer, version, Cfg.TLSSkipVerify)

		result, err := client.CleanAssemblies(context.Background(), dryRun)
		if err != nil {
			return fmt.Errorf("failed to clean assemblies: %w", err)
		}

		if len(result.Orphans) == 0 {
			fmt.Println("No orphaned assembly PDFs found.")
			return nil
		}

		for _, name := range result.Orphans {
			fmt.Printf("  %s\n", name)
		}

		if dryRun {
			fmt.Printf("Found %d orphaned PDF(s). Use without --dry-run to delete.\n", len(result.Orphans))
		} else {
			fmt.Printf("Removed %d orphaned PDF(s).\n", len(result.Orphans))
		}

		return nil
	},
}

func init() {
	planCmd.AddCommand(planCleanCmd)
	planCleanCmd.Flags().Bool("dry-run", false, "list orphaned PDFs without deleting them")
}
