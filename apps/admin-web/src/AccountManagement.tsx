import { useEffect, useRef, useState } from "react";
import {
  ClientError,
  type BrowserSession,
  type BrowserSessionClient,
  type BrowserTenant,
  type IdentityAccount,
  type IdentityAuditEvent,
} from "@cloud-agents/cloud-agent-platform-sdk/platform";
import { PasswordForm, passwordLabels } from "../../user-web/src/PasswordForm";
import {
  LinkedLogins,
  type IdentityReturnState,
  type LinkedLoginLabels,
} from "../../user-web/src/LinkedLogins";
import { AdminSheet, SheetHeading, SheetTrigger } from "./AdminSheet";
import { adminErrorKey } from "./admin";
import { useI18n } from "./i18n";

export function AccountManagement(
  props: Readonly<{
    session: BrowserSession;
    sessionClient: BrowserSessionClient;
    tenant?: BrowserTenant;
    onSessionEnded: () => void;
  }>,
) {
  const { t } = useI18n();
  const [returnState, setReturnState] = useState(
    () =>
      /^#identity=(reauthenticated|linked|failed)$/.exec(window.location.hash)?.[1] as
        | IdentityReturnState
        | undefined,
  );
  useEffect(() => {
    if (window.location.hash.startsWith("#identity="))
      window.history.replaceState(null, "", window.location.pathname + window.location.search);
  }, []);
  return (
    <SheetTrigger
      label={t("identity.account")}
      openOnMount={returnState !== undefined}
      onClose={() => setReturnState(undefined)}
    >
      {(trigger, close) => (
        <AccountSheet
          {...props}
          {...(returnState === undefined ? {} : { returnState })}
          trigger={trigger}
          onClose={close}
        />
      )}
    </SheetTrigger>
  );
}

