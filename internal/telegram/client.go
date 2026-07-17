package telegram

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	defaultAPIURL       = "https://api.telegram.org"
	defaultHTTPTimeout  = 45 * time.Second
	maxResponseBodySize = 16 << 20
)

type ClientConfig struct {
	BotToken   string
	APIURL     string
	Timeout    time.Duration
	HTTPClient *http.Client
}

type Client struct {
	botToken   string
	apiURL     string
	httpClient *http.Client
}

type APIError struct {
	Method      string
	Code        int
	Description string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("Telegram %s failed (%d): %s", e.Method, e.Code, e.Description)
}

type Update struct {
	UpdateID int64    `json:"update_id"`
	Message  *Message `json:"message,omitempty"`
}

type Message struct {
	MessageID int64  `json:"message_id"`
	From      *User  `json:"from,omitempty"`
	Chat      Chat   `json:"chat"`
	Text      string `json:"text,omitempty"`
}

type User struct {
	ID        int64  `json:"id"`
	IsBot     bool   `json:"is_bot"`
	FirstName string `json:"first_name"`
	Username  string `json:"username,omitempty"`
}

type Chat struct {
	ID   int64  `json:"id"`
	Type string `json:"type"`
}

func NewClient(config ClientConfig) (*Client, error) {
	botToken := strings.TrimSpace(config.BotToken)
	if botToken == "" {
		return nil, errors.New("Telegram bot token is required")
	}
	if strings.ContainsAny(botToken, "/?# \t\r\n") {
		return nil, errors.New("Telegram bot token contains invalid characters")
	}

	apiURL := strings.TrimRight(strings.TrimSpace(config.APIURL), "/")
	if apiURL == "" {
		apiURL = defaultAPIURL
	}
	parsed, err := url.Parse(apiURL)
	if err != nil {
		return nil, fmt.Errorf("parse Telegram API URL: %w", err)
	}
	if (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		return nil, errors.New("Telegram API URL must be an absolute HTTP(S) URL")
	}
	if parsed.RawQuery != "" || parsed.Fragment != "" {
		return nil, errors.New("Telegram API URL cannot contain a query or fragment")
	}

	httpClient := config.HTTPClient
	if httpClient == nil {
		timeout := config.Timeout
		if timeout == 0 {
			timeout = defaultHTTPTimeout
		}
		if timeout < 0 {
			return nil, errors.New("Telegram HTTP timeout cannot be negative")
		}
		httpClient = &http.Client{Timeout: timeout}
	}
	return &Client{botToken: botToken, apiURL: apiURL, httpClient: httpClient}, nil
}

func (c *Client) GetUpdates(ctx context.Context, offset int64, timeoutSeconds int) ([]Update, error) {
	request := struct {
		Offset         int64    `json:"offset,omitempty"`
		Limit          int      `json:"limit"`
		Timeout        int      `json:"timeout"`
		AllowedUpdates []string `json:"allowed_updates"`
	}{
		Offset: offset, Limit: 100, Timeout: timeoutSeconds,
		AllowedUpdates: []string{"message"},
	}
	var updates []Update
	if err := c.call(ctx, "getUpdates", request, &updates); err != nil {
		return nil, err
	}
	return updates, nil
}

func (c *Client) GetMe(ctx context.Context) (User, error) {
	var user User
	if err := c.call(ctx, "getMe", struct{}{}, &user); err != nil {
		return User{}, err
	}
	return user, nil
}

func (c *Client) SendMessage(ctx context.Context, chatID int64, text string) error {
	request := struct {
		ChatID int64  `json:"chat_id"`
		Text   string `json:"text"`
	}{ChatID: chatID, Text: text}
	return c.call(ctx, "sendMessage", request, nil)
}

func (c *Client) SendChatAction(ctx context.Context, chatID int64, action string) error {
	request := struct {
		ChatID int64  `json:"chat_id"`
		Action string `json:"action"`
	}{ChatID: chatID, Action: action}
	return c.call(ctx, "sendChatAction", request, nil)
}

func (c *Client) call(ctx context.Context, method string, payload any, result any) error {
	encoded, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("encode Telegram %s request: %w", method, err)
	}
	endpoint := c.apiURL + "/bot" + url.PathEscape(c.botToken) + "/" + method
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(encoded))
	if err != nil {
		return fmt.Errorf("create Telegram %s request: %w", method, err)
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json")
	request.Header.Set("User-Agent", "roleloom-telegram-bot/1.0")

	response, err := c.httpClient.Do(request)
	if err != nil {
		message := strings.ReplaceAll(err.Error(), c.botToken, "[REDACTED]")
		return fmt.Errorf("send Telegram %s request: %s", method, message)
	}
	defer response.Body.Close()

	body, err := io.ReadAll(io.LimitReader(response.Body, maxResponseBodySize+1))
	if err != nil {
		return fmt.Errorf("read Telegram %s response: %w", method, err)
	}
	if len(body) > maxResponseBodySize {
		return fmt.Errorf("Telegram %s response exceeds %d bytes", method, maxResponseBodySize)
	}
	var envelope struct {
		OK          bool            `json:"ok"`
		Result      json.RawMessage `json:"result"`
		ErrorCode   int             `json:"error_code"`
		Description string          `json:"description"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		return fmt.Errorf("decode Telegram %s response: %w", method, err)
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 || !envelope.OK {
		description := strings.TrimSpace(envelope.Description)
		if description == "" {
			description = http.StatusText(response.StatusCode)
		}
		return &APIError{Method: method, Code: envelope.ErrorCode, Description: description}
	}
	if result != nil {
		if len(envelope.Result) == 0 || string(envelope.Result) == "null" {
			return fmt.Errorf("Telegram %s returned an empty result", method)
		}
		if err := json.Unmarshal(envelope.Result, result); err != nil {
			return fmt.Errorf("decode Telegram %s result: %w", method, err)
		}
	}
	return nil
}
