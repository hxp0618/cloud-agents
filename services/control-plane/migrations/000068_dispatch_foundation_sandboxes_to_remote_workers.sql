CREATE FUNCTION cloud_agents.foundation_target_available_v1(
    p_tenant text, p_project text, p_target text
)
RETURNS boolean
LANGUAGE sql STABLE SET search_path = pg_catalog, cloud_agents
AS $$
    SELECT EXISTS (
        SELECT 1
        FROM cloud_agents.deployment_targets AS target
        LEFT JOIN cloud_agents.remote_worker_enrollments AS enrollment
          ON enrollment.tenant_id = target.tenant_id
         AND enrollment.project_uid = target.project_uid
         AND enrollment.target_uid = target.target_uid
         AND target.target_kind = 'remote-worker'
        WHERE target.tenant_id = p_tenant AND target.project_uid = p_project
          AND target.target_uid = p_target AND target.scheduling_state = 'active'
          AND (
              target.target_kind = 'docker' AND target.observed_phase = 'ready'
              OR target.target_kind = 'remote-worker' AND target.observed_phase = 'ready'
                 AND enrollment.state = 'enrolled' AND enrollment.certificate_state = 'active'
                 AND enrollment.certificate_not_after > clock_timestamp()
                 AND enrollment.node_heartbeat_expires_at > clock_timestamp()
                 AND enrollment.node_desired_state = 'active'
                 AND enrollment.node_observed_state = 'active'
                 AND 'docker' = ANY(enrollment.node_capabilities)
          )
    )
$$;

CREATE OR REPLACE FUNCTION cloud_agents.create_runtime_profile_draft_v1(
    p_tenant text, p_project text, p_profile text, p_name text, p_version bigint,
    p_description text, p_target text, p_image text, p_release text,
    p_cpu bigint, p_memory bigint, p_key text, p_digest text, p_request text, p_subject text
)
RETURNS TABLE (
    profile_version_uid text, profile_uid text, profile_name text, profile_version bigint,
    description text, status text, target_uid text, image_uri text, release_digest text,
    cpu_millis bigint, memory_bytes bigint, resource_version bigint,
    created_at timestamptz, updated_at timestamptz, published_at timestamptz, disabled_at timestamptz
)
LANGUAGE plpgsql SECURITY DEFINER SET search_path = pg_catalog, cloud_agents
AS $$
DECLARE
    ignored text;
    existing cloud_agents.runtime_profiles%ROWTYPE;
    previous cloud_agents.runtime_profiles%ROWTYPE;
    mutation_at timestamptz := transaction_timestamp();
    operation_value text;
