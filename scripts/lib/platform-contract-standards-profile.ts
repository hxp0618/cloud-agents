import { createHash } from "node:crypto";
import {
  closeSync,
  constants,
  fstatSync,
  lstatSync,
  openSync,
  readFileSync,
  readdirSync,
  realpathSync,
} from "node:fs";
import { isAbsolute, relative, resolve, sep } from "node:path";

export const CONTRACT_STANDARDS_PROFILE_V1_PATH = "tools/contract-standards/profile.json";
export const CONTRACT_STANDARDS_PROFILE_V2_PATH = "tools/contract-standards/profile-v2.json";
export const CONTRACT_STANDARDS_CORPUS_PATH =
  "tools/contract-standards/vendor/json-schema-test-suite";

export const CONTRACT_STANDARDS_PROFILE_V1_IMMUTABLE = {
  path: CONTRACT_STANDARDS_PROFILE_V1_PATH,
  sha256: "dfb79ae54631d9f61f53846c91ac74bebb5b213fac023af2527c3ce352873a11",
  sizeBytes: 3_218,
} as const;

const FORMAT_V1 = "cloud-agents-contract-standards-profile/v1";
const FORMAT_V2 = "cloud-agents-contract-standards-profile/v2";
const CORPUS_ALGORITHM = "sorted-path-nul-sha256-nul-size-v1";
const CURRENT_SOURCE_CONTRACT_MANIFEST_SHA256 =
  "sha256:97ccd739db755b1fbfaf9166f87c4cd985980d6ec78a1b172bbd65638006413c";
const EXPECTED_OPENAPI_DOCUMENTS = [
  "contracts/managed-agent/v1alpha1/openapi.json",
  "contracts/managed-host/v1alpha1/openapi.json",
] as const;
const EXPECTED_BOUNDARY = {
  productionRuntimeDependency: "FORBIDDEN",
  productionDatabaseWrites: "NOT_AUTHORIZED",
  httpSurface: "NOT_IMPLEMENTED",
  p2Surface: "NOT_IMPLEMENTED",
  providerSideEffects: "FORBIDDEN",
  deployment: "NOT_AUTHORIZED",
  publication: "NOT_AUTHORIZED",
  gateStatus: "ALL_GATES_OPEN",
  independentReview: "PENDING",
} as const;

type JsonObject = Record<string, unknown>;

export type ContractStandardsProfile = {
  readonly formatVersion: string;
  readonly status: string;
  readonly notGateClosure: boolean;
  readonly predecessor?: {
    readonly path: string;
    readonly sha256: string;
    readonly sizeBytes: number;
    readonly mutation: string;
  };
  readonly toolchain: {
    readonly bun: string;
    readonly python: string;
    readonly uv: string;
    readonly pyproject: { readonly path: string; readonly sha256: string };
    readonly lock: { readonly path: string; readonly sha256: string };
  };
  readonly packages: Record<string, string>;
  readonly jsonSchemaOfficialSuite: {
    readonly dialect: string;
    readonly upstream: string;
    readonly commit: string;
    readonly tree: string;
    readonly mandatoryTree: string;
    readonly localRoot: string;
    readonly corpusManifestAlgorithm: string;
    readonly corpusManifestSha256: string;
    readonly corpusFiles: number;
    readonly license: string;
    readonly licenseSha256: string;
    readonly mandatoryFiles: number;
    readonly cases: number;
    readonly assertions: number;
    readonly remoteFiles: number;
    readonly independentValidator: string;
    readonly expectedFailures: number;
    readonly productionAjvOfficialSuiteAudit: {
      readonly validator: string;
      readonly status: string;
    };
  };
  readonly currentContracts: {
    readonly schemaFiles: number;
    readonly fixtureManifests: number;
    readonly fixtureCases: number;
    readonly sourceContractManifestSha256?: string;
    readonly productionValidator: string;
    readonly independentValidator: string;
    readonly crossEngineExactFixtureResults: boolean;
  };
  readonly openapi: {
    readonly documentVersion: string;
    readonly documents: readonly string[];
    readonly documentCount: number;
    readonly operationCount: number;
    readonly independentValidator: string;
    readonly expectedFailures: number;
  };
  readonly implementationBoundary: Record<string, string>;
};

