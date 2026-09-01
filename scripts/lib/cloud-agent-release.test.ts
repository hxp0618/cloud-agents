import { createHash } from "node:crypto";
import { readFileSync } from "node:fs";
import { describe, expect, it } from "vitest";

import {
  assertSameCloudAgentBits,
  cloudAgentStableImportSpecifiers,
  cloudAgentTarballClosure,
  cloudAgentCandidateDigest,
  CLOUD_AGENT_PUBLIC_PACKAGES,
  parseCloudAgentReleaseSmokeOptions,
  type PackedCloudAgentPackage,
  validatePackedCloudAgentManifest,
  validatePackedCloudAgentSet,
} from "./cloud-agent-release";

function packed(
  name: (typeof CLOUD_AGENT_PUBLIC_PACKAGES)[number],
  version = name === "@cloud-agents/cloud-agent-runtime" ? "0.2.0-rc.1" : "0.1.0-rc.1",
): PackedCloudAgentPackage {
  return {
    name,
    version,
    filename: `${name.split("/").at(-1)}-${version}.tgz`,
    sha256: `sha256:${createHash("sha256").update(name).digest("hex")}`,
  };
}

type TestManifest = {
  name: (typeof CLOUD_AGENT_PUBLIC_PACKAGES)[number];
  version: string;
  dependencies: Record<string, string>;
  peerDependencies: Record<string, string>;
};

function validManifests(): TestManifest[] {
  const versions = Object.fromEntries(
    CLOUD_AGENT_PUBLIC_PACKAGES.map((name) => [
      name,
      name === "@cloud-agents/cloud-agent-runtime" ? "0.2.0-rc.1" : "0.1.0-rc.1",
    ]),
  ) as Record<(typeof CLOUD_AGENT_PUBLIC_PACKAGES)[number], string>;
  const peerDependencies = {
    "@cloud-agents/cloud-agent-protocol": {},
    "@cloud-agents/cloud-agent-provider-api": {
      "@cloud-agents/cloud-agent-protocol": versions["@cloud-agents/cloud-agent-protocol"],
    },
    "@cloud-agents/cloud-agent-runtime": {
      "@cloud-agents/cloud-agent-protocol": versions["@cloud-agents/cloud-agent-protocol"],
      "@cloud-agents/cloud-agent-provider-api": versions["@cloud-agents/cloud-agent-provider-api"],
    },
    "@cloud-agents/cloud-agent-provider-codex": {
      "@cloud-agents/cloud-agent-provider-api": versions["@cloud-agents/cloud-agent-provider-api"],
    },
    "@cloud-agents/cloud-agent-provider-claude": {
      "@cloud-agents/cloud-agent-provider-api": versions["@cloud-agents/cloud-agent-provider-api"],
    },
    "@cloud-agents/cloud-agent-testkit": {
      "@cloud-agents/cloud-agent-protocol": versions["@cloud-agents/cloud-agent-protocol"],
      "@cloud-agents/cloud-agent-provider-api": versions["@cloud-agents/cloud-agent-provider-api"],
    },
    "@cloud-agents/cloud-agent-distribution": {
      "@cloud-agents/cloud-agent-protocol": versions["@cloud-agents/cloud-agent-protocol"],
      "@cloud-agents/cloud-agent-provider-api": versions["@cloud-agents/cloud-agent-provider-api"],
      "@cloud-agents/cloud-agent-runtime": versions["@cloud-agents/cloud-agent-runtime"],
      "@cloud-agents/cloud-agent-provider-codex": versions["@cloud-agents/cloud-agent-provider-codex"],
      "@cloud-agents/cloud-agent-provider-claude": versions["@cloud-agents/cloud-agent-provider-claude"],
    },
  } satisfies Record<(typeof CLOUD_AGENT_PUBLIC_PACKAGES)[number], Record<string, string>>;
  return CLOUD_AGENT_PUBLIC_PACKAGES.map((name) => ({
    name,
    version: versions[name],
    dependencies:
      name === "@cloud-agents/cloud-agent-provider-claude"
        ? { "@anthropic-ai/claude-agent-sdk": "0.3.207" }
        : {},
    peerDependencies: peerDependencies[name],
  }));
}

function replaceDependencies(
  manifests: ReadonlyArray<TestManifest>,
  name: TestManifest["name"],
  dependencies: Record<string, string>,
): TestManifest[] {
  return manifests.map((manifest) =>
    manifest.name === name ? { ...manifest, dependencies } : manifest,
  );
}

function replacePeerDependencies(
  manifests: ReadonlyArray<TestManifest>,
  name: TestManifest["name"],
  peerDependencies: Record<string, string>,
): TestManifest[] {
  return manifests.map((manifest) =>
    manifest.name === name ? { ...manifest, peerDependencies } : manifest,
  );
}

