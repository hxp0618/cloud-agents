CREATE FUNCTION cloud_agents.remote_worker_target_uid_v1(
    p_tenant text, p_project text, p_enrollment text
)
RETURNS text
LANGUAGE sql IMMUTABLE STRICT SET search_path = pg_catalog
AS $$
    SELECT 'rwt-' || pg_catalog.encode(
        pg_catalog.sha256(pg_catalog.convert_to(p_tenant || '|' || p_project || '|' || p_enrollment, 'UTF8')),
        'hex'
    )
$$;

ALTER TABLE cloud_agents.deployment_targets DROP CONSTRAINT deployment_targets_kind;
ALTER TABLE cloud_agents.deployment_targets ADD CONSTRAINT deployment_targets_kind
    CHECK (target_kind IN ('docker', 'kubernetes', 'ssh', 'remote-worker'));
ALTER TABLE cloud_agents.deployment_targets DROP CONSTRAINT deployment_targets_endpoint;
ALTER TABLE cloud_agents.deployment_targets ADD CONSTRAINT deployment_targets_endpoint CHECK (
    target_kind IN ('docker', 'kubernetes')
        AND pg_catalog.octet_length(endpoint) BETWEEN 9 AND 2048
        AND endpoint ~ '^https://[^/?#[:space:]@]+/?$'
    OR target_kind = 'ssh'
        AND pg_catalog.octet_length(endpoint) BETWEEN 9 AND 2048
        AND endpoint ~ '^ssh://[^/?#[:space:]@]+/?$'
    OR target_kind = 'remote-worker'
        AND endpoint = 'remote-worker://' || credential_ref
);

ALTER TABLE cloud_agents.remote_worker_enrollments ADD COLUMN target_uid text
    GENERATED ALWAYS AS (
        cloud_agents.remote_worker_target_uid_v1(tenant_id, project_uid, enrollment_uid)
    ) STORED;
ALTER TABLE cloud_agents.remote_worker_enrollments ADD CONSTRAINT remote_worker_enrollments_target_uid
    CHECK (cloud_agents.is_valid_identifier(target_uid));

INSERT INTO cloud_agents.deployment_targets (
    tenant_id, tenant_ref_id, project_uid, target_uid, target_name, target_kind,
    endpoint, credential_ref, generation, scheduling_state, observed_phase,
    api_version, engine_version, target_os, target_arch, stable_error_code,
    last_probe_at, resource_version, create_idempotency_key, create_request_digest,
    created_at, updated_at
)
SELECT enrollment.tenant_id, enrollment.tenant_id, enrollment.project_uid,
    enrollment.target_uid, enrollment.worker_name, 'remote-worker',
    'remote-worker://' || enrollment.enrollment_uid, enrollment.enrollment_uid,
    COALESCE(enrollment.node_generation, 1), COALESCE(enrollment.node_desired_state, 'active'),
    CASE
        WHEN enrollment.state IN ('revoked', 'expired') THEN 'unavailable'
        WHEN enrollment.node_last_heartbeat_at IS NULL THEN 'unprobed'
        WHEN enrollment.state = 'enrolled' AND enrollment.certificate_state = 'active' THEN 'ready'
        ELSE 'unavailable'
    END,
    CASE WHEN enrollment.node_last_heartbeat_at IS NOT NULL
              AND enrollment.state = 'enrolled' AND enrollment.certificate_state = 'active'
         THEN 'remote-worker/v1alpha1' ELSE '' END,
    CASE WHEN enrollment.node_last_heartbeat_at IS NOT NULL
              AND enrollment.state = 'enrolled' AND enrollment.certificate_state = 'active'
         THEN enrollment.node_worker_version ELSE '' END,
    CASE WHEN enrollment.node_last_heartbeat_at IS NOT NULL
              AND enrollment.state = 'enrolled' AND enrollment.certificate_state = 'active'
         THEN enrollment.node_os ELSE '' END,
    CASE WHEN enrollment.node_last_heartbeat_at IS NOT NULL
              AND enrollment.state = 'enrolled' AND enrollment.certificate_state = 'active'
         THEN enrollment.node_architecture ELSE '' END,
    CASE WHEN enrollment.state IN ('revoked', 'expired')
              OR enrollment.node_last_heartbeat_at IS NOT NULL
                 AND (enrollment.state <> 'enrolled' OR enrollment.certificate_state <> 'active')
         THEN 'remote-worker-unavailable' ELSE '' END,
    CASE WHEN enrollment.state IN ('revoked', 'expired')
         THEN enrollment.updated_at ELSE enrollment.node_last_heartbeat_at END,
    1, 'remote-worker-target-' || pg_catalog.md5(enrollment.target_uid),
    enrollment.create_request_digest, enrollment.created_at, enrollment.updated_at
