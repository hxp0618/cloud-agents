import { spawnSync } from "node:child_process";
import {
  chmodSync,
  copyFileSync,
  existsSync,
  mkdirSync,
  mkdtempSync,
  readFileSync,
  rmSync,
  writeFileSync,
} from "node:fs";
import { tmpdir } from "node:os";
import { basename, join, resolve } from "node:path";
import { createRequire } from "node:module";

import {
  assertSameCloudAgentBits,
  cloudAgentStableImportSpecifiers,
  cloudAgentTarballClosure,
  cloudAgentCandidateDigest,
  CLOUD_AGENT_PUBLIC_PACKAGES,
  parseCloudAgentReleaseSmokeOptions,
  type CloudAgentPublicPackageName,
  type PackedCloudAgentPackage,
  sha256File,
  validatePackedCloudAgentSet,
} from "./lib/cloud-agent-release.ts";
type JSONRecord = Record<string, unknown>;
type PackedBinConformanceReport = {
  readonly passed: ReadonlyArray<string>;
  readonly realProviderGates: ReadonlyArray<string>;
};

const repositoryRoot = resolve(import.meta.dirname, "..");
const requireModule = createRequire(import.meta.url);
const packageDirectories: ReadonlyArray<readonly [CloudAgentPublicPackageName, string]> = [
  ["@cloud-agents/cloud-agent-protocol", "packages/cloud-agent-protocol"],
  ["@cloud-agents/cloud-agent-provider-api", "packages/cloud-agent-provider-api"],
  ["@cloud-agents/cloud-agent-runtime", "packages/cloud-agent-runtime"],
  ["@cloud-agents/cloud-agent-provider-codex", "packages/cloud-agent-provider-codex"],
  ["@cloud-agents/cloud-agent-provider-claude", "packages/cloud-agent-provider-claude"],
  ["@cloud-agents/cloud-agent-testkit", "packages/cloud-agent-testkit"],
  ["@cloud-agents/cloud-agent-distribution", "packages/cloud-agent-distribution"],
];

const options = parseCloudAgentReleaseSmokeOptions(process.argv.slice(2), repositoryRoot);
assertNode24();
const sourceStatus = run(
  "git",
  ["status", "--porcelain=v1", "--untracked-files=all"],
  repositoryRoot,
);
if (sourceStatus.trim() && !options.allowDirty) {
  throw new Error(
    "Cloud Agent release smoke requires a clean source tree; use --allow-dirty only for local source validation.",
  );
}
if (existsSync(options.outputDirectory)) {
  throw new Error(`Candidate output already exists: ${options.outputDirectory}`);
}
mkdirSync(options.outputDirectory, { recursive: true, mode: 0o755 });

for (const [, directory] of packageDirectories) {
  run("bun", ["run", "--cwd", directory, "build"], repositoryRoot);
}

const packedManifests: JSONRecord[] = [];
const packedPackages: PackedCloudAgentPackage[] = [];
for (const [expectedName, directory] of packageDirectories) {
  const packOutput = run(
    "npm",
    [
      "pack",
      "--json",
      "--pack-destination",
      options.outputDirectory,
      resolve(repositoryRoot, directory),
    ],
    repositoryRoot,
  );
  const packRecords = parseJSONArray(packOutput, `${expectedName} npm pack output`);
  if (packRecords.length !== 1 || !isRecord(packRecords[0])) {
    throw new Error(`${expectedName} npm pack did not return exactly one artifact.`);
  }
  const filename = requireString(packRecords[0].filename, `${expectedName} tarball filename`);
  const tarball = join(options.outputDirectory, basename(filename));
  const manifest = readTarballJSON(tarball, "package/package.json");
  if (manifest.name !== expectedName) {
    throw new Error(`${tarball} contains ${String(manifest.name)}, expected ${expectedName}.`);
  }
  assertPackedFileAllowlist(tarball);
  assertPhysicalPackageBoundary(tarball, expectedName);
  assertNoLegacyProviderFacade(tarball, expectedName);
  packedManifests.push(manifest);
  packedPackages.push({
    name: expectedName,
    version: requireString(manifest.version, `${expectedName} version`),
    filename: basename(tarball),
    sha256: sha256File(tarball),
  });
}

