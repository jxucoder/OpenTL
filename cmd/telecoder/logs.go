package main

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/spf13/cobra"
)

var (
	logsFollow bool
	logsServer string
)

var logsCmd = &cobra.Command{
	Use:   "logs [session-id]",
	Short: "Show session logs",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		id := args[0]

		if logsFollow {
			return followLogs(logsServer, id)
		}

		// Fetch existing events.
		resp, err := http.Get(logsServer + "/api/sessions/" + id + "/events?after=0")
		if err != nil {
			return fmt.Errorf("connect to server: %w", err)
		}
		defer resp.Body.Close()

		var events []struct {
			Type string `json:"type"`
			Data string `json:"data"`
		}
		json.NewDecoder(resp.Body).Decode(&events)

		for _, ev := range events {
			fmt.Printf("[%s] %s\n", ev.Type, ev.Data)
		}
		return nil
	},
}

func init() {
	logsCmd.Flags().BoolVar(&logsFollow, "follow", false, "Follow logs in real time")
	logsCmd.Flags().StringVar(&logsServer, "server", "http://localhost:7080", "TeleCoder server address")
}
