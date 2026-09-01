-- Bind new Managed Host leases to an already-probed deployment target while
-- retaining nullable columns for leases admitted before target registration.
ALTER TABLE cloud_agents.managed_host_environment_leases
    ADD COLUMN deployment_target_uid text;

ALTER TABLE cloud_agents.managed_host_environment_leases
    ADD COLUMN deployment_target_generation bigint;

ALTER TABLE cloud_agents.managed_host_environment_leases
    ADD CONSTRAINT managed_host_leases_target_pair CHECK (
        (deployment_target_uid IS NULL) = (deployment_target_generation IS NULL)
    );

ALTER TABLE cloud_agents.managed_host_environment_leases
    ADD CONSTRAINT managed_host_leases_target_uid CHECK (
        deployment_target_uid IS NULL OR cloud_agents.is_valid_identifier(deployment_target_uid)
    );

ALTER TABLE cloud_agents.managed_host_environment_leases
    ADD CONSTRAINT managed_host_leases_target_generation CHECK (
        deployment_target_generation IS NULL OR deployment_target_generation > 0
    );

ALTER TABLE cloud_agents.managed_host_environment_leases
    ADD CONSTRAINT managed_host_leases_target_fk FOREIGN KEY (
        tenant_id, project_uid, deployment_target_uid
    ) REFERENCES cloud_agents.deployment_targets (
        tenant_id, project_uid, target_uid
    ) ON UPDATE RESTRICT ON DELETE RESTRICT;

CREATE FUNCTION cloud_agents.create_managed_host_environment_lease_v2(
    p_tenant_id text, p_project_uid text, p_lease_uid text, p_lease_name text,
    p_release_digest text, p_target_uid text, p_expected_target_generation bigint,
    p_ttl_seconds bigint, p_idempotency_key text, p_request_digest text
)
RETURNS TABLE (
    lease_uid text, lease_name text, release_digest text,
    deployment_target_uid text, deployment_target_generation bigint, generation bigint,
    desired_phase text, observed_phase text, cleanup_phase text, environment_id text,
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
        OR p_ttl_seconds < 60 OR p_ttl_seconds > 86400
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
        THEN
            RAISE EXCEPTION USING ERRCODE = '23505', MESSAGE = 'managed host environment lease idempotency conflict';
        END IF;
    ELSE
        PERFORM 1 FROM cloud_agents.projects AS project
        WHERE project.tenant_id = p_tenant_id AND project.project_uid = p_project_uid AND project.state = 'active'
        FOR KEY SHARE;
        IF NOT FOUND THEN RAISE EXCEPTION USING ERRCODE = '23503', MESSAGE = 'project is absent or inactive'; END IF;

        PERFORM 1 FROM cloud_agents.deployment_targets AS target
        WHERE target.tenant_id = p_tenant_id AND target.project_uid = p_project_uid
            AND target.target_uid = p_target_uid
            AND target.generation = p_expected_target_generation
            AND target.observed_phase = 'ready'
        FOR SHARE;
        IF NOT FOUND THEN
            RAISE EXCEPTION USING ERRCODE = '23505', MESSAGE = 'deployment target is not ready at the expected generation';
        END IF;

        INSERT INTO cloud_agents.managed_host_environment_leases (
            tenant_id, tenant_ref_id, project_uid, lease_uid, lease_name, release_digest,
            deployment_target_uid, deployment_target_generation, generation,
            desired_phase, observed_phase, cleanup_phase, environment_id, expires_at, resource_version,
            create_idempotency_key, create_request_digest, created_at, updated_at
        ) VALUES (
            p_tenant_id, p_tenant_id, p_project_uid, p_lease_uid, p_lease_name, p_release_digest,
            p_target_uid, p_expected_target_generation, 1,
            'active', 'provisioning', 'none', p_lease_uid,
            mutation_at + make_interval(secs => p_ttl_seconds), 1,
            p_idempotency_key, p_request_digest, mutation_at, mutation_at
        ) RETURNING managed_host_environment_leases.* INTO existing;
    END IF;

    lease_uid := existing.lease_uid; lease_name := existing.lease_name; release_digest := existing.release_digest;
    deployment_target_uid := existing.deployment_target_uid;
    deployment_target_generation := existing.deployment_target_generation; generation := existing.generation;
    desired_phase := existing.desired_phase; observed_phase := existing.observed_phase;
    cleanup_phase := existing.cleanup_phase; environment_id := existing.environment_id; expires_at := existing.expires_at;
    resource_version := existing.resource_version; created_at := existing.created_at; updated_at := existing.updated_at;
    RETURN NEXT;
END;
$cloud_agents_function$;

ALTER FUNCTION cloud_agents.create_managed_host_environment_lease_v2(
    text, text, text, text, text, text, bigint, bigint, text, text
) OWNER TO cloud_agents_migration_owner;
REVOKE EXECUTE ON FUNCTION cloud_agents.create_managed_host_environment_lease_v1(
    text, text, text, text, text, bigint, text, text
) FROM cloud_agents_runtime;
REVOKE ALL ON FUNCTION cloud_agents.create_managed_host_environment_lease_v2(
    text, text, text, text, text, text, bigint, bigint, text, text
) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION cloud_agents.create_managed_host_environment_lease_v2(
    text, text, text, text, text, text, bigint, bigint, text, text
) TO cloud_agents_runtime;
