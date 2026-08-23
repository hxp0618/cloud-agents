# Independent review: `G-AUTHORITY` / P1 / R1

- Verdict: `APPROVE`
- Findings: `P0=0 / P1=0 / P2=0`
- Fixed candidate branch: `codex/cloud-agents-p1-g-authority-r1-current-20260823`
- Fixed candidate commit: `c7930d5840b564f5bf10c6d0be9fb963a28ee4cb`
- Fixed candidate tree: `d21ea93b3970fddff9c126eda8da26374646df69`
- Fixed candidate parent: `a0f24cf48af40d021b47e818fd9f36ffd5b55499`
- Candidate record SHA-256: `60c6feb7b08f19f91e0e022d519303ca412b028b1b11c41cd83ba9780140ed79`
- Review date: 2026-08-23 Asia/Shanghai
- Gate effect: none

This is an independent, read-only review of the fixed four-file `G-AUTHORITY-P1` phase candidate. Approval applies
only to the bounded current-source record and its explicit open-boundary mapping. It does not close
`G-AUTHORITY-P1`, the aggregate `G-AUTHORITY`, a filesystem slice or any other Gate, and it does not authorize a merge,
production database access or writes, remote-host action, HTTP/P2/provider side effects, deployment, publication or
release.

## Fixed identity and candidate scope

Opening checks confirmed that the candidate branch was clean, had upstream divergence `0/0`, and matched the remote
branch exactly. The candidate commit changes exactly four documentation paths:

1. `docs/plan/cloud-agents-platform/06-status-tracker.md`;
2. `docs/plan/cloud-agents-platform/evidence/G-AUTHORITY/P1/CAG-G-AUTHORITY-P1-20260823-R1.md`;
3. `docs/plan/cloud-agents-platform/evidence/README.md`;
4. `docs/plan/p1/README.md`.

There is no code, contract, schema, migration, runtime or deployment change in the candidate. The record correctly
binds its implementation source to parent `a0f24cf48af40d021b47e818fd9f36ffd5b55499` and the following current
subtrees:

- `contracts`: `40f0f3b44f83c986f9b015d059451e195e285c0a`;
- `sdk`: `e4c5abf9d9cb591df39d9377529c201a1307997e`;
- `scripts`: `d65f14ec5cc8b2bda27af056673e891cda8cebd1`;
- `services/control-plane`: `689942aecbc7f84f692dd71c17d66a607d12b950`;
- `services/control-plane/internal/migration`: `e01ddef945d4cec352ac107831c9c86af029ff86`;
- migration catalog: `add22d3f6404a06b9cb584576fc969bc4a428ecd`.

The Inventory R3, Baseline R4, G-CONTRACT R4 and G-DATA R1 record/review SHA-256 values reproduced exactly. ADR-0024,
its independent review, the four authority ADRs and the Gate-criteria SHA-256 values also reproduced exactly.

## Same-bits and historical-evidence audit

The candidate inherits only narrow conclusions whose current implementation bytes and original review scopes both
match:

1. G-CONTRACT R4 binds the exact current `contracts`, `sdk` and `scripts` subtrees. Its approval covers generated
   contract/wire authority only and does not prove database or runtime writer ownership.
2. The reviewed live-instance retirement candidate has the exact current `services/control-plane` subtree. Its
   approval remains limited to the receipt-gated preflight/retirement and local-recovery repair; it does not become a
   whole-control-plane authority verdict.
3. ADR-0023 Slice G's fixed candidate and the current source share the exact
   `services/control-plane/internal/migration` subtree. Its approval remains limited to the ordered local recovery
   result boundary, failure precedence and profile separation.
4. ADR-0024 and its review are exact policy bytes. They alter the accepted evidence boundary only and do not supply a
   filesystem, database-authority or Gate result.

Earlier catalog, lineage/quota, trusted-mount, EvidenceSink, ext4/XFS/QEMU barrier and host-crash records are described
only as historical support. Their broader verdicts are not inherited. The candidate explicitly records the differing
catalog, migration, EvidenceFS and control-plane subtrees, and it keeps the unreviewed single-ext4 host-crash result out
of current filesystem `Done`.

