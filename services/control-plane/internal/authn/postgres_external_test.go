package authn_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/hxp0618/cloud-agents/services/control-plane/internal/authn"
	"github.com/hxp0618/cloud-agents/services/control-plane/internal/authz"
	"github.com/hxp0618/cloud-agents/services/control-plane/internal/coordination"
	"github.com/hxp0618/cloud-agents/services/control-plane/internal/store/postgres"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const externalIdentityIssuer = "https://identity.example.test/"

type externalPostgresEnvironment struct {
	ctx              context.Context
	runtimePool      *pgxpool.Pool
	verificationPool *pgxpool.Pool
	observerPool     *pgxpool.Pool
	applicationName  string
}

func openExternalPostgresEnvironment(t *testing.T) externalPostgresEnvironment {
	t.Helper()
	if testing.Short() {
		t.Skip("external verified-principal PostgreSQL conformance is disabled in short mode")
	}
	runtimeURL := os.Getenv("CLOUD_AGENTS_TEST_DATABASE_URL")
	if runtimeURL == "" {
		if os.Getenv("CLOUD_AGENTS_REQUIRE_POSTGRES_TEST") == "1" {
			t.Fatal("CLOUD_AGENTS_TEST_DATABASE_URL is required by the external PostgreSQL conformance gate")
		}
		t.Skip("CLOUD_AGENTS_TEST_DATABASE_URL is not configured")
	}
	verificationURL := os.Getenv("CLOUD_AGENTS_TEST_MIGRATION_DATABASE_URL")
	if verificationURL == "" {
		t.Fatal("CLOUD_AGENTS_TEST_MIGRATION_DATABASE_URL is required for read-only fact verification")
	}
	observerURL := os.Getenv("CLOUD_AGENTS_TEST_OBSERVER_DATABASE_URL")
	if observerURL == "" {
		observerURL = verificationURL
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	t.Cleanup(cancel)
	applicationName := fmt.Sprintf("cag-authn-external-%d", os.Getpid())
	return externalPostgresEnvironment{
		ctx:              ctx,
		runtimePool:      openExternalPostgresPool(t, ctx, runtimeURL, 4, applicationName),
		verificationPool: openExternalPostgresPool(t, ctx, verificationURL, 2, applicationName+"-verify"),
		observerPool:     openExternalPostgresPool(t, ctx, observerURL, 1, applicationName+"-observer"),
		applicationName:  applicationName,
	}
}

func openExternalPostgresPool(
	t *testing.T,
	ctx context.Context,
	databaseURL string,
	maxConnections int32,
	applicationName string,
) *pgxpool.Pool {
	t.Helper()
	config, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		t.Fatalf("parse external PostgreSQL configuration: %v", err)
	}
	config.MinConns = 1
	config.MaxConns = maxConnections
	config.MaxConnIdleTime = 30 * time.Second
	config.MaxConnLifetime = time.Minute
	config.HealthCheckPeriod = 5 * time.Second
	config.ConnConfig.RuntimeParams["application_name"] = applicationName
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatalf("open external PostgreSQL pool: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

func newExternalPrincipal(
	t *testing.T,
	tenantID string,
	resourceLevel string,
	resourceID string,
	permission string,
	subject string,
) authn.TestPrincipalHandle {
	t.Helper()
	return authn.NewTestVerifiedPrincipal(t, authn.TestPrincipalFixture{
		SubjectKind: "user", SubjectIssuer: externalIdentityIssuer, SubjectValue: subject,
		TenantID: tenantID, ResourceLevel: resourceLevel, ResourceID: resourceID, Permission: permission,
	})
}

func withExternalVerificationTransaction(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	tenantID string,
	callback func(pgx.Tx),
) {
	t.Helper()
	transaction, err := pool.BeginTx(ctx, pgx.TxOptions{AccessMode: pgx.ReadOnly})
	if err != nil {
		t.Fatalf("begin external fact verification: %v", err)
	}
	defer func() { _ = transaction.Rollback(context.Background()) }()
	if _, err := transaction.Exec(ctx, "SET ROLE cloud_agents_migration_owner"); err != nil {
		t.Fatalf("enter migration-owner fact verification role: %v", err)
	}
	var configured string
	if err := transaction.QueryRow(ctx,
		`SELECT pg_catalog.set_config('cloud_agents.tenant_id', $1, true)`, tenantID,
	).Scan(&configured); err != nil || configured != tenantID {
		t.Fatalf("bind external fact verification tenant = %q/%v", configured, err)
	}
	callback(transaction)
}

func externalTenantRevision(t *testing.T, environment externalPostgresEnvironment, tenantID string) int64 {
	t.Helper()
	var revision int64
	withExternalVerificationTransaction(t, environment.ctx, environment.verificationPool, tenantID, func(transaction pgx.Tx) {
		if err := transaction.QueryRow(environment.ctx, `SELECT current_revision
FROM cloud_agents.tenant_resource_versions
WHERE tenant_id = cloud_agents.require_tenant_id() AND tenant_uid = $1`, tenantID).Scan(&revision); err != nil {
			t.Fatalf("read external tenant revision: %v", err)
		}
	})
	return revision
}

func TestPostgresExternalVerifiedPrincipalRBACConformance(t *testing.T) {
	environment := openExternalPostgresEnvironment(t)
	tenantID := os.Getenv("CLOUD_AGENTS_MUTATION_TENANT_ID")
	mode := os.Getenv("CLOUD_AGENTS_EXTERNAL_POSTGRES_RUN_ID")
	if (mode != "normal" && mode != "race") || tenantID != "tenant-mutation-"+mode {
		t.Fatal("external RBAC test requires its isolated normal or race tenant")
	}
	service, err := postgres.NewRBACMutationService(environment.runtimePool)
	if err != nil {
		t.Fatalf("create public RBAC mutation service: %v", err)
	}

	actor := "user-admin"
	target := authz.SubjectRef{Kind: "user", Issuer: externalIdentityIssuer, Subject: "user-external-" + mode}
	scope := authz.ScopeRef{Level: authz.ScopeTenant, ID: tenantID}
	membershipUID := "membership-external-" + mode
	bindingUID := "role-binding-external-" + mode
	revision := externalTenantRevision(t, environment, tenantID)

	createPrincipal := newExternalPrincipal(t, tenantID, "tenant", tenantID, "memberships.create", actor)
	created, err := service.CreateMembership(environment.ctx, tenantID, createPrincipal.Principal, postgres.CreateMembershipInput{
		ExpectedTenantRevision: revision, MembershipUID: membershipUID, MembershipName: membershipUID,
		Subject: target, Scope: scope, AuditFactUID: "audit-external-" + mode + "-membership-create", ReasonCode: "conformance",
	})
	if err != nil || created.TenantID != tenantID || created.ResourceUID != membershipUID ||
		created.ResourceVersion != revision+1 || created.State != authz.MembershipActive {
		t.Fatalf("public CreateMembership result/error = %#v/%v", created, err)
	}
	revision = created.ResourceVersion

	bindPrincipal := newExternalPrincipal(t, tenantID, "tenant", tenantID, "role-bindings.bind", actor)
	bound, err := service.BindRole(environment.ctx, tenantID, bindPrincipal.Principal, postgres.BindRoleInput{
		ExpectedTenantRevision: revision, RoleBindingUID: bindingUID, RoleBindingName: bindingUID,
		Subject: target, RoleName: "tenant.admin", RoleVersion: 1, Scope: scope,
		AuditFactUID: "audit-external-" + mode + "-role-bind", ReasonCode: "conformance",
	})
	if err != nil || bound.ResourceVersion != revision+1 || bound.State != authz.BindingActive {
		t.Fatalf("public BindRole result/error = %#v/%v", bound, err)
	}
	revision = bound.ResourceVersion

	suspendPrincipal := newExternalPrincipal(t, tenantID, "tenant", tenantID, "memberships.update", actor)
	suspended, err := service.SuspendMembership(environment.ctx, tenantID, suspendPrincipal.Principal, postgres.MembershipTransitionInput{
		ExpectedTenantRevision: revision, MembershipUID: membershipUID, ExpectedResourceVersion: created.ResourceVersion,
		AuditFactUID: "audit-external-" + mode + "-membership-suspend", ReasonCode: "conformance",
	})
	if err != nil || suspended.ResourceVersion != revision+1 || suspended.State != authz.MembershipSuspended {
		t.Fatalf("public SuspendMembership result/error = %#v/%v", suspended, err)
	}
	revision = suspended.ResourceVersion

	revokePrincipal := newExternalPrincipal(t, tenantID, "tenant", tenantID, "memberships.delete", actor)
	revoked, err := service.RevokeMembership(environment.ctx, tenantID, revokePrincipal.Principal, postgres.MembershipTransitionInput{
		ExpectedTenantRevision: revision, MembershipUID: membershipUID, ExpectedResourceVersion: suspended.ResourceVersion,
		AuditFactUID: "audit-external-" + mode + "-membership-revoke", ReasonCode: "conformance",
	})
	if err != nil || revoked.ResourceVersion != revision+1 || revoked.State != authz.MembershipRevoked {
		t.Fatalf("public RevokeMembership result/error = %#v/%v", revoked, err)
	}
	revision = revoked.ResourceVersion

	revokeBindingPrincipal := newExternalPrincipal(t, tenantID, "tenant", tenantID, "role-bindings.delete", actor)
	revokedBinding, err := service.RevokeRoleBinding(environment.ctx, tenantID, revokeBindingPrincipal.Principal, postgres.RevokeRoleBindingInput{
		ExpectedTenantRevision: revision, RoleBindingUID: bindingUID, ExpectedResourceVersion: bound.ResourceVersion,
		AuditFactUID: "audit-external-" + mode + "-role-revoke", ReasonCode: "conformance",
	})
	if err != nil || revokedBinding.ResourceVersion != revision+1 || revokedBinding.State != authz.BindingRevoked {
		t.Fatalf("public RevokeRoleBinding result/error = %#v/%v", revokedBinding, err)
	}
	revision = revokedBinding.ResourceVersion
	assertExternalRBACFacts(t, environment, tenantID, membershipUID, bindingUID, target, revoked.ResourceVersion, revokedBinding.ResourceVersion)

	assertRBACRejectedPathsDoNotWrite(t, environment, service, tenantID, mode, actor, scope, revision)
}

func assertExternalRBACFacts(
	t *testing.T,
	environment externalPostgresEnvironment,
	tenantID string,
	membershipUID string,
	bindingUID string,
	target authz.SubjectRef,
	membershipVersion int64,
	bindingVersion int64,
) {
	t.Helper()
	digest, err := target.Digest()
	if err != nil {
		t.Fatal(err)
	}
	withExternalVerificationTransaction(t, environment.ctx, environment.verificationPool, tenantID, func(transaction pgx.Tx) {
		var membershipState, membershipDigest, bindingState, bindingDigest string
		var storedMembershipVersion, storedBindingVersion int64
		if err := transaction.QueryRow(environment.ctx, `SELECT state, subject_digest, resource_version
FROM cloud_agents.memberships
WHERE tenant_id = cloud_agents.require_tenant_id() AND membership_uid = $1`, membershipUID,
		).Scan(&membershipState, &membershipDigest, &storedMembershipVersion); err != nil {
			t.Fatalf("read external membership fact: %v", err)
		}
		if err := transaction.QueryRow(environment.ctx, `SELECT state, subject_digest, resource_version
FROM cloud_agents.role_bindings
WHERE tenant_id = cloud_agents.require_tenant_id() AND role_binding_uid = $1`, bindingUID,
		).Scan(&bindingState, &bindingDigest, &storedBindingVersion); err != nil {
			t.Fatalf("read external role-binding fact: %v", err)
		}
		if membershipState != authz.MembershipRevoked || membershipDigest != digest || storedMembershipVersion != membershipVersion ||
			bindingState != authz.BindingRevoked || bindingDigest != digest || storedBindingVersion != bindingVersion {
			t.Fatalf("external RBAC facts = membership:%s/%s/%d binding:%s/%s/%d",
				membershipState, membershipDigest, storedMembershipVersion, bindingState, bindingDigest, storedBindingVersion)
		}
	})
}

func assertRBACRejectedPathsDoNotWrite(
	t *testing.T,
	environment externalPostgresEnvironment,
	service *postgres.RBACMutationService,
	tenantID string,
	mode string,
	actor string,
	scope authz.ScopeRef,
	revision int64,
) {
	t.Helper()
	input := func(suffix string) postgres.CreateMembershipInput {
		uid := "membership-external-" + mode + "-" + suffix
		return postgres.CreateMembershipInput{
			ExpectedTenantRevision: revision, MembershipUID: uid, MembershipName: uid,
			Subject: authz.SubjectRef{Kind: "user", Issuer: externalIdentityIssuer, Subject: "target-" + suffix}, Scope: scope,
			AuditFactUID: "audit-external-" + mode + "-" + suffix, ReasonCode: "conformance",
		}
	}

	denied := newExternalPrincipal(t, tenantID, "tenant", tenantID, "memberships.create", "user-denied-"+mode)
	if _, err := service.CreateMembership(environment.ctx, tenantID, denied.Principal, input("denied")); !errors.Is(err, postgres.ErrMutationDenied) {
		t.Fatalf("unentitled public principal error = %v", err)
	}

	stale := newExternalPrincipal(t, tenantID, "tenant", tenantID, "memberships.create", actor)
	if err := stale.Invalidate(); err != nil {
		t.Fatal(err)
	}
	if _, err := service.CreateMembership(environment.ctx, tenantID, stale.Principal, input("stale")); err == nil {
		t.Fatal("stale public principal reached the mutation service")
	}

	mismatch := newExternalPrincipal(t, tenantID, "tenant", tenantID, "memberships.update", actor)
	if _, err := service.CreateMembership(environment.ctx, tenantID, mismatch.Principal, input("context-mismatch")); !errors.Is(err, postgres.ErrMutationDenied) {
		t.Fatalf("permission-mismatched public principal error = %v", err)
	}

	canceledContext, cancel := context.WithCancel(environment.ctx)
	cancel()
	canceled := newExternalPrincipal(t, tenantID, "tenant", tenantID, "memberships.create", actor)
	if _, err := service.CreateMembership(canceledContext, tenantID, canceled.Principal, input("canceled")); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled public mutation error = %v", err)
	}

	if actual := externalTenantRevision(t, environment, tenantID); actual != revision {
		t.Fatalf("rejected public paths changed tenant revision: got %d want %d", actual, revision)
	}
	withExternalVerificationTransaction(t, environment.ctx, environment.verificationPool, tenantID, func(transaction pgx.Tx) {
		var count int64
		if err := transaction.QueryRow(environment.ctx, `SELECT pg_catalog.count(*)
FROM cloud_agents.memberships
WHERE tenant_id = cloud_agents.require_tenant_id() AND membership_uid = ANY($1::text[])`, []string{
			"membership-external-" + mode + "-denied",
			"membership-external-" + mode + "-stale",
			"membership-external-" + mode + "-context-mismatch",
			"membership-external-" + mode + "-canceled",
		}).Scan(&count); err != nil {
			t.Fatalf("verify rejected public RBAC paths: %v", err)
		}
		if count != 0 {
			t.Fatalf("rejected public RBAC paths persisted %d memberships", count)
		}
	})
}

type externalMutationOutcome struct {
	result postgres.MutationResult
	err    error
}

func lockExternalTenantRevision(
	t *testing.T,
	environment externalPostgresEnvironment,
	tenantID string,
) (pgx.Tx, int64) {
	t.Helper()
	transaction, err := environment.verificationPool.BeginTx(environment.ctx, pgx.TxOptions{})
	if err != nil {
		t.Fatalf("begin external revision lock transaction: %v", err)
	}
	t.Cleanup(func() { _ = transaction.Rollback(context.Background()) })
	if _, err := transaction.Exec(environment.ctx, "SET ROLE cloud_agents_migration_owner"); err != nil {
		t.Fatalf("enter revision lock owner role: %v", err)
	}
	var configured string
	if err := transaction.QueryRow(environment.ctx,
		`SELECT pg_catalog.set_config('cloud_agents.tenant_id', $1, true)`, tenantID,
	).Scan(&configured); err != nil || configured != tenantID {
		t.Fatalf("bind revision lock tenant = %q/%v", configured, err)
	}
	var revision int64
	if err := transaction.QueryRow(environment.ctx, `SELECT current_revision
FROM cloud_agents.tenant_resource_versions
WHERE tenant_id = cloud_agents.require_tenant_id() AND tenant_uid = $1
FOR UPDATE`, tenantID).Scan(&revision); err != nil {
		t.Fatalf("lock external tenant revision: %v", err)
	}
	return transaction, revision
}

func waitForExternalRuntimeLock(
	t *testing.T,
	environment externalPostgresEnvironment,
	outcomes <-chan externalMutationOutcome,
) {
	t.Helper()
	waitContext, cancel := context.WithTimeout(environment.ctx, 5*time.Second)
	defer cancel()
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case outcome := <-outcomes:
			t.Fatalf("public mutation settled before the expected lock wait: result=%#v err=%v", outcome.result, outcome.err)
		default:
		}
		var waiting bool
		err := environment.observerPool.QueryRow(waitContext, `SELECT EXISTS (
    SELECT 1
    FROM pg_catalog.pg_stat_activity AS activity
    WHERE activity.datname = pg_catalog.current_database()
      AND activity.usename = 'cag_runtime'
      AND activity.application_name = $1
      AND activity.state = 'active'
      AND activity.wait_event_type = 'Lock'
)`, environment.applicationName).Scan(&waiting)
		if err != nil {
			t.Fatalf("observe external runtime lock wait: %v", err)
		}
		if waiting {
			return
		}
		select {
		case <-waitContext.Done():
			t.Fatalf("public mutation did not enter an observable lock wait: %v", waitContext.Err())
		case <-ticker.C:
		}
	}
}

