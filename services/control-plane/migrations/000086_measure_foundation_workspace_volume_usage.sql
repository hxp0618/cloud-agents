CREATE TABLE cloud_agents.workspace_volume_usage_checkpoints (
    tenant_id text NOT NULL,
    project_uid text NOT NULL,
    volume_uid text NOT NULL,
    measurement_generation bigint NOT NULL DEFAULT 0 CHECK (measurement_generation >= 0),
    state text NOT NULL DEFAULT 'pending' CHECK (state IN ('pending', 'measuring', 'ready', 'failed')),
    used_bytes bigint CHECK (used_bytes IS NULL OR used_bytes BETWEEN 0 AND 1152921504606846976),
    stable_error_code text CHECK (stable_error_code IS NULL OR cloud_agents.is_valid_identifier(stable_error_code)),
    checkpointed_at timestamptz,
    observed_at timestamptz,
    claim_expires_at timestamptz,
    PRIMARY KEY (tenant_id, project_uid, volume_uid),
    FOREIGN KEY (tenant_id, project_uid, volume_uid) REFERENCES cloud_agents.workspace_volumes
        ON UPDATE RESTRICT ON DELETE RESTRICT,
    CHECK ((used_bytes IS NULL) = (checkpointed_at IS NULL)),
    CHECK (checkpointed_at IS NULL OR observed_at >= checkpointed_at),
    CHECK (
        state = 'pending' AND measurement_generation = 0 AND used_bytes IS NULL
            AND stable_error_code IS NULL AND checkpointed_at IS NULL
            AND observed_at IS NULL AND claim_expires_at IS NULL
        OR state = 'measuring' AND measurement_generation > 0 AND stable_error_code IS NULL
            AND observed_at IS NOT NULL AND claim_expires_at > observed_at
        OR state = 'ready' AND measurement_generation > 0 AND used_bytes IS NOT NULL
            AND stable_error_code IS NULL AND checkpointed_at IS NOT NULL
            AND observed_at IS NOT NULL AND claim_expires_at IS NULL
        OR state = 'failed' AND measurement_generation > 0 AND stable_error_code IS NOT NULL
            AND observed_at IS NOT NULL AND claim_expires_at IS NULL
    )
);
CREATE INDEX workspace_volume_usage_due_idx
    ON cloud_agents.workspace_volume_usage_checkpoints (observed_at, claim_expires_at, tenant_id, project_uid, volume_uid);

ALTER TABLE cloud_agents.workspace_volume_usage_checkpoints OWNER TO cloud_agents_migration_owner;
ALTER TABLE cloud_agents.workspace_volume_usage_checkpoints ENABLE ROW LEVEL SECURITY;
ALTER TABLE cloud_agents.workspace_volume_usage_checkpoints FORCE ROW LEVEL SECURITY;
CREATE POLICY workspace_volume_usage_runtime ON cloud_agents.workspace_volume_usage_checkpoints TO cloud_agents_runtime
    USING (tenant_id = cloud_agents.require_tenant_id()) WITH CHECK (tenant_id = cloud_agents.require_tenant_id());
CREATE POLICY workspace_volume_usage_owner ON cloud_agents.workspace_volume_usage_checkpoints TO cloud_agents_migration_owner
    USING (true) WITH CHECK (true);
REVOKE ALL ON TABLE cloud_agents.workspace_volume_usage_checkpoints FROM PUBLIC;
GRANT SELECT ON TABLE cloud_agents.workspace_volume_usage_checkpoints TO cloud_agents_runtime;

CREATE FUNCTION cloud_agents.claim_foundation_workspace_volume_usage_v1(p_limit integer, p_lease_seconds integer)
RETURNS TABLE (
    tenant_id text, project_uid text, workspace_uid text, volume_uid text,
    target_uid text, target_generation bigint, endpoint text, credential_ref text,
    physical_volume_uid text, measurement_generation bigint, claim_expires_at timestamptz
)
LANGUAGE plpgsql SECURITY DEFINER SET search_path = pg_catalog, cloud_agents
AS $$
DECLARE
    selected_tenant text;
    selected_project text;
    selected_workspace text;
    selected_volume text;
    selected_target text;
    selected_target_generation bigint;
    selected_endpoint text;
    selected_credential text;
    selected_physical_volume text;
    selected_measurement_generation bigint;
    selected_claim_expires_at timestamptz;
    claimed_at timestamptz := transaction_timestamp();
