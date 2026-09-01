package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

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
