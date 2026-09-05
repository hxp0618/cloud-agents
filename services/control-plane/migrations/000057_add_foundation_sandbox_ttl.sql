ALTER TABLE cloud_agents.sandbox_sessions
    ADD COLUMN ttl_seconds integer;
ALTER TABLE cloud_agents.sandbox_sessions
    ADD COLUMN expires_at timestamptz;
ALTER TABLE cloud_agents.sandbox_sessions
    ADD CONSTRAINT sandbox_sessions_ttl CHECK (
        ttl_seconds IS NULL AND expires_at IS NULL
        OR ttl_seconds BETWEEN 60 AND 86400 AND expires_at IS NOT NULL
    );
CREATE INDEX sandbox_sessions_expiry_idx ON cloud_agents.sandbox_sessions
    (expires_at, tenant_id, project_uid, sandbox_uid);

ALTER TABLE cloud_agents.foundation_sandbox_activity
    ADD COLUMN lifecycle_trigger text NOT NULL DEFAULT 'manual'
    CHECK (lifecycle_trigger IN ('manual', 'ttl'));

CREATE FUNCTION cloud_agents.accept_foundation_sandbox_v2(
    p_project text, p_workspace text, p_workspace_name text, p_sandbox text,
    p_profile text, p_profile_version bigint, p_subject text, p_key text, p_digest text,
    p_ttl_seconds integer
)
RETURNS TABLE (
    operation_uid text, workspace_uid text, sandbox_uid text, profile_uid text,
    profile_version bigint, generation bigint, desired_state text, observed_state text,
    ttl_seconds integer, expires_at timestamptz
)
LANGUAGE plpgsql SECURITY DEFINER SET search_path = pg_catalog, cloud_agents
AS $$
DECLARE
    accepted record;
    accepted_at timestamptz;
    expiry timestamptz;
BEGIN
    IF p_ttl_seconds NOT BETWEEN 60 AND 86400 THEN
        RAISE EXCEPTION USING ERRCODE = '22023', MESSAGE = 'foundation sandbox TTL is invalid';
    END IF;
    SELECT * INTO STRICT accepted FROM cloud_agents.accept_foundation_sandbox_v1(
        p_project, p_workspace, p_workspace_name, p_sandbox, p_profile, p_profile_version,
        p_subject, p_key, p_digest
    );
    SELECT operation.created_at INTO STRICT accepted_at
    FROM cloud_agents.platform_operations AS operation
    WHERE operation.tenant_id = cloud_agents.require_tenant_id()
      AND operation.operation_id = accepted.operation_uid
      AND operation.operation_generation = accepted.generation;
    expiry := accepted_at + pg_catalog.make_interval(secs => p_ttl_seconds);
    UPDATE cloud_agents.sandbox_sessions AS sandbox
    SET ttl_seconds = p_ttl_seconds, expires_at = expiry
    WHERE sandbox.tenant_id = cloud_agents.require_tenant_id()
      AND sandbox.project_uid = p_project AND sandbox.sandbox_uid = p_sandbox
      AND (sandbox.ttl_seconds IS NULL AND sandbox.expires_at IS NULL
        OR sandbox.ttl_seconds = p_ttl_seconds AND sandbox.expires_at = expiry);
    IF NOT FOUND THEN
        RAISE EXCEPTION USING ERRCODE = '23505', MESSAGE = 'foundation sandbox TTL conflict';
    END IF;
    RETURN QUERY SELECT accepted.operation_uid, accepted.workspace_uid, accepted.sandbox_uid,
        accepted.profile_uid, accepted.profile_version, accepted.generation,
        accepted.desired_state, accepted.observed_state, p_ttl_seconds, expiry;
END;
$$;

CREATE FUNCTION cloud_agents.transition_foundation_sandbox_v2(
    p_project text, p_sandbox text, p_expected_generation bigint, p_expected_resource_version bigint,
    p_action text, p_confirmed_sandbox text, p_compute_disposition text, p_workspace_disposition text,
    p_subject text, p_key text, p_digest text, p_request text
)
RETURNS TABLE (
    operation_uid text, idempotency_key text, action text, sandbox_uid text,
    sandbox_generation bigint, requested_by text, request_id text, requested_at timestamptz,
    updated_at timestamptz, operation_state text, cleanup_phase text, stable_error_code text,
    compute_disposition text, workspace_disposition text
)
LANGUAGE plpgsql SECURITY DEFINER SET search_path = pg_catalog, cloud_agents
AS $$
DECLARE
    transitioned record;
