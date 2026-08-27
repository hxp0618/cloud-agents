package worker

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"connectrpc.com/connect"
	workerv1alpha1 "github.com/hxp0618/cloud-agents/sdk/go/gen/cloudagents/worker/v1alpha1"
	workerv1alpha1connect "github.com/hxp0618/cloud-agents/sdk/go/gen/cloudagents/worker/v1alpha1/workerv1alpha1connect"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func testService(t *testing.T) (*Service, *workerv1alpha1.WorkloadIdentity, *time.Time) {
	t.Helper()
	server := &workerv1alpha1.WorkloadIdentity{SpiffeId: "spiffe://cloud-agents.test/worker/test", TrustDomain: "cloud-agents.test"}
	client := &workerv1alpha1.WorkloadIdentity{SpiffeId: "spiffe://cloud-agents.test/supervisor/test", TrustDomain: "cloud-agents.test"}
	now := time.Date(2026, 8, 27, 12, 0, 0, 0, time.UTC)
	returnIdentity := StaticIdentityProvider{Identity: client}
	s, err := NewService(Config{WorkerIdentity: server, IdentityProvider: returnIdentity, IDGenerator: func() (string, error) { return "opaque-test-id", nil }, Clock: func() time.Time { return now }, NegotiationTTL: time.Minute})
	if err != nil {
		t.Fatal(err)
	}
	return s, server, &now
}

func negotiate(t *testing.T, s *Service, server *workerv1alpha1.WorkloadIdentity) *workerv1alpha1.NegotiationResponse {
	t.Helper()
	r, err := s.Negotiate(context.Background(), connect.NewRequest(&workerv1alpha1.NegotiationRequest{
		SupportedVersions:      []*workerv1alpha1.ProtocolVersion{{Major: 1, Minor: 0}},
		RequiredCapabilities:   []workerv1alpha1.Capability{workerv1alpha1.Capability_CAPABILITY_NEGOTIATION, workerv1alpha1.Capability_CAPABILITY_HEALTH},
		ExpectedServerIdentity: cloneIdentity(server),
	}))
	if err != nil {
		t.Fatal(err)
	}
	return r.Msg
}

func bindingFrom(r *workerv1alpha1.NegotiationResponse) *workerv1alpha1.NegotiationBinding {
	return &workerv1alpha1.NegotiationBinding{ProtocolVersion: r.SelectedVersion, NegotiationId: r.NegotiationId, ExpiresAt: r.ExpiresAt}
}

func TestNegotiateHealthHappyPath(t *testing.T) {
	s, server, _ := testService(t)
	r := negotiate(t, s, server)
	h, err := s.CheckHealth(context.Background(), connect.NewRequest(&workerv1alpha1.HealthRequest{Negotiation: bindingFrom(r), RequiredCapabilities: []workerv1alpha1.Capability{workerv1alpha1.Capability_CAPABILITY_HEALTH}, ExpectedServerIdentity: cloneIdentity(server)}))
	if err != nil {
		t.Fatal(err)
	}
	if h.Msg.State != workerv1alpha1.HealthState_HEALTH_STATE_SERVING || h.Msg.Protocol.GetCurrentVersion().GetMinor() != 0 {
		t.Fatalf("unexpected health response: %v", h.Msg)
	}
}

func TestConnectHandlerInProcess(t *testing.T) {
	s, server, _ := testService(t)
	path, handler := NewHandler(s)
	ts := httptest.NewServer(handler)
	defer ts.Close()
	client := workerv1alpha1connect.NewWorkerExecutionServiceClient(ts.Client(), strings.TrimSuffix(ts.URL, path))
	r, err := client.Negotiate(context.Background(), connect.NewRequest(&workerv1alpha1.NegotiationRequest{SupportedVersions: []*workerv1alpha1.ProtocolVersion{{Major: 1, Minor: 0}}, ExpectedServerIdentity: cloneIdentity(server)}))
	if err != nil {
		t.Fatal(err)
	}
	if r.Msg.GetNegotiationId() == "" {
		t.Fatal("missing negotiation id")
	}
}

