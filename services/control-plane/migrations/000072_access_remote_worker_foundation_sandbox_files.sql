CREATE TABLE cloud_agents.remote_worker_sandbox_file_commands (
    tenant_id text NOT NULL,
    project_uid text NOT NULL,
    command_uid text NOT NULL CHECK (cloud_agents.is_valid_identifier(command_uid)),
    event_uid text NOT NULL CHECK (cloud_agents.is_valid_identifier(event_uid)),
    grant_uid text NOT NULL CHECK (cloud_agents.is_valid_identifier(grant_uid)),
    target_uid text NOT NULL CHECK (cloud_agents.is_valid_identifier(target_uid)),
    workspace_uid text NOT NULL CHECK (cloud_agents.is_valid_identifier(workspace_uid)),
    sandbox_uid text NOT NULL CHECK (cloud_agents.is_valid_identifier(sandbox_uid)),
    sandbox_generation bigint NOT NULL CHECK (sandbox_generation > 0),
    runtime_uid text NOT NULL CHECK (cloud_agents.is_valid_identifier(runtime_uid)),
    runtime_operation_uid text NOT NULL CHECK (cloud_agents.is_valid_identifier(runtime_operation_uid)),
    runtime_spec_digest text NOT NULL CHECK (runtime_spec_digest ~ '^sha256:[0-9a-f]{64}$'),
    action text NOT NULL CHECK (action IN ('list', 'read', 'write', 'delete')),
    file_path text NOT NULL CHECK (
        pg_catalog.octet_length(file_path) BETWEEN 1 AND 1024
        AND pg_catalog.strpos(file_path, E'\\') = 0
        AND pg_catalog.strpos(file_path, E'\n') = 0
        AND pg_catalog.strpos(file_path, E'\r') = 0
        AND (file_path = '.' OR file_path !~ '(^/|(^|/)\.\.?(/|$)|//)')
        AND (action = 'list' OR file_path <> '.')
    ),
    read_offset bigint CHECK (read_offset IS NULL OR read_offset BETWEEN 0 AND 16777216),
    read_limit integer CHECK (read_limit IS NULL OR read_limit BETWEEN 1 AND 1048576),
    read_file_version text CHECK (read_file_version IS NULL OR read_file_version ~ '^sfv1_[A-Za-z0-9_-]{43}$'),
    write_content bytea CHECK (write_content IS NULL OR pg_catalog.octet_length(write_content) <= 16777216),
    request_id text NOT NULL CHECK (cloud_agents.is_valid_identifier(request_id)),
    requested_by text NOT NULL CHECK (requested_by ~ '^sha256:[0-9a-f]{64}$'),
    state text NOT NULL CHECK (state IN ('pending', 'delivered', 'succeeded', 'failed')),
    assigned_incarnation_uid text CHECK (assigned_incarnation_uid IS NULL OR cloud_agents.is_valid_identifier(assigned_incarnation_uid)),
    delivery_certificate_sha256 text CHECK (delivery_certificate_sha256 IS NULL OR delivery_certificate_sha256 ~ '^sha256:[0-9a-f]{64}$'),
    receipt_digest text CHECK (receipt_digest IS NULL OR receipt_digest ~ '^sha256:[0-9a-f]{64}$'),
    bytes_transferred bigint CHECK (bytes_transferred IS NULL OR bytes_transferred BETWEEN 0 AND 16777216),
    list_entries jsonb,
    result_read_file_version text CHECK (result_read_file_version IS NULL OR result_read_file_version ~ '^sfv1_[A-Za-z0-9_-]{43}$'),
    result_read_offset bigint CHECK (result_read_offset IS NULL OR result_read_offset BETWEEN 0 AND 16777216),
    result_read_total_bytes bigint CHECK (result_read_total_bytes IS NULL OR result_read_total_bytes BETWEEN 0 AND 16777216),
    result_read_content bytea CHECK (result_read_content IS NULL OR pg_catalog.octet_length(result_read_content) <= 1048576),
    write_entry jsonb,
    stable_error_code text CHECK (stable_error_code IS NULL OR stable_error_code IN (
        'sandbox_access_unavailable', 'sandbox_runtime_unavailable',
        'sandbox_file_not_found', 'sandbox_file_conflict', 'sandbox_file_limit',
        'sandbox_file_invalid', 'sandbox_file_timeout'
    )),
    created_at timestamptz NOT NULL DEFAULT transaction_timestamp(),
    updated_at timestamptz NOT NULL DEFAULT transaction_timestamp(),
    delivered_at timestamptz,
    settled_at timestamptz,
    deadline_at timestamptz NOT NULL,
    PRIMARY KEY (tenant_id, project_uid, command_uid),
    UNIQUE (tenant_id, event_uid),
    FOREIGN KEY (tenant_id, project_uid, grant_uid)
        REFERENCES cloud_agents.sandbox_access_grants ON UPDATE RESTRICT ON DELETE RESTRICT,
    FOREIGN KEY (tenant_id, event_uid)
        REFERENCES cloud_agents.sandbox_access_grant_activity (tenant_id, event_uid) ON UPDATE RESTRICT ON DELETE RESTRICT,
    FOREIGN KEY (tenant_id, project_uid, sandbox_uid)
        REFERENCES cloud_agents.sandbox_sessions ON UPDATE RESTRICT ON DELETE RESTRICT,
    FOREIGN KEY (tenant_id, project_uid, target_uid)
        REFERENCES cloud_agents.deployment_targets ON UPDATE RESTRICT ON DELETE RESTRICT,
    CHECK (
        action IN ('list', 'delete') AND read_offset IS NULL AND read_limit IS NULL
            AND read_file_version IS NULL AND write_content IS NULL
        OR action = 'read' AND read_offset IS NOT NULL AND read_limit IS NOT NULL
            AND (read_offset = 0 OR read_file_version IS NOT NULL) AND write_content IS NULL
        OR action = 'write' AND read_offset IS NULL AND read_limit IS NULL
            AND read_file_version IS NULL AND write_content IS NOT NULL
    ),
    CHECK (updated_at >= created_at AND deadline_at = created_at + interval '65 seconds'),
    CHECK (list_entries IS NULL OR pg_catalog.jsonb_typeof(list_entries) = 'array'
        AND pg_catalog.jsonb_array_length(list_entries) <= 1000),
    CHECK (write_entry IS NULL OR pg_catalog.jsonb_typeof(write_entry) = 'object'),
    CHECK (
        state = 'pending' AND assigned_incarnation_uid IS NULL AND delivery_certificate_sha256 IS NULL
            AND delivered_at IS NULL AND receipt_digest IS NULL AND bytes_transferred IS NULL
            AND list_entries IS NULL AND result_read_file_version IS NULL AND result_read_offset IS NULL
            AND result_read_total_bytes IS NULL AND result_read_content IS NULL AND write_entry IS NULL
            AND stable_error_code IS NULL AND settled_at IS NULL
        OR state = 'delivered' AND assigned_incarnation_uid IS NOT NULL AND delivery_certificate_sha256 IS NOT NULL
            AND delivered_at IS NOT NULL AND receipt_digest IS NULL AND bytes_transferred IS NULL
            AND list_entries IS NULL AND result_read_file_version IS NULL AND result_read_offset IS NULL
            AND result_read_total_bytes IS NULL AND result_read_content IS NULL AND write_entry IS NULL
            AND stable_error_code IS NULL AND settled_at IS NULL
        OR state = 'failed' AND assigned_incarnation_uid IS NOT NULL AND delivery_certificate_sha256 IS NOT NULL
            AND delivered_at IS NOT NULL AND receipt_digest IS NOT NULL AND bytes_transferred = 0
            AND list_entries IS NULL AND result_read_file_version IS NULL AND result_read_offset IS NULL
            AND result_read_total_bytes IS NULL AND result_read_content IS NULL AND write_entry IS NULL
            AND stable_error_code IS NOT NULL AND settled_at IS NOT NULL
        OR state = 'succeeded' AND assigned_incarnation_uid IS NOT NULL AND delivery_certificate_sha256 IS NOT NULL
            AND delivered_at IS NOT NULL AND receipt_digest IS NOT NULL AND stable_error_code IS NULL
            AND settled_at IS NOT NULL AND (
                action = 'list' AND bytes_transferred = 0 AND list_entries IS NOT NULL
                    AND result_read_file_version IS NULL AND result_read_offset IS NULL
                    AND result_read_total_bytes IS NULL AND result_read_content IS NULL AND write_entry IS NULL
                OR action = 'read' AND bytes_transferred = pg_catalog.octet_length(result_read_content)
                    AND list_entries IS NULL AND result_read_file_version IS NOT NULL
                    AND result_read_offset IS NOT NULL AND result_read_total_bytes IS NOT NULL
                    AND result_read_content IS NOT NULL AND write_entry IS NULL
                    AND result_read_total_bytes >= result_read_offset
                    AND result_read_offset + bytes_transferred <= result_read_total_bytes
                OR action = 'write' AND bytes_transferred IS NOT NULL AND list_entries IS NULL
                    AND result_read_file_version IS NULL AND result_read_offset IS NULL
                    AND result_read_total_bytes IS NULL AND result_read_content IS NULL AND write_entry IS NOT NULL
                OR action = 'delete' AND bytes_transferred = 0 AND list_entries IS NULL
                    AND result_read_file_version IS NULL AND result_read_offset IS NULL
                    AND result_read_total_bytes IS NULL AND result_read_content IS NULL AND write_entry IS NULL
            )
    )
);

