import type {
  ManagedAgentEvent,
  McpServer,
  SkillBundle,
} from "@cloud-agents/cloud-agent-platform-sdk/platform";
import type {
  AdminCapabilityBinding,
  AdminClient,
  AdminManagedAgentRuntime,
  SavedAdminConnection,
} from "./admin";
import { AdminAgentEvents } from "./AdminAgentEvents";
import { useI18n } from "./i18n";

export function CapabilityPanel({
  mcpServers,
  skillBundles,
  bindings,
  runtime,
  client,
  connection,
  query,
  onQuery,
  onNextSessions,
  onNextExecutions,
  onSelectSession,
}: Readonly<{
  mcpServers: readonly McpServer[];
  skillBundles: readonly SkillBundle[];
  bindings: readonly AdminCapabilityBinding[];
  runtime: AdminManagedAgentRuntime;
  client: AdminClient | null;
  connection: SavedAdminConnection;
  query: string;
  onQuery: (value: string) => void;
  onNextSessions: () => void;
  onNextExecutions: () => void;
  onSelectSession: (sessionId: string) => void;
}>) {
  const { t, number } = useI18n();
  const normalized = query.trim().toLocaleLowerCase();
  const visibleMcp = mcpServers.filter(({ metadata, spec }) =>
    [metadata.uid, spec.version, spec.digest, spec.transport, spec.status, ...spec.permissions]
      .join(" ")
      .toLocaleLowerCase()
      .includes(normalized),
  );
  const visibleSkills = skillBundles.filter(({ metadata, spec }) =>
    [metadata.uid, spec.version, spec.digest, spec.status, ...spec.compatibleProviders]
      .join(" ")
      .toLocaleLowerCase()
      .includes(normalized),
  );
  const visibleBindings = bindings.filter(
    ({ resourceId, version, digest, kind, sessionIds, executionIds }) =>
      [resourceId, version, digest, kind, ...sessionIds, ...executionIds]
        .join(" ")
        .toLocaleLowerCase()
        .includes(normalized),
  );
  const filterEvents = (events: readonly ManagedAgentEvent[]) =>
    events.filter(({ spec }) => {
      if (!spec.operation.startsWith("mcp.") && !spec.operation.startsWith("skill.")) return false;
      const resourceId = spec.serverId ?? spec.bundleId ?? "";
      return [
        spec.operation,
        resourceId,
        spec.version ?? "",
        spec.digest ?? "",
        spec.result ?? "",
        spec.errorCode ?? "",
        String(spec.generation),
      ]
        .join(" ")
        .toLocaleLowerCase()
        .includes(normalized);
    });
  return (
    <section className="resource-list">
      <div className="list-toolbar">
        <input
          type="search"
          aria-label={t("search.capabilities.label")}
          placeholder={t("search.capabilities.placeholder")}
          value={query}
          onChange={(event) => onQuery(event.target.value)}
        />
        <span className="scope-chip">
          {t("capabilities.mcpCount", { count: number(visibleMcp.length) })} ·{" "}
          {t("capabilities.skillCount", { count: number(visibleSkills.length) })}
        </span>
      </div>
      <div className="panel target-list-panel">
        <h2>{t("capabilities.mcpTitle")}</h2>
        {visibleMcp.length === 0 ? (
          <div className="table-empty">{t("table.empty.mcpServers")}</div>
        ) : (
          <div className="table-scroll">
            <table>
              <thead>
                <tr>
                  <th>{t("capabilities.id")}</th>
                  <th>{t("capabilities.version")}</th>
                  <th>{t("capabilities.digest")}</th>
                  <th>{t("capabilities.transport")}</th>
                  <th>{t("capabilities.permissions")}</th>
                  <th>{t("capabilities.status")}</th>
                </tr>
              </thead>
              <tbody>
                {visibleMcp.map(({ metadata, spec }) => (
                  <tr key={metadata.uid}>
                    <td className="mono">{metadata.uid}</td>
                    <td className="mono">{spec.version}</td>
                    <td className="mono">{spec.digest}</td>
                    <td>{spec.transport}</td>
                    <td>{spec.permissions.join(", ")}</td>
                    <td>{spec.status}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
      </div>
      <div className="panel target-list-panel">
        <h2>{t("capabilities.skillTitle")}</h2>
        {visibleSkills.length === 0 ? (
          <div className="table-empty">{t("table.empty.skillBundles")}</div>
        ) : (
          <div className="table-scroll">
            <table>
              <thead>
                <tr>
                  <th>{t("capabilities.id")}</th>
                  <th>{t("capabilities.version")}</th>
                  <th>{t("capabilities.digest")}</th>
                  <th>{t("capabilities.compatibleProviders")}</th>
                  <th>{t("capabilities.readOnly")}</th>
                  <th>{t("capabilities.status")}</th>
                </tr>
              </thead>
              <tbody>
                {visibleSkills.map(({ metadata, spec }) => (
                  <tr key={metadata.uid}>
                    <td className="mono">{metadata.uid}</td>
                    <td className="mono">{spec.version}</td>
                    <td className="mono">{spec.digest}</td>
                    <td>{spec.compatibleProviders.join(", ")}</td>
                    <td>{spec.mountReadOnly ? t("common.yes") : t("common.no")}</td>
                    <td>{spec.status}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
      </div>
      <div className="panel target-list-panel">
        <h2>{t("capabilities.bindingTitle")}</h2>
        <p>{t("agentRuntime.windowDescription")}</p>
        <div className="button-row">
          <label>
            <span>{t("agentRuntime.sessionWindow")}</span>
            <select
              value={runtime.selectedSessionId}
              onChange={(event) => onSelectSession(event.target.value)}
            >
              {runtime.sessions.map(({ metadata }) => (
                <option key={metadata.uid} value={metadata.uid}>
                  {metadata.uid}
                </option>
              ))}
            </select>
          </label>
          <button
            className="button outline"
            type="button"
            disabled={runtime.nextSessionPageToken === undefined}
            onClick={onNextSessions}
          >
            {t("agentRuntime.nextSessionWindow")}
          </button>
          <button
            className="button outline"
            type="button"
            disabled={runtime.nextExecutionPageToken === undefined}
            onClick={onNextExecutions}
          >
            {t("agentRuntime.nextExecutionWindow")}
          </button>
        </div>
        {visibleBindings.length === 0 ? (
          <div className="table-empty">{t("table.empty.capabilityBindings")}</div>
        ) : (
          <div className="table-scroll">
            <table>
              <thead>
                <tr>
                  <th>{t("capabilities.id")}</th>
                  <th>{t("capabilities.version")}</th>
                  <th>{t("capabilities.digest")}</th>
                  <th>{t("capabilities.boundSessions")}</th>
                  <th>{t("capabilities.boundExecutions")}</th>
                </tr>
              </thead>
              <tbody>
                {visibleBindings.map((binding) => (
                  <tr key={`${binding.kind}:${binding.resourceId}`}>
                    <td className="mono">{binding.resourceId}</td>
                    <td className="mono">{binding.version}</td>
                    <td className="mono">{binding.digest}</td>
                    <td className="mono">{binding.sessionIds.join(", ") || "—"}</td>
                    <td className="mono">{binding.executionIds.join(", ") || "—"}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
      </div>
      <div className="panel target-list-panel">
        <h2>{t("capabilities.eventTitle")}</h2>
        <AdminAgentEvents client={client} connection={connection} sessions={runtime.sessions}>
          {(events) => {
            const visibleEvents = filterEvents(events);
            return visibleEvents.length === 0 ? (
              <div className="table-empty">{t("table.empty.capabilityEvents")}</div>
            ) : (
              <div className="table-scroll">
                <table>
                  <thead>
                    <tr>
                      <th>{t("capabilities.operation")}</th>
                      <th>{t("capabilities.id")}</th>
                      <th>{t("capabilities.version")}</th>
                      <th>{t("capabilities.digest")}</th>
                      <th>{t("capabilities.result")}</th>
                      <th>{t("capabilities.generation")}</th>
                      <th>{t("capabilities.errorCode")}</th>
                    </tr>
                  </thead>
                  <tbody>
                    {visibleEvents.map((event) => (
                      <tr key={event.metadata.uid}>
                        <td className="mono">{event.spec.operation}</td>
                        <td className="mono">
                          {event.spec.serverId ?? event.spec.bundleId ?? "—"}
                        </td>
                        <td className="mono">{event.spec.version ?? "—"}</td>
                        <td className="mono">{event.spec.digest ?? "—"}</td>
                        <td>{event.spec.result ?? "—"}</td>
                        <td className="mono">{event.spec.generation}</td>
                        <td className="mono">{event.spec.errorCode ?? "—"}</td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            );
          }}
        </AdminAgentEvents>
      </div>
      <p className="boundary-note">{t("capabilities.boundary")}</p>
    </section>
  );
}
