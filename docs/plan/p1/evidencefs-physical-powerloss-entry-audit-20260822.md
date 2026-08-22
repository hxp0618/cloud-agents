# P1 evidencefs physical controller/host power-loss entry audit — 2026-08-22

- Status: **ENTRY AUDIT COMPLETE — BLOCKED ON DEDICATED HARDWARE AND EXTERNAL OUT-OF-BAND CONTROL; GATES OPEN**
- Audited source: `d6ec6c848d77e5b67c6602f53816d89bc261f827`
- Source tree: `9d2ed016230c7254b04bc8186add2825f4011de4`
- Control-plane subtree: `c78ffc27c88b0f50871795a281669b7b2ef9bd27`
- Scope: physical controller/host power-loss execution entry only
- Independent Gate reviewer: **not assigned**

This record is a read-only feasibility and safety audit. It is not a physical power-loss execution
record, an implementation result, a filesystem-slice Done record, or a Gate review. No host was
powered off, rebooted, unmounted, reconfigured, or written by this audit.

## 1. Existing evidence and remaining boundary

The fixed source already contains:

- the [isolated QEMU ext4/XFS power-loss matrix](evidencefs-qemu-powerloss-matrix-20260816.md),
  including abrupt whole-guest termination and fresh-guest replay;
- the object, registration, generation header, activation, append, rotation, and repair QEMU
  per-barrier records indexed by the [P1 execution record](README.md);
- the [real Linux ext4/XFS generation matrix](evidencefs-linux-generation-matrix-20260816.md);
- the [required-syscall probe](evidencefs-required-syscall-probe-20260816.md);
- the [trusted-mount authority](evidencefs-trusted-mount-authority-20260817.md); and
- the public production-opened [migration EvidenceSink matrix](migration-production-evidence-sink-20260817.md).

Those records intentionally stop before physical storage-controller cache loss or bare-metal host
power removal. [ADR-0010](../adr/0010-p1-postgres-projection-contract.md) therefore still requires
physical controller/host power-loss evidence and reviewer-signed filesystem Done; QEMU process
termination, process kill, clean reboot, loop devices, or the scoped clean-run production-opened
matrix cannot substitute for it.

## 2. Sanitized read-only feasibility observations

One authorized reachable physical candidate was inspected with read-only host, block-topology,
mount, service, and management-interface queries. Durable project records deliberately omit its IP,
hostname, hardware model, disk serial, management address, and all other device identifiers.

| Observation                                                                  | Entry consequence                                                                                  |
| ---------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------- |
| Bare-metal x86_64 host; no hypervisor reported                               | A physical execution target is technically possible                                                |
| Direct-attached SATA SSD advertises write-back caching                       | A real controller/device-cache loss boundary exists, unlike the QEMU `cache=none` record           |
| Local KCS IPMI 2.0 device/interface and kernel support are present           | Hardware management may exist, but a DUT-local interface is not an independent external controller |
| Active K3s workloads and a local PostgreSQL 15 service are present           | The host is not a dedicated expendable DUT and must not be power-cycled                            |
| Root filesystem is on the active local storage topology                      | The root device must never be used as the evidence target                                          |
| No dedicated disposable block device was proven                              | No safe filesystem target exists                                                                   |
| No second control host with tested off/on, console, and recovery path exists | Recovery from a hard-off event is not proven                                                       |

Bounded read-only connection attempts to two other authorized physical candidates did not establish
a session. An unreachable host is not negative power-loss evidence and was not modified.

No package was installed; no management command, chassis action, SysRq, reboot, unmount, filesystem
creation, cache-policy change, service stop, database operation, or test-fixture write was attempted.

## 3. Safety disposition

The reachable host is rejected as a destructive test target. A hard power cut could interrupt active
workloads or database state, and the audit has neither a dedicated evidence device nor an external
controller proven able to restore the DUT after power removal. Presence of a local management device
does not satisfy that recovery requirement.

The following actions remain prohibited on the observed host:

- installing a BMC client or changing BMC/network configuration;
- issuing chassis off/reset/on, reboot, SysRq, device reset, cache flush, cache-policy, or mount commands;
- stopping K3s, PostgreSQL, or another workload to manufacture an apparent maintenance window;
- writing, formatting, repartitioning, unmounting, or fault-injecting the root or any existing device;
- copying the QEMU harness onto a physical device and treating one successful cut as ADR-0010 Done.

