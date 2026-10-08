#!/bin/sh

set -eu

if [ "$#" -ne 1 ] || [ ! -d "$1" ]; then
  echo "usage: test-platform-oci-images.sh PLATFORM_RELEASE_DIRECTORY" >&2
  exit 2
fi

candidate_directory=$(CDPATH= cd -- "$1" && pwd)
platform=${CLOUD_AGENTS_PLATFORM:-linux/amd64}
case "$platform" in
  linux/amd64 | linux/arm64) ;;
  *)
    echo "CLOUD_AGENTS_PLATFORM must be linux/amd64 or linux/arm64" >&2
    exit 2
    ;;
esac

script_directory=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
node "$script_directory/../../scripts/lib/platform-release-verifier.ts" "$candidate_directory"
evidence_directory=$(node - "${CLOUD_AGENTS_OCI_EVIDENCE_DIR:-}" "$candidate_directory" <<'NODE'
const { mkdirSync, mkdtempSync, realpathSync } = require("node:fs");
const { basename, dirname, isAbsolute, join } = require("node:path");
const [input, candidate] = process.argv.slice(2);
if (input && !isAbsolute(input)) throw new Error("OCI evidence directory must be absolute");
const destination = input
  ? join(realpathSync(dirname(input)), basename(input))
  : join(realpathSync("/tmp"), "cloud-agents-oci-evidence.");
const candidateRoot = realpathSync(candidate);
if (destination === candidateRoot || destination.startsWith(`${candidateRoot}/`)) {
  throw new Error("OCI evidence must be outside the candidate directory");
}
if (input) mkdirSync(destination, { mode: 0o700 });
console.log(input ? destination : mkdtempSync(destination));
NODE
)
echo "OCI smoke evidence: $evidence_directory"

set -- "$candidate_directory"/cloud-agents-deployment-*.tar
if [ "$#" -ne 1 ] || [ ! -f "$1" ]; then
  echo "platform release must contain exactly one deployment package" >&2
  exit 1
fi

smoke_directory=$(mktemp -d "$candidate_directory/.oci-smoke.XXXXXX")
image_prefix="cloud-agents-oci-smoke-$$"
cleanup() {
  rm -rf -- "$smoke_directory"
  for image in control-plane worker migrate; do
    docker image rm "$image_prefix:$image" >/dev/null 2>&1 || true
  done
}
trap cleanup 0 HUP INT TERM
tar -xf "$1" -C "$smoke_directory"
set -- "$candidate_directory"/cloud-agents-migrations-*.tar
if [ "$#" -ne 1 ] || [ ! -f "$1" ]; then
  echo "platform release must contain exactly one migration package" >&2
  exit 1
fi
migration_head=${1##*-}
migration_head=${migration_head%.tar}

for image in control-plane worker migrate; do
  set -- docker buildx build \
    --platform "$platform" \
    --file "$smoke_directory/deploy/docker/$image.Dockerfile" \
    --tag "$image_prefix:$image" \
    --load
  if [ -n "${CLOUD_AGENTS_DEBIAN_MIRROR:-}" ]; then
    set -- "$@" --build-arg "DEBIAN_MIRROR=$CLOUD_AGENTS_DEBIAN_MIRROR"
  fi
  if [ -n "${CLOUD_AGENTS_DEBIAN_SECURITY_MIRROR:-}" ]; then
    set -- "$@" --build-arg "DEBIAN_SECURITY_MIRROR=$CLOUD_AGENTS_DEBIAN_SECURITY_MIRROR"
  fi
  "$@" "$candidate_directory"
done

expect_startup_failure() {
  image=$1
  expected_exit=$2
  expected_message=$3
  set +e
  output=$(docker run --rm "$image_prefix:$image" 2>&1)
  exit_code=$?
  set -e
  if [ "$exit_code" -ne "$expected_exit" ]; then
    echo "$image image exited $exit_code, expected $expected_exit: $output" >&2
    exit 1
  fi
  case "$output" in
    *"$expected_message"*) ;;
    *)
      echo "$image image did not reach its application entry point: $output" >&2
      exit 1
      ;;
  esac
}

