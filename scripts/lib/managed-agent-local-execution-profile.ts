import { execFileSync } from "node:child_process";
import { createHash } from "node:crypto";
import { lstatSync, readFileSync, writeFileSync, mkdirSync } from "node:fs";
import { dirname, resolve } from "node:path";

import Ajv2020 from "ajv/dist/2020.js";

import { canonicalizeJson, type JsonRecord } from "./platform-json-semantics";

/**
 * D-055 is the generated authority for the first transport-neutral bridge
 * between the Managed Agent lifecycle kernel and the already-approved
 * localdev Supervisor -> Worker dispatch seam.  It is intentionally a
 * closed, local-only profile: no filesystem archive, database, HTTP, provider,
 * or production runner is selected by this generator.
 */
export const MANAGED_AGENT_LOCAL_EXECUTION_SOURCE_PATH =
  "services/control-plane/internal/managedagent/local-execution-profile/v1/authority-source.json";
export const MANAGED_AGENT_LOCAL_EXECUTION_SOURCE_SCHEMA_PATH =
  "services/control-plane/internal/managedagent/local-execution-profile/v1/authority-source.schema.json";
export const MANAGED_AGENT_LOCAL_EXECUTION_PROFILE_PATH =
  "services/control-plane/internal/managedagent/local-execution-profile/v1/profile.json";
export const MANAGED_AGENT_LOCAL_EXECUTION_PROFILE_SCHEMA_PATH =
  "services/control-plane/internal/managedagent/local-execution-profile/v1/profile.schema.json";
export const MANAGED_AGENT_LOCAL_EXECUTION_GO_PATH =
  "services/control-plane/internal/managedagent/local_execution_profile_generated.go";

export const MANAGED_AGENT_LOCAL_EXECUTION_AUTHORITY_ID =
  "D-055-MANAGED-AGENT-WORKER-COORDINATION-000001";
export const MANAGED_AGENT_LOCAL_EXECUTION_REVISION =
  "D-055-MANAGED-AGENT-WORKER-COORDINATION-000001.r1";
export const MANAGED_AGENT_LOCAL_EXECUTION_PROFILE_ID =
  "cloud-agents/managed-agent-worker-local-execution/localdev-v1alpha1";

const SOURCE_SCHEMA_URI =
  "https://schemas.cloud-agents.dev/managed-agent/local-execution/v1/authority-source.schema.json";
const PROFILE_SCHEMA_URI =
  "https://schemas.cloud-agents.dev/managed-agent/local-execution/v1/profile.schema.json";
const SOURCE_FORMAT = "cloud-agents-managed-agent-worker-coordination-authority/v1";
const PROFILE_FORMAT = "cloud-agents-managed-agent-worker-coordination-profile/v1";
const SOURCE_DOMAIN = "cloud-agents/managed-agent-worker-coordination/source/v1";
const PROFILE_DOMAIN = "cloud-agents/managed-agent-worker-coordination/profile/v1";
const DIGEST_RE = /^sha256:[0-9a-f]{64}$/u;

const PARENT_PROFILES = [
  "cloud-agents/managed-agent-lifecycle/v1alpha1",
  "cloud-agents/managed-agent-events/v1alpha1",
  "cloud-agents/worker-supervisor-operation-dispatch/localdev-v1alpha1",
  "cloud-agents/worker-operation-admission/v1alpha1",
  "cloud-agents/worker-operation-execution/localdev-v1alpha1",
] as const;

const COMMANDS = ["Probe", "ValidateBinding"] as const;

// This is the complete ordered source set for the coordinator contract.  The
// set is deliberately explicit; a generator may not discover extra files or
// silently include an untracked/symlinked path.
const INPUT_PATHS = [
  ".mise.toml",
  "bun.lock",
  "bunfig.toml",
  "contracts/worker/v1alpha1/README.md",
  "contracts/worker/v1alpha1/kernel.proto",
  "contracts/worker/v1alpha1/worker_supervisor.proto",
  "package.json",
  "sdk/go/gen/cloudagents/worker/v1alpha1/kernel.pb.go",
  "sdk/go/gen/cloudagents/worker/v1alpha1/worker_supervisor.pb.go",
  "sdk/go/gen/cloudagents/worker/v1alpha1/workerv1alpha1connect/worker_supervisor.connect.go",
  "sdk/go/gen/common/v1alpha1/identity_generated.go",
  "sdk/go/go.mod",
  "sdk/go/go.sum",
  "services/worker/go.mod",
  "services/worker/go.sum",
  "services/worker/operation_admission.go",
  "services/worker/operation_admission_test.go",
  "services/worker/operation_builder.go",
  "services/worker/execution.go",
  "services/worker/execution_test.go",
  "services/worker/service.go",
  "services/worker/service_test.go",
  "services/worker/local_dispatch_handle.go",
  "services/worker/supervisor/client.go",
  "services/worker/supervisor/local_dispatch.go",
  "services/worker/supervisor/local_dispatch_test.go",
  "services/worker/supervisor/dispatch_profile_generated.go",
  "services/worker/supervisor/dispatch-profile/v1/authority-source.json",
  "services/worker/supervisor/dispatch-profile/v1/profile.json",
  "services/control-plane/go.mod",
  "services/control-plane/go.sum",
  "services/control-plane/internal/managedagent/lifecycle.go",
  "services/control-plane/internal/managedagent/events.go",
  "services/control-plane/internal/managedagent/profile.go",
  "services/control-plane/internal/managedagent/local_execution.go",
  "services/control-plane/internal/managedagent/local_execution_test.go",
  "scripts/generate-managed-agent-local-execution-profile.ts",
  "scripts/lib/managed-agent-local-execution-profile.ts",
  "scripts/lib/managed-agent-local-execution-profile.test.ts",
  "scripts/lib/platform-json-semantics.ts",
  "tsconfig.base.json",
  "go.work",
] as const;
const ORDERED_INPUT_PATHS = [...INPUT_PATHS].sort();

