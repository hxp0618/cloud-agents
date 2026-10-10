package browserauth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
)

const proofBytes = 32

var errInvalidProof = errors.New("invalid proof")

func NewProof() (string, [32]byte, error) {
	var rawBytes [proofBytes]byte
	if _, err := rand.Read(rawBytes[:]); err != nil {
		return "", [32]byte{}, errInvalidProof
	}
	raw := base64.RawURLEncoding.EncodeToString(rawBytes[:])
	return raw, sha256.Sum256([]byte(raw)), nil
}

func ProofDigest(raw string) ([32]byte, error) {
	if len(raw) != base64.RawURLEncoding.EncodedLen(proofBytes) {
		return [32]byte{}, errInvalidProof
	}
	decoded, err := base64.RawURLEncoding.Strict().DecodeString(raw)
	if err != nil || len(decoded) != proofBytes || base64.RawURLEncoding.EncodeToString(decoded) != raw {
		return [32]byte{}, errInvalidProof
	}
	return sha256.Sum256([]byte(raw)), nil
}
