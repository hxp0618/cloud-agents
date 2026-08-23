import { createHash } from "node:crypto";
import { lstatSync, readdirSync, readFileSync, realpathSync, writeFileSync } from "node:fs";
import { isAbsolute, relative, resolve, sep } from "node:path";

import Ajv2020, { type ErrorObject } from "ajv/dist/2020.js";

import { canonicalizeJson, type JsonRecord } from "./platform-json-semantics";

export const IDENTITY_VERIFIER_SOURCE_PATH =
  "tools/platform-identity-verifier/v1/fixtures/golden/identity-verifier-registry-source-v1.json";
export const IDENTITY_VERIFIER_OUTPUT_PATH =
  "contracts/generated/platform/v1alpha1/identity-verifier-registry-v1.json";

const SOURCE_SCHEMA_PATH =
  "tools/platform-identity-verifier/v1/schemas/identity-verifier-registry-source-v1.schema.json";
const OUTPUT_SCHEMA_PATH =
  "tools/platform-identity-verifier/v1/schemas/identity-verifier-registry-v1.schema.json";
const SOURCE_SCHEMA_ID =
  "https://schemas.cloud-agents.dev/platform/v1alpha1/schemas/identity-verifier-registry-source-v1.schema.json";
const OUTPUT_SCHEMA_ID =
  "https://schemas.cloud-agents.dev/platform/v1alpha1/schemas/identity-verifier-registry-v1.schema.json";
const SOURCE_FORMAT = "cloud-agents-platform-identity-verifier-source/v1";
const OUTPUT_FORMAT = "cloud-agents-platform-identity-verifier-registry/v1";
const REGISTRY_ID = "cloud-agents/platform/identity-verifier";
const PROFILE_ID = "platform-identity-verifier/v1";
const PROFILE_DOMAIN = "cloud-agents/platform-identity-verifier/profile/v1";
const REGISTRY_DOMAIN = "cloud-agents/platform-identity-verifier/registry/v1";
const FATAL_UTF8 = new TextDecoder("utf-8", { fatal: true });

const NEGATIVE_FIXTURE_PATHS = [
  "tools/platform-identity-verifier/v1/fixtures/negative/identity-verifier-caller-selected-algorithm.json",
  "tools/platform-identity-verifier/v1/fixtures/negative/identity-verifier-http-authority.json",
  "tools/platform-identity-verifier/v1/fixtures/negative/identity-verifier-multiple-audiences.json",
] as const;

export const IDENTITY_VERIFIER_FIXTURE_MANIFEST_PATH =
  "tools/platform-identity-verifier/v1/fixtures/manifest.json";
const FIXTURE_ROOT_PATH = "tools/platform-identity-verifier/v1/fixtures";

const EXPECTED_FIXTURE_MANIFEST = {
  formatVersion: "cloud-agents-platform-identity-verifier-fixtures/v1",
  profileId: PROFILE_ID,
  cases: [
    {
      name: "identity-verifier-caller-selected-algorithm",
      instance: "negative/identity-verifier-caller-selected-algorithm.json",
      expected: {
        valid: false,
        code: "IDENTITY_VERIFIER_BINDING_MISMATCH",
        path: "/profile/algorithm",
      },
    },
    {
      name: "identity-verifier-http-authority",
      instance: "negative/identity-verifier-http-authority.json",
      expected: {
        valid: false,
        code: "IDENTITY_VERIFIER_BOUNDARY_MISMATCH",
        path: "/profile/implementationNonClaims",
      },
    },
    {
      name: "identity-verifier-multiple-audiences",
      instance: "negative/identity-verifier-multiple-audiences.json",
      expected: {
        valid: false,
        code: "IDENTITY_VERIFIER_BINDING_MISMATCH",
        path: "/",
      },
    },
    {
      name: "identity-verifier-registry-source-v1",
      instance: "golden/identity-verifier-registry-source-v1.json",
      expected: { valid: true },
    },
  ],
} as const;

