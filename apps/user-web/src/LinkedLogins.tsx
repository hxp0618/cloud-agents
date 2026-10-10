import { useEffect, useMemo, useRef, useState, type FormEvent } from "react";
import type {
  BrowserSessionClient,
  LoginMethod,
  LoginProvider,
} from "@cloud-agents/cloud-agent-platform-sdk/platform";

export type IdentityReturnState = "reauthenticated" | "linked" | "failed";

export type LinkedLoginLabels = Readonly<{
  title: string;
  help: string;
  loading: string;
  password: string;
  passwordEnabled: string;
  noPassword: string;
  currentPassword: string;
  reauthenticate: string;
  reauthenticateWith: (provider: string) => string;
  reauthenticated: string;
  link: (provider: string) => string;
  unlink: (provider: string) => string;
  issuer: (issuer: string) => string;
  subject: (subject: string) => string;
  newPassword: string;
  confirmPassword: string;
  enablePassword: string;
  passwordHelp: string;
  mismatch: string;
  failed: string;
  linked: string;
  unlinked: string;
}>;

export const linkedLoginLabels: LinkedLoginLabels = Object.freeze({
  title: "Linked sign-ins",
  help: "Reauthenticate before linking or removing a sign-in method.",
  loading: "Loading sign-in methods…",
  password: "Password",
  passwordEnabled: "Password sign-in is enabled.",
  noPassword: "This account does not have a password.",
  currentPassword: "Current password",
  reauthenticate: "Reauthenticate",
  reauthenticateWith: (provider) => `Reauthenticate with ${provider}`,
  reauthenticated: "Reauthentication complete. Choose one account change below.",
  link: (provider) => `Link ${provider}`,
  unlink: (provider) => `Unlink ${provider}`,
  issuer: (issuer) => `Issuer: ${issuer}`,
  subject: (subject) => `Subject: ${subject}`,
  newPassword: "New password",
  confirmPassword: "Confirm password",
  enablePassword: "Enable password",
  passwordHelp: "Use 15–128 characters.",
  mismatch: "Passwords do not match.",
  failed: "The sign-in method operation could not be completed. Refresh and try again.",
  linked: "The sign-in method was linked.",
  unlinked: "The sign-in method was removed.",
});

