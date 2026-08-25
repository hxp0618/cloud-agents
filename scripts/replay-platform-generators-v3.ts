import { createHash } from "node:crypto";
import { spawnSync } from "node:child_process";
import { lstatSync, readFileSync, readdirSync, readlinkSync, realpathSync } from "node:fs";
import { dirname, isAbsolute, relative, resolve, sep } from "node:path";
import {
  frameReplayReport,
  isContainedBy,
  requireEmptyReplayDirectory,
  requireExactDirectoryEntries,
  requireFreshReplayPath,
} from "./lib/generator-replay-path-authority";
import { SUCCESSOR_CORE_GENERATOR_OUTPUT_PATHS } from "./lib/platform-successor-dag";

const MANIFEST_ALGORITHM = "utf8-bytewise-sorted-path-nul-sha256-nul-git-mode-v1";
const CORE_PROJECTION_MANIFEST_ALGORITHM = "utf8-bytewise-sorted-path-mode-size-sha256-nul-v1";
const PROJECTION_ARCHIVE_MEMBER_MANIFEST_ALGORITHM =
  "utf8-bytewise-sorted-path-type-mode-size-sha256-linktarget-nul-v1";
const GENERATOR_OUTPUT_PATHS = SUCCESSOR_CORE_GENERATOR_OUTPUT_PATHS;
const GENERATOR_OUTPUT_PATH_SET = new Set<string>(GENERATOR_OUTPUT_PATHS);
const GENERATOR_COMMAND_TIMEOUT_MILLISECONDS = 600_000;
const TOOL_OUTPUT_TIMEOUT_MILLISECONDS = 60_000;
const args = process.argv.slice(2);
if (args.length !== 2 || args[0] !== "--run") {
  throw new Error("Usage: bun scripts/replay-platform-generators-v3.ts --run <root>");
}

const root = realpathSync(resolve(args[1]!));
const platform = requireEnvironment("CLOUD_AGENTS_GENERATOR_PLATFORM");
if (platform !== "darwin-arm64" && platform !== "linux-amd64") {
  throw new Error(`Unsupported generator replay platform ${platform}.`);
}
if (lstatExists(resolve(root, ".git"))) {
  throw new Error("Generator replay root must be a git archive without .git authority.");
}
const replayAuthoritySha256 = {
  wrapper: requireDigestEnvironment("CLOUD_AGENTS_GENERATOR_WRAPPER_SHA256"),
  runner: requireDigestEnvironment("CLOUD_AGENTS_GENERATOR_RUNNER_SHA256"),
  pathHelper: requireDigestEnvironment("CLOUD_AGENTS_GENERATOR_PATH_HELPER_SHA256"),
  archiveInspector: requireDigestEnvironment("CLOUD_AGENTS_GENERATOR_ARCHIVE_INSPECTOR_SHA256"),
};
verifyAuthorityDigest(
  "runner",
  resolve(root, "scripts/replay-platform-generators-v3.ts"),
  replayAuthoritySha256.runner,
);
verifyAuthorityDigest(
  "path helper",
  resolve(root, "scripts/lib/generator-replay-path-authority.ts"),
  replayAuthoritySha256.pathHelper,
);
verifyAuthorityDigest(
  "archive inspector",
  resolve(root, "scripts/lib/inspect-generator-replay-archive.py"),
  replayAuthoritySha256.archiveInspector,
);
const replayRun = requireEnvironment("CLOUD_AGENTS_GENERATOR_REPLAY_RUN");
if (replayRun !== "A" && replayRun !== "B") {
  throw new Error("Generator replay requires the exact fresh archive label A or B.");
}
const cacheLabel = replayRun.toLowerCase();
const sourceArchive = requireRegularFile("CLOUD_AGENTS_GENERATOR_PROJECTION_ARCHIVE");
const sourceArchiveSizeBytes = lstatSync(sourceArchive).size;
if (isContainedBy(root, sourceArchive)) {
  throw new Error("Generator replay projection archive must be external to its extraction root.");
}
const sourceArchiveSha256 = `sha256:${createHash("sha256")
  .update(readFileSync(sourceArchive))
  .digest("hex")}`;
