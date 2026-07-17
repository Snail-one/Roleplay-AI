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
	anthropicDefaultTimeout      = 60 * time.Second
	anthropicDefaultMaxTokens    = 4096
	anthropicAPIVersion          = "2023-06-01"
	anthropicMaxResponseBodySize = 16 << 20
	anthropicMaxErrorBodySize    = 8 << 10
)

type AnthropicMessagesConfig struct {
	BaseURL                 string
	APIKey                  string
	APIKeyHeader            string
	IncludeAnthropicVersion bool
	DisableThinking         bool
	Model                   string
	MaxTokens               int
	Timeout                 time.Duration
	HTTPClient              *http.Client
}

type AnthropicMessagesClient struct {
	endpoint                string
	apiKey                  string
	apiKeyHeader            string
	includeAnthropicVersion bool
	disableThinking         bool
	model                   string
	maxTokens               int
	httpClient              *http.Client
}

type message struct {
	Role    string         `json:"role"`
	Content []contentBlock `json:"content"`
}

type contentBlock struct {
	Type      string          `json:"type"`
	Text      string          `json:"text,omitempty"`
	ID        string          `json:"id,omitempty"`
	Name      string          `json:"name,omitempty"`
	Input     json.RawMessage `json:"input,omitempty"`
	ToolUseID string          `json:"tool_use_id,omitempty"`
	Content   string          `json:"content,omitempty"`
	IsError   bool            `json:"is_error,omitempty"`
}

type toolDefinition struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	InputSchema json.RawMessage `json:"input_schema"`
}

func NewAnthropicMessages(config AnthropicMessagesConfig) (*AnthropicMessagesClient, error) {
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
	maxTokens := config.MaxTokens
	if maxTokens == 0 {
		maxTokens = anthropicDefaultMaxTokens
	}
	if maxTokens < 0 {
		return nil, errors.New("max tokens cannot be negative")
	}

	httpClient := config.HTTPClient
	if httpClient == nil {
		timeout := config.Timeout
		if timeout == 0 {
			timeout = anthropicDefaultTimeout
		}
		if timeout < 0 {
			return nil, errors.New("timeout cannot be negative")
		}
		httpClient = &http.Client{Timeout: timeout}
	}

	apiKeyHeader := strings.TrimSpace(config.APIKeyHeader)
	if apiKeyHeader == "" {
		apiKeyHeader = "x-api-key"
	}
	return &AnthropicMessagesClient{
		endpoint:                strings.TrimRight(baseURL, "/") + "/messages",
		apiKey:                  strings.TrimSpace(config.APIKey),
		apiKeyHeader:            apiKeyHeader,
		includeAnthropicVersion: config.IncludeAnthropicVersion,
		disableThinking:         config.DisableThinking,
		model:                   model,
		maxTokens:               maxTokens,
		httpClient:              httpClient,
	}, nil
}

