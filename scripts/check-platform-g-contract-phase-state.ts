/** Read-only G-CONTRACT/P1 phase-state checker.  It has no write mode. */
import { lstatSync, readFileSync, realpathSync } from "node:fs";
import { relative, resolve, sep } from "node:path";

import {
  classifyGContractPhaseTopology,
  inspectGContractPhaseState,
} from "./lib/platform-g-contract-phase-state";
import {
  buildGContractPhaseReviewTuple,
  type GContractPhaseRecordBuildInput,
  type ReviewBinding,
} from "./lib/platform-g-contract-phase-record";

const root = resolve(import.meta.dirname, "..");

function usage(): never {
  throw new Error(
    "Usage: bun scripts/check-platform-g-contract-phase-state.ts --check | --check-record <build-input.json> | --check-terminal <build-input.json> <tuple.json>",
  );
}

function readObject(path: string): Record<string, unknown> {
  const absolute = safeRepositoryPath(path);
  const stat = lstatSync(absolute);
  if (!stat.isFile() || stat.isSymbolicLink()) {
    throw new Error(`Input JSON must be a regular non-symlink file: ${path}`);
  }
  const value: unknown = JSON.parse(readFileSync(absolute, "utf8"));
  if (typeof value !== "object" || value === null || Array.isArray(value)) {
    throw new Error(`${path} must be a JSON object.`);
  }
  return value as Record<string, unknown>;
}

function safeRepositoryPath(path: string): string {
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
    throw new Error(`Input path must be canonical and contained: ${path}`);
  let current = rootReal;
  for (const [index, component] of relation.split(sep).entries()) {
    current = resolve(current, component);
    const stat = lstatSync(current);
    const final = index === relation.split(sep).length - 1;
    if (stat.isSymbolicLink() || (!final && !stat.isDirectory()) || (final && !stat.isFile()))
      throw new Error(`Input path must be a regular non-symlink file: ${path}`);
  }
  return absolute;
}

function buildInput(path: string): GContractPhaseRecordBuildInput {
  return readObject(path) as unknown as GContractPhaseRecordBuildInput;
}

function tuple(path: string) {
  const value = readObject(path);
  if (!Array.isArray(value.reviews) || value.reviews.length !== 2) {
    throw new Error("Tuple must contain exactly two reviews.");
  }
  const rebuilt = buildGContractPhaseReviewTuple(
    root,
    value.reviews as [ReviewBinding, ReviewBinding],
  );
  if (value.tupleDigest !== undefined && value.tupleDigest !== rebuilt.tupleDigest) {
    throw new Error("Tuple digest is stale.");
  }
  return rebuilt;
}

const args = process.argv.slice(2);
const mode = args.shift();
if (mode === "--check" && args.length === 0) {
  process.stdout.write(`g-contract-phase-state: ${classifyGContractPhaseTopology(root)}\n`);
} else if (mode === "--check-record" && args.length === 1) {
  const state = inspectGContractPhaseState(root, { recordBuildInput: buildInput(args[0]!) });
  process.stdout.write(`g-contract-phase-state: ${state}\n`);
} else if (mode === "--check-terminal" && args.length === 2) {
  const expectedTuple = tuple(args[1]!);
  const state = inspectGContractPhaseState(root, {
    recordBuildInput: buildInput(args[0]!),
    expectedTuple,
  });
  process.stdout.write(`g-contract-phase-state: ${state}\n`);
} else {
  usage();
}
