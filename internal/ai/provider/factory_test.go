package provider_test

import (
	"strings"
	"testing"

	"roleloom/internal/ai/provider"
)

func TestNewAcceptsSupportedProviders(t *testing.T) {
	for _, name := range []string{"openai", "openai_compatible", "deepseek", "anthropic", "claude", "mimo"} {
		t.Run(name, func(t *testing.T) {
			config := provider.Config{Provider: name, Model: "model"}
			switch name {
			case "openai":
				config.APIURL = "https://api.openai.com/v1/chat/completions"
			case "openai_compatible":
				config.APIURL = "http://localhost:8000/v1/chat/completions"
			case "deepseek":
				config.APIURL = "https://api.deepseek.com/chat/completions"
			case "anthropic", "claude":
				config.APIURL = "https://api.anthropic.com/v1/messages"
			case "mimo":
				config.APIURL = "https://api.xiaomimimo.com/v1/chat/completions"
			}
			client, err := provider.New(config)
			if err != nil {
				t.Fatalf("New() error = %v", err)
			}
			if client == nil {
				t.Fatal("New() returned nil client")
			}
		})
	}
}

func TestNewRejectsUnsupportedProvider(t *testing.T) {
	_, err := provider.New(provider.Config{Provider: "unknown", Model: "model"})
	if err == nil || !strings.Contains(err.Error(), "unsupported model provider") {
		t.Fatalf("New() error = %v", err)
	}
}

func TestNewAcceptsOpenAIResponses(t *testing.T) {
	client, err := provider.New(provider.Config{
		Provider: "openai", APIURL: "https://api.openai.com/v1/responses", Model: "model",
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if client == nil {
		t.Fatal("New() returned nil client")
	}
}
