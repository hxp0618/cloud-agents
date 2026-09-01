//go:build localdev

// cloud-agents-worker is an explicitly localdev-only Worker HTTP entry point.
// It is intentionally a loopback process boundary around the in-memory Worker
// kernel. No provider, database, public listener, TLS, or durable receipt
// store is configured here.
package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
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
	"unicode/utf8"

	workerv1alpha1 "github.com/hxp0618/cloud-agents/sdk/go/gen/cloudagents/worker/v1alpha1"
	workerkernel "github.com/hxp0618/cloud-agents/services/worker"
)

var (
	version                = "dev"
	errInvalidWorkerConfig = errors.New("cloud-agents-worker/invalid_config")
	errNonLoopbackListen   = errors.New("cloud-agents-worker/listen_must_be_loopback")
	errInvalidTokenPath    = errors.New("cloud-agents-worker/token_file_path_invalid")
	errInvalidToken        = errors.New("cloud-agents-worker/token_invalid")
)

const (
	defaultWorkerListen = "127.0.0.1:8091"
	maxLocalTokenBytes  = 256
)

type localWorkerConfig struct {
	listen                     string
	tokenFile                  string
	runtimeCommand             string
	runtimeDirectory           string
	runtimeMaxSessions         int
	runtimeCredentialDirectory string
	// token and tokenGenerator are test seams. The command never accepts a
	// token on the command line and therefore cannot leak it via argv.
	token          string
	tokenGenerator func() (string, error)
	clock          workerkernel.Clock
	workerIdentity *workerv1alpha1.WorkloadIdentity
	supervisor     *workerv1alpha1.WorkloadIdentity
}

type localWorkerHTTPServer struct {
	Server *http.Server
	Token  string
}

type localWorkerIdentityContextKey struct{}

// contextIdentityProvider is the only IdentityProvider wired by this
// launcher. It reads an identity established by bearer middleware; expected_*
// request fields are never treated as authentication material.
type contextIdentityProvider struct{}

func (contextIdentityProvider) ClientIdentity(ctx context.Context) (*workerv1alpha1.WorkloadIdentity, error) {
	if ctx == nil {
		return nil, errors.New("worker/transport_identity_missing")
	}
	identity, ok := ctx.Value(localWorkerIdentityContextKey{}).(*workerv1alpha1.WorkloadIdentity)
	if !ok || identity == nil {
		return nil, errors.New("worker/transport_identity_missing")
	}
	return cloneWorkerIdentity(identity), nil
}

