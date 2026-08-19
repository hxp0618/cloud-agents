import {
  chmodSync,
  mkdirSync,
  mkdtempSync,
  readFileSync,
  rmSync,
  symlinkSync,
  writeFileSync,
} from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";

import { afterEach, describe, expect, it } from "vitest";

import {
  listRegularMigrationInputFiles,
  normalizedSourceManifestDigest,
  platformMigrationInputs,
  serializePlatformContractLock,
} from "./platform-contract-lock";

const temporaryRoots: string[] = [];

afterEach(() => {
  for (const root of temporaryRoots.splice(0)) rmSync(root, { force: true, recursive: true });
});

function temporaryRoot(): string {
  const root = mkdtempSync(join(tmpdir(), "platform-contract-lock-"));
  temporaryRoots.push(root);
  return root;
}

describe("Platform contract generation lock", () => {
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
    expect(inputs).toContain("scripts/lib/platform-migration-projection.test.ts");
    expect(inputs).toContain("services/control-plane/migrations/catalog/authority-v1.json");
    expect(inputs).toContain(
      "services/control-plane/migrations/000007_expand_durable_coordination_kernel.sql",
    );
    expect(inputs).toContain(
      "services/control-plane/migrations/000008_add_durable_coordination_service.sql",
    );
    expect(inputs).toContain(
      "services/control-plane/scripts/test-durable-coordination-service-postgres-matrix.sh",
    );
    expect(inputs).toContain(
      "services/control-plane/migrations/fixtures/bundle/negative/ancestor-descriptor-cases.json",
    );
  });

  it("records the generated durable coordination registry as a non-Gate pipeline", () => {
    const root = join(import.meta.dirname, "../..");
    const lock = JSON.parse(readFileSync(join(root, "contracts/generation.lock.json"), "utf8")) as {
      pipelines: Array<Record<string, unknown>>;
    };
    const pipeline = lock.pipelines.find(
      (candidate) => candidate.id === "durable-coordination-registry-generation",
    );
    expect(pipeline).toMatchObject({
      outputStatus: "GENERATED_CONTRACT_REGISTRY",
      notGateClosure: true,
      outputSummary: {
        profileCount: 1,
        runtimeConsumer: "GENERATED_GO_PROFILE_TYPED_SERVICE_000008",
        sqlConsumer: "GENERATED_PROFILE_TYPED_FUNCTIONS_000008",
        httpSurface: "NOT_IMPLEMENTED",
        externalSideEffects: "FORBIDDEN",
      },
      generatedOutputs: [
        {
          path: "contracts/generated/platform/v1alpha1/durable-coordination-registry.json",
        },
      ],
    });
  });

  it("records the generated Go profile without enabling HTTP or side effects", () => {
    const root = join(import.meta.dirname, "../..");
    const lock = JSON.parse(readFileSync(join(root, "contracts/generation.lock.json"), "utf8")) as {
      pipelines: Array<Record<string, unknown>>;
    };
    const pipeline = lock.pipelines.find(
      (candidate) => candidate.id === "durable-coordination-go-profile-generation",
    );
    expect(pipeline).toMatchObject({
      outputStatus: "GENERATED_GO_PROFILE",
      notGateClosure: true,
      outputSummary: {
        profileId: "managedAgentCreateProject/v1alpha1",
        handWrittenProfileFallback: "FORBIDDEN",
        httpSurface: "NOT_IMPLEMENTED",
        externalSideEffects: "FORBIDDEN",
      },
      generatedOutputs: [
        {
          path: "services/control-plane/internal/coordination/registry_generated.go",
        },
      ],
    });
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
