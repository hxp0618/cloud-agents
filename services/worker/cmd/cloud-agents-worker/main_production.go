//go:build !localdev

// cloud-agents-worker is the production Worker HTTP entry point. It accepts
// only verified mTLS clients and exposes the generated Worker contract.
package main

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
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

	workerv1alpha1 "github.com/hxp0618/cloud-agents/sdk/go/gen/cloudagents/worker/v1alpha1"
	workerkernel "github.com/hxp0618/cloud-agents/services/worker"
)

var errInvalidProductionWorkerConfig = errors.New("cloud-agents-worker/invalid_production_config")

type productionWorkerErrorWriter struct{ output io.Writer }

func (writer productionWorkerErrorWriter) Write(message []byte) (int, error) {
	if bytes.Contains(message, []byte("http: TLS handshake error from ")) && bytes.HasSuffix(message, []byte(": EOF\n")) {
		return len(message), nil
	}
	return writer.output.Write(message)
}

const (
	defaultProductionWorkerListen  = ":8091"
	maxProductionCABytes           = 1 << 20
	admissionLeaseIDEnvironment    = "CLOUD_AGENTS_ADMISSION_LEASE_ID"
	admissionGenerationEnvironment = "CLOUD_AGENTS_ADMISSION_GENERATION"
	admissionTokenEnvironment      = "CLOUD_AGENTS_ADMISSION_TOKEN"
)

type productionWorkerConfig struct {
	listen                     string
	tlsCertFile                string
	tlsKeyFile                 string
	clientCAFile               string
	workerSPIFFE               string
	runtimeCommand             string
	runtimeMaxSessions         int
	runtimeCredentialDirectory string
	admissionLeaseID           string
	admissionGeneration        uint64
	admissionToken             []byte
}

func parseProductionWorkerConfig(args []string, getenv func(string) string) (productionWorkerConfig, error) {
	set := flag.NewFlagSet("cloud-agents-worker", flag.ContinueOnError)
	set.SetOutput(io.Discard)
	listen := set.String("listen", defaultProductionWorkerListen, "mTLS listen address")
	tlsCert := set.String("tls-cert", "", "server certificate PEM file")
	tlsKey := set.String("tls-key", "", "server private key PEM file")
	clientCA := set.String("client-ca", "", "client CA certificate PEM file")
	workerSPIFFE := set.String("worker-spiffe-id", "", "worker SPIFFE identity")
	runtimeCommand := set.String("runtime-command", "", "Cloud Agent Runtime executable")
	runtimeMaxSessions := set.Int("runtime-max-sessions", workerkernel.DefaultRuntimeMaxSessions, "maximum concurrent Runtime sessions")
	runtimeCredentialDirectory := set.String("provider-credential-directory", "", "directory containing <providerKind>.json credentials")
	admissionLeaseID := set.String("admission-lease-id", "", "authoritative Runtime lease id")
	admissionGeneration := set.Uint64("admission-generation", 0, "authoritative Runtime fencing generation")
	if err := set.Parse(args); err != nil || set.NArg() != 0 {
		return productionWorkerConfig{}, errInvalidProductionWorkerConfig
	}
	if *admissionLeaseID == "" && getenv != nil {
		*admissionLeaseID = getenv(admissionLeaseIDEnvironment)
	}
	if *admissionGeneration == 0 && getenv != nil {
		value, err := strconv.ParseUint(getenv(admissionGenerationEnvironment), 10, 64)
		if err != nil {
			return productionWorkerConfig{}, errInvalidProductionWorkerConfig
		}
		*admissionGeneration = value
	}
	var admissionToken []byte
	if getenv != nil {
		admissionToken = []byte(getenv(admissionTokenEnvironment))
	}
	cfg := productionWorkerConfig{listen: *listen, tlsCertFile: *tlsCert, tlsKeyFile: *tlsKey, clientCAFile: *clientCA, workerSPIFFE: *workerSPIFFE, runtimeCommand: *runtimeCommand, runtimeMaxSessions: *runtimeMaxSessions, runtimeCredentialDirectory: *runtimeCredentialDirectory, admissionLeaseID: *admissionLeaseID, admissionGeneration: *admissionGeneration, admissionToken: admissionToken}
	if err := validateProductionWorkerConfig(cfg); err != nil {
		return productionWorkerConfig{}, err
	}
	return cfg, nil
}

