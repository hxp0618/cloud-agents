
-- Workspace/Volume lifetime is independent of Sandbox compute. No cascading deletes.
CREATE TABLE cloud_agents.workspaces (
    tenant_id text NOT NULL,
    project_uid text NOT NULL,
    workspace_uid text NOT NULL CHECK (cloud_agents.is_valid_identifier(workspace_uid)),
    workspace_name text NOT NULL CHECK (cloud_agents.is_valid_identifier(workspace_name)),
    resource_version bigint NOT NULL DEFAULT 1 CHECK (resource_version > 0),
    created_at timestamptz NOT NULL DEFAULT transaction_timestamp(),
    PRIMARY KEY (tenant_id, project_uid, workspace_uid),
    FOREIGN KEY (tenant_id, project_uid) REFERENCES cloud_agents.projects (tenant_id, project_uid)
        ON UPDATE RESTRICT ON DELETE RESTRICT
);
CREATE TABLE cloud_agents.workspace_volumes (
    tenant_id text NOT NULL,
    project_uid text NOT NULL,
    workspace_uid text NOT NULL,
    volume_uid text NOT NULL CHECK (cloud_agents.is_valid_identifier(volume_uid)),
    target_uid text NOT NULL,
    retention text NOT NULL DEFAULT 'retain' CHECK (retention = 'retain'),
    observed_state text NOT NULL DEFAULT 'pending' CHECK (observed_state IN ('pending', 'available', 'unknown', 'failed')),
    PRIMARY KEY (tenant_id, project_uid, volume_uid),
    UNIQUE (tenant_id, project_uid, workspace_uid),
    FOREIGN KEY (tenant_id, project_uid, workspace_uid) REFERENCES cloud_agents.workspaces
        ON UPDATE RESTRICT ON DELETE RESTRICT,
    FOREIGN KEY (tenant_id, project_uid, target_uid) REFERENCES cloud_agents.deployment_targets (tenant_id, project_uid, target_uid)
        ON UPDATE RESTRICT ON DELETE RESTRICT
);
CREATE INDEX workspace_volumes_target_idx ON cloud_agents.workspace_volumes (tenant_id, project_uid, target_uid);
CREATE TABLE cloud_agents.sandbox_sessions (
    tenant_id text NOT NULL,
    project_uid text NOT NULL,
    sandbox_uid text NOT NULL CHECK (cloud_agents.is_valid_identifier(sandbox_uid)),
    workspace_uid text NOT NULL,
    generation bigint NOT NULL CHECK (generation > 0),
    desired_state text NOT NULL CHECK (desired_state IN ('running', 'stopped')),
    observed_state text NOT NULL DEFAULT 'pending' CHECK (observed_state IN ('pending', 'running', 'unknown', 'failed', 'stopped')),
    observed_generation bigint NOT NULL DEFAULT 0 CHECK (observed_generation BETWEEN 0 AND generation),
    writer_released boolean NOT NULL DEFAULT false,
    image_uri text NOT NULL CHECK (image_uri ~ '^[A-Za-z0-9._:/-]+@sha256:[0-9a-f]{64}$' AND length(image_uri) <= 1024),
    cpu_millis bigint NOT NULL CHECK (cpu_millis BETWEEN 100 AND 64000),
    memory_bytes bigint NOT NULL CHECK (memory_bytes BETWEEN 134217728 AND 1099511627776),
    operation_id text NOT NULL,
    operation_generation bigint NOT NULL CHECK (operation_generation > 0),
    spec_digest text NOT NULL CHECK (spec_digest ~ '^sha256:[0-9a-f]{64}$'),
    PRIMARY KEY (tenant_id, project_uid, sandbox_uid),
    FOREIGN KEY (tenant_id, project_uid, workspace_uid) REFERENCES cloud_agents.workspace_volumes (tenant_id, project_uid, workspace_uid)
        ON UPDATE RESTRICT ON DELETE RESTRICT,
    FOREIGN KEY (tenant_id, operation_id, operation_generation) REFERENCES cloud_agents.platform_operations
        ON UPDATE RESTRICT ON DELETE RESTRICT,
    CHECK (NOT writer_released OR (observed_state = 'stopped' AND observed_generation = generation))
);
-- Failure/timeout/unknown do not release a writer. Only a fenced stop may do so.
CREATE UNIQUE INDEX sandbox_sessions_single_writer ON cloud_agents.sandbox_sessions
    (tenant_id, project_uid, workspace_uid) WHERE NOT writer_released;
