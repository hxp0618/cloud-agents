CREATE FUNCTION cloud_agents.foundation_remote_worker_capacity_v1(
    p_tenant text, p_project text, p_target text
)
RETURNS TABLE (
    region_uid text,
    resource_pool_uid text,
    node_uid text,
    reserved_cpu_millis bigint,
    reserved_memory_bytes bigint,
    reserved_disk_bytes bigint,
    available_cpu_millis bigint,
    available_memory_bytes bigint,
    available_disk_bytes bigint,
    capacity_state text
)
LANGUAGE sql STABLE SET search_path = pg_catalog, cloud_agents
AS $$
    WITH node AS (
        SELECT target.target_uid,
            enrollment.node_capacity_cpu_millis AS cpu_millis,
            enrollment.node_capacity_memory_bytes AS memory_bytes,
            enrollment.node_capacity_disk_bytes AS disk_bytes
        FROM cloud_agents.deployment_targets AS target
        JOIN cloud_agents.remote_worker_enrollments AS enrollment
          ON enrollment.tenant_id = target.tenant_id
         AND enrollment.project_uid = target.project_uid
         AND enrollment.target_uid = target.target_uid
        WHERE target.tenant_id = p_tenant AND target.project_uid = p_project
          AND target.target_uid = p_target AND target.target_kind = 'remote-worker'
          AND enrollment.node_resource_version IS NOT NULL
    ), reservation AS (
        SELECT COALESCE(pg_catalog.sum(sandbox.cpu_millis)
                    FILTER (WHERE sandbox.desired_state = 'running'), 0)::bigint AS cpu_millis,
            COALESCE(pg_catalog.sum(sandbox.memory_bytes)
                    FILTER (WHERE sandbox.desired_state = 'running'), 0)::bigint AS memory_bytes,
            (pg_catalog.count(DISTINCT volume.workspace_uid) * 21474836480)::bigint AS disk_bytes
        FROM cloud_agents.workspace_volumes AS volume
        LEFT JOIN cloud_agents.sandbox_sessions AS sandbox
          ON sandbox.tenant_id = volume.tenant_id
         AND sandbox.project_uid = volume.project_uid
         AND sandbox.workspace_uid = volume.workspace_uid
        WHERE volume.tenant_id = p_tenant AND volume.project_uid = p_project
          AND volume.target_uid = p_target
    )
    SELECT 'region-local'::text, 'pool-remote-worker'::text, node.target_uid,
        reservation.cpu_millis, reservation.memory_bytes, reservation.disk_bytes,
        GREATEST(node.cpu_millis - reservation.cpu_millis, 0::bigint),
        GREATEST(node.memory_bytes - reservation.memory_bytes, 0::bigint),
        GREATEST(node.disk_bytes - reservation.disk_bytes, 0::bigint),
        CASE
            WHEN reservation.cpu_millis > node.cpu_millis
              OR reservation.memory_bytes > node.memory_bytes
              OR reservation.disk_bytes > node.disk_bytes THEN 'overcommitted'
            WHEN node.cpu_millis - reservation.cpu_millis < 100
              OR node.memory_bytes - reservation.memory_bytes < 134217728
              OR node.disk_bytes - reservation.disk_bytes < 21474836480 THEN 'exhausted'
            ELSE 'available'
        END
    FROM node CROSS JOIN reservation
$$;

CREATE FUNCTION cloud_agents.foundation_target_can_reserve_v1(
    p_tenant text, p_project text, p_target text,
    p_cpu bigint, p_memory bigint, p_workspace text
)
RETURNS boolean
LANGUAGE sql STABLE SET search_path = pg_catalog, cloud_agents
AS $$
    SELECT cloud_agents.foundation_target_available_v2(
        p_tenant, p_project, p_target, p_cpu, p_memory
    ) AND EXISTS (
        SELECT 1
        FROM cloud_agents.deployment_targets AS target
        LEFT JOIN cloud_agents.remote_worker_enrollments AS enrollment
          ON enrollment.tenant_id = target.tenant_id
         AND enrollment.project_uid = target.project_uid
         AND enrollment.target_uid = target.target_uid
         AND target.target_kind = 'remote-worker'
        LEFT JOIN cloud_agents.foundation_remote_worker_capacity_v1(
            p_tenant, p_project, p_target
        ) AS capacity ON true
        WHERE target.tenant_id = p_tenant AND target.project_uid = p_project
          AND target.target_uid = p_target
          AND (target.target_kind <> 'remote-worker' OR (
              capacity.node_uid IS NOT NULL
              AND capacity.reserved_cpu_millis
                    - COALESCE((
                        SELECT pg_catalog.sum(sandbox.cpu_millis)
                        FROM cloud_agents.sandbox_sessions AS sandbox
                        JOIN cloud_agents.workspace_volumes AS volume
                          ON volume.tenant_id = sandbox.tenant_id
                         AND volume.project_uid = sandbox.project_uid
                         AND volume.workspace_uid = sandbox.workspace_uid
                        WHERE volume.tenant_id = p_tenant AND volume.project_uid = p_project
                          AND volume.target_uid = p_target
                          AND sandbox.workspace_uid = p_workspace
                          AND sandbox.desired_state = 'running'
                    ), 0) + p_cpu <= enrollment.node_capacity_cpu_millis
              AND capacity.reserved_memory_bytes
                    - COALESCE((
                        SELECT pg_catalog.sum(sandbox.memory_bytes)
                        FROM cloud_agents.sandbox_sessions AS sandbox
                        JOIN cloud_agents.workspace_volumes AS volume
                          ON volume.tenant_id = sandbox.tenant_id
                         AND volume.project_uid = sandbox.project_uid
                         AND volume.workspace_uid = sandbox.workspace_uid
                        WHERE volume.tenant_id = p_tenant AND volume.project_uid = p_project
                          AND volume.target_uid = p_target
                          AND sandbox.workspace_uid = p_workspace
                          AND sandbox.desired_state = 'running'
                    ), 0) + p_memory <= enrollment.node_capacity_memory_bytes
              AND capacity.reserved_disk_bytes
                    - CASE WHEN EXISTS (
                        SELECT 1 FROM cloud_agents.workspace_volumes AS volume
                        WHERE volume.tenant_id = p_tenant AND volume.project_uid = p_project
                          AND volume.target_uid = p_target AND volume.workspace_uid = p_workspace
                    ) THEN 21474836480 ELSE 0 END
                    + 21474836480 <= enrollment.node_capacity_disk_bytes
          ))
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
    ) AND EXISTS (
        SELECT 1
        FROM cloud_agents.deployment_targets AS target
        LEFT JOIN cloud_agents.foundation_remote_worker_capacity_v1(
            p_tenant, p_project, p_target
        ) AS capacity ON true
        WHERE target.tenant_id = p_tenant AND target.project_uid = p_project
          AND target.target_uid = p_target
          AND (target.target_kind <> 'remote-worker'
            OR capacity.capacity_state <> 'overcommitted')
    )
    FROM cloud_agents.sandbox_sessions AS sandbox
    JOIN cloud_agents.workspace_volumes AS volume
      ON volume.tenant_id = sandbox.tenant_id AND volume.project_uid = sandbox.project_uid
     AND volume.workspace_uid = sandbox.workspace_uid
    WHERE volume.tenant_id = p_tenant AND volume.project_uid = p_project
      AND volume.target_uid = p_target AND sandbox.desired_state = 'running'
