import { createHash } from "node:crypto";

export const PLATFORM_CONTRACT_LOCK_V3_PATH = "contracts/generation.lock.json" as const;
export const PLATFORM_CONTRACT_LOCK_V3_FORMAT_VERSION =
  "cloud-agents-platform-contract-generation-lock/v3" as const;
export const PLATFORM_CONTRACT_LOCK_V3_DIGEST_DOMAIN =
  "cloud-agents/platform-contract-generation-lock/document/v3" as const;

export const PLATFORM_CONTRACT_LOCK_V3_POST_H_PREDECESSOR = Object.freeze({
  formatVersion: "cloud-agents-platform-contract-generation-lock/v2",
  status: "SUCCESSOR_ASSEMBLED_REVIEW_BOUND",
  commitSha1: "16275f6cbf390c343a9ac00f9193e75eaad0094e",
  treeSha1: "ca595b8e1258a8b78c4da3a545b2a31d8f62b531",
  path: PLATFORM_CONTRACT_LOCK_V3_PATH,
  fileType: "REGULAR_FILE",
  gitMode: "100644",
  gitBlobSha1: "39ee20e035d8770340d46a8663633c6519830de1",
  sha256: "sha256:de62a85390e58736a5fef8272878b821415b4180948437e54edb8628e005ff53",
  sizeBytes: 17_377,
} as const);

export const PLATFORM_CONTRACT_LOCK_V3_PHASE_ARTIFACTS = Object.freeze([
  {
    role: "R5_CANDIDATE",
    path: "docs/plan/cloud-agents-platform/evidence/G-CONTRACT/CAG-G-CONTRACT-P1-20260825-R5.md",
  },
  {
    role: "R5_REVIEW",
    path: "docs/plan/p1/g-contract-r5-current-source-independent-review-20260825.md",
  },
  {
    role: "REVIEW_TUPLE",
    path: "tools/gate-phase-record/g-contract-p1/v1/review-tuple.json",
  },
  {
    role: "BINDING_REGISTRY",
    path: "tools/gate-phase-record/g-contract-p1/v1/registry.json",
  },
] as const);

const GENERATOR_SUPPLY_V3_EVIDENCE_MANIFEST_PATH =
  "tools/generator-supply/v3/evidence-manifest.json";
const GENERATOR_SUPPLY_V3_PROFILE_PATH = "tools/generator-supply/v3/profile.json";
const GENERATOR_SUPPLY_V3_PROJECTION_PATH =
  "tools/generator-supply/v3/evidence/replay/projection.json";

const ASSEMBLED_AUTHORITY_KEYS = ["generatorSupply", "projection"] as const;
const GENERATOR_SUPPLY_KEYS = [
  "formatVersion",
  "profileId",
  "profileDigest",
  "registryDigest",
  "candidateManifestSha256",
  "outputFiles",
  "evidenceManifest",
  "profile",
] as const;
const PROJECTION_KEYS = ["algorithm", "exclusionCount", "exclusionsDigest", "receipt"] as const;
const ARTIFACT_KEYS = [
  "path",
  "fileType",
  "gitMode",
  "gitBlobSha1",
  "sha256",
  "sizeBytes",
] as const;
const SNAPSHOT_KEYS = [
  "state",
  "commitSha1",
  "treeSha1",
  "path",
  "fileType",
  "gitMode",
  "gitBlobSha1",
  "sha256",
  "sizeBytes",
] as const;
const FILE_OBSERVATION_KEYS = [
  "path",
  "fileType",
  "gitMode",
  "gitBlobSha1",
  "sha256",
  "sizeBytes",
  "device",
  "inode",
  "mtimeNs",
  "ctimeNs",
] as const;
const IMPLEMENTATION_BOUNDARY = Object.freeze({
  productionDatabaseWrites: "NOT_AUTHORIZED",
  httpSurface: "NOT_IMPLEMENTED",
  p2Surface: "NOT_IMPLEMENTED",
  providerSideEffects: "FORBIDDEN",
  deployment: "NOT_AUTHORIZED",
  publication: "NOT_AUTHORIZED",
  gateStatus: "ALL_GATES_OPEN",
} as const);

