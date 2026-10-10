import { CLIAuthorization } from "./CLIAuthorization";
import { useEffect, useRef, useState, type FormEvent } from "react";
import {
  ClientError,
  createBrowserHTTPClient,
  createSessionHTTPClient,
  type BrowserSession,
  type BrowserSessionClient,
  type BrowserTenant,
  type Client,
  type Project,
} from "@cloud-agents/cloud-agent-platform-sdk/platform";

import {
  listAllBrowserTenants,
  loadUserProjects,
  readSavedConnection,
  sessionErrorMessage,
  writeSavedConnection,
  type SavedConnection,
} from "./connection";
import { EnvironmentWorkspace } from "./EnvironmentWorkspace";
import { InvitationAcceptance } from "./InvitationAcceptance";
import { PasswordForm } from "./PasswordForm";
import { ProviderLogin } from "./ProviderLogin";
import { LinkedLogins, type IdentityReturnState } from "./LinkedLogins";

type SessionState = "restoring" | "login" | "ready";

function LoginView({
  client,
  busy,
  error,
  onSubmit,
  notice = "",
}: Readonly<{
  client: BrowserSessionClient;
  busy: boolean;
  error: string;
  onSubmit: (email: string, password: string) => Promise<void>;
  notice?: string;
}>) {
  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");

  function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    void onSubmit(email, password);
  }

  return (
    <main className="connect-view">
      <section className="connect-card panel" aria-labelledby="user-login-title">
        <div className="eyebrow">Secure session</div>
        <h1 id="user-login-title">Sign in to Cloud Agents</h1>
        <p className="lede">
          Your account supplies the available tenants and projects. Tokens stay behind this web
          server and never enter browser storage.
        </p>
        {notice ? <p role="status">{notice}</p> : null}
        <form className="connect-form" onSubmit={submit}>
          <label>
            <span>Email</span>
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
            <span>Password</span>
            <input
              type="password"
              value={password}
              autoComplete="current-password"
              disabled={busy}
              required
              onChange={(event) => setPassword(event.target.value)}
            />
          </label>
          {error ? (
            <div className="error-banner" role="alert">
              {error}
            </div>
          ) : null}
          <button className="button primary" type="submit" disabled={busy}>
            {busy ? "Signing in…" : "Sign in"}
          </button>
        </form>
        <ProviderLogin client={client} disabled={busy} />
      </section>
      <aside className="connection-notes" aria-label="Session protection">
        <div className="ambient-card">
          <span className="note-index">01</span>
          <strong>Account scope</strong>
          <p>Only active memberships and their projects appear after sign-in.</p>
        </div>
        <div className="ambient-card">
          <span className="note-index">02</span>
          <strong>Server-held access</strong>
          <p>The browser keeps only a protected session cookie.</p>
        </div>
      </aside>
    </main>
  );
}

