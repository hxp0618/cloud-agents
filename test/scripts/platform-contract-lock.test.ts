import { createHash } from "node:crypto";
import {
  chmodSync,
  mkdirSync,
  mkdtempSync,
  readFileSync,
  renameSync,
  rmSync,
  symlinkSync,
  writeFileSync,
} from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";

import { afterEach, describe, expect, it } from "vitest";

import {
  buildPlatformSuccessorContractLockDocument,
  buildPlatformContractStandardsLockState,
  assertPlatformSuccessorCoreOutputSnapshotMutationForTest,
  listRegularMigrationInputFiles,
  normalizedSourceManifestDigest,
  platformContractStandardsInputs,
  platformMigrationInputs,
  serializePlatformContractLock,
  type PlatformSuccessorContractLockAuthority,
  type SuccessorLockCoreOutputRecord,
  type SuccessorLockFileRecord,
  writePlatformContractLockDocument,
} from "../../scripts/lib/platform-contract-lock";
import {
  SUCCESSOR_CORE_GENERATOR_OUTPUT_PATHS,
  SUCCESSOR_PROJECTION_EXCLUSIONS,
} from "../../scripts/lib/platform-successor-dag";

const temporaryRoots: string[] = [];

afterEach(() => {
  for (const root of temporaryRoots.splice(0)) rmSync(root, { force: true, recursive: true });
});

function temporaryRoot(): string {
  const root = mkdtempSync(join(tmpdir(), "platform-contract-lock-"));
  temporaryRoots.push(root);
  return root;
}

const testSha = (character: string): string => `sha256:${character.repeat(64)}`;

function testFileRecord(path: string, character = "a"): SuccessorLockFileRecord {
  return { path, sha256: testSha(character), sizeBytes: 1 };
}

function successorAuthority(
  reviewBinding: PlatformSuccessorContractLockAuthority["reviewBinding"] = {
    state: "PRE_REVIEW_ABSENT",
    sourceDigest: testSha("7"),
  },
): PlatformSuccessorContractLockAuthority {
  const coreOutputs: SuccessorLockCoreOutputRecord[] = SUCCESSOR_CORE_GENERATOR_OUTPUT_PATHS.map(
    (path) => ({
      ...testFileRecord(path, "9"),
      gitMode: "100644",
    }),
  );
  return {
    standards: {
      formatVersion: "cloud-agents-contract-standards-profile/v2",
      profile: testFileRecord("tools/contract-standards/profile-v2.json", "1"),
      predecessor: testFileRecord("tools/contract-standards/profile.json", "2"),
      schemaFiles: 60,
      fixtureManifests: 2,
      fixtureCases: 79,
    },
    closure: {
      formatVersion: "cloud-agents-contract-closure-profile-registry/v3",
      profileId: "contract-closure-profile/v3",
      profileDigest: testSha("3"),
      registryDigest: testSha("4"),
      output: testFileRecord(
        "contracts/generated/platform/v1alpha1/contract-closure-profile-v3.json",
        "5",
      ),
    },
    supply: {
      formatVersion: "cloud-agents-generator-supply-profile-registry/v2",
      profileId: "cloud-agents/generator-supply-profile/v2",
      profileDigest: testSha("6"),
      registryDigest: testSha("7"),
      candidateManifestSha256: testCandidateManifest(coreOutputs),
      outputFiles: 49,
      output: testFileRecord("tools/generator-supply/v2/profile.json", "8"),
    },
    coreOutputs,
    projectionExclusions: [...SUCCESSOR_PROJECTION_EXCLUSIONS],
    reviewBinding,
  };
}

function testCandidateManifest(records: readonly SuccessorLockCoreOutputRecord[]): string {
  const hash = createHash("sha256");
  for (const record of records) {
    hash
      .update(record.path)
      .update("\0")
      .update(record.sha256.replace(/^sha256:/u, ""))
      .update("\0")
      .update(record.gitMode)
      .update("\0");
  }
  return `sha256:${hash.digest("hex")}`;
}

