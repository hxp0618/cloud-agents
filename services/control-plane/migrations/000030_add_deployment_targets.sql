CREATE TABLE cloud_agents.deployment_targets (
    tenant_id text NOT NULL,
    tenant_ref_id text NOT NULL,
    project_uid text NOT NULL,
    target_uid text NOT NULL,
    target_name text NOT NULL,
    target_kind text NOT NULL,
    endpoint text NOT NULL,
    credential_ref text NOT NULL,
    generation bigint NOT NULL,
    observed_phase text NOT NULL,
    api_version text NOT NULL,
    engine_version text NOT NULL,
    target_os text NOT NULL,
    target_arch text NOT NULL,
    stable_error_code text NOT NULL,
    last_probe_at timestamptz,
    resource_version bigint NOT NULL,
    create_idempotency_key text NOT NULL,
    create_request_digest text NOT NULL,
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL,
    PRIMARY KEY (tenant_id, project_uid, target_uid),
    UNIQUE (tenant_id, project_uid, create_idempotency_key),
    CONSTRAINT deployment_targets_tenant_ref CHECK (tenant_id = tenant_ref_id),
    CONSTRAINT deployment_targets_project_fk FOREIGN KEY (tenant_id, project_uid)
        REFERENCES cloud_agents.projects (tenant_id, project_uid) ON UPDATE RESTRICT ON DELETE RESTRICT,
    CONSTRAINT deployment_targets_tenant_fk FOREIGN KEY (tenant_id, tenant_ref_id)
        REFERENCES cloud_agents.platform_tenants (tenant_id, tenant_uid) ON UPDATE RESTRICT ON DELETE RESTRICT,
    CONSTRAINT deployment_targets_target_uid CHECK (cloud_agents.is_valid_identifier(target_uid)),
    CONSTRAINT deployment_targets_target_name CHECK (cloud_agents.is_valid_identifier(target_name)),
    CONSTRAINT deployment_targets_kind CHECK (target_kind = 'docker'),
    CONSTRAINT deployment_targets_endpoint CHECK (
        pg_catalog.octet_length(endpoint) BETWEEN 9 AND 2048
        AND endpoint ~ '^https://[^/?#[:space:]@]+/?$'
    ),
    CONSTRAINT deployment_targets_credential_ref CHECK (cloud_agents.is_valid_identifier(credential_ref)),
    CONSTRAINT deployment_targets_generation CHECK (generation > 0),
    CONSTRAINT deployment_targets_observed_phase CHECK (observed_phase IN ('unprobed', 'probing', 'ready', 'unavailable')),
    CONSTRAINT deployment_targets_probe_facts CHECK (
        (observed_phase IN ('unprobed', 'probing') AND api_version = '' AND engine_version = '' AND target_os = '' AND target_arch = '' AND stable_error_code = '')
        OR (observed_phase = 'ready' AND api_version <> '' AND engine_version <> '' AND target_os <> '' AND target_arch <> '' AND stable_error_code = '' AND last_probe_at IS NOT NULL)
        OR (observed_phase = 'unavailable' AND api_version = '' AND engine_version = '' AND target_os = '' AND target_arch = '' AND cloud_agents.is_valid_identifier(stable_error_code) AND last_probe_at IS NOT NULL)
    ),
    CONSTRAINT deployment_targets_probe_fact_bounds CHECK (
        pg_catalog.octet_length(api_version) <= 128 AND pg_catalog.octet_length(engine_version) <= 128
        AND pg_catalog.octet_length(target_os) <= 128 AND pg_catalog.octet_length(target_arch) <= 128
    ),
    CONSTRAINT deployment_targets_resource_version CHECK (resource_version > 0),
    CONSTRAINT deployment_targets_create_key CHECK (create_idempotency_key ~ '^[A-Za-z0-9._~-]{16,128}$'),
    CONSTRAINT deployment_targets_create_digest CHECK (create_request_digest ~ '^sha256:[0-9a-f]{64}$')
);

