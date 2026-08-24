# G-CONTRACT detached review-binding final independent review

Date: 2026-08-25

## Verdict

`APPROVE`

| Severity | Findings |
| -------- | -------: |
| P0       |        0 |
| P1       |        0 |
| P2       |        0 |

Normalized verdict: `APPROVE_P0_0_P1_0_P2_0`.

This independent fixed-object review approves only ADR-0029 / D-052 Slice G's
detached review tuple, generated binding registry, and successor generation
lock bytes. It does not rewrite the canonical closure-v3 profile, authorize a
production database write, HTTP/P2/provider effect, deployment, publication,
release, main merge, or transition `G-CONTRACT`, `G-SUPPLY-CHAIN`, or any
aggregate Gate. Every Gate remains `OPEN` or `IN PROGRESS`.

## Fixed candidate identity and exact path boundary

- review branch: `codex/review-detached-binding-slice-h-a595bd9`;
- candidate: `a595bd93ceee9d352645b9be66db92517fffb092`;
- parent: `d7c7468a72facc091b8a42be54d5af5c6a5785c4`;
- candidate tree: `9249e43bae506acef702fe70cdde798b37bd5148`;
- parent-to-candidate fixed-Git binary diff SHA-256:
  `2807c531a949afad97252f8bb5dfd738885f34e7d4ad93484c9411b146a846ef`;
- changed path count: exactly `3`; and
- this final review path was absent from the candidate, and the review
  worktree/index was clean before this separate record was authored.

The complete candidate diff is:

| Status | Path                                                   |
| ------ | ------------------------------------------------------ |
| `M`    | `contracts/generation.lock.json`                       |
| `A`    | `tools/contract-review-binding/v1/registry.json`       |
| `A`    | `tools/contract-review-binding/v1/review-tuple.json`   |

The exact candidate file identities are:

| Path                                                   | SHA-256                                                           |  Bytes |
| ------------------------------------------------------ | ----------------------------------------------------------------- | -----: |
| `contracts/generation.lock.json`                       | `de62a85390e58736a5fef8272878b821415b4180948437e54edb8628e005ff53` | 17,377 |
| `tools/contract-review-binding/v1/review-tuple.json`   | `b98d519207cf91544b2120642df1714217fb5b70a1414cc505f3969765479c50` |  3,374 |
| `tools/contract-review-binding/v1/registry.json`       | `e5e5a6abc573fcdcce9d0f1338dad033f5155fe76b555e1fca3a9efc19f14dde` |  2,298 |

All three are regular non-symlink files with a final newline. No binding
source, schema, generator, state-machine implementation, successor DAG,
canonical closure-v3 byte, supply-v2 byte, core generator output, immutable
v1/v2 predecessor, or Slice F review byte differs from the parent.

## Review tuple Git and review-byte authority

Both ordered tuple slots bind one common candidate and one common review
commit:

| Git authority | Value |
| ------------- | ----- |
| assembled candidate | `1ba7eda5ad6241ad8a065408d787e73cd7013ce0` |
| assembled candidate tree | `5a73a8edd4aee56a38aeb37c37b8009e481dfeae` |
| assembled candidate parent | `5def3ad5deb157264429dc5178f57ec916c66dc7` |
| fixed-Git candidate diff SHA-256 | `c5d11be264e6b9bc1e0a5c16c5b320c2d987a26b12ab2f7b4fc16efb594c92e2` |
| review commit | `d7c7468a72facc091b8a42be54d5af5c6a5785c4` |
| review tree | `92d44592cc39b513ffcbd47088a6560ff87c67ec` |
| review parent | `1ba7eda5ad6241ad8a065408d787e73cd7013ce0` |

Each review path was absent from the assembled candidate and exists as the
exact committed blob in the review child:

| Criterion / subject | Review path | SHA-256 / bytes | Verdict |
| ------------------- | ----------- | -------------- | ------- |
| `runtime-server-path-and-tenant-authority-enforcement` / `canonical_contract_closure` | `docs/plan/p1/g-contract-closure-profile-v3-independent-review-20260824.md` | `83975f780dbcaed587988155f680c33e3b1a42ee10776af2a3077a5482d13001` / 10,102 | `APPROVE_P0_0_P1_0_P2_0` |
| `remaining-generator-supply-chain-review` / `generator_supply_profile` | `docs/plan/p1/g-contract-generator-supply-profile-v2-independent-review-20260824.md` | `3607d4cdf9e4a9680c2af91f860385727b90ad52e17288ffc2ff40d6d04e0cec` / 12,963 | `APPROVE_P0_0_P1_0_P2_0` |

The committed review blobs, live file bytes, tuple SHA fields, normalized
verdicts, and zero P0/P1/P2 findings all agree. The review child has exactly
one parent, and the tuple rejects either review commit equalling the reviewed
candidate, a review path present in the candidate, or a tuple/output/final-
review path being used as its own review authority.

## Authority and digest reproduction

The detached source remains byte-current at SHA-256
`99b688b39c2819a02e6d974675c72317285a7545b073bd1cb071a081fcff2f45`
and 1,900 bytes. Independent RFC-8785-subset canonicalization reproduced all
declared detached and bound-authority semantic digests:

| Authority | Reproduced digest |
| --------- | ----------------- |
| detached source | `sha256:d8b2ba5dabe07a663dcb8c576ef9fc2ab21377f530a9d355bb992eb35d75b85d` |
| complete review tuple | `sha256:036c7be10799574296c2890384e242525743a986557c87676075e4e6b6ea49a9` |
| complete bindings | `sha256:7e0890d0727908dafa81cb176b906c1ed3be6b4c5fbf10a06f18d888d9358787` |
| detached binding registry | `sha256:f5d91de65e84e967fe898212c3c08d484cdeb6f43cea9527d25673da203616aa` |
| closure-v3 profile | `sha256:bdac321b602c0fdd350a1af5a214d881804e6648f69e4d730cbb8f9a82c36f5a` |
| closure-v3 registry | `sha256:eaaebbe39a52c63bde0ab99110670607ca7596fd394cdff6007177dab01b31bc` |
| supply-v2 profile | `sha256:cee9bc94a9fce6dd30a21fa832ad264b6b01e2ad8dab1313f9d04d689faaba87` |
| supply-v2 registry | `sha256:88b9317ea08e4a653f1403e6a334b58cd4fa8b31ecc56164916085959f1954d2` |

The tuple's bound file identities also reproduce exactly: closure-v3 is
SHA-256 `e8384fb25f3828dfafeecf0040110df3a51cd64ce5877e966ecec12769099bf4`
at 14,215 bytes, and supply-v2 is SHA-256
`e52a6e24e2903ee403abb5b7252f6472437d8cddcb78bcaf8e2699c5ab393252`
at 9,362 bytes.

The successor lock uses its separately specified domain plus NUL plus
insertion-order JSON serialization. Recomputing that exact algorithm produced
lock digest
`sha256:b0d08160c2c7cf35f91940fc4b644160d715acd3d6c5796ea456b2c005dcfa4f`.
An initial independent probe incorrectly applied the detached registry's
canonical JSON algorithm to the lock as well; after correcting the algorithm
to the lock's versioned implementation, the declared digest matched. This was
a review-probe algorithm error, not candidate drift or a suppressed test
failure.

## Effective missing derivation and canonical immutability

Canonical closure-v3 remains unchanged and continues to derive exactly one
canonical missing criterion from its seven ordered statuses:

```text
remaining-generator-supply-chain-review
```

Its first six criteria remain `SATISFIED_CANDIDATE`; the supply review
criterion remains `REVIEW_PENDING`. Slice G does not delete that item or
rewrite closure-v3.

