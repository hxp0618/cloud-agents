import { createHash } from "node:crypto";
import {
  existsSync,
  lstatSync,
  mkdirSync,
  readFileSync,
  readdirSync,
  realpathSync,
  renameSync,
  writeFileSync,
} from "node:fs";
import { dirname, isAbsolute, relative, resolve, sep } from "node:path";
import { fileURLToPath } from "node:url";
import { spawnSync } from "node:child_process";

const moduleRepoRoot = resolve(dirname(fileURLToPath(import.meta.url)), "../..");
const hex40 = /^[0-9a-f]{40}$/;
const hex64 = /^[0-9a-f]{64}$/;
const imageIdPattern = /^sha256:[0-9a-f]{64}$/;
const digestImagePattern = /@sha256:[0-9a-f]{64}$/;
const childCommandTimeoutMs = 540_000;

export const OPEN_SANDBOX_EXECD_RECIPE_PATH = "tools/opensandbox-execd-successor/v1/source.json";
export const OPEN_SANDBOX_EXECD_BUILD_EVIDENCE_FILENAME = "build-evidence.json";

export type OpenSandboxExecdSuccessorRecipe = {
  schemaVersion: "cloud-agents.opensandbox-execd-successor-source.v1";
  upstream: {
    repository: string;
    commit: string;
    tree: string;
    licensePath: string;
    licenseCopyPath: string;
    licenseSha256: string;
  };
  patch: { path: string; sha256: string };
  toolchain: { image: string; goVersion: string; goProxy: string; goSumDb: string };
  runtimeBase: {
    image: string;
    sourceStatus: "UNKNOWN_BUILD_SOURCE";
    use: "runtime-assets-only";
  };
  build: {
    dockerfile: string;
    dockerfileSha256: string;
    platform: "linux/arm64";
    version: string;
    buildTime: string;
  };
  recipePath: string;
  recipeSha256: string;
  patchAbsolutePath: string;
  licenseCopyAbsolutePath: string;
  dockerfileAbsolutePath: string;
};

export type OpenSandboxExecdBuildEvidence = {
  schemaVersion: "cloud-agents.opensandbox-execd-successor-build-evidence.v1";
  recipeSha256: string;
  upstream: { commit: string; tree: string };
  patchSha256: string;
  toolchainImage: string;
  runtimeBaseImage: string;
  image: {
    tag: string;
    id: string;
    immutableRef: string;
    os: "linux";
    architecture: "arm64";
    entrypoint: string[];
    binarySha256: string;
    licenseSha256: string;
  };
  verification: { dockerBuild: "PASS"; recipeBindings: "PASS" };
  cleanup: {
    imageTag: string;
    imageIdRetained: true;
    sourceCheckout: "source";
    buildContainerRemoved: true;
  };
  notGateClosure: true;
};

const sha256 = (bytes: Uint8Array | string) => createHash("sha256").update(bytes).digest("hex");

function record(value: unknown, label: string): Record<string, unknown> {
  if (typeof value !== "object" || value === null || Array.isArray(value)) {
    throw new Error(`${label} must be an object`);
  }
  return value as Record<string, unknown>;
}

function stringField(value: unknown, label: string): string {
  if (typeof value !== "string" || value.length === 0) {
    throw new Error(`${label} must be a non-empty string`);
  }
  return value;
}

function boundPath(repoRoot: string, value: unknown, label: string): string {
  const path = stringField(value, label);
  if (isAbsolute(path)) throw new Error(`${label} must be repository-relative`);
  const absolute = resolve(repoRoot, path);
  const rel = relative(repoRoot, absolute);
  if (rel === ".." || rel.startsWith(`..${sep}`) || isAbsolute(rel)) {
    throw new Error(`${label} escapes the repository`);
  }
  return absolute;
}

function requireSha(value: unknown, label: string, pattern = hex64): string {
  const digest = stringField(value, label);
  if (!pattern.test(digest)) throw new Error(`${label} must be lowercase hex`);
  return digest;
}

