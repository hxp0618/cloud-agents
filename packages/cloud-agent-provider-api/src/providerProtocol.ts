// FILE: protocol.ts
// Purpose: Implements Provider Host Protocol v2 negotiation and command envelopes.

import { createInterface } from "node:readline";
import type { Readable } from "node:stream";

import {
  CLOUD_AGENT_MAX_COMMAND_BYTES as PROVIDER_HOST_MAX_COMMAND_BYTES,
  CLOUD_AGENT_PROTOCOL_VERSION as PROVIDER_HOST_PROTOCOL_VERSION,
  validateCloudAgentCommandEnvelope,
  type CloudAgentCommandEnvelope as ProviderHostCommand,
  type CloudAgentError as ProviderHostError,
  type CloudAgentMessageEnvelope as ProviderHostMessageEnvelope,
} from "@cloud-agents/cloud-agent-protocol";
import {
  hasAuthoritativeResumeData,
  validateRunnerInput,
  type ProviderPrimaryOperation,
  type ProviderRunController,
  type RunnerCredential,
  type RunnerInput,
  type RunnerMessage,
  type ProviderRunExecutor,
} from "./internalExecution";
import { normalizeRuntimeEventV2 } from "./runtimeEventV2";
import { ManagedCapabilityUnavailableError } from "./capabilityManifest";
import { ManagedCapabilityCallResultUnknownError } from "./providerRunErrors";
import {
  isRecord,
  persistConversationHistory,
  withPersistedConversationHistory,
} from "./providerConversationHistory";
import {
  capabilityMapForProvider,
  providerHostDescriptor,
  type ProviderHostDescriptor,
  type ProviderHostDescriptorOptions,
  type ProviderHostProviderKind,
  type ProviderRuntimeCompatibleRange,
  type ProviderRuntimeDescriptor,
  type ProviderVersionProbeResult,
} from "./providerDescriptor";
import {
  errorMessage,
  isInterruptedTerminalMessage,
  payloadMessage,
  protocolFallbackCommand,
  resultMessage,
  stopResultMessage,
} from "./providerProtocolMessages";
import {
  parseTextGenerationResult,
  readTextGenerationRequest,
  textGenerationPrompt,
} from "./providerTextGeneration";

const SUSPEND_TURN_CHECKPOINT_PROTOCOL = "provider-host-suspend-terminal-v1";
const MAX_IN_FLIGHT_COMMANDS = 128;
const MAX_TERMINAL_RECEIPTS = 4_096;
const STOP_SESSION_QUIESCE_TIMEOUT_MS = 5_000;
const STOP_SESSION_FORCE_TIMEOUT_MS = 1_000;

function decodeCommand(value: unknown): ProviderHostCommand {
  const validation = validateCloudAgentCommandEnvelope(value);
  if (!validation.valid) throw new Error("Command envelope is invalid.");
  return value as unknown as ProviderHostCommand;
}

export { capabilityMapForProvider, providerHostDescriptor };
export type {
  ProviderHostDescriptor,
  ProviderHostDescriptorOptions,
  ProviderHostProviderKind,
  ProviderRuntimeCompatibleRange,
  ProviderRuntimeDescriptor,
  ProviderVersionProbeResult,
};

type ProviderDescriptorFactory = (provider: ProviderHostProviderKind) => ProviderHostDescriptor;

type ProtocolState = {
  sessionInput: RunnerInput | null;
  sessionEpoch: number;
  activeOperation: {
    commandId: string;
    commandType: "SendTurn" | "CompactSession" | "StartReview" | "GenerateText";
    sessionEpoch: number;
    run: ProviderRunController;
  } | null;
  inFlightByCommandId: Map<string, Promise<ProviderHostMessageEnvelope>>;
  terminalByCommandId: Map<string, ProviderHostMessageEnvelope>;
};

type ProtocolHandler = (
  command: ProviderHostCommand,
) => Promise<ReadonlyArray<ProviderHostMessageEnvelope>>;