func TestFailClosedNegotiation(t *testing.T) {
	s, server, _ := testService(t)
	cases := []struct {
		name string
		req  *workerv1alpha1.NegotiationRequest
		code connect.Code
		text string
	}{
		{"unknown version", &workerv1alpha1.NegotiationRequest{SupportedVersions: []*workerv1alpha1.ProtocolVersion{{Major: 9}}, ExpectedServerIdentity: cloneIdentity(server)}, connect.CodeInvalidArgument, "unsupported_protocol_version"},
		{"unknown capability", &workerv1alpha1.NegotiationRequest{SupportedVersions: []*workerv1alpha1.ProtocolVersion{{Major: 1}}, RequiredCapabilities: []workerv1alpha1.Capability{workerv1alpha1.Capability(32767)}, ExpectedServerIdentity: cloneIdentity(server)}, connect.CodeInvalidArgument, "unknown_required_capability"},
		{"identity mismatch", &workerv1alpha1.NegotiationRequest{SupportedVersions: []*workerv1alpha1.ProtocolVersion{{Major: 1}}, ExpectedServerIdentity: &workerv1alpha1.WorkloadIdentity{SpiffeId: "spiffe://wrong", TrustDomain: "cloud-agents.test"}}, connect.CodePermissionDenied, "server_identity_mismatch"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := s.Negotiate(context.Background(), connect.NewRequest(tc.req))
			if connect.CodeOf(err) != tc.code || !strings.Contains(err.Error(), tc.text) {
				t.Fatalf("err=%v code=%v", err, connect.CodeOf(err))
			}
		})
	}
}

func TestBindingFailClosedAndExpiry(t *testing.T) {
	s, server, now := testService(t)
	r := negotiate(t, s, server)
	_, err := s.CheckHealth(context.Background(), connect.NewRequest(&workerv1alpha1.HealthRequest{RequiredCapabilities: []workerv1alpha1.Capability{workerv1alpha1.Capability_CAPABILITY_HEALTH}, ExpectedServerIdentity: cloneIdentity(server)}))
	if connect.CodeOf(err) != connect.CodeFailedPrecondition || !strings.Contains(err.Error(), "negotiation_required") {
		t.Fatalf("missing binding err=%v", err)
	}
	wrong := bindingFrom(r)
	wrong.ExpiresAt = timestamppb.New(r.ExpiresAt.AsTime().Add(time.Second))
	_, err = s.CheckHealth(context.Background(), connect.NewRequest(&workerv1alpha1.HealthRequest{Negotiation: wrong, ExpectedServerIdentity: cloneIdentity(server)}))
	if connect.CodeOf(err) != connect.CodeFailedPrecondition || !strings.Contains(err.Error(), "negotiation_expiry_mismatch") {
		t.Fatalf("wrong binding err=%v", err)
	}
	*now = now.Add(2 * time.Minute)
	_, err = s.CheckHealth(context.Background(), connect.NewRequest(&workerv1alpha1.HealthRequest{Negotiation: bindingFrom(r), ExpectedServerIdentity: cloneIdentity(server)}))
	if connect.CodeOf(err) != connect.CodeDeadlineExceeded || !strings.Contains(err.Error(), "negotiation_expired") {
		t.Fatalf("expired err=%v", err)
	}
}

func TestUnsupportedOperationsAndCancellationHaveNoEffects(t *testing.T) {
	s, _, _ := testService(t)
	_, err := s.ExecuteOperation(context.Background(), connect.NewRequest(&workerv1alpha1.OperationAttemptEnvelope{}))
	if connect.CodeOf(err) != connect.CodeUnimplemented || !strings.Contains(err.Error(), "operation_dispatch_not_implemented") {
		t.Fatalf("execute err=%v", err)
	}
	_, err = s.GetOperationReceipt(context.Background(), connect.NewRequest(&workerv1alpha1.ReceiptRequest{}))
	if connect.CodeOf(err) != connect.CodeUnimplemented || !strings.Contains(err.Error(), "durable_receipts_not_implemented") {
		t.Fatalf("receipt err=%v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = s.Negotiate(ctx, connect.NewRequest(&workerv1alpha1.NegotiationRequest{}))
	if connect.CodeOf(err) != connect.CodeCanceled {
		t.Fatalf("cancel err=%v", err)
	}
}
