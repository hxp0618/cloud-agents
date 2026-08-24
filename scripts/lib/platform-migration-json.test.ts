import { createHash } from "node:crypto";
import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { describe, expect, it } from "vitest";

import {
  canonicalizeMigrationJson,
  deriveSignedInt64,
  migrationDigest,
  parseSignedInt64Decimal,
  parseStrictMigrationJson,
} from "./platform-migration-json";

const fixtureRoot = resolve(
  import.meta.dirname,
  "../../services/control-plane/migrations/fixtures/bundle",
);

describe("strict migration JSON and RFC 8785 profile", () => {
  it("canonicalizes reordered members without Unicode normalization", () => {
    const left = parseStrictMigrationJson(new TextEncoder().encode('{"z":0,"a":"é"}'));
    const right = parseStrictMigrationJson(new TextEncoder().encode('{"a":"é","z":0}'));
    expect(new TextDecoder().decode(canonicalizeMigrationJson(left))).toBe('{"a":"é","z":0}');
    expect(migrationDigest(left)).toBe(migrationDigest(right));
    expect(migrationDigest({ value: "é" })).not.toBe(migrationDigest({ value: "e\u0301" }));
  });

  it.each([
    ["negative/duplicate-key", "DUPLICATE_JSON_KEY"],
    ["negative/escaped-equivalent-key", "DUPLICATE_JSON_KEY"],
    ["negative/unicode-whitespace", "INVALID_JSON"],
  ])("rejects raw fixture %s", (name, code) => {
    const envelope = JSON.parse(
      readFileSync(resolve(fixtureRoot, `${name}.case.json`), "utf8"),
    ) as {
      raw_sha256: string;
      expected_error: string;
    };
    const raw = readFileSync(resolve(fixtureRoot, `${name}.raw`));
    expect(`sha256:${createHash("sha256").update(raw).digest("hex")}`).toBe(envelope.raw_sha256);
    expect(envelope.expected_error).toBe(code);
    expect(() => parseStrictMigrationJson(raw)).toThrow(code);
  });

  it.each(["-0", "-1", "1.0", "1e0", "01", "9007199254740992"])(
    "rejects forbidden numeric token %s",
    (token) => expect(() => parseStrictMigrationJson(new TextEncoder().encode(token))).toThrow(),
  );

  it("rejects invalid UTF-8 and lone surrogates", () => {
    expect(() =>
      parseStrictMigrationJson(Uint8Array.from([0x7b, 0x22, 0x78, 0x22, 0x3a, 0xff, 0x7d])),
    ).toThrow(/INVALID_UTF8/);
    expect(() => parseStrictMigrationJson(new TextEncoder().encode('{"x":"\\ud800"}'))).toThrow(
      /INVALID_UNICODE_SCALAR/,
    );
  });

  it("preserves magic object keys as null-prototype own members", () => {
    const value = parseStrictMigrationJson(
      new TextEncoder().encode(
        '{"__proto__":{"polluted":true},"constructor":1,"prototype":"kept"}',
      ),
    ) as Record<string, unknown>;
    expect(Object.getPrototypeOf(value)).toBeNull();
    expect(Object.hasOwn(value, "__proto__")).toBe(true);
    expect(Object.keys(value)).toEqual(["__proto__", "constructor", "prototype"]);
    expect(new TextDecoder().decode(canonicalizeMigrationJson(value as never))).toBe(
      '{"__proto__":{"polluted":true},"constructor":1,"prototype":"kept"}',
    );
    expect(() =>
      parseStrictMigrationJson(new TextEncoder().encode('{"__proto__":1,"__proto__":2}')),
    ).toThrow(/DUPLICATE_JSON_KEY/);
  });
});

describe("signed int64 advisory identity", () => {
  it("derives the ADR-0009 key", () => {
    expect(deriveSignedInt64("cloud-agents-platform:migrations:v1")).toBe(-1047838957622507638n);
  });

  it.each([
    ["-9223372036854775808", -(1n << 63n)],
    ["9223372036854775807", (1n << 63n) - 1n],
    ["0", 0n],
  ] as const)("accepts %s", (value, expected) =>
    expect(parseSignedInt64Decimal(value)).toBe(expected),
  );

  it.each(["-9223372036854775809", "9223372036854775808", "-0", "+1", "01", " 1"])(
    "rejects %s",
    (value) => expect(() => parseSignedInt64Decimal(value)).toThrow(),
  );
});
