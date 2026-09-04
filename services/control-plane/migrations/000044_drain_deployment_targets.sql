ALTER TABLE cloud_agents.deployment_targets
    ADD COLUMN scheduling_state text NOT NULL DEFAULT 'active';
ALTER TABLE cloud_agents.deployment_targets
    ADD CONSTRAINT deployment_targets_scheduling_state CHECK (scheduling_state IN ('active', 'drained'));

ALTER TABLE cloud_agents.deployment_target_activity
    DROP CONSTRAINT deployment_target_activity_action;
ALTER TABLE cloud_agents.deployment_target_activity
    ADD CONSTRAINT deployment_target_activity_action
    CHECK (action IN ('target.register', 'target.probe', 'target.drain', 'target.resume', 'target.cleanup'));

CREATE FUNCTION cloud_agents.lock_deployment_target_scheduling_v1(
    p_tenant_id text, p_project_uid text, p_target_uid text
)
RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path = pg_catalog, cloud_agents
AS $cloud_agents_function$
DECLARE
    ignored_principal text;
BEGIN
    ignored_principal := cloud_agents.require_runtime_mutation_principal();
    IF p_tenant_id IS DISTINCT FROM cloud_agents.require_tenant_id()
        OR NOT cloud_agents.is_valid_identifier(p_project_uid)
        OR NOT cloud_agents.is_valid_identifier(p_target_uid)
    THEN RAISE EXCEPTION USING ERRCODE = '22023', MESSAGE = 'deployment target scheduling lock input is invalid'; END IF;

    PERFORM 1 FROM cloud_agents.deployment_targets AS target
    WHERE target.tenant_id = p_tenant_id AND target.project_uid = p_project_uid
        AND target.target_uid = p_target_uid
    FOR UPDATE;
    IF NOT FOUND THEN RETURN false; END IF;

    PERFORM lease.lease_uid FROM cloud_agents.managed_host_environment_leases AS lease
    WHERE lease.tenant_id = p_tenant_id AND lease.project_uid = p_project_uid
        AND lease.deployment_target_uid = p_target_uid AND lease.desired_phase = 'active'
    FOR SHARE;
    RETURN true;
END;
$cloud_agents_function$;

CREATE FUNCTION cloud_agents.transition_deployment_target_scheduling_v1(
    p_tenant_id text, p_project_uid text, p_target_uid text,
    p_expected_generation bigint, p_expected_resource_version bigint,
    p_desired_state text, p_impact_digest text, p_current_impact_digest text,
    p_idempotency_key text, p_request_digest text, p_request_id text,
    p_subject_digest text, p_impact_summary text
)
RETURNS TABLE (
    operation_uid text, idempotency_key text, action text, target_uid text,
    target_generation bigint, subject_digest text, request_id text,
    requested_at timestamptz, updated_at timestamptz, state text, current_step text,
    stable_error_code text, impact_summary text, retryable boolean
)
LANGUAGE plpgsql SECURITY DEFINER SET search_path = pg_catalog, cloud_agents
AS $cloud_agents_function$
DECLARE
    ignored_principal text;
    mutation_at timestamptz;
    existing_target cloud_agents.deployment_targets%ROWTYPE;
    activity cloud_agents.deployment_target_activity%ROWTYPE;
    action_value text;
    operation_uid_value text;
