package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

const (
	maxConfigSize       = 1 << 20
	DefaultSystemPrompt = "你是一个简洁、可靠的 AI 助手。需要准确时间或数学计算时，应使用可用工具。"
)

type Config struct {
	API      APIConfig      `json:"api"`
	Agent    AgentConfig    `json:"agent"`
	Telegram TelegramConfig `json:"telegram"`
	Server   ServerConfig   `json:"server"`
}

type APIConfig struct {
	Provider        string `json:"provider"`
	APIURL          string `json:"api_url"`
	APIKey          string `json:"api_key"`
	Model           string `json:"model"`
	TimeoutSeconds  int    `json:"timeout_seconds"`
	MaxOutputTokens int    `json:"max_output_tokens"`
}

type AgentConfig struct {
	SystemPrompt  string `json:"system_prompt"`
	MaxIterations int    `json:"max_iterations"`
}

type TelegramConfig struct {
	BotToken           string  `json:"bot_token"`
	AllowedUserIDs     []int64 `json:"allowed_user_ids"`
	PollTimeoutSeconds int     `json:"poll_timeout_seconds"`
}

type ServerConfig struct {
	Address           string   `json:"address"`
	StaticDir         string   `json:"static_dir"`
	AllowedOrigins    []string `json:"allowed_origins"`
	SessionTTLMinutes int      `json:"session_ttl_minutes"`
}

func Default() Config {
	return Config{
		API: APIConfig{
			Provider:        "openai_compatible",
			APIURL:          "https://your-api-host/v1/chat/completions",
			APIKey:          "your-api-key",
			Model:           "your-model",
			TimeoutSeconds:  60,
			MaxOutputTokens: 4096,
		},
		Agent: AgentConfig{
			SystemPrompt:  DefaultSystemPrompt,
			MaxIterations: 8,
		},
		Telegram: TelegramConfig{
			BotToken:           "your-telegram-bot-token",
			AllowedUserIDs:     []int64{},
			PollTimeoutSeconds: 30,
		},
		Server: ServerConfig{
			Address:           "127.0.0.1:8080",
			StaticDir:         "web/dist",
			AllowedOrigins:    []string{"http://localhost:5173"},
			SessionTTLMinutes: 120,
		},
	}
}

// LoadOrCreate loads path, or creates a secure default configuration when it
// does not exist. Existing files are never overwritten.
func LoadOrCreate(path string) (Config, bool, error) {
	config, err := Load(path)
	if err == nil {
		return config, false, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return Config{}, false, err
	}

	config = Default()
	encoded, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return Config{}, false, fmt.Errorf("encode default config: %w", err)
	}
	encoded = append(encoded, '\n')

	directory := filepath.Dir(path)
	if err := os.MkdirAll(directory, 0o755); err != nil {
		return Config{}, false, fmt.Errorf("create config directory %q: %w", directory, err)
	}
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if errors.Is(err, os.ErrExist) {
		loaded, loadErr := Load(path)
		return loaded, false, loadErr
	}
	if err != nil {
		return Config{}, false, fmt.Errorf("create config file %q: %w", path, err)
	}
	if _, err := file.Write(encoded); err != nil {
		_ = file.Close()
		_ = os.Remove(path)
		return Config{}, false, fmt.Errorf("write config file %q: %w", path, err)
	}
	if err := file.Close(); err != nil {
		return Config{}, false, fmt.Errorf("close config file %q: %w", path, err)
	}
	return config, true, nil
}

func Load(path string) (Config, error) {
	file, err := os.Open(path)
	if err != nil {
		return Config{}, fmt.Errorf("open config file %q: %w", path, err)
	}
	defer file.Close()

	reader := io.LimitReader(file, maxConfigSize+1)
	decoder := json.NewDecoder(reader)
	decoder.DisallowUnknownFields()

	var config Config
	if err := decoder.Decode(&config); err != nil {
		return Config{}, fmt.Errorf("decode config file %q: %w", path, err)
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		if err == nil {
			return Config{}, fmt.Errorf("decode config file %q: multiple JSON values are not allowed", path)
		}
		return Config{}, fmt.Errorf("decode config file %q: %w", path, err)
	}

	if err := config.Validate(); err != nil {
		return Config{}, fmt.Errorf("validate config file %q: %w", path, err)
	}
	return config, nil
}

func (c *Config) Validate() error {
	c.API.Provider = strings.ToLower(strings.TrimSpace(c.API.Provider))
	c.API.APIURL = strings.TrimSpace(c.API.APIURL)
	c.API.APIKey = strings.TrimSpace(c.API.APIKey)
	c.API.Model = strings.TrimSpace(c.API.Model)
	c.Agent.SystemPrompt = strings.TrimSpace(c.Agent.SystemPrompt)
	c.Telegram.BotToken = strings.TrimSpace(c.Telegram.BotToken)
	c.Server.Address = strings.TrimSpace(c.Server.Address)
	c.Server.StaticDir = strings.TrimSpace(c.Server.StaticDir)
	if c.API.Provider == "" {
		c.API.Provider = "openai"
	}
	if c.API.Provider == "anthropic" {
		c.API.Provider = "claude"
	}
	if c.Server.Address == "" {
		c.Server.Address = "127.0.0.1:8080"
	}
	if c.Server.StaticDir == "" {
		c.Server.StaticDir = "web/dist"
	}
	if c.Server.SessionTTLMinutes == 0 {
		c.Server.SessionTTLMinutes = 120
	}
	for index, origin := range c.Server.AllowedOrigins {
		c.Server.AllowedOrigins[index] = strings.TrimRight(strings.TrimSpace(origin), "/")
	}

	if c.API.Model == "" {
		return errors.New("api.model is required")
	}
	if c.API.APIURL == "" {
		return errors.New("api.api_url is required")
	}
	switch c.API.Provider {
	case "openai", "openai_compatible", "deepseek", "claude", "mimo":
	default:
		return fmt.Errorf("unsupported api.provider %q", c.API.Provider)
	}
	if c.API.TimeoutSeconds < 0 {
		return errors.New("api.timeout_seconds cannot be negative")
	}
	if c.API.MaxOutputTokens < 0 {
		return errors.New("api.max_output_tokens cannot be negative")
	}
	if c.Agent.MaxIterations < 0 {
		return errors.New("agent.max_iterations cannot be negative")
	}
	if c.Telegram.PollTimeoutSeconds < 0 {
		return errors.New("telegram.poll_timeout_seconds cannot be negative")
	}
	if c.Server.SessionTTLMinutes < 1 {
		return errors.New("server.session_ttl_minutes must be positive")
	}
	return nil
}