validatePackedCloudAgentSet(packedManifests);
validateDistributionManifest(packedManifests);

const beforeSmoke = packedPackages.toSorted((left, right) => left.name.localeCompare(right.name));
const nodeConformance = runExternalNode24Smoke(options.outputDirectory, beforeSmoke);
runExternalPnpm11Smoke(options.outputDirectory, beforeSmoke);
const packedBinConformance = {
  ...nodeConformance,
  passed: [...nodeConformance.passed, "pnpm-11-coordinated-peer-install"],
};
const afterSmoke = beforeSmoke.map((item) => ({
  ...item,
  sha256: sha256File(join(options.outputDirectory, item.filename)),
}));
assertSameCloudAgentBits(beforeSmoke, afterSmoke);
for (const item of afterSmoke) chmodSync(join(options.outputDirectory, item.filename), 0o444);

const candidate = {
  schemaVersion: 1,
  kind: "cloud-agent-portable-runtime-rc-candidate",
  candidateDigest: cloudAgentCandidateDigest(afterSmoke),
  sourceCommit: run("git", ["rev-parse", "HEAD"], repositoryRoot).trim(),
  sourceDirty: sourceStatus.trim() !== "",
  nodeVersion: process.version,
  npmVersion: run("npm", ["--version"], repositoryRoot).trim(),
  bunVersion: run("bun", ["--version"], repositoryRoot).trim(),
  platform: `${process.platform}-${process.arch}`,
  sameBitsVerified: true,
  packedBinConformance,
  standaloneRuntime: standaloneRuntimeArtifact(options.outputDirectory),
  packages: afterSmoke,
};
writeFileSync(
  join(options.outputDirectory, "candidate-manifest.json"),
  `${JSON.stringify(candidate, null, 2)}\n`,
  { mode: 0o444 },
);
writeFileSync(
  join(options.outputDirectory, "checksums.sha256"),
  `${[
    ...afterSmoke.map((item) => `${item.sha256.slice("sha256:".length)}  ${item.filename}`),
    `${candidate.standaloneRuntime.sha256.slice("sha256:".length)}  ${candidate.standaloneRuntime.filename}`,
  ]
    .toSorted()
    .join("\n")}\n`,
  { mode: 0o444 },
);
run(
  process.execPath,
  ["scripts/generate-sbom.ts", "--output-dir", options.outputDirectory],
  repositoryRoot,
);
writeFileSync(
  join(options.outputDirectory, "provenance.json"),
  `${JSON.stringify(
    {
      _type: "https://in-toto.io/Statement/v1",
      predicateType: "https://slsa.dev/provenance/v1",
      subject: afterSmoke.map((item) => ({
        name: item.filename,
        digest: { sha256: item.sha256.slice("sha256:".length) },
      })),
      predicate: {
        buildDefinition: {
          buildType: "https://github.com/hxp0618/cloud-agents/release-smoke/v1",
          externalParameters: { sourceCommit: candidate.sourceCommit },
          internalParameters: { sourceDirty: candidate.sourceDirty },
          resolvedDependencies: [],
        },
        runDetails: {
          builder: {
            id:
              process.env.CLOUD_AGENT_RELEASE_PROVENANCE_BUILDER ??
              "local-cloud-agent-release-smoke",
          },
          metadata: { invocationId: candidate.candidateDigest },
        },
      },
    },
    null,
    2,
  )}\n`,
  { mode: 0o444 },
);
process.stdout.write(`${JSON.stringify(candidate, null, 2)}\n`);

