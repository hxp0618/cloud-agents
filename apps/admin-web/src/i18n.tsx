import { createContext, useContext, useEffect, useMemo, useState, type ReactNode } from "react";

import { enUS, type MessageKey } from "./i18n/en-US";
import { zhCN } from "./i18n/zh-CN";

export type { MessageKey };

export const supportedLocales = ["zh-CN", "en-US"] as const;
export type Locale = (typeof supportedLocales)[number];

const catalogs: Readonly<Record<Locale, Readonly<Record<MessageKey, string>>>> = Object.freeze({
  "zh-CN": Object.freeze(zhCN),
  "en-US": Object.freeze(enUS),
});

export type MessageValues = Readonly<Record<string, string | number>>;
export type Translate = (key: MessageKey, values?: MessageValues) => string;
type LocaleStorage = Pick<Storage, "getItem" | "setItem">;

export function normalizeLocale(value: string): Locale {
  return value === "zh-CN" || value === "en-US" ? value : "en-US";
}

export function readLocale(storage: LocaleStorage, browserLanguages: readonly string[]): Locale {
  try {
    const saved = storage.getItem("cloud-agents-admin-locale");
    if (saved !== null) return normalizeLocale(saved);
  } catch {
    // Browser preference still provides a safe first-visit default.
  }
  return browserLanguages[0]?.toLocaleLowerCase().startsWith("zh") ? "zh-CN" : "en-US";
}

export function writeLocale(storage: LocaleStorage, locale: Locale): void {
  try {
    storage.setItem("cloud-agents-admin-locale", locale);
  } catch {
    // The selected locale still applies while storage is unavailable.
  }
}

export function translate(locale: string, key: MessageKey, values: MessageValues = {}): string {
  const template = catalogs[normalizeLocale(locale)][key] ?? enUS[key] ?? "";
  return template.replace(/\{([A-Za-z][A-Za-z0-9]*)\}/gu, (_match, name: string) =>
    Object.hasOwn(values, name) ? String(values[name]) : "",
  );
}

export function missingMessageKeys(locale: Locale): readonly MessageKey[] {
  return Object.freeze(
    (Object.keys(enUS) as MessageKey[]).filter((key) => catalogs[locale][key] === undefined),
  );
}

type I18nValue = Readonly<{
  locale: Locale;
  setLocale: (locale: Locale) => void;
  t: Translate;
  number: (value: number) => string;
  dateTime: (value: string | undefined) => string;
}>;

const I18nContext = createContext<I18nValue | null>(null);

export function I18nProvider({ children }: Readonly<{ children: ReactNode }>) {
  const [locale, setLocaleState] = useState(() =>
    readLocale(window.localStorage, navigator.languages),
  );

  useEffect(() => {
    document.documentElement.lang = locale;
    document.title = translate(locale, "document.title");
    writeLocale(window.localStorage, locale);
  }, [locale]);

  const value = useMemo<I18nValue>(() => {
    const t: Translate = (key, values) => translate(locale, key, values);
    const numberFormat = new Intl.NumberFormat(locale);
    const dateTimeFormat = new Intl.DateTimeFormat(locale, {
      dateStyle: "medium",
      timeStyle: "short",
    });
    return {
      locale,
      setLocale: setLocaleState,
      t,
      number: (number) => numberFormat.format(number),
      dateTime: (raw) => {
        if (raw === undefined || raw === "") return t("common.never");
        const parsed = new Date(raw);
        return Number.isNaN(parsed.valueOf()) ? raw : dateTimeFormat.format(parsed);
      },
    };
  }, [locale]);

  return <I18nContext.Provider value={value}>{children}</I18nContext.Provider>;
}

export function useI18n(): I18nValue {
  const value = useContext(I18nContext);
  if (value === null) throw new Error("Admin Web i18n provider is missing");
  return value;
}
