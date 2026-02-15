// OpenTL - Open Tech Lead
//
// An open source background coding agent for engineering teams.
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
	Use:   "opentl",
	Short: "OpenTL - Open Tech Lead",
	Long: `OpenTL is an open source background coding agent for engineering teams.
Send a task, get a PR.

  opentl config setup                           Set up tokens (first time)
  opentl serve                                  Start the server
  opentl run "fix the bug" --repo owner/repo    Run a task
  opentl list                                   List sessions
  opentl status <id>                            Check session status
  opentl logs <id> --follow                     Stream session logs`,
	Version: version,
}

func init() {
	rootCmd.PersistentFlags().StringVar(&serverURL, "server", envOr("OPENTL_SERVER", "http://localhost:7080"), "OpenTL server URL")
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
