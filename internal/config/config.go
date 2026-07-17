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
	API   APIConfig   `json:"api"`
	Agent AgentConfig `json:"agent"`
}

type APIConfig struct {
	Provider        string `json:"provider"`
	Protocol        string `json:"protocol"`
	BaseURL         string `json:"base_url"`
	APIKey          string `json:"api_key"`
	Model           string `json:"model"`
	TimeoutSeconds  int    `json:"timeout_seconds"`
	MaxOutputTokens int    `json:"max_output_tokens"`
}

type AgentConfig struct {
	SystemPrompt  string `json:"system_prompt"`
	MaxIterations int    `json:"max_iterations"`
}

func Default() Config {
	return Config{
		API: APIConfig{
			Provider:        "mimo",
			Protocol:        "chat_completions",
			Model:           "mimo-v2.5-pro",
			TimeoutSeconds:  60,
			MaxOutputTokens: 4096,
		},
		Agent: AgentConfig{
			SystemPrompt:  DefaultSystemPrompt,
			MaxIterations: 8,
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
	c.API.Protocol = strings.ToLower(strings.TrimSpace(c.API.Protocol))
	c.API.BaseURL = strings.TrimSpace(c.API.BaseURL)
	c.API.APIKey = strings.TrimSpace(c.API.APIKey)
	c.API.Model = strings.TrimSpace(c.API.Model)
	c.Agent.SystemPrompt = strings.TrimSpace(c.Agent.SystemPrompt)
	if c.API.Provider == "" {
		c.API.Provider = "openai"
	}
	if c.API.Provider == "anthropic" {
		c.API.Provider = "claude"
	}

	if c.API.Model == "" {
		return errors.New("api.model is required")
	}
	switch c.API.Provider {
	case "openai", "openai_compatible", "deepseek", "claude", "mimo":
	default:
		return fmt.Errorf("unsupported api.provider %q", c.API.Provider)
	}
	if c.API.Provider == "mimo" {
		if c.API.Protocol == "" {
			c.API.Protocol = "chat_completions"
		}
		switch c.API.Protocol {
		case "chat_completions", "responses", "anthropic":
		default:
			return fmt.Errorf("unsupported MiMo api.protocol %q", c.API.Protocol)
		}
	} else if c.API.Protocol != "" {
		return errors.New("api.protocol is only supported when api.provider is mimo")
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
	return nil
}
