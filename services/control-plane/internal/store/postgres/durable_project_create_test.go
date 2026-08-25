package postgres

import (
	"errors"
	"testing"

	"github.com/hxp0618/cloud-agents/services/control-plane/internal/coordination"
	"github.com/jackc/pgx/v5/pgconn"
)

func TestDurableProjectCreateSettlementClosesOutcomes(t *testing.T) {
	operationID := "operation-project-alpha"
	resourceKind := "project"
	resourceID := "project-alpha"
	resourceVersion := int64(7)
	outboxID := "event-project-alpha"
	outboxState := "pending"
	tests := []struct {
		name        string
		result      DurableProjectCreateResult
		err         error
		wantOutcome DatabaseOutcome
		wantErr     error
	}{
		{
			name: "created",
			result: DurableProjectCreateResult{
				Disposition: "created", ReplayState: "succeeded", OperationID: &operationID,
				OperationGeneration: &resourceVersion, ResourceKind: &resourceKind, ResourceID: &resourceID,
				ResourceVersion: &resourceVersion, OutboxEventID: &outboxID, OutboxState: &outboxState,
			},
			wantOutcome: DatabaseCommitted,
		},
		{
			name: "replay without outbox",
			result: DurableProjectCreateResult{
				Disposition: "replay", ReplayState: "succeeded", ResourceKind: &resourceKind,
				ResourceID: &resourceID, ResourceVersion: &resourceVersion,
			},
			wantOutcome: DatabaseCommitted,
		},
		{
			name:        "conflict",
			result:      DurableProjectCreateResult{Disposition: "conflict"},
			wantOutcome: DatabaseRejected,
		},
		{
			name:        "commit unknown",
			result:      DurableProjectCreateResult{Disposition: "created"},
			err:         ErrMutationCommitUnknown,
			wantOutcome: DatabaseUnknown,
		},
		{
			name:        "serialization rejected",
			result:      DurableProjectCreateResult{Disposition: "created"},
			err:         &pgconn.PgError{Code: "40001", Message: "serialization"},
			wantOutcome: DatabaseRejected,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := settleDurableProjectCreate(test.result, test.err)
			if test.wantErr != nil {
				if !errors.Is(err, test.wantErr) {
					t.Fatalf("error = %v, want %v", err, test.wantErr)
				}
				return
			}
			if err != nil || got.DatabaseOutcome != test.wantOutcome {
				t.Fatalf("result/error = %#v / %v", got, err)
			}
			if test.wantOutcome != DatabaseCommitted && got.DatabaseOutcome != test.wantOutcome {
				t.Fatalf("rejected/unknown result leaked = %#v", got)
			}
		})
	}
}

func TestDurableProjectCreateUsesDurableProfileAndDeterministicProjectIdentity(t *testing.T) {
	request := coordinationProjectRequest()
	intent, err := coordination.BindManagedAgentCreateProjectDurable(
		coordination.ManagedAgentCreateProjectDurable(), coordinationTenant, request,
	)
	if err != nil {
		t.Fatal(err)
	}
	if got := durableProjectUID(intent.RequestDigest()); len(got) != len("project-")+32 || got[:len("project-")] != "project-" {
		t.Fatalf("derived project UID = %q", got)
	}
	if got := durableProjectUID(intent.RequestDigest()); got != durableProjectUID(intent.RequestDigest()) {
		t.Fatal("project UID is not deterministic")
	}
}
