package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

var planNewLegacyCmd = &cobra.Command{
	Use:    "new [filename]",
	Hidden: true,
	Short:  "Deprecated: use 'fil new plan' instead",
	RunE: func(cmd *cobra.Command, args []string) error {
		fmt.Fprintln(os.Stderr, "")
		fmt.Fprintln(os.Stderr, "  \033[31m*** 'fil plan new' has been removed. Use 'fil new plan' instead. ***\033[0m")
		fmt.Fprintln(os.Stderr, "")
		os.Exit(1)
		return nil
	},
}

func init() {
	planCmd.AddCommand(planNewLegacyCmd)
}
