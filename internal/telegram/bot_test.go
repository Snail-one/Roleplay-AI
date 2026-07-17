package telegram

import (
	"context"
	"strings"
	"sync"
	"testing"
)

type fakeTelegramAPI struct {
	mutex    sync.Mutex
	messages []sentMessage
	actions  []sentAction
}

type sentMessage struct {
	chatID int64
	text   string
}

type sentAction struct {
	chatID int64
	action string
}

func (f *fakeTelegramAPI) GetUpdates(context.Context, int64, int) ([]Update, error) {
	return nil, nil
}

func (f *fakeTelegramAPI) SendMessage(_ context.Context, chatID int64, text string) error {
	f.mutex.Lock()
	defer f.mutex.Unlock()
	f.messages = append(f.messages, sentMessage{chatID: chatID, text: text})
	return nil
}

func (f *fakeTelegramAPI) SendChatAction(_ context.Context, chatID int64, action string) error {
	f.mutex.Lock()
	defer f.mutex.Unlock()
	f.actions = append(f.actions, sentAction{chatID: chatID, action: action})
	return nil
}

type fakeChatAgent struct {
	inputs []string
	resets int
}

func (f *fakeChatAgent) Chat(_ context.Context, input string) (string, error) {
	f.inputs = append(f.inputs, input)
	return "answer: " + input, nil
}

func (f *fakeChatAgent) Reset() {
	f.resets++
}

func TestBotIsolatesChatsAndHandlesCommands(t *testing.T) {
	api := &fakeTelegramAPI{}
	var agents []*fakeChatAgent
	bot, err := NewBot(api, func() (ChatAgent, error) {
		agent := &fakeChatAgent{}
		agents = append(agents, agent)
		return agent, nil
	}, BotOptions{AllowedUserIDs: []int64{1}})
	if err != nil {
		t.Fatal(err)
	}

	bot.handleMessage(context.Background(), testMessage(10, 1, "hello"))
	bot.handleMessage(context.Background(), testMessage(10, 1, "/reset"))
	bot.handleMessage(context.Background(), testMessage(20, 1, "other chat"))
	bot.handleMessage(context.Background(), testMessage(30, 2, "unauthorized"))

	if len(agents) != 2 {
		t.Fatalf("agent count = %d, want 2", len(agents))
	}
	if len(agents[0].inputs) != 1 || agents[0].inputs[0] != "hello" || agents[0].resets != 1 {
		t.Fatalf("first agent = %#v", agents[0])
	}
	if len(agents[1].inputs) != 1 || agents[1].inputs[0] != "other chat" {
		t.Fatalf("second agent = %#v", agents[1])
	}
	if len(api.actions) != 2 {
		t.Fatalf("actions = %#v", api.actions)
	}
	if len(api.messages) != 3 || api.messages[0].text != "answer: hello" || api.messages[1].text != "当前对话上下文已清空。" || api.messages[2].text != "answer: other chat" {
		t.Fatalf("messages = %#v", api.messages)
	}
}

func TestBotCommands(t *testing.T) {
	tests := map[string]string{
		"hello":        "",
		"/start":       "start",
		"/HELP@my_bot": "help",
		"/reset now":   "reset",
		"/id":          "id",
		"/other":       "unknown",
	}
	for input, want := range tests {
		if got := telegramCommand(input); got != want {
			t.Errorf("telegramCommand(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestSplitTelegramText(t *testing.T) {
	text := strings.Repeat("你", telegramMessageLimit+25)
	parts := splitTelegramText(text)
	if len(parts) != 2 || len([]rune(parts[0])) != telegramMessageLimit || len([]rune(parts[1])) != 25 {
		t.Fatalf("parts lengths = %d, %d", len([]rune(parts[0])), len([]rune(parts[1])))
	}
}

func TestNewBotValidation(t *testing.T) {
	if _, err := NewBot(nil, func() (ChatAgent, error) { return &fakeChatAgent{}, nil }, BotOptions{}); err == nil {
		t.Fatal("NewBot() expected missing API error")
	}
	if _, err := NewBot(&fakeTelegramAPI{}, nil, BotOptions{}); err == nil {
		t.Fatal("NewBot() expected missing factory error")
	}
	if _, err := NewBot(&fakeTelegramAPI{}, func() (ChatAgent, error) { return &fakeChatAgent{}, nil }, BotOptions{AllowedUserIDs: []int64{-1}}); err == nil {
		t.Fatal("NewBot() expected invalid allowed user error")
	}
}

func testMessage(chatID, userID int64, text string) Message {
	return Message{
		Chat: Chat{ID: chatID, Type: "private"},
		From: &User{ID: userID, FirstName: "User"},
		Text: text,
	}
}