CREATE INDEX sandbox_sessions_workspace_idx ON cloud_agents.sandbox_sessions (tenant_id, project_uid, workspace_uid);
CREATE INDEX sandbox_sessions_operation_idx ON cloud_agents.sandbox_sessions (tenant_id, operation_id, operation_generation);

ALTER TABLE cloud_agents.workspaces OWNER TO cloud_agents_migration_owner;
ALTER TABLE cloud_agents.workspace_volumes OWNER TO cloud_agents_migration_owner;
ALTER TABLE cloud_agents.sandbox_sessions OWNER TO cloud_agents_migration_owner;
ALTER TABLE cloud_agents.workspaces ENABLE ROW LEVEL SECURITY;
ALTER TABLE cloud_agents.workspaces FORCE ROW LEVEL SECURITY;
ALTER TABLE cloud_agents.workspace_volumes ENABLE ROW LEVEL SECURITY;
ALTER TABLE cloud_agents.workspace_volumes FORCE ROW LEVEL SECURITY;
ALTER TABLE cloud_agents.sandbox_sessions ENABLE ROW LEVEL SECURITY;
ALTER TABLE cloud_agents.sandbox_sessions FORCE ROW LEVEL SECURITY;
CREATE POLICY workspaces_runtime ON cloud_agents.workspaces TO cloud_agents_runtime
    USING (tenant_id = cloud_agents.require_tenant_id()) WITH CHECK (tenant_id = cloud_agents.require_tenant_id());
CREATE POLICY workspaces_owner ON cloud_agents.workspaces TO cloud_agents_migration_owner USING (true) WITH CHECK (true);
CREATE POLICY workspace_volumes_runtime ON cloud_agents.workspace_volumes TO cloud_agents_runtime
    USING (tenant_id = cloud_agents.require_tenant_id()) WITH CHECK (tenant_id = cloud_agents.require_tenant_id());
CREATE POLICY workspace_volumes_owner ON cloud_agents.workspace_volumes TO cloud_agents_migration_owner USING (true) WITH CHECK (true);
CREATE POLICY sandbox_sessions_runtime ON cloud_agents.sandbox_sessions TO cloud_agents_runtime
    USING (tenant_id = cloud_agents.require_tenant_id()) WITH CHECK (tenant_id = cloud_agents.require_tenant_id());
CREATE POLICY sandbox_sessions_owner ON cloud_agents.sandbox_sessions TO cloud_agents_migration_owner USING (true) WITH CHECK (true);
REVOKE ALL ON TABLE cloud_agents.workspaces FROM PUBLIC;
REVOKE ALL ON TABLE cloud_agents.workspace_volumes FROM PUBLIC;
REVOKE ALL ON TABLE cloud_agents.sandbox_sessions FROM PUBLIC;
GRANT SELECT ON TABLE cloud_agents.workspaces TO cloud_agents_runtime;
GRANT SELECT ON TABLE cloud_agents.workspace_volumes TO cloud_agents_runtime;
GRANT SELECT ON TABLE cloud_agents.sandbox_sessions TO cloud_agents_runtime;

-- Trusted resolved-intent writer, not a public API: future HTTP must authorize and resolve a published RuntimeProfile.
-- No network calls or physical side effects occur inside this transaction.
CREATE FUNCTION cloud_agents.accept_foundation_intent_v1(
    p_project text, p_workspace text, p_name text, p_volume text, p_target text, p_sandbox text,
    p_image text, p_cpu bigint, p_memory bigint, p_operation text, p_event text,
    p_subject text, p_key text, p_digest text)
