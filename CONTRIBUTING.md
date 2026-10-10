# Contributing

Follow the [joint infrastructure/Admin boundary](docs/plan/adr/0032-infrastructure-admin-delivery-and-document-routing.md) and [execution plan](docs/plan/cloud-agents-platform/04-extraction-and-migration.md): infrastructure and the complete Admin Web form the first deliverable; new user conversation features follow their joint readiness. Reuse existing implementations without treating historical Agent evidence as foundation acceptance.

`.mise.toml` is the executable toolchain declaration (Node.js, Bun, Go, Python, uv, Helm). Run checks through a login shell, for example `mise exec -- zsh -lc '<command>'`, so child probes inherit the pinned PATH; a direct `mise exec -- bun ...` can retain a caller-provided Node.js ahead of the pinned Node.js. The contract checks reject any other Bun/Python/uv version, and Proto SDK checks reject any other Node.js version. Scripts that expose `CLOUD_AGENTS_NODE` may receive the pinned Node executable explicitly. Install with `bun install --frozen-lockfile --ignore-scripts` after the lockfile exists.

In a fresh checkout, review `.mise.toml`, then run `mise trust` before the pinned commands. Run `bun run build` after dependency installation to materialize the workspace SDK packages before checks that import their distributions.

On Linux amd64 CPUs without AVX, use the **baseline** Bun build for the version pinned in `.mise.toml`; the generic x64 build can terminate with an illegal instruction. Verify `bun --version` and retain the binary SHA-256 with replay evidence. When `CLOUD_AGENTS_A24_TOOLCHAIN` is set, its `bun` executable must be the same compatible build used to start the replay runner.

The project `.mise.toml` and Go smoke entrypoints set `GOTOOLCHAIN=local` and fail closed unless `go version` reports Go 1.26.6; they do not download another toolchain or reuse a different `GOROOT`.

For an offline contract-standards check, set `CLOUD_AGENTS_CONTRACT_STANDARDS_WHEELHOUSE` to a local hash-verified wheelhouse and add `UV_OFFLINE=1`; an ignored project-local path such as `.tmp/contract-standards-wheelhouse` is suitable. Keep the wheelhouse platform-specific and out of commits or release artifacts.

The managed Worker image uses the official Debian mirrors by default. When a build environment cannot reach those mirrors, set `CLOUD_AGENTS_DEBIAN_MIRROR` and `CLOUD_AGENTS_DEBIAN_SECURITY_MIRROR` for the Compose build, for example `http://mirrors.aliyun.com/debian` and `http://mirrors.aliyun.com/debian-security`. The slim base bootstraps `ca-certificates` through an HTTP apt mirror with apt signature verification; an HTTPS mirror cannot bootstrap that missing CA bundle. These are build-time source selectors; keep credentials and host-local proxy addresses out of the repository and release evidence.

Worker CLI dependencies are declared in `deploy/docker/worker-tools/package.json` and its npm lock. The image consumes those exact files from the generated deployment archive using `npm ci`; the Runtime workspace's `bun.lock` is a separate dependency boundary. Update the dedicated lock with npm, check the direct-package authority against the verified package sources, and regenerate the platform candidate before rebuilding the image. Retain the license review and remaining apt/transitive supply-chain limitations; an integrity-matched installation alone does not establish the full license or image provenance closure.

The Worker also embeds a per-architecture installed inventory under `/usr/share/doc/cloud-agents/worker-oci-installed-inventory.json`. The existing `test/e2e/test-platform-oci-images.sh` recomputes it from the image and checks `npm ls --all --omit=dev`. Set `CLOUD_AGENTS_OCI_EVIDENCE_DIR` to a new directory outside the candidate to retain the inventory and a receipt binding its digest to the local image ID and platform. Observed package-record consistency and license-artifact presence are separate results: missing license files remain `BLOCKED`, and neither inventory nor a successful smoke establishes legal approval, apt download provenance, complete base-image coverage or the final published-image binding.

Keep supplemental license texts in the existing Worker package authority, with exact package path/version/integrity and a bound source: a repository commit, an integrity-bound npm tarball member, or a `source-archive` member with its HTTPS URL and archive SHA-256. Before recording source-archive material, verify the complete compressed archive and the selected regular member; preserve upstream bytes unchanged. Retain observed download hashes separately from publisher checksums and binary build provenance. The generator captures each text once into the deployment archive; the image stores it under its documentation directory, and the installed inventory records supplemental material separately from files shipped inside npm packages. For a license embedded in an installed README, bind that exact member path and SHA-256 in the same authority. Neither a supplement nor a filename hint changes the full supply-chain or legal-review status.

