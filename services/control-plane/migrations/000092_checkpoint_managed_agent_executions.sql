ALTER TABLE cloud_agents.managed_agent_executions
    DROP CONSTRAINT managed_agent_executions_runtime_messages;

ALTER TABLE cloud_agents.managed_agent_executions
    ADD COLUMN attempt_number bigint NOT NULL DEFAULT 0,
    ADD COLUMN claim_holder_id text,
    ADD COLUMN claim_incarnation text,
    ADD COLUMN claim_token text,
    ADD COLUMN claim_expires_at timestamptz,
    ADD COLUMN checkpoint_sequence bigint NOT NULL DEFAULT 0,
    ADD COLUMN checkpoint_digest text,
    ADD COLUMN checkpointed_at timestamptz,
    ADD COLUMN checkpoint_protocol text,
    ADD COLUMN checkpoint_provider_resume_cursor text,
    ADD COLUMN checkpoint_target_uid text,
    ADD COLUMN pending_side_effect boolean NOT NULL DEFAULT false,
    ADD COLUMN pending_interaction_count integer NOT NULL DEFAULT 0,
    ADD COLUMN recovery_state text NOT NULL DEFAULT 'none',
    ADD COLUMN recovery_reason text,
    ADD COLUMN recovery_mode text,
    ADD COLUMN recovery_source_target_uid text,
    ADD COLUMN recovery_target_uid text,
    ADD COLUMN side_effect_reconciliation_outcome text,
    ADD COLUMN side_effect_reconciliation_checkpoint_digest text,
    ADD COLUMN side_effect_reconciliation_digest text,
    ADD COLUMN side_effect_reconciled_at timestamptz;

ALTER TABLE cloud_agents.managed_agent_executions
    ADD CONSTRAINT managed_agent_executions_runtime_messages
        CHECK (
            runtime_messages IS NULL
            OR state IN ('running', 'succeeded', 'failed')
                AND pg_catalog.octet_length(runtime_messages) BETWEEN 2 AND 1048576
                AND pg_catalog.jsonb_typeof(runtime_messages::pg_catalog.jsonb) = 'array'
                AND pg_catalog.jsonb_array_length(runtime_messages::pg_catalog.jsonb) BETWEEN 1 AND 64
        ),
    ADD CONSTRAINT managed_agent_executions_attempt_number CHECK (attempt_number >= 0),
    ADD CONSTRAINT managed_agent_executions_claim CHECK (
        (claim_holder_id IS NULL AND claim_incarnation IS NULL AND claim_token IS NULL AND claim_expires_at IS NULL)
        OR (cloud_agents.is_valid_identifier(claim_holder_id)
            AND cloud_agents.is_valid_identifier(claim_incarnation)
            AND cloud_agents.is_valid_identifier(claim_token)
            AND claim_expires_at IS NOT NULL
            AND state IN ('queued', 'running'))
    ),
    ADD CONSTRAINT managed_agent_executions_checkpoint CHECK (
        checkpoint_sequence >= 0
        AND pending_interaction_count BETWEEN 0 AND 64
        AND recovery_state IN ('none', 'recovering', 'recovered', 'awaiting_reconciliation')
        AND (recovery_reason IS NULL OR recovery_reason ~ '^[a-z0-9_-]{1,64}$')
        AND (
            checkpoint_sequence = 0
                AND checkpoint_digest IS NULL
                AND checkpointed_at IS NULL
                AND checkpoint_protocol IS NULL
                AND checkpoint_provider_resume_cursor IS NULL
                AND checkpoint_target_uid IS NULL
                AND NOT pending_side_effect
                AND pending_interaction_count = 0
            OR checkpoint_sequence > 0
                AND checkpoint_digest ~ '^sha256:[0-9a-f]{64}$'
                AND checkpointed_at IS NOT NULL
                AND checkpoint_protocol ~ '^[a-z0-9][a-z0-9._-]{0,79}$'
                AND (checkpoint_provider_resume_cursor IS NULL OR
                    pg_catalog.octet_length(checkpoint_provider_resume_cursor) BETWEEN 1 AND 4096
                    AND checkpoint_provider_resume_cursor = pg_catalog.btrim(checkpoint_provider_resume_cursor)
                    AND checkpoint_provider_resume_cursor !~ '[[:cntrl:]]')
                AND (checkpoint_target_uid IS NULL OR cloud_agents.is_valid_identifier(checkpoint_target_uid))
        )
    ),
    ADD CONSTRAINT managed_agent_executions_recovery_placement CHECK (
        (
            recovery_mode IS NULL
            AND recovery_source_target_uid IS NULL
            AND recovery_target_uid IS NULL
        ) OR (
            recovery_mode IN ('same-node-reconnect', 'process-restart', 'cross-node-takeover')
            AND (recovery_source_target_uid IS NULL OR cloud_agents.is_valid_identifier(recovery_source_target_uid))
            AND (recovery_target_uid IS NULL OR cloud_agents.is_valid_identifier(recovery_target_uid))
            AND (recovery_mode <> 'cross-node-takeover'
                OR recovery_source_target_uid IS NOT NULL AND recovery_target_uid IS NOT NULL
                    AND recovery_source_target_uid <> recovery_target_uid)
        )
    ),
    ADD CONSTRAINT managed_agent_executions_side_effect_reconciliation CHECK (
        (
            side_effect_reconciliation_outcome IS NULL
            AND side_effect_reconciliation_checkpoint_digest IS NULL
            AND side_effect_reconciliation_digest IS NULL
            AND side_effect_reconciled_at IS NULL
        ) OR (
            side_effect_reconciliation_outcome IN ('confirmed', 'not-applied')
            AND side_effect_reconciliation_checkpoint_digest ~ '^sha256:[0-9a-f]{64}$'
            AND side_effect_reconciliation_digest ~ '^sha256:[0-9a-f]{64}$'
            AND side_effect_reconciled_at IS NOT NULL
            AND checkpoint_sequence > 0
        )
    );

ALTER TABLE cloud_agents.managed_agent_events
    DROP CONSTRAINT managed_agent_events_operation;

ALTER TABLE cloud_agents.managed_agent_events
    ADD CONSTRAINT managed_agent_events_operation CHECK (operation IN (
        'session.create', 'session.close', 'turn.create', 'execution.create',
        'execution.start', 'execution.reconnect', 'execution.restart',
        'execution.takeover', 'execution.recovery-blocked',
        'execution.reconcile', 'execution.receipt-rejected',
        'execution.complete', 'execution.fail', 'turn.interrupt', 'turn.cancel'
    ));

CREATE OR REPLACE FUNCTION cloud_agents.append_managed_agent_event_v1(
    p_tenant_id text,
    p_project_uid text,
    p_session_uid text,
    p_operation text,
    p_resource text,
    p_turn_uid text,
    p_execution_uid text,
    p_generation bigint,
    p_mutation_digest text,
    p_input_digest text,
    p_result_digest text,
    p_error_code text,
    p_changes jsonb
)
RETURNS text
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, cloud_agents
AS $cloud_agents_function$
DECLARE
    sequence_value bigint;
    event_value text;
