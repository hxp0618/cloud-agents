import { createHash } from "node:crypto";
import { realpathSync } from "node:fs";
import { lstat } from "node:fs/promises";
import { isAbsolute, join, relative, resolve, sep } from "node:path";

import type { RunnerMessage } from "./internalExecution";

const GENERATED_FILE_CONTENT_TYPE = "application/octet-stream";
const MAX_GENERATED_FILE_PATH_BYTES = 4 * 1024;
const MAX_TRACKED_GENERATED_FILES = 256;
const MAX_EMITTED_GENERATED_FILES = 64;
const VCS_METADATA_SEGMENTS = new Set([".git", ".hg", ".svn"]);
const GENERATED_FILE_REJECTION_REASONS = [
  "missing_path",
  "invalid_path",
  "outside_workspace",
  "missing",
  "symlink",
  "not_regular",
] as const;

type GeneratedFileRejectionReason = (typeof GENERATED_FILE_REJECTION_REASONS)[number];

type WorkspaceGeneratedFileCollectorOptions = {
  workspaceDirectory: string;
  provider: string;
  emit: (message: RunnerMessage) => void;
};

export class WorkspaceGeneratedFileCollector {
  private readonly workspaceDirectory: string;
  private readonly workspaceDirectoryAliases: ReadonlyArray<string>;
  private readonly candidates = new Set<string>();
  private readonly rejectionKeys = new Set<string>();
  private readonly rejectionCounts = new Map<GeneratedFileRejectionReason, number>();
  private overflowed = false;
  private flushed = false;

  constructor(private readonly options: WorkspaceGeneratedFileCollectorOptions) {
    const configuredDirectory = resolve(options.workspaceDirectory);
    let canonicalDirectory = configuredDirectory;
    try {
      canonicalDirectory = realpathSync(configuredDirectory);
    } catch {
      // Agentd owns the authoritative Workspace open; missing roots simply
      // produce no standalone candidates in this Provider Host process.
    }
    this.workspaceDirectory = canonicalDirectory;
    this.workspaceDirectoryAliases = [...new Set([configuredDirectory, canonicalDirectory])];
  }

  observe(candidate: unknown): void {
    const resolution = this.resolveRelativePathWithReason(candidate);
    const relativePath = resolution.relativePath;
    if (!relativePath) {
      if (resolution.rejection) {
        this.recordRejection(resolution.rejection.reason, resolution.rejection.key);
      }
      return;
    }
    if (this.candidates.has(relativePath)) return;
    if (this.candidates.size >= MAX_TRACKED_GENERATED_FILES) {
      this.overflowed = true;
      return;
    }
    this.candidates.add(relativePath);
  }

  remove(candidate: unknown): void {
    const relativePath = this.resolveRelativePath(candidate);
    if (relativePath) this.candidates.delete(relativePath);
  }

  async flush(): Promise<void> {
    if (this.flushed) return;
    this.flushed = true;

    const safeCandidates: string[] = [];
    for (const relativePath of [...this.candidates].sort()) {
      const result = await checkWorkspaceGeneratedFile(this.workspaceDirectory, relativePath);
      if (result === "safe") {
        safeCandidates.push(relativePath);
      } else {
        this.recordRejection(result, relativePath);
      }
    }

    const emittedCandidates = safeCandidates.slice(0, MAX_EMITTED_GENERATED_FILES);
    for (const path of emittedCandidates) {
      this.options.emit({
        type: "artifact",
        artifact: {
          path,
          kind: "generated_file",
          contentType: GENERATED_FILE_CONTENT_TYPE,
          sourceRoot: "workspace",
        },
      });
    }

    if (this.overflowed || safeCandidates.length > emittedCandidates.length) {
      this.options.emit({
        type: "event",
        eventType: "runtime.provider.warning",
        payload: {
          provider: this.options.provider,
          message:
            `Provider reported more than ${MAX_EMITTED_GENERATED_FILES} durable Workspace file changes; ` +
            "Cloud Agents limited standalone generated-file Artifacts while preserving the complete Workspace through its Checkpoint.",
        },
      });
    }
    this.emitRejectionWarning();
  }

  resolveRelativePath(candidate: unknown): string | undefined {
    return this.resolveRelativePathWithReason(candidate).relativePath;
  }

  private resolveRelativePathWithReason(candidate: unknown): {
    relativePath?: string;
    rejection?: {
      reason: Extract<
        GeneratedFileRejectionReason,
        "missing_path" | "invalid_path" | "outside_workspace"
      >;
      key: string;
    };
  } {
    for (const workspaceDirectory of this.workspaceDirectoryAliases) {
      const relativePath = workspaceGeneratedFileRelativePath(workspaceDirectory, candidate);
      if (relativePath) return { relativePath };
    }
    if (typeof candidate === "string") {
      try {
        const configuredDirectory = this.workspaceDirectoryAliases[0] ?? this.workspaceDirectory;
        const absoluteCandidate = isAbsolute(candidate)
          ? candidate
          : resolve(configuredDirectory, candidate);
        const canonicalCandidate = realpathSync(absoluteCandidate);
        const relativePath = workspaceGeneratedFileRelativePath(
          this.workspaceDirectory,
          canonicalCandidate,
        );
        if (relativePath) return { relativePath };
      } catch {
        // The rejection is reported below without retaining or exposing the path.
      }
    }
    const reason = classifyUnresolvedCandidate(this.workspaceDirectoryAliases, candidate);
    return {
      rejection: {
        reason,
        key: opaqueRejectionKey(candidate),
      },
    };
  }

