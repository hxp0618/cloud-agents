CREATE TABLE cloud_agents.remote_worker_sandbox_exec_commands (
    tenant_id text NOT NULL,
    project_uid text NOT NULL,
    command_uid text NOT NULL CHECK (cloud_agents.is_valid_identifier(command_uid)),
    target_uid text NOT NULL,
    workspace_uid text NOT NULL,
    sandbox_uid text NOT NULL,
    sandbox_generation bigint NOT NULL CHECK (sandbox_generation > 0),
    runtime_uid text NOT NULL CHECK (cloud_agents.is_valid_identifier(runtime_uid)),
    runtime_operation_uid text NOT NULL CHECK (cloud_agents.is_valid_identifier(runtime_operation_uid)),
    runtime_spec_digest text NOT NULL CHECK (runtime_spec_digest ~ '^sha256:[0-9a-f]{64}$'),
    command_text text NOT NULL CHECK (pg_catalog.octet_length(command_text) BETWEEN 1 AND 8192),
    timeout_seconds integer NOT NULL CHECK (timeout_seconds BETWEEN 1 AND 60),
    request_id text NOT NULL CHECK (cloud_agents.is_valid_identifier(request_id)),
    request_digest text NOT NULL CHECK (request_digest ~ '^sha256:[0-9a-f]{64}$'),
    requested_by text NOT NULL CHECK (requested_by ~ '^sha256:[0-9a-f]{64}$'),
    state text NOT NULL CHECK (state IN ('pending', 'delivered', 'succeeded', 'failed')),
    assigned_incarnation_uid text CHECK (assigned_incarnation_uid IS NULL OR cloud_agents.is_valid_identifier(assigned_incarnation_uid)),
    delivery_certificate_sha256 text CHECK (delivery_certificate_sha256 IS NULL OR delivery_certificate_sha256 ~ '^sha256:[0-9a-f]{64}$'),
    receipt_digest text CHECK (receipt_digest IS NULL OR receipt_digest ~ '^sha256:[0-9a-f]{64}$'),
    exit_code integer,
    stdout text,
    stderr text,
    execution_time_millis bigint CHECK (execution_time_millis IS NULL OR execution_time_millis BETWEEN 0 AND 65000),
    stable_error_code text CHECK (stable_error_code IS NULL OR stable_error_code IN (
        'sandbox_access_unavailable', 'sandbox_runtime_unavailable',
        'sandbox_exec_output_limit', 'sandbox_exec_timeout'
    )),
    created_at timestamptz NOT NULL DEFAULT transaction_timestamp(),
    updated_at timestamptz NOT NULL DEFAULT transaction_timestamp(),
    delivered_at timestamptz,
    settled_at timestamptz,
    deadline_at timestamptz NOT NULL,
    PRIMARY KEY (tenant_id, project_uid, command_uid),
    UNIQUE (tenant_id, project_uid, requested_by, request_id),
    FOREIGN KEY (tenant_id, project_uid, sandbox_uid)
        REFERENCES cloud_agents.sandbox_sessions ON UPDATE RESTRICT ON DELETE RESTRICT,
    FOREIGN KEY (tenant_id, project_uid, target_uid)
        REFERENCES cloud_agents.deployment_targets ON UPDATE RESTRICT ON DELETE RESTRICT,
    CHECK (updated_at >= created_at AND deadline_at = created_at + pg_catalog.make_interval(secs => timeout_seconds + 35)),
    CHECK (stdout IS NULL OR stderr IS NULL OR pg_catalog.octet_length(stdout) + pg_catalog.octet_length(stderr) <= 1048576),
    CHECK (
        state = 'pending' AND assigned_incarnation_uid IS NULL AND delivery_certificate_sha256 IS NULL
            AND delivered_at IS NULL AND receipt_digest IS NULL AND exit_code IS NULL
            AND stdout IS NULL AND stderr IS NULL AND execution_time_millis IS NULL
            AND stable_error_code IS NULL AND settled_at IS NULL
        OR state = 'delivered' AND assigned_incarnation_uid IS NOT NULL AND delivery_certificate_sha256 IS NOT NULL
            AND delivered_at IS NOT NULL AND receipt_digest IS NULL AND exit_code IS NULL
            AND stdout IS NULL AND stderr IS NULL AND execution_time_millis IS NULL
            AND stable_error_code IS NULL AND settled_at IS NULL
        OR state = 'succeeded' AND assigned_incarnation_uid IS NOT NULL AND delivery_certificate_sha256 IS NOT NULL
            AND delivered_at IS NOT NULL AND receipt_digest IS NOT NULL AND exit_code IS NOT NULL
            AND stdout IS NOT NULL AND stderr IS NOT NULL AND execution_time_millis IS NOT NULL
            AND stable_error_code IS NULL AND settled_at IS NOT NULL
        OR state = 'failed' AND assigned_incarnation_uid IS NOT NULL AND delivery_certificate_sha256 IS NOT NULL
            AND delivered_at IS NOT NULL AND receipt_digest IS NOT NULL AND exit_code IS NULL
            AND stdout IS NULL AND stderr IS NULL AND execution_time_millis IS NULL
            AND stable_error_code IS NOT NULL AND settled_at IS NOT NULL
    )
);