function standaloneRuntimeArtifact(outputDirectory: string): {
  readonly filename: string;
  readonly sha256: string;
} {
  const filename = "cloud-agent-runtime-standalone.mjs";
  const target = join(outputDirectory, filename);
  copyFileSync(join(repositoryRoot, "packages/cloud-agent-distribution/dist/stdio.mjs"), target);
  chmodSync(target, 0o555);
  return { filename, sha256: sha256File(target) };
}

function runExternalNode24Smoke(
  candidateDirectory: string,
  packages: ReadonlyArray<PackedCloudAgentPackage>,
): PackedBinConformanceReport {
  const packagesByName = new Map(packages.map((item) => [item.name, item]));
  let packedBinConformance: PackedBinConformanceReport | undefined;
  for (const target of CLOUD_AGENT_PUBLIC_PACKAGES) {
    const externalRoot = mkdtempSync(join(tmpdir(), "cloud-agents-external-smoke-"));
    try {
      writeFileSync(
        join(externalRoot, "package.json"),
        `${JSON.stringify({ name: "cloud-agent-external-smoke", private: true, type: "module" }, null, 2)}\n`,
      );
      const tarballs = cloudAgentTarballClosure(target).map((name) => {
        const item = packagesByName.get(name);
        if (!item) throw new Error(`${target} isolated smoke is missing tarball ${name}.`);
        return join(candidateDirectory, item.filename);
      });
      run(
        "npm",
        [
          "install",
          "--ignore-scripts",
          "--no-audit",
          "--no-fund",
          "--no-package-lock",
          ...tarballs,
        ],
        externalRoot,
      );
      assertInstalledCloudAgentClosure(externalRoot, target);

      const specifiers = cloudAgentStableImportSpecifiers(target);
      const esmSmoke = [
        `const specifiers = ${JSON.stringify(specifiers)};`,
        "for (const specifier of specifiers) await import(specifier);",
      ];
      const cjsSmoke = [
        `const specifiers = ${JSON.stringify(specifiers)};`,
        "for (const specifier of specifiers) require(specifier);",
      ];
      if (target === "@cloud-agents/cloud-agent-distribution") {
        esmSmoke.push(
          'const distribution = await import("@cloud-agents/cloud-agent-distribution");',
          'const schemas = await import("@cloud-agents/cloud-agent-distribution/schemas");',
          "const runtime = distribution.createDefaultCloudAgentRuntime();",
          'if (JSON.stringify(runtime.providerKinds) !== JSON.stringify(["claudeAgent", "codex"])) throw new Error("ESM registry allowlist mismatch");',
          'const claude = await runtime.describe("claudeAgent");',
          'if (!claude.runtime.available || !claude.runtime.compatible || claude.runtime.version !== "0.3.207") throw new Error("ESM Claude SDK descriptor mismatch");',
          'if (schemas.CLOUD_AGENT_ENVELOPE_V2_SCHEMA.$id !== "https://schemas.cloud-agents.dev/cloud-agent/envelope-v2.schema.json") throw new Error("ESM schema export mismatch");',
        );
        cjsSmoke.push(
          'const distribution = require("@cloud-agents/cloud-agent-distribution");',
          'const schemas = require("@cloud-agents/cloud-agent-distribution/schemas");',
          'if (JSON.stringify(distribution.createDefaultCloudAgentRuntime().providerKinds) !== JSON.stringify(["claudeAgent", "codex"])) throw new Error("CJS registry allowlist mismatch");',
          'if (schemas.CLOUD_AGENT_ENVELOPE_V2_SCHEMA.$id !== "https://schemas.cloud-agents.dev/cloud-agent/envelope-v2.schema.json") throw new Error("CJS schema export mismatch");',
        );
      }
      writeFileSync(join(externalRoot, "smoke.mjs"), esmSmoke.join("\n"));
      run(process.execPath, ["smoke.mjs"], externalRoot);
      writeFileSync(join(externalRoot, "smoke.cjs"), cjsSmoke.join("\n"));
      run(process.execPath, ["smoke.cjs"], externalRoot);
      runExternalTypeScriptSmoke(externalRoot, specifiers);

      if (target === "@cloud-agents/cloud-agent-distribution") {
        packedBinConformance = runDistributionBinSmoke(externalRoot);
      }
    } finally {
      rmSync(externalRoot, { recursive: true, force: true });
    }
  }
  if (!packedBinConformance) throw new Error("Packed bin conformance did not run.");
  return packedBinConformance;
}

