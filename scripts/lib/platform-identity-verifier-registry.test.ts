import { createHash } from "node:crypto";
import {
  cpSync,
  lstatSync,
  mkdirSync,
  mkdtempSync,
  readFileSync,
  readdirSync,
  renameSync,
  rmSync,
  symlinkSync,
  writeFileSync,
} from "node:fs";
import { tmpdir } from "node:os";
import { join, resolve } from "node:path";

import { afterEach, describe, expect, it } from "vitest";

import { canonicalizeJson } from "./platform-json-semantics";
import {
  assertIdentityVerifierRegistryCurrent,
  buildIdentityVerifierRegistry,
  identityVerifierDomainDigest,
  identityVerifierRegistryInputs,
  IdentityVerifierContractError,
  IDENTITY_VERIFIER_FIXTURE_MANIFEST_PATH,
  IDENTITY_VERIFIER_OUTPUT_PATH,
  IDENTITY_VERIFIER_SOURCE_PATH,
  serializeIdentityVerifierRegistry,
  validateIdentityVerifierFixture,
  validateIdentityVerifierFixtureManifest,
  validateIdentityVerifierSource,
  writeIdentityVerifierRegistry,
} from "./platform-identity-verifier-registry";

type JsonRecord = Record<string, unknown>;

const repositoryRoot = resolve(import.meta.dirname, "../..");
const temporaryRoots: string[] = [];
const PROFILE_DOMAIN = "cloud-agents/platform-identity-verifier/profile/v1";
const REGISTRY_DOMAIN = "cloud-agents/platform-identity-verifier/registry/v1";
const PINNED_PROFILE_DIGEST =
  "sha256:d7da4c6be5048ec8e82e7ace4ef11dc39845843b3718b5e90e4babebd7091459";
const PINNED_REGISTRY_DIGEST =
  "sha256:ac468edeca5bc69b15a57a5d2def9d3c372f87a87423cc7922407da7e1aa8dea";

afterEach(() => {
  for (const root of temporaryRoots.splice(0)) rmSync(root, { force: true, recursive: true });
});

function temporaryContractRoot(): string {
  const root = mkdtempSync(join(tmpdir(), "identity-verifier-registry-"));
  temporaryRoots.push(root);
  mkdirSync(resolve(root, "tools"), { recursive: true });
  cpSync(
    resolve(repositoryRoot, "tools/platform-identity-verifier"),
    resolve(root, "tools/platform-identity-verifier"),
    { recursive: true },
  );
  return root;
}

function readJson(path: string): JsonRecord {
  return JSON.parse(readFileSync(path, "utf8")) as JsonRecord;
}

function readSource(root: string): JsonRecord {
  return readJson(resolve(root, IDENTITY_VERIFIER_SOURCE_PATH));
}

function writeSource(root: string, value: unknown): void {
  writeFileSync(
    resolve(root, IDENTITY_VERIFIER_SOURCE_PATH),
    `${JSON.stringify(value, null, 2)}\n`,
  );
}

function expectContractError(action: () => unknown, code: string): void {
  try {
    action();
    throw new Error(`Expected ${code}.`);
  } catch (error) {
    expect(error).toBeInstanceOf(IdentityVerifierContractError);
    expect((error as IdentityVerifierContractError).code).toBe(code);
  }
}

function exactDomainDigest(domain: string, value: unknown): string {
  const hash = createHash("sha256");
  hash.update(domain, "utf8");
  hash.update(Uint8Array.of(0));
  hash.update(canonicalizeJson(value));
  return `sha256:${hash.digest("hex")}`;
}

function generatedRegistryHashes(): Record<string, string> {
  const directory = resolve(repositoryRoot, "contracts/generated/platform/v1alpha1");
  return Object.fromEntries(
    readdirSync(directory)
      .filter((name) => name.endsWith(".json") && name !== "identity-verifier-registry-v1.json")
      .toSorted()
      .map((name) => [
        name,
        createHash("sha256")
          .update(readFileSync(resolve(directory, name)))
          .digest("hex"),
      ]),
  );
}

