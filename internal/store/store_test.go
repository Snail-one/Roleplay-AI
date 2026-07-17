package store_test

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"roleloom/internal/domain"
	"roleloom/internal/store"
)

func TestPersistenceSnapshotAndMessageTransactions(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "roleloom.db")
	st, err := store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	var journal string
	if err = st.DB().QueryRow(`PRAGMA journal_mode`).Scan(&journal); err != nil || journal != "wal" {
		t.Fatalf("journal=%q err=%v", journal, err)
	}
	profile, err := st.SaveModelProfile(ctx, domain.ModelProfile{Name: "main", Provider: "openai", APIURL: "https://example.test/v1/chat/completions", Model: "model", IsDefault: true})
	if err != nil {
		t.Fatal(err)
	}
	character, err := st.SaveCharacter(ctx, domain.Character{Name: "Aster", Greeting: "hello", DefaultModelProfileID: &profile.ID})
	if err != nil {
		t.Fatal(err)
	}
	conversation, err := st.CreateConversation(ctx, domain.Conversation{Title: "Aster", CharacterID: character.ID, CharacterSnapshot: character.Snapshot(), ModelProfileID: profile.ID}, character.Greeting)
	if err != nil {
		t.Fatal(err)
	}
	character.Name = "Changed"
	if _, err = st.SaveCharacter(ctx, character); err != nil {
		t.Fatal(err)
	}
	loaded, err := st.GetConversation(ctx, conversation.ID)
	if err != nil || loaded.CharacterSnapshot.Name != "Aster" {
		t.Fatalf("snapshot=%#v err=%v", loaded.CharacterSnapshot, err)
	}
	user, draft, duplicate, err := st.AppendUserAndDraft(ctx, conversation.ID, "client-1", "question")
	if err != nil || duplicate {
		t.Fatal(err)
	}
	sameUser, sameDraft, duplicate, err := st.AppendUserAndDraft(ctx, conversation.ID, "client-1", "question")
	if err != nil || !duplicate || sameUser.ID != user.ID || sameDraft.ID != draft.ID {
		t.Fatalf("duplicate=%v err=%v", duplicate, err)
	}
	if err = st.UpdateMessage(ctx, draft.ID, "answer", domain.MessageCompleted, ""); err != nil {
		t.Fatal(err)
	}
	if err = st.EditLatestUser(ctx, conversation.ID, user.ID, "edited"); err != nil {
		t.Fatal(err)
	}
	messages, err := st.ListMessages(ctx, conversation.ID, 0, 100)
	if err != nil || len(messages) != 2 || messages[1].Content != "edited" {
		t.Fatalf("messages=%#v err=%v", messages, err)
	}
	if err = st.DeleteModelProfile(ctx, profile.ID); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("delete referenced model=%v", err)
	}
	if err = st.Close(); err != nil {
		t.Fatal(err)
	}
	st, err = store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if _, err = st.GetConversation(ctx, conversation.ID); err != nil {
		t.Fatalf("restart lost conversation: %v", err)
	}
	if err = st.DeleteCharacter(ctx, character.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = st.GetConversation(ctx, conversation.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("cascade failed: %v", err)
	}
}
