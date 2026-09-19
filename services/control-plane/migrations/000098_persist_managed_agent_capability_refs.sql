-- Persist only opaque capability pins on lifecycle rows. Resolution remains a
-- Runtime concern; no endpoint, token, source, or host path is stored here.

CREATE FUNCTION cloud_agents.is_valid_managed_agent_capability_refs(
    p_values jsonb, p_kind text
)
RETURNS boolean
LANGUAGE plpgsql IMMUTABLE PARALLEL SAFE SET search_path = pg_catalog, cloud_agents
AS $cloud_agents_function$
DECLARE
    item jsonb;
    item_id text;
    item_version text;
    item_digest text;
BEGIN
    IF p_values IS NULL OR pg_catalog.jsonb_typeof(p_values) <> 'array'
        OR pg_catalog.jsonb_array_length(p_values) > 32
        OR p_kind NOT IN ('mcp', 'skill')
    THEN
        RETURN false;
    END IF;
    FOR item IN SELECT value FROM pg_catalog.jsonb_array_elements(p_values) LOOP
        IF pg_catalog.jsonb_typeof(item) <> 'object'
            OR (p_kind = 'mcp' AND NOT (item ? 'serverId' AND item ? 'version' AND item ? 'digest'))
            OR (p_kind = 'skill' AND NOT (item ? 'bundleId' AND item ? 'version' AND item ? 'digest'))
            OR (SELECT pg_catalog.count(*) FROM pg_catalog.jsonb_object_keys(item)) <> 3
        THEN
            RETURN false;
        END IF;
        item_id := COALESCE(item->>'serverId', item->>'bundleId');
        item_version := item->>'version';
        item_digest := item->>'digest';
        IF NOT cloud_agents.is_valid_identifier(item_id)
            OR NOT cloud_agents.is_valid_identifier(item_version)
            OR item_digest !~ '^sha256:[0-9a-f]{64}$'
        THEN
            RETURN false;
        END IF;
    END LOOP;
    RETURN (
        SELECT pg_catalog.count(*) = pg_catalog.count(DISTINCT value)
        FROM pg_catalog.jsonb_array_elements(p_values) AS values(value)
    );
END;
$cloud_agents_function$;

ALTER FUNCTION cloud_agents.is_valid_managed_agent_capability_refs(jsonb, text)
    OWNER TO cloud_agents_migration_owner;
REVOKE ALL ON FUNCTION cloud_agents.is_valid_managed_agent_capability_refs(jsonb, text) FROM PUBLIC;

ALTER TABLE cloud_agents.managed_agent_sessions
    ADD COLUMN mcp_server_refs jsonb NOT NULL DEFAULT '[]'::jsonb,
    ADD COLUMN skill_bundle_refs jsonb NOT NULL DEFAULT '[]'::jsonb,
    ADD CONSTRAINT managed_agent_sessions_mcp_refs
        CHECK (cloud_agents.is_valid_managed_agent_capability_refs(mcp_server_refs, 'mcp')),
    ADD CONSTRAINT managed_agent_sessions_skill_refs
        CHECK (cloud_agents.is_valid_managed_agent_capability_refs(skill_bundle_refs, 'skill'));

ALTER TABLE cloud_agents.managed_agent_executions
    ADD COLUMN mcp_server_refs jsonb NOT NULL DEFAULT '[]'::jsonb,
    ADD COLUMN skill_bundle_refs jsonb NOT NULL DEFAULT '[]'::jsonb,
    ADD CONSTRAINT managed_agent_executions_mcp_refs
        CHECK (cloud_agents.is_valid_managed_agent_capability_refs(mcp_server_refs, 'mcp')),
    ADD CONSTRAINT managed_agent_executions_skill_refs
        CHECK (cloud_agents.is_valid_managed_agent_capability_refs(skill_bundle_refs, 'skill'));

