import { useEffect, useRef, useState, type FormEvent } from "react";
import {
  type BrowserSessionClient,
  type EmailSuffixPolicy,
} from "@cloud-agents/cloud-agent-platform-sdk/platform";
import { parseEmailDomains } from "./app/auth";
import { adminErrorKey } from "./admin";
import { useI18n, type MessageKey } from "./i18n";

export function EmailPolicyForm({
  client,
  tenantId,
}: Readonly<{ client: BrowserSessionClient; tenantId: string }>) {
  const { t } = useI18n();
  const [policy, setPolicy] = useState<EmailSuffixPolicy | null>(null);
  const [domains, setDomains] = useState("");
  const [busy, setBusy] = useState(true);
  const [error, setError] = useState<MessageKey | null>(null);
  const lifetime = useRef<AbortController | null>(null);
  const pending = useRef(false);

  function apply(next: EmailSuffixPolicy) {
    setPolicy(next);
    setDomains(next.allowedDomains.join("\n"));
  }

  useEffect(() => {
    const controller = new AbortController();
    lifetime.current = controller;
    setPolicy(null);
    setDomains("");
    setBusy(true);
    setError(null);
    const signal = AbortSignal.any([controller.signal, AbortSignal.timeout(15_000)]);
    void client
      .getEmailSuffixPolicy(tenantId, signal)
      .then((next) => {
        if (!signal.aborted) apply(next);
      })
      .catch((cause) => {
        if (!controller.signal.aborted) setError(adminErrorKey(cause));
      })
      .finally(() => {
        if (!controller.signal.aborted) setBusy(false);
      });
    return () => controller.abort();
  }, [client, tenantId]);

  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    const controller = lifetime.current;
    if (controller === null || policy === null || busy || pending.current) return;
    const parsed = parseEmailDomains(domains);
    if (parsed === null) {
      setError("auth.emailDomainsInvalid");
      return;
    }
    pending.current = true;
    setBusy(true);
    setError(null);
    try {
      const signal = AbortSignal.any([controller.signal, AbortSignal.timeout(15_000)]);
      const next = await client.updateEmailSuffixPolicy(
        tenantId,
        { allowedDomains: parsed, expectedResourceVersion: policy.resourceVersion },
        signal,
      );
      if (!signal.aborted) apply(next);
    } catch (cause) {
      if (!controller.signal.aborted) setError(adminErrorKey(cause));
    } finally {
      pending.current = false;
      if (!controller.signal.aborted) setBusy(false);
    }
  }

  async function refresh() {
    const controller = lifetime.current;
    if (controller === null || busy || pending.current) return;
    pending.current = true;
    setBusy(true);
    setError(null);
    try {
      const signal = AbortSignal.any([controller.signal, AbortSignal.timeout(15_000)]);
      const next = await client.getEmailSuffixPolicy(tenantId, signal);
      if (!signal.aborted) apply(next);
    } catch (cause) {
      if (!controller.signal.aborted) setError(adminErrorKey(cause));
    } finally {
      pending.current = false;
      if (!controller.signal.aborted) setBusy(false);
    }
  }

  return (
    <form
      className="email-domain-form"
      onSubmit={(event) => {
        void submit(event);
      }}
    >
      <label>
        <span>{t("auth.emailDomains")}</span>
        <textarea
          value={domains}
          disabled={busy || policy === null}
          placeholder={t("auth.emailDomainsPlaceholder")}
          onChange={(event) => setDomains(event.target.value)}
        />
        <small>{t("auth.emailDomainsHelp")}</small>
      </label>
      {error !== null ? (
        <div role="alert" className="error-banner">
          {t(error)}
        </div>
      ) : null}
      <button type="submit" disabled={busy || policy === null}>
        {t("auth.saveEmailDomains")}
      </button>
      <button
        type="button"
        disabled={busy}
        onClick={() => {
          void refresh();
        }}
      >
        {t("auth.reloadEmailDomains")}
      </button>
    </form>
  );
}
