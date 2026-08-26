/** Versioned generation-lock v3 CLI.  All operations are local and explicit. */
import { createHash } from "node:crypto";
import { execFileSync } from "node:child_process";
import {
  closeSync,
  constants,
  fstatSync,
  fsyncSync,
  lstatSync,
  mkdirSync,
  openSync,
  readFileSync,
  realpathSync,
  renameSync,
  unlinkSync,
  writeFileSync,
} from "node:fs";
import { dirname, relative, resolve, sep } from "node:path";

import {
  buildPlatformContractLockV3Assembled,
  buildPlatformContractLockV3PhaseBound,
  derivePlatformContractLockV3AssembledSnapshotIdentity,
  PLATFORM_CONTRACT_LOCK_V3_PATH,
  PLATFORM_CONTRACT_LOCK_V3_PHASE_ARTIFACTS,
  assertPlatformContractLockV3Document,
  serializePlatformContractLockV3,
  PLATFORM_CONTRACT_LOCK_V3_POST_H_PREDECESSOR,
  type PlatformContractLockV3ArtifactIdentity,
  type PlatformContractLockV3AssembledAuthority,
} from "./lib/platform-contract-lock-v3";
import {
  G_CONTRACT_PHASE_BINDING_REGISTRY_PATH,
  G_CONTRACT_PHASE_REVIEW_TUPLE_PATH,
  validateGContractPhaseBindingRegistry,
  validateGContractPhaseReviewTuple,
  type GContractPhaseBindingRegistry,
  type GContractPhaseReviewTuple,
} from "./lib/platform-g-contract-phase-record";
import { classifyGContractPhaseTopology } from "./lib/platform-g-contract-phase-state";
import {
  assertGeneratorSupplyV3RegistryCurrent,
  assertGeneratorSupplyV3ReplayAuthorityCurrent,
} from "./lib/platform-generator-supply-profile-v3";
import type { GeneratorSupplyReplayV3Validation } from "./lib/platform-generator-supply-replay-v3";
import { assertContractStandardsProfileCurrent } from "./lib/platform-contract-standards-profile";
import {
  SUCCESSOR_V3_CORE_GENERATOR_OUTPUT_PATHS,
  SUCCESSOR_V3_PROJECTION_EXCLUSIONS,
} from "./lib/platform-successor-dag-v3";
import { canonicalizeJson } from "./lib/platform-json-semantics";

const root = resolve(import.meta.dirname, "..");
const GIT_ENV = {
  ...process.env,
  PATH: "/usr/bin:/bin",
  LANG: "C",
  LC_ALL: "C",
  TZ: "UTC",
  GIT_CONFIG_NOSYSTEM: "1",
  GIT_CONFIG_GLOBAL: "/dev/null",
  GIT_NO_REPLACE_OBJECTS: "1",
  GIT_OPTIONAL_LOCKS: "0",
  GIT_PAGER: "cat",
};

function usage(): never {
  throw new Error(
    "Usage: bun scripts/generate-platform-contract-lock-v3.ts --check | --write-assembled | --check-assembled | --write-phase-bound [assembled-commit] | --check-phase-bound",
  );
}

function contained(path: string, allowMissingFinal = false): string {
  const rootReal = realpathSync(root);
  const absolute = resolve(rootReal, path);
  const relation = relative(rootReal, absolute);
  if (
    relation === "" ||
    relation === ".." ||
    relation.startsWith(`..${sep}`) ||
    relation.startsWith(sep) ||
    path.includes("\\") ||
    path.split("/").some((part) => part === "" || part === "." || part === "..")
  ) {
    throw new Error(`Path escaped repository root: ${path}`);
  }
  const components = relation.split(sep);
  let current = rootReal;
  for (const [index, component] of components.entries()) {
    current = resolve(current, component);
    try {
      const stat = lstatSync(current);
      const final = index === components.length - 1;
      if (stat.isSymbolicLink() || (!final && !stat.isDirectory()) || (final && !stat.isFile())) {
        throw new Error(`Path is not a regular contained file: ${path}`);
      }
    } catch (error) {
      if (
        allowMissingFinal &&
        index === components.length - 1 &&
        error instanceof Error &&
        "code" in error &&
        error.code === "ENOENT"
      ) {
        return current;
      }
      if (error instanceof Error && "code" in error && error.code === "ENOENT") throw error;
      throw error;
    }
  }
  return absolute;
}