export function createProviderHostProtocolHandler(input: {
  credential: RunnerCredential | null;
  emit: (message: ProviderHostMessageEnvelope) => void;
  startRun?: ProviderRunExecutor;
  descriptorForProvider: ProviderDescriptorFactory;
  stopQuiesceTimeoutMs?: number;
  stopForceTimeoutMs?: number;
}): ProtocolHandler {
  const state: ProtocolState = {
    sessionInput: null,
    sessionEpoch: 0,
    activeOperation: null,
    inFlightByCommandId: new Map(),
    terminalByCommandId: new Map(),
  };
  const startRun = input.startRun ?? missingProviderExecutor;
  const descriptorForProvider = input.descriptorForProvider;

  return async (command) => {
    const cached = state.terminalByCommandId.get(command.commandId);
    if (cached) {
      input.emit(cached);
      return [cached];
    }
    const inFlight = state.inFlightByCommandId.get(command.commandId);
    if (inFlight) {
      const terminal = await inFlight;
      input.emit(terminal);
      return [terminal];
    }
    if (state.inFlightByCommandId.size >= MAX_IN_FLIGHT_COMMANDS) {
      const terminal = errorMessage(command, {
        code: "provider_unavailable",
        message: `Provider Host already has ${MAX_IN_FLIGHT_COMMANDS} commands in flight.`,
        retryable: true,
        requiresNewExecution: false,
        requiresUserAction: false,
        canReconstructFromHistory: true,
        canMoveWorker: true,
      });
      input.emit(terminal);
      return [terminal];
    }

    const terminalPromise = executeCommand(
      command,
      state,
      input.credential,
      input.emit,
      startRun,
      descriptorForProvider,
      input.stopQuiesceTimeoutMs ?? STOP_SESSION_QUIESCE_TIMEOUT_MS,
      input.stopForceTimeoutMs ?? STOP_SESSION_FORCE_TIMEOUT_MS,
    ).catch((error) => errorMessage(command, classifyProviderHostError(error)));
    state.inFlightByCommandId.set(command.commandId, terminalPromise);
    const terminal = await terminalPromise;
    state.inFlightByCommandId.delete(command.commandId);
    state.terminalByCommandId.set(command.commandId, terminal);
    trimTerminalReceipts(state.terminalByCommandId);
    input.emit(terminal);
    return [terminal];
  };
}

function trimTerminalReceipts(receipts: Map<string, ProviderHostMessageEnvelope>): void {
  while (receipts.size > MAX_TERMINAL_RECEIPTS) {
    const oldest = receipts.keys().next().value;
    if (oldest === undefined) return;
    receipts.delete(oldest);
  }
}

async function settlesWithin(
  terminal: Promise<ProviderHostMessageEnvelope>,
  timeoutMs: number,
): Promise<boolean> {
  let timeout: ReturnType<typeof setTimeout> | undefined;
  try {
    return await Promise.race([
      terminal.then(() => true),
      new Promise<boolean>((resolve) => {
        timeout = setTimeout(() => resolve(false), timeoutMs);
        timeout.unref();
      }),
    ]);
  } finally {
    if (timeout) clearTimeout(timeout);
  }
}

export async function runProviderHostProtocolV2(input: {
  source: Readable;
  credential: RunnerCredential | null;
  emit: (message: ProviderHostMessageEnvelope) => void;
  flush?: () => Promise<void>;
  startRun?: ProviderRunExecutor;
  descriptorForProvider: ProviderDescriptorFactory;
}): Promise<void> {
  const handle = createProviderHostProtocolHandler({
    credential: input.credential,
    emit: input.emit,
    ...(input.startRun ? { startRun: input.startRun } : {}),
    descriptorForProvider: input.descriptorForProvider,
  });
  const lines = createInterface({ input: input.source, crlfDelay: Infinity });
  const inFlight = new Set<Promise<ReadonlyArray<ProviderHostMessageEnvelope>>>();

  for await (const line of lines) {
    if (!line.trim()) continue;
    if (Buffer.byteLength(line) > PROVIDER_HOST_MAX_COMMAND_BYTES) {
      input.emit(
        errorMessage(protocolFallbackCommand(), {
          code: "protocol_violation",
          message: "Provider Host command exceeds the negotiated size limit.",
          retryable: false,
          requiresNewExecution: true,
          requiresUserAction: false,
          canReconstructFromHistory: true,
          canMoveWorker: true,
        }),
      );
      continue;
    }

    let parsed: unknown;
    try {
      parsed = JSON.parse(line);
    } catch {
      input.emit(
        errorMessage(protocolFallbackCommand(), {
          code: "protocol_violation",
          message: "Provider Host command is not valid JSON.",
          retryable: false,
          requiresNewExecution: true,
          requiresUserAction: false,
          canReconstructFromHistory: true,
          canMoveWorker: true,
        }),
      );
      continue;
    }

    let command: ProviderHostCommand;
    try {
      command = decodeCommand(parsed);
    } catch {
      input.emit(
        errorMessage(protocolFallbackCommand(parsed), {
          code: "protocol_violation",
          message: "Provider Host command does not match the v2 envelope.",
          retryable: false,
          requiresNewExecution: true,
          requiresUserAction: false,
          canReconstructFromHistory: true,
          canMoveWorker: true,
        }),
      );
      continue;
    }
    const task = handle(command);
    inFlight.add(task);
    task.then(
      () => inFlight.delete(task),
      () => inFlight.delete(task),
    );
  }
  await Promise.all(inFlight);
  await input.flush?.();
}

const missingProviderExecutor: ProviderRunExecutor = () => {
  throw new Error("Cloud Agent Provider executor was not injected.");
};

