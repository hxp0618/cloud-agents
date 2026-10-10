import { useEffect, useMemo, useRef, useState, type FormEvent } from "react";
import {
  ClientError,
  type BrowserSessionClient,
  type BrowserTenant,
  type Client,
  type IdentityAccount,
  type Membership,
  type Organization,
  type Project,
  type Role,
  type RoleBinding,
  type RoleName,
} from "@cloud-agents/cloud-agent-platform-sdk/platform";

import { listAllAdminOrganizations } from "./app/connection";
import { adminErrorKey } from "./admin";
import { useI18n } from "./i18n";

const pageSize = 200;

type GrantableRoleName = Exclude<RoleName, "platform.admin">;
type GrantableRole = Role & Readonly<{ spec: Role["spec"] & { name: GrantableRoleName } }>;

type LoadedMembers = Readonly<{
  tenantRevision: number;
  accounts: readonly IdentityAccount[];
  memberships: readonly Membership[];
  roleBindings: readonly RoleBinding[];
  roles: readonly GrantableRole[];
  organizations: readonly Organization[];
}>;

type ScopeChoice = Readonly<{
  key: string;
  label: string;
  scope: Membership["spec"]["scope"];
}>;

function requestId(): string {
  return `members-${crypto.randomUUID()}`;
}

function mutationId(prefix: string): string {
  return `${prefix}-${crypto.randomUUID()}`;
}

function safeRevision(value: string): number {
  if (!/^[1-9][0-9]{0,18}$/u.test(value)) throw new Error("Invalid resource revision.");
  const revision = Number(value);
  if (!Number.isSafeInteger(revision)) throw new Error("Resource revision is not safely bounded.");
  return revision;
}

function subjectKey(subject: IdentityAccount["subject"]): string {
  return `${subject.kind}\u0000${subject.issuer}\u0000${subject.subject}`;
}

function isGrantableRole(role: Role): role is GrantableRole {
  return role.spec.state === "active" && role.spec.name !== "platform.admin";
}

async function listAllMemberships(client: Client, tenantId: string, signal: AbortSignal) {
  const result: Membership[] = [];
  const seen = new Set<string>();
  let pageToken: string | undefined;
  do {
    if (pageToken !== undefined) {
      if (seen.has(pageToken)) throw new Error("Membership pagination repeated a cursor.");
      seen.add(pageToken);
    }
    const page = await client.listAdminMemberships(
      tenantId,
      requestId(),
      pageSize,
      pageToken,
      signal,
    );
    result.push(...page.value.memberships);
    pageToken = page.value.nextPageToken;
  } while (pageToken !== undefined && pageToken !== "");
  return Object.freeze(result);
}

async function listAllRoleBindings(client: Client, tenantId: string, signal: AbortSignal) {
  const result: RoleBinding[] = [];
  const seen = new Set<string>();
  let pageToken: string | undefined;
  do {
    if (pageToken !== undefined) {
      if (seen.has(pageToken)) throw new Error("Role-binding pagination repeated a cursor.");
      seen.add(pageToken);
    }
    const page = await client.listAdminRoleBindings(
      tenantId,
      requestId(),
      pageSize,
      pageToken,
      signal,
    );
    result.push(...page.value.roleBindings);
    pageToken = page.value.nextPageToken;
  } while (pageToken !== undefined && pageToken !== "");
  return Object.freeze(result);
}

export async function listAllAdminRoles(client: Client, tenantId: string, signal: AbortSignal) {
  const result: Role[] = [];
  const seen = new Set<string>();
  let pageToken: string | undefined;
  do {
    if (pageToken !== undefined) {
      if (seen.has(pageToken)) throw new Error("Role pagination repeated a cursor.");
      seen.add(pageToken);
    }
    const page = await client.listAdminRoles(tenantId, requestId(), pageSize, pageToken, signal);
    result.push(...page.value.roles);
    pageToken = page.value.nextPageToken;
  } while (pageToken !== undefined && pageToken !== "");
  return Object.freeze(result);
}