export class ContractStandardsProfileError extends Error {
  constructor(
    readonly code:
      | "CONTRACT_STANDARDS_PROFILE_INVALID"
      | "CONTRACT_STANDARDS_PREDECESSOR_MISMATCH"
      | "CONTRACT_STANDARDS_PATH_INVALID"
      | "CONTRACT_STANDARDS_BINDING_MISMATCH",
    readonly path: string,
    message: string,
  ) {
    super(message);
    this.name = "ContractStandardsProfileError";
  }
}

export function assertContractStandardsProfileV1Immutable(root: string): void {
  const bytes = readContainedRegularFile(root, CONTRACT_STANDARDS_PROFILE_V1_PATH);
  const digest = sha256(bytes);
  if (
    bytes.byteLength !== CONTRACT_STANDARDS_PROFILE_V1_IMMUTABLE.sizeBytes ||
    digest !== CONTRACT_STANDARDS_PROFILE_V1_IMMUTABLE.sha256
  ) {
    throw profileError(
      "CONTRACT_STANDARDS_PREDECESSOR_MISMATCH",
      `/${CONTRACT_STANDARDS_PROFILE_V1_PATH}`,
      "The contract-standards v1 predecessor bytes are not immutable.",
    );
  }
}

export function readContractStandardsProfile(root: string, path: string): ContractStandardsProfile {
  let parsed: unknown;
  try {
    parsed = JSON.parse(readContainedRegularFile(root, path).toString("utf8"));
  } catch (error) {
    if (error instanceof ContractStandardsProfileError) throw error;
    throw profileError(
      "CONTRACT_STANDARDS_PROFILE_INVALID",
      `/${path}`,
      `Contract-standards profile JSON is invalid: ${String(error)}.`,
    );
  }
  validateContractStandardsProfile(parsed);
  return parsed as ContractStandardsProfile;
}

export function validateContractStandardsProfile(
  value: unknown,
): asserts value is ContractStandardsProfile {
  const profile = object(value, "/");
  const format = string(profile.formatVersion, "/formatVersion");
  const v2 = format === FORMAT_V2;
  if (!v2 && format !== FORMAT_V1) {
    throw profileError(
      "CONTRACT_STANDARDS_PROFILE_INVALID",
      "/formatVersion",
      "Only contract-standards profile v1 and v2 are supported.",
    );
  }
  exactKeys(
    profile,
    [
      "formatVersion",
      "status",
      "notGateClosure",
      ...(v2 ? ["predecessor"] : []),
      "toolchain",
      "packages",
      "jsonSchemaOfficialSuite",
      "currentContracts",
      "openapi",
      "implementationBoundary",
    ],
    "/",
  );
  if (profile.status !== "GENERATED_NON_GATE_EVIDENCE" || profile.notGateClosure !== true) {
    throw profileError(
      "CONTRACT_STANDARDS_PROFILE_INVALID",
      "/status",
      "Contract-standards profiles must remain generated non-Gate evidence.",
    );
  }
  if (v2) validatePredecessor(profile.predecessor);
  validateToolchain(profile.toolchain);
  validatePackages(profile.packages);
  validateOfficialSuite(profile.jsonSchemaOfficialSuite);
  validateCurrentContracts(profile.currentContracts, v2);
  validateOpenApi(profile.openapi);
  exactObject(profile.implementationBoundary, EXPECTED_BOUNDARY, "/implementationBoundary");
}

export function assertContractStandardsProfileCurrent(root: string): ContractStandardsProfile {
  assertContractStandardsProfileV1Immutable(root);
  const profile = readContractStandardsProfile(root, CONTRACT_STANDARDS_PROFILE_V2_PATH);
  if (profile.formatVersion !== FORMAT_V2) {
    throw profileError(
      "CONTRACT_STANDARDS_PROFILE_INVALID",
      "/formatVersion",
      "The current contract-standards authority must explicitly select v2.",
    );
  }
  assertFileBinding(root, profile.toolchain.pyproject, "/toolchain/pyproject");
  assertFileBinding(root, profile.toolchain.lock, "/toolchain/lock");
  assertCorpusCurrent(root, profile);
  assertCurrentContractCounts(root, profile);
  for (const path of profile.openapi.documents) readContainedRegularFile(root, path);
  assertContractStandardsProfileV1Immutable(root);
  return profile;
}

