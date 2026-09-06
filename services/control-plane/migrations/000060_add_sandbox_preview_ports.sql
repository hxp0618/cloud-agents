CREATE TABLE cloud_agents.sandbox_preview_ports (
    tenant_id text NOT NULL,
    project_uid text NOT NULL,
    grant_uid text NOT NULL,
    port integer NOT NULL CHECK (port BETWEEN 1024 AND 65535 AND port <> 44772),
    registered_at timestamptz NOT NULL DEFAULT transaction_timestamp(),
    revoked_at timestamptz,
    PRIMARY KEY (tenant_id, project_uid, grant_uid, port),
    FOREIGN KEY (tenant_id, project_uid, grant_uid) REFERENCES cloud_agents.sandbox_access_grants
        ON UPDATE RESTRICT ON DELETE RESTRICT,
    CHECK (revoked_at IS NULL OR revoked_at >= registered_at)
);

ALTER TABLE cloud_agents.sandbox_preview_ports OWNER TO cloud_agents_migration_owner;
ALTER TABLE cloud_agents.sandbox_preview_ports ENABLE ROW LEVEL SECURITY;
ALTER TABLE cloud_agents.sandbox_preview_ports FORCE ROW LEVEL SECURITY;
CREATE POLICY sandbox_preview_ports_runtime ON cloud_agents.sandbox_preview_ports TO cloud_agents_runtime
    USING (tenant_id = cloud_agents.require_tenant_id()) WITH CHECK (tenant_id = cloud_agents.require_tenant_id());
CREATE POLICY sandbox_preview_ports_owner ON cloud_agents.sandbox_preview_ports TO cloud_agents_migration_owner
    USING (true) WITH CHECK (true);
REVOKE ALL ON TABLE cloud_agents.sandbox_preview_ports FROM PUBLIC;
GRANT SELECT ON TABLE cloud_agents.sandbox_preview_ports TO cloud_agents_runtime;

CREATE FUNCTION cloud_agents.register_sandbox_preview_port_v1(
    p_project text, p_grant text, p_port integer, p_token_digest text
)
RETURNS TABLE (port integer, sandbox_uid text, sandbox_generation bigint, registered_at timestamptz)
LANGUAGE plpgsql SECURITY DEFINER SET search_path = pg_catalog, cloud_agents
AS $$
DECLARE
    ignored text;
    grant_value record;
    registered_at_value timestamptz := transaction_timestamp();
BEGIN
    ignored := cloud_agents.require_runtime_mutation_principal();
    IF NOT cloud_agents.is_valid_identifier(p_project) OR NOT cloud_agents.is_valid_identifier(p_grant)
       OR p_port NOT BETWEEN 1024 AND 65535 OR p_port = 44772
       OR p_token_digest !~ '^sha256:[0-9a-f]{64}$' THEN
        RAISE EXCEPTION USING ERRCODE = '22023', MESSAGE = 'sandbox Preview port input is invalid';
    END IF;
    SELECT access_grant.sandbox_uid, access_grant.sandbox_generation INTO grant_value
    FROM cloud_agents.sandbox_access_grants AS access_grant
    JOIN cloud_agents.sandbox_sessions AS sandbox
      ON sandbox.tenant_id = access_grant.tenant_id AND sandbox.project_uid = access_grant.project_uid
     AND sandbox.sandbox_uid = access_grant.sandbox_uid
    WHERE access_grant.tenant_id = cloud_agents.require_tenant_id()
      AND access_grant.project_uid = p_project AND access_grant.grant_uid = p_grant
      AND access_grant.token_digest = p_token_digest AND access_grant.status = 'active'
      AND access_grant.expires_at > clock_timestamp()
      AND sandbox.generation = access_grant.sandbox_generation
      AND sandbox.observed_generation = sandbox.generation
      AND sandbox.desired_state = 'running' AND sandbox.observed_state = 'running'
      AND NOT sandbox.writer_released AND sandbox.runtime_state = 'Running'
    FOR UPDATE OF access_grant;
    IF NOT FOUND THEN
        RAISE EXCEPTION USING ERRCODE = '28000', MESSAGE = 'sandbox access grant is unavailable';
    END IF;
    IF NOT EXISTS (
        SELECT 1 FROM cloud_agents.sandbox_preview_ports AS preview
        WHERE preview.tenant_id = cloud_agents.require_tenant_id()
          AND preview.project_uid = p_project AND preview.grant_uid = p_grant
          AND preview.port = p_port AND preview.revoked_at IS NULL
    ) AND (SELECT count(*) FROM cloud_agents.sandbox_preview_ports AS preview
        WHERE preview.tenant_id = cloud_agents.require_tenant_id()
          AND preview.project_uid = p_project AND preview.grant_uid = p_grant
          AND preview.revoked_at IS NULL) >= 32 THEN
        RAISE EXCEPTION USING ERRCODE = '54000', MESSAGE = 'sandbox Preview port limit exceeded';
    END IF;
    INSERT INTO cloud_agents.sandbox_preview_ports (
        tenant_id, project_uid, grant_uid, port, registered_at, revoked_at
    ) VALUES (
        cloud_agents.require_tenant_id(), p_project, p_grant, p_port, registered_at_value, NULL
    ) ON CONFLICT ON CONSTRAINT sandbox_preview_ports_pkey DO UPDATE SET
        registered_at = excluded.registered_at, revoked_at = NULL
    WHERE sandbox_preview_ports.revoked_at IS NOT NULL;
    RETURN QUERY SELECT preview.port, grant_value.sandbox_uid, grant_value.sandbox_generation,
        preview.registered_at
    FROM cloud_agents.sandbox_preview_ports AS preview
    WHERE preview.tenant_id = cloud_agents.require_tenant_id()
      AND preview.project_uid = p_project AND preview.grant_uid = p_grant
      AND preview.port = p_port AND preview.revoked_at IS NULL;
