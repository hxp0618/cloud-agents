import { spawnSync } from "node:child_process";
import { dirname, relative, resolve, sep } from "node:path";

import { canonicalizeJson } from "./platform-json-semantics";

import { formatWithOxfmt, PLATFORM_OXFMT_LIBRARY_PATH } from "./platform-oxfmt";
import {
  dependencyFileRecords,
  digestBytes,
  generatedFileRecord,
  normalizedFileManifestDigest,
  outputTreeDigest,
  PLATFORM_SDK_MANIFEST_LIBRARY_PATH,
  readRegularFile,
  SDK_INPUT_MANIFEST_ALGORITHM,
  SDK_OUTPUT_TREE_ALGORITHM,
  writeSDKFiles,
} from "./platform-sdk-manifest";

export const GO_COMMON_JSON_OUTPUT_PATH = "sdk/go/gen/common/v1alpha1/json_generated.go";
export const GO_PLATFORM_JSON_OUTPUT_PATH = "sdk/go/gen/platform/v1alpha1/json_generated.go";
export const GO_OPENAPI_OUTPUT_PATH = "sdk/go/gen/openapi/v1alpha1/client_generated.go";
export const GO_JSON_MANIFEST_PATH = "sdk/go/json-generated-manifest.json";
export const TYPESCRIPT_PLATFORM_OUTPUT_PATH = "sdk/typescript/src/platform.ts";
export const TYPESCRIPT_JSON_MANIFEST_PATH = "sdk/typescript/json-generated-manifest.json";

const GENERATOR_PATH = "scripts/generate-platform-json-sdks.ts";
const LIBRARY_PATH = "scripts/lib/platform-json-sdk.ts";
const GO_COMMON_TEMPLATE_PATH = "scripts/templates/platform-json-sdk-go-common.tmpl";
const GO_PLATFORM_TEMPLATE_PATH = "scripts/templates/platform-json-sdk-go-platform.tmpl";
const GO_OPENAPI_TEMPLATE_PATH = "scripts/templates/platform-json-sdk-go-openapi.tmpl";
const TYPESCRIPT_TEMPLATE_PATH = "scripts/templates/platform-json-sdk-typescript.tmpl";
const ENTRY_PATH = "docs/plan/p1/sdk-identity-closure-entry-20260820.md";
const COMMON_MANIFEST_PATH = "contracts/common/v1alpha1/fixtures/manifest.json";
const PLATFORM_MANIFEST_PATH = "contracts/platform/v1alpha1/fixtures/manifest.json";
const IDENTITY_MANIFEST_PATH = "contracts/identity/v1alpha1/fixtures/manifest.json";
const IDENTITY_OPENAPI_PATH = "contracts/identity/v1alpha1/openapi.json";
const IDENTITY_README_PATH = "contracts/identity/v1alpha1/README.md";
const MANAGED_AGENT_OPENAPI_PATH = "contracts/managed-agent/v1alpha1/openapi.json";
const MANAGED_HOST_OPENAPI_PATH = "contracts/managed-host/v1alpha1/openapi.json";
const GO_MODULE_PATH = "sdk/go/go.mod";
const GO_SUM_PATH = "sdk/go/go.sum";
const GO_NOTICE_PATH = "sdk/go/THIRD_PARTY_NOTICES.md";
const TYPESCRIPT_PACKAGE_PATH = "sdk/typescript/package.json";
const BUN_LOCK_PATH = "bun.lock";
const TYPESCRIPT_NOTICE_PATH = "sdk/typescript/THIRD_PARTY_NOTICES.md";
const IDENTITY_GO_OUTPUT_PATH = "sdk/go/gen/common/v1alpha1/identity_generated.go";
const IDENTITY_TYPESCRIPT_OUTPUT_PATH = "sdk/typescript/src/index.ts";
const IDENTITY_GO_MANIFEST_PATH = "sdk/go/generated-manifest.json";
const IDENTITY_TYPESCRIPT_MANIFEST_PATH = "sdk/typescript/generated-manifest.json";

