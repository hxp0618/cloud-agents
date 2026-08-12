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
  "ambiguous_unresolved",
] as const;
const RECONCILE_RESULTS = [
  "not_run",
  "exact_committed",
  "exact_pending",
  "divergent",
  "unresolved",
] as const;
const CHANGE_KINDS = ["create", "alter", "grant", "revoke"] as const;
const STATE_KINDS = ["schema_absent", "schema_present"] as const;
const UINT32_MAX = 4_294_967_295;
const ADVISORY_DOMAIN = "cloud-agents-platform:migrations:v1";
const ADVISORY_KEY = "-1047838957622507638";
const MAX_MEMBERSHIP_DEPTH = 32;
const MAX_CANONICAL_WITNESS_CANDIDATES = 4_096;
const DEFAULT_ACL_OWNERS = ["cloud_agents_migration_owner"] as const;
const OBJECT_CREATOR_CLOSURE = ["cloud_agents_migration_owner"] as const;
const BOOTSTRAP_ADMIN_ROLE = "cloud_agents_bootstrap_admin";
const MIGRATION_OWNER_ROLE = "cloud_agents_migration_owner";
const RUNTIME_ROLE = "cloud_agents_runtime";
const GROUP_ROLES = [BOOTSTRAP_ADMIN_ROLE, MIGRATION_OWNER_ROLE, RUNTIME_ROLE] as const;
const STABLE_PROJECTION_ERRORS = [
  "MIGRATION_PROJECTION_UNSUPPORTED_MAJOR",
  "MIGRATION_PROJECTION_CAPABILITY_MISMATCH",
  "MIGRATION_PROJECTION_CATALOG_QUERY_FAILED",
  "MIGRATION_PROJECTION_LIMIT_EXCEEDED",
  "MIGRATION_PROJECTION_UNKNOWN_OBJECT",
  "MIGRATION_PROJECTION_INVALID_EXPRESSION",
  "MIGRATION_PROJECTION_INVALID_SCOPE",
  "MIGRATION_PROJECTION_NON_CANONICAL_WITNESS",
  "MIGRATION_PROJECTION_LIMIT_OVERRIDE",
  "MIGRATION_PROJECTION_METADATA_MISMATCH",
  "MIGRATION_PROJECTION_SNAPSHOT_INVALID",
  "MIGRATION_PROJECTION_NOT_IMPLEMENTED",
  "MIGRATION_AUTHORITY_DRIFT",
  "MIGRATION_CATALOG_DRIFT",
  "MIGRATION_INTERMEDIATE_STATE_MISMATCH",
] as const;
const TERMINAL_RUNNER_ERRORS = [
  "MIGRATION_INVALID_SQL",
  "MIGRATION_INVALID_LEDGER",
  "MIGRATION_UNTRUSTED",
  "MIGRATION_LOCK_LOST",
  "MIGRATION_TRANSACTION_BOUNDARY",
  "MIGRATION_AMBIGUOUS_COMMIT",
  "MIGRATION_EVIDENCE_JOURNAL_FAILED",
  "MIGRATION_EVIDENCE_RECOVERY_REQUIRED",
  "MIGRATION_CONTEXT_CANCELED",
  "MIGRATION_DEADLINE_EXCEEDED",
] as const;
export const ATTEMPT_TERMINAL_STABLE_ERROR_CODES = [
  ...STABLE_PROJECTION_ERRORS.filter(
    (code) =>
      code !== "MIGRATION_PROJECTION_NOT_IMPLEMENTED" &&
      code !== "MIGRATION_PROJECTION_LIMIT_OVERRIDE",
  ),
  ...TERMINAL_RUNNER_ERRORS,
] as const;
const FAILURE_PHASES = [
  "preconnect",
  "journal_open",
  "journal_replay",
  "connected_session",
  "migration_role",
  "migration_transaction",
  "commit",
  "reconcile",
  "journal_close",
] as const;
const FAILURE_PATHS = [
  "trust",
  "journal",
  "authority",
  "catalog",
  "sql",
  "ledger",
  "transaction",
  "context",
] as const;
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
  | "default_table"
  | "default_type"
  | "default_schema";

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
  const rolesByName = new Map(
    roles.map((role) => [nonemptyString(role.name, "role name"), role] as const),
  );
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
  const graph = buildMembershipGraph(direct, rolesByName);
  const workloadMembership = validateMechanicalAuthorityProfile(projection, rolesByName, direct);
  const reachability = array(projection.membership_reachability, "reachability").map((entry) =>
    object(entry, "reachability"),
  );
  uniqueSorted(
    reachability,
    (entry) =>
      `${nonemptyString(entry.role, "reachability role")}\0${nonemptyString(entry.member, "reachability member")}`,
    "reachability",
  );
  if (reachability.length !== direct.length) {
    fail("REACHABILITY_ENDPOINT_CLOSURE", `${reachability.length} != ${direct.length}`);
  }
  for (const entry of reachability) {
    const role = nonemptyString(entry.role, "reachability role");
    const member = nonemptyString(entry.member, "reachability member");
    if (workloadMembership.get(member) !== role) {
      fail("REACHABILITY_ENDPOINT_CLOSURE", `${member}->${role}`);
    }
  }
  reachability.forEach((entry) => validateReachability(entry, graph, rolesByName, direct.length));
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
  const roleClosure = [...rolesByName.keys()].toSorted(compareUtf8);
  validateBooleanMap(object(projection.effective_create, "effective create"), roleClosure);
  validateBooleanMap(object(projection.effective_temporary, "effective temporary"), roleClosure);
}

