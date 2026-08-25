//go:build localdev

// cloud-agents-control-plane is an explicitly localdev-only HTTP entry point.
// It binds to loopback, uses an ephemeral in-memory authn trust snapshot, and
// exposes only the versioned claim-only route. It is not a production server.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/hxp0618/cloud-agents/services/control-plane/internal/authn"
	"github.com/hxp0618/cloud-agents/services/control-plane/internal/server"
	"github.com/hxp0618/cloud-agents/services/control-plane/internal/store/postgres"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const databaseURLEnvironment = "CLOUD_AGENTS_PLATFORM_DATABASE_URL"

var (
	errMissingDatabaseURL   = errors.New("database URL is required")
	errNonLoopbackDatabase  = errors.New("database URL must target loopback or a local Unix socket")
	errNonLoopbackListen    = errors.New("listen address must be loopback")
	errInvalidTokenFilePath = errors.New("local token file path is invalid")
	errUnsafeRuntimeRole    = errors.New("database runtime authority is unsafe")
)

const runtimeAuthoritySQL = `SELECT
    login_role.rolcanlogin
    AND login_role.rolinherit
    AND NOT login_role.rolsuper
    AND NOT login_role.rolcreatedb
    AND NOT login_role.rolcreaterole
    AND NOT login_role.rolreplication
    AND NOT login_role.rolbypassrls
    AND current_user = session_user
    AND NOT runtime_role.rolcanlogin
    AND NOT runtime_role.rolinherit
    AND NOT runtime_role.rolsuper
    AND NOT runtime_role.rolcreatedb
    AND NOT runtime_role.rolcreaterole
    AND NOT runtime_role.rolreplication
    AND NOT runtime_role.rolbypassrls
    AND (
        SELECT pg_catalog.count(*) = 1
        FROM pg_catalog.pg_auth_members AS membership
        WHERE membership.member = login_role.oid
    )
    AND EXISTS (
        SELECT 1
        FROM pg_catalog.pg_auth_members AS membership
        JOIN pg_catalog.pg_roles AS grantor_role
          ON grantor_role.oid = membership.grantor
        WHERE membership.member = login_role.oid
          AND membership.roleid = runtime_role.oid
          AND NOT membership.admin_option
          AND coalesce(
              (pg_catalog.to_jsonb(membership)->>'inherit_option')::boolean,
              true
          )
          AND coalesce(
              (pg_catalog.to_jsonb(membership)->>'set_option')::boolean,
              true
          )
          AND grantor_role.rolsuper
          AND grantor_role.rolname NOT IN (
              'cloud_agents_migration_owner',
              'cloud_agents_runtime',
              'cloud_agents_bootstrap_admin'
          )
    )
    AND NOT EXISTS (
        SELECT 1
        FROM pg_catalog.pg_auth_members AS membership
        WHERE membership.member = runtime_role.oid
    )
    AND NOT EXISTS (
        SELECT 1
        FROM pg_catalog.pg_auth_members AS membership
        WHERE membership.roleid = login_role.oid
    )
    AND NOT EXISTS (
        SELECT 1
        FROM pg_catalog.pg_auth_members AS incoming_membership
        JOIN pg_catalog.pg_roles AS incoming_member_role
          ON incoming_member_role.oid = incoming_membership.member
        JOIN pg_catalog.pg_roles AS incoming_grantor_role
          ON incoming_grantor_role.oid = incoming_membership.grantor
        WHERE incoming_membership.roleid = runtime_role.oid
          AND (
              incoming_membership.admin_option
              OR NOT coalesce(
                  (pg_catalog.to_jsonb(incoming_membership)->>'inherit_option')::boolean,
                  true
              )
              OR NOT coalesce(
                  (pg_catalog.to_jsonb(incoming_membership)->>'set_option')::boolean,
                  true
              )
              OR NOT incoming_member_role.rolcanlogin
              OR NOT incoming_member_role.rolinherit
              OR incoming_member_role.rolsuper
              OR incoming_member_role.rolcreatedb
              OR incoming_member_role.rolcreaterole
              OR incoming_member_role.rolreplication
              OR incoming_member_role.rolbypassrls
              OR NOT pg_catalog.pg_has_role(
                  incoming_member_role.oid,
                  runtime_role.oid,
                  'USAGE'
              )
              OR EXISTS (
                  SELECT 1
                  FROM pg_catalog.pg_auth_members AS child_membership
                  WHERE child_membership.roleid = incoming_member_role.oid
              )
              OR (
                  SELECT pg_catalog.count(*)
                  FROM pg_catalog.pg_auth_members AS member_authority_membership
                  WHERE member_authority_membership.member = incoming_member_role.oid
              ) <> 1
              OR NOT incoming_grantor_role.rolsuper
              OR incoming_grantor_role.rolname IN (
                  'cloud_agents_migration_owner',
                  'cloud_agents_runtime',
                  'cloud_agents_bootstrap_admin'
              )
              OR EXISTS (
                  WITH RECURSIVE grantor_memberships (roleid) AS (
                      SELECT grantor_membership.roleid
                      FROM pg_catalog.pg_auth_members AS grantor_membership
                      WHERE grantor_membership.member = incoming_grantor_role.oid

                      UNION

                      SELECT grantor_membership.roleid
                      FROM pg_catalog.pg_auth_members AS grantor_membership
                      JOIN grantor_memberships
                        ON grantor_memberships.roleid = grantor_membership.member
                  )
                  SELECT 1
                  FROM grantor_memberships
                  JOIN pg_catalog.pg_roles AS inherited_role
                    ON inherited_role.oid = grantor_memberships.roleid
                  WHERE inherited_role.rolname IN (
                      'cloud_agents_migration_owner',
                      'cloud_agents_runtime',
                      'cloud_agents_bootstrap_admin'
                  )
              )
          )
    )
    AND pg_catalog.pg_has_role(login_role.oid, runtime_role.oid, 'USAGE')
    AND NOT pg_catalog.has_database_privilege(session_user, current_database(), 'CREATE')
    AND NOT pg_catalog.has_database_privilege(session_user, current_database(), 'TEMPORARY')
    AND NOT EXISTS (
        SELECT 1
        FROM pg_catalog.pg_namespace AS namespace_row
        WHERE namespace_row.nspname <> 'information_schema'
          AND pg_catalog.left(namespace_row.nspname::text, 3) <> 'pg_'
          AND pg_catalog.has_schema_privilege(session_user, namespace_row.oid, 'CREATE')
    )
    AND NOT EXISTS (
        SELECT 1
        FROM pg_catalog.pg_class AS relation
        JOIN pg_catalog.pg_namespace AS namespace_row
          ON namespace_row.oid = relation.relnamespace
        WHERE namespace_row.nspname <> 'information_schema'
          AND pg_catalog.left(namespace_row.nspname::text, 3) <> 'pg_'
          AND (
              relation.relkind IN ('r', 'p', 'v', 'm', 'f')
              AND (
                  pg_catalog.has_table_privilege(session_user, relation.oid, 'INSERT')
                  OR pg_catalog.has_any_column_privilege(session_user, relation.oid, 'INSERT')
                  OR pg_catalog.has_table_privilege(session_user, relation.oid, 'UPDATE')
                  OR pg_catalog.has_any_column_privilege(session_user, relation.oid, 'UPDATE')
                  OR pg_catalog.has_table_privilege(session_user, relation.oid, 'DELETE')
                  OR pg_catalog.has_table_privilege(session_user, relation.oid, 'TRUNCATE')
                  OR pg_catalog.has_table_privilege(session_user, relation.oid, 'REFERENCES')
                  OR pg_catalog.has_any_column_privilege(session_user, relation.oid, 'REFERENCES')
                  OR pg_catalog.has_table_privilege(session_user, relation.oid, 'TRIGGER')
              )
              OR (
                  relation.relkind = 'S'
                  AND (
                      pg_catalog.has_sequence_privilege(session_user, relation.oid, 'USAGE')
                      OR pg_catalog.has_sequence_privilege(session_user, relation.oid, 'UPDATE')
                  )
              )
          )
    )
FROM pg_catalog.pg_roles AS login_role
JOIN pg_catalog.pg_roles AS runtime_role
  ON runtime_role.rolname = 'cloud_agents_runtime'
JOIN pg_catalog.pg_namespace AS target_namespace
  ON target_namespace.nspname = 'cloud_agents'
WHERE login_role.rolname = session_user`