CREATE FUNCTION cloud_agents.create_managed_agent_session_v5(
    p_tenant_id text, p_project_uid text, p_session_uid text, p_provider_kind text,
    p_environment_lease_uid text, p_workspace_uid text, p_sandbox_uid text,
    p_sandbox_generation bigint, p_environment_profile_uid text,
    p_environment_profile_version bigint, p_idempotency_key text, p_request_digest text,
    p_mcp_server_refs jsonb, p_skill_bundle_refs jsonb
)
RETURNS TABLE (
    session_uid text, provider_kind text, environment_lease_uid text,
    environment_generation bigint, workspace_uid text, sandbox_uid text,
    sandbox_generation bigint, environment_profile_uid text,
    environment_profile_version bigint, state text, resource_version bigint,
    created_at timestamptz, updated_at timestamptz,
    mcp_server_refs jsonb, skill_bundle_refs jsonb
)
LANGUAGE plpgsql SECURITY DEFINER SET search_path = pg_catalog, cloud_agents
AS $cloud_agents_function$
BEGIN
    IF NOT cloud_agents.is_valid_managed_agent_capability_refs(p_mcp_server_refs, 'mcp')
        OR NOT cloud_agents.is_valid_managed_agent_capability_refs(p_skill_bundle_refs, 'skill')
    THEN
        RAISE EXCEPTION USING ERRCODE = '22023', MESSAGE = 'managed agent capability refs are invalid';
    END IF;
    PERFORM cloud_agents.create_managed_agent_session_v4(
        p_tenant_id, p_project_uid, p_session_uid, p_provider_kind,
        p_environment_lease_uid, p_workspace_uid, p_sandbox_uid,
        p_sandbox_generation, p_environment_profile_uid,
        p_environment_profile_version, p_idempotency_key, p_request_digest
    );
    UPDATE cloud_agents.managed_agent_sessions AS session
    SET mcp_server_refs = p_mcp_server_refs, skill_bundle_refs = p_skill_bundle_refs
    WHERE session.tenant_id = p_tenant_id AND session.project_uid = p_project_uid
        AND session.create_idempotency_key = p_idempotency_key;
    RETURN QUERY
    SELECT session.session_uid, session.provider_kind, session.environment_lease_uid,
        session.environment_generation, session.workspace_uid, session.sandbox_uid,
        session.sandbox_generation,
        COALESCE(session.environment_profile_uid, environment.environment_profile_uid),
        COALESCE(session.environment_profile_version, environment.environment_profile_version),
        session.state, session.resource_version, session.created_at, session.updated_at,
        session.mcp_server_refs, session.skill_bundle_refs
    FROM cloud_agents.managed_agent_sessions AS session
    LEFT JOIN cloud_agents.managed_host_environment_leases AS environment
      ON environment.tenant_id = session.tenant_id AND environment.project_uid = session.project_uid
     AND environment.lease_uid = session.environment_lease_uid
    WHERE session.tenant_id = p_tenant_id AND session.project_uid = p_project_uid
      AND session.create_idempotency_key = p_idempotency_key;
END;
$cloud_agents_function$;

ALTER FUNCTION cloud_agents.create_managed_agent_session_v5(
    text, text, text, text, text, text, text, bigint, text, bigint, text, text, jsonb, jsonb
) OWNER TO cloud_agents_migration_owner;
REVOKE ALL ON FUNCTION cloud_agents.create_managed_agent_session_v5(
    text, text, text, text, text, text, text, bigint, text, bigint, text, text, jsonb, jsonb
) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION cloud_agents.create_managed_agent_session_v5(
    text, text, text, text, text, text, text, bigint, text, bigint, text, text, jsonb, jsonb
) TO cloud_agents_runtime;

CREATE FUNCTION cloud_agents.create_managed_agent_execution_v2(
    p_tenant_id text, p_project_uid text, p_session_uid text, p_turn_uid text,
    p_execution_uid text, p_generation bigint, p_idempotency_key text, p_request_digest text,
    p_mcp_server_refs jsonb, p_skill_bundle_refs jsonb
)
RETURNS TABLE (
    execution_uid text, generation bigint, state text, result_digest text,
    error_code text, resource_version bigint, created_at timestamptz, updated_at timestamptz,
    mcp_server_refs jsonb, skill_bundle_refs jsonb
)
LANGUAGE plpgsql SECURITY DEFINER SET search_path = pg_catalog, cloud_agents
AS $cloud_agents_function$
BEGIN
    IF NOT cloud_agents.is_valid_managed_agent_capability_refs(p_mcp_server_refs, 'mcp')
        OR NOT cloud_agents.is_valid_managed_agent_capability_refs(p_skill_bundle_refs, 'skill')
    THEN
        RAISE EXCEPTION USING ERRCODE = '22023', MESSAGE = 'managed agent capability refs are invalid';
    END IF;
    PERFORM cloud_agents.create_managed_agent_execution_v1(
        p_tenant_id, p_project_uid, p_session_uid, p_turn_uid,
        p_execution_uid, p_generation, p_idempotency_key, p_request_digest
    );
    UPDATE cloud_agents.managed_agent_executions AS execution
    SET mcp_server_refs = p_mcp_server_refs, skill_bundle_refs = p_skill_bundle_refs
    WHERE execution.tenant_id = p_tenant_id AND execution.project_uid = p_project_uid
      AND execution.session_uid = p_session_uid AND execution.create_idempotency_key = p_idempotency_key;
    RETURN QUERY
    SELECT execution.execution_uid, execution.generation, execution.state,
        execution.result_digest, execution.error_code, execution.resource_version,
        execution.created_at, execution.updated_at,
        execution.mcp_server_refs, execution.skill_bundle_refs
    FROM cloud_agents.managed_agent_executions AS execution
    WHERE execution.tenant_id = p_tenant_id AND execution.project_uid = p_project_uid
      AND execution.session_uid = p_session_uid AND execution.create_idempotency_key = p_idempotency_key;
