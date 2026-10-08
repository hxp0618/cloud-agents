package dockertarget

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestVerifyFoundationSandboxIsolation(t *testing.T) {
	runtimeID := "runtime-1"
	runtime := "runsc"
	internal := true
	icc := "false"
	server := httptest.NewTLSServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		switch request.URL.Path {
		case "/containers/json":
			_ = json.NewEncoder(writer).Encode([]map[string]string{{"Id": "container-1"}})
		case "/containers/container-1/json":
			_ = json.NewEncoder(writer).Encode(map[string]any{"Id": "container-1", "Config": map[string]any{"Labels": map[string]string{"opensandbox.io/id": runtimeID}}, "State": map[string]bool{"Running": true}, "HostConfig": map[string]string{"Runtime": runtime}, "NetworkSettings": map[string]any{"Networks": map[string]any{"isolated": map[string]string{"NetworkID": "network-1"}}}})
		case "/containers/container-1/stats":
			_ = json.NewEncoder(writer).Encode(map[string]any{"networks": map[string]any{
				"eth0": map[string]int64{"rx_bytes": 1234, "tx_bytes": 567},
				"eth1": map[string]int64{"rx_bytes": 6, "tx_bytes": 8},
			}})
		case "/networks/network-1":
			_ = json.NewEncoder(writer).Encode(map[string]any{"Id": "network-1", "Internal": internal, "Options": map[string]string{"com.docker.network.bridge.enable_icc": icc}})
		default:
			http.NotFound(writer, request)
		}
	}))
	defer server.Close()
	directory := credentialDirectoryForIsolationTest(t, server)
	if err := directory.VerifyFoundationSandboxIsolation(context.Background(), server.URL, "docker", runtimeID); err != nil {
		t.Fatal(err)
	}
	received, transmitted, err := directory.MeasureFoundationSandboxNetwork(context.Background(), server.URL, "docker", runtimeID)
	if err != nil || received != 1240 || transmitted != 575 {
		t.Fatalf("network usage = %d/%d err=%v", received, transmitted, err)
	}
	for name, mutate := range map[string]func(){
		"runtime":  func() { runtime = "runc" },
		"internal": func() { internal = false },
		"icc":      func() { icc = "true" },
	} {
		runtime, internal, icc = "runsc", true, "false"
		mutate()
		if err := directory.VerifyFoundationSandboxIsolation(context.Background(), server.URL, "docker", runtimeID); !errors.Is(err, ErrIsolationUnenforced) {
			t.Fatalf("%s drift error=%v", name, err)
		}
	}
}