async function listAllAccounts(
  sessionClient: BrowserSessionClient,
  tenantId: string,
  signal: AbortSignal,
) {
  const result: IdentityAccount[] = [];
  const seen = new Set<string>();
  let pageToken: string | undefined;
  do {
    if (pageToken !== undefined) {
      if (seen.has(pageToken)) throw new Error("Identity-account pagination repeated a cursor.");
      seen.add(pageToken);
    }
    const page = await sessionClient.listTenantIdentityAccounts(
      tenantId,
      pageSize,
      pageToken,
      signal,
    );
    result.push(...page.accounts);
    pageToken = page.nextPageToken;
  } while (pageToken !== undefined && pageToken !== "");
  return Object.freeze(result);
}

async function loadMembers(
  client: Client,
  sessionClient: BrowserSessionClient,
  tenantId: string,
  signal: AbortSignal,
): Promise<LoadedMembers> {
  const [tenant, accounts, memberships, roleBindings, roles, organizations] = await Promise.all([
    client.getAdminPlatformTenant(tenantId, requestId(), signal),
    listAllAccounts(sessionClient, tenantId, signal),
    listAllMemberships(client, tenantId, signal),
    listAllRoleBindings(client, tenantId, signal),
    listAllAdminRoles(client, tenantId, signal),
    listAllAdminOrganizations(client, tenantId, signal),
  ]);
  return Object.freeze({
    tenantRevision: safeRevision(tenant.value.metadata.resourceVersion),
    accounts,
    memberships,
    roleBindings,
    roles: Object.freeze(roles.filter(isGrantableRole)),
    organizations: Object.freeze(organizations.filter(({ spec }) => spec.state === "active")),
  });
}

function scopeId(scope: Membership["spec"]["scope"]): string {
  return scope.ref?.id ?? "";
}

function scopeSpecificity(scope: Membership["spec"]["scope"]): number {
  return scope.level === "tenant" ? 1 : scope.level === "organization" ? 2 : 3;
}

function membershipCovers(
  membership: Membership,
  scope: Membership["spec"]["scope"],
  projects: readonly Project[],
): boolean {
  const memberScope = membership.spec.scope;
  if (memberScope.level === "tenant") return scope.level !== "platform";
  if (memberScope.level === "organization") {
    if (scope.level === "organization") return scopeId(scope) === scopeId(memberScope);
    if (scope.level !== "project") return false;
    return projects.some(
      ({ metadata, spec }) =>
        metadata.uid === scopeId(scope) && spec.organizationRef.id === scopeId(memberScope),
    );
  }
  return (
    memberScope.level === "project" &&
    scope.level === "project" &&
    scopeId(scope) === scopeId(memberScope)
  );
}

function bindingOwner(
  binding: RoleBinding,
  memberships: readonly Membership[],
  projects: readonly Project[],
): string | undefined {
  return memberships
    .filter(
      (membership) =>
        subjectKey(membership.spec.subject) === subjectKey(binding.spec.subject) &&
        membership.spec.state !== "revoked" &&
        membershipCovers(membership, binding.spec.scope, projects),
    )
    .toSorted(
      (left, right) =>
        scopeSpecificity(right.spec.scope) - scopeSpecificity(left.spec.scope) ||
        left.metadata.uid.localeCompare(right.metadata.uid),
    )[0]?.metadata.uid;
}

function roleScopeLevel(role: Role): "tenant" | "organization" | "project" {
  if (role.spec.name === "tenant.admin") return "tenant";
  if (role.spec.name === "organization.admin") return "organization";
  return "project";
}

function scopeChoices(
  role: GrantableRole,
  membership: Membership,
  tenant: BrowserTenant,
  organizations: readonly Organization[],
  projects: readonly Project[],
): readonly ScopeChoice[] {
  const level = roleScopeLevel(role);
  const choices =
    level === "tenant"
      ? [
          {
            key: `tenant:${tenant.id}`,
            label: tenant.name,
            scope: {
              level: "tenant" as const,
              ref: { namespace: "cloud-agents", kind: "tenant", id: tenant.id },
            },
          },
        ]
      : level === "organization"
        ? organizations.map(({ metadata, spec }) => ({
            key: `organization:${metadata.uid}`,
            label: spec.displayName,
            scope: {
              level: "organization" as const,
              ref: { namespace: "cloud-agents", kind: "organization", id: metadata.uid },
            },
          }))
        : projects.map(({ metadata, spec }) => ({
            key: `project:${metadata.uid}`,
            label: spec.displayName,
            scope: {
              level: "project" as const,
              ref: { namespace: "cloud-agents", kind: "project", id: metadata.uid },
            },
          }));
  return Object.freeze(
    choices.filter(({ scope }) => membershipCovers(membership, scope, projects)),
  );
}