function requireDigestImage(value: unknown, label: string): string {
  const image = stringField(value, label);
  if (!digestImagePattern.test(image)) throw new Error(`${label} must be digest-pinned`);
  return image;
}

function verifyFileHash(path: string, expected: string, label: string) {
  if (!existsSync(path)) throw new Error(`${label} is missing: ${path}`);
  const actual = sha256(readFileSync(path));
  if (actual !== expected)
    throw new Error(`${label} sha256 mismatch: expected ${expected}, got ${actual}`);
}

export function readOpenSandboxExecdSuccessorRecipe(
  repoRoot = moduleRepoRoot,
): OpenSandboxExecdSuccessorRecipe {
  const root = resolve(repoRoot);
  const recipePath = resolve(root, OPEN_SANDBOX_EXECD_RECIPE_PATH);
  const recipeBytes = readFileSync(recipePath);
  const value = record(JSON.parse(recipeBytes.toString("utf8")), "execd successor recipe");
  if (value.schemaVersion !== "cloud-agents.opensandbox-execd-successor-source.v1") {
    throw new Error("unsupported execd successor recipe schemaVersion");
  }

  const upstreamValue = record(value.upstream, "upstream");
  const repository = stringField(upstreamValue.repository, "upstream.repository");
  if (repository !== "https://github.com/alibaba/OpenSandbox.git") {
    throw new Error("upstream.repository must be the official OpenSandbox repository");
  }
  const commit = requireSha(upstreamValue.commit, "upstream.commit", hex40);
  const tree = requireSha(upstreamValue.tree, "upstream.tree", hex40);
  const licensePath = stringField(upstreamValue.licensePath, "upstream.licensePath");
  if (isAbsolute(licensePath) || licensePath.includes("..")) {
    throw new Error("upstream.licensePath must be source-relative");
  }
  const licenseCopyAbsolutePath = boundPath(
    root,
    upstreamValue.licenseCopyPath,
    "upstream.licenseCopyPath",
  );
  const licenseSha256 = requireSha(upstreamValue.licenseSha256, "upstream.licenseSha256");

  const patchValue = record(value.patch, "patch");
  const patchAbsolutePath = boundPath(root, patchValue.path, "patch.path");
  const patchSha256 = requireSha(patchValue.sha256, "patch.sha256");

  const toolchainValue = record(value.toolchain, "toolchain");
  const toolchainImage = requireDigestImage(toolchainValue.image, "toolchain image");
  const goVersion = stringField(toolchainValue.goVersion, "toolchain.goVersion");
  if (!/^go[0-9]+\.[0-9]+\.[0-9]+$/.test(goVersion)) {
    throw new Error("toolchain.goVersion must be exact");
  }
  const goProxy = stringField(toolchainValue.goProxy, "toolchain.goProxy");
  const goProxyUrl = new URL(goProxy);
  if (
    goProxyUrl.protocol !== "https:" ||
    goProxyUrl.hostname !== "goproxy.cn" ||
    goProxyUrl.username !== "" ||
    goProxyUrl.password !== ""
  ) {
    throw new Error("toolchain.goProxy must be the credential-free domestic proxy");
  }
  const goSumDb = stringField(toolchainValue.goSumDb, "toolchain.goSumDb");
  if (goSumDb !== "sum.golang.google.cn") {
    throw new Error("toolchain.goSumDb must be sum.golang.google.cn");
  }

  const runtimeBaseValue = record(value.runtimeBase, "runtimeBase");
  const runtimeBaseImage = requireDigestImage(runtimeBaseValue.image, "runtime base image");
  if (runtimeBaseValue.sourceStatus !== "UNKNOWN_BUILD_SOURCE") {
    throw new Error("runtimeBase.sourceStatus must preserve the unknown build-source boundary");
  }
  if (runtimeBaseValue.use !== "runtime-assets-only") {
    throw new Error("runtimeBase.use must be runtime-assets-only");
  }

  const buildValue = record(value.build, "build");
  const dockerfileAbsolutePath = boundPath(root, buildValue.dockerfile, "build.dockerfile");
  const dockerfileSha256 = requireSha(buildValue.dockerfileSha256, "build.dockerfileSha256");
  if (buildValue.platform !== "linux/arm64") throw new Error("build.platform must be linux/arm64");
  const version = stringField(buildValue.version, "build.version");
  const buildTime = stringField(buildValue.buildTime, "build.buildTime");
  if (Number.isNaN(Date.parse(buildTime)))
    throw new Error("build.buildTime must be an ISO timestamp");

  verifyFileHash(patchAbsolutePath, patchSha256, "patch");
  verifyFileHash(licenseCopyAbsolutePath, licenseSha256, "license copy");
  verifyFileHash(dockerfileAbsolutePath, dockerfileSha256, "Dockerfile");

  return {
    schemaVersion: "cloud-agents.opensandbox-execd-successor-source.v1",
    upstream: {
      repository,
      commit,
      tree,
      licensePath,
      licenseCopyPath: stringField(upstreamValue.licenseCopyPath, "upstream.licenseCopyPath"),
      licenseSha256,
    },
    patch: { path: stringField(patchValue.path, "patch.path"), sha256: patchSha256 },
    toolchain: { image: toolchainImage, goVersion, goProxy, goSumDb },
    runtimeBase: {
      image: runtimeBaseImage,
      sourceStatus: "UNKNOWN_BUILD_SOURCE",
      use: "runtime-assets-only",
    },
    build: {
      dockerfile: stringField(buildValue.dockerfile, "build.dockerfile"),
      dockerfileSha256,
      platform: "linux/arm64",
      version,
      buildTime,
    },
    recipePath,
    recipeSha256: sha256(recipeBytes),
    patchAbsolutePath,
    licenseCopyAbsolutePath,
    dockerfileAbsolutePath,
  };
}

