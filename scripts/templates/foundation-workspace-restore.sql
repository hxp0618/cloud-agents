CREATE TABLE cloud_agents.workspace_snapshot_restores (
    tenant_id text NOT NULL,
    project_uid text NOT NULL,
    operation_id text NOT NULL,
    operation_generation bigint NOT NULL DEFAULT 1 CHECK (operation_generation = 1),
    snapshot_uid text NOT NULL,
    snapshot_resource_version bigint NOT NULL CHECK (snapshot_resource_version > 0),
    workspace_uid text NOT NULL,
    sandbox_uid text NOT NULL,
    target_uid text NOT NULL,
    source_workspace_uid text NOT NULL,
    source_physical_snapshot_uid text NOT NULL CHECK (cloud_agents.is_valid_identifier(source_physical_snapshot_uid)),
    source_content_digest text NOT NULL CHECK (source_content_digest ~ '^sha256:[0-9a-f]{64}$'),
    created_at timestamptz NOT NULL DEFAULT transaction_timestamp(),
    PRIMARY KEY (tenant_id, operation_id, operation_generation),
    UNIQUE (tenant_id, project_uid, workspace_uid),
    UNIQUE (tenant_id, project_uid, sandbox_uid),
    FOREIGN KEY (tenant_id, project_uid, snapshot_uid) REFERENCES cloud_agents.workspace_snapshots
        ON UPDATE RESTRICT ON DELETE RESTRICT,
    FOREIGN KEY (tenant_id, project_uid, sandbox_uid) REFERENCES cloud_agents.sandbox_sessions
        ON UPDATE RESTRICT ON DELETE RESTRICT,
    FOREIGN KEY (tenant_id, operation_id, operation_generation) REFERENCES cloud_agents.platform_operations
        ON UPDATE RESTRICT ON DELETE RESTRICT
);
CREATE INDEX workspace_snapshot_restores_source_idx
    ON cloud_agents.workspace_snapshot_restores (tenant_id, project_uid, snapshot_uid, created_at);
ALTER TABLE cloud_agents.workspace_snapshot_restores OWNER TO cloud_agents_migration_owner;
ALTER TABLE cloud_agents.workspace_snapshot_restores ENABLE ROW LEVEL SECURITY;
ALTER TABLE cloud_agents.workspace_snapshot_restores FORCE ROW LEVEL SECURITY;
CREATE POLICY workspace_snapshot_restores_runtime ON cloud_agents.workspace_snapshot_restores TO cloud_agents_runtime
    USING (tenant_id = cloud_agents.require_tenant_id()) WITH CHECK (tenant_id = cloud_agents.require_tenant_id());
CREATE POLICY workspace_snapshot_restores_owner ON cloud_agents.workspace_snapshot_restores TO cloud_agents_migration_owner
    USING (true) WITH CHECK (true);
REVOKE ALL ON TABLE cloud_agents.workspace_snapshot_restores FROM PUBLIC;
GRANT SELECT ON TABLE cloud_agents.workspace_snapshot_restores TO cloud_agents_runtime;

CREATE FUNCTION cloud_agents.accept_foundation_workspace_restore_v1(
    p_project text, p_snapshot text, p_expected_snapshot_version bigint,
    p_workspace text, p_workspace_name text, p_sandbox text,
    p_profile text, p_profile_version bigint, p_ttl_seconds integer,
    p_subject text, p_key text, p_digest text
)
RETURNS TABLE (
    operation_uid text, workspace_uid text, sandbox_uid text, profile_uid text,
    profile_version bigint, generation bigint, desired_state text, observed_state text,
    ttl_seconds integer, expires_at timestamptz
)
LANGUAGE plpgsql SECURITY DEFINER SET search_path = pg_catalog, cloud_agents
AS $$
DECLARE
    tenant text;
    selected_snapshot cloud_agents.workspace_snapshots%ROWTYPE;
    accepted record;
    prior_digest text;
    prior_operation text;
    restore_audit text;
