import { describe, expect, it } from "vitest";

import {
  kubernetesCredentialDigest,
  parseKubeconfig,
  type KubeconfigProblem,
} from "../../src/app/kubeconfig";
import {
  deploymentTargetRegisterRequestFrom,
  kubeconfigSelectionFrom,
  targetRegistrationForm,
} from "../../src/app/targets";

const ca = "Q0EtREFUQQ==";
const certificate = "Q0VSVA==";
const key = "S0VZ";

function kubeconfig({
  cluster = `server: https://cluster.example.test:6443/\n      certificate-authority-data: ${ca}`,
  user = "token: bearer-token",
} = {}): string {
  return `apiVersion: v1
kind: Config
current-context: token
clusters:
  - name: main
    cluster:
      ${cluster}
users:
  - name: token-user
    user:
      ${user}
  - name: cert-user
    user:
      client-certificate-data: ${certificate}
      client-key-data: ${key}
contexts:
  - name: cert
    context: { cluster: main, user: cert-user }
  - name: token
    context: { cluster: main, user: token-user }
`;
}

function kubeconfigDraft(text: string, kubeconfigContext = "") {
  return {
    ...targetRegistrationForm(),
    targetName: "Cluster A",
    targetKind: "kubernetes" as const,
    credentialRef: " kubernetes-primary ",
    kubernetesConnection: "kubeconfig" as const,
    kubeconfig: text,
    kubeconfigContext,
  };
}

describe("target registration form mapping", () => {
  it("derives the target ID from the name and trims registration fields", () => {
    expect(
      deploymentTargetRegisterRequestFrom({
        ...targetRegistrationForm(),
        targetName: " SSH Overflow ",
        targetKind: "ssh",
        endpoint: " ssh://worker.example.test:22 ",
        credentialRef: " ssh-primary ",
      }),
    ).toEqual({
      targetId: "ssh-overflow",
      targetName: "SSH Overflow",
      targetKind: "ssh",
      endpoint: "ssh://worker.example.test:22",
      credentialRef: "ssh-primary",
    });
  });

  it.each([
    ["", { token: "bearer-token" }],
    ["token", { token: "bearer-token" }],
    ["cert", { clientCertificateData: certificate, clientKeyData: key }],
  ])("sends only the selected kubeconfig context %j", (context, auth) => {
    expect(deploymentTargetRegisterRequestFrom(kubeconfigDraft(kubeconfig(), context))).toEqual({
      targetId: "cluster-a",
      targetName: "Cluster A",
      targetKind: "kubernetes",
      endpoint: "https://cluster.example.test:6443",
      credentialRef: "kubernetes-primary",
      kubernetesCredential: { certificateAuthorityData: ca, ...auth },
    });
  });

  it("does not register a kubeconfig context the Control Plane cannot use", () => {
    const text = kubeconfig({ user: "exec: { command: aws }" });
    const selection = kubeconfigSelectionFrom(kubeconfigDraft(text));
    expect(typeof selection === "object" && selection?.context.problem).toBe("authUnsupported");
    expect(deploymentTargetRegisterRequestFrom(kubeconfigDraft(text))).toBeNull();
    expect(deploymentTargetRegisterRequestFrom(kubeconfigDraft(""))).toBeNull();
  });

  it("keeps the credential out of its mutation digest", async () => {
    const digest = await kubernetesCredentialDigest({
      certificateAuthorityData: ca,
      token: "bearer-token",
    });
    expect(digest).toMatch(/^sha256:[0-9a-f]{64}$/u);
    expect(digest).not.toContain("bearer-token");
  });
});

describe("kubeconfig parsing", () => {
  const server = `certificate-authority-data: ${ca}`;
  it.each<[string, string, KubeconfigProblem]>([
    ["plain HTTP", `server: http://cluster.example.test\n      ${server}`, "serverInvalid"],
    [
      "a server path",
      `server: https://rancher.example.test/k8s/c-1\n      ${server}`,
      "serverInvalid",
    ],
    [
      "disabled TLS verification",
      `server: https://cluster.example.test\n      ${server}\n      insecure-skip-tls-verify: true`,
      "clusterUnsupported",
    ],
    [
      "a CA file",
      "server: https://cluster.example.test\n      certificate-authority: /etc/ca.crt",
      "clusterUnsupported",
    ],
    ["a missing CA", "server: https://cluster.example.test", "caMissing"],
  ])("rejects a cluster with %s", (_, cluster, problem) => {
    const selection = kubeconfigSelectionFrom(kubeconfigDraft(kubeconfig({ cluster })));
    expect(typeof selection === "object" && selection?.context.problem).toBe(problem);
  });

  it.each<[string, string, KubeconfigProblem]>([
    ["a token file", "tokenFile: /var/run/token", "authUnsupported"],
    ["basic auth", "username: admin\n      password: secret", "authUnsupported"],
    [
      "a token and a certificate",
      `token: t\n      client-certificate-data: ${certificate}`,
      "authAmbiguous",
    ],
  ])("rejects a user with %s", (_, user, problem) => {
    const selection = kubeconfigSelectionFrom(kubeconfigDraft(kubeconfig({ user })));
    expect(typeof selection === "object" && selection?.context.problem).toBe(problem);
  });

  it.each<[string, string, KubeconfigProblem]>([
    ["non-YAML", "clusters: [", "invalid"],
    ["YAML aliases", "base: &a { name: x }\ncontexts: [*a]", "invalid"],
    ["duplicate keys", "contexts: []\ncontexts: []", "invalid"],
    ["no contexts", "contexts: []", "noContexts"],
    ["oversized input", `# ${"x".repeat(1 << 20)}`, "tooLarge"],
  ])("rejects %s", (_, text, problem) => {
    expect(parseKubeconfig(text)).toBe(problem);
  });
});
