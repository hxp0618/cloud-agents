CREATE TABLE cloud_agents.storage_policies (
    tenant_id text NOT NULL,
    project_uid text NOT NULL,
    policy_uid text NOT NULL,
    policy_name text NOT NULL,
    user_summary text NOT NULL,
    workspace_type text NOT NULL,
    workspace_capacity_bytes bigint NOT NULL,
    retention_seconds bigint NOT NULL,
    cleanup_on_lease_termination boolean NOT NULL,
    snapshot_backend_ref text,
    artifact_backend_ref text,
    allow_workspace_reuse boolean NOT NULL,
    resource_version bigint NOT NULL,
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL,
    PRIMARY KEY (tenant_id, project_uid, policy_uid),
    CONSTRAINT storage_policies_project_fk FOREIGN KEY (tenant_id, project_uid)
        REFERENCES cloud_agents.projects (tenant_id, project_uid) ON UPDATE RESTRICT ON DELETE RESTRICT,
    CONSTRAINT storage_policies_uid CHECK (cloud_agents.is_valid_identifier(policy_uid)),
    CONSTRAINT storage_policies_name CHECK (cloud_agents.is_valid_identifier(policy_name)),
    CONSTRAINT storage_policies_summary CHECK (
        pg_catalog.char_length(user_summary) BETWEEN 1 AND 256 AND user_summary !~ '[[:cntrl:]]'
    ),
    CONSTRAINT storage_policies_workspace_type CHECK (workspace_type = 'managed-volume'),
    CONSTRAINT storage_policies_capacity CHECK (workspace_capacity_bytes BETWEEN 134217728 AND 1099511627776),
    CONSTRAINT storage_policies_retention CHECK (retention_seconds = 0),
    CONSTRAINT storage_policies_cleanup CHECK (cleanup_on_lease_termination),
    CONSTRAINT storage_policies_snapshot_backend CHECK (
        snapshot_backend_ref IS NULL OR cloud_agents.is_valid_identifier(snapshot_backend_ref)
    ),
    CONSTRAINT storage_policies_artifact_backend CHECK (
        artifact_backend_ref IS NULL OR cloud_agents.is_valid_identifier(artifact_backend_ref)
    ),
    CONSTRAINT storage_policies_reuse CHECK (allow_workspace_reuse),
    CONSTRAINT storage_policies_resource_version CHECK (resource_version > 0),
    CONSTRAINT storage_policies_time CHECK (updated_at >= created_at)
);

INSERT INTO cloud_agents.storage_policies (
    tenant_id, project_uid, policy_uid, policy_name, user_summary,
    workspace_type, workspace_capacity_bytes, retention_seconds,
    cleanup_on_lease_termination, snapshot_backend_ref, artifact_backend_ref,
    allow_workspace_reuse, resource_version, created_at, updated_at
)
SELECT DISTINCT profile.tenant_id, profile.project_uid, profile.storage_policy_ref,
    profile.storage_policy_ref, 'Managed workspace storage', 'managed-volume',
    21474836480, 0, true, NULL, NULL, true, 1,
    pg_catalog.transaction_timestamp(), pg_catalog.transaction_timestamp()
FROM cloud_agents.environment_profiles AS profile;

ALTER TABLE cloud_agents.environment_profiles
    ADD CONSTRAINT environment_profiles_storage_policy_fk
    FOREIGN KEY (tenant_id, project_uid, storage_policy_ref)
    REFERENCES cloud_agents.storage_policies (tenant_id, project_uid, policy_uid)
    ON UPDATE RESTRICT ON DELETE RESTRICT;

