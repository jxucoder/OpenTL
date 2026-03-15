package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"

	"github.com/spf13/cobra"
)

var (
	runRepo   string
	runFollow bool
	runServer string
)

var runCmd = &cobra.Command{
	Use:   "run [prompt]",
	Short: "Start a coding task",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		prompt := args[0]
		addr := runServer

		// Create session.
		body, _ := json.Marshal(map[string]string{
			"repo":   runRepo,
			"prompt": prompt,
		})
		resp, err := http.Post(addr+"/api/sessions", "application/json", bytes.NewReader(body))
		if err != nil {
			return fmt.Errorf("connect to server: %w", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusCreated {
			msg, _ := io.ReadAll(resp.Body)
			return fmt.Errorf("server error: %s", msg)
		}

		var sess struct {
			ID     string `json:"id"`
			Status string `json:"status"`
		}
		json.NewDecoder(resp.Body).Decode(&sess)
		fmt.Printf("Session %s created\n", sess.ID)

		if !runFollow {
			fmt.Printf("Run 'telecoder logs %s --follow' to watch progress\n", sess.ID)
			return nil
		}

		// Follow logs via SSE.
		return followLogs(addr, sess.ID)
	},
}

func init() {
	runCmd.Flags().StringVar(&runRepo, "repo", "", "Repository URL or path (required)")
	runCmd.Flags().BoolVar(&runFollow, "follow", true, "Follow session logs")
	runCmd.Flags().StringVar(&runServer, "server", "http://localhost:7080", "TeleCoder server address")
	runCmd.MarkFlagRequired("repo")
}

func followLogs(addr, sessionID string) error {
	req, _ := http.NewRequest("GET", addr+"/api/sessions/"+sessionID+"/events", nil)
	req.Header.Set("Accept", "text/event-stream")

	client := &http.Client{Timeout: 0}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	buf := make([]byte, 4096)
	for {
		n, err := resp.Body.Read(buf)
		if n > 0 {
			os.Stdout.Write(buf[:n])
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
	}
	return nil
}

// pollStatus is used by the status command to poll until complete.
func pollStatus(addr, sessionID string) error {
	for {
		resp, err := http.Get(addr + "/api/sessions/" + sessionID)
		if err != nil {
			return err
		}
		var sess struct {
			Status     string `json:"status"`
			ResultType string `json:"result_type"`
			ResultText string `json:"result_text"`
			Error      string `json:"error"`
		}
		json.NewDecoder(resp.Body).Decode(&sess)
		resp.Body.Close()

		switch sess.Status {
		case "complete":
			fmt.Printf("Result: %s\n%s\n", sess.ResultType, sess.ResultText)
			return nil
		case "error":
			return fmt.Errorf("session failed: %s", sess.Error)
		case "stopped":
			fmt.Println("Session was stopped.")
			return nil
		}

		time.Sleep(2 * time.Second)
	}
}
