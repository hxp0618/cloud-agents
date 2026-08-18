# ADR-0012：P1 versioned lineage/quota profile

- Status：Accepted — implementation and independent review complete
- Date：2026-08-18
- Scope：P1-A2.2-impl-3 admission and historical evidence compatibility
- Depends on：ADR-0009、ADR-0010、ADR-0011
- Supersedes：only the current-generation quota/profile selection boundary in ADR-0010; it does not
  rewrite, reinterpret or re-encode any historical v1 evidence
- Does not authorize：production database mutation, migration admission, Gate closure, deployment,
  release, or entry into A2.3

## 1. Decision

The direct PostgreSQL subject-issuer correction in append-only migration `000006` is required, but the
five-entry v1 bundle formula cannot reserve the six-entry bundle inside the frozen 16 MiB lineage-index
maximum. The approved remedy is an explicit, versioned lineage/quota profile. A generation receives one
profile at admission and carries it immutably through its signed manifest, planned segment-0 header,
`GenerationReserved` quota digest, journal frames and recovery/reopen checks.

Profile selection is an authority fact. It is never inferred from schema head, migration count, source
code version, statement count, or the observed size of a checkpoint. A missing, zero, unknown, stale,
profile-swapped or cross-bound profile fails closed before database connection or durable mutation.

## 2. Profile identifiers and compatibility

### Historical v1

The existing values remain byte-exact for all archived and already durable generations:

- manifest format: `cloud-agents-platform-migration-manifest/v1`;
- journal `limits_profile`: `cloud-agents-platform-evidence-journal-limits/v1`;
- physical lineage-index `limits_profile`: `cloud-agents-platform-lineage-index-limits/v1`;
- closed checkpoint framed maximum used by the v1 reservation formula: `16 KiB`.

The physical lineage-index profile remains v1 even for a v2-selected generation: it continues to describe
the container and its 16 MiB global decoder ceiling. It is not a substitute for the selected lineage/quota
profile.

### New v2

The current generated bundle uses:

- manifest format: `cloud-agents-platform-migration-manifest/v2`;
- `execution_policy.lineage_quota_profile`:
  `cloud-agents-platform-lineage-quota-profile/v2`;
- journal `limits_profile` in every new generation:
  `cloud-agents-platform-lineage-quota-profile/v2`;
- v2 checkpoint framed maximum: `4096` bytes inclusive (8-byte length prefix plus canonical DTO).

The v2 maximum is deliberately smaller than the historical 16 KiB decoder ceiling. Existing generic
lineage decoding remains able to read v1 frames and can inspect v2 bytes up to the physical container
ceiling, but a v2 writer, reservation calculator, admission validator and reopen validator all enforce
the 4096-byte closed maximum. A checkpoint that exceeds it is a stored/creation contradiction, not a
request to widen the index.

The measured golden checkpoint currently encodes to 1,578 framed bytes. The implementation must retain a
test that constructs the fullest valid generated checkpoint shape and proves the generated current bundle
fits 4096 bytes; the test is a conformance check, not permission to use a sample or average size in quota
arithmetic.

## 3. Manifest and generation binding

`ExecutionPolicy` gains one explicit field, `lineage_quota_profile`.

- v1 manifests omit the field and map only to the historical v1 profile. Their bytes, manifest digest,
  schema/boot digests, quota digest and archived fixtures must remain unchanged.
- v2 manifests require the exact v2 format and exact profile identifier. Any other value, including an
  empty string, v1, an unrecognized future value or a duplicate/alternate field spelling, is rejected.
- The verified quota-bundle facts include the profile and include it in their canonical binding digest.
- The planned `JournalHeader.LimitsProfile`, `GenerationReserved.QuotaReservationDigest` and manifest
  profile must be equal. Successor and historical-reopen paths copy the already verified profile; they do
  not accept a caller replacement.
- `QuotaReservationDigest` preserves the old v1 subject/domain encoding for v1 objects. The v2 branch is
  explicitly versioned and includes the selected profile, so a v1/v2 swap cannot retain an old digest.
- `GenerationReserved.Validate`, segment-0 header validation, checkpoint append, strict replay and fresh
  reopen each recheck the same equality. A profile mismatch invalidates the generation/epoch and never
  yields an admission permit.

No profile field is added to the physical v1 `LineageIndexHeader`; changing that would rewrite the
historical container contract. The signed manifest plus the generation header is the dual authority for
the selected quota profile.

## 4. Quota and wire rules

The v1 arithmetic is frozen and must continue to produce the ADR-0010 values byte-for-byte. The v2
arithmetic uses the same statement, attempt, segment, journal-record and index-record counts, but replaces
the reserved checkpoint record-kind maximum with 4096 bytes. It does not change any of these inclusive
maxima:

- 16 MiB physical lineage-index bytes;
- 16,384 lineage-index records;
- 16 journal segments;
- 65,536 journal records;
- 256 MiB journal/whole-bundle reservation;
- root and object count/byte maxima.

For the approved six-entry bundle, the v2 formula must be checked before any DB or filesystem mutation.
The resulting index reservation is approximately 4.46 MiB (subject to the exact generated frame constants),
well below 16 MiB; the exact number is generated and asserted by the quota tests and manifest evidence.
The formula still reserves the closed maximum for every checkpoint. It never reserves the observed encoded
size of a particular checkpoint.

All v2 checkpoint construction sites, including normal append, rotation, successor and recovery paths,
must call the profile-aware encoder/validator. The generic v1 encoder remains available for historical
fixtures and non-profile-aware diagnostic decoding. A v2 checkpoint cannot be admitted merely because the
generic 16 KiB validator accepts it.

## 5. Historical replay, rollback and transition

Historical v1 generations are decoded with their v1 profile and recomputed with the exact v1 formula. No
old frame, manifest, SQL artifact, catalog digest or evidence fixture is rewritten. A current run may
read v1 history and create a v2 successor only after strict v1 replay and same-verifier authority binding;
the successor's profile is selected by the verified current v2 manifest and is recorded in its new reserved
header. A v2 generation cannot be silently downgraded to v1, and a v1 generation cannot be relabeled v2 by
editing an in-memory field.

If a profile is unknown, missing, swapped between manifest/header/reservation, or invalidated after an
append, the epoch is failed/poisoned and the operation returns the stable journal failure/invalid-authority
classification. It must not retry with another profile or perform a second reservation. Rollback of the
SQL correction remains append-only/application-level recovery; it does not mutate `000001`–`000005` or
rewrite a profile already recorded in durable evidence.

## 6. Required tests and review gates

Before `000006` is considered admissible, the implementation must provide:

1. v1 golden same-bits tests for manifest decoding, quota digest, checkpoint encoding and historical
   replay;
2. v2 manifest/profile binding tests, including zero/unknown/missing/profile-swap/cross-generation faults;
3. exact and `+1` checkpoint-size tests, generated-bundle proof that every v2 checkpoint fits 4096 bytes,
   and v1 exact 16 KiB compatibility tests;
4. v1/v2 quota arithmetic tests proving all unchanged root/index/journal limits and the six-entry bundle's
   exact reservation;
5. successor, rotation, crash-reopen and same-verifier tests proving profile propagation and no downgrade;
6. PostgreSQL 15/16/17 normal and race matrix reruns for `000006`, followed by an independent security
   review of the profile authority and all pre-DB fail-closed paths.

These are implementation/conformance gates only. They do not close `G-CONTRACT`, `G-DATA`,
`G-AUTHORITY-P1`, `G-SECURITY-P1` or any aggregate Gate until their separately required immutable closure
records exist.
