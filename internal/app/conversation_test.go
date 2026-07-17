package app_test

import (
	"context"
	"testing"

	"roleloom/internal/ai"
	"roleloom/internal/app"
	"roleloom/internal/domain"
	"roleloom/internal/store"
)

type fakeBackend struct{}

func (fakeBackend) Complete(context.Context, []ai.Message, []ai.ToolDefinition) (ai.Message, error) {
	answer := "summary"
	return ai.Message{Role: ai.RoleAssistant, Content: &answer}, nil
}
func (fakeBackend) Stream(_ context.Context, _ []ai.Message, _ []ai.ToolDefinition, sink ai.EventSink) (ai.Message, error) {
	for _, part := range []string{"hello", " world"} {
		if err := sink(ai.StreamEvent{Delta: part}); err != nil {
			return ai.Message{}, err
		}
	}
	answer := "hello world"
	return ai.Message{Role: ai.RoleAssistant, Content: &answer}, nil
}

type cancellingBackend struct{ started chan struct{} }

func (c cancellingBackend) Complete(context.Context, []ai.Message, []ai.ToolDefinition) (ai.Message, error) {
	return ai.Message{}, nil
}
func (c cancellingBackend) Stream(ctx context.Context, _ []ai.Message, _ []ai.ToolDefinition, sink ai.EventSink) (ai.Message, error) {
	if err := sink(ai.StreamEvent{Delta: "partial"}); err != nil {
		return ai.Message{}, err
	}
	close(c.started)
	<-ctx.Done()
	return ai.Message{}, ctx.Err()
}

func TestConversationStreamPersistsEventOrder(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	profile, err := st.SaveModelProfile(ctx, domain.ModelProfile{Name: "main", Provider: "openai", APIURL: "https://example.test/v1/chat/completions", Model: "model", IsDefault: true})
	if err != nil {
		t.Fatal(err)
	}
	character, err := st.SaveCharacter(ctx, domain.Character{Name: "Aster", Greeting: "welcome", DefaultModelProfileID: &profile.ID})
	if err != nil {
		t.Fatal(err)
	}
	service := app.NewConversationService(st, make([]byte, 32))
	service.SetModelFactory(func(domain.ModelProfile, string) (*ai.Client, error) { return ai.NewClient(fakeBackend{}) })
	conversation, err := service.CreateConversation(ctx, "", character.ID, nil)
	if err != nil {
		t.Fatal(err)
	}
	var events []string
	err = service.Send(ctx, conversation.ID, "client-1", "question", func(event app.StreamEvent) error { events = append(events, event.Type); return nil })
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"user_message", "assistant_start", "assistant_delta", "assistant_delta", "assistant_done"}
	if len(events) != len(want) {
		t.Fatalf("events=%v", events)
	}
	for i := range want {
		if events[i] != want[i] {
			t.Fatalf("events=%v", events)
		}
	}
	messages, err := st.ListMessages(ctx, conversation.ID, 0, 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(messages) != 3 || messages[0].Content != "welcome" || messages[2].Content != "hello world" || messages[2].Status != domain.MessageCompleted {
		t.Fatalf("messages=%#v", messages)
	}
}

func TestStopKeepsCancelledDraft(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	profile, err := st.SaveModelProfile(ctx, domain.ModelProfile{Name: "main", Provider: "openai", APIURL: "https://example.test/v1/chat/completions", Model: "model", IsDefault: true})
	if err != nil {
		t.Fatal(err)
	}
	character, err := st.SaveCharacter(ctx, domain.Character{Name: "Aster", DefaultModelProfileID: &profile.ID})
	if err != nil {
		t.Fatal(err)
	}
	started := make(chan struct{})
	service := app.NewConversationService(st, make([]byte, 32))
	service.SetModelFactory(func(domain.ModelProfile, string) (*ai.Client, error) {
		return ai.NewClient(cancellingBackend{started: started})
	})
	conversation, err := service.CreateConversation(ctx, "", character.ID, nil)
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- service.Send(ctx, conversation.ID, "client-cancel", "question", nil) }()
	<-started
	if !service.Stop(conversation.ID) {
		t.Fatal("generation was not active")
	}
	if err = <-done; err == nil {
		t.Fatal("expected cancellation")
	}
	messages, err := st.ListMessages(ctx, conversation.ID, 0, 100)
	if err != nil {
		t.Fatal(err)
	}
	draft := messages[len(messages)-1]
	if draft.Status != domain.MessageCancelled || draft.Content != "partial" {
		t.Fatalf("draft=%#v", draft)
	}
}
