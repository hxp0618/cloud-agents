package server

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	platform "github.com/hxp0618/cloud-agents/sdk/go/gen/platform/v1alpha1"
	"github.com/hxp0618/cloud-agents/services/control-plane/internal/authn"
	"github.com/hxp0618/cloud-agents/services/control-plane/internal/dockertarget"
	managedhost "github.com/hxp0618/cloud-agents/services/control-plane/internal/managedhost"
)

type healthLeaseStore struct {
	managedHostEnvironmentLeaseStoreFake
	onGet func(int, *managedhost.Snapshot)
}

func (store *healthLeaseStore) GetManagedHostEnvironmentLease(context.Context, string, *authn.VerifiedPrincipal, string, string) (managedhost.Snapshot, error) {
	store.get++
	if store.onGet != nil {
		store.onGet(store.get, &store.snapshot)
	}
	return store.snapshot, nil
}

// Real kernel and TLS transport; only authorization/store snapshots are test doubles.
// This is not Docker/Kubernetes/SSH deployment or Provider E2E evidence.
func TestAdminWorkerHealthRealMTLSKernel(t *testing.T) {
	now := time.Now().UTC()
	ca, caKey := healthTestCertificate(t, nil, nil, "", true)
	roots := x509.NewCertPool()
	roots.AddCert(ca.Leaf)
	serverCert, _ := healthTestCertificate(t, ca.Leaf, caKey, "spiffe://health.test/worker/lease-alpha", false)
	clientCert, _ := healthTestCertificate(t, ca.Leaf, caKey, "spiffe://health.test/supervisor", false)
	endpoint, stopWorker := startHealthTestWorker(t, serverCert, ca.Leaf)
	snapshot := managedhost.Snapshot{
		Scope: managedhost.Scope{TenantID: "tenant-alpha", ProjectID: "project-alpha"}, LeaseID: "lease-alpha", LeaseName: "lease-alpha", TargetID: "docker-alpha", TargetGeneration: 1,
		Generation: 2, ResourceVersion: 3, DesiredPhase: "active", ObservedPhase: "ready", CleanupPhase: "none", ExpiresAt: now.Add(time.Hour),
		WorkerEndpoint: endpoint, WorkerSPIFFEID: "spiffe://health.test/worker/lease-alpha", WorkerServerName: "health.test", ProviderCredentialRef: "secret-ref-must-not-leak",
	}
	store := &healthLeaseStore{managedHostEnvironmentLeaseStoreFake: managedHostEnvironmentLeaseStoreFake{snapshot: snapshot}}
	verifier := &managedHostEnvironmentLeaseVerifierFake{}
	admin, err := NewAdminEnvironmentLeaseHTTPServer(verifier, store, nil, nil, nil, dockertarget.WorkerTrust{ClientCertificate: clientCert, RootCAs: roots})
	if err != nil {
		t.Fatal(err)
	}
	request := func(query string) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest(http.MethodGet, "/v1/admin/tenants/tenant-alpha/projects/project-alpha/workers/lease-alpha/health?"+query, nil)
		req.Header.Set("Authorization", "Bearer admin-token")
		req.Header.Set("X-Request-ID", "request-worker-health")
		response := httptest.NewRecorder()
		admin.ServeHTTP(response, req)
		for _, secret := range []string{snapshot.WorkerEndpoint, snapshot.ProviderCredentialRef, "workerEndpoint", "credentialRef", "Prompt", "PRIVATE KEY"} {
			if strings.Contains(response.Body.String(), secret) {
				t.Fatalf("response leaked %q", secret)
			}
		}
		return response
	}
	response := request("expectedGeneration=2")
	observation, err := platform.DecodeWorkerHealthObservationResponseJSON(response.Body.Bytes())
	if response.Code != 200 || err != nil || observation.Value.State != "serving" || observation.Value.Generation != 2 || response.Header().Get("Cache-Control") != "no-store" || store.get != 2 {
		t.Fatalf("health status=%d err=%v body=%s", response.Code, err, response.Body.String())
	}
	for _, query := range []string{"", "expectedGeneration=02", "expectedGeneration=0", "expectedGeneration=2&expectedGeneration=2", "expectedGeneration=2&endpoint=https://elsewhere.test"} {
		if response := request(query); response.Code != 400 {
			t.Fatalf("query=%q status=%d", query, response.Code)
		}
	}
	if response := request("expectedGeneration=1"); response.Code != 409 {
		t.Fatalf("stale generation=%d", response.Code)
	}
	store.snapshot.ExpiresAt = now.Add(-time.Second)
	if response := request("expectedGeneration=2"); response.Code != 409 {
		t.Fatalf("expired lease=%d", response.Code)
	}
	for _, change := range []func(*managedhost.Snapshot){
		func(lease *managedhost.Snapshot) { lease.Generation++ },
		func(lease *managedhost.Snapshot) { lease.ResourceVersion++ },
		func(lease *managedhost.Snapshot) { lease.WorkerEndpoint += "/changed" },
		func(lease *managedhost.Snapshot) { lease.WorkerSPIFFEID += "/changed" },
		func(lease *managedhost.Snapshot) { lease.WorkerServerName = "changed.test" },
		func(lease *managedhost.Snapshot) { lease.DesiredPhase = "terminated" },
		func(lease *managedhost.Snapshot) { lease.ObservedPhase = "terminating" },
		func(lease *managedhost.Snapshot) { lease.CleanupPhase = "pending" },
		func(lease *managedhost.Snapshot) { lease.ExpiresAt = now.Add(-time.Second) },
	} {
		store.snapshot, store.get = snapshot, 0
		store.onGet = func(count int, lease *managedhost.Snapshot) {
			if count == 2 {
				change(lease)
			}
		}
		if response := request("expectedGeneration=2"); response.Code != 409 || !strings.Contains(response.Body.String(), "WORKER_CHANGED_DURING_HEALTH_CHECK") {
			t.Fatalf("race=%d %s", response.Code, response.Body.String())
		}
	}
	store.onGet = nil
	store.snapshot = snapshot
	store.snapshot.WorkerServerName = ""
	if response := request("expectedGeneration=2"); response.Code != 503 {
		t.Fatalf("missing route identity=%d", response.Code)
	}
	store.snapshot = snapshot
	admin.workerTrust.RootCAs = nil
	if response := request("expectedGeneration=2"); response.Code != 503 {
		t.Fatalf("missing trust=%d", response.Code)
	}
	admin.workerTrust.RootCAs = roots
	store.snapshot = snapshot
	store.snapshot.WorkerSPIFFEID = "spiffe://health.test/worker/wrong"
	response = request("expectedGeneration=2")
	observation, err = platform.DecodeWorkerHealthObservationResponseJSON(response.Body.Bytes())
	if response.Code != 200 || err != nil || observation.Value.State != "unavailable" {
		t.Fatalf("identity mismatch status=%d err=%v", response.Code, err)
	}
	store.snapshot = snapshot
	stopWorker()
	response = request("expectedGeneration=2")
	observation, err = platform.DecodeWorkerHealthObservationResponseJSON(response.Body.Bytes())
	if response.Code != 200 || err != nil || observation.Value.State != "unavailable" {
		t.Fatalf("stopped Worker status=%d err=%v", response.Code, err)
	}
	verifier.requests = nil
	verifier.failAt = 2
	store.get = 0
	if response := request("expectedGeneration=2"); response.Code != 403 || store.get != 0 || verifier.requests[1].RequiredPermission != "workers.list" {
		t.Fatalf("ordinary token=%d reads=%d", response.Code, store.get)
	}
}

