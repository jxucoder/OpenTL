// TeleCoder
//
// An extensible background coding agent framework for engineering teams.
// Send a task, get a PR.
package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

var (
	version   = "dev"
	serverURL string
)

var rootCmd = &cobra.Command{
	Use:   "telecoder",
	Short: "TeleCoder - Background Coding Agent",
	Long: `TeleCoder is an extensible background coding agent framework for engineering teams.
Send a task, get a PR.

  telecoder config setup                           Set up tokens (first time)
  telecoder serve                                  Start the server
  telecoder run "fix the bug" --repo owner/repo    Run a task
  telecoder list                                   List sessions
  telecoder status <id>                            Check session status
  telecoder logs <id> --follow                     Stream session logs`,
	Version: version,
}

func init() {
	rootCmd.PersistentFlags().StringVar(&serverURL, "server", envOr("TELECODER_SERVER", "http://localhost:7080"), "TeleCoder server URL")
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
