ALTER TABLE cloud_agents.managed_agent_sessions
ADD COLUMN provider_resume_cursor text
    CONSTRAINT managed_agent_sessions_provider_resume_cursor_check
    CHECK (
        provider_resume_cursor IS NULL
        OR (
            pg_catalog.octet_length(provider_resume_cursor) BETWEEN 1 AND 4096
            AND provider_resume_cursor = pg_catalog.btrim(provider_resume_cursor)
            AND provider_resume_cursor !~ '[[:cntrl:]]'
        )
    );

CREATE FUNCTION cloud_agents.settle_managed_agent_execution_v2(
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
    p_provider_resume_cursor text
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
BEGIN
    IF p_provider_resume_cursor IS NOT NULL
        AND (
            p_outcome IS DISTINCT FROM 'succeeded'
            OR pg_catalog.octet_length(p_provider_resume_cursor) NOT BETWEEN 1 AND 4096
            OR p_provider_resume_cursor IS DISTINCT FROM pg_catalog.btrim(p_provider_resume_cursor)
            OR p_provider_resume_cursor ~ '[[:cntrl:]]'
        )
    THEN
        RAISE EXCEPTION USING ERRCODE = '22023', MESSAGE = 'managed agent Provider resume cursor is invalid';
    END IF;

    SELECT execution.terminal_idempotency_key IS NOT NULL INTO settlement_replay
    FROM cloud_agents.managed_agent_executions AS execution
    WHERE execution.tenant_id = p_tenant_id
        AND execution.project_uid = p_project_uid
        AND execution.session_uid = p_session_uid
        AND execution.turn_uid = p_turn_uid
        AND execution.execution_uid = p_execution_uid
    FOR UPDATE;

    RETURN QUERY
    SELECT settlement.*
    FROM cloud_agents.settle_managed_agent_execution_v1(
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
        p_request_digest
    ) AS settlement;

    IF p_provider_resume_cursor IS NOT NULL AND NOT COALESCE(settlement_replay, false) THEN
        UPDATE cloud_agents.managed_agent_sessions AS session
        SET provider_resume_cursor = p_provider_resume_cursor
        WHERE session.tenant_id = p_tenant_id
            AND session.project_uid = p_project_uid
            AND session.session_uid = p_session_uid;
        IF NOT FOUND THEN
            RAISE EXCEPTION USING ERRCODE = '23503', MESSAGE = 'managed agent session is absent';
        END IF;
    END IF;
END;
$cloud_agents_function$;

ALTER FUNCTION cloud_agents.settle_managed_agent_execution_v2(text, text, text, text, text, bigint, text, text, text, text, text, text)
    OWNER TO cloud_agents_migration_owner;
REVOKE ALL ON FUNCTION cloud_agents.settle_managed_agent_execution_v2(text, text, text, text, text, bigint, text, text, text, text, text, text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION cloud_agents.settle_managed_agent_execution_v2(text, text, text, text, text, bigint, text, text, text, text, text, text) TO cloud_agents_runtime;