// These are exact excluded roots/paths, not a wildcard search.  Recursive
// roots are interpreted by the review as a closed deny set; files outside the
// input set are also rejected as undeclared.
const EXCLUSION_PATHS = [
  ".idea",
  "contracts/managed-agent/v1alpha1/openapi.json",
  "contracts/platform/v1alpha1",
  "packages/cloud-agent-provider-api",
  "services/control-plane/internal/server",
  "services/control-plane/internal/store/postgres",
  "services/control-plane/internal/migration",
  "services/control-plane/migrations",
  "services/worker/cmd",
  "tools/g-contract-external-consumer",
  "deploy",
  "helm",
  "release",
] as const;
const ORDERED_EXCLUSION_PATHS = [...EXCLUSION_PATHS].sort();

const GENERATED_PATHS = [
  MANAGED_AGENT_LOCAL_EXECUTION_SOURCE_PATH,
  MANAGED_AGENT_LOCAL_EXECUTION_SOURCE_SCHEMA_PATH,
  MANAGED_AGENT_LOCAL_EXECUTION_PROFILE_PATH,
  MANAGED_AGENT_LOCAL_EXECUTION_PROFILE_SCHEMA_PATH,
  MANAGED_AGENT_LOCAL_EXECUTION_GO_PATH,
] as const;

const ARCHIVE = {
  algorithm: "deterministic-ustar-v1",
  emission: "forbidden",
  compression: "none",
  ordering: "utf8-bytewise-sorted-path",
  metadata: "mode=100644,uid=0,gid=0,mtime=0",
  duplicatePolicy: "reject",
  symlinkPolicy: "reject",
} as const;

const MEMBER_MANIFEST = {
  algorithm: "utf8-bytewise-sorted-path-mode-size-sha256-nul-v1",
  emission: "forbidden",
  path: "<not-written>",
  fields: "path\\0mode\\0size\\0sha256\\0",
  regularFileOnly: true,
  duplicatePolicy: "reject",
} as const;

const RUNNER = {
  entrypoint: "GOWORK=off GOFLAGS=-mod=readonly go test",
  mode: "localdev_only",
  network: "deny",
  database: "deny",
  provider: "deny",
  goVersion: "1.26.0",
  toolchain: "go1.26.6",
  node: "24.18.1",
  bun: "1.3.14",
  platforms: ["darwin-arm64", "linux-amd64"],
  timeout: "focused-tests-only",
} as const;

const RECEIPT = {
  runtimePath: "process-local://worker-service/receipts",
  persistence: "no_write",
  state: "ABSENT_PENDING",
  resultDigestAlgorithm: "sha256:deterministic-protobuf-receipt-result-v1",
  canonicalProjection:
    "proto3-deterministic-marshal-after-clearing-receipt_id-sequence-observed_at-fencing.token_sha256",
  excludedFields: ["receipt_id", "sequence", "observed_at", "fencing.token_sha256"],
  stableErrorCodes: [
    "execution_failed",
    "worker_dispatch_failed",
    "deadline_exceeded",
    "fenced",
    "cancelled",
    "worker_failed",
  ],
  evidencePaths: [
    "docs/plan/standalone/managed-agent-worker-local-execution-implementation-20260828.md",
    "docs/plan/standalone/managed-agent-worker-local-execution-independent-review-20260828.md",
  ],
} as const;

const LINEAGE_FENCE = {
  kind: "single-predecessor-append-only",
  predecessorAuthority: "D-054-WORKER-DISPATCH-000001.r1",
  predecessorRevision: "D-054-WORKER-DISPATCH-000001.r1",
  predecessorProfile: "cloud-agents/worker-supervisor-operation-dispatch/localdev-v1alpha1",
  predecessorProfileDigest:
    "sha256:4ed83884e50cf2f55e9799a16afe28c97cf5756969ae47cdc082a1987b5ddbc1",
  historicalAuthorities: ["D-053-MIG-000014.r2", "D-053-EC-2.r3"],
  mutation: "forbidden",
  historicalEvidence: "retain-and-never-rewrite",
  successorRevision: MANAGED_AGENT_LOCAL_EXECUTION_REVISION,
} as const;

