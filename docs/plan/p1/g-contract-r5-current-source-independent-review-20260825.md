# G-CONTRACT current-source R5 independent review

Date: 2026-08-26 Asia/Shanghai

This is an independent fixed-object review of the Slice G R5 candidate
introduced by commit 3c38a88ca6f8355ff37ccc46ae8db68e0dabed09. The review was
performed against the committed bytes and the versioned typed builder/checker;
the candidate was not modified during review.

## Verdict

APPROVE - P0=0 / P1=0 / P2=0

Normalized verdict: APPROVE_P0_0_P1_0_P2_0.

This approval is limited to the R5 candidate and its predeclared Slice H
review tuple. It does not close G-CONTRACT or any aggregate Gate, and it does
not authorize production database writes, HTTP/P2/provider effects, deployment,
publication, signing, release, or any external side effect. The candidate
continues to declare notGateClosure=true and gateStatus=ALL_GATES_OPEN.

## Fixed candidate identity

- candidate commit: 3c38a88ca6f8355ff37ccc46ae8db68e0dabed09;
- unique parent: eb18690dc626c3950921aff8005fee68c37657e4;
- candidate tree: 2463122da6de4d5dbf9113062b19ee9ebd0325cc;
- parent tree: 586c0990da3d17c1aea6559ce442853bef4f3525;
- candidate subject: docs/plan/cloud-agents-platform/evidence/G-CONTRACT/CAG-G-CONTRACT-P1-20260825-R5.md;
- candidate path is absent from the parent tree;
- candidate diff is exactly one added regular 100644 path;
- candidate blob: 526029ceabad1fa60845998d3a01a39c0834e2c2;
- candidate file SHA-256: a56a16599719a2c34db70e8280d584e21d5890d8f98022293f94e1bf5e5d7eea;
- candidate file size: 8318 bytes;
- raw full-index binary diff SHA-256:
  c30c80da131929df958a8d32d3536b68b20cd30f5ad01910792f72fcf13bebc2;
- domain-separated R5 candidate diff:
  sha256:4513798ba42442ffb85fad8653cbe6c0b32627e59758fe5fc027a5bbfbcdb787.

No merge parent, rename, copy, mode change, deletion, symlink, or unrelated
path is present. The H review path is absent from this candidate, so the
candidate does not self-review.

## Reproducible typed R5 record

The deterministic model builder and renderer reproduce the committed R5 bytes
byte-for-byte. The record binds:

- source digest:
  sha256:3715914aebba7b74437e9694dac8427bf94ebcfea5b50505d45641dffb9df34c;
- model digest:
  sha256:71c21d82fa934627edb11405d4869fab8ede83802fca9e918dc5609ae8b6e76c;
- exact projection commit/tree:
  c7e0265c6d0550c64187c6164b078342746b1a10 /
  3c108c78c045bc828c1e6f5016f5232b261b8d47;
- projection archive:
  sha256:1816bdf3636fd444c5e9ac99b6d5ac0e0f6a60c958a66e16931fb71030728c62;
- Slice E assembled supply candidate:
  89458237b5dbb3e8f446d49302b6d2f4c7c68154, tree
  b4cae7e48a26f25ce016e452f40b90b77bfad413, parent
  44b7378775d47624a010f7718a0510736e34cefe;
- Slice E candidate domain diff:
  sha256:aeccc45ba4dca7a083fbb85ecaa1cbf84520a065897eecae436b8e263d53b96f;
- Slice F supply review:
  eb18690dc626c3950921aff8005fee68c37657e4, tree
  586c0990da3d17c1aea6559ce442853bef4f3525, SHA-256
  sha256:5a3c275bf72b27a82d5939b58a0dc826bcc5bc45fa09abd69e7dfc6a3ee91d0a,
  verdict APPROVE_P0_0_P1_0_P2_0;
- assembled v3 lock: commit
  89458237b5dbb3e8f446d49302b6d2f4c7c68154, blob
  5802cf6129b8130b85f89f21422aa32a7e0045f9, SHA-256
  sha256:6ef5c8ee897079c04254e97beeda5e2b5d9ab6b395ba8f690985186ec8420297,
  state ASSEMBLED;
- immutable v2 predecessor:
  16275f6cbf390c343a9ac00f9193e75eaad0094e, tree
  ca595b8e1258a8b78c4da3a545b2a31d8f62b531.

The builder independently checks the supply review child, exact projection
and profile bytes, current source inputs, assembled lock state, and the
authorized superseding lineage. It rejects stale or phase-bound substitution,
review drift, self-review, and non-canonical review children.

## Criteria and non-claims

The rendered R5 record enumerates the complete ordered G-CONTRACT criteria
authority. The derived missing set remains exactly five open rows:

1. json-schema-authority-and-openapi-refs
2. proto-authority-and-generated-connect-grpc-mapping
3. shared-golden-negative-and-n-minus-one-fixtures
4. exact-pinned-external-consumer
5. digest-change-invalidation

The record keeps status IN PROGRESS, independent reviewer PENDING, gate effect
NONE, and closure decision NONE. It explicitly makes no claim of
G-CONTRACT/G-SUPPLY-CHAIN closure, current vulnerability closure, Linux arm64
replay, production migration or database execution, HTTP/OIDC/JWKS/P2/provider
or workload effects, deployment, publication, external signing, release,
Beta, or GA.

The invalidation rules preserve append-only superseding semantics: any
prerequisite, criteria, source-input, projection, supply assembly/review, or
assembled-lock identity drift invalidates this R5; only the versioned
ASSEMBLED-to-PHASE_BOUND snapshot successor is exempt as the authorized
historical transition. R5 and its review must never be overwritten.

## Focused verification and boundary

Independent checks returned:

- typed R5 model/render comparison: exact byte equality;
- R5 candidate topology: single parent and exact one-path add;
- supply-v3 assembly/source/lock current checks: current;
- replay/profile focused checks: pass, with exact-49 output equality and
  nonAllowlistedChanges=0 inherited from the reviewed fresh receipts;
- phase topology before H: R5_CURRENT_REVIEW_ABSENT;
- repository diff check: clean.

No production database, migration, HTTP/P2/provider, deployment, publication,
release, signing, or Gate operation was executed. No live external authority
was promoted. The next permitted action is only the predeclared H review-only
child, followed by independent detached binding; all Gates remain OPEN.

