import { createHash, createPublicKey, verify } from "node:crypto";
import {
  chmodSync,
  existsSync,
  lstatSync,
  mkdirSync,
  mkdtempSync,
  readdirSync,
  renameSync,
  rmSync,
  writeFileSync,
} from "node:fs";
import { dirname, join, resolve, sep } from "node:path";

import type { RuntimeCapabilityManifest } from "@cloud-agents/cloud-agent-provider-api/internal";
import type { CapabilityMaterialization, SkillMaterialization } from "./capabilityBroker";

const DEFAULT_SKILL_ROOT = "/run/cloud-agents/skills";
const MAX_BUNDLE_BYTES = 512 * 1024;
const MAX_BUNDLE_FILES = 128;
const MAX_SKILL_TREE_BYTES = 2 * 1024 * 1024;

export type ManagedSkillMounts = Readonly<{
  environment: Readonly<Record<string, string>>;
  close(): void;
}>;

/** Verifies and mounts Host-delivered Skill Bundles as immutable directories. */
export function materializeManagedSkills(
  manifest: RuntimeCapabilityManifest | null,
  materialization: CapabilityMaterialization | null,
  rootDirectory = DEFAULT_SKILL_ROOT,
): ManagedSkillMounts | null {
  const bindings =
    manifest?.bindings.filter((binding) => binding.resourceKind === "skill-bundle") ?? [];
  if (bindings.length === 0) return null;
  if (!materialization || materialization.skills.length !== bindings.length) {
    throw new Error("Skill Bundle materialization is unavailable.");
  }
  if (resolve(rootDirectory) !== rootDirectory || rootDirectory === sep) {
    throw new Error("Skill Bundle materialization root is invalid.");
  }
  mkdirSync(rootDirectory, { recursive: true, mode: 0o755 });
  const sessionRoot = mkdtempSync(join(rootDirectory, ".runtime-"));
  const generated: string[] = [sessionRoot];
  const environment: Record<string, string> = {};
  const environmentNames = new Set<string>();
  try {
    for (const item of materialization.skills) {
      const binding = bindings.find((candidate) => candidate.resourceId === item.resourceId);
      if (!binding || binding.version !== item.version || binding.digest !== item.digest) {
        throw new Error("Skill Bundle materialization identity does not match the manifest.");
      }
      const environmentName = skillEnvironmentName(item.resourceId);
      if (environmentNames.has(environmentName)) {
        throw new Error("Skill Bundle materialization environment references collide.");
      }
      environmentNames.add(environmentName);
      const expectedRoot = join(sessionRoot, item.resourceId);
      if (existsSync(expectedRoot)) throw new Error("Skill Bundle mount already exists.");
      const staging = mkdtempSync(join(sessionRoot, `.staging-${item.resourceId}-`));
      try {
        materializeBundle(item, staging);
        renameSync(staging, expectedRoot);
      } catch (error) {
        rmSync(staging, { recursive: true, force: true });
        throw error;
      }
      environment[environmentName] = expectedRoot;
    }
    chmodSync(sessionRoot, 0o555);
  } catch (error) {
    removeGeneratedTree(sessionRoot);
    throw error;
  }
  return Object.freeze({
    environment: Object.freeze(environment),
    close() {
      for (const path of generated) removeGeneratedTree(path);
    },
  });
}

