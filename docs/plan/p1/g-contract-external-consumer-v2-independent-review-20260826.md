# D-053-EC-2 r3 independent authority review

Date: `2026-08-27`

This is an independent, read-only review of the fixed r3 authority candidate.
The review was performed against the committed candidate bytes and does not
run projection, replay, profile generation, receipt writers, external HTTP,
provider/P2 work, production database work, deployment, publication, or any
Gate transition.

## Verdict

`APPROVE` — P0=0 / P1=0 / P2=0.

The approval is limited to the authority-only, versioned D-053-EC-2 r3
contract and its separately bounded future replay/profile stages. It does not
claim that any replay, generated profile, receipt, external-consumer result, or
Gate is current or closed.

The `platformMatrix` pending entries are frozen requirement declarations for
the future replay stage, not observations of completed platform runs. Any
runtime/profile state transition requires its own generated artifacts and
review under the frozen state machine; r3 bytes remain immutable.

## Fixed candidate and non-recursive review binding

- candidate commit: `96de06eeecc49914b4dc699e9761fae2bcbf30aa`;
- candidate tree: `a1871a82110d6899d7b59a8035b10877c8536d94`;
- direct parent (r3 authority base):
  `8f75200ef61c5ab7ccdf097188de35a48ace4a95`;
- candidate diff: exactly `M` on
  `tools/g-contract-external-consumer/v2/source.json`, mode `100644`;
- this review path is absent from the candidate tree and is added only by the
  single-parent review child.

The JSON record below deliberately stores the candidate as `reviewParent` but
does not store the review commit/tree: those values would be self-referential
because the document is part of its own Git commit. The checker binds the
actual review child, parent, path, status, mode, and committed bytes externally.

```json
{
  "formatVersion": "cloud-agents-g-contract-external-consumer-independent-review/v2",
  "decisionId": "D-053-EC-2",
  "profileId": "g-contract-external-consumer/v2",
  "authorityRevision": "D-053-EC-2.r3",
  "reviewKind": "AUTHORITY",
  "candidate": {
    "commit": "96de06eeecc49914b4dc699e9761fae2bcbf30aa",
    "tree": "a1871a82110d6899d7b59a8035b10877c8536d94",
    "parent": "8f75200ef61c5ab7ccdf097188de35a48ace4a95"
  },
  "reviewParent": {
    "commit": "96de06eeecc49914b4dc699e9761fae2bcbf30aa",
    "path": "docs/plan/p1/g-contract-external-consumer-v2-independent-review-20260826.md",
    "status": "A",
    "mode": "100644"
  },
  "topology": "DIRECT_SINGLE_PARENT_CHILD_ADDS_EXACTLY_ONE_PREDECLARED_100644_FILE",
  "changedPaths": [
    "docs/plan/p1/g-contract-external-consumer-v2-independent-review-20260826.md"
  ],
  "reviewInput": "FROZEN_SOURCE_SCHEMA_AUTHORITY_BYTES_ONLY",
  "checks": [
    {
      "id": "authority_revision_fence",
      "status": "PASS",
      "evidence": "D-053-EC-2.r3 is explicit; r2 tuple eb0e2c2d7d71ad62ca668e6fcc6b18ff0396b1da/8a5d53c8f6572275171a9af5e22dd587a193bbfe/bda551c3c49a1010346a3fac835ac9e1cdb7e917 is superseded append-only, and the authority registry identity is cloud-agents/g-contract-external-consumer-replay/v2."
    },
    {
      "id": "source_schema_check",
      "status": "PASS",
      "evidence": "The read-only --check-source runner passed; strict AJV 2020-12 compilation passed for source, profile, projection receipt, native replay receipt, replay summary, and review schemas."
    },
    {
      "id": "authority_file_fence",
      "status": "PASS",
      "evidence": "All 11 authority files match their frozen Git blob SHA-1, SHA-256, byte size, and 100644 mode at authority base 8f75200ef61c5ab7ccdf097188de35a48ace4a95."
    },
    {
      "id": "lineage_fence",
      "status": "PASS",
      "evidence": "The immutable EC-1 candidate/review fence and the complete C to r1 to r2 to r3 single-parent chain match their recorded commit/tree/parent tuples; no amend, rebase, squash, merge, force-push, or history rewrite is used."
    },
    {
      "id": "semantic_input_check",
      "status": "PASS",
      "evidence": "Exactly 18 ordered semantic inputs are bound to the immutable EC-1 candidate with the UTF-8-bytewise sorted path/mode/size/SHA-256 NUL manifest algorithm; no non-ignored untracked input is admitted."
    },
    {
      "id": "projection_algorithm_check",
      "status": "PASS",
      "evidence": "Exactly 13 UTF-8 byte-exact exclusions are frozen; archive/member algorithms, uncompressed ustar settings, deterministic tar metadata, and symlink/submodule/special-file rejection are fixed, while projection outputs remain absent."
    },
    {
      "id": "runner_toolchain_platform_check",
      "status": "PASS",
      "evidence": "Only the Bun 1.4.0 check-source runner is enabled with TypeScript 5.7.3, Go go1.27.0 darwin/arm64, GOWORK=off, GOFLAGS=-mod=readonly, network DENY; Darwin arm64 and Linux amd64 A/B are required-pending and Linux arm64 is not claimed."
    },
    {
      "id": "receipt_path_state_check",
      "status": "PASS",
      "evidence": "All 11 exact receipt paths are CREATE_ONCE_APPEND_ONLY and ABSENT_PENDING; synthetic receipts are forbidden, and profile/archive/replay/review outputs are absent at the candidate tip."
    },
    {
      "id": "candidate_topology_check",
      "status": "PASS",
      "evidence": "Candidate 96de06eeecc49914b4dc699e9761fae2bcbf30aa has tree a1871a82110d6899d7b59a8035b10877c8536d94, parent 8f75200ef61c5ab7ccdf097188de35a48ace4a95, and only the declared source.json M/100644 change."
    },
    {
      "id": "independent_review",
      "status": "PASS",
      "evidence": "A separate read-only review reproduced the authority checks and returned APPROVE with P0=0, P1=0, P2=0; the committed review child and its bytes are checked by the external Git binding."
    }
  ],
  "findings": {
    "P0": 0,
    "P1": 0,
    "P2": 0
  },
  "decision": "APPROVE",
  "gateEffect": "NO_GATE_CLOSURE",
  "notGateClosure": true
}
```

`notGateClosure=true` and `gateEffect=NO_GATE_CLOSURE` remain mandatory. The
review authorizes no production database write, public/provider HTTP or P2
effect, OIDC/JWKS, SSH or hardware action, deployment, publication,
release/signing, or Gate transition.
