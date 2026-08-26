# D-053-EC-2 authority implementation record

Revision: `D-053-EC-2.r2` (2026-08-27)

This record freezes a versioned generated-registry authority and its future
evidence interfaces. It is an authority-only slice: it does not execute a
projection, archive, native replay, profile generation, receipt writer, or
network call.

## Append-only lineage

The original v2 source candidate remains immutable:

- authority base `8ffc2c86df6d0d6a02677bec0790b30de233a71a`, tree
  `29520d4c93e547c18c1e6b01641d0b3c90c18c72`;
- source candidate `74f5ad620f5061adde2da14adce5b2032d4399bb`, tree
  `322332a93e712dc400e6e2bc4616c3430dce8c4c`, parent `8ffc2c86df6d0d6a02677bec0790b30de233a71a`.

Because that candidate exposed byte-format, topology-check, and path-type
defects, r1 and r2 are append-only repair chains, never in-place repairs:

1. r1 has a repair authority base as a direct child of the original source
   candidate, then an r1 source candidate that changes only
   `tools/g-contract-external-consumer/v2/source.json` with Git status `M`;
2. r2 has a second repair authority base as a direct child of the r1 source
   candidate and changes exactly the same five regular files (checker, source
   schema, review schema, focused test, and this record), then an r2 source
   candidate that changes only `source.json` with status `M`;
3. the independent authority review is a direct child of the r2 source
   candidate and adds only the predeclared `100644` review document.

The source records `authorityRevision: D-053-EC-2.r2` and the exact superseded
r1 candidate tuple. The checker verifies the complete C→r1→r2 tuple chain,
their trees/parents, and each exact repair diff. No amend, rebase, squash,
merge, force-push, or history rewrite is part of this authority.

## Frozen source and schemas

The source is `tools/g-contract-external-consumer/v2/source.json`, validated by
the bound strict Draft 2020-12 schema
`tools/g-contract-external-consumer/v2/source.schema.json`. Every authority
file is frozen by repository-relative path, Git blob SHA-1, SHA-256, byte size,
and `100644` mode at the repair authority-base commit. Source bytes are bound
to the source candidate Git blob; source formatting is valid UTF-8 JSON with
exactly one LF terminator (whitespace is not silently canonicalized).

The authority-file order is fixed and complete:

1. authorization record;
2. source schema;
3. profile schema;
4. projection-receipt schema;
5. native-replay-receipt schema;
6. replay-summary-receipt schema;
7. review schema;
8. read-only checker;
9. read-only runner;
10. focused test;
11. this implementation record.

## Complete semantic input set

The input declaration is exactly the ordered 18-file EC-1 consumer inventory:

`package.json`, the two generated proto binpb files, generated proto
`manifest.json`, `contracts/proto-generation.profile.json`, the gates and
acceptance document, the TypeScript generator/library/test, the two v1 source
and profile schemas, TypeScript package/generated manifests, and Go `go.mod`,
`go.sum`, and generated manifests. Each item is bound to the immutable EC-1
candidate `f8d44568b0f64b31f466dbc47e0a17b15b96e659` with Git blob, SHA-256,
size, and mode. The regular-file manifest is
`utf8-bytewise-sorted-path-mode-size-sha256-nul-v1`, ordered by UTF-8 bytes,
NUL-framed, with duplicate paths forbidden and declaration order frozen.

## Exact exclusions and archive/member manifests

Projection selection is the complete tracked regular-file candidate tree minus
the following exact paths (no glob or prefix matching):

- `tools/g-contract-external-consumer/v2/evidence/replay/projection.tar`;
- `projection.json` and `projection.member-manifest.json` in that directory;
- Darwin arm64 A/B/isolation receipts;
- Linux amd64 A/B/isolation receipts;
- `replay.json` and `tools/g-contract-external-consumer/v2/profile.json`;
- the authority review and final replay review documents.

The reject-if-present set is `.git`, `node_modules`, `.idea`,
`migration.test`, untracked, special, symlink, and submodule entries. Duplicate
paths, symlinks, submodules, and special modes are fail-closed.

The archive is uncompressed `ustar`, sorted by UTF-8 path bytes, with epoch-zero
mtime, uid/gid zero, empty uname/gname, no PAX headers, and no duplicate
entries. The member manifest algorithm is
`utf8-bytewise-sorted-path-type-mode-size-sha256-linktarget-nul-v1`; regular
files additionally use
`utf8-bytewise-sorted-path-mode-size-sha256-nul-v1`. Records are NUL-framed
with fields `path,type,mode,sizeBytes,sha256,linkTarget` (regular-file
manifest fields omit `type` and `linkTarget`). Archive and both manifest
outputs are late-bound and absent in this slice.

## Runner, toolchain, platform, and receipts

The only executable entrypoint is
`bun scripts/generate-platform-g-contract-external-consumer-v2.ts --check-source`.
It is timeout-bounded at 1800 seconds, `AUTHORITY_CHECK_ONLY`, network-denied,
side-effect-free, and has no replay/profile/receipt writer. The frozen runtime
is Bun 1.4.0, TypeScript 5.7.3, Go `go version go1.27.0 darwin/arm64`,
`GOWORK=off`, `GOFLAGS=-mod=readonly`, and the declared hermetic Git
environment.

Darwin arm64 and Linux amd64 each require A/B plus an isolation receipt;
Linux arm64 is `NOT_CLAIMED`. The disposable future fixture is Connect
`POST` to `http://127.0.0.1:<ephemeral-port>/` with
`application/proto` request/response, one call per TypeScript and Go consumer,
and external egress denied.

The exact `CREATE_ONCE_APPEND_ONLY` receipt paths are projection, Darwin A/B
and isolation, Linux A/B and isolation, replay summary, generated profile,
authority review, and final replay review. All eleven states are
`ABSENT_PENDING`; synthetic receipts are forbidden. No profile, archive, or
receipt is created by the authority runner.

## State, review, and boundaries

Initial state is `AUTHORITY_FROZEN_REVIEW_PENDING`; every byte drift requires a
new versioned successor. The ordered transitions are authority approval,
projection current, native replay current, profile current, and final review
approval. Partial, reordered, divergent, overwritten, and self-referential
transitions are invalid.

The independent review record is a single fenced JSON object validated by the
bound review schema. `AUTHORITY` reviews bind the source/schema authority
bytes and the authority-review path; final replay reviews bind only the future
profile/receipt set and their final-review path. The checker validates the
review child as a single-parent child of the candidate that adds exactly the
declared regular file, then validates the record. `APPROVE` is valid only when
P0, P1, and P2 are all zero; otherwise the verdict is `REQUEST_CHANGES`.
`notGateClosure=true`, `gateEffect=NO_GATE_CLOSURE`, and
`gateStatus=ALL_GATES_OPEN` are mandatory.

No projection/archive, native replay, profile, success receipt, production
database write, HTTP/P2/provider or OIDC/JWKS effect, SSH/hardware action,
deployment, publication, release/signing, or Gate transition was performed or
authorized.
