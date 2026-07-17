package openai_test

import (
	"testing"

	"roleloom/internal/ai/provider/openai"
)

func TestNew(t *testing.T) {
	for _, apiURL := range []string{
		"https://api.openai.com/v1/chat/completions",
		"https://api.openai.com/v1/responses",
	} {
		backend, err := openai.New(openai.Config{APIURL: apiURL, Model: "model"})
		if err != nil {
			t.Fatalf("New(%q) error = %v", apiURL, err)
		}
		if backend == nil {
			t.Fatalf("New(%q) returned nil backend", apiURL)
		}
	}
}

func TestNewRequiresModel(t *testing.T) {
	if _, err := openai.New(openai.Config{APIURL: "https://api.openai.com/v1/chat/completions"}); err == nil {
		t.Fatal("New() expected missing model error")
	}
}

func TestNewRejectsIncompleteAPIURL(t *testing.T) {
	if _, err := openai.New(openai.Config{APIURL: "https://api.openai.com/v1", Model: "model"}); err == nil {
		t.Fatal("New() expected full endpoint URL error")
	}
}