func (c *AnthropicMessagesClient) Complete(ctx context.Context, history []ai.Message, definitions []ai.ToolDefinition) (ai.Message, error) {
	system, messages, err := convertMessages(history)
	if err != nil {
		return ai.Message{}, fmt.Errorf("convert messages: %w", err)
	}
	tools := make([]toolDefinition, 0, len(definitions))
	for _, definition := range definitions {
		tools = append(tools, toolDefinition{
			Name:        definition.Function.Name,
			Description: definition.Function.Description,
			InputSchema: definition.Function.Parameters,
		})
	}

	requestBody := struct {
		Model     string           `json:"model"`
		MaxTokens int              `json:"max_tokens"`
		System    string           `json:"system,omitempty"`
		Messages  []message        `json:"messages"`
		Tools     []toolDefinition `json:"tools,omitempty"`
		Thinking  *struct {
			Type string `json:"type"`
		} `json:"thinking,omitempty"`
	}{
		Model:     c.model,
		MaxTokens: c.maxTokens,
		System:    system,
		Messages:  messages,
		Tools:     tools,
	}
	if c.disableThinking {
		requestBody.Thinking = &struct {
			Type string `json:"type"`
		}{Type: "disabled"}
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
	if c.includeAnthropicVersion {
		request.Header.Set("anthropic-version", anthropicAPIVersion)
	}
	request.Header.Set("User-Agent", "roleloom-agent/1.0")
	if c.apiKey != "" {
		request.Header.Set(c.apiKeyHeader, c.apiKey)
	}

	response, err := c.httpClient.Do(request)
	if err != nil {
		return ai.Message{}, fmt.Errorf("send request: %w", err)
	}
	defer response.Body.Close()

	if response.StatusCode < 200 || response.StatusCode >= 300 {
		body, readErr := io.ReadAll(io.LimitReader(response.Body, anthropicMaxErrorBodySize))
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
		Role       string         `json:"role"`
		Content    []contentBlock `json:"content"`
		StopReason string         `json:"stop_reason"`
	}
	decoder := json.NewDecoder(io.LimitReader(response.Body, anthropicMaxResponseBodySize+1))
	if err := decoder.Decode(&payload); err != nil {
		return ai.Message{}, fmt.Errorf("decode response: %w", err)
	}
	if payload.StopReason == "max_tokens" {
		return ai.Message{}, fmt.Errorf("response reached max_tokens (%d); increase api.max_output_tokens", c.maxTokens)
	}

	result := ai.Message{Role: ai.RoleAssistant}
	var textParts []string
	for _, block := range payload.Content {
		switch block.Type {
		case "text":
			if block.Text != "" {
				textParts = append(textParts, block.Text)
			}
		case "tool_use":
			arguments := block.Input
			if len(arguments) == 0 {
				arguments = json.RawMessage(`{}`)
			}
			result.ToolCalls = append(result.ToolCalls, ai.ToolCall{
				ID:   block.ID,
				Type: "function",
				Function: ai.FunctionCall{
					Name:      block.Name,
					Arguments: string(arguments),
				},
			})
		}
	}
	if len(textParts) > 0 {
		text := strings.Join(textParts, "\n")
		result.Content = &text
	}
	return result, nil
}

func convertMessages(history []ai.Message) (string, []message, error) {
	var systemParts []string
	var result []message
	appendBlocks := func(role string, blocks ...contentBlock) {
		if len(result) > 0 && result[len(result)-1].Role == role {
			result[len(result)-1].Content = append(result[len(result)-1].Content, blocks...)
			return
		}
		result = append(result, message{Role: role, Content: blocks})
	}

	for _, item := range history {
		switch item.Role {
		case ai.RoleSystem:
			if item.Content != nil && *item.Content != "" {
				systemParts = append(systemParts, *item.Content)
			}
		case ai.RoleUser:
			if item.Content == nil {
				return "", nil, errors.New("user message has no content")
			}
			appendBlocks(ai.RoleUser, contentBlock{Type: "text", Text: *item.Content})
		case ai.RoleAssistant:
			var blocks []contentBlock
			if item.Content != nil && *item.Content != "" {
				blocks = append(blocks, contentBlock{Type: "text", Text: *item.Content})
			}
			for _, call := range item.ToolCalls {
				input := json.RawMessage(call.Function.Arguments)
				if len(input) == 0 {
					input = json.RawMessage(`{}`)
				}
				if !json.Valid(input) {
					return "", nil, fmt.Errorf("tool call %q contains invalid JSON arguments", call.ID)
				}
				blocks = append(blocks, contentBlock{Type: "tool_use", ID: call.ID, Name: call.Function.Name, Input: input})
			}
			if len(blocks) == 0 {
				return "", nil, errors.New("assistant message has no content")
			}
			appendBlocks(ai.RoleAssistant, blocks...)
		case ai.RoleTool:
			if item.Content == nil {
				return "", nil, fmt.Errorf("tool result %q has no content", item.ToolCallID)
			}
			appendBlocks(ai.RoleUser, contentBlock{
				Type:      "tool_result",
				ToolUseID: item.ToolCallID,
				Content:   *item.Content,
				IsError:   isToolError(*item.Content),
			})
		default:
			return "", nil, fmt.Errorf("unsupported message role %q", item.Role)
		}
	}
	return strings.Join(systemParts, "\n\n"), result, nil
}

func isToolError(content string) bool {
	var result struct {
		Error string `json:"error"`
	}
	return json.Unmarshal([]byte(content), &result) == nil && result.Error != ""
}
