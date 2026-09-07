import assert from "node:assert/strict";
import { randomUUID } from "node:crypto";
import { execFileSync, spawnSync } from "node:child_process";
import { mkdirSync, mkdtempSync, readFileSync, readdirSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { resolve } from "node:path";
import { setTimeout as delay } from "node:timers/promises";

const [output] = process.argv.slice(2);
assert.ok(
  output,
  "usage: node scripts/test-foundation-controller-kubernetes.mjs NEW_OUTPUT_DIRECTORY",
);
const root = resolve(import.meta.dirname, "..");
const evidenceDirectory = resolve(output);
mkdirSync(evidenceDirectory, { mode: 0o700 });
const build = mkdtempSync(resolve(tmpdir(), "cloud-agents-foundation-kubernetes-"));
const id = randomUUID().slice(0, 8);
const run = `foundation-k8s-${id}`;
const targetNamespace = run;
const operatorNamespace = `${run}-ops`;
const postgresName = `${run}-postgres`;
const sandboxServerName = `${run}-opensandbox`;
const credentialRef = "kubernetes-live";
const kubeconfig = "/Users/huang/.kube/orbstack-config.yaml";
const context = "orbstack";
const managerRole = "opensandbox-manager-role";
const versionRole = `${run}-version`;
const versionBinding = `${run}-version`;
const managerBinding = `${run}-manager`;
const serverImage =
  "sandbox-registry.cn-zhangjiakou.cr.aliyuncs.com/opensandbox/server@sha256:8f8762af7565ed9c6f9dbcf009dd56727aa1fef8ce58a17f2b007b88cfe542bb";
const controllerImage =
  "sandbox-registry.cn-zhangjiakou.cr.aliyuncs.com/opensandbox/controller:v0.2.0";
const execdImage =
  "sandbox-registry.cn-zhangjiakou.cr.aliyuncs.com/opensandbox/execd@sha256:1dc98c7de10b9a73450ac75aa0f200ad7972f2c40f5225f6a8998e166b45d6dd";
const egressImage =
  "sandbox-registry.cn-zhangjiakou.cr.aliyuncs.com/opensandbox/egress@sha256:973130e01bf76e8e686e2853ebf47b21741bc8781919bb4a7cf60af09a3c6e8a";
const sandboxImage = "node@sha256:83f487e0a63425e5b4d146fb5e5be574bcbe1b7b843d3ebafdd95eaf7767a7e5";
const apiKey = randomUUID();
const label = { "cloud-agents.dev/e2e-run": run };
const currentHead = readdirSync(resolve(root, "services/control-plane/migrations/product"))
  .filter((entry) => /^\d{6}$/.test(entry))
  .sort()
  .at(-1);
assert.ok(currentHead, "product migration package is required");

const docker = (...args) =>
  execFileSync("docker", ["--context", "orbstack", ...args], {
    encoding: "utf8",
    timeout: 180_000,
  }).trim();
const kubectl = (...args) =>
  execFileSync("kubectl", ["--kubeconfig", kubeconfig, "--context", context, ...args], {
    encoding: "utf8",
    timeout: 180_000,
  }).trim();
const kubectlInput = (input, ...args) =>
  execFileSync("kubectl", ["--kubeconfig", kubeconfig, "--context", context, ...args], {
    encoding: "utf8",
    input,
    timeout: 180_000,
  }).trim();
const psql = (query, user = "postgres", database = "foundation_live") =>
  execFileSync(
    "docker",
    [
      "--context",
      "orbstack",
      "exec",
      "-i",
      postgresName,
      "psql",
      "-XAt",
      "-v",
      "ON_ERROR_STOP=1",
      "-U",
      user,
      "-d",
      database,
    ],
    { encoding: "utf8", input: query, timeout: 120_000 },
  ).trim();
const mappedPort = (name, port) => {
  const address = docker("port", name, `${port}/tcp`).split("\n")[0];
  return Number(address.slice(address.lastIndexOf(":") + 1));
};
const parseMarker = (value, marker) => {
  const match = value.match(new RegExp(`${marker}=(\\{[^\\n]+\\})`));
  assert.ok(match, `${marker} missing from ${value}`);
  return JSON.parse(match[1]);
};
const absent = (...args) =>
  spawnSync("kubectl", ["--kubeconfig", kubeconfig, "--context", context, ...args], {
    encoding: "utf8",
    timeout: 30_000,
  }).status !== 0;

let postgresStarted = false;
let sandboxServerStarted = false;
let kubernetesCreated = false;
try {
  const manager = JSON.parse(kubectl("get", "clusterrole", managerRole, "-o", "json"));
  assert.equal(manager.metadata.labels["app.kubernetes.io/version"], "0.2.0");
  const crd = JSON.parse(
    kubectl("get", "crd", "batchsandboxes.sandbox.opensandbox.io", "-o", "json"),
  );
  assert.equal(crd.metadata.labels["app.kubernetes.io/version"], "0.2.0");
  const cluster = JSON.parse(kubectl("config", "view", "--raw", "--minify", "-o", "json"));
  const targetEndpoint = cluster.clusters[0].cluster.server;
  const caData = cluster.clusters[0].cluster["certificate-authority-data"];
  assert.match(targetEndpoint, /^https:\/\//u);
  assert.ok(caData);
  for (const image of ["postgres:17.6-bookworm", serverImage]) docker("image", "inspect", image);

  const resources = {
    apiVersion: "v1",
    kind: "List",
    items: [
      {
        apiVersion: "v1",
        kind: "Namespace",
        metadata: { name: targetNamespace, labels: label },
      },
      {
        apiVersion: "v1",
        kind: "Namespace",
        metadata: { name: operatorNamespace, labels: label },
      },
      ...["control-plane", "opensandbox-server"].map((name) => ({
        apiVersion: "v1",
        kind: "ServiceAccount",
        metadata: { name, namespace: targetNamespace, labels: label },
      })),
      {
        apiVersion: "v1",
        kind: "ServiceAccount",
        metadata: { name: "opensandbox-controller", namespace: operatorNamespace, labels: label },
      },
      {
        apiVersion: "rbac.authorization.k8s.io/v1",
        kind: "Role",
        metadata: { name: "foundation-workspace", namespace: targetNamespace, labels: label },
        rules: [
          {
            apiGroups: [""],
            resources: ["persistentvolumeclaims"],
            verbs: ["create", "get", "list", "watch"],
          },
        ],
      },
      {
        apiVersion: "rbac.authorization.k8s.io/v1",
        kind: "RoleBinding",
        metadata: { name: "foundation-workspace", namespace: targetNamespace, labels: label },
        roleRef: {
          apiGroup: "rbac.authorization.k8s.io",
          kind: "Role",
          name: "foundation-workspace",
        },
        subjects: ["control-plane", "opensandbox-server"].map((name) => ({
          kind: "ServiceAccount",
          name,
          namespace: targetNamespace,
        })),
      },
      {
        apiVersion: "rbac.authorization.k8s.io/v1",
        kind: "ClusterRole",
        metadata: { name: versionRole, labels: label },
        rules: [{ nonResourceURLs: ["/version"], verbs: ["get"] }],
      },
      {
        apiVersion: "rbac.authorization.k8s.io/v1",
        kind: "ClusterRoleBinding",
        metadata: { name: versionBinding, labels: label },
        roleRef: { apiGroup: "rbac.authorization.k8s.io", kind: "ClusterRole", name: versionRole },
        subjects: [
          {
            kind: "ServiceAccount",
            name: "control-plane",
            namespace: targetNamespace,
          },
        ],
      },
      {
        apiVersion: "rbac.authorization.k8s.io/v1",
        kind: "ClusterRoleBinding",
        metadata: { name: managerBinding, labels: label },
        roleRef: { apiGroup: "rbac.authorization.k8s.io", kind: "ClusterRole", name: managerRole },
        subjects: [
          {
            kind: "ServiceAccount",
            name: "opensandbox-server",
            namespace: targetNamespace,
          },
          {
            kind: "ServiceAccount",
            name: "opensandbox-controller",
            namespace: operatorNamespace,
          },
        ],
      },
      {
        apiVersion: "apps/v1",
        kind: "Deployment",
        metadata: { name: "opensandbox-controller", namespace: operatorNamespace, labels: label },
        spec: {
          replicas: 1,
          selector: { matchLabels: label },
          template: {
            metadata: { labels: label },
            spec: {
              serviceAccountName: "opensandbox-controller",
              securityContext: { runAsNonRoot: true, seccompProfile: { type: "RuntimeDefault" } },
              containers: [
                {
                  name: "manager",
                  image: controllerImage,
                  imagePullPolicy: "IfNotPresent",
                  command: ["/workspace/server"],
                  args: [
                    "--health-probe-bind-address=:8081",
                    "--zap-log-level=info",
                    "--kube-client-qps=100",
                    "--kube-client-burst=200",
                  ],
                  securityContext: {
                    allowPrivilegeEscalation: false,
                    capabilities: { drop: ["ALL"] },
                  },
                  resources: {
                    requests: { cpu: "10m", memory: "64Mi" },
                    limits: { cpu: "500m", memory: "128Mi" },
                  },
                },
              ],
            },
          },
        },
      },
    ],
  };
  kubectlInput(JSON.stringify(resources), "apply", "-f", "-");
  kubernetesCreated = true;
  kubectl(
    "rollout",
    "status",
    "deployment/opensandbox-controller",
    "-n",
    operatorNamespace,
    "--timeout=180s",
  );
  const controlPlaneToken = kubectl(
    "create",
    "token",
    "control-plane",
    "-n",
    targetNamespace,
    "--duration=1h",
  );
  const serverToken = kubectl(
    "create",
    "token",
    "opensandbox-server",
    "-n",
    targetNamespace,
    "--duration=1h",
  );

  docker(
    "run",
    "-d",
    "--name",
    postgresName,
    "--label",
    `cloud-agents-foundation-kubernetes-test=${run}`,
    "-p",
    "127.0.0.1::5432",
    "-e",
    "POSTGRES_HOST_AUTH_METHOD=trust",
    "postgres:17.6-bookworm",
  );
  postgresStarted = true;
  for (let attempt = 0; ; attempt++) {
    try {
      assert.equal(psql("SELECT 1", "postgres", "postgres"), "1");
      await delay(100);
      assert.equal(psql("SELECT 1", "postgres", "postgres"), "1");
      break;
    } catch {
      if (attempt === 100) throw new Error("PostgreSQL did not start");
      await delay(100);
    }
  }
  const postgresPort = mappedPort(postgresName, 5432);
  const migrationURL = `postgres://foundation_migration@127.0.0.1:${postgresPort}/foundation_live?sslmode=disable`;
  const runtimeURL = `postgres://foundation_runtime@127.0.0.1:${postgresPort}/foundation_live?sslmode=disable`;
  execFileSync(
    "docker",
    [
      "--context",
      "orbstack",
      "exec",
      "-i",
      postgresName,
      "psql",
      "-XAt",
      "-v",
      "ON_ERROR_STOP=1",
      "-U",
      "postgres",
      "-d",
      "postgres",
    ],
    {
      input: readFileSync(resolve(root, "services/control-plane/migrations/bootstrap/roles.sql")),
      timeout: 120_000,
    },
  );
  psql(
    `CREATE DATABASE foundation_live;
CREATE ROLE foundation_migration LOGIN NOINHERIT NOSUPERUSER NOCREATEDB NOCREATEROLE NOREPLICATION NOBYPASSRLS;
CREATE ROLE foundation_runtime LOGIN INHERIT NOSUPERUSER NOCREATEDB NOCREATEROLE NOREPLICATION NOBYPASSRLS;
CREATE ROLE foundation_bootstrap LOGIN INHERIT NOSUPERUSER NOCREATEDB NOCREATEROLE NOREPLICATION NOBYPASSRLS;
GRANT cloud_agents_migration_owner TO foundation_migration;
GRANT cloud_agents_runtime TO foundation_runtime;
GRANT cloud_agents_bootstrap_admin TO foundation_bootstrap;
GRANT CREATE ON DATABASE foundation_live TO cloud_agents_migration_owner;`,
    "postgres",
    "postgres",
  );

  const migrateBinary = resolve(build, "cloud-agents-product-migrate");
  const serverTestBinary = resolve(build, "server.test");
  execFileSync(
    "go",
    [
      "build",
      "-trimpath",
      "-o",
      migrateBinary,
      "./services/control-plane/cmd/cloud-agents-product-migrate",
    ],
    {
      cwd: root,
      env: { ...process.env, GOTOOLCHAIN: "local", GOFLAGS: "-mod=readonly" },
      timeout: 180_000,
    },
  );
  execFileSync(
    "go",
    ["test", "-c", "-o", serverTestBinary, "./services/control-plane/internal/server"],
    {
      cwd: root,
      env: { ...process.env, GOTOOLCHAIN: "local", GOFLAGS: "-mod=readonly" },
      timeout: 180_000,
    },
  );
  const migration = JSON.parse(
    execFileSync(
      migrateBinary,
      [
        "--repository-root",
        root,
        "--manifest",
        `services/control-plane/migrations/product/${currentHead}/manifest.json`,
        "--selector",
        `product-${currentHead}`,
      ],
      {
        encoding: "utf8",
        env: { ...process.env, CLOUD_AGENTS_PLATFORM_DATABASE_URL: migrationURL },
        timeout: 180_000,
      },
    ),
  );
  assert.equal(migration.schema_head, currentHead);
  psql(
    `SELECT * FROM cloud_agents.bootstrap_tenant_administrator_v1(
      'tenant','tenant','Foundation Kubernetes tenant','organization','organization','Foundation Kubernetes organization',
      'user','https://foundation.test','foundation-admin','membership','membership','binding','binding',
      'tenant-audit','membership-audit','binding-audit','foundation-kubernetes');`,
    "foundation_bootstrap",
  );
  psql(
    `SET ROLE cloud_agents_migration_owner;
WITH next_revision AS (
  UPDATE cloud_agents.tenant_resource_versions SET current_revision=current_revision+1,updated_at=clock_timestamp()
  WHERE tenant_id='tenant' AND tenant_uid='tenant' RETURNING current_revision
), project_change AS (
  INSERT INTO cloud_agents.resource_changes (tenant_id,tenant_uid,resource_version,resource_kind,resource_uid,change_kind,actor_database_principal,occurred_at)
  SELECT 'tenant','tenant',current_revision,'project','project','created','foundation-kubernetes',clock_timestamp()
  FROM next_revision RETURNING resource_version
) INSERT INTO cloud_agents.projects (tenant_id,tenant_ref_id,project_uid,project_name,organization_uid,display_name,state,resource_version,created_at,updated_at)
SELECT 'tenant','tenant','project','project','organization','Foundation Kubernetes project','active',resource_version,clock_timestamp(),clock_timestamp() FROM project_change;`,
    "foundation_migration",
  );

  const serverKubeconfigPath = resolve(build, "opensandbox-kubeconfig.json");
  writeFileSync(
    serverKubeconfigPath,
    JSON.stringify({
      apiVersion: "v1",
      kind: "Config",
      clusters: [
        {
          name: "target",
          cluster: { server: targetEndpoint, "certificate-authority-data": caData },
        },
      ],
      users: [{ name: "server", user: { token: serverToken } }],
      contexts: [
        {
          name: "target",
          context: { cluster: "target", user: "server", namespace: targetNamespace },
        },
      ],
      "current-context": "target",
    }) + "\n",
    { mode: 0o600 },
  );
  const configPath = resolve(build, "opensandbox.toml");
  const templatePath = resolve(build, "batchsandbox-template.yaml");
  writeFileSync(
    configPath,
    `[server]\nhost="0.0.0.0"\nport=8080\napi_key="${apiKey}"\nmax_sandbox_timeout_seconds=86400\n[log]\nlevel="INFO"\n[runtime]\ntype="kubernetes"\nexecd_image="${execdImage}"\n[storage]\nallowed_host_paths=[]\nvolume_default_size="20Gi"\n[kubernetes]\nkubeconfig_path="/run/kube/config"\nnamespace="${targetNamespace}"\ninformer_enabled=false\nworkload_provider="batchsandbox"\nimage_pull_policy="IfNotPresent"\nsandbox_create_timeout_seconds=180\nbatchsandbox_template_file="/etc/opensandbox/batchsandbox-template.yaml"\n[ingress]\nmode="direct"\n[egress]\nimage="${egressImage}"\nmode="dns+nft"\n[renew_intent]\nenabled=false\nredis.enabled=false\n[store]\ntype="sqlite"\npath="/tmp/opensandbox.db"\n`,
    { mode: 0o600 },
  );
  writeFileSync(
    templatePath,
    "metadata:\nspec:\n  replicas: 1\n  template:\n    spec:\n      restartPolicy: Never\n",
    { mode: 0o600 },
  );
  docker(
    "run",
    "-d",
    "--name",
    sandboxServerName,
    "--label",
    `cloud-agents-foundation-kubernetes-test=${run}`,
    "-p",
    "127.0.0.1::8080",
    "--mount",
    `type=bind,src=${configPath},dst=/etc/opensandbox/config.toml,readonly`,
    "--mount",
    `type=bind,src=${templatePath},dst=/etc/opensandbox/batchsandbox-template.yaml,readonly`,
    "--mount",
    `type=bind,src=${serverKubeconfigPath},dst=/run/kube/config,readonly`,
    serverImage,
  );
  sandboxServerStarted = true;
  const sandboxBase = `http://127.0.0.1:${mappedPort(sandboxServerName, 8080)}`;
  for (let attempt = 0; ; attempt++) {
    try {
      if ((await fetch(sandboxBase + "/health", { signal: AbortSignal.timeout(1000) })).ok) break;
    } catch {}
    if (attempt === 300) throw new Error("OpenSandbox did not start");
    await delay(100);
  }

  const credentialDirectory = resolve(build, "credentials");
  mkdirSync(credentialDirectory, { mode: 0o700 });
  writeFileSync(
    resolve(credentialDirectory, `${credentialRef}.ca.crt`),
    Buffer.from(caData, "base64"),
    {
      mode: 0o600,
    },
  );
  writeFileSync(resolve(credentialDirectory, `${credentialRef}.token`), controlPlaneToken + "\n", {
    mode: 0o600,
  });
  writeFileSync(
    resolve(credentialDirectory, `${credentialRef}.foundation.json`),
    JSON.stringify({ namespace: targetNamespace }) + "\n",
    { mode: 0o600 },
  );
  writeFileSync(
    resolve(credentialDirectory, `${credentialRef}.opensandbox.json`),
    JSON.stringify({ endpoint: sandboxBase, apiKey }) + "\n",
    { mode: 0o600 },
  );

  const testOutput = execFileSync(
    serverTestBinary,
    ["-test.run", "^TestFoundationKubernetesLifecycle$", "-test.v"],
    {
      encoding: "utf8",
      env: {
        ...process.env,
        CLOUD_AGENTS_FOUNDATION_KUBERNETES_RUNTIME_DATABASE_URL: runtimeURL,
        CLOUD_AGENTS_FOUNDATION_KUBERNETES_OWNER_DATABASE_URL: migrationURL,
        CLOUD_AGENTS_FOUNDATION_KUBERNETES_CREDENTIAL_DIRECTORY: credentialDirectory,
        CLOUD_AGENTS_FOUNDATION_KUBERNETES_TARGET_ENDPOINT: targetEndpoint,
        CLOUD_AGENTS_FOUNDATION_KUBERNETES_CREDENTIAL_REF: credentialRef,
        CLOUD_AGENTS_FOUNDATION_KUBERNETES_IMAGE_URI: sandboxImage,
      },
      timeout: 480_000,
    },
  );
  const receipt = parseMarker(testOutput, "FOUNDATION_KUBERNETES");
  assert.equal(receipt.ordinaryUserAdminStatus, 403);
  assert.equal(receipt.staleGenerationStatus, 409);
  assert.equal(receipt.adminExecStatus, 403);
  assert.equal(receipt.workspaceRetained, true);
  assert.equal(receipt.finalObservedState, "stopped");
  assert.equal(receipt.finalCleanupPhase, "complete");
  assert.notEqual(receipt.firstRuntimeId, receipt.rebuiltRuntimeId);

  for (let attempt = 0; ; attempt++) {
    const sandboxes = JSON.parse(
      kubectl("get", "batchsandboxes.sandbox.opensandbox.io", "-n", targetNamespace, "-o", "json"),
    );
    const pods = JSON.parse(kubectl("get", "pods", "-n", targetNamespace, "-o", "json"));
    if (sandboxes.items.length === 0 && pods.items.length === 0) break;
    if (attempt === 120) throw new Error("stopped Kubernetes runtime left workload residue");
    await delay(500);
  }
  const pvc = JSON.parse(
    kubectl("get", "pvc", receipt.workspaceVolume, "-n", targetNamespace, "-o", "json"),
  );
  assert.equal(pvc.metadata.labels["cloud-agents.dev/resource"], "foundation-workspace");
  assert.equal(pvc.metadata.annotations["cloud-agents.dev/workspace"], "workspace");
  const controllerPod = JSON.parse(
    kubectl(
      "get",
      "pods",
      "-n",
      operatorNamespace,
      "-l",
      `cloud-agents.dev/e2e-run=${run}`,
      "-o",
      "json",
    ),
  ).items[0];
  assert.ok(controllerPod.status.containerStatuses[0].imageID);
  const serverImageID = docker("inspect", "--format", "{{.Image}}", sandboxServerName);
  const evidence = {
    run,
    source: {
      branch: execFileSync("git", ["branch", "--show-current"], {
        cwd: root,
        encoding: "utf8",
      }).trim(),
      head: execFileSync("git", ["rev-parse", "HEAD"], { cwd: root, encoding: "utf8" }).trim(),
      dirty: true,
    },
    backend: {
      kubernetesContext: context,
      kubernetesVersion: kubectl("version", "-o", "json"),
      targetNamespace,
      postgres: psql("SHOW server_version;"),
      schemaHead: migration.schema_head,
      openSandboxSource: "server/v0.2.2 (207d94c7dc7735c143856fe5c6538b743e478786)",
      openSandboxServerImage: serverImage,
      openSandboxServerImageID: serverImageID,
      openSandboxControllerImage: controllerImage,
      openSandboxControllerImageID: controllerPod.status.containerStatuses[0].imageID,
      execdImage,
      egressImage,
      sandboxImage,
    },
    receipt,
    checks: [
      "generated Admin API registers and probes the real Kubernetes target while an ordinary user receives 403",
      "generated Admin API publishes a target-bound RuntimeProfile and generated User API exposes only its redacted summary",
      "generated User API creates the Sandbox and the Control Plane controller creates the owned PVC before OpenSandbox",
      "pinned OpenSandbox server and controller create a real BatchSandbox workload with the pre-created retained PVC",
      "generated User Exec writes a proof, rebuilds onto the same PVC, and verifies the exact digest",
      "generation fences reject stale Exec and Admin cannot use the user content API",
      "Admin stop reaches cleanup complete with zero BatchSandbox and Pod residue while the retained PVC remains owner-bound",
      "the harness deletes only its UUID-named namespaces and RBAC resources after collecting evidence",
    ],
    boundary:
      "Local OrbStack Kubernetes and disposable PostgreSQL only; this proves explicit-target Kubernetes Foundation lifecycle, not Region/Pool placement, capability admission, external Kubernetes, SSH, Codex or Claude Code E2E",
  };
  writeFileSync(
    resolve(evidenceDirectory, "evidence.json"),
    JSON.stringify(evidence, null, 2) + "\n",
  );
  writeFileSync(resolve(evidenceDirectory, "server-test.log"), testOutput);
  writeFileSync(
    resolve(evidenceDirectory, "opensandbox-server.log"),
    docker("logs", sandboxServerName) + "\n",
  );
  writeFileSync(
    resolve(evidenceDirectory, "opensandbox-controller.log"),
    kubectl("logs", "deployment/opensandbox-controller", "-n", operatorNamespace) + "\n",
  );
  process.stdout.write(
    `Verified Kubernetes Foundation lifecycle; evidence ${evidenceDirectory}/evidence.json\n`,
  );
} catch (error) {
  if (sandboxServerStarted) {
    try {
      writeFileSync(
        resolve(evidenceDirectory, "opensandbox-server.failure.log"),
        docker("logs", sandboxServerName) + "\n",
      );
    } catch {}
  }
  if (kubernetesCreated) {
    try {
      writeFileSync(
        resolve(evidenceDirectory, "kubernetes.failure.json"),
        kubectl(
          "get",
          "pods,batchsandboxes.sandbox.opensandbox.io,pvc,events",
          "-n",
          targetNamespace,
          "-o",
          "json",
        ) + "\n",
      );
      writeFileSync(
        resolve(evidenceDirectory, "opensandbox-controller.failure.log"),
        kubectl("logs", "deployment/opensandbox-controller", "-n", operatorNamespace) + "\n",
      );
    } catch {}
  }
  throw error;
} finally {
  if (sandboxServerStarted) {
    try {
      docker("rm", "-f", sandboxServerName);
    } catch {}
  }
  if (postgresStarted) {
    try {
      docker("rm", "-f", postgresName);
    } catch {}
  }
  if (kubernetesCreated) {
    let cleanupError;
    for (let attempt = 0; attempt < 5; attempt++) {
      try {
        kubectl(
          "delete",
          "clusterrolebinding",
          managerBinding,
          versionBinding,
          "--ignore-not-found",
          "--wait=true",
        );
        kubectl("delete", "clusterrole", versionRole, "--ignore-not-found", "--wait=true");
        kubectl(
          "delete",
          "namespace",
          targetNamespace,
          operatorNamespace,
          "--ignore-not-found",
          "--wait=true",
          "--timeout=180s",
        );
        assert.ok(absent("get", "namespace", targetNamespace));
        assert.ok(absent("get", "namespace", operatorNamespace));
        assert.ok(absent("get", "clusterrolebinding", managerBinding));
        assert.ok(absent("get", "clusterrolebinding", versionBinding));
        assert.ok(absent("get", "clusterrole", versionRole));
        cleanupError = undefined;
        break;
      } catch (error) {
        cleanupError = error;
        await delay(500);
      }
    }
    if (cleanupError) {
      process.stderr.write(`Kubernetes cleanup failed: ${cleanupError.message}\n`);
      process.exitCode = 1;
    }
  }
  rmSync(build, { recursive: true, force: true });
}
