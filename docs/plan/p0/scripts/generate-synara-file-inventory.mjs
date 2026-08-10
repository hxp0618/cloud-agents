#!/usr/bin/env node

import { createHash } from "node:crypto";
import { lstatSync, readFileSync, readlinkSync, writeFileSync } from "node:fs";
import { basename, dirname, join, relative, sep } from "node:path";
import { spawnSync } from "node:child_process";

const expectedHead = "2c50b1eb54ed3228719bb55cc8bdcd1b0babc8e0";
const sourceRoot =
  process.env.SYNARA_P0_SOURCE ??
  "/Users/huang/devel/project/huang/business/synara-cloud-agent-external-runtime";
const outputRoot = process.env.CLOUD_AGENTS_P0_OUTPUT ?? join(process.cwd(), "docs/plan/p0");

function runGit(...args) {
  const result = spawnSync("git", ["-C", sourceRoot, ...args], {
    encoding: "utf8",
    maxBuffer: 64 * 1024 * 1024,
    env: { ...process.env, LC_ALL: "C" },
  });
  if (result.status !== 0) {
    throw new Error(`git ${args.join(" ")} failed: ${(result.stderr ?? "").trim()}`);
  }
  return result.stdout ?? "";
}

function sha256(value) {
  return createHash("sha256").update(value).digest("hex");
}

function tsv(value) {
  const normalized = value === null || value === undefined || value === "" ? "-" : value;
  return String(normalized).replaceAll("\t", " ").replaceAll("\r", " ").replaceAll("\n", " ");
}

const head = runGit("rev-parse", "HEAD").trim();
if (head !== expectedHead) {
  throw new Error(`P0 source moved: expected ${expectedHead}, observed ${head}`);
}
const dirty = runGit("status", "--porcelain=v1", "--untracked-files=all").trim();
if (dirty) {
  throw new Error(`P0 source is dirty; refusing inventory generation:\n${dirty}`);
}
const snapshotTree = runGit("rev-parse", "HEAD^{tree}").trim();

const scopes = [
  "services/control-plane",
  "deploy",
  "scripts",
  ".github",
  "apps/provider-host",
  "docs/contracts",
  "Dockerfile",
  "package.json",
  "bun.lock",
  "cloud-agent-candidate.lock.json",
  "cloud-agent-candidate.lock.schema.json",
];

const treeOutput = runGit("ls-tree", "-r", "-z", "HEAD", "--", ...scopes);
const entries = treeOutput
  .split("\0")
  .filter(Boolean)
  .map((record) => {
    const [metadata, path] = record.split("\t", 2);
    const [mode, type, oid] = metadata.split(" ");
    return { mode, type, oid, path };
  });

function fileBuffer(path) {
  const absolute = join(sourceRoot, path);
  const stat = lstatSync(absolute);
  return stat.isSymbolicLink() ? Buffer.from(readlinkSync(absolute)) : readFileSync(absolute);
}

function scopeFor(path) {
  if (path.startsWith("services/control-plane/")) return "control-plane";
  if (path.startsWith("deploy/")) return "deploy";
  if (path.startsWith("scripts/")) return "scripts";
  if (path.startsWith(".github/")) return "ci";
  if (path.startsWith("apps/provider-host/")) return "provider-host-compat";
  if (path.startsWith("docs/contracts/")) return "contract-reference";
  return "root-supply-chain";
}

function capabilityFor(path) {
  const rules = [
    [/services\/control-plane\/cmd\/api|internal\/httpapi/, "management-api"],
    [/internal\/(executions|sessions|executiontargets|persistence)/, "execution-authority"],
    [/internal\/agentd|cmd\/agentd/, "worker-agentd-workspace-provider-host"],
    [/cocoontransport|cocoonsupervisor|cmd\/cocoon-/, "cocoon-isolation"],
    [/kmsworker|kmsrotation|runtimesecretrotation|cmd\/kms-worker/, "kms-secret-rotation"],
    [/metadatamigration|cmd\/metadata/, "metadata-migration"],
    [/gvisorattestor|cmd\/gvisor-node-attestor/, "gvisor-attestation"],
    [/internal\/routing|cmd\/routing-authority-sign/, "routing-ingress"],
    [/migrations\/|internal\/database/, "postgres-schema"],
    [/providercatalog|providerCapabilityCatalog/, "provider-catalog-generation"],
    [/cloud-agent-candidate|provider-host|cloud-agent/i, "external-runtime-candidate"],
    [/deploy\/kubernetes/, "kubernetes-deployment"],
    [/deploy\/worker|Dockerfile/, "worker-image-supply-chain"],
    [/deploy\/(personal|saas)/, "product-composition"],
    [/credential|broker|secret/i, "credential-broker"],
    [/workspace|materialization|artifact/i, "workspace-artifact"],
    [/billing|invoice|commercial/i, "billing-commercial"],
    [/privacy|legal|compliance|retention/i, "governance-retention"],
    [/auth|oidc|saml|scim|membership|tenant|project/i, "identity-tenancy"],
  ];
  return rules.find(([pattern]) => pattern.test(path))?.[1] ?? "unclassified";
}

