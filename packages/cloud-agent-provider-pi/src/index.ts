import { mkdirSync, realpathSync, statSync, writeFileSync } from "node:fs";
import { isAbsolute, join, relative, resolve, sep } from "node:path";

import {
  DefaultResourceLoader,
  ModelRuntime,
  SessionManager,
  SettingsManager,
  createAgentSession,
  loadSkillsFromDir,
  type AgentSession,
  type ToolDefinition,
} from "@earendil-works/pi-coding-agent";
import type { CloudAgentProviderPluginV1 } from "@cloud-agents/cloud-agent-provider-api";
import {
  ManagedCapabilityCallResultUnknownError,
  ProviderInterruptedError,
  WorkspaceGeneratedFileCollector,
  assertCredentialKeys,
  credentialBaseUrl,
  createProviderPlugin,
  hasAuthoritativeResumeData,
  nativeResumeContinuationPrompt,
  providerEnvironment,
  optionalCredentialString,
  requiredCredentialString,
  reconstructedPrompt,
  requireProviderOuterSandboxProfile,
  validateRunnerInput,
  managedSkillBundleDirectories,
  isManagedSkillBundlePath,
  ManagedCapabilityUnavailableError,
  readCapabilityManifest,
  recordValue,
  stringValue,
  type RuntimeCapabilityBinding,
  type ProviderRunController,
  type ProviderRunExecutor,
  type ProviderRunOptions,
  type RunnerCredential,
  type RunnerInput,
  type RunnerMessage,
} from "@cloud-agents/cloud-agent-provider-api/internal";
import {
  createManagedPiMcpTools,
  type ManagedPiMcpToolMetadata,
  type ManagedPiMcpTools,
} from "./managedMcpTools";

