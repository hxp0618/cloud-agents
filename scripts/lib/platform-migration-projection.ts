import { createHash } from "node:crypto";

import {
  canonicalizeMigrationJson,
  type MigrationJson,
  migrationDigest,
  MigrationValidationError,
  parseSignedInt64Decimal,
  parseStrictMigrationJson,
} from "./platform-migration-json";

export type JsonObject = { [key: string]: MigrationJson };

export const DIGEST_PATTERN = /^sha256:[0-9a-f]{64}$/u;
const IDENTIFIER = /^[a-z][a-z0-9_]{0,62}$/u;
const PHASES = ["connected_session", "migration_role", "migration_transaction"] as const;
const PRIVILEGE_KINDS = ["member", "usage", "set"] as const;
const ACL_ORIGINS = [
  "catalog_explicit",
  "owner_implicit",
  "public_default",
  "default_acl_catalog",
] as const;
const SCOPE_KINDS = ["predecessor", "statement_prefix", "final"] as const;
const OUTCOMES = [
  "committed",
  "aborted_retryable",
  "aborted_terminal",
  "ambiguous_reconciled_committed",
  "ambiguous_reconciled_pending",
  "ambiguous_divergent",
] as const;
const RECONCILE_RESULTS = ["not_run", "exact_committed", "exact_pending", "divergent"] as const;
const CHANGE_KINDS = ["create", "alter", "grant", "revoke"] as const;
const STATE_KINDS = ["schema_absent", "schema_present"] as const;
const UINT32_MAX = 4_294_967_295;
const ADVISORY_DOMAIN = "cloud-agents-platform:migrations:v1";
const ADVISORY_KEY = "-1047838957622507638";
const ACL_PRIVILEGES = [
  "CONNECT",
  "CREATE",
  "DELETE",
  "EXECUTE",
  "INSERT",
  "REFERENCES",
  "SELECT",
  "TEMPORARY",
  "TRIGGER",
  "TRUNCATE",
  "UPDATE",
  "USAGE",
] as const;
type ACLSurface =
  | "generic"
  | "database"
  | "schema_explicit"
  | "schema_effective"
  | "default_function"
  | "default_sequence"
  | "default_table";

export function decodeAuthorityProfile(bytes: Uint8Array): JsonObject {
  const profile = object(parseStrictMigrationJson(bytes), "authority profile");
  validateAuthorityProfile(profile);
  return profile;
}

export function validateAuthorityProfile(profile: JsonObject): void {
  keys(profile, [
    "format_version",
    "contract_kind",
    "publication_status",
    "runtime_introspection_status",
    "database",
    "group_roles",
    "required_projection_fields",
    "required_binding_fields",
  ]);
  literal(
    profile.format_version,
    ["cloud-agents-platform-authority-contract/v1"],
    "authority format",
  );
  literal(profile.contract_kind, ["database_role_authority"], "authority kind");
  literal(profile.publication_status, ["UNPUBLISHED_BOOTSTRAP_MUTABLE"], "authority publication");
  literal(profile.runtime_introspection_status, ["NOT_IMPLEMENTED"], "authority runtime status");
  const database = object(profile.database, "authority database");
  keys(database, [
    "encoding",
    "locale_provider",
    "datcollate",
    "datctype",
    "icu_locale",
    "icu_rules",
    "collation_version",
  ]);
  literal(database.encoding, ["UTF8"], "database encoding");
  literal(database.locale_provider, ["libc"], "locale provider");
  literal(database.datcollate, ["C"], "datcollate");
  literal(database.datctype, ["C"], "datctype");
  literal(database.icu_locale, [null], "icu locale");
  literal(database.icu_rules, [null], "icu rules");
  literal(database.collation_version, [null], "collation version");
  const groups = stringArray(profile.group_roles, "group roles");
  exactArray(
    groups,
    ["cloud_agents_migration_owner", "cloud_agents_runtime", "cloud_agents_bootstrap_admin"],
    "group roles",
  );
  exactArray(
    stringArray(profile.required_projection_fields, "required projection fields"),
    [
      "phase",
      "session_user",
      "current_user",
      "database_name",
      "database_owner",
      "database_encoding",
      "locale_provider",
      "datcollate",
      "datctype",
      "icu_locale",
      "icu_rules",
      "collation_version",
      "database_acl",
      "roles",
      "direct_memberships",
      "membership_reachability",
      "database_role_settings",
      "effective_create",
      "effective_temporary",
    ],
    "required projection fields",
  );
  exactArray(
    stringArray(profile.required_binding_fields, "required binding fields"),
    [
      "authority_profile_digest",
      "deployment_id",
      "issued_at",
      "expires_at",
      "security_epoch",
      "expected_projections",
    ],
    "required binding fields",
  );
}

export function decodeAuthorityBinding(bytes: Uint8Array): JsonObject {
  const binding = object(parseStrictMigrationJson(bytes), "authority binding");
  validateAuthorityBinding(binding);
  return binding;
}

export function validateAuthorityBinding(binding: JsonObject): void {
  keys(binding, [
    "format_version",
    "authority_profile_digest",
    "deployment_id",
    "issued_at",
    "expires_at",
    "security_epoch",
    "expected_projections",
  ]);
  literal(binding.format_version, ["cloud-agents-platform-authority-binding/v1"], "binding format");
  digest(binding.authority_profile_digest, "authority profile digest");
  normalizedIdentifier(binding.deployment_id, "deployment id");
  rfc3339(binding.issued_at, "issued at");
  rfc3339(binding.expires_at, "expires at");
  uint32(binding.security_epoch, "security epoch", 1);
  const projections = object(binding.expected_projections, "expected projections");
  keys(projections, PHASES);
  for (const phase of PHASES) {
    const projection = object(projections[phase], `${phase} projection`);
    validateAuthorityProjection(projection, phase);
  }
}

