# G-CONTRACT closure/supply successor R1 assembly-writer fixed-object review

Date: 2026-08-25

## Verdict

`REQUEST_CHANGES`

| Severity | Findings |
| -------- | -------: |
| P0       |        0 |
| P1       |        1 |
| P2       |        0 |

The independent reviewer examined the exact fixed candidate identity and
found one P1 authority-binding gap. The review did not modify the candidate,
the primary worktree, generated successor artifacts, a remote host, a
database, a deployment, a release, or a Gate record. The candidate remains
unchanged. This review record was absent from the candidate and cannot make
the candidate self-reviewing.

The verdict rejects only the fixed-object admissibility of the R1 assembly
writer. It does not invalidate the two earlier working-byte repairs recorded
by the implementation record, and it does not authorize formal Slice C/D,
Slice E, native replay, successor-lock publication, external effects, or Gate
closure. `ALL_GATES_OPEN` remains in force.

## Fixed candidate identity

- branch: `codex/cloud-agents-platform-p0`;
- parent: `7504e2ee9fb4941bcbcee3cdb4a29ebd13f5de58`;
- candidate: `96d72c966bd86ed29abb301cb0ff5bb1fb8ce43e`;
- candidate tree: `84e9c7125c26dc90f637d0a5afca910bdca42b61`;
- parent-to-candidate binary diff SHA-256:
  `0ba28c0e9b3e8110f8a5f466249e63979cbb1602e81cb70f81c58faa24dcbe7d`;
- candidate path count: `12`;
- local `HEAD` and `origin/codex/cloud-agents-platform-p0` pointed exactly to
  the candidate during review;
- this review path was absent from the candidate, and the candidate remained
  unchanged throughout review.

The candidate's exact path identity table is below. SHA-256 is the content
digest of each candidate file (not a Git blob ID); sizes are candidate byte
counts.

| Path                                                                                    | SHA-256                                                            |   Bytes |
| --------------------------------------------------------------------------------------- | ------------------------------------------------------------------ | ------: |
| `docs/plan/cloud-agents-platform/06-status-tracker.md`                                  | `798ebbf206c7a9be4c1b2cfbd9192eaf7c0fa43a1ef34f7e2095d58421f25130` | 136,644 |
| `docs/plan/p1/README.md`                                                                | `989bc55797be2b142333aa9812387cb9ad84c1935fa1d596b241fca7bc40904f` |  64,171 |
| `docs/plan/p1/g-contract-successor-supply-rebind-r1-assembly-writer-repair-20260825.md` | `e688bf268a2da7e9954c8627e0379a0a175dd5499136893f22c629972d615674` |  13,336 |
| `scripts/generate-platform-generator-supply-profile.ts`                                 | `c2113367abf8151cf4c235200eced233f34ebd7b5fbf79bd5dc0766b0fe4215a` |   2,634 |
| `scripts/lib/platform-generator-supply-profile-v2.test.ts`                              | `9d553b7800ad32c95879747f1180407f66a93b729458dda708de41e832398716` |  31,278 |
| `scripts/lib/platform-generator-supply-profile-v2.ts`                                   | `f56d22721d004d738d9df12371d52eb305964ae5d0a227c55bcebfd1f0caf158` |  63,357 |
| `scripts/lib/platform-generator-supply-replay-v2.test.ts`                               | `24dbdb0ed13bcdf1b319491c58fd6d50f4b0e736a7cd809d05217892f0a7b07f` |  19,659 |
| `scripts/lib/platform-generator-supply-replay-v2.ts`                                    | `0c975b849ab0511e983ea5a47c9aa1ec6a3613aa35956743927b2dae74ab636e` |  71,487 |
| `scripts/lib/platform-successor-dag.test.ts`                                            | `dc680820acc10c61a24efa62c76eee83b1bdb77e03379e4264358977a933c689` |   8,637 |
| `scripts/lib/platform-successor-dag.ts`                                                 | `2ef4a346dff522b6093aa2b71f984a705c54c4d9df60a5edba0dd3f7a337d4fe` |  17,006 |
| `scripts/lib/platform-successor-predecessor.test.ts`                                    | `bfc30fc25f2aa3fe08fbcfbe2b7ccd87a73f86d86454f84437904288aa55659b` |  14,940 |
| `scripts/lib/platform-successor-predecessor.ts`                                         | `cb124fa3fc359aab2aeb6e00a2f80fb7ddbe76ebbbe90fc1e5371d246bc6ada3` |  28,879 |

