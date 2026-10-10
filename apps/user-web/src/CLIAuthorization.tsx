import { useEffect, useRef, useState, type FormEvent } from "react";
import type {
  BrowserSession,
  BrowserSessionClient,
} from "@cloud-agents/cloud-agent-platform-sdk/platform";

export type CLIAuthorizationLabels = Readonly<{
  title: string;
  description: string;
  loading: string;
  expired: string;
  failed: string;
  approve: string;
  cancel: string;
  back: string;
}>;

const defaultLabels: CLIAuthorizationLabels = {
  title: "Sign in to Cloud Agents CLI",
  description:
    "Approve only if you started this login in your terminal. The CLI will be able to use your current account permissions in this console.",
  loading: "Checking the CLI login request…",
  expired: "This login request has expired. Start login again in your terminal.",
  failed: "The login request could not be checked. Try again.",
  approve: "Approve CLI login",
  cancel: "Cancel",
  back: "Return to console",
};

export function CLIAuthorization({
  client,
  session,
  labels = defaultLabels,
}: Readonly<{
  client: BrowserSessionClient;
  session: BrowserSession;
  labels?: CLIAuthorizationLabels;
}>) {
  const [state, setState] = useState<"loading" | "ready" | "expired" | "failed">("loading");
  const [submitting, setSubmitting] = useState(false);
  const pending = useRef(false);
  useEffect(() => {
    const controller = new AbortController();
    const signal = AbortSignal.any([controller.signal, AbortSignal.timeout(15_000)]);
    void client
      .getCLIAuthorizationRequest(signal)
      .then((request) => {
        if (!signal.aborted)
          setState(
            request.pending && request.application === session.application ? "ready" : "expired",
          );
      })
      .catch(() => {
        if (!controller.signal.aborted) setState("failed");
      });
    return () => controller.abort();
  }, [client, session.application]);

  function submit(event: FormEvent<HTMLFormElement>) {
    if (state !== "ready" || pending.current) {
      event.preventDefault();
      return;
    }
    pending.current = true;
    setSubmitting(true);
  }

  return (
    <main className="connect-view">
      <section
        className="connect-card panel"
        aria-labelledby="cli-authorization-title"
        aria-busy={state === "loading" || submitting}
      >
        <h1 id="cli-authorization-title">{labels.title}</h1>
        <p className="lede">{labels.description}</p>
        <p>
          {session.user.displayName} · {session.user.email}
        </p>
        {state === "ready" ? (
          <>
            <form action="/v1/auth/cli/approve" method="post" onSubmit={submit}>
              <input type="hidden" name="csrfToken" value={session.csrfToken} />
              <button className="button primary" type="submit" disabled={submitting}>
                {labels.approve}
              </button>
            </form>
            <form action="/v1/auth/cli/cancel" method="post" onSubmit={submit}>
              <input type="hidden" name="csrfToken" value={session.csrfToken} />
              <button className="button outline" type="submit" disabled={submitting}>
                {labels.cancel}
              </button>
            </form>
          </>
        ) : (
          <p role={state === "failed" ? "alert" : "status"}>{labels[state]}</p>
        )}
        {state === "expired" || state === "failed" ? <a href="/">{labels.back}</a> : null}
      </section>
    </main>
  );
}
