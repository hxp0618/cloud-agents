package migration

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"
)

const (
	projectionTargetSchema = "cloud_agents"
	projectionPublicRole   = "PUBLIC"
)

// projectionFixedQuery is the sole SQL registry used by ProjectionSnapshot.
// Callers select a closed query ID and can only supply typed bind parameters.
// Optional post-PG15 catalog columns are read through to_jsonb field lookup, so
// the PG15 statement never refers to a PG16-only column identifier.
func projectionFixedQuery(id projectionQueryID) (string, bool) {
	switch id {
	case projectionQuerySnapshotMetadata:
		return `SELECT current_setting('server_version_num'), pg_catalog.current_database(),
session_user, current_user, current_setting('transaction_isolation'),
current_setting('transaction_read_only'), current_setting('transaction_deferrable'),
(SELECT setting::pg_catalog.int8 FROM pg_catalog.pg_settings WHERE name = 'statement_timeout' AND unit = 'ms'),
(SELECT setting::pg_catalog.int8 FROM pg_catalog.pg_settings WHERE name = 'lock_timeout' AND unit = 'ms'),
(SELECT setting::pg_catalog.int8 FROM pg_catalog.pg_settings WHERE name = 'idle_in_transaction_session_timeout' AND unit = 'ms')`, true
	case projectionQuerySnapshotConfigure:
		return `SELECT pg_catalog.set_config('statement_timeout', '5000ms', true),
pg_catalog.set_config('lock_timeout', '1000ms', true),
pg_catalog.set_config('idle_in_transaction_session_timeout', '60000ms', true)`, true
	case projectionQuerySnapshotReset:
		return `DISCARD ALL`, true
	case projectionQuerySnapshotSanitation:
		return `SELECT session_user, current_user,
current_setting('search_path'), (SELECT reset_val FROM pg_catalog.pg_settings WHERE name = 'search_path'),
current_setting('statement_timeout'), (SELECT reset_val FROM pg_catalog.pg_settings WHERE name = 'statement_timeout'),
current_setting('lock_timeout'), (SELECT reset_val FROM pg_catalog.pg_settings WHERE name = 'lock_timeout'),
current_setting('idle_in_transaction_session_timeout'), (SELECT reset_val FROM pg_catalog.pg_settings WHERE name = 'idle_in_transaction_session_timeout'),
(SELECT pg_catalog.count(*)::pg_catalog.int8 FROM pg_catalog.pg_prepared_statements)`, true
	case projectionQuerySnapshotSetMigrationRole:
		return `SET ROLE cloud_agents_migration_owner`, true
	case projectionQuerySnapshotRoleReadback:
		return `SELECT session_user, current_user`, true
	case projectionQueryCapability:
		return `SELECT current_setting('server_version_num'),
EXISTS (SELECT 1 FROM pg_catalog.pg_attribute a JOIN pg_catalog.pg_class c ON c.oid = a.attrelid JOIN pg_catalog.pg_namespace n ON n.oid = c.relnamespace WHERE n.nspname = 'pg_catalog' AND c.relname = 'pg_auth_members' AND a.attname = 'inherit_option' AND a.attnum > 0 AND NOT a.attisdropped),
EXISTS (SELECT 1 FROM pg_catalog.pg_attribute a JOIN pg_catalog.pg_class c ON c.oid = a.attrelid JOIN pg_catalog.pg_namespace n ON n.oid = c.relnamespace WHERE n.nspname = 'pg_catalog' AND c.relname = 'pg_auth_members' AND a.attname = 'set_option' AND a.attnum > 0 AND NOT a.attisdropped),
EXISTS (SELECT 1 FROM pg_catalog.pg_attribute a JOIN pg_catalog.pg_class c ON c.oid = a.attrelid JOIN pg_catalog.pg_namespace n ON n.oid = c.relnamespace WHERE n.nspname = 'pg_catalog' AND c.relname = 'pg_database' AND a.attname = 'daticurules' AND a.attnum > 0 AND NOT a.attisdropped),
EXISTS (SELECT 1 FROM pg_catalog.pg_attribute a JOIN pg_catalog.pg_class c ON c.oid = a.attrelid JOIN pg_catalog.pg_namespace n ON n.oid = c.relnamespace WHERE n.nspname = 'pg_catalog' AND c.relname = 'pg_database' AND a.attname = 'datlocale' AND a.attnum > 0 AND NOT a.attisdropped)`, true
	case projectionQueryAuthorityRoles:
		return `SELECT r.rolname, r.rolcanlogin, r.rolinherit, r.rolsuper, r.rolcreaterole,
r.rolcreatedb, r.rolreplication, r.rolbypassrls, r.rolconnlimit::pg_catalog.text,
r.rolvaliduntil::pg_catalog.text, COALESCE(r.rolconfig, ARRAY[]::pg_catalog.text[])
FROM pg_catalog.pg_roles r
WHERE r.rolname = ANY($1::pg_catalog.text[])
ORDER BY r.rolname COLLATE "C"`, true
	case projectionQueryAuthorityMemberships:
		return `SELECT role_role.rolname, member_role.rolname, grantor_role.rolname,
m.admin_option, pg_catalog.to_jsonb(m)->>'inherit_option', pg_catalog.to_jsonb(m)->>'set_option'
FROM pg_catalog.pg_auth_members m
JOIN pg_catalog.pg_roles role_role ON role_role.oid = m.roleid
JOIN pg_catalog.pg_roles member_role ON member_role.oid = m.member
JOIN pg_catalog.pg_roles grantor_role ON grantor_role.oid = m.grantor
WHERE role_role.rolname = ANY($1::pg_catalog.text[]) OR member_role.rolname = ANY($1::pg_catalog.text[])
ORDER BY role_role.rolname COLLATE "C", member_role.rolname COLLATE "C", grantor_role.rolname COLLATE "C"`, true
	case projectionQueryAuthorityReachability:
		return `WITH input_cardinality AS (
 SELECT pg_catalog.cardinality($1::pg_catalog.text[]) AS member_count,
        pg_catalog.cardinality($2::pg_catalog.text[]) AS role_count
), members AS (
 SELECT member, ordinality
 FROM pg_catalog.unnest($1::pg_catalog.text[]) WITH ORDINALITY AS member_input(member, ordinality)
), roles AS (
 SELECT role, ordinality
 FROM pg_catalog.unnest($2::pg_catalog.text[]) WITH ORDINALITY AS role_input(role, ordinality)
), requested AS (
 SELECT members.member, roles.role
 FROM input_cardinality
 JOIN members ON input_cardinality.member_count = input_cardinality.role_count
 JOIN roles USING (ordinality)
)
SELECT member, role,
pg_catalog.pg_has_role(member, role, 'MEMBER'),
pg_catalog.pg_has_role(member, role, 'USAGE'),
CASE WHEN current_setting('server_version_num')::pg_catalog.int4 / 10000 = 15
 THEN pg_catalog.pg_has_role(member, role, 'MEMBER')
 ELSE pg_catalog.pg_has_role(member, role, 'SET')
END
FROM requested
ORDER BY role COLLATE "C", member COLLATE "C"`, true
	case projectionQueryDatabaseAuthority:
		return `WITH target AS (
 SELECT d.*, owner_role.rolname AS owner_name
 FROM pg_catalog.pg_database d JOIN pg_catalog.pg_roles owner_role ON owner_role.oid = d.datdba
 WHERE d.datname = pg_catalog.current_database()
), rows AS (
 SELECT 'database'::pg_catalog.text AS row_kind, t.datname, t.owner_name,
        pg_catalog.pg_encoding_to_char(t.encoding) AS encoding, t.datlocprovider::pg_catalog.text AS provider,
        t.datcollate, t.datctype,
        COALESCE(pg_catalog.to_jsonb(t)->>'datlocale', pg_catalog.to_jsonb(t)->>'daticulocale') AS icu_locale,
        pg_catalog.to_jsonb(t)->>'daticurules' AS icu_rules, t.datcollversion,
        t.datacl IS NULL AS acl_is_null, NULL::pg_catalog.text AS grantor,
        NULL::pg_catalog.text AS grantee, NULL::pg_catalog.text AS privilege_type,
        NULL::boolean AS is_grantable, NULL::pg_catalog.text AS principal,
        NULL::boolean AS effective_create, NULL::boolean AS effective_temporary
 FROM target t
 UNION ALL
 SELECT 'acl', t.datname, t.owner_name, NULL, NULL, NULL, NULL, NULL, NULL, NULL,
        false, grantor_role.rolname,
        CASE WHEN acl.grantee = 0 THEN 'PUBLIC' ELSE grantee_role.rolname END,
        acl.privilege_type, acl.is_grantable, NULL, NULL, NULL
 FROM target t CROSS JOIN LATERAL pg_catalog.aclexplode(t.datacl) acl
 LEFT JOIN pg_catalog.pg_roles grantor_role ON grantor_role.oid = acl.grantor
 LEFT JOIN pg_catalog.pg_roles grantee_role ON grantee_role.oid = acl.grantee
 UNION ALL
 SELECT 'effective', t.datname, t.owner_name, NULL, NULL, NULL, NULL, NULL, NULL, NULL,
        false, NULL, NULL, NULL, NULL, principal.name,
        pg_catalog.has_database_privilege(principal.name, t.oid, 'CREATE'),
        pg_catalog.has_database_privilege(principal.name, t.oid, 'TEMPORARY')
 FROM target t CROSS JOIN pg_catalog.unnest($1::pg_catalog.text[]) AS principal(name)
)
SELECT row_kind, datname, owner_name, encoding, provider, datcollate, datctype,
icu_locale, icu_rules, datcollversion, acl_is_null, grantor, grantee,
privilege_type, is_grantable, principal, effective_create, effective_temporary
FROM rows
ORDER BY CASE row_kind WHEN 'database' THEN 0 WHEN 'acl' THEN 1 ELSE 2 END,
principal COLLATE "C" NULLS FIRST,
grantor COLLATE "C" NULLS FIRST, grantee COLLATE "C" NULLS FIRST,
privilege_type COLLATE "C" NULLS FIRST`, true
	case projectionQueryRoleSettings:
		return `SELECT COALESCE(d.datname, '*'), COALESCE(r.rolname, '*'), s.setconfig
FROM pg_catalog.pg_db_role_setting s
LEFT JOIN pg_catalog.pg_database d ON d.oid = s.setdatabase
LEFT JOIN pg_catalog.pg_roles r ON r.oid = s.setrole
WHERE (s.setdatabase = 0 OR d.datname = $1)
AND (s.setrole = 0 OR r.rolname = ANY($2::pg_catalog.text[]))
ORDER BY COALESCE(d.datname, '*') COLLATE "C", COALESCE(r.rolname, '*') COLLATE "C"`, true
	case projectionQueryNamespace:
		return `WITH target AS (
 SELECT n.oid, n.nspname, owner_role.rolname AS owner, n.nspacl,
        pg_catalog.obj_description(n.oid, 'pg_namespace') AS comment
 FROM pg_catalog.pg_namespace n JOIN pg_catalog.pg_roles owner_role ON owner_role.oid = n.nspowner
 WHERE n.nspname = $1
), rows AS (
 SELECT 'schema'::pg_catalog.text AS row_kind, t.nspname, t.owner, t.nspacl IS NULL AS acl_is_null,
        NULL::pg_catalog.text AS grantor, NULL::pg_catalog.text AS grantee,
        NULL::pg_catalog.text AS privilege_type, NULL::boolean AS is_grantable,
        t.comment AS value1, NULL::pg_catalog.text AS value2, NULL::pg_catalog.text AS value3
 FROM target t
 UNION ALL
 SELECT 'schema_acl', t.nspname, t.owner, false, grantor_role.rolname,
        CASE WHEN acl.grantee = 0 THEN 'PUBLIC' ELSE grantee_role.rolname END,
        acl.privilege_type, acl.is_grantable, NULL, NULL, NULL
 FROM target t CROSS JOIN LATERAL pg_catalog.aclexplode(t.nspacl) acl
 LEFT JOIN pg_catalog.pg_roles grantor_role ON grantor_role.oid = acl.grantor
 LEFT JOIN pg_catalog.pg_roles grantee_role ON grantee_role.oid = acl.grantee
 UNION ALL
 SELECT 'security_label', t.nspname, t.owner, false, NULL, NULL, NULL, NULL,
        l.provider, l.label, NULL
 FROM target t JOIN pg_catalog.pg_seclabel l ON l.classoid = 'pg_catalog.pg_namespace'::pg_catalog.regclass AND l.objoid = t.oid AND l.objsubid = 0
 UNION ALL
 SELECT 'relation', t.nspname, owner_role.rolname, false, NULL, NULL, NULL, NULL,
        c.relname, c.relkind::pg_catalog.text, NULL
 FROM target t JOIN pg_catalog.pg_class c ON c.relnamespace = t.oid
 JOIN pg_catalog.pg_roles owner_role ON owner_role.oid = c.relowner
 UNION ALL
 SELECT 'function', t.nspname, owner_role.rolname, false, NULL, NULL, NULL, NULL,
        p.proname, p.prokind::pg_catalog.text, NULL
 FROM target t JOIN pg_catalog.pg_proc p ON p.pronamespace = t.oid
 JOIN pg_catalog.pg_roles owner_role ON owner_role.oid = p.proowner
 UNION ALL
 SELECT 'type', t.nspname, owner_role.rolname, false, NULL, NULL, NULL, NULL,
        y.typname, y.typtype::pg_catalog.text, NULL
 FROM target t JOIN pg_catalog.pg_type y ON y.typnamespace = t.oid
 JOIN pg_catalog.pg_roles owner_role ON owner_role.oid = y.typowner
 UNION ALL
 SELECT 'extension', t.nspname, owner_role.rolname, false, NULL, NULL, NULL, NULL,
        e.extname, NULL, NULL
 FROM target t JOIN pg_catalog.pg_extension e ON e.extnamespace = t.oid
 JOIN pg_catalog.pg_roles owner_role ON owner_role.oid = e.extowner
	UNION ALL
 SELECT 'collation', t.nspname, owner_role.rolname, false, NULL, NULL, NULL, NULL,
        c.collname, c.collprovider::pg_catalog.text, NULL
 FROM target t JOIN pg_catalog.pg_collation c ON c.collnamespace = t.oid
 JOIN pg_catalog.pg_roles owner_role ON owner_role.oid = c.collowner
	UNION ALL
 SELECT 'operator', t.nspname, owner_role.rolname, false, NULL, NULL, NULL, NULL,
        o.oprname, NULL, NULL
 FROM target t JOIN pg_catalog.pg_operator o ON o.oprnamespace = t.oid
 JOIN pg_catalog.pg_roles owner_role ON owner_role.oid = o.oprowner
	UNION ALL
 SELECT 'opclass', t.nspname, owner_role.rolname, false, NULL, NULL, NULL, NULL,
        o.opcname, am.amname, NULL
 FROM target t JOIN pg_catalog.pg_opclass o ON o.opcnamespace = t.oid
 JOIN pg_catalog.pg_am am ON am.oid = o.opcmethod
 JOIN pg_catalog.pg_roles owner_role ON owner_role.oid = o.opcowner
	UNION ALL
 SELECT 'opfamily', t.nspname, owner_role.rolname, false, NULL, NULL, NULL, NULL,
        o.opfname, am.amname, NULL
 FROM target t JOIN pg_catalog.pg_opfamily o ON o.opfnamespace = t.oid
 JOIN pg_catalog.pg_am am ON am.oid = o.opfmethod
 JOIN pg_catalog.pg_roles owner_role ON owner_role.oid = o.opfowner
	UNION ALL
 SELECT 'conversion', t.nspname, owner_role.rolname, false, NULL, NULL, NULL, NULL,
        c.conname, NULL, NULL
 FROM target t JOIN pg_catalog.pg_conversion c ON c.connamespace = t.oid
 JOIN pg_catalog.pg_roles owner_role ON owner_role.oid = c.conowner
	UNION ALL
 SELECT 'ts_config', t.nspname, owner_role.rolname, false, NULL, NULL, NULL, NULL,
        c.cfgname, NULL, NULL
 FROM target t JOIN pg_catalog.pg_ts_config c ON c.cfgnamespace = t.oid
 JOIN pg_catalog.pg_roles owner_role ON owner_role.oid = c.cfgowner
	UNION ALL
 SELECT 'ts_dict', t.nspname, owner_role.rolname, false, NULL, NULL, NULL, NULL,
        d.dictname, NULL, NULL
 FROM target t JOIN pg_catalog.pg_ts_dict d ON d.dictnamespace = t.oid
 JOIN pg_catalog.pg_roles owner_role ON owner_role.oid = d.dictowner
	UNION ALL
 SELECT 'ts_parser', t.nspname, t.owner, false, NULL, NULL, NULL, NULL,
        p.prsname, NULL, NULL
 FROM target t JOIN pg_catalog.pg_ts_parser p ON p.prsnamespace = t.oid
	UNION ALL
 SELECT 'ts_template', t.nspname, t.owner, false, NULL, NULL, NULL, NULL,
        p.tmplname, NULL, NULL
 FROM target t JOIN pg_catalog.pg_ts_template p ON p.tmplnamespace = t.oid
	UNION ALL
 SELECT 'statistic_ext', t.nspname, owner_role.rolname, false, NULL, NULL, NULL, NULL,
        s.stxname, NULL, NULL
 FROM target t JOIN pg_catalog.pg_statistic_ext s ON s.stxnamespace = t.oid
 JOIN pg_catalog.pg_roles owner_role ON owner_role.oid = s.stxowner
), output AS (
 SELECT r.row_kind AS row_kind, r.nspname AS nspname, r.owner AS owner,
        r.acl_is_null AS acl_is_null, r.grantor AS grantor, r.grantee AS grantee,
        r.privilege_type AS privilege_type, r.is_grantable AS is_grantable,
        r.value1 AS value1, r.value2 AS value2, r.value3 AS value3
 FROM rows r
 UNION ALL
 SELECT 'absent'::pg_catalog.text AS row_kind, $1::pg_catalog.text AS nspname,
        NULL::pg_catalog.text AS owner, true AS acl_is_null,
        NULL::pg_catalog.text AS grantor, NULL::pg_catalog.text AS grantee,
        NULL::pg_catalog.text AS privilege_type, NULL::pg_catalog.bool AS is_grantable,
        NULL::pg_catalog.text AS value1, NULL::pg_catalog.text AS value2,
        NULL::pg_catalog.text AS value3
 WHERE NOT EXISTS (SELECT 1 FROM target)
)
SELECT row_kind, nspname, owner, acl_is_null, grantor, grantee,
privilege_type, is_grantable, value1, value2, value3
FROM output
ORDER BY row_kind COLLATE "C", nspname COLLATE "C",
owner COLLATE "C" NULLS FIRST, grantor COLLATE "C" NULLS FIRST,
grantee COLLATE "C" NULLS FIRST, privilege_type COLLATE "C" NULLS FIRST,
value1 COLLATE "C" NULLS FIRST, value2 COLLATE "C" NULLS FIRST,
value3 COLLATE "C" NULLS FIRST`, true
	case projectionQueryNamespaceCreators:
		return `SELECT r.rolname
FROM pg_catalog.pg_roles r
JOIN pg_catalog.pg_namespace n ON n.nspname = $1
WHERE pg_catalog.has_schema_privilege(r.rolname, n.oid, 'CREATE')
ORDER BY r.rolname COLLATE "C"`, true
	case projectionQueryDefaultACLs:
		return `WITH projected AS (
 SELECT a.oid::pg_catalog.text AS row_identity,
        owner_role.rolname::pg_catalog.text AS owner,
        CASE WHEN a.defaclnamespace = 0 THEN NULL::pg_catalog.text ELSE n.nspname::pg_catalog.text END AS schema_name,
        a.defaclobjtype::pg_catalog.text AS object_kind,
        grantor_role.rolname::pg_catalog.text AS grantor,
        CASE WHEN acl.grantee = 0 THEN 'PUBLIC'::pg_catalog.text ELSE grantee_role.rolname::pg_catalog.text END AS grantee,
        acl.privilege_type::pg_catalog.text AS privilege_type,
        acl.is_grantable AS is_grantable,
        CASE WHEN target_namespace.oid IS NULL THEN false ELSE pg_catalog.has_schema_privilege(owner_role.rolname, target_namespace.oid, 'CREATE') END AS effective_create
 FROM pg_catalog.pg_default_acl a
 JOIN pg_catalog.pg_roles owner_role ON owner_role.oid = a.defaclrole
 LEFT JOIN pg_catalog.pg_namespace n ON n.oid = a.defaclnamespace
 LEFT JOIN (SELECT oid FROM pg_catalog.pg_namespace WHERE nspname = $1) target_namespace ON true
 LEFT JOIN LATERAL pg_catalog.aclexplode(a.defaclacl) acl ON true
 LEFT JOIN pg_catalog.pg_roles grantor_role ON grantor_role.oid = acl.grantor
 LEFT JOIN pg_catalog.pg_roles grantee_role ON grantee_role.oid = acl.grantee
 WHERE n.nspname = $1 OR a.defaclnamespace = 0 AND (
  owner_role.rolname = ANY($2::pg_catalog.text[])
  OR target_namespace.oid IS NOT NULL AND pg_catalog.has_schema_privilege(owner_role.rolname, target_namespace.oid, 'CREATE')
  OR EXISTS (
   SELECT 1 FROM pg_catalog.pg_default_acl scoped
   JOIN pg_catalog.pg_namespace scoped_namespace ON scoped_namespace.oid = scoped.defaclnamespace
   WHERE scoped.defaclrole = a.defaclrole AND scoped_namespace.nspname = $1
  )
 )
)
SELECT row_identity, owner, schema_name, object_kind, grantor, grantee,
privilege_type, is_grantable, effective_create
FROM projected
ORDER BY owner COLLATE "C", schema_name COLLATE "C" NULLS FIRST,
object_kind COLLATE "C", grantor COLLATE "C" NULLS FIRST,
grantee COLLATE "C" NULLS FIRST, privilege_type COLLATE "C" NULLS FIRST`, true
	case projectionQueryCatalogRelations:
		return projectionCatalogRelationsQuery, true
	case projectionQueryCatalogColumns:
		return projectionCatalogColumnsQuery, true
	case projectionQueryCatalogConstraints:
		return projectionCatalogConstraintsQuery, true
	case projectionQueryCatalogIndexes:
		return projectionCatalogIndexesQuery, true
	case projectionQueryCatalogIndexTerms:
		return projectionCatalogIndexTermsQuery, true
	case projectionQueryCatalogPolicies:
		return projectionCatalogPoliciesQuery, true
	case projectionQueryCatalogTriggers:
		return projectionCatalogTriggersQuery, true
	case projectionQueryCatalogFunctions:
		return projectionCatalogFunctionsQuery, true
	case projectionQueryCatalogFunctionArguments:
		return projectionCatalogFunctionArgumentsQuery, true
	case projectionQueryCatalogInternalObjects:
		return projectionCatalogInternalObjectsQuery, true
	case projectionQueryCatalogDependencies:
		return projectionCatalogDependenciesQuery, true
	case projectionQueryCatalogExpressions:
		return projectionCatalogExpressionsQuery, true
	default:
		return "", false
	}
}

