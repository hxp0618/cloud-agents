-- Durable Managed Host admission. It records the lease boundary only;
-- external workload and volume actuators are deliberately not part of this
-- migration.
CREATE TABLE cloud_agents.managed_host_environment_leases (
    tenant_id text NOT NULL,
    tenant_ref_id text NOT NULL,
    project_uid text NOT NULL,
    lease_uid text NOT NULL,
    lease_name text NOT NULL,
    release_digest text NOT NULL,
    generation bigint NOT NULL,
    desired_phase text NOT NULL,
    observed_phase text NOT NULL,
    cleanup_phase text NOT NULL,
    environment_id text NOT NULL,
    expires_at timestamptz NOT NULL,
    resource_version bigint NOT NULL,
    create_idempotency_key text NOT NULL,
    create_request_digest text NOT NULL,
    terminate_idempotency_key text,
    terminate_request_digest text,
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL,
    PRIMARY KEY (tenant_id, project_uid, lease_uid),
    CONSTRAINT managed_host_leases_tenant_ref CHECK (tenant_id = tenant_ref_id),
    CONSTRAINT managed_host_leases_project_fk FOREIGN KEY (tenant_id, project_uid)
        REFERENCES cloud_agents.projects (tenant_id, project_uid) ON UPDATE RESTRICT ON DELETE RESTRICT,
    CONSTRAINT managed_host_leases_tenant_fk FOREIGN KEY (tenant_id, tenant_ref_id)
        REFERENCES cloud_agents.platform_tenants (tenant_id, tenant_uid) ON UPDATE RESTRICT ON DELETE RESTRICT,
    CONSTRAINT managed_host_leases_lease_uid CHECK (cloud_agents.is_valid_identifier(lease_uid)),
    CONSTRAINT managed_host_leases_lease_name CHECK (cloud_agents.is_valid_identifier(lease_name)),
    CONSTRAINT managed_host_leases_environment_id CHECK (cloud_agents.is_valid_identifier(environment_id)),
    CONSTRAINT managed_host_leases_release_digest CHECK (release_digest ~ '^sha256:[0-9a-f]{64}$'),
    CONSTRAINT managed_host_leases_generation CHECK (generation > 0),
    CONSTRAINT managed_host_leases_desired_phase CHECK (desired_phase IN ('active', 'terminated')),
    CONSTRAINT managed_host_leases_observed_phase CHECK (observed_phase IN ('provisioning', 'ready', 'terminating', 'terminated', 'failed')),
    CONSTRAINT managed_host_leases_cleanup_phase CHECK (cleanup_phase IN ('none', 'pending', 'revoking', 'reaping', 'complete', 'blocked')),
    CONSTRAINT managed_host_leases_resource_version CHECK (resource_version > 0),
    CONSTRAINT managed_host_leases_create_key CHECK (create_idempotency_key ~ '^[A-Za-z0-9._~-]{16,128}$'),
    CONSTRAINT managed_host_leases_create_digest CHECK (create_request_digest ~ '^sha256:[0-9a-f]{64}$'),
    CONSTRAINT managed_host_leases_terminate_pair CHECK ((terminate_idempotency_key IS NULL) = (terminate_request_digest IS NULL)),
    CONSTRAINT managed_host_leases_terminate_key CHECK (terminate_idempotency_key IS NULL OR terminate_idempotency_key ~ '^[A-Za-z0-9._~-]{16,128}$'),
    CONSTRAINT managed_host_leases_terminate_digest CHECK (terminate_request_digest IS NULL OR terminate_request_digest ~ '^sha256:[0-9a-f]{64}$')
);

CREATE UNIQUE INDEX managed_host_leases_create_key_idx
    ON cloud_agents.managed_host_environment_leases (tenant_id, project_uid, create_idempotency_key);

ALTER TABLE cloud_agents.managed_host_environment_leases OWNER TO cloud_agents_migration_owner;
ALTER TABLE cloud_agents.managed_host_environment_leases ENABLE ROW LEVEL SECURITY;
ALTER TABLE cloud_agents.managed_host_environment_leases FORCE ROW LEVEL SECURITY;
CREATE POLICY managed_host_leases_runtime_tenant ON cloud_agents.managed_host_environment_leases
    TO cloud_agents_runtime USING (tenant_id = cloud_agents.require_tenant_id())
    WITH CHECK (tenant_id = cloud_agents.require_tenant_id());
