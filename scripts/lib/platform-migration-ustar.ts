import { MigrationValidationError } from "./platform-migration-json";

export type UstarEntry = { readonly path: string; readonly data: Uint8Array };

const BLOCK = 512;
const ZERO_BLOCK = new Uint8Array(BLOCK);

export function createDeterministicUstar(entries: ReadonlyArray<UstarEntry>): Uint8Array {
  const sorted = [...entries].toSorted((left, right) => compareAscii(left.path, right.path));
  const seen = new Set<string>();
  const chunks: Uint8Array[] = [];
  for (const entry of sorted) {
    validateUstarPath(entry.path);
    if (seen.has(entry.path))
      throw new MigrationValidationError("USTAR_DUPLICATE_PATH", entry.path);
    seen.add(entry.path);
    const { name, prefix } = splitUstarPath(entry.path);
    const header = new Uint8Array(BLOCK);
    writeAscii(header, 0, 100, name);
    writeOctal(header, 100, 8, 0o644);
    writeOctal(header, 108, 8, 0);
    writeOctal(header, 116, 8, 0);
    writeOctal(header, 124, 12, entry.data.length);
    writeOctal(header, 136, 12, 0);
    header.fill(0x20, 148, 156);
    header[156] = 0x30;
    writeAscii(header, 257, 6, "ustar\0");
    writeAscii(header, 263, 2, "00");
    writeAscii(header, 345, 155, prefix);
    const checksum = header.reduce((sum, byte) => sum + byte, 0);
    writeChecksum(header, checksum);
    chunks.push(header, entry.data);
    const padding = (BLOCK - (entry.data.length % BLOCK)) % BLOCK;
    if (padding > 0) chunks.push(new Uint8Array(padding));
  }
  chunks.push(ZERO_BLOCK, ZERO_BLOCK);
  return concatenate(chunks);
}

export function readDeterministicUstar(bytes: Uint8Array): ReadonlyArray<UstarEntry> {
  if (bytes.length > 64 * 1024 * 1024 || bytes.length < BLOCK * 2 || bytes.length % BLOCK !== 0) {
    throw new MigrationValidationError("USTAR_SIZE", String(bytes.length));
  }
  const entries: UstarEntry[] = [];
  let offset = 0;
  let previous = "";
  const seen = new Set<string>();
  while (offset < bytes.length - BLOCK * 2) {
    const header = Uint8Array.from(bytes.subarray(offset, offset + BLOCK));
    if (isZeroBlock(header)) throw new MigrationValidationError("USTAR_EARLY_END", String(offset));
    const storedChecksum = readOctal(header, 148, 8);
    const checksumHeader = header.slice();
    checksumHeader.fill(0x20, 148, 156);
    const actualChecksum = checksumHeader.reduce((sum, byte) => sum + byte, 0);
    if (storedChecksum !== actualChecksum)
      throw new MigrationValidationError("USTAR_CHECKSUM", String(offset));
    if (readAscii(header, 257, 6) !== "ustar\0" || readAscii(header, 263, 2) !== "00") {
      throw new MigrationValidationError("USTAR_PROFILE", "magic/version");
    }
    if (header[156] !== 0x30 || readOctal(header, 100, 8) !== 0o644) {
      throw new MigrationValidationError("USTAR_PROFILE", "regular file mode required");
    }
    if (
      readOctal(header, 108, 8) !== 0 ||
      readOctal(header, 116, 8) !== 0 ||
      readOctal(header, 136, 12) !== 0 ||
      readAscii(header, 265, 80) !== "\0".repeat(80)
    ) {
      throw new MigrationValidationError("USTAR_PROFILE", "identity/mtime fields");
    }
    const name = trimNul(readAscii(header, 0, 100));
    const prefix = trimNul(readAscii(header, 345, 155));
    const path = prefix.length > 0 ? `${prefix}/${name}` : name;
    validateUstarPath(path);
    const canonicalSplit = splitUstarPath(path);
    if (canonicalSplit.name !== name || canonicalSplit.prefix !== prefix) {
      throw new MigrationValidationError("USTAR_PATH_SPLIT", path);
    }
    if (seen.has(path)) throw new MigrationValidationError("USTAR_DUPLICATE_PATH", path);
    if (previous && compareAscii(previous, path) >= 0) {
      throw new MigrationValidationError("USTAR_PATH_ORDER", path);
    }
    seen.add(path);
    previous = path;
    const size = readOctal(header, 124, 12);
    if (
      (path.endsWith(".sql") && size > 16 * 1024 * 1024) ||
      (path.endsWith(".json") && size > 1024 * 1024)
    ) {
      throw new MigrationValidationError("USTAR_MEMBER_SIZE", path);
    }
    const dataStart = offset + BLOCK;
    const dataEnd = dataStart + size;
    const next = dataStart + Math.ceil(size / BLOCK) * BLOCK;
    if (next > bytes.length - BLOCK * 2)
      throw new MigrationValidationError("USTAR_TRUNCATED", path);
    if (bytes.slice(dataEnd, next).some((byte) => byte !== 0)) {
      throw new MigrationValidationError("USTAR_PADDING", path);
    }
    const data = bytes.slice(dataStart, dataEnd);
    const canonicalHeader = createDeterministicUstar([{ path, data }]).slice(0, BLOCK);
    if (!Buffer.from(canonicalHeader).equals(Buffer.from(header))) {
      throw new MigrationValidationError("USTAR_NON_CANONICAL_HEADER", path);
    }
    entries.push({ path, data });
    if (entries.length > 8192)
      throw new MigrationValidationError("USTAR_FILE_LIMIT", String(entries.length));
    offset = next;
  }
  if (offset !== bytes.length - BLOCK * 2)
    throw new MigrationValidationError("USTAR_TRAILING_DATA", String(offset));
  if (
    !isZeroBlock(bytes.slice(offset, offset + BLOCK)) ||
    !isZeroBlock(bytes.slice(offset + BLOCK))
  ) {
    throw new MigrationValidationError("USTAR_END_BLOCKS", "exactly two zero blocks required");
  }
  return entries;
}

