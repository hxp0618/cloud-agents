# Independent review: `G-SECURITY` / P1 / R1

- Verdict: `APPROVE`
- Findings: `P0=0 / P1=0 / P2=0`
- Fixed candidate branch: `codex/cloud-agents-p1-g-security-r1-current-20260823`
- Fixed candidate commit: `c95b1007eb63d32ec4e38b0b9227c62321103c1e`
- Fixed candidate tree: `a7414d3c295f077840efddde4f9df4f4eba2939b`
- Fixed candidate parent: `f399c89bb0f2cc9ee4c62a1aa7bbd61332fe0992`
- Candidate record SHA-256: `54a1450b43a44456820b202fa2846019cb4756d828ea577c473d2c96a28427c7`
- Review date: 2026-08-23 Asia/Shanghai
- Gate effect: none

This is an independent, read-only review of the fixed four-file `G-SECURITY-P1` phase candidate. Approval applies
only to the record's accuracy, lineage, bounded current-source conclusions and explicit non-claims. It does not close
`G-SECURITY-P1`, aggregate `G-SECURITY`, any filesystem/durability slice or another Gate. It does not authorize a
merge, production database access or writes, remote-host action, HTTP/P2/provider effects, deployment, publication,
release, Beta or GA.

## Fixed identity and candidate scope

Opening checks confirmed that the candidate branch was clean, had upstream divergence `0/0`, and matched the remote
branch exactly. The candidate commit changes exactly four documentation paths:

1. `docs/plan/cloud-agents-platform/06-status-tracker.md`;
2. `docs/plan/cloud-agents-platform/evidence/G-SECURITY/P1/CAG-G-SECURITY-P1-20260823-R1.md`;
3. `docs/plan/cloud-agents-platform/evidence/README.md`;
4. `docs/plan/p1/README.md`.

There is no code, contract, schema, migration, runtime or deployment change in the candidate. The record correctly
binds its implementation source to parent `f399c89bb0f2cc9ee4c62a1aa7bbd61332fe0992`, tree
`3e21c1c888761ecd131dfd94038dbc96f623fa08`, and these current source subtrees:

- `contracts`: `40f0f3b44f83c986f9b015d059451e195e285c0a`;
- `sdk`: `e4c5abf9d9cb591df39d9377529c201a1307997e`;
- `scripts`: `d65f14ec5cc8b2bda27af056673e891cda8cebd1`;
- `services/control-plane`: `689942aecbc7f84f692dd71c17d66a607d12b950`;
- `services/control-plane/internal/authz`: `427eb08fb69bef7750b0d8b02de84fb6d2bda35e`;
- `services/control-plane/internal/store/postgres`: `6580be0bbbb3e1056439da0698ec08cca5d46e33`;
- `services/control-plane/internal/migration`: `e01ddef945d4cec352ac107831c9c86af029ff86`;
- `services/control-plane/internal/evidencefs`: `5e4b0b76a8f1b8c6408b4505a8befa8e5e3369ed`;
- `services/control-plane/internal/mountauthority`: `f64f4c47e96843ff8dde38c401d138ade9b0eaaf`;
- `services/control-plane/scripts`: `6337c5c73ce21ac1487a680352db7393185274da`;
- `services/control-plane/migrations`: `d4911935523374018393f3b13e24c8b8f698343a`.

The Inventory R3, Baseline R4, G-CONTRACT R4, G-DATA R1 and G-AUTHORITY-P1 R1 record/review SHA-256 values reproduced
exactly. ADR-0024, its original independent review and the current Gate-criteria SHA-256 values also reproduced
exactly.

## Security-scope audit

The candidate inherits only exact, narrow reviewed scopes whose current bytes reproduce:

1. G-CONTRACT R4 contributes generated contract and wire-authority facts only; it does not become runtime identity,
   tenant-isolation or request-enforcement evidence.
2. The ADR-0012 issuer/authz slice contributes the exact current authz subtree and `000006` migration. Its scope is
   lexical issuer/subject validation, profile separation and fail-closed evaluation, not cryptographic verification.
3. The A2.3 and A2.4 scopes reproduce the exact current `000009`/`000011` migrations, typed services, tests and local
   service-matrix scripts. Their reviewed conflict redaction, role separation and one-shot behavior remain bounded to
   those slices.
4. ADR-0023 Slice G shares the exact current migration subtree. Its approval remains limited to local recovery
   ordering, redacted failure precedence, profile separation and forbidden external writer edges.
5. G-DATA R1 and G-AUTHORITY-P1 R1 contribute only their current-source identity and open-boundary facts. Their
   approvals do not close their own Gates or this security record.
6. ADR-0024 and its review are exact policy bytes. They remove mandatory physical controller/cache-loss testing from
   the P1 acceptance boundary, but do not make clean `poweroff` crash evidence or import historical filesystem
   results into current closure.

The eleven security-relevant file/script SHA-256 values listed by the candidate reproduced exactly. Earlier
membership mutation, catalog, trusted-mount, production EvidenceSink, ext4/XFS/QEMU barrier and host-crash records are
kept historical; no broader historical verdict is inherited.

