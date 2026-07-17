package common

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"roleloom/internal/ai"
)

func TestClientComplete(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodPost {
			t.Errorf("method = %s", request.Method)
		}
		if request.URL.Path != "/v1/chat/completions" {
			t.Errorf("path = %s", request.URL.Path)
		}
		if got := request.Header.Get("Authorization"); got != "Bearer secret" {
			t.Errorf("Authorization = %q", got)
		}

		var payload struct {
			Model     string              `json:"model"`
			Messages  []ai.Message        `json:"messages"`
			Tools     []ai.ToolDefinition `json:"tools"`
			MaxTokens int                 `json:"max_tokens"`
		}
		if err := json.NewDecoder(request.Body).Decode(&payload); err != nil {
			t.Errorf("decode request: %v", err)
		}
		if payload.Model != "test-model" || payload.MaxTokens != 2048 || len(payload.Messages) != 1 || len(payload.Tools) != 1 {
			t.Errorf("request payload = %#v", payload)
		}

		response.Header().Set("Content-Type", "application/json")
		_, _ = response.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"hello"}}]}`))
	}))
	defer server.Close()

	client, err := NewChatCompletions(ChatCompletionsConfig{
		BaseURL:   server.URL + "/v1/",
		APIKey:    "secret",
		Model:     "test-model",
		MaxTokens: 2048,
	})
	if err != nil {
		t.Fatal(err)
	}
	content := "hi"
	message, err := client.Complete(context.Background(), []ai.Message{{Role: ai.RoleUser, Content: &content}}, []ai.ToolDefinition{{
		Type:     "function",
		Function: ai.FunctionDefinition{Name: "test", Parameters: json.RawMessage(`{"type":"object"}`)},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if message.Content == nil || *message.Content != "hello" {
		t.Fatalf("message = %#v", message)
	}
}

func TestClientDecodesToolCallsAndOmitsEmptyAuth(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if got := request.Header.Get("Authorization"); got != "" {
			t.Errorf("Authorization = %q, want empty", got)
		}
		var requestPayload struct {
			Messages []map[string]json.RawMessage `json:"messages"`
		}
		if err := json.NewDecoder(request.Body).Decode(&requestPayload); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if len(requestPayload.Messages) != 1 || string(requestPayload.Messages[0]["content"]) != "null" {
			t.Errorf("tool-call assistant content was not preserved as null: %#v", requestPayload.Messages)
		}
		if string(requestPayload.Messages[0]["reasoning_content"]) != `"prior reasoning"` {
			t.Errorf("reasoning_content was not preserved: %#v", requestPayload.Messages)
		}
		_, _ = response.Write([]byte(`{
            "choices":[{"message":{"role":"assistant","tool_calls":[{
                "id":"call-1","type":"function",
                "function":{"name":"calculate","arguments":"{\"operation\":\"add\",\"a\":1,\"b\":2}"}
			}],"reasoning_content":"reasoning trace"}}]
        }`))
	}))
	defer server.Close()

	client, err := NewChatCompletions(ChatCompletionsConfig{BaseURL: server.URL, Model: "model"})
	if err != nil {
		t.Fatal(err)
	}
	reasoning := "prior reasoning"
	message, err := client.Complete(context.Background(), []ai.Message{{
		Role:             ai.RoleAssistant,
		ReasoningContent: &reasoning,
		ToolCalls: []ai.ToolCall{{
			ID: "prior-call", Type: "function",
			Function: ai.FunctionCall{Name: "calculate", Arguments: `{}`},
		}},
	}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(message.ToolCalls) != 1 || message.ToolCalls[0].Function.Name != "calculate" {
		t.Fatalf("message = %#v", message)
	}
	if message.ReasoningContent == nil || *message.ReasoningContent != "reasoning trace" {
		t.Fatalf("reasoning_content = %#v", message.ReasoningContent)
	}
}

func TestClientReportsAPIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		response.WriteHeader(http.StatusTooManyRequests)
		_, _ = response.Write([]byte(`{"error":{"message":"rate limited"}}`))
	}))
	defer server.Close()

	client, err := NewChatCompletions(ChatCompletionsConfig{BaseURL: server.URL, Model: "model"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.Complete(context.Background(), nil, nil)
	if err == nil || !strings.Contains(err.Error(), "429") || !strings.Contains(err.Error(), "rate limited") {
		t.Fatalf("Complete() error = %v", err)
	}
}

func TestClientRejectsMalformedResponses(t *testing.T) {
	tests := []struct {
		name string
		body string
		want string
	}{
		{name: "invalid JSON", body: `{`, want: "decode response"},
		{name: "no choices", body: `{"choices":[]}`, want: "no choices"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
				_, _ = response.Write([]byte(test.body))
			}))
			defer server.Close()

			client, err := NewChatCompletions(ChatCompletionsConfig{BaseURL: server.URL, Model: "model"})
			if err != nil {
				t.Fatal(err)
			}
			_, err = client.Complete(context.Background(), nil, nil)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("Complete() error = %v, want %q", err, test.want)
			}
		})
	}
}

func TestClientTimeout(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		select {
		case <-request.Context().Done():
		case <-time.After(time.Second):
			response.WriteHeader(http.StatusOK)
		}
	}))
	defer server.Close()

	client, err := NewChatCompletions(ChatCompletionsConfig{BaseURL: server.URL, Model: "model", Timeout: 20 * time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.Complete(context.Background(), nil, nil)
	if err == nil || !strings.Contains(err.Error(), "Client.Timeout") {
		t.Fatalf("Complete() error = %v", err)
	}
}

func TestNewClientValidation(t *testing.T) {
	if _, err := NewChatCompletions(ChatCompletionsConfig{BaseURL: "relative", Model: "model"}); err == nil {
		t.Fatal("NewClient() expected invalid URL error")
	}
	if _, err := NewChatCompletions(ChatCompletionsConfig{BaseURL: "https://example.com/v1?x=1", Model: "model"}); err == nil {
		t.Fatal("NewClient() expected query error")
	}
	if _, err := NewChatCompletions(ChatCompletionsConfig{}); err == nil {
		t.Fatal("NewClient() expected missing model error")
	}
}
