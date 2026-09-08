import assert from "node:assert/strict";
import { existsSync, mkdtempSync, readFileSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { spawn } from "node:child_process";

const [origin, adminTokenFile, userTokenFile, tenantId, projectId] = process.argv.slice(2);
if (![origin, adminTokenFile, userTokenFile, tenantId, projectId].every(Boolean)) {
  throw new Error(
    "usage: node test-platform-compose-admin-web.mjs ORIGIN ADMIN_TOKEN_FILE USER_TOKEN_FILE TENANT_ID PROJECT_ID",
  );
}
const browserPath = [
  process.env.CLOUD_AGENTS_BROWSER_PATH,
  "/Applications/Brave Browser.app/Contents/MacOS/Brave Browser",
  "/Applications/Google Chrome.app/Contents/MacOS/Google Chrome",
  "/usr/bin/google-chrome",
  "/usr/bin/chromium",
  "/usr/bin/chromium-browser",
].find((path) => path && existsSync(path));
if (!browserPath) throw new Error("Compose Admin Web smoke requires Chrome, Chromium, or Brave");

const adminToken = readFileSync(adminTokenFile, "utf8").trim();
const userToken = readFileSync(userTokenFile, "utf8").trim();
const profile = mkdtempSync(join(tmpdir(), "cloud-agents-admin-web-smoke-"));
const browser = spawn(
  browserPath,
  [
    "--headless=new",
    "--disable-gpu",
    "--no-first-run",
    "--no-default-browser-check",
    "--disable-background-networking",
    "--remote-debugging-port=0",
    `--user-data-dir=${profile}`,
    "about:blank",
  ],
  { stdio: "ignore" },
);
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
  if (!port) throw new Error("browser did not expose DevToolsActivePort");
  const targets = await fetch(`http://127.0.0.1:${port}/json/list`).then((response) =>
    response.json(),
  );
  const page = targets.find((target) => target.type === "page");
  if (!page) throw new Error("browser did not expose a page target");
  socket = new WebSocket(page.webSocketDebuggerUrl);
  await new Promise((resolve, reject) => {
    socket.addEventListener("open", resolve, { once: true });
    socket.addEventListener("error", reject, { once: true });
  });

  let nextId = 1;
  const pending = new Map();
  const apiRequests = [];
  const errors = [];
  socket.addEventListener("message", ({ data }) => {
    const message = JSON.parse(data);
    if (message.method === "Network.requestWillBeSent") {
      const url = new URL(message.params.request.url);
      if (url.pathname.startsWith("/v1/")) {
        apiRequests.push({
          method: message.params.request.method,
          origin: url.origin,
          path: url.pathname,
        });
      }
    } else if (message.method === "Runtime.consoleAPICalled" && message.params.type === "error") {
      errors.push(
        message.params.args.map(({ value, description }) => value ?? description).join(" "),
      );
    } else if (message.method === "Log.entryAdded" && message.params.entry.level === "error") {
      errors.push(message.params.entry.text);
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
      awaitPromise: true,
      returnByValue: true,
    });
    if (result.exceptionDetails) throw new Error(result.exceptionDetails.text);
    return result.result.value;
  };
  const waitFor = async (expression, label) => {
    for (let attempt = 0; attempt < 200; attempt += 1) {
      if (await evaluate(expression)) return;
      await delay(50);
    }
    throw new Error(`timed out waiting for ${label}`);
  };

  await Promise.all([
    command("Network.enable"),
    command("Page.enable"),
    command("Runtime.enable"),
    command("Log.enable"),
  ]);
  await command("Emulation.setDeviceMetricsOverride", {
    width: 1440,
    height: 900,
    deviceScaleFactor: 1,
    mobile: false,
  });
  await command("Page.navigate", { url: origin });
  await waitFor("document.querySelector('.connect-form') !== null", "Admin connection form");

  const denied = await evaluate(
    `fetch(${JSON.stringify(`${origin}/v1/admin/tenants/${encodeURIComponent(tenantId)}/projects/${encodeURIComponent(projectId)}/deployment-targets?pageSize=1`)}, { headers: { Authorization: ${JSON.stringify(`Bearer ${userToken}`)}, "X-Request-ID": "compose-admin-web-user-denied" } }).then(response => response.status)`,
  );
  assert.equal(denied, 403, "ordinary User token must not cross the Admin API proxy");
  await delay(50);
  errors.length = 0;

  const submitted = await evaluate(`(() => {
    const inputs = [...document.querySelectorAll('.connect-form input')];
    const values = ${JSON.stringify([origin, tenantId, projectId, adminToken])};
    if (inputs.length !== values.length) return false;
    for (let index = 0; index < inputs.length; index += 1) {
      Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value').set.call(inputs[index], values[index]);
      inputs[index].dispatchEvent(new Event('input', { bubbles: true }));
    }
    document.querySelector('.connect-form').requestSubmit();
    return true;
  })()`);
  assert.equal(submitted, true);
  await waitFor("document.querySelector('.app-shell') !== null", "connected Admin Web");
  await waitFor(
    "document.body.textContent.includes(" + JSON.stringify(projectId) + ")",
    "project context",
  );

  const desktop = await evaluate(
    "({ width: document.documentElement.scrollWidth, clientWidth: document.documentElement.clientWidth, stored: JSON.stringify({...localStorage, ...sessionStorage}) })",
  );
  assert.equal(desktop.width, desktop.clientWidth, "desktop layout must not overflow horizontally");
  assert.ok(
    !desktop.stored.includes(adminToken) && !desktop.stored.includes(userToken),
    "tokens must remain out of browser storage",
  );

  await evaluate("document.querySelector('.profile-menu summary').click()");
  await evaluate(
    `(() => { const select = document.querySelector('.locale-picker select'); Object.getOwnPropertyDescriptor(HTMLSelectElement.prototype, 'value').set.call(select, 'en-US'); select.dispatchEvent(new Event('change', { bubbles: true })); })()`,
  );
  await waitFor("document.documentElement.lang === 'en-US'", "English locale");
  const originalTheme = await evaluate("document.documentElement.dataset.theme");
  await evaluate(
    "document.querySelector('.profile-menu .dropdown-menu button:first-of-type').click()",
  );
  await waitFor(
    `document.documentElement.dataset.theme !== ${JSON.stringify(originalTheme)}`,
    "theme switch",
  );
  await command("Emulation.setDeviceMetricsOverride", {
    width: 390,
    height: 844,
    deviceScaleFactor: 1,
    mobile: true,
  });
  await delay(100);
  const mobile = await evaluate(
    "({ width: document.documentElement.scrollWidth, clientWidth: document.documentElement.clientWidth, navVisible: getComputedStyle(document.querySelector('.mobile-nav-trigger')).display !== 'none' })",
  );
  assert.equal(mobile.width, mobile.clientWidth, "mobile layout must not overflow horizontally");
  assert.equal(mobile.navVisible, true, "mobile resource navigation must remain reachable");
  assert.ok(apiRequests.length >= 10, "Admin Web must load real Admin API resources");
  assert.ok(
    apiRequests.every(
      ({ origin: value, path }) => value === origin && path.startsWith("/v1/admin/"),
    ),
  );
  assert.deepEqual(errors, []);
  process.stdout.write(
    `Admin Web browser smoke passed (requests=${apiRequests.length}, user-admin=403, locale=en-US, theme=${originalTheme}->${await evaluate("document.documentElement.dataset.theme")}, widths=1440/390)\n`,
  );
} finally {
  socket?.close();
  if (browser.exitCode === null) {
    browser.kill("SIGTERM");
    await new Promise((resolve) => browser.once("close", resolve));
  }
  rmSync(profile, { recursive: true, force: true });
}
