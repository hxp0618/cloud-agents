package supervisor

// This file is the narrow Supervisor-side consumer for the generated D-057
// localdev launcher profile.  It is deliberately separate from the in-process
// D-054 dispatch path: the launcher is a loopback HTTP process boundary, not a
// public endpoint and not a durable execution transport.

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"syscall"
	"time"
	"unicode"
	"unicode/utf8"

	"connectrpc.com/connect"
	workerv1alpha1 "github.com/hxp0618/cloud-agents/sdk/go/gen/cloudagents/worker/v1alpha1"
	workerv1alpha1connect "github.com/hxp0618/cloud-agents/sdk/go/gen/cloudagents/worker/v1alpha1/workerv1alpha1connect"
	workerkernel "github.com/hxp0618/cloud-agents/services/worker"
)

var (
	errLocalLauncherProfile  = errors.New("worker_supervisor/local_launcher_profile_invalid")
	errLocalLauncherURL      = errors.New("worker_supervisor/local_launcher_url_invalid")
	errLocalLauncherToken    = errors.New("worker_supervisor/local_launcher_token_invalid")
	errLocalLauncherHealth   = errors.New("worker_supervisor/local_launcher_health_invalid")
	errLocalLauncherRedirect = errors.New("worker_supervisor/local_launcher_redirect_rejected")
)

const (
	localLauncherMaxTokenBytes = 256
	localLauncherHTTPTimeout   = 15 * time.Second
)

// LocalLauncherConfig is intentionally limited to a loopback URL and a
// pre-created 0600 token file. A caller may provide only inert HTTP client
// resource settings; profile, identity, lease, proxy, dial, and protocol hooks
// are never accepted.
type LocalLauncherConfig struct {
	Endpoint   string
	TokenFile  string
	Clock      Clock
	HTTPClient *http.Client
}

// LocalLauncher is a read/health binding to the D-057 localdev process. The
// operation RPCs remain explicitly unimplemented because the D-056 launcher
// service has no executor and no durable receipt store.
type LocalLauncher struct {
	supervisor *Supervisor
	client     workerv1alpha1connect.WorkerExecutionServiceClient
	endpoint   string
	httpClient *http.Client
}

// NewLocalLauncher validates all immutable inputs and performs no network I/O.
func NewLocalLauncher(config LocalLauncherConfig) (*LocalLauncher, error) {
	if !workerkernel.WorkerLocalDevBridgeProfileValid() {
		return nil, errLocalLauncherProfile
	}
	endpoint, err := validateLocalLauncherEndpoint(config.Endpoint)
	if err != nil {
		return nil, err
	}
	token, err := readLocalLauncherToken(config.TokenFile)
	if err != nil {
		return nil, err
	}
	workerID, err := localLauncherIdentity(workerkernel.WorkerLocalDevLauncherWorkerIdentitySPIFFE)
	if err != nil {
		return nil, errLocalLauncherProfile
	}
	httpClient := &http.Client{}
	if config.HTTPClient != nil {
		copyClient := *config.HTTPClient
		httpClient = &copyClient
	}
	httpClient.Timeout = localLauncherHTTPTimeout
	httpClient.CheckRedirect = func(*http.Request, []*http.Request) error { return errLocalLauncherRedirect }
	// A caller-provided Jar can execute arbitrary policy code and can retain
	// credentials. Cookies are not part of this fixed localdev contract.
	httpClient.Jar = nil
	transport := httpClient.Transport
	if transport == nil {
		transport = http.DefaultTransport
	}
	base, ok := transport.(*http.Transport)
	if !ok {
		return nil, errLocalLauncherURL
	}
	if !safeLocalLauncherTransport(base, transport == http.DefaultTransport) {
		return nil, errLocalLauncherURL
	}
	copyTransport := base.Clone()
	copyTransport.Proxy = nil
	// Do not carry custom dialing, TLS, proxy, or protocol hooks across the
	// boundary. A custom *http.Transport is accepted only as a test/resource
	// configuration shape; the destination remains the validated loopback URL.
	copyTransport.OnProxyConnectResponse = nil
	copyTransport.DialContext = nil
	copyTransport.Dial = nil
	copyTransport.DialTLSContext = nil
	copyTransport.DialTLS = nil
	copyTransport.TLSClientConfig = nil
	copyTransport.TLSNextProto = nil
	copyTransport.ProxyConnectHeader = nil
	copyTransport.GetProxyConnectHeader = nil
	copyTransport.ForceAttemptHTTP2 = false
	copyTransport.HTTP2 = nil
	copyTransport.Protocols = nil
	transport = copyTransport
	httpClient.Transport = &localLauncherRoundTripper{base: transport, token: token, endpoint: endpoint}
	client := workerv1alpha1connect.NewWorkerExecutionServiceClient(httpClient, endpoint)
	s, err := New(Config{Client: client, ExpectedWorkerIdentity: workerID, Clock: config.Clock})
	if err != nil {
		return nil, err
	}
	return &LocalLauncher{supervisor: s, client: client, endpoint: endpoint, httpClient: httpClient}, nil
}

