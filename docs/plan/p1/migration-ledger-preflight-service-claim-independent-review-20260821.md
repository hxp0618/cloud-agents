# P1 runner ledger preflight Slice C independent review - 2026-08-21

- Verdict: **APPROVE**
- Findings: **P0 0 / P1 0 / P2 0**
- Fixed candidate: `e64e0a28e2cd81203c91a7eba4df264bd29889f3`
- Candidate branch: `codex/cloud-agents-p1-ledger-preflight-service-claim-20260821`
- Review branch: `codex/cloud-agents-p1-ledger-preflight-service-claim-independent-review-20260821`
- Parent Slice A/B review: `f871cf334ee9b862c10c63f38567f22764873b3c`
- Scope: generated profile -> read-only catalog kernel -> sealed same-verifier evidence binder -> one-shot claim -> ordinary dispatch

This is an independent, fixed-source review of the bounded Slice C candidate. It approves only the package-private
service/claim/dispatch boundary described below. It is not a Gate signature, production database authority,
deployment evidence, release approval, or permission to connect this path to `Runner.Run` or a writer.

## 1. Fixed-source identity

The candidate was clean, matched its pushed upstream at `0/0`, and retained these identities before and after review:

| Identity | Exact value |
| --- | --- |
| commit | `e64e0a28e2cd81203c91a7eba4df264bd29889f3` |
| repository tree | `e7e886e54648779fcf757a564939ee8eb03ddfba` |
| `services/control-plane` subtree | `091a42f5abb4798c7860e9e44f5164607abfb534` |
| parent | `f871cf334ee9b862c10c63f38567f22764873b3c` |

The parent-to-candidate diff contains exactly nine files:

| File | SHA-256 |
| --- | --- |
| `contracts/generation.lock.json` | `7708b422373649ea9afe207e5e99bb55a46395682eaedef81c89234e3ff7515e` |
| `docs/plan/p1/migration-ledger-preflight-service-claim-matrix-20260821.md` | `effd740654a0afec3b02761c15e9c5730abb0cdac11db00e93727234809ca5ee` |
| `services/control-plane/internal/migration/evidence_session.go` | `401cd6edb4eab9be2e29f327137ba3314da7fa8f2f4124bc8ceb79e383518b31` |
| `services/control-plane/internal/migration/runner_ledger_catalog_preflight.go` | `638c499745089ff3ffd106445a9f3396301512622bf4c81c46db002ce9bbe004` |
| `services/control-plane/internal/migration/runner_ledger_catalog_preflight_test.go` | `d2a4eae87a75b90ac1f70288639237e37cb1e54d2428733549365dbbbb588f33` |
| `services/control-plane/internal/migration/runner_ledger_preflight_profile.go` | `1d5349594512a7414edf9d1e76e6ad2d05c96b88d5f662c549ea301a20038efd` |
| `services/control-plane/internal/migration/runner_ledger_preflight_profile_test.go` | `3c1291d31c9532a655e0fde25e0fe251fec4ea4a5d75c8d1ac33865894ef12a4` |
| `services/control-plane/internal/migration/runner_ledger_preflight_service.go` | `a6337d848828853d94251201bbbcc6b25c7719d33ecb29b0a89b2db7347a966a` |
| `services/control-plane/internal/migration/runner_ledger_preflight_service_test.go` | `b9358da4499da9d332e9d00c081cc8022e54f9083f714be6e18e1df8a405976f` |

## 2. Independent review result

No P0, P1, or P2 finding was identified in the fixed candidate.

The generated registry remains the sole source for five closed dispositions and 17 exact
disposition/recovery-state/action triples. Slice C does not add a caller-selected profile, alternate matrix, or
handwritten fallback. The service derives the exact next-entry digest from the same-verifier signed ledger row and
uses the generated binder to create the ordinary fact.

The Slice B projection remains locked and read-only. The sole new production caller is the package-private claim
service. The service cross-binds schema bundle, execution lineage, runner projection decision, migration count,
ordered signed migrations, exact durable ledger rows/digest, recovery snapshot, and final catalog digest. The
complete path reconstructs the final `CatalogStateProjection` and requires the exact evidence final-catalog digest
before it can produce `return_success`.

