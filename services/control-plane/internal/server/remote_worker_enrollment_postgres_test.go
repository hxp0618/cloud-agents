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

// Real generated Admin/bootstrap clients, HTTP scopes, one-time secret handling,
// PostgreSQL RLS/functions and durable audit acceptance. No customer node connects.
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
		"projects.act projects.get audit.list remote-worker-enrollments.act remote-worker-enrollments.create remote-worker-enrollments.get remote-worker-enrollments.list",
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
	handler, err := NewRemoteWorkerEnrollmentHTTPServer(verifier, store, authority)
	if err != nil {
		t.Fatal(err)
	}
	httpServer := httptest.NewServer(handler)
	defer httpServer.Close()
	admin, _ := api.NewHTTPClientWithClient(httpServer.URL, tokens[0], httpServer.Client())
	user, _ := api.NewHTTPClientWithClient(httpServer.URL, tokens[1], httpServer.Client())
	bootstrap, _ := api.NewHTTPClientWithClient(httpServer.URL, tokens[2], httpServer.Client())
	create := platform.RemoteWorkerEnrollmentCreateRequest{
		EnrollmentID: "enrollment-remote-1", WorkerID: "worker-customer-1",
		WorkerName: "customer-node-1", TTLSeconds: 600,
	}
	created, err := admin.CreateAdminRemoteWorkerEnrollment(ctx, "tenant", "project", "request-enrollment-create", "remote-enroll-create-key-1", create)
	if err != nil || created.Value.Spec.State != internalremoteworker.StatePending || created.Value.Metadata.ResourceVersion != "1" {
		t.Fatalf("create enrollment: value=%+v err=%v", created.Value, err)
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
	if err != nil || current.Value.Spec.State != internalremoteworker.StateEnrolled || current.Value.Metadata.ResourceVersion != "3" || current.Value.Spec.CertificateSHA256 != issued.Value.CertificateSHA256 || current.Value.Spec.CertificateExpiresAt != issued.Value.ExpiresAt || current.Value.Spec.IncarnationID != certificateRequest.IncarnationID {
		t.Fatalf("Admin issued enrollment metadata: value=%+v err=%v", current.Value, err)
	}
	assertRemoteWorkerAdminRedaction(t, ctx, httpServer, tokens[0], create.EnrollmentID, secret)
	audit, err = admin.ListAdminRemoteWorkerEnrollmentAuditEvents(ctx, "tenant", "project", create.EnrollmentID, "request-enrollment-audit-final", 50, "")
	if err != nil || len(audit.Value.Events) != 3 {
		t.Fatalf("final enrollment audit: value=%+v err=%v", audit.Value, err)
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
	if persistedDigest != expectedDigest || certificateChain != issued.Value.CertificateChainPEM || strings.Contains(enrollmentJSON, secret) || strings.Contains(activityJSON, secret) {
		t.Fatal("one-time enrollment secret persisted outside its digest")
	}
	t.Log("real Admin/bootstrap generated clients, HTTP scope separation, no-store secret/CSR exchange, signed short-lived mTLS identity, exact certificate replay, RLS-backed persistence and redacted Admin audit passed; no outbound customer-node connection claimed")
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
