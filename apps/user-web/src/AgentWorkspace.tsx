import { useEffect, useRef, useState, type FormEvent } from "react";
import {
  ClientError,
  type EnvironmentLease,
  type ManagedAgentEvent,
  type ManagedAgentExecution,
  type ManagedAgentSession,
} from "@cloud-agents/cloud-agent-platform-sdk/platform";

import {
  agentArtifacts,
  agentErrorMessage,
  agentInteractions,
  executionMessageText,
  isAgentPollingFatal,
  isExecutionActive,
  loadAgentResources,
  loadSessionExecutions,
  mergeAgentEvents,
  readAgentEventBatch,
  readAgentSelection,
  writeAgentSelection,
  type AgentClient,
  type AgentArtifact,
  type AgentInteraction,
  type AgentResources,
} from "./agent";
import { newIdempotencyKey, newRequestId } from "./infrastructure";
import { InteractionCard } from "./InteractionCard";

type AgentWorkspaceProps = Readonly<{
  client: AgentClient;
  tenantId: string;
  projectId: string;
  projectName: string;
  targetPhase?: string | undefined;
  lease?: EnvironmentLease | undefined;
}>;

type ProviderKind = "codex" | "claudeAgent";
type BusyOperation = Readonly<{ key: string; label: string }>;
type PendingSubmission = Readonly<{
  sessionId: string;
  turnId: string;
  executionId: string;
  inputText: string;
}>;
type LocalPrompt = Readonly<{ turnId: string; text: string }>;
type InteractionResolution =
  | Readonly<{ kind: "approval"; decision: "accept" | "decline" }>
  | Readonly<{ kind: "user-input"; answers: Readonly<Record<string, readonly string[]>> }>;

const emptyResources: AgentResources = Object.freeze({
  sessions: Object.freeze([]),
  executions: Object.freeze([]),
});

function newResourceId(prefix: string): string {
  return `${prefix}-${crypto.randomUUID()}`;
}

function providerKind(value: string | undefined): ProviderKind {
  return value === "claudeAgent" ? "claudeAgent" : "codex";
}

function replaceSession(
  sessions: readonly ManagedAgentSession[],
  value: ManagedAgentSession,
): readonly ManagedAgentSession[] {
  return [value, ...sessions.filter(({ metadata }) => metadata.uid !== value.metadata.uid)];
}

function replaceExecution(
  executions: readonly ManagedAgentExecution[],
  value: ManagedAgentExecution,
): readonly ManagedAgentExecution[] {
  return [value, ...executions.filter(({ metadata }) => metadata.uid !== value.metadata.uid)];
}

function formatTime(value: string): string {
  const date = new Date(value);
  return Number.isNaN(date.valueOf())
    ? value
    : date.toLocaleTimeString([], { hour: "2-digit", minute: "2-digit", second: "2-digit" });
}

