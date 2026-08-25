import { readFileSync } from "node:fs";
import { resolve } from "node:path";

import { describe, expect, it } from "vitest";

import {
  SUCCESSOR_V3_CORE_GENERATOR_OUTPUT_PATHS,
  SUCCESSOR_V3_PROJECTION_EXCLUSIONS,
} from "./platform-successor-dag-v3";

const repositoryRoot = resolve(import.meta.dirname, "../..");
const wrapperPath = resolve(repositoryRoot, "scripts/replay-platform-generators-isolated-v3.sh");
const runnerPath = resolve(repositoryRoot, "scripts/replay-platform-generators-v3.ts");

function quotedPaths(block: string): string[] {
  return [...block.matchAll(/^[ \t]*"([^"]+)"[ \t]*,?[ \t]*$/gmu)].map((match) => match[1]!);
}

function projectionLists(wrapper: string): string[][] {
  const lists: string[][] = [];
  const top = /readonly PROJECTION_EXCLUSIONS=\(\n([\s\S]*?)\n\)/u.exec(wrapper);
  if (top) lists.push(quotedPaths(top[1]!));
  for (const match of wrapper.matchAll(
    /(?:metadata\.get\("excluded"\) != |"excluded": )\[\n([\s\S]*?)\n[ \t]*\]/gu,
  ))
    lists.push(quotedPaths(match[1]!));
  return lists;
}

describe("generator-supply v3 isolation wrapper authority", () => {
  it("uses the V3 runner and repeats the exact ordered 17-path exclusion list", () => {
    const wrapper = readFileSync(wrapperPath, "utf8");
    const runner = readFileSync(runnerPath, "utf8");
    expect(wrapper).toContain('readonly WRAPPER_POLICY="VERSIONED_ISOLATION_WRAPPER_V3"');
    expect(wrapper).toContain("replay-platform-generators-v3.ts");
    expect(wrapper).not.toContain("replay-platform-generators.ts");
    expect(wrapper).not.toContain("tools/generator-supply/v2/");
    expect(runner).toContain("VERSIONED_ISOLATION_WRAPPER_V3");
    expect(runner).not.toContain("VERSIONED_ISOLATION_WRAPPER_V1");
    expect(projectionLists(wrapper)).toEqual([
      [...SUCCESSOR_V3_PROJECTION_EXCLUSIONS],
      [...SUCCESSOR_V3_PROJECTION_EXCLUSIONS],
      [...SUCCESSOR_V3_PROJECTION_EXCLUSIONS],
    ]);
  });

  it("retains exactly the frozen 49 core generator output paths", () => {
    const wrapper = readFileSync(wrapperPath, "utf8");
    const block = /readonly GENERATOR_OUTPUT_FILES=\(\n([\s\S]*?)\n\)/u.exec(wrapper);
    expect(block).not.toBeNull();
    expect(quotedPaths(block![1]!)).toEqual([...SUCCESSOR_V3_CORE_GENERATOR_OUTPUT_PATHS]);
  });
});
