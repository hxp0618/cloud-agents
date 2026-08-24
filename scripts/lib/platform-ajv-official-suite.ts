import { createHash } from "node:crypto";
import { createRequire } from "node:module";
import { lstatSync, readFileSync, readdirSync, realpathSync, writeFileSync } from "node:fs";
import { isAbsolute, relative, resolve, sep } from "node:path";

import Ajv2020 from "ajv/dist/2020.js";

import { canonicalJsonDigest, type JsonRecord } from "./platform-json-semantics";

export const AJV_OFFICIAL_SUITE_AUDIT_SOURCE_PATH =
  "contracts/platform/v1alpha1/fixtures/golden/ajv-official-suite-audit-source-v1.json";
export const AJV_OFFICIAL_SUITE_AUDIT_OUTPUT_PATH =
  "contracts/generated/platform/v1alpha1/ajv-official-suite-audit-v1.json";

const SOURCE_SCHEMA_PATH =
  "contracts/platform/v1alpha1/schemas/ajv-official-suite-audit-source-v1.schema.json";
const OUTPUT_SCHEMA_PATH =
  "contracts/platform/v1alpha1/schemas/ajv-official-suite-audit-v1.schema.json";
const SOURCE_SCHEMA_ID =
  "https://schemas.cloud-agents.dev/platform/v1alpha1/schemas/ajv-official-suite-audit-source-v1.schema.json";
const OUTPUT_SCHEMA_ID =
  "https://schemas.cloud-agents.dev/platform/v1alpha1/schemas/ajv-official-suite-audit-v1.schema.json";
const SOURCE_DOMAIN = "cloud-agents/ajv-official-suite-audit/source/v1";
const AUDIT_DOMAIN = "cloud-agents/ajv-official-suite-audit/result/v1";
const STATUS = "EXECUTED_NONCONFORMANT";

export const AJV_OFFICIAL_SUITE_OPTIONS = {
  allErrors: true,
  strict: false,
  validateSchema: true,
  validateFormats: false,
  ownProperties: true,
  removeAdditional: false,
  useDefaults: false,
  coerceTypes: false,
} as const;

type Summary = Readonly<{
  files: number;
  cases: number;
  assertions: number;
  remotes: number;
  passedAssertions: number;
  compileFailedCases: number;
  notRunAssertions: number;
  validityMismatches: number;
  runtimeErrors: number;
  discrepancyRecords: number;
  nonPassingAssertions: number;
}>;
type CategorySummary = Readonly<{
  category: string;
  compileFailedCases: number;
  notRunAssertions: number;
  validityMismatches: number;
  runtimeErrors: number;
  discrepancyRecords: number;
  nonPassingAssertions: number;
}>;
type DiscrepancyKind = "COMPILE_FAILURE" | "RUNTIME_ERROR" | "VALIDITY_MISMATCH";
type Boundary =
  | "EMPTY_ENUM_REJECTED"
  | "NON_HASH_DYNAMIC_REF_REJECTED"
  | "DUNDER_PROTO_PROPERTY_FILTERED"
  | "VOCABULARY_REGISTRATION_BEHAVIOR";

export type AjvOfficialSuiteDiscrepancy = JsonRecord & {
  readonly id: string;
  readonly category: string;
  readonly file: string;
  readonly caseIndex: number;
  readonly caseDescription: string;
  readonly kind: DiscrepancyKind;
  readonly testIndex?: number;
  readonly testDescription?: string;
  readonly expected?: boolean;
  readonly actual?: boolean;
  readonly message?: string;
  readonly explanationClass: "DOCUMENTED_AJV_BOUNDARY" | "OBSERVED_DIFFERENCE";
  readonly boundary?: Boundary;
};

