import assert from "node:assert/strict";
import { execFileSync } from "node:child_process";
import { createHash } from "node:crypto";
import { mkdirSync, readFileSync, writeFileSync } from "node:fs";
import { dirname, resolve } from "node:path";
import { migrationDigest } from "./lib/platform-migration-json";
import { splitPostgresStatements, classifyMigrationStatement } from "./lib/platform-migration-sql";
import { validateObjectIdentity } from "./lib/platform-migration-projection";

// One exact product successor; frozen predecessor artifacts are inputs, never rewritten.
const root = resolve(import.meta.dirname, "..");
const mode = process.argv[2];
assert.ok(mode === "--write" || mode === "--check");
const base = "services/control-plane/migrations/product/000060";
const sqlPath = "services/control-plane/migrations/000060_add_sandbox_preview_ports.sql";
const read = (path: string) => readFileSync(resolve(root, path));
const json = (path: string) => JSON.parse(read(path).toString());
const bytes = (value: unknown) => Buffer.from(JSON.stringify(value, null, 2) + "\n");
const digest = (data: Uint8Array) => "sha256:" + createHash("sha256").update(data).digest("hex");
const artifact = (path: string, data = read(path)) => ({
  path,
  mode: "100644",
  size_bytes: data.length,
  sha256: digest(data),
});
const prior = json("services/control-plane/migrations/product/000059/manifest.json");
const catalog = json(prior.schema_bundle.migrations.at(-1).catalog_contract.path);
catalog.schema_head = "000060";
const sql = read(sqlPath);
catalog.source_descriptors.push({
  migration_id: "000060",
  sql_sha256: digest(sql),
  statements: splitPostgresStatements(sql).map((s) => ({
    index: s.index,
    start: s.start,
    end: s.end,
    sha256: s.sha256,
    classification: classifyMigrationStatement(s, "000060"),
  })),
});
const additions: any[] = [];
for (const [name, arguments_] of [
  ["register_sandbox_preview_port_v1", ["text", "text", "integer", "text"]],
  ["revoke_sandbox_preview_port_v1", ["text", "text", "integer", "text"]],
] as const)
  additions.push({
    kind: "function",
    identity: {
      schema: "cloud_agents",
      name,
      arguments: arguments_.map((argument) => ({ schema: "pg_catalog", name: argument })),
    },
  });
for (const item of additions) validateObjectIdentity(item);
catalog.declared_object_identities.push(...additions);
const catalogBytes = bytes(catalog);
const catalogArtifact = artifact(`${base}/catalog/schema-000059.json`, catalogBytes);
const schema = structuredClone(prior.schema_bundle);
schema.schema_head = "000060";
schema.migrations.push({
  ...structuredClone(schema.migrations.at(-1)),
  id: "000060",
  name: "add_sandbox_preview_ports",
  predecessor_id: "000059",
  schema_from: "000059",
  schema_to: "000060",
  sql_artifact: artifact(sqlPath),
  predecessor_catalog_contract: prior.schema_bundle.migrations.at(-1).catalog_contract,
  catalog_contract: catalogArtifact,
});
const schemaDigest = migrationDigest({
  domain: "cloud-agents-platform-schema-bundle/v1",
  schema_bundle: schema,
});
const schemaBytes = bytes({
  format_version: "cloud-agents-platform-schema-bundle/v1",
  schema_bundle: schema,
  schema_bundle_digest: schemaDigest,
});
const schemaArtifact = artifact(`${base}/schema-bundle.json`, schemaBytes);
const manifest = structuredClone(prior);
manifest.schema_bundle = schema;
manifest.schema_bundle_digest = schemaDigest;
manifest.runtime_artifacts.push(artifact(sqlPath), catalogArtifact, schemaArtifact);
manifest.runtime_artifacts.sort((a: any, b: any) => a.path.localeCompare(b.path, "en"));
delete manifest.manifest_digest;
manifest.manifest_digest = migrationDigest(manifest);
const manifestBytes = bytes(manifest);
const runnerBinding = (version: string, manifestRaw: Buffer, schemaBundleRaw: Buffer) => {
  const manifestValue = JSON.parse(manifestRaw.toString());
  const schemaBundleValue = JSON.parse(schemaBundleRaw.toString());
  assert.equal(manifestValue.schema_bundle.schema_head, version);
  assert.equal(manifestValue.schema_bundle.migrations.length, Number(version));
  assert.equal(schemaBundleValue.schema_bundle_digest, manifestValue.schema_bundle_digest);
  const versionBase = `services/control-plane/migrations/product/${version}`;
  return `{selectorID:"product-${version}",schemaHead:"${version}",migrationCount:${Number(version)},
manifestPath:"${versionBase}/manifest.json",manifestSizeBytes:${manifestRaw.length},manifestRawDigest:"${digest(manifestRaw)}",manifestDigest:"${manifestValue.manifest_digest}",
schemaBundlePath:"${versionBase}/schema-bundle.json",schemaBundleSizeBytes:${schemaBundleRaw.length},schemaBundleRawDigest:"${digest(schemaBundleRaw)}",schemaBundleDigest:"${manifestValue.schema_bundle_digest}"}`;
};
const runnerBindings = Array.from({ length: Number(schema.schema_head) - 14 }, (_, index) => {
  const version = String(index + 15).padStart(6, "0");
  const versionBase = `services/control-plane/migrations/product/${version}`;
  return runnerBinding(
    version,
    version === schema.schema_head ? manifestBytes : read(`${versionBase}/manifest.json`),
    version === schema.schema_head ? schemaBytes : read(`${versionBase}/schema-bundle.json`),
  );
});
const binding = `// Code generated by scripts/generate-foundation-migration-package.ts; DO NOT EDIT.
package localmigration
var productFoundationRunnerBindings = [...]generatedRunnerBindingSelector{
${runnerBindings.join(",\n")},
}
`;
for (const [path, data] of new Map([
  [catalogArtifact.path, catalogBytes],
  [schemaArtifact.path, schemaBytes],
  [`${base}/manifest.json`, manifestBytes],
  [
    "services/control-plane/internal/localmigration/product_foundation_generated.go",
    execFileSync("gofmt", { input: binding }),
  ],
])) {
  if (mode === "--write") {
    mkdirSync(dirname(resolve(root, path)), { recursive: true });
    writeFileSync(resolve(root, path), data);
  } else assert.deepEqual(read(path), data, `${path} is stale`);
}
process.stdout.write(`product-000060 ${mode}: ${schemaDigest}\n`);