CREATE TABLE cloud_agents.storage_policy_activity (
    tenant_id text NOT NULL,
    project_uid text NOT NULL,
    policy_uid text NOT NULL,
    event_uid text NOT NULL,
    operation_uid text NOT NULL,
    action text NOT NULL,
    idempotency_key text NOT NULL,
    request_id text NOT NULL,
    request_digest text NOT NULL,
    subject_digest text NOT NULL,
    policy_resource_version bigint NOT NULL,
    policy_name text NOT NULL,
    user_summary text NOT NULL,
    workspace_type text NOT NULL,
    workspace_capacity_bytes bigint NOT NULL,
    retention_seconds bigint NOT NULL,
    cleanup_on_lease_termination boolean NOT NULL,
    snapshot_backend_ref text,
    artifact_backend_ref text,
    allow_workspace_reuse boolean NOT NULL,
    policy_created_at timestamptz NOT NULL,
    policy_updated_at timestamptz NOT NULL,
    result text NOT NULL,
    occurred_at timestamptz NOT NULL,
    PRIMARY KEY (tenant_id, project_uid, policy_uid, event_uid),
    UNIQUE (tenant_id, project_uid, idempotency_key),
    CONSTRAINT storage_policy_activity_policy_fk FOREIGN KEY (tenant_id, project_uid, policy_uid)
        REFERENCES cloud_agents.storage_policies (tenant_id, project_uid, policy_uid)
        ON UPDATE RESTRICT ON DELETE RESTRICT,
    CONSTRAINT storage_policy_activity_event_uid CHECK (cloud_agents.is_valid_identifier(event_uid)),
    CONSTRAINT storage_policy_activity_operation_uid CHECK (cloud_agents.is_valid_identifier(operation_uid)),
    CONSTRAINT storage_policy_activity_action CHECK (action = 'storage-policy.set'),
    CONSTRAINT storage_policy_activity_key CHECK (idempotency_key ~ '^[A-Za-z0-9._~-]{16,128}$'),
    CONSTRAINT storage_policy_activity_request_id CHECK (cloud_agents.is_valid_identifier(request_id)),
    CONSTRAINT storage_policy_activity_request_digest CHECK (request_digest ~ '^sha256:[0-9a-f]{64}$'),
    CONSTRAINT storage_policy_activity_subject_digest CHECK (subject_digest ~ '^sha256:[0-9a-f]{64}$'),
    CONSTRAINT storage_policy_activity_version CHECK (policy_resource_version > 0),
    CONSTRAINT storage_policy_activity_name CHECK (cloud_agents.is_valid_identifier(policy_name)),
    CONSTRAINT storage_policy_activity_summary CHECK (
        pg_catalog.char_length(user_summary) BETWEEN 1 AND 256 AND user_summary !~ '[[:cntrl:]]'
    ),
    CONSTRAINT storage_policy_activity_workspace CHECK (
        workspace_type = 'managed-volume'
        AND workspace_capacity_bytes BETWEEN 134217728 AND 1099511627776
        AND retention_seconds = 0 AND cleanup_on_lease_termination AND allow_workspace_reuse
    ),
    CONSTRAINT storage_policy_activity_snapshot_backend CHECK (
        snapshot_backend_ref IS NULL OR cloud_agents.is_valid_identifier(snapshot_backend_ref)
    ),
    CONSTRAINT storage_policy_activity_artifact_backend CHECK (
        artifact_backend_ref IS NULL OR cloud_agents.is_valid_identifier(artifact_backend_ref)
    ),
    CONSTRAINT storage_policy_activity_result CHECK (result = 'succeeded'),
    CONSTRAINT storage_policy_activity_time CHECK (
        policy_updated_at >= policy_created_at AND occurred_at = policy_updated_at
    )
);

CREATE INDEX storage_policies_page_idx
    ON cloud_agents.storage_policies (tenant_id, project_uid, policy_uid);
CREATE INDEX storage_policy_activity_audit_idx
    ON cloud_agents.storage_policy_activity
    (tenant_id, project_uid, policy_uid, occurred_at DESC, event_uid DESC);

ALTER TABLE cloud_agents.storage_policies OWNER TO cloud_agents_migration_owner;
ALTER TABLE cloud_agents.storage_policy_activity OWNER TO cloud_agents_migration_owner;
ALTER TABLE cloud_agents.storage_policies ENABLE ROW LEVEL SECURITY;
ALTER TABLE cloud_agents.storage_policies FORCE ROW LEVEL SECURITY;
ALTER TABLE cloud_agents.storage_policy_activity ENABLE ROW LEVEL SECURITY;
ALTER TABLE cloud_agents.storage_policy_activity FORCE ROW LEVEL SECURITY;
CREATE POLICY storage_policies_runtime_tenant ON cloud_agents.storage_policies
    TO cloud_agents_runtime USING (tenant_id = cloud_agents.require_tenant_id())
    WITH CHECK (tenant_id = cloud_agents.require_tenant_id());
