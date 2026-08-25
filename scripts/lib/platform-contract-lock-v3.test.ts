import { createHash } from "node:crypto";
import { readFileSync } from "node:fs";

import { describe, expect, it } from "vitest";

import {
  assertPlatformContractLockV3Document,
  assertPlatformContractLockV3Transition,
  buildPlatformContractLockV3Assembled,
  buildPlatformContractLockV3PhaseBound,
  derivePlatformContractLockV3AssembledSnapshotIdentity,
  PLATFORM_CONTRACT_LOCK_V3_PHASE_ARTIFACTS,
  PLATFORM_CONTRACT_LOCK_V3_POST_H_PREDECESSOR,
  serializePlatformContractLockV3,
  type PlatformContractLockV3ArtifactIdentity,
  type PlatformContractLockV3AssembledAuthority,
  type PlatformContractLockV3FileObservation,
  type PlatformContractLockV3PhaseBinding,
} from "./platform-contract-lock-v3";

const gitSha = (character: string): string => character.repeat(40);
const sha256 = (character: string): string => `sha256:${character.repeat(64)}`;

function artifact(path: string, character: string): PlatformContractLockV3ArtifactIdentity {
  return {
    path,
    fileType: "REGULAR_FILE",
    gitMode: "100644",
    gitBlobSha1: gitSha(character),
    sha256: sha256(character),
    sizeBytes: 100 + character.charCodeAt(0),
  };
}

function authority(): PlatformContractLockV3AssembledAuthority {
  return {
    generatorSupply: {
      formatVersion: "cloud-agents-generator-supply-profile-registry/v3",
      profileId: "cloud-agents/generator-supply-profile/v3",
      profileDigest: sha256("1"),
      registryDigest: sha256("2"),
      candidateManifestSha256: sha256("3"),
      outputFiles: 49,
      evidenceManifest: artifact("tools/generator-supply/v3/evidence-manifest.json", "4"),
      profile: artifact("tools/generator-supply/v3/profile.json", "5"),
    },
    projection: {
      algorithm: "exact-ordered-paths-v1",
      exclusionCount: 17,
      exclusionsDigest: sha256("6"),
      receipt: artifact("tools/generator-supply/v3/evidence/replay/projection.json", "7"),
    },
  };
}

function phaseBinding(): PlatformContractLockV3PhaseBinding {
  return {
    state: "PHASE_BINDING_CURRENT_FINAL_REVIEW_ABSENT",
    artifacts: PLATFORM_CONTRACT_LOCK_V3_PHASE_ARTIFACTS.map((entry, index) => ({
      role: entry.role,
      artifact: artifact(entry.path, String(index + 1)),
    })),
  };
}

function observation(
  snapshot: ReturnType<typeof derivePlatformContractLockV3AssembledSnapshotIdentity>,
): PlatformContractLockV3FileObservation {
  return {
    path: snapshot.path,
    fileType: "REGULAR_FILE",
    gitMode: snapshot.gitMode,
    gitBlobSha1: snapshot.gitBlobSha1,
    sha256: snapshot.sha256,
    sizeBytes: snapshot.sizeBytes,
    device: "1",
    inode: "2",
    mtimeNs: "3",
    ctimeNs: "4",
  };
}

function redigest(document: Record<string, unknown>): Record<string, unknown> {
  const body = Object.fromEntries(Object.entries(document).filter(([key]) => key !== "lockDigest"));
  return {
    ...body,
    lockDigest: `sha256:${createHash("sha256")
      .update("cloud-agents/platform-contract-generation-lock/document/v3")
      .update("\0")
      .update(JSON.stringify(body))
      .digest("hex")}`,
  };
}

