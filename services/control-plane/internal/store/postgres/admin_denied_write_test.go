package postgres

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

// Same-package exception: validates the private audit storage boundary against its wire contract.
func TestAdminDeniedWriteAcceptsEveryContractAction(t *testing.T) {
	raw, err := os.ReadFile("../../../../../contracts/platform/v1alpha1/schemas/admin-denied-write-event.schema.json")
	if err != nil {
		t.Fatal(err)
	}
	var contract struct {
		Properties struct {
			Action struct {
				Enum []string `json:"enum"`
			} `json:"action"`
		} `json:"properties"`
	}
	if err := json.Unmarshal(raw, &contract); err != nil || len(contract.Properties.Action.Enum) == 0 {
		t.Fatalf("invalid audit contract: %v", err)
	}
	raw, err = os.ReadFile("../../../../../contracts/managed-host/v1alpha1/openapi.json")
	if err != nil {
		t.Fatal(err)
	}
	var routes struct {
		Paths map[string]map[string]struct {
			OperationID string `json:"operationId"`
		} `json:"paths"`
	}
	if err := json.Unmarshal(raw, &routes); err != nil {
		t.Fatal(err)
	}
	for _, action := range contract.Properties.Action.Enum {
		t.Run(action, func(t *testing.T) {
			var route string
			for path, methods := range routes.Paths {
				for method, operation := range methods {
					if (method == "post" || method == "put") && operation.OperationID == action {
						route = path
					}
				}
			}
			if route == "" {
				t.Fatal("audit action has no write route")
			}
			resourcePath := strings.NewReplacer("{tenantId}", "", "{projectId}", "", "{profileVersion}", "").Replace(route)
			accepted := 0
			for _, shape := range []struct {
				resource string
				version  int64
			}{{"", 0}, {"resource-one", 0}, {"resource-one", 1}} {
				event := AdminDeniedWrite{TenantID: "tenant-one", ProjectID: "project-one", RequestID: "request-one", Action: action, ResourceID: shape.resource, ProfileVersion: shape.version}
				if !validAdminDeniedWrite(event) {
					if (shape.resource != "") == strings.Contains(resourcePath, "{") && (shape.version > 0) == strings.Contains(route, "{profileVersion}") {
						t.Fatal("rejected trusted route metadata shape")
					}
					continue
				}
				accepted++
				event.RequestID = "invalid/request"
				if validAdminDeniedWrite(event) {
					t.Fatal("accepted invalid correlation identifier")
				}
			}
			if accepted != 1 {
				t.Fatalf("contract action must have exactly one accepted metadata shape, got %d", accepted)
			}
		})
	}
	if validAdminDeniedWrite(AdminDeniedWrite{TenantID: "tenant-one", ProjectID: "project-one", RequestID: "request-one", Action: "unknownAction"}) {
		t.Fatal("accepted unknown action")
	}
}
