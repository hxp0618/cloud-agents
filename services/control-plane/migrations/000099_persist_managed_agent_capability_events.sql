-- Extend the existing Managed Agent event stream with redacted MCP/Skill
-- facts. Payloads, source bytes, endpoint material, and credentials remain
-- outside the database projection.

ALTER TABLE cloud_agents.managed_agent_events
    ADD COLUMN capability_server_uid text,
    ADD COLUMN capability_bundle_uid text,
    ADD COLUMN capability_version text,
    ADD COLUMN capability_digest text,
    ADD COLUMN capability_result text;

ALTER TABLE cloud_agents.managed_agent_events
    DROP CONSTRAINT managed_agent_events_operation;

ALTER TABLE cloud_agents.managed_agent_events
    DROP CONSTRAINT managed_agent_events_resource;

ALTER TABLE cloud_agents.managed_agent_events
    ADD CONSTRAINT managed_agent_events_operation CHECK (operation IN (
        'session.create', 'session.close', 'turn.create', 'execution.create',
        'execution.start', 'execution.reconnect', 'execution.restart',
        'execution.takeover', 'execution.recovery-blocked',
        'execution.reconcile', 'execution.receipt-rejected',
        'execution.complete', 'execution.fail', 'turn.interrupt', 'turn.cancel',
        'mcp.call', 'mcp.fail', 'mcp.revoke',
        'skill.load', 'skill.fail', 'skill.revoke'
    )),
    ADD CONSTRAINT managed_agent_events_resource CHECK (resource IN (
        'Session', 'Turn', 'Execution', 'McpServer', 'SkillBundle'
    )),
    ADD CONSTRAINT managed_agent_events_capability_shape CHECK (
        (
            operation NOT LIKE 'mcp.%' AND operation NOT LIKE 'skill.%'
            AND capability_server_uid IS NULL AND capability_bundle_uid IS NULL
            AND capability_version IS NULL AND capability_digest IS NULL
            AND capability_result IS NULL
        ) OR (
            operation LIKE 'mcp.%' AND resource = 'McpServer'
            AND cloud_agents.is_valid_identifier(capability_server_uid)
            AND capability_bundle_uid IS NULL
            AND cloud_agents.is_valid_identifier(capability_version)
            AND capability_digest ~ '^sha256:[0-9a-f]{64}$'
            AND capability_result IN ('accepted', 'succeeded', 'failed', 'revoked')
        ) OR (
            operation LIKE 'skill.%' AND resource = 'SkillBundle'
            AND capability_server_uid IS NULL
            AND cloud_agents.is_valid_identifier(capability_bundle_uid)
            AND cloud_agents.is_valid_identifier(capability_version)
            AND capability_digest ~ '^sha256:[0-9a-f]{64}$'
            AND capability_result IN ('accepted', 'succeeded', 'failed', 'revoked')
        )
    );