CREATE TABLE cloud_agents.deployment_target_probe_operations (
    tenant_id text NOT NULL,
    project_uid text NOT NULL,
    target_uid text NOT NULL,
    idempotency_key text NOT NULL,
    request_digest text NOT NULL,
    generation bigint NOT NULL,
    phase text NOT NULL,
    api_version text NOT NULL,
    engine_version text NOT NULL,
    target_os text NOT NULL,
    target_arch text NOT NULL,
    stable_error_code text NOT NULL,
    started_at timestamptz NOT NULL,
    completed_at timestamptz,
    PRIMARY KEY (tenant_id, project_uid, target_uid, idempotency_key),
    CONSTRAINT deployment_target_probes_target_fk FOREIGN KEY (tenant_id, project_uid, target_uid)
        REFERENCES cloud_agents.deployment_targets (tenant_id, project_uid, target_uid) ON UPDATE RESTRICT ON DELETE RESTRICT,
    CONSTRAINT deployment_target_probes_key CHECK (idempotency_key ~ '^[A-Za-z0-9._~-]{16,128}$'),
    CONSTRAINT deployment_target_probes_digest CHECK (request_digest ~ '^sha256:[0-9a-f]{64}$'),
    CONSTRAINT deployment_target_probes_generation CHECK (generation > 0),
    CONSTRAINT deployment_target_probes_phase CHECK (phase IN ('running', 'succeeded', 'failed')),
    CONSTRAINT deployment_target_probes_terminal CHECK (
        (phase = 'running' AND completed_at IS NULL AND api_version = '' AND engine_version = '' AND target_os = '' AND target_arch = '' AND stable_error_code = '')
        OR (phase = 'succeeded' AND completed_at IS NOT NULL AND api_version <> '' AND engine_version <> '' AND target_os <> '' AND target_arch <> '' AND stable_error_code = '')
        OR (phase = 'failed' AND completed_at IS NOT NULL AND api_version = '' AND engine_version = '' AND target_os = '' AND target_arch = '' AND cloud_agents.is_valid_identifier(stable_error_code))
    )
);

ALTER TABLE cloud_agents.deployment_targets OWNER TO cloud_agents_migration_owner;
ALTER TABLE cloud_agents.deployment_target_probe_operations OWNER TO cloud_agents_migration_owner;
ALTER TABLE cloud_agents.deployment_targets ENABLE ROW LEVEL SECURITY;
ALTER TABLE cloud_agents.deployment_targets FORCE ROW LEVEL SECURITY;
ALTER TABLE cloud_agents.deployment_target_probe_operations ENABLE ROW LEVEL SECURITY;
ALTER TABLE cloud_agents.deployment_target_probe_operations FORCE ROW LEVEL SECURITY;
CREATE POLICY deployment_targets_runtime_tenant ON cloud_agents.deployment_targets
    TO cloud_agents_runtime USING (tenant_id = cloud_agents.require_tenant_id())
    WITH CHECK (tenant_id = cloud_agents.require_tenant_id());
CREATE POLICY deployment_targets_migration_owner ON cloud_agents.deployment_targets
    TO cloud_agents_migration_owner USING (true) WITH CHECK (true);
CREATE POLICY deployment_target_probe_operations_runtime_tenant ON cloud_agents.deployment_target_probe_operations
    TO cloud_agents_runtime USING (tenant_id = cloud_agents.require_tenant_id())
    WITH CHECK (tenant_id = cloud_agents.require_tenant_id());
CREATE POLICY deployment_target_probe_operations_migration_owner ON cloud_agents.deployment_target_probe_operations
    TO cloud_agents_migration_owner USING (true) WITH CHECK (true);
REVOKE ALL ON TABLE cloud_agents.deployment_targets FROM PUBLIC;
REVOKE ALL ON TABLE cloud_agents.deployment_target_probe_operations FROM PUBLIC;
GRANT SELECT ON TABLE cloud_agents.deployment_targets TO cloud_agents_runtime;

