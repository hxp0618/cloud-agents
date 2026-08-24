import { createHash } from "node:crypto";
import { lstatSync, mkdirSync, readFileSync, writeFileSync } from "node:fs";
import { dirname, relative, resolve, sep } from "node:path";

import { validatePlatformContractTree } from "./platform-contracts";
import { canonicalizeJson } from "./platform-json-semantics";

export const GO_IDENTITY_OUTPUT_PATH = "sdk/go/gen/common/v1alpha1/identity_generated.go";
export const GO_IDENTITY_MANIFEST_PATH = "sdk/go/generated-manifest.json";
export const TYPESCRIPT_IDENTITY_OUTPUT_PATH = "sdk/typescript/src/index.ts";
export const TYPESCRIPT_IDENTITY_MANIFEST_PATH = "sdk/typescript/generated-manifest.json";

const GO_TEMPLATE_PATH = "scripts/templates/platform-identity-sdk-go.tmpl";
const TYPESCRIPT_TEMPLATE_PATH = "scripts/templates/platform-identity-sdk-typescript.tmpl";
const GENERATOR_PATH = "scripts/generate-platform-identity-sdks.ts";
const LIBRARY_PATH = "scripts/lib/platform-identity-sdk.ts";
const TEST_PATH = "scripts/lib/platform-identity-sdk.test.ts";
const ENTRY_PATH = "docs/plan/p1/sdk-identity-closure-entry-20260820.md";
const GO_DEPENDENCY_REVIEW_PATH =
  "docs/plan/p1/dependency-reviews/x-text-v0.39.0-go-sdk-use-20260820.md";
const FIXTURE_MANIFEST_PATH = "contracts/common/v1alpha1/fixtures/manifest.json";
const NAMESPACE_SCHEMA_PATH = "contracts/common/v1alpha1/schemas/namespace-ref.schema.json";
const SUBJECT_SCHEMA_PATH = "contracts/common/v1alpha1/schemas/subject-ref.schema.json";
const NAMESPACE_SCHEMA_SHA256 =
  "sha256:2303ad9902bda5c50476c9b28d88bae708396ae413fe22321ffe4eb946978a99";
const SUBJECT_SCHEMA_SHA256 =
  "sha256:766e571265096b6f1a092eb587048bcfc955ddae308db22c9afd08fed5dc931c";
const MANIFEST_ALGORITHM = "sorted-path-nul-sha256-nul-git-mode-v1";
const OUTPUT_TREE_ALGORITHM = "sorted-path-nul-sha256-nul-v1";

