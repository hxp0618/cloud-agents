import { mkdirSync, mkdtempSync, realpathSync, rmSync, symlinkSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { describe, expect, it } from "vitest";

import type { RunnerMessage } from "../src/internalExecution";
import {
  WorkspaceGeneratedFileCollector,
  workspaceGeneratedFileRelativePath,
} from "../src/workspaceGeneratedFiles";

describe("Workspace generated files", () => {
  it("emits each Provider-observed regular file once without exposing physical paths", async () => {
    const workspace = mkdtempSync(join(tmpdir(), "cloud-agents-generated-files-"));
    const outside = mkdtempSync(join(tmpdir(), "cloud-agents-generated-files-outside-"));
    const messages: RunnerMessage[] = [];
    try {
      mkdirSync(join(workspace, "reports"), { recursive: true });
      writeFileSync(join(workspace, "reports", "result.txt"), "ready\n");
      writeFileSync(join(outside, "secret.txt"), "secret\n");
      symlinkSync(join(outside, "secret.txt"), join(workspace, "linked-secret.txt"));
      mkdirSync(join(workspace, ".git"), { recursive: true });
      writeFileSync(join(workspace, ".git", "index"), "internal\n");

      const collector = new WorkspaceGeneratedFileCollector({
        workspaceDirectory: workspace,
        provider: "codex",
        emit: (message) => messages.push(message),
      });
      collector.observe(realpathSync(join(workspace, "reports", "result.txt")));
      collector.observe("reports/result.txt");
      collector.observe(join(workspace, "linked-secret.txt"));
      collector.observe(join(workspace, ".git", "index"));
      collector.observe(join(outside, "secret.txt"));
      collector.observe("reports/missing.txt");

      await collector.flush();
      await collector.flush();

      expect(messages).toEqual([
        {
          type: "artifact",
          artifact: {
            path: join("reports", "result.txt"),
            kind: "generated_file",
            contentType: "application/octet-stream",
            sourceRoot: "workspace",
          },
        },
        {
          type: "event",
          eventType: "runtime.provider.warning",
          payload: {
            provider: "codex",
            message:
              "Generated-file candidates rejected: invalid_path=1, outside_workspace=1, missing=1, symlink=1",
          },
        },
      ]);
      expect(JSON.stringify(messages)).not.toContain(workspace);
      expect(JSON.stringify(messages)).not.toContain(outside);
    } finally {
      rmSync(workspace, { recursive: true, force: true });
      rmSync(outside, { recursive: true, force: true });
    }
  });

  it("aggregates missing and invalid observations without retaining paths", async () => {
    const workspace = mkdtempSync(join(tmpdir(), "cloud-agents-generated-files-"));
    const outside = mkdtempSync(join(tmpdir(), "cloud-agents-generated-files-outside-"));
    const messages: RunnerMessage[] = [];
    try {
      const collector = new WorkspaceGeneratedFileCollector({
        workspaceDirectory: workspace,
        provider: "claudeAgent",
        emit: (message) => messages.push(message),
      });
      collector.observe(undefined);
      collector.observe(undefined);
      collector.observe("bad\nname.txt");
      collector.observe("bad\nname.txt");
      collector.observe(join(outside, "secret.txt"));
      collector.observe(join(outside, "secret.txt"));

      await collector.flush();

      expect(messages).toEqual([
        {
          type: "event",
          eventType: "runtime.provider.warning",
          payload: {
            provider: "claudeAgent",
            message:
              "Generated-file candidates rejected: missing_path=1, invalid_path=1, outside_workspace=1",
          },
        },
      ]);
      expect(JSON.stringify(messages)).not.toContain(workspace);
      expect(JSON.stringify(messages)).not.toContain(outside);
    } finally {
      rmSync(workspace, { recursive: true, force: true });
      rmSync(outside, { recursive: true, force: true });
    }
  });

  it("does not warn for a valid file when no candidate is rejected", async () => {
    const workspace = mkdtempSync(join(tmpdir(), "cloud-agents-generated-files-"));
    const messages: RunnerMessage[] = [];
    try {
      writeFileSync(join(workspace, "result.txt"), "ready\n");
      const collector = new WorkspaceGeneratedFileCollector({
        workspaceDirectory: workspace,
        provider: "pi",
        emit: (message) => messages.push(message),
      });
      collector.observe("result.txt");
      await collector.flush();

      expect(messages).toEqual([
        {
          type: "artifact",
          artifact: {
            path: "result.txt",
            kind: "generated_file",
            contentType: "application/octet-stream",
            sourceRoot: "workspace",
          },
        },
      ]);
    } finally {
      rmSync(workspace, { recursive: true, force: true });
    }
  });

  it("normalizes only bounded Workspace-relative non-VCS paths", () => {
    const workspace = join(tmpdir(), "cloud-agents-generated-files-root");

    expect(workspaceGeneratedFileRelativePath(workspace, join(workspace, "nested", "a.txt"))).toBe(
      join("nested", "a.txt"),
    );
    expect(workspaceGeneratedFileRelativePath(workspace, "nested/a.txt")).toBe(
      join("nested", "a.txt"),
    );
    expect(workspaceGeneratedFileRelativePath(workspace, "../escape.txt")).toBeUndefined();
    expect(workspaceGeneratedFileRelativePath(workspace, ".git/index")).toBeUndefined();
    expect(workspaceGeneratedFileRelativePath(workspace, "bad\nname.txt")).toBeUndefined();
  });
});