function runExternalPnpm11Smoke(
  candidateDirectory: string,
  packages: ReadonlyArray<PackedCloudAgentPackage>,
): void {
  const externalRoot = mkdtempSync(join(tmpdir(), "cloud-agents-pnpm-smoke-"));
  try {
    const packagesByName = new Map(packages.map((item) => [item.name, item]));
    const dependencies = Object.fromEntries(
      cloudAgentTarballClosure("@cloud-agents/cloud-agent-distribution").map((name) => {
        const item = packagesByName.get(name);
        if (!item) throw new Error(`pnpm smoke is missing tarball ${name}.`);
        return [name, `file:${join(candidateDirectory, item.filename)}`];
      }),
    );
    writeFileSync(
      join(externalRoot, "package.json"),
      `${JSON.stringify(
        {
          name: "cloud-agent-pnpm-external-smoke",
          private: true,
          type: "module",
          packageManager: "pnpm@11.10.0",
          dependencies,
        },
        null,
        2,
      )}\n`,
    );
    const pnpm = join(repositoryRoot, "node_modules", ".bin", "pnpm");
    if (run(pnpm, ["--version"], externalRoot).trim() !== "11.10.0") {
      throw new Error("Cloud Agent release smoke did not use pnpm 11.10.0.");
    }
    run(
      pnpm,
      [
        "install",
        "--ignore-scripts",
        "--no-frozen-lockfile",
        "--registry=https://registry.npmjs.org/",
        "--store-dir=.pnpm-store",
      ],
      externalRoot,
    );
    assertInstalledCloudAgentClosure(externalRoot, "@cloud-agents/cloud-agent-distribution");
    const lock = readFileSync(join(externalRoot, "pnpm-lock.yaml"), "utf8");
    if (/registry\.npmjs\.org\/@cloud-agents(?:%2f|\/)/iu.test(lock)) {
      throw new Error("pnpm resolved an unpublished Cloud Agent package through npm.");
    }
  } finally {
    rmSync(externalRoot, { recursive: true, force: true });
  }
}

function runExternalTypeScriptSmoke(externalRoot: string, specifiers: ReadonlyArray<string>): void {
  const esmNamespaces = specifiers.map((specifier, index) => ({
    name: `CloudAgentEsmPackage${String(index)}`,
    specifier,
  }));
  const cjsNamespaces = specifiers.map((specifier, index) => ({
    name: `CloudAgentCjsPackage${String(index)}`,
    specifier,
  }));
  writeFileSync(
    join(externalRoot, "types-smoke.mts"),
    `${[
      ...esmNamespaces.map(
        ({ name, specifier }) => `import type * as ${name} from ${JSON.stringify(specifier)};`,
      ),
      `export type CloudAgentEsmSurface = ${esmNamespaces
        .map(({ name }) => `keyof typeof ${name}`)
        .join(" | ")};`,
    ].join("\n")}\n`,
  );
  writeFileSync(
    join(externalRoot, "types-smoke.cts"),
    `${[
      ...cjsNamespaces.map(
        ({ name, specifier }) => `import ${name} = require(${JSON.stringify(specifier)});`,
      ),
      `export type CloudAgentCjsSurface = ${cjsNamespaces
        .map(({ name }) => `keyof typeof ${name}`)
        .join(" | ")};`,
    ].join("\n")}\n`,
  );
  writeFileSync(
    join(externalRoot, "tsconfig.json"),
    `${JSON.stringify(
      {
        compilerOptions: {
          target: "ES2022",
          module: "NodeNext",
          moduleResolution: "NodeNext",
          strict: true,
          noEmit: true,
          skipLibCheck: false,
          types: ["node"],
          typeRoots: [join(repositoryRoot, "node_modules", "@types")],
        },
        files: ["types-smoke.mts", "types-smoke.cts"],
      },
      null,
      2,
    )}\n`,
  );
  run(
    process.execPath,
    [
      join(repositoryRoot, "node_modules", "typescript", "bin", "tsc"),
      "--project",
      "tsconfig.json",
    ],
    externalRoot,
  );
}

