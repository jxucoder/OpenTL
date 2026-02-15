// Package anthropic implements llm.Client using the Anthropic Messages API.
package anthropic

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// Client implements llm.Client using the Anthropic Messages API.
type Client struct {
	apiKey string
	model  string
	client *http.Client
}

// New creates a client for the Anthropic API.
// Model defaults to "claude-sonnet-4-20250514" if empty.
func New(apiKey, model string) *Client {
	if model == "" {
		model = "claude-sonnet-4-20250514"
	}
	return &Client{
		apiKey: apiKey,
		model:  model,
		client: &http.Client{Timeout: 2 * time.Minute},
	}
}

func (c *Client) Complete(ctx context.Context, system, user string) (string, error) {
	var result struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	}
	reqBody := map[string]any{
		"model":      c.model,
		"max_tokens": 4096,
		"system":     system,
		"messages": []map[string]string{
			{"role": "user", "content": user},
		},
	}
	err := doJSONRoundTrip(ctx, c.client, "POST", "https://api.anthropic.com/v1/messages",
		map[string]string{
			"Content-Type":      "application/json",
			"x-api-key":         c.apiKey,
			"anthropic-version": "2023-06-01",
		},
		reqBody, &result)
	if err != nil {
		return "", fmt.Errorf("anthropic API: %w", err)
	}

	for _, c := range result.Content {
		if c.Type == "text" {
			return c.Text, nil
		}
	}
	return "", fmt.Errorf("no text content in response")
}

func doJSONRoundTrip(
	ctx context.Context,
	client *http.Client,
	method, url string,
	headers map[string]string,
	reqBody any,
	respBody any,
) error {
	jsonBody, err := json.Marshal(reqBody)
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, method, url, bytes.NewReader(jsonBody))
	if err != nil {
		return err
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}

	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("error (%d): %s", resp.StatusCode, string(body))
	}
	if err := json.Unmarshal(body, respBody); err != nil {
		return fmt.Errorf("parsing response: %w", err)
	}
	return nil
}
