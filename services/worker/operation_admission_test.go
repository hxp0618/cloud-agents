package worker

import (
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"strings"
	"testing"
	"time"

	"connectrpc.com/connect"
	workerv1alpha1 "github.com/hxp0618/cloud-agents/sdk/go/gen/cloudagents/worker/v1alpha1"
	commonv1alpha1 "github.com/hxp0618/cloud-agents/sdk/go/gen/common/v1alpha1"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type admissionTestIdentityProvider struct {
	identity *workerv1alpha1.WorkloadIdentity
}

func (p *admissionTestIdentityProvider) ClientIdentity(context.Context) (*workerv1alpha1.WorkloadIdentity, error) {
	if p == nil || p.identity == nil {
		return nil, fmt.Errorf("missing identity")
	}
	return cloneIdentity(p.identity), nil
}

type admissionFixture struct {
	s      *Service
	server *workerv1alpha1.WorkloadIdentity
	client *workerv1alpha1.WorkloadIdentity
	clock  *time.Time
	ident  *admissionTestIdentityProvider
	req    *workerv1alpha1.OperationAttemptEnvelope
	token  []byte
}

func newAdmissionFixture(t *testing.T, operationCapability bool) admissionFixture {
	t.Helper()
	server := &workerv1alpha1.WorkloadIdentity{SpiffeId: "spiffe://cloud-agents.test/worker/admission", TrustDomain: "cloud-agents.test"}
	client := &workerv1alpha1.WorkloadIdentity{SpiffeId: "spiffe://cloud-agents.test/supervisor/admission", TrustDomain: "cloud-agents.test"}
	now := time.Date(2026, 8, 27, 12, 0, 0, 0, time.UTC)
	ident := &admissionTestIdentityProvider{identity: client}
	caps := []workerv1alpha1.Capability{workerv1alpha1.Capability_CAPABILITY_NEGOTIATION, workerv1alpha1.Capability_CAPABILITY_HEALTH}
	if operationCapability {
		caps = append(caps, workerv1alpha1.Capability_CAPABILITY_OPERATION_DISPATCH)
	}
	s, err := NewService(Config{
		WorkerIdentity: server, Capabilities: caps,
		AdmissionLeaseID: "lease-001", AdmissionGeneration: 42,
		IdentityProvider: ident, IDGenerator: func() (string, error) { return "negotiation-admission-test", nil },
		Clock: func() time.Time { return *(&now) }, NegotiationTTL: time.Minute,
	})
	if err != nil {
		t.Fatal(err)
	}
	required := []workerv1alpha1.Capability{workerv1alpha1.Capability_CAPABILITY_NEGOTIATION, workerv1alpha1.Capability_CAPABILITY_HEALTH}
	if operationCapability {
		required = append(required, workerv1alpha1.Capability_CAPABILITY_OPERATION_DISPATCH)
	}
	neg, err := s.Negotiate(context.Background(), connect.NewRequest(&workerv1alpha1.NegotiationRequest{
		SupportedVersions: []*workerv1alpha1.ProtocolVersion{{Major: 1, Minor: 0}}, RequiredCapabilities: required,
		ExpectedServerIdentity: cloneIdentity(server),
	}))
	if err != nil {
		t.Fatal(err)
	}
	token := []byte("p1-fixed-fencing-token")
	op := &workerv1alpha1.OperationEnvelope{
		OperationId: "operation-001", IdempotencyKey: "idem-operation-001",
		Scope:    &workerv1alpha1.NamespaceRef{Namespace: "cloud-agents", Kind: "project", Id: "project-123"},
		Fencing:  &workerv1alpha1.FencingProof{LeaseId: "lease-001", Generation: 42, Token: append([]byte(nil), token...)},
		Deadline: timestamppb.New(now.Add(30 * time.Second)), RequiredCapability: workerv1alpha1.Capability_CAPABILITY_OPERATION_DISPATCH,
		Command:    &workerv1alpha1.OperationCommand{Command: &workerv1alpha1.OperationCommand_Probe{Probe: &workerv1alpha1.ProbeOperation{ProbeName: "contract-only"}}},
		Finalizers: []*workerv1alpha1.FinalizerSpec{{Name: "cloud-agents.release", IdempotencyKey: "finalize-operation-001"}},
	}
	setAdmissionDigest(t, op)
	req := &workerv1alpha1.OperationAttemptEnvelope{Operation: op, AttemptId: "attempt-001", AttemptNumber: 1,
		ExpectedExecutorIdentity: cloneIdentity(server), Negotiation: bindingFrom(neg.Msg)}
	return admissionFixture{s: s, server: server, client: client, clock: &now, ident: ident, req: req, token: token}
}

func setAdmissionDigest(t *testing.T, op *workerv1alpha1.OperationEnvelope) {
	t.Helper()
	_, normalizedPtr, err := normalizeNamespaceRef(op.Scope)
	if err != nil {
		t.Fatal(err)
	}
	normalized := commonv1alpha1.NamespaceRef{Namespace: normalizedPtr.GetNamespace(), Kind: normalizedPtr.GetKind(), ID: normalizedPtr.GetId()}
	canonical, err := canonicalOperationEnvelope(op, normalized)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(canonical)
	op.CanonicalRequestSha256 = append([]byte(nil), sum[:]...)
}

func assertAdmissionError(t *testing.T, err error, code connect.Code, stable string) {
	t.Helper()
	if err == nil || connect.CodeOf(err) != code || !strings.Contains(err.Error(), stable) {
		t.Fatalf("err=%v code=%v want code=%v stable=%q", err, connect.CodeOf(err), code, stable)
	}
}

func TestOperationAdmissionProfileAndCanonicalFixture(t *testing.T) {
	profile := WorkerOperationAdmissionProfile()
	if !profile.Valid() || profile.ExternalSideEffects || profile.ID != OperationAdmissionProfileID {
		t.Fatalf("invalid admission profile: %+v", profile)
	}
	op := &workerv1alpha1.OperationEnvelope{
		OperationId: "operation-001", IdempotencyKey: "idem-operation-001",
		Scope:              &workerv1alpha1.NamespaceRef{Namespace: "cloud-agents", Kind: "project", Id: "project-123"},
		Fencing:            &workerv1alpha1.FencingProof{LeaseId: "lease-001", Generation: 42},
		Deadline:           timestamppb.New(time.Date(2026, 8, 10, 8, 0, 30, 0, time.UTC)),
		RequiredCapability: workerv1alpha1.Capability_CAPABILITY_OPERATION_DISPATCH,
		Command:            &workerv1alpha1.OperationCommand{Command: &workerv1alpha1.OperationCommand_Probe{Probe: &workerv1alpha1.ProbeOperation{ProbeName: "contract-only"}}},
		Finalizers:         []*workerv1alpha1.FinalizerSpec{{Name: "cloud-agents.release", IdempotencyKey: "finalize-operation-001"}},
	}
	scope, _, err := normalizeNamespaceRef(op.Scope)
	if err != nil {
		t.Fatal(err)
	}
	canonical, err := canonicalOperationEnvelope(op, scope)
	if err != nil {
		t.Fatal(err)
	}
	want := `{"command":{"probe":{"probeName":"contract-only"}},"deadline":"2026-08-10T08:00:30Z","fencing":{"generation":"42","leaseId":"lease-001"},"finalizers":[{"idempotencyKey":"finalize-operation-001","name":"cloud-agents.release"}],"idempotencyKey":"idem-operation-001","operationId":"operation-001","requiredCapability":"CAPABILITY_OPERATION_DISPATCH","scope":{"id":"project-123","kind":"project","namespace":"cloud-agents"}}`
	if string(canonical) != want {
		t.Fatalf("canonical=%s\nwant=%s", canonical, want)
	}
	sum := sha256.Sum256(canonical)
	if got := fmt.Sprintf("sha256:%x", sum[:]); got != "sha256:1b934930a1a506eab1ff406c614c92a0aa2c7efb206edeebf42e79329a9fa8b8" {
		t.Fatalf("canonical digest=%s", got)
	}
}

func TestOperationAdmissionHappyReplayAndNoRawToken(t *testing.T) {
	f := newAdmissionFixture(t, true)
	claim, err := f.s.AdmitOperation(context.Background(), f.req)
	if err != nil {
		t.Fatal(err)
	}
	if claim.ProfileID() != OperationAdmissionProfileID || claim.OperationID() != "operation-001" || claim.AttemptID() != "attempt-001" || claim.AttemptNumber() != 1 || claim.LeaseID() != "lease-001" || claim.Generation() != 42 || claim.Replayed() {
		t.Fatalf("unexpected claim: %+v", claim)
	}
	if strings.Contains(fmt.Sprintf("%#v", claim), string(f.token)) || strings.Contains(claim.FencingTokenDigest(), string(f.token)) {
		t.Fatal("raw fencing token leaked into claim")
	}
	before := claim.CanonicalRequestDigest()
	f.req.Operation.Fencing.Token[0] = 'X'
	f.req.Operation.Command.GetProbe().ProbeName = "mutated-after-admission"
	if claim.CanonicalRequestDigest() != before || claim.OperationID() != "operation-001" {
		t.Fatal("claim changed after caller mutation")
	}
	// A replay of the original exact request is accepted; the mutated request is not.
	f2 := newAdmissionFixture(t, true)
	first, err := f2.s.AdmitOperation(context.Background(), f2.req)
	if err != nil {
		t.Fatal(err)
	}
	replay, err := f2.s.AdmitOperation(context.Background(), f2.req)
	if err != nil || !replay.Replayed() || replay.CanonicalRequestDigest() != first.CanonicalRequestDigest() {
		t.Fatalf("replay=%+v err=%v", replay, err)
	}
}

func TestOperationAdmissionFailClosedNegatives(t *testing.T) {
	tests := []struct {
		name, stable string
		code         connect.Code
		mutate       func(*admissionFixture)
	}{
		{"missing operation", "operation_required", connect.CodeInvalidArgument, func(f *admissionFixture) { f.req.Operation = nil }},
		{"missing digest", "canonical_request_digest_required", connect.CodeInvalidArgument, func(f *admissionFixture) { f.req.Operation.CanonicalRequestSha256 = nil }},
		{"digest wrong length", "canonical_request_digest_invalid", connect.CodeInvalidArgument, func(f *admissionFixture) { f.req.Operation.CanonicalRequestSha256 = []byte{1} }},
		{"digest mismatch", "canonical_request_digest_mismatch", connect.CodeInvalidArgument, func(f *admissionFixture) {
			f.req.Operation.CanonicalRequestSha256 = bytes.Repeat([]byte{0}, sha256.Size)
		}},
		{"lease mismatch", "lease_mismatch", connect.CodePermissionDenied, func(f *admissionFixture) {
			f.req.Operation.Fencing.LeaseId = "lease-other"
			setAdmissionDigestForTest(f)
		}},
		{"stale generation", "stale_generation", connect.CodeFailedPrecondition, func(f *admissionFixture) { f.req.Operation.Fencing.Generation = 41; setAdmissionDigestForTest(f) }},
		{"empty token", "fencing_token_invalid", connect.CodeInvalidArgument, func(f *admissionFixture) { f.req.Operation.Fencing.Token = nil; setAdmissionDigestForTest(f) }},
		{"missing capability", "required_capability_invalid", connect.CodeFailedPrecondition, func(f *admissionFixture) {
			f.req.Operation.RequiredCapability = workerv1alpha1.Capability_CAPABILITY_HEALTH
			setAdmissionDigestForTest(f)
		}},
		{"executor mismatch", "executor_identity_mismatch", connect.CodePermissionDenied, func(f *admissionFixture) { f.req.ExpectedExecutorIdentity.SpiffeId = "spiffe://wrong" }},
		{"client mismatch", "client_identity_mismatch", connect.CodePermissionDenied, func(f *admissionFixture) {
			f.ident.identity = &workerv1alpha1.WorkloadIdentity{SpiffeId: "spiffe://wrong", TrustDomain: "cloud-agents.test"}
		}},
		{"scope invalid", "invalid_scope", connect.CodeInvalidArgument, func(f *admissionFixture) { f.req.Operation.Scope.Namespace = "bad/path"; setAdmissionDigestForTest(f) }},
		{"deadline expired", "deadline_exceeded", connect.CodeDeadlineExceeded, func(f *admissionFixture) {
			f.req.Operation.Deadline = timestamppb.New((*f.clock).Add(-time.Second))
			setAdmissionDigestForTest(f)
		}},
		{"deadline horizon", "deadline_horizon_exceeded", connect.CodeInvalidArgument, func(f *admissionFixture) {
			f.req.Operation.Deadline = timestamppb.New((*f.clock).Add(301 * time.Second))
			setAdmissionDigestForTest(f)
		}},
		{"extension command", "extension_payload_not_implemented", connect.CodeUnimplemented, func(f *admissionFixture) {
			data := []byte(`{}`)
			digest := sha256.Sum256(data)
			f.req.Operation.Command = &workerv1alpha1.OperationCommand{ExtensionPayload: &workerv1alpha1.BoundedPayload{MediaType: "application/json", Data: data, DeclaredSizeBytes: uint32(len(data)), Sha256: digest[:]}}
			setAdmissionDigestForTest(f)
		}},
		{"duplicate finalizer", "duplicate_finalizer", connect.CodeInvalidArgument, func(f *admissionFixture) {
			f.req.Operation.Finalizers = append(f.req.Operation.Finalizers, proto.Clone(f.req.Operation.Finalizers[0]).(*workerv1alpha1.FinalizerSpec))
			setAdmissionDigestForTest(f)
		}},
		{"unknown fields", "unknown_fields", connect.CodeInvalidArgument, func(f *admissionFixture) { f.req.Operation.ProtoReflect().SetUnknown([]byte{0xf8, 0x01, 0x01}) }},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := newAdmissionFixture(t, true)
			tc.mutate(&f)
			_, err := f.s.AdmitOperation(context.Background(), f.req)
			assertAdmissionError(t, err, tc.code, tc.stable)
			if len(f.s.admissions) != 0 {
				t.Fatalf("failed admission left records: %d", len(f.s.admissions))
			}
		})
	}
	// The negotiation binding can be valid while operation dispatch is not in its capability set.
	f := newAdmissionFixture(t, false)
	_, err := f.s.AdmitOperation(context.Background(), f.req)
	assertAdmissionError(t, err, connect.CodeFailedPrecondition, "capability_not_negotiated")
}

