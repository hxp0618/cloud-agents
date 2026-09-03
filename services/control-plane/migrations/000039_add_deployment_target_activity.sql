CREATE TABLE cloud_agents.deployment_target_activity (
    tenant_id text NOT NULL,
    project_uid text NOT NULL,
    target_uid text NOT NULL,
    event_uid text NOT NULL,
    operation_uid text NOT NULL,
    action text NOT NULL,
    idempotency_key text NOT NULL,
    request_id text NOT NULL,
    request_digest text NOT NULL,
    subject_digest text NOT NULL,
    target_generation bigint NOT NULL,
    state text NOT NULL,
    current_step text NOT NULL,
    stable_error_code text NOT NULL,
    impact_summary text NOT NULL,
    retryable boolean NOT NULL,
    requested_at timestamptz NOT NULL,
    occurred_at timestamptz NOT NULL,
    PRIMARY KEY (tenant_id, project_uid, target_uid, event_uid),
    CONSTRAINT deployment_target_activity_target_fk FOREIGN KEY (tenant_id, project_uid, target_uid)
        REFERENCES cloud_agents.deployment_targets (tenant_id, project_uid, target_uid)
        ON UPDATE RESTRICT ON DELETE RESTRICT,
    CONSTRAINT deployment_target_activity_event_uid CHECK (cloud_agents.is_valid_identifier(event_uid)),
    CONSTRAINT deployment_target_activity_operation_uid CHECK (cloud_agents.is_valid_identifier(operation_uid)),
    CONSTRAINT deployment_target_activity_action CHECK (action IN ('target.register', 'target.probe')),
    CONSTRAINT deployment_target_activity_key CHECK (idempotency_key ~ '^[A-Za-z0-9._~-]{16,128}$'),
    CONSTRAINT deployment_target_activity_request_id CHECK (cloud_agents.is_valid_identifier(request_id)),
    CONSTRAINT deployment_target_activity_request_digest CHECK (request_digest ~ '^sha256:[0-9a-f]{64}$'),
    CONSTRAINT deployment_target_activity_subject_digest CHECK (subject_digest ~ '^sha256:[0-9a-f]{64}$'),
    CONSTRAINT deployment_target_activity_generation CHECK (target_generation > 0),
    CONSTRAINT deployment_target_activity_state CHECK (state IN ('running', 'succeeded', 'failed')),
    CONSTRAINT deployment_target_activity_step CHECK (cloud_agents.is_valid_identifier(current_step)),
    CONSTRAINT deployment_target_activity_terminal CHECK (
        (state IN ('running', 'succeeded') AND stable_error_code = '' AND retryable = false)
        OR (state = 'failed' AND cloud_agents.is_valid_identifier(stable_error_code) AND retryable = true)
    ),
    CONSTRAINT deployment_target_activity_impact CHECK (
        pg_catalog.char_length(impact_summary) BETWEEN 1 AND 256
        AND impact_summary !~ '[[:cntrl:]]'
    ),
    CONSTRAINT deployment_target_activity_time CHECK (occurred_at >= requested_at)
);

CREATE INDEX deployment_target_activity_operation_idx
    ON cloud_agents.deployment_target_activity
    (tenant_id, project_uid, target_uid, requested_at DESC, operation_uid DESC, occurred_at DESC);
CREATE INDEX deployment_target_activity_audit_idx
    ON cloud_agents.deployment_target_activity
    (tenant_id, project_uid, target_uid, occurred_at DESC, event_uid DESC);
CREATE UNIQUE INDEX deployment_target_activity_terminal_idx
    ON cloud_agents.deployment_target_activity
    (tenant_id, project_uid, target_uid, operation_uid)
    WHERE state IN ('succeeded', 'failed');