describe("platform contract generation lock v3", () => {
  it("builds a deterministic ASSEMBLED document on the exact fixed post-H v2 blob", () => {
    const first = buildPlatformContractLockV3Assembled(authority());
    const second = buildPlatformContractLockV3Assembled(authority());
    expect(first).toEqual(second);
    expect(first).toMatchObject({
      formatVersion: "cloud-agents-platform-contract-generation-lock/v3",
      lockVersion: 3,
      state: "ASSEMBLED",
      notGateClosure: true,
      gateStatus: "ALL_GATES_OPEN",
      predecessorV2: {
        ...PLATFORM_CONTRACT_LOCK_V3_POST_H_PREDECESSOR,
        status: "SUCCESSOR_ASSEMBLED_REVIEW_BOUND",
      },
      assembledAuthority: {
        generatorSupply: { outputFiles: 49 },
        projection: { exclusionCount: 17 },
      },
    });
    expect(first.predecessorV2).not.toHaveProperty("state");
    expect(serializePlatformContractLockV3(first)).not.toMatch(/generatedAt|\/Users\//u);
    expect(() => assertPlatformContractLockV3Document(first)).not.toThrow();
  });

  it("derives and accepts only the exact ASSEMBLED -> PHASE_BOUND successor", () => {
    const assembled = buildPlatformContractLockV3Assembled(authority());
    const snapshot = derivePlatformContractLockV3AssembledSnapshotIdentity(assembled, {
      commitSha1: gitSha("a"),
      treeSha1: gitSha("b"),
    });
    const phaseBound = buildPlatformContractLockV3PhaseBound(assembled, snapshot, phaseBinding());
    const stableRead = observation(snapshot);
    expect(phaseBound).toMatchObject({
      state: "PHASE_BOUND",
      assembledSnapshot: snapshot,
      phaseBinding: { state: "PHASE_BINDING_CURRENT_FINAL_REVIEW_ABSENT" },
    });
    expect(() =>
      assertPlatformContractLockV3Transition(assembled, phaseBound, {
        readBefore: stableRead,
        readAfter: { ...stableRead },
      }),
    ).not.toThrow();
    expect(() =>
      assertPlatformContractLockV3Transition(phaseBound, phaseBound, {
        readBefore: stableRead,
        readAfter: stableRead,
      }),
    ).toThrow(/only ASSEMBLED -> PHASE_BOUND/u);
    expect(() =>
      assertPlatformContractLockV3Transition(phaseBound, assembled, {
        readBefore: stableRead,
        readAfter: stableRead,
      }),
    ).toThrow(/only ASSEMBLED -> PHASE_BOUND/u);
  });

  it("fails closed for predecessor drift, unknown fields, reordered fields, and self-digest drift", () => {
    const assembled = buildPlatformContractLockV3Assembled(authority());
    const predecessorDrift = redigest({
      ...assembled,
      predecessorV2: {
        ...assembled.predecessorV2,
        sha256: sha256("0"),
      },
    });
    expect(() => assertPlatformContractLockV3Document(predecessorDrift)).toThrow(
      /predecessor identity drifted/u,
    );

    const unknown = redigest({ ...assembled, unexpected: true });
    expect(() => assertPlatformContractLockV3Document(unknown)).toThrow(/topology/u);

    const reorderedBody = {
      state: assembled.state,
      formatVersion: assembled.formatVersion,
      lockVersion: assembled.lockVersion,
      notGateClosure: assembled.notGateClosure,
      gateStatus: assembled.gateStatus,
      predecessorV2: assembled.predecessorV2,
      assembledAuthority: assembled.assembledAuthority,
      implementationBoundary: assembled.implementationBoundary,
    };
    expect(() => assertPlatformContractLockV3Document(redigest(reorderedBody))).toThrow(
      /field order/u,
    );

    expect(() =>
      assertPlatformContractLockV3Document({ ...assembled, lockDigest: sha256("f") }),
    ).toThrow(/self digest mismatch/u);
  });

  it("rejects partial, reordered, unknown, symlink, and terminal-review phase binding", () => {
    const assembled = buildPlatformContractLockV3Assembled(authority());
    const snapshot = derivePlatformContractLockV3AssembledSnapshotIdentity(assembled, {
      commitSha1: gitSha("a"),
      treeSha1: gitSha("b"),
    });
    const binding = phaseBinding();
    expect(() =>
      buildPlatformContractLockV3PhaseBound(assembled, snapshot, {
        ...binding,
        artifacts: binding.artifacts.slice(1),
      }),
    ).toThrow(/partial/u);
    expect(() =>
      buildPlatformContractLockV3PhaseBound(assembled, snapshot, {
        ...binding,
        artifacts: binding.artifacts.toReversed(),
      }),
    ).toThrow(/reordered/u);
    expect(() =>
      buildPlatformContractLockV3PhaseBound(assembled, snapshot, {
        ...binding,
        unexpected: true,
      } as unknown as PlatformContractLockV3PhaseBinding),
    ).toThrow(/topology/u);
    expect(() =>
      buildPlatformContractLockV3PhaseBound(assembled, snapshot, {
        ...binding,
        artifacts: binding.artifacts.map((entry, index) =>
          index === 0
            ? {
                ...entry,
                artifact: { ...entry.artifact, fileType: "SYMLINK" },
              }
            : entry,
        ),
      } as unknown as PlatformContractLockV3PhaseBinding),
    ).toThrow(/regular non-symlink/u);
    expect(() =>
      buildPlatformContractLockV3PhaseBound(assembled, snapshot, {
        state: "PHASE_BINDING_CURRENT_FINAL_REVIEW_PRESENT",
        artifacts: binding.artifacts,
        terminalReview: artifact(
          "docs/plan/p1/g-contract-r5-review-binding-independent-review-20260825.md",
          "9",
        ),
      } as unknown as PlatformContractLockV3PhaseBinding),
    ).toThrow(/topology|terminal-review-absent/u);
  });

  it("rejects assembled snapshot substitution, symlink observations, and byte-identical ABA", () => {
    const assembled = buildPlatformContractLockV3Assembled(authority());
    const snapshot = derivePlatformContractLockV3AssembledSnapshotIdentity(assembled, {
      commitSha1: gitSha("a"),
      treeSha1: gitSha("b"),
    });
    expect(() =>
      buildPlatformContractLockV3PhaseBound(
        assembled,
        { ...snapshot, sha256: sha256("f") },
        phaseBinding(),
      ),
    ).toThrow(/exact historical ASSEMBLED bytes/u);

    const phaseBound = buildPlatformContractLockV3PhaseBound(assembled, snapshot, phaseBinding());
    const stableRead = observation(snapshot);
    expect(() =>
      assertPlatformContractLockV3Transition(assembled, phaseBound, {
        readBefore: stableRead,
        readAfter: { ...stableRead, inode: "999" },
      }),
    ).toThrow(/ABA mutation/u);
    expect(() =>
      assertPlatformContractLockV3Transition(assembled, phaseBound, {
        readBefore: stableRead,
        readAfter: {
          ...stableRead,
          fileType: "SYMLINK",
        } as unknown as PlatformContractLockV3FileObservation,
      }),
    ).toThrow(/regular non-symlink/u);
  });

  it("contains no writer or filesystem mutation capability", () => {
    const source = readFileSync(new URL("./platform-contract-lock-v3.ts", import.meta.url), "utf8");
    expect(source).not.toMatch(/node:fs|writeFile|renameSync|unlinkSync|fsyncSync/u);
    expect(source).not.toContain("g-contract-r5-review-binding-independent-review-20260825.md");
  });
});
