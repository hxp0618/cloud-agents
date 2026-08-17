package migration

// The A2.1b catalog queries expose closed, typed catalog facts. OIDs are only
// ephemeral join addresses inside one owned snapshot; callers must normalize
// every retained identity before it can enter a projection or digest.
const projectionCatalogRelationsQuery = `WITH target AS (
 SELECT n.oid FROM pg_catalog.pg_namespace n WHERE n.nspname = $1
), relation_rows AS (
 SELECT c.oid::pg_catalog.text AS relation_id, c.relname,
        c.relkind::pg_catalog.text AS relkind,
        c.relpersistence::pg_catalog.text AS persistence,
        am.amname AS access_method, owner_role.rolname AS owner,
        c.relacl, c.relacl IS NULL AS acl_is_null,
        COALESCE(c.reloptions, ARRAY[]::pg_catalog.text[]) AS reloptions,
        c.relreplident::pg_catalog.text AS replica_identity,
        c.relrowsecurity AS rls_enabled, c.relforcerowsecurity AS rls_forced
 FROM target t
 JOIN pg_catalog.pg_class c ON c.relnamespace = t.oid
 JOIN pg_catalog.pg_roles owner_role ON owner_role.oid = c.relowner
 LEFT JOIN pg_catalog.pg_am am ON am.oid = c.relam
 WHERE c.relkind IN ('r', 'p', 'v', 'm', 'f', 'S')
)
SELECT r.relation_id, r.relname, r.relkind, r.persistence, r.access_method,
       r.owner, r.acl_is_null, grantor_role.rolname,
       CASE WHEN acl.grantee = 0 THEN 'PUBLIC' ELSE grantee_role.rolname END,
       acl.privilege_type, acl.is_grantable, r.reloptions,
       r.replica_identity, r.rls_enabled, r.rls_forced
FROM relation_rows r
LEFT JOIN LATERAL pg_catalog.aclexplode(r.relacl) acl ON true
LEFT JOIN pg_catalog.pg_roles grantor_role ON grantor_role.oid = acl.grantor
LEFT JOIN pg_catalog.pg_roles grantee_role ON grantee_role.oid = acl.grantee
ORDER BY r.relname COLLATE "C", grantor_role.rolname COLLATE "C" NULLS FIRST,
grantee_role.rolname COLLATE "C" NULLS FIRST,
acl.privilege_type COLLATE "C" NULLS FIRST`

const projectionCatalogColumnsQuery = `WITH target AS (
 SELECT n.oid FROM pg_catalog.pg_namespace n WHERE n.nspname = $1
), column_rows AS (
 SELECT c.oid::pg_catalog.text AS relation_id, c.relname,
        a.attnum::pg_catalog.text AS attnum, a.attname,
        y.oid::pg_catalog.text AS type_id, yn.nspname AS type_schema, y.typname AS type_name,
        a.atttypmod::pg_catalog.text AS typmod,
        CASE WHEN a.attcollation = 0 THEN NULL ELSE co.oid::pg_catalog.text END AS collation_id,
        con.nspname AS collation_schema, co.collname AS collation_name,
        a.attnotnull, a.attidentity::pg_catalog.text AS identity_mode,
        a.attgenerated::pg_catalog.text AS generated_mode,
        ad.oid IS NOT NULL AS has_default, a.attstorage::pg_catalog.text AS storage_mode,
        COALESCE(pg_catalog.to_jsonb(a)->>'attcompression', '') AS compression_mode,
        a.attacl, a.attacl IS NULL AS acl_is_null
 FROM target t
 JOIN pg_catalog.pg_class c ON c.relnamespace = t.oid
 JOIN pg_catalog.pg_attribute a ON a.attrelid = c.oid AND a.attnum > 0 AND NOT a.attisdropped
 JOIN pg_catalog.pg_type y ON y.oid = a.atttypid
 JOIN pg_catalog.pg_namespace yn ON yn.oid = y.typnamespace
 LEFT JOIN pg_catalog.pg_collation co ON co.oid = a.attcollation AND a.attcollation <> 0
 LEFT JOIN pg_catalog.pg_namespace con ON con.oid = co.collnamespace
 LEFT JOIN pg_catalog.pg_attrdef ad ON ad.adrelid = c.oid AND ad.adnum = a.attnum
 WHERE c.relkind IN ('r', 'p', 'v', 'm', 'f')
)
SELECT c.relation_id, c.relname, c.attnum, c.attname,
       c.type_id, c.type_schema, c.type_name, c.typmod,
       c.collation_id, c.collation_schema, c.collation_name,
       c.attnotnull, c.identity_mode, c.generated_mode, c.has_default,
       c.storage_mode, c.compression_mode, c.acl_is_null,
       grantor_role.rolname,
       CASE WHEN acl.grantee = 0 THEN 'PUBLIC' ELSE grantee_role.rolname END,
       acl.privilege_type, acl.is_grantable
FROM column_rows c
LEFT JOIN LATERAL pg_catalog.aclexplode(c.attacl) acl ON true
LEFT JOIN pg_catalog.pg_roles grantor_role ON grantor_role.oid = acl.grantor
LEFT JOIN pg_catalog.pg_roles grantee_role ON grantee_role.oid = acl.grantee
ORDER BY c.relname COLLATE "C", c.attnum::pg_catalog.int4,
grantor_role.rolname COLLATE "C" NULLS FIRST,
grantee_role.rolname COLLATE "C" NULLS FIRST,
acl.privilege_type COLLATE "C" NULLS FIRST`

