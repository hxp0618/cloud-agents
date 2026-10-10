import { useEffect, useState, type FormEvent } from "react";
import { LocaleSelect, useI18n, type MessageKey } from "../i18n";
import { ProviderLogin } from "../../../user-web/src/ProviderLogin";
import {
  ClientError,
  type BrowserSession,
  type BrowserSessionClient,
  type BrowserTenant,
  type Client,
  type Project,
} from "@cloud-agents/cloud-agent-platform-sdk/platform";

const pageSize = 200;

function requestId(): string {
  return `web-${crypto.randomUUID()}`;
}

export async function listAllBrowserTenants(
  sessionClient: Pick<BrowserSessionClient, "listBrowserTenants">,
  session: BrowserSession,
  signal: AbortSignal,
): Promise<readonly BrowserTenant[]> {
  const tenants = [...session.tenants];
  const seenTenantIds = new Set(tenants.map(({ id }) => id));
  const seenTokens = new Set<string>();
  let pageToken = session.nextPageToken;
  while (pageToken !== undefined && pageToken !== "") {
    if (seenTokens.has(pageToken)) throw new Error("Identity tenant pagination repeated a cursor.");
    seenTokens.add(pageToken);
    const page = await sessionClient.listBrowserTenants(pageSize, pageToken, signal);
    for (const tenant of page.tenants) {
      if (seenTenantIds.has(tenant.id))
        throw new Error("Identity tenant pagination repeated a tenant.");
      seenTenantIds.add(tenant.id);
      tenants.push(tenant);
    }
    pageToken = page.nextPageToken;
  }
  return Object.freeze(tenants);
}

export async function listAllAdminOrganizations(
  client: Pick<Client, "listAdminOrganizations">,
  tenantId: string,
  signal: AbortSignal,
) {
  const organizations = [];
  const seenTokens = new Set<string>();
  let pageToken: string | undefined;
  do {
    if (pageToken !== undefined) {
      if (seenTokens.has(pageToken)) throw new Error("Organization pagination repeated a cursor.");
      seenTokens.add(pageToken);
    }
    const { value } = await client.listAdminOrganizations(
      tenantId,
      requestId(),
      pageSize,
      pageToken,
      signal,
    );
    organizations.push(...value.organizations);
    pageToken = value.nextPageToken;
  } while (pageToken !== undefined && pageToken !== "");
  return organizations;
}

async function listAllAdminOrganizationProjects(
  client: Pick<Client, "listAdminProjects">,
  tenantId: string,
  organizationId: string,
  signal: AbortSignal,
): Promise<readonly Project[]> {
  const projects: Project[] = [];
  const seenTokens = new Set<string>();
  let pageToken: string | undefined;
  do {
    if (pageToken !== undefined) {
      if (seenTokens.has(pageToken)) throw new Error("Project pagination repeated a cursor.");
      seenTokens.add(pageToken);
    }
    const { value } = await client.listAdminProjects(
      tenantId,
      organizationId,
      requestId(),
      pageSize,
      pageToken,
      signal,
    );
    projects.push(...value.projects);
    pageToken = value.nextPageToken;
  } while (pageToken !== undefined && pageToken !== "");
  return projects;
}

export async function listAllAdminProjects(
  client: Pick<Client, "listAdminOrganizations" | "listAdminProjects">,
  tenantId: string,
  signal: AbortSignal,
): Promise<readonly Project[]> {
  const organizations = await listAllAdminOrganizations(client, tenantId, signal);
  const pages = await Promise.all(
    organizations.map(({ metadata }) =>
      listAllAdminOrganizationProjects(client, tenantId, metadata.uid, signal),
    ),
  );
  return Object.freeze(
    pages
      .flat()
      .filter(({ spec }) => spec.state === "active")
      .toSorted(
        (left, right) =>
          left.spec.displayName.localeCompare(right.spec.displayName) ||
          left.metadata.uid.localeCompare(right.metadata.uid),
      ),
  );
}

export function sessionMessage(error: unknown): MessageKey {
  if (error instanceof Error && error.message === "session_wrong_console")
    return "auth.wrongConsole";
  if (error instanceof ClientError && error.status === 401) return "auth.sessionExpired";
  if (error instanceof ClientError && error.status === 403) return "auth.accountDenied";
  if (error instanceof ClientError && error.status === 429) return "error.rateLimited";
  return "auth.accountLoadFailed";
}

export function loginMessage(error: unknown): MessageKey {
  if (error instanceof Error && error.message === "session_wrong_console")
    return "auth.wrongConsole";
  if (error instanceof ClientError && error.status === 401) return "auth.invalidCredentials";
  if (error instanceof ClientError && error.status === 403) return "auth.accountDenied";
  if (error instanceof ClientError && error.status === 429) return "error.rateLimited";
  return "auth.loginFailed";
}

export function LoginView({
  client,
  busy,
  error,
  onSubmit,
}: Readonly<{
  client: BrowserSessionClient;
  busy: boolean;
  error: string;
  onSubmit: (email: string, password: string) => Promise<void>;
}>) {
  const { t } = useI18n();
  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");
  const [callbackFailed] = useState(() => window.location.hash === "#identity=failed");
  useEffect(() => {
    if (window.location.hash.startsWith("#identity="))
      window.history.replaceState(null, "", window.location.pathname + window.location.search);
  }, []);

  function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    void onSubmit(email, password);
  }

  return (
    <main className="connect-view">
      <div className="connect-preferences">
        <LocaleSelect />
      </div>
      <section className="connect-card" aria-labelledby="admin-login-title">
        <div className="brand-lockup">
          <span className="brand-mark" aria-hidden="true">
            CA
          </span>
          <span>
            <strong>{t("brand.cloudAgents")}</strong>
            <small>{t("brand.adminConsole")}</small>
          </span>
        </div>
        <h1 id="admin-login-title">{t("auth.loginTitle")}</h1>
        <p className="lede">{t("auth.loginDescription")}</p>
        <form className="connect-form" onSubmit={submit}>
          <label>
            <span>{t("identity.email")}</span>
            <input
              type="email"
              value={email}
              autoComplete="username"
              disabled={busy}
              required
              onChange={(event) => setEmail(event.target.value)}
            />
          </label>
          <label>
            <span>{t("auth.password")}</span>
            <input
              type="password"
              value={password}
              autoComplete="current-password"
              disabled={busy}
              required
              onChange={(event) => setPassword(event.target.value)}
            />
          </label>
          {error || callbackFailed ? (
            <div className="error-banner" role="alert">
              {error || t("identity.providerFailed")}
            </div>
          ) : null}
          <button className="button primary" type="submit" disabled={busy}>
            {t(busy ? "auth.signingIn" : "auth.signIn")}
          </button>
        </form>
        <ProviderLogin
          client={client}
          disabled={busy}
          labels={{
            loading: t("identity.providerLoading"),
            start: (provider) => t("identity.providerContinue", { provider }),
            failed: t("identity.providerFailed"),
          }}
        />
      </section>
    </main>
  );
}
