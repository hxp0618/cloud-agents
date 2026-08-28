-- Durable Managed Agent Execution lifecycle. One Turn owns at most one
-- Execution; the Turn row is locked for every transition.
CREATE TABLE cloud_agents.managed_agent_executions (
    tenant_id text NOT NULL,
    tenant_ref_id text NOT NULL,
    project_uid text NOT NULL,
    session_uid text NOT NULL,
    turn_uid text NOT NULL,
    execution_uid text NOT NULL,
    generation bigint NOT NULL,
    state text NOT NULL,
    result_digest text,
    error_code text,
    resource_version bigint NOT NULL,
    create_idempotency_key text NOT NULL,
    create_request_digest text NOT NULL,
    start_idempotency_key text,
    start_request_digest text,
    terminal_idempotency_key text,
    terminal_request_digest text,
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL,
    PRIMARY KEY (tenant_id, project_uid, session_uid, turn_uid, execution_uid),
    CONSTRAINT managed_agent_executions_tenant_ref
        CHECK (tenant_id = tenant_ref_id),
    CONSTRAINT managed_agent_executions_execution_uid
        CHECK (cloud_agents.is_valid_identifier(execution_uid)),
    CONSTRAINT managed_agent_executions_generation
        CHECK (generation > 0),
    CONSTRAINT managed_agent_executions_state
        CHECK (state IN ('queued', 'running', 'succeeded', 'failed', 'cancelled')),
    CONSTRAINT managed_agent_executions_result_digest
        CHECK (result_digest IS NULL OR result_digest ~ '^sha256:[0-9a-f]{64}$'),
    CONSTRAINT managed_agent_executions_error_code
        CHECK (error_code IS NULL OR error_code ~ '^[a-z0-9_-]{1,64}$'),
    CONSTRAINT managed_agent_executions_resource_version
        CHECK (resource_version > 0),
    CONSTRAINT managed_agent_executions_create_key
        CHECK (create_idempotency_key ~ '^[A-Za-z0-9._~-]{16,128}$'),
    CONSTRAINT managed_agent_executions_create_digest
        CHECK (create_request_digest ~ '^sha256:[0-9a-f]{64}$'),
    CONSTRAINT managed_agent_executions_start_key
        CHECK (start_idempotency_key IS NULL OR start_idempotency_key ~ '^[A-Za-z0-9._~-]{16,128}$'),
    CONSTRAINT managed_agent_executions_start_digest
        CHECK (start_request_digest IS NULL OR start_request_digest ~ '^sha256:[0-9a-f]{64}$'),
    CONSTRAINT managed_agent_executions_terminal_key
        CHECK (terminal_idempotency_key IS NULL OR terminal_idempotency_key ~ '^[A-Za-z0-9._~-]{16,128}$'),
    CONSTRAINT managed_agent_executions_terminal_digest
        CHECK (terminal_request_digest IS NULL OR terminal_request_digest ~ '^sha256:[0-9a-f]{64}$'),
    CONSTRAINT managed_agent_executions_terminal_payload
        CHECK ((state = 'succeeded' AND result_digest IS NOT NULL AND error_code IS NULL) OR
               (state = 'failed' AND result_digest IS NULL AND error_code IS NOT NULL) OR
               (state IN ('queued', 'running', 'cancelled'))),
    CONSTRAINT managed_agent_executions_project_fk
        FOREIGN KEY (tenant_id, project_uid)
        REFERENCES cloud_agents.projects (tenant_id, project_uid)
        ON UPDATE RESTRICT ON DELETE RESTRICT,
    CONSTRAINT managed_agent_executions_session_fk
        FOREIGN KEY (tenant_id, project_uid, session_uid)
        REFERENCES cloud_agents.managed_agent_sessions (tenant_id, project_uid, session_uid)
        ON UPDATE RESTRICT ON DELETE RESTRICT,
    CONSTRAINT managed_agent_executions_turn_fk
        FOREIGN KEY (tenant_id, project_uid, session_uid, turn_uid)
        REFERENCES cloud_agents.managed_agent_turns (tenant_id, project_uid, session_uid, turn_uid)
        ON UPDATE RESTRICT ON DELETE RESTRICT
);

CREATE INDEX managed_agent_executions_create_key_idx
    ON cloud_agents.managed_agent_executions (tenant_id, project_uid, session_uid, create_idempotency_key);