export function validateAuthorityProjection(
  projection: JsonObject,
  expectedPhase?: (typeof PHASES)[number],
): void {
  keys(projection, [
    "phase",
    "session_user",
    "current_user",
    "database_name",
    "database_owner",
    "database_encoding",
    "locale_provider",
    "datcollate",
    "datctype",
    "icu_locale",
    "icu_rules",
    "collation_version",
    "database_acl",
    "roles",
    "direct_memberships",
    "membership_reachability",
    "database_role_settings",
    "effective_create",
    "effective_temporary",
  ]);
  const phase = literal(projection.phase, PHASES, "authority phase");
  if (expectedPhase !== undefined && phase !== expectedPhase) {
    fail("AUTHORITY_PHASE", `${phase} != ${expectedPhase}`);
  }
  for (const field of [
    "session_user",
    "current_user",
    "database_name",
    "database_owner",
    "database_encoding",
    "locale_provider",
    "datcollate",
    "datctype",
  ] as const) {
    nonemptyString(projection[field], field);
  }
  literal(projection.icu_locale, [null], "icu locale");
  literal(projection.icu_rules, [null], "icu rules");
  literal(projection.collation_version, [null], "collation version");
  validateACLSet(object(projection.database_acl, "database acl"), "database");
  const roles = array(projection.roles, "roles").map((role) => object(role, "role"));
  uniqueSorted(roles, (role) => nonemptyString(role.name, "role name"), "roles");
  roles.forEach(validateRole);
  const direct = array(projection.direct_memberships, "direct memberships").map((entry) =>
    object(entry, "direct membership"),
  );
  uniqueSorted(
    direct,
    (entry) =>
      [entry.role, entry.member, entry.grantor]
        .map((value) => nonemptyString(value, "direct membership identity"))
        .join("\0"),
    "direct memberships",
  );
  direct.forEach(validateDirectMembership);
  const reachability = array(projection.membership_reachability, "reachability").map((entry) =>
    object(entry, "reachability"),
  );
  uniqueSorted(
    reachability,
    (entry) =>
      `${nonemptyString(entry.role, "reachability role")}\0${nonemptyString(entry.member, "reachability member")}`,
    "reachability",
  );
  reachability.forEach(validateReachability);
  const settings = array(projection.database_role_settings, "database role settings").map((entry) =>
    object(entry, "database role setting"),
  );
  uniqueSorted(
    settings,
    (entry) =>
      `${nonemptyString(entry.database, "setting database")}\0${nonemptyString(entry.role, "setting role")}`,
    "database role settings",
  );
  settings.forEach((setting) => {
    keys(setting, ["database", "role", "settings"]);
    nonemptyString(setting.database, "setting database");
    nonemptyString(setting.role, "setting role");
    sortedStrings(setting.settings, "settings");
  });
  validateBooleanMap(object(projection.effective_create, "effective create"));
  validateBooleanMap(object(projection.effective_temporary, "effective temporary"));
}

function validateRole(role: JsonObject): void {
  keys(role, [
    "name",
    "login",
    "inherit",
    "superuser",
    "create_role",
    "create_db",
    "replication",
    "bypass_rls",
    "connection_limit_int32_decimal",
    "valid_until",
    "config",
  ]);
  nonemptyString(role.name, "role name");
  for (const field of [
    "login",
    "inherit",
    "superuser",
    "create_role",
    "create_db",
    "replication",
    "bypass_rls",
  ] as const) {
    boolean(role[field], `role ${field}`);
  }
  const connectionLimit = nonemptyString(role.connection_limit_int32_decimal, "connection limit");
  const parsed = parseSignedInt64Decimal(connectionLimit);
  if (parsed < -1n || parsed > 2_147_483_647n) fail("NUMERIC_RANGE", "connection limit");
  nullableString(role.valid_until, "valid until");
  const config = sortedStrings(role.config, "role config");
  for (const setting of config) {
    const name = setting.split("=", 1)[0]!;
    if (
      ![
        "client_encoding",
        "idle_in_transaction_session_timeout",
        "lock_timeout",
        "search_path",
        "statement_timeout",
      ].includes(name)
    ) {
      fail("ROLE_CONFIG_UNSAFE", name);
    }
  }
  if (config.length !== 0) fail("ROLE_CONFIG_INITIAL_NONEMPTY", role.name as string);
}

function validateDirectMembership(entry: JsonObject): void {
  keys(entry, ["role", "member", "grantor", "admin_option", "inherit_option", "set_option"]);
  nonemptyString(entry.role, "direct role");
  nonemptyString(entry.member, "direct member");
  nonemptyString(entry.grantor, "direct grantor");
  boolean(entry.admin_option, "admin option");
  boolean(entry.inherit_option, "inherit option");
  boolean(entry.set_option, "set option");
}

function validateReachability(entry: JsonObject): void {
  keys(entry, ["role", "member", "privileges"]);
  nonemptyString(entry.role, "reachability role");
  nonemptyString(entry.member, "reachability member");
  const privileges = array(entry.privileges, "reachability privileges").map((privilege) =>
    object(privilege, "reachability privilege"),
  );
  if (privileges.length !== 3) fail("REACHABILITY_PRIVILEGES", "expected MEMBER/USAGE/SET");
  const kinds = privileges.map((privilege) =>
    literal(privilege.privilege_kind, PRIVILEGE_KINDS, "privilege kind"),
  );
  exactArray(kinds, PRIVILEGE_KINDS, "reachability privilege order");
  privileges.forEach((privilege) => {
    keys(privilege, [
      "privilege_kind",
      "reachable",
      "min_depth",
      "canonical_witness",
      "edge_count",
    ]);
    const reachable = boolean(privilege.reachable, "reachable");
    const depth = nullableSafeUint(privilege.min_depth, "min depth");
    const witness = privilege.canonical_witness;
    if (witness !== null) sortedStrings(witness, "canonical witness", false);
    const edgeCount = uint32(privilege.edge_count, "edge count");
    if (
      reachable !== (depth !== null && witness !== null) ||
      (reachable && (depth === 0 || edgeCount === 0)) ||
      (!reachable && edgeCount !== 0)
    ) {
      fail("REACHABILITY_SHAPE", String(privilege.privilege_kind));
    }
  });
}