export type PlatformContractLockV3ArtifactIdentity = Readonly<{
  path: string;
  fileType: "REGULAR_FILE";
  gitMode: "100644";
  gitBlobSha1: string;
  sha256: string;
  sizeBytes: number;
}>;

export type PlatformContractLockV3AssembledAuthority = Readonly<{
  generatorSupply: Readonly<{
    formatVersion: "cloud-agents-generator-supply-profile-registry/v3";
    profileId: "cloud-agents/generator-supply-profile/v3";
    profileDigest: string;
    registryDigest: string;
    candidateManifestSha256: string;
    outputFiles: 49;
    evidenceManifest: PlatformContractLockV3ArtifactIdentity;
    profile: PlatformContractLockV3ArtifactIdentity;
  }>;
  projection: Readonly<{
    algorithm: "exact-ordered-paths-v1";
    exclusionCount: 17;
    exclusionsDigest: string;
    receipt: PlatformContractLockV3ArtifactIdentity;
  }>;
}>;

export type PlatformContractLockV3AssembledSnapshotIdentity = Readonly<{
  state: "ASSEMBLED";
  commitSha1: string;
  treeSha1: string;
  path: typeof PLATFORM_CONTRACT_LOCK_V3_PATH;
  fileType: "REGULAR_FILE";
  gitMode: "100644";
  gitBlobSha1: string;
  sha256: string;
  sizeBytes: number;
}>;

export type PlatformContractLockV3PhaseBinding = Readonly<{
  state: "PHASE_BINDING_CURRENT_FINAL_REVIEW_ABSENT";
  artifacts: readonly Readonly<{
    role: (typeof PLATFORM_CONTRACT_LOCK_V3_PHASE_ARTIFACTS)[number]["role"];
    artifact: PlatformContractLockV3ArtifactIdentity;
  }>[];
}>;

type PlatformContractLockV3Boundary = typeof IMPLEMENTATION_BOUNDARY;
type PlatformContractLockV3Predecessor = typeof PLATFORM_CONTRACT_LOCK_V3_POST_H_PREDECESSOR;

export type PlatformContractLockV3AssembledDocument = Readonly<{
  formatVersion: typeof PLATFORM_CONTRACT_LOCK_V3_FORMAT_VERSION;
  lockVersion: 3;
  state: "ASSEMBLED";
  notGateClosure: true;
  gateStatus: "ALL_GATES_OPEN";
  predecessorV2: PlatformContractLockV3Predecessor;
  assembledAuthority: PlatformContractLockV3AssembledAuthority;
  implementationBoundary: PlatformContractLockV3Boundary;
  lockDigest: string;
}>;

export type PlatformContractLockV3PhaseBoundDocument = Readonly<{
  formatVersion: typeof PLATFORM_CONTRACT_LOCK_V3_FORMAT_VERSION;
  lockVersion: 3;
  state: "PHASE_BOUND";
  notGateClosure: true;
  gateStatus: "ALL_GATES_OPEN";
  predecessorV2: PlatformContractLockV3Predecessor;
  assembledAuthority: PlatformContractLockV3AssembledAuthority;
  assembledSnapshot: PlatformContractLockV3AssembledSnapshotIdentity;
  phaseBinding: PlatformContractLockV3PhaseBinding;
  implementationBoundary: PlatformContractLockV3Boundary;
  lockDigest: string;
}>;

export type PlatformContractLockV3Document =
  | PlatformContractLockV3AssembledDocument
  | PlatformContractLockV3PhaseBoundDocument;

