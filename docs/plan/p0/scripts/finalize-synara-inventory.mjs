#!/usr/bin/env node

import { createHash } from "node:crypto";
import { readFileSync, writeFileSync } from "node:fs";
import { basename, dirname, join, relative } from "node:path";
import { spawnSync } from "node:child_process";

const expectedSourceHead = "2c50b1eb54ed3228719bb55cc8bdcd1b0babc8e0";
const expectedSourceTree = "ba41fc168ea65978b1f17fdb8abc5afbc22ca9cc";
const expectedInventorySha256 = "bee237da890f4f3d62fd524fd11142a6b6c883e82790e5d455c415461ae7b4e5";
const expectedInventoryRows = 8625;
const expectedManualRows = 355;
const reviewedAt = "2026-08-10";
const secretTriageReference = "docs/plan/p0/provenance/synara-extraction-secret-triage.json";
const sourceLicenseProvenance =
  "MIT@2c50b1eb54ed3228719bb55cc8bdcd1b0babc8e0:LICENSE#blob=960499447d8ea8f6ce86017893f132f0c3885fef;sha256=305724dd050ca7ded99c662de813d755bc4ec3887c4543a37159c6662ca36d1b";

const sourceRoot =
  process.env.SYNARA_P0_SOURCE ??
  "/Users/huang/devel/project/huang/business/synara-cloud-agent-external-runtime";
const outputRoot = process.env.CLOUD_AGENTS_P0_OUTPUT ?? join(process.cwd(), "docs/plan/p0");
const inventoryPath =
  process.env.SYNARA_P0_INVENTORY ?? join(outputRoot, "synara-file-inventory.tsv");
const decisionsPath = join(outputRoot, "synara-inventory-decisions.tsv");
const summaryPath = join(outputRoot, "inventory-decision-summary.md");

const allowedClassifications = new Set([
  "move",
  "rewrite-public",
  "adapter",
  "synara-only",
  "retire",
  "deferred-public-extension",
]);
const allowedOwners = new Set([
  "public-core",
  "public-platform-adapter",
  "synara-host",
  "runtime-release",
  "deferred",
]);

function sha256(value) {
  return createHash("sha256").update(value).digest("hex");
}

function runGit(...args) {
  const result = spawnSync("git", ["-C", sourceRoot, ...args], {
    encoding: "utf8",
    maxBuffer: 64 * 1024 * 1024,
    env: { ...process.env, LC_ALL: "C" },
  });
  if (result.status !== 0) {
    throw new Error(`git ${args.join(" ")} failed: ${(result.stderr ?? "").trim()}`);
  }
  return result.stdout ?? "";
}

function runGitBuffer(...args) {
  const result = spawnSync("git", ["-C", sourceRoot, ...args], {
    encoding: null,
    maxBuffer: 64 * 1024 * 1024,
    env: { ...process.env, LC_ALL: "C" },
  });
  if (result.status !== 0) {
    throw new Error(
      `git ${args.join(" ")} failed: ${Buffer.from(result.stderr ?? [])
        .toString("utf8")
        .trim()}`,
    );
  }
  return Buffer.from(result.stdout ?? []);
}

function parseTSV(encoded) {
  const lines = encoded.trimEnd().split("\n");
  if (lines.length < 2) throw new Error("inventory is empty");
  const headers = lines[0].split("\t");
  const rows = lines.slice(1).map((line, rowIndex) => {
    const values = line.split("\t");
    if (values.length !== headers.length) {
      throw new Error(
        `inventory row ${rowIndex + 2} has ${values.length} columns; expected ${headers.length}`,
      );
    }
    return Object.fromEntries(headers.map((header, index) => [header, values[index]]));
  });
  return { headers, rows };
}

function tsv(value) {
  const normalized = String(value ?? "");
  if (!normalized.trim() || normalized === "-" || /unknown/i.test(normalized)) {
    throw new Error(`invalid empty/unknown decision field: ${JSON.stringify(value)}`);
  }
  return normalized.replaceAll("\t", " ").replaceAll("\r", " ").replaceAll("\n", " ");
}

function publicDecision(finalClassification, owner, target, reason) {
  return { finalClassification, owner, target, reason };
}

function retainedInSynara(path, reason) {
  return publicDecision("synara-only", "synara-host", `hxp0618/synara:${path}`, reason);
}

function secretProvenanceFor(path, publicCandidate) {
  if (!publicCandidate) return "not-imported; fixed source hash retained only";
  if (path === "scripts/stage3-provider-acceptance/test_vault_audit_acceptance_sink.py") {
    return `REWRITE_REQUIRED_BEFORE_PUBLICATION: static test private-key bytes; ${secretTriageReference}`;
  }
  return `AUDITED_EXACT_FINDINGS: ${secretTriageReference}`;
}

function targetWithSameName(root, path) {
  return `${root}/${basename(path)}`;
}

function agentdDecision(path) {
  const file = basename(path);

  const adapterRules = [
    {
      pattern: /^cocoon_supervisor_attestation(?:_test)?\.go$/,
      root: "services/worker/adapters/cocoon",
      reason:
        "Cocoon attestation is a built-in isolation side-effect implementation, not Worker authority; port behind the public adapter boundary.",
    },
    {
      pattern: /^gvisor_runtime(?:_test)?\.go$/,
      root: "services/worker/adapters/container/gvisor",
      reason:
        "gVisor launch policy is a container isolation adapter; preserve behavior while removing Synara-private configuration and types.",
    },
    {
      pattern: /^kubernetes_(?:network_boundary|registration_token)(?:_test)?\.go$/,
      root: "services/worker/adapters/kubernetes",
      reason:
        "Kubernetes network/registration operations are public built-in adapter effects and must return fenced observations to Worker core.",
    },
    {
      pattern: /^(?:git_ssh_agent(?:_test)?|workspace_ssh_test)\.go$/,
      root: "services/worker/adapters/ssh",
      reason:
        "SSH agent handling is an optional workspace credential actuator; port behind the public SSH adapter boundary.",
    },
  ];
  for (const rule of adapterRules) {
    if (rule.pattern.test(file)) {
      return publicDecision(
        "adapter",
        "public-platform-adapter",
        targetWithSameName(rule.root, path),
        rule.reason,
      );
    }
  }

  const coreRules = [
    {
      pattern: /^artifact_source(?:_secret_guard_test)?\.go$/,
      root: "services/worker/internal/artifact",
      reason:
        "Bounded artifact source handling is Worker core, but it imports Synara secret guards and must be rewritten against public primitives.",
    },
    {
      pattern: /^checkpoint(?:_patch|_test)?\.go$/,
      root: "services/worker/internal/checkpoint",
      reason:
        "Checkpoint capture is managed-agent Worker core; rewrite Synara execution/artifact models to public SDK contracts.",
    },
    {
      pattern: /^client(?:_.+_test|_test)?\.go$/,
      root: "services/worker/internal/controlplane",
      reason:
        "The agentd HTTP client becomes the public Worker-to-Control-Plane client and must be rewritten around generated Worker SDK types and fencing.",
    },
    {
      pattern: /^(?:config|models)(?:_test)?\.go$/,
      root: "services/worker/internal/config",
      reason:
        "Worker configuration/models currently embed Synara packages and environment identity; rewrite to public contracts and neutral CLOUD_AGENT names.",
    },
    {
      pattern: /^credential_(?:access_monitor_test|grants(?:_test)?)\.go$/,
      root: "services/worker/internal/credential",
      reason:
        "Credential grant lifecycle is public Worker core; rewrite Synara execution descriptors to public generation-fenced SDK contracts.",
    },
    {
      pattern:
        /^(?:package_registry_credentials|provider_credential_(?:access|broker|scope))(?:_test)?\.go$/,
      root: "services/worker/internal/credential",
      reason:
        "Credential materialization/broker containment belongs to public Worker core; remove Synara aliases and bind all access to lease/generation policy.",
    },
    {
      pattern: /^daemon(?:_.+_test|_resource_suspend)?\.go$/,
      root: "services/worker/internal/daemon",
      reason:
        "The mixed daemon owns Worker orchestration; rewrite it around public claims, receipts, SDK types and separated workspace/provider-host services.",
    },
    {
      pattern: /^git_askpass(?:_test)?\.go$/,
      root: "services/worker/internal/workspace/git",
      reason:
        "Fail-closed Git askpass mediation is reusable Worker workspace core and must use public credential policy types.",
    },
    {
      pattern: /^(?:memory_test|terminal_log_collector(?:_test)?)\.go$/,
      root: "services/worker/internal/telemetry",
      reason:
        "Worker memory/log collection is public execution telemetry; rewrite event emission to public Runtime/Worker contracts.",
    },
    {
      pattern: /^observability_environment(?:_test|_unix|_windows)?\.go$/,
      root: "services/worker/internal/observability",
      reason:
        "Provider observability environment construction is Worker core; retain platform-neutral OTLP propagation and remove Synara-specific naming.",
    },
    {
      pattern: /^process_termination(?:_.+)?\.go$/,
      root: "services/worker/internal/process",
      reason:
        "Cross-platform bounded process termination is low-level Worker core; preserve OS variants and characterization tests during rewrite.",
    },
    {
      pattern: /^protected_cgroup_.+\.go$/,
      root: "services/worker/internal/containment/cgroupv2",
      reason:
        "Protected cgroup identity, attestation and supervision are public Worker containment core; rewrite authority inputs to public generation fencing.",
    },
    {
      pattern: /^provider_host_(?:prestart|v2)(?:_test)?\.go$/,
      root: "services/worker/internal/providerhost",
      reason:
        "Provider Host supervision is public Worker core, but the Synara Protocol 2.2/2.3 projection must be replaced by the immutable Runtime distribution contract.",
    },
    {
      pattern: /^(?:runner|secret_guard|supervisor)(?:_test)?\.go$/,
      root: "services/worker/internal/supervisor",
      reason:
        "Local runner/supervisor and secret containment form public Worker orchestration core; rewrite Synara imports and keep a single execution authority.",
    },
    {
      pattern: /^stage5_tenant_isolation_integration_test\.go$/,
      root: "conformance/worker/security",
      reason:
        "The Stage 5 test is a useful negative tenant-isolation oracle; rewrite it as product-neutral Worker conformance rather than ship Stage naming.",
    },
    {
      pattern: /^worker_image_manifest(?:_test)?\.go$/,
      root: "services/worker/internal/release",
      reason:
        "Worker image/runtime identity validation belongs to the public Worker release train and must bind the Platform candidate manifest digest.",
    },
    {
      pattern: /^worker_storage_scrub(?:_test)?\.go$/,
      root: "services/worker/internal/reaper",
      reason:
        "Generation-scoped storage scrub is public Worker cleanup core and must remain fenced and receipt-driven.",
    },
    {
      pattern: /^workspace_.+\.go$|^workspace\.go$/,
      root: "services/worker/internal/workspace",
      reason:
        "Workspace materialization/cache/restore/cleanup/locking is managed-agent Worker core; rewrite Synara execution/git policy dependencies to public SDK contracts.",
    },
  ];
  for (const rule of coreRules) {
    if (rule.pattern.test(file)) {
      return publicDecision(
        "rewrite-public",
        "public-core",
        targetWithSameName(rule.root, path),
        rule.reason,
      );
    }
  }

  throw new Error(`unclassified mixed agentd file: ${path}`);
}