ALTER TABLE cloud_agents.managed_agent_executions OWNER TO cloud_agents_migration_owner;
ALTER TABLE cloud_agents.managed_agent_executions ENABLE ROW LEVEL SECURITY;
ALTER TABLE cloud_agents.managed_agent_executions FORCE ROW LEVEL SECURITY;

CREATE POLICY managed_agent_executions_runtime_tenant
    ON cloud_agents.managed_agent_executions
    TO cloud_agents_runtime
    USING (tenant_id = cloud_agents.require_tenant_id())
    WITH CHECK (tenant_id = cloud_agents.require_tenant_id());

CREATE POLICY managed_agent_executions_migration_owner
    ON cloud_agents.managed_agent_executions
    TO cloud_agents_migration_owner
    USING (true) WITH CHECK (true);

REVOKE ALL ON TABLE cloud_agents.managed_agent_executions FROM PUBLIC;
REVOKE ALL ON TABLE cloud_agents.managed_agent_executions FROM cloud_agents_bootstrap_admin;
GRANT SELECT ON TABLE cloud_agents.managed_agent_executions TO cloud_agents_runtime;

CREATE FUNCTION cloud_agents.create_managed_agent_execution_v1(
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
    execution_uid text,
    generation bigint,
    state text,
    result_digest text,
    error_code text,
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
    existing cloud_agents.managed_agent_executions%ROWTYPE;
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

    SELECT execution.* INTO existing
    FROM cloud_agents.managed_agent_executions AS execution
    WHERE execution.tenant_id = p_tenant_id
        AND execution.project_uid = p_project_uid
        AND execution.session_uid = p_session_uid
        AND execution.create_idempotency_key = p_idempotency_key
    FOR UPDATE;
    IF FOUND THEN
        IF existing.create_request_digest IS DISTINCT FROM p_request_digest THEN
            RAISE EXCEPTION USING ERRCODE = '23505', MESSAGE = 'managed agent execution idempotency conflict';
        END IF;
        execution_uid := existing.execution_uid;
        generation := existing.generation;
        state := existing.state;
        result_digest := existing.result_digest;
        error_code := existing.error_code;
        resource_version := existing.resource_version;
        created_at := existing.created_at;
        updated_at := existing.updated_at;
        RETURN NEXT;
        RETURN;
    END IF;

    PERFORM 1
    FROM cloud_agents.managed_agent_sessions AS session
    WHERE session.tenant_id = p_tenant_id
        AND session.project_uid = p_project_uid
        AND session.session_uid = p_session_uid
        AND session.state = 'active'
    FOR UPDATE;
    IF NOT FOUND THEN
        RAISE EXCEPTION USING ERRCODE = '23503', MESSAGE = 'session is absent or inactive';
    END IF;

    SELECT turn.* INTO turn_row
    FROM cloud_agents.managed_agent_turns AS turn
    WHERE turn.tenant_id = p_tenant_id
        AND turn.project_uid = p_project_uid
        AND turn.session_uid = p_session_uid
        AND turn.turn_uid = p_turn_uid
    FOR UPDATE;
    IF NOT FOUND OR turn_row.state <> 'queued' OR turn_row.execution_uid IS NOT NULL THEN
        RAISE EXCEPTION USING ERRCODE = '23505', MESSAGE = 'managed agent turn is not available for execution';
    END IF;

    INSERT INTO cloud_agents.managed_agent_executions (
        tenant_id, tenant_ref_id, project_uid, session_uid, turn_uid, execution_uid,
        generation, state, resource_version, create_idempotency_key, create_request_digest,
        created_at, updated_at
    ) VALUES (
        p_tenant_id, p_tenant_id, p_project_uid, p_session_uid, p_turn_uid, p_execution_uid,
        p_generation, 'queued', 1, p_idempotency_key, p_request_digest, mutation_at, mutation_at
    );
    UPDATE cloud_agents.managed_agent_turns
    SET execution_uid = p_execution_uid, resource_version = turn_row.resource_version + 1,
        updated_at = mutation_at
    WHERE tenant_id = p_tenant_id AND project_uid = p_project_uid
        AND session_uid = p_session_uid AND turn_uid = p_turn_uid;
    execution_uid := p_execution_uid;
    generation := p_generation;
    state := 'queued';
    result_digest := NULL;
    error_code := NULL;
    resource_version := 1;
    created_at := mutation_at;
    updated_at := mutation_at;
    RETURN NEXT;
END;
$cloud_agents_function$;

CREATE FUNCTION cloud_agents.start_managed_agent_execution_v1(
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
        UPDATE cloud_agents.managed_agent_executions
        SET state = 'running', resource_version = execution_row.resource_version + 1,
            start_idempotency_key = p_idempotency_key, start_request_digest = p_request_digest,
            updated_at = mutation_at
        WHERE tenant_id = p_tenant_id AND project_uid = p_project_uid
            AND session_uid = p_session_uid AND turn_uid = p_turn_uid AND execution_uid = p_execution_uid;
        UPDATE cloud_agents.managed_agent_turns
        SET state = 'running', resource_version = turn_row.resource_version + 1, updated_at = mutation_at
        WHERE tenant_id = p_tenant_id AND project_uid = p_project_uid
            AND session_uid = p_session_uid AND turn_uid = p_turn_uid;
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

CREATE FUNCTION cloud_agents.settle_managed_agent_execution_v1(
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
            UPDATE cloud_agents.managed_agent_executions
            SET state = 'succeeded', result_digest = p_result_digest, error_code = NULL,
                resource_version = execution_row.resource_version + 1,
                terminal_idempotency_key = p_idempotency_key, terminal_request_digest = p_request_digest,
                updated_at = mutation_at
            WHERE tenant_id = p_tenant_id AND project_uid = p_project_uid
                AND session_uid = p_session_uid AND turn_uid = p_turn_uid AND execution_uid = p_execution_uid;
            UPDATE cloud_agents.managed_agent_turns
            SET state = 'completed', resource_version = turn_row.resource_version + 1, updated_at = mutation_at
            WHERE tenant_id = p_tenant_id AND project_uid = p_project_uid
                AND session_uid = p_session_uid AND turn_uid = p_turn_uid;
            execution_row.state := 'succeeded';
            turn_row.state := 'completed';
        ELSE
            UPDATE cloud_agents.managed_agent_executions
            SET state = 'failed', result_digest = NULL, error_code = p_error_code,
                resource_version = execution_row.resource_version + 1,
                terminal_idempotency_key = p_idempotency_key, terminal_request_digest = p_request_digest,
                updated_at = mutation_at
            WHERE tenant_id = p_tenant_id AND project_uid = p_project_uid
                AND session_uid = p_session_uid AND turn_uid = p_turn_uid AND execution_uid = p_execution_uid;
            UPDATE cloud_agents.managed_agent_turns
            SET state = 'failed', resource_version = turn_row.resource_version + 1, updated_at = mutation_at
            WHERE tenant_id = p_tenant_id AND project_uid = p_project_uid
                AND session_uid = p_session_uid AND turn_uid = p_turn_uid;
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

ALTER FUNCTION cloud_agents.create_managed_agent_execution_v1(text, text, text, text, text, bigint, text, text)
    OWNER TO cloud_agents_migration_owner;
ALTER FUNCTION cloud_agents.start_managed_agent_execution_v1(text, text, text, text, text, bigint, text, text)
    OWNER TO cloud_agents_migration_owner;
ALTER FUNCTION cloud_agents.settle_managed_agent_execution_v1(text, text, text, text, text, bigint, text, text, text, text, text)
    OWNER TO cloud_agents_migration_owner;

REVOKE ALL ON FUNCTION cloud_agents.create_managed_agent_execution_v1(text, text, text, text, text, bigint, text, text) FROM PUBLIC;
REVOKE ALL ON FUNCTION cloud_agents.start_managed_agent_execution_v1(text, text, text, text, text, bigint, text, text) FROM PUBLIC;
REVOKE ALL ON FUNCTION cloud_agents.settle_managed_agent_execution_v1(text, text, text, text, text, bigint, text, text, text, text, text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION cloud_agents.create_managed_agent_execution_v1(text, text, text, text, text, bigint, text, text) TO cloud_agents_runtime;
GRANT EXECUTE ON FUNCTION cloud_agents.start_managed_agent_execution_v1(text, text, text, text, text, bigint, text, text) TO cloud_agents_runtime;
GRANT EXECUTE ON FUNCTION cloud_agents.settle_managed_agent_execution_v1(text, text, text, text, text, bigint, text, text, text, text, text) TO cloud_agents_runtime;