export function validateACLSet(acl: JsonObject, surface: ACLSurface = "generic"): void {
  keys(acl, ["catalog_value", "entries"]);
  const catalogValue = literal(acl.catalog_value, ["null", "explicit"] as const, "acl value");
  const entries = array(acl.entries, "acl entries").map((entry) => object(entry, "acl entry"));
  if (catalogValue === "null" && entries.length !== 0) {
    fail("ACL_NULL_ENTRIES", "NULL catalog ACL must have no entries");
  }
  uniqueSorted(
    entries,
    (entry) =>
      `${nonemptyString(entry.grantor, "acl grantor")}\0${nonemptyString(entry.grantee, "acl grantee")}`,
    "acl entries",
  );
  entries.forEach((entry) => validateACLEntry(entry, surface));
}

function validateACLEntry(entry: JsonObject, surface: ACLSurface): void {
  keys(entry, ["grantor", "grantee", "privileges", "grantable", "origin"]);
  nonemptyString(entry.grantor, "acl grantor");
  nonemptyString(entry.grantee, "acl grantee");
  const privileges = sortedStrings(entry.privileges, "acl privileges");
  const grantable = sortedStrings(entry.grantable, "acl grantable");
  const allowed = aclPrivilegesForSurface(surface);
  if (
    privileges.some(
      (privilege) =>
        !/^[A-Z]+$/u.test(privilege) ||
        !ACL_PRIVILEGES.includes(privilege as (typeof ACL_PRIVILEGES)[number]) ||
        !allowed.has(privilege),
    )
  ) {
    fail("ACL_PRIVILEGE", `${surface}:${privileges.join(",")}`);
  }
  if (grantable.some((privilege) => !privileges.includes(privilege))) {
    fail("ACL_GRANTABLE", "grantable privilege missing from privileges");
  }
  const origin = literal(entry.origin, ACL_ORIGINS, "acl origin");
  const allowedOrigins = aclOriginsForSurface(surface);
  if (!allowedOrigins.has(origin)) fail("ACL_ORIGIN", `${surface}:${origin}`);
}

export function validateObjectIdentity(identity: JsonObject, depth = 0): void {
  if (depth > 1) fail("OBJECT_IDENTITY_DEPTH", String(depth));
  const kind = literal(
    identity.kind,
    [
      "schema",
      "relation",
      "column",
      "index",
      "policy",
      "type",
      "extension",
      "collation",
      "opclass",
      "function",
      "operator",
      "cast",
      "constraint",
      "trigger",
      "internal",
    ] as const,
    "object identity kind",
  );
  switch (kind) {
    case "schema":
      keys(identity, ["kind", "name"]);
      nonemptyString(identity.name, "schema identity");
      return;
    case "relation":
    case "type":
    case "collation":
      keys(identity, ["kind", "identity"]);
      validateTypeIdentity(object(identity.identity, `${kind} identity`));
      return;
    case "column":
    case "constraint":
    case "policy":
      keys(identity, ["kind", "relation", "name"]);
      validateTypeIdentity(object(identity.relation, `${kind} relation`));
      nonemptyString(identity.name, `${kind} name`);
      return;
    case "index":
      keys(identity, ["kind", "identity", "relation"]);
      validateTypeIdentity(object(identity.identity, "index identity"));
      validateTypeIdentity(object(identity.relation, "index relation"));
      return;
    case "extension":
      keys(identity, ["kind", "name"]);
      nonemptyString(identity.name, "extension name");
      return;
    case "opclass":
      keys(identity, ["kind", "identity", "access_method"]);
      validateTypeIdentity(object(identity.identity, "opclass identity"));
      nonemptyString(identity.access_method, "opclass access method");
      return;
    case "function":
    case "operator":
      keys(identity, ["kind", "identity"]);
      validateSQLIdentity(object(identity.identity, `${kind} identity`));
      return;
    case "cast":
      keys(identity, ["kind", "source_type", "target_type"]);
      validateTypeIdentity(object(identity.source_type, "cast source"));
      validateTypeIdentity(object(identity.target_type, "cast target"));
      return;
    case "trigger":
      keys(identity, ["kind", "relation", "name", "owning_constraint"]);
      validateTypeIdentity(object(identity.relation, "trigger relation"));
      nonemptyString(identity.name, "trigger name");
      if (identity.owning_constraint !== null) {
        const constraint = object(identity.owning_constraint, "trigger constraint");
        if (constraint.kind !== "constraint") {
          fail("TRIGGER_OWNING_CONSTRAINT", String(constraint.kind));
        }
        validateObjectIdentity(constraint, depth + 1);
      }
      return;
    case "internal": {
      keys(identity, ["kind", "semantic_kind", "owning_object"]);
      nonemptyString(identity.semantic_kind, "internal semantic kind");
      const owner = object(identity.owning_object, "internal owner");
      if (owner.kind === "internal") fail("OBJECT_IDENTITY_DEPTH", "nested internal identity");
      validateObjectIdentity(owner, depth + 1);
    }
  }
}

