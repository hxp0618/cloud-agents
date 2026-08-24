import { createHash } from "node:crypto";

export type MigrationJson =
  | null
  | boolean
  | number
  | string
  | MigrationJson[]
  | { [key: string]: MigrationJson };

export class MigrationValidationError extends Error {
  constructor(
    readonly code: string,
    message: string,
  ) {
    super(`${code}: ${message}`);
  }
}

const UTF8 = new TextEncoder();
const MAX_SAFE_INTEGER = 9_007_199_254_740_991;
const MIN_INT64 = -(1n << 63n);
const MAX_INT64 = (1n << 63n) - 1n;

export function parseStrictMigrationJson(bytes: Uint8Array): MigrationJson {
  let text: string;
  try {
    text = new TextDecoder("utf-8", { fatal: true }).decode(bytes);
  } catch {
    throw new MigrationValidationError("INVALID_UTF8", "input is not valid UTF-8");
  }
  if (text.startsWith("\ufeff")) throw new MigrationValidationError("JSON_BOM", "BOM is forbidden");
  return new StrictJsonParser(text).parse();
}

export function canonicalizeMigrationJson(value: MigrationJson): Uint8Array {
  return UTF8.encode(canonicalText(value));
}

export function migrationDigest(value: MigrationJson): string {
  return `sha256:${createHash("sha256").update(canonicalizeMigrationJson(value)).digest("hex")}`;
}

export function parseSignedInt64Decimal(value: string): bigint {
  if (!/^-?(?:0|[1-9][0-9]*)$/u.test(value) || value === "-0") {
    throw new MigrationValidationError(
      "INVALID_SIGNED_INT64",
      `invalid decimal ${JSON.stringify(value)}`,
    );
  }
  const parsed = BigInt(value);
  if (parsed < MIN_INT64 || parsed > MAX_INT64) {
    throw new MigrationValidationError("SIGNED_INT64_OUT_OF_RANGE", value);
  }
  return parsed;
}

export function deriveSignedInt64(domain: string): bigint {
  assertUnicodeScalars(domain);
  const digest = createHash("sha256").update(domain, "utf8").digest();
  let unsigned = 0n;
  for (const byte of digest.subarray(0, 8)) unsigned = (unsigned << 8n) | BigInt(byte);
  return BigInt.asIntN(64, unsigned);
}

function canonicalText(value: MigrationJson): string {
  if (value === null) return "null";
  if (typeof value === "boolean") return value ? "true" : "false";
  if (typeof value === "number") {
    if (!Number.isSafeInteger(value) || value < 0 || Object.is(value, -0)) {
      throw new MigrationValidationError("INVALID_JSON_NUMBER", String(value));
    }
    return String(value);
  }
  if (typeof value === "string") {
    assertUnicodeScalars(value);
    return JSON.stringify(value);
  }
  if (Array.isArray(value)) return `[${value.map(canonicalText).join(",")}]`;
  const members = Object.keys(value)
    .toSorted()
    .map((key) => {
      assertUnicodeScalars(key);
      return `${JSON.stringify(key)}:${canonicalText(value[key]!)}`;
    });
  return `{${members.join(",")}}`;
}

function assertUnicodeScalars(value: string): void {
  if (Buffer.byteLength(value, "utf8") > 1_048_576) {
    throw new MigrationValidationError("JSON_STRING_TOO_LARGE", "string exceeds 1 MiB");
  }
  for (let index = 0; index < value.length; index += 1) {
    const unit = value.charCodeAt(index);
    if (unit >= 0xd800 && unit <= 0xdbff) {
      const next = value.charCodeAt(index + 1);
      if (!(next >= 0xdc00 && next <= 0xdfff)) {
        throw new MigrationValidationError("INVALID_UNICODE_SCALAR", "lone high surrogate");
      }
      index += 1;
    } else if (unit >= 0xdc00 && unit <= 0xdfff) {
      throw new MigrationValidationError("INVALID_UNICODE_SCALAR", "lone low surrogate");
    }
  }
}

class StrictJsonParser {
  private offset = 0;

  constructor(private readonly source: string) {}

  parse(): MigrationJson {
    const value = this.value(0);
    this.space();
    if (this.offset !== this.source.length) this.fail("JSON_TRAILING_TOKEN", "trailing token");
    return value;
  }

