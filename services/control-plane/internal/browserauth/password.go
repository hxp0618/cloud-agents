package browserauth

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"strings"
	"unicode/utf8"

	"golang.org/x/crypto/argon2"
)

const (
	minimumPasswordScalars = 15
	maximumPasswordScalars = 128
	passwordSaltBytes      = 16
	passwordHashBytes      = 32
	passwordMemoryKiB      = 19456
	passwordIterations     = 2
	passwordLanes          = 1
	passwordPHCParameters  = "m=19456,t=2,p=1"
	passwordPHCHeader      = "$argon2id$v=19$" + passwordPHCParameters + "$"
)

var errInvalidPassword = errors.New("invalid password")

func HashPassword(password string) (string, error) {
	if !validPassword(password) {
		return "", errInvalidPassword
	}

	salt := make([]byte, passwordSaltBytes)
	if _, err := rand.Read(salt); err != nil {
		return "", errInvalidPassword
	}
	hash := argon2.IDKey([]byte(password), salt, passwordIterations, passwordMemoryKiB, passwordLanes, passwordHashBytes)
	return passwordPHCHeader + base64.RawStdEncoding.EncodeToString(salt) + "$" + base64.RawStdEncoding.EncodeToString(hash), nil
}

func VerifyPassword(encoded, password string) bool {
	if !validPassword(password) {
		return false
	}
	salt, expected, ok := parsePasswordHash(encoded)
	if !ok {
		return false
	}
	hash := argon2.IDKey([]byte(password), salt, passwordIterations, passwordMemoryKiB, passwordLanes, passwordHashBytes)
	return subtle.ConstantTimeCompare(hash, expected) == 1
}

// ValidPasswordHash reports whether encoded is the canonical Argon2id profile
// accepted by password authentication. It does not evaluate a password.
func ValidPasswordHash(encoded string) bool {
	_, _, ok := parsePasswordHash(encoded)
	return ok
}

func validPassword(password string) bool {
	if !utf8.ValidString(password) {
		return false
	}
	count := utf8.RuneCountInString(password)
	return count >= minimumPasswordScalars && count <= maximumPasswordScalars
}

func parsePasswordHash(encoded string) ([]byte, []byte, bool) {
	if len(encoded) > 256 {
		return nil, nil, false
	}
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 || parts[0] != "" || parts[1] != "argon2id" || parts[2] != "v=19" || parts[3] != passwordPHCParameters {
		return nil, nil, false
	}
	salt, ok := decodePHCBase64(parts[4], passwordSaltBytes)
	if !ok {
		return nil, nil, false
	}
	hash, ok := decodePHCBase64(parts[5], passwordHashBytes)
	if !ok {
		return nil, nil, false
	}
	return salt, hash, true
}

func decodePHCBase64(encoded string, expectedBytes int) ([]byte, bool) {
	if len(encoded) != base64.RawStdEncoding.EncodedLen(expectedBytes) {
		return nil, false
	}
	decoded, err := base64.RawStdEncoding.Strict().DecodeString(encoded)
	if err != nil || len(decoded) != expectedBytes || base64.RawStdEncoding.EncodeToString(decoded) != encoded {
		return nil, false
	}
	return decoded, true
}
