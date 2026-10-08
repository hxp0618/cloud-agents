import { createHash } from "node:crypto";
import {
  cpSync,
  existsSync,
  mkdirSync,
  mkdtempSync,
  readFileSync,
  realpathSync,
  rmSync,
  symlinkSync,
  writeFileSync,
} from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";

import { afterEach, describe, expect, it, vi } from "vitest";

import {
  EXTERNAL_CONSUMER_V3_SOURCE_PATH,
  ExternalConsumerV3Source,
  fileRecord,
} from "../../scripts/lib/platform-g-contract-external-consumer-v3";
import * as externalAuthority from "../../scripts/lib/platform-g-contract-external-consumer-v3";
import {
  assertExternalConsumerV3SummaryCurrent,
  writeExternalConsumerV3Summary,
} from "../../scripts/lib/platform-g-contract-external-consumer-v3-summary";
import { encodeExternalConsumerV3Ustar } from "../../scripts/lib/platform-g-contract-external-consumer-v3-ustar";
import * as nativeReplay from "../../scripts/lib/platform-g-contract-external-consumer-v3-native";

const authorityRoot = new URL("../..", import.meta.url).pathname;
const evidenceDirectory = "tools/g-contract-external-consumer/v3/evidence/replay";
const projectionPath = `${evidenceDirectory}/projection.json`;
const archivePath = `${evidenceDirectory}/projection.tar`;
const memberManifestPath = `${evidenceDirectory}/projection.member-manifest.json`;
const profilePath = "tools/g-contract-external-consumer/v3/profile.json";
const summaryPath = `${evidenceDirectory}/replay.json`;
const nativeSpecs = [
  ["darwin-arm64-A", "darwin-arm64", "A", `${evidenceDirectory}/darwin-arm64-a.json`],
  ["darwin-arm64-B", "darwin-arm64", "B", `${evidenceDirectory}/darwin-arm64-b.json`],
  ["linux-amd64-A", "linux-amd64", "A", `${evidenceDirectory}/linux-amd64-a.json`],
  ["linux-amd64-B", "linux-amd64", "B", `${evidenceDirectory}/linux-amd64-b.json`],
] as const;

const temporaryRoots: string[] = [];

afterEach(() => {
  for (const root of temporaryRoots.splice(0)) rmSync(root, { recursive: true, force: true });
});