function validateTypeIdentity(identity: JsonObject): void {
  keys(identity, ["schema", "name"]);
  nonemptyString(identity.schema, "identity schema");
  nonemptyString(identity.name, "identity name");
}

function validateSQLIdentity(identity: JsonObject): void {
  keys(identity, ["schema", "name", "arguments"]);
  nonemptyString(identity.schema, "sql identity schema");
  nonemptyString(identity.name, "sql identity name");
  array(identity.arguments, "sql identity arguments")
    .map((argument) => object(argument, "sql identity argument"))
    .forEach(validateTypeIdentity);
}

export function validateProjectionScope(scope: JsonObject): void {
  keys(scope, [
    "scope_kind",
    "schema_head",
    "migration_id",
    "through_statement_index",
    "declared_objects",
  ]);
  const kind = literal(scope.scope_kind, SCOPE_KINDS, "scope kind");
  const schemaHead = nullableMigrationId(scope.schema_head, "scope schema head");
  const migrationId = nullableMigrationId(scope.migration_id, "scope migration id");
  const statementIndex = nullableSafeUint(scope.through_statement_index, "scope statement index");
  const declared = array(scope.declared_objects, "scope declared objects").map((identity) =>
    object(identity, "scope object identity"),
  );
  declared.forEach((identity) => validateObjectIdentity(identity));
  uniqueSorted(declared, objectIdentityKey, "scope declared objects");
  if (
    (kind === "predecessor" &&
      (schemaHead !== null || migrationId === null || statementIndex !== null)) ||
    (kind === "statement_prefix" &&
      (schemaHead !== null || migrationId === null || statementIndex === null)) ||
    (kind === "final" && (schemaHead === null || migrationId !== null || statementIndex !== null))
  ) {
    fail("PROJECTION_SCOPE", kind);
  }
}

export function decodeCatalogState(bytes: Uint8Array): JsonObject {
  const state = object(parseStrictMigrationJson(bytes), "catalog state");
  validateCatalogState(state);
  return state;
}

export function validateCatalogState(state: JsonObject): void {
  const kind = literal(state.state, STATE_KINDS, "catalog state kind");
  if (kind === "schema_absent") {
    keys(state, ["state", "scope", "schema"]);
    const scope = object(state.scope, "absent scope");
    validateProjectionScope(scope);
    literal(state.schema, ["cloud_agents"], "absent schema");
    if (array(scope.declared_objects, "absent declared objects").length !== 0) {
      fail("CATALOG_ABSENT_DECLARED_OBJECTS", "schema absent closure must be empty");
    }
  } else {
    keys(state, ["state", "scope", "body"]);
    const scope = object(state.scope, "present scope");
    const body = object(state.body, "catalog body");
    validateProjectionScope(scope);
    validateCatalogProjectionBody(body);
    literal(object(body.schema, "catalog schema").name, ["cloud_agents"], "present schema");
    const scoped = array(scope.declared_objects, "scope declared objects").map((identity) =>
      object(identity, "scope declared object"),
    );
    const projected = array(body.declared_objects, "body declared objects").map((identity) =>
      object(identity, "body declared object"),
    );
    if (scoped.map(objectIdentityKey).join("\0") !== projected.map(objectIdentityKey).join("\0")) {
      fail("CATALOG_STATE_DECLARED_CLOSURE", "scope/body mismatch");
    }
  }
}

export function catalogStateDigest(state: JsonObject): string {
  validateCatalogState(state);
  return migrationDigest({
    domain: "cloud-agents-platform-catalog-state/v1",
    ...state,
  });
}

export function validateCatalogProjection(projection: JsonObject): void {
  keys(projection, ["schema_head", "body"]);
  migrationId(projection.schema_head, "catalog schema head");
  validateCatalogProjectionBody(object(projection.body, "catalog projection body"));
}