func setAdmissionDigestForTest(f *admissionFixture) {
	// Invalid semantic values may fail before digest comparison; for values that
	// remain structurally valid, refresh the expected digest to reach the target check.
	if f.req.Operation.Scope != nil {
		if _, normalizedPtr, err := normalizeNamespaceRef(f.req.Operation.Scope); err == nil {
			normalized := commonv1alpha1.NamespaceRef{Namespace: normalizedPtr.GetNamespace(), Kind: normalizedPtr.GetKind(), ID: normalizedPtr.GetId()}
			if canonical, err := canonicalOperationEnvelope(f.req.Operation, normalized); err == nil {
				sum := sha256.Sum256(canonical)
				f.req.Operation.CanonicalRequestSha256 = append([]byte(nil), sum[:]...)
			}
		}
	}
}

func TestOperationAdmissionContextCancellationAndNoOpWriters(t *testing.T) {
	f := newAdmissionFixture(t, true)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := f.s.AdmitOperation(ctx, f.req)
	assertAdmissionError(t, err, connect.CodeCanceled, "request_canceled")
	if len(f.s.admissions) != 0 {
		t.Fatal("canceled admission recorded state")
	}
	_, err = f.s.ExecuteOperation(context.Background(), connect.NewRequest(f.req))
	assertAdmissionError(t, err, connect.CodeUnimplemented, "operation_dispatch_not_implemented")
	_, err = f.s.GetOperationReceipt(context.Background(), connect.NewRequest(&workerv1alpha1.ReceiptRequest{}))
	assertAdmissionError(t, err, connect.CodeUnimplemented, "durable_receipts_not_implemented")
}

