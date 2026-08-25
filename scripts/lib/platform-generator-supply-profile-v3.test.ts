import { createHash } from "node:crypto";
import {
  copyFileSync,
  mkdirSync,
  mkdtempSync,
  readFileSync,
  rmSync,
  symlinkSync,
  writeFileSync,
} from "node:fs";
import { tmpdir } from "node:os";
import { dirname, join, resolve } from "node:path";

import { afterEach, describe, expect, it } from "vitest";

import { SUCCESSOR_V3_REPLAY_RECEIPT_PATHS } from "./platform-successor-dag-v3";
import {
  assertGeneratorSupplyV3RegistrySemantics,
  assertGeneratorSupplyV3SourceCurrent,
  buildGeneratorSupplyV3Registry,
  buildGeneratorSupplyV3EvidenceManifest,
  buildGeneratorSupplyV3Source,
  GENERATOR_SUPPLY_V3_OUTPUT_SCHEMA_PATH,
  GENERATOR_SUPPLY_V3_SOURCE_PATH,
  GENERATOR_SUPPLY_V3_SOURCE_SCHEMA_PATH,
  inspectGeneratorSupplyV3AuthorityState,
  serializeGeneratorSupplyV3Source,
  validateGeneratorSupplyV3Source,
  writeGeneratorSupplyV3Assembly,
  writeGeneratorSupplyV3Source,
  type GeneratorSupplyV3Source,
} from "./platform-generator-supply-profile-v3";
import { assertGeneratorSupplyReplayV3ContractCurrent } from "./platform-generator-supply-replay-v3";
import type { GeneratorSupplyReplayV3PreparedReceipts } from "./platform-generator-supply-replay-v3";

const repositoryRoot = resolve(import.meta.dirname, "../..");
const temporaryRoots: string[] = [];

afterEach(() => {
  for (const root of temporaryRoots.splice(0)) rmSync(root, { recursive: true, force: true });
});

function createRoot(): string {
  const root = mkdtempSync(join(tmpdir(), "generator-supply-profile-v3-"));
  temporaryRoots.push(root);
  for (const path of [
    GENERATOR_SUPPLY_V3_SOURCE_SCHEMA_PATH,
    GENERATOR_SUPPLY_V3_OUTPUT_SCHEMA_PATH,
    "scripts/replay-platform-generators-isolated-v3.sh",
    "scripts/replay-platform-generators-v3.ts",
    "scripts/lib/generator-replay-path-authority.ts",
    "scripts/lib/inspect-generator-replay-archive.py",
  ])
    copyFromRepository(root, path);
  return root;
}

function copyFromRepository(root: string, path: string): void {
  const target = resolve(root, path);
  mkdirSync(dirname(target), { recursive: true });
  copyFileSync(resolve(repositoryRoot, path), target);
}

function writeJson(root: string, path: string, value: unknown): void {
  const target = resolve(root, path);
  mkdirSync(dirname(target), { recursive: true });
  writeFileSync(target, `${JSON.stringify(value, null, 2)}\n`);
}

function fakePrepared(root: string): GeneratorSupplyReplayV3PreparedReceipts {
  const receipts = SUCCESSOR_V3_REPLAY_RECEIPT_PATHS.map((path) => {
    const bytes = Buffer.from(`{"path":"${path}"}\n`);
    const target = resolve(root, path);
    mkdirSync(dirname(target), { recursive: true });
    writeFileSync(target, bytes);
    return {
      path,
      bytes,
      sha256: `sha256:${createHash("sha256").update(bytes).digest("hex")}`,
      sizeBytes: bytes.byteLength,
    };
  });
  return {
    receipts: new Map(receipts.map(({ path, bytes }) => [path, bytes])),
    receiptRecords: receipts.map(({ path, sha256, sizeBytes }) => ({ path, sha256, sizeBytes })),
    projection: {} as never,
    candidateManifestSha256: "sha256:" + "a".repeat(64),
    outputFiles: 49,
    assertInputSnapshotCurrent: () => {},
    assertPreparedSnapshotCurrent: () => {},
  } as GeneratorSupplyReplayV3PreparedReceipts;
}