if (
  sourceArchiveSha256 !== requireEnvironment("CLOUD_AGENTS_GENERATOR_PROJECTION_ARCHIVE_SHA256")
) {
  throw new Error("Generator replay projection archive digest drifted from wrapper authority.");
}
const projectionTreeSha = requireEnvironment("CLOUD_AGENTS_GENERATOR_PROJECTION_TREE_SHA");
if (!/^[0-9a-f]{40}$/u.test(projectionTreeSha)) {
  throw new Error("Generator replay projection tree authority must be one exact Git SHA-1.");
}
const materialEvidence = JSON.parse(
  readFileSync(resolve(root, "tools/generator-supply/v1/evidence/artifacts.json"), "utf8"),
) as {
  executables: Record<string, readonly { id: string; sha256: string }[]>;
};
const expectedExecutables = new Map(
  (materialEvidence.executables[platform] ?? []).map((entry) => [entry.id, entry.sha256]),
);
const archiveInspectorPython = verifiedExecutable(
  "CLOUD_AGENTS_PYTHON",
  expectedExecutables.get("python"),
);
const projectionInspection = inspectProjectionArchive(sourceArchive, archiveInspectorPython);
const projectionArchiveMembers = requirePositiveInteger(
  "projection archive member count",
  projectionInspection.entries,
);
if (projectionInspection.reconstructedGitTreeSha !== projectionTreeSha) {
  throw new Error("Generator replay projection archive does not reconstruct the exact Git tree.");
}
const projectionMetadataPath = requireRegularFile("CLOUD_AGENTS_GENERATOR_PROJECTION_METADATA");
if (isContainedBy(root, projectionMetadataPath)) {
  throw new Error("Generator replay projection metadata must be external to its extraction root.");
}
const projectionMetadata = JSON.parse(readFileSync(projectionMetadataPath, "utf8")) as {
  formatVersion: string;
  treeSha: string;
  archiveSha256: string;
  archiveSizeBytes: number;
  archiveInspection: ProjectionInspection;
};
if (
  projectionMetadata.formatVersion !== "cloud-agents-core-generator-projection/v1" ||
  projectionMetadata.treeSha !== projectionTreeSha ||
  projectionMetadata.archiveSha256 !== sourceArchiveSha256 ||
  projectionMetadata.archiveSizeBytes !== sourceArchiveSizeBytes ||
  JSON.stringify(projectionMetadata.archiveInspection) !== JSON.stringify(projectionInspection)
) {
  throw new Error("Generator replay projection metadata drifted from the exact archive authority.");
}
const nodeModules = resolve(root, "node_modules");
const nodeModulesBinding = requireNodeModulesBinding(nodeModules, root, platform);
const target = nodeModulesBinding.target;
const nodeModulesManifest = dependencyManifest(target);
const wheelhouse = realpathSync(requireEnvironment("CLOUD_AGENTS_CONTRACT_STANDARDS_WHEELHOUSE"));
if (!lstatSync(wheelhouse).isDirectory() || wheelhouse.startsWith(`${root}${sep}`)) {
  throw new Error("Generator replay wheelhouse must be one explicit external directory.");
}
const wheelhouseManifestSha256 = verifyWheelhouse(root, wheelhouse, platform);
const npmEvidence = JSON.parse(
  readFileSync(resolve(root, "tools/generator-supply/v1/evidence/npm.json"), "utf8"),
) as {
  installed: readonly {
    platform: string;
    packages: readonly { path: string }[];
    nodeModules: {
      algorithm: string;
      sha256: string;
      files: number;
      symlinks: readonly { path: string; target: string }[];
    };
  }[];
};
const expectedNodeModules = npmEvidence.installed.find(
  (entry) => entry.platform === platform,
)?.nodeModules;
const expectedInstalled = npmEvidence.installed.find((entry) => entry.platform === platform);
if (
  expectedNodeModules === undefined ||
  expectedNodeModules.algorithm !== MANIFEST_ALGORITHM ||
  expectedNodeModules.sha256 !== nodeModulesManifest.digest.replace(/^sha256:/u, "") ||
  expectedNodeModules.files !== nodeModulesManifest.files.length ||
  JSON.stringify(expectedNodeModules.symlinks) !== JSON.stringify(nodeModulesManifest.symlinks)
) {
  throw new Error(
    `Generator replay node_modules content manifest drifted from bound npm evidence: expected=${JSON.stringify(expectedNodeModules)} actual=${JSON.stringify({ algorithm: MANIFEST_ALGORITHM, sha256: nodeModulesManifest.digest.replace(/^sha256:/u, ""), files: nodeModulesManifest.files.length, symlinks: nodeModulesManifest.symlinks })}.`,
  );
}
if (expectedInstalled === undefined) {
  throw new Error("Generator replay npm installed package evidence is absent.");
}
const expectedTopLevelEntries = new Set([".bin", ".package-lock.json"]);
for (const installedPackage of expectedInstalled.packages) {
  const segments = installedPackage.path.split("/");
  if (
    segments[0] !== "node_modules" ||
    segments.length < 2 ||
    (segments[1]!.startsWith("@") && segments.length < 3)
  ) {
    throw new Error(`Generator replay npm package path is invalid: ${installedPackage.path}.`);
  }
  expectedTopLevelEntries.add(segments[1]!);
}
const bytewise = (left: string, right: string): number =>
  Buffer.from(left).compare(Buffer.from(right));
const actualTopLevelEntries = readdirSync(target).sort(bytewise);
if (
  JSON.stringify(actualTopLevelEntries) !==
  JSON.stringify([...expectedTopLevelEntries].sort(bytewise))
) {
  throw new Error(
    `Generator replay node_modules top-level closure drifted: ${JSON.stringify(actualTopLevelEntries)}.`,
  );
}

const runnerEnvironmentPolicy = requireEnvironment(
  "CLOUD_AGENTS_GENERATOR_RUNNER_ENVIRONMENT_POLICY",
);
if (runnerEnvironmentPolicy !== "ENV_I_MINIMAL_V1") {
  throw new Error("Generator replay runner itself must start in the exact empty-base environment.");
}
const isolation =
  platform === "darwin-arm64"
    ? "SANDBOX_EXEC_DENY_NETWORK_WITH_NEGATIVE_PROBES"
    : "UNSHARE_NETWORK_MOUNT_PID_PINNED_UBUNTU_READ_ONLY_ROOTFS";
if (
  requireEnvironment("CLOUD_AGENTS_GENERATOR_WRAPPER_POLICY") !== "VERSIONED_ISOLATION_WRAPPER_V3"
) {
  throw new Error("Generator replay must be launched by the versioned isolation wrapper policy.");
}
const runAuthority = requireRegularDirectory("CLOUD_AGENTS_GENERATOR_RUN_ROOT");
const replayHome = requireFreshReplayPath(
  "HOME",
  requireEnvironment("HOME"),
  runAuthority,
  `home-${cacheLabel}`,
);
const replayTmp = requireEmptyReplayDirectory(
  "TMPDIR",
  requireEnvironment("TMPDIR"),
  runAuthority,
  `tmp-${cacheLabel}`,
);
const uvCache = requireFreshReplayPath(
  "UV_CACHE_DIR",
  requireEnvironment("UV_CACHE_DIR"),
  runAuthority,
  `uv-cache-${cacheLabel}`,
);
const xdgCache = requireFreshReplayPath(
  "XDG_CACHE_HOME",
  requireEnvironment("XDG_CACHE_HOME"),
  runAuthority,
  `xdg-cache-${cacheLabel}`,
);
const childEnvironment = buildChildEnvironment({ replayHome, replayTmp, uvCache, xdgCache });
validateRunnerEnvironment(childEnvironment);

