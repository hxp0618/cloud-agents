package supervisor

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"strings"
	"time"
	"unicode/utf8"

	"connectrpc.com/connect"
	workerv1alpha1 "github.com/hxp0618/cloud-agents/sdk/go/gen/cloudagents/worker/v1alpha1"
	workerkernel "github.com/hxp0618/cloud-agents/services/worker"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
)

// LocalConfig selects the only constructor that can enable the localdev
// dispatch profile. Handle is an opaque capability minted by a real Worker
// Service; it is not an endpoint, URL, transport selector, or persistence
// handle. The constructor performs no I/O.
type LocalConfig struct {
	Handle                 workerkernel.LocalDispatchHandle
	ExpectedWorkerIdentity *workerv1alpha1.WorkloadIdentity
	Clock                  Clock
}

// localDispatchMarker is deliberately package-private and compared by
// identity. Passing a generated Connect client to New can therefore never
// enable the dispatch methods, even if that client happens to implement all
// four generated RPCs.
type localDispatchMarker struct{}

var localDispatchAuthorityMarker = &localDispatchMarker{}

// NewLocal constructs a local-only Supervisor over an opaque in-process
// Worker capability. It does not bind or perform any RPC until the caller
// explicitly invokes BindLocalDispatch.
func NewLocal(config LocalConfig) (*Supervisor, error) {
	if !config.Handle.Valid() {
		return nil, errInvalidConfig
	}
	s, err := New(Config{
		Client:                 config.Handle,
		ExpectedWorkerIdentity: config.ExpectedWorkerIdentity,
		Clock:                  config.Clock,
	})
	if err != nil {
		return nil, err
	}
	s.localHandle = config.Handle
	s.localDispatchMarker = localDispatchAuthorityMarker
	return s, nil
}

// NewInProcess is a descriptive alias for NewLocal. It is kept as a narrow
// alias so callers cannot accidentally infer that the generic New constructor
// authorizes dispatch.
func NewInProcess(config LocalConfig) (*Supervisor, error) {
	return NewLocal(config)
}

func (s *Supervisor) localDispatchEnabled() bool {
	return s != nil && s.localDispatchMarker == localDispatchAuthorityMarker && s.localHandle.Valid() && localDispatchProfileValid()
}

func localDispatchProfileValid() bool {
	return GeneratedLocalDispatchProfile.Valid() &&
		GeneratedLocalDispatchProfile.ID == LocalDispatchProfileID &&
		GeneratedLocalDispatchProfile.Transport == "in_process" &&
		!GeneratedLocalDispatchProfile.ExternalSideEffects &&
		!GeneratedLocalDispatchProfile.GenericClientDispatch
}

// BindLocalDispatch negotiates the fixed generated local dispatch profile. A
// caller cannot provide a profile ID or capability selector. The generic New
// constructor is intentionally rejected here, preserving its health/admission
// compatibility behavior and its stable no-op dispatch methods.
func (s *Supervisor) BindLocalDispatch(ctx context.Context) (BindingSnapshot, error) {
	if err := contextErr(ctx); err != nil {
		return BindingSnapshot{}, err
	}
	if !s.localDispatchEnabled() {
		return BindingSnapshot{}, fail(connect.CodeUnimplemented, "operation_dispatch_not_implemented")
	}
	return s.bindWithProfile(ctx, operationAdmissionCapabilities(), LocalDispatchProfileID)
}

// DispatchOperation validates the exact local binding and delegates to the
// opaque Worker handle. The operation and receipt remain process-local and
// bounded by the Worker kernel; this method does not open a listener, write a
// database, invoke a provider, or create a durable receipt.
func (s *Supervisor) DispatchOperation(ctx context.Context, req *workerv1alpha1.OperationAttemptEnvelope) (*connect.Response[workerv1alpha1.DurableReceipt], error) {
	if err := contextErr(ctx); err != nil {
		return nil, err
	}
	live, state, err := s.localDispatchBinding()
	if err != nil {
		return nil, err
	}
	attempt, err := prepareLocalDispatchAttempt(req, state)
	if err != nil {
		return nil, err
	}
	operationDeadline, err := operationDeadline(attempt)
	if err != nil {
		return nil, err
	}
	now, ok := s.nowUTC()
	if !ok {
		return nil, errInvalidConfig
	}
	if !now.Before(operationDeadline) {
		return nil, fail(connect.CodeDeadlineExceeded, "operation_deadline_exceeded")
	}
	callCtx, cancel := localBoundContext(ctx, now, state.expiresAt, operationDeadline)
	defer cancel()
	response, callErr := s.localHandle.ExecuteOperation(callCtx, connect.NewRequest(attempt))
	if callErr != nil {
		if err := contextErr(ctx); err != nil {
			return nil, err
		}
		if err := s.localDispatchPostCall(ctx, live, state, operationDeadline); err != nil {
			return nil, err
		}
		return nil, rpcFailure("execute_operation", callErr)
	}
	if err := s.localDispatchPostCall(ctx, live, state, operationDeadline); err != nil {
		return nil, err
	}
	if err := validateLocalDispatchReceipt(response, attempt); err != nil {
		return nil, err
	}
	return detachedReceiptResponse(response), nil
}