function providerCatalogDecision(path) {
  const file = basename(path);
  if (file === "catalog_gen.go") {
    return publicDecision(
      "rewrite-public",
      "public-core",
      "services/control-plane/internal/provider/catalog/catalog_gen.go",
      "Orphaned checked-in output must not be copied as authority; regenerate it reproducibly from the new public catalog contract.",
    );
  }
  if (path.endsWith("/cmd/generate/main.go")) {
    return publicDecision(
      "rewrite-public",
      "public-core",
      "tools/generate/provider-catalog/main.go",
      "Retain a deterministic public generator command, but make the neutral contract catalog its explicit input and verify output digest.",
    );
  }
  if (file === "generate.go") {
    return publicDecision(
      "rewrite-public",
      "public-core",
      "tools/generate/provider-catalog/generate.go",
      "The legacy generator references a missing Synara JSON file; rewrite it to consume contracts/managed-agent/v1alpha1/provider-capability-catalog.json and fail on drift.",
    );
  }
  throw new Error(`unclassified provider catalog file: ${path}`);
}

function stage3ProviderAcceptanceDecision(path) {
  const file = basename(path);
  if (/vault_/.test(file)) {
    return publicDecision(
      "deferred-public-extension",
      "deferred",
      targetWithSameName("conformance/platform-adapter/vault/legacy", path),
      "Vault audit/SIEM/snapshot behavior is an enterprise secret-source extension oracle; retain for a later signed out-of-process Vault adapter, not P1 core.",
    );
  }
  if (/protected_cgroup|stage5_provider_isolation/.test(file)) {
    return publicDecision(
      "adapter",
      "public-platform-adapter",
      targetWithSameName("conformance/worker/adapters/isolation/legacy", path),
      "The legacy isolation gate is useful public adapter conformance, but environment-specific inputs must be rewritten for gVisor/Cocoon/container adapters.",
    );
  }
  if (/registry_supply_chain|worker_manifest|worker_release_rollout/.test(file)) {
    return publicDecision(
      "rewrite-public",
      "runtime-release",
      targetWithSameName("conformance/worker/supply-chain/legacy", path),
      "Worker manifest/registry rollout checks are public release evidence; rewrite Synara schema names to the Worker and Platform manifest contracts.",
    );
  }
  if (
    /^(?:test_)?(?:acceptance_runner|controlled_remote_release_gate|docker_release_gate|docker_worker_release_rollout_gate|local_release_gate|registry_release_gate|release_gate_common|worker_release_rollout_common)\.py$/u.test(
      file,
    )
  ) {
    return publicDecision(
      "rewrite-public",
      "runtime-release",
      targetWithSameName("conformance/worker/supply-chain/legacy", path),
      "This script drives external Docker/remote/Registry release conformance. Keep it in the release train with explicit adapter endpoints and sanitized evidence; it is not Control Plane core authority.",
    );
  }
  return publicDecision(
    "rewrite-public",
    "public-core",
    targetWithSameName("conformance/worker/lifecycle/legacy", path),
    "The legacy provider acceptance path is a characterization oracle for public Worker lifecycle; rewrite product endpoints and evidence schemas rather than copy them.",
  );
}

function stage6SecurityDecision(path) {
  const file = basename(path);
  if (file === "README.md" || /internal_self_hosted_boundary/.test(file)) {
    return retainedInSynara(
      path,
      "This Stage 6 security document/check validates Synara's private self-hosted product boundary, not the standalone public platform.",
    );
  }
  if (/artifact_storage_deployment/.test(file)) {
    return publicDecision(
      "adapter",
      "public-platform-adapter",
      targetWithSameName("conformance/platform-adapter/storage/legacy", path),
      "Artifact storage deployment checks are public filesystem/S3 adapter conformance after removing Synara Stage 6 evidence schemas.",
    );
  }
  if (/observability_deployment/.test(file)) {
    return publicDecision(
      "adapter",
      "public-platform-adapter",
      targetWithSameName("conformance/platform-adapter/otlp/legacy", path),
      "Observability deployment checks become public OTLP adapter conformance after replacing Synara-specific deployment evidence.",
    );
  }
  if (/route_auth_boundaries|tenant_isolation_matrix/.test(file)) {
    return publicDecision(
      "rewrite-public",
      "public-core",
      targetWithSameName("conformance/control-plane/security/legacy", path),
      "Route authorization and tenant isolation are public Control Plane security invariants; rewrite fixtures against public API/RBAC contracts.",
    );
  }
  if (/worker_supply_chain_evidence/.test(file)) {
    return publicDecision(
      "rewrite-public",
      "runtime-release",
      targetWithSameName("conformance/worker/supply-chain/legacy", path),
      "Worker supply-chain evidence validation belongs to the public Worker release train; replace Synara release schemas with Platform manifest subjects.",
    );
  }
  throw new Error(`unclassified Stage 6 security file: ${path}`);
}

function scriptDecision(path) {
  if (path.startsWith("scripts/stage3-provider-acceptance/")) {
    return stage3ProviderAcceptanceDecision(path);
  }
  if (path.startsWith("scripts/stage6-security/")) {
    return stage6SecurityDecision(path);
  }
  if (/^scripts\/stage6_common\/(?:__init__|immutable_evidence_io)\.py$/.test(path)) {
    return publicDecision(
      "move",
      "runtime-release",
      targetWithSameName("tools/evidence", path),
      "Exclusive immutable evidence I/O is low-dependency release tooling; move with provenance and rename Stage 6 namespace during P1.",
    );
  }
  if (path === "scripts/worker-image-manifest.test.ts") {
    return publicDecision(
      "rewrite-public",
      "runtime-release",
      "conformance/worker/supply-chain/worker-image-manifest.test.ts",
      "This test binds Worker build inputs and provider runtime identity; rewrite it to the public Worker/Platform candidate manifest.",
    );
  }
  return retainedInSynara(
    path,
    "This script is coupled to Synara desktop/server packaging, Polaris SDK, Stage 6 governance/evidence, product release, or private operations; the public repo must create independent tooling.",
  );
}

