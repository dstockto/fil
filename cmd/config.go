/*
Copyright © 2025 David Stockton <dave@davidstockton.com>
*/
package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/dstockto/fil/api"
	"github.com/spf13/cobra"
)

var configCmd = &cobra.Command{
	Use:   "config",
	Short: "Manage shared configuration",
	Long:  `View, push, and pull shared configuration to/from the plan server.`,
}

var configPullCmd = &cobra.Command{
	Use:   "pull",
	Short: "Pull shared config from the server",
	Long:  `Fetches shared configuration from the plan server and writes it to $HOME/.config/fil/shared-config.json. Local config files always take precedence over pulled config.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		if Cfg == nil || Cfg.PlansServer == "" {
			return fmt.Errorf("plans_server must be configured to pull shared config")
		}

		dryRun, _ := cmd.Flags().GetBool("dry-run")

		client := api.NewPlanServerClient(Cfg.PlansServer, version)
		data, err := client.GetSharedConfig(context.Background())
		if err != nil {
			return fmt.Errorf("failed to fetch shared config: %w", err)
		}

		// Pretty-print the JSON for readability
		var raw json.RawMessage
		if err := json.Unmarshal(data, &raw); err != nil {
			return fmt.Errorf("server returned invalid JSON: %w", err)
		}
		pretty, err := json.MarshalIndent(raw, "", "  ")
		if err != nil {
			return fmt.Errorf("failed to format config: %w", err)
		}

		home, err := os.UserHomeDir()
		if err != nil {
			return fmt.Errorf("failed to determine home directory: %w", err)
		}
		destPath := filepath.Join(home, ".config", "fil", "shared-config.json")

		if dryRun {
			fmt.Printf("Would write to %s:\n%s\n", destPath, pretty)
			return nil
		}

		if err := os.MkdirAll(filepath.Dir(destPath), 0755); err != nil {
			return fmt.Errorf("failed to create config directory: %w", err)
		}
		if err := os.WriteFile(destPath, pretty, 0644); err != nil {
			return fmt.Errorf("failed to write shared config: %w", err)
		}

		fmt.Printf("Shared config written to %s\n", destPath)
		return nil
	},
}

var configPushCmd = &cobra.Command{
	Use:   "push",
	Short: "Push shared config to the server",
	Long:  `Extracts shared configuration fields from the current merged config and uploads them to the plan server.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		if Cfg == nil || Cfg.PlansServer == "" {
			return fmt.Errorf("plans_server must be configured to push shared config")
		}

		dryRun, _ := cmd.Flags().GetBool("dry-run")

		shared := Cfg.ToSharedConfig()
		data, err := json.MarshalIndent(shared, "", "  ")
		if err != nil {
			return fmt.Errorf("failed to marshal shared config: %w", err)
		}

		if dryRun {
			fmt.Printf("Would push to %s:\n%s\n", Cfg.PlansServer, data)
			return nil
		}

		client := api.NewPlanServerClient(Cfg.PlansServer, version)
		if err := client.PutSharedConfig(context.Background(), data); err != nil {
			return fmt.Errorf("failed to push shared config: %w", err)
		}

		fmt.Println("Shared config pushed to server")
		return nil
	},
}

var configShowCmd = &cobra.Command{
	Use:   "show",
	Short: "Display the current merged configuration",
	Long:  `Pretty-prints the current merged configuration as JSON, useful for debugging config merge order.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		if Cfg == nil {
			fmt.Println("{}")
			return nil
		}

		data, err := json.MarshalIndent(Cfg, "", "  ")
		if err != nil {
			return fmt.Errorf("failed to marshal config: %w", err)
		}

		fmt.Println(string(data))
		return nil
	},
}

//nolint:gochecknoinits
func init() {
	rootCmd.AddCommand(configCmd)
	configCmd.AddCommand(configPullCmd)
	configCmd.AddCommand(configPushCmd)
	configCmd.AddCommand(configShowCmd)

	configPullCmd.Flags().Bool("dry-run", false, "show what would be written without writing")
	configPushCmd.Flags().Bool("dry-run", false, "show what would be pushed without pushing")
}
