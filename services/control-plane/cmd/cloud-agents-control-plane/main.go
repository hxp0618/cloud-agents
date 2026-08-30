//go:build localdev

// cloud-agents-control-plane is an explicitly localdev-only HTTP entry point.
// It binds to loopback, uses an ephemeral in-memory authn trust snapshot, and
// exposes loopback-only project routes and can connect to the independent
// localdev Worker process. It is not a production server.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
	"unicode"
	"unicode/utf8"

	workerruntimev1alpha1connect "github.com/hxp0618/cloud-agents/sdk/go/gen/cloudagents/worker/runtime/v1alpha1/workerruntimev1alpha1connect"
	workerv1alpha1 "github.com/hxp0618/cloud-agents/sdk/go/gen/cloudagents/worker/v1alpha1"
	workerv1alpha1connect "github.com/hxp0618/cloud-agents/sdk/go/gen/cloudagents/worker/v1alpha1/workerv1alpha1connect"
	"github.com/hxp0618/cloud-agents/services/control-plane/internal/authn"
	internalmanagedagent "github.com/hxp0618/cloud-agents/services/control-plane/internal/managedagent"
	"github.com/hxp0618/cloud-agents/services/control-plane/internal/server"
	"github.com/hxp0618/cloud-agents/services/control-plane/internal/store/postgres"
	"github.com/hxp0618/cloud-agents/services/control-plane/internal/workerclient"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	databaseURLEnvironment                = "CLOUD_AGENTS_PLATFORM_DATABASE_URL"
	localRuntimeWorkerEndpointEnvironment = "CLOUD_AGENTS_PLATFORM_WORKER_ENDPOINT"
	localRuntimeWorkerTokenEnvironment    = "CLOUD_AGENTS_PLATFORM_WORKER_TOKEN_FILE"
	localRuntimeWorkspaceEnvironment      = "CLOUD_AGENTS_PLATFORM_WORKSPACE_DIRECTORY"
	localTokenRefreshInterval             = 4 * time.Minute
)

var (
	errMissingDatabaseURL   = errors.New("database URL is required")
	errNonLoopbackDatabase  = errors.New("database URL must target loopback or a local Unix socket")
	errNonLoopbackListen    = errors.New("listen address must be loopback")
	errInvalidTokenFilePath = errors.New("local token file path is invalid")
	errUnsafeRuntimeRole    = errors.New("database runtime authority is unsafe")
	errInvalidRuntimeConfig = errors.New("local Runtime configuration is invalid")
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
	listen             string
	databaseURL        string
	localTokenFile     string
	localTenantID      string
	localSubject       string
	workerEndpoint     string
	workerTokenFile    string
	workspaceDirectory string
}

type localRuntimeWorkerHealth struct {
	APIVersion      string `json:"api_version"`
	Authority       string `json:"authority"`
	Profile         string `json:"profile"`
	Revision        string `json:"revision"`
	ProfileDigest   string `json:"profile_digest"`
	Version         string `json:"version"`
	Status          string `json:"status"`
	WorkerIdentity  string `json:"worker_identity"`
	Supervisor      string `json:"supervisor_identity"`
	LeaseID         string `json:"lease_id"`
	Generation      uint64 `json:"generation"`
	ExternalEffects bool   `json:"external_side_effects"`
	Transport       string `json:"transport"`
}

type localAccessTokenVerifier struct{ verifier *authn.LocalVerifier }

