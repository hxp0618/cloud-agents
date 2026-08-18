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

## 4. Review and Gate boundary

This is implementation evidence only. An independent security review of profile authority, direct PostgreSQL
fail-closed behavior, historical replay, and all admission transitions is still required before `000006` is
considered admissible. Production catalog publication/CLI trust-root wiring remains unpublished or
`NOT_IMPLEMENTED`; no production database mutation was performed.

`G-CONTRACT`, `G-DATA`, `G-AUTHORITY-P1`, `G-SECURITY-P1`, `G-SUPPLY-CHAIN`, and aggregate Gate closure remain
open. The historical blocker record
[`membership-rbac-subject-issuer-quota-blocker-20260817.md`](membership-rbac-subject-issuer-quota-blocker-20260817.md)
is retained unchanged as the reason this versioned profile was required.
