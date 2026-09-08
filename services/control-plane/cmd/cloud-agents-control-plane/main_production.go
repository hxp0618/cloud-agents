//go:build !localdev

package main

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	workerv1alpha1 "github.com/hxp0618/cloud-agents/sdk/go/gen/cloudagents/worker/v1alpha1"
	commonv1alpha1 "github.com/hxp0618/cloud-agents/sdk/go/gen/common/v1alpha1"
	"github.com/hxp0618/cloud-agents/services/control-plane/internal/accessgrant"
	"github.com/hxp0618/cloud-agents/services/control-plane/internal/authn"
	"github.com/hxp0618/cloud-agents/services/control-plane/internal/dockertarget"
	"github.com/hxp0618/cloud-agents/services/control-plane/internal/foundationcontroller"
	"github.com/hxp0618/cloud-agents/services/control-plane/internal/kubernetestarget"
	"github.com/hxp0618/cloud-agents/services/control-plane/internal/localmigration"
	internalmanagedagent "github.com/hxp0618/cloud-agents/services/control-plane/internal/managedagent"
	"github.com/hxp0618/cloud-agents/services/control-plane/internal/opensandbox"
	internalremoteworker "github.com/hxp0618/cloud-agents/services/control-plane/internal/remoteworker"
	"github.com/hxp0618/cloud-agents/services/control-plane/internal/server"
	"github.com/hxp0618/cloud-agents/services/control-plane/internal/sshtarget"
	"github.com/hxp0618/cloud-agents/services/control-plane/internal/store/postgres"
	"github.com/hxp0618/cloud-agents/services/control-plane/internal/workerclient"
	"github.com/hxp0618/cloud-agents/services/control-plane/internal/workerhealth"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	productionDatabaseEnvironment                = "CLOUD_AGENTS_PLATFORM_DATABASE_URL"
	productionAuthConfigEnvironment              = "CLOUD_AGENTS_PLATFORM_AUTH_CONFIG"
	productionWorkerEndpointEnvironment          = "CLOUD_AGENTS_PLATFORM_WORKER_ENDPOINT"
	productionWorkerSPIFFEEnvironment            = "CLOUD_AGENTS_PLATFORM_WORKER_SPIFFE_ID"
	productionWorkerClientCertEnvironment        = "CLOUD_AGENTS_PLATFORM_WORKER_CLIENT_CERT"
	productionWorkerClientKeyEnvironment         = "CLOUD_AGENTS_PLATFORM_WORKER_CLIENT_KEY"
	productionWorkerCAEnvironment                = "CLOUD_AGENTS_PLATFORM_WORKER_CA"
	productionWorkspaceEnvironment               = "CLOUD_AGENTS_PLATFORM_WORKSPACE_DIRECTORY"
	productionDockerCredentialsEnvironment       = "CLOUD_AGENTS_PLATFORM_DOCKER_CREDENTIALS_DIRECTORY"
	productionKubernetesCredentialsEnvironment   = "CLOUD_AGENTS_PLATFORM_KUBERNETES_CREDENTIALS_DIRECTORY"
	productionSSHCredentialsEnvironment          = "CLOUD_AGENTS_PLATFORM_SSH_CREDENTIALS_DIRECTORY"
	productionAccessGrantKeyEnvironment          = "CLOUD_AGENTS_PLATFORM_ACCESS_GRANT_KEY_FILE"
	productionRemoteWorkerCACertEnvironment      = "CLOUD_AGENTS_PLATFORM_REMOTE_WORKER_CA_CERT"
	productionRemoteWorkerCAKeyEnvironment       = "CLOUD_AGENTS_PLATFORM_REMOTE_WORKER_CA_KEY"
	productionRemoteWorkerTrustDomainEnvironment = "CLOUD_AGENTS_PLATFORM_REMOTE_WORKER_TRUST_DOMAIN"
	productionAdmissionLeaseEnvironment          = "CLOUD_AGENTS_PLATFORM_ADMISSION_LEASE_ID"
	productionAdmissionGenerationEnvironment     = "CLOUD_AGENTS_PLATFORM_ADMISSION_GENERATION"
	productionAdmissionTokenEnvironment          = "CLOUD_AGENTS_PLATFORM_ADMISSION_TOKEN"
	maxAuthConfigBytes                           = 1 << 20
	maxProductionCABytes                         = 1 << 20
	productionRuntimeMaxDuration                 = 5 * time.Minute
	productionHTTPWriteGrace                     = 15 * time.Second
	productionJWKSFetchTimeout                   = 5 * time.Second
	maxJWKSResponseBytes                         = 1 << 20
	defaultProductionMaxConcurrentRequests       = 128
	maximumProductionMaxConcurrentRequests       = 10_000
)

var version = "dev"

type productionConfig struct {
	listen                  string
	database                string
	authPath                string
	tlsCert                 string
	tlsKey                  string
	workerEndpoint          string
	workerSPIFFE            string
	workerClientCert        string
	workerClientKey         string
	workerCA                string
	workspaceDirectory      string
	dockerCredentials       string
	kubernetesCredentials   string
	sshCredentials          string
	accessGrantKey          string
	remoteWorkerCACert      string
	remoteWorkerCAKey       string
	remoteWorkerTrustDomain string
	admissionLeaseID        string
	admissionGeneration     uint64
	admissionToken          []byte
	maxConcurrentRequests   int
}