For an installed native package, `packageFiles` in the same authority binds the complete regular-file set observed in its SRI-verified npm tarball. The v2 install manifest carries these alongside license references in `packageBindings`; the collector rejects missing, extra, symlinked, resized or changed members and reports package-content verification separately. Bind the Sharp platform addon together with its libvips package. The existing OCI smoke also executes a small RGB PNG encode/decode through the installed addon and records the two packages actually loaded by that process. Its receipt requires both loaded packages and all applicable content bindings to pass. Component version metadata and exact installed bytes do not prove that upstream source produced the binary, and do not complete the native license or source-material review.

The native source preparation entry, `scripts/prepare-sharp-libvips-successor.py`, reads the same [source authority](tools/sharp-libvips-successor/v1/source.json). It verifies the librsvg source archive and the bound feature patch, Cargo lock and security patch, then prepares `cloud-agents/librsvg-source/` in that order. The feature patch preserves the upstream recipe choices: no embedded GIF/WebP, no Cairo PDF/PostScript surfaces, and no Linux Meson static-library override. The generated build recipe copies this prepared tree without patching it again or updating dependencies. Patch application disables backup files; a digest mismatch or failed patch context aborts preparation. Do not edit the generated tree to bypass it. The main entry also requires `--librsvg-vendor-dir`: it verifies the bound vendor archive, unpacks it under `librsvg-source/vendor`, and generates the Cargo source replacement with offline mode enabled. Existing vendor/config paths are rejected. This prepares native build inputs; it does not prove a complete native rebuild.

The native builder RPM inputs are bound by `builderRpms` in the same authority. Verify the current process-level proxy and network before downloading. Use an ignored local directory with exactly `keys/`, `linux-arm64v8/`, and `linux-x64/`; do not put logs or repository metadata inside it. The following uses the published domestic mirror URLs and retains already verified downloads:

```sh
export BUILDER_RPM_DIR="$PWD/.tmp/native-builder-rpms"
python3 -B - <<'PYTHON'
import hashlib, json, os, subprocess
from pathlib import Path

assert os.environ.get("HTTPS_PROXY") or os.environ.get("https_proxy")
authority = json.loads(Path("tools/sharp-libvips-successor/v1/source.json").read_text())["builderRpms"]
root = Path(os.environ["BUILDER_RPM_DIR"])
groups = {"keys": list(authority["keys"].values())}
groups.update({target: record["packages"] for target, record in authority["platforms"].items()})
for directory, records in groups.items():
    (root / directory).mkdir(parents=True, exist_ok=True)
    for record in records:
        destination = root / directory / record["file"]
        if not destination.exists():
            subprocess.run(["curl", "--silent", "--fail", "--location", "--proto", "=https",
                            "--output", str(destination), record["url"]], check=True)
        assert hashlib.sha256(destination.read_bytes()).hexdigest() == record["sha256"], destination
PYTHON
```

Pass this directory to `prepare-sharp-libvips-successor.py --builder-rpm-dir`. Preparation rejects missing, extra, linked or changed inputs. The generated Dockerfiles expose an `rpm` stage: build it with the generated platform/base binding, `--pull=false --network=none --target rpm`. Its installer verifies each RPM's bound key and exact identity with system RPM tools, disables all DNF repositories, and installs the explicit local package set. This qualifies RPM acquisition and installation on the fixed base; complete native compilation, linked-component inventory, source/license obligations and vulnerability review remain separate. After all bound inputs are prepared and the selected base child manifest is present locally, run `./build.sh linux-arm64v8` or `./build.sh linux-x64` from the generated root. The entry rejects a missing local base, disables pulls, and runs build steps and the native container with networking disabled. A successful run writes the native tarball and its `libvips-cpp-<platform>.map` side by side; a failed native build does not copy a map. The tarball also includes `source-provenance/`, which the supported Linux npm packages preserve. It contains the byte-exact source authority and its three repository-local inputs at their repository-relative paths, plus the final link map and target-specific librsvg Cargo metadata. Preparation rejects a missing, linked or inconsistent authority and changed local inputs. Preserve this bundle for tracing declared sources and reviewing actual static archive members. It is not a complete source redistribution, unique attribution of every archive member, signed build attestation or proof of license compliance. The successor versions.json reports direct prebuilt dependency versions, not a complete linked-component SBOM. It omits proxy-libintl on these GNU Linux targets and identifies bundled libnsgif as vendored-in-libvips-<version>, using the same pinned libvips source. It retains xml2 because the recipe builds it; the link map separately records whether any of its archive members enter the final library. Rust dependencies, nested source components and system/toolchain attribution still require their own evidence.

