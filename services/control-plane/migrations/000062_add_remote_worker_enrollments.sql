ALTER TABLE cloud_agents.admin_denied_writes DROP CONSTRAINT admin_denied_writes_action_check;
ALTER TABLE cloud_agents.admin_denied_writes ADD CONSTRAINT admin_denied_writes_action_check CHECK (action IN (
    'adminUpgradeEnvironmentLease', 'adminRollbackEnvironmentLease', 'adminRegisterWorkerRelease',
    'adminSetStoragePolicy', 'adminSetNetworkPolicy', 'adminSetProjectLeaseQuota',
    'adminCreateEnvironmentProfile', 'adminPublishEnvironmentProfile', 'adminDisableEnvironmentProfile',
    'adminCreateRuntimeProfile', 'adminPublishRuntimeProfile', 'adminDisableRuntimeProfile',
    'adminRegisterDeploymentTarget', 'adminProbeDeploymentTarget',
    'adminTransitionDeploymentTargetScheduling', 'adminCleanupDeploymentTarget',
    'adminStopSandboxSession', 'adminRebuildSandboxSession', 'adminRevokeSandboxAccessGrant',
    'adminCreateRemoteWorkerEnrollment', 'adminRevokeRemoteWorkerEnrollment'
));

CREATE TABLE cloud_agents.remote_worker_enrollments (
    tenant_id text NOT NULL,
    project_uid text NOT NULL,
    enrollment_uid text NOT NULL CHECK (cloud_agents.is_valid_identifier(enrollment_uid)),
    worker_uid text NOT NULL CHECK (cloud_agents.is_valid_identifier(worker_uid)),
    worker_name text NOT NULL CHECK (cloud_agents.is_valid_identifier(worker_name)),
    state text NOT NULL CHECK (state IN ('pending', 'secret-issued', 'enrolled', 'revoked')),
    secret_digest text CHECK (secret_digest IS NULL OR secret_digest ~ '^sha256:[0-9a-f]{64}$'),
    resource_version bigint NOT NULL DEFAULT 1 CHECK (resource_version > 0),
    created_by text NOT NULL CHECK (created_by ~ '^sha256:[0-9a-f]{64}$'),
    create_idempotency_key text NOT NULL CHECK (create_idempotency_key ~ '^[A-Za-z0-9._~-]{16,128}$'),
    create_request_digest text NOT NULL CHECK (create_request_digest ~ '^sha256:[0-9a-f]{64}$'),
    create_request_id text NOT NULL CHECK (cloud_agents.is_valid_identifier(create_request_id)),
    claimed_by text CHECK (claimed_by IS NULL OR claimed_by ~ '^sha256:[0-9a-f]{64}$'),
    claim_idempotency_key text CHECK (claim_idempotency_key IS NULL OR claim_idempotency_key ~ '^[A-Za-z0-9._~-]{16,128}$'),
    claim_request_digest text CHECK (claim_request_digest IS NULL OR claim_request_digest ~ '^sha256:[0-9a-f]{64}$'),
    claim_request_id text CHECK (claim_request_id IS NULL OR cloud_agents.is_valid_identifier(claim_request_id)),
    revoked_by text CHECK (revoked_by IS NULL OR revoked_by ~ '^sha256:[0-9a-f]{64}$'),
    revoke_idempotency_key text CHECK (revoke_idempotency_key IS NULL OR revoke_idempotency_key ~ '^[A-Za-z0-9._~-]{16,128}$'),
    revoke_request_digest text CHECK (revoke_request_digest IS NULL OR revoke_request_digest ~ '^sha256:[0-9a-f]{64}$'),
    revoke_request_id text CHECK (revoke_request_id IS NULL OR cloud_agents.is_valid_identifier(revoke_request_id)),
    created_at timestamptz NOT NULL DEFAULT transaction_timestamp(),
    updated_at timestamptz NOT NULL DEFAULT transaction_timestamp(),
    expires_at timestamptz NOT NULL,
    secret_claimed_at timestamptz,
    enrolled_at timestamptz,
    revoked_at timestamptz,
    PRIMARY KEY (tenant_id, project_uid, enrollment_uid),
    UNIQUE (tenant_id, project_uid, created_by, create_idempotency_key),
    FOREIGN KEY (tenant_id, project_uid) REFERENCES cloud_agents.projects
        ON UPDATE RESTRICT ON DELETE RESTRICT,
    CHECK (expires_at > created_at AND updated_at >= created_at),
    CHECK ((secret_digest IS NULL AND claimed_by IS NULL AND claim_idempotency_key IS NULL
            AND claim_request_digest IS NULL AND claim_request_id IS NULL AND secret_claimed_at IS NULL)
        OR (secret_digest IS NOT NULL AND claimed_by IS NOT NULL AND claim_idempotency_key IS NOT NULL
            AND claim_request_digest IS NOT NULL AND claim_request_id IS NOT NULL
            AND secret_claimed_at IS NOT NULL AND secret_claimed_at < expires_at)),
    CHECK ((state = 'pending' AND secret_digest IS NULL AND enrolled_at IS NULL AND revoked_at IS NULL
            AND revoked_by IS NULL AND revoke_idempotency_key IS NULL AND revoke_request_digest IS NULL AND revoke_request_id IS NULL)
        OR (state = 'secret-issued' AND secret_digest IS NOT NULL AND enrolled_at IS NULL AND revoked_at IS NULL
            AND revoked_by IS NULL AND revoke_idempotency_key IS NULL AND revoke_request_digest IS NULL AND revoke_request_id IS NULL)
        OR (state = 'enrolled' AND secret_digest IS NOT NULL AND enrolled_at IS NOT NULL AND revoked_at IS NULL
            AND revoked_by IS NULL AND revoke_idempotency_key IS NULL AND revoke_request_digest IS NULL AND revoke_request_id IS NULL)
        OR (state = 'revoked' AND enrolled_at IS NULL AND revoked_at IS NOT NULL AND revoked_by IS NOT NULL
            AND revoke_idempotency_key IS NOT NULL AND revoke_request_digest IS NOT NULL AND revoke_request_id IS NOT NULL))
);

