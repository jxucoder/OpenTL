package main

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/spf13/cobra"
)

// configKey describes a single configuration value.
type configKey struct {
	Key      string
	Desc     string
	Required bool
	Secret   bool
	Prefix   string // expected prefix for validation (e.g. "ghp_"), empty = no check
}

// allConfigKeys lists every configurable value in display order.
var allConfigKeys = []configKey{
	{"GITHUB_TOKEN", "GitHub personal access token (repo scope)", true, true, ""},
	{"ANTHROPIC_API_KEY", "Anthropic API key", false, true, "sk-ant-"},
	{"OPENAI_API_KEY", "OpenAI API key", false, true, "sk-"},
	{"TELEGRAM_BOT_TOKEN", "Telegram bot token (from @BotFather)", false, true, ""},
	{"TELEGRAM_DEFAULT_REPO", "Default repo for Telegram (owner/repo)", false, false, ""},
	{"SLACK_BOT_TOKEN", "Slack Bot User OAuth Token (xoxb-...)", false, true, "xoxb-"},
	{"SLACK_APP_TOKEN", "Slack App-Level Token (xapp-...)", false, true, "xapp-"},
	{"SLACK_DEFAULT_REPO", "Default repo for Slack (owner/repo)", false, false, ""},
}

// ---------------------------------------------------------------------------
// Cobra commands
// ---------------------------------------------------------------------------

var configCmd = &cobra.Command{
	Use:   "config",
	Short: "Manage OpenTL configuration",
	Long: `Manage OpenTL configuration (tokens, API keys, etc.).

Configuration is stored in ~/.opentl/config.env and can be overridden
by environment variables.

  opentl config setup              Interactive setup wizard
  opentl config set KEY VALUE      Set a single config value
  opentl config show               Show current configuration
  opentl config path               Print config file path`,
}

var configSetupCmd = &cobra.Command{
	Use:   "setup",
	Short: "Interactive setup wizard",
	Long: `Guided setup that walks you through configuring OpenTL step by step.
It groups settings into logical sections and validates your input.`,
	RunE: runConfigSetup,
}

var configSetCmd = &cobra.Command{
	Use:   "set KEY VALUE",
	Short: "Set a config value",
	Long: `Set a single configuration value. Example:
  opentl config set GITHUB_TOKEN ghp_xxxxxxxxxxxx`,
	Args: cobra.ExactArgs(2),
	RunE: runConfigSet,
}

var configShowCmd = &cobra.Command{
	Use:   "show",
	Short: "Show current configuration",
	Long:  "Display all configured values. Secrets are masked.",
	RunE:  runConfigShow,
}

var configPathCmd = &cobra.Command{
	Use:   "path",
	Short: "Print config file path",
	RunE: func(cmd *cobra.Command, args []string) error {
		fmt.Println(configFilePath())
		return nil
	},
}

func init() {
	configCmd.AddCommand(configSetupCmd)
	configCmd.AddCommand(configSetCmd)
	configCmd.AddCommand(configShowCmd)
	configCmd.AddCommand(configPathCmd)
	rootCmd.AddCommand(configCmd)
}

// ---------------------------------------------------------------------------
// Config file helpers
// ---------------------------------------------------------------------------

// configFilePath returns ~/.opentl/config.env.
func configFilePath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(".opentl", "config.env")
	}
	return filepath.Join(home, ".opentl", "config.env")
}

// loadConfigFile reads key=value pairs from the config file.
func loadConfigFile() (map[string]string, error) {
	values := make(map[string]string)
	path := configFilePath()

	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return values, nil
		}
		return nil, err
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		if len(parts) == 2 {
			values[parts[0]] = parts[1]
		}
	}
	return values, scanner.Err()
}

