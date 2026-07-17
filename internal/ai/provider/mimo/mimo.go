package mimo

import (
	"fmt"
	"net/url"
	"strings"
	"time"

	"roleloom/internal/ai"
	"roleloom/internal/ai/provider/common"
	"roleloom/internal/ai/provider/openai"
)

const (
	protocolChatCompletions = "chat_completions"
	protocolResponses       = "responses"
	protocolAnthropic       = "anthropic"
)

type Config struct {
	APIURL    string
	APIKey    string
	Model     string
	MaxTokens int
	Timeout   time.Duration
}

func New(config Config) (ai.Backend, error) {
	protocol, apiURL, err := resolveProtocol(config.APIURL)
	if err != nil {
		return nil, err
	}

	switch protocol {
	case protocolChatCompletions:
		return common.NewChatCompletions(common.ChatCompletionsConfig{
			APIURL:         apiURL,
			APIKey:         config.APIKey,
			Model:          config.Model,
			MaxTokens:      config.MaxTokens,
			MaxTokensField: "max_completion_tokens",
			Timeout:        config.Timeout,
		})
	case protocolResponses:
		return openai.NewResponses(openai.ResponsesConfig{
			APIURL: apiURL, APIKey: config.APIKey, Model: config.Model,
			APIKeyHeader: "api-key", ReasoningEffort: "none",
			MaxTokens: config.MaxTokens, Timeout: config.Timeout,
		})
	case protocolAnthropic:
		return common.NewAnthropicMessages(common.AnthropicMessagesConfig{
			APIURL: apiURL, APIKey: config.APIKey,
			APIKeyHeader: "api-key", DisableThinking: true,
			Model: config.Model, MaxTokens: config.MaxTokens, Timeout: config.Timeout,
		})
	}
	return nil, fmt.Errorf("unsupported inferred MiMo protocol %q", protocol)
}

func resolveProtocol(configuredURL string) (string, string, error) {
	apiURL := strings.TrimRight(strings.TrimSpace(configuredURL), "/")
	if apiURL == "" {
		return "", "", fmt.Errorf("MiMo API URL is required")
	}

	parsed, err := url.Parse(apiURL)
	if err != nil {
		return "", "", fmt.Errorf("parse MiMo API URL: %w", err)
	}
	if (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		return "", "", fmt.Errorf("MiMo API URL must be an absolute HTTP(S) URL")
	}
	if parsed.RawQuery != "" || parsed.Fragment != "" {
		return "", "", fmt.Errorf("MiMo API URL must not contain a query or fragment")
	}

	path := strings.TrimRight(parsed.Path, "/")
	switch {
	case strings.HasSuffix(path, "/chat/completions"):
		return protocolChatCompletions, apiURL, nil
	case strings.HasSuffix(path, "/responses"):
		return protocolResponses, apiURL, nil
	case strings.HasSuffix(path, "/messages"):
		return protocolAnthropic, apiURL, nil
	default:
		return "", "", fmt.Errorf("unsupported MiMo API URL %q: expected /chat/completions, /responses, or /messages endpoint", configuredURL)
	}
}
