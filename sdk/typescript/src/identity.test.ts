import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { describe, expect, it } from "vitest";

import {
  canonicalizeNamespaceRef,
  canonicalizeSubjectRef,
  decodeNamespaceRefJSON,
  decodeSubjectRefJSON,
  IdentityValidationError,
  namespaceRefDigest,
  namespaceRefUrn,
  normalizeNamespaceRef,
  subjectRefDigest,
  validateNamespaceRef,
} from "./index";

type CanonicalFixture = {
  instance: Record<string, unknown>;
  canonicalUtf8: string;
  digest: `sha256:${string}`;
  urn?: `urn:cloud-agents:ref:sha256:${string}`;
};

type IdentityFixtureManifest = {
  cases: Array<{
    name: string;
    schema?: string;
    instance?: string;
    document?: string;
    expectedError?: string;
  }>;
};

type IdentityFixtureDocument = {
  instance?: Record<string, unknown>;
  canonicalUtf8?: string;
  candidateCanonicalUtf8?: string;
  digest?: `sha256:${string}`;
  urn?: `urn:cloud-agents:ref:sha256:${string}`;
};

const fixtureRoot = resolve(import.meta.dirname, "../../../contracts/common/v1alpha1/fixtures");
const textDecoder = new TextDecoder();

it("replays every versioned common identity fixture", async () => {
  const expected = new Set([
    "namespace-ref-canonical",
    "namespace-ref-nfc",
    "namespace-ref-extra-field",
    "namespace-ref-uppercase",
    "namespace-ref-decomposed",
    "namespace-ref-canonical-trailing-whitespace",
    "namespace-ref-canonical-escape",
    "namespace-ref-lone-surrogate",
    "subject-ref",
    "subject-ref-canonical",
    "subject-ref-canonical-escape",
    "subject-ref-digest-mismatch",
    "subject-ref-extra-field",
  ]);
  const manifest = readFixtureJSON<IdentityFixtureManifest>("manifest.json");
  const seen = new Set<string>();
  for (const fixture of manifest.cases) {
    if (
      fixture.schema !== "../schemas/namespace-ref.schema.json" &&
      fixture.schema !== "../schemas/subject-ref.schema.json"
    ) {
      continue;
    }
    expect(expected.has(fixture.name), fixture.name).toBe(true);
    expect(seen.has(fixture.name), fixture.name).toBe(false);
    seen.add(fixture.name);
    const { input, document } = loadIdentityFixture(fixture.instance, fixture.document);
    if (fixture.schema === "../schemas/namespace-ref.schema.json") {
      await replayNamespaceFixture(fixture.name, fixture.expectedError, input, document);
    } else {
      await replaySubjectFixture(fixture.name, fixture.expectedError, input, document);
    }
  }
  expect(seen).toEqual(expected);
});

describe("generated NamespaceRef identity", () => {
  it("replays canonical bytes, digest, and URN", async () => {
    const fixture = readFixture("golden/namespace-ref-canonical.json");
    const ref = decodeNamespaceRefJSON(JSON.stringify(fixture.instance));
    expect(textDecoder.decode(canonicalizeNamespaceRef(ref))).toBe(fixture.canonicalUtf8);
    expect(await namespaceRefDigest(ref)).toBe(fixture.digest);
    expect(await namespaceRefUrn(ref)).toBe(fixture.urn);
  });

  it("rejects unknown, duplicate, trailing, uppercase, decomposed, and lone-surrogate input", () => {
    const cases = [
      [
        `{"namespace":"cloud-agents","kind":"project","id":"alpha","extra":"secret"}`,
        "UNKNOWN_FIELD",
      ],
      [`{"namespace":"cloud-agents","kind":"project","id":"alpha","id":"beta"}`, "DUPLICATE_FIELD"],
      [`{"namespace":"cloud-agents","kind":"project"}`, "MISSING_FIELD"],
      [`{"namespace":"cloud-agents","kind":"project","id":1}`, "INVALID_FIELD_TYPE"],
      [`{"namespace":"cloud-agents","kind":"project","id":"alpha"}[]`, "TRAILING_JSON"],
      [
        `{"namespace":"Cloud-Agents","kind":"project","id":"alpha"}`,
        "INVALID_NAMESPACE_REF_GRAMMAR",
      ],
      [
        `{"namespace":"cloud-agents","kind":"project","id":"cafe\\u0301"}`,
        "NON_NFC_NAMESPACE_REF_ID",
      ],
      [`{"namespace":"cloud-agents","kind":"project","id":"\\ud800"}`, "INVALID_UNICODE_SCALAR"],
    ] as const;
    for (const [input, code] of cases) {
      expect(() => decodeNamespaceRefJSON(input)).toThrow(
        expect.objectContaining<Partial<IdentityValidationError>>({ code }),
      );
    }
  });

  it("normalizes only through the explicit NFC API", () => {
    const decomposed = { namespace: "cloud-agents", kind: "project", id: " cafe\u0301 " };
    expect(() => validateNamespaceRef(decomposed)).toThrow(
      expect.objectContaining<Partial<IdentityValidationError>>({
        code: "NON_NFC_NAMESPACE_REF_ID",
      }),
    );
    expect(normalizeNamespaceRef(decomposed)).toEqual({ ...decomposed, id: " café " });
  });

  it("counts Unicode scalar values and accepts a valid surrogate pair", () => {
    expect(() =>
      validateNamespaceRef({ namespace: "cloud-agents", kind: "project", id: "😀".repeat(256) }),
    ).not.toThrow();
    expect(() =>
      validateNamespaceRef({ namespace: "cloud-agents", kind: "project", id: "😀".repeat(257) }),
    ).toThrow(
      expect.objectContaining<Partial<IdentityValidationError>>({
        code: "INVALID_NAMESPACE_REF_LENGTH",
      }),
    );
    expect(
      decodeNamespaceRefJSON(`{"namespace":"cloud-agents","kind":"project","id":"\\ud83d\\ude00"}`)
        .id,
    ).toBe("😀");
  });
});

