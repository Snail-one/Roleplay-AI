package common_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"roleloom/internal/ai"
	"roleloom/internal/ai/provider/common"
)

type requestMessage struct {
	Role    string `json:"role"`
	Content []struct {
		Type      string          `json:"type"`
		Text      string          `json:"text"`
		ID        string          `json:"id"`
		Name      string          `json:"name"`
		Input     json.RawMessage `json:"input"`
		ToolUseID string          `json:"tool_use_id"`
		Content   string          `json:"content"`
		IsError   bool            `json:"is_error"`
	} `json:"content"`
}

func TestClientCompleteText(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/v1/messages" {
			t.Errorf("path = %q", request.URL.Path)
		}
		if request.Header.Get("x-api-key") != "secret" {
			t.Errorf("x-api-key = %q", request.Header.Get("x-api-key"))
		}
		if request.Header.Get("anthropic-version") != "2023-06-01" {
			t.Errorf("anthropic-version = %q", request.Header.Get("anthropic-version"))
		}

		var payload struct {
			Model     string           `json:"model"`
			MaxTokens int              `json:"max_tokens"`
			System    string           `json:"system"`
			Messages  []requestMessage `json:"messages"`
			Tools     []struct {
				Name        string          `json:"name"`
				InputSchema json.RawMessage `json:"input_schema"`
			} `json:"tools"`
		}
		if err := json.NewDecoder(request.Body).Decode(&payload); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if payload.Model != "claude-test" || payload.MaxTokens != 2048 || payload.System != "be helpful" {
			t.Errorf("request metadata = %#v", payload)
		}
		if len(payload.Messages) != 1 || payload.Messages[0].Role != "user" || payload.Messages[0].Content[0].Text != "hello" {
			t.Errorf("messages = %#v", payload.Messages)
		}
		if len(payload.Tools) != 1 || payload.Tools[0].Name != "calculate" || !json.Valid(payload.Tools[0].InputSchema) {
			t.Errorf("tools = %#v", payload.Tools)
		}

		_, _ = response.Write([]byte(`{
            "role":"assistant",
			"content":[{"type":"text","text":"hello back"}],
			"stop_reason":"end_turn"
		}`))
	}))
	defer server.Close()

	client, err := common.NewAnthropicMessages(common.AnthropicMessagesConfig{
		APIURL:                  server.URL + "/v1/messages",
		APIKey:                  "secret",
		Model:                   "claude-test",
		MaxTokens:               2048,
		IncludeAnthropicVersion: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	system, user := "be helpful", "hello"
	message, err := client.Complete(context.Background(), []ai.Message{
		{Role: ai.RoleSystem, Content: &system},
		{Role: ai.RoleUser, Content: &user},
	}, []ai.ToolDefinition{{
		Type: "function",
		Function: ai.FunctionDefinition{
			Name: "calculate", Description: "calculate",
			Parameters: json.RawMessage(`{"type":"object"}`),
		},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if message.Content == nil || *message.Content != "hello back" || message.Role != ai.RoleAssistant {
		t.Fatalf("message = %#v", message)
	}
}

func TestClientConvertsToolCallsAndResults(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		var payload struct {
			Messages []requestMessage `json:"messages"`
		}
		if err := json.NewDecoder(request.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
		if len(payload.Messages) != 3 {
			t.Fatalf("messages length = %d, want 3", len(payload.Messages))
		}
		assistantMessage := payload.Messages[1]
		if assistantMessage.Role != "assistant" || len(assistantMessage.Content) != 2 || assistantMessage.Content[0].Type != "tool_use" {
			t.Fatalf("assistant tool message = %#v", assistantMessage)
		}
		resultMessage := payload.Messages[2]
		if resultMessage.Role != "user" || len(resultMessage.Content) != 2 {
			t.Fatalf("tool result message = %#v", resultMessage)
		}
		if resultMessage.Content[0].ToolUseID != "call-1" || resultMessage.Content[0].IsError {
			t.Errorf("successful tool result = %#v", resultMessage.Content[0])
		}
		if resultMessage.Content[1].ToolUseID != "call-2" || !resultMessage.Content[1].IsError {
			t.Errorf("error tool result = %#v", resultMessage.Content[1])
		}

		_, _ = response.Write([]byte(`{
			"role":"assistant",
			"content":[
				{"type":"text","text":"I will calculate."},
				{"type":"tool_use","id":"call-3","name":"calculate","input":{"operation":"add","a":3,"b":4}}
			],
			"stop_reason":"tool_use"
		}`))
	}))
	defer server.Close()

	client, err := common.NewAnthropicMessages(common.AnthropicMessagesConfig{APIURL: server.URL + "/messages", Model: "claude-test"})
	if err != nil {
		t.Fatal(err)
	}
	question, success, failure := "calculate", `{"result":3}`, `{"error":"bad input"}`
	message, err := client.Complete(context.Background(), []ai.Message{
		{Role: ai.RoleUser, Content: &question},
		{Role: ai.RoleAssistant, ToolCalls: []ai.ToolCall{
			{ID: "call-1", Type: "function", Function: ai.FunctionCall{Name: "calculate", Arguments: `{"operation":"add","a":1,"b":2}`}},
			{ID: "call-2", Type: "function", Function: ai.FunctionCall{Name: "calculate", Arguments: `{"operation":"divide","a":1,"b":0}`}},
		}},
		{Role: ai.RoleTool, ToolCallID: "call-1", Name: "calculate", Content: &success},
		{Role: ai.RoleTool, ToolCallID: "call-2", Name: "calculate", Content: &failure},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if message.Content == nil || *message.Content != "I will calculate." || len(message.ToolCalls) != 1 {
		t.Fatalf("message = %#v", message)
	}
	call := message.ToolCalls[0]
	if call.ID != "call-3" || call.Function.Name != "calculate" || !json.Valid([]byte(call.Function.Arguments)) {
		t.Fatalf("tool call = %#v", call)
	}
}

func TestClientReportsMaxTokens(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		_, _ = response.Write([]byte(`{"role":"assistant","content":[{"type":"text","text":"partial"}],"stop_reason":"max_tokens"}`))
	}))
	defer server.Close()

	client, err := common.NewAnthropicMessages(common.AnthropicMessagesConfig{APIURL: server.URL + "/messages", Model: "claude-test", MaxTokens: 10})
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.Complete(context.Background(), nil, nil)
	if err == nil || !strings.Contains(err.Error(), "increase api.max_output_tokens") {
		t.Fatalf("Complete() error = %v", err)
	}
}

func TestClientReportsAPIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		response.WriteHeader(http.StatusUnauthorized)
		_, _ = response.Write([]byte(`{"type":"error","error":{"message":"invalid key"}}`))
	}))
	defer server.Close()

	client, err := common.NewAnthropicMessages(common.AnthropicMessagesConfig{APIURL: server.URL + "/messages", Model: "claude-test"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.Complete(context.Background(), nil, nil)
	if err == nil || !strings.Contains(err.Error(), "401") || !strings.Contains(err.Error(), "invalid key") {
		t.Fatalf("Complete() error = %v", err)
	}
}

func TestNewValidation(t *testing.T) {
	if _, err := common.NewAnthropicMessages(common.AnthropicMessagesConfig{APIURL: "relative", Model: "model"}); err == nil {
		t.Fatal("New() expected invalid URL error")
	}
	if _, err := common.NewAnthropicMessages(common.AnthropicMessagesConfig{APIURL: "https://example.com/v1", Model: "model"}); err == nil {
		t.Fatal("New() expected full endpoint URL error")
	}
	if _, err := common.NewAnthropicMessages(common.AnthropicMessagesConfig{APIURL: "https://example.com/v1/messages"}); err == nil {
		t.Fatal("New() expected missing model error")
	}
	if _, err := common.NewAnthropicMessages(common.AnthropicMessagesConfig{APIURL: "https://example.com/v1/messages", Model: "model", MaxTokens: -1}); err == nil {
		t.Fatal("New() expected max tokens error")
	}
}
