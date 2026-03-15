// Package acp provides a client adapter for the Agent Client Protocol.
//
// TeleCoder acts as the ACP client. It spawns a coding agent process
// (e.g. claude --acp) and communicates via JSON-RPC 2.0 over stdio.
package acp

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"

	acpsdk "github.com/coder/acp-go-sdk"
)

// EventHandler is called when the agent sends a session update.
type EventHandler func(update acpsdk.SessionUpdate)

// Client manages the lifecycle of an ACP agent process.
type Client struct {
	command string
	workDir string

	cmd  *exec.Cmd
	conn *acpsdk.ClientSideConnection

	handler EventHandler
	mu      sync.Mutex
	output  strings.Builder // accumulated agent text output
}

// NewClient creates a new ACP client. The command is the shell command to
// launch the agent (e.g. "claude --acp"). The workDir is the workspace
// directory where the agent runs.
func NewClient(command, workDir string, handler EventHandler) *Client {
	return &Client{
		command: command,
		workDir: workDir,
		handler: handler,
	}
}

// telecoderClient implements acpsdk.Client — the callbacks the agent can invoke.
type telecoderClient struct {
	parent *Client
}

func (c *telecoderClient) RequestPermission(_ context.Context, req acpsdk.RequestPermissionRequest) (acpsdk.RequestPermissionResponse, error) {
	// Auto-approve: select the first option that looks like "allow" or just the first option.
	var optionID acpsdk.PermissionOptionId
	for _, opt := range req.Options {
		if opt.Kind == "allow" || strings.EqualFold(opt.Name, "allow") {
			optionID = opt.OptionId
			break
		}
	}
	if optionID == "" && len(req.Options) > 0 {
		optionID = req.Options[0].OptionId
	}
	return acpsdk.RequestPermissionResponse{
		Outcome: acpsdk.NewRequestPermissionOutcomeSelected(optionID),
	}, nil
}

func (c *telecoderClient) SessionUpdate(_ context.Context, n acpsdk.SessionNotification) error {
	// Extract text from agent message chunks and accumulate.
	if n.Update.AgentMessageChunk != nil {
		if n.Update.AgentMessageChunk.Content.Text != nil {
			c.parent.mu.Lock()
			c.parent.output.WriteString(n.Update.AgentMessageChunk.Content.Text.Text)
			c.parent.mu.Unlock()
		}
	}

	if c.parent.handler != nil {
		c.parent.handler(n.Update)
	}
	return nil
}

func (c *telecoderClient) ReadTextFile(_ context.Context, req acpsdk.ReadTextFileRequest) (acpsdk.ReadTextFileResponse, error) {
	data, err := os.ReadFile(req.Path)
	if err != nil {
		return acpsdk.ReadTextFileResponse{}, err
	}
	return acpsdk.ReadTextFileResponse{Content: string(data)}, nil
}

func (c *telecoderClient) WriteTextFile(_ context.Context, req acpsdk.WriteTextFileRequest) (acpsdk.WriteTextFileResponse, error) {
	err := os.WriteFile(req.Path, []byte(req.Content), 0o644)
	return acpsdk.WriteTextFileResponse{}, err
}

func (c *telecoderClient) CreateTerminal(_ context.Context, _ acpsdk.CreateTerminalRequest) (acpsdk.CreateTerminalResponse, error) {
	return acpsdk.CreateTerminalResponse{}, nil
}

func (c *telecoderClient) KillTerminalCommand(_ context.Context, _ acpsdk.KillTerminalCommandRequest) (acpsdk.KillTerminalCommandResponse, error) {
	return acpsdk.KillTerminalCommandResponse{}, nil
}

func (c *telecoderClient) TerminalOutput(_ context.Context, _ acpsdk.TerminalOutputRequest) (acpsdk.TerminalOutputResponse, error) {
	return acpsdk.TerminalOutputResponse{}, nil
}