const synaraPrivatePattern = new RegExp(
  [
    "billing",
    "capacitygovernance",
    "capacity_governance",
    "commercial",
    "cloud[_-]?cost",
    "cost[_-]?allocation",
    "cost[_-]?role",
    "desktop",
    "enterprise[_-]?operations",
    "entitlement",
    "final[_-]?ga",
    "governanceauthority",
    "governance_authority",
    "incidentexercisegovernance",
    "incident_exercise_governance",
    "incidentgovernance",
    "incident_governance",
    "internal[_-]?cost",
    "operationsexercisegovernance",
    "operations_exercise_governance",
    "operations[_-]?support",
    "payment",
    "penetrationgovernance",
    "penetration_governance",
    "productprofile",
    "providercommercial",
    "provider_commercial",
    "production[_-]?rotation",
    "recoverygovernance",
    "recovery_governance",
    "releasegovernance",
    "release_governance",
    "slogovernance",
    "slo_governance",
    "stage[_-]?6",
    "subscription",
    "supportaccess",
    "support_access",
    "support[_-]?diagnostic",
    "service[_-]?level[_-]?objective",
    "third[_-]?party[_-]?penetration",
  ].join("|"),
  "i",
);
const deferredExtensionPattern = new RegExp(
  [
    "data[_-]?residency",
    "compliancegovernance",
    "compliance_governance",
    "domain[_-]?verification",
    "enterprise[_-]?identity",
    "enterpriseidentity",
    "legal[_-]?hold",
    "legalholds",
    "privacy",
    "saml",
    "scim",
    "tenant[_-]?data[_-]?export",
  ].join("|"),
  "i",
);

function extensionName(path) {
  if (/scim|saml|enterprise[_-]?identity|enterpriseidentity|domain[_-]?verification/i.test(path)) {
    return "enterprise-identity";
  }
  if (/compliance/i.test(path)) return "compliance";
  if (/legal[_-]?hold|legalholds/i.test(path)) return "legal-hold";
  if (/data[_-]?residency/i.test(path)) return "data-residency";
  return "privacy-export";
}

function packageName(path) {
  return path.match(/services\/control-plane\/internal\/([^/]+)/)?.[1] ?? "root";
}

function publicAdapterTarget(path) {
  const file = basename(path);
  const pkg = packageName(path);
  if (/cocoon/i.test(path)) {
    return path.includes("/cmd/")
      ? `services/worker/cmd/${basename(dirname(path))}/${file}`
      : `services/worker/adapters/cocoon/legacy/${pkg}/${file}`;
  }
  if (/gvisor/i.test(path)) {
    return path.includes("/cmd/")
      ? `services/worker/cmd/cloud-agent-gvisor-attestor/${file}`
      : `services/worker/adapters/container/gvisor/legacy/${pkg}/${file}`;
  }
  if (/kubernetes/i.test(path)) {
    const root = /executiontargets|Dockerfile|deploy\//.test(path)
      ? "services/worker/adapters/kubernetes"
      : "services/control-plane/adapters/kubernetes";
    return `${root}/legacy/${pkg}/${file}`;
  }
  if (/ssh/i.test(path)) {
    return `services/worker/adapters/ssh/legacy/${pkg}/${file}`;
  }
  if (/s3/i.test(path)) {
    return `services/control-plane/adapters/s3/legacy/${pkg}/${file}`;
  }
  if (/kms/i.test(path)) {
    return path.includes("/cmd/")
      ? `services/control-plane/cmd/cloud-agent-kms-adapter/${file}`
      : `services/control-plane/adapters/kms/legacy/${pkg}/${file}`;
  }
  if (/routing|ingress/i.test(path)) {
    return path.includes("/cmd/")
      ? `services/control-plane/cmd/cloud-agent-routing-sign/${file}`
      : `services/control-plane/adapters/ingress/legacy/${pkg}/${file}`;
  }
  if (/observability|otlp/i.test(path)) {
    return `services/control-plane/adapters/otlp/legacy/${pkg}/${file}`;
  }
  throw new Error(`adapter seed has no semantic target: ${path}`);
}

function controlPlaneCoreTarget(path, classification) {
  const file = basename(path);
  if (path.startsWith("services/control-plane/migrations/")) {
    const semanticName = file.replace(/^\d{6}_/, "");
    if (classification === "adapter") {
      return `services/control-plane/migrations/planned/adapter-${semanticName}`;
    }
    return `services/control-plane/migrations/planned/${semanticName}`;
  }
  if (path.startsWith("services/control-plane/cmd/api/")) {
    return `services/control-plane/cmd/cloud-agent-control-plane/${file}`;
  }
  if (path.startsWith("services/control-plane/cmd/agentd/")) {
    return `services/worker/cmd/cloud-agent-worker/${file}`;
  }
  if (path.startsWith("services/control-plane/cmd/metadata/")) {
    return `hxp0618/synara:${path}`;
  }
  if (path.startsWith("services/control-plane/testdata/")) {
    return `conformance/worker/legacy-provider/${file}`;
  }
  if (!path.includes("/internal/")) {
    if (file === "Dockerfile") return "deploy/images/control-plane/Dockerfile";
    if (file === "Dockerfile.agentd") return "deploy/images/worker/legacy-agentd.Dockerfile";
    if (file === "Dockerfile.kms-worker") return "deploy/images/adapters/kms/Dockerfile";
    return `services/control-plane/${file}`;
  }

  const pkg = packageName(path);
  const domainRoots = {
    artifacts: "artifact",
    audit: "audit",
    authorization: "iam/authorization",
    bootstrap: "bootstrap",
    capacitygovernance: "scheduler/capacity",
    cgroupv2limits: "workerpolicy/cgroupv2",
    config: "bootstrap/config",
    containmentattestation: "workerpolicy/attestation",
    credentialbindings: "credential/binding",
    credentials: "credential",
    credentialscope: "credential/scope",
    database: "store/postgres/legacy-database",
    developerapi: "api/developer",
    developerwebhooks: "api/developer/webhook",
    eventstream: "durable/eventstream",
    executionqueue: "scheduler/queue",
    executions: "managedagent/execution",
    executiontargets: "scheduler/target",
    idempotency: "durable/idempotency",
    httpapi: "api/legacy",
    identity: "iam/identity",
    leadership: "durable/leadership",
    lifecyclepolicy: "lifecycle/policy",
    memories: "managedagent/memory",
    metricfacts: "observability/metricfacts",
    metricrollup: "observability/metricrollup",
    observability: "observability",
    outbox: "durable/outbox",
    persistence: "store/postgres/legacy-persistence",
    placement: "scheduler/placement",
    platform: "bootstrap/platform",
    podlifecycle: "scheduler/podlifecycle",
    poolautoscaling: "scheduler/autoscaling",
    problem: "problem",
    projects: "iam/project",
    providercapabilities: "provider/capability",
    providercatalog: "provider/catalog",
    providerproxy: "managedagent/providerproxy",
    quotas: "scheduler/quota",
    reconcilerleadership: "durable/reconcilerleadership",
    recoverybundle: "recovery",
    retention: "lifecycle/retention",
    retentiongate: "lifecycle/retention",
    runtimekeys: "credential/runtimekey",
    runtimesecretrotation: "credential/rotation",
    schedulingdecision: "scheduler/decision",
    schedulingpolicy: "scheduler/policy",
    secret: "credential/secret",
    serviceaccounts: "iam/serviceaccount",
    sessions: "managedagent/session",
    targetcapacity: "scheduler/targetcapacity",
    tenancy: "iam/tenancy",
    tenantstate: "iam/tenantstate",
    tenantuseraccess: "iam/useraccess",
    tracing: "observability/tracing",
    usage: "usage",
    warmcapacity: "scheduler/warmcapacity",
    workerfacts: "worker/facts",
    workerreleases: "worker/release",
  };
  if (pkg === "testsupport") {
    const suite = path.match(/internal\/testsupport\/([^/]+)/)?.[1] ?? "shared";
    return `conformance/control-plane/testsupport/${suite}/${file}`;
  }
  const root = domainRoots[pkg] ?? `legacy/${pkg}`;
  return `services/control-plane/internal/${root}/${file}`;
}

