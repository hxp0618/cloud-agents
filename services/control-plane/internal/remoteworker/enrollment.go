package remoteworker

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"time"

	commonv1alpha1 "github.com/hxp0618/cloud-agents/sdk/go/gen/common/v1alpha1"
)

const (
	StatePending      = "pending"
	StateSecretIssued = "secret-issued"
	StateEnrolled     = "enrolled"
	StateRevoked      = "revoked"
	StateExpired      = "expired"
)

var ErrInvalidInput = errors.New("remote worker enrollment input is invalid")

type Scope struct{ TenantID, ProjectID string }
type Mutation struct{ RequestID, IdempotencyKey string }

type CreateInput struct {
	Scope        Scope
	EnrollmentID string
	WorkerID     string
	WorkerName   string
	TTLSeconds   int64
	Mutation     Mutation
}

type TransitionInput struct {
	Scope                   Scope
	EnrollmentID            string
	ExpectedResourceVersion int64
	ConfirmedEnrollmentID   string
	Mutation                Mutation
}

type Snapshot struct {
	Scope           Scope
	EnrollmentID    string
	WorkerID        string
	WorkerName      string
	State           string
	ResourceVersion int64
	CreatedAt       time.Time
	UpdatedAt       time.Time
	ExpiresAt       time.Time
	SecretClaimedAt *time.Time
	EnrolledAt      *time.Time
	RevokedAt       *time.Time
}

type AuditEvent struct {
	Scope                     Scope
	EventID                   string
	OperationID               string
	Actor                     string
	Action                    string
	EnrollmentID              string
	EnrollmentResourceVersion int64
	Result                    string
	RequestID                 string
	OccurredAt                time.Time
}

func (input CreateInput) Validate(tenantID string) error {
	if invalidIdentifier(tenantID) || input.Scope.TenantID != tenantID || invalidIdentifier(input.Scope.ProjectID) ||
		invalidIdentifier(input.EnrollmentID) || invalidIdentifier(input.WorkerID) || invalidIdentifier(input.WorkerName) ||
		input.TTLSeconds < 300 || input.TTLSeconds > 3600 || invalidMutation(input.Mutation) {
		return ErrInvalidInput
	}
	return nil
}

func (input TransitionInput) Validate(tenantID string) error {
	if invalidIdentifier(tenantID) || input.Scope.TenantID != tenantID || invalidIdentifier(input.Scope.ProjectID) ||
		invalidIdentifier(input.EnrollmentID) || input.ConfirmedEnrollmentID != input.EnrollmentID ||
		input.ExpectedResourceVersion < 1 || invalidMutation(input.Mutation) {
		return ErrInvalidInput
	}
	return nil
}

func CreateMutationDigest(input CreateInput) (string, error) {
	if input.Validate(input.Scope.TenantID) != nil {
		return "", ErrInvalidInput
	}
	return mutationDigest("remote-worker-enrollment.create", input.Scope, input.EnrollmentID, input.WorkerID, input.WorkerName, input.TTLSeconds)
}

func TransitionMutationDigest(action string, input TransitionInput) (string, error) {
	if input.Validate(input.Scope.TenantID) != nil || action != "claim-secret" && action != "revoke" {
		return "", ErrInvalidInput
	}
	return mutationDigest("remote-worker-enrollment."+action, input.Scope, input.EnrollmentID, input.ExpectedResourceVersion, input.ConfirmedEnrollmentID)
}

func NewEnrollmentSecret() (string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return "carw1_" + base64.RawURLEncoding.EncodeToString(raw), nil
}

func SecretDigest(secret string) (string, error) {
	if !strings.HasPrefix(secret, "carw1_") || len(secret) != 49 {
		return "", ErrInvalidInput
	}
	if raw, err := base64.RawURLEncoding.Strict().DecodeString(secret[6:]); err != nil || len(raw) != 32 {
		return "", ErrInvalidInput
	}
	sum := sha256.Sum256([]byte(secret))
	return "sha256:" + hex.EncodeToString(sum[:]), nil
}