CREATE INDEX remote_worker_sandbox_file_claim_idx
    ON cloud_agents.remote_worker_sandbox_file_commands (tenant_id, target_uid, state, created_at, command_uid);

ALTER TABLE cloud_agents.remote_worker_sandbox_file_commands OWNER TO cloud_agents_migration_owner;
ALTER TABLE cloud_agents.remote_worker_sandbox_file_commands ENABLE ROW LEVEL SECURITY;
ALTER TABLE cloud_agents.remote_worker_sandbox_file_commands FORCE ROW LEVEL SECURITY;
CREATE POLICY remote_worker_sandbox_file_commands_owner
    ON cloud_agents.remote_worker_sandbox_file_commands TO cloud_agents_migration_owner
    USING (true) WITH CHECK (true);
REVOKE ALL ON TABLE cloud_agents.remote_worker_sandbox_file_commands FROM PUBLIC;
REVOKE ALL ON TABLE cloud_agents.remote_worker_sandbox_file_commands FROM cloud_agents_runtime;

CREATE FUNCTION cloud_agents.request_remote_worker_sandbox_file_v1(
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

CREATE FUNCTION cloud_agents.claim_remote_worker_sandbox_file_v1(
    p_tenant text, p_project text, p_target text,
    p_incarnation text, p_peer_certificate_digest text
)
RETURNS TABLE (
    command_uid text, event_uid text, grant_uid text, workspace_uid text,
    target_uid text, sandbox_uid text, sandbox_generation bigint,
    runtime_uid text, runtime_operation_uid text, runtime_spec_digest text,
    action text, file_path text, read_offset bigint, read_limit integer,
    read_file_version text, write_content bytea, command_deadline_at timestamptz
)
LANGUAGE plpgsql SECURITY DEFINER SET search_path = pg_catalog, cloud_agents
AS $$
DECLARE
    ignored text;
    existing cloud_agents.remote_worker_sandbox_file_commands%ROWTYPE;
    claimed_at timestamptz := transaction_timestamp();
BEGIN
    ignored := cloud_agents.require_tenant_id();
    IF p_tenant IS DISTINCT FROM ignored OR NOT cloud_agents.is_valid_identifier(p_project)
       OR NOT cloud_agents.is_valid_identifier(p_target)
       OR NOT cloud_agents.is_valid_identifier(p_incarnation)
       OR p_peer_certificate_digest !~ '^sha256:[0-9a-f]{64}$' THEN
        RAISE EXCEPTION USING ERRCODE = '22023', MESSAGE = 'remote worker sandbox file claim is invalid';
    END IF;

    PERFORM 1 FROM cloud_agents.remote_worker_enrollments AS enrollment
    WHERE enrollment.tenant_id = p_tenant AND enrollment.project_uid = p_project
      AND enrollment.target_uid = p_target AND enrollment.state = 'enrolled'
      AND enrollment.certificate_state = 'active'
      AND enrollment.certificate_sha256 = p_peer_certificate_digest
      AND enrollment.certificate_not_after > claimed_at
      AND enrollment.incarnation_uid = p_incarnation
      AND enrollment.node_heartbeat_expires_at > claimed_at
      AND enrollment.node_desired_state = 'active' AND enrollment.node_observed_state = 'active'
      AND ARRAY['docker','files']::text[] <@ enrollment.node_capabilities
    FOR SHARE;
    IF NOT FOUND THEN
        RAISE EXCEPTION USING ERRCODE = '28000', MESSAGE = 'remote worker certificate authentication failed';
    END IF;

    SELECT command.* INTO existing
    FROM cloud_agents.remote_worker_sandbox_file_commands AS command
    JOIN cloud_agents.sandbox_access_grants AS access_grant
      ON access_grant.tenant_id = command.tenant_id AND access_grant.project_uid = command.project_uid
     AND access_grant.grant_uid = command.grant_uid
    JOIN cloud_agents.sandbox_access_grant_activity AS activity
      ON activity.tenant_id = command.tenant_id AND activity.project_uid = command.project_uid
     AND activity.event_uid = command.event_uid AND activity.grant_uid = command.grant_uid
    JOIN cloud_agents.sandbox_sessions AS sandbox
      ON sandbox.tenant_id = command.tenant_id AND sandbox.project_uid = command.project_uid
     AND sandbox.sandbox_uid = command.sandbox_uid
    JOIN cloud_agents.workspace_volumes AS volume
      ON volume.tenant_id = sandbox.tenant_id AND volume.project_uid = sandbox.project_uid
     AND volume.workspace_uid = sandbox.workspace_uid
    JOIN cloud_agents.platform_operations AS operation
      ON operation.tenant_id = sandbox.tenant_id AND operation.operation_id = sandbox.operation_id
     AND operation.operation_generation = sandbox.operation_generation
    WHERE command.tenant_id = p_tenant AND command.project_uid = p_project
      AND command.target_uid = p_target AND command.deadline_at > claimed_at
      AND (command.state = 'pending' OR command.state = 'delivered'
           AND command.assigned_incarnation_uid = p_incarnation
           AND command.delivery_certificate_sha256 = p_peer_certificate_digest)
      AND access_grant.token_digest = command.requested_by AND access_grant.status = 'active'
      AND access_grant.expires_at > claimed_at AND access_grant.sandbox_generation = command.sandbox_generation
      AND activity.action = 'file_' || command.action AND activity.subject_digest = command.requested_by
      AND activity.request_id = command.request_id AND activity.outcome = 'started'
      AND activity.completed_at IS NULL
      AND sandbox.workspace_uid = command.workspace_uid
      AND sandbox.generation = command.sandbox_generation
      AND sandbox.observed_generation = sandbox.generation
      AND sandbox.desired_state = 'running' AND sandbox.observed_state = 'running'
      AND NOT sandbox.writer_released AND sandbox.runtime_uid = command.runtime_uid
      AND sandbox.runtime_state = 'Running'
      AND sandbox.runtime_operation_uid = command.runtime_operation_uid
      AND sandbox.runtime_generation = sandbox.generation
      AND sandbox.runtime_spec_digest = command.runtime_spec_digest
      AND sandbox.spec_digest = command.runtime_spec_digest
      AND (sandbox.expires_at IS NULL OR sandbox.expires_at > claimed_at)
      AND volume.target_uid = p_target AND volume.observed_state = 'available'
      AND operation.state = 'succeeded' AND operation.cleanup_phase = 'complete'
      AND NOT EXISTS (
          SELECT 1
          FROM cloud_agents.outbox_events AS event
          JOIN cloud_agents.sandbox_sessions AS lifecycle_sandbox
            ON lifecycle_sandbox.tenant_id = event.tenant_id
           AND lifecycle_sandbox.operation_id = event.operation_id
           AND lifecycle_sandbox.operation_generation = event.operation_generation
           AND lifecycle_sandbox.sandbox_uid = event.aggregate_id
          JOIN cloud_agents.workspace_volumes AS lifecycle_volume
            ON lifecycle_volume.tenant_id = lifecycle_sandbox.tenant_id
           AND lifecycle_volume.project_uid = lifecycle_sandbox.project_uid
           AND lifecycle_volume.workspace_uid = lifecycle_sandbox.workspace_uid
          WHERE event.profile_id = 'foundationSandboxLifecycle/v1alpha1'
            AND event.event_class = 'operation_effect'
            AND event.aggregate_kind = 'sandboxSession'
            AND lifecycle_volume.target_uid = p_target
            AND (event.state = 'pending'
                 OR event.state = 'retry_wait' AND event.next_attempt_at <= claimed_at
                 OR event.state = 'claimed' AND event.claim_expires_at > claimed_at)
      )
      AND NOT EXISTS (
          SELECT 1 FROM cloud_agents.remote_worker_sandbox_exec_commands AS exec_command
          WHERE exec_command.tenant_id = p_tenant AND exec_command.project_uid = p_project
            AND exec_command.target_uid = p_target AND exec_command.deadline_at > claimed_at
            AND exec_command.state IN ('pending', 'delivered')
      )
    ORDER BY (command.state = 'delivered') DESC, command.created_at, command.command_uid
    FOR UPDATE OF command SKIP LOCKED LIMIT 1;
    IF NOT FOUND THEN RETURN; END IF;

    IF existing.state = 'pending' THEN
        UPDATE cloud_agents.remote_worker_sandbox_file_commands AS command
        SET state = 'delivered', assigned_incarnation_uid = p_incarnation,
            delivery_certificate_sha256 = p_peer_certificate_digest,
            delivered_at = claimed_at, updated_at = claimed_at
        WHERE command.tenant_id = p_tenant AND command.project_uid = p_project
          AND command.command_uid = existing.command_uid
        RETURNING * INTO existing;
    END IF;

    RETURN QUERY SELECT existing.command_uid, existing.event_uid, existing.grant_uid,
        existing.workspace_uid, existing.target_uid, existing.sandbox_uid,
        existing.sandbox_generation, existing.runtime_uid, existing.runtime_operation_uid,
        existing.runtime_spec_digest, existing.action, existing.file_path,
        existing.read_offset, existing.read_limit, existing.read_file_version,
        existing.write_content, existing.deadline_at;
END;
$$;

CREATE FUNCTION cloud_agents.settle_remote_worker_sandbox_file_v1(
    p_tenant text, p_project text, p_target text, p_incarnation text,
    p_peer_certificate_digest text, p_command text, p_event text, p_grant text,
    p_sandbox text, p_sandbox_generation bigint, p_action text, p_result text,
    p_bytes bigint, p_list_entries jsonb, p_read_file_version text,
    p_read_offset bigint, p_read_total_bytes bigint, p_read_content bytea,
    p_write_entry jsonb, p_stable_error_code text, p_receipt_digest text
)
RETURNS TABLE (command_state text, command_deadline_at timestamptz)
LANGUAGE plpgsql SECURITY DEFINER SET search_path = pg_catalog, cloud_agents
AS $$
DECLARE
    ignored text;
    existing cloud_agents.remote_worker_sandbox_file_commands%ROWTYPE;
    mutation_at timestamptz := transaction_timestamp();
BEGIN
    ignored := cloud_agents.require_tenant_id();
    IF p_tenant IS DISTINCT FROM ignored OR NOT cloud_agents.is_valid_identifier(p_project)
       OR NOT cloud_agents.is_valid_identifier(p_target)
       OR NOT cloud_agents.is_valid_identifier(p_incarnation)
       OR p_peer_certificate_digest !~ '^sha256:[0-9a-f]{64}$'
       OR NOT cloud_agents.is_valid_identifier(p_command)
       OR NOT cloud_agents.is_valid_identifier(p_event)
       OR NOT cloud_agents.is_valid_identifier(p_grant)
       OR NOT cloud_agents.is_valid_identifier(p_sandbox)
       OR p_sandbox_generation IS NULL OR p_sandbox_generation < 1
       OR p_action IS NULL OR p_action NOT IN ('list', 'read', 'write', 'delete')
       OR p_result IS NULL OR p_result NOT IN ('succeeded', 'failed')
       OR p_bytes IS NULL OR p_bytes NOT BETWEEN 0 AND 16777216
       OR p_receipt_digest !~ '^sha256:[0-9a-f]{64}$'
       OR p_result = 'failed' AND (p_bytes <> 0 OR p_list_entries IS NOT NULL
            OR p_read_file_version IS NOT NULL OR p_read_offset IS NOT NULL
            OR p_read_total_bytes IS NOT NULL OR p_read_content IS NOT NULL
            OR p_write_entry IS NOT NULL OR p_stable_error_code IS NULL
            OR p_stable_error_code NOT IN (
                'sandbox_access_unavailable', 'sandbox_runtime_unavailable',
                'sandbox_file_not_found', 'sandbox_file_conflict', 'sandbox_file_limit',
                'sandbox_file_invalid', 'sandbox_file_timeout'))
       OR p_result = 'succeeded' AND (p_stable_error_code IS NOT NULL OR
            (p_action = 'list' AND (p_bytes <> 0 OR p_list_entries IS NULL
                OR pg_catalog.jsonb_typeof(p_list_entries) <> 'array'
                OR pg_catalog.jsonb_array_length(p_list_entries) > 1000
                OR p_read_file_version IS NOT NULL OR p_read_offset IS NOT NULL
                OR p_read_total_bytes IS NOT NULL OR p_read_content IS NOT NULL OR p_write_entry IS NOT NULL)
            OR p_action = 'read' AND (p_list_entries IS NOT NULL
                OR p_read_file_version IS NULL OR p_read_offset IS NULL
                OR p_read_total_bytes IS NULL
                OR p_read_file_version !~ '^sfv1_[A-Za-z0-9_-]{43}$'
                OR p_read_offset NOT BETWEEN 0 AND 16777216
                OR p_read_total_bytes NOT BETWEEN 0 AND 16777216
                OR p_read_content IS NULL OR pg_catalog.octet_length(p_read_content) <> p_bytes
                OR p_read_offset + p_bytes > p_read_total_bytes OR p_write_entry IS NOT NULL)
            OR p_action = 'write' AND (p_list_entries IS NOT NULL
                OR p_read_file_version IS NOT NULL OR p_read_offset IS NOT NULL
                OR p_read_total_bytes IS NOT NULL OR p_read_content IS NOT NULL
                OR p_write_entry IS NULL OR pg_catalog.jsonb_typeof(p_write_entry) <> 'object')
            OR p_action = 'delete' AND (p_bytes <> 0 OR p_list_entries IS NOT NULL
                OR p_read_file_version IS NOT NULL OR p_read_offset IS NOT NULL
                OR p_read_total_bytes IS NOT NULL OR p_read_content IS NOT NULL OR p_write_entry IS NOT NULL))) THEN
        RAISE EXCEPTION USING ERRCODE = '22023', MESSAGE = 'remote worker sandbox file receipt is invalid';
    END IF;

    PERFORM 1 FROM cloud_agents.remote_worker_enrollments AS enrollment
    WHERE enrollment.tenant_id = p_tenant AND enrollment.project_uid = p_project
      AND enrollment.target_uid = p_target AND enrollment.state = 'enrolled'
      AND enrollment.certificate_state = 'active'
      AND enrollment.certificate_sha256 = p_peer_certificate_digest
      AND enrollment.certificate_not_after > mutation_at
      AND enrollment.incarnation_uid = p_incarnation
      AND enrollment.node_heartbeat_expires_at > mutation_at
      AND enrollment.node_desired_state = 'active' AND enrollment.node_observed_state = 'active'
    FOR SHARE;
    IF NOT FOUND THEN
        RAISE EXCEPTION USING ERRCODE = '28000', MESSAGE = 'remote worker certificate authentication failed';
    END IF;

    SELECT command.* INTO existing
    FROM cloud_agents.remote_worker_sandbox_file_commands AS command
    WHERE command.tenant_id = p_tenant AND command.project_uid = p_project
      AND command.command_uid = p_command FOR UPDATE;
    IF NOT FOUND OR existing.target_uid IS DISTINCT FROM p_target
       OR existing.assigned_incarnation_uid IS DISTINCT FROM p_incarnation
       OR existing.delivery_certificate_sha256 IS DISTINCT FROM p_peer_certificate_digest
       OR existing.event_uid IS DISTINCT FROM p_event OR existing.grant_uid IS DISTINCT FROM p_grant
       OR existing.sandbox_uid IS DISTINCT FROM p_sandbox
       OR existing.sandbox_generation IS DISTINCT FROM p_sandbox_generation
       OR existing.action IS DISTINCT FROM p_action THEN
        RAISE EXCEPTION USING ERRCODE = '40001', MESSAGE = 'remote worker sandbox file receipt conflict';
    END IF;

    IF p_result = 'succeeded' AND (
        p_action = 'list' AND EXISTS (
            SELECT 1 FROM pg_catalog.jsonb_array_elements(p_list_entries) AS entry
            WHERE pg_catalog.jsonb_typeof(entry) <> 'object'
               OR CASE WHEN pg_catalog.strpos(entry->>'path', '/') = 0 THEN '.'
                       ELSE pg_catalog.regexp_replace(entry->>'path', '/[^/]+$', '') END
                  IS DISTINCT FROM existing.file_path
        )
        OR p_action = 'read' AND (
            p_read_offset IS DISTINCT FROM existing.read_offset
            OR p_bytes > existing.read_limit
            OR existing.read_file_version IS NOT NULL
               AND p_read_file_version IS DISTINCT FROM existing.read_file_version
        )
        OR p_action = 'write' AND (
            p_bytes IS DISTINCT FROM pg_catalog.octet_length(existing.write_content)
            OR p_write_entry->>'path' IS DISTINCT FROM existing.file_path
            OR p_write_entry->>'type' IS DISTINCT FROM 'file'
            OR p_write_entry->>'sizeBytes' IS DISTINCT FROM p_bytes::text
        )
    ) THEN
        RAISE EXCEPTION USING ERRCODE = '40001', MESSAGE = 'remote worker sandbox file receipt conflict';
    END IF;

    IF existing.state IN ('succeeded', 'failed') THEN
        IF existing.receipt_digest IS DISTINCT FROM p_receipt_digest THEN
            RAISE EXCEPTION USING ERRCODE = '40001', MESSAGE = 'remote worker sandbox file receipt conflict';
        END IF;
    ELSIF existing.state = 'delivered' THEN
        UPDATE cloud_agents.remote_worker_sandbox_file_commands AS command
        SET state = p_result, receipt_digest = p_receipt_digest,
            bytes_transferred = p_bytes,
            list_entries = CASE WHEN p_result = 'succeeded' THEN p_list_entries END,
            result_read_file_version = CASE WHEN p_result = 'succeeded' THEN p_read_file_version END,
            result_read_offset = CASE WHEN p_result = 'succeeded' THEN p_read_offset END,
            result_read_total_bytes = CASE WHEN p_result = 'succeeded' THEN p_read_total_bytes END,
            result_read_content = CASE WHEN p_result = 'succeeded' THEN p_read_content END,
            write_entry = CASE WHEN p_result = 'succeeded' THEN p_write_entry END,
            stable_error_code = CASE WHEN p_result = 'failed' THEN p_stable_error_code END,
            settled_at = mutation_at, updated_at = mutation_at
        WHERE command.tenant_id = p_tenant AND command.project_uid = p_project
          AND command.command_uid = p_command
        RETURNING * INTO existing;
    ELSE
        RAISE EXCEPTION USING ERRCODE = '40001', MESSAGE = 'remote worker sandbox file receipt conflict';
    END IF;

    RETURN QUERY SELECT existing.state, existing.deadline_at;
END;
$$;

CREATE FUNCTION cloud_agents.get_remote_worker_sandbox_file_v1(
    p_tenant text, p_project text, p_command text, p_token_digest text
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
BEGIN
    ignored := cloud_agents.require_tenant_id();
    IF p_tenant IS DISTINCT FROM ignored OR NOT cloud_agents.is_valid_identifier(p_project)
       OR NOT cloud_agents.is_valid_identifier(p_command)
       OR p_token_digest !~ '^sha256:[0-9a-f]{64}$' THEN
        RAISE EXCEPTION USING ERRCODE = '22023', MESSAGE = 'remote worker sandbox file lookup is invalid';
    END IF;
    SELECT command.* INTO existing
    FROM cloud_agents.remote_worker_sandbox_file_commands AS command
    WHERE command.tenant_id = p_tenant AND command.project_uid = p_project
      AND command.command_uid = p_command AND command.requested_by = p_token_digest;
    IF NOT FOUND THEN
        RAISE EXCEPTION USING ERRCODE = '23503', MESSAGE = 'remote worker sandbox file was not found';
    END IF;
    IF existing.state IN ('pending', 'delivered') AND existing.deadline_at <= clock_timestamp() THEN
        RETURN QUERY SELECT 'failed'::text, existing.deadline_at, 0::bigint,
            NULL::jsonb, NULL::text, NULL::bigint, NULL::bigint, NULL::bytea,
            NULL::jsonb, 'sandbox_file_timeout'::text;
    ELSE
        RETURN QUERY SELECT existing.state, existing.deadline_at, existing.bytes_transferred,
            existing.list_entries, existing.result_read_file_version, existing.result_read_offset,
            existing.result_read_total_bytes, existing.result_read_content, existing.write_entry,
            existing.stable_error_code;
    END IF;
END;
$$;

ALTER FUNCTION cloud_agents.request_remote_worker_sandbox_file_v1(text,text,text,text,text,text,text,text,text,bigint,text,text,text,text,text,bigint,integer,text,bytea,text) OWNER TO cloud_agents_migration_owner;
ALTER FUNCTION cloud_agents.claim_remote_worker_sandbox_file_v1(text,text,text,text,text) OWNER TO cloud_agents_migration_owner;
ALTER FUNCTION cloud_agents.settle_remote_worker_sandbox_file_v1(text,text,text,text,text,text,text,text,text,bigint,text,text,bigint,jsonb,text,bigint,bigint,bytea,jsonb,text,text) OWNER TO cloud_agents_migration_owner;
ALTER FUNCTION cloud_agents.get_remote_worker_sandbox_file_v1(text,text,text,text) OWNER TO cloud_agents_migration_owner;
REVOKE ALL ON FUNCTION cloud_agents.request_remote_worker_sandbox_file_v1(text,text,text,text,text,text,text,text,text,bigint,text,text,text,text,text,bigint,integer,text,bytea,text) FROM PUBLIC;
REVOKE ALL ON FUNCTION cloud_agents.claim_remote_worker_sandbox_file_v1(text,text,text,text,text) FROM PUBLIC;
REVOKE ALL ON FUNCTION cloud_agents.settle_remote_worker_sandbox_file_v1(text,text,text,text,text,text,text,text,text,bigint,text,text,bigint,jsonb,text,bigint,bigint,bytea,jsonb,text,text) FROM PUBLIC;
REVOKE ALL ON FUNCTION cloud_agents.get_remote_worker_sandbox_file_v1(text,text,text,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION cloud_agents.request_remote_worker_sandbox_file_v1(text,text,text,text,text,text,text,text,text,bigint,text,text,text,text,text,bigint,integer,text,bytea,text) TO cloud_agents_runtime;
GRANT EXECUTE ON FUNCTION cloud_agents.claim_remote_worker_sandbox_file_v1(text,text,text,text,text) TO cloud_agents_runtime;
GRANT EXECUTE ON FUNCTION cloud_agents.settle_remote_worker_sandbox_file_v1(text,text,text,text,text,text,text,text,text,bigint,text,text,bigint,jsonb,text,bigint,bigint,bytea,jsonb,text,text) TO cloud_agents_runtime;
GRANT EXECUTE ON FUNCTION cloud_agents.get_remote_worker_sandbox_file_v1(text,text,text,text) TO cloud_agents_runtime;
