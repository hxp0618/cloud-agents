import { createHash } from "node:crypto";

const BLOCK_SIZE = 512;
const MAX_ARCHIVE_BYTES = 128 * 1024 * 1024;
const MAX_PATH_BYTES = 256;
const ZERO_BLOCK = new Uint8Array(BLOCK_SIZE);
const UTF8 = new TextEncoder();
const UTF8_FATAL = new TextDecoder("utf-8", { fatal: true });

export const EXTERNAL_CONSUMER_V3_USTAR_FORMAT =
  "cloud-agents-g-contract-external-consumer-ustar/v1" as const;

export type ExternalConsumerV3UstarInput = Readonly<{
  readonly path: string;
  readonly type: "regular" | "directory";
  readonly mode: 0o644 | 0o755;
  readonly data?: Uint8Array;
}>;

export type ExternalConsumerV3UstarMember = Readonly<{
  readonly path: string;
  readonly type: "regular" | "directory";
  readonly mode: 0o644 | 0o755;
  readonly size: number;
  readonly sha256: `sha256:${string}`;
  readonly linkTarget: null;
  readonly data: Uint8Array;
}>;

export class ExternalConsumerV3UstarError extends Error {
  readonly code: string;
  readonly detail: string;

  constructor(code: string, detail: string) {
    super(`${code}: ${detail}`);
    this.name = "ExternalConsumerV3UstarError";
    this.code = code;
    this.detail = detail;
  }
}

/** Compare canonical archive paths by their UTF-8 byte sequence. */
export function compareExternalConsumerV3UstarPath(left: string, right: string): number {
  return Buffer.compare(Buffer.from(UTF8.encode(left)), Buffer.from(UTF8.encode(right)));
}

/** Validate the relative path grammar shared by archive encoding and decoding. */
export function validateExternalConsumerV3UstarPath(path: string): void {
  if (typeof path !== "string" || path.length === 0) fail("USTAR_PATH", path);
  const encoded = UTF8.encode(path);
  try {
    if (UTF8_FATAL.decode(encoded) !== path) fail("USTAR_PATH_UTF8", path);
  } catch {
    fail("USTAR_PATH_UTF8", path);
  }
  if (
    encoded.length > MAX_PATH_BYTES ||
    path.startsWith("/") ||
    path.endsWith("/") ||
    path.includes("\\") ||
    path.includes("\0") ||
    path.split("/").some((part) => part.length === 0 || part === "." || part === "..")
  ) {
    fail("USTAR_PATH", path);
  }
  for (const byte of encoded) {
    if (byte < 0x20 || byte === 0x7f) fail("USTAR_PATH_CONTROL", path);
  }
}

/** Encode a sorted, metadata-fixed USTAR archive with exactly two terminal zero blocks. */
export function encodeExternalConsumerV3Ustar(
  inputs: ReadonlyArray<ExternalConsumerV3UstarInput>,
): Uint8Array {
  const normalized = inputs.map(normalizeInput);
  normalized.sort((left, right) => compareExternalConsumerV3UstarPath(left.path, right.path));
  for (let index = 1; index < normalized.length; index += 1) {
    if (normalized[index - 1]!.path === normalized[index]!.path) {
      fail("USTAR_DUPLICATE_PATH", normalized[index]!.path);
    }
  }
  const chunks: Uint8Array[] = [];
  for (const member of normalized) {
    const header = encodeHeader(member.path, member.type, member.mode, member.data.length);
    chunks.push(header, member.data);
    const padding = (BLOCK_SIZE - (member.data.length % BLOCK_SIZE)) % BLOCK_SIZE;
    if (padding > 0) chunks.push(new Uint8Array(padding));
  }
  chunks.push(ZERO_BLOCK, ZERO_BLOCK);
  return concatenate(chunks);
}

