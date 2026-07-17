package common

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"roleloom/internal/ai"
)

const (
	defaultTimeout      = 60 * time.Second
	maxResponseBodySize = 16 << 20
	maxErrorBodySize    = 8 << 10
)

type ChatCompletionsConfig struct {
	BaseURL        string
	APIKey         string
	Model          string
	MaxTokens      int
	MaxTokensField string
	Timeout        time.Duration
	HTTPClient     *http.Client
}

type Client struct {
	endpoint       string
	apiKey         string
	model          string
	maxTokens      int
	maxTokensField string
	httpClient     *http.Client
}

func NewChatCompletions(config ChatCompletionsConfig) (*Client, error) {
	baseURL := strings.TrimSpace(config.BaseURL)
	if baseURL == "" {
		return nil, errors.New("base URL is required")
	}
	parsed, err := url.Parse(baseURL)
	if err != nil {
		return nil, fmt.Errorf("parse base URL: %w", err)
	}
	if (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		return nil, errors.New("base URL must be an absolute HTTP(S) URL")
	}
	if parsed.RawQuery != "" || parsed.Fragment != "" {
		return nil, errors.New("base URL cannot contain a query or fragment")
	}

	model := strings.TrimSpace(config.Model)
	if model == "" {
		return nil, errors.New("model is required")
	}
	if config.MaxTokens < 0 {
		return nil, errors.New("max tokens cannot be negative")
	}
	maxTokensField := strings.TrimSpace(config.MaxTokensField)
	if maxTokensField == "" {
		maxTokensField = "max_tokens"
	}
	if maxTokensField != "max_tokens" && maxTokensField != "max_completion_tokens" {
		return nil, fmt.Errorf("unsupported max tokens field %q", maxTokensField)
	}

	httpClient := config.HTTPClient
	if httpClient == nil {
		timeout := config.Timeout
		if timeout == 0 {
			timeout = defaultTimeout
		}
		if timeout < 0 {
			return nil, errors.New("timeout cannot be negative")
		}
		httpClient = &http.Client{Timeout: timeout}
	}

	return &Client{
		endpoint:       strings.TrimRight(baseURL, "/") + "/chat/completions",
		apiKey:         strings.TrimSpace(config.APIKey),
		model:          model,
		maxTokens:      config.MaxTokens,
		maxTokensField: maxTokensField,
		httpClient:     httpClient,
	}, nil
}

func (c *Client) Complete(ctx context.Context, messages []ai.Message, tools []ai.ToolDefinition) (ai.Message, error) {
	requestBody := struct {
		Model               string              `json:"model"`
		Messages            []ai.Message        `json:"messages"`
		Tools               []ai.ToolDefinition `json:"tools,omitempty"`
		MaxTokens           int                 `json:"max_tokens,omitempty"`
		MaxCompletionTokens int                 `json:"max_completion_tokens,omitempty"`
	}{
		Model:    c.model,
		Messages: messages,
		Tools:    tools,
	}
	if c.maxTokensField == "max_completion_tokens" {
		requestBody.MaxCompletionTokens = c.maxTokens
	} else {
		requestBody.MaxTokens = c.maxTokens
	}
	encoded, err := json.Marshal(requestBody)
	if err != nil {
		return ai.Message{}, fmt.Errorf("encode request: %w", err)
	}

	request, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint, bytes.NewReader(encoded))
	if err != nil {
		return ai.Message{}, fmt.Errorf("create request: %w", err)
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json")
	request.Header.Set("User-Agent", "roleloom-agent/1.0")
	if c.apiKey != "" {
		request.Header.Set("Authorization", "Bearer "+c.apiKey)
	}

	response, err := c.httpClient.Do(request)
	if err != nil {
		return ai.Message{}, fmt.Errorf("send request: %w", err)
	}
	defer response.Body.Close()

	if response.StatusCode < 200 || response.StatusCode >= 300 {
		body, readErr := io.ReadAll(io.LimitReader(response.Body, maxErrorBodySize))
		if readErr != nil {
			return ai.Message{}, fmt.Errorf("API returned %s (failed to read error body: %v)", response.Status, readErr)
		}
		message := strings.TrimSpace(string(body))
		if message == "" {
			message = http.StatusText(response.StatusCode)
		}
		return ai.Message{}, fmt.Errorf("API returned %s: %s", response.Status, message)
	}

	var payload struct {
		Choices []struct {
			Message ai.Message `json:"message"`
		} `json:"choices"`
	}
	decoder := json.NewDecoder(io.LimitReader(response.Body, maxResponseBodySize+1))
	if err := decoder.Decode(&payload); err != nil {
		return ai.Message{}, fmt.Errorf("decode response: %w", err)
	}
	if len(payload.Choices) == 0 {
		return ai.Message{}, errors.New("response contains no choices")
	}
	return payload.Choices[0].Message, nil
}
