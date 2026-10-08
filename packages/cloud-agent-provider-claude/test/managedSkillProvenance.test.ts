import { mkdirSync, mkdtempSync, renameSync, rmSync, symlinkSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { describe, expect, it } from "vitest";

import { managedSkillResourceIdsByQualifiedName } from "../src/managedSkillProvenance";

describe("managedSkillResourceIdsByQualifiedName", () => {
  it("indexes zero, one, and multiple bundles by exact qualified name", () => {
    const directories: string[] = [];
    try {
      expect(managedSkillResourceIdsByQualifiedName([])).toEqual({});

      const first = createBundle("first-plugin", "first-skill");
      directories.push(first);
      expect(
        managedSkillResourceIdsByQualifiedName([{ directory: first, resourceId: "skill-1" }]),
      ).toEqual({ "first-plugin:first-skill": "skill-1" });

      const second = createBundle("second-plugin", "second-skill");
      directories.push(second);
      expect(
        managedSkillResourceIdsByQualifiedName([
          { directory: first, resourceId: "skill-1" },
          { directory: second, resourceId: "skill-2" },
        ]),
      ).toEqual({
        "first-plugin:first-skill": "skill-1",
        "second-plugin:second-skill": "skill-2",
      });
    } finally {
      for (const directory of directories) rmSync(directory, { recursive: true, force: true });
    }
  });

  it("does not attribute a duplicate qualified name across bundles", () => {
    const first = createBundle("duplicate-plugin", "duplicate-skill");
    const second = createBundle("duplicate-plugin", "duplicate-skill");
    try {
      expect(
        managedSkillResourceIdsByQualifiedName([
          { directory: first, resourceId: "skill-1" },
          { directory: second, resourceId: "skill-2" },
        ]),
      ).toEqual({});
    } finally {
      rmSync(first, { recursive: true, force: true });
      rmSync(second, { recursive: true, force: true });
    }
  });

  it.each([
    ["skills", "../outside-skills"],
    ["hooks", { PostToolUse: [{ command: "outside-command" }] }],
  ])("fails closed when plugin.json contains the %s redirect field", (field, value) => {
    const directory = createBundle("safe-plugin", "safe-skill");
    writeFileSync(
      join(directory, ".claude-plugin", "plugin.json"),
      JSON.stringify({ name: "safe-plugin", [field]: value }),
    );
    try {
      expect(() =>
        managedSkillResourceIdsByQualifiedName([{ directory, resourceId: "skill-1" }]),
      ).toThrow(`unsupported field ${field}`);
    } finally {
      rmSync(directory, { recursive: true, force: true });
    }
  });

  it.each([
    ["hooks", "directory"],
    ["commands", "directory"],
    ["agents", "directory"],
    ["monitors", "directory"],
    ["output-styles", "directory"],
    ["themes", "directory"],
    ["bin", "directory"],
    ["settings", "directory"],
    [".mcp.json", "file"],
    [".lsp.json", "file"],
    ["README.md", "file"],
    ["LICENSE", "file"],
  ])("rejects an unsupported Bundle root entry: %s", (entry, kind) => {
    const directory = createBundle("safe-plugin", "safe-skill");
    if (kind === "directory") mkdirSync(join(directory, entry));
    else writeFileSync(join(directory, entry), "unsupported");
    try {
      expect(() =>
        managedSkillResourceIdsByQualifiedName([{ directory, resourceId: "skill-1" }]),
      ).toThrow(`unsupported entry ${entry}`);
    } finally {
      rmSync(directory, { recursive: true, force: true });
    }
  });

  it("rejects an extra plugin metadata entry", () => {
    const directory = createBundle("safe-plugin", "safe-skill");
    writeFileSync(join(directory, ".claude-plugin", "hooks.json"), "{}");
    try {
      expect(() =>
        managedSkillResourceIdsByQualifiedName([{ directory, resourceId: "skill-1" }]),
      ).toThrow("may contain only a regular plugin.json file");
    } finally {
      rmSync(directory, { recursive: true, force: true });
    }
  });

  it("rejects a file directly under skills", () => {
    const directory = createBundle("safe-plugin", "safe-skill");
    writeFileSync(join(directory, "skills", "README.md"), "unsupported");
    try {
      expect(() =>
        managedSkillResourceIdsByQualifiedName([{ directory, resourceId: "skill-1" }]),
      ).toThrow("skills entries must be real directories");
    } finally {
      rmSync(directory, { recursive: true, force: true });
    }
  });

  it.each([
    ["quoted key", "'name': impostor\nname: visible"],
    ["merge key", "<<: defaults\nname: visible"],
    ["duplicate key", "name: first\nname: second"],
    ["quoted value", 'name: "visible"'],
    ["boolean value", "name: true"],
    ["YAML 1.1 boolean value", "name: YES"],
    ["null value", "name: null"],
    ["numeric value", "name: 123"],
    ["exponential numeric value", "name: 1e3"],
  ])("rejects ambiguous YAML frontmatter using a %s", (_label, frontmatter) => {
    const directory = createBundle("safe-plugin", "ignored", frontmatter);
    try {
      expect(
        managedSkillResourceIdsByQualifiedName([{ directory, resourceId: "skill-1" }]),
      ).toEqual({});
    } finally {
      rmSync(directory, { recursive: true, force: true });
    }
  });

  it("rejects a symlinked skill directory that escapes the bundle root", () => {
    const directory = createBundle("safe-plugin", "safe-skill");
    const outside = mkdtempSync(join(tmpdir(), "cloud-agents-claude-skill-outside-"));
    symlinkSync(outside, join(directory, "skills", "escaped"), "dir");
    try {
      expect(() =>
        managedSkillResourceIdsByQualifiedName([{ directory, resourceId: "skill-1" }]),
      ).toThrow("must not be a symlink");
    } finally {
      rmSync(directory, { recursive: true, force: true });
      rmSync(outside, { recursive: true, force: true });
    }
  });

  it("rejects a symlinked plugin metadata directory even within the bundle root", () => {
    const directory = createBundle("safe-plugin", "safe-skill");
    renameSync(join(directory, ".claude-plugin"), join(directory, "plugin-metadata"));
    symlinkSync("plugin-metadata", join(directory, ".claude-plugin"), "dir");
    try {
      expect(() =>
        managedSkillResourceIdsByQualifiedName([{ directory, resourceId: "skill-1" }]),
      ).toThrow("root entries must be real directories");
    } finally {
      rmSync(directory, { recursive: true, force: true });
    }
  });
});

function createBundle(pluginName: string, skillName: string, frontmatter?: string): string {
  const managedRoot = join(tmpdir(), "cloud-agents-skills");
  mkdirSync(managedRoot, { recursive: true });
  const directory = mkdtempSync(join(managedRoot, "claude-provenance-"));
  mkdirSync(join(directory, ".claude-plugin"));
  mkdirSync(join(directory, "skills", skillName), { recursive: true });
  writeFileSync(
    join(directory, ".claude-plugin", "plugin.json"),
    JSON.stringify({ name: pluginName }),
  );
  writeFileSync(
    join(directory, "skills", skillName, "SKILL.md"),
    `---\n${frontmatter ?? `name: ${skillName}\ndescription: managed skill`}\n---\n`,
  );
  return directory;
}
