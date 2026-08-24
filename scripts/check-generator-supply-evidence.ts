import { createHash } from "node:crypto";
import { execFileSync } from "node:child_process";
import {
  lstatSync,
  mkdirSync,
  readFileSync,
  readlinkSync,
  readdirSync,
  renameSync,
  realpathSync,
  unlinkSync,
  writeFileSync,
} from "node:fs";
import { dirname, isAbsolute, relative, resolve, sep } from "node:path";

import { assertGeneratorSupplyProfileCurrent } from "./lib/platform-generator-supply-profile";

type Json = Record<string, unknown>;

const root = resolve(import.meta.dirname, "..");
const args = process.argv.slice(2);

if (args[0] === "--collect-materials" && args[1] !== undefined) {
  collectMaterials(resolve(args[1]));
  process.stdout.write("generator-supply-evidence: collected exact material evidence\n");
} else if (args[0] === "--check") {
  assertGeneratorSupplyProfileCurrent(root);
  process.stdout.write("generator-supply-evidence: exact evidence and profile current\n");
} else if (args[0] === "--sanitize-raw") {
  sanitizeRawEvidence();
  process.stdout.write("generator-supply-evidence: sanitized raw scanner evidence\n");
} else {
  throw new Error(
    "Usage: bun scripts/check-generator-supply-evidence.ts --collect-materials <root>|--sanitize-raw|--check",
  );
}

function sanitizeRawEvidence(): void {
  const paths = [
    ...["darwin-bundle", "linux-bundle", "ubuntu-image"].flatMap((scope) =>
      ["syft", "cdx", "spdx"].map(
        (format) => `tools/generator-supply/v1/evidence/sbom/${scope}.${format}.json`,
      ),
    ),
    "tools/generator-supply/v1/evidence/sbom/node-24.13.1-rejected-darwin.syft.json",
    "tools/generator-supply/v1/evidence/sbom/node-24.13.1-rejected-linux.syft.json",
    "tools/generator-supply/v1/evidence/vulnerability/grype-darwin.json",
    "tools/generator-supply/v1/evidence/vulnerability/grype-db-status.json",
    "tools/generator-supply/v1/evidence/vulnerability/grype-linux.json",
    "tools/generator-supply/v1/evidence/vulnerability/grype-node-24.13.1-rejected-darwin.json",
    "tools/generator-supply/v1/evidence/vulnerability/grype-node-24.13.1-rejected-linux.json",
    "tools/generator-supply/v1/evidence/vulnerability/grype-ubuntu.json",
    "tools/generator-supply/v1/evidence/vulnerability/osv.json",
  ];
  for (const relativePath of paths) {
    const absolute = resolve(root, relativePath);
    const sanitized = sanitizeJsonValue(parseJson(absolute));
    atomicWrite(absolute, `${JSON.stringify(sanitized)}\n`);
  }
}

function sanitizeJsonValue(value: unknown): unknown {
  if (typeof value === "string") {
    return value
      .replace(
        /\/(?:private\/)?tmp\/codex-generator-bundles\.[^/]+\/darwin-arm64/gu,
        "generator-supply://effective-bundle/darwin-arm64",
      )
      .replace(
        /\/(?:private\/)?tmp\/codex-generator-bundles\.[^/]+\/linux-amd64/gu,
        "generator-supply://effective-bundle/linux-amd64",
      )
      .replace(
        /\/(?:private\/)?tmp\/codex-generator-bundles-historical-24\.13\.1\/darwin-arm64/gu,
        "generator-supply://rejected-effective-bundle/node-24.13.1/darwin-arm64",
      )
      .replace(
        /\/(?:private\/)?tmp\/codex-generator-bundles-historical-24\.13\.1\/linux-amd64/gu,
        "generator-supply://rejected-effective-bundle/node-24.13.1/linux-amd64",
      )
      .replace(
        /\/(?:private\/)?tmp\/codex-cloud-agents-generator-supply-20260824\/scan-input/gu,
        "generator-supply://isolated-scan-input",
      )
      .replace(
        /\/(?:private\/)?tmp\/codex-cloud-agents-generator-supply-20260824\/scanners\/grype\/db\/6\/vulnerability\.db/gu,
        "generator-supply://grype-db/v6/vulnerability.db",
      )
      .replace(
        /\/(?:private\/)?tmp\/codex-cloud-agents-generator-supply-20260824\/scanners\/grype\/db/gu,
        "generator-supply://grype-db",
      )
      .replaceAll("/Users/huang/go/pkg/mod", "generator-supply://ambient-go-module-cache-not-used")
      .replaceAll("/Users/huang/.m2/repository", "generator-supply://ambient-maven-cache-not-used");
  }
  if (Array.isArray(value)) return value.map(sanitizeJsonValue);
  if (typeof value === "object" && value !== null) {
    return Object.fromEntries(
      Object.entries(value).map(([key, entry]) => [key, sanitizeJsonValue(entry)]),
    );
  }
  return value;
}

