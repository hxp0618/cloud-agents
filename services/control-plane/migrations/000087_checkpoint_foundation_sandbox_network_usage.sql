ALTER TABLE cloud_agents.sandbox_usage_checkpoints
    ADD COLUMN network_measurement_generation bigint NOT NULL DEFAULT 0 CHECK (network_measurement_generation >= 0),
    ADD COLUMN network_state text NOT NULL DEFAULT 'pending' CHECK (network_state IN ('pending', 'measuring', 'ready', 'failed')),
    ADD COLUMN network_received_bytes bigint CHECK (network_received_bytes IS NULL OR network_received_bytes BETWEEN 0 AND 1152921504606846976),
    ADD COLUMN network_transmitted_bytes bigint CHECK (network_transmitted_bytes IS NULL OR network_transmitted_bytes BETWEEN 0 AND 1152921504606846976),
    ADD COLUMN network_stable_error_code text CHECK (network_stable_error_code IS NULL OR cloud_agents.is_valid_identifier(network_stable_error_code)),
    ADD COLUMN network_checkpointed_at timestamptz,
    ADD COLUMN network_observed_at timestamptz,
    ADD COLUMN network_claim_expires_at timestamptz,
    ADD CONSTRAINT sandbox_usage_network_shape CHECK (
        (network_received_bytes IS NULL) = (network_transmitted_bytes IS NULL)
        AND (network_received_bytes IS NULL) = (network_checkpointed_at IS NULL)
        AND (network_checkpointed_at IS NULL OR network_observed_at >= network_checkpointed_at)
        AND (
            network_state = 'pending' AND network_measurement_generation = 0
                AND network_received_bytes IS NULL AND network_stable_error_code IS NULL
                AND network_observed_at IS NULL AND network_claim_expires_at IS NULL
            OR network_state = 'measuring' AND network_measurement_generation > 0
                AND network_stable_error_code IS NULL AND network_observed_at IS NOT NULL
                AND network_claim_expires_at > network_observed_at
            OR network_state = 'ready' AND network_measurement_generation > 0
                AND network_received_bytes IS NOT NULL AND network_stable_error_code IS NULL
                AND network_observed_at IS NOT NULL AND network_claim_expires_at IS NULL
            OR network_state = 'failed' AND network_measurement_generation > 0
                AND network_stable_error_code IS NOT NULL AND network_observed_at IS NOT NULL
                AND network_claim_expires_at IS NULL
        )
    );

CREATE INDEX sandbox_usage_network_due_idx
    ON cloud_agents.sandbox_usage_checkpoints
        (network_observed_at, network_claim_expires_at, tenant_id, project_uid, sandbox_uid);

CREATE FUNCTION cloud_agents.claim_foundation_sandbox_network_usage_v1(p_lease_seconds integer)
RETURNS TABLE (
    tenant_id text, project_uid text, sandbox_uid text, sandbox_generation bigint,
    runtime_uid text, target_uid text, target_generation bigint, endpoint text, credential_ref text,
    previous_received_bytes bigint, previous_transmitted_bytes bigint,
    measurement_generation bigint, claim_expires_at timestamptz
)
LANGUAGE plpgsql SECURITY DEFINER SET search_path = pg_catalog, cloud_agents
AS $$
DECLARE
    selected_tenant text;
    selected_project text;
    selected_sandbox text;
    selected_sandbox_generation bigint;
    selected_runtime text;
    selected_target text;
    selected_target_generation bigint;
    selected_endpoint text;
    selected_credential text;
    selected_received_bytes bigint;
    selected_transmitted_bytes bigint;
    selected_measurement_generation bigint;
    selected_claim_expires_at timestamptz;
    claimed_at timestamptz := transaction_timestamp();