## 4. Exact entry conditions for a physical run

A future execution may start only after one fixed entry record proves every row below.

| Entry condition          | Required proof before the first mutation                                                                                                   |
| ------------------------ | ------------------------------------------------------------------------------------------------------------------------------------------ |
| Dedicated DUT            | Bare-metal maintenance target with no active workload, database, user data, or production dependency                                       |
| Independent controller   | A separate control host can query, hard-off, hard-on, observe console/boot, and recover the DUT while the DUT itself is unpowered          |
| Disposable storage       | Dedicated direct-attached block device or partition, explicitly identified as expendable and proven not to contain or back the DUT root    |
| Device isolation         | Exact preflight rejects mounted children, holders, swap, LVM/RAID membership, unexpected partitions, identity drift, or ambiguous backing  |
| Filesystem matrix        | Fresh ext4 and XFS filesystems on the disposable device; exact mkfs/mount options, kernel, controller, firmware, and cache policy recorded |
| Trusted mount            | Root-owned trusted-mount provisioning plus non-root runner; production `Open` and fresh reopen retain the existing fail-closed checks      |
| Fixed source and harness | Exact commit/tree/subtree, generated artifacts, toolchain, binaries/scripts, package/image inputs, and SHA-256 manifest                    |
| Fixed barrier scope      | Named ADR-0010 object/index/journal/registration/header/activation/append/rotation/repair barriers and accepted fresh-reopen states        |
| Hard-cut semantics       | Power removal is issued externally without DUT sync, unmount, service stop, clean shutdown, or command-side success inference              |
| Recovery path            | Independent power-on and console/SSH recovery timeouts are rehearsed before mutation; failure preserves evidence and leaves no retry loop  |
| Evidence and cleanup     | Pre/post topology, boot identity, mount identity, marker/barrier, filesystem check, exact replay, rejected states, cleanup, and residue    |
| Independent review       | Fixed candidate and immutable execution artifacts receive a separate P0/P1/P2 review before filesystem Done or any Gate claim              |

A staged physical run may cover a smaller explicitly named subset, but it must retain the remaining
rows as open. Only complete coverage of the ADR-defined physical scope can support filesystem-slice
Done; a representative cut, filesystem, barrier, or device cannot be generalized.

## 5. Abort and recovery rules

The executor must abort before any write or power action if the target identity, source, controller,
cache policy, service inventory, device isolation, external power-on path, or reviewer-approved
matrix differs from the fixed entry record.

After mutation begins:

1. only the external control host may issue the fixed hard-off/hard-on sequence;
2. an unknown controller response or recovery timeout stops the matrix and preserves the device;
3. no second destructive attempt may be used to turn an unknown first result into success;
4. fresh boot/reopen classification must consume the persisted state before cleanup;
5. unexpected filesystem/device state is failure evidence, not a reason to repair in place; and
6. cleanup may run only after evidence capture and must prove the disposable target and harness
   residues are gone without touching unrelated devices.

Sensitive controller credentials, host addresses, serials, inventory identifiers, and console logs
containing secrets belong only in an access-controlled execution artifact store. The repository may
bind their redacted manifests and digests, not the values themselves.

## 6. Current blocker and non-claims

The current blocker is environmental: there is no dedicated expendable physical DUT/storage target
and no independently tested out-of-band hard-off/hard-on recovery path. It is not a failed durability
test and cannot be counted as negative evidence against ext4, XFS, the storage device, or evidencefs.

This audit does **not** authorize or prove:

- any physical power action, production database write, deployment, publication, or release;
- physical controller/cache durability, bare-metal recovery, or the full ADR-0010 barrier matrix;
- reviewer-signed filesystem Done, `G-DATA`, `G-AUTHORITY-P1`, `G-SECURITY-P1`, or another Gate;
- Platform RC, Beta, GA, or a change to any existing `IN PROGRESS`/`OPEN` status.

Safe local and remote alternatives are exhausted for this specific boundary: the existing QEMU,
real ext4/XFS, trusted-mount, and production-opened evidence was reviewed, and authorized physical
candidates were inspected only within bounded read-only limits. Until the entry conditions above are
met, physical execution remains blocked while other independent local implementation work may continue.
