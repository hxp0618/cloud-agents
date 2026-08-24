import { createHash } from "node:crypto";
import { execFileSync } from "node:child_process";
import {
  cpSync,
  mkdtempSync,
  mkdirSync,
  readFileSync,
  readdirSync,
  rmSync,
  symlinkSync,
  unlinkSync,
  writeFileSync,
} from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";

import { afterEach, describe, expect, it } from "vitest";

import {
  assertGeneratorSupplyInputSnapshotMutationForTest,
  assertGeneratorSupplyProfileCurrent,
  assertGeneratorSupplyReadSnapshotMutationForTest,
  assertGeneratorSupplyCoreProjectionCurrent,
  assertGeneratorSupplyReplaySummaryCurrent,
  buildCurrentStagedCoreProjection,
  buildGeneratorSupplyProfile,
  buildGeneratorSupplyReplaySummary,
  GeneratorSupplyProfileError,
  generatorSupplyEvidencePaths,
  validateGeneratorSupplyFormattedNonPurlClosureForTest,
  validateGeneratorSupplyLinuxIdentityProbeForTest,
  validateGeneratorSupplyLinuxStdoutProbeForTest,
  validateGeneratorSupplyProjectionArchiveMembersForTest,
  validateGeneratorSupplyRootfsInspectionForTest,
  writeGeneratorSupplyOutputsForTest,
} from "./platform-generator-supply-profile";

const repositoryRoot = join(import.meta.dirname, "../..");
const temporaryRoots: string[] = [];

afterEach(() => {
  for (const root of temporaryRoots.splice(0)) rmSync(root, { force: true, recursive: true });
});

function supplyFixture(): string {
  const root = mkdtempSync(join(tmpdir(), "generator-supply-profile-"));
  temporaryRoots.push(root);
  cpSync(join(repositoryRoot, "tools/generator-supply"), join(root, "tools/generator-supply"), {
    recursive: true,
  });
  cpSync(join(repositoryRoot, "bun.lock"), join(root, "bun.lock"));
  mkdirSync(join(root, "scripts"), { recursive: true });
  cpSync(
    join(repositoryRoot, "scripts/check-platform-contract-standards.ts"),
    join(root, "scripts/check-platform-contract-standards.ts"),
  );
  for (const relativePath of [
    "scripts/replay-platform-generators-isolated.sh",
    "scripts/replay-platform-generators.ts",
    "scripts/lib/generator-replay-path-authority.ts",
    "scripts/lib/inspect-generator-replay-archive.py",
  ]) {
    mkdirSync(join(root, relativePath, ".."), { recursive: true });
    cpSync(join(repositoryRoot, relativePath), join(root, relativePath));
  }
  const sourcePath = join(root, "tools/generator-supply/v1/source.json");
  const source = JSON.parse(readFileSync(sourcePath, "utf8")) as {
    profile: { replayAuthority: Record<string, string> };
  };
  for (const [pathKey, digestKey] of [
    ["wrapperPath", "wrapperSha256"],
    ["runnerPath", "runnerSha256"],
    ["pathAuthorityPath", "pathAuthoritySha256"],
    ["replayArchiveInspectorPath", "replayArchiveInspectorSha256"],
  ] as const) {
    const bytes = readFileSync(join(root, source.profile.replayAuthority[pathKey]!));
    source.profile.replayAuthority[digestKey] = createHash("sha256").update(bytes).digest("hex");
  }
  writeFileSync(sourcePath, `${JSON.stringify(source, null, 2)}\n`);
  mkdirSync(join(root, "tools/contract-standards"), { recursive: true });
  cpSync(
    join(repositoryRoot, "tools/contract-standards/uv.lock"),
    join(root, "tools/contract-standards/uv.lock"),
  );
  return root;
}

function projectionFixture(): string {
  const root = mkdtempSync(join(tmpdir(), "generator-supply-projection-"));
  temporaryRoots.push(root);
  execFileSync("/usr/bin/git", ["-C", root, "init", "-q"]);
  writeFileSync(join(root, "core.txt"), "core-v1\n");
  mkdirSync(join(root, "tools/generator-supply/v1/evidence/replay"), { recursive: true });
  writeFileSync(
    join(root, "tools/generator-supply/v1/evidence/replay/projection.json"),
    "late-bound-v1\n",
  );
  mkdirSync(join(root, "scripts/lib"), { recursive: true });
  cpSync(
    join(repositoryRoot, "scripts/lib/inspect-generator-replay-archive.py"),
    join(root, "scripts/lib/inspect-generator-replay-archive.py"),
  );
  const inspectorSha256 = createHash("sha256")
    .update(readFileSync(join(root, "scripts/lib/inspect-generator-replay-archive.py")))
    .digest("hex");
  mkdirSync(join(root, "tools/generator-supply/v1"), { recursive: true });
  writeFileSync(
    join(root, "tools/generator-supply/v1/source.json"),
    `${JSON.stringify({ profile: { replayAuthority: { replayArchiveInspectorSha256: inspectorSha256 } } })}\n`,
  );
  execFileSync("/usr/bin/git", [
    "-C",
    root,
    "add",
    "core.txt",
    "scripts/lib/inspect-generator-replay-archive.py",
    "tools/generator-supply/v1/source.json",
    "tools/generator-supply/v1/evidence/replay/projection.json",
  ]);
  return root;
}

