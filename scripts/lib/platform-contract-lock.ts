import { createHash } from "node:crypto";
import { spawnSync } from "node:child_process";
import { lstatSync, readFileSync } from "node:fs";
import { relative, resolve, sep } from "node:path";

import { validatePlatformContractTree } from "./platform-contracts";
import { PLATFORM_GO_TOOLCHAIN } from "./platform-go-modules";

const NODE_VERSION = "24.13.1";
const BUN_VERSION = "1.3.14";
const AJV_REVIEW = "docs/plan/p1/dependency-reviews/ajv-8.20.0.md";
const TOOLCHAIN_AUTHORITY_FILES = [".mise.toml", "package.json"] as const;
const PLATFORM_GO_INPUTS = [
  "go.work",
  "sdk/go/go.mod",
  "sdk/go/doc.go",
  "services/control-plane/go.mod",
  "services/control-plane/doc.go",
  "services/worker/go.mod",
  "services/worker/doc.go",
] as const;
const NORMALIZED_MANIFEST_ALGORITHM = "sorted-path-nul-sha256-nul-git-mode-v1";

const IN_REPO_TOOLS = [
  {
    id: "platform-contract-bootstrap-checker",
    kind: "in-repo-typescript-ajv",
    entrypoint: "scripts/check-platform-contracts.ts",
    sources: [
      "scripts/check-platform-contracts.ts",
      "scripts/lib/platform-contracts.ts",
      "scripts/lib/platform-json-semantics.ts",
    ],
  },
  {
    id: "platform-go-module-boundary-checker",
    kind: "in-repo-typescript-go-ast",
    entrypoint: "scripts/check-platform-go-modules.ts",
    sources: [
      "scripts/check-platform-go-modules.ts",
      "scripts/go/importcheck/main.go",
      "scripts/lib/platform-go-modules.ts",
    ],
  },
  {
    id: "platform-contract-lock-writer",
    kind: "in-repo-typescript",
    entrypoint: "scripts/generate-platform-contract-lock.ts",
    sources: [
      "scripts/generate-platform-contract-lock.ts",
      "scripts/lib/platform-contract-lock.ts",
      "scripts/lib/platform-contracts.ts",
      "scripts/lib/platform-go-modules.ts",
      "scripts/lib/platform-json-semantics.ts",
    ],
  },
] as const;

export function buildPlatformContractLock(root: string): Record<string, unknown> {
  const summary = validatePlatformContractTree(root);
  const runtimes = validatePlatformToolchains(root);
  return {
    lockVersion: 1,
    status: "BOOTSTRAP_VALIDATED",
    notGateClosure: true,
    sourceContract: {
      manifestAlgorithm: NORMALIZED_MANIFEST_ALGORITHM,
      manifestSha256: summary.contractManifestSha256,
      sourceTreeBinding: "REQUIRED_AT_GATE",
      excludes: ["contracts/generation.lock.json", "contracts/generated/**"],
    },
    dialects: {
      jsonSchema: {
        identity: "https://json-schema.org/draft/2020-12/schema",
        semanticValidation: "BOOTSTRAP_AJV_AND_IN_REPO_SEMANTIC_FIXTURES",
      },
      openapi: {
        documentVersion: "3.1.1",
        semanticValidation: "BOOTSTRAP_FAIL_CLOSED_SUBSET",
      },
      proto: {
        syntax: "proto3",
        descriptorStatus: "NOT_GENERATED",
        sourceValidation: "BOOTSTRAP_FAIL_CLOSED_SUBSET",
      },
    },
    runtimes,
    toolchainAuthority: {
      manifestAlgorithm: NORMALIZED_MANIFEST_ALGORITHM,
      manifestSha256: normalizedSourceManifestDigest(root, TOOLCHAIN_AUTHORITY_FILES),
      sources: [...TOOLCHAIN_AUTHORITY_FILES],
      actualRuntimeVerified: true,
    },
    dependencyLock: {
      path: "bun.lock",
      sha256: fileSha256(root, "bun.lock"),
    },
    tools: [
      ...IN_REPO_TOOLS.map((tool) => ({
        id: tool.id,
        kind: tool.kind,
        entrypoint: tool.entrypoint,
        sourceManifestSha256: sourceManifestDigest(root, tool.sources),
        sources: [...tool.sources],
        license: "MIT",
      })),
      {
        id: "ajv-2020",
        kind: "npm",
        version: "8.20.0",
        integrity:
          "sha512-Thbli+OlOj+iMPYFBVBfJ3OmCAnaSyNn4M1vz9T6Gka5Jt9ba/HIR56joy65tY6kx/FCF5VXNB819Y7/GUrBGA==",
        license: "MIT",
        reviewEvidence: {
          path: AJV_REVIEW,
          sha256: fileSha256(root, AJV_REVIEW),
          status: "APPROVED",
        },
      },
      {
        id: "ajv-formats",
        kind: "npm",
        version: "3.0.1",
        integrity:
          "sha512-8iUql50EUR+uUcdRQ3HDqa6EVyo3docL8g5WJ3FNcWmu62IbkGUue/pEyLBW8VGKKucTPgqeks4fIU1DA4yowQ==",
        registeredFormats: ["date-time", "uri"],
        license: "MIT",
        reviewEvidence: {
          path: AJV_REVIEW,
          sha256: fileSha256(root, AJV_REVIEW),
          status: "APPROVED",
        },
      },
    ],
    pipelines: [
      {
        id: "bootstrap-contract-validation",
        inputManifestSha256: summary.contractManifestSha256,
        outputStatus: "BOOTSTRAP_VALIDATED",
        generatedOutputs: [],
      },
      {
        id: "go-module-boundary-validation",
        modules: [
          "github.com/hxp0618/cloud-agents/sdk/go",
          "github.com/hxp0618/cloud-agents/services/control-plane",
          "github.com/hxp0618/cloud-agents/services/worker",
        ],
        inputManifestAlgorithm: NORMALIZED_MANIFEST_ALGORITHM,
        inputManifestSha256: normalizedSourceManifestDigest(root, PLATFORM_GO_INPUTS),
        inputs: [...PLATFORM_GO_INPUTS],
        generatedOutputs: [],
      },
    ],
    missing: [...summary.missing],
  };
}

