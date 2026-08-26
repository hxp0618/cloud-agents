# G-CONTRACT external-consumer successor authorization — 2026-08-26

## Authorization

This append-only record is the explicit authorization to supersede the
consumer portion of D-053 with a new, versioned current-criteria successor.
It supersedes only the earlier authorization's prohibition on a second
unversioned consumer edit. It does not supersede, rewrite, or relabel any
D-053 v1/v2/v3 source, R5 record, review, tuple, registry, generation lock,
projection, replay receipt, or other predecessor byte.

The successor identifier is `D-053-EC-1` and its authority is
`cloud-agents/g-contract-external-consumer/v1`. The successor is an evidence
profile, not a contract-discovery authority, release artifact, Gate signature,
or production runtime feature.

## Authorized scope

The following bounded work is authorized in the P0 checkout:

1. repair the versioned external-consumer harness so TypeScript and Go use
   exact package/module versions, SHA-256/SRI or Go checksum bindings, and
   temporary loopback-only artifact/module fixtures;
2. perform one real generated Connect call from each fresh consumer to its
   loopback fixture, recording method, path, content type, and observed call
   count;
3. add a versioned source/schema/generated profile and focused checks under
   `tools/g-contract-external-consumer/v1/`, plus append-only evidence and an
   independent review record;
4. after all pre-replay bytes are fixed, run the separately bounded fresh
   projection/native-replay/evidence process required by the successor
   authority, with fail-closed review and lineage checks.

The profile may bind the D-053 terminal candidate as an immutable predecessor,
but it must never claim that D-053's terminal state has been upgraded. Any
unavailable native supply or platform arm remains explicitly pending or
unclaimed; no receipt may be synthesized from a failed run.

## Evidence and ordering

The successor must use this order: repair and focused checks; fresh consumer
evidence; freeze source, schema, harness, and input bindings; fresh projection;
fresh predeclared replay (Darwin arm64 and Linux amd64 where the authorized
fixture is available); generated profile; independent review; and a detached
successor review/registry if required by the source state machine. Every
candidate/review child is append-only, single-parent, and changes only its
predeclared paths. Existing D-053 evidence remains historical and recoverable.

The exact-pinned consumer criterion is satisfied only when both consumers:

- install from an HTTP loopback fixture rather than `file:`, `file://`,
  `workspace:`, Git, or a local path;
- bind the exact TypeScript tarball SHA-256 and SRI, and the exact Go module
  zip/go.mod SHA-256 plus `go.sum` checksums;
- compile and make one real Connect `POST` call to a loopback fixture, with
  `application/proto` request/response content types and an observed count of
  exactly one call; and
- pass with no workspace, replacement, vendor, Git, or cross-repository
  dependency escape.

Ephemeral ports, temporary directories, and raw command logs are not stable
authority bytes. The generated evidence stores canonical loopback patterns and
artifact/checksum facts only; a fresh run is required whenever a bound input,
toolchain, artifact, or fixture contract changes.

The `v1` evidence and generated profile are immutable once frozen. An existing
evidence or profile byte must never be overwritten when a bound input,
toolchain, artifact, or fixture changes; that change requires a new versioned
successor path (for example `D-053-EC-2`) and a new authorization/review chain.
The current `D-053-EC-1` process status is
`CONSUMER_EVIDENCE_CURRENT_REPLAY_PENDING`: the fresh projection and native
Darwin/Linux replay remain pending, and no synthetic receipt is admissible.

## Explicit boundary

This authorization remains non-Gate and non-production:

- `notGateClosure=true` and `gateStatus=ALL_GATES_OPEN` remain mandatory;
- no production database write, migration installation, HTTP/P2/provider
  effect, OIDC/JWKS or public endpoint, SSH operation, hardware power action,
  deployment, publication, release, signing, force-push, history rewrite, or
  Gate transition is authorized;
- loopback HTTP is allowed only inside the disposable consumer fixture and is
  not an external service or provider integration;
- old D-053, v1/v2 predecessor, `000001`–`000013`, and unrelated dirty state
  remain untouched.

Any request outside this scope requires a new explicit authority. A failed
schema, digest, topology, lineage, or independent-review check stops the
successor process rather than weakening the criterion or changing Gate state.