CREATE INDEX remote_worker_sandbox_exec_claim_idx
    ON cloud_agents.remote_worker_sandbox_exec_commands (tenant_id, target_uid, state, created_at, command_uid);

ALTER TABLE cloud_agents.remote_worker_sandbox_exec_commands OWNER TO cloud_agents_migration_owner;
ALTER TABLE cloud_agents.remote_worker_sandbox_exec_commands ENABLE ROW LEVEL SECURITY;
ALTER TABLE cloud_agents.remote_worker_sandbox_exec_commands FORCE ROW LEVEL SECURITY;
CREATE POLICY remote_worker_sandbox_exec_commands_owner
    ON cloud_agents.remote_worker_sandbox_exec_commands TO cloud_agents_migration_owner
    USING (true) WITH CHECK (true);
REVOKE ALL ON TABLE cloud_agents.remote_worker_sandbox_exec_commands FROM PUBLIC;
REVOKE ALL ON TABLE cloud_agents.remote_worker_sandbox_exec_commands FROM cloud_agents_runtime;

CREATE FUNCTION cloud_agents.request_remote_worker_sandbox_exec_v1(
    p_tenant text, p_project text, p_command_uid text, p_target text,
    p_workspace text, p_sandbox text, p_sandbox_generation bigint,
    p_runtime text, p_runtime_operation text, p_runtime_spec_digest text,
    p_command text, p_timeout_seconds integer, p_request_id text,
    p_request_digest text, p_subject_digest text
)
RETURNS TABLE (
    command_uid text, command_state text, command_deadline_at timestamptz,
    exit_code integer, stdout text, stderr text, execution_time_millis bigint,
    stable_error_code text
)
LANGUAGE plpgsql SECURITY DEFINER SET search_path = pg_catalog, cloud_agents
AS $$
DECLARE
    ignored text;
    existing cloud_agents.remote_worker_sandbox_exec_commands%ROWTYPE;
    mutation_at timestamptz := transaction_timestamp();