type Source = JsonRecord & {
  readonly formatVersion: string;
  readonly auditId: string;
  readonly status: string;
  readonly notGateClosure: boolean;
  readonly validator: {
    readonly package: string;
    readonly name: string;
    readonly version: string;
    readonly dialect: string;
    readonly packageManifestSha256: string;
    readonly dependencyAuthority: readonly string[];
  };
  readonly execution: JsonRecord & {
    readonly freshValidatorPerCase: boolean;
    readonly options: JsonRecord;
    readonly remoteRegistry: JsonRecord & {
      readonly baseUri: string;
      readonly files: number;
      readonly registration: string;
      readonly networkFetch: string;
      readonly loadSchema: string;
    };
  };
  readonly corpus: JsonRecord & {
    readonly profilePath: string;
    readonly localRoot: string;
    readonly commit: string;
    readonly tree: string;
    readonly mandatoryTree: string;
    readonly manifestAlgorithm: string;
    readonly manifestSha256: string;
    readonly corpusFiles: number;
    readonly mandatoryFiles: number;
    readonly cases: number;
    readonly assertions: number;
    readonly remoteFiles: number;
  };
  readonly expected: JsonRecord & {
    readonly status: string;
    readonly conformanceClaim: boolean;
    readonly summary: Summary;
    readonly categories: readonly CategorySummary[];
  };
};

type OfficialCase = {
  readonly description: string;
  readonly schema: unknown;
  readonly tests: ReadonlyArray<{
    readonly description: string;
    readonly data: unknown;
    readonly valid: boolean;
  }>;
};

export class AjvOfficialSuiteAuditError extends Error {
  constructor(
    readonly code:
      | "AJV_OFFICIAL_SUITE_AUDIT_CONFORMANCE_REQUIRED"
      | "AJV_OFFICIAL_SUITE_AUDIT_CORPUS_MISMATCH"
      | "AJV_OFFICIAL_SUITE_AUDIT_OUTPUT_STALE"
      | "AJV_OFFICIAL_SUITE_AUDIT_RESULT_MISMATCH"
      | "AJV_OFFICIAL_SUITE_AUDIT_SOURCE_INVALID",
    message: string,
  ) {
    super(message);
    this.name = "AjvOfficialSuiteAuditError";
  }
}

export function buildAjvOfficialSuiteAudit(root: string): JsonRecord {
  const source = readSource(root);
  validateAjvOfficialSuiteAuditSource(root, source);
  const corpusRoot = resolveContainedPath(root, source.corpus.localRoot, "directory");
  const testRoot = resolveContainedPath(corpusRoot, "tests/draft2020-12", "directory");
  const remoteRoot = resolveContainedPath(corpusRoot, "remotes", "directory");
  const testFiles = listJsonFiles(testRoot, false);
  const remoteFiles = listJsonFiles(remoteRoot, true);
  const remotes = remoteFiles.map((path) => ({
    schema: parseJsonFile(path),
    uri: `http://localhost:1234/${relative(remoteRoot, path).split(sep).join("/")}`,
  }));

  const discrepancies: AjvOfficialSuiteDiscrepancy[] = [];
  let cases = 0;
  let assertions = 0;
  let passedAssertions = 0;
  let notRunAssertions = 0;

  for (const testPath of testFiles) {
    const file = relative(testRoot, testPath).split(sep).join("/");
    const category = file.replace(/\.json$/u, "");
    const suiteCases = requireCases(parseJsonFile(testPath), file);
    for (const [caseIndex, suiteCase] of suiteCases.entries()) {
      cases += 1;
      assertions += suiteCase.tests.length;
      const ajv = new Ajv2020(AJV_OFFICIAL_SUITE_OPTIONS);
      for (const remote of remotes) ajv.addSchema(remote.schema as never, remote.uri, false, false);
      let validate: ReturnType<typeof ajv.compile>;
      try {
        validate = ajv.compile(suiteCase.schema as never);
      } catch (error) {
        notRunAssertions += suiteCase.tests.length;
        discrepancies.push(
          discrepancy({
            id: `${file}#/cases/${caseIndex}/compile`,
            category,
            file,
            caseIndex,
            caseDescription: suiteCase.description,
            kind: "COMPILE_FAILURE",
            message: stableError(error),
          }),
        );
        continue;
      }
      for (const [testIndex, test] of suiteCase.tests.entries()) {
        try {
          const actual = validate(test.data) as boolean;
          if (actual === test.valid) {
            passedAssertions += 1;
            continue;
          }
          discrepancies.push(
            discrepancy({
              id: `${file}#/cases/${caseIndex}/tests/${testIndex}`,
              category,
              file,
              caseIndex,
              caseDescription: suiteCase.description,
              kind: "VALIDITY_MISMATCH",
              testIndex,
              testDescription: test.description,
              expected: test.valid,
              actual,
            }),
          );
        } catch (error) {
          discrepancies.push(
            discrepancy({
              id: `${file}#/cases/${caseIndex}/tests/${testIndex}`,
              category,
              file,
              caseIndex,
              caseDescription: suiteCase.description,
              kind: "RUNTIME_ERROR",
              testIndex,
              testDescription: test.description,
              expected: test.valid,
              message: stableError(error),
            }),
          );
        }
      }
    }
  }

  const summary = buildSummary({
    files: testFiles.length,
    cases,
    assertions,
    remotes: remoteFiles.length,
    passedAssertions,
    notRunAssertions,
    discrepancies,
  });
  const categories = buildCategorySummaries(discrepancies, suiteAssertionCounts(testFiles));
  assertObservedMatchesExpected(summary, categories, source.expected);
  const sourceDigest = domainDigest(SOURCE_DOMAIN, source);
  const body: JsonRecord = {
    formatVersion: "cloud-agents-ajv-official-suite-audit/v1",
    auditId: "cloud-agents/platform/ajv-official-suite-audit",
    sourceDigest,
    status: STATUS,
    conformanceClaim: false,
    notGateClosure: true,
    validator: source.validator,
    execution: source.execution,
    corpus: source.corpus,
    summary,
    categories,
    discrepancies,
    implementationBoundary: {
      closureCriterion: "remains_missing",
      gateStatus: "all_gates_open",
      productionRuntimeDependency: "forbidden",
      networkFetch: "forbidden",
      httpSurface: "not_implemented",
      productionDatabaseWrites: "not_authorized",
      deployment: "not_authorized",
      publication: "not_authorized",
    },
  };
  const result = { ...body, auditDigest: domainDigest(AUDIT_DOMAIN, body) };
  validateAgainstSchema(root, OUTPUT_SCHEMA_ID, result);
  return result;
}