function collectMaterials(materialRoot: string): void {
  const source = parseJson(resolve(root, "tools/generator-supply/v1/source.json"));
  const profile = source.profile as Json;
  const expectedArtifacts = profile.officialArtifacts as Json[];
  const archiveRecordsBase = expectedArtifacts.map((expected) => {
    const path = resolve(materialRoot, "artifacts", String(expected.filename));
    requireRegular(path);
    const actual = fileRecord(path);
    if (actual.sha256 !== expected.sha256 || actual.sizeBytes !== expected.sizeBytes) {
      throw new Error(`Official artifact bytes drifted: ${String(expected.filename)}`);
    }
    return { ...expected, verifiedSha256: actual.sha256, verifiedSizeBytes: actual.sizeBytes };
  });

  const executableLayout = {
    "darwin-arm64": [
      ["node", "toolchains/darwin-arm64/node/bin/node"],
      ["bun", "toolchains/darwin-arm64/bun/bun-darwin-aarch64/bun"],
      ["go", "toolchains/darwin-arm64/go/bin/go"],
      ["gofmt", "toolchains/darwin-arm64/go/bin/gofmt"],
      ["python", "toolchains/darwin-arm64/python/bin/python3.14"],
      ["uv", "toolchains/darwin-arm64/uv/uv"],
      ["protoc", "toolchains/darwin-arm64/protoc/bin/protoc"],
      ["protoc-gen-go", "plugins/darwin-arm64/protoc-gen-go"],
      ["protoc-gen-connect-go", "plugins/darwin-arm64/protoc-gen-connect-go"],
    ],
    "linux-amd64": [
      ["node", "toolchains/linux-amd64/node/bin/node"],
      ["bun", "toolchains/linux-amd64/bun/bun-linux-x64-baseline/bun"],
      ["go", "toolchains/linux-amd64/go/bin/go"],
      ["gofmt", "toolchains/linux-amd64/go/bin/gofmt"],
      ["python", "toolchains/linux-amd64/python/bin/python3.14"],
      ["uv", "toolchains/linux-amd64/uv/uv"],
      ["protoc", "toolchains/linux-amd64/protoc/bin/protoc"],
      ["protoc-gen-go", "plugins/linux-amd64/protoc-gen-go"],
      ["protoc-gen-connect-go", "plugins/linux-amd64/protoc-gen-connect-go"],
    ],
  } as const;
  const executables = Object.fromEntries(
    Object.entries(executableLayout).map(([platform, layout]) => [
      platform,
      layout.map(([id, relativePath]) => ({
        id,
        path: relativePath,
        ...fileRecord(resolve(materialRoot, relativePath)),
      })),
    ]),
  );
  const evidenceExecutables = [
    ["syft", "scanners/bin/syft", "1.51.0"],
    ["grype", "scanners/bin/grype", "0.117.0"],
    ["osv-scanner", "scanners/bin/osv-scanner", "2.5.1"],
  ].map(([id, relativePath, version]) => ({
    id,
    version,
    path: relativePath,
    ...fileRecord(resolve(materialRoot, relativePath)),
  }));
  const archiveBindings: Readonly<
    Record<
      string,
      readonly {
        id: string;
        platform: string;
        memberPath: string;
        effectivePath: string;
      }[]
    >
  > = {
    "node-darwin-arm64": [
      {
        id: "node",
        platform: "darwin-arm64",
        memberPath: "node-v24.18.1-darwin-arm64/bin/node",
        effectivePath: "toolchains/darwin-arm64/node/bin/node",
      },
    ],
    "node-linux-amd64": [
      {
        id: "node",
        platform: "linux-amd64",
        memberPath: "node-v24.18.1-linux-x64/bin/node",
        effectivePath: "toolchains/linux-amd64/node/bin/node",
      },
    ],
    "bun-darwin-arm64": [
      {
        id: "bun",
        platform: "darwin-arm64",
        memberPath: "bun-darwin-aarch64/bun",
        effectivePath: "toolchains/darwin-arm64/bun/bun-darwin-aarch64/bun",
      },
    ],
    "bun-linux-amd64": [
      {
        id: "bun",
        platform: "linux-amd64",
        memberPath: "bun-linux-x64-baseline/bun",
        effectivePath: "toolchains/linux-amd64/bun/bun-linux-x64-baseline/bun",
      },
    ],
    "go-darwin-arm64": [
      {
        id: "go",
        platform: "darwin-arm64",
        memberPath: "go/bin/go",
        effectivePath: "toolchains/darwin-arm64/go/bin/go",
      },
      {
        id: "gofmt",
        platform: "darwin-arm64",
        memberPath: "go/bin/gofmt",
        effectivePath: "toolchains/darwin-arm64/go/bin/gofmt",
      },
    ],
    "go-linux-amd64": [
      {
        id: "go",
        platform: "linux-amd64",
        memberPath: "go/bin/go",
        effectivePath: "toolchains/linux-amd64/go/bin/go",
      },
      {
        id: "gofmt",
        platform: "linux-amd64",
        memberPath: "go/bin/gofmt",
        effectivePath: "toolchains/linux-amd64/go/bin/gofmt",
      },
    ],
    "python-darwin-arm64": [
      {
        id: "python",
        platform: "darwin-arm64",
        memberPath: "python/bin/python3.14",
        effectivePath: "toolchains/darwin-arm64/python/bin/python3.14",
      },
    ],
    "python-linux-amd64": [
      {
        id: "python",
        platform: "linux-amd64",
        memberPath: "python/bin/python3.14",
        effectivePath: "toolchains/linux-amd64/python/bin/python3.14",
      },
    ],
    "uv-darwin-arm64": [
      {
        id: "uv",
        platform: "darwin-arm64",
        memberPath: "uv-aarch64-apple-darwin/uv",
        effectivePath: "toolchains/darwin-arm64/uv/uv",
      },
    ],
    "uv-linux-amd64": [
      {
        id: "uv",
        platform: "linux-amd64",
        memberPath: "uv-x86_64-unknown-linux-gnu/uv",
        effectivePath: "toolchains/linux-amd64/uv/uv",
      },
    ],
    "protoc-darwin-arm64": [
      {
        id: "protoc",
        platform: "darwin-arm64",
        memberPath: "bin/protoc",
        effectivePath: "toolchains/darwin-arm64/protoc/bin/protoc",
      },
    ],
    "protoc-linux-amd64": [
      {
        id: "protoc",
        platform: "linux-amd64",
        memberPath: "bin/protoc",
        effectivePath: "toolchains/linux-amd64/protoc/bin/protoc",
      },
    ],
    "syft-darwin-arm64": [
      {
        id: "syft",
        platform: "evidence-darwin-arm64",
        memberPath: "syft",
        effectivePath: "scanners/bin/syft",
      },
    ],
    "grype-darwin-arm64": [
      {
        id: "grype",
        platform: "evidence-darwin-arm64",
        memberPath: "grype",
        effectivePath: "scanners/bin/grype",
      },
    ],
  };
  const inspector = resolve(root, "scripts/lib/inspect-generator-supply-archive.py");
  requireRegular(inspector);
  const archiveRecords = archiveRecordsBase.map((archive) => {
    const id = String(archive.id);
    if (id === "osv-scanner-darwin-arm64") {
      const effectivePath = "scanners/bin/osv-scanner";
      const effective = fileRecord(resolve(materialRoot, effectivePath));
      if (effective.sha256 !== archive.sha256 || effective.sizeBytes !== archive.sizeBytes) {
        throw new Error("Bare OSV-Scanner executable must equal the official artifact bytes.");
      }
      return {
        ...archive,
        distribution: "BARE_OFFICIAL_BINARY",
        extractionAudit: "NOT_APPLICABLE_BARE_BINARY",
        effectiveExecutables: [
          {
            id: "osv-scanner",
            platform: "evidence-darwin-arm64",
            memberPath: null,
            effectivePath,
            memberSha256: archive.sha256,
            memberSizeBytes: archive.sizeBytes,
            effectiveSha256: effective.sha256,
            effectiveSizeBytes: effective.sizeBytes,
            provenance: "OFFICIAL_BARE_BINARY_BYTES_EXACT",
          },
        ],
      };
    }
    const bindings = archiveBindings[id];
    if (bindings === undefined)
      throw new Error(`Archive ${id} has no effective executable binding.`);
    const inspection = JSON.parse(
      execFileSync(
        "/usr/bin/python3",
        [
          inspector,
          resolve(materialRoot, "artifacts", String(archive.filename)),
          ...bindings.map((entry) => entry.memberPath),
        ],
        { encoding: "utf8" },
      ),
    ) as Json;
    const selected = inspection.selectedMembers as Json[];
    const effectiveExecutables = bindings.map((binding) => {
      const member = selected.find((entry) => entry.path === binding.memberPath);
      if (member === undefined) throw new Error(`Archive member is absent: ${binding.memberPath}`);
      const effective = fileRecord(resolve(materialRoot, binding.effectivePath));
      if (member.sha256 !== effective.sha256 || member.sizeBytes !== effective.sizeBytes) {
        throw new Error(`Effective executable bytes drifted from ${id}:${binding.memberPath}.`);
      }
      return {
        ...binding,
        memberSha256: member.sha256,
        memberSizeBytes: member.sizeBytes,
        effectiveSha256: effective.sha256,
        effectiveSizeBytes: effective.sizeBytes,
        provenance: "SAFE_ARCHIVE_REGULAR_MEMBER_BYTES_EXACT",
      };
    });
    return {
      ...archive,
      distribution: "ARCHIVE",
      extractionAudit: inspection,
      effectiveExecutables,
    };
  });
  writeEvidence("artifacts.json", {
    formatVersion: "cloud-agents-generator-supply-artifacts/v1",
    status: "OFFICIAL_DIGEST_AND_EXECUTABLE_BYTES_VERIFIED",
    officialArtifacts: 15,
    darwinExecutables: 9,
    linuxExecutables: 9,
    evidenceExecutables: 3,
    archives: archiveRecords,
    executables,
    scanners: evidenceExecutables,
    officialVerification: {
      node: "SHASUMS256.txt",
      go: "go.dev downloads JSON",
      githubAssets: "release asset digest and size",
    },
  });

  const wheelPlatforms = ["darwin-arm64", "linux-amd64"].map((platform) => {
    const directory = resolve(materialRoot, "wheelhouse", platform);
    const wheels = readdirSync(directory)
      .filter((name) => name.endsWith(".whl"))
      .toSorted(utf8BytewiseCompare)
      .map((filename) => ({
        filename,
        tags: wheelTags(filename),
        ...fileRecord(resolve(directory, filename)),
      }));
    if (wheels.length !== 21)
      throw new Error(`${platform} wheelhouse must contain exactly 21 wheels.`);
    return { platform, wheelCount: wheels.length, wheels };
  });
  writeEvidence("wheels.json", {
    formatVersion: "cloud-agents-generator-supply-wheels/v1",
    status: "EXACT_WHEELHOUSE_BYTES_VERIFIED",
    darwinWheelCount: 21,
    linuxWheelCount: 21,
    sourceBuild: "FORBIDDEN",
    python: "3.14.7",
    uv: "0.12.5",
    platforms: wheelPlatforms,
  });

  const npmLockPath = resolve(root, "tools/generator-supply/npm/package-lock.json");
  const npmLock = parseJson(npmLockPath);
  const lockPackages = npmLock.packages as Record<string, Json>;
  const resolved = Object.entries(lockPackages).filter(([, value]) => value.resolved !== undefined);
  if (
    Object.keys(lockPackages).length !== 35 ||
    resolved.length !== 34 ||
    resolved.some(([, value]) => !String(value.resolved).startsWith("https://registry.npmjs.org/"))
  ) {
    throw new Error("Sterile npm lock is not the exact official-registry 35/34 closure.");
  }
  const installed = [
    ["darwin-arm64", "npm-node-24.18.1-darwin", 16],
    ["linux-amd64", "npm-linux-glibc", 17],
  ].map(([platform, directory, expectedCount]) => {
    const hiddenLockPath = resolve(
      materialRoot,
      String(directory),
      "node_modules/.package-lock.json",
    );
    const hidden = parseJson(hiddenLockPath);
    const packages = hidden.packages as Record<string, Json>;
    const records = Object.entries(packages)
      .filter(([path]) => path !== "")
      .map(([path, value]) => ({
        path,
        version: value.version,
        integrity: value.integrity,
        resolved: value.resolved,
      }))
      .toSorted((left, right) => utf8BytewiseCompare(left.path, right.path));
    if (records.length !== expectedCount) {
      throw new Error(`${platform} installed package count ${records.length} != ${expectedCount}.`);
    }
    return {
      platform,
      installedPackageCount: records.length,
      hiddenLock: fileRecord(hiddenLockPath),
      nodeModules: directoryContentManifest(
        resolve(materialRoot, String(directory), "node_modules"),
        records.map((record) => String(record.path)),
      ),
      packages: records,
    };
  });
  const rootBunLock = readFileSync(resolve(root, "bun.lock"));
  const npmRuntimePackage = resolve(materialRoot, "toolchains/npm/11.8.0/package.json");
  const npmRuntimeCli = resolve(materialRoot, "toolchains/npm/11.8.0/bin/npm-cli.js");
  if ((parseJson(npmRuntimePackage).version as string) !== "11.8.0") {
    throw new Error("Generator npm runtime must remain exactly 11.8.0.");
  }
  writeEvidence("npm.json", {
    formatVersion: "cloud-agents-generator-supply-npm/v1",
    status: "STERILE_OFFICIAL_REGISTRY_LOCK_AND_NATIVE_LOAD_VERIFIED",
    npm: "11.8.0",
    npmRuntime: {
      package: fileRecord(npmRuntimePackage),
      cli: fileRecord(npmRuntimeCli),
    },
    lockPackageRecordsIncludingRoot: 35,
    officialResolvedDependencyRecords: 34,
    nonOfficialResolvedRecords: 0,
    directExactDependencies: 5,
    packageLock: {
      path: "tools/generator-supply/npm/package-lock.json",
      ...fileRecord(npmLockPath),
    },
    darwinInstalledPackages: 16,
    linuxInstalledPackages: 17,
    darwinLoadedBinding: "@oxfmt/binding-darwin-arm64",
    linuxInstalledBindings: ["@oxfmt/binding-linux-x64-gnu", "@oxfmt/binding-linux-x64-musl"],
    linuxLoadedBinding: "@oxfmt/binding-linux-x64-gnu",
    rootBunLockAuthority: "LEGACY_CONTEXT_ONLY",
    rootBunLock: {
      path: "bun.lock",
      sha256: createHash("sha256").update(rootBunLock).digest("hex"),
      registryContext: "registry.npmmirror.com",
      generatorExecutionAuthority: false,
    },
    installed,
  });

  const go = resolve(materialRoot, "toolchains/darwin-arm64/go/bin/go");
  const pluginRecords = (["darwin-arm64", "linux-amd64"] as const).flatMap((platform) =>
    (["protoc-gen-go", "protoc-gen-connect-go"] as const).map((name) => {
      const path = resolve(materialRoot, "plugins", platform, name);
      return {
        platform,
        name,
        ...fileRecord(path),
        buildReceipt: execFileSync(go, ["version", "-m", path], { encoding: "utf8" })
          .trim()
          .split("\n")
          .map((line) => line.trim().replace(path, `generator-supply://${platform}/${name}`)),
      };
    }),
  );
  writeEvidence("go-plugins.json", {
    formatVersion: "cloud-agents-generator-supply-go-plugins/v1",
    status: "EXACT_MODULE_SUM_AND_BUILD_RECEIPT_VERIFIED",
    platforms: 2,
    binaries: 4,
    moduleAuthority: {
      path: "tools/generator-supply/go/go.mod",
      sha256: fileRecord(resolve(root, "tools/generator-supply/go/go.mod")).sha256,
      sumPath: "tools/generator-supply/go/go.sum",
      sumSha256: fileRecord(resolve(root, "tools/generator-supply/go/go.sum")).sha256,
    },
    plugins: pluginRecords,
  });

  writeEvidence("wheelhouse-repair-lineage.json", {
    formatVersion: "cloud-agents-generator-supply-wheelhouse-repair-lineage/v1",
    status: "APPROVE_P0_0_P1_0_P2_0",
    implementationCommit: "51ce2d6b9faa71a5e89ccf709864f4d570454a38",
    implementationTree: "4f414c088258b09850f7dd440ac0eafcacb938c1",
    implementationParent: "73ba42cb8d5d17833dd96532b2a527f9ed7250f9",
    implementationDocumentSha256:
      "095cd53974416223b1164b14692bcd0f5ab54589c8d8e282830490cf5af9f927",
    reviewCommit: "2a8600b5694b45e39b0c209ae97cbe8f03561339",
    reviewTree: "d5af4cd12d19aca505d55292765466cc5461a4f1",
    reviewParent: "51ce2d6b9faa71a5e89ccf709864f4d570454a38",
    reviewDocumentSha256: "2c71a7e0c0da88f939fd74c0c77d700f340e0f1b9133985de6c461d49aaea1ff",
    currentRunnerPath: "scripts/check-platform-contract-standards.ts",
    currentRunnerSha256: fileRecord(resolve(root, "scripts/check-platform-contract-standards.ts"))
      .sha256,
  });
}

