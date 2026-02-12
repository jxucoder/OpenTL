package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"

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
	if sess.PRUrl != "" {
		fmt.Printf("PR:       %s\n", sess.PRUrl)
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

	// Non-follow mode: fetch the session status and print stored events.
	// Use the session API to get events as a JSON list rather than SSE,
	// which would block indefinitely for running sessions.
	resp, err := http.Get(serverURL + "/api/sessions/" + id + "/events")
	if err != nil {
		return fmt.Errorf("connecting to server: %w", err)
	}
	defer resp.Body.Close()

	// Read the SSE stream but disconnect after a short deadline so we don't
	// block on running sessions. Read until we either get a "done"/"error"
	// event or run out of buffered data (the server sends historical events
	// first, then flushes).
	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}

		data := strings.TrimPrefix(line, "data: ")
		var event struct {
			Type string `json:"type"`
			Data string `json:"data"`
		}
		if err := json.Unmarshal([]byte(data), &event); err != nil {
			continue
		}

		switch event.Type {
		case "status":
			fmt.Printf("\033[36m[status]\033[0m %s\n", event.Data)
		case "output":
			fmt.Println(event.Data)
		case "error":
			fmt.Fprintf(os.Stderr, "\033[31m[error]\033[0m %s\n", event.Data)
		case "done":
			fmt.Printf("\n\033[32m✓ Done:\033[0m %s\n", event.Data)
			return nil
		}
	}

	return scanner.Err()
}
