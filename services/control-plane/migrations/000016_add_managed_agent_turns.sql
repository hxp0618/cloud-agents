-- Durable Managed Agent Turn lifecycle. A Session may have at most one
-- non-terminal Turn; the database owns that invariant under the session row
-- lock.
CREATE TABLE cloud_agents.managed_agent_turns (
    tenant_id text NOT NULL,
    tenant_ref_id text NOT NULL,
    project_uid text NOT NULL,
    session_uid text NOT NULL,
    turn_uid text NOT NULL,
    input_digest text NOT NULL,
    execution_uid text,
    state text NOT NULL,
    resource_version bigint NOT NULL,
    create_idempotency_key text NOT NULL,
    create_request_digest text NOT NULL,
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL,
    PRIMARY KEY (tenant_id, project_uid, session_uid, turn_uid),
    CONSTRAINT managed_agent_turns_tenant_ref
        CHECK (tenant_id = tenant_ref_id),
    CONSTRAINT managed_agent_turns_turn_uid
        CHECK (cloud_agents.is_valid_identifier(turn_uid)),
    CONSTRAINT managed_agent_turns_input_digest
        CHECK (input_digest ~ '^sha256:[0-9a-f]{64}$'),
    CONSTRAINT managed_agent_turns_execution_uid
        CHECK (execution_uid IS NULL OR cloud_agents.is_valid_identifier(execution_uid)),
    CONSTRAINT managed_agent_turns_state
        CHECK (state IN ('queued', 'running', 'completed', 'failed', 'interrupted', 'cancelled')),
    CONSTRAINT managed_agent_turns_resource_version
        CHECK (resource_version > 0),
    CONSTRAINT managed_agent_turns_create_key
        CHECK (create_idempotency_key ~ '^[A-Za-z0-9._~-]{16,128}$'),
    CONSTRAINT managed_agent_turns_create_digest
        CHECK (create_request_digest ~ '^sha256:[0-9a-f]{64}$'),
    CONSTRAINT managed_agent_turns_project_fk
        FOREIGN KEY (tenant_id, project_uid)
        REFERENCES cloud_agents.projects (tenant_id, project_uid)
        ON UPDATE RESTRICT ON DELETE RESTRICT,
    CONSTRAINT managed_agent_turns_session_fk
        FOREIGN KEY (tenant_id, project_uid, session_uid)
        REFERENCES cloud_agents.managed_agent_sessions (tenant_id, project_uid, session_uid)
        ON UPDATE RESTRICT ON DELETE RESTRICT
);

CREATE INDEX managed_agent_turns_create_key_idx
    ON cloud_agents.managed_agent_turns (tenant_id, project_uid, session_uid, create_idempotency_key);

ALTER TABLE cloud_agents.managed_agent_turns OWNER TO cloud_agents_migration_owner;
ALTER TABLE cloud_agents.managed_agent_turns ENABLE ROW LEVEL SECURITY;
ALTER TABLE cloud_agents.managed_agent_turns FORCE ROW LEVEL SECURITY;

CREATE POLICY managed_agent_turns_runtime_tenant
    ON cloud_agents.managed_agent_turns
    TO cloud_agents_runtime
    USING (tenant_id = cloud_agents.require_tenant_id())
    WITH CHECK (tenant_id = cloud_agents.require_tenant_id());

CREATE POLICY managed_agent_turns_migration_owner
    ON cloud_agents.managed_agent_turns
    TO cloud_agents_migration_owner
    USING (true) WITH CHECK (true);

REVOKE ALL ON TABLE cloud_agents.managed_agent_turns FROM PUBLIC;
REVOKE ALL ON TABLE cloud_agents.managed_agent_turns FROM cloud_agents_bootstrap_admin;
GRANT SELECT ON TABLE cloud_agents.managed_agent_turns TO cloud_agents_runtime;

