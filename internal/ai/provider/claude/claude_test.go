package claude_test

import (
	"testing"

	"roleloom/internal/ai/provider/claude"
)

func TestNew(t *testing.T) {
	backend, err := claude.New(claude.Config{APIURL: "https://api.anthropic.com/v1/messages", Model: "claude-model"})
	if err != nil {
		t.Fatal(err)
	}
	if backend == nil {
		t.Fatal("New() returned nil backend")
	}
}

func TestNewRequiresModel(t *testing.T) {
	if _, err := claude.New(claude.Config{APIURL: "https://api.anthropic.com/v1/messages"}); err == nil {
		t.Fatal("New() expected missing model error")
	}
}
