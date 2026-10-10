import type { Browser } from "@e2e-dev/web";
import type { Screen } from "e2e";

// The account menu is a <details>; clicking its summary toggles it, so only click when it is closed.
export async function openAccountMenu(browser: Browser) {
  const open = await browser.evaluate(
    () => document.querySelector("details.profile-menu")?.hasAttribute("open") ?? false,
  );
  if (!open) await browser.locator("details.profile-menu summary").tap();
}

export async function openEmailDomains(browser: Browser, screen: Screen) {
  await openAccountMenu(browser);
  await screen.getByRole("button", "Allowed email domains").tap();
}
