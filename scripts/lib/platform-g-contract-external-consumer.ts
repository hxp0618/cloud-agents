import { createHash } from "node:crypto";
import { execFileSync } from "node:child_process";
import { existsSync, lstatSync, readFileSync, realpathSync, writeFileSync } from "node:fs";
import { relative, resolve, sep } from "node:path";
import Ajv2020 from "ajv/dist/2020.js";
import { canonicalizeJson, type JsonRecord } from "./platform-json-semantics";

export const EXTERNAL_CONSUMER_SOURCE_PATH = "tools/g-contract-external-consumer/v1/source.json";
export const EXTERNAL_CONSUMER_SOURCE_SCHEMA_PATH =
  "tools/g-contract-external-consumer/v1/source.schema.json";
export const EXTERNAL_CONSUMER_PROFILE_SCHEMA_PATH =
  "tools/g-contract-external-consumer/v1/profile.schema.json";
export const EXTERNAL_CONSUMER_PROFILE_PATH = "tools/g-contract-external-consumer/v1/profile.json";
const SOURCE_FORMAT = "cloud-agents-g-contract-external-consumer-source/v1";
const PROFILE_FORMAT = "cloud-agents-g-contract-external-consumer-registry/v1";
const REGISTRY_ID = "cloud-agents/g-contract-external-consumer";
const PROFILE_ID = "g-contract-external-consumer/v1";
const PROFILE_STATUS = "FRESH_CONSUMER_EVIDENCE_CURRENT_REPLAY_PENDING";
const TYPESCRIPT_ARTIFACT_PATH = "typescript-pack/synara-cloud-agent-platform-sdk-0.0.0-a3.2.tgz";
const GO_MODULE_PROXY_PATH = "go-proxy/github.com/hxp0618/cloud-agents/sdk/go/@v/v0.0.0-a3.2.zip";
const EXPECTED_NPM_DEPENDENCIES = {
  "@bufbuild/protobuf": {
    version: "2.14.0",
    artifactPath: "npm-registry-tar/@bufbuild/protobuf/bufbuild-protobuf-2.14.0.tgz",
  },
  "@connectrpc/connect": {
    version: "2.1.2",
    artifactPath: "npm-registry-tar/@connectrpc/connect/connectrpc-connect-2.1.2.tgz",
  },
  "@types/node": {
    version: "24.10.13",
    artifactPath: "npm-registry-tar/@types/node/types-node-24.10.13.tgz",
  },
  "undici-types": {
    version: "7.16.0",
    artifactPath: "npm-registry-tar/undici-types/undici-types-7.16.0.tgz",
  },
  typescript: {
    version: "5.7.3",
    artifactPath: "npm-registry-tar/typescript/typescript-5.7.3.tgz",
  },
} as const;
const AUTHORIZATION_PATH =
  "docs/plan/p1/g-contract-current-source-external-consumer-successor-authorization-20260826.md";
