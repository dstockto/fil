package cmd

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/dstockto/fil/api"
	"github.com/spf13/cobra"
)

var planEditCmd = &cobra.Command{
	Use:     "edit",
	Aliases: []string{"ed", "e"},
	Short:   "Edit an active plan file",
	RunE: func(cmd *cobra.Command, args []string) error {
		var dp *DiscoveredPlan
		if len(args) > 0 {
			dp = &DiscoveredPlan{Path: args[0], DisplayName: FormatPlanPath(args[0])}
		} else {
			plans, err := discoverPlans()
			if err != nil {
				return err
			}
			dp, err = selectPlan("Select plan file to edit", plans)
			if err != nil {
				return err
			}
		}

		editor := os.Getenv("VISUAL")
		if editor == "" {
			editor = os.Getenv("EDITOR")
		}
		if editor == "" {
			// Fallback to common editors
			for _, e := range []string{"vim", "vi", "nano"} {
				if _, err := os.Stat("/usr/bin/" + e); err == nil {
					editor = e
					break
				}
				if _, err := os.Stat("/usr/local/bin/" + e); err == nil {
					editor = e
					break
				}
			}
		}
		if editor == "" {
			return fmt.Errorf("no editor found. Please set $VISUAL or $EDITOR environment variable")
		}

		editPath := dp.Path

		// For remote plans, download to temp file, edit, then upload
		if dp.Remote {
			client := api.NewPlanServerClient(Cfg.PlansServer, version, Cfg.TLSSkipVerify)
			data, err := client.GetPlan(context.Background(), dp.RemoteName)
			if err != nil {
				return fmt.Errorf("failed to download remote plan: %w", err)
			}

			tmpDir, err := os.MkdirTemp("", "fil-edit-*")
			if err != nil {
				return fmt.Errorf("failed to create temp directory: %w", err)
			}
			defer func() { _ = os.RemoveAll(tmpDir) }()

			editPath = filepath.Join(tmpDir, dp.RemoteName)
			if err := os.WriteFile(editPath, data, 0644); err != nil {
				return fmt.Errorf("failed to write temp file: %w", err)
			}
		}

		// Handle editor with arguments (e.g. "code --wait")
		parts := strings.Fields(editor)
		editorCmd := parts[0]
		editorArgs := append(parts[1:], editPath)

		c := exec.Command(editorCmd, editorArgs...)
		c.Stdin = os.Stdin
		c.Stdout = os.Stdout
		c.Stderr = os.Stderr

		if err := c.Run(); err != nil {
			return err
		}

		// For remote plans, upload the edited file back
		if dp.Remote {
			data, err := os.ReadFile(editPath)
			if err != nil {
				return fmt.Errorf("failed to read edited file: %w", err)
			}

			client := api.NewPlanServerClient(Cfg.PlansServer, version, Cfg.TLSSkipVerify)
			if err := client.PutPlan(context.Background(), dp.RemoteName, data); err != nil {
				return fmt.Errorf("failed to upload edited plan: %w", err)
			}
			fmt.Printf("Updated remote plan %s\n", dp.DisplayName)
		}

		return nil
	},
}

func init() {
	planCmd.AddCommand(planEditCmd)
}