const COMMON_SCHEMAS = [
  "authorization-scope.schema.json",
  "idempotency-key.schema.json",
  "idempotency.schema.json",
  "identifier.schema.json",
  "organization-authorization-scope.schema.json",
  "organization-ref.schema.json",
  "page-size.schema.json",
  "page-token.schema.json",
  "pagination.schema.json",
  "platform-authorization-scope.schema.json",
  "problem.schema.json",
  "project-authorization-scope.schema.json",
  "project-ref.schema.json",
  "resource-metadata.schema.json",
  "resource-version.schema.json",
  "stable-error.schema.json",
  "namespace-ref.schema.json",
  "subject-ref.schema.json",
  "tenant-authorization-scope.schema.json",
  "tenant-ref.schema.json",
  "watch-cursor-token.schema.json",
  "watch-cursor.schema.json",
] as const;

const PLATFORM_SCHEMAS = [
  "admin-denied-write-event.schema.json",
  "admin-denied-write-event-page.schema.json",
  "admin-sandbox-access-grant-page.schema.json",
  "admin-sandbox-access-grant.schema.json",
  "admin-sandbox-session-page.schema.json",
  "admin-sandbox-session.schema.json",
  "admin-environment-lease-upgrade-request.schema.json",
  "admin-audit-event-page.schema.json",
  "admin-audit-event.schema.json",
  "deployment-target-cleanup-request.schema.json",
  "deployment-target-cleanup-preview.schema.json",
  "deployment-target-scheduling-request.schema.json",
  "deployment-target-scheduling-preview.schema.json",
  "deployment-target-page.schema.json",
  "deployment-target-probe-request.schema.json",
  "deployment-target-register-request.schema.json",
  "deployment-target.schema.json",
  "environment-lease-create-request.schema.json",
  "environment-lease-page.schema.json",
  "environment-lease-terminate-request.schema.json",
  "environment-lease-upgrade-preview.schema.json",
  "environment-lease-upgrade-request.schema.json",
  "environment-lease.schema.json",
  "environment-profile-create-request.schema.json",
  "environment-profile-page.schema.json",
  "environment-profile-summary-page.schema.json",
  "environment-profile-summary.schema.json",
  "environment-profile-transition-request.schema.json",
  "environment-profile.schema.json",
  "worker-page.schema.json",
  "worker-health-observation.schema.json",
  "worker-health-status.schema.json",
  "worker-release-page.schema.json",
  "worker-release-register-request.schema.json",
  "worker-release.schema.json",
  "worker.schema.json",
  "workspace-snapshot-create-request.schema.json",
  "workspace-snapshot-cleanup-request.schema.json",
  "workspace-snapshot-page.schema.json",
  "workspace-snapshot-restore-request.schema.json",
  "workspace-snapshot.schema.json",
  "user-environment-create-request.schema.json",
  "user-environment-terminate-request.schema.json",
  "user-environment.schema.json",
  "managed-agent-create-project-organization-ref.schema.json",
  "membership-create-request.schema.json",
  "membership-page.schema.json",
  "membership.schema.json",
  "maintenance-operation-page.schema.json",
  "maintenance-operation.schema.json",
  "mcp-server-create-request.schema.json",
  "mcp-server-page.schema.json",
  "mcp-server-revoke-request.schema.json",
  "mcp-server.schema.json",
  "membership-transition-request.schema.json",
  "organization-create-request.schema.json",
  "organization-page.schema.json",
  "organization.schema.json",
  "permission.schema.json",
  "platform-tenant.schema.json",
  "project-lease-quota-set-request.schema.json",
  "project-lease-quota-summary.schema.json",
  "project-lease-quota.schema.json",
  "project-create-request.schema.json",
  "project-page.schema.json",
  "project.schema.json",
  "rbac-mutation-result.schema.json",
  "remote-worker-certificate-issue-request.schema.json",
  "remote-worker-certificate.schema.json",
  "remote-worker-command-receipt.schema.json",
  "remote-worker-command.schema.json",
  "remote-worker-sandbox-exec-command-receipt.schema.json",
  "remote-worker-sandbox-exec-command.schema.json",
  "remote-worker-sandbox-file-command-receipt.schema.json",
  "remote-worker-sandbox-file-command.schema.json",
  "remote-worker-sandbox-pty-command-receipt.schema.json",
  "remote-worker-sandbox-pty-command.schema.json",
  "remote-worker-sandbox-preview-command-receipt.schema.json",
  "remote-worker-sandbox-preview-command.schema.json",
  "remote-worker-sandbox-command-receipt.schema.json",
  "remote-worker-sandbox-command.schema.json",
  "remote-worker-workspace-snapshot-command-receipt.schema.json",
  "remote-worker-workspace-snapshot-command.schema.json",
  "remote-worker-heartbeat-request.schema.json",
  "remote-worker-heartbeat.schema.json",
  "remote-worker-enrollment-create-request.schema.json",
  "remote-worker-enrollment-page.schema.json",
  "remote-worker-enrollment-revoke-request.schema.json",
  "remote-worker-enrollment-secret-claim-request.schema.json",
  "remote-worker-enrollment-secret.schema.json",
  "remote-worker-enrollment.schema.json",
  "remote-worker-node-scheduling-preview.schema.json",
  "remote-worker-node-scheduling-request.schema.json",
  "remote-worker-node-status.schema.json",
  "role-binding-create-request.schema.json",
  "role-binding-page.schema.json",
  "role-binding.schema.json",
  "role-binding-revoke-request.schema.json",
  "role-page.schema.json",
  "role.schema.json",
  "runtime-profile-create-request.schema.json",
  "runtime-profile-page.schema.json",
  "runtime-profile-summary-page.schema.json",
  "runtime-profile-summary.schema.json",
  "runtime-profile-transition-request.schema.json",
  "runtime-profile.schema.json",
  "sandbox-exec-request.schema.json",
  "sandbox-exec-result.schema.json",
  "sandbox-file-entry.schema.json",
  "sandbox-file-page.schema.json",
  "sandbox-file-read-page.schema.json",
  "sandbox-file-write-request.schema.json",
  "sandbox-access-grant-create-request.schema.json",
  "sandbox-access-grant-revoke-request.schema.json",
  "sandbox-access-grant.schema.json",
  "sandbox-pty-session.schema.json",
  "sandbox-preview-port.schema.json",
  "sandbox-session-create-request.schema.json",
  "sandbox-session-lifecycle-operation.schema.json",
  "sandbox-session-lifecycle-request.schema.json",
  "sandbox-usage-correction-request.schema.json",
  "sandbox-session.schema.json",
  "storage-policy-page.schema.json",
  "storage-policy-set-request.schema.json",
  "storage-policy.schema.json",
  "skill-bundle-create-request.schema.json",
  "skill-bundle-page.schema.json",
  "skill-bundle-revoke-request.schema.json",
  "skill-bundle.schema.json",
  "network-policy-page.schema.json",
  "network-policy-set-request.schema.json",
  "network-policy.schema.json",
] as const;
const MANAGED_AGENT_SCHEMAS = [
  "artifact-content-disposition.schema.json",
  "artifact.schema.json",
  "event-page.schema.json",
  "event.schema.json",
  "execution-approval-resolution-request.schema.json",
  "execution-cancel-request.schema.json",
  "execution-interrupt-request.schema.json",
  "execution-side-effect-reconciliation-request.schema.json",
  "execution-create-request.schema.json",
  "execution-page.schema.json",
  "execution.schema.json",
  "execution-user-input-resolution-request.schema.json",
  "message-index.schema.json",
  "capability-event.schema.json",
  "session-create-request.schema.json",
  "session-page.schema.json",
  "session.schema.json",
  "turn-create-request.schema.json",
  "turn-page.schema.json",
  "turn.schema.json",
  "www-authenticate.schema.json",
  "event-cursor.schema.json",
  "event-limit.schema.json",
] as const;
const IDENTITY_SCHEMAS = [
  "browser-session.schema.json",
  "browser-tenant-page.schema.json",
  "browser-tenant.schema.json",
  "current-user.schema.json",
  "email-suffix-policy-update.schema.json",
  "email-suffix-policy.schema.json",
  "identity-jwks.schema.json",
  "identity-account.schema.json",
  "identity-account-page.schema.json",
  "identity-audit-event.schema.json",
  "identity-audit-page.schema.json",
  "invitation.schema.json",
  "invitation-create-request.schema.json",
  "invitation-created.schema.json",
  "invitation-page.schema.json",
  "invitation-accept-request.schema.json",
  "login-provider.schema.json",
  "login-provider-page.schema.json",
  "provider-authorization-request.schema.json",
  "provider-authorization.schema.json",
  "provider-callback-request.schema.json",
  "provider-callback.schema.json",
  "login-method.schema.json",
  "login-method-list.schema.json",
  "password-reauth-request.schema.json",
  "reauthentication.schema.json",
  "enable-password-request.schema.json",
  "provider-client.schema.json",
  "provider-client-page.schema.json",
  "provider-client-update.schema.json",
  "password-login-request.schema.json",
  "password-change-request.schema.json",
  "password-reset-created.schema.json",
  "password-reset-accept-request.schema.json",
  "tenant-token-issue-request.schema.json",
  "tenant-token.schema.json",
  "tenant-token-authorization-request.schema.json",
  "tenant-token-authorization.schema.json",
  "token-status-request.schema.json",
  "token-status.schema.json",
] as const;

