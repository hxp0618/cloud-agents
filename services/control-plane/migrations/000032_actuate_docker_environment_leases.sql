ALTER TABLE cloud_agents.managed_host_environment_leases
    ADD COLUMN provider_credential_ref text;
ALTER TABLE cloud_agents.managed_host_environment_leases
    ADD COLUMN cpu_limit_millis bigint;
ALTER TABLE cloud_agents.managed_host_environment_leases
    ADD COLUMN memory_limit_bytes bigint;
ALTER TABLE cloud_agents.managed_host_environment_leases
    ADD COLUMN worker_endpoint text NOT NULL DEFAULT '';
ALTER TABLE cloud_agents.managed_host_environment_leases
    ADD COLUMN worker_spiffe_id text NOT NULL DEFAULT '';
ALTER TABLE cloud_agents.managed_host_environment_leases
    ADD COLUMN stable_error_code text NOT NULL DEFAULT '';

ALTER TABLE cloud_agents.managed_host_environment_leases
    ADD CONSTRAINT managed_host_leases_deployment_input_set CHECK (
        (provider_credential_ref IS NULL) = (cpu_limit_millis IS NULL)
        AND (provider_credential_ref IS NULL) = (memory_limit_bytes IS NULL)
    );
ALTER TABLE cloud_agents.managed_host_environment_leases
    ADD CONSTRAINT managed_host_leases_provider_credential_ref CHECK (
        provider_credential_ref IS NULL OR cloud_agents.is_valid_identifier(provider_credential_ref)
    );
ALTER TABLE cloud_agents.managed_host_environment_leases
    ADD CONSTRAINT managed_host_leases_cpu_limit CHECK (
        cpu_limit_millis IS NULL OR cpu_limit_millis BETWEEN 100 AND 64000
    );
ALTER TABLE cloud_agents.managed_host_environment_leases
    ADD CONSTRAINT managed_host_leases_memory_limit CHECK (
        memory_limit_bytes IS NULL OR memory_limit_bytes BETWEEN 134217728 AND 1099511627776
    );
ALTER TABLE cloud_agents.managed_host_environment_leases
    ADD CONSTRAINT managed_host_leases_deployment_state CHECK (
        provider_credential_ref IS NULL AND worker_endpoint = '' AND worker_spiffe_id = '' AND stable_error_code = ''
        OR provider_credential_ref IS NOT NULL AND (
            observed_phase = 'provisioning' AND worker_endpoint = '' AND worker_spiffe_id = '' AND stable_error_code = ''
            OR observed_phase = 'ready' AND worker_endpoint ~ '^https://[^/?#[:space:]@]+$'
                AND worker_spiffe_id ~ '^spiffe://[^/?#[:space:]@]+/.+$' AND stable_error_code = ''
            OR observed_phase = 'failed' AND worker_endpoint = '' AND worker_spiffe_id = ''
                AND cloud_agents.is_valid_identifier(stable_error_code)
            OR observed_phase IN ('terminating', 'terminated') AND stable_error_code = '' AND (
                worker_endpoint = '' AND worker_spiffe_id = ''
                OR worker_endpoint ~ '^https://[^/?#[:space:]@]+$' AND worker_spiffe_id ~ '^spiffe://[^/?#[:space:]@]+/.+$'
            )
        )
    );

CREATE FUNCTION cloud_agents.create_managed_host_environment_lease_v3(
    p_tenant_id text, p_project_uid text, p_lease_uid text, p_lease_name text,
    p_release_digest text, p_target_uid text, p_expected_target_generation bigint,
    p_provider_credential_ref text, p_cpu_limit_millis bigint, p_memory_limit_bytes bigint,
    p_ttl_seconds bigint, p_idempotency_key text, p_request_digest text
)
RETURNS TABLE (
    lease_uid text, lease_name text, release_digest text,
    deployment_target_uid text, deployment_target_generation bigint,
    provider_credential_ref text, cpu_limit_millis bigint, memory_limit_bytes bigint,
    generation bigint, desired_phase text, observed_phase text, cleanup_phase text, environment_id text,
    worker_endpoint text, worker_spiffe_id text, stable_error_code text,
    expires_at timestamptz, resource_version bigint, created_at timestamptz, updated_at timestamptz
)
LANGUAGE plpgsql SECURITY DEFINER SET search_path = pg_catalog, cloud_agents
AS $cloud_agents_function$
DECLARE
    ignored_principal text;
    mutation_at timestamptz;
    existing cloud_agents.managed_host_environment_leases%ROWTYPE;
