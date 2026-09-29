import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { describe, expect, it } from "vitest";
import { validateDurableProjectCreateMigrationSuccessorSource } from "./platform-migration-bundle-successor";
import type { JsonObject } from "./platform-migration-projection";

const root = resolve(import.meta.dirname, "../..");

describe("durable Project-create migration successor", () => {
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
