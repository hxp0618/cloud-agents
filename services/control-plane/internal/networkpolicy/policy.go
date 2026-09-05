package networkpolicy

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	commonv1alpha1 "github.com/hxp0618/cloud-agents/sdk/go/gen/common/v1alpha1"
)

const (
	DefaultEgressPublic     = "public"
	DefaultEgressRestricted = "restricted"
	DefaultEgressDeny       = "deny"
)

var ErrInvalidInput = errors.New("network policy input is invalid")

type Scope struct{ TenantID, ProjectID string }
type Mutation struct{ RequestID, IdempotencyKey string }

type SetInput struct {
	Scope                   Scope
	PolicyID                string
	PolicyName              string
	UserSummary             string
	DefaultEgress           string
	AllowlistPolicyRef      string
	IngressEnabled          bool
	PreviewEnabled          bool
	DNSPolicyRef            string
	ProxyPolicyRef          string
	ExpectedResourceVersion int64
	Mutation                Mutation
}

type Snapshot struct {
	Scope              Scope
	PolicyID           string
	PolicyName         string
	UserSummary        string
	DefaultEgress      string
	AllowlistPolicyRef string
	IngressEnabled     bool
	PreviewEnabled     bool
	DNSPolicyRef       string
	ProxyPolicyRef     string
	ResourceVersion    int64
	CreatedAt          time.Time
	UpdatedAt          time.Time
}

type AuditEvent struct {
	Scope                 Scope
	EventID               string
	OperationID           string
	Actor                 string
	Action                string
	PolicyID              string
	PolicyResourceVersion int64
	Result                string
	RequestID             string
	OccurredAt            time.Time
}

func (input SetInput) Validate(tenantID string) error {
	if invalidIdentifier(tenantID) || input.Scope.TenantID != tenantID || invalidIdentifier(input.Scope.ProjectID) ||
		invalidIdentifier(input.PolicyID) || invalidIdentifier(input.PolicyName) ||
		invalidSummary(input.UserSummary) || !validDefaultEgress(input.DefaultEgress) ||
		(input.AllowlistPolicyRef != "" && invalidIdentifier(input.AllowlistPolicyRef)) ||
		(input.DNSPolicyRef != "" && invalidIdentifier(input.DNSPolicyRef)) ||
		(input.ProxyPolicyRef != "" && invalidIdentifier(input.ProxyPolicyRef)) ||
		input.ExpectedResourceVersion < 0 || invalidIdentifier(input.Mutation.RequestID) ||
		commonv1alpha1.ValidateIdempotencyKey(input.Mutation.IdempotencyKey, "/idempotencyKey") != nil {
		return ErrInvalidInput
	}
	return nil
}

func MutationDigest(input SetInput) (string, error) {
	if input.Validate(input.Scope.TenantID) != nil {
		return "", ErrInvalidInput
	}
	encoded, err := json.Marshal(struct {
		Operation, TenantID, ProjectID, PolicyID, PolicyName, UserSummary string
		DefaultEgress, AllowlistPolicyRef, DNSPolicyRef, ProxyPolicyRef   string
		IngressEnabled, PreviewEnabled                                    bool
		ExpectedResourceVersion                                           int64
	}{
		"network-policy.set", input.Scope.TenantID, input.Scope.ProjectID,
		input.PolicyID, input.PolicyName, input.UserSummary, input.DefaultEgress,
		input.AllowlistPolicyRef, input.DNSPolicyRef, input.ProxyPolicyRef,
		input.IngressEnabled, input.PreviewEnabled, input.ExpectedResourceVersion,
	})
	if err != nil {
		return "", ErrInvalidInput
	}
	sum := sha256.Sum256(encoded)
	return "sha256:" + hex.EncodeToString(sum[:]), nil
}

func (snapshot Snapshot) Validate() error {
	input := SetInput{
		Scope: snapshot.Scope, PolicyID: snapshot.PolicyID, PolicyName: snapshot.PolicyName,
		UserSummary: snapshot.UserSummary, DefaultEgress: snapshot.DefaultEgress,
		AllowlistPolicyRef: snapshot.AllowlistPolicyRef, IngressEnabled: snapshot.IngressEnabled,
		PreviewEnabled: snapshot.PreviewEnabled, DNSPolicyRef: snapshot.DNSPolicyRef,
		ProxyPolicyRef: snapshot.ProxyPolicyRef, ExpectedResourceVersion: snapshot.ResourceVersion - 1,
		Mutation: Mutation{RequestID: "snapshot-validation", IdempotencyKey: "snapshot-validation"},
	}
	if input.Validate(snapshot.Scope.TenantID) != nil || snapshot.ResourceVersion < 1 ||
		snapshot.CreatedAt.IsZero() || snapshot.UpdatedAt.Before(snapshot.CreatedAt) {
		return ErrInvalidInput
	}
	return nil
}

func (event AuditEvent) Validate() error {
	if invalidIdentifier(event.Scope.TenantID) || invalidIdentifier(event.Scope.ProjectID) ||
		invalidIdentifier(event.EventID) || invalidIdentifier(event.OperationID) || !digest(event.Actor) ||
		event.Action != "network-policy.set" || invalidIdentifier(event.PolicyID) ||
		event.PolicyResourceVersion < 1 || event.Result != "succeeded" ||
		invalidIdentifier(event.RequestID) || event.OccurredAt.IsZero() {
		return ErrInvalidInput
	}
	return nil
}

func validDefaultEgress(value string) bool {
	return value == DefaultEgressPublic || value == DefaultEgressRestricted || value == DefaultEgressDeny
}

func invalidSummary(value string) bool {
	return !utf8.ValidString(value) || utf8.RuneCountInString(value) < 1 || utf8.RuneCountInString(value) > 256 ||
		strings.ContainsFunc(value, func(character rune) bool { return character < 32 || character == 127 })
}

func invalidIdentifier(value string) bool {
	return commonv1alpha1.ValidateIdentifier(value, "/value") != nil
}

func digest(value string) bool {
	if len(value) != 71 || value[:7] != "sha256:" {
		return false
	}
	for _, character := range value[7:] {
		if !(character >= '0' && character <= '9' || character >= 'a' && character <= 'f') {
			return false
		}
	}
	return true
}
