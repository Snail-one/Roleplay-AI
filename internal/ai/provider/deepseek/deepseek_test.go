package deepseek_test

import (
	"testing"

	"roleloom/internal/ai/provider/deepseek"
)

func TestNew(t *testing.T) {
	for _, apiURL := range []string{
		"https://api.deepseek.com/chat/completions",
		"https://api.deepseek.com/anthropic/v1/messages",
	} {
		backend, err := deepseek.New(deepseek.Config{APIURL: apiURL, Model: "deepseek-model"})
		if err != nil {
			t.Fatalf("New(%q) error = %v", apiURL, err)
		}
		if backend == nil {
			t.Fatalf("New(%q) returned nil backend", apiURL)
		}
	}
}

func TestNewRequiresModel(t *testing.T) {
	if _, err := deepseek.New(deepseek.Config{APIURL: "https://api.deepseek.com/chat/completions"}); err == nil {
		t.Fatal("New() expected missing model error")
	}
}

func TestNewRequiresFullEndpoint(t *testing.T) {
	if _, err := deepseek.New(deepseek.Config{APIURL: "https://api.deepseek.com", Model: "model"}); err == nil {
		t.Fatal("New() expected full endpoint URL error")
	}
}