export function contractStandardsCorpusInputs(root: string): string[] {
  return listContainedRegularFiles(root, CONTRACT_STANDARDS_CORPUS_PATH).toSorted();
}

function validatePredecessor(value: unknown): void {
  const predecessor = object(value, "/predecessor");
  exactKeys(predecessor, ["path", "sha256", "sizeBytes", "mutation"], "/predecessor");
  if (
    predecessor.path !== CONTRACT_STANDARDS_PROFILE_V1_IMMUTABLE.path ||
    predecessor.sha256 !== CONTRACT_STANDARDS_PROFILE_V1_IMMUTABLE.sha256 ||
    predecessor.sizeBytes !== CONTRACT_STANDARDS_PROFILE_V1_IMMUTABLE.sizeBytes ||
    predecessor.mutation !== "forbidden"
  ) {
    throw profileError(
      "CONTRACT_STANDARDS_PREDECESSOR_MISMATCH",
      "/predecessor",
      "Contract-standards v2 must retain the exact v1 path, SHA-256, size, and mutation fence.",
    );
  }
}

function validateToolchain(value: unknown): void {
  const toolchain = object(value, "/toolchain");
  exactKeys(toolchain, ["bun", "python", "uv", "pyproject", "lock"], "/toolchain");
  if (toolchain.bun !== "1.3.14" || toolchain.python !== "3.14.7" || toolchain.uv !== "0.12.5") {
    throw profileError(
      "CONTRACT_STANDARDS_PROFILE_INVALID",
      "/toolchain",
      "Contract-standards toolchain versions drifted.",
    );
  }
  for (const [key, expectedPath] of [
    ["pyproject", "tools/contract-standards/pyproject.toml"],
    ["lock", "tools/contract-standards/uv.lock"],
  ] as const) {
    const binding = object(toolchain[key], `/toolchain/${key}`);
    exactKeys(binding, ["path", "sha256"], `/toolchain/${key}`);
    if (
      binding.path !== expectedPath ||
      !/^[0-9a-f]{64}$/u.test(string(binding.sha256, "sha256"))
    ) {
      throw profileError(
        "CONTRACT_STANDARDS_PROFILE_INVALID",
        `/toolchain/${key}`,
        `Contract-standards ${key} binding is invalid.`,
      );
    }
  }
}

function validatePackages(value: unknown): void {
  const packages = object(value, "/packages");
  if (
    packages["jsonschema-rs"] !== "0.50.1" ||
    packages["openapi-spec-validator"] !== "0.9.0" ||
    Object.entries(packages).some(
      ([name, version]) => name === "" || typeof version !== "string" || version === "",
    )
  ) {
    throw profileError(
      "CONTRACT_STANDARDS_PROFILE_INVALID",
      "/packages",
      "Contract-standards package authority is invalid.",
    );
  }
}

function validateOfficialSuite(value: unknown): void {
  const suite = object(value, "/jsonSchemaOfficialSuite");
  exactKeys(
    suite,
    [
      "dialect",
      "upstream",
      "commit",
      "tree",
      "mandatoryTree",
      "localRoot",
      "corpusManifestAlgorithm",
      "corpusManifestSha256",
      "corpusFiles",
      "license",
      "licenseSha256",
      "mandatoryFiles",
      "cases",
      "assertions",
      "remoteFiles",
      "independentValidator",
      "expectedFailures",
      "productionAjvOfficialSuiteAudit",
    ],
    "/jsonSchemaOfficialSuite",
  );
  const audit = object(
    suite.productionAjvOfficialSuiteAudit,
    "/jsonSchemaOfficialSuite/productionAjvOfficialSuiteAudit",
  );
  exactObject(
    audit,
    { validator: "Ajv 8.20.0", status: "EXECUTED_NONCONFORMANT" },
    "/jsonSchemaOfficialSuite/productionAjvOfficialSuiteAudit",
  );
  if (
    suite.localRoot !== CONTRACT_STANDARDS_CORPUS_PATH ||
    suite.corpusManifestAlgorithm !== CORPUS_ALGORITHM ||
    suite.corpusFiles !== 126 ||
    suite.mandatoryFiles !== 46 ||
    suite.cases !== 383 ||
    suite.assertions !== 1_299 ||
    suite.remoteFiles !== 79 ||
    suite.expectedFailures !== 0 ||
    !/^[0-9a-f]{64}$/u.test(string(suite.corpusManifestSha256, "corpus SHA-256")) ||
    !/^[0-9a-f]{64}$/u.test(string(suite.licenseSha256, "license SHA-256"))
  ) {
    throw profileError(
      "CONTRACT_STANDARDS_PROFILE_INVALID",
      "/jsonSchemaOfficialSuite",
      "Official JSON Schema suite authority drifted.",
    );
  }
}

