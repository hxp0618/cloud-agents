ALTER TABLE cloud_agents.sandbox_sessions DROP CONSTRAINT sandbox_sessions_observation;
ALTER TABLE cloud_agents.sandbox_sessions ADD COLUMN runtime_operation_uid text;
ALTER TABLE cloud_agents.sandbox_sessions ADD COLUMN runtime_generation bigint;
ALTER TABLE cloud_agents.sandbox_sessions ADD COLUMN runtime_spec_digest text;
UPDATE cloud_agents.sandbox_sessions SET
    runtime_operation_uid = operation_id,
    runtime_generation = generation,
    runtime_spec_digest = spec_digest
WHERE runtime_uid IS NOT NULL;
ALTER TABLE cloud_agents.sandbox_sessions ADD CONSTRAINT sandbox_sessions_runtime_receipt CHECK (
    runtime_uid IS NULL AND runtime_state = '' AND runtime_operation_uid IS NULL
        AND runtime_generation IS NULL AND runtime_spec_digest IS NULL
    OR runtime_uid IS NOT NULL AND runtime_state <> ''
        AND cloud_agents.is_valid_identifier(runtime_operation_uid)
        AND runtime_generation > 0 AND runtime_generation <= generation
        AND runtime_spec_digest ~ '^sha256:[0-9a-f]{64}$'
);
ALTER TABLE cloud_agents.sandbox_sessions ADD CONSTRAINT sandbox_sessions_observation CHECK (
        observed_state = 'pending' AND generation = 1 AND observed_generation = 0 AND resource_version = 0
            AND runtime_uid IS NULL AND stable_error_code IS NULL AND observed_at IS NULL
        OR observed_state = 'running' AND observed_generation BETWEEN generation - 1 AND generation
            AND resource_version > 0 AND runtime_uid IS NOT NULL AND runtime_state = 'Running'
            AND stable_error_code IS NULL AND observed_at IS NOT NULL
        OR observed_state IN ('unknown', 'failed') AND observed_generation = generation
            AND stable_error_code IS NOT NULL AND observed_at IS NOT NULL
        OR observed_state = 'stopped' AND observed_generation BETWEEN generation - 1 AND generation
            AND resource_version > 0 AND runtime_uid IS NULL AND stable_error_code IS NULL AND observed_at IS NOT NULL
);

ALTER TABLE cloud_agents.admin_denied_writes DROP CONSTRAINT admin_denied_writes_action_check;
ALTER TABLE cloud_agents.admin_denied_writes ADD CONSTRAINT admin_denied_writes_action_check CHECK (action IN (
    'adminUpgradeEnvironmentLease', 'adminRollbackEnvironmentLease', 'adminRegisterWorkerRelease',
    'adminSetStoragePolicy', 'adminSetNetworkPolicy', 'adminSetProjectLeaseQuota',
    'adminCreateEnvironmentProfile', 'adminPublishEnvironmentProfile', 'adminDisableEnvironmentProfile',
    'adminCreateRuntimeProfile', 'adminPublishRuntimeProfile', 'adminDisableRuntimeProfile',
    'adminRegisterDeploymentTarget', 'adminProbeDeploymentTarget',
    'adminTransitionDeploymentTargetScheduling', 'adminCleanupDeploymentTarget',
    'adminStopSandboxSession', 'adminRebuildSandboxSession'
));

CREATE TABLE cloud_agents.foundation_sandbox_activity (
    tenant_id text NOT NULL,
    project_uid text NOT NULL,
    sandbox_uid text NOT NULL,
    operation_uid text NOT NULL,
    action text NOT NULL,
    idempotency_key text NOT NULL,
    request_id text NOT NULL,
    request_digest text NOT NULL,
    subject_digest text NOT NULL,
    sandbox_generation bigint NOT NULL,
    compute_disposition text NOT NULL,
    workspace_disposition text NOT NULL,
    requested_at timestamptz NOT NULL,
    PRIMARY KEY (tenant_id, operation_uid),
    UNIQUE (tenant_id, project_uid, sandbox_uid, sandbox_generation),
    FOREIGN KEY (tenant_id, project_uid, sandbox_uid) REFERENCES cloud_agents.sandbox_sessions
        (tenant_id, project_uid, sandbox_uid) ON UPDATE RESTRICT ON DELETE RESTRICT,
    FOREIGN KEY (tenant_id, operation_uid, sandbox_generation) REFERENCES cloud_agents.platform_operations
        (tenant_id, operation_id, operation_generation) ON UPDATE RESTRICT ON DELETE RESTRICT,
    CHECK (cloud_agents.is_valid_identifier(operation_uid)),
    CHECK (action IN ('sandbox.stop', 'sandbox.rebuild')),
    CHECK (idempotency_key ~ '^[A-Za-z0-9._~-]{16,128}$'),
    CHECK (cloud_agents.is_valid_identifier(request_id)),
    CHECK (request_digest ~ '^sha256:[0-9a-f]{64}$'),
    CHECK (subject_digest ~ '^sha256:[0-9a-f]{64}$'),
    CHECK (sandbox_generation > 0),
    CHECK (compute_disposition IN ('delete', 'create')),
    CHECK (workspace_disposition = 'retain'),
    CHECK (action = 'sandbox.stop' AND compute_disposition = 'delete'
        OR action = 'sandbox.rebuild' AND compute_disposition = 'create')
);
CREATE INDEX foundation_sandbox_activity_page_idx ON cloud_agents.foundation_sandbox_activity
    (tenant_id, project_uid, sandbox_uid, requested_at DESC);
