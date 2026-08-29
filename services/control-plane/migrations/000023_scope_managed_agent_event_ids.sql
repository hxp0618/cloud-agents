-- Keep the per-session sequence used by cursors, but derive the stored event
-- identifier from the complete scope and sequence so equal sequence values in
-- two sessions cannot collide on the tenant-wide event primary key.
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
    event_value := 'managed-agent-event-' || pg_catalog.encode(
        pg_catalog.sha256(
            pg_catalog.convert_to(
                pg_catalog.octet_length('cloud-agents/managed-agent-events/event-id/v1')::text || ':'
                    || 'cloud-agents/managed-agent-events/event-id/v1'
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

