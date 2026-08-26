# G-CONTRACT external-consumer replay authority v2 authorization — 2026-08-26

## Decision and scope

The owner explicitly authorizes `D-053-EC-2`, a new versioned authority for
the still-pending projection and native-replay portion of the external
consumer successor. Its authority identifier is
`cloud-agents/g-contract-external-consumer-replay/v2` and its profile
identifier is `g-contract-external-consumer/v2`.

This authorization is limited to an append-only local authority candidate,
focused contract checks, and one independent read-only review of that fixed
candidate. It does not authorize execution of the projection or native replay
in the authority commit, generation of success receipts, production database
writes, public or provider HTTP, P2/provider effects, OIDC/JWKS, SSH or
hardware actions, deployment, publication, release, signing, force-push,
history rewrite, or any Gate transition.

`D-053-EC-1` and every D-053 predecessor remain immutable. The EC-1
implementation candidate is `f8d44568b0f64b31f466dbc47e0a17b15b96e659`
(tree `50662a40d175aa18f3d5eaf6f1c60d0a58c816db`, parent
`f3a058291ba6fbae53bc8dc96c695944426b2fb4`). Its independent review child is
`9f9815e72cf108972a6fd12627cdeaad8cb71449`; that review is evidence about the
candidate and must not be reclassified as the projection input tree.

## Required frozen authority

The v2 source, strict schemas, checker, and focused tests must freeze all of
the following before any replay is admissible:

1. the exact EC-1 candidate, parent, tree, review child, review-only path, and
   immutable EC-1 source/schema/profile/evidence bytes by path, Git blob,
   SHA-256, byte size, and mode;
2. the exact ordered semantic input bindings and the full tracked projection
   rule: one fixed candidate Git tree minus one exact ordered late-bound path
   set, with untracked files, path aliases, symlinks, submodules, and special
   files rejected rather than silently omitted;
3. deterministic uncompressed Git-compatible tar construction with epoch
   timestamps, no `.git`, no duplicate entries, and these exact manifest
   algorithms:
   - `utf8-bytewise-sorted-path-type-mode-size-sha256-linktarget-nul-v1` for
     archive members;
   - `utf8-bytewise-sorted-path-mode-size-sha256-nul-v1` for regular input
     files;
4. the EC-specific projection builder, replay runner/wrapper/helper authority
   paths and exact bytes, the sanitized environment, command timeouts, and
   exact Bun, TypeScript, Go, OS, architecture, `GOWORK=off`, and
   `GOFLAGS=-mod=readonly` bindings;
5. required Darwin arm64 A/B and Linux amd64 A/B runs, Linux arm64
   `NOT_CLAIMED`, disposable `127.0.0.1` loopback-only fixture policy, denied
   external egress, one generated Connect POST per TypeScript and Go consumer,
   `application/proto` request/response, and zero non-allowlisted changes;
6. exact append-only receipt paths for projection, per-run, per-platform
   isolation, replay summary, generated profile, authority review, and final
   replay/profile review. A receipt is absent until a successful owning stage;
   failed or unavailable runs remain pending and never produce synthetic
   success bytes;
7. the projection/tree/archive/member-manifest/input-manifest tuple carried
   unchanged into every native receipt, exact A/B and cross-platform equality
   rules, artifact/checksum facts, runner/toolchain identity, and invalidation
   on any bound-input drift; and
8. candidate/review separation: the authority review must be one
   single-parent direct child that adds exactly its predeclared regular
   `100644` review file, names the fixed candidate commit/tree/parent and exact
   changed paths, and reports `APPROVE` or `REQUEST_CHANGES` with explicit
   P0/P1/P2 counts. `APPROVE` is valid only for `P0=0/P1=0/P2=0`.

The authority source must begin in
`AUTHORITY_FROZEN_REVIEW_PENDING`. An approving review may classify the fixed
candidate as `AUTHORITY_APPROVED_REPLAY_PENDING`, but no tracked v2 profile or
success receipt may be created by this authority-only slice. Later stages are:

```text
AUTHORITY_APPROVED_REPLAY_PENDING
  -> PROJECTION_CURRENT_NATIVE_REPLAY_PENDING
  -> NATIVE_REPLAY_CURRENT_PROFILE_PENDING
  -> PROFILE_CURRENT_FINAL_REVIEW_PENDING
  -> APPROVED_NON_GATE
```

Only an exact next transition may append its predeclared output. Partial,
reordered, divergent, overwritten, or self-referential states fail closed.
Any authority-byte drift requires a new versioned successor; v1 and v2 bytes
must never be repaired in place.

## Review entry and non-claims

The implementation candidate may contain only the predeclared v2 authority,
schema, checker, runner-contract, focused-test, and implementation-record
paths. It must not contain projection/native receipts, a generated replay
profile, or either review file. The independent reviewer must reproduce the
schemas, exact path sets, lineage and byte fences, manifest algorithms,
runner/toolchain/platform policy, receipt state machine, review topology, and
non-production boundaries from the fixed candidate.

`notGateClosure=true` and `gateStatus=ALL_GATES_OPEN` are mandatory. An
approving authority review permits only the separately bounded future
projection step; it does not prove a projection, native replay, same-bits
result, external-consumer criterion, Gate closure, release, or deployed
runtime.
