CREATE OR REPLACE FUNCTION cloud_agents.is_valid_remote_worker_capabilities(p_values text[])
RETURNS boolean
LANGUAGE sql IMMUTABLE SET search_path = pg_catalog
AS $$
    SELECT pg_catalog.cardinality(p_values) BETWEEN 1 AND 16
       AND p_values <@ ARRAY[
           'docker','exec','files','network-dns-nft','preview','pty','ssh','workspace-volume'
       ]::text[]
       AND p_values = ARRAY(
           SELECT DISTINCT value
           FROM pg_catalog.unnest(p_values) AS value
           ORDER BY value
       )
$$;

CREATE FUNCTION cloud_agents.foundation_target_available_v2(
    p_tenant text, p_project text, p_target text, p_cpu bigint, p_memory bigint
)
RETURNS boolean
LANGUAGE sql STABLE SET search_path = pg_catalog, cloud_agents
AS $$
    SELECT p_cpu BETWEEN 100 AND 64000
       AND p_memory BETWEEN 134217728 AND 1099511627776
       AND EXISTS (
           SELECT 1
           FROM cloud_agents.deployment_targets AS target
           LEFT JOIN cloud_agents.remote_worker_enrollments AS enrollment
             ON enrollment.tenant_id = target.tenant_id
            AND enrollment.project_uid = target.project_uid
            AND enrollment.target_uid = target.target_uid
            AND target.target_kind = 'remote-worker'
           WHERE target.tenant_id = p_tenant AND target.project_uid = p_project
             AND target.target_uid = p_target AND target.scheduling_state = 'active'
             AND (
                 target.target_kind IN ('docker', 'kubernetes') AND target.observed_phase = 'ready'
                 OR target.target_kind = 'remote-worker' AND target.observed_phase = 'ready'
                    AND enrollment.state = 'enrolled' AND enrollment.certificate_state = 'active'
                    AND enrollment.certificate_not_after > clock_timestamp()
                    AND enrollment.node_heartbeat_expires_at > clock_timestamp()
                    AND enrollment.node_desired_state = 'active'
                    AND enrollment.node_observed_state = 'active'
                    AND enrollment.node_architecture IN ('amd64', 'arm64')
                    AND enrollment.node_capabilities @> ARRAY[
                        'docker','network-dns-nft','workspace-volume'
                    ]::text[]
                    AND enrollment.node_capacity_cpu_millis >= p_cpu
                    AND enrollment.node_capacity_memory_bytes >= p_memory
                    AND enrollment.node_capacity_disk_bytes >= 21474836480
             )
       )
$$;

CREATE OR REPLACE FUNCTION cloud_agents.foundation_target_available_v1(
    p_tenant text, p_project text, p_target text
)
RETURNS boolean
LANGUAGE sql STABLE SET search_path = pg_catalog, cloud_agents
AS $$
    SELECT cloud_agents.foundation_target_available_v2(
        p_tenant, p_project, p_target,
        COALESCE(pg_catalog.max(sandbox.cpu_millis), 100),
        COALESCE(pg_catalog.max(sandbox.memory_bytes), 134217728)
    )
    FROM cloud_agents.sandbox_sessions AS sandbox
    JOIN cloud_agents.workspace_volumes AS volume
      ON volume.tenant_id = sandbox.tenant_id AND volume.project_uid = sandbox.project_uid
     AND volume.workspace_uid = sandbox.workspace_uid
    WHERE volume.tenant_id = p_tenant AND volume.project_uid = p_project
      AND volume.target_uid = p_target AND sandbox.desired_state = 'running'
$$;

CREATE FUNCTION cloud_agents.guard_remote_worker_foundation_admission_v1()
RETURNS trigger
LANGUAGE plpgsql SECURITY DEFINER SET search_path = pg_catalog, cloud_agents
AS $$
DECLARE
    target_id text;
    required_cpu bigint;
    required_memory bigint;
