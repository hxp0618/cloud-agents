CREATE FUNCTION cloud_agents.claim_foundation_sandbox_v6(
    p_target_kind text, p_target_uid text, p_holder text, p_incarnation text, p_token text,
    p_lease_seconds integer, p_subject text, p_audit text
)
RETURNS TABLE (
    tenant_id text, event_id text, delivery_attempts integer, claim_expires_at timestamptz,
    operation_id text, operation_generation bigint, action text,
    project_uid text, workspace_uid text, workspace_name text, volume_uid text,
    physical_volume_uid text, target_uid text, target_generation bigint, target_endpoint text,
    credential_ref text, sandbox_uid text, sandbox_generation bigint, image_uri text,
    runtime_profile_uid text, runtime_profile_version bigint, cpu_millis bigint, memory_bytes bigint,
    spec_digest text, runtime_uid text, runtime_state text, runtime_operation_uid text,
    runtime_generation bigint, runtime_spec_digest text, ttl_seconds integer, expires_at timestamptz,
    network_policy_uid text, network_default_egress text, network_allowed_egress text[],
    network_preview_enabled boolean
)
LANGUAGE plpgsql SECURITY DEFINER SET search_path = pg_catalog, cloud_agents
AS $$
DECLARE
    claimed_at timestamptz := transaction_timestamp();
    candidate cloud_agents.outbox_events%ROWTYPE;
