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

const maxConfigSize = 1 << 20

type Config struct {
	Server ServerConfig `json:"server"`
	Log    LogConfig    `json:"log"`
}

type ServerConfig struct {
	Address      string `json:"address"`
	DatabasePath string `json:"database_path"`
	StaticDir    string `json:"static_dir"`
	SecureCookie bool   `json:"secure_cookie"`
}

type LogConfig struct {
	Level string `json:"level"`
}

func Default() Config {
	return Config{Server: ServerConfig{Address: "127.0.0.1:8080", DatabasePath: "data/roleloom.db", StaticDir: "web/dist"}, Log: LogConfig{Level: "info"}}
}

func LoadOrCreate(path string) (Config, bool, error) {
	c, err := Load(path)
	if err == nil {
		return c, false, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return Config{}, false, err
	}
	c = Default()
	b, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return Config{}, false, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return Config{}, false, fmt.Errorf("create config directory: %w", err)
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if errors.Is(err, os.ErrExist) {
		loaded, loadErr := Load(path)
		return loaded, false, loadErr
	}
	if err != nil {
		return Config{}, false, fmt.Errorf("create config: %w", err)
	}
	if _, err = f.Write(append(b, '\n')); err != nil {
		_ = f.Close()
		_ = os.Remove(path)
		return Config{}, false, err
	}
	if err = f.Close(); err != nil {
		return Config{}, false, err
	}
	return c, true, nil
}

func Load(path string) (Config, error) {
	f, err := os.Open(path)
	if err != nil {
		return Config{}, fmt.Errorf("open config file %q: %w", path, err)
	}
	defer f.Close()
	dec := json.NewDecoder(io.LimitReader(f, maxConfigSize+1))
	dec.DisallowUnknownFields()
	var c Config
	if err := dec.Decode(&c); err != nil {
		return Config{}, fmt.Errorf("decode config file %q: %w", path, err)
	}
	if err := dec.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return Config{}, fmt.Errorf("decode config file %q: multiple JSON values are not allowed", path)
	}
	if err := c.Validate(); err != nil {
		return Config{}, fmt.Errorf("validate config file %q: %w", path, err)
	}
	return c, nil
}

func (c *Config) Validate() error {
	c.Server.Address = strings.TrimSpace(c.Server.Address)
	c.Server.DatabasePath = strings.TrimSpace(c.Server.DatabasePath)
	c.Server.StaticDir = strings.TrimSpace(c.Server.StaticDir)
	c.Log.Level = strings.ToLower(strings.TrimSpace(c.Log.Level))
	if c.Server.Address == "" {
		c.Server.Address = "127.0.0.1:8080"
	}
	if c.Server.DatabasePath == "" {
		c.Server.DatabasePath = "data/roleloom.db"
	}
	if c.Server.StaticDir == "" {
		c.Server.StaticDir = "web/dist"
	}
	if c.Log.Level == "" {
		c.Log.Level = "info"
	}
	switch c.Log.Level {
	case "debug", "info", "warn", "error":
	default:
		return fmt.Errorf("unsupported log.level %q", c.Log.Level)
	}
	return nil
}