RETURNS text LANGUAGE plpgsql SECURITY DEFINER SET search_path = pg_catalog, cloud_agents
AS $$
DECLARE
    t text;
    prior cloud_agents.idempotency_records%ROWTYPE;
BEGIN
    PERFORM cloud_agents.require_runtime_mutation_principal();
    t := cloud_agents.require_tenant_id();
    IF p_project IS NULL OR p_workspace IS NULL OR p_name IS NULL OR p_volume IS NULL OR p_target IS NULL
        OR p_sandbox IS NULL OR p_image IS NULL OR p_cpu IS NULL OR p_memory IS NULL
        OR p_operation IS NULL OR p_event IS NULL OR p_subject IS NULL OR p_key IS NULL OR p_digest IS NULL
        OR NOT cloud_agents.is_valid_identifier(p_project) OR NOT cloud_agents.is_valid_identifier(p_operation)
        OR NOT cloud_agents.is_valid_identifier(p_event)
    THEN RAISE EXCEPTION USING ERRCODE = '22023', MESSAGE = 'invalid foundation intent'; END IF;
    INSERT INTO cloud_agents.idempotency_records
        (tenant_id, tenant_ref_id, subject_digest, registry_digest, profile_id, profile_digest,
         idempotency_key, request_digest, state, expires_at)
    VALUES (t, t, p_subject, '@REGISTRY_DIGEST@', '@PROFILE_ID@', '@PROFILE_DIGEST@',
        p_key, p_digest, 'pending', transaction_timestamp() + interval '1 day')
    ON CONFLICT DO NOTHING;
    SELECT * INTO STRICT prior FROM cloud_agents.idempotency_records
    WHERE tenant_id = t AND subject_digest = p_subject AND profile_id = '@PROFILE_ID@'
        AND profile_digest = '@PROFILE_DIGEST@' AND idempotency_key = p_key FOR UPDATE;
    IF prior.request_digest <> p_digest THEN
        RAISE EXCEPTION USING ERRCODE = '22023', MESSAGE = 'foundation idempotency conflict';
    END IF;
    IF prior.operation_id IS NOT NULL THEN
        -- Bind replay to resolved fields as well as the supplied canonical digest.
        IF NOT EXISTS (
            SELECT 1 FROM cloud_agents.sandbox_sessions s
            JOIN cloud_agents.workspaces w USING (tenant_id, project_uid, workspace_uid)
            JOIN cloud_agents.workspace_volumes v USING (tenant_id, project_uid, workspace_uid)
            WHERE s.tenant_id = t AND s.project_uid = p_project AND s.workspace_uid = p_workspace
                AND s.sandbox_uid = p_sandbox AND w.workspace_name = p_name AND v.volume_uid = p_volume
                AND v.target_uid = p_target AND s.image_uri = p_image AND s.cpu_millis = p_cpu
                AND s.memory_bytes = p_memory AND s.operation_id = prior.operation_id
        ) THEN RAISE EXCEPTION USING ERRCODE = '22023', MESSAGE = 'foundation idempotency conflict'; END IF;
        RETURN prior.operation_id;
    END IF;
    PERFORM 1 FROM cloud_agents.projects p JOIN cloud_agents.platform_tenants tenant ON tenant.tenant_id = p.tenant_id
    WHERE p.tenant_id = t AND p.project_uid = p_project AND p.state = 'active' AND tenant.state = 'active'
    FOR SHARE OF p, tenant;
    IF NOT FOUND THEN RAISE EXCEPTION USING ERRCODE = '22023', MESSAGE = 'foundation project unavailable'; END IF;
    -- Existing Target authority is reused; no new endpoint or credential copy.
    PERFORM 1 FROM cloud_agents.deployment_targets
    WHERE tenant_id = t AND project_uid = p_project AND target_uid = p_target
        AND target_kind = 'docker' AND observed_phase = 'ready' AND scheduling_state = 'active' FOR SHARE;
    IF NOT FOUND THEN RAISE EXCEPTION USING ERRCODE = '22023', MESSAGE = 'foundation target unavailable'; END IF;
    INSERT INTO cloud_agents.platform_operations
        (tenant_id, tenant_ref_id, operation_id, operation_generation, registry_digest, state_machine_digest,
         policy_digest, profile_id, profile_digest, subject_digest, request_digest, state, cleanup_phase,
         recovery_generation, current_attempt_number)
    VALUES (t, t, p_operation, 1, '@REGISTRY_DIGEST@', cloud_agents.coordination_state_machine_digest(),
        cloud_agents.coordination_policy_digest(), '@PROFILE_ID@', '@PROFILE_DIGEST@', p_subject, p_digest,
        'pending', 'none', 0, 0);
    INSERT INTO cloud_agents.workspaces (tenant_id, project_uid, workspace_uid, workspace_name)
    VALUES (t, p_project, p_workspace, p_name);
    INSERT INTO cloud_agents.workspace_volumes (tenant_id, project_uid, workspace_uid, volume_uid, target_uid)
    VALUES (t, p_project, p_workspace, p_volume, p_target);
    INSERT INTO cloud_agents.sandbox_sessions
        (tenant_id, project_uid, sandbox_uid, workspace_uid, generation, desired_state,
         image_uri, cpu_millis, memory_bytes, operation_id, operation_generation, spec_digest)
    VALUES (t, p_project, p_sandbox, p_workspace, 1, 'running', p_image, p_cpu, p_memory, p_operation, 1, p_digest);
    INSERT INTO cloud_agents.operation_finalizers
        (tenant_id, tenant_ref_id, operation_id, operation_generation, finalizer_name, required, state, delivery_attempts)
    VALUES (t, t, p_operation, 1, 'sandbox-compute', true, 'pending', 0);
    INSERT INTO cloud_agents.outbox_events
        (tenant_id, tenant_ref_id, event_id, registry_digest, profile_id, profile_digest, event_class,
         aggregate_kind, aggregate_id, aggregate_sequence, generation, operation_id, operation_generation,
         payload_digest, state, delivery_attempts)
    VALUES (t, t, p_event, '@REGISTRY_DIGEST@', '@PROFILE_ID@', '@PROFILE_DIGEST@', 'operation_effect',
        'sandboxSession', p_sandbox, 1, 1, p_operation, 1, p_digest, 'pending', 0);
    INSERT INTO cloud_agents.coordination_audit_facts
        (tenant_id, tenant_ref_id, audit_fact_id, registry_digest, profile_id, profile_digest,
         subject_digest, operation_id, operation_generation, transition, outcome)
    VALUES (t, t, p_event, '@REGISTRY_DIGEST@', '@PROFILE_ID@', '@PROFILE_DIGEST@',
        p_subject, p_operation, 1, 'sandbox.accept', 'pending');
    UPDATE cloud_agents.idempotency_records SET operation_id = p_operation, operation_generation = 1
    WHERE tenant_id = t AND subject_digest = p_subject AND profile_id = '@PROFILE_ID@'
        AND profile_digest = '@PROFILE_DIGEST@' AND idempotency_key = p_key;
    RETURN p_operation;
END;
$$;
ALTER FUNCTION cloud_agents.accept_foundation_intent_v1(text,text,text,text,text,text,text,bigint,bigint,text,text,text,text,text)
    OWNER TO cloud_agents_migration_owner;
REVOKE ALL ON FUNCTION cloud_agents.accept_foundation_intent_v1(text,text,text,text,text,text,text,bigint,bigint,text,text,text,text,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION cloud_agents.accept_foundation_intent_v1(text,text,text,text,text,text,text,bigint,bigint,text,text,text,text,text) TO cloud_agents_runtime;