describe("G-CONTRACT external-consumer v3 summary", () => {
  it("copies verified evidence and writes a current, independently checkable bundle", () => {
    const fixture = createFixture();
    const result = writeExternalConsumerV3Summary(fixture.options);

    expect(result.summaryPath).toBe(join(fixture.outputRoot, summaryPath));
    expect(result.profilePath).toBe(join(fixture.outputRoot, profilePath));
    expect(readFileSync(join(fixture.outputRoot, projectionPath))).toEqual(
      readFileSync(join(fixture.projectionRoot, projectionPath)),
    );
    for (const [key, , , path] of nativeSpecs) {
      expect(readFileSync(join(fixture.outputRoot, path))).toEqual(
        readFileSync(join(fixture.nativeRoots[key], path)),
      );
    }

    const summary = readJson(join(fixture.outputRoot, summaryPath));
    const profile = readJson(join(fixture.outputRoot, profilePath));
    expect(summary.status).toBe("CURRENT");
    expect(summary.platformRuns).toHaveLength(4);
    expect(profile.status).toBe("PROFILE_CURRENT_FINAL_REVIEW_PENDING");
    expect(profile.stateMachine).toBe("PROFILE_CURRENT");
    expect(profile.inputBindings).toEqual(fixture.source.semanticInputs);
    expect(profile.receiptBindings).toHaveLength(6);

    const before = snapshotTree(fixture.outputRoot);
    expect(() =>
      assertExternalConsumerV3SummaryCurrent({
        authorityRoot,
        outputRoot: fixture.outputRoot,
      }),
    ).not.toThrow();
    expect(snapshotTree(fixture.outputRoot)).toEqual(before);
  });

  it("copies the projection receipt bytes returned by the verifier without rereading the path", () => {
    const fixture = createFixture();
    const sourceCapture = externalAuthority.readCurrentExternalConsumerV3Source(authorityRoot);
    const verified = nativeReplay.readVerifiedExternalConsumerV3Projection(
      authorityRoot,
      fixture.projectionRoot,
      sourceCapture.source,
      sourceCapture.sourceBinding,
    );
    const verifiedBytes = Buffer.from(verified.projectionReceiptBytes);
    writeFileSync(
      join(fixture.projectionRoot, projectionPath),
      Buffer.concat([verifiedBytes, Buffer.from("\n")]),
    );
    const verifier = vi
      .spyOn(nativeReplay, "readVerifiedExternalConsumerV3Projection")
      .mockReturnValue(verified);
    try {
      writeExternalConsumerV3Summary(fixture.options);
    } finally {
      verifier.mockRestore();
    }
    expect(readFileSync(join(fixture.outputRoot, projectionPath))).toEqual(verifiedBytes);
  });

  it("uses one captured authority source without rereading its path", () => {
    const fixture = createFixture();
    const captured = externalAuthority.readCurrentExternalConsumerV3Source(authorityRoot);
    const fixtureAuthority = join(fixture.projectionRoot, "..", "authority");
    mkdirSync(fixtureAuthority);
    for (const path of [
      "tools/g-contract-external-consumer/v3/projection-receipt.schema.json",
      "tools/g-contract-external-consumer/v3/native-replay-receipt.schema.json",
      "tools/g-contract-external-consumer/v3/replay-summary-receipt.schema.json",
      "tools/g-contract-external-consumer/v3/profile.schema.json",
    ]) {
      write(join(fixtureAuthority, path), readFileSync(join(authorityRoot, path)));
    }
    write(join(fixtureAuthority, EXTERNAL_CONSUMER_V3_SOURCE_PATH), Buffer.from("changed\n"));
    const sourceReader = vi
      .spyOn(externalAuthority, "readCurrentExternalConsumerV3Source")
      .mockReturnValue(captured);
    try {
      writeExternalConsumerV3Summary({
        ...fixture.options,
        authorityRoot: fixtureAuthority,
      });
    } finally {
      sourceReader.mockRestore();
    }
    const profile = readJson(join(fixture.outputRoot, profilePath));
    expect(profile.sourceBinding).toEqual(captured.sourceBinding);
  });

  it("checks profile bindings from captured receipt bytes without rereading changed paths", () => {
    const fixture = createFixture();
    writeExternalConsumerV3Summary(fixture.options);
    const originalVerifier = nativeReplay.readVerifiedExternalConsumerV3Projection;
    const verifier = vi
      .spyOn(nativeReplay, "readVerifiedExternalConsumerV3Projection")
      .mockImplementation((...args) => {
        const verified = originalVerifier(...args);
        const receipt = join(fixture.outputRoot, projectionPath);
        writeFileSync(receipt, Buffer.concat([readFileSync(receipt), Buffer.from("\n")]));
        return verified;
      });
    try {
      expect(() =>
        assertExternalConsumerV3SummaryCurrent({
          authorityRoot,
          outputRoot: fixture.outputRoot,
        }),
      ).not.toThrow();
    } finally {
      verifier.mockRestore();
    }
  });

  it("rejects stale source and successor bindings", () => {
    const staleSource = createFixture();
    mutateJson(join(staleSource.projectionRoot, projectionPath), (projection) => {
      projection.sourceBinding.sha256 = digest(Buffer.from("stale"));
    });
    expect(() => writeExternalConsumerV3Summary(staleSource.options)).toThrow(/source binding/i);

    const staleSuccessor = createFixture();
    mutateJson(join(staleSuccessor.projectionRoot, projectionPath), (projection) => {
      projection.contractSuccessorBinding.outputManifestSha256 = digest(Buffer.from("stale"));
    });
    expect(() => writeExternalConsumerV3Summary(staleSuccessor.options)).toThrow(
      /successor binding/i,
    );
  });

  it("rejects altered projection bytes and missing native evidence", () => {
    const altered = createFixture();
    writeFileSync(join(altered.projectionRoot, archivePath), "altered");
    expect(() => writeExternalConsumerV3Summary(altered.options)).toThrow(/archive/i);

    const missing = createFixture();
    rmSync(join(missing.nativeRoots["linux-amd64-B"], nativeSpecs[3][3]));
    expect(() => writeExternalConsumerV3Summary(missing.options)).toThrow();
  });

  it("rejects duplicate run ownership and mismatched consumer results", () => {
    const duplicate = createFixture();
    mutateJson(join(duplicate.nativeRoots["darwin-arm64-B"], nativeSpecs[1][3]), (receipt) => {
      receipt.platform = "darwin-arm64";
      receipt.runId = "A";
    });
    expect(() => writeExternalConsumerV3Summary(duplicate.options)).toThrow(/identity|duplicate/i);

    const mismatch = createFixture();
    mutateJson(join(mismatch.nativeRoots["linux-amd64-B"], nativeSpecs[3][3]), (receipt) => {
      receipt.consumers.go.outputSha256 = digest(Buffer.from("different-go-result"));
    });
    expect(() => writeExternalConsumerV3Summary(mismatch.options)).toThrow(/consumer.*output/i);
  });

  it("rejects symlinked evidence, nested roots and an existing output target", () => {
    const symlinked = createFixture();
    const nativePath = join(symlinked.nativeRoots["darwin-arm64-A"], nativeSpecs[0][3]);
    const target = `${nativePath}.target`;
    cpSync(nativePath, target);
    rmSync(nativePath);
    symlinkSync(target, nativePath);
    expect(() => writeExternalConsumerV3Summary(symlinked.options)).toThrow(/symlink/i);

    const nested = createFixture();
    mkdirSync(join(nested.projectionRoot, "nested-output"));
    expect(() =>
      writeExternalConsumerV3Summary({
        ...nested.options,
        outputRoot: join(nested.projectionRoot, "nested-output"),
      }),
    ).toThrow(/distinct|nested/i);

    const overwrite = createFixture();
    mkdirSync(join(overwrite.outputRoot, evidenceDirectory), { recursive: true });
    writeFileSync(join(overwrite.outputRoot, summaryPath), "occupied\n");
    expect(() => writeExternalConsumerV3Summary(overwrite.options)).toThrow(/overwrite|already/i);
    expect(existsSync(join(overwrite.outputRoot, projectionPath))).toBe(false);
  });

  it("detects bundle tampering without modifying the bundle", () => {
    const fixture = createFixture();
    writeExternalConsumerV3Summary(fixture.options);
    mutateJson(join(fixture.outputRoot, nativeSpecs[2][3]), (receipt) => {
      receipt.consumers.typescript.outputSha256 = digest(Buffer.from("tampered"));
    });
    const before = snapshotTree(fixture.outputRoot);
    expect(() =>
      assertExternalConsumerV3SummaryCurrent({
        authorityRoot,
        outputRoot: fixture.outputRoot,
      }),
    ).toThrow();
    expect(snapshotTree(fixture.outputRoot)).toEqual(before);
  });
});

