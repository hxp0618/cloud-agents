ALTER TABLE cloud_agents.resource_changes DROP CONSTRAINT resource_changes_resource_kind;
ALTER TABLE cloud_agents.resource_changes ADD CONSTRAINT resource_changes_resource_kind
    CHECK (resource_kind IN ('platform_tenant', 'organization', 'project', 'membership', 'role_binding', 'sandboxSession'));

ALTER TABLE cloud_agents.workspace_volumes ADD COLUMN physical_volume_uid text;
ALTER TABLE cloud_agents.workspace_volumes ADD COLUMN observed_at timestamptz;
ALTER TABLE cloud_agents.workspace_volumes ADD CONSTRAINT workspace_volumes_physical_uid CHECK (
    physical_volume_uid IS NULL OR cloud_agents.is_valid_identifier(physical_volume_uid)
);
ALTER TABLE cloud_agents.workspace_volumes ADD CONSTRAINT workspace_volumes_observation CHECK (
    observed_state = 'pending' AND physical_volume_uid IS NULL AND observed_at IS NULL
    OR observed_state IN ('available', 'unknown', 'failed') AND observed_at IS NOT NULL
);

ALTER TABLE cloud_agents.sandbox_sessions ADD COLUMN resource_version bigint NOT NULL DEFAULT 0 CHECK (resource_version >= 0);
ALTER TABLE cloud_agents.sandbox_sessions ADD COLUMN runtime_uid text;
ALTER TABLE cloud_agents.sandbox_sessions ADD COLUMN runtime_state text NOT NULL DEFAULT '';
ALTER TABLE cloud_agents.sandbox_sessions ADD COLUMN stable_error_code text;
ALTER TABLE cloud_agents.sandbox_sessions ADD COLUMN observed_at timestamptz;
ALTER TABLE cloud_agents.sandbox_sessions ADD CONSTRAINT sandbox_sessions_runtime_uid CHECK (
    runtime_uid IS NULL OR cloud_agents.is_valid_identifier(runtime_uid)
);
ALTER TABLE cloud_agents.sandbox_sessions ADD CONSTRAINT sandbox_sessions_runtime_state CHECK (
    runtime_state IN ('', 'Pending', 'Running', 'Pausing', 'Paused', 'Resuming', 'Stopping', 'Terminated', 'Failed')
);
ALTER TABLE cloud_agents.sandbox_sessions ADD CONSTRAINT sandbox_sessions_stable_error CHECK (
    stable_error_code IS NULL OR cloud_agents.is_valid_identifier(stable_error_code)
);
ALTER TABLE cloud_agents.sandbox_sessions ADD CONSTRAINT sandbox_sessions_observation CHECK (
        observed_state = 'pending' AND observed_generation = 0 AND resource_version = 0
            AND runtime_uid IS NULL AND runtime_state = '' AND stable_error_code IS NULL AND observed_at IS NULL
        OR observed_state = 'running' AND observed_generation = generation AND resource_version > 0
            AND runtime_uid IS NOT NULL AND runtime_state = 'Running' AND stable_error_code IS NULL AND observed_at IS NOT NULL
        OR observed_state IN ('unknown', 'failed') AND observed_generation = generation
            AND stable_error_code IS NOT NULL AND observed_at IS NOT NULL
        OR observed_state = 'stopped' AND observed_generation = generation AND observed_at IS NOT NULL
);

-- This legacy dispatcher only delivers the project resource-change profile. Without this
-- predicate it can claim operation effects owned by the foundation controller.
CREATE OR REPLACE FUNCTION cloud_agents.claim_outbox_event(
    p_tenant_id text, p_holder_id text, p_holder_incarnation text, p_claim_token text,
    p_lease_seconds integer, p_subject_digest text, p_audit_fact_id text
)
RETURNS TABLE (
    event_id text, profile_id text, profile_digest text, event_class text,
    aggregate_kind text, aggregate_id text, aggregate_sequence bigint,
    resource_version bigint, generation bigint, operation_id text,
    operation_generation bigint, payload_digest text, delivery_attempts integer,
    claim_expires_at timestamptz
)
LANGUAGE plpgsql SECURITY DEFINER SET search_path = pg_catalog, cloud_agents
AS $$
DECLARE
    actor_principal text;
    claimed_at timestamptz := transaction_timestamp();
    candidate cloud_agents.outbox_events%ROWTYPE;