describe("generator supply profile", () => {
  it("binds current staged core projection bytes and ignores only late-bound exclusions", () => {
    const root = projectionFixture();
    const authority = buildCurrentStagedCoreProjection(root);
    expect(() =>
      assertGeneratorSupplyCoreProjectionCurrent(root, {
        treeSha: authority.treeSha,
        archiveSha256: authority.archiveSha256,
        archiveSizeBytes: authority.archiveSizeBytes,
        archiveInspection: authority.archiveInspection,
      }),
    ).not.toThrow();

    writeFileSync(
      join(root, "tools/generator-supply/v1/evidence/replay/projection.json"),
      "late-bound-v2\n",
    );
    execFileSync("/usr/bin/git", [
      "-C",
      root,
      "add",
      "tools/generator-supply/v1/evidence/replay/projection.json",
    ]);
    expect(() =>
      assertGeneratorSupplyCoreProjectionCurrent(root, {
        treeSha: authority.treeSha,
        archiveSha256: authority.archiveSha256,
        archiveSizeBytes: authority.archiveSizeBytes,
        archiveInspection: authority.archiveInspection,
      }),
    ).not.toThrow();

    writeFileSync(join(root, "core.txt"), "core-v2\n");
    execFileSync("/usr/bin/git", ["-C", root, "add", "core.txt"]);
    expect(() =>
      assertGeneratorSupplyCoreProjectionCurrent(root, {
        treeSha: authority.treeSha,
        archiveSha256: authority.archiveSha256,
        archiveSizeBytes: authority.archiveSizeBytes,
        archiveInspection: authority.archiveInspection,
      }),
    ).toThrowError(
      expect.objectContaining<Partial<GeneratorSupplyProfileError>>({
        code: "GENERATOR_SUPPLY_PROFILE_STALE",
        path: "/replay/projection/currentness",
      }),
    );
  });

  it("fails closed when an untracked non-excluded core path is present", () => {
    const root = projectionFixture();
    const authority = buildCurrentStagedCoreProjection(root);
    writeFileSync(join(root, "untracked-core.txt"), "must-be-staged\n");
    expect(() =>
      assertGeneratorSupplyCoreProjectionCurrent(root, {
        treeSha: authority.treeSha,
        archiveSha256: authority.archiveSha256,
        archiveSizeBytes: authority.archiveSizeBytes,
        archiveInspection: authority.archiveInspection,
      }),
    ).toThrowError(
      expect.objectContaining<Partial<GeneratorSupplyProfileError>>({
        code: "GENERATOR_SUPPLY_PROFILE_STALE",
        path: "/replay/projection/currentness",
      }),
    );
  });

  it("rejects non-regular staged projection entries before archive authority is returned", () => {
    const root = projectionFixture();
    symlinkSync("/etc/passwd", join(root, "staged-link"));
    execFileSync("/usr/bin/git", ["-C", root, "add", "staged-link"]);
    expect(() => buildCurrentStagedCoreProjection(root)).toThrow(/non-regular/u);
  });

  it("rejects staged replay archive inspector bytes not authorized by staged source", () => {
    const root = projectionFixture();
    const inspectorPath = join(root, "scripts/lib/inspect-generator-replay-archive.py");
    writeFileSync(inspectorPath, `${readFileSync(inspectorPath, "utf8")}# staged drift\n`);
    execFileSync("/usr/bin/git", ["-C", root, "add", inspectorPath]);
    expect(() => buildCurrentStagedCoreProjection(root)).toThrow(/source-authorized/u);
  });

  it("rolls back a caught multi-output rename failure without mixed generation", () => {
    const root = mkdtempSync(join(tmpdir(), "generator-supply-transaction-"));
    temporaryRoots.push(root);
    mkdirSync(join(root, "generated"), { recursive: true });
    const outputs = [
      { path: "generated/a.json", value: { generation: "old-a" } },
      { path: "generated/b.json", value: { generation: "old-b" } },
      { path: "generated/c.json", value: { generation: "old-c" } },
    ] as const;
    for (const output of outputs) {
      writeFileSync(join(root, output.path), `${JSON.stringify(output.value)}\n`);
    }
    expect(() =>
      writeGeneratorSupplyOutputsForTest(
        root,
        outputs.map(({ path }) => ({ path, value: { generation: "new" } })),
        5,
      ),
    ).toThrow(/transaction failed/u);
    for (const output of outputs) {
      expect(readFileSync(join(root, output.path), "utf8")).toBe(
        `${JSON.stringify(output.value)}\n`,
      );
    }
    expect(
      readdirSync(join(root, "generated")).filter((name) => /\.(?:tmp|rollback)-/u.test(name)),
    ).toEqual([]);
    expect(
      readdirSync(root).filter((name) => name.startsWith(".generator-supply-transaction-")),
    ).toEqual([]);
  });

  it("does not destructively roll back committed outputs when backup cleanup fails", () => {
    const root = mkdtempSync(join(tmpdir(), "generator-supply-transaction-cleanup-"));
    temporaryRoots.push(root);
    mkdirSync(join(root, "generated"), { recursive: true });
    const paths = ["generated/a.json", "generated/b.json", "generated/c.json"];
    for (const path of paths) writeFileSync(join(root, path), '{"generation":"old"}\n');
    expect(() =>
      writeGeneratorSupplyOutputsForTest(
        root,
        paths.map((path) => ({ path, value: { generation: "new" } })),
        0,
        2,
      ),
    ).toThrow(/outputs committed consistently/u);
    for (const path of paths) {
      expect(readFileSync(join(root, path), "utf8")).toBe(
        `${JSON.stringify({ generation: "new" }, null, 2)}\n`,
      );
    }
    expect(
      readdirSync(root).filter((name) => name.startsWith(".generator-supply-transaction-")),
    ).toHaveLength(1);
  });

  it("fails closed when captured generator-supply input bytes change before commit", () => {
    const root = supplyFixture();
    const evidencePath = join(root, "tools/generator-supply/v1/evidence/security-repair.json");
    expect(() =>
      assertGeneratorSupplyInputSnapshotMutationForTest(root, () => {
        writeFileSync(evidencePath, `${readFileSync(evidencePath, "utf8")} `);
      }),
    ).toThrowError(
      expect.objectContaining<Partial<GeneratorSupplyProfileError>>({
        code: "GENERATOR_SUPPLY_PROFILE_STALE",
        path: "/profile/inputSnapshot/tools/generator-supply/v1/evidence/security-repair.json",
      }),
    );
  });

  it("fails closed when a generated output changes after the read gate snapshots it", () => {
    const root = supplyFixture();
    const replaySummaryPath = join(root, "tools/generator-supply/v1/evidence/replay.json");
    const manifestPath = join(root, "tools/generator-supply/v1/evidence-manifest.json");
    const profilePath = join(root, "tools/generator-supply/v1/profile.json");
    for (const path of [replaySummaryPath, manifestPath, profilePath]) {
      writeFileSync(path, "{}\n");
    }
    expect(() =>
      assertGeneratorSupplyReadSnapshotMutationForTest(root, () => {
        writeFileSync(profilePath, '{"drift":true}\n');
      }),
    ).toThrowError(
      expect.objectContaining<Partial<GeneratorSupplyProfileError>>({
        code: "GENERATOR_SUPPLY_PROFILE_STALE",
        path: "/profile/inputSnapshot/tools/generator-supply/v1/profile.json",
      }),
    );
  });

  it("keeps generated profile and evidence manifest current", () => {
    expect(() => assertGeneratorSupplyProfileCurrent(repositoryRoot)).not.toThrow();
    const profile = buildGeneratorSupplyProfile(repositoryRoot);
    expect(profile.registryId).toBe("cloud-agents/generator-supply-profile");
    expect(profile.registryDigest).toMatch(/^sha256:[0-9a-f]{64}$/u);
  });

  it("requires declared, actual, and semantic evidence to be the same exact closure", () => {
    const paths = generatorSupplyEvidencePaths(repositoryRoot);
    expect(paths).toEqual(
      paths.toSorted((left, right) =>
        Buffer.from(left, "utf8").compare(Buffer.from(right, "utf8")),
      ),
    );
    expect(new Set(paths).size).toBe(paths.length);
    expect(paths).toContain("tools/generator-supply/v1/evidence/replay/linux-isolation.json");

    const root = supplyFixture();
    writeFileSync(join(root, "tools/generator-supply/v1/evidence/undeclared.json"), "{}\n");
    expect(() => generatorSupplyEvidencePaths(root)).toThrowError(
      expect.objectContaining<Partial<GeneratorSupplyProfileError>>({
        code: "GENERATOR_SUPPLY_BINDING_MISMATCH",
        path: "/profile/evidence",
      }),
    );
  });

  it("rejects source schema extensions and evidence symlinks", () => {
    const schemaRoot = supplyFixture();
    const sourcePath = join(schemaRoot, "tools/generator-supply/v1/source.json");
    const source = JSON.parse(readFileSync(sourcePath, "utf8")) as Record<string, unknown>;
    source.unknownAuthority = true;
    writeFileSync(sourcePath, `${JSON.stringify(source, null, 2)}\n`);
    expect(() => buildGeneratorSupplyProfile(schemaRoot)).toThrowError(
      expect.objectContaining<Partial<GeneratorSupplyProfileError>>({
        code: "GENERATOR_SUPPLY_BINDING_MISMATCH",
        path: "/source",
      }),
    );

    const symlinkRoot = supplyFixture();
    const evidencePath = join(symlinkRoot, "tools/generator-supply/v1/evidence/artifacts.json");
    const external = join(symlinkRoot, "outside.json");
    writeFileSync(external, readFileSync(evidencePath));
    unlinkSync(evidencePath);
    symlinkSync(external, evidencePath);
    expect(() => generatorSupplyEvidencePaths(symlinkRoot)).toThrowError(
      expect.objectContaining<Partial<GeneratorSupplyProfileError>>({
        code: "GENERATOR_SUPPLY_BINDING_MISMATCH",
        path: "/tools/generator-supply/v1/evidence/artifacts.json",
      }),
    );
  });

  it("derives vulnerability and replay status from raw bound evidence", () => {
    const root = supplyFixture();
    const reportPath = join(
      root,
      "tools/generator-supply/v1/evidence/vulnerability/grype-darwin.json",
    );
    const report = JSON.parse(readFileSync(reportPath, "utf8")) as {
      ignoredMatches?: unknown[];
    };
    report.ignoredMatches = [{ hidden: true }];
    writeFileSync(reportPath, `${JSON.stringify(report)}\n`);
    expect(() => buildGeneratorSupplyProfile(root)).toThrowError(
      expect.objectContaining<Partial<GeneratorSupplyProfileError>>({
        code: "GENERATOR_SUPPLY_EVIDENCE_MISMATCH",
        path: "/tools/generator-supply/v1/evidence/vulnerability/grype-darwin.json",
      }),
    );

    const nestedRoot = supplyFixture();
    const nestedPath = join(
      nestedRoot,
      "tools/generator-supply/v1/evidence/vulnerability/grype-darwin.json",
    );
    const nested = JSON.parse(readFileSync(nestedPath, "utf8")) as {
      matches: { vulnerability: Record<string, unknown> }[];
    };
    nested.matches[0]!.vulnerability.undeclared = true;
    writeFileSync(nestedPath, `${JSON.stringify(nested)}\n`);
    expect(() => buildGeneratorSupplyProfile(nestedRoot)).toThrowError(
      expect.objectContaining<Partial<GeneratorSupplyProfileError>>({
        code: "GENERATOR_SUPPLY_EVIDENCE_MISMATCH",
        path: "/tools/generator-supply/v1/evidence/vulnerability/grype-darwin.json/matches/0/vulnerability",
      }),
    );
  });

  it("rejects archive provenance that no longer equals effective executable bytes", () => {
    const root = supplyFixture();
    const path = join(root, "tools/generator-supply/v1/evidence/artifacts.json");
    const evidence = JSON.parse(readFileSync(path, "utf8")) as {
      archives: { effectiveExecutables: { effectiveSha256: string }[] }[];
    };
    evidence.archives[0]!.effectiveExecutables[0]!.effectiveSha256 = "0".repeat(64);
    writeFileSync(path, `${JSON.stringify(evidence)}\n`);
    expect(() => buildGeneratorSupplyProfile(root)).toThrowError(
      expect.objectContaining<Partial<GeneratorSupplyProfileError>>({
        code: "GENERATOR_SUPPLY_EVIDENCE_MISMATCH",
        path: "/artifacts/archives/node-darwin-arm64/effectiveExecutables",
      }),
    );
  });

  it("rejects undeclared node_modules runtime cache evidence", () => {
    const root = supplyFixture();
    const path = join(root, "tools/generator-supply/v1/evidence/npm.json");
    const evidence = JSON.parse(readFileSync(path, "utf8")) as {
      installed: { nodeModules: { cacheEntries: number; topLevelEntries: string[] } }[];
    };
    evidence.installed[0]!.nodeModules.cacheEntries = 1;
    evidence.installed[0]!.nodeModules.topLevelEntries.push(".vite");
    writeFileSync(path, `${JSON.stringify(evidence)}\n`);
    expect(() => buildGeneratorSupplyProfile(root)).toThrowError(
      expect.objectContaining<Partial<GeneratorSupplyProfileError>>({
        code: "GENERATOR_SUPPLY_EVIDENCE_MISMATCH",
        path: "/npm/installed/darwin-arm64/nodeModules",
      }),
    );
  });

  it("rejects SBOM PURL multiplicity drift instead of collapsing to sets", () => {
    const root = supplyFixture();
    const path = join(root, "tools/generator-supply/v1/evidence/sbom/darwin-bundle.cdx.json");
    const evidence = JSON.parse(readFileSync(path, "utf8")) as {
      components: { purl?: string }[];
    };
    evidence.components[0]!.purl = evidence.components[1]!.purl;
    writeFileSync(path, `${JSON.stringify(evidence)}\n`);
    expect(() => buildGeneratorSupplyProfile(root)).toThrowError(
      expect.objectContaining<Partial<GeneratorSupplyProfileError>>({
        code: "GENERATOR_SUPPLY_EVIDENCE_MISMATCH",
        path: "/sbom/darwin-bundle/crossFormat",
      }),
    );

    const grypeRoot = supplyFixture();
    const grypePath = join(
      grypeRoot,
      "tools/generator-supply/v1/evidence/vulnerability/grype-darwin.json",
    );
    const grype = JSON.parse(readFileSync(grypePath, "utf8")) as {
      matches: { artifact: { purl: string } }[];
    };
    grype.matches[0]!.artifact.purl = "pkg:generic/not-bound@0";
    writeFileSync(grypePath, `${JSON.stringify(grype)}\n`);
    expect(() => buildGeneratorSupplyProfile(grypeRoot)).toThrowError(
      expect.objectContaining<Partial<GeneratorSupplyProfileError>>({
        code: "GENERATOR_SUPPLY_EVIDENCE_MISMATCH",
        path: "/tools/generator-supply/v1/evidence/vulnerability/grype-darwin.json/matches/0/artifact",
      }),
    );

    const missingPurlRoot = supplyFixture();
    const rawPath = join(
      missingPurlRoot,
      "tools/generator-supply/v1/evidence/sbom/darwin-bundle.syft.json",
    );
    const raw = JSON.parse(readFileSync(rawPath, "utf8")) as {
      artifacts: { purl?: string }[];
    };
    delete raw.artifacts[0]!.purl;
    writeFileSync(rawPath, `${JSON.stringify(raw)}\n`);
    expect(() => buildGeneratorSupplyProfile(missingPurlRoot)).toThrowError(
      expect.objectContaining<Partial<GeneratorSupplyProfileError>>({
        code: "GENERATOR_SUPPLY_EVIDENCE_MISMATCH",
        path: "/sbom/darwin-bundle.syft/artifacts/0/purl",
      }),
    );

    const malformedPurlRoot = supplyFixture();
    const cdxPath = join(
      malformedPurlRoot,
      "tools/generator-supply/v1/evidence/sbom/darwin-bundle.cdx.json",
    );
    const cdx = JSON.parse(readFileSync(cdxPath, "utf8")) as {
      components: { purl?: string }[];
    };
    cdx.components.find((component) => component.purl !== undefined)!.purl = "not-a-purl";
    writeFileSync(cdxPath, `${JSON.stringify(cdx)}\n`);
    expect(() => buildGeneratorSupplyProfile(malformedPurlRoot)).toThrowError(
      expect.objectContaining<Partial<GeneratorSupplyProfileError>>({
        code: "GENERATOR_SUPPLY_EVIDENCE_MISMATCH",
        path: "/sbom/darwin-bundle/components/0/purl",
      }),
    );
  });

  it("binds formatted SBOM non-PURL records to reviewed canonical multisets", () => {
    for (const scope of ["darwin-bundle", "linux-bundle", "ubuntu-image"] as const) {
      const cdx = JSON.parse(
        readFileSync(
          join(repositoryRoot, `tools/generator-supply/v1/evidence/sbom/${scope}.cdx.json`),
          "utf8",
        ),
      ) as { components: Record<string, unknown>[] };
      const spdx = JSON.parse(
        readFileSync(
          join(repositoryRoot, `tools/generator-supply/v1/evidence/sbom/${scope}.spdx.json`),
          "utf8",
        ),
      ) as { packages: Record<string, unknown>[] };
      expect(() =>
        validateGeneratorSupplyFormattedNonPurlClosureForTest(scope, cdx.components, spdx.packages),
      ).not.toThrow();
      expect(() =>
        validateGeneratorSupplyFormattedNonPurlClosureForTest(
          scope,
          [...cdx.components].reverse(),
          [...spdx.packages].reverse(),
        ),
      ).not.toThrow();

      const nonPurlIndex = cdx.components.findIndex(
        (component) => !Object.hasOwn(component, "purl"),
      );
      if (nonPurlIndex === -1) throw new Error(`${scope} CycloneDX lacks non-PURL records`);
      const mutated = structuredClone(cdx.components);
      mutated[nonPurlIndex]!.name = `${String(mutated[nonPurlIndex]!.name)}-drift`;
      const deleted = cdx.components.filter((_, index) => index !== nonPurlIndex);
      const duplicated = [...cdx.components, structuredClone(cdx.components[nonPurlIndex]!)];
      for (const candidate of [mutated, deleted, duplicated]) {
        expect(() =>
          validateGeneratorSupplyFormattedNonPurlClosureForTest(scope, candidate, spdx.packages),
        ).toThrowError(
          expect.objectContaining<Partial<GeneratorSupplyProfileError>>({
            code: "GENERATOR_SUPPLY_EVIDENCE_MISMATCH",
            path: `/sbom/${scope}/formattedNonPurlClosure`,
          }),
        );
      }

      const spdxNonPurlIndex = spdx.packages.findIndex((packageRecord) => {
        const references = packageRecord.externalRefs;
        return (
          !Array.isArray(references) ||
          !references.some(
            (reference) =>
              typeof reference === "object" &&
              reference !== null &&
              (reference as Record<string, unknown>).referenceType === "purl",
          )
        );
      });
      const spdxCandidates: Record<string, unknown>[][] = [];
      if (spdxNonPurlIndex === -1) {
        spdxCandidates.push([
          ...spdx.packages,
          { SPDXID: "SPDXRef-injected-non-purl", name: "injected-non-purl" },
        ]);
      } else {
        const mutatedSpdx = structuredClone(spdx.packages);
        mutatedSpdx[spdxNonPurlIndex]!.name =
          `${String(mutatedSpdx[spdxNonPurlIndex]!.name)}-drift`;
        spdxCandidates.push(
          mutatedSpdx,
          spdx.packages.filter((_, index) => index !== spdxNonPurlIndex),
          [...spdx.packages, structuredClone(spdx.packages[spdxNonPurlIndex]!)],
        );
      }
      for (const candidate of spdxCandidates) {
        expect(() =>
          validateGeneratorSupplyFormattedNonPurlClosureForTest(scope, cdx.components, candidate),
        ).toThrowError(
          expect.objectContaining<Partial<GeneratorSupplyProfileError>>({
            code: "GENERATOR_SUPPLY_EVIDENCE_MISMATCH",
            path: `/sbom/${scope}/formattedNonPurlClosure`,
          }),
        );
      }
    }
  });

  it("rejects stale OSV scanner and current wheelhouse runner receipts", () => {
    const osvRoot = supplyFixture();
    const receiptPath = join(
      osvRoot,
      "tools/generator-supply/v1/evidence/vulnerability/osv-scanner-receipt.json",
    );
    const receipt = JSON.parse(readFileSync(receiptPath, "utf8")) as { version: string };
    receipt.version = "2.5.0";
    writeFileSync(receiptPath, `${JSON.stringify(receipt)}\n`);
    expect(() => buildGeneratorSupplyProfile(osvRoot)).toThrowError(
      expect.objectContaining<Partial<GeneratorSupplyProfileError>>({
        code: "GENERATOR_SUPPLY_EVIDENCE_MISMATCH",
        path: "/vulnerability/osvScannerReceipt",
      }),
    );

    const osvMetadataRoot = supplyFixture();
    const osvPath = join(
      osvMetadataRoot,
      "tools/generator-supply/v1/evidence/vulnerability/osv.json",
    );
    const osv = JSON.parse(readFileSync(osvPath, "utf8")) as {
      experimental_config: { licenses: { allowlist: unknown } };
    };
    osv.experimental_config.licenses.allowlist = [];
    writeFileSync(osvPath, `${JSON.stringify(osv)}\n`);
    expect(() => buildGeneratorSupplyProfile(osvMetadataRoot)).toThrowError(
      expect.objectContaining<Partial<GeneratorSupplyProfileError>>({
        code: "GENERATOR_SUPPLY_EVIDENCE_MISMATCH",
        path: "/vulnerability/osv/experimental_config/licenses",
      }),
    );

    const runnerRoot = supplyFixture();
    const lineagePath = join(
      runnerRoot,
      "tools/generator-supply/v1/evidence/wheelhouse-repair-lineage.json",
    );
    const lineage = JSON.parse(readFileSync(lineagePath, "utf8")) as {
      currentRunnerSha256: string;
    };
    lineage.currentRunnerSha256 = "0".repeat(64);
    writeFileSync(lineagePath, `${JSON.stringify(lineage)}\n`);
    expect(() => buildGeneratorSupplyProfile(runnerRoot)).toThrowError(
      expect.objectContaining<Partial<GeneratorSupplyProfileError>>({
        code: "GENERATOR_SUPPLY_EVIDENCE_MISMATCH",
        path: "/wheelhouseRepairLineage/currentRunner",
      }),
    );
  });

  it("derives replay summary from four reports and rejects projection drift", () => {
    const root = supplyFixture();
    expect(buildGeneratorSupplyReplaySummary(root).status).toBe(
      "DUAL_PLATFORM_TWO_ARCHIVES_EXACT_REPLAY_VERIFIED",
    );
    const reportPath = join(root, "tools/generator-supply/v1/evidence/replay/linux-b.json");
    const report = JSON.parse(readFileSync(reportPath, "utf8")) as {
      projectionArchiveSizeBytes: number;
    };
    report.projectionArchiveSizeBytes += 1;
    writeFileSync(reportPath, `${JSON.stringify(report)}\n`);
    expect(() => buildGeneratorSupplyReplaySummary(root)).toThrowError(
      expect.objectContaining<Partial<GeneratorSupplyProfileError>>({
        code: "GENERATOR_SUPPLY_EVIDENCE_MISMATCH",
        path: "/tools/generator-supply/v1/evidence/replay/linux-b.json",
      }),
    );
  });

  it("makes replay summary freshness fail when a same-boundary receipt changes", () => {
    const root = supplyFixture();
    const receiptPath = join(
      root,
      "tools/generator-supply/v1/evidence/replay/darwin-isolation.json",
    );
    const receipt = JSON.parse(readFileSync(receiptPath, "utf8")) as {
      notGateClosure: boolean;
    };
    receipt.notGateClosure = false;
    writeFileSync(receiptPath, `${JSON.stringify(receipt)}\n`);
    expect(() => assertGeneratorSupplyReplaySummaryCurrent(root)).toThrowError(
      expect.objectContaining<Partial<GeneratorSupplyProfileError>>({
        code: "GENERATOR_SUPPLY_PROFILE_STALE",
        path: "/tools/generator-supply/v1/evidence/replay.json",
      }),
    );
  });

  it("binds the source replay authority to current wrapper bytes", () => {
    const root = supplyFixture();
    const wrapperPath = join(root, "scripts/replay-platform-generators-isolated.sh");
    writeFileSync(wrapperPath, `${readFileSync(wrapperPath, "utf8")}# drift\n`);
    expect(() => buildGeneratorSupplyReplaySummary(root)).toThrowError(
      expect.objectContaining<Partial<GeneratorSupplyProfileError>>({
        code: "GENERATOR_SUPPLY_BINDING_MISMATCH",
        path: "/profile/replayAuthority/wrapperSha256",
      }),
    );
  });

  it("rejects a projection receipt mutation instead of trusting its digest", () => {
    const root = supplyFixture();
    const projectionPath = join(root, "tools/generator-supply/v1/evidence/replay/projection.json");
    const projection = JSON.parse(readFileSync(projectionPath, "utf8")) as {
      treeSha: string;
    };
    projection.treeSha = "f".repeat(40);
    writeFileSync(projectionPath, `${JSON.stringify(projection)}\n`);
    expect(() => buildGeneratorSupplyProfile(root)).toThrowError(
      expect.objectContaining<Partial<GeneratorSupplyProfileError>>({
        code: "GENERATOR_SUPPLY_EVIDENCE_MISMATCH",
        path: "/replay/projection",
      }),
    );
  });

  it("rejects nested A/B isolation probe mutation", () => {
    const root = supplyFixture();
    const receiptPath = join(
      root,
      "tools/generator-supply/v1/evidence/replay/darwin-isolation.json",
    );
    const receipt = JSON.parse(readFileSync(receiptPath, "utf8")) as {
      probes: { a: { node: { exitCode: number } } };
    };
    receipt.probes.a.node.exitCode = 0;
    writeFileSync(receiptPath, `${JSON.stringify(receipt)}\n`);
    expect(() => buildGeneratorSupplyProfile(root)).toThrowError(
      expect.objectContaining<Partial<GeneratorSupplyProfileError>>({
        code: "GENERATOR_SUPPLY_EVIDENCE_MISMATCH",
        path: "/replay/darwin-arm64/isolation/probes/a/node",
      }),
    );
  });

  it("validates the Linux trusted-stdout probe without ambient replay fixtures", () => {
    const baseline = {
      command: "read /proc/1/fd/1 trusted runner stdout channel",
      exitCode: 1,
      stdout: "",
      stderr: "cat: /proc/1/fd/1: No such file or directory",
    };
    expect(() => validateGeneratorSupplyLinuxStdoutProbeForTest(baseline)).not.toThrow();
    for (const mutation of [
      { ...baseline, stderr: "access denied" },
      { ...baseline, exitCode: 0 },
      { ...baseline, undeclared: true },
    ]) {
      expect(() => validateGeneratorSupplyLinuxStdoutProbeForTest(mutation)).toThrowError(
        expect.objectContaining<Partial<GeneratorSupplyProfileError>>({
          code: "GENERATOR_SUPPLY_EVIDENCE_MISMATCH",
          path: "/replay/linux-amd64/isolation/probes/a/stdoutChannel",
        }),
      );
    }
  });

  it("validates Linux uid, groups, capabilities, and NNP without ambient replay fixtures", () => {
    const stdout = [
      "Uid:\t65534\t65534\t65534\t65534",
      "Gid:\t65534\t65534\t65534\t65534",
      "Groups:\t",
      "CapInh:\t0000000000000000",
      "CapPrm:\t0000000000000000",
      "CapEff:\t0000000000000000",
      "CapBnd:\t0000000000000000",
      "CapAmb:\t0000000000000000",
      "NoNewPrivs:\t1",
    ].join("\n");
    const baseline = {
      command: "read uid gid groups capabilities and no-new-privileges",
      exitCode: 0,
      stdout,
      stderr: "",
    };
    expect(() => validateGeneratorSupplyLinuxIdentityProbeForTest(baseline)).not.toThrow();
    for (const mutation of [
      { ...baseline, undeclared: true },
      {
        ...baseline,
        stdout: stdout.replace("Uid:\t65534\t65534\t65534\t65534", "Uid:\t0\t0\t0\t0"),
      },
      {
        ...baseline,
        stdout: stdout.replace("Gid:\t65534\t65534\t65534\t65534", "Gid:\t0\t0\t0\t0"),
      },
      { ...baseline, stdout: stdout.replace("CapBnd:\t0000000000000000", "CapBnd:\t1") },
      { ...baseline, stdout: stdout.replace("Groups:\t", "Groups:\t1") },
      { ...baseline, stdout: stdout.replace("NoNewPrivs:\t1", "NoNewPrivs:\t0") },
    ]) {
      expect(() => validateGeneratorSupplyLinuxIdentityProbeForTest(mutation)).toThrowError(
        expect.objectContaining<Partial<GeneratorSupplyProfileError>>({
          code: "GENERATOR_SUPPLY_EVIDENCE_MISMATCH",
          path: "/replay/linux-amd64/isolation/probes/a/identity",
        }),
      );
    }
  });

  it("validates immutable Ubuntu rootfs inspection without ambient replay fixtures", () => {
    const baseline = {
      formatVersion: "cloud-agents-generator-replay-archive-inspection/v1",
      profile: "rootfs",
      manifestAlgorithm: "utf8-bytewise-sorted-path-type-mode-size-sha256-linktarget-nul-v1",
      manifestSha256: "b2f581777b04657540dffa9b4f6ba98e6e0d310ea11b100cd84e6fcf19ec4af6",
      entries: 3448,
      regularFiles: 2587,
      directories: 661,
      symlinks: 198,
      hardlinks: 2,
      unsafeEntries: 0,
      duplicateEntries: 0,
      specialEntries: 0,
      linkPrefixDescendants: 0,
      linkCycles: 0,
    };
    expect(() => validateGeneratorSupplyRootfsInspectionForTest(baseline)).not.toThrow();
    for (const [key, value] of [
      ["manifestSha256", "0".repeat(64)],
      ["entries", 3449],
      ["regularFiles", 2588],
      ["directories", 662],
      ["symlinks", 199],
      ["hardlinks", 3],
      ["unsafeEntries", 1],
      ["undeclared", true],
    ] as const) {
      expect(() =>
        validateGeneratorSupplyRootfsInspectionForTest({ ...baseline, [key]: value }),
      ).toThrowError(
        expect.objectContaining<Partial<GeneratorSupplyProfileError>>({
          code: "GENERATOR_SUPPLY_EVIDENCE_MISMATCH",
          path: "/replay/linux-amd64/isolation/ubuntuRootfs/archiveInspection",
        }),
      );
    }
  });

  it("validates replay projection member count without A/B comparison shortcuts", () => {
    expect(() => validateGeneratorSupplyProjectionArchiveMembersForTest(1)).not.toThrow();
    for (const invalid of [false, 0, 1.5, -1, Number.NaN]) {
      expect(() => validateGeneratorSupplyProjectionArchiveMembersForTest(invalid)).toThrowError(
        expect.objectContaining<Partial<GeneratorSupplyProfileError>>({
          code: "GENERATOR_SUPPLY_EVIDENCE_MISMATCH",
          path: "/tools/generator-supply/v1/evidence/replay/linux-a.json/projectionArchiveMembers",
        }),
      );
    }
  });

  it("rejects replay and rejected-executor object key extensions", () => {
    const replayRoot = supplyFixture();
    const replayPath = join(replayRoot, "tools/generator-supply/v1/evidence/replay/darwin-a.json");
    const replay = JSON.parse(readFileSync(replayPath, "utf8")) as Record<string, unknown>;
    replay.undeclared = true;
    writeFileSync(replayPath, `${JSON.stringify(replay)}\n`);
    expect(() => buildGeneratorSupplyProfile(replayRoot)).toThrowError(
      expect.objectContaining<Partial<GeneratorSupplyProfileError>>({
        code: "GENERATOR_SUPPLY_EVIDENCE_MISMATCH",
        path: "/tools/generator-supply/v1/evidence/replay/darwin-a.json",
      }),
    );

    const rejectedRoot = supplyFixture();
    const rejectedPath = join(
      rejectedRoot,
      "tools/generator-supply/v1/evidence/replay/rejected-executor.json",
    );
    const rejected = JSON.parse(readFileSync(rejectedPath, "utf8")) as Record<string, unknown>;
    rejected.undeclared = true;
    writeFileSync(rejectedPath, `${JSON.stringify(rejected)}\n`);
    expect(() => buildGeneratorSupplyProfile(rejectedRoot)).toThrowError(
      expect.objectContaining<Partial<GeneratorSupplyProfileError>>({
        code: "GENERATOR_SUPPLY_EVIDENCE_MISMATCH",
        path: "/replay/rejectedExecutor",
      }),
    );
  });

  it("rejects package-lock closure and whole-summary identity mutations", () => {
    const lockRoot = supplyFixture();
    const lockPath = join(lockRoot, "tools/generator-supply/npm/package-lock.json");
    const lock = JSON.parse(readFileSync(lockPath, "utf8")) as {
      packages: Record<string, { version?: string }>;
    };
    lock.packages["node_modules/oxfmt"]!.version = "0.62.1";
    writeFileSync(lockPath, `${JSON.stringify(lock)}\n`);
    expect(() => buildGeneratorSupplyProfile(lockRoot)).toThrowError(
      expect.objectContaining<Partial<GeneratorSupplyProfileError>>({
        code: "GENERATOR_SUPPLY_EVIDENCE_MISMATCH",
        path: "/npm/packageLock",
      }),
    );

    const summaryRoot = supplyFixture();
    const summaryPath = join(summaryRoot, "tools/generator-supply/v1/evidence/sbom-summary.json");
    const summary = JSON.parse(readFileSync(summaryPath, "utf8")) as Record<string, unknown>;
    summary.undeclared = true;
    writeFileSync(summaryPath, `${JSON.stringify(summary)}\n`);
    expect(() => buildGeneratorSupplyProfile(summaryRoot)).toThrowError(
      expect.objectContaining<Partial<GeneratorSupplyProfileError>>({
        code: "GENERATOR_SUPPLY_EVIDENCE_MISMATCH",
        path: "/sbom/summary",
      }),
    );

    const hiddenLockRoot = supplyFixture();
    const npmPath = join(hiddenLockRoot, "tools/generator-supply/v1/evidence/npm.json");
    const npm = JSON.parse(readFileSync(npmPath, "utf8")) as {
      installed: { hiddenLock: { sha256: string } }[];
    };
    npm.installed[0]!.hiddenLock.sha256 = "0".repeat(64);
    writeFileSync(npmPath, `${JSON.stringify(npm)}\n`);
    expect(() => buildGeneratorSupplyProfile(hiddenLockRoot)).toThrowError(
      expect.objectContaining<Partial<GeneratorSupplyProfileError>>({
        code: "GENERATOR_SUPPLY_EVIDENCE_MISMATCH",
        path: "/npm/installed/darwin-arm64/hiddenLock",
      }),
    );
  });
});
