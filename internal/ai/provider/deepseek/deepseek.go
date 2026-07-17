package deepseek

import (
	"fmt"
	"net/url"
	"strings"
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
	apiURL := strings.TrimRight(strings.TrimSpace(config.APIURL), "/")
	if err := validateEndpoint(apiURL); err != nil {
		return nil, err
	}

	switch {
	case strings.HasSuffix(apiURL, "/chat/completions"):
		return common.NewChatCompletions(common.ChatCompletionsConfig{
			APIURL: apiURL, APIKey: config.APIKey, Model: config.Model,
			MaxTokens: config.MaxTokens, MaxTokensField: "max_tokens", Timeout: config.Timeout,
		})
	case strings.HasSuffix(apiURL, "/messages"):
		return common.NewAnthropicMessages(common.AnthropicMessagesConfig{
			APIURL: apiURL, APIKey: config.APIKey,
			APIKeyHeader: "x-api-key", DisableThinking: true,
			Model: config.Model, MaxTokens: config.MaxTokens, Timeout: config.Timeout,
		})
	default:
		return nil, fmt.Errorf("unsupported DeepSeek API URL %q: expected /chat/completions or /messages endpoint", config.APIURL)
	}
}

func validateEndpoint(apiURL string) error {
	if apiURL == "" {
		return fmt.Errorf("DeepSeek API URL is required")
	}
	parsed, err := url.Parse(apiURL)
	if err != nil {
		return fmt.Errorf("parse DeepSeek API URL: %w", err)
	}
	if (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		return fmt.Errorf("DeepSeek API URL must be an absolute HTTP(S) URL")
	}
	if parsed.RawQuery != "" || parsed.Fragment != "" {
		return fmt.Errorf("DeepSeek API URL cannot contain a query or fragment")
	}
	return nil
}
