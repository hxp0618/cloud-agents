# ADR-0014: P1 lineage/quota profile v3

- Status: Accepted — direction approved; quota/profile and remediated A2.3 implementation/review slice approved;
  full migration closure pending
- Date: 2026-08-19
- Decision owner: hxp0618
- Implementation executor: Codex
- Scope: P1-A2.3 migration admission remediation
- Extends: [ADR-0010](0010-p1-postgres-projection-contract.md),
  [ADR-0012](0012-p1-versioned-lineage-quota-profile.md), and
  [ADR-0013](0013-p1-durable-coordination-contract.md)
- Approval basis: the owner explicitly approved the versioned lineage/quota profile route and the ordered A2.3
  registry -> append-only PostgreSQL kernel -> service/claim/matrix/independent-review slices on 2026-08-19
- Does not authorize: a lineage rollover, HTTP/P2 external effects, production database mutation, deployment,
  release, or any Gate closure

## Context

The immutable bundle ending at `000008` requires 17 journal segments and a `290,078,720`-byte combined
reservation. The current versioned `000009` remediation pair requires 19 segments and `321,007,616` combined
bytes. The frozen historical pair remains available for exact replay while the current pair is selected by the
generated service entry.
Both are rejected by the v2 generation limits of 16 segments and 256 MiB even though the physical lineage index,
root and object limits remain valid.

This is a protocol-capacity decision, not a reason to rewrite `000008`, lower `max_attempts`, omit durable records,
infer authority from the observed bundle, or relabel historical evidence. ADR-0012 already makes quota-profile
selection an explicit signed authority fact. This ADR adds one closed profile value through that existing mechanism.

## Decision

### 1. Choose an explicit v3 profile, not a rollover

The selected forward route is a new versioned lineage/quota profile. No schema-bundle or lineage-epoch rollover is
introduced in A2.3. A generation may use v3 only when its verified generated migration manifest selects the exact
v3 pair:

- manifest format: `cloud-agents-platform-migration-manifest/v3`;
- `execution_policy.lineage_quota_profile`:
  `cloud-agents-platform-lineage-quota-profile/v3`;
- every planned and durable `JournalHeader.limits_profile`:
  `cloud-agents-platform-lineage-quota-profile/v3`;
- the v3 quota-reservation and quota-bundle binding domains.

The only accepted format/profile pairs are historical v1, v2/v2 and v3/v3. Empty, unknown, mismatched, swapped,
caller-selected or inferred values fail closed before database or filesystem mutation. A generated current manifest
is the only v3 selector; an A2.3 operation profile is not quota authority.

### 2. Closed inclusive limits

| Limit                                       | Historical v1 |        v2 |        v3 |
| ------------------------------------------- | ------------: | --------: | --------: |
| generation journal segments                 |            16 |        16 |        32 |
| generation journal records                  |        65,536 |    65,536 |    65,536 |
| journal/whole-bundle reservation bytes      |       256 MiB |   256 MiB |   512 MiB |
| reserved checkpoint framed maximum          |        16 KiB |     4 KiB |     4 KiB |
| physical lineage-index bytes                |        16 MiB |    16 MiB |    16 MiB |
| physical lineage-index records              |        16,384 |    16,384 |    16,384 |
| root journal/index/object count/byte limits |     unchanged | unchanged | unchanged |

The v3 record maximum deliberately remains 65,536; it is not derived as `segments * records_per_segment`.
The formula continues to reserve the closed record-kind maximum for every checkpoint, not an observed encoded
size. Increasing evidencefs' profile-neutral physical segment grammar to 32 merely permits v3 bytes to be
inspected; it never authorizes a v1/v2 generation to exceed 16 segments.

### 3. Exact arithmetic and profile binding

The v3 arithmetic is the v2 arithmetic with the v3 segment and byte ceilings. For the fixed bundles currently in
scope it must produce these exact facts:

| Bundle                             | Segments | Records | Checkpoints | Journal bytes | Index records | Index bytes | Combined bytes |
| ---------------------------------- | -------: | ------: | ----------: | ------------: | ------------: | ----------: | -------------: |
| through immutable `000008`         |       17 |   1,781 |       1,780 |   282,492,928 |         1,784 |   7,585,792 |    290,078,720 |
| through current versioned `000009` |       19 |   1,972 |       1,971 |   312,639,488 |         1,975 |   8,368,128 |    321,007,616 |

These are generated conformance facts, not authority inferred from statement counts. The verified manifest,
bundle facts, planned header, `GenerationReserved`, quota digest, successor/reopen state and strict historical
replay must all bind the same profile. v3 uses distinct domain-separated quota reservation, quota-bundle,
admission-history and recovery binding digests so a v2/v3 field swap cannot preserve authority.

### 4. Historical compatibility and state-machine rules

