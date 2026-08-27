package worker

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"connectrpc.com/connect"
	connectv1alpha1 "github.com/hxp0618/cloud-agents/sdk/go/gen/cloudagents/worker/v1alpha1"
	commonv1alpha1 "github.com/hxp0618/cloud-agents/sdk/go/gen/common/v1alpha1"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/known/timestamppb"
)

const (
	// OperationAdmissionProfileID identifies this local, transport-neutral
	// admission seam. It is not an HTTP or durable-receipt version.
	OperationAdmissionProfileID = "cloud-agents/worker-operation-admission/v1alpha1"
	operationAdmissionAlgorithm = "rfc8785-operation-envelope-v1"
	// Fencing proofs use the same hard byte ceiling as extension payloads. The
	// wire ceiling still applies to the complete decoded envelope.
	maxOperationTokenBytes = int(MaxPayloadBytes)
	maxAdmissionRecords    = 1024
)

var finalizerNamePattern = regexp.MustCompile(`^[a-z][a-z0-9]*(?:[.-][a-z0-9]+)*$`)

// OperationAdmissionProfile is the immutable local profile consumed by the
// admission kernel. Execution, receipt persistence, and provider dispatch are
// deliberately outside this profile.
type OperationAdmissionProfile struct {
	ID                   string
	Canonicalization     string
	DigestAlgorithm      string
	MaxFinalizers        uint32
	MaxIdentifierBytes   uint32
	MaxPayloadBytes      uint32
	MaxDeadlineSeconds   uint32
	MaxFencingTokenBytes uint32
	MaxAdmissionRecords  uint32
	ExternalSideEffects  bool
}

// WorkerOperationAdmissionProfile returns the frozen profile metadata.
func WorkerOperationAdmissionProfile() OperationAdmissionProfile {
	return OperationAdmissionProfile{
		ID:                   OperationAdmissionProfileID,
		Canonicalization:     operationAdmissionAlgorithm,
		DigestAlgorithm:      "SHA-256",
		MaxFinalizers:        MaxRepeatedItems,
		MaxIdentifierBytes:   MaxIdentifierBytes,
		MaxPayloadBytes:      MaxPayloadBytes,
		MaxDeadlineSeconds:   MaxDeadlineSeconds,
		MaxFencingTokenBytes: uint32(maxOperationTokenBytes),
		MaxAdmissionRecords:  uint32(maxAdmissionRecords),
		ExternalSideEffects:  false,
	}
}

func (profile OperationAdmissionProfile) Valid() bool {
	return profile.ID == OperationAdmissionProfileID &&
		profile.Canonicalization == operationAdmissionAlgorithm &&
		profile.DigestAlgorithm == "SHA-256" &&
		profile.MaxFinalizers == MaxRepeatedItems &&
		profile.MaxIdentifierBytes == MaxIdentifierBytes &&
		profile.MaxPayloadBytes == MaxPayloadBytes &&
		profile.MaxDeadlineSeconds == MaxDeadlineSeconds &&
		profile.MaxFencingTokenBytes == uint32(maxOperationTokenBytes) &&
		profile.MaxAdmissionRecords == uint32(maxAdmissionRecords) &&
		!profile.ExternalSideEffects
}

// AdmissionClaim is an opaque, in-memory result of successful operation
// admission. It contains only normalized references and digests; the raw
// fencing token, command payload, and any secret-bearing value are never
// retained. A claim does not authorize execution or receipt issuance.
type AdmissionClaim struct {
	profileID       string
	operationID     string
	attemptID       string
	attemptNumber   uint32
	scope           commonv1alpha1.NamespaceRef
	leaseID         string
	generation      uint64
	fencingDigest   string
	canonicalDigest string
	admittedAt      time.Time
	replayed        bool
}

func (claim *AdmissionClaim) valid() bool {
	return claim != nil &&
		claim.profileID == OperationAdmissionProfileID &&
		claim.operationID != "" && claim.attemptID != "" && claim.attemptNumber > 0 &&
		claim.leaseID != "" && claim.generation > 0 && claim.fencingDigest != "" &&
		claim.canonicalDigest != "" && claim.scope.Validate() == nil
}

