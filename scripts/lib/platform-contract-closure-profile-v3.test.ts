import { rmSync } from "node:fs";
import { resolve } from "node:path";

import { afterEach, describe, expect, it } from "vitest";

import {
  buildContractClosureProfileV3Source,
  buildContractClosureProfileV3TestSource,
  serializeContractClosureProfileV3Registry,
  serializeContractClosureProfileV3Source,
} from "./platform-contract-closure-profile-v3";

const repositoryRoot = resolve(import.meta.dirname, "../..");
const temporaryRoots: string[] = [];

afterEach(() => {
  for (const root of temporaryRoots.splice(0)) rmSync(root, { force: true, recursive: true });
});

describe("contract closure profile v3 Slice A authority", () => {
  it("promotes the deterministic test builder to the canonical production source serializer", () => {
    const source = buildContractClosureProfileV3Source(repositoryRoot);
    expect(source).toEqual(buildContractClosureProfileV3TestSource(repositoryRoot));
    expect(serializeContractClosureProfileV3Source(source)).toBe(
      serializeContractClosureProfileV3Registry(source),
    );
  });
});
