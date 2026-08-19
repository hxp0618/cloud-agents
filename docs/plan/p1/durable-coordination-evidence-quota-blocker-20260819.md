# P1-A2.3 durable coordination evidence-quota blocker - 2026-08-19

- Status: **QUOTA/PROFILE AND A2.3 REMEDIATED SLICE REVIEW PASS; LOCAL FULL MIGRATION CLOSURE PASS; ALL GATES OPEN**
- Fixed source: `79b603b7a4569a3f1f816c9d3bf3a417431e5926`
- Subsequent full-closure source: `67b8acb0f8d405d893bd4f89d71cf3587dd92977`
- Fixed A2.3 slice-3 implementation: `59ec26037ddeba7157f358693426ca1fe2d6231e`
- Branch: `codex/cloud-agents-platform-p1`
- Affected boundary: P1-A2.3 slice-3 independent-review remediation and migration admission
- Existing authorization: generated contract-registry profile only, ordered A2.3 slices, explicit
  `cloud-agents-platform-lineage-quota-profile/v3`, no HTTP/P2 external side effect, and no Gate closure
- Does not authorize: rewriting `000008`, inferring a quota profile, widening v1/v2 in place, production database
  mutation, deployment, release, or any Gate closure

## 1. Finding

The A2.3 slice-3 handoff fixed append-only `000008_add_durable_coordination_service.sql` at 34 classified SQL
statements and explicitly recorded that the full long-running migration test suite had not completed. During the
subsequent independent-review remediation, the full migration boundary reached the current checked-in bundle and
reproduced a deterministic evidence-quota failure:

```text
MIGRATION_EVIDENCE_JOURNAL_LIMIT_EXCEEDED: evidence-quota: journal requires more than sixteen segments
```

This is not introduced by the uncommitted forward `000009` remediation candidate. The checked-in eight-migration
bundle ending at immutable `000008` already needs 17 reserved journal segments and exceeds the 256 MiB journal and
whole-bundle reservation maxima. Adding any nonempty forward migration cannot make that whole-bundle reservation
smaller under the currently selected v2 arithmetic.

The generated contract-registry profile, typed request-projection binder, conflict-redaction correction and
PostgreSQL matrix changes in the current worktree therefore remain an uncommitted candidate. They are not migration
admission evidence, an independently reviewed closure, or a Gate result. The later local identity remediation is
described below; the historical independent-review verdict remains controlling until it is rerun against this exact
candidate.

## 2. Exact capacity contradiction

The checked-in statement-count history through `000007` is `[20, 71, 46, 20, 1, 1, 89]`. `000008` adds 34
statements and the current versioned `000009` candidate contains 30 classified statements. The frozen historical
registry/profile pair remains available for replay, while the current pair is used by the versioned service entry.
`execution_policy.max_attempts` remains the frozen value `3`; the immutable rows use the approved
`cloud-agents-platform-lineage-quota-profile/v2` formula and the current row uses the explicitly generated
`cloud-agents-platform-lineage-quota-profile/v3` formula.

| Bundle                                  | Migrations | Journal segments | Journal records |   Journal bytes | Index bytes |  Combined bytes |
| --------------------------------------- | ---------: | ---------------: | --------------: | --------------: | ----------: | --------------: |
| through `000007`                        |          7 |               15 |           1,566 |     248,414,208 |   6,705,152 |     255,119,360 |
| through fixed `000008`                  |          8 |           **17** |           1,781 | **282,492,928** |   7,585,792 | **290,078,720** |
| through current versioned `000009` pair |          9 |               19 |           1,972 |     312,639,488 |   8,368,128 |     321,007,616 |
| v2 inclusive maximum                    |            |               16 |          65,536 |     268,435,456 |  16,777,216 |     268,435,456 |
| v3 inclusive maximum                    |            |               32 |          65,536 |     536,870,912 |  16,777,216 |     536,870,912 |

For the fixed `000008` bundle alone, the journal reservation is `14,057,472` bytes over its maximum and the
combined reservation is `21,643,264` bytes over its maximum. The calculator encounters segment 17 first and returns
the stable segment-limit error before reaching those byte checks.

The focused reproduction on the current candidate is:

```sh
GOWORK=off GOFLAGS=-mod=readonly \
  go -C services/control-plane test \
  -run '^(TestCheckedInBundleQuotaReservationExact|TestRootQuotaAdmissionDedupeExactAndOneShot)$' \
  -count=1 ./internal/migration
```

It reports the exact current statement counts `[20 71 46 20 1 1 89 34 30]` and the v3 reservation facts above. The
focused quota assertion now passes; the historical v1/v2 pair remains byte-exact and is still checked against its
original limits.