BEGIN
    ignored := cloud_agents.require_runtime_mutation_principal();
    IF p_tenant IS DISTINCT FROM cloud_agents.require_tenant_id()
       OR NOT cloud_agents.is_valid_identifier(p_project)
       OR NOT cloud_agents.is_valid_identifier(p_command_uid)
       OR NOT cloud_agents.is_valid_identifier(p_target)
       OR NOT cloud_agents.is_valid_identifier(p_workspace)
       OR NOT cloud_agents.is_valid_identifier(p_sandbox)
       OR p_sandbox_generation IS NULL OR p_sandbox_generation < 1
       OR NOT cloud_agents.is_valid_identifier(p_runtime)
       OR NOT cloud_agents.is_valid_identifier(p_runtime_operation)
       OR p_runtime_spec_digest !~ '^sha256:[0-9a-f]{64}$'
       OR p_command IS NULL OR pg_catalog.octet_length(p_command) NOT BETWEEN 1 AND 8192
       OR p_timeout_seconds IS NULL OR p_timeout_seconds NOT BETWEEN 1 AND 60
       OR NOT cloud_agents.is_valid_identifier(p_request_id)
       OR p_request_digest !~ '^sha256:[0-9a-f]{64}$'
       OR p_subject_digest !~ '^sha256:[0-9a-f]{64}$' THEN
        RAISE EXCEPTION USING ERRCODE = '22023', MESSAGE = 'remote worker sandbox exec request is invalid';
    END IF;

    SELECT command.* INTO existing
    FROM cloud_agents.remote_worker_sandbox_exec_commands AS command
    WHERE command.tenant_id = p_tenant AND command.project_uid = p_project
      AND command.requested_by = p_subject_digest AND command.request_id = p_request_id
    FOR UPDATE;
    IF FOUND THEN
        IF existing.request_digest IS DISTINCT FROM p_request_digest
           OR existing.target_uid IS DISTINCT FROM p_target
           OR existing.workspace_uid IS DISTINCT FROM p_workspace
           OR existing.sandbox_uid IS DISTINCT FROM p_sandbox
           OR existing.sandbox_generation IS DISTINCT FROM p_sandbox_generation
           OR existing.runtime_uid IS DISTINCT FROM p_runtime
           OR existing.runtime_operation_uid IS DISTINCT FROM p_runtime_operation
           OR existing.runtime_spec_digest IS DISTINCT FROM p_runtime_spec_digest
           OR existing.command_text IS DISTINCT FROM p_command
           OR existing.timeout_seconds IS DISTINCT FROM p_timeout_seconds THEN
            RAISE EXCEPTION USING ERRCODE = '23505', MESSAGE = 'remote worker sandbox exec request conflict';
        END IF;
    ELSE
        PERFORM 1
        FROM cloud_agents.sandbox_sessions AS sandbox
        JOIN cloud_agents.workspace_volumes AS volume USING (tenant_id, project_uid, workspace_uid)
        JOIN cloud_agents.deployment_targets AS target
          ON target.tenant_id = volume.tenant_id AND target.project_uid = volume.project_uid
         AND target.target_uid = volume.target_uid
        JOIN cloud_agents.platform_operations AS operation
          ON operation.tenant_id = sandbox.tenant_id AND operation.operation_id = sandbox.operation_id
         AND operation.operation_generation = sandbox.operation_generation
        JOIN cloud_agents.remote_worker_enrollments AS enrollment
          ON enrollment.tenant_id = target.tenant_id AND enrollment.project_uid = target.project_uid
         AND enrollment.target_uid = target.target_uid
        WHERE sandbox.tenant_id = p_tenant AND sandbox.project_uid = p_project
          AND sandbox.sandbox_uid = p_sandbox AND sandbox.workspace_uid = p_workspace
          AND sandbox.generation = p_sandbox_generation
          AND sandbox.observed_generation = sandbox.generation
          AND sandbox.desired_state = 'running' AND sandbox.observed_state = 'running'
          AND NOT sandbox.writer_released AND sandbox.runtime_uid = p_runtime
          AND sandbox.runtime_state = 'Running'
          AND sandbox.runtime_operation_uid = p_runtime_operation
          AND sandbox.runtime_generation = sandbox.generation
          AND sandbox.runtime_spec_digest = p_runtime_spec_digest
          AND sandbox.spec_digest = p_runtime_spec_digest
          AND (sandbox.expires_at IS NULL OR sandbox.expires_at > mutation_at)
          AND volume.observed_state = 'available' AND volume.target_uid = p_target
          AND target.target_kind = 'remote-worker' AND target.observed_phase = 'ready'
          AND target.scheduling_state = 'active'
          AND operation.state = 'succeeded' AND operation.cleanup_phase = 'complete'
          AND enrollment.state = 'enrolled' AND enrollment.certificate_state = 'active'
          AND enrollment.certificate_not_after > mutation_at
          AND enrollment.node_heartbeat_expires_at > mutation_at
          AND enrollment.node_generation = target.generation
          AND enrollment.node_desired_state = 'active' AND enrollment.node_observed_state = 'active'
          AND ARRAY['docker','exec']::text[] <@ enrollment.node_capabilities
        FOR SHARE OF sandbox, volume, target, operation, enrollment;
        IF NOT FOUND THEN
            RAISE EXCEPTION USING ERRCODE = '23505', MESSAGE = 'remote worker sandbox exec unavailable';
        END IF;

        INSERT INTO cloud_agents.remote_worker_sandbox_exec_commands (
            tenant_id, project_uid, command_uid, target_uid, workspace_uid,
            sandbox_uid, sandbox_generation, runtime_uid, runtime_operation_uid,
            runtime_spec_digest, command_text, timeout_seconds, request_id,
            request_digest, requested_by, state, created_at, updated_at, deadline_at
        ) VALUES (
            p_tenant, p_project, p_command_uid, p_target, p_workspace,
            p_sandbox, p_sandbox_generation, p_runtime, p_runtime_operation,
            p_runtime_spec_digest, p_command, p_timeout_seconds, p_request_id,
            p_request_digest, p_subject_digest, 'pending', mutation_at, mutation_at,
            mutation_at + pg_catalog.make_interval(secs => p_timeout_seconds + 35)
        ) RETURNING * INTO existing;
    END IF;

    RETURN QUERY SELECT existing.command_uid, existing.state, existing.deadline_at,
        existing.exit_code, existing.stdout, existing.stderr,
        existing.execution_time_millis, existing.stable_error_code;
