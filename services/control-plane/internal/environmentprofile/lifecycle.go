package environmentprofile

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	commonv1alpha1 "github.com/hxp0618/cloud-agents/sdk/go/gen/common/v1alpha1"
)

const LifecycleProfileID = "cloud-agents/environment-profile/v1alpha1"

const (
	TransitionPublish = "publish"
	TransitionDisable = "disable"
)

var (
	ErrInvalidInput        = errors.New("environment profile input is invalid")
	ErrNotFound            = errors.New("environment profile was not found")
	ErrConflict            = errors.New("environment profile transition is invalid")
	ErrIdempotencyConflict = errors.New("environment profile idempotency key conflicts")
	digestPattern          = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)
)

type Scope struct{ TenantID, ProjectID string }
type Mutation struct{ RequestID, IdempotencyKey string }

type CreateInput struct {
	Scope                 Scope
	ProfileID             string
	ProfileName           string
	Version               int64
	Description           string
	ProviderKinds         []string
	CPULimitMillis        int64
	MemoryLimitBytes      int64
	StoragePolicyRef      string
	NetworkPolicyRef      string
	ReleaseDigest         string
	TargetRefs            []string
	ProviderCredentialRef string
	Mutation              Mutation
}

type TransitionInput struct {
	Scope                   Scope
	ProfileID               string
	Version                 int64
	ExpectedResourceVersion int64
	Action                  string
	Mutation                Mutation
}

type Snapshot struct {
	Scope                 Scope
	ProfileVersionUID     string
	ProfileID             string
	ProfileName           string
	Version               int64
	Description           string
	Status                string
	ProviderKinds         []string
	CPULimitMillis        int64
	MemoryLimitBytes      int64
	StoragePolicyRef      string
	NetworkPolicyRef      string
	ReleaseDigest         string
	TargetRefs            []string
	ProviderCredentialRef string
	ResourceVersion       int64
	CreatedAt             time.Time
	UpdatedAt             time.Time
	PublishedAt           *time.Time
	DisabledAt            *time.Time
}

type AuditEvent struct {
	Scope                     Scope
	EventID, OperationID      string
	Actor, Action, ProfileUID string
	ProfileVersion            int64
	Result, RequestID         string
	OccurredAt                time.Time
}

func (input CreateInput) Validate(tenantID string) error {
	if input.Scope.TenantID != tenantID || invalidIdentifier(input.Scope.ProjectID) ||
		invalidIdentifier(input.ProfileID) || invalidIdentifier(input.ProfileName) ||
		input.Version < 1 || input.Version > 2147483647 || invalidDescription(input.Description) ||
		!validProviderKinds(input.ProviderKinds) || input.CPULimitMillis < 100 || input.CPULimitMillis > 64000 ||
		input.MemoryLimitBytes < 134217728 || input.MemoryLimitBytes > 1099511627776 ||
		invalidIdentifier(input.StoragePolicyRef) || invalidIdentifier(input.NetworkPolicyRef) ||
		!digestPattern.MatchString(input.ReleaseDigest) || !validIdentifiers(input.TargetRefs, 32) ||
		invalidIdentifier(input.ProviderCredentialRef) {
		return ErrInvalidInput
	}
	return validateMutation(input.Mutation)
}

func CreateMutationDigest(input CreateInput) (string, error) {
	if input.Validate(input.Scope.TenantID) != nil {
		return "", ErrInvalidInput
	}
	encoded, err := json.Marshal(struct {
		Operation, TenantID, ProjectID, ProfileID, ProfileName, Description string
		ProviderKinds, TargetRefs                                           []string
		Version, CPULimitMillis, MemoryLimitBytes                           int64
		StoragePolicyRef, NetworkPolicyRef, ReleaseDigest                   string
		ProviderCredentialRef                                               string
	}{
		"environment-profile.create", input.Scope.TenantID, input.Scope.ProjectID,
		input.ProfileID, input.ProfileName, input.Description, input.ProviderKinds,
		input.TargetRefs, input.Version, input.CPULimitMillis, input.MemoryLimitBytes,
		input.StoragePolicyRef, input.NetworkPolicyRef, input.ReleaseDigest,
		input.ProviderCredentialRef,
	})
	if err != nil {
		return "", ErrInvalidInput
	}
	sum := sha256.Sum256(encoded)
	return "sha256:" + hex.EncodeToString(sum[:]), nil
}

func (input TransitionInput) Validate(tenantID string) error {
	if input.Scope.TenantID != tenantID || invalidIdentifier(input.Scope.ProjectID) ||
		invalidIdentifier(input.ProfileID) || input.Version < 1 || input.Version > 2147483647 ||
		input.ExpectedResourceVersion < 1 || !validTransition(input.Action) {
		return ErrInvalidInput
	}
	return validateMutation(input.Mutation)
}

