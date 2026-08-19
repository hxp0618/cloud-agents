# P1-A2.3 durable coordination full migration closure - 2026-08-20

- Status: **LOCAL FULL MIGRATION SUITE PASS — NO GATE CLOSURE**
- Fixed source: `67b8acb0f8d405d893bd4f89d71cf3587dd92977`
- Preceding fixture bindings: `b39b070289df4b3682017aea5f854db2534054bb` and `67b8acb0f8d405d893bd4f89d71cf3587dd92977`
- Branch: `codex/cloud-agents-platform-p1`
- Scope: the deferred full `internal/migration` confirmation for the A2.3 durable-coordination remediation, run against the current checked-in bundle that also contains the already-authorized A2.4 `000010` schema-only kernel
- Does not authorize: A2.4 writer/service work, HTTP/P2/provider effects, production database mutation, deployment, release, immutable evidence signatures, or any aggregate Gate closure

## 1. Why this rerun was needed

The earlier A2.3 remediation review correctly recorded a bounded ten-minute local attempt as **not a pass**. The
current checked-in bundle subsequently included schema-only migration `000010`, so two test-only assertions still
described the older bundle:

1. `b39b070` updated only the exact quota fixture binding in
   `internal/migration/evidence_quota_test.go`; it now covers statement counts
   `[20, 71, 46, 20, 1, 1, 89, 34, 30, 52]` and leaves the historical through-`000008` profile assertions intact.
2. `67b8acb` updated only the checked-in bundle identity assertions in
   `internal/migration/bundle_test.go`; it binds the already-generated current schema bundle, manifest, and runtime
   tar identities. No migration SQL, generated bundle artifact, service behavior, or authorization rule was changed.

Those corrections make the full-suite assertion describe the current checked-in bundle; they do not reinterpret the
historical ten-minute observation or broaden the approved A2.3/A2.4 boundary.

## 2. Exact local result

From `services/control-plane` at fixed source `67b8acb`, the authoritative rerun was:

```sh
GOWORK=off GOFLAGS=-mod=readonly \
  go test -count=1 -timeout=30m ./internal/migration
```

It completed successfully:

```text
ok  github.com/hxp0618/cloud-agents/services/control-plane/internal/migration  1012.165s
```

Focused quota/profile and checked-in identity tests also passed in normal and race modes before the full run. The
current v3 quota assertion is intentionally exact:

| Fact                           |       Value |
| ------------------------------ | ----------: |
| Reserved segments              |          22 |
| Reserved journal records       |       2,296 |
| Reserved checkpoint records    |       2,295 |
| Reserved journal bytes         | 364,445,696 |
| Reserved lineage-index records |       2,299 |
| Reserved lineage-index bytes   |   9,695,232 |
| Combined reserved bytes        | 374,140,928 |

The generated identity assertions bound by `67b8acb` are:

- schema bundle: `sha256:a1673fcdf71fd49439ec9cefde2d02c627029799a700913653ed1f1f6fca7f09`;
- manifest: `sha256:7fa7ef8e9aa9eba67c56b8ed1e5b8183c9add4065e3e8c3bb196c4d1fe9d6eeb`;
- runtime tar: `sha256:8ac00f6e57db8160ee3f48cc249fab2d4032f63eaf44ed1859642cdb0a1f56da`.

## 3. Boundary retained after the pass

This records only a local full-suite result. The earlier independent review remains historical evidence at its own
fixed source; it is not rewritten by this later test closure. The `000010` schema-only kernel is part of the tested
bundle, but its proposed versioned-registry/writer/service follow-up remains **OWNER APPROVAL REQUIRED**.

HTTP/P2 external effects remain absent. No production database was contacted or changed, no deployment or release was
performed, and every immutable and aggregate Gate remains open.