// GetOperationReceipt retrieves a detached receipt from the same bounded
// process-local Worker kernel. It requires the exact binding tuple used for
// dispatch and never consults a durable store.
func (s *Supervisor) GetOperationReceipt(ctx context.Context, req *workerv1alpha1.ReceiptRequest) (*connect.Response[workerv1alpha1.DurableReceipt], error) {
	if err := contextErr(ctx); err != nil {
		return nil, err
	}
	if !s.localDispatchEnabled() {
		return nil, fail(connect.CodeUnimplemented, "durable_receipts_not_implemented")
	}
	live, state, err := s.localDispatchBinding()
	if err != nil {
		return nil, err
	}
	receiptRequest, err := prepareLocalReceiptRequest(req, state)
	if err != nil {
		return nil, err
	}
	now, ok := s.nowUTC()
	if !ok {
		return nil, errInvalidConfig
	}
	callCtx, cancel := localBoundContext(ctx, now, state.expiresAt, time.Time{})
	defer cancel()
	response, callErr := s.localHandle.GetOperationReceipt(callCtx, connect.NewRequest(receiptRequest))
	if callErr != nil {
		if err := contextErr(ctx); err != nil {
			return nil, err
		}
		if err := s.localDispatchPostCall(ctx, live, state, time.Time{}); err != nil {
			return nil, err
		}
		return nil, rpcFailure("get_operation_receipt", callErr)
	}
	if err := s.localDispatchPostCall(ctx, live, state, time.Time{}); err != nil {
		return nil, err
	}
	if err := validateLocalReceiptResponse(response, receiptRequest.GetOperationId(), receiptRequest.GetReceiptId(), receiptRequest.GetFencing()); err != nil {
		return nil, err
	}
	return detachedReceiptResponse(response), nil
}

// localDispatchBinding returns both the live pointer and its detached view.
// The pointer is used as an ABA-resistant lineage fence after the RPC.
func (s *Supervisor) localDispatchBinding() (*bindingState, *bindingState, error) {
	if !s.localDispatchEnabled() {
		return nil, nil, fail(connect.CodeUnimplemented, "operation_dispatch_not_implemented")
	}
	return s.operationBinding(LocalDispatchProfileID)
}

func (s *Supervisor) remoteDispatchBinding() (*bindingState, *bindingState, error) {
	return s.operationBinding(OperationAdmissionProfileID)
}

func (s *Supervisor) operationBinding(profileID string) (*bindingState, *bindingState, error) {
	s.mu.RLock()
	live := s.binding
	state := cloneBindingState(live)
	s.mu.RUnlock()
	if live == nil || state == nil {
		return nil, nil, fail(connect.CodeFailedPrecondition, "binding_required")
	}
	if state.profileID != profileID || !sameIdentity(state.identity, s.workerIdentity) || state.descriptor == nil ||
		!exactCapabilities(state.accepted, operationAdmissionCapabilities()) ||
		!exactCapabilities(state.descriptor.GetCapabilities(), operationAdmissionCapabilities()) {
		return nil, nil, fail(connect.CodeFailedPrecondition, "operation_binding_required")
	}
	now, ok := s.nowUTC()
	if !ok {
		return nil, nil, errInvalidConfig
	}
	if !now.Before(state.expiresAt) {
		s.clearBindingPointerIf(live)
		return nil, nil, fail(connect.CodeDeadlineExceeded, "binding_expired")
	}
	return live, state, nil
}

