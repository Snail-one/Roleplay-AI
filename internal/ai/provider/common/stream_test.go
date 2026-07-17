package common

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"roleloom/internal/ai"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func streamResponse(body string) *http.Response {
	return &http.Response{StatusCode: 200, Status: "200 OK", Header: http.Header{"Content-Type": []string{"text/event-stream"}}, Body: io.NopCloser(strings.NewReader(body))}
}

func TestChatCompletionsStream(t *testing.T) {
	transport := roundTripFunc(func(r *http.Request) (*http.Response, error) {
		data, _ := io.ReadAll(r.Body)
		if !strings.Contains(string(data), `"stream":true`) {
			t.Fatalf("request=%s", data)
		}
		return streamResponse("data: {\"choices\":[{\"delta\":{\"content\":\"你\"}}]}\n\ndata: {\"choices\":[{\"delta\":{\"content\":\"好\",\"tool_calls\":[{\"index\":0,\"id\":\"call\",\"function\":{\"name\":\"calculate\",\"arguments\":\"{\\\"a\\\":1\"}}]}}]}\n\ndata: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"function\":{\"arguments\":\",\\\"b\\\":2}\"}}]}}]}\n\ndata: [DONE]\n\n"), nil
	})
	client, err := NewChatCompletions(ChatCompletionsConfig{APIURL: "https://example.test/v1/chat/completions", Model: "model", HTTPClient: &http.Client{Transport: transport}})
	if err != nil {
		t.Fatal(err)
	}
	prompt := "hello"
	var deltas strings.Builder
	result, err := client.Stream(context.Background(), []ai.Message{{Role: ai.RoleUser, Content: &prompt}}, nil, func(e ai.StreamEvent) error { deltas.WriteString(e.Delta); return nil })
	if err != nil {
		t.Fatal(err)
	}
	if deltas.String() != "你好" || result.Content == nil || *result.Content != "你好" || len(result.ToolCalls) != 1 || result.ToolCalls[0].Function.Arguments != `{"a":1,"b":2}` {
		t.Fatalf("result=%#v deltas=%q", result, deltas.String())
	}
}

func TestAnthropicStream(t *testing.T) {
	transport := roundTripFunc(func(*http.Request) (*http.Response, error) {
		return streamResponse("event: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"text\"}}\n\nevent: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"hello\"}}\n\n"), nil
	})
	client, err := NewAnthropicMessages(AnthropicMessagesConfig{APIURL: "https://example.test/v1/messages", Model: "model", HTTPClient: &http.Client{Transport: transport}})
	if err != nil {
		t.Fatal(err)
	}
	prompt := "hello"
	result, err := client.Stream(context.Background(), []ai.Message{{Role: ai.RoleUser, Content: &prompt}}, nil, nil)
	if err != nil || result.Content == nil || *result.Content != "hello" {
		t.Fatalf("result=%#v err=%v", result, err)
	}
}