export function validateCatalogProjectionBody(body: JsonObject): void {
  keys(body, [
    "schema",
    "default_acl",
    "relations",
    "functions",
    "dependencies",
    "object_count",
    "declared_objects",
    "denied_objects",
  ]);
  const schema = object(body.schema, "schema projection");
  keys(schema, ["name", "owner", "explicit_acl", "effective_acl", "comment", "security_labels"]);
  nonemptyString(schema.name, "schema name");
  nonemptyString(schema.owner, "schema owner");
  validateACLSet(object(schema.explicit_acl, "schema explicit acl"), "schema_explicit");
  const effectiveACL = array(schema.effective_acl, "schema effective acl").map((entry) =>
    object(entry, "effective acl entry"),
  );
  uniqueSorted(
    effectiveACL,
    (entry) =>
      `${nonemptyString(entry.grantor, "acl grantor")}\0${nonemptyString(entry.grantee, "acl grantee")}`,
    "schema effective acl",
  );
  effectiveACL.forEach((entry) => validateACLEntry(entry, "schema_effective"));
  nullableString(schema.comment, "schema comment");
  const labels = array(schema.security_labels, "security labels").map((label) =>
    object(label, "security label"),
  );
  uniqueSorted(labels, (label) => nonemptyString(label.provider, "label provider"), "labels");
  labels.forEach((label) => {
    keys(label, ["provider", "label"]);
    nonemptyString(label.provider, "label provider");
    nonemptyString(label.label, "label");
  });
  const defaultACL = array(body.default_acl, "default acl").map((entry) =>
    object(entry, "default acl projection"),
  );
  uniqueSorted(
    defaultACL,
    (entry) =>
      `${nonemptyString(entry.owner, "default acl owner")}\0${entry.schema === null ? "" : nonemptyString(entry.schema, "default acl schema")}\0${nonemptyString(entry.object_kind, "default acl kind")}`,
    "default acl",
  );
  defaultACL.forEach((entry) => {
    keys(entry, ["owner", "schema", "object_kind", "acl"]);
    nonemptyString(entry.owner, "default acl owner");
    nullableString(entry.schema, "default acl schema");
    nonemptyString(entry.object_kind, "default acl kind");
    const objectKind = literal(
      entry.object_kind,
      ["function", "sequence", "table"] as const,
      "default acl kind",
    );
    validateACLSet(object(entry.acl, "default acl entries"), `default_${objectKind}`);
  });
  if (array(body.relations, "relations").length !== 0) {
    fail("A21A_RELATIONS_NOT_IMPLEMENTED", "relations must be empty");
  }
  if (array(body.functions, "functions").length !== 0) {
    fail("A21A_FUNCTIONS_NOT_IMPLEMENTED", "functions must be empty");
  }
  const dependencies = array(body.dependencies, "dependencies").map((entry) =>
    object(entry, "dependency"),
  );
  dependencies.forEach((entry) => {
    keys(entry, ["depender", "depended_on", "dependency_kind"]);
    validateObjectIdentity(object(entry.depender, "dependency depender"));
    validateObjectIdentity(object(entry.depended_on, "dependency target"));
    nonemptyString(entry.dependency_kind, "dependency kind");
  });
  uniqueSorted(
    dependencies,
    (entry) =>
      `${objectIdentityKey(object(entry.depender, "dependency depender"))}\0${objectIdentityKey(object(entry.depended_on, "dependency target"))}\0${String(entry.dependency_kind)}`,
    "dependencies",
  );
  const objectCount = uint32(body.object_count, "object count");
  const declared = array(body.declared_objects, "declared objects").map((identity) =>
    object(identity, "declared object"),
  );
  declared.forEach((identity) => validateObjectIdentity(identity));
  uniqueSorted(declared, objectIdentityKey, "declared objects");
  if (objectCount !== declared.length) {
    fail("CATALOG_OBJECT_COUNT", `${objectCount} != ${declared.length}`);
  }
  const denied = array(body.denied_objects, "denied objects").map((entry) =>
    object(entry, "denied object"),
  );
  denied.forEach((entry) => {
    keys(entry, ["object", "owner", "dependency_kind", "depended_on", "reason_code"]);
    validateObjectIdentity(object(entry.object, "denied identity"));
    nullableString(entry.owner, "denied owner");
    nullableString(entry.dependency_kind, "denied dependency kind");
    if (entry.depended_on !== null) {
      validateObjectIdentity(object(entry.depended_on, "denied dependency target"));
    }
    literal(
      entry.reason_code,
      [
        "undeclared_object",
        "unsupported_object_kind",
        "unbound_internal_object",
        "dependency_outside_closure",
      ] as const,
      "denied reason",
    );
  });
  uniqueSorted(
    denied,
    (entry) => objectIdentityKey(object(entry.object, "denied identity")),
    "denied objects",
  );
}

export function decodeExpectedStatementTransition(bytes: Uint8Array): JsonObject {
  const transition = object(parseStrictMigrationJson(bytes), "statement transition");
  validateExpectedStatementTransition(transition);
  return transition;
}

export function validateExpectedStatementTransition(transition: JsonObject): void {
  keys(transition, [
    "profile",
    "catalog_before",
    "catalog_after",
    "authority_relation",
    "control_plane_delta",
  ]);
  literal(
    transition.profile,
    ["cloud-agents-platform-statement-transition/v1"],
    "transition profile",
  );
  validateCatalogStateDigestRef(object(transition.catalog_before, "catalog before"));
  validateCatalogStateDigestRef(object(transition.catalog_after, "catalog after"));
  literal(
    transition.authority_relation,
    ["unchanged_relative_to_verified_binding"],
    "authority relation",
  );
  const delta = array(transition.control_plane_delta, "control plane delta").map((entry) =>
    object(entry, "object transition"),
  );
  uniqueSorted(
    delta,
    (entry) =>
      `${objectIdentityKey(object(entry.object, "transition object"))}\0${String(entry.change_kind)}\0${entry.grantee === null ? "" : String(entry.grantee)}`,
    "control plane delta",
  );
  delta.forEach((entry) => {
    keys(entry, ["change_kind", "object", "grantee"]);
    literal(entry.change_kind, CHANGE_KINDS, "change kind");
    validateObjectIdentity(object(entry.object, "transition object"));
    nullableString(entry.grantee, "transition grantee");
  });
}

function validateCatalogStateDigestRef(ref: JsonObject): void {
  keys(ref, ["scope", "state_kind", "digest"]);
  validateProjectionScope(object(ref.scope, "catalog digest scope"));
  literal(ref.state_kind, STATE_KINDS, "catalog digest state kind");
  digest(ref.digest, "catalog state digest");
}

