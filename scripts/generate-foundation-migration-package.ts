import assert from "node:assert/strict";
import { execFileSync } from "node:child_process";
import { createHash } from "node:crypto";
import { mkdirSync, readFileSync, readdirSync, writeFileSync } from "node:fs";
import { dirname, resolve } from "node:path";
import { migrationDigest } from "./lib/platform-migration-json";
import { splitPostgresStatements, classifyMigrationStatement } from "./lib/platform-migration-sql";
import { validateObjectIdentity } from "./lib/platform-migration-projection";
import { migrationObjectIdentity } from "./lib/platform-migration-bundle";

// One exact product successor; frozen predecessor artifacts are inputs, never rewritten.
const root = resolve(import.meta.dirname, "..");
const mode = process.argv[2];
assert.ok(mode === "--write" || mode === "--check");
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
const latest = readdirSync(resolve(root, "services/control-plane/migrations"))
  .map((name) => /^(?<version>[0-9]{6})_(?<name>.+)\.sql$/u.exec(name))
  .filter((match) => match?.groups && Number(match.groups.version) >= 15)
  .toSorted((left, right) => left!.groups!.version!.localeCompare(right!.groups!.version!, "en"))
  .at(-1);
assert.ok(latest?.groups);
const current = latest.groups.version!;
const previous = String(Number(current) - 1).padStart(6, "0");
const migrationName = latest.groups.name!;
const base = `services/control-plane/migrations/product/${current}`;
const sqlPath = `services/control-plane/migrations/${latest[0]}`;
const prior = json(`services/control-plane/migrations/product/${previous}/manifest.json`);
const catalog = json(prior.schema_bundle.migrations.at(-1).catalog_contract.path);
catalog.schema_head = current;
const sql = read(sqlPath);
const statements = splitPostgresStatements(sql).map((statement) => ({
  index: statement.index,
  start: statement.start,
  end: statement.end,
  sha256: statement.sha256,
  classification: classifyMigrationStatement(statement, current),
}));
catalog.source_descriptors.push({
  migration_id: current,
  sql_sha256: digest(sql),
  statements,
});
const additions = statements
  .filter(({ classification }) =>
    classification.command === "CREATE" && classification.object_kind === "FUNCTION")
  .map(({ classification }) => migrationObjectIdentity(classification.target_identity));
for (const item of additions) validateObjectIdentity(item);
catalog.declared_object_identities.push(...additions);
const catalogBytes = bytes(catalog);
const catalogArtifact = artifact(`${base}/catalog/schema-${previous}.json`, catalogBytes);
const schema = structuredClone(prior.schema_bundle);
schema.schema_head = current;
schema.migrations.push({
  ...structuredClone(schema.migrations.at(-1)),
  id: current,
  name: migrationName,
  predecessor_id: previous,
  schema_from: previous,
  schema_to: current,
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
process.stdout.write(`product-${current} ${mode}: ${schemaDigest}\n`);
