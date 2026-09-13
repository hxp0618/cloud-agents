CREATE OR REPLACE FUNCTION cloud_agents.request_remote_worker_sandbox_pty_v3(
    p_tenant text, p_project text, p_command text, p_grant text,
    p_token_digest text, p_target text, p_workspace text, p_sandbox text,
    p_sandbox_generation bigint, p_runtime text, p_runtime_operation text,
    p_runtime_spec_digest text, p_action text, p_command_text text,
    p_session text, p_since bigint, p_takeover boolean,
    p_input_message_type text, p_input_payload bytea,
    p_ssh_session boolean, p_pty_enabled boolean, p_request_id text
)
RETURNS TABLE (
    command_state text, command_deadline_at timestamptz, bytes_transferred bigint,
    result_session_uid text, result_running boolean, result_output_offset bigint,
    result_frames jsonb, stable_error_code text
)
LANGUAGE plpgsql SECURITY DEFINER SET search_path = pg_catalog, cloud_agents
AS $$
DECLARE
    mutation_at timestamptz := transaction_timestamp();
BEGIN
    IF p_ssh_session IS NULL
       OR p_action IN ('get', 'delete') AND p_pty_enabled IS NOT NULL
       OR p_action <> 'exchange' AND p_pty_enabled IS NOT NULL
       OR p_action NOT IN ('create', 'get', 'delete', 'exchange')
       OR p_command_text IS NOT NULL AND (
            p_action <> 'create'
            OR pg_catalog.octet_length(p_command_text) NOT BETWEEN 1 AND 4096
            OR pg_catalog.strpos(p_command_text, pg_catalog.chr(13)) > 0
            OR pg_catalog.strpos(p_command_text, pg_catalog.chr(10)) > 0) THEN
        RAISE EXCEPTION USING ERRCODE = '22023', MESSAGE = 'remote worker sandbox PTY request is invalid';
    END IF;

    PERFORM 1 FROM cloud_agents.remote_worker_enrollments AS enrollment
    WHERE enrollment.tenant_id = p_tenant AND enrollment.project_uid = p_project
      AND enrollment.target_uid = p_target AND enrollment.state = 'enrolled'
      AND enrollment.certificate_state = 'active'
      AND enrollment.certificate_not_after > mutation_at
      AND enrollment.node_heartbeat_expires_at > mutation_at
      AND enrollment.node_desired_state = 'active' AND enrollment.node_observed_state = 'active'
      AND ARRAY['docker','pty']::text[] <@ enrollment.node_capabilities
      AND (NOT p_ssh_session OR 'ssh' = ANY(enrollment.node_capabilities))
    FOR SHARE;
    IF NOT FOUND THEN
        RAISE EXCEPTION USING ERRCODE = '23505', MESSAGE = 'remote worker sandbox PTY unavailable';
    END IF;

    IF p_action <> 'create' THEN
        PERFORM 1 FROM cloud_agents.remote_worker_sandbox_pty_commands AS created
        WHERE created.tenant_id = p_tenant AND created.project_uid = p_project
          AND created.grant_uid = p_grant AND created.requested_by = p_token_digest
          AND created.sandbox_uid = p_sandbox AND created.sandbox_generation = p_sandbox_generation
          AND created.action = 'create' AND created.state = 'succeeded'
          AND created.result_session_uid = p_session AND created.ssh_session = p_ssh_session
        FOR SHARE;
        IF NOT FOUND THEN
            RAISE EXCEPTION USING ERRCODE = '23505', MESSAGE = 'remote worker sandbox PTY unavailable';
        END IF;
    END IF;

    SELECT * INTO command_state, command_deadline_at, bytes_transferred,
        result_session_uid, result_running, result_output_offset, result_frames,
        stable_error_code
    FROM cloud_agents.request_remote_worker_sandbox_pty_v1(
        p_tenant, p_project, p_command, p_grant, p_token_digest, p_target,
        p_workspace, p_sandbox, p_sandbox_generation, p_runtime,
        p_runtime_operation, p_runtime_spec_digest, p_action, p_session,
        p_since, p_takeover, p_input_message_type, p_input_payload, p_request_id
    );

    UPDATE cloud_agents.remote_worker_sandbox_pty_commands AS command
    SET ssh_session = p_ssh_session, pty_enabled = p_pty_enabled,
        command_text = p_command_text
    WHERE command.tenant_id = p_tenant AND command.project_uid = p_project
      AND command.command_uid = p_command;
    IF NOT FOUND THEN
        RAISE EXCEPTION USING ERRCODE = '40001', MESSAGE = 'remote worker sandbox PTY request conflict';
    END IF;
    RETURN NEXT;
END;
$$;
