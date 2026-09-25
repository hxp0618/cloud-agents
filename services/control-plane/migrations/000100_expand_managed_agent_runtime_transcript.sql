ALTER TABLE cloud_agents.managed_agent_executions
    DROP CONSTRAINT managed_agent_executions_runtime_messages;

ALTER TABLE cloud_agents.managed_agent_executions
    ADD CONSTRAINT managed_agent_executions_runtime_messages
        CHECK (
            runtime_messages IS NULL
            OR state IN ('running', 'succeeded', 'failed')
                AND pg_catalog.octet_length(runtime_messages) BETWEEN 2 AND 1048576
                AND pg_catalog.jsonb_typeof(runtime_messages::pg_catalog.jsonb) = 'array'
                AND pg_catalog.jsonb_array_length(runtime_messages::pg_catalog.jsonb) BETWEEN 1 AND 128
        );

CREATE OR REPLACE FUNCTION cloud_agents.checkpoint_managed_agent_execution_v1(
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
        OR pg_catalog.jsonb_array_length(p_runtime_messages::pg_catalog.jsonb) NOT BETWEEN 1 AND 128
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

CREATE OR REPLACE FUNCTION cloud_agents.settle_managed_agent_execution_v4(
    p_tenant_id text,
    p_project_uid text,
    p_session_uid text,
    p_turn_uid text,
    p_execution_uid text,
    p_generation bigint,
    p_outcome text,
    p_result_digest text,
    p_error_code text,
    p_idempotency_key text,
    p_request_digest text,
    p_provider_resume_cursor text,
    p_terminal_message text,
    p_runtime_messages text
)
RETURNS TABLE (
    turn_uid text,
    turn_state text,
    turn_resource_version bigint,
    turn_created_at timestamptz,
    turn_updated_at timestamptz,
    execution_uid text,
    execution_generation bigint,
    execution_state text,
    result_digest text,
    error_code text,
    execution_resource_version bigint,
    execution_created_at timestamptz,
    execution_updated_at timestamptz
)
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, cloud_agents
AS $cloud_agents_function$
DECLARE
    settlement_replay boolean;
    existing_runtime_messages text;
BEGIN
    IF p_runtime_messages IS NOT NULL AND (
            pg_catalog.octet_length(p_runtime_messages) NOT BETWEEN 2 AND 1048576
            OR pg_catalog.jsonb_typeof(p_runtime_messages::pg_catalog.jsonb) IS DISTINCT FROM 'array'
            OR pg_catalog.jsonb_array_length(p_runtime_messages::pg_catalog.jsonb) NOT BETWEEN 1 AND 128
        )
        OR p_outcome = 'succeeded' AND (
            p_runtime_messages IS NULL
            OR p_terminal_message IS NULL
            OR p_runtime_messages::pg_catalog.jsonb -> -1 IS DISTINCT FROM p_terminal_message::pg_catalog.jsonb
        )
    THEN
        RAISE EXCEPTION USING ERRCODE = '22023', MESSAGE = 'managed agent Runtime messages are invalid';
    END IF;

    SELECT execution.terminal_idempotency_key IS NOT NULL, execution.runtime_messages
    INTO settlement_replay, existing_runtime_messages
    FROM cloud_agents.managed_agent_executions AS execution
    WHERE execution.tenant_id = p_tenant_id
        AND execution.project_uid = p_project_uid
        AND execution.session_uid = p_session_uid
        AND execution.turn_uid = p_turn_uid
        AND execution.execution_uid = p_execution_uid
    FOR UPDATE;

    RETURN QUERY
    SELECT settlement.*
    FROM cloud_agents.settle_managed_agent_execution_v3(
        p_tenant_id,
        p_project_uid,
        p_session_uid,
        p_turn_uid,
        p_execution_uid,
        p_generation,
        p_outcome,
        p_result_digest,
        p_error_code,
        p_idempotency_key,
        p_request_digest,
        p_provider_resume_cursor,
        p_terminal_message
    ) AS settlement;

    IF COALESCE(settlement_replay, false) THEN
        IF existing_runtime_messages IS DISTINCT FROM p_runtime_messages
            AND NOT (p_outcome = 'succeeded' AND existing_runtime_messages IS NULL)
        THEN
            RAISE EXCEPTION USING ERRCODE = '23505', MESSAGE = 'managed agent Runtime messages replay conflicts';
        END IF;
    ELSIF p_runtime_messages IS NOT NULL THEN
        UPDATE cloud_agents.managed_agent_executions AS execution
        SET runtime_messages = p_runtime_messages
        WHERE execution.tenant_id = p_tenant_id
            AND execution.project_uid = p_project_uid
            AND execution.session_uid = p_session_uid
            AND execution.turn_uid = p_turn_uid
            AND execution.execution_uid = p_execution_uid;
        IF NOT FOUND THEN
            RAISE EXCEPTION USING ERRCODE = '23503', MESSAGE = 'managed agent execution is absent';
        END IF;
    END IF;
END;
$cloud_agents_function$;
