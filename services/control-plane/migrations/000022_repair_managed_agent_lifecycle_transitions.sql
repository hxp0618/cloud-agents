-- Repair the durable Managed Agent lifecycle without rewriting historical
-- migrations.  The qualified UPDATE targets avoid PL/pgSQL output-column
-- name conflicts, and settlement replay reloads the Turn projection.
CREATE OR REPLACE FUNCTION cloud_agents.close_managed_agent_session_v1(
    p_tenant_id text,
    p_project_uid text,
    p_session_uid text,
    p_idempotency_key text,
    p_request_digest text
)
RETURNS TABLE (
    session_uid text,
    provider_kind text,
    state text,
    resource_version bigint,
    created_at timestamptz,
    updated_at timestamptz
)
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, cloud_agents
AS $cloud_agents_function$
DECLARE
    actor_principal text;
    mutation_at timestamptz;
    existing cloud_agents.managed_agent_sessions%ROWTYPE;
BEGIN
    actor_principal := cloud_agents.require_runtime_mutation_principal();
    mutation_at := pg_catalog.transaction_timestamp();
    IF p_tenant_id IS DISTINCT FROM cloud_agents.require_tenant_id()
        OR NOT cloud_agents.is_valid_identifier(p_project_uid)
        OR NOT cloud_agents.is_valid_identifier(p_session_uid)
        OR p_idempotency_key !~ '^[A-Za-z0-9._~-]{16,128}$'
        OR p_request_digest !~ '^sha256:[0-9a-f]{64}$'
    THEN
        RAISE EXCEPTION USING ERRCODE = '22023', MESSAGE = 'managed agent session input is invalid';
    END IF;

    SELECT session.* INTO existing
    FROM cloud_agents.managed_agent_sessions AS session
    WHERE session.tenant_id = p_tenant_id
        AND session.project_uid = p_project_uid
        AND session.session_uid = p_session_uid
    FOR UPDATE;
    IF NOT FOUND THEN
        RETURN;
    END IF;
    IF existing.close_idempotency_key IS NOT NULL THEN
        IF existing.close_idempotency_key <> p_idempotency_key
            OR existing.close_request_digest IS DISTINCT FROM p_request_digest
        THEN
            RAISE EXCEPTION USING ERRCODE = '23505', MESSAGE = 'managed agent session close idempotency conflict';
        END IF;
        session_uid := existing.session_uid;
        provider_kind := existing.provider_kind;
        state := existing.state;
        resource_version := existing.resource_version;
        created_at := existing.created_at;
        updated_at := existing.updated_at;
        RETURN NEXT;
        RETURN;
    END IF;
    IF existing.state <> 'active' THEN
        RAISE EXCEPTION USING ERRCODE = '23505', MESSAGE = 'managed agent session transition is invalid';
    END IF;
    IF EXISTS (
        SELECT 1
        FROM cloud_agents.managed_agent_turns AS turn
        WHERE turn.tenant_id = p_tenant_id
            AND turn.project_uid = p_project_uid
            AND turn.session_uid = p_session_uid
            AND turn.state IN ('queued', 'running')
    ) THEN
        RAISE EXCEPTION USING ERRCODE = '23505', MESSAGE = 'managed agent session has an active turn';
    END IF;

    UPDATE cloud_agents.managed_agent_sessions AS session
    SET state = 'closed', resource_version = existing.resource_version + 1,
        close_idempotency_key = p_idempotency_key,
        close_request_digest = p_request_digest, updated_at = mutation_at
    WHERE session.tenant_id = p_tenant_id
        AND session.project_uid = p_project_uid
        AND session.session_uid = p_session_uid;
    session_uid := existing.session_uid;
    provider_kind := existing.provider_kind;
    state := 'closed';
    resource_version := existing.resource_version + 1;
    created_at := existing.created_at;
    updated_at := mutation_at;
    RETURN NEXT;
END;
$cloud_agents_function$;

