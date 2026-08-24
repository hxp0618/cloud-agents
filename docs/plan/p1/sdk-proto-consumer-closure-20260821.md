# P1-A3 Proto SDK and consumer closure implementation - 2026-08-21

- Status: **IMPLEMENTED CANDIDATE - INDEPENDENT REVIEW PENDING**
- Baseline commit: `24a47b2f95afde7de0aeffbb0deb4574339399d0`
- Branch: `codex/cloud-agents-platform-p1`
- Authority: [`sdk-identity-closure-entry-20260820.md`](./sdk-identity-closure-entry-20260820.md), Slice C
- This record does not authorize HTTP routes, P2/provider behavior, worker or adapter side effects, production
  database writes, deployment, publication, release, or Gate closure

## Frozen generated profile

The checked-in profile is `contracts/proto-generation.profile.json` with status
`GENERATED_NON_GATE_EVIDENCE`. It consumes exactly three existing Proto authorities:

1. `contracts/worker/v1alpha1/kernel.proto`;
2. `contracts/worker/v1alpha1/worker_supervisor.proto`; and
3. `contracts/platform-adapter/v1alpha1/platform_adapter.proto`.

The descriptor set includes imports, excludes source info, and is checked against the exact breaking baseline. The
descriptor contains three services and twelve unary methods: four WorkerExecution methods, three PlatformAdapterRegistry
methods, and five PlatformAdapterExecution methods. Streaming methods, hand-written wire models, and new RPCs are
outside this slice. Descriptor SHA-256 is `cd896a7aaccf70426be94314c5cfa09733a6908c33867c1f8942b4eb6b179218`.

Compiler and plugin identity is pinned by the profile: `protoc v35.1`, `protoc-gen-go v1.36.12`,
`protoc-gen-connect-go v1.20.0`, and `@bufbuild/protoc-gen-es v2.14.0`. Generation validates tool versions, output
paths, descriptor service cardinality, unary-only shape, and exact breaking-baseline bytes. Go and TypeScript
manifests bind the source/profile/contract manifests, dependency review, notice files, and output tree.

## Conformance and fresh consumers

The generated Go package exercises both ConnectRPC and gRPC-compatible mappings for all twelve methods through an
in-memory/loopback fixture transport. It also covers cancellation, deadline, stable error mapping, exact Proto
unknown-field round-trip, and mTLS rejection when no client certificate is supplied. The missing-certificate case
asserts that the fixture service observes zero calls.

The TypeScript package uses the injected transport seam only. Its conformance suite covers all twelve method mappings,
cancellation/deadline/stable errors, and exact unknown-field round-trip. No generated client constructs a network,
HTTP, provider, worker, session, turn, execution, or database side effect by itself.

`scripts/test-platform-sdk-consumers.ts` creates fresh temporary consumers. The TypeScript consumer packs and installs
the exact private SDK tarball, then typechecks/builds/imports the Proto entrypoint. The Go consumer copies the exact
module into a local file proxy, runs with `GOWORK=off`, rejects `replace`, `file:`, Git, workspace, and service imports,
and compiles/calls the generated package. These consumers are evidence fixtures only; they are not release or
publication workflows.

## Dependency, legal, and forbidden-surface evidence

The exact production/test/tool closure and license text hashes are recorded in
[`dependency-reviews/proto-sdk-toolchain-20260821.md`](./dependency-reviews/proto-sdk-toolchain-20260821.md),
`sdk/go/THIRD_PARTY_NOTICES.md`, and `sdk/typescript/THIRD_PARTY_NOTICES.md`. The generated lock binds those bytes to
the SDK manifests. The candidate contains no route registration, HTTP handler, provider call, worker action, session or
turn execution, production database write, deployment command, release metadata, credential handling, or external
side effect. The mTLS negative case is local fixture evidence and does not establish a production trust deployment.

## Gates and non-claims

The implementation candidate may record passing local generator, descriptor, Go, TypeScript, consumer, race, vet,
build, diff, dependency, and forbidden-surface checks after the final refresh. Those checks are bounded local evidence.
They do not close `G-CONTRACT`, `G-DATA`, `G-AUTHORITY-P1`, `G-SECURITY-P1`, `G-SUPPLY-CHAIN`, or any aggregate Gate.
Full migration closure, current vulnerability status, production artifact scanning, cloud/DB matrices, deployment,
publication, release, and immutable approval remain unverified or explicitly out of scope. Independent review must
tie its verdict to the final source/output hashes before any later slice can rely on this record.

## Final local verification

The final candidate verification was run with the pinned A3 toolchain and completed successfully for the generated
contract/descriptor check, Proto generator re-run, contract-lock check, 18 targeted Bun expectations, the complete
TypeScript package suite (20 tests), TypeScript typecheck/build/lint, Go SDK test/race/vet/build, `go mod verify`,
`go mod tidy -diff`, fresh packed TypeScript and Go consumers, and Linux `amd64`/`arm64` test compilation and build.
The descriptor remains three services and twelve unary methods with SHA-256
`cd896a7aaccf70426be94314c5cfa09733a6908c33867c1f8942b4eb6b179218`. Candidate formatting and
`git diff --check` pass. The repository-wide format check still reports only three pre-existing unrelated files
(`pgx-v5.10.0-x-text-v0.39.0-implemented-closure.md`, `x-sys-v0.44.0.md`, and
`services/control-plane/internal/migration/testdata/postgres_projection/catalog-representative.json`); they remain
untouched and are not presented as a repository-wide format pass. The full historical secret scan is still running
under the repository script and is not represented as passed until its process exits successfully.