export type PlatformContractLockV3FileObservation = Readonly<{
  path: typeof PLATFORM_CONTRACT_LOCK_V3_PATH;
  fileType: "REGULAR_FILE";
  gitMode: "100644";
  gitBlobSha1: string;
  sha256: string;
  sizeBytes: number;
  device: string;
  inode: string;
  mtimeNs: string;
  ctimeNs: string;
}>;

export type PlatformContractLockV3TransitionObservation = Readonly<{
  readBefore: PlatformContractLockV3FileObservation;
  readAfter: PlatformContractLockV3FileObservation;
}>;

export function buildPlatformContractLockV3Assembled(
  authority: PlatformContractLockV3AssembledAuthority,
): PlatformContractLockV3AssembledDocument {
  assertAssembledAuthority(authority);
  const body = {
    formatVersion: PLATFORM_CONTRACT_LOCK_V3_FORMAT_VERSION,
    lockVersion: 3 as const,
    state: "ASSEMBLED" as const,
    notGateClosure: true as const,
    gateStatus: "ALL_GATES_OPEN" as const,
    predecessorV2: { ...PLATFORM_CONTRACT_LOCK_V3_POST_H_PREDECESSOR },
    assembledAuthority: cloneAssembledAuthority(authority),
    implementationBoundary: { ...IMPLEMENTATION_BOUNDARY },
  };
  return {
    ...body,
    lockDigest: digestDocumentBody(body),
  };
}

export function derivePlatformContractLockV3AssembledSnapshotIdentity(
  assembled: PlatformContractLockV3AssembledDocument,
  git: Readonly<{ commitSha1: string; treeSha1: string }>,
): PlatformContractLockV3AssembledSnapshotIdentity {
  assertPlatformContractLockV3Document(assembled);
  if (assembled.state !== "ASSEMBLED") {
    throw new Error("Generation-lock v3 snapshot derivation requires the ASSEMBLED state.");
  }
  assertExactOrderedKeys(git, ["commitSha1", "treeSha1"], "assembled Git identity");
  assertGitSha1(git.commitSha1, "assembled commit");
  assertGitSha1(git.treeSha1, "assembled tree");
  const bytes = serializePlatformContractLockV3(assembled);
  return {
    state: "ASSEMBLED",
    commitSha1: git.commitSha1,
    treeSha1: git.treeSha1,
    path: PLATFORM_CONTRACT_LOCK_V3_PATH,
    fileType: "REGULAR_FILE",
    gitMode: "100644",
    gitBlobSha1: gitBlobSha1(bytes),
    sha256: sha256(bytes),
    sizeBytes: Buffer.byteLength(bytes),
  };
}

export function buildPlatformContractLockV3PhaseBound(
  assembled: PlatformContractLockV3AssembledDocument,
  assembledSnapshot: PlatformContractLockV3AssembledSnapshotIdentity,
  phaseBinding: PlatformContractLockV3PhaseBinding,
): PlatformContractLockV3PhaseBoundDocument {
  assertPlatformContractLockV3Document(assembled);
  if (assembled.state !== "ASSEMBLED") {
    throw new Error("Generation-lock v3 PHASE_BOUND requires an ASSEMBLED predecessor.");
  }
  assertAssembledSnapshot(assembledSnapshot);
  assertSnapshotMatchesAssembledDocument(assembled, assembledSnapshot);
  assertPhaseBinding(phaseBinding);

  const body = {
    formatVersion: PLATFORM_CONTRACT_LOCK_V3_FORMAT_VERSION,
    lockVersion: 3 as const,
    state: "PHASE_BOUND" as const,
    notGateClosure: true as const,
    gateStatus: "ALL_GATES_OPEN" as const,
    predecessorV2: { ...PLATFORM_CONTRACT_LOCK_V3_POST_H_PREDECESSOR },
    assembledAuthority: cloneAssembledAuthority(assembled.assembledAuthority),
    assembledSnapshot: { ...assembledSnapshot },
    phaseBinding: clonePhaseBinding(phaseBinding),
    implementationBoundary: { ...IMPLEMENTATION_BOUNDARY },
  };
  return {
    ...body,
    lockDigest: digestDocumentBody(body),
  };
}

