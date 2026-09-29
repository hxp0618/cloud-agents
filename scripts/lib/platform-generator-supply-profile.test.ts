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
});
