import { mkdirSync, realpathSync, statSync, writeFileSync } from "node:fs";
import { isAbsolute, join, relative, resolve, sep } from "node:path";

import {
  DefaultResourceLoader,
  ModelRuntime,
  SessionManager,
  SettingsManager,
  createAgentSession,
  type AgentSession,
} from "@earendil-works/pi-coding-agent";
import type { CloudAgentProviderPluginV1 } from "@cloud-agents/cloud-agent-provider-api";
import {
  ProviderInterruptedError,
  WorkspaceGeneratedFileCollector,
  createProviderPlugin,
  hasAuthoritativeResumeData,
  nativeResumeContinuationPrompt,
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

export const PI_PROVIDER_KIND = "pi" as const;
const PI_VERSION = "0.85.1";
const MANAGED_PROVIDER = "cloud-agents-openai";

type PiSession = Pick<
  AgentSession,
  | "abort"
  | "compact"
  | "dispose"
  | "getLastAssistantText"
  | "prompt"
  | "sessionFile"
  | "steer"
  | "subscribe"
>;

type PiSessionFactoryOptions = Readonly<{
  cwd: string;
  agentDirectory: string;
  modelsPath: string;
  sessionManager: SessionManager;
  model: string;
  apiKey: string;
}>;

type PiRunOptions = ProviderRunOptions & {
  readonly sessionFactory?: (options: PiSessionFactoryOptions) => Promise<PiSession>;
};

export function startPiProviderRun(
  input: RunnerInput,
  credential: RunnerCredential | null,
  emit: (message: RunnerMessage) => void,
  options: PiRunOptions = {},
): ProviderRunController {
  validateRunnerInput(input, { allowEmptyInputText: options.operation !== undefined });
  if (input.workload.provider.trim().toLowerCase() !== PI_PROVIDER_KIND)
    throw new Error(`Pi Provider cannot execute provider ${input.workload.provider}.`);
  requireProviderOuterSandboxProfile(options.environment ?? process.env);

  const effectiveInput = withCredentialModel(input, credential);
  const { environment, redact } = providerEnvironment(
    options.environment ?? process.env,
    credential,
    applyPiCredentialEnvironment,
  );
  const stateRoot = effectiveInput.providerStateDirectory?.trim();
  if (!stateRoot) throw new Error("Pi Provider requires providerStateDirectory.");
  const model = effectiveInput.workload.model?.trim();
  if (!model) throw new Error("Pi Provider requires model.");
  const sessionDirectory = join(stateRoot, "sessions");
  const { agentDirectory, modelsPath } = configurePi(
    stateRoot,
    sessionDirectory,
    model,
    credential,
  );
  const apiKey = requiredString(environment.CLOUD_AGENT_PI_API_KEY, "Pi Credential apiKey");
  const generatedFiles = new WorkspaceGeneratedFileCollector({
    workspaceDirectory: effectiveInput.workspaceDirectory,
    provider: PI_PROVIDER_KIND,
    emit,
  });
  let session: PiSession | undefined;
  let cursor: string | undefined;
  let interrupted = false;

  const result = (async (): Promise<Extract<RunnerMessage, { type: "result" }>> => {
    try {
      const requestedCursor = stringValue(effectiveInput.providerResumeCursor);
      let resumed = requestedCursor !== undefined;
      try {
        session = await (options.sessionFactory ?? createPiSession)({
          cwd: effectiveInput.workspaceDirectory,
          agentDirectory,
          modelsPath,
          sessionManager: requestedCursor
            ? SessionManager.open(
                piSessionPath(requestedCursor, sessionDirectory),
                sessionDirectory,
                effectiveInput.workspaceDirectory,
              )
            : SessionManager.create(effectiveInput.workspaceDirectory, sessionDirectory),
          model,
          apiKey,
        });
      } catch (error) {
        if (
          !requestedCursor ||
          !hasAuthoritativeResumeData(effectiveInput.workload, effectiveInput.memoryDocuments)
        )
          throw error;
        resumed = false;
        emit({
          type: "event",
          eventType: "runtime.provider.warning",
          payload: {
            provider: PI_PROVIDER_KIND,
            kind: "session_resume",
            attemptedStrategy: "native-cursor",
            selectedStrategy: "authoritative-history",
            outcome: "fallback_selected",
            reasonCode: "session_resume_invalid",
            fallbackSafety: "before_turn_activity",
          },
        });
        session = await (options.sessionFactory ?? createPiSession)({
          cwd: effectiveInput.workspaceDirectory,
          agentDirectory,
          modelsPath,
          sessionManager: SessionManager.create(
            effectiveInput.workspaceDirectory,
            sessionDirectory,
          ),
          model,
          apiKey,
        });
      }
      session.subscribe((event) =>
        handlePiEvent(event as unknown as Record<string, unknown>, generatedFiles, emit),
      );
      if (interrupted) {
        await session.abort();
        throw new ProviderInterruptedError();
      }
      const prompt = resumed
        ? (nativeResumeContinuationPrompt(effectiveInput, options.hostIdentity) ??
          effectiveInput.workload.inputText)
        : hasAuthoritativeResumeData(effectiveInput.workload, effectiveInput.memoryDocuments)
          ? reconstructedPrompt(effectiveInput, options.hostIdentity)
          : effectiveInput.workload.inputText;
      cursor = session.sessionFile;
      if (options.operation?.commandType === "CompactSession") {
        await session.compact();
      } else if (
        options.operation?.commandType &&
        options.operation.commandType !== "GenerateText"
      ) {
        throw new Error(`Pi Provider does not support ${options.operation.commandType}.`);
      } else {
        await session.prompt(prompt);
      }
      cursor = session.sessionFile ?? cursor;
      const text = session.getLastAssistantText() ?? "";
      await generatedFiles.flush();
      return {
        type: "result",
        output: { provider: PI_PROVIDER_KIND, model, text },
        ...(cursor ? { providerResumeCursor: cursor } : {}),
      };
    } catch (error) {
      if (interrupted) throw new ProviderInterruptedError();
      throw new Error(redact(error instanceof Error ? error.message : String(error)));
    } finally {
      session?.dispose();
    }
  })();

  return {
    result,
    interrupt() {
      interrupted = true;
      void session?.abort();
    },
    forceStop() {
      interrupted = true;
      session?.dispose();
    },
    getResumeCursor: () => cursor,
    steer(payload) {
      const text = stringValue(payload.text) ?? stringValue(payload.inputText);
      if (!text) throw new Error("Pi steer requires text.");
      if (!session) throw new Error("Pi Session is not ready.");
      return session.steer(text);
    },
  };
}

export function createPiProvider(): CloudAgentProviderPluginV1 {
  const executor: ProviderRunExecutor = (input, credential, emit, options) =>
    startPiProviderRun(input, credential, emit, options);
  return createProviderPlugin({
    providerKind: PI_PROVIDER_KIND,
    displayName: "Pi",
    descriptor: { runtimeVersion: PI_VERSION },
    configurationSchema: {
      type: "object",
      additionalProperties: false,
      properties: { model: { type: "string", minLength: 1 } },
    },
    startRun: executor,
  });
}

async function createPiSession(options: PiSessionFactoryOptions): Promise<PiSession> {
  const modelRuntime = await ModelRuntime.create({
    authPath: join(options.agentDirectory, "auth.json"),
    modelsPath: options.modelsPath,
    modelsStorePath: join(options.agentDirectory, "models-store.json"),
    allowModelNetwork: false,
    refreshOnCreate: false,
  });
  await modelRuntime.setRuntimeApiKey(MANAGED_PROVIDER, options.apiKey);
  const model = modelRuntime.getModel(MANAGED_PROVIDER, options.model);
  if (!model) throw new Error(`Pi model ${options.model} is unavailable.`);
  const settingsManager = SettingsManager.inMemory(
    {
      defaultProjectTrust: "never",
      packages: [],
      extensions: [],
      skills: [],
      prompts: [],
      themes: [],
    },
    { projectTrusted: false },
  );
  const resourceLoader = new DefaultResourceLoader({
    cwd: options.cwd,
    agentDir: options.agentDirectory,
    settingsManager,
    noExtensions: true,
    noSkills: true,
    noPromptTemplates: true,
    noThemes: true,
    noContextFiles: true,
  });
  await resourceLoader.reload();
  return (
    await createAgentSession({
      cwd: options.cwd,
      agentDir: options.agentDirectory,
      modelRuntime,
      model,
      sessionManager: options.sessionManager,
      settingsManager,
      resourceLoader,
      tools: ["read", "bash", "edit", "write"],
    })
  ).session;
}

function configurePi(
  stateRoot: string,
  sessionDirectory: string,
  model: string,
  credential: RunnerCredential | null,
): { readonly agentDirectory: string; readonly modelsPath: string } {
  const agentDirectory = join(stateRoot, "agent");
  const modelsPath = join(agentDirectory, "models.json");
  mkdirSync(agentDirectory, { recursive: true, mode: 0o700 });
  mkdirSync(sessionDirectory, { recursive: true, mode: 0o700 });
  if (!credential) throw new Error("Pi Provider requires Credential.");
  writeFileSync(
    modelsPath,
    JSON.stringify({
      providers: {
        [MANAGED_PROVIDER]: {
          baseUrl: credentialBaseUrl(credential.payload, "Pi Credential"),
          api: "openai-completions",
          apiKey: "$CLOUD_AGENT_PI_API_KEY",
          authHeader: true,
          models: [{ id: model, contextWindow: 128_000, maxTokens: 16_384 }],
        },
      },
    }),
    { mode: 0o600 },
  );
  return { agentDirectory, modelsPath };
}

function piSessionPath(value: string, sessionDirectory: string): string {
  if (!isAbsolute(value)) throw new Error("Pi Provider resume cursor must be an absolute path.");
  const root = realpathSync(resolve(sessionDirectory));
  const path = realpathSync(resolve(value));
  const child = relative(root, path);
  if (
    !child ||
    child === ".." ||
    child.startsWith(`..${sep}`) ||
    isAbsolute(child) ||
    !path.endsWith(".jsonl") ||
    !statSync(path).isFile()
  )
    throw new Error("Pi Provider resume cursor is outside its durable session directory.");
  return path;
}

function applyPiCredentialEnvironment(
  environment: NodeJS.ProcessEnv,
  payload: Record<string, unknown>,
): void {
  assertOnlyKeys(payload, ["apiKey", "baseUrl", "baseURL", "model"]);
  environment.CLOUD_AGENT_PI_API_KEY = requiredString(payload.apiKey, "Pi Credential apiKey");
  credentialBaseUrl(payload, "Pi Credential");
}

function withCredentialModel(input: RunnerInput, credential: RunnerCredential | null): RunnerInput {
  const configured = optionalString(credential?.payload.model, "Pi Credential model");
  return configured && !input.workload.model?.trim()
    ? { ...input, workload: { ...input.workload, model: configured } }
    : input;
}

function handlePiEvent(
  event: Record<string, unknown>,
  generatedFiles: WorkspaceGeneratedFileCollector,
  emit: (message: RunnerMessage) => void,
): void {
  const type = stringValue(event.type);
  if (type === "message_update") {
    const delta = recordValue(event.assistantMessageEvent);
    if (delta?.type === "text_delta" && typeof delta.delta === "string")
      emit({
        type: "event",
        eventType: "runtime.output.delta",
        payload: { provider: PI_PROVIDER_KIND, text: delta.delta },
      });
    const usage = recordValue(event.usage);
    if (usage) emit({ type: "event", eventType: "runtime.usage", payload: usage });
    return;
  }
  if (type !== "tool_execution_start" && type !== "tool_execution_end") return;
  const toolName = stringValue(event.toolName) ?? "tool";
  const itemId = stringValue(event.toolCallId) ?? toolName;
  const args = recordValue(event.args);
  if (type === "tool_execution_start" && /^(?:edit|write)$/iu.test(toolName))
    generatedFiles.observe(args?.path ?? args?.filePath ?? args?.file_path);
  emit({
    type: "event",
    eventType: "runtime.provider.activity",
    payload: {
      provider: PI_PROVIDER_KIND,
      itemType: toolName,
      itemId,
      status:
        type === "tool_execution_start"
          ? "inProgress"
          : event.isError === true
            ? "failed"
            : "completed",
    },
  });
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
  if (extra) throw new Error(`Pi Credential contains unsupported field ${extra}.`);
}