ALTER TABLE cloud_agents.deployment_target_activity OWNER TO cloud_agents_migration_owner;
ALTER TABLE cloud_agents.deployment_target_activity ENABLE ROW LEVEL SECURITY;
ALTER TABLE cloud_agents.deployment_target_activity FORCE ROW LEVEL SECURITY;
CREATE POLICY deployment_target_activity_runtime_tenant ON cloud_agents.deployment_target_activity
    TO cloud_agents_runtime USING (tenant_id = cloud_agents.require_tenant_id())
    WITH CHECK (tenant_id = cloud_agents.require_tenant_id());
CREATE POLICY deployment_target_activity_migration_owner ON cloud_agents.deployment_target_activity
    TO cloud_agents_migration_owner USING (true) WITH CHECK (true);
REVOKE ALL ON TABLE cloud_agents.deployment_target_activity FROM PUBLIC;
GRANT SELECT ON TABLE cloud_agents.deployment_target_activity TO cloud_agents_runtime;

INSERT INTO cloud_agents.deployment_target_activity (
    tenant_id, project_uid, target_uid, event_uid, operation_uid, action, idempotency_key,
    request_id, request_digest, subject_digest, target_generation, state, current_step,
    stable_error_code, impact_summary, retryable, requested_at, occurred_at
)
SELECT target.tenant_id, target.project_uid, target.target_uid,
    'op-' || pg_catalog.md5(target.tenant_id || '|' || target.project_uid || '|' || target.target_uid || '|target.register|' || target.create_idempotency_key) || '-succeeded',
    'op-' || pg_catalog.md5(target.tenant_id || '|' || target.project_uid || '|' || target.target_uid || '|target.register|' || target.create_idempotency_key),
    'target.register', target.create_idempotency_key, 'migration-backfill', target.create_request_digest,
    'sha256:' || pg_catalog.repeat('0', 64), target.generation, 'succeeded', 'complete', '',
    'Register deployment target', false, target.created_at, target.created_at
FROM cloud_agents.deployment_targets AS target;

INSERT INTO cloud_agents.deployment_target_activity (
    tenant_id, project_uid, target_uid, event_uid, operation_uid, action, idempotency_key,
    request_id, request_digest, subject_digest, target_generation, state, current_step,
    stable_error_code, impact_summary, retryable, requested_at, occurred_at
)
SELECT probe.tenant_id, probe.project_uid, probe.target_uid,
    'op-' || pg_catalog.md5(probe.tenant_id || '|' || probe.project_uid || '|' || probe.target_uid || '|target.probe|' || probe.idempotency_key) || '-requested',
    'op-' || pg_catalog.md5(probe.tenant_id || '|' || probe.project_uid || '|' || probe.target_uid || '|target.probe|' || probe.idempotency_key),
    'target.probe', probe.idempotency_key, 'migration-backfill', probe.request_digest,
    'sha256:' || pg_catalog.repeat('0', 64), probe.generation, 'running', 'connectivity', '',
    'Probe deployment target connectivity and capabilities', false, probe.started_at, probe.started_at
FROM cloud_agents.deployment_target_probe_operations AS probe;

INSERT INTO cloud_agents.deployment_target_activity (
    tenant_id, project_uid, target_uid, event_uid, operation_uid, action, idempotency_key,
    request_id, request_digest, subject_digest, target_generation, state, current_step,
    stable_error_code, impact_summary, retryable, requested_at, occurred_at
)
SELECT probe.tenant_id, probe.project_uid, probe.target_uid,
    'op-' || pg_catalog.md5(probe.tenant_id || '|' || probe.project_uid || '|' || probe.target_uid || '|target.probe|' || probe.idempotency_key) || '-' || probe.phase,
    'op-' || pg_catalog.md5(probe.tenant_id || '|' || probe.project_uid || '|' || probe.target_uid || '|target.probe|' || probe.idempotency_key),
    'target.probe', probe.idempotency_key, 'migration-backfill', probe.request_digest,
    'sha256:' || pg_catalog.repeat('0', 64), probe.generation, probe.phase,
    CASE WHEN probe.phase = 'succeeded' THEN 'complete' ELSE 'failed' END,
    probe.stable_error_code, 'Probe deployment target connectivity and capabilities',
    probe.phase = 'failed', probe.started_at, probe.completed_at
