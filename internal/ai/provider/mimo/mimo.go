package mimo

import (
	"fmt"
	"strings"
	"time"

	"roleloom/internal/ai"
	"roleloom/internal/ai/provider/common"
)

const (
	defaultOpenAIBaseURL    = "https://api.xiaomimimo.com/v1"
	defaultAnthropicBaseURL = "https://api.xiaomimimo.com/anthropic/v1"
	ProtocolChatCompletions = "chat_completions"
	ProtocolResponses       = "responses"
	ProtocolAnthropic       = "anthropic"
)

type Config struct {
	Protocol  string
	BaseURL   string
	APIKey    string
	Model     string
	MaxTokens int
	Timeout   time.Duration
}

func New(config Config) (ai.Backend, error) {
	baseURL := strings.TrimSpace(config.BaseURL)
	protocol := strings.ToLower(strings.TrimSpace(config.Protocol))
	if protocol == "" {
		protocol = ProtocolChatCompletions
	}

	switch protocol {
	case ProtocolChatCompletions:
		if baseURL == "" {
			baseURL = defaultOpenAIBaseURL
		}
		return common.NewChatCompletions(common.ChatCompletionsConfig{
			BaseURL:        baseURL,
			APIKey:         config.APIKey,
			Model:          config.Model,
			MaxTokens:      config.MaxTokens,
			MaxTokensField: "max_completion_tokens",
			Timeout:        config.Timeout,
		})
	case ProtocolResponses:
		if baseURL == "" {
			baseURL = defaultOpenAIBaseURL
		}
		return newResponsesClient(responsesConfig{
			BaseURL: baseURL, APIKey: config.APIKey, Model: config.Model,
			MaxTokens: config.MaxTokens, Timeout: config.Timeout,
		})
	case ProtocolAnthropic:
		if baseURL == "" {
			baseURL = defaultAnthropicBaseURL
		}
		return common.NewAnthropicMessages(common.AnthropicMessagesConfig{
			BaseURL: baseURL, APIKey: config.APIKey,
			APIKeyHeader: "api-key", DisableThinking: true,
			Model: config.Model, MaxTokens: config.MaxTokens, Timeout: config.Timeout,
		})
	default:
		return nil, fmt.Errorf("unsupported MiMo protocol %q", config.Protocol)
	}
}
