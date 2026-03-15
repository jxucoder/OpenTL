package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"text/tabwriter"

	"github.com/spf13/cobra"
)

var listServer string

var listCmd = &cobra.Command{
	Use:   "list",
	Short: "List all sessions",
	RunE: func(cmd *cobra.Command, args []string) error {
		resp, err := http.Get(listServer + "/api/sessions")
		if err != nil {
			return fmt.Errorf("connect to server: %w", err)
		}
		defer resp.Body.Close()

		var sessions []struct {
			ID         string `json:"id"`
			Repo       string `json:"repo"`
			Status     string `json:"status"`
			Mode       string `json:"mode"`
			ResultType string `json:"result_type"`
			CreatedAt  string `json:"created_at"`
		}
		json.NewDecoder(resp.Body).Decode(&sessions)

		w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
		fmt.Fprintln(w, "ID\tREPO\tSTATUS\tMODE\tRESULT\tCREATED")
		for _, s := range sessions {
			fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\n",
				s.ID, s.Repo, s.Status, s.Mode, s.ResultType, s.CreatedAt)
		}
		return w.Flush()
	},
}

func init() {
	listCmd.Flags().StringVar(&listServer, "server", "http://localhost:7080", "TeleCoder server address")
}
