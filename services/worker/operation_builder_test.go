package worker

import (
	"bytes"
	"testing"
	"time"

	workerv1alpha1 "github.com/hxp0618/cloud-agents/sdk/go/gen/cloudagents/worker/v1alpha1"
	commonv1alpha1 "github.com/hxp0618/cloud-agents/sdk/go/gen/common/v1alpha1"
	"google.golang.org/protobuf/proto"
)

func TestBuildLocalOperationAttemptCanonicalAndDetached(t *testing.T) {
	now := time.Date(2026, 8, 28, 13, 0, 0, 0, time.UTC)
	token := []byte("builder-token")
	identity := &workerv1alpha1.WorkloadIdentity{SpiffeId: "spiffe://cloud-agents.test/worker/builder", TrustDomain: "cloud-agents.test"}
	negotiation := &workerv1alpha1.NegotiationBinding{
		ProtocolVersion: &workerv1alpha1.ProtocolVersion{Major: ProtocolMajor, Minor: ProtocolMinor},
		NegotiationId:   "negotiation-builder",
	}
	command := &workerv1alpha1.OperationCommand{Command: &workerv1alpha1.OperationCommand_Probe{
		Probe: &workerv1alpha1.ProbeOperation{ProbeName: "contract-only"},
	}}
	attempt, err := BuildLocalOperationAttempt(LocalOperationAttemptInput{
		OperationID: "operation-builder", IdempotencyKey: "idempotency-builder",
		Scope: commonScopeForBuilder(), FencingLeaseID: "lease-builder", FencingGeneration: 9,
		FencingToken: token, Deadline: now.Add(10 * time.Second), AttemptID: "attempt-builder", AttemptNumber: 1,
		ExpectedExecutorIdentity: identity, Negotiation: negotiation, Command: command, Now: now,
	})
	if err != nil {
		t.Fatal(err)
	}
	if attempt.GetOperation().GetCanonicalRequestSha256() == nil || len(attempt.GetOperation().GetCanonicalRequestSha256()) != 32 {
		t.Fatalf("canonical digest = %x", attempt.GetOperation().GetCanonicalRequestSha256())
	}
	if !bytes.Equal(attempt.GetOperation().GetFencing().GetToken(), token) {
		t.Fatal("fencing token was not copied")
	}
	if attempt.GetExpectedExecutorIdentity() == identity || attempt.GetNegotiation() == negotiation {
		t.Fatal("nested inputs were not detached")
	}
	// Mutating caller-owned messages after construction cannot alter the attempt.
	command.GetProbe().ProbeName = "caller-mutated"
	token[0] = 'X'
	identity.SpiffeId = "caller-mutated"
	if attempt.GetOperation().GetCommand().GetProbe().GetProbeName() != "contract-only" || attempt.GetExpectedExecutorIdentity().GetSpiffeId() != "spiffe://cloud-agents.test/worker/builder" {
		t.Fatal("attempt changed after caller mutation")
	}
	replay, err := BuildLocalOperationAttempt(LocalOperationAttemptInput{
		OperationID: "operation-builder", IdempotencyKey: "idempotency-builder",
		Scope: commonScopeForBuilder(), FencingLeaseID: "lease-builder", FencingGeneration: 9,
		FencingToken: []byte("builder-token"), Deadline: now.Add(10 * time.Second), AttemptID: "attempt-builder", AttemptNumber: 1,
		ExpectedExecutorIdentity: &workerv1alpha1.WorkloadIdentity{SpiffeId: "spiffe://cloud-agents.test/worker/builder", TrustDomain: "cloud-agents.test"},
		Negotiation:              &workerv1alpha1.NegotiationBinding{ProtocolVersion: &workerv1alpha1.ProtocolVersion{Major: ProtocolMajor, Minor: ProtocolMinor}, NegotiationId: "negotiation-builder"},
		Command:                  &workerv1alpha1.OperationCommand{Command: &workerv1alpha1.OperationCommand_Probe{Probe: &workerv1alpha1.ProbeOperation{ProbeName: "contract-only"}}}, Now: now,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !proto.Equal(attempt, replay) {
		t.Fatal("same typed input did not produce a deterministic attempt")
	}
}

func TestBuildLocalOperationAttemptRejectsMalformedAuthorityInputs(t *testing.T) {
	now := time.Date(2026, 8, 28, 13, 0, 0, 0, time.UTC)
	base := LocalOperationAttemptInput{
		OperationID: "operation-builder", IdempotencyKey: "idempotency-builder", Scope: commonScopeForBuilder(),
		FencingLeaseID: "lease-builder", FencingGeneration: 9, FencingToken: []byte("builder-token"), Deadline: now.Add(time.Second),
		AttemptID: "attempt-builder", AttemptNumber: 1,
		ExpectedExecutorIdentity: &workerv1alpha1.WorkloadIdentity{SpiffeId: "spiffe://cloud-agents.test/worker/builder", TrustDomain: "cloud-agents.test"},
		Negotiation:              &workerv1alpha1.NegotiationBinding{ProtocolVersion: &workerv1alpha1.ProtocolVersion{Major: ProtocolMajor, Minor: ProtocolMinor}, NegotiationId: "negotiation-builder"},
		Command:                  &workerv1alpha1.OperationCommand{Command: &workerv1alpha1.OperationCommand_Probe{Probe: &workerv1alpha1.ProbeOperation{ProbeName: "contract-only"}}}, Now: now,
	}
	tests := []struct {
		name   string
		mutate func(*LocalOperationAttemptInput)
	}{
		{"missing lease", func(in *LocalOperationAttemptInput) { in.FencingLeaseID = "" }},
		{"oversized token", func(in *LocalOperationAttemptInput) {
			in.FencingToken = bytes.Repeat([]byte{'x'}, maxOperationTokenBytes+1)
		}},
		{"expired deadline", func(in *LocalOperationAttemptInput) { in.Deadline = now }},
		{"missing command", func(in *LocalOperationAttemptInput) { in.Command = nil }},
		{"unknown extension", func(in *LocalOperationAttemptInput) {
			in.Command.ExtensionPayload = &workerv1alpha1.BoundedPayload{MediaType: "text/plain", Data: []byte("x"), DeclaredSizeBytes: 1, Sha256: bytes.Repeat([]byte{'x'}, 32)}
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			input := base
			test.mutate(&input)
			if _, err := BuildLocalOperationAttempt(input); err == nil {
				t.Fatal("malformed input was accepted")
			}
		})
	}
}

func commonScopeForBuilder() commonv1alpha1.NamespaceRef {
	return commonv1alpha1.NamespaceRef{Namespace: "cloud-agents", Kind: "project", ID: "tenant-builder~project-builder"}
}