func (v localAccessTokenVerifier) Verify(token string, request authn.VerificationRequest) (*authn.VerifiedPrincipal, error) {
	if v.verifier == nil {
		return nil, errors.New("local verifier is unavailable")
	}
	return v.verifier.Verify(token, authn.LocalVerificationRequest{
		TenantID: request.TenantID, ResourceLevel: request.ResourceLevel, ResourceID: request.ResourceID, RequiredPermission: request.RequiredPermission,
	})
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
	rbacMutationService, err := postgres.NewRBACMutationService(pool)
	if err != nil {
		return errors.New("local RBAC mutation store is unavailable")
	}
	claimServer, err := server.NewManagedAgentCreateProjectServer(coordinationService)
	if err != nil {
		return errors.New("claim server is unavailable")
	}
	durableProjectCreateServer, err := server.NewDurableProjectCreateServer(coordinationService)
	if err != nil {
		return errors.New("durable project create server is unavailable")
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
	refreshContext, stopTokenRefresh := context.WithCancel(ctx)
	defer stopTokenRefresh()
	tokenRefreshErrors := refreshLocalTokenFile(refreshContext, verifier, authn.LocalTokenClaims{TenantID: config.localTenantID, Subject: config.localSubject}, config.localTokenFile, localTokenRefreshInterval)
	claimHTTPServer, err := server.NewLocalProjectClaimHTTPServer(verifier, claimServer)
	if err != nil {
		return errors.New("local HTTP server is unavailable")
	}
	durableProjectHTTPServer, err := server.NewLocalDurableProjectCreateHTTPServer(verifier, durableProjectCreateServer)
	if err != nil {
		return errors.New("local durable project HTTP server is unavailable")
	}
	projectGetHTTPServer, err := server.NewLocalProjectGetHTTPServer(verifier, coordinationService)
	if err != nil {
		return errors.New("local project get HTTP server is unavailable")
	}
	var runtimeSupervisor *workerclient.Supervisor
	var runtimeFencingToken []byte
	var runtimeLeaseID string
	var runtimeGeneration uint64
	workspaceDirectory := config.workspaceDirectory
	if config.workerEndpoint != "" {
		if workspaceDirectory == "" {
			workspaceDirectory, err = os.Getwd()
			if err != nil {
				return errInvalidRuntimeConfig
			}
		}
		workspaceDirectory, err = filepath.Abs(workspaceDirectory)
		if err != nil {
			return errInvalidRuntimeConfig
		}
		workspaceInfo, statErr := os.Stat(workspaceDirectory)
		if statErr != nil || !workspaceInfo.IsDir() {
			return errInvalidRuntimeConfig
		}
		var health localRuntimeWorkerHealth
		runtimeSupervisor, health, runtimeFencingToken, err = newLocalRuntimeSupervisor(ctx, config.workerEndpoint, config.workerTokenFile)
		if err != nil {
			return err
		}
		runtimeLeaseID = health.LeaseID
		runtimeGeneration = health.Generation
	}
	verifierAdapter := localAccessTokenVerifier{verifier: verifier}
	tenantHTTPServer, tenantErr := server.NewPlatformTenantHTTPServer(verifierAdapter, coordinationService)
	if tenantErr != nil {
		return errors.New("local tenant HTTP server is unavailable")
	}
	organizationHTTPServer, organizationErr := server.NewOrganizationHTTPServer(verifierAdapter, coordinationService)
	if organizationErr != nil {
		return errors.New("local organization HTTP server is unavailable")
	}
	roleHTTPServer, roleErr := server.NewRoleHTTPServer(verifierAdapter, coordinationService)
	if roleErr != nil {
		return errors.New("local role HTTP server is unavailable")
	}
	rbacHTTPServer, rbacErr := server.NewRBACHTTPServer(verifierAdapter, coordinationService, rbacMutationService)
	if rbacErr != nil {
		return errors.New("local RBAC HTTP server is unavailable")
	}
	leaseHTTPServer, leaseErr := server.NewManagedHostEnvironmentLeaseHTTPServer(verifierAdapter, coordinationService)
	if leaseErr != nil {
		return errors.New("local managed host environment lease HTTP server is unavailable")
	}
	mux := http.NewServeMux()
	mux.Handle(server.OrganizationCollectionRoute, organizationHTTPServer)
	mux.Handle(server.OrganizationRoute, organizationHTTPServer)
	mux.Handle(server.RoleCollectionRoute, roleHTTPServer)
	mux.Handle(server.RoleRoute, roleHTTPServer)
	mux.Handle(server.MembershipRoute, rbacHTTPServer)
	mux.Handle(server.MembershipCollectionRoute, rbacHTTPServer)
	mux.Handle(server.RoleBindingRoute, rbacHTTPServer)
	mux.Handle(server.RoleBindingCollectionRoute, rbacHTTPServer)
	mux.Handle(server.ManagedHostRoleBindingRoute, rbacHTTPServer)
	mux.Handle(server.PlatformTenantRoute, tenantHTTPServer)
	mux.Handle(server.ManagedHostEnvironmentLeaseRoutePrefix, leaseHTTPServer)
	mux.Handle(server.LocalProjectClaimRoutePrefix, claimHTTPServer)
	mux.Handle("/v1alpha1/tenants/{tenantId}/project-creations", durableProjectHTTPServer)
	if runtimeSupervisor == nil {
		mux.Handle(server.LocalProjectGetRoutePrefix, projectGetHTTPServer)
	} else {
		projectHTTPServer, projectErr := server.NewProjectHTTPServer(verifierAdapter, coordinationService, durableProjectCreateServer)
		if projectErr != nil {
			return errors.New("local project HTTP server is unavailable")
		}
		sessionHTTPServer, sessionErr := server.NewManagedAgentSessionHTTPServer(verifierAdapter, coordinationService)
		if sessionErr != nil {
			return errors.New("local managed agent session HTTP server is unavailable")
		}
		eventsHTTPServer, eventsErr := server.NewManagedAgentEventsHTTPServer(verifierAdapter, coordinationService)
		if eventsErr != nil {
			return errors.New("local managed agent events HTTP server is unavailable")
		}
		turnHTTPServer, turnErr := server.NewManagedAgentTurnHTTPServer(verifierAdapter, coordinationService)
		if turnErr != nil {
			return errors.New("local managed agent turn HTTP server is unavailable")
		}
		runtimeCoordinator, coordinatorErr := internalmanagedagent.NewDurableRuntimeExecutionCoordinator(internalmanagedagent.DurableRuntimeExecutionConfig{
			Store: coordinationService, Supervisor: runtimeSupervisor, Clock: time.Now,
			FencingLeaseID: runtimeLeaseID, FencingGeneration: runtimeGeneration,
			FencingToken: runtimeFencingToken, WorkspaceDirectory: workspaceDirectory, MaxDuration: 5 * time.Minute,
		})
		if coordinatorErr != nil {
			return errors.New("local managed agent Runtime coordinator is unavailable")
		}
		executionHTTPServer, executionErr := server.NewManagedAgentExecutionHTTPServer(verifierAdapter, coordinationService, runtimeCoordinator)
		if executionErr != nil {
			return errors.New("local managed agent execution HTTP server is unavailable")
		}
		mux.Handle(server.LocalProjectGetRoutePrefix, http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
			if server.HandlesManagedAgentExecutionPath(request.URL.Path) {
				executionHTTPServer.ServeHTTP(writer, request)
				return
			}
			if server.HandlesManagedAgentEventsPath(request.URL.Path) {
				eventsHTTPServer.ServeHTTP(writer, request)
				return
			}
			if server.HandlesManagedAgentTurnPath(request.URL.Path) {
				turnHTTPServer.ServeHTTP(writer, request)
				return
			}
			if server.HandlesManagedAgentSessionPath(request.URL.Path) {
				sessionHTTPServer.ServeHTTP(writer, request)
				return
			}
			projectHTTPServer.ServeHTTP(writer, request)
		}))
	}
	healthCheck := pool.Ping
	if runtimeSupervisor != nil {
		healthCheck = func(ctx context.Context) error {
			if err := pool.Ping(ctx); err != nil {
				return err
			}
			return runtimeSupervisor.CheckRuntimeHealth(ctx)
		}
	}
	healthHTTPServer, err := server.NewLocalControlPlaneHealthHTTPServer(healthCheck)
	if err != nil {
		return errors.New("local health HTTP server is unavailable")
	}
	mux.Handle(server.LocalControlPlaneHealthRoute, healthHTTPServer)
	mux.Handle(server.LocalControlPlaneReadinessRoute, healthHTTPServer)
	writeTimeout := 15 * time.Second
	if runtimeSupervisor != nil {
		writeTimeout = 5*time.Minute + 15*time.Second
	}
	httpServer := &http.Server{
		Addr:              config.listen,
		Handler:           mux,
		BaseContext:       func(net.Listener) context.Context { return ctx },
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      writeTimeout,
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
	case err := <-tokenRefreshErrors:
		shutdownContext, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if shutdownErr := httpServer.Shutdown(shutdownContext); shutdownErr != nil {
			return errors.New("HTTP shutdown failed")
		}
		return err
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
	workerEndpoint := set.String("worker-endpoint", "", "loopback HTTP endpoint of the localdev Worker")
	workerTokenFile := set.String("worker-token-file", "", "0600 bearer token file written by the localdev Worker")
	workspaceDirectory := set.String("workspace-directory", "", "workspace passed to the local Runtime")
	if err := set.Parse(args); err != nil || set.NArg() != 0 {
		return controlPlaneConfig{}, errors.New("invalid control-plane configuration")
	}
	resolvedDatabaseURL := *databaseURL
	if resolvedDatabaseURL == "" && getenv != nil {
		resolvedDatabaseURL = getenv(databaseURLEnvironment)
	}
	resolvedWorkerEndpoint, resolvedWorkerTokenFile, resolvedWorkspaceDirectory := *workerEndpoint, *workerTokenFile, *workspaceDirectory
	if getenv != nil {
		if resolvedWorkerEndpoint == "" {
			resolvedWorkerEndpoint = getenv(localRuntimeWorkerEndpointEnvironment)
		}
		if resolvedWorkerTokenFile == "" {
			resolvedWorkerTokenFile = getenv(localRuntimeWorkerTokenEnvironment)
		}
		if resolvedWorkspaceDirectory == "" {
			resolvedWorkspaceDirectory = getenv(localRuntimeWorkspaceEnvironment)
		}
	}
	if *localTokenFile != "" && (strings.TrimSpace(*localTokenFile) != *localTokenFile || strings.HasSuffix(*localTokenFile, string(os.PathSeparator))) {
		return controlPlaneConfig{}, errInvalidTokenFilePath
	}
	if strings.TrimSpace(resolvedWorkerEndpoint) != resolvedWorkerEndpoint || strings.TrimSpace(resolvedWorkerTokenFile) != resolvedWorkerTokenFile || strings.TrimSpace(resolvedWorkspaceDirectory) != resolvedWorkspaceDirectory || (resolvedWorkerEndpoint == "") != (resolvedWorkerTokenFile == "") {
		return controlPlaneConfig{}, errInvalidRuntimeConfig
	}
	return controlPlaneConfig{
		listen:             *listen,
		databaseURL:        resolvedDatabaseURL,
		localTokenFile:     *localTokenFile,
		localTenantID:      *localTenantID,
		localSubject:       *localSubject,
		workerEndpoint:     resolvedWorkerEndpoint,
		workerTokenFile:    resolvedWorkerTokenFile,
		workspaceDirectory: resolvedWorkspaceDirectory,
	}, nil
}