## 3. Frozen constraints

The existing approvals do not permit any of these shortcuts:

1. rewrite, squash, or silently reclassify fixed `000008`; ADR-0013 requires immutable applied-migration rules and
   the slice-3 handoff binds its exact 34-statement source;
2. lower `max_attempts = 3`, omit SQL statements, omit durable checkpoints, or use observed/average frame sizes;
3. widen the 16-segment or 256 MiB inclusive maxima in place;
4. infer a different quota profile from schema head, migration count, source revision, statement count, or observed
   reservation size;
5. reinterpret `predecessor_schema_bundle` as a lineage rollover without an explicit versioned rollover contract;
6. commit the current `000009` candidate and describe deterministic generation or a passing PostgreSQL matrix as
   migration admission;
7. open HTTP/P2, add an external delivery implementation, mutate a production database, or close any Gate.

ADR-0012 v2 changes only the closed checkpoint maximum to 4 KiB. It deliberately leaves the 16-segment,
65,536-record, 256 MiB journal/whole-bundle, index, root and object maxima unchanged. The earlier approval of the
versioned lineage/quota profile direction authorized that explicit v2 decision; it does not implicitly mint a v3
profile or a rollover authority.

## 4. Approved decision and remaining review boundary

The owner selected route 1 on 2026-08-19: an explicit generated-manifest v3 lineage/quota profile. The exact
decision is frozen in [ADR-0014](../adr/0014-p1-lineage-quota-profile-v3.md):

- exact manifest/profile pair `migration-manifest/v3` + `lineage-quota-profile/v3`;
- 32 journal segments and 512 MiB journal/whole-bundle reservation;
- unchanged 65,536 journal records and 4 KiB checkpoint maximum;
- unchanged lineage-index, root and object maxima;
- byte-exact v1/v2 historical replay;
- no rollover, inference, HTTP/P2 external effect, production mutation, release or Gate closure.

The decision is approved, and independent review accepted this quota/profile boundary. The reviewed candidate has
closed format/profile semantics, zero/unknown/profile-swap faults, exact quota arithmetic,
historical profile compatibility, generated fixtures and fresh PostgreSQL matrix evidence. Contract/bundle/
generation-lock checks, focused v3 normal and race tests, control-plane compile/vet/build, Linux amd64/arm64
compilation, the PG15/16/17 kernel matrix and the PG15/16/17 service/claim normal/race/fault matrix all pass locally.
The operation-specific generated identity profile now binds an ASCII identifier of at most 128 bytes with
`exact_string_no_rewrite`; the public Unicode organization-reference contract remains unchanged, so no lossy
conversion or global authorization widening is used.

The historical full `internal/migration` attempt reached the known long-running runner surface and timed out after
10 minutes without an assertion failure. That observation remains **not a pass**. Subsequently, stale test-only quota
and checked-in bundle identity assertions were bound to the current `000010`-inclusive bundle by `b39b070 → 67b8acb`.
At exact `67b8acb`, the authoritative local rerun with `-timeout=30m` passed in `1012.165s`; the precise command,
fixture-only scope, and retained A2.4/Gate boundary are recorded in the
[full migration closure](durable-coordination-full-migration-closure-20260820.md).

The historical review returned `NOT APPROVE, P0=0/P1=1/P2=0` because generated `organizationRef` accepted NFC
Unicode identifiers while service/claim used an ASCII-only, 128-byte `authz.ScopeRef`. The exact remediation candidate
was independently rereviewed as `APPROVE, P0=0/P1=0/P2=0`: the operation-specific generated profile is ASCII, at
most 128 bytes and `exact_string_no_rewrite`; the public Unicode organization-reference contract remains unchanged;
no identity conversion or authorization widening is used. See the
[remediation independent review](durable-coordination-v3-remediation-independent-review-20260820.md) and retain the
[historical review](durable-coordination-v3-independent-review-20260819.md) as history.

After the remediation independent review, fixed-source evidence and the remaining full migration closure have the
following bounded state:

- fixed `000008` implementation evidence remains historical and unchanged;
- quota/profile, append-only kernel and remediated service/claim/matrix implementation-review slice is approved;
- the remediation implementation/review slice is approved; this record does not itself claim a commit, push or
  deployment;
- the later current-bundle full migration suite passed locally at `67b8acb`; the historical ten-minute timeout remains
  a non-pass observation, not retroactive evidence;
- HTTP/P2/external effects remain absent;
- every immutable and aggregate Gate remains open.