export function assertPlatformContractLockV3Document(
  value: unknown,
): asserts value is PlatformContractLockV3Document {
  assertRecord(value, "generation-lock v3 document");
  const state = value.state;
  if (state !== "ASSEMBLED" && state !== "PHASE_BOUND") {
    throw new Error(`Generation-lock v3 rejects unknown or skipped state: ${String(state)}.`);
  }
  const expectedKeys =
    state === "ASSEMBLED"
      ? [
          "formatVersion",
          "lockVersion",
          "state",
          "notGateClosure",
          "gateStatus",
          "predecessorV2",
          "assembledAuthority",
          "implementationBoundary",
          "lockDigest",
        ]
      : [
          "formatVersion",
          "lockVersion",
          "state",
          "notGateClosure",
          "gateStatus",
          "predecessorV2",
          "assembledAuthority",
          "assembledSnapshot",
          "phaseBinding",
          "implementationBoundary",
          "lockDigest",
        ];
  assertExactOrderedKeys(value, expectedKeys, `${state} document`);
  if (
    value.formatVersion !== PLATFORM_CONTRACT_LOCK_V3_FORMAT_VERSION ||
    value.lockVersion !== 3 ||
    value.notGateClosure !== true ||
    value.gateStatus !== "ALL_GATES_OPEN"
  ) {
    throw new Error("Generation-lock v3 format or non-Gate boundary is invalid.");
  }
  assertPostHPredecessor(value.predecessorV2);
  assertAssembledAuthority(value.assembledAuthority);
  if (state === "PHASE_BOUND") {
    assertAssembledSnapshot(value.assembledSnapshot);
    assertPhaseBinding(value.phaseBinding);
  }
  assertImplementationBoundary(value.implementationBoundary);
  assertSha256(value.lockDigest, "lock digest");
  const body = Object.fromEntries(Object.entries(value).filter(([key]) => key !== "lockDigest"));
  const expectedDigest = digestDocumentBody(body);
  if (value.lockDigest !== expectedDigest) {
    throw new Error(
      `Generation-lock v3 self digest mismatch: expected=${expectedDigest} actual=${String(value.lockDigest)}.`,
    );
  }
}

export function assertPlatformContractLockV3Transition(
  previous: unknown,
  next: unknown,
  observation: PlatformContractLockV3TransitionObservation,
): asserts next is PlatformContractLockV3PhaseBoundDocument {
  assertPlatformContractLockV3Document(previous);
  assertPlatformContractLockV3Document(next);
  if (previous.state !== "ASSEMBLED" || next.state !== "PHASE_BOUND") {
    throw new Error(
      `Generation-lock v3 permits only ASSEMBLED -> PHASE_BOUND; received ${previous.state} -> ${next.state}.`,
    );
  }
  if (
    JSON.stringify(previous.predecessorV2) !== JSON.stringify(next.predecessorV2) ||
    JSON.stringify(previous.assembledAuthority) !== JSON.stringify(next.assembledAuthority)
  ) {
    throw new Error("Generation-lock v3 transition changed immutable assembled authority.");
  }
  assertSnapshotMatchesAssembledDocument(previous, next.assembledSnapshot);
  assertStableTransitionObservation(observation, next.assembledSnapshot);
}

export function serializePlatformContractLockV3(value: unknown): string {
  assertPlatformContractLockV3Document(value);
  return `${JSON.stringify(value, null, 2)}\n`;
}

