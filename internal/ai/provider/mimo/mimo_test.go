package mimo_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"roleloom/internal/ai"
	"roleloom/internal/ai/provider/mimo"
)

func TestChatCompletionsProtocol(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/v1/chat/completions" {
			t.Errorf("path = %q", request.URL.Path)
		}
		if request.Header.Get("Authorization") != "Bearer secret" {
			t.Errorf("Authorization = %q", request.Header.Get("Authorization"))
		}
		var payload map[string]json.RawMessage
		if err := json.NewDecoder(request.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
		if string(payload["max_completion_tokens"]) != "2048" {
			t.Errorf("max_completion_tokens = %s", payload["max_completion_tokens"])
		}
		if _, exists := payload["max_tokens"]; exists {
			t.Error("unexpected max_tokens field")
		}
		_, _ = response.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"chat reply"}}]}`))
	}))
	defer server.Close()

	backend, err := mimo.New(mimo.Config{
		Protocol: mimo.ProtocolChatCompletions, BaseURL: server.URL + "/v1",
		APIKey: "secret", Model: "mimo-v2.5-pro", MaxTokens: 2048,
	})
	if err != nil {
		t.Fatal(err)
	}
	question := "hello"
	response, err := backend.Complete(context.Background(), []ai.Message{{Role: ai.RoleUser, Content: &question}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if response.Content == nil || *response.Content != "chat reply" {
		t.Fatalf("response = %#v", response)
	}
}

func TestResponsesProtocolToolRoundTrip(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/v1/responses" {
			t.Errorf("path = %q", request.URL.Path)
		}
		if request.Header.Get("api-key") != "secret" {
			t.Errorf("api-key = %q", request.Header.Get("api-key"))
		}
		var payload struct {
			Model           string `json:"model"`
			Instructions    string `json:"instructions"`
			MaxOutputTokens int    `json:"max_output_tokens"`
			Reasoning       struct {
				Effort string `json:"effort"`
			} `json:"reasoning"`
			Input []struct {
				Type      string `json:"type"`
				Role      string `json:"role"`
				Content   string `json:"content"`
				CallID    string `json:"call_id"`
				Name      string `json:"name"`
				Arguments string `json:"arguments"`
				Output    string `json:"output"`
			} `json:"input"`
			Tools []struct {
				Type string `json:"type"`
				Name string `json:"name"`
			} `json:"tools"`
		}
		if err := json.NewDecoder(request.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
		if payload.Model != "mimo-v2.5-pro" || payload.Instructions != "system" || payload.MaxOutputTokens != 1024 {
			t.Errorf("request metadata = %#v", payload)
		}
		if payload.Reasoning.Effort != "none" {
			t.Errorf("reasoning effort = %q", payload.Reasoning.Effort)
		}
		if len(payload.Input) != 3 {
			t.Fatalf("input length = %d, want 3", len(payload.Input))
		}
		if payload.Input[0].Role != "user" || payload.Input[0].Content != "calculate" {
			t.Errorf("user input = %#v", payload.Input[0])
		}
		if payload.Input[1].Type != "function_call" || payload.Input[1].CallID != "call-1" || payload.Input[1].Name != "calculate" {
			t.Errorf("function call = %#v", payload.Input[1])
		}
		if payload.Input[2].Type != "function_call_output" || payload.Input[2].CallID != "call-1" || payload.Input[2].Output != `{"result":3}` {
			t.Errorf("function output = %#v", payload.Input[2])
		}
		if len(payload.Tools) != 1 || payload.Tools[0].Type != "function" || payload.Tools[0].Name != "calculate" {
			t.Errorf("tools = %#v", payload.Tools)
		}

		_, _ = response.Write([]byte(`{
			"status":"completed",
			"output":[
				{"type":"message","role":"assistant","content":[{"type":"output_text","text":"I need one more calculation."}]},
				{"type":"function_call","id":"item-2","call_id":"call-2","name":"calculate","arguments":"{\"operation\":\"add\",\"a\":3,\"b\":4}"}
			]
		}`))
	}))
	defer server.Close()

	backend, err := mimo.New(mimo.Config{
		Protocol: mimo.ProtocolResponses, BaseURL: server.URL + "/v1",
		APIKey: "secret", Model: "mimo-v2.5-pro", MaxTokens: 1024,
	})
	if err != nil {
		t.Fatal(err)
	}
	system, question, result := "system", "calculate", `{"result":3}`
	response, err := backend.Complete(context.Background(), []ai.Message{
		{Role: ai.RoleSystem, Content: &system},
		{Role: ai.RoleUser, Content: &question},
		{Role: ai.RoleAssistant, ToolCalls: []ai.ToolCall{{
			ID: "call-1", Type: "function",
			Function: ai.FunctionCall{Name: "calculate", Arguments: `{"operation":"add","a":1,"b":2}`},
		}}},
		{Role: ai.RoleTool, ToolCallID: "call-1", Content: &result},
	}, []ai.ToolDefinition{{
		Type:     "function",
		Function: ai.FunctionDefinition{Name: "calculate", Parameters: json.RawMessage(`{"type":"object"}`)},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if response.Content == nil || *response.Content != "I need one more calculation." || len(response.ToolCalls) != 1 {
		t.Fatalf("response = %#v", response)
	}
	if response.ToolCalls[0].ID != "call-2" || response.ToolCalls[0].Function.Name != "calculate" {
		t.Fatalf("tool call = %#v", response.ToolCalls[0])
	}
}

func TestAnthropicProtocol(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/anthropic/v1/messages" {
			t.Errorf("path = %q", request.URL.Path)
		}
		if request.Header.Get("api-key") != "secret" || request.Header.Get("x-api-key") != "" {
			t.Errorf("authentication headers = %#v", request.Header)
		}
		if request.Header.Get("anthropic-version") != "" {
			t.Errorf("unexpected anthropic-version = %q", request.Header.Get("anthropic-version"))
		}
		var payload struct {
			Thinking *struct {
				Type string `json:"type"`
			} `json:"thinking"`
		}
		if err := json.NewDecoder(request.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
		if payload.Thinking == nil || payload.Thinking.Type != "disabled" {
			t.Errorf("thinking = %#v", payload.Thinking)
		}
		_, _ = response.Write([]byte(`{"role":"assistant","content":[{"type":"text","text":"anthropic reply"}],"stop_reason":"end_turn"}`))
	}))
	defer server.Close()

	backend, err := mimo.New(mimo.Config{
		Protocol: mimo.ProtocolAnthropic, BaseURL: server.URL + "/anthropic/v1",
		APIKey: "secret", Model: "mimo-v2.5-pro",
	})
	if err != nil {
		t.Fatal(err)
	}
	question := "hello"
	response, err := backend.Complete(context.Background(), []ai.Message{{Role: ai.RoleUser, Content: &question}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if response.Content == nil || *response.Content != "anthropic reply" {
		t.Fatalf("response = %#v", response)
	}
}

func TestResponsesProtocolReportsIncomplete(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		_, _ = response.Write([]byte(`{"status":"incomplete","incomplete_details":{"reason":"max_output_tokens"},"output":[]}`))
	}))
	defer server.Close()
	backend, err := mimo.New(mimo.Config{Protocol: mimo.ProtocolResponses, BaseURL: server.URL, Model: "model"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = backend.Complete(context.Background(), nil, nil)
	if err == nil || !strings.Contains(err.Error(), "max_output_tokens") {
		t.Fatalf("Complete() error = %v", err)
	}
}

func TestNewValidation(t *testing.T) {
	if _, err := mimo.New(mimo.Config{Protocol: "other", Model: "model"}); err == nil {
		t.Fatal("New() expected unsupported protocol error")
	}
	if _, err := mimo.New(mimo.Config{Protocol: mimo.ProtocolResponses}); err == nil {
		t.Fatal("New() expected missing model error")
	}
}
