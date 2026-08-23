import { spawnSync } from "node:child_process";
import { existsSync, lstatSync, readFileSync, realpathSync, writeFileSync } from "node:fs";
import { dirname, relative, resolve } from "node:path";

import { assertIdentityVerifierRegistryCurrent } from "./platform-identity-verifier-registry";

export const IDENTITY_VERIFIER_REGISTRY_OUTPUT_PATH =
  "contracts/generated/platform/v1alpha1/identity-verifier-registry-v1.json";
export const IDENTITY_VERIFIER_GO_OUTPUT_PATH =
  "services/control-plane/internal/authn/profile_generated.go";

type JSONObject = Record<string, unknown>;
type Entry = readonly [field: string, literal: string];

function isWithin(root: string, target: string): boolean {
  const path = relative(root, target);
  return path === "" || (!path.startsWith("..") && !path.startsWith(`/`));
}

function regularInput(root: string, path: string): string {
  const rootReal = realpathSync(root);
  const target = resolve(rootReal, path);
  if (!isWithin(rootReal, target)) throw new Error(`${path} escapes the repository root.`);
  const stat = lstatSync(target);
  if (!stat.isFile() || stat.isSymbolicLink()) {
    throw new Error(`${path} must be a non-symlink regular file.`);
  }
  const targetReal = realpathSync(target);
  if (!isWithin(rootReal, targetReal))
    throw new Error(`${path} resolves outside the repository root.`);
  return targetReal;
}

function safeOutput(root: string, path: string): string {
  const rootReal = realpathSync(root);
  const target = resolve(rootReal, path);
  if (!isWithin(rootReal, target)) throw new Error(`${path} escapes the repository root.`);
  const parentReal = realpathSync(dirname(target));
  if (!isWithin(rootReal, parentReal))
    throw new Error(`${path} has a parent outside the repository root.`);
  if (existsSync(target)) {
    const stat = lstatSync(target);
    if (!stat.isFile() || stat.isSymbolicLink()) {
      throw new Error(`${path} must be a non-symlink regular file.`);
    }
    if (!isWithin(rootReal, realpathSync(target))) {
      throw new Error(`${path} resolves outside the repository root.`);
    }
  }
  return target;
}

function object(value: unknown, keys: ReadonlyArray<string>, name: string): JSONObject {
  if (value === null || typeof value !== "object" || Array.isArray(value)) {
    throw new Error(`${name} must be an object.`);
  }
  const record = value as JSONObject;
  const actual = Object.keys(record).toSorted();
  const expected = [...keys].toSorted();
  if (JSON.stringify(actual) !== JSON.stringify(expected)) {
    throw new Error(`${name} has a non-closed field set.`);
  }
  return record;
}

function stringValue(record: JSONObject, key: string, name: string): string {
  const value = record[key];
  if (typeof value !== "string" || value.length === 0) {
    throw new Error(`${name}.${key} must be a non-empty string.`);
  }
  return value;
}

function uint32Value(record: JSONObject, key: string, name: string): number {
  const value = record[key];
  if (!Number.isSafeInteger(value) || (value as number) < 0 || (value as number) > 0xffffffff) {
    throw new Error(`${name}.${key} must be a uint32.`);
  }
  return value as number;
}

function stringArrayValue(
  record: JSONObject,
  key: string,
  length: number,
  name: string,
): ReadonlyArray<string> {
  const value = record[key];
  if (
    !Array.isArray(value) ||
    value.length !== length ||
    value.some((item) => typeof item !== "string" || item.length === 0)
  ) {
    throw new Error(`${name}.${key} must contain exactly ${length} non-empty strings.`);
  }
  return value as ReadonlyArray<string>;
}

function requireExact(actual: string, expected: string, name: string): void {
  if (actual !== expected) throw new Error(`${name} drifted from ${JSON.stringify(expected)}.`);
}