END;
$cloud_agents_function$;

ALTER FUNCTION cloud_agents.create_managed_agent_execution_v2(
    text, text, text, text, text, bigint, text, text, jsonb, jsonb
) OWNER TO cloud_agents_migration_owner;
REVOKE ALL ON FUNCTION cloud_agents.create_managed_agent_execution_v2(
    text, text, text, text, text, bigint, text, text, jsonb, jsonb
) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION cloud_agents.create_managed_agent_execution_v2(
    text, text, text, text, text, bigint, text, text, jsonb, jsonb
) TO cloud_agents_runtime;

CREATE TABLE cloud_agents.mcp_servers (
    tenant_id text NOT NULL,
    tenant_ref_id text NOT NULL,
    project_uid text NOT NULL,
    server_uid text NOT NULL,
    version text NOT NULL,
    digest text NOT NULL,
    transport text NOT NULL,
    connection_ref text NOT NULL,
    credential_ref text NOT NULL,
    network_policy_ref text NOT NULL,
    permissions text[] NOT NULL,
    status text NOT NULL,
    resource_version bigint NOT NULL,
    create_idempotency_key text NOT NULL,
    create_request_digest text NOT NULL,
    revoke_idempotency_key text,
    revoke_request_digest text,
    revoked_at timestamptz,
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL,
    PRIMARY KEY (tenant_id, project_uid, server_uid),
    CONSTRAINT mcp_servers_create_key UNIQUE (tenant_id, project_uid, create_idempotency_key),
    CONSTRAINT mcp_servers_tenant_ref CHECK (tenant_id = tenant_ref_id),
    CONSTRAINT mcp_servers_ids CHECK (cloud_agents.is_valid_identifier(server_uid) AND cloud_agents.is_valid_identifier(version) AND cloud_agents.is_valid_identifier(connection_ref) AND cloud_agents.is_valid_identifier(credential_ref) AND cloud_agents.is_valid_identifier(network_policy_ref)),
    CONSTRAINT mcp_servers_digest CHECK (digest ~ '^sha256:[0-9a-f]{64}$'),
    CONSTRAINT mcp_servers_transport CHECK (transport IN ('stdio', 'sse', 'streamable-http')),
    CONSTRAINT mcp_servers_permissions CHECK (cardinality(permissions) BETWEEN 1 AND 64),
    CONSTRAINT mcp_servers_status CHECK (status IN ('active', 'revoked') AND ((status = 'active' AND revoked_at IS NULL) OR (status = 'revoked' AND revoked_at IS NOT NULL))),
    CONSTRAINT mcp_servers_resource_version CHECK (resource_version > 0),
    CONSTRAINT mcp_servers_create_mutation CHECK (create_idempotency_key ~ '^[A-Za-z0-9._~-]{16,128}$' AND create_request_digest ~ '^sha256:[0-9a-f]{64}$'),
    CONSTRAINT mcp_servers_revoke_mutation CHECK ((revoke_idempotency_key IS NULL AND revoke_request_digest IS NULL) OR (revoke_idempotency_key ~ '^[A-Za-z0-9._~-]{16,128}$' AND revoke_request_digest ~ '^sha256:[0-9a-f]{64}$')),
    CONSTRAINT mcp_servers_project_fk FOREIGN KEY (tenant_id, project_uid) REFERENCES cloud_agents.projects (tenant_id, project_uid) ON UPDATE RESTRICT ON DELETE RESTRICT
);

