package migrationcore

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func migrationRoot(t *testing.T) string { return filepath.Join(moduleRoot(t), "migrations") }

func mustRead(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

// IsCode reports whether err or one of its wrapped errors has code.
func IsCode(err error, code ErrorCode) bool {
	var target *Error
	return errors.As(err, &target) && target.Code == code
}

const StatementTransitionProfile = "cloud-agents-platform-statement-transition/v1"

func minimalCatalogBody() CatalogProjectionBody {
	return CatalogProjectionBody{
		Schema: SchemaProjection{
			Name: "cloud_agents", Owner: MigrationOwnerRole,
			ExplicitACL:    ACLSetProjection{CatalogValue: "null", Entries: []ACLProjection{}},
			EffectiveACL:   []ACLProjection{{Grantor: MigrationOwnerRole, Grantee: MigrationOwnerRole, Privileges: []string{"CREATE", "USAGE"}, Grantable: []string{"CREATE", "USAGE"}, Origin: "owner_implicit"}},
			SecurityLabels: []SecurityLabel{},
		},
		DefaultACL: []DefaultACLProjection{}, Relations: []RelationProjection{}, Functions: []FunctionProjection{},
		Dependencies: []DependencyProjection{}, ObjectCount: 0, DeclaredObjects: []ObjectIdentityProjection{}, DeniedObjects: []DeniedObjectProjection{},
	}
}

func moduleRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	return root
}