type authConfigFile struct {
	Issuer        string          `json:"issuer"`
	Audience      string          `json:"audience"`
	AdminAudience string          `json:"adminAudience"`
	JWKSURL       string          `json:"jwksUrl,omitempty"`
	Generation    int64           `json:"generation"`
	SecurityEpoch int64           `json:"securityEpoch"`
	NotBefore     int64           `json:"notBefore"`
	ExpiresAt     int64           `json:"expiresAt"`
	Keys          []authConfigKey `json:"keys"`
}

type authConfigKey struct {
	JWK       json.RawMessage `json:"jwk"`
	Enabled   bool            `json:"enabled"`
	NotBefore int64           `json:"notBefore"`
	NotAfter  int64           `json:"notAfter"`
}

type productionStatusWriter struct {
	http.ResponseWriter
	status int
}

func (writer *productionStatusWriter) WriteHeader(status int) {
	if writer.status != 0 {
		return
	}
	writer.status = status
	writer.ResponseWriter.WriteHeader(status)
}

func (writer *productionStatusWriter) Write(body []byte) (int, error) {
	if writer.status == 0 {
		writer.WriteHeader(http.StatusOK)
	}
	return writer.ResponseWriter.Write(body)
}

func (writer *productionStatusWriter) Unwrap() http.ResponseWriter { return writer.ResponseWriter }

func productionAccessLogHandler(logger *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		started := time.Now()
		writer := &productionStatusWriter{ResponseWriter: response}
		next.ServeHTTP(writer, request)
		status := writer.status
		if status == 0 {
			status = http.StatusOK
		}
		path := request.URL.Path
		if status < http.StatusBadRequest && (path == "/healthz" || path == "/readyz") {
			return
		}
		logger.InfoContext(request.Context(), "http request",
			"method", request.Method,
			"path", path,
			"status", status,
			"duration_ms", time.Since(started).Milliseconds(),
			"request_id", writer.Header().Get("X-Request-ID"),
		)
	})
}