export function serializePlatformContractLock(lock: Record<string, unknown>): string {
  return `${JSON.stringify(lock, null, 2)}\n`;
}

export function assertPlatformContractLockCurrent(root: string): void {
  const expected = serializePlatformContractLock(buildPlatformContractLock(root));
  const actual = readFileSync(resolve(root, "contracts/generation.lock.json"), "utf8");
  if (actual !== expected) {
    throw new Error(
      "contracts/generation.lock.json is stale; run bun scripts/generate-platform-contract-lock.ts --write.",
    );
  }
}

export function normalizedSourceManifestDigest(root: string, files: ReadonlyArray<string>): string {
  const hash = createHash("sha256");
  const entries = files.map((file) => {
    const target = resolve(root, file);
    const path = relative(root, target).split(sep).join("/");
    if (path === ".." || path.startsWith("../") || path.startsWith("/")) {
      throw new Error(`Manifest input escapes the repository root: ${file}.`);
    }
    const stat = lstatSync(target);
    if (!stat.isFile() || stat.isSymbolicLink()) {
      throw new Error(`Manifest input must be a regular file: ${file}.`);
    }
    const digest = createHash("sha256").update(readFileSync(target)).digest("hex");
    const gitMode = (stat.mode & 0o111) === 0 ? "100644" : "100755";
    return { digest, gitMode, path };
  });
  const paths = entries.map((entry) => entry.path);
  if (new Set(paths).size !== paths.length) {
    throw new Error("Manifest inputs must have unique normalized paths.");
  }
  for (const { digest, gitMode, path } of entries.toSorted((left, right) =>
    left.path < right.path ? -1 : left.path > right.path ? 1 : 0,
  )) {
    hash.update(path).update("\0").update(digest).update("\0").update(gitMode).update("\0");
  }
  return `sha256:${hash.digest("hex")}`;
}

function sourceManifestDigest(root: string, files: ReadonlyArray<string>): string {
  return normalizedSourceManifestDigest(root, files);
}

function validatePlatformToolchains(root: string): {
  readonly node: string;
  readonly bun: string;
  readonly go: string;
} {
  const mise = readFileSync(resolve(root, ".mise.toml"), "utf8");
  const packageDocument = JSON.parse(readFileSync(resolve(root, "package.json"), "utf8")) as {
    engines?: { node?: unknown };
    packageManager?: unknown;
  };
  const declared = {
    node: singleMiseTool(mise, "node"),
    bun: singleMiseTool(mise, "bun"),
    go: singleMiseTool(mise, "go"),
  };
  if (
    declared.node !== NODE_VERSION ||
    packageDocument.engines?.node !== NODE_VERSION ||
    declared.bun !== BUN_VERSION ||
    packageDocument.packageManager !== `bun@${BUN_VERSION}` ||
    declared.go !== PLATFORM_GO_TOOLCHAIN.slice(2)
  ) {
    throw new Error(
      `Platform toolchain declarations mismatch: mise=${JSON.stringify(declared)}, package node=${String(packageDocument.engines?.node)}, package manager=${String(packageDocument.packageManager)}.`,
    );
  }

  const actual = {
    node: runVersion(root, "node", ["--version"]).replace(/^v/u, ""),
    bun: runVersion(root, "bun", ["--version"]),
    go: parseGoVersion(runVersion(root, "go", ["version"])),
  };
  const executingBun = process.versions.bun;
  if (actual.node !== declared.node || actual.bun !== declared.bun || actual.go !== declared.go) {
    throw new Error(
      `Platform toolchain runtime mismatch: declared=${JSON.stringify(declared)}, actual=${JSON.stringify(actual)}.`,
    );
  }
  if (executingBun !== declared.bun) {
    throw new Error(
      `Platform lock writer must execute under Bun ${declared.bun}, found ${String(executingBun)}.`,
    );
  }
  return actual;
}

function singleMiseTool(source: string, tool: string): string {
  const matches = [...source.matchAll(new RegExp(`^\\s*${tool}\\s*=\\s*"([^"]+)"\\s*$`, "gmu"))];
  if (matches.length !== 1) throw new Error(`.mise.toml must pin exactly one ${tool} version.`);
  return matches[0]![1]!;
}

function runVersion(root: string, command: string, args: ReadonlyArray<string>): string {
  const result = spawnSync(command, args, {
    cwd: root,
    encoding: "utf8",
    env: { ...process.env, GOTOOLCHAIN: "local" },
  });
  if (result.error) throw result.error;
  if (result.status !== 0) {
    throw new Error(
      `${command} ${args.join(" ")} failed (${String(result.status)}):\n${result.stdout}${result.stderr}`,
    );
  }
  return result.stdout.trim();
}

function parseGoVersion(source: string): string {
  const match = /^go version go([^\s]+)\s/u.exec(source);
  if (!match) throw new Error(`Unexpected go version output: ${source}.`);
  return match[1]!;
}

function fileSha256(root: string, file: string): string {
  return `sha256:${createHash("sha256")
    .update(readFileSync(resolve(root, file)))
    .digest("hex")}`;
}
