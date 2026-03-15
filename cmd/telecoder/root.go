package main

import "github.com/spf13/cobra"

var rootCmd = &cobra.Command{
	Use:   "telecoder",
	Short: "TeleCoder — remote coding sessions on your VPS",
}

func init() {
	rootCmd.AddCommand(serveCmd)
	rootCmd.AddCommand(runCmd)
	rootCmd.AddCommand(listCmd)
	rootCmd.AddCommand(statusCmd)
	rootCmd.AddCommand(stopCmd)
	rootCmd.AddCommand(logsCmd)
	rootCmd.AddCommand(configCmd)
}
