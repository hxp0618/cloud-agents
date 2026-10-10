package identity

// This test stays package-local because cursor MACs and their key must remain private.
import (
	"strings"
	"testing"

	api "github.com/hxp0618/cloud-agents/sdk/go/gen/openapi/v1alpha1"
)

func TestTenantCursorCannotCrossSessionOrApplication(t *testing.T) {
	store := &PasswordStore{csrfKey: []byte(strings.Repeat("k", 32))}
	session := [32]byte{1}
	cursor := store.tenantCursor(session, api.IdentityApplicationAdmin, "tenant-001")
	if after, err := store.tenantCursorAfter(session, api.IdentityApplicationAdmin, cursor); err != nil || after != "tenant-001" {
		t.Fatal("tenant cursor did not round-trip")
	}
	for _, input := range []struct {
		session [32]byte
		app     api.IdentityApplication
		cursor  string
	}{
		{[32]byte{2}, api.IdentityApplicationAdmin, cursor},
		{session, api.IdentityApplicationUser, cursor},
		{session, api.IdentityApplicationAdmin, cursor + "="},
		{session, api.IdentityApplicationAdmin, "A" + cursor},
		{session, api.IdentityApplicationAdmin, strings.Repeat("A", 300)},
	} {
		if _, err := store.tenantCursorAfter(input.session, input.app, input.cursor); err == nil {
			t.Fatal("unbound or malformed tenant cursor accepted")
		}
	}
}
