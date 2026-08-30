import { createHash } from "node:crypto";
import { spawnSync } from "node:child_process";
import { lstatSync, mkdirSync, readFileSync, writeFileSync } from "node:fs";
import { dirname, relative, resolve, sep } from "node:path";

import { canonicalizeJson } from "./platform-json-semantics";
import {
  formatWithOxfmt,
  PLATFORM_OXFMT_LIBRARY_PATH,
  PLATFORM_OXFMT_TEST_PATH,
} from "./platform-oxfmt";

export const GO_COMMON_JSON_OUTPUT_PATH = "sdk/go/gen/common/v1alpha1/json_generated.go";
export const GO_PLATFORM_JSON_OUTPUT_PATH = "sdk/go/gen/platform/v1alpha1/json_generated.go";
export const GO_OPENAPI_OUTPUT_PATH = "sdk/go/gen/openapi/v1alpha1/client_generated.go";
export const GO_JSON_MANIFEST_PATH = "sdk/go/json-generated-manifest.json";
export const TYPESCRIPT_PLATFORM_OUTPUT_PATH = "sdk/typescript/src/platform.ts";
export const TYPESCRIPT_JSON_MANIFEST_PATH = "sdk/typescript/json-generated-manifest.json";

const GO_COMMON_JSON_TEST_PATH = "sdk/go/gen/common/v1alpha1/json_generated_test.go";
const GO_PLATFORM_JSON_TEST_PATH = "sdk/go/gen/platform/v1alpha1/json_generated_test.go";
const GO_OPENAPI_TEST_PATH = "sdk/go/gen/openapi/v1alpha1/client_generated_test.go";
const TYPESCRIPT_PLATFORM_TEST_PATH = "sdk/typescript/src/platform.test.ts";

const GENERATOR_PATH = "scripts/generate-platform-json-sdks.ts";
const LIBRARY_PATH = "scripts/lib/platform-json-sdk.ts";
const TEST_PATH = "scripts/lib/platform-json-sdk.test.ts";
const GO_COMMON_TEMPLATE_PATH = "scripts/templates/platform-json-sdk-go-common.tmpl";
const GO_PLATFORM_TEMPLATE_PATH = "scripts/templates/platform-json-sdk-go-platform.tmpl";
const GO_OPENAPI_TEMPLATE_PATH = "scripts/templates/platform-json-sdk-go-openapi.tmpl";
const TYPESCRIPT_TEMPLATE_PATH = "scripts/templates/platform-json-sdk-typescript.tmpl";
const ENTRY_PATH = "docs/plan/p1/sdk-identity-closure-entry-20260820.md";
const COMMON_MANIFEST_PATH = "contracts/common/v1alpha1/fixtures/manifest.json";
const PLATFORM_MANIFEST_PATH = "contracts/platform/v1alpha1/fixtures/manifest.json";
const MANAGED_AGENT_OPENAPI_PATH = "contracts/managed-agent/v1alpha1/openapi.json";
const MANAGED_HOST_OPENAPI_PATH = "contracts/managed-host/v1alpha1/openapi.json";
const MANIFEST_ALGORITHM = "sorted-path-nul-sha256-nul-git-mode-v1";
const OUTPUT_TREE_ALGORITHM = "sorted-path-nul-sha256-nul-v1";
const JSON_SDK_AUTHORITY_PROFILE_ID = "cloud-agents-json-contract-sdk-model-authority/v1alpha1";
const JSON_SDK_AUTHORITY_PROFILE_SHA256 =
	"sha256:fbe6fafc2d7baaa50b0aed345b1ad078b1f0dc618fee127bdf5e0630bccafeed";

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
  "subject-ref.schema.json",
  "tenant-authorization-scope.schema.json",
  "tenant-ref.schema.json",
  "watch-cursor-token.schema.json",
  "watch-cursor.schema.json",
] as const;

