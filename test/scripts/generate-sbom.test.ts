import { execFileSync } from "node:child_process";
import { mkdtempSync, readFileSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";

import { describe, expect, it } from "vitest";

const repositoryRoot = join(import.meta.dirname, "../..");

describe("runtime SPDX generator", () => {
  it("produces stable identity and creation metadata for a fixed source date", () => {
    const first = mkdtempSync(join(tmpdir(), "cloud-agents-sbom-first-"));
    const second = mkdtempSync(join(tmpdir(), "cloud-agents-sbom-second-"));
    try {
      const environment = { ...process.env, SOURCE_DATE_EPOCH: "1760000000" };
      for (const outputDirectory of [first, second]) {
        execFileSync(
          process.execPath,
          ["scripts/generate-sbom.ts", "--output-dir", outputDirectory],
          { cwd: repositoryRoot, env: environment, stdio: "pipe" },
        );
      }
      const firstBytes = readFileSync(join(first, "sbom.spdx.json"));
      const secondBytes = readFileSync(join(second, "sbom.spdx.json"));
      expect(secondBytes).toEqual(firstBytes);
      const document = JSON.parse(firstBytes.toString("utf8")) as {
        documentNamespace: string;
        creationInfo: { created: string };
      };
      expect(document.documentNamespace).toMatch(
        /^https:\/\/github\.com\/hxp0618\/cloud-agents\/sbom\/[0-9a-f]{64}$/u,
      );
      expect(document.creationInfo.created).toBe("2025-10-09T08:53:20.000Z");
    } finally {
      rmSync(first, { force: true, recursive: true });
      rmSync(second, { force: true, recursive: true });
    }
  });
});