func (s *Supervisor) clearBindingPointerIf(expected *bindingState) {
	if s == nil || expected == nil {
		return
	}
	s.mu.Lock()
	if s.binding == expected {
		s.binding = nil
	}
	s.mu.Unlock()
}

func (s *Supervisor) localDispatchPostCall(ctx context.Context, live, state *bindingState, opDeadline time.Time) error {
	if err := contextErr(ctx); err != nil {
		return err
	}
	now, ok := s.nowUTC()
	if !ok {
		return errInvalidConfig
	}
	s.mu.RLock()
	current := s.binding
	s.mu.RUnlock()
	if current != live {
		return fail(connect.CodeFailedPrecondition, "binding_changed")
	}
	if !now.Before(state.expiresAt) {
		s.clearBindingPointerIf(live)
		return fail(connect.CodeDeadlineExceeded, "binding_expired")
	}
	if !opDeadline.IsZero() && !now.Before(opDeadline) {
		return fail(connect.CodeDeadlineExceeded, "operation_deadline_exceeded")
	}
	return nil
}

func prepareLocalDispatchAttempt(req *workerv1alpha1.OperationAttemptEnvelope, state *bindingState) (*workerv1alpha1.OperationAttemptEnvelope, error) {
	if req == nil {
		return nil, fail(connect.CodeInvalidArgument, "operation_request_invalid")
	}
	attempt, ok := proto.Clone(req).(*workerv1alpha1.OperationAttemptEnvelope)
	if !ok || attempt == nil {
		return nil, fail(connect.CodeInvalidArgument, "operation_request_invalid")
	}
	if hasUnknownSupervisorFields(attempt) {
		return nil, fail(connect.CodeInvalidArgument, "unknown_fields")
	}
	if proto.Size(attempt) > int(GeneratedLocalDispatchProfile.MaxWireMessageBytes) {
		return nil, fail(connect.CodeInvalidArgument, "wire_message_too_large")
	}
	if !sameNegotiationBinding(attempt.GetNegotiation(), state) {
		return nil, fail(connect.CodeFailedPrecondition, "negotiation_binding_mismatch")
	}
	if !sameIdentity(attempt.GetExpectedExecutorIdentity(), state.identity) {
		return nil, fail(connect.CodePermissionDenied, "executor_identity_mismatch")
	}
	if attempt.GetOperation() == nil || attempt.GetOperation().GetRequiredCapability() != workerv1alpha1.Capability_CAPABILITY_OPERATION_DISPATCH {
		return nil, fail(connect.CodeFailedPrecondition, "required_capability_invalid")
	}
	if attempt.GetOperation().GetDeadline() == nil || attempt.GetOperation().GetDeadline().CheckValid() != nil {
		return nil, fail(connect.CodeInvalidArgument, "operation_deadline_invalid")
	}
	return attempt, nil
}

func prepareLocalReceiptRequest(req *workerv1alpha1.ReceiptRequest, state *bindingState) (*workerv1alpha1.ReceiptRequest, error) {
	if req == nil {
		return nil, fail(connect.CodeInvalidArgument, "receipt_request_invalid")
	}
	receiptRequest, ok := proto.Clone(req).(*workerv1alpha1.ReceiptRequest)
	if !ok || receiptRequest == nil {
		return nil, fail(connect.CodeInvalidArgument, "receipt_request_invalid")
	}
	if hasUnknownSupervisorFields(receiptRequest) {
		return nil, fail(connect.CodeInvalidArgument, "unknown_fields")
	}
	if proto.Size(receiptRequest) > int(GeneratedLocalDispatchProfile.MaxWireMessageBytes) {
		return nil, fail(connect.CodeInvalidArgument, "wire_message_too_large")
	}
	if !validLocalIdentifier(receiptRequest.GetOperationId()) || !validLocalIdentifier(receiptRequest.GetReceiptId()) {
		return nil, fail(connect.CodeInvalidArgument, "receipt_identity_invalid")
	}
	if !sameNegotiationBinding(receiptRequest.GetNegotiation(), state) {
		return nil, fail(connect.CodeFailedPrecondition, "negotiation_binding_mismatch")
	}
	if !sameIdentity(receiptRequest.GetExpectedServerIdentity(), state.identity) {
		return nil, fail(connect.CodePermissionDenied, "server_identity_mismatch")
	}
	if receiptRequest.GetRequiredCapability() != workerv1alpha1.Capability_CAPABILITY_OPERATION_DISPATCH {
		return nil, fail(connect.CodeFailedPrecondition, "required_capability_invalid")
	}
	fencing := receiptRequest.GetFencing()
	if fencing == nil || !validLocalIdentifier(fencing.GetLeaseId()) || fencing.GetGeneration() == 0 || len(fencing.GetToken()) == 0 || len(fencing.GetToken()) > int(GeneratedLocalDispatchProfile.MaxFencingTokenBytes) {
		return nil, fail(connect.CodeInvalidArgument, "fencing_required")
	}
	return receiptRequest, nil
}

