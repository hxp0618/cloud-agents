ALTER TABLE cloud_agents.managed_host_environment_leases
    ADD COLUMN rollback_release_digest text;
ALTER TABLE cloud_agents.managed_host_environment_leases
    ADD COLUMN rollback_generation bigint;
ALTER TABLE cloud_agents.managed_host_environment_leases
    ADD CONSTRAINT managed_host_leases_rollback_pair CHECK (
        (rollback_release_digest IS NULL) = (rollback_generation IS NULL)
    );
ALTER TABLE cloud_agents.managed_host_environment_leases
    ADD CONSTRAINT managed_host_leases_rollback_digest CHECK (
        rollback_release_digest IS NULL OR rollback_release_digest ~ '^sha256:[0-9a-f]{64}$'
    );
ALTER TABLE cloud_agents.managed_host_environment_leases
    ADD CONSTRAINT managed_host_leases_rollback_generation CHECK (
        rollback_generation IS NULL OR rollback_generation > 0
    );

CREATE FUNCTION cloud_agents.track_managed_host_environment_lease_release_v1()
RETURNS trigger
LANGUAGE plpgsql SET search_path = pg_catalog, cloud_agents
AS $cloud_agents_function$
BEGIN
    IF NEW.release_digest IS DISTINCT FROM OLD.release_digest THEN
        PERFORM 1 FROM cloud_agents.worker_releases AS release
        WHERE release.tenant_id = NEW.tenant_id AND release.project_uid = NEW.project_uid
            AND release.release_digest = NEW.release_digest
            AND release.status = 'approved' AND release.verification_state = 'attested';
        IF NOT FOUND THEN
            RAISE EXCEPTION USING ERRCODE = '23503', MESSAGE = 'Worker release is not approved';
        END IF;
        NEW.rollback_release_digest := OLD.release_digest;
        NEW.rollback_generation := OLD.generation;
    END IF;
    RETURN NEW;
END;
$cloud_agents_function$;

ALTER FUNCTION cloud_agents.track_managed_host_environment_lease_release_v1()
    OWNER TO cloud_agents_migration_owner;
REVOKE ALL ON FUNCTION cloud_agents.track_managed_host_environment_lease_release_v1() FROM PUBLIC;

CREATE TRIGGER managed_host_environment_lease_release_guard
BEFORE UPDATE OF release_digest ON cloud_agents.managed_host_environment_leases
FOR EACH ROW EXECUTE FUNCTION cloud_agents.track_managed_host_environment_lease_release_v1();

ALTER TABLE cloud_agents.deployment_target_activity
    DROP CONSTRAINT deployment_target_activity_action;
ALTER TABLE cloud_agents.deployment_target_activity
    ADD CONSTRAINT deployment_target_activity_action
    CHECK (action IN (
        'target.register', 'target.probe', 'target.drain', 'target.resume', 'target.cleanup',
        'target.upgrade', 'target.rollback'
    ));

CREATE FUNCTION cloud_agents.begin_admin_environment_lease_upgrade_v1(
    p_tenant_id text, p_project_uid text, p_lease_uid text, p_action text,
    p_release_digest text, p_expected_generation bigint, p_expected_resource_version bigint,
    p_impact_digest text, p_current_impact_digest text, p_idempotency_key text,
    p_request_digest text, p_request_id text, p_subject_digest text, p_impact_summary text
)
RETURNS TABLE (
    operation_uid text, idempotency_key text, action text, target_uid text,
    target_generation bigint, subject_digest text, request_id text,
    requested_at timestamptz, updated_at timestamptz, state text, current_step text,
    stable_error_code text, impact_summary text, retryable boolean, execute_upgrade boolean
)
LANGUAGE plpgsql SECURITY DEFINER SET search_path = pg_catalog, cloud_agents
AS $cloud_agents_function$
DECLARE
    ignored_principal text;
    mutation_at timestamptz;
    existing_lease cloud_agents.managed_host_environment_leases%ROWTYPE;
    existing_target cloud_agents.deployment_targets%ROWTYPE;
    activity cloud_agents.deployment_target_activity%ROWTYPE;
    action_value text;
    operation_uid_value text;