## Exit-criteria and blocker audit

The current `G-SECURITY-P1` Gate criteria require whole-schema tenant isolation, cryptographic identity validation,
secret-flow exclusion/redaction, runtime limits/backpressure, current dependency/license/secret scanning and bounded
waivers. The candidate maps these criteria conservatively:

- tenant/composite-FK/FORCE-RLS/runtime-role/pool-context isolation is partial and lacks a fresh whole-current-source
  live PostgreSQL matrix;
- management/service/workload identity is lexical only; no production OIDC/JWT/JWKS verifier, signature/audience/key
  lifecycle implementation or negative matrix is claimed;
- exact A2.3/A2.4/Slice G redaction is narrow; current receipt/outbox/audit/log/trace/fixture/backup/SDK secret-flow
  closure remains open;
- typed fixed SQL exists, but production HTTP limit/rate/deadline/backpressure enforcement is absent;
- current dependency/license/secret/waiver closure is absent, and no current waiver is exercised;
- P2+ Worker secret access, Managed Host pairing, deployment hardening and cutover/proof-session security are not
  claimed.

A bounded production-source scan reproduced zero non-test Go files containing `OIDC`, `JWKS`, `JWT` or `JOSE`, and
zero production HTTP-server implementations. The two OpenAPI documents contain only `BearerAuth` and JWT
bearer-format wire placeholders. The candidate correctly treats this runtime absence as a blocker, not a pass.

The checked-in authority/catalog files reproduced these fail-closed states:

- `authority-v1.json`: `UNPUBLISHED_BOOTSTRAP_MUTABLE` / `NOT_IMPLEMENTED`;
- `global-table-authority-v4.json`: `UNPUBLISHED_BOOTSTRAP_MUTABLE` / `NOT_IMPLEMENTED`;
- `schema-000012.json`: `UNPUBLISHED_BOOTSTRAP_MUTABLE` / `NOT_IMPLEMENTED` /
  `NOT_IMPLEMENTED_A2_1B_REQUIRED`, with `expected_projection` absent;
- migration manifest: lineage `cloud-agents-platform` plus the fixed global-table and database-authority descriptor
  paths;
- generation lock: current generation binding with `notGateClosure=true`.

The dependency lock and SBOM hashes reproduced, but both bind source `f731c6b...`; the SBOM binds the older
vulnerability-scan source `350b53c...`, timestamp `2026-08-18T14:58:19Z`, and
`cloud-agents:current-source-vulnerability-scan=NOT_CLAIMED`. The candidate therefore correctly declines to inherit
the historical zero-finding results.

The accepted durability boundary remains open: physical hard-power/controller/cache-loss is optional hardening, but
the required externally observed no-sync bare-metal crash, ext4/XFS/QEMU combination, exact post-boot verification,
current-source phase record, filesystem `Done` and independent Gate aggregation are not current-closed. Clean
`poweroff` or `reboot` is not promoted to abrupt-crash evidence.

## Independent checks

The following bounded checks ran against the fixed candidate:

- candidate commit/tree/parent, clean branch, upstream `0/0` and remote identity: PASS;
- exact source/subtree/file and prerequisite record/review SHA-256 reproduction: PASS;
- strict authority/catalog status, manifest lineage/path and generation-lock checks: PASS;
- production OIDC/JWT/JWKS/JOSE and HTTP-server absence scan, with OpenAPI placeholders separated: PASS AS BLOCKER
  CONFIRMATION;
- exact named focused test functions and the reviewed same-bits scopes exist at the fixed source: PASS;
- oxfmt `0.62.0` check on all four candidate files: PASS;
- candidate-range `git diff --check` and local Markdown links: PASS;
- Gitleaks `8.30.1` scan of the single candidate commit: PASS, approximately `60.49 KB`, no leaks.

The candidate's exact focused Go results were reviewed as fixed record content and test/source identities were
reproduced. The reviewer did not rerun them because the candidate is documentation-only and no changed runtime byte
introduced a new execution risk. No full or broad migration suite, race suite, live PostgreSQL, current vulnerability
or OSV scan, filesystem/crash test, remote host, production database, HTTP/P2/provider, deployment, publication,
release or Gate action ran for this review.

## Findings and non-claims

- P0: none.
- P1: none.
- P2: none.
- This is a phase-progress record, not immutable Gate closure.
- Cryptographic identity, whole-schema live tenant/RLS/pool isolation, current supply/license/secret evidence, HTTP
  enforcement and accepted durability aggregation remain open.
- Database authority/catalog remains unpublished and runtime-not-implemented.
- No production or external effect is authorized by this review.

## Final verdict

`APPROVE, P0=0/P1=0/P2=0` for fixed candidate `c95b1007eb63d32ec4e38b0b9227c62321103c1e` only. The R1
phase record may proceed under the existing approvals, while `G-SECURITY-P1` and aggregate `G-SECURITY` remain
`IN PROGRESS`, independent review remains separate from Gate closure, and Gate effect remains none.
