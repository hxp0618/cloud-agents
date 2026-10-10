import { useEffect, useMemo, useRef, useState, type FormEvent } from "react";
import {
  ClientError,
  type BrowserTenant,
  type Client,
  type IdentityApplication,
  type Organization,
  type Project,
  type Role,
  type RoleName,
  type ServiceAccount,
} from "@cloud-agents/cloud-agent-platform-sdk/platform";

import { AdminSheet, SheetHeading, SheetTrigger } from "./AdminSheet";
import { adminErrorKey } from "./admin";
import { listAllAdminRoles } from "./MembershipManagement";
import { listAllAdminOrganizations } from "./app/connection";
import { useI18n } from "./i18n";

const pageSize = 200;

type GrantableRole = Role &
  Readonly<{ spec: Role["spec"] & { name: Exclude<RoleName, "platform.admin"> } }>;

type ScopeChoice = Readonly<{
  key: string;
  level: ServiceAccount["scopeLevel"];
  id: string;
  label: string;
}>;

type LoadedData = Readonly<{
  accounts: readonly ServiceAccount[];
  roles: readonly GrantableRole[];
  organizations: readonly Organization[];
}>;

type OneTimeCredential = Readonly<{
  accountId: string;
  accountName: string;
  value: string;
  expiresAt: string;
}>;

function requestId(): string {
  return `service-account-${crypto.randomUUID()}`;
}

function nextAccountId(): string {
  return `automation-${crypto.randomUUID()}`;
}

function isGrantableRole(role: Role): role is GrantableRole {
  return role.spec.state === "active" && role.spec.name !== "platform.admin";
}

function roleScopeLevel(role: GrantableRole): ServiceAccount["scopeLevel"] {
  if (role.spec.name === "tenant.admin") return "tenant";
  if (role.spec.name === "organization.admin") return "organization";
  return "project";
}

function scopeChoices(
  role: GrantableRole,
  tenant: BrowserTenant,
  organizations: readonly Organization[],
  projects: readonly Project[],
): readonly ScopeChoice[] {
  const level = roleScopeLevel(role);
  if (level === "tenant")
    return Object.freeze([
      { key: `tenant:${tenant.id}`, level, id: tenant.id, label: tenant.name },
    ]);
  if (level === "organization")
    return Object.freeze(
      organizations
        .filter(({ spec }) => spec.state === "active")
        .map(({ metadata, spec }) => ({
          key: `organization:${metadata.uid}`,
          level,
          id: metadata.uid,
          label: spec.displayName,
        }))
        .toSorted((left, right) => left.label.localeCompare(right.label)),
    );
  return Object.freeze(
    projects
      .filter(({ spec }) => spec.state === "active")
      .map(({ metadata, spec }) => ({
        key: `project:${metadata.uid}`,
        level,
        id: metadata.uid,
        label: spec.displayName,
      }))
      .toSorted((left, right) => left.label.localeCompare(right.label)),
  );
}

function scopeLabel(
  account: ServiceAccount,
  tenant: BrowserTenant,
  organizations: readonly Organization[],
  projects: readonly Project[],
): string {
  if (account.scopeLevel === "tenant") return tenant.name;
  if (account.scopeLevel === "organization")
    return (
      organizations.find(({ metadata }) => metadata.uid === account.scopeId)?.spec.displayName ??
      account.scopeId
    );
  return (
    projects.find(({ metadata }) => metadata.uid === account.scopeId)?.spec.displayName ??
    account.scopeId
  );
}

async function listAllServiceAccounts(
  client: Pick<Client, "listAdminServiceAccounts">,
  tenantId: string,
  signal: AbortSignal,
): Promise<readonly ServiceAccount[]> {
  const accounts: ServiceAccount[] = [];
  const seen = new Set<string>();
  let pageToken: string | undefined;
  do {
    if (pageToken !== undefined) {
      if (seen.has(pageToken)) throw new Error("Service-account pagination repeated a cursor.");
      seen.add(pageToken);
    }
    const page = await client.listAdminServiceAccounts(
      tenantId,
      requestId(),
      pageSize,
      pageToken,
      signal,
    );
    accounts.push(...page.serviceAccounts);
    pageToken = page.nextPageToken;
  } while (pageToken !== undefined && pageToken !== "");
  return Object.freeze(
    accounts.toSorted(
      (left, right) =>
        left.displayName.localeCompare(right.displayName) || left.id.localeCompare(right.id),
    ),
  );
}

