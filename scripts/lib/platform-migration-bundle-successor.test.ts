import { createHash } from "node:crypto";
import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { describe, expect, it } from "vitest";

import { buildMigrationBundle } from "./platform-migration-bundle";
import {
  buildDurableProjectCreateMigrationSuccessor,
  DURABLE_PROJECT_CREATE_MIGRATION_SUCCESSOR_ARCHIVE_PATH,
  DURABLE_PROJECT_CREATE_MIGRATION_SUCCESSOR_MANIFEST_PATH,
  DURABLE_PROJECT_CREATE_MIGRATION_SUCCESSOR_PROFILE_PATH,
  DURABLE_PROJECT_CREATE_MIGRATION_SUCCESSOR_SCHEMA_BUNDLE_PATH,
  validateCheckedInDurableProjectCreateMigrationSuccessor,
  validateDurableProjectCreateMigrationSuccessorSource,
} from "./platform-migration-bundle-successor";
import { readDeterministicUstar } from "./platform-migration-ustar";
import type { JsonObject } from "./platform-migration-projection";

const root = resolve(import.meta.dirname, "../..");

describe("durable Project-create migration successor", () => {
  it("keeps the canonical 000013 predecessor and builds a deterministic 000014 runtime", () => {
    const predecessor = buildMigrationBundle(root);
    const successor = buildDurableProjectCreateMigrationSuccessor(root);
    expect(predecessor.manifest.schema_bundle.schema_head).toBe("000013");
    expect(predecessor.manifest.schema_bundle_digest).toBe(
      "sha256:c7e08e81b463d04dd267438ac636811200586d5d84d8cb2e8d18799bd2c5faca",
    );
    expect(successor.manifest.schema_bundle.schema_head).toBe("000014");
    expect(successor.schemaBundle.schema_bundle.schema_head).toBe("000014");
    expect(successor.schemaBundle.schema_bundle.migrations).toHaveLength(14);
    expect(successor.schemaBundle.schema_bundle.migrations.at(-1)).toMatchObject({
      id: "000014",
      predecessor_id: "000013",
      schema_from: "000013",
      schema_to: "000014",
    });
    expect(successor.profile.runner.mode).toBe("localdev_only");
    expect(successor.profile.runner.completeLedger).toBe("no-op");
    expect(successor.profile.runner.entryWriter).toBe("NOT_IMPLEMENTED");
    expect(successor.profile.implementationBoundary.databaseWrites).toBe("not_authorized");
    expect(successor.profile.$schema).toBe(
      "https://schemas.cloud-agents.dev/platform/migrations/successor/000014/profile.schema.json",
    );
    expect(successor.profile.authorityId).toBe("D-053-MIG-000014");
    expect(successor.profile.revision).toBe("D-053-MIG-000014.r1");
    expect(successor.profile.inputScope).toBe("generator-and-focused-verification-closure/v1");
    expect(successor.source.schemaDescriptor).toMatchObject({
      path: "services/control-plane/migrations/successor/000014/authority-source.schema.json",
      mode: "100644",
    });
    expect(successor.source.profileDescriptor).toMatchObject({
      path: "services/control-plane/migrations/successor/000014/profile.schema.json",
      mode: "100644",
    });

    const members = readDeterministicUstar(successor.runtimeTar);
    const paths = members.map(({ path }) => path);
    expect(new Set(paths).size).toBe(paths.length);
    expect(paths).toEqual([...paths].toSorted());
    expect(paths).toContain(
      "services/control-plane/migrations/000014_harden_durable_project_create_identifiers.sql",
    );
    expect(paths).toContain("services/control-plane/migrations/catalog/schema-000014.json");
    expect(paths).toContain(DURABLE_PROJECT_CREATE_MIGRATION_SUCCESSOR_ARCHIVE_PATH);
    expect(successor.runtimeTar).toEqual(
      buildDurableProjectCreateMigrationSuccessor(root).runtimeTar,
    );
    expect(successor.profile.runtime.sha256).toBe(digest(successor.runtimeTar));
    expect(successor.profile.runtime.memberManifest).toMatchObject({
      state: "ABSENT_PENDING",
      formatVersion: "cloud-agents-platform-runtime-member-manifest/v1",
      algorithm: "deterministic-ustar-v1",
      order: "ASCII-byte-path",
      recordCount: members.length,
    });
    expect(successor.profile.runtime.memberManifest.records).toHaveLength(members.length);
    expect(successor.profile.runtime.memberManifest.records.map(({ path }) => path)).toEqual(paths);
  }, 30_000);

  it("is current only when every generated successor artifact and the archive are exact", () => {
    const current = validateCheckedInDurableProjectCreateMigrationSuccessor(root);
    expect(
      current.generatedFiles.has(DURABLE_PROJECT_CREATE_MIGRATION_SUCCESSOR_PROFILE_PATH),
    ).toBe(true);
    expect(
      current.generatedFiles.has(DURABLE_PROJECT_CREATE_MIGRATION_SUCCESSOR_MANIFEST_PATH),
    ).toBe(true);
    expect(
      current.generatedFiles.has(DURABLE_PROJECT_CREATE_MIGRATION_SUCCESSOR_SCHEMA_BUNDLE_PATH),
    ).toBe(true);
    expect(
      readFileSync(resolve(root, DURABLE_PROJECT_CREATE_MIGRATION_SUCCESSOR_ARCHIVE_PATH)),
    ).toEqual(readFileSync(resolve(root, "services/control-plane/migrations/schema-bundle.json")));
  });

  it("fails closed on predecessor or source substitution", () => {
    const value = JSON.parse(
      readFileSync(
        resolve(root, "services/control-plane/migrations/successor/000014/authority-source.json"),
        "utf8",
      ),
    ) as Record<string, unknown>;
    value.successorId = "substituted";
    expect(() =>
      validateDurableProjectCreateMigrationSuccessorSource(root, value as JsonObject),
    ).toThrow(/SOURCE_IDENTITY/u);
  });

  it("fails closed when the frozen source closure or runner policy drifts", () => {
    const source = JSON.parse(
      readFileSync(
        resolve(root, "services/control-plane/migrations/successor/000014/authority-source.json"),
        "utf8",
      ),
    ) as Record<string, unknown>;
    source.inputPaths = [
      ...(source.inputPaths as string[]),
      "services/control-plane/migrations/successor/000014/profile.json",
    ];
    expect(() =>
      validateDurableProjectCreateMigrationSuccessorSource(root, source as JsonObject),
    ).toThrow(/SOURCE_SCHEMA_INVALID|SOURCE_PATH_ORDER|SOURCE_INPUT_SET/u);

    const runnerSource = JSON.parse(
      readFileSync(
        resolve(root, "services/control-plane/migrations/successor/000014/authority-source.json"),
        "utf8",
      ),
    ) as Record<string, unknown>;
    (runnerSource.runner as Record<string, unknown>).completeLedger = "write";
    expect(() =>
      validateDurableProjectCreateMigrationSuccessorSource(root, runnerSource as JsonObject),
    ).toThrow(/SOURCE_SCHEMA_INVALID|SOURCE_RUNNER/u);
  });
});

function digest(bytes: Uint8Array): string {
  return `sha256:${createHash("sha256").update(bytes).digest("hex")}`;
}