expect_startup_failure control-plane 2 "database, authentication, TLS, and access Grant configuration are required"
expect_startup_failure worker 2 "startup or shutdown failed"
expect_startup_failure migrate 1 "database URL, repository root, and product-$migration_head selector are required"

test "$(docker run --rm --entrypoint /usr/local/bin/codex "$image_prefix:worker" --version)" = "codex-cli 0.154.0"
test "$(docker run --rm --entrypoint /usr/local/bin/claude "$image_prefix:worker" --version)" = "2.1.207 (Claude Code)"
test "$(docker run --rm --entrypoint /usr/local/bin/dsh "$image_prefix:worker" --version)" = "0.1.2-rc.1"
test "$(docker run --rm --entrypoint /usr/bin/readlink "$image_prefix:worker" /usr/local/bin/codex)" = "/opt/cloud-agents-worker-tools/node_modules/.bin/codex"
test "$(docker run --rm --entrypoint /usr/bin/readlink "$image_prefix:worker" /usr/local/bin/dsh)" = "/opt/cloud-agents-worker-tools/node_modules/.bin/dsh"
docker run --rm --entrypoint /usr/local/bin/node "$image_prefix:worker" -e \
	'const pty = require("/opt/cloud-agents-worker-tools/node_modules/node-pty"); const child = pty.spawn("/bin/sh", ["-c", "printf lock-pty-ok"], { cols: 80, rows: 24 }); let output = ""; child.onData((data) => { output += data; }); child.onExit(({ exitCode }) => { if (exitCode !== 0 || output !== "lock-pty-ok") process.exit(1); });'
docker run --rm -i --network none --read-only --entrypoint /usr/local/bin/node "$image_prefix:worker" \
  > "$evidence_directory/sharp-native.json" <<'NODE'
const assert = require("node:assert/strict");
const { existsSync, readFileSync, realpathSync } = require("node:fs");
const { dirname, join, relative } = require("node:path");
const sharp = require("/opt/cloud-agents-worker-tools/node_modules/sharp");
(async () => {
  const png = await sharp({ create: { width: 1, height: 1, channels: 3, background: { r: 2, g: 3, b: 4 } } }).png().toBuffer();
  assert.equal(png.subarray(0, 8).toString("hex"), "89504e470d0a1a0a");
  const { data, info } = await sharp(png).raw().toBuffer({ resolveWithObject: true });
  assert.equal(info.width, 1);
  assert.equal(info.height, 1);
  assert.equal(info.channels, 3);
  assert.deepEqual([...data], [2, 3, 4]);
  const addons = Object.keys(require.cache).filter((path) => path.endsWith(".node"));
  const libraries = [...new Set(readFileSync("/proc/self/maps", "utf8").split("\n")
    .map((line) => line.trim().split(/\s+/).slice(5).join(" "))
    .filter((path) => /\/libvips-cpp\.so(?:\.|$)/.test(path)))];
  assert.equal(addons.length, 1);
  assert.equal(libraries.length, 1);
  const toolsRoot = "/opt/cloud-agents-worker-tools";
  const loadedPackagePaths = [...new Set([...addons, ...libraries].map((file) => {
    let directory = dirname(realpathSync(file));
    while (directory.startsWith(`${toolsRoot}/node_modules/`)) {
      if (existsSync(join(directory, "package.json"))) return relative(toolsRoot, directory);
      directory = dirname(directory);
    }
    throw new Error("Sharp loaded a native file outside an installed package");
  }))].sort();
  assert.equal(loadedPackagePaths.length, 2);
  console.log(JSON.stringify({ status: "PASS", scope: "native Sharp RGB PNG encode/decode", width: info.width, height: info.height, channels: info.channels, pixelHex: data.toString("hex"), loadedPackagePaths }));
})().catch((error) => { console.error(error); process.exitCode = 1; });
NODE
docker run --rm --entrypoint /usr/bin/test "$image_prefix:worker" \
	-r /usr/share/doc/cloud-agents/worker-oci-install-manifest.json
docker run --rm --entrypoint /usr/bin/test "$image_prefix:worker" \
	-r /usr/share/doc/cloud-agents/worker-oci-notices.md
