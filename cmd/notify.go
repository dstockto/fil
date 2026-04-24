/*
Copyright © 2025 David Stockton <dave@davidstockton.com>
*/
package cmd

import (
	"context"
	"fmt"
	"sort"

	"github.com/dstockto/fil/api"
	"github.com/spf13/cobra"
)

var notifyCmd = &cobra.Command{
	Use:   "notify",
	Short: "Notification channel commands",
}

var notifyTestCmd = &cobra.Command{
	Use:   "test",
	Short: "Send a test message through every configured notification channel",
	Long: `Asks the plan server to fire all configured notification channels
(Pushover, ntfy, Voice Monkey) with a canned message and reports the
per-channel outcome. Useful for verifying credentials and device wiring
without needing to start or finish a print.

By default the test is suppressed during configured quiet hours — pass
--force to override.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		if Cfg == nil || Cfg.PlansServer == "" {
			return fmt.Errorf("plans_server must be configured")
		}

		message, _ := cmd.Flags().GetString("message")
		force, _ := cmd.Flags().GetBool("force")

		client := api.NewPlanServerClient(Cfg.PlansServer, version, Cfg.TLSSkipVerify)
		result, err := client.TestNotify(context.Background(), message, force)
		if err != nil {
			return err
		}

		fmt.Printf("Message: %q\n", result.Message)
		if result.QuietHours {
			if result.Forced {
				fmt.Println("Quiet hours: yes (overridden by --force)")
			} else {
				fmt.Println("Quiet hours: yes — notifications suppressed (use --force to override)")
			}
		}

		names := make([]string, 0, len(result.Channels))
		for name := range result.Channels {
			names = append(names, name)
		}
		sort.Strings(names)
		for _, name := range names {
			fmt.Printf("  %-12s %s\n", name, result.Channels[name])
		}
		return nil
	},
}

func init() {
	notifyTestCmd.Flags().String("message", "", "override the canned test message")
	notifyTestCmd.Flags().Bool("force", false, "send even during quiet hours")
	notifyCmd.AddCommand(notifyTestCmd)
	rootCmd.AddCommand(notifyCmd)
}
