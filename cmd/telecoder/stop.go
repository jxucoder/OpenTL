package main

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/spf13/cobra"
)

var stopServer string

var stopCmd = &cobra.Command{
	Use:   "stop [session-id]",
	Short: "Stop a running session",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		id := args[0]
		resp, err := http.Post(stopServer+"/api/sessions/"+id+"/stop", "application/json", strings.NewReader("{}"))
		if err != nil {
			return fmt.Errorf("connect to server: %w", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode == http.StatusOK {
			fmt.Printf("Session %s stopped\n", id)
		} else {
			fmt.Printf("Failed to stop session %s (status %d)\n", id, resp.StatusCode)
		}
		return nil
	},
}

func init() {
	stopCmd.Flags().StringVar(&stopServer, "server", "http://localhost:7080", "TeleCoder server address")
}