worker_oci_manifest=$(docker run --rm --entrypoint /usr/bin/cat "$image_prefix:worker" \
	/usr/share/doc/cloud-agents/worker-oci-install-manifest.json)
case "$worker_oci_manifest" in
	*'"kind": "cloud-agents-worker-oci-install-manifest"'*) ;;
	*) echo "Worker image OCI install manifest is missing or invalid" >&2; exit 1 ;;
esac
cmp "$smoke_directory/deploy/docker/worker-oci-install-manifest.json" \
	"$candidate_directory/cloud-agents-worker-oci-install-manifest.json"
cmp "$smoke_directory/deploy/docker/worker-oci-notices.md" \
	"$candidate_directory/cloud-agents-worker-oci-notices.md"
CLOUD_AGENTS_OCI_MANIFEST="$candidate_directory/cloud-agents-worker-oci-install-manifest.json" \
	CLOUD_AGENTS_OCI_DEPLOY="$smoke_directory" node <<'NODE'
const { createHash } = require("node:crypto");
const { readFileSync } = require("node:fs");
const { join } = require("node:path");
const manifest = JSON.parse(readFileSync(process.env.CLOUD_AGENTS_OCI_MANIFEST, "utf8"));
for (const [pathField, digestField, expectedPath] of [
  ["dockerfile", "dockerfileSha256", "deploy/docker/worker.Dockerfile"],
  ["packageManifest", "packageManifestSha256", "deploy/docker/worker-tools/package.json"],
  ["lockfile", "lockfileSha256", "deploy/docker/worker-tools/package-lock.json"],
  ["packageAuthority", "packageAuthoritySha256", "deploy/docker/worker-oci-package-authority.json"],
  ["installedInventoryCollector", "installedInventoryCollectorSha256", "scripts/lib/worker-oci-installed.ts"],
]) {
  const path = manifest.source?.[pathField];
  const expected = manifest.source?.[digestField];
  if (path !== expectedPath || !/^[0-9a-f]{64}$/u.test(expected)) {
    throw new Error(`Worker OCI source binding ${pathField} is invalid`);
  }
  const actual = createHash("sha256")
    .update(readFileSync(join(process.env.CLOUD_AGENTS_OCI_DEPLOY, expectedPath)))
    .digest("hex");
  if (actual !== expected) {
    throw new Error(`Worker OCI source binding ${pathField} does not match deployment bytes`);
  }
}
NODE
docker run --rm --entrypoint /usr/bin/cat "$image_prefix:worker" \
	/opt/cloud-agents-worker-tools/package.json > "$smoke_directory/image-worker-package.json"
docker run --rm --entrypoint /usr/bin/cat "$image_prefix:worker" \
	/opt/cloud-agents-worker-tools/package-lock.json > "$smoke_directory/image-worker-package-lock.json"
docker run --rm --entrypoint /usr/bin/cat "$image_prefix:worker" \
	/usr/share/doc/cloud-agents/worker-oci-install-manifest.json > "$smoke_directory/image-worker-oci-install-manifest.json"
docker run --rm --entrypoint /usr/bin/cat "$image_prefix:worker" \
	/usr/share/doc/cloud-agents/worker-oci-notices.md > "$smoke_directory/image-worker-oci-notices.md"
cmp "$smoke_directory/image-worker-package.json" \
	"$smoke_directory/deploy/docker/worker-tools/package.json"
cmp "$smoke_directory/image-worker-package-lock.json" \
	"$smoke_directory/deploy/docker/worker-tools/package-lock.json"
cp "$smoke_directory/image-worker-package-lock.json" "$evidence_directory/package-lock.json"
cmp "$smoke_directory/image-worker-oci-install-manifest.json" \
	"$candidate_directory/cloud-agents-worker-oci-install-manifest.json"
cmp "$smoke_directory/image-worker-oci-notices.md" \
	"$candidate_directory/cloud-agents-worker-oci-notices.md"
