import { createHash } from "node:crypto";
import { mkdirSync, readFileSync, writeFileSync } from "node:fs";
import { dirname, resolve } from "node:path";

import { canonicalizeJson } from "./lib/platform-json-semantics";

const root = resolve(import.meta.dirname, "..");
const sourcePath =
  "contracts/platform/v1alpha1/fixtures/golden/durable-coordination-registry-source-v2.json";
const routePath =
  "contracts/platform/v1alpha1/fixtures/golden/durable-project-create-route-v2.json";
const profilePath =
  "contracts/platform/v1alpha1/fixtures/golden/durable-coordination-profile-managed-agent-create-project-durable-v1alpha1.json";
const priorOutputPath = "contracts/generated/platform/v1alpha1/durable-coordination-registry.json";
const outputPath = "contracts/generated/platform/v1alpha1/durable-coordination-registry-v2.json";
const priorDigest = "sha256:ca5703cbbc68f7501e6fb4da0a0f09bc9fdd6e52bc48f080627bec64fd1b635a";
const sourceDomain = "cloud-agents/durable-coordination/source/v2";
const profileDomain = "cloud-agents/durable-coordination/profile/v2";
const registryDomain = "cloud-agents/durable-coordination/registry/v2";

type JsonRecord = Record<string, any>;

function readJson(path: string): JsonRecord {
  return JSON.parse(readFileSync(resolve(root, path), "utf8")) as JsonRecord;
}

function digest(domain: string, value: unknown): string {
  const bytes = new TextEncoder().encode(`${domain}\0`);
  const input = new Uint8Array(bytes.length + canonicalizeJson(value).length);
  input.set(bytes);
  input.set(canonicalizeJson(value), bytes.length);
  return `sha256:${createHash("sha256").update(input).digest("hex")}`;
}

function build(): JsonRecord {
  const source = readJson(sourcePath);
  const route = readJson(routePath);
  const profile = readJson(profilePath);
  const prior = readJson(priorOutputPath);
  if (prior.registryDigest !== priorDigest)
    throw new Error("durable coordination v1 output drifted");
  if (source.historicalCompatibility?.priorRegistryDigest !== priorDigest) {
    throw new Error("v2 source does not bind the exact v1 registry digest");
  }
  if (source.routeDescriptor !== routePath.split("/fixtures/")[1]) {
    throw new Error("v2 route descriptor binding drifted");
  }
  if (
    profile.profileId !== "managedAgentCreateProjectDurable/v1alpha1" ||
    profile.operationId !== "managedAgentCreateProjectDurable" ||
    profile.http?.method !== route.method ||
    profile.http?.path !== route.path ||
    profile.http?.idempotencyHeader !== route.idempotencyHeader ||
    route.boundary !== "localdev_loopback_only" ||
    route.externalEffects !== "forbidden"
  ) {
    throw new Error("durable project create route/profile binding drifted");
  }
  if (
    profile.coordination?.createsPlatformOperation !== true ||
    profile.coordination?.externalSideEffect !== "forbidden" ||
    profile.coordination?.outboxEventClass !== "operation_effect" ||
    JSON.stringify(profile.coordination?.requiredFinalizers) !== JSON.stringify(["project-create"])
  ) {
    throw new Error("durable project create profile coordination boundary drifted");
  }
  // The successor profile uses the same immutable state machines and policy
  // catalog.  Keeping these exact digests lets the append-only SQL kernel
  // accept both generations without changing the historical helpers.
  const stateMachineDigest = prior.stateMachineDigest as string;
  const policyDigest = prior.policyDigest as string;
  const body: JsonRecord = {
    formatVersion: "cloud-agents-durable-coordination-registry/v2",
    registryId: source.registryId,
    sourceDigest: digest(sourceDomain, source),
    stateMachineDigest,
    policyDigest,
    historicalCompatibility: source.historicalCompatibility,
    selector: source.selector,
    profiles: [
      {
        profileDigest: digest(profileDomain, {
          registryId: source.registryId,
          stateMachineDigest,
          policyDigest,
          profile,
        }),
        spec: profile,
      },
    ],
    stateMachines: prior.stateMachines,
    policies: prior.policies,
    implementationBoundary: source.implementationBoundary,
  };
  return { ...body, registryDigest: digest(registryDomain, body) };
}

function serialize(value: JsonRecord): string {
  return `${JSON.stringify(value, null, 2)}\n`;
}

const mode = process.argv[2] ?? "--check";
if (mode !== "--write" && mode !== "--check")
  throw new Error(
    "usage: bun scripts/generate-platform-durable-coordination-registry-v2.ts [--write|--check]",
  );
const expected = serialize(build());
const output = resolve(root, outputPath);
if (mode === "--write") {
  mkdirSync(dirname(output), { recursive: true, mode: 0o755 });
  writeFileSync(output, expected, { mode: 0o644 });
  process.stdout.write(`platform-durable-coordination-registry-v2: wrote ${outputPath}\n`);
} else {
  if (readFileSync(output, "utf8") !== expected)
    throw new Error(`${outputPath} is stale; run generator with --write.`);
  process.stdout.write("platform-durable-coordination-registry-v2: current\n");
}