END;
$$;

CREATE FUNCTION cloud_agents.claim_remote_worker_sandbox_exec_v1(
    p_tenant text, p_project text, p_target text,
    p_incarnation text, p_peer_certificate_digest text
)
RETURNS TABLE (
    command_uid text, workspace_uid text, target_uid text, sandbox_uid text,
    sandbox_generation bigint, runtime_uid text, runtime_operation_uid text,
    runtime_spec_digest text, command_text text, timeout_seconds integer,
    command_deadline_at timestamptz
)
LANGUAGE plpgsql SECURITY DEFINER SET search_path = pg_catalog, cloud_agents
AS $$
DECLARE
    ignored text;
    existing cloud_agents.remote_worker_sandbox_exec_commands%ROWTYPE;
    claimed_at timestamptz := transaction_timestamp();
BEGIN
    ignored := cloud_agents.require_tenant_id();
    IF p_tenant IS DISTINCT FROM ignored OR NOT cloud_agents.is_valid_identifier(p_project)
       OR NOT cloud_agents.is_valid_identifier(p_target)
       OR NOT cloud_agents.is_valid_identifier(p_incarnation)
       OR p_peer_certificate_digest !~ '^sha256:[0-9a-f]{64}$' THEN
        RAISE EXCEPTION USING ERRCODE = '22023', MESSAGE = 'remote worker sandbox exec claim is invalid';
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
      AND ARRAY['docker','exec']::text[] <@ enrollment.node_capabilities
    FOR SHARE;
    IF NOT FOUND THEN
        RAISE EXCEPTION USING ERRCODE = '28000', MESSAGE = 'remote worker certificate authentication failed';
    END IF;

    SELECT command.* INTO existing
    FROM cloud_agents.remote_worker_sandbox_exec_commands AS command
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
    ORDER BY (command.state = 'delivered') DESC, command.created_at, command.command_uid
    FOR UPDATE OF command SKIP LOCKED LIMIT 1;
    IF NOT FOUND THEN RETURN; END IF;

    IF existing.state = 'pending' THEN
        UPDATE cloud_agents.remote_worker_sandbox_exec_commands AS command
        SET state = 'delivered', assigned_incarnation_uid = p_incarnation,
            delivery_certificate_sha256 = p_peer_certificate_digest,
            delivered_at = claimed_at, updated_at = claimed_at
        WHERE command.tenant_id = p_tenant AND command.project_uid = p_project
          AND command.command_uid = existing.command_uid
        RETURNING * INTO existing;
    END IF;

    RETURN QUERY SELECT existing.command_uid, existing.workspace_uid, existing.target_uid,
        existing.sandbox_uid, existing.sandbox_generation, existing.runtime_uid,
        existing.runtime_operation_uid, existing.runtime_spec_digest, existing.command_text,
        existing.timeout_seconds, existing.deadline_at;
END;
$$;

CREATE FUNCTION cloud_agents.settle_remote_worker_sandbox_exec_v1(
    p_tenant text, p_project text, p_target text, p_incarnation text,
    p_peer_certificate_digest text, p_command_uid text, p_sandbox text,
    p_sandbox_generation bigint, p_result text, p_exit_code integer,
    p_stdout text, p_stderr text, p_execution_time_millis bigint,
    p_stable_error_code text, p_receipt_digest text
)
RETURNS TABLE (command_state text, command_deadline_at timestamptz)
LANGUAGE plpgsql SECURITY DEFINER SET search_path = pg_catalog, cloud_agents
AS $$
DECLARE
    ignored text;
    existing cloud_agents.remote_worker_sandbox_exec_commands%ROWTYPE;
    mutation_at timestamptz := transaction_timestamp();
