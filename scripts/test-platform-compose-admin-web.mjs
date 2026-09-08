import assert from "node:assert/strict";
import { createHash } from "node:crypto";
import { existsSync, mkdtempSync, readFileSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { fileURLToPath } from "node:url";
import { spawn } from "node:child_process";

const [
  argumentOrigin,
  adminTokenFile,
  adminDeniedTokenFile,
  userTokenFile,
  argumentTenantId,
  argumentProjectId,
  argumentUserOrigin,
] = process.argv.slice(2);
const snapshotMode = Boolean(process.env.SNAPSHOT_ADMIN_APP_URL);
const origin = process.env.SNAPSHOT_ADMIN_APP_URL ?? argumentOrigin;
const tenantId = process.env.SNAPSHOT_ADMIN_TENANT_ID ?? argumentTenantId;
const projectId = process.env.SNAPSHOT_ADMIN_PROJECT_ID ?? argumentProjectId;
if (
  ![origin, tenantId, projectId].every(Boolean) ||
  (!snapshotMode && ![adminTokenFile, adminDeniedTokenFile, userTokenFile].every(Boolean))
) {
  throw new Error(
    "usage: node test-platform-compose-admin-web.mjs ORIGIN ADMIN_TOKEN_FILE ADMIN_DENIED_TOKEN_FILE USER_TOKEN_FILE TENANT_ID PROJECT_ID [USER_ORIGIN]",
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

const adminToken = process.env.SNAPSHOT_ADMIN_TOKEN ?? readFileSync(adminTokenFile, "utf8").trim();
const adminDeniedToken = snapshotMode
  ? (process.env.SNAPSHOT_ADMIN_USER_TOKEN ?? readFileSync(adminDeniedTokenFile, "utf8").trim())
  : readFileSync(adminDeniedTokenFile, "utf8").trim();
const userToken =
  process.env.SNAPSHOT_ADMIN_USER_TOKEN ?? readFileSync(userTokenFile, "utf8").trim();
const fullCapture = snapshotMode && process.env.FOUNDATION_FULL_ADMIN_CAPTURE === "1";
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
    const href = await evaluate("location.href");
    throw new Error(`timed out waiting for ${label} at ${href}: ${errors.join("; ")}`);
  };
  const navigate = async (url) => {
    const result = await command("Page.navigate", { url });
    assert.equal(result.errorText, undefined, `browser could not navigate to ${url}`);
  };

  if (fullCapture) {
    for (const target of [
      {
        targetId: "visual-kubernetes",
        targetName: "visual-kubernetes",
        targetKind: "kubernetes",
        endpoint: "https://visual-kubernetes.test",
        credentialRef: "visual-kubernetes-credential",
      },
      {
        targetId: "visual-ssh",
        targetName: "visual-ssh",
        targetKind: "ssh",
        endpoint: "ssh://visual-ssh.test:22",
        credentialRef: "visual-ssh-credential",
      },
    ]) {
      const response = await fetch(
        `${origin}/v1/admin/tenants/${encodeURIComponent(tenantId)}/projects/${encodeURIComponent(projectId)}/deployment-targets`,
        {
          method: "POST",
          headers: {
            Authorization: `Bearer ${adminToken}`,
            "Content-Type": "application/json",
            "Idempotency-Key": `admin-visual-${target.targetId}`,
            "X-Request-ID": `admin-visual-${target.targetId}`,
          },
          body: JSON.stringify(target),
        },
      );
      assert.equal(response.status, 201, await response.text());
    }
    const response = await fetch(
      `${origin}/v1/admin/tenants/${encodeURIComponent(tenantId)}/projects/${encodeURIComponent(projectId)}/deployment-targets?pageSize=200`,
      {
        headers: {
          Authorization: `Bearer ${adminToken}`,
          "X-Request-ID": "admin-visual-target-list",
        },
      },
    );
    const targetPageBody = await response.text();
    assert.equal(response.status, 200, targetPageBody);
    const targetPage = JSON.parse(targetPageBody);
    assert.ok(
      ["target", "visual-kubernetes", "visual-ssh"].every((targetId) =>
        targetPage.deploymentTargets.some(({ metadata }) => metadata.uid === targetId),
      ),
      JSON.stringify(targetPage),
    );
  }

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
  await navigate(origin);
  await waitFor("document.querySelector('.connect-form') !== null", "Admin connection form");

  const denied = snapshotMode
    ? 403
    : await evaluate(
        `fetch(${JSON.stringify(`${origin}/v1/admin/tenants/${encodeURIComponent(tenantId)}/projects/${encodeURIComponent(projectId)}/deployment-targets?pageSize=1`)}, { headers: { Authorization: ${JSON.stringify(`Bearer ${adminDeniedToken}`)}, "X-Request-ID": "compose-admin-web-user-denied" } }).then(response => response.status)`,
      );
  if (!snapshotMode) {
    assert.equal(denied, 403, "ordinary User token must not cross the Admin API proxy");
    const wrongAudience = await evaluate(
      `fetch(${JSON.stringify(`${origin}/v1/admin/tenants/${encodeURIComponent(tenantId)}/projects/${encodeURIComponent(projectId)}/deployment-targets?pageSize=1`)}, { headers: { Authorization: ${JSON.stringify(`Bearer ${userToken}`)}, "X-Request-ID": "compose-admin-web-user-audience-denied" } }).then(response => response.status)`,
    );
    assert.equal(wrongAudience, 401, "User API audience must not authenticate on Admin API");
    await delay(50);
    errors.length = 0;
  }

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
    !desktop.stored.includes(adminToken) &&
      !desktop.stored.includes(adminDeniedToken) &&
      !desktop.stored.includes(userToken),
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
  let usageCorrection;
  if (snapshotMode) {
    await evaluate("document.querySelector('[data-page=\"sandboxes\"]').click()");
    await waitFor(
      "document.querySelector('.sandbox-table tbody tr button') !== null",
      "Sandbox table",
    );
    await evaluate("document.querySelector('.sandbox-table tbody tr button').click()");
    await waitFor(
      "document.querySelector('#sandbox-usage-corrections-title') !== null",
      "usage correction detail",
    );
    usageCorrection = await evaluate(`(() => {
      const section = document.querySelector('#sandbox-usage-corrections-title').closest('section');
      return { text: section.textContent, fields: section.querySelectorAll('select, input').length };
    })()`);
    assert.ok(usageCorrection.text.includes("Network received bytes"));
    assert.ok(usageCorrection.text.includes("offline-reconciliation"));
    assert.ok(usageCorrection.text.includes("Record correction"));
    assert.equal(usageCorrection.fields, 4);
    await evaluate(
      "document.querySelector('#sandbox-usage-corrections-title').scrollIntoView({ block: 'start' })",
    );
    await delay(100);
    const screenshot = await command("Page.captureScreenshot", { format: "png" });
    writeFileSync(
      join(
        process.env.CLOUD_AGENTS_FOUNDATION_BROWSER_OUTPUT,
        "admin-sandbox-usage-correction.png",
      ),
      Buffer.from(screenshot.data, "base64"),
    );
  }
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
  const updatedTheme = await evaluate("document.documentElement.dataset.theme");
  let fullCaptureResult;
  if (fullCapture) {
    const captureOutput = join(
      process.env.CLOUD_AGENTS_FOUNDATION_BROWSER_OUTPUT,
      "admin-acceptance",
    );
    const fullAdminTokenFile = join(profile, "full-admin-token");
    const fullUserTokenFile = join(profile, "full-user-token");
    writeFileSync(fullAdminTokenFile, adminToken, { mode: 0o600 });
    writeFileSync(fullUserTokenFile, adminDeniedToken, { mode: 0o600 });
    const captureScript = fileURLToPath(
      new URL(
        "../apps/admin-web/visual-baseline/daytona-v0.190.0/capture-actual.mjs",
        import.meta.url,
      ),
    );
    const capture = spawn(
      process.execPath,
      [captureScript, captureOutput, fullAdminTokenFile, fullUserTokenFile, projectId, tenantId],
      {
        env: {
          ...process.env,
          CLOUD_AGENTS_ADMIN_CAPTURE_APP_URL: origin,
          CLOUD_AGENTS_ADMIN_CAPTURE_DOCKER_TARGET: "target",
          CLOUD_AGENTS_ADMIN_CAPTURE_KUBERNETES_TARGET: "visual-kubernetes",
          CLOUD_AGENTS_ADMIN_CAPTURE_SSH_TARGET: "visual-ssh",
          CLOUD_AGENTS_ADMIN_CAPTURE_RUNTIME_PROFILE: "profile",
          CLOUD_AGENTS_ADMIN_CAPTURE_SANDBOX: "sandbox",
          CLOUD_AGENTS_BROWSER_PATH: browserPath,
        },
        stdio: ["ignore", "pipe", "pipe"],
      },
    );
    let captureOutputText = "";
    capture.stdout.on("data", (value) => (captureOutputText += value));
    capture.stderr.on("data", (value) => (captureOutputText += value));
    const captureExit = await new Promise((resolve) => capture.once("close", resolve));
    assert.equal(captureExit, 0, captureOutputText);
    const evidenceBytes = readFileSync(join(captureOutput, "browser-evidence.json"));
    const evidence = JSON.parse(evidenceBytes);
    fullCaptureResult = {
      evidence: "admin-acceptance/browser-evidence.json",
      evidenceSHA256: createHash("sha256").update(evidenceBytes).digest("hex"),
      screenshots: Object.keys(evidence.screenshotHashes).length,
      accessibilityMatrices: evidence.accessibilityChecks.length,
      referenceCommit: evidence.reference.commit,
      targetCount: evidence.targetCount,
      referenceMatches: evidence.reference.matches.length,
      expectedPreviewFailures: evidence.adminHTTPFailures.length,
      unexpectedHTTPFailures: evidence.unexpectedAdminHTTPFailures.length,
      ordinaryUserDeniedRequests: evidence.permissionDeniedHTTPFailures.length,
    };
  }
  const adminRequestCount = apiRequests.length;
  let userRequestCount;
  if (!snapshotMode && argumentUserOrigin) {
    errors.length = 0;
    await command("Emulation.setDeviceMetricsOverride", {
      width: 1440,
      height: 900,
      deviceScaleFactor: 1,
      mobile: false,
    });
    await navigate(argumentUserOrigin);
    await waitFor("document.querySelector('.connect-form') !== null", "User connection form");
    await evaluate(`(() => {
      document.documentElement.dataset.smokeBeforeReload = "true";
      sessionStorage.setItem("cloud-agents.user-web.connection.v1", JSON.stringify({
        endpoint: "https://forbidden.example.test",
        tenantId: ${JSON.stringify(tenantId)},
        projectId: "",
      }));
      location.reload();
    })()`);
    await waitFor(
      "document.readyState === 'complete' && document.documentElement.dataset.smokeBeforeReload !== 'true' && document.querySelector('.connect-form') !== null",
      "reloaded User document",
    );
    const initialUserState = await evaluate(
      `({ text: document.body.textContent, inputs: document.querySelectorAll('.connect-form input').length, stored: JSON.stringify({...localStorage, ...sessionStorage}) })`,
    );
    assert.equal(initialUserState.inputs, 2, "User Web must ask only for tenant and token");
    assert.ok(!/endpoint|credentialref|kubeconfig/iu.test(initialUserState.text));
    assert.ok(
      !/endpoint|credentialref|kubeconfig|forbidden\.example/iu.test(initialUserState.stored),
    );
    const userAdminStatus = await evaluate(
      `fetch(${JSON.stringify(`${argumentUserOrigin}/v1/admin/tenants/${encodeURIComponent(tenantId)}/projects/${encodeURIComponent(projectId)}/deployment-targets?pageSize=1`)}, { headers: { Authorization: ${JSON.stringify(`Bearer ${adminToken}`)}, "X-Request-ID": "user-web-admin-route-denied" } }).then(response => response.status)`,
    );
    assert.equal(userAdminStatus, 404, "User Web origin must not proxy Admin API routes");
    const adminUserStatus = await evaluate(
      `fetch(${JSON.stringify(`${argumentUserOrigin}/v1/tenants/${encodeURIComponent(tenantId)}/projects/${encodeURIComponent(projectId)}/environment-profiles?pageSize=1`)}, { headers: { Authorization: ${JSON.stringify(`Bearer ${adminToken}`)}, "X-Request-ID": "admin-audience-user-api-denied" } }).then(response => response.status)`,
    );
    assert.equal(adminUserStatus, 401, "Admin API audience must not authenticate on User API");
    await delay(50);
    errors.length = 0;
    const requestOffset = apiRequests.length;
    const submittedUser = await evaluate(`(() => {
      const inputs = [...document.querySelectorAll('.connect-form input')];
      const values = ${JSON.stringify([tenantId, userToken])};
      if (inputs.length !== values.length) return false;
      for (let index = 0; index < inputs.length; index += 1) {
        Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value').set.call(inputs[index], values[index]);
        inputs[index].dispatchEvent(new Event('input', { bubbles: true }));
      }
      document.querySelector('.connect-form').requestSubmit();
      return true;
    })()`);
    assert.equal(submittedUser, true);
    await waitFor("document.querySelector('.workspace') !== null", "connected User Web");
    await waitFor(
      "document.querySelector('.project-picker select')?.value === " + JSON.stringify(projectId),
      "User project context",
    );
    const connectedUserState = await evaluate(
      `({ text: document.body.textContent, stored: JSON.stringify({...localStorage, ...sessionStorage}), width: document.documentElement.scrollWidth, clientWidth: document.documentElement.clientWidth })`,
    );
    assert.equal(connectedUserState.width, connectedUserState.clientWidth);
    assert.ok(!/endpoint|credentialref|kubeconfig/iu.test(connectedUserState.text));
    assert.ok(!/endpoint|credentialref|kubeconfig/iu.test(connectedUserState.stored));
    assert.ok(
      !connectedUserState.stored.includes(userToken),
      "User token must remain in memory only",
    );
    const userRequests = apiRequests.slice(requestOffset);
    assert.ok(userRequests.length >= 3, "User Web must load real User API resources");
    assert.ok(
      userRequests.every(
        ({ origin: value, path }) =>
          value === argumentUserOrigin && path.startsWith("/v1/") && !path.startsWith("/v1/admin/"),
      ),
    );
    assert.deepEqual(errors, []);
    userRequestCount = userRequests.length;
  }
  const browserResult = {
    requests: adminRequestCount,
    ordinaryUserStatus: snapshotMode ? undefined : denied,
    locale: "en-US",
    desktopWidth: 1440,
    mobileWidth: 390,
    usageCorrectionVisible: usageCorrection !== undefined,
    screenshot: snapshotMode ? "admin-sandbox-usage-correction.png" : undefined,
    fullCapture: fullCaptureResult,
  };
  process.stdout.write(
    snapshotMode
      ? `FOUNDATION_SNAPSHOT_BROWSER=${JSON.stringify(browserResult)}\n`
      : `User/Admin Web browser smoke passed (admin-requests=${browserResult.requests}, user-requests=${userRequestCount ?? 0}, user-admin=403, locale=en-US, theme=${originalTheme}->${updatedTheme}, widths=1440/390)\n`,
  );
} finally {
  socket?.close();
  if (browser.exitCode === null) {
    browser.kill("SIGTERM");
    await new Promise((resolve) => browser.once("close", resolve));
  }
  rmSync(profile, { recursive: true, force: true });
}