type pgProjectionCapabilities struct {
	Major                       uint16
	ServerVersionNum            uint32
	MembershipInheritOption     bool
	MembershipSetOption         bool
	DatabaseICURules            bool
	DatabaseLocaleUnifiedColumn bool
}

type PGProjector struct {
	major        uint16
	capabilities pgProjectionCapabilities
	normalizer   pgMajorNormalizer
}

var _ Projector = (*PGProjector)(nil)

func NewPGProjector(ctx context.Context, snapshot ProjectionSnapshot) (*PGProjector, error) {
	if snapshot == nil {
		return nil, pgProjectionFailure(CodeProjectionCatalogQueryFailed, "capability", 0, "projection snapshot is unavailable")
	}
	if err := snapshot.Metadata().validate(); err != nil {
		return nil, err
	}
	capabilities, err := probePGProjectionCapabilities(ctx, snapshot)
	if err != nil {
		return nil, err
	}
	projector := &PGProjector{major: capabilities.Major, capabilities: capabilities}
	if err := projector.validateCapabilities(); err != nil {
		return nil, err
	}
	metadata := snapshot.Metadata()
	if metadata.PostgresMajor != capabilities.Major || metadata.ServerVersionNum != capabilities.ServerVersionNum {
		return nil, pgProjectionFailure(CodeProjectionCapabilityMismatch, "capability.metadata", capabilities.Major, "capability probe differs from snapshot server metadata")
	}
	switch capabilities.Major {
	case 15:
		projector.normalizer = pg15Normalizer{}
	case 16:
		projector.normalizer = pg16Normalizer{}
	case 17:
		projector.normalizer = pg17Normalizer{}
	}
	return projector, nil
}

