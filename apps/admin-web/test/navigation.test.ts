import { renderToStaticMarkup } from "react-dom/server";
import { createElement } from "react";
import { describe, expect, it, vi } from "vitest";
import { matchingNavigation, ResourceNavigation } from "../src/navigation";
import { I18nProvider, translate, type Translate } from "../src/i18n";

describe("admin navigation commands", () => {
  const english: Translate = (key, values) => translate("en-US", key, values);
  const chinese: Translate = (key, values) => translate("zh-CN", key, values);

  it("matches localized labels and all query words without storing or interpreting input", () => {
    expect(matchingNavigation("targets", "deployment target", english)).toEqual([]);
    expect(
      matchingNavigation("overview", " deployment  TARGET ", english).map(({ id }) => id),
    ).toEqual(["targets"]);
    expect(matchingNavigation("overview", "网络", chinese).map(({ id }) => id)).toEqual([
      "network",
    ]);
    expect(matchingNavigation("overview", "go to storage", english).map(({ id }) => id)).toEqual([
      "storage",
    ]);
    expect(matchingNavigation("overview", "<script>credentialRef</script>", english)).toEqual([]);
    expect(matchingNavigation("overview", "does-not-exist", chinese)).toEqual([]);
  });

  it("disables navigation while an operation owns the authority request", () => {
    vi.stubGlobal("navigator", { platform: "MacIntel", languages: ["en-US"] });
    vi.stubGlobal("window", { localStorage: { getItem: () => "en-US" } });
    const html = renderToStaticMarkup(
      createElement(
        I18nProvider,
        null,
        createElement(ResourceNavigation, {
          page: "overview",
          counts: {},
          disabled: true,
          onNavigate: () => undefined,
          onSearch: () => undefined,
        }),
      ),
    );
    expect(html.match(/disabled=""/g)).toHaveLength(15);
    vi.unstubAllGlobals();
  });
});
