import { createHash, randomUUID } from "node:crypto";
import { mkdirSync, writeFileSync } from "node:fs";
import { join } from "node:path";

import {
  DeepSeekHarness,
  type DeepSeekHarnessOptions,
  type HarnessNotification,
  type RunResult,
} from "@deepseek-ai/dsh-sdk-client";
import type { CloudAgentProviderPluginV1 } from "@cloud-agents/cloud-agent-provider-api";
import {
  ManagedCapabilityCallResultUnknownError,
  ProviderInterruptedError,
  WorkspaceGeneratedFileCollector,
  capabilityTokenEnvironmentName,
  createProviderPlugin,
  hasAuthoritativeResumeData,
  managedMcpConfiguration,
  managedSkillBundleDirectories,
  isManagedMcpCallResultUnknown,
  providerEnvironment,
  reconstructedPrompt,
  requireProviderOuterSandboxProfile,
  validateRunnerInput,
  readCapabilityManifest,
  type ProviderRunController,
  type ProviderRunExecutor,
  type ProviderRunOptions,
  type RunnerCredential,
  type RunnerInput,
  type RunnerMessage,
  type RuntimeCapabilityManifest,
} from "@cloud-agents/cloud-agent-provider-api/internal";

export const DEEPSEEK_HARNESS_PROVIDER_KIND = "deepseek-harness" as const;
const DEEPSEEK_HARNESS_VERSION = "0.1.2-rc.1";
const DEEPSEEK_HARNESS_ROUTE = "cloud-agents-openai";

type Harness = Pick<DeepSeekHarness, "close" | "run">;
type DeepSeekHarnessRunOptions = ProviderRunOptions & {
  readonly harnessFactory?: (options: DeepSeekHarnessOptions) => Harness;
};

