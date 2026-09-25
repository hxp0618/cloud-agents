package managedagent

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"unicode/utf8"
)

func (scope Scope) validate() error {
	if err := validateIdentifier(scope.TenantID, maxIdentifierBytes, "tenant id"); err != nil {
		return err
	}
	return validateIdentifier(scope.ProjectID, maxIdentifierBytes, "project id")
}

func (mutation Mutation) validate() error {
	if err := validateToken(mutation.RequestID, maxMutationBytes, "request id"); err != nil {
		return err
	}
	if err := validateToken(mutation.IdempotencyKey, maxMutationBytes, "idempotency key"); err != nil {
		return err
	}
	if mutation.executionBindingDigest != "" {
		return validateDigest(mutation.executionBindingDigest, "execution binding digest")
	}
	return nil
}

// SessionCreateMutationDigest returns the canonical request digest used by
// both the in-memory kernel and durable Session writers.
func SessionCreateMutationDigest(input CreateSessionInput) (string, error) {
	if err := input.Scope.validate(); err != nil {
		return "", err
	}
	if err := validateIdentifier(input.SessionID, maxIdentifierBytes, "session id"); err != nil {
		return "", err
	}
	if err := validateIdentifier(input.ProviderKind, maxProviderBytes, "provider kind"); err != nil {
		return "", err
	}
	if err := validateSessionBinding(input); err != nil {
		return "", err
	}
	if err := validateCapabilityRefs(input.McpServerRefs, input.SkillBundleRefs); err != nil {
		return "", err
	}
	if err := input.Mutation.validate(); err != nil {
		return "", err
	}
	return digestMutationWithBinding(input.Mutation, mutationDigestInput{
		Operation: "session.create", TenantID: input.Scope.TenantID, ProjectID: input.Scope.ProjectID,
		SessionID: input.SessionID, ProviderKind: input.ProviderKind, EnvironmentLeaseID: input.EnvironmentLeaseID,
		WorkspaceID: input.WorkspaceID, SandboxID: input.SandboxID, SandboxGeneration: input.SandboxGeneration,
		EnvironmentProfileID: input.EnvironmentProfileID, EnvironmentProfileVersion: input.EnvironmentProfileVersion,
		McpServerRefs: input.McpServerRefs, SkillBundleRefs: input.SkillBundleRefs,
	}), nil
}

func validateSessionBinding(input CreateSessionInput) error {
	legacy := input.EnvironmentLeaseID != "" && input.WorkspaceID == "" && input.SandboxID == "" && input.SandboxGeneration == 0 && input.EnvironmentProfileID == "" && input.EnvironmentProfileVersion == 0
	foundation := input.EnvironmentLeaseID == "" && input.WorkspaceID != "" && input.SandboxID != "" && input.SandboxGeneration > 0 && input.SandboxGeneration <= 9007199254740991 && input.EnvironmentProfileID != "" && input.EnvironmentProfileVersion > 0 && input.EnvironmentProfileVersion <= 2147483647
	if !legacy && !foundation {
		return fmt.Errorf("%w: environment binding", ErrInvalidInput)
	}
	for value, field := range map[string]string{
		input.EnvironmentLeaseID:   "environment lease id",
		input.WorkspaceID:          "workspace id",
		input.SandboxID:            "sandbox id",
		input.EnvironmentProfileID: "environment profile id",
	} {
		if value != "" {
			if err := validateIdentifier(value, maxIdentifierBytes, field); err != nil {
				return err
			}
		}
	}
	return nil
}

// SessionCloseMutationDigest returns the canonical request digest used by
// both the in-memory kernel and durable Session writers.
func SessionCloseMutationDigest(input CloseSessionInput) (string, error) {
	if err := input.Scope.validate(); err != nil {
		return "", err
	}
	if err := validateIdentifier(input.SessionID, maxIdentifierBytes, "session id"); err != nil {
		return "", err
	}
	if err := input.Mutation.validate(); err != nil {
		return "", err
	}
	return digestMutationWithBinding(input.Mutation, mutationDigestInput{
		Operation: "session.close", TenantID: input.Scope.TenantID, ProjectID: input.Scope.ProjectID,
		SessionID: input.SessionID,
	}), nil
}