func sameNegotiationBinding(got *workerv1alpha1.NegotiationBinding, state *bindingState) bool {
	if got == nil || state == nil || !isProtocolVersion(got.GetProtocolVersion()) || got.GetNegotiationId() != state.negotiationID {
		return false
	}
	return got.GetExpiresAt() != nil && got.GetExpiresAt().CheckValid() == nil && got.GetExpiresAt().AsTime().UTC().Equal(state.expiresAt.UTC())
}

func operationDeadline(attempt *workerv1alpha1.OperationAttemptEnvelope) (time.Time, error) {
	if attempt == nil || attempt.GetOperation() == nil || attempt.GetOperation().GetDeadline() == nil || attempt.GetOperation().GetDeadline().CheckValid() != nil {
		return time.Time{}, fail(connect.CodeInvalidArgument, "operation_deadline_invalid")
	}
	return attempt.GetOperation().GetDeadline().AsTime().UTC(), nil
}

func localBoundContext(parent context.Context, now, bindingExpiry, operationExpiry time.Time) (context.Context, context.CancelFunc) {
	deadline := bindingExpiry
	if deadline.IsZero() || (!operationExpiry.IsZero() && operationExpiry.Before(deadline)) {
		deadline = operationExpiry
	}
	if deadline.IsZero() || now.IsZero() {
		return parent, func() {}
	}
	if parentDeadline, ok := parent.Deadline(); ok && !deadline.Before(parentDeadline) {
		return parent, func() {}
	}
	remaining := deadline.Sub(now)
	if remaining <= 0 {
		return context.WithTimeout(parent, 0)
	}
	return context.WithTimeout(parent, remaining)
}

func validateLocalDispatchReceipt(response *connect.Response[workerv1alpha1.DurableReceipt], attempt *workerv1alpha1.OperationAttemptEnvelope) error {
	if attempt == nil || attempt.GetOperation() == nil || attempt.GetOperation().GetFencing() == nil {
		return fail(connect.CodeInternal, "receipt_request_context_invalid")
	}
	return validateLocalReceiptResponse(response, attempt.GetOperation().GetOperationId(), "", attempt.GetOperation().GetFencing(), attempt.GetAttemptId(), attempt.GetOperation().GetIdempotencyKey())
}