// saveConfigFile writes key=value pairs to the config file.
func saveConfigFile(values map[string]string) error {
	path := configFilePath()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("creating config directory: %w", err)
	}

	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return fmt.Errorf("opening config file: %w", err)
	}
	defer f.Close()

	fmt.Fprintln(f, "# OpenTL configuration")
	fmt.Fprintln(f, "# Managed by: opentl config")
	fmt.Fprintln(f, "# Environment variables override these values.")
	fmt.Fprintln(f)

	// Write in a stable order: known keys first, then any extras.
	written := make(map[string]bool)
	for _, ck := range allConfigKeys {
		if v, ok := values[ck.Key]; ok && v != "" {
			fmt.Fprintf(f, "%s=%s\n", ck.Key, v)
			written[ck.Key] = true
		}
	}

	// Write any remaining keys not in the known list.
	var extras []string
	for k := range values {
		if !written[k] && values[k] != "" {
			extras = append(extras, k)
		}
	}
	sort.Strings(extras)
	for _, k := range extras {
		fmt.Fprintf(f, "%s=%s\n", k, values[k])
	}

	return nil
}

// effectiveValue returns the current value for a key, preferring env vars over config file.
func effectiveValue(key string, fileValues map[string]string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fileValues[key]
}

// maskSecret masks a secret string, showing only the first 4 and last 4 characters.
func maskSecret(s string) string {
	if len(s) <= 12 {
		return strings.Repeat("*", len(s))
	}
	return s[:4] + strings.Repeat("*", len(s)-8) + s[len(s)-4:]
}

// ---------------------------------------------------------------------------
// Interactive helpers
// ---------------------------------------------------------------------------

// wizard holds shared state for the interactive setup.
type wizard struct {
	reader     *bufio.Reader
	fileValues map[string]string
	changed    int // number of values the user entered or changed
}

// newWizard creates a wizard with existing config values loaded.
func newWizard(fileValues map[string]string) *wizard {
	return &wizard{
		reader:     bufio.NewReader(os.Stdin),
		fileValues: fileValues,
	}
}

// askYesNo asks a yes/no question and returns true for yes.
// defaultYes controls what happens when the user presses Enter.
func (w *wizard) askYesNo(prompt string, defaultYes bool) (bool, error) {
	hint := "[Y/n]"
	if !defaultYes {
		hint = "[y/N]"
	}
	fmt.Printf("  %s %s ", prompt, hint)
	input, err := w.reader.ReadString('\n')
	if err != nil {
		return false, err
	}
	input = strings.TrimSpace(strings.ToLower(input))
	if input == "" {
		return defaultYes, nil
	}
	return input == "y" || input == "yes", nil
}

// askValue prompts for a single config value with validation.
// Returns true if a new value was accepted.
func (w *wizard) askValue(ck configKey) (bool, error) {
	current := effectiveValue(ck.Key, w.fileValues)

	// Status indicator.
	status := "\033[31m✗ not set\033[0m"
	if current != "" {
		if ck.Secret {
			status = fmt.Sprintf("\033[32m✓ set\033[0m (%s)", maskSecret(current))
		} else {
			status = fmt.Sprintf("\033[32m✓ set\033[0m (%s)", current)
		}
	}

	fmt.Printf("  %s  %s\n", ck.Key, status)

	for {
		fmt.Print("  Paste value (Enter to keep): ")
		input, err := w.reader.ReadString('\n')
		if err != nil {
			return false, err
		}
		input = strings.TrimSpace(input)

		// Enter = keep current.
		if input == "" {
			return false, nil
		}

		// Validate prefix if defined.
		if ck.Prefix != "" && !strings.HasPrefix(input, ck.Prefix) {
			fmt.Printf("  \033[33m!\033[0m  That doesn't look right — expected prefix \"%s\". Try again or press Enter to skip.\n", ck.Prefix)
			continue
		}

		// Validate repo format for *_DEFAULT_REPO keys.
		if strings.HasSuffix(ck.Key, "_DEFAULT_REPO") {
			if !strings.Contains(input, "/") || strings.HasPrefix(input, "/") {
				fmt.Print("  \033[33m!\033[0m  Expected format: owner/repo (e.g. myorg/myapp). Try again or press Enter to skip.\n")
				continue
			}
		}

		w.fileValues[ck.Key] = input
		w.changed++
		fmt.Printf("  \033[32m✓ saved\033[0m\n")
		return true, nil
	}
}

// ---------------------------------------------------------------------------
// Setup wizard (guided, multi-step)
// ---------------------------------------------------------------------------

