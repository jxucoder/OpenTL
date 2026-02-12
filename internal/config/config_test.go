package config_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jxucoder/opentl/internal/config"
)

// clearConfigEnv unsets all environment variables that Load reads so each
// sub-test starts from a clean slate.  t.Setenv already restores values
// after the test, but we also need to make sure variables from the outer
// process don't leak into "defaults" tests.
func clearConfigEnv(t *testing.T) {
	t.Helper()
	for _, key := range []string{
		"OPENTL_ADDR",
		"OPENTL_DATA_DIR",
		"OPENTL_DOCKER_IMAGE",
		"OPENTL_DOCKER_NETWORK",
		"GITHUB_TOKEN",
		"ANTHROPIC_API_KEY",
		"OPENAI_API_KEY",
		"SLACK_BOT_TOKEN",
		"SLACK_APP_TOKEN",
		"SLACK_DEFAULT_REPO",
		"TELEGRAM_BOT_TOKEN",
		"TELEGRAM_DEFAULT_REPO",
	} {
		t.Setenv(key, "")
		os.Unsetenv(key)
	}
}

// ---------------------------------------------------------------------------
// Load
// ---------------------------------------------------------------------------

func TestLoad_Defaults(t *testing.T) {
	clearConfigEnv(t)

	// Use a temp dir so MkdirAll does not fail and we don't pollute $HOME.
	tmpDir := t.TempDir()
	t.Setenv("OPENTL_DATA_DIR", tmpDir)

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("Load() returned unexpected error: %v", err)
	}

	if cfg.ServerAddr != ":7080" {
		t.Errorf("ServerAddr = %q, want %q", cfg.ServerAddr, ":7080")
	}
	if cfg.DataDir != tmpDir {
		t.Errorf("DataDir = %q, want %q", cfg.DataDir, tmpDir)
	}
	wantDB := filepath.Join(tmpDir, "opentl.db")
	if cfg.DatabasePath != wantDB {
		t.Errorf("DatabasePath = %q, want %q", cfg.DatabasePath, wantDB)
	}
	if cfg.DockerImage != "opentl-sandbox" {
		t.Errorf("DockerImage = %q, want %q", cfg.DockerImage, "opentl-sandbox")
	}
	if cfg.DockerNetwork != "opentl-net" {
		t.Errorf("DockerNetwork = %q, want %q", cfg.DockerNetwork, "opentl-net")
	}
	if cfg.GitHubToken != "" {
		t.Errorf("GitHubToken = %q, want empty", cfg.GitHubToken)
	}
	if cfg.AnthropicAPIKey != "" {
		t.Errorf("AnthropicAPIKey = %q, want empty", cfg.AnthropicAPIKey)
	}
	if cfg.OpenAIAPIKey != "" {
		t.Errorf("OpenAIAPIKey = %q, want empty", cfg.OpenAIAPIKey)
	}
	if cfg.SlackBotToken != "" {
		t.Errorf("SlackBotToken = %q, want empty", cfg.SlackBotToken)
	}
	if cfg.SlackAppToken != "" {
		t.Errorf("SlackAppToken = %q, want empty", cfg.SlackAppToken)
	}
	if cfg.TelegramBotToken != "" {
		t.Errorf("TelegramBotToken = %q, want empty", cfg.TelegramBotToken)
	}
}

func TestLoad_CustomEnvVars(t *testing.T) {
	clearConfigEnv(t)

	tmpDir := t.TempDir()

	t.Setenv("OPENTL_ADDR", ":9090")
	t.Setenv("OPENTL_DATA_DIR", tmpDir)
	t.Setenv("OPENTL_DOCKER_IMAGE", "my-sandbox:latest")
	t.Setenv("OPENTL_DOCKER_NETWORK", "custom-net")
	t.Setenv("GITHUB_TOKEN", "ghp_test123")
	t.Setenv("ANTHROPIC_API_KEY", "sk-ant-test")
	t.Setenv("OPENAI_API_KEY", "sk-openai-test")
	t.Setenv("SLACK_BOT_TOKEN", "xoxb-test")
	t.Setenv("SLACK_APP_TOKEN", "xapp-test")
	t.Setenv("SLACK_DEFAULT_REPO", "owner/repo")
	t.Setenv("TELEGRAM_BOT_TOKEN", "123456:ABC")
	t.Setenv("TELEGRAM_DEFAULT_REPO", "owner/tg-repo")

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("Load() returned unexpected error: %v", err)
	}

	checks := []struct {
		field string
		got   string
		want  string
	}{
		{"ServerAddr", cfg.ServerAddr, ":9090"},
		{"DataDir", cfg.DataDir, tmpDir},
		{"DatabasePath", cfg.DatabasePath, filepath.Join(tmpDir, "opentl.db")},
		{"GitHubToken", cfg.GitHubToken, "ghp_test123"},
		{"AnthropicAPIKey", cfg.AnthropicAPIKey, "sk-ant-test"},
		{"OpenAIAPIKey", cfg.OpenAIAPIKey, "sk-openai-test"},
		{"DockerImage", cfg.DockerImage, "my-sandbox:latest"},
		{"DockerNetwork", cfg.DockerNetwork, "custom-net"},
		{"SlackBotToken", cfg.SlackBotToken, "xoxb-test"},
		{"SlackAppToken", cfg.SlackAppToken, "xapp-test"},
		{"SlackDefaultRepo", cfg.SlackDefaultRepo, "owner/repo"},
		{"TelegramBotToken", cfg.TelegramBotToken, "123456:ABC"},
		{"TelegramDefaultRepo", cfg.TelegramDefaultRepo, "owner/tg-repo"},
	}
	for _, c := range checks {
		if c.got != c.want {
			t.Errorf("%s = %q, want %q", c.field, c.got, c.want)
		}
	}
}