describe("Platform contract generation lock", () => {
  it("builds deterministic pre-review and post-binding successor lock documents", () => {
    const pre = buildPlatformSuccessorContractLockDocument(successorAuthority());
    expect(pre).toMatchObject({
      formatVersion: "cloud-agents-platform-contract-generation-lock/v2",
      lockVersion: 2,
      status: "SUCCESSOR_ASSEMBLED_PRE_REVIEW",
      notGateClosure: true,
      gateStatus: "ALL_GATES_OPEN",
      authorities: {
        generatorSupply: {
          outputFiles: 49,
          candidateManifestSha256: successorAuthority().supply.candidateManifestSha256,
        },
      },
      coreGeneratorOutputs: {
        count: 49,
        algorithm: "utf8-bytewise-sorted-path-nul-sha256-nul-git-mode-v1",
        replayCandidateManifestSha256: successorAuthority().supply.candidateManifestSha256,
      },
      projection: {
        trackedLegacyLock: "EXCLUDED_FROM_SUCCESSOR_PROJECTION",
        exclusionCount: 16,
        exclusions: SUCCESSOR_PROJECTION_EXCLUSIONS,
      },
      reviewBinding: { state: "PRE_REVIEW_ABSENT" },
    });
    expect(pre).not.toHaveProperty("sourceContract");
    expect(serializePlatformContractLock(pre)).toBe(
      serializePlatformContractLock(
        buildPlatformSuccessorContractLockDocument(successorAuthority()),
      ),
    );
    expect(serializePlatformContractLock(pre)).not.toMatch(/generatedAt|\/Users\//u);

    const post = buildPlatformSuccessorContractLockDocument(
      successorAuthority({
        state: "COMPLETE_TUPLE_OUTPUT_CURRENT",
        sourceDigest: testSha("7"),
        tupleDigest: testSha("8"),
        registryDigest: testSha("9"),
        tuple: testFileRecord("tools/contract-review-binding/v1/review-tuple.json", "a"),
        registry: testFileRecord("tools/contract-review-binding/v1/registry.json", "b"),
      }),
    );
    expect(post).toMatchObject({
      status: "SUCCESSOR_ASSEMBLED_REVIEW_BOUND",
      reviewBinding: {
        state: "COMPLETE_TUPLE_OUTPUT_CURRENT",
        tuple: { path: "tools/contract-review-binding/v1/review-tuple.json" },
        registry: { path: "tools/contract-review-binding/v1/registry.json" },
      },
    });
    expect(post.lockDigest).not.toBe(pre.lockDigest);
  });

  it("fails closed on successor output, exclusion, boundary, and binding-state drift", () => {
    const base = successorAuthority();
    const missingOutput = { ...base, coreOutputs: base.coreOutputs.slice(1) };
    expect(() => buildPlatformSuccessorContractLockDocument(missingOutput)).toThrow(/49-path/u);

    const exclusions = {
      ...base,
      projectionExclusions: base.projectionExclusions.slice(1),
    };
    expect(() => buildPlatformSuccessorContractLockDocument(exclusions)).toThrow(/16-path/u);

    const counts = { ...base, standards: { ...base.standards, fixtureCases: 77 } };
    expect(() => buildPlatformSuccessorContractLockDocument(counts)).toThrow(/standards v2/u);

    const candidateManifest = {
      ...base,
      supply: { ...base.supply, candidateManifestSha256: testSha("0") },
    };
    expect(() => buildPlatformSuccessorContractLockDocument(candidateManifest)).toThrow(
      /receipt-verified/u,
    );

    const ready = {
      ...base,
      reviewBinding: {
        state: "COMPLETE_TUPLE_READY_TO_WRITE",
        sourceDigest: testSha("7"),
      },
    } as unknown as PlatformSuccessorContractLockAuthority;
    expect(() => buildPlatformSuccessorContractLockDocument(ready)).toThrow(
      /partial|ready-to-write/u,
    );

    const topology = { ...base, unexpected: true } as PlatformSuccessorContractLockAuthority;
    expect(() => buildPlatformSuccessorContractLockDocument(topology)).toThrow(/topology/u);
  });

  it("keeps every lock writer scoped to generation.lock.json only", () => {
    const source = readFileSync(
      join(import.meta.dirname, "../../scripts/generate-platform-contract-lock.ts"),
      "utf8",
    );
    expect(source).toContain("--write-successor");
    expect(source).toContain("--check-successor");
    expect(source).not.toMatch(/writeIdentityVerifier|generate-platform-identity/u);
    expect(source.match(/writePlatformContractLockDocument\(/gu)).toHaveLength(2);
    expect(source).not.toMatch(/writeFileSync|from "node:fs"/u);
  });

  it("does not suggest rewriting the frozen predecessor lock", () => {
    const source = readFileSync(
      join(import.meta.dirname, "../../scripts/lib/platform-contract-lock.ts"),
      "utf8",
    );
    expect(source).toContain(
      "contracts/generation.lock.json is a frozen predecessor and is stale; do not run --write.",
    );
    expect(source).not.toContain(
      "contracts/generation.lock.json is stale; run bun scripts/generate-platform-contract-lock.ts --write.",
    );
  });

  it("rejects core-output replacement and restoration after the 49-path capture", () => {
    const root = temporaryRoot();
    for (const path of SUCCESSOR_CORE_GENERATOR_OUTPUT_PATHS) {
      const target = join(root, path);
      mkdirSync(join(target, ".."), { recursive: true });
      writeFileSync(target, `${path}\n`);
    }
    const target = join(root, SUCCESSOR_CORE_GENERATOR_OUTPUT_PATHS[0]);
    const backup = `${target}.capture-backup`;
    expect(() =>
      assertPlatformSuccessorCoreOutputSnapshotMutationForTest(root, () => {
        renameSync(target, backup);
        writeFileSync(target, "replacement\n");
        rmSync(target);
        renameSync(backup, target);
      }),
    ).toThrow(/parent topology changed/u);
    expect(readFileSync(target, "utf8")).toBe(`${SUCCESSOR_CORE_GENERATOR_OUTPUT_PATHS[0]}\n`);
  });

  it("writes only through a contained regular atomic lock destination", () => {
    const finalSymlinkRoot = temporaryRoot();
    mkdirSync(join(finalSymlinkRoot, "contracts"), { recursive: true });
    const sentinel = join(finalSymlinkRoot, "sentinel.json");
    const output = join(finalSymlinkRoot, "contracts/generation.lock.json");
    writeFileSync(sentinel, "sentinel\n");
    symlinkSync(sentinel, output);
    expect(() => writePlatformContractLockDocument(finalSymlinkRoot, { lockVersion: 2 })).toThrow(
      /non-symlink/u,
    );
    expect(readFileSync(sentinel, "utf8")).toBe("sentinel\n");

    rmSync(output);
    writePlatformContractLockDocument(finalSymlinkRoot, { lockVersion: 2 });
    expect(readFileSync(output, "utf8")).toBe('{\n  "lockVersion": 2\n}\n');

    const ancestorSymlinkRoot = temporaryRoot();
    const outside = temporaryRoot();
    mkdirSync(join(outside, "lock-target"), { recursive: true });
    symlinkSync(join(outside, "lock-target"), join(ancestorSymlinkRoot, "contracts"));
    expect(() =>
      writePlatformContractLockDocument(ancestorSymlinkRoot, { lockVersion: 2 }),
    ).toThrow(/parent/u);
  });

  it("keeps historical standards metadata independent from current source changes", () => {
    const root = join(import.meta.dirname, "../..");
    expect(buildPlatformContractStandardsLockState(root).profile.formatVersion).toBe(
      "cloud-agents-contract-standards-profile/v3",
    );
    const inputs = platformContractStandardsInputs(root);
    expect(inputs).toEqual(inputs.toSorted());
    expect(new Set(inputs).size).toBe(inputs.length);
    expect(inputs).toContain("tools/contract-standards/profile.json");
    expect(inputs).toContain("tools/contract-standards/profile-v2.json");
    expect(inputs).toContain("scripts/lib/platform-contract-standards-profile.ts");
    expect(inputs).not.toContain("contracts/generation.lock.json");
  });

  it("serializes without timestamps or host paths", () => {
    const serialized = serializePlatformContractLock({
      lockVersion: 1,
      status: "BOOTSTRAP_VALIDATED",
    });
    expect(serialized).toBe('{\n  "lockVersion": 1,\n  "status": "BOOTSTRAP_VALIDATED"\n}\n');
    expect(serialized).not.toMatch(/generatedAt|\/Users\//u);
  });

  it("normalizes non-executable permissions to the Git 100644 mode", () => {
    const root = temporaryRoot();
    const source = join(root, "input.txt");
    writeFileSync(source, "same bytes\n");
    chmodSync(source, 0o644);
    const ordinary = normalizedSourceManifestDigest(root, ["input.txt"]);
    chmodSync(source, 0o600);
    expect(normalizedSourceManifestDigest(root, ["input.txt"])).toBe(ordinary);
    chmodSync(source, 0o755);
    expect(normalizedSourceManifestDigest(root, ["input.txt"])).not.toBe(ordinary);
  });

  it("binds source bytes and normalized paths", () => {
    const root = temporaryRoot();
    const source = join(root, "module.go");
    writeFileSync(source, "package module\n");
    const initial = normalizedSourceManifestDigest(root, ["module.go"]);
    writeFileSync(source, "package changed\n");
    expect(normalizedSourceManifestDigest(root, ["module.go"])).not.toBe(initial);
  });

  it("recursively binds every catalog and fixture file", () => {
    const repositoryRoot = join(import.meta.dirname, "../..");
    const inputs = platformMigrationInputs(repositoryRoot);
    expect(inputs).toEqual(inputs.toSorted());
    expect(new Set(inputs).size).toBe(inputs.length);
    expect(inputs).toContain("docs/plan/adr/0010-p1-postgres-projection-contract.md");
    expect(inputs).toContain("docs/plan/adr/0011-p1-membership-rbac-contract.md");
    expect(inputs).toContain("docs/plan/adr/0013-p1-durable-coordination-contract.md");
    expect(inputs).toContain(
      "contracts/generated/platform/v1alpha1/durable-coordination-registry.json",
    );
    expect(inputs).toContain("scripts/lib/platform-migration-projection.ts");
    expect(inputs).toContain("test/scripts/platform-migration-projection.test.ts");
    expect(inputs).toContain("services/control-plane/migrations/catalog/authority-v1.json");
    expect(inputs).toContain(
      "services/control-plane/migrations/000007_expand_durable_coordination_kernel.sql",
    );
    expect(inputs).toContain(
      "services/control-plane/migrations/000008_add_durable_coordination_service.sql",
    );
    expect(inputs).toContain(
      "services/control-plane/test/test-durable-coordination-service-postgres-matrix.sh",
    );
    expect(inputs).toContain(
      "services/control-plane/migrations/000012_fix_compatibility_recovery_preflight.sql",
    );
    expect(inputs).toContain(
      "services/control-plane/test/test-compatibility-recovery-preflight-retirement-postgres-matrix.sh",
    );
    expect(inputs).toContain(
      "services/control-plane/migrations/fixtures/bundle/negative/ancestor-descriptor-cases.json",
    );
  });

  it("rejects symbolic links in recursive migration inputs", () => {
    const root = temporaryRoot();
    mkdirSync(join(root, "catalog"));
    writeFileSync(join(root, "outside.json"), "{}\n");
    symlinkSync(join(root, "outside.json"), join(root, "catalog", "linked.json"));
    expect(() => listRegularMigrationInputFiles(root, "catalog")).toThrow(/symbolic links/u);
  });

  it("sorts nested migration inputs and rejects an escaping root", () => {
    const root = temporaryRoot();
    mkdirSync(join(root, "fixtures", "z"), { recursive: true });
    mkdirSync(join(root, "fixtures", "a"), { recursive: true });
    writeFileSync(join(root, "fixtures", "z", "second.json"), "{}\n");
    writeFileSync(join(root, "fixtures", "a", "first.json"), "{}\n");
    expect(listRegularMigrationInputFiles(root, "fixtures")).toEqual([
      "fixtures/a/first.json",
      "fixtures/z/second.json",
    ]);
    expect(() => listRegularMigrationInputFiles(root, "../outside")).toThrow(/escapes/u);
  });
});
