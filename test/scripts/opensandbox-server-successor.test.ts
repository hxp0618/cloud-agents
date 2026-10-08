import { createHash } from "node:crypto";
import { mkdtempSync, mkdirSync, readFileSync, symlinkSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { describe, expect, it } from "vitest";
import {
  buildOpenSandboxServerSuccessor,
  parseOpenSandboxServerDockerDaemonPlatform,
  readOpenSandboxServerBuildEvidence,
  readOpenSandboxServerSuccessorRecipe,
  selectOpenSandboxServerRecipePlatform,
} from "../../scripts/build-opensandbox-server-successor";

const sha256 = (value: string) => createHash("sha256").update(value).digest("hex");

function fixture() {
  const root = mkdtempSync(join(tmpdir(), "cloud-agents-server-recipe-"));
  const authorityDir = join(root, "tools/opensandbox-server-successor/v1");
  mkdirSync(join(authorityDir, "patches"), { recursive: true });
  mkdirSync(join(authorityDir, "tests"), { recursive: true });
  const patch = "diff --git a/a b/a\n--- a/a\n+++ b/a\n@@ -1 +1 @@\n-old\n+new\n";
  const regression = "print('cookie isolation fixture')\n";
  const license = "Apache License fixture\n";
  const dockerfile = "ARG BASE_IMAGE\nFROM ${BASE_IMAGE}\n";
  writeFileSync(join(authorityDir, "patches/isolate-http-client-cookie.patch"), patch);
  writeFileSync(join(authorityDir, "tests/cookie-isolation-regression.py"), regression);
  writeFileSync(join(authorityDir, "LICENSE"), license);
  writeFileSync(join(authorityDir, "Dockerfile"), dockerfile);
  const repository = "sandbox-registry.cn-zhangjiakou.cr.aliyuncs.com/opensandbox/server@sha256:";
  const sourceDigest = "1".repeat(64);
  const recipe = {
    schemaVersion: "cloud-agents.opensandbox-server-successor-source.v2",
    base: {
      image: repository + "2".repeat(64),
      buildSourceStatus: "UNKNOWN_BUILD_SOURCE",
      sourcePath: "/app/opensandbox_server/api/proxy.py",
      sourceSha256: sourceDigest,
      platforms: [
        {
          platform: "linux/amd64",
          image: repository + "3".repeat(64),
          os: "linux",
          architecture: "amd64",
        },
        {
          platform: "linux/arm64",
          image: repository + "4".repeat(64),
          os: "linux",
          architecture: "arm64",
        },
      ],
    },
    upstreamFileMatch: {
      repository: "https://github.com/alibaba/OpenSandbox.git",
      commit: "f31696b429b2b67c197b9336583a78e114d7d36a",
      tree: "1969b038103a6911861d0cd052ccca0f6aaada1a",
      path: "server/opensandbox_server/api/proxy.py",
      sha256: sourceDigest,
      scope: "EXACT_FILE_MATCH_ONLY",
    },
    patch: {
      path: "tools/opensandbox-server-successor/v1/patches/isolate-http-client-cookie.patch",
      sha256: sha256(patch),
      patchedSourceSha256: "5".repeat(64),
    },
    regression: {
      path: "tools/opensandbox-server-successor/v1/tests/cookie-isolation-regression.py",
      sha256: sha256(regression),
    },
    license: {
      path: "tools/opensandbox-server-successor/v1/LICENSE",
      sha256: sha256(license),
    },
    build: {
      dockerfile: "tools/opensandbox-server-successor/v1/Dockerfile",
      dockerfileSha256: sha256(dockerfile),
    },
  };
  writeFileSync(join(authorityDir, "source.json"), JSON.stringify(recipe, null, 2) + "\n");
  return { root, authorityDir, recipe };
}

describe("OpenSandbox server successor recipe", () => {
  it("selects the immutable child digest for each qualified platform", () => {
    const { root } = fixture();
    const recipe = readOpenSandboxServerSuccessorRecipe(root);

    expect(selectOpenSandboxServerRecipePlatform(recipe, "linux/amd64").image).toContain(
      `@sha256:${"3".repeat(64)}`,
    );
    expect(selectOpenSandboxServerRecipePlatform(recipe, "linux/arm64").architecture).toBe("arm64");
    expect(() => selectOpenSandboxServerRecipePlatform(recipe, "linux/s390x")).toThrow(
      /unsupported server successor platform/,
    );
  });

  it("derives the default from Docker daemon fields and normalizes daemon architecture names", () => {
    expect(
      parseOpenSandboxServerDockerDaemonPlatform({ OSType: "linux", Architecture: "x86_64" }),
    ).toBe("linux/amd64");
    expect(
      parseOpenSandboxServerDockerDaemonPlatform({ OSType: "linux", Architecture: "aarch64" }),
    ).toBe("linux/arm64");
    expect(() =>
      parseOpenSandboxServerDockerDaemonPlatform({ OSType: "darwin", Architecture: "arm64" }),
    ).toThrow(/Docker daemon platform is unsupported/);
  });

  it("rejects duplicate platform bindings", () => {
    const { root, authorityDir, recipe } = fixture();
    recipe.base.platforms[1] = { ...recipe.base.platforms[0] };
    writeFileSync(join(authorityDir, "source.json"), JSON.stringify(recipe));

    expect(() => readOpenSandboxServerSuccessorRecipe(root)).toThrow(
      /duplicate server successor platform/,
    );
  });

  it("accepts platform-bound evidence and rejects a selected base mismatch", () => {
    const { root } = fixture();
    const recipe = readOpenSandboxServerSuccessorRecipe(root);
    const selected = selectOpenSandboxServerRecipePlatform(recipe, "linux/amd64");
    const evidencePath = join(root, "build-evidence.json");
    const imageId = "sha256:" + "6".repeat(64);
    const evidence = {
      schemaVersion: "cloud-agents.opensandbox-server-successor-build-evidence.v2",
      recipeSha256: recipe.recipeSha256,
      platform: selected.platform,
      base: {
        image: recipe.base.image,
        selectedImage: selected.image,
        id: "sha256:" + "7".repeat(64),
        os: selected.os,
        architecture: selected.architecture,
        buildSourceStatus: "UNKNOWN_BUILD_SOURCE",
      },
      source: {
        path: recipe.base.sourcePath,
        originalSha256: recipe.base.sourceSha256,
        patchedSha256: recipe.patch.patchedSourceSha256,
        upstreamCommit: recipe.upstreamFileMatch.commit,
        upstreamTree: recipe.upstreamFileMatch.tree,
        upstreamMatchScope: "EXACT_FILE_MATCH_ONLY",
      },
      patchSha256: recipe.patch.sha256,
      regressionSha256: recipe.regression.sha256,
      dockerfileSha256: recipe.build.dockerfileSha256,
      licenseSha256: recipe.license.sha256,
      image: {
        tag: "local/server:test",
        id: imageId,
        immutableRef: imageId,
        os: selected.os,
        architecture: selected.architecture,
        entrypoint: ["opensandbox-server"],
        command: ["--config", "/etc/opensandbox/config.toml"],
        proxySha256: recipe.patch.patchedSourceSha256,
        licenseSha256: recipe.license.sha256,
      },
      verification: {
        dockerBuild: "PASS",
        cookieIsolationRegression: "PASS",
        recipeBindings: "PASS",
      },
      cleanup: {
        extractionContainerRemoved: true,
        inspectionContainerRemoved: true,
        imageTag: "local/server:test",
        imageIDRetained: true,
      },
      notGateClosure: true,
    };
    writeFileSync(evidencePath, JSON.stringify(evidence));

    expect(readOpenSandboxServerBuildEvidence(evidencePath, recipe, "linux/amd64").platform).toBe(
      "linux/amd64",
    );
    evidence.base.selectedImage = recipe.base.platforms[1].image;
    writeFileSync(evidencePath, JSON.stringify(evidence));
    expect(() => readOpenSandboxServerBuildEvidence(evidencePath, recipe, "linux/amd64")).toThrow(
      /server build evidence base mismatch/,
    );
  });

  it("uses the Docker daemon platform and rejects mismatched local base image metadata", () => {
    const { root } = fixture();
    const outputRoot = join(mkdtempSync(join(tmpdir(), "cloud-agents-server-build-")), "output");
    const fakeRoot = mkdtempSync(join(tmpdir(), "cloud-agents-server-fake-docker-"));
    const fakeDocker = join(fakeRoot, "docker");
    const calls = join(fakeRoot, "calls");
    writeFileSync(
      fakeDocker,
      `#!/bin/sh
printf '%s\\n' "$*" >> '${calls}'
if [ "$1 $2" = "info --format" ]; then
  printf '%s\\n' '{"OSType":"linux","Architecture":"x86_64"}'
elif [ "$1 $2" = "image inspect" ]; then
  printf '%s\\n' '[{"Id":"sha256:${"8".repeat(64)}","Os":"linux","Architecture":"arm64"}]'
else
  exit 99
fi
`,
      { mode: 0o700 },
    );

    expect(() =>
      buildOpenSandboxServerSuccessor({
        repoRoot: root,
        outputRoot,
        tag: "local/server:test",
        dockerCommand: fakeDocker,
      }),
    ).toThrow(/base image platform or ID mismatch/);
    const recorded = readFileSync(calls, "utf8");
    expect(recorded).toContain("info --format");
    expect(recorded).toContain(`@sha256:${"3".repeat(64)}`);
    expect(recorded).not.toContain(`@sha256:${"4".repeat(64)}`);
  });

  it("rejects a symbolic-link output root before running external commands", () => {
    const { root } = fixture();
    const target = mkdtempSync(join(tmpdir(), "cloud-agents-server-output-target-"));
    const parent = mkdtempSync(join(tmpdir(), "cloud-agents-server-output-link-"));
    const outputRoot = join(parent, "output");
    symlinkSync(target, outputRoot);

    expect(() =>
      buildOpenSandboxServerSuccessor({
        repoRoot: root,
        outputRoot,
        tag: "local/server:test",
        platform: "linux/arm64",
      }),
    ).toThrow(/must not be a symbolic link/);
  });

  it("rejects an output whose symbolic-link ancestor resolves into the repository", () => {
    const { root } = fixture();
    const inside = join(root, "inside");
    mkdirSync(inside);
    const parent = mkdtempSync(join(tmpdir(), "cloud-agents-server-output-parent-"));
    const alias = join(parent, "alias");
    symlinkSync(inside, alias);

    expect(() =>
      buildOpenSandboxServerSuccessor({
        repoRoot: root,
        outputRoot: join(alias, "output"),
        tag: "local/server:test",
        platform: "linux/arm64",
      }),
    ).toThrow(/must be outside the repository/);
  });
});
