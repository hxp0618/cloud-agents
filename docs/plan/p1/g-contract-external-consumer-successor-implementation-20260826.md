# G-CONTRACT external-consumer successor implementation — 2026-08-26

## Scope and lineage

This record documents the bounded implementation slice authorized as
`D-053-EC-1`, authority `cloud-agents/g-contract-external-consumer/v1`. It is
a versioned successor for the exact-pinned external-consumer criterion; it
does not rewrite, relabel, or close the D-053 v1/v2/v3 source, R5 evidence,
review tuple, registry, generation lock, projection, or replay receipts.

The successor fences the D-053 terminal candidate at commit
`4f71e38205fc25b3b164a24f13141644bd378cf7` and records the predecessor file
bindings (path, Git blob, SHA-256, byte size, and `100644` mode) in its source
authority. Predecessor mutation remains forbidden. Source and generated
profile bytes are versioned under `tools/g-contract-external-consumer/v1/`.

## Implemented contract surface

The consumer harness is intended to exercise two fresh consumers against
disposable loopback fixtures:

- TypeScript installs the generated SDK and its four supporting packages
  (`@bufbuild/protobuf`, `@connectrpc/connect`, `typescript`, and
  `@types/node`) from loopback-served tarballs. The SDK tarball is bound by
  SHA-256 and SRI; the lock records the exact artifact URL and integrity.
- Go resolves the generated module through a loopback GOPROXY fixture. The
  module zip and `go.mod` are SHA-256 bound, while the downloaded module and
  `go.mod` checksums are retained from `go.sum`; `GOWORK=off` and
  `GOFLAGS=-mod=readonly` are required.
- Each generated client performs exactly one real Connect `POST` to
  `/cloudagents.worker.v1alpha1.WorkerExecutionService/Negotiate`, using
  `application/proto` where applicable. The fixture is loopback-only and
  records an observed call count of one for each consumer.

The successor generator/checker validates the source schema, gate-criteria
digest, immutable predecessor bindings, live harness and input digests,
artifact/checksum fields, exact call counts, and forbidden dependency forms
(`file:`, `file://`, `workspace:`, Git, GitHub, and local-path escapes).
Source/profile/registry identities use domain-separated SHA-256 values.
Profile writes are exclusive: an existing differing output is a conflict,
not an overwrite. Evidence input is bound by path, SHA-256, and byte size;
the generated profile is deterministic for fixed source, inputs, harness, and
evidence.

## Verification status

The successor source authority and focused generator tests are present. The
focused checks exercised after the final harness/schema edit are:

```text
bun scripts/generate-platform-g-contract-external-consumer.ts --check-source
bunx vitest run scripts/lib/platform-g-contract-external-consumer.test.ts --reporter=dot
bunx oxfmt --check scripts/test-platform-sdk-consumers.ts scripts/generate-platform-g-contract-external-consumer.ts scripts/lib/platform-g-contract-external-consumer.ts scripts/lib/platform-g-contract-external-consumer.test.ts tools/g-contract-external-consumer/v1/source.schema.json tools/g-contract-external-consumer/v1/profile.schema.json
bunx oxlint scripts/test-platform-sdk-consumers.ts scripts/generate-platform-g-contract-external-consumer.ts scripts/lib/platform-g-contract-external-consumer.ts scripts/lib/platform-g-contract-external-consumer.test.ts
git diff --check
```

All of those focused checks passed. A fresh run of
`bun scripts/test-platform-sdk-consumers.ts` completed both consumers, and the
evidence-producing invocation wrote the append-only v1 evidence byte. Its
stable bindings are:

- harness SHA-256 `sha256:f337bb8d905e6a4eb3c6ce9a64d83cb82e21a6cf9aaca1da30ca7f1fd101a86a`, 33,289 bytes;
- TypeScript SDK SHA-256 `sha256:8266b703c4691f41e558aeefd4b2e765e072c18d5a663b358fdddb0f1a5b65bc` and SRI `sha512-caQ0/HRjDawA5Re/IrimB5cziC9VX5soPOikx77vFSgZk9jcQUoCZ1/Oawg3ZgubCuf5vgt0XWD53+9m7t8KyA==`;
- five loopback npm dependency tarballs, each recorded with version, path, SHA-256, and SRI;
- Go module zip SHA-256 `sha256:d249960e73bdef5386510a9ad9200a2d8457f8d296baca5e0c4247f02539acff`, go.mod SHA-256 `sha256:8b9e28f4db2db796bd69a6d1df5c93ff9f145c621209779b76d2df0a52794063`, and exact `go.sum` module/go.mod checksums;
- evidence SHA-256 `sha256:6b5b5669577aee59aa4bb775c585a04f01aae631bf7384d83727ca125a0a9344` (4,494 bytes), with generated profile status `CURRENT` and profile digest `sha256:f747d2405d2973e895929e7a8c54f13c19af4489bdcd0001b4fe52b43ca64f8c`.

The profile is current for the fixed source/input/evidence bytes, but it is not
an independent review or Gate result. A failed or partial future run must not
be converted into synthetic evidence; a drifted byte requires a new versioned
successor rather than overwriting this v1 output.

Fresh projection and native replay (Darwin arm64 and Linux amd64 where the
authorized disposable fixture is available) are subsequent steps after the
source, schema, harness, and input bindings are frozen. Their receipts and a
detached independent review are pending; existing D-053 receipts remain
historical and immutable.

## Explicit boundary

This is non-Gate, non-production evidence work. The mandatory boundary is
`notGateClosure=true` and `gateStatus=ALL_GATES_OPEN`. The authorization does
not permit production database writes or migrations, provider or P2/HTTP
effects, public endpoints, OIDC/JWKS calls, SSH or hardware power actions,
deployment, publication, release/signing, force-push, history rewrite, or any
Gate transition. Loopback HTTP is allowed only inside the disposable
consumer/artifact fixtures and is not an external service integration.

Any unavailable native supply, platform arm, dependency artifact, schema
check, digest check, topology/lineage check, or independent-review check
remains explicitly pending or unclaimed and requires a new evidence update;
the criterion and Gate state must not be weakened to make a run pass.