type controlPlaneConfig struct {
	listen         string
	databaseURL    string
	localTokenFile string
	localTenantID  string
	localSubject   string
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := run(ctx, os.Args[1:]); err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "cloud-agents-control-plane: %v\n", err)
		os.Exit(2)
	}
}

func run(ctx context.Context, args []string) error {
	config, err := parseControlPlaneConfig(args, os.Getenv)
	if err != nil {
		return err
	}
	if err := validateLoopbackListenAddress(config.listen); err != nil {
		return err
	}
	if config.databaseURL == "" {
		return errMissingDatabaseURL
	}
	if config.localTokenFile == "" {
		return errInvalidTokenFilePath
	}

	poolConfig, err := parseLoopbackDatabaseConfig(config.databaseURL)
	if err != nil {
		return err
	}
	pool, err := pgxpool.NewWithConfig(ctx, poolConfig)
	if err != nil {
		return errors.New("database pool configuration failed")
	}
	defer pool.Close()
	if err := pool.Ping(ctx); err != nil {
		return errors.New("database is unavailable")
	}
	coordinationService, err := postgres.NewDurableCoordinationService(pool)
	if err != nil {
		return errors.New("durable coordination service is unavailable")
	}
	claimServer, err := server.NewManagedAgentCreateProjectServer(coordinationService)
	if err != nil {
		return errors.New("claim server is unavailable")
	}
	verifier, err := authn.NewLocalVerifier(authn.LocalVerifierConfig{})
	if err != nil {
		return errors.New("local verifier is unavailable")
	}
	defer verifier.Invalidate()
	token, issueErr := verifier.IssueToken(authn.LocalTokenClaims{TenantID: config.localTenantID, Subject: config.localSubject})
	if issueErr != nil {
		return errors.New("local token issuance failed")
	}
	if err := writeLocalTokenFile(config.localTokenFile, token); err != nil {
		return err
	}
	defer func() { _ = os.Remove(config.localTokenFile) }()
	handler, err := server.NewLocalProjectClaimHTTPServer(verifier, claimServer)
	if err != nil {
		return errors.New("local HTTP server is unavailable")
	}
	httpServer := &http.Server{
		Addr:              config.listen,
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      15 * time.Second,
		IdleTimeout:       30 * time.Second,
	}
	errorChannel := make(chan error, 1)
	go func() {
		errorChannel <- httpServer.ListenAndServe()
	}()
	select {
	case <-ctx.Done():
		shutdownContext, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := httpServer.Shutdown(shutdownContext); err != nil {
			return errors.New("HTTP shutdown failed")
		}
		return nil
	case err := <-errorChannel:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return errors.New("HTTP server stopped")
	}
}