const REVIEW_RULES = {
  independentReadOnly: true,
  reviewPath:
    "docs/plan/standalone/managed-agent-worker-local-execution-independent-review-20260828.md",
  verdicts: ["APPROVE", "REQUEST_CHANGES"],
  classification: ["P0", "P1", "P2"],
  candidateMutation: "forbidden",
  p0P1Repair: "one repair and re-review within r1 candidate",
  p2: "record-and-defer",
  gateTransition: "forbidden",
} as const;

const EXTERNAL_SIDE_EFFECTS = {
  database: false,
  durableReceipt: false,
  http: false,
  p2: false,
  provider: false,
  workspace: false,
  artifact: false,
  credential: false,
  deployment: false,
  publication: false,
  gate: false,
} as const;

const SELECTOR = {
  profileSelection: "exact_generated_profile_id_and_digest",
  callerSelectedProfile: "forbidden",
  foreignPath: "forbidden",
  foreignTransport: "forbidden",
  genericSupervisor: "forbidden",
  commands: [...COMMANDS],
  binding: "BindLocalDispatch_before_database_or_external_setup",
} as const;

const STATE_MACHINE = {
  initial: "queued",
  states: ["queued", "running", "succeeded", "failed", "cancelled"],
  transitions: [
    { from: "queued", event: "start", to: "running" },
    { from: "running", event: "complete", to: "succeeded" },
    { from: "running", event: "fail", to: "failed" },
    { from: "running", event: "cancel", to: "cancelled" },
  ],
  outcomeMapping: {
    SUCCEEDED: "execution.complete",
    FAILED: "execution.fail",
    CANCELLED: "turn.cancel",
    DEADLINE_EXCEEDED: "execution.fail/deadline_exceeded",
    FENCED: "execution.fail/fenced",
  },
} as const;

const IMPLEMENTATION_BOUNDARY = {
  lifecycle: "managedagent.Store_in_memory_only",
  dispatch: "worker.Supervisor.NewLocal_and_BindLocalDispatch_only",
  receipt: "detached_bounded_process_local_only",
  completeLedger: "not_applicable_no_writer",
  entryWriter: "not_implemented",
  recoveryWriter: "not_implemented",
  databaseWrites: "forbidden",
  durablePersistence: "forbidden",
  http: "forbidden",
  provider: "forbidden",
  p2: "forbidden",
  workspace: "forbidden",
  artifact: "forbidden",
  credential: "forbidden",
  productionRunner: "forbidden",
  deployment: "forbidden",
  publication: "forbidden",
  gateTransition: "forbidden",
} as const;

const LIMITS = {
  maxDeadlineSeconds: 300,
  maxInputBytes: 1 << 20,
  maxIdentifierBytes: 128,
  maxReceiptSummaryBytes: 512,
  maxAttemptNumber: 1,
} as const;

const SCOPE_PROJECTION = "sha256-length-prefixed-tenant-project-v1" as const;

type Digest = `sha256:${string}`;
export type ManagedAgentLocalExecutionContract = JsonRecord & {
  readonly $schema: string;
  readonly formatVersion: string;
  readonly authorityId: string;
  readonly revision: string;
  readonly decision: string;
  readonly profileId: string;
  readonly inputManifestDigest?: Digest;
  readonly sourceDigest?: Digest;
  readonly profileDigest?: Digest;
};

function digest(domain: string, value: JsonRecord): Digest {
  return `sha256:${createHash("sha256")
    .update(domain)
    .update("\0")
    .update(canonicalizeJson(value))
    .digest("hex")}`;
}

const DEFAULT_ROOT = resolve(import.meta.dirname, "../..");

function inputManifestDigest(root: string): Digest {
  const hash = createHash("sha256");
  for (const path of ORDERED_INPUT_PATHS) {
    const target = resolve(root, path);
    const before = lstatSync(target);
    if (!before.isFile() || before.isSymbolicLink()) {
      throw new Error(`input path must be a regular file: ${path}`);
    }
    const bytes = readFileSync(target);
    const after = lstatSync(target);
    if (
      !after.isFile() ||
      after.isSymbolicLink() ||
      after.size !== before.size ||
      after.mode !== before.mode ||
      after.ino !== before.ino ||
      after.dev !== before.dev
    ) {
      throw new Error(`input path changed while hashing: ${path}`);
    }
    const mode = (before.mode & 0o111) === 0 ? "100644" : "100755";
    const rawDigest = createHash("sha256").update(bytes).digest("hex");
    hash
      .update(path)
      .update("\0")
      .update(mode)
      .update("\0")
      .update(String(bytes.byteLength))
      .update("\0")
      .update(rawDigest)
      .update("\0");
  }
  return `sha256:${hash.digest("hex")}`;
}