CREATE FUNCTION cloud_agents.register_deployment_target_v1(
    p_tenant_id text, p_project_uid text, p_target_uid text, p_target_name text,
    p_target_kind text, p_endpoint text, p_credential_ref text,
    p_idempotency_key text, p_request_digest text
)
RETURNS TABLE (
    target_uid text, target_name text, target_kind text, endpoint text, credential_ref text,
    generation bigint, observed_phase text, api_version text, engine_version text,
    target_os text, target_arch text, stable_error_code text, last_probe_at timestamptz,
    resource_version bigint, created_at timestamptz, updated_at timestamptz
)
LANGUAGE plpgsql SECURITY DEFINER SET search_path = pg_catalog, cloud_agents
AS $cloud_agents_function$
DECLARE
    ignored_principal text;
    mutation_at timestamptz;
    existing cloud_agents.deployment_targets%ROWTYPE;
BEGIN
    ignored_principal := cloud_agents.require_runtime_mutation_principal();
    mutation_at := pg_catalog.transaction_timestamp();
    IF p_tenant_id IS DISTINCT FROM cloud_agents.require_tenant_id()
        OR NOT cloud_agents.is_valid_identifier(p_project_uid)
        OR NOT cloud_agents.is_valid_identifier(p_target_uid)
        OR NOT cloud_agents.is_valid_identifier(p_target_name)
        OR p_target_kind <> 'docker'
        OR pg_catalog.octet_length(p_endpoint) NOT BETWEEN 9 AND 2048
        OR p_endpoint !~ '^https://[^/?#[:space:]@]+/?$'
        OR NOT cloud_agents.is_valid_identifier(p_credential_ref)
        OR p_idempotency_key !~ '^[A-Za-z0-9._~-]{16,128}$'
        OR p_request_digest !~ '^sha256:[0-9a-f]{64}$'
    THEN RAISE EXCEPTION USING ERRCODE = '22023', MESSAGE = 'deployment target input is invalid'; END IF;
    SELECT target.* INTO existing FROM cloud_agents.deployment_targets AS target
    WHERE target.tenant_id = p_tenant_id AND target.project_uid = p_project_uid
        AND target.create_idempotency_key = p_idempotency_key FOR UPDATE;
    IF FOUND THEN
        IF existing.create_request_digest IS DISTINCT FROM p_request_digest THEN
            RAISE EXCEPTION USING ERRCODE = '23505', MESSAGE = 'deployment target idempotency conflict';
        END IF;
    ELSE
        PERFORM 1 FROM cloud_agents.projects AS project
        WHERE project.tenant_id = p_tenant_id AND project.project_uid = p_project_uid AND project.state = 'active'
        FOR KEY SHARE;
        IF NOT FOUND THEN RAISE EXCEPTION USING ERRCODE = '23503', MESSAGE = 'project is absent or inactive'; END IF;
        INSERT INTO cloud_agents.deployment_targets (
            tenant_id, tenant_ref_id, project_uid, target_uid, target_name, target_kind, endpoint, credential_ref,
            generation, observed_phase, api_version, engine_version, target_os, target_arch, stable_error_code,
            resource_version, create_idempotency_key, create_request_digest, created_at, updated_at
        ) VALUES (
            p_tenant_id, p_tenant_id, p_project_uid, p_target_uid, p_target_name, p_target_kind, p_endpoint, p_credential_ref,
            1, 'unprobed', '', '', '', '', '', 1, p_idempotency_key, p_request_digest, mutation_at, mutation_at
        ) RETURNING deployment_targets.* INTO existing;
    END IF;
    target_uid := existing.target_uid; target_name := existing.target_name; target_kind := existing.target_kind;
    endpoint := existing.endpoint; credential_ref := existing.credential_ref; generation := existing.generation;
    observed_phase := existing.observed_phase; api_version := existing.api_version; engine_version := existing.engine_version;
    target_os := existing.target_os; target_arch := existing.target_arch; stable_error_code := existing.stable_error_code;
    last_probe_at := existing.last_probe_at; resource_version := existing.resource_version;
    created_at := existing.created_at; updated_at := existing.updated_at;
    RETURN NEXT;