function git(args: readonly string[]): string {
  return execFileSync("/usr/bin/git", args, {
    cwd: root,
    env: GIT_ENV,
    encoding: "utf8",
  }).trim();
}

function sha256(bytes: Uint8Array): string {
  return `sha256:${createHash("sha256").update(bytes).digest("hex")}`;
}

function gitBlob(bytes: Uint8Array): string {
  return createHash("sha1").update(`blob ${bytes.byteLength}\0`).update(bytes).digest("hex");
}

type StableFileObservation = Readonly<{
  bytes: Buffer;
  dev: bigint;
  ino: bigint;
  size: bigint;
  mtimeNs: bigint;
  ctimeNs: bigint;
}>;

function readStableRegularFile(path: string): StableFileObservation {
  const absolute = contained(path);
  let descriptor: number | undefined;
  try {
    const pathBefore = lstatSync(absolute, { bigint: true });
    if (!pathBefore.isFile() || pathBefore.isSymbolicLink())
      throw new Error(`${path} must be a regular non-symlink file.`);
    descriptor = openSync(absolute, constants.O_RDONLY | constants.O_NOFOLLOW);
    const before = fstatSync(descriptor, { bigint: true });
    if (!sameStat(pathBefore, before)) throw new Error(`${path} changed before stable read.`);
    const bytes = readFileSync(descriptor);
    const after = fstatSync(descriptor, { bigint: true });
    const pathAfter = lstatSync(absolute, { bigint: true });
    if (!sameStat(before, after) || !sameStat(before, pathAfter)) {
      throw new Error(`${path} changed during stable read (ABA or concurrent mutation).`);
    }
    return {
      bytes,
      dev: before.dev,
      ino: before.ino,
      size: before.size,
      mtimeNs: before.mtimeNs,
      ctimeNs: before.ctimeNs,
    };
  } finally {
    if (descriptor !== undefined) closeSync(descriptor);
  }
}

function sameStat(
  left: { dev: bigint; ino: bigint; size: bigint; mtimeNs: bigint; ctimeNs: bigint },
  right: { dev: bigint; ino: bigint; size: bigint; mtimeNs: bigint; ctimeNs: bigint },
): boolean {
  return (
    left.dev === right.dev &&
    left.ino === right.ino &&
    left.size === right.size &&
    left.mtimeNs === right.mtimeNs &&
    left.ctimeNs === right.ctimeNs
  );
}

function sameStableFileObservation(
  left: StableFileObservation,
  right: StableFileObservation,
): boolean {
  return (
    left.dev === right.dev &&
    left.ino === right.ino &&
    left.size === right.size &&
    left.mtimeNs === right.mtimeNs &&
    left.ctimeNs === right.ctimeNs &&
    left.bytes.equals(right.bytes)
  );
}

