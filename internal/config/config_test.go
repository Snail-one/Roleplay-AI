package config_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"roleloom/internal/config"
)

func TestLoad(t *testing.T) {
	path := writeConfig(t, `{
        "api": {
			"provider": " DEEPSEEK ",
			"api_url": " https://example.com/chat/completions ",
            "api_key": " secret ",
            "model": " model-name ",
			"timeout_seconds": 30,
			"max_output_tokens": 2048
        },
		"agent": {
            "system_prompt": " be helpful ",
            "max_iterations": 4
		},
		"telegram": {
			"bot_token": " token ",
			"allowed_user_ids": [123, 456],
			"poll_timeout_seconds": 20
        }
    }`)

	loaded, err := config.Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if loaded.API.Provider != "deepseek" || loaded.API.APIURL != "https://example.com/chat/completions" || loaded.API.APIKey != "secret" || loaded.API.Model != "model-name" {
		t.Fatalf("API config = %#v", loaded.API)
	}
	if loaded.API.TimeoutSeconds != 30 || loaded.API.MaxOutputTokens != 2048 || loaded.Agent.SystemPrompt != "be helpful" || loaded.Agent.MaxIterations != 4 {
		t.Fatalf("loaded config = %#v", loaded)
	}
	if loaded.Telegram.BotToken != "token" || loaded.Telegram.PollTimeoutSeconds != 20 || len(loaded.Telegram.AllowedUserIDs) != 2 {
		t.Fatalf("Telegram config = %#v", loaded.Telegram)
	}
}

func TestLoadValidation(t *testing.T) {
	tests := []struct {
		name string
		body string
		want string
	}{
		{name: "missing model", body: `{"api":{"api_url":"https://example.com/chat/completions"}}`, want: "api.model is required"},
		{name: "missing API URL", body: `{"api":{"model":"m"}}`, want: "api.api_url is required"},
		{name: "unknown provider", body: `{"api":{"provider":"other","api_url":"https://example.com/chat/completions","model":"m"}}`, want: "unsupported api.provider"},
		{name: "negative timeout", body: `{"api":{"api_url":"https://example.com/chat/completions","model":"m","timeout_seconds":-1}}`, want: "timeout_seconds cannot be negative"},
		{name: "negative max tokens", body: `{"api":{"api_url":"https://example.com/chat/completions","model":"m","max_output_tokens":-1}}`, want: "max_output_tokens cannot be negative"},
		{name: "negative iterations", body: `{"api":{"api_url":"https://example.com/chat/completions","model":"m"},"agent":{"max_iterations":-1}}`, want: "max_iterations cannot be negative"},
		{name: "negative Telegram poll timeout", body: `{"api":{"api_url":"https://example.com/chat/completions","model":"m"},"telegram":{"poll_timeout_seconds":-1}}`, want: "poll_timeout_seconds cannot be negative"},
		{name: "unknown field", body: `{"api":{"model":"m","unknown":true}}`, want: "unknown field"},
		{name: "invalid JSON", body: `{`, want: "decode config"},
		{name: "multiple values", body: `{"api":{"model":"m"}} {}`, want: "multiple JSON values"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := config.Load(writeConfig(t, test.body))
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("Load() error = %v, want substring %q", err, test.want)
			}
		})
	}
}

func TestLoadDefaultsProvider(t *testing.T) {
	loaded, err := config.Load(writeConfig(t, `{"api":{"api_url":"https://example.com/chat/completions","model":"m"}}`))
	if err != nil {
		t.Fatal(err)
	}
	if loaded.API.Provider != "openai" {
		t.Fatalf("provider = %q, want openai", loaded.API.Provider)
	}
}

func TestLoadNormalizesAnthropicAlias(t *testing.T) {
	loaded, err := config.Load(writeConfig(t, `{"api":{"provider":"anthropic","api_url":"https://example.com/messages","model":"m"}}`))
	if err != nil {
		t.Fatal(err)
	}
	if loaded.API.Provider != "claude" {
		t.Fatalf("provider = %q, want claude", loaded.API.Provider)
	}
}

func TestLoadMissingFile(t *testing.T) {
	_, err := config.Load(filepath.Join(t.TempDir(), "missing.json"))
	if err == nil || !strings.Contains(err.Error(), "open config file") {
		t.Fatalf("Load() error = %v", err)
	}
}

func TestLoadOrCreateCreatesSecureDefault(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "config.json")
	loaded, created, err := config.LoadOrCreate(path)
	if err != nil {
		t.Fatalf("LoadOrCreate() error = %v", err)
	}
	if !created {
		t.Fatal("LoadOrCreate() created = false, want true")
	}
	if loaded.API.Provider != "openai_compatible" || loaded.API.APIURL != "https://your-api-host/v1/chat/completions" || loaded.API.Model != "your-model" {
		t.Fatalf("default config = %#v", loaded)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if permission := info.Mode().Perm(); permission != 0o600 {
		t.Fatalf("config permission = %o, want 600", permission)
	}
	fromDisk, err := config.Load(path)
	if err != nil {
		t.Fatalf("generated config cannot be loaded: %v", err)
	}
	if fromDisk.Agent.SystemPrompt != config.DefaultSystemPrompt {
		t.Fatalf("system prompt = %q", fromDisk.Agent.SystemPrompt)
	}
	if fromDisk.Telegram.BotToken != "your-telegram-bot-token" || fromDisk.Telegram.PollTimeoutSeconds != 30 {
		t.Fatalf("Telegram config = %#v", fromDisk.Telegram)
	}
}

func TestLoadOrCreatePreservesExistingConfig(t *testing.T) {
	path := writeConfig(t, `{"api":{"provider":"deepseek","api_url":"https://api.deepseek.com/chat/completions","model":"model"}}`)
	loaded, created, err := config.LoadOrCreate(path)
	if err != nil {
		t.Fatal(err)
	}
	if created {
		t.Fatal("LoadOrCreate() overwrote existing config")
	}
	if loaded.API.Provider != "deepseek" || loaded.API.Model != "model" {
		t.Fatalf("loaded config = %#v", loaded)
	}
}

func writeConfig(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}
