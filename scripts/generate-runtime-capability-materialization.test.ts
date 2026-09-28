import { createHash, generateKeyPairSync, sign } from "node:crypto";
import { chmodSync, mkdirSync, readFileSync, rmSync, statSync, writeFileSync } from "node:fs";
import { join } from "node:path";
import { describe, expect, it } from "vitest";

import { generateRuntimeCapabilityMaterialization } from "./generate-runtime-capability-materialization";

describe("runtime capability materialization generator", () => {
  it("verifies signed Skill source and emits protected tenant material", () => {
    const root = `/tmp/cloud-agents-capability-generator-${process.pid}-${Date.now()}`;
    const source = join(root, "source");
    const output = join(root, "output");
    mkdirSync(source, { recursive: true, mode: 0o700 });
    const bundle = Buffer.from(
      JSON.stringify({
        version: 1,
        files: [
          {
            path: "skills/managed/SKILL.md",
            content: Buffer.from("# managed\n").toString("base64url"),
          },
        ],
      }),
    );
    const { privateKey, publicKey } = generateKeyPairSync("ed25519");
    const publicKeyBytes = publicKey.export({ format: "der", type: "spki" });
    writeProtected(join(source, "token"), Buffer.from("token-from-file"));
    writeProtected(join(source, "bundle"), bundle);
    writeProtected(join(source, "signature"), sign(null, bundle, privateKey));
    writeProtected(join(source, "public-key"), publicKeyBytes);
    const digest = `sha256:${createHash("sha256").update(bundle).digest("hex")}`;
    writeProtected(
      join(source, "config.json"),
      Buffer.from(
        JSON.stringify({
          version: 1,
          tenantId: "tenant-alpha",
          mcp: [
            {
              resourceId: "mcp-1",
              version: "2026.09.1",
              digest: "sha256:" + "1".repeat(64),
              transport: "streamable-http",
              endpoint: "https://mcp.example.test/mcp",
              tokenFile: "token",
              allowedHosts: ["mcp.example.test"],
            },
          ],
          skills: [
            {
              resourceId: "skill-1",
              version: "2026.09.1",
              digest,
              bundleFile: "bundle",
              signatureFile: "signature",
              publicKeyFile: "public-key",
              signingKeyId: "key-1",
            },
          ],
        }),
      ),
    );
    try {
      const result = generateRuntimeCapabilityMaterialization(join(source, "config.json"), output);
      expect(result.descriptorPath).toBe(join(output, "tenant-alpha.capabilities.json"));
      expect(statSync(result.descriptorPath).mode & 0o777).toBe(0o600);
      expect(statSync(join(output, "key-1.pub")).mode & 0o777).toBe(0o600);
      const descriptor = JSON.parse(readFileSync(result.descriptorPath, "utf8"));
      expect(descriptor.mcp[0].token).toBe("token-from-file");
      expect(descriptor.skills[0].publicKey).toBe(
        Buffer.from(publicKeyBytes).toString("base64url"),
      );
      expect(result.descriptorDigest).toMatch(/^sha256:[0-9a-f]{64}$/u);
    } finally {
      rmSync(root, { recursive: true, force: true });
    }
  });

  it("fails closed when the signed Skill bytes are changed", () => {
    const root = `/tmp/cloud-agents-capability-generator-invalid-${process.pid}-${Date.now()}`;
    mkdirSync(root, { recursive: true, mode: 0o700 });
    const { privateKey, publicKey } = generateKeyPairSync("ed25519");
    const original = Buffer.from("signed source");
    writeProtected(join(root, "bundle"), Buffer.from("tampered source"));
    writeProtected(join(root, "signature"), sign(null, original, privateKey));
    writeProtected(join(root, "public-key"), publicKey.export({ format: "der", type: "spki" }));
    const digest = "sha256:" + createHash("sha256").update(original).digest("hex");
    writeProtected(
      join(root, "config.json"),
      Buffer.from(
        JSON.stringify({
          version: 1,
          tenantId: "tenant-alpha",
          mcp: [],
          skills: [
            {
              resourceId: "skill-1",
              version: "1",
              digest,
              bundleFile: "bundle",
              signatureFile: "signature",
              publicKeyFile: "public-key",
              signingKeyId: "key-1",
            },
          ],
        }),
      ),
    );
    try {
      expect(() =>
        generateRuntimeCapabilityMaterialization(join(root, "config.json"), join(root, "out")),
      ).toThrow(/digest mismatch/u);
    } finally {
      rmSync(root, { recursive: true, force: true });
    }
  });

  it("rejects SSE because the common Runtime broker is streamable-http only", () => {
    const root = `/tmp/cloud-agents-capability-generator-sse-${process.pid}-${Date.now()}`;
    mkdirSync(root, { recursive: true, mode: 0o700 });
    writeProtected(join(root, "token"), Buffer.from("token"));
    writeProtected(
      join(root, "config.json"),
      Buffer.from(
        JSON.stringify({
          version: 1,
          tenantId: "tenant-alpha",
          mcp: [
            {
              resourceId: "mcp-1",
              version: "1",
              digest: "sha256:" + "1".repeat(64),
              transport: "sse",
              endpoint: "https://mcp.example.test/sse",
              tokenFile: "token",
              allowedHosts: ["mcp.example.test"],
            },
          ],
          skills: [],
        }),
      ),
    );
    try {
      expect(() =>
        generateRuntimeCapabilityMaterialization(join(root, "config.json"), join(root, "out")),
      ).toThrow(/MCP materialization config entry is invalid/u);
    } finally {
      rmSync(root, { recursive: true, force: true });
    }
  });

  it("rejects duplicate MCP identities, credentials, and Skill identities", () => {
    const root = `/tmp/cloud-agents-capability-generator-duplicates-${process.pid}-${Date.now()}`;
    mkdirSync(root, { recursive: true, mode: 0o700 });
    writeProtected(join(root, "token-1"), Buffer.from("token-one"));
    writeProtected(join(root, "token-2"), Buffer.from("token-two"));
    const mcp = (resourceId: string, tokenFile: string) => ({
      resourceId,
      version: "1",
      digest: "sha256:" + "1".repeat(64),
      transport: "streamable-http",
      endpoint: "https://mcp.example.test/mcp",
      tokenFile,
      allowedHosts: ["mcp.example.test"],
    });
    const writeConfig = (value: unknown) =>
      writeProtected(join(root, "config.json"), Buffer.from(JSON.stringify(value)));
    const output = join(root, "out");
    try {
      writeConfig({
        version: 1,
        tenantId: "tenant-alpha",
        mcp: [mcp("mcp-1", "token-1"), mcp("mcp-1", "token-2")],
        skills: [],
      });
      expect(() =>
        generateRuntimeCapabilityMaterialization(join(root, "config.json"), output),
      ).toThrow(/MCP resource mcp-1 is duplicated/u);

      writeConfig({
        version: 1,
        tenantId: "tenant-alpha",
        mcp: [mcp("mcp-1", "token-1"), mcp("mcp-2", "token-1")],
        skills: [],
      });
      expect(() =>
        generateRuntimeCapabilityMaterialization(join(root, "config.json"), output),
      ).toThrow(/MCP tokens must be unique/u);

      const bundle = Buffer.from(
        JSON.stringify({
          version: 1,
          files: [
            {
              path: "skills/managed/SKILL.md",
              content: Buffer.from("# managed\n").toString("base64url"),
            },
          ],
        }),
      );
      const { privateKey, publicKey } = generateKeyPairSync("ed25519");
      writeProtected(join(root, "bundle"), bundle);
      writeProtected(join(root, "signature"), sign(null, bundle, privateKey));
      writeProtected(join(root, "public-key"), publicKey.export({ format: "der", type: "spki" }));
      const skill = {
        resourceId: "skill-1",
        version: "1",
        digest: `sha256:${createHash("sha256").update(bundle).digest("hex")}`,
        bundleFile: "bundle",
        signatureFile: "signature",
        publicKeyFile: "public-key",
        signingKeyId: "key-1",
      };
      writeConfig({
        version: 1,
        tenantId: "tenant-alpha",
        mcp: [],
        skills: [skill, skill],
      });
      expect(() =>
        generateRuntimeCapabilityMaterialization(join(root, "config.json"), output),
      ).toThrow(/Skill resource skill-1 is duplicated/u);
    } finally {
      rmSync(root, { recursive: true, force: true });
    }
  });
});

function writeProtected(path: string, bytes: Uint8Array): void {
  writeFileSync(path, bytes, { mode: 0o600 });
  chmodSync(path, 0o600);
}
