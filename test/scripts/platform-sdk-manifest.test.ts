import { createHash } from "node:crypto";
import { mkdirSync, mkdtempSync, readFileSync, rmSync, symlinkSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { describe, expect, it } from "vitest";

import {
  generatedFileRecord,
  normalizedFileManifestDigest,
  outputTreeDigest,
  regularFileDigest,
  writeSDKFiles,
} from "../../scripts/lib/platform-sdk-manifest";

describe("SDK generated manifest helpers", () => {
  it("binds path, digest, and byte size deterministically", () => {
    const record = generatedFileRecord("sdk/example.txt", "alpha\n");
    expect(record).toEqual({
      path: "sdk/example.txt",
      sha256: `sha256:${createHash("sha256").update("alpha\n").digest("hex")}`,
      sizeBytes: 6,
    });
    expect(outputTreeDigest([record])).toBe(outputTreeDigest([record]));
    const other = generatedFileRecord("sdk/other.txt", "beta\n");
    expect(outputTreeDigest([record, other])).toBe(outputTreeDigest([other, record]));
  });

  it("rejects escaped and symlinked inputs and preflights output paths before writing", () => {
    const temporary = mkdtempSync(join(tmpdir(), "cloud-agents-sdk-manifest-"));
    const root = join(temporary, "root");
    const outside = join(temporary, "outside");
    mkdirSync(root);
    mkdirSync(outside);
    writeFileSync(join(outside, "input.txt"), "outside");
    writeFileSync(join(root, "output.txt"), "original");
    symlinkSync(outside, join(root, "linked"));
    symlinkSync(join(outside, "input.txt"), join(root, "alias.txt"));
    symlinkSync(join(outside, "missing.txt"), join(root, "dangling.txt"));
    try {
      for (const path of ["../outside/input.txt", "linked/input.txt", "alias.txt"]) {
        expect(() => normalizedFileManifestDigest(root, [path])).toThrow();
        expect(() => regularFileDigest(root, path)).toThrow();
      }
      for (const path of ["linked/input.txt", "alias.txt", "dangling.txt"]) {
        expect(() =>
          writeSDKFiles(root, [
            { path: "output.txt", bytes: "changed" },
            { path, bytes: "changed" },
          ]),
        ).toThrow();
        expect(readFileSync(join(root, "output.txt"), "utf8")).toBe("original");
      }
      expect(readFileSync(join(outside, "input.txt"), "utf8")).toBe("outside");
      writeSDKFiles(root, [{ path: "new/output.txt", bytes: "created" }]);
      expect(readFileSync(join(root, "new/output.txt"), "utf8")).toBe("created");
    } finally {
      rmSync(temporary, { recursive: true, force: true });
    }
  });
});