function AccountSheet({
  session,
  sessionClient,
  tenant,
  onSessionEnded,
  trigger,
  onClose,
  returnState,
}: Readonly<{
  session: BrowserSession;
  sessionClient: BrowserSessionClient;
  tenant?: BrowserTenant;
  onSessionEnded: () => void;
  trigger: HTMLElement;
  onClose: () => void;
  returnState?: IdentityReturnState;
}>) {
  const { t, dateTime } = useI18n();
  const platformAdmin = session.user.displayRoles.includes("platform.admin");
  const canAudit = platformAdmin || tenant?.displayRoles.includes("tenant.admin") === true;
  const [accounts, setAccounts] = useState<readonly IdentityAccount[]>([]);
  const [accountsCursor, setAccountsCursor] = useState<string | undefined>();
  const [events, setEvents] = useState<readonly IdentityAuditEvent[]>([]);
  const [eventsCursor, setEventsCursor] = useState<string | undefined>();
  const [auditTenant, setAuditTenant] = useState(platformAdmin ? "" : (tenant?.id ?? ""));
  const [reset, setReset] = useState<{ link: string; email: string; expiresAt: string } | null>(
    null,
  );
  const [confirmDisable, setConfirmDisable] = useState<IdentityAccount | null>(null);
  const [busy, setBusy] = useState(true);
  const [error, setError] = useState("");
  const [linkedOpen, setLinkedOpen] = useState(returnState !== undefined);
  const lifetime = useRef<AbortController | null>(null);
  const pending = useRef(false);
  const labels = Object.fromEntries(
    Object.keys(passwordLabels).map((key) => [
      key,
      t(`identity.password.${key as keyof typeof passwordLabels}`),
    ]),
  ) as Record<keyof typeof passwordLabels, string>;
  const linkedLabels: LinkedLoginLabels = {
    title: t("identity.linkedTitle"),
    help: t("identity.linkedHelp"),
    loading: t("identity.linkedLoading"),
    password: t("identity.linkedPassword"),
    passwordEnabled: t("identity.linkedPasswordEnabled"),
    noPassword: t("identity.linkedNoPassword"),
    currentPassword: t("identity.linkedCurrentPassword"),
    reauthenticate: t("identity.linkedReauthenticate"),
    reauthenticateWith: (provider) => t("identity.linkedReauthenticateWith", { provider }),
    reauthenticated: t("identity.linkedReauthenticated"),
    link: (provider) => t("identity.linkedLink", { provider }),
    unlink: (provider) => t("identity.linkedUnlink", { provider }),
    issuer: (issuer) => t("identity.linkedIssuer", { issuer }),
    subject: (subject) => t("identity.linkedSubject", { subject }),
    newPassword: t("identity.linkedNewPassword"),
    confirmPassword: t("identity.linkedConfirmPassword"),
    enablePassword: t("identity.linkedEnablePassword"),
    passwordHelp: t("identity.linkedPasswordHelp"),
    mismatch: t("identity.linkedMismatch"),
    failed: t("identity.linkedFailed"),
    linked: t("identity.linkedLinked"),
    unlinked: t("identity.linkedUnlinked"),
  };

  function failure(cause: unknown) {
    return t(
      cause instanceof ClientError && cause.status === 409
        ? "identity.accountConflict"
        : cause instanceof ClientError && cause.status === 403
          ? "identity.accountDenied"
          : adminErrorKey(cause),
    );
  }

  useEffect(() => {
    const controller = new AbortController();
    lifetime.current = controller;
    const signal = AbortSignal.any([controller.signal, AbortSignal.timeout(15_000)]);
    setBusy(true);
    setError("");
    void Promise.all([
      platformAdmin ? sessionClient.listIdentityAccounts(50, undefined, signal) : undefined,
      canAudit
        ? sessionClient.listIdentityAuditEvents(auditTenant || undefined, 50, undefined, signal)
        : undefined,
    ])
      .then(([accountPage, auditPage]) => {
        if (signal.aborted) return;
        setAccounts(accountPage?.accounts ?? []);
        setAccountsCursor(accountPage?.nextPageToken);
        setEvents(auditPage?.events ?? []);
        setEventsCursor(auditPage?.nextPageToken);
      })
      .catch((cause: unknown) => {
        if (!controller.signal.aborted) setError(failure(cause));
      })
      .finally(() => {
        if (!controller.signal.aborted) setBusy(false);
      });
    return () => controller.abort();
  }, [sessionClient, platformAdmin, canAudit, auditTenant]);

  async function run(action: (signal: AbortSignal) => Promise<void>) {
    const controller = lifetime.current;
    if (controller === null || busy || pending.current) return;
    pending.current = true;
    setBusy(true);
    setError("");
    try {
      await action(AbortSignal.any([controller.signal, AbortSignal.timeout(15_000)]));
    } catch (cause) {
      if (!controller.signal.aborted) setError(failure(cause));
    } finally {
      pending.current = false;
      if (!controller.signal.aborted) setBusy(false);
    }
  }

  async function refreshAudit(signal: AbortSignal) {
    if (!canAudit) return;
    const page = await sessionClient.listIdentityAuditEvents(
      auditTenant || undefined,
      50,
      undefined,
      signal,
    );
    if (!signal.aborted) {
      setEvents(page.events);
      setEventsCursor(page.nextPageToken);
    }
  }

  return (
    <AdminSheet
      label={t("identity.account")}
      returnFocus={trigger}
      onClose={() => {
        if (!busy) onClose();
      }}
      feedback={null}
    >
      <section className="dialog" aria-labelledby="account-title">
        <SheetHeading
          id="account-title"
          title={t("identity.account")}
          subject={session.user.email}
          disabled={busy}
          onClose={onClose}
        />
        <div className="sheet-body">
          <details>
            <summary>{t("identity.changePassword")}</summary>
            <PasswordForm client={sessionClient} labels={labels} onComplete={onSessionEnded} />
          </details>
          <details open={linkedOpen} onToggle={(event) => setLinkedOpen(event.currentTarget.open)}>
            <summary>{t("identity.linkedTitle")}</summary>
            {linkedOpen ? (
              <LinkedLogins
                client={sessionClient}
                {...(returnState === undefined ? {} : { returnState })}
                onSessionEnded={onSessionEnded}
                labels={linkedLabels}
              />
            ) : null}
          </details>
          {error ? (
            <div className="error-banner" role="alert">
              {error}
            </div>
          ) : null}
          {platformAdmin ? (
            <section className="resource-form" aria-busy={busy}>
              <h3>{t("identity.platformAccounts")}</h3>
              <p>{t("identity.platformAccountsHelp")}</p>
              <ul className="identity-invitation-list">
                {accounts.map((account) => (
                  <li key={account.id} data-account-id={account.id}>
                    <div>
                      <strong>{account.displayName || account.email}</strong>
                      <small>{account.email}</small>
                      <small>
                        {t(
                          account.state === "active"
                            ? "identity.accountActive"
                            : "identity.accountDisabled",
                        )}
                        {account.platformAdmin ? ` · ${t("identity.platformAdmin")}` : ""}
                      </small>
                    </div>
                    {account.state === "active" ? (
                      <div>
                        <button
                          type="button"
                          className="button outline compact"
                          aria-label={`${t("identity.resetPassword")} · ${account.email}`}
                          disabled={busy}
                          onClick={() => {
                            void run(async (signal) => {
                              const result = await sessionClient.issuePasswordReset(
                                account.id,
                                signal,
                              );
                              if (!signal.aborted)
                                setReset({
                                  link: `${window.location.origin}/user-console#password-reset=${result.resetCode}`,
                                  email: account.email,
                                  expiresAt: result.expiresAt,
                                });
                              await refreshAudit(signal);
                            });
                          }}
                        >
                          {t("identity.resetPassword")}
                        </button>
                        <button
                          type="button"
                          className="button outline compact"
                          aria-label={`${t("identity.disableAccount")} · ${account.email}`}
                          disabled={busy}
                          onClick={() => setConfirmDisable(account)}
                        >
                          {t("identity.disableAccount")}
                        </button>
                      </div>
                    ) : null}
                  </li>
                ))}
              </ul>
              {accountsCursor ? (
                <button
                  type="button"
                  className="button outline"
                  disabled={busy}
                  onClick={() => {
                    void run(async (signal) => {
                      const page = await sessionClient.listIdentityAccounts(
                        50,
                        accountsCursor,
                        signal,
                      );
                      if (!signal.aborted) {
                        setAccounts((previous) => appendNew(previous, page.accounts));
                        setAccountsCursor(page.nextPageToken);
                      }
                    });
                  }}
                >
                  {t("identity.loadMore")}
                </button>
              ) : null}
              {confirmDisable ? (
                <div role="alert" className="resource-form">
                  <p>{t("identity.disableConfirm", { email: confirmDisable.email })}</p>
                  <button
                    type="button"
                    className="button primary"
                    disabled={busy}
                    onClick={() => {
                      void run(async (signal) => {
                        await sessionClient.disableIdentityAccount(confirmDisable.id, signal);
                        if (signal.aborted) return;
                        if (confirmDisable.id === session.user.id) {
                          onSessionEnded();
                          return;
                        }
                        setAccounts((previous) =>
                          previous.map((account) =>
                            account.id === confirmDisable.id
                              ? { ...account, state: "disabled" }
                              : account,
                          ),
                        );
                        setConfirmDisable(null);
                        await refreshAudit(signal);
                      });
                    }}
                  >
                    {t("identity.disableAccount")}
                  </button>
                  <button
                    type="button"
                    className="button outline"
                    disabled={busy}
                    onClick={() => setConfirmDisable(null)}
                  >
                    {t("identity.cancel")}
                  </button>
                </div>
              ) : null}
              {reset ? (
                <div className="resource-form" aria-live="polite">
                  <strong>{t("identity.resetReady", { email: reset.email })}</strong>
                  <p>{t("identity.resetHelp")}</p>
                  <label>
                    <span>{t("identity.resetLink")}</span>
                    <textarea readOnly spellCheck={false} rows={3} value={reset.link} />
                  </label>
                  <small>{t("identity.expires", { value: dateTime(reset.expiresAt) })}</small>
                </div>
              ) : null}
            </section>
          ) : null}
          {canAudit ? (
            <section className="resource-form" aria-busy={busy}>
              <header className="panel-heading">
                <h3>{t("identity.audit")}</h3>
                <button
                  type="button"
                  className="button outline compact"
                  disabled={busy}
                  onClick={() => {
                    void run(refreshAudit);
                  }}
                >
                  {t("identity.refresh")}
                </button>
              </header>
              {platformAdmin && tenant ? (
                <label>
                  <span>{t("identity.auditScope")}</span>
                  <select
                    value={auditTenant}
                    disabled={busy}
                    onChange={(event) => setAuditTenant(event.target.value)}
                  >
                    <option value="">{t("identity.allAccounts")}</option>
                    <option value={tenant.id}>{tenant.name}</option>
                  </select>
                </label>
              ) : null}
              <ul className="identity-invitation-list">
                {events.map((event) => (
                  <li key={event.id}>
                    <div>
                      <strong>{event.eventKind.replaceAll("_", " ")}</strong>
                      <small>
                        {dateTime(event.occurredAt)} · {event.decision}
                      </small>
                      <small>
                        {event.reasonCode}
                        {event.targetUserId
                          ? ` · ${accounts.find(({ id }) => id === event.targetUserId)?.email ?? event.targetUserId}`
                          : ""}
                      </small>
                      <small>
                        {t("identity.auditActor")}:{" "}
                        {event.actorUserId
                          ? (accounts.find(({ id }) => id === event.actorUserId)?.email ??
                            event.actorUserId)
                          : t("identity.auditActorUnavailable")}
                      </small>
                      <small>{event.correlationId}</small>
                    </div>
                  </li>
                ))}
              </ul>
              {events.length === 0 ? <p>{t("identity.noAudit")}</p> : null}
              {eventsCursor ? (
                <button
                  type="button"
                  className="button outline"
                  disabled={busy}
                  onClick={() => {
                    void run(async (signal) => {
                      const page = await sessionClient.listIdentityAuditEvents(
                        auditTenant || undefined,
                        50,
                        eventsCursor,
                        signal,
                      );
                      if (!signal.aborted) {
                        setEvents((previous) => appendNew(previous, page.events));
                        setEventsCursor(page.nextPageToken);
                      }
                    });
                  }}
                >
                  {t("identity.loadMore")}
                </button>
              ) : null}
            </section>
          ) : null}
        </div>
      </section>
    </AdminSheet>
  );
}

// Appends a later page while dropping entries already shown, since pages can shift between requests.
function appendNew<T extends Readonly<{ id: string }>>(previous: readonly T[], next: readonly T[]): T[] {
  const seen = new Set(previous.map(({ id }) => id));
  return [...previous, ...next.filter(({ id }) => !seen.has(id))];
}
