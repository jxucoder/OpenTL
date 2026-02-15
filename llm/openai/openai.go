// Package openai implements llm.Client using the OpenAI Chat Completions API.
package openai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// Client implements llm.Client using the OpenAI Chat Completions API.
type Client struct {
	apiKey string
	model  string
	client *http.Client
}

// New creates a client for the OpenAI API.
// Model defaults to "gpt-4o" if empty.
func New(apiKey, model string) *Client {
	if model == "" {
		model = "gpt-4o"
	}
	return &Client{
		apiKey: apiKey,
		model:  model,
		client: &http.Client{Timeout: 2 * time.Minute},
	}
}

func (c *Client) Complete(ctx context.Context, system, user string) (string, error) {
	var result struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	reqBody := map[string]any{
		"model":      c.model,
		"max_tokens": 4096,
		"messages": []map[string]string{
			{"role": "system", "content": system},
			{"role": "user", "content": user},
		},
	}
	err := doJSONRoundTrip(ctx, c.client, "POST", "https://api.openai.com/v1/chat/completions",
		map[string]string{
			"Content-Type":  "application/json",
			"Authorization": "Bearer " + c.apiKey,
		},
		reqBody, &result)
	if err != nil {
		return "", fmt.Errorf("openai API: %w", err)
	}

	if len(result.Choices) == 0 {
		return "", fmt.Errorf("no choices in response")
	}
	return result.Choices[0].Message.Content, nil
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
