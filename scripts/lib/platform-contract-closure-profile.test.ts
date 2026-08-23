import { copyFileSync, mkdirSync, mkdtempSync, readFileSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { dirname, resolve } from "node:path";

import { describe, expect, it } from "vitest";

import {
  assertContractClosureProfileRegistryCurrent,
  assertContractClosureProfileRegistryBytesCurrent,
  assertContractClosureProfileV1Immutable,
  buildContractClosureProfileRegistry,
  buildContractClosureProfileV1Registry,
  CONTRACT_CLOSURE_MISSING,
  CONTRACT_CLOSURE_PROFILE_SOURCE_PATH,
  CONTRACT_CLOSURE_SATISFIED_CANDIDATES,
  contractClosureProfileInputs,
  ContractClosureProfileError,
  deriveContractClosureMissing,
  serializeContractClosureProfileRegistry,
  validateContractClosureProfileSource,
} from "./platform-contract-closure-profile";

type JsonRecord = Record<string, unknown>;

const repositoryRoot = resolve(import.meta.dirname, "../..");
const immutableV1 = [
  "contracts/platform/v1alpha1/fixtures/golden/contract-closure-profile-source-v1.json",
  "contracts/platform/v1alpha1/schemas/contract-closure-profile-source-v1.schema.json",
  "contracts/platform/v1alpha1/schemas/contract-closure-profile-v1.schema.json",
  "contracts/generated/platform/v1alpha1/contract-closure-profile-v1.json",
] as const;

function readSource(): JsonRecord {
  return JSON.parse(
    readFileSync(resolve(repositoryRoot, CONTRACT_CLOSURE_PROFILE_SOURCE_PATH), "utf8"),
  ) as JsonRecord;
}

function clone<T>(value: T): T {
  return JSON.parse(JSON.stringify(value)) as T;
}

function expectSourceFailure(mutate: (source: JsonRecord) => void, pattern?: RegExp): void {
  const source = clone(readSource());
  mutate(source);
  const execute = (): void => validateContractClosureProfileSource(repositoryRoot, source as never);
  if (pattern) expect(execute).toThrow(pattern);
  else expect(execute).toThrow(ContractClosureProfileError);
}

describe("platform contract closure generated profile", () => {
  it("preserves v1 bytes and keeps both v1 and v2 registries deterministic/current", () => {
    expect(() => assertContractClosureProfileV1Immutable(repositoryRoot)).not.toThrow();
    expect(() => assertContractClosureProfileRegistryCurrent(repositoryRoot)).not.toThrow();
    const v1 = buildContractClosureProfileV1Registry(repositoryRoot) as JsonRecord & {
      missing: string[];
      profile: { spec: { profileId: string } };
    };
    expect(v1.profile.spec.profileId).toBe("contract-closure-profile/v1");
    expect(v1.missing).toEqual([
      "json-schema-2020-12-official-test-suite",
      ...CONTRACT_CLOSURE_MISSING,
    ]);

    const first = buildContractClosureProfileRegistry(repositoryRoot) as JsonRecord & {
      missing: string[];
      profile: { profileDigest: string; spec: JsonRecord };
      predecessor: JsonRecord;
      officialSuiteEvidence: JsonRecord;
    };
    const second = buildContractClosureProfileRegistry(repositoryRoot);
    expect(serializeContractClosureProfileRegistry(first)).toBe(
      serializeContractClosureProfileRegistry(second),
    );
    expect(first.missing).toEqual(CONTRACT_CLOSURE_MISSING);
    expect(first.profile.spec).toMatchObject({
      profileId: "contract-closure-profile/v2",
      status: "BOOTSTRAP_VALIDATED",
      notGateClosure: true,
      implementationBoundary: {
        officialAjvSuiteAudit: "executed_nonconformant",
        ajvGenericConformanceClaim: false,
        runtimeTrustAndHttp: "not_implemented",
        supplyScanner: "not_implemented",
        productionDatabaseWrites: "not_authorized",
        deployment: "not_authorized",
        publication: "not_authorized",
        gateStatus: "all_gates_open",
      },
    });
    expect(first.predecessor).toMatchObject({
      profileId: "contract-closure-profile/v1",
      predecessorMutation: "forbidden",
    });
    expect(first.officialSuiteEvidence).toMatchObject({
      independentOracle: { assertions: 1299, passedAssertions: 1299, failures: 0 },
      productionAjvAudit: {
        status: "EXECUTED_NONCONFORMANT",
        conformanceClaim: false,
        nonPassingAssertions: 58,
      },
      currentContractParity: { crossEngineExactFixtureResults: true },
    });
    expect(serializeContractClosureProfileRegistry(first)).not.toMatch(
      /generatedAt|generated_at|\/Users\//u,
    );
  });

  it("fails closed on actual mutation of any frozen v1 file", () => {
    const root = mkdtempSync(resolve(tmpdir(), "cloud-agents-contract-closure-v1-"));
    try {
      for (const path of immutableV1) {
        const target = resolve(root, path);
        mkdirSync(dirname(target), { recursive: true });
        copyFileSync(resolve(repositoryRoot, path), target);
      }
      expect(() => assertContractClosureProfileV1Immutable(root)).not.toThrow();
      writeFileSync(resolve(root, immutableV1[0]), "{}\n");
      expect(() => assertContractClosureProfileV1Immutable(root)).toThrow(/immutable file/u);
    } finally {
      rmSync(root, { force: true, recursive: true });
    }
  });

  it("rejects stale generated v2 bytes", () => {
    expect(() => assertContractClosureProfileRegistryBytesCurrent(repositoryRoot, "{}\n")).toThrow(
      /is stale/u,
    );
  });

  it("derives the active lock missing list from v2 and forbids manual removal", () => {
    const source = readSource() as JsonRecord & {
      profile: JsonRecord & { criteria: Array<JsonRecord & { id: string; status: string }> };
    };
    const satisfied = source.profile.criteria
      .filter((criterion) => criterion.status === "SATISFIED_CANDIDATE")
      .map((criterion) => criterion.id);
    expect(satisfied).toEqual(CONTRACT_CLOSURE_SATISFIED_CANDIDATES);
    expect(deriveContractClosureMissing(source.profile as never)).toEqual(CONTRACT_CLOSURE_MISSING);

    expectSourceFailure((candidate) => {
      const criteria = (candidate.profile as JsonRecord).criteria as JsonRecord[];
      criteria[5]!.status = "SATISFIED_CANDIDATE";
      delete criteria[5]!.reason;
      criteria[5]!.review = clone(criteria[4]!.review);
    });
  });

  it("rejects an Ajv conformance claim, changed oracle result, or false current parity", () => {
    expectSourceFailure((source) => {
      const audit = (source.officialSuiteEvidence as JsonRecord).productionAjvAudit as JsonRecord;
      audit.status = "CONFORMANT";
      audit.conformanceClaim = true;
    });
    expectSourceFailure((source) => {
      const oracle = (source.officialSuiteEvidence as JsonRecord).independentOracle as JsonRecord;
      oracle.assertions = 1298;
    });
    expectSourceFailure((source) => {
      const parity = (source.officialSuiteEvidence as JsonRecord)
        .currentContractParity as JsonRecord;
      parity.crossEngineExactFixtureResults = false;
    });
  });

  it("rejects bad B1 review digest and any change to prior review bindings", () => {
    expectSourceFailure((source) => {
      const criteria = (source.profile as JsonRecord).criteria as JsonRecord[];
      (criteria[0]!.review as JsonRecord).sha256 = `sha256:${"0".repeat(64)}`;
    }, /review identity or verdict drifted/u);
    expectSourceFailure((source) => {
      const criteria = (source.profile as JsonRecord).criteria as JsonRecord[];
      (criteria[1]!.evidencePaths as string[]).push("docs/plan/README.md");
    }, /retain each prior satisfied criterion/u);
  });

  it("binds every satisfied authority, evidence, and review path into sorted pipeline inputs", () => {
    const source = readSource() as JsonRecord & {
      profile: JsonRecord & {
        criteria: Array<{
          status: string;
          authorityPaths: string[];
          evidencePaths: string[];
          review?: { path: string };
        }>;
      };
    };
    const inputs = contractClosureProfileInputs(repositoryRoot);
    expect(inputs).toEqual(inputs.toSorted());
    expect(new Set(inputs).size).toBe(inputs.length);
    for (const criterion of source.profile.criteria.filter(
      (candidate) => candidate.status === "SATISFIED_CANDIDATE",
    )) {
      for (const path of [...criterion.authorityPaths, ...criterion.evidencePaths]) {
        expect(inputs.includes(path) || inputs.some((input) => input.startsWith(`${path}/`))).toBe(
          true,
        );
      }
      expect(inputs).toContain(criterion.review!.path);
    }
    for (const path of immutableV1) expect(inputs).toContain(path);
  });
});
