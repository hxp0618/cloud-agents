ALTER TABLE cloud_agents.managed_host_environment_leases
    ADD COLUMN worker_health_claim text;
ALTER TABLE cloud_agents.managed_host_environment_leases
    ADD COLUMN worker_health_claim_expires_at timestamptz;
ALTER TABLE cloud_agents.managed_host_environment_leases
    ADD COLUMN worker_health_generation bigint;
ALTER TABLE cloud_agents.managed_host_environment_leases
    ADD COLUMN worker_health_resource_version bigint;
ALTER TABLE cloud_agents.managed_host_environment_leases
    ADD COLUMN worker_health_checked_at timestamptz;
ALTER TABLE cloud_agents.managed_host_environment_leases
    ADD COLUMN worker_health_success_at timestamptz;
ALTER TABLE cloud_agents.managed_host_environment_leases
    ADD COLUMN worker_health_succeeded boolean;
ALTER TABLE cloud_agents.managed_host_environment_leases
    ADD CONSTRAINT managed_host_worker_health_claim CHECK (
        (worker_health_claim IS NULL AND worker_health_claim_expires_at IS NULL)
        OR (worker_health_claim IS NOT NULL AND worker_health_claim ~ '^[0-9a-f]{64}$' AND worker_health_claim_expires_at IS NOT NULL)
    );
ALTER TABLE cloud_agents.managed_host_environment_leases
    ADD CONSTRAINT managed_host_worker_health_result CHECK (
        (worker_health_generation IS NULL AND worker_health_resource_version IS NULL
            AND worker_health_checked_at IS NULL AND worker_health_success_at IS NULL
            AND worker_health_succeeded IS NULL)
        OR (worker_health_generation IS NOT NULL AND worker_health_resource_version IS NOT NULL
            AND worker_health_generation > 0 AND worker_health_resource_version > 0
            AND worker_health_checked_at IS NOT NULL AND worker_health_succeeded IS NOT NULL
            AND (worker_health_success_at IS NULL OR worker_health_success_at <= worker_health_checked_at)
            AND (NOT worker_health_succeeded OR (worker_health_success_at IS NOT NULL AND worker_health_success_at = worker_health_checked_at)))
    );

CREATE INDEX managed_host_worker_health_due_idx ON cloud_agents.managed_host_environment_leases
    (desired_phase, observed_phase, cleanup_phase, worker_health_checked_at NULLS FIRST, tenant_id, project_uid, lease_uid);

-- Internal Control Plane maintenance authority only. Returns registered routes, never user content
-- or credential references. This bounded cross-tenant claim is not exposed by an HTTP route.
CREATE FUNCTION cloud_agents.claim_worker_health_checks_v1(p_claim text)
RETURNS TABLE (tenant_id text, project_uid text, lease_uid text, generation bigint,
    resource_version bigint, worker_endpoint text, worker_spiffe_id text, worker_server_name text)
LANGUAGE plpgsql SECURITY DEFINER SET search_path = pg_catalog, cloud_agents
AS $$
DECLARE candidate cloud_agents.managed_host_environment_leases%ROWTYPE;
    checked_now timestamptz := pg_catalog.clock_timestamp();
