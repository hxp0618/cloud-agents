# P1-A3 Proto SDK toolchain and dependency review - 2026-08-21

- Status: **IMPLEMENTED CANDIDATE - INDEPENDENT REVIEW PENDING**
- Scope: generated Proto descriptor, Go SDK, TypeScript SDK, local conformance, and fresh consumer closure
- Authority: [`sdk-identity-closure-entry-20260820.md`](../sdk-identity-closure-entry-20260820.md), Slice C
- Boundary: non-Gate evidence only; no publication, release, deployment, production database write, HTTP route,
  P2/provider behavior, worker side effect, adapter side effect, or Gate closure is authorized

## Decision

This slice uses exact, checked-in source/profile inputs and pinned generator/runtime dependencies. The generated
descriptor and language outputs are derived artifacts; they are not a second Proto authority. The profile freezes
`protocolbuffers/protoc v35.1`, `protoc-gen-go v1.36.12`, `protoc-gen-connect-go v1.20.0`, and
`@bufbuild/protoc-gen-es v2.14.0`. The generator rejects toolchain version drift and compares every generated output
against its checked-in bytes.

The production dependency closure is intentionally separate from the test/tool closure:

| Surface                   | Exact packages                                                                                                                                                           | License                                                                | Evidence                                                                                 |
| ------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------ | ---------------------------------------------------------------------- | ---------------------------------------------------------------------------------------- |
| Go runtime                | `connectrpc.com/connect v1.20.0`; `golang.org/x/text v0.39.0`; `google.golang.org/protobuf v1.36.12`                                                                     | Apache-2.0; BSD-3-Clause plus Go PATENTS; BSD-3-Clause plus Go PATENTS | `sdk/go/go.mod`, `sdk/go/go.sum`, generated Go manifest                                  |
| Go local tests            | `golang.org/x/net v0.55.0`; `golang.org/x/sys v0.45.0`; `google.golang.org/genproto/googleapis/rpc v0.0.0-20260526163538-3dc84a4a5aaa`; `google.golang.org/grpc v1.83.1` | BSD-3-Clause; BSD-3-Clause; Apache-2.0; Apache-2.0                     | `sdk/go/go.mod`, generated Go manifest                                                   |
| TypeScript runtime        | `@bufbuild/protobuf@2.14.0`; `@connectrpc/connect@2.1.2`                                                                                                                 | `(Apache-2.0 AND BSD-3-Clause)`; Apache-2.0                            | `sdk/typescript/package.json`, `bun.lock`, generated TypeScript manifest                 |
| TypeScript generator only | `@bufbuild/protoc-gen-es@2.14.0` and its generator closure                                                                                                               | Apache-2.0                                                             | `contracts/proto-generation.profile.json`, root `bun.lock`; not a SDK runtime dependency |

The Go module graph and generated-package dependency closure are fixed by the following sorted-manifest digests
(algorithm: SHA-256 of sorted newline records):

- `go list -m all`: `sha256:28d0a7479564f07e37e2d34fbac3f66f86509802536d6fea0d732c9936f9e045`;
- `go mod graph`: `sha256:48548d60d54ac6021620eaa8646d556ea4e9d465677278f7d2b3e5f98e4fa1f7`;
- `go list -deps ./gen/...`: `sha256:47fecfe61d104fa3f67905f7e71064283ff5a702f4be9e926accb16433eef691`.

The exact npm tarball integrities are recorded by `bun.lock` and are repeated here to make the runtime boundary
reviewable without treating the workspace as a consumer:

- `@bufbuild/protobuf@2.14.0`: `sha512-C3UGsiCwSprE2NKIIFA3hCDlpXTMCAXRZuEVp88L1GY36Y41+rYL5fryE+nOFhp4p4JPQvdV8PQ4DWgHgeTE+w==`;
- `@connectrpc/connect@2.1.2`: `sha512-MXkBijtcX09R10Eb6sFeIetc6w6746eio6xtfuyVOH7oQAacT1X0GzMIQFux6Qy8cq3W/T5qX5Bei8YbFtmRGA==`;
- generator-only `@bufbuild/protoc-gen-es@2.14.0`:
  `sha512-/Rrui0BJWSRONiKGYzqWq9ivGN3+cYkmc47QWITufr7K6yDb8HfssuJUSQZeptyg/rHbHrfDzCUjzoJ2pwuafg==`.

The runtime package tarballs contain no standalone `LICENSE` file in their published file lists. The checked-in
[`sdk/typescript/THIRD_PARTY_NOTICES.md`](../../../../sdk/typescript/THIRD_PARTY_NOTICES.md) therefore records the
declared SPDX expression, upstream canonical source URLs, source hashes, Apache-2.0 Sections 1-9, and the BSD-3-Clause
runtime component text. This is a notice boundary for the private unpublished package; it is not a publication or
license-approval claim.

## Source and license identity

The generator profile pins these compiler artifacts and SHA-256 values:

- macOS arm64 `protoc-35.1-osx-aarch_64.zip`: `193289af0470c6a1aada357d4fba0bbf8d78bfaac8b5e42ca30af2ef75583de2`;
- Linux amd64 `protoc-35.1-linux-x86_64.zip`: `6930ebf62bd4ea607b98fff052596c6ee564b9835b4ce172c75a3f53ae9d91b7`;
- Linux arm64 `protoc-35.1-linux-aarch_64.zip`: `01bf9d08808c7f96678b63f4bd8efa559bb4f83d5a7a270d5edaf507f9d5d9cf`.

Go notice evidence is in [`sdk/go/THIRD_PARTY_NOTICES.md`](../../../../sdk/go/THIRD_PARTY_NOTICES.md):

- Connect Apache-2.0 root license: `595c92e7a9bc933c07da143f527d199e2fc3010e2afd8a8c0654c1f43bfa076c`;
- protobuf BSD-3-Clause license: `4835612df0098ca95f8e7d9e3bffcb02358d435dbb38057c844c99d7f725eb20`;
- shared `x/text`, `x/net`, and `x/sys` BSD-3-Clause license:
  `911f8f5782931320f5b8d1160a76365b83aea6447ee6c04fa6d5591467db9dad`;
- shared Go PATENTS grant: `96f408bfae65bf137fc2525d3ecb030271c50c1e90799f87abf8846d8dd505cc`;
- gRPC NOTICE: `693ff28ec216d5112ac1bbfe64ef539005867d1c7bd427b57d579683293b947f`.

The module files contain no `replace`, `exclude`, Git, workspace, vendor, or local file dependency. The fresh Go
consumer is built with `GOWORK=off` through an exact local module proxy populated from the packed SDK module; the
packed module itself remains an ordinary versioned module. The fresh TypeScript consumer installs an exact SDK
tarball only for the isolated test fixture; the packed SDK has no `file:`, `workspace:`, or Git runtime dependency.

## Security and freshness boundary

This review records package identity and license bytes, not a timeless vulnerability result. A current vulnerability
scan, publication artifact scan, and immutable supply-chain closure remain separate review work. The generated
manifest, contract lock, and implementation record bind the dependency review bytes into the non-Gate candidate so
that any dependency or notice drift invalidates regeneration. Unknown Proto fields are preserved only as wire data;
they do not authorize mutation or side effects. mTLS tests reject missing client certificates before the fixture
service observes a call.

Independent review must re-check the exact source/output hashes, package integrities, license/notice bytes, Go
module/import DAG, fresh consumer installation, the explicit absence of a current vulnerability claim, and
forbidden-surface scans. A current vulnerability scan remains outside this slice and must not be inferred from its
local test results. Until that review and every later aggregate decision, all platform Gates remain OPEN.