BEGIN
    ignored := cloud_agents.require_tenant_id();
    IF p_tenant IS DISTINCT FROM ignored OR NOT cloud_agents.is_valid_identifier(p_project)
       OR NOT cloud_agents.is_valid_identifier(p_target)
       OR NOT cloud_agents.is_valid_identifier(p_incarnation)
       OR p_peer_certificate_digest !~ '^sha256:[0-9a-f]{64}$'
       OR NOT cloud_agents.is_valid_identifier(p_command_uid)
       OR NOT cloud_agents.is_valid_identifier(p_sandbox)
       OR p_sandbox_generation IS NULL OR p_sandbox_generation < 1
       OR p_result IS NULL OR p_result NOT IN ('succeeded', 'failed')
       OR p_receipt_digest !~ '^sha256:[0-9a-f]{64}$'
       OR p_result = 'succeeded' AND (
            p_exit_code IS NULL OR p_stdout IS NULL OR p_stderr IS NULL
            OR p_execution_time_millis NOT BETWEEN 0 AND 65000
            OR pg_catalog.octet_length(p_stdout) + pg_catalog.octet_length(p_stderr) > 1048576
            OR p_stable_error_code IS NOT NULL)
       OR p_result = 'failed' AND (
            p_exit_code IS NOT NULL OR p_stdout IS NOT NULL OR p_stderr IS NOT NULL
            OR p_execution_time_millis IS NOT NULL
            OR p_stable_error_code IS NULL OR p_stable_error_code NOT IN (
                'sandbox_access_unavailable', 'sandbox_runtime_unavailable',
                'sandbox_exec_output_limit', 'sandbox_exec_timeout')) THEN
        RAISE EXCEPTION USING ERRCODE = '22023', MESSAGE = 'remote worker sandbox exec receipt is invalid';
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
    FROM cloud_agents.remote_worker_sandbox_exec_commands AS command
    WHERE command.tenant_id = p_tenant AND command.project_uid = p_project
      AND command.command_uid = p_command_uid FOR UPDATE;
    IF NOT FOUND OR existing.target_uid IS DISTINCT FROM p_target
       OR existing.assigned_incarnation_uid IS DISTINCT FROM p_incarnation
       OR existing.delivery_certificate_sha256 IS DISTINCT FROM p_peer_certificate_digest
       OR existing.sandbox_uid IS DISTINCT FROM p_sandbox
       OR existing.sandbox_generation IS DISTINCT FROM p_sandbox_generation THEN
        RAISE EXCEPTION USING ERRCODE = '40001', MESSAGE = 'remote worker sandbox exec receipt conflict';
    END IF;

    IF existing.state IN ('succeeded', 'failed') THEN
        IF existing.receipt_digest IS DISTINCT FROM p_receipt_digest THEN
            RAISE EXCEPTION USING ERRCODE = '40001', MESSAGE = 'remote worker sandbox exec receipt conflict';
        END IF;
    ELSIF existing.state = 'delivered' THEN
        UPDATE cloud_agents.remote_worker_sandbox_exec_commands AS command
        SET state = p_result,
            receipt_digest = p_receipt_digest,
            exit_code = CASE WHEN p_result = 'succeeded' THEN p_exit_code END,
            stdout = CASE WHEN p_result = 'succeeded' THEN p_stdout END,
            stderr = CASE WHEN p_result = 'succeeded' THEN p_stderr END,
            execution_time_millis = CASE WHEN p_result = 'succeeded' THEN p_execution_time_millis END,
            stable_error_code = CASE WHEN p_result = 'failed' THEN p_stable_error_code END,
            settled_at = mutation_at, updated_at = mutation_at
        WHERE command.tenant_id = p_tenant AND command.project_uid = p_project
          AND command.command_uid = p_command_uid
        RETURNING * INTO existing;
    ELSE
        RAISE EXCEPTION USING ERRCODE = '40001', MESSAGE = 'remote worker sandbox exec receipt conflict';
    END IF;

    RETURN QUERY SELECT existing.state, existing.deadline_at;
END;
$$;

