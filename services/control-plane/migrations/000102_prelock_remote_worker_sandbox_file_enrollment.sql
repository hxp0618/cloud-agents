-- Pre-lock enrollment before the sandbox file access join to match heartbeat lock ordering.

CREATE OR REPLACE FUNCTION cloud_agents.request_remote_worker_sandbox_file_v1(
    p_tenant text, p_project text, p_command text, p_event text, p_grant text,
    p_token_digest text, p_target text, p_workspace text, p_sandbox text,
    p_sandbox_generation bigint, p_runtime text, p_runtime_operation text,
    p_runtime_spec_digest text, p_action text, p_path text, p_read_offset bigint,
    p_read_limit integer, p_read_file_version text, p_write_content bytea,
    p_request_id text
)
RETURNS TABLE (
    command_state text, command_deadline_at timestamptz, bytes_transferred bigint,
    list_entries jsonb, result_read_file_version text, result_read_offset bigint,
    result_read_total_bytes bigint, result_read_content bytea, write_entry jsonb,
    stable_error_code text
)
LANGUAGE plpgsql SECURITY DEFINER SET search_path = pg_catalog, cloud_agents
AS $$
DECLARE
    ignored text;
    existing cloud_agents.remote_worker_sandbox_file_commands%ROWTYPE;
    mutation_at timestamptz := transaction_timestamp();