func TransitionMutationDigest(input TransitionInput) (string, error) {
	if input.Validate(input.Scope.TenantID) != nil {
		return "", ErrInvalidInput
	}
	encoded, err := json.Marshal(struct {
		Operation, TenantID, ProjectID, ProfileID string
		Version, ExpectedResourceVersion          int64
	}{
		"environment-profile." + input.Action, input.Scope.TenantID, input.Scope.ProjectID,
		input.ProfileID, input.Version, input.ExpectedResourceVersion,
	})
	if err != nil {
		return "", ErrInvalidInput
	}
	sum := sha256.Sum256(encoded)
	return "sha256:" + hex.EncodeToString(sum[:]), nil
}

func (snapshot Snapshot) Validate() error {
	if invalidIdentifier(snapshot.Scope.TenantID) || invalidIdentifier(snapshot.Scope.ProjectID) ||
		invalidIdentifier(snapshot.ProfileVersionUID) || invalidIdentifier(snapshot.ProfileID) ||
		invalidIdentifier(snapshot.ProfileName) || snapshot.Version < 1 || snapshot.Version > 2147483647 ||
		invalidDescription(snapshot.Description) || !validProviderKinds(snapshot.ProviderKinds) ||
		snapshot.CPULimitMillis < 100 || snapshot.CPULimitMillis > 64000 ||
		snapshot.MemoryLimitBytes < 134217728 || snapshot.MemoryLimitBytes > 1099511627776 ||
		invalidIdentifier(snapshot.StoragePolicyRef) || invalidIdentifier(snapshot.NetworkPolicyRef) ||
		!digestPattern.MatchString(snapshot.ReleaseDigest) || !validIdentifiers(snapshot.TargetRefs, 32) ||
		invalidIdentifier(snapshot.ProviderCredentialRef) || snapshot.ResourceVersion < 1 ||
		snapshot.CreatedAt.IsZero() || snapshot.UpdatedAt.Before(snapshot.CreatedAt) {
		return ErrInvalidInput
	}
	switch snapshot.Status {
	case "draft":
		if snapshot.PublishedAt != nil || snapshot.DisabledAt != nil {
			return ErrInvalidInput
		}
	case "published":
		if snapshot.PublishedAt == nil || snapshot.DisabledAt != nil || snapshot.PublishedAt.Before(snapshot.CreatedAt) {
			return ErrInvalidInput
		}
	case "disabled":
		if snapshot.PublishedAt == nil || snapshot.DisabledAt == nil || snapshot.PublishedAt.Before(snapshot.CreatedAt) || snapshot.DisabledAt.Before(*snapshot.PublishedAt) {
			return ErrInvalidInput
		}
	default:
		return ErrInvalidInput
	}
	return nil
}

func (event AuditEvent) Validate() error {
	if invalidIdentifier(event.Scope.TenantID) || invalidIdentifier(event.Scope.ProjectID) ||
		invalidIdentifier(event.EventID) || invalidIdentifier(event.OperationID) ||
		!digestPattern.MatchString(event.Actor) || !validAuditAction(event.Action) ||
		invalidIdentifier(event.ProfileUID) || event.ProfileVersion < 1 ||
		event.Result != "succeeded" || invalidIdentifier(event.RequestID) || event.OccurredAt.IsZero() {
		return ErrInvalidInput
	}
	return nil
}

func validTransition(action string) bool {
	return action == TransitionPublish || action == TransitionDisable
}

func validAuditAction(action string) bool {
	return action == "profile.create" || action == "profile.publish" || action == "profile.disable"
}

func validProviderKinds(values []string) bool {
	if len(values) < 1 || len(values) > 2 {
		return false
	}
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		if value != "codex" && value != "claudeAgent" {
			return false
		}
		if _, duplicate := seen[value]; duplicate {
			return false
		}
		seen[value] = struct{}{}
	}
	return true
}

func validIdentifiers(values []string, maximum int) bool {
	if len(values) < 1 || len(values) > maximum {
		return false
	}
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		if invalidIdentifier(value) {
			return false
		}
		if _, duplicate := seen[value]; duplicate {
			return false
		}
		seen[value] = struct{}{}
	}
	return true
}

func invalidDescription(value string) bool {
	if !utf8.ValidString(value) || utf8.RuneCountInString(value) < 1 || utf8.RuneCountInString(value) > 1024 {
		return true
	}
	return strings.ContainsFunc(value, func(character rune) bool { return character < 32 || character == 127 })
}

func invalidIdentifier(value string) bool {
	return commonv1alpha1.ValidateIdentifier(value, "/value") != nil
}

func validateMutation(mutation Mutation) error {
	if invalidIdentifier(mutation.RequestID) || commonv1alpha1.ValidateIdempotencyKey(mutation.IdempotencyKey, "/idempotencyKey") != nil {
		return ErrInvalidInput
	}
	return nil
}