CREATE POLICY storage_policies_migration_owner ON cloud_agents.storage_policies
    TO cloud_agents_migration_owner USING (true) WITH CHECK (true);
CREATE POLICY storage_policy_activity_runtime_tenant ON cloud_agents.storage_policy_activity
    TO cloud_agents_runtime USING (tenant_id = cloud_agents.require_tenant_id())
    WITH CHECK (tenant_id = cloud_agents.require_tenant_id());
CREATE POLICY storage_policy_activity_migration_owner ON cloud_agents.storage_policy_activity
    TO cloud_agents_migration_owner USING (true) WITH CHECK (true);
REVOKE ALL ON TABLE cloud_agents.storage_policies FROM PUBLIC;
REVOKE ALL ON TABLE cloud_agents.storage_policy_activity FROM PUBLIC;
GRANT SELECT ON TABLE cloud_agents.storage_policies TO cloud_agents_runtime;
GRANT SELECT ON TABLE cloud_agents.storage_policy_activity TO cloud_agents_runtime;

CREATE FUNCTION cloud_agents.set_storage_policy_v1(
    p_tenant_id text, p_project_uid text, p_policy_uid text, p_policy_name text,
    p_user_summary text, p_workspace_type text, p_workspace_capacity_bytes bigint,
    p_retention_seconds bigint, p_cleanup_on_lease_termination boolean,
    p_snapshot_backend_ref text, p_artifact_backend_ref text, p_allow_workspace_reuse boolean,
    p_expected_resource_version bigint, p_idempotency_key text, p_request_digest text,
    p_request_id text, p_subject_digest text
)
RETURNS TABLE (
    policy_uid text, policy_name text, user_summary text, workspace_type text,
    workspace_capacity_bytes bigint, retention_seconds bigint,
    cleanup_on_lease_termination boolean, snapshot_backend_ref text,
    artifact_backend_ref text, allow_workspace_reuse boolean,
    resource_version bigint, created_at timestamptz, updated_at timestamptz
)
LANGUAGE plpgsql SECURITY DEFINER SET search_path = pg_catalog, cloud_agents
AS $cloud_agents_function$
DECLARE
    ignored_principal text;
    mutation_at timestamptz;
    existing cloud_agents.storage_policies%ROWTYPE;
    replay cloud_agents.storage_policy_activity%ROWTYPE;
    operation_uid_value text;
