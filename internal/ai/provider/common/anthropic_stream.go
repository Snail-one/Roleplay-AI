package common

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"roleloom/internal/ai"
)

func (c *AnthropicMessagesClient) Stream(ctx context.Context, history []ai.Message, definitions []ai.ToolDefinition, sink ai.EventSink) (ai.Message, error) {
	system, messages, err := convertMessages(history)
	if err != nil {
		return ai.Message{}, err
	}
	tools := make([]toolDefinition, 0, len(definitions))
	for _, d := range definitions {
		tools = append(tools, toolDefinition{Name: d.Function.Name, Description: d.Function.Description, InputSchema: d.Function.Parameters})
	}
	body := struct {
		Model     string           `json:"model"`
		MaxTokens int              `json:"max_tokens"`
		System    string           `json:"system,omitempty"`
		Messages  []message        `json:"messages"`
		Tools     []toolDefinition `json:"tools,omitempty"`
		Stream    bool             `json:"stream"`
		Thinking  *struct {
			Type string `json:"type"`
		} `json:"thinking,omitempty"`
	}{Model: c.model, MaxTokens: c.maxTokens, System: system, Messages: messages, Tools: tools, Stream: true}
	if c.disableThinking {
		body.Thinking = &struct {
			Type string `json:"type"`
		}{Type: "disabled"}
	}
	encoded, _ := json.Marshal(body)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint, bytes.NewReader(encoded))
	if err != nil {
		return ai.Message{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("User-Agent", "roleloom/1.0")
	if c.includeAnthropicVersion {
		req.Header.Set("anthropic-version", anthropicAPIVersion)
	}
	if c.apiKey != "" {
		req.Header.Set(c.apiKeyHeader, c.apiKey)
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return ai.Message{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, anthropicMaxErrorBodySize))
		return ai.Message{}, fmt.Errorf("API returned %s: %s", resp.Status, strings.TrimSpace(string(b)))
	}
	result := ai.Message{Role: ai.RoleAssistant}
	var text strings.Builder
	calls := map[int]*ai.ToolCall{}
	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 4096), 16<<20)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		var e struct {
			Type         string `json:"type"`
			Index        int    `json:"index"`
			ContentBlock struct {
				Type string `json:"type"`
				ID   string `json:"id"`
				Name string `json:"name"`
			} `json:"content_block"`
			Delta struct {
				Type        string `json:"type"`
				Text        string `json:"text"`
				PartialJSON string `json:"partial_json"`
			} `json:"delta"`
			Error *struct {
				Message string `json:"message"`
			} `json:"error"`
		}
		if err := json.Unmarshal([]byte(data), &e); err != nil {
			return ai.Message{}, err
		}
		if e.Type == "error" && e.Error != nil {
			return ai.Message{}, fmt.Errorf("stream error: %s", e.Error.Message)
		}
		if e.Type == "content_block_start" && e.ContentBlock.Type == "tool_use" {
			calls[e.Index] = &ai.ToolCall{ID: e.ContentBlock.ID, Type: "function", Function: ai.FunctionCall{Name: e.ContentBlock.Name}}
		}
		if e.Type == "content_block_delta" {
			if e.Delta.Type == "text_delta" {
				text.WriteString(e.Delta.Text)
				if sink != nil {
					if err := sink(ai.StreamEvent{Delta: e.Delta.Text}); err != nil {
						return ai.Message{}, err
					}
				}
			}
			if e.Delta.Type == "input_json_delta" && calls[e.Index] != nil {
				calls[e.Index].Function.Arguments += e.Delta.PartialJSON
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return ai.Message{}, err
	}
	if text.Len() > 0 {
		v := text.String()
		result.Content = &v
	}
	for i := 0; i < len(calls); i++ {
		if calls[i] != nil {
			result.ToolCalls = append(result.ToolCalls, *calls[i])
		}
	}
	return result, nil
}