BEGIN
    actor_principal := cloud_agents.require_runtime_mutation_principal();
    IF p_tenant_id IS DISTINCT FROM cloud_agents.require_tenant_id()
        OR NOT cloud_agents.is_valid_identifier(p_holder_id)
        OR NOT cloud_agents.is_valid_identifier(p_holder_incarnation)
        OR NOT cloud_agents.is_valid_identifier(p_claim_token)
        OR p_lease_seconds NOT BETWEEN 1 AND 60
        OR p_subject_digest !~ '^sha256:[0-9a-f]{64}$'
        OR NOT cloud_agents.is_valid_identifier(p_audit_fact_id)
    THEN RAISE EXCEPTION USING ERRCODE = '22023', MESSAGE = 'outbox claim input is invalid'; END IF;

    SELECT event.* INTO candidate FROM cloud_agents.outbox_events AS event
    WHERE event.tenant_id = p_tenant_id
        AND event.profile_id = 'managedAgentCreateProject/v1alpha1'
        AND event.profile_digest = 'sha256:cdd54c59c0c363687eeb0e0e48aff00221cc4d42780bdf935c4b47bcd297f308'
        AND event.event_class = 'resource_change' AND event.aggregate_kind = 'project'
        AND (event.state = 'pending' OR event.state = 'retry_wait' AND event.next_attempt_at <= claimed_at)
        AND event.delivery_attempts < 8
    ORDER BY event.created_at, event.event_id FOR UPDATE SKIP LOCKED LIMIT 1;
    IF NOT FOUND THEN RETURN; END IF;

    UPDATE cloud_agents.outbox_events AS event SET state = 'claimed',
        delivery_attempts = event.delivery_attempts + 1, next_attempt_at = NULL,
        claim_holder_id = p_holder_id, claim_incarnation = p_holder_incarnation,
        claim_token = p_claim_token, claim_started_at = claimed_at,
        claim_expires_at = claimed_at + pg_catalog.make_interval(secs => p_lease_seconds),
        updated_at = claimed_at
    WHERE event.tenant_id = p_tenant_id AND event.event_id = candidate.event_id
    RETURNING event.event_id, event.profile_id, event.profile_digest, event.event_class,
        event.aggregate_kind, event.aggregate_id, event.aggregate_sequence,
        event.resource_version, event.generation, event.operation_id,
        event.operation_generation, event.payload_digest, event.delivery_attempts,
        event.claim_expires_at
    INTO event_id, profile_id, profile_digest, event_class, aggregate_kind,
        aggregate_id, aggregate_sequence, resource_version, generation, operation_id,
        operation_generation, payload_digest, delivery_attempts, claim_expires_at;

    PERFORM cloud_agents.append_coordination_audit(
        p_tenant_id, p_audit_fact_id, profile_id, profile_digest, p_subject_digest,
        operation_id, operation_generation, NULL, aggregate_kind, aggregate_id,
        resource_version, 'outbox.claim', 'pending', NULL, NULL, claimed_at
    );
    RETURN NEXT;
END;
$$;

CREATE FUNCTION cloud_agents.claim_foundation_sandbox_v1(
    p_holder text, p_incarnation text, p_token text, p_lease_seconds integer,
    p_subject text, p_audit text
)
RETURNS TABLE (
    tenant_id text, event_id text, delivery_attempts integer, claim_expires_at timestamptz,
    operation_id text, operation_generation bigint, project_uid text, workspace_uid text, workspace_name text,
    volume_uid text, target_uid text, target_generation bigint, target_endpoint text,
    credential_ref text, sandbox_uid text, sandbox_generation bigint, image_uri text,
    runtime_profile_uid text, runtime_profile_version bigint,
    cpu_millis bigint, memory_bytes bigint, spec_digest text
)
LANGUAGE plpgsql SECURITY DEFINER SET search_path = pg_catalog, cloud_agents
AS $$
DECLARE
    actor text;
    claimed_at timestamptz := transaction_timestamp();
    candidate cloud_agents.outbox_events%ROWTYPE;
