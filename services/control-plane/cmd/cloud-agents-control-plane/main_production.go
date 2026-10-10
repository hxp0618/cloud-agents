//go:build !localdev

package main

import (
	"context"
	"crypto/tls"
	"crypto/x509"
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
	"github.com/hxp0618/cloud-agents/services/control-plane/internal/accessgrant"
	"github.com/hxp0618/cloud-agents/services/control-plane/internal/dockertarget"
	"github.com/hxp0618/cloud-agents/services/control-plane/internal/foundationcontroller"
	"github.com/hxp0618/cloud-agents/services/control-plane/internal/identitytrust"
	"github.com/hxp0618/cloud-agents/services/control-plane/internal/kubernetestarget"
	"github.com/hxp0618/cloud-agents/services/control-plane/internal/localmigration"
	internalmanagedagent "github.com/hxp0618/cloud-agents/services/control-plane/internal/managedagent"
	"github.com/hxp0618/cloud-agents/services/control-plane/internal/opensandbox"
	internalremoteworker "github.com/hxp0618/cloud-agents/services/control-plane/internal/remoteworker"
	"github.com/hxp0618/cloud-agents/services/control-plane/internal/server"
	"github.com/hxp0618/cloud-agents/services/control-plane/internal/sshtarget"
	"github.com/hxp0618/cloud-agents/services/control-plane/internal/store/postgres"
	"github.com/hxp0618/cloud-agents/services/control-plane/internal/targetcredential"
	"github.com/hxp0618/cloud-agents/services/control-plane/internal/workerclient"
	"github.com/hxp0618/cloud-agents/services/control-plane/internal/workerhealth"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	productionDatabaseEnvironment                  = "CLOUD_AGENTS_PLATFORM_DATABASE_URL"
	productionAuthConfigEnvironment                = "CLOUD_AGENTS_PLATFORM_AUTH_CONFIG"
	productionWorkerEndpointEnvironment            = "CLOUD_AGENTS_PLATFORM_WORKER_ENDPOINT"
	productionWorkerSPIFFEEnvironment              = "CLOUD_AGENTS_PLATFORM_WORKER_SPIFFE_ID"
	productionWorkerClientCertEnvironment          = "CLOUD_AGENTS_PLATFORM_WORKER_CLIENT_CERT"
	productionWorkerClientKeyEnvironment           = "CLOUD_AGENTS_PLATFORM_WORKER_CLIENT_KEY"
	productionWorkerCAEnvironment                  = "CLOUD_AGENTS_PLATFORM_WORKER_CA"
	productionWorkspaceEnvironment                 = "CLOUD_AGENTS_PLATFORM_WORKSPACE_DIRECTORY"
	productionProviderCredentialsEnvironment       = "CLOUD_AGENTS_PLATFORM_PROVIDER_CREDENTIALS_DIRECTORY"
	productionCapabilityMaterializationEnvironment = "CLOUD_AGENTS_PLATFORM_CAPABILITY_MATERIALIZATION_DIRECTORY"
	productionDockerCredentialsEnvironment         = "CLOUD_AGENTS_PLATFORM_DOCKER_CREDENTIALS_DIRECTORY"
	productionSnapshotDirectoryEnvironment         = "CLOUD_AGENTS_PLATFORM_SNAPSHOT_DIRECTORY"
	productionKubernetesCredentialsEnvironment     = "CLOUD_AGENTS_PLATFORM_KUBERNETES_CREDENTIALS_DIRECTORY"
	productionSSHCredentialsEnvironment            = "CLOUD_AGENTS_PLATFORM_SSH_CREDENTIALS_DIRECTORY"
	productionAccessGrantKeyEnvironment            = "CLOUD_AGENTS_PLATFORM_ACCESS_GRANT_KEY_FILE"
	productionTargetCredentialKeyEnvironment       = "CLOUD_AGENTS_PLATFORM_TARGET_CREDENTIAL_KEY_FILE"
	productionRemoteWorkerCACertEnvironment        = "CLOUD_AGENTS_PLATFORM_REMOTE_WORKER_CA_CERT"
	productionRemoteWorkerCAKeyEnvironment         = "CLOUD_AGENTS_PLATFORM_REMOTE_WORKER_CA_KEY"
	productionRemoteWorkerTrustDomainEnvironment   = "CLOUD_AGENTS_PLATFORM_REMOTE_WORKER_TRUST_DOMAIN"
	productionAdmissionLeaseEnvironment            = "CLOUD_AGENTS_PLATFORM_ADMISSION_LEASE_ID"
	productionAdmissionGenerationEnvironment       = "CLOUD_AGENTS_PLATFORM_ADMISSION_GENERATION"
	productionAdmissionTokenEnvironment            = "CLOUD_AGENTS_PLATFORM_ADMISSION_TOKEN"
	maxAuthConfigBytes                             = 1 << 20
	maxProductionCABytes                           = 1 << 20
	productionRuntimeMaxDuration                   = 30 * time.Minute
	productionHTTPWriteGrace                       = 15 * time.Second
	productionIdentityRefreshInterval              = time.Minute
	defaultProductionMaxConcurrentRequests         = 128
	maximumProductionMaxConcurrentRequests         = 10_000
)

