import { describe, expect, it } from "vitest";

import { workerReleaseForm, workerReleaseRegisterRequestFrom } from "./policies";

describe("worker release form mapping", () => {
  it("trims release metadata and preserves selected architectures", () => {
    expect(
      workerReleaseRegisterRequestFrom({
        ...workerReleaseForm(),
        releaseId: " worker-v1 ",
        releaseName: " Worker v1 ",
        imageRepository: " registry.example/worker ",
        releaseDigest: ` ${"sha256:"}${"a".repeat(64)} `,
        platformVersion: " platform-v1 ",
        runtimeVersion: " runtime-v1 ",
        codexVersion: " codex-v1 ",
        claudeCodeVersion: " claude-v1 ",
        amd64: true,
        arm64: true,
        verificationEvidenceDigest: ` ${"sha256:"}${"b".repeat(64)} `,
      }),
    ).toEqual({
      releaseId: "worker-v1",
      releaseName: "Worker v1",
      imageRepository: "registry.example/worker",
      releaseDigest: `sha256:${"a".repeat(64)}`,
      platformVersion: "platform-v1",
      runtimeVersion: "runtime-v1",
      codexVersion: "codex-v1",
      claudeCodeVersion: "claude-v1",
      architectures: ["linux/amd64", "linux/arm64"],
      verificationEvidenceDigest: `sha256:${"b".repeat(64)}`,
    });
  });
});
