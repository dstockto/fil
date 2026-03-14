package cmd

import "github.com/spf13/cobra"

var newCmd = &cobra.Command{
	Use:     "new",
	Aliases: []string{"n"},
	Short:   "Create new resources in Spoolman",
	Long:    `Create new resources such as filaments, spools, and projects in Spoolman.`,
}

//nolint:gochecknoinits
func init() {
	rootCmd.AddCommand(newCmd)
}
