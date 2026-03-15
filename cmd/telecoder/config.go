package main

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/jxucoder/telecoder/internal/config"
)

var configCmd = &cobra.Command{
	Use:   "config",
	Short: "Show current configuration",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg := config.Load()
		fmt.Printf("Data directory:    %s\n", cfg.DataDir)
		fmt.Printf("Agent command:     %s\n", cfg.AgentCommand)
		fmt.Printf("Listen address:    %s\n", cfg.ListenAddr)
		fmt.Printf("Verify command:    %s\n", cfg.VerifyCommand)
		fmt.Printf("Lint command:      %s\n", cfg.LintCommand)
		fmt.Printf("Workspaces:        %s\n", cfg.WorkspacesDir())
		return nil
	},
}
