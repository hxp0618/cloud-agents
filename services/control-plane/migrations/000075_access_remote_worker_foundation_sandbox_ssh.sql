ALTER TABLE cloud_agents.remote_worker_sandbox_pty_commands
    ADD COLUMN ssh_session boolean NOT NULL DEFAULT false,
    ADD COLUMN pty_enabled boolean,
    ADD CONSTRAINT remote_worker_sandbox_pty_ssh_shape CHECK (
        (pty_enabled IS NULL OR action = 'exchange')
        AND (ssh_session OR pty_enabled IS NULL)
    );

CREATE FUNCTION cloud_agents.request_remote_worker_sandbox_pty_v2(
    p_tenant text, p_project text, p_command text, p_grant text,
    p_token_digest text, p_target text, p_workspace text, p_sandbox text,
    p_sandbox_generation bigint, p_runtime text, p_runtime_operation text,
    p_runtime_spec_digest text, p_action text, p_session text, p_since bigint,
    p_takeover boolean, p_input_message_type text, p_input_payload bytea,
    p_ssh_session boolean, p_pty_enabled boolean,
    p_request_id text
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
       OR NOT p_ssh_session AND p_pty_enabled IS NOT NULL
       OR p_action IN ('create', 'get', 'delete') AND p_pty_enabled IS NOT NULL
       OR p_action = 'exchange' AND p_ssh_session AND p_pty_enabled IS NULL
       OR p_action NOT IN ('create', 'get', 'delete', 'exchange') THEN
        RAISE EXCEPTION USING ERRCODE = '22023', MESSAGE = 'remote worker sandbox PTY request is invalid';
    END IF;

    IF p_ssh_session THEN
        PERFORM 1 FROM cloud_agents.remote_worker_enrollments AS enrollment
        WHERE enrollment.tenant_id = p_tenant AND enrollment.project_uid = p_project
          AND enrollment.target_uid = p_target AND enrollment.state = 'enrolled'
          AND enrollment.certificate_state = 'active'
          AND enrollment.certificate_not_after > mutation_at
          AND enrollment.node_heartbeat_expires_at > mutation_at
          AND enrollment.node_desired_state = 'active' AND enrollment.node_observed_state = 'active'
          AND ARRAY['docker','pty','ssh']::text[] <@ enrollment.node_capabilities
        FOR SHARE;
        IF NOT FOUND THEN
            RAISE EXCEPTION USING ERRCODE = '23505', MESSAGE = 'remote worker sandbox PTY unavailable';
        END IF;
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
    SET ssh_session = p_ssh_session, pty_enabled = p_pty_enabled
    WHERE command.tenant_id = p_tenant AND command.project_uid = p_project
      AND command.command_uid = p_command;
    IF NOT FOUND THEN
        RAISE EXCEPTION USING ERRCODE = '40001', MESSAGE = 'remote worker sandbox PTY request conflict';
    END IF;
    RETURN NEXT;
END;
$$;

CREATE FUNCTION cloud_agents.claim_remote_worker_sandbox_pty_v2(
    p_tenant text, p_project text, p_target text,
    p_incarnation text, p_peer_certificate_digest text
)
RETURNS TABLE (
    command_uid text, grant_uid text, workspace_uid text, target_uid text,
    sandbox_uid text, sandbox_generation bigint, runtime_uid text,
    runtime_operation_uid text, runtime_spec_digest text, action text,
    session_uid text, since_offset bigint, takeover boolean,
    input_message_type text, input_payload bytea, pty_enabled boolean,
    command_deadline_at timestamptz
)
LANGUAGE plpgsql SECURITY DEFINER SET search_path = pg_catalog, cloud_agents
AS $$
DECLARE
    claimed record;
    extension record;
    claimed_at timestamptz := transaction_timestamp();
BEGIN
    SELECT * INTO claimed
    FROM cloud_agents.claim_remote_worker_sandbox_pty_v1(
        p_tenant, p_project, p_target, p_incarnation, p_peer_certificate_digest
    );
    IF NOT FOUND THEN RETURN; END IF;

    SELECT command.ssh_session, command.pty_enabled
    INTO extension
    FROM cloud_agents.remote_worker_sandbox_pty_commands AS command
    WHERE command.tenant_id = p_tenant AND command.project_uid = p_project
      AND command.command_uid = claimed.command_uid;
    IF NOT FOUND THEN
        RAISE EXCEPTION USING ERRCODE = '40001', MESSAGE = 'remote worker sandbox PTY claim conflict';
    END IF;

    IF extension.ssh_session AND NOT EXISTS (
        SELECT 1 FROM cloud_agents.remote_worker_enrollments AS enrollment
        WHERE enrollment.tenant_id = p_tenant AND enrollment.project_uid = p_project
          AND enrollment.target_uid = p_target AND enrollment.state = 'enrolled'
          AND enrollment.certificate_state = 'active'
          AND enrollment.certificate_sha256 = p_peer_certificate_digest
          AND enrollment.certificate_not_after > claimed_at
          AND enrollment.incarnation_uid = p_incarnation
          AND enrollment.node_heartbeat_expires_at > claimed_at
          AND enrollment.node_desired_state = 'active' AND enrollment.node_observed_state = 'active'
          AND ARRAY['docker','pty','ssh']::text[] <@ enrollment.node_capabilities
    ) THEN
        RAISE EXCEPTION USING ERRCODE = '28000', MESSAGE = 'remote worker certificate authentication failed';
    END IF;

    RETURN QUERY SELECT claimed.command_uid, claimed.grant_uid,
        claimed.workspace_uid, claimed.target_uid, claimed.sandbox_uid,
        claimed.sandbox_generation, claimed.runtime_uid,
        claimed.runtime_operation_uid, claimed.runtime_spec_digest,
        claimed.action, claimed.session_uid, claimed.since_offset,
        claimed.takeover, claimed.input_message_type, claimed.input_payload,
        extension.pty_enabled, claimed.command_deadline_at;
END;
$$;

ALTER FUNCTION cloud_agents.request_remote_worker_sandbox_pty_v2(text,text,text,text,text,text,text,text,bigint,text,text,text,text,text,bigint,boolean,text,bytea,boolean,boolean,text) OWNER TO cloud_agents_migration_owner;
ALTER FUNCTION cloud_agents.claim_remote_worker_sandbox_pty_v2(text,text,text,text,text) OWNER TO cloud_agents_migration_owner;
REVOKE ALL ON FUNCTION cloud_agents.request_remote_worker_sandbox_pty_v2(text,text,text,text,text,text,text,text,bigint,text,text,text,text,text,bigint,boolean,text,bytea,boolean,boolean,text) FROM PUBLIC;
REVOKE ALL ON FUNCTION cloud_agents.claim_remote_worker_sandbox_pty_v2(text,text,text,text,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION cloud_agents.request_remote_worker_sandbox_pty_v2(text,text,text,text,text,text,text,text,bigint,text,text,text,text,text,bigint,boolean,text,bytea,boolean,boolean,text) TO cloud_agents_runtime;
GRANT EXECUTE ON FUNCTION cloud_agents.claim_remote_worker_sandbox_pty_v2(text,text,text,text,text) TO cloud_agents_runtime;
