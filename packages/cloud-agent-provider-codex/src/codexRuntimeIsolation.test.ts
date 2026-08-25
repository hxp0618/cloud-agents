import { readFileSync } from "node:fs";
import { describe, expect, it } from "vitest";

import { isCodexRuntimeIsolationConfigAttested } from "./codexRuntimeIsolation";

const officialCodex0145ConfigRead = JSON.parse(
  readFileSync(new URL("../test-fixtures/codex-0.145.0-config-read.json", import.meta.url), "utf8"),
) as unknown;

describe("Codex runtime-isolation configuration attestation", () => {
  it("accepts the official Codex 0.145 null shell-policy defaults when no exclusions are expected", () => {
    expect(isCodexRuntimeIsolationConfigAttested(officialCodex0145ConfigRead, [])).toBe(true);
  });

  it("fails closed when the official null shell policy cannot prove a required exclusion", () => {
    expect(
      isCodexRuntimeIsolationConfigAttested(
        officialCodex0145ConfigRead,
        [],
        ["CLOUD_AGENT_GATEWAY_TOKEN"],
      ),
    ).toBe(false);
  });

  it("requires every explicit exclusion to be a string", () => {
    const response = structuredClone(officialCodex0145ConfigRead) as {
      config: { shell_environment_policy: { exclude: unknown } };
    };
    response.config.shell_environment_policy.exclude = ["SAFE_NAME", 42];
    expect(isCodexRuntimeIsolationConfigAttested(response, [])).toBe(false);
  });

  it("rejects shell policy fields that can inject or broaden the child environment", () => {
    const response = structuredClone(officialCodex0145ConfigRead) as {
      config: { shell_environment_policy: { set: unknown } };
    };
    response.config.shell_environment_policy.set = {
      NODE_OPTIONS: "--require=/workspace/untrusted.js",
    };
    expect(isCodexRuntimeIsolationConfigAttested(response, [])).toBe(false);
  });

  it("accepts an explicitly attested required exclusion", () => {
    const response = structuredClone(officialCodex0145ConfigRead) as {
      config: { shell_environment_policy: { exclude: unknown } };
    };
    response.config.shell_environment_policy.exclude = ["cloud_agent_gateway_token"];
    expect(isCodexRuntimeIsolationConfigAttested(response, [], ["CLOUD_AGENT_GATEWAY_TOKEN"])).toBe(
      true,
    );
  });
});
