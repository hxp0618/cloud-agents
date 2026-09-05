ALTER TABLE cloud_agents.admin_denied_writes DROP CONSTRAINT admin_denied_writes_action_check;
ALTER TABLE cloud_agents.admin_denied_writes ADD CONSTRAINT admin_denied_writes_action_check CHECK (action IN (
    'adminUpgradeEnvironmentLease', 'adminRollbackEnvironmentLease', 'adminRegisterWorkerRelease',
    'adminSetStoragePolicy', 'adminSetNetworkPolicy', 'adminSetProjectLeaseQuota',
    'adminCreateEnvironmentProfile', 'adminPublishEnvironmentProfile', 'adminDisableEnvironmentProfile',
    'adminCreateRuntimeProfile', 'adminPublishRuntimeProfile', 'adminDisableRuntimeProfile',
    'adminRegisterDeploymentTarget', 'adminProbeDeploymentTarget',
    'adminTransitionDeploymentTargetScheduling', 'adminCleanupDeploymentTarget',
    'adminStopSandboxSession', 'adminRebuildSandboxSession', 'adminRevokeSandboxAccessGrant'
));

CREATE TABLE cloud_agents.sandbox_access_grants (
    tenant_id text NOT NULL,
    project_uid text NOT NULL,
    grant_uid text NOT NULL CHECK (cloud_agents.is_valid_identifier(grant_uid)),
    sandbox_uid text NOT NULL,
    sandbox_generation bigint NOT NULL CHECK (sandbox_generation > 0),
    access_kind text NOT NULL CHECK (access_kind = 'pty'),
    token_digest text NOT NULL CHECK (token_digest ~ '^sha256:[0-9a-f]{64}$'),
    status text NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'revoked')),
    resource_version bigint NOT NULL DEFAULT 1 CHECK (resource_version > 0),
    issued_by text NOT NULL CHECK (issued_by ~ '^sha256:[0-9a-f]{64}$'),
    create_idempotency_key text NOT NULL CHECK (cloud_agents.is_valid_identifier(create_idempotency_key)),
    create_request_digest text NOT NULL CHECK (create_request_digest ~ '^sha256:[0-9a-f]{64}$'),
    create_request_id text NOT NULL CHECK (cloud_agents.is_valid_identifier(create_request_id)),
    revoke_idempotency_key text CHECK (revoke_idempotency_key IS NULL OR cloud_agents.is_valid_identifier(revoke_idempotency_key)),
    revoke_request_digest text CHECK (revoke_request_digest IS NULL OR revoke_request_digest ~ '^sha256:[0-9a-f]{64}$'),
    revoke_request_id text CHECK (revoke_request_id IS NULL OR cloud_agents.is_valid_identifier(revoke_request_id)),
    created_at timestamptz NOT NULL DEFAULT transaction_timestamp(),
    updated_at timestamptz NOT NULL DEFAULT transaction_timestamp(),
    expires_at timestamptz NOT NULL,
    revoked_at timestamptz,
    PRIMARY KEY (tenant_id, project_uid, grant_uid),
    UNIQUE (tenant_id, project_uid, issued_by, create_idempotency_key),
    FOREIGN KEY (tenant_id, project_uid, sandbox_uid) REFERENCES cloud_agents.sandbox_sessions
        ON UPDATE RESTRICT ON DELETE RESTRICT,
    CHECK ((status = 'active' AND revoked_at IS NULL AND revoke_idempotency_key IS NULL
            AND revoke_request_digest IS NULL AND revoke_request_id IS NULL)
        OR (status = 'revoked' AND revoked_at IS NOT NULL AND revoke_idempotency_key IS NOT NULL
            AND revoke_request_digest IS NOT NULL AND revoke_request_id IS NOT NULL)),
    CHECK (expires_at > created_at AND updated_at >= created_at)
);
CREATE INDEX sandbox_access_grants_page_idx ON cloud_agents.sandbox_access_grants
    (tenant_id, project_uid, sandbox_uid, created_at DESC, grant_uid DESC);

