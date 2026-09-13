-- Enable portable Workspace snapshot claims and restores on Kubernetes targets.

CREATE OR REPLACE FUNCTION cloud_agents.accept_foundation_workspace_snapshot_v1(
    p_project text, p_snapshot text, p_sandbox text, p_expected_sandbox_generation bigint,
    p_subject text, p_key text, p_digest text
)
RETURNS text LANGUAGE plpgsql SECURITY DEFINER SET search_path = pg_catalog, cloud_agents
AS $$
DECLARE
    actor text;
    tenant text;
    source_volume text;
    source_physical_volume text;
    selected_workspace text;
    selected_workspace_version bigint;
    selected_target text;
    selected_image text;
    operation_value text;
    event_value text;
    prior_digest text;
    prior_operation text;
BEGIN
    actor := cloud_agents.require_runtime_mutation_principal();
    tenant := cloud_agents.require_tenant_id();
    IF NOT cloud_agents.is_valid_identifier(p_project) OR NOT cloud_agents.is_valid_identifier(p_snapshot)
        OR NOT cloud_agents.is_valid_identifier(p_sandbox) OR p_expected_sandbox_generation < 1
        OR p_subject !~ '^sha256:[0-9a-f]{64}$' OR p_key !~ '^[A-Za-z0-9._~-]{16,128}$'
        OR p_digest !~ '^sha256:[0-9a-f]{64}$'
    THEN RAISE EXCEPTION USING ERRCODE = '22023', MESSAGE = 'workspace snapshot request is invalid'; END IF;

    SELECT record.request_digest, record.operation_id INTO prior_digest, prior_operation
    FROM cloud_agents.idempotency_records AS record
    WHERE record.tenant_id = tenant AND record.subject_digest = p_subject
      AND record.profile_id = 'foundationWorkspaceSnapshot/v1alpha1' AND record.profile_digest = 'sha256:76432dc8e1962f28c4679c70fb4bcedc93171945ff816f625fb4c66aed3c177f'
      AND record.idempotency_key = p_key FOR UPDATE;
    IF FOUND THEN
        IF prior_digest IS DISTINCT FROM p_digest OR NOT EXISTS (
            SELECT 1 FROM cloud_agents.workspace_snapshots AS snapshot
            WHERE snapshot.tenant_id = tenant AND snapshot.project_uid = p_project
              AND snapshot.snapshot_uid = p_snapshot AND snapshot.source_sandbox_uid = p_sandbox
              AND snapshot.source_sandbox_generation = p_expected_sandbox_generation
              AND snapshot.operation_id = prior_operation
        ) THEN RAISE EXCEPTION USING ERRCODE = '23505', MESSAGE = 'workspace snapshot idempotency conflict'; END IF;
        RETURN prior_operation;
    END IF;

    SELECT sandbox.workspace_uid, workspace.resource_version, volume.volume_uid,
      volume.physical_volume_uid, volume.target_uid, sandbox.image_uri
      INTO selected_workspace, selected_workspace_version, source_volume,
        source_physical_volume, selected_target, selected_image
    FROM cloud_agents.sandbox_sessions AS sandbox
    JOIN cloud_agents.workspaces AS workspace
      ON workspace.tenant_id = sandbox.tenant_id AND workspace.project_uid = sandbox.project_uid AND workspace.workspace_uid = sandbox.workspace_uid
    JOIN cloud_agents.workspace_volumes AS volume
      ON volume.tenant_id = sandbox.tenant_id AND volume.project_uid = sandbox.project_uid AND volume.workspace_uid = sandbox.workspace_uid
    JOIN cloud_agents.deployment_targets AS target
      ON target.tenant_id = volume.tenant_id AND target.project_uid = volume.project_uid AND target.target_uid = volume.target_uid
    WHERE sandbox.tenant_id = tenant AND sandbox.project_uid = p_project AND sandbox.sandbox_uid = p_sandbox
      AND sandbox.generation = p_expected_sandbox_generation AND sandbox.desired_state = 'stopped'
      AND sandbox.observed_state = 'stopped' AND sandbox.observed_generation = sandbox.generation AND sandbox.writer_released
      AND volume.observed_state = 'available' AND volume.physical_volume_uid IS NOT NULL
      AND target.target_kind IN ('docker', 'kubernetes') AND target.observed_phase = 'ready' AND target.scheduling_state = 'active'
    FOR SHARE OF sandbox, workspace, volume, target;
    IF NOT FOUND OR EXISTS (
        SELECT 1 FROM cloud_agents.sandbox_sessions AS writer
        WHERE writer.tenant_id = tenant AND writer.project_uid = p_project
          AND writer.workspace_uid = selected_workspace AND NOT writer.writer_released
    ) THEN RAISE EXCEPTION USING ERRCODE = '23505', MESSAGE = 'workspace snapshot requires an offline source'; END IF;

    BEGIN
        UPDATE cloud_agents.sandbox_sessions SET writer_released = false
        WHERE tenant_id = tenant AND project_uid = p_project AND sandbox_uid = p_sandbox
          AND generation = p_expected_sandbox_generation AND desired_state = 'stopped'
          AND observed_state = 'stopped' AND observed_generation = generation AND writer_released;
    EXCEPTION WHEN unique_violation THEN
        RAISE EXCEPTION USING ERRCODE = '23505', MESSAGE = 'workspace snapshot source conflict';
    END;
    IF NOT FOUND THEN RAISE EXCEPTION USING ERRCODE = '23505', MESSAGE = 'workspace snapshot source conflict'; END IF;

    operation_value := 'op-' || pg_catalog.md5(tenant || '|' || p_project || '|' || p_snapshot || '|' || p_key);
    event_value := 'evt-' || pg_catalog.md5(tenant || '|' || p_project || '|' || p_snapshot || '|' || p_key);
    INSERT INTO cloud_agents.idempotency_records
        (tenant_id, tenant_ref_id, subject_digest, registry_digest, profile_id, profile_digest,
         idempotency_key, request_digest, state, expires_at)
    VALUES (tenant, tenant, p_subject, 'sha256:b1398bb866a44269a21a101f6e4d2b12eb19cbccd001b7f673010eecbf015f94', 'foundationWorkspaceSnapshot/v1alpha1', 'sha256:76432dc8e1962f28c4679c70fb4bcedc93171945ff816f625fb4c66aed3c177f',
        p_key, p_digest, 'pending', transaction_timestamp() + interval '1 day');
    INSERT INTO cloud_agents.platform_operations
        (tenant_id, tenant_ref_id, operation_id, operation_generation, registry_digest, state_machine_digest,
         policy_digest, profile_id, profile_digest, subject_digest, request_digest, state, cleanup_phase,
         recovery_generation, current_attempt_number)
    VALUES (tenant, tenant, operation_value, 1, 'sha256:b1398bb866a44269a21a101f6e4d2b12eb19cbccd001b7f673010eecbf015f94', cloud_agents.coordination_state_machine_digest(),
        cloud_agents.coordination_policy_digest(), 'foundationWorkspaceSnapshot/v1alpha1', 'sha256:76432dc8e1962f28c4679c70fb4bcedc93171945ff816f625fb4c66aed3c177f', p_subject, p_digest,
        'pending', 'none', 0, 0);
    INSERT INTO cloud_agents.workspace_snapshots
        (tenant_id, project_uid, snapshot_uid, source_workspace_uid, source_workspace_resource_version,
         source_sandbox_uid, source_sandbox_generation, source_volume_uid, target_uid, operation_id, source_physical_volume_uid, image_uri)
    VALUES (tenant, p_project, p_snapshot, selected_workspace, selected_workspace_version, p_sandbox, p_expected_sandbox_generation,
        source_volume, selected_target, operation_value, source_physical_volume, selected_image);
    INSERT INTO cloud_agents.operation_finalizers
        (tenant_id, tenant_ref_id, operation_id, operation_generation, finalizer_name, required, state, delivery_attempts)
    VALUES (tenant, tenant, operation_value, 1, 'workspace-snapshot-copy', true, 'pending', 0);
    INSERT INTO cloud_agents.outbox_events
        (tenant_id, tenant_ref_id, event_id, registry_digest, profile_id, profile_digest, event_class,
         aggregate_kind, aggregate_id, aggregate_sequence, generation, operation_id, operation_generation,
         payload_digest, state, delivery_attempts)
    VALUES (tenant, tenant, event_value, 'sha256:b1398bb866a44269a21a101f6e4d2b12eb19cbccd001b7f673010eecbf015f94', 'foundationWorkspaceSnapshot/v1alpha1', 'sha256:76432dc8e1962f28c4679c70fb4bcedc93171945ff816f625fb4c66aed3c177f', 'operation_effect',
        'workspaceSnapshot', p_snapshot, 1, 1, operation_value, 1, p_digest, 'pending', 0);
    INSERT INTO cloud_agents.coordination_audit_facts
        (tenant_id, tenant_ref_id, audit_fact_id, registry_digest, profile_id, profile_digest,
         subject_digest, operation_id, operation_generation, transition, outcome)
    VALUES (tenant, tenant, event_value, 'sha256:b1398bb866a44269a21a101f6e4d2b12eb19cbccd001b7f673010eecbf015f94', 'foundationWorkspaceSnapshot/v1alpha1', 'sha256:76432dc8e1962f28c4679c70fb4bcedc93171945ff816f625fb4c66aed3c177f',
        p_subject, operation_value, 1, 'workspace.snapshot.accept', 'pending');
    UPDATE cloud_agents.idempotency_records SET operation_id = operation_value, operation_generation = 1
    WHERE tenant_id = tenant AND subject_digest = p_subject AND profile_id = 'foundationWorkspaceSnapshot/v1alpha1'
      AND profile_digest = 'sha256:76432dc8e1962f28c4679c70fb4bcedc93171945ff816f625fb4c66aed3c177f' AND idempotency_key = p_key;
    RETURN operation_value;