func (snapshot Snapshot) Validate() error {
	if invalidIdentifier(snapshot.Scope.TenantID) || invalidIdentifier(snapshot.Scope.ProjectID) || invalidIdentifier(snapshot.EnrollmentID) ||
		invalidIdentifier(snapshot.WorkerID) || invalidIdentifier(snapshot.WorkerName) || snapshot.ResourceVersion < 1 ||
		snapshot.CreatedAt.IsZero() || snapshot.UpdatedAt.Before(snapshot.CreatedAt) || !snapshot.ExpiresAt.After(snapshot.CreatedAt) {
		return ErrInvalidInput
	}
	between := func(value *time.Time) bool {
		return value == nil || !value.Before(snapshot.CreatedAt) && !value.After(snapshot.UpdatedAt)
	}
	if !between(snapshot.SecretClaimedAt) || !between(snapshot.EnrolledAt) || !between(snapshot.RevokedAt) ||
		snapshot.SecretClaimedAt != nil && !snapshot.SecretClaimedAt.Before(snapshot.ExpiresAt) ||
		snapshot.EnrolledAt != nil && (snapshot.SecretClaimedAt == nil || snapshot.EnrolledAt.Before(*snapshot.SecretClaimedAt) || !snapshot.EnrolledAt.Before(snapshot.ExpiresAt)) {
		return ErrInvalidInput
	}
	switch snapshot.State {
	case StatePending:
		if snapshot.SecretClaimedAt != nil || snapshot.EnrolledAt != nil || snapshot.RevokedAt != nil {
			return ErrInvalidInput
		}
	case StateSecretIssued:
		if snapshot.SecretClaimedAt == nil || snapshot.EnrolledAt != nil || snapshot.RevokedAt != nil {
			return ErrInvalidInput
		}
	case StateEnrolled:
		if snapshot.SecretClaimedAt == nil || snapshot.EnrolledAt == nil || snapshot.RevokedAt != nil {
			return ErrInvalidInput
		}
	case StateRevoked:
		if snapshot.EnrolledAt != nil || snapshot.RevokedAt == nil {
			return ErrInvalidInput
		}
	case StateExpired:
		if snapshot.EnrolledAt != nil || snapshot.RevokedAt != nil {
			return ErrInvalidInput
		}
	default:
		return ErrInvalidInput
	}
	return nil
}

func (event AuditEvent) Validate() error {
	if invalidIdentifier(event.Scope.TenantID) || invalidIdentifier(event.Scope.ProjectID) || invalidIdentifier(event.EventID) ||
		invalidIdentifier(event.OperationID) || !digest(event.Actor) ||
		event.Action != "remote-worker-enrollment.create" && event.Action != "remote-worker-enrollment.claim-secret" && event.Action != "remote-worker-enrollment.revoke" ||
		invalidIdentifier(event.EnrollmentID) || event.EnrollmentResourceVersion < 1 || event.Result != "succeeded" ||
		invalidIdentifier(event.RequestID) || event.OccurredAt.IsZero() {
		return ErrInvalidInput
	}
	return nil
}

func mutationDigest(values ...any) (string, error) {
	encoded, err := json.Marshal(values)
	if err != nil {
		return "", ErrInvalidInput
	}
	sum := sha256.Sum256(encoded)
	return "sha256:" + hex.EncodeToString(sum[:]), nil
}

func invalidMutation(mutation Mutation) bool {
	return invalidIdentifier(mutation.RequestID) || commonv1alpha1.ValidateIdempotencyKey(mutation.IdempotencyKey, "/idempotencyKey") != nil
}

func invalidIdentifier(value string) bool {
	return commonv1alpha1.ValidateIdentifier(value, "/value") != nil
}

func digest(value string) bool {
	if len(value) != 71 || !strings.HasPrefix(value, "sha256:") {
		return false
	}
	_, err := hex.DecodeString(value[7:])
	return err == nil
}
