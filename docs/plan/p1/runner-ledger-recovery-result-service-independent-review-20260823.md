# Runner ledger recovery result service independent review — 2026-08-23

- Verdict: `APPROVE`
- Severity: `P0=0 / P1=0 / P2=0`
- Fixed candidate: `2b01ede54b7a1b2a1c42dfcce3361a8301ab7fce`
- Candidate tree: `7a6a3028a9fa1486aa6674d9a3cfe40a235596f8`
- Candidate control-plane subtree: `c1d678f708ec231b446a11e46572a11fccefc97c`
- Code commit: `1d58d43e4fe0551f3aabaeebfccbcd04835ca26f`
- Approved Slice F base: `e1cb598c0c950e6f0ce34ae38e629e5ec4c5438f`
- Slice F independent review: `39d5d758eab82f09d2d27593a7fdc2994b49a419`
- Candidate branch: `codex/cloud-agents-p1-runner-recovery-result-20260823`
- Candidate matrix: [`runner-ledger-recovery-result-service-matrix-20260823.md`](runner-ledger-recovery-result-service-matrix-20260823.md)
- Candidate matrix SHA-256: `6a3ed3c8219041bdde2d5aca5b3b7d9d5a054ad5e6065379314fd8bd8165a88e`

This is an independent, read-only review of ADR-0023/D-047 Slice G. Approval applies only to the fixed candidate above
and does not authorize a merge, production database access or writes, HTTP/P2/provider behavior, deployment,
publication, release, or Gate closure.

## Fixed identity and scope

The candidate branch was clean, matched its remote branch exactly, and was `0/0` relative to its upstream at review
time. HEAD, parent, tree, control-plane subtree, matrix hash, generation-lock hash, and the supplied result hashes
matched the fixed identity. The diff from the approved Slice F candidate contains exactly 11 paths. It changes no
generated registry, schema, fixture, generated Go profile, SDK, migration asset, or historical entry-v1 artifact.

Verified candidate hashes include:

- consumer loop: `b51784386957a46994c5fa52167db75381f8f0c630f9903a7e600cbe8931fbd0`;
- recovery admission consumer: `4864e6a601860d39cf24b101075df76f5e68eee5cd625a21a1a306355969d9e1`;
- ordinary typed failure result: `b68db38c6068493aec9e2cd4c5f49615b9f009d9769c551906a053c0c56c76a9`;
- typed result and fresh-reentry tests: `d374a714fbe0157a68aa660b1d299657548b59bdf030a1203fa8af2fb2cce97d`;
- complete consumer matrix tests: `7c96650bccc51308e366db40250cc0143f30a7b1977ff16cfe9edc3f930c63c5`;
- recovery profile and production-graph tests: `07aa249089e00f851ff7cd137a8e1fd0f779bbdf2f23a0df2984731fe3fc2136`;
- generation lock: `eb5df821dd7723fcead07075333beb16237d5b7b40f03561d692cc8d35e57fdf`.

The generation-lock diff changes only the recovery Go profile-suite input-manifest SHA-256 to
`sha256:417c736efe2c92c86b9d6481d3984f0ab8ca58c2c2eb3c30ff50c3bd9483d37a`. Independent same-bits checks confirmed
the consumer and recovery generated Go profiles remain byte-identical to Slice F. The generated consumer mapping
still has exactly 17 rows: one complete no-op, five entry rows, and eleven recovery rows. The recovery selector still
has exactly twelve pairs, with profile counts `12/4/1/1/1/3/0/2` and no direct recovery-success-writer pair.

## Loop and result verdict

The reviewed consumer loop accepts only three package-private ordinary step kinds: complete, committed entry, and
re-entry. Abort, pending reconciliation, and retry handoff return only an empty re-entry fact with the unchanged
verified prefix. Exact committed reconciliation and recovery execution return only a canonical committed-entry
outcome bound to migration, ledger head, ledger length, and generated order. Neither ordinary value contains a
permit, session, evidence writer, cursor, or registry capability.

Every iteration consumes a fresh preflight claim. A committed final entry cannot directly return success: it must be
followed by another fresh complete-ledger preflight, whose empty `Applied` and `AmbiguousRecovered` values are ignored
in favor of the loop's separately accumulated ordinary outcomes. The exact expected-prefix check rejects reuse,
skips, duplicate accumulation, and contradictory completion. Successful recovery actions retire the exact consumed
recovery-use record only after their writer, handoff, classification, and cleanup succeed. A missing, replaced, or
inconsistent registry record returns recovery-required rather than converting ordinary data into new authority.

The verified loop bound is exactly:

```text
entry_count * (3 * ExecutionPolicy.MaxAttempts + 2) + 1
```

The implementation validates the execution policy and non-empty ordered entry set, bounds both values to `uint32`,
checks the multiplication before evaluating it, and caps the result at the JSON-safe integer maximum. The extra one
iteration covers the mandatory final fresh complete-ledger reread.

## Typed stable-failure verdict

The return-failure path rebuilds an ordinary result only after rereading the current candidate, active generation,
journal, composite cursor, exact recovery snapshot and tail, ledger prefix, catalog, and verified runtime. Its
domain-separated canonical digest cross-binds terminal or divergent state, migration and attempt, stable redacted
failure evidence, terminal and optional adjacent resolution records, lineage, journal, recovery, consumer fact,
permit boundary, ledger, catalog, and runtime identities.

Only the closed terminal and divergent shapes are accepted. Resolution outcome, record adjacency, attempt, cursor,
catalog, runtime, or durable snapshot drift returns recovery-required. Context, lock, unlock, session-close, and
evidence-close uncertainty takes precedence over the typed error. The public projection uses a fixed message and the
durable stable error tuple; raw database or fixture errors are not returned. No result path introduces an append,
transaction, SQL, successor-generation, HTTP, P2, provider, deployment, publication, release, or Gate edge.

## Checks and evidence boundary

Fresh independent checks on the fixed candidate:

- HEAD/parent/tree/control-plane subtree, supplied SHA-256 values, clean worktree, upstream `0/0`, and remote exact
  branch: PASS;
- static review of failure precedence, fresh final re-entry, bound/overflow arithmetic, ordinary-result reuse and
  accumulation, recovery-use retirement, terminal/resolution/cursor cross-binding, exact 17/12 generated mappings,
  same-bits generated profiles, and forbidden production edges: PASS;
- exact Go 1.26.6 seven-test normal set covering bound, fresh complete preflight, cleanup precedence, result mutation,
  17-row matrix, recovery production graph, and consumer production graph: PASS in `11.197s`;
- exact Go 1.26.6 recovery pair and historical same-bits tests: PASS in `0.488s`;
- candidate-range `git diff --check`: PASS.

The review relied on the candidate record for the wider bounded evidence: the final named normal suite passed in
`94.884s`, the tiny two-test race passed in `67.819s`, and vet, build, recovery generators, lock current, formatting,
diff, and Gitleaks were recorded as PASS. Those wider commands were not independently repeated.

No full `internal/migration`, full shard run, broad race, live PostgreSQL, production database, live power-loss run,
deployment, publication, release, or Gate check was run or claimed. Database and evidence behavior in the focused
path uses in-process fixtures. This review does not claim a production authority invocation.

## Final verdict

`APPROVE, P0=0/P1=0/P2=0` for fixed candidate
`2b01ede54b7a1b2a1c42dfcce3361a8301ab7fce` only. ADR-0023 Slice G's ordered local implementation/review boundary may
proceed under the existing approval, but merge, every external-side-effect boundary, and every Gate remain open and
unauthorized.
