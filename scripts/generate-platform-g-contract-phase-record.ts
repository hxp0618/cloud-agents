/**
 * Gate-specific G-CONTRACT/P1 phase authority CLI.
 *
 * This command intentionally has no generic Gate selector and no production
 * or network surface.  The only writes it can perform are exclusive creates
 * of the predeclared R5 Markdown record or the detached tuple/registry pair.
 */
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
  unlinkSync,
  writeFileSync,
} from "node:fs";
import { dirname, relative, resolve, sep } from "node:path";

import {
  buildGContractPhaseBindingRegistry,
  buildGContractPhaseRecordModel,
  buildGContractPhaseReviewTuple,
  G_CONTRACT_PHASE_BINDING_REGISTRY_PATH,
  G_CONTRACT_PHASE_RECORD_PATH,
  G_CONTRACT_PHASE_REVIEW_TUPLE_PATH,
  readGContractPhaseRecordSource,
  renderGContractPhaseRecord,
  serializeGContractPhaseJson,
  validateGContractPhaseBindingRegistry,
  validateGContractPhaseReviewTuple,
  type GContractPhaseRecordBuildInput,
  type GContractPhaseReviewTuple,
  type ReviewBinding,
} from "./lib/platform-g-contract-phase-record";
import {
  assertGContractPhaseReviewLineages,
  classifyGContractPhaseTopology,
} from "./lib/platform-g-contract-phase-state";

const root = resolve(import.meta.dirname, "..");

type JsonObject = Record<string, unknown>;

function usage(): never {
  throw new Error(
    "Usage: bun scripts/generate-platform-g-contract-phase-record.ts --write <build-input.json> | --check <build-input.json> | --write-binding <review-input.json> | --check-binding | --state",
  );
}

function parseObject(bytes: Buffer, label: string): JsonObject {
  let value: unknown;
  try {
    value = JSON.parse(bytes.toString("utf8"));
  } catch (error) {
    throw new Error(`${label} is not valid JSON: ${String(error)}`);
  }
  if (typeof value !== "object" || value === null || Array.isArray(value)) {
    throw new Error(`${label} must be a JSON object.`);
  }
  return value as JsonObject;
}

function readInput(path: string): JsonObject {
  const absolute = safeRepositoryPath(path);
  return parseObject(readStableRegularFile(absolute, path), path);
}

function buildInput(path: string): GContractPhaseRecordBuildInput {
  const value = readInput(path);
  const required = [
    "projectionCommit",
    "projectionTree",
    "projectionArchiveSha256",
    "supplyCandidate",
    "supplyReview",
  ];
  for (const key of required) {
    if (!(key in value)) throw new Error(`Build input is missing ${key}.`);
  }
  return value as unknown as GContractPhaseRecordBuildInput;
}

function tupleFromFile(path: string): GContractPhaseReviewTuple {
  const value = readInput(path);
  if (!Array.isArray(value.reviews) || value.reviews.length !== 2) {
    throw new Error("Review tuple input must contain exactly two ordered reviews.");
  }
  // Rebuild the digest from the typed reviews.  A caller-supplied digest is
  // accepted only when it equals the deterministic derivation.
  const tuple = buildGContractPhaseReviewTuple(
    root,
    value.reviews as ReviewBinding[] as [ReviewBinding, ReviewBinding],
  );
  if (value.tupleDigest !== undefined && value.tupleDigest !== tuple.tupleDigest) {
    throw new Error("Review tuple input carries a stale tupleDigest.");
  }
  return tuple;
}

function ensureContainedRegularDestination(path: string): string {
  const absolute = safeRepositoryPath(path, true);
  const parent = dirname(absolute);
  const rootReal = realpathSync(root);
  const relation = relative(rootReal, parent);
  let current = rootReal;
  for (const component of relation === "" ? [] : relation.split(sep)) {
    current = resolve(current, component);
    try {
      const stat = lstatSync(current);
      if (!stat.isDirectory() || stat.isSymbolicLink())
        throw new Error(`Destination parent is not a regular directory: ${path}`);
    } catch (error) {
      if (!(error instanceof Error && "code" in error && error.code === "ENOENT")) throw error;
      mkdirSync(current, { mode: 0o700 });
    }
  }
  return absolute;
}

function safeRepositoryPath(path: string, allowMissingFinal = false): string {
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
  )
    throw new Error(`Path must be canonical and contained: ${path}`);
  const components = relation.split(sep);
  let current = rootReal;
  for (const [index, component] of components.entries()) {
    current = resolve(current, component);
    try {
      const stat = lstatSync(current);
      const final = index === components.length - 1;
      if (stat.isSymbolicLink() || (!final && !stat.isDirectory()) || (final && !stat.isFile()))
        throw new Error(`Path must be a regular non-symlink file: ${path}`);
    } catch (error) {
      if (
        allowMissingFinal &&
        index === components.length - 1 &&
        error instanceof Error &&
        "code" in error &&
        error.code === "ENOENT"
      )
        return current;
      throw error;
    }
  }
  return absolute;
}

