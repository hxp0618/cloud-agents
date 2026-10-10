package browserauth_test

import (
	"encoding/base64"
	"strings"
	"testing"

	"github.com/hxp0618/cloud-agents/services/control-plane/internal/browserauth"
)

func TestPasswordRoundTripAndDistinctSalts(t *testing.T) {
	t.Parallel()
	passwords := []string{
		strings.Repeat("a", 15),
		strings.Repeat("界", 15),
		strings.Repeat("密码", 64),
		strings.Repeat("z", 128),
	}
	for _, password := range passwords {
		hash, err := browserauth.HashPassword(password)
		if err != nil {
			t.Fatalf("valid password was rejected: %v", err)
		}
		if !browserauth.VerifyPassword(hash, password) {
			t.Fatal("valid password did not verify")
		}
		if browserauth.VerifyPassword(hash, password+"x") {
			t.Fatal("wrong password verified")
		}
	}

	first, err := browserauth.HashPassword(strings.Repeat("a", 15))
	if err != nil {
		t.Fatalf("first valid password was rejected: %v", err)
	}
	second, err := browserauth.HashPassword(strings.Repeat("a", 15))
	if err != nil {
		t.Fatalf("second valid password was rejected: %v", err)
	}
	if first == second {
		t.Fatal("password hashes reused the same salt")
	}
}

func TestPasswordPolicyDoesNotTrimOrNormalize(t *testing.T) {
	t.Parallel()
	password := strings.Repeat("a", 15)
	hash, err := browserauth.HashPassword(password)
	if err != nil {
		t.Fatalf("valid password was rejected: %v", err)
	}
	for _, candidate := range []string{" " + password, password + " ", "Ａ" + strings.Repeat("a", 14), strings.Repeat("a", 14) + "Ａ"} {
		if browserauth.VerifyPassword(hash, candidate) {
			t.Fatal("trimmed or normalized password verified")
		}
	}
}

func TestPasswordPolicyBoundsAndUTF8(t *testing.T) {
	t.Parallel()
	invalid := []string{
		strings.Repeat("a", 14),
		strings.Repeat("界", 129),
		string([]byte{0xff}) + strings.Repeat("a", 14),
	}
	for _, password := range invalid {
		if _, err := browserauth.HashPassword(password); err == nil {
			t.Fatal("invalid password was accepted")
		}
		if browserauth.VerifyPassword("", password) {
			t.Fatal("invalid password verified")
		}
	}
	marker := strings.Repeat("secret", 2)
	if _, err := browserauth.HashPassword(marker); err == nil || strings.Contains(err.Error(), marker) {
		t.Fatal("password error exposed input")
	}
}

func TestPasswordPHCProfileAndCanonicalParsing(t *testing.T) {
	t.Parallel()
	password := strings.Repeat("a", 15)
	encoded, err := browserauth.HashPassword(password)
	if err != nil {
		t.Fatalf("valid password was rejected: %v", err)
	}
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 || parts[0] != "" || parts[1] != "argon2id" || parts[2] != "v=19" || parts[3] != "m=19456,t=2,p=1" {
		t.Fatal("password hash did not use the required PHC profile")
	}
	if len(parts[4]) != base64.RawStdEncoding.EncodedLen(16) || len(parts[5]) != base64.RawStdEncoding.EncodedLen(32) {
		t.Fatal("password hash used an unexpected salt or hash length")
	}
	if _, decodeErr := base64.RawStdEncoding.Strict().DecodeString(parts[4]); decodeErr != nil {
		t.Fatal("password salt was not canonical base64")
	}
	if _, decodeErr := base64.RawStdEncoding.Strict().DecodeString(parts[5]); decodeErr != nil {
		t.Fatal("password hash was not canonical base64")
	}

	mutations := []string{
		encoded + "$trailing",
		strings.Replace(encoded, "v=19", "v=18", 1),
		strings.Replace(encoded, "m=19456,t=2,p=1", "m=1,t=2,p=1", 1),
		strings.Replace(encoded, "m=19456,t=2,p=1", "m=19456,t=3,p=1", 1),
		strings.Replace(encoded, "m=19456,t=2,p=1", "m=19456,t=2,p=2", 1),
		strings.Replace(encoded, "$"+parts[4]+"$", "$"+parts[4]+"=$", 1),
		strings.Replace(encoded, "$"+parts[5], "$"+parts[5]+"A", 1),
		strings.Replace(encoded, parts[4], parts[4][:len(parts[4])-1]+"B", 1),
		strings.Replace(encoded, parts[5], parts[5][:len(parts[5])-1]+"B", 1),
	}
	for _, mutation := range mutations {
		if browserauth.VerifyPassword(mutation, password) {
			t.Fatal("non-canonical password hash was accepted")
		}
	}
}
