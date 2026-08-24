# Software-crash durability acceptance independent review — 2026-08-23

- Verdict: `APPROVE`
- Severity: `P0=0 / P1=0 / P2=0`
- Fixed candidate: `0d64d9b4d31655f4d36fc6c2b6662e343ef61532`
- Candidate tree: `2a5852e0d43a96c8d4baf762189f1f85203cec7f`
- Candidate parent: `75b93c10952c99d07a6a6c3e3466ecbd245ac178`
- Candidate branch: `codex/cloud-agents-p1-software-crash-acceptance-decision-20260823`
- Decision: [`ADR-0024`](../adr/0024-p1-software-crash-durability-acceptance.md)
- Decision SHA-256: `597f0d9881aabe44e9d67876ae81d83808a18c3725f5c7ac66279bcda53e0bd0`

This is an independent, read-only review of D-048/ADR-0024 and its plan indexes. Approval applies only to the fixed
four-document candidate above. It does not approve a crash execution, alter historical evidence, authorize production
or shared-machine operations, merge the candidate, or close a filesystem slice, Platform RC, or any Gate.

## Fixed identity and scope

The candidate branch was clean, matched its remote branch exactly, and was `0/0` relative to its upstream at review
time. HEAD, tree, parent, ADR hash, and the exact four-path diff matched the supplied fixed identity. The only changes
are the new ADR and updates to the plan root, P1 README, and status tracker.

Reviewed document hashes:

- plan root: `fef965f10b8342b5591bb36e0c81d6a6e5c1e1ccb278606d533ca65f740ab4ef`;
- P1 README: `77a80b16ae38ea6b8536a86a09cf7aad61e600f8b6758f5d256c6aa220b8a072`;
- status tracker: `32d1ab14e91da398b7d0d803afb62665df89bd5a421823ccf8d288d7156a7aee`.

The referenced historical gap audit, its independent review, the physical-power entry audit, and the bare-metal
host-crash record were byte-identical to the parent candidate. Their SHA-256 values remain respectively
`5d745aee5f2188da81b639c5c45d5b93da2c8271e5c420cbe04baf9975159462`,
`00bf3a0b06fe92676c64709327c4c094c19e9ceefb84d453bbaeca739432706b`,
`9e4867ba436388751c2f07b9ab30f065435c192699dc37e2cfe9c5c02b2595b7`, and
`c4df9602872ac62c303d681782f1f6a35c544e53df3daa2a7fdb557212d283eb`.

## Decision semantics

The review confirms that explicit owner approval changes only the P1/Platform-RC engineering acceptance boundary:

1. physical host or storage power removal, controller reset, volatile hardware-cache loss, dedicated destructive
   DUT/test disk, BMC hard-off, and independent hard-on recovery are no longer mandatory blockers;
2. those scenarios remain explicitly unclaimed optional future hardening, and no existing record is relabeled as a
   physical power-loss result;
3. clean `poweroff` or `reboot` remains lifecycle smoke only and cannot satisfy abrupt-crash evidence;
4. accepted closure still requires the existing ext4/XFS process-kill, production-open/replay, whole-QEMU, and fixed
   barrier evidence; at least one owner-authorized, externally observed bare-metal crash with no application sync,
   unmount, clean shutdown, or `Lease.Close`; exact post-boot recovery/invariant verification and a bounded immutable
   execution record; and a current-source phase record plus independent P0/P1/P2 Gate review; and
5. the single ext4 SysRq `b` record is only one named candidate. It has no physical/cache-loss, XFS, complete
   bare-metal barrier, live PostgreSQL, logical backup/restore, filesystem-Done, or Gate-closure effect.

The README and tracker updates preserve the same distinction. They describe the aggregate gap audit and physical
entry audit as accurate fixed-source history, supersede only the old mandatory-physical premise, and keep remaining
matrices, current-source immutable records, live PostgreSQL, backup/restore, and independent closure open.

## Authority and Gate boundary

Every affected Gate remains `IN PROGRESS`; no filesystem slice, Platform RC, or aggregate Gate is marked complete.
The candidate contains no code, schema, runtime, database, HTTP/P2/provider, deployment, publication, or release
change. ADR-0024 also expressly requires owner-authorized isolated targets and independent observers for any future
crash run and denies destructive production/shared-machine use.

## Checks and non-claims

Fresh independent checks on the fixed candidate:

- HEAD/tree/parent, exact four-file scope, supplied ADR SHA-256, clean worktree, upstream `0/0`, and remote exact:
  PASS;
- semantic cross-check of the ADR, plan root, P1 README, status tracker, historical audit boundaries, closure
  prerequisites, explicit non-claims, and every affected Gate status: PASS;
- historical audit and host-crash records unchanged from the parent: PASS;
- exact oxfmt 0.62.0 check on all four candidate documents, named local links, and candidate-range `git diff --check`:
  PASS;
- Gitleaks 8.30.1 scan of the single candidate commit, approximately `57.47 KB`: PASS, no leaks.

No remote host, crash primitive, power operation, destructive action, Go or migration test, race, live PostgreSQL,
production database, HTTP/P2/provider surface, deployment, publication, release, merge, or Gate action was run or is
claimed by this review.

## Final verdict

`APPROVE, P0=0/P1=0/P2=0` for fixed candidate
`0d64d9b4d31655f4d36fc6c2b6662e343ef61532` only. The decision may be used as the current P1/RC engineering
acceptance rule under existing owner approval, but it supplies no missing durability execution record and grants no
production or Gate authority.

## Superseding owner decision — 2026-08-24

This review verdict remains the historical verdict for its fixed candidate and is not rewritten. A later explicit
owner decision supersedes only item 3 of the reviewed decision semantics: for current project acceptance, ordinary
`poweroff`/`reboot` counts as “掉电恢复”. Any resulting record must still name the exact mechanism as clean
shutdown/restart and must not claim abrupt crash, BMC hard-off, physical power removal, SSD/controller cache loss, or
no-sync behavior. The later decision does not close a filesystem slice, Platform RC, or any Gate.
