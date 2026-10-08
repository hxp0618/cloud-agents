import { createHash } from "node:crypto";
import { execFileSync } from "node:child_process";
import {
  chmodSync,
  lstatSync,
  mkdirSync,
  readFileSync,
  realpathSync,
  writeFileSync,
} from "node:fs";
import { isAbsolute, relative, resolve, sep } from "node:path";

import Ajv2020 from "ajv/dist/2020.js";

export const PUBLIC_EVIDENCE_MIRROR_V1_FORMAT = "cloud-agents-public-evidence-mirror/v1" as const;
export const PUBLIC_EVIDENCE_MIRROR_V1_SCHEMA_PATH =
  "docs/acceptance/public-evidence-mirror-v1.schema.json" as const;
export const PUBLIC_EVIDENCE_MIRROR_V1_MANIFEST_PATH =
  "docs/acceptance/public-evidence-mirror-v1.json" as const;
export const PUBLIC_EVIDENCE_MIRROR_V1_OUTPUT_PATH = "docs/acceptance/phase-1.public.md" as const;

export const PUBLIC_EVIDENCE_MIRROR_V1_SOURCE_PATHS = [
  "docs/acceptance/phase-1.md",
  "docs/plan/cloud-agents-platform/06-status-tracker.md",
] as const;

const SCHEMA_URI =
  "https://schemas.cloud-agents.dev/docs/acceptance/public-evidence-mirror-v1.schema.json" as const;

type FileBinding = Readonly<{
  path: string;
  sha256: string;
  sizeBytes: number;
  gitMode: string;
}>;

type ReplacementId =
  | "host-path"
  | "tmp-artifact"
  | "pod-cidr"
  | "service-cidr"
  | "cluster-cidr-summary"
  | "pod-ip"
  | "service-ip"
  | "vm-ip"
  | "private-ip"
  | "loopback-endpoint"
  | "kubernetes-api-endpoint"
  | "credential-path"
  | "provider-token-env";

export type SanitizedText = Readonly<{
  text: string;
  replacements: Readonly<Record<ReplacementId, number>>;
}>;

export type PublicEvidenceMirrorV1 = Readonly<{
  $schema: typeof SCHEMA_URI;
  formatVersion: typeof PUBLIC_EVIDENCE_MIRROR_V1_FORMAT;
  status: "SANITIZED_PUBLIC_MIRROR";
  sourceInputs: readonly FileBinding[];
  output: FileBinding;
  sanitization: Readonly<{
    profile: "public-evidence-mirror/v1";
    sourcePreserved: true;
    publicLinksNormalized: number;
    replacementRules: readonly Readonly<{
      id: ReplacementId;
      replacement: string;
      count: number;
    }>[];
  }>;
  omittedHistoricalObjects: Readonly<{
    patterns: readonly string[];
    count: number;
    source: "git log --all --diff-filter=D --name-only";
    reason: string;
  }>;
}>;

export type PublicEvidenceMirrorV1Build = Readonly<{
  markdown: string;
  manifest: PublicEvidenceMirrorV1;
}>;

