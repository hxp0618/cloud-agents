import { execFileSync } from "node:child_process";
import { createHash } from "node:crypto";
import { lstatSync, readFileSync, writeFileSync, mkdirSync } from "node:fs";
import { dirname, resolve } from "node:path";

import Ajv2020 from "ajv/dist/2020.js";

import { canonicalizeJson, type JsonRecord } from "./platform-json-semantics";

/**
 * Versioned authority for the process-local Supervisor -> Worker dispatch
 * seam.  This module is intentionally self contained: it does not discover
 * profiles from the filesystem and it never opens a transport or database.
 */
export const LOCAL_DISPATCH_SOURCE_PATH =
  "services/worker/supervisor/dispatch-profile/v1/authority-source.json";
export const LOCAL_DISPATCH_SOURCE_SCHEMA_PATH =
  "services/worker/supervisor/dispatch-profile/v1/authority-source.schema.json";
export const LOCAL_DISPATCH_PROFILE_PATH =
  "services/worker/supervisor/dispatch-profile/v1/profile.json";
export const LOCAL_DISPATCH_PROFILE_SCHEMA_PATH =
  "services/worker/supervisor/dispatch-profile/v1/profile.schema.json";
export const LOCAL_DISPATCH_GO_PATH = "services/worker/supervisor/dispatch_profile_generated.go";

export const LOCAL_DISPATCH_AUTHORITY_ID = "D-054-WORKER-DISPATCH-000001";
export const LOCAL_DISPATCH_REVISION = "D-054-WORKER-DISPATCH-000001.r1";
export const LOCAL_DISPATCH_PROFILE_ID =
  "cloud-agents/worker-supervisor-operation-dispatch/localdev-v1alpha1";

const SOURCE_SCHEMA_URI =
  "https://schemas.cloud-agents.dev/worker/supervisor/dispatch-profile/v1/authority-source.schema.json";
const PROFILE_SCHEMA_URI =
  "https://schemas.cloud-agents.dev/worker/supervisor/dispatch-profile/v1/profile.schema.json";
const SOURCE_FORMAT = "cloud-agents-worker-supervisor-local-dispatch-authority/v1";
const PROFILE_FORMAT = "cloud-agents-worker-supervisor-local-dispatch-profile/v1";
const PROFILE_DOMAIN = "cloud-agents/worker-supervisor-operation-dispatch/profile/v1";
const SOURCE_DOMAIN = "cloud-agents/worker-supervisor-operation-dispatch/source/v1";
const DIGEST_RE = /^sha256:[0-9a-f]{64}$/u;

const LIMITS = {
  protocolMajor: 1,
  protocolMinor: 0,
  maxWireMessageBytes: 1 << 20,
  maxRepeatedItems: 64,
  maxStringBytes: 1024,
  maxPayloadBytes: 64 << 10,
  maxDeadlineSeconds: 300,
  maxIdentifierBytes: 256,
  maxFencingTokenBytes: 64 << 10,
  maxAdmissionRecords: 1024,
  maxReceiptRecords: 1024,
} as const;

const CAPABILITIES = ["negotiation", "health", "operation_dispatch"] as const;
const COMMANDS = ["Probe", "ValidateBinding"] as const;

type Digest = `sha256:${string}`;
export type LocalDispatchContract = JsonRecord & {
  readonly $schema: string;
  readonly formatVersion: string;
  readonly authorityId: string;
  readonly revision: string;
  readonly decision: string;
  readonly profileId: string;
  readonly mode: string;
  readonly transport: string;
  readonly genericClientDispatch: string;
  readonly parentProfiles: readonly string[];
  readonly capabilities: readonly string[];
  readonly commands: readonly string[];
  readonly limits: JsonRecord;
  readonly externalSideEffects: JsonRecord;
  readonly selector: JsonRecord;
  readonly predecessor: JsonRecord;
  readonly lineageFence: JsonRecord;
  readonly reviewRules: JsonRecord;
  readonly implementationBoundary: JsonRecord;
  readonly sourceDigest?: Digest;
  readonly profileDigest?: Digest;
};

