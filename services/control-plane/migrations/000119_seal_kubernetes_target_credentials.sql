CREATE TABLE cloud_agents.deployment_target_credentials (
    tenant_id text NOT NULL,
    project_uid text NOT NULL,
    target_uid text NOT NULL,
    key_id text NOT NULL,
    sealed_credential bytea NOT NULL,
    created_at timestamptz NOT NULL,
    PRIMARY KEY (tenant_id, project_uid, target_uid),
    CONSTRAINT deployment_target_credentials_target_fk FOREIGN KEY (tenant_id, project_uid, target_uid)
        REFERENCES cloud_agents.deployment_targets (tenant_id, project_uid, target_uid)
        ON UPDATE RESTRICT ON DELETE RESTRICT,
    CONSTRAINT deployment_target_credentials_key_id CHECK (key_id ~ '^k1-[0-9a-f]{32}$'),
    CONSTRAINT deployment_target_credentials_sealed CHECK (
        pg_catalog.octet_length(sealed_credential) BETWEEN 29 AND 262144
    )
);

ALTER TABLE cloud_agents.deployment_target_credentials OWNER TO cloud_agents_migration_owner;
ALTER TABLE cloud_agents.deployment_target_credentials ENABLE ROW LEVEL SECURITY;
ALTER TABLE cloud_agents.deployment_target_credentials FORCE ROW LEVEL SECURITY;
CREATE POLICY deployment_target_credentials_runtime_tenant ON cloud_agents.deployment_target_credentials
    TO cloud_agents_runtime USING (tenant_id = cloud_agents.require_tenant_id())
    WITH CHECK (tenant_id = cloud_agents.require_tenant_id());
CREATE POLICY deployment_target_credentials_migration_owner ON cloud_agents.deployment_target_credentials
    TO cloud_agents_migration_owner USING (true) WITH CHECK (true);
REVOKE ALL ON TABLE cloud_agents.deployment_target_credentials FROM PUBLIC;
GRANT SELECT ON TABLE cloud_agents.deployment_target_credentials TO cloud_agents_runtime;

CREATE FUNCTION cloud_agents.store_deployment_target_credential_v1(
    p_tenant_id text, p_project_uid text, p_target_uid text,
    p_idempotency_key text, p_request_digest text, p_key_id text, p_sealed_credential bytea
)
RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path = pg_catalog, cloud_agents
AS $cloud_agents_function$
DECLARE
    ignored_principal text;
    existing cloud_agents.deployment_targets%ROWTYPE;
BEGIN
    ignored_principal := cloud_agents.require_runtime_mutation_principal();
    IF p_tenant_id IS DISTINCT FROM cloud_agents.require_tenant_id()
        OR NOT cloud_agents.is_valid_identifier(p_project_uid)
        OR NOT cloud_agents.is_valid_identifier(p_target_uid)
        OR p_idempotency_key !~ '^[A-Za-z0-9._~-]{16,128}$'
        OR p_request_digest !~ '^sha256:[0-9a-f]{64}$'
        OR p_key_id !~ '^k1-[0-9a-f]{32}$'
        OR p_sealed_credential IS NULL
        OR pg_catalog.octet_length(p_sealed_credential) NOT BETWEEN 29 AND 262144
    THEN RAISE EXCEPTION USING ERRCODE = '22023', MESSAGE = 'deployment target credential input is invalid'; END IF;
    SELECT target.* INTO existing FROM cloud_agents.deployment_targets AS target
    WHERE target.tenant_id = p_tenant_id AND target.project_uid = p_project_uid AND target.target_uid = p_target_uid
    FOR UPDATE;
    -- Only the registration that created the target may attach its credential.
    IF NOT FOUND OR existing.target_kind <> 'kubernetes'
        OR existing.create_idempotency_key IS DISTINCT FROM p_idempotency_key
        OR existing.create_request_digest IS DISTINCT FROM p_request_digest
    THEN RAISE EXCEPTION USING ERRCODE = '23505', MESSAGE = 'deployment target credential conflicts with registration'; END IF;
    INSERT INTO cloud_agents.deployment_target_credentials (
        tenant_id, project_uid, target_uid, key_id, sealed_credential, created_at
    ) VALUES (
        p_tenant_id, p_project_uid, p_target_uid, p_key_id, p_sealed_credential, pg_catalog.transaction_timestamp()
    ) ON CONFLICT (tenant_id, project_uid, target_uid) DO NOTHING;
    RETURN true;
END;
$cloud_agents_function$;

ALTER FUNCTION cloud_agents.store_deployment_target_credential_v1(text, text, text, text, text, text, bytea)
    OWNER TO cloud_agents_migration_owner;
REVOKE ALL ON FUNCTION cloud_agents.store_deployment_target_credential_v1(text, text, text, text, text, text, bytea)
    FROM PUBLIC;
GRANT EXECUTE ON FUNCTION cloud_agents.store_deployment_target_credential_v1(text, text, text, text, text, text, bytea)
    TO cloud_agents_runtime;
