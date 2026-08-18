# P1-A2.2 versioned lineage/quota profile implementation evidence - 2026-08-18

- Status: **IMPLEMENTATION COMMITTED - INDEPENDENT REVIEW PENDING**
- Implementation commit: `cd64deec8846395f2d2424d945f6bd0674ba41dc`
- Stored-replay follow-up commit: `77de97eb1d99857dbf20440984a0df08e81186e9`
- Branch: `codex/cloud-agents-platform-p1`
- Base: `857e502ea6a0995d8ae29ec2dc5377ebbf15b7bf`
- Remote: implementation and stored-replay follow-up commits pushed
- Scope: ADR-0012 v1 historical compatibility, v2 lineage/quota profile, and append-only `000006`
- Does not authorize: production migration admission, database publication, deployment, release, Gate closure,
  or entry into A2.3

## 1. Implemented decision

The approved direction in [ADR-0012](../adr/0012-p1-versioned-lineage-quota-profile.md) is implemented as an
explicit authority fact. New manifests select
`cloud-agents-platform-lineage-quota-profile/v2`; the selected profile is carried through verified bundle
facts, quota reservation, planned journal headers, generation transitions, strict replay, recovery, append,
rotation, and reopen validation. Unknown, missing, zero, swapped, or cross-generation profiles fail closed.

Historical v1 remains a separate branch. Its manifest/profile omission, quota subject encoding, checkpoint
ceiling, and archived fixture behavior remain byte-compatible. The generic v1 framing path is retained for
historical and diagnostic decoding; v2 writers and validators use the profile-aware 4096-byte inclusive
checkpoint maximum.

The append-only `000006_close_subject_issuer_validation.sql` function now applies the same closed issuer
language as Go: `user`, `serviceAccount`, or `workload`; an ASCII scheme with a required colon; no ASCII
control or DEL bytes; and valid two-hex-digit percent escapes. Invalid direct PostgreSQL mutations are rejected
before tenant revision, resource, membership, role-binding, or audit state changes.

## 2. Exact generated facts

The six-entry generated bundle selects `cloud-agents-platform-lineage-quota-profile/v2`. The quota tests assert:

| Fact                         | Exact value |
| ---------------------------- | ----------: |
| journal records              |      `1018` |
| reserved journal bytes       | `160169984` |
| journal segments             |        `10` |
| checkpoint records           |      `1017` |
| lineage-index records        |      `1021` |
| reserved lineage-index bytes |   `4460544` |
| combined reserved bytes      | `164630528` |

These values reserve the closed v2 checkpoint maximum for every checkpoint. They do not use the observed size
of a sample checkpoint. The v2 index reservation remains below the unchanged 16 MiB physical index maximum.

Source and generated artifact hashes:

- `000006_close_subject_issuer_validation.sql`: `5b24e8462c90b7d430717ac746e00f999b21f21eae0fb855444379807c0b47e5`
- `migrations/manifest.json`: `3e491232916863f77302a9a07f519042443e043a5d3b1c2a29c847b01c0895ec`
- `migrations/schema-bundle.json`: `8088b2ff98a7077ec98ca4f925c076501f9478b5b3aa1d8f976582d956884336`
- `contracts/generation.lock.json`: `a868f8ac39d21a7c4b968e0864e2baa86977a44aa1d0a8dbe42ebf40131c80fe`
- schema bundle digest: `sha256:efa8240997f191f6e1540897bf391d6ed3c0a921e5958ea97338aec9e3befeec`
- bootstrap bundle digest: `sha256:db95649924f259cfa320e897bd5e0934c35fcc9009d8492a69ec5dc71132081c`
- manifest digest: `sha256:f3ccdc9498f136f7f11fc25435d26dd6f0f48fe5cdd046e89175ee2d05838f8c`
- runtime tar digest: `sha256:65db6f34d51366a877a8a4d9d8e0a252627f689f53d61aff9c56856d753d57d5`
- bootstrap tar digest: `sha256:6654946d58f707d48c71740a41407674c34b5fbeced2e38eeb6c8d1bb08ae175`

## 3. Verification performed

The following local checks passed against the committed worktree:

- focused migration profile/quota/checkpoint/admission/recovery tests;
- Bun migration bundle and SQL tests: 2 files, 19 tests;
- generated bundle `--check` and migration-bundle checker;
- PostgreSQL 15/16/17 local normal and race matrix, including five direct invalid-issuer faults per version;
- `go vet ./...` and `go build ./...`;
- full Bun test suite, build, lint, and typecheck;
- contract semantic check and generation-lock check under the pinned Go 1.26.6 toolchain binary;
- `gofmt -d` on changed Go files and `git diff --check`;
- targeted secret-pattern scan over changed files: no findings.

