package telegram

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestClientCallsTelegramAPI(t *testing.T) {
	var methods []string
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		methods = append(methods, request.URL.Path)
		response.Header().Set("Content-Type", "application/json")
		switch request.URL.Path {
		case "/bot123:secret/getMe":
			_, _ = response.Write([]byte(`{"ok":true,"result":{"id":123,"is_bot":true,"first_name":"RoleLoom","username":"roleloom_bot"}}`))
		case "/bot123:secret/getUpdates":
			var payload struct {
				Offset         int64    `json:"offset"`
				Limit          int      `json:"limit"`
				Timeout        int      `json:"timeout"`
				AllowedUpdates []string `json:"allowed_updates"`
			}
			if err := json.NewDecoder(request.Body).Decode(&payload); err != nil {
				t.Fatal(err)
			}
			if payload.Offset != 42 || payload.Limit != 100 || payload.Timeout != 30 || len(payload.AllowedUpdates) != 1 || payload.AllowedUpdates[0] != "message" {
				t.Errorf("getUpdates payload = %#v", payload)
			}
			_, _ = response.Write([]byte(`{"ok":true,"result":[{"update_id":42,"message":{"message_id":7,"from":{"id":9,"is_bot":false,"first_name":"User"},"chat":{"id":10,"type":"private"},"text":"hello"}}]}`))
		case "/bot123:secret/sendMessage":
			var payload struct {
				ChatID int64  `json:"chat_id"`
				Text   string `json:"text"`
			}
			if err := json.NewDecoder(request.Body).Decode(&payload); err != nil {
				t.Fatal(err)
			}
			if payload.ChatID != 10 || payload.Text != "reply" {
				t.Errorf("sendMessage payload = %#v", payload)
			}
			_, _ = response.Write([]byte(`{"ok":true,"result":{"message_id":8}}`))
		case "/bot123:secret/sendChatAction":
			var payload struct {
				ChatID int64  `json:"chat_id"`
				Action string `json:"action"`
			}
			if err := json.NewDecoder(request.Body).Decode(&payload); err != nil {
				t.Fatal(err)
			}
			if payload.ChatID != 10 || payload.Action != "typing" {
				t.Errorf("sendChatAction payload = %#v", payload)
			}
			_, _ = response.Write([]byte(`{"ok":true,"result":true}`))
		default:
			http.NotFound(response, request)
		}
	}))
	defer server.Close()

	client, err := NewClient(ClientConfig{BotToken: "123:secret", APIURL: server.URL})
	if err != nil {
		t.Fatal(err)
	}
	botUser, err := client.GetMe(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if botUser.ID != 123 || botUser.Username != "roleloom_bot" || !botUser.IsBot {
		t.Fatalf("bot user = %#v", botUser)
	}
	updates, err := client.GetUpdates(context.Background(), 42, 30)
	if err != nil {
		t.Fatal(err)
	}
	if len(updates) != 1 || updates[0].Message == nil || updates[0].Message.Text != "hello" {
		t.Fatalf("updates = %#v", updates)
	}
	if err := client.SendMessage(context.Background(), 10, "reply"); err != nil {
		t.Fatal(err)
	}
	if err := client.SendChatAction(context.Background(), 10, "typing"); err != nil {
		t.Fatal(err)
	}
	if len(methods) != 4 {
		t.Fatalf("methods = %#v", methods)
	}
}

func TestClientReportsAPIErrorWithoutLeakingToken(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		response.WriteHeader(http.StatusUnauthorized)
		_, _ = response.Write([]byte(`{"ok":false,"error_code":401,"description":"Unauthorized"}`))
	}))
	defer server.Close()
	client, err := NewClient(ClientConfig{BotToken: "123:secret", APIURL: server.URL})
	if err != nil {
		t.Fatal(err)
	}
	err = client.SendMessage(context.Background(), 1, "hello")
	if err == nil || !strings.Contains(err.Error(), "401") || strings.Contains(err.Error(), "123:secret") {
		t.Fatalf("SendMessage() error = %v", err)
	}
}

func TestNewClientValidation(t *testing.T) {
	if _, err := NewClient(ClientConfig{}); err == nil {
		t.Fatal("NewClient() expected missing token error")
	}
	if _, err := NewClient(ClientConfig{BotToken: "bad/token"}); err == nil {
		t.Fatal("NewClient() expected invalid token error")
	}
	if _, err := NewClient(ClientConfig{BotToken: "token", APIURL: "relative"}); err == nil {
		t.Fatal("NewClient() expected invalid API URL error")
	}
}
