import { createHash } from "node:crypto";
import {
  cpSync,
  copyFileSync,
  mkdirSync,
  mkdtempSync,
  readFileSync,
  renameSync,
  rmSync,
  writeFileSync,
} from "node:fs";
import { tmpdir } from "node:os";
import { dirname, join, resolve } from "node:path";

import { afterEach, describe, expect, it } from "vitest";

import {
  assertGeneratorSupplyReplayV3ContractCurrent,
  assertGeneratorSupplyReplayV3InputSnapshotMutationForTest,
  assertGeneratorSupplyReplayV3Receipts,
  assertGeneratorSupplyReplayV3SnapshotMutationForTest,
  assertGeneratorSupplyReplayV3V2DerivedABAMutationForTest,
  buildGeneratorSupplyReplayV3PreparedReceipts,
  buildGeneratorSupplyReplayV3SummaryForTest,
  buildGeneratorSupplyReplayV3TestFixture,
  type GeneratorSupplyReplayV3Contract,
  type GeneratorSupplyReplayV3Expected,
} from "./platform-generator-supply-replay-v3";
import {
  SUCCESSOR_V3_CORE_GENERATOR_OUTPUT_PATHS,
  SUCCESSOR_V3_PROJECTION_EXCLUSIONS,
  SUCCESSOR_V3_REPLAY_RECEIPT_PATHS,
} from "./platform-successor-dag-v3";
import type { JsonRecord } from "./platform-json-semantics";

const SUCCESSOR_V3_DERIVED_REPLAY_SUMMARY_PATH = SUCCESSOR_V3_REPLAY_RECEIPT_PATHS[0];
const SUCCESSOR_V3_RAW_REPLAY_RECEIPT_PATHS = SUCCESSOR_V3_REPLAY_RECEIPT_PATHS.slice(1);

const repositoryRoot = resolve(import.meta.dirname, "../..");
const temporaryRoots: string[] = [];
const authorityPaths = {
  wrapper: "scripts/replay-platform-generators-isolated-v3.sh",
  runner: "scripts/replay-platform-generators-v3.ts",
  pathHelper: "scripts/lib/generator-replay-path-authority.ts",
  archiveInspector: "scripts/lib/inspect-generator-replay-archive.py",
} as const;
const immutableFixturePaths = [
  "tools/generator-supply/v1/source.json",
  "tools/generator-supply/v1/evidence-manifest.json",
  "tools/generator-supply/v1/evidence/npm.json",
  "tools/generator-supply/v1/evidence/artifacts.json",
  "tools/generator-supply/v1/evidence/wheels.json",
  "tools/generator-supply/v1/evidence/ubuntu-image-binding.json",
  "tools/generator-supply/v1/evidence/replay/darwin-a.json",
  "tools/generator-supply/v1/evidence/replay/darwin-b.json",
  "tools/generator-supply/v1/evidence/replay/darwin-isolation.json",
  "tools/generator-supply/v1/evidence/replay/linux-a.json",
  "tools/generator-supply/v1/evidence/replay/linux-b.json",
  "tools/generator-supply/v1/evidence/replay/linux-isolation.json",
  "tools/generator-supply/v1/evidence/replay/projection.json",
] as const;

type Fixture = {
  readonly root: string;
  readonly expected: GeneratorSupplyReplayV3Expected;
  readonly receipts: Record<string, JsonRecord>;
};

afterEach(() => {
  for (const root of temporaryRoots.splice(0)) rmSync(root, { recursive: true, force: true });
});

function createFixture(): Fixture {
  const root = mkdtempSync(join(tmpdir(), "generator-supply-replay-v3-"));
  temporaryRoots.push(root);
  for (const path of [...Object.values(authorityPaths), ...immutableFixturePaths]) copy(root, path);
  const built = buildGeneratorSupplyReplayV3TestFixture(root, buildContract());
  const receipts = clone(built.receipts) as Record<string, JsonRecord>;
  const fixture = { root, expected: built.expected, receipts };
  writeReceipts(fixture);
  return fixture;
}

