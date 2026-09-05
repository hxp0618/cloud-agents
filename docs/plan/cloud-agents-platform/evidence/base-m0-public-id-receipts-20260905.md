# BASE-M0 public identifier receipt compatibility — 2026-09-05

Source base `30336340`, branch `codex/cloud-agents-platform-p0`, with only the
OpenSandbox client/tests and candidate harness changes described here. Unrelated
`.gitignore`, `go.work.sum` and `docs/img.png` remain untouched.

The common Identifier schema permits 128 ASCII characters and internal `~`.
The prior candidate receipt implementation incorrectly limited product IDs to
Kubernetes label values. Receipt encoding v2 now hashes each exact identifier
with SHA-256 and carries both 32-hex halves. It never truncates or normalizes IDs.
Discovery filters on both Sandbox digest halves; receipt validation also checks
tenant, project, Workspace, operation, generation, spec digest and encoding version.
This is identifier encoding, not encryption or tenant authorization.

This pre-product encoding replaces the earlier PoC encoding. It does not adopt
or migrate legacy physical objects. The candidate harness refuses to start with
existing OpenSandbox resources; production upgrade/adoption remains unimplemented.
No legacy Agent/Lease/Profile contract, actuator or User/Admin UI was changed.

## Executed checks

Go 1.26.6, Node 24.18.1; OrbStack Docker with the same fixed server v0.2.2,
execd v1.0.21 and Node image digests recorded in the JSON evidence.

```sh
go test -race ./services/control-plane/internal/opensandbox
go vet ./services/control-plane/internal/opensandbox
node scripts/test-base-opensandbox-docker.mjs /tmp/cloud-agents-base-public-id-receipts
```

- Unit/race and vet passed. Added maximum-length/tilde, invalid identifier,
  exact-case preservation, full-digest discovery filter and receipt version guards.
- The real candidate accepted the encoded 128-character tenant ID (`A`, 126 `~`,
  `Z`); Go discovery and guarded cleanup passed all five harness invocation phases.
- All 14 candidate checks passed, including duplicate rejection, stale generation
  rejection, compute deletion/replay, retained-volume rebuild/Files checksum and
  failed-entrypoint cleanup. [Actual results](base-m0-public-id-receipts-20260905.json).
- The harness removed only its new disposable containers and volume; zero owned
  resources remain. Existing containers and images were preserved. Test volume
  contents were deliberately disposable and were removed at final teardown.

Ponytail: reused standard SHA-256 and the existing client/harness; no new dependency
or coordination framework. This fixes a prerequisite, not the promised persistent
Workspace/Sandbox vertical slice. No API/SDK/Controller/Admin lifecycle, restart
recovery, single-writer fencing or BASE acceptance is claimed. Next work remains in 06.
