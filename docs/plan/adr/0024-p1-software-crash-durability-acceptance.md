# ADR-0024: P1 software-crash durability acceptance boundary

- Status: Accepted by explicit owner approval on 2026-08-23; acceptance terminology amended by explicit owner approval
  on 2026-08-24
- Scope: P1 durability evidence and Platform RC engineering acceptance
- Depends on: ADR-0010, the physical power-loss entry audit, and the bare-metal host-crash simulation

This decision changes the required evidence boundary only. It does not declare any existing filesystem slice or Gate
complete. Under the owner-defined project acceptance terminology, an ordinary `poweroff`/`reboot` recovery run counts
as “掉电恢复”; the execution record must still identify that exact mechanism as clean shutdown/restart and must not
relabel it as abrupt-crash consistency, BMC hard-off, physical power removal, or hardware-cache-loss evidence.

## Context

The P1 aggregate Gate gap audit originally treated physical storage-controller reset, removal of host power, volatile
device-cache loss, a dedicated destructive DUT, an independent test disk, and an out-of-band hard-off/hard-on controller
as one remaining prerequisite. The subsequent entry audit correctly refused to use reachable machines with active
workloads or no isolated disposable disk for destructive physical power removal.

The repository already contains bounded process-kill/reopen evidence on real ext4/XFS filesystems, isolated whole-QEMU
power-cycle and exact durability-barrier matrices, and one owner-authorized bare-metal Magic SysRq `b` run observed from
a second machine. The SysRq run rebooted without application sync, unmount, `Lease.Close`, or a clean shutdown, then
verified fresh-mount journal replay and the exact recovered object state.

The owner has explicitly approved using software shutdown/crash mechanisms instead of physical hard power removal and,
on 2026-08-24, further fixed the project acceptance terminology: normal `poweroff`/`reboot` counts as “掉电恢复”. Such
commands normally perform a clean shutdown/restart, including filesystem sync/unmount behavior, so their exact evidence
strength remains distinct from a no-sync primitive such as Magic SysRq `b` or an equivalently documented abrupt
kernel/host reset. Acceptance naming does not erase that mechanism distinction.

## Decision

For P1 Exit Gates and Platform RC engineering acceptance:

1. Physical removal of host or storage power, storage-controller reset, volatile hardware write-cache loss, a dedicated
   physical DUT/test disk, BMC hard-off, and independent hard-on recovery are no longer mandatory acceptance evidence.
2. Those physical scenarios remain unclaimed optional future hardening. Their absence is not by itself a P1 Gate or
   Platform RC blocker after this decision.
3. The accepted durability closure must instead bind all of the following:
   - the existing isolated ext4/XFS whole-QEMU power-cycle and fixed syscall-barrier matrices;
   - the existing real ext4/XFS process-kill/restart, trusted-mount, production-open, and replay evidence;
   - at least one owner-authorized bare-metal recovery run observed from another host, using either ordinary
     `poweroff`/`reboot` or a separately identified abrupt software-crash primitive;
   - an exact mechanism record that distinguishes clean shutdown/restart from no-sync crash and records whether
     application sync, filesystem unmount, clean shutdown, or `Lease.Close` occurred;
   - post-boot exact recovery/invariant verification and a bounded immutable execution record; and
   - a current-source phase record plus independent P0/P1/P2 review for every Gate that consumes this evidence.
4. Clean `poweroff`/`reboot` runs may satisfy the project-defined “掉电恢复” item. They must be described as clean
   shutdown/restart evidence and cannot be described as abrupt-crash, BMC hard-off, physical power-loss,
   SSD/controller cache-loss, or no-sync evidence.
5. The historical physical power-loss entry audit remains an accurate record of the environment and requirements at
   its fixed source. This decision supersedes only its physical-hardware prerequisite as a current acceptance blocker;
   it does not rewrite or invalidate that audit.

## Current evidence effect

The existing bare-metal host-crash record supplies one stronger no-sync candidate for the recovery item: ext4
`after-directory-fsync` object publication followed by Magic SysRq `b`, external `UP -> DOWN -> UP` observation,
fresh-mount journal recovery, exact `final`/44-byte/no-temp classification, and clean read-only filesystem checking.

That single scenario still does not provide XFS coverage, a complete bare-metal barrier matrix, live PostgreSQL,
logical backup/restore, filesystem-slice `Done`, current-source immutable phase records, or independent Gate review.
Consequently this ADR removes the physical-hardware prerequisite but does not close `G-DATA`, `G-AUTHORITY-P1`,
`G-SECURITY-P1`, any filesystem slice, Platform RC, or an aggregate Gate.

## Operational boundary

Future recovery runs must use an owner-authorized target and an independent observer, establish the exact target and
cleanup scope before shutdown/crash, avoid production/shared data, and retain a bounded record naming the exact
mechanism. This decision does not
authorize destructive operation on production or shared machines, production database writes, HTTP/P2/provider side
effects, deployment, publication, release, main-branch merge, or Gate closure.

## Explicit non-claims

- No physical power interruption, controller reset, SSD cache-loss, or BMC hard-off result is claimed.
- An ordinary `poweroff`/`reboot` result may count as project-defined “掉电恢复”, but is not promoted to abrupt-crash,
  physical power-loss, BMC hard-off, or hardware-cache-loss evidence.
- No existing local/QEMU/bare-metal record becomes a reviewer-signed immutable Gate record by this decision alone.
- All previously stated production, deployment, publication, release, and Gate prohibitions remain in force.