BEGIN
    IF p_tenant_id IS DISTINCT FROM cloud_agents.require_tenant_id()
        OR NOT cloud_agents.is_valid_identifier(p_project_uid)
        OR NOT cloud_agents.is_valid_identifier(p_session_uid)
        OR p_operation IS NULL OR p_operation NOT IN (
            'session.create', 'session.close', 'turn.create', 'execution.create',
            'execution.start', 'execution.reconnect', 'execution.restart',
            'execution.takeover', 'execution.recovery-blocked',
            'execution.reconcile', 'execution.receipt-rejected',
            'execution.complete', 'execution.fail', 'turn.interrupt', 'turn.cancel')
        OR p_resource IS NULL OR p_resource NOT IN ('Session', 'Turn', 'Execution')
        OR p_generation IS NULL OR p_generation < 0
        OR p_mutation_digest IS NULL OR p_mutation_digest !~ '^sha256:[0-9a-f]{64}$'
        OR (p_input_digest IS NOT NULL AND p_input_digest !~ '^sha256:[0-9a-f]{64}$')
        OR (p_result_digest IS NOT NULL AND p_result_digest !~ '^sha256:[0-9a-f]{64}$')
        OR (p_error_code IS NOT NULL AND p_error_code !~ '^[a-z0-9_-]{1,64}$')
        OR p_changes IS NULL OR jsonb_typeof(p_changes) <> 'array'
        OR jsonb_array_length(p_changes) NOT BETWEEN 1 AND 4
        OR EXISTS (
            SELECT 1
            FROM jsonb_array_elements(p_changes) AS change
            WHERE change->>'resource' NOT IN ('Session', 'Turn', 'Execution')
                OR change->>'from' IS NULL OR change->>'to' IS NULL
                OR change->>'version' !~ '^[1-9][0-9]*$'
        )
    THEN
        RAISE EXCEPTION USING ERRCODE = '22023', MESSAGE = 'managed agent event input is invalid';
    END IF;
    PERFORM pg_catalog.pg_advisory_xact_lock(
        pg_catalog.hashtextextended(
            pg_catalog.jsonb_build_array(
                'cloud-agents-managed-agent-event-lock/v1',
                p_tenant_id,
                p_project_uid,
                p_session_uid
            )::text,
            0
        )
    );
    SELECT event_uid INTO event_value
    FROM cloud_agents.managed_agent_events
    WHERE tenant_id = p_tenant_id AND project_uid = p_project_uid
        AND session_uid = p_session_uid AND operation = p_operation
        AND mutation_digest = p_mutation_digest;
    IF event_value IS NOT NULL THEN
        RETURN event_value;
    END IF;
    SELECT COALESCE(pg_catalog.max(event_sequence), 0) + 1 INTO sequence_value
    FROM cloud_agents.managed_agent_events
    WHERE tenant_id = p_tenant_id AND project_uid = p_project_uid AND session_uid = p_session_uid;
    event_value := 'managed-agent-event-' || pg_catalog.encode(
        pg_catalog.sha256(
            pg_catalog.convert_to(
                pg_catalog.octet_length('cloud-agents/managed-agent-events/event-id/v1')::text || ':'
                    || 'cloud-agents/managed-agent-events/event-id/v1'
                    || pg_catalog.octet_length(p_tenant_id)::text || ':' || p_tenant_id
                    || pg_catalog.octet_length(p_project_uid)::text || ':' || p_project_uid
                    || pg_catalog.octet_length(p_session_uid)::text || ':' || p_session_uid
                    || pg_catalog.octet_length(sequence_value::text)::text || ':' || sequence_value::text,
                'UTF8')),
        'hex');
    INSERT INTO cloud_agents.managed_agent_events (
        tenant_id, tenant_ref_id, project_uid, session_uid, event_sequence,
        event_uid, operation, resource, turn_uid, execution_uid, generation,
        mutation_digest, input_digest, result_digest, error_code, changes,
        occurred_at
    ) VALUES (
        p_tenant_id, p_tenant_id, p_project_uid, p_session_uid, sequence_value,
        event_value, p_operation, p_resource, p_turn_uid, p_execution_uid,
        p_generation, p_mutation_digest, p_input_digest, p_result_digest,
        p_error_code, p_changes, pg_catalog.transaction_timestamp()
    );
    RETURN event_value;
END;
$cloud_agents_function$;

CREATE TABLE cloud_agents.managed_agent_execution_interaction_resolutions (
    tenant_id text NOT NULL,
    tenant_ref_id text NOT NULL,
    project_uid text NOT NULL,
    session_uid text NOT NULL,
    turn_uid text NOT NULL,
    execution_uid text NOT NULL,
    interaction_request_uid text NOT NULL,
    interaction_type text NOT NULL,
    request_uid text NOT NULL,
    resolution_digest text NOT NULL,
    resolution_payload text NOT NULL,
    created_at timestamptz NOT NULL,
    PRIMARY KEY (tenant_id, project_uid, session_uid, turn_uid, execution_uid, interaction_request_uid),
    CONSTRAINT managed_agent_execution_interaction_resolutions_tenant CHECK (tenant_id = tenant_ref_id),
    CONSTRAINT managed_agent_execution_interaction_resolutions_request CHECK (
        pg_catalog.char_length(interaction_request_uid) BETWEEN 1 AND 200
        AND interaction_request_uid !~ '[[:cntrl:]]'
    ),
    CONSTRAINT managed_agent_execution_interaction_resolutions_type CHECK (interaction_type IN ('approval', 'user-input')),
    CONSTRAINT managed_agent_execution_interaction_resolutions_request_uid CHECK (cloud_agents.is_valid_identifier(request_uid)),
    CONSTRAINT managed_agent_execution_interaction_resolutions_digest CHECK (resolution_digest ~ '^sha256:[0-9a-f]{64}$'),
    CONSTRAINT managed_agent_execution_interaction_resolutions_payload CHECK (
        pg_catalog.octet_length(resolution_payload) BETWEEN 2 AND 65536
        AND pg_catalog.jsonb_typeof(resolution_payload::pg_catalog.jsonb) = 'object'
    ),
    CONSTRAINT managed_agent_execution_interaction_resolutions_execution_fk
        FOREIGN KEY (tenant_id, project_uid, session_uid, turn_uid, execution_uid)
        REFERENCES cloud_agents.managed_agent_executions (tenant_id, project_uid, session_uid, turn_uid, execution_uid)
        ON UPDATE RESTRICT ON DELETE RESTRICT
);

ALTER TABLE cloud_agents.managed_agent_execution_interaction_resolutions OWNER TO cloud_agents_migration_owner;
ALTER TABLE cloud_agents.managed_agent_execution_interaction_resolutions ENABLE ROW LEVEL SECURITY;
ALTER TABLE cloud_agents.managed_agent_execution_interaction_resolutions FORCE ROW LEVEL SECURITY;
CREATE POLICY managed_agent_execution_interaction_resolutions_runtime_tenant
    ON cloud_agents.managed_agent_execution_interaction_resolutions TO cloud_agents_runtime
    USING (tenant_id = cloud_agents.require_tenant_id())
    WITH CHECK (tenant_id = cloud_agents.require_tenant_id());
CREATE POLICY managed_agent_execution_interaction_resolutions_migration_owner
    ON cloud_agents.managed_agent_execution_interaction_resolutions TO cloud_agents_migration_owner
    USING (true) WITH CHECK (true);
REVOKE ALL ON TABLE cloud_agents.managed_agent_execution_interaction_resolutions FROM PUBLIC;
REVOKE ALL ON TABLE cloud_agents.managed_agent_execution_interaction_resolutions FROM cloud_agents_bootstrap_admin;
GRANT SELECT ON TABLE cloud_agents.managed_agent_execution_interaction_resolutions TO cloud_agents_runtime;