type pgMajorNormalizer interface {
	membershipOptions(inherit, set *string) (bool, bool, error)
	usageEdgeAllowed(current RoleProjection, edge membershipGraphEdge) bool
	databaseProfile(provider string, icuLocale, icuRules, collationVersion *string) (string, *string, *string, *string, error)
}

func probePGProjectionCapabilities(ctx context.Context, queryer catalogQueryer) (pgProjectionCapabilities, error) {
	rows, cancel, err := queryProjectionBounded(ctx, queryer, projectionQueryCapability)
	if err != nil {
		return pgProjectionCapabilities{}, err
	}
	defer cancel()
	defer rows.Close()
	var versionText string
	var capabilities pgProjectionCapabilities
	if !rows.Next() {
		return pgProjectionCapabilities{}, pgProjectionFailure(CodeProjectionCatalogQueryFailed, "capability", 0, "capability probe returned no row")
	}
	if err := rows.Scan(&versionText, &capabilities.MembershipInheritOption, &capabilities.MembershipSetOption, &capabilities.DatabaseICURules, &capabilities.DatabaseLocaleUnifiedColumn); err != nil {
		return pgProjectionCapabilities{}, pgProjectionFailure(CodeProjectionCatalogQueryFailed, "capability", 0, "capability probe scan failed")
	}
	if rows.Next() {
		return pgProjectionCapabilities{}, pgProjectionFailure(CodeProjectionLimitExceeded, "capability", 0, "capability probe returned multiple rows")
	}
	if err := rows.Err(); err != nil {
		return pgProjectionCapabilities{}, pgProjectionFailure(CodeProjectionCatalogQueryFailed, "capability", 0, "capability probe iteration failed")
	}
	version, err := strconv.ParseUint(versionText, 10, 32)
	if err != nil || version < 10000 {
		return pgProjectionCapabilities{}, pgProjectionFailure(CodeProjectionCapabilityMismatch, "capability", 0, "server_version_num is invalid")
	}
	capabilities.ServerVersionNum = uint32(version)
	capabilities.Major = uint16(version / 10000)
	return capabilities, nil
}