All archived v1 and v2 manifests, frames, digests, fixtures and quota results remain byte-exact. Historical replay
chooses limits from the durable generation profile; it never applies the current manifest profile retroactively.
The profile-neutral reader may accept up to the maximum supported 32 physical segments, but structural replay then
enforces the selected header/reservation profile and rejects a v1/v2 generation with segment 17.

A v1/v2 historical generation may lead to a v3 successor only after strict historical replay and same-verifier
binding. The new generation receives the generated current v3 profile in its new reservation/header. Recovery,
rotation and reopen copy the already bound generation profile and cannot accept a caller replacement. Unknown,
missing, stale, zero, copied or profile-swapped authority invalidates the epoch and never triggers a fallback
reservation under another profile.

### 5. A2.3 slice and effect boundary

ADR-0013's three ordered slices remain authoritative. The v3 manifest and derived registries are generated contract
inputs to the existing migration/admission path; they do not add an HTTP handler, real outbox delivery, provider or
worker call, P2 Session/Turn/Execution behavior, or any other external side effect. The forward `000009` remains
append-only and cannot be called admitted merely because generation and PostgreSQL matrix checks pass.

## Required implementation and review evidence

Before the A2.3 independent-review slice can be described as complete, the implementation must provide:

1. deterministic v3 manifest generation and exact v3/v3 format/profile binding faults;
2. v1/v2 byte-exact manifest, digest, checkpoint, quota and historical replay tests;
3. v3 exact and `+1` tests for 32 segments, 65,536 records, 512 MiB bytes and 4 KiB checkpoints;
4. exact quota goldens for immutable `000008` and the forward `000009` candidate;
5. successor, rotation, reopen, copied/stale authority and profile-swap faults;
6. generated migration fixtures, lock/checker consistency, full local migration gates and PostgreSQL 15/16/17
   normal/race/direct-fault matrix reruns;
7. an independent security review of the generated selector, profile/state binding, historical same-bits and all
   pre-mutation failure paths.

Passing these checks is implementation evidence only. It does not close `G-CONTRACT`, `G-DATA`,
`G-AUTHORITY-P1`, `G-SECURITY-P1`, `G-SUPPLY-CHAIN`, or any aggregate Gate.

## Current local implementation evidence

The current uncommitted worktree implements the generated v3/v3 selector, distinct profile domains, profile-aware
quota arithmetic and a profile-neutral 32-segment physical reader while retaining the v1/v2 semantic ceilings. The
following local evidence passed on 2026-08-19:

- deterministic contract registry, migration bundle and generation-lock checks;
- 27 focused TypeScript registry/lock/bundle tests;
- exact v3 quota/manifest/history/recovery tests in normal and race modes, plus v1/v2 compatibility faults;
- control-plane compile-only, vet and build, changed-package normal/race tests, and Linux amd64/arm64 compile checks;
- PostgreSQL 15/16/17 append-only kernel schema matrix;
- PostgreSQL 15/16/17 service/claim normal, race and direct-fault matrix.

The long-running full `internal/migration` suite did not complete within the local 10-minute bound. It produced no
assertion failure before timeout, but it is not recorded as a pass. The historical independent review returned
`NOT APPROVE, P0=0/P1=1/P2=0`: this ADR's quota/profile boundary passed, while the generated Unicode organization
identifier could not enter the ASCII-only authorization `ScopeRef` used by service/claim. The local remediation now
uses an operation-specific generated ASCII identity profile of at most 128 bytes with exact no-rewrite semantics;
the public Unicode organization-reference contract and quota profile remain separate. That historical identity
verdict remains recorded in the
[A2.3 v3 independent review](../p1/durable-coordination-v3-independent-review-20260819.md), while the exact
remediated candidate was independently rereviewed as `APPROVE, P0=0/P1=0/P2=0` in the
[A2.3 remediation independent review](../p1/durable-coordination-v3-remediation-independent-review-20260820.md).
That approval closes only the generated registry/profile, append-only PostgreSQL kernel and service/claim/matrix
implementation-review slice. The remaining full migration closure is still required. No HTTP handler, P2 external
effect, production write, deployment, release or Gate closure is introduced by this evidence.

## Rejected alternatives

### Widen v2 in place

Rejected. It would reinterpret already signed and durable v2 evidence and destroy byte-exact historical replay.

### Infer v3 from bundle size or schema head

Rejected. Capacity is not authority. Only the exact verified generated manifest may select v3.

### Introduce a lineage rollover in A2.3

Rejected for this slice. A rollover requires a separate historical registry/recovery protocol and is unnecessary
for the approved bounded v3 capacity.

### Use A2.3's generated operation profile as quota authority

Rejected. The operation registry controls service/idempotency/state semantics; the migration manifest controls
lineage/quota authority. Neither may substitute for the other.