export function serializeAjvOfficialSuiteAudit(audit: JsonRecord): string {
  return `${JSON.stringify(audit, null, 2)}\n`;
}

export function writeAjvOfficialSuiteAudit(root: string): void {
  const output = resolveContainedPath(root, AJV_OFFICIAL_SUITE_AUDIT_OUTPUT_PATH, "file", true);
  writeFileSync(output, serializeAjvOfficialSuiteAudit(buildAjvOfficialSuiteAudit(root)));
}

export function assertAjvOfficialSuiteAuditCurrent(root: string): void {
  const expected = serializeAjvOfficialSuiteAudit(buildAjvOfficialSuiteAudit(root));
  const output = resolveContainedPath(root, AJV_OFFICIAL_SUITE_AUDIT_OUTPUT_PATH, "file");
  const actual = readFileSync(output, "utf8");
  if (actual !== expected) {
    throw new AjvOfficialSuiteAuditError(
      "AJV_OFFICIAL_SUITE_AUDIT_OUTPUT_STALE",
      `${AJV_OFFICIAL_SUITE_AUDIT_OUTPUT_PATH} is stale; run the audit checker with --write.`,
    );
  }
}

export function requireAjvOfficialSuiteConformance(root: string): never {
  const result = buildAjvOfficialSuiteAudit(root) as JsonRecord & { status: string };
  throw new AjvOfficialSuiteAuditError(
    "AJV_OFFICIAL_SUITE_AUDIT_CONFORMANCE_REQUIRED",
    `Ajv official-suite conformance is required but the deterministic audit status is ${result.status}.`,
  );
}