const EXPECTED_AUTHORIZATION = {
  path: AUTHORIZATION_PATH,
  gitBlob: "686f6db15e0e795b1e751972cc519ca2d6a60372",
  sha256: "sha256:960964f784c383bda1dcda67452c8c844888f091e91cf2d2e0de71acba29fb55",
  sizeBytes: 4688,
  mode: "100644",
} as const;
const TERMINAL_COMMIT = "4f71e38205fc25b3b164a24f13141644bd378cf7";
const TERMINAL_TREE = "13e999863a1c60538b6601b67c6d75822a9f8384";
const EXPECTED_PREDECESSOR_FILES = [
  {
    path: "contracts/generation.lock.json",
    gitBlob: "243a55c6462c6ab2b341abc8dd98703f05e0c4ba",
    sha256: "sha256:efe5ae588655739377d019bd9883f1f40254f1f37e1b0162288264f80dafc2f5",
    sizeBytes: 5979,
    mode: "100644",
  },
  {
    path: "docs/plan/cloud-agents-platform/evidence/G-CONTRACT/CAG-G-CONTRACT-P1-20260825-R5.md",
    gitBlob: "d2f36405146bb555c58dab4b70f87df82c3854be",
    sha256: "sha256:8f19e7ca49c075a475caa4af1a70bc3cfb31e88a8135301071089f9aff4d1c0a",
    sizeBytes: 8318,
    mode: "100644",
  },
  {
    path: "docs/plan/p1/g-contract-r5-review-binding-independent-review-20260825.md",
    gitBlob: "62751d47e1af23ed44cbb13ad69554b0f4974b7d",
    sha256: "sha256:e4d3b421c20a86eae2d8eda45fd9e0318a40a343f5531bb54f285cefb6cdefc7",
    sizeBytes: 3100,
    mode: "100644",
  },
  {
    path: "docs/plan/p1/g-contract-current-source-superseding-repair-authorization-20260826.md",
    gitBlob: "cd2bfbe3b351e4339cd5f18a9aa6faeb21368bf9",
    sha256: "sha256:5bd49bab97ac3e838a126e8351e9f5b68d3e959e1416936a35d486a68a800754",
    sizeBytes: 3787,
    mode: "100644",
  },
] as const;
const EXPECTED_INPUT_BINDINGS = [
  "package.json",
  "contracts/generated/proto/cloud-agents-v1alpha1.binpb",
  "contracts/generated/proto/cloud-agents-v1alpha1-breaking-baseline.binpb",
  "contracts/generated/proto/manifest.json",
  "contracts/proto-generation.profile.json",
  "docs/plan/cloud-agents-platform/05-gates-and-acceptance.md",
  "scripts/generate-platform-g-contract-external-consumer.ts",
  "scripts/lib/platform-g-contract-external-consumer.ts",
  "scripts/lib/platform-g-contract-external-consumer.test.ts",
  "tools/g-contract-external-consumer/v1/source.schema.json",
  "tools/g-contract-external-consumer/v1/profile.schema.json",
  "sdk/typescript/package.json",
  "sdk/typescript/generated-manifest.json",
  "sdk/typescript/proto-generated-manifest.json",
  "sdk/go/go.mod",
  "sdk/go/go.sum",
  "sdk/go/generated-manifest.json",
  "sdk/go/proto-generated-manifest.json",
] as const;
const FRESH_EVIDENCE_PROCESS = {
  version: "v1",
  orderedStages: [
    "repair",
    "focused_checks",
    "fresh_consumer_evidence",
    "freeze_source_and_inputs",
    "fresh_projection",
    "fresh_native_replay",
    "generated_profile",
    "independent_review",
  ],
  currentStatus: "CONSUMER_EVIDENCE_CURRENT_REPLAY_PENDING",
  projectionStatus: "PENDING",
  nativeReplay: { "darwin-arm64": "PENDING", "linux-amd64": "PENDING" },
  profileStatus: "CURRENT",
  independentReviewStatus: "PENDING",
  syntheticReceipts: "FORBIDDEN",
  successorOnDrift: "REQUIRED",
} as const;
const DIGEST = /^sha256:[0-9a-f]{64}$/u;
const FORBIDDEN = /(?:^|\b)(?:file:|workspace:|git(?:\+|:)|github:|local-path)/iu;
const LOOPBACK_URL = /^http:\/\/127\.0\.0\.1:<ephemeral-port>(?:\/[^\s]*)?$/u;
const H1 = /^h1:[A-Za-z0-9+/]{43}=$/u;
const FIXED_GIT_ENV = {
  PATH: "/usr/bin:/bin",
  LANG: "C",
  LC_ALL: "C",
  TZ: "UTC",
  GIT_CONFIG_NOSYSTEM: "1",
  GIT_CONFIG_GLOBAL: "/dev/null",
  GIT_EXTERNAL_DIFF: "",
  GIT_NO_REPLACE_OBJECTS: "1",
  GIT_OPTIONAL_LOCKS: "0",
  GIT_PAGER: "cat",
} as const;
const FIXED_GIT_ARGS = [
  "-c",
  "core.attributesFile=/dev/null",
  "-c",
  "core.abbrev=7",
  "-c",
  "diff.external=",
  "-c",
  "diff.renames=false",
] as const;

export type ExternalConsumerSource = JsonRecord & {
  readonly formatVersion: typeof SOURCE_FORMAT;
  readonly registryId: typeof REGISTRY_ID;
  readonly profileId: typeof PROFILE_ID;
  readonly status: typeof PROFILE_STATUS;
  readonly decisionId: "D-053-EC-1";
  readonly authorization: JsonRecord;
  readonly predecessorFence: JsonRecord;
  readonly gateCriteria: { readonly path: string; readonly sha256: string };
  readonly harness: { readonly path: string; readonly mode: "100644" };
  readonly toolchain: JsonRecord;
  readonly inputBindings: readonly string[];
  readonly criteria: readonly string[];
  readonly evidenceContract: JsonRecord;
  readonly fixturePolicy: JsonRecord;
  readonly freshEvidenceProcess: JsonRecord;
  readonly implementationBoundary: JsonRecord;
};

