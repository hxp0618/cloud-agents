import { describe, expect, it } from "vitest";

import { providerKind, providerLabel } from "./provider";

describe("User Web Provider metadata", () => {
  it("keeps the display label mapping in one domain module", () => {
    expect(providerLabel("codex")).toBe("Codex");
    expect(providerLabel("claudeAgent")).toBe("Claude Code");
    expect(providerLabel("deepseek-harness")).toBe("deepseek-harness");
  });

  it("falls back to the compatible default for unknown persisted values", () => {
    expect(providerKind(undefined)).toBe("codex");
    expect(providerKind("unknown-provider")).toBe("codex");
    expect(providerKind("pi")).toBe("pi");
  });
});
