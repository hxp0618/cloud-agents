package postgres

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/hxp0618/cloud-agents/services/control-plane/internal/authz"
	"github.com/hxp0618/cloud-agents/services/control-plane/internal/coordination"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	coordinationIntegrationRequest  = "sha256:1111111111111111111111111111111111111111111111111111111111111111"
	coordinationIntegrationConflict = "sha256:2222222222222222222222222222222222222222222222222222222222222222"
	coordinationIntegrationPayload  = "sha256:3333333333333333333333333333333333333333333333333333333333333333"
)

func TestDurableCoordinationPostgresConformance(t *testing.T) {
	if testing.Short() {
		t.Skip("PostgreSQL durable coordination conformance is disabled in short mode")
	}
	databaseURL := os.Getenv("CLOUD_AGENTS_TEST_DATABASE_URL")
	if databaseURL == "" {
		if os.Getenv("CLOUD_AGENTS_REQUIRE_POSTGRES_TEST") == "1" {
			t.Fatal("CLOUD_AGENTS_TEST_DATABASE_URL is required by the durable coordination gate")
		}
		t.Skip("CLOUD_AGENTS_TEST_DATABASE_URL is not configured")
	}
	verificationDatabaseURL := os.Getenv("CLOUD_AGENTS_TEST_MIGRATION_DATABASE_URL")
	if verificationDatabaseURL == "" {
		t.Fatal("CLOUD_AGENTS_TEST_MIGRATION_DATABASE_URL is required by the durable coordination audit gate")
	}
	runID := os.Getenv("CLOUD_AGENTS_COORDINATION_RUN_ID")
	tenantID := os.Getenv("CLOUD_AGENTS_COORDINATION_TENANT_ID")
	if (runID != "normal" && runID != "race") || !validMutationIdentifier(tenantID) {
		t.Fatal("durable coordination test requires an isolated normal or race tenant")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	pool := openCoordinationIntegrationPool(t, ctx, databaseURL, 8)
	verificationPool := openCoordinationIntegrationPool(t, ctx, verificationDatabaseURL, 2)
	assertDurableCoordinationPrivileges(t, ctx, pool)
	service, err := NewDurableCoordinationService(pool)
	if err != nil {
		t.Fatalf("create durable coordination service: %v", err)
	}

	actor := authz.SubjectRef{Kind: "user", Issuer: "https://identity.example.test/", Subject: "user-admin"}
	scope := authz.ScopeRef{Level: authz.ScopeOrganization, ID: "organization-" + runID}
	profile := coordination.ManagedAgentCreateProject()
	projectID := "project-" + runID

	denied, err := service.ClaimIdempotency(ctx, tenantID, IdempotencyClaimInput{
		Profile: profile,
		Actor: authz.SubjectRef{
			Kind: "user", Issuer: "https://identity.example.test/", Subject: "user-denied",
		},
		Scope: scope, IdempotencyKey: "idempotency-" + runID + "-denied",
		RequestDigest: coordinationIntegrationRequest, AuditFactID: "audit-" + runID + "-denied",
	})
	if !errors.Is(err, ErrMutationDenied) || denied != (IdempotencyClaimResult{}) {
		t.Fatalf("unauthorized claim result/error = %#v / %v", denied, err)
	}

	mainKey := "idempotency-" + runID + "-main"
	mainClaim := claimCoordinationIntegration(t, ctx, service, tenantID, profile, actor, scope, mainKey, coordinationIntegrationRequest, "audit-"+runID+"-main-claim")
	if mainClaim.DatabaseOutcome != DatabaseCommitted || mainClaim.Disposition != "created" || mainClaim.ReplayState != "pending" {
		t.Fatalf("main claim = %#v", mainClaim)
	}
	if remaining := time.Until(mainClaim.ExpiresAt); remaining < 23*time.Hour+59*time.Minute || remaining > 24*time.Hour+time.Minute {
		t.Fatalf("generated replay TTL drift = %s", remaining)
	}
	replay := claimCoordinationIntegration(t, ctx, service, tenantID, profile, actor, scope, mainKey, coordinationIntegrationRequest, "audit-"+runID+"-main-replay")
	if replay.DatabaseOutcome != DatabaseCommitted || replay.Disposition != "replay" || replay.ReplayState != "pending" {
		t.Fatalf("pending replay = %#v", replay)
	}
	conflict := claimCoordinationIntegration(t, ctx, service, tenantID, profile, actor, scope, mainKey, coordinationIntegrationConflict, "audit-"+runID+"-main-conflict")
	if conflict.DatabaseOutcome != DatabaseRejected || conflict.Disposition != "conflict" {
		t.Fatalf("digest conflict = %#v", conflict)
	}

	mainSuccess := completeCoordinationIntegrationSuccess(
		t, ctx, service, tenantID, profile, actor, scope, mainKey, coordinationIntegrationRequest,
		projectID, "event-"+runID+"-main", "audit-"+runID+"-main-success",
	)
	if mainSuccess.DatabaseOutcome != DatabaseCommitted || mainSuccess.OutboxState != "pending" {
		t.Fatalf("main completion = %#v", mainSuccess)
	}
	terminalReplay := claimCoordinationIntegration(t, ctx, service, tenantID, profile, actor, scope, mainKey, coordinationIntegrationRequest, "audit-"+runID+"-main-terminal-replay")
	if terminalReplay.DatabaseOutcome != DatabaseCommitted || terminalReplay.Disposition != "replay" ||
		terminalReplay.ReplayState != "succeeded" || terminalReplay.ResourceID == nil || *terminalReplay.ResourceID != projectID {
		t.Fatalf("terminal replay = %#v", terminalReplay)
	}

	failureKey := "idempotency-" + runID + "-failure"
	failureClaim := claimCoordinationIntegration(t, ctx, service, tenantID, profile, actor, scope, failureKey, coordinationIntegrationRequest, "audit-"+runID+"-failure-claim")
	if failureClaim.Disposition != "created" {
		t.Fatalf("failure claim = %#v", failureClaim)
	}
	failure, err := service.CompleteIdempotencyFailure(ctx, tenantID, IdempotencyFailureInput{
		Profile: profile, Actor: actor, Scope: scope, IdempotencyKey: failureKey,
		RequestDigest: coordinationIntegrationRequest, StableErrorCode: "stable.failure",
		AuditFactID: "audit-" + runID + "-failure-complete",
	})
	if err != nil || failure.DatabaseOutcome != DatabaseCommitted || failure.ReplayState != "failed" {
		t.Fatalf("failure completion = %#v / %v", failure, err)
	}

	assertCoordinationClaimRace(t, ctx, service, tenantID, profile, actor, scope, runID)

	claimInput := OutboxClaimInput{
		HolderID: "holder-" + runID, HolderIncarnation: "incarnation-" + runID,
		ClaimToken: "claim-" + runID + "-main", LeaseSeconds: 60,
		SubjectDigest: coordinationIntegrationRequest, AuditFactID: "audit-" + runID + "-outbox-main-claim",
	}
	claimed, err := service.ClaimOutbox(ctx, tenantID, claimInput)
	if err != nil || claimed.DatabaseOutcome != DatabaseCommitted || !claimed.Found || claimed.Claim.EventID != mainSuccess.OutboxEventID {
		t.Fatalf("main outbox claim = %#v / %v", claimed, err)
	}
	staleClaim := claimed.Claim
	staleClaim.ClaimToken += "-stale"
	stale, err := service.AcknowledgeOutbox(ctx, tenantID, OutboxSettlementInput{
		Claim: staleClaim, SubjectDigest: coordinationIntegrationRequest,
		AuditFactID: "audit-" + runID + "-outbox-stale",
	})
	if err != nil || stale != (OutboxSettlementResult{DatabaseOutcome: DatabaseRejected}) {
		t.Fatalf("stale tuple result/error = %#v / %v", stale, err)
	}
	acknowledged, err := service.AcknowledgeOutbox(ctx, tenantID, OutboxSettlementInput{
		Claim: claimed.Claim, SubjectDigest: coordinationIntegrationRequest,
		AuditFactID: "audit-" + runID + "-outbox-delivered",
	})
	if err != nil || acknowledged.DatabaseOutcome != DatabaseCommitted || acknowledged.State != "delivered" {
		t.Fatalf("acknowledge outbox = %#v / %v", acknowledged, err)
	}

	retryClaim := createAndClaimCoordinationOutbox(t, ctx, service, tenantID, profile, actor, scope, runID, "retry", 60)
	retried, err := service.RetryOutbox(ctx, tenantID, OutboxSettlementInput{
		Claim: retryClaim, SubjectDigest: coordinationIntegrationRequest,
		AuditFactID: "audit-" + runID + "-outbox-retry",
	})
	if err != nil || retried.DatabaseOutcome != DatabaseCommitted || retried.State != "retry_wait" || retried.NextAttemptAt == nil {
		t.Fatalf("retry outbox = %#v / %v", retried, err)
	}

	deadClaim := createAndClaimCoordinationOutbox(t, ctx, service, tenantID, profile, actor, scope, runID, "dead", 60)
	dead, err := service.DeadLetterOutbox(ctx, tenantID, OutboxSettlementInput{
		Claim: deadClaim, SubjectDigest: coordinationIntegrationRequest, StableErrorCode: "delivery.terminal",
		AuditFactID: "audit-" + runID + "-outbox-dead",
	})
	if err != nil || dead.DatabaseOutcome != DatabaseCommitted || dead.State != "dead_letter" {
		t.Fatalf("dead-letter outbox = %#v / %v", dead, err)
	}

	lease, err := service.AcquireLeader(ctx, LeaderLeaseInput{
		LeaderName: "outbox-dispatcher", HolderID: "leader-" + runID,
		HolderIncarnation: "leader-incarnation-" + runID, LeaseSeconds: 60,
	})
	if err != nil || lease.DatabaseOutcome != DatabaseCommitted || lease.FencingToken < 1 {
		t.Fatalf("leader acquisition = %#v / %v", lease, err)
	}
	busy, err := service.AcquireLeader(ctx, LeaderLeaseInput{
		LeaderName: "outbox-dispatcher", HolderID: "other-leader-" + runID,
		HolderIncarnation: "other-incarnation-" + runID, LeaseSeconds: 60,
	})
	if err != nil || busy.DatabaseOutcome != DatabaseRejected || busy.Disposition != "busy" || busy.FencingToken != lease.FencingToken {
		t.Fatalf("leader busy = %#v / %v", busy, err)
	}
	rejectedRenewal, err := service.RenewLeader(ctx, LeaderLeaseInput{
		LeaderName: "outbox-dispatcher", HolderID: "leader-" + runID,
		HolderIncarnation: "leader-incarnation-" + runID, FencingToken: lease.FencingToken + 1, LeaseSeconds: 60,
	})
	if err != nil || rejectedRenewal != (LeaderLeaseResult{DatabaseOutcome: DatabaseRejected, Disposition: "rejected"}) {
		t.Fatalf("stale leader renewal = %#v / %v", rejectedRenewal, err)
	}
	renewed, err := service.RenewLeader(ctx, LeaderLeaseInput{
		LeaderName: "outbox-dispatcher", HolderID: "leader-" + runID,
		HolderIncarnation: "leader-incarnation-" + runID, FencingToken: lease.FencingToken, LeaseSeconds: 60,
	})
	if err != nil || renewed.DatabaseOutcome != DatabaseCommitted || renewed.FencingToken != lease.FencingToken {
		t.Fatalf("leader renewal = %#v / %v", renewed, err)
	}

	expiredClaim := createAndClaimCoordinationOutbox(t, ctx, service, tenantID, profile, actor, scope, runID, "expired", 1)
	time.Sleep(1100 * time.Millisecond)
	staleReap, err := service.ReapExpiredOutbox(ctx, tenantID, OutboxReapInput{
		LeaderHolderID: "leader-" + runID, LeaderHolderIncarnation: "leader-incarnation-" + runID,
		FencingToken: renewed.FencingToken + 1, SubjectDigest: coordinationIntegrationRequest,
		AuditFactID: "audit-" + runID + "-reap-stale",
	})
	if err != nil || staleReap != (OutboxReapResult{DatabaseOutcome: DatabaseRejected}) {
		t.Fatalf("stale reaper = %#v / %v", staleReap, err)
	}
	reaped, err := service.ReapExpiredOutbox(ctx, tenantID, OutboxReapInput{
		LeaderHolderID: "leader-" + runID, LeaderHolderIncarnation: "leader-incarnation-" + runID,
		FencingToken: renewed.FencingToken, SubjectDigest: coordinationIntegrationRequest,
		AuditFactID: "audit-" + runID + "-reap-valid",
	})
	if err != nil || reaped.DatabaseOutcome != DatabaseCommitted || !reaped.Found ||
		reaped.EventID != expiredClaim.EventID || reaped.State != "pending" {
		t.Fatalf("valid reaper = %#v / %v", reaped, err)
	}

	assertDurableCoordinationFacts(t, ctx, verificationPool, tenantID, runID, actor)
}

func openCoordinationIntegrationPool(t *testing.T, ctx context.Context, databaseURL string, maxConns int32) *pgxpool.Pool {
	t.Helper()
	config, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		t.Fatalf("parse durable coordination database configuration: %v", err)
	}
	config.MinConns = 1
	config.MaxConns = maxConns
	config.MaxConnIdleTime = 30 * time.Second
	config.MaxConnLifetime = time.Minute
	config.HealthCheckPeriod = 5 * time.Second
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatalf("open durable coordination database: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

func claimCoordinationIntegration(
	t *testing.T,
	ctx context.Context,
	service *DurableCoordinationService,
	tenantID string,
	profile coordination.Profile,
	actor authz.SubjectRef,
	scope authz.ScopeRef,
	key string,
	requestDigest string,
	auditFactID string,
) IdempotencyClaimResult {
	t.Helper()
	result, err := service.ClaimIdempotency(ctx, tenantID, IdempotencyClaimInput{
		Profile: profile, Actor: actor, Scope: scope, IdempotencyKey: key,
		RequestDigest: requestDigest, AuditFactID: auditFactID,
	})
	if err != nil {
		t.Fatalf("claim %s: %v", key, err)
	}
	return result
}

func completeCoordinationIntegrationSuccess(
	t *testing.T,
	ctx context.Context,
	service *DurableCoordinationService,
	tenantID string,
	profile coordination.Profile,
	actor authz.SubjectRef,
	scope authz.ScopeRef,
	key string,
	requestDigest string,
	projectID string,
	eventID string,
	auditFactID string,
) IdempotencySuccessResult {
	t.Helper()
	result, err := service.CompleteIdempotencySuccess(ctx, tenantID, IdempotencySuccessInput{
		Profile: profile, Actor: actor, Scope: scope, IdempotencyKey: key, RequestDigest: requestDigest,
		ResourceID: projectID, ResourceVersion: 3, EventID: eventID,
		PayloadDigest: coordinationIntegrationPayload, AuditFactID: auditFactID,
	})
	if err != nil {
		t.Fatalf("complete %s: %v", key, err)
	}
	return result
}

func createAndClaimCoordinationOutbox(
	t *testing.T,
	ctx context.Context,
	service *DurableCoordinationService,
	tenantID string,
	profile coordination.Profile,
	actor authz.SubjectRef,
	scope authz.ScopeRef,
	runID string,
	suffix string,
	leaseSeconds int32,
) OutboxClaim {
	t.Helper()
	key := "idempotency-" + runID + "-" + suffix
	claim := claimCoordinationIntegration(t, ctx, service, tenantID, profile, actor, scope, key, coordinationIntegrationRequest, "audit-"+runID+"-"+suffix+"-claim")
	if claim.Disposition != "created" {
		t.Fatalf("%s claim = %#v", suffix, claim)
	}
	success := completeCoordinationIntegrationSuccess(
		t, ctx, service, tenantID, profile, actor, scope, key, coordinationIntegrationRequest,
		"project-"+runID, "event-"+runID+"-"+suffix, "audit-"+runID+"-"+suffix+"-success",
	)
	result, err := service.ClaimOutbox(ctx, tenantID, OutboxClaimInput{
		HolderID: "holder-" + runID, HolderIncarnation: "incarnation-" + runID,
		ClaimToken: "claim-" + runID + "-" + suffix, LeaseSeconds: leaseSeconds,
		SubjectDigest: coordinationIntegrationRequest, AuditFactID: "audit-" + runID + "-" + suffix + "-outbox-claim",
	})
	if err != nil || result.DatabaseOutcome != DatabaseCommitted || !result.Found || result.Claim.EventID != success.OutboxEventID {
		t.Fatalf("claim %s outbox = %#v / %v", suffix, result, err)
	}
	return result.Claim
}

func assertCoordinationClaimRace(
	t *testing.T,
	ctx context.Context,
	service *DurableCoordinationService,
	tenantID string,
	profile coordination.Profile,
	actor authz.SubjectRef,
	scope authz.ScopeRef,
	runID string,
) {
	t.Helper()
	key := "idempotency-" + runID + "-race"
	start := make(chan struct{})
	results := make(chan IdempotencyClaimResult, 2)
	errorsSeen := make(chan error, 2)
	var wait sync.WaitGroup
	for index := 0; index < 2; index++ {
		index := index
		wait.Add(1)
		go func() {
			defer wait.Done()
			<-start
			result, err := service.ClaimIdempotency(ctx, tenantID, IdempotencyClaimInput{
				Profile: profile, Actor: actor, Scope: scope, IdempotencyKey: key,
				RequestDigest: coordinationIntegrationRequest,
				AuditFactID:   fmt.Sprintf("audit-%s-race-%d", runID, index),
			})
			if err != nil {
				errorsSeen <- err
				return
			}
			results <- result
		}()
	}
	close(start)
	wait.Wait()
	close(results)
	close(errorsSeen)
	for err := range errorsSeen {
		t.Fatalf("concurrent claim error: %v", err)
	}
	dispositions := make([]string, 0, 2)
	for result := range results {
		if result.DatabaseOutcome == DatabaseCommitted {
			dispositions = append(dispositions, result.Disposition)
			continue
		}
		if result.DatabaseOutcome != DatabaseRejected || result.Disposition != "" {
			t.Fatalf("concurrent claim leaked result = %#v", result)
		}
	}
	sort.Strings(dispositions)
	if len(dispositions) == 0 || dispositions[0] != "created" {
		t.Fatalf("concurrent claim dispositions = %#v", dispositions)
	}
	final := claimCoordinationIntegration(t, ctx, service, tenantID, profile, actor, scope, key, coordinationIntegrationRequest, "audit-"+runID+"-race-final")
	if final.DatabaseOutcome != DatabaseCommitted || final.Disposition != "replay" || final.ReplayState != "pending" {
		t.Fatalf("concurrent final replay = %#v", final)
	}
}

func assertDurableCoordinationPrivileges(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	var callable, helpers, directDML, publicCallable int64
	err := pool.QueryRow(ctx, `SELECT
    (SELECT pg_catalog.count(*)
     FROM pg_catalog.pg_proc AS routine
     JOIN pg_catalog.pg_namespace AS namespace ON namespace.oid = routine.pronamespace
     WHERE namespace.nspname = 'cloud_agents'
       AND routine.proname IN (
         'claim_managed_agent_create_project_idempotency',
         'complete_managed_agent_create_project_success',
         'complete_managed_agent_create_project_failure',
         'acquire_coordination_leader', 'renew_coordination_leader',
         'claim_outbox_event', 'acknowledge_outbox_event', 'retry_outbox_event',
         'dead_letter_outbox_event', 'reap_expired_outbox_claim'
       )
       AND pg_catalog.has_function_privilege(current_user, routine.oid, 'EXECUTE')),
    (SELECT pg_catalog.count(*)
     FROM pg_catalog.pg_proc AS routine
     JOIN pg_catalog.pg_namespace AS namespace ON namespace.oid = routine.pronamespace
     WHERE namespace.nspname = 'cloud_agents'
       AND routine.proname IN ('append_coordination_audit', 'transition_outbox_claim')
       AND pg_catalog.has_function_privilege(current_user, routine.oid, 'EXECUTE')),
    (SELECT pg_catalog.count(*)
     FROM (VALUES
       ('platform_operations'::text), ('operation_attempts'), ('terminal_receipts'),
       ('operation_finalizers'), ('idempotency_records'), ('outbox_events'),
       ('coordination_audit_facts'), ('leader_leases')
     ) AS target(table_name)
     WHERE pg_catalog.has_table_privilege(
       current_user, pg_catalog.format('cloud_agents.%I', target.table_name),
       'INSERT,UPDATE,DELETE,TRUNCATE'
     )),
    (SELECT pg_catalog.count(*)
     FROM pg_catalog.pg_proc AS routine
     JOIN pg_catalog.pg_namespace AS namespace ON namespace.oid = routine.pronamespace
     WHERE namespace.nspname = 'cloud_agents'
       AND routine.proname IN (
         'append_coordination_audit', 'transition_outbox_claim',
         'claim_managed_agent_create_project_idempotency',
         'complete_managed_agent_create_project_success',
         'complete_managed_agent_create_project_failure',
         'acquire_coordination_leader', 'renew_coordination_leader',
         'claim_outbox_event', 'acknowledge_outbox_event', 'retry_outbox_event',
         'dead_letter_outbox_event', 'reap_expired_outbox_claim'
       )
       AND EXISTS (
         SELECT 1 FROM pg_catalog.aclexplode(
           COALESCE(routine.proacl, pg_catalog.acldefault('f', routine.proowner))
         ) AS privilege
         WHERE privilege.grantee = 0 AND privilege.privilege_type = 'EXECUTE'
       ))`).Scan(&callable, &helpers, &directDML, &publicCallable)
	if err != nil {
		t.Fatalf("read durable coordination privileges: %v", err)
	}
	if callable != 10 || helpers != 0 || directDML != 0 || publicCallable != 0 {
		t.Fatalf("durable coordination privileges = %d/%d/%d/%d", callable, helpers, directDML, publicCallable)
	}
}

func assertDurableCoordinationFacts(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	tenantID string,
	runID string,
	actor authz.SubjectRef,
) {
	t.Helper()
	transaction, err := pool.BeginTx(ctx, pgx.TxOptions{AccessMode: pgx.ReadOnly})
	if err != nil {
		t.Fatalf("begin durable coordination fact read: %v", err)
	}
	defer func() { _ = transaction.Rollback(context.Background()) }()
	if _, err := transaction.Exec(ctx, "SET ROLE cloud_agents_migration_owner"); err != nil {
		t.Fatalf("enter durable coordination verification role: %v", err)
	}
	var configured string
	if err := transaction.QueryRow(ctx, bindTenantSQL, tenantID).Scan(&configured); err != nil || configured != tenantID {
		t.Fatalf("bind durable coordination fact tenant = %q/%v", configured, err)
	}
	wantSubject, err := actor.Digest()
	if err != nil {
		t.Fatal(err)
	}
	var idempotencyCount, raceCount, unauthorizedCount, outboxCount, operationCount, finalizerCount int64
	var subjectDigests []string
	if err := transaction.QueryRow(ctx, `SELECT
    (SELECT pg_catalog.count(*) FROM cloud_agents.idempotency_records WHERE tenant_id = cloud_agents.require_tenant_id()),
    (SELECT pg_catalog.count(*) FROM cloud_agents.idempotency_records WHERE tenant_id = cloud_agents.require_tenant_id() AND idempotency_key = $1),
    (SELECT pg_catalog.count(*) FROM cloud_agents.idempotency_records WHERE tenant_id = cloud_agents.require_tenant_id() AND idempotency_key = $2),
    (SELECT pg_catalog.array_agg(DISTINCT subject_digest ORDER BY subject_digest) FROM cloud_agents.idempotency_records WHERE tenant_id = cloud_agents.require_tenant_id()),
    (SELECT pg_catalog.count(*) FROM cloud_agents.outbox_events WHERE tenant_id = cloud_agents.require_tenant_id()),
    (SELECT pg_catalog.count(*) FROM cloud_agents.platform_operations WHERE tenant_id = cloud_agents.require_tenant_id()),
    (SELECT pg_catalog.count(*) FROM cloud_agents.operation_finalizers WHERE tenant_id = cloud_agents.require_tenant_id())`,
		"idempotency-"+runID+"-race", "idempotency-"+runID+"-denied",
	).Scan(&idempotencyCount, &raceCount, &unauthorizedCount, &subjectDigests, &outboxCount, &operationCount, &finalizerCount); err != nil {
		t.Fatalf("read durable coordination facts: %v", err)
	}
	if idempotencyCount != 6 || raceCount != 1 || unauthorizedCount != 0 ||
		len(subjectDigests) != 1 || subjectDigests[0] != wantSubject || outboxCount != 4 ||
		operationCount != 0 || finalizerCount != 0 {
		t.Fatalf("durable facts = idempotency:%d race:%d denied:%d subjects:%#v outbox:%d operations:%d finalizers:%d",
			idempotencyCount, raceCount, unauthorizedCount, subjectDigests, outboxCount, operationCount, finalizerCount)
	}
	var delivered, retryWait, deadLetter, pending int64
	if err := transaction.QueryRow(ctx, `SELECT
    pg_catalog.count(*) FILTER (WHERE state = 'delivered'),
    pg_catalog.count(*) FILTER (WHERE state = 'retry_wait'),
    pg_catalog.count(*) FILTER (WHERE state = 'dead_letter'),
    pg_catalog.count(*) FILTER (WHERE state = 'pending')
FROM cloud_agents.outbox_events
WHERE tenant_id = cloud_agents.require_tenant_id()`).Scan(&delivered, &retryWait, &deadLetter, &pending); err != nil {
		t.Fatalf("read durable outbox states: %v", err)
	}
	if delivered != 1 || retryWait != 1 || deadLetter != 1 || pending != 1 {
		t.Fatalf("durable outbox states = %d/%d/%d/%d", delivered, retryWait, deadLetter, pending)
	}
	var staleAudit, reapAudit, rawSecretColumns int64
	if err := transaction.QueryRow(ctx, `SELECT
    (SELECT pg_catalog.count(*) FROM cloud_agents.coordination_audit_facts WHERE tenant_id = cloud_agents.require_tenant_id() AND audit_fact_id = $1),
    (SELECT pg_catalog.count(*) FROM cloud_agents.coordination_audit_facts WHERE tenant_id = cloud_agents.require_tenant_id() AND audit_fact_id = $2 AND fencing_token IS NOT NULL),
    (SELECT pg_catalog.count(*) FROM information_schema.columns
     WHERE table_schema = 'cloud_agents'
       AND table_name IN ('terminal_receipts','idempotency_records','outbox_events','coordination_audit_facts')
       AND column_name IN ('authorization','cookie','credential','pairing_token','pairing_url','private_key','provider_payload','raw_request','raw_response','refresh_token','secret','token_hash'))`,
		"audit-"+runID+"-outbox-stale", "audit-"+runID+"-reap-valid",
	).Scan(&staleAudit, &reapAudit, &rawSecretColumns); err != nil {
		t.Fatalf("read durable coordination audit boundary: %v", err)
	}
	if staleAudit != 0 || reapAudit != 1 || rawSecretColumns != 0 {
		t.Fatalf("audit/secret boundary = stale:%d reap:%d secrets:%d", staleAudit, reapAudit, rawSecretColumns)
	}
}