END;
$cloud_agents_function$;

CREATE FUNCTION cloud_agents.begin_deployment_target_probe_v1(
    p_tenant_id text, p_project_uid text, p_target_uid text, p_expected_generation bigint,
    p_idempotency_key text, p_request_digest text
)
RETURNS TABLE (
    target_uid text, target_name text, target_kind text, endpoint text, credential_ref text,
    generation bigint, observed_phase text, api_version text, engine_version text,
    target_os text, target_arch text, stable_error_code text, last_probe_at timestamptz,
    resource_version bigint, created_at timestamptz, updated_at timestamptz, execute_probe boolean
)
LANGUAGE plpgsql SECURITY DEFINER SET search_path = pg_catalog, cloud_agents
AS $cloud_agents_function$
DECLARE
    ignored_principal text;
    mutation_at timestamptz;
    existing cloud_agents.deployment_targets%ROWTYPE;
    operation cloud_agents.deployment_target_probe_operations%ROWTYPE;
BEGIN
    ignored_principal := cloud_agents.require_runtime_mutation_principal();
    mutation_at := pg_catalog.transaction_timestamp();
    IF p_tenant_id IS DISTINCT FROM cloud_agents.require_tenant_id()
        OR NOT cloud_agents.is_valid_identifier(p_project_uid)
        OR NOT cloud_agents.is_valid_identifier(p_target_uid)
        OR p_expected_generation < 1 OR p_idempotency_key !~ '^[A-Za-z0-9._~-]{16,128}$'
        OR p_request_digest !~ '^sha256:[0-9a-f]{64}$'
    THEN RAISE EXCEPTION USING ERRCODE = '22023', MESSAGE = 'deployment target probe input is invalid'; END IF;
    SELECT target.* INTO existing FROM cloud_agents.deployment_targets AS target
    WHERE target.tenant_id = p_tenant_id AND target.project_uid = p_project_uid AND target.target_uid = p_target_uid FOR UPDATE;
    IF NOT FOUND THEN RETURN; END IF;
    IF existing.generation <> p_expected_generation THEN
        RAISE EXCEPTION USING ERRCODE = '23505', MESSAGE = 'deployment target generation conflict';
    END IF;
    SELECT probe.* INTO operation FROM cloud_agents.deployment_target_probe_operations AS probe
    WHERE probe.tenant_id = p_tenant_id AND probe.project_uid = p_project_uid AND probe.target_uid = p_target_uid
        AND probe.idempotency_key = p_idempotency_key;
    IF FOUND THEN
        IF operation.request_digest IS DISTINCT FROM p_request_digest OR operation.generation <> p_expected_generation THEN
            RAISE EXCEPTION USING ERRCODE = '23505', MESSAGE = 'deployment target probe idempotency conflict';
        END IF;
        execute_probe := operation.phase = 'running';
    ELSE
        PERFORM 1 FROM cloud_agents.deployment_target_probe_operations AS probe
        WHERE probe.tenant_id = p_tenant_id AND probe.project_uid = p_project_uid AND probe.target_uid = p_target_uid
            AND probe.phase = 'running';
        IF FOUND THEN RAISE EXCEPTION USING ERRCODE = '23505', MESSAGE = 'deployment target probe is already running'; END IF;
        INSERT INTO cloud_agents.deployment_target_probe_operations (
            tenant_id, project_uid, target_uid, idempotency_key, request_digest, generation, phase,
            api_version, engine_version, target_os, target_arch, stable_error_code, started_at
        ) VALUES (p_tenant_id, p_project_uid, p_target_uid, p_idempotency_key, p_request_digest, p_expected_generation,
            'running', '', '', '', '', '', mutation_at);
        UPDATE cloud_agents.deployment_targets AS target SET observed_phase = 'probing', api_version = '', engine_version = '',
            target_os = '', target_arch = '', stable_error_code = '', resource_version = existing.resource_version + 1,
            updated_at = mutation_at
        WHERE target.tenant_id = p_tenant_id AND target.project_uid = p_project_uid AND target.target_uid = p_target_uid
        RETURNING target.* INTO existing;
        execute_probe := true;
    END IF;
    target_uid := existing.target_uid; target_name := existing.target_name; target_kind := existing.target_kind;
    endpoint := existing.endpoint; credential_ref := existing.credential_ref; generation := existing.generation;
    observed_phase := existing.observed_phase; api_version := existing.api_version; engine_version := existing.engine_version;
    target_os := existing.target_os; target_arch := existing.target_arch; stable_error_code := existing.stable_error_code;
    last_probe_at := existing.last_probe_at; resource_version := existing.resource_version;
    created_at := existing.created_at; updated_at := existing.updated_at;
    RETURN NEXT;
