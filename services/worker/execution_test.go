package worker

import (
	"context"
	"strings"
	"sync"
	"testing"

	"connectrpc.com/connect"
	workerv1alpha1 "github.com/hxp0618/cloud-agents/sdk/go/gen/cloudagents/worker/v1alpha1"
	"google.golang.org/protobuf/proto"
)

type countingExecutor struct {
	mu     sync.Mutex
	calls  int
	result OperationExecutionResult
}

func (e *countingExecutor) Execute(context.Context, OperationExecutionInput) (OperationExecutionResult, error) {
	e.mu.Lock()
	e.calls++
	e.mu.Unlock()
	return e.result, nil
}

func TestDeterministicLocalExecutionAndDetachedReceipt(t *testing.T) {
	f := newAdmissionFixture(t, true)
	f.s.executor = DeterministicLocalExecutor{}
	response, err := f.s.ExecuteOperation(context.Background(), connect.NewRequest(f.req))
	if err != nil {
		t.Fatal(err)
	}
	receipt := response.Msg
	if receipt.GetOutcome() != workerv1alpha1.OperationOutcome_OPERATION_OUTCOME_SUCCEEDED || receipt.GetOperationId() != "operation-001" || receipt.GetAttemptId() != "attempt-001" || receipt.GetSequence() != 1 {
		t.Fatalf("unexpected receipt: %v", receipt)
	}
	if len(receipt.GetFencing().GetTokenSha256()) != 32 || strings.Contains(receipt.String(), string(f.token)) {
		t.Fatalf("receipt leaked or omitted fencing digest: %v", receipt)
	}
	request := &workerv1alpha1.ReceiptRequest{OperationId: receipt.GetOperationId(), ReceiptId: receipt.GetReceiptId(), Fencing: &workerv1alpha1.FencingProof{LeaseId: "lease-001", Generation: 42, Token: append([]byte(nil), f.token...)}, ExpectedServerIdentity: cloneIdentity(f.server), Negotiation: f.req.GetNegotiation(), RequiredCapability: workerv1alpha1.Capability_CAPABILITY_OPERATION_DISPATCH}
	got, err := f.s.GetOperationReceipt(context.Background(), connect.NewRequest(request))
	if err != nil {
		t.Fatal(err)
	}
	if got.Msg.GetReceiptId() != receipt.GetReceiptId() || got.Msg.GetSequence() != 1 {
		t.Fatalf("unexpected detached receipt: %v", got.Msg)
	}
	// Replaying the same attempt is idempotent and does not allocate a second receipt.
	replay, err := f.s.ExecuteOperation(context.Background(), connect.NewRequest(f.req))
	if err != nil || replay.Msg.GetReceiptId() != receipt.GetReceiptId() || replay.Msg.GetSequence() != receipt.GetSequence() {
		t.Fatalf("replay=%v err=%v", replay, err)
	}
}

func TestDetachedReceiptFailClosed(t *testing.T) {
	f := newAdmissionFixture(t, true)
	f.s.executor = DeterministicLocalExecutor{}
	response, err := f.s.ExecuteOperation(context.Background(), connect.NewRequest(f.req))
	if err != nil {
		t.Fatal(err)
	}
	base := &workerv1alpha1.ReceiptRequest{OperationId: response.Msg.GetOperationId(), ReceiptId: response.Msg.GetReceiptId(), Fencing: &workerv1alpha1.FencingProof{LeaseId: "lease-001", Generation: 42, Token: append([]byte(nil), f.token...)}, ExpectedServerIdentity: cloneIdentity(f.server), Negotiation: f.req.GetNegotiation(), RequiredCapability: workerv1alpha1.Capability_CAPABILITY_OPERATION_DISPATCH}
	cases := []struct {
		name, stable string
		mutate       func(*workerv1alpha1.ReceiptRequest)
		code         connect.Code
	}{
		{"wrong receipt", "receipt_not_found", func(r *workerv1alpha1.ReceiptRequest) { r.ReceiptId = "missing" }, connect.CodeNotFound},
		{"wrong operation", "receipt_not_found", func(r *workerv1alpha1.ReceiptRequest) { r.OperationId = "other" }, connect.CodeNotFound},
		{"wrong fencing", "fencing_mismatch", func(r *workerv1alpha1.ReceiptRequest) { r.Fencing.Token[0] = 'x' }, connect.CodePermissionDenied},
		{"wrong capability", "required_capability_invalid", func(r *workerv1alpha1.ReceiptRequest) {
			r.RequiredCapability = workerv1alpha1.Capability_CAPABILITY_HEALTH
		}, connect.CodeFailedPrecondition},
		{"unknown fields", "unknown_fields", func(r *workerv1alpha1.ReceiptRequest) {
			r.ProtoReflect().SetUnknown([]byte{0xf8, 0x01, 0x01})
		}, connect.CodeInvalidArgument},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := protoCloneReceiptRequest(base)
			tc.mutate(r)
			_, err := f.s.GetOperationReceipt(context.Background(), connect.NewRequest(r))
			if connect.CodeOf(err) != tc.code || !strings.Contains(err.Error(), tc.stable) {
				t.Fatalf("err=%v code=%v", err, connect.CodeOf(err))
			}
		})
	}
}