async function executeCommand(
  command: ProviderHostCommand,
  state: ProtocolState,
  credential: RunnerCredential | null,
  emit: (message: ProviderHostMessageEnvelope) => void,
  startRun: ProviderRunExecutor,
  descriptorForProvider: ProviderDescriptorFactory,
  stopQuiesceTimeoutMs: number,
  stopForceTimeoutMs: number,
): Promise<ProviderHostMessageEnvelope> {
  assertCompatibleProtocol(command);

  switch (command.commandType) {
    case "Describe": {
      const provider = readProvider(command.payload.provider);
      return resultMessage(command, {
        descriptor: descriptorForProvider(provider),
      });
    }
    case "StartSession":
    case "ResumeSession": {
      if (state.activeOperation) {
        throw new ProtocolFailure({
          code: "protocol_violation",
          message: `${command.commandType} cannot replace a Session while a primary operation is still active.`,
          retryable: true,
          requiresNewExecution: false,
          requiresUserAction: false,
          canReconstructFromHistory: true,
          canMoveWorker: true,
        });
      }
      let runnerInput = bindRunnerInputGeneration(
        readRunnerInput(command.payload.runnerInput),
        command.generation,
      );
      const provider = readProvider(runnerInput.workload.provider);
      const descriptor = descriptorForProvider(provider);
      assertProviderExecutionAllowed(provider, descriptor);
      if (
        command.commandType === "ResumeSession" &&
        descriptor.capabilityDescriptor.capabilities["resume-session"] === "emulated" &&
        !hasAuthoritativeResumeData(runnerInput.workload, runnerInput.memoryDocuments)
      ) {
        runnerInput = withPersistedConversationHistory(
          runnerInput,
          provider,
          missingEmulatedHistory,
        );
      }
      if (
        command.commandType === "ResumeSession" &&
        !runnerInput.providerResumeCursor?.trim() &&
        !hasAuthoritativeResumeData(runnerInput.workload, runnerInput.memoryDocuments)
      ) {
        throw new ProtocolFailure({
          code: "session_resume_invalid",
          message: "ResumeSession requires a native Cursor or authoritative history.",
          retryable: false,
          requiresNewExecution: false,
          requiresUserAction: false,
          canReconstructFromHistory: false,
          canMoveWorker: true,
        });
      }
      state.sessionEpoch += 1;
      state.sessionInput = {
        ...runnerInput,
        workload: { ...runnerInput.workload, inputText: "" },
      };
      return resultMessage(command, {
        provider,
        resumed: command.commandType === "ResumeSession",
      });
    }
    case "SendTurn": {
      if (!state.sessionInput) {
        throw new ProtocolFailure({
          code: "session_resume_invalid",
          message: "StartSession or ResumeSession must succeed before SendTurn.",
          retryable: false,
          requiresNewExecution: false,
          requiresUserAction: false,
          canReconstructFromHistory: true,
          canMoveWorker: true,
        });
      }
      const sessionEpoch = state.sessionEpoch;
      const sessionInput = state.sessionInput;
      const inputText = requiredString(command.payload.inputText, "SendTurn inputText");
      const runInput: RunnerInput = {
        ...sessionInput,
        workload: { ...sessionInput.workload, inputText },
      };
      if (state.activeOperation) {
        throw new ProtocolFailure({
          code: "protocol_violation",
          message: "Only one primary operation may be active in a Provider Session.",
          retryable: false,
          requiresNewExecution: true,
          requiresUserAction: false,
          canReconstructFromHistory: true,
          canMoveWorker: true,
        });
      }
      const run = startRun(runInput, credential, (message) => {
        // StopSession advances the epoch before waiting for the provider. Drop
        // late output so a stopped Session cannot leak into its successor.
        if (state.sessionEpoch !== sessionEpoch) return;
        if (message.type === "event") {
          emit(payloadMessage(command, "Event", normalizeRuntimeEventV2(message)));
        } else if (message.type === "artifact") {
          emit(
            payloadMessage(command, "ArtifactCandidate", {
              artifact: message.artifact,
            }),
          );
        } else if (message.type === "interaction") {
          emit(
            payloadMessage(command, "InteractionRequest", {
              ...message.payload,
              interactionType: message.interactionType,
            }),
          );
        }
      });
      state.activeOperation = {
        commandId: command.commandId,
        commandType: command.commandType,
        sessionEpoch,
        run,
      };
      let terminalResult: Extract<RunnerMessage, { type: "result" }>;
      try {
        terminalResult = await run.result;
      } finally {
        if (state.activeOperation?.commandId === command.commandId) {
          state.activeOperation = null;
        }
      }
      const outputText = terminalResult.output.text;
      if (state.sessionEpoch !== sessionEpoch || !state.sessionInput) {
        return resultMessage(command, {
          output: terminalResult.output,
          ...(terminalResult.providerResumeCursor
            ? { providerResumeCursor: terminalResult.providerResumeCursor }
            : {}),
        });
      }
      const history = [...(sessionInput.workload.conversationHistory ?? [])];
      history.push({ role: "user", text: inputText });
      if (typeof outputText === "string" && outputText.trim()) {
        history.push({ role: "assistant", text: outputText });
      }
      const nextSessionInput = {
        ...sessionInput,
        ...(terminalResult.providerResumeCursor
          ? { providerResumeCursor: terminalResult.providerResumeCursor }
          : {}),
        workload: {
          ...sessionInput.workload,
          inputText: "",
          conversationHistory: history,
        },
      };
      state.sessionInput = nextSessionInput;
      const provider = readProvider(nextSessionInput.workload.provider);
      if (
        descriptorForProvider(provider).capabilityDescriptor.capabilities["resume-session"] ===
        "emulated"
      ) {
        persistConversationHistory(nextSessionInput, provider, missingEmulatedHistory);
      }
      return resultMessage(command, {
        output: terminalResult.output,
        ...(terminalResult.providerResumeCursor
          ? { providerResumeCursor: terminalResult.providerResumeCursor }
          : {}),
      });
    }
    case "CompactSession":
    case "StartReview": {
      if (!state.sessionInput) {
        throw sessionOperationRequiresSession(command.commandType);
      }
      if (state.activeOperation) {
        throw new ProtocolFailure({
          code: "protocol_violation",
          message: "Only one primary operation may be active in a Provider Session.",
          retryable: false,
          requiresNewExecution: true,
          requiresUserAction: false,
          canReconstructFromHistory: true,
          canMoveWorker: true,
        });
      }
      const sessionEpoch = state.sessionEpoch;
      const sessionInput = state.sessionInput;
      const provider = readProvider(sessionInput.workload.provider);
      if (
        command.commandType === "CompactSession" &&
        descriptorForProvider(provider).capabilityDescriptor.capabilities.compact === "unsupported"
      ) {
        throw unsupportedSessionOperation(
          command.commandType,
          "The selected Provider does not expose a stable manual compact API.",
        );
      }
      const operation: ProviderPrimaryOperation =
        command.commandType === "CompactSession"
          ? { commandType: command.commandType, payload: command.payload }
          : {
              commandType: command.commandType,
              payload: {
                ...command.payload,
                target: readReviewTarget(command.payload.target),
              },
            };
      const run = startRun(
        sessionInput,
        credential,
        (message) => {
          if (state.sessionEpoch === sessionEpoch) emitRunnerMessage(command, message, emit);
        },
        { operation },
      );
      state.activeOperation = {
        commandId: command.commandId,
        commandType: command.commandType,
        sessionEpoch,
        run,
      };
      let terminalResult: Extract<RunnerMessage, { type: "result" }>;
      try {
        terminalResult = await run.result;
      } finally {
        if (state.activeOperation?.commandId === command.commandId) {
          state.activeOperation = null;
        }
      }
      if (
        terminalResult.providerResumeCursor &&
        state.sessionEpoch === sessionEpoch &&
        state.sessionInput
      ) {
        state.sessionInput = {
          ...sessionInput,
          providerResumeCursor: terminalResult.providerResumeCursor,
        };
      }
      return primaryOperationResultMessage(command, terminalResult);
    }
    case "GenerateText": {
      if (!state.sessionInput) {
        throw sessionOperationRequiresSession(command.commandType);
      }
      if (state.activeOperation) {
        throw new ProtocolFailure({
          code: "protocol_violation",
          message: "GenerateText cannot run while a primary Provider operation is active.",
          retryable: true,
          requiresNewExecution: false,
          requiresUserAction: false,
          canReconstructFromHistory: true,
          canMoveWorker: true,
        });
      }
      const request = readTextGenerationRequest(command.payload);
      const { providerResumeCursor: _providerResumeCursor, ...sessionInput } = state.sessionInput;
      const textRunInput: RunnerInput = {
        ...sessionInput,
        execution: {
          ...sessionInput.execution,
          id: `${sessionInput.execution.id}:text:${command.commandId}`,
        },
        workload: {
          ...sessionInput.workload,
          ...(request.model ? { model: request.model } : {}),
          inputText: textGenerationPrompt(request),
          conversationHistory: [],
          resumeSnapshot: null,
        },
      };
      const sessionEpoch = state.sessionEpoch;
      const run = startRun(textRunInput, credential, () => undefined, {
        interactive: false,
        operation: { commandType: "GenerateText", payload: command.payload },
      });
      state.activeOperation = {
        commandId: command.commandId,
        commandType: command.commandType,
        sessionEpoch,
        run,
      };
      let terminal: Extract<RunnerMessage, { type: "result" }>;
      try {
        terminal = await run.result;
      } finally {
        if (state.activeOperation?.commandId === command.commandId) {
          state.activeOperation = null;
        }
      }
      const outputText = terminal.output.text;
      if (typeof outputText !== "string" || !outputText.trim()) {
        throw new Error("GenerateText Provider returned empty output.");
      }
      return resultMessage(command, {
        result: parseTextGenerationResult(request.task, outputText),
      });
    }
    case "RollbackSession":
    case "ForkSession":
      throw unsupportedSessionOperation(
        command.commandType,
        `${command.commandType} is intentionally emulated by the Control Plane in this release.`,
      );
    case "SteerTurn": {
      const activeTurn = requireActiveOperation(state, command.commandType);
      if (activeTurn.commandType !== "SendTurn") {
        throw unsupportedActiveTurnCommand(command.commandType);
      }
      validateTargetCommandId(command.payload.targetCommandId, activeTurn.commandId);
      if (!activeTurn.run.steer) {
        throw unsupportedActiveTurnCommand(command.commandType);
      }
      const inputText = requiredString(command.payload.inputText, "SteerTurn inputText");
      await activeTurn.run.steer({ inputText });
      return resultMessage(command, {
        steered: true,
        targetCommandId: activeTurn.commandId,
      });
    }
    case "InterruptTurn": {
      const activeTurn = requireActiveOperation(state, command.commandType);
      validateTargetCommandId(command.payload.targetCommandId, activeTurn.commandId);
      activeTurn.run.interrupt();
      const providerResumeCursor = activeTurn.run.getResumeCursor?.();
      return resultMessage(command, {
        interrupted: true,
        targetCommandId: activeTurn.commandId,
        ...(providerResumeCursor ? { providerResumeCursor } : {}),
      });
    }
    case "SuspendTurn": {
      const activeTurn = requireActiveOperation(state, command.commandType);
      if (activeTurn.commandType !== "SendTurn") {
        throw unsupportedActiveTurnCommand(command.commandType);
      }
      validateTargetCommandId(command.payload.targetCommandId, activeTurn.commandId);
      const terminalPromise = state.inFlightByCommandId.get(activeTurn.commandId);
      if (!terminalPromise) {
        throw suspendTurnFailure(
          "SuspendTurn could not observe the active SendTurn terminal confirmation.",
        );
      }
      activeTurn.run.interrupt();
      const terminal = await terminalPromise;
      if (!isInterruptedTerminalMessage(terminal)) {
        const detail =
          terminal.messageType === "Error"
            ? `the active SendTurn ended with ${terminal.error.code}`
            : "the active SendTurn completed naturally";
        throw suspendTurnFailure(
          `SuspendTurn requires an interrupted terminal confirmation, but ${detail}.`,
        );
      }
      const providerResumeCursor = activeTurn.run.getResumeCursor?.()?.trim();
      if (!providerResumeCursor) {
        throw suspendTurnFailure(
          "SuspendTurn requires a non-empty providerResumeCursor after interrupted terminal confirmation.",
        );
      }
      if (state.sessionInput) {
        state.sessionInput = { ...state.sessionInput, providerResumeCursor };
      }
      return resultMessage(command, {
        quiesced: true,
        targetCommandId: activeTurn.commandId,
        checkpointProtocol: SUSPEND_TURN_CHECKPOINT_PROTOCOL,
        providerResumeCursor,
      });
    }
    case "ResolveApproval": {
      const activeTurn = requireActiveOperation(state, command.commandType);
      if (!activeTurn.run.resolveApproval) {
        throw unsupportedInteractiveCommand(command.commandType);
      }
      validateResolutionCommandPayload(command.payload, command.commandType);
      await activeTurn.run.resolveApproval(command.payload);
      return resultMessage(command, {
        acknowledged: true,
        requestId: command.payload.requestId,
      });
    }
    case "ResolveUserInput": {
      const activeTurn = requireActiveOperation(state, command.commandType);
      if (!activeTurn.run.resolveUserInput) {
        throw unsupportedInteractiveCommand(command.commandType);
      }
      validateResolutionCommandPayload(command.payload, command.commandType);
      await activeTurn.run.resolveUserInput(command.payload);
      return resultMessage(command, {
        acknowledged: true,
        requestId: command.payload.requestId,
      });
    }
    case "StopSession": {
      const activeOperation = state.activeOperation;
      // Fence state and events immediately; quiescence below only governs when
      // it is safe for the Host to start a replacement Session.
      state.sessionEpoch += 1;
      state.sessionInput = null;
      if (activeOperation) {
        const terminal = state.inFlightByCommandId.get(activeOperation.commandId);
        try {
          activeOperation.run.interrupt();
        } catch (error) {
          return stopResultMessage(command, "failed", error);
        }
        if (!terminal) {
          return stopResultMessage(
            command,
            "failed",
            new Error("StopSession could not observe the active operation terminal."),
          );
        }
        if (!(await settlesWithin(terminal, stopQuiesceTimeoutMs))) {
          if (!activeOperation.run.forceStop) {
            return stopResultMessage(command, "timed-out");
          }
          try {
            activeOperation.run.forceStop();
          } catch (error) {
            return stopResultMessage(command, "failed", error);
          }
          if (await settlesWithin(terminal, stopForceTimeoutMs)) {
            return stopResultMessage(command, "forced");
          }
          return stopResultMessage(command, "timed-out");
        }
      }
      return stopResultMessage(command, "quiesced");
    }
    default:
      throw new ProtocolFailure({
        code: "capability_unsupported",
        message: `${command.commandType} is not implemented by this Provider Host adapter.`,
        retryable: false,
        requiresNewExecution: false,
        requiresUserAction: true,
        canReconstructFromHistory: true,
        canMoveWorker: true,
      });
  }
}

