package worker

import (
	"strings"
	"testing"

	"connectrpc.com/connect"
	workerruntimev1alpha1 "github.com/hxp0618/cloud-agents/sdk/go/gen/cloudagents/worker/runtime/v1alpha1"
	workerv1alpha1 "github.com/hxp0618/cloud-agents/sdk/go/gen/cloudagents/worker/v1alpha1"
)

func TestRuntimeFencingRequiresConfiguredToken(t *testing.T) {
	service, err := NewService(Config{
		WorkerIdentity:      &workerv1alpha1.WorkloadIdentity{SpiffeId: "spiffe://cloud-agents.test/worker", TrustDomain: "cloud-agents.test"},
		RuntimeCommand:      []string{"runtime"},
		AdmissionLeaseID:    "lease-runtime",
		AdmissionGeneration: 7,
		AdmissionToken:      []byte("expected-token"),
	})
	if err != nil {
		t.Fatal(err)
	}
	open := &workerruntimev1alpha1.RuntimeSessionOpen{ExecutionId: "execution", Generation: 7, Fencing: &workerv1alpha1.FencingProof{LeaseId: "lease-runtime", Generation: 7, Token: []byte("expected-token")}}
	if err := service.validateRuntimeFencing(open); err != nil {
		t.Fatalf("matching Runtime token = %v", err)
	}
	open.Fencing.Token = []byte("wrong-token")
	if err := service.validateRuntimeFencing(open); connect.CodeOf(err) != connect.CodePermissionDenied || !strings.Contains(err.Error(), "fencing_token_mismatch") {
		t.Fatalf("wrong Runtime token = %v", err)
	}
}