const PREDECESSOR = {
  authorityId: "D-053-MIG-000014",
  revision: "D-053-MIG-000014.r2",
  profileId: "cloud-agents-platform-migration-runner-binding/v1",
  profileLogicalDigest: "sha256:7ffe830d854e5037994e2b5019da792a42d97928da456639bcdbfc4c3fa05489",
  mutation: "forbidden",
  historicalEvidence: "retain-and-never-rewrite",
} as const;

const SELECTOR = {
  mode: "in_process",
  profileSelection: "exact_profile_id_and_digest",
  callerSelectedProfile: "forbidden",
  genericClientDispatch: "forbidden",
  foreignTransport: "forbidden",
  networkListener: "forbidden",
} as const;

const EXTERNAL_SIDE_EFFECTS = {
  database: false,
  durableReceipt: false,
  http: false,
  p2: false,
  provider: false,
  workspace: false,
  credential: false,
  artifact: false,
  deployment: false,
  publication: false,
} as const;

const LINEAGE_FENCE = {
  kind: "single-predecessor-append-only",
  predecessorRevision: PREDECESSOR.revision,
  successorRevision: LOCAL_DISPATCH_REVISION,
  historicalEvidence: "retain-and-never-rewrite",
  d053Objects: "immutable-byte-and-digest-references-only",
} as const;

const REVIEW_RULES = {
  independentReadOnly: true,
  reviewPath:
    "docs/plan/standalone/worker-supervisor-operation-dispatch-localdev-independent-review-20260828.md",
  verdicts: ["APPROVE", "REQUEST_CHANGES"],
  classification: ["P0", "P1", "P2"],
  candidateMutation: "forbidden",
  gateTransition: "forbidden",
  p0P1Repair: "one repair and re-review within r1 candidate",
  p2: "record-and-defer",
} as const;

const IMPLEMENTATION_BOUNDARY = {
  dispatch: "in_process_only",
  receipt: "detached_bounded_process_local_only",
  databaseWrites: "not_authorized",
  durablePersistence: "forbidden",
  http: "forbidden",
  p2: "forbidden",
  provider: "forbidden",
  workspace: "forbidden",
  credential: "forbidden",
  artifact: "forbidden",
  productionRunner: "forbidden",
  deployment: "forbidden",
  publication: "forbidden",
  gateTransition: "forbidden",
} as const;

const BASE: Omit<LocalDispatchContract, "sourceDigest" | "profileDigest" | "$schema"> = {
  formatVersion: PROFILE_FORMAT,
  authorityId: LOCAL_DISPATCH_AUTHORITY_ID,
  revision: LOCAL_DISPATCH_REVISION,
  decision: LOCAL_DISPATCH_REVISION,
  profileId: LOCAL_DISPATCH_PROFILE_ID,
  mode: "localdev_only",
  transport: "in_process",
  genericClientDispatch: "forbidden",
  parentProfiles: [
    "cloud-agents/worker-operation-admission/v1alpha1",
    "cloud-agents/worker-operation-execution/localdev-v1alpha1",
  ],
  capabilities: [...CAPABILITIES],
  commands: [...COMMANDS],
  limits: { ...LIMITS },
  externalSideEffects: { ...EXTERNAL_SIDE_EFFECTS },
  selector: { ...SELECTOR },
  predecessor: { ...PREDECESSOR },
  lineageFence: { ...LINEAGE_FENCE },
  reviewRules: { ...REVIEW_RULES },
  implementationBoundary: { ...IMPLEMENTATION_BOUNDARY },
};

function digest(domain: string, value: JsonRecord): Digest {
  return `sha256:${createHash("sha256")
    .update(domain)
    .update("\0")
    .update(canonicalizeJson(value))
    .digest("hex")}`;
}

function sourceBody(): LocalDispatchContract {
  return {
    $schema: SOURCE_SCHEMA_URI,
    ...BASE,
    formatVersion: SOURCE_FORMAT,
  };
}

