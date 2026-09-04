CREATE TABLE cloud_agents.project_lease_quotas (
    tenant_id text NOT NULL,
    project_uid text NOT NULL,
    quota_uid text NOT NULL,
    quota_name text NOT NULL,
    max_concurrent_leases bigint NOT NULL,
    max_cpu_millis bigint NOT NULL,
    max_memory_bytes bigint NOT NULL,
    max_lease_ttl_seconds bigint NOT NULL,
    resource_version bigint NOT NULL,
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL,
    PRIMARY KEY (tenant_id, project_uid),
    UNIQUE (tenant_id, project_uid, quota_uid),
    CONSTRAINT project_lease_quotas_project_fk FOREIGN KEY (tenant_id, project_uid)
        REFERENCES cloud_agents.projects (tenant_id, project_uid) ON UPDATE RESTRICT ON DELETE RESTRICT,
    CONSTRAINT project_lease_quotas_uid CHECK (cloud_agents.is_valid_identifier(quota_uid)),
    CONSTRAINT project_lease_quotas_name CHECK (cloud_agents.is_valid_identifier(quota_name)),
    CONSTRAINT project_lease_quotas_concurrent CHECK (max_concurrent_leases BETWEEN 1 AND 8000),
    CONSTRAINT project_lease_quotas_cpu CHECK (max_cpu_millis BETWEEN 100 AND 512000000),
    CONSTRAINT project_lease_quotas_memory CHECK (max_memory_bytes BETWEEN 134217728 AND 8796093022208000),
    CONSTRAINT project_lease_quotas_ttl CHECK (max_lease_ttl_seconds BETWEEN 60 AND 86400),
    CONSTRAINT project_lease_quotas_resource_version CHECK (resource_version > 0),
    CONSTRAINT project_lease_quotas_time CHECK (updated_at >= created_at)
);

CREATE TABLE cloud_agents.project_lease_quota_activity (
    tenant_id text NOT NULL,
    project_uid text NOT NULL,
    quota_uid text NOT NULL,
    event_uid text NOT NULL,
    operation_uid text NOT NULL,
    action text NOT NULL,
    idempotency_key text NOT NULL,
    request_id text NOT NULL,
    request_digest text NOT NULL,
    subject_digest text NOT NULL,
    quota_resource_version bigint NOT NULL,
    max_concurrent_leases bigint NOT NULL,
    max_cpu_millis bigint NOT NULL,
    max_memory_bytes bigint NOT NULL,
    max_lease_ttl_seconds bigint NOT NULL,
    quota_created_at timestamptz NOT NULL,
    quota_updated_at timestamptz NOT NULL,
    result text NOT NULL,
    occurred_at timestamptz NOT NULL,
    PRIMARY KEY (tenant_id, project_uid, quota_uid, event_uid),
    UNIQUE (tenant_id, project_uid, idempotency_key),
    CONSTRAINT project_lease_quota_activity_quota_fk FOREIGN KEY (tenant_id, project_uid, quota_uid)
        REFERENCES cloud_agents.project_lease_quotas (tenant_id, project_uid, quota_uid)
        ON UPDATE RESTRICT ON DELETE RESTRICT,
    CONSTRAINT project_lease_quota_activity_event_uid CHECK (cloud_agents.is_valid_identifier(event_uid)),
    CONSTRAINT project_lease_quota_activity_operation_uid CHECK (cloud_agents.is_valid_identifier(operation_uid)),
    CONSTRAINT project_lease_quota_activity_action CHECK (action = 'quota.set'),
    CONSTRAINT project_lease_quota_activity_key CHECK (idempotency_key ~ '^[A-Za-z0-9._~-]{16,128}$'),
    CONSTRAINT project_lease_quota_activity_request_id CHECK (cloud_agents.is_valid_identifier(request_id)),
    CONSTRAINT project_lease_quota_activity_request_digest CHECK (request_digest ~ '^sha256:[0-9a-f]{64}$'),
    CONSTRAINT project_lease_quota_activity_subject_digest CHECK (subject_digest ~ '^sha256:[0-9a-f]{64}$'),
    CONSTRAINT project_lease_quota_activity_version CHECK (quota_resource_version > 0),
    CONSTRAINT project_lease_quota_activity_limits CHECK (
        max_concurrent_leases BETWEEN 1 AND 8000
        AND max_cpu_millis BETWEEN 100 AND 512000000
        AND max_memory_bytes BETWEEN 134217728 AND 8796093022208000
        AND max_lease_ttl_seconds BETWEEN 60 AND 86400
    ),
    CONSTRAINT project_lease_quota_activity_result CHECK (result = 'succeeded'),
    CONSTRAINT project_lease_quota_activity_time CHECK (
        quota_updated_at >= quota_created_at AND occurred_at = quota_updated_at
    )
);