const REPLACEMENT_RULES: readonly Readonly<{
  id: ReplacementId;
  replacement: string;
  pattern: RegExp;
}>[] = [
  {
    id: "host-path",
    replacement: "<host-path>",
    pattern:
      /\/(?:Users|home)\/(?:[A-Za-z0-9._-]+|\[[^\]]+\])(?:\/[A-Za-z0-9.@_:+-]+)*|\/private\/var(?:\/[A-Za-z0-9.@_:+-]+)*/g,
  },
  {
    id: "tmp-artifact",
    replacement: "<evidence-artifact>",
    pattern:
      /(?:\/private|\/tmp)<evidence-artifact>|(?:\/private)?\/tmp(?:\/[A-Za-z0-9._@:+-]+(?:\/[A-Za-z0-9._@:+-]+)*)?/g,
  },
  {
    id: "pod-cidr",
    replacement: "<pod-cidr>",
    pattern: /\b(?:10\.42\.0\.0\/24|10\.42\/24)\b/g,
  },
  {
    id: "service-cidr",
    replacement: "<service-cidr>",
    pattern: /\b(?:10\.43\.0\.0\/16|10\.43\/16)\b/g,
  },
  {
    id: "cluster-cidr-summary",
    replacement: "<cluster-cidr-summary>",
    pattern: /\b10\.42\/10\.43\b/g,
  },
  {
    id: "pod-ip",
    replacement: "<pod-ip>",
    pattern: /\b10\.42\.0\.(?:[1-9]\d?|1\d\d|2[0-4]\d|25[0-5])\b/g,
  },
  {
    id: "service-ip",
    replacement: "<service-ip>",
    pattern: /\b10\.43\.0\.(?:[1-9]\d?|1\d\d|2[0-4]\d|25[0-5])\b/g,
  },
  {
    id: "vm-ip",
    replacement: "<test-vm-ip>",
    pattern: /\b192\.168\.139\.215\b/g,
  },
  {
    id: "private-ip",
    replacement: "<private-ip>",
    pattern:
      /\b(?:10\.(?:[0-9]{1,3}\.){2}[0-9]{1,3}|172\.(?:1[6-9]|2[0-9]|3[0-1])\.[0-9]{1,3}\.[0-9]{1,3}|192\.168\.(?:[0-9]{1,3}\.)[0-9]{1,3})\b/g,
  },
  {
    id: "loopback-endpoint",
    replacement: "<loopback-endpoint>",
    pattern: /\b127\.0\.0\.1(?::\d+)?\b/g,
  },
  {
    id: "kubernetes-api-endpoint",
    replacement: "<kubernetes-api-endpoint>",
    pattern: /(?:https?:\/\/)?k8s\.orb\.local(?::\d+)?/g,
  },
  {
    id: "credential-path",
    replacement: "<credential-path>",
    pattern: /\bprovider-credentials(?:\/[A-Za-z0-9*._-]+)*\/?/g,
  },
  {
    id: "provider-token-env",
    replacement: "<provider-token-env>",
    pattern: /\b(?:ANTHROPIC_API_KEY|ANTHROPIC_AUTH_TOKEN)\b/g,
  },
] as const;

const CREDENTIAL_VALUE_PATTERN =
  /\b(?:sk-[A-Za-z0-9_-]{16,}|sk_[A-Za-z0-9_-]{16,}|gh[pousr]_[A-Za-z0-9]{20,}|xox[baprs]-[A-Za-z0-9-]{20,})\b/g;

const OMITTED_HISTORICAL_OBJECT_PATTERNS = ["*.png", "*.jpg", "*.jpeg", "*.log", "*.html"] as const;
const REVIEWED_HISTORICAL_OBJECT_COUNT = 138 as const;

function omittedHistoricalObjects(
  root: string,
): PublicEvidenceMirrorV1["omittedHistoricalObjects"] {
  const gitDirectory = lstatSync(resolve(root, ".git"), { throwIfNoEntry: false });
  let count: number = REVIEWED_HISTORICAL_OBJECT_COUNT;
  if (gitDirectory !== undefined) {
    let output: string;
    try {
      output = execFileSync(
        "git",
        [
          "-C",
          root,
          "log",
          "--all",
          "--diff-filter=D",
          "--name-only",
          "--format=",
          "--",
          ...OMITTED_HISTORICAL_OBJECT_PATTERNS,
        ],
        { encoding: "utf8", stdio: ["ignore", "pipe", "ignore"] },
      );
    } catch {
      throw new Error(
        "Unable to inspect Git history for omitted public evidence objects; refusing to regenerate the mirror.",
      );
    }
    count = new Set(
      output
        .split("\n")
        .map((path) => path.trim())
        .filter(Boolean),
    ).size;
    if (count !== REVIEWED_HISTORICAL_OBJECT_COUNT) {
      throw new Error(
        `Historical omitted-object count drifted: expected ${REVIEWED_HISTORICAL_OBJECT_COUNT}, found ${count}. Review the public-history boundary before regenerating.`,
      );
    }
  }
  return {
    patterns: OMITTED_HISTORICAL_OBJECT_PATTERNS,
    count,
    source: "git log --all --diff-filter=D --name-only",
    reason:
      "Historical binary and log deletion objects are not restored into the public mirror without an independent review.",
  };
}

