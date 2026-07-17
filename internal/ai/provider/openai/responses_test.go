package openai_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"roleloom/internal/ai"
	"roleloom/internal/ai/provider/openai"
)

func TestResponsesToolRoundTrip(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/v1/responses" {
			t.Errorf("path = %q", request.URL.Path)
		}
		if request.Header.Get("Authorization") != "Bearer secret" {
			t.Errorf("Authorization = %q", request.Header.Get("Authorization"))
		}
		if request.Header.Get("api-key") != "" {
			t.Errorf("unexpected api-key = %q", request.Header.Get("api-key"))
		}

		var payload struct {
			Model           string          `json:"model"`
			Instructions    string          `json:"instructions"`
			MaxOutputTokens int             `json:"max_output_tokens"`
			Reasoning       json.RawMessage `json:"reasoning"`
			Input           []struct {
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
		if payload.Model != "model" || payload.Instructions != "system" || payload.MaxOutputTokens != 1024 {
			t.Errorf("request metadata = %#v", payload)
		}
		if payload.Reasoning != nil {
			t.Errorf("OpenAI request should omit reasoning, got %s", payload.Reasoning)
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

		response.Header().Set("Content-Type", "application/json")
		_, _ = response.Write([]byte(`{
			"status":"completed",
			"output":[
				{"type":"message","role":"assistant","content":[{"type":"output_text","text":"one more calculation"}]},
				{"type":"function_call","id":"item-2","call_id":"call-2","name":"calculate","arguments":"{\"operation\":\"add\",\"a\":3,\"b\":4}"}
			]
		}`))
	}))
	defer server.Close()

	backend, err := openai.New(openai.Config{
		APIURL: server.URL + "/v1/responses", APIKey: "secret",
		Model: "model", MaxTokens: 1024,
	})
	if err != nil {
		t.Fatal(err)
	}
	system, question, result := "system", "calculate", `{"result":3}`
	message, err := backend.Complete(context.Background(), []ai.Message{
		{Role: ai.RoleSystem, Content: &system},
		{Role: ai.RoleUser, Content: &question},
		{Role: ai.RoleAssistant, ToolCalls: []ai.ToolCall{{
			ID: "call-1", Type: "function",
			Function: ai.FunctionCall{Name: "calculate", Arguments: `{"operation":"add","a":1,"b":2}`},
		}}},
		{Role: ai.RoleTool, ToolCallID: "call-1", Content: &result},
	}, []ai.ToolDefinition{{
		Type: "function",
		Function: ai.FunctionDefinition{
			Name: "calculate", Parameters: json.RawMessage(`{"type":"object"}`),
		},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if message.Content == nil || *message.Content != "one more calculation" || len(message.ToolCalls) != 1 {
		t.Fatalf("message = %#v", message)
	}
	if message.ToolCalls[0].ID != "call-2" || message.ToolCalls[0].Function.Name != "calculate" {
		t.Fatalf("tool call = %#v", message.ToolCalls[0])
	}
}

func TestResponsesReportsIncomplete(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		_, _ = response.Write([]byte(`{"status":"incomplete","incomplete_details":{"reason":"max_output_tokens"},"output":[]}`))
	}))
	defer server.Close()
	backend, err := openai.NewResponses(openai.ResponsesConfig{APIURL: server.URL + "/v1/responses", Model: "model"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = backend.Complete(context.Background(), nil, nil)
	if err == nil || !strings.Contains(err.Error(), "max_output_tokens") {
		t.Fatalf("Complete() error = %v", err)
	}
}

func TestResponsesValidation(t *testing.T) {
	if _, err := openai.NewResponses(openai.ResponsesConfig{APIURL: "https://api.openai.com/v1", Model: "model"}); err == nil {
		t.Fatal("NewResponses() expected full endpoint URL error")
	}
	if _, err := openai.NewResponses(openai.ResponsesConfig{APIURL: "https://api.openai.com/v1/responses"}); err == nil {
		t.Fatal("NewResponses() expected missing model error")
	}
	if _, err := openai.NewResponses(openai.ResponsesConfig{APIURL: "https://api.openai.com/v1/responses", Model: "model", APIKeyHeader: "bad header"}); err == nil {
		t.Fatal("NewResponses() expected invalid header error")
	}
}
