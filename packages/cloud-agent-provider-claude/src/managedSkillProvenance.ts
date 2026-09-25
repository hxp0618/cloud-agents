import {
  closeSync,
  constants,
  fstatSync,
  lstatSync,
  openSync,
  readFileSync,
  readdirSync,
  realpathSync,
} from "node:fs";
import { dirname, join, relative, resolve } from "node:path";

const MAX_PLUGIN_MANIFEST_BYTES = 64 * 1024;
const MAX_SKILL_MANIFEST_BYTES = 64 * 1024;
const MAX_SKILLS_PER_BUNDLE = 128;
const COMPONENT_NAME = /^[A-Za-z][A-Za-z0-9._-]{0,127}$/u;
const YAML_NON_STRING_SCALARS = /^(?:true|false|null|yes|no|on|off|~)$/iu;
const ALLOWED_PLUGIN_METADATA_KEYS = new Set([
  "name",
  "version",
  "description",
  "author",
  "homepage",
  "repository",
  "license",
  "keywords",
]);

export type ManagedSkillBundle = Readonly<{
  directory: string;
  resourceId: string;
}>;

export function managedSkillResourceIdsByQualifiedName(
  bundles: ReadonlyArray<ManagedSkillBundle>,
): Readonly<Record<string, string>> {
  const resourceIds = new Map<string, string>();
  const ambiguousNames = new Set<string>();

  for (const bundle of bundles) {
    const root = safeDirectory(bundle.directory, "Skill Bundle root");
    assertClosedBundleRoot(root);
    const pluginDirectory = optionalSafeDirectory(
      root,
      join(root, ".claude-plugin"),
      "plugin metadata directory",
    );
    if (!pluginDirectory) continue;
    assertClosedPluginMetadataDirectory(pluginDirectory);
    const pluginManifest = safeMetadataFile(
      root,
      join(pluginDirectory, "plugin.json"),
      MAX_PLUGIN_MANIFEST_BYTES,
    );
    const pluginName = pluginManifest ? pluginNameFromManifest(pluginManifest) : undefined;
    if (!pluginName) continue;

    const skillsDirectory = optionalSafeDirectory(root, join(root, "skills"), "skills directory");
    if (!skillsDirectory) continue;
    const entries = readdirSync(skillsDirectory, { withFileTypes: true });
    if (entries.length > MAX_SKILLS_PER_BUNDLE) {
      throw new Error("Skill Bundle contains too many skill entries.");
    }

    for (const entry of entries) {
      if (entry.isSymbolicLink())
        throw new Error("Skill Bundle skill entry must not be a symlink.");
      if (!entry.isDirectory()) {
        throw new Error("Skill Bundle skills entries must be real directories.");
      }
      const skillDirectory = safeDirectoryWithin(
        root,
        join(skillsDirectory, entry.name),
        "skill directory",
      );
      const skillManifest = safeMetadataFile(
        root,
        join(skillDirectory, "SKILL.md"),
        MAX_SKILL_MANIFEST_BYTES,
      );
      const skillName = skillManifest ? skillNameFromFrontmatter(skillManifest) : undefined;
      if (!skillName) continue;
      const qualifiedName = `${pluginName}:${skillName}`;
      if (ambiguousNames.has(qualifiedName)) continue;
      if (resourceIds.has(qualifiedName)) {
        resourceIds.delete(qualifiedName);
        ambiguousNames.add(qualifiedName);
        continue;
      }
      resourceIds.set(qualifiedName, bundle.resourceId);
    }
  }

  return Object.fromEntries(resourceIds);
}

function pluginNameFromManifest(contents: string): string | undefined {
  let parsed: unknown;
  try {
    parsed = JSON.parse(contents);
  } catch {
    return undefined;
  }
  if (!parsed || typeof parsed !== "object" || Array.isArray(parsed)) return undefined;
  const manifest = parsed as Record<string, unknown>;
  const unsupportedKey = Object.keys(manifest).find(
    (key) => !ALLOWED_PLUGIN_METADATA_KEYS.has(key),
  );
  if (unsupportedKey) {
    throw new Error(`Managed Skill plugin manifest contains unsupported field ${unsupportedKey}.`);
  }
  const name = manifest.name;
  return typeof name === "string" && validComponentName(name) ? name : undefined;
}

