# Durable Project-create identifier hardening successor entry

Date: 2026-08-26 Asia/Shanghai

## Decision boundary

`AUTHORIZED BOUNDED SUCCESSOR IMPLEMENTATION — GATES OPEN`.

The current-criteria audit after D-053 Slice J found the still-open P2-001
namespace finding in the durable Project-create review.  Under the continuing
Platform goal, this entry fixes the smallest follow-up implementation boundary:
an append-only migration that changes the identifier derivation for *new*
durable Project-create calls from MD5 to domain-separated SHA-256.

This is a local, non-Gate repair.  It does not authorize production database
writes, HTTP, P2, provider, deployment, publication, release, SSH, or any
Gate transition.

## Immutable predecessors

The following bytes remain historical and must not be edited or regenerated:

- `services/control-plane/migrations/000013_add_durable_project_create_writer.sql`;
- the v2 durable Project-create profile, registry, generated Go profile, and
  `durableProjectCreateMigrationClosure` assertion for `000013`;
- all migrations `000001`–`000012`, their catalogs, archives, and recorded
  lineage/review documents;
- the D-053 G–J candidate, reviews, projection, profile, replay receipts, and
  phase-bound lock already fixed before this successor.

Adding the successor changes the current repository source projection.  The
existing D-053 evidence therefore remains recoverable historical evidence and
must not be re-described as current after this change.  No D-053 supply or
replay successor is part of this milestone; a future Gate/freeze window may
refresh it under its own approved process.

## Exact successor semantics

Add only migration `000014_harden_durable_project_create_identifiers.sql`.
It replaces the existing function body with the same signature and return
contract, preserving owner/ACLs and every transaction/replay path.  The only
semantic change is the identifier suffix:

- operation domain:
  `cloud-agents/durable-project-create/operation-id/v1`;
- event domain: `cloud-agents/durable-project-create/event-id/v1`;
- each input frame is domain-separated, length-prefixed, and encoded as UTF-8;
- PostgreSQL `pg_catalog.sha256(bytea)` is encoded as lowercase hexadecimal;
- the complete 64 hexadecimal characters are retained (operation ID length
  79, event ID length 85, both within the existing 128-character identifier
  limit).

Existing rows and their stored operation/event IDs are never rewritten.
Replaying an idempotency key created under `000013` returns the stored old
identifier.  A new call after `000014` receives deterministic, distinct
operation/event namespaces from the new domains.

This milestone records the SQL successor as a versioned implementation
candidate only.  It does not regenerate or retarget the canonical migration
manifest/schema-bundle, the v2 lineage document, or the D-053 supply profile;
those generated authority/runner bindings are a separate future entry once
their current-source projection and review boundary is explicitly opened.
The historical `000013` closure therefore remains byte-current in the
integration checkout while this identifier candidate is reviewed in isolation.

## Focused evidence and review

The implementation candidate must be checked with only affected migration,
SQL-classifier, local-migration, and durable-store/identifier tests.  Static
checks must prove `000013` is byte-identical, `000014` contains no `md5(`, both
domain labels are present, the full 64-hex suffix and prefix bounds are valid,
and the SQL statement is admitted by the existing classifier.  Canonical
bundle/runner currentness is explicitly out of scope for this isolated
candidate.  A disposable local PostgreSQL assertion may be used for actual ID
behavior; it is not production evidence.

After the implementation is clean, create one direct-child candidate and one
fresh independent read-only review with an explicit P0/P1/P2 verdict.  The
review may approve only this bounded repair; it cannot close an aggregate Gate
or authorize any external side effect.
