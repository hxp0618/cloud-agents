package v1alpha1

import (
	"strings"
	"testing"
)

func TestPersistedWorkerHealthStatus(t *testing.T) {
	body := `{"state":"online","checkedAt":"2026-09-05T12:00:00Z","expiresAt":"2026-09-05T12:01:00Z","lastSuccessAt":"2026-09-05T12:00:00Z"}`
	for _, state := range []string{"online", "unavailable", "expired"} {
		value, err := DecodeWorkerHealthStatusJSON([]byte(strings.Replace(body, "online", state, 1)))
		if err != nil || value.State != state {
			t.Fatalf("state=%s err=%v", state, err)
		}
	}
	for _, invalid := range []string{
		strings.Replace(body, "online", "ready", 1),
		strings.Replace(body, "12:01:00Z", "12:00:00Z", 1),
		strings.Replace(body, "12:01:00Z", "12:01:01Z", 1),
		strings.Replace(body, `,"lastSuccessAt":"2026-09-05T12:00:00Z"`, "", 1),
		strings.Replace(body, `"lastSuccessAt":"2026-09-05T12:00:00Z"`, `"lastSuccessAt":"2026-09-05T12:00:01Z"`, 1),
		strings.Replace(body, "}", `,"endpoint":"https://secret.test"}`, 1),
	} {
		if _, err := DecodeWorkerHealthStatusJSON([]byte(invalid)); err == nil {
			t.Fatal("accepted invalid health status")
		}
	}
}