BEGIN
    SELECT * INTO STRICT transitioned FROM cloud_agents.transition_foundation_sandbox_v1(
        p_project, p_sandbox, p_expected_generation, p_expected_resource_version,
        p_action, p_confirmed_sandbox, p_compute_disposition, p_workspace_disposition,
        p_subject, p_key, p_digest, p_request
    );
    IF p_action = 'rebuild' THEN
        UPDATE cloud_agents.sandbox_sessions AS sandbox
        SET expires_at = transitioned.requested_at + pg_catalog.make_interval(secs => sandbox.ttl_seconds)
        WHERE sandbox.tenant_id = cloud_agents.require_tenant_id()
          AND sandbox.project_uid = p_project AND sandbox.sandbox_uid = p_sandbox
          AND sandbox.operation_id = transitioned.operation_uid
          AND sandbox.ttl_seconds IS NOT NULL;
    END IF;
    RETURN QUERY SELECT transitioned.operation_uid, transitioned.idempotency_key,
        transitioned.action, transitioned.sandbox_uid, transitioned.sandbox_generation,
        transitioned.requested_by, transitioned.request_id, transitioned.requested_at,
        transitioned.updated_at, transitioned.operation_state, transitioned.cleanup_phase,
        transitioned.stable_error_code, transitioned.compute_disposition,
        transitioned.workspace_disposition;
END;
$$;

CREATE FUNCTION cloud_agents.expire_foundation_sandbox_v1(p_subject text, p_audit text)
RETURNS TABLE (
    tenant_id text, project_uid text, sandbox_uid text, expired_at timestamptz,
    operation_uid text, sandbox_generation bigint
)
LANGUAGE plpgsql SECURITY DEFINER SET search_path = pg_catalog, cloud_agents
AS $$
DECLARE
    ignored text;
    candidate record;
    transitioned record;
    key_value text;
    request_value text;
    canonical_request text;
    request_digest text;
