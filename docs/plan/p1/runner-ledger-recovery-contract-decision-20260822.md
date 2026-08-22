# Runner ledger recovery contract owner decision — 2026-08-22

- Status: **APPROVED FOR ORDERED LOCAL IMPLEMENTATION/REVIEW**
- Decision: D-047 / ADR-0023 accepted
- Fixed input source: `353108ca4a7ec9019bb0f844ed4f97af815a4344`
- Fixed input tree: `1e648a5159c64b436e85bd1c3359d1925469ab2b`
- Fixed control-plane subtree: `44174a871ddf2da859f901050634f9f1995f0aa6`
- Branch: `codex/cloud-agents-p1-runner-recovery-contract-decision-20260822`
- Gate effect: **none**

The owner explicitly approved the [ADR-0023](../adr/0023-p1-runner-ledger-recovery-writer-contract.md)
**Decision**, **Closed pair mapping**, and **Ordered slices A-G** on 2026-08-22. This record resolves the decision
blocker identified by the independently approved
[P1 aggregate Gate gap audit](p1-aggregate-gate-gap-audit-independent-review-20260822.md). It does not treat the
earlier contract-only audit review as implementation evidence.

## Approved order

1. Slice A: generated contracts only;
2. Slice B: read-only recovery admission and close-only permits;
3. Slice C: abort-terminal writer;
4. Slice D: commit-observation and ambiguous-resolution writers;
5. Slice E: retry lineage handoff;
6. Slice F: distinct recovery execution-admission and success-writer profiles; and
7. Slice G: typed failure result and complete caller matrix.

The distinct `runner-ledger-recovery-execution-admission/v1` and
`runner-ledger-recovery-success-writer/v1` identities remain mandatory. A close-only admission permit is not a writer,
the writer may consume only one exact bound admission permit, and no ordinary outcome or direct consumer fact may be
converted into mutation authority. Existing preflight, consumer, entry-admission, execution-admission, and
first-attempt success-writer v1 artifacts remain immutable.

Each authority-expanding fixed slice requires its own P0/P1/P2 independent verdict before the next such slice. A
slice may change only the exact `MIGRATION_PROJECTION_NOT_IMPLEMENTED` rows assigned to it; all other unsupported rows
remain fail closed.

## Explicit non-claims

This approval does not authorize production database writes, HTTP/P2/provider behavior, deployment, publication,
release, a main merge, or any immutable or aggregate Gate closure. It does not authorize physical power actions or
substitute for the dedicated DUT/storage/out-of-band-controller evidence still required by the P1 Gate gap audit.
Local PostgreSQL, filesystem, generator, and contract checks remain test/evidence activity only.