BEGIN
    IF p_claim IS NULL OR p_claim !~ '^[0-9a-f]{64}$' THEN
        RAISE EXCEPTION USING ERRCODE = '22023', MESSAGE = 'invalid worker health claim';
    END IF;
    FOR candidate IN
        SELECT lease.* FROM cloud_agents.managed_host_environment_leases AS lease
        WHERE lease.desired_phase = 'active' AND lease.observed_phase = 'ready'
            AND lease.cleanup_phase = 'none' AND lease.expires_at > checked_now
            AND lease.deployment_target_uid IS NOT NULL
            AND lease.worker_endpoint <> '' AND lease.worker_spiffe_id <> '' AND lease.worker_server_name <> ''
            AND (lease.worker_health_claim_expires_at IS NULL OR lease.worker_health_claim_expires_at <= checked_now)
            AND (lease.worker_health_checked_at IS NULL OR lease.worker_health_checked_at <= checked_now - interval '20 seconds'
                OR lease.worker_health_generation IS DISTINCT FROM lease.generation
                OR lease.worker_health_resource_version IS DISTINCT FROM lease.resource_version)
        ORDER BY lease.worker_health_checked_at NULLS FIRST, lease.tenant_id, lease.project_uid, lease.lease_uid
        LIMIT 8 FOR UPDATE SKIP LOCKED
    LOOP
        UPDATE cloud_agents.managed_host_environment_leases AS lease
        SET worker_health_claim = p_claim, worker_health_claim_expires_at = checked_now + interval '10 seconds'
        WHERE lease.tenant_id = candidate.tenant_id AND lease.project_uid = candidate.project_uid AND lease.lease_uid = candidate.lease_uid;
        tenant_id := candidate.tenant_id; project_uid := candidate.project_uid; lease_uid := candidate.lease_uid;
        generation := candidate.generation; resource_version := candidate.resource_version;
        worker_endpoint := candidate.worker_endpoint; worker_spiffe_id := candidate.worker_spiffe_id;
        worker_server_name := candidate.worker_server_name;
        RETURN NEXT;
    END LOOP;
END;
$$;

CREATE FUNCTION cloud_agents.complete_worker_health_check_v1(
    p_tenant text, p_project text, p_lease text, p_generation bigint, p_resource_version bigint,
    p_claim text, p_succeeded boolean)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path = pg_catalog, cloud_agents
AS $$
DECLARE checked_now timestamptz := pg_catalog.clock_timestamp();
BEGIN
    IF p_tenant IS NULL OR p_project IS NULL OR p_lease IS NULL
        OR NOT cloud_agents.is_valid_identifier(p_tenant) OR NOT cloud_agents.is_valid_identifier(p_project)
        OR NOT cloud_agents.is_valid_identifier(p_lease) OR p_generation IS NULL OR p_generation < 1
        OR p_resource_version IS NULL OR p_resource_version < 1 OR p_claim IS NULL
        OR p_claim !~ '^[0-9a-f]{64}$' OR p_succeeded IS NULL THEN
        RAISE EXCEPTION USING ERRCODE = '22023', MESSAGE = 'invalid worker health result';
    END IF;
    UPDATE cloud_agents.managed_host_environment_leases AS lease SET
        worker_health_success_at = CASE WHEN p_succeeded THEN checked_now
            WHEN lease.worker_health_generation = p_generation AND lease.worker_health_resource_version = p_resource_version
                THEN lease.worker_health_success_at ELSE NULL END,
        worker_health_generation = p_generation, worker_health_resource_version = p_resource_version,
        worker_health_checked_at = checked_now, worker_health_succeeded = p_succeeded,
        worker_health_claim = NULL, worker_health_claim_expires_at = NULL
    WHERE lease.tenant_id = p_tenant AND lease.project_uid = p_project AND lease.lease_uid = p_lease
        AND lease.generation = p_generation AND lease.resource_version = p_resource_version
        AND lease.worker_health_claim = p_claim AND lease.worker_health_claim_expires_at > pg_catalog.clock_timestamp()
        AND lease.desired_phase = 'active' AND lease.observed_phase = 'ready'
        AND lease.cleanup_phase = 'none' AND lease.expires_at > pg_catalog.clock_timestamp();
    RETURN FOUND;
END;
$$;

ALTER FUNCTION cloud_agents.claim_worker_health_checks_v1(text) OWNER TO cloud_agents_migration_owner;
ALTER FUNCTION cloud_agents.complete_worker_health_check_v1(text, text, text, bigint, bigint, text, boolean) OWNER TO cloud_agents_migration_owner;
REVOKE ALL ON FUNCTION cloud_agents.claim_worker_health_checks_v1(text) FROM PUBLIC;
REVOKE ALL ON FUNCTION cloud_agents.complete_worker_health_check_v1(text, text, text, bigint, bigint, text, boolean) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION cloud_agents.claim_worker_health_checks_v1(text) TO cloud_agents_runtime;
GRANT EXECUTE ON FUNCTION cloud_agents.complete_worker_health_check_v1(text, text, text, bigint, bigint, text, boolean) TO cloud_agents_runtime;