BEGIN
    ignored := cloud_agents.require_runtime_mutation_principal();
    IF p_tenant IS DISTINCT FROM cloud_agents.require_tenant_id()
        OR NOT cloud_agents.is_valid_identifier(p_project) OR NOT cloud_agents.is_valid_identifier(p_profile)
        OR NOT cloud_agents.is_valid_identifier(p_name) OR p_version NOT BETWEEN 1 AND 2147483647
        OR pg_catalog.char_length(p_description) NOT BETWEEN 1 AND 1024 OR p_description ~ '[[:cntrl:]]'
        OR NOT cloud_agents.is_valid_identifier(p_target)
        OR p_image !~ '^[A-Za-z0-9._:/-]+@sha256:[0-9a-f]{64}$' OR pg_catalog.length(p_image) > 1024
        OR p_release !~ '^sha256:[0-9a-f]{64}$' OR pg_catalog.right(p_image, 72) IS DISTINCT FROM '@' || p_release
        OR p_cpu NOT BETWEEN 100 AND 64000 OR p_memory NOT BETWEEN 134217728 AND 1099511627776
        OR p_key !~ '^[A-Za-z0-9._~-]{16,128}$' OR p_digest !~ '^sha256:[0-9a-f]{64}$'
        OR NOT cloud_agents.is_valid_identifier(p_request) OR p_subject !~ '^sha256:[0-9a-f]{64}$'
    THEN RAISE EXCEPTION USING ERRCODE = '22023', MESSAGE = 'runtime profile input is invalid'; END IF;

    SELECT profile.* INTO existing FROM cloud_agents.runtime_profiles AS profile
    WHERE profile.tenant_id = p_tenant AND profile.project_uid = p_project
        AND profile.create_idempotency_key = p_key FOR UPDATE;
    IF FOUND THEN
        IF existing.create_request_digest IS DISTINCT FROM p_digest THEN
            RAISE EXCEPTION USING ERRCODE = '23505', MESSAGE = 'runtime profile idempotency conflict';
        END IF;
    ELSE
        PERFORM 1 FROM cloud_agents.projects AS project
        WHERE project.tenant_id = p_tenant AND project.project_uid = p_project AND project.state = 'active' FOR KEY SHARE;
        IF NOT FOUND THEN RAISE EXCEPTION USING ERRCODE = '23503', MESSAGE = 'runtime profile project unavailable'; END IF;
        PERFORM 1 FROM cloud_agents.deployment_targets AS target
        WHERE target.tenant_id = p_tenant AND target.project_uid = p_project
            AND target.target_uid = p_target AND target.target_kind IN ('docker', 'remote-worker') FOR KEY SHARE;
        IF NOT FOUND THEN RAISE EXCEPTION USING ERRCODE = '23503', MESSAGE = 'runtime profile target unavailable'; END IF;
        SELECT profile.* INTO previous FROM cloud_agents.runtime_profiles AS profile
        WHERE profile.tenant_id = p_tenant AND profile.project_uid = p_project AND profile.profile_uid = p_profile
        ORDER BY profile.profile_version DESC LIMIT 1 FOR UPDATE;
        IF FOUND THEN
            IF previous.profile_name IS DISTINCT FROM p_name OR p_version <> previous.profile_version + 1 THEN
                RAISE EXCEPTION USING ERRCODE = '23505', MESSAGE = 'runtime profile version conflict';
            END IF;
        ELSIF p_version <> 1 THEN
            RAISE EXCEPTION USING ERRCODE = '23505', MESSAGE = 'runtime profile version conflict';
        END IF;
        INSERT INTO cloud_agents.runtime_profiles (
            tenant_id, tenant_ref_id, project_uid, profile_version_uid, profile_uid, profile_name,
            profile_version, description, status, target_uid, image_uri, release_digest,
            cpu_millis, memory_bytes, resource_version, create_idempotency_key,
            create_request_digest, created_at, updated_at
        ) VALUES (
            p_tenant, p_tenant, p_project,
            'rp-' || pg_catalog.md5(p_tenant || '|' || p_project || '|' || p_profile || '|' || p_version::text),
            p_profile, p_name, p_version, p_description, 'draft', p_target, p_image, p_release,
            p_cpu, p_memory, 1, p_key, p_digest, mutation_at, mutation_at
        ) RETURNING runtime_profiles.* INTO existing;
        operation_value := 'op-' || pg_catalog.md5(p_tenant || '|' || p_project || '|' || existing.profile_version_uid || '|create|' || p_key);
        INSERT INTO cloud_agents.runtime_profile_activity (
            tenant_id, project_uid, profile_version_uid, event_uid, operation_uid, action,
            idempotency_key, request_id, request_digest, subject_digest, profile_version, result, occurred_at
        ) VALUES (
            p_tenant, p_project, existing.profile_version_uid, operation_value || '-done', operation_value,
            'runtime-profile.create', p_key, p_request, p_digest, p_subject, p_version, 'succeeded', mutation_at
        );
    END IF;
    RETURN QUERY SELECT existing.profile_version_uid, existing.profile_uid, existing.profile_name,
        existing.profile_version, existing.description, existing.status, existing.target_uid,
        existing.image_uri, existing.release_digest, existing.cpu_millis, existing.memory_bytes,
        existing.resource_version, existing.created_at, existing.updated_at,
        existing.published_at, existing.disabled_at;
END;
$$;

CREATE OR REPLACE FUNCTION cloud_agents.transition_runtime_profile_v1(
    p_tenant text, p_project text, p_profile text, p_version bigint,
    p_expected_resource_version bigint, p_action text, p_key text,
    p_digest text, p_request text, p_subject text
)
RETURNS TABLE (
    profile_version_uid text, profile_uid text, profile_name text, profile_version bigint,
    description text, status text, target_uid text, image_uri text, release_digest text,
    cpu_millis bigint, memory_bytes bigint, resource_version bigint,
    created_at timestamptz, updated_at timestamptz, published_at timestamptz, disabled_at timestamptz
)
LANGUAGE plpgsql SECURITY DEFINER SET search_path = pg_catalog, cloud_agents
AS $$
DECLARE
    ignored text;
    existing cloud_agents.runtime_profiles%ROWTYPE;
    mutation_at timestamptz := transaction_timestamp();
    operation_value text;