The detached registry instead derives a separate
`REVIEW_BOUND_SATISFIED_CANDIDATE` view. Its two ordered criteria bindings are
one-to-one with the tuple's closure-v3 and supply-v2 review slots, use their
exact review SHA-256 values, and each produce only `SATISFIED_CANDIDATE`.
Because those bindings cover the complete declared pair, the effective view
derives `missing=[]`. This is a candidate classification inside the detached
consumer, not a phase record, deployment approval, aggregate signature, or
Gate transition.

## State machine, lock, and fail-closed behavior

The checked candidate is exactly:

- binding state: `COMPLETE_TUPLE_OUTPUT_CURRENT`;
- lock status: `SUCCESSOR_ASSEMBLED_REVIEW_BOUND`;
- tuple, registry, and lock: `notGateClosure=true`;
- tuple, effective view, source, and lock: `ALL_GATES_OPEN`.

The focused state-machine tests prove that tuple-present/output-absent remains
`COMPLETE_TUPLE_READY_TO_WRITE` and requires an explicit write; output without
tuple, partial groups, unknown fields, reordered slots, stale output, wrong
Git objects or parentage, review/authority digest drift, schema drift,
masquerading or skeletal authorities, self-review, symlinked paths, ABA input
replacement, and Gate/bootstrap/core-output source mutations all fail closed.
The successor lock independently refuses `COMPLETE_TUPLE_READY_TO_WRITE`,
stale binding output, changed exclusion/boundary state, unsafe lock
destinations, and core-output replacement.

This review ran check-only modes against the candidate. It did not invoke the
binding writer or successor-lock writer.

## Non-bootstrap and core-output separation

The source fixes `bootstrapDiscovery=FORBIDDEN` and
`coreReplayOutput=FORBIDDEN`. The tuple, registry, and this final review are
the exact final three members of the ordered 16-path successor projection
exclusion list. None is one of the 49 sorted core generator outputs, and all
49 core-output Git blobs are identical to the Slice F parent.

The detached files live under the separate tooling registry, outside the
contract bootstrap roots. No detached binding path or identity appears in the
contract tree outside the successor generation lock, nor in generated SDK or
service manifests. The canonical closure-v3 registry remains a core output;
the detached registry never identifies itself as the canonical closure
profile and cannot feed back into bootstrap discovery or the replayed core
output set.

## Focused verification

All executable checks used the fixed Bun 1.3.14 supply and its fixed dependency
tree. A transient root `node_modules` symlink was used only for module
resolution, removed by the command trap, and verified absent afterward. No
dependency installation occurred.

| Check | Result |
| ----- | -----: |
| detached binding plus successor DAG focused tests | 24/24 PASS |
| successor-lock state/output/atomicity focused tests | 6/6 PASS; 21 unrelated historical-lock cases filtered |
| binding source and complete tuple/output current checks | `CURRENT` / `COMPLETE_TUPLE_OUTPUT_CURRENT` |
| closure-v3 and supply-v2 current checks | PASS / `ASSEMBLED_PROFILE_CURRENT` |
| successor generation-lock current check | PASS |
| independent raw SHA/size/newline and semantic digest reproduction | PASS |
| fixed Git object/parent/tree/diff/review-blob/path checks | PASS |
| 49 core-output parent/candidate Git-blob comparison | 49/49 unchanged |
| exact candidate `git diff --check` and host-path hygiene scan | PASS |

No broad Bun suite, Go or migration test, native replay, SSH, production
writer, database, HTTP/P2/provider operation, deployment, publication,
release, main merge, or Gate action was executed by this review.

## Review boundary

The existing Slice G detached-consumer bytes are approved with zero P0/P1/P2
findings. The effective `missing=[]` claim is valid only as the versioned,
review-bound candidate view described above. Canonical closure-v3 remains
immutable, the final review remains detached from the candidate it reviews,
and every Gate remains OPEN.