describe("generator-supply profile v3 authority", () => {
  it("consumes the checked-in v3 source contract with its four versioned authorities", () => {
    const source = buildGeneratorSupplyV3Source();
    expect(() =>
      assertGeneratorSupplyReplayV3ContractCurrent(repositoryRoot, source.replayContract),
    ).not.toThrow();
  });

  it("keeps schema-only and pre-replay states explicit", () => {
    const root = createRoot();
    expect(inspectGeneratorSupplyV3AuthorityState(root)).toBe("SCHEMA_ONLY");

    writeGeneratorSupplyV3Source(root);
    expect(assertGeneratorSupplyV3SourceCurrent(root)).toBe("DECLARED_PRE_REPLAY");
    expect(readFileSync(resolve(root, GENERATOR_SUPPLY_V3_SOURCE_PATH), "utf8")).toBe(
      serializeGeneratorSupplyV3Source(buildGeneratorSupplyV3Source()),
    );
  });

  it("rejects source drift and partial late-bound receipt groups", () => {
    const root = createRoot();
    writeGeneratorSupplyV3Source(root);
    const source = JSON.parse(
      readFileSync(resolve(root, GENERATOR_SUPPLY_V3_SOURCE_PATH), "utf8"),
    ) as GeneratorSupplyV3Source;
    const drifted = structuredClone(source) as Record<string, unknown>;
    drifted.decisionId = "D-DRIFT";
    expect(() => validateGeneratorSupplyV3Source(root, drifted as GeneratorSupplyV3Source)).toThrow(
      /schema|source/i,
    );

    writeJson(root, SUCCESSOR_V3_REPLAY_RECEIPT_PATHS[0], { partial: true });
    expect(() => inspectGeneratorSupplyV3AuthorityState(root)).toThrow(/partial|eight/i);
  });

  it("rejects on-disk source drift from the canonical source authority", () => {
    const root = createRoot();
    writeGeneratorSupplyV3Source(root);
    const sourcePath = resolve(root, GENERATOR_SUPPLY_V3_SOURCE_PATH);
    const drifted = JSON.parse(readFileSync(sourcePath, "utf8")) as Record<string, unknown>;
    drifted.decisionId = "D-DRIFT-ON-DISK";
    writeFileSync(sourcePath, `${JSON.stringify(drifted, null, 2)}\n`);
    expect(() => assertGeneratorSupplyV3SourceCurrent(root)).toThrow(/source|schema/i);
  });

  it("rejects an assembly write from a partial replay topology", () => {
    const root = createRoot();
    writeGeneratorSupplyV3Source(root);
    writeJson(root, SUCCESSOR_V3_REPLAY_RECEIPT_PATHS[0], { partial: true });
    expect(() =>
      writeGeneratorSupplyV3Assembly(root, {
        projection: "/absent/projection.json",
        darwinOutputDirectory: "/absent/darwin",
        linuxOutputDirectory: "/absent/linux",
      }),
    ).toThrow(/partial|replay/i);
  });

  it("rejects a symlinked ancestor in the v3 authority path", () => {
    const root = createRoot();
    const supplyDirectory = resolve(root, "tools/generator-supply");
    rmSync(supplyDirectory, { recursive: true, force: true });
    symlinkSync(resolve(repositoryRoot, "tools/generator-supply"), supplyDirectory, "dir");
    expect(() => inspectGeneratorSupplyV3AuthorityState(root)).toThrow(/unsafe|symlink|partial/i);
  });

  it("binds the exact ordered eight receipt records into v3 registry digests", () => {
    const root = createRoot();
    writeGeneratorSupplyV3Source(root);
    const source = buildGeneratorSupplyV3Source();
    const prepared = fakePrepared(root);
    const manifest = buildGeneratorSupplyV3EvidenceManifest(prepared);
    const registry = buildGeneratorSupplyV3Registry(source, prepared, manifest);
    assertGeneratorSupplyV3RegistrySemantics(root, registry, manifest);
  });
});