func TestLoad_CreatesDataDir(t *testing.T) {
	clearConfigEnv(t)

	base := t.TempDir()
	nested := filepath.Join(base, "a", "b", "c")
	t.Setenv("OPENTL_DATA_DIR", nested)

	_, err := config.Load()
	if err != nil {
		t.Fatalf("Load() returned unexpected error: %v", err)
	}

	info, statErr := os.Stat(nested)
	if statErr != nil {
		t.Fatalf("data dir was not created: %v", statErr)
	}
	if !info.IsDir() {
		t.Fatal("data dir path exists but is not a directory")
	}
}

// ---------------------------------------------------------------------------
// Validate
// ---------------------------------------------------------------------------

func TestValidate_MissingGitHubToken(t *testing.T) {
	cfg := &config.Config{
		GitHubToken:     "",
		AnthropicAPIKey: "sk-ant-test",
	}

	err := cfg.Validate()
	if err == nil {
		t.Fatal("Validate() should return an error when GITHUB_TOKEN is missing")
	}
	if !strings.Contains(err.Error(), "GITHUB_TOKEN") {
		t.Errorf("error message %q should mention GITHUB_TOKEN", err.Error())
	}
}

func TestValidate_MissingLLMKeys(t *testing.T) {
	cfg := &config.Config{
		GitHubToken:     "ghp_test",
		AnthropicAPIKey: "",
		OpenAIAPIKey:    "",
	}

	err := cfg.Validate()
	if err == nil {
		t.Fatal("Validate() should return an error when both LLM keys are missing")
	}
	if !strings.Contains(err.Error(), "ANTHROPIC_API_KEY") && !strings.Contains(err.Error(), "OPENAI_API_KEY") {
		t.Errorf("error message %q should mention the LLM key requirement", err.Error())
	}
}

func TestValidate_ValidWithAnthropic(t *testing.T) {
	cfg := &config.Config{
		GitHubToken:     "ghp_test",
		AnthropicAPIKey: "sk-ant-test",
	}

	if err := cfg.Validate(); err != nil {
		t.Errorf("Validate() returned unexpected error: %v", err)
	}
}

func TestValidate_ValidWithOpenAI(t *testing.T) {
	cfg := &config.Config{
		GitHubToken:  "ghp_test",
		OpenAIAPIKey: "sk-openai-test",
	}

	if err := cfg.Validate(); err != nil {
		t.Errorf("Validate() returned unexpected error: %v", err)
	}
}

func TestValidate_ValidWithBothLLMKeys(t *testing.T) {
	cfg := &config.Config{
		GitHubToken:     "ghp_test",
		AnthropicAPIKey: "sk-ant-test",
		OpenAIAPIKey:    "sk-openai-test",
	}

	if err := cfg.Validate(); err != nil {
		t.Errorf("Validate() returned unexpected error: %v", err)
	}
}

// ---------------------------------------------------------------------------
// SlackEnabled
// ---------------------------------------------------------------------------

func TestSlackEnabled_True(t *testing.T) {
	cfg := &config.Config{
		SlackBotToken: "xoxb-test",
		SlackAppToken: "xapp-test",
	}
	if !cfg.SlackEnabled() {
		t.Error("SlackEnabled() = false, want true when both tokens are set")
	}
}

func TestSlackEnabled_MissingBotToken(t *testing.T) {
	cfg := &config.Config{
		SlackBotToken: "",
		SlackAppToken: "xapp-test",
	}
	if cfg.SlackEnabled() {
		t.Error("SlackEnabled() = true, want false when SlackBotToken is empty")
	}
}

