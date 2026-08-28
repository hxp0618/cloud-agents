package worker

import (
	"crypto/sha256"
	"fmt"
	"time"

	workerv1alpha1 "github.com/hxp0618/cloud-agents/sdk/go/gen/cloudagents/worker/v1alpha1"
	commonv1alpha1 "github.com/hxp0618/cloud-agents/sdk/go/gen/common/v1alpha1"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// LocalOperationAttemptInput is the typed, side-effect-free builder input for
// an in-process operation attempt. CanonicalRequestSha256 is intentionally
// absent: the builder computes it from the normalized operation authority.
type LocalOperationAttemptInput struct {
	OperationID              string
	IdempotencyKey           string
	Scope                    commonv1alpha1.NamespaceRef
	FencingLeaseID           string
	FencingGeneration        uint64
	FencingToken             []byte
	Deadline                 time.Time
	AttemptID                string
	AttemptNumber            uint32
	ExpectedExecutorIdentity *workerv1alpha1.WorkloadIdentity
	Negotiation              *workerv1alpha1.NegotiationBinding
	Command                  *workerv1alpha1.OperationCommand
	Now                      time.Time
}

// BuildLocalOperationAttempt constructs the exact v1 local operation attempt
// accepted by the admission kernel. It performs no I/O and rejects malformed
// or unsupported commands before returning a request.
func BuildLocalOperationAttempt(input LocalOperationAttemptInput) (*workerv1alpha1.OperationAttemptEnvelope, error) {
	if err := validateIdentifier(input.OperationID, "operation_id"); err != nil {
		return nil, fmt.Errorf("worker/local_attempt: %w", err)
	}
	if err := validateIdentifier(input.IdempotencyKey, "idempotency_key"); err != nil {
		return nil, fmt.Errorf("worker/local_attempt: %w", err)
	}
	if err := validateIdentifier(input.AttemptID, "attempt_id"); err != nil {
		return nil, fmt.Errorf("worker/local_attempt: %w", err)
	}
	if input.AttemptNumber == 0 {
		return nil, fmt.Errorf("worker/local_attempt: attempt_number is required")
	}
	if err := validateIdentity(input.ExpectedExecutorIdentity); err != nil {
		return nil, fmt.Errorf("worker/local_attempt: expected executor identity: %w", err)
	}
	if input.FencingGeneration == 0 || len(input.FencingToken) == 0 || len(input.FencingToken) > maxOperationTokenBytes {
		return nil, fmt.Errorf("worker/local_attempt: fencing proof is required")
	}
	if err := validateIdentifier(input.FencingLeaseID, "lease_id"); err != nil {
		return nil, fmt.Errorf("worker/local_attempt: %w", err)
	}
	if input.Deadline.IsZero() || input.Now.IsZero() {
		return nil, fmt.Errorf("worker/local_attempt: deadline and clock are required")
	}
	deadline := timestamppb.New(input.Deadline.UTC())
	if err := validateDeadline(deadline, input.Now.UTC()); err != nil {
		return nil, err
	}
	scope, normalizedScope, err := normalizeNamespaceRef(&workerv1alpha1.NamespaceRef{Namespace: input.Scope.Namespace, Kind: input.Scope.Kind, Id: input.Scope.ID})
	if err != nil {
		return nil, err
	}
	command, err := normalizeOperationCommand(input.Command)
	if err != nil {
		return nil, err
	}
	operation := &workerv1alpha1.OperationEnvelope{
		OperationId: input.OperationID, IdempotencyKey: input.IdempotencyKey,
		Scope:    normalizedScope,
		Fencing:  &workerv1alpha1.FencingProof{LeaseId: input.FencingLeaseID, Generation: input.FencingGeneration, Token: append([]byte(nil), input.FencingToken...)},
		Deadline: deadline, RequiredCapability: workerv1alpha1.Capability_CAPABILITY_OPERATION_DISPATCH,
		Command: command,
	}
	canonical, err := canonicalOperationEnvelope(operation, scope)
	if err != nil {
		return nil, err
	}
	sum := sha256Bytes(canonical)
	operation.CanonicalRequestSha256 = sum
	return &workerv1alpha1.OperationAttemptEnvelope{
		Operation:                operation,
		AttemptId:                input.AttemptID,
		AttemptNumber:            input.AttemptNumber,
		ExpectedExecutorIdentity: cloneIdentity(input.ExpectedExecutorIdentity),
		Negotiation:              cloneNegotiation(input.Negotiation),
	}, nil
}

func sha256Bytes(value []byte) []byte {
	sum := sha256.Sum256(value)
	return append([]byte(nil), sum[:]...)
}

func cloneNegotiation(value *workerv1alpha1.NegotiationBinding) *workerv1alpha1.NegotiationBinding {
	if value == nil {
		return nil
	}
	return proto.Clone(value).(*workerv1alpha1.NegotiationBinding)
}