BEGIN
    ignored_principal := cloud_agents.require_runtime_mutation_principal();
    mutation_at := pg_catalog.transaction_timestamp();
    IF p_tenant_id IS DISTINCT FROM cloud_agents.require_tenant_id()
        OR NOT cloud_agents.is_valid_identifier(p_project_uid)
        OR NOT cloud_agents.is_valid_identifier(p_lease_uid)
        OR NOT cloud_agents.is_valid_identifier(p_lease_name)
        OR p_release_digest !~ '^sha256:[0-9a-f]{64}$'
        OR NOT cloud_agents.is_valid_identifier(p_target_uid)
        OR p_expected_target_generation < 1
        OR NOT cloud_agents.is_valid_identifier(p_provider_credential_ref)
        OR p_cpu_limit_millis NOT BETWEEN 100 AND 64000
        OR p_memory_limit_bytes NOT BETWEEN 134217728 AND 1099511627776
        OR p_ttl_seconds NOT BETWEEN 60 AND 86400
        OR p_idempotency_key !~ '^[A-Za-z0-9._~-]{16,128}$'
        OR p_request_digest !~ '^sha256:[0-9a-f]{64}$'
    THEN RAISE EXCEPTION USING ERRCODE = '22023', MESSAGE = 'managed host environment lease input is invalid'; END IF;

    SELECT lease.* INTO existing FROM cloud_agents.managed_host_environment_leases AS lease
    WHERE lease.tenant_id = p_tenant_id AND lease.project_uid = p_project_uid
        AND lease.create_idempotency_key = p_idempotency_key FOR UPDATE;
    IF FOUND THEN
        IF existing.create_request_digest IS DISTINCT FROM p_request_digest
            OR existing.deployment_target_uid IS DISTINCT FROM p_target_uid
            OR existing.deployment_target_generation IS DISTINCT FROM p_expected_target_generation
            OR existing.provider_credential_ref IS DISTINCT FROM p_provider_credential_ref
            OR existing.cpu_limit_millis IS DISTINCT FROM p_cpu_limit_millis
            OR existing.memory_limit_bytes IS DISTINCT FROM p_memory_limit_bytes
        THEN RAISE EXCEPTION USING ERRCODE = '23505', MESSAGE = 'managed host environment lease idempotency conflict'; END IF;
    ELSE
        PERFORM 1 FROM cloud_agents.projects AS project
        WHERE project.tenant_id = p_tenant_id AND project.project_uid = p_project_uid AND project.state = 'active'
        FOR KEY SHARE;
        IF NOT FOUND THEN RAISE EXCEPTION USING ERRCODE = '23503', MESSAGE = 'project is absent or inactive'; END IF;

        PERFORM 1 FROM cloud_agents.deployment_targets AS target
        WHERE target.tenant_id = p_tenant_id AND target.project_uid = p_project_uid
            AND target.target_uid = p_target_uid AND target.generation = p_expected_target_generation
            AND target.observed_phase = 'ready'
        FOR SHARE;
        IF NOT FOUND THEN RAISE EXCEPTION USING ERRCODE = '23505', MESSAGE = 'deployment target is not ready at the expected generation'; END IF;

        INSERT INTO cloud_agents.managed_host_environment_leases (
            tenant_id, tenant_ref_id, project_uid, lease_uid, lease_name, release_digest,
            deployment_target_uid, deployment_target_generation,
            provider_credential_ref, cpu_limit_millis, memory_limit_bytes,
            generation, desired_phase, observed_phase, cleanup_phase, environment_id,
            worker_endpoint, worker_spiffe_id, stable_error_code,
            expires_at, resource_version, create_idempotency_key, create_request_digest, created_at, updated_at
        ) VALUES (
            p_tenant_id, p_tenant_id, p_project_uid, p_lease_uid, p_lease_name, p_release_digest,
            p_target_uid, p_expected_target_generation,
            p_provider_credential_ref, p_cpu_limit_millis, p_memory_limit_bytes,
            1, 'active', 'provisioning', 'none', p_lease_uid, '', '', '',
            mutation_at + make_interval(secs => p_ttl_seconds), 1,
            p_idempotency_key, p_request_digest, mutation_at, mutation_at
        ) RETURNING managed_host_environment_leases.* INTO existing;
    END IF;

    lease_uid := existing.lease_uid; lease_name := existing.lease_name; release_digest := existing.release_digest;
    deployment_target_uid := existing.deployment_target_uid; deployment_target_generation := existing.deployment_target_generation;
    provider_credential_ref := existing.provider_credential_ref; cpu_limit_millis := existing.cpu_limit_millis;
    memory_limit_bytes := existing.memory_limit_bytes; generation := existing.generation;
    desired_phase := existing.desired_phase; observed_phase := existing.observed_phase; cleanup_phase := existing.cleanup_phase;
    environment_id := existing.environment_id; worker_endpoint := existing.worker_endpoint;
    worker_spiffe_id := existing.worker_spiffe_id; stable_error_code := existing.stable_error_code;
    expires_at := existing.expires_at; resource_version := existing.resource_version;
    created_at := existing.created_at; updated_at := existing.updated_at;
    RETURN NEXT;