function validateCurrentContracts(value: unknown, v2: boolean): void {
  const current = object(value, "/currentContracts");
  exactKeys(
    current,
    [
      "schemaFiles",
      "fixtureManifests",
      "fixtureCases",
      ...(v2 ? ["sourceContractManifestSha256"] : []),
      "productionValidator",
      "independentValidator",
      "crossEngineExactFixtureResults",
    ],
    "/currentContracts",
  );
  const expected = v2
    ? { schemaFiles: 60, fixtureManifests: 2, fixtureCases: 79 }
    : { schemaFiles: 58, fixtureManifests: 2, fixtureCases: 77 };
  if (
    current.schemaFiles !== expected.schemaFiles ||
    current.fixtureManifests !== expected.fixtureManifests ||
    current.fixtureCases !== expected.fixtureCases ||
    (v2 && current.sourceContractManifestSha256 !== CURRENT_SOURCE_CONTRACT_MANIFEST_SHA256) ||
    current.crossEngineExactFixtureResults !== true
  ) {
    throw profileError(
      "CONTRACT_STANDARDS_PROFILE_INVALID",
      "/currentContracts",
      "Contract-standards profile cardinalities or cross-engine boundary drifted.",
    );
  }
}

function validateOpenApi(value: unknown): void {
  const openapi = object(value, "/openapi");
  exactKeys(
    openapi,
    [
      "documentVersion",
      "documents",
      "documentCount",
      "operationCount",
      "independentValidator",
      "expectedFailures",
    ],
    "/openapi",
  );
  if (
    openapi.documentVersion !== "3.1.1" ||
    JSON.stringify(openapi.documents) !== JSON.stringify(EXPECTED_OPENAPI_DOCUMENTS) ||
    openapi.documentCount !== 2 ||
    openapi.operationCount !== 9 ||
    openapi.expectedFailures !== 0
  ) {
    throw profileError(
      "CONTRACT_STANDARDS_PROFILE_INVALID",
      "/openapi",
      "Contract-standards OpenAPI authority drifted.",
    );
  }
}

function assertFileBinding(
  root: string,
  binding: { readonly path: string; readonly sha256: string },
  label: string,
): void {
  const actual = sha256(readContainedRegularFile(root, binding.path));
  if (actual !== binding.sha256) {
    throw profileError(
      "CONTRACT_STANDARDS_BINDING_MISMATCH",
      label,
      `Contract-standards file binding drifted: ${binding.path}.`,
    );
  }
}

function assertCorpusCurrent(root: string, profile: ContractStandardsProfile): void {
  const files = contractStandardsCorpusInputs(root);
  const hash = createHash("sha256");
  const prefix = `${CONTRACT_STANDARDS_CORPUS_PATH}/`;
  for (const path of files) {
    const bytes = readContainedRegularFile(root, path);
    hash
      .update(path.slice(prefix.length))
      .update("\0")
      .update(sha256(bytes))
      .update("\0")
      .update(String(bytes.byteLength))
      .update("\0");
  }
  const suite = profile.jsonSchemaOfficialSuite;
  if (files.length !== suite.corpusFiles || hash.digest("hex") !== suite.corpusManifestSha256) {
    throw profileError(
      "CONTRACT_STANDARDS_BINDING_MISMATCH",
      "/jsonSchemaOfficialSuite",
      "Contract-standards corpus topology or manifest digest drifted.",
    );
  }
  const license = sha256(
    readContainedRegularFile(root, `${CONTRACT_STANDARDS_CORPUS_PATH}/LICENSE`),
  );
  if (license !== suite.licenseSha256) {
    throw profileError(
      "CONTRACT_STANDARDS_BINDING_MISMATCH",
      "/jsonSchemaOfficialSuite/licenseSha256",
      "Contract-standards corpus license digest drifted.",
    );
  }
}