export type ExternalConsumerEvidence = JsonRecord & {
  readonly formatVersion: "cloud-agents-platform-sdk-consumer-evidence/v1";
  readonly harness: {
    readonly path: string;
    readonly sha256: string;
    readonly sizeBytes: number;
    readonly mode: "100644";
  };
  readonly typescript: JsonRecord & {
    readonly package: string;
    readonly version: string;
    readonly artifactSha256: string;
    readonly integrity: string;
    readonly dependencies: JsonRecord;
    readonly dependencyArtifacts: JsonRecord;
    readonly loopbackCallCount: number;
  };
  readonly go: JsonRecord & {
    readonly module: string;
    readonly version: string;
    readonly moduleZipSha256: string;
    readonly goModSha256: string;
    readonly goSumSha256: string;
    readonly loopbackCallCount: number;
  };
  readonly toolchain: JsonRecord;
};

export class ExternalConsumerError extends Error {
  constructor(
    readonly code: string,
    readonly path: string,
    message: string,
  ) {
    super(message);
    this.name = "ExternalConsumerError";
  }
}

export function readExternalConsumerSource(root: string): ExternalConsumerSource {
  const value = readJson(resolveContained(root, EXTERNAL_CONSUMER_SOURCE_PATH));
  validateSchema(root, value, EXTERNAL_CONSUMER_SOURCE_SCHEMA_PATH);
  const source = value as ExternalConsumerSource;
  if (source.authorization.path !== AUTHORIZATION_PATH)
    throw error(
      "EXTERNAL_CONSUMER_AUTHORIZATION_INVALID",
      "/authorization/path",
      "Successor authorization path is not the fixed approved record.",
    );
  if (
    !canonicalEqual(source.authorization, EXPECTED_AUTHORIZATION) ||
    source.profileId !== PROFILE_ID ||
    source.status !== PROFILE_STATUS ||
    source.predecessorFence.terminalCommit !== TERMINAL_COMMIT ||
    source.predecessorFence.terminalTree !== TERMINAL_TREE ||
    !canonicalEqual(source.predecessorFence.files, EXPECTED_PREDECESSOR_FILES) ||
    !canonicalEqual(source.inputBindings, EXPECTED_INPUT_BINDINGS) ||
    !canonicalEqual(source.freshEvidenceProcess, FRESH_EVIDENCE_PROCESS)
  )
    throw error(
      "EXTERNAL_CONSUMER_AUTHORITY_DRIFT",
      "/predecessorFence",
      "Versioned successor authority or exact input inventory drifted.",
    );
  const gate = resolveContained(root, source.gateCriteria.path);
  if (sha256File(gate) !== source.gateCriteria.sha256)
    throw error(
      "EXTERNAL_CONSUMER_GATE_CRITERIA_DRIFT",
      "/gateCriteria/sha256",
      "Gate criteria digest drifted.",
    );
  const fencedFiles = [
    source.authorization,
    ...(source.predecessorFence.files as readonly JsonRecord[]),
  ];
  for (const [index, binding] of fencedFiles.entries()) {
    const file = resolveContained(root, String((binding as JsonRecord).path));
    const stat = lstatSync(file);
    const blob = execFileSync(
      "git",
      [...FIXED_GIT_ARGS, "hash-object", "--", String((binding as JsonRecord).path)],
      { cwd: root, encoding: "utf8", env: FIXED_GIT_ENV },
    ).trim();
    const expectedMode =
      index === 0
        ? stat.mode.toString(8).slice(-6)
        : gitModeAtCommit(root, TERMINAL_COMMIT, String((binding as JsonRecord).path));
    const expectedBlob =
      index === 0
        ? blob
        : gitBlobAtCommit(root, TERMINAL_COMMIT, String((binding as JsonRecord).path));
    if (
      sha256File(file) !== (binding as JsonRecord).sha256 ||
      stat.size !== (binding as JsonRecord).sizeBytes ||
      blob !== (binding as JsonRecord).gitBlob ||
      expectedBlob !== (binding as JsonRecord).gitBlob ||
      expectedMode !== (binding as JsonRecord).mode
    )
      throw error(
        "EXTERNAL_CONSUMER_PREDECESSOR_DRIFT",
        index === 0 ? "/authorization" : `/predecessorFence/files/${index - 1}`,
        "Immutable D-053 predecessor bytes drifted.",
      );
  }
  const terminalTree = execFileSync(
    "git",
    [
      ...FIXED_GIT_ARGS,
      "show",
      "-s",
      "--format=%T",
      String(source.predecessorFence.terminalCommit),
    ],
    { cwd: root, encoding: "utf8", env: FIXED_GIT_ENV },
  ).trim();
  if (terminalTree !== String(source.predecessorFence.terminalTree))
    throw error(
      "EXTERNAL_CONSUMER_PREDECESSOR_DRIFT",
      "/predecessorFence/terminalTree",
      "D-053 terminal tree does not match the fenced commit.",
    );
  const terminalParents = execFileSync(
    "git",
    [
      ...FIXED_GIT_ARGS,
      "show",
      "-s",
      "--format=%P",
      String(source.predecessorFence.terminalCommit),
    ],
    { cwd: root, encoding: "utf8", env: FIXED_GIT_ENV },
  ).trim();
  if (terminalParents !== "cfdaaf702f17153f4469a6bf4f08ffdffa7ae3b6")
    throw error(
      "EXTERNAL_CONSUMER_PREDECESSOR_DRIFT",
      "/predecessorFence/terminalCommit",
      "D-053 terminal commit parent topology drifted.",
    );
  return source;
}

