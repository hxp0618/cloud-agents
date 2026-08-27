package worker

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"reflect"
	"strings"
	"time"
	"unicode/utf8"

	"connectrpc.com/connect"
	workerv1alpha1 "github.com/hxp0618/cloud-agents/sdk/go/gen/cloudagents/worker/v1alpha1"
	commonv1alpha1 "github.com/hxp0618/cloud-agents/sdk/go/gen/common/v1alpha1"
	"google.golang.org/protobuf/proto"
)

// OperationExecutionProfileID identifies the process-local execution seam.
// It intentionally does not authorize PostgreSQL, HTTP, provider, workspace,
// credential, artifact, deployment, or production Runner effects.
const OperationExecutionProfileID = "cloud-agents/worker-operation-execution/localdev-v1alpha1"

const maxReceiptRecords = 1024

// OperationExecutionInput is the only input exposed to a local executor. Raw
// fencing tokens and transport credentials are excluded by construction.
type OperationExecutionInput struct {
	Claim    *AdmissionClaim
	Scope    commonv1alpha1.NamespaceRef
	Command  *workerv1alpha1.OperationCommand
	Deadline time.Time
}

// OperationExecutionResult describes one terminal, in-memory result. Results
// are detached from any durable receipt or external resource.
type OperationExecutionResult struct {
	Outcome         workerv1alpha1.OperationOutcome
	Finalizers      []*workerv1alpha1.FinalizerReceipt
	Results         []*workerv1alpha1.ResultReference
	StableErrorCode string
	RedactedSummary string
}

// OperationExecutor is injected by local tests or a future explicitly
// authorized adapter. Implementations must be deterministic and side-effect
// free for this profile.
type OperationExecutor interface {
	Execute(context.Context, OperationExecutionInput) (OperationExecutionResult, error)
}

func executorAvailable(executor OperationExecutor) bool {
	if executor == nil {
		return false
	}
	v := reflect.ValueOf(executor)
	switch v.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Ptr, reflect.Slice:
		return !v.IsNil()
	default:
		return true
	}
}

// DeterministicLocalExecutor handles only the two currently admitted command
// variants. It performs no I/O and is suitable for local focused checks.
type DeterministicLocalExecutor struct{}

func (DeterministicLocalExecutor) Execute(ctx context.Context, input OperationExecutionInput) (OperationExecutionResult, error) {
	if err := contextErr(ctx); err != nil {
		return OperationExecutionResult{}, err
	}
	if input.Claim == nil || input.Claim.ProfileID() != OperationAdmissionProfileID || input.Command == nil {
		return OperationExecutionResult{}, errors.New("invalid execution input")
	}
	var summary string
	switch command := input.Command.GetCommand().(type) {
	case *workerv1alpha1.OperationCommand_Probe:
		summary = "local probe completed"
	case *workerv1alpha1.OperationCommand_ValidateBinding:
		binding := command.ValidateBinding.GetBinding()
		_ = binding
		summary = "local binding validated"
	default:
		return OperationExecutionResult{}, errors.New("unsupported operation command")
	}
	return OperationExecutionResult{Outcome: workerv1alpha1.OperationOutcome_OPERATION_OUTCOME_SUCCEEDED, RedactedSummary: summary}, nil
}

type receiptRecord struct {
	receipt *workerv1alpha1.DurableReceipt
	claim   AdmissionClaim
	client  *workerv1alpha1.WorkloadIdentity
}

