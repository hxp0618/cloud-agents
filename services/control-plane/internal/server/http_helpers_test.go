package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestJSONContentTypeHandlerEnforcesOnlyNonEmptyPOSTBodies(t *testing.T) {
	tests := []struct {
		name, method, contentType string
		body                      []byte
		duplicateContentType      bool
		wantStatus                int
		wantCalls                 int
	}{
		{name: "json", method: http.MethodPost, contentType: "application/json", body: []byte(`{}`), wantStatus: http.StatusNoContent, wantCalls: 1},
		{name: "json charset", method: http.MethodPost, contentType: "application/json; charset=utf-8", body: []byte(`{}`), wantStatus: http.StatusNoContent, wantCalls: 1},
		{name: "missing", method: http.MethodPost, body: []byte(`{}`), wantStatus: http.StatusUnsupportedMediaType},
		{name: "wrong", method: http.MethodPost, contentType: "text/plain", body: []byte(`{}`), wantStatus: http.StatusUnsupportedMediaType},
		{name: "duplicate", method: http.MethodPost, contentType: "application/json", body: []byte(`{}`), duplicateContentType: true, wantStatus: http.StatusUnsupportedMediaType},
		{name: "empty post", method: http.MethodPost, wantStatus: http.StatusNoContent, wantCalls: 1},
		{name: "get body", method: http.MethodGet, body: []byte(`{}`), wantStatus: http.StatusNoContent, wantCalls: 1},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			calls := 0
			handler := JSONContentTypeHandler(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
				calls++
				writer.WriteHeader(http.StatusNoContent)
			}))
			request := httptest.NewRequest(test.method, "/resource", bytes.NewReader(test.body))
			request.Header.Set("X-Request-ID", "request-media-type")
			if test.contentType != "" {
				request.Header.Set("Content-Type", test.contentType)
			}
			if test.duplicateContentType {
				request.Header.Add("Content-Type", test.contentType)
			}
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			if response.Code != test.wantStatus || calls != test.wantCalls {
				t.Fatalf("status=%d calls=%d", response.Code, calls)
			}
			if test.wantStatus == http.StatusUnsupportedMediaType {
				var problem publicProblem
				if err := json.NewDecoder(response.Body).Decode(&problem); err != nil || problem.Error.Code != "UNSUPPORTED_MEDIA_TYPE" || problem.RequestID != "request-media-type" {
					t.Fatalf("problem=%#v err=%v", problem, err)
				}
			}
		})
	}
}

func TestConcurrentRequestLimitRejectsExcessAndKeepsProbesAvailable(t *testing.T) {
	entered := make(chan struct{})
	release := make(chan struct{})
	handler := ConcurrentRequestLimitHandler(1, http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path == "/work" {
			close(entered)
			<-release
		}
		writer.WriteHeader(http.StatusNoContent)
	}))
	firstDone := make(chan struct{})
	go func() {
		handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, "/work", nil))
		close(firstDone)
	}()
	<-entered

	probe := httptest.NewRecorder()
	handler.ServeHTTP(probe, httptest.NewRequest(http.MethodGet, "/readyz", nil))
	if probe.Code != http.StatusNoContent {
		t.Fatalf("probe status = %d", probe.Code)
	}

	overload := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/other", nil)
	request.Header.Set("X-Request-ID", "request-overload")
	handler.ServeHTTP(overload, request)
	var problem publicProblem
	if err := json.NewDecoder(overload.Body).Decode(&problem); err != nil {
		t.Fatal(err)
	}
	if overload.Code != http.StatusTooManyRequests || overload.Header().Get("Retry-After") != "1" || overload.Header().Get("Cache-Control") != "no-store" || problem.Status != http.StatusTooManyRequests || problem.Error.Code != "REQUEST_CAPACITY_EXHAUSTED" || !problem.Error.Retryable || problem.RequestID != "request-overload" {
		t.Fatalf("overload response: status=%d headers=%v problem=%#v", overload.Code, overload.Header(), problem)
	}

	close(release)
	<-firstDone
}
