ALTER TABLE cloud_agents.admin_denied_writes DROP CONSTRAINT admin_denied_writes_action_check;
ALTER TABLE cloud_agents.admin_denied_writes ADD CONSTRAINT admin_denied_writes_action_check CHECK (action IN (
    'adminUpgradeEnvironmentLease', 'adminRollbackEnvironmentLease', 'adminRegisterWorkerRelease',
    'adminSetStoragePolicy', 'adminSetNetworkPolicy', 'adminSetProjectLeaseQuota',
    'adminCreateEnvironmentProfile', 'adminPublishEnvironmentProfile', 'adminDisableEnvironmentProfile',
    'adminCreateRuntimeProfile', 'adminPublishRuntimeProfile', 'adminDisableRuntimeProfile',
    'adminRegisterDeploymentTarget', 'adminProbeDeploymentTarget',
    'adminTransitionDeploymentTargetScheduling', 'adminCleanupDeploymentTarget',
    'adminStopSandboxSession', 'adminRebuildSandboxSession', 'adminCorrectSandboxUsage',
    'adminRevokeSandboxAccessGrant', 'adminCreateWorkspaceSnapshot', 'adminRestoreWorkspaceSnapshot',
    'adminCleanupWorkspaceSnapshot', 'adminCreateRemoteWorkerEnrollment',
    'adminRevokeRemoteWorkerEnrollment', 'adminTransitionRemoteWorkerScheduling'
));

CREATE TABLE cloud_agents.sandbox_usage_corrections (
    tenant_id text NOT NULL,
    project_uid text NOT NULL,
    sandbox_uid text NOT NULL,
    correction_uid text NOT NULL CHECK (cloud_agents.is_valid_identifier(correction_uid)),
    metric text NOT NULL CHECK (metric IN (
        'allocatedMilliseconds', 'cpuMillisMilliseconds', 'memoryByteMilliseconds',
        'workspaceUsedBytes', 'networkReceivedBytes', 'networkTransmittedBytes'
    )),
    adjustment numeric(40, 0) NOT NULL CHECK (adjustment <> 0),
    reason_code text NOT NULL CHECK (cloud_agents.is_valid_identifier(reason_code)),
    sandbox_generation bigint NOT NULL CHECK (sandbox_generation > 0),
    prior_resource_version bigint NOT NULL CHECK (prior_resource_version > 0),
    subject_digest text NOT NULL CHECK (subject_digest ~ '^sha256:[0-9a-f]{64}$'),
    idempotency_key text NOT NULL CHECK (idempotency_key ~ '^[A-Za-z0-9._~-]{16,128}$'),
    request_digest text NOT NULL CHECK (request_digest ~ '^sha256:[0-9a-f]{64}$'),
    request_id text NOT NULL CHECK (cloud_agents.is_valid_identifier(request_id)),
    created_at timestamptz NOT NULL,
    PRIMARY KEY (tenant_id, project_uid, correction_uid),
    UNIQUE (tenant_id, project_uid, idempotency_key),
    FOREIGN KEY (tenant_id, project_uid, sandbox_uid) REFERENCES cloud_agents.sandbox_sessions
        ON UPDATE RESTRICT ON DELETE RESTRICT
);
CREATE INDEX sandbox_usage_corrections_audit_idx ON cloud_agents.sandbox_usage_corrections
    (tenant_id, project_uid, sandbox_uid, created_at DESC, correction_uid DESC);

ALTER TABLE cloud_agents.sandbox_usage_corrections OWNER TO cloud_agents_migration_owner;
ALTER TABLE cloud_agents.sandbox_usage_corrections ENABLE ROW LEVEL SECURITY;
ALTER TABLE cloud_agents.sandbox_usage_corrections FORCE ROW LEVEL SECURITY;
CREATE POLICY sandbox_usage_corrections_runtime ON cloud_agents.sandbox_usage_corrections TO cloud_agents_runtime
    USING (tenant_id = cloud_agents.require_tenant_id()) WITH CHECK (tenant_id = cloud_agents.require_tenant_id());
