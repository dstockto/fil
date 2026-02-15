package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/dstockto/fil/models"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

var planNewCmd = &cobra.Command{
	Use:     "new [filename]",
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

		plan := models.PlanFile{
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

		// Check if we should move it to central Location
		moveToCentral, _ := cmd.Flags().GetBool("move")
		if moveToCentral {
			if Cfg == nil || Cfg.PlansDir == "" {
				fmt.Println("Warning: plans_dir not configured, cannot move to central Location.")
				return nil
			}

			// Ensure plans dir exists
			if _, err := os.Stat(Cfg.PlansDir); os.IsNotExist(err) {
				_ = os.MkdirAll(Cfg.PlansDir, 0755)
			}

			// Load the plan to update OriginalLocation
			absPath, err := filepath.Abs(filename)
			if err != nil {
				return fmt.Errorf("failed to get absolute path: %w", err)
			}
			plan.OriginalLocation = absPath

			updatedData, err := yaml.Marshal(plan)
			if err != nil {
				return fmt.Errorf("failed to marshal plan: %w", err)
			}
			if err := os.WriteFile(filename, updatedData, 0644); err != nil {
				return fmt.Errorf("failed to update plan file with original location: %w", err)
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
	planCmd.AddCommand(planNewCmd)
	planNewCmd.Flags().BoolP("move", "m", false, "Move the created plan to the central plans directory")
}