func validateLocalReceiptResponse(response *connect.Response[workerv1alpha1.DurableReceipt], operationID, receiptID string, expectedFencing *workerv1alpha1.FencingProof, optionalAttemptAndKey ...string) error {
	if response == nil || response.Msg == nil {
		return fail(connect.CodeInternal, "receipt_response_missing")
	}
	receipt := response.Msg
	if hasUnknownSupervisorFields(receipt) || proto.Size(receipt) > int(GeneratedLocalDispatchProfile.MaxWireMessageBytes) {
		return fail(connect.CodeInternal, "receipt_response_invalid")
	}
	if !validLocalIdentifier(receipt.GetReceiptId()) || !validLocalIdentifier(receipt.GetAttemptId()) || !validLocalIdentifier(receipt.GetIdempotencyKey()) || receipt.GetSequence() == 0 || receipt.GetObservedAt() == nil || receipt.GetObservedAt().CheckValid() != nil {
		return fail(connect.CodeInternal, "receipt_response_invalid")
	}
	if len(receipt.GetFinalizers()) > int(GeneratedLocalDispatchProfile.MaxRepeatedItems) || len(receipt.GetResults()) > int(GeneratedLocalDispatchProfile.MaxRepeatedItems) || !validLocalRedactedSummary(receipt.GetRedactedSummary()) || (receipt.GetStableErrorCode() != "" && !validLocalIdentifier(receipt.GetStableErrorCode())) {
		return fail(connect.CodeInternal, "receipt_response_invalid")
	}
	if operationID == "" || receipt.GetOperationId() != operationID {
		return fail(connect.CodeInternal, "receipt_identity_mismatch")
	}
	if receiptID != "" && receipt.GetReceiptId() != receiptID {
		return fail(connect.CodeInternal, "receipt_identity_mismatch")
	}
	if len(optionalAttemptAndKey) >= 1 && receipt.GetAttemptId() != optionalAttemptAndKey[0] {
		return fail(connect.CodeInternal, "receipt_identity_mismatch")
	}
	if len(optionalAttemptAndKey) >= 2 && receipt.GetIdempotencyKey() != optionalAttemptAndKey[1] {
		return fail(connect.CodeInternal, "receipt_identity_mismatch")
	}
	if !terminalOutcome(receipt.GetOutcome()) {
		return fail(connect.CodeInternal, "receipt_outcome_invalid")
	}
	fencing := receipt.GetFencing()
	if fencing == nil || !validLocalIdentifier(fencing.GetLeaseId()) || fencing.GetGeneration() == 0 || len(fencing.GetTokenSha256()) != sha256.Size {
		return fail(connect.CodeInternal, "receipt_fencing_invalid")
	}
	if expectedFencing == nil || len(expectedFencing.GetToken()) == 0 || receipt.GetFencing().GetLeaseId() != expectedFencing.GetLeaseId() || receipt.GetFencing().GetGeneration() != expectedFencing.GetGeneration() {
		return fail(connect.CodeInternal, "receipt_fencing_invalid")
	}
	digest := sha256.Sum256(expectedFencing.GetToken())
	if subtle.ConstantTimeCompare(fencing.GetTokenSha256(), digest[:]) != 1 {
		return fail(connect.CodeInternal, "receipt_fencing_mismatch")
	}
	return nil
}

func terminalOutcome(outcome workerv1alpha1.OperationOutcome) bool {
	switch outcome {
	case workerv1alpha1.OperationOutcome_OPERATION_OUTCOME_SUCCEEDED,
		workerv1alpha1.OperationOutcome_OPERATION_OUTCOME_FAILED,
		workerv1alpha1.OperationOutcome_OPERATION_OUTCOME_CANCELLED,
		workerv1alpha1.OperationOutcome_OPERATION_OUTCOME_DEADLINE_EXCEEDED,
		workerv1alpha1.OperationOutcome_OPERATION_OUTCOME_FENCED:
		return true
	default:
		return false
	}
}

func detachedReceiptResponse(response *connect.Response[workerv1alpha1.DurableReceipt]) *connect.Response[workerv1alpha1.DurableReceipt] {
	if response == nil || response.Msg == nil {
		return nil
	}
	return connect.NewResponse(proto.Clone(response.Msg).(*workerv1alpha1.DurableReceipt))
}

func validLocalIdentifier(value string) bool {
	if value == "" || !utf8.ValidString(value) || len(value) > int(GeneratedLocalDispatchProfile.MaxIdentifierBytes) {
		return false
	}
	for _, r := range value {
		if r < 0x20 || r == 0x7f {
			return false
		}
	}
	return true
}

func validLocalRedactedSummary(value string) bool {
	if value == "" {
		return true
	}
	if !utf8.ValidString(value) || len(value) > 512 || strings.ContainsAny(value, "\r\n\x00") {
		return false
	}
	lower := strings.ToLower(value)
	for _, marker := range []string{"token", "secret", "password", "api-key", "apikey", "bearer", "private-key", "ssh-rsa"} {
		if strings.Contains(lower, marker) {
			return false
		}
	}
	return true
}

func hasUnknownSupervisorFields(message proto.Message) bool {
	if message == nil {
		return false
	}
	var walk func(protoreflect.Message) bool
	walk = func(current protoreflect.Message) bool {
		if !current.IsValid() {
			return false
		}
		if len(current.GetUnknown()) > 0 {
			return true
		}
		found := false
		current.Range(func(field protoreflect.FieldDescriptor, value protoreflect.Value) bool {
			if field.Kind() != protoreflect.MessageKind && field.Kind() != protoreflect.GroupKind {
				return true
			}
			if field.IsList() {
				list := value.List()
				for i := 0; i < list.Len(); i++ {
					if walk(list.Get(i).Message()) {
						found = true
						return false
					}
				}
				return true
			}
			if walk(value.Message()) {
				found = true
				return false
			}
			return true
		})
		return found
	}
	return walk(message.ProtoReflect())
}
