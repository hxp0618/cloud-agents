import type { EnvironmentProfileSummary } from "@cloud-agents/cloud-agent-platform-sdk/platform";

export type ProviderKind = EnvironmentProfileSummary["providerKinds"][number];

const providerLabels: Readonly<Record<ProviderKind, string>> = Object.freeze({
  codex: "Codex",
  claudeAgent: "Claude Code",
  pi: "Pi",
  "deepseek-harness": "deepseek-harness",
});

export function providerLabel(provider: string): string {
  const known = providerKind(provider);
  return known === undefined ? provider : providerLabels[known];
}

export function providerKind(value: string | undefined): ProviderKind | undefined {
  return value === "codex" ||
    value === "claudeAgent" ||
    value === "pi" ||
    value === "deepseek-harness"
    ? value
    : undefined;
}