## Authority and durability mapping

The checked-in authority/catalog subjects reproduced these exact fail-closed states:

- `authority-v1.json`: `UNPUBLISHED_BOOTSTRAP_MUTABLE` / `NOT_IMPLEMENTED`;
- `global-table-authority-v4.json`: `UNPUBLISHED_BOOTSTRAP_MUTABLE` / `NOT_IMPLEMENTED`;
- `schema-000012.json`: `UNPUBLISHED_BOOTSTRAP_MUTABLE` / `NOT_IMPLEMENTED` /
  `NOT_IMPLEMENTED_A2_1B_REQUIRED`, with `expected_projection` absent;
- migration manifest: lineage `cloud-agents-platform`, schema head `000012`, and the fixed global-authority descriptor;
- generation lock: `notGateClosure=true`.

A bounded current-source scan found no provider-catalog implementation. The only active generated durable-coordination
profile fixes `createsPlatformOperation=false` and `externalSideEffect=forbidden`; the opaque service profile is valid
only while both runtime flags remain disabled. Existing PlatformOperation/attempt/receipt tables therefore do not
constitute an enabled writer.

The exit table remains conservative: Tenant/Organization/Project and receipt/aggregate persistence authority are open;
membership/RBAC, contract, coordination and rollback facts are partial; provider catalog is absent; published/runtime
database authority and cumulative executable projection are absent; and P2+ Session/Turn/Worker/Lease/workload/pairing
and T3 writers are explicitly not claimed.

ADR-0024 correctly makes physical controller and cache-loss testing optional hardening. The candidate does not promote
clean `poweroff` or `reboot` to crash evidence. The accepted ext4/XFS/QEMU plus externally observed no-sync bare-metal
crash combination, post-boot exact verification, current-source phase aggregation, filesystem `Done` and independent
Gate review remain open.

## Fresh independent checks

The following bounded checks ran against the fixed candidate:

- exact Go `1.26.6 darwin/arm64` five-test authority/catalog set: PASS in `0.760s`;
- exact Go `1.26.6 darwin/arm64` five-test PostgreSQL service/writer/rollback set: PASS in `0.666s`;
- strict descriptor/hash and generation-lock checks: PASS;
- provider-catalog absence and active generated-profile/static binder checks: PASS;
- exact same-bits comparison for G-CONTRACT, retirement and Slice G review scopes: PASS;
- exact oxfmt `0.62.0` check on the four candidate files: PASS;
- candidate-range `git diff --check` and named local links: PASS;
- Gitleaks `8.30.1` scan of the single candidate commit: PASS, approximately `18.17 KB`, no leaks.

The unqualified PATH Go binary reported `1.26.7`; its focused passing run was not used as exact-toolchain evidence. Both
named sets were rerun with the fixed Go `1.26.6` binary and only those results support this review.

No full or broad `internal/migration` suite, race suite, live PostgreSQL, filesystem/crash, remote host, production
database, HTTP/P2/provider, deployment, publication, release or Gate action ran for this review.

## Findings and non-claims

- P0: none.
- P1: none.
- P2: none.
- The candidate is a phase-progress record, not an immutable Gate closure.
- No provider catalog, complete neutral-resource writer set, enabled operation/attempt/receipt writer, published
  database-authority subject, executable cumulative projection, aggregate authority review or filesystem `Done` is
  claimed.
- No legacy Synara schema or writer becomes public authority.
- No production or external effect is authorized by this review.

## Final verdict

`APPROVE, P0=0/P1=0/P2=0` for fixed candidate `c7930d5840b564f5bf10c6d0be9fb963a28ee4cb` only. The R1
phase record may proceed under the existing approvals, while `G-AUTHORITY-P1` and aggregate `G-AUTHORITY` remain
`IN PROGRESS`, reviewer closure remains separate from Gate closure, and Gate effect remains none.