const projectionCatalogConstraintsQuery = `WITH target AS (
 SELECT n.oid FROM pg_catalog.pg_namespace n WHERE n.nspname = $1
)
SELECT c.oid::pg_catalog.text AS constraint_id,
       relation_row.oid::pg_catalog.text AS relation_id, relation_row.relname,
       c.conname, c.contype::pg_catalog.text,
       COALESCE(ARRAY(
        SELECT a.attname
        FROM pg_catalog.unnest(c.conkey) WITH ORDINALITY AS key(attnum, ordinal)
        JOIN pg_catalog.pg_attribute a ON a.attrelid = c.conrelid AND a.attnum = key.attnum
        ORDER BY key.ordinal
       ), ARRAY[]::pg_catalog.text[]) AS columns,
       CASE WHEN referenced_relation.oid IS NULL THEN NULL ELSE referenced_relation.oid::pg_catalog.text END,
       referenced_namespace.nspname, referenced_relation.relname,
       COALESCE(ARRAY(
        SELECT a.attname
        FROM pg_catalog.unnest(c.confkey) WITH ORDINALITY AS key(attnum, ordinal)
        JOIN pg_catalog.pg_attribute a ON a.attrelid = c.confrelid AND a.attnum = key.attnum
        ORDER BY key.ordinal
       ), ARRAY[]::pg_catalog.text[]) AS referenced_columns,
       c.confmatchtype::pg_catalog.text, c.confupdtype::pg_catalog.text,
       c.confdeltype::pg_catalog.text, c.condeferrable, c.condeferred,
       c.convalidated, c.conbin IS NOT NULL AS has_expression,
       CASE WHEN c.conindid = 0 THEN NULL ELSE c.conindid::pg_catalog.text END AS index_id
FROM target t
JOIN pg_catalog.pg_class relation_row ON relation_row.relnamespace = t.oid
JOIN pg_catalog.pg_constraint c ON c.conrelid = relation_row.oid
LEFT JOIN pg_catalog.pg_class referenced_relation ON referenced_relation.oid = c.confrelid AND c.confrelid <> 0
LEFT JOIN pg_catalog.pg_namespace referenced_namespace ON referenced_namespace.oid = referenced_relation.relnamespace
WHERE relation_row.relkind IN ('r', 'p', 'v', 'm', 'f')
ORDER BY relation_row.relname COLLATE "C", c.conname COLLATE "C"`

const projectionCatalogIndexesQuery = `WITH target AS (
 SELECT n.oid FROM pg_catalog.pg_namespace n WHERE n.nspname = $1
)
SELECT parent.oid::pg_catalog.text AS relation_id, parent.relname,
       index_relation.oid::pg_catalog.text AS index_id, index_relation.relname AS index_name,
       am.amname, i.indisunique, i.indisprimary, i.indisvalid, i.indisready,
       i.indislive, i.indimmediate, i.indisclustered, i.indcheckxmin,
       i.indnullsnotdistinct, COALESCE(c.contype = 'x', false) AS exclusion,
       i.indisreplident, i.indpred IS NOT NULL AS has_predicate,
       CASE WHEN c.oid IS NULL THEN NULL ELSE c.oid::pg_catalog.text END AS constraint_id,
       i.indnkeyatts::pg_catalog.text, i.indnatts::pg_catalog.text
FROM target t
JOIN pg_catalog.pg_class parent ON parent.relnamespace = t.oid
JOIN pg_catalog.pg_index i ON i.indrelid = parent.oid
JOIN pg_catalog.pg_class index_relation ON index_relation.oid = i.indexrelid
JOIN pg_catalog.pg_am am ON am.oid = index_relation.relam
LEFT JOIN pg_catalog.pg_constraint c ON c.conindid = i.indexrelid
 AND c.conrelid = parent.oid AND c.contype IN ('p', 'u', 'x')
WHERE parent.relkind IN ('r', 'p', 'm', 'f')
ORDER BY parent.relname COLLATE "C", index_relation.relname COLLATE "C"`