function sourceBody(root: string): JsonRecord {
  return {
    $schema: SOURCE_SCHEMA_URI,
    formatVersion: SOURCE_FORMAT,
    authorityId: MANAGED_AGENT_LOCAL_EXECUTION_AUTHORITY_ID,
    revision: MANAGED_AGENT_LOCAL_EXECUTION_REVISION,
    decision: MANAGED_AGENT_LOCAL_EXECUTION_REVISION,
    profileId: MANAGED_AGENT_LOCAL_EXECUTION_PROFILE_ID,
    mode: "localdev_only",
    transport: "in_process",
    scopeProjection: SCOPE_PROJECTION,
    parentProfiles: [...PARENT_PROFILES],
    commands: [...COMMANDS],
    inputPaths: [...ORDERED_INPUT_PATHS],
    exclusionPaths: [...ORDERED_EXCLUSION_PATHS],
    generatedPaths: [...GENERATED_PATHS],
    inputSetAlgorithm: "utf8-bytewise-sorted-path-regular-file-mode-size-sha256-nul-v1",
    inputManifestDigest: inputManifestDigest(root),
    archive: { ...ARCHIVE },
    memberManifest: { ...MEMBER_MANIFEST },
    runner: { ...RUNNER },
    receipt: { ...RECEIPT },
    lineageFence: { ...LINEAGE_FENCE },
    reviewRules: { ...REVIEW_RULES },
    selector: { ...SELECTOR },
    stateMachine: { ...STATE_MACHINE },
    limits: { ...LIMITS },
    externalSideEffects: { ...EXTERNAL_SIDE_EFFECTS },
    implementationBoundary: { ...IMPLEMENTATION_BOUNDARY },
    sourceDigest: "sha256:" + "0".repeat(64),
  };
}

export function buildManagedAgentLocalExecutionSource(
  root: string = DEFAULT_ROOT,
): ManagedAgentLocalExecutionContract {
  const body = sourceBody(root);
  const withoutPlaceholder = { ...body, sourceDigest: undefined } as JsonRecord;
  delete withoutPlaceholder.sourceDigest;
  return { ...body, sourceDigest: digest(SOURCE_DOMAIN, withoutPlaceholder) };
}

export function buildManagedAgentLocalExecutionProfile(
  root: string = DEFAULT_ROOT,
): ManagedAgentLocalExecutionContract {
  const source = buildManagedAgentLocalExecutionSource(root);
  const body: JsonRecord = {
    $schema: PROFILE_SCHEMA_URI,
    formatVersion: PROFILE_FORMAT,
    authorityId: MANAGED_AGENT_LOCAL_EXECUTION_AUTHORITY_ID,
    revision: MANAGED_AGENT_LOCAL_EXECUTION_REVISION,
    decision: MANAGED_AGENT_LOCAL_EXECUTION_REVISION,
    profileId: MANAGED_AGENT_LOCAL_EXECUTION_PROFILE_ID,
    mode: "localdev_only",
    transport: "in_process",
    scopeProjection: SCOPE_PROJECTION,
    parentProfiles: [...PARENT_PROFILES],
    commands: [...COMMANDS],
    inputPaths: [...ORDERED_INPUT_PATHS],
    exclusionPaths: [...ORDERED_EXCLUSION_PATHS],
    generatedPaths: [...GENERATED_PATHS],
    inputSetAlgorithm: "utf8-bytewise-sorted-path-regular-file-mode-size-sha256-nul-v1",
    inputManifestDigest: source.inputManifestDigest,
    archive: { ...ARCHIVE },
    memberManifest: { ...MEMBER_MANIFEST },
    runner: { ...RUNNER },
    receipt: { ...RECEIPT },
    lineageFence: { ...LINEAGE_FENCE },
    reviewRules: { ...REVIEW_RULES },
    selector: { ...SELECTOR },
    stateMachine: { ...STATE_MACHINE },
    limits: { ...LIMITS },
    externalSideEffects: { ...EXTERNAL_SIDE_EFFECTS },
    implementationBoundary: { ...IMPLEMENTATION_BOUNDARY },
    sourceAuthority: {
      authorityId: source.authorityId,
      revision: source.revision,
      sourceDigest: source.sourceDigest,
    },
  };
  return { ...body, profileDigest: digest(PROFILE_DOMAIN, body) };
}

function closedConstObject(properties: JsonRecord): JsonRecord {
  return {
    type: "object",
    additionalProperties: false,
    required: Object.keys(properties),
    properties,
  };
}

