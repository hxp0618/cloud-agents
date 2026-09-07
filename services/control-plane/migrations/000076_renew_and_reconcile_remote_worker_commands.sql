CREATE FUNCTION cloud_agents.renew_remote_worker_foundation_sandbox_v1(
    p_tenant text, p_project text, p_target text, p_incarnation text,
    p_peer_certificate_digest text, p_command text
)
RETURNS timestamptz
LANGUAGE plpgsql SECURITY DEFINER SET search_path = pg_catalog, cloud_agents
AS $$
DECLARE
    event cloud_agents.outbox_events%ROWTYPE;
    expected_command text;
BEGIN
    PERFORM cloud_agents.require_runtime_mutation_principal();
    IF p_tenant IS DISTINCT FROM cloud_agents.require_tenant_id()
       OR NOT cloud_agents.is_valid_identifier(p_project)
       OR NOT cloud_agents.is_valid_identifier(p_target)
       OR NOT cloud_agents.is_valid_identifier(p_incarnation)
       OR p_peer_certificate_digest !~ '^sha256:[0-9a-f]{64}$'
       OR NOT cloud_agents.is_valid_identifier(p_command) THEN
        RAISE EXCEPTION USING ERRCODE = '22023', MESSAGE = 'remote worker sandbox renewal input is invalid';
    END IF;

    SELECT stored.* INTO event
    FROM cloud_agents.outbox_events AS stored
    JOIN cloud_agents.sandbox_sessions AS sandbox
      ON sandbox.tenant_id = stored.tenant_id
     AND sandbox.operation_id = stored.operation_id
     AND sandbox.operation_generation = stored.operation_generation
     AND sandbox.sandbox_uid = stored.aggregate_id
     AND sandbox.generation = stored.generation
    JOIN cloud_agents.workspace_volumes AS volume
      ON volume.tenant_id = sandbox.tenant_id
     AND volume.project_uid = sandbox.project_uid
     AND volume.workspace_uid = sandbox.workspace_uid
    JOIN cloud_agents.remote_worker_enrollments AS enrollment
      ON enrollment.tenant_id = volume.tenant_id
     AND enrollment.project_uid = volume.project_uid
     AND enrollment.target_uid = volume.target_uid
    WHERE stored.tenant_id = p_tenant
      AND sandbox.project_uid = p_project
      AND volume.target_uid = p_target
      AND stored.profile_id = 'foundationSandboxLifecycle/v1alpha1'
      AND stored.profile_digest = 'sha256:aabd6aa244c89a4850eba529dbe387132c3a0aff7ffba430f6115afb6e215a1b'
      AND stored.event_class = 'operation_effect'
      AND stored.aggregate_kind = 'sandboxSession'
      AND stored.state = 'claimed'
      AND stored.claim_holder_id = p_target
      AND stored.claim_incarnation = p_incarnation
      AND enrollment.state = 'enrolled'
      AND enrollment.certificate_state = 'active'
      AND enrollment.certificate_sha256 = p_peer_certificate_digest
      AND enrollment.certificate_not_after > clock_timestamp()
      AND enrollment.incarnation_uid = p_incarnation
      AND enrollment.node_heartbeat_expires_at > clock_timestamp()
      AND enrollment.node_desired_state = 'active'
      AND enrollment.node_observed_state = 'active'
    FOR UPDATE OF stored;
    IF NOT FOUND THEN
        RAISE EXCEPTION USING ERRCODE = '40001', MESSAGE = 'remote worker sandbox renewal conflict';
    END IF;
    expected_command := 'rwsc-' || pg_catalog.substr(pg_catalog.encode(
        pg_catalog.sha256(pg_catalog.convert_to(event.operation_id || '|' || event.delivery_attempts::text, 'UTF8')), 'hex'), 1, 32);
    IF p_command IS DISTINCT FROM expected_command THEN
        RAISE EXCEPTION USING ERRCODE = '40001', MESSAGE = 'remote worker sandbox renewal conflict';
    END IF;
    RETURN cloud_agents.renew_foundation_sandbox_claim_v1(
        p_tenant, event.event_id, p_target, p_incarnation, event.claim_token,
        event.claim_expires_at, 60
    );
END;
$$;

ALTER FUNCTION cloud_agents.renew_remote_worker_foundation_sandbox_v1(text,text,text,text,text,text)
    OWNER TO cloud_agents_migration_owner;
REVOKE ALL ON FUNCTION cloud_agents.renew_remote_worker_foundation_sandbox_v1(text,text,text,text,text,text)
    FROM PUBLIC;
GRANT EXECUTE ON FUNCTION cloud_agents.renew_remote_worker_foundation_sandbox_v1(text,text,text,text,text,text)
    TO cloud_agents_runtime;
