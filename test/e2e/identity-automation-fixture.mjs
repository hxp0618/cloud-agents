import { chmodSync, lstatSync, readFileSync, renameSync, statSync, writeFileSync } from "node:fs";

const identifier = /^[A-Za-z0-9](?:[A-Za-z0-9._~-]{0,126}[A-Za-z0-9])?$/u;

function privateText(path, label) {
  if ((statSync(path).mode & 0o077) !== 0) throw new Error(`${label} must be private`);
  const value = readFileSync(path, "utf8").trimEnd();
  if (!value || /[\r\n]/u.test(value)) throw new Error(`${label} is invalid`);
  return value;
}

function privateAccount(path) {
  const value = JSON.parse(privateText(path, "account fixture"));
  if (
    Object.keys(value).sort().join(",") !== "email,password" ||
    typeof value.email !== "string" ||
    !/^[^@\s]+@[^@\s]+$/u.test(value.email) ||
    typeof value.password !== "string" ||
    value.password.length < 15 ||
    value.password.length > 128
  ) {
    throw new Error("account fixture is invalid");
  }
  return value;
}

function fixedHTTPSOrigin(value) {
  const url = new URL(value);
  if (
    url.protocol !== "https:" ||
    url.username ||
    url.password ||
    url.pathname !== "/" ||
    url.search ||
    url.hash ||
    url.origin !== value
  ) {
    throw new Error("Web origin must be a fixed HTTPS origin");
  }
  return url;
}

async function responseBody(response) {
  const body = await response.text();
  return { body, value: body ? JSON.parse(body) : undefined };
}

function expectStatus(response, expected, label) {
  if (response.status !== expected) throw new Error(`${label} returned HTTP ${response.status}`);
}

function requireSecret(value, expression, label) {
  if (typeof value !== "string" || !expression.test(value)) throw new Error(`${label} is invalid`);
  return value;
}

function writePrivate(path, value) {
  try {
    const current = lstatSync(path);
    if (!current.isFile() || (current.mode & 0o077) !== 0) throw new Error("output secret is unsafe");
  } catch (error) {
    if (error?.code !== "ENOENT") throw error;
  }
  const temporary = `${path}.tmp-${process.pid}-${crypto.randomUUID()}`;
  writeFileSync(temporary, value, { mode: 0o600, flag: "wx" });
  chmodSync(temporary, 0o600);
  renameSync(temporary, path);
}

async function exchange(origin, credentialFile, tenantId, outputPrefix, projectId) {
  const credential = privateText(credentialFile, "automation credential");
  requireSecret(credential, /^[A-Za-z0-9_-]{43}$/u, "automation credential");
  const response = await fetch(new URL("/v1/auth/automation/tenant-token", origin), {
    method: "POST",
    headers: {
      Authorization: `Bearer ${credential}`,
      "Content-Type": "application/json",
      "X-Request-ID": `automation-exchange-${crypto.randomUUID()}`,
    },
    body: JSON.stringify({ tenantId, ...(projectId ? { projectId } : {}) }),
  });
  const result = await responseBody(response);
  expectStatus(response, 200, "automation token exchange");
  requireSecret(result.value?.accessToken, /^[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+$/u, "automation access token");
  writePrivate(`${outputPrefix}.token`, `${result.value.accessToken}\n`);
  writePrivate(
    `${outputPrefix}.curl.conf`,
    `header = "Authorization: Bearer ${result.value.accessToken}"\n`,
  );
}

async function create(args) {
  const [originValue, accountFile, tenantId, serviceAccountId, application, roleName, scopeLevel, scopeId, credentialFile] = args;
  if (!identifier.test(tenantId) || !identifier.test(serviceAccountId) || !identifier.test(roleName) || !identifier.test(scopeId))
    throw new Error("service-account fixture identifiers are invalid");
  if (!["admin", "user"].includes(application) || !["tenant", "organization", "project"].includes(scopeLevel))
    throw new Error("service-account fixture authority is invalid");
  const origin = fixedHTTPSOrigin(originValue);
  const login = await fetch(new URL("/v1/identity/login/password", origin), {
    method: "POST",
    headers: { Origin: origin.origin, "Content-Type": "application/json", "X-Request-ID": `automation-login-${crypto.randomUUID()}` },
    body: JSON.stringify(privateAccount(accountFile)),
  });
  const loginResult = await responseBody(login);
  expectStatus(login, 200, "administrator login");
  const cookie = login.headers.get("set-cookie")?.split(";", 1)[0] ?? "";
  requireSecret(cookie, /^__Host-[^=]+=[A-Za-z0-9_-]{43}$/u, "administrator session cookie");
  requireSecret(loginResult.value?.csrfToken, /^[A-Za-z0-9_-]{43}$/u, "administrator CSRF proof");
  const created = await fetch(
    new URL(`/v1/admin/tenants/${encodeURIComponent(tenantId)}/service-accounts`, origin),
    {
      method: "POST",
      headers: {
        Cookie: cookie,
        Origin: origin.origin,
        "Content-Type": "application/json",
        "X-CSRF-Token": loginResult.value.csrfToken,
        "X-Request-ID": `automation-create-${crypto.randomUUID()}`,
      },
      body: JSON.stringify({ serviceAccountId, displayName: serviceAccountId, application, roleName, scopeLevel, scopeId }),
    },
  );
  const createdResult = await responseBody(created);
  expectStatus(created, 201, "service-account creation");
  if (createdResult.value?.serviceAccount?.id !== serviceAccountId) {
    throw new Error("service-account creation returned the wrong identifier");
  }
  requireSecret(createdResult.value?.credential, /^[A-Za-z0-9_-]{43}$/u, "service-account credential");
  writePrivate(credentialFile, `${createdResult.value.credential}\n`);
}

const [command, ...args] = process.argv.slice(2);
if (command === "create" && args.length === 9) {
  await create(args);
} else if (command === "exchange" && (args.length === 4 || args.length === 5)) {
  const [origin, credentialFile, tenantId, outputPrefix, projectId = ""] = args;
  if (!identifier.test(tenantId) || (projectId && !identifier.test(projectId)))
    throw new Error("token scope is invalid");
  await exchange(fixedHTTPSOrigin(origin), credentialFile, tenantId, outputPrefix, projectId);
} else {
  throw new Error("usage: identity-automation-fixture.mjs create ADMIN_ORIGIN ADMIN_ACCOUNT TENANT ACCOUNT_ID APPLICATION ROLE SCOPE_LEVEL SCOPE_ID CREDENTIAL_FILE | exchange APPLICATION_ORIGIN CREDENTIAL TENANT OUTPUT_PREFIX [PROJECT]");
}
