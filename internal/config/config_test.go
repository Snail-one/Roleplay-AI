package config_test

import (
	"os"
	"path/filepath"
	"roleloom/internal/config"
	"testing"
)

func TestLoadOrCreate(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	c, created, err := config.LoadOrCreate(path)
	if err != nil || !created {
		t.Fatalf("created=%v err=%v", created, err)
	}
	if c.Server.DatabasePath != "data/roleloom.db" {
		t.Fatal(c.Server.DatabasePath)
	}
	info, _ := os.Stat(path)
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("mode=%o", info.Mode().Perm())
	}
	loaded, err := config.Load(path)
	if err != nil || loaded.Server.Address != "127.0.0.1:8080" {
		t.Fatalf("%#v %v", loaded, err)
	}
}
func TestRejectsLegacyConfig(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	_ = os.WriteFile(path, []byte(`{"api":{}}`), 0o600)
	if _, err := config.Load(path); err == nil {
		t.Fatal("expected legacy config rejection")
	}
}