CREATE TABLE cloud_agents.remote_worker_enrollment_activity (
    tenant_id text NOT NULL,
    project_uid text NOT NULL,
    enrollment_uid text NOT NULL,
    event_uid text NOT NULL CHECK (cloud_agents.is_valid_identifier(event_uid)),
    operation_uid text NOT NULL CHECK (cloud_agents.is_valid_identifier(operation_uid)),
    action text NOT NULL CHECK (action IN ('remote-worker-enrollment.create', 'remote-worker-enrollment.claim-secret', 'remote-worker-enrollment.revoke')),
    idempotency_key text NOT NULL CHECK (idempotency_key ~ '^[A-Za-z0-9._~-]{16,128}$'),
    request_id text NOT NULL CHECK (cloud_agents.is_valid_identifier(request_id)),
    request_digest text NOT NULL CHECK (request_digest ~ '^sha256:[0-9a-f]{64}$'),
    subject_digest text NOT NULL CHECK (subject_digest ~ '^sha256:[0-9a-f]{64}$'),
    enrollment_resource_version bigint NOT NULL CHECK (enrollment_resource_version > 0),
    result text NOT NULL CHECK (result = 'succeeded'),
    occurred_at timestamptz NOT NULL DEFAULT transaction_timestamp(),
    PRIMARY KEY (tenant_id, project_uid, enrollment_uid, event_uid),
    UNIQUE (tenant_id, project_uid, subject_digest, action, idempotency_key),
    FOREIGN KEY (tenant_id, project_uid, enrollment_uid) REFERENCES cloud_agents.remote_worker_enrollments
        ON UPDATE RESTRICT ON DELETE RESTRICT
);

CREATE INDEX remote_worker_enrollments_page_idx ON cloud_agents.remote_worker_enrollments
    (tenant_id, project_uid, enrollment_uid);
CREATE INDEX remote_worker_enrollment_activity_page_idx ON cloud_agents.remote_worker_enrollment_activity
    (tenant_id, project_uid, enrollment_uid, occurred_at DESC, event_uid DESC);