function moveTarget(path) {
  const pkg = packageName(path);
  const roots = {
    databasetime: "platformtime",
    fairqueue: "fairqueue",
    gitpolicy: "gitpolicy",
    problem: "problem",
    secretguard: "secretguard",
    validation: "validation",
    workertiming: "workertiming",
  };
  const root = roots[pkg];
  if (!root) throw new Error(`move seed has no low-dependency target: ${path}`);
  return `sdk/go/${root}/${basename(path)}`;
}

function contractDecision(row) {
  const { path } = row;
  const file = basename(path);
  if (synaraPrivatePattern.test(path)) {
    return retainedInSynara(
      path,
      "This contract defines Synara commercial, desktop, Stage 6 approval, support, or product-release policy and is not a public platform wire contract.",
    );
  }
  if (deferredExtensionPattern.test(path)) {
    const extension = extensionName(path);
    return publicDecision(
      "deferred-public-extension",
      "deferred",
      `conformance/platform-adapter/${extension}/legacy/contracts/${file}`,
      "This enterprise governance/identity contract is retained as a future out-of-process adapter oracle, not imported into public core.",
    );
  }
  if (row.classificationSeed === "retire" || /provider-host-v1/.test(path)) {
    return publicDecision(
      "retire",
      "synara-host",
      `hxp0618/synara:${path}`,
      "Provider Host v1 is a legacy Synara contract retained only for bounded read/rollback compatibility and is not migrated to public source.",
    );
  }
  if (
    new Set([
      "credential-kms-rotation-v1.md",
      "global-target-routing-dr-v1.md",
      "kubernetes-allocation-backend-v1.md",
      "kubernetes-resilience-acceptance-v1.md",
      "self-hosted-kms-worker-v1.md",
    ]).has(file)
  ) {
    return publicDecision(
      "rewrite-public",
      "public-core",
      `contracts/managed-agent/v1alpha1/legacy-${file}`,
      "This mixed document defines durable authority/state-machine behavior plus environment-specific mechanics. Rewrite the authority as a public core contract and split provider-specific effects into a separate adapter annex.",
    );
  }
  if (row.classificationSeed === "adapter") {
    return publicDecision(
      "adapter",
      "public-platform-adapter",
      `contracts/platform-adapter/v1alpha1/legacy-${file}`,
      "The legacy document is an adapter behavior oracle; rewrite it into the versioned public Platform Adapter contract and conformance fixtures.",
    );
  }
  const domain = /provider-host|runtime-event/.test(file)
    ? "runtime/v2"
    : /worker|agentd|cgroup/.test(file)
      ? "worker/v1alpha1"
      : /session|execution|artifact|workspace|credential|memory|provider/.test(file)
        ? "managed-agent/v1alpha1"
        : "managed-agent/v1alpha1/platform";
  return publicDecision(
    "rewrite-public",
    "public-core",
    `contracts/${domain}/legacy-${file}`,
    "Rewrite this Synara contract as a product-neutral, versioned public schema/golden fixture; prose or Stage naming cannot remain wire authority.",
  );
}

function deployDecision(row) {
  const { path } = row;
  if (path.startsWith("deploy/billing/") || path.startsWith("deploy/saas/")) {
    return retainedInSynara(
      path,
      "This deployment composes Synara billing/SaaS product services and private operational policy; it is not a standalone Cloud Agents deployment input.",
    );
  }
  if (path.startsWith("deploy/kubernetes/security/vault/")) {
    const suffix = path.slice("deploy/kubernetes/security/vault/".length);
    return publicDecision(
      "deferred-public-extension",
      "deferred",
      `conformance/platform-adapter/vault/legacy/deploy/${suffix}`,
      "The Synara Vault production configuration is a future external adapter oracle; do not publish private roles/policies as built-in defaults.",
    );
  }
  if (
    /^deploy\/kubernetes\/(?:admin-(?:deployment|service)\.yaml|developer-docs\/)/u.test(path) ||
    new Set([
      "deploy/kubernetes/README.md",
      "deploy/kubernetes/acceptance.sh",
      "deploy/kubernetes/config.example.yaml",
      "deploy/kubernetes/deployment.yaml",
      "deploy/kubernetes/kustomization.yaml",
      "deploy/kubernetes/remote-stage6-acceptance.sh",
    ]).has(path)
  ) {
    return retainedInSynara(
      path,
      "This manifest/script deploys Synara Admin, Developer Docs, or the private Stage 6 acceptance profile; it is not a public Control Plane/Worker adapter input.",
    );
  }
  if (/^deploy\/kubernetes\/(?:gvisor|sandbox-operator|vk-cocoon)\//u.test(path)) {
    const suffix = path.slice("deploy/kubernetes/".length);
    return publicDecision(
      "adapter",
      "public-platform-adapter",
      `deploy/adapters/kubernetes/${suffix}`,
      "This file installs or characterizes an optional Kubernetes isolation actuator; keep it behind the public adapter boundary and do not grant it Control Plane state authority.",
    );
  }
  if (path.startsWith("deploy/kubernetes/")) {
    const suffix = path.slice("deploy/kubernetes/".length);
    return publicDecision(
      "rewrite-public",
      "runtime-release",
      `deploy/helm/cloud-agents/legacy-kubernetes/${suffix}`,
      "This is public Helm/control-plane/Worker/operations/release composition, not an adapter implementation; rewrite Synara product wiring to neutral images, contracts, security context and digest pins.",
    );
  }
  if (path.startsWith("deploy/personal/")) {
    const suffix = path.slice("deploy/personal/".length);
    return publicDecision(
      "rewrite-public",
      "runtime-release",
      `deploy/compose/legacy-personal/${suffix}`,
      "Use this personal profile only as a characterization input for the independent public Compose stack; remove Synara services and ambient credentials.",
    );
  }
  if (path.startsWith("deploy/remote/")) {
    const suffix = path.slice("deploy/remote/".length);
    return publicDecision(
      "rewrite-public",
      "runtime-release",
      `deploy/compose/remote/${suffix}`,
      "Rewrite the remote worker profile as a public digest-pinned Compose/Worker deployment without Synara source or private images.",
    );
  }
  if (path.startsWith("deploy/worker/")) {
    const suffix = path.slice("deploy/worker/".length);
    return publicDecision(
      "rewrite-public",
      "runtime-release",
      `deploy/images/worker/${suffix}`,
      "Rebuild this Worker supply-chain input in the independent Worker release train and bind every tool, Runtime and base image by digest.",
    );
  }
  throw new Error(`deploy input has no final decision: ${path}`);
}

function seededScriptDecision(row) {
  const { path } = row;
  const file = basename(path);
  if (/cloud-agent-candidate|provider-host-fixture/.test(path)) {
    const root = /fixture/.test(path)
      ? "conformance/runtime/provider-host"
      : "tools/release/candidate";
    return publicDecision(
      "rewrite-public",
      "runtime-release",
      `${root}/${file}`,
      "Rewrite the legacy candidate/Provider Host check to consume public immutable release manifests and same-bits Runtime artifacts.",
    );
  }
  if (/vault_kms/.test(path)) {
    return publicDecision(
      "deferred-public-extension",
      "deferred",
      `conformance/platform-adapter/vault/legacy/${file}`,
      "Vault/KMS admission is retained as a future external adapter conformance oracle rather than a public core dependency.",
    );
  }
  if (/kubernetes/.test(path)) {
    return publicDecision(
      "adapter",
      "public-platform-adapter",
      `conformance/worker/adapters/kubernetes/legacy/${file}`,
      "Rewrite the Synara Kubernetes rollout gate as public Worker adapter conformance with generation fencing and immutable releases.",
    );
  }
  if (/ssh/.test(path)) {
    return publicDecision(
      "adapter",
      "public-platform-adapter",
      `conformance/worker/adapters/ssh/legacy/${file}`,
      "Rewrite the SSH gate as public adapter conformance; remove Synara environment assumptions and preserve fenced bootstrap semantics.",
    );
  }
  if (/s3_object_lock/.test(path)) {
    return publicDecision(
      "adapter",
      "public-platform-adapter",
      `conformance/platform-adapter/s3/legacy/${file}`,
      "Rewrite S3 object-lock verification as public storage adapter conformance with immutable receipt evidence.",
    );
  }
  if (/vk-cocoon/.test(path)) {
    return publicDecision(
      "adapter",
      "public-platform-adapter",
      `tools/adapters/cocoon/${file}`,
      "Rewrite source preparation as a pinned Cocoon adapter build tool with verified upstream commit and patch digests.",
    );
  }
  throw new Error(`seeded script has no final decision: ${path}`);
}