function profileBody(sourceDigest: Digest): LocalDispatchContract {
  return {
    $schema: PROFILE_SCHEMA_URI,
    ...BASE,
    sourceAuthority: {
      authorityId: LOCAL_DISPATCH_AUTHORITY_ID,
      revision: LOCAL_DISPATCH_REVISION,
      sourceDigest,
    },
  } as LocalDispatchContract;
}

function schemaFor(kind: "source" | "profile", digestValue?: Digest): JsonRecord {
  const isSource = kind === "source";
  const schemaUri = isSource ? SOURCE_SCHEMA_URI : PROFILE_SCHEMA_URI;
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
    "genericClientDispatch",
    "parentProfiles",
    "capabilities",
    "commands",
    "limits",
    "externalSideEffects",
    "selector",
    "predecessor",
    "lineageFence",
    "reviewRules",
    "implementationBoundary",
    ...(isSource ? ["sourceDigest"] : ["sourceAuthority", "profileDigest"]),
  ];
  const digestSchema = { type: "string", pattern: "^sha256:[0-9a-f]{64}$" };
  const closedStringMap = (properties: JsonRecord): JsonRecord => ({
    type: "object",
    additionalProperties: false,
    required: Object.keys(properties),
    properties,
  });
  const boolProperties = Object.fromEntries(
    Object.keys(EXTERNAL_SIDE_EFFECTS).map((key) => [key, { const: false }]),
  );
  const limitsProperties = Object.fromEntries(
    Object.entries(LIMITS).map(([key, value]) => [key, { const: value }]),
  );
  const sourceAuthority = {
    type: "object",
    additionalProperties: false,
    required: ["authorityId", "revision", "sourceDigest"],
    properties: {
      authorityId: { const: LOCAL_DISPATCH_AUTHORITY_ID },
      revision: { const: LOCAL_DISPATCH_REVISION },
      sourceDigest: { const: buildWorkerSupervisorLocalDispatchSource().sourceDigest },
    },
  };
  return {
    $schema: "https://json-schema.org/draft/2020-12/schema",
    $id: schemaUri,
    title: `${LOCAL_DISPATCH_AUTHORITY_ID} ${kind}`,
    type: "object",
    additionalProperties: false,
    required,
    properties: {
      $schema: { const: schemaUri },
      formatVersion: { const: format },
      authorityId: { const: LOCAL_DISPATCH_AUTHORITY_ID },
      revision: { const: LOCAL_DISPATCH_REVISION },
      decision: { const: LOCAL_DISPATCH_REVISION },
      profileId: { const: LOCAL_DISPATCH_PROFILE_ID },
      mode: { const: "localdev_only" },
      transport: { const: "in_process" },
      genericClientDispatch: { const: "forbidden" },
      parentProfiles: {
        type: "array",
        const: [...BASE.parentProfiles],
        items: { type: "string" },
      },
      capabilities: { type: "array", const: [...CAPABILITIES], items: { type: "string" } },
      commands: { type: "array", const: [...COMMANDS], items: { type: "string" } },
      limits: closedStringMap(limitsProperties),
      externalSideEffects: closedStringMap(boolProperties),
      selector: closedStringMap(
        Object.fromEntries(Object.entries(SELECTOR).map(([key, value]) => [key, { const: value }])),
      ),
      predecessor: closedStringMap(
        Object.fromEntries(
          Object.entries(PREDECESSOR).map(([key, value]) => [key, { const: value }]),
        ),
      ),
      lineageFence: closedStringMap(
        Object.fromEntries(
          Object.entries(LINEAGE_FENCE).map(([key, value]) => [key, { const: value }]),
        ),
      ),
      reviewRules: {
        type: "object",
        additionalProperties: false,
        required: Object.keys(REVIEW_RULES),
        properties: {
          independentReadOnly: { const: true },
          reviewPath: {
            const:
              "docs/plan/standalone/worker-supervisor-operation-dispatch-localdev-independent-review-20260828.md",
          },
          verdicts: {
            type: "array",
            const: ["APPROVE", "REQUEST_CHANGES"],
            items: { type: "string" },
          },
          classification: { type: "array", const: ["P0", "P1", "P2"], items: { type: "string" } },
          candidateMutation: { const: "forbidden" },
          gateTransition: { const: "forbidden" },
          p0P1Repair: { const: "one repair and re-review within r1 candidate" },
          p2: { const: "record-and-defer" },
        },
      },
      implementationBoundary: closedStringMap(
        Object.fromEntries(
          Object.entries(IMPLEMENTATION_BOUNDARY).map(([key, value]) => [key, { const: value }]),
        ),
      ),
      sourceDigest: { const: buildWorkerSupervisorLocalDispatchSource().sourceDigest },
      sourceAuthority,
      profileDigest: digestValue ? { const: digestValue } : digestSchema,
    },
  };
}

