import { createHash } from "node:crypto";
import { lstatSync, readFileSync } from "node:fs";
import { resolve } from "node:path";
import Ajv2020 from "ajv/dist/2020.js";

import {
  canonicalizeMigrationJson,
  parseStrictMigrationJson,
  type MigrationJson,
} from "./platform-migration-json";

export const RUNNER_BINDING_SOURCE_PATH =
  "services/control-plane/migrations/successor/000014/runner-binding/authority-source.json";
export const RUNNER_BINDING_PROFILE_PATH =
  "services/control-plane/migrations/successor/000014/runner-binding/profile.json";
export const RUNNER_BINDING_SOURCE_SCHEMA_PATH =
  "services/control-plane/migrations/successor/000014/runner-binding/authority-source.schema.json";
export const RUNNER_BINDING_PROFILE_SCHEMA_PATH =
  "services/control-plane/migrations/successor/000014/runner-binding/profile.schema.json";
export const RUNNER_BINDING_GO_PATH =
  "services/control-plane/internal/localmigration/runner_binding_profile_generated.go";

export const RUNNER_BINDING_AUTHORITY_ID = "D-053-MIG-000014";
export const RUNNER_BINDING_REVISION = "D-053-MIG-000014.r2";
export const RUNNER_BINDING_PROFILE_ID = "cloud-agents-platform-migration-runner-binding/v1";
export const RUNNER_BINDING_INPUT_SCOPE =
  "runner-binding-generator-and-focused-verification-closure/v1";

const R1_SOURCE_PATH = "services/control-plane/migrations/successor/000014/authority-source.json";
const R1_PROFILE_PATH = "services/control-plane/migrations/successor/000014/profile.json";
const R1_SOURCE_SCHEMA_PATH =
  "services/control-plane/migrations/successor/000014/authority-source.schema.json";
const R1_PROFILE_SCHEMA_PATH =
  "services/control-plane/migrations/successor/000014/profile.schema.json";
const CANONICAL_MANIFEST_PATH = "services/control-plane/migrations/manifest.json";
const CANONICAL_SCHEMA_PATH = "services/control-plane/migrations/schema-bundle.json";
const SUCCESSOR_MANIFEST_PATH = "services/control-plane/migrations/successor/000014/manifest.json";
const SUCCESSOR_SCHEMA_PATH =
  "services/control-plane/migrations/successor/000014/schema-bundle.json";
const R1_PROFILE_LOGICAL_DIGEST =
  "sha256:0637e32e1e07d82ff2917a13f8ade6276c2518ff0aeb7a80451f9da0f69b2630";
