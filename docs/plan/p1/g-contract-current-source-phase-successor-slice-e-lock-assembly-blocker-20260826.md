# G-CONTRACT current-source phase successor Slice E lock assembly blocker

Date: 2026-08-26 Asia/Shanghai

## Result

`BLOCKED - deterministic pre-write contract mismatch (P1=1, P0=0, P2=0)`

This record covers one bounded, local Slice E preflight attempt from the
unchanged fixed replay candidate
`e45ef4e3c5014bec97c7cbe73661559c3d6eced2`. It did not modify the candidate,
the live generation lock, any receipt, a remote host, a database, or a Gate.
No temporary lock or transition file remained after the failed command.

## Fixed input and commands

The attempt used a clean branch rooted directly at `e45ef4e` (without the
later review/tracker child commits):

```text
bun scripts/generate-platform-generator-supply-profile-v3.ts --check-assembly
platform-generator-supply-profile: v3 ASSEMBLED_PROFILE_CURRENT

bun scripts/generate-platform-contract-lock-v3.ts --check
platform-contract-lock-v3: PRE_REPLAY_LEGACY_LOCK_ONLY

bun scripts/generate-platform-contract-lock-v3.ts --write-assembled
```

The writer failed before its exclusive lock transition:

```text
error: v3 profile does not expose the exact 49-output assembled authority.
at buildAuthority (scripts/generate-platform-contract-lock-v3.ts:279)
```

The working tree remained clean and `contracts/generation.lock.json` stayed
byte-identical to the fixed post-H v2 predecessor
(`sha256:de62a85390e58736a5fef8272878b821415b4180948437e54edb8628e005ff53`).

## Deepest verified cause

`buildAuthority` in
`scripts/generate-platform-contract-lock-v3.ts` reads only
`tools/generator-supply/v3/profile.json` and requires recursive
`candidateManifestSha256` and `outputFiles` values. The canonical v3 profile
schema and generated profile intentionally expose the profile registry,
evidence manifest, and receipt records; they do not expose those two replay
output summary fields. The values exist in the raw replay reports and the
derived `tools/generator-supply/v3/evidence/replay.json`, but the lock writer
does not read that summary before its authority-shape check. Consequently the
writer cannot construct the otherwise valid 49-output assembled authority.

This is a deterministic source/consumer contract mismatch, not an environment
or permission failure. The existing focused lock unit tests use an in-memory
authority object with those fields and therefore do not exercise the CLI's
current checked-in profile shape.

## Boundary and required repair

No lock write, `ASSEMBLED` state, no-output current check, or formal Slice E
candidate exists. The v3 lock remains `PRE_REPLAY_LEGACY_LOCK_ONLY`; Slice F's
predeclared supply review cannot start from this state.

The repair must be a new versioned/pre-replay source-consumer correction (for
example, a single canonical replay-summary binding used by both the profile
and lock writer) with focused writer tests. Because the writer and its tests
are projection inputs under ADR-0030, a repair candidate must receive a fresh
projection and fresh Darwin/Linux A/B replay before any assembled lock is
materialized. The current replay receipts are retained as historical evidence
and must not be relabeled as proof of the repaired source.

Production database writes, HTTP/OIDC/JWKS, P2/provider effects, deployment,
publication, release, force-push, history rewrite, and Gate transitions remain
unauthorized.