CREATE POLICY sandbox_usage_corrections_owner ON cloud_agents.sandbox_usage_corrections TO cloud_agents_migration_owner
    USING (true) WITH CHECK (true);
REVOKE ALL ON TABLE cloud_agents.sandbox_usage_corrections FROM PUBLIC;
GRANT SELECT ON TABLE cloud_agents.sandbox_usage_corrections TO cloud_agents_runtime;

CREATE FUNCTION cloud_agents.create_foundation_sandbox_usage_correction_v1(
    p_project text, p_sandbox text, p_correction text,
    p_expected_generation bigint, p_expected_resource_version bigint, p_confirmed_sandbox text,
    p_metric text, p_adjustment text, p_reason_code text,
    p_subject_digest text, p_idempotency_key text, p_request_digest text, p_request_id text
)
RETURNS text
LANGUAGE plpgsql SECURITY DEFINER SET search_path = pg_catalog, cloud_agents
AS $cloud_agents_function$
DECLARE
    ignored_principal text;
    sandbox cloud_agents.sandbox_sessions%ROWTYPE;
    replay cloud_agents.sandbox_usage_corrections%ROWTYPE;
    correction_count integer;
    correction_at timestamptz := pg_catalog.transaction_timestamp();
BEGIN
    ignored_principal := cloud_agents.require_runtime_mutation_principal();
    IF NOT cloud_agents.is_valid_identifier(p_project)
        OR NOT cloud_agents.is_valid_identifier(p_sandbox)
        OR NOT cloud_agents.is_valid_identifier(p_correction)
        OR p_expected_generation < 1 OR p_expected_resource_version < 1
        OR p_confirmed_sandbox IS DISTINCT FROM p_sandbox
        OR p_metric NOT IN (
            'allocatedMilliseconds', 'cpuMillisMilliseconds', 'memoryByteMilliseconds',
            'workspaceUsedBytes', 'networkReceivedBytes', 'networkTransmittedBytes'
        )
        OR p_adjustment !~ '^-?[1-9][0-9]{0,39}$'
        OR NOT cloud_agents.is_valid_identifier(p_reason_code)
        OR p_subject_digest !~ '^sha256:[0-9a-f]{64}$'
        OR p_idempotency_key !~ '^[A-Za-z0-9._~-]{16,128}$'
        OR p_request_digest !~ '^sha256:[0-9a-f]{64}$'
        OR NOT cloud_agents.is_valid_identifier(p_request_id)
    THEN RAISE EXCEPTION USING ERRCODE = '22023', MESSAGE = 'sandbox usage correction input is invalid'; END IF;

    SELECT stored.* INTO sandbox
    FROM cloud_agents.sandbox_sessions AS stored
    WHERE stored.tenant_id = cloud_agents.require_tenant_id()
        AND stored.project_uid = p_project AND stored.sandbox_uid = p_sandbox
    FOR UPDATE;
    IF NOT FOUND THEN
        RAISE EXCEPTION USING ERRCODE = '23503', MESSAGE = 'foundation sandbox was not found';
    END IF;

    SELECT correction.* INTO replay
    FROM cloud_agents.sandbox_usage_corrections AS correction
    WHERE correction.tenant_id = sandbox.tenant_id AND correction.project_uid = p_project
        AND correction.idempotency_key = p_idempotency_key
    FOR SHARE;
    IF FOUND THEN
        IF replay.sandbox_uid IS DISTINCT FROM p_sandbox
            OR replay.request_digest IS DISTINCT FROM p_request_digest
        THEN
            RAISE EXCEPTION USING ERRCODE = '23505', MESSAGE = 'sandbox usage correction idempotency conflict';
        END IF;
        RETURN replay.correction_uid;
    END IF;

    IF sandbox.generation IS DISTINCT FROM p_expected_generation
        OR sandbox.resource_version IS DISTINCT FROM p_expected_resource_version
    THEN
        RAISE EXCEPTION USING ERRCODE = '23505', MESSAGE = 'sandbox usage correction conflict';
    END IF;

    IF p_metric IN ('allocatedMilliseconds', 'cpuMillisMilliseconds', 'memoryByteMilliseconds') THEN
        PERFORM 1 FROM cloud_agents.sandbox_usage_checkpoints AS usage
        WHERE usage.tenant_id = sandbox.tenant_id AND usage.project_uid = p_project
            AND usage.sandbox_uid = p_sandbox LIMIT 1;
    ELSIF p_metric = 'workspaceUsedBytes' THEN
        PERFORM 1 FROM cloud_agents.workspace_volume_usage_checkpoints AS usage
        JOIN cloud_agents.workspace_volumes AS volume
          ON volume.tenant_id = usage.tenant_id AND volume.project_uid = usage.project_uid
         AND volume.volume_uid = usage.volume_uid
        WHERE volume.tenant_id = sandbox.tenant_id AND volume.project_uid = p_project
            AND volume.workspace_uid = sandbox.workspace_uid AND usage.used_bytes IS NOT NULL LIMIT 1;
    ELSE
        PERFORM 1 FROM cloud_agents.sandbox_usage_checkpoints AS usage
        WHERE usage.tenant_id = sandbox.tenant_id AND usage.project_uid = p_project
            AND usage.sandbox_uid = p_sandbox
            AND usage.network_received_bytes IS NOT NULL AND usage.network_transmitted_bytes IS NOT NULL LIMIT 1;
    END IF;
    IF NOT FOUND THEN
        RAISE EXCEPTION USING ERRCODE = '23505', MESSAGE = 'sandbox usage correction source is absent';
    END IF;

    SELECT pg_catalog.count(*)::integer INTO correction_count
    FROM cloud_agents.sandbox_usage_corrections AS correction
    WHERE correction.tenant_id = sandbox.tenant_id AND correction.project_uid = p_project
        AND correction.sandbox_uid = p_sandbox;
    -- ponytail: 100 complete records avoid a paging API; add cursor pagination before lifting this ceiling.
    IF correction_count >= 100 THEN
        RAISE EXCEPTION USING ERRCODE = '23505', MESSAGE = 'sandbox usage correction limit reached';
    END IF;

    INSERT INTO cloud_agents.sandbox_usage_corrections (
        tenant_id, project_uid, sandbox_uid, correction_uid, metric, adjustment, reason_code,
        sandbox_generation, prior_resource_version, subject_digest,
        idempotency_key, request_digest, request_id, created_at
    ) VALUES (
        sandbox.tenant_id, p_project, p_sandbox, p_correction, p_metric, p_adjustment::numeric, p_reason_code,
        sandbox.generation, sandbox.resource_version, p_subject_digest,
        p_idempotency_key, p_request_digest, p_request_id, correction_at
    );
    UPDATE cloud_agents.sandbox_sessions AS stored
    SET resource_version = stored.resource_version + 1
    WHERE stored.tenant_id = sandbox.tenant_id AND stored.project_uid = p_project
        AND stored.sandbox_uid = p_sandbox;
    RETURN p_correction;
END;
$cloud_agents_function$;

ALTER FUNCTION cloud_agents.create_foundation_sandbox_usage_correction_v1(
    text, text, text, bigint, bigint, text, text, text, text, text, text, text, text
) OWNER TO cloud_agents_migration_owner;
REVOKE ALL ON FUNCTION cloud_agents.create_foundation_sandbox_usage_correction_v1(
    text, text, text, bigint, bigint, text, text, text, text, text, text, text, text
) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION cloud_agents.create_foundation_sandbox_usage_correction_v1(
    text, text, text, bigint, bigint, text, text, text, text, text, text, text, text
) TO cloud_agents_runtime;