const projectionCatalogIndexTermsQuery = `WITH target AS (
 SELECT n.oid FROM pg_catalog.pg_namespace n WHERE n.nspname = $1
), index_rows AS (
 SELECT parent.oid AS relation_oid, parent.relname,
        index_relation.oid AS index_oid, index_relation.relname AS index_name,
        i.*, key.attnum, key.ordinal,
        opclass_value.opclass_oid, collation_value.collation_oid,
        option_value.option_bits
 FROM target t
 JOIN pg_catalog.pg_class parent ON parent.relnamespace = t.oid
 JOIN pg_catalog.pg_index i ON i.indrelid = parent.oid
 JOIN pg_catalog.pg_class index_relation ON index_relation.oid = i.indexrelid
 CROSS JOIN LATERAL pg_catalog.unnest(i.indkey::pg_catalog.int2[]) WITH ORDINALITY AS key(attnum, ordinal)
 LEFT JOIN LATERAL (
  SELECT value AS opclass_oid
  FROM pg_catalog.unnest(i.indclass::pg_catalog.oid[]) WITH ORDINALITY AS item(value, ordinal)
  WHERE item.ordinal = key.ordinal
 ) opclass_value ON true
 LEFT JOIN LATERAL (
  SELECT value AS collation_oid
  FROM pg_catalog.unnest(i.indcollation::pg_catalog.oid[]) WITH ORDINALITY AS item(value, ordinal)
  WHERE item.ordinal = key.ordinal
 ) collation_value ON true
 LEFT JOIN LATERAL (
  SELECT value AS option_bits
  FROM pg_catalog.unnest(i.indoption::pg_catalog.int2[]) WITH ORDINALITY AS item(value, ordinal)
  WHERE item.ordinal = key.ordinal
 ) option_value ON true
 WHERE parent.relkind IN ('r', 'p', 'm', 'f')
)
SELECT r.relation_oid::pg_catalog.text, r.relname,
       r.index_oid::pg_catalog.text, r.index_name,
       r.ordinal::pg_catalog.text, r.ordinal > r.indnkeyatts AS included,
       column_row.attname, r.attnum = 0 AS has_expression,
       CASE WHEN opclass_row.oid IS NULL THEN NULL ELSE opclass_row.oid::pg_catalog.text END,
       opclass_namespace.nspname, opclass_row.opcname, opclass_method.amname,
       COALESCE(index_attribute.attoptions, ARRAY[]::pg_catalog.text[]),
       CASE WHEN collation_row.oid IS NULL OR collation_row.oid = 0 THEN NULL ELSE collation_row.oid::pg_catalog.text END,
       collation_namespace.nspname, collation_row.collname,
       CASE WHEN (COALESCE(r.option_bits, 0)::pg_catalog.int4 & 1) <> 0 THEN 'desc' ELSE 'asc' END,
       CASE WHEN (COALESCE(r.option_bits, 0)::pg_catalog.int4 & 2) <> 0 THEN 'first' ELSE 'last' END,
       CASE WHEN exclusion_operator.oid IS NULL OR exclusion_operator.oid = 0 THEN NULL ELSE exclusion_operator.oid::pg_catalog.text END,
       exclusion_namespace.nspname, exclusion_operator.oprname,
       left_type_namespace.nspname, left_type.typname,
       right_type_namespace.nspname, right_type.typname
FROM index_rows r
LEFT JOIN pg_catalog.pg_attribute column_row ON column_row.attrelid = r.relation_oid AND column_row.attnum = r.attnum AND r.attnum <> 0
LEFT JOIN pg_catalog.pg_attribute index_attribute ON index_attribute.attrelid = r.index_oid AND index_attribute.attnum = r.ordinal
LEFT JOIN pg_catalog.pg_opclass opclass_row ON opclass_row.oid = r.opclass_oid AND r.ordinal <= r.indnkeyatts
LEFT JOIN pg_catalog.pg_namespace opclass_namespace ON opclass_namespace.oid = opclass_row.opcnamespace
LEFT JOIN pg_catalog.pg_am opclass_method ON opclass_method.oid = opclass_row.opcmethod
LEFT JOIN pg_catalog.pg_collation collation_row ON collation_row.oid = r.collation_oid AND r.collation_oid <> 0
LEFT JOIN pg_catalog.pg_namespace collation_namespace ON collation_namespace.oid = collation_row.collnamespace
LEFT JOIN pg_catalog.pg_constraint exclusion_constraint ON exclusion_constraint.conindid = r.index_oid AND exclusion_constraint.contype = 'x'
LEFT JOIN LATERAL (
 SELECT value AS operator_oid
 FROM pg_catalog.unnest(exclusion_constraint.conexclop) WITH ORDINALITY AS item(value, ordinal)
 WHERE item.ordinal = r.ordinal
) exclusion_value ON true
LEFT JOIN pg_catalog.pg_operator exclusion_operator ON exclusion_operator.oid = exclusion_value.operator_oid
LEFT JOIN pg_catalog.pg_namespace exclusion_namespace ON exclusion_namespace.oid = exclusion_operator.oprnamespace
LEFT JOIN pg_catalog.pg_type left_type ON left_type.oid = exclusion_operator.oprleft
LEFT JOIN pg_catalog.pg_namespace left_type_namespace ON left_type_namespace.oid = left_type.typnamespace
LEFT JOIN pg_catalog.pg_type right_type ON right_type.oid = exclusion_operator.oprright
LEFT JOIN pg_catalog.pg_namespace right_type_namespace ON right_type_namespace.oid = right_type.typnamespace
ORDER BY r.relname COLLATE "C", r.index_name COLLATE "C", r.ordinal`

