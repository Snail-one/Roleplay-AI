package ai_test

import (
	"context"
	"errors"
	"testing"

	"roleloom/internal/ai"
)

type fakeBackend struct {
	response ai.Message
	err      error
	called   bool
}

func (b *fakeBackend) Complete(_ context.Context, _ []ai.Message, _ []ai.ToolDefinition) (ai.Message, error) {
	b.called = true
	return b.response, b.err
}

func TestClientDelegatesToBackend(t *testing.T) {
	content := "hello"
	backend := &fakeBackend{response: ai.Message{Role: ai.RoleAssistant, Content: &content}}
	client, err := ai.NewClient(backend)
	if err != nil {
		t.Fatal(err)
	}
	response, err := client.Complete(context.Background(), nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !backend.called || response.Content == nil || *response.Content != "hello" {
		t.Fatalf("backend called = %v, response = %#v", backend.called, response)
	}
}

func TestClientPropagatesBackendError(t *testing.T) {
	backendError := errors.New("backend failed")
	client, err := ai.NewClient(&fakeBackend{err: backendError})
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.Complete(context.Background(), nil, nil)
	if !errors.Is(err, backendError) {
		t.Fatalf("Complete() error = %v", err)
	}
}

func TestNewClientRequiresBackend(t *testing.T) {
	if _, err := ai.NewClient(nil); err == nil {
		t.Fatal("NewClient() expected error")
	}
}