CREATE TABLE cloud_agents.sandbox_access_grant_activity (
    tenant_id text NOT NULL,
    project_uid text NOT NULL,
    event_uid text NOT NULL CHECK (cloud_agents.is_valid_identifier(event_uid)),
    grant_uid text NOT NULL,
    action text NOT NULL CHECK (action IN ('issued', 'revoked')),
    subject_digest text NOT NULL CHECK (subject_digest ~ '^sha256:[0-9a-f]{64}$'),
    request_id text NOT NULL CHECK (cloud_agents.is_valid_identifier(request_id)),
    occurred_at timestamptz NOT NULL DEFAULT transaction_timestamp(),
    PRIMARY KEY (tenant_id, event_uid),
    FOREIGN KEY (tenant_id, project_uid, grant_uid) REFERENCES cloud_agents.sandbox_access_grants
        ON UPDATE RESTRICT ON DELETE RESTRICT
);
CREATE INDEX sandbox_access_grant_activity_page_idx ON cloud_agents.sandbox_access_grant_activity
    (tenant_id, project_uid, grant_uid, occurred_at DESC, event_uid DESC);

CREATE TABLE cloud_agents.sandbox_pty_sessions (
    tenant_id text NOT NULL,
    project_uid text NOT NULL,
    grant_uid text NOT NULL,
    session_uid text NOT NULL CHECK (cloud_agents.is_valid_identifier(session_uid)),
    sandbox_uid text NOT NULL,
    sandbox_generation bigint NOT NULL CHECK (sandbox_generation > 0),
    created_at timestamptz NOT NULL DEFAULT transaction_timestamp(),
    deleted_at timestamptz,
    PRIMARY KEY (tenant_id, project_uid, grant_uid, session_uid),
    FOREIGN KEY (tenant_id, project_uid, grant_uid) REFERENCES cloud_agents.sandbox_access_grants
        ON UPDATE RESTRICT ON DELETE RESTRICT,
    FOREIGN KEY (tenant_id, project_uid, sandbox_uid) REFERENCES cloud_agents.sandbox_sessions
        ON UPDATE RESTRICT ON DELETE RESTRICT,
    CHECK (deleted_at IS NULL OR deleted_at >= created_at)
);

ALTER TABLE cloud_agents.sandbox_access_grants OWNER TO cloud_agents_migration_owner;
ALTER TABLE cloud_agents.sandbox_access_grant_activity OWNER TO cloud_agents_migration_owner;
ALTER TABLE cloud_agents.sandbox_pty_sessions OWNER TO cloud_agents_migration_owner;
ALTER TABLE cloud_agents.sandbox_access_grants ENABLE ROW LEVEL SECURITY;
ALTER TABLE cloud_agents.sandbox_access_grants FORCE ROW LEVEL SECURITY;
ALTER TABLE cloud_agents.sandbox_access_grant_activity ENABLE ROW LEVEL SECURITY;
ALTER TABLE cloud_agents.sandbox_access_grant_activity FORCE ROW LEVEL SECURITY;
ALTER TABLE cloud_agents.sandbox_pty_sessions ENABLE ROW LEVEL SECURITY;
ALTER TABLE cloud_agents.sandbox_pty_sessions FORCE ROW LEVEL SECURITY;
CREATE POLICY sandbox_access_grants_runtime ON cloud_agents.sandbox_access_grants TO cloud_agents_runtime
    USING (tenant_id = cloud_agents.require_tenant_id()) WITH CHECK (tenant_id = cloud_agents.require_tenant_id());
CREATE POLICY sandbox_access_grants_owner ON cloud_agents.sandbox_access_grants TO cloud_agents_migration_owner
    USING (true) WITH CHECK (true);