// ProfileID returns the bound profile identity. Invalid or copied claims
// return an empty value rather than exposing mutable authority.
func (claim *AdmissionClaim) ProfileID() string {
	if !claim.valid() {
		return ""
	}
	return claim.profileID
}

func (claim *AdmissionClaim) OperationID() string {
	if !claim.valid() {
		return ""
	}
	return claim.operationID
}

func (claim *AdmissionClaim) AttemptID() string {
	if !claim.valid() {
		return ""
	}
	return claim.attemptID
}

func (claim *AdmissionClaim) AttemptNumber() uint32 {
	if !claim.valid() {
		return 0
	}
	return claim.attemptNumber
}

func (claim *AdmissionClaim) Scope() (commonv1alpha1.NamespaceRef, bool) {
	if !claim.valid() {
		return commonv1alpha1.NamespaceRef{}, false
	}
	return claim.scope, true
}

func (claim *AdmissionClaim) LeaseID() string {
	if !claim.valid() {
		return ""
	}
	return claim.leaseID
}

func (claim *AdmissionClaim) Generation() uint64 {
	if !claim.valid() {
		return 0
	}
	return claim.generation
}

func (claim *AdmissionClaim) FencingTokenDigest() string {
	if !claim.valid() {
		return ""
	}
	return claim.fencingDigest
}

func (claim *AdmissionClaim) CanonicalRequestDigest() string {
	if !claim.valid() {
		return ""
	}
	return claim.canonicalDigest
}

func (claim *AdmissionClaim) AdmittedAt() time.Time {
	if !claim.valid() {
		return time.Time{}
	}
	return claim.admittedAt
}

func (claim *AdmissionClaim) Replayed() bool {
	return claim.valid() && claim.replayed
}

type admissionRecord struct {
	claim          AdmissionClaim
	idempotencyKey string
}

