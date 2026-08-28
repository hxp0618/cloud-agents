//go:build localdev

package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestLocalControlPlaneHealthHTTPServerReportsLivenessAndReadiness(t *testing.T) {
	readinessCalls := 0
	server, err := NewLocalControlPlaneHealthHTTPServer(func(context.Context) error {
		readinessCalls++
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	liveness := httptest.NewRecorder()
	server.ServeHTTP(liveness, httptest.NewRequest(http.MethodGet, LocalControlPlaneHealthRoute, nil))
	assertHealthResponse(t, liveness, http.StatusOK, "ControlPlaneHealth", "ok")
	if readinessCalls != 0 {
		t.Fatalf("liveness called readiness probe %d times", readinessCalls)
	}

	readiness := httptest.NewRecorder()
	server.ServeHTTP(readiness, httptest.NewRequest(http.MethodGet, LocalControlPlaneReadinessRoute, nil))
	assertHealthResponse(t, readiness, http.StatusOK, "ControlPlaneReadiness", "ready")
	if readinessCalls != 1 {
		t.Fatalf("readiness calls = %d, want 1", readinessCalls)
	}
}

func TestLocalControlPlaneHealthHTTPServerFailsClosed(t *testing.T) {
	if server, err := NewLocalControlPlaneHealthHTTPServer(nil); server != nil || !errors.Is(err, ErrInvalidLocalControlPlaneHealthServer) {
		t.Fatalf("nil probe constructor = %#v/%v", server, err)
	}

	server, err := NewLocalControlPlaneHealthHTTPServer(func(context.Context) error {
		return errors.New("database detail must not escape")
	})
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name       string
		method     string
		path       string
		wantStatus int
		wantCode   string
	}{
		{name: "not ready", method: http.MethodGet, path: LocalControlPlaneReadinessRoute, wantStatus: http.StatusServiceUnavailable, wantCode: "not_ready"},
		{name: "method", method: http.MethodPost, path: LocalControlPlaneHealthRoute, wantStatus: http.StatusMethodNotAllowed, wantCode: "method_not_allowed"},
		{name: "route", method: http.MethodGet, path: "/healthz/extra", wantStatus: http.StatusNotFound, wantCode: "route_not_found"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			response := httptest.NewRecorder()
			server.ServeHTTP(response, httptest.NewRequest(test.method, test.path, nil))
			if response.Code != test.wantStatus {
				t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
			}
			var body localControlPlaneHealthErrorResponse
			if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
				t.Fatal(err)
			}
			if body.Code != test.wantCode || body.APIVersion != localHealthAPIVersion || body.Kind != "Error" {
				t.Fatalf("error body = %#v", body)
			}
			if response.Header().Get("Content-Type") != "application/json" || response.Header().Get("Cache-Control") != "no-store" {
				t.Fatalf("headers = %#v", response.Header())
			}
			if body.Code == "not_ready" && strings.Contains(response.Body.String(), "database detail") {
				t.Fatal("readiness error leaked dependency detail")
			}
		})
	}
}

func TestLocalControlPlaneHealthHTTPServerNilReceiverFailsClosed(t *testing.T) {
	var server *LocalControlPlaneHealthHTTPServer
	response := httptest.NewRecorder()
	server.ServeHTTP(response, httptest.NewRequest(http.MethodGet, LocalControlPlaneHealthRoute, nil))
	if response.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
}

func TestLocalControlPlaneHealthHTTPServerNilRequestFailsClosed(t *testing.T) {
	server, err := NewLocalControlPlaneHealthHTTPServer(func(context.Context) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	response := httptest.NewRecorder()
	server.ServeHTTP(response, nil)
	if response.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
}

func assertHealthResponse(t *testing.T, response *httptest.ResponseRecorder, status int, kind, state string) {
	t.Helper()
	if response.Code != status {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	var body localControlPlaneHealthResponse
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.APIVersion != localHealthAPIVersion || body.Kind != kind || body.Component != "cloud-agents-control-plane" || body.Status != state {
		t.Fatalf("body = %#v", body)
	}
	if response.Header().Get("Content-Type") != "application/json" || response.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("headers = %#v", response.Header())
	}
}
