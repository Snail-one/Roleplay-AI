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
			if name == "openai_compatible" {
				config.BaseURL = "http://localhost:8000/v1"
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
