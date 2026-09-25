import { spawnSync } from "node:child_process";
import { createHash } from "node:crypto";
import { readFileSync } from "node:fs";
import { join } from "node:path";
import { parseConfigFileTextToJson } from "typescript";

import { PLATFORM_RELEASE_TARGETS, platformReleaseArtifact } from "./platform-release-verifier.ts";

const PACKAGE = "@deepseek-ai/node-addon-landlock-run";
const MAX_PACKAGE_BYTES = 4 * 1024 * 1024;

/** Preserve the pinned upstream binary and license; do not compile a parallel launcher. */
export async function platformLandlockArtifacts(root: string) {
  const version = JSON.parse(
    readFileSync(join(root, "packages/cloud-agent-runtime/package.json"), "utf8"),
  ).dependencies?.[PACKAGE];
  const lock = parseLandlockLockfile(readFileSync(join(root, "bun.lock"), "utf8"));
  const outputs: Array<{ artifact: ReturnType<typeof platformReleaseArtifact>; bytes: Buffer }> =
    [];
  let license: string | undefined;
  for (const target of PLATFORM_RELEASE_TARGETS) {
    const architecture = target === "linux-amd64" ? "x64" : "arm64";
    const name = `${PACKAGE}-linux-${architecture}`;
    const source = lockedLandlockSource(lock.packages[name], name, version);
    const response = await fetch(source.url, { signal: AbortSignal.timeout(60_000) });
    const archive = await readBoundedLandlockPackage(response);
    const decoded = decodeLandlockPackage(archive, source.integrity, name, version, architecture);
    if (license !== undefined && license !== decoded.license) {
      throw new Error("Landlock platform package licenses disagree.");
    }
    license = decoded.license;
    outputs.push({
      artifact: platformReleaseArtifact(
        "cloud-agents-landlock-run",
        target,
        `cloud-agents-landlock-run-${target}`,
        decoded.binary,
      ),
      bytes: decoded.binary,
    });
  }
  const notices = Buffer.from(
    `${PACKAGE}@${version}\nSource: https://github.com/deepseek-harness/deepseek-harness/tree/main/native/landlock-run\nIntegrity authority: bun.lock platform package entries\n\n${license}`,
  );
  outputs.push({
    artifact: platformReleaseArtifact(
      "cloud-agents-landlock-notices",
      "portable",
      "cloud-agents-landlock-notices.txt",
      notices,
    ),
    bytes: notices,
  });
  return outputs;
}

export function parseLandlockLockfile(source: string): { packages: Record<string, unknown> } {
  const parsed = parseConfigFileTextToJson("bun.lock", source);
  if (parsed.error || !isRecord(parsed.config) || !isRecord(parsed.config.packages)) {
    throw new Error("bun.lock is not valid JSONC with a packages object.");
  }
  return { packages: parsed.config.packages };
}

export async function readBoundedLandlockPackage(response: Response): Promise<Buffer> {
  const contentLength = response.headers.get("content-length");
  if (
    !response.ok ||
    (contentLength !== null &&
      (!/^\d+$/u.test(contentLength) || Number(contentLength) > MAX_PACKAGE_BYTES)) ||
    !response.body
  ) {
    throw new Error("Pinned Landlock package download failed.");
  }
  const reader = response.body.getReader();
  const chunks: Uint8Array[] = [];
  let total = 0;
  try {
    while (true) {
      const { done, value } = await reader.read();
      if (done) break;
      total += value.byteLength;
      if (total > MAX_PACKAGE_BYTES) {
        await reader.cancel();
        throw new Error("Pinned Landlock package download failed.");
      }
      chunks.push(value);
    }
  } finally {
    reader.releaseLock();
  }
  return Buffer.concat(chunks, total);
}

export function lockedLandlockSource(entry: unknown, name: string, version: unknown) {
  if (
    typeof version !== "string" ||
    !/^\d+\.\d+\.\d+$/u.test(version) ||
    !Array.isArray(entry) ||
    entry[0] !== `${name}@${version}` ||
    typeof entry[1] !== "string" ||
    !/^https:\/\/(?:registry\.npmjs\.org|registry\.npmmirror\.com)\//u.test(entry[1]) ||
    typeof entry[3] !== "string" ||
    !/^sha512-[A-Za-z0-9+/]{86}==$/u.test(entry[3])
  ) {
    throw new Error("Landlock package is not exactly pinned by the Runtime manifest and lockfile.");
  }
  return { url: entry[1], integrity: entry[3] };
}

export function decodeLandlockPackage(
  archive: Buffer,
  integrity: string,
  name: string,
  version: string,
  architecture: string,
) {
  if (
    archive.length > MAX_PACKAGE_BYTES ||
    `sha512-${createHash("sha512").update(archive).digest("base64")}` !== integrity
  ) {
    throw new Error("Landlock package integrity mismatch.");
  }
  const member = (path: string): Buffer => {
    // Extract only the named regular member to stdout, never into the host filesystem.
    const result = spawnSync("tar", ["-xzOf", "-", path], {
      input: archive,
      maxBuffer: MAX_PACKAGE_BYTES,
    });
    if (result.error || result.status !== 0)
      throw new Error("Landlock package member is unavailable.");
    return result.stdout;
  };
  const metadata = JSON.parse(member("package/package.json").toString("utf8"));
  if (
    metadata.name !== name ||
    metadata.version !== version ||
    metadata.license !== "BSD-3-Clause"
  ) {
    throw new Error("Landlock package metadata does not match its locked identity.");
  }
  const binary = member("package/bin/landlock-run");
  const programHeaderOffset = binary.length >= 64 ? binary.readBigUInt64LE(32) : 0n;
  const programHeaderSize = binary.length >= 64 ? binary.readUInt16LE(54) : 0;
  const programHeaderCount = binary.length >= 64 ? binary.readUInt16LE(56) : 0;
  if (
    binary.length < 64 ||
    !binary.subarray(0, 4).equals(Buffer.from([0x7f, 0x45, 0x4c, 0x46])) ||
    binary[4] !== 2 ||
    binary[5] !== 1 ||
    binary[6] !== 1 ||
    binary.readUInt16LE(16) !== 2 ||
    binary.readUInt16LE(18) !== (architecture === "arm64" ? 183 : 62) ||
    binary.readUInt32LE(20) !== 1 ||
    binary.readUInt16LE(52) !== 64 ||
    programHeaderOffset < 64n ||
    programHeaderSize !== 56 ||
    programHeaderCount === 0 ||
    programHeaderOffset + BigInt(programHeaderSize * programHeaderCount) > BigInt(binary.length)
  ) {
    throw new Error("Landlock launcher ELF architecture is invalid.");
  }
  const license = member("package/LICENSE").toString("utf8");
  if (!license.trim()) throw new Error("Landlock license is missing.");
  return { binary, license };
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}