/** Decode and fully validate a deterministic USTAR archive, returning content digests. */
export function decodeExternalConsumerV3Ustar(
  archive: Uint8Array,
): ReadonlyArray<ExternalConsumerV3UstarMember> {
  if (
    !(archive instanceof Uint8Array) ||
    archive.length < BLOCK_SIZE * 2 ||
    archive.length > MAX_ARCHIVE_BYTES ||
    archive.length % BLOCK_SIZE !== 0
  ) {
    fail("USTAR_SIZE", String(archive?.length ?? "unknown"));
  }
  const members: ExternalConsumerV3UstarMember[] = [];
  const seen = new Set<string>();
  let offset = 0;
  let previousPath: string | undefined;
  const terminalOffset = archive.length - BLOCK_SIZE * 2;
  while (offset < terminalOffset) {
    const header = archive.slice(offset, offset + BLOCK_SIZE);
    if (isZeroBlock(header)) fail("USTAR_EARLY_END", String(offset));
    const member = decodeHeader(header);
    if (seen.has(member.path)) fail("USTAR_DUPLICATE_PATH", member.path);
    if (previousPath !== undefined && compareExternalConsumerV3UstarPath(previousPath, member.path) >= 0) {
      fail("USTAR_PATH_ORDER", member.path);
    }
    seen.add(member.path);
    previousPath = member.path;
    const dataStart = offset + BLOCK_SIZE;
    const dataEnd = dataStart + member.size;
    const nextOffset = dataStart + Math.ceil(member.size / BLOCK_SIZE) * BLOCK_SIZE;
    if (nextOffset > terminalOffset) fail("USTAR_TRUNCATED", member.path);
    const data = archive.slice(dataStart, dataEnd);
    const padding = archive.slice(dataEnd, nextOffset);
    if (padding.some((byte) => byte !== 0)) fail("USTAR_PADDING", member.path);
    if (member.type === "directory" && member.size !== 0) {
      fail("USTAR_DIRECTORY_SIZE", member.path);
    }
    const canonicalHeader = encodeHeader(member.path, member.type, member.mode, member.size);
    if (!bytesEqual(canonicalHeader, header)) fail("USTAR_NON_CANONICAL_HEADER", member.path);
    members.push({
      ...member,
      data,
      sha256: digest(data),
      linkTarget: null,
    });
    offset = nextOffset;
  }
  if (!isZeroBlock(archive.slice(terminalOffset, terminalOffset + BLOCK_SIZE))) {
    fail("USTAR_END_BLOCKS", "first terminal block is not zero");
  }
  if (!isZeroBlock(archive.slice(terminalOffset + BLOCK_SIZE))) {
    fail("USTAR_END_BLOCKS", "second terminal block is not zero");
  }
  return members;
}

type NormalizedInput = Readonly<{
  readonly path: string;
  readonly type: "regular" | "directory";
  readonly mode: 0o644 | 0o755;
  readonly data: Uint8Array;
}>;

type DecodedHeader = Readonly<{
  readonly path: string;
  readonly type: "regular" | "directory";
  readonly mode: 0o644 | 0o755;
  readonly size: number;
}>;

function normalizeInput(input: ExternalConsumerV3UstarInput): NormalizedInput {
  if (!input || (input.type !== "regular" && input.type !== "directory")) {
    fail("USTAR_MEMBER_TYPE", String(input?.type));
  }
  validateExternalConsumerV3UstarPath(input.path);
  if (input.mode !== 0o644 && input.mode !== 0o755) fail("USTAR_MODE", String(input.mode));
  if (input.type === "directory" && input.mode !== 0o755) {
    fail("USTAR_DIRECTORY_MODE", input.path);
  }
  if (input.type === "directory" && input.data !== undefined && input.data.length !== 0) {
    fail("USTAR_DIRECTORY_DATA", input.path);
  }
  if (input.type === "regular" && !(input.data instanceof Uint8Array)) {
    fail("USTAR_DATA", input.path);
  }
  const data = input.data === undefined ? new Uint8Array() : Uint8Array.from(input.data);
  if (data.length > 0o77777777777) fail("USTAR_MEMBER_SIZE", input.path);
  return { path: input.path, type: input.type, mode: input.mode, data };
}

function encodeHeader(
  path: string,
  type: "regular" | "directory",
  mode: 0o644 | 0o755,
  size: number,
): Uint8Array {
  const { name, prefix } = splitUstarPath(path);
  const header = new Uint8Array(BLOCK_SIZE);
  writeUtf8(header, 0, 100, name);
  writeOctal(header, 100, 8, mode);
  writeOctal(header, 108, 8, 0);
  writeOctal(header, 116, 8, 0);
  writeOctal(header, 124, 12, size);
  writeOctal(header, 136, 12, 0);
  header[156] = type === "regular" ? 0x30 : 0x35;
  writeUtf8(header, 257, 6, "ustar\0");
  writeUtf8(header, 263, 2, "00");
  writeUtf8(header, 345, 155, prefix);
  const checksum = header.reduce((sum, value) => sum + value, 0) + 0x20 * 8;
  writeChecksum(header, checksum);
  return header;
}