BEGIN
    ignored := cloud_agents.require_runtime_mutation_principal();
    IF p_subject !~ '^sha256:[0-9a-f]{64}$' OR NOT cloud_agents.is_valid_identifier(p_audit) THEN
        RAISE EXCEPTION USING ERRCODE = '22023', MESSAGE = 'foundation expiry input is invalid';
    END IF;
    SELECT sandbox.tenant_id, sandbox.project_uid, sandbox.sandbox_uid, sandbox.generation,
        sandbox.resource_version, sandbox.expires_at
    INTO candidate
    FROM cloud_agents.sandbox_sessions AS sandbox
    JOIN cloud_agents.workspace_volumes AS volume
      ON volume.tenant_id = sandbox.tenant_id AND volume.project_uid = sandbox.project_uid
     AND volume.workspace_uid = sandbox.workspace_uid
    JOIN cloud_agents.platform_operations AS operation
      ON operation.tenant_id = sandbox.tenant_id AND operation.operation_id = sandbox.operation_id
     AND operation.operation_generation = sandbox.operation_generation
    WHERE sandbox.expires_at <= pg_catalog.clock_timestamp()
      AND sandbox.desired_state = 'running' AND sandbox.observed_state = 'running'
      AND sandbox.observed_generation = sandbox.generation AND NOT sandbox.writer_released
      AND sandbox.runtime_uid IS NOT NULL AND sandbox.runtime_operation_uid IS NOT NULL
      AND sandbox.runtime_generation = sandbox.generation
      AND sandbox.runtime_spec_digest = sandbox.spec_digest
      AND volume.retention = 'retain' AND volume.observed_state = 'available'
      AND volume.physical_volume_uid IS NOT NULL
      AND operation.state = 'succeeded' AND operation.cleanup_phase = 'complete'
    ORDER BY sandbox.expires_at, sandbox.tenant_id, sandbox.project_uid, sandbox.sandbox_uid
    FOR UPDATE OF sandbox SKIP LOCKED LIMIT 1;
    IF NOT FOUND THEN RETURN; END IF;

    PERFORM pg_catalog.set_config('cloud_agents.tenant_id', candidate.tenant_id, true);
    key_value := 'sandbox-ttl-' || pg_catalog.md5(candidate.tenant_id || '|' || candidate.project_uid
        || '|' || candidate.sandbox_uid || '|' || candidate.generation::text || '|' || candidate.expires_at::text);
    request_value := 'ttl-' || pg_catalog.md5(key_value);
    canonical_request := '{"Operation":"foundation-sandbox.stop","TenantID":'
        || pg_catalog.to_json(candidate.tenant_id)::text || ',"ProjectID":'
        || pg_catalog.to_json(candidate.project_uid)::text || ',"SandboxID":'
        || pg_catalog.to_json(candidate.sandbox_uid)::text || ',"ConfirmedSandboxID":'
        || pg_catalog.to_json(candidate.sandbox_uid)::text || ',"ComputeDisposition":"delete"'
        || ',"WorkspaceDisposition":"retain","ExpectedGeneration":'
        || candidate.generation::text || ',"ExpectedResourceVersion":'
        || candidate.resource_version::text || '}';
    request_digest := 'sha256:' || pg_catalog.encode(
        pg_catalog.sha256(pg_catalog.convert_to(canonical_request, 'UTF8')), 'hex');

    SELECT * INTO STRICT transitioned FROM cloud_agents.transition_foundation_sandbox_v1(
        candidate.project_uid, candidate.sandbox_uid, candidate.generation,
        candidate.resource_version, 'stop', candidate.sandbox_uid, 'delete', 'retain',
        p_subject, key_value, request_digest, request_value
    );
    UPDATE cloud_agents.foundation_sandbox_activity AS activity SET lifecycle_trigger = 'ttl'
    WHERE activity.tenant_id = candidate.tenant_id
      AND activity.operation_uid = transitioned.operation_uid;
    IF NOT FOUND THEN
        RAISE EXCEPTION USING ERRCODE = '40001', MESSAGE = 'foundation expiry activity drifted';
    END IF;
    PERFORM cloud_agents.append_coordination_audit(
        candidate.tenant_id, p_audit, 'foundationSandboxLifecycle/v1alpha1',
        'sha256:aabd6aa244c89a4850eba529dbe387132c3a0aff7ffba430f6115afb6e215a1b',
        p_subject, transitioned.operation_uid, transitioned.sandbox_generation, NULL,
        'sandboxSession', candidate.sandbox_uid, candidate.resource_version,
        'sandbox.ttl.accept', 'pending', NULL, NULL, pg_catalog.transaction_timestamp()
    );
    RETURN QUERY SELECT candidate.tenant_id, candidate.project_uid, candidate.sandbox_uid,
        candidate.expires_at, transitioned.operation_uid, transitioned.sandbox_generation;
END;
$$;

CREATE FUNCTION cloud_agents.claim_foundation_sandbox_v3(
    p_holder text, p_incarnation text, p_token text, p_lease_seconds integer,
    p_subject text, p_audit text
)
RETURNS TABLE (
    tenant_id text, event_id text, delivery_attempts integer, claim_expires_at timestamptz,
    operation_id text, operation_generation bigint, action text,
    project_uid text, workspace_uid text, workspace_name text, volume_uid text,
    physical_volume_uid text, target_uid text, target_generation bigint, target_endpoint text,
    credential_ref text, sandbox_uid text, sandbox_generation bigint, image_uri text,
    runtime_profile_uid text, runtime_profile_version bigint, cpu_millis bigint, memory_bytes bigint,
    spec_digest text, runtime_uid text, runtime_state text, runtime_operation_uid text,
    runtime_generation bigint, runtime_spec_digest text, ttl_seconds integer, expires_at timestamptz
)
LANGUAGE plpgsql SECURITY DEFINER SET search_path = pg_catalog, cloud_agents
AS $$
DECLARE
    claimed record;