END;
$cloud_agents_function$;

CREATE FUNCTION cloud_agents.complete_deployment_target_probe_v1(
    p_tenant_id text, p_project_uid text, p_target_uid text, p_expected_generation bigint,
    p_idempotency_key text, p_request_digest text, p_succeeded boolean,
    p_api_version text, p_engine_version text, p_target_os text, p_target_arch text, p_stable_error_code text
)
RETURNS TABLE (
    target_uid text, target_name text, target_kind text, endpoint text, credential_ref text,
    generation bigint, observed_phase text, api_version text, engine_version text,
    target_os text, target_arch text, stable_error_code text, last_probe_at timestamptz,
    resource_version bigint, created_at timestamptz, updated_at timestamptz
)
LANGUAGE plpgsql SECURITY DEFINER SET search_path = pg_catalog, cloud_agents
AS $cloud_agents_function$
DECLARE
    ignored_principal text;
    mutation_at timestamptz;
    existing cloud_agents.deployment_targets%ROWTYPE;
    operation cloud_agents.deployment_target_probe_operations%ROWTYPE;
BEGIN
    ignored_principal := cloud_agents.require_runtime_mutation_principal();
    mutation_at := pg_catalog.transaction_timestamp();
    IF p_tenant_id IS DISTINCT FROM cloud_agents.require_tenant_id()
        OR NOT cloud_agents.is_valid_identifier(p_project_uid) OR NOT cloud_agents.is_valid_identifier(p_target_uid)
        OR p_expected_generation < 1 OR p_idempotency_key !~ '^[A-Za-z0-9._~-]{16,128}$'
        OR p_request_digest !~ '^sha256:[0-9a-f]{64}$'
        OR (p_succeeded AND (p_api_version = '' OR p_engine_version = '' OR p_target_os = '' OR p_target_arch = '' OR p_stable_error_code <> ''))
        OR (NOT p_succeeded AND (p_api_version <> '' OR p_engine_version <> '' OR p_target_os <> '' OR p_target_arch <> '' OR NOT cloud_agents.is_valid_identifier(p_stable_error_code)))
        OR pg_catalog.octet_length(p_api_version) > 128 OR pg_catalog.octet_length(p_engine_version) > 128
        OR pg_catalog.octet_length(p_target_os) > 128 OR pg_catalog.octet_length(p_target_arch) > 128
    THEN RAISE EXCEPTION USING ERRCODE = '22023', MESSAGE = 'deployment target probe completion is invalid'; END IF;
    SELECT target.* INTO existing FROM cloud_agents.deployment_targets AS target
    WHERE target.tenant_id = p_tenant_id AND target.project_uid = p_project_uid AND target.target_uid = p_target_uid FOR UPDATE;
    IF NOT FOUND THEN RETURN; END IF;
    IF existing.generation <> p_expected_generation THEN
        RAISE EXCEPTION USING ERRCODE = '23505', MESSAGE = 'deployment target generation conflict';
    END IF;
    SELECT probe.* INTO operation FROM cloud_agents.deployment_target_probe_operations AS probe
    WHERE probe.tenant_id = p_tenant_id AND probe.project_uid = p_project_uid AND probe.target_uid = p_target_uid
        AND probe.idempotency_key = p_idempotency_key FOR UPDATE;
    IF NOT FOUND THEN RETURN; END IF;
    IF operation.request_digest IS DISTINCT FROM p_request_digest OR operation.generation <> p_expected_generation THEN
        RAISE EXCEPTION USING ERRCODE = '23505', MESSAGE = 'deployment target probe idempotency conflict';
    END IF;
    IF operation.phase = 'running' THEN
        UPDATE cloud_agents.deployment_target_probe_operations AS probe SET
            phase = CASE WHEN p_succeeded THEN 'succeeded' ELSE 'failed' END,
            api_version = p_api_version, engine_version = p_engine_version, target_os = p_target_os,
            target_arch = p_target_arch, stable_error_code = p_stable_error_code, completed_at = mutation_at
        WHERE probe.tenant_id = p_tenant_id AND probe.project_uid = p_project_uid AND probe.target_uid = p_target_uid
            AND probe.idempotency_key = p_idempotency_key;
        UPDATE cloud_agents.deployment_targets AS target SET
            observed_phase = CASE WHEN p_succeeded THEN 'ready' ELSE 'unavailable' END,
            api_version = p_api_version, engine_version = p_engine_version, target_os = p_target_os,
            target_arch = p_target_arch, stable_error_code = p_stable_error_code, last_probe_at = mutation_at,
            resource_version = existing.resource_version + 1, updated_at = mutation_at
        WHERE target.tenant_id = p_tenant_id AND target.project_uid = p_project_uid AND target.target_uid = p_target_uid
        RETURNING target.* INTO existing;
    END IF;
    target_uid := existing.target_uid; target_name := existing.target_name; target_kind := existing.target_kind;
    endpoint := existing.endpoint; credential_ref := existing.credential_ref; generation := existing.generation;
    observed_phase := existing.observed_phase; api_version := existing.api_version; engine_version := existing.engine_version;
    target_os := existing.target_os; target_arch := existing.target_arch; stable_error_code := existing.stable_error_code;
    last_probe_at := existing.last_probe_at; resource_version := existing.resource_version;
    created_at := existing.created_at; updated_at := existing.updated_at;
    RETURN NEXT;
