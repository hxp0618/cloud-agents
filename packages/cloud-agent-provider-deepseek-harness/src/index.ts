import { randomUUID } from "node:crypto";
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
  ProviderInterruptedError,
  WorkspaceGeneratedFileCollector,
  createProviderPlugin,
  hasAuthoritativeResumeData,
  providerEnvironment,
  reconstructedPrompt,
  requireProviderOuterSandboxProfile,
  validateRunnerInput,
  type ProviderRunController,
  type ProviderRunExecutor,
  type ProviderRunOptions,
  type RunnerCredential,
  type RunnerInput,
  type RunnerMessage,
} from "@cloud-agents/cloud-agent-provider-api/internal";

export const DEEPSEEK_HARNESS_PROVIDER_KIND = "deepseek-harness" as const;
const DEEPSEEK_HARNESS_VERSION = "0.1.2-rc.1";

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

  const effectiveInput = withCredentialModel(input, credential);
  const { environment, redact } = providerEnvironment(
    options.environment ?? process.env,
    credential,
    applyDeepSeekHarnessCredentialEnvironment,
  );
  const stateRoot = effectiveInput.providerStateDirectory?.trim();
  if (!stateRoot) throw new Error("deepseek-harness Provider requires providerStateDirectory.");
  const model = effectiveInput.workload.model?.trim();
  if (!model) throw new Error("deepseek-harness Provider requires model.");
  const dshHome = join(stateRoot, "dsh-home");
  mkdirSync(dshHome, { recursive: true, mode: 0o700 });
  const patchPath = writeModelPatch(stateRoot, model);
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
    patches: [patchPath],
    dshHome,
    processCwd: effectiveInput.workspaceDirectory,
    cwd: effectiveInput.workspaceDirectory,
    env: environment,
    ...(dshBin ? { dshBin } : {}),
    provider: "deepseek-official",
    model,
    initializeTimeoutMs: 30_000,
  });
  let interrupted = false;
  let turnFailure: string | undefined;
  const prompt = hasAuthoritativeResumeData(effectiveInput.workload, effectiveInput.memoryDocuments)
    ? reconstructedPrompt(effectiveInput, options.hostIdentity)
    : effectiveInput.workload.inputText;

  const result = (async (): Promise<Extract<RunnerMessage, { type: "result" }>> => {
    try {
      const run = await harness.run(prompt, {
        sessionId,
        onNotification(notification) {
          const failure = handleHarnessNotification(notification, generatedFiles, emit);
          if (failure) turnFailure = failure;
        },
      });
      if (turnFailure) throw new Error(turnFailure);
      if (run.finalResponse)
        emit({
          type: "event",
          eventType: "runtime.output.delta",
          payload: { provider: DEEPSEEK_HARNESS_PROVIDER_KIND, text: run.finalResponse },
        });
      await generatedFiles.flush();
      return providerResult(run, model);
    } catch (error) {
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
    configurationSchema: {
      type: "object",
      additionalProperties: false,
      properties: { model: { type: "string", minLength: 1 } },
    },
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
  generatedFiles: WorkspaceGeneratedFileCollector,
  emit: (message: RunnerMessage) => void,
): string | undefined {
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
      const name = stringValue(block.name) ?? "tool";
      const itemId = stringValue(block.id) ?? name;
      const args = parseRecord(block.arguments);
      if (/^(?:edit|write|str_replace_editor)$/iu.test(name))
        generatedFiles.observe(args?.path ?? args?.filePath ?? args?.file_path);
      emit({
        type: "event",
        eventType: "runtime.provider.activity",
        payload: {
          provider: DEEPSEEK_HARNESS_PROVIDER_KIND,
          itemType: name,
          itemId,
          status: "inProgress",
        },
      });
    }
    const usage = recordValue(data?.usage);
    if (usage) emit({ type: "event", eventType: "runtime.usage", payload: usage });
    return undefined;
  }
  if (type === "tool/result") {
    const message = recordValue(data?.message);
    const source = recordValue(message?.source);
    const itemId = stringValue(source?.callId) ?? "tool";
    emit({
      type: "event",
      eventType: "runtime.provider.activity",
      payload: {
        provider: DEEPSEEK_HARNESS_PROVIDER_KIND,
        itemType: "tool",
        itemId,
        status: "completed",
      },
    });
    return undefined;
  }
  if (type !== "turn/end") return undefined;
  const reason = recordValue(data?.reason);
  if (reason?.kind !== "error") return undefined;
  const error = recordValue(reason.error);
  return stringValue(error?.message) ?? "deepseek-harness turn failed.";
}

function writeModelPatch(stateRoot: string, model: string): string {
  const path = join(stateRoot, "cloud-agents-model.cordis.yml");
  writeFileSync(
    path,
    [
      "- id: llm-deepseek",
      "  config:",
      "    baseURL: !!js process.env.DEEPSEEK_BASE_URL",
      "    models:",
      `      - id: ${JSON.stringify(model)}`,
      "        contextWindow: 128000",
      "",
    ].join("\n"),
    { mode: 0o600 },
  );
  return path;
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
  if (typeof value !== "string" || !value.trim())
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