CREATE POLICY managed_host_leases_migration_owner ON cloud_agents.managed_host_environment_leases
    TO cloud_agents_migration_owner USING (true) WITH CHECK (true);
REVOKE ALL ON TABLE cloud_agents.managed_host_environment_leases FROM PUBLIC;
REVOKE ALL ON TABLE cloud_agents.managed_host_environment_leases FROM cloud_agents_bootstrap_admin;
GRANT SELECT ON TABLE cloud_agents.managed_host_environment_leases TO cloud_agents_runtime;

CREATE FUNCTION cloud_agents.create_managed_host_environment_lease_v1(
    p_tenant_id text, p_project_uid text, p_lease_uid text, p_lease_name text,
    p_release_digest text, p_ttl_seconds bigint, p_idempotency_key text, p_request_digest text
)
RETURNS TABLE (
    lease_uid text, lease_name text, release_digest text, generation bigint,
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
        OR p_ttl_seconds < 60 OR p_ttl_seconds > 86400
        OR p_idempotency_key !~ '^[A-Za-z0-9._~-]{16,128}$'
        OR p_request_digest !~ '^sha256:[0-9a-f]{64}$'
    THEN RAISE EXCEPTION USING ERRCODE = '22023', MESSAGE = 'managed host environment lease input is invalid'; END IF;

    SELECT lease.* INTO existing FROM cloud_agents.managed_host_environment_leases AS lease
    WHERE lease.tenant_id = p_tenant_id AND lease.project_uid = p_project_uid
        AND lease.create_idempotency_key = p_idempotency_key FOR UPDATE;
    IF FOUND THEN
        IF existing.create_request_digest IS DISTINCT FROM p_request_digest THEN
            RAISE EXCEPTION USING ERRCODE = '23505', MESSAGE = 'managed host environment lease idempotency conflict';
        END IF;
        lease_uid := existing.lease_uid; lease_name := existing.lease_name; release_digest := existing.release_digest;
        generation := existing.generation; desired_phase := existing.desired_phase; observed_phase := existing.observed_phase;
        cleanup_phase := existing.cleanup_phase; environment_id := existing.environment_id; expires_at := existing.expires_at;
        resource_version := existing.resource_version; created_at := existing.created_at; updated_at := existing.updated_at;
        RETURN NEXT; RETURN;
    END IF;
    PERFORM 1 FROM cloud_agents.projects AS project
    WHERE project.tenant_id = p_tenant_id AND project.project_uid = p_project_uid AND project.state = 'active'
    FOR KEY SHARE;
    IF NOT FOUND THEN RAISE EXCEPTION USING ERRCODE = '23503', MESSAGE = 'project is absent or inactive'; END IF;
    INSERT INTO cloud_agents.managed_host_environment_leases (
        tenant_id, tenant_ref_id, project_uid, lease_uid, lease_name, release_digest, generation,
        desired_phase, observed_phase, cleanup_phase, environment_id, expires_at, resource_version,
        create_idempotency_key, create_request_digest, created_at, updated_at
    ) VALUES (
        p_tenant_id, p_tenant_id, p_project_uid, p_lease_uid, p_lease_name, p_release_digest, 1,
        'active', 'provisioning', 'none', p_lease_uid,
        mutation_at + make_interval(secs => p_ttl_seconds), 1,
        p_idempotency_key, p_request_digest, mutation_at, mutation_at
    ) RETURNING managed_host_environment_leases.* INTO existing;
    lease_uid := existing.lease_uid; lease_name := existing.lease_name; release_digest := existing.release_digest;
    generation := existing.generation; desired_phase := existing.desired_phase; observed_phase := existing.observed_phase;
    cleanup_phase := existing.cleanup_phase; environment_id := existing.environment_id; expires_at := existing.expires_at;
    resource_version := existing.resource_version; created_at := existing.created_at; updated_at := existing.updated_at;
    RETURN NEXT;
END;
$cloud_agents_function$;

CREATE FUNCTION cloud_agents.terminate_managed_host_environment_lease_v1(
    p_tenant_id text, p_project_uid text, p_lease_uid text, p_expected_generation bigint,
    p_idempotency_key text, p_request_digest text
)
RETURNS TABLE (
    lease_uid text, lease_name text, release_digest text, generation bigint,
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
        OR p_expected_generation < 1
        OR p_idempotency_key !~ '^[A-Za-z0-9._~-]{16,128}$'
        OR p_request_digest !~ '^sha256:[0-9a-f]{64}$'
    THEN RAISE EXCEPTION USING ERRCODE = '22023', MESSAGE = 'managed host environment lease input is invalid'; END IF;
    SELECT lease.* INTO existing FROM cloud_agents.managed_host_environment_leases AS lease
    WHERE lease.tenant_id = p_tenant_id AND lease.project_uid = p_project_uid AND lease.lease_uid = p_lease_uid FOR UPDATE;
    IF NOT FOUND THEN RETURN; END IF;
    IF existing.terminate_idempotency_key IS NOT NULL THEN
        IF existing.terminate_idempotency_key <> p_idempotency_key OR existing.terminate_request_digest IS DISTINCT FROM p_request_digest THEN
            RAISE EXCEPTION USING ERRCODE = '23505', MESSAGE = 'managed host environment lease termination idempotency conflict';
        END IF;
    ELSIF existing.generation <> p_expected_generation OR existing.observed_phase <> 'provisioning' OR existing.desired_phase <> 'active' THEN
        RAISE EXCEPTION USING ERRCODE = '23505', MESSAGE = 'managed host environment lease termination transition is invalid';
    ELSE
        UPDATE cloud_agents.managed_host_environment_leases AS lease
        SET desired_phase = 'terminated', observed_phase = 'terminated', cleanup_phase = 'complete',
            generation = existing.generation + 1, resource_version = existing.resource_version + 1,
            terminate_idempotency_key = p_idempotency_key, terminate_request_digest = p_request_digest, updated_at = mutation_at
        WHERE lease.tenant_id = p_tenant_id AND lease.project_uid = p_project_uid AND lease.lease_uid = p_lease_uid;
        existing.desired_phase := 'terminated'; existing.observed_phase := 'terminated'; existing.cleanup_phase := 'complete';
        existing.generation := existing.generation + 1; existing.resource_version := existing.resource_version + 1;
        existing.terminate_idempotency_key := p_idempotency_key; existing.terminate_request_digest := p_request_digest; existing.updated_at := mutation_at;
    END IF;
    lease_uid := existing.lease_uid; lease_name := existing.lease_name; release_digest := existing.release_digest;
    generation := existing.generation; desired_phase := existing.desired_phase; observed_phase := existing.observed_phase;
    cleanup_phase := existing.cleanup_phase; environment_id := existing.environment_id; expires_at := existing.expires_at;
    resource_version := existing.resource_version; created_at := existing.created_at; updated_at := existing.updated_at;
    RETURN NEXT;
END;
$cloud_agents_function$;

ALTER FUNCTION cloud_agents.create_managed_host_environment_lease_v1(text, text, text, text, text, bigint, text, text) OWNER TO cloud_agents_migration_owner;
ALTER FUNCTION cloud_agents.terminate_managed_host_environment_lease_v1(text, text, text, bigint, text, text) OWNER TO cloud_agents_migration_owner;
REVOKE ALL ON FUNCTION cloud_agents.create_managed_host_environment_lease_v1(text, text, text, text, text, bigint, text, text) FROM PUBLIC;
REVOKE ALL ON FUNCTION cloud_agents.terminate_managed_host_environment_lease_v1(text, text, text, bigint, text, text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION cloud_agents.create_managed_host_environment_lease_v1(text, text, text, text, text, bigint, text, text) TO cloud_agents_runtime;
GRANT EXECUTE ON FUNCTION cloud_agents.terminate_managed_host_environment_lease_v1(text, text, text, bigint, text, text) TO cloud_agents_runtime;