CREATE TABLE cloud_agents.skill_bundles (
    tenant_id text NOT NULL,
    tenant_ref_id text NOT NULL,
    project_uid text NOT NULL,
    bundle_uid text NOT NULL,
    version text NOT NULL,
    digest text NOT NULL,
    source_ref text NOT NULL,
    signature_ref text NOT NULL,
    signing_key_id text NOT NULL,
    compatible_providers text[] NOT NULL,
    status text NOT NULL,
    resource_version bigint NOT NULL,
    create_idempotency_key text NOT NULL,
    create_request_digest text NOT NULL,
    revoke_idempotency_key text,
    revoke_request_digest text,
    revoked_at timestamptz,
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL,
    PRIMARY KEY (tenant_id, project_uid, bundle_uid),
    CONSTRAINT skill_bundles_create_key UNIQUE (tenant_id, project_uid, create_idempotency_key),
    CONSTRAINT skill_bundles_tenant_ref CHECK (tenant_id = tenant_ref_id),
    CONSTRAINT skill_bundles_ids CHECK (cloud_agents.is_valid_identifier(bundle_uid) AND cloud_agents.is_valid_identifier(version) AND cloud_agents.is_valid_identifier(source_ref) AND cloud_agents.is_valid_identifier(signature_ref) AND cloud_agents.is_valid_identifier(signing_key_id)),
    CONSTRAINT skill_bundles_digest CHECK (digest ~ '^sha256:[0-9a-f]{64}$'),
    CONSTRAINT skill_bundles_providers CHECK (cardinality(compatible_providers) BETWEEN 1 AND 4),
    CONSTRAINT skill_bundles_status CHECK (status IN ('active', 'revoked') AND ((status = 'active' AND revoked_at IS NULL) OR (status = 'revoked' AND revoked_at IS NOT NULL))),
    CONSTRAINT skill_bundles_resource_version CHECK (resource_version > 0),
    CONSTRAINT skill_bundles_create_mutation CHECK (create_idempotency_key ~ '^[A-Za-z0-9._~-]{16,128}$' AND create_request_digest ~ '^sha256:[0-9a-f]{64}$'),
    CONSTRAINT skill_bundles_revoke_mutation CHECK ((revoke_idempotency_key IS NULL AND revoke_request_digest IS NULL) OR (revoke_idempotency_key ~ '^[A-Za-z0-9._~-]{16,128}$' AND revoke_request_digest ~ '^sha256:[0-9a-f]{64}$')),
    CONSTRAINT skill_bundles_project_fk FOREIGN KEY (tenant_id, project_uid) REFERENCES cloud_agents.projects (tenant_id, project_uid) ON UPDATE RESTRICT ON DELETE RESTRICT
);

ALTER TABLE cloud_agents.mcp_servers OWNER TO cloud_agents_migration_owner;
ALTER TABLE cloud_agents.skill_bundles OWNER TO cloud_agents_migration_owner;
ALTER TABLE cloud_agents.mcp_servers ENABLE ROW LEVEL SECURITY;
ALTER TABLE cloud_agents.skill_bundles ENABLE ROW LEVEL SECURITY;
ALTER TABLE cloud_agents.mcp_servers FORCE ROW LEVEL SECURITY;
ALTER TABLE cloud_agents.skill_bundles FORCE ROW LEVEL SECURITY;
CREATE POLICY mcp_servers_runtime_read ON cloud_agents.mcp_servers TO cloud_agents_runtime USING (tenant_id = cloud_agents.require_tenant_id());
CREATE POLICY skill_bundles_runtime_read ON cloud_agents.skill_bundles TO cloud_agents_runtime USING (tenant_id = cloud_agents.require_tenant_id());
CREATE POLICY mcp_servers_migration_read ON cloud_agents.mcp_servers TO cloud_agents_migration_owner USING (true) WITH CHECK (true);
CREATE POLICY skill_bundles_migration_read ON cloud_agents.skill_bundles TO cloud_agents_migration_owner USING (true) WITH CHECK (true);
REVOKE ALL ON TABLE cloud_agents.mcp_servers FROM PUBLIC;
REVOKE ALL ON TABLE cloud_agents.skill_bundles FROM PUBLIC;
GRANT SELECT ON TABLE cloud_agents.mcp_servers TO cloud_agents_runtime;
GRANT SELECT ON TABLE cloud_agents.skill_bundles TO cloud_agents_runtime;