const projectionCatalogPoliciesQuery = `WITH target AS (
 SELECT n.oid FROM pg_catalog.pg_namespace n WHERE n.nspname = $1
)
SELECT c.oid::pg_catalog.text AS relation_id, c.relname,
       p.oid::pg_catalog.text AS policy_id, p.polname, p.polpermissive,
       p.polcmd::pg_catalog.text,
       ARRAY(
        SELECT CASE WHEN role_id = 0 THEN 'PUBLIC' ELSE r.rolname END
        FROM pg_catalog.unnest(p.polroles) WITH ORDINALITY AS roles(role_id, ordinal)
        LEFT JOIN pg_catalog.pg_roles r ON r.oid = roles.role_id
        ORDER BY CASE WHEN role_id = 0 THEN 'PUBLIC' ELSE r.rolname END COLLATE "C"
       ) AS roles,
       p.polqual IS NOT NULL AS has_using,
       p.polwithcheck IS NOT NULL AS has_with_check
FROM target t
JOIN pg_catalog.pg_class c ON c.relnamespace = t.oid
JOIN pg_catalog.pg_policy p ON p.polrelid = c.oid
WHERE c.relkind IN ('r', 'p')
ORDER BY c.relname COLLATE "C", p.polname COLLATE "C"`

const projectionCatalogTriggersQuery = `WITH target AS (
 SELECT n.oid FROM pg_catalog.pg_namespace n WHERE n.nspname = $1
)
SELECT c.oid::pg_catalog.text AS relation_id, c.relname,
       t.oid::pg_catalog.text AS trigger_id, t.tgname, t.tgisinternal,
       CASE WHEN con.oid IS NULL THEN NULL ELSE con.oid::pg_catalog.text END,
       con.conname,
       p.oid::pg_catalog.text AS function_id, pn.nspname, p.proname,
       ARRAY(
        SELECT an.nspname
        FROM pg_catalog.unnest(p.proargtypes::pg_catalog.oid[]) WITH ORDINALITY AS args(type_id, ordinal)
        JOIN pg_catalog.pg_type ay ON ay.oid = args.type_id
        JOIN pg_catalog.pg_namespace an ON an.oid = ay.typnamespace
        ORDER BY args.ordinal
       ),
       ARRAY(
        SELECT ay.typname
        FROM pg_catalog.unnest(p.proargtypes::pg_catalog.oid[]) WITH ORDINALITY AS args(type_id, ordinal)
        JOIN pg_catalog.pg_type ay ON ay.oid = args.type_id
        ORDER BY args.ordinal
       ),
       t.tgenabled::pg_catalog.text, t.tgtype::pg_catalog.text,
       COALESCE(ARRAY(
        SELECT a.attname
        FROM pg_catalog.unnest(t.tgattr::pg_catalog.int2[]) WITH ORDINALITY AS attrs(attnum, ordinal)
        JOIN pg_catalog.pg_attribute a ON a.attrelid = c.oid AND a.attnum = attrs.attnum
        ORDER BY attrs.ordinal
       ), ARRAY[]::pg_catalog.text[]),
       t.tgnargs::pg_catalog.text, pg_catalog.encode(t.tgargs, 'hex'),
       t.tgqual IS NOT NULL AS has_when
FROM target target_namespace
JOIN pg_catalog.pg_class c ON c.relnamespace = target_namespace.oid
JOIN pg_catalog.pg_trigger t ON t.tgrelid = c.oid
JOIN pg_catalog.pg_proc p ON p.oid = t.tgfoid
JOIN pg_catalog.pg_namespace pn ON pn.oid = p.pronamespace
LEFT JOIN pg_catalog.pg_constraint con ON con.oid = t.tgconstraint AND t.tgconstraint <> 0
WHERE c.relkind IN ('r', 'p', 'v', 'm', 'f')
ORDER BY c.relname COLLATE "C", t.tgname COLLATE "C"`

