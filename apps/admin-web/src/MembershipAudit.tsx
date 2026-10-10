import { useEffect, useState } from "react";
import {
  type BrowserSessionClient,
  type ControlPlaneAuditPage,
} from "@cloud-agents/cloud-agent-platform-sdk/platform";

import { useI18n } from "./i18n";
import { adminErrorKey } from "./admin";

type Props = Readonly<{ client: BrowserSessionClient; tenantId: string }>;

export function MembershipAudit(props: Props) {
  const { t } = useI18n();
  const [open, setOpen] = useState(false);
  return (
    <details className="resource-form" onToggle={(event) => setOpen(event.currentTarget.open)}>
      <summary>{t("identity.memberAudit")}</summary>
      {open ? <AuditPage key={props.tenantId} {...props} /> : null}
    </details>
  );
}

function AuditPage({ client, tenantId }: Props) {
  const { t, locale } = useI18n();
  const [page, setPage] = useState<ControlPlaneAuditPage | null>(null);
  const [cursor, setCursor] = useState<string>();
  const [revision, setRevision] = useState(0);
  const [busy, setBusy] = useState(true);
  const [error, setError] = useState("");

  useEffect(() => {
    const controller = new AbortController();
    setBusy(true);
    setError("");
    void client
      .listControlPlaneAuditEvents(
        tenantId,
        50,
        cursor,
        AbortSignal.any([controller.signal, AbortSignal.timeout(15_000)]),
      )
      .then((result) => {
        if (controller.signal.aborted) return;
        if (result.nextPageToken !== undefined && result.nextPageToken === cursor)
          throw new Error("Audit pagination repeated a cursor");
        setPage(result);
      })
      .catch((cause) => {
        if (!controller.signal.aborted) setError(t(adminErrorKey(cause)));
      })
      .finally(() => {
        if (!controller.signal.aborted) setBusy(false);
      });
    return () => controller.abort();
  }, [client, tenantId, cursor, revision, t]);

  return (
    <section aria-label={t("identity.memberAudit")} aria-busy={busy}>
      <button
        type="button"
        className="button outline compact"
        disabled={busy}
        onClick={() => {
          setCursor(undefined);
          setRevision((value) => value + 1);
        }}
      >
        {t("identity.refresh")}
      </button>
      {error ? <p role="alert">{error}</p> : null}
      <ul className="identity-invitation-list">
        {page?.events.map((event) => (
          <li key={event.id}>
            <div>
              <strong>{event.action.replaceAll("_", " ").replaceAll(".", " ")}</strong>
              <small>
                {new Date(event.occurredAt).toLocaleString(locale)} · {event.decision}
              </small>
              <small>
                {event.resourceId} · {event.reasonCode}
              </small>
              <small>
                {t("identity.auditActor")}:{" "}
                {event.actor
                  ? `${event.actor.kind} · ${event.actor.issuer} · ${event.actor.subject}`
                  : t("identity.auditActorUnavailable")}
              </small>
              {event.correlationId ? (
                <small>
                  {event.application} · {event.correlationId}
                </small>
              ) : null}
            </div>
          </li>
        ))}
      </ul>
      {page?.events.length === 0 ? <p>{t("identity.noAudit")}</p> : null}
      {page?.nextPageToken ? (
        <button
          type="button"
          className="button outline compact"
          disabled={busy || error !== ""}
          onClick={() => setCursor(page.nextPageToken)}
        >
          {t("identity.memberAuditNext")}
        </button>
      ) : null}
    </section>
  );
}