CREATE POLICY sandbox_access_grant_activity_runtime ON cloud_agents.sandbox_access_grant_activity TO cloud_agents_runtime
    USING (tenant_id = cloud_agents.require_tenant_id()) WITH CHECK (tenant_id = cloud_agents.require_tenant_id());
CREATE POLICY sandbox_access_grant_activity_owner ON cloud_agents.sandbox_access_grant_activity TO cloud_agents_migration_owner
    USING (true) WITH CHECK (true);
CREATE POLICY sandbox_pty_sessions_runtime ON cloud_agents.sandbox_pty_sessions TO cloud_agents_runtime
    USING (tenant_id = cloud_agents.require_tenant_id()) WITH CHECK (tenant_id = cloud_agents.require_tenant_id());
CREATE POLICY sandbox_pty_sessions_owner ON cloud_agents.sandbox_pty_sessions TO cloud_agents_migration_owner
    USING (true) WITH CHECK (true);
REVOKE ALL ON TABLE cloud_agents.sandbox_access_grants FROM PUBLIC;
REVOKE ALL ON TABLE cloud_agents.sandbox_access_grant_activity FROM PUBLIC;
REVOKE ALL ON TABLE cloud_agents.sandbox_pty_sessions FROM PUBLIC;
GRANT SELECT ON TABLE cloud_agents.sandbox_access_grants TO cloud_agents_runtime;
GRANT SELECT ON TABLE cloud_agents.sandbox_access_grant_activity TO cloud_agents_runtime;
GRANT SELECT ON TABLE cloud_agents.sandbox_pty_sessions TO cloud_agents_runtime;

CREATE FUNCTION cloud_agents.issue_sandbox_access_grant_v1(
    p_project text, p_sandbox text, p_expected_generation bigint, p_grant text,
    p_token_digest text, p_ttl_seconds integer, p_subject text, p_key text,
    p_digest text, p_request text
)
RETURNS TABLE (
    grant_uid text, sandbox_uid text, sandbox_generation bigint, access_kind text,
    status text, resource_version bigint, created_at timestamptz, updated_at timestamptz,
    expires_at timestamptz, revoked_at timestamptz
)
LANGUAGE plpgsql SECURITY DEFINER SET search_path = pg_catalog, cloud_agents
AS $$
DECLARE
    ignored text;
    existing record;
    sandbox record;
    issued_at timestamptz := transaction_timestamp();
