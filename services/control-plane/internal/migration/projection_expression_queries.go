package migration

// projectionCatalogExpressionsQuery returns the raw node tree and the
// server's non-pretty deparse for every expression-bearing catalog slot. The
// raw tree is used only as a closed-node/shape witness; no OID, raw Datum, or
// deparse string is retained in the projection digest. The expression
// normalizer resolves each row to logical identities and typed values.
const projectionCatalogExpressionsQuery = `WITH target AS (
 SELECT n.oid FROM pg_catalog.pg_namespace n WHERE n.nspname = $1
), target_relations AS (
 SELECT c.oid, c.relname
 FROM target t JOIN pg_catalog.pg_class c ON c.relnamespace = t.oid
 WHERE c.relkind IN ('r', 'p', 'v', 'm', 'f')
), target_functions AS (
 SELECT p.oid, p.proname, p.pronargdefaults, p.pronargs, p.proargdefaults,
        p.proargmodes, p.proallargtypes, p.proargtypes,
        COALESCE(ARRAY(
         SELECT argument_type_namespace.nspname
         FROM pg_catalog.unnest(p.proargtypes::pg_catalog.oid[]) WITH ORDINALITY AS argument(type_id, ordinal)
         JOIN pg_catalog.pg_type argument_type ON argument_type.oid = argument.type_id
         JOIN pg_catalog.pg_namespace argument_type_namespace ON argument_type_namespace.oid = argument_type.typnamespace
         ORDER BY argument.ordinal
        ), ARRAY[]::pg_catalog.text[]) AS identity_schemas,
        COALESCE(ARRAY(
         SELECT argument_type.typname
         FROM pg_catalog.unnest(p.proargtypes::pg_catalog.oid[]) WITH ORDINALITY AS argument(type_id, ordinal)
         JOIN pg_catalog.pg_type argument_type ON argument_type.oid = argument.type_id
         ORDER BY argument.ordinal
        ), ARRAY[]::pg_catalog.text[]) AS identity_names
 FROM target t JOIN pg_catalog.pg_proc p ON p.pronamespace = t.oid
 WHERE p.pronargdefaults > 0 AND p.proargdefaults IS NOT NULL
), expanded_function_args AS (
 SELECT p.oid AS function_id, p.proname, p.pronargs, p.pronargdefaults,
        arg.type_id, arg.ordinal,
        CASE WHEN p.proargmodes IS NULL THEN 'i'
             ELSE (p.proargmodes::pg_catalog.text[])[arg.ordinal] END AS mode
 FROM target_functions p
 CROSS JOIN LATERAL pg_catalog.unnest(COALESCE(p.proallargtypes, p.proargtypes::pg_catalog.oid[]))
      WITH ORDINALITY AS arg(type_id, ordinal)
), numbered_function_args AS (
 SELECT e.*,
        pg_catalog.count(*) FILTER (WHERE e.mode IN ('i', 'b', 'v'))
        OVER (PARTITION BY e.function_id ORDER BY e.ordinal) AS input_ordinal
 FROM expanded_function_args e
), function_default_rows AS (
 SELECT p.oid, p.proname, p.identity_schemas, p.identity_names,
        p.proargdefaults::pg_catalog.text AS raw_nodes,
        pg_catalog.pg_get_expr(p.proargdefaults, 0, false) AS deparsed,
        COALESCE(ARRAY(
         SELECT n.ordinal::pg_catalog.text
         FROM numbered_function_args n
         WHERE n.function_id = p.oid AND n.mode IN ('i', 'b', 'v')
           AND n.input_ordinal > p.pronargs - p.pronargdefaults
         ORDER BY n.ordinal
        ), ARRAY[]::pg_catalog.text[]) AS ordinals,
        COALESCE(ARRAY(
         SELECT type_namespace.nspname
         FROM numbered_function_args n
         JOIN pg_catalog.pg_type type_row ON type_row.oid = n.type_id
         JOIN pg_catalog.pg_namespace type_namespace ON type_namespace.oid = type_row.typnamespace
         WHERE n.function_id = p.oid AND n.mode IN ('i', 'b', 'v')
           AND n.input_ordinal > p.pronargs - p.pronargdefaults
         ORDER BY n.ordinal
        ), ARRAY[]::pg_catalog.text[]) AS expected_type_schemas,
        COALESCE(ARRAY(
         SELECT type_row.typname
         FROM numbered_function_args n
         JOIN pg_catalog.pg_type type_row ON type_row.oid = n.type_id
         WHERE n.function_id = p.oid AND n.mode IN ('i', 'b', 'v')
           AND n.input_ordinal > p.pronargs - p.pronargdefaults
         ORDER BY n.ordinal
        ), ARRAY[]::pg_catalog.text[]) AS expected_type_names
 FROM target_functions p
), expression_rows(
 source_kind, relation_name, object_name, function_schema, function_name,
 function_arg_schemas, function_arg_names, field, ordinals, raw_nodes,
 deparsed, expected_type_schemas, expected_type_names
) AS (
 SELECT 'column', r.relname, a.attname, NULL, NULL, ARRAY[]::pg_catalog.text[], ARRAY[]::pg_catalog.text[],
        'column_default', ARRAY['0']::pg_catalog.text[], ad.adbin::pg_catalog.text,
        pg_catalog.pg_get_expr(ad.adbin, ad.adrelid, false), ARRAY[type_namespace.nspname]::pg_catalog.text[], ARRAY[type_row.typname]::pg_catalog.text[]
 FROM target_relations r
 JOIN pg_catalog.pg_attribute a ON a.attrelid = r.oid AND a.attnum > 0 AND NOT a.attisdropped
 JOIN pg_catalog.pg_attrdef ad ON ad.adrelid = a.attrelid AND ad.adnum = a.attnum
 JOIN pg_catalog.pg_type type_row ON type_row.oid = a.atttypid
 JOIN pg_catalog.pg_namespace type_namespace ON type_namespace.oid = type_row.typnamespace
 UNION ALL
 SELECT 'constraint', r.relname, c.conname, NULL, NULL, ARRAY[]::pg_catalog.text[], ARRAY[]::pg_catalog.text[],
        'constraint_expression', ARRAY['0']::pg_catalog.text[], c.conbin::pg_catalog.text,
        pg_catalog.pg_get_expr(c.conbin, c.conrelid, false), ARRAY['pg_catalog']::pg_catalog.text[], ARRAY['bool']::pg_catalog.text[]
 FROM target_relations r
 JOIN pg_catalog.pg_constraint c ON c.conrelid = r.oid
 WHERE c.conbin IS NOT NULL
 UNION ALL
 SELECT 'index_predicate', r.relname, index_relation.relname, NULL, NULL, ARRAY[]::pg_catalog.text[], ARRAY[]::pg_catalog.text[],
        'index_predicate', ARRAY['0']::pg_catalog.text[], i.indpred::pg_catalog.text,
        pg_catalog.pg_get_expr(i.indpred, i.indrelid, false), ARRAY['pg_catalog']::pg_catalog.text[], ARRAY['bool']::pg_catalog.text[]
 FROM target_relations r
 JOIN pg_catalog.pg_index i ON i.indrelid = r.oid AND i.indpred IS NOT NULL
 JOIN pg_catalog.pg_class index_relation ON index_relation.oid = i.indexrelid
 UNION ALL
 SELECT 'index_terms', r.relname, index_relation.relname, NULL, NULL, ARRAY[]::pg_catalog.text[], ARRAY[]::pg_catalog.text[],
        'index_term',
        COALESCE(ARRAY(
         SELECT key.ordinal::pg_catalog.text
         FROM pg_catalog.unnest(i.indkey::pg_catalog.int2[]) WITH ORDINALITY AS key(attnum, ordinal)
         WHERE key.attnum = 0 ORDER BY key.ordinal
        ), ARRAY[]::pg_catalog.text[]),
        i.indexprs::pg_catalog.text, pg_catalog.pg_get_expr(i.indexprs, i.indrelid, false),
        COALESCE(ARRAY(
         SELECT type_namespace.nspname
         FROM pg_catalog.unnest(i.indkey::pg_catalog.int2[]) WITH ORDINALITY AS key(attnum, ordinal)
         JOIN pg_catalog.pg_attribute index_attribute ON index_attribute.attrelid = i.indexrelid AND index_attribute.attnum = key.ordinal
         JOIN pg_catalog.pg_type type_row ON type_row.oid = index_attribute.atttypid
         JOIN pg_catalog.pg_namespace type_namespace ON type_namespace.oid = type_row.typnamespace
         WHERE key.attnum = 0 ORDER BY key.ordinal
        ), ARRAY[]::pg_catalog.text[]),
        COALESCE(ARRAY(
         SELECT type_row.typname
         FROM pg_catalog.unnest(i.indkey::pg_catalog.int2[]) WITH ORDINALITY AS key(attnum, ordinal)
         JOIN pg_catalog.pg_attribute index_attribute ON index_attribute.attrelid = i.indexrelid AND index_attribute.attnum = key.ordinal
         JOIN pg_catalog.pg_type type_row ON type_row.oid = index_attribute.atttypid
         WHERE key.attnum = 0 ORDER BY key.ordinal
        ), ARRAY[]::pg_catalog.text[])
 FROM target_relations r
 JOIN pg_catalog.pg_index i ON i.indrelid = r.oid AND i.indexprs IS NOT NULL
 JOIN pg_catalog.pg_class index_relation ON index_relation.oid = i.indexrelid
 UNION ALL
 SELECT 'policy', r.relname, p.polname, NULL, NULL, ARRAY[]::pg_catalog.text[], ARRAY[]::pg_catalog.text[],
        'policy_using', ARRAY['0']::pg_catalog.text[], p.polqual::pg_catalog.text,
        pg_catalog.pg_get_expr(p.polqual, p.polrelid, false), ARRAY['pg_catalog']::pg_catalog.text[], ARRAY['bool']::pg_catalog.text[]
 FROM target_relations r JOIN pg_catalog.pg_policy p ON p.polrelid = r.oid
 WHERE p.polqual IS NOT NULL
 UNION ALL
 SELECT 'policy', r.relname, p.polname, NULL, NULL, ARRAY[]::pg_catalog.text[], ARRAY[]::pg_catalog.text[],
        'policy_with_check', ARRAY['0']::pg_catalog.text[], p.polwithcheck::pg_catalog.text,
        pg_catalog.pg_get_expr(p.polwithcheck, p.polrelid, false), ARRAY['pg_catalog']::pg_catalog.text[], ARRAY['bool']::pg_catalog.text[]
 FROM target_relations r JOIN pg_catalog.pg_policy p ON p.polrelid = r.oid
 WHERE p.polwithcheck IS NOT NULL
 UNION ALL
	SELECT 'trigger', r.relname, t.tgname, NULL, NULL, ARRAY[]::pg_catalog.text[], ARRAY[]::pg_catalog.text[],
	       'trigger_when', ARRAY['0']::pg_catalog.text[], t.tgqual::pg_catalog.text,
	       pg_catalog.pg_get_triggerdef(t.oid, false), ARRAY['pg_catalog']::pg_catalog.text[], ARRAY['bool']::pg_catalog.text[]
 FROM target_relations r JOIN pg_catalog.pg_trigger t ON t.tgrelid = r.oid
 WHERE t.tgqual IS NOT NULL
 UNION ALL
 SELECT 'function_defaults', NULL, NULL, 'cloud_agents', p.proname, p.identity_schemas, p.identity_names,
        'function_argument_default', p.ordinals, p.raw_nodes, p.deparsed, p.expected_type_schemas, p.expected_type_names
 FROM function_default_rows p
)
SELECT source_kind, relation_name, object_name, function_schema, function_name,
       function_arg_schemas, function_arg_names, field, ordinals, raw_nodes,
       deparsed, expected_type_schemas, expected_type_names
FROM expression_rows
ORDER BY source_kind COLLATE "C", relation_name COLLATE "C" NULLS FIRST,
object_name COLLATE "C" NULLS FIRST, function_name COLLATE "C" NULLS FIRST,
field COLLATE "C", ordinals::pg_catalog.text COLLATE "C"`