  private value(depth: number): MigrationJson {
    if (depth > 64) this.fail("JSON_DEPTH_LIMIT", "nesting exceeds 64");
    this.space();
    const current = this.source[this.offset];
    if (current === "{") return this.object(depth + 1);
    if (current === "[") return this.array(depth + 1);
    if (current === '"') return this.string();
    if (this.source.startsWith("true", this.offset)) return this.literal("true", true);
    if (this.source.startsWith("false", this.offset)) return this.literal("false", false);
    if (this.source.startsWith("null", this.offset)) return this.literal("null", null);
    if (current === "-" || (current !== undefined && /[0-9]/u.test(current))) return this.number();
    this.fail("INVALID_JSON", `unexpected token at ${this.offset}`);
  }

  private object(depth: number): { [key: string]: MigrationJson } {
    this.offset += 1;
    const result = Object.create(null) as { [key: string]: MigrationJson };
    const keys = new Set<string>();
    this.space();
    if (this.source[this.offset] === "}") {
      this.offset += 1;
      return result;
    }
    for (let count = 0; ; count += 1) {
      if (count >= 16_384) this.fail("JSON_MEMBER_LIMIT", "too many object members");
      this.space();
      if (this.source[this.offset] !== '"') this.fail("INVALID_JSON", "object key must be string");
      const key = this.string();
      if (keys.has(key)) this.fail("DUPLICATE_JSON_KEY", key);
      keys.add(key);
      this.space();
      if (this.source[this.offset] !== ":") this.fail("INVALID_JSON", "missing colon");
      this.offset += 1;
      Object.defineProperty(result, key, {
        value: this.value(depth),
        enumerable: true,
        configurable: true,
        writable: true,
      });
      this.space();
      const delimiter = this.source[this.offset++];
      if (delimiter === "}") return result;
      if (delimiter !== ",") this.fail("INVALID_JSON", "missing object delimiter");
    }
  }

  private array(depth: number): MigrationJson[] {
    this.offset += 1;
    const result: MigrationJson[] = [];
    this.space();
    if (this.source[this.offset] === "]") {
      this.offset += 1;
      return result;
    }
    for (;;) {
      if (result.length >= 16_384) this.fail("JSON_MEMBER_LIMIT", "too many array entries");
      result.push(this.value(depth));
      this.space();
      const delimiter = this.source[this.offset++];
      if (delimiter === "]") return result;
      if (delimiter !== ",") this.fail("INVALID_JSON", "missing array delimiter");
    }
  }

  private string(): string {
    const start = this.offset;
    this.offset += 1;
    for (;;) {
      const current = this.source[this.offset];
      if (current === undefined) this.fail("INVALID_JSON", "unterminated string");
      if (current === '"') {
        this.offset += 1;
        let value: string;
        try {
          value = JSON.parse(this.source.slice(start, this.offset)) as string;
        } catch {
          this.fail("INVALID_JSON", "invalid string escape");
        }
        assertUnicodeScalars(value!);
        return value!;
      }
      if (current === "\\") {
        this.offset += 2;
        continue;
      }
      if (current.charCodeAt(0) < 0x20) this.fail("INVALID_JSON", "control character in string");
      this.offset += 1;
    }
  }

  private number(): number {
    const rest = this.source.slice(this.offset);
    const token = /^-?(?:0|[1-9][0-9]*)(?:\.[0-9]+)?(?:[eE][+-]?[0-9]+)?/u.exec(rest)?.[0];
    if (!token) this.fail("INVALID_JSON_NUMBER", "invalid numeric token");
    this.offset += token!.length;
    if (!/^(?:0|[1-9][0-9]*)$/u.test(token!)) {
      this.fail("INVALID_JSON_NUMBER", token!);
    }
    const value = Number(token);
    if (!Number.isSafeInteger(value) || value > MAX_SAFE_INTEGER) {
      this.fail("JSON_NUMBER_OUT_OF_RANGE", token!);
    }
    return value;
  }

  private literal<T extends MigrationJson>(token: string, value: T): T {
    this.offset += token.length;
    return value;
  }

  private space(): void {
    while (isJsonWhitespace(this.source.charCodeAt(this.offset))) this.offset += 1;
  }

  private fail(code: string, message: string): never {
    throw new MigrationValidationError(code, message);
  }
}

function isJsonWhitespace(unit: number): boolean {
  return unit === 0x20 || unit === 0x09 || unit === 0x0a || unit === 0x0d;
}