func TestOperationAdmissionReplayConflictsAndNestedUnknowns(t *testing.T) {
	f := newAdmissionFixture(t, true)
	if _, err := f.s.AdmitOperation(context.Background(), f.req); err != nil {
		t.Fatal(err)
	}
	changedKey := proto.Clone(f.req).(*workerv1alpha1.OperationAttemptEnvelope)
	changedKey.Operation.IdempotencyKey = "idem-operation-other"
	setAdmissionDigestForTest(&admissionFixture{req: changedKey})
	_, err := f.s.AdmitOperation(context.Background(), changedKey)
	assertAdmissionError(t, err, connect.CodeAlreadyExists, "idempotency_conflict")

	changedToken := proto.Clone(f.req).(*workerv1alpha1.OperationAttemptEnvelope)
	changedToken.Operation.Fencing.Token = []byte("different-fencing-token")
	// The token is intentionally excluded from canonical JSON; its digest is
	// still part of the immutable admission identity and must conflict.
	_, err = f.s.AdmitOperation(context.Background(), changedToken)
	assertAdmissionError(t, err, connect.CodeAlreadyExists, "idempotency_conflict")

	nestedUnknown := proto.Clone(f.req).(*workerv1alpha1.OperationAttemptEnvelope)
	nestedUnknown.Operation.Command.GetProbe().ProtoReflect().SetUnknown([]byte{0xf8, 0x01, 0x01})
	_, err = f.s.AdmitOperation(context.Background(), nestedUnknown)
	assertAdmissionError(t, err, connect.CodeInvalidArgument, "unknown_fields")
}

