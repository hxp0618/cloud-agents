# Independent review: `G-DATA` / P1 / R1

## Verdict

`APPROVE — P0=0 / P1=0 / P2=0`

This is an independent review of the fixed G-DATA R1 candidate. The verdict approves only the accuracy and boundary of
the current-source phase record. It does not close `G-DATA`, satisfy the P1 Exit Gate, authorize a merge, or authorize
production database access or writes, remote-host access, HTTP/P2/provider effects, deployment, publication or release.

## Fixed candidate identity

- Candidate branch: `codex/cloud-agents-p1-g-data-r4-rebind-20260823`
- Candidate commit: `0eb281556b1408b1fc1ef7ee86de0ecf05dbdc07`
- Candidate tree: `da64b2190e56ff99a540c34898260f84c2ccc06e`
- Candidate parent / fixed source: `6420eaf31e1e9350b2aafd09f47505c5a57e4b73`
- Fixed source tree: `c9633659db407f813e57c191743df50396cd27b2`
- Fixed `services/control-plane` subtree: `689942aecbc7f84f692dd71c17d66a607d12b950`
- Fixed `services/control-plane/internal/migration` subtree:
  `e01ddef945d4cec352ac107831c9c86af029ff86`
- [Candidate record](CAG-G-DATA-P1-20260823-R1.md) SHA-256:
  `1cf34ac76778f28dc790ac2d6b780b0d7b526826d9b2d8596065a610d80d7a0d`

At opening and final reread, the candidate worktree was clean, upstream divergence was `0/0`, and the remote branch
resolved to the exact candidate commit. The parent diff contains exactly four paths: the new candidate record plus the
status tracker and two evidence indexes. No implementation, contract, migration, generated artifact or SDK path changes
in the candidate.

## Prerequisite and source binding

The record's prerequisite and current artifact hashes were independently recomputed and matched:

- Inventory R3: `d865bb9fd1195342a7251bd6822130437753f5be4af8312d9d5c5ee4d5b71400`;
- Baseline R4 record / review:
  `57429377291d1b6a41ff886cde2a6692afd63b5c15adf0677767d59e87b03dd9` /
  `44db2df153bbfcc5fa0bd4c928bbdf9b207c60c4458ec61b2e2557c7d97d4c94`;
- G-CONTRACT R4 record / review:
  `0982261244e7315c2798db4a4f0913f7f93037c251140c8f14ed2cbc3bcd7152` /
  `f0d5b12f1f6e0f2936783868331d4d74d7a3ee0fc49b3e370894b95884458f61`;
- Gate criteria:
  `4a3d0b3c184e9673944411adbc5c8ea933883c855d5aada67862dad8e4dcc994`;
- ADR-0007/0008/0009/0010:
  `9e59b17e6f43db986ca4d5cc09ff62f4acb7cd8ebdfc61583d821ecdba11899c`,
  `6a7b9c525b4625e0f6074ba64b53151224a093837f4dda131fc533cef05fdb91`,
  `baf10f9982a519e0c616281e94cd6daf4f70a098c08f1110e6bbc4317aa666b8` and
  `4f98e7d7acd165fcae85f39ce4337f37ebf2b817fa2999aef0b1f99678d3f32d`;
- migration manifest / schema bundle / generation lock:
  `97c02a54639d9a7d00dbc55a14e06db8e97bc2c36444cf51b61a680539cfd44e`,
  `948e504b77c409065d2160056f45356d84d136d2512f35a4c4fe9e16e575aaaf` and
  `4f2953540e9305f034a8f6fc7d13af0947d7f5b91f43b7ce6256bc137d071c76`.

The source branch `codex/cloud-agents-p1-g-contract-r4-status-20260823` also resolved remotely to the exact fixed source
commit. The candidate correctly distinguishes that implementation source from its evidence-only candidate commit.

## Same-bits and historical-evidence disposition

The fixed control-plane subtree is byte-identical to the independently approved current-source retirement candidate's
control-plane subtree. The fixed `internal/migration` subtree is byte-identical to the independently approved ADR-0023
Slice G candidate's migration subtree. Therefore R1 may inherit only those two bounded reviewed implementation outcomes:

