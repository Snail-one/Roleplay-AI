package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"path/filepath"
	"testing"

	"roleloom/internal/store"
)

func TestAdminPasswordInitializesOnceAndEnvironmentRotates(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	prompts := 0
	changed, err := ensureAdminPassword(ctx, st, "", func() (string, error) { prompts++; return "first secure password", nil })
	if err != nil || !changed || prompts != 1 {
		t.Fatalf("changed=%v prompts=%d err=%v", changed, prompts, err)
	}
	changed, err = ensureAdminPassword(ctx, st, "", func() (string, error) { t.Fatal("prompt called after initialization"); return "", nil })
	if err != nil || changed {
		t.Fatalf("changed=%v err=%v", changed, err)
	}
	changed, err = ensureAdminPassword(ctx, st, "second secure password", nil)
	if err != nil || !changed {
		t.Fatalf("rotation changed=%v err=%v", changed, err)
	}
	ok, err := st.VerifyAdminPassword(ctx, "second secure password")
	if err != nil || !ok {
		t.Fatalf("rotated password invalid: %v", err)
	}
}

func TestResolveMasterKey(t *testing.T) {
	path := filepath.Join(t.TempDir(), "master.key")
	key, created, err := resolveMasterKey(path, "", false)
	if err != nil || !created || len(key) != 32 {
		t.Fatalf("created=%v len=%d err=%v", created, len(key), err)
	}
	again, created, err := resolveMasterKey(path, "", true)
	if err != nil || created || !bytes.Equal(key, again) {
		t.Fatalf("reload created=%v equal=%v err=%v", created, bytes.Equal(key, again), err)
	}
	missing := filepath.Join(t.TempDir(), "missing.key")
	if _, _, err = resolveMasterKey(missing, "", true); err == nil {
		t.Fatal("regenerated a missing key for encrypted profiles")
	}
	environment := bytes.Repeat([]byte{9}, 32)
	parsed, created, err := resolveMasterKey(missing, base64.StdEncoding.EncodeToString(environment), true)
	if err != nil || created || !bytes.Equal(parsed, environment) {
		t.Fatal("environment override failed")
	}
}