const before = treeManifest(root);
const inputTreeManifest = before;
if (
  projectionInspection.regularFileManifestAlgorithm !== CORE_PROJECTION_MANIFEST_ALGORITHM ||
  `sha256:${projectionInspection.regularFileManifestSha256}` !== inputTreeManifest.digest ||
  projectionInspection.regularFiles !== inputTreeManifest.files.length
) {
  throw new Error(
    "Generator replay projection archive regular-file content differs from the extracted input tree.",
  );
}
const candidateOutputs = outputManifest(before.files);
const versions = verifyToolVersions(materialEvidence);
const externalExecutableSetSha256 = executableSetDigest(materialEvidence);
const loadedBinding = verifyNativeFormatterBinding(root, platform);
runCoreGeneratorReplay("--write");
assertNodeModulesBinding(nodeModules, nodeModulesBinding, root, platform);
runCoreGeneratorReplay("--check");
assertNodeModulesBinding(nodeModules, nodeModulesBinding, root, platform);
const nodeModulesManifestAfterReplay = dependencyManifest(target);
if (JSON.stringify(nodeModulesManifestAfterReplay) !== JSON.stringify(nodeModulesManifest)) {
  throw new Error("Generator replay mutated the external node_modules supply closure.");
}
if (verifyWheelhouse(root, wheelhouse, platform) !== wheelhouseManifestSha256) {
  throw new Error("Generator replay mutated the external wheelhouse supply closure.");
}
if (
  canonicalStringify(verifyToolVersions(materialEvidence)) !== canonicalStringify(versions) ||
  executableSetDigest(materialEvidence) !== externalExecutableSetSha256
) {
  throw new Error("Generator replay mutated the external executable supply closure.");
}
if (verifyNativeFormatterBinding(root, platform) !== loadedBinding) {
  throw new Error("Generator replay changed the effective native formatter binding.");
}
const archiveSha256AfterReplay = `sha256:${createHash("sha256")
  .update(readFileSync(sourceArchive))
  .digest("hex")}`;
if (
  archiveSha256AfterReplay !== sourceArchiveSha256 ||
  JSON.stringify(inspectProjectionArchive(sourceArchive, archiveInspectorPython)) !==
    JSON.stringify(projectionInspection)
) {
  throw new Error("Generator replay mutated the external projection archive authority.");
}
const after = treeManifest(root);
if (
  before.digest !== after.digest ||
  JSON.stringify(before.files) !== JSON.stringify(after.files)
) {
  const beforeMap = new Map(before.files.map((file) => [file.path, `${file.mode}:${file.sha256}`]));
  const changes = after.files
    .filter((file) => beforeMap.get(file.path) !== `${file.mode}:${file.sha256}`)
    .map((file) => file.path);
  throw new Error(
    `Generator replay changed candidate bytes outside exact replay: ${changes.join(",")}`,
  );
}
const replayOutputs = outputManifest(after.files);
if (
  candidateOutputs.digest !== replayOutputs.digest ||
  JSON.stringify(candidateOutputs.files) !== JSON.stringify(replayOutputs.files)
) {
  throw new Error("Generator replay output manifest differs from checked-in candidate outputs.");
}

const report = {
  formatVersion: "cloud-agents-generator-replay-run/v1",
  platform,
  replayRun,
  manifestAlgorithm: MANIFEST_ALGORITHM,
  perCommandTimeoutMilliseconds: GENERATOR_COMMAND_TIMEOUT_MILLISECONDS,
  archiveHasGitDirectory: false,
  projectionTreeSha,
  projectionArchiveSha256: sourceArchiveSha256,
  projectionArchiveSizeBytes: sourceArchiveSizeBytes,
  projectionArchiveMemberManifestAlgorithm: PROJECTION_ARCHIVE_MEMBER_MANIFEST_ALGORITHM,
  projectionArchiveMemberManifestSha256: `sha256:${projectionInspection.manifestSha256}`,
  projectionArchiveMembers,
  inputTreeManifestAlgorithm: CORE_PROJECTION_MANIFEST_ALGORITHM,
  inputTreeManifestSha256: inputTreeManifest.digest,
  inputTreeFiles: inputTreeManifest.files.length,
  freshExtractionRoot: `generator-supply://core-projection/${cacheLabel}`,
  extractionRootInitiallyAbsent: true,
  ambientNodeModules: false,
  nodeModulesManifestSha256: nodeModulesManifest.digest,
  nodeModulesFiles: nodeModulesManifest.files.length,
  wheelhouseManifestSha256,
  externalExecutableSetSha256,
  isolation,
  isolationEvidenceAuthority: "VERSIONED_WRAPPER_SAME_BOUNDARY_RECEIPT",
  environmentPolicy: "MINIMAL_EXACT_V1",
  runnerEnvironmentPolicy,
  runnerEnvironmentSanitized: true,
  freshPerReplayCaches: true,
  ephemeralCachePolicy:
    platform === "linux-amd64"
      ? "FRESH_PER_REPLAY_TMPFS_ONLY"
      : "FRESH_PER_REPLAY_SANDBOX_TASK_OWNED",
  homeDirectory: `generator-supply://ephemeral/${cacheLabel}/home`,
  temporaryDirectory: `generator-supply://ephemeral/${cacheLabel}/tmp`,
  uvCacheDirectory: `generator-supply://ephemeral/${cacheLabel}/uv-cache`,
  xdgCacheHome: `generator-supply://ephemeral/${cacheLabel}/xdg-cache`,
  versions,
  loadedOxfmtBinding: loadedBinding,
  candidateManifestSha256: candidateOutputs.digest,
  replayManifestSha256: replayOutputs.digest,
  outputFiles: replayOutputs.files.length,
  candidateOutputsEqual: true,
  nonAllowlistedChanges: 0,
  replayAuthoritySha256,
};
process.stdout.write(frameReplayReport(report));

