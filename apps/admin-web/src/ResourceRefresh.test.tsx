import { renderToStaticMarkup } from "react-dom/server";
import { expect, test } from "vitest";
import { ResourceRefresh } from "./ResourceRefresh";

test("pending refresh hides, but retains, the last authority; settled refresh reveals it", () => {
  for (const loading of [true, false]) {
    const html = renderToStaticMarkup(
      <ResourceRefresh loading={loading} label="Deployment Targets" columns={["Name", "Status"]}>
        <button>Last successful Target</button>
      </ResourceRefresh>,
    );
    expect(html).toContain(`aria-busy="${loading}"`);
    expect(html).toContain("<button>Last successful Target</button>");
    expect(html.includes('hidden=""')).toBe(loading);
    expect(html.includes('aria-hidden="true"')).toBe(loading);
    expect(html.match(/class="skeleton"/g)?.length ?? 0).toBe(loading ? 12 : 0);
  }
});
