package provider

import (
	"fmt"
	"strings"
	"time"

	"roleloom/internal/ai"
	"roleloom/internal/ai/provider/claude"
	"roleloom/internal/ai/provider/deepseek"
	"roleloom/internal/ai/provider/mimo"
	"roleloom/internal/ai/provider/openai"
	openaicompatible "roleloom/internal/ai/provider/openai_compatible"
)

type Config struct {
	Provider  string
	Protocol  string
	BaseURL   string
	APIKey    string
	Model     string
	MaxTokens int
	Timeout   time.Duration
}

// New selects a provider backend and wraps it in the common AI calling layer.
func New(config Config) (*ai.Client, error) {
	var (
		backend ai.Backend
		err     error
	)

	switch strings.ToLower(strings.TrimSpace(config.Provider)) {
	case "", "openai":
		backend, err = openai.New(openai.Config{
			BaseURL: config.BaseURL, APIKey: config.APIKey, Model: config.Model,
			MaxTokens: config.MaxTokens, Timeout: config.Timeout,
		})
	case "deepseek":
		backend, err = deepseek.New(deepseek.Config{
			BaseURL: config.BaseURL, APIKey: config.APIKey, Model: config.Model,
			MaxTokens: config.MaxTokens, Timeout: config.Timeout,
		})
	case "mimo":
		backend, err = mimo.New(mimo.Config{
			Protocol: config.Protocol, BaseURL: config.BaseURL,
			APIKey: config.APIKey, Model: config.Model,
			MaxTokens: config.MaxTokens, Timeout: config.Timeout,
		})
	case "claude", "anthropic":
		backend, err = claude.New(claude.Config{
			BaseURL: config.BaseURL, APIKey: config.APIKey, Model: config.Model,
			MaxTokens: config.MaxTokens, Timeout: config.Timeout,
		})
	case "openai_compatible":
		backend, err = openaicompatible.New(openaicompatible.Config{
			BaseURL: config.BaseURL, APIKey: config.APIKey, Model: config.Model,
			MaxTokens: config.MaxTokens, Timeout: config.Timeout,
		})
	default:
		return nil, fmt.Errorf("unsupported model provider %q", config.Provider)
	}
	if err != nil {
		return nil, fmt.Errorf("create %s provider: %w", config.Provider, err)
	}
	return ai.NewClient(backend)
}