The broad command
`GOWORK=off GOTOOLCHAIN=local GOFLAGS=-mod=readonly go test ./internal/migration ./internal/authz ./internal/store/postgres -count=1 -timeout=10m`
completed the authz and PostgreSQL packages, but migration timed out at 600 seconds in the pre-existing
`TestRunnerPreledgerProjectionFaultsRollbackWithoutAppendingEvidenceOrLedger/catalog-scope` path. It is
recorded as an open test-environment boundary, not as a pass and not as a new profile failure. The repository-wide
historical secret scanner was stopped after several minutes because it scans every path of every Git revision;
it is not claimed as passed here.

### 3.1 Stored-replay self-review follow-up

Self-review found that the generic lineage decoder retained the historical v1 16 KiB physical checkpoint
ceiling, while admission replay did not independently apply the selected generation profile before runtime
object inspection. A stored v2 checkpoint above 4096 bytes could therefore reach a later recovery-required
path when its referenced object was absent instead of being classified immediately as a stored contradiction.

Commit `77de97eb1d99857dbf20440984a0df08e81186e9` centralizes the inclusive per-record/profile ceiling and applies
it during lineage admission decode. The regression test constructs the same valid checkpoint in the
4097..16384-byte compatibility window and proves that historical v1 admission replay accepts it while v2
admission replay returns `MIGRATION_EVIDENCE_JOURNAL_CORRUPT`.

Follow-up source hashes:

- `evidence_contract.go`: `f6f8388bcb95d5529041624386e31ca031881dcf0cb146d7800e9502e07bcad4`
- `evidence_frame_io.go`: `67785159c42388bf395bc5bb3c6c8c6ae52ceedd4cefb1fe1c6b96cad25076ff`
- `evidence_admission_replay.go`: `33cc1703da82267420693b06fec21149fed87fca60f6e91a1e3a008f571e2641`
- `evidence_admission_replay_test.go`: `8715e23e63f13ade02e19b3b15e7d2524147a76d048e5da428f890a4ecde37b7`

The focused normal and race tests, `go vet ./...`, `go build ./...`, and `git diff --check` passed for this
follow-up. A fresh migration-only full test again reached the 600-second timeout in the same pre-existing
preledger projection fault suite, this time while executing the `status-drift` case. It remains an open broad
test boundary and is not counted as a pass.

### 3.2 Current-ref full-module rerun

On current branch commit `b51497b618ade15b6b2b31fbdafa713ea6030b9c`,
`go test ./...` from `services/control-plane` again passed the control-plane, provision, authz, evidencefs,
modpolicy, mountauthority, and PostgreSQL packages, but the migration package hit the Go 10-minute test
timeout in `TestRunnerStatementAfterProjectionFaultsRollbackWithoutAppendingIntermediate/snapshot`.
The focused `TestRunnerPreledgerProjectionFaultsRollbackWithoutAppendingEvidenceOrLedger/status-drift` rerun
passed in 2.27 seconds. This current-ref result supersedes neither prior timeout record nor any focused
profile result; the broad migration test boundary remains open and is not counted as a pass.

### 3.3 Signed-bundle characterization fixture repair

An extended `go test ./... -count=1 -timeout=30m` rerun on commit
`69695d2adff40bd85d2ee58df6514c7064bf0f68` completed rather than timing out. The non-migration packages
passed, while `internal/migration` failed after 1028.579 seconds. The failing runner cases still expected
the former four-entry checked-in bundle (`000004`, four ledger rows), even though the exact signed bundle
now ends at `000006` with six entries. One synthetic admission test also omitted the now-mandatory
lineage/quota profile from its constructed journal header, and the production validator correctly rejected
that loose value.

Commit `f7baf95cf7d07ed4a5aa3c6632079bdb4e50c0c2` changes only those two test files. Runner assertions now derive
the expected head and row count from the decoded signed manifest, and the synthetic admission header binds
the exact profile returned by runtime inspection. No production validator or quota rule was relaxed.

Verification on the repaired source:

- the six previously failing focused tests passed in 1.621 seconds;
- `GOWORK=off GOFLAGS=-mod=readonly go test ./internal/migration -count=1 -timeout=30m` passed in
  1083.628 seconds;
- the same focused scope under `-race` passed in 5.323 seconds;
- `go vet ./...`, `go build ./...`, current-ref all-package compile-only, and `git diff --check` passed.