ALTER TABLE cloud_agents.foundation_sandbox_activity OWNER TO cloud_agents_migration_owner;
ALTER TABLE cloud_agents.foundation_sandbox_activity ENABLE ROW LEVEL SECURITY;
ALTER TABLE cloud_agents.foundation_sandbox_activity FORCE ROW LEVEL SECURITY;
CREATE POLICY foundation_sandbox_activity_runtime ON cloud_agents.foundation_sandbox_activity TO cloud_agents_runtime
    USING (tenant_id = cloud_agents.require_tenant_id()) WITH CHECK (tenant_id = cloud_agents.require_tenant_id());
CREATE POLICY foundation_sandbox_activity_owner ON cloud_agents.foundation_sandbox_activity TO cloud_agents_migration_owner
    USING (true) WITH CHECK (true);
REVOKE ALL ON TABLE cloud_agents.foundation_sandbox_activity FROM PUBLIC;
GRANT SELECT ON TABLE cloud_agents.foundation_sandbox_activity TO cloud_agents_runtime;

CREATE FUNCTION cloud_agents.transition_foundation_sandbox_v1(
    p_project text, p_sandbox text, p_expected_generation bigint, p_expected_resource_version bigint,
    p_action text, p_confirmed_sandbox text, p_compute_disposition text, p_workspace_disposition text,
    p_subject text, p_key text, p_digest text, p_request text
)
RETURNS TABLE (
    operation_uid text, idempotency_key text, action text, sandbox_uid text,
    sandbox_generation bigint, requested_by text, request_id text, requested_at timestamptz,
    updated_at timestamptz, operation_state text, cleanup_phase text, stable_error_code text,
    compute_disposition text, workspace_disposition text
)
LANGUAGE plpgsql SECURITY DEFINER SET search_path = pg_catalog, cloud_agents
AS $$
DECLARE
    ignored text;
    sandbox cloud_agents.sandbox_sessions%ROWTYPE;
    volume cloud_agents.workspace_volumes%ROWTYPE;
    prior cloud_agents.idempotency_records%ROWTYPE;
    mutation_at timestamptz := transaction_timestamp();
    operation_value text;
    event_value text;
    next_generation bigint;
