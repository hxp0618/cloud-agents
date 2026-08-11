import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { describe, expect, it } from "vitest";

import { type MigrationJson, migrationDigest } from "./platform-migration-json";
import {
  canonicalExactNumeric,
  canonicalRyuFloat,
  canonicalSignedInteger,
  catalogStateDigest,
  decodeAuthorityBinding,
  type JsonObject,
  validateAttemptTerminalState,
  validateAuthorityBinding,
  validateAuthorityProfile,
  validateCatalogProjectionBody,
  validateCatalogState,
  validateExpectedStatementTransition,
  validateIntermediateState,
  validateNumericFixture,
  validateObjectIdentity,
} from "./platform-migration-projection";

const root = resolve(import.meta.dirname, "../..");
const fixtureRoot = resolve(root, "services/control-plane/migrations/fixtures/projection");

describe("P1-A2.1a strict authority and catalog projection contracts", () => {
  it("accepts the three complete authority phases without claiming signed trust", () => {
    const binding = fixture("golden/authority-binding-v1.json");
    expect(() => validateAuthorityBinding(binding)).not.toThrow();
    const projections = binding.expected_projections as JsonObject;
    expect(Object.keys(projections)).toEqual([
      "connected_session",
      "migration_role",
      "migration_transaction",
    ]);
    expect((projections.connected_session as JsonObject).current_user).toBe(
      "cloud_agents_migration_login_fixture",
    );
    expect((projections.migration_role as JsonObject).current_user).toBe(
      "cloud_agents_migration_owner",
    );
    const reachability = (
      (projections.connected_session as JsonObject).membership_reachability as JsonObject[]
    )[0]!;
    expect((reachability.privileges as JsonObject[]).map((entry) => entry.privilege_kind)).toEqual([
      "member",
      "usage",
      "set",
    ]);
  });

  it("rejects unknown, missing, duplicate, digest, phase and ACL faults", () => {
    const binding = fixture("golden/authority-binding-v1.json");
    const unknown = structuredClone(binding);
    unknown.unsigned_overlay = true;
    expect(() => validateAuthorityBinding(unknown)).toThrow(/UNKNOWN_OR_MISSING_FIELD/u);

    const missing = structuredClone(binding);
    delete missing.expires_at;
    expect(() => validateAuthorityBinding(missing)).toThrow(/UNKNOWN_OR_MISSING_FIELD/u);

    expect(() =>
      decodeAuthorityBinding(
        readFileSync(resolve(fixtureRoot, "negative/authority-binding-duplicate.raw")),
      ),
    ).toThrow(/DUPLICATE_JSON_KEY/u);

    const badDigest = structuredClone(binding);
    badDigest.authority_profile_digest = "sha256:ABC";
    expect(() => validateAuthorityBinding(badDigest)).toThrow(/DIGEST_FORMAT/u);

    const badPhase = structuredClone(binding);
    const phaseProjection = (badPhase.expected_projections as JsonObject)
      .migration_transaction as JsonObject;
    phaseProjection.phase = "migration_role";
    expect(() => validateAuthorityBinding(badPhase)).toThrow(/AUTHORITY_PHASE/u);

    const badACL = structuredClone(binding);
    const databaseACL = (
      (badACL.expected_projections as JsonObject).connected_session as JsonObject
    ).database_acl as JsonObject;
    databaseACL.catalog_value = "null";
    expect(() => validateAuthorityBinding(badACL)).toThrow(/ACL_NULL_ENTRIES/u);

    const unsafeConfig = structuredClone(binding);
    const roles = (
      (unsafeConfig.expected_projections as JsonObject).connected_session as JsonObject
    ).roles as JsonObject[];
    roles[0]!.config = ["password=fixture-secret"];
    expect(() => validateAuthorityBinding(unsafeConfig)).toThrow(/ROLE_CONFIG_UNSAFE/u);

    const nonemptySafeConfig = structuredClone(binding);
    const safeRoles = (
      (nonemptySafeConfig.expected_projections as JsonObject).connected_session as JsonObject
    ).roles as JsonObject[];
    safeRoles[0]!.config = ["statement_timeout=300000"];
    expect(() => validateAuthorityBinding(nonemptySafeConfig)).toThrow(
      /ROLE_CONFIG_INITIAL_NONEMPTY/u,
    );

    const authority = JSON.parse(
      readFileSync(
        resolve(root, "services/control-plane/migrations/catalog/authority-v1.json"),
        "utf8",
      ),
    ) as JsonObject;
    const icu = structuredClone(authority);
    (icu.database as JsonObject).icu_locale = "en-US";
    expect(() => validateAuthorityProfile(icu)).toThrow(/UNEXPECTED_VALUE/u);
    const collationVersion = structuredClone(authority);
    (collationVersion.database as JsonObject).collation_version = "2.39";
    expect(() => validateAuthorityProfile(collationVersion)).toThrow(/UNEXPECTED_VALUE/u);
  });

  it("freezes schema absent/present and rejects empty_schema or invalid scope", () => {
    const absent = fixture("golden/catalog-state-schema-absent-v1.json");
    const present = fixture("golden/catalog-state-schema-present-v1.json");
    expect(() => validateCatalogState(absent)).not.toThrow();
    expect(() => validateCatalogState(present)).not.toThrow();
    expect(catalogStateDigest(absent)).toMatch(/^sha256:[0-9a-f]{64}$/u);
    expect(catalogStateDigest(present)).not.toBe(catalogStateDigest(absent));

    const legacy = structuredClone(absent);
    legacy.state = "empty_schema";
    expect(() => validateCatalogState(legacy)).toThrow(/UNEXPECTED_VALUE/u);

    const scope = structuredClone(present);
    const projectionScope = scope.scope as JsonObject;
    projectionScope.scope_kind = "final";
    expect(() => validateCatalogState(scope)).toThrow(/PROJECTION_SCOPE/u);

    const predecessorHead = structuredClone(absent);
    (predecessorHead.scope as JsonObject).schema_head = "000000";
    expect(() => validateCatalogState(predecessorHead)).toThrow(/PROJECTION_SCOPE/u);

    const wrongSchema = structuredClone(absent);
    wrongSchema.schema = "other_schema";
    expect(() => validateCatalogState(wrongSchema)).toThrow(/UNEXPECTED_VALUE/u);

    const closure = structuredClone(present);
    (closure.scope as JsonObject).declared_objects = [{ kind: "schema", name: "cloud_agents" }];
    expect(() => validateCatalogState(closure)).toThrow(/CATALOG_STATE_DECLARED_CLOSURE/u);

    const absentClosure = structuredClone(absent);
    (absentClosure.scope as JsonObject).declared_objects = [
      { kind: "schema", name: "cloud_agents" },
    ];
    expect(() => validateCatalogState(absentClosure)).toThrow(/CATALOG_ABSENT_DECLARED_OBJECTS/u);
  });

  it("validates namespace body, ACL provenance and closed object identities", () => {
    const body = fixture("golden/catalog-projection-body-v1.json");
    expect(() => validateCatalogProjectionBody(body)).not.toThrow();
    const schema = body.schema as JsonObject;
    expect((schema.explicit_acl as JsonObject).catalog_value).toBe("explicit");
    expect((schema.effective_acl as JsonObject[]).some((entry) => entry.grantor)).toBe(true);

    const openIdentity = {
      kind: "relation",
      identity: { schema: "cloud_agents", name: "schema_migrations" },
      oid: 16_384,
    } satisfies JsonObject;
    expect(() => validateObjectIdentity(openIdentity)).toThrow(/UNKNOWN_OR_MISSING_FIELD/u);

    const nestedInternal = {
      kind: "internal",
      semantic_kind: "toast",
      owning_object: {
        kind: "internal",
        semantic_kind: "array",
        owning_object: { kind: "schema", name: "cloud_agents" },
      },
    } satisfies JsonObject;
    expect(() => validateObjectIdentity(nestedInternal)).toThrow(/OBJECT_IDENTITY_DEPTH/u);

    const trigger = {
      kind: "trigger",
      relation: { schema: "cloud_agents", name: "platform_tenants" },
      name: "internal_trigger",
      owning_constraint: { kind: "schema", name: "cloud_agents" },
    } satisfies JsonObject;
    expect(() => validateObjectIdentity(trigger)).toThrow(/TRIGGER_OWNING_CONSTRAINT/u);

    const relation = structuredClone(body);
    relation.relations = [{}];
    expect(() => validateCatalogProjectionBody(relation)).toThrow(
      /A21A_RELATIONS_NOT_IMPLEMENTED/u,
    );
    const functions = structuredClone(body);
    functions.functions = [{}];
    expect(() => validateCatalogProjectionBody(functions)).toThrow(
      /A21A_FUNCTIONS_NOT_IMPLEMENTED/u,
    );

    const dependency = {
      depender: { kind: "schema", name: "cloud_agents" },
      depended_on: { kind: "schema", name: "pg_catalog" },
      dependency_kind: "normal",
    } satisfies JsonObject;
    const duplicateDependency = structuredClone(body);
    duplicateDependency.dependencies = [dependency, structuredClone(dependency)];
    expect(() => validateCatalogProjectionBody(duplicateDependency)).toThrow(
      /DUPLICATE_OR_UNSORTED/u,
    );

    const denied = {
      object: { kind: "schema", name: "unknown_schema" },
      owner: null,
      dependency_kind: null,
      depended_on: null,
      reason_code: "undeclared_object",
    } satisfies JsonObject;
    const duplicateDenied = structuredClone(body);
    duplicateDenied.denied_objects = [denied, structuredClone(denied)];
    expect(() => validateCatalogProjectionBody(duplicateDenied)).toThrow(/DUPLICATE_OR_UNSORTED/u);

    const badOrigin = structuredClone(body);
    const defaultEntry = (
      ((badOrigin.default_acl as JsonObject[])[0]!.acl as JsonObject).entries as JsonObject[]
    )[0]!;
    defaultEntry.origin = "catalog_explicit";
    expect(() => validateCatalogProjectionBody(badOrigin)).toThrow(/ACL_ORIGIN/u);

    const badPrivilege = structuredClone(body);
    const explicitEntry = (
      ((badPrivilege.schema as JsonObject).explicit_acl as JsonObject).entries as JsonObject[]
    )[0]!;
    explicitEntry.privileges = ["SELECT"];
    expect(() => validateCatalogProjectionBody(badPrivilege)).toThrow(/ACL_PRIVILEGE/u);

    const lowercasePrivilege = structuredClone(body);
    const lowercaseEntry = (
      ((lowercasePrivilege.schema as JsonObject).explicit_acl as JsonObject).entries as JsonObject[]
    )[0]!;
    lowercaseEntry.privileges = ["usage"];
    expect(() => validateCatalogProjectionBody(lowercasePrivilege)).toThrow(/ACL_PRIVILEGE/u);
  });

  it("keeps ExpectedStatementTransition closed and digest-ref-only", () => {
    const transition = fixture("golden/expected-statement-transition-v1.json");
    expect(() => validateExpectedStatementTransition(transition)).not.toThrow();
    const before = transition.catalog_before as JsonObject;
    expect(Object.keys(before)).toEqual(["scope", "state_kind", "digest"]);
    expect(before).not.toHaveProperty("projection");

    const unknown = structuredClone(transition);
    unknown.actual_projection = {};
    expect(() => validateExpectedStatementTransition(unknown)).toThrow(/UNKNOWN_OR_MISSING_FIELD/u);
    const badDigest = structuredClone(transition);
    (badDigest.catalog_after as JsonObject).digest = "sha256:0";
    expect(() => validateExpectedStatementTransition(badDigest)).toThrow(/DIGEST_FORMAT/u);
    const openIdentity = structuredClone(transition);
    const delta = (openIdentity.control_plane_delta as JsonObject[])[0]!;
    (delta.object as JsonObject).oid = 42;
    expect(() => validateExpectedStatementTransition(openIdentity)).toThrow(
      /UNKNOWN_OR_MISSING_FIELD/u,
    );
  });

  it("validates canonical signed, numeric and Ryu fixture boundaries", () => {
    const numeric = fixture("golden/numeric-v1.json");
    expect(() => validateNumericFixture(numeric)).not.toThrow();
    expect(canonicalSignedInteger("-9223372036854775808", 64)).toBe("-9223372036854775808");
    expect(() => canonicalSignedInteger("32768", 16)).toThrow(/NUMERIC_RANGE/u);
    expect(canonicalExactNumeric("123.4500")).toBe("123.45");
    expect(canonicalExactNumeric("-0.125")).toBe("-0.125");
    expect(() => canonicalExactNumeric("1e3")).toThrow(/NUMERIC_FORMAT/u);
    expect(canonicalRyuFloat("0.1", "float4")).toBe("0.1");
    expect(canonicalRyuFloat("5e-324", "float8")).toBe("5e-324");
    expect(() => canonicalRyuFloat("NaN", "float8")).toThrow(/FLOAT_FORMAT/u);
  });

  it("validates intermediate and attempt terminal closed shapes and digests", () => {
    const intermediate = fixture("golden/intermediate-state-v1.json");
    const terminal = fixture("golden/attempt-terminal-state-v1.json");
    expect(() => validateIntermediateState(intermediate)).not.toThrow();
    expect(() => validateAttemptTerminalState(terminal)).not.toThrow();

    const controlPlaneUnknown = structuredClone(intermediate);
    (controlPlaneUnknown.control_plane_states as JsonObject).backend_pid = 123;
    expect(() => validateIntermediateState(controlPlaneUnknown)).toThrow(
      /UNKNOWN_OR_MISSING_FIELD/u,
    );
    const intermediateDigest = structuredClone(intermediate);
    intermediateDigest.intermediate_state_digest = `sha256:${"0".repeat(64)}`;
    expect(() => validateIntermediateState(intermediateDigest)).toThrow(/INTERMEDIATE_DIGEST/u);

    const attemptLink = structuredClone(intermediate);
    attemptLink.attempt_index = 2;
    expect(() => validateIntermediateState(attemptLink)).toThrow(/INTERMEDIATE_ATTEMPT_LINK/u);
    const firstAttemptLink = structuredClone(intermediate);
    firstAttemptLink.previous_attempt_terminal_digest = `sha256:${"5".repeat(64)}`;
    expect(() => validateIntermediateState(firstAttemptLink)).toThrow(/INTERMEDIATE_ATTEMPT_LINK/u);

    const statementLink = structuredClone(intermediate);
    statementLink.statement_index = 1;
    expect(() => validateIntermediateState(statementLink)).toThrow(/INTERMEDIATE_STATEMENT_LINK/u);
    const firstStatementLink = structuredClone(intermediate);
    firstStatementLink.previous_intermediate_state_digest = `sha256:${"6".repeat(64)}`;
    expect(() => validateIntermediateState(firstStatementLink)).toThrow(
      /INTERMEDIATE_STATEMENT_LINK/u,
    );

    const overflow = structuredClone(intermediate);
    overflow.statement_index = 4_294_967_296;
    expect(() => validateIntermediateState(overflow)).toThrow(/UINT32_RANGE/u);

    const advisory = structuredClone(intermediate);
    ((advisory.control_plane_states as JsonObject).advisory_lock as JsonObject).domain =
      "wrong-domain";
    expect(() => validateIntermediateState(advisory)).toThrow(/UNEXPECTED_VALUE/u);
    const advisoryKey = structuredClone(intermediate);
    (
      (advisoryKey.control_plane_states as JsonObject).advisory_lock as JsonObject
    ).key_int64_decimal = "1";
    expect(() => validateIntermediateState(advisoryKey)).toThrow(/UNEXPECTED_VALUE/u);

    const badOutcome = structuredClone(terminal);
    badOutcome.outcome = "committed";
    badOutcome.stable_error_code = "SERIALIZATION_FAILURE";
    badOutcome.terminal_digest = terminalDigest(badOutcome);
    expect(() => validateAttemptTerminalState(badOutcome)).toThrow(/ATTEMPT_TERMINAL_COMBINATION/u);

    const terminalLink = structuredClone(terminal);
    terminalLink.attempt_index = 2;
    expect(() => validateAttemptTerminalState(terminalLink)).toThrow(/ATTEMPT_TERMINAL_LINK/u);
    const firstTerminalLink = structuredClone(terminal);
    firstTerminalLink.previous_attempt_terminal_digest = `sha256:${"7".repeat(64)}`;
    expect(() => validateAttemptTerminalState(firstTerminalLink)).toThrow(/ATTEMPT_TERMINAL_LINK/u);

    const ambiguousCommitted = structuredClone(terminal);
    ambiguousCommitted.outcome = "ambiguous_reconciled_committed";
    ambiguousCommitted.stable_error_code = "COMMIT_RESULT_UNKNOWN";
    ambiguousCommitted.reconcile_result = "exact_committed";
    ambiguousCommitted.last_intermediate_state_digest = null;
    ambiguousCommitted.terminal_digest = terminalDigest(ambiguousCommitted);
    expect(() => validateAttemptTerminalState(ambiguousCommitted)).toThrow(
      /ATTEMPT_TERMINAL_COMBINATION/u,
    );
  });

  it("keeps runtime cumulative catalogs explicitly non-executable until A2.1b", () => {
    for (const head of ["000001", "000002"]) {
      const catalog = JSON.parse(
        readFileSync(
          resolve(root, `services/control-plane/migrations/catalog/schema-${head}.json`),
          "utf8",
        ),
      ) as JsonObject;
      expect(catalog.runtime_introspection_status).toBe("NOT_IMPLEMENTED");
      expect(catalog.publication_status).toBe("UNPUBLISHED_BOOTSTRAP_MUTABLE");
      expect(catalog.executable_expected_projection_status).toBe("NOT_IMPLEMENTED_A2_1B_REQUIRED");
      expect(catalog).not.toHaveProperty("expected_projection");
      for (const source of catalog.source_descriptors as JsonObject[]) {
        for (const statement of source.statements as JsonObject[]) {
          expect(statement).not.toHaveProperty("expected_transition");
        }
      }
    }
  });
});

function fixture(path: string): JsonObject {
  return JSON.parse(readFileSync(resolve(fixtureRoot, path), "utf8")) as JsonObject;
}

function terminalDigest(state: JsonObject): string {
  const withoutDigest = structuredClone(state);
  delete withoutDigest.terminal_digest;
  return migrationDigest({
    domain: "cloud-agents-platform-attempt-terminal/v1",
    ...withoutDigest,
  } as MigrationJson);
}
