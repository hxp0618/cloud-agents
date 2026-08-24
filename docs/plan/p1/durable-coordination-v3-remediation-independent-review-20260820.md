# P1-A2.3 durable coordination v3 remediation independent review - 2026-08-20

- Status: **APPROVE bounded implementation/review slice — P0=0 / P1=0 / P2=0**
- Fixed base HEAD: `4c8b1654fdaeb6ec55154928d76d1e00fd1efa98`
- Reviewed source: fixed base plus the dirty/untracked A2.3 remediation candidate recorded below
- Branch: `codex/cloud-agents-platform-p1`
- Review mode: independent, read-only
- Scope: generated registry/profile, append-only PostgreSQL kernel, service/claim wiring and local matrices
- Does not authorize: HTTP/P2 external effects, production database mutation, deployment, release, commit/push by
  this review, or any immutable/aggregate Gate closure

## 1. Verdict

The remediation candidate closes the sole P1 from the 2026-08-19 review. The approved boundary is the A2.3
implementation/review slice only; it is not a claim that the full migration suite, production provisioning or any
Gate is closed.

| Slice / boundary                                       | Verdict | Evidence                                                                                                                                                                                                                                                   |
| ------------------------------------------------------ | ------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| versioned lineage/quota profile and generated registry | PASS    | v1/v2 historical same-bits; v3 is the generated manifest selector with 32 segments, 512 MiB and 4 KiB checkpoint ceiling; exact arithmetic, `+1` faults and domain-separated digests pass                                                                  |
| operation-specific organization identity               | PASS    | generated profile uses ASCII opaque identifier grammar, at most 128 bytes and `exact_string_no_rewrite`; binder, golden, service and `authz.ScopeRef` validate the same value without conversion                                                           |
| append-only PostgreSQL kernel / `000009`               | PASS    | 30 classified statements retain the historical registry/profile pair, add the versioned current pair, replace only allowlisted helpers/claims/constraints and revoke/grant only the specified runtime surface; `000007` and `000008` remain byte-identical |
| service/claim                                          | PASS    | typed binder and pre-DB validation reject profile, digest, identity, authority and redaction faults; valid generated requests reach the intended claim path                                                                                                |
| PostgreSQL matrix                                      | PASS    | isolated PostgreSQL 15/16/17 normal, race and direct-fault service/kernel legs pass without production database mutation                                                                                                                                   |
| HTTP/P2/external effects                               | PASS    | no HTTP handler/route, provider, worker, Session, Turn, Execution or external-delivery implementation is added                                                                                                                                             |
| immutable and aggregate Gates                          | OPEN    | this review is not a Gate signature and closes no Gate                                                                                                                                                                                                     |

## 2. Closed identity boundary

The historical finding was a contract break: the generated managed-agent create-project profile accepted NFC
Unicode organization identifiers while the service copied the same value into the narrower ASCII authorization
`ScopeRef`. The remediation does not transliterate, truncate, hash, alias or widen the shared authorization grammar.

Instead, the operation-specific generated profile references a dedicated organization-ref schema whose identifier is
the existing ASCII opaque identifier grammar and whose maximum is 128 bytes. The profile binds the authority
`cloud-agents-authorization-scope-identifier/ascii-v1` and the canonical rule `exact_string_no_rewrite`. The typed
binder, request digest, service scope construction and database preflight all revalidate that exact generated
profile. The public common Unicode organization-reference schema remains unchanged and is not silently narrowed.

The golden request therefore uses `organization-cafe`, while the operation-specific negative fixtures cover NFC
Unicode and over-length identifiers. A generated-profile-valid request and the derived authorization scope now
have one byte-exact identity authority.

## 3. Append-only `000009` kernel

The current migration is a 30-statement, generated and classified compatibility migration. It preserves the frozen
historical registry/profile pair, adds a versioned current service pair, updates only the allowlisted registry
helpers/profile bindings/claim functions and four digest constraints, and tightens the intended ACL. The static
bundle validator and PostgreSQL matrix verify the statement surface, historical `000007`/`000008` same-bits,
function coexistence, runtime/helper callability, redaction and absence of raw DML/table/privilege side effects.

The historical independent-review note that described an earlier two-function candidate is retained as history; this
record is the current review of the 30-statement candidate.

## 4. Evidence and limitations

Passed local evidence:

- generated contract registry/profile/schema/fixture checks and deterministic migration bundle/lock checks;
- focused TypeScript tests (27 registry/lock/bundle cases);
- focused Go normal and race tests for coordination, PostgreSQL store, evidencefs and migration;
- isolated PostgreSQL 15/16/17 service/kernel normal, race and direct-fault matrices;
- control-plane compile/vet/build and Linux amd64/arm64 compile checks;
- `git diff --check` and scope checks showing no HTTP/P2/external implementation.

Reviewed artifact SHA-256 bindings:

- `000009_redact_coordination_conflicts.sql`:
  `b4c544a1a1a0236fa485f55bcfce8e03d759b977290f7c22b5f598e4e657cc90`;
- generated migration manifest:
  `4437984bca7f456dff754a39c6dbce2ea6d1919402821d31cc0c5eef5de0df2f`;
- generated schema bundle:
  `902b1002b9423628ee06fadbc255106d500dc9cb58f8b929f5dbdc53a03e3ae1`;
- generated durable-coordination registry:
  `2dbce40aac86d2d1709c4aa07d683d916fbba3a992e7f8699843e470c0876dc6`;
- managed-agent contract binder:
  `ee1e49eded5761818380bc5323196cdd507edc6d0c11546f867ce229cfd0ae5f`;
- PostgreSQL service binding:
  `2126be99c3a1b46c8a82f6ed38026261d9d1d0b3e06d832052b5a504f66a839c`;
- PostgreSQL matrix script:
  `7cb524794c5ebb5723f43d8e7e31355309aecfe412a09d409a03b8141a0999a6`;
- regenerated contract lock after the ADR/status update:
  `d9979e08fe1f9853eafb4fae6b7a98ec68b54d3151678ef950020fbd97248647`.

The bounded full `internal/migration` run did not finish within the local 10-minute limit. It emitted no assertion
failure before timing out in `TestRunnerPreparedBinderRevalidatesEveryClosedInput/plans`, but it is **NOT PASS** and
is not used as closure evidence. Production database writes, deployment, release and external effects were not
performed. The candidate was uncommitted and unpushed when this review was recorded.

## 5. Boundary after approval

This record approves the generated contract registry -> append-only PostgreSQL kernel -> service/claim/matrix
implementation/review slice requested by the owner. It does not approve an HTTP/P2 surface, a runtime/provider
integration, production provisioning, a full migration admission claim, or any Gate closure. All immutable and
aggregate Gates remain OPEN pending their own evidence and independent signatures.
