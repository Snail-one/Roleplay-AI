package telegram

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"
)

const (
	defaultPollTimeoutSeconds = 30
	defaultMaxConcurrent      = 8
	telegramMessageLimit      = 4000
)

type API interface {
	GetUpdates(ctx context.Context, offset int64, timeoutSeconds int) ([]Update, error)
	SendMessage(ctx context.Context, chatID int64, text string) error
	SendChatAction(ctx context.Context, chatID int64, action string) error
}

type ChatAgent interface {
	Chat(ctx context.Context, input string) (string, error)
	Reset()
}

type AgentFactory func() (ChatAgent, error)

type BotOptions struct {
	AllowedUserIDs     []int64
	PollTimeoutSeconds int
	MaxConcurrent      int
	Logf               func(format string, arguments ...any)
}

type Bot struct {
	api           API
	agentFactory  AgentFactory
	allowedUsers  map[int64]struct{}
	pollTimeout   int
	semaphore     chan struct{}
	logf          func(string, ...any)
	sessionsMutex sync.Mutex
	sessions      map[int64]*chatSession
	workers       sync.WaitGroup
}

type chatSession struct {
	mutex sync.Mutex
	agent ChatAgent
}

func NewBot(api API, agentFactory AgentFactory, options BotOptions) (*Bot, error) {
	if api == nil {
		return nil, errors.New("Telegram API client is required")
	}
	if agentFactory == nil {
		return nil, errors.New("agent factory is required")
	}
	pollTimeout := options.PollTimeoutSeconds
	if pollTimeout == 0 {
		pollTimeout = defaultPollTimeoutSeconds
	}
	if pollTimeout < 1 {
		return nil, errors.New("Telegram poll timeout must be positive")
	}
	maxConcurrent := options.MaxConcurrent
	if maxConcurrent == 0 {
		maxConcurrent = defaultMaxConcurrent
	}
	if maxConcurrent < 1 {
		return nil, errors.New("maximum concurrent Telegram messages must be positive")
	}
	allowedUsers := make(map[int64]struct{}, len(options.AllowedUserIDs))
	for _, userID := range options.AllowedUserIDs {
		if userID <= 0 {
			return nil, fmt.Errorf("invalid allowed Telegram user ID %d", userID)
		}
		allowedUsers[userID] = struct{}{}
	}
	logf := options.Logf
	if logf == nil {
		logf = func(string, ...any) {}
	}
	return &Bot{
		api: api, agentFactory: agentFactory, allowedUsers: allowedUsers,
		pollTimeout: pollTimeout, semaphore: make(chan struct{}, maxConcurrent),
		logf: logf, sessions: make(map[int64]*chatSession),
	}, nil
}

func (b *Bot) Run(ctx context.Context) error {
	var offset int64
	backoff := time.Second
	for {
		updates, err := b.api.GetUpdates(ctx, offset, b.pollTimeout)
		if err != nil {
			if ctx.Err() != nil {
				b.workers.Wait()
				return nil
			}
			var apiError *APIError
			if errors.As(err, &apiError) && (apiError.Code == http.StatusUnauthorized || apiError.Code == http.StatusNotFound || apiError.Code == http.StatusConflict) {
				b.workers.Wait()
				return err
			}
			b.logf("获取 Telegram 更新失败: %v", err)
			timer := time.NewTimer(backoff)
			select {
			case <-ctx.Done():
				timer.Stop()
				b.workers.Wait()
				return nil
			case <-timer.C:
			}
			if backoff < 30*time.Second {
				backoff *= 2
			}
			continue
		}
		backoff = time.Second

		for _, update := range updates {
			if update.UpdateID >= offset {
				offset = update.UpdateID + 1
			}
			if update.Message == nil || strings.TrimSpace(update.Message.Text) == "" {
				continue
			}
			message := *update.Message
			select {
			case b.semaphore <- struct{}{}:
			case <-ctx.Done():
				b.workers.Wait()
				return nil
			}
			b.workers.Add(1)
			go func() {
				defer func() {
					<-b.semaphore
					b.workers.Done()
				}()
				b.handleMessage(ctx, message)
			}()
		}
	}
}