function writeExclusiveOrNoop(path: string, bytes: string): "written" | "current" {
  const output = ensureContainedRegularDestination(path);
  try {
    const current = readStableRegularFile(output, path).toString("utf8");
    if (current !== bytes)
      throw new Error(`Divergent existing bytes at ${path}; refusing overwrite.`);
    return "current";
  } catch (error) {
    if (!(error instanceof Error && "code" in error && error.code === "ENOENT")) throw error;
  }

  let descriptor: number | undefined;
  try {
    descriptor = openSync(
      output,
      constants.O_WRONLY | constants.O_CREAT | constants.O_EXCL | constants.O_NOFOLLOW,
      0o644,
    );
    const buffer = Buffer.from(bytes, "utf8");
    // writeFileSync on an already-open descriptor does not follow the output
    // path again, so the O_NOFOLLOW fence remains effective.
    writeFileSync(descriptor, buffer);
    fsyncSync(descriptor);
    closeSync(descriptor);
    descriptor = undefined;
    const parentDescriptor = openSync(dirname(output), constants.O_RDONLY | constants.O_NOFOLLOW);
    try {
      fsyncSync(parentDescriptor);
    } finally {
      closeSync(parentDescriptor);
    }
    return "written";
  } catch (error) {
    if (descriptor !== undefined) closeSync(descriptor);
    if (error instanceof Error && "code" in error && error.code === "EEXIST") {
      const current = readStableRegularFile(output, path).toString("utf8");
      if (current === bytes) return "current";
      throw new Error(`Divergent concurrent bytes at ${path}; refusing overwrite.`);
    }
    throw error;
  }
}

function writeRecord(inputPath: string): void {
  const topology = classifyGContractPhaseTopology(root);
  if (topology !== "PRE_CANDIDATE_ABSENT" && topology !== "R5_CURRENT_REVIEW_ABSENT") {
    throw new Error(
      `--write requires PRE_CANDIDATE_ABSENT or an exact R5_CURRENT_REVIEW_ABSENT no-op; current topology is ${topology}.`,
    );
  }
  const input = buildInput(inputPath);
  const model = buildGContractPhaseRecordModel(root, input);
  const bytes = renderGContractPhaseRecord(root, model);
  const result = writeExclusiveOrNoop(G_CONTRACT_PHASE_RECORD_PATH, bytes);
  process.stdout.write(`g-contract-phase-record: ${result} ${G_CONTRACT_PHASE_RECORD_PATH}\n`);
}

function checkRecord(inputPath: string): void {
  const input = buildInput(inputPath);
  const model = buildGContractPhaseRecordModel(root, input);
  const expected = renderGContractPhaseRecord(root, model);
  const actual = readStableRegularFile(
    safeRepositoryPath(G_CONTRACT_PHASE_RECORD_PATH),
    G_CONTRACT_PHASE_RECORD_PATH,
  ).toString("utf8");
  if (actual !== expected) throw new Error("G-CONTRACT R5 Markdown bytes are not current.");
  process.stdout.write("g-contract-phase-record: current\n");
}

function writeBinding(inputPath: string): void {
  const topology = classifyGContractPhaseTopology(root);
  if (
    topology !== "R5_REVIEW_CURRENT_BINDING_ABSENT" &&
    topology !== "PHASE_BINDING_CURRENT_FINAL_REVIEW_ABSENT"
  ) {
    throw new Error(
      `--write-binding requires R5_REVIEW_CURRENT_BINDING_ABSENT or an exact pre-terminal no-op; current topology is ${topology}.`,
    );
  }
  const source = readGContractPhaseRecordSource(root);
  const tuple = tupleFromFile(inputPath);
  validateGContractPhaseReviewTuple(root, tuple);
  // The JSON input is caller supplied.  Validate every candidate/review Git
  // object and the live review bytes before either detached output is created.
  // The schema/digest check above is intentionally not sufficient authority.
  assertGContractPhaseReviewLineages(root, tuple);
  const registry = buildGContractPhaseBindingRegistry(root, tuple);
  const tupleBytes = serializeGContractPhaseJson(tuple);
  const registryBytes = serializeGContractPhaseJson(registry);
  // Preflight both destinations before the first write.  This makes a stale
  // sibling fail without leaving a newly-created half-pair behind.
  preflightExclusiveOrNoop(G_CONTRACT_PHASE_REVIEW_TUPLE_PATH, tupleBytes);
  preflightExclusiveOrNoop(G_CONTRACT_PHASE_BINDING_REGISTRY_PATH, registryBytes);

  let tupleResult: "written" | "current" | undefined;
  let registryResult: "written" | "current" | undefined;
  try {
    tupleResult = writeExclusiveOrNoop(G_CONTRACT_PHASE_REVIEW_TUPLE_PATH, tupleBytes);
    registryResult = writeExclusiveOrNoop(G_CONTRACT_PHASE_BINDING_REGISTRY_PATH, registryBytes);
    validateGContractPhaseBindingRegistry(root, tuple, registry);
  } catch (error) {
    // A second-file failure can only occur after the first file was newly
    // created (or after an external race).  Remove only bytes we created and
    // only while the destination still contains those exact bytes.
    if (tupleResult === "written" && registryResult !== "written")
      removeExactCreatedFile(G_CONTRACT_PHASE_REVIEW_TUPLE_PATH, tupleBytes);
    if (registryResult === "written" && tupleResult !== "written")
      removeExactCreatedFile(G_CONTRACT_PHASE_BINDING_REGISTRY_PATH, registryBytes);
    if (tupleResult === "written" && registryResult === "written") {
      // Validation after publication should be unreachable for stable inputs,
      // but clean both newly-created outputs if an input changed mid-flight.
      removeExactCreatedFile(G_CONTRACT_PHASE_REVIEW_TUPLE_PATH, tupleBytes);
      removeExactCreatedFile(G_CONTRACT_PHASE_BINDING_REGISTRY_PATH, registryBytes);
    }
    throw error;
  }
  // Keep the source read in this command so a source mutation cannot be
  // mistaken for a successful detached write.
  if (source.binding.registryPath !== G_CONTRACT_PHASE_BINDING_REGISTRY_PATH) {
    throw new Error("G-CONTRACT source binding path drifted.");
  }
  process.stdout.write(
    `g-contract-phase-binding: tuple=${tupleResult} registry=${registryResult} ${G_CONTRACT_PHASE_BINDING_REGISTRY_PATH}\n`,
  );
}