## Scope reviewed

The candidate implements the approved R1 boundary: exact seven raw replay
receipts, one canonical derived summary, ordered eight replay receipts,
exactly two assembly outputs, append-only no-replace publication, resumable
prefix recovery, collective destination-parent/output fences, caller-buffer
copy-on-read, and cumulative v1 predecessor revalidation. The v1 profile and
`contracts/generation.lock.json` remain immutable, and the writer has no
production database, HTTP/P2/provider, deployment, publication, or release
effect.

The reviewer accepted the bounded candidate evidence available before the
finding:

| Check                                                                            |                            Result |
| -------------------------------------------------------------------------------- | --------------------------------: |
| Seven named focused Vitest files                                                 |                    `103/103 PASS` |
| Generator-supply state, exact-arity, exact-16-exclusion and lock-sentinel checks |                            `PASS` |
| Candidate path count and origin identity                                         |                       `12`, exact |
| Fixed-object review                                                              | `REQUEST_CHANGES, P0=0/P1=1/P2=0` |

After the finding was identified, no additional formatter, linter, typecheck,
or Gitleaks run was claimed. No broad Bun suite, broad
`go test ./internal/migration`, native replay, SSH, production database,
HTTP/P2/provider, deployment, publication, release, main merge, or Gate
operation was run.

## P1 finding: schema bytes are not the captured validation authority

In `scripts/lib/platform-generator-supply-profile-v2.ts`, the assembly writer
captures the source and the two schema identities at the beginning of the
transaction (the source read and schema identity reads around lines 682–696),
but discards the schema bytes. The subsequent source and output validation
calls around lines 698 and 720 invoke `validateAgainstSchema(root, ...)`.

`validateAgainstSchema` builds its Ajv instance through `schemaValidator(root)`
(around lines 1482–1490). `schemaValidator` then opens both schema paths again
by lexical path (around lines 1493–1503), rather than using the bytes from the
initial authority snapshot. The final authority fence around lines 1317–1345
checks the identities of the source and schemas, but it does not prove that
the Ajv validator used those captured bytes.

This permits the following working-byte ABA sequence:

```text
schema directory A is captured
  -> schema directory A is replaced by materially different B
  -> Ajv reads B for source/output validation
  -> schema directory B is restored to A
  -> final identity fence observes A and succeeds
```

The writer can therefore validate with B while the final identity evidence
appears to prove A. This breaks the required authority rule that validation,
derivation, and final identity evidence use one stable captured schema
snapshot. The current focused tests cover source/output, raw/ancestor,
parent/output, no-replace and resumable mutations, but contain no deterministic
schema working-byte ABA test for this sequence.

### Crosscheck disagreement and control verdict

A second read-only crosscheck returned `APPROVE, P0=0/P1=0/P2=0`. It assessed
this sequence as, at most, proof hardening because canonical output bytes are
not changed by Ajv's transient B read and the final identity fence re-observes
A. The formal fixed-object review applies the stricter authority standard:
the validator must be demonstrably constructed from the same captured schema
bytes whose identities are later fenced. These are different review
interpretations, not a consensus verdict. For progression control, the
conservative fixed-object result is `REQUEST_CHANGES, P0=0/P1=1/P2=0`.

## Required repair

The next candidate must:

1. retain owned copies of both schema byte sequences in the initial stable
   authority snapshot, alongside their identities;
2. construct one Ajv validator from those owned schema bytes and use that
   validator for both source and output validation, without lexical-path
   rereads;
3. preserve the identity and ancestor fences so any schema identity/bytes
   drift remains fail-closed;
4. add a deterministic test hook that performs a materially divergent
   schema A → B → A working-byte ABA during the validation window and proves
   that the captured validator is used (or rejects closed), never silently
   validating against B; and
5. rerun the focused formatter, linter, typecheck, diff and secret checks after
   the repair before creating the next fixed candidate.

The repair must retain the already-closed caller-owned `Buffer` copy and
complete v1 predecessor cumulative-fence protections. It must not weaken the
exact ten-file writer, external-root topology, no-replace, same-byte no-op,
divergent-conflict, or identity-safe temporary cleanup rules.

## Slice boundary

The candidate is not admissible for formal progression. The R1 repair remains
pending; formal Slice C projection and Slice D Darwin/Linux native replay have
not started, and Slice E successor-lock/evidence assembly is not authorized.
`G-CONTRACT`, `G-SUPPLY-CHAIN`, and every aggregate Gate remain
`IN PROGRESS`/OPEN.