BEGIN
    IF TG_TABLE_NAME = 'runtime_profiles' THEN
        IF NEW.status <> 'published' OR NEW.status IS NOT DISTINCT FROM OLD.status THEN
            RETURN NEW;
        END IF;
        target_id := NEW.target_uid;
        required_cpu := NEW.cpu_millis;
        required_memory := NEW.memory_bytes;
    ELSE
        IF NEW.runtime_profile_uid IS NULL OR NEW.desired_state <> 'running' THEN
            RETURN NEW;
        END IF;
        IF TG_OP = 'UPDATE'
           AND ROW(NEW.generation, NEW.desired_state, NEW.runtime_profile_uid,
                   NEW.runtime_profile_version, NEW.cpu_millis, NEW.memory_bytes)
               IS NOT DISTINCT FROM
               ROW(OLD.generation, OLD.desired_state, OLD.runtime_profile_uid,
                   OLD.runtime_profile_version, OLD.cpu_millis, OLD.memory_bytes) THEN
            RETURN NEW;
        END IF;
        SELECT profile.target_uid INTO target_id
        FROM cloud_agents.runtime_profiles AS profile
        WHERE profile.tenant_id = NEW.tenant_id AND profile.project_uid = NEW.project_uid
          AND profile.profile_uid = NEW.runtime_profile_uid
          AND profile.profile_version = NEW.runtime_profile_version
          AND profile.status IN ('published', 'disabled');
        required_cpu := NEW.cpu_millis;
        required_memory := NEW.memory_bytes;
    END IF;

    IF target_id IS NULL OR NOT cloud_agents.foundation_target_available_v2(
        NEW.tenant_id, NEW.project_uid, target_id, required_cpu, required_memory
    ) THEN
        RAISE EXCEPTION USING ERRCODE = '23503', MESSAGE = 'runtime profile target unavailable';
    END IF;
    RETURN NEW;
END;
$$;

CREATE TRIGGER runtime_profiles_remote_worker_foundation_admission
BEFORE UPDATE OF status ON cloud_agents.runtime_profiles
FOR EACH ROW EXECUTE FUNCTION cloud_agents.guard_remote_worker_foundation_admission_v1();

CREATE TRIGGER sandbox_sessions_remote_worker_foundation_profile_admission
BEFORE INSERT OR UPDATE OF runtime_profile_uid ON cloud_agents.sandbox_sessions
FOR EACH ROW EXECUTE FUNCTION cloud_agents.guard_remote_worker_foundation_admission_v1();

CREATE TRIGGER sandbox_sessions_remote_worker_foundation_rebuild_admission
BEFORE UPDATE OF generation ON cloud_agents.sandbox_sessions
FOR EACH ROW EXECUTE FUNCTION cloud_agents.guard_remote_worker_foundation_admission_v1();

ALTER FUNCTION cloud_agents.is_valid_remote_worker_capabilities(text[]) OWNER TO cloud_agents_migration_owner;
ALTER FUNCTION cloud_agents.foundation_target_available_v1(text,text,text) OWNER TO cloud_agents_migration_owner;
ALTER FUNCTION cloud_agents.foundation_target_available_v2(text,text,text,bigint,bigint) OWNER TO cloud_agents_migration_owner;
ALTER FUNCTION cloud_agents.guard_remote_worker_foundation_admission_v1() OWNER TO cloud_agents_migration_owner;
REVOKE ALL ON FUNCTION cloud_agents.is_valid_remote_worker_capabilities(text[]) FROM PUBLIC;
REVOKE ALL ON FUNCTION cloud_agents.foundation_target_available_v1(text,text,text) FROM PUBLIC;
REVOKE ALL ON FUNCTION cloud_agents.foundation_target_available_v2(text,text,text,bigint,bigint) FROM PUBLIC;
REVOKE ALL ON FUNCTION cloud_agents.guard_remote_worker_foundation_admission_v1() FROM PUBLIC;
GRANT EXECUTE ON FUNCTION cloud_agents.foundation_target_available_v2(text,text,text,bigint,bigint) TO cloud_agents_runtime;
