package main

import (
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"net"
	"net/http"
	"sync"
	"time"
)

type fileProviderMaterials struct {
	secretFiles map[string]string
	rootCAFiles map[string]string

	mu      sync.Mutex
	clients map[string]pooledProviderClient
}

// Clients are reused so provider connections are pooled; the root file is
// still read on every call so a rotated CA takes effect without a restart.
type pooledProviderClient struct {
	rootsSHA256 [sha256.Size]byte
	client      *http.Client
}

func newFileProviderMaterials(secretFiles, rootCAFiles map[string]string) (*fileProviderMaterials, error) {
	if secretFiles == nil || rootCAFiles == nil || len(secretFiles) > 128 || len(rootCAFiles) > 128 {
		return nil, errors.New("provider material configuration is invalid")
	}
	return &fileProviderMaterials{
		secretFiles: cloneReferenceFiles(secretFiles),
		rootCAFiles: cloneReferenceFiles(rootCAFiles),
		clients:     make(map[string]pooledProviderClient),
	}, nil
}

func (materials *fileProviderMaterials) ClientSecret(reference string) (string, error) {
	path, ok := materials.secretFiles[reference]
	if !ok {
		return "", errors.New("provider client secret is unavailable")
	}
	secret, err := readSecretText(path, maximumIdentitySecretBytes)
	if err != nil {
		return "", errors.New("provider client secret is unavailable")
	}
	return secret, nil
}

func (materials *fileProviderMaterials) HTTPClient(reference string) (*http.Client, error) {
	var certificatePEM []byte
	if reference != "" {
		path, ok := materials.rootCAFiles[reference]
		if !ok {
			return nil, errors.New("provider root CAs are unavailable")
		}
		var err error
		certificatePEM, err = readRegularFile(path, maximumCertificateBytes, false)
		if err != nil {
			return nil, errors.New("provider root CAs are unavailable")
		}
	}
	rootsSHA256 := sha256.Sum256(certificatePEM)
	materials.mu.Lock()
	defer materials.mu.Unlock()
	if pooled, ok := materials.clients[reference]; ok {
		if pooled.rootsSHA256 == rootsSHA256 {
			return pooled.client, nil
		}
		pooled.client.CloseIdleConnections()
		delete(materials.clients, reference)
	}
	client, err := newProviderHTTPClient(reference, certificatePEM)
	if err != nil {
		return nil, err
	}
	materials.clients[reference] = pooledProviderClient{rootsSHA256: rootsSHA256, client: client}
	return client, nil
}

func newProviderHTTPClient(reference string, certificatePEM []byte) (*http.Client, error) {
	var roots *x509.CertPool
	if reference != "" {
		roots = x509.NewCertPool()
		if !roots.AppendCertsFromPEM(certificatePEM) {
			return nil, errors.New("provider root CAs are unavailable")
		}
	}
	transport := &http.Transport{
		Proxy: http.ProxyFromEnvironment, ForceAttemptHTTP2: true, IdleConnTimeout: 60 * time.Second,
		TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12, RootCAs: roots},
		DialContext:     (&net.Dialer{Timeout: 5 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
	}
	return &http.Client{Transport: transport, Timeout: identityHTTPTimeout}, nil
}

func cloneReferenceFiles(input map[string]string) map[string]string {
	result := make(map[string]string, len(input))
	for reference, path := range input {
		result[reference] = path
	}
	return result
}