END;
$$;


CREATE OR REPLACE FUNCTION cloud_agents.claim_foundation_workspace_snapshot_v1(
    p_holder text, p_incarnation text, p_token text, p_lease_seconds integer, p_subject text, p_audit text
)
RETURNS TABLE (
    tenant_id text, event_id text, delivery_attempts integer, claim_expires_at timestamptz,
    operation_id text, project_uid text, snapshot_uid text, workspace_uid text,
    target_uid text, target_endpoint text, credential_ref text, source_volume_uid text, image_uri text
)
LANGUAGE plpgsql SECURITY DEFINER SET search_path = pg_catalog, cloud_agents
AS $$
DECLARE claimed_at timestamptz := transaction_timestamp(); candidate cloud_agents.outbox_events%ROWTYPE;
BEGIN
    PERFORM cloud_agents.require_runtime_mutation_principal();
    IF NOT cloud_agents.is_valid_identifier(p_holder) OR NOT cloud_agents.is_valid_identifier(p_incarnation)
      OR NOT cloud_agents.is_valid_identifier(p_token) OR p_lease_seconds NOT BETWEEN 1 AND 60
      OR p_subject !~ '^sha256:[0-9a-f]{64}$' OR NOT cloud_agents.is_valid_identifier(p_audit)
    THEN RAISE EXCEPTION USING ERRCODE = '22023', MESSAGE = 'workspace snapshot claim is invalid'; END IF;
    SELECT event.* INTO candidate FROM cloud_agents.outbox_events AS event
    JOIN cloud_agents.workspace_snapshots AS snapshot
      ON snapshot.tenant_id = event.tenant_id AND snapshot.operation_id = event.operation_id
     AND snapshot.operation_generation = event.operation_generation AND snapshot.snapshot_uid = event.aggregate_id
    JOIN cloud_agents.workspaces AS workspace
      ON workspace.tenant_id = snapshot.tenant_id AND workspace.project_uid = snapshot.project_uid AND workspace.workspace_uid = snapshot.source_workspace_uid
    JOIN cloud_agents.workspace_volumes AS volume
      ON volume.tenant_id = snapshot.tenant_id AND volume.project_uid = snapshot.project_uid AND volume.volume_uid = snapshot.source_volume_uid
    JOIN cloud_agents.sandbox_sessions AS source
      ON source.tenant_id = snapshot.tenant_id AND source.project_uid = snapshot.project_uid
     AND source.sandbox_uid = snapshot.source_sandbox_uid AND source.workspace_uid = snapshot.source_workspace_uid
    JOIN cloud_agents.deployment_targets AS target
      ON target.tenant_id = snapshot.tenant_id AND target.project_uid = snapshot.project_uid AND target.target_uid = snapshot.target_uid
    WHERE event.profile_id = 'foundationWorkspaceSnapshot/v1alpha1' AND event.profile_digest = 'sha256:76432dc8e1962f28c4679c70fb4bcedc93171945ff816f625fb4c66aed3c177f'
      AND event.event_class = 'operation_effect' AND event.aggregate_kind = 'workspaceSnapshot'
      AND (event.state = 'pending' OR event.state = 'retry_wait' AND event.next_attempt_at <= claimed_at)
      AND event.delivery_attempts < 8 AND snapshot.status IN ('pending', 'unknown')
      AND workspace.resource_version = snapshot.source_workspace_resource_version
      AND source.generation = snapshot.source_sandbox_generation AND source.desired_state = 'stopped'
      AND source.observed_state = 'stopped' AND source.observed_generation = source.generation AND NOT source.writer_released
      AND volume.observed_state = 'available' AND volume.physical_volume_uid = snapshot.source_physical_volume_uid
      AND target.target_kind IN ('docker', 'kubernetes') AND target.observed_phase = 'ready' AND target.scheduling_state = 'active'
      AND NOT EXISTS (SELECT 1 FROM cloud_agents.sandbox_sessions AS writer
          WHERE writer.tenant_id = snapshot.tenant_id AND writer.project_uid = snapshot.project_uid
            AND writer.workspace_uid = snapshot.source_workspace_uid AND writer.sandbox_uid <> snapshot.source_sandbox_uid
            AND NOT writer.writer_released)
    ORDER BY event.created_at, event.tenant_id, event.event_id FOR UPDATE OF event SKIP LOCKED LIMIT 1;
    IF NOT FOUND THEN RETURN; END IF;
    PERFORM pg_catalog.set_config('cloud_agents.tenant_id', candidate.tenant_id, true);
    UPDATE cloud_agents.outbox_events AS event SET state = 'claimed', delivery_attempts = event.delivery_attempts + 1,
      next_attempt_at = NULL, claim_holder_id = p_holder, claim_incarnation = p_incarnation, claim_token = p_token,
      claim_started_at = claimed_at, claim_expires_at = claimed_at + pg_catalog.make_interval(secs => p_lease_seconds), updated_at = claimed_at
    WHERE event.tenant_id = candidate.tenant_id AND event.event_id = candidate.event_id RETURNING event.* INTO candidate;
    UPDATE cloud_agents.platform_operations AS operation SET state = CASE WHEN candidate.delivery_attempts = 1 THEN 'running' ELSE 'reconciling' END,
      recovery_generation = candidate.delivery_attempts - 1, current_attempt_number = candidate.delivery_attempts, updated_at = claimed_at
    WHERE operation.tenant_id = candidate.tenant_id AND operation.operation_id = candidate.operation_id
      AND operation.operation_generation = candidate.operation_generation AND operation.state IN ('pending', 'running', 'reconciling');
    IF NOT FOUND THEN RAISE EXCEPTION USING ERRCODE = '40001', MESSAGE = 'workspace snapshot operation is not claimable'; END IF;
    INSERT INTO cloud_agents.operation_attempts
      (tenant_id, tenant_ref_id, operation_id, operation_generation, attempt_number, state,
       claim_holder_id, claim_incarnation, claim_token, claim_started_at, claim_expires_at, created_at, updated_at)
    VALUES (candidate.tenant_id, candidate.tenant_id, candidate.operation_id, candidate.operation_generation,
      candidate.delivery_attempts, 'claimed', p_holder, p_incarnation, p_token, claimed_at, candidate.claim_expires_at, claimed_at, claimed_at);
    PERFORM cloud_agents.append_coordination_audit(candidate.tenant_id, p_audit, candidate.profile_id, candidate.profile_digest,
      p_subject, candidate.operation_id, candidate.operation_generation, candidate.delivery_attempts,
      NULL, NULL, NULL, 'workspace.snapshot.claim', 'pending', NULL, NULL, claimed_at);
    RETURN QUERY SELECT candidate.tenant_id, candidate.event_id, candidate.delivery_attempts, candidate.claim_expires_at,
      candidate.operation_id, snapshot.project_uid, snapshot.snapshot_uid, snapshot.source_workspace_uid,
      snapshot.target_uid, target.endpoint, target.credential_ref, snapshot.source_physical_volume_uid, snapshot.image_uri
    FROM cloud_agents.workspace_snapshots AS snapshot
    JOIN cloud_agents.deployment_targets AS target
      ON target.tenant_id = snapshot.tenant_id AND target.project_uid = snapshot.project_uid AND target.target_uid = snapshot.target_uid
    WHERE snapshot.tenant_id = candidate.tenant_id AND snapshot.operation_id = candidate.operation_id;
