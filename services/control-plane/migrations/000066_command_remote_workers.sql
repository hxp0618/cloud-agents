ALTER TABLE cloud_agents.admin_denied_writes DROP CONSTRAINT admin_denied_writes_action_check;
ALTER TABLE cloud_agents.admin_denied_writes ADD CONSTRAINT admin_denied_writes_action_check CHECK (action IN (
    'adminUpgradeEnvironmentLease', 'adminRollbackEnvironmentLease', 'adminRegisterWorkerRelease',
    'adminSetStoragePolicy', 'adminSetNetworkPolicy', 'adminSetProjectLeaseQuota',
    'adminCreateEnvironmentProfile', 'adminPublishEnvironmentProfile', 'adminDisableEnvironmentProfile',
    'adminCreateRuntimeProfile', 'adminPublishRuntimeProfile', 'adminDisableRuntimeProfile',
    'adminRegisterDeploymentTarget', 'adminProbeDeploymentTarget',
    'adminTransitionDeploymentTargetScheduling', 'adminCleanupDeploymentTarget',
    'adminStopSandboxSession', 'adminRebuildSandboxSession', 'adminRevokeSandboxAccessGrant',
    'adminCreateRemoteWorkerEnrollment', 'adminRevokeRemoteWorkerEnrollment',
    'adminTransitionRemoteWorkerScheduling'
));

ALTER TABLE cloud_agents.remote_worker_enrollments
    ADD COLUMN node_command_uid text CHECK (node_command_uid IS NULL OR cloud_agents.is_valid_identifier(node_command_uid)),
    ADD COLUMN node_command_operation_uid text CHECK (node_command_operation_uid IS NULL OR cloud_agents.is_valid_identifier(node_command_operation_uid)),
    ADD COLUMN node_command_deadline_at timestamptz,
    ADD CONSTRAINT remote_worker_enrollments_command_check CHECK (
        (node_command_uid IS NULL AND node_command_operation_uid IS NULL AND node_command_deadline_at IS NULL)
        OR (node_command_uid IS NOT NULL AND node_command_operation_uid IS NOT NULL
            AND node_command_deadline_at IS NOT NULL AND node_generation >= 2)
    );

CREATE TABLE cloud_agents.remote_worker_node_activity (
    tenant_id text NOT NULL,
    project_uid text NOT NULL,
    enrollment_uid text NOT NULL,
    event_uid text NOT NULL CHECK (cloud_agents.is_valid_identifier(event_uid)),
    operation_uid text NOT NULL CHECK (cloud_agents.is_valid_identifier(operation_uid)),
    command_uid text NOT NULL CHECK (cloud_agents.is_valid_identifier(command_uid)),
    action text NOT NULL CHECK (action IN ('remote-worker.drain', 'remote-worker.resume')),
    idempotency_key text NOT NULL CHECK (idempotency_key ~ '^[A-Za-z0-9._~-]{16,128}$'),
    request_id text NOT NULL CHECK (cloud_agents.is_valid_identifier(request_id)),
    request_digest text NOT NULL CHECK (request_digest ~ '^sha256:[0-9a-f]{64}$'),
    subject_digest text NOT NULL CHECK (subject_digest ~ '^sha256:[0-9a-f]{64}$'),
    node_generation bigint NOT NULL CHECK (node_generation >= 2),
    node_resource_version bigint NOT NULL CHECK (node_resource_version >= 2),
    state text NOT NULL CHECK (state IN ('queued', 'running', 'succeeded', 'failed')),
    current_step text NOT NULL CHECK (cloud_agents.is_valid_identifier(current_step)),
    stable_error_code text CHECK (stable_error_code IS NULL OR cloud_agents.is_valid_identifier(stable_error_code)),
    impact_summary text NOT NULL CHECK (char_length(impact_summary) BETWEEN 1 AND 256 AND impact_summary !~ '[[:cntrl:]]'),
    retryable boolean NOT NULL,
    receipt_digest text CHECK (receipt_digest IS NULL OR receipt_digest ~ '^sha256:[0-9a-f]{64}$'),
    command_deadline_at timestamptz NOT NULL,
    requested_at timestamptz NOT NULL,
    result text NOT NULL CHECK (result IN ('requested', 'succeeded', 'failed')),
    occurred_at timestamptz NOT NULL DEFAULT transaction_timestamp(),
    PRIMARY KEY (tenant_id, project_uid, enrollment_uid, event_uid),
    FOREIGN KEY (tenant_id, project_uid, enrollment_uid) REFERENCES cloud_agents.remote_worker_enrollments
        ON UPDATE RESTRICT ON DELETE RESTRICT,
    CHECK (occurred_at >= requested_at AND command_deadline_at = requested_at + interval '30 seconds'),
    CHECK ((state = 'failed') = (stable_error_code IS NOT NULL AND retryable)),
    CHECK ((result = 'failed') = (stable_error_code IS NOT NULL)),
    CHECK ((state IN ('succeeded', 'failed')) = (receipt_digest IS NOT NULL OR stable_error_code = 'remote-worker-command-expired'))
);

