import { mkdirSync, mkdtempSync, rmSync, symlinkSync } from "node:fs";
import { tmpdir } from "node:os";
import { resolve } from "node:path";
import { afterEach, describe, expect, it } from "vitest";
import {
  frameReplayReport,
  parseReplayReportFrame,
  requireEmptyReplayDirectory,
  requireFreshReplayPath,
} from "./generator-replay-path-authority";

const temporaryRoots: string[] = [];

afterEach(() => {
  for (const root of temporaryRoots.splice(0)) rmSync(root, { recursive: true, force: true });
});

describe("generator replay path authority", () => {
  it("rejects dangling symlinks as occupied fresh paths", () => {
    const root = mkdtempSync(resolve(tmpdir(), "generator-replay-authority-test-"));
    temporaryRoots.push(root);
    const authority = resolve(root, "authority");
    mkdirSync(authority);
    const path = resolve(authority, "home-a");
    symlinkSync("missing", path);
    expect(() => requireFreshReplayPath("HOME", path, authority, "home-a")).toThrow(
      /fresh absent path/u,
    );
  });

  it("rejects symlinked or non-empty temporary directories", () => {
    const root = mkdtempSync(resolve(tmpdir(), "generator-replay-authority-test-"));
    temporaryRoots.push(root);
    const authority = resolve(root, "authority");
    mkdirSync(authority);
    const path = resolve(authority, "tmp-a");
    mkdirSync(path);
    symlinkSync("tmp-a", resolve(authority, "tmp-link"));
    expect(() =>
      requireEmptyReplayDirectory("TMPDIR", resolve(authority, "tmp-link"), authority, "tmp-link"),
    ).toThrow(/empty regular directory/u);
  });

  it("rejects raw trailing bytes after a framed report", () => {
    const frame = frameReplayReport({ formatVersion: "test/v1" });
    expect(() => parseReplayReportFrame(`${frame}\n`)).toThrow(/size|trailing/u);
  });
});