function materializeBundle(item: SkillMaterialization, root: string): void {
  if (!item.bundle || !item.signature || !item.publicKey || !item.signingKeyId)
    throw new Error("Skill Bundle signature is missing.");
  const bundle = decode(item.bundle, MAX_BUNDLE_BYTES);
  const signature = decode(item.signature, 256);
  const publicKey = decode(item.publicKey, 256);
  const digest = `sha256:${createHash("sha256").update(bundle).digest("hex")}`;
  if (digest !== item.digest) throw new Error("Skill Bundle digest verification failed.");
  let key;
  try {
    key = createPublicKey({ key: publicKey, format: "der", type: "spki" });
  } catch {
    throw new Error("Skill Bundle signing key is invalid.");
  }
  if (!verify(null, bundle, key, signature))
    throw new Error("Skill Bundle signature verification failed.");
  let parsed: unknown;
  try {
    parsed = JSON.parse(bundle.toString("utf8"));
  } catch {
    throw new Error("Skill Bundle content is invalid.");
  }
  if (
    !isRecord(parsed) ||
    !onlyKeys(parsed, ["version", "files"]) ||
    parsed.version !== 1 ||
    !Array.isArray(parsed.files) ||
    parsed.files.length === 0 ||
    parsed.files.length > MAX_BUNDLE_FILES
  ) {
    throw new Error("Skill Bundle content shape is invalid.");
  }
  const paths = new Set<string>();
  let skillFileCount = 0;
  for (const file of parsed.files) {
    if (
      !isRecord(file) ||
      !onlyKeys(file, ["path", "content"]) ||
      typeof file.path !== "string" ||
      typeof file.content !== "string" ||
      !/^[A-Za-z0-9.][A-Za-z0-9._/-]{0,255}$/u.test(file.path) ||
      file.path.includes("\\") ||
      file.path.split("/").some((part) => part === ".." || part === ".") ||
      paths.has(file.path)
    ) {
      throw new Error("Skill Bundle file path is invalid.");
    }
    if (file.path !== ".claude-plugin/plugin.json" && !file.path.startsWith("skills/")) {
      throw new Error("Skill Bundle contains an unsupported plugin surface.");
    }
    if (file.path === "SKILL.md" || file.path.endsWith("/SKILL.md")) skillFileCount += 1;
    const target = resolve(root, file.path);
    if (!target.startsWith(`${resolve(root)}${sep}`))
      throw new Error("Skill Bundle file path escapes its root.");
    const content = decode(file.content, MAX_BUNDLE_BYTES);
    mkdirSync(dirname(target), { recursive: true, mode: 0o755 });
    writeFileSync(target, content, { mode: 0o444, flag: "wx" });
    chmodSync(target, 0o444);
    paths.add(file.path);
  }
  if (skillFileCount === 0) throw new Error("Skill Bundle does not contain a SKILL.md file.");
  makeTreeReadOnly(root);
  validateExistingSkillTree(root);
}

function makeTreeReadOnly(directory: string): void {
  for (const entry of readdirSync(directory, { withFileTypes: true })) {
    const path = join(directory, entry.name);
    if (entry.isDirectory()) makeTreeReadOnly(path);
    else if (!entry.isFile()) throw new Error("Skill Bundle contains a non-regular entry.");
  }
  chmodSync(directory, 0o555);
}

function removeGeneratedTree(root: string): void {
  if (!existsSync(root)) return;
  const makeWritable = (path: string): void => {
    const stat = lstatSync(path);
    if (stat.isDirectory()) {
      chmodSync(path, 0o700);
      for (const entry of readdirSync(path)) makeWritable(join(path, entry));
    } else chmodSync(path, 0o600);
  };
  makeWritable(root);
  rmSync(root, { recursive: true, force: true });
}

function validateExistingSkillTree(root: string): void {
  const realRoot = resolve(root);
  const info = lstatSync(realRoot);
  if (!info.isDirectory() || (info.mode & 0o222) !== 0 || realRoot !== root)
    throw new Error("Skill Bundle mount is not immutable.");
  let bytes = 0;
  const visit = (directory: string): void => {
    for (const entry of readdirSync(directory, { withFileTypes: true })) {
      const path = join(directory, entry.name);
      const stat = lstatSync(path);
      if (entry.isSymbolicLink() || (stat.mode & 0o222) !== 0)
        throw new Error("Skill Bundle contains a writable or linked entry.");
      if (stat.isDirectory()) visit(path);
      else if (stat.isFile()) {
        bytes += stat.size;
        if (bytes > MAX_SKILL_TREE_BYTES) throw new Error("Skill Bundle exceeds the size limit.");
      } else throw new Error("Skill Bundle contains a non-regular entry.");
    }
  };
  visit(realRoot);
}

function decode(value: string, maximum: number): Buffer {
  if (!/^[A-Za-z0-9_-]+$/u.test(value)) throw new Error("Skill Bundle encoding is invalid.");
  const bytes = Buffer.from(value, "base64url");
  if (bytes.length === 0 || bytes.length > maximum)
    throw new Error("Skill Bundle size is invalid.");
  return bytes;
}

function skillEnvironmentName(resourceId: string): string {
  return `CLOUD_AGENT_SKILL_BUNDLE_${resourceId.replaceAll(/[^A-Za-z0-9]/gu, "_").toUpperCase()}_ROOT`;
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return value !== null && typeof value === "object" && !Array.isArray(value);
}

function onlyKeys(value: Record<string, unknown>, allowed: ReadonlyArray<string>): boolean {
  const keys = Object.keys(value);
  return keys.length === allowed.length && keys.every((key) => allowed.includes(key));
}
