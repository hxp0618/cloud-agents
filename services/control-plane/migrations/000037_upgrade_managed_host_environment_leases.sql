ALTER TABLE cloud_agents.managed_host_environment_leases
    ADD COLUMN upgrade_idempotency_key text;
ALTER TABLE cloud_agents.managed_host_environment_leases
    ADD COLUMN upgrade_request_digest text;
ALTER TABLE cloud_agents.managed_host_environment_leases
    ADD CONSTRAINT managed_host_leases_upgrade_pair CHECK (
        (upgrade_idempotency_key IS NULL) = (upgrade_request_digest IS NULL)
    );
ALTER TABLE cloud_agents.managed_host_environment_leases
    ADD CONSTRAINT managed_host_leases_upgrade_key CHECK (
        upgrade_idempotency_key IS NULL OR upgrade_idempotency_key ~ '^[A-Za-z0-9._~-]{16,128}$'
    );
ALTER TABLE cloud_agents.managed_host_environment_leases
    ADD CONSTRAINT managed_host_leases_upgrade_digest CHECK (
        upgrade_request_digest IS NULL OR upgrade_request_digest ~ '^sha256:[0-9a-f]{64}$'
    );

CREATE FUNCTION cloud_agents.begin_managed_host_environment_lease_upgrade_v1(
    p_tenant_id text, p_project_uid text, p_lease_uid text, p_expected_generation bigint,
    p_release_digest text, p_idempotency_key text, p_request_digest text
)
RETURNS TABLE (
    lease_uid text, lease_name text, release_digest text,
    deployment_target_uid text, deployment_target_generation bigint,
    provider_credential_ref text, cpu_limit_millis bigint, memory_limit_bytes bigint,
    generation bigint, desired_phase text, observed_phase text, cleanup_phase text, environment_id text,
    worker_endpoint text, worker_spiffe_id text, worker_server_name text, stable_error_code text,
    expires_at timestamptz, resource_version bigint, created_at timestamptz, updated_at timestamptz,
    execute_upgrade boolean
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
        OR p_release_digest !~ '^sha256:[0-9a-f]{64}$'
        OR p_idempotency_key !~ '^[A-Za-z0-9._~-]{16,128}$'
        OR p_request_digest !~ '^sha256:[0-9a-f]{64}$'
    THEN
        RAISE EXCEPTION USING ERRCODE = '22023', MESSAGE = 'managed host environment lease upgrade input is invalid';
    END IF;

    SELECT lease.* INTO existing
    FROM cloud_agents.managed_host_environment_leases AS lease
    WHERE lease.tenant_id = p_tenant_id AND lease.project_uid = p_project_uid AND lease.lease_uid = p_lease_uid
    FOR UPDATE;
    IF NOT FOUND THEN RETURN; END IF;

    IF existing.upgrade_idempotency_key = p_idempotency_key THEN
        IF existing.upgrade_request_digest IS DISTINCT FROM p_request_digest
            OR existing.generation <> p_expected_generation + 1
            OR existing.release_digest IS DISTINCT FROM p_release_digest
        THEN
            RAISE EXCEPTION USING ERRCODE = '23505', MESSAGE = 'managed host environment lease upgrade idempotency conflict';
        END IF;
    ELSE
        IF existing.generation <> p_expected_generation
            OR existing.desired_phase <> 'active'
            OR existing.observed_phase NOT IN ('ready', 'failed')
            OR existing.cleanup_phase <> 'none'
        THEN
            RAISE EXCEPTION USING ERRCODE = '23505', MESSAGE = 'managed host environment lease upgrade transition is invalid';
        END IF;
        UPDATE cloud_agents.managed_host_environment_leases AS lease
        SET release_digest = p_release_digest, observed_phase = 'provisioning',
            worker_endpoint = '', worker_spiffe_id = '', worker_server_name = '', stable_error_code = '',
            generation = existing.generation + 1, resource_version = existing.resource_version + 1,
            upgrade_idempotency_key = p_idempotency_key, upgrade_request_digest = p_request_digest,
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
    execute_upgrade := true;
    RETURN NEXT;
END;
$cloud_agents_function$;

ALTER FUNCTION cloud_agents.begin_managed_host_environment_lease_upgrade_v1(text, text, text, bigint, text, text, text)
    OWNER TO cloud_agents_migration_owner;
REVOKE ALL ON FUNCTION cloud_agents.begin_managed_host_environment_lease_upgrade_v1(text, text, text, bigint, text, text, text)
    FROM PUBLIC;
GRANT EXECUTE ON FUNCTION cloud_agents.begin_managed_host_environment_lease_upgrade_v1(text, text, text, bigint, text, text, text)
    TO cloud_agents_runtime;
