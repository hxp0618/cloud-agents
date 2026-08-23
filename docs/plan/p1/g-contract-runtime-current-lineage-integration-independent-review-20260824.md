# G-CONTRACT runtime current-lineage integration independent review

Date: 2026-08-24

## Verdict

`APPROVE`

| Severity | Findings |
| -------- | -------: |
| P0       |        0 |
| P1       |        0 |
| P2       |        0 |

This is a fresh fixed-candidate code, provenance, and focused-behavior review. It approves only the bounded runtime
server path-and-tenant criterion candidate on the reviewed closure-profile v2 lineage. It does not edit the generated
closure profile, remove a missing item, approve HTTP or production trust, or close `G-CONTRACT` or any aggregate Gate.

No deployment, production database write, remote database test, provider/P2 action, publication, release, or Gate
operation was run.

## Fixed candidate and canonical lineage

- candidate branch: `codex/cloud-agents-p1-runtime-current-integration-20260824`
- candidate commit: `b3eda9e7cc97225c1e2256ee27e0c07c8dbd462e`
- candidate tree: `2165fd70efd097e7e1decb109cee31e9f6af8ee5`
- parent: `9fe7338d3c424731e0b9946f5252e3f61d5326a9`
- HEAD diff SHA-256: `d4e6e96595d9d1554356e30878ce4d57143efb579d5a369ebf97c085f3f67562`
- `98cd0a3` to HEAD aggregate diff SHA-256:
  `33f9651245d887cb0c48599afa4293cbcecfe19dad086dcd4f1f710ffc5d9345`
- `services/control-plane/go.mod` raw SHA-256:
  `1664dce4a62ceca72a721690b80aa77d069372229b42aebade535c140499f4ad`
- `services/control-plane/go.sum` raw SHA-256:
  `f85e74742ea1cbbe7622488afabfa567445f2ad45bf75173840d699ef275dc65`

The candidate branch resolved to the exact commit on the remote. Its HEAD diff changes exactly `go.mod` and `go.sum`:
the SDK predecessor pin and its two checksum rows. There is no source, generated contract/profile, generation-lock,
status-tracker, or Gate-record change in the HEAD diff.

The ancestry is a direct three-commit linear chain:

```text
98cd0a30d151cdb3c667911a540a3e4006972bbf
  -> b269e2af7c985edef83654783173e1625cc58e1a  runtime implementation
  -> 9fe7338d3c424731e0b9946f5252e3f61d5326a9  fixed review record
  -> b3eda9e7cc97225c1e2256ee27e0c07c8dbd462e  reviewed-v2 SDK rebind
```

`98cd0a3` is the reviewed closure-profile v2 lineage. The implementation and its fixed review therefore precede this
SDK rebind without a merge, rewrite, or self-reference.

## SDK predecessor provenance

The control-plane module pins exactly:

```text
github.com/hxp0618/cloud-agents/sdk/go v0.0.0-20260823210219-5599f9d20e76
```

The pseudo-version timestamp is the UTC commit time of
`5599f9d20e761532e08906eab1fc8384d48e5b8e`, and its suffix is that commit's exact first twelve hexadecimal digits.
That fixed commit is the closure-profile v2 candidate immediately below its independent review record.

Pinned Go `1.26.6` independently resolved:

```text
Version:  v0.0.0-20260823210219-5599f9d20e76
Time:     2026-08-23T21:02:19Z
Sum:      h1:Qt6XpGHbm3TkLJB8QAx04blpcXxZQh3DfQ/sbzttCJI=
GoModSum: h1:qLQE6Q2bV2hZM0c7CZDZUx78EuODm+Vzl90AII5zYJs=
Replace:  none
```

The fetched module's 21 files were compared to the fixed commit's `sdk/go` subtree: zero missing and zero mismatched
files. The downloaded `go.mod` also reproduced the fixed source byte-for-byte. `go mod verify` reported all modules
verified, `go mod tidy -diff` was empty, and the selected graph retained `golang.org/x/sys v0.45.0` with no replacement.

