package openai

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
	responsesDefaultTimeout      = 60 * time.Second
	responsesMaxResponseBodySize = 16 << 20
	responsesMaxErrorBodySize    = 8 << 10
)

type ResponsesConfig struct {
	APIURL          string
	APIKey          string
	APIKeyHeader    string
	Model           string
	ReasoningEffort string
	MaxTokens       int
	Timeout         time.Duration
	HTTPClient      *http.Client
}

type responsesClient struct {
	endpoint        string
	apiKey          string
	apiKeyHeader    string
	model           string
	reasoningEffort string
	maxTokens       int
	httpClient      *http.Client
}

type responseInputItem struct {
	Type      string `json:"type,omitempty"`
	Role      string `json:"role,omitempty"`
	Content   string `json:"content,omitempty"`
	CallID    string `json:"call_id,omitempty"`
	Name      string `json:"name,omitempty"`
	Arguments string `json:"arguments,omitempty"`
	Output    string `json:"output,omitempty"`
}

type responseTool struct {
	Type        string          `json:"type"`
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	Parameters  json.RawMessage `json:"parameters"`
}

func NewResponses(config ResponsesConfig) (ai.Backend, error) {
	apiURL := strings.TrimRight(strings.TrimSpace(config.APIURL), "/")
	if apiURL == "" {
		return nil, errors.New("API URL is required")
	}
	parsed, err := url.Parse(apiURL)
	if err != nil {
		return nil, fmt.Errorf("parse API URL: %w", err)
	}
	if (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		return nil, errors.New("API URL must be an absolute HTTP(S) URL")
	}
	if parsed.RawQuery != "" || parsed.Fragment != "" {
		return nil, errors.New("API URL cannot contain a query or fragment")
	}
	if !strings.HasSuffix(strings.TrimRight(parsed.Path, "/"), "/responses") {
		return nil, errors.New("API URL must be a complete /responses endpoint")
	}

	model := strings.TrimSpace(config.Model)
	if model == "" {
		return nil, errors.New("model is required")
	}
	if config.MaxTokens < 0 {
		return nil, errors.New("max tokens cannot be negative")
	}

	apiKeyHeader := strings.TrimSpace(config.APIKeyHeader)
	if apiKeyHeader == "" {
		apiKeyHeader = "Authorization"
	}
	switch {
	case strings.EqualFold(apiKeyHeader, "Authorization"):
		apiKeyHeader = "Authorization"
	case strings.EqualFold(apiKeyHeader, "api-key"):
		apiKeyHeader = "api-key"
	default:
		return nil, fmt.Errorf("unsupported API key header %q", config.APIKeyHeader)
	}

	httpClient := config.HTTPClient
	if httpClient == nil {
		timeout := config.Timeout
		if timeout == 0 {
			timeout = responsesDefaultTimeout
		}
		if timeout < 0 {
			return nil, errors.New("timeout cannot be negative")
		}
		httpClient = &http.Client{Timeout: timeout}
	}

	return &responsesClient{
		endpoint: apiURL, apiKey: strings.TrimSpace(config.APIKey), apiKeyHeader: apiKeyHeader,
		model: model, reasoningEffort: strings.TrimSpace(config.ReasoningEffort),
		maxTokens: config.MaxTokens, httpClient: httpClient,
	}, nil
}

