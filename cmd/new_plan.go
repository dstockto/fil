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

var newPlanCmd = &cobra.Command{
	Use:     "plan [filename]",
	Aliases: []string{"p"},
	Short:   "Create a new template plan file in the current directory",
	RunE: func(cmd *cobra.Command, args []string) error {
		cwd, err := os.Getwd()
		if err != nil {
			return fmt.Errorf("failed to get current working directory (it may have been deleted): %w", err)
		}
		projectName := filepath.Base(cwd)

		var filename string
		if len(args) > 0 {
			filename = args[0]
			if !strings.HasSuffix(filename, ".yaml") && !strings.HasSuffix(filename, ".yml") {
				filename += ".yaml"
			}
			projectName = ToProjectName(strings.TrimSuffix(strings.TrimSuffix(filename, ".yaml"), ".yml"))
		} else {
			defaultName := projectName
			if isInteractiveAllowed(false) {
				namePrompt := promptui.Prompt{
					Label:   "Plan name",
					Default: defaultName,
					Stdout:  NoBellStdout,
				}
				result, err := namePrompt.Run()
				if err != nil {
					return err
				}
				result = strings.TrimSpace(result)
				if result != "" {
					projectName = result
				}
			}
			filename = projectName + ".yaml"
			projectName = ToProjectName(projectName)
		}

		var plates []models.Plate
		files, err := os.ReadDir(cwd)
		if err == nil {
			for _, f := range files {
				if f.IsDir() {
					continue
				}
				ext := strings.ToLower(filepath.Ext(f.Name()))
				if ext == ".stl" {
					name := strings.TrimSuffix(f.Name(), filepath.Ext(f.Name()))
					filamentName := strings.Map(func(r rune) rune {
						if r >= '0' && r <= '9' {
							return -1
						}
						return r
					}, name)
					filamentName = strings.TrimSpace(filamentName)
					if filamentName == "" {
						filamentName = "Replace Me"
					}

					plates = append(plates, models.Plate{
						Name:   name,
						Status: "todo",
						Needs: []models.PlateRequirement{
							{Name: filamentName, Material: "PLA", Amount: 0},
						},
					})
				}
			}
		}

		if len(plates) == 0 {
			plates = append(plates, models.Plate{
				Name:   "Sample Plate",
				Status: "todo",
				Needs: []models.PlateRequirement{
					{Name: "black", Material: "PLA", Amount: 100},
				},
			})
		}

		// Scan CWD for PDF files
		var pdfFiles []string
		for _, f := range files {
			if f.IsDir() {
				continue
			}
			if strings.ToLower(filepath.Ext(f.Name())) == ".pdf" {
				pdfFiles = append(pdfFiles, f.Name())
			}
		}

		var assemblyFile string
		if len(pdfFiles) == 1 {
			confirmPrompt := promptui.Prompt{
				Label:     fmt.Sprintf("Attach assembly instructions? [%s]", pdfFiles[0]),
				IsConfirm: true,
				Stdout:    NoBellStdout,
			}
			if _, err := confirmPrompt.Run(); err == nil {
				assemblyFile = pdfFiles[0]
			}
		} else if len(pdfFiles) > 1 {
			items := append([]string{"None"}, pdfFiles...)
			prompt := promptui.Select{
				Label:  "Select PDF for assembly instructions",
				Items:  items,
				Stdout: NoBellStdout,
			}
			idx, _, err := prompt.Run()
			if err == nil && idx > 0 {
				assemblyFile = pdfFiles[idx-1]
			}
		}

		plan := models.PlanFile{
			Assembly: assemblyFile,
			Projects: []models.Project{
				{
					Name:   projectName,
					Status: "todo",
					Plates: plates,
				},
			},
		}

		// If filename already exists, try to avoid overwriting by adding a suffix or just erroring
		if _, err := os.Stat(filename); err == nil {
			return fmt.Errorf("file %s already exists", filename)
		}

		out, err := yaml.Marshal(plan)
		if err != nil {
			return err
		}

		err = os.WriteFile(filename, out, 0644)
		if err != nil {
			return err
		}

		fmt.Printf("Created new plan: %s\n", FormatPlanPath(filename))

		// Check if we should move it to central location
		moveToCentral, _ := cmd.Flags().GetBool("move")
		if moveToCentral {
			// If plans_server is configured, upload to server
			if Cfg != nil && Cfg.PlansServer != "" {
				data, readErr := os.ReadFile(filename)
				if readErr != nil {
					return fmt.Errorf("failed to read file for upload: %w", readErr)
				}

				client := api.NewPlanServerClient(Cfg.PlansServer, version, Cfg.TLSSkipVerify)
				if uploadErr := client.PutPlan(context.Background(), filename, data); uploadErr != nil {
					return fmt.Errorf("failed to upload plan to server: %w", uploadErr)
				}

				// Upload assembly PDF if one was selected
				if assemblyFile != "" {
					pdfData, readErr := os.ReadFile(assemblyFile)
					if readErr != nil {
						fmt.Printf("Warning: failed to read assembly PDF %s: %v\n", assemblyFile, readErr)
					} else if serverFilename, uploadErr := client.PutAssembly(context.Background(), filename, pdfData); uploadErr != nil {
						fmt.Printf("Warning: failed to upload assembly PDF: %v\n", uploadErr)
					} else {
						// Update the plan on the server with the server-side assembly filename
						plan.Assembly = serverFilename
						if updatedYAML, marshalErr := yaml.Marshal(plan); marshalErr == nil {
							_ = client.PutPlan(context.Background(), filename, updatedYAML)
						}
						fmt.Printf("Uploaded assembly instructions: %s\n", assemblyFile)
					}
				}

				if removeErr := os.Remove(filename); removeErr != nil {
					fmt.Printf("Warning: uploaded to server but failed to remove local file: %v\n", removeErr)
				}

				fmt.Printf("Moved %s to <server>/%s\n", FormatPlanPath(filename), filename)
				return nil
			}

			if Cfg == nil || Cfg.PlansDir == "" {
				fmt.Println("Warning: plans_dir not configured, cannot move to central Location.")
				return nil
			}

			// Ensure plans dir exists
			if _, err := os.Stat(Cfg.PlansDir); os.IsNotExist(err) {
				_ = os.MkdirAll(Cfg.PlansDir, 0755)
			}

			dest := filepath.Join(Cfg.PlansDir, filename)
			if _, err := os.Stat(dest); err == nil {
				return fmt.Errorf("file %s already exists in central Location", dest)
			}

			err = os.Rename(filename, dest)
			if err != nil {
				return fmt.Errorf("failed to move file: %w", err)
			}
			fmt.Printf("Moved %s to %s\n", FormatPlanPath(filename), FormatPlanPath(dest))
		}

		return nil
	},
}

func init() {
	newCmd.AddCommand(newPlanCmd)
	newPlanCmd.Flags().BoolP("move", "m", false, "Move the created plan to the central plans directory")
}
