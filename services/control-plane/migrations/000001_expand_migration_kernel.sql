DO $cloud_agents_schema$
DECLARE
    existing_schema record;
BEGIN
    SELECT
        namespace_row.oid,
        owner_role.rolname AS owner_name
    INTO existing_schema
    FROM pg_catalog.pg_namespace AS namespace_row
    JOIN pg_catalog.pg_roles AS owner_role
        ON owner_role.oid = namespace_row.nspowner
    WHERE namespace_row.nspname = 'cloud_agents';

    IF FOUND THEN
        IF existing_schema.owner_name IS DISTINCT FROM 'cloud_agents_migration_owner' THEN
            RAISE EXCEPTION USING
                ERRCODE = '42501',
                MESSAGE = 'existing cloud_agents schema has the wrong owner';
        END IF;

        BEGIN
            DROP SCHEMA cloud_agents RESTRICT;
        EXCEPTION
            WHEN dependent_objects_still_exist THEN
                RAISE EXCEPTION USING
                    ERRCODE = '55000',
                    MESSAGE = 'existing cloud_agents schema is not empty';
        END;
    END IF;

    CREATE SCHEMA cloud_agents AUTHORIZATION cloud_agents_migration_owner;
END
$cloud_agents_schema$;

REVOKE ALL ON SCHEMA cloud_agents FROM PUBLIC;
GRANT USAGE ON SCHEMA cloud_agents TO cloud_agents_runtime;
GRANT USAGE ON SCHEMA cloud_agents TO cloud_agents_bootstrap_admin;

ALTER DEFAULT PRIVILEGES FOR ROLE cloud_agents_migration_owner IN SCHEMA cloud_agents
    REVOKE ALL ON TABLES FROM PUBLIC;
ALTER DEFAULT PRIVILEGES FOR ROLE cloud_agents_migration_owner IN SCHEMA cloud_agents
    REVOKE ALL ON SEQUENCES FROM PUBLIC;
ALTER DEFAULT PRIVILEGES FOR ROLE cloud_agents_migration_owner IN SCHEMA cloud_agents
    REVOKE EXECUTE ON FUNCTIONS FROM PUBLIC;

CREATE TABLE cloud_agents.schema_migrations (
    migration_id text PRIMARY KEY,
    migration_name text NOT NULL UNIQUE,
    predecessor_id text,
    phase text NOT NULL,
    schema_from text NOT NULL,
    schema_to text NOT NULL,
    compatible_binary_min text NOT NULL,
    compatible_binary_max text NOT NULL,
    sql_path text NOT NULL UNIQUE,
    sql_size_bytes bigint NOT NULL,
    sql_sha256 text NOT NULL,
    bundle_digest text NOT NULL,
    transaction_mode text NOT NULL,
    reentrancy text NOT NULL,
    rollback_boundary text NOT NULL,
    requires_live_instance_preflight boolean NOT NULL,
    requires_pitr_preflight boolean NOT NULL,
    applied_at timestamptz NOT NULL DEFAULT pg_catalog.clock_timestamp(),
    applied_by text NOT NULL DEFAULT SESSION_USER,
    CONSTRAINT schema_migrations_id_format
        CHECK (migration_id ~ '^[0-9]{6}$'),
    CONSTRAINT schema_migrations_predecessor_format
        CHECK (predecessor_id IS NULL OR predecessor_id ~ '^[0-9]{6}$'),
    CONSTRAINT schema_migrations_not_own_predecessor
        CHECK (predecessor_id IS NULL OR predecessor_id <> migration_id),
    CONSTRAINT schema_migrations_phase
        CHECK (phase IN ('expand', 'backfill', 'contract')),
    CONSTRAINT schema_migrations_size_positive
        CHECK (sql_size_bytes > 0),
    CONSTRAINT schema_migrations_sql_digest
        CHECK (sql_sha256 ~ '^sha256:[0-9a-f]{64}$'),
    CONSTRAINT schema_migrations_bundle_digest
        CHECK (bundle_digest ~ '^sha256:[0-9a-f]{64}$'),
    CONSTRAINT schema_migrations_transaction_mode
        CHECK (transaction_mode IN ('transactional', 'non_transactional')),
    CONSTRAINT schema_migrations_applied_by_present
        CHECK (pg_catalog.char_length(applied_by) BETWEEN 1 AND 128)
);

ALTER TABLE cloud_agents.schema_migrations OWNER TO cloud_agents_migration_owner;
REVOKE ALL ON TABLE cloud_agents.schema_migrations FROM PUBLIC;
REVOKE ALL ON TABLE cloud_agents.schema_migrations FROM cloud_agents_bootstrap_admin;
GRANT SELECT ON TABLE cloud_agents.schema_migrations TO cloud_agents_runtime;

CREATE FUNCTION cloud_agents.is_valid_identifier(candidate text)
RETURNS boolean
LANGUAGE sql
IMMUTABLE
PARALLEL SAFE
SET search_path = pg_catalog, cloud_agents
AS $cloud_agents_function$
    SELECT candidate IS NOT NULL
        AND pg_catalog.char_length(candidate) BETWEEN 1 AND 128
        AND candidate ~ '^[A-Za-z0-9](?:[A-Za-z0-9._~-]{0,126}[A-Za-z0-9])?$'
$cloud_agents_function$;

ALTER FUNCTION cloud_agents.is_valid_identifier(text)
    OWNER TO cloud_agents_migration_owner;
REVOKE ALL ON FUNCTION cloud_agents.is_valid_identifier(text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION cloud_agents.is_valid_identifier(text)
    TO cloud_agents_runtime;

CREATE FUNCTION cloud_agents.require_tenant_id()
RETURNS text
LANGUAGE plpgsql
STABLE
PARALLEL SAFE
SET search_path = pg_catalog, cloud_agents
AS $cloud_agents_function$
DECLARE
    tenant_context text;
BEGIN
    tenant_context := pg_catalog.current_setting('cloud_agents.tenant_id', true);

    IF NOT cloud_agents.is_valid_identifier(tenant_context) THEN
        RAISE EXCEPTION USING
            ERRCODE = '22023',
            MESSAGE = 'cloud_agents.tenant_id is missing or is not a valid opaque identifier';
    END IF;

    RETURN tenant_context;
END
$cloud_agents_function$;

ALTER FUNCTION cloud_agents.require_tenant_id()
    OWNER TO cloud_agents_migration_owner;
REVOKE ALL ON FUNCTION cloud_agents.require_tenant_id() FROM PUBLIC;
GRANT EXECUTE ON FUNCTION cloud_agents.require_tenant_id()
    TO cloud_agents_runtime;
