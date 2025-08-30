/*
Copyright © 2025 David Stockton <dave@davidstockton.com>
*/
package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

// findCmd represents the find command
var findCmd = &cobra.Command{
	Use:     "find",
	Short:   "find a spool based on name or id",
	Long:    `find a spool based on name or id`,
	RunE:    runFind,
	Aliases: []string{"f"},
}

func runFind(cmd *cobra.Command, args []string) error {
	fmt.Println("find called")
	fmt.Println(Cfg.Database)
	return nil
}

func init() {
	rootCmd.AddCommand(findCmd)

	// Here you will define your flags and configuration settings.

	// Cobra supports Persistent Flags which will work for this command
	// and all subcommands, e.g.:
	// findCmd.PersistentFlags().String("foo", "", "A help for foo")

	// Cobra supports local flags which will only run when this command
	// is called directly, e.g.:
	// findCmd.Flags().BoolP("toggle", "t", false, "Help message for toggle")
}
