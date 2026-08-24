# Gate candidate record: `G-AUTHORITY` / P1 / R1

- Evidence ID：`CAG-G-AUTHORITY-P1-20260823-R1`
- Record type：`PHASE`
- Phase / aggregate Gate：P1 authority foundation / `G-AUTHORITY`
- Prerequisite record IDs：`CAG-G-INVENTORY-P0-20260810-R3`、`CAG-G-BASELINE-P0-20260823-R4`
- Supporting current-source records：`CAG-G-CONTRACT-P1-20260823-R4`、`CAG-G-DATA-P1-20260823-R1`
- Accepted durability boundary：D-048 / ADR-0024
- Supersedes：none；this is the first current-source `G-AUTHORITY-P1` phase candidate
- Status：`IN PROGRESS`
- DRI：hxp0618（owner）；Codex current-source evidence executor
- Independent reviewer：`PENDING`
- Date：2026-08-23 Asia/Shanghai
- Gate effect：none；this record does not close `G-AUTHORITY-P1` or the aggregate `G-AUTHORITY`

## Scope

This record binds the current P1 single-writer, generated-contract, PostgreSQL authority, live-instance and local
runner-recovery facts to Inventory R3 and Baseline R4. It separates fresh bounded checks, exact reviewed subtrees and
historical supporting records. A shared current subtree never inherits a broader review verdict than that review's
explicit scope.

Current source preserves narrow fail-closed authority slices. It does not yet supply a provider-catalog persistence
writer, complete Tenant/Organization/Project runtime ownership, an enabled PlatformOperation/attempt/receipt writer,
published database authority subjects, executable cumulative catalog projection, a reviewer-signed current filesystem
Done result or an immutable authority closure. Those boundaries remain open.

D-048/ADR-0024 makes physical controller/cache-loss optional hardening rather than mandatory P1 evidence. It does not
turn clean `poweroff` into crash evidence or complete durability: closure still needs the accepted no-sync externally
observed bare-metal crash, ext4/XFS/QEMU evidence combination, post-boot exact verification, current-source phase record
and independent Gate review.

## Fixed source and prerequisites

- Candidate source commit/tree：`a0f24cf48af40d021b47e818fd9f36ffd5b55499` /
  `3dc0fa166664ed0d56946cd136dfe4234043074d`
- Source branch：`codex/cloud-agents-p1-software-crash-r4-rebind-20260823`
- Evidence branch：`codex/cloud-agents-p1-g-authority-r1-current-20260823`
- Source state before this evidence-only change：clean；upstream `0/0`；remote source branch exact
- Current subtrees：
  - `contracts`：`40f0f3b44f83c986f9b015d059451e195e285c0a`
  - `sdk`：`e4c5abf9d9cb591df39d9377529c201a1307997e`
  - `scripts`：`d65f14ec5cc8b2bda27af056673e891cda8cebd1`
  - `services/control-plane`：`689942aecbc7f84f692dd71c17d66a607d12b950`
  - `internal/migration`：`e01ddef945d4cec352ac107831c9c86af029ff86`
  - `internal/store/postgres`：`6580be0bbbb3e1056439da0698ec08cca5d46e33`
  - `internal/evidencefs`：`5e4b0b76a8f1b8c6408b4505a8befa8e5e3369ed`
  - `internal/mountauthority`：`f64f4c47e96843ff8dde38c401d138ade9b0eaaf`
  - migration catalog：`add22d3f6404a06b9cb584576fc969bc4a428ecd`
- Inventory R3 record SHA-256：
  `d865bb9fd1195342a7251bd6822130437753f5be4af8312d9d5c5ee4d5b71400`
- Baseline R4 record / review SHA-256：
  - `57429377291d1b6a41ff886cde2a6692afd63b5c15adf0677767d59e87b03dd9`
  - `44db2df153bbfcc5fa0bd4c928bbdf9b207c60c4458ec61b2e2557c7d97d4c94`
- G-CONTRACT R4 record / review SHA-256：
  - `0982261244e7315c2798db4a4f0913f7f93037c251140c8f14ed2cbc3bcd7152`
  - `f0d5b12f1f6e0f2936783868331d4d74d7a3ee0fc49b3e370894b95884458f61`
- G-DATA R1 record / review SHA-256：
  - `1cf34ac76778f28dc790ac2d6b780b0d7b526826d9b2d8596065a610d80d7a0d`
  - `78233500ce5758115e153c09c342692eda08e212de451fc21e589e03bbd9708b`
- ADR-0024 / its original independent review SHA-256：
  - `597f0d9881aabe44e9d67876ae81d83808a18c3725f5c7ac66279bcda53e0bd0`
  - `0b7fb81292e507b9bf15204d44670027c41bf7e83efe07612758217a5c5a712e`