func (projector *PGProjector) validateCapabilities() error {
	if projector == nil {
		return pgProjectionFailure(CodeProjectionUnsupportedMajor, "capability", 0, "PostgreSQL adapter is unavailable")
	}
	switch projector.major {
	case 15:
		if projector.capabilities.MembershipInheritOption || projector.capabilities.MembershipSetOption || projector.capabilities.DatabaseICURules || projector.capabilities.DatabaseLocaleUnifiedColumn {
			return pgProjectionFailure(CodeProjectionCapabilityMismatch, "capability", projector.major, "PostgreSQL 15 membership capabilities differ from the closed profile")
		}
	case 16:
		if !projector.capabilities.MembershipInheritOption || !projector.capabilities.MembershipSetOption || !projector.capabilities.DatabaseICURules || projector.capabilities.DatabaseLocaleUnifiedColumn {
			return pgProjectionFailure(CodeProjectionCapabilityMismatch, "capability", projector.major, "PostgreSQL membership grant options are unavailable")
		}
	case 17:
		if !projector.capabilities.MembershipInheritOption || !projector.capabilities.MembershipSetOption || !projector.capabilities.DatabaseICURules || !projector.capabilities.DatabaseLocaleUnifiedColumn {
			return pgProjectionFailure(CodeProjectionCapabilityMismatch, "capability", projector.major, "PostgreSQL membership or locale capabilities differ from the closed profile")
		}
	default:
		return pgProjectionFailure(CodeProjectionUnsupportedMajor, "capability", projector.major, "PostgreSQL major is outside the supported 15-17 range")
	}
	return nil
}

