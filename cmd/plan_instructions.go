package cmd

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"

	"github.com/dstockto/fil/api"
	"github.com/spf13/cobra"
)

var planInstructionsCmd = &cobra.Command{
	Use:     "instructions",
	Aliases: []string{"i"},
	Short:   "Open or download assembly instructions PDF for a plan",
	RunE: func(cmd *cobra.Command, args []string) error {
		if Cfg == nil || Cfg.PlansServer == "" {
			return fmt.Errorf("plans_server must be configured to fetch assembly PDFs")
		}

		plans, err := discoverPlans()
		if err != nil {
			return err
		}

		// Filter to plans with assembly
		var withAssembly []DiscoveredPlan
		for _, p := range plans {
			if p.Plan.Assembly != "" {
				withAssembly = append(withAssembly, p)
				continue
			}
			// For remote plans, check HasAssembly from server listing
			if p.Remote {
				client := api.NewPlanServerClient(Cfg.PlansServer, version, Cfg.TLSSkipVerify)
				summaries, listErr := client.ListPlans(context.Background(), "")
				if listErr == nil {
					for _, s := range summaries {
						if s.Name == p.RemoteName && s.HasAssembly {
							withAssembly = append(withAssembly, p)
							break
						}
					}
				}
			}
		}

		if len(withAssembly) == 0 {
			fmt.Println("No plans with assembly instructions found.")
			return nil
		}

		var dp *DiscoveredPlan
		if len(args) > 0 {
			for i := range withAssembly {
				if withAssembly[i].RemoteName == args[0] || withAssembly[i].DisplayName == args[0] || filepath.Base(withAssembly[i].Path) == args[0] {
					dp = &withAssembly[i]
					break
				}
			}
			if dp == nil {
				return fmt.Errorf("plan %q not found or has no assembly", args[0])
			}
		} else {
			dp, err = selectPlan("Select plan to view instructions", withAssembly)
			if err != nil {
				return err
			}
		}

		planName := dp.RemoteName
		if planName == "" {
			planName = filepath.Base(dp.Path)
		}

		client := api.NewPlanServerClient(Cfg.PlansServer, version, Cfg.TLSSkipVerify)
		data, filename, err := client.GetAssembly(context.Background(), planName)
		if err != nil {
			return fmt.Errorf("failed to download assembly PDF: %w", err)
		}

		if filename == "" {
			filename = planName + "-assembly.pdf"
		}

		outputPath, _ := cmd.Flags().GetString("output")
		if outputPath != "" {
			if err := os.WriteFile(outputPath, data, 0644); err != nil {
				return fmt.Errorf("failed to write PDF: %w", err)
			}
			fmt.Printf("Saved assembly instructions to %s\n", outputPath)
			return nil
		}

		// Write to temp file and open with system viewer
		tmpFile, err := os.CreateTemp("", "fil-assembly-*.pdf")
		if err != nil {
			return fmt.Errorf("failed to create temp file: %w", err)
		}

		if _, err := tmpFile.Write(data); err != nil {
			_ = tmpFile.Close()
			return fmt.Errorf("failed to write temp file: %w", err)
		}
		_ = tmpFile.Close()

		var openCmd *exec.Cmd
		switch runtime.GOOS {
		case "darwin":
			openCmd = exec.Command("open", tmpFile.Name())
		default:
			openCmd = exec.Command("xdg-open", tmpFile.Name())
		}

		if err := openCmd.Start(); err != nil {
			fmt.Printf("Could not open PDF viewer. File saved at: %s\n", tmpFile.Name())
			return nil
		}

		fmt.Printf("Opened %s\n", filename)
		return nil
	},
}

func init() {
	planCmd.AddCommand(planInstructionsCmd)
	planInstructionsCmd.Flags().StringP("output", "o", "", "save PDF to file path instead of opening")
}