const PLATFORM_SCHEMAS = [
	"environment-lease-create-request.schema.json",
	"environment-lease-terminate-request.schema.json",
	"environment-lease.schema.json",
	"managed-agent-create-project-organization-ref.schema.json",
	"membership-create-request.schema.json",
	"membership.schema.json",
	"membership-transition-request.schema.json",
	"organization-create-request.schema.json",
	"organization-page.schema.json",
	"organization.schema.json",
	"permission.schema.json",
	"platform-tenant.schema.json",
	"project-create-request.schema.json",
	"project.schema.json",
	"rbac-mutation-result.schema.json",
	"role-binding-create-request.schema.json",
	"role-binding.schema.json",
	"role-binding-revoke-request.schema.json",
	"role.schema.json",
] as const;
const MANAGED_AGENT_SCHEMAS = [
	"event-page.schema.json",
	"event.schema.json",
	"execution-cancel-request.schema.json",
	"execution-interrupt-request.schema.json",
	"execution-create-request.schema.json",
  "execution.schema.json",
  "session-create-request.schema.json",
  "session.schema.json",
  "turn-create-request.schema.json",
  "turn.schema.json",
] as const;

const SELECTED_COMMON_SCHEMA_REFS = new Set(COMMON_SCHEMAS.map((name) => `../schemas/${name}`));
const SELECTED_PLATFORM_SCHEMA_REFS = new Set(PLATFORM_SCHEMAS.map((name) => `../schemas/${name}`));

const JSON_SDK_CONFIG = {
  profile: "cloud-agents-json-contract-sdk/v1alpha1",
  jsonAuthority: "JSON Schema 2020-12",
  routeAuthority: "OpenAPI 3.1.1 HTTP metadata only",
  mutationUnknownFields: "REJECT",
  responseUnknownFields: "PRESERVE_IN_EXPLICIT_SIDECAR_ONLY",
  nMinusOneReader: true,
  clients: {
    transport: "INJECTED_FIXTURE_TRANSPORT_ONLY",
    productionHTTP: "SERVER_ROUTE_IMPLEMENTED_CLIENT_TRANSPORT_INJECTED",
    cancellation: "CONTEXT_OR_ABORT_SIGNAL",
  },
  serverSeam: {
    language: "go",
    routeRegistration: "NOT_IMPLEMENTED",
    pathBodyAuthority: "PATH_TENANT_ONLY",
  },
  implementationBoundary: {
    gateClosure: false,
    httpSurface: "PRODUCTION_GO_ROUTE_AND_INJECTED_CLIENT",
    p2Surface: "NOT_IMPLEMENTED",
    providerSideEffects: "FORBIDDEN",
    productionDatabaseWrites: "CONTROL_PLANE_ROUTE_IMPLEMENTED_OUTSIDE_SDK",
    deployment: "COMPOSE_DEPLOYMENT_CONFIGURED",
    publication: "NOT_AUTHORIZED",
  },
  authorityProfile: {
    id: JSON_SDK_AUTHORITY_PROFILE_ID,
    manifestAlgorithm: MANIFEST_ALGORITHM,
    manifestSha256: JSON_SDK_AUTHORITY_PROFILE_SHA256,
  },
} as const;

type FixtureManifest = {
  readonly cases: ReadonlyArray<{
    readonly schema?: unknown;
    readonly instance?: unknown;
    readonly document?: unknown;
  }>;
};

type GeneratedOutput = { readonly path: string; readonly source: string };
type GeneratedFileRecord = {
  readonly path: string;
  readonly sha256: string;
  readonly sizeBytes: number;
};

export function platformJSONSDKGeneratorSources(): string[] {
  return [
    GENERATOR_PATH,
    LIBRARY_PATH,
    TEST_PATH,
    PLATFORM_OXFMT_LIBRARY_PATH,
    PLATFORM_OXFMT_TEST_PATH,
    GO_COMMON_TEMPLATE_PATH,
    GO_PLATFORM_TEMPLATE_PATH,
    GO_OPENAPI_TEMPLATE_PATH,
    TYPESCRIPT_TEMPLATE_PATH,
    GO_COMMON_JSON_TEST_PATH,
    GO_PLATFORM_JSON_TEST_PATH,
    GO_OPENAPI_TEST_PATH,
    TYPESCRIPT_PLATFORM_TEST_PATH,
  ].toSorted();
}

