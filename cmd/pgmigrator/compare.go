package main

import (
	"fmt"

	"github.com/spf13/cobra"
)

var compareCmd = &cobra.Command{
	Use:   "compare",
	Short: "Compare source and destination schemas and data",
	Long:  `Compare validates that the destination database matches the source in both schema and data.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		fmt.Println("compare: not yet implemented")
		return nil
	},
}

func init() {
	rootCmd.AddCommand(compareCmd)
}