BEGIN
    ignored := cloud_agents.require_runtime_mutation_principal();
    IF NOT cloud_agents.is_valid_identifier(p_project) OR NOT cloud_agents.is_valid_identifier(p_sandbox)
        OR p_expected_generation < 1 OR p_expected_resource_version < 1
        OR p_action NOT IN ('stop', 'rebuild') OR p_confirmed_sandbox IS DISTINCT FROM p_sandbox
        OR p_compute_disposition IS DISTINCT FROM (CASE WHEN p_action = 'stop' THEN 'delete' ELSE 'create' END)
        OR p_workspace_disposition IS DISTINCT FROM 'retain'
        OR p_subject !~ '^sha256:[0-9a-f]{64}$' OR p_key !~ '^[A-Za-z0-9._~-]{16,128}$'
        OR p_digest !~ '^sha256:[0-9a-f]{64}$' OR NOT cloud_agents.is_valid_identifier(p_request)
    THEN RAISE EXCEPTION USING ERRCODE = '22023', MESSAGE = 'foundation sandbox transition input is invalid'; END IF;

    INSERT INTO cloud_agents.idempotency_records (
        tenant_id, tenant_ref_id, subject_digest, registry_digest, profile_id, profile_digest,
        idempotency_key, request_digest, state, expires_at
    ) VALUES (
        cloud_agents.require_tenant_id(), cloud_agents.require_tenant_id(), p_subject,
        'sha256:9ebf663f49a5d1f5209d5c94656419d21fc2022105355d51a5b2fc3ed318378c',
        'foundationSandboxLifecycle/v1alpha1', 'sha256:aabd6aa244c89a4850eba529dbe387132c3a0aff7ffba430f6115afb6e215a1b',
        p_key, p_digest, 'pending', mutation_at + interval '1 day'
    ) ON CONFLICT DO NOTHING;
    SELECT record.* INTO STRICT prior FROM cloud_agents.idempotency_records AS record
    WHERE record.tenant_id = cloud_agents.require_tenant_id() AND record.subject_digest = p_subject
        AND record.profile_id = 'foundationSandboxLifecycle/v1alpha1'
        AND record.profile_digest = 'sha256:aabd6aa244c89a4850eba529dbe387132c3a0aff7ffba430f6115afb6e215a1b'
        AND record.idempotency_key = p_key FOR UPDATE;
    IF prior.request_digest IS DISTINCT FROM p_digest THEN
        RAISE EXCEPTION USING ERRCODE = '23505', MESSAGE = 'foundation sandbox idempotency conflict';
    END IF;

    operation_value := prior.operation_id;
    IF operation_value IS NULL THEN
        SELECT stored.* INTO sandbox FROM cloud_agents.sandbox_sessions AS stored
        WHERE stored.tenant_id = cloud_agents.require_tenant_id() AND stored.project_uid = p_project
            AND stored.sandbox_uid = p_sandbox FOR UPDATE;
        IF NOT FOUND THEN RAISE EXCEPTION USING ERRCODE = '23503', MESSAGE = 'foundation sandbox was not found'; END IF;
        SELECT stored.* INTO STRICT volume FROM cloud_agents.workspace_volumes AS stored
        WHERE stored.tenant_id = sandbox.tenant_id AND stored.project_uid = sandbox.project_uid
            AND stored.workspace_uid = sandbox.workspace_uid FOR KEY SHARE;
        IF sandbox.generation IS DISTINCT FROM p_expected_generation
            OR sandbox.resource_version IS DISTINCT FROM p_expected_resource_version
            OR volume.retention <> 'retain' OR volume.observed_state <> 'available'
            OR volume.physical_volume_uid IS NULL
        THEN RAISE EXCEPTION USING ERRCODE = '23505', MESSAGE = 'foundation sandbox transition conflict'; END IF;
        IF p_action = 'stop' THEN
            IF sandbox.desired_state <> 'running' OR sandbox.observed_state <> 'running'
                OR sandbox.observed_generation <> sandbox.generation OR sandbox.writer_released
                OR sandbox.runtime_uid IS NULL OR sandbox.runtime_operation_uid IS NULL
                OR sandbox.runtime_generation IS NULL OR sandbox.runtime_spec_digest IS NULL
            THEN RAISE EXCEPTION USING ERRCODE = '23505', MESSAGE = 'foundation sandbox transition conflict'; END IF;
        ELSE
            IF sandbox.desired_state <> 'stopped' OR sandbox.observed_state <> 'stopped'
                OR sandbox.observed_generation <> sandbox.generation OR NOT sandbox.writer_released
                OR sandbox.runtime_uid IS NOT NULL
            THEN RAISE EXCEPTION USING ERRCODE = '23505', MESSAGE = 'foundation sandbox transition conflict'; END IF;
            PERFORM 1 FROM cloud_agents.runtime_profiles AS profile
            JOIN cloud_agents.deployment_targets AS target
              ON target.tenant_id = profile.tenant_id AND target.project_uid = profile.project_uid
             AND target.target_uid = profile.target_uid
            WHERE profile.tenant_id = sandbox.tenant_id AND profile.project_uid = sandbox.project_uid
                AND profile.profile_uid = sandbox.runtime_profile_uid
                AND profile.profile_version = sandbox.runtime_profile_version
                AND profile.status IN ('published', 'disabled') AND target.target_kind = 'docker'
                AND target.observed_phase = 'ready' AND target.scheduling_state = 'active' FOR KEY SHARE OF profile, target;
            IF NOT FOUND THEN RAISE EXCEPTION USING ERRCODE = '23503', MESSAGE = 'runtime profile target unavailable'; END IF;
        END IF;

        next_generation := sandbox.generation + 1;
        operation_value := 'op-' || pg_catalog.md5(sandbox.tenant_id || '|' || p_project || '|' || p_sandbox || '|' || p_key);
        event_value := 'evt-' || pg_catalog.md5(sandbox.tenant_id || '|' || p_project || '|' || p_sandbox || '|' || p_key);
        INSERT INTO cloud_agents.platform_operations (
            tenant_id, tenant_ref_id, operation_id, operation_generation, registry_digest,
            state_machine_digest, policy_digest, profile_id, profile_digest, subject_digest,
            request_digest, state, cleanup_phase, recovery_generation, current_attempt_number
        ) VALUES (
            sandbox.tenant_id, sandbox.tenant_id, operation_value, next_generation,
            'sha256:9ebf663f49a5d1f5209d5c94656419d21fc2022105355d51a5b2fc3ed318378c',
            cloud_agents.coordination_state_machine_digest(), cloud_agents.coordination_policy_digest(),
            'foundationSandboxLifecycle/v1alpha1', 'sha256:aabd6aa244c89a4850eba529dbe387132c3a0aff7ffba430f6115afb6e215a1b',
            p_subject, p_digest, 'pending', 'none', 0, 0
        );
        INSERT INTO cloud_agents.foundation_sandbox_activity (
            tenant_id, project_uid, sandbox_uid, operation_uid, action, idempotency_key,
            request_id, request_digest, subject_digest, sandbox_generation,
            compute_disposition, workspace_disposition, requested_at
        ) VALUES (
            sandbox.tenant_id, p_project, p_sandbox, operation_value, 'sandbox.' || p_action,
            p_key, p_request, p_digest, p_subject, next_generation,
            p_compute_disposition, p_workspace_disposition, mutation_at
        );
        INSERT INTO cloud_agents.operation_finalizers (
            tenant_id, tenant_ref_id, operation_id, operation_generation, finalizer_name,
            required, state, delivery_attempts
        ) VALUES (sandbox.tenant_id, sandbox.tenant_id, operation_value, next_generation,
            'sandbox-compute', true, 'pending', 0);
        INSERT INTO cloud_agents.outbox_events (
            tenant_id, tenant_ref_id, event_id, registry_digest, profile_id, profile_digest,
            event_class, aggregate_kind, aggregate_id, aggregate_sequence, generation,
            operation_id, operation_generation, payload_digest, state, delivery_attempts
        ) VALUES (
            sandbox.tenant_id, sandbox.tenant_id, event_value,
            'sha256:9ebf663f49a5d1f5209d5c94656419d21fc2022105355d51a5b2fc3ed318378c',
            'foundationSandboxLifecycle/v1alpha1', 'sha256:aabd6aa244c89a4850eba529dbe387132c3a0aff7ffba430f6115afb6e215a1b',
            'operation_effect', 'sandboxSession', p_sandbox, next_generation, next_generation,
            operation_value, next_generation, p_digest, 'pending', 0
        );
        INSERT INTO cloud_agents.coordination_audit_facts (
            tenant_id, tenant_ref_id, audit_fact_id, registry_digest, profile_id, profile_digest,
            subject_digest, operation_id, operation_generation, transition, outcome
        ) VALUES (
            sandbox.tenant_id, sandbox.tenant_id, event_value,
            'sha256:9ebf663f49a5d1f5209d5c94656419d21fc2022105355d51a5b2fc3ed318378c',
            'foundationSandboxLifecycle/v1alpha1', 'sha256:aabd6aa244c89a4850eba529dbe387132c3a0aff7ffba430f6115afb6e215a1b',
            p_subject, operation_value, next_generation, 'sandbox.' || p_action || '.accept', 'pending'
        );
        UPDATE cloud_agents.sandbox_sessions AS stored SET
            generation = next_generation,
            desired_state = CASE WHEN p_action = 'stop' THEN 'stopped' ELSE 'running' END,
            writer_released = false,
            operation_id = operation_value,
            operation_generation = next_generation,
            spec_digest = p_digest,
            stable_error_code = NULL
        WHERE stored.tenant_id = sandbox.tenant_id AND stored.project_uid = p_project
            AND stored.sandbox_uid = p_sandbox;
        UPDATE cloud_agents.idempotency_records AS record SET
            operation_id = operation_value, operation_generation = next_generation, updated_at = mutation_at
        WHERE record.tenant_id = sandbox.tenant_id AND record.subject_digest = p_subject
            AND record.profile_id = 'foundationSandboxLifecycle/v1alpha1'
            AND record.profile_digest = 'sha256:aabd6aa244c89a4850eba529dbe387132c3a0aff7ffba430f6115afb6e215a1b'
            AND record.idempotency_key = p_key;
    END IF;

    RETURN QUERY SELECT activity.operation_uid, activity.idempotency_key, activity.action,
        activity.sandbox_uid, activity.sandbox_generation, activity.subject_digest,
        activity.request_id, activity.requested_at, operation.updated_at, operation.state,
        operation.cleanup_phase, operation.terminal_error_code,
        activity.compute_disposition, activity.workspace_disposition
    FROM cloud_agents.foundation_sandbox_activity AS activity
    JOIN cloud_agents.platform_operations AS operation
      ON operation.tenant_id = activity.tenant_id AND operation.operation_id = activity.operation_uid
     AND operation.operation_generation = activity.sandbox_generation
    WHERE activity.tenant_id = cloud_agents.require_tenant_id() AND activity.operation_uid = operation_value;
    IF NOT FOUND THEN RAISE EXCEPTION USING ERRCODE = '40001', MESSAGE = 'foundation sandbox activity drifted'; END IF;
