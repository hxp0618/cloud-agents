ALTER TABLE cloud_agents.managed_host_environment_leases
    ADD COLUMN worker_server_name text NOT NULL DEFAULT '';
ALTER TABLE cloud_agents.managed_host_environment_leases
    ADD CONSTRAINT managed_host_leases_worker_server_name CHECK (
        worker_server_name = '' OR (
            pg_catalog.octet_length(worker_server_name) BETWEEN 1 AND 253
            AND worker_server_name = pg_catalog.btrim(worker_server_name)
            AND worker_server_name !~ '[/@[:cntrl:]]'
        )
    );

ALTER TABLE cloud_agents.managed_agent_sessions
    ADD COLUMN environment_lease_uid text;
ALTER TABLE cloud_agents.managed_agent_sessions
    ADD COLUMN environment_generation bigint;
ALTER TABLE cloud_agents.managed_agent_sessions
    ADD CONSTRAINT managed_agent_sessions_environment_pair CHECK (
        (environment_lease_uid IS NULL) = (environment_generation IS NULL)
        AND (environment_lease_uid IS NULL OR (
            cloud_agents.is_valid_identifier(environment_lease_uid)
            AND environment_generation > 0
        ))
    );
ALTER TABLE cloud_agents.managed_agent_sessions
    ADD CONSTRAINT managed_agent_sessions_environment_fk
    FOREIGN KEY (tenant_id, project_uid, environment_lease_uid)
    REFERENCES cloud_agents.managed_host_environment_leases (tenant_id, project_uid, lease_uid)
    ON UPDATE RESTRICT ON DELETE RESTRICT;

CREATE FUNCTION cloud_agents.complete_managed_host_environment_lease_deployment_v2(
    p_tenant_id text, p_project_uid text, p_lease_uid text, p_expected_generation bigint,
    p_target_uid text, p_expected_target_generation bigint, p_succeeded boolean,
    p_worker_endpoint text, p_worker_spiffe_id text, p_worker_server_name text, p_stable_error_code text
)
RETURNS TABLE (
    lease_uid text, lease_name text, release_digest text,
    deployment_target_uid text, deployment_target_generation bigint,
    provider_credential_ref text, cpu_limit_millis bigint, memory_limit_bytes bigint,
    generation bigint, desired_phase text, observed_phase text, cleanup_phase text, environment_id text,
    worker_endpoint text, worker_spiffe_id text, worker_server_name text, stable_error_code text,
    expires_at timestamptz, resource_version bigint, created_at timestamptz, updated_at timestamptz
)
LANGUAGE plpgsql SECURITY DEFINER SET search_path = pg_catalog, cloud_agents
AS $cloud_agents_function$
DECLARE
    existing cloud_agents.managed_host_environment_leases%ROWTYPE;
BEGIN
    IF p_worker_server_name IS NULL OR (p_succeeded AND (
            pg_catalog.octet_length(p_worker_server_name) NOT BETWEEN 1 AND 253
            OR p_worker_server_name IS DISTINCT FROM pg_catalog.btrim(p_worker_server_name)
            OR p_worker_server_name ~ '[/@[:cntrl:]]'
        ))
        OR (NOT p_succeeded AND p_worker_server_name <> '')
    THEN
        RAISE EXCEPTION USING ERRCODE = '22023', MESSAGE = 'managed host environment lease Worker server name is invalid';
    END IF;

    PERFORM * FROM cloud_agents.complete_managed_host_environment_lease_deployment_v1(
        p_tenant_id, p_project_uid, p_lease_uid, p_expected_generation,
        p_target_uid, p_expected_target_generation, p_succeeded,
        p_worker_endpoint, p_worker_spiffe_id, p_stable_error_code
    );

    SELECT lease.* INTO existing
    FROM cloud_agents.managed_host_environment_leases AS lease
    WHERE lease.tenant_id = p_tenant_id AND lease.project_uid = p_project_uid AND lease.lease_uid = p_lease_uid
    FOR UPDATE;
    IF NOT FOUND THEN RETURN; END IF;
    IF existing.worker_server_name NOT IN ('', p_worker_server_name) THEN
        RAISE EXCEPTION USING ERRCODE = '23505', MESSAGE = 'managed host environment lease Worker server name conflicts';
    END IF;
    IF existing.worker_server_name = '' AND p_worker_server_name <> '' THEN
        UPDATE cloud_agents.managed_host_environment_leases AS lease
        SET worker_server_name = p_worker_server_name
        WHERE lease.tenant_id = p_tenant_id AND lease.project_uid = p_project_uid AND lease.lease_uid = p_lease_uid
        RETURNING lease.* INTO existing;
    END IF;

    lease_uid := existing.lease_uid; lease_name := existing.lease_name; release_digest := existing.release_digest;
    deployment_target_uid := existing.deployment_target_uid; deployment_target_generation := existing.deployment_target_generation;
    provider_credential_ref := existing.provider_credential_ref; cpu_limit_millis := existing.cpu_limit_millis;
    memory_limit_bytes := existing.memory_limit_bytes; generation := existing.generation;
    desired_phase := existing.desired_phase; observed_phase := existing.observed_phase; cleanup_phase := existing.cleanup_phase;
    environment_id := existing.environment_id; worker_endpoint := existing.worker_endpoint;
    worker_spiffe_id := existing.worker_spiffe_id; worker_server_name := existing.worker_server_name;
    stable_error_code := existing.stable_error_code; expires_at := existing.expires_at;
    resource_version := existing.resource_version; created_at := existing.created_at; updated_at := existing.updated_at;
    RETURN NEXT;
END;
$cloud_agents_function$;

