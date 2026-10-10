import { useEffect, useRef, useState, type FormEvent } from "react";
import {
  ClientError,
  type BrowserSessionClient,
  type BrowserTenant,
  type Client,
  type Invitation,
  type Organization,
  type Project,
  type Role,
} from "@cloud-agents/cloud-agent-platform-sdk/platform";
import { AdminSheet, SheetHeading, SheetTrigger } from "./AdminSheet";
import { adminErrorKey } from "./admin";
import { MembershipManagement } from "./MembershipManagement";
import { MembershipAudit } from "./MembershipAudit";
import { useI18n } from "./i18n";
import { listAllAdminOrganizations } from "./app/connection";

export function InvitationManagement(
  props: Readonly<{
    client: Client;
    sessionClient: BrowserSessionClient;
    tenant: BrowserTenant;
    projects: readonly Project[];
  }>,
) {
  const { t } = useI18n();
  return (
    <SheetTrigger label={t("identity.invitations")}>
      {(trigger, close) => (
        <InvitationSheet key={props.tenant.id} {...props} trigger={trigger} onClose={close} />
      )}
    </SheetTrigger>
  );
}

function InvitationSheet({
  client,
  sessionClient,
  tenant,
  projects,
  trigger,
  onClose,
}: Readonly<{
  client: Client;
  sessionClient: BrowserSessionClient;
  tenant: BrowserTenant;
  projects: readonly Project[];
  trigger: HTMLElement;
  onClose: () => void;
}>) {
  const { t, dateTime } = useI18n();
  const [roles, setRoles] = useState<readonly Role[]>([]);
  const [organizations, setOrganizations] = useState<readonly Organization[]>([]);
  const [invitations, setInvitations] = useState<readonly Invitation[]>([]);
  const [nextPageToken, setNextPageToken] = useState<string | undefined>();
  const [email, setEmail] = useState("");
  const [roleName, setRoleName] = useState("project.developer");
  const [scopeId, setScopeId] = useState("");
  const [attested, setAttested] = useState(false);
  const [link, setLink] = useState("");
  const [copied, setCopied] = useState(false);
  const [busy, setBusy] = useState(true);
  const [error, setError] = useState("");
  const [view, setView] = useState<"invitations" | "members">("invitations");
  const lifetime = useRef<AbortController | null>(null);
  const scopeLevel = roleName.startsWith("project.")
    ? "project"
    : roleName === "organization.admin"
      ? "organization"
      : "tenant";
  const choices =
    scopeLevel === "tenant"
      ? [{ id: tenant.id, name: tenant.name }]
      : scopeLevel === "organization"
        ? organizations.map(({ metadata, spec }) => ({ id: metadata.uid, name: spec.displayName }))
        : projects.map(({ metadata, spec }) => ({ id: metadata.uid, name: spec.displayName }));
  const selectedScope = choices.some(({ id }) => id === scopeId) ? scopeId : (choices[0]?.id ?? "");

  function message(cause: unknown) {
    return t(
      cause instanceof ClientError && cause.status === 409
        ? "identity.invitationConflict"
        : cause instanceof ClientError && cause.status === 403
          ? "identity.invitationDenied"
          : adminErrorKey(cause),
    );
  }

  useEffect(() => {
    const controller = new AbortController();
    lifetime.current = controller;
    const signal = AbortSignal.any([controller.signal, AbortSignal.timeout(15_000)]);
    void Promise.all([
      client.listAdminRoles(tenant.id, `roles-${crypto.randomUUID()}`, 200, undefined, signal),
      listAllAdminOrganizations(client, tenant.id, signal),
      sessionClient.listInvitations(tenant.id, 50, undefined, signal),
    ])
      .then(([catalog, organizations, page]) => {
        if (signal.aborted) return;
        setRoles(
          catalog.value.roles.filter(
            ({ spec }) => spec.state === "active" && spec.name !== "platform.admin",
          ),
        );
        setOrganizations(organizations.filter(({ spec }) => spec.state === "active"));
        setInvitations(page.invitations);
        setNextPageToken(page.nextPageToken);
      })
      .catch((cause: unknown) => {
        if (!controller.signal.aborted) setError(message(cause));
      })
      .finally(() => {
        if (!controller.signal.aborted) setBusy(false);
      });
    return () => controller.abort();
  }, [client, sessionClient, tenant.id]);

  async function run(action: (signal: AbortSignal) => Promise<void>) {
    const controller = lifetime.current;
    if (controller === null || busy) return;
    setBusy(true);
    setError("");
    const signal = AbortSignal.any([controller.signal, AbortSignal.timeout(15_000)]);
    try {
      await action(signal);
    } catch (cause) {
      if (!controller.signal.aborted) setError(message(cause));
    } finally {
      if (!controller.signal.aborted) setBusy(false);
    }
  }

  async function reload(signal: AbortSignal) {
    const page = await sessionClient.listInvitations(tenant.id, 50, undefined, signal);
    if (!signal.aborted) {
      setInvitations(page.invitations);
      setNextPageToken(page.nextPageToken);
    }
  }

  function create(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!attested || selectedScope === "" || !roles.some(({ spec }) => spec.name === roleName))
      return;
    void run(async (signal) => {
      const created = await sessionClient.createInvitation(
        tenant.id,
        { email, roleName, scopeLevel, scopeId: selectedScope, verification: "admin-attested" },
        signal,
      );
      if (signal.aborted) return;
      setLink(`${window.location.origin}/user-console#invitation=${created.invitationCode}`);
      setCopied(false);
      setEmail("");
      setAttested(false);
      await reload(signal);
    });
  }

  return (
    <AdminSheet
      label={t("identity.invitations")}
      returnFocus={trigger}
      onClose={() => {
        if (!busy) onClose();
      }}
      feedback={null}
    >
      <section className="dialog" aria-labelledby="invitations-title">
        <SheetHeading
          id="invitations-title"
          title={t("identity.invitations")}
          subject={tenant.name}
          disabled={busy}
          onClose={onClose}
        />
        <div className="sheet-body">
          <div className="form-row" role="tablist" aria-label={t("identity.memberViews")}>
            <button
              type="button"
              role="tab"
              aria-selected={view === "invitations"}
              className={
                view === "invitations" ? "button primary compact" : "button outline compact"
              }
              onClick={() => setView("invitations")}
            >
              {t("identity.invitationsTab")}
            </button>
            <button
              type="button"
              role="tab"
              aria-selected={view === "members"}
              className={view === "members" ? "button primary compact" : "button outline compact"}
              onClick={() => setView("members")}
            >
              {t("identity.membersTab")}
            </button>
          </div>
          {view === "members" ? (
            <>
              <MembershipManagement
                client={client}
                sessionClient={sessionClient}
                tenant={tenant}
                projects={projects}
              />
              <MembershipAudit client={sessionClient} tenantId={tenant.id} />
            </>
          ) : (
            <>
              <form className="resource-form" onSubmit={create}>
                <label>
                  <span>{t("identity.email")}</span>
                  <input
                    data-sheet-autofocus
                    type="email"
                    autoComplete="off"
                    maxLength={254}
                    required
                    value={email}
                    disabled={busy}
                    onChange={(event) => setEmail(event.target.value)}
                  />
                </label>
                <label>
                  <span>{t("identity.role")}</span>
                  <select
                    value={roleName}
                    disabled={busy}
                    onChange={(event) => {
                      setRoleName(event.target.value);
                      setScopeId("");
                    }}
                  >
                    {roles.map(({ spec }) => (
                      <option key={spec.name} value={spec.name}>
                        {spec.name}
                      </option>
                    ))}
                  </select>
                </label>
                <label>
                  <span>{t("identity.scope")}</span>
                  <select
                    value={selectedScope}
                    required
                    disabled={busy || choices.length === 0}
                    onChange={(event) => setScopeId(event.target.value)}
                  >
                    {choices.length === 0 ? (
                      <option value="">{t("identity.noScope")}</option>
                    ) : (
                      choices.map(({ id, name }) => (
                        <option key={id} value={id}>
                          {name}
                        </option>
                      ))
                    )}
                  </select>
                </label>
                <label className="confirmation-check">
                  <input
                    type="checkbox"
                    checked={attested}
                    required
                    disabled={busy}
                    onChange={(event) => setAttested(event.target.checked)}
                  />
                  <span>{t("identity.attestation")}</span>
                </label>
                <small>{t("identity.attestationHelp")}</small>
                <button
                  type="submit"
                  className="button primary"
                  disabled={busy || !attested || selectedScope === "" || roles.length === 0}
                >
                  {t("identity.createInvitation")}
                </button>
              </form>
              {link ? (
                <section className="resource-form" aria-live="polite">
                  <strong>{t("identity.linkReady")}</strong>
                  <p>{t("identity.linkHelp")}</p>
                  <label>
                    <span>{t("identity.invitationLink")}</span>
                    <textarea readOnly value={link} rows={3} spellCheck={false} />
                  </label>
                  <button
                    className="button outline"
                    type="button"
                    onClick={() => {
                      void navigator.clipboard
                        .writeText(link)
                        .then(() => setCopied(true))
                        .catch(() => setCopied(false));
                    }}
                  >
                    {t(copied ? "identity.copied" : "identity.copyLink")}
                  </button>
                </section>
              ) : null}
              {error ? (
                <div role="alert" className="error-banner">
                  {error}
                </div>
              ) : null}
              <section className="resource-form" aria-busy={busy}>
                <h3>{t("identity.recentInvitations")}</h3>
                {invitations.length === 0 ? (
                  <p>{t("identity.noInvitations")}</p>
                ) : (
                  <ul className="identity-invitation-list">
                    {invitations.map((invitation) => (
                      <li key={invitation.id}>
                        <div>
                          <strong>{invitation.email}</strong>
                          <small>
                            {invitation.roleName} ·{" "}
                            {t(`identity.invitationState.${invitation.state}`)}
                          </small>
                          <small>
                            {t("identity.expires", { value: dateTime(invitation.expiresAt) })}
                          </small>
                        </div>
                        {invitation.state === "pending" ? (
                          <button
                            type="button"
                            className="button outline compact"
                            disabled={busy}
                            onClick={() => {
                              void run(async (signal) => {
                                await sessionClient.revokeInvitation(
                                  tenant.id,
                                  invitation.id,
                                  signal,
                                );
                                await reload(signal);
                              });
                            }}
                          >
                            {t("identity.revoke")}
                          </button>
                        ) : null}
                      </li>
                    ))}
                  </ul>
                )}
                {nextPageToken ? (
                  <button
                    type="button"
                    className="button outline"
                    disabled={busy}
                    onClick={() => {
                      void run(async (signal) => {
                        const page = await sessionClient.listInvitations(
                          tenant.id,
                          50,
                          nextPageToken,
                          signal,
                        );
                        if (!signal.aborted) {
                          setInvitations((current) => [
                            ...current,
                            ...page.invitations.filter(
                              (entry) => !current.some(({ id }) => id === entry.id),
                            ),
                          ]);
                          setNextPageToken(page.nextPageToken);
                        }
                      });
                    }}
                  >
                    {t("identity.loadMore")}
                  </button>
                ) : null}
              </section>
            </>
          )}
        </div>
      </section>
    </AdminSheet>
  );
}