// TurnCreateMutationDigest returns the canonical request digest used by the
// in-memory kernel and durable Turn writers.
func TurnCreateMutationDigest(input CreateTurnInput) (string, error) {
	if err := input.Scope.validate(); err != nil {
		return "", err
	}
	if err := validateIdentifier(input.SessionID, maxIdentifierBytes, "session id"); err != nil {
		return "", err
	}
	if err := validateIdentifier(input.TurnID, maxIdentifierBytes, "turn id"); err != nil {
		return "", err
	}
	inputDigest, err := TurnInputDigest(input.InputText)
	if err != nil {
		return "", err
	}
	if err := input.Mutation.validate(); err != nil {
		return "", err
	}
	return digestMutationWithBinding(input.Mutation, mutationDigestInput{
		Operation: "turn.create", TenantID: input.Scope.TenantID, ProjectID: input.Scope.ProjectID,
		SessionID: input.SessionID, TurnID: input.TurnID, InputDigest: inputDigest,
	}), nil
}

// TurnInputDigest returns the bounded content digest persisted with a Turn.
func TurnInputDigest(inputText string) (string, error) {
	return digestInput(inputText)
}

// ExecutionCreateMutationDigest returns the canonical digest used by the
// durable Execution create writer.
func ExecutionCreateMutationDigest(input CreateExecutionInput) (string, error) {
	if err := validateExecutionInput(input.Scope, input.SessionID, input.TurnID, input.ExecutionID, input.Generation); err != nil {
		return "", err
	}
	if err := input.Mutation.validate(); err != nil {
		return "", err
	}
	if err := validateCapabilityRefs(input.McpServerRefs, input.SkillBundleRefs); err != nil {
		return "", err
	}
	return digestMutationWithBinding(input.Mutation, mutationDigestInput{
		Operation: "execution.create", TenantID: input.Scope.TenantID, ProjectID: input.Scope.ProjectID,
		SessionID: input.SessionID, TurnID: input.TurnID, ExecutionID: input.ExecutionID, Generation: input.Generation,
		McpServerRefs: input.McpServerRefs, SkillBundleRefs: input.SkillBundleRefs,
	}), nil
}

// ExecutionStartMutationDigest returns the canonical digest used by the
// durable Execution start writer.
func ExecutionStartMutationDigest(input StartExecutionInput) (string, error) {
	if err := validateExecutionInput(input.Scope, input.SessionID, input.TurnID, input.ExecutionID, input.Generation); err != nil {
		return "", err
	}
	if err := input.Mutation.validate(); err != nil {
		return "", err
	}
	return digestMutationWithBinding(input.Mutation, mutationDigestInput{
		Operation: "execution.start", TenantID: input.Scope.TenantID, ProjectID: input.Scope.ProjectID,
		SessionID: input.SessionID, TurnID: input.TurnID, ExecutionID: input.ExecutionID, Generation: input.Generation,
	}), nil
}

// ExecutionCompleteMutationDigest returns the canonical digest used by the
// durable successful Execution settlement writer.
func ExecutionCompleteMutationDigest(input CompleteExecutionInput) (string, error) {
	return executionCompleteMutationDigest(input, "")
}

// RuntimeExecutionCompleteMutationDigest binds Provider-private continuation
// state to durable completion idempotency without exposing it publicly.
func RuntimeExecutionCompleteMutationDigest(input CompleteRuntimeExecutionInput) (string, error) {
	if err := validateRuntimeTerminalMessage(input); err != nil {
		return "", err
	}
	return executionCompleteMutationDigest(input.CompleteExecutionInput, input.ProviderResumeCursor)
}

