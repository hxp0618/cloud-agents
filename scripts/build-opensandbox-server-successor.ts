import { spawnSync } from "node:child_process";
import { createHash } from "node:crypto";
import {
  copyFileSync,
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

const moduleRepoRoot = resolve(dirname(fileURLToPath(import.meta.url)), "..");
const recipeRelativePath = "tools/opensandbox-server-successor/v1/source.json";
const evidenceFilename = "build-evidence.json";
const baseImage =
  /^sandbox-registry\.cn-zhangjiakou\.cr\.aliyuncs\.com\/opensandbox\/server@sha256:[0-9a-f]{64}$/u;
const sourcePath = "/app/opensandbox_server/api/proxy.py";
const hex40 = /^[0-9a-f]{40}$/u;
const hex64 = /^[0-9a-f]{64}$/u;
const imageID = /^sha256:[0-9a-f]{64}$/u;
const commandTimeoutMilliseconds = 540_000;

export type OpenSandboxServerRecipePlatform = {
  platform: string;
  image: string;
  os: "linux";
  architecture: string;
};

export type OpenSandboxServerSuccessorRecipe = {
  schemaVersion: "cloud-agents.opensandbox-server-successor-source.v2";
  base: {
    image: string;
    buildSourceStatus: "UNKNOWN_BUILD_SOURCE";
    sourcePath: typeof sourcePath;
    sourceSha256: string;
    platforms: OpenSandboxServerRecipePlatform[];
  };
  upstreamFileMatch: {
    repository: "https://github.com/alibaba/OpenSandbox.git";
    commit: string;
    tree: string;
    path: "server/opensandbox_server/api/proxy.py";
    sha256: string;
    scope: "EXACT_FILE_MATCH_ONLY";
  };
  patch: { path: string; sha256: string; patchedSourceSha256: string };
  regression: { path: string; sha256: string };
  license: { path: string; sha256: string };
  build: { dockerfile: string; dockerfileSha256: string };
  recipePath: string;
  recipeSha256: string;
  patchAbsolutePath: string;
  regressionAbsolutePath: string;
  licenseAbsolutePath: string;
  dockerfileAbsolutePath: string;
};

export type OpenSandboxServerBuildEvidence = {
  schemaVersion: "cloud-agents.opensandbox-server-successor-build-evidence.v2";
  recipeSha256: string;
  platform: string;
  base: {
    image: string;
    selectedImage: string;
    id: string;
    os: "linux";
    architecture: string;
    buildSourceStatus: "UNKNOWN_BUILD_SOURCE";
  };
  source: {
    path: typeof sourcePath;
    originalSha256: string;
    patchedSha256: string;
    upstreamCommit: string;
    upstreamTree: string;
    upstreamMatchScope: "EXACT_FILE_MATCH_ONLY";
  };
  patchSha256: string;
  regressionSha256: string;
  dockerfileSha256: string;
  licenseSha256: string;
  image: {
    tag: string;
    id: string;
    immutableRef: string;
    os: "linux";
    architecture: string;
    entrypoint: ["opensandbox-server"];
    command: ["--config", "/etc/opensandbox/config.toml"];
    proxySha256: string;
    licenseSha256: string;
  };
  verification: {
    dockerBuild: "PASS";
    cookieIsolationRegression: "PASS";
    recipeBindings: "PASS";
  };
  cleanup: {
    extractionContainerRemoved: true;
    inspectionContainerRemoved: true;
    imageTag: string;
    imageIDRetained: true;
  };
  notGateClosure: true;
};

const sha256 = (value: Uint8Array | string) => createHash("sha256").update(value).digest("hex");

function object(value: unknown, label: string): Record<string, unknown> {
  if (typeof value !== "object" || value === null || Array.isArray(value)) {
    throw new Error(`${label} must be an object`);
  }
  return value as Record<string, unknown>;
}

function string(value: unknown, label: string): string {
  if (typeof value !== "string" || value === "") {
    throw new Error(`${label} must be a non-empty string`);
  }
  return value;
}

function digest(value: unknown, label: string, pattern = hex64): string {
  const result = string(value, label);
  if (!pattern.test(result)) throw new Error(`${label} must be lowercase hexadecimal`);
  return result;
}

function repositoryPath(root: string, value: unknown, label: string): string {
  const path = string(value, label);
  if (isAbsolute(path)) throw new Error(`${label} must be repository-relative`);
  const absolute = resolve(root, path);
  const rel = relative(root, absolute);
  if (rel === ".." || rel.startsWith(`..${sep}`) || isAbsolute(rel)) {
    throw new Error(`${label} escapes the repository`);
  }
  return absolute;
}

function verifyFile(path: string, expected: string, label: string) {
  if (!existsSync(path) || sha256(readFileSync(path)) !== expected) {
    throw new Error(`${label} sha256 mismatch`);
  }
}

export function readOpenSandboxServerSuccessorRecipe(
  repoRoot = moduleRepoRoot,
): OpenSandboxServerSuccessorRecipe {
  const root = resolve(repoRoot);
  const recipePath = resolve(root, recipeRelativePath);
  const recipeBytes = readFileSync(recipePath);
  const value = object(JSON.parse(recipeBytes.toString("utf8")), "server successor recipe");
  if (value.schemaVersion !== "cloud-agents.opensandbox-server-successor-source.v2") {
    throw new Error("unsupported server successor recipe schemaVersion");
  }

  const base = object(value.base, "base");
  const baseImageReference = string(base.image, "base.image");
  if (
    !baseImage.test(baseImageReference) ||
    base.buildSourceStatus !== "UNKNOWN_BUILD_SOURCE" ||
    base.sourcePath !== sourcePath
  ) {
    throw new Error("server successor base binding mismatch");
  }
  const sourceSha256 = digest(base.sourceSha256, "base.sourceSha256");
  if (!Array.isArray(base.platforms) || base.platforms.length === 0) {
    throw new Error("base.platforms must be a non-empty array");
  }
  const platformNames = new Set<string>();
  const platforms = base.platforms.map((entry, index): OpenSandboxServerRecipePlatform => {
    const binding = object(entry, `base.platforms[${index}]`);
    const platform = string(binding.platform, `base.platforms[${index}].platform`);
    const os = string(binding.os, `base.platforms[${index}].os`);
    const architecture = string(binding.architecture, `base.platforms[${index}].architecture`);
    if (
      os !== "linux" ||
      platform !== `${os}/${architecture}` ||
      !/^linux\/[a-z0-9][a-z0-9_.-]*$/u.test(platform)
    ) {
      throw new Error(`base.platforms[${index}] platform binding mismatch`);
    }
    if (platformNames.has(platform)) {
      throw new Error(`duplicate server successor platform: ${platform}`);
    }
    platformNames.add(platform);
    const image = string(binding.image, `base.platforms[${index}].image`);
    if (!baseImage.test(image) || image === baseImageReference) {
      throw new Error(`base.platforms[${index}].image binding mismatch`);
    }
    return { platform, image, os: "linux", architecture };
  });

  const upstream = object(value.upstreamFileMatch, "upstreamFileMatch");
  if (
    upstream.repository !== "https://github.com/alibaba/OpenSandbox.git" ||
    upstream.path !== "server/opensandbox_server/api/proxy.py" ||
    upstream.scope !== "EXACT_FILE_MATCH_ONLY"
  ) {
    throw new Error("upstream exact-file binding mismatch");
  }
  const commit = digest(upstream.commit, "upstreamFileMatch.commit", hex40);
  const tree = digest(upstream.tree, "upstreamFileMatch.tree", hex40);
  if (digest(upstream.sha256, "upstreamFileMatch.sha256") !== sourceSha256) {
    throw new Error("upstream exact-file sha256 mismatch");
  }

  const patch = object(value.patch, "patch");
  const patchAbsolutePath = repositoryPath(root, patch.path, "patch.path");
  const patchSha256 = digest(patch.sha256, "patch.sha256");
  const patchedSourceSha256 = digest(patch.patchedSourceSha256, "patch.patchedSourceSha256");
  const regression = object(value.regression, "regression");
  const regressionAbsolutePath = repositoryPath(root, regression.path, "regression.path");
  const regressionSha256 = digest(regression.sha256, "regression.sha256");
  const license = object(value.license, "license");
  const licenseAbsolutePath = repositoryPath(root, license.path, "license.path");
  const licenseSha256 = digest(license.sha256, "license.sha256");
  const build = object(value.build, "build");
  const dockerfileAbsolutePath = repositoryPath(root, build.dockerfile, "build.dockerfile");
  const dockerfileSha256 = digest(build.dockerfileSha256, "build.dockerfileSha256");

  verifyFile(patchAbsolutePath, patchSha256, "patch");
  verifyFile(regressionAbsolutePath, regressionSha256, "regression");
  verifyFile(licenseAbsolutePath, licenseSha256, "license");
  verifyFile(dockerfileAbsolutePath, dockerfileSha256, "Dockerfile");

  return {
    schemaVersion: "cloud-agents.opensandbox-server-successor-source.v2",
    base: {
      image: baseImageReference,
      buildSourceStatus: "UNKNOWN_BUILD_SOURCE",
      sourcePath,
      sourceSha256,
      platforms,
    },
    upstreamFileMatch: {
      repository: "https://github.com/alibaba/OpenSandbox.git",
      commit,
      tree,
      path: "server/opensandbox_server/api/proxy.py",
      sha256: sourceSha256,
      scope: "EXACT_FILE_MATCH_ONLY",
    },
    patch: {
      path: string(patch.path, "patch.path"),
      sha256: patchSha256,
      patchedSourceSha256,
    },
    regression: {
      path: string(regression.path, "regression.path"),
      sha256: regressionSha256,
    },
    license: { path: string(license.path, "license.path"), sha256: licenseSha256 },
    build: {
      dockerfile: string(build.dockerfile, "build.dockerfile"),
      dockerfileSha256,
    },
    recipePath,
    recipeSha256: sha256(recipeBytes),
    patchAbsolutePath,
    regressionAbsolutePath,
    licenseAbsolutePath,
    dockerfileAbsolutePath,
  };
}

export function selectOpenSandboxServerRecipePlatform(
  recipe: OpenSandboxServerSuccessorRecipe,
  platform: string,
): OpenSandboxServerRecipePlatform {
  const selected = recipe.base.platforms.find((candidate) => candidate.platform === platform);
  if (!selected) throw new Error(`unsupported server successor platform: ${platform}`);
  return selected;
}

export function parseOpenSandboxServerDockerDaemonPlatform(value: unknown): string {
  const info = object(value, "Docker daemon info");
  const os = string(info.OSType, "Docker daemon OSType");
  const reportedArchitecture = string(info.Architecture, "Docker daemon Architecture");
  const architecture =
    reportedArchitecture === "x86_64"
      ? "amd64"
      : reportedArchitecture === "aarch64"
        ? "arm64"
        : reportedArchitecture;
  if (os !== "linux" || !/^[a-z0-9][a-z0-9_.-]*$/u.test(architecture)) {
    throw new Error(`Docker daemon platform is unsupported: ${os}/${reportedArchitecture}`);
  }
  return `${os}/${architecture}`;
}

function exactArray(value: unknown, expected: string[], label: string) {
  if (!Array.isArray(value) || value.join("\0") !== expected.join("\0")) {
    throw new Error(`${label} mismatch`);
  }
}

export function readOpenSandboxServerBuildEvidence(
  evidencePath: string,
  recipe = readOpenSandboxServerSuccessorRecipe(),
  expectedPlatform?: string,
): OpenSandboxServerBuildEvidence {
  const value = object(JSON.parse(readFileSync(evidencePath, "utf8")), "server build evidence");
  if (
    value.schemaVersion !== "cloud-agents.opensandbox-server-successor-build-evidence.v2" ||
    value.recipeSha256 !== recipe.recipeSha256 ||
    value.patchSha256 !== recipe.patch.sha256 ||
    value.regressionSha256 !== recipe.regression.sha256 ||
    value.dockerfileSha256 !== recipe.build.dockerfileSha256 ||
    value.licenseSha256 !== recipe.license.sha256
  ) {
    throw new Error("server build evidence recipe binding mismatch");
  }
  const platform = string(value.platform, "build evidence platform");
  if (expectedPlatform !== undefined && platform !== expectedPlatform) {
    throw new Error("server build evidence platform mismatch");
  }
  const selected = selectOpenSandboxServerRecipePlatform(recipe, platform);
  const base = object(value.base, "build evidence base");
  const baseID = string(base.id, "build evidence base.id");
  if (
    base.image !== recipe.base.image ||
    base.selectedImage !== selected.image ||
    !imageID.test(baseID) ||
    base.os !== selected.os ||
    base.architecture !== selected.architecture ||
    base.buildSourceStatus !== "UNKNOWN_BUILD_SOURCE"
  ) {
    throw new Error("server build evidence base mismatch");
  }
  const source = object(value.source, "build evidence source");
  if (
    source.path !== sourcePath ||
    source.originalSha256 !== recipe.base.sourceSha256 ||
    source.patchedSha256 !== recipe.patch.patchedSourceSha256 ||
    source.upstreamCommit !== recipe.upstreamFileMatch.commit ||
    source.upstreamTree !== recipe.upstreamFileMatch.tree ||
    source.upstreamMatchScope !== "EXACT_FILE_MATCH_ONLY"
  ) {
    throw new Error("server build evidence source mismatch");
  }
  const image = object(value.image, "build evidence image");
  const id = string(image.id, "build evidence image.id");
  const tag = string(image.tag, "build evidence image.tag");
  if (
    !imageID.test(id) ||
    image.immutableRef !== id ||
    image.os !== selected.os ||
    image.architecture !== selected.architecture ||
    image.proxySha256 !== recipe.patch.patchedSourceSha256 ||
    image.licenseSha256 !== recipe.license.sha256
  ) {
    throw new Error("server build evidence image mismatch");
  }
  exactArray(image.entrypoint, ["opensandbox-server"], "image entrypoint");
  exactArray(image.command, ["--config", "/etc/opensandbox/config.toml"], "image command");
  const verification = object(value.verification, "build evidence verification");
  if (
    verification.dockerBuild !== "PASS" ||
    verification.cookieIsolationRegression !== "PASS" ||
    verification.recipeBindings !== "PASS"
  ) {
    throw new Error("server build evidence verification is not PASS");
  }
  const cleanup = object(value.cleanup, "build evidence cleanup");
  if (
    cleanup.extractionContainerRemoved !== true ||
    cleanup.inspectionContainerRemoved !== true ||
    cleanup.imageTag !== tag ||
    cleanup.imageIDRetained !== true ||
    value.notGateClosure !== true
  ) {
    throw new Error("server build evidence cleanup mismatch");
  }
  return value as unknown as OpenSandboxServerBuildEvidence;
}

function run(command: string, args: string[], cwd?: string, inherit = false) {
  const result = spawnSync(command, args, {
    cwd,
    encoding: "utf8",
    stdio: inherit ? "inherit" : "pipe",
    maxBuffer: 16 * 1024 * 1024,
    timeout: commandTimeoutMilliseconds,
  });
  if (result.error || result.status !== 0) {
    throw new Error(`${command} operation failed with status ${String(result.status)}`);
  }
  return inherit ? "" : result.stdout.trim();
}

function dockerDaemonPlatform(docker: string) {
  const value = JSON.parse(run(docker, ["info", "--format", "{{json .}}"]));
  return parseOpenSandboxServerDockerDaemonPlatform(value);
}

function removeContainer(docker: string, id: string) {
  const result = spawnSync(docker, ["container", "rm", "--force", id], {
    encoding: "utf8",
    timeout: commandTimeoutMilliseconds,
  });
  return result.status === 0;
}

function externalEmptyRoot(repoRoot: string, outputRoot: string) {
  if (!isAbsolute(outputRoot)) throw new Error("--output-root must be absolute");
  const result = resolve(outputRoot);
  if (existsSync(result) && lstatSync(result).isSymbolicLink()) {
    throw new Error("--output-root must not be a symbolic link");
  }
  let ancestor = result;
  while (!existsSync(ancestor)) ancestor = dirname(ancestor);
  const prospective = resolve(realpathSync(ancestor), relative(ancestor, result));
  const repo = realpathSync(repoRoot);
  const prospectiveRelative = relative(repo, prospective);
  if (
    prospectiveRelative === "" ||
    (!prospectiveRelative.startsWith(`..${sep}`) &&
      prospectiveRelative !== ".." &&
      !isAbsolute(prospectiveRelative))
  ) {
    throw new Error("--output-root must be outside the repository");
  }
  mkdirSync(result, { recursive: true });
  const actual = realpathSync(result);
  const rel = relative(repo, actual);
  if (rel === "" || (!rel.startsWith(`..${sep}`) && rel !== ".." && !isAbsolute(rel))) {
    throw new Error("--output-root must be outside the repository");
  }
  if (readdirSync(actual).length !== 0) throw new Error("--output-root must be empty");
  return actual;
}

function validateTag(tag: string) {
  if (tag === "" || tag.includes("@") || /\s/u.test(tag)) {
    throw new Error("--tag must be a local mutable tag without whitespace");
  }
}

export function buildOpenSandboxServerSuccessor(options: {
  outputRoot: string;
  tag: string;
  platform?: string;
  repoRoot?: string;
  dockerCommand?: string;
}): OpenSandboxServerBuildEvidence {
  const repoRoot = resolve(options.repoRoot ?? moduleRepoRoot);
  const recipe = readOpenSandboxServerSuccessorRecipe(repoRoot);
  const outputRoot = externalEmptyRoot(repoRoot, options.outputRoot);
  validateTag(options.tag);
  const docker = options.dockerCommand ?? "docker";
  const platform = options.platform ?? dockerDaemonPlatform(docker);
  const selected = selectOpenSandboxServerRecipePlatform(recipe, platform);
  const context = resolve(outputRoot, "context");
  const extractedSource = resolve(context, "server/opensandbox_server/api/proxy.py");
  mkdirSync(dirname(extractedSource), { recursive: true });

  const baseInspection = JSON.parse(run(docker, ["image", "inspect", selected.image])) as unknown;
  if (!Array.isArray(baseInspection) || baseInspection.length !== 1) {
    throw new Error("base image inspection shape mismatch");
  }
  const base = object(baseInspection[0], "base image");
  const baseID = string(base.Id, "base image ID");
  if (
    !imageID.test(baseID) ||
    base.Os !== selected.os ||
    base.Architecture !== selected.architecture
  ) {
    throw new Error("base image platform or ID mismatch");
  }

  const extractionContainer = run(docker, [
    "create",
    "--platform",
    selected.platform,
    selected.image,
  ]);
  let extractionContainerRemoved = false;
  try {
    run(docker, ["cp", `${extractionContainer}:${sourcePath}`, extractedSource]);
  } finally {
    extractionContainerRemoved = removeContainer(docker, extractionContainer);
  }
  if (!extractionContainerRemoved) throw new Error("base extraction container cleanup failed");
  verifyFile(extractedSource, recipe.base.sourceSha256, "extracted base proxy source");

  run("git", ["apply", "--check", recipe.patchAbsolutePath], context);
  run("git", ["apply", recipe.patchAbsolutePath], context);
  verifyFile(extractedSource, recipe.patch.patchedSourceSha256, "patched proxy source");
  copyFileSync(recipe.dockerfileAbsolutePath, resolve(context, "Dockerfile"));
  copyFileSync(recipe.regressionAbsolutePath, resolve(context, "cookie-isolation-regression.py"));
  copyFileSync(recipe.licenseAbsolutePath, resolve(context, "LICENSE"));

  run(
    docker,
    [
      "build",
      "--platform",
      selected.platform,
      "--build-arg",
      `BASE_IMAGE=${selected.image}`,
      "--build-arg",
      `ORIGINAL_PROXY_SHA256=${recipe.base.sourceSha256}`,
      "--build-arg",
      `PATCH_SHA256=${recipe.patch.sha256}`,
      "--build-arg",
      `PATCHED_PROXY_SHA256=${recipe.patch.patchedSourceSha256}`,
      "--tag",
      options.tag,
      context,
    ],
    undefined,
    true,
  );

  const inspection = JSON.parse(run(docker, ["image", "inspect", options.tag])) as unknown;
  if (!Array.isArray(inspection) || inspection.length !== 1) {
    throw new Error("successor image inspection shape mismatch");
  }
  const image = object(inspection[0], "successor image");
  const id = string(image.Id, "successor image ID");
  if (
    !imageID.test(id) ||
    image.Os !== selected.os ||
    image.Architecture !== selected.architecture
  ) {
    throw new Error("successor image platform or ID mismatch");
  }
  const config = object(image.Config, "successor image config");
  exactArray(config.Entrypoint, ["opensandbox-server"], "successor entrypoint");
  exactArray(config.Cmd, ["--config", "/etc/opensandbox/config.toml"], "successor command");
  const labels = object(config.Labels, "successor image labels");
  const expectedLabels: Record<string, string> = {
    "cloud-agents.opensandbox-server.base-image": selected.image,
    "cloud-agents.opensandbox-server.base-build-source-status": "UNKNOWN_BUILD_SOURCE",
    "cloud-agents.opensandbox-server.original-proxy-sha256": recipe.base.sourceSha256,
    "cloud-agents.opensandbox-server.patch-sha256": recipe.patch.sha256,
    "cloud-agents.opensandbox-server.patched-proxy-sha256": recipe.patch.patchedSourceSha256,
    "org.opencontainers.image.licenses": "Apache-2.0",
  };
  for (const [name, expected] of Object.entries(expectedLabels)) {
    if (labels[name] !== expected) throw new Error(`successor image label mismatch: ${name}`);
  }

  const imageProxy = resolve(outputRoot, "image-proxy.py");
  const imageLicense = resolve(outputRoot, "image-LICENSE");
  const inspectionContainer = run(docker, ["create", "--platform", selected.platform, id]);
  let inspectionContainerRemoved = false;
  try {
    run(docker, ["cp", `${inspectionContainer}:${sourcePath}`, imageProxy]);
    run(docker, [
      "cp",
      `${inspectionContainer}:/usr/share/licenses/opensandbox/LICENSE`,
      imageLicense,
    ]);
  } finally {
    inspectionContainerRemoved = removeContainer(docker, inspectionContainer);
  }
  if (!inspectionContainerRemoved) throw new Error("image inspection container cleanup failed");
  verifyFile(imageProxy, recipe.patch.patchedSourceSha256, "image proxy source");
  verifyFile(imageLicense, recipe.license.sha256, "image license");

  const evidence: OpenSandboxServerBuildEvidence = {
    schemaVersion: "cloud-agents.opensandbox-server-successor-build-evidence.v2",
    recipeSha256: recipe.recipeSha256,
    platform: selected.platform,
    base: {
      image: recipe.base.image,
      selectedImage: selected.image,
      id: baseID,
      os: selected.os,
      architecture: selected.architecture,
      buildSourceStatus: "UNKNOWN_BUILD_SOURCE",
    },
    source: {
      path: sourcePath,
      originalSha256: recipe.base.sourceSha256,
      patchedSha256: recipe.patch.patchedSourceSha256,
      upstreamCommit: recipe.upstreamFileMatch.commit,
      upstreamTree: recipe.upstreamFileMatch.tree,
      upstreamMatchScope: "EXACT_FILE_MATCH_ONLY",
    },
    patchSha256: recipe.patch.sha256,
    regressionSha256: recipe.regression.sha256,
    dockerfileSha256: recipe.build.dockerfileSha256,
    licenseSha256: recipe.license.sha256,
    image: {
      tag: options.tag,
      id,
      immutableRef: id,
      os: selected.os,
      architecture: selected.architecture,
      entrypoint: ["opensandbox-server"],
      command: ["--config", "/etc/opensandbox/config.toml"],
      proxySha256: sha256(readFileSync(imageProxy)),
      licenseSha256: sha256(readFileSync(imageLicense)),
    },
    verification: {
      dockerBuild: "PASS",
      cookieIsolationRegression: "PASS",
      recipeBindings: "PASS",
    },
    cleanup: {
      extractionContainerRemoved: true,
      inspectionContainerRemoved: true,
      imageTag: options.tag,
      imageIDRetained: true,
    },
    notGateClosure: true,
  };
  const evidencePath = resolve(outputRoot, evidenceFilename);
  const temporary = `${evidencePath}.tmp`;
  writeFileSync(temporary, `${JSON.stringify(evidence, null, 2)}\n`, { mode: 0o600 });
  renameSync(temporary, evidencePath);
  return readOpenSandboxServerBuildEvidence(evidencePath, recipe, selected.platform);
}

export function main(argv = process.argv.slice(2)) {
  let outputRoot = "";
  let tag = "";
  let platform: string | undefined;
  for (let index = 0; index < argv.length; index += 1) {
    const argument = argv[index];
    if (argument === "--output-root") outputRoot = argv[++index] ?? "";
    else if (argument === "--tag") tag = argv[++index] ?? "";
    else if (argument === "--platform") platform = argv[++index] ?? "";
    else throw new Error(`unknown argument: ${argument}`);
  }
  if (outputRoot === "" || tag === "" || platform === "") {
    throw new Error(
      "usage: --output-root <absolute-empty-directory> --tag <local-image-tag> [--platform <qualified-platform>]",
    );
  }
  const evidence = buildOpenSandboxServerSuccessor({ outputRoot, tag, platform });
  process.stdout.write(
    `${JSON.stringify({
      evidencePath: resolve(outputRoot, evidenceFilename),
      imageId: evidence.image.id,
      immutableImageRef: evidence.image.immutableRef,
      platform: evidence.platform,
    })}\n`,
  );
}

if (process.argv[1] && resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
  try {
    main();
  } catch (error) {
    process.stderr.write(
      `OpenSandbox server successor build failed: ${error instanceof Error ? error.message : String(error)}\n`,
    );
    process.exitCode = 1;
  }
}