var version = "dev"

type productionConfig struct {
	listen                    string
	database                  string
	authPath                  string
	tlsCert                   string
	tlsKey                    string
	workerEndpoint            string
	workerSPIFFE              string
	workerClientCert          string
	workerClientKey           string
	workerCA                  string
	workspaceDirectory        string
	providerCredentials       string
	capabilityMaterialization string
	dockerCredentials         string
	snapshotDirectory         string
	kubernetesCredentials     string
	sshCredentials            string
	accessGrantKey            string
	targetCredentialKey       string
	remoteWorkerCACert        string
	remoteWorkerCAKey         string
	remoteWorkerTrustDomain   string
	admissionLeaseID          string
	admissionGeneration       uint64
	admissionToken            []byte
	maxConcurrentRequests     int
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
	checkpoint, err := identitytrust.NewPostgresCheckpoint(pool)
	if err != nil {
		return errors.New("identity trust checkpoint is unavailable")
	}
	authentication, err := loadProductionIdentityAuthentication(ctx, config.authPath, checkpoint)
	if err != nil {
		return err
	}
	defer authentication.close()
	userVerifier, adminVerifier := authentication.userAccess, authentication.adminAccess
	tokenAuthorization, err := postgres.NewTokenAuthorizationService(pool)
	if err != nil {
		return errors.New("identity authorization store is unavailable")
	}
	identityAuthorizationServer, err := server.NewIdentityAuthorizationHTTPServer(tokenAuthorization, authentication.authorizationCredential)
	if err != nil {
		return errors.New("identity authorization server is unavailable")
	}
	principalAuthorizationServer, err := server.NewIdentityPrincipalAuthorizationHTTPServer(tokenAuthorization, authentication.authorizationCredential)
	if err != nil {
		return errors.New("identity principal authorization server is unavailable")
	}
	serviceAccountStore, err := postgres.NewServiceAccountStore(pool)
	if err != nil {
		return errors.New("service account store is unavailable")
	}
	serviceAccountServer, err := server.NewServiceAccountHTTPServer(adminVerifier, serviceAccountStore)
	if err != nil {
		return errors.New("service account HTTP server is unavailable")
	}
	coordinationService, err := postgres.NewDurableCoordinationService(pool)
	if err != nil {
		return errors.New("control-plane store is unavailable")
	}
	adminCoordinationService, err := postgres.NewAdminDurableCoordinationService(pool)
	if err != nil {
		return errors.New("Admin control-plane store is unavailable")
	}
	rbacMutationService, err := postgres.NewRBACMutationService(pool)
	if err != nil {
		return errors.New("RBAC mutation store is unavailable")
	}
	adminRBACMutationService, err := postgres.NewAdminRBACMutationService(pool)
	if err != nil {
		return errors.New("Admin RBAC mutation store is unavailable")
	}
	adminManagementServer, err := server.NewAdminManagementHTTPServer(adminVerifier, adminCoordinationService, adminRBACMutationService)
	if err != nil {
		return errors.New("Admin management HTTP server is unavailable")
	}
	var workerClientCertificate tls.Certificate
	var workerCAs *x509.CertPool
	var workerSupervisor *workerclient.Supervisor
	var runtimeCoordinator *internalmanagedagent.DurableRuntimeExecutionCoordinator
	if config.workerClientCert != "" {
		workerClientCertificate, err = tls.LoadX509KeyPair(config.workerClientCert, config.workerClientKey)
		if err != nil {
			return errors.New("worker client certificate is invalid")
		}
		workerCAs, err = readProductionCAPool(config.workerCA)
		if err != nil {
			return errors.New("worker CA configuration is invalid")
		}
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
	}
	projectCreator, err := server.NewDurableProjectCreateServer(coordinationService)
	if err != nil {
		return errors.New("project create server is unavailable")
	}
	projectServer, err := server.NewProjectHTTPServer(userVerifier, coordinationService, projectCreator)
	if err != nil {
		return errors.New("project HTTP server is unavailable")
	}
	myProjectsServer, err := server.NewMyProjectsHTTPServer(userVerifier, coordinationService)
	if err != nil {
		return errors.New("project selector HTTP server is unavailable")
	}
	var dockerProber *dockertarget.CredentialDirectory
	var snapshotArchives *dockertarget.FoundationSnapshotArchiveDirectory
	if config.snapshotDirectory != "" {
		snapshotArchives, err = dockertarget.NewFoundationSnapshotArchiveDirectory(config.snapshotDirectory)
		if err != nil {
			return errors.New("Workspace snapshot directory is invalid")
		}
	}
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
		var keyring *targetcredential.Keyring
		if config.targetCredentialKey != "" {
			if keyring, err = targetcredential.Load(config.targetCredentialKey); err != nil {
				return errors.New("deployment target credential key is invalid")
			}
		}
		kubernetesProber.UseSealedCredentials(coordinationService, keyring)
	}
	credentialDirectories := make([]string, 0, 2)
	if config.dockerCredentials != "" {
		credentialDirectories = append(credentialDirectories, config.dockerCredentials)
	}
	if config.kubernetesCredentials != "" {
		credentialDirectories = append(credentialDirectories, config.kubernetesCredentials)
	}
	var sandboxCredentials *opensandbox.CredentialDirectory
	var foundationRuntime *internalmanagedagent.FoundationRuntime
	if len(credentialDirectories) != 0 {
		sandboxCredentials, err = opensandbox.NewCredentialDirectory(credentialDirectories...)
		if err != nil {
			return errors.New("OpenSandbox credential directory is invalid")
		}
		foundationController, controllerErr := foundationcontroller.New(coordinationService, dockerProber, kubernetesProber, sandboxCredentials, snapshotArchives)
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
	if config.providerCredentials != "" {
		foundationRuntime, err = internalmanagedagent.NewFoundationRuntimeWithCapabilities(sandboxCredentials, config.providerCredentials, config.capabilityMaterialization, coordinationService)
		if err != nil {
			return errors.New("Foundation Runtime configuration is invalid")
		}
	}
	if config.workerClientCert != "" || foundationRuntime != nil {
		runtimeCoordinator, err = internalmanagedagent.NewDurableRuntimeExecutionCoordinator(internalmanagedagent.DurableRuntimeExecutionConfig{
			Store: coordinationService, Supervisor: workerSupervisor, FoundationRuntime: foundationRuntime, WorkerClientCertificate: workerClientCertificate, WorkerRootCAs: workerCAs, Clock: time.Now,
			FencingLeaseID: config.admissionLeaseID, FencingGeneration: config.admissionGeneration, FencingToken: config.admissionToken,
			WorkspaceDirectory: config.workspaceDirectory, MaxDuration: productionRuntimeMaxDuration,
		})
		if err != nil {
			return errors.New("managed agent Runtime coordinator is unavailable")
		}
	}
	var sshProber *sshtarget.CredentialDirectory
	if config.sshCredentials != "" {
		sshProber, err = sshtarget.NewCredentialDirectory(config.sshCredentials)
		if err != nil {
			return errors.New("SSH target credential directory is invalid")
		}
	}
	adminDeploymentTargetServer, err := server.NewAdminDeploymentTargetHTTPServer(adminVerifier, adminCoordinationService, dockerProber, kubernetesProber, sshProber)
	if err != nil {
		return errors.New("admin deployment target HTTP server is unavailable")
	}
	adminEnvironmentLeaseServer, err := server.NewAdminEnvironmentLeaseHTTPServer(adminVerifier, adminCoordinationService, dockerProber, kubernetesProber, sshProber, dockertarget.WorkerTrust{ClientCertificate: workerClientCertificate, RootCAs: workerCAs})
	if err != nil {
		return errors.New("admin environment lease HTTP server is unavailable")
	}
	adminEnvironmentProfileServer, err := server.NewAdminEnvironmentProfileHTTPServer(adminVerifier, adminCoordinationService)
	if err != nil {
		return errors.New("admin environment profile HTTP server is unavailable")
	}
	adminWorkerReleaseServer, err := server.NewAdminWorkerReleaseHTTPServer(adminVerifier, adminCoordinationService)
	if err != nil {
		return errors.New("admin worker release HTTP server is unavailable")
	}
	projectLeaseQuotaServer, err := server.NewProjectLeaseQuotaHTTPServer(userVerifier, coordinationService)
	if err != nil {
		return errors.New("project lease quota HTTP server is unavailable")
	}
	adminProjectLeaseQuotaServer, err := server.NewProjectLeaseQuotaHTTPServer(adminVerifier, adminCoordinationService)
	if err != nil {
		return errors.New("admin project lease quota HTTP server is unavailable")
	}
	networkPolicyServer, err := server.NewNetworkPolicyHTTPServer(adminVerifier, adminCoordinationService)
	if err != nil {
		return errors.New("network policy HTTP server is unavailable")
	}
	capabilityServer, err := server.NewCapabilityHTTPServer(adminVerifier, adminCoordinationService)
	if err != nil {
		return errors.New("capability HTTP server is unavailable")
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
	remoteWorkerEnrollmentServer, err := server.NewRemoteWorkerEnrollmentHTTPServer(userVerifier, coordinationService, remoteWorkerCertificateAuthority, snapshotArchives)
	if err != nil {
		return errors.New("RemoteWorker enrollment HTTP server is unavailable")
	}
	adminRemoteWorkerEnrollmentServer, err := server.NewRemoteWorkerEnrollmentHTTPServer(adminVerifier, adminCoordinationService, remoteWorkerCertificateAuthority, snapshotArchives)
	if err != nil {
		return errors.New("admin RemoteWorker enrollment HTTP server is unavailable")
	}
	storagePolicyServer, err := server.NewStoragePolicyHTTPServer(adminVerifier, adminCoordinationService)
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
	adminFoundationServer, err := server.NewFoundationHTTPServer(adminVerifier, adminCoordinationService, sandboxCredentials, grantCodec)
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
	adminSessionServer, err := server.NewManagedAgentSessionHTTPServer(adminVerifier, coordinationService)
	if err != nil {
		return errors.New("admin managed agent session HTTP server is unavailable")
	}
	adminEventsServer, err := server.NewManagedAgentEventsHTTPServer(adminVerifier, coordinationService)
	if err != nil {
		return errors.New("admin managed agent events HTTP server is unavailable")
	}
	var executionServer, adminExecutionServer *server.ManagedAgentExecutionHTTPServer
	var userEnvironmentServer *server.UserEnvironmentHTTPServer
	if runtimeCoordinator != nil {
		executionServer, err = server.NewManagedAgentExecutionHTTPServer(userVerifier, coordinationService, runtimeCoordinator)
		if err != nil {
			return errors.New("managed agent execution HTTP server is unavailable")
		}
		adminExecutionServer, err = server.NewManagedAgentExecutionHTTPServer(adminVerifier, coordinationService, runtimeCoordinator)
		if err != nil {
			return errors.New("admin managed agent execution HTTP server is unavailable")
		}
		leaseServer, leaseErr := server.NewManagedHostEnvironmentLeaseHTTPServer(userVerifier, coordinationService, dockerProber, kubernetesProber, sshProber, dockertarget.WorkerTrust{ClientCertificate: workerClientCertificate, RootCAs: workerCAs})
		if leaseErr != nil {
			return errors.New("managed host environment lease HTTP server is unavailable")
		}
		userEnvironmentServer, err = server.NewUserEnvironmentHTTPServer(userVerifier, coordinationService, leaseServer)
		if err != nil {
			return errors.New("user environment HTTP server is unavailable")
		}
	}
	mux := http.NewServeMux()
	mux.Handle(server.IdentityTenantTokenAuthorizationRoute, identityAuthorizationServer)
	mux.Handle(server.IdentityPrincipalTokenAuthorizationRoute, principalAuthorizationServer)
	mux.Handle("/v1/admin/", server.AdminDeniedWriteHandler(adminVerifier, adminCoordinationService, http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if serviceAccountServer.HandlesPath(request.URL.Path) {
			serviceAccountServer.ServeHTTP(writer, request)
			return
		}
		if adminManagementServer.HandlesPath(request.URL.Path) {
			adminManagementServer.ServeHTTP(writer, request)
			return
		}
		if server.HandlesCapabilityAdminPath(request.URL.Path) {
			capabilityServer.ServeHTTP(writer, request)
			return
		}
		if runtimeRequest, ok := server.AdminManagedAgentRuntimeRequest(request); ok {
			if server.HandlesManagedAgentExecutionPath(runtimeRequest.URL.Path) {
				if adminExecutionServer == nil {
					http.NotFound(writer, request)
					return
				}
				adminExecutionServer.ServeHTTP(writer, runtimeRequest)
				return
			}
			if server.HandlesManagedAgentEventsPath(runtimeRequest.URL.Path) {
				adminEventsServer.ServeHTTP(writer, runtimeRequest)
				return
			}
			adminSessionServer.ServeHTTP(writer, runtimeRequest)
			return
		}
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
	mux.Handle("/v1/remote-worker-bootstrap/", adminRemoteWorkerEnrollmentServer)
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
	mux.Handle(server.PlatformTenantRoute, tenantServer)
	mux.Handle(server.MyProjectsRoute, myProjectsServer)
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
			if userEnvironmentServer == nil {
				http.NotFound(writer, request)
				return
			}
			userEnvironmentServer.ServeHTTP(writer, request)
			return
		}
		if server.HandlesPublishedEnvironmentProfilePath(request.URL.Path) {
			publishedEnvironmentProfileServer.ServeHTTP(writer, request)
			return
		}
		if server.HandlesRBACPath(request.URL.Path) {
			rbacServer.ServeHTTP(writer, request)
			return
		}
		if server.HandlesManagedAgentExecutionPath(request.URL.Path) {
			if executionServer == nil {
				http.NotFound(writer, request)
				return
			}
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
		if !authentication.ready() {
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
	defer httpServer.Close()
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
	refreshTicker := time.NewTicker(productionIdentityRefreshInterval)
	defer refreshTicker.Stop()
	for {
		select {
		case <-ctx.Done():
			shutdownContext, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			return httpServer.Shutdown(shutdownContext)
		case <-hup:
		case <-refreshTicker.C:
		case err := <-errorChannel:
			if errors.Is(err, http.ErrServerClosed) {
				return nil
			}
			return errors.New("HTTP server stopped")
		}
		if err := authentication.refresh(ctx); err != nil {
			if errors.Is(err, identitytrust.ErrTransportUnavailable) {
				logger.Warn("identity trust refresh unavailable; current authority remains valid")
				continue
			}
			return errors.New("identity trust refresh rejected")
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
	providerCredentials := set.String("provider-credentials-directory", "", "tenant Provider credential directory for Foundation Runtime")
	capabilityMaterialization := set.String("capability-materialization-directory", "", "operator-owned MCP capability materialization directory")
	dockerCredentials := set.String("docker-credentials-directory", "", "deployment-owned Docker mTLS credential directory")
	snapshotDirectory := set.String("snapshot-directory", "", "persistent portable Workspace snapshot directory")
	kubernetesCredentials := set.String("kubernetes-credentials-directory", "", "deployment-owned Kubernetes ServiceAccount credential directory")
	sshCredentials := set.String("ssh-credentials-directory", "", "deployment-owned SSH credential directory")
	accessGrantKey := set.String("access-grant-key-file", "", "shared 32-64 byte Sandbox access Grant key file")
	targetCredentialKey := set.String("target-credential-key-file", "", "owner-only 32 byte key that seals kubeconfig target credentials")
	remoteWorkerCACert := set.String("remote-worker-ca-cert", "", "RemoteWorker enrollment CA certificate path")
	remoteWorkerCAKey := set.String("remote-worker-ca-key", "", "RemoteWorker enrollment CA private key path")
	remoteWorkerTrustDomain := set.String("remote-worker-trust-domain", "", "RemoteWorker SPIFFE trust domain")
	admissionLeaseID := set.String("admission-lease-id", "", "authoritative Runtime lease id")
	admissionGeneration := set.Uint64("admission-generation", 0, "authoritative Runtime fencing generation")
	maxConcurrentRequests := set.Int("max-concurrent-requests", defaultProductionMaxConcurrentRequests, "maximum concurrent requests per ordinary or execution-start API pool")
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
	fill(providerCredentials, productionProviderCredentialsEnvironment)
	fill(capabilityMaterialization, productionCapabilityMaterializationEnvironment)
	fill(dockerCredentials, productionDockerCredentialsEnvironment)
	fill(snapshotDirectory, productionSnapshotDirectoryEnvironment)
	fill(kubernetesCredentials, productionKubernetesCredentialsEnvironment)
	fill(sshCredentials, productionSSHCredentialsEnvironment)
	fill(accessGrantKey, productionAccessGrantKeyEnvironment)
	fill(targetCredentialKey, productionTargetCredentialKeyEnvironment)
	fill(remoteWorkerCACert, productionRemoteWorkerCACertEnvironment)
	fill(remoteWorkerCAKey, productionRemoteWorkerCAKeyEnvironment)
	fill(remoteWorkerTrustDomain, productionRemoteWorkerTrustDomainEnvironment)
	fill(admissionLeaseID, productionAdmissionLeaseEnvironment)
	if strings.TrimSpace(*providerCredentials) != *providerCredentials || strings.TrimSpace(*capabilityMaterialization) != *capabilityMaterialization || strings.TrimSpace(*dockerCredentials) != *dockerCredentials || strings.TrimSpace(*snapshotDirectory) != *snapshotDirectory || strings.TrimSpace(*kubernetesCredentials) != *kubernetesCredentials || strings.TrimSpace(*sshCredentials) != *sshCredentials || strings.TrimSpace(*accessGrantKey) != *accessGrantKey || strings.TrimSpace(*targetCredentialKey) != *targetCredentialKey || strings.TrimSpace(*remoteWorkerCACert) != *remoteWorkerCACert || strings.TrimSpace(*remoteWorkerCAKey) != *remoteWorkerCAKey || strings.TrimSpace(*remoteWorkerTrustDomain) != *remoteWorkerTrustDomain {
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
	required := []string{*database, *authPath, *tlsCert, *tlsKey, *accessGrantKey}
	for _, value := range required {
		if value == "" || strings.TrimSpace(value) != value {
			return productionConfig{}, errors.New("database, authentication, TLS, and access Grant configuration are required")
		}
	}
	staticWorker := *workerEndpoint != "" || *workerSPIFFE != "" || *admissionLeaseID != "" || *admissionGeneration != 0
	legacyRuntime := staticWorker || *workerClientCert != "" || *workerClientKey != "" || *workerCA != "" || admissionToken != ""
	managedAgentRuntime := legacyRuntime || *providerCredentials != "" || *workspaceDirectory != ""
	if legacyRuntime && (*workerClientCert == "" || *workerClientKey == "" || *workerCA == "" || *workspaceDirectory == "" || admissionToken == "") ||
		managedAgentRuntime && (*workspaceDirectory == "" || !legacyRuntime && *providerCredentials == "") || *providerCredentials != "" && *dockerCredentials == "" && *kubernetesCredentials == "" || *capabilityMaterialization != "" && *providerCredentials == "" || *snapshotDirectory != "" && *dockerCredentials == "" && *kubernetesCredentials == "" ||
		managedAgentRuntime && *kubernetesCredentials != "" && *snapshotDirectory == "" || *targetCredentialKey != "" && *kubernetesCredentials == "" ||
		staticWorker && (*workerEndpoint == "" || *workerSPIFFE == "" || *admissionLeaseID == "" || *admissionGeneration == 0) || len(admissionToken) > 1<<20 {
		return productionConfig{}, errors.New("database, authentication, TLS, Worker Runtime, and admission configuration are required")
	}
	remoteWorkerAuthorityConfigured := *remoteWorkerCACert != "" || *remoteWorkerCAKey != "" || *remoteWorkerTrustDomain != ""
	if remoteWorkerAuthorityConfigured && (*remoteWorkerCACert == "" || *remoteWorkerCAKey == "" || *remoteWorkerTrustDomain == "") {
		return productionConfig{}, errors.New("RemoteWorker certificate authority configuration must be complete")
	}
	return productionConfig{
		listen: *listen, database: *database, authPath: *authPath, tlsCert: *tlsCert, tlsKey: *tlsKey,
		workerEndpoint: *workerEndpoint, workerSPIFFE: *workerSPIFFE, workerClientCert: *workerClientCert, workerClientKey: *workerClientKey, workerCA: *workerCA,
		workspaceDirectory: *workspaceDirectory, providerCredentials: *providerCredentials, capabilityMaterialization: *capabilityMaterialization, dockerCredentials: *dockerCredentials, snapshotDirectory: *snapshotDirectory, kubernetesCredentials: *kubernetesCredentials, sshCredentials: *sshCredentials, accessGrantKey: *accessGrantKey, targetCredentialKey: *targetCredentialKey, remoteWorkerCACert: *remoteWorkerCACert, remoteWorkerCAKey: *remoteWorkerCAKey, remoteWorkerTrustDomain: *remoteWorkerTrustDomain, admissionLeaseID: *admissionLeaseID, admissionGeneration: *admissionGeneration, admissionToken: []byte(admissionToken), maxConcurrentRequests: *maxConcurrentRequests,
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