type localWorkerHealth struct {
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

func parseLocalWorkerConfig(args []string) (localWorkerConfig, error) {
	set := flag.NewFlagSet("cloud-agents-worker", flag.ContinueOnError)
	set.SetOutput(io.Discard)
	listen := set.String("listen", defaultWorkerListen, "loopback listen address")
	tokenFile := set.String("token-file", "", "write one ephemeral local bearer token to this 0600 file")
	runtimeCommand := set.String("runtime-command", "", "Cloud Agent Runtime executable")
	runtimeDirectory := set.String("runtime-directory", "", "absolute Runtime working directory")
	runtimeMaxSessions := set.Int("runtime-max-sessions", workerkernel.DefaultRuntimeMaxSessions, "maximum concurrent Runtime sessions")
	runtimeCredentialDirectory := set.String("provider-credential-directory", "", "optional absolute directory containing <providerKind>.json credentials")
	if err := set.Parse(args); err != nil || set.NArg() != 0 {
		return localWorkerConfig{}, errInvalidWorkerConfig
	}
	cfg := localWorkerConfig{listen: *listen, tokenFile: *tokenFile, runtimeCommand: *runtimeCommand, runtimeDirectory: *runtimeDirectory, runtimeMaxSessions: *runtimeMaxSessions, runtimeCredentialDirectory: *runtimeCredentialDirectory}
	if err := validateLoopbackListen(cfg.listen); err != nil {
		return localWorkerConfig{}, err
	}
	if cfg.tokenFile == "" {
		return localWorkerConfig{}, errInvalidTokenPath
	}
	if err := validateLocalWorkerConfig(cfg); err != nil {
		return localWorkerConfig{}, err
	}
	return cfg, nil
}

func validateLocalWorkerConfig(cfg localWorkerConfig) error {
	if err := validateLoopbackListen(cfg.listen); err != nil {
		return err
	}
	if cfg.tokenFile != "" {
		if strings.TrimSpace(cfg.tokenFile) != cfg.tokenFile || strings.HasSuffix(cfg.tokenFile, string(os.PathSeparator)) {
			return errInvalidTokenPath
		}
		if !utf8.ValidString(cfg.tokenFile) {
			return errInvalidTokenPath
		}
	}
	if cfg.token != "" {
		if err := validateLocalToken(cfg.token); err != nil {
			return err
		}
	}
	if strings.TrimSpace(cfg.runtimeCommand) != cfg.runtimeCommand || strings.ContainsRune(cfg.runtimeCommand, '\x00') || strings.TrimSpace(cfg.runtimeDirectory) != cfg.runtimeDirectory || strings.TrimSpace(cfg.runtimeCredentialDirectory) != cfg.runtimeCredentialDirectory || cfg.runtimeMaxSessions < 0 || cfg.runtimeMaxSessions > workerkernel.MaxRuntimeSessions || (cfg.runtimeCommand != "" && cfg.runtimeMaxSessions == 0) {
		return errInvalidWorkerConfig
	}
	if cfg.runtimeCommand == "" {
		if cfg.runtimeDirectory != "" || cfg.runtimeCredentialDirectory != "" {
			return errInvalidWorkerConfig
		}
		return nil
	}
	if !filepath.IsAbs(cfg.runtimeDirectory) || (cfg.runtimeCredentialDirectory != "" && !filepath.IsAbs(cfg.runtimeCredentialDirectory)) {
		return errInvalidWorkerConfig
	}
	return nil
}

func validateLoopbackListen(address string) error {
	host, port, err := net.SplitHostPort(address)
	if err != nil || host == "" {
		return errNonLoopbackListen
	}
	ip := net.ParseIP(host)
	if ip == nil || !ip.IsLoopback() {
		return errNonLoopbackListen
	}
	portNumber, err := strconv.Atoi(port)
	if err != nil || portNumber < 1 || portNumber > 65535 {
		return errNonLoopbackListen
	}
	return nil
}

func validateLocalToken(token string) error {
	if token == "" || len(token) > maxLocalTokenBytes || strings.TrimSpace(token) != token || strings.ContainsAny(token, " \t\r\n") || !utf8.ValidString(token) {
		return errInvalidToken
	}
	return nil
}

func randomLocalToken() (string, error) {
	bytes := make([]byte, 32)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(bytes), nil
}

// writeLocalWorkerTokenFile deliberately uses O_EXCL and mode 0600. A token
// file is never overwritten or printed; partial writes remove only the file
// created by this call.
func writeLocalWorkerTokenFile(path, token string) error {
	if path == "" || validateLocalToken(token) != nil {
		return errInvalidTokenPath
	}
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return errInvalidTokenPath
	}
	remove := true
	defer func() {
		if remove {
			_ = os.Remove(path)
		}
	}()
	if err := file.Chmod(0o600); err != nil {
		_ = file.Close()
		return errInvalidTokenPath
	}
	if _, err := file.WriteString(token + "\n"); err != nil {
		_ = file.Close()
		return errInvalidTokenPath
	}
	if err := file.Close(); err != nil {
		return errInvalidTokenPath
	}
	remove = false
	return nil
}