function artifact(
  path: string,
  options: Readonly<{ allowUntrackedLateBound?: boolean }> = {},
): PlatformContractLockV3ArtifactIdentity {
  const entry = git(["ls-tree", "HEAD", "--", path]);
  const match = /^100644 blob ([0-9a-f]{40})\t(.+)$/u.exec(entry);
  if (!match || match[2] !== path) {
    if (
      !options.allowUntrackedLateBound ||
      git(["ls-tree", "-r", "--name-only", "HEAD", "--", path])
    )
      throw new Error(`${path} is not a tracked 100644 file at HEAD.`);
    const observation = readStableRegularFile(path);
    // Slice I deliberately creates the tuple and registry in the same
    // candidate commit as the lock.  They are therefore absent from the
    // fixed R5-review HEAD while the lock writer runs.  Their stable live
    // bytes are the exact future 100644 Git blobs; the exact three-path
    // candidate checker verifies those bytes again after commit.
    return {
      path,
      fileType: "REGULAR_FILE",
      gitMode: "100644",
      gitBlobSha1: gitBlob(observation.bytes),
      sha256: sha256(observation.bytes),
      sizeBytes: observation.bytes.byteLength,
    };
  }
  const observation = readStableRegularFile(path);
  if (gitBlob(observation.bytes) !== match[1]) {
    throw new Error(`${path} has dirty bytes; lock authority requires the fixed HEAD blob.`);
  }
  return {
    path,
    fileType: "REGULAR_FILE",
    gitMode: "100644",
    gitBlobSha1: match[1]!,
    sha256: sha256(observation.bytes),
    sizeBytes: observation.bytes.byteLength,
  };
}

function json(path: string): Record<string, unknown> {
  const value: unknown = JSON.parse(readStableRegularFile(path).bytes.toString("utf8"));
  if (typeof value !== "object" || value === null || Array.isArray(value)) {
    throw new Error(`${path} must contain a JSON object.`);
  }
  return value as Record<string, unknown>;
}

function findString(value: unknown, key: string): string | undefined {
  if (typeof value !== "object" || value === null) return undefined;
  if (!Array.isArray(value) && typeof (value as Record<string, unknown>)[key] === "string") {
    return (value as Record<string, unknown>)[key] as string;
  }
  for (const child of Array.isArray(value)
    ? value
    : Object.values(value as Record<string, unknown>)) {
    const found = findString(child, key);
    if (found !== undefined) return found;
  }
  return undefined;
}

function buildAuthority(
  replayValidation: GeneratorSupplyReplayV3Validation,
): PlatformContractLockV3AssembledAuthority {
  const profilePath = "tools/generator-supply/v3/profile.json";
  const manifestPath = "tools/generator-supply/v3/evidence-manifest.json";
  const projectionPath = "tools/generator-supply/v3/evidence/replay/projection.json";
  const profile = json(profilePath);
  const projection = json(projectionPath);
  const contractStandards = assertContractStandardsProfileCurrent(root);
  if (contractStandards.formatVersion !== "cloud-agents-contract-standards-profile/v3") {
    throw new Error(
      "Current contract-standards authority must be profile v3 before lock assembly.",
    );
  }
  const excluded = projection.excluded;
  if (
    !Array.isArray(excluded) ||
    excluded.length !== SUCCESSOR_V3_PROJECTION_EXCLUSIONS.length ||
    excluded.some((path, index) => path !== SUCCESSOR_V3_PROJECTION_EXCLUSIONS[index])
  ) {
    throw new Error("v3 projection excluded paths are not the exact ordered D-053 exact17 set.");
  }
  const exclusionsDigest = sha256(canonicalizeJson(SUCCESSOR_V3_PROJECTION_EXCLUSIONS));
  if (
    projection.exclusionsDigest !== undefined &&
    projection.exclusionsDigest !== exclusionsDigest
  ) {
    throw new Error("v3 projection exclusionsDigest does not match canonical exact17 bytes.");
  }
  const profileDigest = findString(profile, "profileDigest");
  const registryDigest = profile.registryDigest;
  const candidateManifestSha256 = replayValidation.candidateManifestSha256;
  const outputFiles = replayValidation.outputFiles;
  if (
    typeof profileDigest !== "string" ||
    typeof registryDigest !== "string" ||
    typeof candidateManifestSha256 !== "string" ||
    outputFiles !== SUCCESSOR_V3_CORE_GENERATOR_OUTPUT_PATHS.length
  ) {
    throw new Error("v3 replay receipts do not expose the exact 49-output assembled authority.");
  }
  return {
    generatorSupply: {
      formatVersion: "cloud-agents-generator-supply-profile-registry/v3",
      profileId: "cloud-agents/generator-supply-profile/v3",
      profileDigest,
      registryDigest,
      candidateManifestSha256,
      outputFiles: SUCCESSOR_V3_CORE_GENERATOR_OUTPUT_PATHS.length,
      evidenceManifest: artifact(manifestPath),
      profile: artifact(profilePath),
    },
    projection: {
      algorithm: "exact-ordered-paths-v1",
      exclusionCount: 17,
      exclusionsDigest,
      receipt: artifact(projectionPath),
    },
    contractStandards: {
      formatVersion: "cloud-agents-contract-standards-profile/v3",
      profile: artifact("tools/contract-standards/profile-v3.json"),
      predecessor: artifact("tools/contract-standards/profile-v2.json"),
    },
  };
}