function assertCurrentContractCounts(root: string, profile: ContractStandardsProfile): void {
  const contractFiles = listContainedRegularFiles(root, "contracts");
  const schemaFiles = contractFiles.filter((path) => path.endsWith(".schema.json"));
  const manifests = contractFiles.filter((path) => path.endsWith("/fixtures/manifest.json"));
  let fixtureCases = 0;
  for (const path of manifests) {
    const manifest = object(
      JSON.parse(readContainedRegularFile(root, path).toString("utf8")) as unknown,
      `/${path}`,
    );
    if (!Array.isArray(manifest.cases)) {
      throw profileError(
        "CONTRACT_STANDARDS_BINDING_MISMATCH",
        `/${path}/cases`,
        "Fixture manifest cases must be an array.",
      );
    }
    fixtureCases += manifest.cases.length;
  }
  const actual = {
    schemaFiles: schemaFiles.length,
    fixtureManifests: manifests.length,
    fixtureCases,
  };
  const expected = {
    schemaFiles: profile.currentContracts.schemaFiles,
    fixtureManifests: profile.currentContracts.fixtureManifests,
    fixtureCases: profile.currentContracts.fixtureCases,
  };
  if (JSON.stringify(actual) !== JSON.stringify(expected)) {
    throw profileError(
      "CONTRACT_STANDARDS_BINDING_MISMATCH",
      "/currentContracts",
      `Current contract cardinality mismatch: expected=${JSON.stringify(expected)} actual=${JSON.stringify(actual)}.`,
    );
  }
  const sourceFiles = contractFiles
    .filter(
      (path) =>
        path !== "contracts/generation.lock.json" && !path.startsWith("contracts/generated/"),
    )
    .toSorted();
  const manifest = createHash("sha256");
  for (const path of sourceFiles) {
    const bytes = readContainedRegularFile(root, path);
    const mode = lstatSync(resolve(root, path)).mode & 0o111 ? "100755" : "100644";
    manifest
      .update(path.slice("contracts/".length))
      .update("\0")
      .update(sha256(bytes))
      .update("\0")
      .update(mode)
      .update("\0");
  }
  const sourceDigest = `sha256:${manifest.digest("hex")}`;
  if (profile.currentContracts.sourceContractManifestSha256 !== sourceDigest) {
    throw profileError(
      "CONTRACT_STANDARDS_BINDING_MISMATCH",
      "/currentContracts/sourceContractManifestSha256",
      `Current source contract manifest mismatch: expected=${String(profile.currentContracts.sourceContractManifestSha256)} actual=${sourceDigest}.`,
    );
  }
}

function listContainedRegularFiles(root: string, directory: string): string[] {
  const rootReal = realpathSync(root);
  const absolute = containedPath(rootReal, directory);
  const stat = lstatSync(absolute);
  if (!stat.isDirectory() || stat.isSymbolicLink() || realpathSync(absolute) !== absolute) {
    throw profileError(
      "CONTRACT_STANDARDS_PATH_INVALID",
      `/${directory}`,
      "Contract-standards input root must be a contained real directory.",
    );
  }
  const result: string[] = [];
  for (const entry of readdirSync(absolute).toSorted()) {
    const path = `${directory}/${entry}`;
    const entryAbsolute = containedPath(rootReal, path);
    const entryStat = lstatSync(entryAbsolute);
    if (entryStat.isSymbolicLink()) {
      throw profileError(
        "CONTRACT_STANDARDS_PATH_INVALID",
        `/${path}`,
        "Contract-standards input closures reject symbolic links.",
      );
    }
    if (entryStat.isDirectory()) result.push(...listContainedRegularFiles(rootReal, path));
    else if (entryStat.isFile() && realpathSync(entryAbsolute) === entryAbsolute) result.push(path);
    else {
      throw profileError(
        "CONTRACT_STANDARDS_PATH_INVALID",
        `/${path}`,
        "Contract-standards input closures accept only real directories and regular files.",
      );
    }
  }
  return result;
}