func executionCompleteMutationDigest(input CompleteExecutionInput, providerResumeCursor string) (string, error) {
	if err := validateExecutionInput(input.Scope, input.SessionID, input.TurnID, input.ExecutionID, input.Generation); err != nil {
		return "", err
	}
	if err := validateDigest(input.ResultDigest, "result digest"); err != nil {
		return "", err
	}
	if err := ValidateProviderResumeCursor(providerResumeCursor); err != nil {
		return "", err
	}
	if err := input.Mutation.validate(); err != nil {
		return "", err
	}
	return digestMutationWithBinding(input.Mutation, mutationDigestInput{
		Operation: "execution.complete", TenantID: input.Scope.TenantID, ProjectID: input.Scope.ProjectID,
		SessionID: input.SessionID, TurnID: input.TurnID, ExecutionID: input.ExecutionID, Generation: input.Generation,
		ResultDigest: input.ResultDigest, ProviderResumeCursor: providerResumeCursor,
	}), nil
}

// ExecutionFailMutationDigest returns the canonical digest used by the
// durable failed Execution settlement writer.
func ExecutionFailMutationDigest(input FailExecutionInput) (string, error) {
	if err := validateExecutionInput(input.Scope, input.SessionID, input.TurnID, input.ExecutionID, input.Generation); err != nil {
		return "", err
	}
	if err := validateErrorCode(input.ErrorCode); err != nil {
		return "", err
	}
	if err := input.Mutation.validate(); err != nil {
		return "", err
	}
	return digestMutationWithBinding(input.Mutation, mutationDigestInput{
		Operation: "execution.fail", TenantID: input.Scope.TenantID, ProjectID: input.Scope.ProjectID,
		SessionID: input.SessionID, TurnID: input.TurnID, ExecutionID: input.ExecutionID, Generation: input.Generation,
		ErrorCode: input.ErrorCode,
	}), nil
}

// RuntimeExecutionFailMutationDigest validates the bounded Runtime transcript
// while retaining the existing durable failure idempotency digest.
func RuntimeExecutionFailMutationDigest(input FailRuntimeExecutionInput) (string, error) {
	if err := validateRuntimeFailureMessages(input); err != nil {
		return "", err
	}
	return ExecutionFailMutationDigest(input.FailExecutionInput)
}

// TurnCancelMutationDigest returns the canonical digest used by the durable
// caller-requested cancellation writer.
func TurnCancelMutationDigest(input CancelTurnInput) (string, error) {
	if err := validateExecutionInput(input.Scope, input.SessionID, input.TurnID, input.TargetExecutionID, input.Generation); err != nil {
		return "", err
	}
	if err := input.Mutation.validate(); err != nil {
		return "", err
	}
	return digestMutationWithBinding(input.Mutation, mutationDigestInput{
		Operation: "turn.cancel", TenantID: input.Scope.TenantID, ProjectID: input.Scope.ProjectID,
		SessionID: input.SessionID, TurnID: input.TurnID, ExecutionID: input.TargetExecutionID,
		Generation: input.Generation, TargetExecutionID: input.TargetExecutionID,
	}), nil
}

// TurnInterruptMutationDigest returns the canonical digest used by the durable
// caller-requested interruption writer.
func TurnInterruptMutationDigest(input InterruptTurnInput) (string, error) {
	if err := validateExecutionInput(input.Scope, input.SessionID, input.TurnID, input.TargetExecutionID, input.Generation); err != nil {
		return "", err
	}
	if err := input.Mutation.validate(); err != nil {
		return "", err
	}
	return digestMutationWithBinding(input.Mutation, mutationDigestInput{
		Operation: "turn.interrupt", TenantID: input.Scope.TenantID, ProjectID: input.Scope.ProjectID,
		SessionID: input.SessionID, TurnID: input.TurnID, ExecutionID: input.TargetExecutionID,
		Generation: input.Generation, TargetExecutionID: input.TargetExecutionID,
	}), nil
}

func validateExecutionInput(scope Scope, sessionID, turnID, executionID string, generation uint64) error {
	if err := validateExecutionPath(scope, sessionID, turnID, executionID); err != nil {
		return err
	}
	if generation == 0 {
		return fmt.Errorf("%w: generation", ErrInvalidInput)
	}
	return nil
}