function scopeLabel(
  scope: Membership["spec"]["scope"],
  tenant: BrowserTenant,
  organizations: readonly Organization[],
  projects: readonly Project[],
): string {
  const id = scopeId(scope);
  if (scope.level === "tenant") return tenant.name;
  if (scope.level === "organization")
    return organizations.find(({ metadata }) => metadata.uid === id)?.spec.displayName ?? id;
  if (scope.level === "project")
    return projects.find(({ metadata }) => metadata.uid === id)?.spec.displayName ?? id;
  return id;
}

export function MembershipManagement({
  client,
  sessionClient,
  tenant,
  projects,
}: Readonly<{
  client: Client;
  sessionClient: BrowserSessionClient;
  tenant: BrowserTenant;
  projects: readonly Project[];
}>) {
  const { t } = useI18n();
  const [data, setData] = useState<LoadedMembers | null>(null);
  const [busy, setBusy] = useState(true);
  const [error, setError] = useState("");
  const [notice, setNotice] = useState("");
  const lifetime = useRef<AbortController | null>(null);

  useEffect(() => {
    const controller = new AbortController();
    lifetime.current = controller;
    setBusy(true);
    setError("");
    void loadMembers(
      client,
      sessionClient,
      tenant.id,
      AbortSignal.any([controller.signal, AbortSignal.timeout(15_000)]),
    )
      .then((loaded) => {
        if (!controller.signal.aborted) setData(loaded);
      })
      .catch((cause) => {
        if (!controller.signal.aborted) setError(t(adminErrorKey(cause)));
      })
      .finally(() => {
        if (!controller.signal.aborted) setBusy(false);
      });
    return () => {
      controller.abort();
      if (lifetime.current === controller) lifetime.current = null;
    };
  }, [client, sessionClient, tenant.id, t]);

  const accountsBySubject = useMemo(
    () => new Map(data?.accounts.map((account) => [subjectKey(account.subject), account]) ?? []),
    [data?.accounts],
  );

  async function mutate(
    action: (snapshot: LoadedMembers, signal: AbortSignal) => Promise<void>,
    success: string,
  ) {
    if (busy || data === null) return;
    setBusy(true);
    setError("");
    setNotice("");
    const controller = lifetime.current;
    if (controller === null) return;
    const signal = AbortSignal.any([controller.signal, AbortSignal.timeout(15_000)]);
    let failure: unknown;
    try {
      await action(data, signal);
    } catch (cause) {
      failure = cause;
    }
    try {
      const refreshed = await loadMembers(client, sessionClient, tenant.id, signal);
      if (controller.signal.aborted) return;
      setData(refreshed);
      if (failure === undefined) setNotice(success);
      else
        setError(
          failure instanceof ClientError && failure.status === 403
            ? t("identity.memberActionDenied")
            : failure instanceof ClientError && failure.status === 409
              ? t("identity.memberActionConflict")
              : t(adminErrorKey(failure)),
        );
    } catch {
      if (!controller.signal.aborted) setError(t("identity.memberRefreshFailed"));
    } finally {
      if (!controller.signal.aborted) setBusy(false);
    }
  }

  if (data === null)
    return (
      <section className="resource-form" aria-busy={busy}>
        <h3>{t("identity.members")}</h3>
        {busy ? <p>{t("identity.memberLoading")}</p> : null}
        {error ? (
          <div className="error-banner" role="alert">
            {error}
          </div>
        ) : null}
      </section>
    );

  return (
    <section className="resource-form" aria-busy={busy}>
      <div>
        <h3>{t("identity.members")}</h3>
        <p>{t("identity.membersHelp")}</p>
      </div>
      {error ? (
        <div className="error-banner" role="alert">
          {error}
        </div>
      ) : null}
      {notice ? <p role="status">{notice}</p> : null}
      {data.memberships.length === 0 ? (
        <p>{t("identity.memberEmpty")}</p>
      ) : (
        <ul className="identity-invitation-list">
          {data.memberships.map((membership) => {
            const account = accountsBySubject.get(subjectKey(membership.spec.subject));
            const name = account?.displayName || account?.email || t("identity.memberUnknown");
            const memberScope = scopeLabel(
              membership.spec.scope,
              tenant,
              data.organizations,
              projects,
            );
            const bindings = data.roleBindings.filter(
              (binding) =>
                bindingOwner(binding, data.memberships, projects) === membership.metadata.uid,
            );
            return (
              <MemberEntry
                key={membership.metadata.uid}
                account={account}
                bindings={bindings}
                busy={busy}
                client={client}
                data={data}
                label={`${name} — ${memberScope}`}
                membership={membership}
                projects={projects}
                tenant={tenant}
                onMutate={mutate}
              />
            );
          })}
        </ul>
      )}
    </section>
  );
}