BEGIN
    ignored_principal := cloud_agents.require_runtime_mutation_principal();
    mutation_at := pg_catalog.transaction_timestamp();
    IF p_tenant_id IS DISTINCT FROM cloud_agents.require_tenant_id()
        OR NOT cloud_agents.is_valid_identifier(p_project_uid)
        OR NOT cloud_agents.is_valid_identifier(p_lease_uid)
        OR p_action NOT IN ('upgrade', 'rollback')
        OR p_release_digest !~ '^sha256:[0-9a-f]{64}$'
        OR p_expected_generation < 1 OR p_expected_resource_version < 1
        OR p_impact_digest !~ '^sha256:[0-9a-f]{64}$'
        OR p_current_impact_digest !~ '^sha256:[0-9a-f]{64}$'
        OR p_idempotency_key !~ '^[A-Za-z0-9._~-]{16,128}$'
        OR p_request_digest !~ '^sha256:[0-9a-f]{64}$'
        OR NOT cloud_agents.is_valid_identifier(p_request_id)
        OR p_subject_digest !~ '^sha256:[0-9a-f]{64}$'
        OR pg_catalog.char_length(p_impact_summary) NOT BETWEEN 1 AND 256
        OR p_impact_summary ~ '[[:cntrl:]]'
    THEN
        RAISE EXCEPTION USING ERRCODE = '22023', MESSAGE = 'Admin environment lease upgrade input is invalid';
    END IF;

    SELECT lease.* INTO existing_lease
    FROM cloud_agents.managed_host_environment_leases AS lease
    WHERE lease.tenant_id = p_tenant_id AND lease.project_uid = p_project_uid
        AND lease.lease_uid = p_lease_uid;
    IF NOT FOUND THEN RETURN; END IF;

    SELECT target.* INTO existing_target
    FROM cloud_agents.deployment_targets AS target
    WHERE target.tenant_id = p_tenant_id AND target.project_uid = p_project_uid
        AND target.target_uid = existing_lease.deployment_target_uid
    FOR UPDATE;
    IF NOT FOUND THEN RETURN; END IF;

    SELECT lease.* INTO existing_lease
    FROM cloud_agents.managed_host_environment_leases AS lease
    WHERE lease.tenant_id = p_tenant_id AND lease.project_uid = p_project_uid
        AND lease.lease_uid = p_lease_uid
    FOR UPDATE;
    IF NOT FOUND THEN RETURN; END IF;

    action_value := CASE WHEN p_action = 'upgrade' THEN 'target.upgrade' ELSE 'target.rollback' END;
    SELECT event.* INTO activity
    FROM cloud_agents.deployment_target_activity AS event
    WHERE event.tenant_id = p_tenant_id AND event.project_uid = p_project_uid
        AND event.target_uid = existing_target.target_uid
        AND event.action IN ('target.upgrade', 'target.rollback')
        AND event.idempotency_key = p_idempotency_key
    ORDER BY event.occurred_at DESC, event.event_uid DESC
    LIMIT 1;
    IF FOUND THEN
        IF activity.request_digest IS DISTINCT FROM p_request_digest OR activity.action <> action_value THEN
            RAISE EXCEPTION USING ERRCODE = '23505', MESSAGE = 'environment lease upgrade idempotency conflict';
        END IF;
        execute_upgrade := activity.state = 'running';
    ELSE
        IF existing_lease.generation <> p_expected_generation THEN
            RAISE EXCEPTION USING ERRCODE = '23505', MESSAGE = 'environment lease generation conflict';
        END IF;
        IF existing_lease.resource_version <> p_expected_resource_version THEN
            RAISE EXCEPTION USING ERRCODE = '23505', MESSAGE = 'environment lease resource version conflict';
        END IF;
        IF existing_lease.desired_phase <> 'active'
            OR existing_lease.observed_phase NOT IN ('ready', 'failed')
            OR existing_lease.cleanup_phase <> 'none'
        THEN
            RAISE EXCEPTION USING ERRCODE = '23505', MESSAGE = 'environment lease upgrade state conflict';
        END IF;
        IF existing_target.generation IS DISTINCT FROM existing_lease.deployment_target_generation
            OR existing_target.observed_phase <> 'ready' OR existing_target.scheduling_state <> 'drained'
        THEN
            RAISE EXCEPTION USING ERRCODE = '23505', MESSAGE = 'deployment target is not ready and drained';
        END IF;
        IF p_release_digest = existing_lease.release_digest THEN
            RAISE EXCEPTION USING ERRCODE = '23505', MESSAGE = 'environment lease release is unchanged';
        END IF;
        IF p_action = 'rollback' AND (
            existing_lease.rollback_release_digest IS DISTINCT FROM p_release_digest
            OR existing_lease.rollback_generation IS NULL
        ) THEN
            RAISE EXCEPTION USING ERRCODE = '23505', MESSAGE = 'environment lease rollback target conflict';
        END IF;
        PERFORM 1 FROM cloud_agents.worker_releases AS release
        WHERE release.tenant_id = p_tenant_id AND release.project_uid = p_project_uid
            AND release.release_digest = p_release_digest
            AND release.status = 'approved' AND release.verification_state = 'attested'
        FOR KEY SHARE;
        IF NOT FOUND THEN
            RAISE EXCEPTION USING ERRCODE = '23503', MESSAGE = 'Worker release is not approved';
        END IF;
        IF p_impact_digest IS DISTINCT FROM p_current_impact_digest THEN
            RAISE EXCEPTION USING ERRCODE = '23505', MESSAGE = 'environment lease upgrade impact conflict';
        END IF;

        UPDATE cloud_agents.managed_host_environment_leases AS lease
        SET release_digest = p_release_digest, observed_phase = 'provisioning',
            worker_endpoint = '', worker_spiffe_id = '', worker_server_name = '', stable_error_code = '',
            generation = existing_lease.generation + 1,
            resource_version = existing_lease.resource_version + 1,
            updated_at = mutation_at
        WHERE lease.tenant_id = p_tenant_id AND lease.project_uid = p_project_uid
            AND lease.lease_uid = p_lease_uid;

        operation_uid_value := 'op-' || pg_catalog.md5(
            p_tenant_id || '|' || p_project_uid || '|' || existing_target.target_uid || '|' || action_value || '|' || p_idempotency_key
        );
        INSERT INTO cloud_agents.deployment_target_activity (
            tenant_id, project_uid, target_uid, event_uid, operation_uid, action, idempotency_key,
            request_id, request_digest, subject_digest, target_generation, state, current_step,
            stable_error_code, impact_summary, retryable, requested_at, occurred_at
        ) VALUES (
            p_tenant_id, p_project_uid, existing_target.target_uid,
            operation_uid_value || '-requested', operation_uid_value, action_value,
            p_idempotency_key, p_request_id, p_request_digest, p_subject_digest,
            existing_target.generation, 'running',
            CASE WHEN p_action = 'upgrade' THEN 'worker-upgrade' ELSE 'worker-rollback' END,
            '', p_impact_summary, false, mutation_at, mutation_at
        ) RETURNING deployment_target_activity.* INTO activity;
        execute_upgrade := true;
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

