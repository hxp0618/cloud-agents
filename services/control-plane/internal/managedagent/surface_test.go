package managedagent

import (
	"go/parser"
	"go/token"
	"path/filepath"
	"strconv"
	"testing"
)

func TestLifecycleKernelHasNoActuatorOrTransportImports(t *testing.T) {
	for _, name := range []string{"lifecycle.go", "profile.go", "events.go"} {
		path := filepath.Join(".", name)
		file, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		for _, specification := range file.Imports {
			importPath, err := strconv.Unquote(specification.Path.Value)
			if err != nil {
				t.Fatal(err)
			}
			switch importPath {
			case "net/http", "github.com/jackc/pgx/v5", "github.com/jackc/pgx/v5/pgxpool",
				"github.com/hxp0618/cloud-agents/services/worker",
				"github.com/hxp0618/cloud-agents/packages/cloud-agent-provider-api":
				t.Fatalf("%s imports forbidden actuator/transport package %q", name, importPath)
			}
		}
	}
}
