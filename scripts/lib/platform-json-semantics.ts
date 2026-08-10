import { createHash } from "node:crypto";

export type JsonRecord = Record<string, unknown>;

export type SemanticErrorCode =
  | "CANONICAL_NAMESPACE_REF_DIGEST_MISMATCH"
  | "CANONICAL_NAMESPACE_REF_MISMATCH"
  | "CROSS_TENANT_REFERENCE"
  | "INVALID_NAMESPACE_REF_GRAMMAR"
  | "INVALID_NAMESPACE_REF_KIND"
  | "INVALID_NAMESPACE_REF_LENGTH"
  | "INVALID_UNICODE_SCALAR"
  | "NAMESPACE_REF_MISMATCH"
  | "NON_NFC_NAMESPACE_REF_ID"
  | "ROLE_SCOPE_MISMATCH"
  | "SCOPE_KIND_MISMATCH"
  | "UNKNOWN_ROLE"
  | "WILDCARD_PERMISSION_FORBIDDEN";

export type SemanticError = {
  readonly code: SemanticErrorCode;
  readonly path: string;
};

export type SemanticResult =
  | { readonly valid: true; readonly errors: readonly [] }
  | { readonly valid: false; readonly errors: ReadonlyArray<SemanticError> };

export type NamespaceRef = {
  readonly namespace: string;
  readonly kind: string;
  readonly id: string;
};

const UTF8 = new TextEncoder();
const NAMESPACE_REF_KEYS = ["id", "kind", "namespace"] as const;
const NAMESPACE_REF_SEGMENT = /^[a-z][a-z0-9]*(?:[.-][a-z0-9]+)*$/u;
const KNOWN_ROLES = new Set([
  "platform.admin",
  "tenant.admin",
  "organization.admin",
  "project.admin",
  "project.operator",
  "project.developer",
  "project.viewer",
]);
const ROLE_SCOPE = new Map<string, string>([
  ["platform.admin", "platform"],
  ["tenant.admin", "tenant"],
  ["organization.admin", "organization"],
  ["project.admin", "project"],
  ["project.operator", "project"],
  ["project.developer", "project"],
  ["project.viewer", "project"],
]);

/**
 * RFC 8785/JCS canonicalization deliberately scoped to NamespaceRef.
 *
 * The contract has exactly three string properties, so number serialization,
 * array ordering, and generic object recursion are intentionally out of scope.
 * JSON.stringify supplies the ECMAScript string escaping required by JCS after
 * we reject lone surrogates, and the keys are ordered by UTF-16 code units.
 */
export function canonicalizeNamespaceRef(value: unknown): Uint8Array {
  const ref = requireNamespaceRef(value);
  for (const [name, text] of Object.entries(ref)) {
    assertUnicodeScalarString(text, `NamespaceRef.${name}`);
  }
  for (const name of ["namespace", "kind"] as const) {
    const text = ref[name];
    if (text.length < 1 || text.length > 63) {
      throw semanticFailure("INVALID_NAMESPACE_REF_LENGTH", `/${name}`);
    }
    if (!NAMESPACE_REF_SEGMENT.test(text)) {
      throw semanticFailure("INVALID_NAMESPACE_REF_GRAMMAR", `/${name}`);
    }
  }
  const idLength = [...ref.id].length;
  if (idLength < 1 || idLength > 256) {
    throw semanticFailure("INVALID_NAMESPACE_REF_LENGTH", "/id");
  }
  if (ref.id !== ref.id.normalize("NFC")) {
    throw semanticFailure("NON_NFC_NAMESPACE_REF_ID", "/id");
  }
  return UTF8.encode(
    `{"id":${JSON.stringify(ref.id)},"kind":${JSON.stringify(ref.kind)},"namespace":${JSON.stringify(ref.namespace)}}`,
  );
}

/** RFC 8785 subset used by checked-in fixtures and operation idempotency input. */
export function canonicalizeJson(value: unknown): Uint8Array {
  return UTF8.encode(canonicalJsonText(value));
}