func protoCloneReceiptRequest(value *workerv1alpha1.ReceiptRequest) *workerv1alpha1.ReceiptRequest {
	return proto.Clone(value).(*workerv1alpha1.ReceiptRequest)
}

func TestExecutionParallelReplayInvokesExecutorOnce(t *testing.T) {
	f := newAdmissionFixture(t, true)
	executor := &countingExecutor{result: OperationExecutionResult{Outcome: workerv1alpha1.OperationOutcome_OPERATION_OUTCOME_SUCCEEDED, RedactedSummary: "ok"}}
	f.s.executor = executor
	var wg sync.WaitGroup
	responses := make([]*workerv1alpha1.DurableReceipt, 2)
	errs := make([]error, 2)
	for i := range responses {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			response, err := f.s.ExecuteOperation(context.Background(), connect.NewRequest(f.req))
			if response != nil {
				responses[index] = response.Msg
			}
			errs[index] = err
		}(i)
	}
	wg.Wait()
	for i := range errs {
		if errs[i] != nil || responses[i] == nil {
			t.Fatalf("parallel execution[%d] response=%v err=%v", i, responses[i], errs[i])
		}
	}
	executor.mu.Lock()
	calls := executor.calls
	executor.mu.Unlock()
	if calls != 1 || responses[0].GetReceiptId() != responses[1].GetReceiptId() {
		t.Fatalf("executor calls=%d receipts=%q/%q", calls, responses[0].GetReceiptId(), responses[1].GetReceiptId())
	}
}

func TestExecutionRejectsSensitiveSummary(t *testing.T) {
	f := newAdmissionFixture(t, true)
	f.s.executor = &countingExecutor{result: OperationExecutionResult{Outcome: workerv1alpha1.OperationOutcome_OPERATION_OUTCOME_SUCCEEDED, RedactedSummary: "token=secret"}}
	_, err := f.s.ExecuteOperation(context.Background(), connect.NewRequest(f.req))
	if connect.CodeOf(err) != connect.CodeInternal || !strings.Contains(err.Error(), "execution_result_invalid") {
		t.Fatalf("err=%v code=%v", err, connect.CodeOf(err))
	}
}

func TestAdmissionAndExecutionRejectCrossClientReplay(t *testing.T) {
	f := newAdmissionFixture(t, true)
	f.s.executor = DeterministicLocalExecutor{}
	if _, err := f.s.ExecuteOperation(context.Background(), connect.NewRequest(f.req)); err != nil {
		t.Fatal(err)
	}
	// Establish a distinct negotiation for a different authenticated client,
	// then replay the same immutable operation envelope.
	f.ident.identity = &workerv1alpha1.WorkloadIdentity{SpiffeId: "spiffe://cloud-agents.test/supervisor/other", TrustDomain: "cloud-agents.test"}
	f.s.newID = func() (string, error) { return "negotiation-other", nil }
	neg, err := f.s.Negotiate(context.Background(), connect.NewRequest(&workerv1alpha1.NegotiationRequest{SupportedVersions: []*workerv1alpha1.ProtocolVersion{{Major: 1, Minor: 0}}, RequiredCapabilities: []workerv1alpha1.Capability{workerv1alpha1.Capability_CAPABILITY_NEGOTIATION, workerv1alpha1.Capability_CAPABILITY_OPERATION_DISPATCH}, ExpectedServerIdentity: cloneIdentity(f.server)}))
	if err != nil {
		t.Fatal(err)
	}
	replay := proto.Clone(f.req).(*workerv1alpha1.OperationAttemptEnvelope)
	replay.Negotiation = bindingFrom(neg.Msg)
	_, err = f.s.ExecuteOperation(context.Background(), connect.NewRequest(replay))
	if connect.CodeOf(err) != connect.CodePermissionDenied || !strings.Contains(err.Error(), "client_identity_mismatch") {
		t.Fatalf("cross-client replay err=%v code=%v", err, connect.CodeOf(err))
	}
	if _, err = f.s.AdmitOperation(context.Background(), replay); connect.CodeOf(err) != connect.CodePermissionDenied || !strings.Contains(err.Error(), "client_identity_mismatch") {
		t.Fatalf("cross-client admission err=%v code=%v", err, connect.CodeOf(err))
	}
}