BEGIN
    PERFORM cloud_agents.require_runtime_mutation_principal();
    tenant := cloud_agents.require_tenant_id();
    IF NOT cloud_agents.is_valid_identifier(p_project) OR NOT cloud_agents.is_valid_identifier(p_snapshot)
      OR p_expected_snapshot_version < 1 OR NOT cloud_agents.is_valid_identifier(p_workspace)
      OR NOT cloud_agents.is_valid_identifier(p_workspace_name) OR NOT cloud_agents.is_valid_identifier(p_sandbox)
      OR NOT cloud_agents.is_valid_identifier(p_profile) OR p_profile_version NOT BETWEEN 1 AND 2147483647
      OR p_ttl_seconds NOT BETWEEN 60 AND 86400 OR p_subject !~ '^sha256:[0-9a-f]{64}$'
      OR p_key !~ '^[A-Za-z0-9._~-]{16,128}$' OR p_digest !~ '^sha256:[0-9a-f]{64}$'
    THEN RAISE EXCEPTION USING ERRCODE = '22023', MESSAGE = 'workspace snapshot restore request is invalid'; END IF;

    SELECT record.request_digest, record.operation_id INTO prior_digest, prior_operation
    FROM cloud_agents.idempotency_records AS record
    WHERE record.tenant_id = tenant AND record.subject_digest = p_subject
      AND record.profile_id = 'foundationSandboxLifecycle/v1alpha1'
      AND record.profile_digest = 'sha256:aabd6aa244c89a4850eba529dbe387132c3a0aff7ffba430f6115afb6e215a1b'
      AND record.idempotency_key = p_key FOR UPDATE;
    IF FOUND THEN
      IF prior_digest IS DISTINCT FROM p_digest THEN
        RAISE EXCEPTION USING ERRCODE = '23505', MESSAGE = 'workspace snapshot restore conflict';
      END IF;
      SELECT * INTO accepted FROM cloud_agents.accept_foundation_sandbox_v3(
        p_project, p_workspace, p_workspace_name, p_sandbox, p_profile, p_profile_version,
        p_subject, p_key, p_digest, p_ttl_seconds
      );
      IF NOT EXISTS (
        SELECT 1 FROM cloud_agents.workspace_snapshot_restores AS restore
        WHERE restore.tenant_id = tenant AND restore.operation_id = prior_operation
          AND restore.project_uid = p_project AND restore.snapshot_uid = p_snapshot
          AND restore.snapshot_resource_version = p_expected_snapshot_version
          AND restore.workspace_uid = p_workspace AND restore.sandbox_uid = p_sandbox
      ) THEN RAISE EXCEPTION USING ERRCODE = '23505', MESSAGE = 'workspace snapshot restore conflict'; END IF;
      RETURN QUERY SELECT accepted.operation_uid, accepted.workspace_uid, accepted.sandbox_uid,
        accepted.profile_uid, accepted.profile_version, accepted.generation, accepted.desired_state,
        accepted.observed_state, accepted.ttl_seconds, accepted.expires_at;
      RETURN;
    END IF;

    SELECT snapshot.* INTO selected_snapshot FROM cloud_agents.workspace_snapshots AS snapshot
    WHERE snapshot.tenant_id = tenant AND snapshot.project_uid = p_project AND snapshot.snapshot_uid = p_snapshot
    FOR SHARE;
    IF NOT FOUND THEN RAISE EXCEPTION USING ERRCODE = '23503', MESSAGE = 'workspace snapshot was not found'; END IF;
    IF selected_snapshot.status <> 'available' OR selected_snapshot.resource_version <> p_expected_snapshot_version
      OR selected_snapshot.physical_snapshot_uid IS NULL OR selected_snapshot.content_digest IS NULL
    THEN RAISE EXCEPTION USING ERRCODE = '23505', MESSAGE = 'workspace snapshot restore conflict'; END IF;
    PERFORM 1 FROM cloud_agents.runtime_profiles AS profile
    JOIN cloud_agents.deployment_targets AS target
      ON target.tenant_id = profile.tenant_id AND target.project_uid = profile.project_uid
     AND target.target_uid = profile.target_uid
    WHERE profile.tenant_id = tenant AND profile.project_uid = p_project
      AND profile.profile_uid = p_profile AND profile.profile_version = p_profile_version
      AND profile.status = 'published' AND profile.target_uid = selected_snapshot.target_uid
      AND target.target_kind = 'docker' AND target.observed_phase = 'ready' AND target.scheduling_state = 'active'
    FOR SHARE OF profile, target;
    IF NOT FOUND THEN RAISE EXCEPTION USING ERRCODE = '23505', MESSAGE = 'workspace snapshot restore target conflict'; END IF;

    SELECT * INTO accepted FROM cloud_agents.accept_foundation_sandbox_v3(
      p_project, p_workspace, p_workspace_name, p_sandbox, p_profile, p_profile_version,
      p_subject, p_key, p_digest, p_ttl_seconds
    );
    INSERT INTO cloud_agents.workspace_snapshot_restores
      (tenant_id, project_uid, operation_id, snapshot_uid, snapshot_resource_version,
       workspace_uid, sandbox_uid, target_uid, source_workspace_uid,
       source_physical_snapshot_uid, source_content_digest)
    VALUES (tenant, p_project, accepted.operation_uid, p_snapshot, p_expected_snapshot_version,
      p_workspace, p_sandbox, selected_snapshot.target_uid, selected_snapshot.source_workspace_uid,
      selected_snapshot.physical_snapshot_uid, selected_snapshot.content_digest);
    restore_audit := 'audit-' || pg_catalog.md5(tenant || '|' || accepted.operation_uid || '|restore');
    INSERT INTO cloud_agents.coordination_audit_facts
      (tenant_id, tenant_ref_id, audit_fact_id, registry_digest, profile_id, profile_digest,
       subject_digest, operation_id, operation_generation, transition, outcome)
    SELECT tenant, tenant, restore_audit, operation.registry_digest, operation.profile_id,
      operation.profile_digest, p_subject, operation.operation_id, operation.operation_generation,
      'workspace.snapshot.restore.accept', 'pending'
    FROM cloud_agents.platform_operations AS operation
    WHERE operation.tenant_id = tenant AND operation.operation_id = accepted.operation_uid
      AND operation.operation_generation = 1;
    RETURN QUERY SELECT accepted.operation_uid, accepted.workspace_uid, accepted.sandbox_uid,
      accepted.profile_uid, accepted.profile_version, accepted.generation, accepted.desired_state,
      accepted.observed_state, accepted.ttl_seconds, accepted.expires_at;
END;
$$;

ALTER FUNCTION cloud_agents.accept_foundation_workspace_restore_v1(text,text,bigint,text,text,text,text,bigint,integer,text,text,text)
    OWNER TO cloud_agents_migration_owner;
REVOKE ALL ON FUNCTION cloud_agents.accept_foundation_workspace_restore_v1(text,text,bigint,text,text,text,text,bigint,integer,text,text,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION cloud_agents.accept_foundation_workspace_restore_v1(text,text,bigint,text,text,text,text,bigint,integer,text,text,text) TO cloud_agents_runtime;