END;
$$;

CREATE FUNCTION cloud_agents.claim_foundation_sandbox_v2(
    p_holder text, p_incarnation text, p_token text, p_lease_seconds integer,
    p_subject text, p_audit text
)
RETURNS TABLE (
    tenant_id text, event_id text, delivery_attempts integer, claim_expires_at timestamptz,
    operation_id text, operation_generation bigint, action text,
    project_uid text, workspace_uid text, workspace_name text, volume_uid text,
    physical_volume_uid text, target_uid text, target_generation bigint, target_endpoint text,
    credential_ref text, sandbox_uid text, sandbox_generation bigint, image_uri text,
    runtime_profile_uid text, runtime_profile_version bigint, cpu_millis bigint, memory_bytes bigint,
    spec_digest text, runtime_uid text, runtime_state text, runtime_operation_uid text,
    runtime_generation bigint, runtime_spec_digest text
)
LANGUAGE plpgsql SECURITY DEFINER SET search_path = pg_catalog, cloud_agents
AS $$
DECLARE
    claimed_at timestamptz := transaction_timestamp();
    candidate cloud_agents.outbox_events%ROWTYPE;
BEGIN
    PERFORM cloud_agents.require_runtime_mutation_principal();
    IF NOT cloud_agents.is_valid_identifier(p_holder) OR NOT cloud_agents.is_valid_identifier(p_incarnation)
        OR NOT cloud_agents.is_valid_identifier(p_token) OR p_lease_seconds NOT BETWEEN 1 AND 60
        OR p_subject !~ '^sha256:[0-9a-f]{64}$' OR NOT cloud_agents.is_valid_identifier(p_audit)
    THEN RAISE EXCEPTION USING ERRCODE = '22023', MESSAGE = 'foundation claim input is invalid'; END IF;

    SELECT event.* INTO candidate FROM cloud_agents.outbox_events AS event
    WHERE event.profile_id = 'foundationSandboxLifecycle/v1alpha1'
        AND event.profile_digest = 'sha256:aabd6aa244c89a4850eba529dbe387132c3a0aff7ffba430f6115afb6e215a1b'
        AND event.event_class = 'operation_effect' AND event.aggregate_kind = 'sandboxSession'
        AND (event.state = 'pending' OR event.state = 'retry_wait' AND event.next_attempt_at <= claimed_at)
        AND event.delivery_attempts < 8
    ORDER BY event.created_at, event.tenant_id, event.event_id FOR UPDATE SKIP LOCKED LIMIT 1;
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
        sandbox.runtime_spec_digest
    FROM cloud_agents.sandbox_sessions AS sandbox
    JOIN cloud_agents.workspaces AS workspace USING (tenant_id, project_uid, workspace_uid)
    JOIN cloud_agents.workspace_volumes AS volume USING (tenant_id, project_uid, workspace_uid)
    JOIN cloud_agents.deployment_targets AS target
      ON target.tenant_id = volume.tenant_id AND target.project_uid = volume.project_uid
     AND target.target_uid = volume.target_uid
    LEFT JOIN cloud_agents.foundation_sandbox_activity AS activity
      ON activity.tenant_id = candidate.tenant_id AND activity.operation_uid = candidate.operation_id
     AND activity.sandbox_generation = candidate.operation_generation
    WHERE sandbox.tenant_id = candidate.tenant_id AND sandbox.operation_id = candidate.operation_id
        AND sandbox.operation_generation = candidate.operation_generation
        AND sandbox.sandbox_uid = candidate.aggregate_id AND sandbox.generation = candidate.generation
        AND NOT sandbox.writer_released AND target.target_kind = 'docker'
        AND (activity.action = 'sandbox.stop' AND sandbox.desired_state = 'stopped'
                AND sandbox.runtime_uid IS NOT NULL AND volume.observed_state = 'available'
            OR COALESCE(activity.action, 'sandbox.create') IN ('sandbox.create', 'sandbox.rebuild')
                AND sandbox.desired_state = 'running'
                AND target.observed_phase = 'ready' AND target.scheduling_state = 'active'
                AND (activity.action IS NULL OR volume.observed_state = 'available'));
    IF NOT FOUND THEN RAISE EXCEPTION USING ERRCODE = '40001', MESSAGE = 'foundation effect authority drifted'; END IF;