function requireDigest(value: string, name: string): void {
  if (!/^sha256:[0-9a-f]{64}$/.test(value)) {
    throw new Error(`${name} must be a lowercase SHA-256 digest.`);
  }
}

function goString(value: string): string {
  return JSON.stringify(value);
}

function goStrings(values: ReadonlyArray<string>): string {
  return `[${values.length}]string{${values.map(goString).join(", ")}}`;
}

function goStruct(type: string, entries: ReadonlyArray<Entry>): string {
  return `${type}{\n${entries.map(([field, value]) => `\t${field}: ${value},`).join("\n")}\n}`;
}

function fields(record: JSONObject, name: string, keys: ReadonlyArray<string>): JSONObject {
  return object(record, keys, name);
}

function renderProfile(registryValue: unknown): string {
  const registry = object(
    registryValue,
    ["formatVersion", "registryId", "profile", "registryDigest"],
    "registry",
  );
  const formatVersion = stringValue(registry, "formatVersion", "registry");
  const registryID = stringValue(registry, "registryId", "registry");
  const registryDigest = stringValue(registry, "registryDigest", "registry");
  requireExact(
    formatVersion,
    "cloud-agents-platform-identity-verifier-registry/v1",
    "registry.formatVersion",
  );
  requireExact(registryID, "cloud-agents/platform/identity-verifier", "registry.registryId");
  requireDigest(registryDigest, "registry.registryDigest");

  const profileKeys = [
    "profileId",
    "token",
    "algorithm",
    "protectedHeader",
    "jwk",
    "claims",
    "limits",
    "lexicalRules",
    "timeRules",
    "parsingRules",
    "bindingRules",
    "errorRules",
    "digestRules",
    "keyLineage",
    "trustSnapshot",
    "verificationContext",
    "verifiedPrincipal",
    "implementationNonClaims",
    "profileDigest",
  ] as const;
  const profile = object(registry.profile, profileKeys, "registry.profile");
  const profileID = stringValue(profile, "profileId", "registry.profile");
  const profileDigest = stringValue(profile, "profileDigest", "registry.profile");
  requireExact(profileID, "platform-identity-verifier/v1", "registry.profile.profileId");
  requireDigest(profileDigest, "registry.profile.profileDigest");

  const token = fields(profile.token as JSONObject, "profile.token", [
    "serialization",
    "segmentCount",
    "signature",
    "acceptedTypes",
    "typeComparison",
    "canonicalType",
    "forbiddenForms",
  ]);
  const algorithm = fields(profile.algorithm as JSONObject, "profile.algorithm", [
    "accepted",
    "selectionAuthority",
    "implementation",
    "none",
    "hmac",
    "callerSelected",
  ]);
  const header = fields(profile.protectedHeader as JSONObject, "profile.protectedHeader", [
    "requiredMembers",
    "allowedMembers",
    "algorithmComparison",
    "forbiddenMembers",
    "unknownMembers",
    "tokenSuppliedKeyLookup",
  ]);
  const jwk = fields(profile.jwk as JSONObject, "profile.jwk", [
    "allowedMembers",
    "requiredMembers",
    "kty",
    "alg",
    "use",
    "keyOps",
    "exponentBase64urlUInt",
    "exponentDecimal",
    "modulusEncoding",
    "privateMembers",
    "privateOrSymmetricMaterial",
    "unknownMembers",
  ]);
  const claims = fields(profile.claims as JSONObject, "profile.claims", [
    "requiredRegistered",
    "subjectKindClaim",
    "tenantIdClaim",
    "projectIdClaim",
    "securityEpochClaim",
    "tokenProfileClaim",
    "tokenProfileValue",
    "requiredCustom",
    "optionalCustom",
    "subjectKinds",
    "audienceCardinality",
    "scopeEncoding",
    "projectBinding",
    "additionalSignedClaims",
  ]);
  const limitKeys = [
    "compactTokenBytes",
    "decodedProtectedHeaderBytes",
    "decodedClaimsBytes",
    "jsonDepth",
    "trustSnapshotBytes",
    "lifetimeKeyLineageRecords",
    "audiences",
    "scopes",
    "revokedTokenIds",
    "kidBytes",
    "issuerScalars",
    "audienceScalars",
    "subjectScalars",
    "clientIdScalars",
    "tokenIdScalars",
    "opaqueIdentifierBytes",
    "scopeItemBytesMin",
    "scopeItemBytesMax",
    "rsaModulusBitsMin",
    "rsaModulusBitsMax",
    "tokenLifetimeSeconds",
    "clockSkewSeconds",
    "trustSnapshotValiditySeconds",
  ] as const;
  const limits = fields(profile.limits as JSONObject, "profile.limits", limitKeys);
  const lexical = fields(profile.lexicalRules as JSONObject, "profile.lexicalRules", [
    "decodedStringComparison",
    "jsonEscapeEquivalence",
    "issuerAndAudience",
    "subject",
    "clientIdAndTokenId",
    "opaqueIdentifier",
    "audience",
    "scopeSplit",
    "scopeItemPattern",
    "scopeOrdering",
    "integerEncoding",
    "epochAndGenerationRange",
    "numericDateRange",
    "compactBase64url",
  ]);
  const time = fields(profile.timeRules as JSONObject, "profile.timeRules", [
    "clock",
    "tokenChecks",
    "keyIssuanceInterval",
    "snapshotInterval",
    "snapshotMaximumValidity",
    "snapshotClockSkew",
  ]);
  const parsing = fields(profile.parsingRules as JSONObject, "profile.parsingRules", [
    "duplicateDecodedMembers",
    "jsonEncoding",
    "topLevel",
    "numericDates",
    "sizeAndDepthAdmission",
    "base64url",
    "claimsObject",
  ]);
  const binding = fields(profile.bindingRules as JSONObject, "profile.bindingRules", [
    "issuer",
    "key",
    "audience",
    "tokenType",
    "time",
    "revocation",
    "securityEpoch",
    "subject",
    "tenant",
    "project",
    "permission",
    "profile",
    "inference",
  ]);
  const errors = fields(profile.errorRules as JSONObject, "profile.errorRules", [
    "categories",
    "stability",
    "redactedFacts",
    "redactionSurfaces",
  ]);
  const digest = fields(profile.digestRules as JSONObject, "profile.digestRules", [
    "algorithm",
    "textFormat",
    "framing",
    "jsonCanonicalization",
    "setArrayOrdering",
    "keyAndLineageOrdering",
    "domains",
    "projections",
    "ordinaryGenerationLockHashes",
  ]);
  const domains = fields(digest.domains as JSONObject, "profile.digestRules.domains", [
    "profile",
    "registry",
    "trustSnapshot",
    "tokenInput",
    "verifiedPrincipal",
  ]);
  const projections = fields(digest.projections as JSONObject, "profile.digestRules.projections", [
    "profile",
    "registry",
    "trustSnapshot",
    "tokenInput",
    "verifiedPrincipal",
  ]);
  const lineage = fields(profile.keyLineage as JSONObject, "profile.keyLineage", [
    "generationStart",
    "generationStep",
    "previousSnapshotAtGenerationOne",
    "previousSnapshotAfterGenerationOne",
    "kidBinding",
    "sameKidNewMaterial",
    "sameKidSameMaterial",
    "records",
    "overflow",
  ]);
  const snapshot = fields(profile.trustSnapshot as JSONObject, "profile.trustSnapshot", [
    "authority",
    "provisioning",
    "requiredFacts",
    "mutation",
    "selection",
    "externalLookup",
    "invalidation",
  ]);
  const context = fields(profile.verificationContext as JSONObject, "profile.verificationContext", [
    "authority",
    "requiredFacts",
    "audienceAuthority",
    "tenantProjectPermissionAuthority",
    "productionConstructor",
  ]);
  const principal = fields(profile.verifiedPrincipal as JSONObject, "profile.verifiedPrincipal", [
    "construction",
    "lifetime",
    "consumption",
    "leaseScope",
    "boundFacts",
    "forbiddenPayloadFacts",
    "secondOrConcurrentConsume",
  ]);
  const nonClaims = fields(
    profile.implementationNonClaims as JSONObject,
    "profile.implementationNonClaims",
    [
      "httpSurface",
      "oidcDiscovery",
      "remoteJwks",
      "providerSideEffects",
      "p2Surface",
      "productionTrustProvisioning",
      "productionDatabaseWrites",
      "deployment",
      "publication",
      "gateStatus",
    ],
  );

  const s = (record: JSONObject, key: string, name: string) =>
    goString(stringValue(record, key, name));
  const n = (record: JSONObject, key: string, name: string) =>
    String(uint32Value(record, key, name));
  const a = (record: JSONObject, key: string, length: number, name: string) =>
    goStrings(stringArrayValue(record, key, length, name));

  const tokenLiteral = goStruct("identityVerifierTokenRules", [
    ["serialization", s(token, "serialization", "profile.token")],
    ["segmentCount", n(token, "segmentCount", "profile.token")],
    ["signature", s(token, "signature", "profile.token")],
    ["acceptedTypes", a(token, "acceptedTypes", 2, "profile.token")],
    ["typeComparison", s(token, "typeComparison", "profile.token")],
    ["canonicalType", s(token, "canonicalType", "profile.token")],
    ["forbiddenForms", a(token, "forbiddenForms", 6, "profile.token")],
  ]);
  const algorithmLiteral = goStruct("identityVerifierAlgorithmRules", [
    ["accepted", a(algorithm, "accepted", 1, "profile.algorithm")],
    ["selectionAuthority", s(algorithm, "selectionAuthority", "profile.algorithm")],
    ["implementation", s(algorithm, "implementation", "profile.algorithm")],
    ["none", s(algorithm, "none", "profile.algorithm")],
    ["hmac", s(algorithm, "hmac", "profile.algorithm")],
    ["callerSelected", s(algorithm, "callerSelected", "profile.algorithm")],
  ]);
  const headerLiteral = goStruct("identityVerifierProtectedHeaderRules", [
    ["requiredMembers", a(header, "requiredMembers", 3, "profile.protectedHeader")],
    ["allowedMembers", a(header, "allowedMembers", 3, "profile.protectedHeader")],
    ["algorithmComparison", s(header, "algorithmComparison", "profile.protectedHeader")],
    ["forbiddenMembers", a(header, "forbiddenMembers", 5, "profile.protectedHeader")],
    ["unknownMembers", s(header, "unknownMembers", "profile.protectedHeader")],
    ["tokenSuppliedKeyLookup", s(header, "tokenSuppliedKeyLookup", "profile.protectedHeader")],
  ]);
  const jwkLiteral = goStruct("identityVerifierJWKRules", [
    ["allowedMembers", a(jwk, "allowedMembers", 7, "profile.jwk")],
    ["requiredMembers", a(jwk, "requiredMembers", 7, "profile.jwk")],
    ["kty", s(jwk, "kty", "profile.jwk")],
    ["alg", s(jwk, "alg", "profile.jwk")],
    ["use", s(jwk, "use", "profile.jwk")],
    ["keyOps", a(jwk, "keyOps", 1, "profile.jwk")],
    ["exponentBase64urlUInt", s(jwk, "exponentBase64urlUInt", "profile.jwk")],
    ["exponentDecimal", n(jwk, "exponentDecimal", "profile.jwk")],
    ["modulusEncoding", s(jwk, "modulusEncoding", "profile.jwk")],
    ["privateMembers", a(jwk, "privateMembers", 7, "profile.jwk")],
    ["privateOrSymmetricMaterial", s(jwk, "privateOrSymmetricMaterial", "profile.jwk")],
    ["unknownMembers", s(jwk, "unknownMembers", "profile.jwk")],
  ]);
  const claimsLiteral = goStruct("identityVerifierClaimRules", [
    ["requiredRegistered", a(claims, "requiredRegistered", 8, "profile.claims")],
    ["subjectKindClaim", s(claims, "subjectKindClaim", "profile.claims")],
    ["tenantIDClaim", s(claims, "tenantIdClaim", "profile.claims")],
    ["projectIDClaim", s(claims, "projectIdClaim", "profile.claims")],
    ["securityEpochClaim", s(claims, "securityEpochClaim", "profile.claims")],
    ["tokenProfileClaim", s(claims, "tokenProfileClaim", "profile.claims")],
    ["tokenProfileValue", s(claims, "tokenProfileValue", "profile.claims")],
    ["requiredCustom", a(claims, "requiredCustom", 4, "profile.claims")],
    ["optionalCustom", a(claims, "optionalCustom", 1, "profile.claims")],
    ["subjectKinds", a(claims, "subjectKinds", 3, "profile.claims")],
    ["audienceCardinality", s(claims, "audienceCardinality", "profile.claims")],
    ["scopeEncoding", s(claims, "scopeEncoding", "profile.claims")],
    ["projectBinding", s(claims, "projectBinding", "profile.claims")],
    ["additionalSignedClaims", s(claims, "additionalSignedClaims", "profile.claims")],
  ]);
  const limitFieldNames = [
    "compactTokenBytes",
    "decodedProtectedHeaderBytes",
    "decodedClaimsBytes",
    "jsonDepth",
    "trustSnapshotBytes",
    "lifetimeKeyLineageRecords",
    "audiences",
    "scopes",
    "revokedTokenIDs",
    "kidBytes",
    "issuerScalars",
    "audienceScalars",
    "subjectScalars",
    "clientIDScalars",
    "tokenIDScalars",
    "opaqueIdentifierBytes",
    "scopeItemBytesMin",
    "scopeItemBytesMax",
    "rsaModulusBitsMin",
    "rsaModulusBitsMax",
    "tokenLifetimeSeconds",
    "clockSkewSeconds",
    "trustSnapshotValiditySeconds",
  ] as const;
  const limitsLiteral = goStruct(
    "identityVerifierLimits",
    limitKeys.map((key, index) => [limitFieldNames[index]!, n(limits, key, "profile.limits")]),
  );
  const lexicalLiteral = goStruct("identityVerifierLexicalRules", [
    ["decodedStringComparison", s(lexical, "decodedStringComparison", "profile.lexicalRules")],
    ["jsonEscapeEquivalence", s(lexical, "jsonEscapeEquivalence", "profile.lexicalRules")],
    ["issuerAndAudience", s(lexical, "issuerAndAudience", "profile.lexicalRules")],
    ["subject", s(lexical, "subject", "profile.lexicalRules")],
    ["clientIDAndTokenID", s(lexical, "clientIdAndTokenId", "profile.lexicalRules")],
    ["opaqueIdentifier", s(lexical, "opaqueIdentifier", "profile.lexicalRules")],
    ["audience", s(lexical, "audience", "profile.lexicalRules")],
    ["scopeSplit", s(lexical, "scopeSplit", "profile.lexicalRules")],
    ["scopeItemPattern", s(lexical, "scopeItemPattern", "profile.lexicalRules")],
    ["scopeOrdering", s(lexical, "scopeOrdering", "profile.lexicalRules")],
    ["integerEncoding", s(lexical, "integerEncoding", "profile.lexicalRules")],
    ["epochAndGenerationRange", s(lexical, "epochAndGenerationRange", "profile.lexicalRules")],
    ["numericDateRange", s(lexical, "numericDateRange", "profile.lexicalRules")],
    ["compactBase64url", s(lexical, "compactBase64url", "profile.lexicalRules")],
  ]);
  const timeLiteral = goStruct("identityVerifierTimeRules", [
    ["clock", s(time, "clock", "profile.timeRules")],
    ["tokenChecks", a(time, "tokenChecks", 6, "profile.timeRules")],
    ["keyIssuanceInterval", s(time, "keyIssuanceInterval", "profile.timeRules")],
    ["snapshotInterval", s(time, "snapshotInterval", "profile.timeRules")],
    ["snapshotMaximumValidity", s(time, "snapshotMaximumValidity", "profile.timeRules")],
    ["snapshotClockSkew", s(time, "snapshotClockSkew", "profile.timeRules")],
  ]);
  const parsingLiteral = goStruct("identityVerifierParsingRules", [
    ["duplicateDecodedMembers", s(parsing, "duplicateDecodedMembers", "profile.parsingRules")],
    ["jsonEncoding", s(parsing, "jsonEncoding", "profile.parsingRules")],
    ["topLevel", s(parsing, "topLevel", "profile.parsingRules")],
    ["numericDates", s(parsing, "numericDates", "profile.parsingRules")],
    ["sizeAndDepthAdmission", s(parsing, "sizeAndDepthAdmission", "profile.parsingRules")],
    ["base64url", s(parsing, "base64url", "profile.parsingRules")],
    ["claimsObject", s(parsing, "claimsObject", "profile.parsingRules")],
  ]);
  const bindingLiteral = goStruct("identityVerifierBindingRules", [
    ["issuer", s(binding, "issuer", "profile.bindingRules")],
    ["key", s(binding, "key", "profile.bindingRules")],
    ["audience", s(binding, "audience", "profile.bindingRules")],
    ["tokenType", s(binding, "tokenType", "profile.bindingRules")],
    ["time", s(binding, "time", "profile.bindingRules")],
    ["revocation", s(binding, "revocation", "profile.bindingRules")],
    ["securityEpoch", s(binding, "securityEpoch", "profile.bindingRules")],
    ["subject", s(binding, "subject", "profile.bindingRules")],
    ["tenant", s(binding, "tenant", "profile.bindingRules")],
    ["project", s(binding, "project", "profile.bindingRules")],
    ["permission", s(binding, "permission", "profile.bindingRules")],
    ["profile", s(binding, "profile", "profile.bindingRules")],
    ["inference", s(binding, "inference", "profile.bindingRules")],
  ]);
  const errorsLiteral = goStruct("identityVerifierErrorRules", [
    ["categories", a(errors, "categories", 15, "profile.errorRules")],
    ["stability", s(errors, "stability", "profile.errorRules")],
    ["redactedFacts", a(errors, "redactedFacts", 4, "profile.errorRules")],
    ["redactionSurfaces", a(errors, "redactionSurfaces", 4, "profile.errorRules")],
  ]);
  const domainsLiteral = goStruct("identityVerifierDigestDomains", [
    ["profile", s(domains, "profile", "profile.digestRules.domains")],
    ["registry", s(domains, "registry", "profile.digestRules.domains")],
    ["trustSnapshot", s(domains, "trustSnapshot", "profile.digestRules.domains")],
    ["tokenInput", s(domains, "tokenInput", "profile.digestRules.domains")],
    ["verifiedPrincipal", s(domains, "verifiedPrincipal", "profile.digestRules.domains")],
  ]);
  const projectionsLiteral = goStruct("identityVerifierDigestProjections", [
    ["profile", s(projections, "profile", "profile.digestRules.projections")],
    ["registry", s(projections, "registry", "profile.digestRules.projections")],
    ["trustSnapshot", s(projections, "trustSnapshot", "profile.digestRules.projections")],
    ["tokenInput", s(projections, "tokenInput", "profile.digestRules.projections")],
    ["verifiedPrincipal", s(projections, "verifiedPrincipal", "profile.digestRules.projections")],
  ]);
  const digestLiteral = goStruct("identityVerifierDigestRules", [
    ["algorithm", s(digest, "algorithm", "profile.digestRules")],
    ["textFormat", s(digest, "textFormat", "profile.digestRules")],
    ["framing", s(digest, "framing", "profile.digestRules")],
    ["jsonCanonicalization", s(digest, "jsonCanonicalization", "profile.digestRules")],
    ["setArrayOrdering", s(digest, "setArrayOrdering", "profile.digestRules")],
    ["keyAndLineageOrdering", s(digest, "keyAndLineageOrdering", "profile.digestRules")],
    ["domains", domainsLiteral],
    ["projections", projectionsLiteral],
    [
      "ordinaryGenerationLockHashes",
      s(digest, "ordinaryGenerationLockHashes", "profile.digestRules"),
    ],
  ]);
  const lineageLiteral = goStruct("identityVerifierKeyLineageRules", [
    ["generationStart", n(lineage, "generationStart", "profile.keyLineage")],
    ["generationStep", n(lineage, "generationStep", "profile.keyLineage")],
    [
      "previousSnapshotAtGenerationOne",
      s(lineage, "previousSnapshotAtGenerationOne", "profile.keyLineage"),
    ],
    [
      "previousSnapshotAfterGenerationOne",
      s(lineage, "previousSnapshotAfterGenerationOne", "profile.keyLineage"),
    ],
    ["kidBinding", s(lineage, "kidBinding", "profile.keyLineage")],
    ["sameKidNewMaterial", s(lineage, "sameKidNewMaterial", "profile.keyLineage")],
    ["sameKidSameMaterial", s(lineage, "sameKidSameMaterial", "profile.keyLineage")],
    ["records", s(lineage, "records", "profile.keyLineage")],
    ["overflow", s(lineage, "overflow", "profile.keyLineage")],
  ]);
  const snapshotLiteral = goStruct("identityVerifierTrustSnapshotRules", [
    ["authority", s(snapshot, "authority", "profile.trustSnapshot")],
    ["provisioning", s(snapshot, "provisioning", "profile.trustSnapshot")],
    ["requiredFacts", a(snapshot, "requiredFacts", 12, "profile.trustSnapshot")],
    ["mutation", s(snapshot, "mutation", "profile.trustSnapshot")],
    ["selection", s(snapshot, "selection", "profile.trustSnapshot")],
    ["externalLookup", s(snapshot, "externalLookup", "profile.trustSnapshot")],
    ["invalidation", s(snapshot, "invalidation", "profile.trustSnapshot")],
  ]);
  const contextLiteral = goStruct("identityVerifierContextRules", [
    ["authority", s(context, "authority", "profile.verificationContext")],
    ["requiredFacts", a(context, "requiredFacts", 6, "profile.verificationContext")],
    ["audienceAuthority", s(context, "audienceAuthority", "profile.verificationContext")],
    [
      "tenantProjectPermissionAuthority",
      s(context, "tenantProjectPermissionAuthority", "profile.verificationContext"),
    ],
    ["productionConstructor", s(context, "productionConstructor", "profile.verificationContext")],
  ]);
  const principalLiteral = goStruct("identityVerifierPrincipalRules", [
    ["construction", s(principal, "construction", "profile.verifiedPrincipal")],
    ["lifetime", s(principal, "lifetime", "profile.verifiedPrincipal")],
    ["consumption", s(principal, "consumption", "profile.verifiedPrincipal")],
    ["leaseScope", s(principal, "leaseScope", "profile.verifiedPrincipal")],
    ["boundFacts", a(principal, "boundFacts", 17, "profile.verifiedPrincipal")],
    [
      "forbiddenPayloadFacts",
      a(principal, "forbiddenPayloadFacts", 7, "profile.verifiedPrincipal"),
    ],
    [
      "secondOrConcurrentConsume",
      s(principal, "secondOrConcurrentConsume", "profile.verifiedPrincipal"),
    ],
  ]);
  const nonClaimsLiteral = goStruct("identityVerifierImplementationNonClaims", [
    ["httpSurface", s(nonClaims, "httpSurface", "profile.implementationNonClaims")],
    ["oidcDiscovery", s(nonClaims, "oidcDiscovery", "profile.implementationNonClaims")],
    ["remoteJWKs", s(nonClaims, "remoteJwks", "profile.implementationNonClaims")],
    ["providerSideEffects", s(nonClaims, "providerSideEffects", "profile.implementationNonClaims")],
    ["p2Surface", s(nonClaims, "p2Surface", "profile.implementationNonClaims")],
    [
      "productionTrustProvisioning",
      s(nonClaims, "productionTrustProvisioning", "profile.implementationNonClaims"),
    ],
    [
      "productionDatabaseWrites",
      s(nonClaims, "productionDatabaseWrites", "profile.implementationNonClaims"),
    ],
    ["deployment", s(nonClaims, "deployment", "profile.implementationNonClaims")],
    ["publication", s(nonClaims, "publication", "profile.implementationNonClaims")],
    ["gateStatus", s(nonClaims, "gateStatus", "profile.implementationNonClaims")],
  ]);

  const profileLiteral = goStruct("identityVerifierProfile", [
    ["formatVersion", goString(formatVersion)],
    ["registryID", goString(registryID)],
    ["registryDigest", goString(registryDigest)],
    ["profileID", goString(profileID)],
    ["profileDigest", goString(profileDigest)],
    ["token", tokenLiteral],
    ["algorithm", algorithmLiteral],
    ["protectedHeader", headerLiteral],
    ["jwk", jwkLiteral],
    ["claims", claimsLiteral],
    ["limits", limitsLiteral],
    ["lexicalRules", lexicalLiteral],
    ["timeRules", timeLiteral],
    ["parsingRules", parsingLiteral],
    ["bindingRules", bindingLiteral],
    ["errorRules", errorsLiteral],
    ["digestRules", digestLiteral],
    ["keyLineage", lineageLiteral],
    ["trustSnapshot", snapshotLiteral],
    ["verificationContext", contextLiteral],
    ["verifiedPrincipal", principalLiteral],
    ["implementationNonClaims", nonClaimsLiteral],
  ]);

  return `const (\n\tidentityVerifierRegistryDigest = ${goString(registryDigest)}\n\tidentityVerifierProfileDigest = ${goString(profileDigest)}\n)\n\nfunc generatedIdentityVerifierProfile() identityVerifierProfile {\n\treturn ${profileLiteral}\n}`;
}

