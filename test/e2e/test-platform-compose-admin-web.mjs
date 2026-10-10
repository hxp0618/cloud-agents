import assert from "node:assert/strict";
import { createHash } from "node:crypto";
import { existsSync, mkdtempSync, readFileSync, rmSync, statSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { fileURLToPath } from "node:url";
import { spawn } from "node:child_process";

const [
  argumentOrigin,
  adminAccountFile,
  adminDeniedAccountFile,
  userAccountFile,
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
  ![adminAccountFile, adminDeniedAccountFile, userAccountFile].every(Boolean)
) {
  throw new Error(
    "usage: node test-platform-compose-admin-web.mjs ORIGIN ADMIN_ACCOUNT_FILE ADMIN_DENIED_ACCOUNT_FILE USER_ACCOUNT_FILE TENANT_ID PROJECT_ID [USER_ORIGIN]",
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
const browserTLSSPKI = process.env.CLOUD_AGENTS_BROWSER_TLS_SPKI ?? "";
if (browserTLSSPKI && !/^[A-Za-z0-9+/=]+(?:,[A-Za-z0-9+/=]+)*$/u.test(browserTLSSPKI)) {
  throw new Error("CLOUD_AGENTS_BROWSER_TLS_SPKI must contain comma-separated SHA-256 SPKI hashes");
}

function readPrivateAccount(path) {
  if ((statSync(path).mode & 0o077) !== 0) throw new Error("account fixture must be private");
  const value = JSON.parse(readFileSync(path, "utf8"));
  if (
    Object.keys(value).sort().join(",") !== "email,password" ||
    typeof value.email !== "string" ||
    !/^[^@\s]+@[^@\s]+$/u.test(value.email) ||
    value.email.length > 320 ||
    typeof value.password !== "string" ||
    value.password.length < 15 ||
    value.password.length > 128 ||
    /[\r\n]/u.test(value.password)
  )
    throw new Error("account fixture is invalid");
  return Object.freeze(value);
}

const adminAccount = readPrivateAccount(adminAccountFile);
const adminDeniedAccount = readPrivateAccount(adminDeniedAccountFile);
const userAccount = readPrivateAccount(userAccountFile);
assert.deepEqual(
  adminDeniedAccount,
  userAccount,
  "the non-admin Admin check and User workspace must use the same invited account",
);
const fullCapture = snapshotMode && process.env.FOUNDATION_FULL_ADMIN_CAPTURE === "1";

async function provisionInvitedUser() {
  if (process.env.CLOUD_AGENTS_BROWSER_PROVISION_MEMBER !== "1") return;
  if (!argumentUserOrigin) throw new Error("member provisioning requires the User Web origin");
  const request = async (base, path, options = {}) => {
    const response = await fetch(new URL(path, base), {
      redirect: "error",
      ...options,
      headers: { "X-Request-ID": `browser-fixture-${crypto.randomUUID()}`, ...options.headers },
    });
    return { response, body: await response.text() };
  };
  const existing = await request(argumentUserOrigin, "/v1/identity/login/password", {
    method: "POST",
    headers: { Origin: argumentUserOrigin, "Content-Type": "application/json" },
    body: JSON.stringify(userAccount),
  });
  if (existing.response.status === 200) return;
  assert.equal(existing.response.status, 401, existing.body);

  const login = await request(origin, "/v1/identity/login/password", {
    method: "POST",
    headers: { Origin: origin, "Content-Type": "application/json" },
    body: JSON.stringify(adminAccount),
  });
  assert.equal(login.response.status, 200, login.body);
  const session = JSON.parse(login.body);
  const cookie = login.response.headers.get("set-cookie")?.split(";", 1)[0];
  assert.match(cookie ?? "", /^__Host-[^=]+=[A-Za-z0-9_-]{43}$/u);
  assert.match(session.csrfToken ?? "", /^[A-Za-z0-9_-]{43}$/u);
  const invitation = await request(
    origin,
    `/v1/identity/tenants/${encodeURIComponent(tenantId)}/invitations`,
    {
      method: "POST",
      headers: {
        Cookie: cookie,
        Origin: origin,
        "Content-Type": "application/json",
        "X-CSRF-Token": session.csrfToken,
      },
      body: JSON.stringify({
        email: userAccount.email,
        roleName: "project.viewer",
        scopeLevel: "project",
        scopeId: projectId,
        verification: "admin-attested",
      }),
    },
  );
  assert.equal(invitation.response.status, 201, invitation.body);
  const invitationCode = JSON.parse(invitation.body).invitationCode;
  assert.match(invitationCode, /^[A-Za-z0-9_-]{43}$/u);
  const accepted = await request(argumentUserOrigin, "/v1/identity/invitations/accept", {
    method: "POST",
    headers: { Origin: argumentUserOrigin, "Content-Type": "application/json" },
    body: JSON.stringify({
      invitationCode,
      password: userAccount.password,
      displayName: "Browser smoke member",
    }),
  });
  assert.equal(accepted.response.status, 204, accepted.body);
}

await provisionInvitedUser();
const profile = mkdtempSync(join(tmpdir(), "cloud-agents-admin-web-smoke-"));
const browser = spawn(
  browserPath,
  [
    "--headless=new",
    "--disable-gpu",
    "--no-first-run",
    "--no-default-browser-check",
    "--disable-background-networking",
    ...(browserTLSSPKI ? [`--ignore-certificate-errors-spki-list=${browserTLSSPKI}`] : []),
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
    const timer = setTimeout(() => {
      socket.close();
      reject(new Error("browser DevTools websocket did not open within 5 seconds"));
    }, 5000);
    socket.addEventListener(
      "open",
      () => {
        clearTimeout(timer);
        resolve();
      },
      { once: true },
    );
    socket.addEventListener(
      "error",
      (error) => {
        clearTimeout(timer);
        reject(error);
      },
      { once: true },
    );
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
    const page = await evaluate(
      `({ readyState: document.readyState, title: document.title, body: document.body?.textContent?.slice(0, 240), resources: performance.getEntriesByType("resource").map(({ name }) => name).slice(-8) })`,
    );
    throw new Error(
      `timed out waiting for ${label} at ${href}: ${errors.join("; ")}; ${JSON.stringify(page)}`,
    );
  };
  const navigate = async (url) => {
    const result = await command("Page.navigate", { url });
    assert.equal(result.errorText, undefined, `browser could not navigate to ${url}`);
  };

  const login = async (account, label) => {
    await waitFor("document.querySelector('.connect-form') !== null", `${label} login form`);
    const submitted = await evaluate(`(() => {
      const inputs = [...document.querySelectorAll('.connect-form input')];
      const values = ${JSON.stringify([account.email, account.password])};
      if (inputs.length !== values.length) return false;
      for (let index = 0; index < inputs.length; index += 1) {
        Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value').set.call(inputs[index], values[index]);
        inputs[index].dispatchEvent(new Event('input', { bubbles: true }));
      }
      document.querySelector('.connect-form').requestSubmit();
      return true;
    })()`);
    assert.equal(submitted, true, `${label} login form submission`);
  };
  const logout = async (label) => {
    const status = await evaluate(`fetch('/v1/identity/session', {
      method: 'DELETE',
      credentials: 'same-origin',
      headers: { 'X-CSRF-Token': globalThis.__cloudAgentsSmokeCSRF ?? '' },
    }).then(response => response.status)`);
    assert.equal(status, 204, `${label} logout`);
    const currentOrigin = await evaluate("location.origin");
    await navigate(currentOrigin);
    await waitFor("document.querySelector('.connect-form') !== null", `${label} signed out`);
  };
  const selectScope = async (tenantSelector, projectSelector, label) => {
    const selected = await evaluate(`(() => {
      const tenant = document.querySelector(${JSON.stringify(tenantSelector)});
      const project = document.querySelector(${JSON.stringify(projectSelector)});
      if (!tenant || !project) return false;
      const set = Object.getOwnPropertyDescriptor(HTMLSelectElement.prototype, 'value').set;
      set.call(tenant, ${JSON.stringify(tenantId)});
      tenant.dispatchEvent(new Event('change', { bubbles: true }));
      return true;
    })()`);
    assert.equal(selected, true, `${label} tenant selector`);
    await waitFor(
      `document.querySelector(${JSON.stringify(tenantSelector)})?.value === ${JSON.stringify(tenantId)}`,
      `${label} tenant context`,
    );
    await waitFor(
      `[...document.querySelectorAll(${JSON.stringify(projectSelector)} + ' option')].some(option => option.value === ${JSON.stringify(projectId)})`,
      `${label} project option`,
    );
    await evaluate(`(() => {
      const project = document.querySelector(${JSON.stringify(projectSelector)});
      Object.getOwnPropertyDescriptor(HTMLSelectElement.prototype, 'value').set.call(project, ${JSON.stringify(projectId)});
      project.dispatchEvent(new Event('change', { bubbles: true }));
    })()`);
    await waitFor(
      `document.querySelector(${JSON.stringify(projectSelector)})?.value === ${JSON.stringify(projectId)}`,
      `${label} project context`,
    );
  };

  if (fullCapture) {
    const controlPlaneOrigin = new URL(process.env.CLOUD_AGENTS_ADMIN_CAPTURE_CONTROL_PLANE_URL)
      .origin;
    const automationCredentialFile =
      process.env.CLOUD_AGENTS_ADMIN_CAPTURE_AUTOMATION_CREDENTIAL_FILE;
    if (!automationCredentialFile || (statSync(automationCredentialFile).mode & 0o077) !== 0)
      throw new Error("full capture requires a private automation credential file");
    const automationCredential = readFileSync(automationCredentialFile, "utf8").trim();
    if (!/^[A-Za-z0-9_-]{43}$/u.test(automationCredential))
      throw new Error("automation credential is invalid");
    const tokenResponse = await fetch(new URL("/v1/auth/automation/tenant-token", origin), {
      method: "POST",
      headers: {
        Authorization: `Bearer ${automationCredential}`,
        "Content-Type": "application/json",
        "X-Request-ID": "admin-visual-automation-token",
      },
      body: JSON.stringify({ tenantId, projectId }),
    });
    const tokenBody = await tokenResponse.text();
    assert.equal(tokenResponse.status, 200, tokenBody);
    const accessToken = JSON.parse(tokenBody).accessToken;
    assert.match(accessToken, /^[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+$/u);
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
        `${controlPlaneOrigin}/v1/admin/tenants/${encodeURIComponent(tenantId)}/projects/${encodeURIComponent(projectId)}/deployment-targets`,
        {
          method: "POST",
          headers: {
            Authorization: `Bearer ${accessToken}`,
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
      `${controlPlaneOrigin}/v1/admin/tenants/${encodeURIComponent(tenantId)}/projects/${encodeURIComponent(projectId)}/deployment-targets?pageSize=200`,
      {
        headers: {
          Authorization: `Bearer ${accessToken}`,
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
  await command("Page.addScriptToEvaluateOnNewDocument", {
    source: `(() => {
      const originalFetch = globalThis.fetch;
      globalThis.fetch = async (...args) => {
        const response = await originalFetch(...args);
        const path = new URL(typeof args[0] === 'string' ? args[0] : args[0].url, location.href).pathname;
        if ((path === '/v1/identity/login/password' || path === '/v1/identity/session') && response.ok) {
          void response.clone().json().then(value => {
            if (typeof value?.csrfToken === 'string') globalThis.__cloudAgentsSmokeCSRF = value.csrfToken;
          }).catch(() => {});
        }
        return response;
      };
    })();`,
  });
  await command("Emulation.setDeviceMetricsOverride", {
    width: 1440,
    height: 900,
    deviceScaleFactor: 1,
    mobile: false,
  });
  await navigate(origin);
  await login(adminDeniedAccount, "non-admin account");
  await waitFor("typeof globalThis.__cloudAgentsSmokeCSRF === 'string'", "denied session CSRF");
  const denied = await evaluate(
    `fetch(${JSON.stringify(`${origin}/v1/admin/tenants/${encodeURIComponent(tenantId)}/projects/${encodeURIComponent(projectId)}/deployment-targets?pageSize=1`)}, { credentials: 'same-origin', headers: { "X-CSRF-Token": globalThis.__cloudAgentsSmokeCSRF, "X-Request-ID": "compose-admin-web-user-denied" } }).then(response => response.status)`,
  );
  assert.equal(denied, 403, "ordinary account must not cross the Admin API proxy");
  await logout("non-admin account");
  await delay(50);
  errors.length = 0;

  await login(adminAccount, "administrator");
  await waitFor("document.querySelector('.app-shell') !== null", "connected Admin Web");
  await selectScope(
    ".scope-switchers label:first-child select",
    ".scope-switchers label:last-child select",
    "Admin",
  );

  let runtimeVisible = false;
  if (process.env.CLOUD_AGENTS_ADMIN_RUNTIME_SMOKE === "1") {
    await evaluate("document.querySelector('[data-page=\"sandboxes\"]').click()");
    await waitFor(
      "[...document.querySelectorAll('.sandbox-table tbody tr')].some(row => row.textContent.includes('compose-agent-sandbox'))",
      "Managed Agent Sandbox row",
    );
    await evaluate(`(() => {
      const row = [...document.querySelectorAll('.sandbox-table tbody tr')].find(row => row.textContent.includes('compose-agent-sandbox'));
      row?.querySelector('button')?.click();
      return row !== undefined;
    })()`);
    await waitFor(
      "document.querySelector('#managed-agent-runtime-title') !== null",
      "Anywhere Runtime section",
    );
    const runtime = await evaluate(`(() => {
      const section = document.querySelector('#managed-agent-runtime-title')?.closest('section');
      return { text: section?.textContent ?? '', fields: section?.querySelectorAll('input, textarea').length ?? 0 };
    })()`);
    assert.ok(runtime.text.includes("Anywhere Runtime"));
    assert.ok(!runtime.text.includes("approved interaction E2E"));
    assert.ok(!runtime.text.includes("which environment to use"));
    assert.equal(runtime.fields, 0);
    runtimeVisible = true;
  }

  const desktop = await evaluate(
    "({ width: document.documentElement.scrollWidth, clientWidth: document.documentElement.clientWidth, stored: JSON.stringify({...localStorage, ...sessionStorage}) })",
  );
  assert.equal(desktop.width, desktop.clientWidth, "desktop layout must not overflow horizontally");
  assert.ok(
    !desktop.stored.includes(adminAccount.password) &&
      !desktop.stored.includes(adminDeniedAccount.password) &&
      !desktop.stored.includes(userAccount.password) &&
      !/bearer|authorization|session.?handle|eyJ[A-Za-z0-9_-]*\./iu.test(desktop.stored),
    "credentials must remain out of browser storage",
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
    await evaluate("document.querySelector('[data-page=\"storage\"]').click()");
    await waitFor(
      "[...document.querySelectorAll('table tbody tr')].some(row => row.textContent.includes('snapshot'))",
      "Workspace Snapshot table",
    );
    const portableFailover = process.env.CLOUD_AGENTS_FOUNDATION_CROSS_TARGET_RESTORE === "1";
    const snapshotRestore = await evaluate(`(() => {
      const row = [...document.querySelectorAll('table tbody tr')].find(row => row.textContent.includes('snapshot'));
      const snapshotSelect = [...document.querySelectorAll('select')].find(select => [...select.options].some(option => option.value === 'snapshot'));
      if (!row || !snapshotSelect) return null;
      Object.getOwnPropertyDescriptor(HTMLSelectElement.prototype, 'value').set.call(snapshotSelect, 'snapshot');
      snapshotSelect.dispatchEvent(new Event('change', { bubbles: true }));
      return { row: row.textContent };
    })()`);
    assert.ok(snapshotRestore, "Snapshot restore controls must be rendered");
    if (portableFailover) {
      assert.ok(snapshotRestore.row.includes("portable-tar-v1"));
      assert.ok(snapshotRestore.row.includes("target"));
      await waitFor(
        "[...document.querySelectorAll('select option')].some(option => option.textContent.includes('target-restore'))",
        "cross-Target restore profile",
      );
    }
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
  const managedAgentRuntimePrefix = `/v1/tenants/${encodeURIComponent(tenantId)}/projects/${encodeURIComponent(projectId)}/sessions`;
  const adminAPIRequests = apiRequests.filter(
    ({ path }) => path.startsWith("/v1/admin/") || path.startsWith(managedAgentRuntimePrefix),
  );
  assert.ok(adminAPIRequests.length >= 10, "Admin Web must load real Admin API resources");
  assert.ok(
    adminAPIRequests.every(
      ({ origin: value, path }) =>
        value === origin &&
        (path.startsWith("/v1/admin/") || path.startsWith(managedAgentRuntimePrefix)),
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
    const captureScript = fileURLToPath(
      new URL("./admin-visual/capture-actual.mjs", import.meta.url),
    );
    const capture = spawn(
      process.execPath,
      [captureScript, captureOutput, adminAccountFile, adminDeniedAccountFile, projectId, tenantId],
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
  const adminRequestCount = adminAPIRequests.length;
  let userRequestCount;
  if (!snapshotMode && argumentUserOrigin) {
    await logout("administrator");
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
    assert.equal(initialUserState.inputs, 2, "User Web must ask only for email and password");
    assert.ok(!/endpoint|credentialref|kubeconfig/iu.test(initialUserState.text));
    assert.ok(
      !/endpoint|credentialref|kubeconfig|forbidden\.example/iu.test(initialUserState.stored),
    );
    const userAdminStatus = await evaluate(
      `fetch(${JSON.stringify(`${argumentUserOrigin}/v1/admin/tenants/${encodeURIComponent(tenantId)}/projects/${encodeURIComponent(projectId)}/deployment-targets?pageSize=1`)}, { credentials: 'same-origin', headers: { "X-Request-ID": "user-web-admin-route-denied" } }).then(response => response.status)`,
    );
    assert.equal(userAdminStatus, 404, "User Web origin must not proxy Admin API routes");
    const adminUserStatus = await evaluate(
      `fetch(${JSON.stringify(`${argumentUserOrigin}/v1/tenants/${encodeURIComponent(tenantId)}/projects/${encodeURIComponent(projectId)}/environment-profiles?pageSize=1`)}, { credentials: 'same-origin', headers: { "X-Request-ID": "signed-out-user-api-denied" } }).then(response => response.status)`,
    );
    assert.equal(adminUserStatus, 401, "signed-out User Web must reject API access");
    await delay(50);
    errors.length = 0;
    const requestOffset = apiRequests.length;
    await login(userAccount, "user");
    await waitFor("document.querySelector('.app-shell') !== null", "connected User Web");
    await selectScope(
      'select[aria-label="Current tenant"]',
      'select[aria-label="Current project"]',
      "User",
    );
    const connectedUserState = await evaluate(
      `({ text: document.body.textContent, stored: JSON.stringify({...localStorage, ...sessionStorage}), width: document.documentElement.scrollWidth, clientWidth: document.documentElement.clientWidth })`,
    );
    assert.equal(connectedUserState.width, connectedUserState.clientWidth);
    assert.ok(!/endpoint|credentialref|kubeconfig/iu.test(connectedUserState.text));
    assert.ok(!/endpoint|credentialref|kubeconfig/iu.test(connectedUserState.stored));
    assert.ok(
      !connectedUserState.stored.includes(userAccount.password) &&
        !/bearer|authorization|session.?handle|eyJ[A-Za-z0-9_-]*\./iu.test(
          connectedUserState.stored,
        ),
      "User credentials must remain out of browser storage",
    );
    const userRequests = apiRequests.slice(requestOffset);
    assert.ok(userRequests.length >= 3, "User Web must load real User API resources");
    assert.ok(
      userRequests.every(
        ({ origin: value, path }) =>
          value === argumentUserOrigin && path.startsWith("/v1/") && !path.startsWith("/v1/admin/"),
      ),
    );
    await navigate(origin);
    await waitFor(
      "document.querySelector('.connect-form') !== null",
      "User session rejected by Admin Web",
    );
    await navigate(argumentUserOrigin);
    await waitFor("document.querySelector('.app-shell') !== null", "User session restored");
    await delay(50);
    errors.length = 0;
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
    runtimeVisible,
    portableFailoverVisible:
      process.env.CLOUD_AGENTS_FOUNDATION_CROSS_TARGET_RESTORE === "1" ? true : undefined,
    screenshot: snapshotMode ? "admin-sandbox-usage-correction.png" : undefined,
    fullCapture: fullCaptureResult,
  };
  process.stdout.write(
    snapshotMode
      ? `FOUNDATION_SNAPSHOT_BROWSER=${JSON.stringify(browserResult)}\n`
      : `User/Admin Web browser smoke passed (admin-requests=${browserResult.requests}, user-requests=${userRequestCount ?? 0}, user-admin=403, runtime=${browserResult.runtimeVisible}, locale=en-US, theme=${originalTheme}->${updatedTheme}, widths=1440/390)\n`,
  );
} finally {
  socket?.close();
  if (browser.exitCode === null) {
    browser.kill("SIGTERM");
    await new Promise((resolve) => browser.once("close", resolve));
  }
  rmSync(profile, { recursive: true, force: true });
}
