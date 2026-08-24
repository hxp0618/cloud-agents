import { createHash } from "node:crypto";
import { execFileSync } from "node:child_process";
import { cpSync, mkdirSync, mkdtempSync, readFileSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { dirname, join, resolve } from "node:path";

import { afterEach, describe, expect, it } from "vitest";

import { canonicalizeJson } from "./platform-json-semantics";
import {
  buildContractClosureProfileV3Registry,
  buildContractClosureProfileV3TestSource,
  CONTRACT_CLOSURE_PROFILE_V3_OUTPUT_PATH,
  CONTRACT_CLOSURE_PROFILE_V3_OUTPUT_SCHEMA_PATH,
  CONTRACT_CLOSURE_PROFILE_V3_SOURCE_PATH,
  CONTRACT_CLOSURE_PROFILE_V3_SOURCE_SCHEMA_PATH,
  CONTRACT_CLOSURE_V3_RUNTIME_FILES,
  CONTRACT_CLOSURE_V3_RUNTIME_REVIEW_FILE,
} from "./platform-contract-closure-profile-v3";
import {
  assertContractReviewBindingCurrentOrAbsent,
  buildContractReviewBindingTestSource,
  buildContractReviewBindingTestTuple,
  contractReviewBindingAuthorityInputs,
  CONTRACT_REVIEW_BINDING_FINAL_REVIEW_PATH,
  CONTRACT_REVIEW_BINDING_OUTPUT_PATH,
  CONTRACT_REVIEW_BINDING_REGISTRY_SCHEMA_PATH,
  CONTRACT_REVIEW_BINDING_SOURCE_PATH,
  CONTRACT_REVIEW_BINDING_SOURCE_SCHEMA_PATH,
  CONTRACT_REVIEW_TUPLE_PATH,
  CONTRACT_REVIEW_TUPLE_SCHEMA_PATH,
  inspectContractReviewBindingState,
  publishContractReviewBindingExclusiveForTest,
  serializeContractReviewBinding,
  writeContractReviewBinding,
  type ContractReviewBindingSource,
  type ContractReviewTuple,
} from "./platform-contract-review-binding";
import {
  buildGeneratorSupplyV2TestSource,
  GENERATOR_SUPPLY_V2_EVIDENCE_MANIFEST_PATH,
  GENERATOR_SUPPLY_V2_OUTPUT_PATH,
  GENERATOR_SUPPLY_V2_OUTPUT_SCHEMA_PATH,
  GENERATOR_SUPPLY_V2_REPLAY_CONTRACT,
  GENERATOR_SUPPLY_V2_SOURCE_PATH,
  GENERATOR_SUPPLY_V2_SOURCE_SCHEMA_PATH,
  type GeneratorSupplyV2Registry,
  type GeneratorSupplyV2Source,
} from "./platform-generator-supply-profile-v2";
import { buildGeneratorSupplyReplayV2TestFixture } from "./platform-generator-supply-replay-v2";
import { SUCCESSOR_REPLAY_RECEIPT_PATHS } from "./platform-successor-dag";
import {
  CONTRACT_CLOSURE_V1_IMMUTABLE_FILES,
  CONTRACT_CLOSURE_V2_IMMUTABLE_FILES,
  GENERATOR_SUPPLY_V1_EVIDENCE_MANIFEST,
  GENERATOR_SUPPLY_V1_IMMUTABLE_FILES,
} from "./platform-successor-predecessor";

const repositoryRoot = resolve(import.meta.dirname, "../..");
const temporaryRoots: string[] = [];

afterEach(() => {
  for (const root of temporaryRoots.splice(0)) rmSync(root, { recursive: true, force: true });
});

type Fixture = {
  readonly root: string;
  readonly source: ContractReviewBindingSource;
  readonly tuple: ContractReviewTuple;
};

function createFixture(
  options: {
    readonly writeTuple?: boolean;
    readonly mutateBeforeCandidate?: (root: string, source: ContractReviewBindingSource) => void;
  } = {},
): Fixture {
  const root = mkdtempSync(join(tmpdir(), "contract-review-binding-"));
  temporaryRoots.push(root);
  git(root, ["init", "-q"]);
  writeText(root, "base.txt", "base\n");

  for (const schemaPath of [
    CONTRACT_REVIEW_BINDING_SOURCE_SCHEMA_PATH,
    CONTRACT_REVIEW_TUPLE_SCHEMA_PATH,
    CONTRACT_REVIEW_BINDING_REGISTRY_SCHEMA_PATH,
  ]) {
    writeParent(root, schemaPath);
    cpSync(resolve(repositoryRoot, schemaPath), resolve(root, schemaPath));
  }
  materializeSemanticDependencies(root);
  commitAll(root, "base semantic predecessors");

  const source = buildContractReviewBindingTestSource();
  writeJson(root, CONTRACT_REVIEW_BINDING_SOURCE_PATH, source);
  writeSemanticAuthorities(root);
  options.mutateBeforeCandidate?.(root, source);
  commitAll(root, "candidate");

  for (const [index, slot] of source.reviewSlots.entries()) {
    writeText(
      root,
      slot.reviewPath,
      `# Fixed independent review ${index + 1}\n\nAPPROVE P0=0 P1=0 P2=0\n`,
    );
  }
  commitAll(root, "independent reviews");
  const tuple = buildContractReviewBindingTestTuple(root, source);
  if (options.writeTuple) writeJson(root, CONTRACT_REVIEW_TUPLE_PATH, tuple);
  return { root, source, tuple };
}

function git(root: string, args: readonly string[]): string {
  return execFileSync("/usr/bin/git", args, {
    cwd: root,
    encoding: "utf8",
    env: {
      PATH: "/usr/bin:/bin",
      LANG: "C",
      LC_ALL: "C",
      GIT_CONFIG_NOSYSTEM: "1",
      GIT_CONFIG_GLOBAL: "/dev/null",
      GIT_AUTHOR_NAME: "Contract Review Test",
      GIT_AUTHOR_EMAIL: "contract-review-test@example.invalid",
      GIT_COMMITTER_NAME: "Contract Review Test",
      GIT_COMMITTER_EMAIL: "contract-review-test@example.invalid",
      GIT_AUTHOR_DATE: "2026-08-24T00:00:00Z",
      GIT_COMMITTER_DATE: "2026-08-24T00:00:00Z",
    },
  }).trim();
}

function commitAll(root: string, message: string): void {
  git(root, ["add", "-A"]);
  git(root, ["commit", "-q", "-m", message]);
}

function writeSemanticAuthorities(root: string): void {
  materializeSemanticDependencies(root);

  const closureSource = buildContractClosureProfileV3TestSource(root);
  writeJson(root, CONTRACT_CLOSURE_PROFILE_V3_SOURCE_PATH, closureSource);
  writeJson(
    root,
    CONTRACT_CLOSURE_PROFILE_V3_OUTPUT_PATH,
    buildContractClosureProfileV3Registry(root, closureSource),
  );

  const supplySource = buildGeneratorSupplyV2TestSource();
  writeJson(root, GENERATOR_SUPPLY_V2_SOURCE_PATH, supplySource);
  const replayFixture = buildGeneratorSupplyReplayV2TestFixture(
    root,
    GENERATOR_SUPPLY_V2_REPLAY_CONTRACT,
  );
  for (const path of SUCCESSOR_REPLAY_RECEIPT_PATHS) {
    writeJson(root, path, replayFixture.receipts[path]);
  }
  const supplyRegistry = buildSupplyRegistry(root, supplySource);
  writeJson(root, GENERATOR_SUPPLY_V2_OUTPUT_PATH, supplyRegistry);
}

function materializeSemanticDependencies(root: string): void {
  const paths = new Set<string>([
    ...CONTRACT_CLOSURE_V1_IMMUTABLE_FILES.map(({ path }) => path),
    ...CONTRACT_CLOSURE_V2_IMMUTABLE_FILES.map(({ path }) => path),
    ...GENERATOR_SUPPLY_V1_IMMUTABLE_FILES.map(({ path }) => path),
    ...CONTRACT_CLOSURE_V3_RUNTIME_FILES.map(({ path }) => path),
    CONTRACT_CLOSURE_V3_RUNTIME_REVIEW_FILE.path,
    CONTRACT_CLOSURE_PROFILE_V3_SOURCE_SCHEMA_PATH,
    CONTRACT_CLOSURE_PROFILE_V3_OUTPUT_SCHEMA_PATH,
    GENERATOR_SUPPLY_V2_SOURCE_SCHEMA_PATH,
    GENERATOR_SUPPLY_V2_OUTPUT_SCHEMA_PATH,
  ]);
  for (const authority of Object.values(GENERATOR_SUPPLY_V2_REPLAY_CONTRACT.authorityFiles)) {
    paths.add(authority.path);
  }
  const manifest = JSON.parse(
    readFileSync(
      resolve(repositoryRoot, GENERATOR_SUPPLY_V1_EVIDENCE_MANIFEST.manifestPath),
      "utf8",
    ),
  ) as { readonly files: readonly { readonly path: string }[] };
  for (const member of manifest.files) paths.add(member.path);
  for (const path of paths) copyFromRepository(root, path);
}

function copyFromRepository(root: string, path: string): void {
  writeParent(root, path);
  cpSync(resolve(repositoryRoot, path), resolve(root, path));
}

function buildSupplyRegistry(
  root: string,
  source: GeneratorSupplyV2Source,
): GeneratorSupplyV2Registry {
  const receipts = SUCCESSOR_REPLAY_RECEIPT_PATHS.map((path) => fileRecord(root, path));
  const evidenceManifest = {
    algorithm: "sorted-path-nul-sha256-nul-size-v1",
    files: receipts,
  };
  writeJson(root, GENERATOR_SUPPLY_V2_EVIDENCE_MANIFEST_PATH, evidenceManifest);
  const sourceDigest = domainDigest("cloud-agents/generator-supply/source/v2", source);
  const artifactSetDigest = domainDigest("cloud-agents/generator-supply/artifact-set/v2", {
    predecessor: source.predecessor,
    inheritance: source.inheritance,
    receipts,
  });
  const evidenceManifestDigest = domainDigest(
    "cloud-agents/generator-supply/evidence-manifest/v2",
    evidenceManifest,
  );
  const evidence = {
    state: "ASSEMBLED_LATE_BOUND",
    inheritance: source.inheritance,
    receipts,
    evidenceManifest,
  };
  const body = {
    formatVersion: "cloud-agents-generator-supply-profile-registry/v2",
    registryId: "cloud-agents/generator-supply-profile",
    predecessor: source.predecessor,
    sourceDigest,
    artifactSetDigest,
    evidenceManifestDigest,
    profile: {
      profileDigest: domainDigest("cloud-agents/generator-supply/profile/v2", {
        sourceDigest,
        artifactSetDigest,
        evidenceManifestDigest,
        spec: source.declaredProfile,
        evidence,
      }),
      spec: source.declaredProfile,
      evidence,
    },
  };
  return {
    ...body,
    registryDigest: domainDigest("cloud-agents/generator-supply/registry/v2", body),
  } as GeneratorSupplyV2Registry;
}

function fileRecord(
  root: string,
  path: string,
): { path: string; sha256: string; sizeBytes: number } {
  const bytes = readFileSync(resolve(root, path));
  return {
    path,
    sha256: `sha256:${createHash("sha256").update(bytes).digest("hex")}`,
    sizeBytes: bytes.byteLength,
  };
}

function domainDigest(domain: string, value: unknown): string {
  return `sha256:${createHash("sha256")
    .update(domain, "utf8")
    .update(Uint8Array.of(0))
    .update(canonicalizeJson(value))
    .digest("hex")}`;
}

function writeJson(root: string, path: string, value: unknown): void {
  writeText(root, path, serializeContractReviewBinding(value));
}

function writeText(root: string, path: string, value: string): void {
  writeParent(root, path);
  writeFileSync(resolve(root, path), value);
}

function writeParent(root: string, path: string): void {
  mkdirSync(dirname(resolve(root, path)), { recursive: true });
}

function clone<T>(value: T): T {
  return JSON.parse(JSON.stringify(value)) as T;
}

function expectCode(action: () => unknown, code: string): void {
  expect(action).toThrowError(expect.objectContaining({ code }));
}

describe("detached contract review-binding state machine", () => {
  it("requires a strict authority source before any absent-state classification", () => {
    const root = mkdtempSync(join(tmpdir(), "contract-review-binding-no-source-"));
    temporaryRoots.push(root);
    expectCode(
      () => inspectContractReviewBindingState(root),
      "CONTRACT_REVIEW_BINDING_SOURCE_REQUIRED",
    );
  });

  it("classifies source plus absent tuple/output as PRE_REVIEW_ABSENT", () => {
    const fixture = createFixture();
    expect(inspectContractReviewBindingState(fixture.root).kind).toBe("PRE_REVIEW_ABSENT");
    expect(() => assertContractReviewBindingCurrentOrAbsent(fixture.root)).not.toThrow();
    writeContractReviewBinding(fixture.root);
    expect(inspectContractReviewBindingState(fixture.root).kind).toBe("PRE_REVIEW_ABSENT");
  });

  it("requires explicit write for a complete tuple and then checks exact current bytes", () => {
    const fixture = createFixture({ writeTuple: true });
    expect(inspectContractReviewBindingState(fixture.root).kind).toBe(
      "COMPLETE_TUPLE_READY_TO_WRITE",
    );
    expectCode(
      () => assertContractReviewBindingCurrentOrAbsent(fixture.root),
      "CONTRACT_REVIEW_BINDING_OUTPUT_REQUIRED",
    );
    expect(() => writeContractReviewBinding(fixture.root)).not.toThrow();
    const current = inspectContractReviewBindingState(fixture.root);
    expect(current.kind).toBe("COMPLETE_TUPLE_OUTPUT_CURRENT");
    if (current.kind !== "COMPLETE_TUPLE_OUTPUT_CURRENT") throw new Error("unreachable");
    expect(current.registry).toMatchObject({
      registryId: "cloud-agents/platform/contract-review-binding",
      bindingId: "g-contract-current-source-review-binding/v1",
      effectiveCandidate: {
        status: "REVIEW_BOUND_SATISFIED_CANDIDATE",
        missing: [],
        notGateClosure: true,
        gateStatus: "ALL_GATES_OPEN",
      },
    });
    expect(current.registry.registryId).not.toContain("contract-closure-profile");
    expect(() => assertContractReviewBindingCurrentOrAbsent(fixture.root)).not.toThrow();
    expect(() => writeContractReviewBinding(fixture.root)).not.toThrow();
  });

  it("publishes with kernel-enforced no-overwrite when a competitor wins the race", () => {
    const fixture = createFixture();
    const competitor = "competitor-won\n";
    expect(() =>
      publishContractReviewBindingExclusiveForTest(fixture.root, "candidate\n", () => {
        writeText(fixture.root, CONTRACT_REVIEW_BINDING_OUTPUT_PATH, competitor);
      }),
    ).toThrowError(
      expect.objectContaining({
        code: "CONTRACT_REVIEW_BINDING_PARTIAL_STATE",
        path: "/state",
      }),
    );
    expect(readFileSync(resolve(fixture.root, CONTRACT_REVIEW_BINDING_OUTPUT_PATH), "utf8")).toBe(
      competitor,
    );
  });

  it("fails closed when output exists without the tuple", () => {
    const fixture = createFixture();
    writeJson(fixture.root, CONTRACT_REVIEW_BINDING_OUTPUT_PATH, {});
    expectCode(
      () => inspectContractReviewBindingState(fixture.root),
      "CONTRACT_REVIEW_BINDING_PARTIAL_STATE",
    );
  });

  it("fails closed on partial, unknown-field, or reordered tuple authority", () => {
    const fixture = createFixture();
    const malformed = clone(fixture.tuple) as ContractReviewTuple & { unexpected?: boolean };
    malformed.unexpected = true;
    writeJson(fixture.root, CONTRACT_REVIEW_TUPLE_PATH, malformed);
    expectCode(
      () => inspectContractReviewBindingState(fixture.root),
      "CONTRACT_REVIEW_BINDING_SCHEMA_INVALID",
    );

    const reordered = clone(fixture.tuple) as ContractReviewTuple & { reviews: ReviewBinding[] };
    reordered.reviews.reverse();
    writeJson(fixture.root, CONTRACT_REVIEW_TUPLE_PATH, reordered);
    expectCode(
      () => inspectContractReviewBindingState(fixture.root),
      "CONTRACT_REVIEW_BINDING_SCHEMA_INVALID",
    );
  });

  it("verifies tuple commit, tree, parent, diff, and review-blob Git authority", () => {
    const fixture = createFixture({ writeTuple: true });
    const forged = clone(fixture.tuple) as ContractReviewTuple & {
      reviews: Array<ReviewBinding & { candidate: Record<string, unknown> }>;
    };
    forged.reviews[0]!.candidate.tree = "f".repeat(40);
    writeJson(fixture.root, CONTRACT_REVIEW_TUPLE_PATH, forged);
    expectCode(
      () => inspectContractReviewBindingState(fixture.root),
      "CONTRACT_REVIEW_BINDING_IDENTITY_MISMATCH",
    );
  });

  it("binds actual authority and review bytes and rejects any digest drift", () => {
    const fixture = createFixture({ writeTuple: true });
    writeText(
      fixture.root,
      fixture.source.reviewSlots[0]!.reviewPath,
      "# Mutated after tuple construction\n",
    );
    expectCode(
      () => inspectContractReviewBindingState(fixture.root),
      "CONTRACT_REVIEW_BINDING_DIGEST_MISMATCH",
    );

    const authorityFixture = createFixture({ writeTuple: true });
    const authorityPath = resolve(
      authorityFixture.root,
      authorityFixture.source.supplyProfileAuthority.path,
    );
    writeFileSync(authorityPath, `${readFileSync(authorityPath, "utf8")} `);
    expectCode(
      () => inspectContractReviewBindingState(authorityFixture.root),
      "CONTRACT_REVIEW_BINDING_DIGEST_MISMATCH",
    );
  });

  it("rejects a profile-id-shaped document that masquerades as a canonical registry", () => {
    expect(() =>
      createFixture({
        mutateBeforeCandidate: (root, source) => {
          const path = source.canonicalClosureAuthority.path;
          const document = JSON.parse(readFileSync(resolve(root, path), "utf8")) as Record<
            string,
            unknown
          >;
          document.formatVersion = "test-authority/masquerade";
          const { registryDigest: _registryDigest, ...body } = document;
          document.registryDigest = domainDigest(
            "cloud-agents/contract-closure-profile/registry/v3",
            body,
          );
          writeJson(root, path, document);
        },
      }),
    ).toThrowError(
      expect.objectContaining({
        code: "CONTRACT_REVIEW_BINDING_IDENTITY_MISMATCH",
      }),
    );
  });

  it("rejects skeletal profile-shaped closure and supply authorities", () => {
    for (const kind of ["closure", "supply"] as const) {
      expect(() =>
        createFixture({
          mutateBeforeCandidate: (root, source) => {
            const authority =
              kind === "closure" ? source.canonicalClosureAuthority : source.supplyProfileAuthority;
            writeJson(root, authority.path, {
              formatVersion:
                kind === "closure"
                  ? "cloud-agents-contract-closure-profile-registry/v3"
                  : "cloud-agents-generator-supply-profile-registry/v2",
              registryId:
                kind === "closure"
                  ? "cloud-agents/platform/contract-closure-profile"
                  : "cloud-agents/generator-supply-profile",
              profile: {
                profileDigest: `sha256:${"0".repeat(64)}`,
                spec: {
                  profileId: authority.profileId,
                  notGateClosure: true,
                },
              },
              registryDigest: `sha256:${"0".repeat(64)}`,
            });
          },
        }),
      ).toThrowError(
        expect.objectContaining({
          code: "CONTRACT_REVIEW_BINDING_IDENTITY_MISMATCH",
        }),
      );
    }
  });

  it("rejects stale output rather than repairing it through write", () => {
    const fixture = createFixture({ writeTuple: true });
    writeContractReviewBinding(fixture.root);
    const output = JSON.parse(
      readFileSync(resolve(fixture.root, CONTRACT_REVIEW_BINDING_OUTPUT_PATH), "utf8"),
    ) as Record<string, unknown>;
    output.registryDigest = `sha256:${"f".repeat(64)}`;
    writeJson(fixture.root, CONTRACT_REVIEW_BINDING_OUTPUT_PATH, output);
    expectCode(
      () => inspectContractReviewBindingState(fixture.root),
      "CONTRACT_REVIEW_BINDING_OUTPUT_DRIFT",
    );
    expectCode(
      () => writeContractReviewBinding(fixture.root),
      "CONTRACT_REVIEW_BINDING_OUTPUT_DRIFT",
    );
  });

  it("forbids Gate, bootstrap, and self-review source mutations", () => {
    const gateFixture = createFixture();
    const gateSource = clone(gateFixture.source) as ContractReviewBindingSource & {
      implementationBoundary: Record<string, unknown>;
    };
    gateSource.implementationBoundary.gateStatus = "CLOSED";
    writeJson(gateFixture.root, CONTRACT_REVIEW_BINDING_SOURCE_PATH, gateSource);
    expectCode(
      () => inspectContractReviewBindingState(gateFixture.root),
      "CONTRACT_REVIEW_BINDING_SCHEMA_INVALID",
    );

    const bootstrapFixture = createFixture();
    const bootstrapSource = clone(bootstrapFixture.source) as ContractReviewBindingSource & {
      implementationBoundary: Record<string, unknown>;
    };
    bootstrapSource.implementationBoundary.bootstrapDiscovery = "ALLOWED";
    writeJson(bootstrapFixture.root, CONTRACT_REVIEW_BINDING_SOURCE_PATH, bootstrapSource);
    expectCode(
      () => inspectContractReviewBindingState(bootstrapFixture.root),
      "CONTRACT_REVIEW_BINDING_SCHEMA_INVALID",
    );

    const reviewFixture = createFixture();
    const selfReviewSource = clone(reviewFixture.source) as ContractReviewBindingSource & {
      reviewSlots: Array<Record<string, unknown>>;
    };
    selfReviewSource.reviewSlots[0]!.reviewPath = CONTRACT_REVIEW_BINDING_FINAL_REVIEW_PATH;
    writeJson(reviewFixture.root, CONTRACT_REVIEW_BINDING_SOURCE_PATH, selfReviewSource);
    expectCode(
      () => inspectContractReviewBindingState(reviewFixture.root),
      "CONTRACT_REVIEW_BINDING_SCHEMA_INVALID",
    );
  });

  it("keeps authority inputs deterministic and excludes every late-bound artifact", () => {
    const inputs = contractReviewBindingAuthorityInputs();
    expect(inputs).toEqual(inputs.toSorted());
    expect(new Set(inputs).size).toBe(inputs.length);
    expect(inputs).toContain(CONTRACT_REVIEW_BINDING_SOURCE_PATH);
    expect(inputs).toContain("scripts/lib/platform-contract-closure-profile-v3.ts");
    expect(inputs).toContain("scripts/lib/platform-contract-closure-profile-v3.test.ts");
    expect(inputs).toContain("scripts/lib/platform-generator-supply-profile-v2.ts");
    expect(inputs).toContain("scripts/lib/platform-generator-supply-profile-v2.test.ts");
    expect(inputs).toContain("scripts/lib/platform-generator-supply-replay-v2.ts");
    expect(inputs).toContain("scripts/lib/platform-generator-supply-replay-v2.test.ts");
    expect(inputs).not.toContain(CONTRACT_REVIEW_TUPLE_PATH);
    expect(inputs).not.toContain(CONTRACT_REVIEW_BINDING_OUTPUT_PATH);
    expect(inputs).not.toContain(CONTRACT_REVIEW_BINDING_FINAL_REVIEW_PATH);
    expect(inputs.every((path) => !path.includes("fixtures/manifest.json"))).toBe(true);
  });
});

type ReviewBinding = ContractReviewTuple["reviews"][number];
