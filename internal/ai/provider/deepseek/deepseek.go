package deepseek

import (
	"strings"
	"time"

	"roleloom/internal/ai"
	"roleloom/internal/ai/provider/common"
)

const defaultBaseURL = "https://api.deepseek.com"

type Config struct {
	BaseURL   string
	APIKey    string
	Model     string
	MaxTokens int
	Timeout   time.Duration
}

// New creates a DeepSeek backend. The shared Chat Completions transport also
// preserves DeepSeek reasoning_content during tool-call round trips.
func New(config Config) (ai.Backend, error) {
	baseURL := strings.TrimSpace(config.BaseURL)
	if baseURL == "" {
		baseURL = defaultBaseURL
	}
	return common.NewChatCompletions(common.ChatCompletionsConfig{
		BaseURL:   baseURL,
		APIKey:    config.APIKey,
		Model:     config.Model,
		MaxTokens: config.MaxTokens,
		Timeout:   config.Timeout,
	})
}