CREATE INDEX remote_worker_node_activity_page_idx ON cloud_agents.remote_worker_node_activity
    (tenant_id, project_uid, enrollment_uid, requested_at DESC, operation_uid DESC, occurred_at DESC);
CREATE INDEX remote_worker_node_activity_idempotency_idx ON cloud_agents.remote_worker_node_activity
    (tenant_id, project_uid, subject_digest, action, idempotency_key, occurred_at DESC);

ALTER TABLE cloud_agents.remote_worker_node_activity OWNER TO cloud_agents_migration_owner;
ALTER TABLE cloud_agents.remote_worker_node_activity ENABLE ROW LEVEL SECURITY;
ALTER TABLE cloud_agents.remote_worker_node_activity FORCE ROW LEVEL SECURITY;
CREATE POLICY remote_worker_node_activity_runtime ON cloud_agents.remote_worker_node_activity TO cloud_agents_runtime
    USING (tenant_id = cloud_agents.require_tenant_id()) WITH CHECK (tenant_id = cloud_agents.require_tenant_id());
CREATE POLICY remote_worker_node_activity_owner ON cloud_agents.remote_worker_node_activity TO cloud_agents_migration_owner
    USING (true) WITH CHECK (true);
REVOKE ALL ON TABLE cloud_agents.remote_worker_node_activity FROM PUBLIC;
GRANT SELECT ON TABLE cloud_agents.remote_worker_node_activity TO cloud_agents_runtime;

CREATE OR REPLACE FUNCTION cloud_agents.reset_remote_worker_node_status_on_incarnation_change_v1()
RETURNS trigger
LANGUAGE plpgsql SET search_path = pg_catalog, cloud_agents
AS $$
BEGIN
    IF OLD.incarnation_uid IS DISTINCT FROM NEW.incarnation_uid THEN
        NEW.node_resource_version := NULL;
        NEW.node_generation := NULL;
        NEW.node_observed_generation := NULL;
        NEW.node_desired_state := NULL;
        NEW.node_observed_state := NULL;
        NEW.node_worker_version := NULL;
        NEW.node_os := NULL;
        NEW.node_architecture := NULL;
        NEW.node_kernel_version := NULL;
        NEW.node_capabilities := NULL;
        NEW.node_capacity_cpu_millis := NULL;
        NEW.node_capacity_memory_bytes := NULL;
        NEW.node_capacity_disk_bytes := NULL;
        NEW.node_first_connected_at := NULL;
        NEW.node_last_heartbeat_at := NULL;
        NEW.node_heartbeat_expires_at := NULL;
        NEW.node_command_uid := NULL;
        NEW.node_command_operation_uid := NULL;
        NEW.node_command_deadline_at := NULL;
    END IF;
    RETURN NEW;
END;
$$;