docker run --rm --network none --read-only --entrypoint /usr/bin/cat "$image_prefix:worker" \
  /usr/share/doc/cloud-agents/worker-oci-installed.ts > "$smoke_directory/image-worker-oci-installed.ts"
cmp "$smoke_directory/image-worker-oci-installed.ts" "$smoke_directory/scripts/lib/worker-oci-installed.ts"
docker run --rm --network none --read-only --entrypoint /usr/bin/cat "$image_prefix:worker" \
  /usr/share/doc/cloud-agents/worker-oci-installed-inventory.json > "$evidence_directory/installed-inventory.json"
docker run --rm --network none --read-only --entrypoint /usr/local/bin/node "$image_prefix:worker" \
  /usr/share/doc/cloud-agents/worker-oci-installed.ts "$platform" > "$smoke_directory/recomputed-inventory.json"
cmp "$evidence_directory/installed-inventory.json" "$smoke_directory/recomputed-inventory.json"
docker run --rm --network none --read-only --entrypoint /usr/local/bin/npm "$image_prefix:worker" \
  ls --prefix /opt/cloud-agents-worker-tools --all --omit=dev --json > "$evidence_directory/npm-ls.json"
docker image inspect --format '{"imageId":{{json .Id}},"os":{{json .Os}},"architecture":{{json .Architecture}}}' \
  "$image_prefix:worker" > "$evidence_directory/image.json"
docker run --rm --entrypoint /usr/bin/test "$image_prefix:migrate" \
	-r "/opt/cloud-agents/migrations/services/control-plane/migrations/product/$migration_head/manifest.json"

runtime_output=$(
  printf '%s\n' \
    '{"requestId":"oci-smoke-describe-codex","protocolVersion":{"major":2,"minor":3},"executionId":"oci-smoke","generation":1,"commandType":"Describe","commandId":"oci-smoke-describe-codex","occurredAt":"2026-09-01T00:00:00.000Z","payload":{"provider":"codex"}}' \
    '{"requestId":"oci-smoke-describe-claude","protocolVersion":{"major":2,"minor":3},"executionId":"oci-smoke","generation":1,"commandType":"Describe","commandId":"oci-smoke-describe-claude","occurredAt":"2026-09-01T00:00:00.000Z","payload":{"provider":"claudeAgent"}}' \
    '{"requestId":"oci-smoke-describe-pi","protocolVersion":{"major":2,"minor":3},"executionId":"oci-smoke","generation":1,"commandType":"Describe","commandId":"oci-smoke-describe-pi","occurredAt":"2026-09-01T00:00:00.000Z","payload":{"provider":"pi"}}' \
    '{"requestId":"oci-smoke-describe-dsh","protocolVersion":{"major":2,"minor":3},"executionId":"oci-smoke","generation":1,"commandType":"Describe","commandId":"oci-smoke-describe-dsh","occurredAt":"2026-09-01T00:00:00.000Z","payload":{"provider":"deepseek-harness"}}' |
    docker run --rm -i \
      --env CLOUD_AGENT_PROVIDER_HOST_EXPERIMENTAL_PROVIDERS=codex,claudeAgent,pi,deepseek-harness \
      --entrypoint /usr/local/bin/cloud-agent-runtime \
      "$image_prefix:worker" \
      --protocol-v2
)
case "$runtime_output" in
  *'"providerKind":"codex"'*'"name":"codex","version":"0.154.0","available":true,"compatible":true'*) ;;
  *) echo "Worker image Codex Runtime descriptor is unavailable or incompatible" >&2; exit 1 ;;
esac
case "$runtime_output" in
  *'"providerKind":"claudeAgent"'*'"name":"@anthropic-ai/claude-agent-sdk","version":"0.3.207","available":true,"compatible":true'*) ;;
  *) echo "Worker image Claude Runtime descriptor is unavailable or incompatible" >&2; exit 1 ;;
esac
case "$runtime_output" in
  *'"providerKind":"pi"'*'"name":"@earendil-works/pi-coding-agent","version":"0.85.1","available":true,"compatible":true'*) ;;
  *) echo "Worker image Pi Runtime descriptor is unavailable or incompatible" >&2; exit 1 ;;
