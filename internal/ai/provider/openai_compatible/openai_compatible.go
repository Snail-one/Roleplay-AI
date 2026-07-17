package openaicompatible

import (
	"fmt"
	"strings"
	"time"

	"roleloom/internal/ai"
	"roleloom/internal/ai/provider/common"
	"roleloom/internal/ai/provider/openai"
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
	if strings.HasSuffix(apiURL, "/responses") {
		return openai.NewResponses(openai.ResponsesConfig{
			APIURL: apiURL, APIKey: config.APIKey, Model: config.Model,
			MaxTokens: config.MaxTokens, Timeout: config.Timeout,
		})
	}
	if strings.HasSuffix(apiURL, "/messages") {
		return nil, fmt.Errorf("unsupported OpenAI-compatible API URL %q: /messages is not supported", config.APIURL)
	}
	if apiURL != "" && !strings.HasSuffix(apiURL, "/chat/completions") {
		apiURL += "/chat/completions"
	}
	return common.NewChatCompletions(common.ChatCompletionsConfig{
		APIURL:    apiURL,
		APIKey:    config.APIKey,
		Model:     config.Model,
		MaxTokens: config.MaxTokens,
		Timeout:   config.Timeout,
	})
}