CREATE FUNCTION cloud_agents.lock_remote_worker_scheduling_v1(
    p_tenant text, p_project text, p_enrollment text
)
RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path = pg_catalog, cloud_agents
AS $$
DECLARE ignored text;
BEGIN
    ignored := cloud_agents.require_runtime_mutation_principal();
    IF p_tenant IS DISTINCT FROM cloud_agents.require_tenant_id()
       OR NOT cloud_agents.is_valid_identifier(p_project) OR NOT cloud_agents.is_valid_identifier(p_enrollment) THEN
        RAISE EXCEPTION USING ERRCODE = '22023', MESSAGE = 'remote worker scheduling lock input is invalid';
    END IF;
    PERFORM 1 FROM cloud_agents.remote_worker_enrollments AS enrollment
    WHERE enrollment.tenant_id = p_tenant AND enrollment.project_uid = p_project
      AND enrollment.enrollment_uid = p_enrollment
    FOR UPDATE;
    RETURN FOUND;
END;
$$;

CREATE FUNCTION cloud_agents.transition_remote_worker_scheduling_v1(
    p_tenant text, p_project text, p_enrollment text,
    p_expected_generation bigint, p_expected_resource_version bigint,
    p_desired_state text, p_impact_digest text, p_current_impact_digest text,
    p_key text, p_request_digest text, p_request_id text, p_subject_digest text,
    p_impact_summary text
)
RETURNS TABLE (
    operation_uid text, idempotency_key text, action text, enrollment_uid text,
    command_uid text, node_generation bigint, subject_digest text, request_id text,
    requested_at timestamptz, updated_at timestamptz, state text, current_step text,
    stable_error_code text, impact_summary text, retryable boolean, command_deadline_at timestamptz
)
LANGUAGE plpgsql SECURITY DEFINER SET search_path = pg_catalog, cloud_agents
AS $$
DECLARE
    ignored text;
    existing cloud_agents.remote_worker_enrollments%ROWTYPE;
    activity cloud_agents.remote_worker_node_activity%ROWTYPE;
    action_value text;
    operation_value text;
    command_value text;
    mutation_at timestamptz := transaction_timestamp();
