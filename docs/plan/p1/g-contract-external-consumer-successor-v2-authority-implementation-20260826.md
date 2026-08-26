# D-053-EC-2 authority implementation record

Date: 2026-08-26

This record is the authority-base implementation for the versioned external
consumer replay successor. It freezes the contract and the future evidence
interfaces; it does not execute projection or native replay.

## Append-only topology

The implementation is intentionally split into two commits:

1. the authority base adds the schemas, read-only checker/runner, focused test,
   and this record as one regular-file set;
2. the authority candidate is a single-parent child that adds only
   `tools/g-contract-external-consumer/v2/source.json`.

The source binds the authority-base commit/tree and every base file by Git
blob, SHA-256, byte size, and mode. Its own bytes are bound by the candidate
commit/tree and the independent review child, avoiding a self-referential
digest. The review child adds exactly one predeclared regular `100644` review
file.

## Frozen contract

- Decision/profile: `D-053-EC-2` / `g-contract-external-consumer/v2`.
- Initial state: `AUTHORITY_FROZEN_REVIEW_PENDING`; an approving authority
  review permits only the next versioned replay successor.
- Semantic inputs are the exact ordered 18-file EC-1 consumer inventory. Each
  record carries Git blob, SHA-256, byte size, and `100644` mode and is bound
  to the immutable EC-1 candidate. The regular-file manifest algorithm is
  `utf8-bytewise-sorted-path-mode-size-sha256-nul-v1`.
- Projection is the full tracked candidate Git tree minus the exact late-bound
  path set. There are no globs. Untracked files, aliases, symlinks, submodules,
  special files, duplicate paths, `.git`, `node_modules`, `.idea`, and
  `migration.test` are rejected in the projection tree.
- The archive is uncompressed `ustar`; paths are UTF-8-bytewise sorted;
  timestamps are epoch zero; uid/gid are zero; uname/gname are empty; PAX and
  duplicate entries are forbidden. The member-manifest algorithm is
  `utf8-bytewise-sorted-path-type-mode-size-sha256-linktarget-nul-v1` with
  explicit NUL-framed fields.
- The only runnable command in this slice is
  `bun scripts/generate-platform-g-contract-external-consumer-v2.ts
--check-source`. It is read-only, timeout-bounded, network-denied, and
  cannot write a profile, receipt, archive, or review. Bun 1.4.0, TypeScript
  5.7.3, Go `go version go1.27.0 darwin/arm64`, `GOWORK=off`, and
  `GOFLAGS=-mod=readonly` are frozen.
- Darwin arm64 and Linux amd64 each require A/B plus isolation receipts;
  Linux arm64 is `NOT_CLAIMED`. The future fixture is disposable,
  `127.0.0.1` loopback-only Connect `POST`, `application/proto` both ways,
  one call per TypeScript and Go consumer, with external egress denied.
- Projection, per-run, per-platform isolation, replay summary, generated
  profile, authority-review, and final-review paths are exact and
  `CREATE_ONCE_APPEND_ONLY`. All are `ABSENT_PENDING` here; synthetic success
  receipts are forbidden.

## Non-claims

No projection/archive, native replay, profile, success receipt, HTTP/P2/provider
effect, production database write, OIDC/JWKS or SSH/hardware action,
deployment, publication, release/signing, history rewrite, or Gate transition
was performed or authorized. `notGateClosure=true` and
`gateStatus=ALL_GATES_OPEN` remain mandatory. Any bound-byte drift requires a
new versioned successor; v1 and this v2 authority are never repaired in place.