export function buildExternalConsumerProfile(
  root: string,
  evidence: ExternalConsumerEvidence,
  evidenceBinding?: { readonly path: string; readonly sha256: string; readonly sizeBytes: number },
): JsonRecord {
  const source = readExternalConsumerSource(root);
  validateEvidence(source, evidence);
  if (evidenceBinding && evidenceBinding.path !== source.evidenceContract.path)
    throw error(
      "EXTERNAL_CONSUMER_EVIDENCE_PATH",
      "/evidence/path",
      "Evidence binding path differs from the versioned source authority.",
    );
  if (evidenceBinding) {
    const evidenceFile = resolveContained(root, evidenceBinding.path);
    const evidenceBytes = readStableFile(evidenceFile);
    if (
      !DIGEST.test(evidenceBinding.sha256) ||
      sha256Bytes(evidenceBytes) !== evidenceBinding.sha256 ||
      evidenceBytes.byteLength !== evidenceBinding.sizeBytes
    )
      throw error(
        "EXTERNAL_CONSUMER_EVIDENCE_BINDING",
        "/evidence",
        "Evidence binding does not match the immutable evidence bytes.",
      );
  }
  const harnessPath = resolveContained(root, source.harness.path);
  const stat = lstatSync(harnessPath);
  if (!stat.isFile() || stat.mode.toString(8).slice(-6) !== "100644")
    throw error(
      "EXTERNAL_CONSUMER_HARNESS_INVALID",
      "/harness",
      "Harness must be a regular 100644 file.",
    );
  const harnessBytes = readStableFile(harnessPath);
  if (
    evidence.harness.path !== source.harness.path ||
    evidence.harness.mode !== source.harness.mode ||
    evidence.harness.sha256 !== sha256Bytes(harnessBytes) ||
    evidence.harness.sizeBytes !== harnessBytes.byteLength
  )
    throw error(
      "EXTERNAL_CONSUMER_HARNESS_MISMATCH",
      "/harness",
      "Evidence does not bind the live harness bytes.",
    );
  const inputBindings = source.inputBindings.map((path) => {
    const file = resolveContained(root, path);
    const inputBytes = readStableFile(file);
    const inputStat = lstatSync(file);
    if (!inputStat.isFile() || inputStat.mode.toString(8).slice(-6) !== "100644")
      throw error(
        "EXTERNAL_CONSUMER_INPUT_INVALID",
        `/${path}`,
        "Input binding must be a regular 100644 file.",
      );
    return {
      path,
      sha256: sha256Bytes(inputBytes),
      sizeBytes: inputBytes.byteLength,
      mode: "100644" as const,
    };
  });
  const body: JsonRecord = {
    $schema:
      "https://schemas.cloud-agents.dev/tools/g-contract-external-consumer/v1/profile.schema.json",
    formatVersion: PROFILE_FORMAT,
    registryId: REGISTRY_ID,
    profileId: PROFILE_ID,
    status: PROFILE_STATUS,
    authorization: source.authorization,
    predecessorFence: source.predecessorFence,
    gateCriteria: source.gateCriteria,
    harness: evidence.harness,
    inputBindings,
    evidence: evidenceBinding ?? {
      path: source.evidenceContract.path,
      sha256: digest(evidence),
      sizeBytes: canonicalizeJson(evidence).byteLength,
    },
    evidenceFormatVersion: evidence.formatVersion,
    toolchain: evidence.toolchain,
    fixturePolicy: source.fixturePolicy,
    freshEvidenceProcess: source.freshEvidenceProcess,
    consumers: { typescript: evidence.typescript, go: evidence.go },
    criteria: source.criteria,
    implementationBoundary: source.implementationBoundary,
    sourceDigest: digestDomain("cloud-agents/g-contract-external-consumer/source/v1", source),
  };
  const profileDigest = digestDomain("cloud-agents/g-contract-external-consumer/profile/v1", body);
  const withProfile = { ...body, profileDigest };
  const registryDigest = digestDomain(
    "cloud-agents/g-contract-external-consumer/registry/v1",
    withProfile,
  );
  const profile = { ...withProfile, registryDigest };
  validateSchema(root, profile, EXTERNAL_CONSUMER_PROFILE_SCHEMA_PATH);
  return profile;
}