export function buildWorkerSupervisorLocalDispatchSource(_root?: string): LocalDispatchContract {
  const body = sourceBody();
  return { ...body, sourceDigest: digest(SOURCE_DOMAIN, body) } as LocalDispatchContract;
}

export function buildWorkerSupervisorLocalDispatchProfile(_root?: string): LocalDispatchContract {
  const source = buildWorkerSupervisorLocalDispatchSource();
  const body = profileBody(source.sourceDigest as Digest);
  return { ...body, profileDigest: digest(PROFILE_DOMAIN, body) } as LocalDispatchContract;
}

export function buildWorkerSupervisorLocalDispatchSourceSchema(_root?: string): JsonRecord {
  return schemaFor("source");
}

export function buildWorkerSupervisorLocalDispatchProfileSchema(_root?: string): JsonRecord {
  const profile = buildWorkerSupervisorLocalDispatchProfile();
  return schemaFor("profile", profile.profileDigest as Digest);
}

export function serializeLocalDispatch(value: JsonRecord): string {
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
  if (!validate(value))
    throw new Error(`${name} schema validation failed: ${JSON.stringify(validate.errors)}`);
}

function assertExact(root: string, path: string, expected: string): void {
  const actual = readFileSync(resolve(root, path), "utf8");
  if (actual !== expected) throw new Error(`${path} is stale; run generator --write.`);
}

export function assertWorkerSupervisorLocalDispatchCurrent(root: string): void {
  const source = buildWorkerSupervisorLocalDispatchSource();
  const profile = buildWorkerSupervisorLocalDispatchProfile();
  const sourceSchema = buildWorkerSupervisorLocalDispatchSourceSchema();
  const profileSchema = buildWorkerSupervisorLocalDispatchProfileSchema();
  const actualSource = parse(LOCAL_DISPATCH_SOURCE_PATH, root);
  const actualProfile = parse(LOCAL_DISPATCH_PROFILE_PATH, root);
  const actualSourceSchema = parse(LOCAL_DISPATCH_SOURCE_SCHEMA_PATH, root);
  const actualProfileSchema = parse(LOCAL_DISPATCH_PROFILE_SCHEMA_PATH, root);
  validateSchema(actualSource, actualSourceSchema, "authority-source");
  validateSchema(actualProfile, actualProfileSchema, "profile");
  validateSchema(source, sourceSchema, "generated authority-source");
  validateSchema(profile, profileSchema, "generated profile");
  assertExact(root, LOCAL_DISPATCH_SOURCE_PATH, serializeLocalDispatch(source));
  assertExact(root, LOCAL_DISPATCH_PROFILE_PATH, serializeLocalDispatch(profile));
  assertExact(root, LOCAL_DISPATCH_SOURCE_SCHEMA_PATH, serializeLocalDispatch(sourceSchema));
  assertExact(root, LOCAL_DISPATCH_PROFILE_SCHEMA_PATH, serializeLocalDispatch(profileSchema));
  const generated = serializeWorkerSupervisorLocalDispatchGo(profile);
  assertExact(root, LOCAL_DISPATCH_GO_PATH, generated);
}

function goString(value: unknown): string {
  return JSON.stringify(value);
}