// Git object identities are evidence references to the immutable r1
// candidate.  They are deliberately carried as data (rather than derived
// from the current checkout) so a later worktree cannot silently substitute a
// self-consistent but different predecessor.
const R1_GIT = {
  commit: "1325dc1773ef9bad2d809fedee9b392e3cdbf959",
  tree: "49e53f2462af20201231c2428eb56cce543403a2",
  subtree: {
    path: "services/control-plane/migrations/successor/000014",
    sha1: "9d704eec0c8ca04fc0f1bd41b4a348db0b853096",
  },
  blobs: {
    authoritySource: {
      path: R1_SOURCE_PATH,
      sha1: "040c765971adde44d2171382d726ff294de05954",
    },
    authoritySourceSchema: {
      path: R1_SOURCE_SCHEMA_PATH,
      sha1: "34705605fb42f049135da3a31a911387820f872f",
    },
    profile: {
      path: R1_PROFILE_PATH,
      sha1: "046e2c51581964a59e770308da4a9fe23635f3ee",
    },
    profileSchema: {
      path: R1_PROFILE_SCHEMA_PATH,
      sha1: "124ea8dda97838f14cc9512fa06022b55ca74f87",
    },
    canonicalManifest: {
      path: CANONICAL_MANIFEST_PATH,
      sha1: "6f5f0c35b488fa221e814c0b768c3317ce9b9c68",
    },
    canonicalSchemaBundle: {
      path: CANONICAL_SCHEMA_PATH,
      sha1: "b064e10ab06011f80523be6f44a7eff96db91549",
    },
    successorManifest: {
      path: SUCCESSOR_MANIFEST_PATH,
      sha1: "2a4948e082e606a10564fb7c77a469975e6c888d",
    },
    successorSchemaBundle: {
      path: SUCCESSOR_SCHEMA_PATH,
      sha1: "18f4fa00d7b17a9d72df61b60e1124e18bf73b87",
    },
  },
} as const;
const R1_EXPECTED_ARTIFACTS: Readonly<Record<string, RunnerBindingArtifact>> = {
  [R1_SOURCE_PATH]: {
    path: R1_SOURCE_PATH,
    mode: "100644",
    sizeBytes: 21698,
    sha256: "sha256:6436c991dc838c353f27f91f9aff3257d02e18a6c3e0535244fe7f7d1d7a5d8e",
  },
  [R1_PROFILE_PATH]: {
    path: R1_PROFILE_PATH,
    mode: "100644",
    sizeBytes: 22970,
    sha256: "sha256:668c7e9c0337d1e50c81dde0ac465561d4ac4eb5f6d14f7fd8b2e26ef672250a",
  },
  [R1_SOURCE_SCHEMA_PATH]: {
    path: R1_SOURCE_SCHEMA_PATH,
    mode: "100644",
    sizeBytes: 9052,
    sha256: "sha256:b7ccd78f1c8cc3969b0d2c8846157dc645595e397ab97942522ecfbe163c873c",
  },
  [R1_PROFILE_SCHEMA_PATH]: {
    path: R1_PROFILE_SCHEMA_PATH,
    mode: "100644",
    sizeBytes: 10010,
    sha256: "sha256:314cc945796fac537bc89a3c09d2e1d6681fa06e028ea0ddecb3a776eb31e827",
  },
};
const PROFILE_DOMAIN = "cloud-agents-platform-migration-runner-binding/v1";
const INPUT_SCOPE = RUNNER_BINDING_INPUT_SCOPE;
const SOURCE_SCHEMA_URL =
  "https://schemas.cloud-agents.dev/platform/migrations/successor/000014/runner-binding/authority-source.schema.json";
const PROFILE_SCHEMA_URL =
  "https://schemas.cloud-agents.dev/platform/migrations/successor/000014/runner-binding/profile.schema.json";

export type RunnerBindingArtifact = Readonly<{
  path: string;
  mode: "100644";
  sizeBytes: number;
  sha256: string;
}>;

export type RunnerBindingOutput = Readonly<{
  source: Record<string, unknown>;
  profile: Record<string, unknown>;
  generatedFiles: ReadonlyMap<string, Uint8Array>;
  profileDigest: string;
  profileBlobDigest: string;
}>;