function evidenceString(value: unknown, label: string) {
  return stringField(value, `build evidence ${label}`);
}

export function readOpenSandboxExecdBuildEvidence(
  evidencePath: string,
  recipe = readOpenSandboxExecdSuccessorRecipe(),
): OpenSandboxExecdBuildEvidence {
  const value = record(JSON.parse(readFileSync(evidencePath, "utf8")), "execd build evidence");
  if (value.schemaVersion !== "cloud-agents.opensandbox-execd-successor-build-evidence.v1") {
    throw new Error("unsupported execd build evidence schemaVersion");
  }
  if (value.recipeSha256 !== recipe.recipeSha256) throw new Error("build evidence recipe mismatch");
  const upstream = record(value.upstream, "build evidence upstream");
  if (upstream.commit !== recipe.upstream.commit || upstream.tree !== recipe.upstream.tree) {
    throw new Error("build evidence upstream mismatch");
  }
  if (value.patchSha256 !== recipe.patch.sha256) throw new Error("build evidence patch mismatch");
  if (value.toolchainImage !== recipe.toolchain.image) {
    throw new Error("build evidence toolchain mismatch");
  }
  if (value.runtimeBaseImage !== recipe.runtimeBase.image) {
    throw new Error("build evidence runtime base mismatch");
  }

  const image = record(value.image, "build evidence image");
  const id = evidenceString(image.id, "image.id");
  const immutableRef = evidenceString(image.immutableRef, "image.immutableRef");
  if (!imageIdPattern.test(id) || immutableRef !== id) {
    throw new Error("build evidence immutable image ref must equal its sha256 image ID");
  }
  if (image.os !== "linux" || image.architecture !== "arm64") {
    throw new Error("build evidence image platform mismatch");
  }
  if (!Array.isArray(image.entrypoint) || image.entrypoint.join("\0") !== "./execd") {
    throw new Error("build evidence image entrypoint mismatch");
  }
  const binarySha256 = requireSha(image.binarySha256, "build evidence image.binarySha256");
  const licenseSha256 = requireSha(image.licenseSha256, "build evidence image.licenseSha256");
  if (licenseSha256 !== recipe.upstream.licenseSha256) {
    throw new Error("build evidence image license mismatch");
  }

  const verification = record(value.verification, "build evidence verification");
  if (verification.dockerBuild !== "PASS" || verification.recipeBindings !== "PASS") {
    throw new Error("build evidence verification is not PASS");
  }
  const cleanup = record(value.cleanup, "build evidence cleanup");
  const tag = evidenceString(image.tag, "image.tag");
  if (
    cleanup.imageTag !== tag ||
    cleanup.imageIdRetained !== true ||
    cleanup.sourceCheckout !== "source" ||
    cleanup.buildContainerRemoved !== true
  ) {
    throw new Error("build evidence cleanup contract mismatch");
  }
  if (value.notGateClosure !== true) throw new Error("build evidence must remain non-Gate");

  return {
    schemaVersion: "cloud-agents.opensandbox-execd-successor-build-evidence.v1",
    recipeSha256: recipe.recipeSha256,
    upstream: { commit: recipe.upstream.commit, tree: recipe.upstream.tree },
    patchSha256: recipe.patch.sha256,
    toolchainImage: recipe.toolchain.image,
    runtimeBaseImage: recipe.runtimeBase.image,
    image: {
      tag,
      id,
      immutableRef,
      os: "linux",
      architecture: "arm64",
      entrypoint: ["./execd"],
      binarySha256,
      licenseSha256,
    },
    verification: { dockerBuild: "PASS", recipeBindings: "PASS" },
    cleanup: {
      imageTag: tag,
      imageIdRetained: true,
      sourceCheckout: "source",
      buildContainerRemoved: true,
    },
    notGateClosure: true,
  };
}