const SELECTED_COMMON_SCHEMA_REFS = new Set(COMMON_SCHEMAS.map((name) => `../schemas/${name}`));
const SELECTED_PLATFORM_SCHEMA_REFS = new Set(PLATFORM_SCHEMAS.map((name) => `../schemas/${name}`));
const SELECTED_IDENTITY_SCHEMA_REFS = new Set(IDENTITY_SCHEMAS.map((name) => `../schemas/${name}`));

type FixtureManifest = {
  readonly cases: ReadonlyArray<{
    readonly schema?: unknown;
    readonly instance?: unknown;
    readonly document?: unknown;
  }>;
};

type GeneratedOutput = { readonly path: string; readonly source: string };

const JSON_SDK_CONFIG = {
  profile: "cloud-agents-json-contract-sdk/v1alpha1",
  jsonAuthority: "JSON Schema 2020-12",
  routeAuthority: "OpenAPI 3.1.1 HTTP metadata only",
  implementationBoundary: {
    gateClosure: false,
    publication: "NOT_AUTHORIZED",
  },
} as const;

export function platformJSONSDKGeneratorSources(): string[] {
  return [
    GENERATOR_PATH,
    LIBRARY_PATH,
    PLATFORM_OXFMT_LIBRARY_PATH,
    "scripts/lib/platform-json-semantics.ts",
    GO_COMMON_TEMPLATE_PATH,
    GO_PLATFORM_TEMPLATE_PATH,
    GO_OPENAPI_TEMPLATE_PATH,
    TYPESCRIPT_TEMPLATE_PATH,
    PLATFORM_SDK_MANIFEST_LIBRARY_PATH,
  ].toSorted();
}

