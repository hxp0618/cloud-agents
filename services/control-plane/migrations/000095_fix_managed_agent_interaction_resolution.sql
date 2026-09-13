-- Keep terminal executions compatible with the runtime_messages state constraint while fixing interaction resolution.

CREATE OR REPLACE FUNCTION cloud_agents.cancel_managed_agent_execution_v1(
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
        RAISE EXCEPTION USING ERRCODE = '22023', MESSAGE = 'managed agent execution cancellation input is invalid';
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
            OR execution_row.state <> 'cancelled'
        THEN
            RAISE EXCEPTION USING ERRCODE = '23505', MESSAGE = 'managed agent execution cancellation idempotency conflict';
        END IF;
        SELECT turn.* INTO turn_row
        FROM cloud_agents.managed_agent_turns AS turn
        WHERE turn.tenant_id = p_tenant_id AND turn.project_uid = p_project_uid
            AND turn.session_uid = p_session_uid AND turn.turn_uid = p_turn_uid;
        IF NOT FOUND OR turn_row.execution_uid <> p_execution_uid OR turn_row.state <> 'cancelled' THEN
            RAISE EXCEPTION USING ERRCODE = '23505', MESSAGE = 'managed agent turn cancellation replay is invalid';
        END IF;
    ELSE
        IF execution_row.generation <> p_generation OR execution_row.state <> 'running' THEN
            RAISE EXCEPTION USING ERRCODE = '23505', MESSAGE = 'managed agent execution cancellation transition is invalid';
        END IF;
        SELECT turn.* INTO turn_row
        FROM cloud_agents.managed_agent_turns AS turn
        WHERE turn.tenant_id = p_tenant_id AND turn.project_uid = p_project_uid
            AND turn.session_uid = p_session_uid AND turn.turn_uid = p_turn_uid
        FOR UPDATE;
        IF NOT FOUND OR turn_row.execution_uid <> p_execution_uid OR turn_row.state <> 'running' THEN
            RAISE EXCEPTION USING ERRCODE = '23505', MESSAGE = 'managed agent turn cancellation transition is invalid';
        END IF;
        UPDATE cloud_agents.managed_agent_executions AS execution
        SET state = 'cancelled', runtime_messages = NULL, result_digest = NULL, error_code = 'cancelled',
            resource_version = execution_row.resource_version + 1,
            terminal_idempotency_key = p_idempotency_key, terminal_request_digest = p_request_digest,
            updated_at = mutation_at
        WHERE execution.tenant_id = p_tenant_id AND execution.project_uid = p_project_uid
            AND execution.session_uid = p_session_uid AND execution.turn_uid = p_turn_uid AND execution.execution_uid = p_execution_uid;
        UPDATE cloud_agents.managed_agent_turns AS turn
        SET state = 'cancelled', resource_version = turn_row.resource_version + 1, updated_at = mutation_at
        WHERE turn.tenant_id = p_tenant_id AND turn.project_uid = p_project_uid
            AND turn.session_uid = p_session_uid AND turn.turn_uid = p_turn_uid;
        execution_row.state := 'cancelled';
        execution_row.result_digest := NULL;
        execution_row.error_code := 'cancelled';
        execution_row.resource_version := execution_row.resource_version + 1;
        execution_row.terminal_idempotency_key := p_idempotency_key;
        execution_row.terminal_request_digest := p_request_digest;
        execution_row.updated_at := mutation_at;
        turn_row.state := 'cancelled';
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

CREATE OR REPLACE FUNCTION cloud_agents.interrupt_managed_agent_execution_v1(
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
        SET state = 'cancelled', runtime_messages = NULL, result_digest = NULL, error_code = 'interrupted',
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

CREATE OR REPLACE FUNCTION cloud_agents.resolve_managed_agent_execution_interaction_v1(
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
    SET pending_interaction_count = GREATEST(candidate.pending_interaction_count - 1, 0),
        resource_version = candidate.resource_version + 1, updated_at = transaction_timestamp()
    WHERE candidate.tenant_id = p_tenant_id AND candidate.project_uid = p_project_uid
        AND candidate.session_uid = p_session_uid AND candidate.turn_uid = p_turn_uid
        AND candidate.execution_uid = p_execution_uid;
    RETURN true;
END;
$$;