ALTER TABLE cloud_agents.remote_worker_enrollments OWNER TO cloud_agents_migration_owner;
ALTER TABLE cloud_agents.remote_worker_enrollment_activity OWNER TO cloud_agents_migration_owner;
ALTER TABLE cloud_agents.remote_worker_enrollments ENABLE ROW LEVEL SECURITY;
ALTER TABLE cloud_agents.remote_worker_enrollments FORCE ROW LEVEL SECURITY;
ALTER TABLE cloud_agents.remote_worker_enrollment_activity ENABLE ROW LEVEL SECURITY;
ALTER TABLE cloud_agents.remote_worker_enrollment_activity FORCE ROW LEVEL SECURITY;
CREATE POLICY remote_worker_enrollments_runtime ON cloud_agents.remote_worker_enrollments TO cloud_agents_runtime
    USING (tenant_id = cloud_agents.require_tenant_id()) WITH CHECK (tenant_id = cloud_agents.require_tenant_id());
CREATE POLICY remote_worker_enrollments_owner ON cloud_agents.remote_worker_enrollments TO cloud_agents_migration_owner
    USING (true) WITH CHECK (true);
CREATE POLICY remote_worker_enrollment_activity_runtime ON cloud_agents.remote_worker_enrollment_activity TO cloud_agents_runtime
    USING (tenant_id = cloud_agents.require_tenant_id()) WITH CHECK (tenant_id = cloud_agents.require_tenant_id());
CREATE POLICY remote_worker_enrollment_activity_owner ON cloud_agents.remote_worker_enrollment_activity TO cloud_agents_migration_owner
    USING (true) WITH CHECK (true);
REVOKE ALL ON TABLE cloud_agents.remote_worker_enrollments FROM PUBLIC;
REVOKE ALL ON TABLE cloud_agents.remote_worker_enrollment_activity FROM PUBLIC;
GRANT SELECT ON TABLE cloud_agents.remote_worker_enrollments TO cloud_agents_runtime;
GRANT SELECT ON TABLE cloud_agents.remote_worker_enrollment_activity TO cloud_agents_runtime;

CREATE FUNCTION cloud_agents.create_remote_worker_enrollment_v1(
    p_tenant text, p_project text, p_enrollment text, p_worker text, p_worker_name text,
    p_ttl_seconds integer, p_subject text, p_key text, p_digest text, p_request text
)
RETURNS TABLE (
    enrollment_uid text, worker_uid text, worker_name text, state text,
    resource_version bigint, created_at timestamptz, updated_at timestamptz, expires_at timestamptz,
    secret_claimed_at timestamptz, enrolled_at timestamptz, revoked_at timestamptz
)
LANGUAGE plpgsql SECURITY DEFINER SET search_path = pg_catalog, cloud_agents
AS $$
DECLARE
    ignored text;
    existing cloud_agents.remote_worker_enrollments%ROWTYPE;
    mutation_at timestamptz := transaction_timestamp();
    operation_value text;