function assertAssembledAuthority(
  value: unknown,
): asserts value is PlatformContractLockV3AssembledAuthority {
  assertRecord(value, "assembled authority");
  assertExactOrderedKeys(value, ASSEMBLED_AUTHORITY_KEYS, "assembled authority");
  assertRecord(value.generatorSupply, "generator-supply v3 authority");
  assertExactOrderedKeys(
    value.generatorSupply,
    GENERATOR_SUPPLY_KEYS,
    "generator-supply v3 authority",
  );
  if (
    value.generatorSupply.formatVersion !== "cloud-agents-generator-supply-profile-registry/v3" ||
    value.generatorSupply.profileId !== "cloud-agents/generator-supply-profile/v3" ||
    value.generatorSupply.outputFiles !== 49
  ) {
    throw new Error(
      "Generation-lock v3 requires the exact assembled generator-supply-v3 authority.",
    );
  }
  assertSha256(value.generatorSupply.profileDigest, "generator-supply profile digest");
  assertSha256(value.generatorSupply.registryDigest, "generator-supply registry digest");
  assertSha256(
    value.generatorSupply.candidateManifestSha256,
    "generator-supply candidate manifest",
  );
  assertArtifact(
    value.generatorSupply.evidenceManifest,
    GENERATOR_SUPPLY_V3_EVIDENCE_MANIFEST_PATH,
    "generator-supply evidence manifest",
  );
  assertArtifact(
    value.generatorSupply.profile,
    GENERATOR_SUPPLY_V3_PROFILE_PATH,
    "generator-supply profile",
  );

  assertRecord(value.projection, "projection authority");
  assertExactOrderedKeys(value.projection, PROJECTION_KEYS, "projection authority");
  if (
    value.projection.algorithm !== "exact-ordered-paths-v1" ||
    value.projection.exclusionCount !== 17
  ) {
    throw new Error("Generation-lock v3 requires the exact17 ordered successor projection.");
  }
  assertSha256(value.projection.exclusionsDigest, "projection exclusions digest");
  assertArtifact(
    value.projection.receipt,
    GENERATOR_SUPPLY_V3_PROJECTION_PATH,
    "projection receipt",
  );
}

function assertPhaseBinding(value: unknown): asserts value is PlatformContractLockV3PhaseBinding {
  assertRecord(value, "phase binding");
  assertExactOrderedKeys(value, ["state", "artifacts"], "phase binding");
  if (value.state !== "PHASE_BINDING_CURRENT_FINAL_REVIEW_ABSENT") {
    throw new Error(
      "Generation-lock v3 phase binding must remain terminal-review-absent before the review-only child.",
    );
  }
  if (!Array.isArray(value.artifacts) || value.artifacts.length !== 4) {
    throw new Error(
      "Generation-lock v3 rejects partial phase binding; exactly four artifacts are required.",
    );
  }
  value.artifacts.forEach((entry, index) => {
    assertRecord(entry, `phase artifact ${index}`);
    assertExactOrderedKeys(entry, ["role", "artifact"], `phase artifact ${index}`);
    const expected = PLATFORM_CONTRACT_LOCK_V3_PHASE_ARTIFACTS[index];
    if (entry.role !== expected.role) {
      throw new Error(
        `Generation-lock v3 phase artifacts are reordered or unknown at index ${index}: expected=${expected.role} actual=${String(entry.role)}.`,
      );
    }
    assertArtifact(entry.artifact, expected.path, `phase artifact ${expected.role}`);
  });
}

function assertPostHPredecessor(
  value: unknown,
): asserts value is PlatformContractLockV3Predecessor {
  assertRecord(value, "post-H v2 predecessor");
  assertExactOrderedKeys(
    value,
    [
      "formatVersion",
      "status",
      "commitSha1",
      "treeSha1",
      "path",
      "fileType",
      "gitMode",
      "gitBlobSha1",
      "sha256",
      "sizeBytes",
    ],
    "post-H v2 predecessor",
  );
  if (JSON.stringify(value) !== JSON.stringify(PLATFORM_CONTRACT_LOCK_V3_POST_H_PREDECESSOR)) {
    throw new Error("Generation-lock v3 post-H v2 predecessor identity drifted.");
  }
}

