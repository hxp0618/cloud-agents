package server

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	api "github.com/hxp0618/cloud-agents/sdk/go/gen/openapi/v1alpha1"
	platform "github.com/hxp0618/cloud-agents/sdk/go/gen/platform/v1alpha1"
	"github.com/hxp0618/cloud-agents/services/control-plane/internal/accessgateway"
	"github.com/hxp0618/cloud-agents/services/control-plane/internal/accessgrant"
	"github.com/hxp0618/cloud-agents/services/control-plane/internal/dockertarget"
	"github.com/hxp0618/cloud-agents/services/control-plane/internal/opensandbox"
	internalremoteworker "github.com/hxp0618/cloud-agents/services/control-plane/internal/remoteworker"
	"github.com/hxp0618/cloud-agents/services/control-plane/internal/store/postgres"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/crypto/ssh"
)

// Real generated Admin/bootstrap/node clients, verified mTLS identity,
// PostgreSQL RLS/functions, outbound commands and durable Operation/Audit acceptance.
func TestRemoteWorkerEnrollmentPostgres(t *testing.T) {
	runtimeURL := os.Getenv("CLOUD_AGENTS_FOUNDATION_PROFILE_RUNTIME_DATABASE_URL")
	ownerURL := os.Getenv("CLOUD_AGENTS_FOUNDATION_PROFILE_OWNER_DATABASE_URL")
	if runtimeURL == "" || ownerURL == "" {
		t.Skip("isolated RemoteWorker enrollment PostgreSQL environment not configured")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Second)
	defer cancel()
	runtimePool, err := pgxpool.New(ctx, runtimeURL)
	if err != nil {
		t.Fatal("runtime pool unavailable")
	}
	defer runtimePool.Close()
	ownerConfig, err := pgxpool.ParseConfig(ownerURL)
	if err != nil {
		t.Fatal("owner pool configuration invalid")
	}
	ownerConfig.AfterConnect = func(ctx context.Context, connection *pgx.Conn) error {
		_, err := connection.Exec(ctx, "SET ROLE cloud_agents_migration_owner")
		return err
	}
	owner, err := pgxpool.NewWithConfig(ctx, ownerConfig)
	if err != nil {
		t.Fatal("owner pool unavailable")
	}
	defer owner.Close()

	verifier, tokens := foundationVerifierAndScopedTokens(t,
		"projects.act projects.get targets.act targets.get targets.list audit.list operations.list remote-worker-enrollments.act remote-worker-enrollments.create remote-worker-enrollments.get remote-worker-enrollments.list profiles.act profiles.create profiles.get profiles.list sandboxes.act sandboxes.get network-policies.update",
		"projects.act projects.get environment-profiles.list environments.create sandboxes.update",
		"projects.act remote-worker-bootstrap.act",
	)
	store, err := postgres.NewDurableCoordinationService(runtimePool)
	if err != nil {
		t.Fatal(err)
	}
	authority, err := internalremoteworker.NewEphemeralCertificateAuthority("remote-worker.test")
	if err != nil {
		t.Fatal(err)
	}
	remoteWorkerHandler, err := NewRemoteWorkerEnrollmentHTTPServer(verifier, store, authority)
	if err != nil {
		t.Fatal(err)
	}
	targetHandler, err := NewAdminDeploymentTargetHTTPServer(verifier, store, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	grantCodec, err := accessgrant.New([]byte("0123456789abcdef0123456789abcdef"))
	if err != nil {
		t.Fatal(err)
	}
	foundationHandler, err := NewFoundationHTTPServer(verifier, store, nil, grantCodec)
	if err != nil {
		t.Fatal(err)
	}
	networkHandler, err := NewNetworkPolicyHTTPServer(verifier, store)
	if err != nil {
		t.Fatal(err)
	}
	routes := http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if HandlesAdminDeploymentTargetPath(request.URL.Path) {
			targetHandler.ServeHTTP(writer, request)
			return
		}
		if HandlesNetworkPolicyPath(request.URL.Path) {
			networkHandler.ServeHTTP(writer, request)
			return
		}
		if HandlesFoundationPath(request.URL.Path) {
			foundationHandler.ServeHTTP(writer, request)
			return
		}
		remoteWorkerHandler.ServeHTTP(writer, request)
	})
	handler := AdminDeniedWriteHandler(verifier, store, routes)
	clientCAs, err := authority.ClientCAPool()
	if err != nil {
		t.Fatal(err)
	}
	httpServer := httptest.NewUnstartedServer(handler)
	httpServer.TLS = &tls.Config{MinVersion: tls.VersionTLS12, ClientAuth: tls.VerifyClientCertIfGiven, ClientCAs: clientCAs}
	httpServer.StartTLS()
	defer httpServer.Close()
	admin, _ := api.NewHTTPClientWithClient(httpServer.URL, tokens[0], httpServer.Client())
	user, _ := api.NewHTTPClientWithClient(httpServer.URL, tokens[1], httpServer.Client())
	bootstrap, _ := api.NewHTTPClientWithClient(httpServer.URL, tokens[2], httpServer.Client())
	create := platform.RemoteWorkerEnrollmentCreateRequest{
		EnrollmentID: "enrollment-remote-1", WorkerID: "worker-customer-1",
		WorkerName: "customer-node-1", TTLSeconds: 600,
	}
	created, err := admin.CreateAdminRemoteWorkerEnrollment(ctx, "tenant", "project", "request-enrollment-create", "remote-enroll-create-key-1", create)
	expectedTargetID := internalremoteworker.TargetID(internalremoteworker.Scope{TenantID: "tenant", ProjectID: "project"}, create.EnrollmentID)
	if err != nil || created.Value.Spec.TargetID != expectedTargetID || created.Value.Spec.State != internalremoteworker.StatePending || created.Value.Metadata.ResourceVersion != "1" {
		t.Fatalf("create enrollment: value=%+v err=%v", created.Value, err)
	}
	target, err := admin.GetAdminDeploymentTarget(ctx, "tenant", "project", expectedTargetID, "request-remote-target-get-created")
	if err != nil || target.Value.Spec.TargetKind != "remote-worker" || target.Value.Spec.Endpoint != "remote-worker://"+create.EnrollmentID || target.Value.Spec.CredentialRef != create.EnrollmentID || target.Value.Spec.ObservedPhase != "unprobed" {
		t.Fatalf("projected target after enrollment: value=%+v err=%v", target.Value, err)
	}
	targets, err := admin.ListAdminDeploymentTargets(ctx, "tenant", "project", "request-remote-target-list", 50, "")
	foundTarget := false
	for _, listed := range targets.Value.DeploymentTargets {
		foundTarget = foundTarget || listed.Metadata.UID == expectedTargetID && listed.Spec.TargetKind == "remote-worker"
	}
	if err != nil || !foundTarget {
		t.Fatalf("projected target list: value=%+v err=%v", targets.Value, err)
	}
	if _, err := user.GetAdminDeploymentTarget(ctx, "tenant", "project", expectedTargetID, "request-remote-target-user-get"); clientStatus(err) != http.StatusForbidden {
		t.Fatalf("ordinary user projected target status=%d err=%v", clientStatus(err), err)
	}
	if _, err := admin.ProbeAdminDeploymentTarget(ctx, "tenant", "project", expectedTargetID, "request-remote-target-probe", "remote-target-probe-key-1", platform.DeploymentTargetProbeRequest{ExpectedGeneration: 1}); clientStatus(err) != http.StatusConflict {
		t.Fatalf("generic projected target probe status=%d err=%v", clientStatus(err), err)
	}
	if _, err := admin.PreviewAdminDeploymentTargetScheduling(ctx, "tenant", "project", expectedTargetID, "request-remote-target-scheduling-preview"); clientStatus(err) != http.StatusConflict {
		t.Fatalf("generic projected target scheduling status=%d err=%v", clientStatus(err), err)
	}
	if _, err := admin.PreviewAdminDeploymentTargetCleanup(ctx, "tenant", "project", expectedTargetID, "request-remote-target-cleanup-preview"); clientStatus(err) != http.StatusConflict {
		t.Fatalf("generic projected target cleanup status=%d err=%v", clientStatus(err), err)
	}
	replayed, err := admin.CreateAdminRemoteWorkerEnrollment(ctx, "tenant", "project", "request-enrollment-replay", "remote-enroll-create-key-1", create)
	if err != nil || replayed.Value.Metadata.ResourceVersion != "1" {
		t.Fatalf("create replay: value=%+v err=%v", replayed.Value, err)
	}
	if _, err := user.CreateAdminRemoteWorkerEnrollment(ctx, "tenant", "project", "request-enrollment-user", "remote-enroll-user-key-1", create); clientStatus(err) != http.StatusForbidden {
		t.Fatalf("ordinary user Admin status=%d err=%v", clientStatus(err), err)
	}
	if _, err := admin.ClaimRemoteWorkerEnrollmentSecret(ctx, "tenant", "project", create.EnrollmentID, "request-enrollment-admin-claim", "remote-enroll-admin-key-1", platform.RemoteWorkerEnrollmentSecretClaimRequest{ExpectedResourceVersion: "1", ConfirmedEnrollmentID: create.EnrollmentID}); clientStatus(err) != http.StatusForbidden {
		t.Fatalf("Admin secret claim status=%d err=%v", clientStatus(err), err)
	}
	secretResult, err := bootstrap.ClaimRemoteWorkerEnrollmentSecret(ctx, "tenant", "project", create.EnrollmentID, "request-enrollment-claim", "remote-enroll-claim-key-1", platform.RemoteWorkerEnrollmentSecretClaimRequest{ExpectedResourceVersion: "1", ConfirmedEnrollmentID: create.EnrollmentID})
	if err != nil || !strings.HasPrefix(secretResult.Value.EnrollmentSecret, "carw1_") {
		t.Fatalf("bootstrap secret claim: value=%+v err=%v", secretResult.Value, err)
	}
	secret := secretResult.Value.EnrollmentSecret
	if _, err := bootstrap.ClaimRemoteWorkerEnrollmentSecret(ctx, "tenant", "project", create.EnrollmentID, "request-enrollment-claim-replay", "remote-enroll-claim-key-1", platform.RemoteWorkerEnrollmentSecretClaimRequest{ExpectedResourceVersion: "1", ConfirmedEnrollmentID: create.EnrollmentID}); clientStatus(err) != http.StatusConflict {
		t.Fatalf("secret replay status=%d err=%v", clientStatus(err), err)
	}
	if _, err := bootstrap.ListAdminRemoteWorkerEnrollments(ctx, "tenant", "project", "request-bootstrap-list", 50, ""); clientStatus(err) != http.StatusUnauthorized {
		t.Fatalf("bootstrap Admin list status=%d err=%v", clientStatus(err), err)
	}
	list, err := admin.ListAdminRemoteWorkerEnrollments(ctx, "tenant", "project", "request-enrollment-list", 50, "")
	if err != nil || len(list.Value.RemoteWorkerEnrollments) != 1 || list.Value.RemoteWorkerEnrollments[0].Spec.State != internalremoteworker.StateSecretIssued {
		t.Fatalf("Admin enrollment list: value=%+v err=%v", list.Value, err)
	}
	current, err := admin.GetAdminRemoteWorkerEnrollment(ctx, "tenant", "project", create.EnrollmentID, "request-enrollment-get")
	if err != nil || current.Value.Spec.State != internalremoteworker.StateSecretIssued || current.Value.Metadata.ResourceVersion != "2" {
		t.Fatalf("Admin enrollment get: value=%+v err=%v", current.Value, err)
	}
	assertRemoteWorkerAdminRedaction(t, ctx, httpServer, tokens[0], create.EnrollmentID, secret)
	audit, err := admin.ListAdminRemoteWorkerEnrollmentAuditEvents(ctx, "tenant", "project", create.EnrollmentID, "request-enrollment-audit", 50, "")
	if err != nil || len(audit.Value.Events) != 2 {
		t.Fatalf("Admin enrollment audit: value=%+v err=%v", audit.Value, err)
	}

	csrPEM, privateKey := remoteWorkerCertificateRequest(t, create.EnrollmentID)
	certificateClient, err := api.NewRemoteWorkerBootstrapHTTPClientWithClient(httpServer.URL, secret, httpServer.Client())
	if err != nil {
		t.Fatal(err)
	}
	certificateRequest := platform.RemoteWorkerCertificateIssueRequest{ExpectedResourceVersion: "2", ConfirmedEnrollmentID: create.EnrollmentID, IncarnationID: "incarnation-remote-1", CertificateSigningRequestPEM: csrPEM}
	issued, err := certificateClient.IssueRemoteWorkerCertificate(ctx, "tenant", "project", create.EnrollmentID, "request-certificate-issue", "remote-cert-issue-key-1", certificateRequest)
	if err != nil || issued.Value.WorkerID != create.WorkerID || issued.Value.SPIFFEID != "spiffe://remote-worker.test/remote-worker/tenant/project/enrollment-remote-1/incarnation-remote-1" {
		t.Fatalf("issue certificate: value=%+v err=%v", issued.Value, err)
	}
	if _, err := tls.X509KeyPair([]byte(issued.Value.CertificateChainPEM), privateKey); err != nil {
		t.Fatalf("issued certificate/private key mismatch: %v", err)
	}
	replayedCertificate, err := certificateClient.IssueRemoteWorkerCertificate(ctx, "tenant", "project", create.EnrollmentID, "request-certificate-replay", "remote-cert-issue-key-1", certificateRequest)
	if err != nil || replayedCertificate.Value.CertificateChainPEM != issued.Value.CertificateChainPEM {
		t.Fatalf("certificate replay: value=%+v err=%v", replayedCertificate.Value, err)
	}
	wrongSecretClient, _ := api.NewRemoteWorkerBootstrapHTTPClientWithClient(httpServer.URL, "carw1_"+strings.Repeat("A", 43), httpServer.Client())
	if _, err := wrongSecretClient.IssueRemoteWorkerCertificate(ctx, "tenant", "project", create.EnrollmentID, "request-certificate-wrong-secret", "remote-cert-wrong-key-1", certificateRequest); clientStatus(err) != http.StatusUnauthorized {
		t.Fatalf("wrong enrollment secret status=%d err=%v", clientStatus(err), err)
	}
	current, err = admin.GetAdminRemoteWorkerEnrollment(ctx, "tenant", "project", create.EnrollmentID, "request-enrollment-get-issued")
	if err != nil || current.Value.Spec.State != internalremoteworker.StateEnrolled || current.Value.Metadata.ResourceVersion != "3" || current.Value.Spec.CertificateSHA256 != issued.Value.CertificateSHA256 || current.Value.Spec.CertificateExpiresAt != issued.Value.ExpiresAt || current.Value.Spec.IncarnationID != certificateRequest.IncarnationID || current.Value.Spec.CertificateState != "active" {
		t.Fatalf("Admin issued enrollment metadata: value=%+v err=%v", current.Value, err)
	}
	assertRemoteWorkerAdminRedaction(t, ctx, httpServer, tokens[0], create.EnrollmentID, secret)

	oldNodeHTTP := remoteWorkerMTLSHTTPClient(t, httpServer, issued.Value.CertificateChainPEM, privateKey)
	oldNode, err := api.NewRemoteWorkerMTLSHTTPClientWithClient(httpServer.URL, oldNodeHTTP)
	if err != nil {
		t.Fatal(err)
	}
	heartbeat := platform.RemoteWorkerHeartbeatRequest{
		IncarnationID: certificateRequest.IncarnationID, ObservedGeneration: 1, ObservedState: "active",
		WorkerVersion: "v0.1.0", OS: "linux", Architecture: "arm64", KernelVersion: "6.12.1",
		Capabilities: []string{"docker", "exec", "files", "network-dns-nft", "preview", "pty", "ssh", "workspace-volume"},
		Capacity:     platform.RemoteWorkerCapacity{CPUMillis: 4000, MemoryBytes: 8 << 30, DiskBytes: 40 << 30},
	}
	accepted, err := oldNode.HeartbeatRemoteWorker(ctx, "tenant", "project", create.EnrollmentID, "request-heartbeat-initial", heartbeat)
	if err != nil || accepted.Value.HealthState != "online" || accepted.Value.Generation != 1 || accepted.Value.ReconcileRequired {
		t.Fatalf("initial heartbeat: value=%+v err=%v", accepted.Value, err)
	}
	current, err = admin.GetAdminRemoteWorkerEnrollment(ctx, "tenant", "project", create.EnrollmentID, "request-enrollment-get-heartbeat")
	if err != nil || current.Value.Spec.Node == nil || current.Value.Spec.Node.HealthState != "online" || current.Value.Spec.Node.Capacity.CPUMillis != 4000 {
		t.Fatalf("Admin heartbeat status: value=%+v err=%v", current.Value, err)
	}
	target, err = admin.GetAdminDeploymentTarget(ctx, "tenant", "project", expectedTargetID, "request-remote-target-get-ready")
	if err != nil || target.Value.Spec.ObservedPhase != "ready" || target.Value.Spec.APIVersion != "remote-worker/v1alpha1" || target.Value.Spec.EngineVersion != heartbeat.WorkerVersion || target.Value.Spec.OS != heartbeat.OS || target.Value.Spec.Architecture != heartbeat.Architecture {
		t.Fatalf("projected target after heartbeat: value=%+v err=%v", target.Value, err)
	}
	rotationCSR, rotationPrivateKey := remoteWorkerCertificateRequest(t, create.EnrollmentID)
	rotationRequest := platform.RemoteWorkerCertificateIssueRequest{ExpectedResourceVersion: "3", ConfirmedEnrollmentID: create.EnrollmentID, IncarnationID: "incarnation-remote-2", CertificateSigningRequestPEM: rotationCSR}
	rotated, err := oldNode.RotateRemoteWorkerCertificate(ctx, "tenant", "project", create.EnrollmentID, "request-certificate-rotate", "remote-cert-rotate-key-1", rotationRequest)
	if err != nil || rotated.Value.CertificateSHA256 == issued.Value.CertificateSHA256 || rotated.Value.IncarnationID != rotationRequest.IncarnationID {
		t.Fatalf("rotate certificate: value=%+v err=%v", rotated.Value, err)
	}
	if _, err := tls.X509KeyPair([]byte(rotated.Value.CertificateChainPEM), rotationPrivateKey); err != nil {
		t.Fatalf("rotated certificate/private key mismatch: %v", err)
	}
	rotationReplay, err := oldNode.RotateRemoteWorkerCertificate(ctx, "tenant", "project", create.EnrollmentID, "request-certificate-rotate-replay", "remote-cert-rotate-key-1", rotationRequest)
	if err != nil || rotationReplay.Value.CertificateChainPEM != rotated.Value.CertificateChainPEM {
		t.Fatalf("rotation replay: value=%+v err=%v", rotationReplay.Value, err)
	}
	rotationConflict := rotationRequest
	rotationConflict.IncarnationID = "incarnation-remote-conflict"
	if _, err := oldNode.RotateRemoteWorkerCertificate(ctx, "tenant", "project", create.EnrollmentID, "request-certificate-rotate-conflict", "remote-cert-rotate-key-1", rotationConflict); clientStatus(err) != http.StatusConflict {
		t.Fatalf("rotation idempotency conflict status=%d err=%v", clientStatus(err), err)
	}
	if _, err := oldNode.RotateRemoteWorkerCertificate(ctx, "tenant", "project", create.EnrollmentID, "request-certificate-old-node", "remote-cert-old-node-key-1", rotationRequest); clientStatus(err) != http.StatusUnauthorized {
		t.Fatalf("displaced certificate status=%d err=%v", clientStatus(err), err)
	}
	if _, err := oldNode.HeartbeatRemoteWorker(ctx, "tenant", "project", create.EnrollmentID, "request-heartbeat-old-node", heartbeat); clientStatus(err) != http.StatusUnauthorized {
		t.Fatalf("displaced heartbeat status=%d err=%v", clientStatus(err), err)
	}
	currentNodeHTTP := remoteWorkerMTLSHTTPClient(t, httpServer, rotated.Value.CertificateChainPEM, rotationPrivateKey)
	currentNode, err := api.NewRemoteWorkerMTLSHTTPClientWithClient(httpServer.URL, currentNodeHTTP)
	if err != nil {
		t.Fatal(err)
	}
	current, err = admin.GetAdminRemoteWorkerEnrollment(ctx, "tenant", "project", create.EnrollmentID, "request-enrollment-get-rotated")
	if err != nil || current.Value.Metadata.ResourceVersion != "4" || current.Value.Spec.CertificateState != "active" || current.Value.Spec.CertificateSHA256 != rotated.Value.CertificateSHA256 || current.Value.Spec.IncarnationID != rotationRequest.IncarnationID || current.Value.Spec.Node != nil {
		t.Fatalf("Admin rotated enrollment metadata: value=%+v err=%v", current.Value, err)
	}
	target, err = admin.GetAdminDeploymentTarget(ctx, "tenant", "project", expectedTargetID, "request-remote-target-get-rotated")
	if err != nil || target.Value.Spec.ObservedPhase != "unprobed" || target.Value.Spec.StableErrorCode != "" {
		t.Fatalf("projected target after certificate rotation: value=%+v err=%v", target.Value, err)
	}
	heartbeat.IncarnationID = rotationRequest.IncarnationID
	if _, err := currentNode.HeartbeatRemoteWorker(ctx, "tenant", "project", create.EnrollmentID, "request-heartbeat-current", heartbeat); err != nil {
		t.Fatalf("current heartbeat: %v", err)
	}
	reconnectedNode, err := api.NewRemoteWorkerMTLSHTTPClientWithClient(httpServer.URL, remoteWorkerMTLSHTTPClient(t, httpServer, rotated.Value.CertificateChainPEM, rotationPrivateKey))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := reconnectedNode.HeartbeatRemoteWorker(ctx, "tenant", "project", create.EnrollmentID, "request-heartbeat-reconnected", heartbeat); err != nil {
		t.Fatalf("reconnected heartbeat: %v", err)
	}
	selectedCreate := platform.RemoteWorkerEnrollmentCreateRequest{
		EnrollmentID: "enrollment-remote-2", WorkerID: "worker-customer-2",
		WorkerName: "customer-node-2", TTLSeconds: 600,
	}
	selectedEnrollment, err := admin.CreateAdminRemoteWorkerEnrollment(ctx, "tenant", "project", "request-enrollment-select-create", "remote-enroll-select-create-key", selectedCreate)
	if err != nil || selectedEnrollment.Value.Metadata.ResourceVersion != "1" {
		t.Fatalf("create selected enrollment: value=%+v err=%v", selectedEnrollment.Value, err)
	}
	selectedSecret, err := bootstrap.ClaimRemoteWorkerEnrollmentSecret(ctx, "tenant", "project", selectedCreate.EnrollmentID, "request-enrollment-select-claim", "remote-enroll-select-claim-key", platform.RemoteWorkerEnrollmentSecretClaimRequest{ExpectedResourceVersion: "1", ConfirmedEnrollmentID: selectedCreate.EnrollmentID})
	if err != nil {
		t.Fatalf("claim selected enrollment: %v", err)
	}
	selectedCSR, selectedPrivateKey := remoteWorkerCertificateRequest(t, selectedCreate.EnrollmentID)
	selectedBootstrap, err := api.NewRemoteWorkerBootstrapHTTPClientWithClient(httpServer.URL, selectedSecret.Value.EnrollmentSecret, httpServer.Client())
	if err != nil {
		t.Fatal(err)
	}
	selectedCertificate, err := selectedBootstrap.IssueRemoteWorkerCertificate(ctx, "tenant", "project", selectedCreate.EnrollmentID, "request-certificate-select-issue", "remote-cert-select-issue-key", platform.RemoteWorkerCertificateIssueRequest{
		ExpectedResourceVersion: "2", ConfirmedEnrollmentID: selectedCreate.EnrollmentID,
		IncarnationID: "incarnation-remote-select", CertificateSigningRequestPEM: selectedCSR,
	})
	if err != nil {
		t.Fatalf("issue selected certificate: %v", err)
	}
	selectedNode, err := api.NewRemoteWorkerMTLSHTTPClientWithClient(httpServer.URL, remoteWorkerMTLSHTTPClient(t, httpServer, selectedCertificate.Value.CertificateChainPEM, selectedPrivateKey))
	if err != nil {
		t.Fatal(err)
	}
	selectedHeartbeat := platform.RemoteWorkerHeartbeatRequest{
		IncarnationID: selectedCertificate.Value.IncarnationID, ObservedGeneration: 1, ObservedState: "active",
		WorkerVersion: "v0.1.0", OS: "linux", Architecture: "arm64", KernelVersion: "6.12.1",
		Capabilities: []string{"docker", "exec", "files", "network-dns-nft", "preview", "pty", "ssh", "workspace-volume"},
		Capacity:     platform.RemoteWorkerCapacity{CPUMillis: 8000, MemoryBytes: 16 << 30, DiskBytes: 80 << 30},
	}
	if os.Getenv("CLOUD_AGENTS_REMOTE_WORKER_ISOLATION_RUNTIME") == "gvisor" {
		selectedHeartbeat.Capabilities = []string{"docker", "exec", "files", "isolation-gvisor", "network-dns-nft", "network-internal-deny", "preview", "pty", "ssh", "workspace-volume"}
	}
	if _, err := selectedNode.HeartbeatRemoteWorker(ctx, "tenant", "project", selectedCreate.EnrollmentID, "request-heartbeat-select", selectedHeartbeat); err != nil {
		t.Fatalf("selected node heartbeat: %v", err)
	}
	runRemoteWorkerSandboxLive(t, ctx, runtimePool, owner, admin, user, selectedNode, httpServer, selectedCreate.EnrollmentID,
		selectedCertificate.Value.IncarnationID, selectedCertificate.Value.CertificateChainPEM, selectedCertificate.Value.CertificateSHA256,
		selectedPrivateKey, selectedHeartbeat, currentNode, create.EnrollmentID, heartbeat)
	drainPreview, err := admin.PreviewAdminRemoteWorkerScheduling(ctx, "tenant", "project", create.EnrollmentID, "request-remote-worker-drain-preview")
	if err != nil || drainPreview.Value.Spec.DesiredState != "drained" || drainPreview.Value.Spec.ExpectedGeneration != 1 {
		t.Fatalf("drain preview: value=%+v err=%v", drainPreview.Value, err)
	}
	drainRequest := platform.RemoteWorkerNodeSchedulingRequest{ExpectedGeneration: drainPreview.Value.Spec.ExpectedGeneration,
		ExpectedResourceVersion: drainPreview.Value.Spec.ExpectedResourceVersion, ConfirmedEnrollmentID: create.EnrollmentID,
		DesiredState: drainPreview.Value.Spec.DesiredState, ImpactDigest: drainPreview.Value.Spec.ImpactDigest}
	if _, err := user.TransitionAdminRemoteWorkerScheduling(ctx, "tenant", "project", create.EnrollmentID, "request-remote-worker-drain-user", "remote-worker-drain-user-key-1", drainRequest); clientStatus(err) != http.StatusForbidden {
		t.Fatalf("ordinary user drain status=%d err=%v", clientStatus(err), err)
	}
	drainOperation, err := admin.TransitionAdminRemoteWorkerScheduling(ctx, "tenant", "project", create.EnrollmentID, "request-remote-worker-drain", "remote-worker-drain-key-1", drainRequest)
	if err != nil || drainOperation.Value.State != "queued" || drainOperation.Value.ResourceGeneration != 2 {
		t.Fatalf("queue drain: value=%+v err=%v", drainOperation.Value, err)
	}
	delivered, err := currentNode.HeartbeatRemoteWorker(ctx, "tenant", "project", create.EnrollmentID, "request-remote-worker-drain-delivery", heartbeat)
	if err != nil || delivered.Value.Command == nil || delivered.Value.Command.Generation != 2 || delivered.Value.Command.DesiredState != "drained" {
		t.Fatalf("deliver drain: value=%+v err=%v", delivered.Value, err)
	}
	if binary := os.Getenv("CLOUD_AGENTS_REMOTE_WORKER_BINARY"); binary != "" {
		stateFile := filepath.Join(t.TempDir(), "remote-worker-state.json")
		runRemoteWorkerHeartbeatProcess(t, ctx, binary, httpServer, create.EnrollmentID, rotationRequest.IncarnationID, rotated.Value.CertificateChainPEM, rotationPrivateKey, stateFile)
		runRemoteWorkerHeartbeatProcess(t, ctx, binary, httpServer, create.EnrollmentID, rotationRequest.IncarnationID, rotated.Value.CertificateChainPEM, rotationPrivateKey, stateFile)
	} else {
		heartbeat.ObservedGeneration, heartbeat.ObservedState = 2, "drained"
		heartbeat.CommandReceipt = &platform.RemoteWorkerCommandReceipt{CommandID: delivered.Value.Command.CommandID, Generation: 2, Result: "succeeded"}
		if _, err := currentNode.HeartbeatRemoteWorker(ctx, "tenant", "project", create.EnrollmentID, "request-remote-worker-drain-receipt", heartbeat); err != nil {
			t.Fatalf("drain receipt: %v", err)
		}
	}
	heartbeat.ObservedGeneration, heartbeat.ObservedState = 2, "drained"
	heartbeat.CommandReceipt = &platform.RemoteWorkerCommandReceipt{CommandID: delivered.Value.Command.CommandID, Generation: 2, Result: "succeeded"}
	if _, err := currentNode.HeartbeatRemoteWorker(ctx, "tenant", "project", create.EnrollmentID, "request-remote-worker-drain-receipt-replay", heartbeat); err != nil {
		t.Fatalf("drain receipt replay: %v", err)
	}
	heartbeat.CommandReceipt = nil
	operations, err := admin.ListAdminRemoteWorkerOperations(ctx, "tenant", "project", create.EnrollmentID, "request-remote-worker-operations-drain", 50, "")
	if err != nil || len(operations.Value.Operations) != 1 || operations.Value.Operations[0].State != "succeeded" {
		t.Fatalf("drain operation: value=%+v err=%v", operations.Value, err)
	}

	resumePreview, err := admin.PreviewAdminRemoteWorkerScheduling(ctx, "tenant", "project", create.EnrollmentID, "request-remote-worker-resume-preview")
	if err != nil || resumePreview.Value.Spec.DesiredState != "active" || resumePreview.Value.Spec.ExpectedGeneration != 2 {
		t.Fatalf("resume preview: value=%+v err=%v", resumePreview.Value, err)
	}
	resumeRequest := platform.RemoteWorkerNodeSchedulingRequest{ExpectedGeneration: resumePreview.Value.Spec.ExpectedGeneration,
		ExpectedResourceVersion: resumePreview.Value.Spec.ExpectedResourceVersion, ConfirmedEnrollmentID: create.EnrollmentID,
		DesiredState: resumePreview.Value.Spec.DesiredState, ImpactDigest: resumePreview.Value.Spec.ImpactDigest}
	resumeOperation, err := admin.TransitionAdminRemoteWorkerScheduling(ctx, "tenant", "project", create.EnrollmentID, "request-remote-worker-resume", "remote-worker-resume-key-1", resumeRequest)
	if err != nil || resumeOperation.Value.State != "queued" || resumeOperation.Value.ResourceGeneration != 3 {
		t.Fatalf("queue resume: value=%+v err=%v", resumeOperation.Value, err)
	}
	resumeReplay, err := admin.TransitionAdminRemoteWorkerScheduling(ctx, "tenant", "project", create.EnrollmentID, "request-remote-worker-resume-replay", "remote-worker-resume-key-1", resumeRequest)
	if err != nil || resumeReplay.Value.OperationID != resumeOperation.Value.OperationID {
		t.Fatalf("resume replay: value=%+v err=%v", resumeReplay.Value, err)
	}
	resumeDelivery, err := currentNode.HeartbeatRemoteWorker(ctx, "tenant", "project", create.EnrollmentID, "request-remote-worker-resume-delivery", heartbeat)
	if err != nil || resumeDelivery.Value.Command == nil || resumeDelivery.Value.Command.Generation != 3 || resumeDelivery.Value.Command.DesiredState != "active" {
		t.Fatalf("deliver resume: value=%+v err=%v", resumeDelivery.Value, err)
	}
	wrongReceipt := heartbeat
	wrongReceipt.ObservedGeneration, wrongReceipt.ObservedState = 3, "active"
	wrongReceipt.CommandReceipt = &platform.RemoteWorkerCommandReceipt{CommandID: "command-wrong", Generation: 3, Result: "succeeded"}
	if _, err := currentNode.HeartbeatRemoteWorker(ctx, "tenant", "project", create.EnrollmentID, "request-remote-worker-wrong-receipt", wrongReceipt); clientStatus(err) != http.StatusConflict {
		t.Fatalf("wrong receipt status=%d err=%v", clientStatus(err), err)
	}
	heartbeat.ObservedGeneration, heartbeat.ObservedState = 3, "active"
	heartbeat.CommandReceipt = &platform.RemoteWorkerCommandReceipt{CommandID: resumeDelivery.Value.Command.CommandID, Generation: 3, Result: "succeeded"}
	if _, err := currentNode.HeartbeatRemoteWorker(ctx, "tenant", "project", create.EnrollmentID, "request-remote-worker-resume-receipt", heartbeat); err != nil {
		t.Fatalf("resume receipt: %v", err)
	}
	if _, err := currentNode.HeartbeatRemoteWorker(ctx, "tenant", "project", create.EnrollmentID, "request-remote-worker-resume-receipt-replay", heartbeat); err != nil {
		t.Fatalf("resume receipt replay: %v", err)
	}
	heartbeat.CommandReceipt = nil

	expiryPreview, err := admin.PreviewAdminRemoteWorkerScheduling(ctx, "tenant", "project", create.EnrollmentID, "request-remote-worker-expiry-preview")
	if err != nil || expiryPreview.Value.Spec.DesiredState != "drained" || expiryPreview.Value.Spec.ExpectedGeneration != 3 {
		t.Fatalf("expiry preview: value=%+v err=%v", expiryPreview.Value, err)
	}
	expiryRequest := platform.RemoteWorkerNodeSchedulingRequest{ExpectedGeneration: expiryPreview.Value.Spec.ExpectedGeneration,
		ExpectedResourceVersion: expiryPreview.Value.Spec.ExpectedResourceVersion, ConfirmedEnrollmentID: create.EnrollmentID,
		DesiredState: expiryPreview.Value.Spec.DesiredState, ImpactDigest: expiryPreview.Value.Spec.ImpactDigest}
	expiryOperation, err := admin.TransitionAdminRemoteWorkerScheduling(ctx, "tenant", "project", create.EnrollmentID, "request-remote-worker-expiry", "remote-worker-expiry-key-1", expiryRequest)
	if err != nil || expiryOperation.Value.State != "queued" {
		t.Fatalf("queue expiring command: value=%+v err=%v", expiryOperation.Value, err)
	}
	expiryDelivery, err := currentNode.HeartbeatRemoteWorker(ctx, "tenant", "project", create.EnrollmentID, "request-remote-worker-expiry-delivery", heartbeat)
	if err != nil || expiryDelivery.Value.Command == nil || expiryDelivery.Value.Command.Generation != 4 {
		t.Fatalf("deliver expiring command: value=%+v err=%v", expiryDelivery.Value, err)
	}
	if _, err := owner.Exec(ctx, `WITH observed AS (SELECT clock_timestamp() AS at)
		UPDATE cloud_agents.remote_worker_node_activity AS activity
		SET requested_at = observed.at - interval '31 seconds', command_deadline_at = observed.at - interval '1 second'
		FROM observed WHERE activity.tenant_id='tenant' AND activity.project_uid='project'
		AND activity.enrollment_uid=$1 AND activity.operation_uid=$2`, create.EnrollmentID, expiryOperation.Value.OperationID); err != nil {
		t.Fatal(err)
	}
	if _, err := owner.Exec(ctx, `UPDATE cloud_agents.remote_worker_enrollments SET node_command_deadline_at=clock_timestamp()-interval '1 second'
		WHERE tenant_id='tenant' AND project_uid='project' AND enrollment_uid=$1`, create.EnrollmentID); err != nil {
		t.Fatal(err)
	}
	heartbeat.CommandReceipt = &platform.RemoteWorkerCommandReceipt{CommandID: expiryDelivery.Value.Command.CommandID, Generation: 4, Result: "failed", StableErrorCode: "remote-worker-command-expired"}
	if _, err := currentNode.HeartbeatRemoteWorker(ctx, "tenant", "project", create.EnrollmentID, "request-remote-worker-expire", heartbeat); err != nil {
		t.Fatalf("expired command receipt: %v", err)
	}
	if _, err := currentNode.HeartbeatRemoteWorker(ctx, "tenant", "project", create.EnrollmentID, "request-remote-worker-expire-replay", heartbeat); err != nil {
		t.Fatalf("expired command receipt replay: %v", err)
	}
	heartbeat.CommandReceipt = nil
	operations, err = admin.ListAdminRemoteWorkerOperations(ctx, "tenant", "project", create.EnrollmentID, "request-remote-worker-operations-final", 50, "")
	var expiredOperation *platform.MaintenanceOperation
	for index := range operations.Value.Operations {
		if operations.Value.Operations[index].OperationID == expiryOperation.Value.OperationID {
			expiredOperation = &operations.Value.Operations[index]
		}
	}
	if err != nil || len(operations.Value.Operations) != 3 || expiredOperation == nil || expiredOperation.State != "failed" || expiredOperation.StableErrorCode != "remote-worker-command-expired" {
		t.Fatalf("final operations: value=%+v err=%v", operations.Value, err)
	}
	conflictingHeartbeat := heartbeat
	conflictingHeartbeat.ObservedGeneration = 5
	if _, err := currentNode.HeartbeatRemoteWorker(ctx, "tenant", "project", create.EnrollmentID, "request-heartbeat-generation-conflict", conflictingHeartbeat); clientStatus(err) != http.StatusConflict {
		t.Fatalf("heartbeat generation conflict status=%d err=%v", clientStatus(err), err)
	}
	wrongIncarnationHeartbeat := heartbeat
	wrongIncarnationHeartbeat.IncarnationID = "incarnation-remote-wrong"
	if _, err := currentNode.HeartbeatRemoteWorker(ctx, "tenant", "project", create.EnrollmentID, "request-heartbeat-wrong-incarnation", wrongIncarnationHeartbeat); clientStatus(err) != http.StatusUnauthorized {
		t.Fatalf("heartbeat incarnation mismatch status=%d err=%v", clientStatus(err), err)
	}
	if _, err := owner.Exec(ctx, `WITH observed AS (SELECT clock_timestamp() AS at)
		UPDATE cloud_agents.remote_worker_enrollments
		SET node_first_connected_at = observed.at - interval '20 seconds',
			node_last_heartbeat_at = observed.at - interval '15 seconds',
			node_heartbeat_expires_at = observed.at + interval '15 seconds'
		FROM observed
		WHERE tenant_id = 'tenant' AND project_uid = 'project' AND enrollment_uid = $1`, create.EnrollmentID); err != nil {
		t.Fatal(err)
	}
	current, err = admin.GetAdminRemoteWorkerEnrollment(ctx, "tenant", "project", create.EnrollmentID, "request-enrollment-get-degraded")
	if err != nil || current.Value.Spec.Node == nil || current.Value.Spec.Node.HealthState != "degraded" {
		t.Fatalf("Admin degraded heartbeat status: value=%+v err=%v", current.Value, err)
	}
	if _, err := owner.Exec(ctx, `WITH observed AS (SELECT clock_timestamp() AS at)
		UPDATE cloud_agents.remote_worker_enrollments
		SET node_first_connected_at = observed.at - interval '40 seconds',
			node_last_heartbeat_at = observed.at - interval '31 seconds',
			node_heartbeat_expires_at = observed.at - interval '1 second'
		FROM observed
		WHERE tenant_id = 'tenant' AND project_uid = 'project' AND enrollment_uid = $1`, create.EnrollmentID); err != nil {
		t.Fatal(err)
	}
	current, err = admin.GetAdminRemoteWorkerEnrollment(ctx, "tenant", "project", create.EnrollmentID, "request-enrollment-get-offline")
	if err != nil || current.Value.Spec.Node == nil || current.Value.Spec.Node.HealthState != "offline" {
		t.Fatalf("Admin offline heartbeat status: value=%+v err=%v", current.Value, err)
	}
	target, err = admin.GetAdminDeploymentTarget(ctx, "tenant", "project", expectedTargetID, "request-remote-target-get-offline")
	if err != nil || target.Value.Spec.ObservedPhase != "unavailable" || target.Value.Spec.StableErrorCode != "remote-worker-offline" {
		t.Fatalf("projected target after heartbeat expiry: value=%+v err=%v", target.Value, err)
	}
	if _, err := currentNode.HeartbeatRemoteWorker(ctx, "tenant", "project", create.EnrollmentID, "request-heartbeat-recovered", heartbeat); err != nil {
		t.Fatalf("recovered heartbeat: %v", err)
	}
	target, err = admin.GetAdminDeploymentTarget(ctx, "tenant", "project", expectedTargetID, "request-remote-target-get-recovered")
	if err != nil || target.Value.Spec.ObservedPhase != "ready" || target.Value.Spec.StableErrorCode != "" {
		t.Fatalf("projected target after reconnect: value=%+v err=%v", target.Value, err)
	}
	certificateRevoke := platform.RemoteWorkerEnrollmentRevokeRequest{ExpectedResourceVersion: "4", ConfirmedEnrollmentID: create.EnrollmentID}
	if _, err := user.RevokeAdminRemoteWorkerEnrollment(ctx, "tenant", "project", create.EnrollmentID, "request-certificate-revoke-user", "remote-cert-revoke-user-key-1", certificateRevoke); clientStatus(err) != http.StatusForbidden {
		t.Fatalf("ordinary user certificate revoke status=%d err=%v", clientStatus(err), err)
	}
	certificateRevoked, err := admin.RevokeAdminRemoteWorkerEnrollment(ctx, "tenant", "project", create.EnrollmentID, "request-certificate-revoke", "remote-cert-revoke-key-1", certificateRevoke)
	if err != nil || certificateRevoked.Value.Spec.State != internalremoteworker.StateEnrolled || certificateRevoked.Value.Spec.CertificateState != "revoked" || certificateRevoked.Value.Spec.CertificateRevokedAt == "" || certificateRevoked.Value.Metadata.ResourceVersion != "5" {
		t.Fatalf("revoke certificate: value=%+v err=%v", certificateRevoked.Value, err)
	}
	target, err = admin.GetAdminDeploymentTarget(ctx, "tenant", "project", expectedTargetID, "request-remote-target-get-revoked")
	if err != nil || target.Value.Spec.ObservedPhase != "unavailable" || target.Value.Spec.StableErrorCode != "remote-worker-unavailable" {
		t.Fatalf("projected target after certificate revoke: value=%+v err=%v", target.Value, err)
	}
	certificateRevoked, err = admin.RevokeAdminRemoteWorkerEnrollment(ctx, "tenant", "project", create.EnrollmentID, "request-certificate-revoke-replay", "remote-cert-revoke-key-1", certificateRevoke)
	if err != nil || certificateRevoked.Value.Metadata.ResourceVersion != "5" {
		t.Fatalf("revoke certificate replay: value=%+v err=%v", certificateRevoked.Value, err)
	}
	postRevokeRequest := rotationRequest
	postRevokeRequest.ExpectedResourceVersion = "5"
	if _, err := currentNode.RotateRemoteWorkerCertificate(ctx, "tenant", "project", create.EnrollmentID, "request-certificate-revoked-node", "remote-cert-revoked-key-1", postRevokeRequest); clientStatus(err) != http.StatusUnauthorized {
		t.Fatalf("revoked certificate status=%d err=%v", clientStatus(err), err)
	}
	if _, err := currentNode.HeartbeatRemoteWorker(ctx, "tenant", "project", create.EnrollmentID, "request-heartbeat-revoked-node", heartbeat); clientStatus(err) != http.StatusUnauthorized {
		t.Fatalf("revoked heartbeat status=%d err=%v", clientStatus(err), err)
	}
	assertRemoteWorkerAdminRedaction(t, ctx, httpServer, tokens[0], create.EnrollmentID, secret)
	audit, err = admin.ListAdminRemoteWorkerEnrollmentAuditEvents(ctx, "tenant", "project", create.EnrollmentID, "request-enrollment-audit-final", 50, "")
	if err != nil {
		t.Fatalf("final enrollment audit: value=%+v err=%v", audit.Value, err)
	}
	var drainSucceeded, resumeSucceeded, expiryFailed bool
	for _, event := range audit.Value.Events {
		drainSucceeded = drainSucceeded || event.Action == "remote-worker.drain" && event.Result == "succeeded"
		resumeSucceeded = resumeSucceeded || event.Action == "remote-worker.resume" && event.Result == "succeeded"
		expiryFailed = expiryFailed || event.Action == "remote-worker.drain" && event.Result == "failed" && event.StableErrorCode == "remote-worker-command-expired"
	}
	if !drainSucceeded || !resumeSucceeded || !expiryFailed {
		t.Fatalf("missing lifecycle audit closure: value=%+v", audit.Value.Events)
	}
	revokeCreate := platform.RemoteWorkerEnrollmentCreateRequest{EnrollmentID: "enrollment-revoke-1", WorkerID: "worker-revoke-1", WorkerName: "customer-revoke-1", TTLSeconds: 600}
	if _, err := admin.CreateAdminRemoteWorkerEnrollment(ctx, "tenant", "project", "request-revoke-create", "remote-revoke-create-key-1", revokeCreate); err != nil {
		t.Fatalf("create revocable enrollment: %v", err)
	}
	revoke := platform.RemoteWorkerEnrollmentRevokeRequest{ExpectedResourceVersion: "1", ConfirmedEnrollmentID: revokeCreate.EnrollmentID}
	revoked, err := admin.RevokeAdminRemoteWorkerEnrollment(ctx, "tenant", "project", revokeCreate.EnrollmentID, "request-enrollment-revoke", "remote-enroll-revoke-key-1", revoke)
	if err != nil || revoked.Value.Spec.State != internalremoteworker.StateRevoked || revoked.Value.Metadata.ResourceVersion != "2" {
		t.Fatalf("revoke enrollment: value=%+v err=%v", revoked.Value, err)
	}
	revoked, err = admin.RevokeAdminRemoteWorkerEnrollment(ctx, "tenant", "project", revokeCreate.EnrollmentID, "request-enrollment-revoke-replay", "remote-enroll-revoke-key-1", revoke)
	if err != nil || revoked.Value.Metadata.ResourceVersion != "2" {
		t.Fatalf("revoke replay: value=%+v err=%v", revoked.Value, err)
	}

	expectedDigest, _ := internalremoteworker.SecretDigest(secret)
	var persistedDigest, enrollmentJSON, activityJSON, certificateChain string
	if err := owner.QueryRow(ctx, `SELECT secret_digest, certificate_chain_pem, to_jsonb(enrollment)::text,
		(SELECT jsonb_agg(activity)::text FROM cloud_agents.remote_worker_enrollment_activity AS activity
		 WHERE activity.tenant_id=enrollment.tenant_id AND activity.project_uid=enrollment.project_uid AND activity.enrollment_uid=enrollment.enrollment_uid)
		FROM cloud_agents.remote_worker_enrollments AS enrollment
		WHERE tenant_id='tenant' AND project_uid='project' AND enrollment_uid=$1`, create.EnrollmentID).Scan(&persistedDigest, &certificateChain, &enrollmentJSON, &activityJSON); err != nil {
		t.Fatal(err)
	}
	if persistedDigest != expectedDigest || certificateChain != rotated.Value.CertificateChainPEM || strings.Contains(enrollmentJSON, secret) || strings.Contains(activityJSON, secret) {
		t.Fatal("one-time enrollment secret persisted outside its digest")
	}
	var deniedScheduling int
	if err := owner.QueryRow(ctx, `SELECT count(*) FROM cloud_agents.admin_denied_writes
		WHERE tenant_id='tenant' AND project_uid='project' AND action='adminTransitionRemoteWorkerScheduling'`).Scan(&deniedScheduling); err != nil || deniedScheduling != 1 {
		t.Fatalf("denied scheduling audit count=%d err=%v", deniedScheduling, err)
	}
	t.Log("real Admin/bootstrap/node generated clients, server-owned DeploymentTarget projection, verified mTLS heartbeat/reconnect, generation-fenced Drain/Resume delivery, durable node restart state, exact receipt replay, deadline failure, 403 audit, Operation/Audit closure, database-time health, certificate rotation/revocation, RLS and redacted Admin reads")
}

