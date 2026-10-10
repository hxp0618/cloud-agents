import { useEffect, useRef, useState } from "react";
import type {
  BrowserSessionClient,
  LoginProvider,
  ProviderAuthorizationRequest,
} from "@cloud-agents/cloud-agent-platform-sdk/platform";

export type ProviderLoginLabels = Readonly<{
  loading: string;
  empty: string;
  start: (provider: string) => string;
  failed: string;
}>;

const defaultLabels: ProviderLoginLabels = Object.freeze({
  loading: "Loading sign-in providers…",
  empty: "No external sign-in providers are available.",
  start: (provider) => `Continue with ${provider}`,
  failed: "External sign-in could not be started. Try again.",
});

export function ProviderLogin({
  client,
  purpose = "login",
  invitationCode,
  displayName,
  disabled = false,
  labels = defaultLabels,
}: Readonly<{
  client: BrowserSessionClient;
  purpose?: Extract<ProviderAuthorizationRequest["purpose"], "login" | "invitation">;
  invitationCode?: string;
  displayName?: string;
  disabled?: boolean;
  labels?: ProviderLoginLabels;
}>) {
  const [providers, setProviders] = useState<readonly LoginProvider[]>([]);
  const [loading, setLoading] = useState(true);
  const [activeProvider, setActiveProvider] = useState("");
  const [error, setError] = useState("");
  const request = useRef<AbortController | null>(null);
  const pending = useRef(false);
  const invitationReady =
    purpose !== "invitation" ||
    (invitationCode !== undefined &&
      invitationCode !== "" &&
      displayName !== undefined &&
      displayName.trim() !== "");

  useEffect(() => {
    const controller = new AbortController();
    request.current = controller;
    const signal = AbortSignal.any([controller.signal, AbortSignal.timeout(15_000)]);
    setLoading(true);
    setError("");
    void client
      .listLoginProviders(signal)
      .then((page) => {
        if (!signal.aborted) setProviders(page.providers);
      })
      .catch(() => {
        if (!controller.signal.aborted) setError(labels.failed);
      })
      .finally(() => {
        if (!controller.signal.aborted) setLoading(false);
      });
    return () => request.current?.abort();
  }, [client, labels.failed]);

  async function start(provider: LoginProvider) {
    if (disabled || loading || pending.current || !invitationReady) return;
    pending.current = true;
    request.current?.abort();
    const controller = new AbortController();
    request.current = controller;
    const signal = AbortSignal.any([controller.signal, AbortSignal.timeout(15_000)]);
    setActiveProvider(provider.id);
    setError("");
    try {
      const body: ProviderAuthorizationRequest =
        purpose === "invitation"
          ? {
              providerId: provider.id,
              purpose,
              invitationCode: invitationCode ?? "",
              displayName: displayName ?? "",
            }
          : { providerId: provider.id, purpose };
      const authorization = await client.startProviderAuthorization(body, signal);
      if (!signal.aborted) window.location.assign(authorization.authorizationUrl);
    } catch {
      if (!controller.signal.aborted) setError(labels.failed);
    } finally {
      pending.current = false;
      if (!controller.signal.aborted) setActiveProvider("");
    }
  }

  return (
    <section className="provider-login" aria-busy={loading || activeProvider !== ""}>
      {loading ? <p role="status">{labels.loading}</p> : null}
      {!loading && providers.length === 0 && error === "" ? <p>{labels.empty}</p> : null}
      {providers.map((provider) => (
        <button
          key={provider.id}
          type="button"
          className="button outline"
          disabled={disabled || loading || activeProvider !== "" || !invitationReady}
          onClick={() => void start(provider)}
        >
          {labels.start(provider.displayName)}
        </button>
      ))}
      {error ? (
        <div className="error-banner" role="alert">
          {error}
        </div>
      ) : null}
    </section>
  );
}