function assertAssembledSnapshot(
  value: unknown,
): asserts value is PlatformContractLockV3AssembledSnapshotIdentity {
  assertRecord(value, "assembled snapshot");
  assertExactOrderedKeys(value, SNAPSHOT_KEYS, "assembled snapshot");
  if (value.state !== "ASSEMBLED") {
    throw new Error("Generation-lock v3 historical snapshot must be ASSEMBLED.");
  }
  assertGitSha1(value.commitSha1, "assembled commit");
  assertGitSha1(value.treeSha1, "assembled tree");
  assertArtifactValues(value, PLATFORM_CONTRACT_LOCK_V3_PATH, "assembled lock snapshot");
}

function assertSnapshotMatchesAssembledDocument(
  assembled: PlatformContractLockV3AssembledDocument,
  snapshot: PlatformContractLockV3AssembledSnapshotIdentity,
): void {
  const bytes = serializePlatformContractLockV3(assembled);
  const expected = {
    blob: gitBlobSha1(bytes),
    sha256: sha256(bytes),
    sizeBytes: Buffer.byteLength(bytes),
  };
  if (
    snapshot.gitBlobSha1 !== expected.blob ||
    snapshot.sha256 !== expected.sha256 ||
    snapshot.sizeBytes !== expected.sizeBytes
  ) {
    throw new Error(
      "Generation-lock v3 assembled snapshot does not identify the exact historical ASSEMBLED bytes.",
    );
  }
}

function assertStableTransitionObservation(
  value: unknown,
  snapshot: PlatformContractLockV3AssembledSnapshotIdentity,
): void {
  assertRecord(value, "transition observation");
  assertExactOrderedKeys(value, ["readBefore", "readAfter"], "transition observation");
  assertFileObservation(value.readBefore, "first lock read");
  assertFileObservation(value.readAfter, "second lock read");
  const expectedContent = {
    path: snapshot.path,
    gitMode: snapshot.gitMode,
    gitBlobSha1: snapshot.gitBlobSha1,
    sha256: snapshot.sha256,
    sizeBytes: snapshot.sizeBytes,
  };
  for (const [label, read] of [
    ["first", value.readBefore],
    ["second", value.readAfter],
  ] as const) {
    if (
      read.path !== expectedContent.path ||
      read.gitMode !== expectedContent.gitMode ||
      read.gitBlobSha1 !== expectedContent.gitBlobSha1 ||
      read.sha256 !== expectedContent.sha256 ||
      read.sizeBytes !== expectedContent.sizeBytes
    ) {
      throw new Error(`Generation-lock v3 ${label} read does not match the assembled snapshot.`);
    }
  }
  if (JSON.stringify(value.readBefore) !== JSON.stringify(value.readAfter)) {
    throw new Error(
      "Generation-lock v3 stable-read identity changed; possible symlink, replacement, or ABA mutation.",
    );
  }
}

function assertFileObservation(
  value: unknown,
  label: string,
): asserts value is PlatformContractLockV3FileObservation {
  assertRecord(value, label);
  assertExactOrderedKeys(value, FILE_OBSERVATION_KEYS, label);
  assertArtifactValues(value, PLATFORM_CONTRACT_LOCK_V3_PATH, label);
  for (const field of ["device", "inode", "mtimeNs", "ctimeNs"] as const) {
    if (typeof value[field] !== "string" || !/^(?:0|[1-9][0-9]*)$/u.test(value[field])) {
      throw new Error(`Generation-lock v3 ${label} ${field} is not a canonical integer string.`);
    }
  }
}

function assertArtifact(value: unknown, expectedPath: string, label: string): void {
  assertRecord(value, label);
  assertExactOrderedKeys(value, ARTIFACT_KEYS, label);
  assertArtifactValues(value, expectedPath, label);
}

