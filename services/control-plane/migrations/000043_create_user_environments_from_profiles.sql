ALTER TABLE cloud_agents.managed_host_environment_leases
    ADD COLUMN environment_profile_uid text;
ALTER TABLE cloud_agents.managed_host_environment_leases
    ADD COLUMN environment_profile_version bigint;
ALTER TABLE cloud_agents.managed_host_environment_leases
    ADD CONSTRAINT managed_host_leases_profile_pair CHECK (
        (environment_profile_uid IS NULL) = (environment_profile_version IS NULL)
    );
ALTER TABLE cloud_agents.managed_host_environment_leases
    ADD CONSTRAINT managed_host_leases_profile_uid CHECK (
        environment_profile_uid IS NULL OR cloud_agents.is_valid_identifier(environment_profile_uid)
    );
ALTER TABLE cloud_agents.managed_host_environment_leases
    ADD CONSTRAINT managed_host_leases_profile_version CHECK (
        environment_profile_version IS NULL OR environment_profile_version BETWEEN 1 AND 2147483647
    );
ALTER TABLE cloud_agents.managed_host_environment_leases
    ADD CONSTRAINT managed_host_leases_profile_fk
    FOREIGN KEY (tenant_id, project_uid, environment_profile_uid, environment_profile_version)
    REFERENCES cloud_agents.environment_profiles (tenant_id, project_uid, profile_uid, profile_version)
    ON UPDATE RESTRICT ON DELETE RESTRICT;

CREATE FUNCTION cloud_agents.create_user_environment_v1(
    p_tenant_id text, p_project_uid text, p_environment_uid text,
    p_profile_uid text, p_profile_version bigint, p_ttl_seconds bigint,
    p_idempotency_key text, p_request_digest text
)
RETURNS TABLE (
    lease_uid text, lease_name text, release_digest text,
    deployment_target_uid text, deployment_target_generation bigint,
    provider_credential_ref text, cpu_limit_millis bigint, memory_limit_bytes bigint,
    generation bigint, desired_phase text, observed_phase text, cleanup_phase text,
    environment_id text, worker_endpoint text, worker_spiffe_id text, worker_server_name text,
    stable_error_code text, expires_at timestamptz, resource_version bigint,
    created_at timestamptz, updated_at timestamptz,
    environment_profile_uid text, environment_profile_version bigint
)
LANGUAGE plpgsql SECURITY DEFINER SET search_path = pg_catalog, cloud_agents
AS $cloud_agents_function$
DECLARE
    ignored_principal text;
    existing cloud_agents.managed_host_environment_leases%ROWTYPE;
    selected_profile cloud_agents.environment_profiles%ROWTYPE;
    selected_target cloud_agents.deployment_targets%ROWTYPE;
BEGIN
    ignored_principal := cloud_agents.require_runtime_mutation_principal();
    IF p_tenant_id IS DISTINCT FROM cloud_agents.require_tenant_id()
        OR NOT cloud_agents.is_valid_identifier(p_project_uid)
        OR NOT cloud_agents.is_valid_identifier(p_environment_uid)
        OR NOT cloud_agents.is_valid_identifier(p_profile_uid)
        OR p_profile_version NOT BETWEEN 1 AND 2147483647
        OR p_ttl_seconds NOT BETWEEN 60 AND 86400
        OR p_idempotency_key !~ '^[A-Za-z0-9._~-]{16,128}$'
        OR p_request_digest !~ '^sha256:[0-9a-f]{64}$'
    THEN RAISE EXCEPTION USING ERRCODE = '22023', MESSAGE = 'user environment input is invalid'; END IF;

    SELECT lease.* INTO existing
    FROM cloud_agents.managed_host_environment_leases AS lease
    WHERE lease.tenant_id = p_tenant_id AND lease.project_uid = p_project_uid
        AND lease.create_idempotency_key = p_idempotency_key
    FOR UPDATE;
    IF FOUND THEN
        IF existing.create_request_digest IS DISTINCT FROM p_request_digest
            OR existing.environment_profile_uid IS DISTINCT FROM p_profile_uid
            OR existing.environment_profile_version IS DISTINCT FROM p_profile_version
        THEN RAISE EXCEPTION USING ERRCODE = '23505', MESSAGE = 'user environment idempotency conflict'; END IF;
    ELSE
        SELECT profile.* INTO selected_profile
        FROM cloud_agents.environment_profiles AS profile
        WHERE profile.tenant_id = p_tenant_id AND profile.project_uid = p_project_uid
            AND profile.profile_uid = p_profile_uid AND profile.profile_version = p_profile_version
            AND profile.status = 'published'
        FOR SHARE;
        IF NOT FOUND THEN
            RAISE EXCEPTION USING ERRCODE = '23505', MESSAGE = 'environment profile is not available';
        END IF;

        SELECT target.* INTO selected_target
        FROM cloud_agents.deployment_targets AS target
        WHERE target.tenant_id = p_tenant_id AND target.project_uid = p_project_uid
            AND target.target_uid = ANY(selected_profile.target_refs)
            AND target.observed_phase = 'ready'
        ORDER BY pg_catalog.array_position(selected_profile.target_refs, target.target_uid), target.target_uid
        LIMIT 1 FOR SHARE;
        IF NOT FOUND THEN
            RAISE EXCEPTION USING ERRCODE = '23505', MESSAGE = 'environment profile is not available';
        END IF;

        PERFORM * FROM cloud_agents.create_managed_host_environment_lease_v3(
            p_tenant_id, p_project_uid, p_environment_uid, p_environment_uid,
            selected_profile.release_digest, selected_target.target_uid, selected_target.generation,
            selected_profile.provider_credential_ref, selected_profile.cpu_limit_millis,
            selected_profile.memory_limit_bytes, p_ttl_seconds, p_idempotency_key, p_request_digest
        );
        UPDATE cloud_agents.managed_host_environment_leases AS lease
        SET environment_profile_uid = p_profile_uid,
            environment_profile_version = p_profile_version
        WHERE lease.tenant_id = p_tenant_id AND lease.project_uid = p_project_uid
            AND lease.lease_uid = p_environment_uid
        RETURNING lease.* INTO existing;
    END IF;

    lease_uid := existing.lease_uid; lease_name := existing.lease_name;
    release_digest := existing.release_digest;
    deployment_target_uid := existing.deployment_target_uid;
    deployment_target_generation := existing.deployment_target_generation;
    provider_credential_ref := existing.provider_credential_ref;
    cpu_limit_millis := existing.cpu_limit_millis; memory_limit_bytes := existing.memory_limit_bytes;
    generation := existing.generation; desired_phase := existing.desired_phase;
    observed_phase := existing.observed_phase; cleanup_phase := existing.cleanup_phase;
    environment_id := existing.environment_id; worker_endpoint := existing.worker_endpoint;
    worker_spiffe_id := existing.worker_spiffe_id; worker_server_name := existing.worker_server_name;
    stable_error_code := existing.stable_error_code; expires_at := existing.expires_at;
    resource_version := existing.resource_version; created_at := existing.created_at;
    updated_at := existing.updated_at; environment_profile_uid := existing.environment_profile_uid;
    environment_profile_version := existing.environment_profile_version;
    RETURN NEXT;
