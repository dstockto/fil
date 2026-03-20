package cmd

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/dstockto/fil/api"
	"github.com/manifoldco/promptui"
	"github.com/spf13/cobra"
)

var planAttachCmd = &cobra.Command{
	Use:     "attach",
	Aliases: []string{"a"},
	Short:   "Attach an assembly instructions PDF to a plan",
	RunE: func(cmd *cobra.Command, args []string) error {
		if Cfg == nil || Cfg.PlansServer == "" {
			return fmt.Errorf("plans_server must be configured to attach assembly PDFs")
		}

		plans, err := discoverPlans()
		if err != nil {
			return err
		}

		var dp *DiscoveredPlan
		if len(args) > 0 {
			for i := range plans {
				if plans[i].RemoteName == args[0] || plans[i].DisplayName == args[0] || filepath.Base(plans[i].Path) == args[0] {
					dp = &plans[i]
					break
				}
			}
			if dp == nil {
				return fmt.Errorf("plan %q not found", args[0])
			}
		} else {
			dp, err = selectPlan("Select plan to attach PDF to", plans)
			if err != nil {
				return err
			}
		}

		fileFlag, _ := cmd.Flags().GetString("file")

		var pdfPath string
		if fileFlag != "" {
			pdfPath = fileFlag
		} else {
			// Scan CWD for PDFs
			cwd, err := os.Getwd()
			if err != nil {
				return fmt.Errorf("failed to get current directory: %w", err)
			}

			entries, err := os.ReadDir(cwd)
			if err != nil {
				return fmt.Errorf("failed to read directory: %w", err)
			}

			var pdfs []string
			for _, e := range entries {
				if e.IsDir() {
					continue
				}
				if strings.ToLower(filepath.Ext(e.Name())) == ".pdf" {
					pdfs = append(pdfs, e.Name())
				}
			}

			switch len(pdfs) {
			case 0:
				return fmt.Errorf("no PDF files found in current directory; use --file to specify a path")
			case 1:
				confirmPrompt := promptui.Prompt{
					Label:     fmt.Sprintf("Attach %s", pdfs[0]),
					IsConfirm: true,
					Stdout:    NoBellStdout,
				}
				_, err := confirmPrompt.Run()
				if err != nil {
					fmt.Println("Cancelled.")
					return nil
				}
				pdfPath = pdfs[0]
			default:
				items := append([]string{"None"}, pdfs...)
				prompt := promptui.Select{
					Label:  "Select PDF to attach",
					Items:  items,
					Stdout: NoBellStdout,
				}
				idx, _, err := prompt.Run()
				if err != nil || idx == 0 {
					fmt.Println("No PDF selected.")
					return nil
				}
				pdfPath = pdfs[idx-1]
			}
		}

		data, err := os.ReadFile(pdfPath)
		if err != nil {
			return fmt.Errorf("failed to read PDF file: %w", err)
		}

		// Determine the plan name for the API
		planName := dp.RemoteName
		if planName == "" {
			planName = filepath.Base(dp.Path)
		}

		client := api.NewPlanServerClient(Cfg.PlansServer, version, Cfg.TLSSkipVerify)
		serverFilename, err := client.PutAssembly(context.Background(), planName, data)
		if err != nil {
			return fmt.Errorf("failed to upload assembly PDF: %w", err)
		}

		// Update plan YAML with the server-side assembly filename
		dp.Plan.Assembly = serverFilename
		if err := savePlan(*dp, dp.Plan); err != nil {
			return fmt.Errorf("PDF uploaded but failed to update plan YAML: %w", err)
		}

		fmt.Printf("Attached %s to plan %s\n", filepath.Base(pdfPath), dp.DisplayName)
		return nil
	},
}

func init() {
	planCmd.AddCommand(planAttachCmd)
	planAttachCmd.Flags().StringP("file", "f", "", "path to PDF file to attach")
}
