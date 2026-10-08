import { describe, expect, it } from "vitest";

import {
  decodeExternalConsumerV3Ustar,
  encodeExternalConsumerV3Ustar,
  ExternalConsumerV3UstarError,
} from "../../scripts/lib/platform-g-contract-external-consumer-v3-ustar";

function bytes(value: string): Uint8Array {
  return new TextEncoder().encode(value);
}

function withDuplicateMember(archive: Uint8Array): Uint8Array {
  const firstMember = archive.slice(0, 1024);
  const terminal = archive.slice(-1024);
  return Uint8Array.from([...firstMember, ...firstMember, ...terminal]);
}

describe("G-CONTRACT v3 deterministic USTAR", () => {
  it("sorts paths by UTF-8 bytes and produces byte-identical output", () => {
    const entries = [
      { path: "é.txt", type: "regular" as const, mode: 0o644 as const, data: bytes("e") },
      { path: "z.txt", type: "regular" as const, mode: 0o644 as const, data: bytes("z") },
      { path: "a.txt", type: "regular" as const, mode: 0o644 as const, data: bytes("a") },
      { path: "中.txt", type: "regular" as const, mode: 0o644 as const, data: bytes("中") },
    ];
    const first = encodeExternalConsumerV3Ustar(entries);
    const second = encodeExternalConsumerV3Ustar([...entries].reverse());
    expect(Buffer.from(first).equals(Buffer.from(second))).toBe(true);
    expect(decodeExternalConsumerV3Ustar(first).map((entry) => entry.path)).toEqual([
      "a.txt",
      "z.txt",
      "é.txt",
      "中.txt",
    ]);
  });

  it("encodes directories and regular 0644/0755 members with fixed metadata", () => {
    const archive = encodeExternalConsumerV3Ustar([
      { path: "bin", type: "directory", mode: 0o755 },
      { path: "bin/run", type: "regular", mode: 0o755, data: bytes("#!/bin/sh\n") },
      { path: "README", type: "regular", mode: 0o644, data: bytes("hello") },
    ]);
    const decoded = decodeExternalConsumerV3Ustar(archive);
    expect(
      decoded.map(({ path, type, mode, size, linkTarget }) => ({
        path,
        type,
        mode,
        size,
        linkTarget,
      })),
    ).toEqual([
      { path: "README", type: "regular", mode: 0o644, size: 5, linkTarget: null },
      { path: "bin", type: "directory", mode: 0o755, size: 0, linkTarget: null },
      { path: "bin/run", type: "regular", mode: 0o755, size: 10, linkTarget: null },
    ]);
    expect(decoded[2]?.sha256).toMatch(/^sha256:[0-9a-f]{64}$/u);
    expect(archive.slice(257, 265)).toEqual(bytes("ustar\u000000"));
    expect(archive.slice(265, 345).every((value) => value === 0)).toBe(true);
  });

  it("rejects a checksum-tampered archive", () => {
    const archive = encodeExternalConsumerV3Ustar([
      { path: "file", type: "regular", mode: 0o644, data: bytes("payload") },
    ]);
    const tampered = archive.slice();
    tampered[0] ^= 0x01;
    expect(() => decodeExternalConsumerV3Ustar(tampered)).toThrow(ExternalConsumerV3UstarError);
  });

  it("rejects duplicate paths during encoding and decoding", () => {
    const duplicate = {
      path: "same",
      type: "regular" as const,
      mode: 0o644 as const,
      data: bytes("x"),
    };
    expect(() => encodeExternalConsumerV3Ustar([duplicate, duplicate])).toThrow(
      ExternalConsumerV3UstarError,
    );
    const archive = encodeExternalConsumerV3Ustar([duplicate]);
    expect(() => decodeExternalConsumerV3Ustar(withDuplicateMember(archive))).toThrow(
      ExternalConsumerV3UstarError,
    );
  });

  it.each(["", "/absolute", "../escape", "a/../b", "a//b", "a/", "a\\b", "a\u0000b"])(
    "rejects non-canonical path %j",
    (path) => {
      expect(() =>
        encodeExternalConsumerV3Ustar([{ path, type: "regular", mode: 0o644, data: bytes("x") }]),
      ).toThrow(ExternalConsumerV3UstarError);
    },
  );

  it("rejects symlink and unsupported mode members", () => {
    expect(() =>
      encodeExternalConsumerV3Ustar([
        { path: "link", type: "symlink" as never, mode: 0o777 as never, data: bytes("target") },
      ]),
    ).toThrow(ExternalConsumerV3UstarError);
    expect(() =>
      encodeExternalConsumerV3Ustar([
        { path: "file", type: "regular", mode: 0o600 as never, data: bytes("x") },
      ]),
    ).toThrow(ExternalConsumerV3UstarError);
  });
});
