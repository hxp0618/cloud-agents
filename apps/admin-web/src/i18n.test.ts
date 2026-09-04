import { describe, expect, it, vi } from "vitest";

import {
  missingMessageKeys,
  normalizeLocale,
  readLocale,
  supportedLocales,
  translate,
  writeLocale,
} from "./i18n";

function storage(value: string | null = null) {
  return {
    getItem: vi.fn(() => value),
    setItem: vi.fn(),
  };
}

describe("Admin Web locale", () => {
  it("uses a saved supported locale and falls invalid values back to English", () => {
    expect(readLocale(storage("zh-CN"), ["en-US"])).toBe("zh-CN");
    expect(readLocale(storage("fr-FR"), ["zh-CN"])).toBe("en-US");
    expect(normalizeLocale("invalid")).toBe("en-US");
  });

  it("uses the first browser language only on first visit", () => {
    expect(readLocale(storage(), ["zh-Hans-CN", "en-US"])).toBe("zh-CN");
    expect(readLocale(storage(), ["en-US", "zh-CN"])).toBe("en-US");
  });

  it("persists only a supported locale", () => {
    const target = storage();
    writeLocale(target, "zh-CN");
    expect(target.setItem).toHaveBeenCalledWith("cloud-agents-admin-locale", "zh-CN");
  });

  it("has every message in both catalogs and never exposes a key on fallback", () => {
    for (const locale of supportedLocales) expect(missingMessageKeys(locale)).toEqual([]);
    expect(translate("invalid", "action.refresh")).toBe("Refresh");
    expect(translate("zh-CN", "overview.readyLeases", { count: 2 })).toBe("当前 2 个就绪");
  });
});
