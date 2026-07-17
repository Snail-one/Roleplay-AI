package openaicompatible_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"roleloom/internal/ai"
	openaicompatible "roleloom/internal/ai/provider/openai_compatible"
)

func TestNewRequiresAPIURL(t *testing.T) {
	if _, err := openaicompatible.New(openaicompatible.Config{Model: "model"}); err == nil {
		t.Fatal("New() expected missing API URL error")
	}
}

func TestNew(t *testing.T) {
	backend, err := openaicompatible.New(openaicompatible.Config{
		APIURL: "http://localhost:8000/v1/chat/completions",
		Model:  "model",
	})
	if err != nil {
		t.Fatal(err)
	}
	if backend == nil {
		t.Fatal("New() returned nil backend")
	}
}

func TestNewCompletesChatCompletionsPath(t *testing.T) {
	tests := []struct {
		name       string
		configured string
		wantPath   string
	}{
		{name: "root URL", wantPath: "/chat/completions"},
		{name: "API prefix", configured: "/v1/", wantPath: "/v1/chat/completions"},
		{name: "complete endpoint", configured: "/v1/chat/completions", wantPath: "/v1/chat/completions"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
				if request.URL.Path != test.wantPath {
					t.Errorf("path = %q, want %q", request.URL.Path, test.wantPath)
				}
				_, _ = response.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"ok"}}]}`))
			}))
			defer server.Close()

			backend, err := openaicompatible.New(openaicompatible.Config{
				APIURL: server.URL + test.configured, Model: "model",
			})
			if err != nil {
				t.Fatal(err)
			}
			message, err := backend.Complete(context.Background(), nil, nil)
			if err != nil {
				t.Fatal(err)
			}
			if message.Role != ai.RoleAssistant {
				t.Fatalf("message = %#v", message)
			}
		})
	}
}

func TestNewUsesResponsesEndpoint(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/v1/responses" {
			t.Errorf("path = %q, want /v1/responses", request.URL.Path)
		}
		if request.Header.Get("Authorization") != "Bearer secret" {
			t.Errorf("Authorization = %q", request.Header.Get("Authorization"))
		}
		_, _ = response.Write([]byte(`{
			"status":"completed",
			"output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"ok"}]}]
		}`))
	}))
	defer server.Close()

	backend, err := openaicompatible.New(openaicompatible.Config{
		APIURL: server.URL + "/v1/responses", APIKey: "secret", Model: "model",
	})
	if err != nil {
		t.Fatal(err)
	}
	message, err := backend.Complete(context.Background(), nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if message.Content == nil || *message.Content != "ok" {
		t.Fatalf("message = %#v", message)
	}
}

func TestNewRejectsMessagesEndpoint(t *testing.T) {
	if _, err := openaicompatible.New(openaicompatible.Config{
		APIURL: "https://example.com/v1/messages", Model: "model",
	}); err == nil {
		t.Fatal("New() expected unsupported messages endpoint error")
	}
}