CREATE FUNCTION cloud_agents.create_managed_agent_turn_v1(
    p_tenant_id text,
    p_project_uid text,
    p_session_uid text,
    p_turn_uid text,
    p_input_digest text,
    p_idempotency_key text,
    p_request_digest text
)
RETURNS TABLE (
    turn_uid text,
    input_digest text,
    execution_uid text,
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
    existing cloud_agents.managed_agent_turns%ROWTYPE;
BEGIN
    actor_principal := cloud_agents.require_runtime_mutation_principal();
    mutation_at := pg_catalog.transaction_timestamp();
    IF p_tenant_id IS DISTINCT FROM cloud_agents.require_tenant_id()
        OR NOT cloud_agents.is_valid_identifier(p_project_uid)
        OR NOT cloud_agents.is_valid_identifier(p_session_uid)
        OR NOT cloud_agents.is_valid_identifier(p_turn_uid)
        OR p_input_digest !~ '^sha256:[0-9a-f]{64}$'
        OR p_idempotency_key !~ '^[A-Za-z0-9._~-]{16,128}$'
        OR p_request_digest !~ '^sha256:[0-9a-f]{64}$'
    THEN
        RAISE EXCEPTION USING ERRCODE = '22023', MESSAGE = 'managed agent turn input is invalid';
    END IF;

    SELECT turn.* INTO existing
    FROM cloud_agents.managed_agent_turns AS turn
    WHERE turn.tenant_id = p_tenant_id
        AND turn.project_uid = p_project_uid
        AND turn.session_uid = p_session_uid
        AND turn.create_idempotency_key = p_idempotency_key
    FOR UPDATE;
    IF FOUND THEN
        IF existing.create_request_digest IS DISTINCT FROM p_request_digest THEN
            RAISE EXCEPTION USING ERRCODE = '23505', MESSAGE = 'managed agent turn idempotency conflict';
        END IF;
        turn_uid := existing.turn_uid;
        input_digest := existing.input_digest;
        execution_uid := existing.execution_uid;
        state := existing.state;
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

    INSERT INTO cloud_agents.managed_agent_turns (
        tenant_id, tenant_ref_id, project_uid, session_uid, turn_uid,
        input_digest, state, resource_version, create_idempotency_key,
        create_request_digest, created_at, updated_at
    ) VALUES (
        p_tenant_id, p_tenant_id, p_project_uid, p_session_uid, p_turn_uid,
        p_input_digest, 'queued', 1, p_idempotency_key, p_request_digest,
        mutation_at, mutation_at
    );
    turn_uid := p_turn_uid;
    input_digest := p_input_digest;
    execution_uid := NULL;
    state := 'queued';
    resource_version := 1;
    created_at := mutation_at;
    updated_at := mutation_at;
    RETURN NEXT;
END;
$cloud_agents_function$;

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

    UPDATE cloud_agents.managed_agent_sessions
    SET state = 'closed', resource_version = existing.resource_version + 1,
        close_idempotency_key = p_idempotency_key,
        close_request_digest = p_request_digest, updated_at = mutation_at
    WHERE tenant_id = p_tenant_id AND project_uid = p_project_uid AND session_uid = p_session_uid;
    session_uid := existing.session_uid;
    provider_kind := existing.provider_kind;
    state := 'closed';
    resource_version := existing.resource_version + 1;
    created_at := existing.created_at;
    updated_at := mutation_at;
    RETURN NEXT;
END;
$cloud_agents_function$;

ALTER FUNCTION cloud_agents.create_managed_agent_turn_v1(text, text, text, text, text, text, text)
    OWNER TO cloud_agents_migration_owner;
ALTER FUNCTION cloud_agents.close_managed_agent_session_v1(text, text, text, text, text)
    OWNER TO cloud_agents_migration_owner;
REVOKE ALL ON FUNCTION cloud_agents.create_managed_agent_turn_v1(text, text, text, text, text, text, text) FROM PUBLIC;
REVOKE ALL ON FUNCTION cloud_agents.close_managed_agent_session_v1(text, text, text, text, text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION cloud_agents.create_managed_agent_turn_v1(text, text, text, text, text, text, text) TO cloud_agents_runtime;
GRANT EXECUTE ON FUNCTION cloud_agents.close_managed_agent_session_v1(text, text, text, text, text) TO cloud_agents_runtime;