func runConfigSetup(cmd *cobra.Command, args []string) error {
	fileValues, err := loadConfigFile()
	if err != nil {
		return fmt.Errorf("reading config: %w", err)
	}

	w := newWizard(fileValues)

	// ── Welcome ──────────────────────────────────────────────────────────
	fmt.Println()
	fmt.Println("  \033[1mOpenTL Setup\033[0m")
	fmt.Println("  ───────────")
	fmt.Println("  This wizard will walk you through configuring OpenTL.")
	fmt.Println("  Press Enter at any prompt to keep the current value.")
	fmt.Println()

	// ── Step 1: GitHub Token (required) ──────────────────────────────────
	fmt.Println("  \033[1mStep 1 of 4 — GitHub Token (required)\033[0m")
	fmt.Println("  OpenTL needs a GitHub personal access token to clone repos and create PRs.")
	fmt.Println("  Create one at: \033[4mhttps://github.com/settings/tokens\033[0m")
	fmt.Println("  Required scopes: \033[1mrepo\033[0m")
	fmt.Println()

	ghKey := findKey("GITHUB_TOKEN")
	for {
		if _, err := w.askValue(ghKey); err != nil {
			return err
		}
		if effectiveValue("GITHUB_TOKEN", w.fileValues) != "" {
			break
		}
		fmt.Println("  \033[33m!\033[0m  GitHub token is required. Please paste your token or Ctrl+C to quit.")
	}
	fmt.Println()

	// ── Step 2: LLM API Key (at least one required) ──────────────────────
	fmt.Println("  \033[1mStep 2 of 4 — LLM API Key (at least one required)\033[0m")
	fmt.Println("  The coding agents need an LLM API key to work.")
	fmt.Println("  You need at least one of Anthropic (Claude) or OpenAI.")
	fmt.Println()

	anthropicKey := findKey("ANTHROPIC_API_KEY")
	openaiKey := findKey("OPENAI_API_KEY")

	if _, err := w.askValue(anthropicKey); err != nil {
		return err
	}
	fmt.Println()
	if _, err := w.askValue(openaiKey); err != nil {
		return err
	}

	if effectiveValue("ANTHROPIC_API_KEY", w.fileValues) == "" &&
		effectiveValue("OPENAI_API_KEY", w.fileValues) == "" {
		fmt.Println()
		fmt.Println("  \033[33m!\033[0m  Warning: No LLM key configured. You'll need at least one to run tasks.")
	}
	fmt.Println()

	// ── Step 3: Telegram (optional) ──────────────────────────────────────
	fmt.Println("  \033[1mStep 3 of 4 — Telegram Bot (optional)\033[0m")
	fmt.Println("  Send tasks to OpenTL from your phone via Telegram.")
	fmt.Println("  Get a bot token from @BotFather on Telegram (takes 30 seconds).")
	fmt.Println()

	setupTelegram, err := w.askYesNo("Set up Telegram?", false)
	if err != nil {
		return err
	}

	if setupTelegram {
		fmt.Println()
		tgToken := findKey("TELEGRAM_BOT_TOKEN")
		if _, err := w.askValue(tgToken); err != nil {
			return err
		}
		fmt.Println()
		tgRepo := findKey("TELEGRAM_DEFAULT_REPO")
		fmt.Println("  Default repo lets you skip --repo in every message.")
		if _, err := w.askValue(tgRepo); err != nil {
			return err
		}
	} else {
		fmt.Println("  Skipped. You can set this up later with: opentl config setup")
	}
	fmt.Println()

	// ── Step 4: Slack (optional) ─────────────────────────────────────────
	fmt.Println("  \033[1mStep 4 of 4 — Slack Bot (optional)\033[0m")
	fmt.Println("  Let your team send tasks via Slack.")
	fmt.Println("  Requires a Slack app with Socket Mode enabled.")
	fmt.Println("  See: docs/slack-setup.md")
	fmt.Println()

	setupSlack, err := w.askYesNo("Set up Slack?", false)
	if err != nil {
		return err
	}

	if setupSlack {
		fmt.Println()
		slackBot := findKey("SLACK_BOT_TOKEN")
		if _, err := w.askValue(slackBot); err != nil {
			return err
		}
		fmt.Println()
		slackApp := findKey("SLACK_APP_TOKEN")
		if _, err := w.askValue(slackApp); err != nil {
			return err
		}
		fmt.Println()
		slackRepo := findKey("SLACK_DEFAULT_REPO")
		fmt.Println("  Default repo lets your team skip --repo in every message.")
		if _, err := w.askValue(slackRepo); err != nil {
			return err
		}
	} else {
		fmt.Println("  Skipped. You can set this up later with: opentl config setup")
	}
	fmt.Println()

	// ── Save ─────────────────────────────────────────────────────────────
	if err := saveConfigFile(w.fileValues); err != nil {
		return err
	}

	// ── Summary ──────────────────────────────────────────────────────────
	fmt.Println("  \033[1mConfiguration Summary\033[0m")
	fmt.Println("  ────────────────────")
	printSummaryLine("GitHub", effectiveValue("GITHUB_TOKEN", w.fileValues) != "")
	printSummaryLine("Anthropic", effectiveValue("ANTHROPIC_API_KEY", w.fileValues) != "")
	printSummaryLine("OpenAI", effectiveValue("OPENAI_API_KEY", w.fileValues) != "")
	printSummaryLine("Telegram", effectiveValue("TELEGRAM_BOT_TOKEN", w.fileValues) != "")
	printSummaryLine("Slack", effectiveValue("SLACK_BOT_TOKEN", w.fileValues) != "" &&
		effectiveValue("SLACK_APP_TOKEN", w.fileValues) != "")
	fmt.Println()
	fmt.Printf("  Saved to %s\n", configFilePath())
	fmt.Println()

	// ── Next steps ───────────────────────────────────────────────────────
	fmt.Println("  \033[1mNext Steps\033[0m")
	fmt.Println("  ──────────")
	fmt.Println("  1. Build the sandbox image (one-time):")
	fmt.Println("       make sandbox-image")
	fmt.Println()
	fmt.Println("  2. Start the server:")
	fmt.Println("       opentl serve")
	fmt.Println()
	fmt.Println("  3. Run your first task:")
	fmt.Println("       opentl run \"fix the typo in README\" --repo owner/repo")
	fmt.Println()

	return nil
}