  private recordRejection(reason: GeneratedFileRejectionReason, key: string): void {
    const rejectionKey = `${reason}:${key}`;
    if (this.rejectionKeys.has(rejectionKey)) return;
    if (this.rejectionKeys.size >= MAX_TRACKED_GENERATED_FILES) return;
    this.rejectionKeys.add(rejectionKey);
    this.rejectionCounts.set(reason, (this.rejectionCounts.get(reason) ?? 0) + 1);
  }

  private emitRejectionWarning(): void {
    if (this.rejectionCounts.size === 0) return;
    const details = GENERATED_FILE_REJECTION_REASONS.flatMap((reason) => {
      const count = this.rejectionCounts.get(reason);
      return count === undefined ? [] : [`${reason}=${count}`];
    });
    this.options.emit({
      type: "event",
      eventType: "runtime.provider.warning",
      payload: {
        provider: this.options.provider,
        message: `Generated-file candidates rejected: ${details.join(", ")}`,
      },
    });
  }
}

export function workspaceGeneratedFileRelativePath(
  workspaceDirectory: string,
  candidate: unknown,
): string | undefined {
  if (
    typeof candidate !== "string" ||
    candidate.trim() === "" ||
    Buffer.byteLength(candidate) > MAX_GENERATED_FILE_PATH_BYTES ||
    /[\u0000-\u001f\u007f]/u.test(candidate)
  ) {
    return undefined;
  }

  let relativePath: string;
  try {
    const workspace = resolve(workspaceDirectory);
    const absoluteCandidate = isAbsolute(candidate)
      ? resolve(candidate)
      : resolve(workspace, candidate);
    relativePath = relative(workspace, absoluteCandidate);
  } catch {
    return undefined;
  }

  if (
    relativePath === "" ||
    relativePath === "." ||
    relativePath === ".." ||
    isAbsolute(relativePath) ||
    relativePath.startsWith(`..${sep}`) ||
    Buffer.byteLength(relativePath) > MAX_GENERATED_FILE_PATH_BYTES
  ) {
    return undefined;
  }
  if (relativePath.split(sep).some((segment) => VCS_METADATA_SEGMENTS.has(segment.toLowerCase()))) {
    return undefined;
  }
  return relativePath;
}

async function checkWorkspaceGeneratedFile(
  workspaceDirectory: string,
  relativePath: string,
): Promise<"safe" | "missing" | "symlink" | "not_regular"> {
  const segments = relativePath.split(sep);
  let current = workspaceDirectory;
  for (const [index, segment] of segments.entries()) {
    current = join(current, segment);
    let info;
    try {
      info = await lstat(current);
    } catch {
      return "missing";
    }
    if (info.isSymbolicLink()) return "symlink";
    if (index === segments.length - 1) return info.isFile() ? "safe" : "not_regular";
    if (!info.isDirectory()) return "not_regular";
  }
  return "not_regular";
}

function classifyUnresolvedCandidate(
  workspaceDirectoryAliases: ReadonlyArray<string>,
  candidate: unknown,
): Extract<GeneratedFileRejectionReason, "missing_path" | "invalid_path" | "outside_workspace"> {
  if (candidate === undefined || candidate === null) return "missing_path";
  if (typeof candidate !== "string") return "invalid_path";
  if (candidate.trim() === "") return "missing_path";
  if (
    Buffer.byteLength(candidate) > MAX_GENERATED_FILE_PATH_BYTES ||
    /[\u0000-\u001f\u007f]/u.test(candidate)
  ) {
    return "invalid_path";
  }
  let hasInsideAlias = false;
  let hasOutsideAlias = false;
  for (const workspaceDirectory of workspaceDirectoryAliases) {
    try {
      const workspace = resolve(workspaceDirectory);
      const absoluteCandidate = isAbsolute(candidate)
        ? resolve(candidate)
        : resolve(workspace, candidate);
      const relativePath = relative(workspace, absoluteCandidate);
      const outside =
        relativePath !== "" &&
        relativePath !== "." &&
        (isAbsolute(relativePath) || relativePath === ".." || relativePath.startsWith(`..${sep}`));
      if (outside) hasOutsideAlias = true;
      else hasInsideAlias = true;
    } catch {
      return "invalid_path";
    }
  }
  return !hasInsideAlias && hasOutsideAlias ? "outside_workspace" : "invalid_path";
}

function opaqueRejectionKey(candidate: unknown): string {
  if (typeof candidate !== "string") {
    return `type:${candidate === null ? "null" : typeof candidate}`;
  }
  return `sha256:${createHash("sha256").update(candidate).digest("hex")}`;
}