describe("Cloud Agent packed release validation", () => {
  it("runs and publishes the packed Runtime candidate in product workflows", () => {
    const ci = readFileSync(".github/workflows/ci.yml", "utf8");
    const release = readFileSync(".github/workflows/release.yml", "utf8");
    const smoke = "node scripts/cloud-agent-release-smoke.ts --output-dir";
    expect(ci).toContain(smoke);
    expect(release).toContain(smoke);
    for (const asset of [
      "*.tgz",
      "candidate-manifest.json",
      "cloud-agent-runtime-checksums.sha256",
      "cloud-agent-runtime-sbom.spdx.json",
      "cloud-agent-runtime-provenance.json",
    ]) {
      expect(release).toContain(asset);
    }
    expect(release).toContain(
      'cmp "$runtime_directory/cloud-agent-runtime-standalone.mjs" "$CLOUD_AGENTS_PLATFORM_DIRECTORY/cloud-agent-runtime-standalone.mjs"',
    );
  });

  it("isolates every package import to its exact transitive tarball closure", () => {
    const expected = {
      "@cloud-agents/cloud-agent-protocol": ["@cloud-agents/cloud-agent-protocol"],
      "@cloud-agents/cloud-agent-provider-api": [
        "@cloud-agents/cloud-agent-protocol",
        "@cloud-agents/cloud-agent-provider-api",
      ],
      "@cloud-agents/cloud-agent-runtime": [
        "@cloud-agents/cloud-agent-protocol",
        "@cloud-agents/cloud-agent-provider-api",
        "@cloud-agents/cloud-agent-runtime",
      ],
      "@cloud-agents/cloud-agent-provider-codex": [
        "@cloud-agents/cloud-agent-protocol",
        "@cloud-agents/cloud-agent-provider-api",
        "@cloud-agents/cloud-agent-provider-codex",
      ],
      "@cloud-agents/cloud-agent-provider-claude": [
        "@cloud-agents/cloud-agent-protocol",
        "@cloud-agents/cloud-agent-provider-api",
        "@cloud-agents/cloud-agent-provider-claude",
      ],
      "@cloud-agents/cloud-agent-testkit": [
        "@cloud-agents/cloud-agent-protocol",
        "@cloud-agents/cloud-agent-provider-api",
        "@cloud-agents/cloud-agent-testkit",
      ],
      "@cloud-agents/cloud-agent-distribution": [
        "@cloud-agents/cloud-agent-protocol",
        "@cloud-agents/cloud-agent-provider-api",
        "@cloud-agents/cloud-agent-runtime",
        "@cloud-agents/cloud-agent-provider-codex",
        "@cloud-agents/cloud-agent-provider-claude",
        "@cloud-agents/cloud-agent-distribution",
      ],
    } satisfies Record<
      (typeof CLOUD_AGENT_PUBLIC_PACKAGES)[number],
      ReadonlyArray<(typeof CLOUD_AGENT_PUBLIC_PACKAGES)[number]>
    >;
    for (const target of CLOUD_AGENT_PUBLIC_PACKAGES) {
      expect(cloudAgentTarballClosure(target)).toEqual(expected[target]);
    }
  });

  it("imports stable public subpaths in the isolated target environment", () => {
    expect(cloudAgentStableImportSpecifiers("@cloud-agents/cloud-agent-provider-api")).toEqual([
      "@cloud-agents/cloud-agent-provider-api",
      "@cloud-agents/cloud-agent-provider-api/internal",
    ]);
    expect(cloudAgentStableImportSpecifiers("@cloud-agents/cloud-agent-runtime")).toEqual([
      "@cloud-agents/cloud-agent-runtime",
      "@cloud-agents/cloud-agent-runtime/node",
    ]);
    expect(cloudAgentStableImportSpecifiers("@cloud-agents/cloud-agent-distribution")).toEqual([
      "@cloud-agents/cloud-agent-distribution",
      "@cloud-agents/cloud-agent-distribution/schemas",
      "@cloud-agents/cloud-agent-distribution/schemas/cloud-agent-envelope-v2",
    ]);
  });

  it("rejects local protocols and unpublished private dependencies", () => {
    expect(() =>
      validatePackedCloudAgentManifest({
        name: "@cloud-agents/cloud-agent-runtime",
        version: "0.2.0",
        dependencies: { "@cloud-agents/cloud-agent-protocol": "workspace:*" },
      }),
    ).toThrow(/local protocol/);
    expect(() =>
      validatePackedCloudAgentManifest({
        name: "@cloud-agents/cloud-agent-runtime",
        version: "0.2.0",
        dependencies: { "@cloud-agents/contracts": "0.0.0" },
      }),
    ).toThrow(/unpublished private package/);
    expect(() =>
      validatePackedCloudAgentManifest({
        name: "@cloud-agents/cloud-agent-runtime",
        version: "0.2.0",
        exports: { "./legacy-provider-host": "./dist/legacyProviderHost.mjs" },
      }),
    ).toThrow(/legacy Provider facade/);
  });

  it("requires all seven tarballs and exact cross-package pins", () => {
    const manifests = validManifests();
    expect(() => validatePackedCloudAgentSet(manifests)).not.toThrow();
    expect(() =>
      validatePackedCloudAgentSet(
        replacePeerDependencies(manifests, "@cloud-agents/cloud-agent-distribution", {
          ...manifests.find((manifest) => manifest.name === "@cloud-agents/cloud-agent-distribution")!
            .peerDependencies,
          "@cloud-agents/cloud-agent-runtime": "^0.2.0",
        }),
      ),
    ).toThrow(/exact semver/);
  });

  it.each([
    ["Runtime to Provider", "@cloud-agents/cloud-agent-runtime", "@cloud-agents/cloud-agent-provider-codex"],
    ["Runtime to Distribution", "@cloud-agents/cloud-agent-runtime", "@cloud-agents/cloud-agent-distribution"],
    ["Runtime to Testkit", "@cloud-agents/cloud-agent-runtime", "@cloud-agents/cloud-agent-testkit"],
    ["Provider API to Runtime", "@cloud-agents/cloud-agent-provider-api", "@cloud-agents/cloud-agent-runtime"],
    [
      "Protocol to Provider API",
      "@cloud-agents/cloud-agent-protocol",
      "@cloud-agents/cloud-agent-provider-api",
    ],
  ] as const)("rejects an extra %s internal edge", (_label, source, target) => {
    const manifests = validManifests();
    const versions = Object.fromEntries(
      manifests.map((manifest) => [manifest.name, manifest.version]),
    );
    expect(() =>
      validatePackedCloudAgentSet(
        replacePeerDependencies(manifests, source, {
          ...manifests.find((manifest) => manifest.name === source)!.peerDependencies,
          [target]: versions[target]!,
        }),
      ),
    ).toThrow(/internal dependencies must be exactly/);
  });

  it("rejects a missing required internal edge", () => {
    const manifests = validManifests();
    expect(() =>
      validatePackedCloudAgentSet(
        replacePeerDependencies(manifests, "@cloud-agents/cloud-agent-runtime", {
          "@cloud-agents/cloud-agent-protocol": "0.1.0-rc.1",
        }),
      ),
    ).toThrow(/internal dependencies must be exactly/);
  });

  it("rejects internal runtime edges outside peerDependencies", () => {
    const manifests = validManifests();
    expect(() =>
      validatePackedCloudAgentSet(
        manifests.map((manifest) =>
          manifest.name === "@cloud-agents/cloud-agent-runtime"
            ? Object.assign({}, manifest, {
                optionalDependencies: { "@cloud-agents/cloud-agent-provider-codex": "0.1.0" },
              })
            : manifest,
        ),
      ),
    ).toThrow(/not optionalDependencies/);
    expect(() =>
      validatePackedCloudAgentSet(
        manifests.map((manifest) =>
          manifest.name === "@cloud-agents/cloud-agent-provider-api"
            ? Object.assign({}, manifest, {
                dependencies: { "@cloud-agents/cloud-agent-runtime": "0.2.0-rc.1" },
              })
            : manifest,
        ),
      ),
    ).toThrow(/not dependencies/);
    expect(() =>
      validatePackedCloudAgentSet(
        manifests.map((manifest) =>
          manifest.name === "@cloud-agents/cloud-agent-testkit"
            ? Object.assign({}, manifest, {
                devDependencies: { "@cloud-agents/cloud-agent-provider-codex": "0.1.0" },
              })
            : manifest,
        ),
      ),
    ).toThrow(/not devDependencies/);
  });

  it("requires the exact Claude Agent SDK production dependency", () => {
    const manifests = validManifests();
    const claude = manifests.find(
      (manifest) => manifest.name === "@cloud-agents/cloud-agent-provider-claude",
    )!;
    expect(() =>
      validatePackedCloudAgentSet(
        replaceDependencies(manifests, claude.name, {
          ...claude.dependencies,
          "@anthropic-ai/claude-agent-sdk": "^0.3.207",
        }),
      ),
    ).toThrow(/exclusively pin/);
  });

  it("rejects --skip-build so candidates always rebuild from source", () => {
    expect(() =>
      parseCloudAgentReleaseSmokeOptions(
        ["--allow-dirty", "--skip-build", "--output-dir", "candidate"],
        "/repo",
      ),
    ).toThrow(/must build every package from source/);
    expect(
      parseCloudAgentReleaseSmokeOptions(["--allow-dirty", "--output-dir", "candidate"], "/repo"),
    ).toEqual({ outputDirectory: "/repo/candidate", allowDirty: true });
  });

  it("binds candidate identity to unchanged tarball bits", () => {
    const packages = CLOUD_AGENT_PUBLIC_PACKAGES.map((name) => packed(name));
    expect(() =>
      assertSameCloudAgentBits(
        packages,
        packages.map((item) => ({ ...item })),
      ),
    ).not.toThrow();
    expect(() =>
      assertSameCloudAgentBits(packages, [
        ...packages.slice(0, -1),
        { ...packages.at(-1)!, sha256: `sha256:${"0".repeat(64)}` },
      ]),
    ).toThrow(/bits changed/);
    expect(cloudAgentCandidateDigest(packages)).toMatch(/^sha256:[0-9a-f]{64}$/u);
  });
});