export function ajvOfficialSuiteAuditInputs(root: string): string[] {
  const source = readSource(root);
  validateAjvOfficialSuiteAuditSource(root, source);
  const corpusRoot = resolveContainedPath(root, source.corpus.localRoot, "directory");
  const corpusInputs = listRegularFiles(corpusRoot).map((path) =>
    relative(root, path).split(sep).join("/"),
  );
  return [
    AJV_OFFICIAL_SUITE_AUDIT_SOURCE_PATH,
    SOURCE_SCHEMA_PATH,
    OUTPUT_SCHEMA_PATH,
    source.corpus.profilePath,
    "docs/plan/p1/g-contract-r5-b1-ajv-official-suite-audit-20260824.md",
    "package.json",
    "bun.lock",
    "scripts/check-platform-ajv-official-suite.ts",
    "scripts/lib/platform-ajv-official-suite.test.ts",
    "scripts/lib/platform-ajv-official-suite.ts",
    "scripts/lib/platform-json-semantics.ts",
    ...corpusInputs,
  ]
    .filter((path, index, paths) => paths.indexOf(path) === index)
    .toSorted();
}

export function validateAjvOfficialSuiteAuditSource(root: string, source: Source): void {
  validateAgainstSchema(root, SOURCE_SCHEMA_ID, source);
  assertResolvedAjvPackageIdentity(root, {
    name: source.validator.package,
    version: source.validator.version,
    packageManifestSha256: source.validator.packageManifestSha256,
  });
  const profilePath = resolveContainedPath(root, source.corpus.profilePath, "file");
  const profile = parseJsonFile(profilePath) as JsonRecord;
  const suite = requireRecord(profile.jsonSchemaOfficialSuite, "standards profile suite");
  const facts = {
    localRoot: requireString(suite.localRoot, "suite localRoot"),
    commit: requireString(suite.commit, "suite commit"),
    tree: requireString(suite.tree, "suite tree"),
    mandatoryTree: requireString(suite.mandatoryTree, "suite mandatoryTree"),
    manifestAlgorithm: requireString(suite.corpusManifestAlgorithm, "suite manifest algorithm"),
    manifestSha256: requireString(suite.corpusManifestSha256, "suite manifest SHA-256"),
    corpusFiles: requireInteger(suite.corpusFiles, "suite corpus files"),
    mandatoryFiles: requireInteger(suite.mandatoryFiles, "suite mandatory files"),
    cases: requireInteger(suite.cases, "suite cases"),
    assertions: requireInteger(suite.assertions, "suite assertions"),
    remoteFiles: requireInteger(suite.remoteFiles, "suite remote files"),
  };
  for (const [key, expected] of Object.entries(facts)) {
    if (source.corpus[key] !== expected) {
      throw new AjvOfficialSuiteAuditError(
        "AJV_OFFICIAL_SUITE_AUDIT_SOURCE_INVALID",
        `Audit source corpus ${key} drifted from ${source.corpus.profilePath}.`,
      );
    }
  }
  const corpusRoot = resolveContainedPath(root, source.corpus.localRoot, "directory");
  const manifest = corpusManifest(corpusRoot);
  if (
    source.corpus.manifestAlgorithm !== "sorted-path-nul-sha256-nul-size-v1" ||
    manifest.sha256 !== source.corpus.manifestSha256 ||
    manifest.files !== source.corpus.corpusFiles
  ) {
    throw new AjvOfficialSuiteAuditError(
      "AJV_OFFICIAL_SUITE_AUDIT_CORPUS_MISMATCH",
      `Official-suite corpus mismatch: expected=${source.corpus.manifestSha256}/${source.corpus.corpusFiles} actual=${manifest.sha256}/${manifest.files}.`,
    );
  }
}

export function assertObservedMatchesExpected(
  summary: Readonly<Record<string, number>>,
  categories: readonly Readonly<Record<string, string | number>>[],
  expected: Readonly<{
    status: string;
    conformanceClaim: boolean;
    summary: unknown;
    categories: unknown;
  }>,
): void {
  if (
    expected.status !== STATUS ||
    expected.conformanceClaim !== false ||
    JSON.stringify(summary) !== JSON.stringify(expected.summary) ||
    JSON.stringify(categories) !== JSON.stringify(expected.categories)
  ) {
    throw new AjvOfficialSuiteAuditError(
      "AJV_OFFICIAL_SUITE_AUDIT_RESULT_MISMATCH",
      "Observed Ajv official-suite result drifted from the versioned EXECUTED_NONCONFORMANT expectation.",
    );
  }
}

