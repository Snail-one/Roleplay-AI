package security_test

import (
	"bytes"
	"encoding/base64"
	"os"
	"path/filepath"
	"testing"

	"roleloom/internal/security"
)

func TestEncryptionAndPassword(t *testing.T) {
	key := bytes.Repeat([]byte{7}, 32)
	encrypted, err := security.Encrypt(key, []byte("secret"))
	if err != nil {
		t.Fatal(err)
	}
	plain, err := security.Decrypt(key, encrypted)
	if err != nil || string(plain) != "secret" {
		t.Fatalf("plain=%q err=%v", plain, err)
	}
	if _, err = security.Decrypt(bytes.Repeat([]byte{8}, 32), encrypted); err == nil {
		t.Fatal("wrong key accepted")
	}
	hash, err := security.HashPassword("correct horse battery")
	if err != nil {
		t.Fatal(err)
	}
	if !security.VerifyPassword(hash, "correct horse battery") || security.VerifyPassword(hash, "wrong password value") {
		t.Fatal("password verification failed")
	}
	encoded := base64.StdEncoding.EncodeToString(key)
	parsed, err := security.ParseMasterKey(encoded)
	if err != nil || !bytes.Equal(parsed, key) {
		t.Fatal(err)
	}
}

func TestMasterKeyFileLifecycle(t *testing.T) {
	path := filepath.Join(t.TempDir(), "secrets", "master.key")
	created, wasCreated, err := security.LoadOrCreateMasterKey(path, true)
	if err != nil || !wasCreated || len(created) != 32 {
		t.Fatalf("created=%v len=%d err=%v", wasCreated, len(created), err)
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("mode=%v err=%v", info.Mode().Perm(), err)
	}
	loaded, wasCreated, err := security.LoadOrCreateMasterKey(path, false)
	if err != nil || wasCreated || !bytes.Equal(loaded, created) {
		t.Fatalf("reload created=%v equal=%v err=%v", wasCreated, bytes.Equal(loaded, created), err)
	}
	if err = os.Chmod(path, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, err = security.LoadOrCreateMasterKey(path, false); err == nil {
		t.Fatal("insecure permissions accepted")
	}
	missing := filepath.Join(t.TempDir(), "missing.key")
	if _, _, err = security.LoadOrCreateMasterKey(missing, false); err == nil {
		t.Fatal("missing protected key was regenerated")
	}
}