BEGIN
    ignored := cloud_agents.require_runtime_mutation_principal();
    IF NOT cloud_agents.is_valid_identifier(p_project)
       OR NOT cloud_agents.is_valid_identifier(p_sandbox)
       OR NOT cloud_agents.is_valid_identifier(p_grant)
       OR p_expected_generation < 1 OR p_expected_generation > 9007199254740991
       OR p_token_digest !~ '^sha256:[0-9a-f]{64}$'
       OR p_ttl_seconds NOT BETWEEN 60 AND 900
       OR p_subject !~ '^sha256:[0-9a-f]{64}$'
       OR NOT cloud_agents.is_valid_identifier(p_key)
       OR p_digest !~ '^sha256:[0-9a-f]{64}$'
       OR NOT cloud_agents.is_valid_identifier(p_request) THEN
        RAISE EXCEPTION USING ERRCODE = '22023', MESSAGE = 'sandbox access grant input is invalid';
    END IF;

    SELECT access_grant.* INTO existing
    FROM cloud_agents.sandbox_access_grants AS access_grant
    WHERE access_grant.tenant_id = cloud_agents.require_tenant_id()
      AND access_grant.project_uid = p_project AND access_grant.issued_by = p_subject
      AND access_grant.create_idempotency_key = p_key
    FOR UPDATE;
    IF FOUND THEN
        IF existing.sandbox_uid <> p_sandbox OR existing.sandbox_generation <> p_expected_generation
           OR existing.grant_uid <> p_grant OR existing.token_digest <> p_token_digest
           OR existing.create_request_digest <> p_digest OR existing.status <> 'active'
           OR existing.expires_at <= clock_timestamp() THEN
            RAISE EXCEPTION USING ERRCODE = '23505', MESSAGE = 'sandbox access grant idempotency conflict';
        END IF;
        RETURN QUERY SELECT existing.grant_uid, existing.sandbox_uid, existing.sandbox_generation,
            existing.access_kind, existing.status, existing.resource_version, existing.created_at,
            existing.updated_at, existing.expires_at, existing.revoked_at;
        RETURN;
    END IF;

    SELECT current_sandbox.generation, current_sandbox.expires_at,
        current_sandbox.observed_generation = current_sandbox.generation
        AND current_sandbox.desired_state = 'running' AND current_sandbox.observed_state = 'running'
        AND NOT current_sandbox.writer_released AND current_sandbox.runtime_state = 'Running'
        AND current_sandbox.runtime_generation = current_sandbox.generation
        AND current_sandbox.runtime_spec_digest = current_sandbox.spec_digest
        AND volume.observed_state = 'available'
        AND operation.state = 'succeeded' AND operation.cleanup_phase = 'complete' AS ready
    INTO sandbox
    FROM cloud_agents.sandbox_sessions AS current_sandbox
    JOIN cloud_agents.workspace_volumes AS volume USING (tenant_id, project_uid, workspace_uid)
    JOIN cloud_agents.platform_operations AS operation
      ON operation.tenant_id = current_sandbox.tenant_id
     AND operation.operation_id = current_sandbox.operation_id
     AND operation.operation_generation = current_sandbox.operation_generation
    WHERE current_sandbox.tenant_id = cloud_agents.require_tenant_id()
      AND current_sandbox.project_uid = p_project AND current_sandbox.sandbox_uid = p_sandbox
    FOR UPDATE OF current_sandbox;
    IF NOT FOUND THEN
        RAISE EXCEPTION USING ERRCODE = '23503', MESSAGE = 'foundation sandbox was not found';
    END IF;
    IF sandbox.generation <> p_expected_generation OR NOT sandbox.ready
       OR sandbox.expires_at IS NOT NULL
       AND sandbox.expires_at < issued_at + pg_catalog.make_interval(secs => p_ttl_seconds) THEN
        RAISE EXCEPTION USING ERRCODE = '23505', MESSAGE = 'sandbox access grant conflicts with current Sandbox authority';
    END IF;

    INSERT INTO cloud_agents.sandbox_access_grants (
        tenant_id, project_uid, grant_uid, sandbox_uid, sandbox_generation, access_kind,
        token_digest, issued_by, create_idempotency_key, create_request_digest,
        create_request_id, created_at, updated_at, expires_at
    ) VALUES (
        cloud_agents.require_tenant_id(), p_project, p_grant, p_sandbox, p_expected_generation,
        'pty', p_token_digest, p_subject, p_key, p_digest, p_request,
        issued_at, issued_at, issued_at + pg_catalog.make_interval(secs => p_ttl_seconds)
    );
    INSERT INTO cloud_agents.sandbox_access_grant_activity (
        tenant_id, project_uid, event_uid, grant_uid, action, subject_digest, request_id, occurred_at
    ) VALUES (
        cloud_agents.require_tenant_id(), p_project, 'grant-issued-' || p_grant,
        p_grant, 'issued', p_subject, p_request, issued_at
    );
    RETURN QUERY SELECT access_grant.grant_uid, access_grant.sandbox_uid, access_grant.sandbox_generation,
        access_grant.access_kind, access_grant.status, access_grant.resource_version, access_grant.created_at,
        access_grant.updated_at, access_grant.expires_at, access_grant.revoked_at
    FROM cloud_agents.sandbox_access_grants AS access_grant
    WHERE access_grant.tenant_id = cloud_agents.require_tenant_id()
      AND access_grant.project_uid = p_project AND access_grant.grant_uid = p_grant;
END;
$$;