FROM cloud_agents.deployment_target_probe_operations AS probe
WHERE probe.phase IN ('succeeded', 'failed');

CREATE FUNCTION cloud_agents.register_deployment_target_v3(
    p_tenant_id text, p_project_uid text, p_target_uid text, p_target_name text,
    p_target_kind text, p_endpoint text, p_credential_ref text,
    p_idempotency_key text, p_request_digest text, p_request_id text, p_subject_digest text
)
RETURNS TABLE (
    target_uid text, target_name text, target_kind text, endpoint text, credential_ref text,
    generation bigint, observed_phase text, api_version text, engine_version text,
    target_os text, target_arch text, stable_error_code text, last_probe_at timestamptz,
    resource_version bigint, created_at timestamptz, updated_at timestamptz
)
LANGUAGE plpgsql SECURITY DEFINER SET search_path = pg_catalog, cloud_agents
AS $cloud_agents_function$
DECLARE
    ignored_principal text;
    mutation_at timestamptz;
    existing cloud_agents.deployment_targets%ROWTYPE;
    operation_uid_value text;
BEGIN
    ignored_principal := cloud_agents.require_runtime_mutation_principal();
    mutation_at := pg_catalog.transaction_timestamp();
    IF p_tenant_id IS DISTINCT FROM cloud_agents.require_tenant_id()
        OR NOT cloud_agents.is_valid_identifier(p_project_uid)
        OR NOT cloud_agents.is_valid_identifier(p_target_uid)
        OR NOT cloud_agents.is_valid_identifier(p_target_name)
        OR p_target_kind NOT IN ('docker', 'kubernetes', 'ssh')
        OR pg_catalog.octet_length(p_endpoint) NOT BETWEEN 7 AND 2048
        OR NOT (
            p_target_kind = 'ssh' AND p_endpoint ~ '^ssh://[^/?#[:space:]@]+/?$'
            OR p_target_kind IN ('docker', 'kubernetes') AND p_endpoint ~ '^https://[^/?#[:space:]@]+/?$'
        )
        OR NOT cloud_agents.is_valid_identifier(p_credential_ref)
        OR p_idempotency_key !~ '^[A-Za-z0-9._~-]{16,128}$'
        OR p_request_digest !~ '^sha256:[0-9a-f]{64}$'
        OR NOT cloud_agents.is_valid_identifier(p_request_id)
        OR p_subject_digest !~ '^sha256:[0-9a-f]{64}$'
    THEN RAISE EXCEPTION USING ERRCODE = '22023', MESSAGE = 'deployment target input is invalid'; END IF;
    SELECT target.* INTO existing FROM cloud_agents.deployment_targets AS target
    WHERE target.tenant_id = p_tenant_id AND target.project_uid = p_project_uid
        AND target.create_idempotency_key = p_idempotency_key FOR UPDATE;
    IF FOUND THEN
        IF existing.create_request_digest IS DISTINCT FROM p_request_digest THEN
            RAISE EXCEPTION USING ERRCODE = '23505', MESSAGE = 'deployment target idempotency conflict';
        END IF;
    ELSE
        PERFORM 1 FROM cloud_agents.projects AS project
        WHERE project.tenant_id = p_tenant_id AND project.project_uid = p_project_uid AND project.state = 'active'
        FOR KEY SHARE;
        IF NOT FOUND THEN RAISE EXCEPTION USING ERRCODE = '23503', MESSAGE = 'project is absent or inactive'; END IF;
        INSERT INTO cloud_agents.deployment_targets (
            tenant_id, tenant_ref_id, project_uid, target_uid, target_name, target_kind, endpoint, credential_ref,
            generation, observed_phase, api_version, engine_version, target_os, target_arch, stable_error_code,
            resource_version, create_idempotency_key, create_request_digest, created_at, updated_at
        ) VALUES (
            p_tenant_id, p_tenant_id, p_project_uid, p_target_uid, p_target_name, p_target_kind, p_endpoint, p_credential_ref,
            1, 'unprobed', '', '', '', '', '', 1, p_idempotency_key, p_request_digest, mutation_at, mutation_at
        ) RETURNING deployment_targets.* INTO existing;
        operation_uid_value := 'op-' || pg_catalog.md5(p_tenant_id || '|' || p_project_uid || '|' || p_target_uid || '|target.register|' || p_idempotency_key);
        INSERT INTO cloud_agents.deployment_target_activity (
            tenant_id, project_uid, target_uid, event_uid, operation_uid, action, idempotency_key,
            request_id, request_digest, subject_digest, target_generation, state, current_step,
            stable_error_code, impact_summary, retryable, requested_at, occurred_at
        ) VALUES (
            p_tenant_id, p_project_uid, p_target_uid, operation_uid_value || '-succeeded', operation_uid_value,
            'target.register', p_idempotency_key, p_request_id, p_request_digest, p_subject_digest,
            existing.generation, 'succeeded', 'complete', '', 'Register deployment target', false,
            mutation_at, mutation_at
        );
    END IF;
    target_uid := existing.target_uid; target_name := existing.target_name; target_kind := existing.target_kind;
    endpoint := existing.endpoint; credential_ref := existing.credential_ref; generation := existing.generation;
    observed_phase := existing.observed_phase; api_version := existing.api_version; engine_version := existing.engine_version;
    target_os := existing.target_os; target_arch := existing.target_arch; stable_error_code := existing.stable_error_code;
    last_probe_at := existing.last_probe_at; resource_version := existing.resource_version;
    created_at := existing.created_at; updated_at := existing.updated_at;
    RETURN NEXT;
