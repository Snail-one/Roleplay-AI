package openai

import (
	"strings"
	"time"

	"roleloom/internal/ai"
	"roleloom/internal/ai/provider/common"
)

const defaultBaseURL = "https://api.openai.com/v1"

type Config struct {
	BaseURL   string
	APIKey    string
	Model     string
	MaxTokens int
	Timeout   time.Duration
}

func New(config Config) (ai.Backend, error) {
	baseURL := strings.TrimSpace(config.BaseURL)
	if baseURL == "" {
		baseURL = defaultBaseURL
	}
	return common.NewChatCompletions(common.ChatCompletionsConfig{
		BaseURL:        baseURL,
		APIKey:         config.APIKey,
		Model:          config.Model,
		MaxTokens:      config.MaxTokens,
		MaxTokensField: "max_completion_tokens",
		Timeout:        config.Timeout,
	})
}