func (c *responsesClient) Complete(ctx context.Context, history []ai.Message, definitions []ai.ToolDefinition) (ai.Message, error) {
	instructions, input, err := convertResponseInput(history)
	if err != nil {
		return ai.Message{}, fmt.Errorf("convert messages: %w", err)
	}
	tools := make([]responseTool, 0, len(definitions))
	for _, definition := range definitions {
		tools = append(tools, responseTool{
			Type: "function", Name: definition.Function.Name,
			Description: definition.Function.Description,
			Parameters:  definition.Function.Parameters,
		})
	}

	type reasoningConfig struct {
		Effort string `json:"effort"`
	}
	requestBody := struct {
		Model           string              `json:"model"`
		Instructions    string              `json:"instructions,omitempty"`
		Input           []responseInputItem `json:"input"`
		Tools           []responseTool      `json:"tools,omitempty"`
		MaxOutputTokens int                 `json:"max_output_tokens,omitempty"`
		Stream          bool                `json:"stream"`
		Reasoning       *reasoningConfig    `json:"reasoning,omitempty"`
	}{
		Model: c.model, Instructions: instructions, Input: input,
		Tools: tools, MaxOutputTokens: c.maxTokens,
	}
	if c.reasoningEffort != "" {
		requestBody.Reasoning = &reasoningConfig{Effort: c.reasoningEffort}
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
		value := c.apiKey
		if strings.EqualFold(c.apiKeyHeader, "Authorization") {
			value = "Bearer " + value
		}
		request.Header.Set(c.apiKeyHeader, value)
	}

	response, err := c.httpClient.Do(request)
	if err != nil {
		return ai.Message{}, fmt.Errorf("send request: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		body, readErr := io.ReadAll(io.LimitReader(response.Body, responsesMaxErrorBodySize))
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
		Status string `json:"status"`
		Error  *struct {
			Message string `json:"message"`
		} `json:"error"`
		IncompleteDetails *struct {
			Reason string `json:"reason"`
		} `json:"incomplete_details"`
		Output []struct {
			Type      string `json:"type"`
			ID        string `json:"id"`
			CallID    string `json:"call_id"`
			Name      string `json:"name"`
			Arguments string `json:"arguments"`
			Content   []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"content"`
		} `json:"output"`
	}
	decoder := json.NewDecoder(io.LimitReader(response.Body, responsesMaxResponseBodySize+1))
	if err := decoder.Decode(&payload); err != nil {
		return ai.Message{}, fmt.Errorf("decode response: %w", err)
	}
	if payload.Error != nil {
		return ai.Message{}, fmt.Errorf("response error: %s", payload.Error.Message)
	}
	if payload.Status == "incomplete" {
		reason := "unknown reason"
		if payload.IncompleteDetails != nil && payload.IncompleteDetails.Reason != "" {
			reason = payload.IncompleteDetails.Reason
		}
		return ai.Message{}, fmt.Errorf("response incomplete: %s", reason)
	}

	result := ai.Message{Role: ai.RoleAssistant}
	var textParts []string
	for _, output := range payload.Output {
		switch output.Type {
		case "message":
			for _, content := range output.Content {
				if content.Type == "output_text" && content.Text != "" {
					textParts = append(textParts, content.Text)
				}
			}
		case "function_call":
			callID := output.CallID
			if callID == "" {
				callID = output.ID
			}
			result.ToolCalls = append(result.ToolCalls, ai.ToolCall{
				ID: callID, Type: "function",
				Function: ai.FunctionCall{Name: output.Name, Arguments: output.Arguments},
			})
		}
	}
	if len(textParts) > 0 {
		text := strings.Join(textParts, "\n")
		result.Content = &text
	}
	return result, nil
}

func convertResponseInput(history []ai.Message) (string, []responseInputItem, error) {
	var instructions []string
	var input []responseInputItem
	for _, message := range history {
		switch message.Role {
		case ai.RoleSystem:
			if message.Content != nil && *message.Content != "" {
				instructions = append(instructions, *message.Content)
			}
		case ai.RoleUser:
			if message.Content == nil {
				return "", nil, errors.New("user message has no content")
			}
			input = append(input, responseInputItem{Role: ai.RoleUser, Content: *message.Content})
		case ai.RoleAssistant:
			if message.Content != nil && *message.Content != "" {
				input = append(input, responseInputItem{Role: ai.RoleAssistant, Content: *message.Content})
			}
			for _, call := range message.ToolCalls {
				if !json.Valid([]byte(call.Function.Arguments)) {
					return "", nil, fmt.Errorf("tool call %q contains invalid JSON arguments", call.ID)
				}
				input = append(input, responseInputItem{
					Type: "function_call", CallID: call.ID,
					Name: call.Function.Name, Arguments: call.Function.Arguments,
				})
			}
		case ai.RoleTool:
			if message.Content == nil {
				return "", nil, fmt.Errorf("tool result %q has no content", message.ToolCallID)
			}
			input = append(input, responseInputItem{
				Type: "function_call_output", CallID: message.ToolCallID, Output: *message.Content,
			})
		default:
			return "", nil, fmt.Errorf("unsupported message role %q", message.Role)
		}
	}
	return strings.Join(instructions, "\n\n"), input, nil
}