END;
$cloud_agents_function$;

CREATE FUNCTION cloud_agents.begin_deployment_target_probe_v2(
    p_tenant_id text, p_project_uid text, p_target_uid text, p_expected_generation bigint,
    p_idempotency_key text, p_request_digest text, p_request_id text, p_subject_digest text
)
RETURNS TABLE (
    target_uid text, target_name text, target_kind text, endpoint text, credential_ref text,
    generation bigint, observed_phase text, api_version text, engine_version text,
    target_os text, target_arch text, stable_error_code text, last_probe_at timestamptz,
    resource_version bigint, created_at timestamptz, updated_at timestamptz, execute_probe boolean
)
LANGUAGE plpgsql SECURITY DEFINER SET search_path = pg_catalog, cloud_agents
AS $cloud_agents_function$
DECLARE
    ignored_principal text;
    mutation_at timestamptz;
    existing cloud_agents.deployment_targets%ROWTYPE;
    operation cloud_agents.deployment_target_probe_operations%ROWTYPE;
    operation_uid_value text;
BEGIN
    ignored_principal := cloud_agents.require_runtime_mutation_principal();
    mutation_at := pg_catalog.transaction_timestamp();
    IF p_tenant_id IS DISTINCT FROM cloud_agents.require_tenant_id()
        OR NOT cloud_agents.is_valid_identifier(p_project_uid)
        OR NOT cloud_agents.is_valid_identifier(p_target_uid)
        OR p_expected_generation < 1 OR p_idempotency_key !~ '^[A-Za-z0-9._~-]{16,128}$'
        OR p_request_digest !~ '^sha256:[0-9a-f]{64}$'
        OR NOT cloud_agents.is_valid_identifier(p_request_id)
        OR p_subject_digest !~ '^sha256:[0-9a-f]{64}$'
    THEN RAISE EXCEPTION USING ERRCODE = '22023', MESSAGE = 'deployment target probe input is invalid'; END IF;
    SELECT target.* INTO existing FROM cloud_agents.deployment_targets AS target
    WHERE target.tenant_id = p_tenant_id AND target.project_uid = p_project_uid AND target.target_uid = p_target_uid FOR UPDATE;
    IF NOT FOUND THEN RETURN; END IF;
    IF existing.generation <> p_expected_generation THEN
        RAISE EXCEPTION USING ERRCODE = '23505', MESSAGE = 'deployment target generation conflict';
    END IF;
    SELECT probe.* INTO operation FROM cloud_agents.deployment_target_probe_operations AS probe
    WHERE probe.tenant_id = p_tenant_id AND probe.project_uid = p_project_uid AND probe.target_uid = p_target_uid
        AND probe.idempotency_key = p_idempotency_key;
    IF FOUND THEN
        IF operation.request_digest IS DISTINCT FROM p_request_digest OR operation.generation <> p_expected_generation THEN
            RAISE EXCEPTION USING ERRCODE = '23505', MESSAGE = 'deployment target probe idempotency conflict';
        END IF;
        execute_probe := operation.phase = 'running';
    ELSE
        PERFORM 1 FROM cloud_agents.deployment_target_probe_operations AS probe
        WHERE probe.tenant_id = p_tenant_id AND probe.project_uid = p_project_uid AND probe.target_uid = p_target_uid
            AND probe.phase = 'running';
        IF FOUND THEN RAISE EXCEPTION USING ERRCODE = '23505', MESSAGE = 'deployment target probe is already running'; END IF;
        INSERT INTO cloud_agents.deployment_target_probe_operations (
            tenant_id, project_uid, target_uid, idempotency_key, request_digest, generation, phase,
            api_version, engine_version, target_os, target_arch, stable_error_code, started_at
        ) VALUES (p_tenant_id, p_project_uid, p_target_uid, p_idempotency_key, p_request_digest, p_expected_generation,
            'running', '', '', '', '', '', mutation_at);
        UPDATE cloud_agents.deployment_targets AS target SET observed_phase = 'probing', api_version = '', engine_version = '',
            target_os = '', target_arch = '', stable_error_code = '', resource_version = existing.resource_version + 1,
            updated_at = mutation_at
        WHERE target.tenant_id = p_tenant_id AND target.project_uid = p_project_uid AND target.target_uid = p_target_uid
        RETURNING target.* INTO existing;
        operation_uid_value := 'op-' || pg_catalog.md5(p_tenant_id || '|' || p_project_uid || '|' || p_target_uid || '|target.probe|' || p_idempotency_key);
        INSERT INTO cloud_agents.deployment_target_activity (
            tenant_id, project_uid, target_uid, event_uid, operation_uid, action, idempotency_key,
            request_id, request_digest, subject_digest, target_generation, state, current_step,
            stable_error_code, impact_summary, retryable, requested_at, occurred_at
        ) VALUES (
            p_tenant_id, p_project_uid, p_target_uid, operation_uid_value || '-requested', operation_uid_value,
            'target.probe', p_idempotency_key, p_request_id, p_request_digest, p_subject_digest,
            p_expected_generation, 'running', 'connectivity', '',
            'Probe deployment target connectivity and capabilities', false, mutation_at, mutation_at
        );
        execute_probe := true;
    END IF;
    target_uid := existing.target_uid; target_name := existing.target_name; target_kind := existing.target_kind;
    endpoint := existing.endpoint; credential_ref := existing.credential_ref; generation := existing.generation;
    observed_phase := existing.observed_phase; api_version := existing.api_version; engine_version := existing.engine_version;
    target_os := existing.target_os; target_arch := existing.target_arch; stable_error_code := existing.stable_error_code;
    last_probe_at := existing.last_probe_at; resource_version := existing.resource_version;
    created_at := existing.created_at; updated_at := existing.updated_at;
    RETURN NEXT;