func (b *Bot) handleMessage(ctx context.Context, message Message) {
	if message.From != nil && message.From.IsBot {
		return
	}
	if !b.isAllowed(message.From) {
		userID := int64(0)
		if message.From != nil {
			userID = message.From.ID
		}
		b.logf("忽略未授权 Telegram 用户 %d（chat_id=%d）", userID, message.Chat.ID)
		return
	}

	session, err := b.session(message.Chat.ID)
	if err != nil {
		b.logf("创建 Telegram 会话失败（chat_id=%d）: %v", message.Chat.ID, err)
		_ = b.api.SendMessage(ctx, message.Chat.ID, "创建对话失败，请稍后重试。")
		return
	}
	session.mutex.Lock()
	defer session.mutex.Unlock()

	text := strings.TrimSpace(message.Text)
	switch telegramCommand(text) {
	case "start":
		session.agent.Reset()
		b.sendText(ctx, message.Chat.ID, "你好，我是 RoleLoom AI Agent。直接发送消息即可聊天。\n\n命令：\n/reset - 清空当前对话\n/id - 查看用户和聊天 ID\n/help - 查看帮助")
		return
	case "help":
		b.sendText(ctx, message.Chat.ID, "直接发送文本即可与 AI 对话。\n/reset - 清空当前聊天的上下文\n/id - 查看用户和聊天 ID\n/help - 查看帮助")
		return
	case "reset":
		session.agent.Reset()
		b.sendText(ctx, message.Chat.ID, "当前对话上下文已清空。")
		return
	case "id":
		userID := int64(0)
		if message.From != nil {
			userID = message.From.ID
		}
		b.sendText(ctx, message.Chat.ID, fmt.Sprintf("user_id: %d\nchat_id: %d", userID, message.Chat.ID))
		return
	case "unknown":
		b.sendText(ctx, message.Chat.ID, "未知命令。发送 /help 查看可用命令。")
		return
	}

	if err := b.api.SendChatAction(ctx, message.Chat.ID, "typing"); err != nil && ctx.Err() == nil {
		b.logf("发送 Telegram 输入状态失败（chat_id=%d）: %v", message.Chat.ID, err)
	}
	answer, err := session.agent.Chat(ctx, text)
	if err != nil {
		if ctx.Err() == nil {
			b.logf("Telegram Agent 处理失败（chat_id=%d）: %v", message.Chat.ID, err)
			b.sendText(ctx, message.Chat.ID, "处理消息失败，请稍后重试。")
		}
		return
	}
	b.sendText(ctx, message.Chat.ID, answer)
}

func (b *Bot) session(chatID int64) (*chatSession, error) {
	b.sessionsMutex.Lock()
	defer b.sessionsMutex.Unlock()
	if session, exists := b.sessions[chatID]; exists {
		return session, nil
	}
	chatAgent, err := b.agentFactory()
	if err != nil {
		return nil, err
	}
	session := &chatSession{agent: chatAgent}
	b.sessions[chatID] = session
	return session, nil
}

func (b *Bot) isAllowed(user *User) bool {
	if len(b.allowedUsers) == 0 {
		return true
	}
	if user == nil {
		return false
	}
	_, allowed := b.allowedUsers[user.ID]
	return allowed
}

func (b *Bot) sendText(ctx context.Context, chatID int64, text string) {
	for _, part := range splitTelegramText(text) {
		if err := b.api.SendMessage(ctx, chatID, part); err != nil {
			if ctx.Err() == nil {
				b.logf("发送 Telegram 消息失败（chat_id=%d）: %v", chatID, err)
			}
			return
		}
	}
}

func telegramCommand(text string) string {
	fields := strings.Fields(text)
	if len(fields) == 0 || !strings.HasPrefix(fields[0], "/") {
		return ""
	}
	command := strings.TrimPrefix(fields[0], "/")
	if index := strings.IndexByte(command, '@'); index >= 0 {
		command = command[:index]
	}
	switch strings.ToLower(command) {
	case "start", "help", "reset", "id":
		return strings.ToLower(command)
	default:
		return "unknown"
	}
}

func splitTelegramText(text string) []string {
	runes := []rune(strings.TrimSpace(text))
	if len(runes) == 0 {
		return nil
	}
	parts := make([]string, 0, (len(runes)+telegramMessageLimit-1)/telegramMessageLimit)
	for len(runes) > 0 {
		length := min(len(runes), telegramMessageLimit)
		parts = append(parts, string(runes[:length]))
		runes = runes[length:]
	}
	return parts
}
