import { createHash } from "node:crypto";
import { lstatSync, readFileSync, readdirSync, realpathSync, writeFileSync } from "node:fs";
import { isAbsolute, relative, resolve, sep } from "node:path";

import Ajv2020 from "ajv/dist/2020.js";

import { canonicalizeJson, type JsonRecord } from "./platform-json-semantics";

export const CONTRACT_CLOSURE_PROFILE_SOURCE_PATH =
  "contracts/platform/v1alpha1/fixtures/golden/contract-closure-profile-source-v1.json";
export const CONTRACT_CLOSURE_PROFILE_OUTPUT_PATH =
  "contracts/generated/platform/v1alpha1/contract-closure-profile-v1.json";

const SOURCE_SCHEMA_PATH =
  "contracts/platform/v1alpha1/schemas/contract-closure-profile-source-v1.schema.json";
const OUTPUT_SCHEMA_PATH =
  "contracts/platform/v1alpha1/schemas/contract-closure-profile-v1.schema.json";
const SOURCE_SCHEMA_ID =
  "https://schemas.cloud-agents.dev/platform/v1alpha1/schemas/contract-closure-profile-source-v1.schema.json";
const OUTPUT_SCHEMA_ID =
  "https://schemas.cloud-agents.dev/platform/v1alpha1/schemas/contract-closure-profile-v1.schema.json";
const SOURCE_FORMAT = "cloud-agents-contract-closure-profile-source/v1";
const OUTPUT_FORMAT = "cloud-agents-contract-closure-profile-registry/v1";
const REGISTRY_ID = "cloud-agents/platform/contract-closure-profile";
const PROFILE_ID = "contract-closure-profile/v1";
const SOURCE_DOMAIN = "cloud-agents/contract-closure-profile/source/v1";
const PROFILE_DOMAIN = "cloud-agents/contract-closure-profile/profile/v1";
const REGISTRY_DOMAIN = "cloud-agents/contract-closure-profile/registry/v1";

export const CONTRACT_CLOSURE_CRITERIA = [
  "json-schema-2020-12-official-test-suite",
  "openapi-3.1-semantic-validation",
  "generated-sdk-replay",
  "n-minus-one-compatibility",
  "response-watch-unknown-field-preservation",
  "runtime-server-path-and-tenant-authority-enforcement",
  "remaining-generator-supply-chain-review",
] as const;

export const CONTRACT_CLOSURE_SATISFIED_CANDIDATES = [
  "openapi-3.1-semantic-validation",
  "generated-sdk-replay",
  "n-minus-one-compatibility",
  "response-watch-unknown-field-preservation",
] as const;

export const CONTRACT_CLOSURE_MISSING = [
  "json-schema-2020-12-official-test-suite",
  "runtime-server-path-and-tenant-authority-enforcement",
  "remaining-generator-supply-chain-review",
] as const;

type CriterionStatus = "SATISFIED_CANDIDATE" | "PARTIAL" | "MISSING";
type ClosureCriterion = JsonRecord & {
  readonly id: string;
  readonly status: CriterionStatus;
  readonly authorityPaths: readonly string[];
  readonly evidencePaths: readonly string[];
  readonly review?: JsonRecord & {
    readonly path: string;
    readonly sha256: string;
    readonly verdict: string;
  };
  readonly reason?: string;
};
type ClosureProfile = JsonRecord & {
  readonly profileId: string;
  readonly status: string;
  readonly notGateClosure: boolean;
  readonly criteria: readonly ClosureCriterion[];
};
type RegistrySource = JsonRecord & {
  readonly formatVersion: string;
  readonly registryId: string;
  readonly profile: ClosureProfile;
};

export class ContractClosureProfileError extends Error {
  constructor(
    readonly code:
      | "CONTRACT_CLOSURE_BINDING_MISMATCH"
      | "CONTRACT_CLOSURE_EVIDENCE_MISMATCH"
      | "CONTRACT_CLOSURE_REGISTRY_DIGEST_MISMATCH",
    readonly path: string,
    message: string,
  ) {
    super(message);
    this.name = "ContractClosureProfileError";
  }
}

export function buildContractClosureProfileRegistry(root: string): JsonRecord {
  const source = readSource(root);
  validateContractClosureProfileSource(root, source);
  const missing = deriveContractClosureMissing(source.profile);
  const sourceDigest = domainDigest(SOURCE_DOMAIN, source);
  const profileDigest = domainDigest(PROFILE_DOMAIN, source.profile);
  const body: JsonRecord = {
    formatVersion: OUTPUT_FORMAT,
    registryId: REGISTRY_ID,
    sourceDigest,
    profile: { profileDigest, spec: source.profile },
    missing,
  };
  const generated = { ...body, registryDigest: domainDigest(REGISTRY_DOMAIN, body) };
  validateAgainstSchema(root, OUTPUT_SCHEMA_ID, generated);
  return generated;
}

export function serializeContractClosureProfileRegistry(registry: JsonRecord): string {
  return `${JSON.stringify(registry, null, 2)}\n`;
}

