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
		fmt.Fprintln(os.Stderr, "Deprecated: use 'fil new plan' instead")
		move, _ := cmd.Flags().GetBool("move")
		if move {
			_ = newPlanCmd.Flags().Set("move", "true")
		}
		return newPlanCmd.RunE(newPlanCmd, args)
	},
}

func init() {
	planCmd.AddCommand(planNewLegacyCmd)
	planNewLegacyCmd.Flags().BoolP("move", "m", false, "Move the created plan to the central plans directory")
}
