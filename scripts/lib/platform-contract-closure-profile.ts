import { createHash } from "node:crypto";
import { lstatSync, readFileSync, readdirSync, realpathSync, writeFileSync } from "node:fs";
import { isAbsolute, relative, resolve, sep } from "node:path";

import Ajv2020 from "ajv/dist/2020.js";

import { canonicalizeJson, type JsonRecord } from "./platform-json-semantics";

export const CONTRACT_CLOSURE_PROFILE_V1_SOURCE_PATH =
  "contracts/platform/v1alpha1/fixtures/golden/contract-closure-profile-source-v1.json";
export const CONTRACT_CLOSURE_PROFILE_V1_OUTPUT_PATH =
  "contracts/generated/platform/v1alpha1/contract-closure-profile-v1.json";
export const CONTRACT_CLOSURE_PROFILE_V2_SOURCE_PATH =
  "contracts/platform/v1alpha1/fixtures/golden/contract-closure-profile-source-v2.json";
export const CONTRACT_CLOSURE_PROFILE_V2_OUTPUT_PATH =
  "contracts/generated/platform/v1alpha1/contract-closure-profile-v2.json";
export const CONTRACT_CLOSURE_PROFILE_SOURCE_PATH = CONTRACT_CLOSURE_PROFILE_V2_SOURCE_PATH;
export const CONTRACT_CLOSURE_PROFILE_OUTPUT_PATH = CONTRACT_CLOSURE_PROFILE_V2_OUTPUT_PATH;

const V1_SOURCE_SCHEMA_PATH =
  "contracts/platform/v1alpha1/schemas/contract-closure-profile-source-v1.schema.json";
const V1_OUTPUT_SCHEMA_PATH =
  "contracts/platform/v1alpha1/schemas/contract-closure-profile-v1.schema.json";
const V2_SOURCE_SCHEMA_PATH =
  "contracts/platform/v1alpha1/schemas/contract-closure-profile-source-v2.schema.json";
const V2_OUTPUT_SCHEMA_PATH =
  "contracts/platform/v1alpha1/schemas/contract-closure-profile-v2.schema.json";
const V1_SOURCE_SCHEMA_ID =
  "https://schemas.cloud-agents.dev/platform/v1alpha1/schemas/contract-closure-profile-source-v1.schema.json";
const V1_OUTPUT_SCHEMA_ID =
  "https://schemas.cloud-agents.dev/platform/v1alpha1/schemas/contract-closure-profile-v1.schema.json";
const V2_SOURCE_SCHEMA_ID =
  "https://schemas.cloud-agents.dev/platform/v1alpha1/schemas/contract-closure-profile-source-v2.schema.json";
const V2_OUTPUT_SCHEMA_ID =
  "https://schemas.cloud-agents.dev/platform/v1alpha1/schemas/contract-closure-profile-v2.schema.json";
const REGISTRY_ID = "cloud-agents/platform/contract-closure-profile";

const V1_IMMUTABLE_FILES = {
  [CONTRACT_CLOSURE_PROFILE_V1_SOURCE_PATH]:
    "411e4b649c5b812339817b5836c25a6a2f27c9aa0e24497b7aa65da8fe2baa49",
  [V1_SOURCE_SCHEMA_PATH]: "8b87a0e24e42db87987a1dc1b4931b7ff2b8edef6bef6ccd184e0586a7bdc4af",
  [V1_OUTPUT_SCHEMA_PATH]: "107dbc21f240cd912f567ef1e0a6bfaf78d2e6171f0f4189ca9812c225630bc0",
  [CONTRACT_CLOSURE_PROFILE_V1_OUTPUT_PATH]:
    "823e9356342511b611538fb669e8af99962555b153324d09c7208f3f00b51e68",
} as const;

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
  "json-schema-2020-12-official-test-suite",
  "openapi-3.1-semantic-validation",
  "generated-sdk-replay",
  "n-minus-one-compatibility",
  "response-watch-unknown-field-preservation",
] as const;

export const CONTRACT_CLOSURE_MISSING = [
  "runtime-server-path-and-tenant-authority-enforcement",
  "remaining-generator-supply-chain-review",
] as const;