function verifyToolVersions(material: {
  executables: Record<string, readonly { id: string; sha256: string }[]>;
}): Record<string, string> {
  const expectedExecutables = new Map(
    (material.executables[platform] ?? []).map((entry) => [entry.id, entry.sha256]),
  );
  const tools = {
    node: verifiedExecutable("CLOUD_AGENTS_NODE", expectedExecutables.get("node")),
    bun: verifiedExecutable("CLOUD_AGENTS_BUN", expectedExecutables.get("bun")),
    go: verifiedExecutable("CLOUD_AGENTS_GO", expectedExecutables.get("go")),
    gofmt: verifiedExecutable("CLOUD_AGENTS_GOFMT", expectedExecutables.get("gofmt")),
    python: verifiedExecutable("CLOUD_AGENTS_PYTHON", expectedExecutables.get("python")),
    uv: verifiedExecutable("CLOUD_AGENTS_UV", expectedExecutables.get("uv")),
    protoc: verifiedExecutable("CLOUD_AGENTS_PROTOC", expectedExecutables.get("protoc")),
    protocGenGo: verifiedExecutable(
      "CLOUD_AGENTS_PROTOC_GEN_GO",
      expectedExecutables.get("protoc-gen-go"),
    ),
    protocGenConnectGo: verifiedExecutable(
      "CLOUD_AGENTS_PROTOC_GEN_CONNECT_GO",
      expectedExecutables.get("protoc-gen-connect-go"),
    ),
  };
  const values = {
    node: output(tools.node, ["--version"]).replace(/^v/u, ""),
    bun: output(tools.bun, ["--version"]),
    go: output(tools.go, ["version"]),
    python: output(tools.python, ["--version"]).replace(/^Python\s+/u, ""),
    uv: output(tools.uv, ["--version"]).match(/^uv\s+(\S+)/u)?.[1] ?? "",
    protoc: output(tools.protoc, ["--version"]).replace(/^libprotoc\s+/u, ""),
    protocGenGo: output(tools.protocGenGo, ["--version"]).replace(/^protoc-gen-go\s+v/u, ""),
    protocGenConnectGo: output(tools.protocGenConnectGo, ["--version"]),
  };
  const expectedVersions = {
    node: "24.18.1",
    bun: "1.3.14",
    python: "3.14.7",
    uv: "0.12.5",
    protoc: "35.1",
    protocGenGo: "1.36.12",
    protocGenConnectGo: "1.20.0",
  };
  for (const [key, expectedValue] of Object.entries(expectedVersions)) {
    if (values[key as keyof typeof values] !== expectedValue) {
      throw new Error(`Generator replay ${key} version drifted.`);
    }
  }
  if (!values.go.startsWith("go version go1.26.6 ")) {
    throw new Error("Generator replay Go version drifted.");
  }
  return values;
}

function executableSetDigest(material: {
  executables: Record<string, readonly { id: string; sha256: string }[]>;
}): string {
  const entries = [...(material.executables[platform] ?? [])].sort((left, right) =>
    Buffer.from(left.id).compare(Buffer.from(right.id)),
  );
  const digest = createHash("sha256");
  for (const entry of entries) {
    digest.update(entry.id).update("\0").update(entry.sha256).update("\0");
  }
  return `sha256:${digest.digest("hex")}`;
}

function canonicalStringify(value: unknown): string {
  return JSON.stringify(value, Object.keys(value as Record<string, unknown>).sort());
}

function verifyNativeFormatterBinding(repositoryRoot: string, currentPlatform: string): string {
  const expected =
    currentPlatform === "darwin-arm64"
      ? "@oxfmt/binding-darwin-arm64"
      : "@oxfmt/binding-linux-x64-gnu";
  const script = `
const expected = process.argv[1];
const binding = require.resolve(expected);
const formatter = require("oxfmt");
if (typeof formatter.format !== "function") throw new Error("oxfmt format is absent");
process.stdout.write(binding);
`;
  const binding = output(requireEnvironment("CLOUD_AGENTS_NODE"), ["-e", script, expected], {
    NODE_PATH: resolve(repositoryRoot, "node_modules"),
  });
  if (!binding.includes(expected.replace("@oxfmt/", ""))) {
    throw new Error(`Expected native formatter binding ${expected}, received ${binding}.`);
  }
  return expected;
}

function verifyWheelhouse(
  repositoryRoot: string,
  directory: string,
  currentPlatform: string,
): string {
  const evidence = JSON.parse(
    readFileSync(resolve(repositoryRoot, "tools/generator-supply/v1/evidence/wheels.json"), "utf8"),
  ) as {
    platforms: readonly {
      platform: string;
      wheels: readonly { filename: string; sha256: string; sizeBytes: number }[];
    }[];
  };
  const expected = evidence.platforms.find((entry) => entry.platform === currentPlatform)?.wheels;
  if (expected === undefined) throw new Error("Generator replay wheelhouse evidence is absent.");
  requireExactDirectoryEntries(
    "Generator replay wheelhouse",
    directory,
    expected.map((entry) => entry.filename),
  );
  for (const wheel of expected) {
    const path = resolve(directory, wheel.filename);
    const metadata = lstatSync(path);
    if (!metadata.isFile() || metadata.isSymbolicLink() || realpathSync(path) !== path) {
      throw new Error(`Generator replay wheel ${wheel.filename} must be regular and non-symlink.`);
    }
    const bytes = readFileSync(path);
    if (
      bytes.byteLength !== wheel.sizeBytes ||
      createHash("sha256").update(bytes).digest("hex") !== wheel.sha256
    ) {
      throw new Error(`Generator replay wheel ${wheel.filename} bytes drifted.`);
    }
  }
  const digest = createHash("sha256");
  for (const wheel of expected) {
    digest
      .update(wheel.filename)
      .update("\0")
      .update(String(wheel.sizeBytes))
      .update("\0")
      .update(wheel.sha256)
      .update("\0");
  }
  return `sha256:${digest.digest("hex")}`;
}

