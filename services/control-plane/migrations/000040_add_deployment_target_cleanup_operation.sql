ALTER TABLE cloud_agents.deployment_target_activity
    DROP CONSTRAINT deployment_target_activity_action;
ALTER TABLE cloud_agents.deployment_target_activity
    ADD CONSTRAINT deployment_target_activity_action
    CHECK (action IN ('target.register', 'target.probe', 'target.cleanup'));

CREATE FUNCTION cloud_agents.begin_deployment_target_cleanup_v1(
    p_tenant_id text, p_project_uid text, p_target_uid text,
    p_expected_generation bigint, p_expected_resource_version bigint, p_impact_digest text,
    p_idempotency_key text, p_request_digest text, p_request_id text, p_subject_digest text
)
RETURNS TABLE (
    operation_uid text, idempotency_key text, action text, target_uid text,
    target_generation bigint, subject_digest text, request_id text,
    requested_at timestamptz, updated_at timestamptz, state text, current_step text,
    stable_error_code text, impact_summary text, retryable boolean, execute_cleanup boolean
)
LANGUAGE plpgsql SECURITY DEFINER SET search_path = pg_catalog, cloud_agents
AS $cloud_agents_function$
DECLARE
    ignored_principal text;
    mutation_at timestamptz;
    existing_target cloud_agents.deployment_targets%ROWTYPE;
    activity cloud_agents.deployment_target_activity%ROWTYPE;
BEGIN
    ignored_principal := cloud_agents.require_runtime_mutation_principal();
    mutation_at := pg_catalog.transaction_timestamp();
    IF p_tenant_id IS DISTINCT FROM cloud_agents.require_tenant_id()
        OR NOT cloud_agents.is_valid_identifier(p_project_uid)
        OR NOT cloud_agents.is_valid_identifier(p_target_uid)
        OR p_expected_generation < 1 OR p_expected_resource_version < 1
        OR p_impact_digest !~ '^sha256:[0-9a-f]{64}$'
        OR p_idempotency_key !~ '^[A-Za-z0-9._~-]{16,128}$'
        OR p_request_digest !~ '^sha256:[0-9a-f]{64}$'
        OR NOT cloud_agents.is_valid_identifier(p_request_id)
        OR p_subject_digest !~ '^sha256:[0-9a-f]{64}$'
    THEN RAISE EXCEPTION USING ERRCODE = '22023', MESSAGE = 'deployment target cleanup input is invalid'; END IF;

    SELECT target.* INTO existing_target
    FROM cloud_agents.deployment_targets AS target
    WHERE target.tenant_id = p_tenant_id AND target.project_uid = p_project_uid
        AND target.target_uid = p_target_uid
    FOR UPDATE;
    IF NOT FOUND THEN RETURN; END IF;

    SELECT event.* INTO activity
    FROM cloud_agents.deployment_target_activity AS event
    WHERE event.tenant_id = p_tenant_id AND event.project_uid = p_project_uid
        AND event.target_uid = p_target_uid AND event.action = 'target.cleanup'
        AND event.idempotency_key = p_idempotency_key
    ORDER BY event.occurred_at DESC, event.event_uid DESC
    LIMIT 1;
    IF FOUND THEN
        IF activity.request_digest IS DISTINCT FROM p_request_digest
            OR activity.target_generation <> p_expected_generation
        THEN RAISE EXCEPTION USING ERRCODE = '23505', MESSAGE = 'deployment target cleanup idempotency conflict'; END IF;
        execute_cleanup := false;
    ELSE
        IF existing_target.generation <> p_expected_generation THEN
            RAISE EXCEPTION USING ERRCODE = '23505', MESSAGE = 'deployment target generation conflict';
        END IF;
        IF existing_target.resource_version <> p_expected_resource_version THEN
            RAISE EXCEPTION USING ERRCODE = '23505', MESSAGE = 'deployment target resource version conflict';
        END IF;
        IF existing_target.observed_phase <> 'ready' THEN
            RAISE EXCEPTION USING ERRCODE = '23505', MESSAGE = 'deployment target is not ready';
        END IF;
        PERFORM 1
        FROM cloud_agents.deployment_target_activity AS running
        WHERE running.tenant_id = p_tenant_id AND running.project_uid = p_project_uid
            AND running.target_uid = p_target_uid AND running.action = 'target.cleanup'
            AND running.state = 'running'
            AND NOT EXISTS (
                SELECT 1 FROM cloud_agents.deployment_target_activity AS terminal
                WHERE terminal.tenant_id = running.tenant_id
                    AND terminal.project_uid = running.project_uid
                    AND terminal.target_uid = running.target_uid
                    AND terminal.operation_uid = running.operation_uid
                    AND terminal.state IN ('succeeded', 'failed')
            );
        IF FOUND THEN RAISE EXCEPTION USING ERRCODE = '23505', MESSAGE = 'deployment target cleanup is already running'; END IF;

        operation_uid := 'op-' || pg_catalog.md5(p_tenant_id || '|' || p_project_uid || '|' || p_target_uid || '|target.cleanup|' || p_idempotency_key);
        INSERT INTO cloud_agents.deployment_target_activity (
            tenant_id, project_uid, target_uid, event_uid, operation_uid, action, idempotency_key,
            request_id, request_digest, subject_digest, target_generation, state, current_step,
            stable_error_code, impact_summary, retryable, requested_at, occurred_at
        ) VALUES (
            p_tenant_id, p_project_uid, p_target_uid, operation_uid || '-requested', operation_uid,
            'target.cleanup', p_idempotency_key, p_request_id, p_request_digest, p_subject_digest,
            p_expected_generation, 'running', 'cleanup', '',
            'Clean deployment target resources confirmed by preview', false, mutation_at, mutation_at
        ) RETURNING deployment_target_activity.* INTO activity;
        execute_cleanup := true;
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