func TestOperationAdmissionAllowsStrictlyIncreasingAttempts(t *testing.T) {
	f := newAdmissionFixture(t, true)
	first, err := f.s.AdmitOperation(context.Background(), f.req)
	if err != nil {
		t.Fatal(err)
	}
	second := proto.Clone(f.req).(*workerv1alpha1.OperationAttemptEnvelope)
	second.AttemptId = "attempt-002"
	second.AttemptNumber = 2
	second.Operation.Fencing.Token = []byte("rotated-fencing-token")
	// The raw token is excluded from canonical JSON, so a new attempt may use
	// a renewed proof while retaining the same operation intent.
	secondClaim, err := f.s.AdmitOperation(context.Background(), second)
	if err != nil {
		t.Fatal(err)
	}
	if secondClaim.Replayed() || secondClaim.AttemptID() != "attempt-002" || secondClaim.AttemptNumber() != 2 || secondClaim.CanonicalRequestDigest() != first.CanonicalRequestDigest() {
		t.Fatalf("second attempt claim = %+v", secondClaim)
	}
	if got := len(f.s.admissions); got != 2 {
		t.Fatalf("admission records = %d, want 2", got)
	}
	older := proto.Clone(f.req).(*workerv1alpha1.OperationAttemptEnvelope)
	older.AttemptId = "attempt-003"
	older.AttemptNumber = 1
	_, err = f.s.AdmitOperation(context.Background(), older)
	assertAdmissionError(t, err, connect.CodeFailedPrecondition, "attempt_number_not_monotonic")
}

