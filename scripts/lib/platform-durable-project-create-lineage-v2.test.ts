import {
  cpSync,
  mkdirSync,
  mkdtempSync,
  readFileSync,
  rmSync,
  symlinkSync,
  writeFileSync,
} from "node:fs";
import { tmpdir } from "node:os";
import { resolve } from "node:path";

import { describe, expect, it } from "vitest";

import {
  assertDurableProjectCreateLineageV2Current,
  assertDurableProjectCreateLineagePredecessor,
  buildDurableProjectCreateLineageV2,
  durableProjectCreateLineageV2Inputs,
  serializeDurableProjectCreateLineageV2,
  validateVersionedFixtureManifest,
} from "./platform-durable-project-create-lineage-v2";

const repositoryRoot = resolve(import.meta.dirname, "../..");

describe("durable Project-create lineage v2", () => {
  it("is byte-current and deterministic", () => {
    expect(() => assertDurableProjectCreateLineageV2Current(repositoryRoot)).not.toThrow();
    const first = serializeDurableProjectCreateLineageV2(
      buildDurableProjectCreateLineageV2(repositoryRoot),
    );
    const second = serializeDurableProjectCreateLineageV2(
      buildDurableProjectCreateLineageV2(repositoryRoot),
    );
    expect(first).toBe(second);
    expect(first).toContain('"gateStatus": "ALL_GATES_OPEN"');
    expect(first).not.toMatch(/generatedAt|generated_at|\/Users\//u);
  });

  it("owns a unique, regular-file source closure and exact six-case manifest", () => {
    const inputs = durableProjectCreateLineageV2Inputs(repositoryRoot);
    expect(inputs).toEqual(inputs.toSorted());
    expect(new Set(inputs).size).toBe(inputs.length);
    expect(inputs).toContain(
      "services/control-plane/migrations/000013_add_durable_project_create_writer.sql",
    );
    expect(inputs).toContain("services/control-plane/migrations/catalog/schema-000012.json");
    expect(inputs).toContain("contracts/generation.lock.json");
    const lineage = buildDurableProjectCreateLineageV2(repositoryRoot) as {
      migration: { predecessorCatalog: { path: string } };
    };
    expect(lineage.migration.predecessorCatalog.path).toBe(
      "services/control-plane/migrations/catalog/schema-000012.json",
    );
    const manifest = validateVersionedFixtureManifest(repositoryRoot);
    expect(manifest.caseNames).toHaveLength(6);
    expect(manifest.sha256).toMatch(/^sha256:[0-9a-f]{64}$/u);
    expect(
      readFileSync(
        resolve(repositoryRoot, "contracts/platform/v1alpha1/fixtures/manifest-v2.json"),
        "utf8",
      ),
    ).toContain("durable-project-create-route-authority-v2");
  });

  it("rejects predecessor symlinks and source/fixture substitution", () => {
    const temporaryRoot = mkdtempSync(`${tmpdir()}/durable-lineage-`);
    try {
      const sourcePath = resolve(
        temporaryRoot,
        "contracts/platform/v1alpha1/fixtures/golden/durable-project-create-lineage-source-v2.json",
      );
      mkdirSync(resolve(sourcePath, ".."), { recursive: true });
      cpSync(
        resolve(
          repositoryRoot,
          "contracts/platform/v1alpha1/fixtures/golden/durable-project-create-lineage-source-v2.json",
        ),
        sourcePath,
      );
      const source = JSON.parse(readFileSync(sourcePath, "utf8")) as Record<string, any>;
      source.durableAuthority.schemaPaths[0] =
        "contracts/platform/v1alpha1/schemas/project.schema.json";
      writeFileSync(sourcePath, `${JSON.stringify(source, null, 2)}\n`);
      expect(() => durableProjectCreateLineageV2Inputs(temporaryRoot)).toThrow(
        /exact versioned authority path set/u,
      );

      const manifestPath = resolve(
        temporaryRoot,
        "contracts/platform/v1alpha1/fixtures/manifest-v2.json",
      );
      mkdirSync(resolve(manifestPath, ".."), { recursive: true });
      cpSync(
        resolve(repositoryRoot, "contracts/platform/v1alpha1/fixtures/manifest-v2.json"),
        manifestPath,
      );
      const manifest = JSON.parse(readFileSync(manifestPath, "utf8")) as Record<string, any>;
      manifest.cases[3].instance = "golden/durable-project-create-route-authority-v2.json";
      writeFileSync(manifestPath, `${JSON.stringify(manifest, null, 2)}\n`);
      expect(() => validateVersionedFixtureManifest(temporaryRoot)).toThrow(
        /schema\/instance mapping drifted/u,
      );

      const predecessorRoot = mkdtempSync(`${tmpdir()}/durable-lineage-predecessor-`);
      try {
        mkdirSync(resolve(predecessorRoot, "contracts"), { recursive: true });
        symlinkSync("/dev/null", resolve(predecessorRoot, "contracts/generation.lock.json"));
        expect(() =>
          assertDurableProjectCreateLineagePredecessor(predecessorRoot, {
            generationLock: { path: "contracts/generation.lock.json" },
          }),
        ).toThrow(/regular file/u);
      } finally {
        rmSync(predecessorRoot, { recursive: true, force: true });
      }
    } finally {
      rmSync(temporaryRoot, { recursive: true, force: true });
    }
  });
});
