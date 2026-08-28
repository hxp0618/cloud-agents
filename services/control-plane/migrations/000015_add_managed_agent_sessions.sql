-- Durable Managed Agent Session lifecycle.  The existing 000013/000014
-- migration lineage remains immutable; this entry is consumed by the product
-- successor bundle.
CREATE TABLE cloud_agents.managed_agent_sessions (
    tenant_id text NOT NULL,
    tenant_ref_id text NOT NULL,
    project_uid text NOT NULL,
    session_uid text NOT NULL,
    provider_kind text NOT NULL,
    state text NOT NULL,
    resource_version bigint NOT NULL,
    create_idempotency_key text NOT NULL,
    create_request_digest text NOT NULL,
    close_idempotency_key text,
    close_request_digest text,
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL,
    PRIMARY KEY (tenant_id, project_uid, session_uid),
    CONSTRAINT managed_agent_sessions_tenant_ref
        CHECK (tenant_id = tenant_ref_id),
    CONSTRAINT managed_agent_sessions_session_uid
        CHECK (cloud_agents.is_valid_identifier(session_uid)),
    CONSTRAINT managed_agent_sessions_provider_kind
        CHECK (cloud_agents.is_valid_identifier(provider_kind)),
    CONSTRAINT managed_agent_sessions_state
        CHECK (state IN ('active', 'closed')),
    CONSTRAINT managed_agent_sessions_resource_version
        CHECK (resource_version > 0),
    CONSTRAINT managed_agent_sessions_create_key
        CHECK (create_idempotency_key ~ '^[A-Za-z0-9._~-]{16,128}$'),
    CONSTRAINT managed_agent_sessions_create_digest
        CHECK (create_request_digest ~ '^sha256:[0-9a-f]{64}$'),
    CONSTRAINT managed_agent_sessions_close_key
        CHECK (close_idempotency_key IS NULL OR close_idempotency_key ~ '^[A-Za-z0-9._~-]{16,128}$'),
    CONSTRAINT managed_agent_sessions_close_digest
        CHECK (close_request_digest IS NULL OR close_request_digest ~ '^sha256:[0-9a-f]{64}$'),
    CONSTRAINT managed_agent_sessions_project_fk
        FOREIGN KEY (tenant_id, project_uid)
        REFERENCES cloud_agents.projects (tenant_id, project_uid)
        ON UPDATE RESTRICT ON DELETE RESTRICT,
    CONSTRAINT managed_agent_sessions_tenant_fk
        FOREIGN KEY (tenant_id, tenant_ref_id)
        REFERENCES cloud_agents.platform_tenants (tenant_id, tenant_uid)
        ON UPDATE RESTRICT ON DELETE RESTRICT,
    CONSTRAINT managed_agent_sessions_close_pair
        CHECK ((close_idempotency_key IS NULL) = (close_request_digest IS NULL))
);

CREATE INDEX managed_agent_sessions_create_key_idx
    ON cloud_agents.managed_agent_sessions (tenant_id, project_uid, create_idempotency_key);

ALTER TABLE cloud_agents.managed_agent_sessions OWNER TO cloud_agents_migration_owner;
ALTER TABLE cloud_agents.managed_agent_sessions ENABLE ROW LEVEL SECURITY;
ALTER TABLE cloud_agents.managed_agent_sessions FORCE ROW LEVEL SECURITY;

CREATE POLICY managed_agent_sessions_runtime_tenant
    ON cloud_agents.managed_agent_sessions
    TO cloud_agents_runtime
    USING (tenant_id = cloud_agents.require_tenant_id())
    WITH CHECK (tenant_id = cloud_agents.require_tenant_id());

CREATE POLICY managed_agent_sessions_migration_owner
    ON cloud_agents.managed_agent_sessions
    TO cloud_agents_migration_owner
    USING (true) WITH CHECK (true);

REVOKE ALL ON TABLE cloud_agents.managed_agent_sessions FROM PUBLIC;
REVOKE ALL ON TABLE cloud_agents.managed_agent_sessions FROM cloud_agents_bootstrap_admin;
GRANT SELECT ON TABLE cloud_agents.managed_agent_sessions TO cloud_agents_runtime;

CREATE FUNCTION cloud_agents.create_managed_agent_session_v1(
    p_tenant_id text,
    p_project_uid text,
    p_session_uid text,
    p_provider_kind text,
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
        OR NOT cloud_agents.is_valid_identifier(p_provider_kind)
        OR p_idempotency_key !~ '^[A-Za-z0-9._~-]{16,128}$'
        OR p_request_digest !~ '^sha256:[0-9a-f]{64}$'
    THEN
        RAISE EXCEPTION USING ERRCODE = '22023', MESSAGE = 'managed agent session input is invalid';
    END IF;

    SELECT session.* INTO existing
    FROM cloud_agents.managed_agent_sessions AS session
    WHERE session.tenant_id = p_tenant_id
        AND session.project_uid = p_project_uid
        AND session.create_idempotency_key = p_idempotency_key
    FOR UPDATE;
    IF FOUND THEN
        IF existing.create_request_digest IS DISTINCT FROM p_request_digest THEN
            RAISE EXCEPTION USING ERRCODE = '23505', MESSAGE = 'managed agent session idempotency conflict';
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

    PERFORM 1
    FROM cloud_agents.projects AS project
    WHERE project.tenant_id = p_tenant_id
        AND project.project_uid = p_project_uid
        AND project.state = 'active'
    FOR KEY SHARE;
    IF NOT FOUND THEN
        RAISE EXCEPTION USING ERRCODE = '23503', MESSAGE = 'project is absent or inactive';
    END IF;

    INSERT INTO cloud_agents.managed_agent_sessions (
        tenant_id, tenant_ref_id, project_uid, session_uid, provider_kind,
        state, resource_version, create_idempotency_key, create_request_digest,
        created_at, updated_at
    ) VALUES (
        p_tenant_id, p_tenant_id, p_project_uid, p_session_uid, p_provider_kind,
        'active', 1, p_idempotency_key, p_request_digest, mutation_at, mutation_at
    );
    session_uid := p_session_uid;
    provider_kind := p_provider_kind;
    state := 'active';
    resource_version := 1;
    created_at := mutation_at;
    updated_at := mutation_at;
    RETURN NEXT;
END;
$cloud_agents_function$;

CREATE FUNCTION cloud_agents.close_managed_agent_session_v1(
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

ALTER FUNCTION cloud_agents.create_managed_agent_session_v1(text, text, text, text, text, text)
    OWNER TO cloud_agents_migration_owner;
ALTER FUNCTION cloud_agents.close_managed_agent_session_v1(text, text, text, text, text)
    OWNER TO cloud_agents_migration_owner;
REVOKE ALL ON FUNCTION cloud_agents.create_managed_agent_session_v1(text, text, text, text, text, text) FROM PUBLIC;
REVOKE ALL ON FUNCTION cloud_agents.close_managed_agent_session_v1(text, text, text, text, text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION cloud_agents.create_managed_agent_session_v1(text, text, text, text, text, text) TO cloud_agents_runtime;
GRANT EXECUTE ON FUNCTION cloud_agents.close_managed_agent_session_v1(text, text, text, text, text) TO cloud_agents_runtime;