CREATE FUNCTION cloud_agents.create_mcp_server_v1(
    p_tenant_id text, p_project_uid text, p_server_uid text, p_version text, p_digest text,
    p_transport text, p_connection_ref text, p_credential_ref text, p_network_policy_ref text,
    p_permissions text[], p_idempotency_key text, p_request_digest text
)
RETURNS TABLE (server_uid text, version text, digest text, transport text, connection_ref text, credential_ref text, network_policy_ref text, permissions text[], status text, resource_version bigint, created_at timestamptz, updated_at timestamptz, revoked_at timestamptz)
LANGUAGE plpgsql SECURITY DEFINER SET search_path = pg_catalog, cloud_agents
AS $cloud_agents_function$
DECLARE existing cloud_agents.mcp_servers%ROWTYPE;
BEGIN
    PERFORM cloud_agents.require_runtime_mutation_principal();
    IF p_tenant_id IS DISTINCT FROM cloud_agents.require_tenant_id() OR NOT cloud_agents.is_valid_identifier(p_project_uid) OR NOT cloud_agents.is_valid_identifier(p_server_uid) OR NOT cloud_agents.is_valid_identifier(p_version) OR p_digest !~ '^sha256:[0-9a-f]{64}$' OR p_transport NOT IN ('stdio', 'sse', 'streamable-http') OR NOT cloud_agents.is_valid_identifier(p_connection_ref) OR NOT cloud_agents.is_valid_identifier(p_credential_ref) OR NOT cloud_agents.is_valid_identifier(p_network_policy_ref) OR p_permissions IS NULL OR pg_catalog.cardinality(p_permissions) NOT BETWEEN 1 AND 64 OR EXISTS (SELECT 1 FROM pg_catalog.unnest(p_permissions) AS permission WHERE permission !~ '^[a-z][a-z0-9._:-]{0,127}$') OR pg_catalog.cardinality(p_permissions) <> (SELECT pg_catalog.count(DISTINCT permission) FROM pg_catalog.unnest(p_permissions) AS permission) OR p_idempotency_key !~ '^[A-Za-z0-9._~-]{16,128}$' OR p_request_digest !~ '^sha256:[0-9a-f]{64}$' THEN RAISE EXCEPTION USING ERRCODE = '22023', MESSAGE = 'MCP Server input is invalid'; END IF;
    SELECT * INTO existing FROM cloud_agents.mcp_servers WHERE tenant_id = p_tenant_id AND project_uid = p_project_uid AND create_idempotency_key = p_idempotency_key FOR UPDATE;
    IF FOUND THEN
        IF existing.create_request_digest IS DISTINCT FROM p_request_digest THEN RAISE EXCEPTION USING ERRCODE = '23505', MESSAGE = 'MCP Server idempotency conflict'; END IF;
        RETURN QUERY SELECT existing.server_uid, existing.version, existing.digest, existing.transport, existing.connection_ref, existing.credential_ref, existing.network_policy_ref, existing.permissions, existing.status, existing.resource_version, existing.created_at, existing.updated_at, existing.revoked_at; RETURN;
    END IF;
    PERFORM 1 FROM cloud_agents.projects WHERE tenant_id = p_tenant_id AND project_uid = p_project_uid AND state = 'active' FOR KEY SHARE;
    IF NOT FOUND THEN RAISE EXCEPTION USING ERRCODE = '23503', MESSAGE = 'project is absent or inactive'; END IF;
    INSERT INTO cloud_agents.mcp_servers(tenant_id, tenant_ref_id, project_uid, server_uid, version, digest, transport, connection_ref, credential_ref, network_policy_ref, permissions, status, resource_version, create_idempotency_key, create_request_digest, created_at, updated_at) VALUES (p_tenant_id, p_tenant_id, p_project_uid, p_server_uid, p_version, p_digest, p_transport, p_connection_ref, p_credential_ref, p_network_policy_ref, p_permissions, 'active', 1, p_idempotency_key, p_request_digest, pg_catalog.transaction_timestamp(), pg_catalog.transaction_timestamp());
    RETURN QUERY SELECT server.server_uid, server.version, server.digest, server.transport, server.connection_ref, server.credential_ref, server.network_policy_ref, server.permissions, server.status, server.resource_version, server.created_at, server.updated_at, server.revoked_at FROM cloud_agents.mcp_servers AS server WHERE server.tenant_id = p_tenant_id AND server.project_uid = p_project_uid AND server.server_uid = p_server_uid;