export function startDeepSeekHarnessProviderRun(
  input: RunnerInput,
  credential: RunnerCredential | null,
  emit: (message: RunnerMessage) => void,
  options: DeepSeekHarnessRunOptions = {},
): ProviderRunController {
  validateRunnerInput(input, { allowEmptyInputText: options.operation !== undefined });
  if (input.workload.provider.trim().toLowerCase() !== DEEPSEEK_HARNESS_PROVIDER_KIND)
    throw new Error(
      `deepseek-harness Provider cannot execute provider ${input.workload.provider}.`,
    );
  if (options.operation && options.operation.commandType !== "GenerateText")
    throw new Error(`deepseek-harness Provider does not support ${options.operation.commandType}.`);
  requireProviderOuterSandboxProfile(options.environment ?? process.env);
  const sourceEnvironment = options.environment ?? process.env;
  const capabilityManifest = readCapabilityManifest(sourceEnvironment);

  const effectiveInput = withCredentialModel(input, credential);
  const { environment, redact } = providerEnvironment(
    sourceEnvironment,
    credential,
    applyDeepSeekHarnessCredentialEnvironment,
  );
  const stateRoot = effectiveInput.providerStateDirectory?.trim();
  if (!stateRoot) throw new Error("deepseek-harness Provider requires providerStateDirectory.");
  const model = effectiveInput.workload.model?.trim();
  if (!model) throw new Error("deepseek-harness Provider requires model.");
  const mcpConfiguration = managedMcpConfiguration(capabilityManifest, sourceEnvironment);
  Object.assign(environment, mcpConfiguration.environment);
  const skillDirectories = managedSkillBundleDirectories(capabilityManifest, sourceEnvironment);
  const dshHome = join(stateRoot, "dsh-home");
  mkdirSync(dshHome, { recursive: true, mode: 0o700 });
  const patchPath = writeModelPatch(stateRoot, model);
  const capabilityPatchPath = writeCapabilityPatch(stateRoot, capabilityManifest, skillDirectories);
  const dshBin = optionalString(
    environment.CLOUD_AGENT_DEEPSEEK_HARNESS_BIN,
    "CLOUD_AGENT_DEEPSEEK_HARNESS_BIN",
  );
  environment.DSH_TELEMETRY_DISABLED = "1";

  const generatedFiles = new WorkspaceGeneratedFileCollector({
    workspaceDirectory: effectiveInput.workspaceDirectory,
    provider: DEEPSEEK_HARNESS_PROVIDER_KIND,
    emit,
  });
  const requestedCursor = effectiveInput.providerResumeCursor;
  const resuming = validSessionId(requestedCursor);
  if (
    requestedCursor !== undefined &&
    (!resuming ||
      !hasAuthoritativeResumeData(effectiveInput.workload, effectiveInput.memoryDocuments))
  )
    throw new Error(
      "deepseek-harness Provider resume requires a valid cursor and authoritative history.",
    );
  const sessionId = `session-${randomUUID().replaceAll("-", "")}`;
  const harness = (
    options.harnessFactory ?? ((harnessOptions) => new DeepSeekHarness(harnessOptions))
  )({
    profile: "sdk",
    patches: [patchPath, ...(capabilityPatchPath ? [capabilityPatchPath] : [])],
    dshHome,
    processCwd: effectiveInput.workspaceDirectory,
    cwd: effectiveInput.workspaceDirectory,
    env: environment,
    ...(dshBin ? { dshBin } : {}),
    provider: DEEPSEEK_HARNESS_ROUTE,
    model,
    initializeTimeoutMs: 30_000,
  });
  let interrupted = false;
  let turnFailure: Error | undefined;
  let reportResultUnknown!: (error: ManagedCapabilityCallResultUnknownError) => void;
  const resultUnknown = new Promise<ManagedCapabilityCallResultUnknownError>((resolve) => {
    reportResultUnknown = resolve;
  });
  const activeTools = new Map<string, { name: string; capabilityResourceId?: string }>();
  const seenToolCalls = new Set<string>();
  const prompt = hasAuthoritativeResumeData(effectiveInput.workload, effectiveInput.memoryDocuments)
    ? reconstructedPrompt(effectiveInput, options.hostIdentity)
    : effectiveInput.workload.inputText;

  const result = (async (): Promise<Extract<RunnerMessage, { type: "result" }>> => {
    try {
      const run = await Promise.race([
        harness.run(prompt, {
          sessionId,
          onNotification(notification) {
            if (turnFailure) return;
            const failure = handleHarnessNotification(
              notification,
              capabilityManifest,
              activeTools,
              seenToolCalls,
              generatedFiles,
              emit,
            );
            if (failure) {
              turnFailure = failure;
              void harness.close();
              if (failure instanceof ManagedCapabilityCallResultUnknownError)
                reportResultUnknown(failure);
            }
          },
        }),
        resultUnknown.then((error) => {
          throw error;
        }),
      ]);
      if (turnFailure) throw turnFailure;
      if (
        run.events.some(isManagedMcpCallResultUnknown) ||
        run.notifications.some((notification) => isManagedMcpCallResultUnknown(notification.params))
      ) {
        throw new ManagedCapabilityCallResultUnknownError();
      }
      if (run.finalResponse)
        emit({
          type: "event",
          eventType: "runtime.output.delta",
          payload: { provider: DEEPSEEK_HARNESS_PROVIDER_KIND, text: run.finalResponse },
        });
      await generatedFiles.flush();
      return providerResult(run, model);
    } catch (error) {
      if (error instanceof ManagedCapabilityCallResultUnknownError) throw error;
      if (interrupted) throw new ProviderInterruptedError();
      throw new Error(redact(error instanceof Error ? error.message : String(error)));
    } finally {
      await harness.close().catch(() => undefined);
    }
  })();

  return {
    result,
    interrupt() {
      interrupted = true;
      void harness.close();
    },
    forceStop() {
      interrupted = true;
      void harness.close();
    },
    getResumeCursor: () => sessionId,
  };
}

export function createDeepSeekHarnessProvider(): CloudAgentProviderPluginV1 {
  const executor: ProviderRunExecutor = (input, credential, emit, options) =>
    startDeepSeekHarnessProviderRun(input, credential, emit, options);
  return createProviderPlugin({
    providerKind: DEEPSEEK_HARNESS_PROVIDER_KIND,
    displayName: "deepseek-harness",
    descriptor: { runtimeVersion: DEEPSEEK_HARNESS_VERSION },
    startRun: executor,
  });
}