const projectionCatalogFunctionsQuery = `WITH target AS (
 SELECT n.oid FROM pg_catalog.pg_namespace n WHERE n.nspname = $1
), function_rows AS (
 SELECT p.oid::pg_catalog.text AS function_id, p.proname,
        p.prokind::pg_catalog.text AS function_kind, language.lanname,
        ARRAY(
         SELECT an.nspname
         FROM pg_catalog.unnest(p.proargtypes::pg_catalog.oid[]) WITH ORDINALITY AS args(type_id, ordinal)
         JOIN pg_catalog.pg_type ay ON ay.oid = args.type_id
         JOIN pg_catalog.pg_namespace an ON an.oid = ay.typnamespace
         ORDER BY args.ordinal
        ) AS identity_argument_schemas,
        ARRAY(
         SELECT ay.typname
         FROM pg_catalog.unnest(p.proargtypes::pg_catalog.oid[]) WITH ORDINALITY AS args(type_id, ordinal)
         JOIN pg_catalog.pg_type ay ON ay.oid = args.type_id
         ORDER BY args.ordinal
        ) AS identity_argument_names,
        CASE WHEN p.provariadic = 0 THEN NULL ELSE variadic_type.oid::pg_catalog.text END AS variadic_type_id,
        variadic_namespace.nspname AS variadic_type_schema, variadic_type.typname AS variadic_type_name,
        return_type.oid::pg_catalog.text AS return_type_id,
        return_namespace.nspname AS return_type_schema, return_type.typname AS return_type_name,
        p.proretset, owner_role.rolname AS owner, p.proacl, p.proacl IS NULL AS acl_is_null,
        p.prosecdef, p.provolatile::pg_catalog.text, p.proparallel::pg_catalog.text,
        p.proleakproof, p.proisstrict,
        COALESCE(p.proconfig, ARRAY[]::pg_catalog.text[]) AS config,
        p.procost::pg_catalog.text, p.prorows::pg_catalog.text,
        p.prosrc, p.probin
 FROM target t
 JOIN pg_catalog.pg_proc p ON p.pronamespace = t.oid
 JOIN pg_catalog.pg_language language ON language.oid = p.prolang
 JOIN pg_catalog.pg_roles owner_role ON owner_role.oid = p.proowner
 JOIN pg_catalog.pg_type return_type ON return_type.oid = p.prorettype
 JOIN pg_catalog.pg_namespace return_namespace ON return_namespace.oid = return_type.typnamespace
 LEFT JOIN pg_catalog.pg_type variadic_type ON variadic_type.oid = p.provariadic AND p.provariadic <> 0
 LEFT JOIN pg_catalog.pg_namespace variadic_namespace ON variadic_namespace.oid = variadic_type.typnamespace
)
SELECT f.function_id, f.proname, f.function_kind, f.lanname,
       f.identity_argument_schemas, f.identity_argument_names,
       f.variadic_type_id, f.variadic_type_schema, f.variadic_type_name,
       f.return_type_id, f.return_type_schema, f.return_type_name,
       f.proretset, f.owner, f.acl_is_null,
       grantor_role.rolname,
       CASE WHEN acl.grantee = 0 THEN 'PUBLIC' ELSE grantee_role.rolname END,
       acl.privilege_type, acl.is_grantable,
       f.prosecdef, f.provolatile, f.proparallel, f.proleakproof, f.proisstrict,
       f.config, f.procost, f.prorows, f.prosrc, f.probin
FROM function_rows f
LEFT JOIN LATERAL pg_catalog.aclexplode(f.proacl) acl ON true
LEFT JOIN pg_catalog.pg_roles grantor_role ON grantor_role.oid = acl.grantor
LEFT JOIN pg_catalog.pg_roles grantee_role ON grantee_role.oid = acl.grantee
ORDER BY f.proname COLLATE "C", f.function_id COLLATE "C",
grantor_role.rolname COLLATE "C" NULLS FIRST,
grantee_role.rolname COLLATE "C" NULLS FIRST,
acl.privilege_type COLLATE "C" NULLS FIRST`

const projectionCatalogFunctionArgumentsQuery = `WITH target AS (
 SELECT n.oid FROM pg_catalog.pg_namespace n WHERE n.nspname = $1
), expanded AS (
 SELECT p.oid::pg_catalog.text AS function_id, p.proname,
        p.pronargs, p.pronargdefaults,
        arg.type_id, arg.ordinal,
        CASE WHEN p.proargmodes IS NULL THEN 'i'
             ELSE (p.proargmodes::pg_catalog.text[])[arg.ordinal] END AS mode,
        CASE WHEN p.proargnames IS NULL THEN NULL
             ELSE NULLIF(p.proargnames[arg.ordinal], '') END AS argument_name
 FROM target t
 JOIN pg_catalog.pg_proc p ON p.pronamespace = t.oid
 CROSS JOIN LATERAL pg_catalog.unnest(COALESCE(p.proallargtypes, p.proargtypes::pg_catalog.oid[]))
      WITH ORDINALITY AS arg(type_id, ordinal)
), numbered AS (
 SELECT e.*,
        pg_catalog.count(*) FILTER (WHERE e.mode IN ('i', 'b', 'v'))
        OVER (PARTITION BY e.function_id ORDER BY e.ordinal) AS input_ordinal
 FROM expanded e
)
SELECT n.function_id, n.proname, n.ordinal::pg_catalog.text,
       n.argument_name, n.mode,
       y.oid::pg_catalog.text, yn.nspname, y.typname,
       n.mode IN ('i', 'b', 'v') AND n.input_ordinal > n.pronargs - n.pronargdefaults AS has_default
FROM numbered n
JOIN pg_catalog.pg_type y ON y.oid = n.type_id
JOIN pg_catalog.pg_namespace yn ON yn.oid = y.typnamespace
ORDER BY n.proname COLLATE "C", n.function_id COLLATE "C", n.ordinal`

