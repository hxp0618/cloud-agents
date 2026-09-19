package managedagent

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
)

// CapabilityEventInput is the transport-neutral, redacted audit fact written
// after a Runtime capability decision. It carries no MCP payload or Skill
// source bytes.
type CapabilityEventInput struct {
	Scope          Scope
	SessionID      string
	TurnID         string
	ExecutionID    string
	Generation     uint64
	Operation      string
	Resource       ResourceKind
	ResourceID     string
	Version        string
	Digest         string
	Result         string
	MutationDigest string
	InputDigest    string
	ResultDigest   string
	ErrorCode      string
}

// CapabilityResolutionFailure identifies the one opaque binding that failed
// authorization. Reason is a stable redacted code; catalog values and secrets
// are deliberately excluded.
type CapabilityResolutionFailure struct {
	Resource   ResourceKind
	ResourceID string
	Version    string
	Digest     string
	Reason     string
}

func (failure *CapabilityResolutionFailure) Error() string {
	return "managed capability resolution failed: " + failure.Reason
}

// CapabilityEventMutationDigest gives each redacted admission fact a stable
// idempotency key without hashing any provider payload, source, or secret.
func CapabilityEventMutationDigest(input CapabilityEventInput) string {
	encoded, _ := json.Marshal(struct {
		Scope        Scope
		SessionID    string
		TurnID       string
		ExecutionID  string
		Generation   uint64
		Operation    string
		Resource     ResourceKind
		ResourceID   string
		Version      string
		Digest       string
		Result       string
		InputDigest  string
		ResultDigest string
		ErrorCode    string
	}{
		Scope: input.Scope, SessionID: input.SessionID, TurnID: input.TurnID, ExecutionID: input.ExecutionID,
		Generation: input.Generation, Operation: input.Operation, Resource: input.Resource, ResourceID: input.ResourceID,
		Version: input.Version, Digest: input.Digest, Result: input.Result, InputDigest: input.InputDigest,
		ResultDigest: input.ResultDigest, ErrorCode: input.ErrorCode,
	})
	hash := sha256.Sum256(append([]byte(LifecycleProfileID+"/capability-event\x00"), encoded...))
	return "sha256:" + hex.EncodeToString(hash[:])
}