export function validateIntermediateState(state: JsonObject): void {
  keys(state, [
    "schema_bundle_digest",
    "catalog_contract_digest",
    "authority_profile_digest",
    "authority_binding_digest",
    "migration_id",
    "attempt_index",
    "statement_index",
    "statement_sha256",
    "previous_attempt_terminal_digest",
    "previous_intermediate_state_digest",
    "control_plane_states",
    "authority_before_digest",
    "authority_after_digest",
    "catalog_before_digest",
    "catalog_after_digest",
    "intermediate_state_digest",
  ]);
  for (const field of [
    "schema_bundle_digest",
    "catalog_contract_digest",
    "authority_profile_digest",
    "authority_binding_digest",
    "statement_sha256",
    "authority_before_digest",
    "authority_after_digest",
    "catalog_before_digest",
    "catalog_after_digest",
  ] as const) {
    digest(state[field], field);
  }
  const previousAttempt = nullableDigest(
    state.previous_attempt_terminal_digest,
    "previous attempt terminal digest",
  );
  const previousIntermediate = nullableDigest(
    state.previous_intermediate_state_digest,
    "previous intermediate digest",
  );
  migrationId(state.migration_id, "intermediate migration id");
  const attempt = uint32(state.attempt_index, "attempt index", 1);
  const statement = uint32(state.statement_index, "statement index");
  if ((attempt === 1) !== (previousAttempt === null)) {
    fail("INTERMEDIATE_ATTEMPT_LINK", `attempt ${attempt}`);
  }
  if ((statement === 0) !== (previousIntermediate === null)) {
    fail("INTERMEDIATE_STATEMENT_LINK", `statement ${statement}`);
  }
  validateControlPlaneStates(object(state.control_plane_states, "control plane states"));
  const claimed = digest(state.intermediate_state_digest, "intermediate state digest");
  const withoutDigest = { ...state };
  delete withoutDigest.intermediate_state_digest;
  const expected = migrationDigest({
    domain: "cloud-agents-platform-intermediate-state/v1",
    ...withoutDigest,
  });
  if (claimed !== expected) fail("INTERMEDIATE_DIGEST", `${claimed} != ${expected}`);
}

export function validateControlPlaneStates(state: JsonObject): void {
  keys(state, [
    "tx_status",
    "session_user",
    "current_user",
    "migration_role",
    "advisory_lock",
    "verified_authority_decision_digest",
    "schema_owner",
    "schema_explicit_acl_digest",
    "schema_effective_acl_digest",
    "default_acl_digest",
    "expected_transition_digest",
  ]);
  literal(state.tx_status, ["T"], "tx status");
  nonemptyString(state.session_user, "control plane session user");
  literal(state.current_user, ["cloud_agents_migration_owner"], "control plane current user");
  literal(state.migration_role, ["cloud_agents_migration_owner"], "migration role");
  const lock = object(state.advisory_lock, "advisory lock");
  keys(lock, ["domain", "key_int64_decimal", "held"]);
  literal(lock.domain, [ADVISORY_DOMAIN], "advisory lock domain");
  literal(lock.key_int64_decimal, [ADVISORY_KEY], "advisory lock key");
  if (!boolean(lock.held, "advisory lock held")) fail("CONTROL_PLANE_LOCK", "not held");
  for (const field of [
    "verified_authority_decision_digest",
    "schema_explicit_acl_digest",
    "schema_effective_acl_digest",
    "default_acl_digest",
    "expected_transition_digest",
  ] as const) {
    digest(state[field], field);
  }
  nonemptyString(state.schema_owner, "schema owner");
}

export function intermediateStateDigest(stateWithoutDigest: JsonObject): string {
  const state: JsonObject = {
    ...stateWithoutDigest,
    intermediate_state_digest: migrationDigest({
      domain: "cloud-agents-platform-intermediate-state/v1",
      ...stateWithoutDigest,
    }),
  };
  validateIntermediateState(state);
  return String(state.intermediate_state_digest);
}

export function validateAttemptTerminalState(state: JsonObject, maxAttempts = 3): void {
  keys(state, [
    "schema_bundle_digest",
    "catalog_contract_digest",
    "authority_profile_digest",
    "authority_binding_digest",
    "migration_id",
    "attempt_index",
    "previous_attempt_terminal_digest",
    "last_intermediate_state_digest",
    "outcome",
    "stable_error_code",
    "reconcile_result",
    "terminal_digest",
  ]);
  for (const field of [
    "schema_bundle_digest",
    "catalog_contract_digest",
    "authority_profile_digest",
    "authority_binding_digest",
  ] as const) {
    digest(state[field], field);
  }
  const previousAttempt = nullableDigest(
    state.previous_attempt_terminal_digest,
    "previous attempt digest",
  );
  const lastIntermediate = nullableDigest(
    state.last_intermediate_state_digest,
    "last intermediate digest",
  );
  migrationId(state.migration_id, "terminal migration id");
  const attempt = uint32(state.attempt_index, "terminal attempt index", 1);
  if ((attempt === 1) !== (previousAttempt === null)) {
    fail("ATTEMPT_TERMINAL_LINK", `attempt ${attempt}`);
  }
  const outcome = literal(state.outcome, OUTCOMES, "terminal outcome");
  const error = nullableString(state.stable_error_code, "stable error");
  const reconcile = literal(state.reconcile_result, RECONCILE_RESULTS, "reconcile result");
  const valid =
    (outcome === "committed" && error === null && reconcile === "not_run") ||
    ((outcome === "aborted_retryable" || outcome === "aborted_terminal") &&
      error !== null &&
      reconcile === "not_run") ||
    (outcome === "ambiguous_reconciled_committed" &&
      error !== null &&
      reconcile === "exact_committed") ||
    (outcome === "ambiguous_reconciled_pending" &&
      error !== null &&
      reconcile === "exact_pending") ||
    (outcome === "ambiguous_divergent" && error !== null && reconcile === "divergent");
  if (!valid || (outcome === "aborted_retryable" && attempt >= maxAttempts)) {
    fail("ATTEMPT_TERMINAL_COMBINATION", `${outcome}/${reconcile}/${String(error)}`);
  }
  if (
    (outcome === "committed" || outcome === "ambiguous_reconciled_committed") &&
    lastIntermediate === null
  ) {
    fail("ATTEMPT_TERMINAL_COMBINATION", "committed attempt missing intermediate digest");
  }
  const claimed = digest(state.terminal_digest, "terminal digest");
  const withoutDigest = { ...state };
  delete withoutDigest.terminal_digest;
  const expected = migrationDigest({
    domain: "cloud-agents-platform-attempt-terminal/v1",
    ...withoutDigest,
  });
  if (claimed !== expected) fail("ATTEMPT_TERMINAL_DIGEST", `${claimed} != ${expected}`);
}

