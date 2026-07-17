package openai_test

import (
	"testing"

	"roleloom/internal/ai/provider/openai"
)

func TestNew(t *testing.T) {
	backend, err := openai.New(openai.Config{Model: "model"})
	if err != nil {
		t.Fatal(err)
	}
	if backend == nil {
		t.Fatal("New() returned nil backend")
	}
}

func TestNewRequiresModel(t *testing.T) {
	if _, err := openai.New(openai.Config{}); err == nil {
		t.Fatal("New() expected missing model error")
	}
}
