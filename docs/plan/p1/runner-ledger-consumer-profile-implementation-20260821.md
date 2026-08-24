# Runner ledger consumer generated profile implementation — 2026-08-21

- Status: `LOCAL_SLICE_A_CANDIDATE`
- Parent HEAD: `ffab704cd26c4d817152602b0854ebb31ee1cc4b`
- Branch: `codex/cloud-agents-p1-runner-ledger-consumer-profile-20260821`
- Gate effect: none; every Gate remains open or at its prior phase status
- Runtime consumer: none in Slice A

## Frozen direction

This slice adds the versioned generated registry/profile `runner-ledger-consumer/v1`. It binds the exact immutable
`runner-ledger-preflight/v1` generated identity and maps its 17 closed pairs to:

- one `return_success_noop` pair;
- five `entry_not_implemented` pairs; and
- eleven `recovery_not_implemented` pairs.

The Go fact is ordinary copyable data. It contains no claim, session, transaction, database handle, evidence lease,
receipt, verifier artifact, writer token, HTTP handler, provider client, or mutation port. There is deliberately no
production caller before Slice B.

## Exact generated evidence

| Artifact                          | SHA-256                                                            |
| --------------------------------- | ------------------------------------------------------------------ |
| generated consumer registry       | `fa7082803ea97d06eefa83eec3de784f7199fc0b47f0ca2d0f8203b8b7e96852` |
| editable registry source          | `3b81553a58077bb1e748f7f4f6474c59ac9d8dcfb5fdbffd1cab00d7d4361b64` |
| generated Go profile              | `afc77e723b7a4439c47043376cb79f5cb6416ce22d54ab1dcffbfe49686ce928` |
| handwritten ordinary fact/profile | `36b0c369b4b9f072808f6b1a8874c3e1c9f58cabbeffddf41619ec42ed410b34` |
| registry generator                | `de12bc4ae7be2b46fca0b166bc35245700a3131196be46101e0c294b503f0fe1` |
| Go generator                      | `d532772736779fe8d1661c75bc0bcc965860c6dbb57f33b2bbb78c1d1d82ae1a` |
| registry library                  | `adda68eb19e220b09c8b19a3a2d974e3384e34f053b50da988e9bca882b30230` |
| generation lock                   | `c3abeb3f993e54303f187909e4eec32ece7eb21baa1570143bc9e91effe9802e` |

The two immutable preflight v1 outputs remain byte-identical to the parent:

- registry JSON: `2a04f67f9b06f25bc13211934d8cb914dcbe3f92d42053f636ec66b4f28ac11c`;
- generated Go profile: `599b78537a3f1dd5d70c1b50aa5e7bc54e1b65ee463876ee3f111709a5ab2112`.

The identity, JSON, and Proto SDK source bodies did not change. Their generated contract-manifest comments and
manifests were regenerated because the closed contract tree gained two schemas and two fixtures.

## Reproducible checks

Executed with Node `24.13.1`, Bun `1.3.14`, and Go `1.26.6` unless stated otherwise:

- contract bootstrap: `106` JSON files, `44` schemas, `56` fixture cases — PASS;
- consumer registry tests: `6/6` — PASS;
- scripts suite under the exact toolchain: `171/171` — PASS; an ambient Go `1.26.7` replay was intentionally
  rejected by the exact `1.26.6` Proto generator guard and is not counted as a candidate failure;
- focused migration normal and consumer-only race — PASS;
- control-plane `vet`, `build`, `mod tidy -diff`, and `mod verify` — PASS;
- generated Go SDK test/vet/mod verify — PASS;
- TypeScript typecheck and repository lint — PASS;
- consumer registry/Go, identity SDK, JSON SDK, Proto SDK, and generation-lock `--check` — PASS;
- Linux `amd64` and `arm64` migration test compilation — PASS;
- tracked-dirty and untracked formatting checks plus `git diff --check` — PASS;
- the 33-file candidate-only secret-pattern scan — PASS.

The repository-wide formatter still reports five pre-existing fixed-HEAD files outside this candidate. They were not
modified. A full-history secret scan and the full migration race closure are separate long-running evidence and are
not claimed by this Slice A record.

## Explicit boundary

This record does not implement or authorize the complete-ledger service consumer, entry execution, retry,
recovery, reconciliation, SQL, ledger/evidence append, transaction, commit, HTTP/P2/provider work, production
database writes, deployment, publication, release, or Gate closure. Slice B and Slice C remain pending, including
the fixed-candidate independent P0/P1/P2 review.