CREATE INDEX project_lease_quota_activity_audit_idx
    ON cloud_agents.project_lease_quota_activity
    (tenant_id, project_uid, occurred_at DESC, event_uid DESC);

ALTER TABLE cloud_agents.project_lease_quotas OWNER TO cloud_agents_migration_owner;
ALTER TABLE cloud_agents.project_lease_quota_activity OWNER TO cloud_agents_migration_owner;
ALTER TABLE cloud_agents.project_lease_quotas ENABLE ROW LEVEL SECURITY;
ALTER TABLE cloud_agents.project_lease_quotas FORCE ROW LEVEL SECURITY;
ALTER TABLE cloud_agents.project_lease_quota_activity ENABLE ROW LEVEL SECURITY;
ALTER TABLE cloud_agents.project_lease_quota_activity FORCE ROW LEVEL SECURITY;
CREATE POLICY project_lease_quotas_runtime_tenant ON cloud_agents.project_lease_quotas
    TO cloud_agents_runtime USING (tenant_id = cloud_agents.require_tenant_id())
    WITH CHECK (tenant_id = cloud_agents.require_tenant_id());
CREATE POLICY project_lease_quotas_migration_owner ON cloud_agents.project_lease_quotas
    TO cloud_agents_migration_owner USING (true) WITH CHECK (true);
CREATE POLICY project_lease_quota_activity_runtime_tenant ON cloud_agents.project_lease_quota_activity
    TO cloud_agents_runtime USING (tenant_id = cloud_agents.require_tenant_id())
    WITH CHECK (tenant_id = cloud_agents.require_tenant_id());
CREATE POLICY project_lease_quota_activity_migration_owner ON cloud_agents.project_lease_quota_activity
    TO cloud_agents_migration_owner USING (true) WITH CHECK (true);
REVOKE ALL ON TABLE cloud_agents.project_lease_quotas FROM PUBLIC;
REVOKE ALL ON TABLE cloud_agents.project_lease_quota_activity FROM PUBLIC;
GRANT SELECT ON TABLE cloud_agents.project_lease_quotas TO cloud_agents_runtime;
GRANT SELECT ON TABLE cloud_agents.project_lease_quota_activity TO cloud_agents_runtime;

CREATE FUNCTION cloud_agents.set_project_lease_quota_v1(
    p_tenant_id text, p_project_uid text, p_expected_resource_version bigint,
    p_max_concurrent_leases bigint, p_max_cpu_millis bigint,
    p_max_memory_bytes bigint, p_max_lease_ttl_seconds bigint,
    p_idempotency_key text, p_request_digest text, p_request_id text, p_subject_digest text
)
RETURNS TABLE (
    quota_uid text, quota_name text, max_concurrent_leases bigint,
    max_cpu_millis bigint, max_memory_bytes bigint, max_lease_ttl_seconds bigint,
    active_leases bigint, used_cpu_millis bigint, used_memory_bytes bigint,
    resource_version bigint, created_at timestamptz, updated_at timestamptz
)
LANGUAGE plpgsql SECURITY DEFINER SET search_path = pg_catalog, cloud_agents
AS $cloud_agents_function$
DECLARE
    ignored_principal text;
    mutation_at timestamptz;
    existing cloud_agents.project_lease_quotas%ROWTYPE;
    replay cloud_agents.project_lease_quota_activity%ROWTYPE;
    operation_uid_value text;
