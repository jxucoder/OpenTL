package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"text/tabwriter"

	"github.com/spf13/cobra"
)

var listCmd = &cobra.Command{
	Use:   "list",
	Short: "List all sessions",
	RunE:  runList,
}

func init() {
	rootCmd.AddCommand(listCmd)
}

func runList(cmd *cobra.Command, args []string) error {
	resp, err := http.Get(serverURL + "/api/sessions")
	if err != nil {
		return fmt.Errorf("connecting to server: %w\nIs the server running? Start it with: telecoder serve", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("server error (%d): %s", resp.StatusCode, string(body))
	}

	var sessions []struct {
		ID        string `json:"id"`
		Repo      string `json:"repo"`
		Status    string `json:"status"`
		Prompt    string `json:"prompt"`
		PRUrl     string `json:"pr_url"`
		CreatedAt string `json:"created_at"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&sessions); err != nil {
		return fmt.Errorf("parsing response: %w", err)
	}

	if len(sessions) == 0 {
		fmt.Println("No sessions found.")
		return nil
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "ID\tREPO\tSTATUS\tPROMPT\tPR")
	for _, s := range sessions {
		prompt := s.Prompt
		if len(prompt) > 50 {
			prompt = prompt[:47] + "..."
		}
		pr := s.PRUrl
		if pr == "" {
			pr = "-"
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n", s.ID, s.Repo, statusIcon(s.Status), prompt, pr)
	}
	return w.Flush()
}

func statusIcon(status string) string {
	switch status {
	case "pending":
		return "⏳ pending"
	case "running":
		return "🔄 running"
	case "complete":
		return "✅ complete"
	case "error":
		return "❌ error"
	default:
		return status
	}
}