BEGIN
    SELECT * INTO claimed FROM cloud_agents.claim_foundation_sandbox_v2(
        p_holder, p_incarnation, p_token, p_lease_seconds, p_subject, p_audit
    );
    IF NOT FOUND THEN RETURN; END IF;
    RETURN QUERY SELECT claimed.tenant_id, claimed.event_id, claimed.delivery_attempts,
        claimed.claim_expires_at, claimed.operation_id, claimed.operation_generation, claimed.action,
        claimed.project_uid, claimed.workspace_uid, claimed.workspace_name, claimed.volume_uid,
        claimed.physical_volume_uid, claimed.target_uid, claimed.target_generation,
        claimed.target_endpoint, claimed.credential_ref, claimed.sandbox_uid,
        claimed.sandbox_generation, claimed.image_uri, claimed.runtime_profile_uid,
        claimed.runtime_profile_version, claimed.cpu_millis, claimed.memory_bytes,
        claimed.spec_digest, claimed.runtime_uid, claimed.runtime_state,
        claimed.runtime_operation_uid, claimed.runtime_generation, claimed.runtime_spec_digest,
        sandbox.ttl_seconds, sandbox.expires_at
    FROM cloud_agents.sandbox_sessions AS sandbox
    WHERE sandbox.tenant_id = claimed.tenant_id AND sandbox.project_uid = claimed.project_uid
      AND sandbox.sandbox_uid = claimed.sandbox_uid
      AND sandbox.operation_id = claimed.operation_id
      AND sandbox.operation_generation = claimed.operation_generation;
    IF NOT FOUND THEN
        RAISE EXCEPTION USING ERRCODE = '40001', MESSAGE = 'foundation TTL claim drifted';
    END IF;
END;
$$;

ALTER FUNCTION cloud_agents.accept_foundation_sandbox_v2(text,text,text,text,text,bigint,text,text,text,integer)
    OWNER TO cloud_agents_migration_owner;
ALTER FUNCTION cloud_agents.transition_foundation_sandbox_v2(text,text,bigint,bigint,text,text,text,text,text,text,text,text)
    OWNER TO cloud_agents_migration_owner;
ALTER FUNCTION cloud_agents.expire_foundation_sandbox_v1(text,text)
    OWNER TO cloud_agents_migration_owner;
ALTER FUNCTION cloud_agents.claim_foundation_sandbox_v3(text,text,text,integer,text,text)
    OWNER TO cloud_agents_migration_owner;
REVOKE ALL ON FUNCTION cloud_agents.accept_foundation_sandbox_v2(text,text,text,text,text,bigint,text,text,text,integer) FROM PUBLIC;
REVOKE ALL ON FUNCTION cloud_agents.transition_foundation_sandbox_v2(text,text,bigint,bigint,text,text,text,text,text,text,text,text) FROM PUBLIC;
REVOKE ALL ON FUNCTION cloud_agents.expire_foundation_sandbox_v1(text,text) FROM PUBLIC;
REVOKE ALL ON FUNCTION cloud_agents.claim_foundation_sandbox_v3(text,text,text,integer,text,text) FROM PUBLIC;
REVOKE EXECUTE ON FUNCTION cloud_agents.accept_foundation_sandbox_v1(text,text,text,text,text,bigint,text,text,text) FROM cloud_agents_runtime;
REVOKE EXECUTE ON FUNCTION cloud_agents.transition_foundation_sandbox_v1(text,text,bigint,bigint,text,text,text,text,text,text,text,text) FROM cloud_agents_runtime;
REVOKE EXECUTE ON FUNCTION cloud_agents.claim_foundation_sandbox_v2(text,text,text,integer,text,text) FROM cloud_agents_runtime;
GRANT EXECUTE ON FUNCTION cloud_agents.accept_foundation_sandbox_v2(text,text,text,text,text,bigint,text,text,text,integer) TO cloud_agents_runtime;
GRANT EXECUTE ON FUNCTION cloud_agents.transition_foundation_sandbox_v2(text,text,bigint,bigint,text,text,text,text,text,text,text,text) TO cloud_agents_runtime;
GRANT EXECUTE ON FUNCTION cloud_agents.expire_foundation_sandbox_v1(text,text) TO cloud_agents_runtime;
GRANT EXECUTE ON FUNCTION cloud_agents.claim_foundation_sandbox_v3(text,text,text,integer,text,text) TO cloud_agents_runtime;