esac
case "$runtime_output" in
  *'"providerKind":"deepseek-harness"'*'"name":"@deepseek-ai/dsh-sdk-client","version":"0.1.2-rc.1","available":true,"compatible":true'*) ;;
  *) echo "Worker image DeepSeek Harness Runtime descriptor is unavailable or incompatible" >&2; exit 1 ;;
esac

node - "$evidence_directory" "$platform" "$candidate_directory" <<'NODE'
const { createHash } = require("node:crypto");
const { readFileSync, writeFileSync } = require("node:fs");
const { join } = require("node:path");
const [directory, platform, candidateDirectory] = process.argv.slice(2);
const image = JSON.parse(readFileSync(join(directory, "image.json"), "utf8"));
const inventory = JSON.parse(readFileSync(join(directory, "installed-inventory.json"), "utf8"));
if (`${image.os}/${image.architecture}` !== platform || inventory.platform !== platform ||
    !/^sha256:[0-9a-f]{64}$/.test(image.imageId)) throw new Error("Worker inventory image binding mismatch");
if (inventory.schemaVersion !== 1 || inventory.kind !== "cloud-agents-worker-oci-installed-inventory" ||
    inventory.status !== "BLOCKED" || inventory.packageRecordConsistency !== "PASS" ||
    !["PASS", "BLOCKED"].includes(inventory.licenseArtifactPresence) ||
    inventory.fullSupplyChain?.status !== "BLOCKED" ||
    !Array.isArray(inventory.fullSupplyChain.limits) || inventory.fullSupplyChain.limits.length === 0) {
  throw new Error("Worker inventory schema or supply-chain scope is invalid");
}
const npmTree = JSON.parse(readFileSync(join(directory, "npm-ls.json"), "utf8"));
if (typeof npmTree.name !== "string" || typeof npmTree.version !== "string" || npmTree.problems?.length) {
  throw new Error("npm ls did not provide a valid dependency tree");
}
const candidateBytes = readFileSync(join(candidateDirectory, "platform-release-manifest.json"));
const candidate = JSON.parse(candidateBytes);
const hash = (bytes) => createHash("sha256").update(bytes).digest("hex");
const bindings = ["cloud-agents-deployment", "cloud-agents-worker-oci-install-manifest"].map((name) => {
  const artifact = candidate.artifacts.find((item) => item.name === name && item.target === "portable");
  if (!artifact || hash(readFileSync(join(candidateDirectory, artifact.filename))) !== artifact.sha256.replace(/^sha256:/, "")) {
    throw new Error(`OCI candidate artifact ${name} has drifted`);
  }
  return { filename: artifact.filename, sha256: artifact.sha256 };
});
if (`sha256:${inventory.source?.staticManifestSha256}` !== bindings[1].sha256) {
  throw new Error("Installed inventory does not bind the candidate Worker manifest");
}
const workerManifest = JSON.parse(readFileSync(join(candidateDirectory, bindings[1].filename), "utf8"));
if (workerManifest.schemaVersion !== 2 || !Array.isArray(workerManifest.packageBindings) ||
    !Array.isArray(inventory.npm?.packages) || inventory.npm.packages.length === 0) {
  throw new Error("Worker package content binding schema is invalid");
}
const packageLockBytes = readFileSync(join(directory, "package-lock.json"));
if (hash(packageLockBytes) !== workerManifest.source?.lockfileSha256 ||
    hash(packageLockBytes) !== inventory.source?.packageLockSha256) {
  throw new Error("Worker package lock binding mismatch");
}
const packageLock = JSON.parse(packageLockBytes);
if (packageLock.lockfileVersion !== 3 || !packageLock.packages || typeof packageLock.packages !== "object") {
  throw new Error("Worker package lock schema is invalid");
}
const applies = (constraint, expected) => {
  if (constraint === undefined) return true;
  if (!Array.isArray(constraint) || constraint.some((value) => typeof value !== "string")) {
    throw new Error("Worker package platform constraint is invalid");
  }
  const positive = constraint.filter((value) => !value.startsWith("!"));
  return !constraint.includes(`!${expected}`) && (positive.length === 0 || positive.includes(expected));
};
const expectedContentPaths = workerManifest.packageBindings.filter((binding) => {
  if (binding.packageFiles === undefined) return false;
  const entry = packageLock.packages[binding.path];
  if (!entry) throw new Error(`Worker package binding is absent from lock: ${binding.path}`);
  return applies(entry.os, "linux") && applies(entry.cpu, platform === "linux/arm64" ? "arm64" : "x64");
}).map(({ path }) => path).sort();
const normalizeFiles = (files) => files.map(({ path, size, sha256 }) => ({ path, size, sha256 }))
  .sort((left, right) => left.path < right.path ? -1 : left.path > right.path ? 1 : 0);
