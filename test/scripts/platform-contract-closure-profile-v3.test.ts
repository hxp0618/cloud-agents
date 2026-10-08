import { resolve } from "node:path";

import { describe, expect, it } from "vitest";

import {
  buildContractClosureProfileV3Source,
  buildContractClosureProfileV3TestSource,
  serializeContractClosureProfileV3Registry,
  serializeContractClosureProfileV3Source,
} from "../../scripts/lib/platform-contract-closure-profile-v3";

const repositoryRoot = resolve(import.meta.dirname, "../..");
describe("contract closure profile v3 Slice A authority", () => {
  it("promotes the deterministic test builder to the canonical production source serializer", () => {
    const source = buildContractClosureProfileV3Source(repositoryRoot);
    expect(source).toEqual(buildContractClosureProfileV3TestSource(repositoryRoot));
    expect(serializeContractClosureProfileV3Source(source)).toBe(
      serializeContractClosureProfileV3Registry(source),
    );
  });
});