BEGIN
    PERFORM cloud_agents.require_runtime_mutation_principal();
    IF p_target_kind NOT IN ('docker', 'remote-worker')
        OR p_target_kind = 'docker' AND p_target_uid <> ''
        OR p_target_kind = 'remote-worker' AND NOT cloud_agents.is_valid_identifier(p_target_uid)
        OR NOT cloud_agents.is_valid_identifier(p_holder) OR NOT cloud_agents.is_valid_identifier(p_incarnation)
        OR NOT cloud_agents.is_valid_identifier(p_token) OR p_lease_seconds NOT BETWEEN 1 AND 60
        OR p_subject !~ '^sha256:[0-9a-f]{64}$' OR NOT cloud_agents.is_valid_identifier(p_audit)
    THEN RAISE EXCEPTION USING ERRCODE = '22023', MESSAGE = 'foundation claim input is invalid'; END IF;

    SELECT event.* INTO candidate
    FROM cloud_agents.outbox_events AS event
    JOIN cloud_agents.sandbox_sessions AS sandbox
      ON sandbox.tenant_id = event.tenant_id AND sandbox.operation_id = event.operation_id
     AND sandbox.operation_generation = event.operation_generation
     AND sandbox.sandbox_uid = event.aggregate_id AND sandbox.generation = event.generation
    JOIN cloud_agents.workspace_volumes AS volume
      ON volume.tenant_id = sandbox.tenant_id AND volume.project_uid = sandbox.project_uid
     AND volume.workspace_uid = sandbox.workspace_uid
    JOIN cloud_agents.deployment_targets AS target
      ON target.tenant_id = volume.tenant_id AND target.project_uid = volume.project_uid
     AND target.target_uid = volume.target_uid
    LEFT JOIN cloud_agents.foundation_sandbox_activity AS activity
      ON activity.tenant_id = event.tenant_id AND activity.operation_uid = event.operation_id
     AND activity.sandbox_generation = event.operation_generation
    WHERE event.profile_id = 'foundationSandboxLifecycle/v1alpha1'
      AND event.profile_digest = 'sha256:aabd6aa244c89a4850eba529dbe387132c3a0aff7ffba430f6115afb6e215a1b'
      AND event.event_class = 'operation_effect' AND event.aggregate_kind = 'sandboxSession'
      AND (event.state = 'pending' OR event.state = 'retry_wait' AND event.next_attempt_at <= claimed_at)
      AND event.delivery_attempts < 8 AND NOT sandbox.writer_released
      AND target.target_kind = p_target_kind AND (p_target_uid = '' OR target.target_uid = p_target_uid)
      AND (p_target_kind = 'docker'
           OR COALESCE(activity.action, 'sandbox.create') = 'sandbox.create'
              AND sandbox.generation = 1
              AND cloud_agents.foundation_target_available_v1(target.tenant_id, target.project_uid, target.target_uid)
           OR activity.action = 'sandbox.stop')
      AND (activity.action = 'sandbox.stop' AND sandbox.desired_state = 'stopped'
              AND sandbox.runtime_uid IS NOT NULL AND volume.observed_state = 'available'
           OR COALESCE(activity.action, 'sandbox.create') IN ('sandbox.create', 'sandbox.rebuild')
              AND sandbox.desired_state = 'running'
              AND target.observed_phase = 'ready' AND target.scheduling_state = 'active'
              AND (activity.action IS NULL OR volume.observed_state = 'available'))
      AND (p_target_kind = 'docker' OR NOT EXISTS (
          SELECT 1 FROM cloud_agents.outbox_events AS active_event
          JOIN cloud_agents.sandbox_sessions AS active_sandbox
            ON active_sandbox.tenant_id = active_event.tenant_id
           AND active_sandbox.operation_id = active_event.operation_id
           AND active_sandbox.operation_generation = active_event.operation_generation
          JOIN cloud_agents.workspace_volumes AS active_volume
            ON active_volume.tenant_id = active_sandbox.tenant_id
           AND active_volume.project_uid = active_sandbox.project_uid
           AND active_volume.workspace_uid = active_sandbox.workspace_uid
          WHERE active_event.state = 'claimed' AND active_event.claim_expires_at > claimed_at
            AND active_volume.target_uid = p_target_uid
      ))
    ORDER BY event.created_at, event.tenant_id, event.event_id
    FOR UPDATE OF event SKIP LOCKED LIMIT 1;
    IF NOT FOUND THEN RETURN; END IF;

    PERFORM pg_catalog.set_config('cloud_agents.tenant_id', candidate.tenant_id, true);
    UPDATE cloud_agents.outbox_events AS event SET state = 'claimed',
        delivery_attempts = event.delivery_attempts + 1, next_attempt_at = NULL,
        claim_holder_id = p_holder, claim_incarnation = p_incarnation, claim_token = p_token,
        claim_started_at = claimed_at,
        claim_expires_at = claimed_at + pg_catalog.make_interval(secs => p_lease_seconds), updated_at = claimed_at
    WHERE event.tenant_id = candidate.tenant_id AND event.event_id = candidate.event_id
    RETURNING event.* INTO candidate;
    UPDATE cloud_agents.platform_operations AS operation SET
        state = CASE WHEN candidate.delivery_attempts = 1 THEN 'running' ELSE 'reconciling' END,
        recovery_generation = candidate.delivery_attempts - 1,
        current_attempt_number = candidate.delivery_attempts, updated_at = claimed_at
    WHERE operation.tenant_id = candidate.tenant_id AND operation.operation_id = candidate.operation_id
      AND operation.operation_generation = candidate.operation_generation
      AND operation.state IN ('pending', 'running', 'reconciling');
    IF NOT FOUND THEN RAISE EXCEPTION USING ERRCODE = '40001', MESSAGE = 'foundation operation is not claimable'; END IF;
    INSERT INTO cloud_agents.operation_attempts (
        tenant_id, tenant_ref_id, operation_id, operation_generation, attempt_number, state,
        claim_holder_id, claim_incarnation, claim_token, claim_started_at, claim_expires_at,
        created_at, updated_at
    ) VALUES (
        candidate.tenant_id, candidate.tenant_id, candidate.operation_id, candidate.operation_generation,
        candidate.delivery_attempts, 'claimed', p_holder, p_incarnation, p_token,
        claimed_at, candidate.claim_expires_at, claimed_at, claimed_at
    );
    PERFORM cloud_agents.append_coordination_audit(
        candidate.tenant_id, p_audit, candidate.profile_id, candidate.profile_digest,
        p_subject, candidate.operation_id, candidate.operation_generation, candidate.delivery_attempts,
        NULL, NULL, NULL, 'sandbox.claim', 'pending', NULL, NULL, claimed_at
    );

    RETURN QUERY SELECT candidate.tenant_id, candidate.event_id, candidate.delivery_attempts,
        candidate.claim_expires_at, candidate.operation_id, candidate.operation_generation,
        COALESCE(activity.action, 'sandbox.create'), sandbox.project_uid, sandbox.workspace_uid,
        workspace.workspace_name, volume.volume_uid, volume.physical_volume_uid, volume.target_uid,
        target.generation, target.endpoint, target.credential_ref, sandbox.sandbox_uid,
        sandbox.generation, sandbox.image_uri, sandbox.runtime_profile_uid, sandbox.runtime_profile_version,
        sandbox.cpu_millis, sandbox.memory_bytes, sandbox.spec_digest, sandbox.runtime_uid,
        sandbox.runtime_state, sandbox.runtime_operation_uid, sandbox.runtime_generation,
        sandbox.runtime_spec_digest, sandbox.ttl_seconds, sandbox.expires_at,
        COALESCE(profile.network_policy_ref, ''), COALESCE(policy.default_egress, ''),
        COALESCE(policy.allowed_egress, ARRAY[]::text[]), COALESCE(policy.preview_enabled, false)
    FROM cloud_agents.sandbox_sessions AS sandbox
    JOIN cloud_agents.workspaces AS workspace USING (tenant_id, project_uid, workspace_uid)
    JOIN cloud_agents.workspace_volumes AS volume USING (tenant_id, project_uid, workspace_uid)
    JOIN cloud_agents.deployment_targets AS target
      ON target.tenant_id = volume.tenant_id AND target.project_uid = volume.project_uid
     AND target.target_uid = volume.target_uid
    JOIN cloud_agents.runtime_profiles AS profile
      ON profile.tenant_id = sandbox.tenant_id AND profile.project_uid = sandbox.project_uid
     AND profile.profile_uid = sandbox.runtime_profile_uid
     AND profile.profile_version = sandbox.runtime_profile_version
    LEFT JOIN cloud_agents.network_policies AS policy
      ON policy.tenant_id = profile.tenant_id AND policy.project_uid = profile.project_uid
     AND policy.policy_uid = profile.network_policy_ref
    LEFT JOIN cloud_agents.foundation_sandbox_activity AS activity
      ON activity.tenant_id = candidate.tenant_id AND activity.operation_uid = candidate.operation_id
     AND activity.sandbox_generation = candidate.operation_generation
    WHERE sandbox.tenant_id = candidate.tenant_id AND sandbox.operation_id = candidate.operation_id
      AND sandbox.operation_generation = candidate.operation_generation
      AND sandbox.sandbox_uid = candidate.aggregate_id AND sandbox.generation = candidate.generation;
    IF NOT FOUND THEN RAISE EXCEPTION USING ERRCODE = '40001', MESSAGE = 'foundation effect authority drifted'; END IF;