function formatGo(source: string): string {
  const result = spawnSync("gofmt", [], { input: source, encoding: "utf8" });
  if (result.status !== 0 || result.signal !== null) {
    throw new Error(`gofmt failed: ${result.stderr || result.signal || result.status}`);
  }
  return result.stdout;
}

export function serializeIdentityVerifierGo(registry: unknown): string {
  return formatGo(
    `// Code generated by scripts/generate-platform-identity-verifier-go.ts; DO NOT EDIT.\n\npackage authn\n\n${renderProfile(registry)}\n`,
  );
}

export function buildIdentityVerifierGo(root: string): string {
  assertIdentityVerifierRegistryCurrent(root);
  const input = regularInput(root, IDENTITY_VERIFIER_REGISTRY_OUTPUT_PATH);
  const registry = JSON.parse(readFileSync(input, "utf8")) as unknown;
  return serializeIdentityVerifierGo(registry);
}

export function writeIdentityVerifierGo(root: string): void {
  const generated = buildIdentityVerifierGo(root);
  const output = safeOutput(root, IDENTITY_VERIFIER_GO_OUTPUT_PATH);
  writeFileSync(output, generated);
}

export function assertIdentityVerifierGoCurrent(root: string): void {
  const generated = buildIdentityVerifierGo(root);
  const output = safeOutput(root, IDENTITY_VERIFIER_GO_OUTPUT_PATH);
  if (!existsSync(output) || readFileSync(output, "utf8") !== generated) {
    throw new Error(
      `${IDENTITY_VERIFIER_GO_OUTPUT_PATH} is stale; run its generator with --write.`,
    );
  }
}