function buildArg(name: string, value: string) {
  return ["--build-arg", `${name}=${value}`];
}

export function buildOpenSandboxExecdDockerArgs(
  recipe: OpenSandboxExecdSuccessorRecipe,
  sourceRoot: string,
  tag: string,
) {
  return [
    "build",
    "--platform",
    recipe.build.platform,
    ...buildArg("GO_IMAGE", recipe.toolchain.image),
    ...buildArg("RUNTIME_BASE_IMAGE", recipe.runtimeBase.image),
    ...buildArg("EXPECTED_GO_VERSION", recipe.toolchain.goVersion),
    ...buildArg("GOPROXY", recipe.toolchain.goProxy),
    ...buildArg("GOSUMDB", recipe.toolchain.goSumDb),
    ...buildArg("SOURCE_COMMIT", recipe.upstream.commit),
    ...buildArg("SOURCE_TREE", recipe.upstream.tree),
    ...buildArg("PATCH_SHA256", recipe.patch.sha256),
    ...buildArg("BUILDER_IMAGE", recipe.toolchain.image),
    ...buildArg("BUILD_VERSION", recipe.build.version),
    ...buildArg("BUILD_TIME", recipe.build.buildTime),
    "--file",
    recipe.dockerfileAbsolutePath,
    "--tag",
    tag,
    sourceRoot,
  ];
}

function commandText(command: string, args: string[]) {
  return [command, ...args].join(" ");
}

function run(command: string, args: string[], cwd?: string) {
  const result = spawnSync(command, args, {
    cwd,
    encoding: "utf8",
    stdio: "inherit",
    timeout: childCommandTimeoutMs,
  });
  if (result.error) throw new Error(`${command} operation failed or timed out`);
  if (result.status !== 0) {
    throw new Error(`${commandText(command, args)} exited ${String(result.status)}`);
  }
}

function runSilent(command: string, args: string[], cwd?: string) {
  const result = spawnSync(command, args, {
    cwd,
    encoding: "utf8",
    maxBuffer: 16 * 1024 * 1024,
    timeout: childCommandTimeoutMs,
  });
  if (result.error) throw new Error(`${command} operation failed or timed out`);
  if (result.status !== 0) {
    throw new Error(`${command} operation exited ${String(result.status)}`);
  }
}