BEGIN
    ignored := cloud_agents.require_runtime_mutation_principal();
    IF p_tenant IS DISTINCT FROM cloud_agents.require_tenant_id()
       OR NOT cloud_agents.is_valid_identifier(p_project) OR NOT cloud_agents.is_valid_identifier(p_enrollment)
       OR NOT cloud_agents.is_valid_identifier(p_worker) OR NOT cloud_agents.is_valid_identifier(p_worker_name)
       OR p_ttl_seconds NOT BETWEEN 300 AND 3600 OR p_subject !~ '^sha256:[0-9a-f]{64}$'
       OR p_key !~ '^[A-Za-z0-9._~-]{16,128}$' OR p_digest !~ '^sha256:[0-9a-f]{64}$'
       OR NOT cloud_agents.is_valid_identifier(p_request) THEN
        RAISE EXCEPTION USING ERRCODE = '22023', MESSAGE = 'remote worker enrollment input is invalid';
    END IF;
    SELECT enrollment.* INTO existing
    FROM cloud_agents.remote_worker_enrollments AS enrollment
    WHERE enrollment.tenant_id = p_tenant AND enrollment.project_uid = p_project
      AND enrollment.created_by = p_subject AND enrollment.create_idempotency_key = p_key
    FOR UPDATE;
    IF FOUND THEN
        IF existing.enrollment_uid <> p_enrollment OR existing.worker_uid <> p_worker
           OR existing.worker_name <> p_worker_name OR existing.create_request_digest <> p_digest THEN
            RAISE EXCEPTION USING ERRCODE = '23505', MESSAGE = 'remote worker enrollment idempotency conflict';
        END IF;
    ELSE
        PERFORM 1 FROM cloud_agents.projects AS project
        WHERE project.tenant_id = p_tenant AND project.project_uid = p_project AND project.state = 'active'
        FOR KEY SHARE;
        IF NOT FOUND THEN RAISE EXCEPTION USING ERRCODE = '23503', MESSAGE = 'project is absent or inactive'; END IF;
        INSERT INTO cloud_agents.remote_worker_enrollments (
            tenant_id, project_uid, enrollment_uid, worker_uid, worker_name, state,
            resource_version, created_by, create_idempotency_key, create_request_digest,
            create_request_id, created_at, updated_at, expires_at
        ) VALUES (
            p_tenant, p_project, p_enrollment, p_worker, p_worker_name, 'pending',
            1, p_subject, p_key, p_digest, p_request, mutation_at, mutation_at,
            mutation_at + pg_catalog.make_interval(secs => p_ttl_seconds)
        ) RETURNING remote_worker_enrollments.* INTO existing;
        operation_value := 'op-' || pg_catalog.md5(p_tenant || '|' || p_project || '|create|' || p_key);
        INSERT INTO cloud_agents.remote_worker_enrollment_activity (
            tenant_id, project_uid, enrollment_uid, event_uid, operation_uid, action,
            idempotency_key, request_id, request_digest, subject_digest,
            enrollment_resource_version, result, occurred_at
        ) VALUES (
            p_tenant, p_project, p_enrollment, operation_value || '-succeeded', operation_value,
            'remote-worker-enrollment.create', p_key, p_request, p_digest, p_subject, 1, 'succeeded', mutation_at
        );
    END IF;
    RETURN QUERY SELECT existing.enrollment_uid, existing.worker_uid, existing.worker_name,
        CASE WHEN existing.state IN ('pending', 'secret-issued') AND existing.expires_at <= clock_timestamp()
            THEN 'expired' ELSE existing.state END,
        existing.resource_version, existing.created_at, existing.updated_at, existing.expires_at,
        existing.secret_claimed_at, existing.enrolled_at, existing.revoked_at;
END;
$$;

CREATE FUNCTION cloud_agents.claim_remote_worker_enrollment_secret_v1(
    p_tenant text, p_project text, p_enrollment text, p_expected_version bigint,
    p_confirmed_enrollment text, p_secret_digest text, p_subject text,
    p_key text, p_digest text, p_request text
)
RETURNS TABLE (
    enrollment_uid text, worker_uid text, worker_name text, state text,
    resource_version bigint, created_at timestamptz, updated_at timestamptz, expires_at timestamptz,
    secret_claimed_at timestamptz, enrolled_at timestamptz, revoked_at timestamptz
)
LANGUAGE plpgsql SECURITY DEFINER SET search_path = pg_catalog, cloud_agents
AS $$
DECLARE
    ignored text;
    existing cloud_agents.remote_worker_enrollments%ROWTYPE;
    mutation_at timestamptz := transaction_timestamp();
    operation_value text;
