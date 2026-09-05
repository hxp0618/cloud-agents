package server

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"regexp"
	"strings"
	"testing"
)

func TestAdminDeniedWriteRoutesCoverEveryContractWriteWithoutReadingBody(t *testing.T) {
	raw, err := os.ReadFile("../../../../contracts/managed-host/v1alpha1/openapi.json")
	if err != nil {
		t.Fatal(err)
	}
	var contract struct {
		Paths map[string]map[string]struct {
			OperationID string `json:"operationId"`
		} `json:"paths"`
	}
	if err := json.Unmarshal(raw, &contract); err != nil {
		t.Fatal(err)
	}
	parameter := regexp.MustCompile(`\{([^}]+)\}`)
	count := 0
	for path, methods := range contract.Paths {
		if !strings.HasPrefix(path, "/v1/admin/") {
			continue
		}
		for method, operation := range methods {
			if method != "post" && method != "put" {
				continue
			}
			route := parameter.ReplaceAllStringFunc(path, func(value string) string {
				if value == "{profileVersion}" {
					return "2"
				}
				return "id-valid"
			})
			request := httptest.NewRequest(strings.ToUpper(method), route+"?token=do-not-record", nil)
			request.Body = unreadableAuditBody{t}
			request.Header.Set("X-Request-ID", "request-valid")
			event, ok := adminDeniedWriteRoute(request)
			if !ok || event.Action != operation.OperationID || event.RequestID != "request-valid" || event.TenantID != "id-valid" || event.ProjectID != "id-valid" {
				t.Fatalf("missing trusted denial route: %s %s", method, path)
			}
			if strings.Contains(path, "profileVersion") && event.ProfileVersion != 2 {
				t.Fatal("requested profile version lost")
			}
			encoded, _ := json.Marshal(event)
			if strings.Contains(string(encoded), "do-not-record") {
				t.Fatal("raw query leaked into event")
			}
			request.Header.Set("X-Request-ID", "invalid/id")
			if fallback, ok := adminDeniedWriteRoute(request); !ok || fallback.RequestID != publicFallbackRequestID || request.Header.Get("X-Request-ID") != "invalid/id" {
				t.Fatal("invalid correlation data bypassed audit or altered request validation")
			}
			count++
		}
	}
	if count != 13 {
		t.Fatalf("review changed Admin write surface: %d", count)
	}
	for _, path := range []string{
		"/v1/admin/tenants/t/projects/p/deployment-targets/invalid:id:probe",
		"/v1/admin/tenants/t/projects/p/deployment-targets/id:unknown",
		"/v1/tenants/t/projects/p/environment-profiles",
	} {
		request := httptest.NewRequest(http.MethodPost, path, nil)
		request.Header.Set("X-Request-ID", "request-valid")
		if _, ok := adminDeniedWriteRoute(request); ok {
			t.Fatalf("unexpected audit route %s", path)
		}
	}
}

type unreadableAuditBody struct{ t *testing.T }

func (body unreadableAuditBody) Read([]byte) (int, error) {
	body.t.Fatal("audit read request body")
	return 0, errors.New("unreachable")
}
func (unreadableAuditBody) Close() error { return nil }

func TestAdminDeniedWriteResponseDurableBeforeForbiddenAndFailureReplacement(t *testing.T) {
	for _, test := range []struct {
		status int
		fail   bool
	}{{403, false}, {403, true}, {401, false}, {400, false}, {200, false}} {
		recorder := httptest.NewRecorder()
		calls := 0
		writer := &adminDeniedWriteResponse{ResponseWriter: recorder, record: func() error {
			calls++
			if recorder.Flushed || recorder.Body.Len() != 0 {
				t.Fatal("response escaped before durable audit")
			}
			if test.fail {
				return errors.New("private database diagnostics")
			}
			return nil
		}}
		preparePublicRequestID(writer, httptest.NewRequest("POST", "/", nil))
		if test.status >= 400 {
			writePublicProblem(writer, test.status, "AUTHORIZATION_DENIED")
		} else {
			_, _ = writer.Write([]byte("success"))
		}
		writer.WriteHeader(403) // Duplicate status must neither append again nor replace a success.
		if (calls == 1) != (test.status == 403) {
			t.Fatalf("unexpected audit count %d for %d", calls, test.status)
		}
		if test.fail {
			var problem publicProblem
			if json.Unmarshal(recorder.Body.Bytes(), &problem) != nil || recorder.Code != 503 || problem.Status != 503 || problem.Error.Code != "ADMIN_AUDIT_UNAVAILABLE" || !problem.Error.Retryable {
				t.Fatal("failed audit did not replace the complete Problem envelope")
			}
			if strings.Contains(recorder.Body.String(), "private database") {
				t.Fatal("private error leaked")
			}
		} else if recorder.Code != test.status {
			t.Fatalf("status changed: %d", recorder.Code)
		}
	}
}
