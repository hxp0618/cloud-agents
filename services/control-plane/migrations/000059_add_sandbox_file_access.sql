ALTER TABLE cloud_agents.sandbox_access_grants DROP CONSTRAINT sandbox_access_grants_access_kind_check;
UPDATE cloud_agents.sandbox_access_grants SET access_kind = 'sandbox' WHERE access_kind = 'pty';
ALTER TABLE cloud_agents.sandbox_access_grants ADD CONSTRAINT sandbox_access_grants_access_kind_check
    CHECK (access_kind = 'sandbox');

ALTER TABLE cloud_agents.sandbox_access_grant_activity DROP CONSTRAINT sandbox_access_grant_activity_action_check;
ALTER TABLE cloud_agents.sandbox_access_grant_activity ADD CONSTRAINT sandbox_access_grant_activity_action_check
    CHECK (action IN ('issued', 'revoked', 'file_list', 'file_read', 'file_write', 'file_delete'));
ALTER TABLE cloud_agents.sandbox_access_grant_activity ADD COLUMN
    outcome text NOT NULL DEFAULT 'succeeded' CHECK (outcome IN ('started', 'succeeded', 'failed'));
ALTER TABLE cloud_agents.sandbox_access_grant_activity ADD COLUMN
    stable_error_code text CHECK (stable_error_code IS NULL OR cloud_agents.is_valid_identifier(stable_error_code));
ALTER TABLE cloud_agents.sandbox_access_grant_activity ADD COLUMN
    bytes_transferred bigint NOT NULL DEFAULT 0 CHECK (bytes_transferred BETWEEN 0 AND 16777216);
ALTER TABLE cloud_agents.sandbox_access_grant_activity ADD COLUMN
    completed_at timestamptz DEFAULT transaction_timestamp();
ALTER TABLE cloud_agents.sandbox_access_grant_activity ADD CONSTRAINT sandbox_access_grant_activity_completion_check CHECK (
    (action IN ('issued', 'revoked') AND outcome = 'succeeded' AND stable_error_code IS NULL
        AND bytes_transferred = 0 AND completed_at >= occurred_at)
    OR
    (action LIKE 'file_%' AND (
        (outcome = 'started' AND stable_error_code IS NULL AND bytes_transferred = 0 AND completed_at IS NULL)
        OR (outcome = 'succeeded' AND stable_error_code IS NULL AND completed_at >= occurred_at)
        OR (outcome = 'failed' AND stable_error_code IS NOT NULL AND completed_at >= occurred_at)
    ))
);

CREATE OR REPLACE FUNCTION cloud_agents.issue_sandbox_access_grant_v1(
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
        'sandbox', p_token_digest, p_subject, p_key, p_digest, p_request,
        issued_at, issued_at, issued_at + pg_catalog.make_interval(secs => p_ttl_seconds)
    );
    INSERT INTO cloud_agents.sandbox_access_grant_activity (
        tenant_id, project_uid, event_uid, grant_uid, action, subject_digest, request_id,
        occurred_at, outcome, completed_at
    ) VALUES (
        cloud_agents.require_tenant_id(), p_project, 'grant-issued-' || p_grant,
        p_grant, 'issued', p_subject, p_request, issued_at, 'succeeded', issued_at
    );
    RETURN QUERY SELECT access_grant.grant_uid, access_grant.sandbox_uid, access_grant.sandbox_generation,
        access_grant.access_kind, access_grant.status, access_grant.resource_version, access_grant.created_at,
        access_grant.updated_at, access_grant.expires_at, access_grant.revoked_at
    FROM cloud_agents.sandbox_access_grants AS access_grant
    WHERE access_grant.tenant_id = cloud_agents.require_tenant_id()
      AND access_grant.project_uid = p_project AND access_grant.grant_uid = p_grant;
END;
$$;

CREATE FUNCTION cloud_agents.start_sandbox_file_access_v1(
    p_project text, p_grant text, p_event text, p_action text,
    p_token_digest text, p_request text
)
RETURNS timestamptz
LANGUAGE plpgsql SECURITY DEFINER SET search_path = pg_catalog, cloud_agents
AS $$
DECLARE
    ignored text;
    started_at_value timestamptz := transaction_timestamp();