BEGIN
    ignored := cloud_agents.require_runtime_mutation_principal();
    IF p_tenant IS DISTINCT FROM cloud_agents.require_tenant_id()
       OR NOT cloud_agents.is_valid_identifier(p_project) OR NOT cloud_agents.is_valid_identifier(p_enrollment)
       OR p_expected_generation < 1 OR p_expected_resource_version < 1
       OR p_desired_state NOT IN ('active', 'drained')
       OR p_impact_digest !~ '^sha256:[0-9a-f]{64}$' OR p_current_impact_digest !~ '^sha256:[0-9a-f]{64}$'
       OR p_key !~ '^[A-Za-z0-9._~-]{16,128}$' OR p_request_digest !~ '^sha256:[0-9a-f]{64}$'
       OR NOT cloud_agents.is_valid_identifier(p_request_id) OR p_subject_digest !~ '^sha256:[0-9a-f]{64}$'
       OR char_length(p_impact_summary) NOT BETWEEN 1 AND 256 OR p_impact_summary ~ '[[:cntrl:]]' THEN
        RAISE EXCEPTION USING ERRCODE = '22023', MESSAGE = 'remote worker scheduling input is invalid';
    END IF;

    SELECT enrollment.* INTO existing FROM cloud_agents.remote_worker_enrollments AS enrollment
    WHERE enrollment.tenant_id = p_tenant AND enrollment.project_uid = p_project
      AND enrollment.enrollment_uid = p_enrollment FOR UPDATE;
    IF NOT FOUND THEN RETURN; END IF;
    IF existing.state <> 'enrolled' OR existing.certificate_state <> 'active' OR existing.node_generation IS NULL THEN
        RAISE EXCEPTION USING ERRCODE = '23505', MESSAGE = 'remote worker node is unavailable';
    END IF;
    action_value := CASE WHEN p_desired_state = 'drained' THEN 'remote-worker.drain' ELSE 'remote-worker.resume' END;

    SELECT event.* INTO activity FROM cloud_agents.remote_worker_node_activity AS event
    WHERE event.tenant_id = p_tenant AND event.project_uid = p_project
      AND event.enrollment_uid = p_enrollment AND event.subject_digest = p_subject_digest
      AND event.action = action_value AND event.idempotency_key = p_key
    ORDER BY event.occurred_at DESC, event.event_uid DESC LIMIT 1;
    IF FOUND THEN
        IF activity.request_digest IS DISTINCT FROM p_request_digest
           OR activity.node_generation <> p_expected_generation + 1 THEN
            RAISE EXCEPTION USING ERRCODE = '23505', MESSAGE = 'remote worker scheduling idempotency conflict';
        END IF;
    ELSE
        SELECT event.* INTO activity FROM (
            SELECT DISTINCT ON (source.operation_uid) source.*
            FROM cloud_agents.remote_worker_node_activity AS source
            WHERE source.tenant_id = p_tenant AND source.project_uid = p_project
              AND source.enrollment_uid = p_enrollment
            ORDER BY source.operation_uid, source.occurred_at DESC, source.event_uid DESC
        ) AS event
        WHERE event.state IN ('queued', 'running')
        ORDER BY event.occurred_at DESC, event.event_uid DESC LIMIT 1;
        IF FOUND THEN
            RAISE EXCEPTION USING ERRCODE = '23505', MESSAGE = 'remote worker operation is in progress';
        END IF;
        IF existing.node_generation <> p_expected_generation THEN
            RAISE EXCEPTION USING ERRCODE = '23505', MESSAGE = 'remote worker generation conflict';
        END IF;
        IF existing.node_resource_version <> p_expected_resource_version THEN
            RAISE EXCEPTION USING ERRCODE = '23505', MESSAGE = 'remote worker resource version conflict';
        END IF;
        IF existing.node_desired_state = p_desired_state AND existing.node_observed_state = p_desired_state THEN
            RAISE EXCEPTION USING ERRCODE = '23505', MESSAGE = 'remote worker scheduling state conflict';
        END IF;
        IF p_impact_digest IS DISTINCT FROM p_current_impact_digest THEN
            RAISE EXCEPTION USING ERRCODE = '23505', MESSAGE = 'remote worker scheduling impact conflict';
        END IF;

        operation_value := 'op-' || md5(p_tenant || '|' || p_project || '|' || p_enrollment || '|' || action_value || '|' || p_key);
        command_value := 'cmd-' || md5(operation_value || '|' || (existing.node_generation + 1)::text);
        UPDATE cloud_agents.remote_worker_enrollments AS enrollment
        SET node_resource_version = enrollment.node_resource_version + 1,
            node_generation = enrollment.node_generation + 1,
            node_desired_state = p_desired_state,
            node_command_uid = command_value,
            node_command_operation_uid = operation_value,
            node_command_deadline_at = mutation_at + interval '30 seconds'
        WHERE enrollment.tenant_id = p_tenant AND enrollment.project_uid = p_project
          AND enrollment.enrollment_uid = p_enrollment
        RETURNING enrollment.* INTO existing;

        INSERT INTO cloud_agents.remote_worker_node_activity (
            tenant_id, project_uid, enrollment_uid, event_uid, operation_uid, command_uid, action,
            idempotency_key, request_id, request_digest, subject_digest, node_generation,
            node_resource_version, state, current_step, stable_error_code, impact_summary,
            retryable, receipt_digest, command_deadline_at, requested_at, result, occurred_at
        ) VALUES (
            p_tenant, p_project, p_enrollment, operation_value || '-queued', operation_value, command_value,
            action_value, p_key, p_request_id, p_request_digest, p_subject_digest,
            existing.node_generation, existing.node_resource_version, 'queued', 'pending-node', NULL,
            p_impact_summary, false, NULL, existing.node_command_deadline_at, mutation_at, 'requested', mutation_at
        ) RETURNING * INTO activity;
    END IF;

    operation_uid := activity.operation_uid; idempotency_key := activity.idempotency_key;
    action := activity.action; enrollment_uid := activity.enrollment_uid; command_uid := activity.command_uid;
    node_generation := activity.node_generation; subject_digest := activity.subject_digest;
    request_id := activity.request_id; requested_at := activity.requested_at; updated_at := activity.occurred_at;
    state := activity.state; current_step := activity.current_step;
    stable_error_code := COALESCE(activity.stable_error_code, ''); impact_summary := activity.impact_summary;
    retryable := activity.retryable; command_deadline_at := activity.command_deadline_at;
    RETURN NEXT;