function assertArtifactValues(
  value: Record<string, unknown>,
  expectedPath: string,
  label: string,
): void {
  if (
    value.path !== expectedPath ||
    value.fileType !== "REGULAR_FILE" ||
    value.gitMode !== "100644"
  ) {
    throw new Error(
      `Generation-lock v3 ${label} must be the exact regular non-symlink 100644 path ${expectedPath}.`,
    );
  }
  assertGitSha1(value.gitBlobSha1, `${label} Git blob`);
  assertSha256(value.sha256, `${label} SHA-256`);
  if (!Number.isSafeInteger(value.sizeBytes) || (value.sizeBytes as number) < 0) {
    throw new Error(`Generation-lock v3 ${label} size is invalid.`);
  }
}

function assertImplementationBoundary(
  value: unknown,
): asserts value is PlatformContractLockV3Boundary {
  assertRecord(value, "implementation boundary");
  assertExactOrderedKeys(
    value,
    [
      "productionDatabaseWrites",
      "httpSurface",
      "p2Surface",
      "providerSideEffects",
      "deployment",
      "publication",
      "gateStatus",
    ],
    "implementation boundary",
  );
  if (JSON.stringify(value) !== JSON.stringify(IMPLEMENTATION_BOUNDARY)) {
    throw new Error("Generation-lock v3 implementation boundary was widened.");
  }
}

function cloneAssembledAuthority(
  value: PlatformContractLockV3AssembledAuthority,
): PlatformContractLockV3AssembledAuthority {
  return {
    generatorSupply: {
      ...value.generatorSupply,
      evidenceManifest: { ...value.generatorSupply.evidenceManifest },
      profile: { ...value.generatorSupply.profile },
    },
    projection: {
      ...value.projection,
      receipt: { ...value.projection.receipt },
    },
  };
}

function clonePhaseBinding(
  value: PlatformContractLockV3PhaseBinding,
): PlatformContractLockV3PhaseBinding {
  return {
    state: value.state,
    artifacts: value.artifacts.map((entry) => ({
      role: entry.role,
      artifact: { ...entry.artifact },
    })),
  };
}

function assertRecord(value: unknown, label: string): asserts value is Record<string, unknown> {
  if (typeof value !== "object" || value === null || Array.isArray(value)) {
    throw new Error(`Generation-lock v3 ${label} must be an object.`);
  }
}

function assertExactOrderedKeys(
  value: object,
  expected: readonly string[],
  label: string,
  ...allowedAlternatives: readonly (readonly string[])[]
): void {
  const actual = Object.keys(value);
  if (
    ![expected, ...allowedAlternatives].some(
      (keys) => JSON.stringify(actual) === JSON.stringify(keys),
    )
  ) {
    throw new Error(
      `Generation-lock v3 ${label} topology or field order mismatch: expected=${JSON.stringify(expected)} actual=${JSON.stringify(actual)}.`,
    );
  }
}

function assertSha256(value: unknown, label: string): asserts value is string {
  if (typeof value !== "string" || !/^sha256:[0-9a-f]{64}$/u.test(value)) {
    throw new Error(`Generation-lock v3 ${label} is invalid.`);
  }
}

function assertGitSha1(value: unknown, label: string): asserts value is string {
  if (typeof value !== "string" || !/^[0-9a-f]{40}$/u.test(value)) {
    throw new Error(`Generation-lock v3 ${label} is invalid.`);
  }
}

function digestDocumentBody(value: unknown): string {
  return `sha256:${createHash("sha256")
    .update(PLATFORM_CONTRACT_LOCK_V3_DIGEST_DOMAIN)
    .update("\0")
    .update(JSON.stringify(value))
    .digest("hex")}`;
}

function sha256(value: string): string {
  return `sha256:${createHash("sha256").update(value).digest("hex")}`;
}

function gitBlobSha1(value: string): string {
  const bytes = Buffer.from(value);
  return createHash("sha1").update(`blob ${bytes.byteLength}\0`).update(bytes).digest("hex");
}