function readLock(): unknown {
  return JSON.parse(readStableRegularFile(PLATFORM_CONTRACT_LOCK_V3_PATH).bytes.toString("utf8"));
}

function isExactPostHPredecessor(bytes: Buffer): boolean {
  return (
    bytes.byteLength === PLATFORM_CONTRACT_LOCK_V3_POST_H_PREDECESSOR.sizeBytes &&
    sha256(bytes) === PLATFORM_CONTRACT_LOCK_V3_POST_H_PREDECESSOR.sha256 &&
    gitBlob(bytes) === PLATFORM_CONTRACT_LOCK_V3_POST_H_PREDECESSOR.gitBlobSha1
  );
}

function assertExactPostHPredecessor(bytes: Buffer): void {
  if (!isExactPostHPredecessor(bytes)) {
    throw new Error("Live v2 lock is not the exact fixed post-H predecessor.");
  }
  let parsed: unknown;
  try {
    parsed = JSON.parse(bytes.toString("utf8"));
  } catch {
    throw new Error("Fixed post-H lock predecessor is not valid JSON.");
  }
  if (
    typeof parsed !== "object" ||
    parsed === null ||
    Array.isArray(parsed) ||
    (parsed as Record<string, unknown>).formatVersion !==
      PLATFORM_CONTRACT_LOCK_V3_POST_H_PREDECESSOR.formatVersion ||
    (parsed as Record<string, unknown>).status !==
      PLATFORM_CONTRACT_LOCK_V3_POST_H_PREDECESSOR.status
  ) {
    throw new Error("Fixed post-H lock predecessor identity drifted.");
  }
}

function ensureParentDirectory(path: string): string {
  const rootReal = realpathSync(root);
  const parent = dirname(resolve(rootReal, path));
  const relation = relative(rootReal, parent);
  if (relation === ".." || relation.startsWith(`..${sep}`) || relation.startsWith(sep))
    throw new Error(`Parent escaped repository root: ${path}`);
  let current = rootReal;
  for (const component of relation === "" ? [] : relation.split(sep)) {
    current = resolve(current, component);
    try {
      const stat = lstatSync(current);
      if (!stat.isDirectory() || stat.isSymbolicLink())
        throw new Error(`Parent is not a regular directory: ${path}`);
    } catch (error) {
      if (!(error instanceof Error && "code" in error && error.code === "ENOENT")) throw error;
      mkdirSync(current, { mode: 0o755 });
      const created = lstatSync(current);
      if (!created.isDirectory() || created.isSymbolicLink())
        throw new Error(`Parent creation was not a regular directory: ${path}`);
    }
  }
  return parent;
}