The native successor's cargo-c input can be regenerated with `scripts/vendor-sharp-libvips-cargo-c.py` (Python 3.12+ and the Cargo version recorded in `cargoC.vendorGeneratedWith`). Read `cargoC.source.url`, `cargoC.source.sha256`, and `cargoC.registry` from [the source authority](tools/sharp-libvips-successor/v1/source.json). Download the original crate to an ignored directory, verify its SHA-256, and inspect it with `scripts/lib/inspect-generator-supply-archive.py` before extracting it into a new source directory. Keep the original `Cargo.lock` unchanged.

Seed a dedicated Cargo cache using that extracted source and a credential-free source replacement matching `cargoC.registry`. The current authority uses the following configuration; put it in the dedicated cache's `config.toml`, and supply any required proxy only through process environment variables:

```toml
[source.crates-io]
replace-with = "native-inputs"
[source.native-inputs]
registry = "sparse+https://rsproxy.cn/index/"
```

Set `CARGO_C_ARCHIVE`, `CARGO_C_SOURCE`, and `CARGO_C_CACHE` to absolute local paths. Fetching is the network preparation step; archive generation is offline and does not inherit the cache's configuration, credentials or unpacked `registry/src`:

```sh
CARGO_HOME="$CARGO_C_CACHE" cargo fetch --locked --manifest-path "$CARGO_C_SOURCE/Cargo.toml"
mise exec -- zsh -lc 'python3 -B scripts/vendor-sharp-libvips-cargo-c.py \
  --cargo-c-archive "$CARGO_C_ARCHIVE" \
  --cargo-registry-dir "$CARGO_C_CACHE/registry" \
  --output-dir .tmp/cargo-c-inputs'
```

Export those three variables before the login-shell command. The output directory must not exist. The generator verifies the source and every locked crate, runs standard `cargo vendor --frozen` in isolation, compares its output to the original archives, and only delivers the source/vendor pair when the deterministic archive matches the bound digest. Packing uses component-sorted paths, PAX tar, xz preset 3, zero timestamps/owners, directories at 0755 and original file modes with group/other write bits removed. Pass the resulting directory to `prepare-sharp-libvips-successor.py --cargo-c-dir`. A missing cache entry or digest mismatch is a failure; do not edit the authority or vendor contents to bypass it. This recipe covers cargo-c inputs, not the complete native build or release qualification.

The librsvg vendor uses `scripts/vendor-sharp-libvips-librsvg.py` and the Cargo version recorded in `librsvg.vendorGeneratedWith`. Download the source archive and security patch from their authority URLs using the process-level proxy. Set `LIBRSVG_ARCHIVE`, `LIBRSVG_SECURITY_PATCH`, `LIBRSVG_SOURCE`, and `LIBRSVG_CACHE` to absolute local paths and export them. The source output must not exist. Prepare the cache-seeding workspace through the same source helper used by the vendor generator:

```sh
mise exec -- zsh -lc 'python3 -B -' <<'PYTHON'
import importlib.util
import json
import os
from pathlib import Path

repo = Path.cwd()
spec = importlib.util.spec_from_file_location(
    "native_prepare", repo / "scripts/prepare-sharp-libvips-successor.py"
)
prepare = importlib.util.module_from_spec(spec)
spec.loader.exec_module(prepare)
authority = json.loads((repo / prepare.SOURCE_RELATIVE_PATH).read_text())
prepare.prepare_librsvg_source(
    librsvg=authority["librsvg"], cargo_lock=authority["cargoLock"], repo_root=repo,
    librsvg_archive=Path(os.environ["LIBRSVG_ARCHIVE"]),
    librsvg_security_patch=Path(os.environ["LIBRSVG_SECURITY_PATCH"]),
    output_dir=Path(os.environ["LIBRSVG_SOURCE"]),
)
PYTHON
```