BEGIN
    actor := cloud_agents.require_runtime_mutation_principal();
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
    ORDER BY event.created_at, event.tenant_id, event.event_id
    FOR UPDATE SKIP LOCKED LIMIT 1;
    IF NOT FOUND THEN RETURN; END IF;
    PERFORM pg_catalog.set_config('cloud_agents.tenant_id', candidate.tenant_id, true);

    UPDATE cloud_agents.outbox_events AS event SET state = 'claimed',
        delivery_attempts = event.delivery_attempts + 1, next_attempt_at = NULL,
        claim_holder_id = p_holder, claim_incarnation = p_incarnation, claim_token = p_token,
        claim_started_at = claimed_at,
        claim_expires_at = claimed_at + pg_catalog.make_interval(secs => p_lease_seconds),
        updated_at = claimed_at
    WHERE event.tenant_id = candidate.tenant_id AND event.event_id = candidate.event_id
    RETURNING event.* INTO candidate;

    UPDATE cloud_agents.platform_operations AS operation SET
        state = CASE WHEN candidate.delivery_attempts = 1 THEN 'running' ELSE 'reconciling' END,
        recovery_generation = candidate.delivery_attempts - 1,
        current_attempt_number = candidate.delivery_attempts,
        updated_at = claimed_at
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
        sandbox.project_uid, sandbox.workspace_uid, workspace.workspace_name, volume.volume_uid, volume.target_uid,
        target.generation, target.endpoint, target.credential_ref, sandbox.sandbox_uid,
        sandbox.generation, sandbox.image_uri, sandbox.runtime_profile_uid, sandbox.runtime_profile_version,
        sandbox.cpu_millis, sandbox.memory_bytes, sandbox.spec_digest
    FROM cloud_agents.sandbox_sessions AS sandbox
    JOIN cloud_agents.workspaces AS workspace USING (tenant_id, project_uid, workspace_uid)
    JOIN cloud_agents.workspace_volumes AS volume USING (tenant_id, project_uid, workspace_uid)
    JOIN cloud_agents.deployment_targets AS target
        ON target.tenant_id = volume.tenant_id AND target.project_uid = volume.project_uid
        AND target.target_uid = volume.target_uid
    WHERE sandbox.tenant_id = candidate.tenant_id AND sandbox.operation_id = candidate.operation_id
        AND sandbox.operation_generation = candidate.operation_generation
        AND sandbox.sandbox_uid = candidate.aggregate_id AND sandbox.generation = candidate.generation
        AND sandbox.desired_state = 'running' AND NOT sandbox.writer_released
        AND target.target_kind = 'docker' AND target.observed_phase = 'ready'
        AND target.scheduling_state = 'active';
    IF NOT FOUND THEN RAISE EXCEPTION USING ERRCODE = '40001', MESSAGE = 'foundation effect authority drifted'; END IF;
END;
$$;

CREATE FUNCTION cloud_agents.renew_foundation_sandbox_claim_v1(
    p_tenant text, p_event text, p_holder text, p_incarnation text, p_token text,
    p_prior_expiry timestamptz, p_lease_seconds integer
)
RETURNS timestamptz LANGUAGE plpgsql SECURITY DEFINER SET search_path = pg_catalog, cloud_agents
AS $$
DECLARE renewed_at timestamptz := transaction_timestamp(); renewed_expiry timestamptz;
BEGIN
    PERFORM cloud_agents.require_runtime_mutation_principal();
    IF p_tenant IS DISTINCT FROM cloud_agents.require_tenant_id() OR p_lease_seconds NOT BETWEEN 1 AND 60
        OR NOT cloud_agents.is_valid_identifier(p_event) OR NOT cloud_agents.is_valid_identifier(p_holder)
        OR NOT cloud_agents.is_valid_identifier(p_incarnation) OR NOT cloud_agents.is_valid_identifier(p_token)
        OR p_prior_expiry IS NULL
    THEN RAISE EXCEPTION USING ERRCODE = '22023', MESSAGE = 'foundation renewal input is invalid'; END IF;
    renewed_expiry := renewed_at + pg_catalog.make_interval(secs => p_lease_seconds);
    UPDATE cloud_agents.outbox_events AS event SET claim_expires_at = renewed_expiry, updated_at = renewed_at
    WHERE event.tenant_id = p_tenant AND event.event_id = p_event AND event.state = 'claimed'
        AND event.profile_id = 'foundationSandboxLifecycle/v1alpha1'
        AND event.claim_holder_id = p_holder AND event.claim_incarnation = p_incarnation
        AND event.claim_token = p_token AND event.claim_expires_at = p_prior_expiry
        AND event.claim_expires_at > renewed_at;
    IF NOT FOUND THEN RAISE EXCEPTION USING ERRCODE = '40001', MESSAGE = 'foundation claim is stale'; END IF;
    UPDATE cloud_agents.operation_attempts AS attempt SET claim_expires_at = renewed_expiry, updated_at = renewed_at
    FROM cloud_agents.outbox_events AS event
    WHERE event.tenant_id = p_tenant AND event.event_id = p_event
        AND attempt.tenant_id = event.tenant_id AND attempt.operation_id = event.operation_id
        AND attempt.operation_generation = event.operation_generation
        AND attempt.attempt_number = event.delivery_attempts AND attempt.state = 'claimed'
        AND attempt.claim_holder_id = p_holder AND attempt.claim_incarnation = p_incarnation
        AND attempt.claim_token = p_token AND attempt.claim_expires_at = p_prior_expiry;
    IF NOT FOUND THEN RAISE EXCEPTION USING ERRCODE = '40001', MESSAGE = 'foundation attempt claim is stale'; END IF;
    RETURN renewed_expiry;