type IdentityVerifierErrorCode =
  | "IDENTITY_VERIFIER_BINDING_MISMATCH"
  | "IDENTITY_VERIFIER_BOUNDARY_MISMATCH"
  | "IDENTITY_VERIFIER_COLLECTION_ORDER_MISMATCH"
  | "IDENTITY_VERIFIER_REGISTRY_DIGEST_MISMATCH";

export type IdentityVerifierSemanticResult =
  | { readonly valid: true; readonly errors: readonly [] }
  | {
      readonly valid: false;
      readonly errors: ReadonlyArray<{
        readonly code: IdentityVerifierErrorCode;
        readonly path: string;
      }>;
    };

type RegistrySource = JsonRecord & {
  readonly formatVersion: string;
  readonly registryId: string;
  readonly profile: JsonRecord & { readonly profileId: string };
};

export class IdentityVerifierContractError extends Error {
  constructor(
    readonly code: IdentityVerifierErrorCode,
    readonly path: string,
    message: string,
  ) {
    super(message);
    this.name = "IdentityVerifierContractError";
  }
}

export function buildIdentityVerifierRegistry(root: string): JsonRecord {
  validateIdentityVerifierFixtureManifest(root);
  const source = readSource(root);
  validateIdentityVerifierSource(root, source);

  const profile = {
    ...source.profile,
    profileDigest: identityVerifierDomainDigest(PROFILE_DOMAIN, source.profile),
  };
  const body: JsonRecord = {
    formatVersion: OUTPUT_FORMAT,
    registryId: REGISTRY_ID,
    profile,
  };
  const generated = {
    ...body,
    registryDigest: identityVerifierDomainDigest(REGISTRY_DOMAIN, body),
  };
  validateAgainstSchema(root, OUTPUT_SCHEMA_ID, generated);
  return generated;
}

export function serializeIdentityVerifierRegistry(registry: JsonRecord): string {
  return `${JSON.stringify(registry, null, 2)}\n`;
}

export function writeIdentityVerifierRegistry(root: string): void {
  const output = resolveContainedAuthorityPath(root, IDENTITY_VERIFIER_OUTPUT_PATH, "file", true);
  writeFileSync(output, serializeIdentityVerifierRegistry(buildIdentityVerifierRegistry(root)));
}

export function assertIdentityVerifierRegistryCurrent(root: string): void {
  const expected = serializeIdentityVerifierRegistry(buildIdentityVerifierRegistry(root));
  const output = resolveContainedAuthorityPath(root, IDENTITY_VERIFIER_OUTPUT_PATH, "file");
  const actual = decodeAuthorityUtf8(readFileSync(output), output);
  parseJsonText(actual, output);
  if (actual !== expected) {
    throw contractError(
      "IDENTITY_VERIFIER_REGISTRY_DIGEST_MISMATCH",
      "/registryDigest",
      `${IDENTITY_VERIFIER_OUTPUT_PATH} is stale; run its generator with --write.`,
    );
  }
}

export function identityVerifierRegistryInputs(_root: string): string[] {
  return [
    IDENTITY_VERIFIER_SOURCE_PATH,
    ...NEGATIVE_FIXTURE_PATHS,
    IDENTITY_VERIFIER_FIXTURE_MANIFEST_PATH,
    SOURCE_SCHEMA_PATH,
    OUTPUT_SCHEMA_PATH,
    "docs/plan/adr/0025-p1-offline-jwt-access-token-verifier-contract.md",
    "docs/plan/p1/identity-verifier-entry-20260823.md",
    "scripts/generate-platform-identity-verifier-registry.ts",
    "scripts/lib/platform-identity-verifier-registry.test.ts",
    "scripts/lib/platform-identity-verifier-registry.ts",
    "scripts/lib/platform-json-semantics.ts",
  ].toSorted();
}