function runCoreGeneratorReplay(mode: "--write" | "--check"): void {
  const bun = requireEnvironment("CLOUD_AGENTS_BUN");
  const scripts = [
    "scripts/check-platform-ajv-official-suite.ts",
    "scripts/generate-platform-contract-closure-profile.ts",
    "scripts/generate-platform-durable-coordination-registry.ts",
    "scripts/generate-platform-durable-coordination-go.ts",
    "scripts/generate-platform-compatibility-recovery-registry.ts",
    "scripts/generate-platform-compatibility-recovery-go.ts",
    "scripts/generate-platform-runner-ledger-preflight-registry.ts",
    "scripts/generate-platform-runner-ledger-preflight-go.ts",
    "scripts/generate-platform-runner-ledger-consumer-registry.ts",
    "scripts/generate-platform-runner-ledger-consumer-go.ts",
    "scripts/generate-platform-runner-ledger-entry-admission-registry.ts",
    "scripts/generate-platform-runner-ledger-entry-admission-go.ts",
    "scripts/generate-platform-runner-ledger-entry-writer-registries.ts",
    "scripts/generate-platform-runner-ledger-entry-writer-go.ts",
    "scripts/generate-platform-runner-ledger-recovery-registries.ts",
    "scripts/generate-platform-runner-ledger-recovery-go.ts",
    "scripts/generate-platform-identity-sdks.ts",
    "scripts/generate-platform-json-sdks.ts",
    "scripts/generate-platform-proto-sdks.ts",
  ];
  if (mode === "--check") run(bun, ["scripts/check-platform-contract-standards.ts"]);
  for (const script of scripts) run(bun, [script, mode]);
}

function treeManifest(repositoryRoot: string): {
  digest: string;
  files: readonly { path: string; mode: string; size: number; sha256: string }[];
} {
  if (repositoryRoot === root) {
    assertNodeModulesBinding(nodeModules, nodeModulesBinding, root, platform);
  }
  const files: { path: string; mode: string; size: number; sha256: string }[] = [];
  visit(repositoryRoot, "", files);
  files.sort((left, right) => Buffer.from(left.path).compare(Buffer.from(right.path)));
  const digest = createHash("sha256");
  for (const file of files) {
    digest
      .update(file.path)
      .update("\0")
      .update(file.mode)
      .update("\0")
      .update(String(file.size))
      .update("\0")
      .update(file.sha256)
      .update("\0");
  }
  return { digest: `sha256:${digest.digest("hex")}`, files };
}

function outputManifest(
  files: readonly { path: string; mode: string; size: number; sha256: string }[],
): {
  digest: string;
  files: readonly { path: string; mode: string; size: number; sha256: string }[];
} {
  const selected = files.filter((file) => GENERATOR_OUTPUT_PATH_SET.has(file.path));
  if (selected.length !== GENERATOR_OUTPUT_PATHS.length) {
    const selectedPaths = new Set(selected.map((file) => file.path));
    const missing = GENERATOR_OUTPUT_PATHS.filter((path) => !selectedPaths.has(path));
    throw new Error(`Generator replay exact output closure is missing: ${missing.join(",")}.`);
  }
  const digest = createHash("sha256");
  for (const file of selected) {
    digest
      .update(file.path)
      .update("\0")
      .update(file.sha256)
      .update("\0")
      .update(file.mode)
      .update("\0");
  }
  return { digest: `sha256:${digest.digest("hex")}`, files: selected };
}

type ProjectionInspection = {
  formatVersion: string;
  profile: string;
  manifestAlgorithm: string;
  manifestSha256: string;
  entries: PositiveInteger;
  regularFiles: number;
  directories: number;
  symlinks: number;
  hardlinks: number;
  unsafeEntries: number;
  duplicateEntries: number;
  specialEntries: number;
  linkPrefixDescendants: number;
  linkCycles: number;
  regularFileManifestAlgorithm: string;
  regularFileManifestSha256: string;
  reconstructedGitTreeSha: string;
};

type PositiveInteger = number & { readonly __positiveInteger: unique symbol };

function requirePositiveInteger(name: string, value: unknown): PositiveInteger {
  if (typeof value !== "number" || !Number.isSafeInteger(value) || value <= 0) {
    throw new Error(`Generator replay ${name} must be one positive safe integer.`);
  }
  return value as PositiveInteger;
}

function inspectProjectionArchive(archive: string, python: string): ProjectionInspection {
  const inspector = resolve(root, "scripts/lib/inspect-generator-replay-archive.py");
  const result = spawnSync(python, [inspector, "core-projection", archive], {
    encoding: "utf8",
    env: { PATH: "/usr/bin:/bin", LC_ALL: "C", LANG: "C", TZ: "UTC" },
    timeout: TOOL_OUTPUT_TIMEOUT_MILLISECONDS,
    killSignal: "SIGTERM",
  });
  if (result.error || result.status !== 0) {
    throw new Error(
      `Generator replay could not inspect the projection archive: ${String(result.error ?? result.stderr)}`,
    );
  }
  const inspection = JSON.parse(result.stdout) as ProjectionInspection;
  if (
    inspection.formatVersion !== "cloud-agents-generator-replay-archive-inspection/v1" ||
    inspection.profile !== "core-projection" ||
    inspection.manifestAlgorithm !== PROJECTION_ARCHIVE_MEMBER_MANIFEST_ALGORITHM ||
    !Number.isSafeInteger(inspection.entries) ||
    inspection.entries <= 0 ||
    inspection.regularFiles <= 0 ||
    inspection.symlinks !== 0 ||
    inspection.hardlinks !== 0 ||
    inspection.unsafeEntries !== 0 ||
    inspection.duplicateEntries !== 0 ||
    inspection.specialEntries !== 0 ||
    inspection.linkPrefixDescendants !== 0 ||
    inspection.linkCycles !== 0 ||
    !/^[0-9a-f]{64}$/u.test(inspection.manifestSha256) ||
    !/^[0-9a-f]{64}$/u.test(inspection.regularFileManifestSha256) ||
    !/^[0-9a-f]{40}$/u.test(inspection.reconstructedGitTreeSha)
  ) {
    throw new Error("Generator replay projection archive inspection is not exact fail-closed v1.");
  }
  return inspection;
}