END;
$cloud_agents_function$;

CREATE FUNCTION cloud_agents.complete_deployment_target_probe_v2(
    p_tenant_id text, p_project_uid text, p_target_uid text, p_expected_generation bigint,
    p_idempotency_key text, p_request_digest text, p_succeeded boolean,
    p_api_version text, p_engine_version text, p_target_os text, p_target_arch text, p_stable_error_code text
)
RETURNS TABLE (
    target_uid text, target_name text, target_kind text, endpoint text, credential_ref text,
    generation bigint, observed_phase text, api_version text, engine_version text,
    target_os text, target_arch text, stable_error_code text, last_probe_at timestamptz,
    resource_version bigint, created_at timestamptz, updated_at timestamptz
)
LANGUAGE plpgsql SECURITY DEFINER SET search_path = pg_catalog, cloud_agents
AS $cloud_agents_function$
DECLARE
    ignored_principal text;
    mutation_at timestamptz;
    existing cloud_agents.deployment_targets%ROWTYPE;
    operation cloud_agents.deployment_target_probe_operations%ROWTYPE;
    activity cloud_agents.deployment_target_activity%ROWTYPE;
BEGIN
    ignored_principal := cloud_agents.require_runtime_mutation_principal();
    mutation_at := pg_catalog.transaction_timestamp();
    IF p_tenant_id IS DISTINCT FROM cloud_agents.require_tenant_id()
        OR NOT cloud_agents.is_valid_identifier(p_project_uid) OR NOT cloud_agents.is_valid_identifier(p_target_uid)
        OR p_expected_generation < 1 OR p_idempotency_key !~ '^[A-Za-z0-9._~-]{16,128}$'
        OR p_request_digest !~ '^sha256:[0-9a-f]{64}$'
        OR (p_succeeded AND (p_api_version = '' OR p_engine_version = '' OR p_target_os = '' OR p_target_arch = '' OR p_stable_error_code <> ''))
        OR (NOT p_succeeded AND (p_api_version <> '' OR p_engine_version <> '' OR p_target_os <> '' OR p_target_arch <> '' OR NOT cloud_agents.is_valid_identifier(p_stable_error_code)))
        OR pg_catalog.octet_length(p_api_version) > 128 OR pg_catalog.octet_length(p_engine_version) > 128
        OR pg_catalog.octet_length(p_target_os) > 128 OR pg_catalog.octet_length(p_target_arch) > 128
    THEN RAISE EXCEPTION USING ERRCODE = '22023', MESSAGE = 'deployment target probe completion is invalid'; END IF;
    SELECT target.* INTO existing FROM cloud_agents.deployment_targets AS target
    WHERE target.tenant_id = p_tenant_id AND target.project_uid = p_project_uid AND target.target_uid = p_target_uid FOR UPDATE;
    IF NOT FOUND THEN RETURN; END IF;
    IF existing.generation <> p_expected_generation THEN
        RAISE EXCEPTION USING ERRCODE = '23505', MESSAGE = 'deployment target generation conflict';
    END IF;
    SELECT probe.* INTO operation FROM cloud_agents.deployment_target_probe_operations AS probe
    WHERE probe.tenant_id = p_tenant_id AND probe.project_uid = p_project_uid AND probe.target_uid = p_target_uid
        AND probe.idempotency_key = p_idempotency_key FOR UPDATE;
    IF NOT FOUND THEN RETURN; END IF;
    IF operation.request_digest IS DISTINCT FROM p_request_digest OR operation.generation <> p_expected_generation THEN
        RAISE EXCEPTION USING ERRCODE = '23505', MESSAGE = 'deployment target probe idempotency conflict';
    END IF;
    IF operation.phase = 'running' THEN
        SELECT event.* INTO STRICT activity FROM cloud_agents.deployment_target_activity AS event
        WHERE event.tenant_id = p_tenant_id AND event.project_uid = p_project_uid AND event.target_uid = p_target_uid
            AND event.action = 'target.probe' AND event.idempotency_key = p_idempotency_key AND event.state = 'running'
        FOR KEY SHARE;
        UPDATE cloud_agents.deployment_target_probe_operations AS probe SET
            phase = CASE WHEN p_succeeded THEN 'succeeded' ELSE 'failed' END,
            api_version = p_api_version, engine_version = p_engine_version, target_os = p_target_os,
            target_arch = p_target_arch, stable_error_code = p_stable_error_code, completed_at = mutation_at
        WHERE probe.tenant_id = p_tenant_id AND probe.project_uid = p_project_uid AND probe.target_uid = p_target_uid
            AND probe.idempotency_key = p_idempotency_key;
        UPDATE cloud_agents.deployment_targets AS target SET
            observed_phase = CASE WHEN p_succeeded THEN 'ready' ELSE 'unavailable' END,
            api_version = p_api_version, engine_version = p_engine_version, target_os = p_target_os,
            target_arch = p_target_arch, stable_error_code = p_stable_error_code, last_probe_at = mutation_at,
            resource_version = existing.resource_version + 1, updated_at = mutation_at
        WHERE target.tenant_id = p_tenant_id AND target.project_uid = p_project_uid AND target.target_uid = p_target_uid
        RETURNING target.* INTO existing;
        INSERT INTO cloud_agents.deployment_target_activity (
            tenant_id, project_uid, target_uid, event_uid, operation_uid, action, idempotency_key,
            request_id, request_digest, subject_digest, target_generation, state, current_step,
            stable_error_code, impact_summary, retryable, requested_at, occurred_at
        ) VALUES (
            p_tenant_id, p_project_uid, p_target_uid,
            activity.operation_uid || CASE WHEN p_succeeded THEN '-succeeded' ELSE '-failed' END,
            activity.operation_uid, activity.action, activity.idempotency_key, activity.request_id,
            activity.request_digest, activity.subject_digest, activity.target_generation,
            CASE WHEN p_succeeded THEN 'succeeded' ELSE 'failed' END,
            CASE WHEN p_succeeded THEN 'complete' ELSE 'failed' END,
            p_stable_error_code, activity.impact_summary, NOT p_succeeded, activity.requested_at, mutation_at
        );
    END IF;
    target_uid := existing.target_uid; target_name := existing.target_name; target_kind := existing.target_kind;
    endpoint := existing.endpoint; credential_ref := existing.credential_ref; generation := existing.generation;
    observed_phase := existing.observed_phase; api_version := existing.api_version; engine_version := existing.engine_version;
    target_os := existing.target_os; target_arch := existing.target_arch; stable_error_code := existing.stable_error_code;
    last_probe_at := existing.last_probe_at; resource_version := existing.resource_version;
    created_at := existing.created_at; updated_at := existing.updated_at;
    RETURN NEXT;