func (projector *PGProjector) ProjectAuthority(ctx context.Context, snapshot ProjectionSnapshot, contract VerifiedAuthorityContract, phase AuthorityPhase) (ProjectionResult[AuthorityProjection], error) {
	if err := projector.validateSnapshot(snapshot, phase); err != nil {
		return ProjectionResult[AuthorityProjection]{}, err
	}
	expected, err := contract.ExpectedProjection(phase)
	if err != nil {
		return ProjectionResult[AuthorityProjection]{}, err
	}
	before := snapshot.projectionStats()
	components, err := projector.readAuthorityComponents(ctx, snapshot, expected)
	if err != nil {
		return ProjectionResult[AuthorityProjection]{}, err
	}
	reachability, err := projector.projectReachability(expected.MembershipReachability, components.RolesByName, components.DirectMemberships)
	if err != nil {
		return ProjectionResult[AuthorityProjection]{}, err
	}
	if err := projector.crossCheckReachability(ctx, snapshot, reachability); err != nil {
		return ProjectionResult[AuthorityProjection]{}, err
	}
	metadata := snapshot.Metadata()
	projection := AuthorityProjection{
		Phase: phase, SessionUser: metadata.SessionUser, CurrentUser: metadata.CurrentUser,
		DatabaseName: components.DatabaseName, DatabaseOwner: components.DatabaseOwner,
		DatabaseEncoding: components.DatabaseEncoding, LocaleProvider: components.LocaleProvider,
		Datcollate: components.Datcollate, Datctype: components.Datctype,
		ICULocale: components.ICULocale, ICURules: components.ICURules, CollationVersion: components.CollationVersion,
		DatabaseACL: components.DatabaseACL, Roles: components.Roles,
		DirectMemberships: components.DirectMemberships, MembershipReachability: reachability,
		DatabaseRoleSettings: components.DatabaseRoleSettings,
		EffectiveCreate:      components.EffectiveCreate, EffectiveTemporary: components.EffectiveTemporary,
	}
	if metadata.DatabaseName != projection.DatabaseName {
		return ProjectionResult[AuthorityProjection]{}, pgProjectionFailure(CodeAuthorityDrift, "authority.database_name", projector.major, "snapshot and catalog database names differ")
	}
	if projection.DatabaseEncoding != "UTF8" || projection.Datcollate != "C" || projection.Datctype != "C" {
		return ProjectionResult[AuthorityProjection]{}, pgProjectionFailure(CodeAuthorityDrift, "authority.database_profile", projector.major, "database encoding or libc locale differs from the closed profile")
	}
	if err := projection.Validate(); err != nil {
		return ProjectionResult[AuthorityProjection]{}, pgProjectionFailure(CodeAuthorityDrift, "authority.projection", projector.major, "actual authority projection is invalid")
	}
	actualKey, actualErr := canonicalContractKey(projection)
	expectedKey, expectedErr := canonicalContractKey(expected)
	if actualErr != nil || expectedErr != nil || actualKey != expectedKey {
		return ProjectionResult[AuthorityProjection]{}, pgProjectionFailure(CodeAuthorityDrift, "authority.expected", projector.major, "actual authority differs from the verified binding")
	}
	digest, err := digestProjectionWrapper(AuthorityProjectionDigestDomain, projection)
	if err != nil {
		return ProjectionResult[AuthorityProjection]{}, err
	}
	projectionMetadata, err := projector.resultMetadata(snapshot, before, ProjectionKindAuthority, AuthorityProjectionDigestDomain, PostgreSQLAuthorityAdapter, contract.SubjectDigest(), nil)
	if err != nil {
		return ProjectionResult[AuthorityProjection]{}, err
	}
	return ProjectionResult[AuthorityProjection]{Projection: projection, Digest: digest, Metadata: projectionMetadata}, nil
}