END;
$$;

CREATE OR REPLACE FUNCTION cloud_agents.claim_foundation_workspace_snapshot_cleanup_v1(
    p_holder text, p_incarnation text, p_token text, p_lease_seconds integer, p_subject text, p_audit text
)
RETURNS TABLE (
    tenant_id text, event_id text, delivery_attempts integer, claim_expires_at timestamptz,
    operation_id text, project_uid text, snapshot_uid text, source_workspace_uid text,
    target_uid text, target_endpoint text, credential_ref text, physical_snapshot_uid text
)
LANGUAGE plpgsql SECURITY DEFINER SET search_path = pg_catalog, cloud_agents
AS $$
DECLARE claimed_at timestamptz := transaction_timestamp(); candidate cloud_agents.outbox_events%ROWTYPE;
BEGIN
    PERFORM cloud_agents.require_runtime_mutation_principal();
    IF NOT cloud_agents.is_valid_identifier(p_holder) OR NOT cloud_agents.is_valid_identifier(p_incarnation)
      OR NOT cloud_agents.is_valid_identifier(p_token) OR p_lease_seconds NOT BETWEEN 1 AND 60
      OR p_subject !~ '^sha256:[0-9a-f]{64}$' OR NOT cloud_agents.is_valid_identifier(p_audit)
    THEN RAISE EXCEPTION USING ERRCODE = '22023', MESSAGE = 'workspace snapshot cleanup claim is invalid'; END IF;
    SELECT event.* INTO candidate FROM cloud_agents.outbox_events AS event
    JOIN cloud_agents.workspace_snapshots AS snapshot
      ON snapshot.tenant_id = event.tenant_id AND snapshot.cleanup_operation_id = event.operation_id
     AND snapshot.snapshot_uid = event.aggregate_id
    JOIN cloud_agents.deployment_targets AS target
      ON target.tenant_id = snapshot.tenant_id AND target.project_uid = snapshot.project_uid
     AND target.target_uid = snapshot.target_uid
    WHERE event.profile_id = 'foundationWorkspaceSnapshotCleanup/v1alpha1' AND event.profile_digest = 'sha256:6157975f3a02c036472fca30bb309fcdee757c2cc99d04a0f053bb7cfd064f69'
      AND event.event_class = 'operation_effect' AND event.aggregate_kind = 'workspaceSnapshot'
      AND (event.state = 'pending' OR event.state = 'retry_wait' AND event.next_attempt_at <= claimed_at)
      AND event.delivery_attempts < 8 AND snapshot.status = 'deleting'
      AND snapshot.physical_snapshot_uid IS NOT NULL AND target.target_kind IN ('docker', 'kubernetes')
    ORDER BY event.created_at, event.tenant_id, event.event_id FOR UPDATE OF event SKIP LOCKED LIMIT 1;
    IF NOT FOUND THEN RETURN; END IF;
    PERFORM pg_catalog.set_config('cloud_agents.tenant_id', candidate.tenant_id, true);
    UPDATE cloud_agents.outbox_events AS event SET state = 'claimed', delivery_attempts = event.delivery_attempts + 1,
      next_attempt_at = NULL, claim_holder_id = p_holder, claim_incarnation = p_incarnation, claim_token = p_token,
      claim_started_at = claimed_at, claim_expires_at = claimed_at + pg_catalog.make_interval(secs => p_lease_seconds), updated_at = claimed_at
    WHERE event.tenant_id = candidate.tenant_id AND event.event_id = candidate.event_id RETURNING event.* INTO candidate;
    UPDATE cloud_agents.platform_operations AS operation
    SET state = CASE WHEN candidate.delivery_attempts = 1 THEN 'running' ELSE 'reconciling' END,
      recovery_generation = candidate.delivery_attempts - 1, current_attempt_number = candidate.delivery_attempts, updated_at = claimed_at
    WHERE operation.tenant_id = candidate.tenant_id AND operation.operation_id = candidate.operation_id
      AND operation.operation_generation = candidate.operation_generation AND operation.state IN ('pending', 'running', 'reconciling');
    IF NOT FOUND THEN RAISE EXCEPTION USING ERRCODE = '40001', MESSAGE = 'workspace snapshot cleanup operation is not claimable'; END IF;
    INSERT INTO cloud_agents.operation_attempts
      (tenant_id, tenant_ref_id, operation_id, operation_generation, attempt_number, state,
       claim_holder_id, claim_incarnation, claim_token, claim_started_at, claim_expires_at, created_at, updated_at)
    VALUES (candidate.tenant_id, candidate.tenant_id, candidate.operation_id, candidate.operation_generation,
      candidate.delivery_attempts, 'claimed', p_holder, p_incarnation, p_token, claimed_at, candidate.claim_expires_at, claimed_at, claimed_at);
    PERFORM cloud_agents.append_coordination_audit(candidate.tenant_id, p_audit, candidate.profile_id, candidate.profile_digest,
      p_subject, candidate.operation_id, candidate.operation_generation, candidate.delivery_attempts,
      NULL, NULL, NULL, 'workspace.snapshot.cleanup.claim', 'pending', NULL, NULL, claimed_at);
    RETURN QUERY SELECT candidate.tenant_id, candidate.event_id, candidate.delivery_attempts, candidate.claim_expires_at,
      candidate.operation_id, snapshot.project_uid, snapshot.snapshot_uid, snapshot.source_workspace_uid,
      snapshot.target_uid, target.endpoint, target.credential_ref, snapshot.physical_snapshot_uid
    FROM cloud_agents.workspace_snapshots AS snapshot
    JOIN cloud_agents.deployment_targets AS target
      ON target.tenant_id = snapshot.tenant_id AND target.project_uid = snapshot.project_uid AND target.target_uid = snapshot.target_uid
    WHERE snapshot.tenant_id = candidate.tenant_id AND snapshot.cleanup_operation_id = candidate.operation_id;
