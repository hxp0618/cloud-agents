CREATE FUNCTION cloud_agents.begin_managed_host_environment_lease_termination_v1(
    p_tenant_id text, p_project_uid text, p_lease_uid text, p_expected_generation bigint,
    p_idempotency_key text, p_request_digest text
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
    ignored_principal text;
    mutation_at timestamptz;
    existing cloud_agents.managed_host_environment_leases%ROWTYPE;
BEGIN
    ignored_principal := cloud_agents.require_runtime_mutation_principal();
    mutation_at := pg_catalog.transaction_timestamp();
    IF p_tenant_id IS DISTINCT FROM cloud_agents.require_tenant_id()
        OR NOT cloud_agents.is_valid_identifier(p_project_uid)
        OR NOT cloud_agents.is_valid_identifier(p_lease_uid)
        OR p_expected_generation < 1
        OR p_idempotency_key !~ '^[A-Za-z0-9._~-]{16,128}$'
        OR p_request_digest !~ '^sha256:[0-9a-f]{64}$'
    THEN
        RAISE EXCEPTION USING ERRCODE = '22023', MESSAGE = 'managed host environment lease input is invalid';
    END IF;

    SELECT lease.* INTO existing
    FROM cloud_agents.managed_host_environment_leases AS lease
    WHERE lease.tenant_id = p_tenant_id AND lease.project_uid = p_project_uid AND lease.lease_uid = p_lease_uid
    FOR UPDATE;
    IF NOT FOUND THEN RETURN; END IF;
    IF existing.terminate_idempotency_key IS NOT NULL THEN
        IF existing.terminate_idempotency_key <> p_idempotency_key
            OR existing.terminate_request_digest IS DISTINCT FROM p_request_digest
        THEN
            RAISE EXCEPTION USING ERRCODE = '23505', MESSAGE = 'managed host environment lease termination idempotency conflict';
        END IF;
    ELSIF existing.generation <> p_expected_generation
        OR existing.desired_phase <> 'active'
        OR existing.observed_phase NOT IN ('provisioning', 'ready', 'failed')
        OR existing.cleanup_phase <> 'none'
    THEN
        RAISE EXCEPTION USING ERRCODE = '23505', MESSAGE = 'managed host environment lease termination transition is invalid';
    ELSE
        UPDATE cloud_agents.managed_host_environment_leases AS lease
        SET desired_phase = 'terminated', observed_phase = 'terminating', cleanup_phase = 'pending',
            stable_error_code = '', generation = existing.generation + 1,
            resource_version = existing.resource_version + 1,
            terminate_idempotency_key = p_idempotency_key, terminate_request_digest = p_request_digest,
            updated_at = mutation_at
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

CREATE FUNCTION cloud_agents.complete_managed_host_environment_lease_termination_v1(
    p_tenant_id text, p_project_uid text, p_lease_uid text, p_expected_generation bigint
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
    ignored_principal text;
    mutation_at timestamptz;
    existing cloud_agents.managed_host_environment_leases%ROWTYPE;
BEGIN
    ignored_principal := cloud_agents.require_runtime_mutation_principal();
    mutation_at := pg_catalog.transaction_timestamp();
    IF p_tenant_id IS DISTINCT FROM cloud_agents.require_tenant_id()
        OR NOT cloud_agents.is_valid_identifier(p_project_uid)
        OR NOT cloud_agents.is_valid_identifier(p_lease_uid)
        OR p_expected_generation < 1
    THEN
        RAISE EXCEPTION USING ERRCODE = '22023', MESSAGE = 'managed host environment lease cleanup completion is invalid';
    END IF;

    SELECT lease.* INTO existing
    FROM cloud_agents.managed_host_environment_leases AS lease
    WHERE lease.tenant_id = p_tenant_id AND lease.project_uid = p_project_uid AND lease.lease_uid = p_lease_uid
    FOR UPDATE;
    IF NOT FOUND THEN RETURN; END IF;
    IF existing.generation <> p_expected_generation THEN
        RAISE EXCEPTION USING ERRCODE = '23505', MESSAGE = 'managed host environment lease generation conflict';
    END IF;
    IF existing.desired_phase = 'terminated' AND existing.observed_phase = 'terminated' AND existing.cleanup_phase = 'complete' THEN
        NULL;
    ELSIF existing.desired_phase <> 'terminated' OR existing.observed_phase <> 'terminating' OR existing.cleanup_phase <> 'pending' THEN
        RAISE EXCEPTION USING ERRCODE = '23505', MESSAGE = 'managed host environment lease cleanup transition is invalid';
    ELSE
        UPDATE cloud_agents.managed_host_environment_leases AS lease
        SET observed_phase = 'terminated', cleanup_phase = 'complete',
            worker_endpoint = '', worker_spiffe_id = '', worker_server_name = '', stable_error_code = '',
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
    worker_spiffe_id := existing.worker_spiffe_id; worker_server_name := existing.worker_server_name;
    stable_error_code := existing.stable_error_code; expires_at := existing.expires_at;
    resource_version := existing.resource_version; created_at := existing.created_at; updated_at := existing.updated_at;
    RETURN NEXT;
END;
$cloud_agents_function$;

ALTER FUNCTION cloud_agents.begin_managed_host_environment_lease_termination_v1(text, text, text, bigint, text, text)
    OWNER TO cloud_agents_migration_owner;
ALTER FUNCTION cloud_agents.complete_managed_host_environment_lease_termination_v1(text, text, text, bigint)
    OWNER TO cloud_agents_migration_owner;
REVOKE ALL ON FUNCTION cloud_agents.begin_managed_host_environment_lease_termination_v1(text, text, text, bigint, text, text)
    FROM PUBLIC;
REVOKE ALL ON FUNCTION cloud_agents.complete_managed_host_environment_lease_termination_v1(text, text, text, bigint)
    FROM PUBLIC;
REVOKE EXECUTE ON FUNCTION cloud_agents.terminate_managed_host_environment_lease_v1(text, text, text, bigint, text, text)
    FROM cloud_agents_runtime;
GRANT EXECUTE ON FUNCTION cloud_agents.begin_managed_host_environment_lease_termination_v1(text, text, text, bigint, text, text)
    TO cloud_agents_runtime;
GRANT EXECUTE ON FUNCTION cloud_agents.complete_managed_host_environment_lease_termination_v1(text, text, text, bigint)
    TO cloud_agents_runtime;