BEGIN
    ignored_principal := cloud_agents.require_runtime_mutation_principal();
    mutation_at := pg_catalog.transaction_timestamp();
    IF p_tenant_id IS DISTINCT FROM cloud_agents.require_tenant_id()
        OR NOT cloud_agents.is_valid_identifier(p_project_uid)
        OR p_expected_resource_version < 0
        OR p_max_concurrent_leases NOT BETWEEN 1 AND 8000
        OR p_max_cpu_millis NOT BETWEEN 100 AND 512000000
        OR p_max_memory_bytes NOT BETWEEN 134217728 AND 8796093022208000
        OR p_max_lease_ttl_seconds NOT BETWEEN 60 AND 86400
        OR p_idempotency_key !~ '^[A-Za-z0-9._~-]{16,128}$'
        OR p_request_digest !~ '^sha256:[0-9a-f]{64}$'
        OR NOT cloud_agents.is_valid_identifier(p_request_id)
        OR p_subject_digest !~ '^sha256:[0-9a-f]{64}$'
    THEN RAISE EXCEPTION USING ERRCODE = '22023', MESSAGE = 'project lease quota input is invalid'; END IF;

    SELECT activity.* INTO replay
    FROM cloud_agents.project_lease_quota_activity AS activity
    WHERE activity.tenant_id = p_tenant_id AND activity.project_uid = p_project_uid
        AND activity.idempotency_key = p_idempotency_key
    FOR SHARE;
    IF FOUND THEN
        IF replay.request_digest IS DISTINCT FROM p_request_digest THEN
            RAISE EXCEPTION USING ERRCODE = '23505', MESSAGE = 'project lease quota idempotency conflict';
        END IF;
        quota_uid := replay.quota_uid;
        quota_name := 'project-lease-quota';
        max_concurrent_leases := replay.max_concurrent_leases;
        max_cpu_millis := replay.max_cpu_millis;
        max_memory_bytes := replay.max_memory_bytes;
        max_lease_ttl_seconds := replay.max_lease_ttl_seconds;
        resource_version := replay.quota_resource_version;
        created_at := replay.quota_created_at;
        updated_at := replay.quota_updated_at;
    ELSE
        PERFORM 1 FROM cloud_agents.projects AS project
        WHERE project.tenant_id = p_tenant_id AND project.project_uid = p_project_uid
            AND project.state = 'active'
        FOR KEY SHARE;
        IF NOT FOUND THEN RAISE EXCEPTION USING ERRCODE = '23503', MESSAGE = 'project is absent or inactive'; END IF;

        SELECT quota.* INTO existing
        FROM cloud_agents.project_lease_quotas AS quota
        WHERE quota.tenant_id = p_tenant_id AND quota.project_uid = p_project_uid
        FOR UPDATE;
        IF FOUND THEN
            IF existing.resource_version <> p_expected_resource_version THEN
                RAISE EXCEPTION USING ERRCODE = '23505', MESSAGE = 'project lease quota resource version conflict';
            END IF;
            UPDATE cloud_agents.project_lease_quotas AS quota
            SET max_concurrent_leases = p_max_concurrent_leases,
                max_cpu_millis = p_max_cpu_millis,
                max_memory_bytes = p_max_memory_bytes,
                max_lease_ttl_seconds = p_max_lease_ttl_seconds,
                resource_version = quota.resource_version + 1,
                updated_at = mutation_at
            WHERE quota.tenant_id = p_tenant_id AND quota.project_uid = p_project_uid
            RETURNING quota.* INTO existing;
        ELSE
            IF p_expected_resource_version <> 0 THEN
                RAISE EXCEPTION USING ERRCODE = '23505', MESSAGE = 'project lease quota resource version conflict';
            END IF;
            INSERT INTO cloud_agents.project_lease_quotas (
                tenant_id, project_uid, quota_uid, quota_name,
                max_concurrent_leases, max_cpu_millis, max_memory_bytes,
                max_lease_ttl_seconds, resource_version, created_at, updated_at
            ) VALUES (
                p_tenant_id, p_project_uid,
                'quota-' || pg_catalog.md5(p_tenant_id || '|' || p_project_uid || '|project-lease-quota'),
                'project-lease-quota', p_max_concurrent_leases, p_max_cpu_millis,
                p_max_memory_bytes, p_max_lease_ttl_seconds, 1, mutation_at, mutation_at
            ) RETURNING project_lease_quotas.* INTO existing;
        END IF;

        operation_uid_value := 'op-' || pg_catalog.md5(
            p_tenant_id || '|' || p_project_uid || '|quota.set|' || p_idempotency_key
        );
        INSERT INTO cloud_agents.project_lease_quota_activity (
            tenant_id, project_uid, quota_uid, event_uid, operation_uid, action,
            idempotency_key, request_id, request_digest, subject_digest,
            quota_resource_version, max_concurrent_leases, max_cpu_millis,
            max_memory_bytes, max_lease_ttl_seconds, quota_created_at,
            quota_updated_at, result, occurred_at
        ) VALUES (
            p_tenant_id, p_project_uid, existing.quota_uid,
            operation_uid_value || '-succeeded', operation_uid_value, 'quota.set',
            p_idempotency_key, p_request_id, p_request_digest, p_subject_digest,
            existing.resource_version, existing.max_concurrent_leases, existing.max_cpu_millis,
            existing.max_memory_bytes, existing.max_lease_ttl_seconds,
            existing.created_at, existing.updated_at, 'succeeded', mutation_at
        );

        quota_uid := existing.quota_uid;
        quota_name := existing.quota_name;
        max_concurrent_leases := existing.max_concurrent_leases;
        max_cpu_millis := existing.max_cpu_millis;
        max_memory_bytes := existing.max_memory_bytes;
        max_lease_ttl_seconds := existing.max_lease_ttl_seconds;
        resource_version := existing.resource_version;
        created_at := existing.created_at;
        updated_at := existing.updated_at;
    END IF;

    SELECT pg_catalog.count(*),
        COALESCE(pg_catalog.sum(lease.cpu_limit_millis), 0),
        COALESCE(pg_catalog.sum(lease.memory_limit_bytes), 0)
    INTO active_leases, used_cpu_millis, used_memory_bytes
    FROM cloud_agents.managed_host_environment_leases AS lease
    WHERE lease.tenant_id = p_tenant_id AND lease.project_uid = p_project_uid
        AND NOT (lease.observed_phase = 'terminated' AND lease.cleanup_phase = 'complete');
    RETURN NEXT;