BEGIN
    ignored := cloud_agents.require_runtime_mutation_principal();
    IF p_tenant IS DISTINCT FROM cloud_agents.require_tenant_id()
       OR NOT cloud_agents.is_valid_identifier(p_project) OR NOT cloud_agents.is_valid_identifier(p_enrollment)
       OR p_confirmed_enrollment IS DISTINCT FROM p_enrollment OR p_expected_version < 1
       OR p_secret_digest !~ '^sha256:[0-9a-f]{64}$' OR p_subject !~ '^sha256:[0-9a-f]{64}$'
       OR p_key !~ '^[A-Za-z0-9._~-]{16,128}$' OR p_digest !~ '^sha256:[0-9a-f]{64}$'
       OR NOT cloud_agents.is_valid_identifier(p_request) THEN
        RAISE EXCEPTION USING ERRCODE = '22023', MESSAGE = 'remote worker enrollment claim input is invalid';
    END IF;
    SELECT enrollment.* INTO existing
    FROM cloud_agents.remote_worker_enrollments AS enrollment
    WHERE enrollment.tenant_id = p_tenant AND enrollment.project_uid = p_project
      AND enrollment.enrollment_uid = p_enrollment
    FOR UPDATE;
    IF NOT FOUND THEN RAISE EXCEPTION USING ERRCODE = '23503', MESSAGE = 'remote worker enrollment was not found'; END IF;
    IF existing.state <> 'pending' OR existing.resource_version <> p_expected_version
       OR existing.expires_at <= clock_timestamp() THEN
        RAISE EXCEPTION USING ERRCODE = '23505', MESSAGE = 'remote worker enrollment secret is unavailable';
    END IF;
    UPDATE cloud_agents.remote_worker_enrollments AS enrollment
    SET state = 'secret-issued', secret_digest = p_secret_digest, resource_version = enrollment.resource_version + 1,
        claimed_by = p_subject, claim_idempotency_key = p_key, claim_request_digest = p_digest,
        claim_request_id = p_request, updated_at = mutation_at, secret_claimed_at = mutation_at
    WHERE enrollment.tenant_id = p_tenant AND enrollment.project_uid = p_project
      AND enrollment.enrollment_uid = p_enrollment
    RETURNING enrollment.* INTO existing;
    operation_value := 'op-' || pg_catalog.md5(p_tenant || '|' || p_project || '|claim-secret|' || p_key);
    INSERT INTO cloud_agents.remote_worker_enrollment_activity (
        tenant_id, project_uid, enrollment_uid, event_uid, operation_uid, action,
        idempotency_key, request_id, request_digest, subject_digest,
        enrollment_resource_version, result, occurred_at
    ) VALUES (
        p_tenant, p_project, p_enrollment, operation_value || '-succeeded', operation_value,
        'remote-worker-enrollment.claim-secret', p_key, p_request, p_digest, p_subject,
        existing.resource_version, 'succeeded', mutation_at
    );
    RETURN QUERY SELECT existing.enrollment_uid, existing.worker_uid, existing.worker_name, existing.state,
        existing.resource_version, existing.created_at, existing.updated_at, existing.expires_at,
        existing.secret_claimed_at, existing.enrolled_at, existing.revoked_at;
END;
$$;

CREATE FUNCTION cloud_agents.revoke_remote_worker_enrollment_v1(
    p_tenant text, p_project text, p_enrollment text, p_expected_version bigint,
    p_confirmed_enrollment text, p_subject text, p_key text, p_digest text, p_request text
)
RETURNS TABLE (
    enrollment_uid text, worker_uid text, worker_name text, state text,
    resource_version bigint, created_at timestamptz, updated_at timestamptz, expires_at timestamptz,
    secret_claimed_at timestamptz, enrolled_at timestamptz, revoked_at timestamptz
)
LANGUAGE plpgsql SECURITY DEFINER SET search_path = pg_catalog, cloud_agents
AS $$
DECLARE
    ignored text;
    existing cloud_agents.remote_worker_enrollments%ROWTYPE;
    mutation_at timestamptz := transaction_timestamp();
    operation_value text;