END;
$$;

CREATE FUNCTION cloud_agents.settle_foundation_sandbox_v2(
    p_tenant text, p_event text, p_holder text, p_incarnation text, p_token text,
    p_expiry timestamptz, p_transition text, p_runtime_uid text, p_runtime_state text,
    p_volume_uid text, p_error text, p_cleanup_complete boolean, p_subject text, p_audit text
)
RETURNS TABLE (outbox_state text, operation_state text, resource_version bigint)
LANGUAGE plpgsql SECURITY DEFINER SET search_path = pg_catalog, cloud_agents
AS $$
DECLARE
    actor text;
    settled_at timestamptz := transaction_timestamp();
    event cloud_agents.outbox_events%ROWTYPE;
    sandbox_action text;
    next_revision bigint;
    expected_revision bigint;
BEGIN
    actor := cloud_agents.require_runtime_mutation_principal();
    IF p_tenant IS DISTINCT FROM cloud_agents.require_tenant_id()
        OR p_transition NOT IN ('succeeded', 'retry', 'failed')
        OR NOT cloud_agents.is_valid_identifier(p_event) OR NOT cloud_agents.is_valid_identifier(p_holder)
        OR NOT cloud_agents.is_valid_identifier(p_incarnation) OR NOT cloud_agents.is_valid_identifier(p_token)
        OR p_expiry IS NULL OR p_cleanup_complete IS NULL OR p_subject !~ '^sha256:[0-9a-f]{64}$'
        OR NOT cloud_agents.is_valid_identifier(p_audit)
        OR p_runtime_uid IS NOT NULL AND NOT cloud_agents.is_valid_identifier(p_runtime_uid)
        OR p_volume_uid IS NOT NULL AND NOT cloud_agents.is_valid_identifier(p_volume_uid)
        OR p_runtime_state IS NOT NULL AND p_runtime_state NOT IN
            ('Pending', 'Running', 'Pausing', 'Paused', 'Resuming', 'Stopping', 'Terminated', 'Failed')
        OR p_transition <> 'succeeded' AND NOT cloud_agents.is_valid_identifier(p_error)
    THEN RAISE EXCEPTION USING ERRCODE = '22023', MESSAGE = 'foundation settlement input is invalid'; END IF;
    SELECT stored.* INTO event FROM cloud_agents.outbox_events AS stored
    WHERE stored.tenant_id = p_tenant AND stored.event_id = p_event FOR UPDATE;
    IF NOT FOUND OR event.state <> 'claimed' OR event.profile_id <> 'foundationSandboxLifecycle/v1alpha1'
        OR event.profile_digest <> 'sha256:aabd6aa244c89a4850eba529dbe387132c3a0aff7ffba430f6115afb6e215a1b'
        OR event.claim_holder_id IS DISTINCT FROM p_holder OR event.claim_incarnation IS DISTINCT FROM p_incarnation
        OR event.claim_token IS DISTINCT FROM p_token OR event.claim_expires_at IS DISTINCT FROM p_expiry
        OR event.claim_expires_at <= settled_at OR p_transition = 'retry' AND event.delivery_attempts >= 8
    THEN RAISE EXCEPTION USING ERRCODE = '40001', MESSAGE = 'foundation settlement claim is stale'; END IF;
    SELECT COALESCE(activity.action, 'sandbox.create') INTO sandbox_action
    FROM (SELECT 1) AS singleton
    LEFT JOIN cloud_agents.foundation_sandbox_activity AS activity
      ON activity.tenant_id = p_tenant AND activity.operation_uid = event.operation_id
     AND activity.sandbox_generation = event.operation_generation;
    IF p_transition = 'succeeded' AND (
        sandbox_action = 'sandbox.stop' AND (p_runtime_uid IS NOT NULL OR p_runtime_state IS NOT NULL
            OR NOT cloud_agents.is_valid_identifier(p_volume_uid) OR p_error IS NOT NULL)
        OR sandbox_action IN ('sandbox.create', 'sandbox.rebuild')
            AND (NOT cloud_agents.is_valid_identifier(p_runtime_uid) OR p_runtime_state <> 'Running'
                OR NOT cloud_agents.is_valid_identifier(p_volume_uid) OR p_error IS NOT NULL)
    ) THEN RAISE EXCEPTION USING ERRCODE = '22023', MESSAGE = 'foundation settlement input is invalid'; END IF;

    IF p_volume_uid IS NOT NULL THEN
        UPDATE cloud_agents.workspace_volumes AS volume SET observed_state = 'available',
            physical_volume_uid = p_volume_uid, observed_at = settled_at
        FROM cloud_agents.sandbox_sessions AS sandbox
        WHERE sandbox.tenant_id = p_tenant AND sandbox.operation_id = event.operation_id
            AND sandbox.operation_generation = event.operation_generation
            AND volume.tenant_id = sandbox.tenant_id AND volume.project_uid = sandbox.project_uid
            AND volume.workspace_uid = sandbox.workspace_uid
            AND (volume.physical_volume_uid IS NULL OR volume.physical_volume_uid = p_volume_uid);
        IF NOT FOUND THEN RAISE EXCEPTION USING ERRCODE = '40001', MESSAGE = 'foundation volume receipt conflicts'; END IF;
    END IF;

    IF p_transition = 'succeeded' THEN
        SELECT revision.current_revision INTO expected_revision FROM cloud_agents.tenant_resource_versions AS revision
        WHERE revision.tenant_id = p_tenant AND revision.tenant_uid = p_tenant FOR UPDATE;
        next_revision := cloud_agents.allocate_tenant_revision(p_tenant, expected_revision, settled_at);
        INSERT INTO cloud_agents.resource_changes (
            tenant_id, tenant_uid, resource_version, resource_kind, resource_uid,
            change_kind, actor_database_principal, occurred_at
        ) VALUES (p_tenant, p_tenant, next_revision, 'sandboxSession', event.aggregate_id,
            CASE WHEN sandbox_action = 'sandbox.create' THEN 'created' ELSE 'updated' END, actor, settled_at);
        PERFORM * FROM cloud_agents.transition_outbox_claim(
            p_tenant, p_event, p_holder, p_incarnation, p_token, p_expiry,
            'delivery_succeeded', NULL, p_subject, p_audit
        );
        IF sandbox_action = 'sandbox.stop' THEN
            UPDATE cloud_agents.sandbox_sessions AS sandbox SET observed_state = 'stopped',
                observed_generation = sandbox.generation, resource_version = next_revision,
                writer_released = true, runtime_uid = NULL, runtime_state = '',
                runtime_operation_uid = NULL, runtime_generation = NULL, runtime_spec_digest = NULL,
                stable_error_code = NULL, observed_at = settled_at
            WHERE sandbox.tenant_id = p_tenant AND sandbox.operation_id = event.operation_id
                AND sandbox.operation_generation = event.operation_generation AND sandbox.sandbox_uid = event.aggregate_id;
        ELSE
            UPDATE cloud_agents.sandbox_sessions AS sandbox SET observed_state = 'running',
                observed_generation = sandbox.generation, resource_version = next_revision,
                runtime_uid = p_runtime_uid, runtime_state = p_runtime_state,
                runtime_operation_uid = event.operation_id, runtime_generation = sandbox.generation,
                runtime_spec_digest = sandbox.spec_digest, stable_error_code = NULL, observed_at = settled_at
            WHERE sandbox.tenant_id = p_tenant AND sandbox.operation_id = event.operation_id
                AND sandbox.operation_generation = event.operation_generation AND sandbox.sandbox_uid = event.aggregate_id;
        END IF;
        IF NOT FOUND THEN RAISE EXCEPTION USING ERRCODE = '40001', MESSAGE = 'foundation sandbox authority drifted'; END IF;
        UPDATE cloud_agents.operation_attempts SET state = 'succeeded', claim_holder_id = NULL,
            claim_incarnation = NULL, claim_token = NULL, claim_started_at = NULL, claim_expires_at = NULL,
            updated_at = settled_at, terminal_at = settled_at
        WHERE tenant_id = p_tenant AND operation_id = event.operation_id
            AND operation_generation = event.operation_generation AND attempt_number = event.delivery_attempts;
        UPDATE cloud_agents.platform_operations SET state = 'succeeded', cleanup_phase = 'complete',
            terminal_resource_kind = 'sandboxSession', terminal_resource_id = event.aggregate_id,
            terminal_resource_version = next_revision, updated_at = settled_at, terminal_at = settled_at
        WHERE tenant_id = p_tenant AND operation_id = event.operation_id AND operation_generation = event.operation_generation;
        UPDATE cloud_agents.operation_finalizers SET state = 'succeeded', updated_at = settled_at, terminal_at = settled_at
        WHERE tenant_id = p_tenant AND operation_id = event.operation_id AND operation_generation = event.operation_generation;
        UPDATE cloud_agents.idempotency_records SET state = 'succeeded', resource_kind = 'sandboxSession',
            resource_id = event.aggregate_id, resource_version = next_revision, updated_at = settled_at, terminal_at = settled_at
        WHERE tenant_id = p_tenant AND operation_id = event.operation_id AND operation_generation = event.operation_generation;
        INSERT INTO cloud_agents.terminal_receipts (
            tenant_id, tenant_ref_id, operation_id, operation_generation, attempt_number,
            receipt_id, outcome, resource_kind, resource_id, resource_version, persisted_at
        ) VALUES (p_tenant, p_tenant, event.operation_id, event.operation_generation,
            event.delivery_attempts, 'success', 'succeeded', 'sandboxSession', event.aggregate_id, next_revision, settled_at);
        outbox_state := 'delivered'; operation_state := 'succeeded'; resource_version := next_revision;
    ELSE
        PERFORM * FROM cloud_agents.transition_outbox_claim(
            p_tenant, p_event, p_holder, p_incarnation, p_token, p_expiry,
            CASE WHEN p_transition = 'retry' THEN 'delivery_failed_retryable' ELSE 'delivery_failed_terminal' END,
            CASE WHEN p_transition = 'failed' THEN p_error END, p_subject, p_audit
        );
        UPDATE cloud_agents.operation_attempts SET state = 'failed', claim_holder_id = NULL,
            claim_incarnation = NULL, claim_token = NULL, claim_started_at = NULL, claim_expires_at = NULL,
            stable_error_code = p_error, updated_at = settled_at, terminal_at = settled_at
        WHERE tenant_id = p_tenant AND operation_id = event.operation_id
            AND operation_generation = event.operation_generation AND attempt_number = event.delivery_attempts;
        IF sandbox_action = 'sandbox.stop' THEN
            UPDATE cloud_agents.sandbox_sessions SET
                observed_state = CASE WHEN p_transition = 'retry' THEN 'unknown' ELSE 'failed' END,
                observed_generation = generation, runtime_state = COALESCE(p_runtime_state, runtime_state),
                stable_error_code = p_error, observed_at = settled_at
            WHERE tenant_id = p_tenant AND operation_id = event.operation_id AND operation_generation = event.operation_generation;
        ELSE
            UPDATE cloud_agents.sandbox_sessions SET
                observed_state = CASE WHEN p_transition = 'retry' THEN 'unknown' ELSE 'failed' END,
                observed_generation = generation, runtime_uid = p_runtime_uid,
                runtime_state = COALESCE(p_runtime_state, ''),
                runtime_operation_uid = CASE WHEN p_runtime_uid IS NULL THEN NULL ELSE event.operation_id END,
                runtime_generation = CASE WHEN p_runtime_uid IS NULL THEN NULL ELSE generation END,
                runtime_spec_digest = CASE WHEN p_runtime_uid IS NULL THEN NULL ELSE spec_digest END,
                stable_error_code = p_error, observed_at = settled_at
            WHERE tenant_id = p_tenant AND operation_id = event.operation_id AND operation_generation = event.operation_generation;
        END IF;
        IF NOT FOUND THEN RAISE EXCEPTION USING ERRCODE = '40001', MESSAGE = 'foundation sandbox authority drifted'; END IF;
        IF p_transition = 'retry' THEN
            UPDATE cloud_agents.platform_operations SET state = 'reconciling', updated_at = settled_at
            WHERE tenant_id = p_tenant AND operation_id = event.operation_id AND operation_generation = event.operation_generation;
            outbox_state := 'retry_wait'; operation_state := 'reconciling'; resource_version := NULL;
        ELSE
            UPDATE cloud_agents.platform_operations SET state = 'failed',
                cleanup_phase = CASE WHEN p_cleanup_complete THEN 'complete' ELSE 'blocked' END,
                terminal_error_code = p_error, updated_at = settled_at, terminal_at = settled_at
            WHERE tenant_id = p_tenant AND operation_id = event.operation_id AND operation_generation = event.operation_generation;
            UPDATE cloud_agents.operation_finalizers SET state = CASE WHEN p_cleanup_complete THEN 'succeeded' ELSE 'dead_letter' END,
                stable_error_code = CASE WHEN p_cleanup_complete THEN NULL ELSE p_error END,
                updated_at = settled_at, terminal_at = settled_at
            WHERE tenant_id = p_tenant AND operation_id = event.operation_id AND operation_generation = event.operation_generation;
            UPDATE cloud_agents.idempotency_records SET state = 'failed', stable_error_code = p_error,
                updated_at = settled_at, terminal_at = settled_at
            WHERE tenant_id = p_tenant AND operation_id = event.operation_id AND operation_generation = event.operation_generation;
            INSERT INTO cloud_agents.terminal_receipts (
                tenant_id, tenant_ref_id, operation_id, operation_generation, attempt_number,
                receipt_id, outcome, stable_error_code, persisted_at
            ) VALUES (p_tenant, p_tenant, event.operation_id, event.operation_generation,
                event.delivery_attempts, 'failure', 'failed', p_error, settled_at);
            outbox_state := 'dead_letter'; operation_state := 'failed'; resource_version := NULL;
        END IF;
    END IF;
    RETURN NEXT;
