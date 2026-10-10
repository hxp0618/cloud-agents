import { useEffect, useRef, useState, type FormEvent } from "react";
import {
  ClientError,
  type BrowserSessionClient,
  type BrowserSession,
} from "@cloud-agents/cloud-agent-platform-sdk/platform";
import { ProviderLogin } from "./ProviderLogin";

export function InvitationAcceptance({
  code,
  user,
  client,
  loginError,
  restoring,
  onLogin,
  onAccepted,
  onCancel,
}: Readonly<{
  code: string;
  user: BrowserSession["user"] | undefined;
  client: BrowserSessionClient;
  loginError: string;
  restoring: boolean;
  onLogin: (email: string, password: string) => Promise<void>;
  onAccepted: () => Promise<void>;
  onCancel: () => void;
}>) {
  const [existing, setExisting] = useState(false);
  const [email, setEmail] = useState("");
  const [displayName, setDisplayName] = useState("");
  const [password, setPassword] = useState("");
  const [confirmation, setConfirmation] = useState("");
  const [pending, setBusy] = useState(false);
  const busy = pending || restoring;
  const [error, setError] = useState("");
  const request = useRef<AbortController | null>(null);
  useEffect(() => () => request.current?.abort(), []);
  useEffect(() => {
    if (user !== undefined) {
      setPassword("");
      setConfirmation("");
    }
  }, [user]);

  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (busy || request.current !== null) return;
    setError("");
    if (user === undefined && existing) {
      await onLogin(email, password);
      return;
    }
    if (user === undefined && password !== confirmation) {
      setError("Passwords do not match.");
      return;
    }
    const controller = new AbortController();
    request.current = controller;
    setBusy(true);
    try {
      await client.acceptInvitation(
        user === undefined
          ? { invitationCode: code, displayName, password }
          : { invitationCode: code },
        AbortSignal.any([controller.signal, AbortSignal.timeout(15_000)]),
      );
      setPassword("");
      setConfirmation("");
      await onAccepted();
    } catch (cause) {
      if (!controller.signal.aborted)
        setError(
          cause instanceof ClientError &&
            (cause.status === 400 || cause.status === 403 || cause.status === 409)
            ? "This invitation cannot be accepted. It may have expired, been used or revoked, or no longer match your account or the tenant’s policy. Ask the administrator for a new invitation."
            : "The invitation could not be accepted. Try again.",
        );
    } finally {
      if (!controller.signal.aborted) {
        request.current = null;
        setBusy(false);
      }
    }
  }

  return (
    <main className="connect-view">
      <section className="connect-card panel" aria-labelledby="invitation-title">
        <div className="eyebrow">Admin invitation</div>
        <h1 id="invitation-title">Accept your invitation</h1>
        <p className="lede">
          {user
            ? `Accept as ${user.email}. The invitation must match this email address.`
            : existing
              ? "Sign in with the account that matches the invited email address."
              : "Set a password to create your invited account. Already have an account? Sign in below to keep your existing account."}
        </p>
        <form
          className="connect-form"
          onSubmit={(event) => {
            void submit(event);
          }}
        >
          {user === undefined ? (
            <>
              {existing ? (
                <label>
                  <span>Email</span>
                  <input
                    type="email"
                    autoComplete="username"
                    required
                    value={email}
                    disabled={busy}
                    onChange={(event) => setEmail(event.target.value)}
                  />
                </label>
              ) : (
                <label>
                  <span>Display name</span>
                  <input
                    autoComplete="name"
                    required
                    maxLength={160}
                    value={displayName}
                    disabled={busy}
                    onChange={(event) => setDisplayName(event.target.value)}
                  />
                </label>
              )}
              <label>
                <span>{existing ? "Password" : "New password"}</span>
                <input
                  type="password"
                  autoComplete={existing ? "current-password" : "new-password"}
                  required
                  minLength={existing ? undefined : 15}
                  maxLength={128}
                  value={password}
                  disabled={busy}
                  onChange={(event) => setPassword(event.target.value)}
                />
              </label>
              {existing ? null : (
                <>
                  <small>Use 15–128 characters.</small>
                  <label>
                    <span>Confirm password</span>
                    <input
                      type="password"
                      autoComplete="new-password"
                      required
                      minLength={15}
                      maxLength={128}
                      value={confirmation}
                      disabled={busy}
                      onChange={(event) => setConfirmation(event.target.value)}
                    />
                  </label>
                </>
              )}
            </>
          ) : null}
          {error || loginError ? (
            <div className="error-banner" role="alert">
              {error || loginError}
            </div>
          ) : null}
          <button className="button primary" type="submit" disabled={busy}>
            {busy
              ? "Please wait…"
              : user === undefined && existing
                ? "Sign in to accept"
                : "Accept invitation"}
          </button>
          {user === undefined ? (
            <button
              className="button outline"
              type="button"
              disabled={busy}
              onClick={() => {
                setExisting(!existing);
                setPassword("");
                setConfirmation("");
                setError("");
              }}
            >
              {existing ? "Create invited account" : "I already have an account"}
            </button>
          ) : null}
          <button className="button outline" type="button" disabled={busy} onClick={onCancel}>
            Cancel
          </button>
        </form>
        {user === undefined && !existing ? (
          <ProviderLogin
            client={client}
            purpose="invitation"
            invitationCode={code}
            displayName={displayName}
            disabled={busy || displayName.trim() === ""}
          />
        ) : null}
      </section>
    </main>
  );
}