/** Build the r2 source, profile, and localdev generated constants without DB access. */
export function buildMigrationRunnerBinding(root: string): RunnerBindingOutput {
  const r1Source = readJson(root, R1_SOURCE_PATH);
  const r1Profile = readJson(root, R1_PROFILE_PATH);
  for (const [path, expected] of Object.entries(R1_EXPECTED_ARTIFACTS)) {
    const actual = artifact(root, path);
    if (!sameArtifact(actual, expected)) {
      throw new Error(`D-053-MIG-000014.r1 immutable artifact drifted: ${path}`);
    }
  }
  if (
    r1Source.authorityId !== RUNNER_BINDING_AUTHORITY_ID ||
    r1Source.revision !== "D-053-MIG-000014.r1" ||
    r1Profile.authorityId !== RUNNER_BINDING_AUTHORITY_ID ||
    r1Profile.revision !== "D-053-MIG-000014.r1" ||
    r1Profile.profileDigest !== R1_PROFILE_LOGICAL_DIGEST
  ) {
    throw new Error("D-053-MIG-000014.r1 identity drifted");
  }
  const r1 = {
    authorityId: RUNNER_BINDING_AUTHORITY_ID,
    revision: "D-053-MIG-000014.r1",
    successorId: String(r1Source.successorId),
    inputScope: String(r1Source.inputScope),
    source: artifact(root, R1_SOURCE_PATH),
    sourceSchema: artifact(root, R1_SOURCE_SCHEMA_PATH),
    profile: artifact(root, R1_PROFILE_PATH),
    profileSchema: artifact(root, R1_PROFILE_SCHEMA_PATH),
    profileLogicalDigest: R1_PROFILE_LOGICAL_DIGEST,
    git: clone(R1_GIT),
    predecessor: clone(r1Profile.predecessor),
    successor: clone(r1Profile.successor),
    runtime: clone(r1Profile.runtime),
    inputPaths: clone(r1Source.inputPaths),
    protectedPaths: clone(r1Source.protectedPaths),
    exclusionPaths: clone(r1Source.exclusionPaths),
    receiptPaths: clone(r1Source.receiptPaths),
    receiptState: clone(r1Source.receiptState),
    runner: clone(r1Source.runner),
    toolchain: clone(r1Source.toolchain),
    platforms: clone(r1Source.platforms),
    lineageFence: clone(r1Source.lineageFence),
    reviewRules: clone(r1Source.reviewRules),
    implementationBoundary: clone(r1Source.implementationBoundary),
    archiveAlgorithm: clone(r1Source.archiveAlgorithm),
    memberAlgorithm: clone(r1Source.memberAlgorithm),
  } satisfies Record<string, MigrationJson>;
  const selectors = [
    selectorFromR1(
      root,
      "canonical-000013",
      "000013",
      CANONICAL_MANIFEST_PATH,
      CANONICAL_SCHEMA_PATH,
      r1Profile.predecessor,
      {
        manifestRawDigest:
          "sha256:95a584a9e517d68e9a904fadfc76f84dcdbd8b532a24d8716468d0ff53d59d6b",
        manifestDigest: "sha256:56af03a65461e2009cf73c16ac2b1d74d856f68e3efc8b363ab84c537660c4d1",
        schemaRawDigest: "sha256:d5ce27597e2218240a276dbbec01431e4fe26774e195b70445078d8662a3826d",
        schemaDigest: "sha256:c7e08e81b463d04dd267438ac636811200586d5d84d8cb2e8d18799bd2c5faca",
        migrationCount: 13,
      },
    ),
    selectorFromR1(
      root,
      "successor-000014",
      "000014",
      SUCCESSOR_MANIFEST_PATH,
      SUCCESSOR_SCHEMA_PATH,
      r1Profile.successor,
      {
        manifestRawDigest:
          "sha256:961ccac428f8bf0d55a828fd93ae8e2085ae17d34c09cb2b46c28f653851f8ae",
        manifestDigest: "sha256:1ece795f54e049a15e4de37f351841be0ba611f8eb0be6eaf1aa68dc0145b620",
        schemaRawDigest: "sha256:d90661ac0271b78de563e565bb861b35d570be30fa788131e9813dde56870edc",
        schemaDigest: "sha256:2f363d4dc412803a3c126dd9b85f4e2fe7109b92b04706d077a786fdaa673677",
        migrationCount: 14,
      },
    ),
  ];
  const sourceSchemaArtifact = artifact(root, RUNNER_BINDING_SOURCE_SCHEMA_PATH);
  const profileSchemaArtifact = artifact(root, RUNNER_BINDING_PROFILE_SCHEMA_PATH);
  const sourceBody = {
    $schema: SOURCE_SCHEMA_URL,
    formatVersion: "cloud-agents-platform-migration-runner-binding-source/v1",
    authorityId: RUNNER_BINDING_AUTHORITY_ID,
    revision: RUNNER_BINDING_REVISION,
    profileId: RUNNER_BINDING_PROFILE_ID,
    inputScope: INPUT_SCOPE,
    sourceSchema: sourceSchemaArtifact,
    profileSchema: profileSchemaArtifact,
    predecessorAuthority: {
      authorityId: RUNNER_BINDING_AUTHORITY_ID,
      revision: "D-053-MIG-000014.r1",
      profileLogicalDigest: R1_PROFILE_LOGICAL_DIGEST,
      profile: r1.profile,
      source: r1.source,
      sourceSchema: r1.sourceSchema,
      profileSchema: r1.profileSchema,
      git: clone(R1_GIT),
    },
    selectors,
    r1Closure: r1,
    runner: {
      entrypoint: "services/control-plane/internal/localmigration.Run",
      mode: "localdev_only",
      bindBeforeConnect: true,
      completeLedger: "no-op",
      entryWriter: "NOT_IMPLEMENTED",
      recoveryWriter: "NOT_IMPLEMENTED",
      externalEffects: "forbidden",
    },
    reviewRules: {
      independentReadOnly: true,
      verdicts: ["APPROVE", "REQUEST_CHANGES"],
      candidateMutation: "forbidden",
      gateTransition: "forbidden",
      p0P1Repair: "one repair and re-review within r2 candidate",
      p2: "record-and-defer",
    },
    lineageFence: {
      kind: "single-predecessor-append-only",
      predecessorRevision: "D-053-MIG-000014.r1",
      successorRevision: RUNNER_BINDING_REVISION,
      historicalEvidence: "retain-and-never-rewrite",
    },
    implementationBoundary: {
      databaseWrites: "not_authorized",
      productionRunner: "forbidden",
      http: "forbidden",
      p2: "forbidden",
      provider: "forbidden",
      deployment: "forbidden",
      publication: "forbidden",
      gateTransition: "forbidden",
    },
  } satisfies Record<string, MigrationJson>;
  const sourceBytes = pretty(sourceBody);
  const profileBody = {
    $schema: PROFILE_SCHEMA_URL,
    formatVersion: "cloud-agents-platform-migration-runner-binding/v1",
    authorityId: RUNNER_BINDING_AUTHORITY_ID,
    revision: RUNNER_BINDING_REVISION,
    profileId: RUNNER_BINDING_PROFILE_ID,
    inputScope: INPUT_SCOPE,
    source: artifactBytes(RUNNER_BINDING_SOURCE_PATH, sourceBytes),
    sourceSchema: sourceSchemaArtifact,
    profileSchema: profileSchemaArtifact,
    predecessorAuthority: sourceBody.predecessorAuthority,
    selectors,
    r1Closure: r1,
    runner: sourceBody.runner,
    reviewRules: sourceBody.reviewRules,
    lineageFence: sourceBody.lineageFence,
    implementationBoundary: sourceBody.implementationBoundary,
  } satisfies Record<string, MigrationJson>;
  const profileDigest = domainDigest(PROFILE_DOMAIN, profileBody);
  const profile = { ...profileBody, profileDigest } satisfies Record<string, MigrationJson>;
  const profileBytes = pretty(profile);
  validateDocumentsAgainstSchemas(root, sourceBody, profile);
  const profileBlobDigest = digestBytes(profileBytes);
  const generatedGo = generateGo(
    profileDigest,
    profileBlobDigest,
    artifactBytes(RUNNER_BINDING_SOURCE_PATH, sourceBytes),
    artifactBytes(RUNNER_BINDING_PROFILE_PATH, profileBytes),
    sourceSchemaArtifact,
    profileSchemaArtifact,
    selectors,
  );
  return {
    source: sourceBody,
    profile,
    profileDigest,
    profileBlobDigest,
    generatedFiles: new Map([
      [RUNNER_BINDING_SOURCE_SCHEMA_PATH, readRegular(root, RUNNER_BINDING_SOURCE_SCHEMA_PATH)],
      [RUNNER_BINDING_PROFILE_SCHEMA_PATH, readRegular(root, RUNNER_BINDING_PROFILE_SCHEMA_PATH)],
      [RUNNER_BINDING_SOURCE_PATH, sourceBytes],
      [RUNNER_BINDING_PROFILE_PATH, profileBytes],
      [RUNNER_BINDING_GO_PATH, new TextEncoder().encode(generatedGo)],
    ]),
  };
}

