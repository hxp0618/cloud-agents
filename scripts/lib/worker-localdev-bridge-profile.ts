import { createHash } from "node:crypto";
import { lstatSync, mkdirSync, readFileSync, writeFileSync } from "node:fs";
import { dirname, resolve } from "node:path";
import { execFileSync } from "node:child_process";
import Ajv2020 from "ajv/dist/2020.js";
import { canonicalizeJson, type JsonRecord } from "./platform-json-semantics";

export const WORKER_LOCALDEV_BRIDGE_SOURCE_PATH =
  "services/worker/localdev-bridge-profile/v1/authority-source.json";
export const WORKER_LOCALDEV_BRIDGE_SOURCE_SCHEMA_PATH =
  "services/worker/localdev-bridge-profile/v1/authority-source.schema.json";
export const WORKER_LOCALDEV_BRIDGE_PROFILE_PATH =
  "services/worker/localdev-bridge-profile/v1/profile.json";
export const WORKER_LOCALDEV_BRIDGE_PROFILE_SCHEMA_PATH =
  "services/worker/localdev-bridge-profile/v1/profile.schema.json";
export const WORKER_LOCALDEV_BRIDGE_GO_PATH =
  "services/worker/localdev_bridge_profile_generated.go";
export const WORKER_LOCALDEV_BRIDGE_AUTHORITY_ID = "D-057-WORKER-LOCALDEV-BRIDGE-000001";
export const WORKER_LOCALDEV_BRIDGE_REVISION = `${WORKER_LOCALDEV_BRIDGE_AUTHORITY_ID}.r1`;
export const WORKER_LOCALDEV_BRIDGE_PROFILE_ID =
  "cloud-agents/worker-supervisor-operation-dispatch/launcher-localdev-v1alpha1";

const SOURCE_SCHEMA =
  "https://schemas.cloud-agents.dev/worker/localdev-bridge/v1/authority-source.schema.json";
const PROFILE_SCHEMA =
  "https://schemas.cloud-agents.dev/worker/localdev-bridge/v1/profile.schema.json";
const SOURCE_FORMAT = "cloud-agents-worker-localdev-bridge-authority/v1";
const PROFILE_FORMAT = "cloud-agents-worker-localdev-bridge-profile/v1";
const SOURCE_DOMAIN = "cloud-agents/worker-localdev-bridge/source/v1";
const PROFILE_DOMAIN = "cloud-agents/worker-localdev-bridge/profile/v1";
const DIGEST_RE = /^sha256:[0-9a-f]{64}$/u;