export function writeExternalConsumerProfile(
  root: string,
  evidencePath: string,
  outputPath = EXTERNAL_CONSUMER_PROFILE_PATH,
): JsonRecord {
  const source = readExternalConsumerSource(root);
  if (evidencePath !== source.evidenceContract.path)
    throw error(
      "EXTERNAL_CONSUMER_EVIDENCE_PATH",
      "/evidence/path",
      "Evidence path must equal the versioned source authority.",
    );
  const resolvedEvidence = resolveContained(root, evidencePath);
  const evidenceBytes = readStableFile(resolvedEvidence);
  const evidence = parseJson(evidenceBytes, resolvedEvidence) as ExternalConsumerEvidence;
  const profile = buildExternalConsumerProfile(root, evidence, {
    path: evidencePath,
    sha256: sha256Bytes(evidenceBytes),
    sizeBytes: evidenceBytes.byteLength,
  });
  const path = resolveContained(root, outputPath, true);
  const bytes = `${JSON.stringify(profile, null, 2)}\n`;
  if (existsSync(path)) {
    if (readStableFile(path).toString("utf8") !== bytes)
      throw error(
        "EXTERNAL_CONSUMER_WRITE_CONFLICT",
        `/${outputPath}`,
        "Refusing to overwrite an existing profile.",
      );
  } else writeFileSync(path, bytes, { flag: "wx" });
  return profile;
}

export function assertExternalConsumerProfileCurrent(
  root: string,
  evidencePath: string,
  profilePath = EXTERNAL_CONSUMER_PROFILE_PATH,
): JsonRecord {
  const source = readExternalConsumerSource(root);
  if (evidencePath !== source.evidenceContract.path)
    throw error(
      "EXTERNAL_CONSUMER_EVIDENCE_PATH",
      "/evidence/path",
      "Evidence path must equal the versioned source authority.",
    );
  const resolvedEvidence = resolveContained(root, evidencePath);
  const evidenceBytes = readStableFile(resolvedEvidence);
  const expected = buildExternalConsumerProfile(
    root,
    parseJson(evidenceBytes, resolvedEvidence) as ExternalConsumerEvidence,
    {
      path: evidencePath,
      sha256: sha256Bytes(evidenceBytes),
      sizeBytes: evidenceBytes.byteLength,
    },
  );
  const actual = readJson(resolveContained(root, profilePath));
  validateSchema(root, actual, EXTERNAL_CONSUMER_PROFILE_SCHEMA_PATH);
  if (JSON.stringify(actual) !== JSON.stringify(expected))
    throw error(
      "EXTERNAL_CONSUMER_PROFILE_STALE",
      `/${profilePath}`,
      "Generated profile differs from current source, harness, or evidence.",
    );
  return actual;
}