func validateExecutionPath(scope Scope, sessionID, turnID, executionID string) error {
	if err := scope.validate(); err != nil {
		return err
	}
	if err := validateIdentifier(sessionID, maxIdentifierBytes, "session id"); err != nil {
		return err
	}
	if err := validateIdentifier(turnID, maxIdentifierBytes, "turn id"); err != nil {
		return err
	}
	return validateIdentifier(executionID, maxIdentifierBytes, "execution id")
}

func validateIdentifier(value string, maximum int, field string) error {
	if len(value) == 0 || len(value) > maximum || !utf8.ValidString(value) {
		return fmt.Errorf("%w: %s", ErrInvalidInput, field)
	}
	for index := 0; index < len(value); index++ {
		character := value[index]
		alphaNumeric := character >= 'a' && character <= 'z' || character >= 'A' && character <= 'Z' || character >= '0' && character <= '9'
		if index == 0 || index == len(value)-1 {
			if !alphaNumeric {
				return fmt.Errorf("%w: %s", ErrInvalidInput, field)
			}
			continue
		}
		if !alphaNumeric && character != '-' && character != '_' && character != '.' && character != '~' {
			return fmt.Errorf("%w: %s", ErrInvalidInput, field)
		}
	}
	return nil
}

func validateToken(value string, maximum int, field string) error {
	if len(value) == 0 || len(value) > maximum || !utf8.ValidString(value) {
		return fmt.Errorf("%w: %s", ErrInvalidInput, field)
	}
	for _, character := range value {
		if character < 0x20 || character == 0x7f {
			return fmt.Errorf("%w: %s", ErrInvalidInput, field)
		}
	}
	return nil
}

// ValidateProviderResumeCursor validates the opaque Runtime/Provider trust
// boundary. Empty means the Provider did not supply native continuation state.
func ValidateProviderResumeCursor(value string) error {
	if value == "" {
		return nil
	}
	if strings.Trim(value, " ") != value {
		return fmt.Errorf("%w: provider resume cursor", ErrInvalidInput)
	}
	return validateToken(value, maxProviderResumeCursorBytes, "provider resume cursor")
}

func digestInput(input string) (string, error) {
	if len(input) == 0 || len(input) > maxInputBytes || !utf8.ValidString(input) {
		return "", fmt.Errorf("%w: input text", ErrInvalidInput)
	}
	digest := sha256.Sum256([]byte(input))
	return "sha256:" + hex.EncodeToString(digest[:]), nil
}

func validateDigest(value, field string) error {
	if len(value) != len("sha256:")+64 || !strings.HasPrefix(value, "sha256:") {
		return fmt.Errorf("%w: %s", ErrInvalidInput, field)
	}
	for _, character := range value[len("sha256:"):] {
		if !(character >= '0' && character <= '9' || character >= 'a' && character <= 'f') {
			return fmt.Errorf("%w: %s", ErrInvalidInput, field)
		}
	}
	return nil
}

func validateCapabilityRefs(mcp []McpServerRef, skills []SkillBundleRef) error {
	if len(mcp) > 32 || len(skills) > 32 {
		return fmt.Errorf("%w: capability refs", ErrInvalidInput)
	}
	seen := make(map[string]struct{}, len(mcp)+len(skills))
	for index, ref := range mcp {
		if err := validateIdentifier(ref.ServerID, maxIdentifierBytes, fmt.Sprintf("mcp server ref %d id", index)); err != nil {
			return err
		}
		if err := validateIdentifier(ref.Version, maxIdentifierBytes, fmt.Sprintf("mcp server ref %d version", index)); err != nil {
			return err
		}
		if err := validateDigest(ref.Digest, fmt.Sprintf("mcp server ref %d digest", index)); err != nil {
			return err
		}
		key := "mcp\x00" + ref.ServerID + "\x00" + ref.Version + "\x00" + ref.Digest
		if _, ok := seen[key]; ok {
			return fmt.Errorf("%w: duplicate capability ref", ErrInvalidInput)
		}
		seen[key] = struct{}{}
	}
	for index, ref := range skills {
		if err := validateIdentifier(ref.BundleID, maxIdentifierBytes, fmt.Sprintf("skill bundle ref %d id", index)); err != nil {
			return err
		}
		if err := validateIdentifier(ref.Version, maxIdentifierBytes, fmt.Sprintf("skill bundle ref %d version", index)); err != nil {
			return err
		}
		if err := validateDigest(ref.Digest, fmt.Sprintf("skill bundle ref %d digest", index)); err != nil {
			return err
		}
		key := "skill\x00" + ref.BundleID + "\x00" + ref.Version + "\x00" + ref.Digest
		if _, ok := seen[key]; ok {
			return fmt.Errorf("%w: duplicate capability ref", ErrInvalidInput)
		}
		seen[key] = struct{}{}
	}
	return nil
}