BEGIN
    ignored_principal := cloud_agents.require_runtime_mutation_principal();
    mutation_at := pg_catalog.transaction_timestamp();
    IF p_tenant_id IS DISTINCT FROM cloud_agents.require_tenant_id()
        OR NOT cloud_agents.is_valid_identifier(p_project_uid)
        OR NOT cloud_agents.is_valid_identifier(p_target_uid)
        OR p_expected_generation < 1 OR p_expected_resource_version < 1
        OR p_desired_state NOT IN ('active', 'drained')
        OR p_impact_digest !~ '^sha256:[0-9a-f]{64}$'
        OR p_current_impact_digest !~ '^sha256:[0-9a-f]{64}$'
        OR p_idempotency_key !~ '^[A-Za-z0-9._~-]{16,128}$'
        OR p_request_digest !~ '^sha256:[0-9a-f]{64}$'
        OR NOT cloud_agents.is_valid_identifier(p_request_id)
        OR p_subject_digest !~ '^sha256:[0-9a-f]{64}$'
        OR pg_catalog.char_length(p_impact_summary) NOT BETWEEN 1 AND 256
        OR p_impact_summary ~ '[[:cntrl:]]'
    THEN RAISE EXCEPTION USING ERRCODE = '22023', MESSAGE = 'deployment target scheduling input is invalid'; END IF;

    SELECT target.* INTO existing_target
    FROM cloud_agents.deployment_targets AS target
    WHERE target.tenant_id = p_tenant_id AND target.project_uid = p_project_uid
        AND target.target_uid = p_target_uid
    FOR UPDATE;
    IF NOT FOUND THEN RETURN; END IF;

    action_value := CASE WHEN p_desired_state = 'drained' THEN 'target.drain' ELSE 'target.resume' END;
    SELECT event.* INTO activity
    FROM cloud_agents.deployment_target_activity AS event
    WHERE event.tenant_id = p_tenant_id AND event.project_uid = p_project_uid
        AND event.target_uid = p_target_uid
        AND event.action IN ('target.drain', 'target.resume')
        AND event.idempotency_key = p_idempotency_key
    ORDER BY event.occurred_at DESC, event.event_uid DESC
    LIMIT 1;
    IF FOUND THEN
        IF activity.request_digest IS DISTINCT FROM p_request_digest
            OR activity.target_generation <> p_expected_generation
            OR activity.action <> action_value
        THEN RAISE EXCEPTION USING ERRCODE = '23505', MESSAGE = 'deployment target scheduling idempotency conflict'; END IF;
    ELSE
        IF existing_target.generation <> p_expected_generation THEN
            RAISE EXCEPTION USING ERRCODE = '23505', MESSAGE = 'deployment target generation conflict';
        END IF;
        IF existing_target.resource_version <> p_expected_resource_version THEN
            RAISE EXCEPTION USING ERRCODE = '23505', MESSAGE = 'deployment target resource version conflict';
        END IF;
        IF existing_target.scheduling_state = p_desired_state THEN
            RAISE EXCEPTION USING ERRCODE = '23505', MESSAGE = 'deployment target scheduling state conflict';
        END IF;
        IF p_impact_digest IS DISTINCT FROM p_current_impact_digest THEN
            RAISE EXCEPTION USING ERRCODE = '23505', MESSAGE = 'deployment target scheduling impact conflict';
        END IF;

        UPDATE cloud_agents.deployment_targets AS target
        SET scheduling_state = p_desired_state,
            resource_version = target.resource_version + 1,
            updated_at = mutation_at
        WHERE target.tenant_id = p_tenant_id AND target.project_uid = p_project_uid
            AND target.target_uid = p_target_uid;

        operation_uid_value := 'op-' || pg_catalog.md5(
            p_tenant_id || '|' || p_project_uid || '|' || p_target_uid || '|' || action_value || '|' || p_idempotency_key
        );
        INSERT INTO cloud_agents.deployment_target_activity (
            tenant_id, project_uid, target_uid, event_uid, operation_uid, action, idempotency_key,
            request_id, request_digest, subject_digest, target_generation, state, current_step,
            stable_error_code, impact_summary, retryable, requested_at, occurred_at
        ) VALUES (
            p_tenant_id, p_project_uid, p_target_uid, operation_uid_value || '-succeeded', operation_uid_value,
            action_value, p_idempotency_key, p_request_id, p_request_digest, p_subject_digest,
            p_expected_generation, 'succeeded', 'complete', '', p_impact_summary, false, mutation_at, mutation_at
        ) RETURNING deployment_target_activity.* INTO activity;
    END IF;

    operation_uid := activity.operation_uid;
    idempotency_key := activity.idempotency_key;
    action := activity.action;
    target_uid := activity.target_uid;
    target_generation := activity.target_generation;
    subject_digest := activity.subject_digest;
    request_id := activity.request_id;
    requested_at := activity.requested_at;
    updated_at := activity.occurred_at;
    state := activity.state;
    current_step := activity.current_step;
    stable_error_code := activity.stable_error_code;
    impact_summary := activity.impact_summary;
    retryable := activity.retryable;
    RETURN NEXT;