FROM cloud_agents.remote_worker_enrollments AS enrollment;

ALTER TABLE cloud_agents.remote_worker_enrollments ADD CONSTRAINT remote_worker_enrollments_target_fk
    FOREIGN KEY (tenant_id, project_uid, target_uid)
    REFERENCES cloud_agents.deployment_targets (tenant_id, project_uid, target_uid)
    ON UPDATE RESTRICT ON DELETE RESTRICT DEFERRABLE INITIALLY DEFERRED;

CREATE FUNCTION cloud_agents.sync_remote_worker_target_v1()
RETURNS trigger
LANGUAGE plpgsql SECURITY DEFINER SET search_path = pg_catalog, cloud_agents
AS $$
DECLARE
    mutation_at timestamptz := transaction_timestamp();
    projected_phase text;
BEGIN
    projected_phase := CASE
        WHEN NEW.state IN ('revoked', 'expired') THEN 'unavailable'
        WHEN NEW.node_last_heartbeat_at IS NULL THEN 'unprobed'
        WHEN NEW.state = 'enrolled' AND NEW.certificate_state = 'active' THEN 'ready'
        ELSE 'unavailable'
    END;
    INSERT INTO cloud_agents.deployment_targets (
        tenant_id, tenant_ref_id, project_uid, target_uid, target_name, target_kind,
        endpoint, credential_ref, generation, scheduling_state, observed_phase,
        api_version, engine_version, target_os, target_arch, stable_error_code,
        last_probe_at, resource_version, create_idempotency_key, create_request_digest,
        created_at, updated_at
    ) VALUES (
        NEW.tenant_id, NEW.tenant_id, NEW.project_uid, NEW.target_uid, NEW.worker_name,
        'remote-worker', 'remote-worker://' || NEW.enrollment_uid, NEW.enrollment_uid,
        COALESCE(NEW.node_generation, 1), COALESCE(NEW.node_desired_state, 'active'),
        projected_phase,
        CASE WHEN projected_phase = 'ready' THEN 'remote-worker/v1alpha1' ELSE '' END,
        CASE WHEN projected_phase = 'ready' THEN NEW.node_worker_version ELSE '' END,
        CASE WHEN projected_phase = 'ready' THEN NEW.node_os ELSE '' END,
        CASE WHEN projected_phase = 'ready' THEN NEW.node_architecture ELSE '' END,
        CASE WHEN projected_phase = 'unavailable' THEN 'remote-worker-unavailable' ELSE '' END,
        CASE WHEN projected_phase = 'unavailable' THEN COALESCE(NEW.node_last_heartbeat_at, mutation_at)
             ELSE NEW.node_last_heartbeat_at END,
        1, 'remote-worker-target-' || pg_catalog.md5(NEW.target_uid),
        NEW.create_request_digest, NEW.created_at, mutation_at
    )
    ON CONFLICT (tenant_id, project_uid, target_uid) DO UPDATE SET
        target_name = EXCLUDED.target_name,
        generation = EXCLUDED.generation,
        scheduling_state = EXCLUDED.scheduling_state,
        observed_phase = EXCLUDED.observed_phase,
        api_version = EXCLUDED.api_version,
        engine_version = EXCLUDED.engine_version,
        target_os = EXCLUDED.target_os,
        target_arch = EXCLUDED.target_arch,
        stable_error_code = EXCLUDED.stable_error_code,
        last_probe_at = EXCLUDED.last_probe_at,
        resource_version = cloud_agents.deployment_targets.resource_version + 1,
        updated_at = mutation_at
    WHERE cloud_agents.deployment_targets.target_kind = 'remote-worker'
      AND cloud_agents.deployment_targets.endpoint = EXCLUDED.endpoint
      AND cloud_agents.deployment_targets.credential_ref = EXCLUDED.credential_ref;
    IF NOT FOUND THEN
        RAISE EXCEPTION USING ERRCODE = '23505', MESSAGE = 'remote worker target projection conflict';
    END IF;
    RETURN NEW;
END;
$$;

CREATE TRIGGER sync_remote_worker_target_v1
AFTER INSERT OR UPDATE OF worker_name, state, certificate_state, incarnation_uid, node_generation,
    node_desired_state, node_last_heartbeat_at, node_worker_version, node_os, node_architecture
ON cloud_agents.remote_worker_enrollments
FOR EACH ROW EXECUTE FUNCTION cloud_agents.sync_remote_worker_target_v1();