END;
$$;

CREATE OR REPLACE FUNCTION cloud_agents.accept_foundation_workspace_restore_v1(
    p_project text, p_snapshot text, p_expected_snapshot_version bigint,
    p_workspace text, p_workspace_name text, p_sandbox text,
    p_profile text, p_profile_version bigint, p_ttl_seconds integer,
    p_subject text, p_key text, p_digest text
)
RETURNS TABLE (
    operation_uid text, workspace_uid text, sandbox_uid text, profile_uid text,
    profile_version bigint, generation bigint, desired_state text, observed_state text,
    ttl_seconds integer, expires_at timestamptz
)
LANGUAGE plpgsql SECURITY DEFINER SET search_path = pg_catalog, cloud_agents
AS $$
DECLARE
    tenant text;
    selected_snapshot cloud_agents.workspace_snapshots%ROWTYPE;
    selected_target text;
    accepted record;
    prior_digest text;
    prior_operation text;
    restore_audit text;
BEGIN
    PERFORM cloud_agents.require_runtime_mutation_principal();
    tenant := cloud_agents.require_tenant_id();
    IF NOT cloud_agents.is_valid_identifier(p_project) OR NOT cloud_agents.is_valid_identifier(p_snapshot)
      OR p_expected_snapshot_version < 1 OR NOT cloud_agents.is_valid_identifier(p_workspace)
      OR NOT cloud_agents.is_valid_identifier(p_workspace_name) OR NOT cloud_agents.is_valid_identifier(p_sandbox)
      OR NOT cloud_agents.is_valid_identifier(p_profile) OR p_profile_version NOT BETWEEN 1 AND 2147483647
      OR p_ttl_seconds NOT BETWEEN 60 AND 86400 OR p_subject !~ '^sha256:[0-9a-f]{64}$'
      OR p_key !~ '^[A-Za-z0-9._~-]{16,128}$' OR p_digest !~ '^sha256:[0-9a-f]{64}$'
    THEN RAISE EXCEPTION USING ERRCODE = '22023', MESSAGE = 'workspace snapshot restore request is invalid'; END IF;

    SELECT record.request_digest, record.operation_id INTO prior_digest, prior_operation
    FROM cloud_agents.idempotency_records AS record
    WHERE record.tenant_id = tenant AND record.subject_digest = p_subject
      AND record.profile_id = 'foundationSandboxLifecycle/v1alpha1'
      AND record.profile_digest = 'sha256:aabd6aa244c89a4850eba529dbe387132c3a0aff7ffba430f6115afb6e215a1b'
      AND record.idempotency_key = p_key FOR UPDATE;
    IF FOUND THEN
      IF prior_digest IS DISTINCT FROM p_digest THEN
        RAISE EXCEPTION USING ERRCODE = '23505', MESSAGE = 'workspace snapshot restore conflict';
      END IF;
      SELECT * INTO accepted FROM cloud_agents.accept_foundation_sandbox_v3(
        p_project, p_workspace, p_workspace_name, p_sandbox, p_profile, p_profile_version,
        p_subject, p_key, p_digest, p_ttl_seconds
      );
      IF NOT EXISTS (
        SELECT 1 FROM cloud_agents.workspace_snapshot_restores AS restore
        WHERE restore.tenant_id = tenant AND restore.operation_id = prior_operation
          AND restore.project_uid = p_project AND restore.snapshot_uid = p_snapshot
          AND restore.snapshot_resource_version = p_expected_snapshot_version
          AND restore.workspace_uid = p_workspace AND restore.sandbox_uid = p_sandbox
      ) THEN RAISE EXCEPTION USING ERRCODE = '23505', MESSAGE = 'workspace snapshot restore conflict'; END IF;
      RETURN QUERY SELECT accepted.operation_uid, accepted.workspace_uid, accepted.sandbox_uid,
        accepted.profile_uid, accepted.profile_version, accepted.generation, accepted.desired_state,
        accepted.observed_state, accepted.ttl_seconds, accepted.expires_at;
      RETURN;
    END IF;

    SELECT snapshot.* INTO selected_snapshot FROM cloud_agents.workspace_snapshots AS snapshot
    WHERE snapshot.tenant_id = tenant AND snapshot.project_uid = p_project AND snapshot.snapshot_uid = p_snapshot
    FOR SHARE;
    IF NOT FOUND THEN RAISE EXCEPTION USING ERRCODE = '23503', MESSAGE = 'workspace snapshot was not found'; END IF;
    IF selected_snapshot.status <> 'available' OR selected_snapshot.resource_version <> p_expected_snapshot_version
      OR selected_snapshot.physical_snapshot_uid IS NULL OR selected_snapshot.content_digest IS NULL
    THEN RAISE EXCEPTION USING ERRCODE = '23505', MESSAGE = 'workspace snapshot restore conflict'; END IF;

    SELECT profile.target_uid INTO selected_target
    FROM cloud_agents.runtime_profiles AS profile
    JOIN cloud_agents.deployment_targets AS target
      ON target.tenant_id = profile.tenant_id AND target.project_uid = profile.project_uid
     AND target.target_uid = profile.target_uid
    WHERE profile.tenant_id = tenant AND profile.project_uid = p_project
      AND profile.profile_uid = p_profile AND profile.profile_version = p_profile_version
      AND profile.status = 'published'
      AND (profile.target_uid = selected_snapshot.target_uid OR selected_snapshot.backend = 'portable-tar-v1')
      AND target.target_kind IN ('docker', 'kubernetes') AND target.observed_phase = 'ready' AND target.scheduling_state = 'active'
    FOR SHARE OF profile, target;
    IF NOT FOUND THEN RAISE EXCEPTION USING ERRCODE = '23505', MESSAGE = 'workspace snapshot restore target conflict'; END IF;

    PERFORM 1 FROM cloud_agents.sandbox_sessions AS source
    WHERE source.tenant_id = tenant AND source.project_uid = p_project
      AND source.sandbox_uid = selected_snapshot.source_sandbox_uid
      AND source.workspace_uid = selected_snapshot.source_workspace_uid
      AND source.generation = selected_snapshot.source_sandbox_generation
      AND source.desired_state = 'stopped' AND source.observed_state = 'stopped'
      AND source.observed_generation = source.generation AND source.writer_released
      AND NOT EXISTS (
        SELECT 1 FROM cloud_agents.sandbox_sessions AS writer
        WHERE writer.tenant_id = tenant AND writer.project_uid = p_project
          AND writer.workspace_uid = selected_snapshot.source_workspace_uid
          AND NOT writer.writer_released
      )
    FOR UPDATE OF source;
    IF NOT FOUND THEN RAISE EXCEPTION USING ERRCODE = '23505', MESSAGE = 'workspace snapshot source writer is not fenced'; END IF;

    SELECT * INTO accepted FROM cloud_agents.accept_foundation_sandbox_v3(
      p_project, p_workspace, p_workspace_name, p_sandbox, p_profile, p_profile_version,
      p_subject, p_key, p_digest, p_ttl_seconds
    );
    INSERT INTO cloud_agents.workspace_snapshot_restores
      (tenant_id, project_uid, operation_id, snapshot_uid, snapshot_resource_version,
       workspace_uid, sandbox_uid, target_uid, source_workspace_uid,
       source_physical_snapshot_uid, source_content_digest)
    VALUES (tenant, p_project, accepted.operation_uid, p_snapshot, p_expected_snapshot_version,
      p_workspace, p_sandbox, selected_target, selected_snapshot.source_workspace_uid,
      selected_snapshot.physical_snapshot_uid, selected_snapshot.content_digest);
    restore_audit := 'audit-' || pg_catalog.md5(tenant || '|' || accepted.operation_uid || '|restore');
    INSERT INTO cloud_agents.coordination_audit_facts
      (tenant_id, tenant_ref_id, audit_fact_id, registry_digest, profile_id, profile_digest,
       subject_digest, operation_id, operation_generation, transition, outcome)
    SELECT tenant, tenant, restore_audit, operation.registry_digest, operation.profile_id,
      operation.profile_digest, p_subject, operation.operation_id, operation.operation_generation,
      'workspace.snapshot.restore.accept', 'pending'
    FROM cloud_agents.platform_operations AS operation
    WHERE operation.tenant_id = tenant AND operation.operation_id = accepted.operation_uid
      AND operation.operation_generation = 1;
    RETURN QUERY SELECT accepted.operation_uid, accepted.workspace_uid, accepted.sandbox_uid,
      accepted.profile_uid, accepted.profile_version, accepted.generation, accepted.desired_state,
      accepted.observed_state, accepted.ttl_seconds, accepted.expires_at;
END;
$$;
