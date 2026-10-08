# SDK development

Public SDK behavior is tested by capability rather than by generated source
file. TypeScript tests live in `typescript/test/`; Go public package tests live
in `go/test/`, grouped by package where their fixtures and helper names differ.
Use table-driven cases for related boundaries.

Keep shared resource metadata and injected-transport request recording in small
local helpers; retain each capability's request and security assertions.
List responses whose resources are scoped by request paths must reject entries
from another tenant, project, or parent resource after decoding.

`go/gen/openapi/v1alpha1/http_test.go` remains beside its package because it
verifies the private `roundTrip` transport, response limits, and credential
handling. The public JSON/client tests retain their external-consumer semantic
authority bindings at their new test paths.

Do not export internal APIs, duplicate production code, or add a custom runner
to move those files. Generator manifests bind code that produces SDK output;
ordinary test source is discovered and run separately.