const projectionCatalogInternalObjectsQuery = `WITH target AS (
 SELECT n.oid FROM pg_catalog.pg_namespace n WHERE n.nspname = $1
), owned_relations AS (
 SELECT c.oid, c.relname, c.reltoastrelid
 FROM target t JOIN pg_catalog.pg_class c ON c.relnamespace = t.oid
 WHERE c.relkind IN ('r', 'p', 'm', 'f')
), internal_rows AS (
 SELECT 'pg_type'::pg_catalog.text AS object_class,
        row_type.oid::pg_catalog.text AS object_id, '0'::pg_catalog.text AS subobject_id, row_type.typname AS object_name,
        'relation_row_type'::pg_catalog.text AS semantic_kind,
        r.relname AS owner_name
 FROM owned_relations r JOIN pg_catalog.pg_type row_type ON row_type.typrelid = r.oid
 UNION ALL
 SELECT 'pg_type', array_type.oid::pg_catalog.text, '0', array_type.typname, 'relation_array_type', r.relname
 FROM owned_relations r
 JOIN pg_catalog.pg_type row_type ON row_type.typrelid = r.oid
 JOIN pg_catalog.pg_type array_type ON array_type.typelem = row_type.oid AND array_type.typlen = -1
 UNION ALL
 SELECT 'pg_class', toast_relation.oid::pg_catalog.text, '0', toast_relation.relname, 'toast_relation', r.relname
 FROM owned_relations r
 JOIN pg_catalog.pg_class toast_relation ON toast_relation.oid = r.reltoastrelid AND r.reltoastrelid <> 0
 UNION ALL
 SELECT 'pg_class', toast_index.oid::pg_catalog.text, '0', toast_index.relname, 'toast_index', r.relname
 FROM owned_relations r
 JOIN pg_catalog.pg_class toast_relation ON toast_relation.oid = r.reltoastrelid AND r.reltoastrelid <> 0
 JOIN pg_catalog.pg_index toast_index_entry ON toast_index_entry.indrelid = toast_relation.oid
 JOIN pg_catalog.pg_class toast_index ON toast_index.oid = toast_index_entry.indexrelid
 UNION ALL
 SELECT 'pg_class', toast_relation.oid::pg_catalog.text, toast_attribute.attnum::pg_catalog.text,
        toast_attribute.attname, 'toast_column_' || toast_attribute.attname, r.relname
 FROM owned_relations r
 JOIN pg_catalog.pg_class toast_relation ON toast_relation.oid = r.reltoastrelid AND r.reltoastrelid <> 0
 JOIN pg_catalog.pg_attribute toast_attribute ON toast_attribute.attrelid = toast_relation.oid
  AND toast_attribute.attnum > 0 AND NOT toast_attribute.attisdropped
)
SELECT object_class, object_id, subobject_id, object_name, semantic_kind, owner_name
FROM internal_rows
ORDER BY owner_name COLLATE "C", semantic_kind COLLATE "C", object_id COLLATE "C", subobject_id::pg_catalog.int4`

