package accessgrant

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

var ErrInvalidKey = errors.New("sandbox access grant key is invalid")

var identifier = regexp.MustCompile(`^[A-Za-z0-9](?:[A-Za-z0-9._~-]{0,126}[A-Za-z0-9])?$`)

type Codec struct{ key []byte }

func New(key []byte) (*Codec, error) {
	if len(key) < 32 || len(key) > 64 {
		return nil, ErrInvalidKey
	}
	return &Codec{key: append([]byte(nil), key...)}, nil
}

func Load(path string) (*Codec, error) {
	clean := filepath.Clean(path)
	info, err := os.Lstat(clean)
	if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() || info.Mode().Perm()&0o077 != 0 {
		return nil, ErrInvalidKey
	}
	file, err := os.Open(clean)
	if err != nil {
		return nil, ErrInvalidKey
	}
	defer file.Close()
	key, err := io.ReadAll(io.LimitReader(file, 65))
	if err != nil || len(key) > 64 {
		return nil, ErrInvalidKey
	}
	return New(key)
}

func GrantID(tenant, project, sandbox, idempotencyKey string) string {
	sum := sha256.Sum256([]byte(tenant + "\x00" + project + "\x00" + sandbox + "\x00" + idempotencyKey))
	return "grant-" + hex.EncodeToString(sum[:16])
}

func (codec *Codec) Token(grantID string) (string, error) {
	if codec == nil || len(codec.key) < 32 || grantID == "" {
		return "", ErrInvalidKey
	}
	mac := hmac.New(sha256.New, codec.key)
	_, _ = mac.Write([]byte("cloud-agents-sandbox-access-grant/v1\x00" + grantID))
	return "cag1_" + base64.RawURLEncoding.EncodeToString(mac.Sum(nil)), nil
}

func Digest(token string) string {
	sum := sha256.Sum256([]byte(token))
	return "sha256:" + hex.EncodeToString(sum[:])
}

func SSHUsername(tenant, project, grant string) (string, error) {
	if !identifier.MatchString(tenant) || !identifier.MatchString(project) || !identifier.MatchString(grant) {
		return "", ErrInvalidKey
	}
	return tenant + ":" + project + ":" + grant, nil
}

func ParseSSHUsername(value string) (tenant, project, grant string, ok bool) {
	parts := strings.Split(value, ":")
	if len(parts) != 3 || !identifier.MatchString(parts[0]) || !identifier.MatchString(parts[1]) || !identifier.MatchString(parts[2]) {
		return "", "", "", false
	}
	return parts[0], parts[1], parts[2], true
}
