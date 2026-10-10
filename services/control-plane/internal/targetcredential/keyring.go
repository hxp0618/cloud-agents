// Package targetcredential seals deployment target connection credentials
// that administrators submit once and the Control Plane stores in Postgres.
package targetcredential

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
)

const (
	keyBytes        = 32
	maxSealedBytes  = 256 << 10
	minSealedBytes  = 12 + 16 + 1
	sealContext     = "cloud-agents-target-credential/v1\x00"
	keyIDContext    = "cloud-agents-target-credential-key-id/v1"
	sealKeyContext  = "cloud-agents-target-credential-seal/v1"
	fingerprintMark = "cloud-agents-target-credential-fingerprint/v1"
)

var (
	ErrInvalidKey      = errors.New("deployment target credential key is invalid")
	ErrKeyMismatch     = errors.New("deployment target credential was sealed with another key")
	ErrInvalidSealed   = errors.New("deployment target credential cannot be opened")
	ErrInvalidArgument = errors.New("deployment target credential input is invalid")
)

// Keyring holds one 32-byte root key. The AES-GCM key, the request
// fingerprint key and the public key identifier are derived from it so the
// root key itself never encrypts data or leaves the process.
type Keyring struct {
	aead           cipher.AEAD
	fingerprintKey []byte
	keyID          string
}

func New(key []byte) (*Keyring, error) {
	if len(key) != keyBytes {
		return nil, ErrInvalidKey
	}
	block, err := aes.NewCipher(derive(key, sealKeyContext))
	if err != nil {
		return nil, ErrInvalidKey
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, ErrInvalidKey
	}
	identity := derive(key, keyIDContext)
	return &Keyring{aead: aead, fingerprintKey: derive(key, fingerprintMark), keyID: "k1-" + hex.EncodeToString(identity[:16])}, nil
}

// Load reads an owner-only regular key file containing exactly 32 bytes.
func Load(path string) (*Keyring, error) {
	clean := filepath.Clean(path)
	info, err := os.Lstat(clean)
	if err != nil || !filepath.IsAbs(clean) || info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() || info.Mode().Perm()&0o077 != 0 {
		return nil, ErrInvalidKey
	}
	file, err := os.Open(clean)
	if err != nil {
		return nil, ErrInvalidKey
	}
	defer file.Close()
	key, err := io.ReadAll(io.LimitReader(file, keyBytes+1))
	if err != nil {
		return nil, ErrInvalidKey
	}
	return New(key)
}

func (keyring *Keyring) KeyID() string {
	if keyring == nil {
		return ""
	}
	return keyring.keyID
}

// Fingerprint identifies credential bytes inside request digests without
// letting anyone who reads the digest test guesses offline.
func (keyring *Keyring) Fingerprint(plaintext []byte) (string, error) {
	if keyring == nil || len(plaintext) == 0 {
		return "", ErrInvalidArgument
	}
	mac := hmac.New(sha256.New, keyring.fingerprintKey)
	_, _ = mac.Write(plaintext)
	return "hmac-sha256:" + hex.EncodeToString(mac.Sum(nil)), nil
}

// Seal binds the ciphertext to the owning target so a stored row copied to
// another tenant, project or target cannot be opened there.
func (keyring *Keyring) Seal(tenantID, projectID, targetID string, plaintext []byte) (string, []byte, error) {
	if keyring == nil || len(plaintext) == 0 || len(plaintext) > maxSealedBytes-minSealedBytes {
		return "", nil, ErrInvalidArgument
	}
	nonce := make([]byte, keyring.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", nil, err
	}
	return keyring.keyID, keyring.aead.Seal(nonce, nonce, plaintext, associatedData(tenantID, projectID, targetID)), nil
}

func (keyring *Keyring) Open(tenantID, projectID, targetID, keyID string, sealed []byte) ([]byte, error) {
	if keyring == nil {
		return nil, ErrInvalidKey
	}
	if keyID != keyring.keyID {
		return nil, ErrKeyMismatch
	}
	if len(sealed) < minSealedBytes || len(sealed) > maxSealedBytes {
		return nil, ErrInvalidSealed
	}
	nonceSize := keyring.aead.NonceSize()
	plaintext, err := keyring.aead.Open(nil, sealed[:nonceSize], sealed[nonceSize:], associatedData(tenantID, projectID, targetID))
	if err != nil {
		return nil, ErrInvalidSealed
	}
	return plaintext, nil
}

func associatedData(tenantID, projectID, targetID string) []byte {
	return []byte(sealContext + tenantID + "\x00" + projectID + "\x00" + targetID)
}

func derive(key []byte, label string) []byte {
	mac := hmac.New(sha256.New, key)
	_, _ = mac.Write([]byte(label))
	return mac.Sum(nil)
}
