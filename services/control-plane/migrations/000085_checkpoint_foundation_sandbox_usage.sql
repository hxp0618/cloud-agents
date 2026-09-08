CREATE TABLE cloud_agents.sandbox_usage_checkpoints (
    tenant_id text NOT NULL,
    project_uid text NOT NULL,
    sandbox_uid text NOT NULL,
    sandbox_generation bigint NOT NULL CHECK (sandbox_generation > 0),
    runtime_uid text NOT NULL CHECK (cloud_agents.is_valid_identifier(runtime_uid)),
    cpu_millis bigint NOT NULL CHECK (cpu_millis BETWEEN 100 AND 64000),
    memory_bytes bigint NOT NULL CHECK (memory_bytes BETWEEN 134217728 AND 1099511627776),
    allocated_milliseconds numeric(40, 0) NOT NULL DEFAULT 0 CHECK (allocated_milliseconds >= 0),
    cpu_millis_milliseconds numeric(40, 0) NOT NULL DEFAULT 0 CHECK (cpu_millis_milliseconds >= 0),
    memory_byte_milliseconds numeric(40, 0) NOT NULL DEFAULT 0 CHECK (memory_byte_milliseconds >= 0),
    started_at timestamptz NOT NULL,
    checkpointed_at timestamptz NOT NULL,
    finalized_at timestamptz,
    PRIMARY KEY (tenant_id, project_uid, sandbox_uid, sandbox_generation, runtime_uid),
    FOREIGN KEY (tenant_id, project_uid, sandbox_uid) REFERENCES cloud_agents.sandbox_sessions
        ON UPDATE RESTRICT ON DELETE RESTRICT,
    CHECK (checkpointed_at >= started_at AND (finalized_at IS NULL OR finalized_at = checkpointed_at))
);
CREATE INDEX sandbox_usage_checkpoints_active_idx
    ON cloud_agents.sandbox_usage_checkpoints (finalized_at, checkpointed_at, tenant_id, project_uid, sandbox_uid);

ALTER TABLE cloud_agents.sandbox_usage_checkpoints OWNER TO cloud_agents_migration_owner;
ALTER TABLE cloud_agents.sandbox_usage_checkpoints ENABLE ROW LEVEL SECURITY;
ALTER TABLE cloud_agents.sandbox_usage_checkpoints FORCE ROW LEVEL SECURITY;
CREATE POLICY sandbox_usage_checkpoints_runtime ON cloud_agents.sandbox_usage_checkpoints TO cloud_agents_runtime
    USING (tenant_id = cloud_agents.require_tenant_id()) WITH CHECK (tenant_id = cloud_agents.require_tenant_id());
CREATE POLICY sandbox_usage_checkpoints_owner ON cloud_agents.sandbox_usage_checkpoints TO cloud_agents_migration_owner
    USING (true) WITH CHECK (true);
REVOKE ALL ON TABLE cloud_agents.sandbox_usage_checkpoints FROM PUBLIC;
GRANT SELECT ON TABLE cloud_agents.sandbox_usage_checkpoints TO cloud_agents_runtime;

CREATE FUNCTION cloud_agents.capture_foundation_sandbox_usage_v1()
RETURNS trigger LANGUAGE plpgsql SECURITY DEFINER SET search_path = pg_catalog, cloud_agents
AS $$
DECLARE
    captured_at timestamptz := transaction_timestamp();
