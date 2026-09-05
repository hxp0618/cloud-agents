import { spawn } from "node:child_process";
import assert from "node:assert/strict";
import { mkdirSync, mkdtempSync, readFileSync, renameSync, writeFileSync } from "node:fs";
import { homedir, tmpdir } from "node:os";
import { join } from "node:path";
import { format } from "oxfmt";

const [output, adminTokenFile, userTokenFile, projectId, tenantId = "tenant-local"] =
  process.argv.slice(2);
if (!output || !adminTokenFile || !userTokenFile || !projectId) {
  throw new Error(
    "usage: node capture-actual.mjs OUTPUT ADMIN_TOKEN_FILE USER_TOKEN_FILE PROJECT_ID [TENANT_ID]",
  );
}

const app = "http://127.0.0.1:4174/";
const browserPath = "/Applications/Brave Browser.app/Contents/MacOS/Brave Browser";
const profile = mkdtempSync(join(tmpdir(), "cloud-agents-admin-cdp-"));
const browser = spawn(
  browserPath,
  [
    "--headless=new",
    "--disable-gpu",
    "--hide-scrollbars",
    "--no-first-run",
    "--no-default-browser-check",
    "--disable-background-networking",
    "--remote-debugging-port=0",
    `--user-data-dir=${profile}`,
    "about:blank",
  ],
  { stdio: "ignore" },
);

mkdirSync(output, { recursive: true });
const delay = (milliseconds) => new Promise((resolve) => setTimeout(resolve, milliseconds));
let socket;

