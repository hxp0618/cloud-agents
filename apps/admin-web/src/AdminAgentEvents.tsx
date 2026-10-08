import { useEffect, useRef, useState, type ReactNode } from "react";
import type {
  ManagedAgentEvent,
  ManagedAgentSession,
} from "@cloud-agents/cloud-agent-platform-sdk/platform";
import {
  adminFailure,
  loadAdminManagedAgentEventPage,
  type AdminClient,
  type AdminManagedAgentEventPage,
  type SavedAdminConnection,
} from "./admin";
import { useI18n } from "./i18n";

export function AdminAgentEvents({
  client,
  connection,
  sessions,
  children,
}: Readonly<{
  client: AdminClient | null;
  connection: SavedAdminConnection;
  sessions: readonly ManagedAgentSession[];
  children: (events: readonly ManagedAgentEvent[]) => ReactNode;
}>) {
  const { t, number } = useI18n();
  const [selectedSessionId, setSelectedSessionId] = useState("");
  const sessionId = sessions.some(({ metadata }) => metadata.uid === selectedSessionId)
    ? selectedSessionId
    : (sessions[0]?.metadata.uid ?? "");
  const scope = `${connection.tenantId}\0${connection.projectId}\0${sessionId}`;
  const [page, setPage] = useState<AdminManagedAgentEventPage>();
  const [loadedScope, setLoadedScope] = useState("");
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<ReturnType<typeof adminFailure> | null>(null);
  const [pageNumber, setPageNumber] = useState(0);
  const request = useRef<AbortController | null>(null);
  useEffect(() => {
    request.current?.abort();
    request.current = null;
    setPage(undefined);
    setLoadedScope("");
    setPageNumber(0);
    setLoading(false);
    setError(null);
    return () => request.current?.abort();
  }, [client, connection.tenantId, connection.projectId, sessionId]);

  async function load() {
    if (client === null || sessionId === "" || request.current !== null) return;
    const previous = loadedScope === scope ? page : undefined;
    const controller = new AbortController();
    request.current = controller;
    setLoading(true);
    setError(null);
    try {
      const next = await loadAdminManagedAgentEventPage(
        client,
        connection.tenantId,
        connection.projectId,
        sessionId,
        AbortSignal.any([controller.signal, AbortSignal.timeout(15_000)]),
        previous,
      );
      if (controller.signal.aborted) return;
      setPage(next);
      setLoadedScope(scope);
      setPageNumber((current) => current + 1);
    } catch (cause) {
      if (!controller.signal.aborted) setError(adminFailure(cause));
    } finally {
      if (request.current === controller) {
        request.current = null;
        setLoading(false);
      }
    }
  }

  const currentPage = loadedScope === scope ? page : undefined;
  return (
    <div aria-busy={loading}>
      <label>
        <span>{t("agentEvents.session")}</span>
        <select
          aria-label={t("agentEvents.session")}
          value={sessionId}
          onChange={(event) => setSelectedSessionId(event.target.value)}
          disabled={sessions.length === 0}
        >
          {sessions.map(({ metadata }) => (
            <option key={metadata.uid} value={metadata.uid}>
              {metadata.uid}
            </option>
          ))}
        </select>
      </label>
      <p role="status">
        {t(
          loading
            ? "agentEvents.loading"
            : currentPage === undefined
              ? "agentEvents.notLoaded"
              : currentPage.hasMore
                ? "agentEvents.more"
                : "agentEvents.complete",
          { page: number(pageNumber) },
        )}
      </p>
      {error === null ? null : (
        <p className="danger-text" role="alert">
          {t(error.key)}
          {error.code ? ` · ${error.code}` : ""}
        </p>
      )}
      {children(currentPage?.events ?? [])}
      {currentPage === undefined || currentPage.hasMore ? (
        <button
          className="button outline"
          type="button"
          disabled={loading || sessionId === "" || client === null}
          onClick={() => void load()}
        >
          {t(currentPage === undefined ? "agentEvents.load" : "agentEvents.loadMore")}
        </button>
      ) : null}
    </div>
  );
}
