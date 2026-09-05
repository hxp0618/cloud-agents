import { useState, type FormEvent } from "react";
import type {
  AdminAuditEvent,
  EnvironmentProfile,
  NetworkPolicy,
  NetworkPolicySetRequest,
} from "@cloud-agents/cloud-agent-platform-sdk/platform";
import {
  listAdminNetworkPolicyAuditEvents,
  newRequestId,
  replaceNetworkPolicy,
  type AdminClient,
  type SavedAdminConnection,
} from "./admin";
import { useI18n, type MessageKey } from "./i18n";

function formFrom(policy?: NetworkPolicy) {
  return {
    expectedResourceVersion: policy?.metadata.resourceVersion ?? "0",
    policyId: policy?.metadata.uid ?? "",
    policyName: policy?.metadata.name ?? "",
    userSummary: policy?.spec.userSummary ?? "",
    defaultEgress: policy?.spec.defaultEgress ?? "public",
    allowlistPolicyRef: policy?.spec.allowlistPolicyRef ?? "",
    ingressEnabled: policy?.spec.ingressEnabled ?? false,
    previewEnabled: policy?.spec.previewEnabled ?? false,
    dnsPolicyRef: policy?.spec.dnsPolicyRef ?? "",
    proxyPolicyRef: policy?.spec.proxyPolicyRef ?? "",
  };
}

