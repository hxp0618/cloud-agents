import { dirname, relative, resolve, sep } from "node:path";

import {
  dependencyFileRecords,
  generatedFileRecord,
  normalizedFileManifestDigest,
  outputTreeDigest,
  PLATFORM_SDK_MANIFEST_LIBRARY_PATH,
  readRegularFile,
  regularFileDigest,
  SDK_INPUT_MANIFEST_ALGORITHM,
  SDK_OUTPUT_TREE_ALGORITHM,
  writeSDKFiles,
} from "./platform-sdk-manifest";

export const GO_IDENTITY_OUTPUT_PATH = "sdk/go/gen/common/v1alpha1/identity_generated.go";
export const GO_IDENTITY_MANIFEST_PATH = "sdk/go/generated-manifest.json";
export const TYPESCRIPT_IDENTITY_OUTPUT_PATH = "sdk/typescript/src/index.ts";
export const TYPESCRIPT_IDENTITY_MANIFEST_PATH = "sdk/typescript/generated-manifest.json";

const GO_TEMPLATE_PATH = "scripts/templates/platform-identity-sdk-go.tmpl";
const TYPESCRIPT_TEMPLATE_PATH = "scripts/templates/platform-identity-sdk-typescript.tmpl";
const GENERATOR_PATH = "scripts/generate-platform-identity-sdks.ts";
const LIBRARY_PATH = "scripts/lib/platform-identity-sdk.ts";
const ENTRY_PATH = "docs/plan/p1/sdk-identity-closure-entry-20260820.md";
const GO_DEPENDENCY_REVIEW_PATH =
  "docs/plan/p1/dependency-reviews/x-text-v0.39.0-go-sdk-use-20260820.md";
const GO_MODULE_PATH = "sdk/go/go.mod";
const GO_SUM_PATH = "sdk/go/go.sum";
const GO_NOTICE_PATH = "sdk/go/THIRD_PARTY_NOTICES.md";
const TYPESCRIPT_PACKAGE_PATH = "sdk/typescript/package.json";
const BUN_LOCK_PATH = "bun.lock";
const TYPESCRIPT_NOTICE_PATH = "sdk/typescript/THIRD_PARTY_NOTICES.md";
const FIXTURE_MANIFEST_PATH = "contracts/common/v1alpha1/fixtures/manifest.json";
const NAMESPACE_SCHEMA_PATH = "contracts/common/v1alpha1/schemas/namespace-ref.schema.json";
const SUBJECT_SCHEMA_PATH = "contracts/common/v1alpha1/schemas/subject-ref.schema.json";
const NAMESPACE_SCHEMA_SHA256 =
  "sha256:2303ad9902bda5c50476c9b28d88bae708396ae413fe22321ffe4eb946978a99";
const SUBJECT_SCHEMA_SHA256 =
  "sha256:766e571265096b6f1a092eb587048bcfc955ddae308db22c9afd08fed5dc931c";

type FixtureManifest = {
  readonly cases: ReadonlyArray<{
    readonly schema?: unknown;
    readonly instance?: unknown;
    readonly document?: unknown;
  }>;
};

type GeneratedOutput = {
  readonly path: string;
  readonly source: string;
};

export function identitySDKGeneratorSources(): string[] {
  return [
    GENERATOR_PATH,
    LIBRARY_PATH,
    GO_TEMPLATE_PATH,
    TYPESCRIPT_TEMPLATE_PATH,
    PLATFORM_SDK_MANIFEST_LIBRARY_PATH,
  ].toSorted();
}

export function identitySDKContractInputs(root: string): string[] {
  const manifest = readJSON<FixtureManifest>(root, FIXTURE_MANIFEST_PATH);
  const fixtureRoot = dirname(FIXTURE_MANIFEST_PATH);
  const selected = manifest.cases
    .filter(
      (entry) =>
        entry.schema === "../schemas/namespace-ref.schema.json" ||
        entry.schema === "../schemas/subject-ref.schema.json",
    )
    .map((entry) => {
      const fixture = entry.document ?? entry.instance;
      if (typeof fixture !== "string") {
        throw new Error("Identity fixture entries must reference a checked-in document.");
      }
      return normalizeRelativePath(resolve(root, fixtureRoot, fixture), root);
    });
  const inputs = [
    ENTRY_PATH,
    FIXTURE_MANIFEST_PATH,
    NAMESPACE_SCHEMA_PATH,
    SUBJECT_SCHEMA_PATH,
    ...selected,
  ].toSorted();
  if (new Set(inputs).size !== inputs.length) {
    throw new Error("Identity SDK contract inputs must be unique.");
  }
  return inputs;
}