func validateProductionWorkerConfig(cfg productionWorkerConfig) error {
	if err := validateProductionListen(cfg.listen); err != nil || cfg.tlsCertFile == "" || cfg.tlsKeyFile == "" || cfg.clientCAFile == "" || cfg.runtimeCommand == "" || cfg.runtimeMaxSessions < 1 || cfg.runtimeMaxSessions > workerkernel.MaxRuntimeSessions || !filepath.IsAbs(cfg.runtimeCredentialDirectory) || cfg.admissionLeaseID == "" || cfg.admissionGeneration == 0 || len(cfg.admissionToken) == 0 || len(cfg.admissionToken) > int(workerkernel.MaxPayloadBytes) {
		return errInvalidProductionWorkerConfig
	}
	if strings.TrimSpace(cfg.tlsCertFile) != cfg.tlsCertFile || strings.TrimSpace(cfg.tlsKeyFile) != cfg.tlsKeyFile || strings.TrimSpace(cfg.clientCAFile) != cfg.clientCAFile || strings.TrimSpace(cfg.runtimeCommand) != cfg.runtimeCommand || strings.TrimSpace(cfg.runtimeCredentialDirectory) != cfg.runtimeCredentialDirectory || strings.TrimSpace(cfg.admissionLeaseID) != cfg.admissionLeaseID {
		return errInvalidProductionWorkerConfig
	}
	if _, err := productionIdentity(cfg.workerSPIFFE); err != nil {
		return errInvalidProductionWorkerConfig
	}
	return nil
}

func validateProductionListen(address string) error {
	_, port, err := net.SplitHostPort(address)
	if err != nil {
		return errInvalidProductionWorkerConfig
	}
	portNumber, err := strconv.Atoi(port)
	if err != nil || portNumber < 1 || portNumber > 65535 {
		return errInvalidProductionWorkerConfig
	}
	return nil
}

func productionIdentity(value string) (*workerv1alpha1.WorkloadIdentity, error) {
	if strings.TrimSpace(value) != value || value == "" {
		return nil, errInvalidProductionWorkerConfig
	}
	parsed, err := url.Parse(value)
	if err != nil || parsed.Scheme != "spiffe" || parsed.Host == "" || parsed.Path == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return nil, errInvalidProductionWorkerConfig
	}
	return &workerv1alpha1.WorkloadIdentity{SpiffeId: value, TrustDomain: parsed.Host}, nil
}

func readProductionCAPool(path string) (*x509.CertPool, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, errInvalidProductionWorkerConfig
	}
	defer file.Close()
	contents, err := io.ReadAll(io.LimitReader(file, maxProductionCABytes+1))
	if err != nil || len(contents) > maxProductionCABytes {
		return nil, errInvalidProductionWorkerConfig
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(contents) {
		return nil, errInvalidProductionWorkerConfig
	}
	return pool, nil
}

func runProductionWorker(ctx context.Context, cfg productionWorkerConfig) error {
	if ctx == nil || validateProductionWorkerConfig(cfg) != nil {
		return errInvalidProductionWorkerConfig
	}
	identity, err := productionIdentity(cfg.workerSPIFFE)
	if err != nil {
		return errInvalidProductionWorkerConfig
	}
	certificate, err := tls.LoadX509KeyPair(cfg.tlsCertFile, cfg.tlsKeyFile)
	if err != nil {
		return errInvalidProductionWorkerConfig
	}
	clientCAs, err := readProductionCAPool(cfg.clientCAFile)
	if err != nil {
		return err
	}
	service, err := workerkernel.NewService(workerkernel.Config{
		WorkerIdentity:             identity,
		Capabilities:               productionWorkerCapabilities(),
		IdentityProvider:           workerkernel.TLSIdentityProvider{},
		RuntimeCommand:             []string{cfg.runtimeCommand},
		RuntimeMaxSessions:         cfg.runtimeMaxSessions,
		RuntimeEnvironment:         productionRuntimeEnvironment(os.Environ()),
		RuntimeCredentialDirectory: cfg.runtimeCredentialDirectory,
		AdmissionLeaseID:           cfg.admissionLeaseID,
		AdmissionGeneration:        cfg.admissionGeneration,
		AdmissionToken:             cfg.admissionToken,
	})
	if err != nil {
		return errInvalidProductionWorkerConfig
	}
	connectPath, connectHandler := workerkernel.NewHandler(service)
	runtimePath, runtimeHandler := workerkernel.NewRuntimeHandler(service)
	mux := http.NewServeMux()
	mux.Handle(connectPath, connectHandler)
	mux.Handle(runtimePath, runtimeHandler)
	server := &http.Server{
		Addr:        cfg.listen,
		Handler:     workerkernel.NewTLSHandler(mux),
		BaseContext: func(net.Listener) context.Context { return ctx },
		ErrorLog:    log.New(productionWorkerErrorWriter{output: os.Stderr}, "", log.LstdFlags),
		IdleTimeout: 60 * time.Second,
		TLSConfig: &tls.Config{
			MinVersion:   tls.VersionTLS13,
			Certificates: []tls.Certificate{certificate},
			ClientAuth:   tls.RequireAndVerifyClientCert,
			ClientCAs:    clientCAs,
		},
	}
	listener, err := net.Listen("tcp", cfg.listen)
	if err != nil {
		return err
	}
	serve := make(chan error, 1)
	go func() { serve <- server.ServeTLS(listener, "", "") }()
	select {
	case <-ctx.Done():
		shutdownContext, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdownContext); err != nil {
			return err
		}
		return nil
	case err := <-serve:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	}
}