function providerResult(run: RunResult, model: string): Extract<RunnerMessage, { type: "result" }> {
  return {
    type: "result",
    output: { provider: DEEPSEEK_HARNESS_PROVIDER_KIND, model, text: run.finalResponse },
    providerResumeCursor: run.sessionId,
  };
}

function handleHarnessNotification(
  notification: HarnessNotification,
  capabilityManifest: RuntimeCapabilityManifest | null,
  activeTools: Map<string, { name: string; capabilityResourceId?: string }>,
  seenToolCalls: Set<string>,
  generatedFiles: WorkspaceGeneratedFileCollector,
  emit: (message: RunnerMessage) => void,
): Error | undefined {
  if (notification.method !== "session.event") return undefined;
  const event = recordValue(notification.params.event);
  const type = stringValue(event?.type);
  const data = recordValue(event?.data);
  if (type === "assistant/message") {
    const message = recordValue(data?.message);
    const content = Array.isArray(message?.content) ? message.content : [];
    for (const blockValue of content) {
      const block = recordValue(blockValue);
      if (block?.type !== "tool-call") continue;
      recordToolCall(
        stringValue(block.id),
        stringValue(block.name),
        block.arguments,
        capabilityManifest,
        activeTools,
        seenToolCalls,
        generatedFiles,
        emit,
      );
    }
    const usage = recordValue(data?.usage);
    if (usage) emit({ type: "event", eventType: "runtime.usage", payload: usage });
    return undefined;
  }
  if (type === "tool/call") {
    recordToolCall(
      stringValue(data?.callId),
      stringValue(data?.name),
      data?.arguments,
      capabilityManifest,
      activeTools,
      seenToolCalls,
      generatedFiles,
      emit,
    );
    return undefined;
  }
  if (type === "tool/result") {
    const message = recordValue(data?.message);
    if (isManagedMcpCallResultUnknown(message))
      return new ManagedCapabilityCallResultUnknownError();
    const source = recordValue(message?.source);
    const itemId = stringValue(source?.callId) ?? "tool";
    const sourceName = stringValue(source?.name) ?? stringValue(source?.toolName);
    const activeTool = activeTools.get(itemId);
    const name = activeTool?.name ?? sourceName ?? "tool";
    const capabilityResourceId =
      activeTool?.capabilityResourceId ?? managedMcpCapabilityResourceId(name, capabilityManifest);
    const failed =
      message?.isError === true ||
      message?.status === "error" ||
      (message?.error !== undefined && message?.error !== null);
    activeTools.delete(itemId);
    emit({
      type: "event",
      eventType: "runtime.provider.activity",
      payload: {
        provider: DEEPSEEK_HARNESS_PROVIDER_KIND,
        itemType: name,
        itemId,
        status: failed ? "failed" : "completed",
        ...(capabilityResourceId ? { capabilityResourceId } : {}),
        ...(capabilityResourceId ? { supportMode: "emulated" as const } : {}),
      },
    });
    return failed ? new Error("deepseek-harness managed tool failed.") : undefined;
  }
  if (type !== "turn/end") return undefined;
  const reason = recordValue(data?.reason);
  if (reason?.kind !== "error") return undefined;
  const error = recordValue(reason.error);
  return new Error(stringValue(error?.message) ?? "deepseek-harness turn failed.");
}