const PARENT_PROFILES = ["cloud-agents/worker-localdev-launcher/v1alpha1"] as const;
const COMMANDS = ["Negotiate", "CheckHealth", "ExecuteOperation", "GetOperationReceipt"] as const;
const INPUT_PATHS = [
  ".mise.toml",
  "bun.lock",
  "bunfig.toml",
  "go.work",
  "package.json",
  "tsconfig.base.json",
  "contracts/worker/v1alpha1/README.md",
  "contracts/worker/v1alpha1/kernel.proto",
  "contracts/worker/v1alpha1/worker_supervisor.proto",
  "sdk/go/go.mod",
  "sdk/go/go.sum",
  "sdk/go/gen/cloudagents/worker/v1alpha1/kernel.pb.go",
  "sdk/go/gen/cloudagents/worker/v1alpha1/worker_supervisor.pb.go",
  "sdk/go/gen/cloudagents/worker/v1alpha1/workerv1alpha1connect/worker_supervisor.connect.go",
  "sdk/go/gen/common/v1alpha1/identity_generated.go",
  "services/worker/go.mod",
  "services/worker/go.sum",
  "services/worker/localdev-launcher-profile/v1/authority-source.json",
  "services/worker/localdev-launcher-profile/v1/authority-source.schema.json",
  "services/worker/localdev-launcher-profile/v1/profile.json",
  "services/worker/localdev-launcher-profile/v1/profile.schema.json",
  "services/worker/localdev_launcher_profile_generated.go",
  "services/worker/cmd/cloud-agents-worker/README.md",
  "services/worker/doc.go",
  "services/worker/service.go",
  "services/worker/service_test.go",
  "services/worker/operation_admission.go",
  "services/worker/operation_admission_test.go",
  "services/worker/operation_builder.go",
  "services/worker/operation_builder_test.go",
  "services/worker/execution.go",
  "services/worker/execution_test.go",
  "services/worker/local_dispatch_handle.go",
  "services/worker/supervisor/client.go",
  "services/worker/supervisor/client_test.go",
  "services/worker/supervisor/local_dispatch.go",
  "services/worker/supervisor/local_dispatch_test.go",
  "services/worker/supervisor/dispatch_profile_generated.go",
  "services/worker/supervisor/local_launcher.go",
  "services/worker/supervisor/local_launcher_test.go",
  "services/worker/supervisor/remote_dispatch.go",
  "services/worker/cmd/cloud-agents-worker/main.go",
  "services/worker/cmd/cloud-agents-worker/main_test.go",
  "scripts/lib/platform-json-semantics.ts",
  "scripts/lib/worker-localdev-bridge-profile.ts",
  "scripts/lib/worker-localdev-bridge-profile.test.ts",
  "scripts/generate-worker-localdev-bridge-profile.ts",
] as const;
const ORDERED_INPUT_PATHS = [...INPUT_PATHS].sort();
const EXCLUSION_PATHS = [
  ".idea",
  "node_modules",
  "packages/cloud-agent-provider-api",
  "packages/cloud-agent-runtime",
  "services/control-plane/internal/migration",
  "services/control-plane/internal/store/postgres",
  "services/control-plane/migrations",
  "services/worker/cmd/cloud-agents-worker/provider",
  "deploy",
  "helm",
  "release",
  "tmp",
] as const;
const ORDERED_EXCLUSION_PATHS = [...EXCLUSION_PATHS].sort();
const GENERATED_PATHS = [
  WORKER_LOCALDEV_BRIDGE_SOURCE_PATH,
  WORKER_LOCALDEV_BRIDGE_SOURCE_SCHEMA_PATH,
  WORKER_LOCALDEV_BRIDGE_PROFILE_PATH,
  WORKER_LOCALDEV_BRIDGE_PROFILE_SCHEMA_PATH,
  WORKER_LOCALDEV_BRIDGE_GO_PATH,
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
  entrypoint: "GOWORK=off GOFLAGS=-mod=readonly go test -tags localdev ./...",
  launcher: "go run -tags localdev ./cmd/cloud-agents-worker",
  mode: "localdev_only",
  network: "loopback_only",
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
  runtimePath: "process-local://worker-supervisor/localdev-bridge",
  persistence: "no_write",
  state: "PRESENT_EPHEMERAL",
  resultDigestAlgorithm: "sha256:deterministic-protobuf-receipt-result-v1",
  canonicalProjection: "deterministic_protobuf_receipt",
  excludedFields: ["receipt_id", "sequence", "observed_at", "fencing.token_sha256"],
  stableErrorCodes: [
    "unauthenticated",
    "permission_denied",
    "invalid_argument",
    "failed_precondition",
    "deadline_exceeded",
    "canceled",
    "not_found",
    "method_not_allowed",
    "internal",
    "unimplemented",
    "unavailable",
  ],
  evidencePaths: [
    "docs/plan/standalone/worker-localdev-bridge-implementation-20260828.md",
    "docs/plan/standalone/worker-localdev-bridge-independent-review-20260828.md",
  ],
} as const;
const LINEAGE_FENCE = {
  kind: "single-predecessor-append-only",
  predecessorAuthority: "D-056-WORKER-LOCALDEV-LAUNCHER-000001",
  predecessorRevision: "D-056-WORKER-LOCALDEV-LAUNCHER-000001.r1",
  predecessorProfile: "cloud-agents/worker-localdev-launcher/v1alpha1",
  predecessorProfileDigest:
    "sha256:dc83b89cad24104093e86e69d14743ca9bbc1b106113c90ca52bfa9bde04b72e",
  historicalAuthorities: [
    "D-055-MANAGED-AGENT-WORKER-COORDINATION-000001.r1",
    "D-054-WORKER-DISPATCH-000001.r1",
    "D-053-MIG-000014.r2",
    "D-053-EC-2.r3",
  ],
  mutation: "forbidden",
  historicalEvidence: "retain-and-never-rewrite",
  successorRevision: WORKER_LOCALDEV_BRIDGE_REVISION,
} as const;
const REVIEW_RULES = {
  independentReadOnly: true,
  reviewPath: "docs/plan/standalone/worker-localdev-bridge-independent-review-20260828.md",
  verdicts: ["APPROVE", "REQUEST_CHANGES"],
  classification: ["P0", "P1", "P2"],
  candidateMutation: "forbidden",
  p0P1Repair: "one repair and re-review within r1 candidate",
  p2: "record-and-defer",
  gateTransition: "forbidden",
} as const;
const EFFECTS = {
  database: false,
  durableReceipt: false,
  http: false,
  p2: false,
  provider: false,
  runtime: false,
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
  listenAddress: "loopback_only",
  endpointBase: "http://<IPv4|IPv6-loopback>:<1..65535>",
  endpointInput: "fixed_loopback_host_valid_port_no_userinfo_query_fragment_proxy_redirect",
  tokenSource: "parent_ephemeral_file_o_excl_mode_0600_regular_non_symlink_bounded_bytes_read_only",
  healthMetadata: "exact_before_negotiate_and_check_health",
  parentAuthority: "D-056-WORKER-LOCALDEV-LAUNCHER-000001",
  parentRevision: "D-056-WORKER-LOCALDEV-LAUNCHER-000001.r1",
  parentProfile: "cloud-agents/worker-localdev-launcher/v1alpha1",
  parentProfileDigest: "sha256:8ecdba81fd4a57eef127afae05e1fd26670201c0db4ecb92b22023a495394b0c",
  dispatch: "process_local_ephemeral",
  receipt: "process_local_ephemeral",
  workerIdentitySPIFFE: "spiffe://cloud-agents.local/worker/localdev",
  supervisorIdentitySPIFFE: "spiffe://cloud-agents.local/supervisor/localdev",
  leaseID: "worker-localdev-lease-000001",
  generation: 1,
  routes: [
    "/healthz",
    "/cloudagents.worker.v1alpha1.WorkerExecutionService/Negotiate",
    "/cloudagents.worker.v1alpha1.WorkerExecutionService/CheckHealth",
    "/cloudagents.worker.v1alpha1.WorkerExecutionService/ExecuteOperation",
    "/cloudagents.worker.v1alpha1.WorkerExecutionService/GetOperationReceipt",
  ],
} as const;
const STATE_MACHINE = {
  initial: "starting",
  states: ["starting", "serving", "stopping", "stopped"],
  transitions: [
    { from: "starting", event: "listen", to: "serving" },
    { from: "serving", event: "signal", to: "stopping" },
    { from: "stopping", event: "closed", to: "stopped" },
  ],
} as const;
const BOUNDARY = {
  transport: "local_loopback_http_connect_only",
  auth: "bearer_to_context_identity",
  identity: "fixed_generated_spiffe_pair",
  health: "read_only_process_local",
  completeLedger: "no_op",
  entryWriter: "not_implemented",
  recoveryWriter: "not_implemented",
  dispatchOperation: "process_local_ephemeral",
  getOperationReceipt: "process_local_ephemeral",
  databaseWrites: "forbidden",
  durablePersistence: "forbidden",
  provider: "forbidden",
  runtime: "forbidden",
  productionHTTP: "forbidden",
  publicHTTP: "forbidden",
  p2: "forbidden",
  deployment: "forbidden",
  publication: "forbidden",
  gateTransition: "forbidden",
} as const;