export function serializeWorkerSupervisorLocalDispatchGo(profile: LocalDispatchContract): string {
  const limits = profile.limits;
  const source = `// Code generated by scripts/generate-worker-supervisor-local-dispatch-profile.ts; DO NOT EDIT.

package supervisor

const (
	WorkerSupervisorLocalDispatchAuthorityID = ${goString(profile.authorityId)}
	WorkerSupervisorLocalDispatchRevision = ${goString(profile.revision)}
	WorkerSupervisorLocalDispatchDecision = ${goString(profile.decision)}
	WorkerSupervisorLocalDispatchProfileID = ${goString(profile.profileId)}
	WorkerSupervisorLocalDispatchProfileDigest = ${goString(profile.profileDigest)}
	WorkerSupervisorLocalDispatchSourceDigest = ${goString((profile.sourceAuthority as JsonRecord).sourceDigest)}

	// Stable aliases used by the Supervisor client.
	LocalDispatchProfileID string = WorkerSupervisorLocalDispatchProfileID
	LocalDispatchAuthorityID string = WorkerSupervisorLocalDispatchAuthorityID
	LocalDispatchRevision string = WorkerSupervisorLocalDispatchRevision
)

// LocalDispatchProfile is the generated, immutable binding consumed by the
// process-local dispatch implementation.
type LocalDispatchProfile struct {
	ID string
	AuthorityID string
	Revision string
	Decision string
	ProfileDigest string
	SourceDigest string
	Mode string
	Transport string
	ExternalSideEffects bool
	GenericClientDispatch bool
	ParentProfiles [2]string
	Capabilities [3]string
	Commands [2]string
	ProtocolMajor uint32
	ProtocolMinor uint32
	MaxWireMessageBytes uint32
	MaxRepeatedItems uint32
	MaxStringBytes uint32
	MaxPayloadBytes uint32
	MaxDeadlineSeconds uint32
	MaxIdentifierBytes uint32
	MaxFencingTokenBytes uint32
	MaxAdmissionRecords uint32
	MaxReceiptRecords uint32
}

func (p LocalDispatchProfile) Valid() bool {
	return p.ID == LocalDispatchProfileID &&
		p.AuthorityID == LocalDispatchAuthorityID &&
		p.Revision == WorkerSupervisorLocalDispatchRevision &&
		p.Decision == WorkerSupervisorLocalDispatchDecision &&
		p.ProfileDigest == WorkerSupervisorLocalDispatchProfileDigest &&
		p.SourceDigest == WorkerSupervisorLocalDispatchSourceDigest &&
		p.Mode == "localdev_only" && p.Transport == "in_process" &&
		!p.ExternalSideEffects && !p.GenericClientDispatch &&
		p.ParentProfiles == [2]string{"cloud-agents/worker-operation-admission/v1alpha1", "cloud-agents/worker-operation-execution/localdev-v1alpha1"} &&
		p.Capabilities == [3]string{"negotiation", "health", "operation_dispatch"} &&
		p.Commands == [2]string{"Probe", "ValidateBinding"} &&
		p.ProtocolMajor == 1 && p.ProtocolMinor == 0 &&
		p.MaxWireMessageBytes == 1048576 && p.MaxRepeatedItems == 64 &&
		p.MaxStringBytes == 1024 && p.MaxPayloadBytes == 65536 &&
		p.MaxDeadlineSeconds == 300 && p.MaxIdentifierBytes == 256 &&
		p.MaxFencingTokenBytes == 65536 && p.MaxAdmissionRecords == 1024 &&
		p.MaxReceiptRecords == 1024
}

var GeneratedLocalDispatchProfile = LocalDispatchProfile{
	ID: LocalDispatchProfileID, AuthorityID: LocalDispatchAuthorityID, Revision: WorkerSupervisorLocalDispatchRevision, Decision: WorkerSupervisorLocalDispatchDecision,
	ProfileDigest: WorkerSupervisorLocalDispatchProfileDigest, SourceDigest: WorkerSupervisorLocalDispatchSourceDigest,
	Mode: "localdev_only", Transport: "in_process", ExternalSideEffects: false, GenericClientDispatch: false,
	ParentProfiles: [2]string{"cloud-agents/worker-operation-admission/v1alpha1", "cloud-agents/worker-operation-execution/localdev-v1alpha1"},
	Capabilities: [3]string{"negotiation", "health", "operation_dispatch"}, Commands: [2]string{"Probe", "ValidateBinding"},
	ProtocolMajor: ${goString(limits.protocolMajor)}, ProtocolMinor: ${goString(limits.protocolMinor)}, MaxWireMessageBytes: ${goString(limits.maxWireMessageBytes)},
	MaxRepeatedItems: ${goString(limits.maxRepeatedItems)}, MaxStringBytes: ${goString(limits.maxStringBytes)}, MaxPayloadBytes: ${goString(limits.maxPayloadBytes)},
	MaxDeadlineSeconds: ${goString(limits.maxDeadlineSeconds)}, MaxIdentifierBytes: ${goString(limits.maxIdentifierBytes)}, MaxFencingTokenBytes: ${goString(limits.maxFencingTokenBytes)},
	MaxAdmissionRecords: ${goString(limits.maxAdmissionRecords)}, MaxReceiptRecords: ${goString(limits.maxReceiptRecords)},
}

func WorkerSupervisorLocalDispatchProfile() LocalDispatchProfile { return GeneratedLocalDispatchProfile }
`;
  return execFileSync("gofmt", { input: source, encoding: "utf8" });
}