CREATE FUNCTION cloud_agents.complete_deployment_target_cleanup_v1(
    p_tenant_id text, p_project_uid text, p_target_uid text, p_expected_generation bigint,
    p_idempotency_key text, p_request_digest text, p_succeeded boolean,
    p_stable_error_code text, p_impact_summary text
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
BEGIN
    ignored_principal := cloud_agents.require_runtime_mutation_principal();
    mutation_at := pg_catalog.transaction_timestamp();
    IF p_tenant_id IS DISTINCT FROM cloud_agents.require_tenant_id()
        OR NOT cloud_agents.is_valid_identifier(p_project_uid)
        OR NOT cloud_agents.is_valid_identifier(p_target_uid)
        OR p_expected_generation < 1
        OR p_idempotency_key !~ '^[A-Za-z0-9._~-]{16,128}$'
        OR p_request_digest !~ '^sha256:[0-9a-f]{64}$'
        OR pg_catalog.char_length(p_impact_summary) NOT BETWEEN 1 AND 256
        OR p_impact_summary ~ '[[:cntrl:]]'
        OR (p_succeeded AND p_stable_error_code <> '')
        OR (NOT p_succeeded AND NOT cloud_agents.is_valid_identifier(p_stable_error_code))
    THEN RAISE EXCEPTION USING ERRCODE = '22023', MESSAGE = 'deployment target cleanup completion is invalid'; END IF;

    PERFORM 1 FROM cloud_agents.deployment_targets AS target
    WHERE target.tenant_id = p_tenant_id AND target.project_uid = p_project_uid
        AND target.target_uid = p_target_uid
    FOR UPDATE;
    IF NOT FOUND THEN RETURN; END IF;

    SELECT event.* INTO activity
    FROM cloud_agents.deployment_target_activity AS event
    WHERE event.tenant_id = p_tenant_id AND event.project_uid = p_project_uid
        AND event.target_uid = p_target_uid AND event.action = 'target.cleanup'
        AND event.idempotency_key = p_idempotency_key
    ORDER BY event.occurred_at DESC, event.event_uid DESC
    LIMIT 1;
    IF NOT FOUND THEN RETURN; END IF;
    IF activity.request_digest IS DISTINCT FROM p_request_digest
        OR activity.target_generation <> p_expected_generation
    THEN RAISE EXCEPTION USING ERRCODE = '23505', MESSAGE = 'deployment target cleanup idempotency conflict'; END IF;

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
            p_stable_error_code, p_impact_summary, NOT p_succeeded, activity.requested_at, mutation_at
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

ALTER FUNCTION cloud_agents.begin_deployment_target_cleanup_v1(text, text, text, bigint, bigint, text, text, text, text, text)
    OWNER TO cloud_agents_migration_owner;
ALTER FUNCTION cloud_agents.complete_deployment_target_cleanup_v1(text, text, text, bigint, text, text, boolean, text, text)
    OWNER TO cloud_agents_migration_owner;
REVOKE ALL ON FUNCTION cloud_agents.begin_deployment_target_cleanup_v1(text, text, text, bigint, bigint, text, text, text, text, text) FROM PUBLIC;
REVOKE ALL ON FUNCTION cloud_agents.complete_deployment_target_cleanup_v1(text, text, text, bigint, text, text, boolean, text, text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION cloud_agents.begin_deployment_target_cleanup_v1(text, text, text, bigint, bigint, text, text, text, text, text) TO cloud_agents_runtime;
GRANT EXECUTE ON FUNCTION cloud_agents.complete_deployment_target_cleanup_v1(text, text, text, bigint, text, text, boolean, text, text) TO cloud_agents_runtime;