CREATE FUNCTION cloud_agents.append_managed_agent_capability_event_v1(
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
    p_capability_id text,
    p_capability_version text,
    p_capability_digest text,
    p_capability_result text
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
        OR p_operation IS NULL OR p_operation NOT IN (
            'mcp.call', 'mcp.fail', 'mcp.revoke',
            'skill.load', 'skill.fail', 'skill.revoke')
        OR p_resource IS NULL OR p_resource NOT IN ('McpServer', 'SkillBundle')
        OR p_operation LIKE 'mcp.%' AND p_resource <> 'McpServer'
        OR p_operation LIKE 'skill.%' AND p_resource <> 'SkillBundle'
        OR p_generation IS NULL OR p_generation < 1
        OR p_mutation_digest IS NULL OR p_mutation_digest !~ '^sha256:[0-9a-f]{64}$'
        OR (p_input_digest IS NOT NULL AND p_input_digest !~ '^sha256:[0-9a-f]{64}$')
        OR (p_result_digest IS NOT NULL AND p_result_digest !~ '^sha256:[0-9a-f]{64}$')
        OR (p_error_code IS NOT NULL AND p_error_code !~ '^[a-z0-9_-]{1,64}$')
        OR NOT cloud_agents.is_valid_identifier(p_capability_id)
        OR NOT cloud_agents.is_valid_identifier(p_capability_version)
        OR p_capability_digest IS NULL OR p_capability_digest !~ '^sha256:[0-9a-f]{64}$'
        OR p_capability_result IS NULL OR p_capability_result NOT IN ('accepted', 'succeeded', 'failed', 'revoked')
        OR p_operation IN ('mcp.revoke', 'skill.revoke') AND p_capability_result <> 'revoked'
        OR p_operation IN ('mcp.fail', 'skill.fail') AND p_capability_result <> 'failed'
    THEN
        RAISE EXCEPTION USING ERRCODE = '22023', MESSAGE = 'managed agent capability event input is invalid';
    END IF;
    PERFORM pg_catalog.pg_advisory_xact_lock(
        pg_catalog.hashtextextended(
            pg_catalog.jsonb_build_array(
                'cloud-agents-managed-agent-event-lock/v1',
                p_tenant_id, p_project_uid, p_session_uid
            )::text,
            0
        )
    );
    SELECT event_uid INTO event_value
    FROM cloud_agents.managed_agent_events
    WHERE tenant_id = p_tenant_id AND project_uid = p_project_uid
        AND session_uid = p_session_uid AND operation = p_operation
        AND resource = p_resource AND capability_server_uid IS NOT DISTINCT FROM CASE WHEN p_resource = 'McpServer' THEN p_capability_id END
        AND capability_bundle_uid IS NOT DISTINCT FROM CASE WHEN p_resource = 'SkillBundle' THEN p_capability_id END
        AND mutation_digest = p_mutation_digest;
    IF event_value IS NOT NULL THEN
        RETURN event_value;
    END IF;
    SELECT COALESCE(pg_catalog.max(event_sequence), 0) + 1 INTO sequence_value
    FROM cloud_agents.managed_agent_events
    WHERE tenant_id = p_tenant_id AND project_uid = p_project_uid AND session_uid = p_session_uid;
    event_value := 'managed-agent-event-' || pg_catalog.encode(
        pg_catalog.sha256(
            pg_catalog.convert_to(
                pg_catalog.octet_length('cloud-agents-managed-agent-event-id/v1')::text || ':'
                    || 'cloud-agents-managed-agent-event-id/v1'
                    || pg_catalog.octet_length(p_tenant_id)::text || ':' || p_tenant_id
                    || pg_catalog.octet_length(p_project_uid)::text || ':' || p_project_uid
                    || pg_catalog.octet_length(p_session_uid)::text || ':' || p_session_uid
                    || pg_catalog.octet_length(sequence_value::text)::text || ':' || sequence_value::text,
                'UTF8')),
        'hex');
    INSERT INTO cloud_agents.managed_agent_events (
        tenant_id, tenant_ref_id, project_uid, session_uid, event_sequence,
        event_uid, operation, resource, turn_uid, execution_uid, generation,
        mutation_digest, input_digest, result_digest, error_code, changes,
        occurred_at, capability_server_uid, capability_bundle_uid,
        capability_version, capability_digest, capability_result
    ) VALUES (
        p_tenant_id, p_tenant_id, p_project_uid, p_session_uid, sequence_value,
        event_value, p_operation, p_resource, p_turn_uid, p_execution_uid,
        p_generation, p_mutation_digest, p_input_digest, p_result_digest,
        p_error_code,
        pg_catalog.jsonb_build_array(pg_catalog.jsonb_build_object(
            'resource', p_resource, 'from', '', 'to', p_capability_result, 'version', 1)),
        pg_catalog.transaction_timestamp(),
        CASE WHEN p_resource = 'McpServer' THEN p_capability_id END,
        CASE WHEN p_resource = 'SkillBundle' THEN p_capability_id END,
        p_capability_version, p_capability_digest, p_capability_result
    );
    RETURN event_value;
END;
$cloud_agents_function$;

ALTER FUNCTION cloud_agents.append_managed_agent_capability_event_v1(
    text, text, text, text, text, text, text, bigint, text, text, text,
    text, text, text, text, text
) OWNER TO cloud_agents_migration_owner;
REVOKE ALL ON FUNCTION cloud_agents.append_managed_agent_capability_event_v1(
    text, text, text, text, text, text, text, bigint, text, text, text,
    text, text, text, text, text
) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION cloud_agents.append_managed_agent_capability_event_v1(
    text, text, text, text, text, text, text, bigint, text, text, text,
    text, text, text, text, text
) TO cloud_agents_runtime;
