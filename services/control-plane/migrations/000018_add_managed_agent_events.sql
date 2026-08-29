-- Durable Managed Agent lifecycle events. The existing state writers call the
-- append function in their transaction, so a committed mutation and event
-- become visible together without triggers or a second writer.
CREATE TABLE cloud_agents.managed_agent_events (
    tenant_id text NOT NULL,
    tenant_ref_id text NOT NULL,
    project_uid text NOT NULL,
    session_uid text NOT NULL,
    event_sequence bigint NOT NULL,
    event_uid text NOT NULL,
    operation text NOT NULL,
    resource text NOT NULL,
    turn_uid text,
    execution_uid text,
    generation bigint NOT NULL,
    mutation_digest text NOT NULL,
    input_digest text,
    result_digest text,
    error_code text,
    changes jsonb NOT NULL,
    occurred_at timestamptz NOT NULL,
    PRIMARY KEY (tenant_id, event_uid),
    UNIQUE (tenant_id, project_uid, session_uid, event_sequence),
    CONSTRAINT managed_agent_events_tenant_ref CHECK (tenant_id = tenant_ref_id),
    CONSTRAINT managed_agent_events_project CHECK (cloud_agents.is_valid_identifier(project_uid)),
    CONSTRAINT managed_agent_events_session CHECK (cloud_agents.is_valid_identifier(session_uid)),
    CONSTRAINT managed_agent_events_sequence CHECK (event_sequence > 0),
    CONSTRAINT managed_agent_events_uid CHECK (cloud_agents.is_valid_identifier(event_uid)),
    CONSTRAINT managed_agent_events_operation CHECK (operation IN (
        'session.create', 'session.close', 'turn.create', 'execution.create',
        'execution.start', 'execution.complete', 'execution.fail',
        'turn.interrupt', 'turn.cancel'
    )),
    CONSTRAINT managed_agent_events_resource CHECK (resource IN ('Session', 'Turn', 'Execution')),
    CONSTRAINT managed_agent_events_generation CHECK (generation >= 0),
    CONSTRAINT managed_agent_events_mutation_digest CHECK (mutation_digest ~ '^sha256:[0-9a-f]{64}$'),
    CONSTRAINT managed_agent_events_input_digest CHECK (input_digest IS NULL OR input_digest ~ '^sha256:[0-9a-f]{64}$'),
    CONSTRAINT managed_agent_events_result_digest CHECK (result_digest IS NULL OR result_digest ~ '^sha256:[0-9a-f]{64}$'),
    CONSTRAINT managed_agent_events_error_code CHECK (error_code IS NULL OR error_code ~ '^[a-z0-9_-]{1,64}$'),
    CONSTRAINT managed_agent_events_changes CHECK (jsonb_typeof(changes) = 'array' AND jsonb_array_length(changes) BETWEEN 1 AND 4),
    CONSTRAINT managed_agent_events_tenant_fk
        FOREIGN KEY (tenant_id, tenant_ref_id)
        REFERENCES cloud_agents.platform_tenants (tenant_id, tenant_uid)
        ON UPDATE RESTRICT ON DELETE RESTRICT
);

ALTER TABLE cloud_agents.managed_agent_events OWNER TO cloud_agents_migration_owner;
ALTER TABLE cloud_agents.managed_agent_events ENABLE ROW LEVEL SECURITY;
ALTER TABLE cloud_agents.managed_agent_events FORCE ROW LEVEL SECURITY;

CREATE POLICY managed_agent_events_runtime_tenant
    ON cloud_agents.managed_agent_events
    TO cloud_agents_runtime
    USING (tenant_id = cloud_agents.require_tenant_id());

CREATE POLICY managed_agent_events_migration_owner
    ON cloud_agents.managed_agent_events
    TO cloud_agents_migration_owner
    USING (true) WITH CHECK (true);

REVOKE ALL ON TABLE cloud_agents.managed_agent_events FROM PUBLIC;
REVOKE ALL ON TABLE cloud_agents.managed_agent_events FROM cloud_agents_bootstrap_admin;
GRANT SELECT ON TABLE cloud_agents.managed_agent_events TO cloud_agents_runtime;

CREATE FUNCTION cloud_agents.append_managed_agent_event_v1(
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
    PERFORM pg_catalog.pg_advisory_xact_lock(pg_catalog.hashtextextended(p_tenant_id || chr(0) || p_project_uid || chr(0) || p_session_uid, 0));
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

ALTER FUNCTION cloud_agents.append_managed_agent_event_v1(text, text, text, text, text, text, text, bigint, text, text, text, text, jsonb) OWNER TO cloud_agents_migration_owner;
REVOKE ALL ON FUNCTION cloud_agents.append_managed_agent_event_v1(text, text, text, text, text, text, text, bigint, text, text, text, text, jsonb) FROM PUBLIC;