BEGIN
    IF OLD.runtime_uid IS NOT NULL AND (
        NEW.runtime_uid IS NULL OR NEW.runtime_uid IS DISTINCT FROM OLD.runtime_uid
        OR NEW.runtime_generation IS DISTINCT FROM OLD.runtime_generation
    ) THEN
        UPDATE cloud_agents.sandbox_usage_checkpoints AS usage SET
            allocated_milliseconds = usage.allocated_milliseconds
                + pg_catalog.floor(EXTRACT(epoch FROM (GREATEST(captured_at, usage.checkpointed_at) - usage.checkpointed_at)) * 1000),
            cpu_millis_milliseconds = usage.cpu_millis_milliseconds + usage.cpu_millis
                * pg_catalog.floor(EXTRACT(epoch FROM (GREATEST(captured_at, usage.checkpointed_at) - usage.checkpointed_at)) * 1000),
            memory_byte_milliseconds = usage.memory_byte_milliseconds + usage.memory_bytes
                * pg_catalog.floor(EXTRACT(epoch FROM (GREATEST(captured_at, usage.checkpointed_at) - usage.checkpointed_at)) * 1000),
            checkpointed_at = GREATEST(captured_at, usage.checkpointed_at),
            finalized_at = GREATEST(captured_at, usage.checkpointed_at)
        WHERE usage.tenant_id = OLD.tenant_id AND usage.project_uid = OLD.project_uid
          AND usage.sandbox_uid = OLD.sandbox_uid AND usage.sandbox_generation = OLD.runtime_generation
          AND usage.runtime_uid = OLD.runtime_uid AND usage.finalized_at IS NULL;
        IF NOT FOUND THEN
            INSERT INTO cloud_agents.sandbox_usage_checkpoints (
                tenant_id, project_uid, sandbox_uid, sandbox_generation, runtime_uid,
                cpu_millis, memory_bytes, allocated_milliseconds,
                cpu_millis_milliseconds, memory_byte_milliseconds,
                started_at, checkpointed_at, finalized_at
            ) VALUES (
                OLD.tenant_id, OLD.project_uid, OLD.sandbox_uid, OLD.runtime_generation, OLD.runtime_uid,
                OLD.cpu_millis, OLD.memory_bytes,
                pg_catalog.floor(EXTRACT(epoch FROM (captured_at - LEAST(OLD.observed_at, captured_at))) * 1000),
                OLD.cpu_millis * pg_catalog.floor(EXTRACT(epoch FROM (captured_at - LEAST(OLD.observed_at, captured_at))) * 1000),
                OLD.memory_bytes * pg_catalog.floor(EXTRACT(epoch FROM (captured_at - LEAST(OLD.observed_at, captured_at))) * 1000),
                LEAST(OLD.observed_at, captured_at), captured_at, captured_at
            );
        END IF;
    END IF;

    IF NEW.runtime_uid IS NOT NULL AND (
        OLD.runtime_uid IS NULL OR NEW.runtime_uid IS DISTINCT FROM OLD.runtime_uid
        OR NEW.runtime_generation IS DISTINCT FROM OLD.runtime_generation
    ) THEN
        IF NEW.runtime_generation IS NULL THEN
            RAISE EXCEPTION USING ERRCODE = '40001', MESSAGE = 'sandbox usage runtime generation drifted';
        END IF;
        INSERT INTO cloud_agents.sandbox_usage_checkpoints (
            tenant_id, project_uid, sandbox_uid, sandbox_generation, runtime_uid,
            cpu_millis, memory_bytes, started_at, checkpointed_at
        ) VALUES (
            NEW.tenant_id, NEW.project_uid, NEW.sandbox_uid, NEW.runtime_generation, NEW.runtime_uid,
            NEW.cpu_millis, NEW.memory_bytes, captured_at, captured_at
        );
    END IF;
    RETURN NEW;
END;
$$;
CREATE TRIGGER sandbox_sessions_capture_usage
    BEFORE UPDATE OF runtime_uid ON cloud_agents.sandbox_sessions
    FOR EACH ROW EXECUTE FUNCTION cloud_agents.capture_foundation_sandbox_usage_v1();

CREATE FUNCTION cloud_agents.checkpoint_foundation_sandbox_usage_v1(p_limit integer)
RETURNS integer LANGUAGE plpgsql SECURITY DEFINER SET search_path = pg_catalog, cloud_agents
AS $$
DECLARE
    checkpoint_at timestamptz := transaction_timestamp();
    checkpoint_count integer;