export function canonicalJsonDigest(value: unknown): string {
  return `sha256:${createHash("sha256").update(canonicalizeJson(value)).digest("hex")}`;
}

export function namespaceRefDigest(value: unknown): string {
  return `sha256:${createHash("sha256").update(canonicalizeNamespaceRef(value)).digest("hex")}`;
}

export function namespaceRefUrn(value: unknown): string {
  return `urn:cloud-agents:ref:${namespaceRefDigest(value)}`;
}

export function sameNamespaceRef(left: unknown, right: unknown): boolean {
  return namespaceRefDigest(left) === namespaceRefDigest(right);
}

export function validateCanonicalNamespaceRefFixture(document: unknown): SemanticResult {
  if (!isRecord(document) || !isRecord(document.instance)) return success();
  const canonicalUtf8 = document.canonicalUtf8 ?? document.candidateCanonicalUtf8;
  const digest = document.digest;
  const urn = document.urn;
  if (typeof canonicalUtf8 !== "string") return success();

  try {
    const expectedBytes = canonicalizeNamespaceRef(document.instance);
    const suppliedBytes = UTF8.encode(canonicalUtf8);
    if (!bytesEqual(expectedBytes, suppliedBytes)) {
      return failure(
        "CANONICAL_NAMESPACE_REF_MISMATCH",
        document.canonicalUtf8 === undefined ? "/candidateCanonicalUtf8" : "/canonicalUtf8",
      );
    }
    const expectedDigest = namespaceRefDigest(document.instance);
    if (digest !== expectedDigest || urn !== `urn:cloud-agents:ref:${expectedDigest}`) {
      return failure("CANONICAL_NAMESPACE_REF_DIGEST_MISMATCH", "/digest");
    }
    return success();
  } catch (error) {
    if (error instanceof PlatformSemanticFailure) {
      return failure(error.code, `/instance${error.path}`);
    }
    throw error;
  }
}

/** Validates cross-field platform invariants that JSON Schema cannot express. */
export function validatePlatformSemantics(
  instance: unknown,
  document: unknown = instance,
): SemanticResult {
  const errors: SemanticError[] = [];
  collectNamespaceRefErrors(instance, "", errors);

  if (!isRecord(instance)) return finish(errors);
  if (
    typeof instance.level === "string" &&
    (instance.ref !== undefined || instance.level === "platform")
  ) {
    validateScope(instance, "", errors);
  }
  const metadata = optionalRecord(instance.metadata);
  const spec = optionalRecord(instance.spec);
  const kind = typeof instance.kind === "string" ? instance.kind : undefined;

  if (metadata && spec && metadata.tenantRef !== undefined && spec.tenantRef !== undefined) {
    compareRefs(
      metadata.tenantRef,
      spec.tenantRef,
      "/spec/tenantRef",
      "NAMESPACE_REF_MISMATCH",
      errors,
    );
  }

  enforceExpectedKind(metadata?.tenantRef, "tenant", "/metadata/tenantRef", errors);
  enforceExpectedKind(spec?.tenantRef, "tenant", "/spec/tenantRef", errors);
  if (kind === "Organization")
    enforceExpectedKind(spec?.tenantRef, "tenant", "/spec/tenantRef", errors);
  if (kind === "Project") {
    enforceExpectedKind(spec?.organizationRef, "organization", "/spec/organizationRef", errors);
  }

  if (kind === "PlatformTenant" && metadata) {
    const tenantRef = optionalRecord(metadata.tenantRef);
    if (tenantRef && typeof metadata.uid === "string" && tenantRef.id !== metadata.uid) {
      errors.push({ code: "NAMESPACE_REF_MISMATCH", path: "/metadata/tenantRef/id" });
    }
  }

  const scope = optionalRecord(spec?.scope);
  if (scope) validateScope(scope, "/spec/scope", errors);

  const roleName = spec?.roleName ?? (kind === "Role" ? spec?.name : undefined);
  if (typeof roleName === "string") {
    if (!KNOWN_ROLES.has(roleName)) errors.push({ code: "UNKNOWN_ROLE", path: "/spec/roleName" });
    if (scope) {
      const expectedLevel = ROLE_SCOPE.get(roleName);
      if (expectedLevel && scope.level !== expectedLevel) {
        errors.push({ code: "ROLE_SCOPE_MISMATCH", path: "/spec/scope/level" });
      }
    }
  }

  if (Array.isArray(spec?.permissions)) {
    spec.permissions.forEach((permission, index) => {
      if (typeof permission === "string" && permission.includes("*")) {
        errors.push({
          code: "WILDCARD_PERMISSION_FORBIDDEN",
          path: `/spec/permissions/${index}`,
        });
      }
    });
  }

  const resolvedReferences = isRecord(document)
    ? optionalRecord(document.resolvedReferences)
    : undefined;
  if (resolvedReferences && spec) {
    const organizationRef = optionalRecord(spec.organizationRef);
    const organizationId = organizationRef?.id;
    const target =
      typeof organizationId === "string"
        ? optionalRecord(resolvedReferences[organizationId])
        : undefined;
    if (target?.tenantRef !== undefined && spec.tenantRef !== undefined) {
      compareRefs(
        spec.tenantRef,
        target.tenantRef,
        "/spec/organizationRef",
        "CROSS_TENANT_REFERENCE",
        errors,
      );
    }
  }

  return finish(deduplicate(errors));
}

