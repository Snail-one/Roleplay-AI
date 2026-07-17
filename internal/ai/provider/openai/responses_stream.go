package openai

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

func (c *responsesClient) Stream(ctx context.Context, history []ai.Message, definitions []ai.ToolDefinition, sink ai.EventSink) (ai.Message, error) {
	instructions, input, err := convertResponseInput(history)
	if err != nil {
		return ai.Message{}, err
	}
	tools := make([]responseTool, 0, len(definitions))
	for _, d := range definitions {
		tools = append(tools, responseTool{Type: "function", Name: d.Function.Name, Description: d.Function.Description, Parameters: d.Function.Parameters})
	}
	type reasoningConfig struct {
		Effort string `json:"effort"`
	}
	body := struct {
		Model           string              `json:"model"`
		Instructions    string              `json:"instructions,omitempty"`
		Input           []responseInputItem `json:"input"`
		Tools           []responseTool      `json:"tools,omitempty"`
		MaxOutputTokens int                 `json:"max_output_tokens,omitempty"`
		Stream          bool                `json:"stream"`
		Reasoning       *reasoningConfig    `json:"reasoning,omitempty"`
	}{Model: c.model, Instructions: instructions, Input: input, Tools: tools, MaxOutputTokens: c.maxTokens, Stream: true}
	if c.reasoningEffort != "" {
		body.Reasoning = &reasoningConfig{Effort: c.reasoningEffort}
	}
	encoded, _ := json.Marshal(body)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint, bytes.NewReader(encoded))
	if err != nil {
		return ai.Message{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("User-Agent", "roleloom/1.0")
	if c.apiKey != "" {
		value := c.apiKey
		if strings.EqualFold(c.apiKeyHeader, "Authorization") {
			value = "Bearer " + value
		}
		req.Header.Set(c.apiKeyHeader, value)
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return ai.Message{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, responsesMaxErrorBodySize))
		return ai.Message{}, fmt.Errorf("API returned %s: %s", resp.Status, strings.TrimSpace(string(b)))
	}
	result := ai.Message{Role: ai.RoleAssistant}
	var text strings.Builder
	calls := map[string]*ai.ToolCall{}
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
		var e struct {
			Type        string `json:"type"`
			Delta       string `json:"delta"`
			ItemID      string `json:"item_id"`
			OutputIndex int    `json:"output_index"`
			Item        struct {
				Type      string `json:"type"`
				ID        string `json:"id"`
				CallID    string `json:"call_id"`
				Name      string `json:"name"`
				Arguments string `json:"arguments"`
			} `json:"item"`
			Error *struct {
				Message string `json:"message"`
			} `json:"error"`
		}
		if err := json.Unmarshal([]byte(data), &e); err != nil {
			return ai.Message{}, err
		}
		switch e.Type {
		case "response.output_text.delta":
			text.WriteString(e.Delta)
			if sink != nil {
				if err := sink(ai.StreamEvent{Delta: e.Delta}); err != nil {
					return ai.Message{}, err
				}
			}
		case "response.output_item.added":
			if e.Item.Type == "function_call" {
				id := e.Item.CallID
				if id == "" {
					id = e.Item.ID
				}
				calls[e.Item.ID] = &ai.ToolCall{ID: id, Type: "function", Function: ai.FunctionCall{Name: e.Item.Name, Arguments: e.Item.Arguments}}
			}
		case "response.function_call_arguments.delta":
			if calls[e.ItemID] != nil {
				calls[e.ItemID].Function.Arguments += e.Delta
			}
		case "error", "response.failed":
			if e.Error != nil {
				return ai.Message{}, fmt.Errorf("stream error: %s", e.Error.Message)
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
	for _, call := range calls {
		result.ToolCalls = append(result.ToolCalls, *call)
	}
	return result, nil
}