function decodeHeader(header: Uint8Array): DecodedHeader {
  const storedChecksum = readChecksum(header);
  const checksumHeader = header.slice();
  checksumHeader.fill(0x20, 148, 156);
  const actualChecksum = checksumHeader.reduce((sum, value) => sum + value, 0);
  if (storedChecksum !== actualChecksum) fail("USTAR_CHECKSUM", String(storedChecksum));
  if (!bytesEqual(header.slice(257, 263), UTF8.encode("ustar\0"))) {
    fail("USTAR_PROFILE", "magic");
  }
  if (!bytesEqual(header.slice(263, 265), UTF8.encode("00"))) fail("USTAR_PROFILE", "version");
  if (header[156] !== 0x30 && header[156] !== 0x35) {
    fail("USTAR_MEMBER_TYPE", String(header[156]));
  }
  const mode = readOctal(header, 100, 8);
  if (mode !== 0o644 && mode !== 0o755) fail("USTAR_MODE", String(mode));
  const type = header[156] === 0x35 ? "directory" : "regular";
  if (type === "directory" && mode !== 0o755) fail("USTAR_DIRECTORY_MODE", String(mode));
  if (readOctal(header, 108, 8) !== 0 || readOctal(header, 116, 8) !== 0) {
    fail("USTAR_PROFILE", "uid/gid");
  }
  if (readOctal(header, 136, 12) !== 0) fail("USTAR_PROFILE", "mtime");
  if (!allZero(header.slice(157, 257))) fail("USTAR_SYMLINK", "link target is forbidden");
  if (!allZero(header.slice(265, 345)) || !allZero(header.slice(500, 512))) {
    fail("USTAR_PROFILE", "uname/gname/device/reserved fields");
  }
  const name = readUtf8Field(header.slice(0, 100), "name");
  const prefix = readUtf8Field(header.slice(345, 500), "prefix");
  const path = prefix.length > 0 ? `${prefix}/${name}` : name;
  validateExternalConsumerV3UstarPath(path);
  const expectedSplit = splitUstarPath(path);
  if (expectedSplit.name !== name || expectedSplit.prefix !== prefix) {
    fail("USTAR_PATH_SPLIT", path);
  }
  const size = readOctal(header, 124, 12);
  if (!Number.isSafeInteger(size) || size < 0) fail("USTAR_MEMBER_SIZE", String(size));
  return { path, type, mode, size };
}

function splitUstarPath(path: string): { readonly name: string; readonly prefix: string } {
  validateExternalConsumerV3UstarPath(path);
  if (UTF8.encode(path).length <= 100) return { name: path, prefix: "" };
  for (let index = path.lastIndexOf("/"); index > 0; index = path.lastIndexOf("/", index - 1)) {
    const prefix = path.slice(0, index);
    const name = path.slice(index + 1);
    if (UTF8.encode(prefix).length <= 155 && UTF8.encode(name).length <= 100) {
      return { name, prefix };
    }
  }
  fail("USTAR_PATH_SPLIT", path);
}

function writeUtf8(target: Uint8Array, offset: number, width: number, value: string): void {
  const encoded = UTF8.encode(value);
  if (encoded.length > width) fail("USTAR_FIELD", value);
  target.set(encoded, offset);
}

function writeOctal(target: Uint8Array, offset: number, width: number, value: number): void {
  const digits = value.toString(8);
  if (digits.length > width - 1) fail("USTAR_OCTAL", String(value));
  const field = `${digits.padStart(width - 1, "0")}\0`;
  writeUtf8(target, offset, width, field);
}

function writeChecksum(target: Uint8Array, value: number): void {
  const digits = value.toString(8);
  if (digits.length > 6) fail("USTAR_CHECKSUM", String(value));
  writeUtf8(target, 148, 8, `${digits.padStart(6, "0")}\0 `);
}

function readUtf8Field(field: Uint8Array, name: string): string {
  let end = field.indexOf(0);
  if (end < 0) end = field.length;
  if (!allZero(field.slice(end))) fail("USTAR_FIELD", name);
  try {
    return UTF8_FATAL.decode(field.slice(0, end));
  } catch {
    fail("USTAR_FIELD_UTF8", name);
  }
}

function readOctal(header: Uint8Array, offset: number, width: number): number {
  const field = new TextDecoder("ascii").decode(header.slice(offset, offset + width));
  const expected = new RegExp(`^[0-7]{${width - 1}}\\0$`, "u");
  if (!expected.test(field)) fail("USTAR_OCTAL", field);
  return Number.parseInt(field.slice(0, -1), 8);
}

function readChecksum(header: Uint8Array): number {
  const field = new TextDecoder("ascii").decode(header.slice(148, 156));
  if (!/^[0-7]{6}\0 $/u.test(field)) fail("USTAR_CHECKSUM", field);
  return Number.parseInt(field.slice(0, 6), 8);
}

function digest(data: Uint8Array): `sha256:${string}` {
  return `sha256:${createHash("sha256").update(data).digest("hex")}`;
}

function bytesEqual(left: Uint8Array, right: Uint8Array): boolean {
  return left.length === right.length && left.every((value, index) => value === right[index]);
}

function allZero(bytes: Uint8Array): boolean {
  return bytes.every((value) => value === 0);
}

function isZeroBlock(bytes: Uint8Array): boolean {
  return bytes.length === BLOCK_SIZE && allZero(bytes);
}

function concatenate(chunks: ReadonlyArray<Uint8Array>): Uint8Array {
  const result = new Uint8Array(chunks.reduce((size, chunk) => size + chunk.length, 0));
  let offset = 0;
  for (const chunk of chunks) {
    result.set(chunk, offset);
    offset += chunk.length;
  }
  return result;
}

function fail(code: string, detail: string): never {
  throw new ExternalConsumerV3UstarError(code, detail);
}