export function buildIdentitySDKOutputs(root: string): ReadonlyArray<GeneratedOutput> {
  validateIdentityAuthority(root);
  return [
    {
      path: GO_IDENTITY_OUTPUT_PATH,
      source: readText(root, GO_TEMPLATE_PATH),
    },
    {
      path: TYPESCRIPT_IDENTITY_OUTPUT_PATH,
      source: readText(root, TYPESCRIPT_TEMPLATE_PATH),
    },
  ];
}

export function buildIdentitySDKManifests(
  root: string,
  outputs = buildIdentitySDKOutputs(root),
): ReadonlyArray<GeneratedOutput> {
  const contractInputs = identitySDKContractInputs(root);
  const generatorSources = identitySDKGeneratorSources();
  const common = {
    formatVersion: "cloud-agents-generated-sdk-manifest/v2",
    profile: "cloud-agents-common-identity/v1alpha1",
    status: "GENERATED_NON_GATE_EVIDENCE",
    notGateClosure: true,
    contract: {
      inputManifestAlgorithm: SDK_INPUT_MANIFEST_ALGORITHM,
      inputManifestSha256: normalizedFileManifestDigest(root, contractInputs),
      inputs: contractInputs,
    },
    generator: {
      id: "platform-common-identity-sdk-generator",
      version: "v2",
      entrypoint: GENERATOR_PATH,
      sourceManifestAlgorithm: SDK_INPUT_MANIFEST_ALGORITHM,
      sourceManifestSha256: normalizedFileManifestDigest(root, generatorSources),
      sources: generatorSources,
      dependencies: dependencyFileRecords(root, [
        ["mise", ".mise.toml"],
        ["rootPackage", "package.json"],
        ["bunLock", BUN_LOCK_PATH],
      ]),
    },
    implementationBoundary: {
      gateClosure: false,
      httpSurface: "NOT_IMPLEMENTED",
      p2Surface: "NOT_IMPLEMENTED",
      providerSideEffects: "FORBIDDEN",
      productionDatabaseWrites: "NOT_AUTHORIZED",
      publication: "NOT_AUTHORIZED",
    },
  } as const;
  const byPath = new Map(outputs.map((output) => [output.path, output]));
  const packageMetadata = typescriptPackageMetadata(root);
  const manifests = [
    {
      language: "go",
      packageIdentity: "github.com/hxp0618/cloud-agents/sdk/go/gen/common/v1alpha1",
      packagePrivate: undefined,
      runtimeDependencies: [
        {
          module: "golang.org/x/text",
          version: "v0.39.0",
          package: "golang.org/x/text/unicode/norm",
          license: "BSD-3-Clause",
        },
      ],
      dependencyFiles: dependencyFileRecords(root, [
        ["goMod", GO_MODULE_PATH],
        ["goSum", GO_SUM_PATH],
        ["notice", GO_NOTICE_PATH],
        ["review", GO_DEPENDENCY_REVIEW_PATH],
      ]),
      outputPaths: [GO_IDENTITY_OUTPUT_PATH],
      manifestPath: GO_IDENTITY_MANIFEST_PATH,
    },
    {
      language: "typescript",
      packageIdentity: packageMetadata.name,
      packagePrivate: packageMetadata.private,
      runtimeDependencies: [],
      dependencyFiles: dependencyFileRecords(root, [
        ["package", TYPESCRIPT_PACKAGE_PATH],
        ["bunLock", BUN_LOCK_PATH],
        ["notice", TYPESCRIPT_NOTICE_PATH],
      ]),
      outputPaths: [TYPESCRIPT_IDENTITY_OUTPUT_PATH],
      manifestPath: TYPESCRIPT_IDENTITY_MANIFEST_PATH,
    },
  ] as const;
  return manifests.map((manifest) => {
    const files = manifest.outputPaths.map((path) => {
      const output = byPath.get(path);
      if (output === undefined) throw new Error(`Missing identity output ${path}.`);
      return generatedFileRecord(output.path, output.source);
    });
    return {
      path: manifest.manifestPath,
      source: `${JSON.stringify(
        {
          ...common,
          language: manifest.language,
          packageIdentity: manifest.packageIdentity,
          packagePrivate: manifest.packagePrivate,
          runtimeDependencies: manifest.runtimeDependencies,
          dependencyFiles: manifest.dependencyFiles,
          outputTreeAlgorithm: SDK_OUTPUT_TREE_ALGORITHM,
          outputTreeSha256: outputTreeDigest(files),
          outputs: files,
        },
        null,
        2,
      )}\n`,
    };
  });
}