CREATE OR REPLACE FUNCTION cloud_agents.start_managed_agent_execution_v1(
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
        OR p_generation <= 0
        OR p_idempotency_key !~ '^[A-Za-z0-9._~-]{16,128}$'
        OR p_request_digest !~ '^sha256:[0-9a-f]{64}$'
    THEN
        RAISE EXCEPTION USING ERRCODE = '22023', MESSAGE = 'managed agent execution input is invalid';
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
    IF execution_row.start_idempotency_key IS NOT NULL THEN
        IF execution_row.start_idempotency_key <> p_idempotency_key
            OR execution_row.start_request_digest IS DISTINCT FROM p_request_digest
        THEN
            RAISE EXCEPTION USING ERRCODE = '23505', MESSAGE = 'managed agent execution start idempotency conflict';
        END IF;
    ELSE
        IF execution_row.generation <> p_generation OR execution_row.state <> 'queued' THEN
            RAISE EXCEPTION USING ERRCODE = '23505', MESSAGE = 'managed agent execution start transition is invalid';
        END IF;
        SELECT turn.* INTO turn_row
        FROM cloud_agents.managed_agent_turns AS turn
        WHERE turn.tenant_id = p_tenant_id AND turn.project_uid = p_project_uid
            AND turn.session_uid = p_session_uid AND turn.turn_uid = p_turn_uid
        FOR UPDATE;
        IF NOT FOUND OR turn_row.execution_uid <> p_execution_uid OR turn_row.state <> 'queued' THEN
            RAISE EXCEPTION USING ERRCODE = '23505', MESSAGE = 'managed agent turn start transition is invalid';
        END IF;
        UPDATE cloud_agents.managed_agent_executions AS execution
        SET state = 'running', resource_version = execution_row.resource_version + 1,
            start_idempotency_key = p_idempotency_key, start_request_digest = p_request_digest,
            updated_at = mutation_at
        WHERE execution.tenant_id = p_tenant_id
            AND execution.project_uid = p_project_uid
            AND execution.session_uid = p_session_uid
            AND execution.turn_uid = p_turn_uid
            AND execution.execution_uid = p_execution_uid;
        UPDATE cloud_agents.managed_agent_turns AS turn
        SET state = 'running', resource_version = turn_row.resource_version + 1, updated_at = mutation_at
        WHERE turn.tenant_id = p_tenant_id
            AND turn.project_uid = p_project_uid
            AND turn.session_uid = p_session_uid
            AND turn.turn_uid = p_turn_uid;
        execution_row.state := 'running';
        execution_row.resource_version := execution_row.resource_version + 1;
        execution_row.start_idempotency_key := p_idempotency_key;
        execution_row.start_request_digest := p_request_digest;
        execution_row.updated_at := mutation_at;
        turn_row.state := 'running';
        turn_row.resource_version := turn_row.resource_version + 1;
        turn_row.updated_at := mutation_at;
    END IF;
    SELECT turn.* INTO turn_row
    FROM cloud_agents.managed_agent_turns AS turn
    WHERE turn.tenant_id = p_tenant_id AND turn.project_uid = p_project_uid
        AND turn.session_uid = p_session_uid AND turn.turn_uid = p_turn_uid;
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

CREATE OR REPLACE FUNCTION cloud_agents.settle_managed_agent_execution_v1(
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
        OR p_generation <= 0
        OR p_outcome NOT IN ('succeeded', 'failed')
        OR (p_outcome = 'succeeded' AND (p_result_digest !~ '^sha256:[0-9a-f]{64}$' OR p_error_code IS NOT NULL))
        OR (p_outcome = 'failed' AND (p_result_digest IS NOT NULL OR p_error_code !~ '^[a-z0-9_-]{1,64}$'))
        OR p_idempotency_key !~ '^[A-Za-z0-9._~-]{16,128}$'
        OR p_request_digest !~ '^sha256:[0-9a-f]{64}$'
    THEN
        RAISE EXCEPTION USING ERRCODE = '22023', MESSAGE = 'managed agent execution settlement input is invalid';
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
        THEN
            RAISE EXCEPTION USING ERRCODE = '23505', MESSAGE = 'managed agent execution settlement idempotency conflict';
        END IF;
        IF p_outcome = 'succeeded' THEN
            IF execution_row.state <> 'succeeded'
                OR execution_row.result_digest IS DISTINCT FROM p_result_digest
                OR execution_row.error_code IS DISTINCT FROM p_error_code
            THEN
                RAISE EXCEPTION USING ERRCODE = '23505', MESSAGE = 'managed agent execution settlement replay is invalid';
            END IF;
        ELSIF execution_row.state <> 'failed'
            OR execution_row.result_digest IS DISTINCT FROM p_result_digest
            OR execution_row.error_code IS DISTINCT FROM p_error_code
        THEN
            RAISE EXCEPTION USING ERRCODE = '23505', MESSAGE = 'managed agent execution settlement replay is invalid';
        END IF;
        SELECT turn.* INTO turn_row
        FROM cloud_agents.managed_agent_turns AS turn
        WHERE turn.tenant_id = p_tenant_id AND turn.project_uid = p_project_uid
            AND turn.session_uid = p_session_uid AND turn.turn_uid = p_turn_uid;
        IF NOT FOUND
            OR turn_row.execution_uid IS DISTINCT FROM p_execution_uid
            OR (p_outcome = 'succeeded' AND turn_row.state <> 'completed')
            OR (p_outcome = 'failed' AND turn_row.state <> 'failed')
        THEN
            RAISE EXCEPTION USING ERRCODE = '23505', MESSAGE = 'managed agent turn settlement replay is invalid';
        END IF;
    ELSE
        IF execution_row.generation <> p_generation OR execution_row.state <> 'running' THEN
            RAISE EXCEPTION USING ERRCODE = '23505', MESSAGE = 'managed agent execution settlement transition is invalid';
        END IF;
        SELECT turn.* INTO turn_row
        FROM cloud_agents.managed_agent_turns AS turn
        WHERE turn.tenant_id = p_tenant_id AND turn.project_uid = p_project_uid
            AND turn.session_uid = p_session_uid AND turn.turn_uid = p_turn_uid
        FOR UPDATE;
        IF NOT FOUND OR turn_row.execution_uid <> p_execution_uid OR turn_row.state <> 'running' THEN
            RAISE EXCEPTION USING ERRCODE = '23505', MESSAGE = 'managed agent turn settlement transition is invalid';
        END IF;
        IF p_outcome = 'succeeded' THEN
            UPDATE cloud_agents.managed_agent_executions AS execution
            SET state = 'succeeded', result_digest = p_result_digest, error_code = NULL,
                resource_version = execution_row.resource_version + 1,
                terminal_idempotency_key = p_idempotency_key, terminal_request_digest = p_request_digest,
                updated_at = mutation_at
            WHERE execution.tenant_id = p_tenant_id
                AND execution.project_uid = p_project_uid
                AND execution.session_uid = p_session_uid
                AND execution.turn_uid = p_turn_uid
                AND execution.execution_uid = p_execution_uid;
            UPDATE cloud_agents.managed_agent_turns AS turn
            SET state = 'completed', resource_version = turn_row.resource_version + 1, updated_at = mutation_at
            WHERE turn.tenant_id = p_tenant_id
                AND turn.project_uid = p_project_uid
                AND turn.session_uid = p_session_uid
                AND turn.turn_uid = p_turn_uid;
            execution_row.state := 'succeeded';
            turn_row.state := 'completed';
        ELSE
            UPDATE cloud_agents.managed_agent_executions AS execution
            SET state = 'failed', result_digest = NULL, error_code = p_error_code,
                resource_version = execution_row.resource_version + 1,
                terminal_idempotency_key = p_idempotency_key, terminal_request_digest = p_request_digest,
                updated_at = mutation_at
            WHERE execution.tenant_id = p_tenant_id
                AND execution.project_uid = p_project_uid
                AND execution.session_uid = p_session_uid
                AND execution.turn_uid = p_turn_uid
                AND execution.execution_uid = p_execution_uid;
            UPDATE cloud_agents.managed_agent_turns AS turn
            SET state = 'failed', resource_version = turn_row.resource_version + 1, updated_at = mutation_at
            WHERE turn.tenant_id = p_tenant_id
                AND turn.project_uid = p_project_uid
                AND turn.session_uid = p_session_uid
                AND turn.turn_uid = p_turn_uid;
            execution_row.state := 'failed';
            turn_row.state := 'failed';
        END IF;
        execution_row.result_digest := p_result_digest;
        execution_row.error_code := p_error_code;
        execution_row.resource_version := execution_row.resource_version + 1;
        execution_row.terminal_idempotency_key := p_idempotency_key;
        execution_row.terminal_request_digest := p_request_digest;
        execution_row.updated_at := mutation_at;
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
        OR p_operation IS NULL OR p_operation NOT IN ('session.create', 'session.close', 'turn.create', 'execution.create', 'execution.start', 'execution.complete', 'execution.fail', 'turn.interrupt', 'turn.cancel')
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
    event_value := 'managed-agent-event-' || sequence_value;
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

ALTER FUNCTION cloud_agents.close_managed_agent_session_v1(text, text, text, text, text)
    OWNER TO cloud_agents_migration_owner;
ALTER FUNCTION cloud_agents.start_managed_agent_execution_v1(text, text, text, text, text, bigint, text, text)
    OWNER TO cloud_agents_migration_owner;
ALTER FUNCTION cloud_agents.settle_managed_agent_execution_v1(text, text, text, text, text, bigint, text, text, text, text, text)
    OWNER TO cloud_agents_migration_owner;

REVOKE ALL ON FUNCTION cloud_agents.close_managed_agent_session_v1(text, text, text, text, text) FROM PUBLIC;
REVOKE ALL ON FUNCTION cloud_agents.start_managed_agent_execution_v1(text, text, text, text, text, bigint, text, text) FROM PUBLIC;
REVOKE ALL ON FUNCTION cloud_agents.settle_managed_agent_execution_v1(text, text, text, text, text, bigint, text, text, text, text, text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION cloud_agents.close_managed_agent_session_v1(text, text, text, text, text) TO cloud_agents_runtime;
GRANT EXECUTE ON FUNCTION cloud_agents.start_managed_agent_execution_v1(text, text, text, text, text, bigint, text, text) TO cloud_agents_runtime;
GRANT EXECUTE ON FUNCTION cloud_agents.settle_managed_agent_execution_v1(text, text, text, text, text, bigint, text, text, text, text, text) TO cloud_agents_runtime;
GRANT EXECUTE ON FUNCTION cloud_agents.append_managed_agent_event_v1(text, text, text, text, text, text, text, bigint, text, text, text, text, jsonb) TO cloud_agents_runtime;