function recordToolCall(
  rawItemId: string | undefined,
  rawName: string | undefined,
  rawArguments: unknown,
  capabilityManifest: RuntimeCapabilityManifest | null,
  activeTools: Map<string, { name: string; capabilityResourceId?: string }>,
  seenToolCalls: Set<string>,
  generatedFiles: WorkspaceGeneratedFileCollector,
  emit: (message: RunnerMessage) => void,
): void {
  const name = rawName ?? "tool";
  const itemId = rawItemId ?? name;
  if (seenToolCalls.has(itemId)) return;
  seenToolCalls.add(itemId);
  const args = parseRecord(rawArguments);
  const capabilityResourceId =
    managedMcpCapabilityResourceId(name, capabilityManifest) ??
    managedSkillCapabilityResourceId(name, args, capabilityManifest);
  activeTools.set(itemId, {
    name,
    ...(capabilityResourceId ? { capabilityResourceId } : {}),
  });
  if (/^(?:create_file|edit|file_write|str_replace_editor|write|write_file|write_text_file)$/iu.test(name))
    generatedFiles.observe(args?.path ?? args?.filePath ?? args?.file_path);
  emit({
    type: "event",
    eventType: "runtime.provider.activity",
    payload: {
      provider: DEEPSEEK_HARNESS_PROVIDER_KIND,
      itemType: name,
      itemId,
      status: "inProgress",
      ...(capabilityResourceId ? { capabilityResourceId } : {}),
      ...(capabilityResourceId ? { supportMode: "emulated" as const } : {}),
    },
  });
}

function managedMcpCapabilityResourceId(
  toolName: string,
  manifest: RuntimeCapabilityManifest | null,
): string | undefined {
  if (!manifest || !toolName.startsWith("mcp__")) return undefined;
  for (const binding of manifest.bindings) {
    if (binding.resourceKind !== "mcp-server") continue;
    if (toolName.startsWith(`mcp__${mcpServerName(binding.resourceId)}__`)) {
      return binding.resourceId;
    }
  }
  return undefined;
}

function managedSkillCapabilityResourceId(
  toolName: string,
  args: Record<string, unknown> | undefined,
  manifest: RuntimeCapabilityManifest | null,
): string | undefined {
  if (toolName !== "skill" || !stringValue(args?.name)) return undefined;
  const skills =
    manifest?.bindings.filter((binding) => binding.resourceKind === "skill-bundle") ?? [];
  // A single bound Bundle is authoritative for all model-visible skill names. Multiple Bundles
  // have no public skill-name-to-bundle contract, so do not guess across them.
  return skills.length === 1 ? skills[0]?.resourceId : undefined;
}

function writeModelPatch(stateRoot: string, model: string): string {
  const path = join(stateRoot, "cloud-agents-model.cordis.yml");
  writeFileSync(
    path,
    [
      "- id: llm-pi-ai",
      "  name: '@deepseek-ai/dsh-llm-pi-ai'",
      "  config:",
      "    providers:",
      `      ${DEEPSEEK_HARNESS_ROUTE}:`,
      "        api: openai-responses",
      "        apiKeyEnv: DEEPSEEK_API_KEY",
      "        baseURL: !!js process.env.DEEPSEEK_BASE_URL",
      "        models:",
      `          - id: ${JSON.stringify(model)}`,
      "            contextWindow: 128000",
      "            maxTokens: 32768",
      "",
      "- id: tool-fs",
      "  disabled: true",
      "",
      "- id: system-prompt",
      "  config:",
      "    persona: >-",
      "      Cloud Agents has already granted the current session its Host-managed workspace-write permission.",
      "      Use str_replace_editor for workspace files; do not use shell commands or request sandbox escalation fields.",
      "",
    ].join("\n"),
    { mode: 0o600 },
  );
  return path;
}

function writeCapabilityPatch(
  stateRoot: string,
  manifest: RuntimeCapabilityManifest | null,
  skillDirectories: ReadonlyArray<string>,
): string | undefined {
  const mcpBindings =
    manifest?.bindings.filter((binding) => binding.resourceKind === "mcp-server") ?? [];
  if (mcpBindings.length === 0 && skillDirectories.length === 0) return undefined;

  const lines = ["# Host-managed MCP and signed Skill Bundle composition."];
  if (skillDirectories.length > 0) {
    lines.push(
      "- id: skill-filesystem",
      "  config:",
      "    includeDefaultRoots: false",
      "    customSkillDirs:",
      ...skillDirectories.map(
        (directory) => `      - ${JSON.stringify(join(directory, "skills"))}`,
      ),
      "    watch: false",
    );
  }
  if (mcpBindings.length > 0) {
    lines.push("- insert:");
    for (const [index, binding] of mcpBindings.entries()) {
      const tokenEnvironment = capabilityTokenEnvironmentName(binding.resourceId);
      lines.push(
        `    - id: cloud-agents-managed-mcp-${index + 1}`,
        "      name: '@deepseek-ai/dsh-mcp-client'",
        "      config:",
        `        serverName: ${mcpServerName(binding.resourceId)}`,
        "        transport: streamable-http",
        '        url: !!js "process.env.CLOUD_AGENT_MCP_BROKER_URL"',
        "        headers:",
        `          Authorization: !!js '\`Bearer \${process.env.${tokenEnvironment}}\`'`,
        "        failOnStartupError: true",
        "        reconnect:",
        "          enabled: true",
        "          initialDelayMs: 500",
        "          maxDelayMs: 30000",
        "          maxAttempts: 10",
      );
    }
  }
  const path = join(stateRoot, "cloud-agents-capabilities.cordis.yml");
  writeFileSync(path, `${lines.join("\n")}\n`, { mode: 0o600 });
  return path;
}