function createFixture() {
  const fixtureRoot = realpathSync(mkdtempSync(join(tmpdir(), "cloud-agents-ec3-summary-test-")));
  temporaryRoots.push(fixtureRoot);
  const projectionRoot = join(fixtureRoot, "projection");
  const outputRoot = join(fixtureRoot, "output");
  mkdirSync(projectionRoot);
  mkdirSync(outputRoot);
  const nativeRoots = Object.fromEntries(
    nativeSpecs.map(([key]) => {
      const root = join(fixtureRoot, key);
      mkdirSync(root);
      return [key, root];
    }),
  ) as Record<(typeof nativeSpecs)[number][0], string>;
  const source = readJson(
    join(authorityRoot, EXTERNAL_CONSUMER_V3_SOURCE_PATH),
  ) as ExternalConsumerV3Source;
  const sourceBinding = fileRecord(authorityRoot, EXTERNAL_CONSUMER_V3_SOURCE_PATH);
  const archive = Buffer.from(
    encodeExternalConsumerV3Ustar([
      { path: "README.md", type: "regular", mode: 0o644, data: Buffer.from("fixture\n") },
    ]),
  );
  const memberManifest = Buffer.from(`${JSON.stringify({ fixture: true })}\n`);
  const projection = {
    formatVersion: "cloud-agents-g-contract-external-consumer-projection-receipt/v3",
    decisionId: source.decisionId,
    profileId: source.profileId,
    status: "CURRENT",
    sourceBinding,
    contractSuccessorBinding: source.contractSuccessor,
    candidateBinding: {
      manifestPath: "manifest.json",
      manifestSha256: digest(Buffer.from("manifest")),
      rootIdentity: digest(Buffer.from("root")),
      candidateFileCount: 1,
      candidateEntryCount: 1,
      manifestAlgorithm: "utf8-bytewise-sorted-path-kind-mode-size-sha256-nul-v1",
    },
    projection: {
      archivePath,
      archiveSha256: digest(archive),
      archiveSizeBytes: archive.byteLength,
      memberManifestPath,
      memberManifestSha256: digest(memberManifest),
      memberCount: 1,
      memberManifestAlgorithm: "utf8-bytewise-sorted-path-type-mode-size-sha256-linktarget-nul-v1",
      inputManifestAlgorithm: "utf8-bytewise-sorted-path-mode-size-sha256-nul-v1",
      inputManifest: { sha256: digest(Buffer.from("input-manifest")), memberCount: 1 },
      pathOrdering: "UTF8_BYTE_LEXICOGRAPHIC",
      archiveFormat: "ustar",
      compression: "none",
      tar: {
        mtimeEpochSeconds: 0,
        uid: 0,
        gid: 0,
        uname: "",
        gname: "",
        paxHeaders: "forbidden",
        duplicateEntries: "forbidden",
      },
    },
    runner: {
      path: "scripts/replay-platform-g-contract-external-consumer-v3.ts",
      entrypoint:
        "bun scripts/replay-platform-g-contract-external-consumer-v3.ts --projection --candidate-root <candidate-root> --candidate-manifest <candidate-manifest> --output-root <output-root>",
    },
    notGateClosure: true,
    gateStatus: "ALL_GATES_OPEN",
  };
  write(join(projectionRoot, archivePath), archive);
  write(join(projectionRoot, memberManifestPath), memberManifest);
  writeJson(join(projectionRoot, projectionPath), projection);

  for (const [key, platform, runId, receiptPath] of nativeSpecs) {
    writeJson(join(nativeRoots[key], receiptPath), {
      formatVersion: "cloud-agents-g-contract-external-consumer-native-replay-receipt/v3",
      decisionId: source.decisionId,
      profileId: source.profileId,
      platform,
      runId,
      status: "CURRENT",
      sourceBinding,
      projectionBinding: {
        archivePath,
        archiveSha256: projection.projection.archiveSha256,
        archiveSizeBytes: projection.projection.archiveSizeBytes,
        memberManifestSha256: projection.projection.memberManifestSha256,
        inputManifestSha256: projection.projection.inputManifest.sha256,
        memberCount: projection.projection.memberCount,
      },
      runner: {
        path: "scripts/replay-platform-g-contract-external-consumer-v3.ts",
        entrypoint:
          "bun scripts/replay-platform-g-contract-external-consumer-v3.ts --native --projection-root <projection-root> --platform <platform> --run-id <run-id> --output-root <output-root>",
      },
      consumers: {
        typescript: consumer(digest(Buffer.from("typescript-result"))),
        go: consumer(digest(Buffer.from("go-result"))),
      },
      isolation: {
        receiptPath,
        network: "LOOPBACK_ONLY",
        filesystem: "DISPOSABLE_ROOT_ONLY",
        ambientNodeModules: false,
      },
      notGateClosure: true,
      gateStatus: "ALL_GATES_OPEN",
    });
  }
  return {
    source,
    projectionRoot,
    outputRoot,
    nativeRoots,
    options: { authorityRoot, projectionRoot, nativeRoots, outputRoot },
  };
}

