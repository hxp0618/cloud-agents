# P1 identity-verifier authz binder independent review - 2026-08-24

- Verdict: **APPROVE**
- Findings: **P0=0 / P1=0 / P2=0**
- Candidate commit: `d6ae9c789f5be06612764c06a5649f5ebd1557c7`
- Candidate tree: `37704851e71e1315b1f2bb3c83a5dd2e68dc3edb`
- Candidate parent: `d2e464be0f3e54aa25e55d6cca7d4f744b04bc1c`
- Candidate diff SHA-256: `3086a28c7214e5d36da12af6e1e55f9905332bc65a3d108a34920aae6b2863fb`
- Review branch: `codex/cloud-agents-p1-identity-verifier-slice-c-independent-review-20260824`

## Reviewed boundary

The fixed candidate exposes exactly five RBAC mutation methods and three JWT-user durable-coordination methods whose
authority parameter is an opaque `*authn.VerifiedPrincipal`. The principal has no production constructor and is spent
once by `authz.WithVerifiedOperation`. Exact binding requires tenant, resource and permission equality, and protected
work can be reached only through the callback-live one-shot `Bind` plus `Execute` path.

The production `go/types` call-graph and exported-surface closures freeze every principal-consume, binder, operation,
actor, snapshot and PostgreSQL bridge edge. The reviewed PostgreSQL paths keep request validation, stored-scope or
profile resolution, authorization fact reads, evaluation, typed SQL, and commit/rollback/unknown-outcome settlement
inside the active verified-principal generation lease. Typed database rejections remain closed results; missing,
stale, copied, tampered, mismatched, cancelled and zero-value authority remains fail-closed without database effects.

The checked-in PostgreSQL 15/16/17 normal/race/fault matrix evidence covers the five public RBAC methods, the three
public durable methods, row-lock invalidation through confirmed commit, cancellation rollback, and statement-level
typed rejection. This independent review inspected that fixed evidence and its pinned scripts but did not rerun the
already-fixed database matrix.

The Slice C mutable runtime closure is bound into `contracts/generation.lock.json` while the generated identity
registry and generated Go profile remain byte-identical to the Slice B base. Their SHA-256 values are:

- contract lock: `abd7d2e99133df1341bd9601fa003d29b8753743340ab134ea78d6f7015ccbc2`;
- generated registry: `474bb31fa5721dd20fc5723b790f39d45fda5ac0392d9e5bb73cb0ecef3e0ccf`; and
- generated Go profile: `e3d9ed08b69b3a7f4ce0ac6d100ea49f577dafdf857ff33be32c4170c357b8de`.

## Independent verification

Using Go `1.26.6`, Node `24.13.1`, and Bun `1.3.14`:

- fixed commit, tree, parent, and diff SHA-256: exact match;
- `go test` for `internal/authn`, `internal/authz`, and `internal/store/postgres`: PASS;
- the same focused packages under `go test -race`: PASS;
- the same focused packages under `go vet` and `go build`: PASS;
- `generate-platform-contract-lock.ts --check`: PASS (`platform-contract-lock: current`);
- targeted identity-verifier registry and contract-lock Vitest: PASS, 2 files / 27 tests;
- both PostgreSQL matrix scripts under `bash -n`: PASS;
- generated registry/profile diff against the candidate parent: empty;
- `git diff --check` for the fixed candidate: PASS; and
- candidate-range gitleaks scan: PASS after a temporary `regexTarget=secret` allowlist limited to the two identical
  synthetic test fixtures `idempotency-key-0001`; manual inspection confirmed those two initial detections were not
  credentials. The temporary allowlist was not committed.

No broad `internal/migration` test and no repeat of the fixed PostgreSQL matrix was run during this review.

## Verdict and authority boundary

The fixed candidate is **APPROVE, P0=0 / P1=0 / P2=0** for Slice C. This verdict approves only the reviewed code and
evidence lineage. It does not authorize or perform production database writes, HTTP/P2/provider side effects, remote
host operations, deployment, publication, release, merge, or Gate closure. Production trust provisioning remains
unimplemented. `G-SECURITY-P1` and every aggregate Gate remain **OPEN**.