// ValidateCapabilityRefs is the shared Control Plane/Runtime boundary check.
func ValidateCapabilityRefs(mcp []McpServerRef, skills []SkillBundleRef) error {
	return validateCapabilityRefs(mcp, skills)
}

func validateErrorCode(value string) error {
	if len(value) == 0 || len(value) > 64 {
		return fmt.Errorf("%w: error code", ErrInvalidInput)
	}
	for index := 0; index < len(value); index++ {
		character := value[index]
		if !(character >= 'a' && character <= 'z' || character >= '0' && character <= '9' || character == '_' || character == '-') {
			return fmt.Errorf("%w: error code", ErrInvalidInput)
		}
	}
	return nil
}

func validateContext(ctx context.Context) error {
	if ctx == nil {
		return ErrNilContext
	}
	return contextErr(ctx)
}

func contextErr(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return nil
}

func (snapshot TurnSnapshot) terminal() bool {
	return snapshot.State == TurnCompleted || snapshot.State == TurnFailed || snapshot.State == TurnInterrupted || snapshot.State == TurnCancelled
}

type mutationDigestInput struct {
	Operation                 string           `json:"operation"`
	TenantID                  string           `json:"tenant_id"`
	ProjectID                 string           `json:"project_id"`
	SessionID                 string           `json:"session_id,omitempty"`
	TurnID                    string           `json:"turn_id,omitempty"`
	ExecutionID               string           `json:"execution_id,omitempty"`
	TargetExecutionID         string           `json:"target_execution_id,omitempty"`
	ProviderKind              string           `json:"provider_kind,omitempty"`
	EnvironmentLeaseID        string           `json:"environment_lease_id,omitempty"`
	WorkspaceID               string           `json:"workspace_id,omitempty"`
	SandboxID                 string           `json:"sandbox_id,omitempty"`
	SandboxGeneration         uint64           `json:"sandbox_generation,omitempty"`
	EnvironmentProfileID      string           `json:"environment_profile_id,omitempty"`
	EnvironmentProfileVersion uint64           `json:"environment_profile_version,omitempty"`
	Generation                uint64           `json:"generation,omitempty"`
	InputDigest               string           `json:"input_digest,omitempty"`
	ResultDigest              string           `json:"result_digest,omitempty"`
	ProviderResumeCursor      string           `json:"provider_resume_cursor,omitempty"`
	ErrorCode                 string           `json:"error_code,omitempty"`
	McpServerRefs             []McpServerRef   `json:"mcp_server_refs,omitempty"`
	SkillBundleRefs           []SkillBundleRef `json:"skill_bundle_refs,omitempty"`
	ExecutionBindingDigest    string           `json:"execution_binding_digest,omitempty"`
}

func digestMutation(input mutationDigestInput) string {
	encoded, _ := json.Marshal(input)
	hash := sha256.Sum256(append([]byte(LifecycleProfileID+"/mutation\x00"), encoded...))
	return "sha256:" + hex.EncodeToString(hash[:])
}

func digestMutationWithBinding(mutation Mutation, input mutationDigestInput) string {
	input.ExecutionBindingDigest = mutation.executionBindingDigest
	return digestMutation(input)
}