END;
$$;

CREATE FUNCTION cloud_agents.heartbeat_remote_worker_v2(
    p_tenant text, p_project text, p_enrollment text, p_peer_certificate_digest text,
    p_incarnation text, p_observed_generation bigint, p_observed_state text, p_worker_version text,
    p_os text, p_architecture text, p_kernel_version text, p_capabilities text[],
    p_capacity_cpu_millis bigint, p_capacity_memory_bytes bigint, p_capacity_disk_bytes bigint,
    p_receipt_command text, p_receipt_generation bigint, p_receipt_result text,
    p_receipt_error text, p_receipt_digest text
)
RETURNS TABLE (
    enrollment_uid text, worker_uid text, worker_name text, incarnation_uid text,
    node_resource_version bigint, node_generation bigint, node_observed_generation bigint,
    node_desired_state text, node_observed_state text, node_health_state text,
    node_worker_version text, node_os text, node_architecture text, node_kernel_version text,
    node_capabilities text[], node_capacity_cpu_millis bigint, node_capacity_memory_bytes bigint,
    node_capacity_disk_bytes bigint, node_first_connected_at timestamptz,
    node_last_heartbeat_at timestamptz, node_heartbeat_expires_at timestamptz,
    reconcile_required boolean, command_uid text, command_generation bigint,
    command_desired_state text, command_deadline_at timestamptz
)
LANGUAGE plpgsql SECURITY DEFINER SET search_path = pg_catalog, cloud_agents
AS $$
DECLARE
    ignored text;
    existing cloud_agents.remote_worker_enrollments%ROWTYPE;
    activity cloud_agents.remote_worker_node_activity%ROWTYPE;
    mutation_at timestamptz := transaction_timestamp();