function missingEmulatedHistory(): ProtocolFailure {
  return new ProtocolFailure({
    code: "session_resume_invalid",
    message: "Emulated Provider resume requires valid persisted authoritative history.",
    retryable: false,
    requiresNewExecution: false,
    requiresUserAction: false,
    canReconstructFromHistory: false,
    canMoveWorker: true,
  });
}

function assertProviderExecutionAllowed(
  provider: ProviderHostProviderKind,
  descriptor: ProviderHostDescriptor,
): void {
  const capabilityDescriptor = descriptor.capabilityDescriptor;
  if (capabilityDescriptor.supportTier === "local-only") {
    throw new ProtocolFailure({
      code: "capability_unsupported",
      message: `${provider} is Local-only and cannot run in a remote Provider Host.`,
      retryable: false,
      requiresNewExecution: false,
      requiresUserAction: true,
      canReconstructFromHistory: false,
      canMoveWorker: false,
    });
  }
  if (
    capabilityDescriptor.releasePolicy.requiresExplicitEnablement &&
    !capabilityDescriptor.releasePolicy.enabled
  ) {
    throw new ProtocolFailure({
      code: "capability_unsupported",
      message: `${provider} remote execution is experimental and is not explicitly enabled on this Provider Host.`,
      retryable: false,
      requiresNewExecution: false,
      requiresUserAction: true,
      canReconstructFromHistory: true,
      canMoveWorker: true,
    });
  }
  if (!capabilityDescriptor.runtime.available) {
    throw new ProtocolFailure({
      code: "provider_not_installed",
      message: `${capabilityDescriptor.runtime.name} is not available on this Provider Host.`,
      retryable: false,
      requiresNewExecution: false,
      requiresUserAction: true,
      canReconstructFromHistory: true,
      canMoveWorker: true,
    });
  }
  if (!capabilityDescriptor.runtime.compatible) {
    const range = capabilityDescriptor.runtime.compatibleRange;
    const maximum = range.maximumExclusive ? ` and below ${range.maximumExclusive}` : "";
    const actual = capabilityDescriptor.runtime.version
      ? `version ${capabilityDescriptor.runtime.version}`
      : "version could not be verified";
    throw new ProtocolFailure({
      code: "provider_version_incompatible",
      message: `${capabilityDescriptor.runtime.name} ${actual}; this Host requires ${range.minimumInclusive} or newer${maximum}.`,
      retryable: false,
      requiresNewExecution: false,
      requiresUserAction: true,
      canReconstructFromHistory: true,
      canMoveWorker: true,
    });
  }
}