CREATE FUNCTION cloud_agents.revoke_sandbox_access_grant_v1(
    p_project text, p_sandbox text, p_grant text, p_expected_generation bigint,
    p_expected_resource_version bigint, p_confirmed_grant text, p_subject text,
    p_key text, p_digest text, p_request text
)
RETURNS TABLE (
    grant_uid text, sandbox_uid text, sandbox_generation bigint, access_kind text,
    status text, resource_version bigint, created_at timestamptz, updated_at timestamptz,
    expires_at timestamptz, revoked_at timestamptz
)
LANGUAGE plpgsql SECURITY DEFINER SET search_path = pg_catalog, cloud_agents
AS $$
DECLARE
    ignored text;
    existing record;
    revoked_at_value timestamptz := transaction_timestamp();
BEGIN
    ignored := cloud_agents.require_runtime_mutation_principal();
    IF NOT cloud_agents.is_valid_identifier(p_project)
       OR NOT cloud_agents.is_valid_identifier(p_sandbox)
       OR NOT cloud_agents.is_valid_identifier(p_grant)
       OR p_expected_generation < 1 OR p_expected_generation > 9007199254740991
       OR p_expected_resource_version < 1 OR p_confirmed_grant <> p_grant
       OR p_subject !~ '^sha256:[0-9a-f]{64}$'
       OR NOT cloud_agents.is_valid_identifier(p_key)
       OR p_digest !~ '^sha256:[0-9a-f]{64}$'
       OR NOT cloud_agents.is_valid_identifier(p_request) THEN
        RAISE EXCEPTION USING ERRCODE = '22023', MESSAGE = 'sandbox access grant revoke input is invalid';
    END IF;
    SELECT access_grant.* INTO existing
    FROM cloud_agents.sandbox_access_grants AS access_grant
    WHERE access_grant.tenant_id = cloud_agents.require_tenant_id()
      AND access_grant.project_uid = p_project AND access_grant.sandbox_uid = p_sandbox
      AND access_grant.grant_uid = p_grant
    FOR UPDATE;
    IF NOT FOUND THEN
        RAISE EXCEPTION USING ERRCODE = '23503', MESSAGE = 'sandbox access grant was not found';
    END IF;
    IF existing.status = 'revoked' THEN
        IF existing.sandbox_generation <> p_expected_generation
           OR existing.revoke_idempotency_key <> p_key
           OR existing.revoke_request_digest <> p_digest THEN
            RAISE EXCEPTION USING ERRCODE = '23505', MESSAGE = 'sandbox access grant revoke conflict';
        END IF;
    ELSE
        IF existing.sandbox_generation <> p_expected_generation
           OR existing.resource_version <> p_expected_resource_version
           OR NOT EXISTS (
                SELECT 1 FROM cloud_agents.sandbox_sessions AS sandbox
                WHERE sandbox.tenant_id = cloud_agents.require_tenant_id()
                  AND sandbox.project_uid = p_project AND sandbox.sandbox_uid = p_sandbox
                  AND sandbox.generation = p_expected_generation
           ) THEN
            RAISE EXCEPTION USING ERRCODE = '23505', MESSAGE = 'sandbox access grant revoke conflict';
        END IF;
        UPDATE cloud_agents.sandbox_access_grants AS access_grant SET
            status = 'revoked', resource_version = access_grant.resource_version + 1,
            revoke_idempotency_key = p_key, revoke_request_digest = p_digest,
            revoke_request_id = p_request, updated_at = revoked_at_value,
            revoked_at = revoked_at_value
        WHERE access_grant.tenant_id = cloud_agents.require_tenant_id()
          AND access_grant.project_uid = p_project AND access_grant.grant_uid = p_grant;
        INSERT INTO cloud_agents.sandbox_access_grant_activity (
            tenant_id, project_uid, event_uid, grant_uid, action, subject_digest, request_id, occurred_at
        ) VALUES (
            cloud_agents.require_tenant_id(), p_project, 'grant-revoked-' || p_grant,
            p_grant, 'revoked', p_subject, p_request, revoked_at_value
        );
    END IF;
    RETURN QUERY SELECT access_grant.grant_uid, access_grant.sandbox_uid, access_grant.sandbox_generation,
        access_grant.access_kind, access_grant.status, access_grant.resource_version, access_grant.created_at,
        access_grant.updated_at, access_grant.expires_at, access_grant.revoked_at
    FROM cloud_agents.sandbox_access_grants AS access_grant
    WHERE access_grant.tenant_id = cloud_agents.require_tenant_id()
      AND access_grant.project_uid = p_project AND access_grant.grant_uid = p_grant;
