/*
Copyright © 2025 David Stockton <dave@davidstockton.com>
*/
package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

// moveCmd represents the move command
var moveCmd = &cobra.Command{
	Use:     "move",
	Short:   "Moves a spool to a new location",
	Long:    `Moves a spool to a new location.`,
	RunE:    runMove,
	Aliases: []string{"mv", "m", "mov"},
}

func runMove(cmd *cobra.Command, args []string) error {
	// fil m 1 2 3 -t 'A' # moves spool 1, 2, and 3 to location 'AMS A' by using location aliases
	// fil m 1 -t 'A' -p 0 # moves spool 1 to location 'AMS A' in position 0
	fmt.Println("move called")
	return nil
}

func init() {
	rootCmd.AddCommand(moveCmd)

	// Here you will define your flags and configuration settings.

	// Cobra supports Persistent Flags which will work for this command
	// and all subcommands, e.g.:
	// moveCmd.PersistentFlags().String("foo", "", "A help for foo")

	// Cobra supports local flags which will only run when this command
	// is called directly, e.g.:
	// moveCmd.Flags().BoolP("toggle", "t", false, "Help message for toggle")
}