BEGIN
    ignored := cloud_agents.require_runtime_mutation_principal();
    IF p_tenant IS DISTINCT FROM cloud_agents.require_tenant_id()
       OR NOT cloud_agents.is_valid_identifier(p_project) OR NOT cloud_agents.is_valid_identifier(p_enrollment)
       OR p_confirmed_enrollment IS DISTINCT FROM p_enrollment OR p_expected_version < 1
       OR p_subject !~ '^sha256:[0-9a-f]{64}$' OR p_key !~ '^[A-Za-z0-9._~-]{16,128}$'
       OR p_digest !~ '^sha256:[0-9a-f]{64}$' OR NOT cloud_agents.is_valid_identifier(p_request) THEN
        RAISE EXCEPTION USING ERRCODE = '22023', MESSAGE = 'remote worker enrollment revoke input is invalid';
    END IF;
    SELECT enrollment.* INTO existing
    FROM cloud_agents.remote_worker_enrollments AS enrollment
    WHERE enrollment.tenant_id = p_tenant AND enrollment.project_uid = p_project
      AND enrollment.enrollment_uid = p_enrollment
    FOR UPDATE;
    IF NOT FOUND THEN RAISE EXCEPTION USING ERRCODE = '23503', MESSAGE = 'remote worker enrollment was not found'; END IF;
    IF existing.state = 'revoked' THEN
        IF existing.revoked_by <> p_subject OR existing.revoke_idempotency_key <> p_key
           OR existing.revoke_request_digest <> p_digest THEN
            RAISE EXCEPTION USING ERRCODE = '23505', MESSAGE = 'remote worker enrollment revoke conflict';
        END IF;
    ELSE
        IF existing.state NOT IN ('pending', 'secret-issued') OR existing.resource_version <> p_expected_version THEN
            RAISE EXCEPTION USING ERRCODE = '23505', MESSAGE = 'remote worker enrollment revoke conflict';
        END IF;
        UPDATE cloud_agents.remote_worker_enrollments AS enrollment
        SET state = 'revoked', resource_version = enrollment.resource_version + 1,
            revoked_by = p_subject, revoke_idempotency_key = p_key, revoke_request_digest = p_digest,
            revoke_request_id = p_request, updated_at = mutation_at, revoked_at = mutation_at
        WHERE enrollment.tenant_id = p_tenant AND enrollment.project_uid = p_project
          AND enrollment.enrollment_uid = p_enrollment
        RETURNING enrollment.* INTO existing;
        operation_value := 'op-' || pg_catalog.md5(p_tenant || '|' || p_project || '|revoke|' || p_key);
        INSERT INTO cloud_agents.remote_worker_enrollment_activity (
            tenant_id, project_uid, enrollment_uid, event_uid, operation_uid, action,
            idempotency_key, request_id, request_digest, subject_digest,
            enrollment_resource_version, result, occurred_at
        ) VALUES (
            p_tenant, p_project, p_enrollment, operation_value || '-succeeded', operation_value,
            'remote-worker-enrollment.revoke', p_key, p_request, p_digest, p_subject,
            existing.resource_version, 'succeeded', mutation_at
        );
    END IF;
    RETURN QUERY SELECT existing.enrollment_uid, existing.worker_uid, existing.worker_name, existing.state,
        existing.resource_version, existing.created_at, existing.updated_at, existing.expires_at,
        existing.secret_claimed_at, existing.enrolled_at, existing.revoked_at;
END;
$$;

ALTER FUNCTION cloud_agents.create_remote_worker_enrollment_v1(text,text,text,text,text,integer,text,text,text,text) OWNER TO cloud_agents_migration_owner;
ALTER FUNCTION cloud_agents.claim_remote_worker_enrollment_secret_v1(text,text,text,bigint,text,text,text,text,text,text) OWNER TO cloud_agents_migration_owner;
ALTER FUNCTION cloud_agents.revoke_remote_worker_enrollment_v1(text,text,text,bigint,text,text,text,text,text) OWNER TO cloud_agents_migration_owner;
REVOKE ALL ON FUNCTION cloud_agents.create_remote_worker_enrollment_v1(text,text,text,text,text,integer,text,text,text,text) FROM PUBLIC;
REVOKE ALL ON FUNCTION cloud_agents.claim_remote_worker_enrollment_secret_v1(text,text,text,bigint,text,text,text,text,text,text) FROM PUBLIC;
REVOKE ALL ON FUNCTION cloud_agents.revoke_remote_worker_enrollment_v1(text,text,text,bigint,text,text,text,text,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION cloud_agents.create_remote_worker_enrollment_v1(text,text,text,text,text,integer,text,text,text,text) TO cloud_agents_runtime;
GRANT EXECUTE ON FUNCTION cloud_agents.claim_remote_worker_enrollment_secret_v1(text,text,text,bigint,text,text,text,text,text,text) TO cloud_agents_runtime;
GRANT EXECUTE ON FUNCTION cloud_agents.revoke_remote_worker_enrollment_v1(text,text,text,bigint,text,text,text,text,text) TO cloud_agents_runtime;
