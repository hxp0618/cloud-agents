# G-CONTRACT external-consumer v3 replay implementation

This document defines the first implementation slice for D-053-EC-3. It binds
the v3 replay runner and its receipt schemas to the current successor authority
and adds a candidate-bound projection writer with scoped native replay evidence.

The runner exposes authority checks and bounded external evidence operations:

- `--check-authority` validates the generated v3 source and verifies that the
  profile and all six replay receipt paths are absent.
- `--plan` performs the same validation and prints a deterministic plan for
  one projection plus Darwin arm64 and Linux amd64 A/B native replay runs.
- `--projection` with no candidate arguments performs the authority-bound
  projection preflight only. It returns `BLOCKED` until an externally frozen
  clean candidate is supplied and never creates output.
- `--projection --candidate-root ... --candidate-manifest ... --output-root ...`
  re-reads the external candidate manifest and Git inventory, verifies stable
  bytes and the source/successor bindings, filters the successor exclusions,
  and writes a deterministic USTAR archive, member manifest, and projection
  receipt only beneath the external output root. The receipt binds the
  candidate manifest digest and tree identity and remains `notGateClosure`.
- `--native --projection-root ... --platform ... --run-id ... --output-root ...`
  validates the projection receipt and archive bytes, extracts the frozen
  members into a disposable realpath, installs and builds from the offline
  lockfile, and runs fresh TypeScript and Go SDK consumers. Their loopback
  fixture requests are recorded as JSONL and must contain exactly one project
  GET and one protobuf Worker `Negotiate` POST for each named consumer before an
  external native receipt is written. Each consumer also records its actual
  decoded Project value and the deterministic protobuf encoding of its decoded
  NegotiationResponse in the same JSONL. The runner requires exactly one outcome
  per consumer and binds the SHA-256 of the canonical JSON result as
  `outputSha256` in that consumer's receipt entry. Dependency-fetch records do
  not count as consumer calls. Missing, duplicate, unknown or misattributed
  consumer calls/outcomes fail closed. Offline Go dependency artifacts are served from
  the host's already materialized module cache through the disposable
  loopback proxy; no provider or internet egress is allowed.

- `--summary --projection-root ... --darwin-a-root ... --darwin-b-root ...
  --linux-a-root ... --linux-b-root ... --output-root ...` validates the current
  source, projection receipt and its archive/member-manifest bytes, then all four
  native receipts against their schemas, exact platform/run identities, source
  and complete projection bindings. Every consumer result digest must be equal
  across the four runs. Only then does it copy the validated evidence unchanged
  into a new external bundle and generate `replay.json` and `profile.json`.
  The profile binds all six receipt bytes and every semantic input from the
  source; inventory equality is checked against that source rather than a
  manually repeated schema count. The profile state is `PROFILE_CURRENT` with
  status `PROFILE_CURRENT_FINAL_REVIEW_PENDING`, never automatic approval.
- `--check-summary --output-root ...` independently re-reads the bundled
  projection/native evidence, recomputes both generated documents and requires
  byte-for-byte equality. It writes nothing. Missing, stale, mismatched,
  symlinked, altered or duplicate-run evidence fails closed. Inputs and output
  must be external to the authority tree; output must be distinct from and not
  nested with any input, must not overwrite existing files, and only the
  declared evidence bundle is written. Test fixtures never become acceptance
  receipts. A source change requires new projection/native replay, not rewriting
  old receipts to claim the new binding.

The authority checks and no-argument plan have no filesystem writers, provider
side effects, database writes, or external egress. Candidate projection only
writes the three declared external artifacts; it does not start providers,
write the authority tree, or advance the `REPLAY_PENDING` state. Native
receipts are also external and remain scoped evidence. A replay summary must
compare each consumer's observed result digest across all four runs as well as
the source and projection bindings; matching request counts alone is
insufficient. Existing receipts without result digests cannot be supplemented
by inference and must be rerun against the updated authority. The summary/profile writer supplies external reproducible evidence only. Source
authority and its checked-in absent-receipt boundary remain `REPLAY_PENDING`;
final review, promotion and formal Gate closure remain independent actions.
Current results belong in 06, not here.

The v3 schemas are authority inputs for the eventual projection receipt,
native replay receipt, replay summary, and profile. They deliberately require
`notGateClosure=true` and `gateStatus=ALL_GATES_OPEN`; no checked-in profile or
receipt is created by this slice.