func parseControlPlaneConfig(args []string, getenv func(string) string) (controlPlaneConfig, error) {
	set := flag.NewFlagSet("cloud-agents-control-plane", flag.ContinueOnError)
	set.SetOutput(io.Discard)
	listen := set.String("listen", "127.0.0.1:8080", "loopback listen address")
	databaseURL := set.String("database-url", "", "task-local PostgreSQL URL")
	localTokenFile := set.String("local-token-file", "", "write one ephemeral local bearer token to this 0600 file")
	localTenantID := set.String("local-tenant-id", "tenant-coordination-normal", "tenant for the optional local token")
	localSubject := set.String("local-subject", "user-admin", "subject for the optional local token")
	if err := set.Parse(args); err != nil || set.NArg() != 0 {
		return controlPlaneConfig{}, errors.New("invalid control-plane configuration")
	}
	resolvedDatabaseURL := *databaseURL
	if resolvedDatabaseURL == "" && getenv != nil {
		resolvedDatabaseURL = getenv(databaseURLEnvironment)
	}
	if *localTokenFile != "" && (strings.TrimSpace(*localTokenFile) != *localTokenFile || strings.HasSuffix(*localTokenFile, string(os.PathSeparator))) {
		return controlPlaneConfig{}, errInvalidTokenFilePath
	}
	return controlPlaneConfig{
		listen:         *listen,
		databaseURL:    resolvedDatabaseURL,
		localTokenFile: *localTokenFile,
		localTenantID:  *localTenantID,
		localSubject:   *localSubject,
	}, nil
}

func validateLoopbackListenAddress(address string) error {
	host, _, err := net.SplitHostPort(address)
	if err != nil || host == "" {
		return errNonLoopbackListen
	}
	ip := net.ParseIP(host)
	if ip == nil || !ip.IsLoopback() {
		return errNonLoopbackListen
	}
	return nil
}

func parseLoopbackDatabaseConfig(databaseURL string) (*pgxpool.Config, error) {
	config, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		return nil, errNonLoopbackDatabase
	}
	if !isLocalDatabaseHost(config.ConnConfig.Host) {
		return nil, errNonLoopbackDatabase
	}
	for _, fallback := range config.ConnConfig.Fallbacks {
		if fallback == nil || !isLocalDatabaseHost(fallback.Host) {
			return nil, errNonLoopbackDatabase
		}
	}
	config.AfterConnect = validateRuntimeConnection
	return config, nil
}

func isLocalDatabaseHost(host string) bool {
	if strings.HasPrefix(host, string(os.PathSeparator)) {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func validateRuntimeConnection(ctx context.Context, connection *pgx.Conn) error {
	if connection == nil {
		return errUnsafeRuntimeRole
	}
	var safe bool
	if err := connection.QueryRow(ctx, runtimeAuthoritySQL).Scan(&safe); err != nil || !safe {
		return errUnsafeRuntimeRole
	}
	return nil
}

func writeLocalTokenFile(path, token string) error {
	if path == "" || token == "" {
		return errInvalidTokenFilePath
	}
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return errors.New("cannot create local token file")
	}
	if _, writeErr := file.WriteString(token + "\n"); writeErr != nil {
		_ = file.Close()
		_ = os.Remove(path)
		return errors.New("cannot write local token file")
	}
	if err := file.Close(); err != nil {
		_ = os.Remove(path)
		return errors.New("cannot close local token file")
	}
	return nil
}