export function validateIdentityVerifierFixtureManifest(root: string): void {
  const manifest = parseJsonFile(root, IDENTITY_VERIFIER_FIXTURE_MANIFEST_PATH);
  if (!canonicalEqual(manifest, EXPECTED_FIXTURE_MANIFEST)) {
    throw contractError(
      "IDENTITY_VERIFIER_BINDING_MISMATCH",
      "/cases",
      "Identity verifier fixture manifest must be the exact closed v1 inventory.",
    );
  }

  const fixtureRoot = resolveContainedAuthorityPath(root, FIXTURE_ROOT_PATH, "directory");
  const actualInventory = ["golden", "negative"]
    .flatMap((directory) => {
      const inventoryDirectory = resolveContainedAuthorityPath(
        root,
        `${FIXTURE_ROOT_PATH}/${directory}`,
        "directory",
      );
      return readdirSync(inventoryDirectory, { withFileTypes: true }).map((entry) => {
        if (!entry.isFile() || entry.isSymbolicLink()) {
          throw contractError(
            "IDENTITY_VERIFIER_BINDING_MISMATCH",
            "/cases",
            `Identity verifier fixture inventory contains a non-regular entry: ${directory}/${entry.name}.`,
          );
        }
        return `${directory}/${entry.name}`;
      });
    })
    .toSorted();
  const expectedInventory = EXPECTED_FIXTURE_MANIFEST.cases
    .map((fixture) => fixture.instance)
    .toSorted();
  if (!canonicalEqual(actualInventory, expectedInventory)) {
    throw contractError(
      "IDENTITY_VERIFIER_BINDING_MISMATCH",
      "/cases",
      "Identity verifier fixture manifest and on-disk inventory differ.",
    );
  }

  for (const fixture of EXPECTED_FIXTURE_MANIFEST.cases) {
    const fixturePath = resolve(fixtureRoot, fixture.instance);
    const fixtureRelative = relative(fixtureRoot, fixturePath);
    if (
      fixtureRelative === "" ||
      fixtureRelative === ".." ||
      fixtureRelative.startsWith(`..${sep}`) ||
      isAbsolute(fixtureRelative)
    ) {
      throw contractError(
        "IDENTITY_VERIFIER_BINDING_MISMATCH",
        `/cases/${escapePointerToken(fixture.name)}/instance`,
        `Identity verifier fixture path escapes its closed root: ${fixture.instance}.`,
      );
    }
    const document = parseJsonFile(root, `${FIXTURE_ROOT_PATH}/${fixture.instance}`);
    const result = validateIdentityVerifierFixture(document, root);
    let actual: { valid: true } | { valid: false; code: IdentityVerifierErrorCode; path: string };
    if (result.valid) {
      actual = { valid: true };
    } else {
      const first = result.errors[0];
      if (!first) throw new Error(`Identity verifier fixture ${fixture.name} returned no error.`);
      actual = { valid: false, code: first.code, path: first.path };
    }
    if (!canonicalEqual(actual, fixture.expected)) {
      throw contractError(
        "IDENTITY_VERIFIER_BINDING_MISMATCH",
        `/cases/${escapePointerToken(fixture.name)}`,
        `Identity verifier fixture ${fixture.name} did not produce its declared result.`,
      );
    }
  }
}

export function validateIdentityVerifierFixture(
  document: unknown,
  root: string,
): IdentityVerifierSemanticResult {
  if (!isRecord(document)) return success();
  if (document.formatVersion === SOURCE_FORMAT) {
    try {
      validateIdentityVerifierSource(root, document as RegistrySource);
      return success();
    } catch (error) {
      if (error instanceof IdentityVerifierContractError) {
        return failure(error.code, error.path);
      }
      return failure("IDENTITY_VERIFIER_BINDING_MISMATCH", "/");
    }
  }
  if (document.formatVersion !== OUTPUT_FORMAT) return success();
  try {
    const expected = buildIdentityVerifierRegistry(root);
    if (!canonicalEqual(document, expected)) {
      return failure("IDENTITY_VERIFIER_REGISTRY_DIGEST_MISMATCH", "/registryDigest");
    }
    return success();
  } catch {
    return failure("IDENTITY_VERIFIER_REGISTRY_DIGEST_MISMATCH", "/registryDigest");
  }
}