function validateMechanicalAuthorityProfile(
  projection: JsonObject,
  roles: ReadonlyMap<string, JsonObject>,
  direct: ReadonlyArray<JsonObject>,
): ReadonlyMap<string, string> {
  const session = nonemptyString(projection.session_user, "session user");
  const current = nonemptyString(projection.current_user, "current user");
  const databaseOwner = nonemptyString(projection.database_owner, "database owner");
  const sessionRole = roles.get(session);
  const currentRole = roles.get(current);
  const databaseOwnerRole = roles.get(databaseOwner);
  if (sessionRole === undefined || currentRole === undefined || databaseOwnerRole === undefined) {
    fail("AUTHORITY_ROLE_CLOSURE", "session/current/database owner");
  }
  const phase = literal(projection.phase, PHASES, "authority phase");
  if (
    (phase === "connected_session" && current !== session) ||
    (phase !== "connected_session" && current !== MIGRATION_OWNER_ROLE)
  ) {
    fail("AUTHORITY_CURRENT_USER", `${phase}:${current}`);
  }
  for (const groupName of GROUP_ROLES) {
    const group = roles.get(groupName);
    if (
      group === undefined ||
      boolean(group.login, "group login") ||
      boolean(group.inherit, "group inherit") ||
      hasUnsafeAuthorityAttributes(group)
    ) {
      fail("AUTHORITY_GROUP_ROLE", groupName);
    }
  }
  if (
    !boolean(sessionRole.login, "session login") ||
    boolean(sessionRole.inherit, "session inherit") ||
    hasUnsafeAuthorityAttributes(sessionRole) ||
    session === databaseOwner ||
    GROUP_ROLES.includes(session as (typeof GROUP_ROLES)[number])
  ) {
    fail("AUTHORITY_SESSION_ROLE", session);
  }
  if (
    GROUP_ROLES.includes(databaseOwner as (typeof GROUP_ROLES)[number]) ||
    hasUnsafeAuthorityAttributes(databaseOwnerRole)
  ) {
    fail("AUTHORITY_DATABASE_OWNER", databaseOwner);
  }
  const workloads = new Map<string, string>();
  const grantors = new Set<string>();
  for (const membership of direct) {
    const role = nonemptyString(membership.role, "direct role");
    const member = nonemptyString(membership.member, "direct member");
    const grantor = nonemptyString(membership.grantor, "direct grantor");
    const memberRole = roles.get(member)!;
    const grantorRole = roles.get(grantor)!;
    if (grantor === session || !boolean(grantorRole.superuser, "grantor superuser")) {
      fail("AUTHORITY_MEMBERSHIP_GRANTOR", grantor);
    }
    if (role === databaseOwner || member === databaseOwner) {
      fail("AUTHORITY_DATABASE_OWNER_DELEGATION", `${member}->${role}`);
    }
    if (
      !boolean(memberRole.login, "workload login") ||
      hasUnsafeAuthorityAttributes(memberRole) ||
      boolean(membership.admin_option, "admin option")
    ) {
      fail("AUTHORITY_WORKLOAD_ROLE", member);
    }
    if (role === MIGRATION_OWNER_ROLE) {
      if (
        boolean(memberRole.inherit, "migration workload inherit") ||
        !boolean(membership.set_option, "migration set option")
      ) {
        fail("AUTHORITY_MIGRATION_MEMBERSHIP", member);
      }
    } else if (role === RUNTIME_ROLE || role === BOOTSTRAP_ADMIN_ROLE) {
      if (!boolean(memberRole.inherit, "runtime/bootstrap workload inherit")) {
        fail("AUTHORITY_INHERITING_MEMBERSHIP", member);
      }
    } else {
      fail("AUTHORITY_MEMBERSHIP_TARGET", role);
    }
    if (workloads.has(member)) fail("AUTHORITY_WORKLOAD_OVERLAP", member);
    workloads.set(member, role);
    grantors.add(grantor);
  }
  if (workloads.get(session) !== MIGRATION_OWNER_ROLE) {
    fail("AUTHORITY_SESSION_MEMBERSHIP", session);
  }
  for (const grantor of grantors) {
    if (workloads.has(grantor)) fail("AUTHORITY_GRANTOR_OVERLAP", grantor);
  }
  const expectedClosure = new Set<string>([...GROUP_ROLES, databaseOwner]);
  for (const workload of workloads.keys()) expectedClosure.add(workload);
  for (const grantor of grantors) expectedClosure.add(grantor);
  if (
    expectedClosure.size !== roles.size ||
    [...roles.keys()].some((role) => !expectedClosure.has(role))
  ) {
    fail("AUTHORITY_ROLE_CLOSURE", "mechanical authority closure");
  }
  return workloads;
}

