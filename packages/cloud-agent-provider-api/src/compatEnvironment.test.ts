import { describe, expect, it } from "vitest";

import {
  CLOUD_AGENT_ENVIRONMENT,
  readCloudAgentEnvironment,
  writeCloudAgentEnvironment,
} from "./compatEnvironment";
import {
  CLOUD_AGENT_GENERIC_HOST_IDENTITY,
  reconstructedPrompt,
  type RunnerInput,
} from "./internalExecution";

describe("portable compatibility configuration", () => {
  it("prefers the portable name, falls back to legacy, and rejects conflicts", () => {
    const alias = CLOUD_AGENT_ENVIRONMENT.providerOuterSandboxProfile;
    expect(readCloudAgentEnvironment({ [alias.name]: "portable" }, alias)).toBe("portable");
    expect(readCloudAgentEnvironment({ [alias.legacyName]: "legacy" }, alias)).toBe("legacy");
    expect(() =>
      readCloudAgentEnvironment({ [alias.name]: "portable", [alias.legacyName]: "legacy" }, alias),
    ).toThrow(`Conflicting ${alias.name}`);
  });

  it("dual-writes identical child metadata", () => {
    const environment: Record<string, string> = {};
    const alias = CLOUD_AGENT_ENVIRONMENT.providerCredentialFd;
    writeCloudAgentEnvironment(environment, alias, "3");
    expect(environment).toEqual({ [alias.name]: "3", [alias.legacyName]: "3" });
  });

  it("supports a generic recovery namespace while preserving the legacy default", () => {
    const input = runnerInput();
    expect(reconstructedPrompt(input)).toContain("<synara_resume_snapshot_json>");
    const generic = reconstructedPrompt(input, CLOUD_AGENT_GENERIC_HOST_IDENTITY);
    expect(generic).toContain("Continue the durable Portable Cloud Agent Session");
    expect(generic).toContain("<cloud_agent_resume_snapshot_json>");
    expect(generic).not.toContain("<synara_resume_snapshot_json>");
  });
});

function runnerInput(): RunnerInput {
  return {
    execution: { id: "execution-1", generation: 1 },
    workload: {
      provider: "codex",
      inputText: "continue",
      resumeSnapshot: {
        version: 1,
        sessionId: "session-1",
        turnId: "turn-1",
        provider: "codex",
        messages: [{ role: "user", text: "prior" }],
      },
    },
    workspaceDirectory: "/workspace",
  };
}