const IDENTITY_CONFIG = {
  profile: "cloud-agents-common-identity/v1alpha1",
  namespaceRef: {
    canonicalization: "RFC8785",
    digest: "SHA-256",
    idNormalization: "NFC",
    strictUnknownFields: true,
  },
  subjectRef: {
    canonicalization: "RFC8785",
    comparison: "exact_string_no_rewrite",
    digest: "SHA-256",
    strictUnknownFields: true,
  },
  languages: {
    go: {
      module: "github.com/hxp0618/cloud-agents/sdk/go",
      package: "github.com/hxp0618/cloud-agents/sdk/go/gen/common/v1alpha1",
      nfcImplementation: "golang.org/x/text/unicode/norm@v0.39.0",
    },
    typescript: {
      package: "@synara/cloud-agent-platform-sdk",
      runtimeDependencies: [],
      sha256Implementation: "WebCrypto.SubtleCrypto",
    },
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

type GeneratedFileRecord = {
  readonly path: string;
  readonly sha256: string;
  readonly sizeBytes: number;
};

export function identitySDKGeneratorSources(): string[] {
  return [
    GENERATOR_PATH,
    LIBRARY_PATH,
    TEST_PATH,
    GO_TEMPLATE_PATH,
    TYPESCRIPT_TEMPLATE_PATH,
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

export function identitySDKConfigDigest(): string {
  return digestBytes(canonicalizeJson(IDENTITY_CONFIG));
}

export function buildIdentitySDKOutputs(root: string): ReadonlyArray<GeneratedOutput> {
  validateIdentityAuthority(root);
  const contractManifest = validatePlatformContractTree(root).contractManifestSha256;
  const generatorManifest = normalizedManifestDigest(root, identitySDKGeneratorSources());
  const replacements = new Map([
    ["{{CONTRACT_MANIFEST_SHA256}}", contractManifest],
    ["{{GENERATOR_SOURCE_MANIFEST_SHA256}}", generatorManifest],
    ["{{CONFIG_DIGEST}}", identitySDKConfigDigest()],
  ]);
  return [
    {
      path: GO_IDENTITY_OUTPUT_PATH,
      source: renderTemplate(readText(root, GO_TEMPLATE_PATH), replacements, GO_TEMPLATE_PATH),
    },
    {
      path: TYPESCRIPT_IDENTITY_OUTPUT_PATH,
      source: renderTemplate(
        readText(root, TYPESCRIPT_TEMPLATE_PATH),
        replacements,
        TYPESCRIPT_TEMPLATE_PATH,
      ),
    },
  ];
}

export function buildIdentitySDKManifests(
  root: string,
  outputs = buildIdentitySDKOutputs(root),
): ReadonlyArray<GeneratedOutput> {
  const contractManifest = validatePlatformContractTree(root).contractManifestSha256;
  const contractInputs = identitySDKContractInputs(root);
  const generatorSources = identitySDKGeneratorSources();
  const common = {
    formatVersion: "cloud-agents-generated-sdk-manifest/v1",
    profile: "cloud-agents-common-identity/v1alpha1",
    status: "GENERATED_NON_GATE_EVIDENCE",
    notGateClosure: true,
    contract: {
      manifestAlgorithm: MANIFEST_ALGORITHM,
      manifestSha256: contractManifest,
      inputManifestSha256: normalizedManifestDigest(root, contractInputs),
      inputs: contractInputs,
    },
    generator: {
      id: "platform-common-identity-sdk-generator",
      version: "v1",
      entrypoint: GENERATOR_PATH,
      sourceManifestAlgorithm: MANIFEST_ALGORITHM,
      sourceManifestSha256: normalizedManifestDigest(root, generatorSources),
      sources: generatorSources,
      configDigest: identitySDKConfigDigest(),
    },
    implementationBoundary: IDENTITY_CONFIG.implementationBoundary,
  };
  const byPath = new Map(outputs.map((output) => [output.path, output]));
  const languageManifests = [
    {
      language: "go",
      packageIdentity: "github.com/hxp0618/cloud-agents/sdk/go/gen/common/v1alpha1",
      runtimeDependencies: [
        {
          module: "golang.org/x/text",
          version: "v0.39.0",
          package: "golang.org/x/text/unicode/norm",
          license: "BSD-3-Clause",
          patentsSha256: "sha256:96f408bfae65bf137fc2525d3ecb030271c50c1e90799f87abf8846d8dd505cc",
          review: GO_DEPENDENCY_REVIEW_PATH,
          bitsReview: "docs/plan/p1/dependency-reviews/x-text-v0.39.0.md",
        },
      ],
      outputPaths: [GO_IDENTITY_OUTPUT_PATH],
      manifestPath: GO_IDENTITY_MANIFEST_PATH,
    },
    {
      language: "typescript",
      packageIdentity: "@synara/cloud-agent-platform-sdk",
      runtimeDependencies: [],
      outputPaths: [TYPESCRIPT_IDENTITY_OUTPUT_PATH],
      manifestPath: TYPESCRIPT_IDENTITY_MANIFEST_PATH,
    },
  ] as const;
  return languageManifests.map((language) => {
    const files = language.outputPaths.map((path) => {
      const output = byPath.get(path);
      if (output === undefined) throw new Error(`Missing identity output ${path}.`);
      return generatedFileRecord(output);
    });
    const document = {
      ...common,
      language: language.language,
      packageIdentity: language.packageIdentity,
      runtimeDependencies: language.runtimeDependencies,
      outputTreeAlgorithm: OUTPUT_TREE_ALGORITHM,
      outputTreeSha256: outputTreeDigest(files),
      outputs: files,
    };
    return {
      path: language.manifestPath,
      source: `${JSON.stringify(document, null, 2)}\n`,
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
  for (const output of expectedIdentitySDKFiles(root)) {
    const target = resolve(root, output.path);
    mkdirSync(dirname(target), { recursive: true });
    writeFileSync(target, output.source);
  }
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

function renderTemplate(
  source: string,
  replacements: ReadonlyMap<string, string>,
  file: string,
): string {
  for (const [placeholder, value] of replacements) {
    const occurrences = source.split(placeholder).length - 1;
    if (occurrences !== 1) {
      throw new Error(`${file} must contain ${placeholder} exactly once.`);
    }
    source = source.replace(placeholder, value);
  }
  if (/\{\{[A-Z0-9_]+\}\}/u.test(source)) {
    throw new Error(`${file} contains an unresolved generator placeholder.`);
  }
  return source;
}

function generatedFileRecord(output: GeneratedOutput): GeneratedFileRecord {
  return {
    path: output.path,
    sha256: digestBytes(output.source),
    sizeBytes: Buffer.byteLength(output.source),
  };
}

function outputTreeDigest(files: ReadonlyArray<GeneratedFileRecord>): string {
  const hash = createHash("sha256");
  for (const file of [...files].toSorted((left, right) => left.path.localeCompare(right.path))) {
    hash.update(file.path).update("\0").update(file.sha256).update("\0");
  }
  return `sha256:${hash.digest("hex")}`;
}

function normalizedManifestDigest(root: string, files: ReadonlyArray<string>): string {
  const entries = files.map((file) => {
    const target = resolve(root, file);
    const path = normalizeRelativePath(target, root);
    const stat = lstatSync(target);
    if (!stat.isFile() || stat.isSymbolicLink()) {
      throw new Error(`Identity manifest input must be a regular file: ${file}.`);
    }
    return {
      path,
      sha256: createHash("sha256").update(readFileSync(target)).digest("hex"),
      mode: (stat.mode & 0o111) === 0 ? "100644" : "100755",
    };
  });
  if (new Set(entries.map((entry) => entry.path)).size !== entries.length) {
    throw new Error("Identity manifest inputs must have unique normalized paths.");
  }
  const hash = createHash("sha256");
  for (const entry of entries.toSorted((left, right) => left.path.localeCompare(right.path))) {
    hash
      .update(entry.path)
      .update("\0")
      .update(entry.sha256)
      .update("\0")
      .update(entry.mode)
      .update("\0");
  }
  return `sha256:${hash.digest("hex")}`;
}

function normalizeRelativePath(target: string, root: string): string {
  const path = relative(root, target).split(sep).join("/");
  if (path === ".." || path.startsWith("../") || path.startsWith("/")) {
    throw new Error(`Identity input escapes the repository root: ${target}.`);
  }
  return path;
}

function fileDigest(root: string, path: string): string {
  return digestBytes(readFileSync(resolve(root, path)));
}

function digestBytes(value: string | Uint8Array): string {
  return `sha256:${createHash("sha256").update(value).digest("hex")}`;
}

function readText(root: string, path: string): string {
  return readFileSync(resolve(root, path), "utf8");
}

function readJSON<T>(root: string, path: string): T {
  return JSON.parse(readText(root, path)) as T;
}