func waitForExternalMutation(t *testing.T, outcomes <-chan externalMutationOutcome) externalMutationOutcome {
	t.Helper()
	select {
	case outcome := <-outcomes:
		return outcome
	case <-time.After(5 * time.Second):
		t.Fatal("public mutation did not settle within the bounded wait")
		return externalMutationOutcome{}
	}
}

func TestPostgresExternalVerifiedPrincipalLeaseThroughCommitAndCancelRollback(t *testing.T) {
	environment := openExternalPostgresEnvironment(t)
	tenantID := os.Getenv("CLOUD_AGENTS_MUTATION_TENANT_ID")
	mode := os.Getenv("CLOUD_AGENTS_EXTERNAL_POSTGRES_RUN_ID")
	if (mode != "normal" && mode != "race") || tenantID != "tenant-mutation-"+mode {
		t.Fatal("external lease test requires its isolated normal or race tenant")
	}
	service, err := postgres.NewRBACMutationService(environment.runtimePool)
	if err != nil {
		t.Fatalf("create public lease-conformance service: %v", err)
	}
	scope := authz.ScopeRef{Level: authz.ScopeTenant, ID: tenantID}

	lockTransaction, revision := lockExternalTenantRevision(t, environment, tenantID)
	leaseUID := "membership-external-lease-" + mode
	leaseSubject := authz.SubjectRef{Kind: "user", Issuer: externalIdentityIssuer, Subject: "user-external-lease-" + mode}
	handle := newExternalPrincipal(t, tenantID, "tenant", tenantID, "memberships.create", "user-admin")
	mutationDone := make(chan externalMutationOutcome, 1)
	go func() {
		result, mutationErr := service.CreateMembership(environment.ctx, tenantID, handle.Principal, postgres.CreateMembershipInput{
			ExpectedTenantRevision: revision, MembershipUID: leaseUID, MembershipName: leaseUID,
			Subject: leaseSubject, Scope: scope,
			AuditFactUID: "audit-external-lease-" + mode, ReasonCode: "conformance",
		})
		mutationDone <- externalMutationOutcome{result: result, err: mutationErr}
	}()
	waitForExternalRuntimeLock(t, environment, mutationDone)

	invalidateStarted := make(chan struct{})
	invalidateDone := make(chan error, 1)
	go func() {
		close(invalidateStarted)
		invalidateDone <- handle.Invalidate()
	}()
	<-invalidateStarted
	select {
	case invalidateErr := <-invalidateDone:
		t.Fatalf("principal invalidation crossed the observed store lock wait: %v", invalidateErr)
	case <-time.After(50 * time.Millisecond):
	}
	// Re-observe the database wait after the bounded invalidation assertion;
	// this prevents scheduler delay from being mistaken for lease evidence.
	waitForExternalRuntimeLock(t, environment, mutationDone)
	if err := lockTransaction.Commit(environment.ctx); err != nil {
		t.Fatalf("release external revision lock: %v", err)
	}
	outcome := waitForExternalMutation(t, mutationDone)
	if outcome.err != nil || outcome.result.ResourceUID != leaseUID || outcome.result.ResourceVersion != revision+1 ||
		outcome.result.State != authz.MembershipActive {
		t.Fatalf("lease-through-commit result/error = %#v/%v", outcome.result, outcome.err)
	}
	select {
	case invalidateErr := <-invalidateDone:
		if invalidateErr != nil {
			t.Fatalf("post-commit invalidation error: %v", invalidateErr)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("invalidation did not finish after the public mutation settled")
	}
	assertExternalCreatedMembership(
		t, environment, tenantID, leaseUID, "audit-external-lease-"+mode, leaseSubject, outcome.result.ResourceVersion,
	)

	cancelLock, cancelRevision := lockExternalTenantRevision(t, environment, tenantID)
	cancelUID := "membership-external-cancel-" + mode
	cancelContext, cancel := context.WithCancel(environment.ctx)
	cancelHandle := newExternalPrincipal(t, tenantID, "tenant", tenantID, "memberships.create", "user-admin")
	cancelDone := make(chan externalMutationOutcome, 1)
	go func() {
		result, mutationErr := service.CreateMembership(cancelContext, tenantID, cancelHandle.Principal, postgres.CreateMembershipInput{
			ExpectedTenantRevision: cancelRevision, MembershipUID: cancelUID, MembershipName: cancelUID,
			Subject: authz.SubjectRef{Kind: "user", Issuer: externalIdentityIssuer, Subject: "user-external-cancel-" + mode}, Scope: scope,
			AuditFactUID: "audit-external-cancel-" + mode, ReasonCode: "conformance",
		})
		cancelDone <- externalMutationOutcome{result: result, err: mutationErr}
	}()
	waitForExternalRuntimeLock(t, environment, cancelDone)
	cancelInvalidateStarted := make(chan struct{})
	cancelInvalidateDone := make(chan error, 1)
	go func() {
		close(cancelInvalidateStarted)
		cancelInvalidateDone <- cancelHandle.Invalidate()
	}()
	<-cancelInvalidateStarted
	select {
	case invalidateErr := <-cancelInvalidateDone:
		t.Fatalf("principal invalidation crossed the canceled-path store lock wait: %v", invalidateErr)
	case <-time.After(50 * time.Millisecond):
	}
	waitForExternalRuntimeLock(t, environment, cancelDone)
	cancel()
	canceledOutcome := waitForExternalMutation(t, cancelDone)
	if canceledOutcome.result != (postgres.MutationResult{}) || !errors.Is(canceledOutcome.err, context.Canceled) {
		t.Fatalf("canceled lock-wait result/error = %#v/%v", canceledOutcome.result, canceledOutcome.err)
	}
	select {
	case invalidateErr := <-cancelInvalidateDone:
		if invalidateErr != nil {
			t.Fatalf("post-rollback invalidation error: %v", invalidateErr)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("invalidation did not finish after the canceled mutation settled")
	}
	if err := cancelLock.Rollback(environment.ctx); err != nil {
		t.Fatalf("release canceled-path revision lock: %v", err)
	}
	if actual := externalTenantRevision(t, environment, tenantID); actual != cancelRevision {
		t.Fatalf("canceled lock-wait changed tenant revision: got %d want %d", actual, cancelRevision)
	}
	withExternalVerificationTransaction(t, environment.ctx, environment.verificationPool, tenantID, func(transaction pgx.Tx) {
		var memberships, changes, audits int64
		if err := transaction.QueryRow(environment.ctx, `SELECT
    (SELECT pg_catalog.count(*) FROM cloud_agents.memberships
     WHERE tenant_id = cloud_agents.require_tenant_id() AND membership_uid = $1),
    (SELECT pg_catalog.count(*) FROM cloud_agents.resource_changes
     WHERE tenant_id = cloud_agents.require_tenant_id() AND resource_kind = 'membership' AND resource_uid = $1),
    (SELECT pg_catalog.count(*) FROM cloud_agents.audit_facts
     WHERE tenant_id = cloud_agents.require_tenant_id() AND audit_fact_uid = $2)`,
			cancelUID, "audit-external-cancel-"+mode,
		).Scan(&memberships, &changes, &audits); err != nil {
			t.Fatalf("verify canceled lock-wait fact absence: %v", err)
		}
		if memberships != 0 || changes != 0 || audits != 0 {
			t.Fatalf("canceled lock-wait persisted membership/change/audit = %d/%d/%d", memberships, changes, audits)
		}
	})
}

func assertExternalCreatedMembership(
	t *testing.T,
	environment externalPostgresEnvironment,
	tenantID string,
	membershipUID string,
	auditFactUID string,
	subject authz.SubjectRef,
	resourceVersion int64,
) {
	t.Helper()
	digest, err := subject.Digest()
	if err != nil {
		t.Fatal(err)
	}
	withExternalVerificationTransaction(t, environment.ctx, environment.verificationPool, tenantID, func(transaction pgx.Tx) {
		var state, storedDigest string
		var storedVersion, changes, audits int64
		if err := transaction.QueryRow(environment.ctx, `SELECT
    membership.state,
    membership.subject_digest,
    membership.resource_version,
    (SELECT pg_catalog.count(*) FROM cloud_agents.resource_changes AS change
     WHERE change.tenant_id = cloud_agents.require_tenant_id()
       AND change.resource_version = membership.resource_version
       AND change.resource_kind = 'membership'
       AND change.resource_uid = membership.membership_uid),
    (SELECT pg_catalog.count(*) FROM cloud_agents.audit_facts AS audit
     WHERE audit.tenant_id = cloud_agents.require_tenant_id()
       AND audit.audit_fact_uid = $2
       AND audit.resource_version = membership.resource_version)
FROM cloud_agents.memberships AS membership
WHERE membership.tenant_id = cloud_agents.require_tenant_id() AND membership.membership_uid = $1`,
			membershipUID, auditFactUID,
		).Scan(&state, &storedDigest, &storedVersion, &changes, &audits); err != nil {
			t.Fatalf("read lease-through-commit membership: %v", err)
		}
		if state != authz.MembershipActive || storedDigest != digest || storedVersion != resourceVersion || changes != 1 || audits != 1 {
			t.Fatalf("lease-through-commit membership/change/audit = %s/%s/%d/%d/%d",
				state, storedDigest, storedVersion, changes, audits)
		}
	})
}

func TestPostgresExternalVerifiedPrincipalDurableCoordinationConformance(t *testing.T) {
	environment := openExternalPostgresEnvironment(t)
	tenantID := os.Getenv("CLOUD_AGENTS_COORDINATION_TENANT_ID")
	mode := os.Getenv("CLOUD_AGENTS_COORDINATION_RUN_ID")
	if (mode != "normal" && mode != "race") || tenantID != "tenant-coordination-"+mode {
		t.Fatal("external durable-coordination test requires its isolated normal or race tenant")
	}
	service, err := postgres.NewDurableCoordinationService(environment.runtimePool)
	if err != nil {
		t.Fatalf("create public durable coordination service: %v", err)
	}
	profile := coordination.ManagedAgentCreateProject()
	request := externalManagedAgentRequest(mode)
	actor := "user-admin"
	permission := "projects.create"
	organizationID := "organization-" + mode
	prefix := "external-" + mode

	mainKey := "idempotency-" + prefix + "-success"
	claimPrincipal := newExternalPrincipal(t, tenantID, "organization", organizationID, permission, actor)
	claim, err := service.ClaimIdempotency(environment.ctx, tenantID, claimPrincipal.Principal, postgres.IdempotencyClaimInput{
		Profile: profile, Request: request, IdempotencyKey: mainKey, AuditFactID: "audit-" + prefix + "-claim",
	})
	if err != nil || claim.DatabaseOutcome != postgres.DatabaseCommitted || claim.Disposition != "created" || claim.ReplayState != "pending" {
		t.Fatalf("public ClaimIdempotency result/error = %#v/%v", claim, err)
	}

	successPrincipal := newExternalPrincipal(t, tenantID, "organization", organizationID, permission, actor)
	success, err := service.CompleteIdempotencySuccess(environment.ctx, tenantID, successPrincipal.Principal, postgres.IdempotencySuccessInput{
		Profile: profile, Request: request, IdempotencyKey: mainKey,
		ResourceID: "project-" + mode, ResourceVersion: 3, EventID: "event-" + prefix,
		PayloadDigest: "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		AuditFactID:   "audit-" + prefix + "-success",
	})
	if err != nil || success.DatabaseOutcome != postgres.DatabaseCommitted || success.ReplayState != "succeeded" ||
		success.ResourceID != "project-"+mode || success.OutboxEventID != "event-"+prefix || success.OutboxState != "pending" {
		t.Fatalf("public CompleteIdempotencySuccess result/error = %#v/%v", success, err)
	}

	failureKey := "idempotency-" + prefix + "-failure"
	failureClaimPrincipal := newExternalPrincipal(t, tenantID, "organization", organizationID, permission, actor)
	failureClaim, err := service.ClaimIdempotency(environment.ctx, tenantID, failureClaimPrincipal.Principal, postgres.IdempotencyClaimInput{
		Profile: profile, Request: request, IdempotencyKey: failureKey, AuditFactID: "audit-" + prefix + "-failure-claim",
	})
	if err != nil || failureClaim.DatabaseOutcome != postgres.DatabaseCommitted || failureClaim.Disposition != "created" {
		t.Fatalf("public failure ClaimIdempotency result/error = %#v/%v", failureClaim, err)
	}
	failurePrincipal := newExternalPrincipal(t, tenantID, "organization", organizationID, permission, actor)
	failure, err := service.CompleteIdempotencyFailure(environment.ctx, tenantID, failurePrincipal.Principal, postgres.IdempotencyFailureInput{
		Profile: profile, Request: request, IdempotencyKey: failureKey,
		StableErrorCode: "external.failure", AuditFactID: "audit-" + prefix + "-failure",
	})
	if err != nil || failure.DatabaseOutcome != postgres.DatabaseCommitted || failure.ReplayState != "failed" || failure.StableErrorCode != "external.failure" {
		t.Fatalf("public CompleteIdempotencyFailure result/error = %#v/%v", failure, err)
	}

	assertExternalDurableStatementRejections(
		t, environment, service, tenantID, mode, actor, organizationID, profile, request, mainKey, failureKey,
	)
	assertDurableRejectedPathsDoNotWrite(t, environment, service, tenantID, mode, actor, profile, request)
	assertExternalDurableFacts(t, environment, tenantID, mode, actor)
}

func assertExternalDurableStatementRejections(
	t *testing.T,
	environment externalPostgresEnvironment,
	service *postgres.DurableCoordinationService,
	tenantID string,
	mode string,
	actor string,
	organizationID string,
	profile coordination.Profile,
	request coordination.ManagedAgentCreateProjectRequest,
	succeededKey string,
	failedKey string,
) {
	t.Helper()
	prefix := "external-" + mode
	permission := "projects.create"
	rejectedClaimKey := "idempotency-" + prefix + "-statement-rejected"
	claimPrincipal := newExternalPrincipal(t, tenantID, "organization", organizationID, permission, actor)
	claim, err := service.ClaimIdempotency(environment.ctx, tenantID, claimPrincipal.Principal, postgres.IdempotencyClaimInput{
		Profile: profile, Request: request, IdempotencyKey: rejectedClaimKey,
		// Reusing a committed audit identifier deterministically raises 23505
		// inside the protected claim statement. The whole claim transaction
		// must roll back and settle as a typed database rejection.
		AuditFactID: "audit-" + prefix + "-claim",
	})
	if err != nil || claim != (postgres.IdempotencyClaimResult{DatabaseOutcome: postgres.DatabaseRejected}) {
		t.Fatalf("public ClaimIdempotency statement rejection = %#v/%v", claim, err)
	}

	rejectedSuccessEvent := "event-" + prefix + "-statement-rejected"
	successPrincipal := newExternalPrincipal(t, tenantID, "organization", organizationID, permission, actor)
	success, err := service.CompleteIdempotencySuccess(environment.ctx, tenantID, successPrincipal.Principal, postgres.IdempotencySuccessInput{
		Profile: profile, Request: request, IdempotencyKey: failedKey,
		ResourceID: "project-" + mode, ResourceVersion: 3, EventID: rejectedSuccessEvent,
		PayloadDigest: "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		AuditFactID:   "audit-" + prefix + "-statement-success",
	})
	if err != nil || success != (postgres.IdempotencySuccessResult{DatabaseOutcome: postgres.DatabaseRejected}) {
		t.Fatalf("public CompleteIdempotencySuccess statement rejection = %#v/%v", success, err)
	}

	failurePrincipal := newExternalPrincipal(t, tenantID, "organization", organizationID, permission, actor)
	failure, err := service.CompleteIdempotencyFailure(environment.ctx, tenantID, failurePrincipal.Principal, postgres.IdempotencyFailureInput{
		Profile: profile, Request: request, IdempotencyKey: succeededKey,
		StableErrorCode: "external.statement-rejected", AuditFactID: "audit-" + prefix + "-statement-failure",
	})
	if err != nil || failure != (postgres.IdempotencyFailureResult{DatabaseOutcome: postgres.DatabaseRejected}) {
		t.Fatalf("public CompleteIdempotencyFailure statement rejection = %#v/%v", failure, err)
	}

	withExternalVerificationTransaction(t, environment.ctx, environment.verificationPool, tenantID, func(transaction pgx.Tx) {
		var rejectedRecords, rejectedEvents, rejectedAudits, originalAuditFacts int64
		if err := transaction.QueryRow(environment.ctx, `SELECT
    (SELECT pg_catalog.count(*) FROM cloud_agents.idempotency_records
     WHERE tenant_id = cloud_agents.require_tenant_id() AND idempotency_key = $1),
    (SELECT pg_catalog.count(*) FROM cloud_agents.outbox_events
     WHERE tenant_id = cloud_agents.require_tenant_id() AND event_id = $2),
    (SELECT pg_catalog.count(*) FROM cloud_agents.coordination_audit_facts
     WHERE tenant_id = cloud_agents.require_tenant_id() AND audit_fact_id = ANY($3::text[])),
    (SELECT pg_catalog.count(*) FROM cloud_agents.coordination_audit_facts
     WHERE tenant_id = cloud_agents.require_tenant_id() AND audit_fact_id = $4)`,
			rejectedClaimKey, rejectedSuccessEvent,
			[]string{"audit-" + prefix + "-statement-success", "audit-" + prefix + "-statement-failure"},
			"audit-"+prefix+"-claim",
		).Scan(&rejectedRecords, &rejectedEvents, &rejectedAudits, &originalAuditFacts); err != nil {
			t.Fatalf("verify public durable statement rejection facts: %v", err)
		}
		if rejectedRecords != 0 || rejectedEvents != 0 || rejectedAudits != 0 || originalAuditFacts != 1 {
			t.Fatalf("statement rejection persisted record/event/audit/original = %d/%d/%d/%d",
				rejectedRecords, rejectedEvents, rejectedAudits, originalAuditFacts)
		}
	})
}

func externalManagedAgentRequest(mode string) coordination.ManagedAgentCreateProjectRequest {
	return coordination.ManagedAgentCreateProjectRequest{
		Name: "project-" + mode,
		OrganizationRef: coordination.OrganizationRef{
			Namespace: "cloud-agents", Kind: "organization", ID: "organization-" + mode,
		},
		DisplayName: "External project " + mode,
	}
}

func assertDurableRejectedPathsDoNotWrite(
	t *testing.T,
	environment externalPostgresEnvironment,
	service *postgres.DurableCoordinationService,
	tenantID string,
	mode string,
	actor string,
	profile coordination.Profile,
	request coordination.ManagedAgentCreateProjectRequest,
) {
	t.Helper()
	organizationID := "organization-" + mode
	claim := func(suffix string) postgres.IdempotencyClaimInput {
		return postgres.IdempotencyClaimInput{
			Profile: profile, Request: request, IdempotencyKey: "idempotency-external-" + mode + "-" + suffix,
			AuditFactID: "audit-external-" + mode + "-" + suffix,
		}
	}
	denied := newExternalPrincipal(t, tenantID, "organization", organizationID, "projects.create", "user-denied-"+mode)
	if _, err := service.ClaimIdempotency(environment.ctx, tenantID, denied.Principal, claim("denied")); !errors.Is(err, postgres.ErrMutationDenied) {
		t.Fatalf("unentitled durable principal error = %v", err)
	}
	stale := newExternalPrincipal(t, tenantID, "organization", organizationID, "projects.create", actor)
	if err := stale.Invalidate(); err != nil {
		t.Fatal(err)
	}
	if _, err := service.ClaimIdempotency(environment.ctx, tenantID, stale.Principal, claim("stale")); err == nil {
		t.Fatal("stale principal reached durable coordination")
	}
	mismatch := newExternalPrincipal(t, tenantID, "organization", "organization-mismatch-"+mode, "projects.create", actor)
	if _, err := service.ClaimIdempotency(environment.ctx, tenantID, mismatch.Principal, claim("context-mismatch")); !errors.Is(err, postgres.ErrMutationDenied) {
		t.Fatalf("scope-mismatched durable principal error = %v", err)
	}
	canceledContext, cancel := context.WithCancel(environment.ctx)
	cancel()
	canceled := newExternalPrincipal(t, tenantID, "organization", organizationID, "projects.create", actor)
	if _, err := service.ClaimIdempotency(canceledContext, tenantID, canceled.Principal, claim("canceled")); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled durable claim error = %v", err)
	}
}

func assertExternalDurableFacts(
	t *testing.T,
	environment externalPostgresEnvironment,
	tenantID string,
	mode string,
	actor string,
) {
	t.Helper()
	digest, err := (authz.SubjectRef{Kind: "user", Issuer: externalIdentityIssuer, Subject: actor}).Digest()
	if err != nil {
		t.Fatal(err)
	}
	prefix := "external-" + mode
	withExternalVerificationTransaction(t, environment.ctx, environment.verificationPool, tenantID, func(transaction pgx.Tx) {
		var records, succeeded, failed, deniedRecords, outboxEvents, audits int64
		var subjectDigests []string
		if err := transaction.QueryRow(environment.ctx, `SELECT
    pg_catalog.count(*),
    pg_catalog.count(*) FILTER (WHERE state = 'succeeded'),
    pg_catalog.count(*) FILTER (WHERE state = 'failed'),
    pg_catalog.count(*) FILTER (WHERE idempotency_key IN ($3, $4, $5, $6)),
    pg_catalog.array_agg(DISTINCT subject_digest ORDER BY subject_digest)
FROM cloud_agents.idempotency_records
WHERE tenant_id = cloud_agents.require_tenant_id() AND idempotency_key IN ($1, $2, $3, $4, $5, $6)`,
			"idempotency-"+prefix+"-success", "idempotency-"+prefix+"-failure",
			"idempotency-"+prefix+"-denied", "idempotency-"+prefix+"-stale",
			"idempotency-"+prefix+"-context-mismatch", "idempotency-"+prefix+"-canceled",
		).Scan(&records, &succeeded, &failed, &deniedRecords, &subjectDigests); err != nil {
			t.Fatalf("read public durable idempotency facts: %v", err)
		}
		if err := transaction.QueryRow(environment.ctx, `SELECT pg_catalog.count(*)
FROM cloud_agents.outbox_events
WHERE tenant_id = cloud_agents.require_tenant_id() AND event_id = $1`, "event-"+prefix).Scan(&outboxEvents); err != nil {
			t.Fatalf("read public durable outbox facts: %v", err)
		}
		if err := transaction.QueryRow(environment.ctx, `SELECT pg_catalog.count(*)
FROM cloud_agents.coordination_audit_facts
WHERE tenant_id = cloud_agents.require_tenant_id() AND audit_fact_id = ANY($1::text[])`, []string{
			"audit-" + prefix + "-claim", "audit-" + prefix + "-success",
			"audit-" + prefix + "-failure-claim", "audit-" + prefix + "-failure",
		}).Scan(&audits); err != nil {
			t.Fatalf("read public durable audit facts: %v", err)
		}
		if records != 2 || succeeded != 1 || failed != 1 || deniedRecords != 0 || len(subjectDigests) != 1 ||
			subjectDigests[0] != digest || outboxEvents != 1 || audits != 4 {
			t.Fatalf("public durable facts = records:%d succeeded:%d failed:%d denied:%d subjects:%#v outbox:%d audits:%d",
				records, succeeded, failed, deniedRecords, subjectDigests, outboxEvents, audits)
		}
	})
}