export const CONTRACT_CLOSURE_V1_SATISFIED_CANDIDATES = [
  "openapi-3.1-semantic-validation",
  "generated-sdk-replay",
  "n-minus-one-compatibility",
  "response-watch-unknown-field-preservation",
] as const;
export const CONTRACT_CLOSURE_V1_MISSING = [
  "json-schema-2020-12-official-test-suite",
  ...CONTRACT_CLOSURE_MISSING,
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

type ProfileVersion = "v1" | "v2";

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

export function assertContractClosureProfileV1Immutable(root: string): void {
  for (const [path, expected] of Object.entries(V1_IMMUTABLE_FILES)) {
    const actual = createHash("sha256")
      .update(readFileSync(resolveContainedPath(root, path, "file")))
      .digest("hex");
    if (actual !== expected) {
      throw closureError(
        "CONTRACT_CLOSURE_EVIDENCE_MISMATCH",
        `/predecessor/${path}`,
        `Contract closure v1 immutable file ${path} drifted.`,
      );
    }
  }
}

export function buildContractClosureProfileV1Registry(root: string): JsonRecord {
  return buildRegistry(root, "v1");
}

export function buildContractClosureProfileV2Registry(root: string): JsonRecord {
  assertContractClosureProfileV1Immutable(root);
  return buildRegistry(root, "v2");
}

export function buildContractClosureProfileRegistry(root: string): JsonRecord {
  return buildContractClosureProfileV2Registry(root);
}

function buildRegistry(root: string, version: ProfileVersion): JsonRecord {
  const source = readSource(root, version);
  validateContractClosureProfileSourceVersion(root, source, version);
  const missing = deriveContractClosureMissing(source.profile);
  const sourceDigest = domainDigest(
    `cloud-agents/contract-closure-profile/source/${version}`,
    source,
  );
  const profileDigest = domainDigest(
    `cloud-agents/contract-closure-profile/profile/${version}`,
    source.profile,
  );
  const body: JsonRecord = {
    formatVersion: `cloud-agents-contract-closure-profile-registry/${version}`,
    registryId: REGISTRY_ID,
    sourceDigest,
    ...(version === "v2"
      ? {
          predecessor: source.predecessor,
          officialSuiteEvidence: source.officialSuiteEvidence,
        }
      : {}),
    profile: { profileDigest, spec: source.profile },
    missing,
  };
  const generated = {
    ...body,
    registryDigest: domainDigest(`cloud-agents/contract-closure-profile/registry/${version}`, body),
  };
  validateAgainstSchema(
    root,
    version === "v1" ? V1_OUTPUT_SCHEMA_ID : V2_OUTPUT_SCHEMA_ID,
    generated,
  );
  return generated;
}

export function serializeContractClosureProfileRegistry(registry: JsonRecord): string {
  return `${JSON.stringify(registry, null, 2)}\n`;
}

export function writeContractClosureProfileRegistry(root: string): void {
  assertContractClosureProfileV1Immutable(root);
  const output = resolveContainedPath(root, CONTRACT_CLOSURE_PROFILE_OUTPUT_PATH, "file", true);
  writeFileSync(
    output,
    serializeContractClosureProfileRegistry(buildContractClosureProfileRegistry(root)),
  );
}

export function assertContractClosureProfileRegistryCurrent(root: string): void {
  assertContractClosureProfileV1RegistryCurrent(root);
  const output = resolveContainedPath(root, CONTRACT_CLOSURE_PROFILE_OUTPUT_PATH, "file");
  assertContractClosureProfileRegistryBytesCurrent(root, readFileSync(output, "utf8"));
}

export function assertContractClosureProfileRegistryBytesCurrent(
  root: string,
  actual: string,
): void {
  const expected = serializeContractClosureProfileRegistry(
    buildContractClosureProfileRegistry(root),
  );
  if (actual !== expected) {
    throw closureError(
      "CONTRACT_CLOSURE_REGISTRY_DIGEST_MISMATCH",
      "/registryDigest",
      `${CONTRACT_CLOSURE_PROFILE_OUTPUT_PATH} is stale; run its generator with --write.`,
    );
  }
}

export function assertContractClosureProfileV1RegistryCurrent(root: string): void {
  assertContractClosureProfileV1Immutable(root);
  const expected = serializeContractClosureProfileRegistry(
    buildContractClosureProfileV1Registry(root),
  );
  const actual = readFileSync(
    resolveContainedPath(root, CONTRACT_CLOSURE_PROFILE_V1_OUTPUT_PATH, "file"),
    "utf8",
  );
  if (actual !== expected) {
    throw closureError(
      "CONTRACT_CLOSURE_REGISTRY_DIGEST_MISMATCH",
      "/registryDigest",
      `${CONTRACT_CLOSURE_PROFILE_V1_OUTPUT_PATH} is stale or mutated.`,
    );
  }
}

export function contractClosureProfileInputs(root: string): string[] {
  const source = readSource(root, "v2");
  validateContractClosureProfileSource(root, source);
  const declaredPaths = source.profile.criteria.flatMap((criterion) => [
    ...criterion.authorityPaths,
    ...criterion.evidencePaths,
    ...(criterion.review ? [criterion.review.path] : []),
  ]);
  return [
    ...Object.keys(V1_IMMUTABLE_FILES),
    CONTRACT_CLOSURE_PROFILE_SOURCE_PATH,
    V2_SOURCE_SCHEMA_PATH,
    V2_OUTPUT_SCHEMA_PATH,
    "docs/plan/p1/g-contract-r5-formal-closure-profile-implementation-20260824.md",
    "docs/plan/p1/g-contract-r5-b2-official-suite-evidence-closure-20260824.md",
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
  assertContractClosureProfileV1Immutable(root);
  validateContractClosureProfileSourceVersion(root, source, "v2");
}

export function validateContractClosureProfileV1Source(root: string, source: RegistrySource): void {
  validateContractClosureProfileSourceVersion(root, source, "v1");
}

function validateContractClosureProfileSourceVersion(
  root: string,
  source: RegistrySource,
  version: ProfileVersion,
): void {
  validateAgainstSchema(root, version === "v1" ? V1_SOURCE_SCHEMA_ID : V2_SOURCE_SCHEMA_ID, source);
  if (
    source.formatVersion !== `cloud-agents-contract-closure-profile-source/${version}` ||
    source.registryId !== REGISTRY_ID ||
    source.profile.profileId !== `contract-closure-profile/${version}` ||
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
  const expectedSatisfied =
    version === "v1"
      ? CONTRACT_CLOSURE_V1_SATISFIED_CANDIDATES
      : CONTRACT_CLOSURE_SATISFIED_CANDIDATES;
  if (!canonicalEqual(satisfied, expectedSatisfied)) {
    throw closureError(
      "CONTRACT_CLOSURE_BINDING_MISMATCH",
      "/profile/criteria",
      `Contract closure ${version} has an unapproved satisfied-candidate inventory.`,
    );
  }
  const missing = deriveContractClosureMissing(source.profile);
  const expectedMissing = version === "v1" ? CONTRACT_CLOSURE_V1_MISSING : CONTRACT_CLOSURE_MISSING;
  if (!canonicalEqual(missing, expectedMissing)) {
    throw closureError(
      "CONTRACT_CLOSURE_BINDING_MISMATCH",
      "/profile/criteria",
      `Contract closure ${version} must retain its exact formal missing criteria.`,
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

  if (version === "v2") validateV2Evidence(root, source);
}

function validateV2Evidence(root: string, source: RegistrySource): void {
  const predecessor = source.predecessor;
  const evidence = source.officialSuiteEvidence;
  if (!isRecord(predecessor) || !isRecord(evidence)) {
    throw closureError(
      "CONTRACT_CLOSURE_EVIDENCE_MISMATCH",
      "/predecessor",
      "Contract closure v2 predecessor and official-suite evidence are required.",
    );
  }
  const expectedPredecessor: JsonRecord = {
    profileId: "contract-closure-profile/v1",
    predecessorMutation: "forbidden",
    sourceSha256: `sha256:${V1_IMMUTABLE_FILES[CONTRACT_CLOSURE_PROFILE_V1_SOURCE_PATH]}`,
    sourceSchemaSha256: `sha256:${V1_IMMUTABLE_FILES[V1_SOURCE_SCHEMA_PATH]}`,
    outputSchemaSha256: `sha256:${V1_IMMUTABLE_FILES[V1_OUTPUT_SCHEMA_PATH]}`,
    generatedRegistrySha256: `sha256:${V1_IMMUTABLE_FILES[CONTRACT_CLOSURE_PROFILE_V1_OUTPUT_PATH]}`,
  };
  if (!canonicalEqual(predecessor, expectedPredecessor)) {
    throw closureError(
      "CONTRACT_CLOSURE_EVIDENCE_MISMATCH",
      "/predecessor",
      "Contract closure v2 predecessor lineage or immutable digest drifted.",
    );
  }

  const expectedEvidence: JsonRecord = {
    criterionMeaning: "fixed_corpus_and_authority_evidence_complete",
    independentOracle: {
      validator: "jsonschema-rs 0.50.1",
      files: 46,
      cases: 383,
      assertions: 1299,
      passedAssertions: 1299,
      remotes: 79,
      failures: 0,
    },
    productionAjvAudit: {
      validator: "Ajv 8.20.0",
      status: "EXECUTED_NONCONFORMANT",
      conformanceClaim: false,
      assertions: 1299,
      passedAssertions: 1241,
      nonPassingAssertions: 58,
      normalReplay: "exact",
      requireConformance: "nonzero",
    },
    currentContractParity: {
      schemaFiles: 58,
      fixtureManifests: 2,
      fixtureCases: 77,
      productionValidator: "Ajv 8.20.0 plus in-repo semantics",
      independentValidator: "jsonschema-rs 0.50.1",
      crossEngineExactFixtureResults: true,
    },
  };
  if (!canonicalEqual(evidence, expectedEvidence)) {
    throw closureError(
      "CONTRACT_CLOSURE_EVIDENCE_MISMATCH",
      "/officialSuiteEvidence",
      "Contract closure v2 official-suite oracle, Ajv non-claim, or parity facts drifted.",
    );
  }

  const auditPath = resolveContainedPath(
    root,
    "contracts/generated/platform/v1alpha1/ajv-official-suite-audit-v1.json",
    "file",
  );
  const audit: unknown = JSON.parse(readFileSync(auditPath, "utf8"));
  if (
    !isRecord(audit) ||
    audit.status !== "EXECUTED_NONCONFORMANT" ||
    audit.conformanceClaim !== false ||
    !isRecord(audit.summary) ||
    audit.summary.assertions !== 1299 ||
    audit.summary.passedAssertions !== 1241 ||
    audit.summary.nonPassingAssertions !== 58
  ) {
    throw closureError(
      "CONTRACT_CLOSURE_EVIDENCE_MISMATCH",
      "/officialSuiteEvidence/productionAjvAudit",
      "Checked-in Ajv audit no longer proves the exact nonconformant/no-claim result.",
    );
  }

  const v1 = readSource(root, "v1");
  for (let index = 1; index <= 4; index += 1) {
    if (!canonicalEqual(source.profile.criteria[index], v1.profile.criteria[index])) {
      throw closureError(
        "CONTRACT_CLOSURE_EVIDENCE_MISMATCH",
        `/profile/criteria/${index}`,
        "Contract closure v2 must retain each prior satisfied criterion and exact review binding.",
      );
    }
  }

  const official = source.profile.criteria[0];
  const expectedAuthority = [
    "docs/plan/adr/0026-p1-json-schema-official-suite-evidence-closure.md",
    "tools/contract-standards/profile.json",
    "tools/contract-standards/vendor/json-schema-test-suite",
  ];
  const expectedEvidencePaths = [
    "contracts/platform/v1alpha1/fixtures/golden/ajv-official-suite-audit-source-v1.json",
    "contracts/platform/v1alpha1/schemas/ajv-official-suite-audit-source-v1.schema.json",
    "contracts/platform/v1alpha1/schemas/ajv-official-suite-audit-v1.schema.json",
    "contracts/generated/platform/v1alpha1/ajv-official-suite-audit-v1.json",
    "scripts/check-platform-ajv-official-suite.ts",
    "scripts/lib/platform-ajv-official-suite.ts",
    "scripts/lib/platform-ajv-official-suite.test.ts",
    "docs/plan/p1/g-contract-r5-b1-ajv-official-suite-audit-20260824.md",
    "tools/contract-standards/check_contract_standards.py",
    "tools/contract-standards/test_contract_standards.py",
  ];
  if (
    official === undefined ||
    !canonicalEqual(official.authorityPaths, expectedAuthority) ||
    !canonicalEqual(official.evidencePaths, expectedEvidencePaths)
  ) {
    throw closureError(
      "CONTRACT_CLOSURE_EVIDENCE_MISMATCH",
      "/profile/criteria/0",
      "Official-suite closure criterion lost a required authority or evidence binding.",
    );
  }
}

function readSource(root: string, version: ProfileVersion): RegistrySource {
  const path = resolveContainedPath(
    root,
    version === "v1"
      ? CONTRACT_CLOSURE_PROFILE_V1_SOURCE_PATH
      : CONTRACT_CLOSURE_PROFILE_V2_SOURCE_PATH,
    "file",
  );
  const parsed: unknown = JSON.parse(readFileSync(path, "utf8"));
  if (!isRecord(parsed)) throw new Error(`Expected JSON object: ${path}.`);
  canonicalizeJson(parsed);
  return parsed as RegistrySource;
}

function validateAgainstSchema(root: string, schemaId: string, value: unknown): void {
  const ajv = new Ajv2020({ allErrors: true, strict: true, validateFormats: true });
  ajv.addKeyword({ keyword: "x-cloud-agents-security", schemaType: "object" });
  for (const path of [
    V1_SOURCE_SCHEMA_PATH,
    V1_OUTPUT_SCHEMA_PATH,
    V2_SOURCE_SCHEMA_PATH,
    V2_OUTPUT_SCHEMA_PATH,
  ]) {
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
