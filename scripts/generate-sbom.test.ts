import { execFileSync } from "node:child_process";
import { mkdirSync, mkdtempSync, readFileSync, rmSync } from "node:fs";
import { join } from "node:path";
import { expect, it } from "vitest";

it("describes the released Runtime packages and their declared dependency graph", () => {
  mkdirSync(".tmp", { recursive: true });
  const output = mkdtempSync(join(process.cwd(), ".tmp/runtime-sbom-test-"));
  try {
    execFileSync(process.execPath, ["scripts/generate-sbom.ts", "--output-dir", output], {
      cwd: process.cwd(),
      stdio: "pipe",
    });
    const document = JSON.parse(readFileSync(join(output, "sbom.spdx.json"), "utf8")) as {
      packages: Array<{ SPDXID: string; name: string }>;
      relationships: Array<{
        spdxElementId: string;
        relatedSpdxElement: string;
        relationshipType: string;
      }>;
    };
    const names = document.packages.map(({ name }) => name);
    expect(names).not.toContain("cloud-agents-portable-runtime");
    expect(names).toContain("@anthropic-ai/claude-agent-sdk");
    expect(names.filter((name) => name.startsWith("@cloud-agents/"))).toHaveLength(7);
    const namesByID = new Map(document.packages.map(({ SPDXID, name }) => [SPDXID, name]));
    const edges = document.relationships.map(
      ({ spdxElementId, relatedSpdxElement, relationshipType }) =>
        `${namesByID.get(spdxElementId)} ${relationshipType} ${namesByID.get(relatedSpdxElement)}`,
    );
    expect(edges).toContain(
      "@cloud-agents/cloud-agent-provider-claude DEPENDS_ON @anthropic-ai/claude-agent-sdk",
    );
    expect(edges).toContain(
      "@cloud-agents/cloud-agent-distribution DEPENDS_ON @cloud-agents/cloud-agent-runtime",
    );
    expect(edges).toHaveLength(13);
  } finally {
    rmSync(output, { recursive: true, force: true });
  }
});
