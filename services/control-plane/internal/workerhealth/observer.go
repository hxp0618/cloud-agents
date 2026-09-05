// Package workerhealth performs bounded Control Plane health observation, never user-content reads.
package workerhealth

import (
	"context"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"errors"
	"log/slog"
	"net/url"
	"sync"
	"time"

	worker "github.com/hxp0618/cloud-agents/sdk/go/gen/cloudagents/worker/v1alpha1"
	"github.com/hxp0618/cloud-agents/services/control-plane/internal/workerclient"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type candidate struct {
	tenant, project, lease, endpoint, spiffe, serverName string
	generation, resourceVersion                          int64
}

// Run survives individual transport/database failures. Database time determines freshness even
// when this process is stopped. Claims expire after ten seconds; no browser session is required.
func Run(ctx context.Context, pool *pgxpool.Pool, certificate tls.Certificate, roots *x509.CertPool, logger *slog.Logger) {
	for ctx.Err() == nil {
		count, err := ObserveBatch(ctx, pool, certificate, roots)
		if err != nil && ctx.Err() == nil {
			logger.WarnContext(ctx, "Worker health observation batch failed")
		}
		delay := 5 * time.Second
		if err == nil && count == 8 {
			delay = time.Millisecond
		}
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
	}
}

// ObserveBatch uses the internal DB claim authority. It must not be reachable from an HTTP handler.
// ponytail: eight parallel checks per batch; additional CP replicas share work via SKIP LOCKED.
func ObserveBatch(ctx context.Context, pool *pgxpool.Pool, certificate tls.Certificate, roots *x509.CertPool) (int, error) {
	if ctx == nil || pool == nil || roots == nil || len(certificate.Certificate) == 0 || certificate.PrivateKey == nil {
		return 0, errors.New("worker health observer configuration unavailable")
	}
	token := make([]byte, 32)
	if _, err := rand.Read(token); err != nil {
		return 0, errors.New("worker health claim unavailable")
	}
	claim := hex.EncodeToString(token)
	items, err := claimBatch(ctx, pool, claim)
	if err != nil {
		return 0, err
	}
	var pending sync.WaitGroup
	failures := make(chan error, len(items))
	for _, item := range items {
		pending.Add(1)
		go func() {
			defer pending.Done()
			checkContext, cancel := context.WithTimeout(ctx, 5*time.Second)
			defer cancel()
			succeeded := false
			identity, parseErr := url.Parse(item.spiffe)
			if parseErr == nil && identity != nil && item.serverName != "" {
				supervisor, configErr := workerclient.NewMTLS(workerclient.MTLSConfig{Endpoint: item.endpoint, ServerName: item.serverName, ExpectedWorkerIdentity: &worker.WorkloadIdentity{SpiffeId: item.spiffe, TrustDomain: identity.Host}, ClientCertificate: certificate, RootCAs: roots})
				if configErr == nil {
					succeeded = supervisor.CheckRuntimeHealth(checkContext) == nil
					supervisor.CloseIdleConnections()
				}
			}
			// A canceled observer must not record a shutdown as a Worker failure.
			if ctx.Err() != nil {
				return
			}
			if err := completeCheck(ctx, pool, item, claim, succeeded); err != nil {
				failures <- err
			}
		}()
	}
	pending.Wait()
	close(failures)
	for failure := range failures {
		return len(items), failure
	}
	return len(items), nil
}

func begin(ctx context.Context, pool *pgxpool.Pool) (pgx.Tx, error) {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return nil, errors.New("worker health database unavailable")
	}
	if _, err := tx.Exec(ctx, "SET LOCAL ROLE cloud_agents_runtime"); err != nil {
		_ = tx.Rollback(ctx)
		return nil, errors.New("worker health database authority unavailable")
	}
	return tx, nil
}

func claimBatch(ctx context.Context, pool *pgxpool.Pool, claim string) ([]candidate, error) {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	tx, err := begin(ctx, pool)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	rows, err := tx.Query(ctx, "SELECT tenant_id, project_uid, lease_uid, generation, resource_version, worker_endpoint, worker_spiffe_id, worker_server_name FROM cloud_agents.claim_worker_health_checks_v1($1)", claim)
	if err != nil {
		return nil, errors.New("worker health claim failed")
	}
	defer rows.Close()
	items := []candidate{}
	for rows.Next() {
		var item candidate
		if err := rows.Scan(&item.tenant, &item.project, &item.lease, &item.generation, &item.resourceVersion, &item.endpoint, &item.spiffe, &item.serverName); err != nil || len(items) >= 8 {
			return nil, errors.New("worker health claim invalid")
		}
		items = append(items, item)
	}
	if rows.Err() != nil {
		return nil, errors.New("worker health claim failed")
	}
	rows.Close()
	if err := tx.Commit(ctx); err != nil {
		return nil, errors.New("worker health claim commit unknown")
	}
	return items, nil
}

func completeCheck(ctx context.Context, pool *pgxpool.Pool, item candidate, claim string, succeeded bool) error {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	tx, err := begin(ctx, pool)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var applied bool
	if err := tx.QueryRow(ctx, "SELECT cloud_agents.complete_worker_health_check_v1($1,$2,$3,$4,$5,$6,$7)", item.tenant, item.project, item.lease, item.generation, item.resourceVersion, claim, succeeded).Scan(&applied); err != nil {
		return errors.New("worker health result failed")
	}
	// False is an expected stale claim/generation, not permission to overwrite newer authority.
	if err := tx.Commit(ctx); err != nil {
		return errors.New("worker health result commit unknown")
	}
	return nil
}