function schemaFor(kind: "source" | "profile", root: string): JsonRecord {
  const source = buildManagedAgentLocalExecutionSource(root);
  const profile = buildManagedAgentLocalExecutionProfile(root);
  const isSource = kind === "source";
  const schemaURI = isSource ? SOURCE_SCHEMA_URI : PROFILE_SCHEMA_URI;
  const format = isSource ? SOURCE_FORMAT : PROFILE_FORMAT;
  const required = [
    "$schema",
    "formatVersion",
    "authorityId",
    "revision",
    "decision",
    "profileId",
    "mode",
    "transport",
    "scopeProjection",
    "parentProfiles",
    "commands",
    "inputPaths",
    "exclusionPaths",
    "generatedPaths",
    "inputSetAlgorithm",
    "inputManifestDigest",
    "archive",
    "memberManifest",
    "runner",
    "receipt",
    "lineageFence",
    "reviewRules",
    "selector",
    "stateMachine",
    "limits",
    "externalSideEffects",
    "implementationBoundary",
    ...(isSource ? ["sourceDigest"] : ["sourceAuthority", "profileDigest"]),
  ];
  const exactArray = (values: readonly unknown[]) => ({
    type: "array",
    const: [...values],
    items: { type: "string" },
  });
  const exactMap = (value: JsonRecord) =>
    closedConstObject(
      Object.fromEntries(Object.entries(value).map(([key, item]) => [key, { const: item }])),
    );
  const schema: JsonRecord = {
    $schema: "https://json-schema.org/draft/2020-12/schema",
    $id: schemaURI,
    title: `${MANAGED_AGENT_LOCAL_EXECUTION_AUTHORITY_ID} ${kind}`,
    type: "object",
    additionalProperties: false,
    required,
    properties: {
      $schema: { const: schemaURI },
      formatVersion: { const: format },
      authorityId: { const: MANAGED_AGENT_LOCAL_EXECUTION_AUTHORITY_ID },
      revision: { const: MANAGED_AGENT_LOCAL_EXECUTION_REVISION },
      decision: { const: MANAGED_AGENT_LOCAL_EXECUTION_REVISION },
      profileId: { const: MANAGED_AGENT_LOCAL_EXECUTION_PROFILE_ID },
      mode: { const: "localdev_only" },
      transport: { const: "in_process" },
      scopeProjection: { const: SCOPE_PROJECTION },
      parentProfiles: exactArray(PARENT_PROFILES),
      commands: exactArray(COMMANDS),
      inputPaths: exactArray(ORDERED_INPUT_PATHS),
      exclusionPaths: exactArray(ORDERED_EXCLUSION_PATHS),
      generatedPaths: exactArray(GENERATED_PATHS),
      inputSetAlgorithm: {
        const: "utf8-bytewise-sorted-path-regular-file-mode-size-sha256-nul-v1",
      },
      inputManifestDigest: { const: source.inputManifestDigest },
      archive: exactMap(ARCHIVE),
      memberManifest: exactMap(MEMBER_MANIFEST),
      runner: {
        type: "object",
        additionalProperties: false,
        required: Object.keys(RUNNER),
        properties: {
          entrypoint: { const: RUNNER.entrypoint },
          mode: { const: RUNNER.mode },
          network: { const: RUNNER.network },
          database: { const: RUNNER.database },
          provider: { const: RUNNER.provider },
          goVersion: { const: RUNNER.goVersion },
          toolchain: { const: RUNNER.toolchain },
          node: { const: RUNNER.node },
          bun: { const: RUNNER.bun },
          platforms: exactArray(RUNNER.platforms),
          timeout: { const: RUNNER.timeout },
        },
      },
      receipt: {
        type: "object",
        additionalProperties: false,
        required: Object.keys(RECEIPT),
        properties: {
          runtimePath: { const: RECEIPT.runtimePath },
          persistence: { const: RECEIPT.persistence },
          state: { const: RECEIPT.state },
          resultDigestAlgorithm: { const: RECEIPT.resultDigestAlgorithm },
          canonicalProjection: { const: RECEIPT.canonicalProjection },
          excludedFields: exactArray(RECEIPT.excludedFields),
          stableErrorCodes: exactArray(RECEIPT.stableErrorCodes),
          evidencePaths: exactArray(RECEIPT.evidencePaths),
        },
      },
      lineageFence: {
        type: "object",
        additionalProperties: false,
        required: Object.keys(LINEAGE_FENCE),
        properties: {
          kind: { const: LINEAGE_FENCE.kind },
          predecessorAuthority: { const: LINEAGE_FENCE.predecessorAuthority },
          predecessorRevision: { const: LINEAGE_FENCE.predecessorRevision },
          predecessorProfile: { const: LINEAGE_FENCE.predecessorProfile },
          predecessorProfileDigest: { const: LINEAGE_FENCE.predecessorProfileDigest },
          historicalAuthorities: exactArray(LINEAGE_FENCE.historicalAuthorities),
          mutation: { const: LINEAGE_FENCE.mutation },
          historicalEvidence: { const: LINEAGE_FENCE.historicalEvidence },
          successorRevision: { const: LINEAGE_FENCE.successorRevision },
        },
      },
      reviewRules: {
        type: "object",
        additionalProperties: false,
        required: Object.keys(REVIEW_RULES),
        properties: {
          independentReadOnly: { const: true },
          reviewPath: { const: REVIEW_RULES.reviewPath },
          verdicts: exactArray(REVIEW_RULES.verdicts),
          classification: exactArray(REVIEW_RULES.classification),
          candidateMutation: { const: REVIEW_RULES.candidateMutation },
          p0P1Repair: { const: REVIEW_RULES.p0P1Repair },
          p2: { const: REVIEW_RULES.p2 },
          gateTransition: { const: REVIEW_RULES.gateTransition },
        },
      },
      selector: {
        type: "object",
        additionalProperties: false,
        required: Object.keys(SELECTOR),
        properties: {
          profileSelection: { const: SELECTOR.profileSelection },
          callerSelectedProfile: { const: SELECTOR.callerSelectedProfile },
          foreignPath: { const: SELECTOR.foreignPath },
          foreignTransport: { const: SELECTOR.foreignTransport },
          genericSupervisor: { const: SELECTOR.genericSupervisor },
          commands: exactArray(COMMANDS),
          binding: { const: SELECTOR.binding },
        },
      },
      stateMachine: {
        type: "object",
        additionalProperties: false,
        required: Object.keys(STATE_MACHINE),
        properties: {
          initial: { const: STATE_MACHINE.initial },
          states: exactArray(STATE_MACHINE.states),
          transitions: {
            type: "array",
            const: STATE_MACHINE.transitions,
            items: {
              type: "object",
              additionalProperties: false,
              required: ["from", "event", "to"],
              properties: {
                from: { type: "string" },
                event: { type: "string" },
                to: { type: "string" },
              },
            },
          },
          outcomeMapping: closedConstObject(
            Object.fromEntries(
              Object.entries(STATE_MACHINE.outcomeMapping).map(([key, value]) => [
                key,
                { const: value },
              ]),
            ),
          ),
        },
      },
      limits: exactMap(LIMITS),
      externalSideEffects: exactMap(EXTERNAL_SIDE_EFFECTS),
      implementationBoundary: exactMap(IMPLEMENTATION_BOUNDARY),
      sourceDigest: isSource ? { const: source.sourceDigest } : undefined,
      sourceAuthority: isSource
        ? undefined
        : {
            type: "object",
            additionalProperties: false,
            required: ["authorityId", "revision", "sourceDigest"],
            properties: {
              authorityId: { const: MANAGED_AGENT_LOCAL_EXECUTION_AUTHORITY_ID },
              revision: { const: MANAGED_AGENT_LOCAL_EXECUTION_REVISION },
              sourceDigest: { const: source.sourceDigest },
            },
          },
      profileDigest: isSource ? undefined : { const: profile.profileDigest },
    },
  };
  // Undefined schema properties are an implementation convenience above, not
  // an open schema member.
  for (const key of Object.keys(schema.properties as JsonRecord)) {
    if ((schema.properties as JsonRecord)[key] === undefined)
      delete (schema.properties as JsonRecord)[key];
  }
  return schema;
}