func (projector *PGProjector) ProjectCatalog(_ context.Context, snapshot ProjectionSnapshot, contract VerifiedCatalogContract, scope ProjectionScope) (ProjectionResult[CatalogProjection], error) {
	if err := projector.validateSnapshot(snapshot, snapshot.Metadata().AuthorityPhase); err != nil {
		return ProjectionResult[CatalogProjection]{}, err
	}
	if err := contract.validate(); err != nil {
		return ProjectionResult[CatalogProjection]{}, err
	}
	if err := scope.Validate(); err != nil {
		return ProjectionResult[CatalogProjection]{}, pgProjectionFailure(CodeProjectionInvalidScope, "catalog.scope", projector.major, "catalog projection scope is invalid")
	}
	if !equalProjectionScopes(scope, contract.Scope()) {
		return ProjectionResult[CatalogProjection]{}, pgProjectionFailure(CodeProjectionInvalidScope, "catalog.scope", projector.major, "catalog projection scope differs from the verified contract")
	}
	return ProjectionResult[CatalogProjection]{}, pgProjectionFailure(CodeProjectionNotImplemented, "catalog", projector.major, "A2.1b expression projection and production binding are not implemented")
}

func (projector *PGProjector) ProjectPrecondition(ctx context.Context, snapshot ProjectionSnapshot, verifiedScope VerifiedSchemaBundleScope, condition CatalogPrecondition) (ProjectionResult[CatalogStateProjection], error) {
	if err := projector.validateSnapshot(snapshot, snapshot.Metadata().AuthorityPhase); err != nil {
		return ProjectionResult[CatalogStateProjection]{}, err
	}
	if err := verifiedScope.validatePrecondition(condition); err != nil {
		return ProjectionResult[CatalogStateProjection]{}, err
	}
	boundCondition := verifiedScope.BoundPrecondition()
	if boundCondition.Artifact != nil || len(boundCondition.AcceptedStates) != 2 {
		return ProjectionResult[CatalogStateProjection]{}, pgProjectionFailure(CodeProjectionNotImplemented, "precondition.contract", projector.major, "artifact-backed or sparse predecessor projection is not implemented in A2.1a")
	}
	scope := verifiedScope.Scope()
	before := snapshot.projectionStats()
	namespace, err := projector.readNamespace(ctx, snapshot, scope, verifiedScope.DefaultACLOwners(), verifiedScope.ObjectCreatorClosure())
	if err != nil {
		return ProjectionResult[CatalogStateProjection]{}, err
	}
	var state CatalogStateProjection
	if namespace.Absent {
		state.Absent = &SchemaAbsentProjection{State: "schema_absent", Scope: scope, Schema: projectionTargetSchema}
	} else {
		state.Present = &SchemaPresentProjection{State: "schema_present", Scope: scope, Body: namespace.Body}
	}
	if err := state.Validate(); err != nil {
		return ProjectionResult[CatalogStateProjection]{}, pgProjectionFailure(CodeCatalogDrift, "precondition.state", projector.major, "actual predecessor projection is invalid")
	}
	actualKey, err := canonicalContractKey(state)
	if err != nil {
		return ProjectionResult[CatalogStateProjection]{}, pgProjectionFailure(CodeCatalogDrift, "precondition.state", projector.major, "actual predecessor projection cannot be canonicalized")
	}
	matched := false
	for _, accepted := range boundCondition.AcceptedStates {
		if !equalProjectionScopes(scope, acceptedScope(accepted)) {
			return ProjectionResult[CatalogStateProjection]{}, pgProjectionFailure(CodeProjectionInvalidScope, "precondition.accepted.scope", projector.major, "accepted predecessor scope differs from the verified bundle")
		}
		key, keyErr := canonicalContractKey(accepted)
		if keyErr == nil && key == actualKey {
			matched = true
		}
	}
	if !matched {
		return ProjectionResult[CatalogStateProjection]{}, pgProjectionFailure(CodeCatalogDrift, "precondition.expected", projector.major, "actual predecessor state differs from the verified accepted states")
	}
	digest, err := state.ComputeDigest()
	if err != nil {
		return ProjectionResult[CatalogStateProjection]{}, err
	}
	resultMetadata, err := projector.resultMetadata(snapshot, before, ProjectionKindCatalogState, CatalogStateDigestDomain, PostgreSQLCatalogAdapter, verifiedScope.SubjectDigest(), &scope)
	if err != nil {
		return ProjectionResult[CatalogStateProjection]{}, err
	}
	return ProjectionResult[CatalogStateProjection]{Projection: state, Digest: digest, Metadata: resultMetadata}, nil
}