func newLocalWorkerHTTPServer(cfg localWorkerConfig) (*localWorkerHTTPServer, error) {
	if err := validateLocalWorkerConfig(cfg); err != nil {
		return nil, err
	}
	if cfg.runtimeCommand == "" && !validGeneratedLauncherProfile() {
		return nil, errInvalidWorkerConfig
	}
	workerIdentity := cfg.workerIdentity
	if workerIdentity == nil {
		workerIdentity = generatedWorkerIdentity()
	}
	supervisorIdentity := cfg.supervisor
	if supervisorIdentity == nil {
		supervisorIdentity = generatedSupervisorIdentity()
	}
	if err := validateFixedIdentities(workerIdentity, supervisorIdentity); err != nil {
		return nil, err
	}
	workerIdentity = cloneWorkerIdentity(workerIdentity)
	supervisorIdentity = cloneWorkerIdentity(supervisorIdentity)
	clock := cfg.clock
	if clock == nil {
		clock = time.Now
	}
	if cfg.runtimeCommand != "" {
		if info, err := os.Stat(cfg.runtimeDirectory); err != nil || !info.IsDir() {
			return nil, errInvalidWorkerConfig
		}
		if cfg.runtimeCredentialDirectory != "" {
			if info, err := os.Stat(cfg.runtimeCredentialDirectory); err != nil || !info.IsDir() {
				return nil, errInvalidWorkerConfig
			}
		}
	}
	token := cfg.token
	if token == "" {
		generator := cfg.tokenGenerator
		if generator == nil {
			generator = randomLocalToken
		}
		var tokenErr error
		token, tokenErr = generator()
		if tokenErr != nil || validateLocalToken(token) != nil {
			return nil, fmt.Errorf("%w: token generation failed", errInvalidWorkerConfig)
		}
	}
	var runtimeCommand []string
	runtimeMaxSessions := 0
	if cfg.runtimeCommand != "" {
		runtimeCommand = []string{cfg.runtimeCommand}
		runtimeMaxSessions = cfg.runtimeMaxSessions
	}
	workerService, err := workerkernel.NewService(workerkernel.Config{
		WorkerIdentity: workerIdentity,
		Capabilities: []workerv1alpha1.Capability{
			workerv1alpha1.Capability_CAPABILITY_NEGOTIATION,
			workerv1alpha1.Capability_CAPABILITY_HEALTH,
			workerv1alpha1.Capability_CAPABILITY_OPERATION_DISPATCH,
		},
		IdentityProvider:           contextIdentityProvider{},
		AdmissionLeaseID:           workerkernel.WorkerLocalDevLauncherLeaseID,
		AdmissionGeneration:        workerkernel.WorkerLocalDevLauncherGeneration,
		AdmissionToken:             []byte(token),
		Clock:                      clock,
		Executor:                   workerkernel.DeterministicLocalExecutor{},
		RuntimeCommand:             runtimeCommand,
		RuntimeMaxSessions:         runtimeMaxSessions,
		RuntimeEnvironment:         localRuntimeEnvironment(os.Environ()),
		RuntimeDirectory:           cfg.runtimeDirectory,
		RuntimeCredentialDirectory: cfg.runtimeCredentialDirectory,
	})
	if err != nil {
		return nil, fmt.Errorf("%w: worker service: %v", errInvalidWorkerConfig, err)
	}
	connectPath, connectHandler := workerkernel.NewHandler(workerService)
	if connectPath != workerkernel.WorkerLocalDevLauncherHTTPRoutePrefix {
		return nil, fmt.Errorf("%w: generated connect route mismatch", errInvalidWorkerConfig)
	}
	if cfg.tokenFile != "" {
		if err := writeLocalWorkerTokenFile(cfg.tokenFile, token); err != nil {
			return nil, err
		}
	}
	mux := http.NewServeMux()
	mux.Handle(connectPath, connectHandler)
	if cfg.runtimeCommand != "" {
		runtimePath, runtimeHandler := workerkernel.NewRuntimeHandler(workerService)
		mux.Handle(runtimePath, runtimeHandler)
	}
	mux.HandleFunc(workerkernel.WorkerLocalDevLauncherHealthRoute, func(response http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodGet {
			response.Header().Set("Allow", http.MethodGet)
			response.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		health := localWorkerHealth{
			APIVersion:      "cloud-agents.localdev/v1",
			Authority:       workerkernel.WorkerLocalDevLauncherAuthorityID,
			Profile:         workerkernel.WorkerLocalDevLauncherProfileID,
			Revision:        workerkernel.WorkerLocalDevLauncherRevision,
			ProfileDigest:   workerkernel.WorkerLocalDevLauncherProfileDigest,
			Version:         "v1.0",
			Status:          "serving",
			WorkerIdentity:  workerIdentity.GetSpiffeId(),
			Supervisor:      supervisorIdentity.GetSpiffeId(),
			LeaseID:         workerkernel.WorkerLocalDevLauncherLeaseID,
			Generation:      workerkernel.WorkerLocalDevLauncherGeneration,
			ExternalEffects: cfg.runtimeCommand != "",
			Transport:       "loopback_http_connect",
		}
		if cfg.runtimeCommand != "" {
			health.Authority = "cloud-agents-localdev"
			health.Profile = "cloud-agents/worker-localdev-runtime/v1"
			health.Revision = "v1"
			health.ProfileDigest = ""
		}
		writeLocalWorkerHealth(response, health)
	})
	root := localWorkerAuthMiddleware(token, supervisorIdentity, mux)
	protocols := new(http.Protocols)
	protocols.SetHTTP1(true)
	protocols.SetUnencryptedHTTP2(true)
	readHeaderTimeout, readTimeout, writeTimeout := 5*time.Second, 30*time.Second, 30*time.Second
	if cfg.runtimeCommand != "" {
		readHeaderTimeout, readTimeout, writeTimeout = 0, 0, 0
	}
	return &localWorkerHTTPServer{Server: &http.Server{Handler: root, ReadHeaderTimeout: readHeaderTimeout, ReadTimeout: readTimeout, WriteTimeout: writeTimeout, IdleTimeout: 60 * time.Second, Protocols: protocols}, Token: token}, nil
}

func localRuntimeEnvironment(source []string) []string {
	filtered := make([]string, 0, len(source))
	for _, entry := range source {
		name, _, found := strings.Cut(entry, "=")
		if found {
			switch name {
			case "CLOUD_AGENTS_PLATFORM_DATABASE_URL", "CLOUD_AGENTS_PLATFORM_AUTH_CONFIG", "CLOUD_AGENTS_PLATFORM_WORKER_ENDPOINT", "CLOUD_AGENTS_PLATFORM_WORKER_SPIFFE_ID", "CLOUD_AGENTS_PLATFORM_WORKER_CLIENT_CERT", "CLOUD_AGENTS_PLATFORM_WORKER_CLIENT_KEY", "CLOUD_AGENTS_PLATFORM_WORKER_CA", "CLOUD_AGENTS_PLATFORM_WORKSPACE_DIRECTORY", "CLOUD_AGENTS_PLATFORM_ADMISSION_LEASE_ID", "CLOUD_AGENTS_PLATFORM_ADMISSION_GENERATION", "CLOUD_AGENTS_PLATFORM_ADMISSION_TOKEN", "CLOUD_AGENTS_ADMISSION_LEASE_ID", "CLOUD_AGENTS_ADMISSION_GENERATION", "CLOUD_AGENTS_ADMISSION_TOKEN", "CLOUD_AGENT_PROVIDER_CREDENTIAL_FD", "SYNARA_PROVIDER_CREDENTIAL_FD":
				continue
			}
		}
		filtered = append(filtered, entry)
	}
	return filtered
}

func validGeneratedLauncherProfile() bool {
	profile := workerkernel.GeneratedWorkerLocalDevLauncherProfile
	return profile.Valid() && profile.ID == workerkernel.WorkerLocalDevLauncherProfileID &&
		profile.AuthorityID == workerkernel.WorkerLocalDevLauncherAuthorityID &&
		profile.Revision == workerkernel.WorkerLocalDevLauncherRevision &&
		profile.ProfileDigest == workerkernel.WorkerLocalDevLauncherProfileDigest &&
		profile.SourceDigest == workerkernel.WorkerLocalDevLauncherSourceDigest &&
		profile.InputManifestDigest == workerkernel.WorkerLocalDevLauncherInputManifestDigest &&
		profile.RoutePrefix == workerkernel.WorkerLocalDevLauncherHTTPRoutePrefix &&
		profile.HealthRoute == workerkernel.WorkerLocalDevLauncherHealthRoute &&
		profile.WorkerIdentitySPIFFE == workerkernel.WorkerLocalDevLauncherWorkerIdentitySPIFFE &&
		profile.SupervisorIdentitySPIFFE == workerkernel.WorkerLocalDevLauncherSupervisorIdentitySPIFFE &&
		profile.LeaseID == workerkernel.WorkerLocalDevLauncherLeaseID &&
		profile.Generation == workerkernel.WorkerLocalDevLauncherGeneration &&
		profile.Mode == "localdev_only" &&
		profile.Transport == "loopback_http_connect" &&
		!profile.ExternalSideEffects &&
		profile.CompleteLedger == "no_op" &&
		profile.EntryWriter == "not_implemented" &&
		profile.RecoveryWriter == "not_implemented" &&
		profile.DatabaseWrites == "forbidden" &&
		profile.DurablePersistence == "forbidden" &&
		profile.Provider == "forbidden" &&
		profile.Runtime == "forbidden" &&
		profile.ProductionHTTP == "forbidden" &&
		profile.PublicHTTP == "forbidden" &&
		profile.P2 == "forbidden" &&
		profile.Deployment == "forbidden" &&
		profile.Publication == "forbidden" &&
		profile.GateTransition == "forbidden"
}

func localWorkerAuthMiddleware(token string, identity *workerv1alpha1.WorkloadIdentity, next http.Handler) http.Handler {
	if next == nil {
		next = http.NotFoundHandler()
	}
	return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request == nil || !requestFromLoopback(request) {
			response.WriteHeader(http.StatusNotFound)
			return
		}
		provided, ok := localWorkerBearer(request.Header)
		if !ok || subtle.ConstantTimeCompare([]byte(provided), []byte(token)) != 1 {
			response.Header().Set("WWW-Authenticate", "Bearer")
			response.WriteHeader(http.StatusUnauthorized)
			return
		}
		ctx := context.WithValue(request.Context(), localWorkerIdentityContextKey{}, cloneWorkerIdentity(identity))
		next.ServeHTTP(response, request.WithContext(ctx))
	})
}

