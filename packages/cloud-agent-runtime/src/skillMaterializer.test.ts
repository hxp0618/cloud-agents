import { createHash, generateKeyPairSync, sign } from "node:crypto";
import {
  existsSync,
  mkdtempSync,
  mkdirSync,
  readFileSync,
  readdirSync,
  realpathSync,
  rmSync,
  statSync,
  symlinkSync,
} from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";

import { describe, expect, it } from "vitest";

import { materializeManagedSkills } from "./skillMaterializer";

function fixture() {
  const bundle = Buffer.from(
    JSON.stringify({
      version: 1,
      files: [
        {
          path: ".claude-plugin/plugin.json",
          content: Buffer.from(JSON.stringify({ name: "managed-skill" })).toString("base64url"),
        },
        {
          path: "skills/managed-skill/SKILL.md",
          content: Buffer.from(
            "---\nname: managed-skill\ndescription: managed\n---\nUse this skill.\n",
          ).toString("base64url"),
        },
      ],
    }),
  );
  const { privateKey, publicKey } = generateKeyPairSync("ed25519");
  const digest = `sha256:${createHash("sha256").update(bundle).digest("hex")}` as const;
  return {
    manifest: {
      version: 1 as const,
      bindings: [
        {
          resourceKind: "skill-bundle" as const,
          resourceId: "skill-1",
          version: "v1",
          digest,
          grantId: "grant-1",
          expiresAtUnixSeconds: Math.floor(Date.now() / 1000) + 300,
          permissions: ["skill.load"],
          readOnly: true,
        },
      ],
      digest,
    },
    materialization: {
      version: 1 as const,
      mcp: [],
      skills: [
        {
          resourceId: "skill-1",
          version: "v1",
          digest,
          bundle: bundle.toString("base64url"),
          signature: sign(null, bundle, privateKey).toString("base64url"),
          publicKey: publicKey.export({ format: "der", type: "spki" }).toString("base64url"),
          signingKeyId: "key-1",
        },
      ],
    },
  };
}

describe("managed Skill Bundle materializer", () => {
  it("verifies, mounts read-only files, and removes only its generated root", () => {
    const root = realpathSync.native(mkdtempSync(join(tmpdir(), "cloud-agent-skills-")));
    try {
      const value = fixture();
      const mounted = materializeManagedSkills(value.manifest, value.materialization, root);
      const skillRoot = mounted?.environment.CLOUD_AGENT_SKILL_BUNDLE_SKILL_1_ROOT;
      expect(skillRoot).toMatch(new RegExp(`^${root}/\\.runtime-[^/]+/skill-1$`));
      if (!skillRoot) throw new Error("Skill Bundle root was not materialized");
      expect(readFileSync(join(skillRoot, "skills/managed-skill/SKILL.md"), "utf8")).toContain(
        "name: managed-skill",
      );
      expect(statSync(join(skillRoot, "skills/managed-skill/SKILL.md")).mode & 0o222).toBe(0);
      mounted?.close();
      expect(existsSync(skillRoot)).toBe(false);
      expect(existsSync(root)).toBe(true);
    } finally {
      rmSync(root, { recursive: true, force: true });
    }
  });

  it("uses an isolated root for overlapping runtime processes", () => {
    const root = realpathSync.native(mkdtempSync(join(tmpdir(), "cloud-agent-skills-overlap-")));
    try {
      const value = fixture();
      const first = materializeManagedSkills(value.manifest, value.materialization, root);
      const second = materializeManagedSkills(value.manifest, value.materialization, root);
      const firstRoot = first?.environment.CLOUD_AGENT_SKILL_BUNDLE_SKILL_1_ROOT;
      const secondRoot = second?.environment.CLOUD_AGENT_SKILL_BUNDLE_SKILL_1_ROOT;
      expect(firstRoot).toBeDefined();
      expect(secondRoot).toBeDefined();
      expect(firstRoot).not.toBe(secondRoot);
      second?.close();
      first?.close();
    } finally {
      rmSync(root, { recursive: true, force: true });
    }
  });

  it("fails closed on a signature mismatch", () => {
    const root = realpathSync.native(mkdtempSync(join(tmpdir(), "cloud-agent-skills-signature-")));
    try {
      const value = fixture();
      value.materialization.skills[0]!.signature = Buffer.alloc(64).toString("base64url");
      expect(() => materializeManagedSkills(value.manifest, value.materialization, root)).toThrow(
        "signature verification failed",
      );
    } finally {
      rmSync(root, { recursive: true, force: true });
    }
  });

  it("rejects a symlinked materialization root before writing a session tree", () => {
    const root = realpathSync.native(mkdtempSync(join(tmpdir(), "cloud-agent-skills-root-link-")));
    const target = join(root, "target");
    const linked = join(root, "linked");
    mkdirSync(target);
    symlinkSync(target, linked);
    try {
      const value = fixture();
      expect(() => materializeManagedSkills(value.manifest, value.materialization, linked)).toThrow(
        "materialization root is untrusted",
      );
      expect(readdirSync(target)).toEqual([]);
    } finally {
      rmSync(root, { recursive: true, force: true });
    }
  });

  it("rejects Skill mounts that collide after environment-name normalization", () => {
    const root = realpathSync.native(mkdtempSync(join(tmpdir(), "cloud-agent-skills-collision-")));
    try {
      const value = fixture();
      const binding = value.manifest.bindings[0]!;
      const skill = value.materialization.skills[0]!;
      value.manifest.bindings.push({
        ...binding,
        resourceId: "skill_1",
        grantId: "grant-2",
      });
      value.materialization.skills.push({ ...skill, resourceId: "skill_1" });
      expect(() => materializeManagedSkills(value.manifest, value.materialization, root)).toThrow(
        "environment references collide",
      );
    } finally {
      rmSync(root, { recursive: true, force: true });
    }
  });
});