func (projector *PGProjector) ProjectTransitionState(_ context.Context, snapshot ProjectionSnapshot, contract VerifiedCatalogContract, scope ProjectionScope) (ProjectionResult[CatalogStateProjection], error) {
	if err := projector.validateSnapshot(snapshot, snapshot.Metadata().AuthorityPhase); err != nil {
		return ProjectionResult[CatalogStateProjection]{}, err
	}
	if err := contract.validate(); err != nil {
		return ProjectionResult[CatalogStateProjection]{}, err
	}
	if err := scope.Validate(); err != nil || !equalProjectionScopes(scope, contract.Scope()) {
		return ProjectionResult[CatalogStateProjection]{}, pgProjectionFailure(CodeProjectionInvalidScope, "transition.scope", projector.major, "transition scope differs from the verified catalog contract")
	}
	return ProjectionResult[CatalogStateProjection]{}, pgProjectionFailure(CodeProjectionNotImplemented, "transition", projector.major, "A2.1b relation and expression transition projection is not implemented")
}

func (projector *PGProjector) validateSnapshot(snapshot ProjectionSnapshot, phase AuthorityPhase) error {
	if projector == nil || snapshot == nil {
		return pgProjectionFailure(CodeProjectionSnapshotInvalid, "snapshot", 0, "projection snapshot or adapter is unavailable")
	}
	metadata := snapshot.Metadata()
	if err := metadata.validate(); err != nil {
		return err
	}
	if metadata.PostgresMajor != projector.major || metadata.ServerVersionNum != projector.capabilities.ServerVersionNum {
		return pgProjectionFailure(CodeProjectionCapabilityMismatch, "snapshot.server_version", projector.major, "snapshot server version differs from the probed adapter")
	}
	if metadata.AuthorityPhase != phase {
		return pgProjectionFailure(CodeProjectionMetadataMismatch, "snapshot.authority_phase", projector.major, "snapshot authority phase differs from the requested phase")
	}
	return nil
}