export function validateIdentityVerifierSource(root: string, source: RegistrySource): void {
  validateAgainstSchema(root, SOURCE_SCHEMA_ID, source);
  if (
    source.formatVersion !== SOURCE_FORMAT ||
    source.registryId !== REGISTRY_ID ||
    source.profile.profileId !== PROFILE_ID
  ) {
    throw contractError(
      "IDENTITY_VERIFIER_BINDING_MISMATCH",
      "/profile/profileId",
      "Identity verifier source identity drifted.",
    );
  }
  validateSortedUniqueArrays(source.profile, "/profile");
}

export function identityVerifierDomainDigest(domain: string, value: unknown): string {
  const hash = createHash("sha256");
  hash.update(domain, "utf8");
  hash.update(Uint8Array.of(0));
  hash.update(canonicalizeJson(value));
  return `sha256:${hash.digest("hex")}`;
}

function validateSortedUniqueArrays(value: unknown, path: string): void {
  if (Array.isArray(value)) {
    if (!value.every((item) => typeof item === "string")) {
      throw contractError(
        "IDENTITY_VERIFIER_COLLECTION_ORDER_MISMATCH",
        path,
        "Identity verifier profile arrays must be string-valued closed sets.",
      );
    }
    const strings = value as string[];
    const sorted = strings.toSorted(compareUnsignedUtf8);
    if (new Set(strings).size !== strings.length || !canonicalEqual(strings, sorted)) {
      throw contractError(
        "IDENTITY_VERIFIER_COLLECTION_ORDER_MISMATCH",
        path,
        "Identity verifier profile set arrays must be unique and sorted by unsigned UTF-8 bytes.",
      );
    }
    return;
  }
  if (!isRecord(value)) return;
  for (const [key, child] of Object.entries(value)) {
    validateSortedUniqueArrays(child, `${path}/${escapePointerToken(key)}`);
  }
}

function compareUnsignedUtf8(left: string, right: string): number {
  return Buffer.compare(Buffer.from(left, "utf8"), Buffer.from(right, "utf8"));
}

function escapePointerToken(value: string): string {
  return value.replaceAll("~", "~0").replaceAll("/", "~1");
}

function readSource(root: string): RegistrySource {
  return parseJsonFile(root, IDENTITY_VERIFIER_SOURCE_PATH) as RegistrySource;
}

function validateAgainstSchema(root: string, schemaId: string, value: unknown): void {
  const ajv = new Ajv2020({ allErrors: true, strict: true });
  ajv.addKeyword({ keyword: "x-cloud-agents-security", schemaType: "object" });
  for (const path of [SOURCE_SCHEMA_PATH, OUTPUT_SCHEMA_PATH]) {
    ajv.addSchema(parseJsonFile(root, path));
  }
  const validate = ajv.getSchema(schemaId);
  if (!validate) throw new Error(`Identity verifier schema ${schemaId} was not registered.`);
  if (validate(value)) return;

  const errors = validate.errors ?? [];
  const boundary = errors.some((error) =>
    schemaErrorPath(error).startsWith("/profile/implementationNonClaims"),
  );
  throw contractError(
    boundary ? "IDENTITY_VERIFIER_BOUNDARY_MISMATCH" : "IDENTITY_VERIFIER_BINDING_MISMATCH",
    errors.length === 1 && errors[0] ? schemaErrorPath(errors[0]) : "/",
    `Identity verifier schema validation failed: ${ajv.errorsText(errors)}.`,
  );
}

function schemaErrorPath(error: ErrorObject): string {
  if (error.keyword === "required" && typeof error.params.missingProperty === "string") {
    return `${error.instancePath}/${escapePointerToken(error.params.missingProperty)}`;
  }
  if (
    error.keyword === "additionalProperties" &&
    typeof error.params.additionalProperty === "string"
  ) {
    return `${error.instancePath}/${escapePointerToken(error.params.additionalProperty)}`;
  }
  return error.instancePath || "/";
}