BEGIN
    PERFORM cloud_agents.require_runtime_mutation_principal();
    IF p_limit IS NULL OR p_limit NOT BETWEEN 1 AND 500
       OR p_lease_seconds IS NULL OR p_lease_seconds NOT BETWEEN 1 AND 60 THEN
        RAISE EXCEPTION USING ERRCODE = '22023', MESSAGE = 'workspace volume usage claim input is invalid';
    END IF;

    INSERT INTO cloud_agents.workspace_volume_usage_checkpoints (tenant_id, project_uid, volume_uid)
    SELECT volume.tenant_id, volume.project_uid, volume.volume_uid
    FROM cloud_agents.workspace_volumes AS volume
    JOIN cloud_agents.deployment_targets AS target
      ON target.tenant_id = volume.tenant_id AND target.project_uid = volume.project_uid
     AND target.target_uid = volume.target_uid
    WHERE target.target_kind = 'docker' AND volume.physical_volume_uid IS NOT NULL
      AND NOT EXISTS (
          SELECT 1 FROM cloud_agents.workspace_volume_usage_checkpoints AS usage
          WHERE usage.tenant_id = volume.tenant_id AND usage.project_uid = volume.project_uid
            AND usage.volume_uid = volume.volume_uid
      )
    ORDER BY volume.tenant_id, volume.project_uid, volume.volume_uid
    LIMIT p_limit
    ON CONFLICT DO NOTHING;

    SELECT usage.tenant_id, usage.project_uid, volume.workspace_uid, usage.volume_uid,
        target.target_uid, target.generation, target.endpoint, target.credential_ref,
        volume.physical_volume_uid, usage.measurement_generation + 1
    INTO selected_tenant, selected_project, selected_workspace, selected_volume,
        selected_target, selected_target_generation, selected_endpoint, selected_credential,
        selected_physical_volume, selected_measurement_generation
    FROM cloud_agents.workspace_volume_usage_checkpoints AS usage
    JOIN cloud_agents.workspace_volumes AS volume
      ON volume.tenant_id = usage.tenant_id AND volume.project_uid = usage.project_uid
     AND volume.volume_uid = usage.volume_uid
    JOIN cloud_agents.deployment_targets AS target
      ON target.tenant_id = volume.tenant_id AND target.project_uid = volume.project_uid
     AND target.target_uid = volume.target_uid
    WHERE target.target_kind = 'docker' AND target.observed_phase = 'ready'
      AND volume.observed_state = 'available' AND volume.physical_volume_uid IS NOT NULL
      AND (usage.state <> 'measuring' OR usage.claim_expires_at <= claimed_at)
      AND (usage.observed_at IS NULL OR usage.observed_at <= claimed_at - interval '1 minute')
    ORDER BY usage.observed_at NULLS FIRST, usage.tenant_id, usage.project_uid, usage.volume_uid
    FOR UPDATE OF usage SKIP LOCKED
    LIMIT 1;
    IF NOT FOUND THEN
        RETURN;
    END IF;

    UPDATE cloud_agents.workspace_volume_usage_checkpoints AS usage SET
        measurement_generation = selected_measurement_generation,
        state = 'measuring', stable_error_code = NULL, observed_at = claimed_at,
        claim_expires_at = claimed_at + pg_catalog.make_interval(secs => p_lease_seconds)
    WHERE usage.tenant_id = selected_tenant AND usage.project_uid = selected_project
      AND usage.volume_uid = selected_volume
    RETURNING usage.claim_expires_at INTO selected_claim_expires_at;

    RETURN QUERY SELECT selected_tenant, selected_project, selected_workspace, selected_volume,
        selected_target, selected_target_generation, selected_endpoint, selected_credential,
        selected_physical_volume, selected_measurement_generation, selected_claim_expires_at;
END;
$$;

CREATE FUNCTION cloud_agents.settle_foundation_workspace_volume_usage_v1(
    p_tenant text, p_project text, p_workspace text, p_volume text,
    p_target text, p_target_generation bigint, p_physical_volume text,
    p_measurement_generation bigint, p_transition text, p_used_bytes bigint, p_stable_error_code text
)
RETURNS TABLE (
    state text, used_bytes bigint, checkpointed_at timestamptz,
    observed_at timestamptz, stable_error_code text
)
LANGUAGE plpgsql SECURITY DEFINER SET search_path = pg_catalog, cloud_agents
AS $$
DECLARE
    current_usage_state text;
    current_measurement_generation bigint;
    current_claim_expires_at timestamptz;
    current_workspace text;
    current_target text;
    current_target_generation bigint;
    current_physical_volume text;
    settled_at timestamptz := transaction_timestamp();
