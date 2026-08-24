package migration

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"regexp"
)

var digestPattern = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)

// Digest is the ADR-0009 sha256:<64-lowercase-hex> profile.
type Digest string

func ParseDigest(value string) (Digest, error) {
	if !digestPattern.MatchString(value) {
		return "", fail(CodeInvalidDigest, "parse", "digest must use the sha256 lowercase-hex profile", nil)
	}
	return Digest(value), nil
}

func DigestBytes(value []byte) Digest {
	sum := sha256.Sum256(value)
	return Digest("sha256:" + hex.EncodeToString(sum[:]))
}

func (d Digest) Validate() error {
	_, err := ParseDigest(string(d))
	return err
}

func (d Digest) Hex() string {
	if len(d) != len("sha256:")+64 {
		return ""
	}
	return string(d[len("sha256:"):])
}

func (d Digest) String() string { return string(d) }

func requireDigest(field string, d Digest) error {
	if err := d.Validate(); err != nil {
		return fail(CodeInvalidManifest, field, fmt.Sprintf("invalid digest %q", d), err)
	}
	return nil
}