Configure the dedicated `LIBRSVG_CACHE/config.toml` with the same source replacement shown above, using `librsvg.registry`. Fetch the complete workspace lock without a target filter, then generate the archive offline:

```sh
CARGO_HOME="$LIBRSVG_CACHE" cargo fetch --locked --manifest-path "$LIBRSVG_SOURCE/Cargo.toml"
mise exec -- zsh -lc 'python3 -B scripts/vendor-sharp-libvips-librsvg.py \
  --librsvg-archive "$LIBRSVG_ARCHIVE" \
  --librsvg-security-patch "$LIBRSVG_SECURITY_PATCH" \
  --cargo-registry-dir "$LIBRSVG_CACHE/registry" \
  --output-dir .tmp/librsvg-inputs'
```

Pass `.tmp/librsvg-inputs` to the main prepare command's `--librsvg-vendor-dir`. The producer derives workspace package identities from the prepared manifests and checks them against the fixed lock; it verifies every registry crate, isolates Cargo from host configuration, and uses the same deterministic archive implementation as cargo-c. It delivers only the archive matching `librsvg.vendor.sha256`; it never updates the authority. Target-filtered metadata does not replace this full lock closure. Native compilation, linked-component inventory and release qualification remain separate checks.

For documentation-only changes, check the diff, local links, current-vs-target claims, and preserved approval/acceptance boundaries; runtime E2E and release closure are not prerequisites for editing prose. If executable examples, contracts, or behavior change, run the affected checks as well. This exception does not waive any explicit candidate/release gate.

For public acceptance evidence, run `bun run public:evidence:check`. The generator binds the local source documents by path, size, mode, and SHA-256, and emits a sanitized public mirror with local paths, runtime endpoints, credential paths, and unreviewed historical binary objects projected out. Both declared source authority documents must be present: a clone containing only the sanitized mirror cannot regenerate or validate the source bindings and must record this check as `NOT RUN`/`BLOCKED` until the same authority batch is included. The source documents and the mirror retain separate status boundaries; a mirror check does not close a runtime or release Gate.

For an external EC-3 replay bundle, follow the [replay instructions](docs/plan/p1/g-contract-external-consumer-v3-replay-implementation-20261006.md). Pre-create separate real output directories outside the repository for native runs and summary generation; symlink roots and overwriting existing artifacts are rejected. Run `bun scripts/replay-platform-g-contract-external-consumer-v3.ts --check-summary --output-root <bundle-root>` to revalidate its frozen inputs, four native receipts, summary and profile without writing files. A current external profile still requires final review and does not close any release Gate.