export const PI_PROVIDER_KIND = "pi" as const;
const PI_VERSION = "0.85.1";
const MANAGED_PROVIDER = "cloud-agents-openai";
const CREDENTIAL_STRING_OPTIONS = {
  allowNull: false,
  punctuation: ".",
  singleLineMessage: false,
} as const;

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
  skillDirectories?: ReadonlyArray<string>;
  customTools?: ReadonlyArray<ToolDefinition>;
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
  validateRunnerInput(input, {
    allowEmptyInputText: options.operation !== undefined,
  });
  if (input.workload.provider.trim().toLowerCase() !== PI_PROVIDER_KIND)
    throw new Error(`Pi Provider cannot execute provider ${input.workload.provider}.`);
  requireProviderOuterSandboxProfile(options.environment ?? process.env);

  const effectiveInput = withCredentialModel(input, credential);
  const { environment, redact } = providerEnvironment(
    options.environment ?? process.env,
    credential,
    applyPiCredentialEnvironment,
  );
  const capabilityManifest = readCapabilityManifest(options.environment ?? process.env);
  const skillDirectories = managedSkillBundleDirectories(
    capabilityManifest,
    options.environment ?? process.env,
  );
  const skillBindings =
    capabilityManifest?.bindings.filter((binding) => binding.resourceKind === "skill-bundle") ?? [];
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
  const apiKey = requiredCredentialString(
    environment.CLOUD_AGENT_PI_API_KEY,
    "Pi Credential apiKey",
    CREDENTIAL_STRING_OPTIONS,
  );
  const generatedFiles = new WorkspaceGeneratedFileCollector({
    workspaceDirectory: effectiveInput.workspaceDirectory,
    provider: PI_PROVIDER_KIND,
    emit,
  });
  let session: PiSession | undefined;
  let managedMcpTools: ManagedPiMcpTools | undefined;
  let cursor: string | undefined;
  let interrupted = false;

  const result = (async (): Promise<Extract<RunnerMessage, { type: "result" }>> => {
    try {
      managedMcpTools = await createManagedPiMcpTools(
        capabilityManifest,
        options.environment ?? process.env,
      );
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
          ...(skillDirectories.length ? { skillDirectories } : {}),
          ...(managedMcpTools.tools.length ? { customTools: managedMcpTools.tools } : {}),
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
          ...(skillDirectories.length ? { skillDirectories } : {}),
          ...(managedMcpTools.tools.length ? { customTools: managedMcpTools.tools } : {}),
        });
      }
      emitPiSkillLoadEvents(skillBindings, emit);
      session.subscribe((event) =>
        handlePiEvent(
          event as unknown as Record<string, unknown>,
          generatedFiles,
          managedMcpTools?.metadataByToolName,
          managedMcpTools?.unknownToolCallIds,
          emit,
        ),
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
        await Promise.race([
          session.prompt(prompt),
          managedMcpTools.resultUnknown.then((error) => {
            void session?.abort();
            throw error;
          }),
        ]);
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
      if (error instanceof ManagedCapabilityCallResultUnknownError) throw error;
      if (interrupted) throw new ProviderInterruptedError();
      throw new Error(redact(error instanceof Error ? error.message : String(error)));
    } finally {
      session?.dispose();
      managedMcpTools?.close();
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
      managedMcpTools?.close();
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

function emitPiSkillLoadEvents(
  bindings: ReadonlyArray<RuntimeCapabilityBinding>,
  emit: (message: RunnerMessage) => void,
): void {
  bindings.forEach((binding, index) => {
    const itemId = `skill-load-${index + 1}`;
    for (const status of ["inProgress", "completed"] as const) {
      emit({
        type: "event",
        eventType: "runtime.provider.activity",
        payload: {
          provider: PI_PROVIDER_KIND,
          itemType: "skill",
          itemId,
          status,
          capabilityResourceId: binding.resourceId,
          supportMode: "native",
        },
      });
    }
  });
}

export function createPiProvider(): CloudAgentProviderPluginV1 {
  const executor: ProviderRunExecutor = (input, credential, emit, options) =>
    startPiProviderRun(input, credential, emit, options);
  return createProviderPlugin({
    providerKind: PI_PROVIDER_KIND,
    displayName: "Pi",
    descriptor: { runtimeVersion: PI_VERSION },
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
  const skills = (options.skillDirectories ?? []).flatMap((directory) => {
    const resolved = realpathSync(directory);
    if (!isManagedSkillBundlePath(resolved) || (statSync(resolved).mode & 0o222) !== 0) {
      throw new ManagedCapabilityUnavailableError(
        "Pi Skill Bundle mount must be an immutable Runtime path.",
      );
    }
    const loaded = loadSkillsFromDir({
      dir: resolved,
      source: "cloud-agents-managed",
    });
    if (loaded.diagnostics.length)
      throw new ManagedCapabilityUnavailableError("Pi Skill Bundle failed validation.");
    return loaded.skills;
  });
  const resourceLoader = new DefaultResourceLoader({
    cwd: options.cwd,
    agentDir: options.agentDirectory,
    settingsManager,
    noExtensions: true,
    noSkills: true,
    skillsOverride: () => ({ skills, diagnostics: [] }),
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
      ...(options.customTools?.length ? { customTools: [...options.customTools] } : {}),
      tools: [
        "read",
        "bash",
        "edit",
        "write",
        ...(options.customTools?.map((tool) => tool.name) ?? []),
      ],
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
          baseUrl: credentialBaseUrl(credential.payload, "Pi Credential", {
            ...CREDENTIAL_STRING_OPTIONS,
            required: true,
            validateHttp: true,
          })!,
          api: "openai-responses",
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
  assertCredentialKeys(payload, ["apiKey", "baseUrl", "baseURL", "model"], "Pi Credential", ".");
  environment.CLOUD_AGENT_PI_API_KEY = requiredCredentialString(
    payload.apiKey,
    "Pi Credential apiKey",
    CREDENTIAL_STRING_OPTIONS,
  );
  credentialBaseUrl(payload, "Pi Credential", {
    ...CREDENTIAL_STRING_OPTIONS,
    required: true,
    validateHttp: true,
  });
}

function withCredentialModel(input: RunnerInput, credential: RunnerCredential | null): RunnerInput {
  const configured = optionalCredentialString(
    credential?.payload.model,
    "Pi Credential model",
    CREDENTIAL_STRING_OPTIONS,
  );
  return configured && !input.workload.model?.trim()
    ? { ...input, workload: { ...input.workload, model: configured } }
    : input;
}

function handlePiEvent(
  event: Record<string, unknown>,
  generatedFiles: WorkspaceGeneratedFileCollector,
  managedMcpTools: ReadonlyMap<string, ManagedPiMcpToolMetadata> | undefined,
  unknownToolCallIds: ReadonlySet<string> | undefined,
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
  const managedMcpTool = managedMcpTools?.get(toolName);
  const itemId = stringValue(event.toolCallId) ?? toolName;
  if (type === "tool_execution_end" && unknownToolCallIds?.has(itemId)) return;
  const args = recordValue(event.args);
  if (type === "tool_execution_start" && /^(?:edit|write)$/iu.test(toolName))
    generatedFiles.observe(args?.path ?? args?.filePath ?? args?.file_path);
  emit({
    type: "event",
    eventType: "runtime.provider.activity",
    payload: {
      provider: PI_PROVIDER_KIND,
      itemType: managedMcpTool ? "mcp" : toolName,
      itemId,
      status:
        type === "tool_execution_start"
          ? "inProgress"
          : event.isError === true
            ? "failed"
            : "completed",
      ...(managedMcpTool
        ? {
            capabilityResourceId: managedMcpTool.capabilityResourceId,
            supportMode: "emulated" as const,
          }
        : {}),
    },
  });
}