function validateEvidence(source: ExternalConsumerSource, value: ExternalConsumerEvidence): void {
  if (!value || typeof value !== "object")
    throw error("EXTERNAL_CONSUMER_EVIDENCE_INVALID", "/", "Evidence must be an object.");
  assertKeys(value, ["formatVersion", "harness", "toolchain", "typescript", "go"], "/");
  if (value.formatVersion !== source.evidenceContract.formatVersion)
    throw error(
      "EXTERNAL_CONSUMER_EVIDENCE_INVALID",
      "/formatVersion",
      "Evidence format differs from the versioned source authority.",
    );
  assertKeys(value.toolchain, ["bun", "go", "typescript", "goFlags", "goWork"], "/toolchain");
  assertKeys(value.harness, ["path", "sha256", "sizeBytes", "mode"], "/harness");
  if (
    value.harness.path !== source.harness.path ||
    value.harness.mode !== source.harness.mode ||
    typeof value.harness.sha256 !== "string" ||
    !DIGEST.test(value.harness.sha256) ||
    !Number.isInteger(value.harness.sizeBytes) ||
    value.harness.sizeBytes < 1
  )
    throw error(
      "EXTERNAL_CONSUMER_HARNESS_INVALID",
      "/harness",
      "Evidence harness binding is malformed.",
    );
  if (!isRecord(value.toolchain) || !canonicalEqual(value.toolchain, source.toolchain))
    throw error(
      "EXTERNAL_CONSUMER_TOOLCHAIN_DRIFT",
      "/toolchain",
      "Evidence toolchain differs from the exact source authority.",
    );
  for (const [name, consumer] of [
    ["typescript", value.typescript],
    ["go", value.go],
  ] as const) {
    if (!consumer || typeof consumer !== "object")
      throw error(
        "EXTERNAL_CONSUMER_EVIDENCE_INVALID",
        `/${name}`,
        "Consumer evidence is missing.",
      );
    assertKeys(
      consumer,
      name === "typescript"
        ? [
            "package",
            "version",
            "toolchain",
            "artifactPath",
            "artifactSha256",
            "integrity",
            "dependencies",
            "dependencyArtifacts",
            "lockArtifactUrl",
            "lockIntegrity",
            "fixture",
            "loopbackCallCount",
          ]
        : [
            "module",
            "version",
            "toolchain",
            "moduleProxyPath",
            "moduleZipSha256",
            "goModSha256",
            "moduleSum",
            "goModSum",
            "goSumSha256",
            "goFlags",
            "goWork",
            "goproxy",
            "fixture",
            "loopbackCallCount",
          ],
      `/${name}`,
    );
    if (consumer.loopbackCallCount !== 1)
      throw error(
        "EXTERNAL_CONSUMER_CALL_COUNT",
        `/${name}/loopbackCallCount`,
        "Exactly one loopback call is required.",
      );
    for (const key of Object.keys(consumer))
      if (containsForbidden(consumer[key]))
        throw error(
          "EXTERNAL_CONSUMER_LOCAL_DEPENDENCY",
          `/${name}/${key}`,
          "Local, workspace, git, or file dependency is forbidden.",
        );
  }
  for (const key of ["artifactSha256", "moduleZipSha256", "goModSha256", "goSumSha256"] as const) {
    const valueAt = key === "artifactSha256" ? value.typescript[key] : value.go[key];
    if (typeof valueAt !== "string" || !DIGEST.test(valueAt))
      throw error(
        "EXTERNAL_CONSUMER_DIGEST_MISSING",
        `/${key}`,
        "Required SHA-256 digest is missing or malformed.",
      );
  }
  if (
    typeof value.typescript.integrity !== "string" ||
    !/^sha512-[A-Za-z0-9+/]{86}==$/.test(value.typescript.integrity)
  )
    throw error(
      "EXTERNAL_CONSUMER_INTEGRITY_INVALID",
      "/typescript/integrity",
      "SRI integrity is required.",
    );
  if (
    value.typescript.package !== "@synara/cloud-agent-platform-sdk" ||
    value.typescript.version !== "0.0.0-a3.2" ||
    value.typescript.toolchain !== "bun@1.4.0;typescript@5.7.3" ||
    value.typescript.artifactPath !== TYPESCRIPT_ARTIFACT_PATH ||
    value.typescript.lockIntegrity !== value.typescript.integrity ||
    value.typescript.lockArtifactUrl !==
      `http://127.0.0.1:<ephemeral-port>/${TYPESCRIPT_ARTIFACT_PATH}`
  )
    throw error(
      "EXTERNAL_CONSUMER_TYPESCRIPT_INVALID",
      "/typescript",
      "TypeScript package, toolchain, lock, or loopback artifact binding drifted.",
    );
  const dependencies = value.typescript.dependencies;
  if (
    !isRecord(dependencies) ||
    !canonicalEqual(dependencies, {
      "@bufbuild/protobuf": "2.14.0",
      "@connectrpc/connect": "2.1.2",
    })
  )
    throw error(
      "EXTERNAL_CONSUMER_TYPESCRIPT_INVALID",
      "/typescript/dependencies",
      "TypeScript dependencies must equal the exact two-package pin set.",
    );
  const dependencyArtifacts = value.typescript.dependencyArtifacts;
  if (!isRecord(dependencyArtifacts))
    throw error(
      "EXTERNAL_CONSUMER_TYPESCRIPT_INVALID",
      "/typescript/dependencyArtifacts",
      "TypeScript dependency artifact bindings are required.",
    );
  assertKeys(
    dependencyArtifacts,
    Object.keys(EXPECTED_NPM_DEPENDENCIES),
    "/typescript/dependencyArtifacts",
  );
  for (const [name, expected] of Object.entries(EXPECTED_NPM_DEPENDENCIES)) {
    const artifact = dependencyArtifacts[name];
    assertKeys(
      artifact,
      ["version", "artifactPath", "sha256", "integrity"],
      `/typescript/dependencyArtifacts/${name}`,
    );
    if (
      !isRecord(artifact) ||
      artifact.version !== expected.version ||
      artifact.artifactPath !== expected.artifactPath ||
      typeof artifact.sha256 !== "string" ||
      !DIGEST.test(artifact.sha256) ||
      typeof artifact.integrity !== "string" ||
      !/^sha512-[A-Za-z0-9+/]{86}==$/.test(artifact.integrity)
    )
      throw error(
        "EXTERNAL_CONSUMER_TYPESCRIPT_INVALID",
        `/typescript/dependencyArtifacts/${name}`,
        "TypeScript dependency artifact version, path, or digest drifted.",
      );
  }
  assertFixture(value.typescript.fixture, true, "/typescript/fixture");
  if (
    value.go.module !== "github.com/hxp0618/cloud-agents/sdk/go" ||
    value.go.version !== "v0.0.0-a3.2" ||
    value.go.toolchain !== "go1.27.0" ||
    value.go.moduleProxyPath !== GO_MODULE_PROXY_PATH ||
    value.go.goFlags !== "-mod=readonly" ||
    value.go.goWork !== "off" ||
    !LOOPBACK_URL.test(String(value.go.goproxy)) ||
    !H1.test(String(value.go.moduleSum)) ||
    !H1.test(String(value.go.goModSum))
  )
    throw error(
      "EXTERNAL_CONSUMER_GO_INVALID",
      "/go",
      "Go module, toolchain, checksum, flags, or loopback proxy binding drifted.",
    );
  assertFixture(value.go.fixture, true, "/go/fixture");
}