$$;

CREATE OR REPLACE FUNCTION cloud_agents.guard_remote_worker_foundation_admission_v1()
RETURNS trigger
LANGUAGE plpgsql SECURITY DEFINER SET search_path = pg_catalog, cloud_agents
AS $$
DECLARE
    target_id text;
BEGIN
    IF TG_TABLE_NAME = 'runtime_profiles' THEN
        IF NEW.status <> 'published' OR NEW.status IS NOT DISTINCT FROM OLD.status THEN
            RETURN NEW;
        END IF;
        IF NOT cloud_agents.foundation_target_available_v2(
            NEW.tenant_id, NEW.project_uid, NEW.target_uid, NEW.cpu_millis, NEW.memory_bytes
        ) THEN
            RAISE EXCEPTION USING ERRCODE = '23503', MESSAGE = 'runtime profile target unavailable';
        END IF;
        RETURN NEW;
    END IF;

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
    IF target_id IS NULL THEN
        RAISE EXCEPTION USING ERRCODE = '23503', MESSAGE = 'runtime profile target unavailable';
    END IF;

    -- Serialize admission on the existing target row so concurrent creates cannot overbook a node.
    PERFORM 1 FROM cloud_agents.deployment_targets AS target
    WHERE target.tenant_id = NEW.tenant_id AND target.project_uid = NEW.project_uid
      AND target.target_uid = target_id
    FOR UPDATE;
    IF NOT FOUND OR NOT cloud_agents.foundation_target_can_reserve_v1(
        NEW.tenant_id, NEW.project_uid, target_id,
        NEW.cpu_millis, NEW.memory_bytes, NEW.workspace_uid
    ) THEN
        RAISE EXCEPTION USING ERRCODE = '23503', MESSAGE = 'runtime profile target unavailable';
    END IF;
    RETURN NEW;
END;
$$;

ALTER FUNCTION cloud_agents.foundation_remote_worker_capacity_v1(text,text,text) OWNER TO cloud_agents_migration_owner;
ALTER FUNCTION cloud_agents.foundation_target_can_reserve_v1(text,text,text,bigint,bigint,text) OWNER TO cloud_agents_migration_owner;
ALTER FUNCTION cloud_agents.foundation_target_available_v1(text,text,text) OWNER TO cloud_agents_migration_owner;
ALTER FUNCTION cloud_agents.guard_remote_worker_foundation_admission_v1() OWNER TO cloud_agents_migration_owner;
REVOKE ALL ON FUNCTION cloud_agents.foundation_remote_worker_capacity_v1(text,text,text) FROM PUBLIC;
REVOKE ALL ON FUNCTION cloud_agents.foundation_target_can_reserve_v1(text,text,text,bigint,bigint,text) FROM PUBLIC;
REVOKE ALL ON FUNCTION cloud_agents.foundation_target_available_v1(text,text,text) FROM PUBLIC;
REVOKE ALL ON FUNCTION cloud_agents.guard_remote_worker_foundation_admission_v1() FROM PUBLIC;
GRANT EXECUTE ON FUNCTION cloud_agents.foundation_remote_worker_capacity_v1(text,text,text) TO cloud_agents_runtime;
GRANT EXECUTE ON FUNCTION cloud_agents.foundation_target_can_reserve_v1(text,text,text,bigint,bigint,text) TO cloud_agents_runtime;