function consumer(outputSha256: string) {
  return {
    requiredCallCount: 1,
    observedCallCount: 1,
    outputSha256,
    requestContentType: "application/proto",
    responseContentType: "application/proto",
    url: "http://127.0.0.1:<ephemeral-port>/fixture",
  };
}

function digest(bytes: Uint8Array): `sha256:${string}` {
  return `sha256:${createHash("sha256").update(bytes).digest("hex")}`;
}

function write(path: string, bytes: Uint8Array): void {
  mkdirSync(join(path, ".."), { recursive: true });
  writeFileSync(path, bytes);
}

function writeJson(path: string, value: unknown): void {
  write(path, Buffer.from(`${JSON.stringify(value, null, 2)}\n`));
}

function readJson(path: string): any {
  return JSON.parse(readFileSync(path, "utf8"));
}

function mutateJson(path: string, mutate: (value: any) => void): void {
  const value = readJson(path);
  mutate(value);
  writeJson(path, value);
}

function snapshotTree(root: string): Record<string, string> {
  const result: Record<string, string> = {};
  for (const path of [
    projectionPath,
    archivePath,
    memberManifestPath,
    ...nativeSpecs.map((item) => item[3]),
    summaryPath,
    profilePath,
  ]) {
    const absolute = join(root, path);
    if (existsSync(absolute)) result[path] = digest(readFileSync(absolute));
  }
  return result;
}
