import type { Readable, Writable } from "node:stream";

import { buildCodexToolPolicyHookCommand } from "@cloud-agents/cloud-agent-provider-codex";
import {
  runCloudAgentRuntimeStdio,
  runManagedSkillSandbox,
} from "@cloud-agents/cloud-agent-runtime";

import { CLOUD_AGENT_DISTRIBUTION_MANIFEST, createDefaultCloudAgentRuntime } from "./index";

export type DefaultCloudAgentRuntimeStdioOptions = {
  readonly source?: Readable;
  readonly output?: Writable;
  readonly diagnostics?: Writable;
  readonly environment?: Readonly<Record<string, string | undefined>>;
  /** @internal Bootstrap injection used by Runtime conformance tests. */
  readonly skillSandbox?: Omit<
    Parameters<typeof runManagedSkillSandbox>[0],
    "source" | "output" | "diagnostics" | "environment" | "childCommand" | "childArgs"
  >;
  /** @internal The executable entrypoint already ran the Skill bootstrap. */
  readonly skillSandboxHandled?: boolean;
};

export const CLOUD_AGENT_DISTRIBUTION_PROVIDER_ALLOWLIST = Object.freeze(
  CLOUD_AGENT_DISTRIBUTION_MANIFEST.providers.map((provider) => provider.kind),
);

export async function runDefaultCloudAgentRuntimeStdio(
  options: DefaultCloudAgentRuntimeStdioOptions = {},
): Promise<void> {
  if (
    !options.skillSandboxHandled &&
    (await runManagedSkillSandbox({
      ...(options.source ? { source: options.source } : {}),
      ...(options.output ? { output: options.output } : {}),
      ...(options.diagnostics ? { diagnostics: options.diagnostics } : {}),
      ...(options.environment ? { environment: options.environment } : {}),
      childCommand: process.execPath,
      childArgs: [process.argv[1] ?? "", ...process.argv.slice(2)],
      ...options.skillSandbox,
    }))
  ) {
    return;
  }
  const runtime = createDefaultCloudAgentRuntime({
    toolPolicyHookCommand: buildCodexToolPolicyHookCommand({
      nodeExecutable: process.execPath,
      providerHostEntrypoint: process.argv[1] ?? "",
    }),
  });
  assertRegistryMatchesManifest(runtime.providerKinds);
  await runCloudAgentRuntimeStdio(runtime, {
    ...options,
    allowedProviders: CLOUD_AGENT_DISTRIBUTION_PROVIDER_ALLOWLIST,
  });
}

function assertRegistryMatchesManifest(registeredProviders: ReadonlyArray<string>): void {
  const registered = [...registeredProviders].toSorted();
  const allowed = [...CLOUD_AGENT_DISTRIBUTION_PROVIDER_ALLOWLIST].toSorted();
  if (JSON.stringify(registered) !== JSON.stringify(allowed)) {
    throw new Error("Cloud Agent Distribution manifest and Runtime registry allowlists disagree.");
  }
}
