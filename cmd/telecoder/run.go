package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"

	"github.com/spf13/cobra"
)

var (
	runRepo  string
	runAgent string
)

var runCmd = &cobra.Command{
	Use:   "run [prompt]",
	Short: "Run a task in a background sandbox",
	Long: `Create a new session that runs an AI coding agent in a sandbox.
The agent will work on the task and create a pull request if code was changed,
or return a text answer directly if no code changes were needed.

Example:
  telecoder run "add rate limiting to /api/users" --repo myorg/myapp
  telecoder run "what testing framework does this project use?" --repo myorg/myapp`,
	Args: cobra.ExactArgs(1),
	RunE: runRun,
}

func init() {
	runCmd.Flags().StringVarP(&runRepo, "repo", "r", "", "GitHub repository (owner/repo)")
	runCmd.Flags().StringVarP(&runAgent, "agent", "a", "", "Coding agent to use (opencode, claude-code, codex)")
	runCmd.MarkFlagRequired("repo")
	rootCmd.AddCommand(runCmd)
}

func runRun(cmd *cobra.Command, args []string) error {
	prompt := args[0]

	// Create session.
	reqPayload := map[string]string{
		"repo":   runRepo,
		"prompt": prompt,
	}
	if runAgent != "" {
		reqPayload["agent"] = runAgent
	}
	body, _ := json.Marshal(reqPayload)

	resp, err := http.Post(serverURL+"/api/sessions", "application/json", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("connecting to server: %w\nIs the server running? Start it with: telecoder serve", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("server error (%d): %s", resp.StatusCode, string(respBody))
	}

	var result struct {
		ID     string `json:"id"`
		Branch string `json:"branch"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return fmt.Errorf("parsing response: %w", err)
	}

	fmt.Printf("Session %s started (branch: %s)\n", result.ID, result.Branch)
	fmt.Printf("Streaming logs...\n\n")

	// Stream events.
	return streamEvents(result.ID)
}

func streamEvents(sessionID string) error {
	req, _ := http.NewRequest("GET", serverURL+"/api/sessions/"+sessionID+"/events", nil)
	req.Header.Set("Accept", "text/event-stream")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("connecting to event stream: %w", err)
	}
	defer resp.Body.Close()

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
		case "result":
			// Result marker — informational, the done event follows.
		case "done":
			if strings.HasPrefix(event.Data, "http") {
				fmt.Printf("\n\033[32m✓ PR created:\033[0m %s\n", event.Data)
			} else {
				fmt.Printf("\n\033[32m✓ Done:\033[0m\n%s\n", event.Data)
			}
			return nil
		}
	}

	return scanner.Err()
}
