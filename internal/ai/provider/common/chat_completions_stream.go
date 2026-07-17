package common

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"roleloom/internal/ai"
)

func (c *Client) Stream(ctx context.Context, messages []ai.Message, tools []ai.ToolDefinition, sink ai.EventSink) (ai.Message, error) {
	body := struct {
		Model               string              `json:"model"`
		Messages            []ai.Message        `json:"messages"`
		Tools               []ai.ToolDefinition `json:"tools,omitempty"`
		MaxTokens           int                 `json:"max_tokens,omitempty"`
		MaxCompletionTokens int                 `json:"max_completion_tokens,omitempty"`
		Stream              bool                `json:"stream"`
	}{Model: c.model, Messages: messages, Tools: tools, Stream: true}
	if c.maxTokensField == "max_completion_tokens" {
		body.MaxCompletionTokens = c.maxTokens
	} else {
		body.MaxTokens = c.maxTokens
	}
	encoded, err := json.Marshal(body)
	if err != nil {
		return ai.Message{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint, bytes.NewReader(encoded))
	if err != nil {
		return ai.Message{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("User-Agent", "roleloom/1.0")
	if c.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return ai.Message{}, fmt.Errorf("send request: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, maxErrorBodySize))
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
		if data == "[DONE]" {
			break
		}
		var event struct {
			Choices []struct {
				Delta struct {
					Content   *string `json:"content"`
					ToolCalls []struct {
						Index    int    `json:"index"`
						ID       string `json:"id"`
						Type     string `json:"type"`
						Function struct {
							Name      string `json:"name"`
							Arguments string `json:"arguments"`
						} `json:"function"`
					} `json:"tool_calls"`
				} `json:"delta"`
			} `json:"choices"`
			Error *struct {
				Message string `json:"message"`
			} `json:"error"`
		}
		if err := json.Unmarshal([]byte(data), &event); err != nil {
			return ai.Message{}, fmt.Errorf("decode stream event: %w", err)
		}
		if event.Error != nil {
			return ai.Message{}, errors.New(event.Error.Message)
		}
		for _, choice := range event.Choices {
			if choice.Delta.Content != nil {
				text.WriteString(*choice.Delta.Content)
				if sink != nil {
					if err := sink(ai.StreamEvent{Delta: *choice.Delta.Content}); err != nil {
						return ai.Message{}, err
					}
				}
			}
			for _, part := range choice.Delta.ToolCalls {
				call := calls[part.Index]
				if call == nil {
					call = &ai.ToolCall{Type: "function"}
					calls[part.Index] = call
				}
				call.ID += part.ID
				if part.Type != "" {
					call.Type = part.Type
				}
				call.Function.Name += part.Function.Name
				call.Function.Arguments += part.Function.Arguments
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return ai.Message{}, fmt.Errorf("read stream: %w", err)
	}
	if text.Len() > 0 {
		v := text.String()
		result.Content = &v
	}
	for i := 0; i < len(calls); i++ {
		if call := calls[i]; call != nil {
			result.ToolCalls = append(result.ToolCalls, *call)
		}
	}
	return result, nil
}