// AdmitOperation validates and records one in-memory operation admission. It
// performs no execution, receipt write, workspace/credential access, network
// call, or provider invocation. A repeated exact attempt returns a detached
// replay claim; later attempts for the same immutable operation must retain
// its idempotency/canonical/lease identity and use a strictly greater attempt
// number. Any changed operation or idempotency identity fails closed.
func (s *Service) AdmitOperation(ctx context.Context, req *connectv1alpha1.OperationAttemptEnvelope) (*AdmissionClaim, error) {
	if s == nil || s.identity == nil || s.now == nil {
		return nil, admissionFailure("admission_unavailable", "operation admission authority is not configured", connect.CodeFailedPrecondition)
	}
	if err := contextErr(ctx); err != nil {
		return nil, err
	}
	if req == nil {
		return nil, admissionFailure("invalid_request", "operation attempt is required", connect.CodeInvalidArgument)
	}
	// Clone before validation so a caller cannot mutate a request while this
	// method is deriving its digest or committing the in-memory record.
	message, ok := proto.Clone(req).(*connectv1alpha1.OperationAttemptEnvelope)
	if !ok || message == nil {
		return nil, admissionFailure("invalid_request", "operation attempt is invalid", connect.CodeInvalidArgument)
	}
	if proto.Size(message) > int(MaxWireMessageBytes) {
		return nil, admissionFailure("wire_message_too_large", "operation attempt exceeds the hard wire limit", connect.CodeInvalidArgument)
	}
	if err := rejectUnknownFields(message); err != nil {
		return nil, err
	}
	if err := contextErr(ctx); err != nil {
		return nil, err
	}
	if message.GetAttemptNumber() == 0 {
		return nil, admissionFailure("attempt_number_required", "attempt number must be positive", connect.CodeInvalidArgument)
	}
	if err := validateIdentifier(message.GetAttemptId(), "attempt_id"); err != nil {
		return nil, admissionFailure("attempt_id_invalid", "attempt id is invalid", connect.CodeInvalidArgument)
	}
	operation := message.GetOperation()
	if operation == nil {
		return nil, admissionFailure("operation_required", "operation is required", connect.CodeInvalidArgument)
	}
	if err := rejectUnknownFields(operation); err != nil {
		return nil, err
	}
	if err := validateIdentifier(operation.GetOperationId(), "operation_id"); err != nil {
		return nil, admissionFailure("operation_id_required", "operation id is invalid", connect.CodeInvalidArgument)
	}
	if err := validateIdentifier(operation.GetIdempotencyKey(), "idempotency_key"); err != nil {
		return nil, admissionFailure("idempotency_key_invalid", "idempotency key is invalid", connect.CodeInvalidArgument)
	}
	if operation.GetRequiredCapability() != connectv1alpha1.Capability_CAPABILITY_OPERATION_DISPATCH {
		return nil, admissionFailure("required_capability_invalid", "operation dispatch capability is required", connect.CodeFailedPrecondition)
	}
	if message.GetExpectedExecutorIdentity() == nil {
		return nil, admissionFailure("executor_identity_required", "expected executor identity is required", connect.CodeInvalidArgument)
	}
	if err := validateExpectedExecutorIdentity(message.GetExpectedExecutorIdentity(), s.workerIdentity); err != nil {
		return nil, err
	}
	client, err := s.identity.ClientIdentity(ctx)
	if err != nil || client == nil {
		return nil, admissionFailure("transport_identity_missing", "authenticated client identity is required", connect.CodeUnauthenticated)
	}
	if err := validateIdentity(client); err != nil {
		return nil, admissionFailure("invalid_transport_identity", "authenticated client identity is invalid", connect.CodeUnauthenticated)
	}
	binding, err := s.validateBinding(message.GetNegotiation(), client)
	if err != nil {
		return nil, err
	}
	if _, negotiated := binding.caps[connectv1alpha1.Capability_CAPABILITY_OPERATION_DISPATCH]; !negotiated {
		return nil, admissionFailure("capability_not_negotiated", "operation dispatch capability was not negotiated", connect.CodeFailedPrecondition)
	}
	if s.admissionLeaseID == "" || s.admissionGeneration == 0 {
		return nil, admissionFailure("generation_authority_missing", "operation generation authority is not configured", connect.CodeFailedPrecondition)
	}
	// Use one authoritative instant for the entire admission decision. A clock
	// that advances between validation and recording must not make a request
	// appear both valid and expired within one call.
	now := s.now().UTC()
	fencing := operation.GetFencing()
	if fencing == nil {
		return nil, admissionFailure("fencing_required", "fencing proof is required", connect.CodeInvalidArgument)
	}
	if err := rejectUnknownFields(fencing); err != nil {
		return nil, err
	}
	if err := validateIdentifier(fencing.GetLeaseId(), "lease_id"); err != nil {
		return nil, admissionFailure("lease_id_invalid", "lease id is invalid", connect.CodeInvalidArgument)
	}
	if fencing.GetLeaseId() != s.admissionLeaseID {
		return nil, admissionFailure("lease_mismatch", "fencing lease does not match the bound authority", connect.CodePermissionDenied)
	}
	if fencing.GetGeneration() != s.admissionGeneration {
		return nil, admissionFailure("stale_generation", "fencing generation does not match the bound authority", connect.CodeFailedPrecondition)
	}
	if tokenBytes := fencing.GetToken(); len(tokenBytes) == 0 || len(tokenBytes) > maxOperationTokenBytes {
		return nil, admissionFailure("fencing_token_invalid", "fencing token is invalid", connect.CodeInvalidArgument)
	}
	scope, normalizedScope, err := normalizeNamespaceRef(operation.GetScope())
	if err != nil {
		return nil, err
	}
	operation.Scope = normalizedScope
	if err := validateDeadline(operation.GetDeadline(), now); err != nil {
		return nil, err
	}
	command, err := normalizeOperationCommand(operation.GetCommand())
	if err != nil {
		return nil, err
	}
	operation.Command = command
	finalizers, err := validateFinalizers(operation.GetFinalizers())
	if err != nil {
		return nil, err
	}
	operation.Finalizers = finalizers
	canonical, err := canonicalOperationEnvelope(operation, scope)
	if err != nil {
		return nil, err
	}
	canonicalSum := sha256.Sum256(canonical)
	if len(operation.GetCanonicalRequestSha256()) == 0 {
		return nil, admissionFailure("canonical_request_digest_required", "canonical request digest is required", connect.CodeInvalidArgument)
	}
	if len(operation.GetCanonicalRequestSha256()) != sha256.Size {
		return nil, admissionFailure("canonical_request_digest_invalid", "canonical request digest must be 32 bytes", connect.CodeInvalidArgument)
	}
	if !bytes.Equal(operation.GetCanonicalRequestSha256(), canonicalSum[:]) {
		return nil, admissionFailure("canonical_request_digest_mismatch", "canonical request digest does not match", connect.CodeInvalidArgument)
	}
	fencingSum := sha256.Sum256(fencing.GetToken())
	canonicalDigest := "sha256:" + hex.EncodeToString(canonicalSum[:])
	fencingDigest := "sha256:" + hex.EncodeToString(fencingSum[:])
	if err := contextErr(ctx); err != nil {
		return nil, err
	}
	base := AdmissionClaim{
		profileID:       OperationAdmissionProfileID,
		operationID:     operation.GetOperationId(),
		attemptID:       message.GetAttemptId(),
		attemptNumber:   message.GetAttemptNumber(),
		scope:           scope,
		leaseID:         fencing.GetLeaseId(),
		generation:      fencing.GetGeneration(),
		fencingDigest:   fencingDigest,
		canonicalDigest: canonicalDigest,
		admittedAt:      now,
	}
	if !base.valid() {
		return nil, admissionFailure("claim_invalid", "admission claim could not be constructed", connect.CodeInternal)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.admissions == nil {
		s.admissions = make(map[string]admissionRecord)
	}
	recordKey := admissionRecordKey(base.operationID, base.attemptID)
	var operationSeen bool
	var highestAttempt uint32
	for key, existing := range s.admissions {
		// Test-only or legacy records with an empty claim cannot establish an
		// operation identity, but still count toward the bounded capacity.
		if existing.claim.operationID == base.operationID {
			operationSeen = true
			if existing.idempotencyKey != operation.GetIdempotencyKey() ||
				existing.claim.canonicalDigest != base.canonicalDigest ||
				existing.claim.leaseID != base.leaseID ||
				existing.claim.generation != base.generation {
				return nil, admissionFailure("idempotency_conflict", "operation identity conflicts with an existing admission", connect.CodeAlreadyExists)
			}
			if key == recordKey {
				if existing.claim.attemptNumber != base.attemptNumber || existing.claim.fencingDigest != base.fencingDigest {
					return nil, admissionFailure("idempotency_conflict", "attempt identity conflicts with an existing admission", connect.CodeAlreadyExists)
				}
				replay := cloneAdmissionClaim(existing.claim)
				replay.replayed = true
				return replay, nil
			}
			if existing.claim.attemptNumber >= highestAttempt {
				highestAttempt = existing.claim.attemptNumber
			}
		}
		if existing.idempotencyKey == operation.GetIdempotencyKey() && existing.claim.operationID != "" && existing.claim.operationID != base.operationID {
			return nil, admissionFailure("idempotency_conflict", "idempotency key conflicts with an existing admission", connect.CodeAlreadyExists)
		}
	}
	if operationSeen && base.attemptNumber <= highestAttempt {
		return nil, admissionFailure("attempt_number_not_monotonic", "attempt number must increase for an existing operation", connect.CodeFailedPrecondition)
	}
	if len(s.admissions) >= maxAdmissionRecords {
		return nil, admissionFailure("admission_capacity_exceeded", "in-memory admission capacity is exhausted", connect.CodeResourceExhausted)
	}
	s.admissions[recordKey] = admissionRecord{claim: base, idempotencyKey: operation.GetIdempotencyKey()}
	return cloneAdmissionClaim(base), nil
}

func admissionRecordKey(operationID, attemptID string) string {
	// Identifiers reject control characters, so a NUL separator cannot collide
	// with either component and keeps the composite key allocation-free beyond
	// the concatenated identifiers.
	return operationID + "\x00" + attemptID
}

func cloneAdmissionClaim(source AdmissionClaim) *AdmissionClaim {
	clone := source
	clone.scope = commonv1alpha1.NamespaceRef{
		Namespace: source.scope.Namespace,
		Kind:      source.scope.Kind,
		ID:        source.scope.ID,
	}
	return &clone
}

func validateExpectedExecutorIdentity(expected, actual *connectv1alpha1.WorkloadIdentity) error {
	if err := validateIdentity(expected); err != nil {
		return admissionFailure("invalid_expected_executor_identity", "expected executor identity is invalid", connect.CodeInvalidArgument)
	}
	if !sameIdentity(expected, actual) {
		return admissionFailure("executor_identity_mismatch", "expected executor identity does not match the Worker", connect.CodePermissionDenied)
	}
	return nil
}

func validateIdentifier(value, label string) error {
	if value == "" || !utf8.ValidString(value) || len(value) > int(MaxIdentifierBytes) {
		return fmt.Errorf("%s is empty, invalid UTF-8, or overlong", label)
	}
	for _, r := range value {
		if r < 0x20 || r == 0x7f {
			return fmt.Errorf("%s contains control characters", label)
		}
	}
	return nil
}

func normalizeNamespaceRef(value *connectv1alpha1.NamespaceRef) (commonv1alpha1.NamespaceRef, *connectv1alpha1.NamespaceRef, error) {
	if value == nil {
		return commonv1alpha1.NamespaceRef{}, nil, admissionFailure("scope_required", "operation scope is required", connect.CodeInvalidArgument)
	}
	normalized, err := commonv1alpha1.NormalizeNamespaceRef(commonv1alpha1.NamespaceRef{
		Namespace: value.GetNamespace(),
		Kind:      value.GetKind(),
		ID:        value.GetId(),
	})
	if err != nil {
		return commonv1alpha1.NamespaceRef{}, nil, admissionFailure("invalid_scope", "operation scope is invalid", connect.CodeInvalidArgument)
	}
	return normalized, &connectv1alpha1.NamespaceRef{Namespace: normalized.Namespace, Kind: normalized.Kind, Id: normalized.ID}, nil
}

func validateDeadline(value *timestamppb.Timestamp, now time.Time) error {
	if value == nil || value.CheckValid() != nil {
		return admissionFailure("deadline_required", "a valid deadline is required", connect.CodeInvalidArgument)
	}
	deadline := value.AsTime().UTC()
	if !deadline.After(now) {
		return admissionFailure("deadline_exceeded", "operation deadline has passed", connect.CodeDeadlineExceeded)
	}
	if deadline.After(now.Add(time.Duration(MaxDeadlineSeconds) * time.Second)) {
		return admissionFailure("deadline_horizon_exceeded", "operation deadline exceeds the negotiated horizon", connect.CodeInvalidArgument)
	}
	return nil
}

func normalizeOperationCommand(value *connectv1alpha1.OperationCommand) (*connectv1alpha1.OperationCommand, error) {
	if value == nil {
		return nil, admissionFailure("command_required", "operation command is required", connect.CodeInvalidArgument)
	}
	if value.GetExtensionPayload() != nil {
		if err := validateBoundedPayload(value.GetExtensionPayload()); err != nil {
			return nil, err
		}
		return nil, admissionFailure("extension_payload_not_implemented", "extension payload admission is not implemented", connect.CodeUnimplemented)
	}
	out := proto.Clone(value).(*connectv1alpha1.OperationCommand)
	switch command := out.GetCommand().(type) {
	case *connectv1alpha1.OperationCommand_Probe:
		if command == nil || command.Probe == nil || command.Probe.GetProbeName() == "" || !validBoundedText(command.Probe.GetProbeName(), MaxStringBytes) {
			return nil, admissionFailure("probe_invalid", "probe command is invalid", connect.CodeInvalidArgument)
		}
	case *connectv1alpha1.OperationCommand_ValidateBinding:
		if command == nil || command.ValidateBinding == nil {
			return nil, admissionFailure("validate_binding_invalid", "validate-binding command is invalid", connect.CodeInvalidArgument)
		}
		_, normalized, err := normalizeNamespaceRef(command.ValidateBinding.GetBinding())
		if err != nil {
			return nil, err
		}
		command.ValidateBinding.Binding = normalized
	default:
		return nil, admissionFailure("command_invalid", "exactly one supported operation command is required", connect.CodeInvalidArgument)
	}
	return out, nil
}

func validateBoundedPayload(payload *connectv1alpha1.BoundedPayload) error {
	if payload == nil {
		return nil
	}
	if !validBoundedText(payload.GetMediaType(), 128) {
		return admissionFailure("payload_media_type_invalid", "extension payload media type is invalid", connect.CodeInvalidArgument)
	}
	if len(payload.GetData()) > int(MaxPayloadBytes) || payload.GetDeclaredSizeBytes() > MaxPayloadBytes || int(payload.GetDeclaredSizeBytes()) != len(payload.GetData()) {
		return admissionFailure("payload_too_large", "extension payload exceeds the profile bound", connect.CodeInvalidArgument)
	}
	if len(payload.GetSha256()) != sha256.Size {
		return admissionFailure("payload_digest_invalid", "extension payload digest must be 32 bytes", connect.CodeInvalidArgument)
	}
	digest := sha256.Sum256(payload.GetData())
	if !bytes.Equal(payload.GetSha256(), digest[:]) {
		return admissionFailure("payload_digest_mismatch", "extension payload digest does not match", connect.CodeInvalidArgument)
	}
	return nil
}

func validateFinalizers(values []*connectv1alpha1.FinalizerSpec) ([]*connectv1alpha1.FinalizerSpec, error) {
	if len(values) > int(MaxRepeatedItems) {
		return nil, admissionFailure("too_many_finalizers", "finalizer count exceeds the profile limit", connect.CodeInvalidArgument)
	}
	seenNames := make(map[string]struct{}, len(values))
	seenKeys := make(map[string]struct{}, len(values))
	result := make([]*connectv1alpha1.FinalizerSpec, len(values))
	for index, value := range values {
		if value == nil || !validBoundedText(value.GetName(), MaxStringBytes) || !finalizerNamePattern.MatchString(value.GetName()) {
			return nil, admissionFailure("finalizer_invalid", "finalizer name is invalid", connect.CodeInvalidArgument)
		}
		if err := validateIdentifier(value.GetIdempotencyKey(), "finalizer idempotency key"); err != nil {
			return nil, admissionFailure("finalizer_invalid", "finalizer idempotency key is invalid", connect.CodeInvalidArgument)
		}
		if _, duplicate := seenNames[value.GetName()]; duplicate {
			return nil, admissionFailure("duplicate_finalizer", "finalizer names must be unique", connect.CodeInvalidArgument)
		}
		if _, duplicate := seenKeys[value.GetIdempotencyKey()]; duplicate {
			return nil, admissionFailure("duplicate_finalizer", "finalizer idempotency keys must be unique", connect.CodeInvalidArgument)
		}
		seenNames[value.GetName()] = struct{}{}
		seenKeys[value.GetIdempotencyKey()] = struct{}{}
		result[index] = proto.Clone(value).(*connectv1alpha1.FinalizerSpec)
	}
	return result, nil
}

func validBoundedText(value string, maximum uint32) bool {
	if value == "" || !utf8.ValidString(value) || len(value) > int(maximum) {
		return false
	}
	for _, r := range value {
		if r < 0x20 || r == 0x7f {
			return false
		}
	}
	return true
}

// canonicalOperationEnvelope writes the exact RFC 8785 projection declared by
// contracts/worker/v1alpha1/README.md. All numeric values are represented as
// decimal strings and every object is emitted in lexicographic member order.
func canonicalOperationEnvelope(operation *connectv1alpha1.OperationEnvelope, scope commonv1alpha1.NamespaceRef) ([]byte, error) {
	if operation == nil || operation.GetDeadline() == nil || operation.GetFencing() == nil || operation.GetCommand() == nil {
		return nil, admissionFailure("canonicalization_invalid", "operation is incomplete", connect.CodeInvalidArgument)
	}
	var out strings.Builder
	out.WriteString(`{"command":`)
	appendCanonicalCommand(&out, operation.GetCommand())
	out.WriteString(`,"deadline":`)
	appendJSONString(&out, operation.GetDeadline().AsTime().UTC().Format(time.RFC3339Nano))
	out.WriteString(`,"fencing":{"generation":`)
	appendJSONString(&out, strconv.FormatUint(operation.GetFencing().GetGeneration(), 10))
	out.WriteString(`,"leaseId":`)
	appendJSONString(&out, operation.GetFencing().GetLeaseId())
	out.WriteString(`},"finalizers":[`)
	for index, finalizer := range operation.GetFinalizers() {
		if index > 0 {
			out.WriteByte(',')
		}
		out.WriteString(`{"idempotencyKey":`)
		appendJSONString(&out, finalizer.GetIdempotencyKey())
		out.WriteString(`,"name":`)
		appendJSONString(&out, finalizer.GetName())
		out.WriteByte('}')
	}
	out.WriteString(`],"idempotencyKey":`)
	appendJSONString(&out, operation.GetIdempotencyKey())
	out.WriteString(`,"operationId":`)
	appendJSONString(&out, operation.GetOperationId())
	out.WriteString(`,"requiredCapability":`)
	appendJSONString(&out, operation.GetRequiredCapability().String())
	out.WriteString(`,"scope":{"id":`)
	appendJSONString(&out, scope.ID)
	out.WriteString(`,"kind":`)
	appendJSONString(&out, scope.Kind)
	out.WriteString(`,"namespace":`)
	appendJSONString(&out, scope.Namespace)
	out.WriteString(`}}`)
	return []byte(out.String()), nil
}

func appendCanonicalCommand(out *strings.Builder, command *connectv1alpha1.OperationCommand) {
	switch value := command.GetCommand().(type) {
	case *connectv1alpha1.OperationCommand_Probe:
		out.WriteString(`{"probe":{"probeName":`)
		appendJSONString(out, value.Probe.GetProbeName())
		out.WriteString(`}}`)
	case *connectv1alpha1.OperationCommand_ValidateBinding:
		binding := value.ValidateBinding.GetBinding()
		normalized := commonv1alpha1.NamespaceRef{Namespace: binding.GetNamespace(), Kind: binding.GetKind(), ID: binding.GetId()}
		out.WriteString(`{"validateBinding":{"binding":{"id":`)
		appendJSONString(out, normalized.ID)
		out.WriteString(`,"kind":`)
		appendJSONString(out, normalized.Kind)
		out.WriteString(`,"namespace":`)
		appendJSONString(out, normalized.Namespace)
		out.WriteString(`}}}`)
	}
}

func appendJSONString(out *strings.Builder, value string) {
	const hexadecimal = "0123456789abcdef"
	out.WriteByte('"')
	for index := 0; index < len(value); index++ {
		character := value[index]
		switch character {
		case '"', '\\':
			out.WriteByte('\\')
			out.WriteByte(character)
		case '\b':
			out.WriteString(`\b`)
		case '\t':
			out.WriteString(`\t`)
		case '\n':
			out.WriteString(`\n`)
		case '\f':
			out.WriteString(`\f`)
		case '\r':
			out.WriteString(`\r`)
		default:
			if character < 0x20 {
				out.WriteString(`\u00`)
				out.WriteByte(hexadecimal[character>>4])
				out.WriteByte(hexadecimal[character&0x0f])
			} else {
				out.WriteByte(character)
			}
		}
	}
	out.WriteByte('"')
}

func rejectUnknownFields(message proto.Message) error {
	if message == nil {
		return nil
	}
	var walk func(protoreflect.Message) bool
	walk = func(current protoreflect.Message) bool {
		if !current.IsValid() {
			return true
		}
		if len(current.GetUnknown()) > 0 {
			return false
		}
		valid := true
		current.Range(func(field protoreflect.FieldDescriptor, value protoreflect.Value) bool {
			if field.Kind() != protoreflect.MessageKind && field.Kind() != protoreflect.GroupKind {
				return true
			}
			if field.IsList() {
				list := value.List()
				for index := 0; index < list.Len(); index++ {
					if !walk(list.Get(index).Message()) {
						valid = false
						return false
					}
				}
				return true
			}
			if !walk(value.Message()) {
				valid = false
				return false
			}
			return true
		})
		return valid
	}
	if !walk(message.ProtoReflect()) {
		return admissionFailure("unknown_fields", "unknown protobuf fields are not accepted", connect.CodeInvalidArgument)
	}
	return nil
}

func admissionFailure(stable, text string, code connect.Code) error {
	return connect.NewError(code, fmt.Errorf("worker/%s: %s", stable, text))
}