function capture(command: string, args: string[], cwd?: string) {
  const result = spawnSync(command, args, {
    cwd,
    encoding: "utf8",
    maxBuffer: 16 * 1024 * 1024,
    timeout: childCommandTimeoutMs,
  });
  if (result.error) throw new Error(`${command} operation failed or timed out`);
  if (result.status !== 0) {
    throw new Error(`${commandText(command, args)} exited ${String(result.status)}`);
  }
  return result.stdout.trim();
}

function ensureExternalEmptyOutputRoot(repoRoot: string, outputRoot: string) {
  if (!isAbsolute(outputRoot)) throw new Error("--output-root must be absolute");
  const resolvedOutput = resolve(outputRoot);
  if (existsSync(resolvedOutput) && lstatSync(resolvedOutput).isSymbolicLink()) {
    throw new Error("--output-root must not be a symbolic link");
  }
  mkdirSync(resolvedOutput, { recursive: true });
  if (lstatSync(resolvedOutput).isSymbolicLink()) {
    throw new Error("--output-root must not be a symbolic link");
  }
  const actualRepoRoot = realpathSync(repoRoot);
  const actualOutputRoot = realpathSync(resolvedOutput);
  const rel = relative(actualRepoRoot, actualOutputRoot);
  if (rel === "" || (!rel.startsWith(`..${sep}`) && rel !== ".." && !isAbsolute(rel))) {
    throw new Error("--output-root must be outside the repository");
  }
  if (readdirSync(actualOutputRoot).length !== 0) {
    throw new Error("--output-root must be empty");
  }
  return actualOutputRoot;
}

function validateTag(tag: string) {
  if (tag.length === 0 || tag.includes("@") || /\s/.test(tag)) {
    throw new Error("--tag must be a mutable local build tag without whitespace or digest");
  }
}

