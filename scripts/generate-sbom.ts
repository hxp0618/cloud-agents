import { readFileSync, writeFileSync } from "node:fs";
import { basename, join, resolve } from "node:path";

type JsonRecord = Record<string, unknown>;

const root = resolve(import.meta.dirname, "..");
const outputDirectory = parseOutputDirectory(process.argv.slice(2));
const packagePaths = [
  "package.json",
  "packages/cloud-agent-protocol/package.json",
  "packages/cloud-agent-provider-api/package.json",
  "packages/cloud-agent-runtime/package.json",
  "packages/cloud-agent-provider-codex/package.json",
  "packages/cloud-agent-provider-claude/package.json",
  "packages/cloud-agent-testkit/package.json",
  "packages/cloud-agent-distribution/package.json",
];
const manifests = packagePaths.map((path) => ({ path, manifest: readJson(join(root, path)) }));
const packages = manifests.map(({ path, manifest }, index) => ({
  SPDXID: `SPDXRef-Package-${index + 1}`,
  name: stringValue(manifest.name, `${path} name`),
  versionInfo: stringValue(manifest.version, `${path} version`),
  downloadLocation: "NOASSERTION",
  filesAnalyzed: false,
  licenseConcluded: stringValue(manifest.license, `${path} license`),
  licenseDeclared: stringValue(manifest.license, `${path} license`),
  supplier: "Organization: hxp0618/cloud-agents contributors",
  externalRefs: [
    {
      referenceCategory: "PACKAGE-MANAGER",
      referenceType: "purl",
      referenceLocator: `pkg:npm/${encodeURIComponent(stringValue(manifest.name, `${path} name`))}@${encodeURIComponent(stringValue(manifest.version, `${path} version`))}`,
    },
  ],
}));
const byName = new Map(packages.map((item) => [item.name, item]));
const relationships = manifests.flatMap(({ manifest }, index) => {
  const dependencies = recordValue(manifest.dependencies);
  return Object.keys(dependencies).flatMap((name) => {
    const dependency = byName.get(name);
    return dependency
      ? [
          {
            spdxElementId: packages[index]!.SPDXID,
            relationshipType: "DEPENDS_ON",
            relatedSpdxElement: dependency.SPDXID,
          },
        ]
      : [];
  });
});
const document = {
  spdxVersion: "SPDX-2.3",
  dataLicense: "CC0-1.0",
  SPDXID: "SPDXRef-DOCUMENT",
  name: "cloud-agents-portable-runtime-rc",
  documentNamespace: `https://github.com/hxp0618/cloud-agents/sbom/${Date.now()}`,
  creationInfo: {
    created: new Date().toISOString(),
    creators: ["Tool: cloud-agents/scripts/generate-sbom.ts"],
  },
  packages,
  relationships,
};
writeFileSync(join(outputDirectory, "sbom.spdx.json"), `${JSON.stringify(document, null, 2)}\n`, {
  mode: 0o444,
});

function parseOutputDirectory(args: string[]): string {
  if (args.length !== 2 || args[0] !== "--output-dir") {
    throw new Error("Usage: generate-sbom.ts --output-dir <existing-directory>");
  }
  return resolve(root, args[1]!);
}

function readJson(path: string): JsonRecord {
  const value: unknown = JSON.parse(readFileSync(path, "utf8"));
  if (!recordValue(value)) throw new Error(`${basename(path)} must contain a JSON object.`);
  return value;
}

function recordValue(value: unknown): JsonRecord {
  return value !== null && typeof value === "object" && !Array.isArray(value)
    ? (value as JsonRecord)
    : {};
}

function stringValue(value: unknown, label: string): string {
  if (typeof value !== "string" || !value.trim()) throw new Error(`${label} is missing.`);
  return value;
}
