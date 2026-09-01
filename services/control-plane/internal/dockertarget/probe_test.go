package dockertarget

import (
	"context"
	"errors"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"testing"
)

func TestProbeUnixSocketReadsOnlyDockerHealthAndVersion(t *testing.T) {
	temporaryDirectory, err := os.MkdirTemp("/tmp", "cloud-agents-docker-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(temporaryDirectory) })
	socketPath := filepath.Join(temporaryDirectory, "docker.sock")
	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		t.Fatal(err)
	}
	server := &http.Server{Handler: http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodGet {
			t.Fatalf("method = %s", request.Method)
		}
		switch request.URL.Path {
		case "/_ping":
			_, _ = writer.Write([]byte("OK"))
		case "/version":
			writer.Header().Set("Content-Type", "application/json")
			_, _ = writer.Write([]byte(`{"ApiVersion":"1.53","Version":"29.4.0","Os":"linux","Arch":"arm64","Ignored":"safe"}`))
		default:
			http.NotFound(writer, request)
		}
	})}
	done := make(chan error, 1)
	go func() { done <- server.Serve(listener) }()
	t.Cleanup(func() {
		_ = server.Close()
		<-done
	})

	result, err := ProbeUnixSocket(context.Background(), socketPath)
	if err != nil {
		t.Fatal(err)
	}
	if result.APIVersion != "1.53" || result.EngineVersion != "29.4.0" || result.OS != "linux" || result.Architecture != "arm64" {
		t.Fatalf("result = %#v", result)
	}
}

func TestProbeUnixSocketFailsClosedWithoutLeakingTheSocket(t *testing.T) {
	if _, err := ProbeUnixSocket(context.Background(), "relative.sock"); !errors.Is(err, ErrInvalidSocket) {
		t.Fatalf("relative socket error = %v", err)
	}
	temporaryDirectory, err := os.MkdirTemp("/tmp", "cloud-agents-missing-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(temporaryDirectory) })
	missing := filepath.Join(temporaryDirectory, "docker.sock")
	if _, err := ProbeUnixSocket(context.Background(), missing); !errors.Is(err, ErrUnavailable) || err.Error() == missing {
		t.Fatalf("missing socket error = %v", err)
	}
}