const publicStateAuthorityPrefixes = [
  "services/control-plane/migrations/",
  "services/control-plane/internal/config/",
  "services/control-plane/internal/database/",
  "services/control-plane/internal/executions/",
  "services/control-plane/internal/httpapi/",
  "services/control-plane/internal/kmsrotation/",
  "services/control-plane/internal/kmsworker/",
  "services/control-plane/internal/observability/",
  "services/control-plane/internal/persistence/",
  "services/control-plane/internal/placement/",
  "services/control-plane/internal/routing/",
  "services/control-plane/internal/sessions/",
];

function isPublicStateAuthority(path) {
  return (
    publicStateAuthorityPrefixes.some((prefix) => path.startsWith(prefix)) ||
    /^services\/control-plane\/internal\/kms\/(?:envelope|factory)(?:_test)?\.go$/u.test(path)
  );
}

function publicStateAuthorityDecision(path) {
  return publicDecision(
    "rewrite-public",
    "public-core",
    controlPlaneCoreTarget(path, "rewrite-public"),
    "This file defines or validates durable Control Plane state, admission, API, projection, migration, receipt, or fencing authority. Keep it in public core and inject environment effects through ports; an adapter must not own this state.",
  );
}

function isDockerExecutionTargetEffect(path) {
  return /^services\/control-plane\/internal\/(?:executions\/docker_runtime_isolation|executiontargets\/docker_reconciler)/u.test(
    path,
  );
}

const executionTargetCoreOrSplitPattern = new RegExp(
  [
    "kubernetes_allocation_backend",
    "kubernetes_foundation_cache",
    "kubernetes_pod_spec",
    "kubernetes_priority",
    "kubernetes_reconciler",
    "kubernetes_resource_quantity",
    "kubernetes_sandbox_materializer",
    "kubernetes_target_capacity",
    "kubernetes_target_lifecycle",
    "kubernetes_warm_pool",
    "kubernetes_workload_identity",
    "managed_kubernetes_routing_publisher",
    "managed_kubernetes_target_capacity_publisher",
    "managed_kubernetes_warm_capacity_publisher",
    "ssh_provisioner",
    "ssh_provisioning_executor",
  ].join("|"),
  "u",
);

function isExecutionTargetCoreOrSplit(path) {
  return (
    path.startsWith("services/control-plane/internal/executiontargets/") &&
    executionTargetCoreOrSplitPattern.test(basename(path))
  );
}

function executionTargetCoreOrSplitDecision(path) {
  return publicDecision(
    "rewrite-public",
    "public-core",
    controlPlaneCoreTarget(path, "rewrite-public"),
    "This mixed target file contains scheduling/reconciliation/fencing/identity authority as well as environment I/O. Rewrite the authority in public core and inject Kubernetes/SSH clients through adapter ports; never move the file wholesale into an adapter.",
  );
}

function dockerExecutionTargetDecision(path) {
  return publicDecision(
    "adapter",
    "public-platform-adapter",
    `services/worker/adapters/container/docker/legacy/${packageName(path)}/${basename(path)}`,
    "This implementation directly actuates or inspects Docker Engine/runtime isolation. Move it behind the public container actuator contract; durable scheduling and lifecycle state remain in Control Plane core.",
  );
}

const explicitPlatformEffectTargets = new Map([
  [
    "services/control-plane/internal/artifacts/local_store.go",
    "services/control-plane/adapters/storage/filesystem/local_store.go",
  ],
  [
    "services/control-plane/internal/developerwebhooks/publisher.go",
    "services/control-plane/adapters/webhook/developer/publisher.go",
  ],
  [
    "services/control-plane/internal/outbox/incident_webhook_publisher.go",
    "services/control-plane/adapters/webhook/incident/publisher.go",
  ],
  [
    "services/control-plane/internal/outbox/incident_webhook_publisher_test.go",
    "services/control-plane/adapters/webhook/incident/publisher_test.go",
  ],
]);

function explicitPlatformEffectDecision(path) {
  const target = explicitPlatformEffectTargets.get(path);
  if (!target) return null;
  return publicDecision(
    "adapter",
    "public-platform-adapter",
    target,
    "This file directly performs filesystem or outbound HTTP effects. Move the effect behind the public adapter contract; policy, idempotency and durable outbox authority stay in public core.",
  );
}

const liveAdapterConformancePaths = new Set([
  "services/control-plane/internal/executions/sandbox_operator_orbstack_integration_test.go",
  "services/control-plane/internal/executions/worker_tenant_isolation_orbstack_integration_test.go",
  "services/control-plane/internal/executiontargets/stage5_runtime_isolation_orbstack_integration_test.go",
]);

function liveAdapterConformanceDecision(path) {
  if (
    !liveAdapterConformancePaths.has(path) &&
    !/^services\/control-plane\/internal\/executiontargets\/.*(?:orbstack|kubernetes_reconciler)_integration_test\.go$/u.test(
      path,
    )
  )
    return null;
  return publicDecision(
    "adapter",
    "public-platform-adapter",
    `conformance/platform-adapter/kubernetes/legacy/${basename(path)}`,
    "This integration test invokes a live Kubernetes/OrbStack actuator. Preserve it as adapter conformance; it cannot serve as portable Control Plane core evidence.",
  );
}

function seededDecision(row) {
  const { path, scope } = row;
  if (scope === "legacy-build-context") {
    return retainedInSynara(
      path,
      "This tracked blob is frozen because the legacy root image used a broad COPY context, but it is outside the approved Control Plane/Worker extraction surface and remains Synara-owned.",
    );
  }
  if (scope === "contract-reference") return contractDecision(row);
  if (scope === "deploy") return deployDecision(row);
  if (scope === "scripts") return seededScriptDecision(row);
  if (scope === "provider-host-compat") {
    return publicDecision(
      "adapter",
      "synara-host",
      `hxp0618/synara:${path}`,
      "Provider Host compatibility remains a Synara-owned Host Adapter/bin and consumes immutable public Runtime artifacts; it is not an eighth public package.",
    );
  }
  if (scope === "root-supply-chain") {
    if (path.endsWith(".schema.json")) {
      return publicDecision(
        "rewrite-public",
        "runtime-release",
        "contracts/runtime/v2/cloud-agent-candidate-lock.schema.json",
        "Rewrite the candidate lock schema as a neutral public Runtime release contract with canonical digest rules.",
      );
    }
    if (path === "cloud-agent-candidate.lock.json") {
      return publicDecision(
        "rewrite-public",
        "runtime-release",
        "tools/release/candidates/legacy-synara-candidate.lock.json",
        "Retain the legacy lock as immutable provenance/compatibility input; public releases generate a new same-bits candidate manifest.",
      );
    }
    return retainedInSynara(
      path,
      "This root/workspace build input belongs to the legacy Synara monorepo. The public platform must own a new minimal manifest, lock, patch set and build configuration instead of copying it.",
    );
  }
  if (scope !== "control-plane") throw new Error(`unsupported seeded scope: ${scope}:${path}`);

  if (synaraPrivatePattern.test(path)) {
    return retainedInSynara(
      path,
      "This code/SQL/test implements Synara commercial, desktop, support, or Stage 6 product governance and remains outside public platform authority.",
    );
  }
  if (deferredExtensionPattern.test(path)) {
    const extension = extensionName(path);
    return publicDecision(
      "deferred-public-extension",
      "deferred",
      `conformance/platform-adapter/${extension}/legacy/${packageName(path)}/${basename(path)}`,
      "Retain behavior only as a future external enterprise adapter oracle; public core must not import this implementation or schema.",
    );
  }
  if (row.capability === "metadata-migration") {
    return publicDecision(
      "retire",
      "synara-host",
      `hxp0618/synara:${path}`,
      "The legacy metadata import surface is retired after cutover; keep only bounded Synara rollback/read compatibility and do not migrate implementation.",
    );
  }
  if (row.classificationSeed === "move") {
    return publicDecision(
      "move",
      "public-core",
      moveTarget(path),
      "This low-dependency mechanism can move with provenance after import/license normalization and focused characterization tests.",
    );
  }
  const explicitEffect = explicitPlatformEffectDecision(path);
  if (explicitEffect) return explicitEffect;
  const liveAdapterConformance = liveAdapterConformanceDecision(path);
  if (liveAdapterConformance) return liveAdapterConformance;
  if (isDockerExecutionTargetEffect(path)) return dockerExecutionTargetDecision(path);
  if (isPublicStateAuthority(path)) return publicStateAuthorityDecision(path);
  if (isExecutionTargetCoreOrSplit(path)) return executionTargetCoreOrSplitDecision(path);
  if (row.classificationSeed === "adapter") {
    const target = path.startsWith("services/control-plane/migrations/")
      ? controlPlaneCoreTarget(path, "adapter")
      : publicAdapterTarget(path);
    return publicDecision(
      "adapter",
      target.startsWith("services/control-plane/migrations/")
        ? "public-core"
        : "public-platform-adapter",
      target,
      "Rewrite this environment side effect behind the versioned public Adapter boundary; adapter returns fenced observations/receipts and never owns Control Plane state.",
    );
  }
  if (row.classificationSeed === "retire") {
    return publicDecision(
      "retire",
      "synara-host",
      `hxp0618/synara:${path}`,
      "This legacy surface is retained only for bounded compatibility and is removed after public SDK/API cutover.",
    );
  }
  if (path.includes("internal/providercatalog/") && !/generate|catalog_gen/.test(path)) {
    return publicDecision(
      "move",
      "public-core",
      `sdk/go/providercatalog/${basename(path)}`,
      "Provider catalog validation is low-dependency public SDK logic; move it while replacing the orphaned Synara generator input with the public contract catalog.",
    );
  }
  return publicDecision(
    "rewrite-public",
    "public-core",
    controlPlaneCoreTarget(path, "rewrite-public"),
    "Rewrite this legacy Control Plane capability against public contracts, new migration lineage and single-writer authority; do not copy Synara internal imports or table identity.",
  );
}