export function validateCheckedInMigrationRunnerBinding(root: string): RunnerBindingOutput {
  const expected = buildMigrationRunnerBinding(root);
  for (const [path, bytes] of expected.generatedFiles) {
    const actual = readRegular(root, path);
    if (!Buffer.from(actual).equals(Buffer.from(bytes))) {
      throw new Error(`RUNNER_BINDING_STALE: ${path}`);
    }
  }
  return expected;
}

/** Validate generated documents with the checked-in Draft 2020-12 schemas. */
function validateDocumentsAgainstSchemas(
  root: string,
  source: Record<string, unknown>,
  profile: Record<string, unknown>,
): void {
  const ajv = new Ajv2020({ allErrors: true, strict: true, validateFormats: false });
  const sourceSchema = readJson(root, RUNNER_BINDING_SOURCE_SCHEMA_PATH);
  const profileSchema = readJson(root, RUNNER_BINDING_PROFILE_SCHEMA_PATH);
  assertClosedDraft202012Schema(sourceSchema, RUNNER_BINDING_SOURCE_SCHEMA_PATH);
  assertClosedDraft202012Schema(profileSchema, RUNNER_BINDING_PROFILE_SCHEMA_PATH);
  ajv.addSchema(sourceSchema as never);
  const validateSource = ajv.getSchema(String(sourceSchema.$id));
  if (!validateSource || !validateSource(source)) {
    throw new Error(
      `RUNNER_BINDING_SOURCE_SCHEMA_INVALID: ${ajv.errorsText(validateSource?.errors)}`,
    );
  }
  ajv.addSchema(profileSchema as never);
  const validateProfile = ajv.getSchema(String(profileSchema.$id));
  if (!validateProfile || !validateProfile(profile)) {
    throw new Error(
      `RUNNER_BINDING_PROFILE_SCHEMA_INVALID: ${ajv.errorsText(validateProfile?.errors)}`,
    );
  }
}