BEGIN
    ignored_principal := cloud_agents.require_runtime_mutation_principal();
    mutation_at := pg_catalog.transaction_timestamp();
    IF p_tenant_id IS DISTINCT FROM cloud_agents.require_tenant_id()
        OR NOT cloud_agents.is_valid_identifier(p_project_uid)
        OR NOT cloud_agents.is_valid_identifier(p_policy_uid)
        OR NOT cloud_agents.is_valid_identifier(p_policy_name)
        OR pg_catalog.char_length(p_user_summary) NOT BETWEEN 1 AND 256
        OR p_user_summary ~ '[[:cntrl:]]'
        OR p_workspace_type IS DISTINCT FROM 'managed-volume'
        OR p_workspace_capacity_bytes NOT BETWEEN 134217728 AND 1099511627776
        OR p_retention_seconds IS DISTINCT FROM 0
        OR p_cleanup_on_lease_termination IS DISTINCT FROM true
        OR NULLIF(p_snapshot_backend_ref, '') IS NOT NULL
            AND NOT cloud_agents.is_valid_identifier(p_snapshot_backend_ref)
        OR NULLIF(p_artifact_backend_ref, '') IS NOT NULL
            AND NOT cloud_agents.is_valid_identifier(p_artifact_backend_ref)
        OR p_allow_workspace_reuse IS DISTINCT FROM true
        OR p_expected_resource_version < 0
        OR p_idempotency_key !~ '^[A-Za-z0-9._~-]{16,128}$'
        OR p_request_digest !~ '^sha256:[0-9a-f]{64}$'
        OR NOT cloud_agents.is_valid_identifier(p_request_id)
        OR p_subject_digest !~ '^sha256:[0-9a-f]{64}$'
    THEN RAISE EXCEPTION USING ERRCODE = '22023', MESSAGE = 'storage policy input is invalid'; END IF;

    SELECT activity.* INTO replay
    FROM cloud_agents.storage_policy_activity AS activity
    WHERE activity.tenant_id = p_tenant_id AND activity.project_uid = p_project_uid
        AND activity.idempotency_key = p_idempotency_key
    FOR SHARE;
    IF FOUND THEN
        IF replay.request_digest IS DISTINCT FROM p_request_digest THEN
            RAISE EXCEPTION USING ERRCODE = '23505', MESSAGE = 'storage policy idempotency conflict';
        END IF;
        policy_uid := replay.policy_uid; policy_name := replay.policy_name;
        user_summary := replay.user_summary; workspace_type := replay.workspace_type;
        workspace_capacity_bytes := replay.workspace_capacity_bytes;
        retention_seconds := replay.retention_seconds;
        cleanup_on_lease_termination := replay.cleanup_on_lease_termination;
        snapshot_backend_ref := COALESCE(replay.snapshot_backend_ref, '');
        artifact_backend_ref := COALESCE(replay.artifact_backend_ref, '');
        allow_workspace_reuse := replay.allow_workspace_reuse;
        resource_version := replay.policy_resource_version;
        created_at := replay.policy_created_at; updated_at := replay.policy_updated_at;
    ELSE
        PERFORM 1 FROM cloud_agents.projects AS project
        WHERE project.tenant_id = p_tenant_id AND project.project_uid = p_project_uid
            AND project.state = 'active'
        FOR KEY SHARE;
        IF NOT FOUND THEN RAISE EXCEPTION USING ERRCODE = '23503', MESSAGE = 'project is absent or inactive'; END IF;

        SELECT policy.* INTO existing
        FROM cloud_agents.storage_policies AS policy
        WHERE policy.tenant_id = p_tenant_id AND policy.project_uid = p_project_uid
            AND policy.policy_uid = p_policy_uid
        FOR UPDATE;
        IF FOUND THEN
            IF existing.resource_version <> p_expected_resource_version THEN
                RAISE EXCEPTION USING ERRCODE = '23505', MESSAGE = 'storage policy resource version conflict';
            END IF;
            PERFORM 1 FROM cloud_agents.environment_profiles AS profile
            WHERE profile.tenant_id = p_tenant_id AND profile.project_uid = p_project_uid
                AND profile.storage_policy_ref = p_policy_uid
            LIMIT 1;
            IF FOUND THEN RAISE EXCEPTION USING ERRCODE = '23505', MESSAGE = 'storage policy is referenced'; END IF;
            UPDATE cloud_agents.storage_policies AS policy
            SET policy_name = p_policy_name, user_summary = p_user_summary,
                workspace_type = p_workspace_type,
                workspace_capacity_bytes = p_workspace_capacity_bytes,
                retention_seconds = p_retention_seconds,
                cleanup_on_lease_termination = p_cleanup_on_lease_termination,
                snapshot_backend_ref = NULLIF(p_snapshot_backend_ref, ''),
                artifact_backend_ref = NULLIF(p_artifact_backend_ref, ''),
                allow_workspace_reuse = p_allow_workspace_reuse,
                resource_version = policy.resource_version + 1, updated_at = mutation_at
            WHERE policy.tenant_id = p_tenant_id AND policy.project_uid = p_project_uid
                AND policy.policy_uid = p_policy_uid
            RETURNING policy.* INTO existing;
        ELSE
            IF p_expected_resource_version <> 0 THEN
                RAISE EXCEPTION USING ERRCODE = '23505', MESSAGE = 'storage policy resource version conflict';
            END IF;
            INSERT INTO cloud_agents.storage_policies (
                tenant_id, project_uid, policy_uid, policy_name, user_summary,
                workspace_type, workspace_capacity_bytes, retention_seconds,
                cleanup_on_lease_termination, snapshot_backend_ref, artifact_backend_ref,
                allow_workspace_reuse, resource_version, created_at, updated_at
            ) VALUES (
                p_tenant_id, p_project_uid, p_policy_uid, p_policy_name, p_user_summary,
                p_workspace_type, p_workspace_capacity_bytes, p_retention_seconds,
                p_cleanup_on_lease_termination, NULLIF(p_snapshot_backend_ref, ''),
                NULLIF(p_artifact_backend_ref, ''), p_allow_workspace_reuse,
                1, mutation_at, mutation_at
            ) RETURNING storage_policies.* INTO existing;
        END IF;

        operation_uid_value := 'op-' || pg_catalog.md5(
            p_tenant_id || '|' || p_project_uid || '|' || p_policy_uid || '|storage-policy.set|' || p_idempotency_key
        );
        INSERT INTO cloud_agents.storage_policy_activity (
            tenant_id, project_uid, policy_uid, event_uid, operation_uid, action,
            idempotency_key, request_id, request_digest, subject_digest,
            policy_resource_version, policy_name, user_summary, workspace_type,
            workspace_capacity_bytes, retention_seconds, cleanup_on_lease_termination,
            snapshot_backend_ref, artifact_backend_ref, allow_workspace_reuse,
            policy_created_at, policy_updated_at, result, occurred_at
        ) VALUES (
            p_tenant_id, p_project_uid, p_policy_uid,
            operation_uid_value || '-succeeded', operation_uid_value, 'storage-policy.set',
            p_idempotency_key, p_request_id, p_request_digest, p_subject_digest,
            existing.resource_version, existing.policy_name, existing.user_summary,
            existing.workspace_type, existing.workspace_capacity_bytes, existing.retention_seconds,
            existing.cleanup_on_lease_termination, existing.snapshot_backend_ref,
            existing.artifact_backend_ref, existing.allow_workspace_reuse,
            existing.created_at, existing.updated_at, 'succeeded', mutation_at
        );

        policy_uid := existing.policy_uid; policy_name := existing.policy_name;
        user_summary := existing.user_summary; workspace_type := existing.workspace_type;
        workspace_capacity_bytes := existing.workspace_capacity_bytes;
        retention_seconds := existing.retention_seconds;
        cleanup_on_lease_termination := existing.cleanup_on_lease_termination;
        snapshot_backend_ref := COALESCE(existing.snapshot_backend_ref, '');
        artifact_backend_ref := COALESCE(existing.artifact_backend_ref, '');
        allow_workspace_reuse := existing.allow_workspace_reuse;
        resource_version := existing.resource_version;
        created_at := existing.created_at; updated_at := existing.updated_at;
    END IF;
    RETURN NEXT;
