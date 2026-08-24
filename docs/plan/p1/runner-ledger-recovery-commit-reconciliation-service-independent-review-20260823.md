# Runner ledger recovery commit-reconciliation service independent review — 2026-08-23

- Verdict: `APPROVE`
- Severity: `P0=0 / P1=0 / P2=0`
- Fixed candidate: `7bbc39185f11e72223e40a081e6dd62b23e53de8`
- Candidate tree: `b8e431c33d4f644dca7269f2313b12749ca0ffc4`
- Candidate control-plane subtree: `faa06742f02da2311eac1f6457d449c20f63a3d6`
- Repair code commit: `ca59e54f64302d865890bc8ac7cfd2bc163f8968`
- Approved Slice C base: `6fd2873497765336d872dfaa53cd4e12541c5c26`
- Candidate branch: `codex/cloud-agents-p1-runner-recovery-commit-reconciliation-20260823`
- Candidate matrix: [`runner-ledger-recovery-commit-reconciliation-service-matrix-20260823.md`](runner-ledger-recovery-commit-reconciliation-service-matrix-20260823.md)
- Candidate matrix SHA-256: `bdd472acfcac25ea247eae9d93b87c8ae40910fd134e346de58bd3913df4930e`

This is an independent, read-only review of ADR-0023/D-047 Slice D. Approval applies only to the fixed candidate above
and does not authorize a merge, production database access or writes, HTTP/P2/provider behavior, deployment,
publication, release, or Gate closure.

## Fixed identity and scope

The candidate branch was clean, matched its remote branch exactly, and was `0/0` relative to its upstream at review
time. The reviewed commit, tree, control-plane subtree, matrix hash, and implementation hashes matched the fixed
identity supplied for review.

The Slice D scope remains limited to the two adjacent generated recovery writer identities:

1. profile 2, `runner-ledger-commit-observation-writer/v1`, for `dangling_commit_intent`;
2. profile 3, `runner-ledger-ambiguous-resolution-writer/v1`, for `ambiguous_unresolved`.

The immutable v1 empty-prefix case remains unsupported and reaches the existing close-only
`MIGRATION_PROJECTION_NOT_IMPLEMENTED` boundary. The four Slice C abort pairs are unchanged, and the other six
generated recovery pairs plus all later entry/recovery families remain unimplemented.

Verified candidate implementation hashes include:

- reconciliation observation: `34469339b210b53ed80407e8f6ef2d6e73ca7c40dae572dd9bdaae1806640765`;
- reconciliation writer kernel: `2170015c271c0b23fc74099aa8e5130fcbc1809bc1797bdbb545386ff02cf225`;
- reconciliation writer tests: `90598b8e1e65349add2e8c9c801b51953c9936c1718b33d0c6515cfaf76c5785`;
- catalog preflight: `075802ce629b2726a6e4e7950b05551eef39a9ee470d7f01994d56110ac69b62`;
- recovery admission routing: `8278cbfa064afeb5217ec9b1cca8a57e036e0e0575c809d8a87f72dd60120c47`;
- generation lock: `ba6eb1409a882c670e49fa08090c7bd8d149db6df0bf277e3c6d505c0f1a9340`.

## Authority and state-machine verdict

The review confirmed the following boundaries:

- commit-observation and ambiguous-resolution use distinct binders, permit registries, record types, digest domains,
  generated profiles, and append kernels; neither identity can consume or convert the other;
- the ordinary reconciliation hint binds the complete canonical `CommitIntent`, durable record digest, signed ledger
  row, migration/attempt, predecessor catalog, final catalog projection-domain digest, and adjacent unresolved
  terminal when present;
- exact pending, exact committed, and divergent classification is rederived from a fresh locked read-only session and
  rechecked against the same-verifier durable prefix; operational uncertainty authorizes no append;
- the concrete `generationEvidenceSession` rereads the physical evidence prefix and binds the exact terminal or
  adjacent resolution witness before the only permitted mutation edge;
- each profile-specific writer contains exactly one `AppendDurable` call and validates composite checkpoint/rotation,
  returned cursor, record digest, post-append snapshot, terminal/resolution body, and next recovery action;
- unknown append outcomes, contradictory result metadata, rotation mismatch, or post-append snapshot drift revoke the
  relevant cursor and require recovery; a proven pre-mutation error retains its stable error class;
- no Slice D kernel acquires `BeginMigration`, SQL execution, ledger insert, database commit, generation
  supersession/reservation/activation, HTTP, P2, or provider authority.

## Independent P1 repair verification

The two superseded candidates remain correctly recorded as blocked:

- `d57f511215eb34e2061e1d02326824723ce999d7` could lose the original locked database session after primary-registry
  tamper;
- `d3bc61ae0e3382c78b0f068b74936d9c2cb4e0a3` allowed a caller-constructed core/binding graph to become cleanup
  authority without registered provenance.

The fixed candidate closes both findings:

- primary and independently sealed cleanup records are removed together under one package-private mutex, so two
  concurrent claims cannot split the registries into separate successor authorities;
- a successful successor claim requires both registry records to match the live owner/core/binding exactly;
- cleanup requires at least one registered provenance source and three exact session/key votes; primary or cleanup
  registry missing, foreign-session replacement, or key drift selects only the original retained session;
- fully forged literals have no registry provenance and return before owner invalidation or database cleanup; copied
  owners use a different map key, cannot consume the original records, and have zero cleanup authority;
- the original permit remains usable after a copied-owner rejection, while a consumed real permit cannot revive or
  repeat cleanup.

No foreign handle is selected by any reviewed single-sided tamper path, and the normal exact claim/close path still
transfers or closes the original session once.

## Checks and evidence boundary

Fresh independent checks on the fixed candidate:

- HEAD/tree/control-plane subtree, matrix SHA-256, clean worktree, upstream `0/0`, and remote exact branch: PASS;
- focused static authority/call-graph review of both registries, cleanup vote selection, successor validity, normal
  claim/close, literal/copy rejection, and one-shot invalidation: PASS;
- Go 1.26.6 `gofmt -d` on the three repair Go files: PASS, empty diff;
- `git diff --check` from Slice C through the fixed candidate: PASS;
- oxfmt `0.62` check of the candidate matrix: PASS.

The review relied on the fixed candidate record for its bounded normal test timings: the exact cleanup regression set
passed in `46.039s`, and the six-row success matrix passed in `17.865s`. These timings were not independently rerun
because the review explicitly prohibited unnecessary long tests.

The earlier narrow race command reached its explicit 10-minute timeout in `601.207s`; it remains **NOT PASS** and is
not race evidence. No full `internal/migration`, full shard run, broad race, live PostgreSQL, production database,
deployment, publication, release, or Gate check was run or claimed by this review.

## Final verdict

`APPROVE, P0=0/P1=0/P2=0` for fixed candidate
`7bbc39185f11e72223e40a081e6dd62b23e53de8` only. Slice D's local implementation/review boundary may proceed under
the existing approval, but every external-side-effect and Gate boundary remains open and unauthorized.
