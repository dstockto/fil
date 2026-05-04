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
)

var newPlanCmd = &cobra.Command{
	Use:     "plan [filename]",
	Aliases: []string{"p"},
	Short:   "Create a new plan directly at its destination (server or plans_dir)",
	RunE: func(cmd *cobra.Command, args []string) error {
		// Deprecation notice for --move
		if moveFlag, _ := cmd.Flags().GetBool("move"); moveFlag {
			fmt.Println("Note: --move is no longer needed; plans are created directly at their destination.")
		}

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

		ctx := context.Background()

		// Track where we created the plan for --edit
		var editPath string
		var editRemote bool

		if PlanOps == nil {
			return fmt.Errorf("plan operations not configured (need either plans_server or api_base+plans_dir)")
		}

		// Local-Mode collision check — Remote Mode would surface a server-
		// side collision via SaveAll's HTTP error, but local users prefer
		// an early friendly message.
		if Cfg.PlansServer == "" && Cfg.PlansDir != "" {
			if _, err := os.Stat(filepath.Join(Cfg.PlansDir, filename)); err == nil {
				return fmt.Errorf("file %s already exists", filepath.Join(Cfg.PlansDir, filename))
			}
		}

		if err := PlanOps.SaveAll(ctx, filename, plan); err != nil {
			return fmt.Errorf("failed to save plan: %w", err)
		}

		if Cfg.PlansServer != "" {
			// Upload assembly PDF and patch the plan with the server-side
			// filename. Server-only feature; assemblies live there, not in
			// plans_dir.
			if assemblyFile != "" {
				pdfData, readErr := os.ReadFile(assemblyFile)
				if readErr != nil {
					fmt.Printf("Warning: failed to read assembly PDF %s: %v\n", assemblyFile, readErr)
				} else {
					client := api.NewPlanServerClient(Cfg.PlansServer, version, Cfg.TLSSkipVerify)
					if serverFilename, uploadErr := client.PutAssembly(ctx, filename, pdfData); uploadErr != nil {
						fmt.Printf("Warning: failed to upload assembly PDF: %v\n", uploadErr)
					} else {
						plan.Assembly = serverFilename
						if err := PlanOps.SaveAll(ctx, filename, plan); err != nil {
							fmt.Printf("Warning: assembly uploaded but failed to patch plan YAML: %v\n", err)
						}
						fmt.Printf("Uploaded assembly instructions: %s\n", assemblyFile)
					}
				}
			}
			fmt.Printf("Created plan: <server>/%s\n", filename)
			editRemote = true
		} else {
			fmt.Printf("Created plan: %s\n", FormatPlanPath(filepath.Join(Cfg.PlansDir, filename)))
			editPath = filepath.Join(Cfg.PlansDir, filename)
		}

		// Optional: open in editor
		edit, _ := cmd.Flags().GetBool("edit")
		if edit {
			editor, err := resolveEditor()
			if err != nil {
				return err
			}

			if editRemote {
				// Pull from server, edit in temp, save bytes back through PlanOps.
				client := api.NewPlanServerClient(Cfg.PlansServer, version, Cfg.TLSSkipVerify)
				data, err := client.GetPlan(ctx, filename)
				if err != nil {
					return fmt.Errorf("failed to download plan for editing: %w", err)
				}

				tmpDir, err := os.MkdirTemp("", "fil-edit-*")
				if err != nil {
					return fmt.Errorf("failed to create temp directory: %w", err)
				}
				defer func() { _ = os.RemoveAll(tmpDir) }()

				editPath = filepath.Join(tmpDir, filename)
				if err := os.WriteFile(editPath, data, 0644); err != nil {
					return fmt.Errorf("failed to write temp file: %w", err)
				}
				if err := runEditor(editor, editPath); err != nil {
					return err
				}
				edited, err := os.ReadFile(editPath)
				if err != nil {
					return fmt.Errorf("failed to read edited file: %w", err)
				}
				if err := PlanOps.SaveBytes(ctx, filename, edited); err != nil {
					return fmt.Errorf("failed to save edited plan: %w", err)
				}
				fmt.Printf("Updated plan: <server>/%s\n", filename)
			} else {
				// Local Mode: editor opens the just-created file directly.
				if err := runEditor(editor, editPath); err != nil {
					return err
				}
			}
		}

		return nil
	},
}

func init() {
	newCmd.AddCommand(newPlanCmd)
	newPlanCmd.Flags().BoolP("edit", "e", false, "Open the plan in your editor after creation")
	newPlanCmd.Flags().BoolP("move", "m", false, "Deprecated: plans are now created directly at their destination")
}