export function platformJSONSDKContractInputs(root: string): string[] {
  const inputs = [
    ENTRY_PATH,
    COMMON_MANIFEST_PATH,
    PLATFORM_MANIFEST_PATH,
    MANAGED_AGENT_OPENAPI_PATH,
    MANAGED_HOST_OPENAPI_PATH,
    ...COMMON_SCHEMAS.map((name) => `contracts/common/v1alpha1/schemas/${name}`),
    ...PLATFORM_SCHEMAS.map((name) => `contracts/platform/v1alpha1/schemas/${name}`),
    ...MANAGED_AGENT_SCHEMAS.map((name) => `contracts/managed-agent/v1alpha1/schemas/${name}`),
    ...selectedFixtures(root, COMMON_MANIFEST_PATH, SELECTED_COMMON_SCHEMA_REFS),
    ...selectedFixtures(root, PLATFORM_MANIFEST_PATH, SELECTED_PLATFORM_SCHEMA_REFS),
  ].toSorted();
  if (new Set(inputs).size !== inputs.length)
    throw new Error("JSON SDK contract inputs must be unique.");
  return inputs;
}

export function platformJSONSDKConfigDigest(): string {
  return digestBytes(canonicalizeJson(JSON_SDK_CONFIG));
}

export function platformJSONSDKAuthorityProfileInputs(): string[] {
  return [
    MANAGED_AGENT_OPENAPI_PATH,
    MANAGED_HOST_OPENAPI_PATH,
    ...COMMON_SCHEMAS.map((name) => `contracts/common/v1alpha1/schemas/${name}`),
    ...PLATFORM_SCHEMAS.map((name) => `contracts/platform/v1alpha1/schemas/${name}`),
    ...MANAGED_AGENT_SCHEMAS.map((name) => `contracts/managed-agent/v1alpha1/schemas/${name}`),
  ].toSorted();
}

export function platformJSONSDKAuthorityProfileDigest(root: string): string {
  return normalizedManifestDigest(root, platformJSONSDKAuthorityProfileInputs());
}

export function buildPlatformJSONSDKOutputs(root: string): ReadonlyArray<GeneratedOutput> {
  validateJSONSDKAuthority(root);
  const contractManifestSha256 = normalizedManifestDigest(root, platformJSONSDKContractInputs(root));
  const replacements = new Map([
    ["{{CONTRACT_MANIFEST_SHA256}}", contractManifestSha256],
    [
      "{{GENERATOR_SOURCE_MANIFEST_SHA256}}",
      normalizedManifestDigest(root, platformJSONSDKGeneratorSources()),
    ],
    ["{{CONFIG_DIGEST}}", platformJSONSDKConfigDigest()],
  ]);
  return [
    [GO_COMMON_JSON_OUTPUT_PATH, GO_COMMON_TEMPLATE_PATH],
    [GO_PLATFORM_JSON_OUTPUT_PATH, GO_PLATFORM_TEMPLATE_PATH],
    [GO_OPENAPI_OUTPUT_PATH, GO_OPENAPI_TEMPLATE_PATH],
    [TYPESCRIPT_PLATFORM_OUTPUT_PATH, TYPESCRIPT_TEMPLATE_PATH],
  ].map(([path, template]) => ({
    path,
    source: formatOutput(
      root,
      path,
      renderTemplate(readText(root, template), replacements, template),
    ),
  }));
}