function requireActiveOperation(
  state: ProtocolState,
  commandType: ProviderHostCommand["commandType"],
): NonNullable<ProtocolState["activeOperation"]> {
  if (state.activeOperation) return state.activeOperation;
  throw new ProtocolFailure({
    code: "session_resume_invalid",
    message: `${commandType} requires an active Provider operation.`,
    retryable: false,
    requiresNewExecution: false,
    requiresUserAction: false,
    canReconstructFromHistory: true,
    canMoveWorker: true,
  });
}

function validateTargetCommandId(value: unknown, activeCommandId: string): void {
  if (value === undefined) return;
  if (typeof value === "string" && value.trim() === activeCommandId) return;
  throw new ProtocolFailure({
    code: "protocol_violation",
    message: "Control command targetCommandId does not match the active Provider operation.",
    retryable: false,
    requiresNewExecution: false,
    requiresUserAction: false,
    canReconstructFromHistory: true,
    canMoveWorker: false,
  });
}

function unsupportedActiveTurnCommand(commandType: "SteerTurn" | "SuspendTurn"): ProtocolFailure {
  return new ProtocolFailure({
    code: "capability_unsupported",
    message: `${commandType} is not supported by the active Provider runtime.`,
    retryable: false,
    requiresNewExecution: false,
    requiresUserAction: true,
    canReconstructFromHistory: true,
    canMoveWorker: true,
  });
}