type Digest = `sha256:${string}`;
export type WorkerLocalDevBridgeContract = JsonRecord & {
  readonly sourceDigest?: Digest;
  readonly profileDigest?: Digest;
  readonly inputManifestDigest?: Digest;
};
const DEFAULT_ROOT = resolve(import.meta.dirname, "../..");
function digest(domain: string, value: JsonRecord): Digest {
  return `sha256:${createHash("sha256").update(domain).update("\0").update(canonicalizeJson(value)).digest("hex")}`;
}
function inputManifestDigest(root: string): Digest {
  const h = createHash("sha256");
  for (const path of ORDERED_INPUT_PATHS) {
    const p = resolve(root, path);
    const before = lstatSync(p);
    if (!before.isFile() || before.isSymbolicLink())
      throw new Error(`input path must be a regular file: ${path}`);
    const b = readFileSync(p);
    const reread = readFileSync(p);
    if (!b.equals(reread)) throw new Error(`input path content changed while hashing: ${path}`);
    const after = lstatSync(p);
    if (
      !after.isFile() ||
      after.isSymbolicLink() ||
      after.size !== before.size ||
      after.mode !== before.mode ||
      after.ino !== before.ino ||
      after.dev !== before.dev
    )
      throw new Error(`input path changed while hashing: ${path}`);
    const mode = (before.mode & 0o111) === 0 ? "100644" : "100755";
    h.update(path)
      .update("\0")
      .update(mode)
      .update("\0")
      .update(String(b.byteLength))
      .update("\0")
      .update(createHash("sha256").update(b).digest("hex"))
      .update("\0");
  }
  return `sha256:${h.digest("hex")}`;
}
function base(root: string): JsonRecord {
  return {
    authorityId: WORKER_LOCALDEV_BRIDGE_AUTHORITY_ID,
    revision: WORKER_LOCALDEV_BRIDGE_REVISION,
    decision: WORKER_LOCALDEV_BRIDGE_REVISION,
    profileId: WORKER_LOCALDEV_BRIDGE_PROFILE_ID,
    mode: "localdev_only",
    transport: "loopback_http_connect",
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
    externalSideEffects: { ...EFFECTS },
    implementationBoundary: { ...BOUNDARY },
  };
}
export function buildWorkerLocalDevBridgeSource(
  root: string = DEFAULT_ROOT,
): WorkerLocalDevBridgeContract {
  const b = { $schema: SOURCE_SCHEMA, formatVersion: SOURCE_FORMAT, ...base(root) } as JsonRecord;
  return { ...b, sourceDigest: digest(SOURCE_DOMAIN, b) };
}
export function buildWorkerLocalDevBridgeProfile(
  root: string = DEFAULT_ROOT,
): WorkerLocalDevBridgeContract {
  const s = buildWorkerLocalDevBridgeSource(root);
  const b = {
    $schema: PROFILE_SCHEMA,
    formatVersion: PROFILE_FORMAT,
    ...base(root),
    sourceAuthority: {
      authorityId: s.authorityId,
      revision: s.revision,
      sourceDigest: s.sourceDigest,
    },
  } as JsonRecord;
  return { ...b, profileDigest: digest(PROFILE_DOMAIN, b) };
}
function strictValue(v: unknown): JsonRecord {
  if (Array.isArray(v)) return { type: "array", const: v };
  if (v && typeof v === "object") {
    const entries = Object.entries(v as JsonRecord);
    return {
      type: "object",
      additionalProperties: false,
      required: entries.map(([k]) => k),
      properties: Object.fromEntries(entries.map(([k, x]) => [k, strictValue(x)])),
    };
  }
  return { const: v };
}
function schema(kind: "source" | "profile", root: string): JsonRecord {
  const value =
    kind === "source"
      ? buildWorkerLocalDevBridgeSource(root)
      : buildWorkerLocalDevBridgeProfile(root);
  return {
    $schema: "https://json-schema.org/draft/2020-12/schema",
    $id: kind === "source" ? SOURCE_SCHEMA : PROFILE_SCHEMA,
    title: `${WORKER_LOCALDEV_BRIDGE_AUTHORITY_ID} ${kind}`,
    type: "object",
    additionalProperties: false,
    required: Object.keys(value),
    properties: Object.fromEntries(Object.entries(value).map(([k, v]) => [k, strictValue(v)])),
  };
}
export const buildWorkerLocalDevBridgeSourceSchema = (root: string = DEFAULT_ROOT) =>
  schema("source", root);
