# P1-A2.3 durable coordination v3 independent review - 2026-08-19

- Status: **NOT APPROVE — P0=0 / P1=1 / P2=0**
- Fixed HEAD: `4c8b1654fdaeb6ec55154928d76d1e00fd1efa98`
- Reviewed snapshot: fixed HEAD plus the dirty/untracked A2.3 v3 worktree captured in section 5
- Branch: `codex/cloud-agents-platform-p1`
- Review mode: independent, read-only
- Does not authorize: an identity mapping, a wider authorization grammar, HTTP/P2 external effects, production
  database mutation, commit, push, deployment, release, or any Gate closure

## 1. Verdict

The independently reviewed quota v3/profile, generated registry, append-only PostgreSQL kernel and focused matrices
are internally consistent. The complete A2.3 candidate is nevertheless **not approved** because one generated-profile
valid organization identity cannot enter the only service/claim path.

| Slice / boundary                            | Verdict | Result                                                                                                     |
| ------------------------------------------- | ------- | ---------------------------------------------------------------------------------------------------------- |
| lineage/quota v3 and migration registry     | PASS    | v1/v2 same-bits; v3 32 segments, 512 MiB and 4 KiB; exact arithmetic and domain separation                 |
| generated operation profile and binder      | PASS    | selector, canonical request digest and typed binder are closed; service conformance is blocked below       |
| append-only PostgreSQL kernel / `000009`    | PASS    | only two existing functions are replaced; no table, `GRANT`, `REVOKE` or previous migration rewrite        |
| service/claim                               | FAIL    | generated Unicode organization identity is passed directly to an ASCII-only authorization scope validator  |
| focused normal/race and PostgreSQL matrices | PASS    | reviewed focused packages and previously recorded PG15/16/17 kernel/service matrices pass                  |
| HTTP/P2/external effects                    | PASS    | no handler, route, provider, worker, Session, Turn, Execution or external-delivery implementation is added |
| immutable/aggregate Gates                   | OPEN    | this review is not a Gate signature and closes no Gate                                                     |

## 2. P1 — generated organization identity and authorization scope disagree

The generated contract accepts an NFC Unicode organization ID with 1–256 Unicode scalar values. Its golden
fixture deliberately uses `organization-café`, and the generated binder accepts that value as a valid
`organizationRef`.

The PostgreSQL service then copies the same organization ID directly into `authz.ScopeRef` and calls
`ScopeRef.Validate`. That authorization type accepts only its ASCII opaque-identifier grammar and at most 128
bytes. The current service test consequently treats the same golden-profile-valid `organization-café` request as
invalid and asserts that no database call occurs.

This is fail-closed, not an authorization widening. It is still a P1 contract break: a request valid under the only
approved generated profile cannot pass the only service/claim entry. The candidate contains no separately approved
Unicode-to-ASCII identity mapping authority.

Relevant evidence:

- `contracts/common/v1alpha1/schemas/organization-ref.schema.json` defines the generated organization identifier;
- `contracts/platform/v1alpha1/fixtures/golden/managed-agent-create-project-idempotency.json` fixes
  `organization-café` and its canonical digest;
- `services/control-plane/internal/coordination/managed_agent_create_project.go` validates and binds the generated
  request;
- `services/control-plane/internal/store/postgres/durable_coordination.go` constructs and validates the
  organization `ScopeRef`;
- `services/control-plane/internal/authz/rbac.go` defines the narrower ASCII scope grammar;
- `services/control-plane/internal/store/postgres/durable_coordination_test.go` proves the generated-valid request
  is rejected before database access.

## 3. Impact inventory and prohibited shortcut

This is a cross-layer identity contract break, not an isolated service-validator defect. The public
`NamespaceRef` authority in ADR-0007 makes the normalized, case-sensitive NFC `id` the value consumed by
authorization, persistence and canonicalization. The current PostgreSQL lineage instead applies the historical
ASCII `cloud_agents.is_valid_identifier` grammar to all of the following identity-bearing surfaces:

- organization and project primary/reference columns in `000002`, including their resource-change foreign-key
  edges;
- organization/project membership and role-binding scope columns and their mutation functions in `000003`–`000005`;
- the durable coordination resource-id checks in `000008` and the generated Go service's `ScopeRef` validation;
- the audit/resource UID checks that are joined to those organization/project references.

The tenant, internal holder, event, reason-code and other operational identifiers may remain ASCII if the approved
rule says so; that distinction must be explicit and level-specific. It is not safe to change the shared validator
globally, to cast a Unicode public id through an ASCII helper, or to accept one grammar in the contract and another
grammar in the database. A silent transliteration, truncation, caller-selected alias, percent encoding, or
unversioned/ad-hoc hash would create a second identity authority and would invalidate the existing NamespaceRef
canonical digest, RBAC scope, foreign-key and audit provenance bindings.

The current candidate has no generated mapping authority, mapping table, collision proof, or versioned database
identity contract. Consequently the review can record the affected surfaces and preserve fail-closed behavior, but
it cannot mark the service/claim slice complete or select an implementation route on the owner's behalf.

## 4. Required authority decision

Closing this P1 requires an explicit, versioned identity decision that makes generated contract identity and
authorization scope identity the same authority. This review does not choose among narrowing the generated
organization identifier, versioning a generated canonical identity mapping, or widening the authorization scope
contract.

Silent ASCII conversion, lossy normalization, truncation, caller-selected aliases and ad hoc hashing are forbidden.
No service/claim completion may be claimed until the selected rule is generated, profile-bound, fault-tested and
independently reviewed.

## 5. Reviewed evidence and limitations

The read-only review reconfirmed focused normal and race tests for coordination, PostgreSQL store, evidencefs and
migration packages, plus `git diff --check`. The reviewed artifact identities were:

- `000009` SHA-256: `cb87e81eb40b67763c4bd3bc02f9fea2b0db4ce01ff24f2f75ebe48cc34823f0`;
- manifest SHA-256: `d670f3850d7f97610575b1d0e525246de79f909daedcdc192aa348bdebdcb362`;
- schema-bundle SHA-256: `38200fb6f254c6eb5d43a1523c9b2d7c57fddeaabdfd82c9eba70f5c5d1855a5`;
- generation-lock SHA-256: `9b5db46f51e3c55515c1d56cb326e75760636b5a8e6b2fbe9194b62a03b3fcd6`.

The full `internal/migration` suite still has only the recorded bounded 10-minute attempt. It produced no assertion
failure before timeout, but that attempt is **not a pass**. The candidate is uncommitted and unpushed; no production
database write, deployment or external side effect was performed. Every immutable and aggregate Gate remains open.

## 6. Post-review local remediation — not independently reviewed

After this review snapshot, the uncommitted candidate was changed to remove the cross-layer identity mismatch
without introducing a conversion or widening the shared authorization grammar:

- the managed-agent create-project operation now selects a generated operation-specific organization identity
  schema limited to the authorization scope grammar, ASCII only and at most 128 bytes;
- the generated profile binds `cloud-agents-authorization-scope-identifier/ascii-v1` with
  `exact_string_no_rewrite`; the common Unicode organization-reference schema remains byte-unchanged;
- the golden request now uses `organization-cafe`, and the typed binder and PostgreSQL service both revalidate the
  exact generated profile before deriving the scope and request digest;
- append-only `000009` now preserves the frozen historical registry/profile pair and adds a versioned service entry
  for the current generated pair; `000007` and `000008` remain byte-identical;
- local contract/bundle/generation-lock checks, focused normal/race tests and the PostgreSQL 15/16/17
  normal/race/fault service matrix pass for this remediated candidate.

These later facts do not rewrite this review's `NOT APPROVE` verdict. The remediated candidate still requires an
independent rereview and the remaining migration-suite closure. It remains uncommitted and unpushed, introduces no
HTTP/P2 external effect or production mutation, and closes no Gate.
