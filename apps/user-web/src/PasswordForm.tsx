import { useEffect, useRef, useState, type FormEvent } from "react";
import {
  ClientError,
  type BrowserSessionClient,
} from "@cloud-agents/cloud-agent-platform-sdk/platform";

export const passwordLabels = {
  current: "Current password",
  next: "New password",
  confirm: "Confirm password",
  help: "Use 15–128 characters. Changing your password signs out all active sessions.",
  mismatch: "Passwords do not match.",
  invalid: "Use a password of 15–128 characters.",
  rejected: "The password or reset link was rejected. Check it and try again.",
  failed: "The password could not be changed. Try again.",
  locked: "Too many attempts. Wait 15 minutes before trying again.",
  save: "Save password",
  saving: "Saving…",
};

export function PasswordForm({
  client,
  resetCode,
  labels = passwordLabels,
  onComplete,
}: Readonly<{
  client: BrowserSessionClient;
  resetCode?: string;
  labels?: Readonly<Record<keyof typeof passwordLabels, string>>;
  onComplete: () => void;
}>) {
  const [currentPassword, setCurrentPassword] = useState("");
  const [newPassword, setNewPassword] = useState("");
  const [confirmation, setConfirmation] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const request = useRef<AbortController | null>(null);
  useEffect(() => () => request.current?.abort(), []);

  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (request.current !== null) return;
    if (newPassword !== confirmation) {
      setError(labels.mismatch);
      return;
    }
    if ([...newPassword].length < 15 || [...newPassword].length > 128) {
      setError(labels.invalid);
      return;
    }
    const controller = new AbortController();
    request.current = controller;
    setBusy(true);
    setError("");
    const signal = AbortSignal.any([controller.signal, AbortSignal.timeout(15_000)]);
    try {
      if (resetCode === undefined)
        await client.changePassword({ currentPassword, newPassword }, signal);
      else await client.acceptPasswordReset({ resetCode, newPassword }, signal);
      if (!signal.aborted) {
        setCurrentPassword("");
        setNewPassword("");
        setConfirmation("");
        onComplete();
      }
    } catch (cause) {
      if (!controller.signal.aborted)
        setError(
          cause instanceof ClientError && cause.status === 429
            ? labels.locked
            : cause instanceof ClientError && [400, 401, 403, 409].includes(cause.status)
              ? labels.rejected
              : labels.failed,
        );
    } finally {
      if (!controller.signal.aborted) {
        request.current = null;
        setBusy(false);
      }
    }
  }

  return (
    <form
      className="connect-form resource-form"
      onSubmit={(event) => {
        void submit(event);
      }}
    >
      {resetCode === undefined ? (
        <label>
          <span>{labels.current}</span>
          <input
            type="password"
            autoComplete="current-password"
            required
            disabled={busy}
            value={currentPassword}
            onChange={(event) => setCurrentPassword(event.target.value)}
          />
        </label>
      ) : null}
      <label>
        <span>{labels.next}</span>
        <input
          type="password"
          autoComplete="new-password"
          required
          disabled={busy}
          value={newPassword}
          onChange={(event) => setNewPassword(event.target.value)}
        />
      </label>
      <label>
        <span>{labels.confirm}</span>
        <input
          type="password"
          autoComplete="new-password"
          required
          disabled={busy}
          value={confirmation}
          onChange={(event) => setConfirmation(event.target.value)}
        />
      </label>
      <small>{labels.help}</small>
      {error ? (
        <div role="alert" className="error-banner">
          {error}
        </div>
      ) : null}
      <button type="submit" className="button primary" disabled={busy}>
        {busy ? labels.saving : labels.save}
      </button>
    </form>
  );
}
