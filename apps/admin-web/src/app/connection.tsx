import type { FormEvent } from "react";
import type { SavedAdminConnection } from "../admin";
import { normalizeLocale, useI18n, type MessageKey, type Translate } from "../i18n";
import type { ConnectionStatus } from "./presentation";

function statusLabel(status: ConnectionStatus, t: Translate): string {
  if (status === "connected") return t("connection.connected");
  if (status === "connecting") return t("connection.authorizing");
  if (status === "error") return t("connection.failed");
  return t("connection.disconnected");
}

export function ConnectionView({
  connection,
  token,
  status,
  error,
  theme,
  onConnectionChange,
  onTokenChange,
  onThemeToggle,
  onConnect,
}: {
  connection: SavedAdminConnection;
  token: string;
  status: ConnectionStatus;
  error: Readonly<{ key: MessageKey }> | null;
  theme: "light" | "dark";
  onConnectionChange: (field: keyof SavedAdminConnection, value: string) => void;
  onTokenChange: (value: string) => void;
  onThemeToggle: () => void;
  onConnect: (event: FormEvent<HTMLFormElement>) => void;
}) {
  const { locale, setLocale, t } = useI18n();

  return (
    <main className="connect-view">
      <div className="connect-preferences">
        <select
          value={locale}
          aria-label={t("account.language")}
          onChange={(event) => setLocale(normalizeLocale(event.target.value))}
        >
          <option value="zh-CN">{t("locale.zhCN")}</option>
          <option value="en-US">{t("locale.enUS")}</option>
        </select>
        <button className="button outline" type="button" onClick={onThemeToggle}>
          {t(theme === "dark" ? "action.lightMode" : "action.darkMode")}
        </button>
      </div>
      <section className="connect-card" aria-labelledby="connect-title">
        <div className="brand-lockup">
          <span className="brand-mark" aria-hidden="true">
            CA
          </span>
          <span>
            <strong>Cloud Agents</strong>
            <small>{t("brand.adminConsole")}</small>
          </span>
        </div>
        <div className="eyebrow">{t("connection.context")}</div>
        <h1 id="connect-title">{t("connection.title")}</h1>
        <p className="lede">{t("connection.description")}</p>
        <form className="connect-form" onSubmit={onConnect}>
          <label>
            <span>{t("connection.endpoint")}</span>
            <input
              type="url"
              value={connection.endpoint}
              onChange={(event) => onConnectionChange("endpoint", event.target.value)}
              placeholder="https://agents.example.com"
              autoComplete="url"
              required
              disabled={status === "connecting"}
            />
            <small>{t("connection.endpointHelp")}</small>
          </label>
          <div className="form-row">
            <label>
              <span>{t("connection.tenantId")}</span>
              <input
                value={connection.tenantId}
                onChange={(event) => onConnectionChange("tenantId", event.target.value)}
                placeholder="tenant-local"
                autoComplete="off"
                spellCheck={false}
                required
              />
            </label>
            <label>
              <span>{t("connection.projectId")}</span>
              <input
                value={connection.projectId}
                onChange={(event) => onConnectionChange("projectId", event.target.value)}
                placeholder="project-local"
                autoComplete="off"
                spellCheck={false}
                required
              />
            </label>
          </div>
          <label>
            <span>{t("connection.token")}</span>
            <input
              type="password"
              value={token}
              onChange={(event) => onTokenChange(event.target.value)}
              placeholder={t("connection.tokenPlaceholder")}
              autoComplete="off"
              spellCheck={false}
              required
              disabled={status === "connecting"}
            />
            <small>{t("connection.tokenHelp")}</small>
          </label>
          {error !== null ? (
            <p className="banner danger" role="alert">
              {t(error.key)}
            </p>
          ) : null}
          <button className="button primary wide" type="submit" disabled={status === "connecting"}>
            {t(status === "connecting" ? "connection.authorizingProgress" : "connection.connect")}
          </button>
        </form>
        <div className={`connection-state state-${status}`} role="status" aria-live="polite">
          <span className="status-dot" aria-hidden="true" />
          {statusLabel(status, t)}
        </div>
      </section>
    </main>
  );
}
