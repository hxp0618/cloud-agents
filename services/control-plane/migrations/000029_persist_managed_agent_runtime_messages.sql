ALTER TABLE cloud_agents.managed_agent_executions
ADD COLUMN runtime_messages text;

ALTER TABLE cloud_agents.managed_agent_executions
ADD CONSTRAINT managed_agent_executions_runtime_messages
    CHECK (
        runtime_messages IS NULL
        OR state IN ('succeeded', 'failed')
            AND pg_catalog.octet_length(runtime_messages) BETWEEN 2 AND 1048576
            AND pg_catalog.jsonb_typeof(runtime_messages::pg_catalog.jsonb) = 'array'
            AND pg_catalog.jsonb_array_length(runtime_messages::pg_catalog.jsonb) BETWEEN 1 AND 64
    );

CREATE FUNCTION cloud_agents.settle_managed_agent_execution_v4(
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
            OR pg_catalog.jsonb_array_length(p_runtime_messages::pg_catalog.jsonb) NOT BETWEEN 1 AND 64
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

ALTER FUNCTION cloud_agents.settle_managed_agent_execution_v4(text, text, text, text, text, bigint, text, text, text, text, text, text, text, text)
    OWNER TO cloud_agents_migration_owner;
REVOKE ALL ON FUNCTION cloud_agents.settle_managed_agent_execution_v4(text, text, text, text, text, bigint, text, text, text, text, text, text, text, text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION cloud_agents.settle_managed_agent_execution_v4(text, text, text, text, text, bigint, text, text, text, text, text, text, text, text) TO cloud_agents_runtime;