function buildSummary(input: {
  readonly files: number;
  readonly cases: number;
  readonly assertions: number;
  readonly remotes: number;
  readonly passedAssertions: number;
  readonly notRunAssertions: number;
  readonly discrepancies: readonly AjvOfficialSuiteDiscrepancy[];
}): Summary {
  const compileFailedCases = input.discrepancies.filter(
    (item) => item.kind === "COMPILE_FAILURE",
  ).length;
  const validityMismatches = input.discrepancies.filter(
    (item) => item.kind === "VALIDITY_MISMATCH",
  ).length;
  const runtimeErrors = input.discrepancies.filter((item) => item.kind === "RUNTIME_ERROR").length;
  const discrepancyRecords = input.discrepancies.length;
  return {
    files: input.files,
    cases: input.cases,
    assertions: input.assertions,
    remotes: input.remotes,
    passedAssertions: input.passedAssertions,
    compileFailedCases,
    notRunAssertions: input.notRunAssertions,
    validityMismatches,
    runtimeErrors,
    discrepancyRecords,
    nonPassingAssertions: input.assertions - input.passedAssertions,
  };
}

function buildCategorySummaries(
  discrepancies: readonly AjvOfficialSuiteDiscrepancy[],
  assertionsByCase: ReadonlyMap<string, number>,
): CategorySummary[] {
  const categories = [...new Set(discrepancies.map((item) => item.category))].toSorted();
  return categories.map((category) => {
    const categoryItems = discrepancies.filter((item) => item.category === category);
    const compileItems = categoryItems.filter((item) => item.kind === "COMPILE_FAILURE");
    const notRunAssertions = compileItems.reduce(
      (total, item) => total + (assertionsByCase.get(`${item.file}#${item.caseIndex}`) ?? 0),
      0,
    );
    const validityMismatches = categoryItems.filter(
      (item) => item.kind === "VALIDITY_MISMATCH",
    ).length;
    const runtimeErrors = categoryItems.filter((item) => item.kind === "RUNTIME_ERROR").length;
    return {
      category,
      compileFailedCases: compileItems.length,
      notRunAssertions,
      validityMismatches,
      runtimeErrors,
      discrepancyRecords: categoryItems.length,
      nonPassingAssertions: notRunAssertions + validityMismatches + runtimeErrors,
    } as CategorySummary;
  });
}

function suiteAssertionCounts(testFiles: readonly string[]): ReadonlyMap<string, number> {
  const counts = new Map<string, number>();
  for (const testPath of testFiles) {
    const testRoot = resolve(testPath, "..");
    const file = relative(testRoot, testPath).split(sep).join("/");
    const suiteCases = requireCases(parseJsonFile(testPath), file);
    suiteCases.forEach((suiteCase, caseIndex) => {
      counts.set(`${file}#${caseIndex}`, suiteCase.tests.length);
    });
  }
  return counts;
}

function discrepancy(
  input: Omit<AjvOfficialSuiteDiscrepancy, "explanationClass" | "boundary">,
): AjvOfficialSuiteDiscrepancy {
  const boundary = documentedBoundary(input);
  return {
    ...input,
    explanationClass: boundary ? "DOCUMENTED_AJV_BOUNDARY" : "OBSERVED_DIFFERENCE",
    ...(boundary ? { boundary } : {}),
  };
}

function documentedBoundary(input: {
  readonly category: string;
  readonly caseDescription: string;
  readonly testDescription?: string;
  readonly message?: string;
}): Boundary | undefined {
  const combined = `${input.caseDescription}\n${input.testDescription ?? ""}\n${input.message ?? ""}`;
  if (input.category === "dynamicRef" && /only supports hash fragment reference/iu.test(combined))
    return "NON_HASH_DYNAMIC_REF_REJECTED";
  if (
    input.category === "enum" &&
    /enum (?:must NOT have fewer than 1 items|must have non-empty array)/iu.test(combined)
  )
    return "EMPTY_ENUM_REJECTED";
  if (input.category === "properties" && /__proto__/u.test(combined))
    return "DUNDER_PROTO_PROPERTY_FILTERED";
  if (input.category === "vocabulary") return "VOCABULARY_REGISTRATION_BEHAVIOR";
  return undefined;
}