CREATE FUNCTION cloud_agents.claim_managed_agent_execution_v1(
    p_tenant_id text, p_project_uid text, p_session_uid text, p_turn_uid text,
    p_execution_uid text, p_generation bigint, p_holder text, p_incarnation text,
    p_token text, p_lease_seconds integer
)
RETURNS TABLE (
    acquired boolean, attempt_number bigint, claim_expires_at timestamptz,
    recovery_state text, recovery_reason text, checkpoint_sequence bigint,
    checkpoint_digest text, checkpointed_at timestamptz, checkpoint_protocol text,
    pending_side_effect boolean, pending_interaction_count integer,
    checkpoint_provider_resume_cursor text
)
LANGUAGE plpgsql SECURITY DEFINER SET search_path = pg_catalog, cloud_agents
AS $$
DECLARE
    claimed_at timestamptz := transaction_timestamp();
    execution cloud_agents.managed_agent_executions%ROWTYPE;
    current_target_uid text;
    recovery_mode_value text;
BEGIN
    PERFORM cloud_agents.require_runtime_mutation_principal();
    IF p_tenant_id IS DISTINCT FROM cloud_agents.require_tenant_id()
        OR NOT cloud_agents.is_valid_identifier(p_project_uid)
        OR NOT cloud_agents.is_valid_identifier(p_session_uid)
        OR NOT cloud_agents.is_valid_identifier(p_turn_uid)
        OR NOT cloud_agents.is_valid_identifier(p_execution_uid)
        OR p_generation <= 0
        OR NOT cloud_agents.is_valid_identifier(p_holder)
        OR NOT cloud_agents.is_valid_identifier(p_incarnation)
        OR NOT cloud_agents.is_valid_identifier(p_token)
        OR p_lease_seconds NOT BETWEEN 1 AND 60
    THEN RAISE EXCEPTION USING ERRCODE = '22023', MESSAGE = 'managed agent execution claim input is invalid'; END IF;

    SELECT candidate.* INTO execution
    FROM cloud_agents.managed_agent_executions AS candidate
    WHERE candidate.tenant_id = p_tenant_id AND candidate.project_uid = p_project_uid
        AND candidate.session_uid = p_session_uid AND candidate.turn_uid = p_turn_uid
        AND candidate.execution_uid = p_execution_uid AND candidate.generation = p_generation
    FOR UPDATE;
    IF NOT FOUND THEN RAISE EXCEPTION USING ERRCODE = '23503', MESSAGE = 'managed agent execution is absent'; END IF;
    IF execution.state NOT IN ('queued', 'running') THEN
        RAISE EXCEPTION USING ERRCODE = '23505', MESSAGE = 'managed agent execution is not claimable';
    END IF;
    SELECT COALESCE(volume.target_uid, environment.deployment_target_uid) INTO current_target_uid
    FROM cloud_agents.managed_agent_sessions AS session
    LEFT JOIN cloud_agents.workspace_volumes AS volume
        ON volume.tenant_id = session.tenant_id AND volume.project_uid = session.project_uid
        AND volume.workspace_uid = session.workspace_uid
    LEFT JOIN cloud_agents.managed_host_environment_leases AS environment
        ON environment.tenant_id = session.tenant_id AND environment.project_uid = session.project_uid
        AND environment.lease_uid = session.environment_lease_uid
    WHERE session.tenant_id = p_tenant_id AND session.project_uid = p_project_uid
        AND session.session_uid = p_session_uid;
    IF execution.state = 'running' THEN
        recovery_mode_value := CASE
            WHEN execution.checkpoint_target_uid IS NOT NULL AND current_target_uid IS NOT NULL
                AND execution.checkpoint_target_uid <> current_target_uid THEN 'cross-node-takeover'
            WHEN execution.claim_holder_id = p_holder AND execution.claim_incarnation = p_incarnation
                THEN 'same-node-reconnect'
            ELSE 'process-restart'
        END;
    END IF;
    IF execution.claim_expires_at > claimed_at THEN
        RAISE EXCEPTION USING ERRCODE = '40001', MESSAGE = 'managed agent execution is already claimed';
    END IF;
    IF execution.state = 'running' AND (execution.pending_side_effect OR execution.checkpoint_sequence = 0) THEN
        UPDATE cloud_agents.managed_agent_executions AS candidate
        SET recovery_state = 'awaiting_reconciliation',
            recovery_reason = CASE WHEN execution.pending_side_effect THEN 'side_effect_outcome_unknown' ELSE 'checkpoint_missing' END,
            recovery_mode = recovery_mode_value,
            recovery_source_target_uid = COALESCE(execution.checkpoint_target_uid, current_target_uid),
            recovery_target_uid = current_target_uid,
            claim_holder_id = NULL, claim_incarnation = NULL, claim_token = NULL, claim_expires_at = NULL,
            resource_version = candidate.resource_version + 1, updated_at = claimed_at
        WHERE candidate.tenant_id = p_tenant_id AND candidate.project_uid = p_project_uid
            AND candidate.session_uid = p_session_uid AND candidate.turn_uid = p_turn_uid
            AND candidate.execution_uid = p_execution_uid;
        execution.recovery_state := 'awaiting_reconciliation';
        execution.recovery_reason := CASE WHEN execution.pending_side_effect THEN 'side_effect_outcome_unknown' ELSE 'checkpoint_missing' END;
        acquired := false;
    ELSE
        UPDATE cloud_agents.managed_agent_executions AS candidate
        SET attempt_number = candidate.attempt_number + 1,
            claim_holder_id = p_holder, claim_incarnation = p_incarnation, claim_token = p_token,
            claim_expires_at = claimed_at + pg_catalog.make_interval(secs => p_lease_seconds),
            recovery_state = CASE WHEN candidate.state = 'running' THEN 'recovering' ELSE 'none' END,
            recovery_reason = CASE WHEN candidate.state = 'running' THEN 'claim_expired' ELSE NULL END,
            recovery_mode = CASE WHEN candidate.state = 'running' THEN recovery_mode_value ELSE candidate.recovery_mode END,
            recovery_source_target_uid = CASE WHEN candidate.state = 'running'
                THEN COALESCE(candidate.checkpoint_target_uid, current_target_uid) ELSE candidate.recovery_source_target_uid END,
            recovery_target_uid = CASE WHEN candidate.state = 'running'
                THEN current_target_uid ELSE candidate.recovery_target_uid END,
            resource_version = candidate.resource_version + 1, updated_at = claimed_at
        WHERE candidate.tenant_id = p_tenant_id AND candidate.project_uid = p_project_uid
            AND candidate.session_uid = p_session_uid AND candidate.turn_uid = p_turn_uid
            AND candidate.execution_uid = p_execution_uid
        RETURNING candidate.* INTO execution;
        acquired := true;
    END IF;
    attempt_number := execution.attempt_number;
    claim_expires_at := execution.claim_expires_at;
    recovery_state := execution.recovery_state;
    recovery_reason := execution.recovery_reason;
    checkpoint_sequence := execution.checkpoint_sequence;
    checkpoint_digest := execution.checkpoint_digest;
    checkpointed_at := execution.checkpointed_at;
    checkpoint_protocol := execution.checkpoint_protocol;
    pending_side_effect := execution.pending_side_effect;
    pending_interaction_count := execution.pending_interaction_count;
    checkpoint_provider_resume_cursor := execution.checkpoint_provider_resume_cursor;
    RETURN NEXT;
END;
$$;

