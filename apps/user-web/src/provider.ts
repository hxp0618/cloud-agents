import type { EnvironmentProfileSummary } from "@cloud-agents/cloud-agent-platform-sdk/platform";

export type ProviderKind = EnvironmentProfileSummary["providerKinds"][number];

const providerLabels: Readonly<Record<ProviderKind, string>> = Object.freeze({
  codex: "Codex",
  claudeAgent: "Claude Code",
  pi: "Pi",
  "deepseek-harness": "deepseek-harness",
});

export function providerLabel(provider: ProviderKind): string {
  return providerLabels[provider];
}

export function providerKind(value: string | undefined): ProviderKind {
  return value === "claudeAgent" || value === "pi" || value === "deepseek-harness"
    ? value
    : "codex";
}
