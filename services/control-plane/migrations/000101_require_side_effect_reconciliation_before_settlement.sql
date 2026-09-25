-- Unknown external side effects must be reconciled before any terminal transition.

CREATE OR REPLACE FUNCTION cloud_agents.settle_claimed_managed_agent_execution_v1(
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
DECLARE settled_recovery_state text; pending_side_effect boolean;
BEGIN
    PERFORM cloud_agents.require_runtime_mutation_principal();
    IF p_tenant_id IS DISTINCT FROM cloud_agents.require_tenant_id() THEN
        RAISE EXCEPTION USING ERRCODE = '22023', MESSAGE = 'managed agent execution settlement input is invalid';
    END IF;
    SELECT CASE WHEN execution.attempt_number > 1 THEN 'recovered' ELSE 'none' END,
        execution.pending_side_effect
    INTO settled_recovery_state, pending_side_effect
    FROM cloud_agents.managed_agent_executions AS execution
    WHERE execution.tenant_id = p_tenant_id AND execution.project_uid = p_project_uid
        AND execution.session_uid = p_session_uid AND execution.turn_uid = p_turn_uid
        AND execution.execution_uid = p_execution_uid AND execution.generation = p_generation
        AND execution.attempt_number = p_attempt_number AND execution.state = 'running'
        AND execution.claim_holder_id = p_holder AND execution.claim_incarnation = p_incarnation
        AND execution.claim_token = p_token AND execution.claim_expires_at > transaction_timestamp()
    FOR UPDATE;
    IF NOT FOUND THEN RAISE EXCEPTION USING ERRCODE = '40001', MESSAGE = 'managed agent execution claim is stale'; END IF;
    IF pending_side_effect THEN
        RAISE EXCEPTION USING ERRCODE = '23505', MESSAGE = 'managed agent side effect requires reconciliation';
    END IF;
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

CREATE OR REPLACE FUNCTION cloud_agents.cancel_managed_agent_execution_v2(
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
DECLARE stored_generation bigint; pending_side_effect boolean;
BEGIN
    PERFORM cloud_agents.require_runtime_mutation_principal();
    IF p_tenant_id IS DISTINCT FROM cloud_agents.require_tenant_id() THEN
        RAISE EXCEPTION USING ERRCODE = '22023', MESSAGE = 'managed agent execution cancellation input is invalid';
    END IF;
    SELECT execution.generation, execution.pending_side_effect
    INTO stored_generation, pending_side_effect
    FROM cloud_agents.managed_agent_executions AS execution
    WHERE execution.tenant_id = p_tenant_id AND execution.project_uid = p_project_uid
        AND execution.session_uid = p_session_uid AND execution.turn_uid = p_turn_uid
        AND execution.execution_uid = p_execution_uid
    FOR UPDATE;
    IF NOT FOUND THEN
        RAISE EXCEPTION USING ERRCODE = '23503', MESSAGE = 'managed agent execution is absent';
    END IF;
    IF stored_generation IS DISTINCT FROM p_generation THEN
        RAISE EXCEPTION USING ERRCODE = '23505', MESSAGE = 'managed agent execution generation is stale';
    END IF;
    IF pending_side_effect THEN
        RAISE EXCEPTION USING ERRCODE = '23505', MESSAGE = 'managed agent side effect requires reconciliation';
    END IF;
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

CREATE OR REPLACE FUNCTION cloud_agents.interrupt_managed_agent_execution_v2(
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
DECLARE stored_generation bigint; pending_side_effect boolean;
BEGIN
    PERFORM cloud_agents.require_runtime_mutation_principal();
    IF p_tenant_id IS DISTINCT FROM cloud_agents.require_tenant_id() THEN
        RAISE EXCEPTION USING ERRCODE = '22023', MESSAGE = 'managed agent execution interruption input is invalid';
    END IF;
    SELECT execution.generation, execution.pending_side_effect
    INTO stored_generation, pending_side_effect
    FROM cloud_agents.managed_agent_executions AS execution
    WHERE execution.tenant_id = p_tenant_id AND execution.project_uid = p_project_uid
        AND execution.session_uid = p_session_uid AND execution.turn_uid = p_turn_uid
        AND execution.execution_uid = p_execution_uid
    FOR UPDATE;
    IF NOT FOUND THEN
        RAISE EXCEPTION USING ERRCODE = '23503', MESSAGE = 'managed agent execution is absent';
    END IF;
    IF stored_generation IS DISTINCT FROM p_generation THEN
        RAISE EXCEPTION USING ERRCODE = '23505', MESSAGE = 'managed agent execution generation is stale';
    END IF;
    IF pending_side_effect THEN
        RAISE EXCEPTION USING ERRCODE = '23505', MESSAGE = 'managed agent side effect requires reconciliation';
    END IF;
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

REVOKE EXECUTE ON FUNCTION cloud_agents.settle_managed_agent_execution_v1(text,text,text,text,text,bigint,text,text,text,text,text) FROM cloud_agents_runtime;
REVOKE EXECUTE ON FUNCTION cloud_agents.settle_managed_agent_execution_v2(text,text,text,text,text,bigint,text,text,text,text,text,text) FROM cloud_agents_runtime;
REVOKE EXECUTE ON FUNCTION cloud_agents.settle_managed_agent_execution_v3(text,text,text,text,text,bigint,text,text,text,text,text,text,text) FROM cloud_agents_runtime;
REVOKE EXECUTE ON FUNCTION cloud_agents.settle_managed_agent_execution_v4(text,text,text,text,text,bigint,text,text,text,text,text,text,text,text) FROM cloud_agents_runtime;
REVOKE EXECUTE ON FUNCTION cloud_agents.cancel_managed_agent_execution_v1(text,text,text,text,text,bigint,text,text) FROM cloud_agents_runtime;
REVOKE EXECUTE ON FUNCTION cloud_agents.interrupt_managed_agent_execution_v1(text,text,text,text,text,bigint,text,text) FROM cloud_agents_runtime;