function writeLock(
  bytes: string,
  allowReplace: boolean,
  expectedBefore?: (bytes: Buffer) => boolean,
): "written" | "current" {
  const output = contained(PLATFORM_CONTRACT_LOCK_V3_PATH, true);
  const parent = ensureParentDirectory(PLATFORM_CONTRACT_LOCK_V3_PATH);
  let before: StableFileObservation | undefined;
  try {
    before = readStableRegularFile(PLATFORM_CONTRACT_LOCK_V3_PATH);
    if (before.bytes.toString("utf8") === bytes) return "current";
    if (!allowReplace)
      throw new Error("Existing lock is divergent; exclusive ASSEMBLED write refused.");
    if (expectedBefore && !expectedBefore(before.bytes)) {
      throw new Error("Existing lock is not the exact authorized predecessor.");
    }
  } catch (error) {
    if (!(error instanceof Error && "code" in error && error.code === "ENOENT")) throw error;
  }
  const temporary = `${output}.v3-write-${process.pid}-${Date.now()}`;
  const transitionLock = output + ".v3-transition-lock";
  let transitionDescriptor: number | undefined;
  try {
    transitionDescriptor = openSync(
      transitionLock,
      constants.O_WRONLY | constants.O_CREAT | constants.O_EXCL | constants.O_NOFOLLOW,
      0o600,
    );
  } catch (error) {
    throw new Error(
      "Generation-lock transition is already in progress or unsafe: " + String(error),
    );
  }
  try {
    const locked = readStableRegularFile(PLATFORM_CONTRACT_LOCK_V3_PATH);
    if (before !== undefined && !sameStableFileObservation(locked, before))
      throw new Error("Live lock changed before transition lock acquisition.");
    before = locked;
  } catch (error) {
    try {
      if (transitionDescriptor !== undefined) closeSync(transitionDescriptor);
      unlinkSync(transitionLock);
    } catch {
      /* best-effort transition-lock cleanup */
    }
    throw error;
  }
  let descriptor: number | undefined;
  try {
    descriptor = openSync(
      temporary,
      constants.O_WRONLY | constants.O_CREAT | constants.O_EXCL | constants.O_NOFOLLOW,
      0o600,
    );
    writeFileSync(descriptor, bytes, "utf8");
    fsyncSync(descriptor);
    closeSync(descriptor);
    descriptor = undefined;
    if (before !== undefined) {
      const current = readStableRegularFile(PLATFORM_CONTRACT_LOCK_V3_PATH);
      if (!sameStableFileObservation(current, before))
        throw new Error("Live lock bytes changed during transition.");
      if (expectedBefore && !expectedBefore(current.bytes))
        throw new Error("Live lock predecessor changed during transition.");
    }
    renameSync(temporary, output);
    const parentDescriptor = openSync(parent, constants.O_RDONLY | constants.O_NOFOLLOW);
    try {
      fsyncSync(parentDescriptor);
    } finally {
      closeSync(parentDescriptor);
    }
    const committed = readStableRegularFile(PLATFORM_CONTRACT_LOCK_V3_PATH);
    if (!committed.bytes.equals(Buffer.from(bytes, "utf8"))) {
      throw new Error("Live lock bytes changed immediately after atomic transition.");
    }
    return "written";
  } finally {
    if (descriptor !== undefined) closeSync(descriptor);
    try {
      unlinkSync(temporary);
    } catch {
      /* already renamed */
    }
    try {
      if (transitionDescriptor !== undefined) closeSync(transitionDescriptor);
      unlinkSync(transitionLock);
    } catch {
      /* best-effort transition-lock cleanup */
    }
  }
}

function check(): void {
  const live = readStableRegularFile(PLATFORM_CONTRACT_LOCK_V3_PATH).bytes;
  const value = JSON.parse(live.toString("utf8")) as Record<string, unknown>;
  if (value.formatVersion === "cloud-agents-platform-contract-generation-lock/v2") {
    assertExactPostHPredecessor(live);
    process.stdout.write("platform-contract-lock-v3: PRE_REPLAY_LEGACY_LOCK_ONLY\n");
    return;
  }
  assertPlatformContractLockV3Document(value);
  process.stdout.write(`platform-contract-lock-v3: ${String(value.state)}\n`);
}