BEGIN
    PERFORM cloud_agents.require_runtime_mutation_principal();
    IF NOT cloud_agents.is_valid_identifier(p_tenant)
       OR NOT cloud_agents.is_valid_identifier(p_project)
       OR NOT cloud_agents.is_valid_identifier(p_workspace)
       OR NOT cloud_agents.is_valid_identifier(p_volume)
       OR NOT cloud_agents.is_valid_identifier(p_target)
       OR NOT cloud_agents.is_valid_identifier(p_physical_volume)
       OR p_target_generation IS NULL OR p_target_generation < 1
       OR p_measurement_generation IS NULL OR p_measurement_generation < 1
       OR p_transition NOT IN ('ready', 'failed')
       OR p_transition = 'ready' AND (p_used_bytes IS NULL OR p_used_bytes NOT BETWEEN 0 AND 1152921504606846976 OR p_stable_error_code IS NOT NULL)
       OR p_transition = 'failed' AND (p_used_bytes IS NOT NULL OR NOT cloud_agents.is_valid_identifier(p_stable_error_code)) THEN
        RAISE EXCEPTION USING ERRCODE = '22023', MESSAGE = 'workspace volume usage settlement input is invalid';
    END IF;

    SELECT usage.state, usage.measurement_generation, usage.claim_expires_at,
        volume.workspace_uid, volume.target_uid, target.generation, volume.physical_volume_uid
    INTO current_usage_state, current_measurement_generation, current_claim_expires_at,
        current_workspace, current_target, current_target_generation, current_physical_volume
    FROM cloud_agents.workspace_volume_usage_checkpoints AS usage
    JOIN cloud_agents.workspace_volumes AS volume
      ON volume.tenant_id = usage.tenant_id AND volume.project_uid = usage.project_uid
     AND volume.volume_uid = usage.volume_uid
    JOIN cloud_agents.deployment_targets AS target
      ON target.tenant_id = volume.tenant_id AND target.project_uid = volume.project_uid
     AND target.target_uid = volume.target_uid
    WHERE usage.tenant_id = p_tenant AND usage.project_uid = p_project AND usage.volume_uid = p_volume
    FOR UPDATE OF usage;
    IF NOT FOUND OR current_workspace <> p_workspace OR current_target <> p_target
       OR current_target_generation <> p_target_generation
       OR current_physical_volume <> p_physical_volume
       OR current_usage_state <> 'measuring'
       OR current_measurement_generation <> p_measurement_generation
       OR current_claim_expires_at < settled_at THEN
        RAISE EXCEPTION USING ERRCODE = '40001', MESSAGE = 'workspace volume usage claim drifted';
    END IF;

    RETURN QUERY UPDATE cloud_agents.workspace_volume_usage_checkpoints AS usage SET
        state = p_transition,
        used_bytes = CASE WHEN p_transition = 'ready' THEN p_used_bytes ELSE usage.used_bytes END,
        checkpointed_at = CASE WHEN p_transition = 'ready' THEN settled_at ELSE usage.checkpointed_at END,
        observed_at = settled_at,
        stable_error_code = CASE WHEN p_transition = 'failed' THEN p_stable_error_code ELSE NULL END,
        claim_expires_at = NULL
    WHERE usage.tenant_id = p_tenant AND usage.project_uid = p_project AND usage.volume_uid = p_volume
    RETURNING usage.state, usage.used_bytes, usage.checkpointed_at, usage.observed_at, usage.stable_error_code;
END;
$$;

ALTER FUNCTION cloud_agents.claim_foundation_workspace_volume_usage_v1(integer,integer) OWNER TO cloud_agents_migration_owner;
ALTER FUNCTION cloud_agents.settle_foundation_workspace_volume_usage_v1(text,text,text,text,text,bigint,text,bigint,text,bigint,text) OWNER TO cloud_agents_migration_owner;
REVOKE ALL ON FUNCTION cloud_agents.claim_foundation_workspace_volume_usage_v1(integer,integer) FROM PUBLIC;
REVOKE ALL ON FUNCTION cloud_agents.settle_foundation_workspace_volume_usage_v1(text,text,text,text,text,bigint,text,bigint,text,bigint,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION cloud_agents.claim_foundation_workspace_volume_usage_v1(integer,integer) TO cloud_agents_runtime;
GRANT EXECUTE ON FUNCTION cloud_agents.settle_foundation_workspace_volume_usage_v1(text,text,text,text,text,bigint,text,bigint,text,bigint,text) TO cloud_agents_runtime;