END;
$cloud_agents_function$;

CREATE FUNCTION cloud_agents.enforce_project_lease_quota_v1()
RETURNS trigger
LANGUAGE plpgsql SECURITY DEFINER SET search_path = pg_catalog, cloud_agents
AS $cloud_agents_function$
DECLARE
    quota cloud_agents.project_lease_quotas%ROWTYPE;
    active_leases bigint;
    used_cpu_millis bigint;
    used_memory_bytes bigint;
    requested_ttl_seconds bigint;
BEGIN
    SELECT configured.* INTO quota
    FROM cloud_agents.project_lease_quotas AS configured
    WHERE configured.tenant_id = NEW.tenant_id AND configured.project_uid = NEW.project_uid
    FOR UPDATE;
    IF NOT FOUND THEN RETURN NEW; END IF;

    IF NEW.cpu_limit_millis IS NULL OR NEW.memory_limit_bytes IS NULL
        OR NEW.created_at IS NULL OR NEW.expires_at IS NULL
    THEN RAISE EXCEPTION USING ERRCODE = '22023', MESSAGE = 'project lease quota input is invalid'; END IF;

    SELECT pg_catalog.count(*),
        COALESCE(pg_catalog.sum(lease.cpu_limit_millis), 0),
        COALESCE(pg_catalog.sum(lease.memory_limit_bytes), 0)
    INTO active_leases, used_cpu_millis, used_memory_bytes
    FROM cloud_agents.managed_host_environment_leases AS lease
    WHERE lease.tenant_id = NEW.tenant_id AND lease.project_uid = NEW.project_uid
        AND NOT (lease.observed_phase = 'terminated' AND lease.cleanup_phase = 'complete');

    requested_ttl_seconds := EXTRACT(EPOCH FROM (NEW.expires_at - NEW.created_at))::bigint;
    IF active_leases >= quota.max_concurrent_leases THEN
        RAISE EXCEPTION USING ERRCODE = '23505', MESSAGE = 'project concurrent lease quota exceeded';
    ELSIF used_cpu_millis + NEW.cpu_limit_millis > quota.max_cpu_millis THEN
        RAISE EXCEPTION USING ERRCODE = '23505', MESSAGE = 'project lease cpu quota exceeded';
    ELSIF used_memory_bytes + NEW.memory_limit_bytes > quota.max_memory_bytes THEN
        RAISE EXCEPTION USING ERRCODE = '23505', MESSAGE = 'project lease memory quota exceeded';
    ELSIF requested_ttl_seconds > quota.max_lease_ttl_seconds THEN
        RAISE EXCEPTION USING ERRCODE = '23505', MESSAGE = 'project lease ttl quota exceeded';
    END IF;
    RETURN NEW;