BEGIN
    ignored := cloud_agents.require_tenant_id();
    IF p_tenant IS DISTINCT FROM ignored OR NOT cloud_agents.is_valid_identifier(p_project)
       OR NOT cloud_agents.is_valid_identifier(p_enrollment)
       OR p_peer_certificate_digest !~ '^sha256:[0-9a-f]{64}$'
       OR NOT cloud_agents.is_valid_identifier(p_incarnation) OR p_observed_generation < 1
       OR p_observed_state NOT IN ('active', 'drained')
       OR p_worker_version !~ '^[A-Za-z0-9][A-Za-z0-9._+-]{0,63}$'
       OR NOT cloud_agents.is_valid_identifier(p_os) OR NOT cloud_agents.is_valid_identifier(p_architecture)
       OR char_length(p_kernel_version) NOT BETWEEN 1 AND 128 OR p_kernel_version !~ '^[ -~]+$'
       OR p_capabilities IS NULL OR NOT cloud_agents.is_valid_remote_worker_capabilities(p_capabilities)
       OR p_capacity_cpu_millis NOT BETWEEN 100 AND 512000000
       OR p_capacity_memory_bytes NOT BETWEEN 134217728 AND 8796093022208000
       OR p_capacity_disk_bytes NOT BETWEEN 134217728 AND 8796093022208000
       OR (p_receipt_command IS NULL) <> (p_receipt_generation IS NULL)
       OR (p_receipt_command IS NULL) <> (p_receipt_result IS NULL)
       OR (p_receipt_command IS NULL) <> (p_receipt_digest IS NULL)
       OR (p_receipt_command IS NOT NULL AND (
            NOT cloud_agents.is_valid_identifier(p_receipt_command) OR p_receipt_generation < 2
            OR p_receipt_result NOT IN ('succeeded', 'failed')
            OR p_receipt_digest !~ '^sha256:[0-9a-f]{64}$'
            OR (p_receipt_result = 'succeeded' AND p_receipt_error IS NOT NULL)
            OR (p_receipt_result = 'failed' AND NOT cloud_agents.is_valid_identifier(p_receipt_error)))) THEN
        RAISE EXCEPTION USING ERRCODE = '22023', MESSAGE = 'remote worker heartbeat input is invalid';
    END IF;

    SELECT enrollment.* INTO existing FROM cloud_agents.remote_worker_enrollments AS enrollment
    WHERE enrollment.tenant_id = p_tenant AND enrollment.project_uid = p_project
      AND enrollment.enrollment_uid = p_enrollment FOR UPDATE;
    IF NOT FOUND OR existing.state <> 'enrolled' OR existing.certificate_state <> 'active'
       OR existing.certificate_not_after <= clock_timestamp()
       OR existing.certificate_sha256 <> p_peer_certificate_digest
       OR existing.incarnation_uid <> p_incarnation THEN
        RAISE EXCEPTION USING ERRCODE = '28000', MESSAGE = 'remote worker certificate authentication failed';
    END IF;
    IF existing.node_generation IS NULL THEN
        IF p_observed_generation <> 1 OR p_observed_state <> 'active' OR p_receipt_command IS NOT NULL THEN
            RAISE EXCEPTION USING ERRCODE = '23505', MESSAGE = 'remote worker generation conflict';
        END IF;
        existing.node_resource_version := 1;
        existing.node_generation := 1;
        existing.node_observed_generation := 1;
        existing.node_desired_state := 'active';
        existing.node_first_connected_at := mutation_at;
    ELSIF p_observed_generation < existing.node_observed_generation
       OR p_observed_generation > existing.node_generation THEN
        RAISE EXCEPTION USING ERRCODE = '23505', MESSAGE = 'remote worker generation conflict';
    END IF;

    IF existing.node_command_uid IS NOT NULL THEN
        SELECT event.* INTO activity FROM cloud_agents.remote_worker_node_activity AS event
        WHERE event.tenant_id = p_tenant AND event.project_uid = p_project
          AND event.enrollment_uid = p_enrollment
          AND event.operation_uid = existing.node_command_operation_uid
        ORDER BY event.occurred_at DESC, event.event_uid DESC LIMIT 1;
        IF NOT FOUND THEN
            RAISE EXCEPTION USING ERRCODE = 'XX001', MESSAGE = 'remote worker command activity is missing';
        END IF;
    END IF;

    IF p_receipt_command IS NOT NULL THEN
        IF existing.node_command_uid IS DISTINCT FROM p_receipt_command
           OR existing.node_generation IS DISTINCT FROM p_receipt_generation
           OR (activity.command_deadline_at <= clock_timestamp() AND
               (p_receipt_result <> 'failed' OR p_receipt_error IS DISTINCT FROM 'remote-worker-command-expired'))
           OR (p_receipt_result = 'succeeded' AND
               (p_observed_generation <> existing.node_generation OR p_observed_state <> existing.node_desired_state))
           OR (p_receipt_result = 'failed' AND p_observed_generation <> existing.node_observed_generation) THEN
            RAISE EXCEPTION USING ERRCODE = '23505', MESSAGE = 'remote worker command receipt conflict';
        END IF;
        IF activity.state IN ('succeeded', 'failed') THEN
            IF activity.receipt_digest IS DISTINCT FROM p_receipt_digest THEN
                RAISE EXCEPTION USING ERRCODE = '23505', MESSAGE = 'remote worker command receipt conflict';
            END IF;
        ELSE
            INSERT INTO cloud_agents.remote_worker_node_activity (
                tenant_id, project_uid, enrollment_uid, event_uid, operation_uid, command_uid, action,
                idempotency_key, request_id, request_digest, subject_digest, node_generation,
                node_resource_version, state, current_step, stable_error_code, impact_summary,
                retryable, receipt_digest, command_deadline_at, requested_at, result, occurred_at
            ) VALUES (
                activity.tenant_id, activity.project_uid, activity.enrollment_uid,
                activity.operation_uid || CASE WHEN p_receipt_result = 'succeeded' THEN '-succeeded' ELSE '-failed' END,
                activity.operation_uid, activity.command_uid, activity.action, activity.idempotency_key,
                activity.request_id, activity.request_digest, activity.subject_digest, activity.node_generation,
                activity.node_resource_version, p_receipt_result,
                CASE WHEN p_receipt_result = 'succeeded' THEN 'complete' ELSE 'failed' END,
                p_receipt_error, activity.impact_summary, p_receipt_result = 'failed', p_receipt_digest,
                activity.command_deadline_at, activity.requested_at, p_receipt_result, mutation_at
            ) RETURNING * INTO activity;
        END IF;
    ELSIF existing.node_command_uid IS NOT NULL AND activity.state IN ('queued', 'running')
          AND activity.command_deadline_at <= clock_timestamp() THEN
        INSERT INTO cloud_agents.remote_worker_node_activity (
            tenant_id, project_uid, enrollment_uid, event_uid, operation_uid, command_uid, action,
            idempotency_key, request_id, request_digest, subject_digest, node_generation,
            node_resource_version, state, current_step, stable_error_code, impact_summary,
            retryable, receipt_digest, command_deadline_at, requested_at, result, occurred_at
        ) VALUES (
            activity.tenant_id, activity.project_uid, activity.enrollment_uid,
            activity.operation_uid || '-expired', activity.operation_uid, activity.command_uid, activity.action,
            activity.idempotency_key, activity.request_id, activity.request_digest, activity.subject_digest,
            activity.node_generation, activity.node_resource_version, 'failed', 'failed',
            'remote-worker-command-expired', activity.impact_summary, true, NULL,
            activity.command_deadline_at, activity.requested_at, 'failed', mutation_at
        ) RETURNING * INTO activity;
    ELSIF existing.node_command_uid IS NOT NULL AND activity.state IN ('queued', 'running')
          AND p_observed_generation = existing.node_generation THEN
        RAISE EXCEPTION USING ERRCODE = '23505', MESSAGE = 'remote worker command receipt conflict';
    END IF;

    existing.node_observed_generation := p_observed_generation;
    existing.node_observed_state := p_observed_state;
    UPDATE cloud_agents.remote_worker_enrollments AS enrollment
    SET node_resource_version = existing.node_resource_version,
        node_generation = existing.node_generation,
        node_observed_generation = existing.node_observed_generation,
        node_desired_state = existing.node_desired_state,
        node_observed_state = existing.node_observed_state,
        node_worker_version = p_worker_version, node_os = p_os,
        node_architecture = p_architecture, node_kernel_version = p_kernel_version,
        node_capabilities = p_capabilities, node_capacity_cpu_millis = p_capacity_cpu_millis,
        node_capacity_memory_bytes = p_capacity_memory_bytes,
        node_capacity_disk_bytes = p_capacity_disk_bytes,
        node_first_connected_at = existing.node_first_connected_at,
        node_last_heartbeat_at = mutation_at,
        node_heartbeat_expires_at = mutation_at + interval '30 seconds'
    WHERE enrollment.tenant_id = p_tenant AND enrollment.project_uid = p_project
      AND enrollment.enrollment_uid = p_enrollment RETURNING enrollment.* INTO existing;

    IF existing.node_command_uid IS NOT NULL AND activity.state = 'queued'
       AND activity.command_deadline_at > clock_timestamp() THEN
        INSERT INTO cloud_agents.remote_worker_node_activity (
            tenant_id, project_uid, enrollment_uid, event_uid, operation_uid, command_uid, action,
            idempotency_key, request_id, request_digest, subject_digest, node_generation,
            node_resource_version, state, current_step, stable_error_code, impact_summary,
            retryable, receipt_digest, command_deadline_at, requested_at, result, occurred_at
        ) VALUES (
            activity.tenant_id, activity.project_uid, activity.enrollment_uid,
            activity.operation_uid || '-running', activity.operation_uid, activity.command_uid, activity.action,
            activity.idempotency_key, activity.request_id, activity.request_digest, activity.subject_digest,
            activity.node_generation, activity.node_resource_version, 'running', 'waiting-receipt', NULL,
            activity.impact_summary, false, NULL, activity.command_deadline_at,
            activity.requested_at, 'requested', mutation_at
        ) RETURNING * INTO activity;
    END IF;

    enrollment_uid := existing.enrollment_uid; worker_uid := existing.worker_uid;
    worker_name := existing.worker_name; incarnation_uid := existing.incarnation_uid;
    node_resource_version := existing.node_resource_version; node_generation := existing.node_generation;
    node_observed_generation := existing.node_observed_generation;
    node_desired_state := existing.node_desired_state; node_observed_state := existing.node_observed_state;
    node_health_state := 'online'; node_worker_version := existing.node_worker_version;
    node_os := existing.node_os; node_architecture := existing.node_architecture;
    node_kernel_version := existing.node_kernel_version; node_capabilities := existing.node_capabilities;
    node_capacity_cpu_millis := existing.node_capacity_cpu_millis;
    node_capacity_memory_bytes := existing.node_capacity_memory_bytes;
    node_capacity_disk_bytes := existing.node_capacity_disk_bytes;
    node_first_connected_at := existing.node_first_connected_at;
    node_last_heartbeat_at := existing.node_last_heartbeat_at;
    node_heartbeat_expires_at := existing.node_heartbeat_expires_at;
    reconcile_required := existing.node_observed_generation <> existing.node_generation
        OR existing.node_observed_state <> existing.node_desired_state;
    IF existing.node_command_uid IS NOT NULL AND activity.state IN ('queued', 'running')
       AND activity.command_deadline_at > clock_timestamp() THEN
        command_uid := existing.node_command_uid;
        command_generation := existing.node_generation;
        command_desired_state := existing.node_desired_state;
        command_deadline_at := existing.node_command_deadline_at;
    ELSE
        command_uid := NULL; command_generation := NULL;
        command_desired_state := NULL; command_deadline_at := NULL;
    END IF;
    RETURN NEXT;
