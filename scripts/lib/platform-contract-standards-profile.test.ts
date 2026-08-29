import {
  cpSync,
  mkdirSync,
  mkdtempSync,
  readFileSync,
  rmSync,
  symlinkSync,
  unlinkSync,
  writeFileSync,
} from "node:fs";
import { tmpdir } from "node:os";
import { dirname, resolve } from "node:path";

import { afterEach, describe, expect, it } from "vitest";

import {
  assertContractStandardsProfileCurrent,
  assertContractStandardsProfileV1Immutable,
  assertContractStandardsProfileV2Immutable,
  CONTRACT_STANDARDS_PROFILE_V1_IMMUTABLE,
  CONTRACT_STANDARDS_PROFILE_V1_PATH,
  CONTRACT_STANDARDS_PROFILE_V2_PATH,
  CONTRACT_STANDARDS_PROFILE_V3_PATH,
  contractStandardsCorpusInputs,
  readContractStandardsProfile,
  validateContractStandardsProfile,
} from "./platform-contract-standards-profile";

type JsonObject = Record<string, unknown>;

const repositoryRoot = resolve(import.meta.dirname, "../..");
const temporaryRoots: string[] = [];

afterEach(() => {
  for (const root of temporaryRoots.splice(0)) rmSync(root, { force: true, recursive: true });
});

function temporaryCurrentRoot(): string {
  const root = mkdtempSync(resolve(tmpdir(), "platform-contract-standards-v3-"));
  temporaryRoots.push(root);
  for (const path of ["contracts", "tools/contract-standards/vendor/json-schema-test-suite"]) {
    cpSync(resolve(repositoryRoot, path), resolve(root, path), { recursive: true });
  }
  for (const path of [
    CONTRACT_STANDARDS_PROFILE_V1_PATH,
    CONTRACT_STANDARDS_PROFILE_V2_PATH,
    CONTRACT_STANDARDS_PROFILE_V3_PATH,
    "tools/contract-standards/pyproject.toml",
    "tools/contract-standards/uv.lock",
  ]) {
    const target = resolve(root, path);
    mkdirSync(dirname(target), { recursive: true });
    cpSync(resolve(repositoryRoot, path), target);
  }

  return root;
}

describe("versioned contract-standards profile", () => {
  it("keeps v1/v2 authority bytes immutable and validates the v3 current source", () => {
    expect(
      readFileSync(resolve(repositoryRoot, CONTRACT_STANDARDS_PROFILE_V1_PATH)).byteLength,
    ).toBe(CONTRACT_STANDARDS_PROFILE_V1_IMMUTABLE.sizeBytes);
    expect(() => assertContractStandardsProfileV1Immutable(repositoryRoot)).not.toThrow();
    expect(() => assertContractStandardsProfileV2Immutable(repositoryRoot)).not.toThrow();

    const v1 = readContractStandardsProfile(repositoryRoot, CONTRACT_STANDARDS_PROFILE_V1_PATH);
    expect(v1.formatVersion).toBe("cloud-agents-contract-standards-profile/v1");
    expect(v1.currentContracts).toMatchObject({ schemaFiles: 58, fixtureCases: 77 });

    const v3 = readContractStandardsProfile(repositoryRoot, CONTRACT_STANDARDS_PROFILE_V3_PATH);
    expect(v3.formatVersion).toBe("cloud-agents-contract-standards-profile/v3");
    expect(v3.currentContracts).toMatchObject({
      schemaFiles: 87,
      fixtureCases: 79,
      bootstrapContracts: { schemaFiles: 83, fixtureCases: 79 },
    });

    const root = temporaryCurrentRoot();
    expect(() => assertContractStandardsProfileCurrent(root)).not.toThrow();
    const profilePath = resolve(root, CONTRACT_STANDARDS_PROFILE_V3_PATH);
    const profile = JSON.parse(readFileSync(profilePath, "utf8")) as JsonObject;
    (profile.currentContracts as JsonObject).schemaFiles = 67;
    writeFileSync(profilePath, `${JSON.stringify(profile)}\n`);
    expect(() => assertContractStandardsProfileCurrent(root)).toThrow(
      /Contract-standards profile cardinalities or cross-engine boundary drifted/u,
    );
  });

  it("fails closed on predecessor, topology, count, and boundary drift", () => {
    const profile = JSON.parse(
      readFileSync(resolve(repositoryRoot, CONTRACT_STANDARDS_PROFILE_V2_PATH), "utf8"),
    ) as JsonObject;

    for (const mutate of [
      (candidate: JsonObject): void => {
        (candidate.predecessor as JsonObject).sizeBytes = 1;
      },
      (candidate: JsonObject): void => {
        candidate.unexpected = true;
      },
      (candidate: JsonObject): void => {
        (candidate.currentContracts as JsonObject).fixtureCases = 77;
      },
      (candidate: JsonObject): void => {
        (candidate.currentContracts as JsonObject).sourceContractManifestSha256 =
          `sha256:${"0".repeat(64)}`;
      },
      (candidate: JsonObject): void => {
        (candidate.implementationBoundary as JsonObject).gateStatus = "CLOSED";
      },
    ]) {
      const candidate = structuredClone(profile);
      mutate(candidate);
      expect(() => validateContractStandardsProfile(candidate)).toThrow();
    }

    const v3 = JSON.parse(
      readFileSync(resolve(repositoryRoot, CONTRACT_STANDARDS_PROFILE_V3_PATH), "utf8"),
    ) as JsonObject;
    (v3.predecessor as JsonObject).sha256 = "0".repeat(64);
    expect(() => validateContractStandardsProfile(v3)).toThrow(/predecessor/u);
  });

  it("rejects v1 byte mutation and symlink substitution", () => {
    const root = temporaryCurrentRoot();
    const path = resolve(root, CONTRACT_STANDARDS_PROFILE_V1_PATH);
    writeFileSync(path, "{}\n");
    expect(() => assertContractStandardsProfileV1Immutable(root)).toThrow(/immutable/u);

    unlinkSync(path);
    symlinkSync(resolve(repositoryRoot, CONTRACT_STANDARDS_PROFILE_V1_PATH), path);
    expect(() => assertContractStandardsProfileV1Immutable(root)).toThrow(/regular file/u);
  });

  it("binds the exact corpus topology without including projection outputs", () => {
    const inputs = contractStandardsCorpusInputs(repositoryRoot);
    expect(inputs).toHaveLength(126);
    expect(inputs).toEqual(inputs.toSorted());
    expect(new Set(inputs).size).toBe(inputs.length);
    expect(inputs).toContain(
      "tools/contract-standards/vendor/json-schema-test-suite/tests/draft2020-12/ref.json",
    );
    expect(inputs).not.toContain("contracts/generation.lock.json");
  });
});
