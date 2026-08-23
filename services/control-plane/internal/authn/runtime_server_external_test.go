package authn_test

import (
	"context"
	"errors"
	"os"
	"testing"

	commonv1 "github.com/hxp0618/cloud-agents/sdk/go/gen/common/v1alpha1"
	platformv1 "github.com/hxp0618/cloud-agents/sdk/go/gen/platform/v1alpha1"
	"github.com/hxp0618/cloud-agents/services/control-plane/internal/authn"
	"github.com/hxp0618/cloud-agents/services/control-plane/internal/server"
	"github.com/hxp0618/cloud-agents/services/control-plane/internal/store/postgres"
	"github.com/jackc/pgx/v5"
)

func TestPostgresExternalManagedAgentCreateProjectServerConformance(t *testing.T) {
	environment := openExternalPostgresEnvironment(t)
	tenantID := os.Getenv("CLOUD_AGENTS_COORDINATION_TENANT_ID")
	mode := os.Getenv("CLOUD_AGENTS_COORDINATION_RUN_ID")
	if (mode != "normal" && mode != "race") || tenantID != "tenant-coordination-"+mode {
		t.Fatal("external runtime-server test requires its isolated normal or race tenant")
	}
	service, err := postgres.NewDurableCoordinationService(environment.runtimePool)
	if err != nil {
		t.Fatalf("create concrete durable coordination service: %v", err)
	}
	runtimeServer, err := server.NewManagedAgentCreateProjectServer(service)
	if err != nil {
		t.Fatalf("create transport-neutral runtime server: %v", err)
	}

	actor := "user-admin"
	organizationID := "organization-" + mode
	body := runtimeServerProjectBody(t, mode, organizationID)
	requestPrefix := "request-runtime-server-" + mode
	keyPrefix := "idempotency-runtime-server-" + mode

	createdRequest := server.ManagedAgentCreateProjectRequest{
		RouteTenantID: tenantID, RequestID: requestPrefix + "-created",
		IdempotencyKey: keyPrefix + "-created", Body: body,
	}
	createdPrincipal := newExternalPrincipal(t, tenantID, "organization", organizationID, "projects.create", actor)
	created, err := runtimeServer.Claim(environment.ctx, createdPrincipal.Principal, createdRequest)
	if err != nil || created.DatabaseOutcome != postgres.DatabaseCommitted || created.Disposition != "created" || created.ReplayState != "pending" {
		t.Fatalf("runtime server created claim = %#v/%v", created, err)
	}

	replayRequest := createdRequest
	replayRequest.RequestID = requestPrefix + "-replay-different-request-id"
	replayPrincipal := newExternalPrincipal(t, tenantID, "organization", organizationID, "projects.create", actor)
	replay, err := runtimeServer.Claim(environment.ctx, replayPrincipal.Principal, replayRequest)
	if err != nil || replay.DatabaseOutcome != postgres.DatabaseCommitted || replay.Disposition != "replay" || replay.ReplayState != "pending" {
		t.Fatalf("runtime server same-digest replay = %#v/%v", replay, err)
	}

	validationPrincipal := newExternalPrincipal(t, tenantID, "organization", organizationID, "projects.create", actor)
	invalid := server.ManagedAgentCreateProjectRequest{
		RouteTenantID: tenantID, RequestID: requestPrefix + "-invalid",
		IdempotencyKey: "short", Body: body,
	}
	if _, err := runtimeServer.Claim(environment.ctx, validationPrincipal.Principal, invalid); err == nil {
		t.Fatal("invalid generated request reached the concrete service")
	}
	validationPreserved := createdRequest
	validationPreserved.RequestID = requestPrefix + "-validation-preserved"
	validationPreserved.IdempotencyKey = keyPrefix + "-validation-preserved"
	if result, err := runtimeServer.Claim(environment.ctx, validationPrincipal.Principal, validationPreserved); err != nil || result.Disposition != "created" {
		t.Fatalf("generated validation consumed principal before service = %#v/%v", result, err)
	}

	writesBefore := runtimeServerWriteCounts(t, environment, tenantID, requestPrefix, keyPrefix)

	tenantMismatch := newExternalPrincipal(t, tenantID, "organization", organizationID, "projects.create", actor)
	tenantMismatchRequest := createdRequest
	tenantMismatchRequest.RouteTenantID = "tenant-route-mismatch-" + mode
	tenantMismatchRequest.RequestID = requestPrefix + "-tenant-mismatch"
	tenantMismatchRequest.IdempotencyKey = keyPrefix + "-tenant-mismatch"
	if _, err := runtimeServer.Claim(environment.ctx, tenantMismatch.Principal, tenantMismatchRequest); !errors.Is(err, postgres.ErrMutationDenied) {
		t.Fatalf("route/principal tenant mismatch error = %v", err)
	}

	organizationMismatch := newExternalPrincipal(t, tenantID, "organization", "organization-mismatch-"+mode, "projects.create", actor)
	organizationMismatchRequest := createdRequest
	organizationMismatchRequest.RequestID = requestPrefix + "-organization-mismatch"
	organizationMismatchRequest.IdempotencyKey = keyPrefix + "-organization-mismatch"
	if _, err := runtimeServer.Claim(environment.ctx, organizationMismatch.Principal, organizationMismatchRequest); !errors.Is(err, postgres.ErrMutationDenied) {
		t.Fatalf("body/principal organization mismatch error = %v", err)
	}

	nilRequest := createdRequest
	nilRequest.RequestID = requestPrefix + "-nil-principal"
	nilRequest.IdempotencyKey = keyPrefix + "-nil-principal"
	if _, err := runtimeServer.Claim(environment.ctx, nil, nilRequest); err == nil {
		t.Fatal("nil principal reached the PostgreSQL claim")
	}

	stale := newExternalPrincipal(t, tenantID, "organization", organizationID, "projects.create", actor)
	if err := stale.Invalidate(); err != nil {
		t.Fatal(err)
	}
	staleRequest := createdRequest
	staleRequest.RequestID = requestPrefix + "-stale-principal"
	staleRequest.IdempotencyKey = keyPrefix + "-stale-principal"
	if _, err := runtimeServer.Claim(environment.ctx, stale.Principal, staleRequest); err == nil {
		t.Fatal("stale principal reached the PostgreSQL claim")
	}

	consumed := newExternalPrincipal(t, tenantID, "organization", organizationID, "projects.create", actor)
	if err := authn.ConsumeVerifiedPrincipal(consumed.Principal, func(authn.VerifiedPrincipalView) error { return nil }); err != nil {
		t.Fatalf("consume test principal: %v", err)
	}
	consumedRequest := createdRequest
	consumedRequest.RequestID = requestPrefix + "-consumed-principal"
	consumedRequest.IdempotencyKey = keyPrefix + "-consumed-principal"
	if _, err := runtimeServer.Claim(environment.ctx, consumed.Principal, consumedRequest); err == nil {
		t.Fatal("consumed principal reached the PostgreSQL claim")
	}

	canceledContext, cancel := context.WithCancel(environment.ctx)
	cancel()
	canceled := newExternalPrincipal(t, tenantID, "organization", organizationID, "projects.create", actor)
	canceledRequest := createdRequest
	canceledRequest.RequestID = requestPrefix + "-canceled"
	canceledRequest.IdempotencyKey = keyPrefix + "-canceled"
	if _, err := runtimeServer.Claim(canceledContext, canceled.Principal, canceledRequest); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled runtime-server claim error = %v", err)
	}

	writesAfter := runtimeServerWriteCounts(t, environment, tenantID, requestPrefix, keyPrefix)
	if writesAfter != writesBefore || writesAfter != (runtimeServerWrites{records: 2, audits: 2}) {
		t.Fatalf("runtime server unintended records/audits before=%#v after=%#v", writesBefore, writesAfter)
	}
	assertRuntimeServerHasNoProjectOrSideEffectWrites(t, environment, tenantID, mode, requestPrefix)
}

