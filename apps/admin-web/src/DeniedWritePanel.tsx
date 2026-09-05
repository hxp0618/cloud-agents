import { useEffect, useState } from "react";
import type { AdminDeniedWriteEventPage } from "@cloud-agents/cloud-agent-platform-sdk/platform";
import { adminFailure, newRequestId, type AdminClient, type SavedAdminConnection } from "./admin";
import { useI18n } from "./i18n";
import { ResourceRefresh } from "./ResourceRefresh";

export function DeniedWritePanel({
  client,
  connection,
}: Readonly<{ client: AdminClient; connection: SavedAdminConnection }>) {
  const { t, dateTime, number } = useI18n();
  const [tokens, setTokens] = useState<string[]>([""]);
  const [refresh, setRefresh] = useState(0);
  const [state, setState] = useState<
    | { kind: "loading" }
    | { kind: "loaded"; page: AdminDeniedWriteEventPage }
    | { kind: "failed"; failure: ReturnType<typeof adminFailure> }
  >({ kind: "loading" });
  const token = tokens[tokens.length - 1];
  useEffect(() => {
    const controller = new AbortController();
    setState({ kind: "loading" });
    void client
      .listAdminDeniedWriteEvents(
        connection.tenantId,
        connection.projectId,
        newRequestId(),
        25,
        token || undefined,
        AbortSignal.any([controller.signal, AbortSignal.timeout(15_000)]),
      )
      .then(({ value }) => {
        if (!controller.signal.aborted) setState({ kind: "loaded", page: value });
      })
      .catch((error: unknown) => {
        if (!controller.signal.aborted) setState({ kind: "failed", failure: adminFailure(error) });
      });
    return () => controller.abort();
  }, [client, connection.tenantId, connection.projectId, token, refresh]);
  const columns = [
    t("denied.time"),
    t("denied.action"),
    t("denied.resource"),
    t("denied.actor"),
    t("maintenance.requestId"),
  ];
  return (
    <section className="resource-list" aria-labelledby="denied-write-title">
      <div className="panel-heading">
        <div>
          <h2 id="denied-write-title">{t("denied.title")}</h2>
          <p>{t("denied.description")}</p>
        </div>
        <button
          type="button"
          className="button outline"
          disabled={state.kind === "loading"}
          onClick={() => {
            setTokens([""]);
            setRefresh((value) => value + 1);
          }}
        >
          {t("action.refresh")}
        </button>
      </div>
      {state.kind === "failed" ? (
        <div role="alert" className="operation-feedback">
          <p>{t(state.failure.key === "error.timeout" ? "denied.timeout" : state.failure.key)}</p>
          {state.failure.code ? <code>{state.failure.code}</code> : null}
        </div>
      ) : null}
      <ResourceRefresh
        loading={state.kind === "loading"}
        label={t("denied.title")}
        columns={columns}
      >
        {state.kind === "loaded" ? (
          <>
            <div className="panel target-list-panel table-scroll">
              <table className="denied-write-table" aria-label={t("denied.title")}>
                <thead>
                  <tr>
                    {columns.map((column) => (
                      <th key={column}>{column}</th>
                    ))}
                  </tr>
                </thead>
                <tbody>
                  {state.page.events.length === 0 ? (
                    <tr>
                      <td colSpan={5}>{t("denied.empty")}</td>
                    </tr>
                  ) : (
                    state.page.events.map((event) => (
                      <tr key={event.eventId}>
                        <td>
                          {dateTime(event.occurredAt)}
                          <small className="table-subline">{t("denied.status")}</small>
                        </td>
                        <td>
                          <code>{event.action}</code>
                          <small className="table-subline">{event.stableErrorCode}</small>
                        </td>
                        <td>
                          <code>{event.resourceId ?? "—"}</code>
                          {event.profileVersion === undefined ? null : (
                            <small className="table-subline">
                              {t("denied.version", { version: number(event.profileVersion) })}
                            </small>
                          )}
                        </td>
                        <td>
                          <code>{event.actor}</code>
                        </td>
                        <td>
                          <code>{event.requestId}</code>
                        </td>
                      </tr>
                    ))
                  )}
                </tbody>
              </table>
            </div>
            <nav className="list-toolbar" aria-label={t("denied.pagination")}>
              <button
                className="button outline"
                type="button"
                disabled={tokens.length === 1}
                onClick={() => setTokens((value) => value.slice(0, -1))}
              >
                {t("pagination.previous")}
              </button>
              <span>{t("denied.page", { page: number(tokens.length) })}</span>
              <button
                className="button outline"
                type="button"
                disabled={!state.page.nextPageToken}
                onClick={() => {
                  if (state.page.nextPageToken)
                    setTokens((value) => [...value, state.page.nextPageToken!]);
                }}
              >
                {t("pagination.next")}
              </button>
            </nav>
          </>
        ) : null}
      </ResourceRefresh>
    </section>
  );
}
