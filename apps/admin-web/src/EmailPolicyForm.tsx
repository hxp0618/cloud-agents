import { useEffect, useRef, useState, type FormEvent } from "react";
import {
  type BrowserSessionClient,
  type EmailSuffixPolicy,
} from "@cloud-agents/cloud-agent-platform-sdk/platform";
import { AdminSheet, SheetHeading, SheetTrigger } from "./AdminSheet";
import { parseEmailDomains } from "./app/auth";
import { adminErrorKey } from "./admin";
import { useI18n, type MessageKey } from "./i18n";

export function EmailPolicyForm({
  client,
  tenantId,
}: Readonly<{ client: BrowserSessionClient; tenantId: string }>) {
  const { t } = useI18n();
  return (
    <SheetTrigger label={t("auth.emailDomains")}>
      {(trigger, close) => (
        <EmailPolicySheet client={client} tenantId={tenantId} trigger={trigger} onClose={close} />
      )}
    </SheetTrigger>
  );
}

function EmailPolicySheet({
  client,
  tenantId,
  trigger,
  onClose,
}: Readonly<{
  client: BrowserSessionClient;
  tenantId: string;
  trigger: HTMLElement;
  onClose: () => void;
}>) {
  const { t } = useI18n();
  const [policy, setPolicy] = useState<EmailSuffixPolicy | null>(null);
  const [domains, setDomains] = useState("");
  const [busy, setBusy] = useState(true);
  const [error, setError] = useState<MessageKey | null>(null);
  const lifetime = useRef<AbortController | null>(null);
  const pending = useRef(false);

  // A save in flight must finish before the sheet can be dismissed; the initial load may be abandoned.
  const saving = busy && policy !== null;
  function close() {
    if (!pending.current) onClose();
  }

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

  return (
    <AdminSheet
      label={t("auth.emailDomains")}
      returnFocus={trigger}
      onClose={close}
      feedback={null}
    >
      <section className="dialog" aria-labelledby="email-domains-title">
        <SheetHeading
          id="email-domains-title"
          title={t("auth.emailDomains")}
          disabled={saving}
          onClose={close}
        />
        <form
          className="resource-form"
          onSubmit={(event) => {
            void submit(event);
          }}
        >
          <label>
            <span>{t("auth.emailDomains")}</span>
            <textarea
              value={domains}
              rows={6}
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
          <div className="dialog-actions">
            <button className="button ghost" type="button" disabled={saving} onClick={close}>
              {t("action.cancel")}
            </button>
            <button className="button primary" type="submit" disabled={busy || policy === null}>
              {t("auth.saveEmailDomains")}
            </button>
          </div>
        </form>
      </section>
    </AdminSheet>
  );
}