func TestOperationAdmissionAuthorityAttemptAndScopeNormalization(t *testing.T) {
	f := newAdmissionFixture(t, true)
	f.s.admissionLeaseID = ""
	_, err := f.s.AdmitOperation(context.Background(), f.req)
	assertAdmissionError(t, err, connect.CodeFailedPrecondition, "generation_authority_missing")

	f = newAdmissionFixture(t, true)
	f.req.AttemptNumber = 0
	_, err = f.s.AdmitOperation(context.Background(), f.req)
	assertAdmissionError(t, err, connect.CodeInvalidArgument, "attempt_number_required")
	if len(f.s.admissions) != 0 {
		t.Fatal("invalid attempt number recorded state")
	}

	f = newAdmissionFixture(t, true)
	f.req.Operation.Scope.Id = "Cafe\u0301"
	setAdmissionDigestForTest(&f)
	claim, err := f.s.AdmitOperation(context.Background(), f.req)
	if err != nil {
		t.Fatal(err)
	}
	scope, ok := claim.Scope()
	if !ok || scope.ID != "Café" {
		t.Fatalf("scope was not normalized to NFC: %+v ok=%v", scope, ok)
	}
}

func TestOperationAdmissionCapacityFailsClosed(t *testing.T) {
	f := newAdmissionFixture(t, true)
	for i := 0; i < maxAdmissionRecords; i++ {
		f.s.admissions[fmt.Sprintf("occupied-%d", i)] = admissionRecord{}
	}
	_, err := f.s.AdmitOperation(context.Background(), f.req)
	assertAdmissionError(t, err, connect.CodeResourceExhausted, "admission_capacity_exceeded")
}