CREATE FUNCTION cloud_agents.create_managed_agent_session_v2(
    p_tenant_id text,
    p_project_uid text,
    p_session_uid text,
    p_provider_kind text,
    p_environment_lease_uid text,
    p_idempotency_key text,
    p_request_digest text
)
RETURNS TABLE (
    session_uid text,
    provider_kind text,
    environment_lease_uid text,
    environment_generation bigint,
    state text,
    resource_version bigint,
    created_at timestamptz,
    updated_at timestamptz
)
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, cloud_agents
AS $cloud_agents_function$
DECLARE
    ignored_principal text;
    mutation_at timestamptz;
    existing cloud_agents.managed_agent_sessions%ROWTYPE;
    environment cloud_agents.managed_host_environment_leases%ROWTYPE;
BEGIN
    ignored_principal := cloud_agents.require_runtime_mutation_principal();
    mutation_at := pg_catalog.transaction_timestamp();
    IF p_tenant_id IS DISTINCT FROM cloud_agents.require_tenant_id()
        OR NOT cloud_agents.is_valid_identifier(p_project_uid)
        OR NOT cloud_agents.is_valid_identifier(p_session_uid)
        OR NOT cloud_agents.is_valid_identifier(p_provider_kind)
        OR NOT cloud_agents.is_valid_identifier(p_environment_lease_uid)
        OR p_idempotency_key !~ '^[A-Za-z0-9._~-]{16,128}$'
        OR p_request_digest !~ '^sha256:[0-9a-f]{64}$'
    THEN
        RAISE EXCEPTION USING ERRCODE = '22023', MESSAGE = 'managed agent session input is invalid';
    END IF;

    SELECT session.* INTO existing
    FROM cloud_agents.managed_agent_sessions AS session
    WHERE session.tenant_id = p_tenant_id
        AND session.project_uid = p_project_uid
        AND session.create_idempotency_key = p_idempotency_key
    FOR UPDATE;
    IF FOUND THEN
        IF existing.create_request_digest IS DISTINCT FROM p_request_digest
            OR existing.environment_lease_uid IS DISTINCT FROM p_environment_lease_uid
        THEN
            RAISE EXCEPTION USING ERRCODE = '23505', MESSAGE = 'managed agent session idempotency conflict';
        END IF;
        session_uid := existing.session_uid; provider_kind := existing.provider_kind;
        environment_lease_uid := existing.environment_lease_uid; environment_generation := existing.environment_generation;
        state := existing.state; resource_version := existing.resource_version;
        created_at := existing.created_at; updated_at := existing.updated_at;
        RETURN NEXT;
        RETURN;
    END IF;

    SELECT lease.* INTO environment
    FROM cloud_agents.managed_host_environment_leases AS lease
    WHERE lease.tenant_id = p_tenant_id
        AND lease.project_uid = p_project_uid
        AND lease.lease_uid = p_environment_lease_uid
        AND lease.desired_phase = 'active'
        AND lease.observed_phase = 'ready'
        AND lease.cleanup_phase = 'none'
        AND lease.expires_at > mutation_at
        AND lease.worker_endpoint <> ''
        AND lease.worker_spiffe_id <> ''
        AND lease.worker_server_name <> ''
    FOR SHARE;
    IF NOT FOUND THEN
        RAISE EXCEPTION USING ERRCODE = '23505', MESSAGE = 'managed agent Session environment is not ready';
    END IF;

    INSERT INTO cloud_agents.managed_agent_sessions (
        tenant_id, tenant_ref_id, project_uid, session_uid, provider_kind,
        environment_lease_uid, environment_generation,
        state, resource_version, create_idempotency_key, create_request_digest,
        created_at, updated_at
    ) VALUES (
        p_tenant_id, p_tenant_id, p_project_uid, p_session_uid, p_provider_kind,
        environment.lease_uid, environment.generation,
        'active', 1, p_idempotency_key, p_request_digest, mutation_at, mutation_at
    ) RETURNING managed_agent_sessions.* INTO existing;

    session_uid := existing.session_uid; provider_kind := existing.provider_kind;
    environment_lease_uid := existing.environment_lease_uid; environment_generation := existing.environment_generation;
    state := existing.state; resource_version := existing.resource_version;
    created_at := existing.created_at; updated_at := existing.updated_at;
    RETURN NEXT;
END;
$cloud_agents_function$;

ALTER FUNCTION cloud_agents.complete_managed_host_environment_lease_deployment_v2(
    text, text, text, bigint, text, bigint, boolean, text, text, text, text
) OWNER TO cloud_agents_migration_owner;
ALTER FUNCTION cloud_agents.create_managed_agent_session_v2(text, text, text, text, text, text, text)
    OWNER TO cloud_agents_migration_owner;
REVOKE ALL ON FUNCTION cloud_agents.complete_managed_host_environment_lease_deployment_v2(
    text, text, text, bigint, text, bigint, boolean, text, text, text, text
) FROM PUBLIC;
REVOKE ALL ON FUNCTION cloud_agents.create_managed_agent_session_v2(text, text, text, text, text, text, text) FROM PUBLIC;
REVOKE EXECUTE ON FUNCTION cloud_agents.complete_managed_host_environment_lease_deployment_v1(
    text, text, text, bigint, text, bigint, boolean, text, text, text
) FROM cloud_agents_runtime;
REVOKE EXECUTE ON FUNCTION cloud_agents.create_managed_agent_session_v1(text, text, text, text, text, text)
    FROM cloud_agents_runtime;
GRANT EXECUTE ON FUNCTION cloud_agents.complete_managed_host_environment_lease_deployment_v2(
    text, text, text, bigint, text, bigint, boolean, text, text, text, text
) TO cloud_agents_runtime;
GRANT EXECUTE ON FUNCTION cloud_agents.create_managed_agent_session_v2(text, text, text, text, text, text, text)
    TO cloud_agents_runtime;
