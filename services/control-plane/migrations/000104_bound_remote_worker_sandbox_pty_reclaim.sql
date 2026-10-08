CREATE OR REPLACE FUNCTION cloud_agents.claim_remote_worker_sandbox_pty_v1(
    p_tenant text, p_project text, p_target text,
    p_incarnation text, p_peer_certificate_digest text
)
RETURNS TABLE (
    command_uid text, grant_uid text, workspace_uid text, target_uid text,
    sandbox_uid text, sandbox_generation bigint, runtime_uid text,
    runtime_operation_uid text, runtime_spec_digest text, action text,
    session_uid text, since_offset bigint, takeover boolean,
    input_message_type text, input_payload bytea, command_deadline_at timestamptz
)
LANGUAGE plpgsql SECURITY DEFINER SET search_path = pg_catalog, cloud_agents
AS $$
DECLARE
    ignored text;
    existing cloud_agents.remote_worker_sandbox_pty_commands%ROWTYPE;
    claimed_at timestamptz := transaction_timestamp();
BEGIN
    ignored := cloud_agents.require_tenant_id();
    IF p_tenant IS DISTINCT FROM ignored OR NOT cloud_agents.is_valid_identifier(p_project)
       OR NOT cloud_agents.is_valid_identifier(p_target)
       OR NOT cloud_agents.is_valid_identifier(p_incarnation)
       OR p_peer_certificate_digest !~ '^sha256:[0-9a-f]{64}$' THEN
        RAISE EXCEPTION USING ERRCODE = '22023', MESSAGE = 'remote worker sandbox PTY claim is invalid';
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
      AND ARRAY['docker','pty']::text[] <@ enrollment.node_capabilities
    FOR SHARE;
    IF NOT FOUND THEN
        RAISE EXCEPTION USING ERRCODE = '28000', MESSAGE = 'remote worker certificate authentication failed';
    END IF;

    SELECT command.* INTO existing
    FROM cloud_agents.remote_worker_sandbox_pty_commands AS command
    JOIN cloud_agents.sandbox_access_grants AS access_grant
      ON access_grant.tenant_id = command.tenant_id AND access_grant.project_uid = command.project_uid
     AND access_grant.grant_uid = command.grant_uid
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
      -- A delivered command may belong to a predecessor incarnation. Keep
      -- same-incarnation delivery replayable, but require the old delivery
      -- lease to expire before a different incarnation can take it over.
      -- The 30-second window matches the heartbeat lease; settlement still
      -- requires the current incarnation and certificate below.
      AND (
          command.state = 'pending'
          OR command.state = 'delivered' AND (
              (command.assigned_incarnation_uid = p_incarnation
               AND command.delivery_certificate_sha256 = p_peer_certificate_digest)
              OR (
                  (command.assigned_incarnation_uid IS DISTINCT FROM p_incarnation
                   OR command.delivery_certificate_sha256 IS DISTINCT FROM p_peer_certificate_digest)
                  AND command.delivered_at IS NOT NULL
                  AND command.delivered_at + interval '30 seconds' <= claimed_at
              )
          )
      )
      -- Do not skip over a recent delivery from another incarnation by
      -- selecting a later pending PTY command for the same target.
      AND NOT EXISTS (
          SELECT 1
          FROM cloud_agents.remote_worker_sandbox_pty_commands AS blocker
          WHERE blocker.tenant_id = p_tenant AND blocker.project_uid = p_project
            AND blocker.target_uid = p_target AND blocker.deadline_at > claimed_at
            AND blocker.state = 'delivered'
            AND NOT (blocker.assigned_incarnation_uid = p_incarnation
                     AND blocker.delivery_certificate_sha256 = p_peer_certificate_digest)
            AND blocker.delivered_at IS NOT NULL
            AND blocker.delivered_at > claimed_at - interval '30 seconds'
      )
      AND access_grant.token_digest = command.requested_by AND access_grant.status = 'active'
      AND access_grant.expires_at > claimed_at
      AND access_grant.sandbox_generation = command.sandbox_generation
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
      AND (command.action = 'create' OR EXISTS (
          SELECT 1 FROM cloud_agents.sandbox_pty_sessions AS session
          WHERE session.tenant_id = command.tenant_id AND session.project_uid = command.project_uid
            AND session.grant_uid = command.grant_uid AND session.session_uid = command.session_uid
            AND session.sandbox_uid = command.sandbox_uid
            AND session.sandbox_generation = command.sandbox_generation AND session.deleted_at IS NULL
      ))
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
      AND NOT EXISTS (
          SELECT 1 FROM cloud_agents.remote_worker_sandbox_file_commands AS file_command
          WHERE file_command.tenant_id = p_tenant AND file_command.project_uid = p_project
            AND file_command.target_uid = p_target AND file_command.deadline_at > claimed_at
            AND file_command.state IN ('pending', 'delivered')
      )
    ORDER BY (command.state = 'delivered') DESC, command.created_at, command.command_uid
    FOR UPDATE OF command SKIP LOCKED LIMIT 1;
    IF NOT FOUND THEN RETURN; END IF;

    IF existing.state = 'pending' OR existing.state = 'delivered'
       AND (existing.assigned_incarnation_uid IS DISTINCT FROM p_incarnation
            OR existing.delivery_certificate_sha256 IS DISTINCT FROM p_peer_certificate_digest) THEN
        UPDATE cloud_agents.remote_worker_sandbox_pty_commands AS command
        SET state = 'delivered', assigned_incarnation_uid = p_incarnation,
            delivery_certificate_sha256 = p_peer_certificate_digest,
            delivered_at = claimed_at, updated_at = claimed_at
        WHERE command.tenant_id = p_tenant AND command.project_uid = p_project
          AND command.command_uid = existing.command_uid
        RETURNING * INTO existing;
    END IF;

    RETURN QUERY SELECT existing.command_uid, existing.grant_uid,
        existing.workspace_uid, existing.target_uid, existing.sandbox_uid,
        existing.sandbox_generation, existing.runtime_uid,
        existing.runtime_operation_uid, existing.runtime_spec_digest,
        existing.action, existing.session_uid, existing.since_offset,
        existing.takeover, existing.input_message_type, existing.input_payload,
        existing.deadline_at;
END;
$$;