BEGIN
    ignored := cloud_agents.require_runtime_mutation_principal();
    IF p_tenant IS DISTINCT FROM cloud_agents.require_tenant_id()
        OR NOT cloud_agents.is_valid_identifier(p_project) OR NOT cloud_agents.is_valid_identifier(p_profile)
        OR p_version NOT BETWEEN 1 AND 2147483647 OR p_expected_resource_version < 1
        OR p_action NOT IN ('publish', 'disable') OR p_key !~ '^[A-Za-z0-9._~-]{16,128}$'
        OR p_digest !~ '^sha256:[0-9a-f]{64}$' OR NOT cloud_agents.is_valid_identifier(p_request)
        OR p_subject !~ '^sha256:[0-9a-f]{64}$'
    THEN RAISE EXCEPTION USING ERRCODE = '22023', MESSAGE = 'runtime profile transition input is invalid'; END IF;
    SELECT profile.* INTO existing FROM cloud_agents.runtime_profiles AS profile
    WHERE profile.tenant_id = p_tenant AND profile.project_uid = p_project
        AND profile.profile_uid = p_profile AND profile.profile_version = p_version FOR UPDATE;
    IF NOT FOUND THEN RAISE EXCEPTION USING ERRCODE = '23503', MESSAGE = 'runtime profile was not found'; END IF;

    IF p_action = 'publish' AND existing.publish_idempotency_key IS NOT NULL THEN
        IF existing.publish_idempotency_key IS DISTINCT FROM p_key OR existing.publish_request_digest IS DISTINCT FROM p_digest THEN
            RAISE EXCEPTION USING ERRCODE = '23505', MESSAGE = 'runtime profile idempotency conflict';
        END IF;
    ELSIF p_action = 'disable' AND existing.disable_idempotency_key IS NOT NULL THEN
        IF existing.disable_idempotency_key IS DISTINCT FROM p_key OR existing.disable_request_digest IS DISTINCT FROM p_digest THEN
            RAISE EXCEPTION USING ERRCODE = '23505', MESSAGE = 'runtime profile idempotency conflict';
        END IF;
    ELSE
        IF existing.resource_version <> p_expected_resource_version
            OR p_action = 'publish' AND existing.status <> 'draft'
            OR p_action = 'disable' AND existing.status <> 'published'
        THEN RAISE EXCEPTION USING ERRCODE = '23505', MESSAGE = 'runtime profile transition conflict'; END IF;
        IF p_action = 'publish' THEN
            PERFORM 1 FROM cloud_agents.deployment_targets AS target
            WHERE target.tenant_id = p_tenant AND target.project_uid = p_project
                AND target.target_uid = existing.target_uid
                AND cloud_agents.foundation_target_available_v1(p_tenant, p_project, target.target_uid) FOR KEY SHARE;
            IF NOT FOUND THEN RAISE EXCEPTION USING ERRCODE = '23503', MESSAGE = 'runtime profile target unavailable'; END IF;
            UPDATE cloud_agents.runtime_profiles AS profile SET status = 'published',
                resource_version = profile.resource_version + 1, updated_at = mutation_at, published_at = mutation_at,
                publish_idempotency_key = p_key, publish_request_digest = p_digest
            WHERE profile.tenant_id = p_tenant AND profile.project_uid = p_project
                AND profile.profile_uid = p_profile AND profile.profile_version = p_version
            RETURNING profile.* INTO existing;
        ELSE
            UPDATE cloud_agents.runtime_profiles AS profile SET status = 'disabled',
                resource_version = profile.resource_version + 1, updated_at = mutation_at, disabled_at = mutation_at,
                disable_idempotency_key = p_key, disable_request_digest = p_digest
            WHERE profile.tenant_id = p_tenant AND profile.project_uid = p_project
                AND profile.profile_uid = p_profile AND profile.profile_version = p_version
            RETURNING profile.* INTO existing;
        END IF;
        operation_value := 'op-' || pg_catalog.md5(p_tenant || '|' || p_project || '|' || existing.profile_version_uid || '|' || p_action || '|' || p_key);
        INSERT INTO cloud_agents.runtime_profile_activity (
            tenant_id, project_uid, profile_version_uid, event_uid, operation_uid, action,
            idempotency_key, request_id, request_digest, subject_digest, profile_version, result, occurred_at
        ) VALUES (
            p_tenant, p_project, existing.profile_version_uid, operation_value || '-done', operation_value,
            'runtime-profile.' || p_action, p_key, p_request, p_digest, p_subject, p_version, 'succeeded', mutation_at
        );
    END IF;
    RETURN QUERY SELECT existing.profile_version_uid, existing.profile_uid, existing.profile_name,
        existing.profile_version, existing.description, existing.status, existing.target_uid,
        existing.image_uri, existing.release_digest, existing.cpu_millis, existing.memory_bytes,
        existing.resource_version, existing.created_at, existing.updated_at,
        existing.published_at, existing.disabled_at;
