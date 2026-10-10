package browserauth_test

import (
	"crypto/sha256"
	"encoding/base64"
	"strings"
	"testing"

	"github.com/hxp0618/cloud-agents/services/control-plane/internal/browserauth"
)

func TestProofRoundTripAndCanonicalDigest(t *testing.T) {
	t.Parallel()
	raw, digest, err := browserauth.NewProof()
	if err != nil {
		t.Fatalf("proof generation failed: %v", err)
	}
	if len(raw) != base64.RawURLEncoding.EncodedLen(32) {
		t.Fatal("proof used an unexpected encoded length")
	}
	decoded, err := base64.RawURLEncoding.Strict().DecodeString(raw)
	if err != nil || len(decoded) != 32 || base64.RawURLEncoding.EncodeToString(decoded) != raw {
		t.Fatal("proof was not canonical raw URL base64")
	}
	want := sha256.Sum256([]byte(raw))
	if digest != want {
		t.Fatal("proof digest did not bind the canonical representation")
	}
	got, err := browserauth.ProofDigest(raw)
	if err != nil || got != digest {
		t.Fatal("proof digest did not round-trip")
	}

	other, _, err := browserauth.NewProof()
	if err != nil {
		t.Fatalf("second proof generation failed: %v", err)
	}
	if raw == other {
		t.Fatal("proof generation reused random bytes")
	}
}

func TestProofTamperAndBounds(t *testing.T) {
	t.Parallel()
	raw, expected, err := browserauth.NewProof()
	if err != nil {
		t.Fatalf("proof generation failed: %v", err)
	}
	tampered := []byte(raw)
	if tampered[0] == 'A' {
		tampered[0] = 'B'
	} else {
		tampered[0] = 'A'
	}
	got, err := browserauth.ProofDigest(string(tampered))
	if err != nil || got == expected {
		t.Fatal("tampered proof was accepted as its original digest")
	}

	invalid := []string{
		"",
		raw[:42],
		raw + "A",
		raw + "=",
		"+" + raw[1:],
		string([]byte{0xff}),
	}
	for _, candidate := range invalid {
		if _, err := browserauth.ProofDigest(candidate); err == nil {
			t.Fatal("invalid proof was accepted")
		}
	}
	trailingBits := raw[:42] + "B"
	if _, err := browserauth.ProofDigest(trailingBits); err == nil {
		t.Fatal("proof with non-zero trailing base64 bits was accepted")
	}
	marker := strings.Repeat("secret", 4)
	if _, err := browserauth.ProofDigest(marker); err == nil || strings.Contains(err.Error(), marker) {
		t.Fatal("proof error exposed input")
	}
}
