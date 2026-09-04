package storagepolicy

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
	WorkspaceTypeManagedVolume = "managed-volume"
	MinWorkspaceCapacityBytes  = int64(134217728)
	MaxWorkspaceCapacityBytes  = int64(1099511627776)
)

var ErrInvalidInput = errors.New("storage policy input is invalid")

type Scope struct{ TenantID, ProjectID string }
type Mutation struct{ RequestID, IdempotencyKey string }

type SetInput struct {
	Scope                     Scope
	PolicyID                  string
	PolicyName                string
	UserSummary               string
	WorkspaceType             string
	WorkspaceCapacityBytes    int64
	RetentionSeconds          int64
	CleanupOnLeaseTermination bool
	SnapshotBackendRef        string
	ArtifactBackendRef        string
	AllowWorkspaceReuse       bool
	ExpectedResourceVersion   int64
	Mutation                  Mutation
}

type Snapshot struct {
	Scope                     Scope
	PolicyID                  string
	PolicyName                string
	UserSummary               string
	WorkspaceType             string
	WorkspaceCapacityBytes    int64
	RetentionSeconds          int64
	CleanupOnLeaseTermination bool
	SnapshotBackendRef        string
	ArtifactBackendRef        string
	AllowWorkspaceReuse       bool
	ResourceVersion           int64
	CreatedAt                 time.Time
	UpdatedAt                 time.Time
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
	if input.Scope.TenantID != tenantID || invalidIdentifier(input.Scope.ProjectID) ||
		invalidIdentifier(input.PolicyID) || invalidIdentifier(input.PolicyName) ||
		invalidSummary(input.UserSummary) || input.WorkspaceType != WorkspaceTypeManagedVolume ||
		input.WorkspaceCapacityBytes < MinWorkspaceCapacityBytes || input.WorkspaceCapacityBytes > MaxWorkspaceCapacityBytes ||
		input.RetentionSeconds != 0 || !input.CleanupOnLeaseTermination || !input.AllowWorkspaceReuse ||
		input.SnapshotBackendRef != "" && invalidIdentifier(input.SnapshotBackendRef) ||
		input.ArtifactBackendRef != "" && invalidIdentifier(input.ArtifactBackendRef) ||
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
		WorkspaceType, SnapshotBackendRef, ArtifactBackendRef             string
		WorkspaceCapacityBytes, RetentionSeconds                          int64
		CleanupOnLeaseTermination, AllowWorkspaceReuse                    bool
		ExpectedResourceVersion                                           int64
	}{
		"storage-policy.set", input.Scope.TenantID, input.Scope.ProjectID,
		input.PolicyID, input.PolicyName, input.UserSummary, input.WorkspaceType,
		input.SnapshotBackendRef, input.ArtifactBackendRef, input.WorkspaceCapacityBytes,
		input.RetentionSeconds, input.CleanupOnLeaseTermination, input.AllowWorkspaceReuse,
		input.ExpectedResourceVersion,
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
		UserSummary: snapshot.UserSummary, WorkspaceType: snapshot.WorkspaceType,
		WorkspaceCapacityBytes: snapshot.WorkspaceCapacityBytes, RetentionSeconds: snapshot.RetentionSeconds,
		CleanupOnLeaseTermination: snapshot.CleanupOnLeaseTermination, SnapshotBackendRef: snapshot.SnapshotBackendRef,
		ArtifactBackendRef: snapshot.ArtifactBackendRef, AllowWorkspaceReuse: snapshot.AllowWorkspaceReuse,
		ExpectedResourceVersion: snapshot.ResourceVersion - 1,
		Mutation:                Mutation{RequestID: "snapshot-validation", IdempotencyKey: "snapshot-validation"},
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
		event.Action != "storage-policy.set" || invalidIdentifier(event.PolicyID) ||
		event.PolicyResourceVersion < 1 || event.Result != "succeeded" ||
		invalidIdentifier(event.RequestID) || event.OccurredAt.IsZero() {
		return ErrInvalidInput
	}
	return nil
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