function suspendTurnFailure(message: string): ProtocolFailure {
  return new ProtocolFailure({
    code: "provider_unavailable",
    message,
    retryable: false,
    requiresNewExecution: true,
    requiresUserAction: false,
    canReconstructFromHistory: true,
    canMoveWorker: true,
  });
}

function validateResolutionCommandPayload(
  payload: Record<string, unknown>,
  commandType: "ResolveApproval" | "ResolveUserInput",
): void {
  requiredString(payload.requestId, `${commandType} requestId`);
  if (!isRecord(payload.resolution)) {
    throw new ProtocolFailure({
      code: "protocol_violation",
      message: `${commandType} resolution must be an object.`,
      retryable: false,
      requiresNewExecution: false,
      requiresUserAction: false,
      canReconstructFromHistory: true,
      canMoveWorker: false,
    });
  }
}

function unsupportedInteractiveCommand(
  commandType: "ResolveApproval" | "ResolveUserInput",
): ProtocolFailure {
  return new ProtocolFailure({
    code: "capability_unsupported",
    message: `${commandType} is not supported by the active Provider runtime.`,
    retryable: false,
    requiresNewExecution: true,
    requiresUserAction: true,
    canReconstructFromHistory: true,
    canMoveWorker: true,
  });
}

function sessionOperationRequiresSession(
  commandType: "CompactSession" | "StartReview" | "GenerateText",
): ProtocolFailure {
  return new ProtocolFailure({
    code: "session_resume_invalid",
    message: `StartSession or ResumeSession must succeed before ${commandType}.`,
    retryable: false,
    requiresNewExecution: false,
    requiresUserAction: false,
    canReconstructFromHistory: true,
    canMoveWorker: true,
  });
}