export function buildManagedAgentLocalExecutionSourceSchema(
  root: string = DEFAULT_ROOT,
): JsonRecord {
  return schemaFor("source", root);
}

export function buildManagedAgentLocalExecutionProfileSchema(
  root: string = DEFAULT_ROOT,
): JsonRecord {
  return schemaFor("profile", root);
}

export function serializeManagedAgentLocalExecution(value: JsonRecord): string {
  return `${JSON.stringify(value, null, 2)}\n`;
}

function parse(path: string, root: string): JsonRecord {
  const stat = lstatSync(resolve(root, path));
  if (!stat.isFile()) throw new Error(`${path} must be a regular file`);
  return JSON.parse(readFileSync(resolve(root, path), "utf8")) as JsonRecord;
}

function validateSchema(value: JsonRecord, schema: JsonRecord, name: string): void {
  const ajv = new Ajv2020({ strict: true, allErrors: true, validateFormats: false });
  const validate = ajv.compile(schema);
  if (!validate(value)) {
    throw new Error(`${name} schema validation failed: ${JSON.stringify(validate.errors)}`);
  }
}

function assertExact(root: string, path: string, expected: string): void {
  const actual = readFileSync(resolve(root, path), "utf8");
  if (actual !== expected) throw new Error(`${path} is stale; run generator --write.`);
}