END;
$$;

CREATE FUNCTION cloud_agents.settle_foundation_sandbox_v1(
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
        OR (p_transition = 'succeeded' AND (NOT cloud_agents.is_valid_identifier(p_runtime_uid)
            OR p_runtime_state <> 'Running' OR NOT cloud_agents.is_valid_identifier(p_volume_uid) OR p_error IS NOT NULL))
        OR (p_transition <> 'succeeded' AND NOT cloud_agents.is_valid_identifier(p_error))
    THEN RAISE EXCEPTION USING ERRCODE = '22023', MESSAGE = 'foundation settlement input is invalid'; END IF;
    SELECT stored.* INTO event FROM cloud_agents.outbox_events AS stored
    WHERE stored.tenant_id = p_tenant AND stored.event_id = p_event FOR UPDATE;
    IF NOT FOUND OR event.state <> 'claimed' OR event.profile_id <> 'foundationSandboxLifecycle/v1alpha1'
        OR event.profile_digest <> 'sha256:aabd6aa244c89a4850eba529dbe387132c3a0aff7ffba430f6115afb6e215a1b'
        OR event.claim_holder_id IS DISTINCT FROM p_holder OR event.claim_incarnation IS DISTINCT FROM p_incarnation
        OR event.claim_token IS DISTINCT FROM p_token OR event.claim_expires_at IS DISTINCT FROM p_expiry
        OR event.claim_expires_at <= settled_at OR p_transition = 'retry' AND event.delivery_attempts >= 8
    THEN RAISE EXCEPTION USING ERRCODE = '40001', MESSAGE = 'foundation settlement claim is stale'; END IF;

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
        ) VALUES (p_tenant, p_tenant, next_revision, 'sandboxSession', event.aggregate_id, 'created', actor, settled_at);
        PERFORM * FROM cloud_agents.transition_outbox_claim(
            p_tenant, p_event, p_holder, p_incarnation, p_token, p_expiry,
            'delivery_succeeded', NULL, p_subject, p_audit
        );
        UPDATE cloud_agents.sandbox_sessions AS sandbox SET observed_state = 'running',
            observed_generation = sandbox.generation, resource_version = next_revision,
            runtime_uid = p_runtime_uid, runtime_state = p_runtime_state,
            stable_error_code = NULL, observed_at = settled_at
        WHERE sandbox.tenant_id = p_tenant AND sandbox.operation_id = event.operation_id
            AND sandbox.operation_generation = event.operation_generation AND sandbox.sandbox_uid = event.aggregate_id;
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
        UPDATE cloud_agents.sandbox_sessions SET observed_state = CASE WHEN p_transition = 'retry' THEN 'unknown' ELSE 'failed' END,
            observed_generation = generation, runtime_uid = p_runtime_uid, runtime_state = COALESCE(p_runtime_state, ''),
            stable_error_code = p_error, observed_at = settled_at
        WHERE tenant_id = p_tenant AND operation_id = event.operation_id AND operation_generation = event.operation_generation;
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

CREATE FUNCTION cloud_agents.reap_foundation_sandbox_claim_v1(p_subject text, p_audit text)
RETURNS TABLE (tenant_id text, event_id text, outbox_state text, delivery_attempts integer)
LANGUAGE plpgsql SECURITY DEFINER SET search_path = pg_catalog, cloud_agents
AS $$
DECLARE
    actor text;
    reaped_at timestamptz := transaction_timestamp();
    candidate cloud_agents.outbox_events%ROWTYPE;
    error_code constant text := 'foundation_claim_expired';