export function LinkedLogins({
  client,
  onSessionEnded,
  returnState,
  labels = linkedLoginLabels,
}: Readonly<{
  client: BrowserSessionClient;
  onSessionEnded: () => void;
  returnState?: IdentityReturnState;
  labels?: LinkedLoginLabels;
}>) {
  const [methods, setMethods] = useState<readonly LoginMethod[]>([]);
  const [providers, setProviders] = useState<readonly LoginProvider[]>([]);
  const [passwordEnabled, setPasswordEnabled] = useState(false);
  const [reauthenticated, setReauthenticated] = useState(returnState === "reauthenticated");
  const [currentPassword, setCurrentPassword] = useState("");
  const [newPassword, setNewPassword] = useState("");
  const [confirmation, setConfirmation] = useState("");
  const [busy, setBusy] = useState(true);
  const [error, setError] = useState(returnState === "failed" ? labels.failed : "");
  const [notice, setNotice] = useState(returnState === "linked" ? labels.linked : "");
  const request = useRef<AbortController | null>(null);
  const pending = useRef(false);
  const linkedProviders = useMemo(
    () => new Set(methods.map(({ providerId }) => providerId)),
    [methods],
  );
  const providerNames = useMemo(
    () => new Map(providers.map((provider) => [provider.id, provider.displayName])),
    [providers],
  );

  async function load(signal: AbortSignal) {
    const [methodList, providerPage] = await Promise.all([
      client.listLoginMethods(signal),
      client.listLoginProviders(signal),
    ]);
    if (signal.aborted) return;
    setMethods(methodList.loginMethods);
    setPasswordEnabled(methodList.passwordEnabled);
    setProviders(providerPage.providers);
  }

  useEffect(() => {
    const controller = new AbortController();
    request.current = controller;
    const signal = AbortSignal.any([controller.signal, AbortSignal.timeout(15_000)]);
    setBusy(true);
    void load(signal)
      .catch(() => {
        if (!controller.signal.aborted) setError(labels.failed);
      })
      .finally(() => {
        if (!controller.signal.aborted) setBusy(false);
      });
    return () => request.current?.abort();
  }, [client, labels.failed]);

  async function run(action: (signal: AbortSignal) => Promise<void>) {
    if (busy || pending.current) return;
    pending.current = true;
    request.current?.abort();
    const controller = new AbortController();
    request.current = controller;
    const signal = AbortSignal.any([controller.signal, AbortSignal.timeout(15_000)]);
    setBusy(true);
    setError("");
    setNotice("");
    try {
      await action(signal);
    } catch {
      if (!controller.signal.aborted) {
        setError(labels.failed);
        await load(signal).catch(() => undefined);
      }
    } finally {
      pending.current = false;
      if (!controller.signal.aborted) setBusy(false);
    }
  }

  function reauthenticateWithPassword(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    void run(async (signal) => {
      await client.passwordReauthenticate({ password: currentPassword }, signal);
      if (signal.aborted) return;
      setCurrentPassword("");
      window.location.assign("/#identity=reauthenticated");
      window.location.reload();
    });
  }

  function redirect(providerId: string, purpose: "reauth" | "link") {
    void run(async (signal) => {
      if (purpose === "link") setReauthenticated(false);
      const authorization = await client.startProviderAuthorization(
        { providerId, purpose },
        signal,
      );
      if (!signal.aborted) window.location.assign(authorization.authorizationUrl);
    });
  }

  function unlink(method: LoginMethod) {
    void run(async (signal) => {
      setReauthenticated(false);
      await client.unlinkLoginMethod(method.id, signal);
      await load(signal);
      if (!signal.aborted) setNotice(labels.unlinked);
    });
  }

  function enablePassword(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (newPassword !== confirmation) {
      setError(labels.mismatch);
      return;
    }
    void run(async (signal) => {
      await client.enablePassword({ newPassword }, signal);
      if (!signal.aborted) onSessionEnded();
    });
  }

  return (
    <section className="resource-form" aria-labelledby="linked-logins-title" aria-busy={busy}>
      <header>
        <h2 id="linked-logins-title">{labels.title}</h2>
        <p>{labels.help}</p>
      </header>
      {busy && methods.length === 0 ? <p role="status">{labels.loading}</p> : null}
      <div>
        <strong>{labels.password}</strong>
        <p>{passwordEnabled ? labels.passwordEnabled : labels.noPassword}</p>
      </div>
      <ul className="identity-invitation-list">
        {methods.map((method) => {
          const name = providerNames.get(method.providerId) ?? method.providerId;
          return (
            <li key={method.id}>
              <div>
                <strong>{name}</strong>
                <small>{labels.issuer(method.issuer)}</small>
                <small>{labels.subject(method.subject)}</small>
              </div>
              <button
                type="button"
                className="button outline compact"
                disabled={busy || !reauthenticated}
                aria-label={labels.unlink(name)}
                onClick={() => unlink(method)}
              >
                {labels.unlink(name)}
              </button>
            </li>
          );
        })}
      </ul>
      {!reauthenticated ? (
        <>
          {passwordEnabled ? (
            <form className="connect-form" onSubmit={reauthenticateWithPassword}>
              <label>
                <span>{labels.currentPassword}</span>
                <input
                  id="linked-login-current-password"
                  type="password"
                  autoComplete="current-password"
                  value={currentPassword}
                  disabled={busy}
                  required
                  onChange={(event) => setCurrentPassword(event.target.value)}
                />
              </label>
              <button type="submit" className="button outline" disabled={busy}>
                {labels.reauthenticate}
              </button>
            </form>
          ) : null}
          {methods.map((method) => {
            const name = providerNames.get(method.providerId) ?? method.providerId;
            return (
              <button
                key={`reauth-${method.id}`}
                type="button"
                className="button outline"
                disabled={busy}
                onClick={() => redirect(method.providerId, "reauth")}
              >
                {labels.reauthenticateWith(name)}
              </button>
            );
          })}
        </>
      ) : (
        <>
          <p role="status">{labels.reauthenticated}</p>
          {providers
            .filter(({ id }) => !linkedProviders.has(id))
            .map((provider) => (
              <button
                key={provider.id}
                type="button"
                className="button outline"
                disabled={busy}
                onClick={() => redirect(provider.id, "link")}
              >
                {labels.link(provider.displayName)}
              </button>
            ))}
          {!passwordEnabled ? (
            <form className="connect-form" onSubmit={enablePassword}>
              <label>
                <span>{labels.newPassword}</span>
                <input
                  id="linked-login-new-password"
                  type="password"
                  autoComplete="new-password"
                  minLength={15}
                  maxLength={128}
                  value={newPassword}
                  disabled={busy}
                  required
                  onChange={(event) => setNewPassword(event.target.value)}
                />
              </label>
              <label>
                <span>{labels.confirmPassword}</span>
                <input
                  id="linked-login-confirm-password"
                  type="password"
                  autoComplete="new-password"
                  minLength={15}
                  maxLength={128}
                  value={confirmation}
                  disabled={busy}
                  required
                  onChange={(event) => setConfirmation(event.target.value)}
                />
              </label>
              <p>{labels.passwordHelp}</p>
              <button type="submit" className="button primary" disabled={busy}>
                {labels.enablePassword}
              </button>
            </form>
          ) : null}
        </>
      )}
      {notice ? <p role="status">{notice}</p> : null}
      {error ? (
        <div className="error-banner" role="alert">
          {error}
        </div>
      ) : null}
    </section>
  );
}