function buildContract(): GeneratorSupplyReplayV3Contract {
  const authorityFiles = Object.fromEntries(
    Object.entries(authorityPaths).map(([name, path]) => {
      const bytes = readFileSync(resolve(repositoryRoot, path));
      return [
        name,
        {
          path,
          sha256: createHash("sha256").update(bytes).digest("hex"),
          sizeBytes: bytes.byteLength,
        },
      ];
    }),
  ) as GeneratorSupplyReplayV3Contract["authorityFiles"];
  return {
    authorityFiles,
    coreGeneratorOutputs: SUCCESSOR_V3_CORE_GENERATOR_OUTPUT_PATHS.map((path) => ({
      path,
      mode: "100644",
      gitBlob: "0".repeat(40),
      sha256: "0".repeat(64),
      sizeBytes: 1,
    })),
    preReplayExclusionPolicy: "EXACT17_ONLY_NO_WILDCARD_ALL_OTHER_TRACKED_BYTES_INCLUDED",
    wrapperPolicy: "VERSIONED_ISOLATION_WRAPPER_V3",
    authoritativeReplayScope: "EXACT49_CORE_OUTPUTS_SUPPLY_PROFILE_AND_LOCK_POST_ASSEMBLY",
    algorithms: {
      nodeModulesManifest: "utf8-bytewise-sorted-path-nul-sha256-nul-git-mode-v1",
      projectionArchiveMemberManifest:
        "utf8-bytewise-sorted-path-type-mode-size-sha256-linktarget-nul-v1",
      inputTreeManifest: "utf8-bytewise-sorted-path-mode-size-sha256-nul-v1",
    },
    projectionExclusions: [...SUCCESSOR_V3_PROJECTION_EXCLUSIONS],
    receiptFormats: {
      summary: "cloud-agents-generator-supply-replay-summary/v3",
      run: "cloud-agents-generator-replay-run/v1",
      isolation: "cloud-agents-generator-replay-isolation/v1",
      projection: "cloud-agents-core-generator-projection/v1",
    },
  };
}

function copy(root: string, path: string): void {
  const target = resolve(root, path);
  mkdirSync(dirname(target), { recursive: true });
  copyFileSync(resolve(repositoryRoot, path), target);
}

function writeReceipts(fixture: Fixture): void {
  for (const path of SUCCESSOR_V3_REPLAY_RECEIPT_PATHS)
    writeJson(fixture.root, path, fixture.receipts[path]);
}

function rawReceiptMap(fixture: Fixture): Map<string, Buffer> {
  return new Map(
    SUCCESSOR_V3_RAW_REPLAY_RECEIPT_PATHS.map((path) => [
      path,
      readFileSync(resolve(fixture.root, path)),
    ]),
  );
}

function refreshRunBindingsAndSummary(fixture: Fixture): void {
  const [, darwinA, darwinB, darwinIsolation, linuxA, linuxB, linuxIsolation] =
    SUCCESSOR_V3_REPLAY_RECEIPT_PATHS;
  for (const [isolation, a, b] of [
    [darwinIsolation, darwinA, darwinB],
    [linuxIsolation, linuxA, linuxB],
  ] as const) {
    fixture.receipts[isolation]!.runReportSha256 = {
      a: sha256(serialize(fixture.receipts[a])),
      b: sha256(serialize(fixture.receipts[b])),
    };
  }
  refreshSummary(fixture);
}

function refreshSummary(fixture: Fixture): void {
  const raw = Object.fromEntries(
    SUCCESSOR_V3_REPLAY_RECEIPT_PATHS.slice(1).map((path) => [
      path,
      {
        value: fixture.receipts[path]!,
        bytes: serialize(fixture.receipts[path]),
      },
    ]),
  );
  fixture.receipts[SUCCESSOR_V3_REPLAY_RECEIPT_PATHS[0]] =
    buildGeneratorSupplyReplayV3SummaryForTest(raw, fixture.expected);
  writeReceipts(fixture);
}

function writeJson(root: string, path: string, value: unknown): void {
  const target = resolve(root, path);
  mkdirSync(dirname(target), { recursive: true });
  writeFileSync(target, serialize(value));
}

function serialize(value: unknown): Buffer {
  return Buffer.from(`${JSON.stringify(value, null, 2)}\n`);
}

function sha256(bytes: Buffer): string {
  return `sha256:${createHash("sha256").update(bytes).digest("hex")}`;
}

function clone<T>(value: T): T {
  return JSON.parse(JSON.stringify(value)) as T;
}

