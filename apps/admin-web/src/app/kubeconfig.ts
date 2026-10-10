import type { DeploymentTargetKubernetesCredential } from "@cloud-agents/cloud-agent-platform-sdk/platform";
import { parse } from "yaml";

export const maxKubeconfigBytes = 1 << 20;
const maxCredentialDataLength = 65536;
const maxTokenLength = 16384;
const base64Pattern = /^(?:[A-Za-z0-9+/]{4})*(?:[A-Za-z0-9+/]{2}==|[A-Za-z0-9+/]{3}=)?$/u;
const tokenPattern = /^[\x21-\x7e]+$/u;

export type KubeconfigProblem =
  | "invalid"
  | "tooLarge"
  | "noContexts"
  | "contextMissing"
  | "serverInvalid"
  | "clusterUnsupported"
  | "caMissing"
  | "authUnsupported"
  | "authAmbiguous";

export type KubeconfigConnection = Readonly<{
  endpoint: string;
  authentication: "token" | "clientCertificate";
  credential: DeploymentTargetKubernetesCredential;
}>;

export type KubeconfigContext = Readonly<{
  name: string;
  server: string;
  connection: KubeconfigConnection | null;
  problem: KubeconfigProblem | null;
}>;

export type Kubeconfig = Readonly<{
  currentContext: string;
  contexts: readonly KubeconfigContext[];
}>;

type Entry = Readonly<Record<string, unknown>>;

/**
 * Parses a kubeconfig in the browser and resolves each context to the static
 * connection the Control Plane can use. Only embedded CA data and either a
 * bearer token or an embedded client certificate are accepted; file paths,
 * exec plugins, auth providers, proxies and disabled TLS verification are not.
 */
export function parseKubeconfig(text: string): Kubeconfig | KubeconfigProblem {
  if (new TextEncoder().encode(text).length > maxKubeconfigBytes) return "tooLarge";
  let document: unknown;
  try {
    document = parse(text, { maxAliasCount: 0, uniqueKeys: true });
  } catch {
    return "invalid";
  }
  if (!isEntry(document)) return "invalid";
  const clusters = namedEntries(document.clusters, "cluster");
  const users = namedEntries(document.users, "user");
  const contexts = namedEntries(document.contexts, "context");
  if (clusters === null || users === null || contexts === null) return "invalid";
  if (contexts.size === 0) return "noContexts";
  const currentContext =
    typeof document["current-context"] === "string" ? document["current-context"] : "";
  return Object.freeze({
    currentContext,
    contexts: Object.freeze(
      [...contexts].map(([name, context]) => resolveContext(name, context, clusters, users)),
    ),
  });
}

/** Picks the requested context, then current-context, then the first one. */
export function selectKubeconfigContext(
  kubeconfig: Kubeconfig,
  requested: string,
): KubeconfigContext {
  return (
    kubeconfig.contexts.find(({ name }) => name === requested) ??
    kubeconfig.contexts.find(({ name }) => name === kubeconfig.currentContext) ??
    kubeconfig.contexts[0]!
  );
}

export async function kubernetesCredentialDigest(
  credential: DeploymentTargetKubernetesCredential,
): Promise<string> {
  const bytes = new TextEncoder().encode(JSON.stringify(credential));
  const digest = new Uint8Array(await crypto.subtle.digest("SHA-256", bytes));
  return `sha256:${Array.from(digest, (byte) => byte.toString(16).padStart(2, "0")).join("")}`;
}

function resolveContext(
  name: string,
  context: Entry,
  clusters: ReadonlyMap<string, Entry>,
  users: ReadonlyMap<string, Entry>,
): KubeconfigContext {
  const cluster = typeof context.cluster === "string" ? clusters.get(context.cluster) : undefined;
  const user = typeof context.user === "string" ? users.get(context.user) : undefined;
  const server = typeof cluster?.server === "string" ? cluster.server : "";
  const failed = (problem: KubeconfigProblem) =>
    Object.freeze({ name, server, connection: null, problem });
  if (cluster === undefined || user === undefined) return failed("contextMissing");
  const endpoint = httpsEndpoint(server);
  if (endpoint === null) return failed("serverInvalid");
  if (
    cluster["insecure-skip-tls-verify"] === true ||
    present(cluster["proxy-url"]) ||
    present(cluster["tls-server-name"]) ||
    present(cluster["certificate-authority"])
  ) {
    return failed("clusterUnsupported");
  }
  const certificateAuthorityData = credentialData(cluster["certificate-authority-data"]);
  if (certificateAuthorityData === null) return failed("caMissing");
  if (
    [
      "exec",
      "auth-provider",
      "username",
      "password",
      "tokenFile",
      "client-certificate",
      "client-key",
    ].some((field) => present(user[field]))
  ) {
    return failed("authUnsupported");
  }
  const hasToken = present(user.token);
  const hasCertificate =
    present(user["client-certificate-data"]) || present(user["client-key-data"]);
  if (hasToken && hasCertificate) return failed("authAmbiguous");
  if (hasToken) {
    const token = user.token;
    if (typeof token !== "string" || token.length > maxTokenLength || !tokenPattern.test(token)) {
      return failed("authUnsupported");
    }
    return Object.freeze({
      name,
      server,
      problem: null,
      connection: Object.freeze({
        endpoint,
        authentication: "token" as const,
        credential: Object.freeze({ certificateAuthorityData, token }),
      }),
    });
  }
  const clientCertificateData = credentialData(user["client-certificate-data"]);
  const clientKeyData = credentialData(user["client-key-data"]);
  if (clientCertificateData === null || clientKeyData === null) return failed("authUnsupported");
  return Object.freeze({
    name,
    server,
    problem: null,
    connection: Object.freeze({
      endpoint,
      authentication: "clientCertificate" as const,
      credential: Object.freeze({ certificateAuthorityData, clientCertificateData, clientKeyData }),
    }),
  });
}

function namedEntries(value: unknown, field: string): Map<string, Entry> | null {
  if (value === undefined || value === null) return new Map();
  if (!Array.isArray(value)) return null;
  const entries = new Map<string, Entry>();
  for (const item of value) {
    if (!isEntry(item) || typeof item.name !== "string" || item.name === "") return null;
    const body = item[field] ?? {};
    if (!isEntry(body) || entries.has(item.name)) return null;
    entries.set(item.name, body);
  }
  return entries;
}

function httpsEndpoint(value: string): string | null {
  if (value.trim() !== value || value.length > 2048) return null;
  let url: URL;
  try {
    url = new URL(value);
  } catch {
    return null;
  }
  if (
    url.protocol !== "https:" ||
    url.hostname === "" ||
    url.username !== "" ||
    url.password !== "" ||
    url.pathname !== "/" ||
    url.search !== "" ||
    url.hash !== ""
  ) {
    return null;
  }
  return value.replace(/\/$/u, "");
}

function credentialData(value: unknown): string | null {
  return typeof value === "string" &&
    value !== "" &&
    value.length <= maxCredentialDataLength &&
    base64Pattern.test(value)
    ? value
    : null;
}

function present(value: unknown): boolean {
  return value !== undefined && value !== null && value !== "" && value !== false;
}

function isEntry(value: unknown): value is Entry {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}