function MemberEntry({
  account,
  bindings,
  busy,
  client,
  data,
  label,
  membership,
  projects,
  tenant,
  onMutate,
}: Readonly<{
  account: IdentityAccount | undefined;
  bindings: readonly RoleBinding[];
  busy: boolean;
  client: Client;
  data: LoadedMembers;
  label: string;
  membership: Membership;
  projects: readonly Project[];
  tenant: BrowserTenant;
  onMutate: (
    action: (snapshot: LoadedMembers, signal: AbortSignal) => Promise<void>,
    success: string,
  ) => Promise<void>;
}>) {
  const { t } = useI18n();
  const availableRoles = data.roles.filter((role) => {
    const choices = scopeChoices(role, membership, tenant, data.organizations, projects);
    return choices.some(
      ({ scope }) =>
        !bindings.some(
          (binding) =>
            binding.spec.state === "active" &&
            binding.spec.roleName === role.spec.name &&
            binding.spec.scope.level === scope.level &&
            scopeId(binding.spec.scope) === scopeId(scope),
        ),
    );
  });
  const [roleName, setRoleName] = useState(availableRoles[0]?.spec.name ?? "");
  const selectedRole =
    availableRoles.find(({ spec }) => spec.name === roleName) ?? availableRoles[0];
  const choices = selectedRole
    ? scopeChoices(selectedRole, membership, tenant, data.organizations, projects).filter(
        ({ scope }) =>
          !bindings.some(
            (binding) =>
              binding.spec.state === "active" &&
              binding.spec.roleName === selectedRole.spec.name &&
              binding.spec.scope.level === scope.level &&
              scopeId(binding.spec.scope) === scopeId(scope),
          ),
      )
    : [];
  const [selectedScope, setSelectedScope] = useState(choices[0]?.key ?? "");
  const choice = choices.find(({ key }) => key === selectedScope) ?? choices[0];
  const canGrant =
    membership.spec.state === "active" &&
    account?.state !== "disabled" &&
    selectedRole !== undefined &&
    choice !== undefined;

  function transition(action: "suspend" | "resume") {
    void onMutate(
      async (snapshot, signal) => {
        const body = {
          expectedTenantRevision: snapshot.tenantRevision,
          expectedResourceVersion: safeRevision(membership.metadata.resourceVersion),
          auditFactUid: mutationId(`audit-membership-${action}`),
          reasonCode: `admin.member.${action}`,
        };
        if (action === "suspend")
          await client.suspendAdminMembership(
            tenant.id,
            membership.metadata.uid,
            requestId(),
            body,
            signal,
          );
        else
          await client.resumeAdminMembership(
            tenant.id,
            membership.metadata.uid,
            requestId(),
            body,
            signal,
          );
      },
      t(action === "suspend" ? "identity.memberSuspended" : "identity.memberResumed", {
        member: label,
      }),
    );
  }

  function grant(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!canGrant || choice === undefined || selectedRole === undefined) return;
    void onMutate(
      async (snapshot, signal) => {
        const id = mutationId("role-binding");
        await client.bindAdminRole(
          tenant.id,
          requestId(),
          {
            expectedTenantRevision: snapshot.tenantRevision,
            roleBindingId: id,
            roleBindingName: id,
            subject: membership.spec.subject,
            roleName: selectedRole.spec.name,
            roleVersion: selectedRole.spec.version,
            scope: choice.scope,
            auditFactUid: mutationId("audit-role-binding"),
            reasonCode: "admin.member.role-bind",
          },
          signal,
        );
      },
      t("identity.memberRoleGranted", { member: label }),
    );
  }

  return (
    <li>
      <div>
        <strong>{account?.displayName || account?.email || t("identity.memberUnknown")}</strong>
        <small>{account?.email ?? membership.spec.subject.subject}</small>
        <small>{t(`identity.memberState.${membership.spec.state}`)}</small>
        <small>{label.split(" — ")[1]}</small>
        {bindings.length === 0 ? (
          <small>{t("identity.memberNoRoles")}</small>
        ) : (
          <ul className="identity-invitation-list">
            {bindings.map((binding) => (
              <li key={binding.metadata.uid}>
                <div>
                  <strong>{binding.spec.roleName}</strong>
                  <small>
                    {scopeLabel(binding.spec.scope, tenant, data.organizations, projects)} ·{" "}
                    {binding.spec.state}
                  </small>
                </div>
                {binding.spec.state === "active" ? (
                  <button
                    type="button"
                    className="button outline compact"
                    disabled={busy}
                    aria-label={t("identity.memberRemoveRoleLabel", {
                      role: binding.spec.roleName,
                      member: label,
                    })}
                    onClick={() => {
                      void onMutate(
                        async (snapshot, signal) => {
                          await client.revokeAdminRoleBinding(
                            tenant.id,
                            binding.metadata.uid,
                            requestId(),
                            {
                              expectedTenantRevision: snapshot.tenantRevision,
                              expectedResourceVersion: safeRevision(
                                binding.metadata.resourceVersion,
                              ),
                              auditFactUid: mutationId("audit-role-binding-revoke"),
                              reasonCode: "admin.member.role-revoke",
                            },
                            signal,
                          );
                        },
                        t("identity.memberRoleRemoved", { member: label }),
                      );
                    }}
                  >
                    {t("identity.memberRemoveRole")}
                  </button>
                ) : null}
              </li>
            ))}
          </ul>
        )}
        {membership.spec.state === "active" ? (
          <button
            type="button"
            className="button outline compact"
            disabled={busy}
            aria-label={t("identity.memberSuspendLabel", { member: label })}
            onClick={() => transition("suspend")}
          >
            {t("identity.memberSuspend")}
          </button>
        ) : membership.spec.state === "suspended" ? (
          <button
            type="button"
            className="button outline compact"
            disabled={busy}
            aria-label={t("identity.memberResumeLabel", { member: label })}
            onClick={() => transition("resume")}
          >
            {t("identity.memberResume")}
          </button>
        ) : null}
        <form className="resource-form" onSubmit={grant}>
          <label>
            <span>{t("identity.memberRoleFor", { member: label })}</span>
            <select
              value={selectedRole?.spec.name ?? ""}
              disabled={busy || availableRoles.length === 0 || membership.spec.state !== "active"}
              onChange={(event) => {
                setRoleName(event.target.value);
                setSelectedScope("");
              }}
            >
              {availableRoles.map((role) => (
                <option key={role.metadata.uid} value={role.spec.name}>
                  {role.spec.name}
                </option>
              ))}
            </select>
          </label>
          <label>
            <span>{t("identity.memberScopeFor", { member: label })}</span>
            <select
              value={choice?.key ?? ""}
              disabled={busy || choices.length === 0 || membership.spec.state !== "active"}
              onChange={(event) => setSelectedScope(event.target.value)}
            >
              {choices.map((entry) => (
                <option key={entry.key} value={entry.key}>
                  {entry.label}
                </option>
              ))}
            </select>
          </label>
          <button
            type="submit"
            className="button primary compact"
            aria-label={t("identity.memberAddRoleFor", { member: label })}
            disabled={busy || !canGrant}
          >
            {t("identity.memberAddRole")}
          </button>
        </form>
      </div>
    </li>
  );
}