function preflightExclusiveOrNoop(path: string, bytes: string): void {
  const output = ensureContainedRegularDestination(path);
  try {
    const current = readStableRegularFile(output, path).toString("utf8");
    if (current !== bytes)
      throw new Error(`Divergent existing bytes at ${path}; refusing pair write.`);
  } catch (error) {
    if (!(error instanceof Error && "code" in error && error.code === "ENOENT")) throw error;
  }
}

function removeExactCreatedFile(path: string, bytes: string): void {
  const output = safeRepositoryPath(path);
  try {
    if (readStableRegularFile(output, path).toString("utf8") === bytes) unlinkSync(output);
  } catch (error) {
    if (error instanceof Error && "code" in error && error.code === "ENOENT") return;
    throw error;
  }
}

function checkBinding(): void {
  const topology = classifyGContractPhaseTopology(root);
  if (
    topology === "PRE_CANDIDATE_ABSENT" ||
    topology === "R5_CURRENT_REVIEW_ABSENT" ||
    topology === "R5_REVIEW_CURRENT_BINDING_ABSENT"
  ) {
    process.stdout.write(`g-contract-phase-binding: ${topology}\n`);
    return;
  }
  const tuple = tupleFromFile(G_CONTRACT_PHASE_REVIEW_TUPLE_PATH);
  const registry = parseObject(
    readStableRegularFile(
      safeRepositoryPath(G_CONTRACT_PHASE_BINDING_REGISTRY_PATH),
      G_CONTRACT_PHASE_BINDING_REGISTRY_PATH,
    ),
    G_CONTRACT_PHASE_BINDING_REGISTRY_PATH,
  ) as unknown as ReturnType<typeof buildGContractPhaseBindingRegistry>;
  validateGContractPhaseBindingRegistry(root, tuple, registry);
  process.stdout.write("g-contract-phase-binding: current; terminal review absent\n");
}

function readStableRegularFile(path: string, label: string): Buffer {
  const before = lstatSync(path, { bigint: true });
  if (!before.isFile() || before.isSymbolicLink()) {
    throw new Error(`${label} must be a regular non-symlink file.`);
  }
  const descriptor = openSync(path, constants.O_RDONLY | constants.O_NOFOLLOW);
  try {
    const descriptorBefore = fstatSync(descriptor, { bigint: true });
    if (!sameStableIdentity(before, descriptorBefore)) {
      throw new Error(`${label} changed before read.`);
    }
    const bytes = readFileSync(descriptor);
    const descriptorAfter = fstatSync(descriptor, { bigint: true });
    const after = lstatSync(path, { bigint: true });
    if (
      !sameStableIdentity(descriptorBefore, descriptorAfter) ||
      !sameStableIdentity(descriptorBefore, after)
    ) {
      throw new Error(`${label} changed during read.`);
    }
    return bytes;
  } finally {
    closeSync(descriptor);
  }
}

function sameStableIdentity(
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

function printState(): void {
  process.stdout.write(`g-contract-phase-state: ${classifyGContractPhaseTopology(root)}\n`);
}

const args = process.argv.slice(2);
const mode = args.shift();
if (mode === "--write" && args.length === 1) writeRecord(args[0]!);
else if (mode === "--check" && args.length === 1) checkRecord(args[0]!);
else if (mode === "--write-binding" && args.length === 1) writeBinding(args[0]!);
else if (mode === "--check-binding" && args.length === 0) checkBinding();
else if (mode === "--state" && args.length === 0) printState();
else usage();