export function assertManagedAgentLocalExecutionCurrent(root: string): void {
  assertDeclaredPathSet(root);
  const source = buildManagedAgentLocalExecutionSource(root);
  const profile = buildManagedAgentLocalExecutionProfile(root);
  const sourceSchema = buildManagedAgentLocalExecutionSourceSchema(root);
  const profileSchema = buildManagedAgentLocalExecutionProfileSchema(root);
  const actualSource = parse(MANAGED_AGENT_LOCAL_EXECUTION_SOURCE_PATH, root);
  const actualProfile = parse(MANAGED_AGENT_LOCAL_EXECUTION_PROFILE_PATH, root);
  const actualSourceSchema = parse(MANAGED_AGENT_LOCAL_EXECUTION_SOURCE_SCHEMA_PATH, root);
  const actualProfileSchema = parse(MANAGED_AGENT_LOCAL_EXECUTION_PROFILE_SCHEMA_PATH, root);
  validateSchema(actualSource, actualSourceSchema, "authority-source");
  validateSchema(actualProfile, actualProfileSchema, "profile");
  validateSchema(source, sourceSchema, "generated authority-source");
  validateSchema(profile, profileSchema, "generated profile");
  assertExact(
    root,
    MANAGED_AGENT_LOCAL_EXECUTION_SOURCE_PATH,
    serializeManagedAgentLocalExecution(source),
  );
  assertExact(
    root,
    MANAGED_AGENT_LOCAL_EXECUTION_PROFILE_PATH,
    serializeManagedAgentLocalExecution(profile),
  );
  assertExact(
    root,
    MANAGED_AGENT_LOCAL_EXECUTION_SOURCE_SCHEMA_PATH,
    serializeManagedAgentLocalExecution(sourceSchema),
  );
  assertExact(
    root,
    MANAGED_AGENT_LOCAL_EXECUTION_PROFILE_SCHEMA_PATH,
    serializeManagedAgentLocalExecution(profileSchema),
  );
  assertExact(
    root,
    MANAGED_AGENT_LOCAL_EXECUTION_GO_PATH,
    serializeManagedAgentLocalExecutionGo(profile),
  );
}

function assertDeclaredPathSet(root: string): void {
  const seen = new Set<string>();
  for (const path of ORDERED_INPUT_PATHS) {
    if (seen.has(path)) throw new Error(`duplicate input path: ${path}`);
    seen.add(path);
    const stat = lstatSync(resolve(root, path));
    if (!stat.isFile()) throw new Error(`input path must be a regular file: ${path}`);
  }
  const generated = new Set(GENERATED_PATHS);
  for (const path of ORDERED_INPUT_PATHS) {
    if (generated.has(path)) throw new Error(`generated path is also an input: ${path}`);
  }
  if (new Set(ORDERED_EXCLUSION_PATHS).size !== ORDERED_EXCLUSION_PATHS.length) {
    throw new Error("duplicate exclusion path");
  }
  const declaredSets: ReadonlyArray<ReadonlyArray<string>> = [
    ORDERED_INPUT_PATHS,
    ORDERED_EXCLUSION_PATHS,
    GENERATED_PATHS,
  ];
  for (let left = 0; left < declaredSets.length; left++) {
    for (let right = left; right < declaredSets.length; right++) {
      for (const first of declaredSets[left]) {
        for (const second of declaredSets[right]) {
          if (left === right && first === second) continue;
          if (
            first === second ||
            first.startsWith(`${second}/`) ||
            second.startsWith(`${first}/`)
          ) {
            throw new Error(`declared path sets overlap: ${first} / ${second}`);
          }
        }
      }
    }
  }
}

function goString(value: unknown): string {
  return JSON.stringify(value);
}