export function canonicalSignedInteger(value: string, bits: 16 | 32 | 64): string {
  const parsed = parseSignedInt64Decimal(value);
  const minimum = -(1n << BigInt(bits - 1));
  const maximum = (1n << BigInt(bits - 1)) - 1n;
  if (parsed < minimum || parsed > maximum) fail("NUMERIC_RANGE", `${value}/int${bits}`);
  return parsed.toString();
}

export function canonicalExactNumeric(value: string): string {
  if (
    Buffer.byteLength(value, "utf8") > 128 ||
    !/^-?(?:0|[1-9][0-9]*)(?:\.[0-9]+)?$/u.test(value)
  ) {
    fail("NUMERIC_FORMAT", value);
  }
  if (/^-0(?:\.0+)?$/u.test(value)) fail("NUMERIC_NEGATIVE_ZERO", value);
  const [integer, fraction] = value.split(".");
  if (fraction === undefined) return value;
  const trimmed = fraction.replace(/0+$/u, "");
  return trimmed.length === 0 ? integer! : `${integer}.${trimmed}`;
}

export function canonicalRyuFloat(value: string, kind: "float4" | "float8"): string {
  if (
    Buffer.byteLength(value, "utf8") > 32 ||
    !/^-?(?:0|[1-9][0-9]*)(?:\.[0-9]+)?(?:e-?[1-9][0-9]*)?$/u.test(value)
  ) {
    fail("FLOAT_FORMAT", value);
  }
  const parsed = Number(value);
  if (!Number.isFinite(parsed) || Object.is(parsed, -0)) fail("FLOAT_VALUE", value);
  const rounded = kind === "float4" ? Math.fround(parsed) : parsed;
  if (!Number.isFinite(rounded) || Object.is(rounded, -0)) fail("FLOAT_VALUE", value);
  const shortest =
    kind === "float4" ? shortestFloat32(rounded) : normalizeJsFloat(rounded.toString());
  if (shortest !== value) fail("FLOAT_NON_CANONICAL", `${value} != ${shortest}`);
  return value;
}

export function validateNumericFixture(document: JsonObject): void {
  keys(document, ["format_version", "signed_integer", "exact_numeric", "float"]);
  literal(
    document.format_version,
    ["cloud-agents-platform-projection-numeric-fixtures/v1"],
    "numeric fixture version",
  );
  for (const test of array(document.signed_integer, "signed fixture").map((entry) =>
    object(entry, "signed case"),
  )) {
    keys(test, ["bits", "input", "expected", "expected_error"]);
    const bits = literal(test.bits, [16, 32, 64] as const, "integer bits");
    validateNumericCase(test, () =>
      canonicalSignedInteger(nonemptyString(test.input, "input"), bits),
    );
  }
  for (const test of array(document.exact_numeric, "numeric fixture").map((entry) =>
    object(entry, "numeric case"),
  )) {
    keys(test, ["input", "expected", "expected_error"]);
    validateNumericCase(test, () => canonicalExactNumeric(nonemptyString(test.input, "input")));
  }
  for (const test of array(document.float, "float fixture").map((entry) =>
    object(entry, "float case"),
  )) {
    keys(test, ["kind", "input", "expected", "expected_error"]);
    const kind = literal(test.kind, ["float4", "float8"] as const, "float kind");
    validateNumericCase(test, () => canonicalRyuFloat(nonemptyString(test.input, "input"), kind));
  }
}

function validateNumericCase(test: JsonObject, canonicalize: () => string): void {
  const expected = nullableString(test.expected, "numeric expected");
  const expectedError = nullableString(test.expected_error, "numeric expected error");
  if ((expected === null) === (expectedError === null))
    fail("NUMERIC_FIXTURE", "one result required");
  try {
    const actual = canonicalize();
    if (expected === null || actual !== expected)
      fail("NUMERIC_FIXTURE", `${actual} != ${expected}`);
  } catch (error) {
    if (
      expectedError === null ||
      !(error instanceof MigrationValidationError) ||
      error.code !== expectedError
    ) {
      throw error;
    }
  }
}

export function authorityProjectionDigest(projection: JsonObject): string {
  validateAuthorityProjection(projection);
  return migrationDigest({
    domain: "cloud-agents-platform-authority-projection/v1",
    projection,
  });
}

export function objectIdentityKey(identity: JsonObject): string {
  validateObjectIdentity(identity);
  return new TextDecoder().decode(canonicalizeMigrationJson(identity));
}

export function rawSha256(bytes: Uint8Array): string {
  return `sha256:${createHash("sha256").update(bytes).digest("hex")}`;
}

function normalizeJsFloat(value: string): string {
  const normalized = value
    .replace("e+", "e")
    .replace(/e(-?)0+([1-9][0-9]*)$/u, "e$1$2")
    .replace(/(\.\d*?[1-9])0+(?=e|$)/u, "$1")
    .replace(/\.0+(?=e|$)/u, "");
  return normalized;
}

function shortestFloat32(value: number): string {
  for (let precision = 1; precision <= 9; precision += 1) {
    const candidate = normalizeJsFloat(value.toPrecision(precision));
    if (Math.fround(Number(candidate)) === value) return candidate;
  }
  fail("FLOAT_VALUE", String(value));
}

function validateBooleanMap(value: JsonObject): void {
  const names = Object.keys(value);
  if (names.length === 0 || names.join("\0") !== names.toSorted().join("\0")) {
    fail("BOOLEAN_MAP_ORDER", names.join(","));
  }
  for (const [name, entry] of Object.entries(value)) {
    nonemptyString(name, "boolean map identity");
    boolean(entry, `boolean map ${name}`);
  }
}