function assertClosedDraft202012Schema(schema: Record<string, any>, path: string): void {
  if (
    schema.$schema !== "https://json-schema.org/draft/2020-12/schema" ||
    typeof schema.$id !== "string"
  ) {
    throw new Error(`RUNNER_BINDING_SCHEMA_METADATA_INVALID: ${path}`);
  }
  const missing: string[] = [];
  const walk = (value: unknown, at: string): void => {
    if (!value || typeof value !== "object") return;
    if (
      (value as Record<string, unknown>).type === "object" &&
      (value as Record<string, unknown>).additionalProperties !== false
    ) {
      missing.push(at);
    }
    for (const [key, child] of Object.entries(value as Record<string, unknown>))
      walk(child, `${at}/${key}`);
  };
  walk(schema, "");
  if (missing.length > 0)
    throw new Error(`RUNNER_BINDING_SCHEMA_NOT_CLOSED: ${path}: ${missing.join(",")}`);
}

function selectorFromR1(
  root: string,
  selectorId: string,
  schemaHead: string,
  manifestPath: string,
  schemaPath: string,
  r1Ref: Record<string, any>,
  expected: {
    manifestRawDigest: string;
    manifestDigest: string;
    schemaRawDigest: string;
    schemaDigest: string;
    migrationCount: number;
  },
): Record<string, MigrationJson> {
  const manifest = artifact(root, manifestPath);
  const schemaBundle = artifact(root, schemaPath);
  const manifestJson = readJson(root, manifestPath);
  const schemaJson = readJson(root, schemaPath);
  const schemaBundleDigest = String(manifestJson.schema_bundle_digest);
  const manifestDigest = String(manifestJson.manifest_digest);
  const migrationCount = Array.isArray(schemaJson.schema_bundle?.migrations)
    ? schemaJson.schema_bundle.migrations.length
    : 0;
  if (
    manifest.sha256 !== expected.manifestRawDigest ||
    manifestDigest !== expected.manifestDigest ||
    schemaBundle.sha256 !== expected.schemaRawDigest ||
    schemaBundleDigest !== expected.schemaDigest ||
    schemaJson.schema_bundle_digest !== schemaBundleDigest ||
    schemaJson.schema_bundle?.schema_head !== schemaHead ||
    migrationCount !== expected.migrationCount
  ) {
    throw new Error(`selector digest or shape mismatch: ${selectorId}`);
  }
  if (
    r1Ref?.manifest?.sha256 !== manifest.sha256 ||
    r1Ref?.schemaBundle?.sha256 !== schemaBundle.sha256
  ) {
    throw new Error(`selector schema digest mismatch: ${selectorId}`);
  }
  return {
    selectorId,
    schemaHead,
    manifest,
    manifestDigest,
    schemaBundle,
    schemaBundleDigest,
    runtimeManifestPath: manifestPath,
    runtimeSchemaBundlePath: schemaPath,
    artifactSet: clone(manifestJson.runtime_artifacts),
    migrationCount,
    completeLedger: "no-op",
    entryWriter: "NOT_IMPLEMENTED",
    recoveryWriter: "NOT_IMPLEMENTED",
  };
}

