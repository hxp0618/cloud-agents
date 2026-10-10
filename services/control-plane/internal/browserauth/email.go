package browserauth

import (
	"errors"
	"net/mail"
	"strings"
	"unicode"
	"unicode/utf8"

	"golang.org/x/net/idna"
)

var errInvalidEmailDomain = errors.New("invalid email domain")

func NormalizeEmailDomain(email string) (string, error) {
	if !validEmailAddress(email) {
		return "", errInvalidEmailDomain
	}
	return normalizeEmailDomain(email[strings.LastIndexByte(email, '@')+1:])
}

// NormalizeEmailPolicyDomain returns the canonical exact domain stored in a
// tenant email policy. It accepts a domain, not a mailbox or wildcard suffix.
func NormalizeEmailPolicyDomain(domain string) (string, error) {
	if strings.ContainsRune(domain, '@') {
		return "", errInvalidEmailDomain
	}
	return normalizeEmailDomain(domain)
}

func EmailDomainAllowed(email string, allowed []string) bool {
	domain, err := NormalizeEmailDomain(email)
	if err != nil {
		return false
	}
	if len(allowed) == 0 {
		return true
	}
	// Canonical duplicates collapse to one policy entry.
	canonicalAllowed := make(map[string]struct{}, len(allowed))
	for _, candidate := range allowed {
		canonical, err := NormalizeEmailPolicyDomain(candidate)
		if err != nil {
			return false
		}
		canonicalAllowed[canonical] = struct{}{}
	}
	_, ok := canonicalAllowed[domain]
	return ok
}

func validEmailAddress(email string) bool {
	if email == "" || len(email) > 254 || !utf8.ValidString(email) {
		return false
	}
	for _, character := range email {
		if unicode.IsSpace(character) || unicode.IsControl(character) {
			return false
		}
	}
	address, err := mail.ParseAddress(email)
	if err != nil || address.Address != email || address.Name != "" {
		return false
	}
	at := strings.LastIndexByte(email, '@')
	if at < 1 || at == len(email)-1 || len(email[:at]) > 64 {
		return false
	}
	return true
}

func normalizeEmailDomain(domain string) (string, error) {
	if domain == "" || len(domain) > 253 || !utf8.ValidString(domain) {
		return "", errInvalidEmailDomain
	}
	for _, character := range domain {
		if unicode.IsSpace(character) || unicode.IsControl(character) {
			return "", errInvalidEmailDomain
		}
	}
	canonical, err := idna.Lookup.ToASCII(domain)
	if err != nil || canonical == "" || len(canonical) > 253 || strings.HasPrefix(canonical, ".") || strings.HasSuffix(canonical, ".") || strings.Contains(canonical, "..") {
		return "", errInvalidEmailDomain
	}
	canonical = strings.ToLower(canonical)
	for _, label := range strings.Split(canonical, ".") {
		if len(label) == 0 || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return "", errInvalidEmailDomain
		}
		for _, character := range label {
			if character >= 'a' && character <= 'z' || character >= '0' && character <= '9' || character == '-' {
				continue
			}
			return "", errInvalidEmailDomain
		}
	}
	return canonical, nil
}