const qualifiedPackageContents = [];
for (const installed of inventory.npm.packages) {
  const binding = workerManifest.packageBindings.find((item) => item.path === installed.physicalPath);
  if (binding?.packageFiles !== undefined) {
    if (installed.packageContentVerification !== "PASS" || !Array.isArray(installed.packageFiles) ||
        JSON.stringify(normalizeFiles(installed.packageFiles)) !== JSON.stringify(normalizeFiles(binding.packageFiles))) {
      throw new Error(`Worker package content binding mismatch: ${installed.physicalPath}`);
    }
    qualifiedPackageContents.push({ path: installed.physicalPath, files: installed.packageFiles.length });
  } else if (installed.packageContentVerification !== "NOT_RUN" || installed.packageFiles !== null) {
    throw new Error(`Worker package content binding mismatch: ${installed.physicalPath}`);
  }
}
qualifiedPackageContents.sort((left, right) => left.path < right.path ? -1 : left.path > right.path ? 1 : 0);
if (expectedContentPaths.length === 0 ||
    JSON.stringify(qualifiedPackageContents.map(({ path }) => path)) !== JSON.stringify(expectedContentPaths)) {
  throw new Error("Worker current-platform package content checks are incomplete");
}
const sharpResult = JSON.parse(readFileSync(join(directory, "sharp-native.json"), "utf8"));
if (sharpResult.status !== "PASS" || sharpResult.scope !== "native Sharp RGB PNG encode/decode" ||
    sharpResult.width !== 1 || sharpResult.height !== 1 || sharpResult.channels !== 3 || sharpResult.pixelHex !== "020304") {
  throw new Error("Sharp native result is invalid");
}
if (!Array.isArray(sharpResult.loadedPackagePaths) || sharpResult.loadedPackagePaths.length !== 2 ||
    new Set(sharpResult.loadedPackagePaths).size !== 2 ||
    sharpResult.loadedPackagePaths.some((path) => !qualifiedPackageContents.some((item) => item.path === path))) {
  throw new Error("Sharp loaded packages do not have verified contents");
}
writeFileSync(join(directory, "platform-release-manifest.json"), candidateBytes, { flag: "wx" });
const receipt = {
  schemaVersion: 1,
  kind: "cloud-agents-worker-oci-smoke-receipt",
  status: "PASS",
  scope: "local-image-build-installation-and-entrypoints",
  candidate: { version: candidate.version, sourceCommit: candidate.sourceCommit, sourceDirty: candidate.sourceDirty, manifestSha256: hash(candidateBytes), artifacts: bindings },
  npmLs: { status: "PASS", basis: "npm ls --all --omit=dev exit 0" },
  qualifiedPackageContents,
  imageId: image.imageId,
  platform,
  inventoryStatus: inventory.status,
  fullSupplyChain: inventory.fullSupplyChain,
  artifacts: ["installed-inventory.json", "npm-ls.json", "image.json", "package-lock.json", "sharp-native.json", "platform-release-manifest.json"].map((filename) => ({
    filename,
    sha256: createHash("sha256").update(readFileSync(join(directory, filename))).digest("hex"),
  })),
};
writeFileSync(join(directory, "receipt.json"), `${JSON.stringify(receipt, null, 2)}\n`, { flag: "wx" });
console.log(`Worker installed inventory: ${inventory.status}; full supply chain: ${inventory.fullSupplyChain.status}`);
NODE

echo "platform OCI image smoke passed"