func TestSlackEnabled_MissingAppToken(t *testing.T) {
	cfg := &config.Config{
		SlackBotToken: "xoxb-test",
		SlackAppToken: "",
	}
	if cfg.SlackEnabled() {
		t.Error("SlackEnabled() = true, want false when SlackAppToken is empty")
	}
}

func TestSlackEnabled_BothMissing(t *testing.T) {
	cfg := &config.Config{}
	if cfg.SlackEnabled() {
		t.Error("SlackEnabled() = true, want false when both tokens are empty")
	}
}

// ---------------------------------------------------------------------------
// TelegramEnabled
// ---------------------------------------------------------------------------

func TestTelegramEnabled_True(t *testing.T) {
	cfg := &config.Config{
		TelegramBotToken: "123456:ABC",
	}
	if !cfg.TelegramEnabled() {
		t.Error("TelegramEnabled() = false, want true when TelegramBotToken is set")
	}
}

func TestTelegramEnabled_False(t *testing.T) {
	cfg := &config.Config{
		TelegramBotToken: "",
	}
	if cfg.TelegramEnabled() {
		t.Error("TelegramEnabled() = true, want false when TelegramBotToken is empty")
	}
}

// ---------------------------------------------------------------------------
// SandboxEnv
// ---------------------------------------------------------------------------

func TestSandboxEnv_BothKeys(t *testing.T) {
	cfg := &config.Config{
		GitHubToken:     "ghp_test",
		AnthropicAPIKey: "sk-ant-test",
		OpenAIAPIKey:    "sk-openai-test",
	}

	env := cfg.SandboxEnv()

	want := map[string]string{
		"GITHUB_TOKEN":     "ghp_test",
		"ANTHROPIC_API_KEY": "sk-ant-test",
		"OPENAI_API_KEY":    "sk-openai-test",
	}

	if len(env) != len(want) {
		t.Fatalf("SandboxEnv() returned %d entries, want %d", len(env), len(want))
	}

	got := make(map[string]string)
	for _, e := range env {
		parts := strings.SplitN(e, "=", 2)
		if len(parts) != 2 {
			t.Fatalf("malformed env entry: %q", e)
		}
		got[parts[0]] = parts[1]
	}

	for k, v := range want {
		if got[k] != v {
			t.Errorf("SandboxEnv %s = %q, want %q", k, got[k], v)
		}
	}
}

func TestSandboxEnv_OnlyAnthropic(t *testing.T) {
	cfg := &config.Config{
		GitHubToken:     "ghp_test",
		AnthropicAPIKey: "sk-ant-test",
	}

	env := cfg.SandboxEnv()

	if len(env) != 2 {
		t.Fatalf("SandboxEnv() returned %d entries, want 2", len(env))
	}

	assertEnvContains(t, env, "GITHUB_TOKEN", "ghp_test")
	assertEnvContains(t, env, "ANTHROPIC_API_KEY", "sk-ant-test")
	assertEnvNotContainsKey(t, env, "OPENAI_API_KEY")
}

func TestSandboxEnv_OnlyOpenAI(t *testing.T) {
	cfg := &config.Config{
		GitHubToken:  "ghp_test",
		OpenAIAPIKey: "sk-openai-test",
	}

	env := cfg.SandboxEnv()

	if len(env) != 2 {
		t.Fatalf("SandboxEnv() returned %d entries, want 2", len(env))
	}

	assertEnvContains(t, env, "GITHUB_TOKEN", "ghp_test")
	assertEnvContains(t, env, "OPENAI_API_KEY", "sk-openai-test")
	assertEnvNotContainsKey(t, env, "ANTHROPIC_API_KEY")
}

func TestSandboxEnv_NoLLMKeys(t *testing.T) {
	cfg := &config.Config{
		GitHubToken: "ghp_test",
	}

	env := cfg.SandboxEnv()

	if len(env) != 1 {
		t.Fatalf("SandboxEnv() returned %d entries, want 1", len(env))
	}

	assertEnvContains(t, env, "GITHUB_TOKEN", "ghp_test")
}

func TestSandboxEnv_AlwaysIncludesGitHubToken(t *testing.T) {
	cfg := &config.Config{
		GitHubToken: "",
	}

	env := cfg.SandboxEnv()

	// GITHUB_TOKEN is always present, even when empty.
	assertEnvContains(t, env, "GITHUB_TOKEN", "")
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func assertEnvContains(t *testing.T, env []string, key, value string) {
	t.Helper()
	target := key + "=" + value
	for _, e := range env {
		if e == target {
			return
		}
	}
	t.Errorf("env slice %v does not contain %q", env, target)
}

func assertEnvNotContainsKey(t *testing.T, env []string, key string) {
	t.Helper()
	prefix := key + "="
	for _, e := range env {
		if strings.HasPrefix(e, prefix) {
			t.Errorf("env slice should not contain key %s, but found %q", key, e)
		}
	}
}