CREATE FUNCTION cloud_agents.get_remote_worker_sandbox_exec_v1(
    p_tenant text, p_project text, p_command_uid text, p_subject_digest text
)
RETURNS TABLE (
    command_state text, command_deadline_at timestamptz,
    exit_code integer, stdout text, stderr text, execution_time_millis bigint,
    stable_error_code text
)
LANGUAGE plpgsql SECURITY DEFINER SET search_path = pg_catalog, cloud_agents
AS $$
DECLARE
    ignored text;
    existing cloud_agents.remote_worker_sandbox_exec_commands%ROWTYPE;
BEGIN
    ignored := cloud_agents.require_tenant_id();
    IF p_tenant IS DISTINCT FROM ignored OR NOT cloud_agents.is_valid_identifier(p_project)
       OR NOT cloud_agents.is_valid_identifier(p_command_uid)
       OR p_subject_digest !~ '^sha256:[0-9a-f]{64}$' THEN
        RAISE EXCEPTION USING ERRCODE = '22023', MESSAGE = 'remote worker sandbox exec lookup is invalid';
    END IF;
    SELECT command.* INTO existing
    FROM cloud_agents.remote_worker_sandbox_exec_commands AS command
    WHERE command.tenant_id = p_tenant AND command.project_uid = p_project
      AND command.command_uid = p_command_uid AND command.requested_by = p_subject_digest;
    IF NOT FOUND THEN
        RAISE EXCEPTION USING ERRCODE = '23503', MESSAGE = 'remote worker sandbox exec was not found';
    END IF;
    IF existing.state IN ('pending', 'delivered') AND existing.deadline_at <= clock_timestamp() THEN
        RETURN QUERY SELECT 'failed'::text, existing.deadline_at, NULL::integer,
            NULL::text, NULL::text, NULL::bigint, 'sandbox_exec_timeout'::text;
    ELSE
        RETURN QUERY SELECT existing.state, existing.deadline_at, existing.exit_code,
            existing.stdout, existing.stderr, existing.execution_time_millis,
            existing.stable_error_code;
    END IF;
END;
$$;

ALTER FUNCTION cloud_agents.request_remote_worker_sandbox_exec_v1(text,text,text,text,text,text,bigint,text,text,text,text,integer,text,text,text) OWNER TO cloud_agents_migration_owner;
ALTER FUNCTION cloud_agents.claim_remote_worker_sandbox_exec_v1(text,text,text,text,text) OWNER TO cloud_agents_migration_owner;
ALTER FUNCTION cloud_agents.settle_remote_worker_sandbox_exec_v1(text,text,text,text,text,text,text,bigint,text,integer,text,text,bigint,text,text) OWNER TO cloud_agents_migration_owner;
ALTER FUNCTION cloud_agents.get_remote_worker_sandbox_exec_v1(text,text,text,text) OWNER TO cloud_agents_migration_owner;
REVOKE ALL ON FUNCTION cloud_agents.request_remote_worker_sandbox_exec_v1(text,text,text,text,text,text,bigint,text,text,text,text,integer,text,text,text) FROM PUBLIC;
REVOKE ALL ON FUNCTION cloud_agents.claim_remote_worker_sandbox_exec_v1(text,text,text,text,text) FROM PUBLIC;
REVOKE ALL ON FUNCTION cloud_agents.settle_remote_worker_sandbox_exec_v1(text,text,text,text,text,text,text,bigint,text,integer,text,text,bigint,text,text) FROM PUBLIC;
REVOKE ALL ON FUNCTION cloud_agents.get_remote_worker_sandbox_exec_v1(text,text,text,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION cloud_agents.request_remote_worker_sandbox_exec_v1(text,text,text,text,text,text,bigint,text,text,text,text,integer,text,text,text) TO cloud_agents_runtime;
GRANT EXECUTE ON FUNCTION cloud_agents.claim_remote_worker_sandbox_exec_v1(text,text,text,text,text) TO cloud_agents_runtime;
GRANT EXECUTE ON FUNCTION cloud_agents.settle_remote_worker_sandbox_exec_v1(text,text,text,text,text,text,text,bigint,text,integer,text,text,bigint,text,text) TO cloud_agents_runtime;
GRANT EXECUTE ON FUNCTION cloud_agents.get_remote_worker_sandbox_exec_v1(text,text,text,text) TO cloud_agents_runtime;