function mcpServerName(resourceId: string): string {
  const normalized = resourceId.replace(/[^A-Za-z0-9_-]/gu, "_").slice(0, 19) || "server";
  const suffix = createHash("sha256").update(resourceId).digest("hex").slice(0, 8);
  return `ca_${normalized}_${suffix}`;
}

function applyDeepSeekHarnessCredentialEnvironment(
  environment: NodeJS.ProcessEnv,
  payload: Record<string, unknown>,
): void {
  assertOnlyKeys(payload, ["apiKey", "baseUrl", "baseURL", "model"]);
  environment.DEEPSEEK_API_KEY = requiredString(
    payload.apiKey,
    "deepseek-harness Credential apiKey",
  );
  environment.DEEPSEEK_BASE_URL = credentialBaseUrl(payload, "deepseek-harness Credential");
}

function withCredentialModel(input: RunnerInput, credential: RunnerCredential | null): RunnerInput {
  const configured = optionalString(credential?.payload.model, "deepseek-harness Credential model");
  return configured && !input.workload.model?.trim()
    ? { ...input, workload: { ...input.workload, model: configured } }
    : input;
}

function validSessionId(value: unknown): value is string {
  return typeof value === "string" && /^session-[A-Za-z0-9_-]{1,180}$/u.test(value);
}

function parseRecord(value: unknown): Record<string, unknown> | undefined {
  if (typeof value !== "string") return recordValue(value);
  try {
    return recordValue(JSON.parse(value));
  } catch {
    return undefined;
  }
}

function recordValue(value: unknown): Record<string, unknown> | undefined {
  return value && typeof value === "object" && !Array.isArray(value)
    ? (value as Record<string, unknown>)
    : undefined;
}

function stringValue(value: unknown): string | undefined {
  return typeof value === "string" && value.trim() ? value.trim() : undefined;
}

function optionalString(value: unknown, label: string): string | undefined {
  if (value === undefined) return undefined;
  if (typeof value !== "string" || !value.trim() || /[\r\n\0]/u.test(value))
    throw new Error(`${label} must be a non-empty string.`);
  return value.trim();
}

function requiredString(value: unknown, label: string): string {
  const result = optionalString(value, label);
  if (!result) throw new Error(`${label} is required.`);
  return result;
}

function credentialBaseUrl(payload: Record<string, unknown>, label: string): string {
  const lower = optionalString(payload.baseUrl, `${label} baseUrl`);
  const upper = optionalString(payload.baseURL, `${label} baseURL`);
  if (lower && upper && lower !== upper)
    throw new Error(`${label} contains conflicting baseUrl and baseURL values.`);
  const value = lower ?? upper;
  if (!value) throw new Error(`${label} requires baseUrl.`);
  const url = new URL(value);
  if (url.protocol !== "https:" && url.protocol !== "http:")
    throw new Error(`${label} baseUrl protocol is unsupported.`);
  return url.toString().replace(/\/$/u, "");
}

function assertOnlyKeys(payload: Record<string, unknown>, allowed: ReadonlyArray<string>): void {
  const allowedKeys = new Set(allowed);
  const extra = Object.keys(payload).find((key) => !allowedKeys.has(key));
  if (extra) throw new Error(`deepseek-harness Credential contains unsupported field ${extra}.`);
}