export const buildWorkerLocalDevBridgeProfileSchema = (root: string = DEFAULT_ROOT) =>
  schema("profile", root);
const serialize = (v: JsonRecord) => `${JSON.stringify(v, null, 2)}\n`;
export function serializeWorkerLocalDevBridgeGo(p: WorkerLocalDevBridgeContract): string {
  const c = p as any;
  const lines = [
    `// Code generated by scripts/generate-worker-localdev-bridge-profile.ts; DO NOT EDIT.`,
    ``,
    `package worker`,
    ``,
    `const (`,
    `\tWorkerLocalDevBridgeProfileID = ${JSON.stringify(c.profileId)}`,
    `\tWorkerLocalDevBridgeAuthorityID = ${JSON.stringify(c.authorityId)}`,
    `\tWorkerLocalDevBridgeRevision = ${JSON.stringify(c.revision)}`,
    `\tWorkerLocalDevBridgeProfileDigest = ${JSON.stringify(c.profileDigest)}`,
    `\tWorkerLocalDevBridgeSourceDigest = ${JSON.stringify(c.sourceAuthority.sourceDigest)}`,
    `\tWorkerLocalDevBridgeInputManifestDigest = ${JSON.stringify(c.inputManifestDigest)}`,
    `\tWorkerLocalDevBridgeHTTPRoutePrefix = ${JSON.stringify("/cloudagents.worker.v1alpha1.WorkerExecutionService/")}`,
    `\tWorkerLocalDevBridgeHealthRoute = ${JSON.stringify("/healthz")}`,
    `\tWorkerLocalDevBridgeWorkerIdentitySPIFFE = ${JSON.stringify(SELECTOR.workerIdentitySPIFFE)}`,
    `\tWorkerLocalDevBridgeSupervisorIdentitySPIFFE = ${JSON.stringify(SELECTOR.supervisorIdentitySPIFFE)}`,
    `\tWorkerLocalDevBridgeLeaseID = ${JSON.stringify(SELECTOR.leaseID)}`,
    `\tWorkerLocalDevBridgeGeneration = 1`,
    `)`,
    ``,
    `type WorkerLocalDevBridgeProfile struct { ID, AuthorityID, Revision, ProfileDigest, SourceDigest, InputManifestDigest string; Mode, Transport string; RoutePrefix, HealthRoute, WorkerIdentitySPIFFE, SupervisorIdentitySPIFFE, LeaseID string; Generation uint64; ExternalSideEffects bool; CompleteLedger, EntryWriter, RecoveryWriter, DatabaseWrites, DurablePersistence, Provider, Runtime, ProductionHTTP, PublicHTTP, P2, Deployment, Publication, GateTransition string }`,
    ``,
    `var GeneratedWorkerLocalDevBridgeProfile = WorkerLocalDevBridgeProfile{ID: WorkerLocalDevBridgeProfileID, AuthorityID: WorkerLocalDevBridgeAuthorityID, Revision: WorkerLocalDevBridgeRevision, ProfileDigest: WorkerLocalDevBridgeProfileDigest, SourceDigest: WorkerLocalDevBridgeSourceDigest, InputManifestDigest: WorkerLocalDevBridgeInputManifestDigest, Mode: "localdev_only", Transport: "loopback_http_connect", RoutePrefix: WorkerLocalDevBridgeHTTPRoutePrefix, HealthRoute: WorkerLocalDevBridgeHealthRoute, WorkerIdentitySPIFFE: WorkerLocalDevBridgeWorkerIdentitySPIFFE, SupervisorIdentitySPIFFE: WorkerLocalDevBridgeSupervisorIdentitySPIFFE, LeaseID: WorkerLocalDevBridgeLeaseID, Generation: WorkerLocalDevBridgeGeneration, ExternalSideEffects: false, CompleteLedger: "no_op", EntryWriter: "not_implemented", RecoveryWriter: "not_implemented", DatabaseWrites: "forbidden", DurablePersistence: "forbidden", Provider: "forbidden", Runtime: "forbidden", ProductionHTTP: "forbidden", PublicHTTP: "forbidden", P2: "forbidden", Deployment: "forbidden", Publication: "forbidden", GateTransition: "forbidden"}`,
    ``,
    `func (p WorkerLocalDevBridgeProfile) Valid() bool {`,
    `\treturn p.ID == WorkerLocalDevBridgeProfileID && p.AuthorityID == WorkerLocalDevBridgeAuthorityID && p.Revision == WorkerLocalDevBridgeRevision && p.ProfileDigest == WorkerLocalDevBridgeProfileDigest && p.SourceDigest == WorkerLocalDevBridgeSourceDigest && p.InputManifestDigest == WorkerLocalDevBridgeInputManifestDigest && p.Mode == "localdev_only" && p.Transport == "loopback_http_connect" && p.RoutePrefix == WorkerLocalDevBridgeHTTPRoutePrefix && p.HealthRoute == WorkerLocalDevBridgeHealthRoute && p.WorkerIdentitySPIFFE == WorkerLocalDevBridgeWorkerIdentitySPIFFE && p.SupervisorIdentitySPIFFE == WorkerLocalDevBridgeSupervisorIdentitySPIFFE && p.LeaseID == WorkerLocalDevBridgeLeaseID && p.Generation == WorkerLocalDevBridgeGeneration && !p.ExternalSideEffects && p.CompleteLedger == "no_op" && p.EntryWriter == "not_implemented" && p.RecoveryWriter == "not_implemented" && p.DatabaseWrites == "forbidden" && p.DurablePersistence == "forbidden" && p.Provider == "forbidden" && p.Runtime == "forbidden" && p.ProductionHTTP == "forbidden" && p.PublicHTTP == "forbidden" && p.P2 == "forbidden" && p.Deployment == "forbidden" && p.Publication == "forbidden" && p.GateTransition == "forbidden"`,
    `}`,
    ``,
    `func WorkerLocalDevBridgeProfileValid() bool { return GeneratedWorkerLocalDevBridgeProfile.Valid() }`,
    ``,
    `func WorkerLocalDevBridgeProfileAuthority() WorkerLocalDevBridgeProfile { return GeneratedWorkerLocalDevBridgeProfile }`,
    ``,
  ].join("\n");
  return execFileSync("gofmt", { input: lines, encoding: "utf8" });
}
function parse(root: string, path: string) {
  return JSON.parse(readFileSync(resolve(root, path), "utf8")) as JsonRecord;
}
function assertExact(root: string, path: string, expected: string) {
  const output = resolve(root, path);
  const stat = lstatSync(output);
  if (!stat.isFile() || stat.isSymbolicLink())
    throw new Error(`${path} must be a regular file and not a symlink`);
  if (readFileSync(output, "utf8") !== expected)
    throw new Error(`${path} is stale; run generator --write.`);
}
function assertDeclared(root: string): void {
  const seen = new Set<string>();
  for (const p of ORDERED_INPUT_PATHS) {
    if (seen.has(p)) throw new Error(`duplicate input path: ${p}`);
    seen.add(p);
    const s = lstatSync(resolve(root, p));
    if (!s.isFile() || s.isSymbolicLink())
      throw new Error(`input path must be a regular file: ${p}`);
  }
  for (const p of GENERATED_PATHS)
    if (seen.has(p)) throw new Error(`generated path is also input: ${p}`);
  if (new Set(ORDERED_EXCLUSION_PATHS).size !== ORDERED_EXCLUSION_PATHS.length)
    throw new Error("duplicate exclusion path");
  const sets = [
    ORDERED_INPUT_PATHS,
    ORDERED_EXCLUSION_PATHS,
    GENERATED_PATHS,
  ] as readonly (readonly string[])[];
  for (let i = 0; i < sets.length; i++)
    for (let j = i; j < sets.length; j++)
      for (const a of sets[i])
        for (const b of sets[j])
          if (!(i === j && a === b) && (a === b || a.startsWith(`${b}/`) || b.startsWith(`${a}/`)))
            throw new Error(`declared path sets overlap: ${a} / ${b}`);
}
export function assertWorkerLocalDevBridgeCurrent(root: string = DEFAULT_ROOT): void {
  assertDeclared(root);
  const s = buildWorkerLocalDevBridgeSource(root),
    p = buildWorkerLocalDevBridgeProfile(root);
  const ss = buildWorkerLocalDevBridgeSourceSchema(root),
    ps = buildWorkerLocalDevBridgeProfileSchema(root);
  const ajv = new Ajv2020({ strict: true, allErrors: true, validateFormats: false });
  if (!ajv.compile(ss)(parse(root, WORKER_LOCALDEV_BRIDGE_SOURCE_PATH)))
    throw new Error("source schema validation failed");
  if (!ajv.compile(ps)(parse(root, WORKER_LOCALDEV_BRIDGE_PROFILE_PATH)))
    throw new Error("profile schema validation failed");
  assertExact(root, WORKER_LOCALDEV_BRIDGE_SOURCE_PATH, serialize(s));
  assertExact(root, WORKER_LOCALDEV_BRIDGE_PROFILE_PATH, serialize(p));
  assertExact(root, WORKER_LOCALDEV_BRIDGE_SOURCE_SCHEMA_PATH, serialize(ss));
  assertExact(root, WORKER_LOCALDEV_BRIDGE_PROFILE_SCHEMA_PATH, serialize(ps));
  assertExact(root, WORKER_LOCALDEV_BRIDGE_GO_PATH, serializeWorkerLocalDevBridgeGo(p));
}
export function writeWorkerLocalDevBridgeFiles(root: string = DEFAULT_ROOT): void {
  assertDeclared(root);
  const s = buildWorkerLocalDevBridgeSource(root),
    p = buildWorkerLocalDevBridgeProfile(root),
    ss = buildWorkerLocalDevBridgeSourceSchema(root),
    ps = buildWorkerLocalDevBridgeProfileSchema(root);
  const files: [string, string][] = [
    [WORKER_LOCALDEV_BRIDGE_SOURCE_PATH, serialize(s)],
    [WORKER_LOCALDEV_BRIDGE_PROFILE_PATH, serialize(p)],
    [WORKER_LOCALDEV_BRIDGE_SOURCE_SCHEMA_PATH, serialize(ss)],
    [WORKER_LOCALDEV_BRIDGE_PROFILE_SCHEMA_PATH, serialize(ps)],
    [WORKER_LOCALDEV_BRIDGE_GO_PATH, serializeWorkerLocalDevBridgeGo(p)],
  ];
  for (const [path, data] of files) {
    const out = resolve(root, path);
    mkdirSync(dirname(out), { recursive: true });
    try {
      const st = lstatSync(out);
      if (!st.isFile() || st.isSymbolicLink())
        throw new Error(`${path} must not be a symlink or directory`);
    } catch (e) {
      if ((e as NodeJS.ErrnoException).code !== "ENOENT") throw e;
    }
    writeFileSync(out, data, { mode: 0o644 });
  }
}
export const workerLocalDevBridgeSourceDigest = (root: string = DEFAULT_ROOT) =>
  buildWorkerLocalDevBridgeSource(root).sourceDigest as Digest;
export const workerLocalDevBridgeProfileDigest = (root: string = DEFAULT_ROOT) =>
  buildWorkerLocalDevBridgeProfile(root).profileDigest as Digest;
export const isWorkerLocalDevBridgeDigest = (v: unknown): v is Digest =>
  typeof v === "string" && DIGEST_RE.test(v);