BEGIN
    actor := cloud_agents.require_runtime_mutation_principal();
    IF p_subject !~ '^sha256:[0-9a-f]{64}$' OR NOT cloud_agents.is_valid_identifier(p_audit)
    THEN RAISE EXCEPTION USING ERRCODE = '22023', MESSAGE = 'foundation reaper input is invalid'; END IF;

    SELECT event.* INTO candidate FROM cloud_agents.outbox_events AS event
    WHERE event.profile_id = 'foundationSandboxLifecycle/v1alpha1'
        AND event.profile_digest = 'sha256:aabd6aa244c89a4850eba529dbe387132c3a0aff7ffba430f6115afb6e215a1b'
        AND event.event_class = 'operation_effect' AND event.aggregate_kind = 'sandboxSession'
        AND event.state = 'claimed' AND event.claim_expires_at <= reaped_at
    ORDER BY event.claim_expires_at, event.tenant_id, event.event_id
    FOR UPDATE SKIP LOCKED LIMIT 1;
    IF NOT FOUND THEN RETURN; END IF;
    PERFORM pg_catalog.set_config('cloud_agents.tenant_id', candidate.tenant_id, true);

    UPDATE cloud_agents.operation_attempts AS attempt SET
        state = CASE WHEN candidate.delivery_attempts < 8 THEN 'unknown' ELSE 'failed' END,
        claim_holder_id = NULL, claim_incarnation = NULL, claim_token = NULL,
        claim_started_at = NULL, claim_expires_at = NULL,
        stable_error_code = CASE WHEN candidate.delivery_attempts < 8 THEN NULL ELSE error_code END,
        updated_at = reaped_at,
        terminal_at = CASE WHEN candidate.delivery_attempts < 8 THEN NULL ELSE reaped_at END
    WHERE attempt.tenant_id = candidate.tenant_id AND attempt.operation_id = candidate.operation_id
        AND attempt.operation_generation = candidate.operation_generation
        AND attempt.attempt_number = candidate.delivery_attempts AND attempt.state = 'claimed';
    IF NOT FOUND THEN RAISE EXCEPTION USING ERRCODE = '40001', MESSAGE = 'foundation attempt authority drifted'; END IF;

    UPDATE cloud_agents.workspace_volumes AS volume SET observed_state = 'unknown', observed_at = reaped_at
    FROM cloud_agents.sandbox_sessions AS sandbox
    WHERE sandbox.tenant_id = candidate.tenant_id AND sandbox.operation_id = candidate.operation_id
        AND sandbox.operation_generation = candidate.operation_generation
        AND volume.tenant_id = sandbox.tenant_id AND volume.project_uid = sandbox.project_uid
        AND volume.workspace_uid = sandbox.workspace_uid AND volume.observed_state = 'pending';
    UPDATE cloud_agents.sandbox_sessions AS sandbox SET
        observed_state = CASE WHEN candidate.delivery_attempts < 8 THEN 'unknown' ELSE 'failed' END,
        observed_generation = sandbox.generation, stable_error_code = error_code, observed_at = reaped_at
    WHERE sandbox.tenant_id = candidate.tenant_id AND sandbox.operation_id = candidate.operation_id
        AND sandbox.operation_generation = candidate.operation_generation AND sandbox.sandbox_uid = candidate.aggregate_id;
    IF NOT FOUND THEN RAISE EXCEPTION USING ERRCODE = '40001', MESSAGE = 'foundation sandbox authority drifted'; END IF;

    IF candidate.delivery_attempts < 8 THEN
        outbox_state := 'pending';
        UPDATE cloud_agents.outbox_events AS event SET state = outbox_state,
            claim_holder_id = NULL, claim_incarnation = NULL, claim_token = NULL,
            claim_started_at = NULL, claim_expires_at = NULL, stable_error_code = NULL,
            updated_at = reaped_at
        WHERE event.tenant_id = candidate.tenant_id AND event.event_id = candidate.event_id;
        UPDATE cloud_agents.platform_operations AS operation SET state = 'reconciling', updated_at = reaped_at
        WHERE operation.tenant_id = candidate.tenant_id AND operation.operation_id = candidate.operation_id
            AND operation.operation_generation = candidate.operation_generation;
    ELSE
        outbox_state := 'dead_letter';
        UPDATE cloud_agents.outbox_events AS event SET state = outbox_state,
            claim_holder_id = NULL, claim_incarnation = NULL, claim_token = NULL,
            claim_started_at = NULL, claim_expires_at = NULL, stable_error_code = error_code,
            dead_lettered_at = reaped_at, updated_at = reaped_at
        WHERE event.tenant_id = candidate.tenant_id AND event.event_id = candidate.event_id;
        UPDATE cloud_agents.platform_operations AS operation SET state = 'failed', cleanup_phase = 'blocked',
            terminal_error_code = error_code, updated_at = reaped_at, terminal_at = reaped_at
        WHERE operation.tenant_id = candidate.tenant_id AND operation.operation_id = candidate.operation_id
            AND operation.operation_generation = candidate.operation_generation;
        UPDATE cloud_agents.operation_finalizers AS finalizer SET state = 'dead_letter',
            delivery_attempts = candidate.delivery_attempts, stable_error_code = error_code,
            updated_at = reaped_at, terminal_at = reaped_at
        WHERE finalizer.tenant_id = candidate.tenant_id AND finalizer.operation_id = candidate.operation_id
            AND finalizer.operation_generation = candidate.operation_generation;
        UPDATE cloud_agents.idempotency_records AS record SET state = 'failed', stable_error_code = error_code,
            updated_at = reaped_at, terminal_at = reaped_at
        WHERE record.tenant_id = candidate.tenant_id AND record.operation_id = candidate.operation_id
            AND record.operation_generation = candidate.operation_generation;
        INSERT INTO cloud_agents.terminal_receipts (
            tenant_id, tenant_ref_id, operation_id, operation_generation, attempt_number,
            receipt_id, outcome, stable_error_code, persisted_at
        ) VALUES (candidate.tenant_id, candidate.tenant_id, candidate.operation_id,
            candidate.operation_generation, candidate.delivery_attempts,
            'claim-expired', 'failed', error_code, reaped_at);
    END IF;

    PERFORM cloud_agents.append_coordination_audit(
        candidate.tenant_id, p_audit, candidate.profile_id, candidate.profile_digest,
        p_subject, candidate.operation_id, candidate.operation_generation,
        candidate.delivery_attempts, NULL, NULL, NULL,
        CASE WHEN outbox_state = 'pending' THEN 'sandbox.claim_expired_retryable'
            ELSE 'sandbox.claim_expired_terminal' END,
        CASE WHEN outbox_state = 'pending' THEN 'unknown' ELSE 'failed' END,
        error_code, NULL, reaped_at
    );
    tenant_id := candidate.tenant_id;
    event_id := candidate.event_id;
    delivery_attempts := candidate.delivery_attempts;
    RETURN NEXT;