function safePath(root: string, path: string): string {
  const rootReal = realpathSync(root);
  if (
    path.length === 0 ||
    isAbsolute(path) ||
    path.includes("\\") ||
    path.split("/").some((segment) => segment.length === 0 || segment === "." || segment === "..")
  ) {
    throw new Error(`Path is not a canonical repository-relative path: ${path}`);
  }
  const absolute = resolve(rootReal, path);
  const relativePath = relative(rootReal, absolute).split(sep).join("/");
  if (relativePath !== path || relativePath === "") {
    throw new Error(`Path escapes repository root: ${path}`);
  }
  let cursor = rootReal;
  for (const [index, segment] of path.split("/").entries()) {
    cursor = resolve(cursor, segment);
    const stat = lstatSync(cursor, { throwIfNoEntry: false });
    if (
      stat === undefined ||
      stat.isSymbolicLink() ||
      (index === path.split("/").length - 1 ? !stat.isFile() : !stat.isDirectory())
    ) {
      throw new Error(`${path} must be a regular file with non-symlink parents.`);
    }
  }
  return absolute;
}

function fileBinding(root: string, path: string): FileBinding {
  const absolute = safePath(root, path);
  const bytes = readFileSync(absolute);
  const stat = lstatSync(absolute);
  return {
    path,
    sha256: `sha256:${createHash("sha256").update(bytes).digest("hex")}`,
    sizeBytes: bytes.byteLength,
    gitMode: `100${(stat.mode & 0o777).toString(8).padStart(3, "0")}`,
  };
}

function assertNoCredentialValue(text: string): void {
  const match = text.match(CREDENTIAL_VALUE_PATTERN);
  if (match !== null) {
    throw new Error("Credential-shaped value found in public evidence source; value withheld.");
  }
}

export function sanitizePublicEvidenceText(text: string): SanitizedText {
  assertNoCredentialValue(text);
  const replacements = Object.fromEntries(REPLACEMENT_RULES.map(({ id }) => [id, 0])) as Record<
    ReplacementId,
    number
  >;
  let sanitized = text;
  for (const rule of REPLACEMENT_RULES) {
    rule.pattern.lastIndex = 0;
    sanitized = sanitized.replace(rule.pattern, () => {
      replacements[rule.id] += 1;
      return rule.replacement;
    });
  }
  return { text: sanitized, replacements };
}

function normalizePublicEvidenceLinks(
  root: string,
  sourcePath: string,
  text: string,
): Readonly<{ text: string; count: number }> {
  let count = 0;
  const rootReal = realpathSync(root);
  const sourceDirectory = resolve(rootReal, sourcePath, "..");
  const outputDirectory = resolve(rootReal, PUBLIC_EVIDENCE_MIRROR_V1_OUTPUT_PATH, "..");
  const normalized = text.replace(/\]\(([^)]+)\)/g, (match, rawTarget: string) => {
    const target = rawTarget.trim();
    const hashIndex = target.indexOf("#");
    const pathPart = hashIndex === -1 ? target : target.slice(0, hashIndex);
    const suffix = hashIndex === -1 ? "" : target.slice(hashIndex);
    if (
      pathPart.length === 0 ||
      pathPart.startsWith("/") ||
      /^(?:https?:|mailto:|codex:|data:)/i.test(pathPart)
    ) {
      return match;
    }
    const targetAbsolute = resolve(sourceDirectory, pathPart);
    const targetRelativeToRoot = relative(rootReal, targetAbsolute).split(sep).join("/");
    if (
      targetRelativeToRoot.startsWith("../") ||
      targetRelativeToRoot === ".." ||
      targetRelativeToRoot.length === 0
    ) {
      return match;
    }
    if (
      targetRelativeToRoot === PUBLIC_EVIDENCE_MIRROR_V1_SOURCE_PATHS[0] ||
      targetRelativeToRoot === PUBLIC_EVIDENCE_MIRROR_V1_OUTPUT_PATH
    ) {
      count += 1;
      return "](#first-stage-acceptance-evidence)";
    }
    if (targetRelativeToRoot === PUBLIC_EVIDENCE_MIRROR_V1_SOURCE_PATHS[1]) {
      count += 1;
      return "](#current-status-tracker)";
    }
    const outputRelative = relative(outputDirectory, targetAbsolute).split(sep).join("/");
    if (outputRelative.length === 0) {
      return match;
    }
    const targetExists = lstatSync(targetAbsolute, { throwIfNoEntry: false }) !== undefined;
    if (!targetExists) {
      return match;
    }
    count += 1;
    return `](${outputRelative}${suffix})`;
  });
  return { text: normalized, count };
}

