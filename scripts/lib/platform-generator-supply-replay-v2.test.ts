import { createHash } from "node:crypto";
import {
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
  assertGeneratorSupplyReplayV2InputSnapshotMutationForTest,
  assertGeneratorSupplyReplayV2Receipts,
  assertGeneratorSupplyReplayV2SnapshotMutationForTest,
  buildGeneratorSupplyReplayV2SummaryForTest,
  buildGeneratorSupplyReplayV2TestFixture,
  type GeneratorSupplyReplayV2Contract,
  type GeneratorSupplyReplayV2Expected,
} from "./platform-generator-supply-replay-v2";
import {
  SUCCESSOR_PROJECTION_EXCLUSIONS,
  SUCCESSOR_REPLAY_RECEIPT_PATHS,
} from "./platform-successor-dag";
import type { JsonRecord } from "./platform-json-semantics";

const repositoryRoot = resolve(import.meta.dirname, "../..");
const temporaryRoots: string[] = [];
const authorityPaths = {
  wrapper: "scripts/replay-platform-generators-isolated.sh",
  runner: "scripts/replay-platform-generators.ts",
  pathHelper: "scripts/lib/generator-replay-path-authority.ts",
  archiveInspector: "scripts/lib/inspect-generator-replay-archive.py",
} as const;
const immutableFixturePaths = [
  "tools/generator-supply/v1/source.json",
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
  readonly expected: GeneratorSupplyReplayV2Expected;
  readonly receipts: Record<string, JsonRecord>;
};

afterEach(() => {
  for (const root of temporaryRoots.splice(0)) rmSync(root, { recursive: true, force: true });
});

function createFixture(): Fixture {
  const root = mkdtempSync(join(tmpdir(), "generator-supply-replay-v2-"));
  temporaryRoots.push(root);
  for (const path of [...Object.values(authorityPaths), ...immutableFixturePaths]) copy(root, path);
  const built = buildGeneratorSupplyReplayV2TestFixture(root, buildContract());
  const receipts = clone(built.receipts) as Record<string, JsonRecord>;
  const fixture = { root, expected: built.expected, receipts };
  writeReceipts(fixture);
  return fixture;
}

function buildContract(): GeneratorSupplyReplayV2Contract {
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
  ) as GeneratorSupplyReplayV2Contract["authorityFiles"];
  return {
    authorityFiles,
    wrapperPolicy: "VERSIONED_ISOLATION_WRAPPER_V1",
    authoritativeReplayScope: "CORE_GENERATORS_ONLY_SUPPLY_PROFILE_AND_LOCK_POST_ASSEMBLY",
    algorithms: {
      nodeModulesManifest: "utf8-bytewise-sorted-path-nul-sha256-nul-git-mode-v1",
      projectionArchiveMemberManifest:
        "utf8-bytewise-sorted-path-type-mode-size-sha256-linktarget-nul-v1",
      inputTreeManifest: "utf8-bytewise-sorted-path-mode-size-sha256-nul-v1",
    },
    projectionExclusions: [...SUCCESSOR_PROJECTION_EXCLUSIONS],
    receiptFormats: {
      summary: "cloud-agents-generator-supply-replay-summary/v2",
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
  for (const path of SUCCESSOR_REPLAY_RECEIPT_PATHS)
    writeJson(fixture.root, path, fixture.receipts[path]);
}

function refreshRunBindingsAndSummary(fixture: Fixture): void {
  const [, darwinA, darwinB, darwinIsolation, linuxA, linuxB, linuxIsolation] =
    SUCCESSOR_REPLAY_RECEIPT_PATHS;
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
    SUCCESSOR_REPLAY_RECEIPT_PATHS.slice(1).map((path) => [
      path,
      {
        value: fixture.receipts[path]!,
        bytes: serialize(fixture.receipts[path]),
      },
    ]),
  );
  fixture.receipts[SUCCESSOR_REPLAY_RECEIPT_PATHS[0]] = buildGeneratorSupplyReplayV2SummaryForTest(
    raw,
    fixture.expected,
  );
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
      code: "GENERATOR_SUPPLY_REPLAY_V2_INVALID",
      ...(path ? { path: expect.stringContaining(path) } : {}),
    }),
  );
}

