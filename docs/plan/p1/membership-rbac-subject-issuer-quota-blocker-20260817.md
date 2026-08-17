# P1-A2.2 subject issuer closure / lineage-index quota blocker - 2026-08-17

- Status: **BLOCKER RECORDED - protocol change not authorized**
- Fixed clean base: `a976279d755e05982a1de10d339347d34e2ecf78`
- Affected slice: P1-A2.2-impl-3 independent-review remediation
- Does not authorize: a limits/profile change, an applied-migration rewrite, a Gate closure, deployment, or release

## 1. Finding

The independent A2.2-impl-3 review found that the public Go `SubjectRef` rejected malformed absolute URI
issuers before acquiring a database connection, while the direct PostgreSQL mutation functions admitted a
strictly larger issuer language. In particular, an invalid percent escape such as
`https://identity.example.test/%zz` could reach `subject_ref_digest`, allocate a tenant revision, and append
Membership/RoleBinding, resource-change, and audit facts even though the typed Go operation rejected the same
bytes.

The narrow correction is append-only migration `000006_close_subject_issuer_validation.sql`. It replaces only
`cloud_agents.subject_ref_digest(text,text,text)` and gives Go and PostgreSQL the same non-normalizing lexical
profile: ASCII scheme, required colon, no ASCII control/DEL bytes, and exactly two ASCII hexadecimal bytes after
each percent sign. Unit/classifier checks pass and the generated six-migration bundle is deterministic.

The local candidate matrix also passes PostgreSQL 15, 16, and 17, normal and race legs, including five direct
invalid-issuer faults per version. The migration-owner observer confirms that each set leaves tenant revision,
Membership, RoleBinding, resource-change, and audit-fact counts at zero. This is candidate implementation
evidence only: it is not admission evidence, an independent reviewed closure, or a Gate result.

That correction cannot yet be admitted by the frozen evidence-quota contract.

## 2. Exact capacity contradiction

The checked-in five-entry bundle has statement counts `[20, 71, 46, 20, 1]`. The append-only correction makes
them `[20, 71, 46, 20, 1, 1]`; `execution_policy.max_attempts` remains the frozen value `3`.

Applying the exact ADR-0010 whole-bundle formula produces:

| Fact                                        | Five-entry bundle | Six-entry candidate |          Inclusive maximum |
| ------------------------------------------- | ----------------: | ------------------: | -------------------------: |
| journal segments                            |                10 |                  10 |                         16 |
| journal records, including rotation headers |             1,003 |               1,018 |                     65,536 |
| checkpoint records                          |             1,002 |               1,017 | 16,384 index records total |
| journal reserved bytes                      |       158,597,120 |         160,169,984 |                268,435,456 |
| lineage-index records                       |             1,006 |               1,021 |                     16,384 |
| lineage-index reserved bytes                |        16,711,680 |      **16,957,440** |             **16,777,216** |
| combined reserved bytes                     |       175,308,800 |         177,127,424 |                268,435,456 |

Only the lineage-index byte limit fails. The candidate exceeds it by exactly `180,224` bytes. The value is not
an observed/sample encoding: ADR-0010 requires every checkpoint to reserve the full closed
`MaxFramedGenerationCheckpointBytes = 16 KiB`.

Even if the new generation header were incorrectly omitted from the calculation, the candidate would reserve
`16,924,672` lineage-index bytes and still exceed the fixed maximum by `147,456` bytes. The contradiction is
therefore not caused by a discretionary new-header allowance.

The focused reproduction is:

```sh
GOWORK=off GOTOOLCHAIN=local GOFLAGS=-mod=readonly \
  go -C services/control-plane test -count=1 \
  -run '^TestCheckedInBundleQuotaReservationExact$' ./internal/migration
```

It fails before SQL/connect authority with:

```text
MIGRATION_EVIDENCE_JOURNAL_LIMIT_EXCEEDED: evidence-quota: whole-bundle reservation exceeds a fixed inclusive maximum
```

The migration generator itself remains deterministic and current for the candidate:

```sh
bun scripts/generate-platform-migration-bundle.ts --check
bun scripts/check-platform-migration-bundle.ts
```

Those generator results do not override the failed admission quota.

## 3. Frozen constraints

The following are not admissible remediations:

1. rewrite `000004` or `000005`; applied SQL is immutable exact-byte input;
2. lower `max_attempts = 3`; ADR-0009 fixes the initial execution-policy object and ADR-0010 makes it the sole
   attempt-budget authority;
3. widen the 16 MiB lineage-index maximum; ADR-0010 makes every evidence/index/root maximum non-overridable;
4. exclude `000006` from the statement closure or skip its checkpoints; the runner must durably evidence every
   SQL statement;
5. reserve a sample/average checkpoint size; ADR-0010 expressly requires the closed record-kind maximum;
6. treat `predecessor_schema_bundle` as a rollover mechanism; the current v1 model still requires migration IDs
   to start at `000001` and remain contiguous in the active bundle.

A Go-only issuer check leaves the reviewed direct-database authority gap open and therefore cannot close
A2.2-impl-3.

## 4. Required decision before implementation

The safe forward route requires a separately approved protocol amendment, not an implementation shortcut. A
candidate design would introduce an explicit new lineage/quota limits profile that:

- preserves byte-exact v1 decoding and quota recomputation for every historical v1 generation;
- binds the selected profile into verified manifest/generation authority rather than inferring it from schema
  head, statement count, or implementation version;
- defines a smaller closed checkpoint DTO/wire maximum for new generations and proves every generated
  checkpoint fits it;
- keeps the existing 16 MiB index, 16-segment journal, root, and object maxima unchanged;
- fails closed on zero/unknown/profile-swapped authority and receives independent security review before
  `000006` is considered admissible.

No such profile or rollover contract exists in the currently approved ADR-0009/ADR-0010 text. Until one is
approved, the candidate remains uncommitted work and A2.2-impl-3 independent review remains open.

An independent reviewer reproduced the exact quota calculation and classified this as a P0 capacity blocker:
the issuer correction is required to close a direct-database authority gap, while every currently approved
admission path rejects the resulting six-entry bundle. The review found no approved implementation seam that
preserves all frozen constraints.

## 5. Resume evidence

The independently reviewed candidate snapshot was bound to these SHA-256 values:

- `000006_close_subject_issuer_validation.sql`:
  `5b24e8462c90b7d430717ac746e00f999b21f21eae0fb855444379807c0b47e5`;
- generated `migrations/manifest.json`:
  `aac35048304495d1a8c723f94fc278bb7e5a86a31cd9b56aac3cf1c7e6f8edf7`;
- `internal/migration/evidence_quota.go`:
  `4f8dbd61c29a124cde2ba6dc1271a8b856ce147e4fef7e03c38a70f4d0b648de`;
- candidate `internal/migration/evidence_quota_test.go`:
  `f6f1bfa1ad9bc72e9e4ccd235511967f020fa4bfa933c5bc88108c2e7b6b2d68`.

These hashes identify the blocked worktree snapshot; they do not make it an admitted or committable bundle.

Resume only from the named worktree and re-check concurrent state first:

```sh
cd /Users/huang/devel/project/huang/business/cloud-agents-platform-p1
git status --short --branch
git rev-parse HEAD
git rev-parse '@{u}'
git diff --check
```

At the time of this record, both local and upstream refs were
`a976279d755e05982a1de10d339347d34e2ecf78`; no implementation, profile, Gate, merge, deployment, or release
commit had been created or pushed for this remediation.
