package openai

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"roleloom/internal/ai"
)

type streamTransport func(*http.Request) (*http.Response, error)

func (f streamTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func TestResponsesStream(t *testing.T) {
	transport := streamTransport(func(r *http.Request) (*http.Response, error) {
		data, _ := io.ReadAll(r.Body)
		if !strings.Contains(string(data), `"stream":true`) {
			t.Fatal("stream flag missing")
		}
		body := "data: {\"type\":\"response.output_text.delta\",\"delta\":\"one\"}\n\ndata: {\"type\":\"response.output_text.delta\",\"delta\":\" two\"}\n\ndata: [DONE]\n\n"
		return &http.Response{StatusCode: 200, Status: "200 OK", Body: io.NopCloser(strings.NewReader(body)), Header: http.Header{}}, nil
	})
	backend, err := NewResponses(ResponsesConfig{APIURL: "https://example.test/v1/responses", Model: "model", HTTPClient: &http.Client{Transport: transport}})
	if err != nil {
		t.Fatal(err)
	}
	streaming := backend.(ai.StreamingBackend)
	prompt := "hello"
	result, err := streaming.Stream(context.Background(), []ai.Message{{Role: ai.RoleUser, Content: &prompt}}, nil, nil)
	if err != nil || result.Content == nil || *result.Content != "one two" {
		t.Fatalf("result=%#v err=%v", result, err)
	}
}