func main() {
	if len(os.Args) == 2 && os.Args[1] == "--version" {
		_, _ = fmt.Printf("cloud-agents-control-plane %s\n", version)
		return
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := runProduction(ctx, os.Args[1:], os.Getenv); err != nil {
		_, _ = fmt.Fprintln(os.Stderr, "cloud-agents-control-plane:", err)
		os.Exit(2)
	}
}

func runProduction(ctx context.Context, args []string, getenv func(string) string) error {
	config, err := parseProductionConfig(args, getenv)
	if err != nil {
		return err
	}
	logger := slog.New(slog.NewJSONHandler(os.Stderr, nil))
	userVerifier, adminVerifier, err := loadConfiguredVerifiers(config.authPath)
	if err != nil {
		return err
	}
	defer userVerifier.Invalidate()
	defer adminVerifier.Invalidate()
	hup := make(chan os.Signal, 1)
	signal.Notify(hup, syscall.SIGHUP)
	defer signal.Stop(hup)
	pool, err := pgxpool.New(ctx, config.database)
	if err != nil {
		return errors.New("database pool configuration failed")
	}
	defer pool.Close()
	if err := pool.Ping(ctx); err != nil {
		return errors.New("database is unavailable")
	}
	coordinationService, err := postgres.NewDurableCoordinationService(pool)
	if err != nil {
		return errors.New("control-plane store is unavailable")
	}
	rbacMutationService, err := postgres.NewRBACMutationService(pool)
	if err != nil {
		return errors.New("RBAC mutation store is unavailable")
	}
	workerClientCertificate, err := tls.LoadX509KeyPair(config.workerClientCert, config.workerClientKey)
	if err != nil {
		return errors.New("worker client certificate is invalid")
	}
	workerCAs, err := readProductionCAPool(config.workerCA)
	if err != nil {
		return errors.New("worker CA configuration is invalid")
	}
	var workerSupervisor *workerclient.Supervisor
	healthContext, cancelHealth := context.WithCancel(ctx)
	healthDone := make(chan struct{})
	go func() {
		defer close(healthDone)
		workerhealth.Run(healthContext, pool, workerClientCertificate, workerCAs, logger)
	}()
	defer func() { cancelHealth(); <-healthDone }()
	if config.workerEndpoint != "" {
		workerIdentity, identityErr := productionWorkerIdentity(config.workerSPIFFE)
		if identityErr != nil {
			return errors.New("worker identity configuration is invalid")
		}
		workerSupervisor, err = workerclient.NewMTLS(workerclient.MTLSConfig{Endpoint: config.workerEndpoint, ExpectedWorkerIdentity: workerIdentity, ClientCertificate: workerClientCertificate, RootCAs: workerCAs, Clock: time.Now})
		if err != nil {
			return errors.New("worker transport configuration is invalid")
		}
	}
	runtimeCoordinator, err := internalmanagedagent.NewDurableRuntimeExecutionCoordinator(internalmanagedagent.DurableRuntimeExecutionConfig{
		Store: coordinationService, Supervisor: workerSupervisor, WorkerClientCertificate: workerClientCertificate, WorkerRootCAs: workerCAs, Clock: time.Now,
		FencingLeaseID: config.admissionLeaseID, FencingGeneration: config.admissionGeneration, FencingToken: config.admissionToken,
		WorkspaceDirectory: config.workspaceDirectory, MaxDuration: productionRuntimeMaxDuration,
	})
	if err != nil {
		return errors.New("managed agent Runtime coordinator is unavailable")
	}
	projectCreator, err := server.NewDurableProjectCreateServer(coordinationService)
	if err != nil {
		return errors.New("project create server is unavailable")
	}
	projectServer, err := server.NewProjectHTTPServer(userVerifier, coordinationService, projectCreator)
	if err != nil {
		return errors.New("project HTTP server is unavailable")
	}
	var dockerProber *dockertarget.CredentialDirectory
	grantCodec, err := accessgrant.Load(config.accessGrantKey)
	if err != nil {
		return errors.New("Sandbox access Grant key is invalid")
	}
	if config.dockerCredentials != "" {
		dockerProber, err = dockertarget.NewCredentialDirectory(config.dockerCredentials)
		if err != nil {
			return errors.New("Docker target credential directory is invalid")
		}
	}
	var kubernetesProber *kubernetestarget.CredentialDirectory
	if config.kubernetesCredentials != "" {
		kubernetesProber, err = kubernetestarget.NewCredentialDirectory(config.kubernetesCredentials)
		if err != nil {
			return errors.New("Kubernetes target credential directory is invalid")
		}
	}
	credentialDirectories := make([]string, 0, 2)
	if config.dockerCredentials != "" {
		credentialDirectories = append(credentialDirectories, config.dockerCredentials)
	}
	if config.kubernetesCredentials != "" {
		credentialDirectories = append(credentialDirectories, config.kubernetesCredentials)
	}
	var sandboxCredentials *opensandbox.CredentialDirectory
	if len(credentialDirectories) != 0 {
		sandboxCredentials, err = opensandbox.NewCredentialDirectory(credentialDirectories...)
		if err != nil {
			return errors.New("OpenSandbox credential directory is invalid")
		}
		foundationController, controllerErr := foundationcontroller.New(coordinationService, dockerProber, kubernetesProber, sandboxCredentials)
		if controllerErr != nil {
			return errors.New("foundation controller is unavailable")
		}
		foundationContext, cancelFoundation := context.WithCancel(ctx)
		foundationDone := make(chan struct{})
		go func() {
			defer close(foundationDone)
			foundationController.Run(foundationContext, logger)
		}()
		defer func() { cancelFoundation(); <-foundationDone }()
	}
	var sshProber *sshtarget.CredentialDirectory
	if config.sshCredentials != "" {
		sshProber, err = sshtarget.NewCredentialDirectory(config.sshCredentials)
		if err != nil {
			return errors.New("SSH target credential directory is invalid")
		}
	}
	deploymentTargetServer, err := server.NewDeploymentTargetHTTPServer(userVerifier, coordinationService, dockerProber, kubernetesProber, sshProber)
	if err != nil {
		return errors.New("deployment target HTTP server is unavailable")
	}
	adminDeploymentTargetServer, err := server.NewAdminDeploymentTargetHTTPServer(adminVerifier, coordinationService, dockerProber, kubernetesProber, sshProber)
	if err != nil {
		return errors.New("admin deployment target HTTP server is unavailable")
	}
	adminEnvironmentLeaseServer, err := server.NewAdminEnvironmentLeaseHTTPServer(adminVerifier, coordinationService, dockerProber, kubernetesProber, sshProber, dockertarget.WorkerTrust{ClientCertificate: workerClientCertificate, RootCAs: workerCAs})
	if err != nil {
		return errors.New("admin environment lease HTTP server is unavailable")
	}
	adminEnvironmentProfileServer, err := server.NewAdminEnvironmentProfileHTTPServer(adminVerifier, coordinationService)
	if err != nil {
		return errors.New("admin environment profile HTTP server is unavailable")
	}
	adminWorkerReleaseServer, err := server.NewAdminWorkerReleaseHTTPServer(adminVerifier, coordinationService)
	if err != nil {
		return errors.New("admin worker release HTTP server is unavailable")
	}
	projectLeaseQuotaServer, err := server.NewProjectLeaseQuotaHTTPServer(userVerifier, coordinationService)
	if err != nil {
		return errors.New("project lease quota HTTP server is unavailable")
	}
	adminProjectLeaseQuotaServer, err := server.NewProjectLeaseQuotaHTTPServer(adminVerifier, coordinationService)
	if err != nil {
		return errors.New("admin project lease quota HTTP server is unavailable")
	}
	networkPolicyServer, err := server.NewNetworkPolicyHTTPServer(adminVerifier, coordinationService)
	if err != nil {
		return errors.New("network policy HTTP server is unavailable")
	}
	var remoteWorkerCertificateAuthority *internalremoteworker.CertificateAuthority
	var remoteWorkerClientCAs *x509.CertPool
	if config.remoteWorkerCACert != "" {
		certificatePEM, readErr := readProductionFile(config.remoteWorkerCACert, maxProductionCABytes)
		if readErr != nil {
			return errors.New("RemoteWorker CA certificate is invalid")
		}
		privateKeyPEM, readErr := readProductionFile(config.remoteWorkerCAKey, maxProductionCABytes)
		if readErr != nil {
			return errors.New("RemoteWorker CA private key is invalid")
		}
		remoteWorkerCertificateAuthority, err = internalremoteworker.NewCertificateAuthority(certificatePEM, privateKeyPEM, config.remoteWorkerTrustDomain)
		if err != nil {
			return errors.New("RemoteWorker certificate authority is invalid")
		}
		remoteWorkerClientCAs, err = remoteWorkerCertificateAuthority.ClientCAPool()
		if err != nil {
			return errors.New("RemoteWorker client CA pool is invalid")
		}
	}
	remoteWorkerEnrollmentServer, err := server.NewRemoteWorkerEnrollmentHTTPServer(userVerifier, coordinationService, remoteWorkerCertificateAuthority)
	if err != nil {
		return errors.New("RemoteWorker enrollment HTTP server is unavailable")
	}
	adminRemoteWorkerEnrollmentServer, err := server.NewRemoteWorkerEnrollmentHTTPServer(adminVerifier, coordinationService, remoteWorkerCertificateAuthority)
	if err != nil {
		return errors.New("admin RemoteWorker enrollment HTTP server is unavailable")
	}
	storagePolicyServer, err := server.NewStoragePolicyHTTPServer(adminVerifier, coordinationService)
	if err != nil {
		return errors.New("storage policy HTTP server is unavailable")
	}
	publishedEnvironmentProfileServer, err := server.NewPublishedEnvironmentProfileHTTPServer(userVerifier, coordinationService)
	if err != nil {
		return errors.New("published environment profile HTTP server is unavailable")
	}
	foundationServer, err := server.NewFoundationHTTPServer(userVerifier, coordinationService, sandboxCredentials, grantCodec)
	if err != nil {
		return errors.New("foundation HTTP server is unavailable")
	}
	adminFoundationServer, err := server.NewFoundationHTTPServer(adminVerifier, coordinationService, sandboxCredentials, grantCodec)
	if err != nil {
		return errors.New("admin foundation HTTP server is unavailable")
	}
	tenantServer, err := server.NewPlatformTenantHTTPServer(userVerifier, coordinationService)
	if err != nil {
		return errors.New("tenant HTTP server is unavailable")
	}
	organizationServer, err := server.NewOrganizationHTTPServer(userVerifier, coordinationService)
	if err != nil {
		return errors.New("organization HTTP server is unavailable")
	}
	roleServer, err := server.NewRoleHTTPServer(userVerifier, coordinationService)
	if err != nil {
		return errors.New("role HTTP server is unavailable")
	}
	rbacServer, err := server.NewRBACHTTPServer(userVerifier, coordinationService, rbacMutationService)
	if err != nil {
		return errors.New("RBAC HTTP server is unavailable")
	}
	sessionServer, err := server.NewManagedAgentSessionHTTPServer(userVerifier, coordinationService)
	if err != nil {
		return errors.New("managed agent session HTTP server is unavailable")
	}
	eventsServer, err := server.NewManagedAgentEventsHTTPServer(userVerifier, coordinationService)
	if err != nil {
		return errors.New("managed agent events HTTP server is unavailable")
	}
	turnServer, err := server.NewManagedAgentTurnHTTPServer(userVerifier, coordinationService)
	if err != nil {
		return errors.New("managed agent turn HTTP server is unavailable")
	}
	executionServer, err := server.NewManagedAgentExecutionHTTPServer(userVerifier, coordinationService, runtimeCoordinator)
	if err != nil {
		return errors.New("managed agent execution HTTP server is unavailable")
	}
	leaseServer, err := server.NewManagedHostEnvironmentLeaseHTTPServer(userVerifier, coordinationService, dockerProber, kubernetesProber, sshProber, dockertarget.WorkerTrust{ClientCertificate: workerClientCertificate, RootCAs: workerCAs})
	if err != nil {
		return errors.New("managed host environment lease HTTP server is unavailable")
	}
	userEnvironmentServer, err := server.NewUserEnvironmentHTTPServer(userVerifier, coordinationService, leaseServer)
	if err != nil {
		return errors.New("user environment HTTP server is unavailable")
	}
	mux := http.NewServeMux()
	mux.Handle("/v1/admin/", server.AdminDeniedWriteHandler(adminVerifier, coordinationService, http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if server.HandlesFoundationPath(request.URL.Path) {
			adminFoundationServer.ServeHTTP(writer, request)
			return
		}
		if server.HandlesNetworkPolicyPath(request.URL.Path) {
			networkPolicyServer.ServeHTTP(writer, request)
			return
		}
		if server.HandlesRemoteWorkerEnrollmentPath(request.URL.Path) {
			adminRemoteWorkerEnrollmentServer.ServeHTTP(writer, request)
			return
		}
		if server.HandlesStoragePolicyPath(request.URL.Path) {
			storagePolicyServer.ServeHTTP(writer, request)
			return
		}
		if server.HandlesProjectLeaseQuotaPath(request.URL.Path) {
			adminProjectLeaseQuotaServer.ServeHTTP(writer, request)
			return
		}
		if server.HandlesAdminWorkerReleasePath(request.URL.Path) {
			adminWorkerReleaseServer.ServeHTTP(writer, request)
			return
		}
		if server.HandlesAdminEnvironmentProfilePath(request.URL.Path) {
			adminEnvironmentProfileServer.ServeHTTP(writer, request)
			return
		}
		if server.HandlesAdminEnvironmentLeasePath(request.URL.Path) {
			adminEnvironmentLeaseServer.ServeHTTP(writer, request)
			return
		}
		adminDeploymentTargetServer.ServeHTTP(writer, request)
	})))
	mux.Handle("/v1/remote-worker-bootstrap/", remoteWorkerEnrollmentServer)
	mux.Handle("/v1/remote-workers/", remoteWorkerEnrollmentServer)
	mux.Handle(server.OrganizationCollectionRoute, organizationServer)
	mux.Handle(server.OrganizationRoute, organizationServer)
	mux.Handle(server.RoleCollectionRoute, roleServer)
	mux.Handle(server.RoleRoute, roleServer)
	mux.Handle(server.MembershipRoute, rbacServer)
	mux.Handle(server.MembershipCollectionRoute, rbacServer)
	mux.Handle(server.RoleBindingRoute, rbacServer)
	mux.Handle(server.RoleBindingCollectionRoute, rbacServer)
	mux.Handle(server.ManagedHostProjectRoute, projectServer)
	mux.Handle(server.ManagedHostRoleBindingRoute, rbacServer)
	mux.Handle(server.ManagedHostEnvironmentLeaseRoutePrefix, leaseServer)
	mux.Handle(server.PlatformTenantRoute, tenantServer)
	mux.Handle(server.ProjectRoutePrefix, http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if server.HandlesFoundationPath(request.URL.Path) {
			foundationServer.ServeHTTP(writer, request)
			return
		}
		if server.HandlesProjectLeaseQuotaPath(request.URL.Path) {
			projectLeaseQuotaServer.ServeHTTP(writer, request)
			return
		}
		if server.HandlesUserEnvironmentPath(request.URL.Path) {
			userEnvironmentServer.ServeHTTP(writer, request)
			return
		}
		if server.HandlesPublishedEnvironmentProfilePath(request.URL.Path) {
			publishedEnvironmentProfileServer.ServeHTTP(writer, request)
			return
		}
		if server.HandlesDeploymentTargetPath(request.URL.Path) {
			deploymentTargetServer.ServeHTTP(writer, request)
			return
		}
		if server.HandlesRBACPath(request.URL.Path) {
			rbacServer.ServeHTTP(writer, request)
			return
		}
		if server.HandlesManagedAgentExecutionPath(request.URL.Path) {
			executionServer.ServeHTTP(writer, request)
			return
		}
		if server.HandlesManagedAgentEventsPath(request.URL.Path) {
			eventsServer.ServeHTTP(writer, request)
			return
		}
		if server.HandlesManagedAgentTurnPath(request.URL.Path) {
			turnServer.ServeHTTP(writer, request)
			return
		}
		if server.HandlesManagedAgentSessionPath(request.URL.Path) {
			sessionServer.ServeHTTP(writer, request)
			return
		}
		projectServer.ServeHTTP(writer, request)
	}))
	mux.HandleFunc("/healthz", func(writer http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodGet {
			writer.Header().Set("Allow", http.MethodGet)
			writer.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		writer.WriteHeader(http.StatusOK)
	})
	mux.HandleFunc("/readyz", func(writer http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodGet {
			writer.Header().Set("Allow", http.MethodGet)
			writer.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		if !userVerifier.Ready() || !adminVerifier.Ready() {
			writer.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		if err := pool.Ping(request.Context()); err != nil {
			writer.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		if err := localmigration.CheckProductSchemaReadiness(request.Context(), pool); err != nil {
			writer.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		if workerSupervisor != nil {
			workerContext, cancel := context.WithTimeout(request.Context(), 5*time.Second)
			defer cancel()
			if err := workerSupervisor.CheckRuntimeHealth(workerContext); err != nil {
				writer.WriteHeader(http.StatusServiceUnavailable)
				return
			}
		}
		writer.WriteHeader(http.StatusOK)
	})
	httpServer := &http.Server{Addr: config.listen, Handler: productionAccessLogHandler(logger, server.ConcurrentRequestLimitHandler(config.maxConcurrentRequests, server.JSONContentTypeHandler(mux))), BaseContext: func(net.Listener) context.Context { return ctx }, ErrorLog: slog.NewLogLogger(logger.Handler(), slog.LevelError), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second, WriteTimeout: productionRuntimeMaxDuration + productionHTTPWriteGrace, IdleTimeout: 30 * time.Second, MaxHeaderBytes: 64 << 10}
	if remoteWorkerClientCAs != nil {
		httpServer.TLSConfig = &tls.Config{MinVersion: tls.VersionTLS12, ClientAuth: tls.VerifyClientCertIfGiven, ClientCAs: remoteWorkerClientCAs}
	}
	errorChannel := make(chan error, 1)
	go func() {
		if config.tlsCert != "" {
			errorChannel <- httpServer.ListenAndServeTLS(config.tlsCert, config.tlsKey)
			return
		}
		errorChannel <- httpServer.ListenAndServe()
	}()
	for {
		select {
		case <-ctx.Done():
			shutdownContext, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			return httpServer.Shutdown(shutdownContext)
		case <-hup:
			userRefreshed, adminRefreshed, refreshErr := loadConfiguredVerifierConfigs(config.authPath)
			if refreshErr != nil {
				logger.Error("authentication reload failed", "error", refreshErr)
				continue
			}
			if refreshErr = userVerifier.Reload(userRefreshed); refreshErr != nil {
				logger.Error("authentication reload failed", "error", refreshErr)
				continue
			}
			if refreshErr = adminVerifier.Reload(adminRefreshed); refreshErr != nil {
				logger.Error("admin authentication reload failed", "error", refreshErr)
				continue
			}
			logger.Info("authentication reloaded", "generation", userRefreshed.Generation)
		case err := <-errorChannel:
			if errors.Is(err, http.ErrServerClosed) {
				return nil
			}
			return errors.New("HTTP server stopped")
		}
	}
}

func parseProductionConfig(args []string, getenv func(string) string) (productionConfig, error) {
	set := flag.NewFlagSet("cloud-agents-control-plane", flag.ContinueOnError)
	set.SetOutput(io.Discard)
	listen := set.String("listen", ":8080", "listen address")
	database := set.String("database-url", "", "PostgreSQL URL")
	authPath := set.String("auth-config", "", "JSON trust configuration path")
	tlsCert := set.String("tls-cert", "", "TLS certificate path")
	tlsKey := set.String("tls-key", "", "TLS private key path")
	workerEndpoint := set.String("worker-endpoint", "", "Worker HTTPS endpoint")
	workerSPIFFE := set.String("worker-spiffe-id", "", "expected Worker SPIFFE identity")
	workerClientCert := set.String("worker-client-cert", "", "Worker mTLS client certificate path")
	workerClientKey := set.String("worker-client-key", "", "Worker mTLS client key path")
	workerCA := set.String("worker-ca", "", "Worker CA certificate path")
	workspaceDirectory := set.String("workspace-directory", "", "Runtime workspace directory on the Worker")
	dockerCredentials := set.String("docker-credentials-directory", "", "deployment-owned Docker mTLS credential directory")
	kubernetesCredentials := set.String("kubernetes-credentials-directory", "", "deployment-owned Kubernetes ServiceAccount credential directory")
	sshCredentials := set.String("ssh-credentials-directory", "", "deployment-owned SSH credential directory")
	accessGrantKey := set.String("access-grant-key-file", "", "shared 32-64 byte Sandbox access Grant key file")
	remoteWorkerCACert := set.String("remote-worker-ca-cert", "", "RemoteWorker enrollment CA certificate path")
	remoteWorkerCAKey := set.String("remote-worker-ca-key", "", "RemoteWorker enrollment CA private key path")
	remoteWorkerTrustDomain := set.String("remote-worker-trust-domain", "", "RemoteWorker SPIFFE trust domain")
	admissionLeaseID := set.String("admission-lease-id", "", "authoritative Runtime lease id")
	admissionGeneration := set.Uint64("admission-generation", 0, "authoritative Runtime fencing generation")
	maxConcurrentRequests := set.Int("max-concurrent-requests", defaultProductionMaxConcurrentRequests, "maximum concurrent API requests")
	if err := set.Parse(args); err != nil || set.NArg() != 0 {
		return productionConfig{}, errors.New("invalid control-plane configuration")
	}
	if *maxConcurrentRequests < 1 || *maxConcurrentRequests > maximumProductionMaxConcurrentRequests {
		return productionConfig{}, errors.New("invalid control-plane configuration")
	}
	if *database == "" && getenv != nil {
		*database = getenv(productionDatabaseEnvironment)
	}
	if *authPath == "" && getenv != nil {
		*authPath = getenv(productionAuthConfigEnvironment)
	}
	fill := func(value *string, name string) {
		if *value == "" && getenv != nil {
			*value = getenv(name)
		}
	}
	fill(workerEndpoint, productionWorkerEndpointEnvironment)
	fill(workerSPIFFE, productionWorkerSPIFFEEnvironment)
	fill(workerClientCert, productionWorkerClientCertEnvironment)
	fill(workerClientKey, productionWorkerClientKeyEnvironment)
	fill(workerCA, productionWorkerCAEnvironment)
	fill(workspaceDirectory, productionWorkspaceEnvironment)
	fill(dockerCredentials, productionDockerCredentialsEnvironment)
	fill(kubernetesCredentials, productionKubernetesCredentialsEnvironment)
	fill(sshCredentials, productionSSHCredentialsEnvironment)
	fill(accessGrantKey, productionAccessGrantKeyEnvironment)
	fill(remoteWorkerCACert, productionRemoteWorkerCACertEnvironment)
	fill(remoteWorkerCAKey, productionRemoteWorkerCAKeyEnvironment)
	fill(remoteWorkerTrustDomain, productionRemoteWorkerTrustDomainEnvironment)
	fill(admissionLeaseID, productionAdmissionLeaseEnvironment)
	if strings.TrimSpace(*dockerCredentials) != *dockerCredentials || strings.TrimSpace(*kubernetesCredentials) != *kubernetesCredentials || strings.TrimSpace(*sshCredentials) != *sshCredentials || strings.TrimSpace(*accessGrantKey) != *accessGrantKey || strings.TrimSpace(*remoteWorkerCACert) != *remoteWorkerCACert || strings.TrimSpace(*remoteWorkerCAKey) != *remoteWorkerCAKey || strings.TrimSpace(*remoteWorkerTrustDomain) != *remoteWorkerTrustDomain {
		return productionConfig{}, errors.New("invalid control-plane configuration")
	}
	if *admissionGeneration == 0 && getenv != nil {
		if raw := getenv(productionAdmissionGenerationEnvironment); raw != "" {
			parsed, parseErr := strconv.ParseUint(raw, 10, 64)
			if parseErr != nil {
				return productionConfig{}, errors.New("invalid control-plane configuration")
			}
			*admissionGeneration = parsed
		}
	}
	var admissionToken string
	if getenv != nil {
		admissionToken = getenv(productionAdmissionTokenEnvironment)
	}
	required := []string{*database, *authPath, *tlsCert, *tlsKey, *workerClientCert, *workerClientKey, *workerCA, *workspaceDirectory, *accessGrantKey, admissionToken}
	for _, value := range required {
		if value == "" || strings.TrimSpace(value) != value {
			return productionConfig{}, errors.New("database, authentication, TLS, Worker Runtime, and admission configuration are required")
		}
	}
	staticWorker := *workerEndpoint != "" || *workerSPIFFE != "" || *admissionLeaseID != "" || *admissionGeneration != 0
	if (staticWorker && (*workerEndpoint == "" || *workerSPIFFE == "" || *admissionLeaseID == "" || *admissionGeneration == 0)) || len(admissionToken) > 1<<20 {
		return productionConfig{}, errors.New("database, authentication, TLS, Worker Runtime, and admission configuration are required")
	}
	remoteWorkerAuthorityConfigured := *remoteWorkerCACert != "" || *remoteWorkerCAKey != "" || *remoteWorkerTrustDomain != ""
	if remoteWorkerAuthorityConfigured && (*remoteWorkerCACert == "" || *remoteWorkerCAKey == "" || *remoteWorkerTrustDomain == "") {
		return productionConfig{}, errors.New("RemoteWorker certificate authority configuration must be complete")
	}
	return productionConfig{
		listen: *listen, database: *database, authPath: *authPath, tlsCert: *tlsCert, tlsKey: *tlsKey,
		workerEndpoint: *workerEndpoint, workerSPIFFE: *workerSPIFFE, workerClientCert: *workerClientCert, workerClientKey: *workerClientKey, workerCA: *workerCA,
		workspaceDirectory: *workspaceDirectory, dockerCredentials: *dockerCredentials, kubernetesCredentials: *kubernetesCredentials, sshCredentials: *sshCredentials, accessGrantKey: *accessGrantKey, remoteWorkerCACert: *remoteWorkerCACert, remoteWorkerCAKey: *remoteWorkerCAKey, remoteWorkerTrustDomain: *remoteWorkerTrustDomain, admissionLeaseID: *admissionLeaseID, admissionGeneration: *admissionGeneration, admissionToken: []byte(admissionToken), maxConcurrentRequests: *maxConcurrentRequests,
	}, nil
}

func productionWorkerIdentity(value string) (*workerv1alpha1.WorkloadIdentity, error) {
	parsed, err := url.Parse(value)
	if strings.TrimSpace(value) != value || value == "" || err != nil || parsed.Scheme != "spiffe" || parsed.Host == "" || parsed.Path == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return nil, errors.New("invalid Worker identity")
	}
	return &workerv1alpha1.WorkloadIdentity{SpiffeId: value, TrustDomain: parsed.Host}, nil
}

func readProductionCAPool(path string) (*x509.CertPool, error) {
	contents, err := readProductionFile(path, maxProductionCABytes)
	if err != nil {
		return nil, errors.New("invalid CA bundle")
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(contents) {
		return nil, errors.New("invalid CA bundle")
	}
	return pool, nil
}

func readProductionFile(path string, maximum int64) ([]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	contents, err := io.ReadAll(io.LimitReader(file, maximum+1))
	if err != nil || int64(len(contents)) > maximum {
		return nil, errors.New("file exceeds limit")
	}
	return contents, nil
}

func loadConfiguredVerifiers(path string) (*authn.ConfiguredVerifier, *authn.ConfiguredVerifier, error) {
	userConfig, adminConfig, err := loadConfiguredVerifierConfigs(path)
	if err != nil {
		return nil, nil, err
	}
	userVerifier, err := authn.NewConfiguredVerifier(userConfig)
	if err != nil {
		return nil, nil, errors.New("auth configuration is invalid")
	}
	adminVerifier, err := authn.NewConfiguredVerifier(adminConfig)
	if err != nil {
		userVerifier.Invalidate()
		return nil, nil, errors.New("admin auth configuration is invalid")
	}
	return userVerifier, adminVerifier, nil
}

func loadConfiguredVerifierConfigs(path string) (authn.ConfiguredVerifierConfig, authn.ConfiguredVerifierConfig, error) {
	return loadConfiguredVerifierConfigsWith(path, fetchJWKS)
}

func loadConfiguredVerifierConfigsWith(path string, fetch func(string) ([]json.RawMessage, error)) (authn.ConfiguredVerifierConfig, authn.ConfiguredVerifierConfig, error) {
	file, err := os.Open(path)
	if err != nil {
		return authn.ConfiguredVerifierConfig{}, authn.ConfiguredVerifierConfig{}, errors.New("auth configuration cannot be opened")
	}
	defer file.Close()
	contents, err := io.ReadAll(io.LimitReader(file, maxAuthConfigBytes+1))
	if err != nil || len(contents) > maxAuthConfigBytes {
		return authn.ConfiguredVerifierConfig{}, authn.ConfiguredVerifierConfig{}, errors.New("auth configuration is invalid")
	}
	decoder := json.NewDecoder(bytes.NewReader(contents))
	decoder.DisallowUnknownFields()
	var input authConfigFile
	if err := decoder.Decode(&input); err != nil {
		return authn.ConfiguredVerifierConfig{}, authn.ConfiguredVerifierConfig{}, errors.New("auth configuration is invalid")
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return authn.ConfiguredVerifierConfig{}, authn.ConfiguredVerifierConfig{}, errors.New("auth configuration has trailing data")
	}
	if input.AdminAudience == "" || input.AdminAudience == input.Audience {
		return authn.ConfiguredVerifierConfig{}, authn.ConfiguredVerifierConfig{}, errors.New("auth configuration must use distinct User and Admin audiences")
	}
	if input.JWKSURL != "" && len(input.Keys) != 0 {
		return authn.ConfiguredVerifierConfig{}, authn.ConfiguredVerifierConfig{}, errors.New("auth configuration must select keys or jwksUrl")
	}
	keys := input.Keys
	if input.JWKSURL != "" {
		if fetch == nil {
			return authn.ConfiguredVerifierConfig{}, authn.ConfiguredVerifierConfig{}, errors.New("JWKS fetcher is required")
		}
		remoteKeys, err := fetch(input.JWKSURL)
		if err != nil {
			return authn.ConfiguredVerifierConfig{}, authn.ConfiguredVerifierConfig{}, errors.New("JWKS fetch failed")
		}
		keys = make([]authConfigKey, 0, len(remoteKeys))
		for _, key := range remoteKeys {
			normalized, supported, err := normalizeRemoteJWK(key)
			if err != nil {
				return authn.ConfiguredVerifierConfig{}, authn.ConfiguredVerifierConfig{}, errors.New("JWKS contains an invalid key")
			}
			if supported {
				keys = append(keys, authConfigKey{JWK: normalized, Enabled: true, NotBefore: input.NotBefore, NotAfter: input.ExpiresAt})
			}
		}
		if len(keys) == 0 {
			return authn.ConfiguredVerifierConfig{}, authn.ConfiguredVerifierConfig{}, errors.New("JWKS contains no supported RS256 key")
		}
	}
	configuredKeys := make([]authn.ConfiguredVerifierKey, len(keys))
	for index, key := range keys {
		configuredKeys[index] = authn.ConfiguredVerifierKey{JWK: key.JWK, Enabled: key.Enabled, NotBefore: key.NotBefore, NotAfter: key.NotAfter}
	}
	userConfig := authn.ConfiguredVerifierConfig{Issuer: input.Issuer, Audience: input.Audience, Generation: input.Generation, SecurityEpoch: input.SecurityEpoch, NotBefore: input.NotBefore, ExpiresAt: input.ExpiresAt, Keys: configuredKeys, Clock: time.Now}
	adminConfig := userConfig
	adminConfig.Audience = input.AdminAudience
	return userConfig, adminConfig, nil
}

func normalizeRemoteJWK(raw json.RawMessage) (json.RawMessage, bool, error) {
	fields, _, err := commonv1alpha1.DecodeJSONObjectWithSidecar(raw, []string{
		"alg", "d", "dp", "dq", "e", "k", "key_ops", "kid", "kty", "n", "oth", "p", "q", "qi", "use",
	})
	if err != nil {
		return nil, false, err
	}
	var key struct {
		Alg    string   `json:"alg"`
		E      string   `json:"e"`
		KeyOps []string `json:"key_ops"`
		Kid    string   `json:"kid"`
		Kty    string   `json:"kty"`
		N      string   `json:"n"`
		Use    string   `json:"use"`
	}
	if json.Unmarshal(raw, &key) != nil {
		return nil, false, errors.New("JWKS key fields are invalid")
	}
	for _, name := range []string{"d", "dp", "dq", "k", "oth", "p", "q", "qi"} {
		if _, present := fields[name]; present {
			return nil, false, errors.New("JWKS key contains private material")
		}
	}
	if key.Kty == "" {
		return nil, false, errors.New("JWKS key type is invalid")
	}
	if key.Kty != "RSA" {
		return nil, false, nil
	}
	if _, present := fields["alg"]; present && key.Alg == "" {
		return nil, false, errors.New("JWKS key algorithm is invalid")
	}
	if key.Alg != "" && key.Alg != "RS256" {
		return nil, false, nil
	}
	if _, present := fields["use"]; present && key.Use == "" {
		return nil, false, errors.New("JWKS key use is invalid")
	}
	if key.Use != "" && key.Use != "sig" {
		return nil, false, nil
	}
	if _, present := fields["key_ops"]; present {
		if len(key.KeyOps) != 1 || key.KeyOps[0] != "verify" {
			return nil, false, nil
		}
	}
	if key.Kid == "" || key.N == "" || key.E == "" {
		return nil, false, errors.New("JWKS RSA key is incomplete")
	}
	normalized, err := json.Marshal(struct {
		Alg    string   `json:"alg"`
		E      string   `json:"e"`
		KeyOps []string `json:"key_ops"`
		Kid    string   `json:"kid"`
		Kty    string   `json:"kty"`
		N      string   `json:"n"`
		Use    string   `json:"use"`
	}{Alg: "RS256", E: key.E, KeyOps: []string{"verify"}, Kid: key.Kid, Kty: "RSA", N: key.N, Use: "sig"})
	return normalized, true, err
}

func fetchJWKS(rawURL string) ([]json.RawMessage, error) {
	return fetchJWKSWithClient(rawURL, &http.Client{Timeout: productionJWKSFetchTimeout, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }})
}

func fetchJWKSWithClient(rawURL string, client *http.Client) ([]json.RawMessage, error) {
	parsed, err := url.Parse(rawURL)
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" || parsed.User != nil || parsed.Fragment != "" || strings.TrimSpace(rawURL) != rawURL {
		return nil, errors.New("JWKS URL must be an HTTPS URL")
	}
	if client == nil {
		return nil, errors.New("JWKS HTTP client is required")
	}
	request, err := http.NewRequest(http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("Accept", "application/json")
	response, err := client.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, errors.New("JWKS endpoint returned non-200")
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, maxJWKSResponseBytes+1))
	if err != nil || len(body) > maxJWKSResponseBytes {
		return nil, errors.New("JWKS response is too large")
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	var document struct {
		Keys []json.RawMessage `json:"keys"`
	}
	if err := decoder.Decode(&document); err != nil || len(document.Keys) == 0 {
		return nil, errors.New("JWKS response is invalid")
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return nil, errors.New("JWKS response has trailing data")
	}
	return document.Keys, nil
}