function canonicalManifestBytes(manifest: PublicEvidenceMirrorV1): Buffer {
  return Buffer.from(`${JSON.stringify(manifest, null, 2)}\n`, "utf8");
}

function validateManifest(
  root: string,
  manifest: unknown,
): asserts manifest is PublicEvidenceMirrorV1 {
  const schema = JSON.parse(
    readFileSync(safePath(root, PUBLIC_EVIDENCE_MIRROR_V1_SCHEMA_PATH), "utf8"),
  );
  const ajv = new Ajv2020({ allErrors: true, strict: false });
  if (!ajv.validate(schema, manifest)) {
    throw new Error(`Public evidence mirror schema validation failed: ${ajv.errorsText()}`);
  }
}

function buildSanitizedMarkdown(root: string): Readonly<{
  markdown: string;
  sourceInputs: readonly FileBinding[];
  replacements: Readonly<Record<ReplacementId, number>>;
  publicLinksNormalized: number;
}> {
  const sourceInputs: FileBinding[] = [];
  const sections: string[] = [];
  let publicLinksNormalized = 0;
  const replacements = Object.fromEntries(REPLACEMENT_RULES.map(({ id }) => [id, 0])) as Record<
    ReplacementId,
    number
  >;
  for (const [index, path] of PUBLIC_EVIDENCE_MIRROR_V1_SOURCE_PATHS.entries()) {
    const absolute = safePath(root, path);
    sourceInputs.push(fileBinding(root, path));
    const sanitized = sanitizePublicEvidenceText(readFileSync(absolute, "utf8"));
    const linked = normalizePublicEvidenceLinks(root, path, sanitized.text);
    publicLinksNormalized += linked.count;
    for (const rule of Object.keys(replacements) as ReplacementId[]) {
      replacements[rule] += sanitized.replacements[rule];
    }
    sections.push(
      `${index === 0 ? "## First-stage acceptance evidence" : "## Current status tracker"}\n\n${linked.text.trimEnd()}`,
    );
  }
  return {
    markdown: [
      "# Cloud Agents public evidence mirror v1",
      "",
      "> Generated from the source documents listed in `public-evidence-mirror-v1.json`. Local paths, runtime endpoints, credential paths, and historical binary objects are projected out; status and verification claims are preserved.",
      "",
      ...sections,
      "",
    ].join("\n"),
    sourceInputs,
    replacements,
    publicLinksNormalized,
  };
}