func safeLocalLauncherTransport(transport *http.Transport, isDefault bool) bool {
	if transport == nil {
		return false
	}
	// The package default intentionally has ProxyFromEnvironment; it is
	// neutralized by the clone below. Any caller-selected proxy or hook is
	// rejected instead of silently trusted. Hook checks also apply when a
	// caller passes the mutable package-default pointer back to us.
	if isDefault {
		return true
	}
	if transport.OnProxyConnectResponse != nil || transport.DialContext != nil ||
		transport.Dial != nil || transport.DialTLSContext != nil || transport.DialTLS != nil ||
		transport.TLSClientConfig != nil || transport.TLSNextProto != nil ||
		transport.ProxyConnectHeader != nil || transport.GetProxyConnectHeader != nil ||
		transport.ForceAttemptHTTP2 || transport.HTTP2 != nil || transport.Protocols != nil ||
		(!isDefault && transport.Proxy != nil) {
		return false
	}
	return true
}

// BindLocalLauncherDispatch first verifies the launcher health document, then
// negotiates and health-checks the fixed v1.0 compatibility profile. Health is
// intentionally checked before either Connect RPC is sent.
func (l *LocalLauncher) BindLocalLauncherDispatch(ctx context.Context) (BindingSnapshot, error) {
	if l == nil || l.supervisor == nil || l.client == nil {
		return BindingSnapshot{}, errInvalidConfig
	}
	if err := contextErr(ctx); err != nil {
		return BindingSnapshot{}, err
	}
	if err := l.checkLauncherHealth(ctx); err != nil {
		return BindingSnapshot{}, err
	}
	snapshot, err := l.supervisor.Bind(ctx)
	if err != nil {
		return BindingSnapshot{}, err
	}
	if _, err := l.supervisor.CheckHealth(ctx); err != nil {
		return BindingSnapshot{}, err
	}
	return snapshot, nil
}

// DispatchOperation and GetOperationReceipt are deliberately not transport
// bridges. D-056's default launcher has no executor or durable store.
func (l *LocalLauncher) DispatchOperation(ctx context.Context, _ *workerv1alpha1.OperationAttemptEnvelope) (*connect.Response[workerv1alpha1.DurableReceipt], error) {
	if err := contextErr(ctx); err != nil {
		return nil, err
	}
	return nil, fail(connect.CodeUnimplemented, "operation_dispatch_not_implemented")
}

func (l *LocalLauncher) GetOperationReceipt(ctx context.Context, _ *workerv1alpha1.ReceiptRequest) (*connect.Response[workerv1alpha1.DurableReceipt], error) {
	if err := contextErr(ctx); err != nil {
		return nil, err
	}
	return nil, fail(connect.CodeUnimplemented, "durable_receipts_not_implemented")
}