async function loadData(
  client: Client,
  tenantId: string,
  signal: AbortSignal,
): Promise<LoadedData> {
  const [accounts, roles, organizations] = await Promise.all([
    listAllServiceAccounts(client, tenantId, signal),
    listAllAdminRoles(client, tenantId, signal),
    listAllAdminOrganizations(client, tenantId, signal),
  ]);
  return Object.freeze({
    accounts,
    roles: Object.freeze(roles.filter(isGrantableRole)),
    organizations: Object.freeze(organizations),
  });
}

export function ServiceAccountManagement({
  client,
  tenant,
  projects,
}: Readonly<{
  client: Client;
  tenant: BrowserTenant;
  projects: readonly Project[];
}>) {
  const { t } = useI18n();
  return (
    <SheetTrigger label={t("identity.serviceAccounts")}>
      {(trigger, close) => (
        <ServiceAccountSheet
          client={client}
          tenant={tenant}
          projects={projects}
          trigger={trigger}
          onClose={close}
        />
      )}
    </SheetTrigger>
  );
}

function ServiceAccountSheet({
  client,
  tenant,
  projects,
  trigger,
  onClose,
}: Readonly<{
  client: Client;
  tenant: BrowserTenant;
  projects: readonly Project[];
  trigger: HTMLElement;
  onClose: () => void;
}>) {
  const { t, dateTime } = useI18n();
  const [data, setData] = useState<LoadedData | null>(null);
  const [accountId, setAccountId] = useState(nextAccountId);
  const [displayName, setDisplayName] = useState("");
  const [application, setApplication] = useState<IdentityApplication>("admin");
  const [roleName, setRoleName] = useState("");
  const [scopeKey, setScopeKey] = useState("");
  const [credential, setCredential] = useState<OneTimeCredential | null>(null);
  const [disableId, setDisableId] = useState("");
  const [busy, setBusy] = useState(true);
  const [error, setError] = useState("");
  const [notice, setNotice] = useState("");
  const request = useRef<AbortController | null>(null);
  const pending = useRef(false);

  async function load(signal: AbortSignal) {
    const loaded = await loadData(client, tenant.id, signal);
    if (!signal.aborted) setData(loaded);
  }

  useEffect(() => {
    const controller = new AbortController();
    request.current = controller;
    const signal = AbortSignal.any([controller.signal, AbortSignal.timeout(15_000)]);
    setBusy(true);
    void load(signal)
      .catch((cause) => {
        if (!controller.signal.aborted) setError(t(adminErrorKey(cause)));
      })
      .finally(() => {
        if (!controller.signal.aborted) setBusy(false);
      });
    return () => request.current?.abort();
  }, [client, tenant.id, t]);

  const selectedRole = useMemo(
    () => data?.roles.find(({ spec }) => spec.name === roleName) ?? data?.roles[0],
    [data?.roles, roleName],
  );
  const choices = useMemo(
    () =>
      selectedRole === undefined
        ? []
        : scopeChoices(selectedRole, tenant, data?.organizations ?? [], projects),
    [data?.organizations, projects, selectedRole, tenant],
  );
  const selectedScope = choices.find(({ key }) => key === scopeKey) ?? choices[0];

  async function mutate(action: (signal: AbortSignal) => Promise<void>, success: string) {
    if (busy || pending.current) return;
    pending.current = true;
    request.current?.abort();
    const controller = new AbortController();
    request.current = controller;
    const signal = AbortSignal.any([controller.signal, AbortSignal.timeout(15_000)]);
    setBusy(true);
    setError("");
    setNotice("");
    let failure: unknown;
    try {
      await action(signal);
    } catch (cause) {
      failure = cause;
    }
    try {
      await load(signal);
      if (failure === undefined) setNotice(success);
      else if (failure instanceof ClientError && failure.status === 403)
        setError(t("identity.serviceAccountDenied"));
      else if (failure instanceof ClientError && failure.status === 409)
        setError(t("identity.serviceAccountConflict"));
      else setError(t(adminErrorKey(failure)));
    } catch {
      if (!controller.signal.aborted) setError(t("identity.serviceAccountRefreshFailed"));
    } finally {
      pending.current = false;
      if (!controller.signal.aborted) setBusy(false);
    }
  }

  function create(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (selectedRole === undefined || selectedScope === undefined || credential !== null) return;
    void mutate(async (signal) => {
      const created = await client.createAdminServiceAccount(
        tenant.id,
        requestId(),
        {
          serviceAccountId: accountId,
          displayName,
          application,
          roleName: selectedRole.spec.name,
          scopeLevel: selectedScope.level,
          scopeId: selectedScope.id,
        },
        signal,
      );
      if (signal.aborted) return;
      setCredential({
        accountId: created.serviceAccount.id,
        accountName: created.serviceAccount.displayName,
        value: created.credential,
        expiresAt: created.credentialExpiresAt,
      });
      setAccountId(nextAccountId());
      setDisplayName("");
    }, t("identity.serviceAccountCreated"));
  }

  function rotate(account: ServiceAccount) {
    if (credential !== null) return;
    void mutate(
      async (signal) => {
        const rotated = await client.rotateAdminServiceAccountCredential(
          tenant.id,
          account.id,
          requestId(),
          { expectedResourceVersion: account.resourceVersion },
          signal,
        );
        if (!signal.aborted)
          setCredential({
            accountId: account.id,
            accountName: account.displayName,
            value: rotated.credential,
            expiresAt: rotated.credentialExpiresAt,
          });
      },
      t("identity.serviceAccountRotated", { account: account.displayName }),
    );
  }

  function disable(account: ServiceAccount) {
    void mutate(
      async (signal) => {
        await client.disableAdminServiceAccount(
          tenant.id,
          account.id,
          requestId(),
          { expectedResourceVersion: account.resourceVersion },
          signal,
        );
        if (!signal.aborted) setDisableId("");
      },
      t("identity.serviceAccountDisabled", { account: account.displayName }),
    );
  }

  function downloadCredential() {
    if (credential === null) return;
    const url = URL.createObjectURL(
      new Blob([credential.value], { type: "application/octet-stream" }),
    );
    const anchor = document.createElement("a");
    anchor.href = url;
    anchor.download = `cloud-agents-service-account-${credential.accountId}.credential`;
    anchor.click();
    queueMicrotask(() => URL.revokeObjectURL(url));
  }

  return (
    <AdminSheet
      label={t("identity.serviceAccounts")}
      onClose={() => {
        if (credential === null) onClose();
      }}
      returnFocus={trigger}
      feedback={null}
    >
      <SheetHeading
        id="service-accounts-title"
        title={t("identity.serviceAccounts")}
        disabled={credential !== null}
        onClose={onClose}
      />
      <div className="sheet-body">
        {error ? (
          <div className="error-banner" role="alert">
            {error}
          </div>
        ) : null}
        {notice ? <p role="status">{notice}</p> : null}
        {credential === null ? null : (
          <section className="resource-form" aria-live="polite">
            <h3>
              {t("identity.serviceAccountCredentialReady", { account: credential.accountName })}
            </h3>
            <p>{t("identity.serviceAccountCredentialHelp")}</p>
            <label>
              <span>{t("identity.serviceAccountCredential")}</span>
              <textarea
                aria-label={t("identity.serviceAccountCredential")}
                readOnly
                rows={4}
                spellCheck={false}
                value={credential.value}
              />
            </label>
            <small>
              {t("identity.serviceAccountCredentialExpires", {
                value: dateTime(credential.expiresAt),
              })}
            </small>
            <div className="form-actions">
              <button
                type="button"
                className="button outline"
                onClick={() => void navigator.clipboard.writeText(credential.value)}
              >
                {t("identity.serviceAccountCopyCredential")}
              </button>
              <button type="button" className="button outline" onClick={downloadCredential}>
                {t("identity.serviceAccountDownloadCredential")}
              </button>
              <button type="button" className="button primary" onClick={() => setCredential(null)}>
                {t("identity.serviceAccountCredentialSaved")}
              </button>
            </div>
          </section>
        )}
        <form className="resource-form" onSubmit={create}>
          <h3>{t("identity.serviceAccountCreate")}</h3>
          <label>
            <span>{t("identity.serviceAccountDisplayName")}</span>
            <input
              data-sheet-autofocus
              required
              minLength={1}
              maxLength={128}
              value={displayName}
              disabled={busy}
              onChange={(event) => setDisplayName(event.target.value)}
            />
          </label>
          <label>
            <span>{t("identity.serviceAccountApplication")}</span>
            <select
              value={application}
              disabled={busy}
              onChange={(event) => setApplication(event.target.value as IdentityApplication)}
            >
              <option value="admin">{t("identity.serviceAccountApplication.admin")}</option>
              <option value="user">{t("identity.serviceAccountApplication.user")}</option>
            </select>
          </label>
          <label>
            <span>{t("identity.serviceAccountRole")}</span>
            <select
              value={selectedRole?.spec.name ?? ""}
              disabled={busy || data?.roles.length === 0}
              onChange={(event) => {
                setRoleName(event.target.value);
                setScopeKey("");
              }}
            >
              {data?.roles.map((role) => (
                <option key={role.metadata.uid} value={role.spec.name}>
                  {role.spec.name}
                </option>
              ))}
            </select>
          </label>
          <label>
            <span>{t("identity.serviceAccountScope")}</span>
            <select
              value={selectedScope?.key ?? ""}
              disabled={busy || choices.length === 0}
              onChange={(event) => setScopeKey(event.target.value)}
            >
              {choices.length === 0 ? (
                <option value="">{t("identity.noScope")}</option>
              ) : (
                choices.map((choice) => (
                  <option key={choice.key} value={choice.key}>
                    {choice.label}
                  </option>
                ))
              )}
            </select>
          </label>
          <button
            type="submit"
            className="button primary"
            disabled={
              busy ||
              credential !== null ||
              displayName.trim() === "" ||
              selectedRole === undefined ||
              selectedScope === undefined
            }
          >
            {t("identity.serviceAccountCreate")}
          </button>
        </form>
        <section className="resource-form" aria-busy={busy}>
          <h3>{t("identity.serviceAccountList")}</h3>
          {data === null && busy ? <p>{t("identity.serviceAccountLoading")}</p> : null}
          {data?.accounts.length === 0 ? <p>{t("identity.serviceAccountEmpty")}</p> : null}
          <ul className="identity-invitation-list">
            {data?.accounts.map((account) => (
              <li key={account.id}>
                <div>
                  <strong>{account.displayName}</strong>
                  <small>{account.id}</small>
                  <small>
                    {account.application} · {account.roleName} ·{" "}
                    {scopeLabel(account, tenant, data.organizations, projects)}
                  </small>
                  <small>{t(`identity.serviceAccountState.${account.state}`)}</small>
                </div>
                {account.state === "active" ? (
                  <div className="form-actions">
                    <button
                      type="button"
                      className="button outline compact"
                      disabled={busy || credential !== null}
                      aria-label={t("identity.serviceAccountRotateLabel", {
                        account: account.displayName,
                      })}
                      onClick={() => rotate(account)}
                    >
                      {t("identity.serviceAccountRotate")}
                    </button>
                    {disableId === account.id ? (
                      <div>
                        <p>
                          {t("identity.serviceAccountDisableConfirm", {
                            account: account.displayName,
                          })}
                        </p>
                        <button
                          type="button"
                          className="button danger compact"
                          disabled={busy}
                          onClick={() => disable(account)}
                        >
                          {t("identity.serviceAccountDisable")}
                        </button>
                        <button
                          type="button"
                          className="button outline compact"
                          disabled={busy}
                          onClick={() => setDisableId("")}
                        >
                          {t("identity.cancel")}
                        </button>
                      </div>
                    ) : (
                      <button
                        type="button"
                        className="button danger compact"
                        disabled={busy}
                        aria-label={t("identity.serviceAccountDisableLabel", {
                          account: account.displayName,
                        })}
                        onClick={() => setDisableId(account.id)}
                      >
                        {t("identity.serviceAccountDisable")}
                      </button>
                    )}
                  </div>
                ) : null}
              </li>
            ))}
          </ul>
        </section>
      </div>
    </AdminSheet>
  );
}
