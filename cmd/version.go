package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

// version is set by main via SetVersion before Execute is called.
var version = "dev"

// SetVersion sets the version string for use by the version command and API clients.
func SetVersion(v string) {
	version = v
}

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Print the version of fil",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Println(version)
	},
}

//nolint:gochecknoinits
func init() {
	rootCmd.AddCommand(versionCmd)
}