// Cross-service validation uses the independently built executable, not a forbidden Go service import.
func startHealthTestWorker(t *testing.T, certificate tls.Certificate, ca *x509.Certificate) (string, func()) {
	t.Helper()
	directory := t.TempDir()
	write := func(name string, data []byte) string {
		path := filepath.Join(directory, name)
		if err := os.WriteFile(path, data, 0600); err != nil {
			t.Fatal(err)
		}
		return path
	}
	key, err := x509.MarshalPKCS8PrivateKey(certificate.PrivateKey)
	if err != nil {
		t.Fatal(err)
	}
	certFile := write("worker.pem", pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certificate.Certificate[0]}))
	keyFile := write("worker.key", pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: key}))
	caFile := write("ca.pem", pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: ca.Raw}))
	tokenFile := write("admission.token", []byte("health-check-only-never-used-for-runtime"))
	workerModule, err := filepath.Abs("../../../worker")
	if err != nil {
		t.Fatal(err)
	}
	binary := filepath.Join(directory, "worker")
	buildContext, cancelBuild := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancelBuild()
	build := exec.CommandContext(buildContext, "go", "-C", workerModule, "build", "-o", binary, "./cmd/cloud-agents-worker")
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build standalone Worker: %v: %s", err, output)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	address := listener.Addr().String()
	listener.Close()
	// Health does not start Runtime. Fail closed if the test accidentally requests a Session.
	unusedRuntime, err := exec.LookPath("false")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	command := exec.CommandContext(ctx, binary, "--listen", address, "--tls-cert", certFile, "--tls-key", keyFile, "--client-ca", caFile, "--worker-spiffe-id", "spiffe://health.test/worker/lease-alpha", "--runtime-command", unusedRuntime, "--runtime-directory", directory, "--provider-credential-directory", directory, "--admission-lease-id", "lease-alpha", "--admission-generation", "2", "--admission-token-file", tokenFile)
	if err := command.Start(); err != nil {
		cancel()
		t.Fatal(err)
	}
	stop := func() {
		if command.ProcessState == nil {
			cancel()
			_ = command.Wait()
		}
	}
	t.Cleanup(stop)
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		connection, err := net.DialTimeout("tcp", address, 50*time.Millisecond)
		if err == nil {
			connection.Close()
			return "https://" + address, stop
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("standalone Worker did not listen")
	return "", stop
}

func healthTestCertificate(t *testing.T, parent *x509.Certificate, signer *ecdsa.PrivateKey, spiffe string, ca bool) (tls.Certificate, *ecdsa.PrivateKey) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 120))
	if err != nil {
		t.Fatal(err)
	}
	template := &x509.Certificate{SerialNumber: serial, NotBefore: time.Now().Add(-time.Minute), NotAfter: time.Now().Add(time.Hour), BasicConstraintsValid: true, IsCA: ca, DNSNames: []string{"health.test"}, KeyUsage: x509.KeyUsageDigitalSignature}
	if ca {
		template.KeyUsage |= x509.KeyUsageCertSign
		parent, signer = template, key
	} else {
		uri, err := url.Parse(spiffe)
		if err != nil {
			t.Fatal(err)
		}
		template.URIs = []*url.URL{uri}
		template.ExtKeyUsage = []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth, x509.ExtKeyUsageServerAuth}
	}
	der, err := x509.CreateCertificate(rand.Reader, template, parent, &key.PublicKey, signer)
	if err != nil {
		t.Fatal(err)
	}
	leaf, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key, Leaf: leaf}, key
}