const projectionCatalogDependenciesQuery = `WITH target_namespace AS (
 SELECT n.oid FROM pg_catalog.pg_namespace n WHERE n.nspname = $1
), target_relations AS (
 SELECT c.oid, c.reltoastrelid
 FROM target_namespace n JOIN pg_catalog.pg_class c ON c.relnamespace = n.oid
 WHERE c.relkind IN ('r', 'p', 'v', 'm', 'f', 'S')
), target_indexes AS (
 SELECT index_relation.oid
 FROM target_relations r
 JOIN pg_catalog.pg_index i ON i.indrelid = r.oid
 JOIN pg_catalog.pg_class index_relation ON index_relation.oid = i.indexrelid
), target_addresses(object_class, object_id, subobject_id) AS (
 SELECT 'pg_class'::pg_catalog.text, c.oid, 0::pg_catalog.int4 FROM target_relations c
 UNION ALL
 SELECT 'pg_class', i.oid, 0 FROM target_indexes i
 UNION ALL
 SELECT 'pg_class', c.oid, a.attnum::pg_catalog.int4
 FROM target_relations c JOIN pg_catalog.pg_attribute a ON a.attrelid = c.oid AND a.attnum > 0 AND NOT a.attisdropped
 UNION ALL
 SELECT 'pg_constraint', c.oid, 0 FROM target_relations r JOIN pg_catalog.pg_constraint c ON c.conrelid = r.oid
 UNION ALL
 SELECT 'pg_trigger', t.oid, 0 FROM target_relations r JOIN pg_catalog.pg_trigger t ON t.tgrelid = r.oid
 UNION ALL
 SELECT 'pg_policy', p.oid, 0 FROM target_relations r JOIN pg_catalog.pg_policy p ON p.polrelid = r.oid
 UNION ALL
 SELECT 'pg_proc', p.oid, 0 FROM target_namespace n JOIN pg_catalog.pg_proc p ON p.pronamespace = n.oid
 UNION ALL
 SELECT 'pg_type', y.oid, 0 FROM target_namespace n JOIN pg_catalog.pg_type y ON y.typnamespace = n.oid
 UNION ALL
 SELECT 'pg_class', toast_relation.oid, 0
 FROM target_relations r JOIN pg_catalog.pg_class toast_relation ON toast_relation.oid = r.reltoastrelid AND r.reltoastrelid <> 0
 UNION ALL
 SELECT 'pg_class', toast_index.oid, 0
 FROM target_relations r
 JOIN pg_catalog.pg_class toast_relation ON toast_relation.oid = r.reltoastrelid AND r.reltoastrelid <> 0
 JOIN pg_catalog.pg_index toast_index_entry ON toast_index_entry.indrelid = toast_relation.oid
 JOIN pg_catalog.pg_class toast_index ON toast_index.oid = toast_index_entry.indexrelid
), dependencies AS (
 SELECT d.*,
        CASE d.classid
         WHEN 'pg_catalog.pg_class'::pg_catalog.regclass THEN 'pg_class'
         WHEN 'pg_catalog.pg_constraint'::pg_catalog.regclass THEN 'pg_constraint'
         WHEN 'pg_catalog.pg_trigger'::pg_catalog.regclass THEN 'pg_trigger'
         WHEN 'pg_catalog.pg_policy'::pg_catalog.regclass THEN 'pg_policy'
         WHEN 'pg_catalog.pg_proc'::pg_catalog.regclass THEN 'pg_proc'
         WHEN 'pg_catalog.pg_type'::pg_catalog.regclass THEN 'pg_type'
         ELSE 'unsupported'
        END AS object_class,
        CASE d.refclassid
         WHEN 'pg_catalog.pg_class'::pg_catalog.regclass THEN 'pg_class'
         WHEN 'pg_catalog.pg_constraint'::pg_catalog.regclass THEN 'pg_constraint'
         WHEN 'pg_catalog.pg_trigger'::pg_catalog.regclass THEN 'pg_trigger'
         WHEN 'pg_catalog.pg_policy'::pg_catalog.regclass THEN 'pg_policy'
         WHEN 'pg_catalog.pg_proc'::pg_catalog.regclass THEN 'pg_proc'
         WHEN 'pg_catalog.pg_type'::pg_catalog.regclass THEN 'pg_type'
         WHEN 'pg_catalog.pg_namespace'::pg_catalog.regclass THEN 'pg_namespace'
         WHEN 'pg_catalog.pg_collation'::pg_catalog.regclass THEN 'pg_collation'
         WHEN 'pg_catalog.pg_opclass'::pg_catalog.regclass THEN 'pg_opclass'
         WHEN 'pg_catalog.pg_operator'::pg_catalog.regclass THEN 'pg_operator'
         WHEN 'pg_catalog.pg_cast'::pg_catalog.regclass THEN 'pg_cast'
         WHEN 'pg_catalog.pg_language'::pg_catalog.regclass THEN 'pg_language'
         WHEN 'pg_catalog.pg_am'::pg_catalog.regclass THEN 'pg_am'
         ELSE 'unsupported'
        END AS referenced_class
 FROM target_addresses target
 JOIN pg_catalog.pg_depend d
   ON d.objid = target.object_id AND d.objsubid = target.subobject_id
  AND (CASE d.classid
       WHEN 'pg_catalog.pg_class'::pg_catalog.regclass THEN 'pg_class'
       WHEN 'pg_catalog.pg_constraint'::pg_catalog.regclass THEN 'pg_constraint'
       WHEN 'pg_catalog.pg_trigger'::pg_catalog.regclass THEN 'pg_trigger'
       WHEN 'pg_catalog.pg_policy'::pg_catalog.regclass THEN 'pg_policy'
       WHEN 'pg_catalog.pg_proc'::pg_catalog.regclass THEN 'pg_proc'
       WHEN 'pg_catalog.pg_type'::pg_catalog.regclass THEN 'pg_type'
       ELSE 'unsupported'
      END) = target.object_class
 WHERE d.deptype <> 'p'
)
SELECT object_class, objid::pg_catalog.text, objsubid::pg_catalog.text,
       referenced_class, refobjid::pg_catalog.text, refobjsubid::pg_catalog.text,
       deptype::pg_catalog.text,
       CASE referenced_class
        WHEN 'pg_type' THEN referenced_type_namespace.nspname
        WHEN 'pg_collation' THEN referenced_collation_namespace.nspname
        WHEN 'pg_opclass' THEN referenced_opclass_namespace.nspname
        WHEN 'pg_operator' THEN referenced_operator_namespace.nspname
        WHEN 'pg_proc' THEN referenced_function_namespace.nspname
        WHEN 'pg_class' THEN referenced_relation_namespace.nspname
        ELSE NULL
       END AS referenced_schema,
       CASE referenced_class
        WHEN 'pg_type' THEN referenced_type.typname
        WHEN 'pg_collation' THEN referenced_collation.collname
        WHEN 'pg_opclass' THEN referenced_opclass.opcname
        WHEN 'pg_operator' THEN referenced_operator.oprname
        WHEN 'pg_proc' THEN referenced_function.proname
        WHEN 'pg_class' THEN referenced_relation.relname
        ELSE NULL
       END AS referenced_name,
       CASE referenced_class WHEN 'pg_opclass' THEN referenced_opclass_method.amname ELSE NULL END AS referenced_aux,
       CASE referenced_class
        WHEN 'pg_operator' THEN COALESCE(ARRAY(
         SELECT argument_namespace.nspname
         FROM pg_catalog.unnest(ARRAY[referenced_operator.oprleft, referenced_operator.oprright]::pg_catalog.oid[])
              WITH ORDINALITY AS arguments(type_id, ordinal)
         JOIN pg_catalog.pg_type argument_type ON argument_type.oid = arguments.type_id
         JOIN pg_catalog.pg_namespace argument_namespace ON argument_namespace.oid = argument_type.typnamespace
         WHERE arguments.type_id <> 0 ORDER BY arguments.ordinal
        ), ARRAY[]::pg_catalog.text[])
        WHEN 'pg_proc' THEN COALESCE(ARRAY(
         SELECT argument_namespace.nspname
         FROM pg_catalog.unnest(referenced_function.proargtypes::pg_catalog.oid[])
              WITH ORDINALITY AS arguments(type_id, ordinal)
         JOIN pg_catalog.pg_type argument_type ON argument_type.oid = arguments.type_id
         JOIN pg_catalog.pg_namespace argument_namespace ON argument_namespace.oid = argument_type.typnamespace
         ORDER BY arguments.ordinal
        ), ARRAY[]::pg_catalog.text[])
        WHEN 'pg_cast' THEN ARRAY[cast_source_namespace.nspname, cast_target_namespace.nspname]::pg_catalog.text[]
        ELSE ARRAY[]::pg_catalog.text[]
       END AS referenced_argument_schemas,
       CASE referenced_class
        WHEN 'pg_operator' THEN COALESCE(ARRAY(
         SELECT argument_type.typname
         FROM pg_catalog.unnest(ARRAY[referenced_operator.oprleft, referenced_operator.oprright]::pg_catalog.oid[])
              WITH ORDINALITY AS arguments(type_id, ordinal)
         JOIN pg_catalog.pg_type argument_type ON argument_type.oid = arguments.type_id
         WHERE arguments.type_id <> 0 ORDER BY arguments.ordinal
        ), ARRAY[]::pg_catalog.text[])
        WHEN 'pg_proc' THEN COALESCE(ARRAY(
         SELECT argument_type.typname
         FROM pg_catalog.unnest(referenced_function.proargtypes::pg_catalog.oid[])
              WITH ORDINALITY AS arguments(type_id, ordinal)
         JOIN pg_catalog.pg_type argument_type ON argument_type.oid = arguments.type_id
         ORDER BY arguments.ordinal
        ), ARRAY[]::pg_catalog.text[])
        WHEN 'pg_cast' THEN ARRAY[cast_source.typname, cast_target.typname]::pg_catalog.text[]
        ELSE ARRAY[]::pg_catalog.text[]
       END AS referenced_argument_names
FROM dependencies
LEFT JOIN pg_catalog.pg_type referenced_type ON referenced_class = 'pg_type' AND referenced_type.oid = refobjid
LEFT JOIN pg_catalog.pg_namespace referenced_type_namespace ON referenced_type_namespace.oid = referenced_type.typnamespace
LEFT JOIN pg_catalog.pg_collation referenced_collation ON referenced_class = 'pg_collation' AND referenced_collation.oid = refobjid
LEFT JOIN pg_catalog.pg_namespace referenced_collation_namespace ON referenced_collation_namespace.oid = referenced_collation.collnamespace
LEFT JOIN pg_catalog.pg_opclass referenced_opclass ON referenced_class = 'pg_opclass' AND referenced_opclass.oid = refobjid
LEFT JOIN pg_catalog.pg_namespace referenced_opclass_namespace ON referenced_opclass_namespace.oid = referenced_opclass.opcnamespace
LEFT JOIN pg_catalog.pg_am referenced_opclass_method ON referenced_opclass_method.oid = referenced_opclass.opcmethod
LEFT JOIN pg_catalog.pg_operator referenced_operator ON referenced_class = 'pg_operator' AND referenced_operator.oid = refobjid
LEFT JOIN pg_catalog.pg_namespace referenced_operator_namespace ON referenced_operator_namespace.oid = referenced_operator.oprnamespace
LEFT JOIN pg_catalog.pg_proc referenced_function ON referenced_class = 'pg_proc' AND referenced_function.oid = refobjid
LEFT JOIN pg_catalog.pg_namespace referenced_function_namespace ON referenced_function_namespace.oid = referenced_function.pronamespace
LEFT JOIN pg_catalog.pg_class referenced_relation ON referenced_class = 'pg_class' AND referenced_relation.oid = refobjid
LEFT JOIN pg_catalog.pg_namespace referenced_relation_namespace ON referenced_relation_namespace.oid = referenced_relation.relnamespace
LEFT JOIN pg_catalog.pg_cast referenced_cast ON referenced_class = 'pg_cast' AND referenced_cast.oid = refobjid
LEFT JOIN pg_catalog.pg_type cast_source ON cast_source.oid = referenced_cast.castsource
LEFT JOIN pg_catalog.pg_namespace cast_source_namespace ON cast_source_namespace.oid = cast_source.typnamespace
LEFT JOIN pg_catalog.pg_type cast_target ON cast_target.oid = referenced_cast.casttarget
LEFT JOIN pg_catalog.pg_namespace cast_target_namespace ON cast_target_namespace.oid = cast_target.typnamespace
ORDER BY object_class COLLATE "C", objid, objsubid,
referenced_class COLLATE "C", refobjid, refobjsubid, deptype`
