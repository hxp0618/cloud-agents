import { describe, expect, it } from "vitest";

import { providerKind, providerLabel } from "../src/provider";

describe("User Web Provider metadata", () => {
  it("keeps the display label mapping in one domain module", () => {
    expect(providerLabel("codex")).toBe("Codex");
    expect(providerLabel("claudeAgent")).toBe("Claude Code");
    expect(providerLabel("deepseek-harness")).toBe("deepseek-harness");
    expect(providerLabel("future-provider")).toBe("future-provider");
  });

  it("selects only Providers the User Web can create", () => {
    expect(providerKind(undefined)).toBeUndefined();
    expect(providerKind("unknown-provider")).toBeUndefined();
    expect(providerKind("pi")).toBe("pi");
  });
});
