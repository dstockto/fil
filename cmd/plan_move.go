package cmd

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/dstockto/fil/api"
	"github.com/dstockto/fil/models"
	"github.com/manifoldco/promptui"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

var planMoveCmd = &cobra.Command{
	Use:     "move [file]",
	Aliases: []string{"mv", "m"},
	Short:   "Move a plan file from CWD into the central plans directory or server",
	RunE: func(cmd *cobra.Command, args []string) error {
		if Cfg == nil {
			return fmt.Errorf("config not loaded")
		}
		if PlanOps == nil {
			return fmt.Errorf("plan operations not configured (need either plans_server or api_base+plans_dir)")
		}

		var path string
		if len(args) > 0 {
			path = args[0]
		} else {
			files, _ := filepath.Glob("*.yaml")
			files2, _ := filepath.Glob("*.yml")
			files = append(files, files2...)

			if len(files) == 0 {
				return fmt.Errorf("no yaml files found in current directory")
			}
			if len(files) == 1 {
				path = files[0]
			} else {
				prompt := promptui.Select{
					Label:             "Select plan file to move",
					Items:             files,
					Stdout:            NoBellStdout,
					StartInSearchMode: true,
					Searcher: func(input string, index int) bool {
						return strings.Contains(strings.ToLower(files[index]), strings.ToLower(input))
					},
				}
				_, result, err := prompt.Run()
				if err != nil {
					return err
				}
				path = result
			}
		}

		data, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("failed to read file: %w", err)
		}
		name := filepath.Base(path)
		ctx := cmd.Context()

		// Save raw bytes through PlanOps. Both modes go through the same
		// path; SaveBytes preserves the user's exact YAML formatting from
		// the CWD file.
		if err := PlanOps.SaveBytes(ctx, name, data); err != nil {
			return fmt.Errorf("failed to save plan: %w", err)
		}

		// Optional assembly PDF upload — server-only feature; in pure Local
		// Mode there's no assemblies endpoint, so this is conditional on
		// PlansServer.
		if Cfg.PlansServer != "" {
			var plan models.PlanFile
			if yamlErr := yaml.Unmarshal(data, &plan); yamlErr == nil && plan.Assembly != "" {
				if err := uploadAssemblyAlongside(ctx, name, path, plan.Assembly); err != nil {
					fmt.Printf("Warning: %v\n", err)
				}
			}
		}

		if err := os.Remove(path); err != nil {
			fmt.Printf("Warning: saved plan but failed to remove local file: %v\n", err)
		}

		if Cfg.PlansServer != "" {
			fmt.Printf("Moved %s to <server>/%s\n", path, name)
		} else {
			fmt.Printf("Moved %s to %s\n", path, filepath.Join(Cfg.PlansDir, name))
		}
		return nil
	},
}

// uploadAssemblyAlongside uploads the PDF that the plan's Assembly field
// names, then patches the plan on the server with the server-side filename
// it returned. Best-effort — assembly upload errors don't fail the move.
func uploadAssemblyAlongside(ctx context.Context, planName, planPath, assemblyName string) error {
	pdfPath := filepath.Join(filepath.Dir(planPath), assemblyName)
	pdfData, err := os.ReadFile(pdfPath)
	if err != nil {
		return fmt.Errorf("read assembly PDF %s: %w", assemblyName, err)
	}
	client := api.NewPlanServerClient(Cfg.PlansServer, version, Cfg.TLSSkipVerify)
	serverFilename, err := client.PutAssembly(ctx, planName, pdfData)
	if err != nil {
		return fmt.Errorf("upload assembly PDF: %w", err)
	}
	// Re-fetch and patch the Assembly field with the server-side name.
	planBytes, err := client.GetPlan(ctx, planName)
	if err != nil {
		return fmt.Errorf("re-fetch plan: %w", err)
	}
	var plan models.PlanFile
	if err := yaml.Unmarshal(planBytes, &plan); err != nil {
		return fmt.Errorf("parse re-fetched plan: %w", err)
	}
	plan.Assembly = serverFilename
	if err := PlanOps.SaveAll(ctx, planName, plan); err != nil {
		return fmt.Errorf("update plan with assembly filename: %w", err)
	}
	fmt.Printf("Uploaded assembly instructions: %s\n", filepath.Base(pdfPath))
	return nil
}

func init() {
	planCmd.AddCommand(planMoveCmd)
}