export function platformJSONSDKContractInputs(root: string): string[] {
  const inputs = [
    ENTRY_PATH,
    COMMON_MANIFEST_PATH,
    PLATFORM_MANIFEST_PATH,
    IDENTITY_MANIFEST_PATH,
    IDENTITY_OPENAPI_PATH,
    IDENTITY_README_PATH,
    MANAGED_AGENT_OPENAPI_PATH,
    MANAGED_HOST_OPENAPI_PATH,
    ...COMMON_SCHEMAS.map((name) => `contracts/common/v1alpha1/schemas/${name}`),
    ...PLATFORM_SCHEMAS.map((name) => `contracts/platform/v1alpha1/schemas/${name}`),
    ...MANAGED_AGENT_SCHEMAS.map((name) => `contracts/managed-agent/v1alpha1/schemas/${name}`),
    ...IDENTITY_SCHEMAS.map((name) => `contracts/identity/v1alpha1/schemas/${name}`),
    ...selectedFixtures(root, COMMON_MANIFEST_PATH, SELECTED_COMMON_SCHEMA_REFS),
    ...selectedFixtures(root, PLATFORM_MANIFEST_PATH, SELECTED_PLATFORM_SCHEMA_REFS),
    ...selectedFixtures(root, IDENTITY_MANIFEST_PATH, SELECTED_IDENTITY_SCHEMA_REFS),
  ].toSorted();
  if (new Set(inputs).size !== inputs.length)
    throw new Error("JSON SDK contract inputs must be unique.");
  return inputs;
}

export function platformJSONSDKConfigDigest(): string {
  return digestBytes(canonicalizeJson(JSON_SDK_CONFIG));
}