function visit(
  repositoryRoot: string,
  relativeDirectory: string,
  files: { path: string; mode: string; size: number; sha256: string }[],
): void {
  const directory = resolve(repositoryRoot, relativeDirectory);
  for (const name of readdirSync(directory).toSorted()) {
    if (relativeDirectory === "" && name === ".git") {
      throw new Error("Generator replay root must not contain a .git authority path.");
    }
    if (relativeDirectory === "" && name === "node_modules") continue;
    const path = relativeDirectory === "" ? name : `${relativeDirectory}/${name}`;
    const absolute = resolve(repositoryRoot, path);
    const metadata = lstatSync(absolute);
    if (metadata.isDirectory()) {
      visit(repositoryRoot, path, files);
    } else if (metadata.isFile()) {
      files.push({
        path,
        mode: metadata.mode & 0o111 ? "100755" : "100644",
        size: metadata.size,
        sha256: createHash("sha256").update(readFileSync(absolute)).digest("hex"),
      });
    } else if (metadata.isSymbolicLink()) {
      throw new Error(`Core projection input must not contain a symlink ${path}.`);
    } else {
      throw new Error(`Unsupported archive entry ${path}.`);
    }
  }
}

function run(command: string, commandArgs: readonly string[]): void {
  assertAuthorityFilesUnchanged();
  assertReplayTreeUnchanged();
  const effectiveCommand = platform === "linux-amd64" ? "/usr/bin/setpriv" : command;
  const effectiveArguments =
    platform === "linux-amd64"
      ? [
          "--reuid=65534",
          "--regid=65534",
          "--clear-groups",
          "--bounding-set=-all",
          "--inh-caps=-all",
          "--ambient-caps=-all",
          "--no-new-privs",
          "--",
          command,
          ...commandArgs,
        ]
      : commandArgs;
  const result = spawnSync(effectiveCommand, effectiveArguments, {
    cwd: root,
    env: childEnvironment,
    // Core generators cannot inherit the replay report channel (fd 1).
    stdio: ["ignore", 2, 2],
    timeout: GENERATOR_COMMAND_TIMEOUT_MILLISECONDS,
    killSignal: "SIGTERM",
  });
  if (result.error) {
    throw new Error(
      `${command} ${commandArgs.join(" ")} failed or timed out after ${GENERATOR_COMMAND_TIMEOUT_MILLISECONDS}ms: ${String(result.error)}`,
    );
  }
  if (result.status !== 0) {
    throw new Error(
      `${command} ${commandArgs.join(" ")} failed: status=${String(result.status)} signal=${String(result.signal)}.`,
    );
  }
  assertNoCandidateProcesses();
  assertAuthorityFilesUnchanged();
  assertReplayTreeUnchanged();
}

function assertAuthorityFilesUnchanged(): void {
  verifyAuthorityDigest(
    "runner",
    resolve(root, "scripts/replay-platform-generators-v3.ts"),
    replayAuthoritySha256.runner,
  );
  verifyAuthorityDigest(
    "path helper",
    resolve(root, "scripts/lib/generator-replay-path-authority.ts"),
    replayAuthoritySha256.pathHelper,
  );
  verifyAuthorityDigest(
    "archive inspector",
    resolve(root, "scripts/lib/inspect-generator-replay-archive.py"),
    replayAuthoritySha256.archiveInspector,
  );
}

function assertReplayTreeUnchanged(): void {
  const current = treeManifest(root);
  if (
    current.digest !== inputTreeManifest.digest ||
    JSON.stringify(current.files) !== JSON.stringify(inputTreeManifest.files)
  ) {
    throw new Error(
      "Generator replay child changed the full candidate tree outside the exact baseline.",
    );
  }
}

function assertNoCandidateProcesses(): void {
  if (platform !== "linux-amd64") return;
  const candidatePids: number[] = [];
  for (const name of readdirSync("/proc")) {
    if (!/^[0-9]+$/u.test(name)) continue;
    const pid = Number(name);
    if (pid === process.pid) continue;
    try {
      const status = readFileSync(`/proc/${name}/status`, "utf8");
      const uid = /^Uid:\s+(\d+)/mu.exec(status)?.[1];
      if (uid === "65534") candidatePids.push(pid);
    } catch {
      // A process can exit between readdir and status read; that is safe.
    }
  }
  if (candidatePids.length !== 0) {
    for (const pid of candidatePids) {
      try {
        process.kill(pid, "SIGKILL");
      } catch {
        // The process may have exited while the trusted runner was closing it.
      }
    }
    throw new Error(
      `Generator replay left candidate processes in the isolated PID namespace: ${candidatePids.join(",")}.`,
    );
  }
}

function output(
  command: string,
  commandArgs: readonly string[],
  extraEnvironment: Readonly<Record<string, string>> = {},
): string {
  const result = spawnSync(command, commandArgs, {
    cwd: root,
    env: { ...childEnvironment, ...extraEnvironment },
    encoding: "utf8",
    timeout: TOOL_OUTPUT_TIMEOUT_MILLISECONDS,
    killSignal: "SIGTERM",
  });
  if (result.error) {
    throw new Error(
      `${command} ${commandArgs.join(" ")} failed or timed out after ${TOOL_OUTPUT_TIMEOUT_MILLISECONDS}ms: ${String(result.error)}`,
    );
  }
  if (result.status !== 0) {
    throw new Error(`${command} ${commandArgs.join(" ")} failed: ${result.stdout}${result.stderr}`);
  }
  return result.stdout.trim();
}

function requireEnvironment(name: string): string {
  const value = process.env[name];
  if (value === undefined || value === "" || (!isAbsolute(value) && name.endsWith("_PATH"))) {
    throw new Error(`Generator replay requires ${name}.`);
  }
  return value;
}