END;
$$;

CREATE FUNCTION cloud_agents.register_sandbox_pty_session_v1(
    p_project text, p_grant text, p_session text, p_token_digest text
)
RETURNS TABLE (session_uid text, sandbox_uid text, sandbox_generation bigint, created_at timestamptz)
LANGUAGE plpgsql SECURITY DEFINER SET search_path = pg_catalog, cloud_agents
AS $$
DECLARE
    ignored text;
    grant_value record;
BEGIN
    ignored := cloud_agents.require_runtime_mutation_principal();
    IF NOT cloud_agents.is_valid_identifier(p_project) OR NOT cloud_agents.is_valid_identifier(p_grant)
       OR NOT cloud_agents.is_valid_identifier(p_session) OR p_token_digest !~ '^sha256:[0-9a-f]{64}$' THEN
        RAISE EXCEPTION USING ERRCODE = '22023', MESSAGE = 'sandbox PTY session input is invalid';
    END IF;
    SELECT access_grant.sandbox_uid, access_grant.sandbox_generation INTO grant_value
    FROM cloud_agents.sandbox_access_grants AS access_grant
    JOIN cloud_agents.sandbox_sessions AS sandbox
      ON sandbox.tenant_id = access_grant.tenant_id AND sandbox.project_uid = access_grant.project_uid
     AND sandbox.sandbox_uid = access_grant.sandbox_uid
    WHERE access_grant.tenant_id = cloud_agents.require_tenant_id()
      AND access_grant.project_uid = p_project AND access_grant.grant_uid = p_grant
      AND access_grant.token_digest = p_token_digest AND access_grant.status = 'active'
      AND access_grant.expires_at > clock_timestamp()
      AND sandbox.generation = access_grant.sandbox_generation
      AND sandbox.observed_generation = sandbox.generation
      AND sandbox.desired_state = 'running' AND sandbox.observed_state = 'running'
      AND NOT sandbox.writer_released AND sandbox.runtime_state = 'Running'
    FOR UPDATE OF access_grant;
    IF NOT FOUND THEN
        RAISE EXCEPTION USING ERRCODE = '28000', MESSAGE = 'sandbox access grant is unavailable';
    END IF;
    IF (SELECT count(*) FROM cloud_agents.sandbox_pty_sessions AS session
        WHERE session.tenant_id = cloud_agents.require_tenant_id()
          AND session.project_uid = p_project AND session.grant_uid = p_grant
          AND session.deleted_at IS NULL) >= 10000 THEN
        RAISE EXCEPTION USING ERRCODE = '54000', MESSAGE = 'sandbox PTY session limit exceeded';
    END IF;
    INSERT INTO cloud_agents.sandbox_pty_sessions (
        tenant_id, project_uid, grant_uid, session_uid, sandbox_uid, sandbox_generation
    ) VALUES (
        cloud_agents.require_tenant_id(), p_project, p_grant, p_session,
        grant_value.sandbox_uid, grant_value.sandbox_generation
    ) ON CONFLICT ON CONSTRAINT sandbox_pty_sessions_pkey DO NOTHING;
    RETURN QUERY SELECT session.session_uid, session.sandbox_uid, session.sandbox_generation,
        session.created_at
    FROM cloud_agents.sandbox_pty_sessions AS session
    WHERE session.tenant_id = cloud_agents.require_tenant_id()
      AND session.project_uid = p_project AND session.grant_uid = p_grant
      AND session.session_uid = p_session AND session.deleted_at IS NULL;
    IF NOT FOUND THEN
        RAISE EXCEPTION USING ERRCODE = '23505', MESSAGE = 'sandbox PTY session conflict';
    END IF;