function requireCases(value: unknown, file: string): OfficialCase[] {
  if (!Array.isArray(value)) throw sourceError(`${file} must contain a test-case array.`);
  return value.map((rawCase, caseIndex) => {
    const item = requireRecord(rawCase, `${file} case ${caseIndex}`);
    const description = requireString(item.description, `${file} case ${caseIndex} description`);
    if (!("schema" in item)) throw sourceError(`${file} case ${caseIndex} has no schema.`);
    if (!Array.isArray(item.tests)) throw sourceError(`${file} case ${caseIndex} has no tests.`);
    const tests = item.tests.map((rawTest, testIndex) => {
      const test = requireRecord(rawTest, `${file} case ${caseIndex} test ${testIndex}`);
      const valid = test.valid;
      if (typeof valid !== "boolean")
        throw sourceError(`${file} case ${caseIndex} test ${testIndex} validity is not boolean.`);
      return {
        description: requireString(
          test.description,
          `${file} case ${caseIndex} test ${testIndex} description`,
        ),
        data: test.data,
        valid,
      };
    });
    return { description, schema: item.schema, tests };
  });
}

function readSource(root: string): Source {
  return parseJsonFile(
    resolveContainedPath(root, AJV_OFFICIAL_SUITE_AUDIT_SOURCE_PATH, "file"),
  ) as Source;
}

function validateAgainstSchema(root: string, schemaId: string, value: unknown): void {
  const ajv = new Ajv2020({ allErrors: true, strict: true, validateFormats: true });
  ajv.addKeyword({ keyword: "x-cloud-agents-security", schemaType: "object" });
  for (const path of [SOURCE_SCHEMA_PATH, OUTPUT_SCHEMA_PATH]) {
    ajv.addSchema(parseJsonFile(resolveContainedPath(root, path, "file")) as never);
  }
  const validate = ajv.getSchema(schemaId);
  if (!validate) throw sourceError(`Audit schema ${schemaId} was not registered.`);
  if (!validate(value))
    throw sourceError(`Audit schema validation failed: ${ajv.errorsText(validate.errors)}.`);
}

function corpusManifest(root: string): { readonly sha256: string; readonly files: number } {
  const hash = createHash("sha256");
  const files = listRegularFiles(root);
  for (const path of files) {
    const content = readFileSync(path);
    hash.update(relative(root, path).split(sep).join("/"));
    hash.update("\0");
    hash.update(createHash("sha256").update(content).digest("hex"));
    hash.update("\0");
    hash.update(String(content.byteLength));
    hash.update("\0");
  }
  return { sha256: hash.digest("hex"), files: files.length };
}

function listJsonFiles(root: string, recursive: boolean): string[] {
  const files = recursive ? listRegularFiles(root) : listImmediateRegularFiles(root);
  const json = files.filter((path) => path.endsWith(".json"));
  if (json.length !== files.length)
    throw sourceError(`Expected only JSON files under ${relative(process.cwd(), root)}.`);
  return json;
}

function listImmediateRegularFiles(root: string): string[] {
  return readdirSync(root, { withFileTypes: true })
    .map((entry) => {
      if (!entry.isFile() || entry.isSymbolicLink())
        throw sourceError(`Expected regular file under ${root}: ${entry.name}.`);
      return resolve(root, entry.name);
    })
    .toSorted();
}

function listRegularFiles(root: string): string[] {
  const files: string[] = [];
  function walk(directory: string): void {
    for (const entry of readdirSync(directory, { withFileTypes: true })) {
      const path = resolve(directory, entry.name);
      if (entry.isSymbolicLink())
        throw sourceError(`Symlink is forbidden in audit input: ${path}.`);
      if (entry.isDirectory()) walk(path);
      else if (entry.isFile()) files.push(path);
      else throw sourceError(`Non-regular audit input: ${path}.`);
    }
  }
  walk(root);
  return files.toSorted((left, right) => {
    const leftRelative = relative(root, left).split(sep).join("/");
    const rightRelative = relative(root, right).split(sep).join("/");
    return leftRelative < rightRelative ? -1 : leftRelative > rightRelative ? 1 : 0;
  });
}