function requireDigestEnvironment(name: string): string {
  const value = requireEnvironment(name);
  if (!/^sha256:[0-9a-f]{64}$/u.test(value)) {
    throw new Error(`Generator replay requires ${name} to be one exact SHA-256 digest.`);
  }
  return value;
}

function sha256RegularFile(path: string): string {
  const metadata = lstatSync(path);
  if (!metadata.isFile() || metadata.isSymbolicLink() || realpathSync(path) !== path) {
    throw new Error(`Generator replay authority file must be regular and non-symlink: ${path}.`);
  }
  return `sha256:${createHash("sha256").update(readFileSync(path)).digest("hex")}`;
}

function verifyAuthorityDigest(name: string, path: string, expected: string): void {
  const actual = sha256RegularFile(path);
  if (actual !== expected) {
    throw new Error(
      `Generator replay ${name} authority digest drifted: expected=${expected} actual=${actual}.`,
    );
  }
}

type NodeModulesBinding = {
  kind: "external-symlink" | "linux-ro-bind-directory";
  target: string;
  linkTarget: string | undefined;
};

function requireNodeModulesBinding(
  path: string,
  repositoryRoot: string,
  currentPlatform: string,
): NodeModulesBinding {
  const metadata = lstatSync(path);
  if (currentPlatform === "darwin-arm64") {
    if (!metadata.isSymbolicLink()) {
      throw new Error(
        "Generator replay Darwin node_modules must be one explicit external symlink.",
      );
    }
    const linkTarget = readlinkSync(path);
    const target = realpathSync(path);
    if (!lstatSync(target).isDirectory() || target.startsWith(`${repositoryRoot}${sep}`)) {
      throw new Error(
        "Generator replay Darwin node_modules must point to an external effective supply bundle.",
      );
    }
    return { kind: "external-symlink", target, linkTarget };
  }
  if (
    !metadata.isDirectory() ||
    metadata.isSymbolicLink() ||
    process.env.CLOUD_AGENTS_GENERATOR_NODE_MODULES_BIND_MODE !== "RO_BIND_MOUNT_V1"
  ) {
    throw new Error(
      "Generator replay Linux node_modules must be one explicit read-only bind directory.",
    );
  }
  return { kind: "linux-ro-bind-directory", target: path, linkTarget: undefined };
}

function assertNodeModulesBinding(
  path: string,
  expected: NodeModulesBinding,
  repositoryRoot: string,
  currentPlatform: string,
): void {
  const actual = requireNodeModulesBinding(path, repositoryRoot, currentPlatform);
  if (
    actual.kind !== expected.kind ||
    actual.target !== expected.target ||
    actual.linkTarget !== expected.linkTarget
  ) {
    throw new Error("Generator replay node_modules binding identity changed during replay.");
  }
}

function requireRegularDirectory(name: string): string {
  const requested = requireEnvironment(name);
  if (!isAbsolute(requested)) {
    throw new Error(`Generator replay requires ${name} to be absolute.`);
  }
  const path = realpathSync(requested);
  const metadata = lstatSync(path);
  if (!metadata.isDirectory() || metadata.isSymbolicLink() || path !== requested) {
    throw new Error(`Generator replay requires ${name} to be a regular non-symlink directory.`);
  }
  return path;
}

function requireRegularFile(name: string): string {
  const path = requireEnvironment(name);
  if (!isAbsolute(path)) throw new Error(`Generator replay requires ${name} to be absolute.`);
  const metadata = lstatSync(path);
  if (!metadata.isFile() || metadata.isSymbolicLink() || realpathSync(path) !== path) {
    throw new Error(`Generator replay requires ${name} to be a regular non-symlink file.`);
  }
  return path;
}

function buildChildEnvironment(paths: {
  replayHome: string;
  replayTmp: string;
  uvCache: string;
  xdgCache: string;
}): NodeJS.ProcessEnv {
  const toolVariables = [
    "CLOUD_AGENTS_NODE",
    "CLOUD_AGENTS_BUN",
    "CLOUD_AGENTS_GO",
    "CLOUD_AGENTS_GOFMT",
    "CLOUD_AGENTS_PYTHON",
    "CLOUD_AGENTS_UV",
    "CLOUD_AGENTS_PROTOC",
    "CLOUD_AGENTS_PROTOC_GEN_GO",
    "CLOUD_AGENTS_PROTOC_GEN_CONNECT_GO",
  ] as const;
  const tools = Object.fromEntries(toolVariables.map((name) => [name, requireEnvironment(name)]));
  const pathDirectories = [
    ...toolVariables.map((name) => dirname(tools[name]!)),
    "/usr/bin",
    "/bin",
  ].filter((value, index, values) => values.indexOf(value) === index);
  return {
    ...tools,
    CLOUD_AGENTS_GENERATOR_PLATFORM: platform,
    CLOUD_AGENTS_GENERATOR_REPLAY_RUN: replayRun,
    CLOUD_AGENTS_GENERATOR_RUNNER_ENVIRONMENT_POLICY: runnerEnvironmentPolicy,
    CLOUD_AGENTS_GENERATOR_NODE_MODULES_BIND_MODE:
      platform === "linux-amd64" ? "RO_BIND_MOUNT_V1" : "EXTERNAL_SYMLINK_V1",
    CLOUD_AGENTS_GENERATOR_RUNNER_SHA256: replayAuthoritySha256.runner,
    CLOUD_AGENTS_GENERATOR_PATH_HELPER_SHA256: replayAuthoritySha256.pathHelper,
    CLOUD_AGENTS_GENERATOR_ARCHIVE_INSPECTOR_SHA256: replayAuthoritySha256.archiveInspector,
    CLOUD_AGENTS_CONTRACT_STANDARDS_WHEELHOUSE: wheelhouse,
    PATH: pathDirectories.join(":"),
    HOME: paths.replayHome,
    TMPDIR: paths.replayTmp,
    UV_CACHE_DIR: paths.uvCache,
    XDG_CACHE_HOME: paths.xdgCache,
    UV_NO_CONFIG: "1",
    LC_ALL: "C",
    LANG: "C",
    TZ: "UTC",
    GOTOOLCHAIN: "local",
    GOWORK: "off",
    GOFLAGS: "-mod=readonly",
    NO_COLOR: "1",
  };
}