CREATE VIEW cloud_agents.deployment_target_admin_projection
WITH (security_invoker = true)
AS
SELECT target.tenant_id, target.tenant_ref_id, target.project_uid, target.target_uid,
    target.target_name, target.target_kind, target.endpoint, target.credential_ref,
    target.generation, target.scheduling_state,
    CASE WHEN target.target_kind = 'remote-worker' AND enrollment.node_last_heartbeat_at IS NOT NULL
              AND (enrollment.state <> 'enrolled' OR enrollment.certificate_state <> 'active'
                   OR enrollment.certificate_not_after <= clock_timestamp()
                   OR enrollment.node_heartbeat_expires_at <= clock_timestamp())
         THEN 'unavailable' ELSE target.observed_phase END AS observed_phase,
    CASE WHEN target.target_kind = 'remote-worker' AND enrollment.node_last_heartbeat_at IS NOT NULL
              AND (enrollment.state <> 'enrolled' OR enrollment.certificate_state <> 'active'
                   OR enrollment.certificate_not_after <= clock_timestamp()
                   OR enrollment.node_heartbeat_expires_at <= clock_timestamp())
         THEN '' ELSE target.api_version END AS api_version,
    CASE WHEN target.target_kind = 'remote-worker' AND enrollment.node_last_heartbeat_at IS NOT NULL
              AND (enrollment.state <> 'enrolled' OR enrollment.certificate_state <> 'active'
                   OR enrollment.certificate_not_after <= clock_timestamp()
                   OR enrollment.node_heartbeat_expires_at <= clock_timestamp())
         THEN '' ELSE target.engine_version END AS engine_version,
    CASE WHEN target.target_kind = 'remote-worker' AND enrollment.node_last_heartbeat_at IS NOT NULL
              AND (enrollment.state <> 'enrolled' OR enrollment.certificate_state <> 'active'
                   OR enrollment.certificate_not_after <= clock_timestamp()
                   OR enrollment.node_heartbeat_expires_at <= clock_timestamp())
         THEN '' ELSE target.target_os END AS target_os,
    CASE WHEN target.target_kind = 'remote-worker' AND enrollment.node_last_heartbeat_at IS NOT NULL
              AND (enrollment.state <> 'enrolled' OR enrollment.certificate_state <> 'active'
                   OR enrollment.certificate_not_after <= clock_timestamp()
                   OR enrollment.node_heartbeat_expires_at <= clock_timestamp())
         THEN '' ELSE target.target_arch END AS target_arch,
    CASE WHEN target.target_kind = 'remote-worker' AND enrollment.node_last_heartbeat_at IS NOT NULL
              AND (enrollment.state <> 'enrolled' OR enrollment.certificate_state <> 'active'
                   OR enrollment.certificate_not_after <= clock_timestamp())
         THEN 'remote-worker-unavailable'
         WHEN target.target_kind = 'remote-worker' AND enrollment.node_last_heartbeat_at IS NOT NULL
              AND enrollment.node_heartbeat_expires_at <= clock_timestamp()
         THEN 'remote-worker-offline' ELSE target.stable_error_code END AS stable_error_code,
    target.last_probe_at, target.resource_version, target.create_idempotency_key,
    target.create_request_digest, target.created_at, target.updated_at
FROM cloud_agents.deployment_targets AS target
LEFT JOIN cloud_agents.remote_worker_enrollments AS enrollment
  ON enrollment.tenant_id = target.tenant_id AND enrollment.project_uid = target.project_uid
 AND enrollment.target_uid = target.target_uid AND target.target_kind = 'remote-worker';

ALTER FUNCTION cloud_agents.remote_worker_target_uid_v1(text,text,text) OWNER TO cloud_agents_migration_owner;
ALTER FUNCTION cloud_agents.sync_remote_worker_target_v1() OWNER TO cloud_agents_migration_owner;
ALTER VIEW cloud_agents.deployment_target_admin_projection OWNER TO cloud_agents_migration_owner;
REVOKE ALL ON FUNCTION cloud_agents.remote_worker_target_uid_v1(text,text,text) FROM PUBLIC;
REVOKE ALL ON FUNCTION cloud_agents.sync_remote_worker_target_v1() FROM PUBLIC;
REVOKE ALL ON TABLE cloud_agents.deployment_target_admin_projection FROM PUBLIC;
GRANT EXECUTE ON FUNCTION cloud_agents.remote_worker_target_uid_v1(text,text,text) TO cloud_agents_runtime;
GRANT SELECT ON TABLE cloud_agents.deployment_target_admin_projection TO cloud_agents_runtime;