function aclPrivilegesForSurface(surface: ACLSurface): ReadonlySet<string> {
  switch (surface) {
    case "database":
      return new Set(["CONNECT", "CREATE", "TEMPORARY"]);
    case "schema_explicit":
    case "schema_effective":
      return new Set(["CREATE", "USAGE"]);
    case "default_function":
      return new Set(["EXECUTE"]);
    case "default_sequence":
      return new Set(["SELECT", "UPDATE", "USAGE"]);
    case "default_table":
      return new Set(["DELETE", "INSERT", "REFERENCES", "SELECT", "TRIGGER", "TRUNCATE", "UPDATE"]);
    case "generic":
      return new Set(ACL_PRIVILEGES);
  }
}

function aclOriginsForSurface(surface: ACLSurface): ReadonlySet<string> {
  switch (surface) {
    case "database":
    case "schema_explicit":
      return new Set(["catalog_explicit"]);
    case "schema_effective":
      return new Set(["catalog_explicit", "owner_implicit", "public_default"]);
    case "default_function":
    case "default_sequence":
    case "default_table":
      return new Set(["default_acl_catalog"]);
    case "generic":
      return new Set(ACL_ORIGINS);
  }
}

function exactArray(
  actual: ReadonlyArray<string>,
  expected: ReadonlyArray<string>,
  label: string,
): void {
  if (actual.join("\0") !== expected.join("\0")) {
    fail("CLOSED_LIST", `${label}: ${actual.join(",")}`);
  }
}

function uniqueSorted<T>(
  values: ReadonlyArray<T>,
  identity: (value: T) => string,
  label: string,
): void {
  const identities = values.map(identity);
  if (
    new Set(identities).size !== identities.length ||
    identities.join("\0") !== identities.toSorted().join("\0")
  ) {
    fail("DUPLICATE_OR_UNSORTED", label);
  }
}

function sortedStrings(value: MigrationJson, label: string, unique = true): string[] {
  const result = stringArray(value, label);
  if (
    result.join("\0") !== result.toSorted().join("\0") ||
    (unique && new Set(result).size !== result.length)
  ) {
    fail("DUPLICATE_OR_UNSORTED", label);
  }
  return result;
}

function stringArray(value: MigrationJson, label: string): string[] {
  return array(value, label).map((entry) => nonemptyString(entry, label));
}

function keys(value: JsonObject, expected: ReadonlyArray<string>): void {
  const actual = Object.keys(value).toSorted();
  const wanted = [...expected].toSorted();
  if (actual.join("\0") !== wanted.join("\0")) {
    fail("UNKNOWN_OR_MISSING_FIELD", `${actual.join(",")} != ${wanted.join(",")}`);
  }
}

function object(value: MigrationJson, label: string): JsonObject {
  if (typeof value !== "object" || value === null || Array.isArray(value)) {
    fail("EXPECTED_OBJECT", label);
  }
  return value as JsonObject;
}

function array(value: MigrationJson, label: string): MigrationJson[] {
  if (!Array.isArray(value)) fail("EXPECTED_ARRAY", label);
  return value as MigrationJson[];
}

function nonemptyString(value: MigrationJson, label: string): string {
  if (typeof value !== "string" || value.length === 0) fail("EXPECTED_STRING", label);
  return value as string;
}

function nullableString(value: MigrationJson, label: string): string | null {
  if (value === null) return null;
  return nonemptyString(value, label);
}

function boolean(value: MigrationJson, label: string): boolean {
  if (typeof value !== "boolean") fail("EXPECTED_BOOLEAN", label);
  return value as boolean;
}

function literal<const T extends readonly MigrationJson[]>(
  value: MigrationJson,
  expected: T,
  label: string,
): T[number] {
  if (!expected.includes(value)) fail("UNEXPECTED_VALUE", `${label}: ${String(value)}`);
  return value as T[number];
}

function safeUint(value: MigrationJson, label: string, minimum = 0): number {
  if (!Number.isSafeInteger(value) || typeof value !== "number" || value < minimum) {
    fail("EXPECTED_UINT", label);
  }
  return value as number;
}

function uint32(value: MigrationJson, label: string, minimum = 0): number {
  const result = safeUint(value, label, minimum);
  if (result > UINT32_MAX) fail("UINT32_RANGE", label);
  return result;
}

function nullableSafeUint(value: MigrationJson, label: string): number | null {
  if (value === null) return null;
  return uint32(value, label);
}

function digest(value: MigrationJson, label: string): string {
  const result = nonemptyString(value, label);
  if (!DIGEST_PATTERN.test(result)) fail("DIGEST_FORMAT", label);
  return result;
}

function nullableDigest(value: MigrationJson, label: string): string | null {
  if (value === null) return null;
  return digest(value, label);
}

function migrationId(value: MigrationJson, label: string): string {
  const result = nonemptyString(value, label);
  if (!/^[0-9]{6}$/u.test(result)) fail("MIGRATION_ID", label);
  return result;
}

function nullableMigrationId(value: MigrationJson, label: string): string | null {
  if (value === null) return null;
  return migrationId(value, label);
}

function normalizedIdentifier(value: MigrationJson, label: string): string {
  const result = nonemptyString(value, label);
  if (!IDENTIFIER.test(result)) fail("NORMALIZED_IDENTIFIER", label);
  return result;
}

function rfc3339(value: MigrationJson, label: string): string {
  const result = nonemptyString(value, label);
  if (!/^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}Z$/u.test(result) || Number.isNaN(Date.parse(result))) {
    fail("RFC3339_UTC", label);
  }
  return result;
}

function fail(code: string, message: string): never {
  throw new MigrationValidationError(code, message);
}