END;
$cloud_agents_function$;

CREATE FUNCTION cloud_agents.create_environment_profile_draft_v3(
    p_tenant_id text, p_project_uid text, p_profile_uid text, p_profile_name text,
    p_profile_version bigint, p_description text, p_provider_kinds_csv text,
    p_cpu_limit_millis bigint, p_memory_limit_bytes bigint,
    p_storage_policy_ref text, p_network_policy_ref text, p_release_digest text,
    p_target_refs_csv text, p_provider_credential_ref text,
    p_idempotency_key text, p_request_digest text, p_request_id text, p_subject_digest text
)
RETURNS TABLE (
    profile_version_uid text, profile_uid text, profile_name text, profile_version bigint,
    description text, status text, provider_kinds text[], cpu_limit_millis bigint,
    memory_limit_bytes bigint, storage_policy_ref text, network_policy_ref text,
    release_digest text, target_refs text[], provider_credential_ref text,
    resource_version bigint, created_at timestamptz, updated_at timestamptz,
    published_at timestamptz, disabled_at timestamptz
)
LANGUAGE plpgsql SECURITY DEFINER SET search_path = pg_catalog, cloud_agents
AS $cloud_agents_function$
DECLARE ignored_principal text; existing_profile cloud_agents.environment_profiles%ROWTYPE;
BEGIN
    ignored_principal := cloud_agents.require_runtime_mutation_principal();
    SELECT profile.* INTO existing_profile
    FROM cloud_agents.environment_profiles AS profile
    WHERE profile.tenant_id = p_tenant_id AND profile.project_uid = p_project_uid
        AND profile.create_idempotency_key = p_idempotency_key
    FOR SHARE;
    IF NOT FOUND THEN
        PERFORM 1 FROM cloud_agents.storage_policies AS policy
        WHERE policy.tenant_id = p_tenant_id AND policy.project_uid = p_project_uid
            AND policy.policy_uid = p_storage_policy_ref
        FOR SHARE;
        IF NOT FOUND THEN
            RAISE EXCEPTION USING ERRCODE = '23503', MESSAGE = 'storage policy is not available';
        END IF;
    END IF;
    RETURN QUERY SELECT * FROM cloud_agents.create_environment_profile_draft_v2(
        p_tenant_id, p_project_uid, p_profile_uid, p_profile_name, p_profile_version,
        p_description, p_provider_kinds_csv, p_cpu_limit_millis, p_memory_limit_bytes,
        p_storage_policy_ref, p_network_policy_ref, p_release_digest, p_target_refs_csv,
        p_provider_credential_ref, p_idempotency_key, p_request_digest, p_request_id, p_subject_digest
    );