export function buildPlatformJSONSDKManifests(
  root: string,
  outputs = buildPlatformJSONSDKOutputs(root),
): ReadonlyArray<GeneratedOutput> {
  const contractInputs = platformJSONSDKContractInputs(root);
  const contractManifestSha256 = normalizedManifestDigest(root, contractInputs);
  const generatorSources = platformJSONSDKGeneratorSources();
  const common = {
    formatVersion: "cloud-agents-generated-sdk-manifest/v1",
    profile: JSON_SDK_CONFIG.profile,
    status: "GENERATED_NON_GATE_EVIDENCE",
    notGateClosure: true,
    contract: {
      manifestAlgorithm: MANIFEST_ALGORITHM,
      manifestSha256: contractManifestSha256,
      inputManifestSha256: contractManifestSha256,
      inputs: contractInputs,
      authorityProfile: {
        id: JSON_SDK_AUTHORITY_PROFILE_ID,
        manifestAlgorithm: MANIFEST_ALGORITHM,
        manifestSha256: JSON_SDK_AUTHORITY_PROFILE_SHA256,
        inputs: platformJSONSDKAuthorityProfileInputs(),
      },
    },
    generator: {
      id: "platform-json-contract-sdk-generator",
      version: "v1",
      entrypoint: GENERATOR_PATH,
      sourceManifestAlgorithm: MANIFEST_ALGORITHM,
      sourceManifestSha256: normalizedManifestDigest(root, generatorSources),
      sources: generatorSources,
      configDigest: platformJSONSDKConfigDigest(),
    },
    implementationBoundary: JSON_SDK_CONFIG.implementationBoundary,
  };
  const byPath = new Map(outputs.map((output) => [output.path, output]));
  return [
    {
      language: "go",
      packageIdentity: "github.com/hxp0618/cloud-agents/sdk/go",
      outputPaths: [
        GO_COMMON_JSON_OUTPUT_PATH,
        GO_PLATFORM_JSON_OUTPUT_PATH,
        GO_OPENAPI_OUTPUT_PATH,
      ],
      manifestPath: GO_JSON_MANIFEST_PATH,
    },
    {
      language: "typescript",
      packageIdentity: "@synara/cloud-agent-platform-sdk/platform",
      outputPaths: [TYPESCRIPT_PLATFORM_OUTPUT_PATH],
      manifestPath: TYPESCRIPT_JSON_MANIFEST_PATH,
    },
  ].map((language) => {
    const files = language.outputPaths.map((path) => {
      const output = byPath.get(path);
      if (output === undefined) throw new Error(`Missing JSON SDK output ${path}.`);
      return generatedFileRecord(output);
    });
    return {
      path: language.manifestPath,
      source: `${JSON.stringify(
        {
          ...common,
          language: language.language,
          packageIdentity: language.packageIdentity,
          runtimeDependencies: [],
          outputTreeAlgorithm: OUTPUT_TREE_ALGORITHM,
          outputTreeSha256: outputTreeDigest(files),
          outputs: files,
        },
        null,
        2,
      )}\n`,
    };
  });
}