END;
$cloud_agents_function$;

CREATE FUNCTION cloud_agents.complete_managed_host_environment_lease_deployment_v1(
    p_tenant_id text, p_project_uid text, p_lease_uid text, p_expected_generation bigint,
    p_target_uid text, p_expected_target_generation bigint, p_succeeded boolean,
    p_worker_endpoint text, p_worker_spiffe_id text, p_stable_error_code text
)
RETURNS TABLE (
    lease_uid text, lease_name text, release_digest text,
    deployment_target_uid text, deployment_target_generation bigint,
    provider_credential_ref text, cpu_limit_millis bigint, memory_limit_bytes bigint,
    generation bigint, desired_phase text, observed_phase text, cleanup_phase text, environment_id text,
    worker_endpoint text, worker_spiffe_id text, stable_error_code text,
    expires_at timestamptz, resource_version bigint, created_at timestamptz, updated_at timestamptz
)
LANGUAGE plpgsql SECURITY DEFINER SET search_path = pg_catalog, cloud_agents
AS $cloud_agents_function$
DECLARE
    ignored_principal text;
    mutation_at timestamptz;
    existing cloud_agents.managed_host_environment_leases%ROWTYPE;
BEGIN
    ignored_principal := cloud_agents.require_runtime_mutation_principal();
    mutation_at := pg_catalog.transaction_timestamp();
    IF p_tenant_id IS DISTINCT FROM cloud_agents.require_tenant_id()
        OR NOT cloud_agents.is_valid_identifier(p_project_uid) OR NOT cloud_agents.is_valid_identifier(p_lease_uid)
        OR p_expected_generation < 1 OR NOT cloud_agents.is_valid_identifier(p_target_uid)
        OR p_expected_target_generation < 1
        OR (p_succeeded AND (
            p_worker_endpoint !~ '^https://[^/?#[:space:]@]+$'
            OR p_worker_spiffe_id !~ '^spiffe://[^/?#[:space:]@]+/.+$' OR p_stable_error_code <> ''
        ))
        OR (NOT p_succeeded AND (
            p_worker_endpoint <> '' OR p_worker_spiffe_id <> '' OR NOT cloud_agents.is_valid_identifier(p_stable_error_code)
        ))
    THEN RAISE EXCEPTION USING ERRCODE = '22023', MESSAGE = 'managed host environment lease deployment completion is invalid'; END IF;

    SELECT lease.* INTO existing FROM cloud_agents.managed_host_environment_leases AS lease
    WHERE lease.tenant_id = p_tenant_id AND lease.project_uid = p_project_uid AND lease.lease_uid = p_lease_uid
    FOR UPDATE;
    IF NOT FOUND THEN RETURN; END IF;
    IF existing.generation <> p_expected_generation
        OR existing.deployment_target_uid IS DISTINCT FROM p_target_uid
        OR existing.deployment_target_generation IS DISTINCT FROM p_expected_target_generation
        OR existing.desired_phase <> 'active'
    THEN RAISE EXCEPTION USING ERRCODE = '23505', MESSAGE = 'managed host environment lease generation conflict'; END IF;

    IF existing.observed_phase = 'ready' THEN
        IF NOT p_succeeded OR existing.worker_endpoint IS DISTINCT FROM p_worker_endpoint
            OR existing.worker_spiffe_id IS DISTINCT FROM p_worker_spiffe_id
        THEN RAISE EXCEPTION USING ERRCODE = '23505', MESSAGE = 'managed host environment lease deployment already completed'; END IF;
    ELSIF existing.observed_phase NOT IN ('provisioning', 'failed') THEN
        RAISE EXCEPTION USING ERRCODE = '23505', MESSAGE = 'managed host environment lease deployment transition conflict';
    ELSE
        UPDATE cloud_agents.managed_host_environment_leases AS lease SET
            observed_phase = CASE WHEN p_succeeded THEN 'ready' ELSE 'failed' END,
            worker_endpoint = p_worker_endpoint, worker_spiffe_id = p_worker_spiffe_id,
            stable_error_code = p_stable_error_code,
            resource_version = existing.resource_version + 1, updated_at = mutation_at
        WHERE lease.tenant_id = p_tenant_id AND lease.project_uid = p_project_uid AND lease.lease_uid = p_lease_uid
        RETURNING lease.* INTO existing;
    END IF;

    lease_uid := existing.lease_uid; lease_name := existing.lease_name; release_digest := existing.release_digest;
    deployment_target_uid := existing.deployment_target_uid; deployment_target_generation := existing.deployment_target_generation;
    provider_credential_ref := existing.provider_credential_ref; cpu_limit_millis := existing.cpu_limit_millis;
    memory_limit_bytes := existing.memory_limit_bytes; generation := existing.generation;
    desired_phase := existing.desired_phase; observed_phase := existing.observed_phase; cleanup_phase := existing.cleanup_phase;
    environment_id := existing.environment_id; worker_endpoint := existing.worker_endpoint;
    worker_spiffe_id := existing.worker_spiffe_id; stable_error_code := existing.stable_error_code;
    expires_at := existing.expires_at; resource_version := existing.resource_version;
    created_at := existing.created_at; updated_at := existing.updated_at;
    RETURN NEXT;