export function serializeManagedAgentLocalExecutionGo(
  profile: ManagedAgentLocalExecutionContract,
): string {
  const lines = [
    "// Code generated by scripts/generate-managed-agent-local-execution-profile.ts; DO NOT EDIT.",
    "",
    "package managedagent",
    "",
    "const (",
    `\tManagedAgentLocalExecutionAuthorityID = ${goString(profile.authorityId)}`,
    `\tManagedAgentLocalExecutionRevision = ${goString(profile.revision)}`,
    `\tManagedAgentLocalExecutionDecision = ${goString(profile.decision)}`,
    `\tManagedAgentLocalExecutionProfileID = ${goString(profile.profileId)}`,
    `\tManagedAgentLocalExecutionProfileDigest = ${goString(profile.profileDigest)}`,
    `\tManagedAgentLocalExecutionSourceDigest = ${goString((profile.sourceAuthority as JsonRecord).sourceDigest)}`,
    `\tManagedAgentLocalExecutionInputManifestDigest = ${goString(profile.inputManifestDigest)}`,
    `\tManagedAgentLocalExecutionScopeProjection = ${goString(SCOPE_PROJECTION)}`,
    `\tManagedAgentLocalExecutionResultDigestAlgorithm = ${goString(RECEIPT.resultDigestAlgorithm)}`,
    `\tManagedAgentLocalExecutionReceiptMode = ${goString(RECEIPT.persistence)}`,
    ")",
    "",
    "type ManagedAgentLocalExecutionProfile struct {",
    "\tID string",
    "\tAuthorityID string",
    "\tRevision string",
    "\tDecision string",
    "\tProfileDigest string",
    "\tSourceDigest string",
    "\tInputManifestDigest string",
    "\tMode string",
    "\tTransport string",
    "\tScopeProjection string",
    "\tResultDigestAlgorithm string",
    "\tReceiptMode string",
    "\tParentProfiles [5]string",
    "\tCommands [2]string",
    "\tMaxDeadlineSeconds uint32",
    "}",
    "",
    "func (p ManagedAgentLocalExecutionProfile) Valid() bool {",
    `\treturn p.ID == ManagedAgentLocalExecutionProfileID && p.AuthorityID == ManagedAgentLocalExecutionAuthorityID && p.Revision == ManagedAgentLocalExecutionRevision && p.Decision == ManagedAgentLocalExecutionDecision && p.ProfileDigest == ManagedAgentLocalExecutionProfileDigest && p.SourceDigest == ManagedAgentLocalExecutionSourceDigest && p.InputManifestDigest == ManagedAgentLocalExecutionInputManifestDigest && p.Mode == "localdev_only" && p.Transport == "in_process" && p.ScopeProjection == ManagedAgentLocalExecutionScopeProjection && p.ResultDigestAlgorithm == ManagedAgentLocalExecutionResultDigestAlgorithm && p.ReceiptMode == ManagedAgentLocalExecutionReceiptMode && p.ParentProfiles == [5]string{${PARENT_PROFILES.map(goString).join(", ")}} && p.Commands == [2]string{${COMMANDS.map(goString).join(", ")}} && p.MaxDeadlineSeconds == 300`,
    "}",
    "",
    "var GeneratedManagedAgentLocalExecutionProfile = ManagedAgentLocalExecutionProfile{",
    `\tID: ManagedAgentLocalExecutionProfileID, AuthorityID: ManagedAgentLocalExecutionAuthorityID, Revision: ManagedAgentLocalExecutionRevision, Decision: ManagedAgentLocalExecutionDecision, ProfileDigest: ManagedAgentLocalExecutionProfileDigest, SourceDigest: ManagedAgentLocalExecutionSourceDigest, InputManifestDigest: ManagedAgentLocalExecutionInputManifestDigest, Mode: "localdev_only", Transport: "in_process", ScopeProjection: ManagedAgentLocalExecutionScopeProjection, ResultDigestAlgorithm: ManagedAgentLocalExecutionResultDigestAlgorithm, ReceiptMode: ManagedAgentLocalExecutionReceiptMode, ParentProfiles: [5]string{${PARENT_PROFILES.map(goString).join(", ")}}, Commands: [2]string{${COMMANDS.map(goString).join(", ")}}, MaxDeadlineSeconds: 300,`,
    "}",
    "",
    "func ManagedAgentLocalExecutionProfileAuthority() ManagedAgentLocalExecutionProfile { return GeneratedManagedAgentLocalExecutionProfile }",
    "",
  ].join("\n");
  return execFileSync("gofmt", { input: lines, encoding: "utf8" });
}

export function writeManagedAgentLocalExecutionFiles(root: string): void {
  assertDeclaredPathSet(root);
  const source = buildManagedAgentLocalExecutionSource(root);
  const profile = buildManagedAgentLocalExecutionProfile(root);
  const files = new Map<string, string>([
    [MANAGED_AGENT_LOCAL_EXECUTION_SOURCE_PATH, serializeManagedAgentLocalExecution(source)],
    [MANAGED_AGENT_LOCAL_EXECUTION_PROFILE_PATH, serializeManagedAgentLocalExecution(profile)],
    [
      MANAGED_AGENT_LOCAL_EXECUTION_SOURCE_SCHEMA_PATH,
      serializeManagedAgentLocalExecution(buildManagedAgentLocalExecutionSourceSchema(root)),
    ],
    [
      MANAGED_AGENT_LOCAL_EXECUTION_PROFILE_SCHEMA_PATH,
      serializeManagedAgentLocalExecution(buildManagedAgentLocalExecutionProfileSchema(root)),
    ],
    [MANAGED_AGENT_LOCAL_EXECUTION_GO_PATH, serializeManagedAgentLocalExecutionGo(profile)],
  ]);
  for (const [path, contents] of files) {
    const output = resolve(root, path);
    mkdirSync(dirname(output), { recursive: true, mode: 0o755 });
    try {
      if (!lstatSync(output).isFile())
        throw new Error(`${path} must not be a symlink or directory`);
    } catch (error) {
      if ((error as NodeJS.ErrnoException).code !== "ENOENT") throw error;
    }
    writeFileSync(output, contents, { mode: 0o644 });
  }
}

export function managedAgentLocalExecutionSourceDigest(root: string = DEFAULT_ROOT): Digest {
  return buildManagedAgentLocalExecutionSource(root).sourceDigest as Digest;
}

export function managedAgentLocalExecutionProfileDigest(root: string = DEFAULT_ROOT): Digest {
  return buildManagedAgentLocalExecutionProfile(root).profileDigest as Digest;
}

export function isManagedAgentLocalExecutionDigest(value: unknown): value is Digest {
  return typeof value === "string" && DIGEST_RE.test(value);
}
