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
	Short:   "Move a plan file to the central plans directory or server",
	RunE: func(cmd *cobra.Command, args []string) error {
		if Cfg == nil || (Cfg.PlansDir == "" && Cfg.PlansServer == "") {
			return fmt.Errorf("plans_dir or plans_server must be configured in config.json")
		}

		var path string
		if len(args) > 0 {
			path = args[0]
		} else {
			// Find yaml files in current directory
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

		// If plans_server is configured, upload to server and remove local file
		if Cfg.PlansServer != "" {
			data, err := os.ReadFile(path)
			if err != nil {
				return fmt.Errorf("failed to read file: %w", err)
			}

			client := api.NewPlanServerClient(Cfg.PlansServer, version, Cfg.TLSSkipVerify)
			if err := client.PutPlan(context.Background(), filepath.Base(path), data); err != nil {
				return fmt.Errorf("failed to upload plan to server: %w", err)
			}

			// Upload assembly PDF if the plan references one
			var plan models.PlanFile
			if yamlErr := yaml.Unmarshal(data, &plan); yamlErr == nil && plan.Assembly != "" {
				pdfPath := filepath.Join(filepath.Dir(path), plan.Assembly)
				pdfData, readErr := os.ReadFile(pdfPath)
				if readErr != nil {
					fmt.Printf("Warning: failed to read assembly PDF %s: %v\n", plan.Assembly, readErr)
				} else if serverFilename, uploadErr := client.PutAssembly(context.Background(), filepath.Base(path), pdfData); uploadErr != nil {
					fmt.Printf("Warning: failed to upload assembly PDF: %v\n", uploadErr)
				} else {
					// Update the plan on the server with the server-side assembly filename
					plan.Assembly = serverFilename
					if updatedYAML, marshalErr := yaml.Marshal(plan); marshalErr == nil {
						_ = client.PutPlan(context.Background(), filepath.Base(path), updatedYAML)
					}
					fmt.Printf("Uploaded assembly instructions: %s\n", filepath.Base(pdfPath))
				}
			}

			if err := os.Remove(path); err != nil {
				fmt.Printf("Warning: uploaded to server but failed to remove local file: %v\n", err)
			}

			fmt.Printf("Moved %s to <server>/%s\n", path, filepath.Base(path))
			return nil
		}

		// Ensure plans dir exists
		if _, err := os.Stat(Cfg.PlansDir); os.IsNotExist(err) {
			_ = os.MkdirAll(Cfg.PlansDir, 0755)
		}

		dest := filepath.Join(Cfg.PlansDir, filepath.Base(path))
		if _, err := os.Stat(dest); err == nil {
			return fmt.Errorf("file %s already exists in central Location", dest)
		}

		if err := os.Rename(path, dest); err != nil {
			return fmt.Errorf("failed to move file: %w", err)
		}
		fmt.Printf("Moved %s to %s\n", path, dest)
		return nil
	},
}

func init() {
	planCmd.AddCommand(planMoveCmd)
}
