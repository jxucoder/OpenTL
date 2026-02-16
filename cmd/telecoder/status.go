package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/spf13/cobra"
)

var statusCmd = &cobra.Command{
	Use:   "status [session-id]",
	Short: "Get the status of a session",
	Args:  cobra.ExactArgs(1),
	RunE:  runStatus,
}

var logsFollow bool

var logsCmd = &cobra.Command{
	Use:   "logs [session-id]",
	Short: "View session logs",
	Args:  cobra.ExactArgs(1),
	RunE:  runLogs,
}

func init() {
	logsCmd.Flags().BoolVarP(&logsFollow, "follow", "f", false, "Follow log output")
	rootCmd.AddCommand(statusCmd)
	rootCmd.AddCommand(logsCmd)
}

func runStatus(cmd *cobra.Command, args []string) error {
	id := args[0]

	resp, err := http.Get(serverURL + "/api/sessions/" + id)
	if err != nil {
		return fmt.Errorf("connecting to server: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("server error (%d): %s", resp.StatusCode, string(body))
	}

	var sess struct {
		ID        string `json:"id"`
		Repo      string `json:"repo"`
		Prompt    string `json:"prompt"`
		Status    string `json:"status"`
		Branch    string `json:"branch"`
		PRUrl     string `json:"pr_url"`
		PRNumber  int    `json:"pr_number"`
		Result    struct {
			Type    string `json:"type"`
			Content string `json:"content"`
			PRUrl   string `json:"pr_url"`
		} `json:"result"`
		Error     string `json:"error"`
		CreatedAt string `json:"created_at"`
		UpdatedAt string `json:"updated_at"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&sess); err != nil {
		return fmt.Errorf("parsing response: %w", err)
	}

	fmt.Printf("Session:  %s\n", sess.ID)
	fmt.Printf("Repo:     %s\n", sess.Repo)
	fmt.Printf("Status:   %s\n", statusIcon(sess.Status))
	fmt.Printf("Branch:   %s\n", sess.Branch)
	fmt.Printf("Prompt:   %s\n", sess.Prompt)
	fmt.Printf("Created:  %s\n", sess.CreatedAt)
	fmt.Printf("Updated:  %s\n", sess.UpdatedAt)
	if sess.Result.Type != "" {
		fmt.Printf("Result:   %s\n", sess.Result.Type)
	}
	if sess.PRUrl != "" {
		fmt.Printf("PR:       %s\n", sess.PRUrl)
	}
	if sess.Result.Type == "text" && sess.Result.Content != "" {
		content := sess.Result.Content
		if len(content) > 500 {
			content = content[:497] + "..."
		}
		fmt.Printf("Answer:   %s\n", content)
	}
	if sess.Error != "" {
		fmt.Printf("Error:    %s\n", sess.Error)
	}

	return nil
}

func runLogs(cmd *cobra.Command, args []string) error {
	id := args[0]
	if logsFollow {
		return streamEvents(id)
	}
	return streamEvents(id)
}