END;
$$;

CREATE OR REPLACE FUNCTION cloud_agents.accept_foundation_intent_v1(
    p_project text, p_workspace text, p_name text, p_volume text, p_target text, p_sandbox text,
    p_image text, p_cpu bigint, p_memory bigint, p_operation text, p_event text,
    p_subject text, p_key text, p_digest text)
RETURNS text LANGUAGE plpgsql SECURITY DEFINER SET search_path = pg_catalog, cloud_agents
AS $$
DECLARE
    t text;
    prior cloud_agents.idempotency_records%ROWTYPE;
BEGIN
    PERFORM cloud_agents.require_runtime_mutation_principal();
    t := cloud_agents.require_tenant_id();
    IF p_project IS NULL OR p_workspace IS NULL OR p_name IS NULL OR p_volume IS NULL OR p_target IS NULL
        OR p_sandbox IS NULL OR p_image IS NULL OR p_cpu IS NULL OR p_memory IS NULL
        OR p_operation IS NULL OR p_event IS NULL OR p_subject IS NULL OR p_key IS NULL OR p_digest IS NULL
        OR NOT cloud_agents.is_valid_identifier(p_project) OR NOT cloud_agents.is_valid_identifier(p_operation)
        OR NOT cloud_agents.is_valid_identifier(p_event)
    THEN RAISE EXCEPTION USING ERRCODE = '22023', MESSAGE = 'invalid foundation intent'; END IF;
    INSERT INTO cloud_agents.idempotency_records
        (tenant_id, tenant_ref_id, subject_digest, registry_digest, profile_id, profile_digest,
         idempotency_key, request_digest, state, expires_at)
    VALUES (t, t, p_subject, 'sha256:9ebf663f49a5d1f5209d5c94656419d21fc2022105355d51a5b2fc3ed318378c', 'foundationSandboxLifecycle/v1alpha1', 'sha256:aabd6aa244c89a4850eba529dbe387132c3a0aff7ffba430f6115afb6e215a1b',
        p_key, p_digest, 'pending', transaction_timestamp() + interval '1 day')
    ON CONFLICT DO NOTHING;
    SELECT * INTO STRICT prior FROM cloud_agents.idempotency_records
    WHERE tenant_id = t AND subject_digest = p_subject AND profile_id = 'foundationSandboxLifecycle/v1alpha1'
        AND profile_digest = 'sha256:aabd6aa244c89a4850eba529dbe387132c3a0aff7ffba430f6115afb6e215a1b' AND idempotency_key = p_key FOR UPDATE;
    IF prior.request_digest <> p_digest THEN
        RAISE EXCEPTION USING ERRCODE = '22023', MESSAGE = 'foundation idempotency conflict';
    END IF;
    IF prior.operation_id IS NOT NULL THEN
        IF NOT EXISTS (
            SELECT 1 FROM cloud_agents.sandbox_sessions s
            JOIN cloud_agents.workspaces w USING (tenant_id, project_uid, workspace_uid)
            JOIN cloud_agents.workspace_volumes v USING (tenant_id, project_uid, workspace_uid)
            WHERE s.tenant_id = t AND s.project_uid = p_project AND s.workspace_uid = p_workspace
                AND s.sandbox_uid = p_sandbox AND w.workspace_name = p_name AND v.volume_uid = p_volume
                AND v.target_uid = p_target AND s.image_uri = p_image AND s.cpu_millis = p_cpu
                AND s.memory_bytes = p_memory AND s.operation_id = prior.operation_id
        ) THEN RAISE EXCEPTION USING ERRCODE = '22023', MESSAGE = 'foundation idempotency conflict'; END IF;
        RETURN prior.operation_id;
    END IF;
    PERFORM 1 FROM cloud_agents.projects p JOIN cloud_agents.platform_tenants tenant ON tenant.tenant_id = p.tenant_id
    WHERE p.tenant_id = t AND p.project_uid = p_project AND p.state = 'active' AND tenant.state = 'active'
    FOR SHARE OF p, tenant;
    IF NOT FOUND THEN RAISE EXCEPTION USING ERRCODE = '22023', MESSAGE = 'foundation project unavailable'; END IF;
    PERFORM 1 FROM cloud_agents.deployment_targets
    WHERE tenant_id = t AND project_uid = p_project AND target_uid = p_target
        AND cloud_agents.foundation_target_available_v1(t, p_project, target_uid) FOR SHARE;
    IF NOT FOUND THEN RAISE EXCEPTION USING ERRCODE = '22023', MESSAGE = 'foundation target unavailable'; END IF;
    INSERT INTO cloud_agents.platform_operations
        (tenant_id, tenant_ref_id, operation_id, operation_generation, registry_digest, state_machine_digest,
         policy_digest, profile_id, profile_digest, subject_digest, request_digest, state, cleanup_phase,
         recovery_generation, current_attempt_number)
    VALUES (t, t, p_operation, 1, 'sha256:9ebf663f49a5d1f5209d5c94656419d21fc2022105355d51a5b2fc3ed318378c', cloud_agents.coordination_state_machine_digest(),
        cloud_agents.coordination_policy_digest(), 'foundationSandboxLifecycle/v1alpha1', 'sha256:aabd6aa244c89a4850eba529dbe387132c3a0aff7ffba430f6115afb6e215a1b', p_subject, p_digest,
        'pending', 'none', 0, 0);
    INSERT INTO cloud_agents.workspaces (tenant_id, project_uid, workspace_uid, workspace_name)
    VALUES (t, p_project, p_workspace, p_name);
    INSERT INTO cloud_agents.workspace_volumes (tenant_id, project_uid, workspace_uid, volume_uid, target_uid)
    VALUES (t, p_project, p_workspace, p_volume, p_target);
    INSERT INTO cloud_agents.sandbox_sessions
        (tenant_id, project_uid, sandbox_uid, workspace_uid, generation, desired_state,
         image_uri, cpu_millis, memory_bytes, operation_id, operation_generation, spec_digest)
    VALUES (t, p_project, p_sandbox, p_workspace, 1, 'running', p_image, p_cpu, p_memory, p_operation, 1, p_digest);
    INSERT INTO cloud_agents.operation_finalizers
        (tenant_id, tenant_ref_id, operation_id, operation_generation, finalizer_name, required, state, delivery_attempts)
    VALUES (t, t, p_operation, 1, 'sandbox-compute', true, 'pending', 0);
    INSERT INTO cloud_agents.outbox_events
        (tenant_id, tenant_ref_id, event_id, registry_digest, profile_id, profile_digest, event_class,
         aggregate_kind, aggregate_id, aggregate_sequence, generation, operation_id, operation_generation,
         payload_digest, state, delivery_attempts)
    VALUES (t, t, p_event, 'sha256:9ebf663f49a5d1f5209d5c94656419d21fc2022105355d51a5b2fc3ed318378c', 'foundationSandboxLifecycle/v1alpha1', 'sha256:aabd6aa244c89a4850eba529dbe387132c3a0aff7ffba430f6115afb6e215a1b', 'operation_effect',
        'sandboxSession', p_sandbox, 1, 1, p_operation, 1, p_digest, 'pending', 0);
    INSERT INTO cloud_agents.coordination_audit_facts
        (tenant_id, tenant_ref_id, audit_fact_id, registry_digest, profile_id, profile_digest,
         subject_digest, operation_id, operation_generation, transition, outcome)
    VALUES (t, t, p_event, 'sha256:9ebf663f49a5d1f5209d5c94656419d21fc2022105355d51a5b2fc3ed318378c', 'foundationSandboxLifecycle/v1alpha1', 'sha256:aabd6aa244c89a4850eba529dbe387132c3a0aff7ffba430f6115afb6e215a1b',
        p_subject, p_operation, 1, 'sandbox.accept', 'pending');
    UPDATE cloud_agents.idempotency_records SET operation_id = p_operation, operation_generation = 1
    WHERE tenant_id = t AND subject_digest = p_subject AND profile_id = 'foundationSandboxLifecycle/v1alpha1'
        AND profile_digest = 'sha256:aabd6aa244c89a4850eba529dbe387132c3a0aff7ffba430f6115afb6e215a1b' AND idempotency_key = p_key;
    RETURN p_operation;