END;
$cloud_agents_function$;

CREATE FUNCTION cloud_agents.create_managed_host_environment_lease_v4(
    p_tenant_id text, p_project_uid text, p_lease_uid text, p_lease_name text,
    p_release_digest text, p_target_uid text, p_expected_target_generation bigint,
    p_provider_credential_ref text, p_cpu_limit_millis bigint, p_memory_limit_bytes bigint,
    p_ttl_seconds bigint, p_idempotency_key text, p_request_digest text
)
RETURNS TABLE (
    lease_uid text, lease_name text, release_digest text,
    deployment_target_uid text, deployment_target_generation bigint,
    provider_credential_ref text, cpu_limit_millis bigint, memory_limit_bytes bigint,
    generation bigint, desired_phase text, observed_phase text, cleanup_phase text, environment_id text,
    worker_endpoint text, worker_spiffe_id text, stable_error_code text,
    expires_at timestamptz, resource_version bigint, created_at timestamptz, updated_at timestamptz
)
LANGUAGE plpgsql SECURITY DEFINER SET search_path = pg_catalog, cloud_agents
AS $cloud_agents_function$
DECLARE
    ignored_principal text;
    existing cloud_agents.managed_host_environment_leases%ROWTYPE;
BEGIN
    ignored_principal := cloud_agents.require_runtime_mutation_principal();
    SELECT lease.* INTO existing
    FROM cloud_agents.managed_host_environment_leases AS lease
    WHERE lease.tenant_id = p_tenant_id AND lease.project_uid = p_project_uid
        AND lease.create_idempotency_key = p_idempotency_key
    FOR UPDATE;
    IF NOT FOUND THEN
        PERFORM 1 FROM cloud_agents.deployment_targets AS target
        WHERE target.tenant_id = p_tenant_id AND target.project_uid = p_project_uid
            AND target.target_uid = p_target_uid
            AND target.generation = p_expected_target_generation
            AND target.observed_phase = 'ready' AND target.scheduling_state = 'active'
        FOR SHARE;
        IF NOT FOUND THEN
            RAISE EXCEPTION USING ERRCODE = '23505', MESSAGE = 'deployment target is not accepting new leases';
        END IF;
    END IF;

    RETURN QUERY SELECT * FROM cloud_agents.create_managed_host_environment_lease_v3(
        p_tenant_id, p_project_uid, p_lease_uid, p_lease_name, p_release_digest,
        p_target_uid, p_expected_target_generation, p_provider_credential_ref,
        p_cpu_limit_millis, p_memory_limit_bytes, p_ttl_seconds,
        p_idempotency_key, p_request_digest
    );
END;
$cloud_agents_function$;

CREATE FUNCTION cloud_agents.create_user_environment_v2(
    p_tenant_id text, p_project_uid text, p_environment_uid text,
    p_profile_uid text, p_profile_version bigint, p_ttl_seconds bigint,
    p_idempotency_key text, p_request_digest text
)
RETURNS TABLE (
    lease_uid text, lease_name text, release_digest text,
    deployment_target_uid text, deployment_target_generation bigint,
    provider_credential_ref text, cpu_limit_millis bigint, memory_limit_bytes bigint,
    generation bigint, desired_phase text, observed_phase text, cleanup_phase text,
    environment_id text, worker_endpoint text, worker_spiffe_id text, worker_server_name text,
    stable_error_code text, expires_at timestamptz, resource_version bigint,
    created_at timestamptz, updated_at timestamptz,
    environment_profile_uid text, environment_profile_version bigint
)
LANGUAGE plpgsql SECURITY DEFINER SET search_path = pg_catalog, cloud_agents
AS $cloud_agents_function$
DECLARE
    ignored_principal text;
    existing cloud_agents.managed_host_environment_leases%ROWTYPE;
    selected_profile cloud_agents.environment_profiles%ROWTYPE;
    selected_target cloud_agents.deployment_targets%ROWTYPE;