export function buildPlatformJSONSDKOutputs(root: string): ReadonlyArray<GeneratedOutput> {
  validateJSONSDKAuthority(root);
  return [
    [GO_COMMON_JSON_OUTPUT_PATH, GO_COMMON_TEMPLATE_PATH],
    [GO_PLATFORM_JSON_OUTPUT_PATH, GO_PLATFORM_TEMPLATE_PATH],
    [GO_OPENAPI_OUTPUT_PATH, GO_OPENAPI_TEMPLATE_PATH],
    [TYPESCRIPT_PLATFORM_OUTPUT_PATH, TYPESCRIPT_TEMPLATE_PATH],
  ].map(([path, template]) => ({
    path,
    source: formatOutput(root, path, readText(root, template)),
  }));
}

export function expectedPlatformJSONSDKFiles(root: string): ReadonlyArray<GeneratedOutput> {
  const outputs = buildPlatformJSONSDKOutputs(root);
  return [...outputs, ...buildPlatformJSONSDKManifests(root, outputs)];
}

export function buildPlatformJSONSDKManifests(
  root: string,
  outputs = buildPlatformJSONSDKOutputs(root),
): ReadonlyArray<GeneratedOutput> {
  const contractInputs = platformJSONSDKContractInputs(root);
  const generatorSources = platformJSONSDKGeneratorSources();
  const common = {
    formatVersion: "cloud-agents-generated-sdk-manifest/v2",
    profile: JSON_SDK_CONFIG.profile,
    status: "GENERATED_NON_GATE_EVIDENCE",
    notGateClosure: true,
    contract: {
      inputManifestAlgorithm: SDK_INPUT_MANIFEST_ALGORITHM,
      inputManifestSha256: normalizedFileManifestDigest(root, contractInputs),
      inputs: contractInputs,
    },
    generator: {
      id: "platform-json-contract-sdk-generator",
      version: "v2",
      entrypoint: GENERATOR_PATH,
      sourceManifestAlgorithm: SDK_INPUT_MANIFEST_ALGORITHM,
      sourceManifestSha256: normalizedFileManifestDigest(root, generatorSources),
      sources: generatorSources,
      configDigest: platformJSONSDKConfigDigest(),
      dependencies: dependencyFileRecords(root, [
        ["mise", ".mise.toml"],
        ["rootPackage", "package.json"],
        ["bunLock", BUN_LOCK_PATH],
        ["identityGoOutput", IDENTITY_GO_OUTPUT_PATH],
        ["identityTypeScriptOutput", IDENTITY_TYPESCRIPT_OUTPUT_PATH],
        ["identityGoManifest", IDENTITY_GO_MANIFEST_PATH],
        ["identityTypeScriptManifest", IDENTITY_TYPESCRIPT_MANIFEST_PATH],
      ]),
    },
    implementationBoundary: JSON_SDK_CONFIG.implementationBoundary,
  } as const;
  const byPath = new Map(outputs.map((output) => [output.path, output]));
  const packageMetadata = typescriptPackageMetadata(root);
  const manifests = [
    {
      language: "go",
      packageIdentity: "github.com/hxp0618/cloud-agents/sdk/go",
      packagePrivate: undefined,
      dependencyFiles: dependencyFileRecords(root, [
        ["goMod", GO_MODULE_PATH],
        ["goSum", GO_SUM_PATH],
        ["notice", GO_NOTICE_PATH],
      ]),
      outputPaths: [
        GO_COMMON_JSON_OUTPUT_PATH,
        GO_PLATFORM_JSON_OUTPUT_PATH,
        GO_OPENAPI_OUTPUT_PATH,
      ],
      manifestPath: GO_JSON_MANIFEST_PATH,
    },
    {
      language: "typescript",
      packageIdentity: `${packageMetadata.name}/platform`,
      packagePrivate: packageMetadata.private,
      dependencyFiles: dependencyFileRecords(root, [
        ["package", TYPESCRIPT_PACKAGE_PATH],
        ["bunLock", BUN_LOCK_PATH],
        ["notice", TYPESCRIPT_NOTICE_PATH],
      ]),
      outputPaths: [TYPESCRIPT_PLATFORM_OUTPUT_PATH],
      manifestPath: TYPESCRIPT_JSON_MANIFEST_PATH,
    },
  ] as const;
  return manifests.map((manifest) => {
    const files = manifest.outputPaths.map((path) => {
      const output = byPath.get(path);
      if (output === undefined) throw new Error(`Missing JSON SDK output ${path}.`);
      return generatedFileRecord(output.path, output.source);
    });
    return {
      path: manifest.manifestPath,
      source: `${JSON.stringify(
        {
          ...common,
          language: manifest.language,
          packageIdentity: manifest.packageIdentity,
          packagePrivate: manifest.packagePrivate,
          runtimeDependencies: [],
          dependencyFiles: manifest.dependencyFiles,
          outputTreeAlgorithm: SDK_OUTPUT_TREE_ALGORITHM,
          outputTreeSha256: outputTreeDigest(files),
          outputs: files,
        },
        null,
        2,
      )}\n`,
    };
  });
}