function assertInstalledCloudAgentClosure(
  externalRoot: string,
  target: CloudAgentPublicPackageName,
): void {
  const expected = cloudAgentTarballClosure(target);
  const installed = CLOUD_AGENT_PUBLIC_PACKAGES.filter((name) =>
    existsSync(join(externalRoot, "node_modules", name, "package.json")),
  );
  if (JSON.stringify(installed) !== JSON.stringify(expected)) {
    throw new Error(
      `${target} isolated smoke installed Cloud Agent packages [${installed.join(", ")}], expected [${expected.join(", ")}].`,
    );
  }
}

function runDistributionBinSmoke(externalRoot: string): PackedBinConformanceReport {
  const describes = ["codex", "claudeAgent"].map((provider) =>
    JSON.stringify({
      requestId: `release-smoke-describe-${provider}`,
      protocolVersion: { major: 2, minor: 3 },
      executionId: "release-smoke",
      generation: 1,
      commandType: "Describe",
      commandId: `release-smoke-describe-${provider}`,
      occurredAt: "2026-08-09T00:00:00.000Z",
      payload: { provider },
    }),
  );
  const binOutput = run(
    join(externalRoot, "node_modules", ".bin", "cloud-agent-runtime"),
    ["--protocol-v2"],
    externalRoot,
    `${describes.join("\n")}\n`,
  );
  assertDistributionDescribeOutput(binOutput, "packed bin");

  const distributionRoot = join(
    externalRoot,
    "node_modules",
    "@cloud-agents",
    "cloud-agent-distribution",
  );
  const distribution = requireModule(join(distributionRoot, "dist", "index.cjs")) as {
    resolveCloudAgentRuntimeLaunch?: (
      packageRoot: string,
      nodeExecutable: string,
    ) => { readonly executable: string; readonly args: ReadonlyArray<string> };
  };
  if (typeof distribution.resolveCloudAgentRuntimeLaunch !== "function") {
    throw new Error("Packed Distribution omitted the cross-platform Runtime launch helper.");
  }
  const launch = distribution.resolveCloudAgentRuntimeLaunch(distributionRoot, process.execPath);
  const launchOutput = run(
    launch.executable,
    [...launch.args, "--protocol-v2"],
    externalRoot,
    `${describes.join("\n")}\n`,
  );
  assertDistributionDescribeOutput(launchOutput, "Node module launch descriptor");

  const testkit = requireBuiltTestkit();
  const report = testkit.runCloudAgentPackedBinConformance({
    executable: join(externalRoot, "node_modules", ".bin", "cloud-agent-runtime"),
  });
  return {
    ...report,
    passed: [...report.passed, "node-module-launch-descriptor"],
  };
}