export function assertExpectedSemanticResult(
  result: SemanticResult,
  expectedValid: boolean,
  expectedError?: unknown,
): void {
  if (result.valid !== expectedValid) {
    throw new Error(
      `Expected semantic valid=${String(expectedValid)}, got ${String(result.valid)}.`,
    );
  }
  if (!expectedValid) {
    if (typeof expectedError !== "string" || expectedError.length === 0) {
      throw new Error("Invalid semantic fixture must declare expectedError.");
    }
    if (result.valid || result.errors[0]?.code !== expectedError) {
      const actual = result.valid ? "VALID" : (result.errors[0]?.code ?? "NO_ERROR");
      throw new Error(`Expected semantic error ${expectedError}, got ${actual}.`);
    }
  }
}

function requireNamespaceRef(value: unknown): NamespaceRef {
  if (
    !isRecord(value) ||
    Object.keys(value).toSorted().join("\0") !== NAMESPACE_REF_KEYS.join("\0")
  ) {
    throw new TypeError("NamespaceRef must contain exactly id, kind, and namespace.");
  }
  if (
    typeof value.id !== "string" ||
    typeof value.kind !== "string" ||
    typeof value.namespace !== "string"
  ) {
    throw new TypeError("NamespaceRef properties must be strings.");
  }
  return { id: value.id, kind: value.kind, namespace: value.namespace };
}

function assertUnicodeScalarString(value: string, label: string): void {
  for (let index = 0; index < value.length; index += 1) {
    const unit = value.charCodeAt(index);
    if (unit >= 0xd800 && unit <= 0xdbff) {
      const next = value.charCodeAt(index + 1);
      if (!(next >= 0xdc00 && next <= 0xdfff)) {
        throw semanticFailure("INVALID_UNICODE_SCALAR", `/${label.split(".").at(-1)}`);
      }
      index += 1;
    } else if (unit >= 0xdc00 && unit <= 0xdfff) {
      throw semanticFailure("INVALID_UNICODE_SCALAR", `/${label.split(".").at(-1)}`);
    }
  }
}

function canonicalJsonText(value: unknown): string {
  if (value === null) return "null";
  if (typeof value === "boolean") return value ? "true" : "false";
  if (typeof value === "string") {
    assertUnicodeScalarString(value, "JSON string");
    return JSON.stringify(value);
  }
  if (typeof value === "number") {
    if (!Number.isFinite(value)) throw new TypeError("JCS numbers must be finite.");
    return JSON.stringify(value);
  }
  if (Array.isArray(value)) return `[${value.map((entry) => canonicalJsonText(entry)).join(",")}]`;
  if (!isRecord(value)) throw new TypeError(`Unsupported JCS value type ${typeof value}.`);
  const entries = Object.keys(value)
    .toSorted()
    .map((key) => {
      assertUnicodeScalarString(key, "JSON property");
      const entry = value[key];
      if (entry === undefined) throw new TypeError(`JCS object property ${key} is undefined.`);
      return `${JSON.stringify(key)}:${canonicalJsonText(entry)}`;
    });
  return `{${entries.join(",")}}`;
}