export function expectedPlatformJSONSDKFiles(root: string): ReadonlyArray<GeneratedOutput> {
  const outputs = buildPlatformJSONSDKOutputs(root);
  return [...outputs, ...buildPlatformJSONSDKManifests(root, outputs)];
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
  for (const output of expectedPlatformJSONSDKFiles(root)) {
    const target = resolve(root, output.path);
    mkdirSync(dirname(target), { recursive: true });
    writeFileSync(target, output.source);
  }
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
  const authorityProfileDigest = platformJSONSDKAuthorityProfileDigest(root);
  if (authorityProfileDigest !== JSON_SDK_AUTHORITY_PROFILE_SHA256) {
    throw new Error(
      `JSON SDK authority profile changed: ${authorityProfileDigest}; update the versioned profile explicitly.`,
    );
  }
  const agent = readJSON<Record<string, unknown>>(root, MANAGED_AGENT_OPENAPI_PATH);
  const host = readJSON<Record<string, unknown>>(root, MANAGED_HOST_OPENAPI_PATH);
  if (agent.openapi !== "3.1.1" || host.openapi !== "3.1.1") {
    throw new Error("JSON SDK OpenAPI authority must remain OpenAPI 3.1.1.");
  }
  const operations = [...openAPIOperations(agent), ...openAPIOperations(host)].toSorted();
	const expected = [
		"managedAgentBindRole",
		"managedAgentCancelExecution",
		"managedAgentCloseSession",
		"managedAgentCreateMembership",
		"managedAgentCreateOrganization",
    "managedAgentCreateProject",
    "managedAgentCreateSession",
		"managedAgentCreateTurn",
    "managedAgentExecute",
		"managedAgentGetExecution",
    "managedAgentGetMembership",
    "managedAgentGetOrganization",
    "managedAgentGetPlatformTenant",
    "managedAgentGetProject",
		"managedAgentGetRole",
		"managedAgentGetRoleBinding",
		"managedAgentGetSession",
	"managedAgentGetTurn",
	"managedAgentInterruptExecution",
	"managedAgentListEvents",
	"managedAgentListOrganizations",
	"managedAgentRevokeMembership",
		"managedAgentRevokeRoleBinding",
		"managedAgentSuspendMembership",
		"managedHostCreateEnvironmentLease",
		"managedHostGetEnvironmentLease",
		"managedHostGetProjectContext",
		"managedHostGetRoleBinding",
		"managedHostTerminateEnvironmentLease",
  ];
  if (JSON.stringify(operations) !== JSON.stringify(expected)) {
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
  const target = resolve(root, path);
  const stat = lstatSync(target);
  if (!stat.isFile() || stat.isSymbolicLink()) throw new Error(`${path} must be a regular file.`);
  return readFileSync(target, "utf8");
}

function renderTemplate(
  template: string,
  replacements: ReadonlyMap<string, string>,
  path: string,
): string {
  let result = template;
  for (const [placeholder, value] of replacements) result = result.replaceAll(placeholder, value);
  if (/\{\{[A-Z0-9_]+\}\}/u.test(result)) throw new Error(`Unresolved placeholder in ${path}.`);
  return result;
}

function formatOutput(root: string, path: string, source: string): string {
  if (!path.endsWith(".go")) return formatWithOxfmt(root, path, source);
  const result = spawnSync("gofmt", [], { input: source, encoding: "utf8", cwd: root });
  if (result.status !== 0) {
    throw new Error(`Formatter failed for ${path}: ${result.stderr.trim()}`);
  }
  return result.stdout;
}

function normalizedManifestDigest(root: string, paths: ReadonlyArray<string>): string {
  const hash = createHash("sha256");
  for (const path of [...paths].toSorted()) {
    const stat = lstatSync(resolve(root, path));
    if (!stat.isFile() || stat.isSymbolicLink()) throw new Error(`${path} must be a regular file.`);
    hash
      .update(path)
      .update("\0")
      .update(digestBytes(readFileSync(resolve(root, path))))
      .update("\0");
    hash.update((stat.mode & 0o111) === 0 ? "100644" : "100755").update("\0");
  }
  return `sha256:${hash.digest("hex")}`;
}

function generatedFileRecord(output: GeneratedOutput): GeneratedFileRecord {
  return {
    path: output.path,
    sha256: digestBytes(Buffer.from(output.source)),
    sizeBytes: Buffer.byteLength(output.source),
  };
}

function outputTreeDigest(files: ReadonlyArray<GeneratedFileRecord>): string {
  const hash = createHash("sha256");
  for (const file of [...files].toSorted((left, right) => left.path.localeCompare(right.path))) {
    hash
      .update(file.path)
      .update("\0")
      .update(file.sha256)
      .update("\0")
      .update(String(file.sizeBytes))
      .update("\0");
  }
  return `sha256:${hash.digest("hex")}`;
}

function digestBytes(value: string | Buffer): string {
  return `sha256:${createHash("sha256").update(value).digest("hex")}`;
}

function normalizeRelativePath(target: string, root: string): string {
  const value = relative(root, target).split(sep).join("/");
  if (value === "" || value === ".." || value.startsWith("../"))
    throw new Error("Path escapes root.");
  return value;
}