END;
$$;

CREATE FUNCTION cloud_agents.claim_foundation_sandbox_v5(
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
              AND cloud_agents.foundation_target_available_v1(target.tenant_id, target.project_uid, target.target_uid))
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

CREATE FUNCTION cloud_agents.settle_remote_worker_foundation_sandbox_v1(
    p_tenant text, p_project text, p_target text, p_incarnation text,
    p_command text, p_attempt bigint, p_operation text, p_sandbox text, p_sandbox_generation bigint,
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
        OR p_attempt NOT BETWEEN 1 AND 8 OR NOT cloud_agents.is_valid_identifier(p_operation)
        OR NOT cloud_agents.is_valid_identifier(p_sandbox) OR p_sandbox_generation < 1
        OR p_transition NOT IN ('succeeded', 'retry', 'failed') OR p_cleanup_complete IS NULL
        OR p_transition = 'succeeded' AND (NOT cloud_agents.is_valid_identifier(p_runtime)
            OR p_runtime_state <> 'Running' OR NOT cloud_agents.is_valid_identifier(p_volume) OR p_error IS NOT NULL)
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
    JOIN cloud_agents.remote_worker_enrollments AS enrollment
      ON enrollment.tenant_id = volume.tenant_id AND enrollment.project_uid = volume.project_uid
     AND enrollment.target_uid = volume.target_uid
    WHERE stored.tenant_id = p_tenant AND sandbox.project_uid = p_project
      AND volume.target_uid = p_target AND enrollment.incarnation_uid = p_incarnation
      AND stored.operation_id = p_operation AND stored.delivery_attempts = p_attempt
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
    JOIN cloud_agents.remote_worker_enrollments AS enrollment
      ON enrollment.tenant_id = volume.tenant_id AND enrollment.project_uid = volume.project_uid
     AND enrollment.target_uid = volume.target_uid
    WHERE attempt.tenant_id = p_tenant AND sandbox.project_uid = p_project
      AND volume.target_uid = p_target AND enrollment.incarnation_uid = p_incarnation
      AND attempt.operation_id = p_operation AND attempt.attempt_number = p_attempt
      AND sandbox.sandbox_uid = p_sandbox AND sandbox.generation = p_sandbox_generation
      AND (p_transition = 'succeeded' AND attempt.state = 'succeeded'
           AND sandbox.observed_state = 'running' AND sandbox.runtime_uid = p_runtime
           AND sandbox.runtime_state = p_runtime_state AND volume.physical_volume_uid = p_volume
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

ALTER FUNCTION cloud_agents.foundation_target_available_v1(text,text,text) OWNER TO cloud_agents_migration_owner;
ALTER FUNCTION cloud_agents.claim_foundation_sandbox_v5(text,text,text,text,text,integer,text,text) OWNER TO cloud_agents_migration_owner;
ALTER FUNCTION cloud_agents.settle_remote_worker_foundation_sandbox_v1(text,text,text,text,text,bigint,text,text,bigint,text,text,text,text,text,boolean,text,text) OWNER TO cloud_agents_migration_owner;
REVOKE ALL ON FUNCTION cloud_agents.foundation_target_available_v1(text,text,text) FROM PUBLIC;
REVOKE ALL ON FUNCTION cloud_agents.claim_foundation_sandbox_v5(text,text,text,text,text,integer,text,text) FROM PUBLIC;
REVOKE ALL ON FUNCTION cloud_agents.settle_remote_worker_foundation_sandbox_v1(text,text,text,text,text,bigint,text,text,bigint,text,text,text,text,text,boolean,text,text) FROM PUBLIC;
REVOKE EXECUTE ON FUNCTION cloud_agents.claim_foundation_sandbox_v4(text,text,text,integer,text,text) FROM cloud_agents_runtime;
GRANT EXECUTE ON FUNCTION cloud_agents.foundation_target_available_v1(text,text,text) TO cloud_agents_runtime;
GRANT EXECUTE ON FUNCTION cloud_agents.claim_foundation_sandbox_v5(text,text,text,text,text,integer,text,text) TO cloud_agents_runtime;
GRANT EXECUTE ON FUNCTION cloud_agents.settle_remote_worker_foundation_sandbox_v1(text,text,text,text,text,bigint,text,text,bigint,text,text,text,text,text,boolean,text,text) TO cloud_agents_runtime;
