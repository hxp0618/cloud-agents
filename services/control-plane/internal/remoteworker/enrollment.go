package remoteworker

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/url"
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

type CertificatePersistenceInput struct {
	Scope                   Scope
	EnrollmentID            string
	ExpectedResourceVersion int64
	ConfirmedEnrollmentID   string
	SecretDigest            string
	ActorDigest             string
	Certificate             Certificate
	Mutation                Mutation
}

type CertificateRotationRequest struct {
	Scope                   Scope
	EnrollmentID            string
	ExpectedResourceVersion int64
	ConfirmedEnrollmentID   string
	PeerCertificateSHA256   string
	IncarnationID           string
	CSRPEM                  string
	Mutation                Mutation
}

type CertificateRotationPersistenceInput struct {
	Request     CertificateRotationRequest
	Certificate Certificate
}

type Snapshot struct {
	Scope                Scope
	EnrollmentID         string
	WorkerID             string
	WorkerName           string
	State                string
	ResourceVersion      int64
	CreatedAt            time.Time
	UpdatedAt            time.Time
	ExpiresAt            time.Time
	SecretClaimedAt      *time.Time
	EnrolledAt           *time.Time
	RevokedAt            *time.Time
	IncarnationID        string
	SPIFFEID             string
	CertificateSHA256    string
	CertificateChainPEM  string
	CertificateSerial    string
	CertificateNotBefore *time.Time
	CertificateNotAfter  *time.Time
	CertificateState     string
	CertificateRevokedAt *time.Time
	Node                 *NodeStatus
}

