-- Target Drain is a durable new-work admission barrier, not a runtime kill switch.
-- All public Session/Turn/Execution mutation functions pass through these table guards.
CREATE FUNCTION cloud_agents.guard_managed_agent_target_admission_v1()
RETURNS trigger
LANGUAGE plpgsql SECURITY DEFINER SET search_path = pg_catalog, cloud_agents
AS $$
DECLARE
    lease_id text;
    lease_generation bigint;
    target_id text;
    scheduling_state text;
    environment cloud_agents.managed_host_environment_leases%ROWTYPE;
BEGIN
    -- Settlement, cancellation, interaction replies and idempotent running replays stay valid.
    IF TG_OP = 'UPDATE' AND (NEW.state NOT IN ('queued', 'running') OR NEW.state = OLD.state) THEN
        RETURN NEW;
    END IF;
    IF TG_TABLE_NAME = 'managed_agent_sessions' THEN
        lease_id := NEW.environment_lease_uid;
        lease_generation := NEW.environment_generation;
    ELSE
        SELECT session.environment_lease_uid, session.environment_generation
        INTO lease_id, lease_generation
        FROM cloud_agents.managed_agent_sessions AS session
        WHERE session.tenant_id = NEW.tenant_id AND session.project_uid = NEW.project_uid
            AND session.session_uid = NEW.session_uid;
        IF NOT FOUND THEN
            RAISE EXCEPTION USING ERRCODE = '23503', MESSAGE = 'managed agent session is absent';
        END IF;
    END IF;
    -- Legacy local sessions have no Target and are outside this Target's admission authority.
    IF lease_id IS NULL THEN RETURN NEW; END IF;

    SELECT lease.deployment_target_uid INTO target_id
    FROM cloud_agents.managed_host_environment_leases AS lease
    WHERE lease.tenant_id = NEW.tenant_id AND lease.project_uid = NEW.project_uid AND lease.lease_uid = lease_id;
    IF target_id IS NULL THEN
        RAISE EXCEPTION USING ERRCODE = '23505', MESSAGE = 'managed agent environment is unavailable';
    END IF;
    -- Share the same Target row barrier as Drain/Resume. Never take an exclusive Lease lock:
    -- Drain locks Target then shares Leases; session creation already shares its Lease.
    SELECT target.scheduling_state INTO scheduling_state
    FROM cloud_agents.deployment_targets AS target
    WHERE target.tenant_id = NEW.tenant_id AND target.project_uid = NEW.project_uid AND target.target_uid = target_id
    FOR SHARE;
    IF NOT FOUND OR scheduling_state <> 'active' THEN
        RAISE EXCEPTION USING ERRCODE = '23505', MESSAGE = 'managed agent target is drained';
    END IF;
    SELECT lease.* INTO environment
    FROM cloud_agents.managed_host_environment_leases AS lease
    WHERE lease.tenant_id = NEW.tenant_id AND lease.project_uid = NEW.project_uid AND lease.lease_uid = lease_id
    FOR SHARE;
    IF NOT FOUND OR environment.deployment_target_uid IS DISTINCT FROM target_id
        OR environment.generation IS DISTINCT FROM lease_generation
        OR environment.desired_phase <> 'active' OR environment.observed_phase <> 'ready'
        OR environment.cleanup_phase <> 'none' OR environment.expires_at <= pg_catalog.clock_timestamp() THEN
        RAISE EXCEPTION USING ERRCODE = '23505', MESSAGE = 'managed agent environment is unavailable';
    END IF;
    RETURN NEW;
END;
$$;

ALTER FUNCTION cloud_agents.guard_managed_agent_target_admission_v1() OWNER TO cloud_agents_migration_owner;
REVOKE ALL ON FUNCTION cloud_agents.guard_managed_agent_target_admission_v1() FROM PUBLIC;

CREATE TRIGGER managed_agent_sessions_target_admission
BEFORE INSERT ON cloud_agents.managed_agent_sessions
FOR EACH ROW EXECUTE FUNCTION cloud_agents.guard_managed_agent_target_admission_v1();
CREATE TRIGGER managed_agent_turns_target_admission
BEFORE INSERT ON cloud_agents.managed_agent_turns
FOR EACH ROW EXECUTE FUNCTION cloud_agents.guard_managed_agent_target_admission_v1();
CREATE TRIGGER managed_agent_executions_target_admission
BEFORE INSERT OR UPDATE OF state ON cloud_agents.managed_agent_executions
FOR EACH ROW EXECUTE FUNCTION cloud_agents.guard_managed_agent_target_admission_v1();