function skillNameFromFrontmatter(contents: string): string | undefined {
  const frontmatter = contents.match(/^---\r?\n([\s\S]*?)\r?\n---(?:\r?\n|$)/u)?.[1];
  if (frontmatter === undefined) return undefined;
  // The pinned SDK does not export its YAML parser. Accept only an unambiguous
  // top-level scalar subset; unsupported YAML is excluded from the exact managed
  // Skill allowlist and receives no provenance ID.
  const values = new Map<string, string>();
  for (const line of frontmatter.split(/\r?\n/u)) {
    if (!line) continue;
    const field = line.match(
      /^([A-Za-z][A-Za-z0-9_-]*):[ \t]+([A-Za-z0-9][A-Za-z0-9 ._/-]{0,1023})[ \t]*$/u,
    );
    if (!field?.[1] || field[2] === undefined || values.has(field[1])) return undefined;
    values.set(field[1], field[2].trimEnd());
  }
  const name = values.get("name");
  return name && validComponentName(name) ? name : undefined;
}

function validComponentName(value: string): boolean {
  return COMPONENT_NAME.test(value) && !YAML_NON_STRING_SCALARS.test(value);
}

function safeDirectory(path: string, label: string): string {
  const status = lstatSync(path);
  if (status.isSymbolicLink() || !status.isDirectory()) {
    throw new Error(`${label} must be a real directory.`);
  }
  return realpathSync(path);
}

function assertClosedBundleRoot(root: string): void {
  for (const entry of readdirSync(root, { withFileTypes: true })) {
    if (entry.name !== ".claude-plugin" && entry.name !== "skills") {
      throw new Error(`Skill Bundle root contains unsupported entry ${entry.name}.`);
    }
    if (entry.isSymbolicLink() || !entry.isDirectory()) {
      throw new Error("Skill Bundle root entries must be real directories.");
    }
  }
}

function assertClosedPluginMetadataDirectory(directory: string): void {
  for (const entry of readdirSync(directory, { withFileTypes: true })) {
    if (entry.name !== "plugin.json" || entry.isSymbolicLink() || !entry.isFile()) {
      throw new Error("Skill Bundle .claude-plugin may contain only a regular plugin.json file.");
    }
  }
}

function safeDirectoryWithin(root: string, path: string, label: string): string {
  const directory = safeDirectory(path, label);
  assertWithin(root, directory);
  return directory;
}

function optionalSafeDirectory(root: string, path: string, label: string): string | undefined {
  try {
    return safeDirectoryWithin(root, path, label);
  } catch (error) {
    if (isMissingPath(error)) return undefined;
    throw error;
  }
}

function safeMetadataFile(root: string, path: string, maxBytes: number): string | undefined {
  let status;
  try {
    status = lstatSync(path);
  } catch (error) {
    if (isMissingPath(error)) return undefined;
    throw error;
  }
  if (status.isSymbolicLink() || !status.isFile()) {
    throw new Error("Skill Bundle metadata must be a regular file, not a symlink.");
  }
  const realPath = realpathSync(path);
  assertWithin(root, realPath);
  assertWithin(root, realpathSync(dirname(path)));

  const descriptor = openSync(realPath, constants.O_RDONLY | constants.O_NOFOLLOW);
  try {
    const openedStatus = fstatSync(descriptor);
    if (!openedStatus.isFile() || openedStatus.size > maxBytes) {
      throw new Error("Skill Bundle metadata size is invalid.");
    }
    return readFileSync(descriptor, "utf8");
  } finally {
    closeSync(descriptor);
  }
}

function assertWithin(root: string, candidate: string): void {
  const pathFromRoot = relative(resolve(root), resolve(candidate));
  if (pathFromRoot === "" || (!pathFromRoot.startsWith("..") && !pathFromRoot.startsWith("/"))) {
    return;
  }
  throw new Error("Skill Bundle metadata escapes its verified root.");
}

function isMissingPath(error: unknown): boolean {
  return (
    error instanceof Error && "code" in error && (error as NodeJS.ErrnoException).code === "ENOENT"
  );
}
