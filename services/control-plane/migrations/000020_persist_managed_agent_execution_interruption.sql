CREATE FUNCTION cloud_agents.interrupt_managed_agent_execution_v1(
    p_tenant_id text,
    p_project_uid text,
    p_session_uid text,
    p_turn_uid text,
    p_execution_uid text,
    p_generation bigint,
    p_idempotency_key text,
    p_request_digest text
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
    actor_principal text;
    mutation_at timestamptz;
    execution_row cloud_agents.managed_agent_executions%ROWTYPE;
    turn_row cloud_agents.managed_agent_turns%ROWTYPE;
BEGIN
    actor_principal := cloud_agents.require_runtime_mutation_principal();
    mutation_at := pg_catalog.transaction_timestamp();
    IF p_tenant_id IS DISTINCT FROM cloud_agents.require_tenant_id()
        OR NOT cloud_agents.is_valid_identifier(p_project_uid)
        OR NOT cloud_agents.is_valid_identifier(p_session_uid)
        OR NOT cloud_agents.is_valid_identifier(p_turn_uid)
        OR NOT cloud_agents.is_valid_identifier(p_execution_uid)
        OR p_generation IS NULL OR p_generation <= 0
        OR p_idempotency_key IS NULL OR p_idempotency_key !~ '^[A-Za-z0-9._~-]{16,128}$'
        OR p_request_digest IS NULL OR p_request_digest !~ '^sha256:[0-9a-f]{64}$'
    THEN
        RAISE EXCEPTION USING ERRCODE = '22023', MESSAGE = 'managed agent execution interruption input is invalid';
    END IF;
    SELECT execution.* INTO execution_row
    FROM cloud_agents.managed_agent_executions AS execution
    WHERE execution.tenant_id = p_tenant_id AND execution.project_uid = p_project_uid
        AND execution.session_uid = p_session_uid AND execution.turn_uid = p_turn_uid
        AND execution.execution_uid = p_execution_uid
    FOR UPDATE;
    IF NOT FOUND THEN
        RAISE EXCEPTION USING ERRCODE = '23503', MESSAGE = 'managed agent execution is absent';
    END IF;
    IF execution_row.terminal_idempotency_key IS NOT NULL THEN
        IF execution_row.terminal_idempotency_key <> p_idempotency_key
            OR execution_row.terminal_request_digest IS DISTINCT FROM p_request_digest
            OR execution_row.state <> 'cancelled' OR execution_row.error_code <> 'interrupted'
        THEN
            RAISE EXCEPTION USING ERRCODE = '23505', MESSAGE = 'managed agent execution interruption idempotency conflict';
        END IF;
        SELECT turn.* INTO turn_row
        FROM cloud_agents.managed_agent_turns AS turn
        WHERE turn.tenant_id = p_tenant_id AND turn.project_uid = p_project_uid
            AND turn.session_uid = p_session_uid AND turn.turn_uid = p_turn_uid;
        IF NOT FOUND OR turn_row.execution_uid <> p_execution_uid OR turn_row.state <> 'interrupted' THEN
            RAISE EXCEPTION USING ERRCODE = '23505', MESSAGE = 'managed agent turn interruption replay is invalid';
        END IF;
    ELSE
        IF execution_row.generation <> p_generation OR execution_row.state <> 'running' THEN
            RAISE EXCEPTION USING ERRCODE = '23505', MESSAGE = 'managed agent execution interruption transition is invalid';
        END IF;
        SELECT turn.* INTO turn_row
        FROM cloud_agents.managed_agent_turns AS turn
        WHERE turn.tenant_id = p_tenant_id AND turn.project_uid = p_project_uid
            AND turn.session_uid = p_session_uid AND turn.turn_uid = p_turn_uid
        FOR UPDATE;
        IF NOT FOUND OR turn_row.execution_uid <> p_execution_uid OR turn_row.state <> 'running' THEN
            RAISE EXCEPTION USING ERRCODE = '23505', MESSAGE = 'managed agent turn interruption transition is invalid';
        END IF;
        UPDATE cloud_agents.managed_agent_executions AS execution
        SET state = 'cancelled', result_digest = NULL, error_code = 'interrupted',
            resource_version = execution_row.resource_version + 1,
            terminal_idempotency_key = p_idempotency_key, terminal_request_digest = p_request_digest,
            updated_at = mutation_at
        WHERE execution.tenant_id = p_tenant_id AND execution.project_uid = p_project_uid
            AND execution.session_uid = p_session_uid AND execution.turn_uid = p_turn_uid AND execution.execution_uid = p_execution_uid;
        UPDATE cloud_agents.managed_agent_turns AS turn
        SET state = 'interrupted', resource_version = turn_row.resource_version + 1, updated_at = mutation_at
        WHERE turn.tenant_id = p_tenant_id AND turn.project_uid = p_project_uid
            AND turn.session_uid = p_session_uid AND turn.turn_uid = p_turn_uid;
        execution_row.state := 'cancelled';
        execution_row.result_digest := NULL;
        execution_row.error_code := 'interrupted';
        execution_row.resource_version := execution_row.resource_version + 1;
        execution_row.terminal_idempotency_key := p_idempotency_key;
        execution_row.terminal_request_digest := p_request_digest;
        execution_row.updated_at := mutation_at;
        turn_row.state := 'interrupted';
        turn_row.resource_version := turn_row.resource_version + 1;
        turn_row.updated_at := mutation_at;
    END IF;
    turn_uid := turn_row.turn_uid;
    turn_state := turn_row.state;
    turn_resource_version := turn_row.resource_version;
    turn_created_at := turn_row.created_at;
    turn_updated_at := turn_row.updated_at;
    execution_uid := execution_row.execution_uid;
    execution_generation := execution_row.generation;
    execution_state := execution_row.state;
    result_digest := execution_row.result_digest;
    error_code := execution_row.error_code;
    execution_resource_version := execution_row.resource_version;
    execution_created_at := execution_row.created_at;
    execution_updated_at := execution_row.updated_at;
    RETURN NEXT;
END;
$cloud_agents_function$;

ALTER FUNCTION cloud_agents.interrupt_managed_agent_execution_v1(text, text, text, text, text, bigint, text, text)
    OWNER TO cloud_agents_migration_owner;
REVOKE ALL ON FUNCTION cloud_agents.interrupt_managed_agent_execution_v1(text, text, text, text, text, bigint, text, text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION cloud_agents.interrupt_managed_agent_execution_v1(text, text, text, text, text, bigint, text, text) TO cloud_agents_runtime;
