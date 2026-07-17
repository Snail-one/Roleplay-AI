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
            "base_url": " https://example.com/v1 ",
            "api_key": " secret ",
            "model": " model-name ",
			"timeout_seconds": 30,
			"max_output_tokens": 2048
        },
        "agent": {
            "system_prompt": " be helpful ",
            "max_iterations": 4
        }
    }`)

	loaded, err := config.Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if loaded.API.Provider != "deepseek" || loaded.API.BaseURL != "https://example.com/v1" || loaded.API.APIKey != "secret" || loaded.API.Model != "model-name" {
		t.Fatalf("API config = %#v", loaded.API)
	}
	if loaded.API.TimeoutSeconds != 30 || loaded.API.MaxOutputTokens != 2048 || loaded.Agent.SystemPrompt != "be helpful" || loaded.Agent.MaxIterations != 4 {
		t.Fatalf("loaded config = %#v", loaded)
	}
}

func TestLoadValidation(t *testing.T) {
	tests := []struct {
		name string
		body string
		want string
	}{
		{name: "missing model", body: `{"api":{}}`, want: "api.model is required"},
		{name: "unknown provider", body: `{"api":{"provider":"other","model":"m"}}`, want: "unsupported api.provider"},
		{name: "unknown MiMo protocol", body: `{"api":{"provider":"mimo","protocol":"other","model":"m"}}`, want: "unsupported MiMo api.protocol"},
		{name: "protocol on other provider", body: `{"api":{"provider":"openai","protocol":"responses","model":"m"}}`, want: "protocol is only supported"},
		{name: "negative timeout", body: `{"api":{"model":"m","timeout_seconds":-1}}`, want: "timeout_seconds cannot be negative"},
		{name: "negative max tokens", body: `{"api":{"model":"m","max_output_tokens":-1}}`, want: "max_output_tokens cannot be negative"},
		{name: "negative iterations", body: `{"api":{"model":"m"},"agent":{"max_iterations":-1}}`, want: "max_iterations cannot be negative"},
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

func TestLoadDefaultsMiMoProtocol(t *testing.T) {
	loaded, err := config.Load(writeConfig(t, `{"api":{"provider":"mimo","model":"mimo-v2.5-pro"}}`))
	if err != nil {
		t.Fatal(err)
	}
	if loaded.API.Protocol != "chat_completions" {
		t.Fatalf("protocol = %q, want chat_completions", loaded.API.Protocol)
	}
}

func TestLoadDefaultsProvider(t *testing.T) {
	loaded, err := config.Load(writeConfig(t, `{"api":{"model":"m"}}`))
	if err != nil {
		t.Fatal(err)
	}
	if loaded.API.Provider != "openai" {
		t.Fatalf("provider = %q, want openai", loaded.API.Provider)
	}
}

func TestLoadNormalizesAnthropicAlias(t *testing.T) {
	loaded, err := config.Load(writeConfig(t, `{"api":{"provider":"anthropic","model":"m"}}`))
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

func writeConfig(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}
