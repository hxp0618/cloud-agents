import { lstatSync, readdirSync, realpathSync } from "node:fs";
import { dirname, isAbsolute, relative, resolve, sep } from "node:path";

export function requireFreshReplayPath(
  name: string,
  path: string,
  authority: string,
  expectedBasename: string,
): string {
  if (
    !isAbsolute(path) ||
    dirname(path) !== authority ||
    path !== resolve(authority, expectedBasename) ||
    pathExists(path)
  ) {
    throw new Error(`Generator replay requires fresh absent path ${name}=*/${expectedBasename}.`);
  }
  return path;
}

export function requireEmptyReplayDirectory(
  name: string,
  path: string,
  authority: string,
  expectedBasename: string,
): string {
  if (
    !isAbsolute(path) ||
    dirname(path) !== authority ||
    path !== resolve(authority, expectedBasename) ||
    !pathExists(path)
  ) {
    throw new Error(
      `Generator replay requires an empty regular directory ${name}=*/${expectedBasename}.`,
    );
  }
  const metadata = lstatSync(path);
  if (
    !metadata.isDirectory() ||
    metadata.isSymbolicLink() ||
    realpathSync(path) !== path ||
    readdirSync(path).length !== 0
  ) {
    throw new Error(
      `Generator replay requires an empty regular directory ${name}=*/${expectedBasename}.`,
    );
  }
  return path;
}

const REPLAY_REPORT_FRAME = "CLOUD_AGENTS_GENERATOR_REPLAY_REPORT_V1";
const MAX_REPLAY_REPORT_BYTES = 1024 * 1024;

export function frameReplayReport(report: unknown): string {
  const payload = JSON.stringify(report);
  const size = Buffer.byteLength(payload, "utf8");
  if (size === 0 || size > MAX_REPLAY_REPORT_BYTES) {
    throw new Error("Generator replay report payload size is outside the exact v1 bound.");
  }
  return `${REPLAY_REPORT_FRAME} ${size}\n${payload}\n`;
}

export function parseReplayReportFrame(frame: string): unknown {
  const firstNewline = frame.indexOf("\n");
  if (firstNewline < 0 || !frame.endsWith("\n")) {
    throw new Error("Generator replay stdout is not one complete v1 report frame.");
  }
  const header = frame.slice(0, firstNewline);
  const match = /^CLOUD_AGENTS_GENERATOR_REPLAY_REPORT_V1 ([1-9][0-9]*)$/u.exec(header);
  if (match === null) {
    throw new Error("Generator replay stdout has an invalid v1 report frame header.");
  }
  const payload = frame.slice(firstNewline + 1, -1);
  const expectedSize = Number(match[1]);
  if (
    !Number.isSafeInteger(expectedSize) ||
    expectedSize > MAX_REPLAY_REPORT_BYTES ||
    Buffer.byteLength(payload, "utf8") !== expectedSize ||
    payload.includes("\n")
  ) {
    throw new Error("Generator replay stdout has invalid size or trailing content.");
  }
  let report: unknown;
  try {
    report = JSON.parse(payload);
  } catch (error) {
    throw new Error(`Generator replay stdout payload is not JSON: ${String(error)}`);
  }
  if (report === null || Array.isArray(report) || typeof report !== "object") {
    throw new Error("Generator replay stdout payload must be one JSON object.");
  }
  return report;
}

export function requireExactDirectoryEntries(
  name: string,
  directory: string,
  expectedEntries: readonly string[],
): readonly string[] {
  const bytewise = (left: string, right: string): number =>
    Buffer.from(left).compare(Buffer.from(right));
  const actual = readdirSync(directory).sort(bytewise);
  const expected = [...expectedEntries].sort(bytewise);
  if (
    new Set(expected).size !== expected.length ||
    JSON.stringify(actual) !== JSON.stringify(expected)
  ) {
    throw new Error(`${name} exact directory entry closure drifted.`);
  }
  return actual;
}

export function isContainedBy(parent: string, child: string): boolean {
  const containment = relative(parent, child);
  return (
    containment === "" ||
    (containment !== ".." && !containment.startsWith(`..${sep}`) && !isAbsolute(containment))
  );
}

/**
 * `existsSync` intentionally follows symlinks.  Replay paths are authority
 * paths, so a dangling symlink must be treated as occupied rather than fresh.
 */
function pathExists(path: string): boolean {
  try {
    lstatSync(path);
    return true;
  } catch (error) {
    if ((error as NodeJS.ErrnoException).code === "ENOENT") return false;
    throw new Error(`Generator replay could not inspect authority path ${path}: ${String(error)}`);
  }
}
