import { spawn } from "node:child_process";
import { mkdirSync, mkdtempSync, readFileSync, renameSync, writeFileSync } from "node:fs";
import { homedir, tmpdir } from "node:os";
import { join } from "node:path";

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
  const requestOrigins = new Set();
  const consoleErrors = [];
  const consoleWarnings = [];
  const httpFailures = [];
  let phase = "admin";
  socket.addEventListener("message", ({ data }) => {
    const message = JSON.parse(data);
    if (message.method === "Network.requestWillBeSent") {
      const requestURL = new URL(message.params.request.url);
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
    throw new Error(`Timed out waiting for ${label}`);
  };
  const click = async (selector) => {
    const clicked = await evaluate(
      `(() => { const element = document.querySelector(${JSON.stringify(selector)}); if (!element) return false; element.click(); return true; })()`,
    );
    if (!clicked) throw new Error(`Missing clickable element: ${selector}`);
  };
  const clickText = async (selector, text) => {
    const clicked = await evaluate(
      `(() => { const element = [...document.querySelectorAll(${JSON.stringify(selector)})].find((candidate) => candidate.textContent.trim() === ${JSON.stringify(text)}); if (!element) return false; element.click(); return true; })()`,
    );
    if (!clicked) throw new Error(`Missing ${selector} with text ${text}`);
  };
  const setViewport = (width, height) =>
    command("Emulation.setDeviceMetricsOverride", {
      width,
      height,
      deviceScaleFactor: 1,
      mobile: width < 600,
    });
  const pressKey = async (key, code = key, modifiers = 0) => {
    const virtualKeyCode = key === "Escape" ? 27 : key.toUpperCase().charCodeAt(0);
    const params = { key, code, modifiers, windowsVirtualKeyCode: virtualKeyCode };
    await command("Input.dispatchKeyEvent", { type: "rawKeyDown", ...params });
    await command("Input.dispatchKeyEvent", { type: "keyUp", ...params });
  };
  const clickAt = async (selector) => {
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
    await delay(250);
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
    await clickText(".profile-menu button", theme === "dark" ? "Dark mode" : "Light mode");
    await waitFor(
      `document.documentElement.dataset.theme === ${JSON.stringify(theme)}`,
      `${theme} theme`,
    );
    await waitFor("!document.querySelector('.profile-menu').open", "profile menu close");
  };
  const closeSheet = async () => {
    await click(".admin-sheet [aria-label='Close']");
    await waitFor("document.querySelector('.admin-sheet') === null", "Sheet close");
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
  await connect(readFileSync(adminTokenFile, "utf8").trim());
  await waitFor("document.querySelector('.app-shell') !== null", "Admin API authority");
  await click(".sidebar nav button[title='Deployment Targets']");
  await waitFor("document.querySelectorAll('tbody tr').length === 3", "three live targets");

  await setTheme("light");
  await screenshot("list-light-desktop.png");
  await pressKey("b", "KeyB", 4);
  await waitFor(
    "document.querySelector('.app-shell').classList.contains('sidebar-collapsed')",
    "sidebar collapse shortcut",
  );
  await screenshot("list-light-desktop-collapsed.png");
  await pressKey("b", "KeyB", 4);
  await waitFor(
    "!document.querySelector('.app-shell').classList.contains('sidebar-collapsed')",
    "sidebar expand shortcut",
  );
  await click(".profile-menu summary");
  await pressKey("Escape");
  await waitFor("!document.querySelector('.profile-menu').open", "dropdown Escape close");
  await click(".profile-menu summary");
  await clickAt(".page-heading h1");
  await waitFor("!document.querySelector('.profile-menu').open", "dropdown outside close");
  await click("[aria-label='View visual-ssh']");
  await waitFor("document.querySelector('.admin-sheet') !== null", "target detail Sheet");
  await screenshot("detail-light-desktop.png");
  await closeSheet();
  await clickText("button", "Register target");
  await waitFor("document.querySelector('.resource-form') !== null", "create Sheet");
  await waitFor(
    "document.activeElement.matches('[data-sheet-autofocus]')",
    "create form autofocus",
  );
  await screenshot("create-form-light-desktop.png");
  await pressKey("Escape");
  await waitFor("document.querySelector('.admin-sheet') === null", "Sheet Escape close");

  await setTheme("dark");
  await screenshot("list-dark-desktop.png");
  await setViewport(390, 844);
  await screenshot("list-dark-mobile.png");
  await click("[aria-label='View visual-ssh']");
  await screenshot("detail-dark-mobile.png");
  await closeSheet();
  await clickText("button", "Register target");
  await screenshot("create-form-dark-mobile.png");
  await closeSheet();
  await setTheme("light");
  await screenshot("list-light-mobile.png");
  await click("[aria-label='Open navigation']");
  await screenshot("navigation-light-mobile.png");
  await click("[aria-label='Close navigation']");
  await waitFor(
    "!document.querySelector('.sidebar').classList.contains('mobile-open')",
    "mobile navigation close",
  );

  const authority = JSON.parse(
    await evaluate(
      `JSON.stringify({ rows: [...document.querySelectorAll('tbody tr')].map((row) => ({ name: row.cells[0].innerText, kind: row.cells[1].innerText.trim() })), localKeys: Object.keys(localStorage).sort(), sessionKeys: Object.keys(sessionStorage).sort(), persistedValues: [...Object.values(localStorage), ...Object.values(sessionStorage)] })`,
    ),
  );
  const adminConsoleErrors = [...consoleErrors];
  const adminConsoleWarnings = [...consoleWarnings];
  const adminHTTPFailures = httpFailures.filter((failure) => failure.phase === "admin");
  consoleErrors.length = 0;
  consoleWarnings.length = 0;
  phase = "permission-denied";
  await setViewport(1440, 900);
  await command("Page.navigate", { url: app });
  await waitFor("document.querySelector('.connect-form') !== null", "user-token connection form");
  await connect(readFileSync(userTokenFile, "utf8").trim());
  await waitFor("document.querySelector('[role=alert]') !== null", "Admin API permission denial");
  const permissionDenied = await evaluate(
    "document.querySelector('[role=alert]').textContent.trim()",
  );
  await screenshot("permission-denied-light-desktop.png");

  writeFileSync(
    join(output, "browser-evidence.json"),
    `${JSON.stringify(
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
        interactions: {
          sidebarShortcut: true,
          dropdownEscapeClose: true,
          dropdownOutsideClose: true,
          sheetEscapeClose: true,
          createFormAutofocus: true,
          mobileNavigationOpenClose: true,
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
    )}\n`,
  );
} finally {
  socket?.close();
  browser.kill("SIGTERM");
  renameSync(profile, join(homedir(), ".Trash", `cloud-agents-admin-cdp-${Date.now()}`));
}
