# P1-A2.4 compatibility/recovery service entry blocker - 2026-08-20

- Status: **OWNER APPROVAL REQUIRED - PROPOSED ONLY**
- Fixed evidence ref: `48c93f9148986e031a98d6677830c8a084f0343b`
- Branch: `codex/cloud-agents-platform-p1`
- Scope: the next A2.4 versioned-registry repair, typed PostgreSQL writer kernel, and service/claim review entry
- This record does not authorize implementation, a new migration, a Go consumer, production database writes, HTTP/P2/provider effects, deployment, release, or Gate closure

## 1. Current boundary

The A2.4 v1 registry and `000010` schema-only kernel are historical, source-bound evidence. The v1 generated
boundary is explicit: its SQL migration and Go consumer are not implemented, external side effects are forbidden,
and the artifact is non-Gate evidence. Its registry digest is
`sha256:9df9dcf4c9e62cd95b43be362bf5a332bf9637ca881f16fbd25486ad0792f72d`; its five profiles are
`backfill/v1`, `live-instance/v1`, `migration-preflight/v1`, `restore-evidence/v1`, and
`retirement-receipt/v1`.

The already-applied `000010_expand_compatibility_recovery_kernel.sql` remains an append-only, schema-only
forward migration. It adds owner-controlled tables for workload-database principals, migration backfills, schema
restore evidence, live instances, and instance-retirement receipts, plus pure digest/profile helpers. It creates no
mutation writer. Its catalog writer names are declarations for future slices, not authority. The v1 source,
generated output, registry digest, `000010`, and their manifests must remain byte-exact historical inputs.

This is a version boundary, not a defect in the historical v1 entry: a v1 artifact that says “no `000010` consumer”
cannot later be edited to authorize one, and an applied migration cannot be rewritten in place. A new generated
registry/profile version and a later forward migration are required before typed service authority exists.

## 2. Frozen constraints for the next entry

1. Preserve v1 source/output/digests, ADR-0015, ADR-0016, `000010`, and every predecessor migration exactly. Any
   repair is a new version and a new forward migration; no checksum or catalog-history rewrite is permitted.
2. Select profiles only from generated registry evidence. A caller, row, schema head, migration name, or guessed
   version cannot select or elevate a profile. The selected registry/profile/schema digests are checked together.
3. Use exact operation/profile identities. No silent normalization, Unicode-to-ASCII rewrite, aliasing, or lossy
   mapping may bridge a generated contract to an authorization identity; the A2.3 operation-specific identity
   profile is the precedent.
4. Keep raw tables non-authoritative and non-writable by runtime/bootstrap/PUBLIC. Only named, typed functions may
   mutate them. Each function must use an exact owner, fixed `search_path`, explicit `SECURITY DEFINER` policy where
   needed, `REVOKE` before grants, and narrowly scoped `EXECUTE`; no generic table grant is acceptable.
5. Use database time and one closed state machine per transition. Apply monotonic epoch/generation/incarnation
   checks, composite tenant/instance keys, bounded values, and atomic UPSERT/update predicates. A response-lost or
   post-statement unknown result is `unknown`, followed by deterministic read/reconcile; it must not be retried as a
   second transition.
6. Keep transactions short and deterministic: establish the documented lock order, perform no network/provider/
   worker/session/turn/execution call while holding a database lock, and make retry/reconciliation idempotent.
7. The scope remains generated registry/profile evidence, append-only PostgreSQL kernel, typed service/claim and
   normal/race/fault matrix review only. HTTP mutation, P2, provider effects, production writes, deployment,
   release, and every immutable or aggregate Gate remain forbidden/open.

## 3. Proposed approval package

Approval of this section would authorize only the following ordered work. It would not authorize implementation
before each preceding slice is reviewed.

### Slice A - versioned generated registry/profile repair

Add a new source and generated registry version (v2 or the next explicitly assigned version), without changing v1.
The new profile set must bind the exact `000010` schema head/catalog digest and name each typed writer/service
contract it authorizes. It must define the profile selector, state-machine transition tables, capability/evidence
requirements, unknown/reconcile rules, and historical compatibility with v1. Generation must produce deterministic
source/output/manifest/lock evidence; only the generated artifact may be consumed.

The v2 registry must explicitly distinguish read-only migration preflight from mutations, empty-registry bootstrap
from existing-ledger operation, live-instance registration/heartbeat/drain/retirement, restore-evidence recording,
principal registration/revocation, and bounded backfill transitions. It must preserve exact profile and identity
validation rather than infer authority from stored rows.

### Slice B - append-only writer kernel

Add a later forward migration (the next available migration after `000010`; the number is not reserved by this
proposal) and immutable generated catalog/manifest evidence. It may add only typed, auditable mutation functions
for the v2 profiles, such as:

- audited principal bootstrap, rotation, and revocation;
- bounded backfill start, lease/heartbeat, cursor advance, completion, and reconcile;
- restore-evidence record and monotonic completion/rejection;
- live-instance register, activate, heartbeat, drain, and fence;
- retirement receipt collect, complete, reject, and reconcile.

Preflight remains read-only and fail-closed. The functions must bind the generated registry/profile/schema digest,
tenant and identity tuple, epoch/generation/incarnation, and transition-specific evidence in the same transaction.
Unknown outcomes must return a closed result that directs reconciliation; they must not expose a second “retry this
write” path. The migration must preserve least privilege, fixed `search_path`, explicit owner/ACL evidence, and
zero runtime/bootstrap/PUBLIC table privileges.

### Slice C - typed service/claim, matrix, and independent review

Implement only a typed Go port and PostgreSQL consumer for the approved generated profiles. The service must bind
the exact registry/profile/schema digests, never accept a caller-selected profile, and expose no HTTP route. Claim
and reconcile paths must fence stale epoch/incarnation claims, redact secrets, and preserve unknown/duplicate/conflict
semantics.

The review matrix must cover PostgreSQL 15/16/17, normal and race execution, stale/cross-profile/cross-tenant
identity, duplicate and response-lost transitions, lock/lease expiry, retry/DLQ or retirement reconciliation, ACL
denials, and fault injection at each durable barrier. The independent reviewer must review the generated binding,
writer ACLs, state-machine transitions, service claims, and forbidden-surface scans before any slice is called
complete.

## 4. Acceptance and explicit non-claims

The proposal is ready to leave “proposed” only after owner approval and a fixed implementation plan records:

- v1 same-bits replay plus deterministic v2 source/output/manifest/lock digests;
- append-only migration/catalog evidence with no edits to `000010` or earlier history;
- typed-function ACL proof, lock-order/short-transaction proof, and closed unknown/reconcile results;
- service/claim normal, race, fault, and PostgreSQL 15/16/17 matrix evidence;
- no HTTP/P2/provider/worker/session/turn/execution paths and no production database mutation;
- an independent review record. All immutable and aggregate Gates remain OPEN.

Until then, this file is a decision-entry proposal only. It does not claim that v2, a writer migration, a Go
consumer, a service, a production authority, or any Gate exists.

## 5. Approval request

Please approve or reject **Section 3 exactly as an A2.4 ordered three-slice entry**. Approval would authorize the
ADR and the generated-registry slice first, followed only after its review by the append-only writer kernel and then
the typed service/claim/matrix/independent-review slice. It would not authorize a public surface, external side
effect, production write, deployment, release, P2, or Gate closure.
