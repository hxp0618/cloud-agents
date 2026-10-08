import { createHash } from "node:crypto";
import { readFileSync } from "node:fs";
import { describe, expect, it } from "vitest";

import {
  assertSameCloudAgentBits,
  cloudAgentStableImportSpecifiers,
  cloudAgentTarballClosure,
  cloudAgentCandidateDigest,
  CLOUD_AGENT_PUBLIC_PACKAGES,
  DEFAULT_CLOUD_AGENT_NPM_REGISTRY,
  parseCloudAgentReleaseSmokeOptions,
  type PackedCloudAgentPackage,
  validatePackedCloudAgentManifest,
  validatePackedCloudAgentSet,
} from "../../scripts/lib/cloud-agent-release";

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
    "@cloud-agents/cloud-agent-provider-pi": {
      "@cloud-agents/cloud-agent-provider-api": versions["@cloud-agents/cloud-agent-provider-api"],
    },
    "@cloud-agents/cloud-agent-provider-deepseek-harness": {
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
      "@cloud-agents/cloud-agent-provider-codex":
        versions["@cloud-agents/cloud-agent-provider-codex"],
      "@cloud-agents/cloud-agent-provider-claude":
        versions["@cloud-agents/cloud-agent-provider-claude"],
      "@cloud-agents/cloud-agent-provider-pi": versions["@cloud-agents/cloud-agent-provider-pi"],
      "@cloud-agents/cloud-agent-provider-deepseek-harness":
        versions["@cloud-agents/cloud-agent-provider-deepseek-harness"],
    },
  } satisfies Record<(typeof CLOUD_AGENT_PUBLIC_PACKAGES)[number], Record<string, string>>;
  return CLOUD_AGENT_PUBLIC_PACKAGES.map((name) => ({
    name,
    version: versions[name],
    dependencies: {
      ...(name === "@cloud-agents/cloud-agent-provider-claude"
        ? { "@anthropic-ai/claude-agent-sdk": "0.3.207" }
        : {}),
      ...(name === "@cloud-agents/cloud-agent-provider-pi"
        ? { "@earendil-works/pi-coding-agent": "0.85.1" }
        : {}),
      ...(name === "@cloud-agents/cloud-agent-provider-deepseek-harness"
        ? { "@deepseek-ai/dsh-sdk-client": "0.1.2-rc.1" }
        : {}),
    },
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
    expect(ci).toContain("bun run public:evidence:check");
    expect(release).toContain("bun run public:evidence:check");
    expect(ci).toContain(smoke);
    expect(release).toContain(smoke);
    for (const workflow of [ci, release]) {
      expect(workflow.indexOf(smoke)).toBeLessThan(workflow.indexOf("bun run platform:release --"));
      expect(
        workflow.match(/node scripts\/cloud-agent-release-smoke\.ts --output-dir/gu),
      ).toHaveLength(1);
      expect(workflow).toContain("--runtime-candidate-dir");
    }
    expect(ci).toContain(
      "CLOUD_AGENT_SECRET_SCAN_BASE: ${{ github.event.pull_request.base.sha || github.event.before }}",
    );
    expect(ci).toContain("timeout-minutes: 60");
    expect(release).toContain("CLOUD_AGENT_SECRET_SCAN_BASE: ${{ github.event.before || '' }}");
    expect(ci).toContain("persist-credentials: false");
    expect(release).toContain("persist-credentials: false");
    expect(release).toContain("GH_TOKEN: ${{ github.token }}");
    expect(release).toContain(
      'git -c "http.extraheader=AUTHORIZATION: basic $auth_header" fetch --no-tags origin main',
    );
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
      'cmp "$runtime_directory/cloud-agent-runtime-standalone.mjs" "$platform_directory/cloud-agent-runtime-standalone.mjs"',
    );
    expect(release).toContain(
      'cmp "$runtime_directory/cloud-agent-runtime-notices.md" "$platform_directory/cloud-agent-runtime-notices.md"',
    );
  });

  it("keeps the scheduled security workflow fail-closed and token-minimal", () => {
    const security = readFileSync(".github/workflows/security.yml", "utf8");
    expect(security).toContain("schedule:");
    expect(security).toContain("workflow_dispatch:");
    expect(security).toContain("timeout-minutes: 10");
    expect(security).toContain("bun install --frozen-lockfile --ignore-scripts");
    expect(security).toContain("bun run public:evidence:check");
    expect(security).toContain("fetch-depth: 0");
    expect(security).toContain("persist-credentials: false");
    expect(security).toMatch(/uses: actions\/checkout@[0-9a-f]{40} # v4/u);
    expect(security).toMatch(/uses: jdx\/mise-action@[0-9a-f]{40} # v3/u);
    expect(security).toContain("bun scripts/check-generator-supply-evidence.ts --check");
    expect(security).toContain("bun scripts/generate-sbom.ts --output-dir");
    expect(security).toContain('test -s "$output_directory/sbom.spdx.json"');
    expect(security).toContain('CLOUD_AGENT_SECRET_SCAN_ALL_HISTORY: "1"');
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
      "@cloud-agents/cloud-agent-provider-pi": [
        "@cloud-agents/cloud-agent-protocol",
        "@cloud-agents/cloud-agent-provider-api",
        "@cloud-agents/cloud-agent-provider-pi",
      ],
      "@cloud-agents/cloud-agent-provider-deepseek-harness": [
        "@cloud-agents/cloud-agent-protocol",
        "@cloud-agents/cloud-agent-provider-api",
        "@cloud-agents/cloud-agent-provider-deepseek-harness",
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
        "@cloud-agents/cloud-agent-provider-pi",
        "@cloud-agents/cloud-agent-provider-deepseek-harness",
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

  it("requires every tarball and exact cross-package pins", () => {
    const manifests = validManifests();
    expect(() => validatePackedCloudAgentSet(manifests)).not.toThrow();
    expect(() =>
      validatePackedCloudAgentSet(
        replacePeerDependencies(manifests, "@cloud-agents/cloud-agent-distribution", {
          ...manifests.find(
            (manifest) => manifest.name === "@cloud-agents/cloud-agent-distribution",
          )!.peerDependencies,
          "@cloud-agents/cloud-agent-runtime": "^0.2.0",
        }),
      ),
    ).toThrow(/exact semver/);
  });

  it.each([
    [
      "Runtime to Provider",
      "@cloud-agents/cloud-agent-runtime",
      "@cloud-agents/cloud-agent-provider-codex",
    ],
    [
      "Runtime to Distribution",
      "@cloud-agents/cloud-agent-runtime",
      "@cloud-agents/cloud-agent-distribution",
    ],
    [
      "Runtime to Testkit",
      "@cloud-agents/cloud-agent-runtime",
      "@cloud-agents/cloud-agent-testkit",
    ],
    [
      "Provider API to Runtime",
      "@cloud-agents/cloud-agent-provider-api",
      "@cloud-agents/cloud-agent-runtime",
    ],
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

  it.each([
    ["@cloud-agents/cloud-agent-provider-claude", "@anthropic-ai/claude-agent-sdk", "^0.3.207"],
    ["@cloud-agents/cloud-agent-provider-pi", "@earendil-works/pi-coding-agent", "^0.85.1"],
    [
      "@cloud-agents/cloud-agent-provider-deepseek-harness",
      "@deepseek-ai/dsh-sdk-client",
      "^0.1.2-rc.1",
    ],
  ] as const)(
    "requires the exact upstream dependency for %s",
    (provider, dependency, invalidVersion) => {
      const manifests = validManifests();
      const manifest = manifests.find(({ name }) => name === provider)!;
      expect(() =>
        validatePackedCloudAgentSet(
          replaceDependencies(manifests, manifest.name, {
            ...manifest.dependencies,
            [dependency]: invalidVersion,
          }),
        ),
      ).toThrow(/exclusively pin/);
    },
  );

  it("rejects --skip-build so candidates always rebuild from source", () => {
    expect(() =>
      parseCloudAgentReleaseSmokeOptions(
        ["--allow-dirty", "--skip-build", "--output-dir", "candidate"],
        "/repo",
      ),
    ).toThrow(/must build every package from source/);
    expect(
      parseCloudAgentReleaseSmokeOptions(["--allow-dirty", "--output-dir", "candidate"], "/repo"),
    ).toEqual({
      outputDirectory: "/repo/candidate",
      allowDirty: true,
      registry: DEFAULT_CLOUD_AGENT_NPM_REGISTRY,
    });
    expect(
      parseCloudAgentReleaseSmokeOptions(
        [
          "--allow-dirty",
          "--registry",
          "https://registry.npmmirror.com",
          "--output-dir",
          "candidate",
        ],
        "/repo",
      ),
    ).toMatchObject({
      outputDirectory: "/repo/candidate",
      registry: "https://registry.npmmirror.com/",
    });
    for (const value of [
      "http://registry.example.com/",
      "https://user:pass@registry.example.com/",
      "https://registry.example.com/?token=secret",
      "https://registry.example.com/#fragment",
    ]) {
      expect(() =>
        parseCloudAgentReleaseSmokeOptions(
          ["--registry", value, "--output-dir", "candidate"],
          "/repo",
        ),
      ).toThrow(/absolute HTTPS registry URL/);
    }
    expect(() =>
      parseCloudAgentReleaseSmokeOptions(["--registry", "--output-dir", "candidate"], "/repo"),
    ).toThrow(/requires an absolute HTTPS registry URL/);
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