function wheelTags(filename: string): string[] {
  const stem = filename.slice(0, -4);
  const parts = stem.split("-");
  if (parts.length < 5) throw new Error(`Invalid wheel filename ${filename}`);
  return parts.slice(-3);
}

function fileRecord(path: string): { sha256: string; sizeBytes: number } {
  requireRegular(path);
  const bytes = readFileSync(path);
  return { sha256: createHash("sha256").update(bytes).digest("hex"), sizeBytes: bytes.byteLength };
}

function requireRegular(path: string): void {
  const metadata = lstatSync(path);
  if (!metadata.isFile() || metadata.isSymbolicLink())
    throw new Error(`Expected regular file: ${path}`);
}

function directoryContentManifest(
  directory: string,
  lockPackagePaths: readonly string[],
): {
  algorithm: string;
  sha256: string;
  files: number;
  symlinks: readonly { path: string; target: string }[];
  topLevelEntries: readonly string[];
  declaredPackageRoots: readonly string[];
  undeclaredEntries: number;
  cacheEntries: number;
} {
  const directoryReal = realpathSync(directory);
  const files: { path: string; sha256: string; mode: string }[] = [];
  const allowedSymlinks = new Map([
    [".bin/oxfmt", "../oxfmt/bin/oxfmt"],
    [".bin/protoc-gen-es", "../@bufbuild/protoc-gen-es/bin/protoc-gen-es"],
    [".bin/tsc", "../typescript/bin/tsc"],
    [".bin/tsserver", "../typescript/bin/tsserver"],
  ]);
  const declaredPackageRoots = lockPackagePaths
    .map((path) => {
      if (!path.startsWith("node_modules/") || path === "node_modules/") {
        throw new Error(`Invalid hidden-lock package path ${path}.`);
      }
      return path.slice("node_modules/".length);
    })
    .toSorted(utf8BytewiseCompare);
  if (declaredPackageRoots.length !== new Set(declaredPackageRoots).size) {
    throw new Error("Generator npm hidden lock contains duplicate package roots.");
  }
  const expectedTopLevel = [
    ".bin",
    ".package-lock.json",
    ...new Set(declaredPackageRoots.map((path) => path.split("/")[0]!)),
  ].toSorted(utf8BytewiseCompare);
  const topLevelEntries = readdirSync(directory).toSorted(utf8BytewiseCompare);
  if (JSON.stringify(topLevelEntries) !== JSON.stringify(expectedTopLevel)) {
    throw new Error(
      `Generator npm closure has undeclared top-level entries: expected=${JSON.stringify(expectedTopLevel)} actual=${JSON.stringify(topLevelEntries)}.`,
    );
  }
  const isDeclaredPath = (path: string): boolean =>
    path === ".package-lock.json" ||
    path === ".bin" ||
    allowedSymlinks.has(path) ||
    declaredPackageRoots.some(
      (root) => path === root || path.startsWith(`${root}/`) || root.startsWith(`${path}/`),
    );
  const symlinks: { path: string; target: string }[] = [];
  const visit = (current: string, prefix: string): void => {
    for (const name of readdirSync(current).toSorted(utf8BytewiseCompare)) {
      const path = prefix === "" ? name : `${prefix}/${name}`;
      if (path.split("/").some((segment) => segment === ".vite" || segment === ".cache")) {
        throw new Error(`Generator npm closure contains forbidden runtime cache ${path}.`);
      }
      if (!isDeclaredPath(path)) {
        throw new Error(`Generator npm closure contains undeclared path ${path}.`);
      }
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
          throw new Error(`Generator npm closure has an unapproved or escaping symlink ${path}.`);
        }
        const targetMetadata = lstatSync(targetAbsolute);
        if (!targetMetadata.isFile() || targetMetadata.isSymbolicLink()) {
          throw new Error(
            `Generator npm closure symlink ${path} must resolve once to a regular file.`,
          );
        }
        files.push({
          path,
          sha256: createHash("sha256").update(target, "utf8").digest("hex"),
          mode: "120000",
        });
        symlinks.push({ path, target });
      } else if (metadata.isDirectory()) {
        visit(absolute, path);
      } else if (metadata.isFile()) {
        files.push({
          path,
          sha256: fileRecord(absolute).sha256,
          mode: metadata.mode & 0o111 ? "100755" : "100644",
        });
      } else {
        throw new Error(`Generator npm closure has unsupported entry ${path}.`);
      }
    }
  };
  visit(directory, "");
  if (
    JSON.stringify(symlinks) !==
    JSON.stringify([...allowedSymlinks].map(([path, target]) => ({ path, target })))
  ) {
    throw new Error("Generator npm closure must contain the exact npm-created .bin symlink set.");
  }
  files.sort((left, right) => utf8BytewiseCompare(left.path, right.path));
  symlinks.sort((left, right) => utf8BytewiseCompare(left.path, right.path));
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
  return {
    algorithm: "utf8-bytewise-sorted-path-nul-sha256-nul-git-mode-v1",
    sha256: digest.digest("hex"),
    files: files.length,
    symlinks,
    topLevelEntries,
    declaredPackageRoots,
    undeclaredEntries: 0,
    cacheEntries: 0,
  };
}

function utf8BytewiseCompare(left: string, right: string): number {
  return Buffer.from(left, "utf8").compare(Buffer.from(right, "utf8"));
}

function parseJson(path: string): Json {
  return JSON.parse(readFileSync(path, "utf8")) as Json;
}

function writeEvidence(name: string, value: Json): void {
  const path = resolve(root, "tools/generator-supply/v1/evidence", name);
  mkdirSync(resolve(path, ".."), { recursive: true });
  atomicWrite(path, `${JSON.stringify(value, null, 2)}\n`);
}

function atomicWrite(path: string, contents: string): void {
  const temporary = `${path}.tmp-${process.pid}-${Date.now()}`;
  try {
    writeFileSync(temporary, contents, { flag: "wx", mode: 0o600 });
    renameSync(temporary, path);
  } catch (error) {
    try {
      unlinkSync(temporary);
    } catch {
      // Keep the previous evidence bytes untouched if cleanup cannot run.
    }
    throw error;
  }
}