function classificationFor(path, capability) {
  if (/provider[-_]?host[-_]?v1/i.test(path)) return "retire";
  if (capability === "billing-commercial") return "synara-only";
  if (/saml|scim|legalhold|privacyexport|advancedretention/i.test(path)) {
    return "deferred-public-extension";
  }
  if (
    /internal\/(problem|validation|secretguard|databasetime|workertiming|limits|attestation|gitpolicy|fairqueue)/.test(
      path,
    )
  ) {
    return "move";
  }
  if (/deploy\/|kubernetes|cocoon|gvisor|ssh|s3|kms|oidc|otlp|ingress|routing/.test(path)) {
    return "adapter";
  }
  if (path.startsWith("services/control-plane/") || path.startsWith("docs/contracts/")) {
    return "rewrite-public";
  }
  if (path.startsWith("apps/provider-host/") || capability === "external-runtime-candidate") {
    return "adapter";
  }
  if (path.startsWith("scripts/") || path.startsWith(".github/")) return "unclassified";
  return "unclassified";
}

function phaseRelation(path) {
  if (/_test\.go$|\.test\.[cm]?[jt]sx?$/.test(path)) return ["validate", "test-input"];
  if (/\/migrations\/\d{6}_.+\.sql$/.test(path)) return ["embed", "embedded-file"];
  if (/Dockerfile/.test(basename(path))) return ["image-build", "build-context"];
  if (path.startsWith("deploy/")) return ["deploy", "deployment-ref"];
  if (path.startsWith("scripts/") || path.startsWith(".github/")) {
    return ["validate", path.startsWith(".github/") ? "ci-workflow" : "evidence-tool"];
  }
  if (path.endsWith("catalog_gen.go")) return ["generate", "generated-output"];
  if (path.endsWith("generate.go")) return ["generate", "generator"];
  if (/cloud-agent-candidate|bun\.lock|package\.json/.test(path)) {
    return ["image-copy", "external-artifact-input"];
  }
  if (path.endsWith(".go")) return ["compile", "source-file"];
  return ["evidence", "tracked-input"];
}

function generatedState(path) {
  if (path.endsWith("internal/providercatalog/catalog_gen.go")) return "orphaned-source";
  if (/internal\/providercatalog\/(generate\.go|cmd\/generate)/.test(path)) {
    return "missing-source-reference";
  }
  if (/(_gen|\.generated)\./.test(path)) return "manual-review";
  return "not-generated";
}