func (c *telecoderClient) ReleaseTerminal(_ context.Context, _ acpsdk.ReleaseTerminalRequest) (acpsdk.ReleaseTerminalResponse, error) {
	return acpsdk.ReleaseTerminalResponse{}, nil
}

func (c *telecoderClient) WaitForTerminalExit(_ context.Context, _ acpsdk.WaitForTerminalExitRequest) (acpsdk.WaitForTerminalExitResponse, error) {
	return acpsdk.WaitForTerminalExitResponse{}, nil
}

// Connect starts the agent process and performs the ACP handshake.
func (c *Client) Connect(ctx context.Context) error {
	parts := strings.Fields(c.command)
	if len(parts) == 0 {
		return fmt.Errorf("empty agent command")
	}

	c.cmd = exec.CommandContext(ctx, parts[0], parts[1:]...)
	c.cmd.Dir = c.workDir
	c.cmd.Env = append(os.Environ(), "ACP=1")

	stdin, err := c.cmd.StdinPipe()
	if err != nil {
		return fmt.Errorf("stdin pipe: %w", err)
	}

	stdout, err := c.cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("stdout pipe: %w", err)
	}

	c.cmd.Stderr = os.Stderr

	if err := c.cmd.Start(); err != nil {
		return fmt.Errorf("start agent: %w", err)
	}

	tc := &telecoderClient{parent: c}
	c.conn = acpsdk.NewClientSideConnection(tc, stdin, stdout)

	// Perform the ACP handshake.
	_, err = c.conn.Initialize(ctx, acpsdk.InitializeRequest{
		ProtocolVersion: acpsdk.ProtocolVersionNumber,
		ClientCapabilities: acpsdk.ClientCapabilities{
			Fs: acpsdk.FileSystemCapability{
				ReadTextFile:  true,
				WriteTextFile: true,
			},
			Terminal: true,
		},
	})
	if err != nil {
		c.Close()
		return fmt.Errorf("acp initialize: %w", err)
	}

	return nil
}

// NewSession creates a new ACP session.
func (c *Client) NewSession(ctx context.Context) (string, error) {
	resp, err := c.conn.NewSession(ctx, acpsdk.NewSessionRequest{
		Cwd: c.workDir,
	})
	if err != nil {
		return "", fmt.Errorf("acp new session: %w", err)
	}
	return string(resp.SessionId), nil
}

// Prompt sends a user message and blocks until the agent responds.
// Session updates are delivered via the EventHandler callback.
func (c *Client) Prompt(ctx context.Context, sessionID, prompt string) (string, error) {
	c.mu.Lock()
	c.output.Reset()
	c.mu.Unlock()

	resp, err := c.conn.Prompt(ctx, acpsdk.PromptRequest{
		SessionId: acpsdk.SessionId(sessionID),
		Prompt:    []acpsdk.ContentBlock{acpsdk.TextBlock(prompt)},
	})
	if err != nil {
		return "", fmt.Errorf("acp prompt: %w", err)
	}

	c.mu.Lock()
	text := c.output.String()
	c.mu.Unlock()

	_ = resp
	return text, nil
}

// Cancel sends a cancellation notification.
func (c *Client) Cancel(ctx context.Context, sessionID string) error {
	return c.conn.Cancel(ctx, acpsdk.CancelNotification{
		SessionId: acpsdk.SessionId(sessionID),
	})
}

// Output returns accumulated agent text output.
func (c *Client) Output() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.output.String()
}

// Close stops the agent process and cleans up.
func (c *Client) Close() error {
	if c.cmd != nil && c.cmd.Process != nil {
		_ = c.cmd.Process.Kill()
		_ = c.cmd.Wait()
	}
	return nil
}

// Done returns a channel that closes when the agent disconnects.
func (c *Client) Done() <-chan struct{} {
	if c.conn != nil {
		return c.conn.Done()
	}
	ch := make(chan struct{})
	close(ch)
	return ch
}