function canonicalEqual(left: unknown, right: unknown): boolean {
  const leftBytes = canonicalizeJson(left);
  const rightBytes = canonicalizeJson(right);
  return (
    leftBytes.byteLength === rightBytes.byteLength &&
    leftBytes.every((byte, index) => byte === rightBytes[index])
  );
}

function parseJsonFile(root: string, repositoryRelativePath: string): JsonRecord {
  const path = resolveContainedAuthorityPath(root, repositoryRelativePath, "file");
  return parseJsonText(decodeAuthorityUtf8(readFileSync(path), path), path);
}

function resolveContainedAuthorityPath(
  root: string,
  repositoryRelativePath: string,
  kind: "directory" | "file",
  allowMissingFile = false,
): string {
  const rootAbsolute = resolve(root);
  const rootReal = realpathSync(rootAbsolute);
  const lexicalCandidate = resolve(rootAbsolute, repositoryRelativePath);
  const lexicalRelative = relative(rootAbsolute, lexicalCandidate);
  if (
    lexicalRelative === "" ||
    lexicalRelative === ".." ||
    lexicalRelative.startsWith(`..${sep}`) ||
    isAbsolute(lexicalRelative)
  ) {
    throw new Error(`Authority path escapes repository root: ${repositoryRelativePath}.`);
  }

  const components = lexicalRelative.split(sep);
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
        kind === "file" &&
        allowMissingFile &&
        error instanceof Error &&
        "code" in error &&
        error.code === "ENOENT"
      ) {
        assertRealpathContained(
          rootReal,
          realpathSync(resolve(current, "..")),
          repositoryRelativePath,
        );
        return current;
      }
      throw error;
    }
    if (stat.isSymbolicLink()) {
      throw new Error(`Authority path contains a symbolic link: ${repositoryRelativePath}.`);
    }
    if (!final && !stat.isDirectory()) {
      throw new Error(
        `Authority path contains a non-directory component: ${repositoryRelativePath}.`,
      );
    }
    if (final) {
      if (kind === "file" && !stat.isFile()) {
        throw new Error(`Expected a regular authority file: ${repositoryRelativePath}.`);
      }
      if (kind === "directory" && !stat.isDirectory()) {
        throw new Error(`Expected an authority directory: ${repositoryRelativePath}.`);
      }
    }
  }
  assertRealpathContained(rootReal, realpathSync(current), repositoryRelativePath);
  return current;
}

function assertRealpathContained(rootReal: string, candidateReal: string, source: string): void {
  const contained = relative(rootReal, candidateReal);
  if (contained === ".." || contained.startsWith(`..${sep}`) || isAbsolute(contained)) {
    throw new Error(`Authority realpath escapes repository root: ${source}.`);
  }
}

function decodeAuthorityUtf8(bytes: Uint8Array, path: string): string {
  try {
    return FATAL_UTF8.decode(bytes);
  } catch {
    throw new Error(`Authority JSON is not valid UTF-8: ${path}.`);
  }
}

function parseJsonText(text: string, path: string): JsonRecord {
  new StrictJsonMemberScanner(text, path).scan();
  const parsed: unknown = JSON.parse(text);
  if (!isRecord(parsed)) throw new Error(`Expected JSON object: ${path}.`);
  canonicalizeJson(parsed);
  return parsed;
}

class StrictJsonMemberScanner {
  private offset = 0;

  constructor(
    private readonly text: string,
    private readonly path: string,
  ) {}

  scan(): void {
    this.skipWhitespace();
    this.scanValue();
    this.skipWhitespace();
    if (this.offset !== this.text.length) this.fail("trailing JSON input");
  }

  private scanValue(): void {
    const current = this.text[this.offset];
    if (current === "{") return this.scanObject();
    if (current === "[") return this.scanArray();
    if (current === '"') {
      this.scanString();
      return;
    }
    if (current === "t") return this.scanLiteral("true");
    if (current === "f") return this.scanLiteral("false");
    if (current === "n") return this.scanLiteral("null");
    if (current === "-" || (current !== undefined && current >= "0" && current <= "9")) {
      this.scanNumber();
      return;
    }
    this.fail("invalid JSON value");
  }