END;
$$;

CREATE FUNCTION cloud_agents.settle_remote_worker_foundation_sandbox_v2(
    p_tenant text, p_project text, p_target text, p_incarnation text,
    p_command text, p_attempt bigint, p_action text, p_operation text, p_sandbox text, p_sandbox_generation bigint,
    p_transition text, p_runtime text, p_runtime_state text, p_volume text, p_error text,
    p_cleanup_complete boolean, p_subject text, p_audit text
)
RETURNS TABLE (outbox_state text, operation_state text, resource_version bigint)
LANGUAGE plpgsql SECURITY DEFINER SET search_path = pg_catalog, cloud_agents
AS $$
DECLARE
    event cloud_agents.outbox_events%ROWTYPE;
    expected_command text;
BEGIN
    PERFORM cloud_agents.require_runtime_mutation_principal();
    expected_command := 'rwsc-' || pg_catalog.substr(pg_catalog.encode(
        pg_catalog.sha256(pg_catalog.convert_to(p_operation || '|' || p_attempt::text, 'UTF8')), 'hex'), 1, 32);
    IF p_tenant IS DISTINCT FROM cloud_agents.require_tenant_id()
        OR NOT cloud_agents.is_valid_identifier(p_project) OR NOT cloud_agents.is_valid_identifier(p_target)
        OR NOT cloud_agents.is_valid_identifier(p_incarnation) OR p_command IS DISTINCT FROM expected_command
        OR p_attempt NOT BETWEEN 1 AND 8 OR p_action NOT IN ('sandbox.create', 'sandbox.stop')
        OR NOT cloud_agents.is_valid_identifier(p_operation)
        OR NOT cloud_agents.is_valid_identifier(p_sandbox) OR p_sandbox_generation < 1
        OR p_transition NOT IN ('succeeded', 'retry', 'failed') OR p_cleanup_complete IS NULL
        OR p_transition = 'succeeded' AND (
            NOT cloud_agents.is_valid_identifier(p_volume) OR p_error IS NOT NULL
            OR p_action = 'sandbox.create' AND (
                NOT cloud_agents.is_valid_identifier(p_runtime) OR p_runtime_state <> 'Running')
            OR p_action = 'sandbox.stop' AND (
                p_runtime IS NOT NULL OR p_runtime_state IS NOT NULL OR NOT p_cleanup_complete))
        OR p_transition <> 'succeeded' AND NOT cloud_agents.is_valid_identifier(p_error)
        OR p_subject !~ '^sha256:[0-9a-f]{64}$' OR NOT cloud_agents.is_valid_identifier(p_audit)
    THEN RAISE EXCEPTION USING ERRCODE = '22023', MESSAGE = 'remote worker sandbox receipt is invalid'; END IF;

    SELECT stored.* INTO event
    FROM cloud_agents.outbox_events AS stored
    JOIN cloud_agents.sandbox_sessions AS sandbox
      ON sandbox.tenant_id = stored.tenant_id AND sandbox.operation_id = stored.operation_id
     AND sandbox.operation_generation = stored.operation_generation
     AND sandbox.sandbox_uid = stored.aggregate_id AND sandbox.generation = stored.generation
    JOIN cloud_agents.workspace_volumes AS volume
      ON volume.tenant_id = sandbox.tenant_id AND volume.project_uid = sandbox.project_uid
     AND volume.workspace_uid = sandbox.workspace_uid
    LEFT JOIN cloud_agents.foundation_sandbox_activity AS activity
      ON activity.tenant_id = stored.tenant_id AND activity.operation_uid = stored.operation_id
     AND activity.sandbox_generation = stored.operation_generation
    JOIN cloud_agents.remote_worker_enrollments AS enrollment
      ON enrollment.tenant_id = volume.tenant_id AND enrollment.project_uid = volume.project_uid
     AND enrollment.target_uid = volume.target_uid
    WHERE stored.tenant_id = p_tenant AND sandbox.project_uid = p_project
      AND volume.target_uid = p_target AND enrollment.incarnation_uid = p_incarnation
      AND stored.operation_id = p_operation AND stored.delivery_attempts = p_attempt
      AND COALESCE(activity.action, 'sandbox.create') = p_action
      AND stored.aggregate_id = p_sandbox AND stored.generation = p_sandbox_generation
      AND stored.state = 'claimed' AND stored.claim_holder_id = p_target
      AND stored.claim_incarnation = p_incarnation FOR UPDATE OF stored;
    IF FOUND THEN
        RETURN QUERY SELECT * FROM cloud_agents.settle_foundation_sandbox_v2(
            p_tenant, event.event_id, p_target, p_incarnation, event.claim_token,
            event.claim_expires_at, p_transition, p_runtime, p_runtime_state, p_volume,
            p_error, p_cleanup_complete, p_subject, p_audit
        );
        RETURN;
    END IF;

    PERFORM 1 FROM cloud_agents.operation_attempts AS attempt
    JOIN cloud_agents.sandbox_sessions AS sandbox
      ON sandbox.tenant_id = attempt.tenant_id AND sandbox.operation_id = attempt.operation_id
     AND sandbox.operation_generation = attempt.operation_generation
    JOIN cloud_agents.workspace_volumes AS volume
      ON volume.tenant_id = sandbox.tenant_id AND volume.project_uid = sandbox.project_uid
     AND volume.workspace_uid = sandbox.workspace_uid
    LEFT JOIN cloud_agents.foundation_sandbox_activity AS activity
      ON activity.tenant_id = attempt.tenant_id AND activity.operation_uid = attempt.operation_id
     AND activity.sandbox_generation = attempt.operation_generation
    JOIN cloud_agents.remote_worker_enrollments AS enrollment
      ON enrollment.tenant_id = volume.tenant_id AND enrollment.project_uid = volume.project_uid
     AND enrollment.target_uid = volume.target_uid
    WHERE attempt.tenant_id = p_tenant AND sandbox.project_uid = p_project
      AND volume.target_uid = p_target AND enrollment.incarnation_uid = p_incarnation
      AND attempt.operation_id = p_operation AND attempt.attempt_number = p_attempt
      AND COALESCE(activity.action, 'sandbox.create') = p_action
      AND sandbox.sandbox_uid = p_sandbox AND sandbox.generation = p_sandbox_generation
      AND (p_transition = 'succeeded' AND attempt.state = 'succeeded'
           AND (p_action = 'sandbox.create' AND sandbox.observed_state = 'running'
                  AND sandbox.runtime_uid = p_runtime AND sandbox.runtime_state = p_runtime_state
                  AND volume.physical_volume_uid = p_volume
                OR p_action = 'sandbox.stop' AND sandbox.observed_state = 'stopped'
                  AND sandbox.runtime_uid IS NULL AND sandbox.runtime_state = ''
                  AND volume.physical_volume_uid = p_volume)
           OR p_transition <> 'succeeded' AND attempt.state = 'failed'
              AND attempt.stable_error_code = p_error);
    IF NOT FOUND THEN
        RAISE EXCEPTION USING ERRCODE = '40001', MESSAGE = 'remote worker sandbox receipt conflict';
    END IF;
    RETURN QUERY SELECT
        CASE WHEN operation.state = 'succeeded' THEN 'delivered'
             WHEN operation.state = 'failed' THEN 'dead_letter' ELSE 'retry_wait' END,
        operation.state, operation.terminal_resource_version
    FROM cloud_agents.platform_operations AS operation
    WHERE operation.tenant_id = p_tenant AND operation.operation_id = p_operation
      AND operation.operation_generation = p_sandbox_generation;