func TestMeasureFoundationSandboxSharedNetwork(t *testing.T) {
	const runtimeID = "runtime-1"
	workloadID, sidecarID := strings.Repeat("a", 64), strings.Repeat("b", 64)
	for _, test := range []struct {
		name, mode, inspectID, ownerID, ownerLabel, ownerMode string
		stopped, missing, unbound, empty, negative, overflow  bool
		wantError                                             error
	}{
		{name: "bound sidecar"},
		{name: "workload inspect ID mismatch", inspectID: strings.Repeat("c", 64), wantError: ErrDeploymentConflict},
		{name: "no shared namespace", mode: "bridge", wantError: ErrDeploymentFailed},
		{name: "name instead of immutable ID", mode: "container:egress", wantError: ErrDeploymentConflict},
		{name: "short ID", mode: "container:" + sidecarID[:12], wantError: ErrDeploymentConflict},
		{name: "uppercase ID", mode: "container:" + strings.ToUpper(sidecarID), wantError: ErrDeploymentConflict},
		{name: "nonhex ID", mode: "container:" + strings.Repeat("z", 64), wantError: ErrDeploymentConflict},
		{name: "self reference", mode: "container:" + workloadID, wantError: ErrDeploymentConflict},
		{name: "missing owner", missing: true, wantError: ErrDeploymentConflict},
		{name: "owner ID mismatch", ownerID: strings.Repeat("c", 64), wantError: ErrDeploymentConflict},
		{name: "foreign owner", ownerLabel: "other-runtime", wantError: ErrDeploymentConflict},
		{name: "missing owner label", unbound: true, wantError: ErrDeploymentConflict},
		{name: "stopped owner", stopped: true, wantError: ErrDeploymentConflict},
		{name: "nested namespace", ownerMode: "container:" + strings.Repeat("c", 64), wantError: ErrDeploymentConflict},
		{name: "empty owner counters", empty: true, wantError: ErrDeploymentFailed},
		{name: "negative owner counters", negative: true, wantError: ErrDeploymentFailed},
		{name: "overflow owner counters", overflow: true, wantError: ErrDeploymentFailed},
	} {
		t.Run(test.name, func(t *testing.T) {
			mode, ownerID, ownerLabel := test.mode, test.ownerID, test.ownerLabel
			inspectID := test.inspectID
			if inspectID == "" {
				inspectID = workloadID
			}
			if mode == "" {
				mode = "container:" + sidecarID
			}
			if ownerID == "" {
				ownerID = sidecarID
			}
			if ownerLabel == "" && !test.unbound {
				ownerLabel = runtimeID
			}
			server := httptest.NewTLSServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				writer.Header().Set("Content-Type", "application/json")
				switch request.URL.Path {
				case "/containers/json":
					_ = json.NewEncoder(writer).Encode([]map[string]string{{"Id": workloadID}})
				case "/containers/" + workloadID + "/json":
					_ = json.NewEncoder(writer).Encode(map[string]any{"Id": inspectID, "Config": map[string]any{"Labels": map[string]string{"opensandbox.io/id": runtimeID}}, "State": map[string]bool{"Running": true}, "HostConfig": map[string]string{"NetworkMode": mode}})
				case "/containers/" + workloadID + "/stats":
					_, _ = writer.Write([]byte(`{"networks":{}}`))
				case "/containers/" + sidecarID + "/json":
					if test.missing {
						http.NotFound(writer, request)
						return
					}
					_ = json.NewEncoder(writer).Encode(map[string]any{"Id": ownerID, "Config": map[string]any{"Labels": map[string]string{"opensandbox.io/egress-sidecar-for": ownerLabel}}, "State": map[string]bool{"Running": !test.stopped}, "HostConfig": map[string]string{"NetworkMode": test.ownerMode}})
				case "/containers/" + sidecarID + "/stats":
					networks := map[string]map[string]int64{"eth0": {"rx_bytes": 1234, "tx_bytes": 567}, "eth1": {"rx_bytes": 6, "tx_bytes": 8}}
					if test.empty {
						networks = nil
					}
					if test.negative {
						networks["eth0"]["rx_bytes"] = -1
					}
					if test.overflow {
						networks["eth0"]["tx_bytes"] = 1 << 60
					}
					_ = json.NewEncoder(writer).Encode(map[string]any{"networks": networks})
				default:
					http.NotFound(writer, request)
				}
			}))
			defer server.Close()
			directory := credentialDirectoryForIsolationTest(t, server)
			received, transmitted, err := directory.MeasureFoundationSandboxNetwork(context.Background(), server.URL, "docker", runtimeID)
			if !errors.Is(err, test.wantError) {
				t.Fatalf("error = %v, want %v", err, test.wantError)
			}
			if test.wantError == nil && (received != 1240 || transmitted != 575) {
				t.Fatalf("network usage = %d/%d", received, transmitted)
			}
			if test.wantError != nil && (received != 0 || transmitted != 0) {
				t.Fatalf("rejected counters = %d/%d", received, transmitted)
			}
		})
	}
}

func credentialDirectoryForIsolationTest(t *testing.T, server *httptest.Server) *CredentialDirectory {
	t.Helper()
	directory := t.TempDir()
	credential := filepath.Join(directory, "docker")
	if err := os.Mkdir(credential, 0o700); err != nil {
		t.Fatal(err)
	}
	certificate := server.Certificate()
	key := server.TLS.Certificates[0].PrivateKey
	keyBytes, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	for name, value := range map[string][]byte{
		"ca.pem":   pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certificate.Raw}),
		"cert.pem": pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certificate.Raw}),
		"key.pem":  pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyBytes}),
	} {
		if err := os.WriteFile(filepath.Join(credential, name), value, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	server.TLS.ClientAuth = tls.RequestClientCert
	result, err := NewCredentialDirectory(directory)
	if err != nil {
		t.Fatal(err)
	}
	return result
}
