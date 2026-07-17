package deepseek_test

import (
	"testing"

	"roleloom/internal/ai/provider/deepseek"
)

func TestNew(t *testing.T) {
	backend, err := deepseek.New(deepseek.Config{Model: "deepseek-model"})
	if err != nil {
		t.Fatal(err)
	}
	if backend == nil {
		t.Fatal("New() returned nil backend")
	}
}

func TestNewRequiresModel(t *testing.T) {
	if _, err := deepseek.New(deepseek.Config{}); err == nil {
		t.Fatal("New() expected missing model error")
	}
}