func validateExecutionResult(result OperationExecutionResult) error {
	switch result.Outcome {
	case workerv1alpha1.OperationOutcome_OPERATION_OUTCOME_SUCCEEDED,
		workerv1alpha1.OperationOutcome_OPERATION_OUTCOME_FAILED,
		workerv1alpha1.OperationOutcome_OPERATION_OUTCOME_CANCELLED,
		workerv1alpha1.OperationOutcome_OPERATION_OUTCOME_DEADLINE_EXCEEDED,
		workerv1alpha1.OperationOutcome_OPERATION_OUTCOME_FENCED:
	default:
		return fail(connect.CodeInternal, "execution_result_invalid", "executor must return one terminal outcome")
	}
	if len(result.Finalizers) > int(MaxRepeatedItems) || len(result.Results) > int(MaxRepeatedItems) {
		return fail(connect.CodeInternal, "execution_result_invalid", "executor result exceeds item limit")
	}
	if !validRedactedSummary(result.RedactedSummary) {
		return fail(connect.CodeInternal, "execution_result_invalid", "executor summary is invalid")
	}
	if result.StableErrorCode != "" && !validBoundedText(result.StableErrorCode, MaxIdentifierBytes) {
		return fail(connect.CodeInternal, "execution_result_invalid", "executor stable error code is invalid")
	}
	for _, finalizer := range result.Finalizers {
		if finalizer == nil || !validBoundedText(finalizer.GetName(), MaxStringBytes) || !finalizerNamePattern.MatchString(finalizer.GetName()) || !validBoundedText(finalizer.GetIdempotencyKey(), MaxIdentifierBytes) {
			return fail(connect.CodeInternal, "execution_result_invalid", "executor finalizer is invalid")
		}
		if finalizer.GetState() != workerv1alpha1.FinalizerState_FINALIZER_STATE_COMPLETED && finalizer.GetState() != workerv1alpha1.FinalizerState_FINALIZER_STATE_FAILED && finalizer.GetState() != workerv1alpha1.FinalizerState_FINALIZER_STATE_PENDING {
			return fail(connect.CodeInternal, "execution_result_invalid", "executor finalizer state is invalid")
		}
		if err := rejectUnknownFields(finalizer); err != nil {
			return fail(connect.CodeInternal, "execution_result_invalid", "executor finalizer contains unknown fields")
		}
	}
	for _, resultRef := range result.Results {
		resource := resultRef.GetResource()
		if resource == nil || len(resultRef.GetSha256()) != 64 {
			return fail(connect.CodeInternal, "execution_result_invalid", "executor result reference is invalid")
		}
		if _, err := commonv1alpha1.NormalizeNamespaceRef(commonv1alpha1.NamespaceRef{Namespace: resource.GetNamespace(), Kind: resource.GetKind(), ID: resource.GetId()}); err != nil {
			return fail(connect.CodeInternal, "execution_result_invalid", "executor result resource is invalid")
		}
		for _, ch := range resultRef.GetSha256() {
			if (ch < '0' || ch > '9') && (ch < 'a' || ch > 'f') {
				return fail(connect.CodeInternal, "execution_result_invalid", "executor result digest is invalid")
			}
		}
		if err := rejectUnknownFields(resultRef); err != nil {
			return fail(connect.CodeInternal, "execution_result_invalid", "executor result contains unknown fields")
		}
	}
	return nil
}

func validRedactedSummary(value string) bool {
	if value == "" || !utf8.ValidString(value) || len(value) > 512 || strings.ContainsAny(value, "\r\n\x00") {
		return value == ""
	}
	lower := strings.ToLower(value)
	for _, marker := range []string{"token", "secret", "password", "api-key", "apikey", "bearer", "private-key", "ssh-rsa"} {
		if strings.Contains(lower, marker) {
			return false
		}
	}
	return true
}

func cloneFinalizerReceipts(values []*workerv1alpha1.FinalizerReceipt) []*workerv1alpha1.FinalizerReceipt {
	if len(values) == 0 {
		return nil
	}
	out := make([]*workerv1alpha1.FinalizerReceipt, 0, len(values))
	for _, value := range values {
		if value != nil {
			out = append(out, proto.Clone(value).(*workerv1alpha1.FinalizerReceipt))
		}
	}
	return out
}

func cloneResultReferences(values []*workerv1alpha1.ResultReference) []*workerv1alpha1.ResultReference {
	if len(values) == 0 {
		return nil
	}
	out := make([]*workerv1alpha1.ResultReference, 0, len(values))
	for _, value := range values {
		if value != nil {
			out = append(out, proto.Clone(value).(*workerv1alpha1.ResultReference))
		}
	}
	return out
}

func fencingDigestBytes(value string) ([]byte, error) {
	if !strings.HasPrefix(value, "sha256:") {
		return nil, errors.New("invalid fencing digest")
	}
	decoded, err := hex.DecodeString(strings.TrimPrefix(value, "sha256:"))
	if err != nil || len(decoded) != sha256.Size {
		return nil, errors.New("invalid fencing digest")
	}
	return decoded, nil
}