function assertDistributionDescribeOutput(output: string, label: string): void {
  const messages = output
    .split("\n")
    .filter(Boolean)
    .map((line) => JSON.parse(line) as JSONRecord);
  for (const provider of ["codex", "claudeAgent"]) {
    const terminal = messages.find(
      (message) =>
        message.commandId === `release-smoke-describe-${provider}` &&
        message.messageType === "Result",
    );
    if (!terminal) {
      throw new Error(`${label} did not answer ${provider} Describe.`);
    }
    const descriptor = isRecord(terminal.payload) ? terminal.payload.descriptor : undefined;
    if (!isRecord(descriptor) || descriptor.providerKind !== provider) {
      throw new Error(`${label} Describe did not use the explicit ${provider} registry.`);
    }
    if (provider === "claudeAgent") {
      const runtimeDescriptor = isRecord(descriptor.runtime) ? descriptor.runtime : undefined;
      if (
        runtimeDescriptor?.available !== true ||
        runtimeDescriptor.compatible !== true ||
        runtimeDescriptor.version !== "0.3.207"
      ) {
        throw new Error(`${label} Claude descriptor does not match its pinned SDK.`);
      }
    }
  }
}

function requireBuiltTestkit(): {
  runCloudAgentPackedBinConformance(input: {
    readonly executable: string;
  }): PackedBinConformanceReport;
} {
  const modulePath = join(repositoryRoot, "packages/cloud-agent-testkit/dist/index.cjs");
  const loaded = requireModule(modulePath) as {
    runCloudAgentPackedBinConformance?: (input: {
      readonly executable: string;
    }) => PackedBinConformanceReport;
  };
  if (typeof loaded.runCloudAgentPackedBinConformance !== "function") {
    throw new Error("Built Cloud Agent Testkit omitted packed-bin conformance.");
  }
  return { runCloudAgentPackedBinConformance: loaded.runCloudAgentPackedBinConformance };
}

function validateDistributionManifest(manifests: ReadonlyArray<JSONRecord>): void {
  const versions = Object.fromEntries(
    manifests.map((manifest) => [
      requireString(manifest.name, "package name"),
      requireString(manifest.version, "package version"),
    ]),
  );
  const distributionTarball = join(
    options.outputDirectory,
    packedPackages.find((item) => item.name === "@cloud-agents/cloud-agent-distribution")!.filename,
  );
  const manifest = readTarballJSON(distributionTarball, "package/manifest.json");
  if (manifest.releaseState !== "source" || manifest.releaseDigest !== null) {
    throw new Error(
      "Source Distribution manifest must remain releaseState=source with null digest.",
    );
  }
  if (manifest.distributionVersion !== versions["@cloud-agents/cloud-agent-distribution"]) {
    throw new Error("Distribution manifest version does not match packed bits.");
  }
  if (
    !isRecord(manifest.runtime) ||
    manifest.runtime.version !== versions["@cloud-agents/cloud-agent-runtime"]
  ) {
    throw new Error("Distribution manifest Runtime version does not match packed bits.");
  }
  if (!Array.isArray(manifest.providers))
    throw new Error("Distribution manifest providers are missing.");
  const manifestProviders = manifest.providers.map((provider) => {
    if (!isRecord(provider)) throw new Error("Distribution manifest provider is invalid.");
    return provider;
  });
  const expected = [
    ["codex", "@cloud-agents/cloud-agent-provider-codex"],
    ["claudeAgent", "@cloud-agents/cloud-agent-provider-claude"],
  ] as const;
  if (manifestProviders.length !== expected.length) {
    throw new Error("Distribution manifest Provider allowlist contains an unexpected entry.");
  }
  for (const [kind, packageName] of expected) {
    const provider = manifestProviders.find((candidate) => candidate.kind === kind);
    if (
      !provider ||
      provider.package !== packageName ||
      provider.version !== versions[packageName]
    ) {
      throw new Error(`Distribution manifest ${kind} pin does not match packed bits.`);
    }
  }
  if (
    !isRecord(manifest.schemas) ||
    manifest.schemas.cloudAgentEnvelopeV2 !== "./schemas/cloud-agent-envelope-v2"
  ) {
    throw new Error("Distribution manifest schema export is missing or unstable.");
  }
}