Between the old predecessor SDK and v2, the generated OpenAPI Go source changes only its contract-manifest header
comment. Removing that generated header leaves the file byte-identical. The request validator's route tenant, request
ID, idempotency key, closed-body decode, return type, order, and failure behavior are unchanged.

## Runtime code and prior PostgreSQL evidence applicability

The production runtime server, its focused tests, the external authn test, and the PostgreSQL matrix script have the
same Git blobs as the independently reviewed runtime candidate:

| Path                                                                                  | Git blob                                   |
| ------------------------------------------------------------------------------------- | ------------------------------------------ |
| `services/control-plane/internal/server/managed_agent_create_project.go`              | `52545f173291f1e2655eb914746d783552a57f06` |
| `services/control-plane/internal/server/managed_agent_create_project_test.go`         | `abc1b07a6f02a3ea40a93a11bfbf34a8ed176a46` |
| `services/control-plane/internal/authn/runtime_server_external_test.go`               | `2a964d332a01bb0785075dacbe1e8cd28eb41852` |
| `services/control-plane/scripts/test-durable-coordination-service-postgres-matrix.sh` | `f2891ad9eb7de1f7233a9eb03f4e50801bbd7864` |

The prior fixed review document is unchanged at raw SHA-256
`d29e9bf0f4cca7e71ef190e43d37acbedabfcc4b5212c65a542497781f5c90d6`. It recorded PostgreSQL 15/16/17
normal/race/fault PASS against those exact production/test/script blobs and the semantically identical generated
validator. The only current integration delta is the reviewed-v2 module metadata pin.

The PostgreSQL matrix was intentionally not repeated in this review. Its behavior evidence remains applicable because
every executed runtime and matrix blob is identical and the only imported generated validator delta is a comment.
Fresh focused normal/race tests, vet, build, module resolution, and checksum verification cover the changed integration
boundary. This does not upgrade the old matrix into production-database evidence or authorize any external database.

## Fresh focused replay

Every Go command used pinned Go `1.26.6` with
`GOWORK=off GOTOOLCHAIN=local GOFLAGS=-mod=readonly`.

| Command                                                     | Result                       |
| ----------------------------------------------------------- | ---------------------------- |
| `go test -count=1 ./internal/server ./internal/authn`       | PASS; both packages          |
| `go test -race -count=1 ./internal/server ./internal/authn` | PASS; both packages          |
| `go vet ./internal/server ./internal/authn`                 | PASS                         |
| `go build ./internal/server ./internal/authn`               | PASS                         |
| `go mod verify`                                             | PASS; `all modules verified` |
| `go mod tidy -diff`                                         | PASS; empty diff             |
| focused Go files `gofmt -d`                                 | PASS; empty diff             |
| changed documentation `oxfmt 0.62.0 --check`                | PASS                         |
| HEAD and whole-lineage `git diff --check`                   | PASS                         |

No broad migration, full control-plane suite, or PostgreSQL matrix was run.

## Secret-scan classification

The candidate HEAD-only Gitleaks scan reports zero findings. The whole three-commit lineage scan reports exactly two
`generic-api-key` findings at lines 24 and 43: the `IdempotencyKey` input field and expected-output field of one mapping
unit test. Both fields contain the same literal test fixture value, `idempotency-route-01`.

The value is a deterministic non-secret placeholder used only to assert exact mapper preservation. It has no credential
source, external endpoint, authorization role, or production use. The two findings are therefore classified as the
same false-positive fixture represented twice, not hidden or treated as a clean whole-range scan.

## Criterion and Gate boundary

The currently generated v2 profile remains byte-unchanged and still lists
`runtime-server-path-and-tenant-authority-enforcement` as `MISSING`. This candidate and review supply current-lineage
evidence for a later versioned generated successor to classify that criterion as `SATISFIED_CANDIDATE`; they do not
manually remove it or mutate v2.

The server remains transport-neutral and claim-only. No HTTP handler, bearer/OIDC/JWKS/provider trust provisioning,
project writer, completion path, production database write, deployment, publication, release, or Gate action is
implemented or approved here. `G-CONTRACT` and every aggregate Gate remain OPEN.