- current-source retirement/recovery implementation / review:
  `388ae77b0758b8ddf85cb7d4ce7fe5c2ac20c9363b3441188d10cd3f232866cf` /
  `816a08901e0f288a8d267a85c08c9b22da1f42d38b8e58d14eef37ad02959339`;
- ADR-0023 Slice G independent review:
  `5cf26966f873c563ba2bc6e84d8b94ebe237534b2d95a137ea89b44db8ce030c`.

The R1 record separately labels the older EvidenceSink, catalog, lineage/quota and local logical-recovery documents as
historical support whose verdicts are not inherited as current same-bits evidence. It also labels the host-crash record
unreviewed. This separation is accurate and avoids upgrading historical implementation reviews into a current Gate
signature.

The inherited retirement review fixes PostgreSQL 15/16/17 patch versions and OCI digests, but proves only the focused
receipt-gated preflight matrix. It also reviews one local PostgreSQL 17 logical backup/restore execution. R1 accurately
classifies these as partial evidence: they do not prove a full compatibility or deployed rolling N/N-1 matrix,
PostgreSQL 15/16 restore, PITR, HA, failover, production recovery or filesystem durability closure.

## Fresh independent checks

The following bounded checks were rerun against the fixed candidate source:

- exact Go `1.26.6 darwin/arm64` four-test data-recovery validator: PASS in `1.026s`;
- Bash 3.2 syntax over the four named recovery/retirement scripts: PASS;
- no-ORM scan for GORM, `AutoMigrate`, Ent, Bun ORM and sqlx under `services/control-plane`: PASS;
- strict manifest check for public `cloud-agents-platform` lineage, exact `000001..000012` order, matching platform SQL
  paths and the fixed global-table-authority path: PASS;
- no `synara` identifier in the authority-bearing manifest, schema bundle or catalog JSON: PASS;
- strict current manifest shape: head `000012`, twelve entries, every entry `phase=expand`, every live-instance preflight
  flag false and every PITR preflight flag false: PASS;
- target oxfmt `0.62.0` on all four candidate files and candidate-range `git diff --check`: PASS;
- relative Markdown links in all four candidate files: PASS;
- Gitleaks `8.30.1` over the one-commit candidate range: PASS, one commit / approximately `17.73 KB`, no leaks.

No `go test ./internal/migration`, full migration suite, shard suite, broad race, live PostgreSQL, container, backup,
restore, crash, QEMU, remote host or production operation was run in this review.

## Exit-criteria audit

The record does not over-close any G-DATA criterion:

- the fixed PG15/16/17 matrix is explicitly limited to the receipt-only retirement preflight;
- current persistence evidence establishes pgx/handwritten-SQL and public migration authority only;
- all twelve migrations remain `expand`, so resumable backfill, code cutover and contract are open;
- runner checksum/lock/reentry/crash-resume evidence is only a partial exact-subtree result, with current filesystem Done
  absent;
- tenant FK/RLS/context/global allowlist evidence is historical support only and has no current-source live DB replay;
- deployed rolling N/N-1 remains open;
- logical recovery remains one bounded local PostgreSQL 17 run;
- P1 PITR preflight is not implemented because all twelve manifest flags are false;
- current-source filesystem/crash aggregation is not verified; and
- this review supplies no immutable Gate closure signature.

The candidate record, tracker and indexes consistently retain `IN PROGRESS`, identify the reviewer as `PENDING` at the
candidate freeze, and declare Gate effect none. This independent verdict approves the fixed candidate record without
changing those status boundaries.

## Findings and final verdict

- P0 findings: `0`
- P1 findings: `0`
- P2 findings: `0`

Final verdict: `APPROVE, P0=0/P1=0/P2=0` for fixed candidate
`0eb281556b1408b1fc1ef7ee86de0ecf05dbdc07` only. `G-DATA` remains `IN PROGRESS`; no Gate, external side effect,
production database action, deployment, publication or release is authorized or closed.
