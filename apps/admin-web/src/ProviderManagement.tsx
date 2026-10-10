import { useEffect, useRef, useState, type FormEvent } from "react";
import type {
  BrowserSession,
  BrowserSessionClient,
  IdentityApplication,
  LoginProvider,
  ProviderClient,
  ProviderClientUpdate,
} from "@cloud-agents/cloud-agent-platform-sdk/platform";

import { AdminSheet, SheetHeading } from "./AdminSheet";
import { adminErrorKey, identifierFromName, uniqueIdentifier } from "./admin";
import { useI18n } from "./i18n";

type ProviderKind = LoginProvider["kind"];
type Draft = Readonly<{
  providerId: string;
  application: IdentityApplication;
  displayName: string;
  providerKind: ProviderKind;
  issuer: string;
  clientId: string;
  userOrigin: string;
  secretRef: string;
  rootCaRef: string;
  agentId: string;
  scopes: string;
  trustProviderEmail: boolean;
  allowedOrganizationIds: string;
  enabled: boolean;
  expectedResourceVersion: string;
}>;

const kinds: readonly ProviderKind[] = Object.freeze([
  "oidc",
  "github",
  "gitlab",
  "feishu",
  "dingtalk",
  "wecom",
]);
const defaultScopes = "openid\nprofile\nemail";
const fixedScopes: Readonly<Partial<Record<ProviderKind, readonly string[]>>> = Object.freeze({
  github: Object.freeze(["read:user", "user:email"]),
  feishu: Object.freeze(["contact:user.email:readonly"]),
  dingtalk: Object.freeze(["corpid", "openid"]),
  wecom: Object.freeze([]),
});

function uniqueLines(value: string): readonly string[] {
  return Object.freeze(
    [
      ...new Set(
        value
          .split(/[\n,]/u)
          .map((entry) => entry.trim())
          .filter(Boolean),
      ),
    ].sort(),
  );
}

function userOriginFromCallback(value: string): string {
  try {
    return new URL(value).origin;
  } catch {
    return "";
  }
}

function callbackFor(application: IdentityApplication, userOrigin: string): string {
  if (application === "admin") return `${window.location.origin}/auth/provider/callback`;
  const origin = new URL(userOrigin);
  if (
    origin.protocol !== "https:" ||
    origin.username !== "" ||
    origin.password !== "" ||
    origin.pathname !== "/" ||
    origin.search !== "" ||
    origin.hash !== ""
  )
    throw new TypeError("invalid User Web origin");
  return `${origin.origin}/auth/provider/callback`;
}

function blankDraft(): Draft {
  return Object.freeze({
    providerId: "",
    application: "admin",
    displayName: "",
    providerKind: "oidc",
    issuer: "",
    clientId: "",
    userOrigin: "",
    secretRef: "",
    rootCaRef: "",
    agentId: "",
    scopes: defaultScopes,
    trustProviderEmail: false,
    allowedOrganizationIds: "",
    enabled: true,
    expectedResourceVersion: "0",
  });
}

function draftFrom(provider: ProviderClient): Draft {
  return Object.freeze({
    providerId: provider.providerId,
    application: provider.application,
    displayName: provider.displayName,
    providerKind: provider.providerKind,
    issuer: provider.issuer,
    clientId: provider.clientId,
    userOrigin: provider.application === "user" ? userOriginFromCallback(provider.redirectUri) : "",
    secretRef: provider.secretRef,
    rootCaRef: provider.rootCaRef ?? "",
    agentId: provider.agentId ?? "",
    scopes: (fixedScopes[provider.providerKind] ?? provider.scopes).join("\n"),
    trustProviderEmail: provider.trustProviderEmail,
    allowedOrganizationIds: provider.allowedOrganizationIds.join("\n"),
    enabled: provider.enabled,
    expectedResourceVersion: provider.resourceVersion,
  });
}

export function ProviderManagement({
  session,
  sessionClient,
}: Readonly<{
  session: BrowserSession;
  sessionClient: BrowserSessionClient;
}>) {
  const { t } = useI18n();
  const [trigger, setTrigger] = useState<HTMLElement | null>(null);
  if (!session.user.displayRoles.includes("platform.admin")) return null;
  return (
    <>
      <button type="button" onClick={(event) => setTrigger(event.currentTarget)}>
        {t("identity.providers")}
      </button>
      {trigger === null ? null : (
        <ProviderSheet
          sessionClient={sessionClient}
          trigger={trigger}
          onClose={() => setTrigger(null)}
        />
      )}
    </>
  );
}