END;
$cloud_agents_function$;

ALTER FUNCTION cloud_agents.create_managed_host_environment_lease_v3(
    text, text, text, text, text, text, bigint, text, bigint, bigint, bigint, text, text
) OWNER TO cloud_agents_migration_owner;
ALTER FUNCTION cloud_agents.complete_managed_host_environment_lease_deployment_v1(
    text, text, text, bigint, text, bigint, boolean, text, text, text
) OWNER TO cloud_agents_migration_owner;
REVOKE EXECUTE ON FUNCTION cloud_agents.create_managed_host_environment_lease_v2(
    text, text, text, text, text, text, bigint, bigint, text, text
) FROM cloud_agents_runtime;
REVOKE ALL ON FUNCTION cloud_agents.create_managed_host_environment_lease_v3(
    text, text, text, text, text, text, bigint, text, bigint, bigint, bigint, text, text
) FROM PUBLIC;
REVOKE ALL ON FUNCTION cloud_agents.complete_managed_host_environment_lease_deployment_v1(
    text, text, text, bigint, text, bigint, boolean, text, text, text
) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION cloud_agents.create_managed_host_environment_lease_v3(
    text, text, text, text, text, text, bigint, text, bigint, bigint, bigint, text, text
) TO cloud_agents_runtime;
GRANT EXECUTE ON FUNCTION cloud_agents.complete_managed_host_environment_lease_deployment_v1(
    text, text, text, bigint, text, bigint, boolean, text, text, text
) TO cloud_agents_runtime;
