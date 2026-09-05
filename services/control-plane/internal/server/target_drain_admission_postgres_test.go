package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	api "github.com/hxp0618/cloud-agents/sdk/go/gen/openapi/v1alpha1"
	platform "github.com/hxp0618/cloud-agents/sdk/go/gen/platform/v1alpha1"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Real Admin/User HTTP + PostgreSQL admission tests; Lease readiness is an explicit SQL fixture.
// No Worker/Provider is deployed or invoked. This is not three-target or Provider E2E evidence.
func TestTargetDrainAdmissionPostgres(t *testing.T) {
	runtimeURL := os.Getenv("CLOUD_AGENTS_DRAIN_TEST_RUNTIME_DATABASE_URL")
	if runtimeURL == "" {
		t.Skip("isolated Drain PostgreSQL environment not configured")
	}
	migrationURL, project, stateDir := os.Getenv("CLOUD_AGENTS_DRAIN_TEST_MIGRATION_DATABASE_URL"), os.Getenv("CLOUD_AGENTS_DRAIN_TEST_PROJECT_ID"), os.Getenv("CLOUD_AGENTS_DRAIN_TEST_DEV_STATE")
	if migrationURL == "" || project == "" || stateDir == "" {
		t.Fatal("complete disposable Drain test configuration required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	runtimePool, err := pgxpool.New(ctx, runtimeURL)
	if err != nil {
		t.Fatal("runtime pool unavailable")
	}
	defer runtimePool.Close()
	config, err := pgxpool.ParseConfig(migrationURL)
	if err != nil {
		t.Fatal("migration configuration invalid")
	}
	config.AfterConnect = func(ctx context.Context, conn *pgx.Conn) error {
		_, err := conn.Exec(ctx, "SET ROLE cloud_agents_migration_owner")
		return err
	}
	owner, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatal("migration pool unavailable")
	}
	defer owner.Close()
	var existing int
	if err := owner.QueryRow(ctx, `SELECT (SELECT count(*) FROM cloud_agents.deployment_targets WHERE tenant_id='tenant-local' AND project_uid=$1)+(SELECT count(*) FROM cloud_agents.managed_agent_sessions WHERE tenant_id='tenant-local' AND project_uid=$1)+(SELECT count(*) FROM cloud_agents.managed_host_environment_leases WHERE tenant_id='tenant-local' AND project_uid=$1)`, project).Scan(&existing); err != nil || existing != 0 {
		t.Fatal("requires an empty disposable project")
	}
	client := func(file string) *api.Client {
		t.Helper()
		token, err := os.ReadFile(stateDir + "/" + file)
		if err != nil {
			t.Fatal("local token missing")
		}
		client, err := api.NewHTTPClient("http://127.0.0.1:18085", strings.TrimSpace(string(token)))
		if err != nil {
			t.Fatal(err)
		}
		return client
	}
	admin, user := client("control-plane-admin.token"), client("control-plane.token")
	key := func() string { return fmt.Sprintf("drain-test-%d", time.Now().UnixNano()) }
	_, err = admin.RegisterAdminDeploymentTarget(ctx, "tenant-local", project, key(), key(), platform.DeploymentTargetRegisterRequest{TargetID: "drain-target", TargetName: "drain-target", TargetKind: "docker", Endpoint: "https://127.0.0.1:1", CredentialRef: "drain-unconfigured"})
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		for _, table := range []string{"managed_agent_events", "managed_agent_executions", "managed_agent_turns", "managed_agent_sessions", "managed_host_environment_leases", "deployment_target_activity", "deployment_targets"} {
			if _, err := owner.Exec(cleanup, "DELETE FROM cloud_agents."+table+" WHERE tenant_id='tenant-local' AND project_uid=$1", project); err != nil {
				t.Errorf("own fixture cleanup %s: %v", table, err)
			}
		}
	}()
	_, err = owner.Exec(ctx, `INSERT INTO cloud_agents.managed_host_environment_leases (
 tenant_id,tenant_ref_id,project_uid,lease_uid,lease_name,release_digest,generation,desired_phase,observed_phase,cleanup_phase,environment_id,expires_at,resource_version,create_idempotency_key,create_request_digest,created_at,updated_at,
 deployment_target_uid,deployment_target_generation,provider_credential_ref,cpu_limit_millis,memory_limit_bytes,worker_endpoint,worker_spiffe_id,worker_server_name)
 VALUES ('tenant-local','tenant-local',$1,'lease-drain','lease-drain','sha256:'||repeat('1',64),1,'active','ready','none','lease-drain',clock_timestamp()+interval '1 hour',1,'drain-lease-fixture','sha256:'||repeat('2',64),clock_timestamp(),clock_timestamp(),
 'drain-target',1,'drain-provider-not-configured',1000,536870912,'https://127.0.0.1:1','spiffe://drain.test/worker/lease-drain','drain.test')`, project)
	if err != nil {
		t.Fatal(err)
	}
	status := func(err error, want int) {
		t.Helper()
		var failure *api.ClientError
		if !errors.As(err, &failure) || failure.Status != want {
			t.Fatalf("want HTTP%d: %v", want, err)
		}
	}
	session := func(id, k string) error {
		_, err := user.CreateManagedAgentSession(ctx, "tenant-local", project, key(), k, api.ManagedAgentSessionCreateRequest{SessionID: id, ProviderKind: "codex", EnvironmentLeaseID: "lease-drain"})
		return err
	}
	turn := func(sessionID, id, k string) error {
		_, err := user.CreateManagedAgentTurn(ctx, "tenant-local", project, sessionID, key(), k, api.ManagedAgentTurnCreateRequest{TurnID: id, InputText: "admission-test-content-not-for-admin"})
		return err
	}
	sessionKey, turnKey := key(), key()
	for _, id := range []string{"session-idle", "session-running", "session-queued", "session-create"} {
		k := key()
		if id == "session-idle" {
			k = sessionKey
		}
		if err := session(id, k); err != nil {
			t.Fatal(err)
		}
	}
	for _, id := range []string{"session-running", "session-queued", "session-create"} {
		k := key()
		if id == "session-running" {
			k = turnKey
		}
		if err := turn(id, "turn-one", k); err != nil {
			t.Fatal(err)
		}
	}
	begin := func() pgx.Tx {
		t.Helper()
		tx, err := runtimePool.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := tx.Exec(ctx, "SET LOCAL ROLE cloud_agents_runtime; SELECT set_config('cloud_agents.tenant_id','tenant-local',true)"); err != nil {
			tx.Rollback(ctx)
			t.Fatal(err)
		}
		return tx
	}
	execute := func(sql string, args ...any) error {
		tx := begin()
		defer tx.Rollback(ctx)
		_, err := tx.Exec(ctx, sql, args...)
		if err != nil {
			return err
		}
		return tx.Commit(ctx)
	}
	digest := "sha256:" + strings.Repeat("3", 64)
	createSQL := `SELECT * FROM cloud_agents.create_managed_agent_execution_v1('tenant-local',$1,$2,'turn-one','execution-one',1,$3,$4)`
	startSQL := `SELECT * FROM cloud_agents.start_managed_agent_execution_v1('tenant-local',$1,$2,'turn-one','execution-one',1,$3,$4)`
	createKey, startKey := key(), key()
	if err := execute(createSQL, project, "session-running", createKey, digest); err != nil {
		t.Fatal(err)
	}
	if err := execute(startSQL, project, "session-running", startKey, digest); err != nil {
		t.Fatal(err)
	}
	if err := execute(createSQL, project, "session-queued", key(), digest); err != nil {
		t.Fatal(err)
	}
	preview := func() platform.DeploymentTargetSchedulingPreview {
		t.Helper()
		value, err := admin.PreviewAdminDeploymentTargetScheduling(ctx, "tenant-local", project, "drain-target", key())
		if err != nil {
			t.Fatal(err)
		}
		return value.Value
	}
	transition := func(p platform.DeploymentTargetSchedulingPreview, k string) (platform.MaintenanceOperation, error) {
		result, err := admin.TransitionAdminDeploymentTargetScheduling(ctx, "tenant-local", project, "drain-target", key(), k, platform.DeploymentTargetSchedulingRequest{ExpectedGeneration: p.Spec.ExpectedGeneration, ExpectedResourceVersion: p.Spec.ExpectedResourceVersion, DesiredState: p.Spec.DesiredState, ImpactDigest: p.Spec.ImpactDigest})
		return result.Value, err
	}
	p := preview()
	drainKey := key()
	operation, err := transition(p, drainKey)
	if err != nil || operation.State != "succeeded" || operation.Action != "target.drain" {
		t.Fatalf("Drain: %v", err)
	}
	replay, err := transition(p, drainKey)
	if err != nil || replay.OperationID != operation.OperationID {
		t.Fatalf("Drain replay: %v", err)
	}
	_, err = transition(p, key())
	status(err, 409)
	_, err = user.PreviewAdminDeploymentTargetScheduling(ctx, "tenant-local", project, "drain-target", key())
	status(err, 403)
	_, err = user.TransitionAdminDeploymentTargetScheduling(ctx, "tenant-local", project, "drain-target", key(), key(), platform.DeploymentTargetSchedulingRequest{ExpectedGeneration: p.Spec.ExpectedGeneration, ExpectedResourceVersion: p.Spec.ExpectedResourceVersion, DesiredState: p.Spec.DesiredState, ImpactDigest: p.Spec.ImpactDigest})
	status(err, 403)
	status(session("session-denied", key()), 409)
	status(turn("session-idle", "turn-denied", key()), 409)
	if err := session("session-idle", sessionKey); err != nil {
		t.Fatalf("Session replay during Drain: %v", err)
	}
	if err := turn("session-running", "turn-one", turnKey); err != nil {
		t.Fatalf("Turn replay during Drain: %v", err)
	}
	rejected := func(err error) {
		t.Helper()
		var failure *pgconn.PgError
		if !errors.As(err, &failure) || failure.Code != "23505" {
			t.Fatalf("expected admission conflict: %v", err)
		}
	}
	rejected(execute(createSQL, project, "session-create", key(), digest))
	queuedStartKey := key()
	rejected(execute(startSQL, project, "session-queued", queuedStartKey, digest))
	if err := execute(createSQL, project, "session-running", createKey, digest); err != nil {
		t.Fatal(err)
	}
	if err := execute(startSQL, project, "session-running", startKey, digest); err != nil {
		t.Fatal(err)
	}
	// Direct terminal UPDATE tests the shared trigger, not a real Provider completion.
	if _, err := owner.Exec(ctx, `UPDATE cloud_agents.managed_agent_executions SET state='succeeded',result_digest=$2 WHERE tenant_id='tenant-local' AND project_uid=$1 AND session_uid='session-running'`, project, digest); err != nil {
		t.Fatal(err)
	}
	resumed, err := transition(preview(), key())
	if err != nil || resumed.Action != "target.resume" {
		t.Fatal(err)
	}
	if err := session("session-denied", key()); err != nil {
		t.Fatal(err)
	}
	if err := turn("session-idle", "turn-denied", key()); err != nil {
		t.Fatal(err)
	}
	if err := execute(startSQL, project, "session-queued", queuedStartKey, digest); err != nil {
		t.Fatal(err)
	}
	// A blocked admission must observe a Drain committed while it waits for the Target row.
	lock, err := owner.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Rollback(ctx)
	if _, err := lock.Exec(ctx, `UPDATE cloud_agents.deployment_targets SET scheduling_state='drained',resource_version=resource_version+1 WHERE tenant_id='tenant-local' AND project_uid=$1 AND target_uid='drain-target'`, project); err != nil {
		t.Fatal(err)
	}
	admission := begin()
	defer admission.Rollback(ctx)
	pid := admission.Conn().PgConn().PID()
	result := make(chan error, 1)
	go func() {
		_, err := admission.Exec(ctx, `SELECT * FROM cloud_agents.create_managed_agent_session_v3('tenant-local',$1,'session-race','codex','lease-drain',$2,$3)`, project, key(), digest)
		result <- err
	}()
	deadline := time.Now().Add(3 * time.Second)
	blocked := false
	for time.Now().Before(deadline) {
		if err := owner.QueryRow(ctx, "SELECT EXISTS (SELECT 1 FROM pg_catalog.pg_locks WHERE pid=$1 AND NOT granted)", pid).Scan(&blocked); err != nil {
			t.Fatal(err)
		}
		if blocked {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !blocked {
		t.Fatal("admission did not wait for Target barrier")
	}
	if err := lock.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	rejected(<-result)
	admission.Rollback(ctx)
	if _, err := transition(preview(), key()); err != nil {
		t.Fatal(err)
	}
	// Conversely, Drain waits for already-admitted work's transaction to finish.
	admitted := begin()
	defer admitted.Rollback(ctx)
	if _, err := admitted.Exec(ctx, `SELECT * FROM cloud_agents.create_managed_agent_session_v3('tenant-local',$1,'session-before-drain','codex','lease-drain',$2,$3)`, project, key(), digest); err != nil {
		t.Fatal(err)
	}
	next := preview()
	drained := make(chan error, 1)
	go func() { _, err := transition(next, key()); drained <- err }()
	select {
	case err := <-drained:
		t.Fatalf("Drain crossed uncommitted admission: %v", err)
	case <-time.After(150 * time.Millisecond):
	}
	if err := admitted.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	if err := <-drained; err != nil {
		t.Fatal(err)
	}
	if _, err := transition(preview(), key()); err != nil {
		t.Fatal(err)
	}
	encoded, _ := json.Marshal(operation)
	if strings.Contains(string(encoded), "admission-test-content") || strings.Contains(string(encoded), "provider-not-configured") {
		t.Fatal("Admin operation leaked user data")
	}
	var audits int
	if err := owner.QueryRow(ctx, `SELECT count(*) FROM cloud_agents.deployment_target_activity WHERE tenant_id='tenant-local' AND project_uid=$1 AND target_uid='drain-target' AND action IN ('target.drain','target.resume') AND state='succeeded'`, project).Scan(&audits); err != nil || audits != 5 {
		t.Fatalf("Audit closure count=%d err=%v", audits, err)
	}
	t.Log("Drain/Resume, HTTP403/409, Session/Turn and Execution admission/replay, terminal settlement guard, both lock orderings and Audit passed; no Provider E2E claimed")
}