END;
$cloud_agents_function$;

ALTER FUNCTION cloud_agents.register_deployment_target_v1(text, text, text, text, text, text, text, text, text) OWNER TO cloud_agents_migration_owner;
ALTER FUNCTION cloud_agents.begin_deployment_target_probe_v1(text, text, text, bigint, text, text) OWNER TO cloud_agents_migration_owner;
ALTER FUNCTION cloud_agents.complete_deployment_target_probe_v1(text, text, text, bigint, text, text, boolean, text, text, text, text, text) OWNER TO cloud_agents_migration_owner;
REVOKE ALL ON FUNCTION cloud_agents.register_deployment_target_v1(text, text, text, text, text, text, text, text, text) FROM PUBLIC;
REVOKE ALL ON FUNCTION cloud_agents.begin_deployment_target_probe_v1(text, text, text, bigint, text, text) FROM PUBLIC;
REVOKE ALL ON FUNCTION cloud_agents.complete_deployment_target_probe_v1(text, text, text, bigint, text, text, boolean, text, text, text, text, text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION cloud_agents.register_deployment_target_v1(text, text, text, text, text, text, text, text, text) TO cloud_agents_runtime;
GRANT EXECUTE ON FUNCTION cloud_agents.begin_deployment_target_probe_v1(text, text, text, bigint, text, text) TO cloud_agents_runtime;
GRANT EXECUTE ON FUNCTION cloud_agents.complete_deployment_target_probe_v1(text, text, text, bigint, text, text, boolean, text, text, text, text, text) TO cloud_agents_runtime;