export function App() {
  const [cliAuthorization] = useState(() => window.location.hash === "#cli");
  const [anonymousSessionClient] = useState(() => createSessionHTTPClient(window.location.origin));
  const [sessionClient, setSessionClient] = useState<BrowserSessionClient | null>(null);
  const [client, setClient] = useState<Client | null>(null);
  const [session, setSession] = useState<BrowserSession | null>(null);
  const [tenants, setTenants] = useState<readonly BrowserTenant[]>([]);
  const [projects, setProjects] = useState<readonly Project[]>([]);
  const [connection, setConnection] = useState<SavedConnection>(() =>
    readSavedConnection(window.sessionStorage),
  );
  const [state, setState] = useState<SessionState>("restoring");
  const [error, setError] = useState("");
  const [identityReturnState, setIdentityReturnState] = useState(
    () =>
      /^#identity=(reauthenticated|linked|failed)$/.exec(window.location.hash)?.[1] as
        | IdentityReturnState
        | undefined,
  );
  const [notice, setNotice] = useState(
    identityReturnState === "failed" ? "External sign-in could not be completed. Try again." : "",
  );
  const [accountOpen, setAccountOpen] = useState(identityReturnState !== undefined);
  const [linkedOpen, setLinkedOpen] = useState(identityReturnState !== undefined);
  const [resetCode, setResetCode] = useState(
    () => /^#password-reset=([A-Za-z0-9_-]{43})$/.exec(window.location.hash)?.[1] ?? "",
  );
  const [invitationCode, setInvitationCode] = useState(
    () => /^#invitation=([A-Za-z0-9_-]{43})$/.exec(window.location.hash)?.[1] ?? "",
  );
  const requestRef = useRef<AbortController | null>(null);

  useEffect(() => {
    if (
      window.location.hash.startsWith("#invitation=") ||
      window.location.hash.startsWith("#password-reset=") ||
      window.location.hash.startsWith("#identity=")
    )
      window.history.replaceState(null, "", window.location.pathname + window.location.search);
  }, []);

  function clearSession(message = "") {
    requestRef.current?.abort();
    requestRef.current = null;
    setSession(null);
    setAccountOpen(false);
    setLinkedOpen(false);
    setIdentityReturnState(undefined);
    setSessionClient(null);
    setClient(null);
    setTenants([]);
    setProjects([]);
    setError(message);
    setState("login");
  }

  async function activate(nextSession: BrowserSession, signal: AbortSignal) {
    if (nextSession.application !== "user")
      throw new Error("This session belongs to the Admin Console. Sign in to User Console.");
    const authenticatedSessionClient = createSessionHTTPClient(
      window.location.origin,
      nextSession.csrfToken,
    );
    const nextClient = createBrowserHTTPClient(window.location.origin, nextSession.csrfToken);
    const nextTenants = await listAllBrowserTenants(
      authenticatedSessionClient,
      nextSession,
      signal,
    );
    const tenantId = nextTenants.some(({ id }) => id === connection.tenantId)
      ? connection.tenantId
      : (nextTenants[0]?.id ?? "");
    const nextProjects =
      tenantId === "" ? [] : await loadUserProjects(nextClient, tenantId, signal);
    const projectId = nextProjects.some(({ metadata }) => metadata.uid === connection.projectId)
      ? connection.projectId
      : (nextProjects[0]?.metadata.uid ?? "");
    const nextConnection = { tenantId, projectId };
    setSession(nextSession);
    setSessionClient(authenticatedSessionClient);
    setClient(nextClient);
    setTenants(nextTenants);
    setProjects(nextProjects);
    setConnection(nextConnection);
    writeSavedConnection(window.sessionStorage, nextConnection);
    setError("");
    setState("ready");
  }

  useEffect(() => {
    window.sessionStorage.removeItem("cloud-agents.user-web.infrastructure.v1");
    const controller = new AbortController();
    requestRef.current = controller;
    const signal = AbortSignal.any([controller.signal, AbortSignal.timeout(15_000)]);
    void anonymousSessionClient
      .getBrowserSession(signal)
      .then((restored) => activate(restored, signal))
      .catch((cause: unknown) => {
        if (controller.signal.aborted) return;
        if (cause instanceof ClientError && cause.status === 401) clearSession();
        else clearSession(sessionErrorMessage(cause));
      });
    return () => controller.abort();
  }, [anonymousSessionClient]);

  async function login(email: string, password: string) {
    const controller = new AbortController();
    requestRef.current?.abort();
    requestRef.current = controller;
    setState("restoring");
    setError("");
    setNotice("");
    const signal = AbortSignal.any([controller.signal, AbortSignal.timeout(15_000)]);
    try {
      await activate(
        await anonymousSessionClient.passwordLogin({ email, password }, signal),
        signal,
      );
    } catch (cause) {
      if (!controller.signal.aborted) clearSession(sessionErrorMessage(cause));
    }
  }

  async function selectTenant(tenantId: string) {
    if (client === null) return;
    const controller = new AbortController();
    requestRef.current?.abort();
    requestRef.current = controller;
    setState("restoring");
    try {
      const nextProjects = await loadUserProjects(
        client,
        tenantId,
        AbortSignal.any([controller.signal, AbortSignal.timeout(15_000)]),
      );
      const nextConnection = { tenantId, projectId: nextProjects[0]?.metadata.uid ?? "" };
      setProjects(nextProjects);
      setConnection(nextConnection);
      writeSavedConnection(window.sessionStorage, nextConnection);
      setState("ready");
    } catch (cause) {
      if (!controller.signal.aborted) {
        setError(sessionErrorMessage(cause));
        setState("ready");
      }
    }
  }

  function selectProject(projectId: string) {
    const nextConnection = { ...connection, projectId };
    setConnection(nextConnection);
    writeSavedConnection(window.sessionStorage, nextConnection);
  }

  if (resetCode)
    return (
      <main className="connect-view">
        <section className="connect-card panel" aria-labelledby="password-reset-title">
          <div className="eyebrow">Account recovery</div>
          <h1 id="password-reset-title">Reset your password</h1>
          <PasswordForm
            client={anonymousSessionClient}
            resetCode={resetCode}
            onComplete={() => {
              setResetCode("");
              clearSession();
              setNotice("Password changed. Sign in with your new password.");
            }}
          />
          <button type="button" className="button outline" onClick={() => setResetCode("")}>
            Cancel
          </button>
        </section>
      </main>
    );
  if (invitationCode)
    return (
      <InvitationAcceptance
        code={invitationCode}
        user={session?.user}
        client={sessionClient ?? anonymousSessionClient}
        loginError={error}
        restoring={state === "restoring"}
        onLogin={login}
        onCancel={() => {
          setInvitationCode("");
          setError("");
        }}
        onAccepted={async () => {
          setInvitationCode("");
          if (session === null) {
            clearSession();
            setNotice(
              "Invitation accepted. Sign in with your invited email address and new password.",
            );
            return;
          }
          const controller = new AbortController();
          requestRef.current?.abort();
          requestRef.current = controller;
          setState("restoring");
          const signal = AbortSignal.any([controller.signal, AbortSignal.timeout(15_000)]);
          try {
            await activate(await anonymousSessionClient.getBrowserSession(signal), signal);
          } catch (cause) {
            if (!controller.signal.aborted) clearSession(sessionErrorMessage(cause));
          }
        }}
      />
    );
  if (state === "login")
    return (
      <LoginView
        client={anonymousSessionClient}
        busy={false}
        error={error}
        notice={notice}
        onSubmit={login}
      />
    );
  if (state === "restoring")
    return (
      <main className="connect-view" aria-live="polite">
        <section className="connect-card panel">
          <div className="eyebrow">Secure session</div>
          <h1>Loading your workspace…</h1>
          <p className="lede">Checking your session and available projects.</p>
        </section>
      </main>
    );
  if (session === null || sessionClient === null || client === null) return null;
  if (cliAuthorization) return <CLIAuthorization client={sessionClient} session={session} />;
  const tenant = tenants.find(({ id }) => id === connection.tenantId);
  const selectedProject = projects.find(({ metadata }) => metadata.uid === connection.projectId);

  return (
    <div className="app-shell">
      <header className="topbar">
        <div className="brand" aria-label="Cloud Agents User Console">
          <span className="brand-mark" aria-hidden="true">
            CA
          </span>
          <span>
            <strong>Cloud Agents</strong>
            <small>User Console</small>
          </span>
        </div>
        <div className="context-strip" aria-label="Current workspace context">
          <label className="context-item project-picker">
            <small>Tenant</small>
            <select
              aria-label="Current tenant"
              value={connection.tenantId}
              disabled={tenants.length === 0}
              onChange={(event) => void selectTenant(event.target.value)}
            >
              {tenants.map((item) => (
                <option key={item.id} value={item.id}>
                  {item.name}
                </option>
              ))}
            </select>
          </label>
          <label className="context-item project-picker">
            <small>Project</small>
            <select
              aria-label="Current project"
              value={connection.projectId}
              disabled={projects.length === 0}
              onChange={(event) => selectProject(event.target.value)}
            >
              {projects.map((project) => (
                <option key={project.metadata.uid} value={project.metadata.uid}>
                  {project.spec.displayName}
                </option>
              ))}
            </select>
          </label>
        </div>
        <div className="connection-actions">
          <span className="connection-state state-connected" role="status">
            <span className="status-dot" aria-hidden="true" />
            {session.user.displayName || session.user.email}
          </span>
          <button
            className="button ghost compact"
            type="button"
            onClick={() => {
              setAccountOpen(!accountOpen);
              if (accountOpen) {
                setLinkedOpen(false);
                setIdentityReturnState(undefined);
              }
            }}
          >
            {accountOpen ? "Back to workspace" : "Account"}
          </button>
          <button
            className="button ghost compact"
            type="button"
            onClick={() => void sessionClient.logoutBrowserSession().finally(() => clearSession())}
          >
            Sign out
          </button>
        </div>
      </header>

      {accountOpen ? (
        <main className="connect-view">
          <section className="connect-card panel" aria-labelledby="account-title">
            <h1 id="account-title">Your account</h1>
            <p>{session.user.email}</p>
            <h2>Change password</h2>
            <PasswordForm
              client={sessionClient}
              onComplete={() => {
                clearSession();
                setNotice("Password changed. Sign in with your new password.");
              }}
            />
            <details
              open={linkedOpen}
              onToggle={(event) => {
                setLinkedOpen(event.currentTarget.open);
                if (!event.currentTarget.open) setIdentityReturnState(undefined);
              }}
            >
              <summary>Linked sign-ins</summary>
              {linkedOpen ? (
                <LinkedLogins
                  client={sessionClient}
                  {...(identityReturnState === undefined
                    ? {}
                    : { returnState: identityReturnState })}
                  onSessionEnded={() => {
                    clearSession();
                    setNotice("Password enabled. Sign in with your new password.");
                  }}
                />
              ) : null}
            </details>
          </section>
        </main>
      ) : tenant === undefined || selectedProject === undefined ? (
        <main className="connect-view">
          <section className="connect-card panel" aria-labelledby="empty-scope-title">
            <div className="eyebrow">Account scope</div>
            <h1 id="empty-scope-title">
              {tenant === undefined
                ? "No tenants are available"
                : "No active projects are available"}
            </h1>
            <p className="lede">
              {tenant === undefined
                ? "An administrator must invite this account to a tenant before it can use Cloud Agents."
                : "Choose another tenant or ask its administrator for an active project membership."}
            </p>
            {error ? (
              <div className="error-banner" role="alert">
                {error}
              </div>
            ) : null}
          </section>
        </main>
      ) : (
        <div className="workspace">
          <EnvironmentWorkspace
            key={selectedProject.metadata.uid}
            client={client}
            tenantId={tenant.id}
            projectId={selectedProject.metadata.uid}
            projectName={selectedProject.spec.displayName}
          />
        </div>
      )}
    </div>
  );
}