export function writeContractClosureProfileRegistry(root: string): void {
  const output = resolveContainedPath(root, CONTRACT_CLOSURE_PROFILE_OUTPUT_PATH, "file", true);
  writeFileSync(
    output,
    serializeContractClosureProfileRegistry(buildContractClosureProfileRegistry(root)),
  );
}

export function assertContractClosureProfileRegistryCurrent(root: string): void {
  const expected = serializeContractClosureProfileRegistry(
    buildContractClosureProfileRegistry(root),
  );
  const output = resolveContainedPath(root, CONTRACT_CLOSURE_PROFILE_OUTPUT_PATH, "file");
  const actual = readFileSync(output, "utf8");
  if (actual !== expected) {
    throw closureError(
      "CONTRACT_CLOSURE_REGISTRY_DIGEST_MISMATCH",
      "/registryDigest",
      `${CONTRACT_CLOSURE_PROFILE_OUTPUT_PATH} is stale; run its generator with --write.`,
    );
  }
}

export function contractClosureProfileInputs(root: string): string[] {
  const source = readSource(root);
  validateContractClosureProfileSource(root, source);
  const declaredPaths = source.profile.criteria.flatMap((criterion) => [
    ...criterion.authorityPaths,
    ...criterion.evidencePaths,
    ...(criterion.review ? [criterion.review.path] : []),
  ]);
  return [
    CONTRACT_CLOSURE_PROFILE_SOURCE_PATH,
    SOURCE_SCHEMA_PATH,
    OUTPUT_SCHEMA_PATH,
    "docs/plan/p1/g-contract-r5-formal-closure-profile-implementation-20260824.md",
    "scripts/generate-platform-contract-closure-profile.ts",
    "scripts/lib/platform-contract-closure-profile.test.ts",
    "scripts/lib/platform-contract-closure-profile.ts",
    "scripts/lib/platform-json-semantics.ts",
    ...declaredPaths.flatMap((path) => expandInputPath(root, path)),
  ]
    .filter((path, index, paths) => paths.indexOf(path) === index)
    .toSorted();
}

export function deriveContractClosureMissing(profile: ClosureProfile): string[] {
  return profile.criteria
    .filter((criterion) => criterion.status !== "SATISFIED_CANDIDATE")
    .map((criterion) => criterion.id);
}

export function validateContractClosureProfileSource(root: string, source: RegistrySource): void {
  validateAgainstSchema(root, SOURCE_SCHEMA_ID, source);
  if (
    source.formatVersion !== SOURCE_FORMAT ||
    source.registryId !== REGISTRY_ID ||
    source.profile.profileId !== PROFILE_ID ||
    source.profile.status !== "BOOTSTRAP_VALIDATED" ||
    source.profile.notGateClosure !== true
  ) {
    throw closureError(
      "CONTRACT_CLOSURE_BINDING_MISMATCH",
      "/profile",
      "Contract closure profile identity or non-Gate status drifted.",
    );
  }

  const actualIds = source.profile.criteria.map((criterion) => criterion.id);
  if (!canonicalEqual(actualIds, CONTRACT_CLOSURE_CRITERIA)) {
    throw closureError(
      "CONTRACT_CLOSURE_BINDING_MISMATCH",
      "/profile/criteria",
      "Contract closure v1 criteria must remain the exact ordered seven-item inventory.",
    );
  }
  const satisfied = source.profile.criteria
    .filter((criterion) => criterion.status === "SATISFIED_CANDIDATE")
    .map((criterion) => criterion.id);
  if (!canonicalEqual(satisfied, CONTRACT_CLOSURE_SATISFIED_CANDIDATES)) {
    throw closureError(
      "CONTRACT_CLOSURE_BINDING_MISMATCH",
      "/profile/criteria",
      "Contract closure v1 may satisfy only the approved four candidate criteria.",
    );
  }
  const missing = deriveContractClosureMissing(source.profile);
  if (!canonicalEqual(missing, CONTRACT_CLOSURE_MISSING)) {
    throw closureError(
      "CONTRACT_CLOSURE_BINDING_MISMATCH",
      "/profile/criteria",
      "Contract closure v1 must retain the exact three formal missing criteria.",
    );
  }

  for (const [index, criterion] of source.profile.criteria.entries()) {
    for (const path of [...criterion.authorityPaths, ...criterion.evidencePaths]) {
      resolveContainedPath(root, path, "file-or-directory");
    }
    if (criterion.status === "SATISFIED_CANDIDATE") {
      const review = criterion.review;
      if (!review) {
        throw closureError(
          "CONTRACT_CLOSURE_EVIDENCE_MISMATCH",
          `/profile/criteria/${index}/review`,
          `Satisfied criterion ${criterion.id} has no independent review binding.`,
        );
      }
      const reviewPath = resolveContainedPath(root, review.path, "file");
      const actualDigest = `sha256:${createHash("sha256").update(readFileSync(reviewPath)).digest("hex")}`;
      if (review.sha256 !== actualDigest || review.verdict !== "APPROVE_P0_0_P1_0_P2_0") {
        throw closureError(
          "CONTRACT_CLOSURE_EVIDENCE_MISMATCH",
          `/profile/criteria/${index}/review`,
          `Satisfied criterion ${criterion.id} review identity or verdict drifted.`,
        );
      }
    }
  }
}