function ProviderSheet({
  sessionClient,
  trigger,
  onClose,
}: Readonly<{
  sessionClient: BrowserSessionClient;
  trigger: HTMLElement;
  onClose: () => void;
}>) {
  const { t } = useI18n();
  const [providers, setProviders] = useState<readonly ProviderClient[]>([]);
  const [draft, setDraft] = useState<Draft>(blankDraft);
  const [creating, setCreating] = useState(true);
  const [busy, setBusy] = useState(true);
  const [error, setError] = useState("");
  const [notice, setNotice] = useState("");
  const request = useRef<AbortController | null>(null);
  const pending = useRef(false);

  async function load(
    signal: AbortSignal,
    selected?: Readonly<{ id: string; app: IdentityApplication }>,
  ) {
    const page = await sessionClient.listProviderClients(signal);
    if (signal.aborted) return;
    setProviders(page.providers);
    if (selected !== undefined) {
      const current = page.providers.find(
        ({ providerId, application }) => providerId === selected.id && application === selected.app,
      );
      if (current !== undefined) {
        setDraft(draftFrom(current));
        setCreating(false);
      } else {
        setDraft(blankDraft());
        setCreating(true);
      }
    }
  }

  useEffect(() => {
    const controller = new AbortController();
    request.current = controller;
    const signal = AbortSignal.any([controller.signal, AbortSignal.timeout(15_000)]);
    void load(signal)
      .catch((cause) => {
        if (!controller.signal.aborted) setError(t(adminErrorKey(cause)));
      })
      .finally(() => {
        if (!controller.signal.aborted) setBusy(false);
      });
    return () => controller.abort();
  }, [sessionClient, t]);

  function update<K extends keyof Draft>(key: K, value: Draft[K]) {
    setDraft((current) => Object.freeze({ ...current, [key]: value }));
  }

  function select(value: string) {
    if (value === "new") {
      setCreating(true);
      setDraft(blankDraft());
      return;
    }
    const [providerId, application] = value.split("\u0000") as [string, IdentityApplication];
    const provider = providers.find(
      (candidate) => candidate.providerId === providerId && candidate.application === application,
    );
    if (provider !== undefined) {
      setCreating(false);
      setDraft(draftFrom(provider));
    }
  }

  function save(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (busy || pending.current) return;
    pending.current = true;
    request.current?.abort();
    const controller = new AbortController();
    request.current = controller;
    const signal = AbortSignal.any([controller.signal, AbortSignal.timeout(15_000)]);
    setBusy(true);
    setError("");
    setNotice("");
    const providerId = creating
      ? uniqueIdentifier(
          identifierFromName(draft.displayName, draft.providerKind),
          new Set(
            providers
              .filter((provider) => provider.application === draft.application)
              .map((provider) => provider.providerId),
          ),
        )
      : draft.providerId;
    const selected = { id: providerId, app: draft.application } as const;
    void (async () => {
      try {
        const body: ProviderClientUpdate = {
          displayName: draft.displayName,
          providerKind: draft.providerKind,
          issuer: draft.issuer,
          clientId: draft.clientId,
          redirectUri: callbackFor(draft.application, draft.userOrigin),
          secretRef: draft.secretRef,
          ...(draft.rootCaRef === "" ? {} : { rootCaRef: draft.rootCaRef }),
          ...(draft.providerKind === "wecom" ? { agentId: draft.agentId } : {}),
          scopes: fixedScopes[draft.providerKind] ?? uniqueLines(draft.scopes),
          trustProviderEmail: draft.trustProviderEmail,
          allowedOrganizationIds: uniqueLines(draft.allowedOrganizationIds),
          enabled: draft.enabled,
          expectedResourceVersion: draft.expectedResourceVersion,
        };
        const saved = await sessionClient.updateProviderClient(
          providerId,
          draft.application,
          body,
          signal,
        );
        if (signal.aborted) return;
        setDraft(draftFrom(saved));
        setCreating(false);
        setNotice(t("identity.providerSaved"));
        await load(signal, selected);
      } catch (cause) {
        if (!controller.signal.aborted) {
          setError(t(adminErrorKey(cause)));
          const refresh = AbortSignal.timeout(15_000);
          await load(refresh, selected).catch(() => undefined);
        }
      } finally {
        pending.current = false;
        if (!controller.signal.aborted) setBusy(false);
      }
    })();
  }

  const callback = (() => {
    try {
      return callbackFor(draft.application, draft.userOrigin);
    } catch {
      return "";
    }
  })();

  return (
    <AdminSheet
      label={t("identity.providers")}
      returnFocus={trigger}
      onClose={onClose}
      feedback={null}
    >
      <section className="dialog" aria-labelledby="providers-title">
<SheetHeading id="providers-title" title={t("identity.providers")} disabled={busy} onClose={onClose} />
        <label>
          <span>{t("identity.providerConfiguration")}</span>
          <select
            value={creating ? "new" : `${draft.providerId}\u0000${draft.application}`}
            disabled={busy}
            onChange={(event) => select(event.target.value)}
          >
            <option value="new">{t("identity.providerNew")}</option>
            {providers.map((provider) => (
              <option
                key={`${provider.providerId}\u0000${provider.application}`}
                value={`${provider.providerId}\u0000${provider.application}`}
              >
                {provider.displayName} · {provider.application}
              </option>
            ))}
          </select>
        </label>
        <form className="resource-form" onSubmit={save} aria-busy={busy}>
          <label>
            <span>{t("identity.providerApplication")}</span>
            <select
              value={draft.application}
              disabled={!creating || busy}
              onChange={(event) =>
                update("application", event.target.value as IdentityApplication)
              }
            >
              <option value="admin">{t("identity.serviceAccountApplication.admin")}</option>
              <option value="user">{t("identity.serviceAccountApplication.user")}</option>
            </select>
          </label>
          <div className="form-row">
            <label>
              <span>{t("identity.providerDisplayName")}</span>
              <input
                value={draft.displayName}
                required
                maxLength={160}
                onChange={(event) => update("displayName", event.target.value)}
              />
            </label>
            <label>
              <span>{t("identity.providerKind")}</span>
              <select
                value={draft.providerKind}
                disabled={busy}
                onChange={(event) => {
                  const providerKind = event.target.value as ProviderKind;
                  const scopes = fixedScopes[providerKind];
                  setDraft((current) =>
                    Object.freeze({
                      ...current,
                      providerKind,
                      scopes:
                        scopes !== undefined
                          ? scopes.join("\n")
                          : fixedScopes[current.providerKind] !== undefined
                            ? defaultScopes
                            : current.scopes,
                    }),
                  );
                }}
              >
                {kinds.map((kind) => (
                  <option key={kind} value={kind}>
                    {kind}
                  </option>
                ))}
              </select>
            </label>
          </div>
          <label>
            <span>{t("identity.providerIssuer")}</span>
            <input
              type="url"
              value={draft.issuer}
              required
              maxLength={512}
              placeholder="https://id.example/realms/cloud-agents"
              onChange={(event) => update("issuer", event.target.value)}
            />
          </label>
          <label>
            <span>{t("identity.providerClientId")}</span>
            <input
              value={draft.clientId}
              required
              maxLength={512}
              onChange={(event) => update("clientId", event.target.value)}
            />
          </label>
          {draft.application === "user" ? (
            <label>
              <span>{t("identity.providerUserOrigin")}</span>
              <input
                type="url"
                value={draft.userOrigin}
                required
                placeholder="https://user.example"
                onChange={(event) => update("userOrigin", event.target.value)}
              />
            </label>
          ) : null}
          <label>
            <span>{t("identity.providerCallback")}</span>
            <input value={callback} readOnly aria-readonly="true" />
          </label>
          <label>
            <span>{t("identity.providerSecretRef")}</span>
            <input
              aria-label={t("identity.providerSecretRef")}
              value={draft.secretRef}
              required
              maxLength={128}
              onChange={(event) => update("secretRef", event.target.value)}
            />
            <small>{t("identity.providerSecretRefHelp")}</small>
          </label>
          <label>
            <span>{t("identity.providerRootCaRef")}</span>
            <input
              aria-label={t("identity.providerRootCaRef")}
              value={draft.rootCaRef}
              maxLength={128}
              onChange={(event) => update("rootCaRef", event.target.value)}
            />
          </label>
          {draft.providerKind === "wecom" ? (
            <label>
              <span>{t("identity.providerAgentId")}</span>
              <input
                value={draft.agentId}
                required
                maxLength={255}
                onChange={(event) => update("agentId", event.target.value)}
              />
            </label>
          ) : null}
          <label>
            <span>{t("identity.providerScopes")}</span>
            <textarea
              value={draft.scopes}
              rows={4}
              readOnly={fixedScopes[draft.providerKind] !== undefined}
              onChange={(event) => update("scopes", event.target.value)}
            />
            <small>{t("identity.providerListHelp")}</small>
          </label>
          <label className="confirmation-check">
            <input
              type="checkbox"
              checked={draft.trustProviderEmail}
              onChange={(event) => update("trustProviderEmail", event.target.checked)}
            />
            <span>{t("identity.providerTrustEmail")}</span>
          </label>
          <small>{t("identity.providerTrustEmailHelp")}</small>
          <label>
            <span>{t("identity.providerOrganizations")}</span>
            <textarea
              value={draft.allowedOrganizationIds}
              rows={3}
              required={draft.trustProviderEmail}
              onChange={(event) => update("allowedOrganizationIds", event.target.value)}
            />
            <small>{t("identity.providerListHelp")}</small>
          </label>
          <label className="confirmation-check">
            <input
              type="checkbox"
              checked={draft.enabled}
              onChange={(event) => update("enabled", event.target.checked)}
            />
            <span>{t("identity.providerEnabled")}</span>
          </label>
          {notice ? <p role="status">{notice}</p> : null}
          {error ? (
            <div className="error-banner" role="alert">
              {error}
            </div>
          ) : null}
          <button type="submit" className="button primary" disabled={busy || callback === ""}>
            {t("identity.providerSave")}
          </button>
        </form>
      </section>
    </AdminSheet>
  );
}
