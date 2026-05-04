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
		if Cfg == nil {
			return fmt.Errorf("config not loaded")
		}
		if PlanOps == nil {
			return fmt.Errorf("plan operations not configured (need either plans_server or api_base+plans_dir)")
		}

		plans, err := discoverPlans()
		if err != nil {
			return err
		}
		dp, err := selectPlan("Select plan file to edit", plans)
		if err != nil {
			return err
		}

		editor, err := resolveEditor()
		if err != nil {
			return err
		}

		// Pull the current YAML bytes (from server in Remote Mode, from disk
		// in Local Mode) into a temp file so the editor flow is uniform —
		// the user always edits a temp copy, and SaveBytes writes the
		// result back. Prevents direct PlansDir mutation from cmd/.
		original, err := readPlanBytes(cmd.Context(), dp)
		if err != nil {
			return err
		}

		tmpDir, err := os.MkdirTemp("", "fil-edit-*")
		if err != nil {
			return fmt.Errorf("failed to create temp directory: %w", err)
		}
		defer func() { _ = os.RemoveAll(tmpDir) }()

		editPath := filepath.Join(tmpDir, planFileName(*dp))
		if err := os.WriteFile(editPath, original, 0644); err != nil {
			return fmt.Errorf("failed to write temp file: %w", err)
		}

		if err := runEditor(editor, editPath); err != nil {
			return err
		}

		edited, err := os.ReadFile(editPath)
		if err != nil {
			return fmt.Errorf("failed to read edited file: %w", err)
		}

		if err := PlanOps.SaveBytes(cmd.Context(), planFileName(*dp), edited); err != nil {
			return fmt.Errorf("failed to save edited plan: %w", err)
		}
		fmt.Printf("Updated %s\n", dp.DisplayName)
		return nil
	},
}

// resolveEditor picks an editor command from $VISUAL, $EDITOR, then a small
// fallback list. Returns the raw command string (may include args, e.g.
// "code --wait").
func resolveEditor() (string, error) {
	editor := os.Getenv("VISUAL")
	if editor == "" {
		editor = os.Getenv("EDITOR")
	}
	if editor == "" {
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
		return "", fmt.Errorf("no editor found. Please set $VISUAL or $EDITOR environment variable")
	}
	return editor, nil
}

// runEditor invokes the editor against path, wired to the user's terminal.
func runEditor(editor, path string) error {
	parts := strings.Fields(editor)
	c := exec.Command(parts[0], append(parts[1:], path)...)
	c.Stdin = os.Stdin
	c.Stdout = os.Stdout
	c.Stderr = os.Stderr
	return c.Run()
}

// readPlanBytes returns the current YAML bytes for a discovered plan,
// fetching from the plan-server in Remote Mode and from disk in Local Mode.
func readPlanBytes(ctx context.Context, dp *DiscoveredPlan) ([]byte, error) {
	if dp.Remote {
		client := api.NewPlanServerClient(Cfg.PlansServer, version, Cfg.TLSSkipVerify)
		data, err := client.GetPlan(ctx, dp.RemoteName)
		if err != nil {
			return nil, fmt.Errorf("failed to download remote plan: %w", err)
		}
		return data, nil
	}
	data, err := os.ReadFile(dp.Path)
	if err != nil {
		return nil, fmt.Errorf("failed to read plan: %w", err)
	}
	return data, nil
}

func init() {
	planCmd.AddCommand(planEditCmd)
}