BEGIN
    PERFORM cloud_agents.require_runtime_mutation_principal();
    IF p_limit IS NULL OR p_limit NOT BETWEEN 1 AND 500 THEN
        RAISE EXCEPTION USING ERRCODE = '22023', MESSAGE = 'sandbox usage checkpoint input is invalid';
    END IF;
    INSERT INTO cloud_agents.sandbox_usage_checkpoints (
        tenant_id, project_uid, sandbox_uid, sandbox_generation, runtime_uid,
        cpu_millis, memory_bytes, allocated_milliseconds,
        cpu_millis_milliseconds, memory_byte_milliseconds, started_at, checkpointed_at
    ) SELECT sandbox.tenant_id, sandbox.project_uid, sandbox.sandbox_uid, sandbox.runtime_generation,
        sandbox.runtime_uid, sandbox.cpu_millis, sandbox.memory_bytes,
        pg_catalog.floor(EXTRACT(epoch FROM (checkpoint_at - LEAST(sandbox.observed_at, checkpoint_at))) * 1000),
        sandbox.cpu_millis * pg_catalog.floor(EXTRACT(epoch FROM (checkpoint_at - LEAST(sandbox.observed_at, checkpoint_at))) * 1000),
        sandbox.memory_bytes * pg_catalog.floor(EXTRACT(epoch FROM (checkpoint_at - LEAST(sandbox.observed_at, checkpoint_at))) * 1000),
        LEAST(sandbox.observed_at, checkpoint_at), checkpoint_at
    FROM cloud_agents.sandbox_sessions AS sandbox
    WHERE sandbox.runtime_uid IS NOT NULL AND sandbox.runtime_generation IS NOT NULL
      AND NOT EXISTS (
          SELECT 1 FROM cloud_agents.sandbox_usage_checkpoints AS usage
          WHERE usage.tenant_id = sandbox.tenant_id AND usage.project_uid = sandbox.project_uid
            AND usage.sandbox_uid = sandbox.sandbox_uid AND usage.sandbox_generation = sandbox.runtime_generation
            AND usage.runtime_uid = sandbox.runtime_uid
      )
    ORDER BY sandbox.tenant_id, sandbox.project_uid, sandbox.sandbox_uid
    LIMIT p_limit
    ON CONFLICT DO NOTHING;
    WITH candidates AS (
        SELECT usage.tenant_id, usage.project_uid, usage.sandbox_uid,
            usage.sandbox_generation, usage.runtime_uid
        FROM cloud_agents.sandbox_usage_checkpoints AS usage
        JOIN cloud_agents.sandbox_sessions AS sandbox
          ON sandbox.tenant_id = usage.tenant_id AND sandbox.project_uid = usage.project_uid
         AND sandbox.sandbox_uid = usage.sandbox_uid
         AND sandbox.runtime_generation = usage.sandbox_generation
         AND sandbox.runtime_uid = usage.runtime_uid
        WHERE usage.finalized_at IS NULL
          AND usage.checkpointed_at <= checkpoint_at - interval '1 minute'
        ORDER BY usage.checkpointed_at, usage.tenant_id, usage.project_uid, usage.sandbox_uid
        FOR UPDATE OF usage SKIP LOCKED
        LIMIT p_limit
    ), updated AS (
        UPDATE cloud_agents.sandbox_usage_checkpoints AS usage SET
            allocated_milliseconds = usage.allocated_milliseconds
                + pg_catalog.floor(EXTRACT(epoch FROM (checkpoint_at - usage.checkpointed_at)) * 1000),
            cpu_millis_milliseconds = usage.cpu_millis_milliseconds + usage.cpu_millis
                * pg_catalog.floor(EXTRACT(epoch FROM (checkpoint_at - usage.checkpointed_at)) * 1000),
            memory_byte_milliseconds = usage.memory_byte_milliseconds + usage.memory_bytes
                * pg_catalog.floor(EXTRACT(epoch FROM (checkpoint_at - usage.checkpointed_at)) * 1000),
            checkpointed_at = checkpoint_at
        FROM candidates AS candidate
        WHERE usage.tenant_id = candidate.tenant_id AND usage.project_uid = candidate.project_uid
          AND usage.sandbox_uid = candidate.sandbox_uid
          AND usage.sandbox_generation = candidate.sandbox_generation
          AND usage.runtime_uid = candidate.runtime_uid
        RETURNING 1
    ) SELECT pg_catalog.count(*)::integer INTO checkpoint_count FROM updated;
    RETURN checkpoint_count;
END;
$$;

ALTER FUNCTION cloud_agents.capture_foundation_sandbox_usage_v1() OWNER TO cloud_agents_migration_owner;
ALTER FUNCTION cloud_agents.checkpoint_foundation_sandbox_usage_v1(integer) OWNER TO cloud_agents_migration_owner;
REVOKE ALL ON FUNCTION cloud_agents.capture_foundation_sandbox_usage_v1() FROM PUBLIC;
REVOKE ALL ON FUNCTION cloud_agents.checkpoint_foundation_sandbox_usage_v1(integer) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION cloud_agents.checkpoint_foundation_sandbox_usage_v1(integer) TO cloud_agents_runtime;
