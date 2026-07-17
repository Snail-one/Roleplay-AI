package openaicompatible_test

import (
	"testing"

	openaicompatible "roleloom/internal/ai/provider/openai_compatible"
)

func TestNewRequiresAPIURL(t *testing.T) {
	if _, err := openaicompatible.New(openaicompatible.Config{Model: "model"}); err == nil {
		t.Fatal("New() expected missing API URL error")
	}
}

func TestNew(t *testing.T) {
	backend, err := openaicompatible.New(openaicompatible.Config{
		APIURL: "http://localhost:8000/v1/chat/completions",
		Model:  "model",
	})
	if err != nil {
		t.Fatal(err)
	}
	if backend == nil {
		t.Fatal("New() returned nil backend")
	}
}