CREATE FUNCTION cloud_agents.renew_managed_agent_execution_claim_v1(
    p_tenant_id text, p_project_uid text, p_session_uid text, p_turn_uid text,
    p_execution_uid text, p_generation bigint, p_attempt_number bigint,
    p_holder text, p_incarnation text, p_token text, p_lease_seconds integer
)
RETURNS timestamptz LANGUAGE plpgsql SECURITY DEFINER SET search_path = pg_catalog, cloud_agents
AS $$
DECLARE renewed_at timestamptz := transaction_timestamp(); renewed_expiry timestamptz;
BEGIN
    PERFORM cloud_agents.require_runtime_mutation_principal();
    IF p_tenant_id IS DISTINCT FROM cloud_agents.require_tenant_id() OR p_generation <= 0
        OR p_attempt_number <= 0 OR p_lease_seconds NOT BETWEEN 1 AND 60
    THEN RAISE EXCEPTION USING ERRCODE = '22023', MESSAGE = 'managed agent execution renewal input is invalid'; END IF;
    renewed_expiry := renewed_at + pg_catalog.make_interval(secs => p_lease_seconds);
    UPDATE cloud_agents.managed_agent_executions AS execution
    SET claim_expires_at = renewed_expiry, updated_at = renewed_at
    WHERE execution.tenant_id = p_tenant_id AND execution.project_uid = p_project_uid
        AND execution.session_uid = p_session_uid AND execution.turn_uid = p_turn_uid
        AND execution.execution_uid = p_execution_uid AND execution.generation = p_generation
        AND execution.attempt_number = p_attempt_number AND execution.state IN ('queued', 'running')
        AND execution.claim_holder_id = p_holder AND execution.claim_incarnation = p_incarnation
        AND execution.claim_token = p_token AND execution.claim_expires_at > renewed_at;
    IF NOT FOUND THEN RAISE EXCEPTION USING ERRCODE = '40001', MESSAGE = 'managed agent execution claim is stale'; END IF;
    RETURN renewed_expiry;
END;
$$;

CREATE FUNCTION cloud_agents.release_queued_managed_agent_execution_claim_v1(
    p_tenant_id text, p_project_uid text, p_session_uid text, p_turn_uid text,
    p_execution_uid text, p_generation bigint, p_attempt_number bigint,
    p_holder text, p_incarnation text, p_token text
)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path = pg_catalog, cloud_agents
AS $$
BEGIN
    PERFORM cloud_agents.require_runtime_mutation_principal();
    UPDATE cloud_agents.managed_agent_executions AS execution
    SET claim_holder_id = NULL, claim_incarnation = NULL, claim_token = NULL, claim_expires_at = NULL,
        resource_version = execution.resource_version + 1, updated_at = transaction_timestamp()
    WHERE execution.tenant_id = p_tenant_id AND execution.project_uid = p_project_uid
        AND execution.session_uid = p_session_uid AND execution.turn_uid = p_turn_uid
        AND execution.execution_uid = p_execution_uid AND execution.generation = p_generation
        AND execution.attempt_number = p_attempt_number AND execution.state = 'queued'
        AND execution.claim_holder_id = p_holder AND execution.claim_incarnation = p_incarnation
        AND execution.claim_token = p_token;
    IF NOT FOUND THEN RAISE EXCEPTION USING ERRCODE = '40001', MESSAGE = 'managed agent execution claim is stale'; END IF;
    RETURN true;
END;
$$;

CREATE FUNCTION cloud_agents.start_claimed_managed_agent_execution_v1(
    p_tenant_id text, p_project_uid text, p_session_uid text, p_turn_uid text,
    p_execution_uid text, p_generation bigint, p_idempotency_key text, p_request_digest text,
    p_attempt_number bigint, p_holder text, p_incarnation text, p_token text
)
RETURNS TABLE (
    turn_uid text, turn_state text, turn_resource_version bigint, turn_created_at timestamptz, turn_updated_at timestamptz,
    execution_uid text, execution_generation bigint, execution_state text, result_digest text, error_code text,
    execution_resource_version bigint, execution_created_at timestamptz, execution_updated_at timestamptz
)
LANGUAGE plpgsql SECURITY DEFINER SET search_path = pg_catalog, cloud_agents
AS $$
BEGIN
    PERFORM cloud_agents.require_runtime_mutation_principal();
    PERFORM 1 FROM cloud_agents.managed_agent_executions AS execution
    WHERE execution.tenant_id = p_tenant_id AND execution.project_uid = p_project_uid
        AND execution.session_uid = p_session_uid AND execution.turn_uid = p_turn_uid
        AND execution.execution_uid = p_execution_uid AND execution.generation = p_generation
        AND execution.attempt_number = p_attempt_number AND execution.state = 'queued'
        AND execution.claim_holder_id = p_holder AND execution.claim_incarnation = p_incarnation
        AND execution.claim_token = p_token AND execution.claim_expires_at > transaction_timestamp()
    FOR UPDATE;
    IF NOT FOUND THEN RAISE EXCEPTION USING ERRCODE = '40001', MESSAGE = 'managed agent execution claim is stale'; END IF;
    RETURN QUERY SELECT * FROM cloud_agents.start_managed_agent_execution_v1(
        p_tenant_id, p_project_uid, p_session_uid, p_turn_uid, p_execution_uid,
        p_generation, p_idempotency_key, p_request_digest
    );
END;
$$;

