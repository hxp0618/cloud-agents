# P0 Linux amd64 validation

- Status: PASS
- Observed at: 2026-08-10 Asia/Shanghai
- Profile: operator-selected primary Linux validation host
- OS: Debian 12, Linux 6.1.0-15-amd64, x86_64
- Source: `585af58c3e685e91107032f9c2e2f366c9d2119a`
- Source tree: `992bfd9244f719533c6beb4efe3d357e11272931`
- Node: `24.13.1`
- Node archive SHA-256: `30215f90ea3cd04dfbc06e762c021393fa173a1d392974298bbc871a8e461089`
- Bun: `1.3.14` baseline x64 build
- Bun archive SHA-256: `a063908ae08b7852ca10939bbdc6ceed3ddabce8fb9402dce83d65d73b36e6c7`
- Install: frozen lockfile, scripts disabled, network concurrency 1

The ordinary Bun x64 build was rejected by this older CPU with `Illegal instruction`; the official same-version
baseline build passed its published digest and was used for every result below. This is a toolchain selection finding,
not an application waiver.

## Results

| Check                                 | Result                  |
| ------------------------------------- | ----------------------- |
| frozen `bun install --ignore-scripts` | PASS, 402 packages      |
| Protocol 2.2/2.3 corpus               | PASS, 12/12             |
| testkit transcript corpus             | PASS, 5/5               |
| protocol typecheck                    | PASS                    |
| testkit typecheck                     | PASS                    |
| repository format check               | PASS                    |
| repository lint                       | PASS, 0 warnings/errors |
| worktree clean after validation       | PASS                    |

## Reproduce inside the isolated Linux checkout

```bash
export PATH=/opt/cloud-agents-toolchains/node-v24.13.1-linux-x64/bin:/opt/cloud-agents-toolchains/bun-v1.3.14/bin:$PATH
cd /root/cloud-agents-p0
bun install --frozen-lockfile --ignore-scripts --network-concurrency 1
bun run --filter @synara/cloud-agent-protocol test
bun run --filter @synara/cloud-agent-testkit test
bun run --filter @synara/cloud-agent-protocol typecheck
bun run --filter @synara/cloud-agent-testkit typecheck
bun run fmt:check
bun run lint
```

This record proves the committed P0 corpus on Linux amd64. It does not prove authenticated Provider behavior,
production deployment, or any Platform P1 implementation.