func newLocalRuntimeSupervisor(ctx context.Context, endpoint, tokenFile string) (*workerclient.Supervisor, localRuntimeWorkerHealth, []byte, error) {
	endpoint, err := validateLocalWorkerEndpoint(endpoint)
	if err != nil {
		return nil, localRuntimeWorkerHealth{}, nil, err
	}
	token, err := readLocalWorkerToken(tokenFile)
	if err != nil {
		return nil, localRuntimeWorkerHealth{}, nil, err
	}
	protocols := new(http.Protocols)
	protocols.SetUnencryptedHTTP2(true)
	transport := &http.Transport{Proxy: nil, Protocols: protocols}
	httpClient := &http.Client{Transport: localWorkerBearerTransport{base: transport, token: token}, CheckRedirect: func(*http.Request, []*http.Request) error { return errInvalidRuntimeConfig }}
	healthRequest, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint+"/healthz", nil)
	if err != nil {
		return nil, localRuntimeWorkerHealth{}, nil, errInvalidRuntimeConfig
	}
	healthResponse, err := httpClient.Do(healthRequest)
	if err != nil {
		return nil, localRuntimeWorkerHealth{}, nil, errors.New("local Worker Runtime is unavailable")
	}
	defer healthResponse.Body.Close()
	healthBody, err := io.ReadAll(io.LimitReader(healthResponse.Body, 64*1024+1))
	if err != nil || len(healthBody) > 64*1024 {
		return nil, localRuntimeWorkerHealth{}, nil, errInvalidRuntimeConfig
	}
	var health localRuntimeWorkerHealth
	decoder := json.NewDecoder(bytes.NewReader(healthBody))
	decoder.DisallowUnknownFields()
	if healthResponse.StatusCode != http.StatusOK || decoder.Decode(&health) != nil || decoder.Decode(&struct{}{}) != io.EOF || health.APIVersion != "cloud-agents.localdev/v1" || health.Authority != "cloud-agents-localdev" || health.Profile != "cloud-agents/worker-localdev-runtime/v1" || health.Revision != "v1" || health.Version != "v1.0" || health.Status != "serving" || health.LeaseID == "" || health.Generation == 0 || !health.ExternalEffects || health.Transport != "loopback_http_connect" {
		return nil, localRuntimeWorkerHealth{}, nil, errInvalidRuntimeConfig
	}
	workerIdentity, err := localWorkerIdentity(health.WorkerIdentity)
	if err != nil {
		return nil, localRuntimeWorkerHealth{}, nil, errInvalidRuntimeConfig
	}
	supervisor, err := workerclient.New(workerclient.Config{
		Client: workerv1alpha1connect.NewWorkerExecutionServiceClient(httpClient, endpoint), RuntimeClient: workerruntimev1alpha1connect.NewWorkerRuntimeServiceClient(httpClient, endpoint), ExpectedWorkerIdentity: workerIdentity, Clock: time.Now,
	})
	if err != nil {
		return nil, localRuntimeWorkerHealth{}, nil, errInvalidRuntimeConfig
	}
	bindContext, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	if err := supervisor.BindRuntime(bindContext); err != nil {
		return nil, localRuntimeWorkerHealth{}, nil, errors.New("local Worker Runtime is unavailable")
	}
	return supervisor, health, []byte(token), nil
}