function generateGo(
  profileDigest: string,
  profileBlobDigest: string,
  sourceArtifact: RunnerBindingArtifact,
  profileArtifact: RunnerBindingArtifact,
  sourceSchemaArtifact: RunnerBindingArtifact,
  profileSchemaArtifact: RunnerBindingArtifact,
  selectors: Record<string, any>[],
): string {
  const selectorLines = selectors
    .map(
      (selector) =>
        `\t{selectorID: ${goString(selector.selectorId)}, schemaHead: ${goString(selector.schemaHead)}, manifestPath: ${goString(selector.manifest.path)}, manifestSizeBytes: ${String(selector.manifest.sizeBytes)}, manifestRawDigest: ${goString(selector.manifest.sha256)}, manifestDigest: ${goString(selector.manifestDigest)}, schemaBundlePath: ${goString(selector.schemaBundle.path)}, schemaBundleSizeBytes: ${String(selector.schemaBundle.sizeBytes)}, schemaBundleRawDigest: ${goString(selector.schemaBundle.sha256)}, schemaBundleDigest: ${goString(selector.schemaBundleDigest)}, migrationCount: ${String(selector.migrationCount)}},`,
    )
    .join("\n");
  const r1Lines = Object.values(R1_EXPECTED_ARTIFACTS)
    .map(
      (artifact) =>
        `\t{path: ${goString(artifact.path)}, mode: ${goString(artifact.mode)}, sizeBytes: ${String(artifact.sizeBytes)}, rawDigest: ${goString(artifact.sha256)}},`,
    )
    .join("\n");
  const constants: Array<[string, string]> = [
    ["runnerBindingProfilePath", goString(RUNNER_BINDING_PROFILE_PATH)],
    ["runnerBindingProfileDigest", goString(profileDigest)],
    ["runnerBindingProfileBlobDigest", goString(profileBlobDigest)],
    ["runnerBindingProfileSizeBytes", String(profileArtifact.sizeBytes)],
    ["runnerBindingProfileSchemaURL", goString(PROFILE_SCHEMA_URL)],
    ["runnerBindingProfileFormatVersion", goString(RUNNER_BINDING_PROFILE_ID)],
    ["runnerBindingProfileID", goString(RUNNER_BINDING_PROFILE_ID)],
    ["runnerBindingProfileDomain", goString(PROFILE_DOMAIN)],
    ["runnerBindingInputScope", goString(INPUT_SCOPE)],
    ["runnerBindingSourcePath", goString(sourceArtifact.path)],
    ["runnerBindingSourceRawDigest", goString(sourceArtifact.sha256)],
    ["runnerBindingSourceSizeBytes", String(sourceArtifact.sizeBytes)],
    ["runnerBindingSourceSchemaPath", goString(sourceSchemaArtifact.path)],
    ["runnerBindingSourceSchemaRawDigest", goString(sourceSchemaArtifact.sha256)],
    ["runnerBindingSourceSchemaSizeBytes", String(sourceSchemaArtifact.sizeBytes)],
    ["runnerBindingProfileSchemaPath", goString(profileSchemaArtifact.path)],
    ["runnerBindingProfileSchemaRawDigest", goString(profileSchemaArtifact.sha256)],
    ["runnerBindingProfileSchemaSizeBytes", String(profileSchemaArtifact.sizeBytes)],
    ["runnerBindingAuthorityID", goString(RUNNER_BINDING_AUTHORITY_ID)],
    ["runnerBindingRevision", goString(RUNNER_BINDING_REVISION)],
  ];
  const constWidth = Math.max(...constants.map(([name]) => name.length));
  const constLines = constants
    .map(([name, value]) => `\t${name}${" ".repeat(constWidth - name.length + 1)}= ${value}`)
    .join("\n");
  const artifactFields = alignedGoFields([
    ["path", "string"],
    ["mode", "string"],
    ["sizeBytes", "int"],
    ["rawDigest", "string"],
  ]);
  const selectorFields = alignedGoFields([
    ["selectorID", "string"],
    ["schemaHead", "string"],
    ["manifestPath", "string"],
    ["manifestSizeBytes", "int"],
    ["manifestRawDigest", "string"],
    ["manifestDigest", "string"],
    ["schemaBundlePath", "string"],
    ["schemaBundleSizeBytes", "int"],
    ["schemaBundleRawDigest", "string"],
    ["schemaBundleDigest", "string"],
    ["migrationCount", "int"],
  ]);
  return `// Code generated by scripts/generate-platform-migration-runner-binding.ts; DO NOT EDIT.\n//go:build localdev\n\npackage localmigration\n\nconst (\n${constLines}\n)\n\ntype generatedRunnerBindingArtifact struct {\n${artifactFields}\n}\n\nfunc generatedRunnerBindingSourceArtifact() generatedRunnerBindingArtifact {\n\treturn generatedRunnerBindingArtifact{path: runnerBindingSourcePath, mode: "100644", sizeBytes: runnerBindingSourceSizeBytes, rawDigest: runnerBindingSourceRawDigest}\n}\n\nfunc generatedRunnerBindingProfileArtifact() generatedRunnerBindingArtifact {\n\treturn generatedRunnerBindingArtifact{path: runnerBindingProfilePath, mode: "100644", sizeBytes: runnerBindingProfileSizeBytes, rawDigest: runnerBindingProfileBlobDigest}\n}\n\nfunc generatedRunnerBindingSourceSchemaArtifact() generatedRunnerBindingArtifact {\n\treturn generatedRunnerBindingArtifact{path: runnerBindingSourceSchemaPath, mode: "100644", sizeBytes: runnerBindingSourceSchemaSizeBytes, rawDigest: runnerBindingSourceSchemaRawDigest}\n}\n\nfunc generatedRunnerBindingProfileSchemaArtifact() generatedRunnerBindingArtifact {\n\treturn generatedRunnerBindingArtifact{path: runnerBindingProfileSchemaPath, mode: "100644", sizeBytes: runnerBindingProfileSchemaSizeBytes, rawDigest: runnerBindingProfileSchemaRawDigest}\n}\n\nvar generatedRunnerBindingR1Artifacts = [...]generatedRunnerBindingArtifact{\n${r1Lines}\n}\n\ntype generatedRunnerBindingSelector struct {\n${selectorFields}\n}\n\nvar generatedRunnerBindingSelectors = [...]generatedRunnerBindingSelector{\n${selectorLines}\n}\n`;
}