func localWorkerBearer(header http.Header) (string, bool) {
	values := header.Values("Authorization")
	if len(values) != 1 || values[0] == "" || strings.TrimSpace(values[0]) != values[0] || !strings.HasPrefix(values[0], "Bearer ") {
		return "", false
	}
	token := strings.TrimPrefix(values[0], "Bearer ")
	return token, validateLocalToken(token) == nil
}

func requestFromLoopback(request *http.Request) bool {
	if request == nil || request.RemoteAddr == "" {
		return false
	}
	host, port, err := net.SplitHostPort(request.RemoteAddr)
	if err != nil || port == "" {
		return false
	}
	portNumber, err := strconv.Atoi(port)
	if err != nil || portNumber < 1 || portNumber > 65535 {
		return false
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func writeLocalWorkerHealth(response http.ResponseWriter, health localWorkerHealth) {
	response.Header().Set("Content-Type", "application/json")
	response.Header().Set("Cache-Control", "no-store")
	response.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(response).Encode(health)
}

func generatedWorkerIdentity() *workerv1alpha1.WorkloadIdentity {
	return identityFromSPIFFE(workerkernel.WorkerLocalDevLauncherWorkerIdentitySPIFFE)
}

func generatedSupervisorIdentity() *workerv1alpha1.WorkloadIdentity {
	return identityFromSPIFFE(workerkernel.WorkerLocalDevLauncherSupervisorIdentitySPIFFE)
}

func identityFromSPIFFE(spiffe string) *workerv1alpha1.WorkloadIdentity {
	parsed, err := url.Parse(spiffe)
	trustDomain := "cloud-agents.localdev"
	if err == nil && parsed.Host != "" {
		trustDomain = parsed.Host
	}
	return &workerv1alpha1.WorkloadIdentity{SpiffeId: spiffe, TrustDomain: trustDomain}
}

func validateFixedIdentities(workerIdentity, supervisorIdentity *workerv1alpha1.WorkloadIdentity) error {
	expectedWorker := generatedWorkerIdentity()
	expectedSupervisor := generatedSupervisorIdentity()
	if workerIdentity == nil || supervisorIdentity == nil || workerIdentity.GetSpiffeId() != expectedWorker.GetSpiffeId() || supervisorIdentity.GetSpiffeId() != expectedSupervisor.GetSpiffeId() || workerIdentity.GetTrustDomain() != expectedWorker.GetTrustDomain() || supervisorIdentity.GetTrustDomain() != expectedSupervisor.GetTrustDomain() || !bytes.Equal(workerIdentity.GetLeafCertificateSha256(), expectedWorker.GetLeafCertificateSha256()) || !bytes.Equal(supervisorIdentity.GetLeafCertificateSha256(), expectedSupervisor.GetLeafCertificateSha256()) {
		return errInvalidWorkerConfig
	}
	if workerIdentity.GetTrustDomain() == "" || supervisorIdentity.GetTrustDomain() == "" {
		return errInvalidWorkerConfig
	}
	return nil
}

func cloneWorkerIdentity(identity *workerv1alpha1.WorkloadIdentity) *workerv1alpha1.WorkloadIdentity {
	if identity == nil {
		return nil
	}
	return &workerv1alpha1.WorkloadIdentity{SpiffeId: identity.GetSpiffeId(), TrustDomain: identity.GetTrustDomain(), LeafCertificateSha256: append([]byte(nil), identity.GetLeafCertificateSha256()...)}
}

func runLocalWorker(ctx context.Context, cfg localWorkerConfig) error {
	if ctx == nil {
		return errInvalidWorkerConfig
	}
	if err := validateLoopbackListen(cfg.listen); err != nil {
		return err
	}
	listener, err := net.Listen("tcp", cfg.listen)
	if err != nil {
		return err
	}
	defer listener.Close()
	// Bind the listener before creating the token file. A failed bind must not
	// leave an apparently valid credential behind for a process that never ran.
	built, err := newLocalWorkerHTTPServer(cfg)
	if err != nil {
		return err
	}
	built.Server.BaseContext = func(net.Listener) context.Context { return ctx }
	errorsCh := make(chan error, 1)
	go func() { errorsCh <- built.Server.Serve(listener) }()
	select {
	case <-ctx.Done():
		shutdownContext, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := built.Server.Shutdown(shutdownContext); err != nil {
			return err
		}
		return nil
	case err := <-errorsCh:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	}
}

func main() {
	if len(os.Args) == 2 && os.Args[1] == "--version" {
		_, _ = fmt.Printf("cloud-agents-worker %s\n", version)
		return
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := runMain(os.Args[1:], ctx); err != nil {
		_, _ = fmt.Fprintln(os.Stderr, "cloud-agents-worker: startup or shutdown failed")
		os.Exit(2)
	}
}

// runMain is kept separate from main so configuration and listener failures
// are testable without terminating the test process. It deliberately emits no
// token-bearing diagnostics and returns a non-nil error for every failure.
func runMain(args []string, ctx context.Context) error {
	if ctx == nil {
		return errInvalidWorkerConfig
	}
	cfg, err := parseLocalWorkerConfig(args)
	if err != nil {
		return err
	}
	return runLocalWorker(ctx, cfg)
}