BEGIN
    ignored := cloud_agents.require_runtime_mutation_principal();
    IF p_tenant IS DISTINCT FROM cloud_agents.require_tenant_id()
       OR NOT cloud_agents.is_valid_identifier(p_project)
       OR NOT cloud_agents.is_valid_identifier(p_command)
       OR NOT cloud_agents.is_valid_identifier(p_event)
       OR NOT cloud_agents.is_valid_identifier(p_grant)
       OR p_token_digest !~ '^sha256:[0-9a-f]{64}$'
       OR NOT cloud_agents.is_valid_identifier(p_target)
       OR NOT cloud_agents.is_valid_identifier(p_workspace)
       OR NOT cloud_agents.is_valid_identifier(p_sandbox)
       OR p_sandbox_generation IS NULL OR p_sandbox_generation < 1
       OR NOT cloud_agents.is_valid_identifier(p_runtime)
       OR NOT cloud_agents.is_valid_identifier(p_runtime_operation)
       OR p_runtime_spec_digest !~ '^sha256:[0-9a-f]{64}$'
       OR p_action IS NULL OR p_action NOT IN ('list', 'read', 'write', 'delete')
       OR p_path IS NULL OR pg_catalog.octet_length(p_path) NOT BETWEEN 1 AND 1024
       OR pg_catalog.strpos(p_path, E'\\') <> 0
       OR pg_catalog.strpos(p_path, E'\n') <> 0 OR pg_catalog.strpos(p_path, E'\r') <> 0
       OR p_path <> '.' AND p_path ~ '(^/|(^|/)\.\.?(/|$)|//)'
       OR p_action <> 'list' AND p_path = '.'
       OR p_action IN ('list', 'delete') AND (p_read_offset IS NOT NULL OR p_read_limit IS NOT NULL
            OR p_read_file_version IS NOT NULL OR p_write_content IS NOT NULL)
       OR p_action = 'read' AND (p_read_offset IS NULL OR p_read_limit IS NULL
            OR p_read_offset NOT BETWEEN 0 AND 16777216
            OR p_read_limit NOT BETWEEN 1 AND 1048576
            OR p_read_offset > 0 AND p_read_file_version IS NULL
            OR p_read_file_version IS NOT NULL AND p_read_file_version !~ '^sfv1_[A-Za-z0-9_-]{43}$'
            OR p_write_content IS NOT NULL)
       OR p_action = 'write' AND (p_read_offset IS NOT NULL OR p_read_limit IS NOT NULL
            OR p_read_file_version IS NOT NULL OR p_write_content IS NULL
            OR pg_catalog.octet_length(p_write_content) > 16777216)
       OR NOT cloud_agents.is_valid_identifier(p_request_id) THEN
        RAISE EXCEPTION USING ERRCODE = '22023', MESSAGE = 'remote worker sandbox file request is invalid';
    END IF;

    -- Acquire the enrollment lock before the multi-table share-lock query.
    -- heartbeat_remote_worker_v2 updates enrollment first and its trigger then
    -- updates deployment_targets; keeping this order avoids a lock-order cycle.
    PERFORM 1
    FROM cloud_agents.remote_worker_enrollments AS enrollment
    WHERE enrollment.tenant_id = p_tenant AND enrollment.project_uid = p_project
      AND enrollment.target_uid = p_target AND enrollment.state = 'enrolled'
      AND enrollment.certificate_state = 'active'
      AND enrollment.certificate_not_after > mutation_at
      AND enrollment.node_heartbeat_expires_at > mutation_at
      AND enrollment.node_desired_state = 'active' AND enrollment.node_observed_state = 'active'
      AND ARRAY['docker','files']::text[] <@ enrollment.node_capabilities
    FOR SHARE;
    IF NOT FOUND THEN
        RAISE EXCEPTION USING ERRCODE = '23505', MESSAGE = 'remote worker sandbox file unavailable';
    END IF;

    PERFORM 1
    FROM cloud_agents.sandbox_access_grants AS access_grant
    JOIN cloud_agents.sandbox_access_grant_activity AS activity
      ON activity.tenant_id = access_grant.tenant_id AND activity.project_uid = access_grant.project_uid
     AND activity.grant_uid = access_grant.grant_uid
    JOIN cloud_agents.sandbox_sessions AS sandbox
      ON sandbox.tenant_id = access_grant.tenant_id AND sandbox.project_uid = access_grant.project_uid
     AND sandbox.sandbox_uid = access_grant.sandbox_uid
    JOIN cloud_agents.workspace_volumes AS volume
      ON volume.tenant_id = sandbox.tenant_id AND volume.project_uid = sandbox.project_uid
     AND volume.workspace_uid = sandbox.workspace_uid
    JOIN cloud_agents.deployment_targets AS target
      ON target.tenant_id = volume.tenant_id AND target.project_uid = volume.project_uid
     AND target.target_uid = volume.target_uid
    JOIN cloud_agents.platform_operations AS operation
      ON operation.tenant_id = sandbox.tenant_id AND operation.operation_id = sandbox.operation_id
     AND operation.operation_generation = sandbox.operation_generation
    JOIN cloud_agents.remote_worker_enrollments AS enrollment
      ON enrollment.tenant_id = target.tenant_id AND enrollment.project_uid = target.project_uid
     AND enrollment.target_uid = target.target_uid
    WHERE access_grant.tenant_id = p_tenant AND access_grant.project_uid = p_project
      AND access_grant.grant_uid = p_grant AND access_grant.token_digest = p_token_digest
      AND access_grant.status = 'active' AND access_grant.expires_at > mutation_at
      AND activity.event_uid = p_event AND activity.action = 'file_' || p_action
      AND activity.subject_digest = p_token_digest AND activity.request_id = p_request_id
      AND activity.outcome = 'started' AND activity.completed_at IS NULL
      AND sandbox.workspace_uid = p_workspace AND sandbox.sandbox_uid = p_sandbox
      AND sandbox.generation = p_sandbox_generation AND access_grant.sandbox_generation = sandbox.generation
      AND sandbox.observed_generation = sandbox.generation
      AND sandbox.desired_state = 'running' AND sandbox.observed_state = 'running'
      AND NOT sandbox.writer_released AND sandbox.runtime_uid = p_runtime
      AND sandbox.runtime_state = 'Running' AND sandbox.runtime_operation_uid = p_runtime_operation
      AND sandbox.runtime_generation = sandbox.generation
      AND sandbox.runtime_spec_digest = p_runtime_spec_digest AND sandbox.spec_digest = p_runtime_spec_digest
      AND (sandbox.expires_at IS NULL OR sandbox.expires_at > mutation_at)
      AND volume.target_uid = p_target AND volume.observed_state = 'available'
      AND target.target_kind = 'remote-worker' AND target.observed_phase = 'ready'
      AND target.scheduling_state = 'active'
      AND operation.state = 'succeeded' AND operation.cleanup_phase = 'complete'
      AND enrollment.state = 'enrolled' AND enrollment.certificate_state = 'active'
      AND enrollment.certificate_not_after > mutation_at
      AND enrollment.node_heartbeat_expires_at > mutation_at
      AND enrollment.node_generation = target.generation
      AND enrollment.node_desired_state = 'active' AND enrollment.node_observed_state = 'active'
      AND ARRAY['docker','files']::text[] <@ enrollment.node_capabilities
      AND NOT EXISTS (
          SELECT 1 FROM cloud_agents.remote_worker_sandbox_exec_commands AS exec_command
          WHERE exec_command.tenant_id = p_tenant AND exec_command.project_uid = p_project
            AND exec_command.target_uid = p_target AND exec_command.deadline_at > mutation_at
            AND exec_command.state IN ('pending', 'delivered')
      )
    FOR SHARE OF access_grant, sandbox, volume, target, operation, enrollment;
    IF NOT FOUND THEN
        RAISE EXCEPTION USING ERRCODE = '23505', MESSAGE = 'remote worker sandbox file unavailable';
    END IF;

    INSERT INTO cloud_agents.remote_worker_sandbox_file_commands (
        tenant_id, project_uid, command_uid, event_uid, grant_uid, target_uid,
        workspace_uid, sandbox_uid, sandbox_generation, runtime_uid,
        runtime_operation_uid, runtime_spec_digest, action, file_path,
        read_offset, read_limit, read_file_version, write_content,
        request_id, requested_by, state, created_at, updated_at, deadline_at
    ) VALUES (
        p_tenant, p_project, p_command, p_event, p_grant, p_target,
        p_workspace, p_sandbox, p_sandbox_generation, p_runtime,
        p_runtime_operation, p_runtime_spec_digest, p_action, p_path,
        p_read_offset, p_read_limit, p_read_file_version, p_write_content,
        p_request_id, p_token_digest, 'pending', mutation_at, mutation_at,
        mutation_at + interval '65 seconds'
    ) RETURNING * INTO existing;

    RETURN QUERY SELECT existing.state, existing.deadline_at, existing.bytes_transferred,
        existing.list_entries, existing.result_read_file_version, existing.result_read_offset,
        existing.result_read_total_bytes, existing.result_read_content, existing.write_entry,
        existing.stable_error_code;
END;
$$;