export function NetworkPolicyPanel({
  client,
  connection,
  policies,
  profiles,
  query,
  busy,
  onQuery,
  onChange,
  run,
  idempotencyKey,
}: Readonly<{
  client: AdminClient;
  connection: SavedAdminConnection;
  policies: readonly NetworkPolicy[];
  profiles: readonly EnvironmentProfile[];
  query: string;
  busy: boolean;
  onQuery: (value: string) => void;
  onChange: (value: readonly NetworkPolicy[]) => void;
  run: (
    key: string,
    message: { key: MessageKey },
    action: (signal: AbortSignal) => Promise<void>,
  ) => Promise<void>;
  idempotencyKey: (key: string) => string;
}>) {
  const { t, number, dateTime } = useI18n();
  const [selectedId, setSelectedId] = useState("");
  const [form, setForm] = useState(formFrom);
  const [audit, setAudit] = useState<readonly AdminAuditEvent[]>([]);
  const selected = policies.find(({ metadata }) => metadata.uid === selectedId);
  const referenced = profiles.some(({ spec }) => spec.networkPolicyRef === selectedId);
  const visible = policies.filter(({ metadata, spec }) =>
    [metadata.uid, metadata.name, spec.userSummary, spec.defaultEgress]
      .join(" ")
      .toLocaleLowerCase()
      .includes(query.trim().toLocaleLowerCase()),
  );

  function select(policyId: string) {
    void run("network-detail", { key: "operation.networkPolicyDetail" }, async (signal) => {
      const [result, events] = await Promise.all([
        client.getAdminNetworkPolicy(
          connection.tenantId,
          connection.projectId,
          policyId,
          newRequestId(),
          signal,
        ),
        listAdminNetworkPolicyAuditEvents(
          client,
          connection.tenantId,
          connection.projectId,
          policyId,
          signal,
        ),
      ]);
      onChange(replaceNetworkPolicy(policies, result.value));
      setSelectedId(policyId);
      setForm(formFrom(result.value));
      setAudit(events);
    });
  }

  function save(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (busy || referenced) return;
    const policyId = form.policyId.trim();
    const body: NetworkPolicySetRequest = {
      expectedResourceVersion: form.expectedResourceVersion,
      policyName: form.policyName.trim(),
      userSummary: form.userSummary.trim(),
      defaultEgress: form.defaultEgress,
      ingressEnabled: form.ingressEnabled,
      previewEnabled: form.previewEnabled,
      ...(form.allowlistPolicyRef.trim()
        ? { allowlistPolicyRef: form.allowlistPolicyRef.trim() }
        : {}),
      ...(form.dnsPolicyRef.trim() ? { dnsPolicyRef: form.dnsPolicyRef.trim() } : {}),
      ...(form.proxyPolicyRef.trim() ? { proxyPolicyRef: form.proxyPolicyRef.trim() } : {}),
    };
    const key = "network-set:" + policyId + ":" + JSON.stringify(body);
    void run(key, { key: "operation.setNetworkPolicy" }, async (signal) => {
      const result = await client.setAdminNetworkPolicy(
        connection.tenantId,
        connection.projectId,
        policyId,
        newRequestId(),
        idempotencyKey(key),
        body,
        signal,
      );
      onChange(replaceNetworkPolicy(policies, result.value));
      setSelectedId(policyId);
      setForm(formFrom(result.value));
      setAudit(
        await listAdminNetworkPolicyAuditEvents(
          client,
          connection.tenantId,
          connection.projectId,
          policyId,
          signal,
        ),
      );
    });
  }

  return (
    <section className="resource-list">
      <div className="list-toolbar">
        <input
          type="search"
          aria-label={t("search.networkPolicies.label")}
          placeholder={t("search.networkPolicies.placeholder")}
          value={query}
          onChange={(event) => onQuery(event.target.value)}
        />
        <span className="scope-chip">network-policies.list · {number(visible.length)}</span>
      </div>
      <div className="panel target-list-panel">
        {visible.length === 0 ? (
          <div className="table-empty">{t("table.empty.networkPolicies")}</div>
        ) : (
          <div className="table-scroll">
            <table>
              <thead>
                <tr>
                  <th>{t("table.name")}</th>
                  <th>{t("networkPolicy.userSummary")}</th>
                  <th>{t("networkPolicy.defaultEgress")}</th>
                  <th>{t("table.version")}</th>
                  <th>{t("table.updated")}</th>
                  <th aria-label={t("table.actions")} />
                </tr>
              </thead>
              <tbody>
                {visible.map((policy) => (
                  <tr
                    key={policy.metadata.uid}
                    className={policy.metadata.uid === selectedId ? "selected" : ""}
                  >
                    <td>
                      <button
                        type="button"
                        disabled={busy}
                        onClick={() => select(policy.metadata.uid)}
                      >
                        <strong>{policy.metadata.name}</strong>
                        <small>{policy.metadata.uid}</small>
                      </button>
                    </td>
                    <td>{policy.spec.userSummary}</td>
                    <td>
                      {t(("networkPolicy.egress." + policy.spec.defaultEgress) as MessageKey)}
                    </td>
                    <td className="mono">rv{policy.metadata.resourceVersion}</td>
                    <td>{dateTime(policy.metadata.updatedAt)}</td>
                    <td className="row-action-cell">
                      <button
                        className="row-action"
                        type="button"
                        disabled={busy}
                        aria-label={t("table.view", { name: policy.metadata.name })}
                        onClick={() => select(policy.metadata.uid)}
                      >
                        ···
                      </button>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
      </div>
      <section className="panel overview-panel">
        <div className="panel-heading">
          <div>
            <h2>{t("networkPolicy.formTitle")}</h2>
            <p>{t("networkPolicy.formDescription")}</p>
          </div>
          <span className="scope-chip">network-policies.get · network-policies.update</span>
        </div>
        <form className="resource-form" onSubmit={save}>
          <div className="form-row">
            <label>
              <span>{t("networkPolicy.id")}</span>
              <input
                required
                maxLength={128}
                spellCheck={false}
                value={form.policyId}
                disabled={busy || selected !== undefined}
                onChange={(event) => setForm({ ...form, policyId: event.target.value })}
              />
            </label>
            <label>
              <span>{t("networkPolicy.name")}</span>
              <input
                required
                maxLength={128}
                spellCheck={false}
                value={form.policyName}
                disabled={busy || referenced}
                onChange={(event) => setForm({ ...form, policyName: event.target.value })}
              />
            </label>
          </div>
          <label>
            <span>{t("networkPolicy.userSummary")}</span>
            <input
              required
              maxLength={256}
              value={form.userSummary}
              disabled={busy || referenced}
              onChange={(event) => setForm({ ...form, userSummary: event.target.value })}
              placeholder={t("networkPolicy.userSummaryPlaceholder")}
            />
            <small>{t("networkPolicy.userSummaryHelp")}</small>
          </label>
          <label>
            <span>{t("networkPolicy.defaultEgress")}</span>
            <select
              value={form.defaultEgress}
              disabled={busy || referenced}
              onChange={(event) =>
                setForm({
                  ...form,
                  defaultEgress: event.target.value as NetworkPolicySetRequest["defaultEgress"],
                })
              }
            >
              {(["public", "restricted", "deny"] as const).map((value) => (
                <option key={value} value={value}>
                  {t(("networkPolicy.egress." + value) as MessageKey)}
                </option>
              ))}
            </select>
          </label>
          {(["allowlistPolicyRef", "dnsPolicyRef", "proxyPolicyRef"] as const).map((field) => (
            <label key={field}>
              <span>{t(("networkPolicy." + field) as MessageKey)}</span>
              <input
                maxLength={128}
                spellCheck={false}
                value={form[field]}
                disabled={busy || referenced}
                onChange={(event) => setForm({ ...form, [field]: event.target.value })}
              />
            </label>
          ))}
          <div className="form-row">
            {(["ingressEnabled", "previewEnabled"] as const).map((field) => (
              <label key={field}>
                <span>{t(("networkPolicy." + field) as MessageKey)}</span>
                <input
                  type="checkbox"
                  checked={form[field]}
                  disabled={busy || referenced}
                  onChange={(event) => setForm({ ...form, [field]: event.target.checked })}
                />
              </label>
            ))}
          </div>
          {referenced && <p className="form-hint">{t("networkPolicy.referencedBoundary")}</p>}
          <button className="button primary" type="submit" disabled={busy || referenced}>
            {t("networkPolicy.save")}
          </button>
        </form>
      </section>
      <section className="panel activity-panel" aria-label={t("networkPolicy.audit")}>
        <div className="activity-heading">
          <h2>{t("networkPolicy.audit")}</h2>
          <span className="scope-chip">audit.list · {number(audit.length)}</span>
        </div>
        {audit.length === 0 ? (
          <p className="activity-empty">{t("networkPolicy.noAudit")}</p>
        ) : (
          <ol className="activity-list compact">
            {audit.map((event) => (
              <li key={event.eventId}>
                <div>
                  <strong>{t("audit.networkPolicySet")}</strong>
                  <span className="phase success">
                    <i />
                    {t("phase.succeeded")}
                  </span>
                </div>
                <small className="mono break">{t("common.actor", { actor: event.actor })}</small>
                <small className="mono">
                  {event.requestId} · {dateTime(event.occurredAt)}
                </small>
              </li>
            ))}
          </ol>
        )}
      </section>
    </section>
  );
}