func productionRuntimeEnvironment(source []string) []string {
	filtered := make([]string, 0, len(source))
	for _, entry := range source {
		name, _, found := strings.Cut(entry, "=")
		if found && productionRuntimeEnvironmentAllowed(name) {
			filtered = append(filtered, entry)
		}
	}
	return filtered
}

func productionRuntimeEnvironmentAllowed(name string) bool {
	switch name {
	case "PATH", "HOME", "CODEX_HOME", "USER", "LOGNAME", "USERNAME", "USERPROFILE", "HOMEDRIVE", "HOMEPATH",
		"TMPDIR", "TMP", "TEMP", "SYSTEMROOT", "WINDIR", "COMSPEC", "PATHEXT",
		"LANG", "LANGUAGE", "LC_ALL", "LC_CTYPE", "LC_COLLATE", "LC_MESSAGES", "LC_MONETARY",
		"LC_NUMERIC", "LC_TIME", "LC_PAPER", "LC_NAME", "LC_ADDRESS", "LC_TELEPHONE",
		"LC_MEASUREMENT", "LC_IDENTIFICATION", "TZ", "TERM", "COLORTERM", "TERM_PROGRAM",
		"TERM_PROGRAM_VERSION", "SHELL", "NO_COLOR", "FORCE_COLOR", "CLICOLOR", "CLICOLOR_FORCE",
		"SSL_CERT_FILE", "SSL_CERT_DIR", "NODE_EXTRA_CA_CERTS",
		"CLOUD_AGENT_PROVIDER_OUTER_SANDBOX_PROFILE",
		"CLOUD_AGENT_PROVIDER_HOST_EXPERIMENTAL_PROVIDERS",
		"CLOUD_AGENT_PROVIDER_HTTP_PROXY", "CLOUD_AGENT_PROVIDER_HTTPS_PROXY",
		"CLOUD_AGENT_PROVIDER_ALL_PROXY", "CLOUD_AGENT_PROVIDER_NO_PROXY",
		"CLOUD_AGENT_PROVIDER_NPM_CONFIG_USERCONFIG", "CLOUD_AGENT_PROVIDER_PIP_CONFIG_FILE":
		return true
	default:
		return false
	}
}

func productionWorkerCapabilities() []workerv1alpha1.Capability {
	return []workerv1alpha1.Capability{
		workerv1alpha1.Capability_CAPABILITY_NEGOTIATION,
		workerv1alpha1.Capability_CAPABILITY_HEALTH,
	}
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := runProductionMain(os.Args[1:], ctx); err != nil {
		_, _ = fmt.Fprintln(os.Stderr, "cloud-agents-worker: startup or shutdown failed")
		os.Exit(2)
	}
}

func runProductionMain(args []string, ctx context.Context) error {
	cfg, err := parseProductionWorkerConfig(args, os.Getenv)
	if err != nil {
		return err
	}
	return runProductionWorker(ctx, cfg)
}