export function buildPublicEvidenceMirrorV1(root: string): PublicEvidenceMirrorV1Build {
  const { markdown, sourceInputs, replacements, publicLinksNormalized } =
    buildSanitizedMarkdown(root);
  const markdownBytes = Buffer.from(markdown, "utf8");
  const output: FileBinding = {
    path: PUBLIC_EVIDENCE_MIRROR_V1_OUTPUT_PATH,
    sha256: `sha256:${createHash("sha256").update(markdownBytes).digest("hex")}`,
    sizeBytes: markdownBytes.byteLength,
    gitMode: "100644",
  };
  const manifest: PublicEvidenceMirrorV1 = {
    $schema: SCHEMA_URI,
    formatVersion: PUBLIC_EVIDENCE_MIRROR_V1_FORMAT,
    status: "SANITIZED_PUBLIC_MIRROR",
    sourceInputs,
    output,
    sanitization: {
      profile: "public-evidence-mirror/v1",
      sourcePreserved: true,
      publicLinksNormalized,
      replacementRules: REPLACEMENT_RULES.map(({ id, replacement }) => ({
        id,
        replacement,
        count: replacements[id],
      })),
    },
    omittedHistoricalObjects: omittedHistoricalObjects(root),
  };
  validateManifest(root, manifest);
  return { markdown, manifest };
}

export function writePublicEvidenceMirrorV1(root: string): void {
  const result = buildPublicEvidenceMirrorV1(root);
  const markdownPath = safeOutputPath(root, PUBLIC_EVIDENCE_MIRROR_V1_OUTPUT_PATH);
  const manifestPath = safeOutputPath(root, PUBLIC_EVIDENCE_MIRROR_V1_MANIFEST_PATH);
  mkdirSync(resolve(markdownPath, ".."), { recursive: true });
  writeFileSync(markdownPath, result.markdown, { mode: 0o644 });
  writeFileSync(manifestPath, canonicalManifestBytes(result.manifest), { mode: 0o644 });
  chmodSync(markdownPath, 0o644);
  chmodSync(manifestPath, 0o644);
}

function safeOutputPath(root: string, path: string): string {
  const rootReal = realpathSync(root);
  if (
    path.length === 0 ||
    isAbsolute(path) ||
    path.includes("\\") ||
    path.split("/").some((segment) => segment.length === 0 || segment === "." || segment === "..")
  ) {
    throw new Error(`Path is not a canonical repository-relative path: ${path}`);
  }
  const absolute = resolve(rootReal, path);
  const segments = path.split("/");
  let cursor = rootReal;
  for (const segment of segments.slice(0, -1)) {
    cursor = resolve(cursor, segment);
    const stat = lstatSync(cursor, { throwIfNoEntry: false });
    if (stat !== undefined && (stat.isSymbolicLink() || !stat.isDirectory())) {
      throw new Error(`${path} must have regular non-symlink parent directories.`);
    }
  }
  const outputStat = lstatSync(absolute, { throwIfNoEntry: false });
  if (outputStat !== undefined && (outputStat.isSymbolicLink() || !outputStat.isFile())) {
    throw new Error(`${path} must be a regular non-symlink output file.`);
  }
  return absolute;
}

export function assertPublicEvidenceMirrorV1Current(root: string): void {
  const expected = buildPublicEvidenceMirrorV1(root);
  const markdownPath = safePath(root, PUBLIC_EVIDENCE_MIRROR_V1_OUTPUT_PATH);
  const manifestPath = safePath(root, PUBLIC_EVIDENCE_MIRROR_V1_MANIFEST_PATH);
  const actualMarkdown = readFileSync(markdownPath, "utf8");
  const actualManifest = JSON.parse(readFileSync(manifestPath, "utf8")) as unknown;
  validateManifest(root, actualManifest);
  if (actualMarkdown !== expected.markdown) {
    throw new Error("Public evidence mirror Markdown drifted from its source inputs.");
  }
  if (JSON.stringify(actualManifest) !== JSON.stringify(expected.manifest)) {
    throw new Error("Public evidence mirror manifest drifted from its source inputs.");
  }
  const actualOutput = fileBinding(root, PUBLIC_EVIDENCE_MIRROR_V1_OUTPUT_PATH);
  if (
    actualOutput.sha256 !== expected.manifest.output.sha256 ||
    actualOutput.sizeBytes !== expected.manifest.output.sizeBytes ||
    actualOutput.gitMode !== expected.manifest.output.gitMode
  ) {
    throw new Error("Public evidence mirror output digest or mode drifted.");
  }
}