function readContainedRegularFile(root: string, path: string): Buffer {
  const rootReal = realpathSync(root);
  const absolute = containedPath(rootReal, path);
  try {
    const pathBefore = lstatSync(absolute, { bigint: true });
    if (
      !pathBefore.isFile() ||
      pathBefore.isSymbolicLink() ||
      realpathSync(absolute) !== absolute
    ) {
      throw new Error("path is not a real regular file");
    }
    const descriptor = openSync(absolute, constants.O_RDONLY | constants.O_NOFOLLOW);
    try {
      const descriptorBefore = fstatSync(descriptor, { bigint: true });
      if (
        !descriptorBefore.isFile() ||
        descriptorBefore.dev !== pathBefore.dev ||
        descriptorBefore.ino !== pathBefore.ino
      ) {
        throw new Error("path changed before open");
      }
      const bytes = readFileSync(descriptor);
      const descriptorAfter = fstatSync(descriptor, { bigint: true });
      const pathAfter = lstatSync(absolute, { bigint: true });
      if (
        descriptorAfter.dev !== descriptorBefore.dev ||
        descriptorAfter.ino !== descriptorBefore.ino ||
        descriptorAfter.size !== descriptorBefore.size ||
        descriptorAfter.mtimeNs !== descriptorBefore.mtimeNs ||
        descriptorAfter.ctimeNs !== descriptorBefore.ctimeNs ||
        pathAfter.dev !== descriptorBefore.dev ||
        pathAfter.ino !== descriptorBefore.ino ||
        !pathAfter.isFile() ||
        pathAfter.isSymbolicLink() ||
        realpathSync(absolute) !== absolute
      ) {
        throw new Error("path changed during read");
      }
      return bytes;
    } finally {
      closeSync(descriptor);
    }
  } catch (error) {
    throw profileError(
      "CONTRACT_STANDARDS_PATH_INVALID",
      `/${path}`,
      `Contract-standards path is missing, unstable, or not a contained regular file: ${String(error)}.`,
    );
  }
}

function containedPath(rootReal: string, path: string): string {
  if (isAbsolute(path) || path === "" || path.split(/[\\/]/u).some((part) => part === "..")) {
    throw profileError(
      "CONTRACT_STANDARDS_PATH_INVALID",
      `/${path}`,
      "Contract-standards paths must be normalized repository-relative paths.",
    );
  }
  const absolute = resolve(rootReal, path);
  const normalized = relative(rootReal, absolute).split(sep).join("/");
  if (normalized === ".." || normalized.startsWith("../") || normalized !== path) {
    throw profileError(
      "CONTRACT_STANDARDS_PATH_INVALID",
      `/${path}`,
      "Contract-standards path escapes or is not normalized within the repository.",
    );
  }
  return absolute;
}

function object(value: unknown, path: string): JsonObject {
  if (typeof value !== "object" || value === null || Array.isArray(value)) {
    throw profileError(
      "CONTRACT_STANDARDS_PROFILE_INVALID",
      path,
      "Contract-standards profile value must be an object.",
    );
  }
  return value as JsonObject;
}

function string(value: unknown, path: string): string {
  if (typeof value !== "string" || value === "") {
    throw profileError(
      "CONTRACT_STANDARDS_PROFILE_INVALID",
      path,
      "Contract-standards profile value must be a non-empty string.",
    );
  }
  return value;
}

function exactKeys(value: JsonObject, expected: readonly string[], path: string): void {
  const actual = Object.keys(value).toSorted();
  const sortedExpected = [...expected].toSorted();
  if (JSON.stringify(actual) !== JSON.stringify(sortedExpected)) {
    throw profileError(
      "CONTRACT_STANDARDS_PROFILE_INVALID",
      path,
      `Contract-standards profile topology mismatch: expected=${JSON.stringify(sortedExpected)} actual=${JSON.stringify(actual)}.`,
    );
  }
}

function exactObject(value: unknown, expected: JsonObject, path: string): void {
  const actual = object(value, path);
  exactKeys(actual, Object.keys(expected), path);
  if (Object.entries(expected).some(([key, expectedValue]) => actual[key] !== expectedValue)) {
    throw profileError(
      "CONTRACT_STANDARDS_PROFILE_INVALID",
      path,
      "Contract-standards exact authority object drifted.",
    );
  }
}

function sha256(bytes: Buffer): string {
  return createHash("sha256").update(bytes).digest("hex");
}

function profileError(
  code: ContractStandardsProfileError["code"],
  path: string,
  message: string,
): ContractStandardsProfileError {
  return new ContractStandardsProfileError(code, path, message);
}
