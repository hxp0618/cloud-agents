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
  validateCanonicalMembershipFixture,
  validateDefaultACLScopeFixture,
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
    const connected = projections.connected_session as JsonObject;
    const roles = connected.roles as JsonObject[];
    expect(roles.map((role) => role.name)).toEqual([
      "cloud_agents_bootstrap_admin",
      "cloud_agents_bootstrap_login_fixture",
      "cloud_agents_database_owner_fixture",
      "cloud_agents_migration_login_fixture",
      "cloud_agents_migration_owner",
      "cloud_agents_runtime",
      "cloud_agents_runtime_login_fixture",
      "fixture_cluster_superuser",
    ]);
    for (const groupName of [
      "cloud_agents_bootstrap_admin",
      "cloud_agents_migration_owner",
      "cloud_agents_runtime",
    ]) {
      const group = roles.find((role) => role.name === groupName)!;
      expect({ login: group.login, inherit: group.inherit, superuser: group.superuser }).toEqual({
        login: false,
        inherit: false,
        superuser: false,
      });
    }
    const direct = connected.direct_memberships as JsonObject[];
    expect(direct.map((entry) => [entry.member, entry.role])).toEqual([
      ["cloud_agents_bootstrap_login_fixture", "cloud_agents_bootstrap_admin"],
      ["cloud_agents_migration_login_fixture", "cloud_agents_migration_owner"],
      ["cloud_agents_runtime_login_fixture", "cloud_agents_runtime"],
    ]);
    const reachability = connected.membership_reachability as JsonObject[];
    expect(reachability).toHaveLength(3);
    for (const endpoint of reachability) {
      const privileges = endpoint.privileges as JsonObject[];
      expect(privileges.map((entry) => entry.privilege_kind)).toEqual(["member", "usage", "set"]);
      expect(privileges.map((entry) => entry.edge_count)).toEqual([3, 3, 3]);
    }
    const migrationPrivileges = reachability[1]!.privileges as JsonObject[];
    expect(migrationPrivileges[0]!.canonical_witness).toEqual([
      "cloud_agents_migration_login_fixture",
      "cloud_agents_migration_owner",
    ]);
    expect(migrationPrivileges[1]!.canonical_witness).toBeNull();
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

  it("rejects mechanical authority invariant drift in the signed binding", () => {
    const binding = fixture("golden/authority-binding-v1.json");
    const legacyUnreachableCount = structuredClone(binding);
    const legacyPrivilege = (
      ((legacyUnreachableCount.expected_projections as JsonObject).connected_session as JsonObject)
        .membership_reachability as JsonObject[]
    )[1]!.privileges as JsonObject[];
    legacyPrivilege[1]!.edge_count = 0;
    expect(() => validateAuthorityBinding(legacyUnreachableCount)).toThrow(
      /REACHABILITY_EDGE_COUNT/u,
    );

    const inheritingGroup = structuredClone(binding);
    const roles = (
      (inheritingGroup.expected_projections as JsonObject).connected_session as JsonObject
    ).roles as JsonObject[];
    roles.find((role) => role.name === "cloud_agents_migration_owner")!.inherit = true;
    expect(() => validateAuthorityBinding(inheritingGroup)).toThrow(/AUTHORITY_GROUP_ROLE/u);

    const extraRole = structuredClone(binding);
    const extraRoles = (
      (extraRole.expected_projections as JsonObject).connected_session as JsonObject
    ).roles as JsonObject[];
    extraRoles.push(syntheticRole("fixture_unbound_role", false, false));
    expect(() => validateAuthorityBinding(extraRole)).toThrow(/AUTHORITY_ROLE_CLOSURE/u);
  });

  it("recomputes canonical equal paths only in a local synthetic graph", () => {
    const synthetic = syntheticMembershipFixture();
    expect(() => validateCanonicalMembershipFixture(synthetic)).not.toThrow();

    const legacyUnreachableCount = structuredClone(synthetic);
    (
      (legacyUnreachableCount.membership_reachability as JsonObject).privileges as JsonObject[]
    )[1]!.edge_count = 0;
    expect(() => validateCanonicalMembershipFixture(legacyUnreachableCount)).toThrow(
      /REACHABILITY_EDGE_COUNT/u,
    );

    const reversed = structuredClone(synthetic);
    (
      (reversed.membership_reachability as JsonObject).privileges as JsonObject[]
    )[0]!.canonical_witness = ["fixture_target", "fixture_membership_a", "fixture_member"];
    expect(() => validateCanonicalMembershipFixture(reversed)).toThrow(/REACHABILITY_WITNESS/u);

    const noncanonical = structuredClone(synthetic);
    (
      (noncanonical.membership_reachability as JsonObject).privileges as JsonObject[]
    )[0]!.canonical_witness = ["fixture_member", "fixture_membership_b", "fixture_target"];
    expect(() => validateCanonicalMembershipFixture(noncanonical)).toThrow(/REACHABILITY_WITNESS/u);

    const duplicateEndpoint = structuredClone(synthetic);
    const direct = duplicateEndpoint.direct_memberships as JsonObject[];
    const duplicate = structuredClone(direct[0]!);
    duplicate.grantor = "fixture_membership_b";
    direct.splice(1, 0, duplicate);
    expect(() => validateCanonicalMembershipFixture(duplicateEndpoint)).toThrow(
      /DIRECT_MEMBERSHIP_DUPLICATE/u,
    );
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

  it("freezes global and schema-scoped DefaultACL closure and ordering", () => {
    const scope = fixture("golden/default-acl-scope-v1.json");
    expect(() => validateDefaultACLScopeFixture(scope)).not.toThrow();
    const rows = scope.rows as JsonObject[];
    expect(rows.filter((row) => row.schema === null).map((row) => row.object_kind)).toEqual([
      "function",
      "schema",
      "sequence",
      "table",
      "type",
    ]);
    expect(
      rows.filter((row) => row.schema === "cloud_agents").map((row) => row.object_kind),
    ).toEqual(["function", "sequence", "table", "type"]);
    expect(rows.every((row) => (row.acl as JsonObject).catalog_value === "explicit")).toBe(true);

    const invalidSchemaKind = structuredClone(scope);
    const invalidSchemaRows = invalidSchemaKind.rows as JsonObject[];
    const schemaRow = invalidSchemaRows.find((row) => row.object_kind === "schema")!;
    schemaRow.schema = "cloud_agents";
    invalidSchemaRows.sort((left, right) =>
      Buffer.compare(Buffer.from(defaultACLKey(left)), Buffer.from(defaultACLKey(right))),
    );
    expect(() => validateDefaultACLScopeFixture(invalidSchemaKind)).toThrow(
      /DEFAULT_ACL_SCHEMA_KIND_SCOPE/u,
    );

    const invalidScope = structuredClone(scope);
    const invalidScopeRows = invalidScope.rows as JsonObject[];
    const scopedType = invalidScopeRows.find(
      (row) => row.schema === "cloud_agents" && row.object_kind === "type",
    )!;
    scopedType.schema = "other_schema";
    invalidScopeRows.sort((left, right) =>
      Buffer.compare(Buffer.from(defaultACLKey(left)), Buffer.from(defaultACLKey(right))),
    );
    expect(() => validateDefaultACLScopeFixture(invalidScope)).toThrow(/DEFAULT_ACL_SCOPE/u);

    const implicitCatalog = structuredClone(scope);
    const firstACL = (implicitCatalog.rows as JsonObject[])[0]!.acl as JsonObject;
    firstACL.catalog_value = "null";
    firstACL.entries = [];
    expect(() => validateDefaultACLScopeFixture(implicitCatalog)).toThrow(
      /DEFAULT_ACL_CATALOG_VALUE/u,
    );

    const ownerOutsideClosure = structuredClone(scope);
    const ownerOutsideRows = ownerOutsideClosure.rows as JsonObject[];
    ownerOutsideRows[0]!.owner = "outside_creator_closure";
    ownerOutsideRows.sort((left, right) =>
      Buffer.compare(Buffer.from(defaultACLKey(left)), Buffer.from(defaultACLKey(right))),
    );
    expect(() => validateDefaultACLScopeFixture(ownerOutsideClosure)).toThrow(
      /DEFAULT_ACL_OWNER_CLOSURE/u,
    );

    const reverseOrder = structuredClone(scope);
    (reverseOrder.rows as JsonObject[]).reverse();
    expect(() => validateDefaultACLScopeFixture(reverseOrder)).toThrow(/DUPLICATE_OR_UNSORTED/u);
  });

  it("publishes exactly 43 projection fault records for the closed negative matrix", () => {
    const faults = fixture("negative/faults-v1.json");
    const cases = faults.cases as JsonObject[];
    expect(cases).toHaveLength(43);
    expect(cases.map((entry) => entry.mutation)).toEqual(
      expect.arrayContaining([
        "unreachable_edge_count_zero",
        "reverse_member_role_witness",
        "select_utf8_later_shortest_path",
        "schema_kind_scoped_to_cloud_agents",
        "schema_outside_closed_scope",
        "legacy_runner_error_code",
      ]),
    );
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
    badOutcome.stable_error_code = "MIGRATION_PROJECTION_CATALOG_QUERY_FAILED";
    badOutcome.failure_evidence = {
      code: "MIGRATION_PROJECTION_CATALOG_QUERY_FAILED",
      projection_kind: "catalog",
      phase: "migration_transaction",
      path: "catalog",
      major: 15,
      retryable: false,
    };
    badOutcome.terminal_digest = terminalDigest(badOutcome);
    expect(() => validateAttemptTerminalState(badOutcome)).toThrow(/ATTEMPT_TERMINAL_COMBINATION/u);

    const allowedStableError = structuredClone(terminal);
    allowedStableError.outcome = "aborted_terminal";
    allowedStableError.stable_error_code = "MIGRATION_LOCK_LOST";
    allowedStableError.failure_evidence = {
      code: "MIGRATION_LOCK_LOST",
      projection_kind: null,
      phase: "migration_transaction",
      path: "transaction",
      major: 15,
      retryable: false,
    };
    allowedStableError.last_intermediate_state_digest = null;
    allowedStableError.reconcile_result = "not_run";
    allowedStableError.terminal_digest = terminalDigest(allowedStableError);
    expect(() => validateAttemptTerminalState(allowedStableError)).not.toThrow();

    const unknownStableError = structuredClone(allowedStableError);
    unknownStableError.stable_error_code = "MIGRATION_PROJECTION_UNKNOWN_FINAL_CODE";
    (unknownStableError.failure_evidence as JsonObject).code =
      "MIGRATION_PROJECTION_UNKNOWN_FINAL_CODE";
    unknownStableError.terminal_digest = terminalDigest(unknownStableError);
    expect(() => validateAttemptTerminalState(unknownStableError)).toThrow(/STABLE_ERROR_CODE/u);

    for (const runOnly of [
      "MIGRATION_PROJECTION_LIMIT_OVERRIDE",
      "MIGRATION_PROJECTION_NOT_IMPLEMENTED",
    ]) {
      const rejected = structuredClone(allowedStableError);
      rejected.stable_error_code = runOnly;
      (rejected.failure_evidence as JsonObject).code = runOnly;
      rejected.terminal_digest = terminalDigest(rejected);
      expect(() => validateAttemptTerminalState(rejected)).toThrow(/STABLE_ERROR_CODE/u);
    }

    const terminalLink = structuredClone(terminal);
    terminalLink.attempt_index = 2;
    expect(() => validateAttemptTerminalState(terminalLink)).toThrow(/ATTEMPT_TERMINAL_LINK/u);
    const firstTerminalLink = structuredClone(terminal);
    firstTerminalLink.previous_attempt_terminal_digest = `sha256:${"7".repeat(64)}`;
    expect(() => validateAttemptTerminalState(firstTerminalLink)).toThrow(/ATTEMPT_TERMINAL_LINK/u);

    const ambiguousCommitted = structuredClone(terminal);
    ambiguousCommitted.outcome = "ambiguous_reconciled_committed";
    ambiguousCommitted.stable_error_code = "MIGRATION_PROJECTION_CATALOG_QUERY_FAILED";
    ambiguousCommitted.failure_evidence = {
      code: "MIGRATION_PROJECTION_CATALOG_QUERY_FAILED",
      projection_kind: "catalog",
      phase: "reconcile",
      path: "catalog",
      major: 15,
      retryable: false,
    };
    ambiguousCommitted.reconcile_result = "exact_committed";
    ambiguousCommitted.last_intermediate_state_digest = null;
    ambiguousCommitted.terminal_digest = terminalDigest(ambiguousCommitted);
    expect(() => validateAttemptTerminalState(ambiguousCommitted)).toThrow(
      /ATTEMPT_TERMINAL_COMBINATION/u,
    );
  });

  it("keeps runtime cumulative catalogs explicitly non-executable until A2.1b", () => {
    for (const head of ["000001", "000002", "000003"]) {
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

function defaultACLKey(row: JsonObject): string {
  return `${String(row.owner)}\0${row.schema === null ? "0" : `1${String(row.schema)}`}\0${String(row.object_kind)}`;
}

function syntheticRole(name: string, login: boolean, inherit: boolean): JsonObject {
  return {
    name,
    login,
    inherit,
    superuser: false,
    create_role: false,
    create_db: false,
    replication: false,
    bypass_rls: false,
    connection_limit_int32_decimal: "-1",
    valid_until: null,
    config: [],
  };
}

function syntheticMembershipFixture(): JsonObject {
  const member = "fixture_member";
  const target = "fixture_target";
  const witness = [member, "fixture_membership_a", target];
  const privilege = (kind: string): JsonObject => ({
    privilege_kind: kind,
    reachable: true,
    min_depth: 2,
    canonical_witness: witness,
    edge_count: 4,
  });
  const direct = (
    role: string,
    directMember: string,
    inheritOption = true,
    setOption = true,
  ): JsonObject => ({
    role,
    member: directMember,
    grantor: "fixture_grantor",
    admin_option: false,
    inherit_option: inheritOption,
    set_option: setOption,
  });
  return {
    roles: [
      syntheticRole("fixture_grantor", false, false),
      syntheticRole(member, true, true),
      syntheticRole("fixture_membership_a", false, true),
      syntheticRole("fixture_membership_b", false, true),
      syntheticRole(target, false, false),
    ],
    direct_memberships: [
      direct("fixture_membership_a", member),
      direct("fixture_membership_b", member),
      direct(target, "fixture_membership_a"),
      direct(target, "fixture_membership_b"),
    ],
    membership_reachability: {
      role: target,
      member,
      privileges: [privilege("member"), privilege("usage"), privilege("set")],
    },
  };
}