func (projector *PGProjector) resultMetadata(snapshot ProjectionSnapshot, before projectionQueryStats, kind ProjectionKind, domain, adapter string, subject Digest, scope *ProjectionScope) (ProjectionMetadata, error) {
	after := snapshot.projectionStats()
	if after.QueryCount < before.QueryCount || after.RowCount < before.RowCount || after.TotalBytes < before.TotalBytes {
		return ProjectionMetadata{}, pgProjectionFailure(CodeProjectionMetadataMismatch, "metadata.stats", projector.major, "projection query statistics moved backwards")
	}
	queryCount := after.QueryCount - before.QueryCount
	if queryCount > projectionMaxQueriesPerProjection {
		return ProjectionMetadata{}, pgProjectionFailure(CodeProjectionLimitExceeded, "metadata.query_count", projector.major, "projection query count limit exceeded")
	}
	metadata := ProjectionMetadata{
		ProjectionKind: kind, DigestDomain: domain, AdapterProfile: adapter,
		Snapshot: snapshot.Metadata(), VerifiedSubjectDigest: subject,
		Scope: cloneScopePointer(scope), LimitsProfile: ProjectionLimitsProfile,
		QueryCount: queryCount, RowCount: after.RowCount - before.RowCount,
		TotalBytes: after.TotalBytes - before.TotalBytes, RedactionProfile: ProjectionRedactionProfile,
	}
	if err := metadata.validate(); err != nil {
		return ProjectionMetadata{}, err
	}
	return metadata, nil
}

func digestProjectionWrapper(domain string, projection any) (Digest, error) {
	return digestFlatDomain(domain, struct {
		Projection any `json:"projection"`
	}{Projection: projection}, "")
}

func acceptedScope(state CatalogStateProjection) ProjectionScope {
	if state.Absent != nil {
		return state.Absent.Scope
	}
	if state.Present != nil {
		return state.Present.Scope
	}
	return ProjectionScope{}
}

func cloneScopePointer(scope *ProjectionScope) *ProjectionScope {
	if scope == nil {
		return nil
	}
	cloned := cloneProjectionValue(*scope)
	return &cloned
}

func queryProjectionBounded(ctx context.Context, queryer catalogQueryer, id projectionQueryID, args ...any) (Rows, context.CancelFunc, error) {
	if queryer == nil {
		return nil, func() {}, pgProjectionFailure(CodeProjectionCatalogQueryFailed, "query", 0, "catalog query boundary is unavailable")
	}
	queryCtx, cancel := context.WithTimeout(ctx, projectionQueryTimeout)
	rows, err := queryer.queryProjection(queryCtx, id, args...)
	if err != nil {
		cancel()
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return nil, func() {}, pgProjectionFailure(CodeProjectionCatalogQueryFailed, "query", 0, "bounded catalog query was canceled")
		}
		return nil, func() {}, pgProjectionFailure(CodeProjectionCatalogQueryFailed, "query", 0, "bounded catalog query failed")
	}
	return rows, cancel, nil
}

type projectionReadBudget struct {
	major      uint16
	rows       uint64
	totalBytes uint64
}

func (budget *projectionReadBudget) add(path string, values ...string) error {
	budget.rows++
	if budget.rows > projectionMaxQueryRows {
		return pgProjectionFailure(CodeProjectionLimitExceeded, path, budget.major, "projection row limit exceeded")
	}
	var rowBytes uint64
	for _, value := range values {
		if !utf8.ValidString(value) {
			return pgProjectionFailure(CodeProjectionCatalogQueryFailed, path, budget.major, "catalog text is not valid UTF-8")
		}
		rowBytes += uint64(len(value))
	}
	if rowBytes > projectionMaxRowBytes {
		return pgProjectionFailure(CodeProjectionLimitExceeded, path, budget.major, "projection row byte limit exceeded")
	}
	budget.totalBytes += rowBytes
	if budget.totalBytes > projectionMaxTotalResultBytes {
		return pgProjectionFailure(CodeProjectionLimitExceeded, path, budget.major, "projection total byte limit exceeded")
	}
	return nil
}

func pgProjectionFailure(code ErrorCode, path string, major uint16, message string) error {
	return projectionFailure(code, "pg-projector", path, major, false, message)
}

func nullableString(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func requireSortedUniqueStrings(path string, values []string) error {
	if uint64(len(values)) > projectionMaxPrincipals {
		return pgProjectionFailure(CodeProjectionLimitExceeded, path, 0, "principal limit exceeded")
	}
	for index, value := range values {
		if value == "" || !utf8.ValidString(value) {
			return pgProjectionFailure(CodeProjectionUnknownObject, path, 0, "principal identity is empty or invalid")
		}
		if index > 0 && strings.Compare(values[index-1], value) >= 0 {
			return pgProjectionFailure(CodeProjectionUnknownObject, path, 0, "principal identities are duplicate or unsorted")
		}
	}
	return nil
}

func sortedUnique(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}

func boolText(value *string, path string) (bool, error) {
	if value == nil {
		return false, pgProjectionFailure(CodeProjectionCapabilityMismatch, path, 0, "required boolean capability value is null")
	}
	switch *value {
	case "true", "t":
		return true, nil
	case "false", "f":
		return false, nil
	default:
		return false, pgProjectionFailure(CodeProjectionCatalogQueryFailed, path, 0, "catalog boolean value is invalid")
	}
}

func checkedUint32(value uint64, path string) (uint32, error) {
	if value > uint64(^uint32(0)) {
		return 0, pgProjectionFailure(CodeProjectionLimitExceeded, path, 0, fmt.Sprintf("bounded count exceeds uint32: %d", value))
	}
	return uint32(value), nil
}
