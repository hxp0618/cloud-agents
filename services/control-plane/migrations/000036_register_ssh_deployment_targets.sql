ALTER TABLE cloud_agents.deployment_targets
    DROP CONSTRAINT deployment_targets_kind;
ALTER TABLE cloud_agents.deployment_targets
    ADD CONSTRAINT deployment_targets_kind CHECK (target_kind IN ('docker', 'kubernetes', 'ssh'));

CREATE OR REPLACE FUNCTION cloud_agents.register_deployment_target_v2(
    p_tenant_id text, p_project_uid text, p_target_uid text, p_target_name text,
    p_target_kind text, p_endpoint text, p_credential_ref text,
    p_idempotency_key text, p_request_digest text
)
RETURNS TABLE (
    target_uid text, target_name text, target_kind text, endpoint text, credential_ref text,
    generation bigint, observed_phase text, api_version text, engine_version text,
    target_os text, target_arch text, stable_error_code text, last_probe_at timestamptz,
    resource_version bigint, created_at timestamptz, updated_at timestamptz
)
LANGUAGE plpgsql SECURITY DEFINER SET search_path = pg_catalog, cloud_agents
AS $cloud_agents_function$
DECLARE
    ignored_principal text;
    mutation_at timestamptz;
    existing cloud_agents.deployment_targets%ROWTYPE;
BEGIN
    ignored_principal := cloud_agents.require_runtime_mutation_principal();
    mutation_at := pg_catalog.transaction_timestamp();
    IF p_tenant_id IS DISTINCT FROM cloud_agents.require_tenant_id()
        OR NOT cloud_agents.is_valid_identifier(p_project_uid)
        OR NOT cloud_agents.is_valid_identifier(p_target_uid)
        OR NOT cloud_agents.is_valid_identifier(p_target_name)
        OR p_target_kind NOT IN ('docker', 'kubernetes', 'ssh')
        OR pg_catalog.octet_length(p_endpoint) NOT BETWEEN 7 AND 2048
        OR NOT (
            p_target_kind = 'ssh' AND p_endpoint ~ '^ssh://[^/?#[:space:]@]+/?$'
            OR p_target_kind IN ('docker', 'kubernetes') AND p_endpoint ~ '^https://[^/?#[:space:]@]+/?$'
        )
        OR NOT cloud_agents.is_valid_identifier(p_credential_ref)
        OR p_idempotency_key !~ '^[A-Za-z0-9._~-]{16,128}$'
        OR p_request_digest !~ '^sha256:[0-9a-f]{64}$'
    THEN RAISE EXCEPTION USING ERRCODE = '22023', MESSAGE = 'deployment target input is invalid'; END IF;
    SELECT target.* INTO existing FROM cloud_agents.deployment_targets AS target
    WHERE target.tenant_id = p_tenant_id AND target.project_uid = p_project_uid
        AND target.create_idempotency_key = p_idempotency_key FOR UPDATE;
    IF FOUND THEN
        IF existing.create_request_digest IS DISTINCT FROM p_request_digest THEN
            RAISE EXCEPTION USING ERRCODE = '23505', MESSAGE = 'deployment target idempotency conflict';
        END IF;
    ELSE
        PERFORM 1 FROM cloud_agents.projects AS project
        WHERE project.tenant_id = p_tenant_id AND project.project_uid = p_project_uid AND project.state = 'active'
        FOR KEY SHARE;
        IF NOT FOUND THEN RAISE EXCEPTION USING ERRCODE = '23503', MESSAGE = 'project is absent or inactive'; END IF;
        INSERT INTO cloud_agents.deployment_targets (
            tenant_id, tenant_ref_id, project_uid, target_uid, target_name, target_kind, endpoint, credential_ref,
            generation, observed_phase, api_version, engine_version, target_os, target_arch, stable_error_code,
            resource_version, create_idempotency_key, create_request_digest, created_at, updated_at
        ) VALUES (
            p_tenant_id, p_tenant_id, p_project_uid, p_target_uid, p_target_name, p_target_kind, p_endpoint, p_credential_ref,
            1, 'unprobed', '', '', '', '', '', 1, p_idempotency_key, p_request_digest, mutation_at, mutation_at
        ) RETURNING deployment_targets.* INTO existing;
    END IF;
    target_uid := existing.target_uid; target_name := existing.target_name; target_kind := existing.target_kind;
    endpoint := existing.endpoint; credential_ref := existing.credential_ref; generation := existing.generation;
    observed_phase := existing.observed_phase; api_version := existing.api_version; engine_version := existing.engine_version;
    target_os := existing.target_os; target_arch := existing.target_arch; stable_error_code := existing.stable_error_code;
    last_probe_at := existing.last_probe_at; resource_version := existing.resource_version;
    created_at := existing.created_at; updated_at := existing.updated_at;
    RETURN NEXT;
END;
$cloud_agents_function$;

ALTER FUNCTION cloud_agents.register_deployment_target_v2(text, text, text, text, text, text, text, text, text)
    OWNER TO cloud_agents_migration_owner;
REVOKE ALL ON FUNCTION cloud_agents.register_deployment_target_v2(text, text, text, text, text, text, text, text, text)
    FROM PUBLIC;
GRANT EXECUTE ON FUNCTION cloud_agents.register_deployment_target_v2(text, text, text, text, text, text, text, text, text)
    TO cloud_agents_runtime;