CREATE FUNCTION cloud_agents.checkpoint_managed_agent_execution_v1(
    p_tenant_id text, p_project_uid text, p_session_uid text, p_turn_uid text,
    p_execution_uid text, p_generation bigint, p_attempt_number bigint,
    p_holder text, p_incarnation text, p_token text, p_lease_seconds integer,
    p_runtime_messages text, p_checkpoint_digest text, p_checkpoint_protocol text,
    p_provider_resume_cursor text, p_pending_side_effect boolean, p_pending_interaction_count integer
)
RETURNS TABLE (checkpoint_sequence bigint, claim_expires_at timestamptz, checkpointed_at timestamptz)
LANGUAGE plpgsql SECURITY DEFINER SET search_path = pg_catalog, cloud_agents
AS $$
DECLARE checkpoint_at timestamptz := transaction_timestamp(); execution cloud_agents.managed_agent_executions%ROWTYPE;
BEGIN
    PERFORM cloud_agents.require_runtime_mutation_principal();
    IF p_tenant_id IS DISTINCT FROM cloud_agents.require_tenant_id()
        OR p_attempt_number <= 0 OR p_lease_seconds NOT BETWEEN 1 AND 60
        OR p_runtime_messages IS NULL OR pg_catalog.octet_length(p_runtime_messages) NOT BETWEEN 2 AND 1048576
        OR pg_catalog.jsonb_typeof(p_runtime_messages::pg_catalog.jsonb) IS DISTINCT FROM 'array'
        OR pg_catalog.jsonb_array_length(p_runtime_messages::pg_catalog.jsonb) NOT BETWEEN 1 AND 64
        OR p_checkpoint_digest !~ '^sha256:[0-9a-f]{64}$'
        OR p_checkpoint_protocol !~ '^[a-z0-9][a-z0-9._-]{0,79}$'
        OR p_pending_interaction_count NOT BETWEEN 0 AND 64
        OR p_provider_resume_cursor IS NOT NULL AND (
            pg_catalog.octet_length(p_provider_resume_cursor) NOT BETWEEN 1 AND 4096
            OR p_provider_resume_cursor IS DISTINCT FROM pg_catalog.btrim(p_provider_resume_cursor)
            OR p_provider_resume_cursor ~ '[[:cntrl:]]')
    THEN RAISE EXCEPTION USING ERRCODE = '22023', MESSAGE = 'managed agent execution checkpoint input is invalid'; END IF;
    UPDATE cloud_agents.managed_agent_executions AS candidate
    SET runtime_messages = p_runtime_messages,
        checkpoint_sequence = candidate.checkpoint_sequence + 1,
        checkpoint_digest = p_checkpoint_digest, checkpointed_at = checkpoint_at,
        checkpoint_protocol = p_checkpoint_protocol,
        checkpoint_provider_resume_cursor = COALESCE(p_provider_resume_cursor, candidate.checkpoint_provider_resume_cursor),
        checkpoint_target_uid = COALESCE((
            SELECT COALESCE(volume.target_uid, environment.deployment_target_uid)
            FROM cloud_agents.managed_agent_sessions AS session
            LEFT JOIN cloud_agents.workspace_volumes AS volume
                ON volume.tenant_id = session.tenant_id AND volume.project_uid = session.project_uid
                AND volume.workspace_uid = session.workspace_uid
            LEFT JOIN cloud_agents.managed_host_environment_leases AS environment
                ON environment.tenant_id = session.tenant_id AND environment.project_uid = session.project_uid
                AND environment.lease_uid = session.environment_lease_uid
            WHERE session.tenant_id = p_tenant_id AND session.project_uid = p_project_uid
                AND session.session_uid = p_session_uid
        ), candidate.checkpoint_target_uid),
        pending_side_effect = p_pending_side_effect,
        pending_interaction_count = p_pending_interaction_count,
        side_effect_reconciliation_outcome = CASE WHEN p_pending_side_effect THEN NULL ELSE candidate.side_effect_reconciliation_outcome END,
        side_effect_reconciliation_checkpoint_digest = CASE WHEN p_pending_side_effect THEN NULL ELSE candidate.side_effect_reconciliation_checkpoint_digest END,
        side_effect_reconciliation_digest = CASE WHEN p_pending_side_effect THEN NULL ELSE candidate.side_effect_reconciliation_digest END,
        side_effect_reconciled_at = CASE WHEN p_pending_side_effect THEN NULL ELSE candidate.side_effect_reconciled_at END,
        claim_expires_at = checkpoint_at + pg_catalog.make_interval(secs => p_lease_seconds),
        resource_version = candidate.resource_version + 1, updated_at = checkpoint_at
    WHERE candidate.tenant_id = p_tenant_id AND candidate.project_uid = p_project_uid
        AND candidate.session_uid = p_session_uid AND candidate.turn_uid = p_turn_uid
        AND candidate.execution_uid = p_execution_uid AND candidate.generation = p_generation
        AND candidate.attempt_number = p_attempt_number AND candidate.state = 'running'
        AND candidate.claim_holder_id = p_holder AND candidate.claim_incarnation = p_incarnation
        AND candidate.claim_token = p_token AND candidate.claim_expires_at > checkpoint_at
    RETURNING candidate.* INTO execution;
    IF NOT FOUND THEN RAISE EXCEPTION USING ERRCODE = '40001', MESSAGE = 'managed agent execution claim is stale'; END IF;
    checkpoint_sequence := execution.checkpoint_sequence;
    claim_expires_at := execution.claim_expires_at;
    checkpointed_at := execution.checkpointed_at;
    RETURN NEXT;
END;
$$;

CREATE FUNCTION cloud_agents.resolve_managed_agent_execution_interaction_v1(
    p_tenant_id text, p_project_uid text, p_session_uid text, p_turn_uid text,
    p_execution_uid text, p_generation bigint, p_interaction_request_uid text,
    p_interaction_type text, p_request_uid text, p_resolution_digest text,
    p_resolution_payload text
)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path = pg_catalog, cloud_agents
AS $$
DECLARE execution cloud_agents.managed_agent_executions%ROWTYPE; existing cloud_agents.managed_agent_execution_interaction_resolutions%ROWTYPE;
BEGIN
    PERFORM cloud_agents.require_runtime_mutation_principal();
    IF p_tenant_id IS DISTINCT FROM cloud_agents.require_tenant_id()
        OR p_interaction_type NOT IN ('approval', 'user-input')
        OR pg_catalog.char_length(p_interaction_request_uid) NOT BETWEEN 1 AND 200
        OR p_interaction_request_uid ~ '[[:cntrl:]]'
        OR NOT cloud_agents.is_valid_identifier(p_request_uid)
        OR p_resolution_digest !~ '^sha256:[0-9a-f]{64}$'
        OR pg_catalog.octet_length(p_resolution_payload) NOT BETWEEN 2 AND 65536
        OR pg_catalog.jsonb_typeof(p_resolution_payload::pg_catalog.jsonb) IS DISTINCT FROM 'object'
    THEN RAISE EXCEPTION USING ERRCODE = '22023', MESSAGE = 'managed agent interaction resolution input is invalid'; END IF;
    SELECT candidate.* INTO execution FROM cloud_agents.managed_agent_executions AS candidate
    WHERE candidate.tenant_id = p_tenant_id AND candidate.project_uid = p_project_uid
        AND candidate.session_uid = p_session_uid AND candidate.turn_uid = p_turn_uid
        AND candidate.execution_uid = p_execution_uid AND candidate.generation = p_generation
    FOR UPDATE;
    IF NOT FOUND THEN RAISE EXCEPTION USING ERRCODE = '23503', MESSAGE = 'managed agent execution is absent'; END IF;
    IF execution.state <> 'running' OR execution.runtime_messages IS NULL OR NOT EXISTS (
        SELECT 1 FROM pg_catalog.jsonb_array_elements(execution.runtime_messages::pg_catalog.jsonb) AS message
        WHERE message ->> 'messageType' = 'InteractionRequest'
            AND message -> 'payload' ->> 'requestId' = p_interaction_request_uid
            AND message -> 'payload' ->> 'interactionType' = p_interaction_type
    ) THEN RAISE EXCEPTION USING ERRCODE = '23505', MESSAGE = 'managed agent interaction is not pending'; END IF;
    SELECT resolution.* INTO existing
    FROM cloud_agents.managed_agent_execution_interaction_resolutions AS resolution
    WHERE resolution.tenant_id = p_tenant_id AND resolution.project_uid = p_project_uid
        AND resolution.session_uid = p_session_uid AND resolution.turn_uid = p_turn_uid
        AND resolution.execution_uid = p_execution_uid
        AND resolution.interaction_request_uid = p_interaction_request_uid
    FOR UPDATE;
    IF FOUND THEN
        IF existing.interaction_type IS DISTINCT FROM p_interaction_type
            OR existing.resolution_digest IS DISTINCT FROM p_resolution_digest
            OR existing.resolution_payload IS DISTINCT FROM p_resolution_payload
        THEN RAISE EXCEPTION USING ERRCODE = '23505', MESSAGE = 'managed agent interaction resolution conflicts'; END IF;
        RETURN true;
    END IF;
    INSERT INTO cloud_agents.managed_agent_execution_interaction_resolutions (
        tenant_id, tenant_ref_id, project_uid, session_uid, turn_uid, execution_uid,
        interaction_request_uid, interaction_type, request_uid, resolution_digest,
        resolution_payload, created_at
    ) VALUES (
        p_tenant_id, p_tenant_id, p_project_uid, p_session_uid, p_turn_uid, p_execution_uid,
        p_interaction_request_uid, p_interaction_type, p_request_uid, p_resolution_digest,
        p_resolution_payload, transaction_timestamp()
    );
    UPDATE cloud_agents.managed_agent_executions AS candidate
    SET pending_interaction_count = pg_catalog.greatest(candidate.pending_interaction_count - 1, 0),
        resource_version = candidate.resource_version + 1, updated_at = transaction_timestamp()
    WHERE candidate.tenant_id = p_tenant_id AND candidate.project_uid = p_project_uid
        AND candidate.session_uid = p_session_uid AND candidate.turn_uid = p_turn_uid
        AND candidate.execution_uid = p_execution_uid;
    RETURN true;