export function expectedIdentitySDKFiles(root: string): ReadonlyArray<GeneratedOutput> {
  const outputs = buildIdentitySDKOutputs(root);
  return [...outputs, ...buildIdentitySDKManifests(root, outputs)];
}

export function assertIdentitySDKCurrent(root: string): void {
  for (const output of expectedIdentitySDKFiles(root)) {
    const actual = readText(root, output.path);
    if (actual !== output.source) {
      throw new Error(
        `${output.path} is stale; run bun scripts/generate-platform-identity-sdks.ts --write.`,
      );
    }
  }
}

export function writeIdentitySDKFiles(root: string): void {
  writeSDKFiles(
    root,
    expectedIdentitySDKFiles(root).map((output) => ({
      path: output.path,
      bytes: output.source,
    })),
  );
}

function validateIdentityAuthority(root: string): void {
  if (fileDigest(root, NAMESPACE_SCHEMA_PATH) !== NAMESPACE_SCHEMA_SHA256) {
    throw new Error("NamespaceRef schema changed; assign a new generated identity profile.");
  }
  if (fileDigest(root, SUBJECT_SCHEMA_PATH) !== SUBJECT_SCHEMA_SHA256) {
    throw new Error("SubjectRef schema changed; assign a new generated identity profile.");
  }
  const namespace = readJSON<Record<string, unknown>>(root, NAMESPACE_SCHEMA_PATH);
  const subject = readJSON<Record<string, unknown>>(root, SUBJECT_SCHEMA_PATH);
  if (
    namespace.$id !==
      "https://schemas.cloud-agents.dev/common/v1alpha1/schemas/namespace-ref.schema.json" ||
    subject.$id !==
      "https://schemas.cloud-agents.dev/common/v1alpha1/schemas/subject-ref.schema.json" ||
    namespace.additionalProperties !== false ||
    subject.additionalProperties !== false
  ) {
    throw new Error("Identity schema authority is not strict or has changed identity.");
  }
  const namespaceRequired = namespace.required;
  const subjectRequired = subject.required;
  if (
    JSON.stringify(namespaceRequired) !== JSON.stringify(["namespace", "kind", "id"]) ||
    JSON.stringify(subjectRequired) !== JSON.stringify(["kind", "issuer", "subject"])
  ) {
    throw new Error("Identity schema required fields drifted.");
  }
}

function normalizeRelativePath(target: string, root: string): string {
  const path = relative(root, target).split(sep).join("/");
  if (path === ".." || path.startsWith("../") || path.startsWith("/")) {
    throw new Error(`Identity input escapes the repository root: ${target}.`);
  }
  return path;
}

function fileDigest(root: string, path: string): string {
  return regularFileDigest(root, path);
}

function typescriptPackageMetadata(root: string): { name: string; private: true } {
  const packageJSON = readJSON<{ readonly name?: unknown; readonly private?: unknown }>(
    root,
    TYPESCRIPT_PACKAGE_PATH,
  );
  if (typeof packageJSON.name !== "string" || !packageJSON.name.startsWith("@cloud-agents/")) {
    throw new Error(
      `TypeScript SDK package name must be a public @cloud-agents package: ${String(packageJSON.name)}.`,
    );
  }
  if (packageJSON.private !== true) {
    throw new Error("TypeScript SDK package must declare private: true.");
  }
  return { name: packageJSON.name, private: true };
}

function readText(root: string, path: string): string {
  return readRegularFile(root, path).toString("utf8");
}

function readJSON<T>(root: string, path: string): T {
  return JSON.parse(readText(root, path)) as T;
}
