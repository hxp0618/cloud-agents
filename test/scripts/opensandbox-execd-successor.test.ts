import { createHash } from "node:crypto";
import { mkdtempSync, mkdirSync, symlinkSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { describe, expect, it } from "vitest";
import {
  buildOpenSandboxExecdSuccessor,
  buildOpenSandboxExecdDockerArgs,
  readOpenSandboxExecdBuildEvidence,
  readOpenSandboxExecdSuccessorRecipe,
} from "../../scripts/lib/opensandbox-execd-successor";

const sha256 = (value: string) => createHash("sha256").update(value).digest("hex");

function fixture() {
  const root = mkdtempSync(join(tmpdir(), "cloud-agents-execd-recipe-"));
  const authorityDir = join(root, "tools/opensandbox-execd-successor/v1");
  mkdirSync(join(authorityDir, "patches"), { recursive: true });
  const license = "Apache License fixture\n";
  const patch = "diff --git a/a b/a\n--- a/a\n+++ b/a\n@@ -1 +1 @@\n-old\n+new\n";
  const dockerfile =
    "ARG GO_IMAGE\nFROM ${GO_IMAGE} AS builder\nARG RUNTIME_BASE_IMAGE\nFROM ${RUNTIME_BASE_IMAGE}\n";
  writeFileSync(join(authorityDir, "LICENSE"), license);
  writeFileSync(join(authorityDir, "patches/pty-terminal-exit.patch"), patch);
  writeFileSync(join(authorityDir, "Dockerfile"), dockerfile);
  const recipe = {
    schemaVersion: "cloud-agents.opensandbox-execd-successor-source.v1",
    upstream: {
      repository: "https://github.com/alibaba/OpenSandbox.git",
      commit: "f31696b429b2b67c197b9336583a78e114d7d36a",
      tree: "1969b038103a6911861d0cd052ccca0f6aaada1a",
      licensePath: "LICENSE",
      licenseCopyPath: "tools/opensandbox-execd-successor/v1/LICENSE",
      licenseSha256: sha256(license),
    },
    patch: {
      path: "tools/opensandbox-execd-successor/v1/patches/pty-terminal-exit.patch",
      sha256: sha256(patch),
    },
    toolchain: {
      image: "docker.io/library/golang@sha256:" + "1".repeat(64),
      goVersion: "go1.25.9",
      goProxy: "https://goproxy.cn",
      goSumDb: "sum.golang.google.cn",
    },
    runtimeBase: {
      image: "registry.example/opensandbox/execd@sha256:" + "2".repeat(64),
      sourceStatus: "UNKNOWN_BUILD_SOURCE",
      use: "runtime-assets-only",
    },
    build: {
      dockerfile: "tools/opensandbox-execd-successor/v1/Dockerfile",
      dockerfileSha256: sha256(dockerfile),
      platform: "linux/arm64",
      version: "cloud-agents-pty-terminal-exit-successor-v1",
      buildTime: "2026-07-09T08:25:32Z",
    },
  };
  writeFileSync(join(authorityDir, "source.json"), JSON.stringify(recipe, null, 2) + "\n");
  return { root, authorityDir, patch, recipe };
}

describe("OpenSandbox execd successor recipe", () => {
  it("binds source, patch, license, buildfile and immutable images", () => {
    const { root } = fixture();
    const recipe = readOpenSandboxExecdSuccessorRecipe(root);
    expect(recipe.upstream.commit).toBe("f31696b429b2b67c197b9336583a78e114d7d36a");
    expect(recipe.runtimeBase.sourceStatus).toBe("UNKNOWN_BUILD_SOURCE");
    expect(recipe.toolchain.image).toContain("@sha256:");
  });

  it("rejects modified patch bytes", () => {
    const { root, authorityDir, patch } = fixture();
    writeFileSync(join(authorityDir, "patches/pty-terminal-exit.patch"), patch + "tampered\n");
    expect(() => readOpenSandboxExecdSuccessorRecipe(root)).toThrow(/patch sha256 mismatch/);
  });

  it("rejects floating toolchain and runtime base images", () => {
    const { root, authorityDir, recipe } = fixture();
    recipe.toolchain.image = "golang:1.25.9";
    writeFileSync(join(authorityDir, "source.json"), JSON.stringify(recipe));
    expect(() => readOpenSandboxExecdSuccessorRecipe(root)).toThrow(
      /toolchain image must be digest-pinned/,
    );

    recipe.toolchain.image = "docker.io/library/golang@sha256:" + "1".repeat(64);
    recipe.runtimeBase.image = "opensandbox/execd:latest";
    writeFileSync(join(authorityDir, "source.json"), JSON.stringify(recipe));
    expect(() => readOpenSandboxExecdSuccessorRecipe(root)).toThrow(
      /runtime base image must be digest-pinned/,
    );
  });

  it("creates Docker arguments only from the validated recipe", () => {
    const { root } = fixture();
    const recipe = readOpenSandboxExecdSuccessorRecipe(root);
    const args = buildOpenSandboxExecdDockerArgs(recipe, "/external/source", "local/execd:test");
    expect(args).toContain("linux/arm64");
    expect(args).toContain(`GO_IMAGE=${recipe.toolchain.image}`);
    expect(args).toContain(`RUNTIME_BASE_IMAGE=${recipe.runtimeBase.image}`);
    expect(args.at(-1)).toBe("/external/source");
  });

  it("rejects a symbolic-link output root before running external commands", () => {
    const { root } = fixture();
    const target = mkdtempSync(join(tmpdir(), "cloud-agents-execd-output-target-"));
    const parent = mkdtempSync(join(tmpdir(), "cloud-agents-execd-output-link-"));
    const outputRoot = join(parent, "output");
    symlinkSync(target, outputRoot);

    expect(() =>
      buildOpenSandboxExecdSuccessor({
        repoRoot: root,
        outputRoot,
        tag: "local/execd:test",
      }),
    ).toThrow(/must not be a symbolic link/);
  });

  it("rejects an output whose symbolic-link ancestor resolves into the repository", () => {
    const { root } = fixture();
    const inside = join(root, "inside");
    mkdirSync(inside);
    const parent = mkdtempSync(join(tmpdir(), "cloud-agents-execd-output-parent-"));
    const alias = join(parent, "alias");
    symlinkSync(inside, alias);

    expect(() =>
      buildOpenSandboxExecdSuccessor({
        repoRoot: root,
        outputRoot: join(alias, "output"),
        tag: "local/execd:test",
      }),
    ).toThrow(/must be outside the repository/);
  });

  it("accepts evidence bound to the recipe and rejects a mutable image ref", () => {
    const { root } = fixture();
    const recipe = readOpenSandboxExecdSuccessorRecipe(root);
    const evidencePath = join(root, "build-evidence.json");
    const imageId = "sha256:" + "3".repeat(64);
    const evidence = {
      schemaVersion: "cloud-agents.opensandbox-execd-successor-build-evidence.v1",
      recipeSha256: recipe.recipeSha256,
      upstream: { commit: recipe.upstream.commit, tree: recipe.upstream.tree },
      patchSha256: recipe.patch.sha256,
      toolchainImage: recipe.toolchain.image,
      runtimeBaseImage: recipe.runtimeBase.image,
      image: {
        tag: "local/execd:test",
        id: imageId,
        immutableRef: imageId,
        os: "linux",
        architecture: "arm64",
        entrypoint: ["./execd"],
        binarySha256: "4".repeat(64),
        licenseSha256: recipe.upstream.licenseSha256,
      },
      verification: { dockerBuild: "PASS", recipeBindings: "PASS" },
      cleanup: {
        imageTag: "local/execd:test",
        imageIdRetained: true,
        sourceCheckout: "source",
        buildContainerRemoved: true,
      },
      notGateClosure: true,
    };
    writeFileSync(evidencePath, JSON.stringify(evidence));
    expect(readOpenSandboxExecdBuildEvidence(evidencePath, recipe).image.immutableRef).toBe(
      imageId,
    );

    evidence.image.immutableRef = "local/execd:test";
    writeFileSync(evidencePath, JSON.stringify(evidence));
    expect(() => readOpenSandboxExecdBuildEvidence(evidencePath, recipe)).toThrow(
      /immutable image ref/,
    );
  });
});