BEGIN
    PERFORM cloud_agents.require_runtime_mutation_principal();
    IF p_lease_seconds IS NULL OR p_lease_seconds NOT BETWEEN 1 AND 60 THEN
        RAISE EXCEPTION USING ERRCODE = '22023', MESSAGE = 'sandbox network usage claim input is invalid';
    END IF;

    SELECT usage.tenant_id, usage.project_uid, usage.sandbox_uid, usage.sandbox_generation,
        usage.runtime_uid, target.target_uid, target.generation, target.endpoint, target.credential_ref,
        usage.network_received_bytes, usage.network_transmitted_bytes,
        usage.network_measurement_generation + 1
    INTO selected_tenant, selected_project, selected_sandbox, selected_sandbox_generation,
        selected_runtime, selected_target, selected_target_generation, selected_endpoint, selected_credential,
        selected_received_bytes, selected_transmitted_bytes, selected_measurement_generation
    FROM cloud_agents.sandbox_usage_checkpoints AS usage
    JOIN cloud_agents.sandbox_sessions AS sandbox
      ON sandbox.tenant_id = usage.tenant_id AND sandbox.project_uid = usage.project_uid
     AND sandbox.sandbox_uid = usage.sandbox_uid
     AND sandbox.runtime_generation = usage.sandbox_generation AND sandbox.runtime_uid = usage.runtime_uid
    JOIN cloud_agents.workspace_volumes AS volume
      ON volume.tenant_id = sandbox.tenant_id AND volume.project_uid = sandbox.project_uid
     AND volume.workspace_uid = sandbox.workspace_uid
    JOIN cloud_agents.deployment_targets AS target
      ON target.tenant_id = volume.tenant_id AND target.project_uid = volume.project_uid
     AND target.target_uid = volume.target_uid
    WHERE usage.finalized_at IS NULL AND target.target_kind = 'docker'
      AND target.observed_phase = 'ready' AND sandbox.observed_state = 'running'
      AND (usage.network_state <> 'measuring' OR usage.network_claim_expires_at <= claimed_at)
      AND (usage.network_observed_at IS NULL OR usage.network_observed_at <= claimed_at - interval '1 minute')
    ORDER BY usage.network_observed_at NULLS FIRST, usage.tenant_id, usage.project_uid, usage.sandbox_uid
    FOR UPDATE OF usage SKIP LOCKED
    LIMIT 1;
    IF NOT FOUND THEN
        RETURN;
    END IF;

    UPDATE cloud_agents.sandbox_usage_checkpoints AS usage SET
        network_measurement_generation = selected_measurement_generation,
        network_state = 'measuring', network_stable_error_code = NULL,
        network_observed_at = claimed_at,
        network_claim_expires_at = claimed_at + pg_catalog.make_interval(secs => p_lease_seconds)
    WHERE usage.tenant_id = selected_tenant AND usage.project_uid = selected_project
      AND usage.sandbox_uid = selected_sandbox AND usage.sandbox_generation = selected_sandbox_generation
      AND usage.runtime_uid = selected_runtime
    RETURNING usage.network_claim_expires_at INTO selected_claim_expires_at;

    RETURN QUERY SELECT selected_tenant, selected_project, selected_sandbox, selected_sandbox_generation,
        selected_runtime, selected_target, selected_target_generation, selected_endpoint, selected_credential,
        selected_received_bytes, selected_transmitted_bytes,
        selected_measurement_generation, selected_claim_expires_at;
END;
$$;

CREATE FUNCTION cloud_agents.settle_foundation_sandbox_network_usage_v1(
    p_tenant text, p_project text, p_sandbox text, p_sandbox_generation bigint,
    p_runtime text, p_target text, p_target_generation bigint, p_measurement_generation bigint,
    p_transition text, p_received_bytes bigint, p_transmitted_bytes bigint, p_stable_error_code text
)
RETURNS TABLE (
    state text, received_bytes bigint, transmitted_bytes bigint, checkpointed_at timestamptz,
    observed_at timestamptz, stable_error_code text
)
LANGUAGE plpgsql SECURITY DEFINER SET search_path = pg_catalog, cloud_agents
AS $$
DECLARE
    current_state text;
    current_measurement_generation bigint;
    current_received_bytes bigint;
    current_transmitted_bytes bigint;
    current_claim_expires_at timestamptz;
    current_target text;
    current_target_generation bigint;
    settled_at timestamptz := transaction_timestamp();