function writeAssembled(): void {
  const live = readStableRegularFile(PLATFORM_CONTRACT_LOCK_V3_PATH).bytes;
  assertExactPostHPredecessor(live);
  assertGeneratorSupplyV3RegistryCurrent(root);
  const replayValidation = assertGeneratorSupplyV3ReplayAuthorityCurrent(root);
  const document = buildPlatformContractLockV3Assembled(buildAuthority(replayValidation));
  replayValidation.assertSnapshotCurrent();
  const result = writeLock(
    serializePlatformContractLockV3(document),
    true,
    isExactPostHPredecessor,
  );
  process.stdout.write(`platform-contract-lock-v3: ${result} ASSEMBLED\n`);
}

function writePhaseBound(assembledCommit = git(["rev-parse", "HEAD"])): void {
  const topology = classifyGContractPhaseTopology(root);
  if (topology !== "PHASE_BINDING_CURRENT_FINAL_REVIEW_ABSENT") {
    throw new Error(
      `PHASE_BOUND requires the exact pre-terminal G-CONTRACT phase topology (got ${topology}).`,
    );
  }
  const tuple = json(G_CONTRACT_PHASE_REVIEW_TUPLE_PATH) as unknown as GContractPhaseReviewTuple;
  const registry = json(
    G_CONTRACT_PHASE_BINDING_REGISTRY_PATH,
  ) as unknown as GContractPhaseBindingRegistry;
  validateGContractPhaseReviewTuple(root, tuple);
  validateGContractPhaseBindingRegistry(root, tuple, registry);
  const assembled = readLock();
  assertPlatformContractLockV3Document(assembled);
  if (assembled.state !== "ASSEMBLED")
    throw new Error("PHASE_BOUND requires the live ASSEMBLED lock.");
  const assembledBytesAtCommit = git([
    "cat-file",
    "blob",
    `${assembledCommit}:${PLATFORM_CONTRACT_LOCK_V3_PATH}`,
  ]);
  if (assembledBytesAtCommit !== serializePlatformContractLockV3(assembled).trimEnd()) {
    throw new Error("assembledCommit does not contain the exact live ASSEMBLED lock bytes.");
  }
  const snapshot = derivePlatformContractLockV3AssembledSnapshotIdentity(assembled, {
    commitSha1: assembledCommit,
    treeSha1: git(["rev-parse", `${assembledCommit}^{tree}`]),
  });
  const phaseBinding = {
    state: "PHASE_BINDING_CURRENT_FINAL_REVIEW_ABSENT" as const,
    artifacts: PLATFORM_CONTRACT_LOCK_V3_PHASE_ARTIFACTS.map((entry) => ({
      role: entry.role,
      artifact: artifact(entry.path, {
        allowUntrackedLateBound:
          entry.path === G_CONTRACT_PHASE_REVIEW_TUPLE_PATH ||
          entry.path === G_CONTRACT_PHASE_BINDING_REGISTRY_PATH,
      }),
    })),
  };
  const next = buildPlatformContractLockV3PhaseBound(assembled, snapshot, phaseBinding);
  const result = writeLock(serializePlatformContractLockV3(next), true);
  process.stdout.write(`platform-contract-lock-v3: ${result} PHASE_BOUND\n`);
}

const args = process.argv.slice(2);
const mode = args.shift();
if (mode === "--check" && args.length === 0) check();
else if (mode === "--write-assembled" && args.length === 0) writeAssembled();
else if (mode === "--check-assembled" && args.length === 0) {
  const value = readLock();
  assertPlatformContractLockV3Document(value);
  if (value.state !== "ASSEMBLED") throw new Error("Live lock is not ASSEMBLED.");
  process.stdout.write("platform-contract-lock-v3: ASSEMBLED current\n");
} else if (mode === "--write-phase-bound" && (args.length === 0 || args.length === 1))
  writePhaseBound(args[0]);
else if (mode === "--check-phase-bound" && args.length === 0) {
  const value = readLock();
  assertPlatformContractLockV3Document(value);
  if (value.state !== "PHASE_BOUND") throw new Error("Live lock is not PHASE_BOUND.");
  process.stdout.write("platform-contract-lock-v3: PHASE_BOUND current\n");
} else usage();