  private scanObject(): void {
    this.offset++;
    this.skipWhitespace();
    if (this.text[this.offset] === "}") {
      this.offset++;
      return;
    }

    const members = new Set<string>();
    for (;;) {
      if (this.text[this.offset] !== '"') this.fail("object member name must be a JSON string");
      const member = this.scanString();
      if (members.has(member)) {
        throw new Error(`Duplicate decoded JSON member ${JSON.stringify(member)} in ${this.path}.`);
      }
      members.add(member);
      this.skipWhitespace();
      if (this.text[this.offset] !== ":") this.fail("object member name must be followed by ':'");
      this.offset++;
      this.skipWhitespace();
      this.scanValue();
      this.skipWhitespace();
      const delimiter = this.text[this.offset];
      if (delimiter === "}") {
        this.offset++;
        return;
      }
      if (delimiter !== ",") this.fail("object members must be separated by ','");
      this.offset++;
      this.skipWhitespace();
    }
  }

  private scanArray(): void {
    this.offset++;
    this.skipWhitespace();
    if (this.text[this.offset] === "]") {
      this.offset++;
      return;
    }
    for (;;) {
      this.scanValue();
      this.skipWhitespace();
      const delimiter = this.text[this.offset];
      if (delimiter === "]") {
        this.offset++;
        return;
      }
      if (delimiter !== ",") this.fail("array values must be separated by ','");
      this.offset++;
      this.skipWhitespace();
    }
  }

  private scanString(): string {
    const start = this.offset;
    this.offset++;
    while (this.offset < this.text.length) {
      const current = this.text[this.offset];
      if (current === '"') {
        this.offset++;
        return JSON.parse(this.text.slice(start, this.offset)) as string;
      }
      if (current === "\\") {
        this.offset++;
        const escape = this.text[this.offset];
        if (escape === "u") {
          const digits = this.text.slice(this.offset + 1, this.offset + 5);
          if (!/^[0-9a-fA-F]{4}$/u.test(digits)) this.fail("invalid JSON unicode escape");
          this.offset += 5;
          continue;
        }
        if (escape === undefined || !'"\\/bfnrt'.includes(escape)) {
          this.fail("invalid JSON string escape");
        }
        this.offset++;
        continue;
      }
      if (current === undefined || current.charCodeAt(0) < 0x20) {
        this.fail("invalid control character in JSON string");
      }
      this.offset++;
    }
    this.fail("unterminated JSON string");
  }

  private scanLiteral(literal: string): void {
    if (!this.text.startsWith(literal, this.offset)) this.fail(`invalid JSON literal ${literal}`);
    this.offset += literal.length;
  }

  private scanNumber(): void {
    const start = this.offset;
    while (this.offset < this.text.length) {
      const current = this.text[this.offset];
      if (
        current === undefined ||
        current === "," ||
        current === "]" ||
        current === "}" ||
        current === " " ||
        current === "\t" ||
        current === "\n" ||
        current === "\r"
      ) {
        break;
      }
      this.offset++;
    }
    if (this.offset === start) this.fail("invalid JSON number");
  }

  private skipWhitespace(): void {
    while (
      this.text[this.offset] === " " ||
      this.text[this.offset] === "\t" ||
      this.text[this.offset] === "\n" ||
      this.text[this.offset] === "\r"
    ) {
      this.offset++;
    }
  }

  private fail(message: string): never {
    throw new Error(`${message} at source offset ${this.offset} in ${this.path}.`);
  }
}

function contractError(
  code: IdentityVerifierErrorCode,
  path: string,
  message: string,
): IdentityVerifierContractError {
  return new IdentityVerifierContractError(code, path, message);
}

function success(): IdentityVerifierSemanticResult {
  return { valid: true, errors: [] };
}

function failure(code: IdentityVerifierErrorCode, path: string): IdentityVerifierSemanticResult {
  return { valid: false, errors: [{ code, path }] };
}

function isRecord(value: unknown): value is JsonRecord {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}