BEGIN
    PERFORM cloud_agents.require_runtime_mutation_principal();
    IF NOT cloud_agents.is_valid_identifier(p_tenant)
       OR NOT cloud_agents.is_valid_identifier(p_project)
       OR NOT cloud_agents.is_valid_identifier(p_sandbox)
       OR NOT cloud_agents.is_valid_identifier(p_runtime)
       OR NOT cloud_agents.is_valid_identifier(p_target)
       OR p_sandbox_generation IS NULL OR p_sandbox_generation < 1
       OR p_target_generation IS NULL OR p_target_generation < 1
       OR p_measurement_generation IS NULL OR p_measurement_generation < 1
       OR p_transition NOT IN ('ready', 'failed')
       OR p_transition = 'ready' AND (
            p_received_bytes IS NULL OR p_received_bytes NOT BETWEEN 0 AND 1152921504606846976
            OR p_transmitted_bytes IS NULL OR p_transmitted_bytes NOT BETWEEN 0 AND 1152921504606846976
            OR p_stable_error_code IS NOT NULL)
       OR p_transition = 'failed' AND (
            p_received_bytes IS NOT NULL OR p_transmitted_bytes IS NOT NULL
            OR NOT cloud_agents.is_valid_identifier(p_stable_error_code)) THEN
        RAISE EXCEPTION USING ERRCODE = '22023', MESSAGE = 'sandbox network usage settlement input is invalid';
    END IF;

    SELECT usage.network_state, usage.network_measurement_generation,
        usage.network_received_bytes, usage.network_transmitted_bytes, usage.network_claim_expires_at,
        volume.target_uid, target.generation
    INTO current_state, current_measurement_generation,
        current_received_bytes, current_transmitted_bytes, current_claim_expires_at,
        current_target, current_target_generation
    FROM cloud_agents.sandbox_usage_checkpoints AS usage
    JOIN cloud_agents.sandbox_sessions AS sandbox
      ON sandbox.tenant_id = usage.tenant_id AND sandbox.project_uid = usage.project_uid
     AND sandbox.sandbox_uid = usage.sandbox_uid
    JOIN cloud_agents.workspace_volumes AS volume
      ON volume.tenant_id = sandbox.tenant_id AND volume.project_uid = sandbox.project_uid
     AND volume.workspace_uid = sandbox.workspace_uid
    JOIN cloud_agents.deployment_targets AS target
      ON target.tenant_id = volume.tenant_id AND target.project_uid = volume.project_uid
     AND target.target_uid = volume.target_uid
    WHERE usage.tenant_id = p_tenant AND usage.project_uid = p_project
      AND usage.sandbox_uid = p_sandbox AND usage.sandbox_generation = p_sandbox_generation
      AND usage.runtime_uid = p_runtime
    FOR UPDATE OF usage;
    IF NOT FOUND OR current_target <> p_target OR current_target_generation <> p_target_generation
       OR current_state <> 'measuring' OR current_measurement_generation <> p_measurement_generation
       OR current_claim_expires_at < settled_at
       OR p_transition = 'ready' AND (
            current_received_bytes IS NOT NULL AND p_received_bytes < current_received_bytes
            OR current_transmitted_bytes IS NOT NULL AND p_transmitted_bytes < current_transmitted_bytes) THEN
        RAISE EXCEPTION USING ERRCODE = '40001', MESSAGE = 'sandbox network usage claim drifted';
    END IF;

    RETURN QUERY UPDATE cloud_agents.sandbox_usage_checkpoints AS usage SET
        network_state = p_transition,
        network_received_bytes = CASE WHEN p_transition = 'ready' THEN p_received_bytes ELSE usage.network_received_bytes END,
        network_transmitted_bytes = CASE WHEN p_transition = 'ready' THEN p_transmitted_bytes ELSE usage.network_transmitted_bytes END,
        network_checkpointed_at = CASE WHEN p_transition = 'ready' THEN settled_at ELSE usage.network_checkpointed_at END,
        network_observed_at = settled_at,
        network_stable_error_code = CASE WHEN p_transition = 'failed' THEN p_stable_error_code ELSE NULL END,
        network_claim_expires_at = NULL
    WHERE usage.tenant_id = p_tenant AND usage.project_uid = p_project
      AND usage.sandbox_uid = p_sandbox AND usage.sandbox_generation = p_sandbox_generation
      AND usage.runtime_uid = p_runtime
    RETURNING usage.network_state, usage.network_received_bytes, usage.network_transmitted_bytes,
        usage.network_checkpointed_at, usage.network_observed_at, usage.network_stable_error_code;
END;
$$;

ALTER FUNCTION cloud_agents.claim_foundation_sandbox_network_usage_v1(integer) OWNER TO cloud_agents_migration_owner;
ALTER FUNCTION cloud_agents.settle_foundation_sandbox_network_usage_v1(text,text,text,bigint,text,text,bigint,bigint,text,bigint,bigint,text) OWNER TO cloud_agents_migration_owner;
REVOKE ALL ON FUNCTION cloud_agents.claim_foundation_sandbox_network_usage_v1(integer) FROM PUBLIC;
REVOKE ALL ON FUNCTION cloud_agents.settle_foundation_sandbox_network_usage_v1(text,text,text,bigint,text,text,bigint,bigint,text,bigint,bigint,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION cloud_agents.claim_foundation_sandbox_network_usage_v1(integer) TO cloud_agents_runtime;
GRANT EXECUTE ON FUNCTION cloud_agents.settle_foundation_sandbox_network_usage_v1(text,text,text,bigint,text,text,bigint,bigint,text,bigint,bigint,text) TO cloud_agents_runtime;