END;
$cloud_agents_function$;

CREATE FUNCTION cloud_agents.transition_environment_profile_v3(
    p_tenant_id text, p_project_uid text, p_profile_uid text, p_profile_version bigint,
    p_expected_resource_version bigint, p_action text, p_idempotency_key text,
    p_request_digest text, p_request_id text, p_subject_digest text
)
RETURNS TABLE (
    profile_version_uid text, profile_uid text, profile_name text, profile_version bigint,
    description text, status text, provider_kinds text[], cpu_limit_millis bigint,
    memory_limit_bytes bigint, storage_policy_ref text, network_policy_ref text,
    release_digest text, target_refs text[], provider_credential_ref text,
    resource_version bigint, created_at timestamptz, updated_at timestamptz,
    published_at timestamptz, disabled_at timestamptz
)
LANGUAGE plpgsql SECURITY DEFINER SET search_path = pg_catalog, cloud_agents
AS $cloud_agents_function$
DECLARE ignored_principal text; referenced_policy text;
BEGIN
    ignored_principal := cloud_agents.require_runtime_mutation_principal();
    IF p_action = 'publish' THEN
        SELECT profile.storage_policy_ref INTO referenced_policy
        FROM cloud_agents.environment_profiles AS profile
        WHERE profile.tenant_id = p_tenant_id AND profile.project_uid = p_project_uid
            AND profile.profile_uid = p_profile_uid AND profile.profile_version = p_profile_version
        FOR SHARE;
        IF FOUND THEN
            PERFORM 1 FROM cloud_agents.storage_policies AS policy
            WHERE policy.tenant_id = p_tenant_id AND policy.project_uid = p_project_uid
                AND policy.policy_uid = referenced_policy
            FOR SHARE;
            IF NOT FOUND THEN
                RAISE EXCEPTION USING ERRCODE = '23503', MESSAGE = 'storage policy is not available';
            END IF;
        END IF;
    END IF;
    RETURN QUERY SELECT * FROM cloud_agents.transition_environment_profile_v2(
        p_tenant_id, p_project_uid, p_profile_uid, p_profile_version,
        p_expected_resource_version, p_action, p_idempotency_key,
        p_request_digest, p_request_id, p_subject_digest
    );
END;
$cloud_agents_function$;