export function writeWorkerSupervisorLocalDispatchFiles(root: string): void {
  const source = buildWorkerSupervisorLocalDispatchSource();
  const profile = buildWorkerSupervisorLocalDispatchProfile();
  const files: ReadonlyMap<string, string> = new Map([
    [LOCAL_DISPATCH_SOURCE_PATH, serializeLocalDispatch(source)],
    [LOCAL_DISPATCH_PROFILE_PATH, serializeLocalDispatch(profile)],
    [
      LOCAL_DISPATCH_SOURCE_SCHEMA_PATH,
      serializeLocalDispatch(buildWorkerSupervisorLocalDispatchSourceSchema()),
    ],
    [
      LOCAL_DISPATCH_PROFILE_SCHEMA_PATH,
      serializeLocalDispatch(buildWorkerSupervisorLocalDispatchProfileSchema()),
    ],
    [LOCAL_DISPATCH_GO_PATH, serializeWorkerSupervisorLocalDispatchGo(profile)],
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

export function expectedWorkerSupervisorLocalDispatchFiles(
  root: string,
): ReadonlyMap<string, string> {
  void root;
  const source = buildWorkerSupervisorLocalDispatchSource();
  const profile = buildWorkerSupervisorLocalDispatchProfile();
  return new Map([
    [LOCAL_DISPATCH_SOURCE_PATH, serializeLocalDispatch(source)],
    [LOCAL_DISPATCH_PROFILE_PATH, serializeLocalDispatch(profile)],
    [
      LOCAL_DISPATCH_SOURCE_SCHEMA_PATH,
      serializeLocalDispatch(buildWorkerSupervisorLocalDispatchSourceSchema()),
    ],
    [
      LOCAL_DISPATCH_PROFILE_SCHEMA_PATH,
      serializeLocalDispatch(buildWorkerSupervisorLocalDispatchProfileSchema()),
    ],
    [LOCAL_DISPATCH_GO_PATH, serializeWorkerSupervisorLocalDispatchGo(profile)],
  ]);
}

export function localDispatchProfileDigest(): Digest {
  return buildWorkerSupervisorLocalDispatchProfile().profileDigest as Digest;
}

export function localDispatchSourceDigest(): Digest {
  return buildWorkerSupervisorLocalDispatchSource().sourceDigest as Digest;
}

export function isLocalDispatchDigest(value: unknown): value is Digest {
  return typeof value === "string" && DIGEST_RE.test(value);
}
