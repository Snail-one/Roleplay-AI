package openaicompatible_test

import (
	"testing"

	openaicompatible "roleloom/internal/ai/provider/openai_compatible"
)

func TestNewRequiresBaseURL(t *testing.T) {
	if _, err := openaicompatible.New(openaicompatible.Config{Model: "model"}); err == nil {
		t.Fatal("New() expected missing base URL error")
	}
}

func TestNew(t *testing.T) {
	backend, err := openaicompatible.New(openaicompatible.Config{
		BaseURL: "http://localhost:8000/v1",
		Model:   "model",
	})
	if err != nil {
		t.Fatal(err)
	}
	if backend == nil {
		t.Fatal("New() returned nil backend")
	}
}