END;
$cloud_agents_function$;

CREATE TRIGGER managed_host_environment_leases_project_quota
    BEFORE INSERT ON cloud_agents.managed_host_environment_leases
    FOR EACH ROW EXECUTE FUNCTION cloud_agents.enforce_project_lease_quota_v1();

CREATE FUNCTION cloud_agents.create_user_environment_v4(
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
    effective_ttl_seconds bigint;
BEGIN
    ignored_principal := cloud_agents.require_runtime_mutation_principal();
    effective_ttl_seconds := p_ttl_seconds;
    SELECT LEAST(p_ttl_seconds, quota.max_lease_ttl_seconds)
    INTO effective_ttl_seconds
    FROM cloud_agents.project_lease_quotas AS quota
    WHERE quota.tenant_id = p_tenant_id AND quota.project_uid = p_project_uid
    FOR SHARE;
    IF NOT FOUND THEN effective_ttl_seconds := p_ttl_seconds; END IF;

    RETURN QUERY SELECT * FROM cloud_agents.create_user_environment_v3(
        p_tenant_id, p_project_uid, p_environment_uid, p_profile_uid,
        p_profile_version, effective_ttl_seconds, p_idempotency_key, p_request_digest
    );
END;
$cloud_agents_function$;

ALTER FUNCTION cloud_agents.set_project_lease_quota_v1(
    text, text, bigint, bigint, bigint, bigint, bigint, text, text, text, text
) OWNER TO cloud_agents_migration_owner;
ALTER FUNCTION cloud_agents.enforce_project_lease_quota_v1()
    OWNER TO cloud_agents_migration_owner;
ALTER FUNCTION cloud_agents.create_user_environment_v4(text, text, text, text, bigint, bigint, text, text)
    OWNER TO cloud_agents_migration_owner;
REVOKE ALL ON FUNCTION cloud_agents.set_project_lease_quota_v1(
    text, text, bigint, bigint, bigint, bigint, bigint, text, text, text, text
) FROM PUBLIC;
REVOKE ALL ON FUNCTION cloud_agents.enforce_project_lease_quota_v1() FROM PUBLIC;
REVOKE ALL ON FUNCTION cloud_agents.create_user_environment_v4(text, text, text, text, bigint, bigint, text, text)
    FROM PUBLIC;
REVOKE EXECUTE ON FUNCTION cloud_agents.create_user_environment_v3(text, text, text, text, bigint, bigint, text, text)
    FROM cloud_agents_runtime;
GRANT EXECUTE ON FUNCTION cloud_agents.set_project_lease_quota_v1(
    text, text, bigint, bigint, bigint, bigint, bigint, text, text, text, text
) TO cloud_agents_runtime;
GRANT EXECUTE ON FUNCTION cloud_agents.create_user_environment_v4(text, text, text, text, bigint, bigint, text, text)
    TO cloud_agents_runtime;
