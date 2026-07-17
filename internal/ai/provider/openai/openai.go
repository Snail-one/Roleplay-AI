package openai

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
	parsed, err := url.Parse(apiURL)
	if err != nil {
		return nil, fmt.Errorf("parse OpenAI API URL: %w", err)
	}
	if (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		return nil, fmt.Errorf("OpenAI API URL must be an absolute HTTP(S) URL")
	}
	if parsed.RawQuery != "" || parsed.Fragment != "" {
		return nil, fmt.Errorf("OpenAI API URL cannot contain a query or fragment")
	}

	switch {
	case strings.HasSuffix(parsed.Path, "/chat/completions"):
		return common.NewChatCompletions(common.ChatCompletionsConfig{
			APIURL:         apiURL,
			APIKey:         config.APIKey,
			Model:          config.Model,
			MaxTokens:      config.MaxTokens,
			MaxTokensField: "max_completion_tokens",
			Timeout:        config.Timeout,
		})
	case strings.HasSuffix(parsed.Path, "/responses"):
		return NewResponses(ResponsesConfig{
			APIURL: apiURL, APIKey: config.APIKey, Model: config.Model,
			MaxTokens: config.MaxTokens, Timeout: config.Timeout,
		})
	default:
		return nil, fmt.Errorf("unsupported OpenAI API URL %q: expected /chat/completions or /responses endpoint", config.APIURL)
	}
}