function alignedGoFields(fields: ReadonlyArray<readonly [string, string]>): string {
  const width = Math.max(...fields.map(([name]) => name.length));
  return fields
    .map(([name, type]) => `\t${name}${" ".repeat(width - name.length + 1)}${type}`)
    .join("\n");
}

function clone(value: any): any {
  return value === undefined ? undefined : JSON.parse(JSON.stringify(value));
}

function readJson(root: string, path: string): Record<string, any> {
  return parseStrictMigrationJson(readRegular(root, path)) as Record<string, any>;
}

function artifact(root: string, path: string): RunnerBindingArtifact {
  return artifactBytes(path, readRegular(root, path));
}

function artifactBytes(path: string, bytes: Uint8Array): RunnerBindingArtifact {
  return { path, mode: "100644", sizeBytes: bytes.length, sha256: digestBytes(bytes) };
}

function sameArtifact(left: RunnerBindingArtifact, right: RunnerBindingArtifact): boolean {
  return (
    left.path === right.path &&
    left.mode === right.mode &&
    left.sizeBytes === right.sizeBytes &&
    left.sha256 === right.sha256
  );
}

function readRegular(root: string, path: string): Uint8Array {
  const stat = lstatSync(resolve(root, path));
  if (!stat.isFile() || stat.isSymbolicLink() || (stat.mode & 0o7777) !== 0o644) {
    throw new Error(`runner binding artifact is not a regular 0644 file: ${path}`);
  }
  return readFileSync(resolve(root, path));
}

function pretty(value: MigrationJson): Uint8Array {
  return new TextEncoder().encode(`${JSON.stringify(value, null, 2)}\n`);
}

function digestBytes(bytes: Uint8Array): string {
  return `sha256:${createHash("sha256").update(bytes).digest("hex")}`;
}

function domainDigest(domain: string, value: MigrationJson): string {
  return `sha256:${createHash("sha256").update(domain).update("\0").update(canonicalizeMigrationJson(value)).digest("hex")}`;
}

function goString(value: string): string {
  return JSON.stringify(value);
}