END;
$$;

ALTER FUNCTION cloud_agents.reset_remote_worker_node_status_on_incarnation_change_v1() OWNER TO cloud_agents_migration_owner;
ALTER FUNCTION cloud_agents.lock_remote_worker_scheduling_v1(text,text,text) OWNER TO cloud_agents_migration_owner;
ALTER FUNCTION cloud_agents.transition_remote_worker_scheduling_v1(text,text,text,bigint,bigint,text,text,text,text,text,text,text,text) OWNER TO cloud_agents_migration_owner;
ALTER FUNCTION cloud_agents.heartbeat_remote_worker_v2(text,text,text,text,text,bigint,text,text,text,text,text,text[],bigint,bigint,bigint,text,bigint,text,text,text) OWNER TO cloud_agents_migration_owner;
REVOKE ALL ON FUNCTION cloud_agents.lock_remote_worker_scheduling_v1(text,text,text) FROM PUBLIC;
REVOKE ALL ON FUNCTION cloud_agents.transition_remote_worker_scheduling_v1(text,text,text,bigint,bigint,text,text,text,text,text,text,text,text) FROM PUBLIC;
REVOKE ALL ON FUNCTION cloud_agents.heartbeat_remote_worker_v2(text,text,text,text,text,bigint,text,text,text,text,text,text[],bigint,bigint,bigint,text,bigint,text,text,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION cloud_agents.lock_remote_worker_scheduling_v1(text,text,text) TO cloud_agents_runtime;
GRANT EXECUTE ON FUNCTION cloud_agents.transition_remote_worker_scheduling_v1(text,text,text,bigint,bigint,text,text,text,text,text,text,text,text) TO cloud_agents_runtime;
GRANT EXECUTE ON FUNCTION cloud_agents.heartbeat_remote_worker_v2(text,text,text,text,text,bigint,text,text,text,text,text,text[],bigint,bigint,bigint,text,bigint,text,text,text) TO cloud_agents_runtime;