BEGIN
    ignored := cloud_agents.require_runtime_mutation_principal();
    IF NOT cloud_agents.is_valid_identifier(p_project) OR NOT cloud_agents.is_valid_identifier(p_grant)
       OR NOT cloud_agents.is_valid_identifier(p_event)
       OR p_action NOT IN ('file_list', 'file_read', 'file_write', 'file_delete')
       OR p_token_digest !~ '^sha256:[0-9a-f]{64}$'
       OR NOT cloud_agents.is_valid_identifier(p_request) THEN
        RAISE EXCEPTION USING ERRCODE = '22023', MESSAGE = 'sandbox file access input is invalid';
    END IF;
    INSERT INTO cloud_agents.sandbox_access_grant_activity (
        tenant_id, project_uid, event_uid, grant_uid, action, subject_digest,
        request_id, occurred_at, outcome, completed_at
    )
    SELECT access_grant.tenant_id, access_grant.project_uid, p_event, access_grant.grant_uid,
        p_action, p_token_digest, p_request, started_at_value, 'started', NULL::timestamptz
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
      AND NOT sandbox.writer_released AND sandbox.runtime_state = 'Running';
    IF NOT FOUND THEN
        RAISE EXCEPTION USING ERRCODE = '28000', MESSAGE = 'sandbox access grant is unavailable';
    END IF;
    RETURN started_at_value;
END;
$$;

CREATE FUNCTION cloud_agents.complete_sandbox_file_access_v1(
    p_project text, p_grant text, p_event text, p_token_digest text,
    p_outcome text, p_error text, p_bytes bigint
)
RETURNS timestamptz
LANGUAGE plpgsql SECURITY DEFINER SET search_path = pg_catalog, cloud_agents
AS $$
DECLARE
    ignored text;
    completed_at_value timestamptz := transaction_timestamp();
BEGIN
    ignored := cloud_agents.require_runtime_mutation_principal();
    IF NOT cloud_agents.is_valid_identifier(p_project) OR NOT cloud_agents.is_valid_identifier(p_grant)
       OR NOT cloud_agents.is_valid_identifier(p_event)
       OR p_token_digest !~ '^sha256:[0-9a-f]{64}$'
       OR p_outcome NOT IN ('succeeded', 'failed')
       OR (p_outcome = 'succeeded' AND p_error IS NOT NULL)
       OR (p_outcome = 'failed' AND (p_error IS NULL OR NOT cloud_agents.is_valid_identifier(p_error)))
       OR p_bytes NOT BETWEEN 0 AND 16777216 THEN
        RAISE EXCEPTION USING ERRCODE = '22023', MESSAGE = 'sandbox file access completion is invalid';
    END IF;
    UPDATE cloud_agents.sandbox_access_grant_activity AS activity SET
        outcome = p_outcome, stable_error_code = p_error,
        bytes_transferred = p_bytes, completed_at = completed_at_value
    FROM cloud_agents.sandbox_access_grants AS access_grant
    WHERE activity.tenant_id = cloud_agents.require_tenant_id()
      AND activity.project_uid = p_project AND activity.grant_uid = p_grant
      AND activity.event_uid = p_event AND activity.action LIKE 'file_%'
      AND activity.outcome = 'started' AND activity.completed_at IS NULL
      AND access_grant.tenant_id = activity.tenant_id
      AND access_grant.project_uid = activity.project_uid
      AND access_grant.grant_uid = activity.grant_uid
      AND access_grant.token_digest = p_token_digest;
    IF NOT FOUND THEN
        RAISE EXCEPTION USING ERRCODE = '28000', MESSAGE = 'sandbox file access completion is unavailable';
    END IF;
    RETURN completed_at_value;
END;
$$;

REVOKE ALL ON FUNCTION cloud_agents.start_sandbox_file_access_v1(text,text,text,text,text,text) FROM PUBLIC;
REVOKE ALL ON FUNCTION cloud_agents.complete_sandbox_file_access_v1(text,text,text,text,text,text,bigint) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION cloud_agents.start_sandbox_file_access_v1(text,text,text,text,text,text) TO cloud_agents_runtime;
GRANT EXECUTE ON FUNCTION cloud_agents.complete_sandbox_file_access_v1(text,text,text,text,text,text,bigint) TO cloud_agents_runtime;