END;
$$;

ALTER FUNCTION cloud_agents.transition_foundation_sandbox_v1(text,text,bigint,bigint,text,text,text,text,text,text,text,text) OWNER TO cloud_agents_migration_owner;
ALTER FUNCTION cloud_agents.claim_foundation_sandbox_v2(text,text,text,integer,text,text) OWNER TO cloud_agents_migration_owner;
ALTER FUNCTION cloud_agents.settle_foundation_sandbox_v2(text,text,text,text,text,timestamptz,text,text,text,text,text,boolean,text,text) OWNER TO cloud_agents_migration_owner;
REVOKE ALL ON FUNCTION cloud_agents.transition_foundation_sandbox_v1(text,text,bigint,bigint,text,text,text,text,text,text,text,text) FROM PUBLIC;
REVOKE ALL ON FUNCTION cloud_agents.claim_foundation_sandbox_v2(text,text,text,integer,text,text) FROM PUBLIC;
REVOKE ALL ON FUNCTION cloud_agents.settle_foundation_sandbox_v2(text,text,text,text,text,timestamptz,text,text,text,text,text,boolean,text,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION cloud_agents.transition_foundation_sandbox_v1(text,text,bigint,bigint,text,text,text,text,text,text,text,text) TO cloud_agents_runtime;
GRANT EXECUTE ON FUNCTION cloud_agents.claim_foundation_sandbox_v2(text,text,text,integer,text,text) TO cloud_agents_runtime;
GRANT EXECUTE ON FUNCTION cloud_agents.settle_foundation_sandbox_v2(text,text,text,text,text,timestamptz,text,text,text,text,text,boolean,text,text) TO cloud_agents_runtime;