function finalCapabilityFor(row, decision) {
  if (row.capability !== "unclassified") return row.capability;
  const path = row.path;
  const rules = [
    [/\.github\//, "repository-governance"],
    [/provider-host|runtime-event|cloud-agent-candidate/, "external-runtime-candidate"],
    [/stage3-provider-acceptance/, "worker-conformance"],
    [/stage6|releasegovernance|governance/, "synara-release-governance"],
    [/Dockerfile|package\.json|bun\.lock|go\.(?:mod|sum)/, "module-supply-chain"],
    [/audit/, "audit"],
    [/backup|recovery/, "disaster-recovery"],
    [/capacity|placement|warmcapacity|autoscal/, "worker-capacity"],
    [/cost|billing|commercial/, "billing-commercial"],
    [/observability|tracing|metric/, "observability"],
    [/deployment|kubernetes|routing|ingress/, "deployment-adapter"],
    [/desktop/, "desktop-integration"],
    [/identity|auth|role|tenant|project|serviceaccount/, "identity-tenancy"],
    [/execution|session|interaction|memory/, "execution-authority"],
    [/worker|cgroup|containment/, "worker-lifecycle"],
    [/credential|secret|kms/, "credential-broker"],
    [/outbox|idempotency|leader|reconciler/, "durability"],
    [/artifact|workspace/, "workspace-artifact"],
    [/release|candidate|rotation|supply-chain/, "release-supply-chain"],
    [/privacy|retention|residency|legal/, "governance-retention"],
    [/api|webhook|httpapi/, "management-api"],
  ];
  const matched = rules.find(([pattern]) => pattern.test(path))?.[1];
  if (matched) return matched;
  if (decision.finalClassification === "synara-only") return "synara-product-operations";
  if (path.startsWith("docs/contracts/")) return "platform-contract";
  if (path.startsWith("scripts/")) return "synara-build-release";
  if (path.startsWith("deploy/")) return "product-composition";
  const pkg = packageName(path);
  if (pkg !== "root") return `control-plane-${pkg}`;
  return "control-plane-foundation";
}

function decisionFor(row) {
  const { path, scope } = row;
  if (path.startsWith("services/control-plane/internal/agentd/")) {
    return agentdDecision(path);
  }
  if (path.startsWith("services/control-plane/internal/providercatalog/")) {
    return providerCatalogDecision(path);
  }
  if (scope === "scripts") return scriptDecision(path);
  if (scope === "ci") {
    return retainedInSynara(
      path,
      "This file governs Synara issues, trust, monorepo CI, Polaris SDK, or product release; cloud-agents requires new least-privilege workflows with pinned actions and OIDC attestation.",
    );
  }
  if (path === "Dockerfile") {
    return publicDecision(
      "rewrite-public",
      "runtime-release",
      "deploy/images/worker/Dockerfile",
      "The root image is a broad Synara build context; reconstruct a minimal digest-pinned public Worker image that consumes immutable Runtime artifacts.",
    );
  }
  if (path === "package.json" || path === "bun.lock") {
    return retainedInSynara(
      path,
      "The Synara root workspace manifest/lock is provenance input for the legacy Worker image only; cloud-agents must generate and own independent manifests and locks.",
    );
  }
  if (scope === "root-supply-chain") {
    return retainedInSynara(
      path,
      "This explicit legacy root-image/workspace input remains Synara-owned. The public platform will create an independent minimal workspace, lock, patch set and build configuration rather than copy this monorepo input.",
    );
  }
  throw new Error(`manual-review row has no decision rule: ${path}`);
}

function countBy(rows, field) {
  const counts = new Map();
  for (const row of rows) counts.set(row[field], (counts.get(row[field]) ?? 0) + 1);
  return [...counts].sort(([a], [b]) => a.localeCompare(b));
}

function markdownTable(counts, left) {
  const labels = counts.map(([key]) => `\`${key}\``);
  const leftWidth = Math.max(left.length, ...labels.map((label) => label.length));
  const countWidth = Math.max("Count".length, ...counts.map(([, count]) => String(count).length));
  return [
    `| ${left.padEnd(leftWidth)} | ${"Count".padStart(countWidth)} |`,
    `| ${"-".repeat(leftWidth)} | ${"-".repeat(countWidth - 1)}: |`,
    ...counts.map(
      ([key, count]) =>
        `| ${`\`${key}\``.padEnd(leftWidth)} | ${String(count).padStart(countWidth)} |`,
    ),
  ].join("\n");
}

const sourceHead = runGit("rev-parse", "HEAD").trim();
const sourceTree = runGit("rev-parse", "HEAD^{tree}").trim();
if (sourceHead !== expectedSourceHead || sourceTree !== expectedSourceTree) {
  throw new Error(
    `P0 source moved: expected ${expectedSourceHead}/${expectedSourceTree}, observed ${sourceHead}/${sourceTree}`,
  );
}
const dirty = runGit("status", "--porcelain=v1", "--untracked-files=all").trim();
if (dirty) throw new Error(`P0 source is dirty; refusing final decisions:\n${dirty}`);

const cmdAPI = runGit("show", `${expectedSourceHead}:services/control-plane/cmd/api/main.go`);
const cmdAPIAgentdSymbols = [
  '"github.com/synara-ai/synara/services/control-plane/internal/agentd"',
  "agentd.RunGitAskPassHelperFromEnvironment",
  "*agentd.LocalSupervisor",
  "agentd.NewLocalSupervisor",
];
for (const symbol of cmdAPIAgentdSymbols) {
  if (!cmdAPI.includes(symbol)) {
    throw new Error(`cmd/api agentd coupling changed; missing expected symbol: ${symbol}`);
  }
}
const legacyCatalogSource = "packages/contracts/src/providerCapabilityCatalog.json";
const providerGenerator = runGit(
  "show",
  `${expectedSourceHead}:services/control-plane/internal/providercatalog/generate.go`,
);
if (!providerGenerator.includes(legacyCatalogSource)) {
  throw new Error(
    "provider catalog generator no longer exposes the expected orphaned source reference",
  );
}
if (runGit("ls-tree", "-r", "--name-only", expectedSourceHead, "--", legacyCatalogSource).trim()) {
  throw new Error(`provider catalog source unexpectedly exists: ${legacyCatalogSource}`);
}

const inventoryBuffer = readFileSync(inventoryPath);
const inventorySha = sha256(inventoryBuffer);
if (inventorySha !== expectedInventorySha256) {
  throw new Error(
    `inventory SHA mismatch: expected ${expectedInventorySha256}, observed ${inventorySha}`,
  );
}
const { headers, rows } = parseTSV(inventoryBuffer.toString("utf8"));
const requiredInputHeaders = [
  "snapshotHead",
  "snapshotTree",
  "scope",
  "classificationSeed",
  "manualReview",
  "path",
  "gitBlobOid",
  "sha256",
];
for (const header of requiredInputHeaders) {
  if (!headers.includes(header)) throw new Error(`inventory missing required column: ${header}`);
}
if (rows.length !== expectedInventoryRows) {
  throw new Error(
    `inventory row count changed: expected ${expectedInventoryRows}, observed ${rows.length}`,
  );
}
const manualRows = rows.filter((row) => row.manualReview === "true");
if (manualRows.length !== expectedManualRows) {
  throw new Error(
    `manual-review row count changed: expected ${expectedManualRows}, observed ${manualRows.length}`,
  );
}
if (rows.some((row) => row.manualReview !== "true" && row.manualReview !== "false")) {
  throw new Error("inventory contains an invalid manualReview value");
}

const expectedScopeAudit = new Map([
  ["deploy", { rows: 117, manual: 0, seeds: { adapter: 115, "synara-only": 2 } }],
  ["scripts", { rows: 235, manual: 216, seeds: { adapter: 19, unclassified: 216 } }],
  ["ci", { rows: 10, manual: 10, seeds: { unclassified: 10 } }],
  ["root-supply-chain", { rows: 21, manual: 19, seeds: { adapter: 2, unclassified: 19 } }],
  ["legacy-build-context", { rows: 7002, manual: 0, seeds: { "synara-only": 7002 } }],
]);
for (const [scope, expected] of expectedScopeAudit) {
  const scopedRows = rows.filter((row) => row.scope === scope);
  const scopedManual = scopedRows.filter((row) => row.manualReview === "true");
  const scopedSeeds = Object.fromEntries(countBy(scopedRows, "classificationSeed"));
  if (
    scopedRows.length !== expected.rows ||
    scopedManual.length !== expected.manual ||
    JSON.stringify(scopedSeeds) !== JSON.stringify(expected.seeds)
  ) {
    throw new Error(
      `${scope} audit changed: observed rows=${scopedRows.length}, manual=${scopedManual.length}, seeds=${JSON.stringify(scopedSeeds)}`,
    );
  }
}

const allPaths = new Set();
for (const row of rows) {
  if (allPaths.has(row.path)) throw new Error(`duplicate inventory path: ${row.path}`);
  allPaths.add(row.path);
  if (row.snapshotHead !== expectedSourceHead || row.snapshotTree !== expectedSourceTree) {
    throw new Error(`row snapshot mismatch: ${row.path}`);
  }
}

const publicTargetPrefixes = [
  "contracts/",
  "sdk/go/",
  "services/control-plane/",
  "services/worker/",
  "deploy/",
  "conformance/",
  "tools/",
];

const decisions = rows.map((row) => {
  const observedBlob = runGit("rev-parse", `${expectedSourceHead}:${row.path}`).trim();
  if (observedBlob !== row.gitBlobOid) {
    throw new Error(
      `blob mismatch for ${row.path}: inventory ${row.gitBlobOid}, source ${observedBlob}`,
    );
  }
  const observedSha256 = sha256(runGitBuffer("cat-file", "blob", observedBlob));
  if (observedSha256 !== row.sha256) {
    throw new Error(
      `content SHA-256 mismatch for ${row.path}: inventory ${row.sha256}, source ${observedSha256}`,
    );
  }
  const decision = row.manualReview === "true" ? decisionFor(row) : seededDecision(row);
  if (!allowedClassifications.has(decision.finalClassification)) {
    throw new Error(`invalid classification for ${row.path}: ${decision.finalClassification}`);
  }
  if (!allowedOwners.has(decision.owner)) {
    throw new Error(`invalid owner for ${row.path}: ${decision.owner}`);
  }
  if (
    !decision.target.startsWith("hxp0618/synara:") &&
    !publicTargetPrefixes.some((prefix) => decision.target.startsWith(prefix))
  ) {
    throw new Error(`target is outside allowed public modules/Synara path: ${row.path}`);
  }
  if (
    decision.target.startsWith("/") ||
    decision.target.includes("..") ||
    /unknown|unclassified/i.test(decision.target)
  ) {
    throw new Error(`invalid target for ${row.path}: ${decision.target}`);
  }
  const criticalRemap = [
    "services/control-plane/internal/executions/",
    "services/control-plane/internal/sessions/",
    "services/control-plane/internal/httpapi/",
    "services/control-plane/internal/database/",
    "services/control-plane/internal/persistence/",
    "services/control-plane/internal/providercatalog/",
    "services/control-plane/migrations/",
    "docs/contracts/",
    "deploy/",
    "apps/provider-host/",
  ].some((prefix) => row.path.startsWith(prefix));
  if (criticalRemap && decision.target === row.path) {
    throw new Error(
      `critical mixed/host input was mechanically mapped to the same path: ${row.path}`,
    );
  }
  const finalCapability = finalCapabilityFor(row, decision);
  if (!finalCapability || /unknown|unclassified/i.test(finalCapability)) {
    throw new Error(`invalid final capability for ${row.path}: ${finalCapability}`);
  }
  const provenance = `${expectedSourceHead}:${row.path}#blob=${row.gitBlobOid};sha256=${row.sha256}`;
  const decisionSource =
    row.manualReview === "true"
      ? "explicit-semantic-rule"
      : row.scope === "legacy-build-context"
        ? "full-tree-default-deny"
        : decision.finalClassification !== row.classificationSeed
          ? "semantic-authority-override"
          : "reviewed-seed-rule";
  const publicCandidate = !["synara-only", "retire"].includes(decision.finalClassification);
  const authorityDisposition =
    decision.finalClassification === "adapter"
      ? "environment effect or conformance only; durable authority remains public core"
      : decision.finalClassification === "synara-only" || decision.finalClassification === "retire"
        ? "not a public platform writer or import candidate"
        : decision.owner === "public-core"
          ? "public core candidate; external effects must be injected through ports"
          : "public release/deferred candidate; no Control Plane durable authority";
  return {
    snapshotHead: expectedSourceHead,
    snapshotTree: expectedSourceTree,
    path: row.path,
    scope: row.scope,
    manualReview: row.manualReview,
    classificationSeed: row.classificationSeed,
    finalCapability,
    finalClassification: decision.finalClassification,
    owner: decision.owner,
    target: decision.target,
    provenance,
    authorityDisposition,
    licenseProvenance: publicCandidate
      ? sourceLicenseProvenance
      : "not-imported; retained in hxp0618/synara",
    secretProvenance: secretProvenanceFor(row.path, publicCandidate),
    decisionSource,
    reviewStatus: "executor-reviewed",
    reviewer: "Codex P0 executor",
    reviewedAt,
    reason: decision.reason,
  };
});

const decisionPaths = new Set(decisions.map((decision) => decision.path));
if (decisionPaths.size !== expectedInventoryRows) {
  throw new Error(
    `decision output has duplicate paths: ${decisionPaths.size}/${expectedInventoryRows}`,
  );
}
for (const row of rows) {
  if (!decisionPaths.has(row.path)) throw new Error(`missing decision: ${row.path}`);
}
const adapterAuthorityViolations = decisions.filter(
  (decision) =>
    decision.finalClassification === "adapter" &&
    (/^services\/control-plane\/migrations\/.*\.sql$/u.test(decision.path) ||
      /^services\/control-plane\/internal\/(?:database|httpapi|kmsrotation|kmsworker|persistence|routing|sessions)\//u.test(
        decision.path,
      ) ||
      new Set([
        "docs/contracts/credential-kms-rotation-v1.md",
        "docs/contracts/global-target-routing-dr-v1.md",
        "docs/contracts/kubernetes-allocation-backend-v1.md",
        "docs/contracts/kubernetes-resilience-acceptance-v1.md",
        "docs/contracts/self-hosted-kms-worker-v1.md",
      ]).has(decision.path)),
);
if (adapterAuthorityViolations.length !== 0) {
  throw new Error(
    `adapter owns durable/API authority: ${adapterAuthorityViolations.map((item) => item.path).join(", ")}`,
  );
}
const publicCoreEffectViolations = decisions.filter(
  (decision) =>
    decision.owner === "public-core" &&
    (isDockerExecutionTargetEffect(decision.path) ||
      explicitPlatformEffectTargets.has(decision.path) ||
      liveAdapterConformancePaths.has(decision.path) ||
      /^services\/control-plane\/internal\/executiontargets\/.*(?:orbstack|kubernetes_reconciler)_integration_test\.go$/u.test(
        decision.path,
      )),
);
if (publicCoreEffectViolations.length !== 0) {
  throw new Error(
    `public core directly retains explicit environment effects: ${publicCoreEffectViolations.map((item) => item.path).join(", ")}`,
  );
}
const synaraKubernetesAdapterViolations = decisions.filter(
  (decision) =>
    decision.finalClassification === "adapter" &&
    (/^deploy\/kubernetes\/(?:admin-|developer-docs\/)/u.test(decision.path) ||
      new Set([
        "deploy/kubernetes/README.md",
        "deploy/kubernetes/acceptance.sh",
        "deploy/kubernetes/config.example.yaml",
        "deploy/kubernetes/deployment.yaml",
        "deploy/kubernetes/kustomization.yaml",
        "deploy/kubernetes/remote-stage6-acceptance.sh",
      ]).has(decision.path)),
);
if (synaraKubernetesAdapterViolations.length !== 0) {
  throw new Error(
    `Synara product deployment is still classified as adapter: ${synaraKubernetesAdapterViolations.map((item) => item.path).join(", ")}`,
  );
}
const decisionTargets = new Set(decisions.map((decision) => decision.target));
if (decisionTargets.size !== expectedInventoryRows) {
  const targetCounts = new Map();
  for (const decision of decisions) {
    targetCounts.set(decision.target, (targetCounts.get(decision.target) ?? 0) + 1);
  }
  const duplicates = [...targetCounts]
    .filter(([, count]) => count > 1)
    .map(([target, count]) => `${target} (${count})`)
    .join(", ");
  throw new Error(`decision output has duplicate targets: ${duplicates}`);
}