END;
$cloud_agents_function$;
ALTER FUNCTION cloud_agents.create_mcp_server_v1(text,text,text,text,text,text,text,text,text,text[],text,text) OWNER TO cloud_agents_migration_owner;
REVOKE ALL ON FUNCTION cloud_agents.create_mcp_server_v1(text,text,text,text,text,text,text,text,text,text[],text,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION cloud_agents.create_mcp_server_v1(text,text,text,text,text,text,text,text,text,text[],text,text) TO cloud_agents_runtime;

CREATE FUNCTION cloud_agents.revoke_mcp_server_v1(p_tenant_id text, p_project_uid text, p_server_uid text, p_expected_resource_version bigint, p_idempotency_key text, p_request_digest text)
RETURNS TABLE (server_uid text, version text, digest text, transport text, connection_ref text, credential_ref text, network_policy_ref text, permissions text[], status text, resource_version bigint, created_at timestamptz, updated_at timestamptz, revoked_at timestamptz)
LANGUAGE plpgsql SECURITY DEFINER SET search_path = pg_catalog, cloud_agents
AS $cloud_agents_function$
DECLARE existing cloud_agents.mcp_servers%ROWTYPE;
BEGIN
    PERFORM cloud_agents.require_runtime_mutation_principal();
    IF p_tenant_id IS DISTINCT FROM cloud_agents.require_tenant_id() OR NOT cloud_agents.is_valid_identifier(p_project_uid) OR NOT cloud_agents.is_valid_identifier(p_server_uid) OR p_expected_resource_version IS NULL OR p_expected_resource_version < 1 OR p_idempotency_key IS NULL OR p_idempotency_key !~ '^[A-Za-z0-9._~-]{16,128}$' OR p_request_digest IS NULL OR p_request_digest !~ '^sha256:[0-9a-f]{64}$' THEN RAISE EXCEPTION USING ERRCODE = '22023', MESSAGE = 'MCP Server revoke input is invalid'; END IF;
    SELECT * INTO existing FROM cloud_agents.mcp_servers AS server WHERE server.tenant_id = p_tenant_id AND server.project_uid = p_project_uid AND server.server_uid = p_server_uid FOR UPDATE;
    IF NOT FOUND THEN RAISE EXCEPTION USING ERRCODE = '23503', MESSAGE = 'MCP Server is absent'; END IF;
    IF existing.status = 'revoked' THEN
        IF existing.revoke_idempotency_key IS DISTINCT FROM p_idempotency_key OR existing.revoke_request_digest IS DISTINCT FROM p_request_digest THEN RAISE EXCEPTION USING ERRCODE = '23505', MESSAGE = 'MCP Server revoke conflict'; END IF;
    ELSE
        IF existing.resource_version IS DISTINCT FROM p_expected_resource_version THEN RAISE EXCEPTION USING ERRCODE = '40001', MESSAGE = 'MCP Server resource version conflict'; END IF;
        UPDATE cloud_agents.mcp_servers AS server SET status = 'revoked', revoked_at = pg_catalog.transaction_timestamp(), resource_version = server.resource_version + 1, updated_at = pg_catalog.transaction_timestamp(), revoke_idempotency_key = p_idempotency_key, revoke_request_digest = p_request_digest WHERE server.tenant_id = p_tenant_id AND server.project_uid = p_project_uid AND server.server_uid = p_server_uid;
    END IF;
    RETURN QUERY SELECT server.server_uid, server.version, server.digest, server.transport, server.connection_ref, server.credential_ref, server.network_policy_ref, server.permissions, server.status, server.resource_version, server.created_at, server.updated_at, server.revoked_at FROM cloud_agents.mcp_servers AS server WHERE server.tenant_id = p_tenant_id AND server.project_uid = p_project_uid AND server.server_uid = p_server_uid;
END;
$cloud_agents_function$;
ALTER FUNCTION cloud_agents.revoke_mcp_server_v1(text,text,text,bigint,text,text) OWNER TO cloud_agents_migration_owner;
REVOKE ALL ON FUNCTION cloud_agents.revoke_mcp_server_v1(text,text,text,bigint,text,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION cloud_agents.revoke_mcp_server_v1(text,text,text,bigint,text,text) TO cloud_agents_runtime;

CREATE FUNCTION cloud_agents.create_skill_bundle_v1(
    p_tenant_id text, p_project_uid text, p_bundle_uid text, p_version text, p_digest text,
    p_source_ref text, p_signature_ref text, p_signing_key_id text, p_compatible_providers text[],
    p_idempotency_key text, p_request_digest text
)
RETURNS TABLE (bundle_uid text, version text, digest text, source_ref text, signature_ref text, signing_key_id text, compatible_providers text[], status text, resource_version bigint, created_at timestamptz, updated_at timestamptz, revoked_at timestamptz)
LANGUAGE plpgsql SECURITY DEFINER SET search_path = pg_catalog, cloud_agents
AS $cloud_agents_function$
DECLARE existing cloud_agents.skill_bundles%ROWTYPE;
BEGIN
    PERFORM cloud_agents.require_runtime_mutation_principal();
    IF p_tenant_id IS DISTINCT FROM cloud_agents.require_tenant_id() OR NOT cloud_agents.is_valid_identifier(p_project_uid) OR NOT cloud_agents.is_valid_identifier(p_bundle_uid) OR NOT cloud_agents.is_valid_identifier(p_version) OR p_digest !~ '^sha256:[0-9a-f]{64}$' OR NOT cloud_agents.is_valid_identifier(p_source_ref) OR NOT cloud_agents.is_valid_identifier(p_signature_ref) OR NOT cloud_agents.is_valid_identifier(p_signing_key_id) OR p_compatible_providers IS NULL OR pg_catalog.cardinality(p_compatible_providers) NOT BETWEEN 1 AND 4 OR EXISTS (SELECT 1 FROM pg_catalog.unnest(p_compatible_providers) AS provider WHERE provider NOT IN ('codex', 'claude-code', 'pi', 'deepseek-harness')) OR pg_catalog.cardinality(p_compatible_providers) <> (SELECT pg_catalog.count(DISTINCT provider) FROM pg_catalog.unnest(p_compatible_providers) AS provider) OR p_idempotency_key !~ '^[A-Za-z0-9._~-]{16,128}$' OR p_request_digest !~ '^sha256:[0-9a-f]{64}$' THEN RAISE EXCEPTION USING ERRCODE = '22023', MESSAGE = 'Skill Bundle input is invalid'; END IF;
    SELECT * INTO existing FROM cloud_agents.skill_bundles WHERE tenant_id = p_tenant_id AND project_uid = p_project_uid AND create_idempotency_key = p_idempotency_key FOR UPDATE;
    IF FOUND THEN
        IF existing.create_request_digest IS DISTINCT FROM p_request_digest THEN RAISE EXCEPTION USING ERRCODE = '23505', MESSAGE = 'Skill Bundle idempotency conflict'; END IF;
        RETURN QUERY SELECT existing.bundle_uid, existing.version, existing.digest, existing.source_ref, existing.signature_ref, existing.signing_key_id, existing.compatible_providers, existing.status, existing.resource_version, existing.created_at, existing.updated_at, existing.revoked_at; RETURN;
    END IF;
    PERFORM 1 FROM cloud_agents.projects WHERE tenant_id = p_tenant_id AND project_uid = p_project_uid AND state = 'active' FOR KEY SHARE;
    IF NOT FOUND THEN RAISE EXCEPTION USING ERRCODE = '23503', MESSAGE = 'project is absent or inactive'; END IF;
    INSERT INTO cloud_agents.skill_bundles(tenant_id, tenant_ref_id, project_uid, bundle_uid, version, digest, source_ref, signature_ref, signing_key_id, compatible_providers, status, resource_version, create_idempotency_key, create_request_digest, created_at, updated_at) VALUES (p_tenant_id, p_tenant_id, p_project_uid, p_bundle_uid, p_version, p_digest, p_source_ref, p_signature_ref, p_signing_key_id, p_compatible_providers, 'active', 1, p_idempotency_key, p_request_digest, pg_catalog.transaction_timestamp(), pg_catalog.transaction_timestamp());
    RETURN QUERY SELECT bundle.bundle_uid, bundle.version, bundle.digest, bundle.source_ref, bundle.signature_ref, bundle.signing_key_id, bundle.compatible_providers, bundle.status, bundle.resource_version, bundle.created_at, bundle.updated_at, bundle.revoked_at FROM cloud_agents.skill_bundles AS bundle WHERE bundle.tenant_id = p_tenant_id AND bundle.project_uid = p_project_uid AND bundle.bundle_uid = p_bundle_uid;
END;
$cloud_agents_function$;
ALTER FUNCTION cloud_agents.create_skill_bundle_v1(text,text,text,text,text,text,text,text,text[],text,text) OWNER TO cloud_agents_migration_owner;
REVOKE ALL ON FUNCTION cloud_agents.create_skill_bundle_v1(text,text,text,text,text,text,text,text,text[],text,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION cloud_agents.create_skill_bundle_v1(text,text,text,text,text,text,text,text,text[],text,text) TO cloud_agents_runtime;

CREATE FUNCTION cloud_agents.revoke_skill_bundle_v1(p_tenant_id text, p_project_uid text, p_bundle_uid text, p_expected_resource_version bigint, p_idempotency_key text, p_request_digest text)
RETURNS TABLE (bundle_uid text, version text, digest text, source_ref text, signature_ref text, signing_key_id text, compatible_providers text[], status text, resource_version bigint, created_at timestamptz, updated_at timestamptz, revoked_at timestamptz)
LANGUAGE plpgsql SECURITY DEFINER SET search_path = pg_catalog, cloud_agents
AS $cloud_agents_function$
DECLARE existing cloud_agents.skill_bundles%ROWTYPE;
BEGIN
    PERFORM cloud_agents.require_runtime_mutation_principal();
    IF p_tenant_id IS DISTINCT FROM cloud_agents.require_tenant_id() OR NOT cloud_agents.is_valid_identifier(p_project_uid) OR NOT cloud_agents.is_valid_identifier(p_bundle_uid) OR p_expected_resource_version IS NULL OR p_expected_resource_version < 1 OR p_idempotency_key IS NULL OR p_idempotency_key !~ '^[A-Za-z0-9._~-]{16,128}$' OR p_request_digest IS NULL OR p_request_digest !~ '^sha256:[0-9a-f]{64}$' THEN RAISE EXCEPTION USING ERRCODE = '22023', MESSAGE = 'Skill Bundle revoke input is invalid'; END IF;
    SELECT * INTO existing FROM cloud_agents.skill_bundles AS bundle WHERE bundle.tenant_id = p_tenant_id AND bundle.project_uid = p_project_uid AND bundle.bundle_uid = p_bundle_uid FOR UPDATE;
    IF NOT FOUND THEN RAISE EXCEPTION USING ERRCODE = '23503', MESSAGE = 'Skill Bundle is absent'; END IF;
    IF existing.status = 'revoked' THEN
        IF existing.revoke_idempotency_key IS DISTINCT FROM p_idempotency_key OR existing.revoke_request_digest IS DISTINCT FROM p_request_digest THEN RAISE EXCEPTION USING ERRCODE = '23505', MESSAGE = 'Skill Bundle revoke conflict'; END IF;
    ELSE
        IF existing.resource_version IS DISTINCT FROM p_expected_resource_version THEN RAISE EXCEPTION USING ERRCODE = '40001', MESSAGE = 'Skill Bundle resource version conflict'; END IF;
        UPDATE cloud_agents.skill_bundles AS bundle SET status = 'revoked', revoked_at = pg_catalog.transaction_timestamp(), resource_version = bundle.resource_version + 1, updated_at = pg_catalog.transaction_timestamp(), revoke_idempotency_key = p_idempotency_key, revoke_request_digest = p_request_digest WHERE bundle.tenant_id = p_tenant_id AND bundle.project_uid = p_project_uid AND bundle.bundle_uid = p_bundle_uid;
    END IF;
    RETURN QUERY SELECT bundle.bundle_uid, bundle.version, bundle.digest, bundle.source_ref, bundle.signature_ref, bundle.signing_key_id, bundle.compatible_providers, bundle.status, bundle.resource_version, bundle.created_at, bundle.updated_at, bundle.revoked_at FROM cloud_agents.skill_bundles AS bundle WHERE bundle.tenant_id = p_tenant_id AND bundle.project_uid = p_project_uid AND bundle.bundle_uid = p_bundle_uid;
END;
$cloud_agents_function$;
ALTER FUNCTION cloud_agents.revoke_skill_bundle_v1(text,text,text,bigint,text,text) OWNER TO cloud_agents_migration_owner;
REVOKE ALL ON FUNCTION cloud_agents.revoke_skill_bundle_v1(text,text,text,bigint,text,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION cloud_agents.revoke_skill_bundle_v1(text,text,text,bigint,text,text) TO cloud_agents_runtime;