function riskFor(path, capability, generatedStateValue) {
  const risks = [];
  if (path.startsWith("services/control-plane/internal/agentd/")) risks.push("mixed-package");
  if (/Dockerfile/.test(path)) risks.push("broad-copy");
  if (generatedStateValue.includes("source")) risks.push("missing-source");
  if (capability === "external-runtime-candidate")
    risks.push("cross-language", "external-immutable");
  if (/\/migrations\//.test(path)) risks.push("schema-lineage");
  return risks.join(",");
}

const rows = entries.map((entry) => {
  const buffer = fileBuffer(entry.path);
  const scope = scopeFor(entry.path);
  const capability = capabilityFor(entry.path);
  const classification = classificationFor(entry.path, capability);
  const [phase, relation] = phaseRelation(entry.path);
  const generatedStateValue = generatedState(entry.path);
  const risk = riskFor(entry.path, capability, generatedStateValue);
  const manualReview =
    classification === "unclassified" ||
    risk.includes("mixed-package") ||
    risk.includes("missing-source");
  return {
    snapshotHead: head,
    snapshotTree,
    scope,
    capability,
    classificationSeed: classification,
    manualReview,
    phase,
    relation,
    path: entry.path,
    gitMode: entry.mode,
    gitBlobOid: entry.oid,
    sha256: sha256(buffer),
    bytes: buffer.byteLength,
    goPackage: entry.path.endsWith(".go") ? dirname(entry.path) : "",
    generatedState: generatedStateValue,
    risk,
    notes:
      generatedStateValue === "orphaned-source"
        ? "checked-in output exists but configured source path is absent at frozen ref"
        : "",
  };
});

const headers = Object.keys(rows[0]);
const tsvText = `${headers.join("\t")}\n${rows
  .map((row) => headers.map((header) => tsv(row[header])).join("\t"))
  .join("\n")}\n`;
const tsvPath = join(outputRoot, "synara-file-inventory.tsv");
writeFileSync(tsvPath, tsvText);

function countBy(key) {
  return Object.fromEntries(
    [...new Set(rows.map((row) => row[key]))]
      .sort()
      .map((value) => [value, rows.filter((row) => row[key] === value).length]),
  );
}

const candidateLock = JSON.parse(
  readFileSync(join(sourceRoot, "cloud-agent-candidate.lock.json"), "utf8"),
);
const commandNodes = [
  ...new Set(
    rows
      .map((row) => row.path.match(/^services\/control-plane\/cmd\/([^/]+)\//)?.[1])
      .filter(Boolean),
  ),
].sort();
const deployScopes = [
  ...new Set(rows.map((row) => row.path.match(/^deploy\/([^/]+)\//)?.[1]).filter(Boolean)),
].sort();
const artifacts = [
  {
    id: "standalone-runtime",
    kind: "external-artifact",
    ...candidateLock.standaloneRuntime,
  },
  ...Object.entries(candidateLock.packages).map(([name, value]) => ({
    id: name,
    kind: "external-artifact",
    ...value,
  })),
];

const graph = {
  schemaVersion: 1,
  generatedAt: new Date().toISOString(),
  snapshot: {
    head,
    tree: rows[0].snapshotTree,
    controlPlaneTree: runGit("rev-parse", "HEAD:services/control-plane").trim(),
    deployTree: runGit("rev-parse", "HEAD:deploy").trim(),
    scriptsTree: runGit("rev-parse", "HEAD:scripts").trim(),
    inventoryPath: relative(process.cwd(), tsvPath).split(sep).join("/"),
    inventorySha256: sha256(tsvText),
  },
  counts: {
    files: rows.length,
    byScope: countBy("scope"),
    byCapability: countBy("capability"),
    byClassificationSeed: countBy("classificationSeed"),
    manualReview: rows.filter((row) => row.manualReview).length,
    migrations: rows.filter((row) => row.phase === "embed").length,
    commands: commandNodes.length,
    externalArtifacts: artifacts.length,
  },
  nodes: [
    ...commandNodes.map((id) => ({
      id: `command:${id}`,
      kind: id === "generate" ? "generator" : "go-command",
      path: `services/control-plane/cmd/${id}`,
    })),
    ...deployScopes.map((id) => ({
      id: `deployment:${id}`,
      kind: "deployment-scope",
      path: `deploy/${id}`,
    })),
    ...artifacts,
  ],
  edges: [
    {
      from: "source:synara-control-plane",
      to: "inventory:synara-file-inventory",
      relation: "described-by",
      count: rows.length,
    },
    ...artifacts.map((artifact) => ({
      from: "lock:cloud-agent-candidate",
      to: artifact.id,
      relation: "pins-external-artifact",
      digest: artifact.sha256,
    })),
    {
      from: "command:agentd",
      to: "lock:cloud-agent-candidate",
      relation: "launches-via-provider-host-bundle",
    },
    {
      from: "deployment:worker",
      to: "command:agentd",
      relation: "packages",
    },
  ],
  limitations: [
    "classificationSeed is heuristic and is not a final P0 authority decision",
    "command import closure is recorded separately from this file/build-context inventory",
    "Docker COPY context can include tests and migrations without semantic runtime dependency",
  ],
};
writeFileSync(
  join(outputRoot, "synara-inventory-graph.json"),
  `${JSON.stringify(graph, null, 2)}\n`,
);

process.stdout.write(
  `${JSON.stringify({ tsvPath, tsvSha256: sha256(tsvText), ...graph.counts }, null, 2)}\n`,
);
