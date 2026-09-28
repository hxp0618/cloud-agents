import { execFileSync } from "node:child_process";
import { createHash } from "node:crypto";
import { readFileSync } from "node:fs";
import path from "node:path";

const archive = process.argv[2]
  ? readFileSync(process.argv[2])
  : execFileSync("sh", ["-c", "find . -mindepth 1 -print0 | tar --null --no-recursion -cf - -T -"]);
const entries = [];
const seen = new Set();
let longName = "";
let longLink = "";
let globalPax = {};
let pendingPax = {};

const field = (offset, length) => {
  const value = archive.subarray(offset, offset + length);
  const end = value.indexOf(0);
  return value.subarray(0, end < 0 ? length : end).toString();
};
const octal = (offset, length) => parseInt(field(offset, length).trim() || "0", 8);
const paxAttributes = (data) => {
  const attributes = {};
  for (let offset = 0; offset < data.length;) {
    const lineEnd = data.indexOf(10, offset);
    if (lineEnd < 0) throw new Error("invalid pax record");
    const record = data.subarray(offset, lineEnd).toString();
    const equals = record.indexOf("=");
    const length = Number.parseInt(record.slice(0, equals), 10);
    if (!Number.isInteger(length) || length < equals + 2 || offset + length > data.length) {
      throw new Error("invalid pax record length");
    }
    const body = data.subarray(offset, offset + length - 1).toString();
    const separator = body.indexOf("=");
    attributes[body.slice(0, separator)] = body.slice(separator + 1);
    offset += length;
  }
  return attributes;
};

for (let offset = 0; offset + 512 <= archive.length;) {
  const header = archive.subarray(offset, offset + 512);
  if (header.every((value) => value === 0)) break;
  let name = field(offset, 100);
  const prefix = field(offset + 345, 155);
  if (prefix) name = `${prefix}/${name}`;
  const type = header[156] || 48;
  const size = octal(offset + 124, 12);
  const data = archive.subarray(offset + 512, offset + 512 + size);
  if (type === 76) {
    longName = data.toString().replace(/\0.*$/s, "");
  } else if (type === 75) {
    longLink = data.toString().replace(/\0.*$/s, "");
  } else if (type === 120 || type === 103) {
    const attributes = paxAttributes(data);
    if (type === 103) globalPax = { ...globalPax, ...attributes };
    else pendingPax = { ...pendingPax, ...attributes };
  } else {
    const attributes = { ...globalPax, ...pendingPax };
    pendingPax = {};
    if (longName) {
      name = longName;
      longName = "";
    }
    name = attributes.path ?? name;
    name = path.posix.normalize(name.replace(/^\.\//, "")).replace(/\/+$/g, "");
    if (name !== "." && name !== "") {
      if (name.startsWith("../") || path.posix.isAbsolute(name) || seen.has(name)) {
        throw new Error(`invalid or duplicate snapshot path ${name}`);
      }
      seen.add(name);
      const entry = { Name: name, Link: "", Digest: "", Type: type, Mode: octal(offset + 100, 8), Size: size };
      if (type === 48) {
        entry.Digest = `sha256:${createHash("sha256").update(data).digest("hex")}`;
      } else if (type === 53) {
        entry.Size = 0;
      } else if (type === 50 || type === 49) {
        entry.Link = attributes.linkpath ?? (longLink || field(offset + 157, 100));
        entry.Size = 0;
      } else {
        throw new Error(`unsupported tar entry type ${type}`);
      }
      entries.push(entry);
    }
    longLink = "";
  }
  offset += 512 + Math.ceil(size / 512) * 512;
}

entries.sort((left, right) => Buffer.compare(Buffer.from(left.Name), Buffer.from(right.Name)));
const semanticDigest = `sha256:${createHash("sha256").update(JSON.stringify(entries)).digest("hex")}`;
const rawDigest = createHash("sha256").update(archive).digest("hex");
process.stdout.write(`${semanticDigest}\n${rawDigest}`);