function hasUnsafeAuthorityAttributes(role: JsonObject): boolean {
  return ["superuser", "bypass_rls", "create_role", "create_db", "replication"].some((field) =>
    boolean(role[field], `role ${field}`),
  );
}

export function validateCanonicalMembershipFixture(document: JsonObject): void {
  keys(document, ["roles", "direct_memberships", "membership_reachability"]);
  const roles = array(document.roles, "synthetic roles").map((role) =>
    object(role, "synthetic role"),
  );
  uniqueSorted(roles, (role) => nonemptyString(role.name, "role name"), "synthetic roles");
  roles.forEach(validateRole);
  const rolesByName = new Map(
    roles.map((role) => [nonemptyString(role.name, "role name"), role] as const),
  );
  const direct = array(document.direct_memberships, "synthetic direct memberships").map((entry) =>
    object(entry, "synthetic direct membership"),
  );
  uniqueSorted(
    direct,
    (entry) =>
      `${nonemptyString(entry.role, "direct role")}\0${nonemptyString(entry.member, "direct member")}\0${nonemptyString(entry.grantor, "direct grantor")}`,
    "synthetic direct memberships",
  );
  direct.forEach(validateDirectMembership);
  const graph = buildMembershipGraph(direct, rolesByName);
  const reachability = object(document.membership_reachability, "synthetic reachability");
  validateReachability(reachability, graph, rolesByName, direct.length);
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

type MembershipGraphEdge = {
  readonly to: string;
  readonly inherit: boolean;
  readonly set: boolean;
};

function buildMembershipGraph(
  direct: ReadonlyArray<JsonObject>,
  roles: ReadonlyMap<string, JsonObject>,
): ReadonlyMap<string, ReadonlyArray<MembershipGraphEdge>> {
  const graph = new Map<string, MembershipGraphEdge[]>();
  const endpoints = new Set<string>();
  for (const membership of direct) {
    const role = nonemptyString(membership.role, "direct role");
    const member = nonemptyString(membership.member, "direct member");
    const grantor = nonemptyString(membership.grantor, "direct grantor");
    if (!roles.has(role) || !roles.has(member) || !roles.has(grantor)) {
      fail("DIRECT_MEMBERSHIP_ROLE_CLOSURE", `${member}->${role}:${grantor}`);
    }
    const endpoint = `${member}\0${role}`;
    if (endpoints.has(endpoint)) fail("DIRECT_MEMBERSHIP_DUPLICATE", endpoint);
    endpoints.add(endpoint);
    const edges = graph.get(member) ?? [];
    edges.push({
      to: role,
      inherit: boolean(membership.inherit_option, "inherit option"),
      set: boolean(membership.set_option, "set option"),
    });
    graph.set(member, edges);
  }
  for (const edges of graph.values()) {
    edges.sort((left, right) => compareUtf8(left.to, right.to));
  }
  rejectMembershipCycles(graph, roles);
  return graph;
}

function rejectMembershipCycles(
  graph: ReadonlyMap<string, ReadonlyArray<MembershipGraphEdge>>,
  roles: ReadonlyMap<string, JsonObject>,
): void {
  const state = new Map<string, "visiting" | "visited">();
  const visit = (node: string, depth: number): void => {
    if (depth > MAX_MEMBERSHIP_DEPTH) fail("REACHABILITY_LIMIT", "membership depth");
    if (state.get(node) === "visiting") fail("REACHABILITY_CYCLE", node);
    if (state.get(node) === "visited") return;
    state.set(node, "visiting");
    for (const edge of graph.get(node) ?? []) visit(edge.to, depth + 1);
    state.set(node, "visited");
  };
  for (const role of [...roles.keys()].toSorted(compareUtf8)) visit(role, 0);
}

function canonicalMembershipWitness(
  graph: ReadonlyMap<string, ReadonlyArray<MembershipGraphEdge>>,
  roles: ReadonlyMap<string, JsonObject>,
  member: string,
  target: string,
  kind: string,
): {
  readonly reachable: boolean;
  readonly minDepth: number | null;
  readonly witness: string[] | null;
} {
  let paths: string[][] = [[member]];
  let candidates = 0;
  for (let depth = 0; depth <= MAX_MEMBERSHIP_DEPTH; depth += 1) {
    const matches = paths.filter((path) => path.at(-1) === target);
    if (matches.length !== 0) {
      const keyed = matches.map((path) => ({
        path,
        key: canonicalizeMigrationJson(path),
      }));
      const seen = new Set<string>();
      for (const candidate of keyed) {
        const key = Buffer.from(candidate.key).toString("hex");
        if (seen.has(key)) fail("REACHABILITY_DUPLICATE_CANDIDATE", key);
        seen.add(key);
      }
      keyed.sort((left, right) => Buffer.compare(Buffer.from(left.key), Buffer.from(right.key)));
      return { reachable: true, minDepth: depth, witness: [...keyed[0]!.path] };
    }
    if (depth === MAX_MEMBERSHIP_DEPTH) break;
    const next: string[][] = [];
    for (const path of paths) {
      const node = path.at(-1)!;
      for (const edge of graph.get(node) ?? []) {
        if (!membershipEdgeAllowed(kind, roles.get(node)!, edge)) continue;
        candidates += 1;
        if (candidates > MAX_CANONICAL_WITNESS_CANDIDATES) {
          fail("REACHABILITY_LIMIT", "canonical witness candidates");
        }
        next.push([...path, edge.to]);
      }
    }
    if (next.length === 0) break;
    paths = next;
  }
  return { reachable: false, minDepth: null, witness: null };
}

function membershipEdgeAllowed(
  kind: string,
  current: JsonObject,
  edge: MembershipGraphEdge,
): boolean {
  if (kind === "member") return true;
  if (kind === "usage") return boolean(current.inherit, "role inherit") && edge.inherit;
  if (kind === "set") return edge.set;
  fail("REACHABILITY_PRIVILEGE", kind);
}

function nullableStringArrayEqual(
  left: ReadonlyArray<string> | null,
  right: ReadonlyArray<string> | null,
): boolean {
  if (left === null || right === null) return left === right;
  return left.length === right.length && left.every((value, index) => value === right[index]);
}

function canonicalJsonText(value: MigrationJson): string {
  return new TextDecoder().decode(canonicalizeMigrationJson(value));
}

function compareUtf8(left: string, right: string): number {
  return Buffer.compare(Buffer.from(left, "utf8"), Buffer.from(right, "utf8"));
}

function validateReachability(
  entry: JsonObject,
  graph: ReadonlyMap<string, ReadonlyArray<MembershipGraphEdge>>,
  roles: ReadonlyMap<string, JsonObject>,
  completeEdgeCount: number,
): void {
  keys(entry, ["role", "member", "privileges"]);
  const role = nonemptyString(entry.role, "reachability role");
  const member = nonemptyString(entry.member, "reachability member");
  if (!roles.has(role) || !roles.has(member)) {
    fail("REACHABILITY_ROLE_CLOSURE", `${member}->${role}`);
  }
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
    const witness =
      privilege.canonical_witness === null
        ? null
        : stringArray(privilege.canonical_witness, "canonical witness");
    const edgeCount = uint32(privilege.edge_count, "edge count");
    if (edgeCount !== completeEdgeCount) {
      fail("REACHABILITY_EDGE_COUNT", `${edgeCount} != ${completeEdgeCount}`);
    }
    if (reachable !== (depth !== null && witness !== null)) {
      fail("REACHABILITY_SHAPE", String(privilege.privilege_kind));
    }
    if (reachable && witness!.length - 1 !== depth) {
      fail("REACHABILITY_SHAPE", "witness depth mismatch");
    }
    const expected = canonicalMembershipWitness(
      graph,
      roles,
      member,
      role,
      String(privilege.privilege_kind),
    );
    if (
      reachable !== expected.reachable ||
      depth !== expected.minDepth ||
      !nullableStringArrayEqual(witness, expected.witness)
    ) {
      fail("REACHABILITY_WITNESS", `${member}->${role}:${String(privilege.privilege_kind)}`);
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
    (entry) => {
      const privileges = sortedStrings(entry.privileges, "acl privileges");
      const grantable = sortedStrings(entry.grantable, "acl grantable");
      return `${nonemptyString(entry.grantor, "acl grantor")}\0${nonemptyString(entry.grantee, "acl grantee")}\0${canonicalJsonText(privileges)}\0${canonicalJsonText(grantable)}`;
    },
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
  validateDefaultACLRows(defaultACL, DEFAULT_ACL_OWNERS, OBJECT_CREATOR_CLOSURE);
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

export function validateDefaultACLScopeFixture(document: JsonObject): void {
  keys(document, ["format_version", "default_acl_owners", "object_creator_closure", "rows"]);
  literal(
    document.format_version,
    ["cloud-agents-platform-default-acl-scope-fixture/v1"],
    "default acl scope fixture",
  );
  const owners = sortedStrings(document.default_acl_owners, "default acl owners");
  const creators = sortedStrings(document.object_creator_closure, "object creator closure");
  exactArray(owners, DEFAULT_ACL_OWNERS, "default acl owners");
  exactArray(creators, OBJECT_CREATOR_CLOSURE, "object creator closure");
  validateDefaultACLRows(
    array(document.rows, "default acl rows").map((entry) =>
      object(entry, "default acl projection"),
    ),
    owners,
    creators,
  );
}

function validateDefaultACLRows(
  rows: ReadonlyArray<JsonObject>,
  owners: ReadonlyArray<string>,
  objectCreators: ReadonlyArray<string>,
): void {
  const ownerSet = new Set(owners);
  const creatorSet = new Set(objectCreators);
  for (const owner of owners) {
    if (!creatorSet.has(owner)) fail("DEFAULT_ACL_OWNER_CLOSURE", owner);
  }
  uniqueSorted(
    rows,
    (entry) => {
      const owner = nonemptyString(entry.owner, "default acl owner");
      const schemaKey =
        entry.schema === null ? "0" : `1${nonemptyString(entry.schema, "default acl schema")}`;
      return `${owner}\0${schemaKey}\0${nonemptyString(entry.object_kind, "default acl kind")}`;
    },
    "default acl",
  );
  for (const entry of rows) {
    keys(entry, ["owner", "schema", "object_kind", "acl"]);
    const owner = nonemptyString(entry.owner, "default acl owner");
    if (!ownerSet.has(owner) || !creatorSet.has(owner)) {
      fail("DEFAULT_ACL_OWNER_CLOSURE", owner);
    }
    const schema = nullableString(entry.schema, "default acl schema");
    if (schema !== null && schema !== "cloud_agents") {
      fail("DEFAULT_ACL_SCOPE", schema);
    }
    const objectKind = literal(
      entry.object_kind,
      ["function", "schema", "sequence", "table", "type"] as const,
      "default acl kind",
    );
    if (objectKind === "schema" && schema !== null) {
      fail("DEFAULT_ACL_SCHEMA_KIND_SCOPE", schema);
    }
    const acl = object(entry.acl, "default acl entries");
    if (acl.catalog_value !== "explicit") {
      fail("DEFAULT_ACL_CATALOG_VALUE", String(acl.catalog_value));
    }
    validateACLSet(acl, `default_${objectKind}`);
  }
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

export function validateStableFailureEvidence(evidence: JsonObject): void {
  keys(evidence, ["code", "projection_kind", "phase", "path", "major", "retryable"]);
  const code = literal(
    evidence.code,
    ATTEMPT_TERMINAL_STABLE_ERROR_CODES,
    "failure evidence code",
  ) as string;
  const phase = literal(evidence.phase, FAILURE_PHASES, "failure evidence phase") as string;
  const path = literal(evidence.path, FAILURE_PATHS, "failure evidence path") as string;
  const projectionKinds: Record<string, readonly string[]> = {
    MIGRATION_AUTHORITY_DRIFT: ["authority"],
    MIGRATION_CATALOG_DRIFT: ["catalog"],
    MIGRATION_INTERMEDIATE_STATE_MISMATCH: ["catalog"],
    MIGRATION_PROJECTION_UNSUPPORTED_MAJOR: ["snapshot"],
    MIGRATION_PROJECTION_CAPABILITY_MISMATCH: ["snapshot"],
    MIGRATION_PROJECTION_CATALOG_QUERY_FAILED: ["authority", "catalog"],
    MIGRATION_PROJECTION_LIMIT_EXCEEDED: ["authority", "catalog"],
    MIGRATION_PROJECTION_NON_CANONICAL_WITNESS: ["authority", "catalog"],
    MIGRATION_PROJECTION_UNKNOWN_OBJECT: ["authority", "catalog"],
    MIGRATION_PROJECTION_INVALID_SCOPE: ["authority", "catalog"],
    MIGRATION_PROJECTION_INVALID_EXPRESSION: ["catalog"],
    MIGRATION_PROJECTION_METADATA_MISMATCH: ["authority", "catalog", "snapshot"],
    MIGRATION_PROJECTION_SNAPSHOT_INVALID: ["snapshot"],
  };
  const projectionKind = evidence.projection_kind;
  const allowedKinds = projectionKinds[code];
  if (allowedKinds !== undefined) {
    if (typeof projectionKind !== "string" || !allowedKinds.includes(projectionKind)) {
      fail("STABLE_FAILURE_TUPLE", `${code}/projection_kind`);
    }
    const projectionPath =
      projectionKind === "authority"
        ? "authority"
        : projectionKind === "catalog"
          ? "catalog"
          : "transaction";
    const legalPhases =
      projectionKind === "authority"
        ? ["connected_session", "migration_role", "migration_transaction", "reconcile"]
        : projectionKind === "catalog"
          ? ["migration_role", "migration_transaction", "reconcile"]
          : ["connected_session", "migration_role", "migration_transaction", "reconcile"];
    if (
      path !== projectionPath ||
      !legalPhases.includes(phase) ||
      ((code === "MIGRATION_PROJECTION_UNSUPPORTED_MAJOR" ||
        code === "MIGRATION_PROJECTION_CAPABILITY_MISMATCH") &&
        phase !== "connected_session")
    ) {
      fail("STABLE_FAILURE_TUPLE", `${code}/${projectionKind}/${phase}/${path}`);
    }
  } else {
    if (projectionKind !== null) fail("STABLE_FAILURE_TUPLE", `${code}/projection_kind`);
    const nonProjection: Record<string, readonly [string, readonly string[]]> = {
      MIGRATION_EVIDENCE_JOURNAL_FAILED: [
        "journal",
        ["journal_open", "journal_replay", "reconcile", "journal_close"],
      ],
      MIGRATION_EVIDENCE_RECOVERY_REQUIRED: ["journal", ["journal_replay", "reconcile"]],
      MIGRATION_CONTEXT_CANCELED: ["context", FAILURE_PHASES],
      MIGRATION_DEADLINE_EXCEEDED: ["context", FAILURE_PHASES],
      MIGRATION_INVALID_SQL: ["sql", ["preconnect", "migration_transaction"]],
      MIGRATION_INVALID_LEDGER: [
        "ledger",
        ["migration_role", "migration_transaction", "reconcile"],
      ],
      MIGRATION_LOCK_LOST: [
        "transaction",
        ["migration_role", "migration_transaction", "reconcile"],
      ],
      MIGRATION_TRANSACTION_BOUNDARY: [
        "transaction",
        ["migration_transaction", "commit", "reconcile"],
      ],
      MIGRATION_AMBIGUOUS_COMMIT: ["transaction", ["commit", "reconcile"]],
      MIGRATION_UNTRUSTED: [
        "trust",
        ["preconnect", "connected_session", "migration_role", "reconcile"],
      ],
    };
    const rule = nonProjection[code];
    if (rule === undefined || path !== rule[0] || !rule[1].includes(phase)) {
      fail("STABLE_FAILURE_TUPLE", `${code}/${phase}/${path}`);
    }
  }
  const major =
    evidence.major === null ? null : uint32(evidence.major, "failure evidence major", 1);
  if (major !== null && major > 65_535) fail("UINT16_RANGE", "failure evidence major");
  const nullMajorPhase = ["preconnect", "journal_open", "journal_replay", "journal_close"].includes(
    phase,
  );
  if (nullMajorPhase && major !== null) fail("STABLE_FAILURE_MAJOR", phase);
  if (code === "MIGRATION_PROJECTION_UNSUPPORTED_MAJOR") {
    if (major === null) fail("STABLE_FAILURE_MAJOR", code);
  } else if (phase === "connected_session" && major !== 15 && major !== 16 && major !== 17) {
    fail("STABLE_FAILURE_MAJOR", phase);
  } else if (["migration_role", "migration_transaction", "commit"].includes(phase)) {
    if (major !== 15 && major !== 16 && major !== 17) fail("STABLE_FAILURE_MAJOR", phase);
  } else if (
    phase === "reconcile" &&
    major !== null &&
    major !== 15 &&
    major !== 16 &&
    major !== 17
  ) {
    fail("STABLE_FAILURE_MAJOR", phase);
  }
  boolean(evidence.retryable, "failure evidence retryable");
}

export function validateRetryProofEvidence(proof: JsonObject): void {
  keys(proof, [
    "proof_kind",
    "attempt_predecessor_catalog_digest",
    "observed_catalog_digest",
    "ledger_prefix_digest",
    "authority_result_digest",
    "commit_rejected_reason",
  ]);
  const kind = literal(
    proof.proof_kind,
    [
      "projection_transient_exact_predecessor",
      "precommit_rollback_exact_predecessor",
      "precommit_connection_terminated_exact_predecessor",
      "commit_rejected_exact_predecessor",
    ] as const,
    "retry proof kind",
  );
  for (const field of [
    "attempt_predecessor_catalog_digest",
    "observed_catalog_digest",
    "ledger_prefix_digest",
    "authority_result_digest",
  ] as const)
    digest(proof[field], field);
  if (proof.attempt_predecessor_catalog_digest !== proof.observed_catalog_digest) {
    fail("RETRY_PROOF_PREDECESSOR", "observed catalog differs");
  }
  const reason = literal(
    proof.commit_rejected_reason,
    [null, "serialization_failure", "deadlock_detected", "other_confirmed_postgres_error"],
    "commit rejected reason",
  );
  if ((kind === "commit_rejected_exact_predecessor") !== (reason !== null)) {
    fail("RETRY_PROOF_REASON", `${kind}/${String(reason)}`);
  }
}

/** Closed wire validation only. Cross-record proof is intentionally not inferred here. */
export function validateAttemptTerminalState(state: JsonObject): void {
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
    "failure_evidence",
    "retry_proof",
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
  if (
    error !== null &&
    !ATTEMPT_TERMINAL_STABLE_ERROR_CODES.includes(
      error as (typeof ATTEMPT_TERMINAL_STABLE_ERROR_CODES)[number],
    )
  ) {
    fail("STABLE_ERROR_CODE", error);
  }
  const failure =
    state.failure_evidence === null ? null : object(state.failure_evidence, "failure");
  if (failure !== null) validateStableFailureEvidence(failure);
  if ((error === null) !== (failure === null)) fail("ATTEMPT_TERMINAL_FAILURE", "nullability");
  if (failure !== null && failure.code !== error) {
    fail("ATTEMPT_TERMINAL_FAILURE", `${String(failure.code)} != ${String(error)}`);
  }
  const retryProof = state.retry_proof === null ? null : object(state.retry_proof, "retry proof");
  if (retryProof !== null) validateRetryProofEvidence(retryProof);
  const reconcile = literal(state.reconcile_result, RECONCILE_RESULTS, "reconcile result");
  const ambiguousStableCodes = new Set([
    "MIGRATION_AMBIGUOUS_COMMIT",
    "MIGRATION_EVIDENCE_JOURNAL_FAILED",
    "MIGRATION_EVIDENCE_RECOVERY_REQUIRED",
  ]);
  const unresolvedStableCodes = new Set([
    ...ambiguousStableCodes,
    "MIGRATION_UNTRUSTED",
    "MIGRATION_CONTEXT_CANCELED",
    "MIGRATION_DEADLINE_EXCEEDED",
  ]);
  const valid =
    (outcome === "committed" &&
      error === null &&
      failure === null &&
      retryProof === null &&
      reconcile === "not_run") ||
    ((outcome === "aborted_retryable" || outcome === "aborted_terminal") &&
      error !== null &&
      reconcile === "not_run") ||
    (outcome === "ambiguous_reconciled_committed" &&
      error !== null &&
      ambiguousStableCodes.has(error) &&
      reconcile === "exact_committed") ||
    (outcome === "ambiguous_reconciled_pending" &&
      error !== null &&
      ambiguousStableCodes.has(error) &&
      reconcile === "exact_pending") ||
    (outcome === "ambiguous_divergent" &&
      error !== null &&
      ambiguousStableCodes.has(error) &&
      reconcile === "divergent") ||
    (outcome === "ambiguous_unresolved" &&
      error !== null &&
      unresolvedStableCodes.has(error) &&
      reconcile === "unresolved");
  if (!valid) {
    fail("ATTEMPT_TERMINAL_COMBINATION", `${outcome}/${reconcile}/${String(error)}`);
  }
  const retryable = failure === null ? false : boolean(failure.retryable, "failure retryable");
  if (outcome === "aborted_retryable") {
    if (!retryable || retryProof === null) fail("ATTEMPT_TERMINAL_RETRY_PROOF", outcome);
    const kind = String(retryProof.proof_kind);
    if (
      (error === "MIGRATION_PROJECTION_CATALOG_QUERY_FAILED" &&
        kind !== "projection_transient_exact_predecessor") ||
      (error === "MIGRATION_TRANSACTION_BOUNDARY" &&
        ![
          "precommit_rollback_exact_predecessor",
          "precommit_connection_terminated_exact_predecessor",
          "commit_rejected_exact_predecessor",
        ].includes(kind)) ||
      (error !== "MIGRATION_PROJECTION_CATALOG_QUERY_FAILED" &&
        error !== "MIGRATION_TRANSACTION_BOUNDARY") ||
      retryProof.commit_rejected_reason === "other_confirmed_postgres_error"
    )
      fail("ATTEMPT_TERMINAL_RETRY_PROOF", `${String(error)}/${kind}`);
  } else if (outcome === "aborted_terminal" && error === "MIGRATION_TRANSACTION_BOUNDARY") {
    if (
      retryable ||
      retryProof === null ||
      ![
        "precommit_rollback_exact_predecessor",
        "precommit_connection_terminated_exact_predecessor",
        "commit_rejected_exact_predecessor",
      ].includes(String(retryProof.proof_kind))
    )
      fail("ATTEMPT_TERMINAL_RETRY_PROOF", outcome);
  } else if (retryable || retryProof !== null) {
    fail("ATTEMPT_TERMINAL_RETRY_PROOF", outcome);
  }
  if (outcome === "aborted_terminal" && error === "MIGRATION_AMBIGUOUS_COMMIT") {
    fail("ATTEMPT_TERMINAL_COMBINATION", "ambiguous commit cannot be aborted_terminal");
  }
  const requiresFinalIntermediate = outcome === "committed" || outcome.startsWith("ambiguous_");
  if (requiresFinalIntermediate && lastIntermediate === null) {
    fail("ATTEMPT_TERMINAL_COMBINATION", "final attempt missing intermediate digest");
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

function validateBooleanMap(value: JsonObject, expectedIdentities: ReadonlyArray<string>): void {
  const names = Object.keys(value);
  if (
    names.length === 0 ||
    names.join("\0") !== names.toSorted(compareUtf8).join("\0") ||
    names.join("\0") !== expectedIdentities.join("\0")
  ) {
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
    case "default_type":
      return new Set(["USAGE"]);
    case "default_schema":
      return new Set(["CREATE", "USAGE"]);
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
    case "default_type":
    case "default_schema":
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
    identities.join("\0") !== identities.toSorted(compareUtf8).join("\0")
  ) {
    fail("DUPLICATE_OR_UNSORTED", label);
  }
}

function sortedStrings(value: MigrationJson, label: string, unique = true): string[] {
  const result = stringArray(value, label);
  if (
    result.join("\0") !== result.toSorted(compareUtf8).join("\0") ||
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