function assertFixture(value: unknown, requireContentTypes: boolean, path: string): void {
  assertKeys(
    value,
    requireContentTypes
      ? [
          "transport",
          "method",
          "path",
          "requestContentType",
          "responseContentType",
          "loopback",
          "callCount",
        ]
      : ["transport", "method", "path", "loopback", "callCount"],
    path,
  );
  if (
    !isRecord(value) ||
    value.transport !== "connect" ||
    value.method !== "POST" ||
    value.path !== "/cloudagents.worker.v1alpha1.WorkerExecutionService/Negotiate" ||
    value.loopback !== true ||
    value.callCount !== 1 ||
    (requireContentTypes &&
      (value.requestContentType !== "application/proto" ||
        value.responseContentType !== "application/proto"))
  )
    throw error(
      "EXTERNAL_CONSUMER_FIXTURE_INVALID",
      path,
      "Fixture must equal the one-call loopback Connect POST authority.",
    );
}

function containsForbidden(value: unknown): boolean {
  if (typeof value === "string") {
    if (FORBIDDEN.test(value) || /^(?:\/tmp|\/Users|\/home|\.\.\/)/u.test(value)) return true;
    if (/^https?:\/\//u.test(value) && !LOOPBACK_URL.test(value)) return true;
    return false;
  }
  if (Array.isArray(value)) return value.some(containsForbidden);
  if (value && typeof value === "object")
    return Object.entries(value).some(([k, v]) => FORBIDDEN.test(k) || containsForbidden(v));
  return false;
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return value !== null && typeof value === "object" && !Array.isArray(value);
}

function assertKeys(value: unknown, expected: readonly string[], path: string): void {
  if (!isRecord(value))
    throw error("EXTERNAL_CONSUMER_EVIDENCE_INVALID", path, "Evidence value must be an object.");
  const actual = Object.keys(value).sort();
  const wanted = [...expected].sort();
  if (actual.length !== wanted.length || actual.some((key, index) => key !== wanted[index]))
    throw error(
      "EXTERNAL_CONSUMER_EVIDENCE_SHAPE",
      path,
      "Evidence contains missing or unknown fields.",
    );
}

function validateSchema(root: string, value: unknown, schemaPath: string): void {
  const schema = readJson(resolveContained(root, schemaPath));
  const ajv = new Ajv2020({ allErrors: true, strict: true });
  if (schemaPath !== EXTERNAL_CONSUMER_SOURCE_SCHEMA_PATH) {
    ajv.addSchema(
      readJson(resolveContained(root, EXTERNAL_CONSUMER_SOURCE_SCHEMA_PATH)),
      "source.schema.json",
    );
  }
  const validator = ajv.compile(schema);
  if (!validator(value)) {
    throw error(
      "EXTERNAL_CONSUMER_SCHEMA_INVALID",
      "/",
      ajv.errorsText(validator.errors ?? undefined),
    );
  }
}

function readJson(path: string): JsonRecord {
  try {
    return parseJson(readStableFile(path), path);
  } catch (cause) {
    if (cause instanceof ExternalConsumerError) throw cause;
    throw error("EXTERNAL_CONSUMER_JSON_INVALID", path, String(cause));
  }
}
function parseJson(bytes: Buffer, path: string): JsonRecord {
  try {
    return JSON.parse(bytes.toString("utf8")) as JsonRecord;
  } catch (cause) {
    throw error("EXTERNAL_CONSUMER_JSON_INVALID", path, String(cause));
  }
}
function resolveContained(root: string, path: string, allowMissingFinal = false): string {
  if (
    path.startsWith("/") ||
    path.includes("\\") ||
    path.split("/").some((part) => part === "" || part === "." || part === "..")
  )
    throw error("EXTERNAL_CONSUMER_PATH_ESCAPE", "/path", "Path is not canonical.");
  const base = realpathSync(root);
  const target = resolve(base, path);
  const relation = relative(base, target);
  if (relation === "" || relation === ".." || relation.startsWith(`..${sep}`))
    throw error("EXTERNAL_CONSUMER_PATH_ESCAPE", "/path", "Path escapes repository root.");
  let current = base;
  const components = relation.split(sep);
  for (const [index, component] of components.entries()) {
    current = resolve(current, component);
    try {
      const stat = lstatSync(current);
      const final = index === components.length - 1;
      if (stat.isSymbolicLink() || (!final && !stat.isDirectory()) || (final && !stat.isFile()))
        throw error(
          "EXTERNAL_CONSUMER_PATH_ESCAPE",
          `/${path}`,
          "Path traverses a symlink or non-regular component.",
        );
    } catch (cause) {
      if (
        allowMissingFinal &&
        index === components.length - 1 &&
        cause instanceof Error &&
        "code" in cause &&
        cause.code === "ENOENT"
      )
        return current;
      throw cause;
    }
  }
  return current;
}
function sha256File(path: string): string {
  return sha256Bytes(readStableFile(path));
}
function sha256Bytes(bytes: Uint8Array): string {
  return `sha256:${createHash("sha256").update(bytes).digest("hex")}`;
}
function readStableFile(path: string): Buffer {
  const before = lstatSync(path, { bigint: true });
  if (!before.isFile() || before.isSymbolicLink()) throw new Error("path is not a regular file");
  if (realpathSync(path) !== path) throw new Error("path resolves through a symlink");
  const bytes = readFileSync(path);
  const after = lstatSync(path, { bigint: true });
  if (
    !after.isFile() ||
    after.isSymbolicLink() ||
    after.dev !== before.dev ||
    after.ino !== before.ino ||
    after.size !== before.size ||
    after.mtimeNs !== before.mtimeNs ||
    after.ctimeNs !== before.ctimeNs ||
    realpathSync(path) !== path
  )
    throw new Error("file changed during stable read");
  return bytes;
}
function gitModeAtCommit(root: string, commit: string, path: string): string {
  const output = execFileSync("git", [...FIXED_GIT_ARGS, "ls-tree", "-z", commit, "--", path], {
    cwd: root,
    encoding: "utf8",
    env: FIXED_GIT_ENV,
  });
  const records = output.split("\0").filter(Boolean);
  if (records.length !== 1) return "";
  const match = records[0]?.match(/^([^ ]+) [^ ]+ [0-9a-f]{40}\t(.+)$/u);
  return match?.[2] === path ? (match[1] ?? "") : "";
}
function gitBlobAtCommit(root: string, commit: string, path: string): string {
  const output = execFileSync("git", [...FIXED_GIT_ARGS, "ls-tree", "-z", commit, "--", path], {
    cwd: root,
    encoding: "utf8",
    env: FIXED_GIT_ENV,
  });
  const records = output.split("\0").filter(Boolean);
  if (records.length !== 1) return "";
  const match = records[0]?.match(/^[^ ]+ blob ([0-9a-f]{40})\t(.+)$/u);
  return match?.[2] === path ? (match[1] ?? "") : "";
}
function digest(value: unknown): string {
  return `sha256:${createHash("sha256").update(canonicalizeJson(value)).digest("hex")}`;
}
function digestDomain(domain: string, value: unknown): string {
  return `sha256:${createHash("sha256").update(`${domain}\0`).update(canonicalizeJson(value)).digest("hex")}`;
}
function canonicalEqual(left: unknown, right: unknown): boolean {
  const leftBytes = canonicalizeJson(left);
  const rightBytes = canonicalizeJson(right);
  return (
    leftBytes.byteLength === rightBytes.byteLength &&
    leftBytes.every((value, index) => value === rightBytes[index])
  );
}
function error(code: string, path: string, message: string): ExternalConsumerError {
  return new ExternalConsumerError(code, path, message);
}