type localLauncherHealth struct {
	APIVersion          string `json:"api_version"`
	Authority           string `json:"authority"`
	Profile             string `json:"profile"`
	Revision            string `json:"revision"`
	ProfileDigest       string `json:"profile_digest"`
	Version             string `json:"version"`
	Status              string `json:"status"`
	WorkerIdentity      string `json:"worker_identity"`
	SupervisorIdentity  string `json:"supervisor_identity"`
	LeaseID             string `json:"lease_id"`
	Generation          uint64 `json:"generation"`
	ExternalSideEffects bool   `json:"external_side_effects"`
	Transport           string `json:"transport"`
}

func (l *LocalLauncher) checkLauncherHealth(ctx context.Context) error {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, l.endpoint+workerkernel.WorkerLocalDevBridgeHealthRoute, nil)
	if err != nil {
		return errLocalLauncherHealth
	}
	response, err := l.httpClient.Do(request)
	if err != nil {
		return err
	}
	if response == nil || response.Body == nil || response.StatusCode != http.StatusOK {
		return errLocalLauncherHealth
	}
	defer response.Body.Close()
	mediaType, _, mediaErr := mime.ParseMediaType(response.Header.Get("Content-Type"))
	if response.ContentLength > localLauncherMaxHealthBytes || mediaErr != nil || !strings.EqualFold(mediaType, "application/json") {
		return errLocalLauncherHealth
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, localLauncherMaxHealthBytes+1))
	if err != nil || len(body) > localLauncherMaxHealthBytes {
		return errLocalLauncherHealth
	}
	var health localLauncherHealth
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&health); err != nil {
		return errLocalLauncherHealth
	}
	var extra struct{}
	if decoder.Decode(&extra) != io.EOF {
		return errLocalLauncherHealth
	}
	if health.APIVersion != "cloud-agents.localdev/v1" || health.Authority != workerkernel.WorkerLocalDevLauncherAuthorityID || health.Profile != workerkernel.WorkerLocalDevLauncherProfileID || health.Revision != workerkernel.WorkerLocalDevLauncherRevision || health.ProfileDigest != workerkernel.WorkerLocalDevLauncherProfileDigest || health.Version != "v1.0" || health.Status != "serving" || health.WorkerIdentity != workerkernel.WorkerLocalDevLauncherWorkerIdentitySPIFFE || health.SupervisorIdentity != workerkernel.WorkerLocalDevLauncherSupervisorIdentitySPIFFE || health.LeaseID != workerkernel.WorkerLocalDevLauncherLeaseID || health.Generation != workerkernel.WorkerLocalDevLauncherGeneration || health.ExternalSideEffects || health.Transport != "loopback_http_connect" {
		return errLocalLauncherHealth
	}
	return nil
}

func validateLocalLauncherEndpoint(raw string) (string, error) {
	if raw == "" || strings.TrimSpace(raw) != raw {
		return "", errLocalLauncherURL
	}
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "http" || u.Host == "" || u.User != nil || u.RawPath != "" || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || u.Opaque != "" || (u.Path != "" && u.Path != "/") {
		return "", errLocalLauncherURL
	}
	host := u.Hostname()
	ip := net.ParseIP(host)
	if ip == nil || !ip.IsLoopback() {
		return "", errLocalLauncherURL
	}
	portText := u.Port()
	if portText == "" {
		return "", errLocalLauncherURL
	}
	for _, character := range portText {
		if character < '0' || character > '9' {
			return "", errLocalLauncherURL
		}
	}
	port, err := strconv.Atoi(portText)
	if err != nil || port < 1 || port > 65535 {
		return "", errLocalLauncherURL
	}
	u.Path = ""
	u.RawPath = ""
	return strings.TrimRight(u.String(), "/"), nil
}