END;
$$;

ALTER FUNCTION cloud_agents.claim_foundation_sandbox_v6(text,text,text,text,text,integer,text,text) OWNER TO cloud_agents_migration_owner;
ALTER FUNCTION cloud_agents.settle_remote_worker_foundation_sandbox_v2(text,text,text,text,text,bigint,text,text,text,bigint,text,text,text,text,text,boolean,text,text) OWNER TO cloud_agents_migration_owner;
REVOKE ALL ON FUNCTION cloud_agents.claim_foundation_sandbox_v6(text,text,text,text,text,integer,text,text) FROM PUBLIC;
REVOKE ALL ON FUNCTION cloud_agents.settle_remote_worker_foundation_sandbox_v2(text,text,text,text,text,bigint,text,text,text,bigint,text,text,text,text,text,boolean,text,text) FROM PUBLIC;
REVOKE EXECUTE ON FUNCTION cloud_agents.claim_foundation_sandbox_v5(text,text,text,text,text,integer,text,text) FROM cloud_agents_runtime;
REVOKE EXECUTE ON FUNCTION cloud_agents.settle_remote_worker_foundation_sandbox_v1(text,text,text,text,text,bigint,text,text,bigint,text,text,text,text,text,boolean,text,text) FROM cloud_agents_runtime;
GRANT EXECUTE ON FUNCTION cloud_agents.claim_foundation_sandbox_v6(text,text,text,text,text,integer,text,text) TO cloud_agents_runtime;
GRANT EXECUTE ON FUNCTION cloud_agents.settle_remote_worker_foundation_sandbox_v2(text,text,text,text,text,bigint,text,text,text,bigint,text,text,text,text,text,boolean,text,text) TO cloud_agents_runtime;