type localWorkerBearerTransport struct {
	base  http.RoundTripper
	token string
}

func (transport localWorkerBearerTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	clone := request.Clone(request.Context())
	clone.Header.Set("Authorization", "Bearer "+transport.token)
	return transport.base.RoundTrip(clone)
}

func validateLocalWorkerEndpoint(value string) (string, error) {
	parsed, err := url.Parse(value)
	if err != nil || parsed.Scheme != "http" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" || (parsed.Path != "" && parsed.Path != "/") {
		return "", errInvalidRuntimeConfig
	}
	host, port, err := net.SplitHostPort(parsed.Host)
	if err != nil || port == "" {
		return "", errInvalidRuntimeConfig
	}
	portNumber, err := strconv.Atoi(port)
	if err != nil || portNumber < 1 || portNumber > 65535 {
		return "", errInvalidRuntimeConfig
	}
	ip := net.ParseIP(host)
	if ip == nil || !ip.IsLoopback() {
		return "", errInvalidRuntimeConfig
	}
	return strings.TrimSuffix(value, "/"), nil
}

func readLocalWorkerToken(path string) (string, error) {
	if path == "" || strings.TrimSpace(path) != path {
		return "", errInvalidRuntimeConfig
	}
	info, err := os.Lstat(path)
	if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() || info.Mode().Perm() != 0o600 {
		return "", errInvalidRuntimeConfig
	}
	file, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return "", errInvalidRuntimeConfig
	}
	defer file.Close()
	stat, err := file.Stat()
	if err != nil || !os.SameFile(info, stat) || !stat.Mode().IsRegular() || stat.Mode().Perm() != 0o600 || stat.Size() <= 1 || stat.Size() > 257 {
		return "", errInvalidRuntimeConfig
	}
	initialSize := stat.Size()
	contents, err := io.ReadAll(io.LimitReader(file, 258))
	if err != nil || len(contents) < 2 || len(contents) > 257 || contents[len(contents)-1] != '\n' {
		return "", errInvalidRuntimeConfig
	}
	contents = contents[:len(contents)-1]
	token := string(contents)
	if !utf8.Valid(contents) || len(contents) > 256 || strings.TrimSpace(token) != token || strings.ContainsAny(token, " \t\r\n") {
		return "", errInvalidRuntimeConfig
	}
	for _, character := range token {
		if unicode.IsControl(character) {
			return "", errInvalidRuntimeConfig
		}
	}
	finalStat, err := file.Stat()
	if err != nil || !os.SameFile(info, finalStat) || finalStat.Size() != initialSize || finalStat.Mode().Perm() != 0o600 {
		return "", errInvalidRuntimeConfig
	}
	return token, nil
}

