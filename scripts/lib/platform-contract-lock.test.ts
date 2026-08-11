import { chmodSync, mkdirSync, mkdtempSync, rmSync, symlinkSync, writeFileSync } from "node:fs";
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
    expect(inputs).toContain("scripts/lib/platform-migration-projection.ts");
    expect(inputs).toContain("scripts/lib/platform-migration-projection.test.ts");
    expect(inputs).toContain("services/control-plane/migrations/catalog/authority-v1.json");
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