function readSource(root: string): RegistrySource {
  const path = resolveContainedPath(root, CONTRACT_CLOSURE_PROFILE_SOURCE_PATH, "file");
  const parsed: unknown = JSON.parse(readFileSync(path, "utf8"));
  if (!isRecord(parsed)) throw new Error(`Expected JSON object: ${path}.`);
  canonicalizeJson(parsed);
  return parsed as RegistrySource;
}

function validateAgainstSchema(root: string, schemaId: string, value: unknown): void {
  const ajv = new Ajv2020({ allErrors: true, strict: true, validateFormats: true });
  ajv.addKeyword({ keyword: "x-cloud-agents-security", schemaType: "object" });
  for (const path of [SOURCE_SCHEMA_PATH, OUTPUT_SCHEMA_PATH]) {
    const schemaPath = resolveContainedPath(root, path, "file");
    ajv.addSchema(JSON.parse(readFileSync(schemaPath, "utf8")));
  }
  const validate = ajv.getSchema(schemaId);
  if (!validate) throw new Error(`Contract closure schema ${schemaId} was not registered.`);
  if (!validate(value)) {
    throw closureError(
      "CONTRACT_CLOSURE_BINDING_MISMATCH",
      "/",
      `Contract closure schema validation failed: ${ajv.errorsText(validate.errors)}.`,
    );
  }
}

function expandInputPath(root: string, repositoryRelativePath: string): string[] {
  const absolute = resolveContainedPath(root, repositoryRelativePath, "file-or-directory");
  const stat = lstatSync(absolute);
  if (stat.isFile()) return [repositoryRelativePath];
  const entries: string[] = [];
  for (const name of readdirSync(absolute).toSorted()) {
    const childRelative = `${repositoryRelativePath}/${name}`;
    entries.push(...expandInputPath(root, childRelative));
  }
  return entries;
}

function resolveContainedPath(
  root: string,
  repositoryRelativePath: string,
  kind: "file" | "file-or-directory",
  allowMissingFile = false,
): string {
  const rootAbsolute = resolve(root);
  const rootReal = realpathSync(rootAbsolute);
  const candidate = resolve(rootAbsolute, repositoryRelativePath);
  const lexical = relative(rootAbsolute, candidate);
  if (lexical === "" || lexical === ".." || lexical.startsWith(`..${sep}`) || isAbsolute(lexical)) {
    throw new Error(`Contract closure path escapes repository root: ${repositoryRelativePath}.`);
  }
  const components = lexical.split(sep);
  let current = rootReal;
  for (const [index, component] of components.entries()) {
    current = resolve(current, component);
    const final = index === components.length - 1;
    let stat: ReturnType<typeof lstatSync>;
    try {
      stat = lstatSync(current);
    } catch (error) {
      if (
        final &&
        allowMissingFile &&
        error instanceof Error &&
        "code" in error &&
        error.code === "ENOENT"
      ) {
        return current;
      }
      throw error;
    }
    if (stat.isSymbolicLink()) {
      throw new Error(`Contract closure path contains a symbolic link: ${repositoryRelativePath}.`);
    }
    if (!final && !stat.isDirectory()) {
      throw new Error(
        `Contract closure path has a non-directory parent: ${repositoryRelativePath}.`,
      );
    }
    if (final && kind === "file" && !stat.isFile()) {
      throw new Error(`Expected a regular contract closure file: ${repositoryRelativePath}.`);
    }
    if (final && kind === "file-or-directory" && !stat.isFile() && !stat.isDirectory()) {
      throw new Error(`Expected a regular contract closure input: ${repositoryRelativePath}.`);
    }
  }
  const resolved = realpathSync(current);
  const contained = relative(rootReal, resolved);
  if (contained === ".." || contained.startsWith(`..${sep}`) || isAbsolute(contained)) {
    throw new Error(
      `Contract closure realpath escapes repository root: ${repositoryRelativePath}.`,
    );
  }
  return current;
}

function domainDigest(domain: string, value: unknown): string {
  const hash = createHash("sha256");
  hash.update(domain, "utf8");
  hash.update(Uint8Array.of(0));
  hash.update(canonicalizeJson(value));
  return `sha256:${hash.digest("hex")}`;
}

function canonicalEqual(left: unknown, right: unknown): boolean {
  const leftBytes = canonicalizeJson(left);
  const rightBytes = canonicalizeJson(right);
  return (
    leftBytes.byteLength === rightBytes.byteLength &&
    leftBytes.every((byte, index) => byte === rightBytes[index])
  );
}

function closureError(
  code: ContractClosureProfileError["code"],
  path: string,
  message: string,
): ContractClosureProfileError {
  return new ContractClosureProfileError(code, path, message);
}

function isRecord(value: unknown): value is JsonRecord {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}
