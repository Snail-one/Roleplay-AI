package security

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/crypto/argon2"
)

const passwordPrefix = "$argon2id$v=19$m=65536,t=3,p=4$"

func ParseMasterKey(value string) ([]byte, error) {
	b, err := base64.StdEncoding.DecodeString(strings.TrimSpace(value))
	if err != nil || len(b) != 32 {
		return nil, errors.New("ROLELOOM_MASTER_KEY must be Base64-encoded 32 bytes")
	}
	return b, nil
}

// LoadOrCreateMasterKey loads a Base64 master key from path. Creation is
// refused when allowCreate is false so a missing backup can never silently
// replace a key that protects existing encrypted data.
func LoadOrCreateMasterKey(path string, allowCreate bool) ([]byte, bool, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil, false, errors.New("master key path is required")
	}
	key, err := loadMasterKeyFile(path)
	if err == nil {
		return key, false, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return nil, false, err
	}
	if !allowCreate {
		return nil, false, fmt.Errorf("master key file %q is missing; restore it from backup", path)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, false, fmt.Errorf("create master key directory: %w", err)
	}
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return nil, false, fmt.Errorf("generate master key: %w", err)
	}
	encoded := append([]byte(base64.StdEncoding.EncodeToString(raw)), '\n')
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if errors.Is(err, os.ErrExist) {
		key, loadErr := loadMasterKeyFile(path)
		return key, false, loadErr
	}
	if err != nil {
		return nil, false, fmt.Errorf("create master key file: %w", err)
	}
	if _, err = file.Write(encoded); err != nil {
		_ = file.Close()
		_ = os.Remove(path)
		return nil, false, fmt.Errorf("write master key file: %w", err)
	}
	if err = file.Close(); err != nil {
		return nil, false, fmt.Errorf("close master key file: %w", err)
	}
	return raw, true, nil
}

func loadMasterKeyFile(path string) ([]byte, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("master key path %q is not a regular file", path)
	}
	if info.Mode().Perm()&0o077 != 0 {
		return nil, fmt.Errorf("master key file %q permissions must be 0600 or stricter", path)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read master key file: %w", err)
	}
	key, err := ParseMasterKey(string(data))
	if err != nil {
		return nil, fmt.Errorf("invalid master key file %q: %w", path, err)
	}
	return key, nil
}

func Encrypt(key, plaintext []byte) ([]byte, error) {
	if len(key) != 32 {
		return nil, errors.New("master key must be 32 bytes")
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	return append(nonce, gcm.Seal(nil, nonce, plaintext, nil)...), nil
}

func Decrypt(key, ciphertext []byte) ([]byte, error) {
	if len(key) != 32 {
		return nil, errors.New("master key must be 32 bytes")
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	if len(ciphertext) < gcm.NonceSize() {
		return nil, errors.New("encrypted API key is invalid")
	}
	plain, err := gcm.Open(nil, ciphertext[:gcm.NonceSize()], ciphertext[gcm.NonceSize():], nil)
	if err != nil {
		return nil, errors.New("decrypt API key: authentication failed")
	}
	return plain, nil
}

func HashPassword(password string) (string, error) {
	if len([]rune(password)) < 12 {
		return "", errors.New("ROLELOOM_ADMIN_PASSWORD must contain at least 12 characters")
	}
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	hash := argon2.IDKey([]byte(password), salt, 3, 64*1024, 4, 32)
	return passwordPrefix + base64.RawStdEncoding.EncodeToString(salt) + "$" + base64.RawStdEncoding.EncodeToString(hash), nil
}

func VerifyPassword(encoded, password string) bool {
	if !strings.HasPrefix(encoded, passwordPrefix) {
		return false
	}
	parts := strings.Split(strings.TrimPrefix(encoded, passwordPrefix), "$")
	if len(parts) != 2 {
		return false
	}
	salt, e1 := base64.RawStdEncoding.DecodeString(parts[0])
	expected, e2 := base64.RawStdEncoding.DecodeString(parts[1])
	if e1 != nil || e2 != nil || len(expected) != 32 {
		return false
	}
	actual := argon2.IDKey([]byte(password), salt, 3, 64*1024, 4, 32)
	return subtle.ConstantTimeCompare(actual, expected) == 1
}

func NewToken() (plain string, hash []byte, err error) {
	b := make([]byte, 32)
	if _, err = rand.Read(b); err != nil {
		return "", nil, err
	}
	plain = base64.RawURLEncoding.EncodeToString(b)
	sum := sha256.Sum256([]byte(plain))
	return plain, sum[:], nil
}

func TokenHash(token string) []byte { sum := sha256.Sum256([]byte(token)); return sum[:] }

func NewID() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return fmt.Sprintf("%x", b), nil
}