try {
  let port;
  for (let attempt = 0; attempt < 100; attempt += 1) {
    try {
      port = readFileSync(join(profile, "DevToolsActivePort"), "utf8").split("\n")[0];
      break;
    } catch {
      await delay(50);
    }
  }
  if (!port) throw new Error("Brave did not expose DevToolsActivePort");

  const targets = await fetch(`http://127.0.0.1:${port}/json/list`).then((response) =>
    response.json(),
  );
  const page = targets.find((target) => target.type === "page");
  if (!page) throw new Error("No browser page target found");

  socket = new WebSocket(page.webSocketDebuggerUrl);
  await new Promise((resolve, reject) => {
    socket.addEventListener("open", resolve, { once: true });
    socket.addEventListener("error", reject, { once: true });
  });

  let nextId = 1;
  const pending = new Map();
  socket.addEventListener("close", (event) => {
    for (const callback of pending.values()) {
      callback.reject(
        new Error(
          `Browser connection closed (${event.code}); process exit=${browser.exitCode}, signal=${browser.signalCode}`,
        ),
      );
    }
    pending.clear();
  });
  const requestOrigins = new Set();
  const consoleErrors = [];
  const consoleWarnings = [];
  const httpFailures = [];
  const layoutChecks = [];
  const tableHeaderChecks = [];
  const toastChecks = [];
  const formChecks = [];
  const commandChecks = [];
  const modalChecks = [];
  const targetFilterChecks = [];
  const overviewChecks = [];
  const mutationRequests = [];
  let phase = "admin";
  socket.addEventListener("message", ({ data }) => {
    const message = JSON.parse(data);
    if (message.method === "Network.requestWillBeSent") {
      const requestURL = new URL(message.params.request.url);
      if (requestURL.pathname.startsWith("/v1/admin/") && message.params.request.method !== "GET") {
        mutationRequests.push({ method: message.params.request.method, path: requestURL.pathname });
      }
      if (requestURL.protocol === "http:" || requestURL.protocol === "https:") {
        requestOrigins.add(requestURL.origin);
      }
    } else if (
      message.method === "Network.responseReceived" &&
      message.params.response.status >= 400
    ) {
      httpFailures.push({
        phase,
        status: message.params.response.status,
        url: message.params.response.url,
      });
    } else if (message.method === "Runtime.consoleAPICalled") {
      const values = message.params.args
        .map(({ value, description }) => value ?? description)
        .join(" ");
      if (message.params.type === "error") consoleErrors.push(values);
      if (message.params.type === "warning") consoleWarnings.push(values);
    } else if (message.method === "Log.entryAdded") {
      if (message.params.entry.level === "error") consoleErrors.push(message.params.entry.text);
      if (message.params.entry.level === "warning") consoleWarnings.push(message.params.entry.text);
    }
    if (!message.id) return;
    const callback = pending.get(message.id);
    if (!callback) return;
    pending.delete(message.id);
    if (message.error) callback.reject(new Error(message.error.message));
    else callback.resolve(message.result);
  });
  const command = (method, params = {}) =>
    new Promise((resolve, reject) => {
      const id = nextId++;
      pending.set(id, { resolve, reject });
      socket.send(JSON.stringify({ id, method, params }));
    });
  const evaluate = async (expression) => {
    const result = await command("Runtime.evaluate", {
      expression,
      returnByValue: true,
      awaitPromise: true,
    });
    if (result.exceptionDetails) throw new Error(result.exceptionDetails.text);
    return result.result.value;
  };
  const waitFor = async (expression, label) => {
    for (let attempt = 0; attempt < 100; attempt += 1) {
      if (await evaluate(expression)) return;
      await delay(50);
    }
    throw new Error(
      `Timed out waiting for ${label}: ${JSON.stringify(await evaluate("({ focus: {tag: document.activeElement?.tagName, class: document.activeElement?.className}, popovers: [...document.querySelectorAll('[popover]')].map(element => ({class: element.className, open: element.matches(':popover-open')})) })"))}`,
    );
  };
  const click = async (selector) => {
    const clicked = await evaluate(
      `(() => { const element = document.querySelector(${JSON.stringify(selector)}); if (!element) return false; element.click(); return true; })()`,
    );
    if (!clicked) throw new Error(`Missing clickable element: ${selector}`);
  };
  const setViewport = (width, height) =>
    command("Emulation.setDeviceMetricsOverride", {
      width,
      height,
      deviceScaleFactor: 1,
      mobile: width < 600,
    });
  const pressKey = async (key, code = key, modifiers = 0) => {
    const virtualKeyCode =
      { Escape: 27, Tab: 9, Enter: 13, ArrowUp: 38, ArrowRight: 39, ArrowDown: 40 }[key] ??
      key.toUpperCase().charCodeAt(0);
    const params = { key, code, modifiers, windowsVirtualKeyCode: virtualKeyCode };
    await command("Input.dispatchKeyEvent", { type: "rawKeyDown", ...params });
    if (key === "Enter") {
      await command("Input.dispatchKeyEvent", { type: "char", ...params, text: "\r" });
    }
    await command("Input.dispatchKeyEvent", { type: "keyUp", ...params });
  };
  const clickAt = async (selector) => {
    await waitFor(
      `(() => {
      const element = document.querySelector(${JSON.stringify(selector)});
      if (!element || element.matches(":disabled")) return false;
      const rect = element.getBoundingClientRect();
      if (!rect.width || !rect.height) return false;
      const hit = document.elementFromPoint(rect.x + rect.width / 2, rect.y + rect.height / 2);
      return hit === element || element.contains(hit);
    })()`,
      `pointer target ${selector}`,
    );
    const point = JSON.parse(
      await evaluate(
        `(() => { const rect = document.querySelector(${JSON.stringify(selector)})?.getBoundingClientRect(); return JSON.stringify(rect ? { x: rect.x + rect.width / 2, y: rect.y + rect.height / 2 } : null); })()`,
      ),
    );
    if (!point) throw new Error(`Missing element for pointer click: ${selector}`);
    await command("Input.dispatchMouseEvent", {
      type: "mousePressed",
      ...point,
      button: "left",
      clickCount: 1,
    });
    await command("Input.dispatchMouseEvent", {
      type: "mouseReleased",
      ...point,
      button: "left",
      clickCount: 1,
    });
  };
  const screenshot = async (filename) => {
    await command("Input.dispatchMouseEvent", { type: "mouseMoved", x: 0, y: 0 });
    await delay(250);
    const headers = await evaluate(
      `([...document.querySelectorAll('th')].filter(header => header.getBoundingClientRect().width > 0).map(header => { const style = getComputedStyle(header); return { height: header.getBoundingClientRect().height, fontSize: style.fontSize, lineHeight: style.lineHeight, whiteSpace: style.whiteSpace }; }))`,
    );
    for (const header of headers) {
      assert.equal(header.height, 32, `${filename}: pinned table header height`);
      assert.equal(header.fontSize, "12px");
      assert.equal(header.lineHeight, "16px");
      assert.equal(header.whiteSpace, "nowrap", `${filename}: do not wrap resource headings`);
    }
    tableHeaderChecks.push({ filename, headers });
    const formCheck = await evaluate(`(() => {
      const form = document.querySelector(".admin-sheet:not(.confirmation-dialog) .resource-form");
      const footer = form?.querySelector(".dialog-actions");
      if (!footer) return null;
      const rect = footer.getBoundingClientRect();
      return { footerBottom: rect.bottom, footerTop: rect.top, viewportHeight: innerHeight, viewportWidth: innerWidth, formScrollable: form.scrollHeight > form.clientHeight, direction: getComputedStyle(footer).flexDirection, contentWidth: footer.clientWidth - parseFloat(getComputedStyle(footer).paddingLeft) - parseFloat(getComputedStyle(footer).paddingRight), buttons: [...footer.querySelectorAll('button')].map(button => { const bounds = button.getBoundingClientRect(); return { y: bounds.y, width: bounds.width, bottom: bounds.bottom }; }) };
    })()`);
    if (formCheck !== null) {
      assert.ok(
        Math.abs(formCheck.footerBottom - formCheck.viewportHeight) <= 1,
        "Sheet footer must reach viewport bottom",
      );
      assert.ok(formCheck.footerTop >= 0, "Sheet actions must remain visible");
      assert.equal(formCheck.buttons.length, 2);
      assert.equal(formCheck.direction, formCheck.viewportWidth < 640 ? "column-reverse" : "row");
      if (formCheck.viewportWidth < 640) {
        assert.ok(formCheck.buttons[0].y > formCheck.buttons[1].bottom, "Cancel below primary");
        for (const button of formCheck.buttons) {
          assert.ok(
            Math.abs(button.width - formCheck.contentWidth) < 1,
            "Full-width mobile action",
          );
        }
      } else {
        assert.equal(formCheck.buttons[0].y, formCheck.buttons[1].y, "Desktop actions stay inline");
      }
      formChecks.push({ filename, ...formCheck });
    }
    layoutChecks.push(
      JSON.parse(
        await evaluate(
          `JSON.stringify({ filename: ${JSON.stringify(filename)}, locale: document.documentElement.lang, theme: document.documentElement.dataset.theme, viewport: [innerWidth, innerHeight], documentWidth: [document.documentElement.clientWidth, document.documentElement.scrollWidth], overflow: document.documentElement.scrollWidth > document.documentElement.clientWidth })`,
        ),
      ),
    );
    const result = await command("Page.captureScreenshot", {
      format: "png",
      fromSurface: true,
      captureBeyondViewport: false,
    });
    writeFileSync(join(output, filename), Buffer.from(result.data, "base64"));
  };
  const connect = async (token) => {
    await waitFor("document.querySelector('.connect-form') !== null", "connection form");
    const values = [app, tenantId, projectId, token];
    const connected = await evaluate(
      `(() => { const inputs = [...document.querySelectorAll('.connect-form input')]; const values = ${JSON.stringify(values)}; if (inputs.length !== values.length) return false; for (let index = 0; index < inputs.length; index += 1) { const setter = Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value').set; setter.call(inputs[index], values[index]); inputs[index].dispatchEvent(new Event('input', { bubbles: true })); } document.querySelector('.connect-form').requestSubmit(); return true; })()`,
    );
    if (!connected) throw new Error("Could not submit connection form");
  };
  const setTheme = async (theme) => {
    if ((await evaluate("document.documentElement.dataset.theme")) === theme) return;
    await click(".profile-menu summary");
    await click(".profile-menu .dropdown-menu button:first-of-type");
    await waitFor(
      `document.documentElement.dataset.theme === ${JSON.stringify(theme)}`,
      `${theme} theme`,
    );
    await waitFor("!document.querySelector('.profile-menu').open", "profile menu close");
  };
  const setLocale = async (locale) => {
    if ((await evaluate("document.documentElement.lang")) === locale) return;
    await click(".profile-menu summary");
    const changed = await evaluate(
      `(() => { const select = document.querySelector('.locale-picker select'); if (!select) return false; const setter = Object.getOwnPropertyDescriptor(HTMLSelectElement.prototype, 'value').set; setter.call(select, ${JSON.stringify(locale)}); select.dispatchEvent(new Event('change', { bubbles: true })); return true; })()`,
    );
    if (!changed) throw new Error("Missing locale selector");
    await waitFor(
      `document.documentElement.lang === ${JSON.stringify(locale)}`,
      `${locale} locale`,
    );
    if (await evaluate("document.querySelector('.profile-menu').open")) {
      await click(".profile-menu summary");
    }
  };
  const navigatePage = async (page) => {
    await waitFor(
      "innerWidth < 768 ? document.querySelector('.mobile-nav-dialog') !== null : document.querySelector('.mobile-nav-dialog') === null",
      "responsive navigation mounted",
    );
    if (await evaluate("innerWidth < 768 && !document.querySelector('.mobile-nav-dialog')?.open")) {
      await clickAt(".mobile-nav-trigger");
      await waitFor(
        "document.querySelector('.mobile-nav-dialog')?.open",
        "navigation for target selection",
      );
    }
    await evaluate(
      `document.querySelector('.sidebar nav button[data-page=${page}]').scrollIntoView({ block: 'nearest' })`,
    );
    await clickAt(`.sidebar nav button[data-page=${page}]`);
    await waitFor(
      `document.querySelector('.sidebar [data-page=${page}]').getAttribute('aria-current') === 'page'`,
      `${page} navigation`,
    );
  };
  const navigateTargets = async () => {
    await navigatePage("targets");
    await waitFor(
      "document.querySelector('.sidebar [data-page=targets]').getAttribute('aria-current') === 'page' && document.querySelectorAll('tbody tr').length === 3",
      "three live targets",
    );
  };
  const openTarget = async (name) => {
    const index = await evaluate(
      `[...document.querySelectorAll('tbody tr')].findIndex(row => row.textContent.includes(${JSON.stringify(name)}))`,
    );
    if (index < 0) throw new Error(`Missing target row: ${name}`);
    const selector = `tbody tr:nth-child(${index + 1}) .row-action`;
    await evaluate(
      `document.querySelector(${JSON.stringify(selector)}).scrollIntoView({ block: 'nearest' })`,
    );
    await clickAt(selector);
    await waitFor("document.querySelector('.admin-sheet') !== null", "target detail Sheet");
  };
  const closeSheet = async () => {
    await click(".admin-sheet .sheet-close, .admin-sheet .icon-button");
    await waitFor("document.querySelector('.admin-sheet') === null", "Sheet close");
  };
  const closeNavigationBackdrop = async () => {
    const point = await evaluate("({ x: innerWidth - 10, y: innerHeight / 2 })");
    await command("Input.dispatchMouseEvent", {
      type: "mousePressed",
      ...point,
      button: "left",
      clickCount: 1,
    });
    await command("Input.dispatchMouseEvent", {
      type: "mouseReleased",
      ...point,
      button: "left",
      clickCount: 1,
    });
    await waitFor(
      "!document.querySelector('.mobile-nav-dialog').open",
      "mobile navigation backdrop close",
    );
  };
  const verifyMobileKeyboard = async () => {
    assert.equal(
      await evaluate("document.querySelector('.sidebar').getBoundingClientRect().width"),
      0,
    );
    await clickAt(".mobile-nav-trigger");
    await waitFor(
      "document.querySelector('.mobile-nav-dialog')?.matches(':modal')",
      "native modal navigation",
    );
    await waitFor(
      "document.activeElement === document.querySelector('.sidebar nav button.active')",
      "active navigation focus",
    );
    assert.equal(
      await evaluate(
        "getComputedStyle(document.querySelector('.sidebar .brand-mark')).display !== 'none'",
      ),
      true,
    );
    assert.equal(
      await evaluate(
        "getComputedStyle(document.querySelector('.mobile-nav-dialog'), '::backdrop').animationDuration",
      ),
      "1e-05s",
    );
    for (let index = 0; index < 12; index += 1) {
      await pressKey("Tab");
      assert.equal(
        await evaluate(
          "document.activeElement === document.body || document.querySelector('.mobile-nav-dialog').contains(document.activeElement)",
        ),
        true,
      );
    }
    await pressKey("Escape");
    await waitFor("!document.querySelector('.mobile-nav-dialog').open", "mobile Escape close");
    await waitFor(
      "document.activeElement === document.querySelector('.mobile-nav-trigger')",
      "mobile trigger focus restore",
    );
    await pressKey("b", "KeyB", 4);
    await waitFor("document.querySelector('.mobile-nav-dialog').open", "mobile sidebar shortcut");
  };

  const verifyModals = async (name) => {
    await clickAt(".heading-actions .primary");
    await waitFor(
      "document.activeElement.matches('[data-sheet-autofocus]')",
      "registration autofocus",
    );
    await pressKey("Escape");
    await waitFor("!document.querySelector('.admin-sheet')", "registration close");
    assert.equal(
      await evaluate(
        "document.activeElement === document.querySelector('.heading-actions .primary')",
      ),
      true,
    );
    await openTarget("visual-docker");
    const trigger = ".detail-panel .action-block:nth-child(1 of .action-block) button";
    await waitFor(
      `document.querySelector(${JSON.stringify(trigger)}) !== null`,
      "target lifecycle action rendered",
    );
    await evaluate(
      `document.querySelector(${JSON.stringify(trigger)}).scrollIntoView({ block: 'nearest' })`,
    );
    await clickAt(trigger);
    await waitFor(
      "document.querySelector('.confirmation-dialog')?.matches(':modal')",
      "confirmation modal",
    );
    await waitFor(
      "document.querySelector('.confirmation-dialog').getAnimations().every(animation => animation.playState === 'finished')",
      "confirmation entrance settled",
    );
    await waitFor(
      "!document.querySelector('.confirmation-dialog input').disabled",
      "preview operation complete",
    );
    const geometry = await evaluate(`(() => {
        const dialog = document.querySelector('.confirmation-dialog'), rect = dialog.getBoundingClientRect();
        return {width:rect.width, height:rect.height, x:rect.x, y:rect.y, viewportWidth:innerWidth, viewportHeight:innerHeight, radius:getComputedStyle(dialog).borderRadius, padding:getComputedStyle(dialog).padding, titleSize:getComputedStyle(dialog.querySelector('h2')).fontSize};
      })()`);
    assert.equal(geometry.width, Math.min(512, geometry.viewportWidth - 32));
    assert.ok(Math.abs(geometry.x + geometry.width / 2 - geometry.viewportWidth / 2) < 1);
    assert.ok(Math.abs(geometry.y + geometry.height / 2 - geometry.viewportHeight / 2) < 1);
    assert.ok(geometry.height <= geometry.viewportHeight * 0.8 + 1);
    assert.equal(geometry.radius, "8px");
    assert.equal(geometry.padding, "24px");
    assert.equal(geometry.titleSize, "18px");
    assert.equal(
      await evaluate("document.querySelector('.confirmation-dialog [type=submit]').disabled"),
      true,
    );
    await clickAt(".confirmation-dialog input[type=checkbox]");
    await waitFor(
      "!document.querySelector('.confirmation-dialog [type=submit]').disabled",
      "review gate enabled",
    );
    // Only preview and inspect the gate; never submit a lifecycle mutation.
    await screenshot(`confirmation-drain-${name}.png`);
    for (let tab = 0; tab < 8; tab++) {
      await pressKey("Tab");
      assert.equal(
        await evaluate(
          "document.activeElement === document.body || document.querySelector('.confirmation-dialog').contains(document.activeElement)",
        ),
        true,
      );
    }
    await pressKey("Escape");
    await waitFor("!document.querySelector('.confirmation-dialog')", "confirmation Escape close");
    assert.equal(
      await evaluate(
        `document.activeElement === document.querySelector(${JSON.stringify(trigger)})`,
      ),
      true,
      JSON.stringify(
        await evaluate(
          "({ activeTag: document.activeElement?.tagName, activeClass: document.activeElement?.className, modalCount: document.querySelectorAll('dialog:modal').length, toastOpen: document.querySelector('.success-toast')?.matches(':popover-open') })",
        ),
      ),
    );
    assert.equal(await evaluate("document.querySelector('.admin-sheet').matches(':modal')"), true);
    modalChecks.push({
      name,
      kind: "drain",
      ...geometry,
      registrationFocusRestore: true,
      nestedFocusRestore: true,
      reviewGate: true,
      keyboardContained: true,
    });
    const cleanup = ".cleanup-preview-block button";
    await evaluate(
      `document.querySelector(${JSON.stringify(cleanup)}).scrollIntoView({ block: 'nearest' })`,
    );
    await clickAt(cleanup);
    await waitFor(
      "document.querySelector('.admin-sheet .operation-feedback code')?.textContent === 'ENVIRONMENT_CLEANUP_UNAVAILABLE'",
      "real cleanup failure",
    );
    assert.equal(await evaluate("document.querySelector('.confirmation-dialog') === null"), true);
    const errorFeedback = await evaluate(`(() => {
      const visible = [...document.querySelectorAll('.operation-feedback')].filter(el => el.checkVisibility());
      const alert = visible[0]?.querySelector('[role=alert]');
      return {count:visible.length,inModal:!!alert?.closest('dialog:modal'),text:alert?.textContent,fontSize:alert ? getComputedStyle(alert.querySelector('p')).fontSize : null};
    })()`);
    assert.equal(errorFeedback.count, 1);
    assert.equal(errorFeedback.inModal, true);
    assert.equal(errorFeedback.fontSize, "14px");
    assert.match(
      errorFeedback.text,
      name.startsWith("zh-CN") ? /目标执行器不可用/ : /target actuator is unavailable/,
    );
    await screenshot(`cleanup-error-${name}.png`);
    // Retrying another real preview clears the old failure without submitting a mutation.
    await evaluate(
      `document.querySelector(${JSON.stringify(trigger)}).scrollIntoView({ block: 'nearest' })`,
    );
    await clickAt(trigger);
    await waitFor(
      "document.querySelector('.confirmation-dialog')?.matches(':modal')",
      "preview recovery",
    );
    assert.equal(
      await evaluate("document.querySelector('.operation-feedback [role=alert]') === null"),
      true,
    );
    await pressKey("Escape");
    await waitFor(
      "!document.querySelector('.confirmation-dialog')",
      "recovered confirmation close",
    );
    modalChecks[modalChecks.length - 1].errorFeedback = errorFeedback;
    modalChecks[modalChecks.length - 1].previewRecovery = true;
    await pressKey("Escape");
    await waitFor("!document.querySelector('.admin-sheet')", "detail Escape close");
    assert.equal(await evaluate("document.activeElement.matches('tbody .row-action')"), true);
  };

  const verifyToast = async (name) => {
    const before = await evaluate(
      "document.querySelector('.target-list-panel').getBoundingClientRect().y",
    );
    await clickAt(".heading-actions button:first-child");
    await waitFor(
      "document.querySelector('.success-toast')?.matches(':popover-open')",
      "successful refresh Toast",
    );
    const geometry = await evaluate(`(() => {
      const toast = document.querySelector('.success-toast');
      const rect = toast.getBoundingClientRect();
      return { width: rect.width, right: innerWidth - rect.right, bottom: innerHeight - rect.bottom, fontSize: getComputedStyle(toast).fontSize, message: toast.innerText, tableY: document.querySelector('.target-list-panel').getBoundingClientRect().y, viewport: innerWidth };
    })()`);
    assert.equal(geometry.tableY, before, "Success feedback must not shift the resource list");
    assert.equal(geometry.width, geometry.viewport <= 600 ? geometry.viewport - 32 : 356);
    assert.equal(geometry.right, geometry.viewport <= 600 ? 16 : 32);
    assert.equal(geometry.bottom, geometry.viewport <= 600 ? 16 : 32);
    assert.equal(geometry.fontSize, "13px");
    await screenshot(`toast-${name}.png`);
    if (name === "en-US-light-desktop") {
      const point = await evaluate(
        "(() => { const r = document.querySelector('.success-toast').getBoundingClientRect(); return { x: r.x + 30, y: r.y + 30 }; })()",
      );
      await command("Input.dispatchMouseEvent", { type: "mouseMoved", ...point });
      await delay(4100);
      assert.equal(
        await evaluate("document.querySelector('.success-toast')?.matches(':popover-open')"),
        true,
        "Hovered Toast must not expire",
      );
      await command("Input.dispatchMouseEvent", { type: "mouseMoved", x: 0, y: 0 });
    }
    await evaluate("document.querySelector('.toast-close').focus()");
    assert.equal(await evaluate("document.activeElement.matches('.toast-close')"), true);
    await delay(4100);
    assert.equal(
      await evaluate("document.querySelector('.success-toast')?.matches(':popover-open')"),
      true,
      "Focused Toast must not expire",
    );
    await pressKey("Enter");
    await waitFor("!document.querySelector('.success-toast')", "keyboard Toast dismissal");
    assert.equal(
      await evaluate(
        "document.activeElement === document.querySelector('.heading-actions button:first-child')",
      ),
      true,
      "Toast dismissal restores the trigger focus",
    );
    await clickAt(".heading-actions button:first-child");
    await waitFor(
      "document.querySelector('.success-toast')?.matches(':popover-open')",
      "second refresh Toast",
    );
    await waitFor("!document.querySelector('.success-toast')", "automatic Toast expiry");
    toastChecks.push({
      name,
      ...geometry,
      focusPause: true,
      keyboardDismiss: true,
      automaticExpiry: true,
    });
  };

  const verifyCommands = async (name) => {
    await evaluate("document.querySelector('.heading-actions button').focus()");
    await pressKey("k", "KeyK", 4);
    await waitFor(
      "document.querySelector('.navigation-commands')?.matches(':modal')",
      "command modal",
    );
    await waitFor(
      "document.activeElement === document.querySelector('.navigation-commands input')",
      "command input focus",
    );
    await waitFor(
      "document.querySelector('.command-surface').getAnimations().every(animation => animation.playState === 'finished')",
      "command entrance settled",
    );
    const geometry = await evaluate(`(() => {
      const dialog = document.querySelector(".navigation-commands");
      return { width: dialog.getBoundingClientRect().width, inputHeight: dialog.querySelector("input").getBoundingClientRect().height, fontSize: getComputedStyle(dialog.querySelector("input")).fontSize, count: dialog.querySelectorAll("[role=option]").length, viewportWidth: innerWidth };
    })()`);
    assert.equal(geometry.width, geometry.viewportWidth < 768 ? 358 : 576);
    assert.equal(geometry.inputHeight, 48);
    assert.equal(geometry.fontSize, "14px");
    assert.equal(geometry.count, 9);
    assert.equal(await evaluate("document.querySelector('#command-targets') === null"), true);
    await screenshot(`commands-${name}.png`);
    await pressKey("ArrowUp");
    await waitFor(
      "document.querySelector('.navigation-commands input').getAttribute('aria-activedescendant') === 'command-maintenance'",
      "command arrow wrap",
    );
    await pressKey("Escape");
    await waitFor("!document.querySelector('.navigation-commands')", "command Escape close");
    assert.equal(
      await evaluate(
        "document.activeElement === document.querySelector('.heading-actions button')",
      ),
      true,
    );
    await pressKey("k", "KeyK", 4);
    await waitFor(
      "document.querySelector('.navigation-commands')?.matches(':modal')",
      "command reopen",
    );
    const search = async (value) => {
      await evaluate(`(() => {
        const input = document.querySelector(".navigation-commands input");
        Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, "value").set.call(input, ${JSON.stringify(value)});
        input.dispatchEvent(new Event("input", { bubbles: true }));
      })()`);
    };
    await search("no-such-navigation-command");
    await waitFor(
      "document.querySelectorAll('.navigation-commands [role=option]').length === 0",
      "empty commands",
    );
    await pressKey("Enter");
    assert.equal(
      await evaluate("document.querySelector('.navigation-commands').matches(':modal')"),
      true,
    );
    await screenshot(`commands-empty-${name}.png`);
    await search(name.startsWith("zh-CN") ? "维护操作" : "Maintenance Operations");
    await waitFor(
      "document.querySelectorAll('.navigation-commands [role=option]').length === 1",
      "filtered command",
    );
    await pressKey("Enter");
    await waitFor(
      "!document.querySelector('.navigation-commands') && document.querySelector('.sidebar [data-page=maintenance]').getAttribute('aria-current') === 'page'",
      "command route selection",
    );
    commandChecks.push({
      name,
      ...geometry,
      arrowWrap: true,
      escapeFocusRestore: true,
      emptyEnterNoop: true,
      realRouteSelection: true,
    });
    await navigateTargets();
    await verifyTargetFilters(name);
    await verifyModals(name);
    await verifyOverview(name);
    const prefix = name.startsWith("zh-CN") ? "zh-CN-" : "";
    const dimensions = name.slice(6);
    await openTarget("visual-ssh");
    await waitFor(
      "document.querySelector('.success-toast')?.matches(':popover-open')",
      "detail success Toast",
    );
    await clickAt(".toast-close");
    await waitFor("!document.querySelector('.success-toast')", "detail Toast pointer dismissal");
    assert.equal(
      await evaluate("document.querySelector('.admin-sheet').contains(document.activeElement)"),
      true,
      "Toast dismissal must keep focus in the open modal",
    );
    assert.equal(
      await evaluate("document.querySelector('.admin-sheet').matches(':modal')"),
      true,
      "Toast close must preserve the detail Sheet",
    );
    await screenshot(`${prefix}detail-${dimensions}.png`);
    await closeSheet();
    await clickAt(".heading-actions .button.primary");
    await waitFor(
      "document.activeElement.matches('[data-sheet-autofocus]')",
      "create form autofocus",
    );
    await screenshot(`${prefix}create-form-${dimensions}.png`);
    await pressKey("Escape");
    await waitFor("!document.querySelector('.admin-sheet')", "create Sheet Escape close");
    await verifyToast(name);
  };

  const verifyOverview = async (name) => {
    const metrics = [
      ["targets", "targets"],
      ["target-attention", "targets"],
      ["leases", "leases"],
      ["workers", "workers"],
      ["lease-attention", "leases"],
    ];
    const counts = {};
    for (const [metric, destination] of metrics) {
      await navigatePage("overview");
      const selector = `[data-metric=${metric}]`;
      await evaluate(
        `document.querySelector(${JSON.stringify(selector)}).scrollIntoView({block: 'center'})`,
      );
      counts[metric] = await evaluate(`document.querySelector('${selector} strong').textContent`);
      // Use a real keyboard activation, not a DOM click, for the metric-to-list transition.
      await evaluate(`document.querySelector(${JSON.stringify(selector)}).focus()`);
      await command("Input.dispatchKeyEvent", {
        type: "keyDown",
        key: "Enter",
        code: "Enter",
        windowsVirtualKeyCode: 13,
        text: "\r",
      });
      await command("Input.dispatchKeyEvent", {
        type: "keyUp",
        key: "Enter",
        code: "Enter",
        windowsVirtualKeyCode: 13,
      });
      await waitFor(
        `document.querySelector('.sidebar [data-page=${destination}]').getAttribute('aria-current') === 'page'`,
        `${metric} destination`,
      );
      assert.equal(await evaluate(`document.querySelector('.list-toolbar input').value`), "");
      assert.ok(
        await evaluate(
          `[...document.querySelectorAll('.list-toolbar .scope-chip')].some(e => e.textContent.trim() === '${destination}.list · ${counts[metric]}')`,
        ),
      );
      if (metric === "target-attention") {
        assert.equal(
          await evaluate(
            "document.querySelectorAll('.target-filter-chips [data-filter=phase]').length",
          ),
          1,
        );
        await click(".target-filter-chips [data-filter=phase] .target-filter-chip-label");
        await waitFor(
          "document.querySelector('.chip-options:popover-open') !== null",
          "overview target filter",
        );
        assert.deepEqual(
          await evaluate(
            "[...document.querySelectorAll('.chip-options [aria-selected=true]')].map(e => e.dataset.value)",
          ),
          ["unprobed", "probing", "unavailable"],
        );
        await pressKey("Escape");
        await click(".target-filters-clear");
      }
      if (destination === "leases") {
        assert.equal(
          await evaluate(
            "document.querySelector('.lease-attention-filter').getAttribute('aria-pressed')",
          ),
          String(metric === "lease-attention"),
        );
      }
      if (metric === "lease-attention") {
        await screenshot(`${name}-lease-attention.png`);
        await click(".lease-attention-filter");
        await waitFor(
          "document.querySelector('.lease-attention-filter').getAttribute('aria-pressed') === 'false'",
          "clear lease attention",
        );
        assert.ok(
          await evaluate(
            `document.querySelector('.list-toolbar .scope-chip').textContent.trim() === 'leases.list · ${counts.leases}'`,
          ),
        );
      }
    }
    await navigatePage("overview");
    await evaluate("document.querySelector('.content').scrollTop = 0");
    await screenshot(`${name}-overview.png`);
    overviewChecks.push({
      name,
      counts,
      keyboardNavigation: true,
      targetPhaseFilter: true,
      leaseAttentionClear: true,
    });
    await navigateTargets();
  };

  const verifyTargetFilters = async (name) => {
    const change = async (selector, value) => {
      await evaluate(`(() => {
        const element = document.querySelector(${JSON.stringify(selector)});
        Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value').set.call(element, ${JSON.stringify(value)});
        element.dispatchEvent(new Event('input', { bubbles: true }));
      })()`);
    };
    const openFilter = async (filter) => {
      await clickAt(".target-filters-trigger");
      await waitFor(
        "document.activeElement.matches('.target-filter-category')",
        "filter menu focus",
      );
      if (filter === "phase") await pressKey("ArrowDown");
      await pressKey("ArrowRight");
      await waitFor(
        "document.querySelector('.target-filter-options:popover-open input') === document.activeElement && document.activeElement.getAttribute('aria-expanded') === 'true'",
        "submenu search focus",
      );
    };
    const closeFilter = async () => {
      await pressKey("Escape");
      await waitFor(
        "!document.querySelector('.target-filter-options:popover-open')",
        "submenu Escape",
      );
      assert.equal(
        await evaluate("document.querySelector('.target-filter-menu').matches(':popover-open')"),
        true,
      );
      await pressKey("Escape");
      await waitFor(
        "document.activeElement.matches('.target-filters-trigger')",
        "filter trigger focus restore",
      );
    };
    await openFilter("kind");
    await pressKey("Enter");
    await waitFor(
      "document.querySelectorAll('.target-table tbody tr').length === 1",
      "kind filter",
    );
    await clickAt(".target-filter-options:popover-open [data-value=ssh]");
    await waitFor(
      "document.querySelectorAll('.target-table tbody tr').length === 2",
      "multi-kind union",
    );
    await waitFor(
      "[...document.querySelectorAll('.target-filter-options:popover-open [aria-selected=true] .filter-checkbox')].length === 2 && [...document.querySelectorAll('.target-filter-options:popover-open [aria-selected=true] .filter-checkbox')].every(element => getComputedStyle(element).opacity === '1' && getComputedStyle(element.querySelector('svg')).visibility === 'visible')",
      "checked visual state",
    );
    await screenshot(`${name}-filter-menu.png`);
    await change(".target-filter-options:popover-open input", "no-such-option");
    await waitFor(
      "document.querySelectorAll('.target-filter-options:popover-open [role=option]').length === 0",
      "empty options",
    );
    await pressKey("Enter");
    assert.equal(await evaluate("document.querySelectorAll('.target-table tbody tr').length"), 2);
    await closeFilter();
    await change(".target-toolbar > input", " VISUAL-DOCKER ");
    await waitFor(
      "document.querySelectorAll('.target-table tbody tr').length === 1",
      "combined filters",
    );
    const facts = await evaluate(
      "document.querySelector('.target-table .target-probe-facts').textContent",
    );
    assert.ok(facts.includes(name.startsWith("zh-CN") ? "暂无数据" : "Not available"));
    await evaluate("document.querySelector('.table-scroll').focus()");
    assert.equal(await evaluate("document.activeElement.matches('.table-scroll')"), true);
    if (name.endsWith("mobile")) {
      await evaluate("document.querySelector('.table-scroll').scrollLeft = 0");
      await pressKey("ArrowRight");
      await waitFor(
        "document.querySelector('.table-scroll').scrollLeft > 0",
        "keyboard table scroll",
      );
      await evaluate("document.querySelector('.table-scroll').scrollLeft = 0");
    }
    await screenshot(`${name}-filtered.png`);
    await openFilter("phase");
    await clickAt(".target-filter-options:popover-open [data-value=unprobed]");
    await clickAt(".target-filter-options:popover-open [data-value=ready]");
    assert.equal(await evaluate("document.querySelectorAll('.target-table tbody tr').length"), 1);
    await clickAt(".target-filter-options:popover-open [data-value=unprobed]");
    await closeFilter();
    await waitFor(
      "document.querySelector('#target-empty-title') !== null",
      "empty filtered results",
    );
    assert.ok(
      (await evaluate("document.querySelector('#target-empty-title').textContent")).includes(
        name.startsWith("zh-CN") ? "没有匹配" : "No matching",
      ),
    );
    await screenshot(`${name}-filter-empty.png`);
    await clickAt(".empty-state .button.primary");
    await waitFor(
      "document.querySelectorAll('.target-table tbody tr').length === 3",
      "clear target filters",
    );
    assert.equal(await evaluate("document.querySelector('.target-filters-clear').disabled"), true);
    targetFilterChecks.push({
      name,
      combinedFilter: true,
      multiSelect: true,
      nestedEscape: true,
      noOptionsEnterNoop: true,
      noMatches: true,
      clearRestored: 3,
      missingProbeFacts: facts,
    });
  };

  await command("Page.enable");
  await command("Network.enable");
  await command("Runtime.enable");
  await command("Log.enable");
  await command("Emulation.setEmulatedMedia", {
    media: "screen",
    features: [{ name: "prefers-reduced-motion", value: "reduce" }],
  });
  await setViewport(1440, 900);
  await command("Page.navigate", { url: app });
  await waitFor("document.readyState === 'complete'", "page load");
  await evaluate("localStorage.setItem('cloud-agents-admin-locale', 'en-US'); location.reload()");
  await waitFor("document.readyState === 'complete'", "English locale reload");
  await waitFor("document.documentElement.lang === 'en-US'", "English locale");
  await connect(readFileSync(adminTokenFile, "utf8").trim());
  await waitFor("document.querySelector('.app-shell') !== null", "Admin API authority");
  await navigateTargets();

  await setTheme("light");
  const shellMetrics = () =>
    evaluate(`(() => {
      const rect = (selector) => { const r = document.querySelector(selector).getBoundingClientRect(); return { width: r.width, height: r.height, y: r.y }; };
      const nav = document.querySelector(".sidebar nav");
      const content = document.querySelector(".content");
      return {
        header: rect(".topbar"), sidebar: rect(".sidebar"), content: rect(".content"),
        navScroll: nav.scrollHeight > nav.clientHeight,
        contentScroll: content.scrollHeight > content.clientHeight,
        documentOverflow: document.documentElement.scrollHeight > innerHeight || document.documentElement.scrollWidth > innerWidth,
        icons: [...nav.querySelectorAll("svg")].map((icon) => ({ width: icon.getBoundingClientRect().width, height: icon.getBoundingClientRect().height, stroke: icon.getAttribute("stroke-width") }))
      };
    })()`);
  const desktopShell = await shellMetrics();
  assert.equal(desktopShell.header.height, 63);
  assert.equal(desktopShell.sidebar.width, 256);
  assert.equal(desktopShell.content.y, 63);
  assert.equal(desktopShell.documentOverflow, false);
  assert.equal(
    desktopShell.icons.length,
    await evaluate("document.querySelectorAll('.sidebar nav button').length"),
  );
  for (const icon of desktopShell.icons) {
    assert.deepEqual(icon, { width: 16, height: 16, stroke: "1.5" });
  }
  await setViewport(1440, 360);
  await waitFor("innerHeight === 360", "short viewport resize");
  await waitFor("document.querySelector('.content').clientHeight <= 297", "short content layout");
  const shortShell = await shellMetrics();
  assert.equal(shortShell.navScroll, true);
  assert.equal(shortShell.contentScroll, true);
  assert.equal(shortShell.documentOverflow, false);
  await evaluate(
    "document.querySelector('.sidebar nav button[data-page=maintenance]').scrollIntoView({ block: 'nearest' })",
  );
  await clickAt(".sidebar nav button[data-page=maintenance]");
  await waitFor(
    "document.querySelector('.sidebar nav button[data-page=maintenance]').classList.contains('active')",
    "short-window final navigation",
  );
  await screenshot("navigation-light-short-desktop.png");
  await evaluate(
    "document.querySelector('.sidebar nav button[data-page=targets]').scrollIntoView({ block: 'nearest' })",
  );
  await clickAt(".sidebar nav button[data-page=targets]");
  await waitFor("document.querySelectorAll('tbody tr').length === 3", "short-window targets");
  await setViewport(1440, 900);
  await waitFor("innerHeight === 900", "desktop viewport restore");
  await evaluate("document.querySelector('.content').scrollTop = 0");
  await screenshot("list-light-desktop.png");
  await verifyCommands("en-US-light-desktop");
  await pressKey("b", "KeyB", 4);
  await waitFor(
    "document.querySelector('.app-shell').classList.contains('sidebar-collapsed')",
    "sidebar collapse shortcut",
  );
  await waitFor(
    "document.querySelector('.sidebar').getBoundingClientRect().width === 48",
    "collapsed sidebar width",
  );
  await screenshot("list-light-desktop-collapsed.png");
  await pressKey("b", "KeyB", 4);
  await waitFor(
    "!document.querySelector('.app-shell').classList.contains('sidebar-collapsed')",
    "sidebar expand shortcut",
  );
  await click(".profile-menu summary");
  assert.equal(
    await evaluate("document.querySelector('.dropdown-menu').getBoundingClientRect().width"),
    256,
  );
  await pressKey("Escape");
  await waitFor("!document.querySelector('.profile-menu').open", "dropdown Escape close");
  await click(".profile-menu summary");
  await clickAt(".page-heading h1");
  await waitFor("!document.querySelector('.profile-menu').open", "dropdown outside close");

  await click(".sidebar nav button[data-page=releases]");
  await click(".heading-actions .button.primary");
  await waitFor(
    "document.querySelector('#register-release-title') !== null",
    "release create Sheet",
  );
  await screenshot("release-form-light-desktop.png");
  await setViewport(390, 360);
  await waitFor(
    "document.querySelector('.admin-sheet').getBoundingClientRect().height === 360",
    "short mobile Sheet resize",
  );
  await evaluate(
    "document.querySelector('.admin-sheet .resource-form').scrollTop = document.querySelector('.admin-sheet .resource-form').scrollHeight",
  );
  await screenshot("release-form-light-short-mobile.png");
  await closeSheet();
  await setViewport(1440, 900);
  await navigateTargets();

  await setTheme("dark");
  await screenshot("list-dark-desktop.png");
  await verifyCommands("en-US-dark-desktop");
  await pressKey("b", "KeyB", 4);
  await waitFor(
    "document.querySelector('.app-shell').classList.contains('sidebar-collapsed')",
    "collapse before mobile breakpoint",
  );
  await setViewport(390, 844);
  await screenshot("list-dark-mobile.png");
  await verifyCommands("en-US-dark-mobile");
  await verifyMobileKeyboard();
  await screenshot("navigation-dark-mobile.png");
  await closeNavigationBackdrop();
  await setTheme("light");
  await screenshot("list-light-mobile.png");
  await verifyCommands("en-US-light-mobile");
  await verifyMobileKeyboard();
  assert.equal((await shellMetrics()).sidebar.width, 288);
  await screenshot("navigation-light-mobile.png");
  await closeNavigationBackdrop();
  await waitFor("!document.querySelector('.mobile-nav-dialog').open", "mobile navigation close");

  await setViewport(1440, 900);
  await waitFor("!document.querySelector('.mobile-nav-dialog')", "restore desktop navigation");
  await pressKey("b", "KeyB", 4);
  await setLocale("zh-CN");
  const immediateChineseTitle = await evaluate(
    "document.querySelector('.page-heading h1').textContent.trim()",
  );
  await command("Page.reload", { ignoreCache: true });
  await waitFor(
    "document.querySelector('.connect-form') !== null",
    "Chinese reload connection form",
  );
  await waitFor("document.documentElement.lang === 'zh-CN'", "persisted Chinese locale");
  const persistedChinese = JSON.parse(
    await evaluate(
      "JSON.stringify({ locale: document.documentElement.lang, saved: localStorage.getItem('cloud-agents-admin-locale'), title: document.querySelector('#connect-title').textContent.trim(), token: document.querySelector('input[type=password]').value })",
    ),
  );
  await connect(readFileSync(adminTokenFile, "utf8").trim());
  await waitFor("document.querySelector('.app-shell') !== null", "Chinese Admin API authority");
  await navigateTargets();

  await setTheme("light");
  await screenshot("zh-CN-list-light-desktop.png");
  await verifyCommands("zh-CN-light-desktop");
  await setTheme("dark");
  await screenshot("zh-CN-list-dark-desktop.png");
  await verifyCommands("zh-CN-dark-desktop");
  await setViewport(390, 844);
  await screenshot("zh-CN-list-dark-mobile.png");
  await verifyCommands("zh-CN-dark-mobile");
  await verifyMobileKeyboard();
  await screenshot("zh-CN-navigation-dark-mobile.png");
  await closeNavigationBackdrop();
  await setTheme("light");
  await screenshot("zh-CN-list-light-mobile.png");
  await verifyCommands("zh-CN-light-mobile");
  await verifyMobileKeyboard();
  await screenshot("zh-CN-navigation-light-mobile.png");
  await closeNavigationBackdrop();
  await waitFor(
    "!document.querySelector('.mobile-nav-dialog').open",
    "Chinese mobile navigation close",
  );

  const authority = JSON.parse(
    await evaluate(
      `JSON.stringify({ rows: [...document.querySelectorAll('tbody tr')].map((row) => ({ name: row.cells[0].innerText, kind: row.querySelector('[data-kind]')?.dataset.kind })), localKeys: Object.keys(localStorage).sort(), sessionKeys: Object.keys(sessionStorage).sort(), persistedValues: [...Object.values(localStorage), ...Object.values(sessionStorage)], locale: document.documentElement.lang, savedLocale: localStorage.getItem('cloud-agents-admin-locale'), messageKeyVisible: /\\b(?:action|account|boundary|cleanup|common|connection|detail|document|error|lease|nav|notice|operation|overview|page|phase|profile|resource|search|sheet|table|target)\\.[A-Za-z]/.test(document.body.innerText) })`,
    ),
  );
  const adminConsoleErrors = [...consoleErrors];
  const adminConsoleWarnings = [...consoleWarnings];
  const adminHTTPFailures = httpFailures.filter((failure) => failure.phase === "admin");
  consoleErrors.length = 0;
  consoleWarnings.length = 0;
  phase = "permission-denied";
  await setViewport(1440, 900);
  await evaluate("localStorage.setItem('cloud-agents-admin-locale', 'fr-FR'); location.reload()");
  await waitFor("document.querySelector('.connect-form') !== null", "user-token connection form");
  await waitFor("document.documentElement.lang === 'en-US'", "invalid locale fallback");
  const fallbackLocale = JSON.parse(
    await evaluate(
      "JSON.stringify({ locale: document.documentElement.lang, saved: localStorage.getItem('cloud-agents-admin-locale'), title: document.querySelector('#connect-title').textContent.trim() })",
    ),
  );
  await connect(readFileSync(userTokenFile, "utf8").trim());
  await waitFor("document.querySelector('[role=alert]') !== null", "Admin API permission denial");
  const permissionDenied = await evaluate(
    "document.querySelector('[role=alert]').textContent.trim()",
  );
  await screenshot("permission-denied-light-desktop.png");
  assert.equal(
    layoutChecks.some((check) => check.overflow),
    false,
    "horizontal page overflow",
  );
  assert.equal(authority.messageKeyVisible, false, "untranslated message key");
  assert.equal(persistedChinese.token, "", "bearer survived reload");
  const deniedRequests = httpFailures.filter((failure) => failure.phase === "permission-denied");
  assert.ok(deniedRequests.length > 0);
  assert.ok(deniedRequests.every((failure) => failure.status === 403));
  assert.deepEqual(mutationRequests, [], "visual checks must not submit lifecycle mutations");
  for (const prefix of ["", "zh-CN-"]) {
    for (const theme of ["light", "dark"]) {
      for (const viewport of ["desktop", "mobile"]) {
        for (const state of ["list", "detail", "create-form"]) {
          const filename = `${prefix}${state}-${theme}-${viewport}.png`;
          assert.equal(
            layoutChecks.filter(
              (check) =>
                check.filename === filename &&
                check.locale === (prefix ? "zh-CN" : "en-US") &&
                check.theme === theme &&
                check.viewport[0] === (viewport === "desktop" ? 1440 : 390) &&
                check.viewport[1] === (viewport === "desktop" ? 900 : 844),
            ).length,
            1,
            `Exactly one matching capture required for ${filename}`,
          );
        }
      }
    }
  }

  const evidence = `${JSON.stringify(
    {
      capturedAt: new Date().toISOString(),
      appOrigin: new URL(app).origin,
      projectId,
      targetKinds: authority.rows.map((row) => row.kind),
      targetCount: authority.rows.length,
      requestOrigins: [...requestOrigins].sort(),
      localStorageKeys: authority.localKeys,
      sessionStorageKeys: authority.sessionKeys,
      bearerPersisted: authority.persistedValues.some((value) => /^eyJ|bearer\s/i.test(value)),
      locale: {
        immediateChineseTitle,
        persistedChinese,
        invalidFallback: fallbackLocale,
        connectedChinese: authority.locale,
        savedChinese: authority.savedLocale,
        messageKeyVisible: authority.messageKeyVisible,
      },
      layoutChecks,
      tableHeaderChecks,
      toastChecks,
      formChecks,
      commandChecks,
      modalChecks,
      targetFilterChecks,
      overviewChecks,
      mutationRequests,
      shellChecks: { desktopShell, shortShell, shortWindowFinalNavigation: true },
      interactions: {
        sidebarShortcut: true,
        dropdownEscapeClose: true,
        dropdownOutsideClose: true,
        sheetEscapeClose: true,
        createFormAutofocus: true,
        mobileNavigationOpenClose: true,
        mobileModalKeyboard: true,
        mobileCollapsedDesktopTransition: true,
        localeImmediateSwitch: true,
        localeReloadRestore: true,
        invalidLocaleFallback: true,
      },
      permissionDenied,
      adminConsoleErrors,
      adminConsoleWarnings,
      adminHTTPFailures,
      permissionDeniedConsoleErrors: consoleErrors,
      permissionDeniedConsoleWarnings: consoleWarnings,
      permissionDeniedHTTPFailures: httpFailures.filter(
        (failure) => failure.phase === "permission-denied",
      ),
    },
    null,
    2,
  )}\n`;
  const formattedEvidence = await format("browser-evidence.json", evidence, {
    printWidth: 100,
  });
  if (formattedEvidence.errors.length > 0) {
    throw new Error(`Could not format browser evidence: ${formattedEvidence.errors[0].message}`);
  }
  writeFileSync(join(output, "browser-evidence.json"), formattedEvidence.code);
} finally {
  socket?.close();
  if (browser.exitCode === null && browser.signalCode === null) {
    const stopped = new Promise((resolve) => browser.once("exit", resolve));
    browser.kill("SIGTERM");
    await stopped;
  }
  renameSync(profile, join(homedir(), ".Trash", `cloud-agents-admin-cdp-${Date.now()}`));
}