const decisionHeaders = [
  "snapshotHead",
  "snapshotTree",
  "path",
  "scope",
  "manualReview",
  "classificationSeed",
  "finalCapability",
  "finalClassification",
  "owner",
  "target",
  "provenance",
  "authorityDisposition",
  "licenseProvenance",
  "secretProvenance",
  "decisionSource",
  "reviewStatus",
  "reviewer",
  "reviewedAt",
  "reason",
];
const encodedDecisions = `${decisionHeaders.join("\t")}\n${decisions
  .map((decision) => decisionHeaders.map((header) => tsv(decision[header])).join("\t"))
  .join("\n")}\n`;
writeFileSync(decisionsPath, encodedDecisions);
const decisionsSha = sha256(encodedDecisions);

const unresolved = decisions.filter(
  (decision) =>
    !decision.finalCapability ||
    !decision.finalClassification ||
    !decision.owner ||
    !decision.target ||
    !decision.provenance ||
    !decision.authorityDisposition ||
    !decision.licenseProvenance ||
    !decision.secretProvenance ||
    !decision.decisionSource ||
    !decision.reviewStatus ||
    !decision.reviewer ||
    !decision.reviewedAt ||
    !decision.reason,
);
if (unresolved.length !== 0) throw new Error(`unresolved decisions remain: ${unresolved.length}`);
const manualDecisions = decisions.filter((decision) => decision.manualReview === "true");

