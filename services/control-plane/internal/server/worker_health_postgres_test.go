package server

import (
	"context"
	"crypto/x509"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	platform "github.com/hxp0618/cloud-agents/sdk/go/gen/platform/v1alpha1"
	"github.com/hxp0618/cloud-agents/services/control-plane/internal/workerhealth"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Opt-in against a disposable, fully migrated local-dev database with an API-created project
// and target. The ready Lease is a SQL fixture, NOT evidence of Target deployment or Provider E2E.
// This test starts real Worker executables, uses real mTLS and the running Admin API/RBAC/store.
func TestWorkerHealthPostgresAndProcess(t *testing.T) {
	runtimeURL := os.Getenv("CLOUD_AGENTS_HEALTH_TEST_RUNTIME_DATABASE_URL")
	if runtimeURL == "" {
		t.Skip("isolated Worker health PostgreSQL environment not configured")
	}
	project := os.Getenv("CLOUD_AGENTS_HEALTH_TEST_PROJECT_ID")
	stateDirectory := os.Getenv("CLOUD_AGENTS_HEALTH_TEST_DEV_STATE")
	migrationURL := os.Getenv("CLOUD_AGENTS_HEALTH_TEST_MIGRATION_DATABASE_URL")
	if project == "" || stateDirectory == "" || migrationURL == "" {
		t.Fatal("complete isolated health test configuration is required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	runtimePool, err := pgxpool.New(ctx, runtimeURL)
	if err != nil {
		t.Fatal("runtime pool configuration failed")
	}
	defer runtimePool.Close()
	config, err := pgxpool.ParseConfig(migrationURL)
	if err != nil {
		t.Fatal("migration pool configuration failed")
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
	var count int
	if err := owner.QueryRow(ctx, "SELECT count(*) FROM cloud_agents.managed_host_environment_leases").Scan(&count); err != nil || count != 0 {
		t.Fatal("requires a disposable database with no existing leases")
	}
	ca, caKey := healthTestCertificate(t, nil, nil, "", true)
	roots := x509.NewCertPool()
	roots.AddCert(ca.Leaf)
	serverCert, _ := healthTestCertificate(t, ca.Leaf, caKey, "spiffe://health.test/worker/lease-alpha", false)
	clientCert, _ := healthTestCertificate(t, ca.Leaf, caKey, "spiffe://health.test/supervisor", false)
	endpoint, stopWorker := startHealthTestWorker(t, serverCert, ca.Leaf)
	_, err = owner.Exec(ctx, `INSERT INTO cloud_agents.managed_host_environment_leases (
 tenant_id,tenant_ref_id,project_uid,lease_uid,lease_name,release_digest,generation,desired_phase,observed_phase,cleanup_phase,environment_id,expires_at,resource_version,create_idempotency_key,create_request_digest,created_at,updated_at,
 deployment_target_uid,deployment_target_generation,provider_credential_ref,cpu_limit_millis,memory_limit_bytes,worker_endpoint,worker_spiffe_id,worker_server_name)
 VALUES ('tenant-local','tenant-local',$1,'lease-alpha','health-observer-worker','sha256:'||repeat('1',64),2,'active','ready','none','lease-alpha',clock_timestamp()+interval '1 hour',3,'health-observer-fixture','sha256:'||repeat('2',64),clock_timestamp(),clock_timestamp(),
 'health-docker',1,'health-provider-not-configured',1000,536870912,$2,'spiffe://health.test/worker/lease-alpha','health.test')`, project, endpoint)
	if err != nil {
		t.Fatalf("insert isolated Lease fixture: %v", err)
	}
	// Exact fixture only; parent harness owns and removes the dedicated database container.
	fixtureIDs := []string{"lease-alpha"}
	defer func() {
		if _, err := owner.Exec(context.Background(), "DELETE FROM cloud_agents.managed_host_environment_leases WHERE tenant_id='tenant-local' AND project_uid=$1 AND lease_uid=ANY($2)", project, fixtureIDs); err != nil {
			t.Error("fixture cleanup failed")
		}
	}()
	readToken := func(name string) string {
		t.Helper()
		data, err := os.ReadFile(stateDirectory + "/" + name)
		if err != nil {
			t.Fatal("local token unavailable")
		}
		return strings.TrimSpace(string(data))
	}
	adminToken, userToken := readToken("control-plane-admin.token"), readToken("control-plane.token")
	client := &http.Client{Timeout: 3 * time.Second}
	get := func(token string) (int, []byte) {
		t.Helper()
		req, _ := http.NewRequestWithContext(ctx, "GET", "http://127.0.0.1:18085/v1/admin/tenants/tenant-local/projects/"+project+"/workers", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("X-Request-ID", "request-health-observer")
		response, err := client.Do(req)
		if err != nil {
			t.Fatal("Admin API unavailable")
		}
		defer response.Body.Close()
		data, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
		if err != nil {
			t.Fatal("Admin response unavailable")
		}
		for _, secret := range []string{endpoint, "health-provider-not-configured", "credentialRef", "providerCredentialRef", "workerEndpoint", "PRIVATE KEY"} {
			if strings.Contains(string(data), secret) {
				t.Fatal("Admin response disclosed private routing/credentials")
			}
		}
		return response.StatusCode, data
	}
	if status, _ := get(userToken); status != 403 {
		t.Fatalf("ordinary user status=%d", status)
	}
	health := func() *platform.WorkerHealthStatus {
		t.Helper()
		status, body := get(adminToken)
		page, err := platform.DecodeWorkerPageJSON(body)
		if status != 200 || err != nil || len(page.Workers) != 1 {
			t.Fatalf("Worker projection status=%d decode=%v", status, err)
		}
		return page.Workers[0].Spec.Health
	}
	waitHealth := func(state string, after string, timeout time.Duration) *platform.WorkerHealthStatus {
		t.Helper()
		deadline := time.Now().Add(timeout)
		for time.Now().Before(deadline) {
			value := health()
			if value != nil && value.State == state && value.CheckedAt != after {
				return value
			}
			time.Sleep(200 * time.Millisecond)
		}
		t.Fatalf("health did not become %s", state)
		return nil
	}
	if health() != nil {
		t.Fatal("unobserved Worker presented as online")
	}
	observerContext, stopObserver := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() {
		defer close(done)
		workerhealth.Run(observerContext, runtimePool, clientCert, roots, slog.New(slog.NewTextHandler(io.Discard, nil)))
	}()
	defer func() { stopObserver(); <-done }()
	first := waitHealth("online", "", 8*time.Second)
	t.Log("online: real mTLS result persisted and read through Admin API; user token 403")
	second := waitHealth("online", first.CheckedAt, 30*time.Second)
	if second.LastSuccessAt != second.CheckedAt {
		t.Fatal("successful periodic observation not persisted")
	}
	stopWorker()
	unavailable := waitHealth("unavailable", "", 30*time.Second)
	if unavailable.LastSuccessAt != second.CheckedAt {
		t.Fatal("transport failure discarded prior success")
	}
	stopObserver()
	<-done
	t.Log("unavailable: stopped Worker observed, periodic loop stopped; waiting actual 60-second expiry")
	waitHealth("expired", "", 65*time.Second)
	t.Log("expired: database clock, no timestamp rewriting and no browser inference")
	endpoint, stopReplacement := startHealthTestWorker(t, serverCert, ca.Leaf)
	defer stopReplacement()
	update := func(sql string, args ...any) {
		t.Helper()
		params := append([]any{project}, args...)
		if _, err := owner.Exec(ctx, "UPDATE cloud_agents.managed_host_environment_leases SET "+sql+" WHERE tenant_id='tenant-local' AND project_uid=$1 AND lease_uid='lease-alpha'", params...); err != nil {
			t.Fatalf("fixture transition: %v", err)
		}
	}
	update("worker_endpoint=$2,resource_version=4", endpoint)
	if health() != nil {
		t.Fatal("old resource-version health survived route replacement")
	}
	if count, err := workerhealth.ObserveBatch(ctx, runtimePool, clientCert, roots); err != nil || count != 1 {
		t.Fatalf("replacement observation count=%d err=%v", count, err)
	}
	if value := health(); value == nil || value.State != "online" {
		t.Fatal("replacement did not recover")
	}
	stopReplacement()
	update("generation=3,resource_version=5")
	claim := strings.Repeat("a", 64)
	if err := runtimePool.QueryRow(ctx, "SELECT count(*) FROM cloud_agents.claim_worker_health_checks_v1($1)", claim).Scan(&count); err != nil || count != 1 {
		t.Fatalf("claim: %d %v", count, err)
	}
	if err := runtimePool.QueryRow(ctx, "SELECT count(*) FROM cloud_agents.claim_worker_health_checks_v1($1)", strings.Repeat("b", 64)).Scan(&count); err != nil || count != 0 {
		t.Fatal("active claim was duplicated")
	}
	complete := func(tenant, projectID string, generation, version int64, token string) bool {
		t.Helper()
		var applied bool
		if err := runtimePool.QueryRow(ctx, "SELECT cloud_agents.complete_worker_health_check_v1($1,$2,'lease-alpha',$3,$4,$5,true)", tenant, projectID, generation, version, token).Scan(&applied); err != nil {
			t.Fatalf("complete check: %v", err)
		}
		return applied
	}
	if complete("tenant-other", project, 3, 5, claim) || complete("tenant-local", "project-other", 3, 5, claim) || complete("tenant-local", project, 2, 5, claim) || complete("tenant-local", project, 3, 4, claim) || complete("tenant-local", project, 3, 5, strings.Repeat("b", 64)) {
		t.Fatal("stale or cross-scope result accepted")
	}
	update("generation=4,resource_version=6")
	if complete("tenant-local", project, 3, 5, claim) {
		t.Fatal("result accepted after generation change")
	}
	update("worker_health_claim_expires_at=clock_timestamp()-interval '1 second'")
	if complete("tenant-local", project, 4, 6, claim) {
		t.Fatal("expired claim accepted")
	}
	if err := runtimePool.QueryRow(ctx, "SELECT count(*) FROM cloud_agents.claim_worker_health_checks_v1($1)", strings.Repeat("b", 64)).Scan(&count); err != nil || count != 1 {
		t.Fatal("expired claim was not reclaimed")
	}
	if complete("tenant-local", project, 4, 6, claim) || !complete("tenant-local", project, 4, 6, strings.Repeat("b", 64)) || complete("tenant-local", project, 4, 6, strings.Repeat("b", 64)) {
		t.Fatal("claim fencing/replay failed")
	}
	// Database claim concurrency only: these nine rows do not represent nine deployed Workers.
	_, err = owner.Exec(ctx, `INSERT INTO cloud_agents.managed_host_environment_leases (
 tenant_id,tenant_ref_id,project_uid,lease_uid,lease_name,release_digest,generation,desired_phase,observed_phase,cleanup_phase,environment_id,expires_at,resource_version,create_idempotency_key,create_request_digest,created_at,updated_at,
 deployment_target_uid,deployment_target_generation,provider_credential_ref,cpu_limit_millis,memory_limit_bytes,worker_endpoint,worker_spiffe_id,worker_server_name)
 SELECT tenant_id,tenant_ref_id,project_uid,'health-batch-'||i,'health-batch-'||i,release_digest,generation,desired_phase,observed_phase,cleanup_phase,'health-batch-'||i,expires_at,resource_version,'health-observer-batch-'||i,create_request_digest,created_at,updated_at,
 deployment_target_uid,deployment_target_generation,provider_credential_ref,cpu_limit_millis,memory_limit_bytes,worker_endpoint,worker_spiffe_id,worker_server_name
 FROM cloud_agents.managed_host_environment_leases CROSS JOIN generate_series(1,9) i
 WHERE tenant_id='tenant-local' AND project_uid=$1 AND lease_uid='lease-alpha'`, project)
	if err != nil {
		t.Fatalf("claim fixtures: %v", err)
	}
	for i := 1; i <= 9; i++ {
		fixtureIDs = append(fixtureIDs, fmt.Sprintf("health-batch-%d", i))
	}
	type batchResult struct {
		ids []string
		err error
	}
	batches := make(chan batchResult, 2)
	for _, token := range []string{strings.Repeat("c", 64), strings.Repeat("d", 64)} {
		go func() {
			rows, err := runtimePool.Query(ctx, "SELECT lease_uid FROM cloud_agents.claim_worker_health_checks_v1($1)", token)
			if err != nil {
				batches <- batchResult{err: err}
				return
			}
			defer rows.Close()
			result := batchResult{}
			for rows.Next() {
				var id string
				if err := rows.Scan(&id); err != nil {
					result.err = err
					break
				}
				result.ids = append(result.ids, id)
			}
			if rows.Err() != nil {
				result.err = rows.Err()
			}
			batches <- result
		}()
	}
	seen := map[string]bool{}
	for range 2 {
		result := <-batches
		if result.err != nil || len(result.ids) < 1 || len(result.ids) > 8 {
			t.Fatalf("claim batch=%d err=%v", len(result.ids), result.err)
		}
		for _, id := range result.ids {
			if seen[id] {
				t.Fatal("concurrent replicas claimed the same Worker")
			}
			seen[id] = true
		}
	}
	if len(seen) != 9 {
		t.Fatalf("concurrent claims=%d", len(seen))
	}
	canceled, cancelBatch := context.WithCancel(ctx)
	cancelBatch()
	if count, err := workerhealth.ObserveBatch(canceled, runtimePool, clientCert, roots); err == nil || count != 0 {
		t.Fatal("canceled observer started work")
	}
	for _, invalid := range []any{nil, "short", strings.Repeat("g", 64)} {
		if _, err := runtimePool.Exec(ctx, "SELECT * FROM cloud_agents.claim_worker_health_checks_v1($1)", invalid); err == nil {
			t.Fatal("invalid claim token accepted")
		}
	}
	tx, err := runtimePool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, "SELECT set_config('cloud_agents.tenant_id','tenant-other',true)"); err != nil {
		t.Fatal(err)
	}
	if err := tx.QueryRow(ctx, "SELECT count(*) FROM cloud_agents.managed_host_environment_leases").Scan(&count); err != nil || count != 0 {
		t.Fatalf("cross tenant read count=%d err=%v", count, err)
	}
	if _, err := tx.Exec(ctx, "UPDATE cloud_agents.managed_host_environment_leases SET worker_health_succeeded=true"); err == nil {
		t.Fatal("runtime role can overwrite health directly")
	}
	t.Log("recovery, generation/resource-version/claim fencing, replay, bounded concurrent claims, canceled observer, tenant RLS and direct-write denial passed")
}