export function assertPlatformJSONSDKCurrent(root: string): void {
  for (const output of expectedPlatformJSONSDKFiles(root)) {
    if (readText(root, output.path) !== output.source) {
      throw new Error(
        `${output.path} is stale; run bun scripts/generate-platform-json-sdks.ts --write.`,
      );
    }
  }
}

export function writePlatformJSONSDKFiles(root: string): void {
  writeSDKFiles(
    root,
    expectedPlatformJSONSDKFiles(root).map((output) => ({
      path: output.path,
      bytes: output.source,
    })),
  );
}

function selectedFixtures(
  root: string,
  manifestPath: string,
  selectedSchemas: ReadonlySet<string>,
): string[] {
  const manifest = readJSON<FixtureManifest>(root, manifestPath);
  const fixtureRoot = dirname(manifestPath);
  return manifest.cases
    .filter((entry) => typeof entry.schema === "string" && selectedSchemas.has(entry.schema))
    .map((entry) => {
      const fixture = entry.document ?? entry.instance;
      if (typeof fixture !== "string")
        throw new Error(`Fixture in ${manifestPath} must reference a file.`);
      return normalizeRelativePath(resolve(root, fixtureRoot, fixture), root);
    });
}

function validateJSONSDKAuthority(root: string): void {
  const agent = readJSON<Record<string, unknown>>(root, MANAGED_AGENT_OPENAPI_PATH);
  const host = readJSON<Record<string, unknown>>(root, MANAGED_HOST_OPENAPI_PATH);
  const identity = readJSON<Record<string, unknown>>(root, IDENTITY_OPENAPI_PATH);
  if (agent.openapi !== "3.1.1" || host.openapi !== "3.1.1" || identity.openapi !== "3.1.1") {
    throw new Error("JSON SDK OpenAPI authority must remain OpenAPI 3.1.1.");
  }
  const operations = [
    ...openAPIOperations(agent),
    ...openAPIOperations(host),
    ...openAPIOperations(identity),
  ].toSorted();
  const expected = [
    "adminBindRole",
    "adminCleanupDeploymentTarget",
    "adminCleanupWorkspaceSnapshot",
    "adminCorrectSandboxUsage",
    "adminCreateEnvironmentProfile",
    "adminCreateMembership",
    "adminCreateServiceAccount",
    "adminCreateMcpServer",
    "adminCreateOrganization",
    "adminCreateProject",
    "adminCreateRemoteWorkerEnrollment",
    "adminCreateRuntimeProfile",
    "adminCreateSkillBundle",
    "adminCreateWorkspaceSnapshot",
    "adminDisableEnvironmentProfile",
    "adminDisableRuntimeProfile",
    "adminGetDeploymentTarget",
    "adminGetEnvironmentLease",
    "adminGetEnvironmentProfile",
    "adminGetMembership",
    "adminGetNetworkPolicy",
    "adminGetMcpServer",
    "adminGetOrganization",
    "adminGetPlatformTenant",
    "adminGetProject",
    "adminGetProjectLeaseQuota",
    "adminGetRemoteWorkerEnrollment",
    "adminGetRuntimeProfile",
    "adminGetRole",
    "adminGetRoleBinding",
    "adminGetSkillBundle",
    "adminGetSandboxSession",
    "adminGetStoragePolicy",
    "adminGetWorkerHealth",
    "adminGetWorkspaceSnapshot",
    "adminListDeniedWriteEvents",
    "adminListDeploymentTargetAuditEvents",
    "adminListDeploymentTargetOperations",
    "adminListDeploymentTargets",
    "adminListEnvironmentLeases",
    "adminListEnvironmentProfileAuditEvents",
    "adminListEnvironmentProfiles",
    "adminListMemberships",
    "adminListServiceAccounts",
    "adminListMaintenanceOperations",
    "adminListMcpServers",
    "adminListNetworkPolicies",
    "adminListNetworkPolicyAuditEvents",
    "adminListProjectLeaseQuotaAuditEvents",
    "adminListRemoteWorkerEnrollmentAuditEvents",
    "adminListRemoteWorkerEnrollments",
    "adminListRemoteWorkerOperations",
    "adminListOrganizations",
    "adminListProjects",
    "adminListRoleBindings",
    "adminListRoles",
    "adminListRuntimeProfiles",
    "adminListSkillBundles",
    "adminListSandboxAccessGrants",
    "adminListSandboxSessions",
    "adminListStoragePolicies",
    "adminListStoragePolicyAuditEvents",
    "adminListWorkerReleases",
    "adminListWorkers",
    "adminListWorkspaceSnapshots",
    "adminPreviewDeploymentTargetCleanup",
    "adminPreviewDeploymentTargetScheduling",
    "adminPreviewEnvironmentLeaseRollback",
    "adminPreviewEnvironmentLeaseUpgrade",
    "adminPreviewRemoteWorkerScheduling",
    "adminProbeDeploymentTarget",
    "adminPublishEnvironmentProfile",
    "adminPublishRuntimeProfile",
    "adminRebuildSandboxSession",
    "adminRegisterDeploymentTarget",
    "adminRegisterWorkerRelease",
    "adminRestoreWorkspaceSnapshot",
    "adminRevokeRemoteWorkerEnrollment",
    "adminRevokeMembership",
    "adminRevokeRoleBinding",
    "adminRotateServiceAccountCredential",
    "adminRevokeMcpServer",
    "adminRevokeSandboxAccessGrant",
    "adminRevokeSkillBundle",
    "adminRollbackEnvironmentLease",
    "adminSetNetworkPolicy",
    "adminSetProjectLeaseQuota",
    "adminSetStoragePolicy",
    "adminStopSandboxSession",
    "adminSuspendMembership",
    "adminDisableServiceAccount",
    "adminTransitionDeploymentTargetScheduling",
    "adminTransitionRemoteWorkerScheduling",
    "adminUpgradeEnvironmentLease",
    "adminResumeMembership",
    "foundationCreatePTYSession",
    "foundationCreateSandbox",
    "foundationCreateSandboxAccessGrant",
    "foundationDeletePTYSession",
    "foundationDeleteSandboxFile",
    "foundationExecSandbox",
    "foundationGetPTYSession",
    "foundationListRuntimeProfiles",
    "foundationListSandboxFiles",
    "foundationReadSandboxFile",
    "foundationRegisterSandboxPreviewPort",
    "foundationRevokeSandboxPreviewPort",
    "foundationWriteSandboxFile",
    "identityAcceptInvitation",
    "identityApproveCLIAuthorization",
    "identityAuthorizePrincipalToken",
    "identityExchangeCLIGrant",
    "identityIssueAutomationTenantToken",
    "identityIssueCLITenantToken",
    "identityListCLITenants",
    "identityListControlPlaneAuditEvents",
    "identityRevokeCLIGrant",
    "identityStartCLIAuthorization",
    "identityCreateInvitation",
    "identityListInvitations",
    "identityRevokeInvitation",
    "identityAcceptPasswordReset",
    "identityChangePassword",
    "identityCompleteProviderAuthorization",
    "identityDisableAccount",
    "identityEnablePassword",
    "identityIssuePasswordReset",
    "identityListAccounts",
    "identityListAuditEvents",
    "identityListLoginMethods",
    "identityListLoginProviders",
    "identityListProviderClients",
    "identityListTenantAuditEvents",
    "identityListTenantAccounts",
    "identityAuthorizeTenantToken",
    "identityCheckTokenStatus",
    "identityGetBrowserSession",
    "identityGetCurrentUser",
    "identityGetEmailSuffixPolicy",
    "identityGetJWKS",
    "identityIssueTenantToken",
    "identityListBrowserTenants",
    "identityLogoutBrowserSession",
    "identityPasswordLogin",
    "identityPasswordReauthenticate",
    "identityStartProviderAuthorization",
    "identityUnlinkLoginMethod",
    "identityUpdateEmailSuffixPolicy",
    "identityUpdateProviderClient",
    "managedAgentBindRole",
    "managedAgentCancelExecution",
    "managedAgentCloseSession",
    "managedAgentCreateEnvironment",
    "managedAgentCreateMembership",
    "managedAgentCreateOrganization",
    "managedAgentCreateProject",
    "managedAgentCreateSession",
    "managedAgentCreateTurn",
    "managedAgentDownloadArtifact",
    "managedAgentExecute",
    "managedAgentGetEnvironment",
    "managedAgentGetExecution",
    "managedAgentGetMembership",
    "managedAgentGetOrganization",
    "managedAgentGetPlatformTenant",
    "managedAgentGetProject",
    "managedAgentGetProjectLeaseQuota",
    "managedAgentGetRole",
    "managedAgentGetRoleBinding",
    "managedAgentGetSession",
    "managedAgentGetTurn",
    "managedAgentInterruptExecution",
    "managedAgentListEnvironmentProfiles",
    "managedAgentListEvents",
    "managedAgentListExecutions",
    "managedAgentListMemberships",
    "managedAgentListMyProjects",
    "managedAgentListOrganizations",
    "managedAgentListProjects",
    "managedAgentListRoleBindings",
    "managedAgentListRoles",
    "managedAgentListSessions",
    "managedAgentListTurns",
    "managedAgentReconcileSideEffect",
    "managedAgentResolveApproval",
    "managedAgentResolveUserInput",
    "managedAgentResumeMembership",
    "managedAgentRevokeMembership",
    "managedAgentRevokeRoleBinding",
    "managedAgentSuspendMembership",
    "managedAgentTerminateEnvironment",
    "managedHostGetProjectContext",
    "managedHostGetRoleBinding",
    "remoteWorkerClaimEnrollmentSecret",
    "remoteWorkerHeartbeat",
    "remoteWorkerIssueCertificate",
    "remoteWorkerRotateCertificate",
  ];
  if (JSON.stringify(operations) !== JSON.stringify(expected.toSorted())) {
    throw new Error(`OpenAPI operation set changed: ${operations.join(",")}`);
  }
}