function validateUstarPath(path: string): void {
  if (
    path.length === 0 ||
    !/^[\x20-\x7e]+$/u.test(path) ||
    path.length > 256 ||
    path.startsWith("/") ||
    path.endsWith("/") ||
    path.includes("\\") ||
    path.split("/").some((part) => part === "" || part === "." || part === "..")
  ) {
    throw new MigrationValidationError("USTAR_PATH", path);
  }
}

function splitUstarPath(path: string): { readonly name: string; readonly prefix: string } {
  if (Buffer.byteLength(path, "ascii") <= 100) return { name: path, prefix: "" };
  for (let index = path.lastIndexOf("/"); index > 0; index = path.lastIndexOf("/", index - 1)) {
    const prefix = path.slice(0, index);
    const name = path.slice(index + 1);
    if (prefix.length <= 155 && name.length <= 100) return { name, prefix };
  }
  throw new MigrationValidationError("USTAR_PATH_SPLIT", path);
}

function writeAscii(target: Uint8Array, offset: number, length: number, value: string): void {
  const bytes = new TextEncoder().encode(value);
  if (bytes.length > length) throw new MigrationValidationError("USTAR_FIELD", value);
  target.set(bytes, offset);
}

function writeOctal(target: Uint8Array, offset: number, length: number, value: number): void {
  const text = value.toString(8).padStart(length - 1, "0");
  if (text.length !== length - 1) throw new MigrationValidationError("USTAR_OCTAL", String(value));
  writeAscii(target, offset, length, `${text}\0`);
}

function writeChecksum(target: Uint8Array, value: number): void {
  const text = value.toString(8).padStart(6, "0");
  if (text.length !== 6) throw new MigrationValidationError("USTAR_CHECKSUM", String(value));
  writeAscii(target, 148, 8, `${text}\0 `);
}

function readAscii(bytes: Uint8Array, offset: number, length: number): string {
  return new TextDecoder("ascii").decode(bytes.slice(offset, offset + length));
}

function readOctal(bytes: Uint8Array, offset: number, length: number): number {
  const raw = readAscii(bytes, offset, length);
  if (!/^[0-7]+\0(?: |\0)*$/u.test(raw)) throw new MigrationValidationError("USTAR_OCTAL", raw);
  return Number.parseInt(raw.slice(0, raw.indexOf("\0")), 8);
}

function trimNul(value: string): string {
  const index = value.indexOf("\0");
  const result = index < 0 ? value : value.slice(0, index);
  if (index >= 0 && /[^\0]/u.test(value.slice(index))) {
    throw new MigrationValidationError("USTAR_FIELD", "nonzero data after NUL");
  }
  return result;
}

function isZeroBlock(bytes: Uint8Array): boolean {
  return bytes.length === BLOCK && bytes.every((byte) => byte === 0);
}

function compareAscii(left: string, right: string): number {
  return Buffer.compare(Buffer.from(left, "ascii"), Buffer.from(right, "ascii"));
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