END;
$$;

CREATE FUNCTION cloud_agents.revoke_sandbox_preview_port_v1(
    p_project text, p_grant text, p_port integer, p_token_digest text
)
RETURNS timestamptz
LANGUAGE plpgsql SECURITY DEFINER SET search_path = pg_catalog, cloud_agents
AS $$
DECLARE
    ignored text;
    revoked_at_value timestamptz;
BEGIN
    ignored := cloud_agents.require_runtime_mutation_principal();
    IF NOT cloud_agents.is_valid_identifier(p_project) OR NOT cloud_agents.is_valid_identifier(p_grant)
       OR p_port NOT BETWEEN 1024 AND 65535 OR p_port = 44772
       OR p_token_digest !~ '^sha256:[0-9a-f]{64}$' THEN
        RAISE EXCEPTION USING ERRCODE = '22023', MESSAGE = 'sandbox Preview port input is invalid';
    END IF;
    IF NOT EXISTS (
        SELECT 1 FROM cloud_agents.sandbox_access_grants AS access_grant
        JOIN cloud_agents.sandbox_sessions AS sandbox
          ON sandbox.tenant_id = access_grant.tenant_id AND sandbox.project_uid = access_grant.project_uid
         AND sandbox.sandbox_uid = access_grant.sandbox_uid
        WHERE access_grant.tenant_id = cloud_agents.require_tenant_id()
          AND access_grant.project_uid = p_project AND access_grant.grant_uid = p_grant
          AND access_grant.token_digest = p_token_digest AND access_grant.status = 'active'
          AND access_grant.expires_at > clock_timestamp()
          AND sandbox.generation = access_grant.sandbox_generation
          AND sandbox.observed_generation = sandbox.generation
          AND sandbox.desired_state = 'running' AND sandbox.observed_state = 'running'
          AND NOT sandbox.writer_released AND sandbox.runtime_state = 'Running'
    ) THEN
        RAISE EXCEPTION USING ERRCODE = '28000', MESSAGE = 'sandbox access grant is unavailable';
    END IF;
    SELECT preview.revoked_at INTO revoked_at_value
    FROM cloud_agents.sandbox_preview_ports AS preview
    WHERE preview.tenant_id = cloud_agents.require_tenant_id()
      AND preview.project_uid = p_project AND preview.grant_uid = p_grant AND preview.port = p_port
    FOR UPDATE;
    IF NOT FOUND THEN
        RAISE EXCEPTION USING ERRCODE = '23503', MESSAGE = 'sandbox Preview port was not found';
    END IF;
    IF revoked_at_value IS NULL THEN
        revoked_at_value := transaction_timestamp();
        UPDATE cloud_agents.sandbox_preview_ports AS preview SET revoked_at = revoked_at_value
        WHERE preview.tenant_id = cloud_agents.require_tenant_id()
          AND preview.project_uid = p_project AND preview.grant_uid = p_grant AND preview.port = p_port;
    END IF;
    RETURN revoked_at_value;
END;
$$;

REVOKE ALL ON FUNCTION cloud_agents.register_sandbox_preview_port_v1(text,text,integer,text) FROM PUBLIC;
REVOKE ALL ON FUNCTION cloud_agents.revoke_sandbox_preview_port_v1(text,text,integer,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION cloud_agents.register_sandbox_preview_port_v1(text,text,integer,text) TO cloud_agents_runtime;
GRANT EXECUTE ON FUNCTION cloud_agents.revoke_sandbox_preview_port_v1(text,text,integer,text) TO cloud_agents_runtime;