func readLocalLauncherToken(path string) ([]byte, error) {
	if path == "" || strings.TrimSpace(path) != path {
		return nil, errLocalLauncherToken
	}
	info, err := os.Lstat(path)
	if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() || info.Mode().Perm() != 0o600 {
		return nil, errLocalLauncherToken
	}
	file, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return nil, errLocalLauncherToken
	}
	defer file.Close()
	stat, err := file.Stat()
	if err != nil || !os.SameFile(info, stat) || stat.Mode()&os.ModeSymlink != 0 || !stat.Mode().IsRegular() || stat.Mode().Perm() != 0o600 {
		return nil, errLocalLauncherToken
	}
	initialSize := stat.Size()
	data, err := io.ReadAll(io.LimitReader(file, localLauncherMaxTokenBytes+2))
	if err != nil || len(data) == 0 || len(data) > localLauncherMaxTokenBytes+1 || data[len(data)-1] != '\n' {
		return nil, errLocalLauncherToken
	}
	data = data[:len(data)-1]
	if !validLocalLauncherToken(data) {
		return nil, errLocalLauncherToken
	}
	finalStat, statErr := file.Stat()
	if statErr != nil || !os.SameFile(info, finalStat) || finalStat.Size() != initialSize || finalStat.Mode().Perm() != 0o600 {
		return nil, errLocalLauncherToken
	}
	return append([]byte(nil), data...), nil
}

func validLocalLauncherToken(data []byte) bool {
	if !utf8.Valid(data) || len(data) == 0 || len(data) > localLauncherMaxTokenBytes {
		return false
	}
	value := string(data)
	if strings.TrimSpace(value) != value || strings.ContainsAny(value, " \t\r\n") {
		return false
	}
	for _, r := range value {
		if unicode.IsControl(r) {
			return false
		}
	}
	return true
}

func localLauncherIdentity(spiffe string) (*workerv1alpha1.WorkloadIdentity, error) {
	const prefix = "spiffe://"
	if !strings.HasPrefix(spiffe, prefix) {
		return nil, errLocalLauncherProfile
	}
	rest := strings.TrimPrefix(spiffe, prefix)
	host, _, ok := strings.Cut(rest, "/")
	if !ok || host == "" {
		return nil, errLocalLauncherProfile
	}
	return &workerv1alpha1.WorkloadIdentity{SpiffeId: spiffe, TrustDomain: host}, nil
}

type localLauncherRoundTripper struct {
	base     http.RoundTripper
	token    []byte
	endpoint string
}

func (t *localLauncherRoundTripper) RoundTrip(request *http.Request) (*http.Response, error) {
	if t == nil || t.base == nil || len(t.token) == 0 || request == nil || request.URL == nil {
		return nil, errLocalLauncherURL
	}
	expected, err := url.Parse(t.endpoint)
	if err != nil || request.URL.Scheme != expected.Scheme || request.URL.Host != expected.Host || (request.Host != "" && request.Host != expected.Host) || request.RequestURI != "" || request.URL.User != nil || request.URL.Opaque != "" || request.URL.RawPath != "" || request.URL.RawFragment != "" || request.URL.ForceQuery || request.URL.RawQuery != "" || request.URL.Fragment != "" || !isLocalLauncherRoute(request.Method, request.URL.Path) {
		return nil, errLocalLauncherURL
	}
	clone := request.Clone(request.Context())
	clone.Header = request.Header.Clone()
	if clone.Header == nil {
		clone.Header = make(http.Header)
	}
	clone.Host = expected.Host
	clone.Header.Set("Authorization", "Bearer "+string(t.token))
	return t.base.RoundTrip(clone)
}

const localLauncherMaxHealthBytes = 64 * 1024

func isLocalLauncherRoute(method, path string) bool {
	switch path {
	case workerkernel.WorkerLocalDevBridgeHealthRoute:
		return method == http.MethodGet
	case workerv1alpha1connect.WorkerExecutionServiceNegotiateProcedure,
		workerv1alpha1connect.WorkerExecutionServiceCheckHealthProcedure:
		return method == http.MethodPost
	default:
		return false
	}
}