describe("generator-supply v2 exact receipt semantics", () => {
  it("accepts a constructable exact-eight fixture bound to immutable v1 material", () => {
    const fixture = createFixture();
    const result = assertGeneratorSupplyReplayV2Receipts(fixture.root, fixture.expected);
    expect(result.projection.projectionTreeSha).not.toBe(
      fixture.expected.predecessorProjectionTreeSha,
    );
    expect(Object.keys(result.receiptSha256)).toEqual([...SUCCESSOR_REPLAY_RECEIPT_PATHS]);
  });

  it("rejects placeholder JSON even when every outer receipt file is recomputed", () => {
    const fixture = createFixture();
    for (const path of SUCCESSOR_REPLAY_RECEIPT_PATHS) {
      fixture.receipts[path] = { formatVersion: "placeholder/v1", notGateClosure: true };
    }
    writeReceipts(fixture);
    expectInvalid(() => assertGeneratorSupplyReplayV2Receipts(fixture.root, fixture.expected));
  });

  it("rejects A/B run drift after all downstream raw hashes are refreshed", () => {
    const fixture = createFixture();
    fixture.receipts[SUCCESSOR_REPLAY_RECEIPT_PATHS[1]]!.runnerEnvironmentSanitized = false;
    refreshRunBindingsAndSummary(fixture);
    expectInvalid(
      () => assertGeneratorSupplyReplayV2Receipts(fixture.root, fixture.expected),
      "runnerEnvironmentSanitized",
    );
  });

  it("rejects isolation raw-hash drift and same-boundary probe drift", () => {
    const hashFixture = createFixture();
    const darwinIsolation = hashFixture.receipts[SUCCESSOR_REPLAY_RECEIPT_PATHS[3]]!;
    (darwinIsolation.runReportSha256 as JsonRecord).a = `sha256:${"0".repeat(64)}`;
    refreshSummary(hashFixture);
    expectInvalid(
      () => assertGeneratorSupplyReplayV2Receipts(hashFixture.root, hashFixture.expected),
      "runReportSha256",
    );

    const probeFixture = createFixture();
    const linuxIsolation = probeFixture.receipts[SUCCESSOR_REPLAY_RECEIPT_PATHS[6]]!;
    const probes = linuxIsolation.probes as Record<string, Record<string, JsonRecord>>;
    for (const run of ["a", "b"]) probes[run]!.rootfs!.stderr = "permission denied";
    refreshSummary(probeFixture);
    expectInvalid(
      () => assertGeneratorSupplyReplayV2Receipts(probeFixture.root, probeFixture.expected),
      "rootfs",
    );
  });

  it("rejects projection exclusion order and every nonzero safety counter", () => {
    const exclusion = createFixture();
    const projection = exclusion.receipts[SUCCESSOR_REPLAY_RECEIPT_PATHS[7]]!;
    (projection.excluded as string[]).reverse();
    refreshSummary(exclusion);
    expectInvalid(
      () => assertGeneratorSupplyReplayV2Receipts(exclusion.root, exclusion.expected),
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
      const inspection = safety.receipts[SUCCESSOR_REPLAY_RECEIPT_PATHS[7]]!
        .archiveInspection as JsonRecord;
      inspection[counter] = 1;
      refreshSummary(safety);
      expectInvalid(
        () => assertGeneratorSupplyReplayV2Receipts(safety.root, safety.expected),
        counter,
      );
    }
  });

  it("rejects a stale summary hash, unknown run key, and receipt version drift", () => {
    const stale = createFixture();
    const summary = stale.receipts[SUCCESSOR_REPLAY_RECEIPT_PATHS[0]]!;
    (summary.runReportSha256 as JsonRecord)["darwin-a"] = `sha256:${"f".repeat(64)}`;
    writeReceipts(stale);
    expectInvalid(
      () => assertGeneratorSupplyReplayV2Receipts(stale.root, stale.expected),
      "replay.json",
    );

    const unknown = createFixture();
    unknown.receipts[SUCCESSOR_REPLAY_RECEIPT_PATHS[1]]!.unexpected = true;
    refreshRunBindingsAndSummary(unknown);
    expectInvalid(() => assertGeneratorSupplyReplayV2Receipts(unknown.root, unknown.expected));

    const version = createFixture();
    version.receipts[SUCCESSOR_REPLAY_RECEIPT_PATHS[3]]!.formatVersion =
      "cloud-agents-generator-replay-isolation/v2";
    refreshSummary(version);
    expectInvalid(
      () => assertGeneratorSupplyReplayV2Receipts(version.root, version.expected),
      "formatVersion",
    );
  });

  it("stable-reads and verifies every source-bound authority file", () => {
    const fixture = createFixture();
    const wrapper = resolve(fixture.root, authorityPaths.wrapper);
    writeFileSync(wrapper, Buffer.concat([readFileSync(wrapper), Buffer.from("\n# drift\n")]));
    expectInvalid(
      () => assertGeneratorSupplyReplayV2Receipts(fixture.root, fixture.expected),
      authorityPaths.wrapper,
    );
  });

  it("rejects an early authority changed after all four authority files were captured", () => {
    const fixture = createFixture();
    const path = authorityPaths.wrapper;
    expectInvalid(
      () =>
        assertGeneratorSupplyReplayV2InputSnapshotMutationForTest(
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

  it("rejects early v1 source and npm inputs changed after the seven-file snapshot", () => {
    for (const path of [
      "tools/generator-supply/v1/source.json",
      "tools/generator-supply/v1/evidence/npm.json",
    ]) {
      const fixture = createFixture();
      expectInvalid(
        () =>
          assertGeneratorSupplyReplayV2InputSnapshotMutationForTest(
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

  it("rejects a receipt changed after the eight-file snapshot was captured", () => {
    const fixture = createFixture();
    const path = SUCCESSOR_REPLAY_RECEIPT_PATHS[0];
    expectInvalid(
      () =>
        assertGeneratorSupplyReplayV2SnapshotMutationForTest(fixture.root, fixture.expected, () => {
          writeFileSync(resolve(fixture.root, path), Buffer.from("{}\n"));
        }),
      path,
    );
  });
});