function unsupportedSessionOperation(
  commandType: "CompactSession" | "RollbackSession" | "ForkSession" | "StartReview",
  detail: string,
): ProtocolFailure {
  return new ProtocolFailure({
    code: "capability_unsupported",
    message: `${commandType} is unsupported by this Provider Host path. ${detail}`,
    retryable: false,
    requiresNewExecution: false,
    requiresUserAction: true,
    canReconstructFromHistory: true,
    canMoveWorker: true,
  });
}

function readReviewTarget(
  value: unknown,
): { type: "uncommittedChanges" } | { type: "baseBranch"; branch: string };
function readReviewTarget(
  value: unknown,
): { type: "uncommittedChanges" } | { type: "baseBranch"; branch: string } {
  if (!isRecord(value)) throw new Error("StartReview target is required");
  if (value.type === "uncommittedChanges") return { type: value.type };
  if (value.type === "baseBranch") {
    const branch = requiredString(value.branch, "StartReview target branch").trim();
    if (branch.length > 500 || /[\r\n\0]/u.test(branch)) {
      throw new Error("StartReview target branch is invalid");
    }
    return { type: value.type, branch };
  }
  throw new Error("StartReview target type is unsupported");
}

function emitRunnerMessage(
  command: ProviderHostCommand,
  message: RunnerMessage,
  emit: (message: ProviderHostMessageEnvelope) => void,
): void {
  if (message.type === "event") {
    emit(payloadMessage(command, "Event", normalizeRuntimeEventV2(message)));
  } else if (message.type === "artifact") {
    emit(
      payloadMessage(command, "ArtifactCandidate", {
        artifact: message.artifact,
      }),
    );
  } else if (message.type === "interaction") {
    emit(
      payloadMessage(command, "InteractionRequest", {
        ...message.payload,
        interactionType: message.interactionType,
      }),
    );
  }
}

function primaryOperationResultMessage(
  command: ProviderHostCommand,
  terminal: Extract<RunnerMessage, { type: "result" }>,
): ProviderHostMessageEnvelope {
  const output = terminal.output;
  const boundary = isRecord(output.boundary) ? output.boundary : undefined;
  const supportMode =
    output.supportMode === "native" || output.supportMode === "emulated"
      ? output.supportMode
      : undefined;
  const providerTurnId =
    typeof output.providerTurnId === "string" && output.providerTurnId.trim()
      ? output.providerTurnId.trim()
      : undefined;
  const summary =
    boundary && typeof boundary.summary === "string" && boundary.summary.trim()
      ? boundary.summary.trim()
      : undefined;
  return resultMessage(command, {
    output,
    ...(terminal.providerResumeCursor
      ? { providerResumeCursor: terminal.providerResumeCursor }
      : {}),
    ...(supportMode ? { supportMode } : {}),
    ...(providerTurnId ? { providerTurnId } : {}),
    ...(summary ? { summary } : {}),
    ...(boundary ? { boundary } : {}),
  });
}

function assertCompatibleProtocol(command: ProviderHostCommand): void {
  if (command.protocolVersion.major !== PROVIDER_HOST_PROTOCOL_VERSION.major) {
    throw new ProtocolFailure({
      code: "provider_version_incompatible",
      message: `Provider Host Protocol major ${command.protocolVersion.major} is not supported.`,
      retryable: false,
      requiresNewExecution: true,
      requiresUserAction: true,
      canReconstructFromHistory: true,
      canMoveWorker: true,
    });
  }
}