The passing migration rerun closes the local assertion/timeout boundary recorded above. It does not
supersede the historical failed outputs, constitute an independent security review, or close a Gate.

### 3.4 Source-bound dependency and SBOM refresh

The source-bound derived metadata was refreshed after the fixture repair. The fixed source remains commit
`04a61afec29961d7e09152168858edd2fde8ecde`; its repository tree is `8660d8bee6d2f6ea0742f367887779daa55993a6`,
and its `services/control-plane` subtree is `8ccde5696832a6eb58394f06aa4390f41f3b33b9`. The subtree is
byte-identical to the repaired test source at `f7baf95cf7d07ed4a5aa3c6632079bdb4e50c0c2`; the metadata-only
refresh is commit `8d5afdbcf365c315288256bd901c12ea31302354` and is explicitly excluded from the fixed source
identity.

The current source-bound manifests are 339 tracked files (`9a12be09a339931dc6addb5cc0d9f509bf893843056f25bd465b0ca34d5c872a`)
and 258 Go files (`360af6d9d8421af885e76dee9c119e9f463e149b0b4b995aea5f22180672137a`). `go.mod`, `go.sum`,
the 16-module selected graph, and Linux/Darwin non-test closures remain same-bits: Linux is 7 modules / 30
packages (`48ca0dbaba0f918d99091decd0520a70327c36badb7d74c7cbbe1e180cd66e5f` /
`12a56c91f56460e9757560f00234c06cec462f248df6a770d41172168e9a8d08`), and Darwin is 6 / 29
(`12203596417e4926a8292ad208df4d410ef0d6e89627320e2c4fe08858a5154b` /
`07d05153aff50a4db408a9e4d34c4a298a21f5ccd5615b9940e4e8521e0de354`). Exact Go 1.26.6 `go mod tidy -diff`,
`go mod verify`, `go vet ./...`, `go build ./...`, and Linux amd64/arm64 CGO-free builds passed. The
CycloneDX 1.6 SBOM passed Ajv validation against the official BOM/SPDX/JSF schemas with SHA-256
`3e92dddbc30cf7f6a02b80f0942b1a4cfd4fb1c26f1dfc4310afa9d613cafb93`,
`baa9d3bd1ed57b6751b0887edead6b5063ff53ff7429cf85d476c6c94af0166e`, and
`8bae002c25e723db7ee1f26afde680ae1a2b1a8f6b4b4b0fd65dc3becb090aae`.

| Derived artifact                                            | SHA-256                                                            |
| ----------------------------------------------------------- | ------------------------------------------------------------------ |
| `services/control-plane/dependency-lock.json`               | `d90c4410016b0c200fb19a7c763e62fb7f0cd0aea81dc3e1a9fb54f79236512c` |
| `services/control-plane/sbom.cdx.json`                      | `5260315ecd6da87ef9b8b3c19adca68a3582cff29f95288e66f567fa5e9a5bd8` |
| `contracts/generation.lock.json` (unchanged)                | `a868f8ac39d21a7c4b968e0864e2baa86977a44aa1d0a8dbe42ebf40131c80fe` |
| `services/control-plane/THIRD_PARTY_NOTICES.md` (same-bits) | `1cadb7fc75886f9085a53d3b9cc174b4c024981f609e4d5951e4e3f877dcbb48` |

The semantic contract checker, JSON formatting, source/lock/SBOM cross-binding, and `git diff --check`
passed. The full platform contract-lock check is not claimed on this machine: the declared Node 24.13.1
and Go 1.26.6 tuple was not available as the ambient runtime (Node 26.7.0 and Go 1.26.5 were detected),
so its fail-closed runtime guard stopped before comparison. The historical vulnerability scans remain
time-bound and `current_source_vulnerability_scan=NOT_CLAIMED`; no zero-finding result is inherited for
`04a61af`. Historical supply records remain unchanged.

## 4. Review and Gate boundary

This is implementation evidence only. An independent security review of profile authority, direct PostgreSQL
fail-closed behavior, historical replay, and all admission transitions is still required before `000006` is
considered admissible. Production catalog publication/CLI trust-root wiring remains unpublished or
`NOT_IMPLEMENTED`; no production database mutation was performed.

`G-CONTRACT`, `G-DATA`, `G-AUTHORITY-P1`, `G-SECURITY-P1`, `G-SUPPLY-CHAIN`, and aggregate Gate closure remain
open. The historical blocker record
[`membership-rbac-subject-issuer-quota-blocker-20260817.md`](membership-rbac-subject-issuer-quota-blocker-20260817.md)
is retained unchanged as the reason this versioned profile was required.