const summary = `# P0 Synara inventory 最终裁决摘要

## 固定输入

- Source：\`${expectedSourceHead}\`
- Source tree：\`${expectedSourceTree}\`
- Inventory：\`${relative(process.cwd(), inventoryPath)}\`
- Inventory SHA-256：\`${inventorySha}\`
- Inventory rows：${rows.length}
- Manual-review input rows：${manualRows.length}
- Decision rows：${decisions.length}
- Decision TSV SHA-256：\`${decisionsSha}\`
- Unresolved/duplicate/missing decisions：**0**
- Source license provenance：\`${sourceLicenseProvenance}\`
- Public-candidate secret provenance：**AUDITED exact-finding triage**；静态测试私钥来源文件为 **REWRITE REQUIRED**

## 最终分类

\`manualReview=true\` 的 ${manualDecisions.length} 条均由 executor explicit semantic rule 决策；该字段不代表人工 owner 已签署 Gate：

${markdownTable(countBy(manualDecisions, "finalClassification"), "Classification")}

完整 ${decisions.length.toLocaleString("en-US")} 条 final manifest（含 legacy root \`COPY . .\` 的完整 tracked build context；seed 仅作输入提示）：

${markdownTable(countBy(decisions, "finalClassification"), "Classification")}

## Owner

${markdownTable(countBy(decisions, "owner"), "Owner")}

## 输入 scope

${markdownTable(countBy(decisions, "scope"), "Scope")}

## Final capability

${markdownTable(countBy(decisions, "finalCapability"), "Capability")}

## 关键裁决

1. \`internal/agentd\` 没有整包搬迁：Worker authority、workspace、checkpoint、credential、Provider Host、process/containment 分入 \`services/worker/internal/*\`；Cocoon、gVisor、Kubernetes、SSH 分入内置 adapter。
2. \`cmd/api -> agentd\` 的现有耦合不作为公共边界继承；agentd client/daemon 必须改写为只依赖 generated Worker SDK/wire，Control Plane 和 Worker 不互相 import \`internal\`。
3. Provider catalog 三文件全部标记 \`rewrite-public\`：旧 \`catalog_gen.go\` 是 orphaned output，旧 generator 指向缺失的 Synara JSON；公共实现以 \`contracts/managed-agent/v1alpha1/provider-capability-catalog.json\` 为 source-of-truth 后重新生成。
4. Synara desktop/mac、Polaris SDK、Stage 6 产品治理和私有运维脚本留在 Synara。公共 Worker 生命周期、隔离、manifest/registry/supply-chain/security conformance 有独立公共目标，Vault oracle 延后到外部 adapter extension。
5. Synara 根 \`package.json\`/\`bun.lock\` 只作为旧镜像 provenance 留在 Synara；根 Dockerfile 不能复制，必须重写为最小、digest-pinned 的 \`deploy/images/worker/Dockerfile\`。
6. 每条 provenance 同时固定 \`source ref:path\`、Git blob OID 和内容 SHA-256；生成器会对 source HEAD/tree/dirty、inventory SHA/行数、blob、重复、缺项和空/unknown 决策 fail closed。
7. Adapter 不得拥有 migration、durable model、routing/KMS lifecycle、HTTP authority 或 receipt truth；混合文件先标 \`rewrite-public\` 并要求 core/port 拆分，直接 Docker/filesystem/webhook/live-cluster effects 单独标 adapter。
8. 旧根 Dockerfile 使用 \`COPY . .\`，所以 inventory 冻结全部 ${decisions.length.toLocaleString("en-US")} 个 tracked blob；不在批准 extraction surface 的 ${decisions.filter((item) => item.scope === "legacy-build-context").length.toLocaleString("en-US")} 项统一 default-deny 留在 Synara，不能进入新的 public image context。

## 全量覆盖

- Deploy：117 条全部进入 final manifest；Synara Admin/Developer Docs/Stage 6 组合留在宿主，公共 CP/Worker Helm 重写，gVisor/Cocoon/Kubernetes actuator 独立 adapter，personal/remote -> public Compose rewrite，Vault production policy -> deferred extension。
- Scripts：235 条全部进入 final manifest；公共 conformance/release 工具与 Synara desktop/Stage 6/Polaris/product release 工具分开 owner/target。
- Contracts：62 条按 Runtime/Worker/Managed Agent/Platform Adapter、Synara product 或 deferred enterprise extension 分域，不把旧 prose 当新 wire authority。
- Go/SQL：Control Plane/Worker/adapter/Synara/deferred/retire 逐文件写入 target；167 个旧 migration 只映射到新 lineage 的 semantic target 或保留面，不继承编号/table identity。
- Root supply chain：21 条显式 root/workspace inputs 与全部 tracked build context 均进入 final manifest；candidate lock/schema 映射公共 release contract/tooling，Dockerfile 重写，Synara root manifests/patches/config 留在宿主。
- CI：10 条全部标记 Synara-owned；公共仓 workflow 必须在 P1 重新设计，不能复制 Synara 权限和发布语义。
- Cross-package edge：固定验证 \`cmd/api/main.go\` 对 \`agentd.RunGitAskPassHelperFromEnvironment\`、\`LocalSupervisor\` 的直接依赖；迁移时必须用 command composition/SDK seam 消除此 internal import。

## 重放

\`\`\`bash
node docs/plan/p0/scripts/finalize-synara-inventory.mjs
shasum -a 256 docs/plan/p0/synara-inventory-decisions.tsv
\`\`\`
`;
writeFileSync(summaryPath, summary);

process.stdout.write(
  `${JSON.stringify(
    {
      sourceHead,
      sourceTree,
      inventorySha256: inventorySha,
      inventoryRows: rows.length,
      manualRows: manualRows.length,
      decisionRows: decisions.length,
      decisionSha256: decisionsSha,
      unresolved: unresolved.length,
      manualClassifications: Object.fromEntries(countBy(manualDecisions, "finalClassification")),
      classifications: Object.fromEntries(countBy(decisions, "finalClassification")),
      owners: Object.fromEntries(countBy(decisions, "owner")),
      capabilities: Object.fromEntries(countBy(decisions, "finalCapability")),
      outputs: [decisionsPath, summaryPath],
    },
    null,
    2,
  )}\n`,
);