// findKey looks up a configKey by name.
func findKey(name string) configKey {
	for _, ck := range allConfigKeys {
		if ck.Key == name {
			return ck
		}
	}
	return configKey{Key: name}
}

// printSummaryLine prints a check or cross for a config section.
func printSummaryLine(label string, ok bool) {
	if ok {
		fmt.Printf("  \033[32m✓\033[0m %-12s configured\n", label)
	} else {
		fmt.Printf("  \033[90m-\033[0m %-12s not configured\n", label)
	}
}

// ---------------------------------------------------------------------------
// config set / config show
// ---------------------------------------------------------------------------

// runConfigSet sets a single key=value in the config file.
func runConfigSet(cmd *cobra.Command, args []string) error {
	key, value := args[0], args[1]

	fileValues, err := loadConfigFile()
	if err != nil {
		return fmt.Errorf("reading config: %w", err)
	}

	fileValues[key] = value

	if err := saveConfigFile(fileValues); err != nil {
		return err
	}

	// Check if it's a known secret key.
	isSecret := false
	for _, ck := range allConfigKeys {
		if ck.Key == key && ck.Secret {
			isSecret = true
			break
		}
	}

	if isSecret {
		fmt.Printf("Set %s = %s\n", key, maskSecret(value))
	} else {
		fmt.Printf("Set %s = %s\n", key, value)
	}
	return nil
}

// runConfigShow displays the current effective configuration.
func runConfigShow(cmd *cobra.Command, args []string) error {
	fileValues, err := loadConfigFile()
	if err != nil {
		return fmt.Errorf("reading config: %w", err)
	}

	fmt.Printf("Config file: %s\n\n", configFilePath())

	for _, ck := range allConfigKeys {
		value := effectiveValue(ck.Key, fileValues)
		source := ""
		if os.Getenv(ck.Key) != "" {
			source = " (from env)"
		} else if fileValues[ck.Key] != "" {
			source = " (from config file)"
		}

		display := "(not set)"
		if value != "" {
			if ck.Secret {
				display = maskSecret(value)
			} else {
				display = value
			}
		}

		reqTag := ""
		if ck.Required {
			reqTag = " *"
		}

		fmt.Printf("  %-25s %s%s\n", ck.Key+reqTag, display, source)
	}

	fmt.Println("\n  * = required")
	return nil
}