function resolveContainedPath(
  root: string,
  path: string,
  expected: "directory" | "file",
  allowMissing = false,
): string {
  const rootAbsolute = resolve(root);
  const rootReal = realpathSync(rootAbsolute);
  const candidate = resolve(rootAbsolute, path);
  const lexical = relative(rootAbsolute, candidate);
  if (lexical === "" || lexical === ".." || lexical.startsWith(`..${sep}`) || isAbsolute(lexical))
    throw sourceError(`Audit path must be a contained repository-relative path: ${path}.`);
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
        allowMissing &&
        error instanceof Error &&
        "code" in error &&
        error.code === "ENOENT"
      )
        return current;
      throw error;
    }
    if (stat.isSymbolicLink()) throw sourceError(`Audit path contains a symlink: ${path}.`);
    if (!final && !stat.isDirectory())
      throw sourceError(`Audit path has a non-directory parent: ${path}.`);
    if (final && expected === "file" && !stat.isFile())
      throw sourceError(`Audit path is not a regular file: ${path}.`);
    if (final && expected === "directory" && !stat.isDirectory())
      throw sourceError(`Audit path is not a regular directory: ${path}.`);
  }
  const actual = realpathSync(current);
  const contained = relative(rootReal, actual);
  if (contained === ".." || contained.startsWith(`..${sep}`) || isAbsolute(contained))
    throw sourceError(`Audit path resolves outside repository root: ${path}.`);
  return current;
}

export function assertResolvedAjvPackageIdentity(
  root: string,
  expected: Readonly<{
    name: string;
    version: string;
    packageManifestSha256: string;
  }>,
): void {
  const manifest = requireRecord(
    parseJsonFile(resolveContainedPath(root, "package.json", "file")),
    "package manifest",
  );
  const devDependencies = requireRecord(manifest.devDependencies, "package devDependencies");
  if (devDependencies.ajv !== expected.version)
    throw sourceError("package.json must pin the audited Ajv version exactly.");

  const require = createRequire(import.meta.url);
  const installedManifestPath = require.resolve("ajv/package.json");
  const installedManifest = requireRecord(
    parseJsonFile(installedManifestPath),
    "installed Ajv package manifest",
  );
  if (installedManifest.name !== expected.name || installedManifest.version !== expected.version) {
    throw sourceError(
      `Resolved Ajv package identity mismatch: expected=${expected.name}@${expected.version} actual=${String(installedManifest.name)}@${String(installedManifest.version)}.`,
    );
  }
  const actualPackageManifestSha256 = createHash("sha256")
    .update(readFileSync(installedManifestPath))
    .digest("hex");
  if (actualPackageManifestSha256 !== expected.packageManifestSha256)
    throw sourceError(
      `Resolved Ajv package manifest drifted: expected=${expected.packageManifestSha256} actual=${actualPackageManifestSha256}.`,
    );
  const lock = readFileSync(resolveContainedPath(root, "bun.lock", "file"), "utf8");
  if (!lock.includes(`"ajv": "${expected.version}"`))
    throw sourceError("bun.lock does not bind the audited Ajv version.");
}

function parseJsonFile(path: string): unknown {
  return JSON.parse(readFileSync(path, "utf8")) as unknown;
}

function requireRecord(value: unknown, label: string): JsonRecord {
  if (value === null || typeof value !== "object" || Array.isArray(value))
    throw sourceError(`${label} must be an object.`);
  return value as JsonRecord;
}

function requireString(value: unknown, label: string): string {
  if (typeof value !== "string" || value === "") throw sourceError(`${label} must be a string.`);
  return value;
}

function requireInteger(value: unknown, label: string): number {
  if (!Number.isSafeInteger(value) || (value as number) < 0)
    throw sourceError(`${label} must be a non-negative integer.`);
  return value as number;
}

function stableError(error: unknown): string {
  if (error instanceof Error) return `${error.name}: ${error.message}`;
  return `Error: ${String(error)}`;
}

function domainDigest(domain: string, value: unknown): string {
  return canonicalJsonDigest({ domain, value });
}

function sourceError(message: string): AjvOfficialSuiteAuditError {
  return new AjvOfficialSuiteAuditError("AJV_OFFICIAL_SUITE_AUDIT_SOURCE_INVALID", message);
}