END;
$$;

CREATE FUNCTION cloud_agents.delete_sandbox_pty_session_v1(
    p_project text, p_grant text, p_session text, p_token_digest text
)
RETURNS timestamptz
LANGUAGE plpgsql SECURITY DEFINER SET search_path = pg_catalog, cloud_agents
AS $$
DECLARE
    ignored text;
    deleted_at_value timestamptz;
BEGIN
    ignored := cloud_agents.require_runtime_mutation_principal();
	IF NOT cloud_agents.is_valid_identifier(p_project) OR NOT cloud_agents.is_valid_identifier(p_grant)
	   OR NOT cloud_agents.is_valid_identifier(p_session) OR p_token_digest !~ '^sha256:[0-9a-f]{64}$' THEN
		RAISE EXCEPTION USING ERRCODE = '22023', MESSAGE = 'sandbox PTY session input is invalid';
	END IF;
    UPDATE cloud_agents.sandbox_pty_sessions AS session SET
        deleted_at = COALESCE(session.deleted_at, transaction_timestamp())
    FROM cloud_agents.sandbox_access_grants AS access_grant,
         cloud_agents.sandbox_sessions AS sandbox
    WHERE session.tenant_id = cloud_agents.require_tenant_id()
      AND session.project_uid = p_project AND session.grant_uid = p_grant
      AND session.session_uid = p_session
      AND access_grant.tenant_id = session.tenant_id AND access_grant.project_uid = session.project_uid
      AND access_grant.grant_uid = session.grant_uid AND access_grant.token_digest = p_token_digest
      AND access_grant.status = 'active' AND access_grant.expires_at > clock_timestamp()
      AND sandbox.tenant_id = access_grant.tenant_id AND sandbox.project_uid = access_grant.project_uid
      AND sandbox.sandbox_uid = access_grant.sandbox_uid AND sandbox.generation = access_grant.sandbox_generation
    RETURNING session.deleted_at INTO deleted_at_value;
    IF NOT FOUND THEN
        RAISE EXCEPTION USING ERRCODE = '28000', MESSAGE = 'sandbox PTY session is unavailable';
    END IF;
    RETURN deleted_at_value;
END;
$$;

REVOKE ALL ON FUNCTION cloud_agents.issue_sandbox_access_grant_v1(text,text,bigint,text,text,integer,text,text,text,text) FROM PUBLIC;
REVOKE ALL ON FUNCTION cloud_agents.revoke_sandbox_access_grant_v1(text,text,text,bigint,bigint,text,text,text,text,text) FROM PUBLIC;
REVOKE ALL ON FUNCTION cloud_agents.register_sandbox_pty_session_v1(text,text,text,text) FROM PUBLIC;
REVOKE ALL ON FUNCTION cloud_agents.delete_sandbox_pty_session_v1(text,text,text,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION cloud_agents.issue_sandbox_access_grant_v1(text,text,bigint,text,text,integer,text,text,text,text) TO cloud_agents_runtime;
GRANT EXECUTE ON FUNCTION cloud_agents.revoke_sandbox_access_grant_v1(text,text,text,bigint,bigint,text,text,text,text,text) TO cloud_agents_runtime;
GRANT EXECUTE ON FUNCTION cloud_agents.register_sandbox_pty_session_v1(text,text,text,text) TO cloud_agents_runtime;
GRANT EXECUTE ON FUNCTION cloud_agents.delete_sandbox_pty_session_v1(text,text,text,text) TO cloud_agents_runtime;