CREATE FUNCTION cloud_agents.create_user_environment_v5(
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
DECLARE ignored_principal text; existing_lease cloud_agents.managed_host_environment_leases%ROWTYPE;
BEGIN
    ignored_principal := cloud_agents.require_runtime_mutation_principal();
    SELECT lease.* INTO existing_lease
    FROM cloud_agents.managed_host_environment_leases AS lease
    WHERE lease.tenant_id = p_tenant_id AND lease.project_uid = p_project_uid
        AND lease.create_idempotency_key = p_idempotency_key
    FOR SHARE;
    IF NOT FOUND THEN
        PERFORM 1 FROM cloud_agents.environment_profiles AS profile
        JOIN cloud_agents.storage_policies AS policy
          ON policy.tenant_id = profile.tenant_id AND policy.project_uid = profile.project_uid
         AND policy.policy_uid = profile.storage_policy_ref
        WHERE profile.tenant_id = p_tenant_id AND profile.project_uid = p_project_uid
            AND profile.profile_uid = p_profile_uid AND profile.profile_version = p_profile_version
            AND profile.status = 'published'
        FOR SHARE OF profile, policy;
        IF NOT FOUND THEN
            RAISE EXCEPTION USING ERRCODE = '23505', MESSAGE = 'environment profile is not available';
        END IF;
    END IF;
    RETURN QUERY SELECT * FROM cloud_agents.create_user_environment_v4(
        p_tenant_id, p_project_uid, p_environment_uid, p_profile_uid,
        p_profile_version, p_ttl_seconds, p_idempotency_key, p_request_digest
    );
END;
$cloud_agents_function$;

ALTER FUNCTION cloud_agents.set_storage_policy_v1(text, text, text, text, text, text, bigint, bigint, boolean, text, text, boolean, bigint, text, text, text, text)
    OWNER TO cloud_agents_migration_owner;
ALTER FUNCTION cloud_agents.create_environment_profile_draft_v3(text, text, text, text, bigint, text, text, bigint, bigint, text, text, text, text, text, text, text, text, text)
    OWNER TO cloud_agents_migration_owner;
ALTER FUNCTION cloud_agents.transition_environment_profile_v3(text, text, text, bigint, bigint, text, text, text, text, text)
    OWNER TO cloud_agents_migration_owner;
ALTER FUNCTION cloud_agents.create_user_environment_v5(text, text, text, text, bigint, bigint, text, text)
    OWNER TO cloud_agents_migration_owner;
REVOKE ALL ON FUNCTION cloud_agents.set_storage_policy_v1(text, text, text, text, text, text, bigint, bigint, boolean, text, text, boolean, bigint, text, text, text, text)
    FROM PUBLIC;
REVOKE ALL ON FUNCTION cloud_agents.create_environment_profile_draft_v3(text, text, text, text, bigint, text, text, bigint, bigint, text, text, text, text, text, text, text, text, text)
    FROM PUBLIC;
REVOKE ALL ON FUNCTION cloud_agents.transition_environment_profile_v3(text, text, text, bigint, bigint, text, text, text, text, text)
    FROM PUBLIC;
REVOKE ALL ON FUNCTION cloud_agents.create_user_environment_v5(text, text, text, text, bigint, bigint, text, text)
    FROM PUBLIC;
REVOKE EXECUTE ON FUNCTION cloud_agents.create_environment_profile_draft_v2(text, text, text, text, bigint, text, text, bigint, bigint, text, text, text, text, text, text, text, text, text)
    FROM cloud_agents_runtime;
REVOKE EXECUTE ON FUNCTION cloud_agents.transition_environment_profile_v2(text, text, text, bigint, bigint, text, text, text, text, text)
    FROM cloud_agents_runtime;
REVOKE EXECUTE ON FUNCTION cloud_agents.create_user_environment_v4(text, text, text, text, bigint, bigint, text, text)
    FROM cloud_agents_runtime;
GRANT EXECUTE ON FUNCTION cloud_agents.set_storage_policy_v1(text, text, text, text, text, text, bigint, bigint, boolean, text, text, boolean, bigint, text, text, text, text)
    TO cloud_agents_runtime;
GRANT EXECUTE ON FUNCTION cloud_agents.create_environment_profile_draft_v3(text, text, text, text, bigint, text, text, bigint, bigint, text, text, text, text, text, text, text, text, text)
    TO cloud_agents_runtime;
GRANT EXECUTE ON FUNCTION cloud_agents.transition_environment_profile_v3(text, text, text, bigint, bigint, text, text, text, text, text)
    TO cloud_agents_runtime;
GRANT EXECUTE ON FUNCTION cloud_agents.create_user_environment_v5(text, text, text, text, bigint, bigint, text, text)
    TO cloud_agents_runtime;