type AuditEvent struct {
	Scope              Scope
	EventID            string
	OperationID        string
	Actor              string
	Action             string
	EnrollmentID       string
	ResourceGeneration int64
	Result             string
	RequestID          string
	StableErrorCode    string
	OccurredAt         time.Time
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

func CertificateMutationDigest(input CertificatePersistenceInput) (string, error) {
	if input.Validate(input.Scope.TenantID) != nil {
		return "", ErrInvalidInput
	}
	return mutationDigest("remote-worker-enrollment.issue-certificate", input.Scope, input.EnrollmentID, input.ExpectedResourceVersion, input.ConfirmedEnrollmentID, input.SecretDigest, input.ActorDigest, input.Certificate.CSRSHA256, input.Certificate.IncarnationID, input.Certificate.SPIFFEID)
}

func CertificateRotationMutationDigest(input CertificateRotationRequest) (string, error) {
	if input.Validate(input.Scope.TenantID) != nil {
		return "", ErrInvalidInput
	}
	return mutationDigest("remote-worker-enrollment.rotate-certificate", input.Scope, input.EnrollmentID, input.ExpectedResourceVersion, input.ConfirmedEnrollmentID, input.IncarnationID, input.CSRPEM)
}

func (input CertificateRotationRequest) Validate(tenantID string) error {
	if invalidIdentifier(tenantID) || input.Scope.TenantID != tenantID || invalidIdentifier(input.Scope.ProjectID) ||
		invalidIdentifier(input.EnrollmentID) || input.ConfirmedEnrollmentID != input.EnrollmentID ||
		input.ExpectedResourceVersion < 1 || !digest(input.PeerCertificateSHA256) || invalidIdentifier(input.IncarnationID) ||
		len(input.CSRPEM) == 0 || len(input.CSRPEM) > 32768 || invalidMutation(input.Mutation) {
		return ErrInvalidInput
	}
	return nil
}

func (input CertificateRotationPersistenceInput) Validate(tenantID string) error {
	if input.Request.Validate(tenantID) != nil || input.Certificate.IncarnationID != input.Request.IncarnationID || input.Certificate.SHA256 == input.Request.PeerCertificateSHA256 || invalidCertificate(input.Request.Scope, input.Request.EnrollmentID, input.Certificate) {
		return ErrInvalidInput
	}
	return nil
}

func (input CertificatePersistenceInput) Validate(tenantID string) error {
	certificate := input.Certificate
	if invalidIdentifier(tenantID) || input.Scope.TenantID != tenantID || invalidIdentifier(input.Scope.ProjectID) || invalidIdentifier(input.EnrollmentID) || input.ConfirmedEnrollmentID != input.EnrollmentID || input.ExpectedResourceVersion < 1 || !digest(input.SecretDigest) || !digest(input.ActorDigest) || invalidCertificate(input.Scope, input.EnrollmentID, certificate) || invalidMutation(input.Mutation) {
		return ErrInvalidInput
	}
	return nil
}

func invalidCertificate(scope Scope, enrollmentID string, certificate Certificate) bool {
	if !digest(certificate.CSRSHA256) || invalidIdentifier(certificate.IncarnationID) || certificate.SPIFFEID == "" || certificate.ChainPEM == "" || len(certificate.ChainPEM) > 32768 || !digest(certificate.SHA256) || certificate.Serial == "" || certificate.NotBefore.IsZero() || !certificate.NotAfter.After(certificate.NotBefore) {
		return true
	}
	identity, err := url.Parse(certificate.SPIFFEID)
	expectedPath := "/remote-worker/" + scope.TenantID + "/" + scope.ProjectID + "/" + enrollmentID + "/" + certificate.IncarnationID
	if err != nil || identity.Scheme != "spiffe" || identity.Host == "" || identity.Path != expectedPath || identity.User != nil || identity.RawQuery != "" || identity.Fragment != "" {
		return true
	}
	return false
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
		identity, err := url.Parse(snapshot.SPIFFEID)
		expectedPath := "/remote-worker/" + snapshot.Scope.TenantID + "/" + snapshot.Scope.ProjectID + "/" + snapshot.EnrollmentID + "/" + snapshot.IncarnationID
		if snapshot.SecretClaimedAt == nil || snapshot.EnrolledAt == nil || snapshot.RevokedAt != nil || invalidIdentifier(snapshot.IncarnationID) || err != nil || identity.Scheme != "spiffe" || identity.Host == "" || identity.Path != expectedPath || identity.User != nil || identity.RawQuery != "" || identity.Fragment != "" || !digest(snapshot.CertificateSHA256) || snapshot.CertificateChainPEM == "" || snapshot.CertificateSerial == "" || snapshot.CertificateNotBefore == nil || snapshot.CertificateNotAfter == nil || !snapshot.CertificateNotAfter.After(*snapshot.CertificateNotBefore) || snapshot.CertificateNotBefore.After(snapshot.UpdatedAt) || snapshot.CertificateState != "active" && snapshot.CertificateState != "revoked" {
			return ErrInvalidInput
		}
		if snapshot.CertificateState == "active" && (snapshot.CertificateRevokedAt != nil || !snapshot.CertificateNotAfter.After(snapshot.UpdatedAt)) || snapshot.CertificateState == "revoked" && (snapshot.CertificateRevokedAt == nil || snapshot.CertificateRevokedAt.Before(*snapshot.EnrolledAt) || snapshot.CertificateRevokedAt.After(snapshot.UpdatedAt)) {
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
	if snapshot.State != StateEnrolled && (snapshot.IncarnationID != "" || snapshot.SPIFFEID != "" || snapshot.CertificateSHA256 != "" || snapshot.CertificateChainPEM != "" || snapshot.CertificateSerial != "" || snapshot.CertificateNotBefore != nil || snapshot.CertificateNotAfter != nil || snapshot.CertificateState != "" || snapshot.CertificateRevokedAt != nil) {
		return ErrInvalidInput
	}
	if snapshot.Node != nil && (snapshot.State != StateEnrolled || snapshot.Node.Validate() != nil ||
		snapshot.Node.Scope != snapshot.Scope || snapshot.Node.EnrollmentID != snapshot.EnrollmentID ||
		snapshot.Node.WorkerID != snapshot.WorkerID || snapshot.Node.WorkerName != snapshot.WorkerName ||
		snapshot.Node.IncarnationID != snapshot.IncarnationID) {
		return ErrInvalidInput
	}
	return nil
}

func (event AuditEvent) Validate() error {
	if invalidIdentifier(event.Scope.TenantID) || invalidIdentifier(event.Scope.ProjectID) || invalidIdentifier(event.EventID) ||
		invalidIdentifier(event.OperationID) || !digest(event.Actor) ||
		event.Action != "remote-worker-enrollment.create" && event.Action != "remote-worker-enrollment.claim-secret" && event.Action != "remote-worker-enrollment.issue-certificate" && event.Action != "remote-worker-enrollment.revoke" && event.Action != "remote-worker.drain" && event.Action != "remote-worker.resume" ||
		invalidIdentifier(event.EnrollmentID) || event.ResourceGeneration < 1 ||
		event.Result != "requested" && event.Result != "succeeded" && event.Result != "failed" ||
		invalidIdentifier(event.RequestID) || event.StableErrorCode != "" && invalidIdentifier(event.StableErrorCode) ||
		(event.Result == "failed") != (event.StableErrorCode != "") || event.OccurredAt.IsZero() {
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