function readRunnerInput(value: unknown): RunnerInput {
  if (!isRecord(value)) throw new Error("runnerInput is required");
  const input = value as RunnerInput;
  try {
    validateRunnerInput(input, { allowEmptyInputText: true });
  } catch {
    throw new ProtocolFailure({
      code: "protocol_violation",
      message: "runnerInput is invalid.",
      retryable: false,
      requiresNewExecution: true,
      requiresUserAction: false,
      canReconstructFromHistory: true,
      canMoveWorker: true,
    });
  }
  return input;
}

function bindRunnerInputGeneration(input: RunnerInput, commandGeneration: number): RunnerInput {
  const inputGeneration = input.execution.generation;
  if (inputGeneration !== undefined && inputGeneration !== commandGeneration) {
    throw new ProtocolFailure({
      code: "protocol_violation",
      message: "runnerInput.execution.generation does not match command.generation.",
      retryable: false,
      requiresNewExecution: true,
      requiresUserAction: false,
      canReconstructFromHistory: true,
      canMoveWorker: true,
    });
  }
  return {
    ...input,
    execution: { ...input.execution, generation: commandGeneration },
  };
}

function readProvider(value: unknown): ProviderHostProviderKind {
  if (typeof value !== "string") throw new Error("provider is required");
  const normalized = value.trim();
  if (/^[a-z][a-z0-9._-]{0,79}$/iu.test(normalized)) return normalized;
  throw new ProtocolFailure({
    code: "provider_not_installed",
    message: `Provider ${value.trim()} is not known to this Provider Host.`,
    retryable: false,
    requiresNewExecution: false,
    requiresUserAction: true,
    canReconstructFromHistory: false,
    canMoveWorker: false,
  });
}

function classifyProviderHostError(error: unknown): ProviderHostError {
  if (error instanceof ManagedCapabilityCallResultUnknownError) {
    return errorDetail("provider_unavailable", error.message, false, true, false, false, false);
  }
  if (error instanceof ManagedCapabilityUnavailableError) {
    return errorDetail("capability_unsupported", error.message, false, false, true, true, true);
  }
  if (error instanceof ProtocolFailure) return error.detail;
  const message = error instanceof Error ? error.message : String(error);
  const normalized = message.toLowerCase();
  if (normalized.includes("interrupted")) {
    return errorDetail("interrupted", message, false, false, false, true, true);
  }
  if (normalized.includes("invalid jsonl") || normalized.includes("result message")) {
    return errorDetail("protocol_violation", message, false, true, false, true, true);
  }
  if (isProviderRateLimitError(normalized)) {
    return errorDetail("provider_rate_limited", message, true, true, false, true, true);
  }
  if (isProviderAuthenticationError(normalized)) {
    return errorDetail("authentication_required", message, false, false, true, true, true);
  }
  if (normalized.includes("credential")) {
    return errorDetail("credential_invalid", message, false, false, true, false, false);
  }
  if (normalized.includes("enoent") || normalized.includes("not found")) {
    return errorDetail("provider_not_installed", message, false, false, true, true, true);
  }
  return errorDetail("provider_unavailable", message, true, true, false, true, true);
}

function isProviderRateLimitError(normalized: string): boolean {
  return [
    "rate limit",
    "rate-limit",
    "rate_limit",
    "ratelimit",
    "too many requests",
    "resource exhausted",
    "resource_exhausted",
    "quota exceeded",
    "usage limit",
    "http 429",
    "status 429",
    "status code 429",
  ].some((marker) => normalized.includes(marker));
}

function isProviderAuthenticationError(normalized: string): boolean {
  return [
    "authentication",
    "authentication_error",
    "authentication required",
    "unauthorized",
    "invalid api key",
    "invalid_api_key",
    "not logged in",
    "login required",
    "please login",
    "please log in",
    "http 401",
    "status 401",
    "status code 401",
  ].some((marker) => normalized.includes(marker));
}

function errorDetail(
  code: ProviderHostError["code"],
  message: string,
  retryable: boolean,
  requiresNewExecution: boolean,
  requiresUserAction: boolean,
  canReconstructFromHistory: boolean,
  canMoveWorker: boolean,
): ProviderHostError {
  return {
    code,
    message: message.trim().slice(0, 2_000) || "Provider Host failed.",
    retryable,
    requiresNewExecution,
    requiresUserAction,
    canReconstructFromHistory,
    canMoveWorker,
  };
}

class ProtocolFailure extends Error {
  constructor(readonly detail: ProviderHostError) {
    super(detail.message);
  }
}

function requiredString(value: unknown, label: string): string {
  if (typeof value !== "string" || !value.trim()) throw new Error(`${label} is required`);
  return value;
}