END;
$$;

CREATE FUNCTION cloud_agents.reconcile_managed_agent_execution_side_effect_v1(
    p_tenant_id text, p_project_uid text, p_session_uid text, p_turn_uid text,
    p_execution_uid text, p_generation bigint, p_checkpoint_digest text,
    p_outcome text, p_reconciliation_digest text
)
RETURNS timestamptz LANGUAGE plpgsql SECURITY DEFINER SET search_path = pg_catalog, cloud_agents
AS $$
DECLARE execution cloud_agents.managed_agent_executions%ROWTYPE; reconciled_at timestamptz := transaction_timestamp();
BEGIN
    PERFORM cloud_agents.require_runtime_mutation_principal();
    IF p_tenant_id IS DISTINCT FROM cloud_agents.require_tenant_id()
        OR p_generation <= 0
        OR p_checkpoint_digest !~ '^sha256:[0-9a-f]{64}$'
        OR p_outcome NOT IN ('confirmed', 'not-applied')
        OR p_reconciliation_digest !~ '^sha256:[0-9a-f]{64}$'
    THEN RAISE EXCEPTION USING ERRCODE = '22023', MESSAGE = 'managed agent side effect reconciliation input is invalid'; END IF;

    SELECT candidate.* INTO execution
    FROM cloud_agents.managed_agent_executions AS candidate
    WHERE candidate.tenant_id = p_tenant_id AND candidate.project_uid = p_project_uid
        AND candidate.session_uid = p_session_uid AND candidate.turn_uid = p_turn_uid
        AND candidate.execution_uid = p_execution_uid AND candidate.generation = p_generation
    FOR UPDATE;
    IF NOT FOUND THEN RAISE EXCEPTION USING ERRCODE = '23503', MESSAGE = 'managed agent execution is absent'; END IF;
    IF execution.side_effect_reconciliation_outcome IS NOT NULL THEN
        IF execution.side_effect_reconciliation_checkpoint_digest IS DISTINCT FROM p_checkpoint_digest
            OR execution.side_effect_reconciliation_outcome IS DISTINCT FROM p_outcome
            OR execution.side_effect_reconciliation_digest IS DISTINCT FROM p_reconciliation_digest
        THEN RAISE EXCEPTION USING ERRCODE = '23505', MESSAGE = 'managed agent side effect reconciliation conflicts'; END IF;
        RETURN execution.side_effect_reconciled_at;
    END IF;
    IF execution.state <> 'running'
        OR execution.recovery_state <> 'awaiting_reconciliation'
        OR execution.recovery_reason <> 'side_effect_outcome_unknown'
        OR NOT execution.pending_side_effect
        OR execution.checkpoint_digest IS DISTINCT FROM p_checkpoint_digest
        OR execution.claim_expires_at > reconciled_at
    THEN RAISE EXCEPTION USING ERRCODE = '23505', MESSAGE = 'managed agent side effect is not awaiting reconciliation'; END IF;

    UPDATE cloud_agents.managed_agent_executions AS candidate
    SET pending_side_effect = false,
        recovery_state = 'none', recovery_reason = NULL,
        claim_holder_id = NULL, claim_incarnation = NULL, claim_token = NULL, claim_expires_at = NULL,
        side_effect_reconciliation_outcome = p_outcome,
        side_effect_reconciliation_checkpoint_digest = p_checkpoint_digest,
        side_effect_reconciliation_digest = p_reconciliation_digest,
        side_effect_reconciled_at = reconciled_at,
        resource_version = candidate.resource_version + 1, updated_at = reconciled_at
    WHERE candidate.tenant_id = p_tenant_id AND candidate.project_uid = p_project_uid
        AND candidate.session_uid = p_session_uid AND candidate.turn_uid = p_turn_uid
        AND candidate.execution_uid = p_execution_uid;
    RETURN reconciled_at;
END;
$$;

CREATE FUNCTION cloud_agents.settle_claimed_managed_agent_execution_v1(
    p_tenant_id text, p_project_uid text, p_session_uid text, p_turn_uid text,
    p_execution_uid text, p_generation bigint, p_outcome text, p_result_digest text,
    p_error_code text, p_idempotency_key text, p_request_digest text,
    p_provider_resume_cursor text, p_terminal_message text, p_runtime_messages text,
    p_attempt_number bigint, p_holder text, p_incarnation text, p_token text
)
RETURNS TABLE (
    turn_uid text, turn_state text, turn_resource_version bigint, turn_created_at timestamptz, turn_updated_at timestamptz,
    execution_uid text, execution_generation bigint, execution_state text, result_digest text, error_code text,
    execution_resource_version bigint, execution_created_at timestamptz, execution_updated_at timestamptz
)
LANGUAGE plpgsql SECURITY DEFINER SET search_path = pg_catalog, cloud_agents
AS $$
DECLARE settled_recovery_state text;
BEGIN
    PERFORM cloud_agents.require_runtime_mutation_principal();
    SELECT CASE WHEN execution.attempt_number > 1 THEN 'recovered' ELSE 'none' END
    INTO settled_recovery_state
    FROM cloud_agents.managed_agent_executions AS execution
    WHERE execution.tenant_id = p_tenant_id AND execution.project_uid = p_project_uid
        AND execution.session_uid = p_session_uid AND execution.turn_uid = p_turn_uid
        AND execution.execution_uid = p_execution_uid AND execution.generation = p_generation
        AND execution.attempt_number = p_attempt_number AND execution.state = 'running'
        AND execution.claim_holder_id = p_holder AND execution.claim_incarnation = p_incarnation
        AND execution.claim_token = p_token AND execution.claim_expires_at > transaction_timestamp()
    FOR UPDATE;
    IF NOT FOUND THEN RAISE EXCEPTION USING ERRCODE = '40001', MESSAGE = 'managed agent execution claim is stale'; END IF;
    UPDATE cloud_agents.managed_agent_executions AS execution
    SET claim_holder_id = NULL, claim_incarnation = NULL, claim_token = NULL, claim_expires_at = NULL
    WHERE execution.tenant_id = p_tenant_id AND execution.project_uid = p_project_uid
        AND execution.session_uid = p_session_uid AND execution.turn_uid = p_turn_uid
        AND execution.execution_uid = p_execution_uid;
    RETURN QUERY SELECT * FROM cloud_agents.settle_managed_agent_execution_v4(
        p_tenant_id, p_project_uid, p_session_uid, p_turn_uid, p_execution_uid,
        p_generation, p_outcome, p_result_digest, p_error_code, p_idempotency_key,
        p_request_digest, p_provider_resume_cursor, p_terminal_message, p_runtime_messages
    );
    UPDATE cloud_agents.managed_agent_executions AS execution
    SET pending_side_effect = false, pending_interaction_count = 0,
        recovery_state = settled_recovery_state, recovery_reason = NULL
    WHERE execution.tenant_id = p_tenant_id AND execution.project_uid = p_project_uid
        AND execution.session_uid = p_session_uid AND execution.turn_uid = p_turn_uid
        AND execution.execution_uid = p_execution_uid;
END;
$$;