describe("generated SubjectRef identity", () => {
  it("replays exact issuer/subject bytes and digest", async () => {
    const fixture = readFixture("golden/subject-ref-canonical.json");
    const ref = decodeSubjectRefJSON(JSON.stringify(fixture.instance));
    expect(textDecoder.decode(canonicalizeSubjectRef(ref))).toBe(fixture.canonicalUtf8);
    expect(await subjectRefDigest(ref)).toBe(fixture.digest);
    expect(
      textDecoder.decode(canonicalizeSubjectRef({ ...ref, issuer: ref.issuer.toLowerCase() })),
    ).not.toBe(fixture.canonicalUtf8);
  });

  it.each([
    `{"kind":"admin","issuer":"https://issuer.example/","subject":"alpha"}`,
    `{"kind":"user","issuer":"relative","subject":"alpha"}`,
    `{"kind":"user","issuer":"https://issuer.example/%zz","subject":"alpha"}`,
    `{"kind":"user","issuer":"https://issuer.example/","subject":"alpha","extra":"value"}`,
  ])("rejects invalid strict input", (input) => {
    expect(() => decodeSubjectRefJSON(input)).toThrow(IdentityValidationError);
  });

  it("uses RFC 8785 string escaping for control characters", () => {
    expect(
      textDecoder.decode(
        canonicalizeSubjectRef({
          kind: "user",
          issuer: "https://identity.example.test/",
          subject: 'a\x00\b\t\n\f\r\x1f"\\中',
        }),
      ),
    ).toBe(
      `{"issuer":"https://identity.example.test/","kind":"user","subject":"a\\u0000\\b\\t\\n\\f\\r\\u001f\\"\\\\中"}`,
    );
  });
});

function readFixture(name: string): CanonicalFixture {
  return readFixtureJSON<CanonicalFixture>(name);
}

function readFixtureJSON<T>(name: string): T {
  return JSON.parse(readFileSync(resolve(fixtureRoot, name), "utf8")) as T;
}

function loadIdentityFixture(
  instance: string | undefined,
  document: string | undefined,
): { input: string; document: IdentityFixtureDocument } {
  if (document === undefined) {
    const value = readFixtureJSON<Record<string, unknown>>(instance!);
    return { input: JSON.stringify(value), document: {} };
  }
  const value = readFixtureJSON<IdentityFixtureDocument>(document);
  return { input: JSON.stringify(value.instance), document: value };
}

async function replayNamespaceFixture(
  name: string,
  expectedError: string | undefined,
  input: string,
  document: IdentityFixtureDocument,
): Promise<void> {
  if (expectedError === undefined) {
    const ref = decodeNamespaceRefJSON(input);
    if (document.canonicalUtf8 !== undefined) {
      expect(textDecoder.decode(canonicalizeNamespaceRef(ref)), name).toBe(document.canonicalUtf8);
    }
    if (document.digest !== undefined) {
      expect(await namespaceRefDigest(ref), name).toBe(document.digest);
    }
    if (document.urn !== undefined) {
      expect(await namespaceRefUrn(ref), name).toBe(document.urn);
    }
    return;
  }
  if (expectedError === "CANONICAL_NAMESPACE_REF_MISMATCH") {
    const ref = decodeNamespaceRefJSON(input);
    expect(textDecoder.decode(canonicalizeNamespaceRef(ref)), name).not.toBe(
      document.candidateCanonicalUtf8,
    );
    return;
  }
  expect(() => decodeNamespaceRefJSON(input), name).toThrow(
    expect.objectContaining({ code: expectedError }),
  );
}

async function replaySubjectFixture(
  name: string,
  expectedError: string | undefined,
  input: string,
  document: IdentityFixtureDocument,
): Promise<void> {
  if (expectedError === undefined) {
    const ref = decodeSubjectRefJSON(input);
    if (document.canonicalUtf8 !== undefined) {
      expect(textDecoder.decode(canonicalizeSubjectRef(ref)), name).toBe(document.canonicalUtf8);
    }
    if (document.digest !== undefined) {
      expect(await subjectRefDigest(ref), name).toBe(document.digest);
    }
    return;
  }
  if (expectedError === "CANONICAL_SUBJECT_REF_MISMATCH") {
    const ref = decodeSubjectRefJSON(input);
    expect(textDecoder.decode(canonicalizeSubjectRef(ref)), name).not.toBe(document.canonicalUtf8);
    return;
  }
  if (expectedError === "CANONICAL_SUBJECT_REF_DIGEST_MISMATCH") {
    const ref = decodeSubjectRefJSON(input);
    expect(await subjectRefDigest(ref), name).not.toBe(document.digest);
    return;
  }
  expect(() => decodeSubjectRefJSON(input), name).toThrow(
    expect.objectContaining({ code: expectedError }),
  );
}