func localWorkerIdentity(value string) (*workerv1alpha1.WorkloadIdentity, error) {
	parsed, err := url.Parse(value)
	if err != nil || parsed.Scheme != "spiffe" || parsed.Host == "" || parsed.Path == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return nil, errInvalidRuntimeConfig
	}
	return &workerv1alpha1.WorkloadIdentity{SpiffeId: value, TrustDomain: parsed.Host}, nil
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

func replaceLocalTokenFile(path, token string) error {
	if path == "" || token == "" {
		return errInvalidTokenFilePath
	}
	file, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".*")
	if err != nil {
		return errors.New("cannot refresh local token file")
	}
	temporaryPath := file.Name()
	remove := true
	defer func() {
		if remove {
			_ = os.Remove(temporaryPath)
		}
	}()
	if _, err := file.WriteString(token + "\n"); err != nil {
		_ = file.Close()
		return errors.New("cannot refresh local token file")
	}
	if err := file.Close(); err != nil {
		return errors.New("cannot refresh local token file")
	}
	if err := os.Rename(temporaryPath, path); err != nil {
		return errors.New("cannot refresh local token file")
	}
	remove = false
	return nil
}

func refreshLocalTokenFile(ctx context.Context, verifier *authn.LocalVerifier, claims authn.LocalTokenClaims, path string, interval time.Duration) <-chan error {
	errorsChannel := make(chan error, 1)
	go func() {
		if ctx == nil || interval <= 0 {
			errorsChannel <- errors.New("local token refresh failed")
			return
		}
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				token, err := verifier.IssueToken(claims)
				if err == nil {
					err = replaceLocalTokenFile(path, token)
				}
				if err != nil {
					errorsChannel <- errors.New("local token refresh failed")
					return
				}
			}
		}
	}()
	return errorsChannel
}
