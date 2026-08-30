import { createHash } from "node:crypto";
import { mkdirSync, readFileSync, writeFileSync } from "node:fs";
import { dirname, relative, resolve, sep } from "node:path";

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

export function expectedIdentitySDKFiles(root: string): ReadonlyArray<GeneratedOutput> {
  return buildIdentitySDKOutputs(root);
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