BEGIN
    ignored_principal := cloud_agents.require_runtime_mutation_principal();
    IF p_tenant_id IS DISTINCT FROM cloud_agents.require_tenant_id()
        OR NOT cloud_agents.is_valid_identifier(p_project_uid)
        OR NOT cloud_agents.is_valid_identifier(p_environment_uid)
        OR NOT cloud_agents.is_valid_identifier(p_profile_uid)
        OR p_profile_version NOT BETWEEN 1 AND 2147483647
        OR p_ttl_seconds NOT BETWEEN 60 AND 86400
        OR p_idempotency_key !~ '^[A-Za-z0-9._~-]{16,128}$'
        OR p_request_digest !~ '^sha256:[0-9a-f]{64}$'
    THEN RAISE EXCEPTION USING ERRCODE = '22023', MESSAGE = 'user environment input is invalid'; END IF;

    SELECT lease.* INTO existing
    FROM cloud_agents.managed_host_environment_leases AS lease
    WHERE lease.tenant_id = p_tenant_id AND lease.project_uid = p_project_uid
        AND lease.create_idempotency_key = p_idempotency_key
    FOR UPDATE;
    IF FOUND THEN
        IF existing.create_request_digest IS DISTINCT FROM p_request_digest
            OR existing.environment_profile_uid IS DISTINCT FROM p_profile_uid
            OR existing.environment_profile_version IS DISTINCT FROM p_profile_version
        THEN RAISE EXCEPTION USING ERRCODE = '23505', MESSAGE = 'user environment idempotency conflict'; END IF;
    ELSE
        SELECT profile.* INTO selected_profile
        FROM cloud_agents.environment_profiles AS profile
        WHERE profile.tenant_id = p_tenant_id AND profile.project_uid = p_project_uid
            AND profile.profile_uid = p_profile_uid AND profile.profile_version = p_profile_version
            AND profile.status = 'published'
        FOR SHARE;
        IF NOT FOUND THEN
            RAISE EXCEPTION USING ERRCODE = '23505', MESSAGE = 'environment profile is not available';
        END IF;

        SELECT target.* INTO selected_target
        FROM cloud_agents.deployment_targets AS target
        WHERE target.tenant_id = p_tenant_id AND target.project_uid = p_project_uid
            AND target.target_uid = ANY(selected_profile.target_refs)
            AND target.observed_phase = 'ready' AND target.scheduling_state = 'active'
        ORDER BY pg_catalog.array_position(selected_profile.target_refs, target.target_uid), target.target_uid
        LIMIT 1 FOR SHARE;
        IF NOT FOUND THEN
            RAISE EXCEPTION USING ERRCODE = '23505', MESSAGE = 'environment profile is not available';
        END IF;

        PERFORM * FROM cloud_agents.create_managed_host_environment_lease_v4(
            p_tenant_id, p_project_uid, p_environment_uid, p_environment_uid,
            selected_profile.release_digest, selected_target.target_uid, selected_target.generation,
            selected_profile.provider_credential_ref, selected_profile.cpu_limit_millis,
            selected_profile.memory_limit_bytes, p_ttl_seconds, p_idempotency_key, p_request_digest
        );
        UPDATE cloud_agents.managed_host_environment_leases AS lease
        SET environment_profile_uid = p_profile_uid,
            environment_profile_version = p_profile_version
        WHERE lease.tenant_id = p_tenant_id AND lease.project_uid = p_project_uid
            AND lease.lease_uid = p_environment_uid
        RETURNING lease.* INTO existing;
    END IF;

    lease_uid := existing.lease_uid; lease_name := existing.lease_name;
    release_digest := existing.release_digest;
    deployment_target_uid := existing.deployment_target_uid;
    deployment_target_generation := existing.deployment_target_generation;
    provider_credential_ref := existing.provider_credential_ref;
    cpu_limit_millis := existing.cpu_limit_millis; memory_limit_bytes := existing.memory_limit_bytes;
    generation := existing.generation; desired_phase := existing.desired_phase;
    observed_phase := existing.observed_phase; cleanup_phase := existing.cleanup_phase;
    environment_id := existing.environment_id; worker_endpoint := existing.worker_endpoint;
    worker_spiffe_id := existing.worker_spiffe_id; worker_server_name := existing.worker_server_name;
    stable_error_code := existing.stable_error_code; expires_at := existing.expires_at;
    resource_version := existing.resource_version; created_at := existing.created_at;
    updated_at := existing.updated_at; environment_profile_uid := existing.environment_profile_uid;
    environment_profile_version := existing.environment_profile_version;
    RETURN NEXT;