Follow the single [development and test policy](CLAUDE.md#tests-and-verification). During a slice, run affected package checks and focused Vitest tests; before delivery, run the applicable combined formatting, lint, typecheck, build and test checks once. Build SDK distributions before suites consuming them. Use package scripts or Vitest, not `bun test`, which bypasses the configured runner. Empty discovery is a failure.

The [test directory guide](test/README.md) lists commands and Go visibility exceptions. Run `bun run test:python` for repository Python tools and `bun run test:e2e` for the pinned Web flow. CI installs Chromium with `bunx --no-install @e2e-dev/web install chromium --with-deps`; local runs can use the runner's browser download. E2E telemetry is disabled by the package script and temporary output is ignored. Ordinary Web checks require no Provider credentials; real backend/Provider harnesses keep their separate prerequisites.

For other code changes, select the applicable verification routes below. Reuse [package scripts](package.json) and [CI](.github/workflows/ci.yml); do not infer Go or infrastructure verification from Runtime tests alone.

| Changed surface | Existing verification route |
| --- | --- |
| Go Control Plane, Worker or Go SDK | Affected package `go test` and `go vet`; add `-race` for concurrency changes. `bun run platform:go:check` and `sh test/scripts/test-platform-go-products.sh` remain the combined CI/product route. |
| Contracts or generated SDKs | `bun run platform:contracts:check` (includes `platform:sdk:check`, the current successor-v3 lock, and the EC-3 external-consumer authority), and `bun run platform:sdk:consumers` when consumer compatibility is affected. |
| SQL/migration bundle | `bun run platform:migrations:check` plus the affected database, isolation and recovery tests in an authorized test environment; static checks do not prove a successful migration. |
| Admin Web | `bun --filter @cloud-agents/admin-web typecheck`, `bun --filter @cloud-agents/admin-web build`, and then `bun --filter @cloud-agents/admin-web test`; exercise affected rendered flows, including relevant authorization, language and visual states. |

Run corresponding real integration checks when runtime behavior changes. These are change-scoped verification routes, not a requirement to run every E2E before each edit or permission to deploy. Full CI and explicit candidate/release gates remain unchanged; an unavailable check is reported as unverified, not passed.

To update an already-installed single-host Compose development deployment from the working tree, set `CLOUD_AGENTS_DEPLOY_TARGET` to an SSH alias (for example in the git-ignored `.env.local`) and run `bun run deploy:dev`. The host keeps its env file, secrets and override in `$CLOUD_AGENTS_DEPLOY_ROOT/state` (default `/opt/cloud-agents/state`). Image tags are digests of each image's build inputs, so only changed services are rebuilt and recreated; `--all` rebuilds and recreates everything, and `--dry-run` only prints the plan. New migrations require confirmation (or `--yes`) and are preceded by a database backup. This is a development convenience, not a release, and does not replace the packaged install in [deploy/compose](deploy/compose/README.md).

With no focused mode or with `--remote-worker-only`, `test/e2e/test-foundation-controller-docker.mjs` builds the repository's [execd successor recipe](tools/opensandbox-execd-successor/v1/README.md) by default and uses the resulting immutable local image ID. Its source, patch, toolchain, binary and image bindings remain in the run's `execd-build/build-evidence.json`. This build needs Docker and access to the pinned upstream source and build dependencies; it does not depend on an earlier local POC checkout. For an isolated comparison, `--remote-worker-only` also accepts `CLOUD_AGENTS_FOUNDATION_EXECD_IMAGE` as an immutable local `sha256:` image ID or a repository reference pinned with `@sha256:`. Floating tags and overrides in other modes are rejected before resources are created. Other focused modes keep their existing execd binding until separately qualified; a passing scoped run does not publish an image or prove execd process-crash recovery.

For Foundation lifecycle validation against an existing single-node OrbStack k3s VM, use `test/e2e/test-foundation-controller-kubernetes-orbstack-vm.sh --vm "$TEST_VM" --kubeconfig "$KUBECONFIG" --context "$TEST_CONTEXT" --output-dir /tmp/cloud-agents-kubernetes-validation` with explicit local values and a new output directory. The wrapper verifies the VM/cluster binding, discovers the Pod and Service ranges, and adds temporary routes inside the OrbStack Docker network namespace only when no exact route already exists. It runs the existing Foundation harness and removes its routes on exit, retaining separate harness and route-cleanup results. It requires the pinned helper image already present locally; it neither creates a cluster nor configures persistent macOS routes. Multi-node, Provider and host-restart acceptance remain separate checks.

The dev fixture, Compose smoke and Docker/Kubernetes Foundation qualifications build the same [OpenSandbox server Cookie isolation successor](tools/opensandbox-server-successor/v1/README.md). Docker's SSH-probe-only mode does not start this server. Compose reads the builder and recipe from the verified deployment archive and uses one immutable image ID for source and destination servers. Build receipts bind the selected arm64/amd64 child image, original source bytes, patch and derived image; dev and Compose print their retained external receipt path. The fixed base image for the selected platform must already be present locally. Preview qualification must pass after one Sandbox has returned `Set-Cookie`; clearing shared Cookie state between test phases does not prove isolation. Building and running the regression under amd64 emulation does not qualify an entire native amd64 environment. Complete base-image provenance and external operator-provided targets remain separate checks.

Keep the public ABI host-neutral. Public packages may expose JSON-compatible objects, ordinary errors, promises, async iterables, abort signals, Node stdio primitives where documented, and JSON Schema. Do not export Effect values or Synara/T3 application types. Do not add Turbo or a dependency on either application root.

Change the current protocol authority and its consumers together under [CLAUDE.md](CLAUDE.md#contract-and-security-invariants). Internal package dependencies are exact RC pins. Each package tarball must retain `LICENSE`.

Never commit credentials. Synthetic secret-shaped inputs belong only in an explicitly allowlisted test or fixture path. A GitHub RC is not an npm publication or GA approval.

`tools/generator-supply/npm` is a private, non-distributable build fixture. Its `UNLICENSED` metadata is deliberate; it is not a public package and remains bound to generator-supply evidence.