func runtimeServerProjectBody(t *testing.T, mode string, organizationID string) []byte {
	t.Helper()
	body, err := platformv1.EncodeProjectCreateRequestJSON(platformv1.ProjectCreateRequest{
		Name: "project-runtime-server-" + mode,
		OrganizationRef: commonv1.OrganizationRef{
			Namespace: "cloud-agents", Kind: "organization", ID: organizationID,
		},
		DisplayName: "Runtime server project " + mode,
	})
	if err != nil {
		t.Fatalf("encode runtime-server project body: %v", err)
	}
	return body
}

type runtimeServerWrites struct {
	records int64
	audits  int64
}

func runtimeServerWriteCounts(
	t *testing.T,
	environment externalPostgresEnvironment,
	tenantID string,
	requestPrefix string,
	keyPrefix string,
) runtimeServerWrites {
	t.Helper()
	var counts runtimeServerWrites
	withExternalVerificationTransaction(t, environment.ctx, environment.verificationPool, tenantID, func(transaction pgx.Tx) {
		if err := transaction.QueryRow(environment.ctx, `SELECT
    (SELECT pg_catalog.count(*) FROM cloud_agents.idempotency_records
     WHERE tenant_id = cloud_agents.require_tenant_id() AND idempotency_key LIKE $1),
    (SELECT pg_catalog.count(*) FROM cloud_agents.coordination_audit_facts
     WHERE tenant_id = cloud_agents.require_tenant_id() AND audit_fact_id LIKE $2)`,
			keyPrefix+"%", requestPrefix+"%",
		).Scan(&counts.records, &counts.audits); err != nil {
			t.Fatalf("count runtime-server writes: %v", err)
		}
	})
	return counts
}

func assertRuntimeServerHasNoProjectOrSideEffectWrites(
	t *testing.T,
	environment externalPostgresEnvironment,
	tenantID string,
	mode string,
	requestPrefix string,
) {
	t.Helper()
	withExternalVerificationTransaction(t, environment.ctx, environment.verificationPool, tenantID, func(transaction pgx.Tx) {
		var projects, operations, finalizers, outbox int64
		if err := transaction.QueryRow(environment.ctx, `SELECT
    (SELECT pg_catalog.count(*) FROM cloud_agents.projects
     WHERE tenant_id = cloud_agents.require_tenant_id() AND project_name = $1),
    (SELECT pg_catalog.count(*) FROM cloud_agents.platform_operations
     WHERE tenant_id = cloud_agents.require_tenant_id()),
    (SELECT pg_catalog.count(*) FROM cloud_agents.operation_finalizers
     WHERE tenant_id = cloud_agents.require_tenant_id()),
    (SELECT pg_catalog.count(*) FROM cloud_agents.outbox_events
     WHERE tenant_id = cloud_agents.require_tenant_id() AND event_id LIKE $2)`,
			"project-runtime-server-"+mode, requestPrefix+"%",
		).Scan(&projects, &operations, &finalizers, &outbox); err != nil {
			t.Fatalf("verify runtime-server side-effect boundary: %v", err)
		}
		if projects != 0 || operations != 0 || finalizers != 0 || outbox != 0 {
			t.Fatalf("runtime server project/operation/finalizer/outbox writes = %d/%d/%d/%d", projects, operations, finalizers, outbox)
		}
	})
}