function readTarballJSON(tarball: string, path: string): JSONRecord {
  const source = run("tar", ["-xOf", tarball, path], repositoryRoot);
  const parsed: unknown = JSON.parse(source);
  if (!isRecord(parsed)) throw new Error(`${tarball}:${path} is not a JSON object.`);
  return parsed;
}

function assertNoLegacyProviderFacade(
  tarball: string,
  packageName: CloudAgentPublicPackageName,
): void {
  if (
    packageName !== "@cloud-agents/cloud-agent-provider-codex" &&
    packageName !== "@cloud-agents/cloud-agent-provider-claude"
  ) {
    return;
  }
  const paths = run("tar", ["-tf", tarball], repositoryRoot)
    .split("\n")
    .filter((path) => /^package\/(?:dist|src)\/.*\.(?:[cm]?[jt]s|[mc]ts)$/u.test(path));
  for (const path of paths) {
    const source = run("tar", ["-xOf", tarball, path], repositoryRoot);
    if (
      source.includes("createLegacyProviderPlugin") ||
      source.includes("@cloud-agents/cloud-agent-runtime/legacy-provider-host")
    ) {
      throw new Error(
        `${packageName} still executes through the legacy Provider facade (${path}).`,
      );
    }
  }
}

function assertPhysicalPackageBoundary(
  tarball: string,
  packageName: CloudAgentPublicPackageName,
): void {
  if (packageName !== "@cloud-agents/cloud-agent-runtime") return;
  const paths = run("tar", ["-tf", tarball], repositoryRoot).split("\n").filter(Boolean);
  const forbidden = paths.find((path) =>
    /^package\/src\/(?:claudeAgentSdkRuntime|codexAppServerRuntime|codexPostToolUseProvenance|providerHost|legacyProviderHost)\./u.test(
      path,
    ),
  );
  if (forbidden) {
    throw new Error(
      `Cloud Agent Runtime still physically contains Provider implementation ${forbidden}.`,
    );
  }
}

function assertPackedFileAllowlist(tarball: string): void {
  const paths = run("tar", ["-tf", tarball], repositoryRoot).split("\n").filter(Boolean);
  for (const path of paths) {
    if (/\.(?:test|spec)\.[cm]?[jt]sx?$/u.test(path)) {
      throw new Error(`${tarball} unexpectedly contains test source ${path}.`);
    }
    if (
      !/^package\/(?:LICENSE|README\.md|package\.json|manifest\.json|provider-capability-catalog\.json|(?:dist|src|schemas|fixtures)\/)/u.test(
        path,
      )
    ) {
      throw new Error(`${tarball} contains file outside the public allowlist: ${path}.`);
    }
  }
}

function run(command: string, args: ReadonlyArray<string>, cwd: string, input?: string): string {
  const result = spawnSync(command, [...args], {
    cwd,
    encoding: "utf8",
    input,
    maxBuffer: 32 * 1024 * 1024,
    env: { ...process.env, npm_config_update_notifier: "false" },
  });
  if (result.status !== 0) {
    throw new Error(
      `${command} ${args.join(" ")} failed (${String(result.status)}).\n${result.stdout}\n${result.stderr}`,
    );
  }
  return result.stdout;
}

function assertNode24(): void {
  const major = Number(process.versions.node.split(".")[0]);
  if (major !== 24)
    throw new Error(`Cloud Agent release smoke requires Node 24, found ${process.version}.`);
}

function parseJSONArray(value: string, label: string): unknown[] {
  const parsed: unknown = JSON.parse(value);
  if (!Array.isArray(parsed)) throw new Error(`${label} is not a JSON array.`);
  return parsed;
}

function requireString(value: unknown, label: string): string {
  if (typeof value !== "string" || value.trim() === "") throw new Error(`${label} is missing.`);
  return value.trim();
}

function isRecord(value: unknown): value is JSONRecord {
  return value !== null && typeof value === "object" && !Array.isArray(value);
}
