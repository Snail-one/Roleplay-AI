package claude

import (
	"time"

	"roleloom/internal/ai"
	"roleloom/internal/ai/provider/common"
)

type Config struct {
	APIURL    string
	APIKey    string
	Model     string
	MaxTokens int
	Timeout   time.Duration
}

func New(config Config) (ai.Backend, error) {
	return common.NewAnthropicMessages(common.AnthropicMessagesConfig{
		APIURL: config.APIURL, APIKey: config.APIKey,
		APIKeyHeader: "x-api-key", IncludeAnthropicVersion: true,
		Model: config.Model, MaxTokens: config.MaxTokens, Timeout: config.Timeout,
	})
}