END;
$cloud_agents_function$;

ALTER FUNCTION cloud_agents.lock_deployment_target_scheduling_v1(text, text, text)
    OWNER TO cloud_agents_migration_owner;
ALTER FUNCTION cloud_agents.transition_deployment_target_scheduling_v1(
    text, text, text, bigint, bigint, text, text, text, text, text, text, text, text
) OWNER TO cloud_agents_migration_owner;
ALTER FUNCTION cloud_agents.create_managed_host_environment_lease_v4(
    text, text, text, text, text, text, bigint, text, bigint, bigint, bigint, text, text
) OWNER TO cloud_agents_migration_owner;
ALTER FUNCTION cloud_agents.create_user_environment_v2(text, text, text, text, bigint, bigint, text, text)
    OWNER TO cloud_agents_migration_owner;

REVOKE EXECUTE ON FUNCTION cloud_agents.create_managed_host_environment_lease_v3(
    text, text, text, text, text, text, bigint, text, bigint, bigint, bigint, text, text
) FROM cloud_agents_runtime;
REVOKE EXECUTE ON FUNCTION cloud_agents.create_user_environment_v1(text, text, text, text, bigint, bigint, text, text)
    FROM cloud_agents_runtime;
REVOKE ALL ON FUNCTION cloud_agents.lock_deployment_target_scheduling_v1(text, text, text)
    FROM PUBLIC;
REVOKE ALL ON FUNCTION cloud_agents.transition_deployment_target_scheduling_v1(
    text, text, text, bigint, bigint, text, text, text, text, text, text, text, text
) FROM PUBLIC;
REVOKE ALL ON FUNCTION cloud_agents.create_managed_host_environment_lease_v4(
    text, text, text, text, text, text, bigint, text, bigint, bigint, bigint, text, text
) FROM PUBLIC;
REVOKE ALL ON FUNCTION cloud_agents.create_user_environment_v2(text, text, text, text, bigint, bigint, text, text)
    FROM PUBLIC;
GRANT EXECUTE ON FUNCTION cloud_agents.lock_deployment_target_scheduling_v1(text, text, text)
    TO cloud_agents_runtime;
GRANT EXECUTE ON FUNCTION cloud_agents.transition_deployment_target_scheduling_v1(
    text, text, text, bigint, bigint, text, text, text, text, text, text, text, text
) TO cloud_agents_runtime;
GRANT EXECUTE ON FUNCTION cloud_agents.create_managed_host_environment_lease_v4(
    text, text, text, text, text, text, bigint, text, bigint, bigint, bigint, text, text
) TO cloud_agents_runtime;
GRANT EXECUTE ON FUNCTION cloud_agents.create_user_environment_v2(text, text, text, text, bigint, bigint, text, text)
    TO cloud_agents_runtime;