func remoteWorkerCertificateRequest(t *testing.T, enrollmentID string) (string, []byte) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	csr, err := x509.CreateCertificateRequest(rand.Reader, &x509.CertificateRequest{Subject: pkix.Name{CommonName: enrollmentID}}, key)
	if err != nil {
		t.Fatal(err)
	}
	keyRaw, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	return string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE REQUEST", Bytes: csr})), pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyRaw})
}

func remoteWorkerMTLSHTTPClient(t *testing.T, server *httptest.Server, certificateChain string, privateKey []byte) *http.Client {
	t.Helper()
	certificate, err := tls.X509KeyPair([]byte(certificateChain), privateKey)
	if err != nil {
		t.Fatal(err)
	}
	transport, ok := server.Client().Transport.(*http.Transport)
	if !ok {
		t.Fatal("test TLS transport is unavailable")
	}
	clone := transport.Clone()
	clone.TLSClientConfig = clone.TLSClientConfig.Clone()
	clone.TLSClientConfig.Certificates = []tls.Certificate{certificate}
	return &http.Client{Transport: clone}
}

func runRemoteWorkerSandboxLive(t *testing.T, ctx context.Context, runtimePool, owner *pgxpool.Pool, admin, user, node *api.Client,
	server *httptest.Server, enrollmentID, incarnationID, certificateChain, certificateSHA256 string, privateKey []byte,
	heartbeat platform.RemoteWorkerHeartbeatRequest, otherNode *api.Client, otherEnrollmentID string,
	otherHeartbeat platform.RemoteWorkerHeartbeatRequest,
) {
	t.Helper()
	binary := os.Getenv("CLOUD_AGENTS_REMOTE_WORKER_BINARY")
	image := os.Getenv("CLOUD_AGENTS_REMOTE_WORKER_SUCCESS_IMAGE_URI")
	allowedEgress := os.Getenv("CLOUD_AGENTS_REMOTE_WORKER_ALLOWED_EGRESS")
	isolationRuntime := os.Getenv("CLOUD_AGENTS_REMOTE_WORKER_ISOLATION_RUNTIME")
	dockerAuthority := os.Getenv("CLOUD_AGENTS_REMOTE_WORKER_DOCKER_SOCKET")
	if address := os.Getenv("CLOUD_AGENTS_REMOTE_WORKER_DOCKER_ADDRESS"); address != "" {
		dockerAuthority = address
	}
	liveValues := []string{image, dockerAuthority,
		os.Getenv("CLOUD_AGENTS_REMOTE_WORKER_CREDENTIAL_DIRECTORY"), os.Getenv("CLOUD_AGENTS_REMOTE_WORKER_CREDENTIAL_REF")}
	if isolationRuntime != "gvisor" {
		liveValues = append(liveValues, allowedEgress)
	}
	liveCount := 0
	for _, value := range liveValues {
		if value != "" {
			liveCount++
		}
	}
	if liveCount == 0 {
		return
	}
	if binary == "" || liveCount != len(liveValues) {
		t.Fatal("RemoteWorker Sandbox live environment is incomplete")
	}
	t.Setenv("CLOUD_AGENTS_REMOTE_WORKER_DOCKER_ENDPOINT", startRemoteWorkerDockerProxy(t))
	createDelay := 50 * time.Second
	if isolationRuntime == "gvisor" {
		createDelay = 0
	}
	startRemoteWorkerOpenSandboxProxy(t, createDelay)
	workerServer, setWorkerConnected := startRemoteWorkerControlPlaneProxy(t, server, certificateChain, privateKey)
	parts := strings.Split(image, "@")
	if len(parts) != 2 {
		t.Fatal("RemoteWorker Sandbox image is not digest pinned")
	}
	selectedTargetID := internalremoteworker.TargetID(internalremoteworker.Scope{TenantID: "tenant", ProjectID: "project"}, enrollmentID)
	if isolationRuntime == "gvisor" {
		runRemoteWorkerSharedUntrustedLive(t, ctx, admin, user, node, workerServer, binary, enrollmentID, incarnationID,
			certificateChain, privateKey, heartbeat, image, parts[1], selectedTargetID)
		return
	}
	policy, err := admin.SetAdminNetworkPolicy(ctx, "tenant", "project", "remote-network", "request-remote-network", "remote-network-key-1", platform.NetworkPolicySetRequest{
		ExpectedResourceVersion: "0", PolicyName: "remote-network", UserSummary: "RemoteWorker approved outbound access",
		DefaultEgress: "restricted", AllowedEgress: []string{allowedEgress}, PreviewEnabled: true,
	})
	if err != nil || policy.Value.Metadata.ResourceVersion != "1" {
		t.Fatalf("create RemoteWorker network policy: value=%+v err=%v", policy.Value, err)
	}
	admissionProfile, err := admin.CreateAdminRuntimeProfile(ctx, "tenant", "project", "request-remote-admission-profile", "remote-admission-profile-key", platform.RuntimeProfileCreateRequest{
		ProfileID: "remote-admission", ProfileName: "remote-admission", Version: 1,
		Description: "RemoteWorker capability admission", TargetID: internalremoteworker.TargetID(internalremoteworker.Scope{TenantID: "tenant", ProjectID: "project"}, enrollmentID),
		WorkloadTrust: "trusted-single-tenant", IsolationRuntime: "runc",
		NetworkPolicyRef: "remote-network", ImageURI: image, ReleaseDigest: parts[1], CPUMillis: 500, MemoryBytes: 536870912,
	})
	if err != nil || admissionProfile.Value.Spec.Status != "draft" {
		t.Fatalf("create RemoteWorker admission profile: value=%+v err=%v", admissionProfile.Value, err)
	}
	admissionCases := []struct {
		name   string
		mutate func(*platform.RemoteWorkerHeartbeatRequest)
	}{
		{"runtime", func(value *platform.RemoteWorkerHeartbeatRequest) {
			value.Capabilities = []string{"exec", "files", "network-dns-nft", "preview", "pty", "ssh", "workspace-volume"}
		}},
		{"architecture", func(value *platform.RemoteWorkerHeartbeatRequest) { value.Architecture = "riscv64" }},
		{"storage", func(value *platform.RemoteWorkerHeartbeatRequest) {
			value.Capabilities = []string{"docker", "exec", "files", "network-dns-nft", "preview", "pty", "ssh"}
		}},
		{"network", func(value *platform.RemoteWorkerHeartbeatRequest) {
			value.Capabilities = []string{"docker", "exec", "files", "preview", "pty", "ssh", "workspace-volume"}
		}},
		{"cpu", func(value *platform.RemoteWorkerHeartbeatRequest) { value.Capacity.CPUMillis = 400 }},
		{"memory", func(value *platform.RemoteWorkerHeartbeatRequest) { value.Capacity.MemoryBytes = 256 << 20 }},
		{"disk", func(value *platform.RemoteWorkerHeartbeatRequest) { value.Capacity.DiskBytes = 10 << 30 }},
	}
	for _, test := range admissionCases {
		incompatible := heartbeat
		test.mutate(&incompatible)
		if _, err := node.HeartbeatRemoteWorker(ctx, "tenant", "project", enrollmentID, "request-remote-admission-heartbeat-"+test.name, incompatible); err != nil {
			t.Fatalf("set incompatible RemoteWorker %s: %v", test.name, err)
		}
		_, err := admin.PublishAdminRuntimeProfile(ctx, "tenant", "project", "remote-admission", 1,
			"request-remote-admission-publish-"+test.name, "remote-admission-publish-key-"+test.name,
			platform.RuntimeProfileTransitionRequest{ExpectedResourceVersion: "1"})
		if clientStatus(err) != http.StatusConflict {
			t.Fatalf("RemoteWorker %s admission status=%d err=%v", test.name, clientStatus(err), err)
		}
	}
	if _, err := node.HeartbeatRemoteWorker(ctx, "tenant", "project", enrollmentID, "request-remote-admission-heartbeat-ready", heartbeat); err != nil {
		t.Fatalf("restore compatible RemoteWorker admission: %v", err)
	}
	admissionProfile, err = admin.PublishAdminRuntimeProfile(ctx, "tenant", "project", "remote-admission", 1,
		"request-remote-admission-publish-ready", "remote-admission-publish-key-ready",
		platform.RuntimeProfileTransitionRequest{ExpectedResourceVersion: "1"})
	if err != nil || admissionProfile.Value.Spec.Status != "published" {
		t.Fatalf("publish compatible RemoteWorker admission profile: value=%+v err=%v", admissionProfile.Value, err)
	}
	if _, err := admin.DisableAdminRuntimeProfile(ctx, "tenant", "project", "remote-admission", 1,
		"request-remote-admission-disable", "remote-admission-disable-key",
		platform.RuntimeProfileTransitionRequest{ExpectedResourceVersion: "2"}); err != nil {
		t.Fatalf("disable RemoteWorker admission profile: %v", err)
	}
	profile, err := admin.CreateAdminRuntimeProfile(ctx, "tenant", "project", "request-remote-profile", "remote-profile-key-1", platform.RuntimeProfileCreateRequest{
		ProfileID: "remote-profile", ProfileName: "remote-profile", Version: 1,
		Description: "RemoteWorker retained workspace", TargetID: selectedTargetID,
		WorkloadTrust: "trusted-single-tenant", IsolationRuntime: "runc",
		NetworkPolicyRef: "remote-network", ImageURI: image, ReleaseDigest: parts[1], CPUMillis: 500, MemoryBytes: 536870912,
	})
	if err != nil || profile.Value.Spec.Status != "draft" {
		t.Fatalf("create RemoteWorker RuntimeProfile: value=%+v err=%v", profile.Value, err)
	}
	profile, err = admin.PublishAdminRuntimeProfile(ctx, "tenant", "project", "remote-profile", 1, "request-remote-profile-publish", "remote-profile-publish-key", platform.RuntimeProfileTransitionRequest{ExpectedResourceVersion: "1"})
	if err != nil || profile.Value.Spec.Status != "published" {
		t.Fatalf("publish RemoteWorker RuntimeProfile: value=%+v err=%v", profile.Value, err)
	}
	for _, invalid := range []struct {
		name     string
		selector platform.RuntimeProfileTargetSelector
	}{
		{"region", platform.RuntimeProfileTargetSelector{RegionID: "region-other", ResourcePoolID: "pool-remote-worker", Runtime: "docker", Architecture: "arm64"}},
		{"pool", platform.RuntimeProfileTargetSelector{RegionID: "region-local", ResourcePoolID: "pool-other", Runtime: "docker", Architecture: "arm64"}},
		{"architecture", platform.RuntimeProfileTargetSelector{RegionID: "region-local", ResourcePoolID: "pool-remote-worker", Runtime: "docker", Architecture: "amd64"}},
	} {
		profileID := "remote-selector-" + invalid.name
		_, err := admin.CreateAdminRuntimeProfile(ctx, "tenant", "project", "request-"+profileID, "create-"+profileID+"-key", platform.RuntimeProfileCreateRequest{
			ProfileID: profileID, ProfileName: profileID, Version: 1, Description: "Unavailable RemoteWorker selector",
			WorkloadTrust: "trusted-single-tenant", IsolationRuntime: "runc",
			TargetSelector: &invalid.selector, NetworkPolicyRef: "remote-network", ImageURI: image,
			ReleaseDigest: parts[1], CPUMillis: 500, MemoryBytes: 536870912,
		})
		if clientStatus(err) != http.StatusConflict {
			t.Fatalf("RemoteWorker selector %s status=%d err=%v", invalid.name, clientStatus(err), err)
		}
	}
	selectorProfile, err := admin.CreateAdminRuntimeProfile(ctx, "tenant", "project", "request-remote-selector-profile", "remote-selector-profile-key", platform.RuntimeProfileCreateRequest{
		ProfileID: "remote-selector-profile", ProfileName: "remote-selector-profile", Version: 1,
		Description:      "RemoteWorker deterministic placement",
		WorkloadTrust:    "trusted-single-tenant",
		IsolationRuntime: "runc",
		TargetSelector:   &platform.RuntimeProfileTargetSelector{RegionID: "region-local", ResourcePoolID: "pool-remote-worker", Runtime: "docker", Architecture: "arm64"},
		NetworkPolicyRef: "remote-network", ImageURI: image, ReleaseDigest: parts[1], CPUMillis: 500, MemoryBytes: 536870912,
	})
	if err != nil || selectorProfile.Value.Spec.TargetID != "" || selectorProfile.Value.Spec.TargetSelector == nil {
		t.Fatalf("create RemoteWorker selector profile: value=%+v err=%v", selectorProfile.Value, err)
	}
	selectorProfile, err = admin.PublishAdminRuntimeProfile(ctx, "tenant", "project", "remote-selector-profile", 1,
		"request-remote-selector-profile-publish", "remote-selector-profile-publish-key",
		platform.RuntimeProfileTransitionRequest{ExpectedResourceVersion: "1"})
	if err != nil || selectorProfile.Value.Spec.Status != "published" {
		t.Fatalf("publish RemoteWorker selector profile: value=%+v err=%v", selectorProfile.Value, err)
	}
	publicProfiles, err := user.ListRuntimeProfiles(ctx, "tenant", "project", "request-remote-profile-list", 50, "")
	foundProfile, foundSelectorProfile := false, false
	for _, item := range publicProfiles.Value.RuntimeProfiles {
		foundProfile = foundProfile || item.ProfileID == "remote-profile"
		foundSelectorProfile = foundSelectorProfile || item.ProfileID == "remote-selector-profile"
	}
	if err != nil || !foundProfile || !foundSelectorProfile {
		t.Fatalf("RemoteWorker RuntimeProfile is not publicly available: value=%+v err=%v", publicProfiles.Value, err)
	}
	sandbox, err := user.CreateSandbox(ctx, "tenant", "project", "request-remote-sandbox", "remote-sandbox-key-1", platform.SandboxSessionCreateRequest{
		WorkspaceID: "remote-workspace", WorkspaceName: "remote-workspace", SandboxID: "remote-sandbox",
		RuntimeProfileID: "remote-selector-profile", RuntimeProfileVersion: 1, TTLSeconds: 120,
	})
	if err != nil || sandbox.Value.ObservedState != "pending" {
		t.Fatalf("create RemoteWorker Sandbox: value=%+v err=%v", sandbox.Value, err)
	}
	placedSandbox, err := admin.GetAdminSandboxSession(ctx, "tenant", "project", "remote-sandbox", "request-remote-sandbox-placement")
	if err != nil || placedSandbox.Value.Spec.TargetID != selectedTargetID {
		t.Fatalf("RemoteWorker Sandbox placement: value=%+v err=%v", placedSandbox.Value, err)
	}
	otherDelivery, err := otherNode.HeartbeatRemoteWorker(ctx, "tenant", "project", otherEnrollmentID, "request-remote-sandbox-other-node", otherHeartbeat)
	if err != nil || otherDelivery.Value.SandboxCommand != nil {
		t.Fatalf("unselected RemoteWorker received Sandbox command: value=%+v err=%v", otherDelivery.Value, err)
	}
	withoutNetwork := heartbeat
	withoutNetwork.Capabilities = []string{"docker", "exec", "files", "preview", "pty", "ssh", "workspace-volume"}
	blocked, err := node.HeartbeatRemoteWorker(ctx, "tenant", "project", enrollmentID, "request-remote-sandbox-blocked-network", withoutNetwork)
	if err != nil || blocked.Value.SandboxCommand != nil {
		t.Fatalf("RemoteWorker Sandbox escaped network capability admission: value=%+v err=%v", blocked.Value, err)
	}
	blockedProfiles, err := user.ListRuntimeProfiles(ctx, "tenant", "project", "request-remote-profile-blocked-network", 50, "")
	directAvailable, selectorAvailable := false, false
	for _, item := range blockedProfiles.Value.RuntimeProfiles {
		directAvailable = directAvailable || item.ProfileID == "remote-profile"
		selectorAvailable = selectorAvailable || item.ProfileID == "remote-selector-profile"
	}
	if err != nil || directAvailable || !selectorAvailable {
		t.Fatalf("incompatible RemoteWorker profile remained public: value=%+v err=%v", blockedProfiles.Value, err)
	}
	delivery, err := node.HeartbeatRemoteWorker(ctx, "tenant", "project", enrollmentID, "request-remote-sandbox-delivery", heartbeat)
	if err != nil || delivery.Value.SandboxCommand == nil {
		var available bool
		var pending int
		queryErr := owner.QueryRow(ctx, `SELECT cloud_agents.foundation_target_available_v1('tenant','project',$1),
            (SELECT count(*) FROM cloud_agents.outbox_events AS event
             JOIN cloud_agents.sandbox_sessions AS sandbox ON sandbox.tenant_id=event.tenant_id
              AND sandbox.operation_id=event.operation_id AND sandbox.operation_generation=event.operation_generation
             JOIN cloud_agents.workspace_volumes AS volume ON volume.tenant_id=sandbox.tenant_id
              AND volume.project_uid=sandbox.project_uid AND volume.workspace_uid=sandbox.workspace_uid
             WHERE event.state='pending' AND volume.target_uid=$1)`,
			internalremoteworker.TargetID(internalremoteworker.Scope{TenantID: "tenant", ProjectID: "project"}, enrollmentID)).Scan(&available, &pending)
		t.Fatalf("deliver RemoteWorker Sandbox command: value=%+v err=%v available=%v pending=%d queryErr=%v", delivery.Value, err, available, pending, queryErr)
	}
	command := delivery.Value.SandboxCommand
	var claimExpiryBefore time.Time
	if err := owner.QueryRow(ctx, `SELECT claim_expires_at FROM cloud_agents.outbox_events
WHERE tenant_id='tenant' AND operation_id=$1 AND state='claimed'`, command.OperationID).Scan(&claimExpiryBefore); err != nil {
		t.Fatalf("read RemoteWorker Sandbox claim: %v", err)
	}
	heartbeat.SandboxCommandID = command.CommandID
	if _, err := node.HeartbeatRemoteWorker(ctx, "tenant", "project", enrollmentID, "request-remote-sandbox-renew", heartbeat); err != nil {
		t.Fatalf("renew RemoteWorker Sandbox claim: %v", err)
	}
	var claimExpiryAfter, attemptExpiryAfter time.Time
	if err := owner.QueryRow(ctx, `SELECT event.claim_expires_at, attempt.claim_expires_at
FROM cloud_agents.outbox_events AS event
JOIN cloud_agents.operation_attempts AS attempt
  ON attempt.tenant_id=event.tenant_id AND attempt.operation_id=event.operation_id
 AND attempt.operation_generation=event.operation_generation AND attempt.attempt_number=event.delivery_attempts
WHERE event.tenant_id='tenant' AND event.operation_id=$1 AND event.state='claimed'`, command.OperationID).Scan(&claimExpiryAfter, &attemptExpiryAfter); err != nil || !claimExpiryAfter.After(claimExpiryBefore) || !claimExpiryAfter.Equal(attemptExpiryAfter) {
		t.Fatalf("renewed claim expiry before=%s after=%s attempt=%s err=%v", claimExpiryBefore, claimExpiryAfter, attemptExpiryAfter, err)
	}
	heartbeat.SandboxCommandID = "rwsc-wrong"
	_, err = node.HeartbeatRemoteWorker(ctx, "tenant", "project", enrollmentID, "request-remote-sandbox-renew-wrong", heartbeat)
	failure, ok := err.(*api.ClientError)
	if !ok || failure.Status != http.StatusConflict {
		t.Fatalf("wrong RemoteWorker Sandbox renewal error=%T %v", err, err)
	}
	heartbeat.SandboxCommandID = ""
	stateFile := filepath.Join(t.TempDir(), "remote-worker-sandbox-state.json")
	state, _ := json.Marshal(map[string]any{"incarnationId": incarnationID, "observedGeneration": heartbeat.ObservedGeneration,
		"observedState": heartbeat.ObservedState, "sandboxCommand": command})
	if err := os.WriteFile(stateFile, append(state, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
	originalCommandID, originalAttempt := command.CommandID, command.Attempt
	interrupted := remoteWorkerProcessCommand(t, ctx, binary, workerServer, enrollmentID, incarnationID, certificateChain, privateKey, stateFile, true)
	var interruptedOutput bytes.Buffer
	interrupted.Stdout, interrupted.Stderr = &interruptedOutput, &interruptedOutput
	if err := interrupted.Start(); err != nil {
		t.Fatalf("start long RemoteWorker Sandbox command: %v", err)
	}
	var longClaimExpiry time.Time
	renewalDeadline := time.Now().Add(35 * time.Second)
	for !time.Now().After(renewalDeadline) {
		if err := owner.QueryRow(ctx, `SELECT claim_expires_at FROM cloud_agents.outbox_events
WHERE tenant_id='tenant' AND operation_id=$1 AND state='claimed'`, command.OperationID).Scan(&longClaimExpiry); err != nil {
			t.Fatalf("read long RemoteWorker Sandbox claim: %v", err)
		}
		if longClaimExpiry.After(claimExpiryAfter) {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if !longClaimExpiry.After(claimExpiryAfter) {
		_ = interrupted.Process.Kill()
		_ = interrupted.Wait()
		t.Fatalf("long RemoteWorker Sandbox claim was not renewed: initial=%s current=%s", claimExpiryAfter, longClaimExpiry)
	}
	setWorkerConnected(false)
	if err := interrupted.Wait(); err == nil {
		t.Fatal("RemoteWorker Sandbox command survived a dropped renewal connection")
	}
	stateData, err := os.ReadFile(stateFile)
	var interruptedState struct {
		ExecutingCommandID    string                                      `json:"executingCommandId"`
		SandboxCommand        *json.RawMessage                            `json:"sandboxCommand"`
		SandboxCommandReceipt *platform.RemoteWorkerSandboxCommandReceipt `json:"sandboxCommandReceipt"`
	}
	if err != nil || json.Unmarshal(stateData, &interruptedState) != nil || interruptedState.ExecutingCommandID != "" || interruptedState.SandboxCommand != nil || interruptedState.SandboxCommandReceipt != nil {
		t.Fatalf("interrupted RemoteWorker state was not cleared: state=%s err=%v output=%s", stateData, err, interruptedOutput.String())
	}
	if delay := time.Until(longClaimExpiry) + 100*time.Millisecond; delay > 0 {
		time.Sleep(delay)
	}
	setWorkerConnected(true)
	runRemoteWorkerHeartbeatProcess(t, ctx, binary, workerServer, enrollmentID, incarnationID, certificateChain, privateKey, stateFile)
	stateData, err = os.ReadFile(stateFile)
	var reconnectedState struct {
		SandboxCommandReceipt *platform.RemoteWorkerSandboxCommandReceipt `json:"sandboxCommandReceipt"`
	}
	if err != nil || json.Unmarshal(stateData, &reconnectedState) != nil || reconnectedState.SandboxCommandReceipt == nil ||
		reconnectedState.SandboxCommandReceipt.Result != "succeeded" || reconnectedState.SandboxCommandReceipt.Attempt != originalAttempt+1 ||
		reconnectedState.SandboxCommandReceipt.CommandID == originalCommandID {
		t.Fatalf("reconnected RemoteWorker did not reconcile a new attempt: state=%s err=%v", stateData, err)
	}
	command.CommandID, command.Attempt = reconnectedState.SandboxCommandReceipt.CommandID, reconnectedState.SandboxCommandReceipt.Attempt
	runRemoteWorkerHeartbeatProcess(t, ctx, binary, workerServer, enrollmentID, incarnationID, certificateChain, privateKey, stateFile)
	adminSandbox, err := admin.GetAdminSandboxSession(ctx, "tenant", "project", "remote-sandbox", "request-remote-sandbox-get")
	if err != nil || adminSandbox.Value.Spec.ObservedState != "running" || adminSandbox.Value.Spec.RuntimeID == "" || adminSandbox.Value.Spec.PhysicalVolumeID == "" {
		t.Fatalf("RemoteWorker Sandbox settlement: value=%+v err=%v", adminSandbox.Value, err)
	}
	adminEnrollment, err := admin.GetAdminRemoteWorkerEnrollment(ctx, "tenant", "project", enrollmentID, "request-remote-capacity-initial")
	var reservation *platform.RemoteWorkerCapacityReservation
	var placement *platform.RemoteWorkerNodePlacement
	if adminEnrollment.Value.Spec.Node != nil {
		reservation = adminEnrollment.Value.Spec.Node.Reservation
		placement = adminEnrollment.Value.Spec.Node.Placement
	}
	if err != nil || reservation == nil || placement == nil || placement.RegionID != "region-local" ||
		placement.ResourcePoolID != "pool-remote-worker" || placement.NodeID != adminEnrollment.Value.Spec.TargetID ||
		reservation.State != "available" || reservation.ReservedCPUMillis != 500 ||
		reservation.ReservedMemoryBytes != 536870912 || reservation.ReservedDiskBytes != 20<<30 {
		t.Fatalf("RemoteWorker capacity projection: value=%+v err=%v", adminEnrollment.Value, err)
	}
	limitedCapacity := heartbeat
	limitedCapacity.Capacity = platform.RemoteWorkerCapacity{CPUMillis: 500, MemoryBytes: 536870912, DiskBytes: 20 << 30}
	if _, err := node.HeartbeatRemoteWorker(ctx, "tenant", "project", enrollmentID, "request-remote-capacity-exhausted", limitedCapacity); err != nil {
		t.Fatalf("report exhausted RemoteWorker capacity: %v", err)
	}
	otherCapacity, err := otherNode.HeartbeatRemoteWorker(ctx, "tenant", "project", otherEnrollmentID, "request-remote-capacity-other-node", otherHeartbeat)
	if err != nil || otherCapacity.Value.SandboxCommand != nil {
		t.Fatalf("refresh unselected RemoteWorker capacity: value=%+v err=%v", otherCapacity.Value, err)
	}
	adminEnrollment, err = admin.GetAdminRemoteWorkerEnrollment(ctx, "tenant", "project", enrollmentID, "request-remote-capacity-exhausted-get")
	reservation = nil
	if adminEnrollment.Value.Spec.Node != nil {
		reservation = adminEnrollment.Value.Spec.Node.Reservation
	}
	if err != nil || reservation == nil || reservation.State != "exhausted" ||
		reservation.AvailableCPUMillis != 0 || reservation.AvailableMemoryBytes != 0 || reservation.AvailableDiskBytes != 0 {
		t.Fatalf("exhausted RemoteWorker capacity projection: value=%+v err=%v", adminEnrollment.Value, err)
	}
	blockedProfiles, err = user.ListRuntimeProfiles(ctx, "tenant", "project", "request-remote-profile-capacity-exhausted", 50, "")
	directAvailable, selectorAvailable = false, false
	for _, item := range blockedProfiles.Value.RuntimeProfiles {
		directAvailable = directAvailable || item.ProfileID == "remote-profile"
		selectorAvailable = selectorAvailable || item.ProfileID == "remote-selector-profile"
	}
	if err != nil || directAvailable || !selectorAvailable {
		t.Fatalf("exhausted RemoteWorker profile remained public: value=%+v err=%v", blockedProfiles.Value, err)
	}
	_, err = user.CreateSandbox(ctx, "tenant", "project", "request-remote-capacity-rejected", "remote-capacity-rejected-key", platform.SandboxSessionCreateRequest{
		WorkspaceID: "remote-capacity-workspace", WorkspaceName: "remote-capacity-workspace", SandboxID: "remote-capacity-sandbox",
		RuntimeProfileID: "remote-profile", RuntimeProfileVersion: 1, TTLSeconds: 120,
	})
	var rejectedCapacityResidue int
	queryErr := owner.QueryRow(ctx, `SELECT count(*) FROM cloud_agents.workspaces
WHERE tenant_id='tenant' AND project_uid='project' AND workspace_uid='remote-capacity-workspace'`).Scan(&rejectedCapacityResidue)
	if clientStatus(err) != http.StatusConflict || queryErr != nil || rejectedCapacityResidue != 0 {
		t.Fatalf("RemoteWorker capacity admission: status=%d err=%v residue=%d queryErr=%v", clientStatus(err), err, rejectedCapacityResidue, queryErr)
	}
	if _, err := node.HeartbeatRemoteWorker(ctx, "tenant", "project", enrollmentID, "request-remote-capacity-restored", heartbeat); err != nil {
		t.Fatalf("restore RemoteWorker capacity: %v", err)
	}
	heartbeat.SandboxCommandReceipt = &platform.RemoteWorkerSandboxCommandReceipt{CommandID: command.CommandID,
		Attempt: command.Attempt, Action: command.Action, OperationID: command.OperationID, SandboxID: command.SandboxID,
		SandboxGeneration: command.SandboxGeneration, Result: "succeeded", RuntimeID: adminSandbox.Value.Spec.RuntimeID,
		RuntimeState: "Running", VolumeName: adminSandbox.Value.Spec.PhysicalVolumeID}
	if _, err := node.HeartbeatRemoteWorker(ctx, "tenant", "project", enrollmentID, "request-remote-sandbox-replay-1", heartbeat); err != nil {
		t.Fatalf("replay RemoteWorker Sandbox receipt: %v", err)
	}
	if _, err := node.HeartbeatRemoteWorker(ctx, "tenant", "project", enrollmentID, "request-remote-sandbox-replay-2", heartbeat); err != nil {
		t.Fatalf("repeat RemoteWorker Sandbox receipt replay: %v", err)
	}
	runtimeID, volumeName := adminSandbox.Value.Spec.RuntimeID, adminSandbox.Value.Spec.PhysicalVolumeID
	sandboxDirectory, err := opensandbox.NewCredentialDirectory(os.Getenv("CLOUD_AGENTS_REMOTE_WORKER_CREDENTIAL_DIRECTORY"))
	if err != nil {
		t.Fatalf("open node-local OpenSandbox credentials: %v", err)
	}
	runtimeClient, err := sandboxDirectory.Client(os.Getenv("CLOUD_AGENTS_REMOTE_WORKER_CREDENTIAL_REF"))
	if err != nil {
		t.Fatalf("open node-local OpenSandbox client: %v", err)
	}
	createIdentity := opensandbox.Identity{Tenant: "tenant", Project: "project", Workspace: command.WorkspaceID,
		Sandbox: command.SandboxID, Operation: command.OperationID, Generation: command.SandboxGeneration, SpecDigest: command.SpecDigest}
	proof, err := runtimeClient.Exec(ctx, opensandbox.ExecInput{Identity: createIdentity, RuntimeID: runtimeID,
		Command: "printf remote-worker-rebuild-proof > /workspace/remote-worker-rebuild-proof.txt && sha256sum /workspace/remote-worker-rebuild-proof.txt", Timeout: 10 * time.Second})
	workspaceDigest := strings.TrimSpace(proof.Stdout)
	if err != nil || proof.ExitCode != 0 || workspaceDigest == "" {
		t.Fatalf("write RemoteWorker Workspace proof: result=%+v err=%v", proof, err)
	}
	stopOperation, err := admin.StopAdminSandboxSession(ctx, "tenant", "project", "remote-sandbox", "request-remote-sandbox-stop", "remote-sandbox-stop-key-1", platform.SandboxSessionLifecycleRequest{
		ExpectedGeneration: adminSandbox.Value.Spec.Generation, ExpectedResourceVersion: adminSandbox.Value.Metadata.ResourceVersion,
		ConfirmedSandboxID: "remote-sandbox", ComputeDisposition: "delete", WorkspaceDisposition: "retain",
	})
	if err != nil || stopOperation.Value.Action != "sandbox.stop" || stopOperation.Value.State != "pending" {
		t.Fatalf("accept RemoteWorker Sandbox stop: value=%+v err=%v", stopOperation.Value, err)
	}
	heartbeat.SandboxCommandReceipt = nil
	stopDelivery, err := node.HeartbeatRemoteWorker(ctx, "tenant", "project", enrollmentID, "request-remote-sandbox-stop-delivery", heartbeat)
	if err != nil || stopDelivery.Value.SandboxCommand == nil {
		t.Fatalf("deliver RemoteWorker Sandbox stop command: value=%+v err=%v", stopDelivery.Value, err)
	}
	stopCommand := stopDelivery.Value.SandboxCommand
	if stopCommand.Action != "sandbox.stop" || stopCommand.RuntimeID != runtimeID || stopCommand.PhysicalVolumeName != volumeName ||
		stopCommand.RuntimeState != "Running" || stopCommand.RuntimeGeneration >= stopCommand.SandboxGeneration {
		t.Fatalf("RemoteWorker Sandbox stop command authority drifted: value=%+v", stopCommand)
	}
	state, _ = json.Marshal(map[string]any{"incarnationId": incarnationID, "observedGeneration": heartbeat.ObservedGeneration,
		"observedState": heartbeat.ObservedState, "sandboxCommand": stopCommand})
	if err := os.WriteFile(stateFile, append(state, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
	runRemoteWorkerHeartbeatProcess(t, ctx, binary, server, enrollmentID, incarnationID, certificateChain, privateKey, stateFile)
	adminSandbox, err = admin.GetAdminSandboxSession(ctx, "tenant", "project", "remote-sandbox", "request-remote-sandbox-get-stopped")
	if err != nil || adminSandbox.Value.Spec.TargetID != selectedTargetID || adminSandbox.Value.Spec.ObservedState != "stopped" || adminSandbox.Value.Spec.RuntimeID != "" ||
		adminSandbox.Value.Spec.PhysicalVolumeID != volumeName || !adminSandbox.Value.Spec.WriterReleased {
		t.Fatalf("RemoteWorker Sandbox stop settlement: value=%+v err=%v", adminSandbox.Value, err)
	}
	heartbeat.SandboxCommandReceipt = &platform.RemoteWorkerSandboxCommandReceipt{CommandID: stopCommand.CommandID,
		Attempt: stopCommand.Attempt, Action: stopCommand.Action, OperationID: stopCommand.OperationID,
		SandboxID: stopCommand.SandboxID, SandboxGeneration: stopCommand.SandboxGeneration,
		Result: "succeeded", VolumeName: volumeName, CleanupComplete: true}
	if _, err := node.HeartbeatRemoteWorker(ctx, "tenant", "project", enrollmentID, "request-remote-sandbox-stop-replay-1", heartbeat); err != nil {
		t.Fatalf("replay RemoteWorker Sandbox stop receipt: %v", err)
	}
	if _, err := node.HeartbeatRemoteWorker(ctx, "tenant", "project", enrollmentID, "request-remote-sandbox-stop-replay-2", heartbeat); err != nil {
		t.Fatalf("repeat RemoteWorker Sandbox stop receipt replay: %v", err)
	}
	rebuildOperation, err := admin.RebuildAdminSandboxSession(ctx, "tenant", "project", "remote-sandbox", "request-remote-sandbox-rebuild", "remote-sandbox-rebuild-key-1", platform.SandboxSessionLifecycleRequest{
		ExpectedGeneration: adminSandbox.Value.Spec.Generation, ExpectedResourceVersion: adminSandbox.Value.Metadata.ResourceVersion,
		ConfirmedSandboxID: "remote-sandbox", ComputeDisposition: "create", WorkspaceDisposition: "retain",
	})
	if err != nil || rebuildOperation.Value.Action != "sandbox.rebuild" || rebuildOperation.Value.State != "pending" {
		t.Fatalf("accept RemoteWorker Sandbox rebuild: value=%+v err=%v", rebuildOperation.Value, err)
	}
	heartbeat.SandboxCommandReceipt = nil
	rebuildDelivery, err := node.HeartbeatRemoteWorker(ctx, "tenant", "project", enrollmentID, "request-remote-sandbox-rebuild-delivery", heartbeat)
	if err != nil || rebuildDelivery.Value.SandboxCommand == nil {
		t.Fatalf("deliver RemoteWorker Sandbox rebuild command: value=%+v err=%v", rebuildDelivery.Value, err)
	}
	rebuildCommand := rebuildDelivery.Value.SandboxCommand
	if rebuildCommand.Action != "sandbox.rebuild" || rebuildCommand.PhysicalVolumeName != volumeName ||
		rebuildCommand.RuntimeID != "" || rebuildCommand.RuntimeOperationID != "" || rebuildCommand.RuntimeGeneration != 0 ||
		rebuildCommand.RuntimeSpecDigest != "" || rebuildCommand.SandboxGeneration != stopCommand.SandboxGeneration+1 {
		t.Fatalf("RemoteWorker Sandbox rebuild command authority drifted: value=%+v", rebuildCommand)
	}
	state, _ = json.Marshal(map[string]any{"incarnationId": incarnationID, "observedGeneration": heartbeat.ObservedGeneration,
		"observedState": heartbeat.ObservedState, "sandboxCommand": rebuildCommand})
	if err := os.WriteFile(stateFile, append(state, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
	runRemoteWorkerHeartbeatProcess(t, ctx, binary, server, enrollmentID, incarnationID, certificateChain, privateKey, stateFile)
	adminSandbox, err = admin.GetAdminSandboxSession(ctx, "tenant", "project", "remote-sandbox", "request-remote-sandbox-get-rebuilt")
	for retry := 0; err != nil || adminSandbox.Value.Spec.ObservedState != "running"; retry++ {
		if retry == 3 {
			t.Fatalf("RemoteWorker Sandbox rebuild settlement: value=%+v err=%v", adminSandbox.Value, err)
		}
		time.Sleep(time.Duration(1<<retry)*time.Second + 100*time.Millisecond)
		retryDelivery, retryErr := node.HeartbeatRemoteWorker(ctx, "tenant", "project", enrollmentID,
			fmt.Sprintf("request-remote-sandbox-rebuild-retry-%d", retry+1), heartbeat)
		if retryErr != nil || retryDelivery.Value.SandboxCommand == nil {
			t.Fatalf("redeliver RemoteWorker Sandbox rebuild command: value=%+v err=%v", retryDelivery.Value, retryErr)
		}
		nextCommand := retryDelivery.Value.SandboxCommand
		if nextCommand.Action != "sandbox.rebuild" || nextCommand.OperationID != rebuildCommand.OperationID ||
			nextCommand.Attempt != rebuildCommand.Attempt+1 || nextCommand.PhysicalVolumeName != volumeName {
			t.Fatalf("RemoteWorker Sandbox rebuild retry authority drifted: value=%+v", nextCommand)
		}
		rebuildCommand = nextCommand
		state, _ = json.Marshal(map[string]any{"incarnationId": incarnationID, "observedGeneration": heartbeat.ObservedGeneration,
			"observedState": heartbeat.ObservedState, "sandboxCommand": rebuildCommand})
		if err := os.WriteFile(stateFile, append(state, '\n'), 0o600); err != nil {
			t.Fatal(err)
		}
		runRemoteWorkerHeartbeatProcess(t, ctx, binary, server, enrollmentID, incarnationID, certificateChain, privateKey, stateFile)
		adminSandbox, err = admin.GetAdminSandboxSession(ctx, "tenant", "project", "remote-sandbox",
			fmt.Sprintf("request-remote-sandbox-get-rebuilt-%d", retry+1))
	}
	rebuiltRuntimeID := adminSandbox.Value.Spec.RuntimeID
	if rebuiltRuntimeID == "" || rebuiltRuntimeID == runtimeID || adminSandbox.Value.Spec.TargetID != selectedTargetID || adminSandbox.Value.Spec.PhysicalVolumeID != volumeName ||
		adminSandbox.Value.Spec.WriterReleased || adminSandbox.Value.Spec.Generation != rebuildCommand.SandboxGeneration {
		t.Fatalf("RemoteWorker Sandbox rebuilt authority drifted: value=%+v", adminSandbox.Value)
	}
	heartbeat.SandboxCommandReceipt = &platform.RemoteWorkerSandboxCommandReceipt{CommandID: rebuildCommand.CommandID,
		Attempt: rebuildCommand.Attempt, Action: rebuildCommand.Action, OperationID: rebuildCommand.OperationID,
		SandboxID: rebuildCommand.SandboxID, SandboxGeneration: rebuildCommand.SandboxGeneration,
		Result: "succeeded", RuntimeID: rebuiltRuntimeID, RuntimeState: "Running", VolumeName: volumeName}
	if _, err := node.HeartbeatRemoteWorker(ctx, "tenant", "project", enrollmentID, "request-remote-sandbox-rebuild-replay-1", heartbeat); err != nil {
		t.Fatalf("replay RemoteWorker Sandbox rebuild receipt: %v", err)
	}
	if _, err := node.HeartbeatRemoteWorker(ctx, "tenant", "project", enrollmentID, "request-remote-sandbox-rebuild-replay-2", heartbeat); err != nil {
		t.Fatalf("repeat RemoteWorker Sandbox rebuild receipt replay: %v", err)
	}
	type sandboxExecOutcome struct {
		result api.SandboxExecResult
		err    error
	}
	execRequest := platform.SandboxExecRequest{
		ExpectedGeneration: rebuildCommand.SandboxGeneration,
		Command:            `ln -sfn /etc /workspace/remote-files-link; node -e 'const http=require("http");http.createServer((req,res)=>{let body="";req.on("data",chunk=>body+=chunk);req.on("end",()=>{res.setHeader("content-type","application/json");res.setHeader("set-cookie","secret=value");res.end(JSON.stringify({method:req.method,url:req.url,body,authorization:req.headers.authorization||"",proxyAuthorization:req.headers["proxy-authorization"]||"",cookie:req.headers.cookie||"",apiKey:req.headers["open-sandbox-api-key"]||"",forwarded:req.headers.forwarded||req.headers["x-forwarded-for"]||"",public:req.headers["x-public"]||""}));});}).listen(3000,"0.0.0.0");' >/workspace/remote-preview.log 2>&1 & sleep 0.2; sha256sum /workspace/remote-worker-rebuild-proof.txt; printf remote-exec-stderr >&2; exit 7`,
		TimeoutSeconds:     10,
	}
	staleExecRequest := execRequest
	staleExecRequest.ExpectedGeneration--
	if _, err := user.ExecSandbox(ctx, "tenant", "project", "remote-sandbox", "request-remote-sandbox-exec-stale", staleExecRequest); clientStatus(err) != http.StatusConflict {
		t.Fatalf("stale RemoteWorker Sandbox Exec status=%d err=%v", clientStatus(err), err)
	}
	if _, err := admin.ExecSandbox(ctx, "tenant", "project", "remote-sandbox", "request-remote-sandbox-exec-admin", execRequest); clientStatus(err) != http.StatusForbidden {
		t.Fatalf("Admin RemoteWorker Sandbox Exec status=%d err=%v", clientStatus(err), err)
	}
	execDone := make(chan sandboxExecOutcome, 1)
	go func() {
		result, execErr := user.ExecSandbox(ctx, "tenant", "project", "remote-sandbox", "request-remote-sandbox-exec", execRequest)
		execDone <- sandboxExecOutcome{result: result, err: execErr}
	}()
	var execCommandID string
	for retry := 0; ; retry++ {
		select {
		case outcome := <-execDone:
			t.Fatalf("queue RemoteWorker Sandbox Exec API: value=%+v err=%v", outcome.result.Value, outcome.err)
		default:
		}
		err = owner.QueryRow(ctx, `SELECT command_uid FROM cloud_agents.remote_worker_sandbox_exec_commands
WHERE tenant_id='tenant' AND project_uid='project' AND request_id='request-remote-sandbox-exec'`).Scan(&execCommandID)
		if err == nil {
			break
		}
		if err != pgx.ErrNoRows || retry == 100 {
			t.Fatalf("queue RemoteWorker Sandbox Exec: %v", err)
		}
		time.Sleep(25 * time.Millisecond)
	}
	heartbeat.SandboxCommandReceipt = nil
	runRemoteWorkerHeartbeatProcess(t, ctx, binary, server, enrollmentID, incarnationID, certificateChain, privateKey, stateFile)
	state, err = os.ReadFile(stateFile)
	if err != nil {
		t.Fatal(err)
	}
	var execState struct {
		SandboxExecCommand        *platform.RemoteWorkerSandboxExecCommand        `json:"sandboxExecCommand"`
		SandboxExecCommandReceipt *platform.RemoteWorkerSandboxExecCommandReceipt `json:"sandboxExecCommandReceipt"`
	}
	if err := json.Unmarshal(state, &execState); err != nil || execState.SandboxExecCommand == nil ||
		execState.SandboxExecCommandReceipt == nil || execState.SandboxExecCommand.CommandID != execCommandID ||
		execState.SandboxExecCommandReceipt.CommandID != execCommandID || execState.SandboxExecCommandReceipt.Result != "succeeded" ||
		execState.SandboxExecCommandReceipt.ExitCode != 7 || strings.TrimSpace(execState.SandboxExecCommandReceipt.Stdout) != workspaceDigest ||
		execState.SandboxExecCommandReceipt.Stderr != "remote-exec-stderr" {
		t.Fatalf("execute RemoteWorker Sandbox Exec: state=%s error=%v", state, err)
	}
	execReceipt := *execState.SandboxExecCommandReceipt
	runRemoteWorkerHeartbeatProcess(t, ctx, binary, server, enrollmentID, incarnationID, certificateChain, privateKey, stateFile)
	var execResult api.SandboxExecResult
	select {
	case outcome := <-execDone:
		if outcome.err != nil {
			t.Fatalf("settle RemoteWorker Sandbox Exec: %v", outcome.err)
		}
		execResult = outcome.result
	case <-time.After(15 * time.Second):
		t.Fatal("RemoteWorker Sandbox Exec did not settle")
	}
	if execResult.Value.Generation != rebuildCommand.SandboxGeneration || execResult.Value.ExitCode != 7 ||
		strings.TrimSpace(execResult.Value.Stdout) != workspaceDigest || execResult.Value.Stderr != "remote-exec-stderr" ||
		execResult.Value.ExecutionTimeMillis < 0 || execResult.Value.ExecutionTimeMillis > 65000 {
		t.Fatalf("RemoteWorker Sandbox Exec result=%+v", execResult.Value)
	}
	replayedExec, err := user.ExecSandbox(ctx, "tenant", "project", "remote-sandbox", "request-remote-sandbox-exec", execRequest)
	if err != nil || replayedExec.Value != execResult.Value {
		t.Fatalf("replay RemoteWorker Sandbox Exec: value=%+v err=%v", replayedExec.Value, err)
	}
	conflictingExecRequest := execRequest
	conflictingExecRequest.Command += " "
	if _, err := user.ExecSandbox(ctx, "tenant", "project", "remote-sandbox", "request-remote-sandbox-exec", conflictingExecRequest); clientStatus(err) != http.StatusConflict {
		t.Fatalf("conflicting RemoteWorker Sandbox Exec replay status=%d err=%v", clientStatus(err), err)
	}
	heartbeat.SandboxExecCommandReceipt = &execReceipt
	if _, err := node.HeartbeatRemoteWorker(ctx, "tenant", "project", enrollmentID, "request-remote-sandbox-exec-replay-1", heartbeat); err != nil {
		t.Fatalf("replay RemoteWorker Sandbox Exec receipt: %v", err)
	}
	if _, err := node.HeartbeatRemoteWorker(ctx, "tenant", "project", enrollmentID, "request-remote-sandbox-exec-replay-2", heartbeat); err != nil {
		t.Fatalf("repeat RemoteWorker Sandbox Exec receipt replay: %v", err)
	}
	conflictingExecReceipt := execReceipt
	conflictingExecReceipt.Stderr += "!"
	heartbeat.SandboxExecCommandReceipt = &conflictingExecReceipt
	if _, err := node.HeartbeatRemoteWorker(ctx, "tenant", "project", enrollmentID, "request-remote-sandbox-exec-receipt-conflict", heartbeat); clientStatus(err) != http.StatusConflict {
		t.Fatalf("conflicting RemoteWorker Sandbox Exec receipt status=%d err=%v", clientStatus(err), err)
	}
	heartbeat.SandboxExecCommandReceipt = nil
	var execStateValue string
	var execIncarnationBound, execCertificateBound, execReceiptSettled, execContentTableHidden bool
	if err := owner.QueryRow(ctx, `SELECT state, assigned_incarnation_uid=$2,
delivery_certificate_sha256=$3, receipt_digest IS NOT NULL,
NOT pg_catalog.has_table_privilege('cloud_agents_runtime', 'cloud_agents.remote_worker_sandbox_exec_commands', 'SELECT')
FROM cloud_agents.remote_worker_sandbox_exec_commands
WHERE tenant_id='tenant' AND project_uid='project' AND command_uid=$1`, execCommandID, incarnationID, certificateSHA256).Scan(
		&execStateValue, &execIncarnationBound, &execCertificateBound, &execReceiptSettled, &execContentTableHidden,
	); err != nil || execStateValue != "succeeded" || !execIncarnationBound || !execCertificateBound || !execReceiptSettled || !execContentTableHidden {
		t.Fatalf("RemoteWorker Sandbox Exec authority: state=%q incarnation=%v certificate=%v receipt=%v hidden=%v err=%v",
			execStateValue, execIncarnationBound, execCertificateBound, execReceiptSettled, execContentTableHidden, err)
	}

	grant, err := user.CreateSandboxAccessGrant(ctx, "tenant", "project", "remote-sandbox", "request-remote-file-grant", "remote-file-grant-key-1", platform.SandboxAccessGrantCreateRequest{
		ExpectedGeneration: adminSandbox.Value.Spec.Generation, TTLSeconds: 90,
	})
	if err != nil || grant.Value.AccessToken == "" || grant.Value.Generation != adminSandbox.Value.Spec.Generation {
		t.Fatalf("issue RemoteWorker Files Grant: value=%+v err=%v", grant.Value, err)
	}
	gatewayStore, err := postgres.NewAccessGatewayStore(runtimePool)
	if err != nil {
		t.Fatal(err)
	}
	newGateway := func() *httptest.Server {
		handler, gatewayErr := accessgateway.New(gatewayStore, sandboxDirectory)
		if gatewayErr != nil {
			t.Fatal(gatewayErr)
		}
		return httptest.NewServer(handler)
	}
	gateway := newGateway()
	defer func() { gateway.Close() }()
	grantClient, _ := api.NewHTTPClientWithClient(gateway.URL, grant.Value.AccessToken, gateway.Client())
	runFile := func(requestID string, call func() error, beforeSettle ...func(platform.RemoteWorkerSandboxFileCommandReceipt)) (string, platform.RemoteWorkerSandboxFileCommandReceipt, error) {
		done := make(chan error, 1)
		go func() { done <- call() }()
		var commandID string
		for retry := 0; ; retry++ {
			select {
			case callErr := <-done:
				t.Fatalf("RemoteWorker Files request completed before command %s: %v", requestID, callErr)
			default:
			}
			queryErr := owner.QueryRow(ctx, `SELECT command_uid FROM cloud_agents.remote_worker_sandbox_file_commands
WHERE tenant_id='tenant' AND project_uid='project' AND request_id=$1`, requestID).Scan(&commandID)
			if queryErr == nil {
				break
			}
			if queryErr != pgx.ErrNoRows || retry == 100 {
				t.Fatalf("queue RemoteWorker Files command %s: %v", requestID, queryErr)
			}
			time.Sleep(25 * time.Millisecond)
		}
		runRemoteWorkerHeartbeatProcess(t, ctx, binary, server, enrollmentID, incarnationID, certificateChain, privateKey, stateFile)
		rawState, readErr := os.ReadFile(stateFile)
		var fileState struct {
			SandboxFileCommandReceipt *platform.RemoteWorkerSandboxFileCommandReceipt `json:"sandboxFileCommandReceipt"`
		}
		if readErr != nil || json.Unmarshal(rawState, &fileState) != nil || fileState.SandboxFileCommandReceipt == nil ||
			fileState.SandboxFileCommandReceipt.CommandID != commandID {
			t.Fatalf("execute RemoteWorker Files command %s: state=%s err=%v", requestID, rawState, readErr)
		}
		receipt := *fileState.SandboxFileCommandReceipt
		if len(beforeSettle) != 0 {
			beforeSettle[0](receipt)
		}
		runRemoteWorkerHeartbeatProcess(t, ctx, binary, server, enrollmentID, incarnationID, certificateChain, privateKey, stateFile)
		select {
		case callErr := <-done:
			return commandID, receipt, callErr
		case <-time.After(15 * time.Second):
			t.Fatalf("RemoteWorker Files command %s did not settle", requestID)
			return "", platform.RemoteWorkerSandboxFileCommandReceipt{}, context.DeadlineExceeded
		}
	}
	settlePTY := func(requestID string, beforeSettle ...func(platform.RemoteWorkerSandboxPTYCommandReceipt)) (string, platform.RemoteWorkerSandboxPTYCommandReceipt) {
		var commandID string
		for retry := 0; ; retry++ {
			queryErr := owner.QueryRow(ctx, `SELECT command_uid FROM cloud_agents.remote_worker_sandbox_pty_commands
WHERE tenant_id='tenant' AND project_uid='project' AND request_id=$1 AND state IN ('pending','delivered')
ORDER BY created_at, command_uid LIMIT 1`, requestID).Scan(&commandID)
			if queryErr == nil {
				break
			}
			if queryErr != pgx.ErrNoRows || retry == 100 {
				t.Fatalf("queue RemoteWorker PTY command %s: %v", requestID, queryErr)
			}
			time.Sleep(25 * time.Millisecond)
		}
		runRemoteWorkerHeartbeatProcess(t, ctx, binary, server, enrollmentID, incarnationID, certificateChain, privateKey, stateFile)
		rawState, readErr := os.ReadFile(stateFile)
		var ptyState struct {
			SandboxPTYCommandReceipt *platform.RemoteWorkerSandboxPTYCommandReceipt `json:"sandboxPtyCommandReceipt"`
		}
		if readErr != nil || json.Unmarshal(rawState, &ptyState) != nil || ptyState.SandboxPTYCommandReceipt == nil ||
			ptyState.SandboxPTYCommandReceipt.CommandID != commandID {
			t.Fatalf("execute RemoteWorker PTY command %s: state=%s err=%v", requestID, rawState, readErr)
		}
		receipt := *ptyState.SandboxPTYCommandReceipt
		if len(beforeSettle) != 0 {
			beforeSettle[0](receipt)
		}
		runRemoteWorkerHeartbeatProcess(t, ctx, binary, server, enrollmentID, incarnationID, certificateChain, privateKey, stateFile)
		return commandID, receipt
	}
	runPTY := func(requestID string, call func() error, beforeSettle ...func(platform.RemoteWorkerSandboxPTYCommandReceipt)) (string, platform.RemoteWorkerSandboxPTYCommandReceipt, error) {
		done := make(chan error, 1)
		go func() { done <- call() }()
		commandID, receipt := settlePTY(requestID, beforeSettle...)
		select {
		case callErr := <-done:
			return commandID, receipt, callErr
		case <-time.After(15 * time.Second):
			t.Fatalf("RemoteWorker PTY command %s did not settle", requestID)
			return "", platform.RemoteWorkerSandboxPTYCommandReceipt{}, context.DeadlineExceeded
		}
	}
	type previewOutcome struct {
		status  int
		headers http.Header
		body    []byte
		err     error
	}
	runPreview := func(requestID, method, target, token string, body []byte, beforeSettle ...func(platform.RemoteWorkerSandboxPreviewCommandReceipt)) (string, platform.RemoteWorkerSandboxPreviewCommandReceipt, previewOutcome) {
		done := make(chan previewOutcome, 1)
		go func() {
			request, requestErr := http.NewRequestWithContext(ctx, method, gateway.URL+target, bytes.NewReader(body))
			if requestErr != nil {
				done <- previewOutcome{err: requestErr}
				return
			}
			request.Header.Set("Authorization", "Bearer "+token)
			request.Header.Set("Proxy-Authorization", "Basic must-not-reach-sandbox")
			request.Header.Set("Cookie", "private-cookie=must-not-reach-sandbox")
			request.Header.Set("Forwarded", "for=must-not-reach-sandbox")
			request.Header.Set("X-Forwarded-For", "must-not-reach-sandbox")
			request.Header.Set("X-Public", "visible")
			request.Header.Set("X-Request-ID", requestID)
			response, requestErr := gateway.Client().Do(request)
			if requestErr != nil {
				done <- previewOutcome{err: requestErr}
				return
			}
			defer response.Body.Close()
			responseBody, readErr := io.ReadAll(io.LimitReader(response.Body, (4<<20)+1))
			done <- previewOutcome{status: response.StatusCode, headers: response.Header.Clone(), body: responseBody, err: readErr}
		}()
		var commandID string
		for retry := 0; ; retry++ {
			select {
			case outcome := <-done:
				t.Fatalf("RemoteWorker Preview request completed before command %s: status=%d body=%q err=%v", requestID, outcome.status, outcome.body, outcome.err)
			default:
			}
			queryErr := owner.QueryRow(ctx, `SELECT command_uid FROM cloud_agents.remote_worker_sandbox_preview_commands
WHERE tenant_id='tenant' AND project_uid='project' AND request_id=$1`, requestID).Scan(&commandID)
			if queryErr == nil {
				break
			}
			if queryErr != pgx.ErrNoRows || retry == 100 {
				t.Fatalf("queue RemoteWorker Preview command %s: %v", requestID, queryErr)
			}
			time.Sleep(25 * time.Millisecond)
		}
		runRemoteWorkerHeartbeatProcess(t, ctx, binary, server, enrollmentID, incarnationID, certificateChain, privateKey, stateFile)
		rawState, readErr := os.ReadFile(stateFile)
		var previewState struct {
			SandboxPreviewCommandReceipt *platform.RemoteWorkerSandboxPreviewCommandReceipt `json:"sandboxPreviewCommandReceipt"`
		}
		if readErr != nil || json.Unmarshal(rawState, &previewState) != nil || previewState.SandboxPreviewCommandReceipt == nil || previewState.SandboxPreviewCommandReceipt.CommandID != commandID {
			t.Fatalf("execute RemoteWorker Preview command %s: state=%s err=%v", requestID, rawState, readErr)
		}
		receipt := *previewState.SandboxPreviewCommandReceipt
		if len(beforeSettle) != 0 {
			beforeSettle[0](receipt)
		}
		runRemoteWorkerHeartbeatProcess(t, ctx, binary, server, enrollmentID, incarnationID, certificateChain, privateKey, stateFile)
		select {
		case outcome := <-done:
			return commandID, receipt, outcome
		case <-time.After(15 * time.Second):
			t.Fatalf("RemoteWorker Preview command %s did not settle", requestID)
			return "", platform.RemoteWorkerSandboxPreviewCommandReceipt{}, previewOutcome{err: context.DeadlineExceeded}
		}
	}
	preview, err := grantClient.RegisterSandboxPreviewPort(ctx, "tenant", "project", grant.Value.GrantID, "request-remote-preview-register", 3000)
	if err != nil || preview.Value.Port != 3000 || !strings.HasSuffix(preview.Value.ProxyPath, "/preview-ports/3000/proxy") {
		t.Fatalf("register RemoteWorker Preview: value=%+v err=%v", preview.Value, err)
	}
	previewCommandID, previewReceipt, previewResult := runPreview("request-remote-preview-post", http.MethodPost,
		preview.Value.ProxyPath+"/hello?value=alpha", grant.Value.AccessToken, []byte("preview-request-body"),
		func(receipt platform.RemoteWorkerSandboxPreviewCommandReceipt) {
			conflictingReceipt := receipt
			conflictingReceipt.GrantID = "other-grant"
			heartbeat.SandboxPreviewCommandReceipt = &conflictingReceipt
			if _, callErr := node.HeartbeatRemoteWorker(ctx, "tenant", "project", enrollmentID, "request-remote-preview-authority-conflict", heartbeat); clientStatus(callErr) != http.StatusConflict {
				t.Fatalf("mismatched RemoteWorker Preview receipt status=%d err=%v", clientStatus(callErr), callErr)
			}
			heartbeat.SandboxPreviewCommandReceipt = nil
		})
	var previewPayload map[string]string
	if json.Unmarshal(previewResult.body, &previewPayload) != nil || previewResult.err != nil || previewResult.status != http.StatusOK ||
		previewPayload["method"] != http.MethodPost || previewPayload["url"] != "/hello?value=alpha" ||
		previewPayload["body"] != "preview-request-body" || previewPayload["public"] != "visible" ||
		previewPayload["authorization"] != "" || previewPayload["proxyAuthorization"] != "" || previewPayload["cookie"] != "" ||
		previewPayload["apiKey"] != "" || strings.Contains(previewPayload["forwarded"], "must-not-reach") ||
		previewResult.headers.Get("Set-Cookie") != "" || previewResult.headers.Get("Cache-Control") != "private, no-store" {
		t.Fatalf("RemoteWorker Preview post: command=%s receipt=%+v status=%d headers=%v payload=%v body=%q err=%v",
			previewCommandID, previewReceipt, previewResult.status, previewResult.headers, previewPayload, previewResult.body, previewResult.err)
	}
	heartbeat.SandboxPreviewCommandReceipt = &previewReceipt
	if _, err := node.HeartbeatRemoteWorker(ctx, "tenant", "project", enrollmentID, "request-remote-preview-receipt-replay", heartbeat); err != nil {
		t.Fatalf("replay RemoteWorker Preview receipt: %v", err)
	}
	conflictingPreviewReceipt := previewReceipt
	conflictingPreviewStatus := *previewReceipt.StatusCode + 1
	conflictingPreviewReceipt.StatusCode = &conflictingPreviewStatus
	heartbeat.SandboxPreviewCommandReceipt = &conflictingPreviewReceipt
	if _, err := node.HeartbeatRemoteWorker(ctx, "tenant", "project", enrollmentID, "request-remote-preview-receipt-conflict", heartbeat); clientStatus(err) != http.StatusConflict {
		t.Fatalf("conflicting RemoteWorker Preview receipt status=%d err=%v", clientStatus(err), err)
	}
	heartbeat.SandboxPreviewCommandReceipt = nil
	previewStatus := func(requestID, target, token string) int {
		request, requestErr := http.NewRequestWithContext(ctx, http.MethodGet, gateway.URL+target, http.NoBody)
		if requestErr != nil {
			t.Fatal(requestErr)
		}
		request.Header.Set("Authorization", "Bearer "+token)
		request.Header.Set("X-Request-ID", requestID)
		response, requestErr := gateway.Client().Do(request)
		if requestErr != nil {
			t.Fatal(requestErr)
		}
		defer response.Body.Close()
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 1<<20))
		return response.StatusCode
	}
	if status := previewStatus("request-remote-preview-wrong-token", preview.Value.ProxyPath, "cag1_"+strings.Repeat("x", 43)); status != http.StatusForbidden {
		t.Fatalf("wrong RemoteWorker Preview token status=%d", status)
	}
	crossTenantPreviewPath := strings.Replace(preview.Value.ProxyPath, "/tenants/tenant/", "/tenants/other-tenant/", 1)
	if status := previewStatus("request-remote-preview-cross-tenant", crossTenantPreviewPath, grant.Value.AccessToken); status != http.StatusForbidden {
		t.Fatalf("cross-tenant RemoteWorker Preview status=%d", status)
	}
	unregisteredPreviewPath := strings.Replace(preview.Value.ProxyPath, "/preview-ports/3000/", "/preview-ports/3001/", 1)
	if status := previewStatus("request-remote-preview-unregistered", unregisteredPreviewPath, grant.Value.AccessToken); status != http.StatusNotFound {
		t.Fatalf("unregistered RemoteWorker Preview status=%d", status)
	}
	internalPreviewPath := strings.Replace(preview.Value.ProxyPath, "/preview-ports/3000/", "/preview-ports/44772/", 1)
	if status := previewStatus("request-remote-preview-internal", internalPreviewPath, grant.Value.AccessToken); status != http.StatusNotFound {
		t.Fatalf("internal RemoteWorker Preview status=%d", status)
	}
	gateway.Close()
	gateway = newGateway()
	grantClient, _ = api.NewHTTPClientWithClient(gateway.URL, grant.Value.AccessToken, gateway.Client())
	_, _, previewRestart := runPreview("request-remote-preview-restart", http.MethodGet, preview.Value.ProxyPath+"/after-restart", grant.Value.AccessToken, nil)
	if previewRestart.err != nil || previewRestart.status != http.StatusOK {
		t.Fatalf("RemoteWorker Preview Gateway restart status=%d body=%q err=%v", previewRestart.status, previewRestart.body, previewRestart.err)
	}
	previewGrantPage, err := admin.ListAdminSandboxAccessGrants(ctx, "tenant", "project", "remote-sandbox", "request-remote-preview-admin", 50, "")
	if err != nil || len(previewGrantPage.Value.AccessGrants) != 1 || len(previewGrantPage.Value.AccessGrants[0].Spec.PreviewPorts) != 1 || previewGrantPage.Value.AccessGrants[0].Spec.PreviewPorts[0] != 3000 {
		t.Fatalf("RemoteWorker Preview Admin metadata: value=%+v err=%v", previewGrantPage.Value, err)
	}
	adminPreviewJSON, _ := json.Marshal(previewGrantPage.Value)
	for _, forbidden := range []string{"preview-request-body", "/hello", "x-public", "bodyBase64Url"} {
		if strings.Contains(string(adminPreviewJSON), forbidden) {
			t.Fatalf("RemoteWorker Preview Admin metadata disclosed %q", forbidden)
		}
	}
	var previewCommands, succeededPreviews int
	var previewIncarnationBound, previewCertificateBound, previewReceiptsSettled, previewContentTableHidden bool
	if err := owner.QueryRow(ctx, `SELECT count(*), count(*) FILTER (WHERE state='succeeded'),
bool_and(assigned_incarnation_uid=$1), bool_and(delivery_certificate_sha256=$2),
bool_and(receipt_digest IS NOT NULL),
NOT pg_catalog.has_table_privilege('cloud_agents_runtime', 'cloud_agents.remote_worker_sandbox_preview_commands', 'SELECT')
FROM cloud_agents.remote_worker_sandbox_preview_commands
WHERE tenant_id='tenant' AND project_uid='project'`, incarnationID, certificateSHA256).Scan(
		&previewCommands, &succeededPreviews, &previewIncarnationBound, &previewCertificateBound,
		&previewReceiptsSettled, &previewContentTableHidden,
	); err != nil || previewCommands != 2 || succeededPreviews != previewCommands || !previewIncarnationBound ||
		!previewCertificateBound || !previewReceiptsSettled || !previewContentTableHidden {
		t.Fatalf("RemoteWorker Preview authority: commands=%d succeeded=%d incarnation=%v certificate=%v receipts=%v hidden=%v err=%v",
			previewCommands, succeededPreviews, previewIncarnationBound, previewCertificateBound, previewReceiptsSettled, previewContentTableHidden, err)
	}
	if err := grantClient.RevokeSandboxPreviewPort(ctx, "tenant", "project", grant.Value.GrantID, "request-remote-preview-revoke", 3000); err != nil {
		t.Fatalf("revoke RemoteWorker Preview: %v", err)
	}
	if status := previewStatus("request-remote-preview-after-revoke", preview.Value.ProxyPath, grant.Value.AccessToken); status != http.StatusNotFound {
		t.Fatalf("revoked RemoteWorker Preview status=%d", status)
	}

	fileContent := []byte(strings.Repeat("remote-files-proof-", 100000))
	var written api.SandboxFileEntryResult
	writeCommandID, writeReceipt, err := runFile("request-remote-file-write", func() error {
		var callErr error
		written, callErr = grantClient.WriteSandboxFile(ctx, "tenant", "project", grant.Value.GrantID, "request-remote-file-write", platform.SandboxFileWriteRequest{
			Path: "remote-files-large.bin", ContentBase64URL: base64.RawURLEncoding.EncodeToString(fileContent),
		})
		return callErr
	}, func(receipt platform.RemoteWorkerSandboxFileCommandReceipt) {
		conflictingReceipt := receipt
		conflictingWrite := *receipt.Write
		conflictingWrite.Entry.Path = "remote-files-other.bin"
		conflictingReceipt.Write = &conflictingWrite
		heartbeat.SandboxFileCommandReceipt = &conflictingReceipt
		if _, callErr := node.HeartbeatRemoteWorker(ctx, "tenant", "project", enrollmentID, "request-remote-file-authority-conflict", heartbeat); clientStatus(callErr) != http.StatusConflict {
			t.Fatalf("mismatched RemoteWorker file receipt status=%d err=%v", clientStatus(callErr), callErr)
		}
		heartbeat.SandboxFileCommandReceipt = nil
	})
	if err != nil || written.Value.Path != "remote-files-large.bin" || written.Value.SizeBytes != int64(len(fileContent)) ||
		writeReceipt.BytesTransferred != int64(len(fileContent)) {
		t.Fatalf("write RemoteWorker file: command=%s value=%+v receipt=%+v err=%v", writeCommandID, written.Value, writeReceipt, err)
	}
	var files api.SandboxFilePageResult
	_, _, err = runFile("request-remote-file-list", func() error {
		var callErr error
		files, callErr = grantClient.ListSandboxFiles(ctx, "tenant", "project", grant.Value.GrantID, "request-remote-file-list", ".")
		return callErr
	})
	foundFile, foundLink := false, false
	for _, entry := range files.Value.Entries {
		foundFile = foundFile || entry.Path == "remote-files-large.bin" && entry.Type == "file"
		foundLink = foundLink || entry.Path == "remote-files-link" && entry.Type == "symlink"
	}
	if err != nil || !foundFile || !foundLink {
		t.Fatalf("list RemoteWorker files: entries=%+v err=%v", files.Value.Entries, err)
	}
	var firstPage api.SandboxFileReadPageResult
	_, _, err = runFile("request-remote-file-read-first", func() error {
		var callErr error
		firstPage, callErr = grantClient.ReadSandboxFile(ctx, "tenant", "project", grant.Value.GrantID, "request-remote-file-read-first", "remote-files-large.bin", 0, 900000, "")
		return callErr
	})
	firstContent, decodeErr := base64.RawURLEncoding.Strict().DecodeString(firstPage.Value.ContentBase64URL)
	if err != nil || decodeErr != nil || len(firstContent) != 900000 || firstPage.Value.EOF {
		t.Fatalf("read first RemoteWorker file page: value=%+v err=%v decode=%v", firstPage.Value, err, decodeErr)
	}
	gateway.Close()
	gateway = newGateway()
	grantClient, _ = api.NewHTTPClientWithClient(gateway.URL, grant.Value.AccessToken, gateway.Client())
	var secondPage api.SandboxFileReadPageResult
	_, _, err = runFile("request-remote-file-read-second", func() error {
		var callErr error
		secondPage, callErr = grantClient.ReadSandboxFile(ctx, "tenant", "project", grant.Value.GrantID, "request-remote-file-read-second", "remote-files-large.bin", firstPage.Value.NextOffset, 1<<20, firstPage.Value.FileVersion)
		return callErr
	})
	secondContent, decodeErr := base64.RawURLEncoding.Strict().DecodeString(secondPage.Value.ContentBase64URL)
	if err != nil || decodeErr != nil || !secondPage.Value.EOF || string(append(firstContent, secondContent...)) != string(fileContent) {
		t.Fatalf("read RemoteWorker file after Gateway restart: value=%+v err=%v decode=%v", secondPage.Value, err, decodeErr)
	}
	wrongClient, _ := api.NewHTTPClientWithClient(gateway.URL, "cag1_"+strings.Repeat("x", 43), gateway.Client())
	if _, err := wrongClient.ListSandboxFiles(ctx, "tenant", "project", grant.Value.GrantID, "request-remote-file-wrong-token", "."); clientStatus(err) != http.StatusForbidden {
		t.Fatalf("wrong RemoteWorker file Grant status=%d err=%v", clientStatus(err), err)
	}
	if _, err := grantClient.ListSandboxFiles(ctx, "other-tenant", "project", grant.Value.GrantID, "request-remote-file-cross-tenant", "."); clientStatus(err) != http.StatusForbidden {
		t.Fatalf("cross-tenant RemoteWorker file Grant status=%d err=%v", clientStatus(err), err)
	}
	_, _, symlinkErr := runFile("request-remote-file-symlink", func() error {
		_, callErr := grantClient.ReadSandboxFile(ctx, "tenant", "project", grant.Value.GrantID, "request-remote-file-symlink", "remote-files-link/passwd", 0, 1, "")
		return callErr
	})
	if clientStatus(symlinkErr) != http.StatusConflict {
		t.Fatalf("RemoteWorker symlink traversal status=%d err=%v", clientStatus(symlinkErr), symlinkErr)
	}
	_, _, err = runFile("request-remote-file-delete", func() error {
		return grantClient.DeleteSandboxFile(ctx, "tenant", "project", grant.Value.GrantID, "request-remote-file-delete", "remote-files-large.bin")
	})
	if err != nil {
		t.Fatalf("delete RemoteWorker file: %v", err)
	}
	_, _, deletedErr := runFile("request-remote-file-after-delete", func() error {
		_, callErr := grantClient.ReadSandboxFile(ctx, "tenant", "project", grant.Value.GrantID, "request-remote-file-after-delete", "remote-files-large.bin", 0, 1, "")
		return callErr
	})
	if clientStatus(deletedErr) != http.StatusNotFound {
		t.Fatalf("deleted RemoteWorker file status=%d err=%v", clientStatus(deletedErr), deletedErr)
	}

	heartbeat.SandboxFileCommandReceipt = &writeReceipt
	if _, err := node.HeartbeatRemoteWorker(ctx, "tenant", "project", enrollmentID, "request-remote-file-receipt-replay", heartbeat); err != nil {
		t.Fatalf("replay RemoteWorker file receipt: %v", err)
	}
	conflictingFileReceipt := writeReceipt
	conflictingWriteResult := *writeReceipt.Write
	conflictingWriteResult.Entry.FileVersion = "sfv1_" + strings.Repeat("A", 43)
	conflictingFileReceipt.Write = &conflictingWriteResult
	heartbeat.SandboxFileCommandReceipt = &conflictingFileReceipt
	if _, err := node.HeartbeatRemoteWorker(ctx, "tenant", "project", enrollmentID, "request-remote-file-receipt-conflict", heartbeat); clientStatus(err) != http.StatusConflict {
		t.Fatalf("conflicting RemoteWorker file receipt status=%d err=%v", clientStatus(err), err)
	}
	heartbeat.SandboxFileCommandReceipt = nil
	if _, err := user.ListAdminSandboxAccessGrants(ctx, "tenant", "project", "remote-sandbox", "request-remote-file-user-admin", 50, ""); clientStatus(err) != http.StatusForbidden {
		t.Fatalf("ordinary user RemoteWorker Admin Grant status=%d err=%v", clientStatus(err), err)
	}
	grantPage, err := admin.ListAdminSandboxAccessGrants(ctx, "tenant", "project", "remote-sandbox", "request-remote-file-admin", 50, "")
	if err != nil || len(grantPage.Value.AccessGrants) != 1 || grantPage.Value.AccessGrants[0].Spec.FileAccessCount != 7 ||
		grantPage.Value.AccessGrants[0].Spec.FileFailureCount != 2 || grantPage.Value.AccessGrants[0].Spec.LastFileAction != "read" ||
		grantPage.Value.AccessGrants[0].Spec.LastFileStatus != "failed" || grantPage.Value.AccessGrants[0].Spec.LastFileErrorCode != "NOT_FOUND" {
		t.Fatalf("RemoteWorker Admin Grant metadata: value=%+v err=%v", grantPage.Value, err)
	}
	adminGrantJSON, _ := json.Marshal(grantPage.Value)
	for _, forbidden := range []string{"remote-files-large.bin", "remote-files-link", "remote-files-proof-", "contentBase64Url"} {
		if strings.Contains(string(adminGrantJSON), forbidden) {
			t.Fatalf("RemoteWorker Admin Grant disclosed %q", forbidden)
		}
	}
	var fileCommands, succeededFiles, failedFiles int
	var fileIncarnationBound, fileCertificateBound, fileReceiptsSettled, fileContentTableHidden bool
	if err := owner.QueryRow(ctx, `SELECT count(*), count(*) FILTER (WHERE state='succeeded'),
count(*) FILTER (WHERE state='failed'), bool_and(assigned_incarnation_uid=$1),
bool_and(delivery_certificate_sha256=$2), bool_and(receipt_digest IS NOT NULL),
NOT pg_catalog.has_table_privilege('cloud_agents_runtime', 'cloud_agents.remote_worker_sandbox_file_commands', 'SELECT')
FROM cloud_agents.remote_worker_sandbox_file_commands
WHERE tenant_id='tenant' AND project_uid='project'`, incarnationID, certificateSHA256).Scan(
		&fileCommands, &succeededFiles, &failedFiles, &fileIncarnationBound, &fileCertificateBound,
		&fileReceiptsSettled, &fileContentTableHidden,
	); err != nil || fileCommands != 7 || succeededFiles != 5 || failedFiles != 2 || !fileIncarnationBound ||
		!fileCertificateBound || !fileReceiptsSettled || !fileContentTableHidden {
		t.Fatalf("RemoteWorker Files authority: commands=%d succeeded=%d failed=%d incarnation=%v certificate=%v receipts=%v hidden=%v err=%v",
			fileCommands, succeededFiles, failedFiles, fileIncarnationBound, fileCertificateBound, fileReceiptsSettled, fileContentTableHidden, err)
	}

	if _, err := wrongClient.CreatePTYSession(ctx, "tenant", "project", grant.Value.GrantID, "request-remote-pty-wrong-token"); clientStatus(err) != http.StatusForbidden {
		t.Fatalf("wrong RemoteWorker PTY Grant status=%d err=%v", clientStatus(err), err)
	}
	if _, err := grantClient.CreatePTYSession(ctx, "other-tenant", "project", grant.Value.GrantID, "request-remote-pty-cross-tenant"); clientStatus(err) != http.StatusForbidden {
		t.Fatalf("cross-tenant RemoteWorker PTY Grant status=%d err=%v", clientStatus(err), err)
	}
	var ptySession api.SandboxPTYSessionResult
	ptyCreateCommandID, ptyCreateReceipt, err := runPTY("request-remote-pty-create", func() error {
		var callErr error
		ptySession, callErr = grantClient.CreatePTYSession(ctx, "tenant", "project", grant.Value.GrantID, "request-remote-pty-create")
		return callErr
	}, func(receipt platform.RemoteWorkerSandboxPTYCommandReceipt) {
		conflictingReceipt := receipt
		conflictingReceipt.GrantID = "other-grant"
		heartbeat.SandboxPTYCommandReceipt = &conflictingReceipt
		if _, callErr := node.HeartbeatRemoteWorker(ctx, "tenant", "project", enrollmentID, "request-remote-pty-authority-conflict", heartbeat); clientStatus(callErr) != http.StatusConflict {
			t.Fatalf("mismatched RemoteWorker PTY receipt status=%d err=%v", clientStatus(callErr), callErr)
		}
		heartbeat.SandboxPTYCommandReceipt = nil
	})
	if err != nil || ptySession.Value.SessionID == "" || ptySession.Value.SessionID != ptyCreateReceipt.SessionID ||
		ptySession.Value.State != "created" || !strings.HasSuffix(ptySession.Value.WebSocketPath, "/ws") {
		t.Fatalf("create RemoteWorker PTY: command=%s value=%+v receipt=%+v err=%v", ptyCreateCommandID, ptySession.Value, ptyCreateReceipt, err)
	}
	heartbeat.SandboxPTYCommandReceipt = &ptyCreateReceipt
	if _, err := node.HeartbeatRemoteWorker(ctx, "tenant", "project", enrollmentID, "request-remote-pty-receipt-replay", heartbeat); err != nil {
		t.Fatalf("replay RemoteWorker PTY receipt: %v", err)
	}
	conflictingPTYReceipt := ptyCreateReceipt
	conflictingPTYReceipt.SessionID = "other-session"
	heartbeat.SandboxPTYCommandReceipt = &conflictingPTYReceipt
	if _, err := node.HeartbeatRemoteWorker(ctx, "tenant", "project", enrollmentID, "request-remote-pty-receipt-conflict", heartbeat); clientStatus(err) != http.StatusConflict {
		t.Fatalf("conflicting RemoteWorker PTY receipt status=%d err=%v", clientStatus(err), err)
	}
	heartbeat.SandboxPTYCommandReceipt = nil

	connection := dialPTY(t, gateway.URL, ptySession.Value.WebSocketPath, grant.Value.AccessToken, 0, false)
	if err := connection.WriteMessage(websocket.BinaryMessage, append([]byte{0}, []byte("printf 'REMOTE_PTY_FIRST=%s\\n' \"$PWD\"\n")...)); err != nil {
		t.Fatal(err)
	}
	ptyExchangeCommandID, ptyExchangeReceipt := settlePTY("request-pty-websocket")
	livePTY := readPTYUntil(t, connection, "REMOTE_PTY_FIRST=/workspace")
	_ = connection.Close()
	if ptyExchangeReceipt.BytesTransferred == 0 || livePTY.outputBytes == 0 {
		t.Fatalf("RemoteWorker PTY first exchange: command=%s receipt=%+v read=%+v", ptyExchangeCommandID, ptyExchangeReceipt, livePTY)
	}
	var observedPTY api.SandboxPTYSessionResult
	_, _, err = runPTY("request-remote-pty-observe", func() error {
		var callErr error
		observedPTY, callErr = grantClient.GetPTYSession(ctx, "tenant", "project", grant.Value.GrantID, ptySession.Value.SessionID, "request-remote-pty-observe")
		return callErr
	})
	if err != nil || observedPTY.Value.OutputOffset <= 0 {
		t.Fatalf("observe RemoteWorker PTY: value=%+v err=%v", observedPTY.Value, err)
	}

	gateway.Close()
	gateway = newGateway()
	grantClient, _ = api.NewHTTPClientWithClient(gateway.URL, grant.Value.AccessToken, gateway.Client())
	var persistedPTY api.SandboxPTYSessionResult
	_, _, err = runPTY("request-remote-pty-after-restart", func() error {
		var callErr error
		persistedPTY, callErr = grantClient.GetPTYSession(ctx, "tenant", "project", grant.Value.GrantID, ptySession.Value.SessionID, "request-remote-pty-after-restart")
		return callErr
	})
	if err != nil || persistedPTY.Value.OutputOffset != observedPTY.Value.OutputOffset {
		t.Fatalf("RemoteWorker PTY Gateway restart: value=%+v previous=%+v err=%v", persistedPTY.Value, observedPTY.Value, err)
	}
	connection = dialPTY(t, gateway.URL, persistedPTY.Value.WebSocketPath, grant.Value.AccessToken, 0, true)
	_, replayReceipt := settlePTY("request-pty-websocket")
	replayedPTY := readPTYUntil(t, connection, "REMOTE_PTY_FIRST=/workspace")
	if replayedPTY.replayOffset < 0 || replayReceipt.Frames == nil {
		t.Fatalf("RemoteWorker PTY cursor replay: receipt=%+v read=%+v", replayReceipt, replayedPTY)
	}
	if err := connection.WriteMessage(websocket.BinaryMessage, append([]byte{0}, []byte("printf 'REMOTE_PTY_SECOND\\n'\n")...)); err != nil {
		t.Fatal(err)
	}
	_, secondExchangeReceipt := settlePTY("request-pty-websocket")
	readPTYUntil(t, connection, "REMOTE_PTY_SECOND")
	_ = connection.Close()
	if secondExchangeReceipt.BytesTransferred == 0 {
		t.Fatalf("RemoteWorker PTY second exchange receipt=%+v", secondExchangeReceipt)
	}
	_, _, err = runPTY("request-remote-pty-delete", func() error {
		return grantClient.DeletePTYSession(ctx, "tenant", "project", grant.Value.GrantID, ptySession.Value.SessionID, "request-remote-pty-delete")
	})
	if err != nil {
		t.Fatalf("delete RemoteWorker PTY: %v", err)
	}
	if _, err := grantClient.GetPTYSession(ctx, "tenant", "project", grant.Value.GrantID, ptySession.Value.SessionID, "request-remote-pty-after-delete"); clientStatus(err) != http.StatusForbidden {
		t.Fatalf("deleted RemoteWorker PTY status=%d err=%v", clientStatus(err), err)
	}

	ptyGrantPage, err := admin.ListAdminSandboxAccessGrants(ctx, "tenant", "project", "remote-sandbox", "request-remote-pty-admin", 50, "")
	if err != nil || len(ptyGrantPage.Value.AccessGrants) != 1 || ptyGrantPage.Value.AccessGrants[0].Spec.PTYSessionCount != 1 {
		t.Fatalf("RemoteWorker PTY Admin metadata: value=%+v err=%v", ptyGrantPage.Value, err)
	}
	adminPTYJSON, _ := json.Marshal(ptyGrantPage.Value)
	for _, forbidden := range []string{"REMOTE_PTY_FIRST", "REMOTE_PTY_SECOND", ptySession.Value.SessionID, "payloadBase64Url"} {
		if strings.Contains(string(adminPTYJSON), forbidden) {
			t.Fatalf("RemoteWorker PTY Admin metadata disclosed %q", forbidden)
		}
	}
	var ptyCommands, succeededPTY int
	var ptyIncarnationBound, ptyCertificateBound, ptyReceiptsSettled, ptyContentTableHidden bool
	if err := owner.QueryRow(ctx, `SELECT count(*), count(*) FILTER (WHERE state='succeeded'),
bool_and(assigned_incarnation_uid=$1), bool_and(delivery_certificate_sha256=$2),
bool_and(receipt_digest IS NOT NULL),
NOT pg_catalog.has_table_privilege('cloud_agents_runtime', 'cloud_agents.remote_worker_sandbox_pty_commands', 'SELECT')
FROM cloud_agents.remote_worker_sandbox_pty_commands
WHERE tenant_id='tenant' AND project_uid='project'`, incarnationID, certificateSHA256).Scan(
		&ptyCommands, &succeededPTY, &ptyIncarnationBound, &ptyCertificateBound, &ptyReceiptsSettled, &ptyContentTableHidden,
	); err != nil || ptyCommands != 7 || succeededPTY != ptyCommands || !ptyIncarnationBound || !ptyCertificateBound ||
		!ptyReceiptsSettled || !ptyContentTableHidden {
		t.Fatalf("RemoteWorker PTY authority: commands=%d succeeded=%d incarnation=%v certificate=%v receipts=%v hidden=%v err=%v",
			ptyCommands, succeededPTY, ptyIncarnationBound, ptyCertificateBound, ptyReceiptsSettled, ptyContentTableHidden, err)
	}

	_, hostPrivateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	hostSigner, err := ssh.NewSignerFromKey(hostPrivateKey)
	if err != nil {
		t.Fatal(err)
	}
	sshAddress, stopSSH := startSSHGateway(t, ctx, gatewayStore, sandboxDirectory, hostSigner)
	if _, err := ssh.Dial("tcp", sshAddress, sshClientConfig(grant.Value.SSHUsername, "cag1_"+strings.Repeat("x", 43), hostSigner)); err == nil {
		t.Fatal("wrong RemoteWorker SSH Grant password was accepted")
	}
	if _, err := ssh.Dial("tcp", sshAddress, sshClientConfig("other-tenant:project:"+grant.Value.GrantID, grant.Value.AccessToken, hostSigner)); err == nil {
		t.Fatal("cross-tenant RemoteWorker SSH username was accepted")
	}
	heartbeat.Capabilities = []string{"docker", "exec", "files", "network-dns-nft", "preview", "pty", "workspace-volume"}
	if _, err := node.HeartbeatRemoteWorker(ctx, "tenant", "project", enrollmentID, "request-remote-ssh-without-capability", heartbeat); err != nil {
		t.Fatal("remove RemoteWorker SSH capability", err)
	}
	disabledClient, err := ssh.Dial("tcp", sshAddress, sshClientConfig(grant.Value.SSHUsername, grant.Value.AccessToken, hostSigner))
	if err != nil {
		t.Fatal("RemoteWorker SSH Grant authentication failed", err)
	}
	disabledSession, err := disabledClient.NewSession()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := disabledSession.CombinedOutput("printf REMOTE_SSH_CAPABILITY_BYPASS"); err == nil {
		t.Fatal("RemoteWorker SSH command was accepted without the node capability")
	}
	_ = disabledClient.Close()
	var rejectedSSHCommands int
	if err := owner.QueryRow(ctx, `SELECT count(*) FROM cloud_agents.remote_worker_sandbox_pty_commands WHERE ssh_session`).Scan(&rejectedSSHCommands); err != nil || rejectedSSHCommands != 0 {
		t.Fatalf("RemoteWorker SSH capability rejection queued commands=%d err=%v", rejectedSSHCommands, err)
	}
	heartbeat.Capabilities = []string{"docker", "exec", "files", "network-dns-nft", "preview", "pty", "ssh", "workspace-volume"}
	if _, err := node.HeartbeatRemoteWorker(ctx, "tenant", "project", enrollmentID, "request-remote-ssh-capability-restore", heartbeat); err != nil {
		t.Fatal("restore RemoteWorker SSH capability", err)
	}
	stopRemoteWorker := startRemoteWorkerProcess(t, ctx, binary, server, enrollmentID, incarnationID, certificateChain, privateKey, stateFile)
	sshClient, err := ssh.Dial("tcp", sshAddress, sshClientConfig(grant.Value.SSHUsername, grant.Value.AccessToken, hostSigner))
	if err != nil {
		t.Fatal("RemoteWorker SSH Grant authentication failed", err)
	}
	if forwarded, err := sshClient.Dial("tcp", "127.0.0.1:22"); err == nil {
		_ = forwarded.Close()
		t.Fatal("RemoteWorker SSH direct-tcpip forwarding was accepted")
	}
	sshSession, err := sshClient.NewSession()
	if err != nil {
		t.Fatal(err)
	}
	if err := sshSession.Setenv("CAG_SHOULD_NOT_EXIST", "true"); err == nil {
		t.Fatal("RemoteWorker SSH environment mutation was accepted")
	}
	if err := sshSession.RequestPty("xterm-256color", 40, 120, ssh.TerminalModes{}); err != nil {
		t.Fatal("RemoteWorker SSH PTY request failed", err)
	}
	sshInput, err := sshSession.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	sshOutputReader, err := sshSession.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := sshSession.Shell(); err != nil {
		t.Fatal("RemoteWorker SSH shell failed", err)
	}
	sshOutputDone := make(chan []byte, 1)
	go func() {
		output, _ := io.ReadAll(io.LimitReader(sshOutputReader, 1<<20))
		sshOutputDone <- output
	}()
	_, _ = io.WriteString(sshInput, "printf 'REMOTE_SSH_DIR=%s\\n' \"$PWD\"; exit 7\n")
	sshErr := sshSession.Wait()
	sshOutput := <-sshOutputDone
	_ = sshClient.Close()
	exitError, exited := sshErr.(*ssh.ExitError)
	if !exited || exitError.ExitStatus() != 7 || !strings.Contains(string(sshOutput), "REMOTE_SSH_DIR=/workspace") {
		t.Fatalf("RemoteWorker SSH output=%q err=%v", sshOutput, sshErr)
	}
	stopSSH()
	sshAddress, stopSSH = startSSHGateway(t, ctx, gatewayStore, sandboxDirectory, hostSigner)
	defer stopSSH()
	sshClient, err = ssh.Dial("tcp", sshAddress, sshClientConfig(grant.Value.SSHUsername, grant.Value.AccessToken, hostSigner))
	if err != nil {
		t.Fatal("RemoteWorker SSH Grant did not survive Gateway restart", err)
	}
	sshSession, err = sshClient.NewSession()
	if err != nil {
		t.Fatal(err)
	}
	sshCommand := `printf 'REMOTE_SSH_RESTART=%s\n' "$PWD"; printf 'REMOTE_SSH_ERR\n' >&2`
	sshOutput, err = sshSession.CombinedOutput(sshCommand)
	_ = sshClient.Close()
	if err != nil || !strings.Contains(string(sshOutput), "REMOTE_SSH_RESTART=/workspace") || !strings.Contains(string(sshOutput), "REMOTE_SSH_ERR") {
		t.Fatalf("RemoteWorker SSH after Gateway restart output=%q err=%v", sshOutput, err)
	}
	stopRemoteWorker()
	sshGrantPage, err := admin.ListAdminSandboxAccessGrants(ctx, "tenant", "project", "remote-sandbox", "request-remote-ssh-admin", 50, "")
	if err != nil || len(sshGrantPage.Value.AccessGrants) != 1 || sshGrantPage.Value.AccessGrants[0].Spec.PTYSessionCount != 3 {
		t.Fatalf("RemoteWorker SSH Admin metadata: value=%+v err=%v", sshGrantPage.Value, err)
	}
	adminSSHJSON, _ := json.Marshal(sshGrantPage.Value)
	for _, forbidden := range []string{"REMOTE_SSH_DIR", "REMOTE_SSH_ERR", "REMOTE_SSH_RESTART", "payloadBase64Url", "providerCredentialRef", "credentialRef"} {
		if strings.Contains(string(adminSSHJSON), forbidden) {
			t.Fatalf("RemoteWorker SSH Admin metadata disclosed %q", forbidden)
		}
	}
	var sshCommands, succeededSSH, sshInputCommands int
	var sshIncarnationBound, sshCertificateBound, sshReceiptsSettled, sshShapeBound, sshPTYBound, sshNonPTYBound, sshContentTableHidden bool
	if err := owner.QueryRow(ctx, `SELECT count(*), count(*) FILTER (WHERE state='succeeded'),
count(*) FILTER (WHERE action='exchange' AND input_message_type='binary' AND pg_catalog.octet_length(input_payload) > 1),
bool_and(assigned_incarnation_uid=$1), bool_and(delivery_certificate_sha256=$2),
bool_and(receipt_digest IS NOT NULL),
bool_and((action='create' AND pty_enabled IS NULL)
 OR (action='exchange' AND pty_enabled IS NOT NULL)
 OR (action='delete' AND pty_enabled IS NULL)),
bool_or(action='exchange' AND pty_enabled),
bool_or(action='exchange' AND NOT pty_enabled),
NOT pg_catalog.has_table_privilege('cloud_agents_runtime', 'cloud_agents.remote_worker_sandbox_pty_commands', 'SELECT')
FROM cloud_agents.remote_worker_sandbox_pty_commands
WHERE tenant_id='tenant' AND project_uid='project' AND ssh_session`, incarnationID, certificateSHA256).Scan(
		&sshCommands, &succeededSSH, &sshInputCommands, &sshIncarnationBound, &sshCertificateBound,
		&sshReceiptsSettled, &sshShapeBound, &sshPTYBound, &sshNonPTYBound, &sshContentTableHidden,
	); err != nil || sshCommands < 6 || succeededSSH != sshCommands || sshInputCommands < 2 || !sshIncarnationBound ||
		!sshCertificateBound || !sshReceiptsSettled || !sshShapeBound || !sshPTYBound || !sshNonPTYBound || !sshContentTableHidden {
		t.Fatalf("RemoteWorker SSH authority: commands=%d succeeded=%d inputs=%d incarnation=%v certificate=%v receipts=%v shape=%v pty=%v nonpty=%v hidden=%v err=%v",
			sshCommands, succeededSSH, sshInputCommands, sshIncarnationBound, sshCertificateBound, sshReceiptsSettled, sshShapeBound, sshPTYBound, sshNonPTYBound, sshContentTableHidden, err)
	}
	cleanupStopOperation, err := admin.StopAdminSandboxSession(ctx, "tenant", "project", "remote-sandbox", "request-remote-sandbox-cleanup-stop", "remote-sandbox-cleanup-stop-key-1", platform.SandboxSessionLifecycleRequest{
		ExpectedGeneration: adminSandbox.Value.Spec.Generation, ExpectedResourceVersion: adminSandbox.Value.Metadata.ResourceVersion,
		ConfirmedSandboxID: "remote-sandbox", ComputeDisposition: "delete", WorkspaceDisposition: "retain",
	})
	if err != nil || cleanupStopOperation.Value.Action != "sandbox.stop" || cleanupStopOperation.Value.State != "pending" {
		t.Fatalf("accept rebuilt RemoteWorker Sandbox cleanup Stop: value=%+v err=%v", cleanupStopOperation.Value, err)
	}
	heartbeat.SandboxCommandReceipt = nil
	cleanupStopDelivery, err := node.HeartbeatRemoteWorker(ctx, "tenant", "project", enrollmentID, "request-remote-sandbox-cleanup-stop-delivery", heartbeat)
	if err != nil || cleanupStopDelivery.Value.SandboxCommand == nil {
		t.Fatalf("deliver rebuilt RemoteWorker Sandbox cleanup Stop: value=%+v err=%v", cleanupStopDelivery.Value, err)
	}
	cleanupStopCommand := cleanupStopDelivery.Value.SandboxCommand
	if cleanupStopCommand.Action != "sandbox.stop" || cleanupStopCommand.RuntimeID != rebuiltRuntimeID ||
		cleanupStopCommand.PhysicalVolumeName != volumeName || cleanupStopCommand.RuntimeGeneration != rebuildCommand.SandboxGeneration {
		t.Fatalf("rebuilt RemoteWorker Sandbox cleanup Stop authority drifted: value=%+v", cleanupStopCommand)
	}
	state, _ = json.Marshal(map[string]any{"incarnationId": incarnationID, "observedGeneration": heartbeat.ObservedGeneration,
		"observedState": heartbeat.ObservedState, "sandboxCommand": cleanupStopCommand})
	if err := os.WriteFile(stateFile, append(state, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
	runRemoteWorkerHeartbeatProcess(t, ctx, binary, server, enrollmentID, incarnationID, certificateChain, privateKey, stateFile)
	adminSandbox, err = admin.GetAdminSandboxSession(ctx, "tenant", "project", "remote-sandbox", "request-remote-sandbox-get-cleaned")
	if err != nil || adminSandbox.Value.Spec.TargetID != selectedTargetID || adminSandbox.Value.Spec.ObservedState != "stopped" || adminSandbox.Value.Spec.RuntimeID != "" ||
		adminSandbox.Value.Spec.PhysicalVolumeID != volumeName || !adminSandbox.Value.Spec.WriterReleased {
		t.Fatalf("rebuilt RemoteWorker Sandbox cleanup Stop settlement: value=%+v err=%v", adminSandbox.Value, err)
	}
	heartbeat.SandboxCommandReceipt = &platform.RemoteWorkerSandboxCommandReceipt{CommandID: cleanupStopCommand.CommandID,
		Attempt: cleanupStopCommand.Attempt, Action: cleanupStopCommand.Action, OperationID: cleanupStopCommand.OperationID,
		SandboxID: cleanupStopCommand.SandboxID, SandboxGeneration: cleanupStopCommand.SandboxGeneration,
		Result: "succeeded", VolumeName: volumeName, CleanupComplete: true}
	if _, err := node.HeartbeatRemoteWorker(ctx, "tenant", "project", enrollmentID, "request-remote-sandbox-cleanup-stop-replay", heartbeat); err != nil {
		t.Fatalf("replay rebuilt RemoteWorker Sandbox cleanup Stop receipt: %v", err)
	}
	var nodeAuditCount int
	var exactNodeAuditSubject bool
	expectedNodeAuditCount := 6 + int(command.Attempt) + int(rebuildCommand.Attempt)
	if err := owner.QueryRow(ctx, `SELECT count(*), bool_and(subject_digest = $2)
FROM cloud_agents.coordination_audit_facts
WHERE operation_id IN ($1, $3, $4, $5) AND transition IN ('sandbox.claim', 'outbox.delivery_succeeded')`,
		sandbox.Value.OperationID, certificateSHA256, stopOperation.Value.OperationID, rebuildOperation.Value.OperationID,
		cleanupStopOperation.Value.OperationID).Scan(&nodeAuditCount, &exactNodeAuditSubject); err != nil || nodeAuditCount != expectedNodeAuditCount || !exactNodeAuditSubject {
		t.Fatalf("RemoteWorker Sandbox audit authority: count=%d exact=%v err=%v", nodeAuditCount, exactNodeAuditSubject, err)
	}
	marker, _ := json.Marshal(map[string]any{"operationId": sandbox.Value.OperationID,
		"stopOperationId": stopOperation.Value.OperationID, "rebuildOperationId": rebuildOperation.Value.OperationID,
		"cleanupStopOperationId": cleanupStopOperation.Value.OperationID, "runtimeId": runtimeID,
		"rebuiltRuntimeId": rebuiltRuntimeID, "volumeName": volumeName, "workspaceDigest": workspaceDigest,
		"rebuildAttempts": rebuildCommand.Attempt, "execCommandId": execCommandID, "execExitCode": execResult.Value.ExitCode,
		"execWorkspaceDigestVerified": true, "execRequestReplay": true, "execRequestConflictStatus": 409,
		"execReceiptReplay": true, "execReceiptConflictStatus": 409, "execAdminStatus": 403, "execStaleGenerationStatus": 409,
		"execIncarnationBound": execIncarnationBound,
		"execCertificateBound": execCertificateBound, "execContentTableHidden": execContentTableHidden,
		"fileCommandId": writeCommandID, "fileBytes": len(fileContent), "fileLargeHeartbeatResponse": true,
		"fileGatewayRestart": true, "fileWrongTokenStatus": 403, "fileCrossTenantStatus": 403,
		"fileSymlinkStatus": 409, "fileAfterDeleteStatus": 404, "fileAuthorityMismatchStatus": 409, "fileReceiptReplay": true,
		"fileReceiptConflictStatus": 409, "fileAdminStatus": 403, "fileAccessCount": fileCommands,
		"fileFailureCount": failedFiles, "fileAdminRedacted": true, "fileIncarnationBound": fileIncarnationBound,
		"fileCertificateBound": fileCertificateBound, "fileReceiptsSettled": fileReceiptsSettled,
		"fileContentTableHidden": fileContentTableHidden,
		"previewCommandId":       previewCommandID, "previewCommandCount": previewCommands,
		"previewGatewayRestart": true, "previewWrongTokenStatus": 403, "previewCrossTenantStatus": 403,
		"previewUnregisteredStatus": 404, "previewInternalPortStatus": 404, "previewAfterRevokeStatus": 404,
		"previewAuthorityMismatchStatus": 409, "previewReceiptReplay": true, "previewReceiptConflictStatus": 409,
		"previewAdminRedacted": true, "previewIncarnationBound": previewIncarnationBound,
		"previewCertificateBound": previewCertificateBound, "previewReceiptsSettled": previewReceiptsSettled,
		"previewContentTableHidden": previewContentTableHidden,
		"ptyCreateCommandId":        ptyCreateCommandID, "ptyExchangeCommandId": ptyExchangeCommandID,
		"ptyOutputOffset": observedPTY.Value.OutputOffset, "ptyGatewayRestart": true, "ptyCursorReplay": true,
		"ptyWrongTokenStatus": 403, "ptyCrossTenantStatus": 403, "ptyAuthorityMismatchStatus": 409,
		"ptyReceiptReplay": true, "ptyReceiptConflictStatus": 409, "ptyAfterDeleteStatus": 403,
		"ptySessionCount": 1, "ptyAdminRedacted": true, "ptyCommandCount": ptyCommands,
		"ptyIncarnationBound": ptyIncarnationBound, "ptyCertificateBound": ptyCertificateBound,
		"ptyReceiptsSettled": ptyReceiptsSettled, "ptyContentTableHidden": ptyContentTableHidden,
		"sshExitCode": 7, "sshGatewayRestart": true, "sshWrongPassword": true, "sshCrossTenant": true,
		"sshCapabilityRejected": true, "sshDirectTCPIPRejected": true, "sshEnvironmentRejected": true,
		"sshCommandCount": sshCommands, "sshInputCommandCount": sshInputCommands, "sshAdminRedacted": true,
		"sshIncarnationBound": sshIncarnationBound, "sshCertificateBound": sshCertificateBound,
		"sshReceiptsSettled": sshReceiptsSettled, "sshShapeBound": sshShapeBound, "sshPTYBound": sshPTYBound,
		"sshNonPTYBound": sshNonPTYBound, "sshContentTableHidden": sshContentTableHidden,
		"longClaimRenewed": true, "renewWrongCommandStatus": 409, "reconnectOriginalCommandNotReplayed": true,
		"reconnectAttempt": command.Attempt, "receiptReplay": true, "stopReceiptReplay": true,
		"rebuildReceiptReplay": true, "cleanupStopReceiptReplay": true,
		"capabilityAdmissionCases": len(admissionCases), "incompatibleProfileHidden": true,
		"incompatibleCommandBlocked": true, "capacityReservationRejected": true,
		"regionId": "region-local", "resourcePoolId": "pool-remote-worker",
		"deterministicPlacement": true, "selectedTargetId": selectedTargetID,
		"otherNodeSkipped": true, "selectorArchitecture": "arm64", "selectorUnavailableRejected": true,
		"nodeAuditCount": nodeAuditCount, "observedState": adminSandbox.Value.Spec.ObservedState})
	fmt.Printf("REMOTE_WORKER_SANDBOX=%s\n", marker)
}

func runRemoteWorkerSharedUntrustedLive(t *testing.T, ctx context.Context, admin, user, node *api.Client,
	server *httptest.Server, binary, enrollmentID, incarnationID, certificateChain string, privateKey []byte,
	heartbeat platform.RemoteWorkerHeartbeatRequest, image, releaseDigest, targetID string,
) {
	t.Helper()
	policy, err := admin.SetAdminNetworkPolicy(ctx, "tenant", "project", "gvisor-deny", "request-gvisor-network", "gvisor-network-key-1", platform.NetworkPolicySetRequest{
		ExpectedResourceVersion: "0", PolicyName: "gvisor-deny", UserSummary: "Shared untrusted deny-only network",
		DefaultEgress: "deny", AllowedEgress: []string{}, IngressEnabled: false, PreviewEnabled: false,
	})
	if err != nil || policy.Value.Metadata.ResourceVersion != "1" {
		t.Fatalf("create gVisor network policy: value=%+v err=%v", policy.Value, err)
	}
	profile, err := admin.CreateAdminRuntimeProfile(ctx, "tenant", "project", "request-gvisor-profile", "gvisor-profile-key-1", platform.RuntimeProfileCreateRequest{
		ProfileID: "gvisor-shared", ProfileName: "gvisor-shared", Version: 1,
		Description: "Shared untrusted gVisor workspace", WorkloadTrust: "shared-untrusted", IsolationRuntime: "gvisor",
		TargetID: targetID, NetworkPolicyRef: "gvisor-deny", ImageURI: image, ReleaseDigest: releaseDigest,
		CPUMillis: 500, MemoryBytes: 536870912,
	})
	if err != nil || profile.Value.Spec.WorkloadTrust != "shared-untrusted" || profile.Value.Spec.IsolationRuntime != "gvisor" {
		t.Fatalf("create gVisor profile: value=%+v err=%v", profile.Value, err)
	}
	withoutGVisor := heartbeat
	withoutGVisor.Capabilities = make([]string, 0, len(heartbeat.Capabilities)-1)
	for _, capability := range heartbeat.Capabilities {
		if capability != "isolation-gvisor" {
			withoutGVisor.Capabilities = append(withoutGVisor.Capabilities, capability)
		}
	}
	if _, err := node.HeartbeatRemoteWorker(ctx, "tenant", "project", enrollmentID, "request-gvisor-capability-missing", withoutGVisor); err != nil {
		t.Fatalf("report node without gVisor capability: %v", err)
	}
	_, err = admin.PublishAdminRuntimeProfile(ctx, "tenant", "project", "gvisor-shared", 1, "request-gvisor-publish-blocked", "gvisor-publish-blocked-key", platform.RuntimeProfileTransitionRequest{ExpectedResourceVersion: "1"})
	capabilityRejected := clientStatus(err) == http.StatusConflict
	if !capabilityRejected {
		t.Fatalf("publish without gVisor capability: status=%d err=%v", clientStatus(err), err)
	}
	if _, err := node.HeartbeatRemoteWorker(ctx, "tenant", "project", enrollmentID, "request-gvisor-capability-restored", heartbeat); err != nil {
		t.Fatalf("restore gVisor capability: %v", err)
	}
	profile, err = admin.PublishAdminRuntimeProfile(ctx, "tenant", "project", "gvisor-shared", 1, "request-gvisor-publish", "gvisor-publish-key-1", platform.RuntimeProfileTransitionRequest{ExpectedResourceVersion: "1"})
	if err != nil || profile.Value.Spec.Status != "published" {
		t.Fatalf("publish gVisor profile: value=%+v err=%v", profile.Value, err)
	}
	publicProfiles, err := user.ListRuntimeProfiles(ctx, "tenant", "project", "request-gvisor-profile-list", 50, "")
	publicJSON, _ := json.Marshal(publicProfiles.Value)
	publicRedacted := err == nil && bytes.Contains(publicJSON, []byte(`"profileId":"gvisor-shared"`)) &&
		!bytes.Contains(publicJSON, []byte("workloadTrust")) && !bytes.Contains(publicJSON, []byte("isolationRuntime")) &&
		!bytes.Contains(publicJSON, []byte("targetId")) && !bytes.Contains(publicJSON, []byte("networkPolicyRef"))
	if !publicRedacted {
		t.Fatalf("public gVisor profile authority leaked: value=%s err=%v", publicJSON, err)
	}
	created, err := user.CreateSandbox(ctx, "tenant", "project", "request-gvisor-sandbox", "gvisor-sandbox-key-1", platform.SandboxSessionCreateRequest{
		WorkspaceID: "gvisor-workspace", WorkspaceName: "gvisor-workspace", SandboxID: "gvisor-sandbox",
		RuntimeProfileID: "gvisor-shared", RuntimeProfileVersion: 1, TTLSeconds: 120,
	})
	if err != nil || created.Value.ObservedState != "pending" {
		t.Fatalf("create gVisor Sandbox: value=%+v err=%v", created.Value, err)
	}
	delivery, err := node.HeartbeatRemoteWorker(ctx, "tenant", "project", enrollmentID, "request-gvisor-sandbox-delivery", heartbeat)
	if err != nil || delivery.Value.SandboxCommand == nil {
		t.Fatalf("deliver gVisor Sandbox command: value=%+v err=%v", delivery.Value, err)
	}
	command := delivery.Value.SandboxCommand
	if command.WorkloadTrust != "shared-untrusted" || command.IsolationRuntime != "gvisor" || len(command.NetworkAllowedEgress) != 0 {
		t.Fatalf("gVisor command authority drifted: value=%+v", command)
	}
	stateFile := filepath.Join(t.TempDir(), "remote-worker-gvisor-state.json")
	state, _ := json.Marshal(map[string]any{"incarnationId": incarnationID, "observedGeneration": heartbeat.ObservedGeneration,
		"observedState": heartbeat.ObservedState, "sandboxCommand": command})
	if err := os.WriteFile(stateFile, append(state, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
	runRemoteWorkerHeartbeatProcess(t, ctx, binary, server, enrollmentID, incarnationID, certificateChain, privateKey, stateFile)
	adminSandbox, err := admin.GetAdminSandboxSession(ctx, "tenant", "project", "gvisor-sandbox", "request-gvisor-sandbox-get")
	if err != nil || adminSandbox.Value.Spec.ObservedState != "running" || adminSandbox.Value.Spec.RuntimeID == "" ||
		adminSandbox.Value.Spec.WorkloadTrust != "shared-untrusted" || adminSandbox.Value.Spec.IsolationRuntime != "gvisor" {
		t.Fatalf("settle gVisor Sandbox: value=%+v err=%v", adminSandbox.Value, err)
	}
	dockerDirectory, err := dockertarget.NewCredentialDirectory(os.Getenv("CLOUD_AGENTS_REMOTE_WORKER_CREDENTIAL_DIRECTORY"))
	if err == nil {
		err = dockerDirectory.VerifyFoundationSandboxIsolation(ctx, os.Getenv("CLOUD_AGENTS_REMOTE_WORKER_DOCKER_ENDPOINT"), os.Getenv("CLOUD_AGENTS_REMOTE_WORKER_CREDENTIAL_REF"), adminSandbox.Value.Spec.RuntimeID)
	}
	if err != nil {
		t.Fatalf("verify physical gVisor isolation: %v", err)
	}
	sandboxDirectory, err := opensandbox.NewCredentialDirectory(os.Getenv("CLOUD_AGENTS_REMOTE_WORKER_CREDENTIAL_DIRECTORY"))
	if err != nil {
		t.Fatal(err)
	}
	runtimeClient, err := sandboxDirectory.Client(os.Getenv("CLOUD_AGENTS_REMOTE_WORKER_CREDENTIAL_REF"))
	if err != nil {
		t.Fatal(err)
	}
	identity := opensandbox.Identity{Tenant: "tenant", Project: "project", Workspace: command.WorkspaceID,
		Sandbox: command.SandboxID, Operation: command.OperationID, Generation: command.SandboxGeneration, SpecDigest: command.SpecDigest}
	probe, err := runtimeClient.Exec(ctx, opensandbox.ExecInput{Identity: identity, RuntimeID: adminSandbox.Value.Spec.RuntimeID,
		Command: `printf 'kernel='; uname -r; dmesg | head -20; node -e 'fetch("http://1.1.1.1",{signal:AbortSignal.timeout(1500)}).then(()=>process.exit(7)).catch(()=>console.log("egress=blocked"))'`, Timeout: 10 * time.Second})
	if err != nil || probe.ExitCode != 0 || !strings.Contains(probe.Stdout, "gVisor") || !strings.Contains(probe.Stdout, "egress=blocked") {
		t.Fatalf("gVisor runtime probe: result=%+v err=%v", probe, err)
	}
	runtimeID, volumeName := adminSandbox.Value.Spec.RuntimeID, adminSandbox.Value.Spec.PhysicalVolumeID
	stop, err := admin.StopAdminSandboxSession(ctx, "tenant", "project", "gvisor-sandbox", "request-gvisor-sandbox-stop", "gvisor-sandbox-stop-key-1", platform.SandboxSessionLifecycleRequest{
		ExpectedGeneration: adminSandbox.Value.Spec.Generation, ExpectedResourceVersion: adminSandbox.Value.Metadata.ResourceVersion,
		ConfirmedSandboxID: "gvisor-sandbox", ComputeDisposition: "delete", WorkspaceDisposition: "retain",
	})
	if err != nil || stop.Value.State != "pending" {
		t.Fatalf("accept gVisor Sandbox stop: value=%+v err=%v", stop.Value, err)
	}
	stopDelivery, err := node.HeartbeatRemoteWorker(ctx, "tenant", "project", enrollmentID, "request-gvisor-sandbox-stop-delivery", heartbeat)
	if err != nil || stopDelivery.Value.SandboxCommand == nil || stopDelivery.Value.SandboxCommand.Action != "sandbox.stop" {
		t.Fatalf("deliver gVisor Sandbox stop: value=%+v err=%v", stopDelivery.Value, err)
	}
	state, _ = json.Marshal(map[string]any{"incarnationId": incarnationID, "observedGeneration": heartbeat.ObservedGeneration,
		"observedState": heartbeat.ObservedState, "sandboxCommand": stopDelivery.Value.SandboxCommand})
	if err := os.WriteFile(stateFile, append(state, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
	runRemoteWorkerHeartbeatProcess(t, ctx, binary, server, enrollmentID, incarnationID, certificateChain, privateKey, stateFile)
	adminSandbox, err = admin.GetAdminSandboxSession(ctx, "tenant", "project", "gvisor-sandbox", "request-gvisor-sandbox-stopped")
	if err != nil || adminSandbox.Value.Spec.ObservedState != "stopped" || adminSandbox.Value.Spec.RuntimeID != "" || !adminSandbox.Value.Spec.WriterReleased {
		t.Fatalf("settle gVisor Sandbox stop: value=%+v err=%v", adminSandbox.Value, err)
	}
	marker, _ := json.Marshal(map[string]any{"runtimeId": runtimeID, "volumeName": volumeName,
		"workloadTrust": "shared-untrusted", "isolationRuntime": "gvisor", "capabilityRejected": capabilityRejected,
		"publicProfileRedacted": publicRedacted, "physicalIsolationVerified": true, "kernelAndDmesg": strings.TrimSpace(probe.Stdout),
		"outboundEgressBlocked": true, "observedState": adminSandbox.Value.Spec.ObservedState})
	fmt.Printf("REMOTE_WORKER_GVISOR=%s\n", marker)
}

func startRemoteWorkerDockerProxy(t *testing.T) string {
	t.Helper()
	socket := os.Getenv("CLOUD_AGENTS_REMOTE_WORKER_DOCKER_SOCKET")
	address := os.Getenv("CLOUD_AGENTS_REMOTE_WORKER_DOCKER_ADDRESS")
	credentialDirectory := os.Getenv("CLOUD_AGENTS_REMOTE_WORKER_CREDENTIAL_DIRECTORY")
	credentialRef := os.Getenv("CLOUD_AGENTS_REMOTE_WORKER_CREDENTIAL_REF")
	if socket == "" && address == "" || credentialDirectory == "" || credentialRef == "" {
		t.Fatal("RemoteWorker Docker proxy environment is incomplete")
	}
	network, target := "tcp", address
	if address == "" {
		if info, err := os.Stat(socket); err != nil || info.Mode()&os.ModeSocket == 0 {
			t.Fatalf("RemoteWorker Docker socket is unavailable: %v", err)
		}
		network, target = "unix", socket
	}
	transport := &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, network, target)
	}}
	proxy := &httputil.ReverseProxy{Director: func(request *http.Request) {
		request.URL.Scheme, request.URL.Host, request.Host = "http", "docker", "docker"
	}, Transport: transport}
	server := httptest.NewUnstartedServer(proxy)
	server.TLS = &tls.Config{MinVersion: tls.VersionTLS12, ClientAuth: tls.RequireAnyClientCert}
	server.StartTLS()
	t.Cleanup(func() { server.Close(); transport.CloseIdleConnections() })
	certificate := server.TLS.Certificates[0]
	privateKey, err := x509.MarshalPKCS8PrivateKey(certificate.PrivateKey)
	if err != nil {
		t.Fatal(err)
	}
	reference := filepath.Join(credentialDirectory, credentialRef)
	if err := os.MkdirAll(reference, 0o700); err != nil {
		t.Fatal(err)
	}
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certificate.Certificate[0]})
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: privateKey})
	for name, contents := range map[string][]byte{"ca.pem": certPEM, "cert.pem": certPEM, "key.pem": keyPEM} {
		if err := os.WriteFile(filepath.Join(reference, name), contents, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return server.URL
}

func startRemoteWorkerOpenSandboxProxy(t *testing.T, createDelay time.Duration) {
	t.Helper()
	credentialPath := filepath.Join(os.Getenv("CLOUD_AGENTS_REMOTE_WORKER_CREDENTIAL_DIRECTORY"),
		os.Getenv("CLOUD_AGENTS_REMOTE_WORKER_CREDENTIAL_REF"), "opensandbox.json")
	data, err := os.ReadFile(credentialPath)
	var config struct {
		Endpoint string `json:"endpoint"`
		APIKey   string `json:"apiKey"`
	}
	if err != nil {
		t.Fatalf("read node-local OpenSandbox credentials: %v", err)
	}
	if err := json.Unmarshal(data, &config); err != nil {
		t.Fatalf("decode node-local OpenSandbox credentials: %v", err)
	}
	target, err := url.Parse(config.Endpoint)
	if err != nil {
		t.Fatal(err)
	}
	reverseProxy := httputil.NewSingleHostReverseProxy(target)
	var delayOnce sync.Once
	handler := http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		delayed := false
		if request.Method == http.MethodPost && request.URL.Path == "/v1/sandboxes" {
			delayOnce.Do(func() { delayed = true })
		}
		if delayed {
			timer := time.NewTimer(createDelay)
			defer timer.Stop()
			select {
			case <-request.Context().Done():
				return
			case <-timer.C:
			}
		}
		reverseProxy.ServeHTTP(writer, request)
	})
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	encoded, err := json.Marshal(map[string]string{"endpoint": server.URL, "apiKey": config.APIKey})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(credentialPath, append(encoded, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
}

func startRemoteWorkerControlPlaneProxy(t *testing.T, upstream *httptest.Server, certificateChain string, privateKey []byte) (*httptest.Server, func(bool)) {
	t.Helper()
	certificate, err := tls.X509KeyPair([]byte(certificateChain), privateKey)
	if err != nil {
		t.Fatal(err)
	}
	transport, ok := upstream.Client().Transport.(*http.Transport)
	if !ok {
		t.Fatal("test TLS transport is unavailable")
	}
	transport = transport.Clone()
	transport.TLSClientConfig = transport.TLSClientConfig.Clone()
	transport.TLSClientConfig.Certificates = []tls.Certificate{certificate}
	target, err := url.Parse(upstream.URL)
	if err != nil {
		t.Fatal(err)
	}
	reverseProxy := httputil.NewSingleHostReverseProxy(target)
	reverseProxy.Transport = transport
	connected := true
	var lock sync.RWMutex
	handler := http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		lock.RLock()
		available := connected
		lock.RUnlock()
		if !available {
			if hijacker, ok := writer.(http.Hijacker); ok {
				connection, _, hijackErr := hijacker.Hijack()
				if hijackErr == nil {
					_ = connection.Close()
					return
				}
			}
			http.Error(writer, "unavailable", http.StatusServiceUnavailable)
			return
		}
		reverseProxy.ServeHTTP(writer, request)
	})
	server := httptest.NewUnstartedServer(handler)
	server.TLS = &tls.Config{MinVersion: tls.VersionTLS12, ClientAuth: tls.RequireAnyClientCert}
	server.StartTLS()
	t.Cleanup(func() { server.Close(); transport.CloseIdleConnections() })
	return server, func(value bool) {
		lock.Lock()
		connected = value
		lock.Unlock()
	}
}

func runRemoteWorkerHeartbeatProcess(t *testing.T, ctx context.Context, binary string, server *httptest.Server, enrollmentID, incarnationID, certificateChain string, privateKey []byte, stateFile string) {
	t.Helper()
	command := remoteWorkerProcessCommand(t, ctx, binary, server, enrollmentID, incarnationID, certificateChain, privateKey, stateFile, true)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("outbound RemoteWorker process: %v: %s", err, output)
	}
	t.Log("outbound RemoteWorker process heartbeat passed")
}

func startRemoteWorkerProcess(t *testing.T, ctx context.Context, binary string, server *httptest.Server, enrollmentID, incarnationID, certificateChain string, privateKey []byte, stateFile string) func() {
	t.Helper()
	command := remoteWorkerProcessCommand(t, ctx, binary, server, enrollmentID, incarnationID, certificateChain, privateKey, stateFile, false)
	var output bytes.Buffer
	command.Stdout, command.Stderr = &output, &output
	if err := command.Start(); err != nil {
		t.Fatal("start outbound RemoteWorker process", err)
	}
	done := make(chan error, 1)
	go func() { done <- command.Wait() }()
	var once sync.Once
	stop := func() {
		once.Do(func() {
			_ = command.Process.Signal(os.Interrupt)
			select {
			case err := <-done:
				if err != nil {
					t.Errorf("outbound RemoteWorker process stopped: %v: %s", err, output.String())
				}
			case <-time.After(5 * time.Second):
				_ = command.Process.Kill()
				<-done
				t.Errorf("outbound RemoteWorker process did not stop: %s", output.String())
			}
		})
	}
	t.Cleanup(stop)
	return stop
}

func remoteWorkerProcessCommand(t *testing.T, ctx context.Context, binary string, server *httptest.Server, enrollmentID, incarnationID, certificateChain string, privateKey []byte, stateFile string, once bool) *exec.Cmd {
	t.Helper()
	directory := t.TempDir()
	certificateFile := filepath.Join(directory, "node.pem")
	privateKeyFile := filepath.Join(directory, "node-key.pem")
	serverCAFile := filepath.Join(directory, "server-ca.pem")
	serverCertificate := server.Certificate()
	if serverCertificate == nil || os.WriteFile(certificateFile, []byte(certificateChain), 0o600) != nil || os.WriteFile(privateKeyFile, privateKey, 0o600) != nil || os.WriteFile(serverCAFile, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: serverCertificate.Raw}), 0o600) != nil {
		t.Fatal("write RemoteWorker process TLS material")
	}
	arguments := []string{
		"--control-plane-url=" + server.URL, "--tenant=tenant", "--project=project",
		"--enrollment=" + enrollmentID, "--incarnation=" + incarnationID,
		"--certificate=" + certificateFile, "--private-key=" + privateKeyFile, "--server-ca=" + serverCAFile,
		"--state-file=" + stateFile,
		"--docker-endpoint=" + remoteWorkerRuntimeEnv("CLOUD_AGENTS_REMOTE_WORKER_DOCKER_ENDPOINT", "https://docker.example.test"),
		"--credential-directory=" + remoteWorkerRuntimeEnv("CLOUD_AGENTS_REMOTE_WORKER_CREDENTIAL_DIRECTORY", "/tmp"),
		"--credential-ref=" + remoteWorkerRuntimeEnv("CLOUD_AGENTS_REMOTE_WORKER_CREDENTIAL_REF", "fixture-only"),
		"--kernel-version=6.12.1", "--capabilities=docker,exec,files,network-dns-nft,preview,pty,ssh,workspace-volume",
		"--capacity-cpu-millis=4000", "--capacity-memory-bytes=8589934592",
		"--capacity-disk-bytes=42949672960",
	}
	if once {
		arguments = append(arguments, "--once")
	}
	return exec.CommandContext(ctx, binary, arguments...)
}

func remoteWorkerRuntimeEnv(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}

func assertRemoteWorkerAdminRedaction(t *testing.T, ctx context.Context, server *httptest.Server, adminToken, enrollmentID, secret string) {
	t.Helper()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, server.URL+"/v1/admin/tenants/tenant/projects/project/remote-worker-enrollments/"+enrollmentID, nil)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Authorization", "Bearer "+adminToken)
	request.Header.Set("X-Request-ID", "request-enrollment-redaction")
	response, err := server.Client().Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil || response.StatusCode != http.StatusOK {
		t.Fatalf("Admin redaction response status=%d err=%v", response.StatusCode, err)
	}
	for _, forbidden := range []string{"enrollmentSecret", "secretDigest", "secret_digest", "certificateChainPem", "certificate_chain_pem", secret} {
		if strings.Contains(string(body), forbidden) {
			t.Fatalf("Admin enrollment response disclosed %q", forbidden)
		}
	}
}
