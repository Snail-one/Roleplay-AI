package openaicompatible

import (
	"time"

	"roleloom/internal/ai"
	"roleloom/internal/ai/provider/common"
)

type Config struct {
	BaseURL   string
	APIKey    string
	Model     string
	MaxTokens int
	Timeout   time.Duration
}

func New(config Config) (ai.Backend, error) {
	return common.NewChatCompletions(common.ChatCompletionsConfig{
		BaseURL:   config.BaseURL,
		APIKey:    config.APIKey,
		Model:     config.Model,
		MaxTokens: config.MaxTokens,
		Timeout:   config.Timeout,
	})
}