function collectNamespaceRefErrors(value: unknown, path: string, errors: SemanticError[]): void {
  if (Array.isArray(value)) {
    value.forEach((entry, index) => collectNamespaceRefErrors(entry, `${path}/${index}`, errors));
    return;
  }
  if (!isRecord(value)) return;
  if (
    Object.hasOwn(value, "namespace") &&
    Object.hasOwn(value, "kind") &&
    Object.hasOwn(value, "id") &&
    typeof value.namespace === "string" &&
    typeof value.kind === "string" &&
    typeof value.id === "string"
  ) {
    try {
      canonicalizeNamespaceRef(value);
    } catch (error) {
      if (error instanceof PlatformSemanticFailure) {
        errors.push({ code: error.code, path: `${path}${error.path}` });
      } else {
        throw error;
      }
    }
  }
  for (const [key, child] of Object.entries(value)) {
    collectNamespaceRefErrors(child, `${path}/${escapePointer(key)}`, errors);
  }
}

function compareRefs(
  left: unknown,
  right: unknown,
  path: string,
  code: "CROSS_TENANT_REFERENCE" | "NAMESPACE_REF_MISMATCH",
  errors: SemanticError[],
): void {
  try {
    if (!sameNamespaceRef(left, right)) errors.push({ code, path });
  } catch (error) {
    if (!(error instanceof PlatformSemanticFailure)) throw error;
  }
}

function enforceExpectedKind(
  value: unknown,
  expected: string,
  path: string,
  errors: SemanticError[],
): void {
  const ref = optionalRecord(value);
  if (ref && typeof ref.kind === "string" && ref.kind !== expected) {
    errors.push({ code: "INVALID_NAMESPACE_REF_KIND", path: `${path}/kind` });
  }
}

function validateScope(scope: JsonRecord, path: string, errors: SemanticError[]): void {
  const level = scope.level;
  if (level === "platform") return;
  if (typeof level !== "string") return;
  const ref = optionalRecord(scope.ref);
  if (ref && typeof ref.kind === "string" && ref.kind !== level) {
    errors.push({ code: "SCOPE_KIND_MISMATCH", path: `${path}/ref/kind` });
  }
}

function deduplicate(errors: ReadonlyArray<SemanticError>): ReadonlyArray<SemanticError> {
  const seen = new Set<string>();
  return errors.filter((error) => {
    const key = `${error.code}\0${error.path}`;
    if (seen.has(key)) return false;
    seen.add(key);
    return true;
  });
}

function bytesEqual(left: Uint8Array, right: Uint8Array): boolean {
  return left.length === right.length && left.every((byte, index) => byte === right[index]);
}

function finish(errors: ReadonlyArray<SemanticError>): SemanticResult {
  return errors.length === 0 ? success() : { valid: false, errors };
}

function success(): SemanticResult {
  return { valid: true, errors: [] };
}

function failure(code: SemanticErrorCode, path: string): SemanticResult {
  return { valid: false, errors: [{ code, path }] };
}

function optionalRecord(value: unknown): JsonRecord | undefined {
  return isRecord(value) ? value : undefined;
}

function isRecord(value: unknown): value is JsonRecord {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}

function escapePointer(value: string): string {
  return value.replaceAll("~", "~0").replaceAll("/", "~1");
}

class PlatformSemanticFailure extends Error {
  constructor(
    readonly code: SemanticErrorCode,
    readonly path: string,
  ) {
    super(`${code} at ${path}`);
  }
}

function semanticFailure(code: SemanticErrorCode, path: string): PlatformSemanticFailure {
  return new PlatformSemanticFailure(code, path);
}