CREATE FUNCTION cloud_agents.cancel_managed_agent_execution_v2(
    p_tenant_id text, p_project_uid text, p_session_uid text, p_turn_uid text,
    p_execution_uid text, p_generation bigint, p_idempotency_key text, p_request_digest text
)
RETURNS TABLE (
    turn_uid text, turn_state text, turn_resource_version bigint, turn_created_at timestamptz, turn_updated_at timestamptz,
    execution_uid text, execution_generation bigint, execution_state text, result_digest text, error_code text,
    execution_resource_version bigint, execution_created_at timestamptz, execution_updated_at timestamptz
)
LANGUAGE plpgsql SECURITY DEFINER SET search_path = pg_catalog, cloud_agents
AS $$
BEGIN
    UPDATE cloud_agents.managed_agent_executions AS execution
    SET claim_holder_id = NULL, claim_incarnation = NULL, claim_token = NULL, claim_expires_at = NULL
    WHERE execution.tenant_id = p_tenant_id AND execution.project_uid = p_project_uid
        AND execution.session_uid = p_session_uid AND execution.turn_uid = p_turn_uid
        AND execution.execution_uid = p_execution_uid AND execution.generation = p_generation
        AND execution.state = 'running';
    RETURN QUERY SELECT * FROM cloud_agents.cancel_managed_agent_execution_v1(
        p_tenant_id, p_project_uid, p_session_uid, p_turn_uid, p_execution_uid,
        p_generation, p_idempotency_key, p_request_digest
    );
    UPDATE cloud_agents.managed_agent_executions AS execution
    SET pending_side_effect = false, pending_interaction_count = 0
    WHERE execution.tenant_id = p_tenant_id AND execution.project_uid = p_project_uid
        AND execution.session_uid = p_session_uid AND execution.turn_uid = p_turn_uid
        AND execution.execution_uid = p_execution_uid;
END;
$$;

CREATE FUNCTION cloud_agents.interrupt_managed_agent_execution_v2(
    p_tenant_id text, p_project_uid text, p_session_uid text, p_turn_uid text,
    p_execution_uid text, p_generation bigint, p_idempotency_key text, p_request_digest text
)
RETURNS TABLE (
    turn_uid text, turn_state text, turn_resource_version bigint, turn_created_at timestamptz, turn_updated_at timestamptz,
    execution_uid text, execution_generation bigint, execution_state text, result_digest text, error_code text,
    execution_resource_version bigint, execution_created_at timestamptz, execution_updated_at timestamptz
)
LANGUAGE plpgsql SECURITY DEFINER SET search_path = pg_catalog, cloud_agents
AS $$
BEGIN
    UPDATE cloud_agents.managed_agent_executions AS execution
    SET claim_holder_id = NULL, claim_incarnation = NULL, claim_token = NULL, claim_expires_at = NULL
    WHERE execution.tenant_id = p_tenant_id AND execution.project_uid = p_project_uid
        AND execution.session_uid = p_session_uid AND execution.turn_uid = p_turn_uid
        AND execution.execution_uid = p_execution_uid AND execution.generation = p_generation
        AND execution.state = 'running';
    RETURN QUERY SELECT * FROM cloud_agents.interrupt_managed_agent_execution_v1(
        p_tenant_id, p_project_uid, p_session_uid, p_turn_uid, p_execution_uid,
        p_generation, p_idempotency_key, p_request_digest
    );
    UPDATE cloud_agents.managed_agent_executions AS execution
    SET pending_side_effect = false, pending_interaction_count = 0
    WHERE execution.tenant_id = p_tenant_id AND execution.project_uid = p_project_uid
        AND execution.session_uid = p_session_uid AND execution.turn_uid = p_turn_uid
        AND execution.execution_uid = p_execution_uid;
END;
$$;

CREATE FUNCTION cloud_agents.rebind_managed_agent_sessions_after_workspace_restore_v1()
RETURNS trigger
LANGUAGE plpgsql SECURITY DEFINER SET search_path = pg_catalog, cloud_agents
AS $$
DECLARE
    restored_source_workspace text;
    restored_source_sandbox text;
    restored_source_generation bigint;
    restored_target text;
BEGIN
    IF NEW.observed_state <> 'running' OR NEW.writer_released
        OR NEW.runtime_uid IS NULL OR NEW.runtime_state <> 'Running'
        OR NEW.runtime_generation IS DISTINCT FROM NEW.generation
        OR NEW.runtime_spec_digest IS DISTINCT FROM NEW.spec_digest
    THEN RETURN NEW; END IF;

    SELECT restore.source_workspace_uid, snapshot.source_sandbox_uid,
        snapshot.source_sandbox_generation, restore.target_uid
    INTO restored_source_workspace, restored_source_sandbox,
        restored_source_generation, restored_target
    FROM cloud_agents.workspace_snapshot_restores AS restore
    JOIN cloud_agents.workspace_snapshots AS snapshot
      ON snapshot.tenant_id = restore.tenant_id
     AND snapshot.project_uid = restore.project_uid
     AND snapshot.snapshot_uid = restore.snapshot_uid
    WHERE restore.tenant_id = NEW.tenant_id
      AND restore.project_uid = NEW.project_uid
      AND restore.operation_id = NEW.operation_id
      AND restore.operation_generation = NEW.operation_generation
      AND restore.workspace_uid = NEW.workspace_uid
      AND restore.sandbox_uid = NEW.sandbox_uid;
    IF NOT FOUND THEN RETURN NEW; END IF;

    IF EXISTS (
        SELECT 1
        FROM cloud_agents.managed_agent_sessions AS session
        WHERE session.tenant_id = NEW.tenant_id
          AND session.project_uid = NEW.project_uid
          AND session.workspace_uid = restored_source_workspace
          AND session.sandbox_uid = restored_source_sandbox
          AND session.sandbox_generation <= restored_source_generation
          AND session.state = 'active'
          AND NOT EXISTS (
              SELECT 1
              FROM cloud_agents.environment_profiles AS profile
              JOIN cloud_agents.worker_releases AS release
                ON release.tenant_id = profile.tenant_id
               AND release.project_uid = profile.project_uid
               AND release.release_digest = profile.release_digest
               AND release.status = 'approved'
               AND release.verification_state = 'attested'
              JOIN cloud_agents.runtime_profiles AS runtime_profile
                ON runtime_profile.tenant_id = NEW.tenant_id
               AND runtime_profile.project_uid = NEW.project_uid
               AND runtime_profile.profile_uid = NEW.runtime_profile_uid
               AND runtime_profile.profile_version = NEW.runtime_profile_version
               AND runtime_profile.status = 'published'
               AND runtime_profile.network_policy_ref = profile.network_policy_ref
              JOIN cloud_agents.deployment_targets AS target
                ON target.tenant_id = NEW.tenant_id
               AND target.project_uid = NEW.project_uid
               AND target.target_uid = restored_target
               AND target.observed_phase = 'ready'
               AND target.scheduling_state = 'active'
              WHERE profile.tenant_id = session.tenant_id
                AND profile.project_uid = session.project_uid
                AND profile.profile_uid = session.environment_profile_uid
                AND profile.profile_version = session.environment_profile_version
                AND profile.status = 'published'
                AND session.provider_kind = ANY(profile.provider_kinds)
                AND restored_target = ANY(profile.target_refs)
                AND NEW.image_uri LIKE '%@' || profile.release_digest
                AND NEW.cpu_millis >= profile.cpu_limit_millis
                AND NEW.memory_bytes >= profile.memory_limit_bytes
          )
    ) THEN
        RAISE EXCEPTION USING ERRCODE = '23505',
            MESSAGE = 'managed agent Session restore target is incompatible';
    END IF;

    UPDATE cloud_agents.managed_agent_sessions AS session
    SET workspace_uid = NEW.workspace_uid,
        sandbox_uid = NEW.sandbox_uid,
        sandbox_generation = NEW.generation,
        resource_version = session.resource_version + 1,
        updated_at = pg_catalog.transaction_timestamp()
    WHERE session.tenant_id = NEW.tenant_id
      AND session.project_uid = NEW.project_uid
      AND session.workspace_uid = restored_source_workspace
      AND session.sandbox_uid = restored_source_sandbox
      AND session.sandbox_generation <= restored_source_generation
      AND session.state = 'active';
    RETURN NEW;
