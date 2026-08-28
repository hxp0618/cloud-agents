//go:build localdev

package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
)

const (
	// LocalControlPlaneHealthRoute reports process liveness. It is intentionally
	// localdev-only and does not expose database or build provenance.
	LocalControlPlaneHealthRoute = "/healthz"
	// LocalControlPlaneReadinessRoute reports whether the local control plane's
	// injected dependency probe currently succeeds.
	LocalControlPlaneReadinessRoute = "/readyz"
	localHealthAPIVersion           = "cloud-agents.localdev/v1"
)

var ErrInvalidLocalControlPlaneHealthServer = errors.New("local control-plane health server configuration is invalid")

// LocalControlPlaneReadinessProbe is a read-only dependency check. The
// control-plane command supplies pgxpool.Ping; tests may inject a deterministic
// probe. No caller input is passed to the probe.
type LocalControlPlaneReadinessProbe func(context.Context) error

// LocalControlPlaneHealthHTTPServer is a loopback/localdev health adapter. It
// has no database writer, provider, or public deployment authority.
type LocalControlPlaneHealthHTTPServer struct {
	readiness LocalControlPlaneReadinessProbe
}

func NewLocalControlPlaneHealthHTTPServer(
	readiness LocalControlPlaneReadinessProbe,
) (*LocalControlPlaneHealthHTTPServer, error) {
	if readiness == nil {
		return nil, ErrInvalidLocalControlPlaneHealthServer
	}
	return &LocalControlPlaneHealthHTTPServer{readiness: readiness}, nil
}

type localControlPlaneHealthResponse struct {
	APIVersion string `json:"apiVersion"`
	Kind       string `json:"kind"`
	Component  string `json:"component"`
	Status     string `json:"status"`
}

type localControlPlaneHealthErrorResponse struct {
	APIVersion string `json:"apiVersion"`
	Kind       string `json:"kind"`
	Code       string `json:"code"`
}

func (server *LocalControlPlaneHealthHTTPServer) ServeHTTP(
	writer http.ResponseWriter,
	request *http.Request,
) {
	writer.Header().Set("Cache-Control", "no-store")
	if server == nil || server.readiness == nil || request == nil {
		writeLocalControlPlaneHealthError(writer, http.StatusInternalServerError, "internal_error")
		return
	}
	if request.URL.Path != LocalControlPlaneHealthRoute && request.URL.Path != LocalControlPlaneReadinessRoute {
		writeLocalControlPlaneHealthError(writer, http.StatusNotFound, "route_not_found")
		return
	}
	if request.Method != http.MethodGet {
		writer.Header().Set("Allow", http.MethodGet)
		writeLocalControlPlaneHealthError(writer, http.StatusMethodNotAllowed, "method_not_allowed")
		return
	}
	if request.URL.Path == LocalControlPlaneReadinessRoute {
		if err := server.readiness(request.Context()); err != nil {
			writeLocalControlPlaneHealthError(writer, http.StatusServiceUnavailable, "not_ready")
			return
		}
		writeLocalControlPlaneHealthJSON(writer, http.StatusOK, localControlPlaneHealthResponse{
			APIVersion: localHealthAPIVersion,
			Kind:       "ControlPlaneReadiness",
			Component:  "cloud-agents-control-plane",
			Status:     "ready",
		})
		return
	}
	writeLocalControlPlaneHealthJSON(writer, http.StatusOK, localControlPlaneHealthResponse{
		APIVersion: localHealthAPIVersion,
		Kind:       "ControlPlaneHealth",
		Component:  "cloud-agents-control-plane",
		Status:     "ok",
	})
}

func writeLocalControlPlaneHealthError(writer http.ResponseWriter, status int, code string) {
	writeLocalControlPlaneHealthJSON(writer, status, localControlPlaneHealthErrorResponse{
		APIVersion: localHealthAPIVersion,
		Kind:       "Error",
		Code:       code,
	})
}

func writeLocalControlPlaneHealthJSON(writer http.ResponseWriter, status int, value any) {
	writer.Header().Set("Content-Type", "application/json")
	writer.WriteHeader(status)
	_ = json.NewEncoder(writer).Encode(value)
}