function expectInvalid(action: () => unknown, path?: string): void {
  expect(action).toThrowError(
    expect.objectContaining({
      code: "GENERATOR_SUPPLY_REPLAY_V3_INVALID",
      ...(path ? { path: expect.stringContaining(path) } : {}),
    }),
  );
}

describe("generator-supply v3 exact receipt semantics", () => {
  it("requires the v3 wrapper, replay scope, and ordered exact17 exclusions", () => {
    const fixture = createFixture();
    const legacyWrapper = {
      ...fixture.expected.replayContract,
      wrapperPolicy: "VERSIONED_ISOLATION_WRAPPER_V2",
    };
    expectInvalid(() => assertGeneratorSupplyReplayV3ContractCurrent(fixture.root, legacyWrapper));

    const shortExclusions = {
      ...fixture.expected.replayContract,
      projectionExclusions: fixture.expected.replayContract.projectionExclusions.slice(0, -1),
    };
    expectInvalid(() =>
      assertGeneratorSupplyReplayV3ContractCurrent(fixture.root, shortExclusions),
    );
  });

  it("prepares the canonical derived summary and exact ordered eight from caller raw bytes", () => {
    const fixture = createFixture();
    const raw = rawReceiptMap(fixture);
    const prepared = buildGeneratorSupplyReplayV3PreparedReceipts(
      fixture.root,
      fixture.expected.replayContract,
      raw,
    );

    expect([...prepared.receipts.keys()]).toEqual([...SUCCESSOR_V3_REPLAY_RECEIPT_PATHS]);
    expect(prepared.receiptRecords.map(({ path }) => path)).toEqual([
      ...SUCCESSOR_V3_REPLAY_RECEIPT_PATHS,
    ]);
    for (const path of SUCCESSOR_V3_RAW_REPLAY_RECEIPT_PATHS) {
      expect(prepared.receipts.get(path)).not.toBe(raw.get(path));
      expect(prepared.receipts.get(path)).toEqual(raw.get(path));
    }
    expect(prepared.receipts.get(SUCCESSOR_V3_DERIVED_REPLAY_SUMMARY_PATH)).toEqual(
      serialize(fixture.receipts[SUCCESSOR_V3_DERIVED_REPLAY_SUMMARY_PATH]),
    );
    expect(prepared.candidateManifestSha256).toBe(
      fixture.receipts[SUCCESSOR_V3_RAW_REPLAY_RECEIPT_PATHS[0]]!.candidateManifestSha256,
    );
    expect(prepared.outputFiles).toBe(SUCCESSOR_V3_CORE_GENERATOR_OUTPUT_PATHS.length);
    expect(Object.isFrozen(prepared)).toBe(true);
    expect(Object.isFrozen(prepared.receipts)).toBe(true);
    expect(Object.isFrozen(prepared.receiptRecords)).toBe(true);
    expect(prepared.receiptRecords.every((record) => Object.isFrozen(record))).toBe(true);
    expect(Object.isFrozen(prepared.projection)).toBe(true);
    expect(() => prepared.assertInputSnapshotCurrent()).not.toThrow();
    expect(() => prepared.assertPreparedSnapshotCurrent()).not.toThrow();
  });

  it("isolates prepared authority from caller and returned Buffer mutation", () => {
    const fixture = createFixture();
    const raw = rawReceiptMap(fixture);
    const prepared = buildGeneratorSupplyReplayV3PreparedReceipts(
      fixture.root,
      fixture.expected.replayContract,
      raw,
    );
    const path = SUCCESSOR_V3_RAW_REPLAY_RECEIPT_PATHS[0];
    const fixed = prepared.receipts.get(path)!;

    raw.get(path)!.fill(0);
    expect(prepared.receipts.get(path)).toEqual(fixed);

    const returned = prepared.receipts.get(path)!;
    returned.fill(1);
    expect(prepared.receipts.get(path)).toEqual(fixed);

    for (const [candidatePath, bytes] of prepared.receipts) {
      if (candidatePath === path) bytes.fill(2);
    }
    expect(prepared.receipts.get(path)).toEqual(fixed);
    expect(() => prepared.assertPreparedSnapshotCurrent()).not.toThrow();
  });

  it("rejects SharedArrayBuffer-backed caller receipt authority", () => {
    if (typeof SharedArrayBuffer === "undefined") return;
    const fixture = createFixture();
    const raw = rawReceiptMap(fixture);
    const path = SUCCESSOR_V3_RAW_REPLAY_RECEIPT_PATHS[0];
    const original = raw.get(path)!;
    const shared = new SharedArrayBuffer(original.byteLength);
    const sharedBytes = Buffer.from(shared);
    original.copy(sharedBytes);
    raw.set(path, sharedBytes);
    expectInvalid(
      () =>
        buildGeneratorSupplyReplayV3PreparedReceipts(
          fixture.root,
          fixture.expected.replayContract,
          raw,
        ),
      path,
    );
  });

  it("rejects missing, extra, and path-swapped caller raw receipt mappings", () => {
    const missingFixture = createFixture();
    const missing = rawReceiptMap(missingFixture);
    missing.delete(SUCCESSOR_V3_RAW_REPLAY_RECEIPT_PATHS[0]);
    expectInvalid(() =>
      buildGeneratorSupplyReplayV3PreparedReceipts(
        missingFixture.root,
        missingFixture.expected.replayContract,
        missing,
      ),
    );

    const extraFixture = createFixture();
    const extra = rawReceiptMap(extraFixture);
    extra.set("tools/generator-supply/v3/evidence/replay/extra.json", Buffer.from("{}\n"));
    expectInvalid(() =>
      buildGeneratorSupplyReplayV3PreparedReceipts(
        extraFixture.root,
        extraFixture.expected.replayContract,
        extra,
      ),
    );

    const swappedFixture = createFixture();
    const swapped = rawReceiptMap(swappedFixture);
    const darwinA = SUCCESSOR_V3_RAW_REPLAY_RECEIPT_PATHS[0];
    const darwinB = SUCCESSOR_V3_RAW_REPLAY_RECEIPT_PATHS[1];
    const aBytes = swapped.get(darwinA)!;
    swapped.set(darwinA, swapped.get(darwinB)!);
    swapped.set(darwinB, aBytes);
    expectInvalid(
      () =>
        buildGeneratorSupplyReplayV3PreparedReceipts(
          swappedFixture.root,
          swappedFixture.expected.replayContract,
          swapped,
        ),
      "replayRun",
    );
  });

  it("terminal-fences authority and immutable predecessor inputs captured by the production builder", () => {
    const fixture = createFixture();
    const prepared = buildGeneratorSupplyReplayV3PreparedReceipts(
      fixture.root,
      fixture.expected.replayContract,
      rawReceiptMap(fixture),
    );
    const wrapper = resolve(fixture.root, authorityPaths.wrapper);
    writeFileSync(wrapper, Buffer.concat([readFileSync(wrapper), Buffer.from("\n# drift\n")]));
    expectInvalid(() => prepared.assertInputSnapshotCurrent(), authorityPaths.wrapper);

    const v1Fixture = createFixture();
    const v1Prepared = buildGeneratorSupplyReplayV3PreparedReceipts(
      v1Fixture.root,
      v1Fixture.expected.replayContract,
      rawReceiptMap(v1Fixture),
    );
    const sourcePath = "tools/generator-supply/v1/source.json";
    const source = resolve(v1Fixture.root, sourcePath);
    writeFileSync(source, Buffer.concat([readFileSync(source), Buffer.from("\n")]));
    expectInvalid(() => v1Prepared.assertInputSnapshotCurrent(), sourcePath);
  });

  it("accepts a constructable exact-eight fixture bound to immutable predecessor material", () => {
    const fixture = createFixture();
    const result = assertGeneratorSupplyReplayV3Receipts(fixture.root, fixture.expected);
    expect(result.projection.projectionTreeSha).not.toBe(
      fixture.expected.predecessorProjectionTreeSha,
    );
    expect(result.candidateManifestSha256).toBe(
      fixture.receipts[SUCCESSOR_V3_REPLAY_RECEIPT_PATHS[1]]!.candidateManifestSha256,
    );
    expect(result.outputFiles).toBe(SUCCESSOR_V3_CORE_GENERATOR_OUTPUT_PATHS.length);
    expect(Object.keys(result.receiptSha256)).toEqual([...SUCCESSOR_V3_REPLAY_RECEIPT_PATHS]);
  });

  it("rejects placeholder JSON even when every outer receipt file is recomputed", () => {
    const fixture = createFixture();
    for (const path of SUCCESSOR_V3_REPLAY_RECEIPT_PATHS) {
      fixture.receipts[path] = { formatVersion: "placeholder/v1", notGateClosure: true };
    }
    writeReceipts(fixture);
    expectInvalid(() => assertGeneratorSupplyReplayV3Receipts(fixture.root, fixture.expected));
  });

  it("rejects A/B run drift after all downstream raw hashes are refreshed", () => {
    const fixture = createFixture();
    fixture.receipts[SUCCESSOR_V3_REPLAY_RECEIPT_PATHS[1]]!.runnerEnvironmentSanitized = false;
    refreshRunBindingsAndSummary(fixture);
    expectInvalid(
      () => assertGeneratorSupplyReplayV3Receipts(fixture.root, fixture.expected),
      "runnerEnvironmentSanitized",
    );
  });

  it("rejects forged output cardinality and summary candidate identity", () => {
    const outputFixture = createFixture();
    outputFixture.receipts[SUCCESSOR_V3_REPLAY_RECEIPT_PATHS[1]]!.outputFiles =
      SUCCESSOR_V3_CORE_GENERATOR_OUTPUT_PATHS.length - 1;
    refreshRunBindingsAndSummary(outputFixture);
    expectInvalid(
      () => assertGeneratorSupplyReplayV3Receipts(outputFixture.root, outputFixture.expected),
      "darwin-arm64",
    );

    const summaryFixture = createFixture();
    const summary = summaryFixture.receipts[SUCCESSOR_V3_REPLAY_RECEIPT_PATHS[0]]!;
    summary.candidateManifestSha256 = `sha256:${"f".repeat(64)}`;
    writeReceipts(summaryFixture);
    expectInvalid(
      () => assertGeneratorSupplyReplayV3Receipts(summaryFixture.root, summaryFixture.expected),
      "replay.json",
    );
  });

  it("rejects isolation raw-hash drift and same-boundary probe drift", () => {
    const hashFixture = createFixture();
    const darwinIsolation = hashFixture.receipts[SUCCESSOR_V3_REPLAY_RECEIPT_PATHS[3]]!;
    (darwinIsolation.runReportSha256 as JsonRecord).a = `sha256:${"0".repeat(64)}`;
    refreshSummary(hashFixture);
    expectInvalid(
      () => assertGeneratorSupplyReplayV3Receipts(hashFixture.root, hashFixture.expected),
      "runReportSha256",
    );

    const probeFixture = createFixture();
    const linuxIsolation = probeFixture.receipts[SUCCESSOR_V3_REPLAY_RECEIPT_PATHS[6]]!;
    const probes = linuxIsolation.probes as Record<string, Record<string, JsonRecord>>;
    for (const run of ["a", "b"]) probes[run]!.rootfs!.stderr = "permission denied";
    refreshSummary(probeFixture);
    expectInvalid(
      () => assertGeneratorSupplyReplayV3Receipts(probeFixture.root, probeFixture.expected),
      "rootfs",
    );
  });

  it("rejects projection exclusion order and every nonzero safety counter", () => {
    const exclusion = createFixture();
    const projection = exclusion.receipts[SUCCESSOR_V3_REPLAY_RECEIPT_PATHS[7]]!;
    (projection.excluded as string[]).reverse();
    refreshSummary(exclusion);
    expectInvalid(
      () => assertGeneratorSupplyReplayV3Receipts(exclusion.root, exclusion.expected),
      "excluded",
    );

    for (const counter of [
      "symlinks",
      "hardlinks",
      "unsafeEntries",
      "duplicateEntries",
      "specialEntries",
      "linkPrefixDescendants",
      "linkCycles",
    ]) {
      const safety = createFixture();
      const inspection = safety.receipts[SUCCESSOR_V3_REPLAY_RECEIPT_PATHS[7]]!
        .archiveInspection as JsonRecord;
      inspection[counter] = 1;
      refreshSummary(safety);
      expectInvalid(
        () => assertGeneratorSupplyReplayV3Receipts(safety.root, safety.expected),
        counter,
      );
    }
  });

  it("rejects a stale summary hash, unknown run key, and receipt version drift", () => {
    const stale = createFixture();
    const summary = stale.receipts[SUCCESSOR_V3_REPLAY_RECEIPT_PATHS[0]]!;
    (summary.runReportSha256 as JsonRecord)["darwin-a"] = `sha256:${"f".repeat(64)}`;
    writeReceipts(stale);
    expectInvalid(
      () => assertGeneratorSupplyReplayV3Receipts(stale.root, stale.expected),
      "replay.json",
    );

    const unknown = createFixture();
    unknown.receipts[SUCCESSOR_V3_REPLAY_RECEIPT_PATHS[1]]!.unexpected = true;
    refreshRunBindingsAndSummary(unknown);
    expectInvalid(() => assertGeneratorSupplyReplayV3Receipts(unknown.root, unknown.expected));

    const version = createFixture();
    version.receipts[SUCCESSOR_V3_REPLAY_RECEIPT_PATHS[3]]!.formatVersion =
      "cloud-agents-generator-replay-isolation/v2";
    refreshSummary(version);
    expectInvalid(
      () => assertGeneratorSupplyReplayV3Receipts(version.root, version.expected),
      "formatVersion",
    );
  });

  it("stable-reads and verifies every source-bound authority file", () => {
    const fixture = createFixture();
    const wrapper = resolve(fixture.root, authorityPaths.wrapper);
    writeFileSync(wrapper, Buffer.concat([readFileSync(wrapper), Buffer.from("\n# drift\n")]));
    expectInvalid(
      () => assertGeneratorSupplyReplayV3Receipts(fixture.root, fixture.expected),
      authorityPaths.wrapper,
    );
  });

  it("rejects an early authority changed after all four authority files were captured", () => {
    const fixture = createFixture();
    const path = authorityPaths.wrapper;
    expectInvalid(
      () =>
        assertGeneratorSupplyReplayV3InputSnapshotMutationForTest(
          fixture.root,
          fixture.expected,
          "authority",
          () => {
            const absolute = resolve(fixture.root, path);
            const replacement = `${absolute}.replacement`;
            writeFileSync(replacement, readFileSync(absolute));
            renameSync(replacement, absolute);
          },
        ),
      path,
    );
  });

  it("rejects early v1 source and npm inputs changed after manifest-plus-seven snapshot", () => {
    for (const path of [
      "tools/generator-supply/v1/source.json",
      "tools/generator-supply/v1/evidence/npm.json",
    ]) {
      const fixture = createFixture();
      expectInvalid(
        () =>
          assertGeneratorSupplyReplayV3InputSnapshotMutationForTest(
            fixture.root,
            fixture.expected,
            "v1",
            () => {
              const absolute = resolve(fixture.root, path);
              writeFileSync(absolute, Buffer.concat([readFileSync(absolute), Buffer.from("\n")]));
            },
          ),
        path,
      );
    }
  });

  it("rejects a v1 parent-directory ABA between fixed manifest and derived reads", () => {
    const fixture = createFixture();
    const path = "tools/generator-supply/v1/evidence/npm.json";
    const live = resolve(fixture.root, "tools/generator-supply/v1");
    const parked = `${live}.original`;
    const alternate = `${live}.alternate`;
    cpSync(live, alternate, { recursive: true });
    const alternateNpm = resolve(alternate, "evidence/npm.json");
    writeFileSync(alternateNpm, Buffer.concat([readFileSync(alternateNpm), Buffer.from(" ")]));

    expectInvalid(
      () =>
        assertGeneratorSupplyReplayV3V2DerivedABAMutationForTest(
          fixture.root,
          fixture.expected.replayContract,
          path,
          () => {
            renameSync(live, parked);
            renameSync(alternate, live);
          },
          () => {
            renameSync(live, alternate);
            renameSync(parked, live);
          },
        ),
      path,
    );
  });

  it("rejects a receipt changed after the eight-file snapshot was captured", () => {
    const fixture = createFixture();
    const path = SUCCESSOR_V3_REPLAY_RECEIPT_PATHS[0];
    expectInvalid(
      () =>
        assertGeneratorSupplyReplayV3SnapshotMutationForTest(fixture.root, fixture.expected, () => {
          writeFileSync(resolve(fixture.root, path), Buffer.from("{}\n"));
        }),
      path,
    );
  });
});
