# P1-A2.3 durable coordination evidence-quota blocker - 2026-08-19

- Status: **BLOCKER RECORDED - NEW PROTOCOL DECISION NOT AUTHORIZED**
- Fixed source: `79b603b7a4569a3f1f816c9d3bf3a417431e5926`
- Fixed A2.3 slice-3 implementation: `59ec26037ddeba7157f358693426ca1fe2d6231e`
- Branch: `codex/cloud-agents-platform-p1`
- Affected boundary: P1-A2.3 slice-3 independent-review remediation and migration admission
- Existing authorization: generated contract-registry profile only, ordered A2.3 slices, no HTTP/P2 external side
  effect, and no Gate closure
- Does not authorize: rewriting `000008`, selecting or inferring a new quota profile, changing an inclusive evidence
  maximum, production database mutation, deployment, release, or any Gate closure

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
admission evidence, an independently reviewed closure, or a Gate result.

## 2. Exact capacity contradiction

The checked-in statement-count history through `000007` is `[20, 71, 46, 20, 1, 1, 89]`. `000008` adds 34
statements. The current `000009` candidate has two statements; the table also shows a hypothetical one-statement
forward repair to prove that reducing `000009` alone cannot remove the contradiction. `execution_policy.max_attempts`
remains the frozen value `3`, and every row uses the approved
`cloud-agents-platform-lineage-quota-profile/v2` formula.

| Bundle                                    | Migrations | Journal segments | Journal records |   Journal bytes | Index bytes |  Combined bytes |
| ----------------------------------------- | ---------: | ---------------: | --------------: | --------------: | ----------: | --------------: |
| through `000007`                          |          7 |               15 |           1,566 |     248,414,208 |   6,705,152 |     255,119,360 |
| through fixed `000008`                    |          8 |           **17** |           1,781 | **282,492,928** |   7,585,792 | **290,078,720** |
| through hypothetical one-statement repair |          9 |           **18** |           1,797 | **284,098,560** |   7,651,328 | **291,749,888** |
| through current two-statement candidate   |          9 |           **18** |           1,803 | **285,081,600** |   7,675,904 | **292,757,504** |
| inclusive maximum                         |            |               16 |          65,536 |     268,435,456 |  16,777,216 |     268,435,456 |

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

It reports the exact current statement counts
`[20 71 46 20 1 1 89 34 2]`, then rejects root quota admission with the 16-segment error. The first test also exposes
that its previous seven-migration golden assertion was stale; updating that assertion cannot turn the rejected
eight- or nine-migration reservation into an admissible one.

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

## 4. Required decision before implementation resumes

A new protocol decision is required before A2.3 slice-3 remediation can be committed. A safe decision must choose
and fully specify one forward-compatible authority model, for example:

1. an explicit new lineage/quota profile whose signed manifest and generation facts bind any changed journal record
   maxima or reservation arithmetic while preserving byte-exact v1/v2 historical replay; or
2. an explicit schema-bundle/lineage epoch rollover protocol that retains and verifies all historical bundle facts
   without requiring every future migration to fit one generation's whole-bundle reservation.

Either route needs closed wire/state semantics, zero/unknown/profile-swap faults, exact quota arithmetic, historical
reopen/successor compatibility, generated fixtures, full migration gates, PostgreSQL matrix reruns and an independent
security review. It must remain separate from the A2.3 generated operation profile; an operation profile is not
evidence quota authority.

Until that decision is explicitly approved, the correct state is:

- fixed `000008` implementation evidence remains historical and unchanged;
- A2.3 slice-3 independent review remains open;
- the current remediation code and migration candidate remain uncommitted and unpushed;
- HTTP/P2/external effects remain absent;
- every immutable and aggregate Gate remains open.