function validateRunnerEnvironment(expectedChildEnvironment: NodeJS.ProcessEnv): void {
  const allowed = new Set([
    ...Object.keys(expectedChildEnvironment),
    "CLOUD_AGENTS_GENERATOR_PROJECTION_ARCHIVE",
    "CLOUD_AGENTS_GENERATOR_PROJECTION_ARCHIVE_SHA256",
    "CLOUD_AGENTS_GENERATOR_PROJECTION_METADATA",
    "CLOUD_AGENTS_GENERATOR_PROJECTION_TREE_SHA",
    "CLOUD_AGENTS_GENERATOR_RUN_ROOT",
    "CLOUD_AGENTS_GENERATOR_WRAPPER_POLICY",
    "CLOUD_AGENTS_GENERATOR_WRAPPER_SHA256",
  ]);
  const unexpected = Object.keys(process.env)
    .filter((name) => !allowed.has(name))
    .toSorted();
  if (unexpected.length !== 0) {
    throw new Error(
      `Generator replay runner environment is not empty-base minimal: ${unexpected.join(",")}.`,
    );
  }
  for (const [name, value] of Object.entries(expectedChildEnvironment)) {
    if (process.env[name] !== value) {
      throw new Error(`Generator replay runner environment drifted for ${name}.`);
    }
  }
}

function dependencyManifest(directory: string): {
  digest: string;
  files: readonly { path: string; mode: string; sha256: string }[];
  symlinks: readonly { path: string; target: string }[];
} {
  const directoryReal = realpathSync(directory);
  const files: { path: string; mode: string; sha256: string }[] = [];
  const allowedSymlinks = new Map([
    [".bin/oxfmt", "../oxfmt/bin/oxfmt"],
    [".bin/protoc-gen-es", "../@bufbuild/protoc-gen-es/bin/protoc-gen-es"],
    [".bin/tsc", "../typescript/bin/tsc"],
    [".bin/tsserver", "../typescript/bin/tsserver"],
  ]);
  const symlinks: { path: string; target: string }[] = [];
  const visitDependency = (current: string, prefix: string): void => {
    for (const name of readdirSync(current).toSorted()) {
      const path = prefix === "" ? name : `${prefix}/${name}`;
      const absolute = resolve(current, name);
      const metadata = lstatSync(absolute);
      if (metadata.isSymbolicLink()) {
        const target = readlinkSync(absolute);
        const expected = allowedSymlinks.get(path);
        const targetAbsolute = resolve(dirname(absolute), target);
        const containment = relative(directory, targetAbsolute);
        const targetReal = realpathSync(targetAbsolute);
        const realContainment = relative(directoryReal, targetReal);
        if (
          expected !== target ||
          isAbsolute(target) ||
          containment === ".." ||
          containment.startsWith(`..${sep}`) ||
          isAbsolute(containment) ||
          realContainment === ".." ||
          realContainment.startsWith(`..${sep}`) ||
          isAbsolute(realContainment)
        ) {
          throw new Error(`Generator replay node_modules has an escaping symlink ${path}.`);
        }
        const targetMetadata = lstatSync(targetAbsolute);
        if (!targetMetadata.isFile() || targetMetadata.isSymbolicLink()) {
          throw new Error(`Generator replay symlink ${path} must resolve once to a regular file.`);
        }
        files.push({
          path,
          mode: "120000",
          sha256: createHash("sha256").update(target, "utf8").digest("hex"),
        });
        symlinks.push({ path, target });
      } else if (metadata.isDirectory()) {
        visitDependency(absolute, path);
      } else if (metadata.isFile()) {
        files.push({
          path,
          mode: metadata.mode & 0o111 ? "100755" : "100644",
          sha256: createHash("sha256").update(readFileSync(absolute)).digest("hex"),
        });
      } else {
        throw new Error(`Generator replay node_modules has unsupported entry ${path}.`);
      }
    }
  };
  visitDependency(directory, "");
  files.sort((left, right) => Buffer.from(left.path).compare(Buffer.from(right.path)));
  symlinks.sort((left, right) => Buffer.from(left.path).compare(Buffer.from(right.path)));
  if (
    JSON.stringify(symlinks) !==
    JSON.stringify([...allowedSymlinks].map(([path, target]) => ({ path, target })))
  ) {
    throw new Error("Generator replay requires the exact npm-created .bin symlink set.");
  }
  const digest = createHash("sha256");
  for (const file of files) {
    digest
      .update(file.path)
      .update("\0")
      .update(file.sha256)
      .update("\0")
      .update(file.mode)
      .update("\0");
  }
  return { digest: `sha256:${digest.digest("hex")}`, files, symlinks };
}

function verifiedExecutable(name: string, expectedSha256: string | undefined): string {
  const path = requireEnvironment(name);
  if (!isAbsolute(path)) throw new Error(`Generator replay ${name} must be absolute.`);
  const metadata = lstatSync(path);
  if (!metadata.isFile() || metadata.isSymbolicLink() || realpathSync(path) !== path) {
    throw new Error(`Generator replay ${name} must be a regular non-symlink executable.`);
  }
  const actual = createHash("sha256").update(readFileSync(path)).digest("hex");
  if (expectedSha256 === undefined || actual !== expectedSha256) {
    throw new Error(`Generator replay ${name} bytes drifted from artifacts evidence.`);
  }
  return path;
}

function lstatExists(path: string): boolean {
  try {
    lstatSync(path);
    return true;
  } catch {
    return false;
  }
}
