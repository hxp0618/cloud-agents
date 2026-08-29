package supervisor

import (
	"bytes"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	workerruntimev1alpha1connect "github.com/hxp0618/cloud-agents/sdk/go/gen/cloudagents/worker/runtime/v1alpha1/workerruntimev1alpha1connect"
	workerv1alpha1 "github.com/hxp0618/cloud-agents/sdk/go/gen/cloudagents/worker/v1alpha1"
	workerv1alpha1connect "github.com/hxp0618/cloud-agents/sdk/go/gen/cloudagents/worker/v1alpha1/workerv1alpha1connect"
	workerkernel "github.com/hxp0618/cloud-agents/services/worker"
)

var errInvalidMTLSConfig = errors.New("worker_supervisor/mtls_config_invalid")

// MTLSConfig is the complete transport configuration for a production
// Supervisor client. RootCAs and a client certificate are mandatory; no
// insecure or environment-selected trust path is available.
type MTLSConfig struct {
	Endpoint               string
	ExpectedWorkerIdentity *workerv1alpha1.WorkloadIdentity
	ClientCertificate      tls.Certificate
	RootCAs                *x509.CertPool
	ServerName             string
	Clock                  Clock
}

// NewMTLS constructs a Supervisor over an HTTPS Connect client authenticated
// with the supplied client certificate and explicit CA pool. It performs no
// network I/O; Bind performs the first handshake.
func NewMTLS(config MTLSConfig) (*Supervisor, error) {
	endpoint, err := validateMTLSEndpoint(config.Endpoint)
	if err != nil || !validIdentity(config.ExpectedWorkerIdentity) || config.RootCAs == nil || len(config.ClientCertificate.Certificate) == 0 || config.ClientCertificate.PrivateKey == nil {
		return nil, errInvalidMTLSConfig
	}
	serverName := config.ServerName
	if serverName == "" {
		parsed, _ := url.Parse(endpoint)
		serverName = parsed.Hostname()
	}
	if strings.TrimSpace(serverName) != serverName || serverName == "" {
		return nil, errInvalidMTLSConfig
	}
	expectedServerIdentity := cloneIdentity(config.ExpectedWorkerIdentity)
	transport := &http.Transport{
		Proxy:                 nil,
		DialContext:           (&net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
		ForceAttemptHTTP2:     true,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ResponseHeaderTimeout: 15 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
		TLSClientConfig: &tls.Config{
			MinVersion:   tls.VersionTLS13,
			RootCAs:      config.RootCAs,
			Certificates: []tls.Certificate{config.ClientCertificate},
			ServerName:   serverName,
			// The server certificate must be validated by RootCAs.
			InsecureSkipVerify: false,
			VerifyConnection: func(state tls.ConnectionState) error {
				if len(state.PeerCertificates) == 0 {
					return errInvalidMTLSConfig
				}
				actual, err := workerkernel.PeerIdentityFromCertificate(state.PeerCertificates[0])
				if err != nil || actual.GetSpiffeId() != expectedServerIdentity.GetSpiffeId() || actual.GetTrustDomain() != expectedServerIdentity.GetTrustDomain() || (len(expectedServerIdentity.GetLeafCertificateSha256()) != 0 && !bytes.Equal(actual.GetLeafCertificateSha256(), expectedServerIdentity.GetLeafCertificateSha256())) {
					return errInvalidMTLSConfig
				}
				return nil
			},
		},
	}
	client := newMTLSHTTPClient(transport)
	return New(Config{
		Client:                 workerv1alpha1connect.NewWorkerExecutionServiceClient(client, endpoint),
		RuntimeClient:          workerruntimev1alpha1connect.NewWorkerRuntimeServiceClient(client, endpoint),
		ExpectedWorkerIdentity: config.ExpectedWorkerIdentity,
		Clock:                  config.Clock,
	})
}

func newMTLSHTTPClient(transport http.RoundTripper) *http.Client {
	return &http.Client{Transport: transport, CheckRedirect: func(*http.Request, []*http.Request) error { return errInvalidMTLSConfig }}
}

func validateMTLSEndpoint(value string) (string, error) {
	if strings.TrimSpace(value) != value || value == "" || !strings.HasPrefix(value, "https://") {
		return "", errInvalidMTLSConfig
	}
	parsed, err := url.Parse(value)
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" || (parsed.Path != "" && parsed.Path != "/") {
		return "", errInvalidMTLSConfig
	}
	return strings.TrimSuffix(value, "/"), nil
}
