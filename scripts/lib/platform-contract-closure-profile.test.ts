import { readFileSync } from "node:fs";
import { resolve } from "node:path";

import { describe, expect, it } from "vitest";

import {
  assertContractClosureProfileRegistryCurrent,
  buildContractClosureProfileRegistry,
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

function readSource(): JsonRecord {
  return JSON.parse(
    readFileSync(resolve(repositoryRoot, CONTRACT_CLOSURE_PROFILE_SOURCE_PATH), "utf8"),
  ) as JsonRecord;
}

function clone<T>(value: T): T {
  return JSON.parse(JSON.stringify(value)) as T;
}

describe("platform contract closure generated profile", () => {
  it("is byte-current, deterministic, and retains the exact three missing criteria", () => {
    expect(() => assertContractClosureProfileRegistryCurrent(repositoryRoot)).not.toThrow();
    const first = buildContractClosureProfileRegistry(repositoryRoot) as JsonRecord & {
      missing: string[];
      profile: { profileDigest: string; spec: JsonRecord };
      registryDigest: string;
    };
    const second = buildContractClosureProfileRegistry(repositoryRoot);

    expect(serializeContractClosureProfileRegistry(first)).toBe(
      serializeContractClosureProfileRegistry(second),
    );
    expect(first.missing).toEqual(CONTRACT_CLOSURE_MISSING);
    expect(first.profile.profileDigest).toBe(
      "sha256:08746a7986a583c550eecb5ef5d7ab15f08511fe934a4c176d2f087bc449a715",
    );
    expect(first.registryDigest).toBe(
      "sha256:c01ef40f826bbec63ede5f43b49dd3e2c4bd5d4c1cd2744d03cf28784a6cf5bd",
    );
    expect(first.profile.spec).toMatchObject({
      profileId: "contract-closure-profile/v1",
      status: "BOOTSTRAP_VALIDATED",
      notGateClosure: true,
      implementationBoundary: {
        officialAjvSuiteRunner: "not_implemented",
        runtimeTrustAndHttp: "not_implemented",
        supplyScanner: "not_implemented",
        productionDatabaseWrites: "not_authorized",
        deployment: "not_authorized",
        publication: "not_authorized",
        gateStatus: "all_gates_open",
      },
    });
    expect(serializeContractClosureProfileRegistry(first)).not.toMatch(
      /generatedAt|generated_at|\/Users\//u,
    );
  });

  it("derives missing from the versioned source rather than a handwritten lock list", () => {
    const source = readSource() as JsonRecord & {
      profile: JsonRecord & {
        criteria: Array<JsonRecord & { id: string; status: string }>;
      };
    };
    const satisfied = source.profile.criteria
      .filter((criterion) => criterion.status === "SATISFIED_CANDIDATE")
      .map((criterion) => criterion.id);
    expect(satisfied).toEqual(CONTRACT_CLOSURE_SATISFIED_CANDIDATES);
    expect(deriveContractClosureMissing(source.profile as never)).toEqual(CONTRACT_CLOSURE_MISSING);
  });

  it("fails closed if v1 tries to satisfy another criterion or loses a review digest", () => {
    const expanded = clone(readSource()) as JsonRecord & {
      profile: JsonRecord & { criteria: Array<JsonRecord> };
    };
    expanded.profile.criteria[0]!.status = "SATISFIED_CANDIDATE";
    delete expanded.profile.criteria[0]!.reason;
    expanded.profile.criteria[0]!.review = clone(expanded.profile.criteria[1]!.review);
    expect(() => validateContractClosureProfileSource(repositoryRoot, expanded as never)).toThrow(
      ContractClosureProfileError,
    );

    const badReview = clone(readSource()) as JsonRecord & {
      profile: JsonRecord & { criteria: Array<JsonRecord> };
    };
    (badReview.profile.criteria[1]!.review as JsonRecord).sha256 = `sha256:${"0".repeat(64)}`;
    expect(() => validateContractClosureProfileSource(repositoryRoot, badReview as never)).toThrow(
      /review identity or verdict drifted/u,
    );
  });

  it("binds every satisfied authority, evidence, and review path into pipeline inputs", () => {
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
        expect(inputs).toContain(path);
      }
      expect(inputs).toContain(criterion.review!.path);
    }
  });
});
