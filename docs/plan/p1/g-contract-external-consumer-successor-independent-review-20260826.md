# G-CONTRACT external-consumer successor independent review

Date: 2026-08-26

This is an independent, read-only review of the frozen `D-053-EC-1`
successor tip after its append-only profile-schema tightening child. This
review is a direct child of the candidate and is excluded from all candidate
inputs.

## Verdict

APPROVE - P0=0 / P1=0 / P2=0

The approval is limited to the versioned external-consumer evidence profile
and its declared pending projection/replay process. It does not upgrade or
close D-053, G-CONTRACT, G-SUPPLY-CHAIN, or any aggregate Gate.

## Fixed lineage and topology

- candidate commit: `f8d44568b0f64b31f466dbc47e0a17b15b96e659`;
- direct parent: `f3a058291ba6fbae53bc8dc96c695944426b2fb4`;
- candidate tree: `50662a40d175aa18f3d5eaf6f1c60d0a58c816db`;
- the candidate is a single-parent child changing exactly the declared
  implementation note, v1 profile, and v1 profile schema;
- all three changed paths are regular `100644` files; no merge, squash,
  rebase, force-push, or history rewrite is present;
- this review path is absent from the candidate tree and is added only by this
  independent-review commit.

The external-consumer candidate itself is a direct child of the repaired P0
terminal D-053 commit `4f71e38205fc25b3b164a24f13141644bd378cf7`. The four
declared predecessor fence files remain byte-identical between that terminal
ancestor and the candidate: generation lock, R5 record, R5 review-binding
review, and superseding-repair authorization.

## Authority, schema, and evidence

- `bun scripts/generate-platform-g-contract-external-consumer.ts --check-source`
  reports `source current`;
- `--check-profile tools/g-contract-external-consumer/v1/evidence/consumer.json`
  reports `profile current`;
- the tightened profile schema requires exactly the two Connect dependencies
  at versions `2.14.0` and `2.1.2`, rejects extra dependency keys, and validates
  Go module/go.mod sums as canonical `h1:` values;
- profile digest is `sha256:f747d2405d2973e895929e7a8c54f13c19af4489bdcd0001b4fe52b43ca64f8c`;
  schema binding is `sha256:8aab7afa284a5640038de58dee7d97d1e9d63521fe64b5641737f19ad826a82a`;
- authorization and all input-binding hashes, sizes, and modes match the
  candidate bytes; the evidence binding is
  `sha256:6b5b5669577aee59aa4bb775c585a04f01aae631bf7384d83727ca125a0a9344`;
- consumer evidence binds exact TypeScript SDK SHA-256/SRI, five npm
  dependency artifacts, Go module zip/go.mod SHA-256 and exact `go.sum`
  checksums, with one Connect `POST` per consumer;
- both consumers use only `http://127.0.0.1:<ephemeral-port>/` fixtures,
  `application/proto` request/response content types, and observed call count
  one. Harness/checker rejects `file:`, `workspace:`, Git/GitHub, local-path,
  and non-loopback URL escapes.

## Checks and boundaries

The following checks passed against the final candidate without external or
production effects:

- external-consumer source and profile checks;
- focused Vitest: 1 file, 8/8 tests;
- oxfmt, oxlint, and `git diff --check`;
- predecessor-fence byte comparison and exact final-tip topology review.

The final Go consumer execution records `GOWORK=off` and
`GOFLAGS=-mod=readonly`; dependency hydration occurs only inside disposable
consumer fixtures. The profile remains
`FRESH_CONSUMER_EVIDENCE_CURRENT_REPLAY_PENDING`; fresh projection and native
replay are pending and synthetic receipts are forbidden.

`notGateClosure=true` and `gateStatus=ALL_GATES_OPEN` remain mandatory. No
production database or migration write, public/provider HTTP/P2/OIDC/JWKS
effect, SSH or hardware action, deployment, publication, release/signing,
force-push, history rewrite, or Gate transition is authorized or claimed.