END;
$$;

ALTER FUNCTION cloud_agents.claim_foundation_sandbox_v1(text,text,text,integer,text,text) OWNER TO cloud_agents_migration_owner;
ALTER FUNCTION cloud_agents.renew_foundation_sandbox_claim_v1(text,text,text,text,text,timestamptz,integer) OWNER TO cloud_agents_migration_owner;
ALTER FUNCTION cloud_agents.settle_foundation_sandbox_v1(text,text,text,text,text,timestamptz,text,text,text,text,text,boolean,text,text) OWNER TO cloud_agents_migration_owner;
ALTER FUNCTION cloud_agents.reap_foundation_sandbox_claim_v1(text,text) OWNER TO cloud_agents_migration_owner;
REVOKE ALL ON FUNCTION cloud_agents.claim_foundation_sandbox_v1(text,text,text,integer,text,text) FROM PUBLIC;
REVOKE ALL ON FUNCTION cloud_agents.renew_foundation_sandbox_claim_v1(text,text,text,text,text,timestamptz,integer) FROM PUBLIC;
REVOKE ALL ON FUNCTION cloud_agents.settle_foundation_sandbox_v1(text,text,text,text,text,timestamptz,text,text,text,text,text,boolean,text,text) FROM PUBLIC;
REVOKE ALL ON FUNCTION cloud_agents.reap_foundation_sandbox_claim_v1(text,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION cloud_agents.claim_foundation_sandbox_v1(text,text,text,integer,text,text) TO cloud_agents_runtime;
GRANT EXECUTE ON FUNCTION cloud_agents.renew_foundation_sandbox_claim_v1(text,text,text,text,text,timestamptz,integer) TO cloud_agents_runtime;
GRANT EXECUTE ON FUNCTION cloud_agents.settle_foundation_sandbox_v1(text,text,text,text,text,timestamptz,text,text,text,text,text,boolean,text,text) TO cloud_agents_runtime;
GRANT EXECUTE ON FUNCTION cloud_agents.reap_foundation_sandbox_claim_v1(text,text) TO cloud_agents_runtime;
