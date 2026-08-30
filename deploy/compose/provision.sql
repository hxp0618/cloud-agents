\set ON_ERROR_STOP on

\if :{?cloud_agents_database}
\else
DO $cloud_agents_missing_database$
BEGIN
    RAISE EXCEPTION USING ERRCODE = '22023', MESSAGE = 'cloud_agents_database is required';
END
$cloud_agents_missing_database$;
\endif

\if :{?cloud_agents_migration_password}
\else
DO $cloud_agents_missing_migration_password$
BEGIN
    RAISE EXCEPTION USING ERRCODE = '22023', MESSAGE = 'cloud_agents_migration_password is required';
END
$cloud_agents_missing_migration_password$;
\endif

\if :{?cloud_agents_runtime_password}
\else
DO $cloud_agents_missing_runtime_password$
BEGIN
    RAISE EXCEPTION USING ERRCODE = '22023', MESSAGE = 'cloud_agents_runtime_password is required';
END
$cloud_agents_missing_runtime_password$;
\endif

\if :{?cloud_agents_tenant_bootstrap_password}
\else
DO $cloud_agents_missing_tenant_bootstrap_password$
BEGIN
    RAISE EXCEPTION USING ERRCODE = '22023', MESSAGE = 'cloud_agents_tenant_bootstrap_password is required';
END
$cloud_agents_missing_tenant_bootstrap_password$;
\endif

CREATE TEMP TABLE cloud_agents_compose_inputs (
    database_name text NOT NULL,
    migration_password text NOT NULL,
    runtime_password text NOT NULL,
    tenant_bootstrap_password text NOT NULL
) ON COMMIT DROP;
REVOKE ALL ON TABLE cloud_agents_compose_inputs FROM PUBLIC;
INSERT INTO cloud_agents_compose_inputs VALUES (
    :'cloud_agents_database',
    :'cloud_agents_migration_password',
    :'cloud_agents_runtime_password',
    :'cloud_agents_tenant_bootstrap_password'
);

DO $cloud_agents_compose_provision$
DECLARE
    compose_input record;
BEGIN
    SELECT * INTO STRICT compose_input FROM cloud_agents_compose_inputs;
    IF compose_input.database_name IS DISTINCT FROM pg_catalog.current_database() THEN
        RAISE EXCEPTION USING ERRCODE = '22023', MESSAGE = 'connected database does not match cloud_agents_database';
    END IF;
    IF pg_catalog.octet_length(compose_input.migration_password) NOT BETWEEN 16 AND 1024
        OR pg_catalog.octet_length(compose_input.runtime_password) NOT BETWEEN 16 AND 1024
        OR pg_catalog.octet_length(compose_input.tenant_bootstrap_password) NOT BETWEEN 16 AND 1024
        OR compose_input.migration_password ~ '[[:cntrl:]]'
        OR compose_input.runtime_password ~ '[[:cntrl:]]'
        OR compose_input.tenant_bootstrap_password ~ '[[:cntrl:]]'
    THEN
        RAISE EXCEPTION USING ERRCODE = '22023', MESSAGE = 'Compose database credentials are invalid';
    END IF;

    IF NOT EXISTS (SELECT 1 FROM pg_catalog.pg_roles WHERE rolname = 'cloud_agents_database_owner') THEN
        CREATE ROLE cloud_agents_database_owner NOLOGIN NOSUPERUSER NOINHERIT NOCREATEDB NOCREATEROLE NOREPLICATION NOBYPASSRLS;
    END IF;
    IF NOT EXISTS (SELECT 1 FROM pg_catalog.pg_roles WHERE rolname = 'cloud_agents_migration') THEN
        CREATE ROLE cloud_agents_migration LOGIN NOSUPERUSER NOINHERIT NOCREATEDB NOCREATEROLE NOREPLICATION NOBYPASSRLS;
    END IF;
    IF NOT EXISTS (SELECT 1 FROM pg_catalog.pg_roles WHERE rolname = 'cloud_agents_runtime_login') THEN
        CREATE ROLE cloud_agents_runtime_login LOGIN NOSUPERUSER INHERIT NOCREATEDB NOCREATEROLE NOREPLICATION NOBYPASSRLS;
    END IF;
    IF NOT EXISTS (SELECT 1 FROM pg_catalog.pg_roles WHERE rolname = 'cloud_agents_tenant_bootstrap') THEN
        CREATE ROLE cloud_agents_tenant_bootstrap LOGIN NOSUPERUSER INHERIT NOCREATEDB NOCREATEROLE NOREPLICATION NOBYPASSRLS;
    END IF;

    EXECUTE pg_catalog.format(
        'ALTER ROLE cloud_agents_migration PASSWORD %L VALID UNTIL ''infinity''',
        compose_input.migration_password
    );
    EXECUTE pg_catalog.format(
        'ALTER ROLE cloud_agents_runtime_login PASSWORD %L VALID UNTIL ''infinity''',
        compose_input.runtime_password
    );
    EXECUTE pg_catalog.format(
        'ALTER ROLE cloud_agents_tenant_bootstrap PASSWORD %L VALID UNTIL ''infinity''',
        compose_input.tenant_bootstrap_password
    );

    IF pg_catalog.current_setting('server_version_num')::integer >= 160000 THEN
        EXECUTE 'GRANT cloud_agents_migration_owner TO cloud_agents_migration WITH ADMIN FALSE, INHERIT FALSE, SET TRUE';
        EXECUTE 'GRANT cloud_agents_runtime TO cloud_agents_runtime_login WITH ADMIN FALSE, INHERIT TRUE, SET TRUE';
        EXECUTE 'GRANT cloud_agents_bootstrap_admin TO cloud_agents_tenant_bootstrap WITH ADMIN FALSE, INHERIT TRUE, SET TRUE';
    ELSE
        GRANT cloud_agents_migration_owner TO cloud_agents_migration;
        GRANT cloud_agents_runtime TO cloud_agents_runtime_login;
        GRANT cloud_agents_bootstrap_admin TO cloud_agents_tenant_bootstrap;
    END IF;

    EXECUTE pg_catalog.format(
        'ALTER DATABASE %I OWNER TO cloud_agents_database_owner',
        compose_input.database_name
    );
END
$cloud_agents_compose_provision$;