END;
$$;

CREATE TRIGGER sandbox_sessions_rebind_managed_agents_after_restore
AFTER UPDATE OF observed_state, runtime_uid, runtime_state ON cloud_agents.sandbox_sessions
FOR EACH ROW EXECUTE FUNCTION cloud_agents.rebind_managed_agent_sessions_after_workspace_restore_v1();

ALTER FUNCTION cloud_agents.claim_managed_agent_execution_v1(text,text,text,text,text,bigint,text,text,text,integer) OWNER TO cloud_agents_migration_owner;
ALTER FUNCTION cloud_agents.renew_managed_agent_execution_claim_v1(text,text,text,text,text,bigint,bigint,text,text,text,integer) OWNER TO cloud_agents_migration_owner;
ALTER FUNCTION cloud_agents.release_queued_managed_agent_execution_claim_v1(text,text,text,text,text,bigint,bigint,text,text,text) OWNER TO cloud_agents_migration_owner;
ALTER FUNCTION cloud_agents.start_claimed_managed_agent_execution_v1(text,text,text,text,text,bigint,text,text,bigint,text,text,text) OWNER TO cloud_agents_migration_owner;
ALTER FUNCTION cloud_agents.checkpoint_managed_agent_execution_v1(text,text,text,text,text,bigint,bigint,text,text,text,integer,text,text,text,text,boolean,integer) OWNER TO cloud_agents_migration_owner;
ALTER FUNCTION cloud_agents.resolve_managed_agent_execution_interaction_v1(text,text,text,text,text,bigint,text,text,text,text,text) OWNER TO cloud_agents_migration_owner;
ALTER FUNCTION cloud_agents.reconcile_managed_agent_execution_side_effect_v1(text,text,text,text,text,bigint,text,text,text) OWNER TO cloud_agents_migration_owner;
ALTER FUNCTION cloud_agents.settle_claimed_managed_agent_execution_v1(text,text,text,text,text,bigint,text,text,text,text,text,text,text,text,bigint,text,text,text) OWNER TO cloud_agents_migration_owner;
ALTER FUNCTION cloud_agents.cancel_managed_agent_execution_v2(text,text,text,text,text,bigint,text,text) OWNER TO cloud_agents_migration_owner;
ALTER FUNCTION cloud_agents.interrupt_managed_agent_execution_v2(text,text,text,text,text,bigint,text,text) OWNER TO cloud_agents_migration_owner;
ALTER FUNCTION cloud_agents.rebind_managed_agent_sessions_after_workspace_restore_v1() OWNER TO cloud_agents_migration_owner;

REVOKE ALL ON FUNCTION cloud_agents.claim_managed_agent_execution_v1(text,text,text,text,text,bigint,text,text,text,integer) FROM PUBLIC;
REVOKE ALL ON FUNCTION cloud_agents.renew_managed_agent_execution_claim_v1(text,text,text,text,text,bigint,bigint,text,text,text,integer) FROM PUBLIC;
REVOKE ALL ON FUNCTION cloud_agents.release_queued_managed_agent_execution_claim_v1(text,text,text,text,text,bigint,bigint,text,text,text) FROM PUBLIC;
REVOKE ALL ON FUNCTION cloud_agents.start_claimed_managed_agent_execution_v1(text,text,text,text,text,bigint,text,text,bigint,text,text,text) FROM PUBLIC;
REVOKE ALL ON FUNCTION cloud_agents.checkpoint_managed_agent_execution_v1(text,text,text,text,text,bigint,bigint,text,text,text,integer,text,text,text,text,boolean,integer) FROM PUBLIC;
REVOKE ALL ON FUNCTION cloud_agents.resolve_managed_agent_execution_interaction_v1(text,text,text,text,text,bigint,text,text,text,text,text) FROM PUBLIC;
REVOKE ALL ON FUNCTION cloud_agents.reconcile_managed_agent_execution_side_effect_v1(text,text,text,text,text,bigint,text,text,text) FROM PUBLIC;
REVOKE ALL ON FUNCTION cloud_agents.settle_claimed_managed_agent_execution_v1(text,text,text,text,text,bigint,text,text,text,text,text,text,text,text,bigint,text,text,text) FROM PUBLIC;
REVOKE ALL ON FUNCTION cloud_agents.cancel_managed_agent_execution_v2(text,text,text,text,text,bigint,text,text) FROM PUBLIC;
REVOKE ALL ON FUNCTION cloud_agents.interrupt_managed_agent_execution_v2(text,text,text,text,text,bigint,text,text) FROM PUBLIC;
REVOKE ALL ON FUNCTION cloud_agents.rebind_managed_agent_sessions_after_workspace_restore_v1() FROM PUBLIC;

GRANT EXECUTE ON FUNCTION cloud_agents.claim_managed_agent_execution_v1(text,text,text,text,text,bigint,text,text,text,integer) TO cloud_agents_runtime;
GRANT EXECUTE ON FUNCTION cloud_agents.renew_managed_agent_execution_claim_v1(text,text,text,text,text,bigint,bigint,text,text,text,integer) TO cloud_agents_runtime;
GRANT EXECUTE ON FUNCTION cloud_agents.release_queued_managed_agent_execution_claim_v1(text,text,text,text,text,bigint,bigint,text,text,text) TO cloud_agents_runtime;
GRANT EXECUTE ON FUNCTION cloud_agents.start_claimed_managed_agent_execution_v1(text,text,text,text,text,bigint,text,text,bigint,text,text,text) TO cloud_agents_runtime;
GRANT EXECUTE ON FUNCTION cloud_agents.checkpoint_managed_agent_execution_v1(text,text,text,text,text,bigint,bigint,text,text,text,integer,text,text,text,text,boolean,integer) TO cloud_agents_runtime;
GRANT EXECUTE ON FUNCTION cloud_agents.resolve_managed_agent_execution_interaction_v1(text,text,text,text,text,bigint,text,text,text,text,text) TO cloud_agents_runtime;
GRANT EXECUTE ON FUNCTION cloud_agents.reconcile_managed_agent_execution_side_effect_v1(text,text,text,text,text,bigint,text,text,text) TO cloud_agents_runtime;
GRANT EXECUTE ON FUNCTION cloud_agents.settle_claimed_managed_agent_execution_v1(text,text,text,text,text,bigint,text,text,text,text,text,text,text,text,bigint,text,text,text) TO cloud_agents_runtime;
GRANT EXECUTE ON FUNCTION cloud_agents.cancel_managed_agent_execution_v2(text,text,text,text,text,bigint,text,text) TO cloud_agents_runtime;
GRANT EXECUTE ON FUNCTION cloud_agents.interrupt_managed_agent_execution_v2(text,text,text,text,text,bigint,text,text) TO cloud_agents_runtime;