END;
$cloud_agents_function$;

CREATE FUNCTION cloud_agents.create_managed_agent_session_v3(
    p_tenant_id text, p_project_uid text, p_session_uid text, p_provider_kind text,
    p_environment_lease_uid text, p_idempotency_key text, p_request_digest text
)
RETURNS TABLE (
    session_uid text, provider_kind text, environment_lease_uid text,
    environment_generation bigint, environment_profile_uid text,
    environment_profile_version bigint, state text, resource_version bigint,
    created_at timestamptz, updated_at timestamptz
)
LANGUAGE plpgsql SECURITY DEFINER SET search_path = pg_catalog, cloud_agents
AS $cloud_agents_function$
DECLARE
    ignored_principal text;
    selected_environment cloud_agents.managed_host_environment_leases%ROWTYPE;
BEGIN
    ignored_principal := cloud_agents.require_runtime_mutation_principal();
    SELECT lease.* INTO selected_environment
    FROM cloud_agents.managed_host_environment_leases AS lease
    WHERE lease.tenant_id = p_tenant_id AND lease.project_uid = p_project_uid
        AND lease.lease_uid = p_environment_lease_uid
    FOR SHARE;
    IF FOUND AND selected_environment.environment_profile_uid IS NOT NULL
        AND NOT EXISTS (
            SELECT 1 FROM cloud_agents.environment_profiles AS profile
            WHERE profile.tenant_id = p_tenant_id AND profile.project_uid = p_project_uid
                AND profile.profile_uid = selected_environment.environment_profile_uid
                AND profile.profile_version = selected_environment.environment_profile_version
                AND p_provider_kind = ANY(profile.provider_kinds)
        )
    THEN RAISE EXCEPTION USING ERRCODE = '23505', MESSAGE = 'environment profile does not support provider'; END IF;

    RETURN QUERY
    SELECT created.session_uid, created.provider_kind, created.environment_lease_uid,
        created.environment_generation, environment.environment_profile_uid,
        environment.environment_profile_version, created.state, created.resource_version,
        created.created_at, created.updated_at
    FROM cloud_agents.create_managed_agent_session_v2(
        p_tenant_id, p_project_uid, p_session_uid, p_provider_kind,
        p_environment_lease_uid, p_idempotency_key, p_request_digest
    ) AS created
    LEFT JOIN cloud_agents.managed_host_environment_leases AS environment
        ON environment.tenant_id = p_tenant_id AND environment.project_uid = p_project_uid
        AND environment.lease_uid = created.environment_lease_uid;
END;
$cloud_agents_function$;

ALTER FUNCTION cloud_agents.create_user_environment_v1(text, text, text, text, bigint, bigint, text, text)
    OWNER TO cloud_agents_migration_owner;
ALTER FUNCTION cloud_agents.create_managed_agent_session_v3(text, text, text, text, text, text, text)
    OWNER TO cloud_agents_migration_owner;
REVOKE ALL ON FUNCTION cloud_agents.create_user_environment_v1(text, text, text, text, bigint, bigint, text, text)
    FROM PUBLIC;
REVOKE ALL ON FUNCTION cloud_agents.create_managed_agent_session_v3(text, text, text, text, text, text, text)
    FROM PUBLIC;
GRANT EXECUTE ON FUNCTION cloud_agents.create_user_environment_v1(text, text, text, text, bigint, bigint, text, text)
    TO cloud_agents_runtime;
GRANT EXECUTE ON FUNCTION cloud_agents.create_managed_agent_session_v3(text, text, text, text, text, text, text)
    TO cloud_agents_runtime;