The production claim binder is sealed on the concrete `generationEvidenceSession`. It revalidates candidate,
current generation, journal schema, recovery snapshot, session identity, and journal identity under the existing
session -> journal lock order at both mint and consume. The claim is bound by self, registry, binder, candidate,
generation, projection, schema, recovery, session, journal, and atomic consumed identity. There is at most one live
claim per concrete evidence binder; exact consumption is one-shot; evidence close revokes the claim. Ordinary copies,
literals, drifted claims, stale evidence, reuse, and concurrent double consumption fail closed.

The returned dispatch is copyable ordinary data. Its canonical subject covers the generated fact, recovery identity,
journal identity, runner decision, and recovery snapshot/tail. It contains no database session, transaction,
evidence session or journal, lease, receipt, verifier artifact, writer token, or mutation port.

Source and AST scans confirm this consumer graph:

- exactly one production caller of `projectRunnerLedgerCatalogPreflight`, in the package-private Slice C service;
- exactly one production bind helper and one consume helper caller, both on `generationEvidenceSession`;
- no production caller of `prepareRunnerLedgerPreflightClaim` or `claimRunnerLedgerPreflightDispatch`;
- no connection to `Runner.Run`, `BeginMigration`, the existing write chain, HTTP, P2, provider, worker, session,
  turn, execution, deployment, publication, or Gate-closing surfaces.

The current production partial and complete runner paths therefore remain
`MIGRATION_PROJECTION_NOT_IMPLEMENTED`. The candidate performs no migration/RW transaction, ledger insert, evidence
append, statement execution, or commit.

## 3. Independent verification

The review used the exact declared toolchain:

- Node `24.13.1`: `/Users/huang/devel/soft/nvm/versions/node/v24.13.1/bin/node`;
- Bun `1.3.14`: `/tmp/codex-cloud-agents-bun-1.3.14/bun-darwin-aarch64/bun`;
- Go `1.26.6`: `/Users/huang/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.6.darwin-arm64/bin/go`.

From `services/control-plane`, the following passed with `GOWORK=off GOFLAGS=-mod=readonly`:

- `go test ./internal/migration -run 'TestRunnerLedger(Preflight|CatalogPreflight)' -count=1`: PASS, `17.386s`;
- the same focused selection with `-race`: PASS, `159.024s`;
- `go vet ./...`: PASS;
- `go build ./...`: PASS.

From the repository root, the exact Node/Bun/Go tuple passed:

- `bun scripts/generate-platform-runner-ledger-preflight-registry.ts --check`;
- `bun scripts/generate-platform-runner-ledger-preflight-go.ts --check`;
- `bun scripts/generate-platform-contract-lock.ts --check`;
- `bun test scripts/lib/platform-runner-ledger-preflight-registry.test.ts`: 5 tests, 44 assertions;
- `bun scripts/check-platform-contracts.ts`: `BOOTSTRAP_VALIDATED`, 103 JSON files, 42 schemas, 54 fixture cases,
  `notGateClosure=true`.

The focused tests cover all 17 generated pairs; PostgreSQL 15/16/17 metadata for empty, partial-next,
partial-recovery, and complete states; cross-bundle and ledger/catalog/recovery identity drift; corrupt-before-invalid
pair precedence; lock and close ambiguity; claim copy/literal/reuse/revocation; and concurrent one-shot consumption.

## 4. Verification boundaries and non-claims

The PostgreSQL 15/16/17 result is the in-process metadata/state matrix, not a live three-version database matrix. No
production or live PostgreSQL database was read or written.

The concrete `generationEvidenceSession` successful path was reviewed statically, compiled, interface-checked, and
checked for literal rejection and lifecycle/call-graph closure. A successful concrete integration requires an
authentic sealed `evidencefs.GenerationLease`; the repository intentionally exposes no production trusted-mount test
constructor. This review does not claim that deferred trusted-provisioning integration.

The broad `go test ./internal/migration -count=1` suite was not rerun. Its recorded bounded five-minute silent stop is
**NOT PASS** and is not replaced by the focused normal/race results.

No deployment, release, publication, merge to main, RC, Beta, GA, immutable Gate signature, aggregate Gate closure,
production database write, HTTP/P2/provider side effect, or external authority was performed or approved. All Gates
remain open.