END;
$cloud_agents_function$;

ALTER FUNCTION cloud_agents.register_deployment_target_v3(text, text, text, text, text, text, text, text, text, text, text)
    OWNER TO cloud_agents_migration_owner;
ALTER FUNCTION cloud_agents.begin_deployment_target_probe_v2(text, text, text, bigint, text, text, text, text)
    OWNER TO cloud_agents_migration_owner;
ALTER FUNCTION cloud_agents.complete_deployment_target_probe_v2(text, text, text, bigint, text, text, boolean, text, text, text, text, text)
    OWNER TO cloud_agents_migration_owner;
REVOKE ALL ON FUNCTION cloud_agents.register_deployment_target_v3(text, text, text, text, text, text, text, text, text, text, text) FROM PUBLIC;
REVOKE ALL ON FUNCTION cloud_agents.begin_deployment_target_probe_v2(text, text, text, bigint, text, text, text, text) FROM PUBLIC;
REVOKE ALL ON FUNCTION cloud_agents.complete_deployment_target_probe_v2(text, text, text, bigint, text, text, boolean, text, text, text, text, text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION cloud_agents.register_deployment_target_v3(text, text, text, text, text, text, text, text, text, text, text) TO cloud_agents_runtime;
GRANT EXECUTE ON FUNCTION cloud_agents.begin_deployment_target_probe_v2(text, text, text, bigint, text, text, text, text) TO cloud_agents_runtime;
GRANT EXECUTE ON FUNCTION cloud_agents.complete_deployment_target_probe_v2(text, text, text, bigint, text, text, boolean, text, text, text, text, text) TO cloud_agents_runtime;
