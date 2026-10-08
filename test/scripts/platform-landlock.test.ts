import { spawnSync } from "node:child_process";
import { createHash } from "node:crypto";
import { gzipSync } from "node:zlib";
import { describe, expect, it } from "vitest";
import { createDeterministicUstar } from "../../scripts/lib/platform-migration-ustar";
import {
  decodeLandlockPackage,
  lockedLandlockSource,
  parseLandlockLockfile,
  readBoundedLandlockPackage,
} from "../../scripts/lib/platform-landlock";

const name = "@deepseek-ai/node-addon-landlock-run-linux-arm64";
const version = "0.1.1";
function fixture(change?: (binary: Buffer) => void) {
  const binary = Buffer.alloc(120);
  binary.set([0x7f, 0x45, 0x4c, 0x46, 2, 1, 1]);
  binary.writeUInt16LE(2, 16);
  binary.writeUInt16LE(183, 18);
  binary.writeUInt32LE(1, 20);
  binary.writeBigUInt64LE(64n, 32);
  binary.writeUInt16LE(64, 52);
  binary.writeUInt16LE(56, 54);
  binary.writeUInt16LE(1, 56);
  change?.(binary);
  const archive = gzipSync(
    createDeterministicUstar([
      { path: "package/bin/landlock-run", data: binary },
      {
        path: "package/package.json",
        data: Buffer.from(JSON.stringify({ name, version, license: "BSD-3-Clause" })),
      },
      { path: "package/LICENSE", data: Buffer.from("BSD-3-Clause test license") },
    ]),
  );
  const integrity = `sha512-${createHash("sha512").update(archive).digest("base64")}`;
  return { archive, integrity, binary };
}

describe("pinned Landlock release artifacts", () => {
  it("parses the installed JSONC lockfile under the supported Node entrypoint", () => {
    expect(parseLandlockLockfile('{ "packages": { /* pinned */ } }')).toEqual({ packages: {} });
    expect(() => parseLandlockLockfile('{ "packages": ')).toThrow(/not valid JSONC/);
    const result = spawnSync(
      process.execPath,
      [
        "--input-type=module",
        "--eval",
        'import { readFileSync } from "node:fs"; import { parseLandlockLockfile } from "./scripts/lib/platform-landlock.ts"; const lock=parseLandlockLockfile(readFileSync("bun.lock", "utf8")); if (!lock.packages["@deepseek-ai/node-addon-landlock-run-linux-arm64"]) process.exit(1);',
      ],
      { encoding: "utf8" },
    );
    expect(result.stderr).toBe("");
    expect(result.status).toBe(0);
  });

  it("requires the same exact package version and lockfile integrity", () => {
    const { integrity } = fixture();
    const entry = [`${name}@${version}`, "https://registry.npmjs.org/package.tgz", {}, integrity];
    expect(lockedLandlockSource(entry, name, version).integrity).toBe(integrity);
    expect(() => lockedLandlockSource(entry, name, "0.1.2")).toThrow(/exactly pinned/);
    expect(() =>
      lockedLandlockSource([entry[0], "http://untrusted.test/a", {}, integrity], name, version),
    ).toThrow(/exactly pinned/);
  });
  it("retains only the verified matching ELF and upstream license", () => {
    const { archive, integrity, binary } = fixture();
    expect(decodeLandlockPackage(archive, integrity, name, version, "arm64")).toEqual({
      binary,
      license: "BSD-3-Clause test license",
    });
    expect(() => decodeLandlockPackage(archive, integrity, name, version, "x64")).toThrow(
      /architecture/,
    );
    expect(() => decodeLandlockPackage(archive, integrity, name, "0.1.2", "arm64")).toThrow(
      /metadata/,
    );
    expect(() =>
      decodeLandlockPackage(
        Buffer.concat([archive, Buffer.from("tamper")]),
        integrity,
        name,
        version,
        "arm64",
      ),
    ).toThrow(/integrity/);
  });

  it("rejects incomplete or non-executable ELF headers", () => {
    for (const change of [
      (binary: Buffer) => binary.writeUInt16LE(0, 16),
      (binary: Buffer) => binary.writeUInt8(0, 6),
      (binary: Buffer) => binary.writeUInt16LE(2, 56),
    ]) {
      const malformed = fixture(change);
      expect(() =>
        decodeLandlockPackage(malformed.archive, malformed.integrity, name, version, "arm64"),
      ).toThrow(/ELF/);
    }
  });

  it("caps streamed package downloads even without Content-Length", async () => {
    const oversized = new Response(
      new ReadableStream({
        start(controller) {
          controller.enqueue(new Uint8Array(4 * 1024 * 1024));
          controller.enqueue(new Uint8Array(1));
          controller.close();
        },
      }),
    );
    await expect(readBoundedLandlockPackage(oversized)).rejects.toThrow(/download failed/);
    await expect(
      readBoundedLandlockPackage(
        new Response(new Uint8Array([1]), { headers: { "content-length": "not-a-number" } }),
      ),
    ).rejects.toThrow(/download failed/);
    await expect(
      readBoundedLandlockPackage(new Response(new Uint8Array([1, 2, 3]))),
    ).resolves.toEqual(Buffer.from([1, 2, 3]));
  });
});
