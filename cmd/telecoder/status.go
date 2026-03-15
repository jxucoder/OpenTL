package main

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/spf13/cobra"
)

var statusServer string

var statusCmd = &cobra.Command{
	Use:   "status [session-id]",
	Short: "Show session status",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		id := args[0]
		resp, err := http.Get(statusServer + "/api/sessions/" + id)
		if err != nil {
			return fmt.Errorf("connect to server: %w", err)
		}
		defer resp.Body.Close()

		var sess map[string]interface{}
		json.NewDecoder(resp.Body).Decode(&sess)

		out, _ := json.MarshalIndent(sess, "", "  ")
		fmt.Println(string(out))
		return nil
	},
}

func init() {
	statusCmd.Flags().StringVar(&statusServer, "server", "http://localhost:7080", "TeleCoder server address")
}
