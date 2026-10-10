package browserauth_test

import (
	"strings"
	"testing"

	"github.com/hxp0618/cloud-agents/services/control-plane/internal/browserauth"
)

func TestNormalizeEmailDomainCanonicalizesASCIIAndIDNA(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		email string
		want  string
	}{
		{name: "ascii case", email: "User@Example.COM", want: "example.com"},
		{name: "unicode label", email: "user@bücher.example", want: "xn--bcher-kva.example"},
		{name: "uppercase A-label", email: "user@XN--BCHER-KVA.EXAMPLE", want: "xn--bcher-kva.example"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := browserauth.NormalizeEmailDomain(test.email)
			if err != nil || got != test.want {
				t.Fatalf("normalized domain mismatch: err=%v", err)
			}
		})
	}
}

func TestNormalizeEmailPolicyDomainCanonicalizesDomainWithoutMailbox(t *testing.T) {
	t.Parallel()
	for input, want := range map[string]string{
		"Example.COM":    "example.com",
		"bücher.example": "xn--bcher-kva.example",
	} {
		got, err := browserauth.NormalizeEmailPolicyDomain(input)
		if err != nil || got != want {
			t.Fatalf("NormalizeEmailPolicyDomain(%q) = %q, %v", input, got, err)
		}
	}
	for _, input := range []string{"user@example.com", "*.example.com", ".example.com", "example.com."} {
		if _, err := browserauth.NormalizeEmailPolicyDomain(input); err == nil {
			t.Fatalf("invalid policy domain %q was accepted", input)
		}
	}
}

func TestNormalizeEmailDomainRejectsNonMailboxAddresses(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		email string
	}{
		{name: "display name", email: "User Name <user@example.com>"},
		{name: "comment", email: "user(comment)@example.com"},
		{name: "list", email: "user@example.com,other@example.com"},
		{name: "whitespace", email: "user @example.com"},
		{name: "newline", email: "user@example.com\n"},
		{name: "control", email: "user@example.com\x00"},
		{name: "missing localpart", email: "@example.com"},
		{name: "missing domain", email: "user@"},
		{name: "multiple at signs", email: "user@example@example.com"},
		{name: "empty label", email: "user@example..com"},
		{name: "trailing dot", email: "user@example.com."},
		{name: "wildcard", email: "user@*.example.com"},
		{name: "domain literal", email: "user@[127.0.0.1]"},
		{name: "invalid UTF8", email: string([]byte("user@")) + string([]byte{0xff})},
		{name: "too long", email: strings.Repeat("a", 245) + "@example.com"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := browserauth.NormalizeEmailDomain(test.email); err == nil {
				t.Fatal("invalid mailbox was accepted")
			}
		})
	}
}

func TestEmailDomainAllowedUsesExactCanonicalDomain(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		email   string
		allowed []string
		want    bool
	}{
		{name: "empty policy allows valid email", email: "user@any.example", want: true},
		{name: "exact match", email: "user@Example.COM", allowed: []string{"example.com"}, want: true},
		{name: "IDNA match", email: "user@bücher.example", allowed: []string{"XN--BCHER-KVA.EXAMPLE"}, want: true},
		{name: "duplicate canonical entries", email: "user@example.com", allowed: []string{"EXAMPLE.COM", "example.com"}, want: true},
		{name: "subdomain denied", email: "user@sub.example.com", allowed: []string{"example.com"}, want: false},
		{name: "suffix denied", email: "user@notexample.com", allowed: []string{"example.com"}, want: false},
		{name: "invalid policy denied", email: "user@example.com", allowed: []string{"*.example.com"}, want: false},
		{name: "invalid policy fails closed", email: "user@example.com", allowed: []string{"example.com", "*.example.com"}, want: false},
		{name: "invalid email denied by empty policy", email: "not-an-email", want: false},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			if got := browserauth.EmailDomainAllowed(test.email, test.allowed); got != test.want {
				t.Fatalf("domain policy result mismatch: got=%v want=%v", got, test.want)
			}
		})
	}
}