export function AgentWorkspace({
  client,
  tenantId,
  projectId,
  projectName,
  targetPhase,
  lease,
}: AgentWorkspaceProps) {
  const savedSelection = useRef(readAgentSelection(window.sessionStorage, tenantId, projectId));
  const [resources, setResources] = useState<AgentResources>(emptyResources);
  const [selectedSessionId, setSelectedSessionId] = useState(savedSelection.current.sessionId);
  const [selectedExecutionId, setSelectedExecutionId] = useState(
    savedSelection.current.executionId,
  );
  const [events, setEvents] = useState<readonly ManagedAgentEvent[]>([]);
  const [loadState, setLoadState] = useState<"loading" | "ready" | "error">("loading");
  const [provider, setProvider] = useState<ProviderKind>("codex");
  const [sessionFormOpen, setSessionFormOpen] = useState(false);
  const [sessionId, setSessionId] = useState("");
  const [prompt, setPrompt] = useState("");
  const [localPrompts, setLocalPrompts] = useState<readonly LocalPrompt[]>([]);
  const [pendingSubmission, setPendingSubmission] = useState<PendingSubmission>();
  const [executionRequestPending, setExecutionRequestPending] = useState(false);
  const [busy, setBusy] = useState<BusyOperation | null>(null);
  const [error, setError] = useState("");
  const [pollError, setPollError] = useState("");
  const [pollingStopped, setPollingStopped] = useState(false);
  const [initialEventsRead, setInitialEventsRead] = useState(false);
  const [resolvedInteractions, setResolvedInteractions] = useState<ReadonlySet<string>>(new Set());
  const operationControllerRef = useRef<AbortController | null>(null);
  const executionControllerRef = useRef<AbortController | null>(null);
  const busyRef = useRef(false);
  const pendingKeysRef = useRef(new Map<string, string>());
  const pendingInteractionRef = useRef(new Map<string, InteractionResolution>());
  const eventCursorRef = useRef(savedSelection.current.eventCursor);

  const selectedSession = resources.sessions.find(
    ({ metadata }) => metadata.uid === selectedSessionId,
  );
  const selectedExecution =
    resources.execution?.metadata.uid === selectedExecutionId
      ? resources.execution
      : resources.executions.find(({ metadata }) => metadata.uid === selectedExecutionId);
  const pendingExecution =
    pendingSubmission?.sessionId === selectedSessionId ? pendingSubmission : undefined;
  const readyLease = lease?.spec.observedPhase === "ready" ? lease : undefined;
  const pollingNeeded =
    selectedSession !== undefined &&
    (pendingExecution !== undefined || !initialEventsRead || isExecutionActive(selectedExecution));
  const interactions = agentInteractions(selectedExecution);
  const artifacts = agentArtifacts(selectedExecution);

  function persistSelection(
    nextSessionId: string,
    turnId: string,
    executionId: string,
    cursor = eventCursorRef.current,
  ) {
    writeAgentSelection(window.sessionStorage, {
      tenantId,
      projectId,
      sessionId: nextSessionId,
      turnId,
      executionId,
      eventCursor: cursor,
    });
  }

  useEffect(() => {
    const controller = new AbortController();
    setLoadState("loading");
    setError("");
    void loadAgentResources(
      client,
      tenantId,
      projectId,
      savedSelection.current.sessionId,
      savedSelection.current.turnId,
      savedSelection.current.executionId,
      AbortSignal.any([controller.signal, AbortSignal.timeout(15_000)]),
    )
      .then((loaded) => {
        const nextSessionId = loaded.session?.metadata.uid ?? "";
        const nextExecution = loaded.execution;
        const cursor =
          nextSessionId !== "" && nextSessionId === savedSelection.current.sessionId
            ? savedSelection.current.eventCursor
            : "";
        eventCursorRef.current = cursor;
        setResources(loaded);
        setSelectedSessionId(nextSessionId);
        setSelectedExecutionId(nextExecution?.metadata.uid ?? "");
        setProvider(providerKind(loaded.session?.spec.providerKind));
        if (nextExecution?.metadata.uid === pendingSubmission?.executionId) {
          setPendingSubmission(undefined);
          setPrompt("");
        }
        setInitialEventsRead(false);
        persistSelection(
          nextSessionId,
          nextExecution?.metadata.turnId ?? "",
          nextExecution?.metadata.uid ?? "",
          cursor,
        );
        setLoadState("ready");
      })
      .catch((cause: unknown) => {
        if (controller.signal.aborted) return;
        setLoadState("error");
        setError(agentErrorMessage(cause));
      });
    return () => controller.abort();
  }, [client, projectId, tenantId]);

  useEffect(
    () => () => {
      operationControllerRef.current?.abort();
      executionControllerRef.current?.abort();
    },
    [],
  );

  useEffect(() => {
    setResolvedInteractions(new Set());
  }, [selectedExecutionId]);

  useEffect(() => {
    if (!pollingNeeded || pollingStopped || selectedSession === undefined) return;
    const session = selectedSession;
    const turnId = selectedExecution?.metadata.turnId ?? pendingExecution?.turnId ?? "";
    const executionId = selectedExecution?.metadata.uid ?? pendingExecution?.executionId ?? "";
    const controller = new AbortController();
    let polling = false;

    const poll = async () => {
      if (document.visibilityState !== "visible" || polling) return;
      polling = true;
      const signal = AbortSignal.any([controller.signal, AbortSignal.timeout(15_000)]);
      try {
        const batch = await readAgentEventBatch(
          client,
          tenantId,
          projectId,
          session.metadata.uid,
          eventCursorRef.current,
          signal,
        );
        if (controller.signal.aborted) return;
        eventCursorRef.current = batch.nextCursor;
        setEvents((current) => mergeAgentEvents(current, batch.events));
        setInitialEventsRead(!batch.hasMore);
        persistSelection(session.metadata.uid, turnId, executionId, batch.nextCursor);
        if (turnId !== "" && executionId !== "") {
          const result = await client.getManagedAgentExecution(
            tenantId,
            projectId,
            session.metadata.uid,
            turnId,
            executionId,
            newRequestId(),
            signal,
          );
          if (controller.signal.aborted) return;
          setResources((current) => ({
            ...current,
            executions: replaceExecution(current.executions, result.value),
            execution: result.value,
          }));
          if (isExecutionActive(selectedExecution) && !isExecutionActive(result.value))
            setInitialEventsRead(false);
        }
        setPollError("");
      } catch (cause) {
        if (controller.signal.aborted) return;
        if (
          pendingExecution !== undefined &&
          cause instanceof ClientError &&
          cause.status === 404
        ) {
          setPollError("");
          return;
        }
        setPollError(agentErrorMessage(cause));
        if (isAgentPollingFatal(cause)) setPollingStopped(true);
      } finally {
        polling = false;
      }
    };

    void poll();
    const interval = window.setInterval(poll, 1_500);
    return () => {
      controller.abort();
      window.clearInterval(interval);
    };
  }, [
    client,
    initialEventsRead,
    pollingNeeded,
    pollingStopped,
    pendingExecution?.executionId,
    pendingExecution?.turnId,
    projectId,
    selectedExecution?.metadata.turnId,
    selectedExecution?.metadata.uid,
    selectedSession,
    tenantId,
  ]);

  function idempotencyKey(operationKey: string): string {
    const existing = pendingKeysRef.current.get(operationKey);
    if (existing !== undefined) return existing;
    const created = newIdempotencyKey();
    pendingKeysRef.current.set(operationKey, created);
    return created;
  }

  async function runOperation(
    key: string,
    label: string,
    operation: (signal: AbortSignal) => Promise<void>,
  ) {
    if (busyRef.current) return;
    busyRef.current = true;
    setBusy({ key, label });
    setError("");
    const controller = new AbortController();
    operationControllerRef.current = controller;
    try {
      await operation(AbortSignal.any([controller.signal, AbortSignal.timeout(150_000)]));
      pendingKeysRef.current.delete(key);
      setPollingStopped(false);
    } catch (cause) {
      setError(agentErrorMessage(cause));
    } finally {
      if (operationControllerRef.current === controller) operationControllerRef.current = null;
      busyRef.current = false;
      setBusy(null);
    }
  }

  function refreshAgentState() {
    void runOperation(
      "refresh-agent",
      "Refreshing Session and Execution authority",
      async (signal) => {
        const loaded = await loadAgentResources(
          client,
          tenantId,
          projectId,
          selectedSessionId,
          selectedExecution?.metadata.turnId ?? "",
          selectedExecutionId,
          signal,
        );
        const nextSessionId = loaded.session?.metadata.uid ?? "";
        const nextExecution = loaded.execution;
        if (nextSessionId !== selectedSessionId) {
          eventCursorRef.current = "";
          setEvents([]);
          setInitialEventsRead(false);
        }
        setResources(loaded);
        setSelectedSessionId(nextSessionId);
        setSelectedExecutionId(nextExecution?.metadata.uid ?? "");
        setProvider(providerKind(loaded.session?.spec.providerKind));
        persistSelection(
          nextSessionId,
          nextExecution?.metadata.turnId ?? "",
          nextExecution?.metadata.uid ?? "",
        );
        setLoadState("ready");
      },
    );
  }

  function selectSession(session: ManagedAgentSession) {
    if (session.metadata.uid === selectedSessionId) return;
    if (pendingSubmission !== undefined) {
      setError("Retry or refresh the pending Turn before switching Sessions.");
      return;
    }
    void runOperation(
      `select-session:${session.metadata.uid}`,
      "Loading Session history",
      async (signal) => {
        const loaded = await loadSessionExecutions(
          client,
          tenantId,
          projectId,
          session.metadata.uid,
          "",
          "",
          signal,
        );
        eventCursorRef.current = "";
        setEvents([]);
        setInitialEventsRead(false);
        setResources((current) => ({ ...current, session, ...loaded }));
        setSelectedSessionId(session.metadata.uid);
        setSelectedExecutionId(loaded.execution?.metadata.uid ?? "");
        setProvider(providerKind(session.spec.providerKind));
        persistSelection(
          session.metadata.uid,
          loaded.execution?.metadata.turnId ?? "",
          loaded.execution?.metadata.uid ?? "",
          "",
        );
      },
    );
  }

  function refreshSelectedSession() {
    if (selectedSession === undefined) return;
    void runOperation(
      `get-session:${selectedSession.metadata.uid}`,
      "Refreshing selected Session",
      async (signal) => {
        const [sessionResult, loaded] = await Promise.all([
          client.getManagedAgentSession(
            tenantId,
            projectId,
            selectedSession.metadata.uid,
            newRequestId(),
            signal,
          ),
          loadSessionExecutions(
            client,
            tenantId,
            projectId,
            selectedSession.metadata.uid,
            selectedExecution?.metadata.turnId ?? "",
            selectedExecutionId,
            signal,
          ),
        ]);
        setResources((current) => ({
          sessions: replaceSession(current.sessions, sessionResult.value),
          session: sessionResult.value,
          ...loaded,
        }));
        setSelectedExecutionId(loaded.execution?.metadata.uid ?? "");
        persistSelection(
          sessionResult.value.metadata.uid,
          loaded.execution?.metadata.turnId ?? "",
          loaded.execution?.metadata.uid ?? "",
        );
      },
    );
  }

  function createSession(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (readyLease === undefined) return;
    const body = {
      sessionId: sessionId.trim(),
      providerKind: provider,
      environmentLeaseId: readyLease.metadata.uid,
    };
    const key = `create-session:${JSON.stringify(body)}`;
    void runOperation(
      key,
      `Creating ${provider === "codex" ? "Codex" : "Claude Code"} Session`,
      async (signal) => {
        const result = await client.createManagedAgentSession(
          tenantId,
          projectId,
          newRequestId(),
          idempotencyKey(key),
          body,
          signal,
        );
        eventCursorRef.current = "";
        setEvents([]);
        setInitialEventsRead(false);
        setResources((current) => ({
          sessions: replaceSession(current.sessions, result.value),
          session: result.value,
          executions: Object.freeze([]),
        }));
        setSelectedSessionId(result.value.metadata.uid);
        setSelectedExecutionId("");
        setSessionId("");
        setSessionFormOpen(false);
        persistSelection(result.value.metadata.uid, "", "", "");
      },
    );
  }

  function closeSession() {
    if (
      selectedSession === undefined ||
      !window.confirm(`Close Session ${selectedSession.metadata.uid}? New Turns will be rejected.`)
    )
      return;
    const key = `close-session:${selectedSession.metadata.uid}`;
    void runOperation(key, "Closing Session", async (signal) => {
      const result = await client.closeManagedAgentSession(
        tenantId,
        projectId,
        selectedSession.metadata.uid,
        newRequestId(),
        idempotencyKey(key),
        signal,
      );
      setResources((current) => ({
        ...current,
        sessions: replaceSession(current.sessions, result.value),
        session: result.value,
      }));
      setInitialEventsRead(false);
    });
  }

  function selectExecution(executionId: string) {
    if (selectedSession === undefined) return;
    const summary = resources.executions.find(({ metadata }) => metadata.uid === executionId);
    if (summary === undefined) return;
    void runOperation(
      `get-execution:${executionId}`,
      "Loading Execution transcript",
      async (signal) => {
        const result = await client.getManagedAgentExecution(
          tenantId,
          projectId,
          selectedSession.metadata.uid,
          summary.metadata.turnId,
          summary.metadata.uid,
          newRequestId(),
          signal,
        );
        setResources((current) => ({
          ...current,
          executions: replaceExecution(current.executions, result.value),
          execution: result.value,
        }));
        setSelectedExecutionId(result.value.metadata.uid);
        persistSelection(
          selectedSession.metadata.uid,
          result.value.metadata.turnId,
          result.value.metadata.uid,
        );
      },
    );
  }

  function sendTurn(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (
      selectedSession === undefined ||
      selectedSession.spec.state !== "active" ||
      executionRequestPending
    )
      return;
    let submission = pendingSubmission;
    if (submission === undefined) {
      const inputText = prompt.trim();
      if (inputText === "") return;
      submission = {
        sessionId: selectedSession.metadata.uid,
        turnId: newResourceId("turn"),
        executionId: newResourceId("execution"),
        inputText,
      };
      setPendingSubmission(submission);
      setSelectedExecutionId(submission.executionId);
      setInitialEventsRead(false);
      persistSelection(submission.sessionId, submission.turnId, submission.executionId);
    }
    if (submission.sessionId !== selectedSession.metadata.uid) {
      setError("Finish the pending Turn retry before switching Session execution state.");
      return;
    }
    const turnKey = `create-turn:${submission.turnId}`;
    const executionKey = `execute:${submission.executionId}`;
    const controller = new AbortController();
    executionControllerRef.current = controller;
    setExecutionRequestPending(true);
    setError("");
    void (async () => {
      try {
        const signal = AbortSignal.any([controller.signal, AbortSignal.timeout(315_000)]);
        await client.createManagedAgentTurn(
          tenantId,
          projectId,
          submission.sessionId,
          newRequestId(),
          idempotencyKey(turnKey),
          { turnId: submission.turnId, inputText: submission.inputText },
          signal,
        );
        const result = await client.executeManagedAgent(
          tenantId,
          projectId,
          submission.sessionId,
          newRequestId(),
          idempotencyKey(executionKey),
          {
            turnId: submission.turnId,
            executionId: submission.executionId,
            inputText: submission.inputText,
            runtimeMode: "approval-required",
            interactionMode: "default",
          },
          signal,
        );
        pendingKeysRef.current.delete(turnKey);
        pendingKeysRef.current.delete(executionKey);
        setResources((current) => ({
          ...current,
          executions: replaceExecution(current.executions, result.value),
          execution: result.value,
        }));
        setSelectedExecutionId(result.value.metadata.uid);
        setLocalPrompts((current) => [
          ...current.filter(({ turnId }) => turnId !== submission.turnId),
          { turnId: submission.turnId, text: submission.inputText },
        ]);
        setPendingSubmission(undefined);
        setPrompt("");
        setInitialEventsRead(false);
        persistSelection(
          submission.sessionId,
          result.value.metadata.turnId,
          result.value.metadata.uid,
        );
      } catch (cause) {
        setError(
          controller.signal.aborted
            ? "Execution request wait was cancelled. Retry keeps the same Turn and Execution identity."
            : agentErrorMessage(cause),
        );
      } finally {
        if (executionControllerRef.current === controller) executionControllerRef.current = null;
        setExecutionRequestPending(false);
      }
    })();
  }

  function controlExecution(action: "cancel" | "interrupt") {
    if (selectedSession === undefined || selectedExecution === undefined) return;
    const key = `${action}:${selectedExecution.metadata.uid}:${selectedExecution.spec.generation}`;
    void runOperation(
      key,
      `${action === "cancel" ? "Cancelling" : "Interrupting"} Execution`,
      async (signal) => {
        const args = [
          tenantId,
          projectId,
          selectedSession.metadata.uid,
          selectedExecution.metadata.turnId,
          selectedExecution.metadata.uid,
          newRequestId(),
          idempotencyKey(key),
          { generation: selectedExecution.spec.generation },
          signal,
        ] as const;
        const result =
          action === "cancel"
            ? await client.cancelManagedAgentExecution(...args)
            : await client.interruptManagedAgentExecution(...args);
        setResources((current) => ({
          ...current,
          executions: replaceExecution(current.executions, result.value),
          execution: result.value,
        }));
        setInitialEventsRead(false);
      },
    );
  }

  function interactionKey(interaction: AgentInteraction): string {
    return `${interaction.executionId}:${interaction.generation}:${interaction.requestId}`;
  }

  function resolveApproval(
    interaction: Extract<AgentInteraction, { kind: "approval" }>,
    decision: "accept" | "decline",
  ) {
    if (
      selectedSession === undefined ||
      selectedExecution?.metadata.uid !== interaction.executionId
    )
      return;
    const identity = interactionKey(interaction);
    const existing = pendingInteractionRef.current.get(identity);
    const resolution: InteractionResolution =
      existing?.kind === "approval" ? existing : { kind: "approval", decision };
    pendingInteractionRef.current.set(identity, resolution);
    void runOperation(
      `resolve-approval:${identity}`,
      "Resolving Agent approval",
      async (signal) => {
        if (resolution.kind !== "approval") return;
        await client.resolveManagedAgentApproval(
          tenantId,
          projectId,
          selectedSession.metadata.uid,
          selectedExecution.metadata.turnId,
          interaction.executionId,
          newRequestId(),
          {
            generation: interaction.generation,
            requestId: interaction.requestId,
            decision: resolution.decision,
          },
          signal,
        );
        pendingInteractionRef.current.delete(identity);
        setResolvedInteractions((current) => new Set(current).add(identity));
        setInitialEventsRead(false);
      },
    );
  }

  function resolveUserInput(
    interaction: Extract<AgentInteraction, { kind: "user-input" }>,
    answers: Readonly<Record<string, readonly string[]>>,
  ) {
    if (
      selectedSession === undefined ||
      selectedExecution?.metadata.uid !== interaction.executionId
    )
      return;
    const identity = interactionKey(interaction);
    const existing = pendingInteractionRef.current.get(identity);
    const resolution: InteractionResolution =
      existing?.kind === "user-input" ? existing : { kind: "user-input", answers };
    pendingInteractionRef.current.set(identity, resolution);
    void runOperation(
      `resolve-input:${identity}`,
      "Submitting Agent user input",
      async (signal) => {
        if (resolution.kind !== "user-input") return;
        await client.resolveManagedAgentUserInput(
          tenantId,
          projectId,
          selectedSession.metadata.uid,
          selectedExecution.metadata.turnId,
          interaction.executionId,
          newRequestId(),
          {
            generation: interaction.generation,
            requestId: interaction.requestId,
            answers: resolution.answers,
          },
          signal,
        );
        pendingInteractionRef.current.delete(identity);
        setResolvedInteractions((current) => new Set(current).add(identity));
        setInitialEventsRead(false);
      },
    );
  }

  function downloadArtifact(artifact: AgentArtifact) {
    if (selectedSession === undefined || selectedExecution?.metadata.uid !== artifact.executionId)
      return;
    void runOperation(
      `download-artifact:${artifact.executionId}:${artifact.messageIndex}`,
      "Downloading verified Artifact",
      async (signal) => {
        const result = await client.downloadManagedAgentArtifact(
          tenantId,
          projectId,
          selectedSession.metadata.uid,
          selectedExecution.metadata.turnId,
          artifact.executionId,
          newRequestId(),
          artifact.messageIndex,
          signal,
        );
        const url = URL.createObjectURL(
          new Blob([new Uint8Array(result.data)], { type: result.contentType }),
        );
        const anchor = document.createElement("a");
        anchor.href = url;
        anchor.download = result.fileName;
        anchor.click();
        queueMicrotask(() => URL.revokeObjectURL(url));
      },
    );
  }

  function resetEventCursor() {
    eventCursorRef.current = "";
    setEvents([]);
    setInitialEventsRead(false);
    setPollingStopped(false);
    setPollError("");
    persistSelection(
      selectedSessionId,
      selectedExecution?.metadata.turnId ?? "",
      selectedExecutionId,
      "",
    );
  }

  const visiblePrompt = localPrompts.find(
    ({ turnId }) => turnId === selectedExecution?.metadata.turnId,
  );

  return (
    <>
      <main className="conversation panel">
        <div className="conversation-head">
          <div className="conversation-toolbar">
            <div>
              <small>Agent workspace</small>
              <h1>{projectName || projectId}</h1>
            </div>
            <div className="toolbar-controls">
              <label>
                <span>New session provider</span>
                <select
                  aria-label="Agent provider"
                  value={provider}
                  onChange={(event) => setProvider(event.target.value as ProviderKind)}
                  disabled={busy !== null || pendingSubmission !== undefined}
                >
                  <option value="codex">Codex</option>
                  <option value="claudeAgent">Claude Code</option>
                </select>
              </label>
              <button
                className="button secondary compact"
                type="button"
                disabled={
                  busy !== null || readyLease === undefined || pendingSubmission !== undefined
                }
                onClick={() => {
                  setSessionId((current) => current || newResourceId("session"));
                  setSessionFormOpen((current) => !current);
                }}
              >
                New session
              </button>
            </div>
          </div>

          {sessionFormOpen && readyLease ? (
            <form
              className="agent-create-form"
              aria-label="Create Agent session"
              onSubmit={createSession}
            >
              <label>
                <span>Session ID</span>
                <input
                  value={sessionId}
                  onChange={(event) => setSessionId(event.target.value)}
                  pattern="[A-Za-z0-9](?:[A-Za-z0-9._~-]{0,126}[A-Za-z0-9])?"
                  maxLength={128}
                  required
                  spellCheck={false}
                />
              </label>
              <span>
                {provider === "codex" ? "Codex" : "Claude Code"} · Lease {readyLease.metadata.name}{" "}
                · generation {readyLease.spec.generation}
              </span>
              <button className="button primary compact" type="submit" disabled={busy !== null}>
                Create
              </button>
              <button
                className="button ghost compact"
                type="button"
                onClick={() => setSessionFormOpen(false)}
              >
                Cancel
              </button>
            </form>
          ) : null}

          <div className="session-strip" aria-label="Agent sessions">
            <div className="session-tabs">
              {resources.sessions.map((session) => (
                <button
                  type="button"
                  key={session.metadata.uid}
                  className={session.metadata.uid === selectedSessionId ? "selected" : ""}
                  onClick={() => selectSession(session)}
                  disabled={busy !== null || pendingSubmission !== undefined}
                >
                  <span className={`status-dot state-${session.spec.state}`} aria-hidden="true" />
                  <span>{session.metadata.uid}</span>
                  <small>{session.spec.providerKind}</small>
                </button>
              ))}
            </div>
            <div className="session-actions">
              <button
                className="icon-button"
                type="button"
                aria-label="Refresh Agent state"
                title="Refresh Session and Execution state"
                onClick={refreshAgentState}
                disabled={busy !== null}
              >
                ↻
              </button>
              <button
                className="button ghost compact"
                type="button"
                onClick={refreshSelectedSession}
                disabled={busy !== null || selectedSession === undefined}
              >
                Get
              </button>
              <button
                className="button ghost compact danger-action"
                type="button"
                onClick={closeSession}
                disabled={
                  busy !== null ||
                  pendingSubmission !== undefined ||
                  selectedSession?.spec.state !== "active" ||
                  isExecutionActive(selectedExecution)
                }
              >
                Close
              </button>
            </div>
          </div>
        </div>

        <div className="conversation-thread" aria-live="polite">
          {loadState === "loading" ? (
            <div className="conversation-empty">
              <strong>Loading Agent authority</strong>
              <p>Fetching Sessions, Executions, and the selected transcript.</p>
            </div>
          ) : selectedSession === undefined ? (
            <div className="conversation-empty">
              <div className="agent-orbit" aria-hidden="true">
                <span>CA</span>
              </div>
              <strong>
                {readyLease ? "Create the first Agent Session" : "Ready a Lease first"}
              </strong>
              <p>
                {readyLease
                  ? "Choose Codex or Claude Code. The Session binds to the selected Lease generation."
                  : "Select and deploy an Environment Lease before starting an Agent."}
              </p>
            </div>
          ) : (
            <>
              <div className="execution-toolbar">
                <span>
                  <strong>{selectedSession.metadata.uid}</strong>
                  <small>
                    {selectedSession.spec.providerKind} · Lease{" "}
                    {selectedSession.spec.environmentLeaseId ?? "legacy"} · generation{" "}
                    {selectedSession.spec.environmentGeneration ?? "—"}
                  </small>
                </span>
                <label>
                  <span className="sr-only">Execution</span>
                  <select
                    aria-label="Execution"
                    value={selectedExecutionId}
                    onChange={(event) => selectExecution(event.target.value)}
                    disabled={
                      busy !== null ||
                      pendingSubmission !== undefined ||
                      resources.executions.length === 0
                    }
                  >
                    {resources.executions.length === 0 ? (
                      <option value="">No executions</option>
                    ) : null}
                    {resources.executions.map((execution) => (
                      <option key={execution.metadata.uid} value={execution.metadata.uid}>
                        {execution.metadata.uid} · {execution.spec.state}
                      </option>
                    ))}
                  </select>
                </label>
              </div>

              {selectedExecution === undefined ? (
                <div className="conversation-empty compact-conversation-empty">
                  <strong>
                    {selectedSession.spec.state === "active"
                      ? "Session ready for a Turn"
                      : "Session closed"}
                  </strong>
                  <p>
                    {selectedSession.spec.state === "active"
                      ? "Send a prompt to create a durable Turn and Execution."
                      : "Select an active Session or create a new one on the Ready Lease."}
                  </p>
                </div>
              ) : (
                <div className="message-list">
                  {visiblePrompt ? (
                    <article className="message user-message">
                      <header>
                        <span>You</span>
                        <code>{selectedExecution.metadata.turnId}</code>
                      </header>
                      <p>{visiblePrompt.text}</p>
                    </article>
                  ) : (
                    <div className="transcript-recovered">
                      Input restored by digest for Turn{" "}
                      <code>{selectedExecution.metadata.turnId}</code>; browser plaintext was not
                      persisted.
                    </div>
                  )}
                  {(selectedExecution.messages ?? []).map((message, index) => (
                    <article
                      className={`message agent-message message-${message.messageType.toLowerCase()}`}
                      key={`${message.requestId}:${message.commandId}:${index}`}
                    >
                      <header>
                        <span>{message.messageType}</span>
                        <time dateTime={message.occurredAt}>{formatTime(message.occurredAt)}</time>
                      </header>
                      <p>{executionMessageText(message)}</p>
                    </article>
                  ))}
                  {(selectedExecution.messages ?? []).length === 0 ? (
                    <div className="execution-pending" role="status">
                      <span className="status-dot" aria-hidden="true" />
                      <span>
                        <strong>Execution {selectedExecution.spec.state}</strong>
                        <small>Transcript messages will appear as the Runtime reports them.</small>
                      </span>
                    </div>
                  ) : null}
                </div>
              )}
            </>
          )}
        </div>

        <form className="prompt-bar" aria-label="Agent prompt" onSubmit={sendTurn}>
          <label className="sr-only" htmlFor="prompt">
            Prompt
          </label>
          <textarea
            id="prompt"
            value={pendingSubmission?.inputText ?? prompt}
            onChange={(event) => setPrompt(event.target.value)}
            placeholder={
              selectedSession?.spec.state === "active"
                ? "Ask the Agent to work in this Lease workspace"
                : "Create or select an active Session"
            }
            rows={2}
            maxLength={1_048_576}
            disabled={
              busy !== null ||
              pendingSubmission !== undefined ||
              selectedSession?.spec.state !== "active"
            }
            required
          />
          <div className="prompt-actions">
            {isExecutionActive(selectedExecution) ? (
              <div className="execution-controls">
                <button
                  className="button ghost compact"
                  type="button"
                  onClick={() => controlExecution("cancel")}
                  disabled={busy !== null}
                >
                  Cancel
                </button>
                <button
                  className="button ghost compact danger-action"
                  type="button"
                  onClick={() => controlExecution("interrupt")}
                  disabled={busy !== null}
                >
                  Interrupt
                </button>
              </div>
            ) : null}
            <button
              className="button primary"
              type="submit"
              disabled={
                busy !== null ||
                executionRequestPending ||
                selectedSession?.spec.state !== "active" ||
                (pendingSubmission === undefined && prompt.trim() === "")
              }
            >
              {executionRequestPending ? "Running" : pendingSubmission ? "Retry send" : "Send"}
            </button>
          </div>
        </form>
      </main>

      <aside className="activity panel" aria-label="Activity and interactions">
        <div className="panel-heading activity-heading">
          <span>
            <small>Live state</small>
            <h2>Activity</h2>
          </span>
          <span
            className={`live-badge ${busy || executionRequestPending || isExecutionActive(selectedExecution) ? "running" : ""}`}
          >
            {busy || executionRequestPending
              ? "Working"
              : (selectedExecution?.spec.state ?? "Idle")}
          </span>
        </div>
        <dl className="status-table">
          <div>
            <dt>Target</dt>
            <dd>{targetPhase ?? "Not selected"}</dd>
          </div>
          <div>
            <dt>Lease</dt>
            <dd>{lease?.spec.observedPhase ?? "Not created"}</dd>
          </div>
          <div>
            <dt>Worker</dt>
            <dd>{lease?.spec.workerEndpoint ? "Online" : "Offline"}</dd>
          </div>
          <div>
            <dt>Execution</dt>
            <dd>{selectedExecution?.spec.state ?? "Idle"}</dd>
          </div>
        </dl>

        {busy ? (
          <div className="operation-stage" role="status" aria-live="polite">
            <span className="status-dot" aria-hidden="true" />
            <span>
              <strong>{busy.label}</strong>
              <small>
                Duplicate submission is disabled; retry identity and request body stay stable.
              </small>
            </span>
            <button type="button" onClick={() => operationControllerRef.current?.abort()}>
              Cancel wait
            </button>
          </div>
        ) : null}

        {executionRequestPending && pendingSubmission ? (
          <div className="operation-stage" role="status" aria-live="polite">
            <span className="status-dot" aria-hidden="true" />
            <span>
              <strong>Running Agent Execution</strong>
              <small>
                Polling durable state; interactions and execution controls remain available.
              </small>
            </span>
            <button type="button" onClick={() => executionControllerRef.current?.abort()}>
              Cancel wait
            </button>
          </div>
        ) : null}

        {error || pollError ? (
          <details className="diagnostic" open>
            <summary>Agent operation needs attention</summary>
            <p>{error || pollError}</p>
            <div className="diagnostic-actions">
              <button className="button ghost compact" type="button" onClick={refreshAgentState}>
                Refresh state
              </button>
              {pollingStopped ? (
                <button className="button ghost compact" type="button" onClick={resetEventCursor}>
                  Reset event cursor
                </button>
              ) : null}
            </div>
          </details>
        ) : null}

        {interactions.length > 0 ? (
          <section className="interaction-stack" aria-label="Agent interactions">
            <div className="activity-section-title">
              <strong>Interactions</strong>
              <small>{interactions.length} request(s)</small>
            </div>
            {interactions.map((interaction) => {
              const identity = interactionKey(interaction);
              return (
                <InteractionCard
                  key={identity}
                  interaction={interaction}
                  active={isExecutionActive(selectedExecution)}
                  disabled={busy !== null || !isExecutionActive(selectedExecution)}
                  resolved={resolvedInteractions.has(identity)}
                  onApproval={(decision) =>
                    interaction.kind === "approval"
                      ? resolveApproval(interaction, decision)
                      : undefined
                  }
                  onUserInput={(answers) =>
                    interaction.kind === "user-input"
                      ? resolveUserInput(interaction, answers)
                      : undefined
                  }
                />
              );
            })}
          </section>
        ) : null}

        {artifacts.length > 0 ? (
          <section className="artifact-stack" aria-label="Execution Artifacts">
            <div className="activity-section-title">
              <strong>Artifacts</strong>
              <small>{artifacts.length} available</small>
            </div>
            {artifacts.map((artifact) => (
              <div className="artifact-row" key={artifact.messageIndex}>
                <span>
                  <strong>{artifact.kind}</strong>
                  <small>
                    {artifact.path} · {artifact.contentType}
                    {artifact.reportedSize === undefined ? "" : ` · ${artifact.reportedSize} B`}
                  </small>
                </span>
                <button
                  className="button ghost compact"
                  type="button"
                  disabled={busy !== null}
                  onClick={() => downloadArtifact(artifact)}
                >
                  Download
                </button>
              </div>
            ))}
          </section>
        ) : null}

        <div className="activity-timeline">
          <div className="timeline-title">
            <strong>Lifecycle events</strong>
            <small>{eventCursorRef.current ? "cursor active" : "from origin"}</small>
          </div>
          {events.length === 0 ? (
            <div className="timeline-empty">
              <span className="timeline-rule" aria-hidden="true" />
              <strong>No Session events loaded</strong>
              <p>Turn and Execution state changes appear here through bounded cursor polling.</p>
            </div>
          ) : (
            <ol className="event-list">
              {events
                .slice(-32)
                .toReversed()
                .map((event) => (
                  <li key={event.metadata.uid}>
                    <span className="event-node" aria-hidden="true" />
                    <div>
                      <strong>{event.spec.operation}</strong>
                      <small>
                        {event.spec.executionId ?? event.spec.turnId ?? event.spec.resource} · gen{" "}
                        {event.spec.generation}
                      </small>
                    </div>
                    <time dateTime={event.metadata.occurredAt}>
                      {formatTime(event.metadata.occurredAt)}
                    </time>
                  </li>
                ))}
            </ol>
          )}
        </div>
      </aside>
    </>
  );
}
