# G-CONTRACT current-source superseding repair authorization — 2026-08-26

## Authorization and decision

This append-only entry records the explicit authorization to continue the
superseding D-053 repair and its fresh-evidence process under ADR-0030. The
authorization is limited to the current P0 checkout and to read-only local
candidate/evidence construction. It does not authorize production database
writes, HTTP/OIDC/JWKS, P2/provider effects, deployment, publication, release,
force-push, history rewrite, or any Gate transition.

The pre-replay source/consumer repair is already present in the current
lineage at `5d9a2666efcdd477edda115a945de96edc11acca` and was followed by the
fresh D-053 chain through `46af0133554571d47b605872bc38c3844201875f`. No second
unversioned edit to those consumers is authorized by this entry. The repaired
source, its focused tests, and the v3 predecessor fence remain immutable
inputs for the next candidate.

## Why fresh evidence is required

The accepted D-053 C–J records remain historical evidence and are retained
without rewriting. The current branch subsequently contains the independent
append-only durable Project identifier candidate (`8e6045c735b5892a129fbe5befb6b34d9ec6c759`)
and its review child. Although that candidate does not change the D-053
generator/consumer implementation, it changes the current Git source tree.
The exact projection therefore cannot silently reuse the old projection
singleton or its Darwin/Linux receipts. A new candidate must bind the current
source tree and produce a new projection and replay tuple.

## Ordered fresh-evidence process

The following order is authorized and must remain non-Gate:

1. **Slice C — projection:** construct one fresh v3 projection from the fixed
   repaired source tree, preserving the exact ADR-0030 exclusion order and
   recording candidate/tree/archive identities.
2. **Slice D — native replay:** run only the predeclared Darwin arm64 and Linux
   amd64 A/B replay against that one projection. Preserve raw receipts and
   verify cross-platform projection/archive/output equality; do not treat a
   failed arm as a success receipt.
3. **Slice E — assembly:** after Slice D is independently admissible, perform
   the bounded late-bound profile/lock assembly and no-output currentness
   checks. Do not rewrite v1/v2 locks or any historical D-053 object.
4. **Slice F — supply review:** independently review the assembled candidate,
   including exact parent/tree/blob/mode/verdict and projection/replay
   bindings. A non-APPROVE or any P0/P1/P2 finding stops the continuation.
5. **Slices G–J — consumers and terminal binding:** only after Slice F
   approval, generate the detached consumer, obtain its independent review,
   bind the exact tuple/registry, and obtain the terminal independent review.
   Each child must be append-only and must not mutate a predecessor object.

The resulting status may only be `REVIEW_BOUND_CURRENT_SOURCE_CANDIDATE` (or a
fail-closed status). It is not a Gate closure, release, deployment, or runtime
claim. Existing D-053 receipts, reviews, locks, and the MD5 review remain
recoverable historical evidence; no previous SHA, file, or review is deleted
or relabelled.

## Boundary checks

- `contracts/generation.lock.json`, v1/v2 profiles, and `000013` migration SQL
  remain byte-identical.
- The isolated `000014` migration candidate is not installed, runner-bound, or
  applied to any database by this entry.
- No HTTP/P2/provider path, SSH host, hardware power action, deployment,
  publication, release, Gate update, or external state mutation is used.
- Expensive native replay is not part of this entry's implementation commit;
  it is the separately ordered Slice D action after the fresh Slice C review.