describe("platform identity verifier generated registry", () => {
  it("is byte-current, deterministic, and host independent", () => {
    expect(() => assertIdentityVerifierRegistryCurrent(repositoryRoot)).not.toThrow();
    const first = serializeIdentityVerifierRegistry(buildIdentityVerifierRegistry(repositoryRoot));
    const second = serializeIdentityVerifierRegistry(buildIdentityVerifierRegistry(repositoryRoot));
    expect(first).toBe(second);
    expect(first).not.toMatch(/generatedAt|generated_at|\/Users\//u);
  });

  it("generates the exact closed v1 profile and exact NUL-framed digests", () => {
    const source = readSource(repositoryRoot) as JsonRecord & { profile: JsonRecord };
    const generated = buildIdentityVerifierRegistry(repositoryRoot) as JsonRecord & {
      profile: JsonRecord & { profileDigest: string };
      registryDigest: string;
    };
    const { profileDigest, ...profileProjection } = generated.profile;
    const { registryDigest, ...registryProjection } = generated;

    expect(profileProjection).toEqual(source.profile);
    expect(generated.profile).toMatchObject({
      profileId: "platform-identity-verifier/v1",
      token: {
        acceptedTypes: ["application/at+jwt", "at+jwt"],
        canonicalType: "at+jwt",
        serialization: "compact_jws",
      },
      algorithm: { accepted: ["RS256"], selectionAuthority: "generated_profile_only" },
      claims: { audienceCardinality: "exactly_one_json_string" },
      lexicalRules: {
        subject:
          "non_empty_valid_utf8_exact_decoded_unicode_scalar_sequence_1..256_deliberately_c0_del_allowed",
      },
      parsingRules: {
        duplicateDecodedMembers: "reject_in_every_object_including_json_escape_equivalent_names",
        jsonEncoding: "valid_utf8_only",
        numericDates: "base10_json_integer_only_no_fraction_or_exponent",
        topLevel: "one_complete_json_object_no_trailing_input",
      },
      bindingRules: {
        audience: "token_aud_is_one_string_exactly_equal_to_snapshot_owned_resource_audience",
        issuer: "token_iss_exactly_equals_snapshot_issuer_decoded_unicode_scalar_sequence",
        permission: "context_owned_required_permission_is_present_in_token_scope_set",
        project:
          "project_bound_token_requires_exact_project_target_and_unbound_token_may_reach_narrower_project_only_subject_to_scope_and_rbac",
      },
      errorRules: {
        categories: [
          "audience_mismatch",
          "epoch_mismatch",
          "internal_failure",
          "invalid_signature",
          "issuer_mismatch",
          "malformed",
          "project_mismatch",
          "revoked_key",
          "revoked_token",
          "scope_mismatch",
          "tenant_mismatch",
          "time_invalid",
          "unknown_key",
          "unsupported_algorithm",
          "unsupported_profile",
        ],
        redactedFacts: [
          "jwk_private_material",
          "secret_bearing_request_headers",
          "signature_bytes",
          "token_bytes",
        ],
      },
      implementationNonClaims: {
        httpSurface: "implemented",
        providerSideEffects: "forbidden",
        productionDatabaseWrites: "not_authorized",
        gateStatus: "all_gates_open",
      },
    });
    expect(profileDigest).toBe(exactDomainDigest(PROFILE_DOMAIN, profileProjection));
    expect(profileDigest).toBe(identityVerifierDomainDigest(PROFILE_DOMAIN, profileProjection));
    expect(registryDigest).toBe(exactDomainDigest(REGISTRY_DOMAIN, registryProjection));
    const oldNewlineFraming = createHash("sha256")
      .update(`${PROFILE_DOMAIN}\n`, "utf8")
      .update(canonicalizeJson(profileProjection))
      .digest("hex");
    expect(profileDigest).not.toBe(`sha256:${oldNewlineFraming}`);
  });

  it("pins the immutable v1 profile and registry identities independently", () => {
    const generated = buildIdentityVerifierRegistry(repositoryRoot) as JsonRecord & {
      profile: { profileDigest: string };
      registryDigest: string;
    };
    expect(generated.profile.profileDigest).toBe(PINNED_PROFILE_DIGEST);
    expect(generated.registryDigest).toBe(PINNED_REGISTRY_DIGEST);
  });

  it("validates the closed independent fixture manifest and declared negatives", () => {
    expect(() => validateIdentityVerifierFixtureManifest(repositoryRoot)).not.toThrow();
    const manifest = readJson(resolve(repositoryRoot, IDENTITY_VERIFIER_FIXTURE_MANIFEST_PATH)) as {
      cases: Array<{ instance: string; expected: JsonRecord }>;
    };
    const fixtureRoot = resolve(repositoryRoot, "tools/platform-identity-verifier/v1/fixtures");
    for (const fixture of manifest.cases) {
      const result = validateIdentityVerifierFixture(
        readJson(resolve(fixtureRoot, fixture.instance)),
        repositoryRoot,
      );
      let actual: JsonRecord;
      if (result.valid) {
        actual = { valid: true };
      } else {
        expect(result.errors).toHaveLength(1);
        const first = result.errors[0];
        expect(first).toBeDefined();
        actual = { valid: false, code: first!.code, path: first!.path };
      }
      expect(actual).toEqual(fixture.expected);
    }
  });

  it("rejects profile, authority, boundary, collection, and digest mutations", () => {
    const invalidUtf8Root = temporaryContractRoot();
    const invalidUtf8Path = resolve(invalidUtf8Root, IDENTITY_VERIFIER_SOURCE_PATH);
    writeFileSync(
      invalidUtf8Path,
      Buffer.concat([Buffer.from([0xff]), readFileSync(invalidUtf8Path)]),
    );
    expect(() => buildIdentityVerifierRegistry(invalidUtf8Root)).toThrow(
      /Authority JSON is not valid UTF-8/u,
    );

    const loneSurrogateRoot = temporaryContractRoot();
    const loneSurrogatePath = resolve(loneSurrogateRoot, IDENTITY_VERIFIER_SOURCE_PATH);
    const loneSurrogateSource = readFileSync(loneSurrogatePath, "utf8").replace(
      "cloud-agents/platform/identity-verifier",
      "cloud-agents/platform/identity-verifier\\uD800",
    );
    writeFileSync(loneSurrogatePath, loneSurrogateSource);
    expect(() => buildIdentityVerifierRegistry(loneSurrogateRoot)).toThrow();

    const symlinkRoot = temporaryContractRoot();
    renameSync(resolve(symlinkRoot, "tools"), resolve(symlinkRoot, "real-tools"));
    symlinkSync("real-tools", resolve(symlinkRoot, "tools"), "dir");
    expect(() => buildIdentityVerifierRegistry(symlinkRoot)).toThrow(
      /Authority path contains a symbolic link/u,
    );

    const duplicateRoot = temporaryContractRoot();
    const duplicatePath = resolve(duplicateRoot, IDENTITY_VERIFIER_SOURCE_PATH);
    const duplicateSource = readFileSync(duplicatePath, "utf8").replace(
      '"formatVersion":',
      '"\\u0066ormatVersion": "cloud-agents-platform-identity-verifier-source/v1",\n  "formatVersion":',
    );
    writeFileSync(duplicatePath, duplicateSource);
    expect(() => buildIdentityVerifierRegistry(duplicateRoot)).toThrow(
      /Duplicate decoded JSON member "formatVersion"/u,
    );

    const parsingRoot = temporaryContractRoot();
    const parsing = readSource(parsingRoot);
    ((parsing.profile as JsonRecord).parsingRules as JsonRecord).numericDates =
      "floating_point_allowed";
    writeSource(parsingRoot, parsing);
    expectContractError(
      () => validateIdentityVerifierSource(parsingRoot, readSource(parsingRoot) as never),
      "IDENTITY_VERIFIER_BINDING_MISMATCH",
    );

    const bindingRoot = temporaryContractRoot();
    const binding = readSource(bindingRoot);
    ((binding.profile as JsonRecord).bindingRules as JsonRecord).inference =
      "infer_resource_hierarchy";
    writeSource(bindingRoot, binding);
    expectContractError(
      () => validateIdentityVerifierSource(bindingRoot, readSource(bindingRoot) as never),
      "IDENTITY_VERIFIER_BINDING_MISMATCH",
    );

    const errorRoot = temporaryContractRoot();
    const errors = readSource(errorRoot);
    (((errors.profile as JsonRecord).errorRules as JsonRecord).categories as string[]).pop();
    writeSource(errorRoot, errors);
    expectContractError(
      () => validateIdentityVerifierSource(errorRoot, readSource(errorRoot) as never),
      "IDENTITY_VERIFIER_BINDING_MISMATCH",
    );

    const algorithmRoot = temporaryContractRoot();
    const algorithm = readSource(algorithmRoot);
    ((algorithm.profile as JsonRecord).algorithm as JsonRecord).accepted = ["HS256"];
    writeSource(algorithmRoot, algorithm);
    expectContractError(
      () => validateIdentityVerifierSource(algorithmRoot, readSource(algorithmRoot) as never),
      "IDENTITY_VERIFIER_BINDING_MISMATCH",
    );

    const audienceRoot = temporaryContractRoot();
    const audience = readSource(audienceRoot);
    ((audience.profile as JsonRecord).limits as JsonRecord).audiences = 16;
    writeSource(audienceRoot, audience);
    expectContractError(
      () => validateIdentityVerifierSource(audienceRoot, readSource(audienceRoot) as never),
      "IDENTITY_VERIFIER_BINDING_MISMATCH",
    );

    const boundaryRoot = temporaryContractRoot();
    const boundary = readSource(boundaryRoot);
    ((boundary.profile as JsonRecord).implementationNonClaims as JsonRecord).productionTrustProvisioning =
      "not_implemented";
    writeSource(boundaryRoot, boundary);
    expectContractError(
      () => validateIdentityVerifierSource(boundaryRoot, readSource(boundaryRoot) as never),
      "IDENTITY_VERIFIER_BOUNDARY_MISMATCH",
    );

    const orderRoot = temporaryContractRoot();
    const order = readSource(orderRoot);
    ((order.profile as JsonRecord).protectedHeader as JsonRecord).forbiddenMembers = [
      "jku",
      "crit",
      "jwk",
      "x5c",
      "x5u",
    ];
    writeSource(orderRoot, order);
    expectContractError(
      () => validateIdentityVerifierSource(orderRoot, readSource(orderRoot) as never),
      "IDENTITY_VERIFIER_BINDING_MISMATCH",
    );

    const generated = buildIdentityVerifierRegistry(repositoryRoot) as JsonRecord & {
      profile: JsonRecord;
    };
    generated.profile.profileDigest = `sha256:${"0".repeat(64)}`;
    expect(validateIdentityVerifierFixture(generated, repositoryRoot)).toEqual({
      valid: false,
      errors: [{ code: "IDENTITY_VERIFIER_REGISTRY_DIGEST_MISMATCH", path: "/registryDigest" }],
    });
  });

  it("binds a sorted, unique, regular-file input set", () => {
    const inputs = identityVerifierRegistryInputs(repositoryRoot);
    expect(inputs).toEqual(inputs.toSorted());
    expect(new Set(inputs).size).toBe(inputs.length);
    expect(inputs).toContain(IDENTITY_VERIFIER_SOURCE_PATH);
    expect(inputs).toContain(IDENTITY_VERIFIER_FIXTURE_MANIFEST_PATH);
    expect(inputs).toContain("scripts/generate-platform-identity-verifier-registry.ts");
    expect(inputs).not.toContain(IDENTITY_VERIFIER_OUTPUT_PATH);
    for (const input of inputs) {
      const stat = lstatSync(resolve(repositoryRoot, input));
      expect(stat.isFile()).toBe(true);
      expect(stat.isSymbolicLink()).toBe(false);
    }
  });

  it("does not modify any previous generated platform registry output", () => {
    const before = generatedRegistryHashes();
    const isolatedRoot = temporaryContractRoot();
    mkdirSync(resolve(isolatedRoot, "contracts/generated/platform/v1alpha1"), { recursive: true });
    writeIdentityVerifierRegistry(isolatedRoot);
    const first = readFileSync(resolve(isolatedRoot, IDENTITY_VERIFIER_OUTPUT_PATH), "utf8");
    writeIdentityVerifierRegistry(isolatedRoot);
    expect(readFileSync(resolve(isolatedRoot, IDENTITY_VERIFIER_OUTPUT_PATH), "utf8")).toBe(first);
    assertIdentityVerifierRegistryCurrent(repositoryRoot);
    expect(generatedRegistryHashes()).toEqual(before);
  });
});
