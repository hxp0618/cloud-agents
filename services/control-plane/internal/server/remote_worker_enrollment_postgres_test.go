package server

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	api "github.com/hxp0618/cloud-agents/sdk/go/gen/openapi/v1alpha1"
	platform "github.com/hxp0618/cloud-agents/sdk/go/gen/platform/v1alpha1"
	internalremoteworker "github.com/hxp0618/cloud-agents/services/control-plane/internal/remoteworker"
	"github.com/hxp0618/cloud-agents/services/control-plane/internal/store/postgres"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Real generated Admin/bootstrap/node clients, verified mTLS identity,
// PostgreSQL RLS/functions, outbound commands and durable Operation/Audit acceptance.
func TestRemoteWorkerEnrollmentPostgres(t *testing.T) {
	runtimeURL := os.Getenv("CLOUD_AGENTS_FOUNDATION_PROFILE_RUNTIME_DATABASE_URL")
	ownerURL := os.Getenv("CLOUD_AGENTS_FOUNDATION_PROFILE_OWNER_DATABASE_URL")
	if runtimeURL == "" || ownerURL == "" {
		t.Skip("isolated RemoteWorker enrollment PostgreSQL environment not configured")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
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
		"projects.act projects.get targets.act targets.get targets.list audit.list operations.list remote-worker-enrollments.act remote-worker-enrollments.create remote-worker-enrollments.get remote-worker-enrollments.list",
		"projects.act projects.get",
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
	routes := http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if HandlesAdminDeploymentTargetPath(request.URL.Path) {
			targetHandler.ServeHTTP(writer, request)
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
		Capabilities: []string{"docker", "exec", "files"},
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

func runRemoteWorkerHeartbeatProcess(t *testing.T, ctx context.Context, binary string, server *httptest.Server, enrollmentID, incarnationID, certificateChain string, privateKey []byte, stateFile string) {
	t.Helper()
	directory := t.TempDir()
	certificateFile := filepath.Join(directory, "node.pem")
	privateKeyFile := filepath.Join(directory, "node-key.pem")
	serverCAFile := filepath.Join(directory, "server-ca.pem")
	serverCertificate := server.Certificate()
	if serverCertificate == nil || os.WriteFile(certificateFile, []byte(certificateChain), 0o600) != nil || os.WriteFile(privateKeyFile, privateKey, 0o600) != nil || os.WriteFile(serverCAFile, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: serverCertificate.Raw}), 0o600) != nil {
		t.Fatal("write RemoteWorker process TLS material")
	}
	command := exec.CommandContext(ctx, binary,
		"--control-plane-url="+server.URL, "--tenant=tenant", "--project=project",
		"--enrollment="+enrollmentID, "--incarnation="+incarnationID,
		"--certificate="+certificateFile, "--private-key="+privateKeyFile, "--server-ca="+serverCAFile,
		"--state-file="+stateFile,
		"--kernel-version=6.12.1", "--capabilities=docker,exec,files",
		"--capacity-cpu-millis=4000", "--capacity-memory-bytes=8589934592",
		"--capacity-disk-bytes=42949672960", "--once",
	)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("outbound RemoteWorker process: %v: %s", err, output)
	}
	t.Log("outbound RemoteWorker process heartbeat passed")
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
