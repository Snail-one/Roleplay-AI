package claude

import (
	"strings"
	"time"

	"roleloom/internal/ai"
	"roleloom/internal/ai/provider/common"
)

const defaultBaseURL = "https://api.anthropic.com/v1"

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
	return common.NewAnthropicMessages(common.AnthropicMessagesConfig{
		BaseURL: baseURL, APIKey: config.APIKey,
		APIKeyHeader: "x-api-key", IncludeAnthropicVersion: true,
		Model: config.Model, MaxTokens: config.MaxTokens, Timeout: config.Timeout,
	})
}
