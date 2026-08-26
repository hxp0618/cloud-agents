# G-CONTRACT generator-supply profile v3 independent review

Date: 2026-08-26

Review subject: the fixed Slice E assembled generator-supply-v3 candidate on
\`codex/cloud-agents-platform-p0\`. This is a superseding D-053 review: the
reviewer evaluated the fresh Slice C projection, fresh native Slice D replay,
and the late-bound Slice E profile/lock as one immutable candidate. The
review record is intentionally a direct child of the candidate and is not an
input to the projection or replay.

## Verdict

`APPROVE - P0=0 / P1=0 / P2=0`

Normalized verdict: \`APPROVE_P0_0_P1_0_P2_0\`.

This verdict approves only the fixed supply-v3 candidate and its predeclared
Slice F review tuple. It is not a Gate closure and does not authorize a
production database write, HTTP/P2/provider effect, deployment, publication,
release, signing, or any other external side effect. \`notGateClosure=true\` and
\`gateStatus=ALL_GATES_OPEN\` remain in force. Linux arm64 remains
\`NOT_CLAIMED\`.

## Fixed candidate and superseding lineage

- candidate commit: \`89458237b5dbb3e8f446d49302b6d2f4c7c68154\`;
- immediate parent (assembled-profile commit):
  \`44b7378775d47624a010f7718a0510736e34cefe\`;
- candidate tree: \`b4cae7e48a26f25ce016e452f40b90b77bfad413\`;
- parent-to-candidate full-index binary diff SHA-256:
  \`60358da0fd4a12c68dd76632e704ae6641ff73f890418394f33a49041159f1cc\`;
- the candidate has one parent and the worktree was clean at review time;
- Slice C projection commit: \`c7e0265c6d0550c64187c6164b078342746b1a10\`;
- fresh Slice D replay commit: \`b37bbc5fccaae05a98653b24284ee213f648bd97\`;
- the stale D-053 repair evidence was superseded by commit
  \`97f882dba5d20b52ddace7dfcfa072d891d21916\`, while the repaired writer and
  review authority remain in the preserved lineage; and
- the immutable v2 predecessor is commit
  \`16275f6cbf390c343a9ac00f9193e75eaad0094e\`, tree
  \`ca595b8e1258a8b78c4da3a545b2a31d8f62b531\`.

The C-to-E change domain is exactly ten late-bound paths:

\`\`\`
contracts/generation.lock.json
tools/generator-supply/v3/evidence-manifest.json
tools/generator-supply/v3/profile.json
tools/generator-supply/v3/evidence/replay.json
tools/generator-supply/v3/evidence/replay/darwin-a.json
tools/generator-supply/v3/evidence/replay/darwin-b.json
tools/generator-supply/v3/evidence/replay/darwin-isolation.json
tools/generator-supply/v3/evidence/replay/linux-a.json
tools/generator-supply/v3/evidence/replay/linux-b.json
tools/generator-supply/v3/evidence/replay/linux-isolation.json
\`\`\`

No pre-replay source, v1 material, predecessor review, or unrelated path was
changed. The review path itself is one of the exact predeclared exclusions and
was absent from the candidate, so the candidate does not self-review.

## Fresh projection and native replay

The projection receipt is byte-identical to the fresh Slice C object:

- projected tree:
  \`2c062cba6c97835b4d955403c6b8bc1059c5c996\`;
- archive SHA-256:
  \`sha256:1816bdf3636fd444c5e9ac99b6d5ac0e0f6a60c958a66e16931fb71030728c62\`;
- archive size: \`48,496,640\` bytes;
- archive-member manifest SHA-256:
  \`5499f1c5713487c25c4ed5c72ae47f1894e137d5ad6779ba2ad0217e295a779b\`;
- input-tree manifest SHA-256:
  \`e4a642a3e6e46b6779ba82debd22480f60d7484e06e5398b782f7cc4a10abe0f\`;
- archive members: \`1680\`; regular input files: \`1484\`; exact exclusions: \`17\`;
- projection receipt SHA-256:
  \`sha256:e95da370bb30338de29abde4c3889583f9f5edcd60d061dc5136907976e87aa0\`.

Fresh Darwin and Linux A/B native replay receipts bind the same projection,
the same exact-49 output candidate manifest
\`sha256:bedb5d26301f627393a107afda9863899dae09097993ae7df8d0ad06018a282e9\`,
and report \`candidateOutputsEqual=true\` with
\`nonAllowlistedChanges=0\`. The installed summary reports
\`DUAL_PLATFORM_TWO_ARCHIVES_EXACT_REPLAY_VERIFIED\` and is bound to all four
run receipts plus both isolation receipts. The raw receipt digests are:

| Receipt | SHA-256 | Bytes |
| --- | --- | ---: |
| Darwin A | \`3524f6c55bd192a2899671c7e1d2c2e9defeb034903f4a126c27286a4705e6d3\` | 3054 |
| Darwin B | \`2784a2d248c7d3e8a615c71d12efb181ad9ccb1d7b3aad191a6516d3bb5866c7\` | 3054 |
| Darwin isolation | \`4abf666ebbeb9c2ea233409c0877797855c747e8af6d9511a1679b726392faa9\` | 7697 |
| Linux A | \`db9483483e442a836614c68b252bc60d03b0cfab8916ef000d608189f57f6233\` | 3055 |
| Linux B | \`b3191909e0423b9eb4f892c36bfe1f996441c0cd68b794903c4a95f7694ceae0\` | 3055 |
| Linux isolation | \`f5a78a4bab1d528ed88bfbeebf3b15aff9ae0f13319440caa31784445e76c06b\` | 11459 |
| Derived replay summary | \`4697d6d602276a43b78d344f548243a5c11656fddbb7b55e4904764e2160c28c\` | 2127 |

The replay authority is versioned and fixed: wrapper
\`sha256:9acfc4163fead4dace517c069b8b0e74aaacc859e8cdd2dee17b84182d0be990\`,
runner \`sha256:759c3b6578d3ec5c818c9ce5bc92b2d560727363e32c833494823a296ef555fd\`,
path helper \`sha256:4cde1599b7e909ef0070b81090d40c2e2c7c1c43af64cebd3c53391489a6fccf\`,
and archive inspector
\`sha256:db932a113dda469367f25c71b56ff28ee8f2245821fceb840c49340ef6c10f31\`.
Darwin records deny network access and use read-only supply/archive inputs;
Linux records fresh rootfs/tmpfs, read-only inputs, network/mount/PID
namespaces, uid/gid 65534, no supplementary groups/capabilities, and
\`NoNewPrivs\`. These are bounded receipt claims, not a claim of full
distribution coverage or Gate completion.

## Assembled profile and generation lock

The v3 profile and evidence manifest are byte-stable assembled outputs. Their
authorities are:

- profile digest:
  \`sha256:b6e63cfaa0d30df553bf439923469762754db0718f5f1b5bc9ac32952f2fc07b\`;
- registry digest:
  \`sha256:4b0a127899997745b6b08a3cc5f0991bed7dff122c659afb760275be9ca325ba\`;
- source digest:
  \`sha256:77070adf213107b72012202cf44c55f93f49df96f4b6bd1a1cc2b28b295dbba3\`;
- artifact-set digest:
  \`sha256:25f6412209fe87adec592de7bd1c9d9660745406def02e7d422273e04f5cd806\`;
- evidence-manifest digest:
  \`sha256:b5ce6d55c934a0639827e5da77ac81de0fa715ccedc88a8f476237172a671d1f\`;
- profile evidence state: \`ASSEMBLED_LATE_BOUND\`;
- profile status: \`REPLAY_VERIFIED_REVIEW_PENDING\`.

The v3 lock is \`ASSEMBLED\`, current, and preserves the exact v2 predecessor.
It binds 49 core output records, the projection receipt, profile and registry
digests, and the 17-path exclusion list. Lock digest:
\`sha256:ca789f000cfc983c0e6965192340d296c0db5e2032ccabf5596b3f07844e1dd4\`.
The v2 predecessor lock remains immutable (SHA-256
\`sha256:de62a85390e58736a5fef8272878b821415b4180948437e54edb8628e005ff53\`,
17,377 bytes).

## Independent checks and limitations

The following checks were independently rerun against the fixed candidate:

- assembly and source checks: \`ASSEMBLED_PROFILE_CURRENT\`;
- assembled lock check: \`ASSEMBLED\` current;
- phase-state check before this review: \`PRE_CANDIDATE_ABSENT\`;
- replay/profile focused tests: \`26/26 PASS\`;
- exact parent diff and \`git diff --check\`: pass;
- no forbidden production, HTTP/P2/provider, deployment, publication, or
  release action: pass by operation boundary.

The first Linux attempt with a mode-0700 transferred supply root is retained
only as non-admissible diagnostic history. It produced no run receipt and was
not mixed into the formal task. The final fresh Linux replay used the pinned
read-only supply root with the required executable directory mode; all final
receipts above were generated by that rerun. No native replay, SSH operation,
database write, broad unrelated test suite, or Gate action is implied by this
record.

## Progression boundary

This review authorizes consumption of the exact v3 supply profile, manifest,
projection, replay receipts, and assembled lock by the predeclared G-CONTRACT
phase state machine. It does not create the R5 phase record, review-binding
tuple, registry, or terminal review. Those objects require their own fixed
candidate and independent review. All aggregate Gates remain OPEN.