function openAPIOperations(document: Record<string, unknown>): string[] {
  const paths = document.paths as Record<string, Record<string, { operationId?: unknown }>>;
  return Object.values(paths).flatMap((item) =>
    Object.values(item).flatMap((operation) =>
      typeof operation.operationId === "string" ? [operation.operationId] : [],
    ),
  );
}

function readJSON<T>(root: string, path: string): T {
  return JSON.parse(readText(root, path)) as T;
}

function readText(root: string, path: string): string {
  return readRegularFile(root, path).toString("utf8");
}

function formatOutput(root: string, path: string, source: string): string {
  if (!path.endsWith(".go")) return formatWithOxfmt(root, path, source);
  const result = spawnSync("gofmt", [], {
    input: source,
    encoding: "utf8",
    cwd: root,
    timeout: 30_000,
    killSignal: "SIGTERM",
  });
  if (result.error) {
    throw new Error(`Formatter failed for ${path}: ${result.error.message}`);
  }
  if (result.signal) {
    throw new Error(`Formatter failed for ${path}: terminated by ${result.signal}`);
  }
  if (result.status !== 0) {
    throw new Error(`Formatter failed for ${path}: ${result.stderr.trim()}`);
  }
  return result.stdout;
}

function normalizeRelativePath(target: string, root: string): string {
  const value = relative(root, target).split(sep).join("/");
  if (value === "" || value === ".." || value.startsWith("../"))
    throw new Error("Path escapes root.");
  return value;
}

function typescriptPackageMetadata(root: string): { name: string; private: true } {
  const packageJSON = readJSON<{ readonly name?: unknown; readonly private?: unknown }>(
    root,
    TYPESCRIPT_PACKAGE_PATH,
  );
  if (typeof packageJSON.name !== "string" || !packageJSON.name.startsWith("@cloud-agents/")) {
    throw new Error(
      `TypeScript SDK package name must be a public @cloud-agents package: ${String(packageJSON.name)}.`,
    );
  }
  if (packageJSON.private !== true) {
    throw new Error("TypeScript SDK package must declare private: true.");
  }
  return { name: packageJSON.name, private: true };
}