export function buildOpenSandboxExecdSuccessor(options: {
  outputRoot: string;
  tag: string;
  repoRoot?: string;
  dockerCommand?: string;
}): OpenSandboxExecdBuildEvidence {
  const repoRoot = resolve(options.repoRoot ?? moduleRepoRoot);
  const recipe = readOpenSandboxExecdSuccessorRecipe(repoRoot);
  const outputRoot = ensureExternalEmptyOutputRoot(repoRoot, options.outputRoot);
  validateTag(options.tag);
  const docker = options.dockerCommand ?? "docker";
  const sourceRoot = resolve(outputRoot, "source");
  mkdirSync(sourceRoot);

  run("git", ["init", "--quiet"], sourceRoot);
  run("git", ["remote", "add", "origin", recipe.upstream.repository], sourceRoot);
  runSilent("git", ["fetch", "--depth=1", "origin", recipe.upstream.commit], sourceRoot);
  run("git", ["checkout", "--quiet", "--detach", "FETCH_HEAD"], sourceRoot);
  if (capture("git", ["rev-parse", "HEAD"], sourceRoot) !== recipe.upstream.commit) {
    throw new Error("fetched upstream commit mismatch");
  }
  if (capture("git", ["rev-parse", "HEAD^{tree}"], sourceRoot) !== recipe.upstream.tree) {
    throw new Error("fetched upstream tree mismatch");
  }
  verifyFileHash(
    resolve(sourceRoot, recipe.upstream.licensePath),
    recipe.upstream.licenseSha256,
    "fetched upstream license",
  );
  run("git", ["apply", "--check", recipe.patchAbsolutePath], sourceRoot);
  run("git", ["apply", recipe.patchAbsolutePath], sourceRoot);
  run("git", ["diff", "--check"], sourceRoot);
  writeFileSync(resolve(sourceRoot, ".dockerignore"), ".git\n");

  runSilent(docker, ["pull", "--platform", recipe.build.platform, recipe.toolchain.image]);
  runSilent(docker, ["pull", "--platform", recipe.build.platform, recipe.runtimeBase.image]);
  runSilent(docker, buildOpenSandboxExecdDockerArgs(recipe, sourceRoot, options.tag));

  const inspect = JSON.parse(capture(docker, ["image", "inspect", options.tag])) as unknown;
  if (!Array.isArray(inspect) || inspect.length !== 1)
    throw new Error("docker image inspect shape");
  const imageValue = record(inspect[0], "docker image inspect");
  const imageId = stringField(imageValue.Id, "docker image ID");
  if (!imageIdPattern.test(imageId)) throw new Error("docker returned a non-immutable image ID");
  const os = stringField(imageValue.Os, "docker image OS");
  const architecture = stringField(imageValue.Architecture, "docker image architecture");
  if (os !== "linux" || architecture !== "arm64") throw new Error("built image platform mismatch");
  const config = record(imageValue.Config, "docker image config");
  const entrypoint = config.Entrypoint;
  if (!Array.isArray(entrypoint) || entrypoint.join("\0") !== "./execd") {
    throw new Error("built image entrypoint mismatch");
  }
  const labels = record(config.Labels, "docker image labels");
  const expectedLabels: Record<string, string> = {
    "org.opencontainers.image.revision": recipe.upstream.commit,
    "org.opencontainers.image.licenses": "Apache-2.0",
    "cloud-agents.execd.source-tree": recipe.upstream.tree,
    "cloud-agents.execd.patch-sha256": recipe.patch.sha256,
    "cloud-agents.execd.builder-image": recipe.toolchain.image,
    "cloud-agents.execd.runtime-base": recipe.runtimeBase.image,
    "cloud-agents.execd.runtime-base-source-status": "UNKNOWN_BUILD_SOURCE",
  };
  for (const [key, expected] of Object.entries(expectedLabels)) {
    if (labels[key] !== expected) throw new Error(`built image label mismatch: ${key}`);
  }

  const binaryPath = resolve(outputRoot, "execd");
  const imageLicensePath = resolve(outputRoot, "image-LICENSE");
  const containerId = capture(docker, ["create", imageId]);
  let buildContainerRemoved = false;
  try {
    run(docker, ["cp", `${containerId}:/execd`, binaryPath]);
    run(docker, ["cp", `${containerId}:/usr/share/licenses/opensandbox/LICENSE`, imageLicensePath]);
  } finally {
    const result = spawnSync(docker, ["container", "rm", "--force", containerId], {
      encoding: "utf8",
      timeout: childCommandTimeoutMs,
    });
    buildContainerRemoved = result.status === 0;
  }
  if (!buildContainerRemoved) throw new Error("failed to remove build inspection container");
  verifyFileHash(imageLicensePath, recipe.upstream.licenseSha256, "image license");

  const evidence: OpenSandboxExecdBuildEvidence = {
    schemaVersion: "cloud-agents.opensandbox-execd-successor-build-evidence.v1",
    recipeSha256: recipe.recipeSha256,
    upstream: { commit: recipe.upstream.commit, tree: recipe.upstream.tree },
    patchSha256: recipe.patch.sha256,
    toolchainImage: recipe.toolchain.image,
    runtimeBaseImage: recipe.runtimeBase.image,
    image: {
      tag: options.tag,
      id: imageId,
      immutableRef: imageId,
      os: "linux",
      architecture: "arm64",
      entrypoint: ["./execd"],
      binarySha256: sha256(readFileSync(binaryPath)),
      licenseSha256: sha256(readFileSync(imageLicensePath)),
    },
    verification: { dockerBuild: "PASS", recipeBindings: "PASS" },
    cleanup: {
      imageTag: options.tag,
      imageIdRetained: true,
      sourceCheckout: "source",
      buildContainerRemoved: true,
    },
    notGateClosure: true,
  };
  const evidencePath = resolve(outputRoot, OPEN_SANDBOX_EXECD_BUILD_EVIDENCE_FILENAME);
  const temporaryEvidencePath = `${evidencePath}.tmp`;
  writeFileSync(temporaryEvidencePath, JSON.stringify(evidence, null, 2) + "\n");
  renameSync(temporaryEvidencePath, evidencePath);
  return readOpenSandboxExecdBuildEvidence(evidencePath, recipe);
}