- Gate criteria SHA-256：`4a3d0b3c184e9673944411adbc5c8ea933883c855d5aada67862dad8e4dcc994`
- ADR-0007/0008/0009/0010 SHA-256：
  - `9e59b17e6f43db986ca4d5cc09ff62f4acb7cd8ebdfc61583d821ecdba11899c`
  - `6a7b9c525b4625e0f6074ba64b53151224a093837f4dda131fc533cef05fdb91`
  - `baf10f9982a519e0c616281e94cd6daf4f70a098c08f1110e6bbc4317aa666b8`
  - `4f98e7d7acd165fcae85f39ce4337f37ebf2b817fa2999aef0b1f99678d3f32d`
- Deployment profile：none；no production database, migration, HTTP/P2/provider, deployment or publication action

## Machine-readable database authority state

| Current descriptor                  | SHA-256                                                            | Exact production status                                                                                          |
| ----------------------------------- | ------------------------------------------------------------------ | ---------------------------------------------------------------------------------------------------------------- |
| `catalog/authority-v1.json`         | `eb8c4ad607dc3443471fa376a9da9bf49e17788ffcc9cda6d2ccecd982327ccd` | `UNPUBLISHED_BOOTSTRAP_MUTABLE` / `NOT_IMPLEMENTED`                                                              |
| `catalog/global-table-authority-v4` | `cceee87df70fc145d24a7d66220dd012adc68f77af1c7aa964312583afc5b42d` | `UNPUBLISHED_BOOTSTRAP_MUTABLE` / `NOT_IMPLEMENTED`                                                              |
| `catalog/schema-000012.json`        | `c424e9a62180c8e3de4cb444d95812c2606c6355065f4fa7e5655fcd733dab48` | `UNPUBLISHED_BOOTSTRAP_MUTABLE` / `NOT_IMPLEMENTED` / `NOT_IMPLEMENTED_A2_1B_REQUIRED`；no `expected_projection` |
| migration `manifest.json`           | `97c02a54639d9a7d00dbc55a14e06db8e97bc2c36444cf51b61a680539cfd44e` | public lineage `cloud-agents-platform`；exact global-authority path                                              |
| migration `schema-bundle.json`      | `948e504b77c409065d2160056f45356d84d136d2512f35a4c4fe9e16e575aaaf` | generated current bundle                                                                                         |
| `contracts/generation.lock.json`    | `4f2953540e9305f034a8f6fc7d13af0947d7f5b91f43b7ce6256bc137d071c76` | `notGateClosure=true`                                                                                            |

These checked-in descriptors are mutable bootstrap inputs, not published or runtime-trusted authority. The runner
continues to fail closed before database access when production publication/introspection/executable projection is
required. A test fixture containing a published state is not production publication.

## Exact reviewed scopes

Only these narrow conclusions are inherited:

1. G-CONTRACT R4 covers the exact current `contracts`/`sdk`/`scripts` generation and wire-authority scope. It does not
   prove runtime/database writer ownership.
2. The live-instance retirement candidate has the exact current `services/control-plane` subtree, but its review scope
   is only receipt-gated retirement/preflight and local recovery. It proves that fencing/expiry alone cannot replace an
   exact complete receipt and six revoke/release facts. Record/review SHA-256：
   - `388ae77b0758b8ddf85cb7d4ce7fe5c2ac20c9363b3441188d10cd3f232866cf`
   - `816a08901e0f288a8d267a85c08c9b22da1f42d38b8e58d14eef37ad02959339`
3. ADR-0023 Slice G has the exact current `internal/migration` subtree. Its review covers the ordered local recovery
   authority/result boundary, one-shot profile separation and closed failure precedence only. Review SHA-256：
   `5cf26966f873c563ba2bc6e84d8b94ebe237534b2d95a137ea89b44db8ce030c`
4. ADR-0024 and its independent review are exact accepted policy bytes. They change the evidence boundary but do not
   turn any filesystem or authority implementation into a pass.

## Historical supporting records not inherited

Earlier PostgreSQL catalog, lineage/quota, trusted-mount, EvidenceSink, ext4/XFS/QEMU barrier and host-crash records
remain useful provenance. Their broader verdicts are not inherited because at least one implementation subtree differs,
their review scope is narrower, or they have no independent reviewer. In particular:

- the old catalog review used catalog subtree `b380b016…` while current is `add22d3f…`；
- the old lineage/quota review used `internal/migration=07aa022…` while current is `e01ddef…`；
- the trusted-mount record used `internal/evidencefs=cdc8283…` while current is `5e4b0b7…` and had no independent
  reviewer；
- the host-crash record used control-plane subtree `c1d678f…`, is an unreviewed single ext4 scenario, and is not a
  current filesystem Done result.

## Fresh bounded checks

The exact Go `1.26.6 darwin/arm64` toolchain ran five authority/catalog fail-closed tests only:

```bash
GOWORK=off GOFLAGS=-mod=readonly GOTOOLCHAIN=local go \
  test ./internal/migration -count=1 \
  -run '^(TestCheckedInMutableCatalogSubjectsFailClosed|TestVerifiedAuthorityProfileTotalBindingFaults|TestAuthorityBindingRequiresThreeCompletePhases|TestCheckedInProjectionFixtureManifestSameBits|TestExecutableContractsCannotUseBootstrapSparseShape)$'
```

Result：PASS in `0.756s`.

Five focused PostgreSQL service/writer/rollback tests ran separately:

```bash
GOWORK=off GOFLAGS=-mod=readonly GOTOOLCHAIN=local go \
  test ./internal/store/postgres -count=1 \
  -run '^(TestTenantReadCommitAcknowledgementUnknownHijacks|TestRBACMutationServiceAllFiveMethodsUseClosedFunctionSet|TestRBACMutationServiceMapsConflictAndUnknownCommit|TestDurableCoordinationCompletionHasClosedCommitOutcomes|TestCompatibilityRecoveryPreflightIsReadOnlyAndExact)$'
```

Result：PASS in `1.148s`.

Strict `jq -e` checks reproduced the descriptor statuses in the table, required absence of `expected_projection`, and
bound the manifest to the public lineage/global-authority path. A bounded source scan found no provider-catalog symbol in
`internal` or `migrations`. The active managed-agent create-project coordination profile fixes
`createsPlatformOperation=false` and `externalSideEffect=forbidden`; its service binder rejects either flag being
enabled. The PlatformOperation schema/table therefore does not constitute an enabled operation writer.

No full `internal/migration`, broad race, live PostgreSQL, filesystem/crash, remote host or external-effect test ran for
this record.

## Exit criteria mapping

| `G-AUTHORITY-P1` criterion                                            | R1 candidate result         | Evidence / open boundary                                                                                   |
| --------------------------------------------------------------------- | --------------------------- | ---------------------------------------------------------------------------------------------------------- |
| Tenant/Organization/Project runtime writer and persistence authority  | NOT COMPLETE                | tenant bootstrap exists；complete neutral resource runtime ownership is not closed                         |
| Membership/basic RBAC single writer                                   | PARTIAL CURRENT-SOURCE      | five typed mutation paths and focused unknown-commit checks；no current aggregate authority review         |
| provider catalog single writer                                        | NOT IMPLEMENTED             | bounded current-source scan found no provider-catalog persistence implementation                           |
| contract/schema/generated-code authority graph                        | PARTIAL EXACT REVIEWED BITS | G-CONTRACT R4 exact generation scope；seven contract missing items remain                                  |
| migration/catalog authority                                           | FAIL CLOSED / NOT PUBLISHED | current descriptors are bootstrap-mutable/runtime-NI；cumulative executable projection absent              |
| idempotency/outbox/leader ownership                                   | PARTIAL CURRENT-SOURCE      | typed coordination/retirement slices exist；whole aggregate writer set not independently reviewed          |
| operation/attempt/receipt writer                                      | NOT ENABLED                 | schema/table exists；active generated profile forbids creation and external effects                        |
| tenant context, roles, global allowlist and live registry owner       | PARTIAL                     | focused fail-closed/current retirement evidence；no fresh whole-schema live DB authority replay            |
| fault/retry/rollback does not switch writer                           | PARTIAL EXACT REVIEWED BITS | exact Slice G local recovery plus focused service tests；not a database/filesystem aggregate result        |
| P2+ Session/Turn/Worker/Lease/workload/pairing/T3 authority non-claim | PASS AS BOUNDARY            | this record neither implements nor verifies later-phase writers                                            |
| ADR-0024 current durability combination and filesystem Done           | NOT VERIFIED                | physical hard-power optional；required software-crash/matrix/current-source independent aggregation absent |
| immutable current-source independent review                           | PENDING                     | this R1 awaits a fresh P0/P1/P2 review                                                                     |

## Hard blockers and non-claims

- Provider-catalog persistence is absent；complete Tenant/Organization/Project runtime ownership and enabled
  operation/attempt/receipt writers are not closed.
- Database authority/catalog subjects are unpublished and runtime-not-implemented；all cumulative executable catalog
  projections remain absent.
- Current-source aggregate tenant-role/global-table/live-registry authority has not been replayed on live PostgreSQL or
  signed by an independent phase reviewer.
- No reviewer-signed current filesystem Done or complete ADR-0024 durability combination exists. Physical controller,
  host power and cache-loss remain unclaimed optional hardening；clean `poweroff` is not crash evidence.
- No production database, migration, HTTP/P2/provider action, deployment, publication, release, Beta, GA or Gate
  transition occurred.
- This P1 record does not pre-authorize Session/Turn/Worker claim, Managed Host Lease/workload/pairing or T3 proof-session
  writers.

## Invalidation

R1 becomes stale if Inventory R3, Baseline R4, G-CONTRACT R4, G-DATA R1 or ADR-0024 is invalidated；if the fixed source,
any named subtree/descriptor/record hash changes；if publication, runtime introspection, executable projection, provider
catalog, aggregate writer or durability semantics change；or if a later phase adds a writer that changes the P1 authority
set. A superseding record must replay the changed criterion and receive a new independent review.

## Sign-off

- DRI conclusion：current source preserves reviewed narrow contract, retirement and local runner authority slices while
  database publication, several P1 aggregate writers and durability aggregation remain explicitly open.
- Reviewer conclusion：`PENDING`.
- Closure decision：none；`G-AUTHORITY-P1` and aggregate `G-AUTHORITY` remain `IN PROGRESS`.