CREATE FUNCTION cloud_agents.complete_admin_environment_lease_upgrade_v1(
    p_tenant_id text, p_project_uid text, p_target_uid text, p_action text,
    p_expected_target_generation bigint, p_idempotency_key text, p_request_digest text,
    p_succeeded boolean, p_stable_error_code text, p_impact_summary text
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
    activity cloud_agents.deployment_target_activity%ROWTYPE;
    action_value text;
BEGIN
    ignored_principal := cloud_agents.require_runtime_mutation_principal();
    mutation_at := pg_catalog.transaction_timestamp();
    action_value := CASE WHEN p_action = 'upgrade' THEN 'target.upgrade' ELSE 'target.rollback' END;
    IF p_tenant_id IS DISTINCT FROM cloud_agents.require_tenant_id()
        OR NOT cloud_agents.is_valid_identifier(p_project_uid)
        OR NOT cloud_agents.is_valid_identifier(p_target_uid)
        OR p_action NOT IN ('upgrade', 'rollback')
        OR p_expected_target_generation < 1
        OR p_idempotency_key !~ '^[A-Za-z0-9._~-]{16,128}$'
        OR p_request_digest !~ '^sha256:[0-9a-f]{64}$'
        OR (p_succeeded AND p_stable_error_code <> '')
        OR (NOT p_succeeded AND NOT cloud_agents.is_valid_identifier(p_stable_error_code))
        OR pg_catalog.char_length(p_impact_summary) NOT BETWEEN 1 AND 256
        OR p_impact_summary ~ '[[:cntrl:]]'
    THEN
        RAISE EXCEPTION USING ERRCODE = '22023', MESSAGE = 'Admin environment lease upgrade completion is invalid';
    END IF;

    SELECT event.* INTO activity
    FROM cloud_agents.deployment_target_activity AS event
    WHERE event.tenant_id = p_tenant_id AND event.project_uid = p_project_uid
        AND event.target_uid = p_target_uid AND event.action = action_value
        AND event.idempotency_key = p_idempotency_key
    ORDER BY event.occurred_at DESC, event.event_uid DESC
    LIMIT 1;
    IF NOT FOUND THEN RETURN; END IF;
    IF activity.request_digest IS DISTINCT FROM p_request_digest
        OR activity.target_generation <> p_expected_target_generation
    THEN
        RAISE EXCEPTION USING ERRCODE = '23505', MESSAGE = 'environment lease upgrade idempotency conflict';
    END IF;

    IF activity.state = 'running' THEN
        INSERT INTO cloud_agents.deployment_target_activity (
            tenant_id, project_uid, target_uid, event_uid, operation_uid, action, idempotency_key,
            request_id, request_digest, subject_digest, target_generation, state, current_step,
            stable_error_code, impact_summary, retryable, requested_at, occurred_at
        ) VALUES (
            activity.tenant_id, activity.project_uid, activity.target_uid,
            activity.operation_uid || CASE WHEN p_succeeded THEN '-succeeded' ELSE '-failed' END,
            activity.operation_uid, activity.action, activity.idempotency_key, activity.request_id,
            activity.request_digest, activity.subject_digest, activity.target_generation,
            CASE WHEN p_succeeded THEN 'succeeded' ELSE 'failed' END,
            CASE WHEN p_succeeded THEN 'complete' ELSE 'failed' END,
            p_stable_error_code, p_impact_summary, NOT p_succeeded,
            activity.requested_at, mutation_at
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

ALTER FUNCTION cloud_agents.begin_admin_environment_lease_upgrade_v1(
    text, text, text, text, text, bigint, bigint, text, text, text, text, text, text, text
) OWNER TO cloud_agents_migration_owner;
ALTER FUNCTION cloud_agents.complete_admin_environment_lease_upgrade_v1(
    text, text, text, text, bigint, text, text, boolean, text, text
) OWNER TO cloud_agents_migration_owner;
REVOKE ALL ON FUNCTION cloud_agents.begin_admin_environment_lease_upgrade_v1(
    text, text, text, text, text, bigint, bigint, text, text, text, text, text, text, text
) FROM PUBLIC;
REVOKE ALL ON FUNCTION cloud_agents.complete_admin_environment_lease_upgrade_v1(
    text, text, text, text, bigint, text, text, boolean, text, text
) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION cloud_agents.begin_admin_environment_lease_upgrade_v1(
    text, text, text, text, text, bigint, bigint, text, text, text, text, text, text, text
) TO cloud_agents_runtime;
GRANT EXECUTE ON FUNCTION cloud_agents.complete_admin_environment_lease_upgrade_v1(
    text, text, text, text, bigint, text, text, boolean, text, text
) TO cloud_agents_runtime;
