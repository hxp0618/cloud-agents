#!/usr/bin/env node
import { runManagedSkillSandbox } from "@cloud-agents/cloud-agent-runtime";

export {};

try {
  const sandboxHandled = await runManagedSkillSandbox({
    childCommand: process.execPath,
    childArgs: [process.argv[1] ?? "", ...process.argv.slice(2)],
  });
  if (!sandboxHandled) {
    const { CODEX_TOOL_POLICY_HOOK_ARGUMENT, runCodexNoToolAwarePolicyHook } =
      await import("@cloud-agents/cloud-agent-provider-codex");
    if (process.argv.includes(CODEX_TOOL_POLICY_HOOK_ARGUMENT)) {
      await runCodexNoToolAwarePolicyHook();
    } else {
      const { runDefaultCloudAgentRuntimeStdio } = await import("./runStdio");
      await runDefaultCloudAgentRuntimeStdio({ skillSandboxHandled: true });
    }
  }
} catch (cause) {
  const message = cause instanceof Error ? cause.message : String(cause);
  process.stderr.write(`cloud-agent-runtime: ${message}\n`);
  process.exitCode ??= 1;
}
