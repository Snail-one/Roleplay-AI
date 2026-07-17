package security_test

import (
	"bytes"
	"encoding/base64"
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
