CREATE TABLE cloud_agents.runtime_profiles (
    tenant_id text NOT NULL,
    tenant_ref_id text NOT NULL,
    project_uid text NOT NULL,
    profile_version_uid text NOT NULL,
    profile_uid text NOT NULL,
    profile_name text NOT NULL,
    profile_version bigint NOT NULL,
    description text NOT NULL,
    status text NOT NULL,
    target_uid text NOT NULL,
    image_uri text NOT NULL,
    release_digest text NOT NULL,
    cpu_millis bigint NOT NULL,
    memory_bytes bigint NOT NULL,
    resource_version bigint NOT NULL,
    create_idempotency_key text NOT NULL,
    create_request_digest text NOT NULL,
    publish_idempotency_key text,
    publish_request_digest text,
    disable_idempotency_key text,
    disable_request_digest text,
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL,
    published_at timestamptz,
    disabled_at timestamptz,
    PRIMARY KEY (tenant_id, project_uid, profile_uid, profile_version),
    UNIQUE (tenant_id, project_uid, profile_version_uid),
    UNIQUE (tenant_id, project_uid, create_idempotency_key),
    FOREIGN KEY (tenant_id, tenant_ref_id) REFERENCES cloud_agents.platform_tenants (tenant_id, tenant_uid)
        ON UPDATE RESTRICT ON DELETE RESTRICT,
    FOREIGN KEY (tenant_id, project_uid) REFERENCES cloud_agents.projects (tenant_id, project_uid)
        ON UPDATE RESTRICT ON DELETE RESTRICT,
    FOREIGN KEY (tenant_id, project_uid, target_uid) REFERENCES cloud_agents.deployment_targets (tenant_id, project_uid, target_uid)
        ON UPDATE RESTRICT ON DELETE RESTRICT,
    CHECK (tenant_id = tenant_ref_id),
    CHECK (cloud_agents.is_valid_identifier(profile_version_uid)),
    CHECK (cloud_agents.is_valid_identifier(profile_uid)),
    CHECK (cloud_agents.is_valid_identifier(profile_name)),
    CHECK (profile_version BETWEEN 1 AND 2147483647),
    CHECK (pg_catalog.char_length(description) BETWEEN 1 AND 1024 AND description !~ '[[:cntrl:]]'),
    CHECK (status IN ('draft', 'published', 'disabled')),
    CHECK (cloud_agents.is_valid_identifier(target_uid)),
    CHECK (image_uri ~ '^[A-Za-z0-9._:/-]+@sha256:[0-9a-f]{64}$' AND pg_catalog.length(image_uri) <= 1024),
    CHECK (release_digest ~ '^sha256:[0-9a-f]{64}$' AND pg_catalog.right(image_uri, 72) = '@' || release_digest),
    CHECK (cpu_millis BETWEEN 100 AND 64000),
    CHECK (memory_bytes BETWEEN 134217728 AND 1099511627776),
    CHECK (resource_version > 0),
    CHECK (create_idempotency_key ~ '^[A-Za-z0-9._~-]{16,128}$'),
    CHECK (create_request_digest ~ '^sha256:[0-9a-f]{64}$'),
    CHECK (
        (publish_idempotency_key IS NULL AND publish_request_digest IS NULL)
        OR (publish_idempotency_key ~ '^[A-Za-z0-9._~-]{16,128}$' AND publish_request_digest ~ '^sha256:[0-9a-f]{64}$')
    ),
    CHECK (
        (disable_idempotency_key IS NULL AND disable_request_digest IS NULL)
        OR (disable_idempotency_key ~ '^[A-Za-z0-9._~-]{16,128}$' AND disable_request_digest ~ '^sha256:[0-9a-f]{64}$')
    ),
    CHECK (
        status = 'draft' AND published_at IS NULL AND disabled_at IS NULL
        OR status = 'published' AND published_at IS NOT NULL AND disabled_at IS NULL
        OR status = 'disabled' AND published_at IS NOT NULL AND disabled_at IS NOT NULL AND disabled_at >= published_at
    ),
    CHECK (updated_at >= created_at AND (published_at IS NULL OR published_at >= created_at))
);
CREATE INDEX runtime_profiles_page_idx ON cloud_agents.runtime_profiles
    (tenant_id, project_uid, profile_version_uid);
CREATE INDEX runtime_profiles_target_idx ON cloud_agents.runtime_profiles
    (tenant_id, project_uid, target_uid);

CREATE TABLE cloud_agents.runtime_profile_activity (
    tenant_id text NOT NULL,
    project_uid text NOT NULL,
    profile_version_uid text NOT NULL,
    event_uid text NOT NULL,
    operation_uid text NOT NULL,
    action text NOT NULL,
    idempotency_key text NOT NULL,
    request_id text NOT NULL,
    request_digest text NOT NULL,
    subject_digest text NOT NULL,
    profile_version bigint NOT NULL,
    result text NOT NULL,
    occurred_at timestamptz NOT NULL,
    PRIMARY KEY (tenant_id, project_uid, profile_version_uid, event_uid),
    FOREIGN KEY (tenant_id, project_uid, profile_version_uid) REFERENCES cloud_agents.runtime_profiles
        (tenant_id, project_uid, profile_version_uid) ON UPDATE RESTRICT ON DELETE RESTRICT,
    CHECK (cloud_agents.is_valid_identifier(event_uid)),
    CHECK (cloud_agents.is_valid_identifier(operation_uid)),
    CHECK (action IN ('runtime-profile.create', 'runtime-profile.publish', 'runtime-profile.disable')),
    CHECK (idempotency_key ~ '^[A-Za-z0-9._~-]{16,128}$'),
    CHECK (cloud_agents.is_valid_identifier(request_id)),
    CHECK (request_digest ~ '^sha256:[0-9a-f]{64}$'),
    CHECK (subject_digest ~ '^sha256:[0-9a-f]{64}$'),
    CHECK (profile_version BETWEEN 1 AND 2147483647),
    CHECK (result = 'succeeded')
);
CREATE INDEX runtime_profile_activity_audit_idx ON cloud_agents.runtime_profile_activity
    (tenant_id, project_uid, profile_version_uid, occurred_at DESC, event_uid DESC);

ALTER TABLE cloud_agents.admin_denied_writes DROP CONSTRAINT admin_denied_writes_action_check;
ALTER TABLE cloud_agents.admin_denied_writes ADD CONSTRAINT admin_denied_writes_action_check CHECK (action IN (
    'adminUpgradeEnvironmentLease', 'adminRollbackEnvironmentLease', 'adminRegisterWorkerRelease',
    'adminSetStoragePolicy', 'adminSetNetworkPolicy', 'adminSetProjectLeaseQuota',
    'adminCreateEnvironmentProfile', 'adminPublishEnvironmentProfile', 'adminDisableEnvironmentProfile',
    'adminCreateRuntimeProfile', 'adminPublishRuntimeProfile', 'adminDisableRuntimeProfile',
    'adminRegisterDeploymentTarget', 'adminProbeDeploymentTarget',
    'adminTransitionDeploymentTargetScheduling', 'adminCleanupDeploymentTarget'
));

ALTER TABLE cloud_agents.runtime_profiles OWNER TO cloud_agents_migration_owner;
ALTER TABLE cloud_agents.runtime_profile_activity OWNER TO cloud_agents_migration_owner;
ALTER TABLE cloud_agents.runtime_profiles ENABLE ROW LEVEL SECURITY;
ALTER TABLE cloud_agents.runtime_profiles FORCE ROW LEVEL SECURITY;
ALTER TABLE cloud_agents.runtime_profile_activity ENABLE ROW LEVEL SECURITY;
ALTER TABLE cloud_agents.runtime_profile_activity FORCE ROW LEVEL SECURITY;
CREATE POLICY runtime_profiles_runtime ON cloud_agents.runtime_profiles TO cloud_agents_runtime
    USING (tenant_id = cloud_agents.require_tenant_id()) WITH CHECK (tenant_id = cloud_agents.require_tenant_id());
CREATE POLICY runtime_profiles_owner ON cloud_agents.runtime_profiles TO cloud_agents_migration_owner USING (true) WITH CHECK (true);
CREATE POLICY runtime_profile_activity_runtime ON cloud_agents.runtime_profile_activity TO cloud_agents_runtime
    USING (tenant_id = cloud_agents.require_tenant_id()) WITH CHECK (tenant_id = cloud_agents.require_tenant_id());
CREATE POLICY runtime_profile_activity_owner ON cloud_agents.runtime_profile_activity TO cloud_agents_migration_owner USING (true) WITH CHECK (true);
REVOKE ALL ON TABLE cloud_agents.runtime_profiles FROM PUBLIC;
REVOKE ALL ON TABLE cloud_agents.runtime_profile_activity FROM PUBLIC;
GRANT SELECT ON TABLE cloud_agents.runtime_profiles TO cloud_agents_runtime;
GRANT SELECT ON TABLE cloud_agents.runtime_profile_activity TO cloud_agents_runtime;

ALTER TABLE cloud_agents.sandbox_sessions ADD COLUMN runtime_profile_uid text;
ALTER TABLE cloud_agents.sandbox_sessions ADD COLUMN runtime_profile_version bigint;
ALTER TABLE cloud_agents.sandbox_sessions ADD CONSTRAINT sandbox_sessions_runtime_profile_pair CHECK (
    (runtime_profile_uid IS NULL AND runtime_profile_version IS NULL)
    OR (cloud_agents.is_valid_identifier(runtime_profile_uid) AND runtime_profile_version BETWEEN 1 AND 2147483647)
);
ALTER TABLE cloud_agents.sandbox_sessions ADD CONSTRAINT sandbox_sessions_runtime_profile_fk
    FOREIGN KEY (tenant_id, project_uid, runtime_profile_uid, runtime_profile_version)
    REFERENCES cloud_agents.runtime_profiles (tenant_id, project_uid, profile_uid, profile_version)
    ON UPDATE RESTRICT ON DELETE RESTRICT;
CREATE INDEX sandbox_sessions_runtime_profile_idx ON cloud_agents.sandbox_sessions
    (tenant_id, project_uid, runtime_profile_uid, runtime_profile_version);

CREATE FUNCTION cloud_agents.create_runtime_profile_draft_v1(
    p_tenant text, p_project text, p_profile text, p_name text, p_version bigint,
    p_description text, p_target text, p_image text, p_release text,
    p_cpu bigint, p_memory bigint, p_key text, p_digest text, p_request text, p_subject text
)
RETURNS TABLE (
    profile_version_uid text, profile_uid text, profile_name text, profile_version bigint,
    description text, status text, target_uid text, image_uri text, release_digest text,
    cpu_millis bigint, memory_bytes bigint, resource_version bigint,
    created_at timestamptz, updated_at timestamptz, published_at timestamptz, disabled_at timestamptz
)
LANGUAGE plpgsql SECURITY DEFINER SET search_path = pg_catalog, cloud_agents
AS $$
DECLARE
    ignored text;
    existing cloud_agents.runtime_profiles%ROWTYPE;
    previous cloud_agents.runtime_profiles%ROWTYPE;
    mutation_at timestamptz := transaction_timestamp();
    operation_value text;
BEGIN
    ignored := cloud_agents.require_runtime_mutation_principal();
    IF p_tenant IS DISTINCT FROM cloud_agents.require_tenant_id()
        OR NOT cloud_agents.is_valid_identifier(p_project) OR NOT cloud_agents.is_valid_identifier(p_profile)
        OR NOT cloud_agents.is_valid_identifier(p_name) OR p_version NOT BETWEEN 1 AND 2147483647
        OR pg_catalog.char_length(p_description) NOT BETWEEN 1 AND 1024 OR p_description ~ '[[:cntrl:]]'
        OR NOT cloud_agents.is_valid_identifier(p_target)
        OR p_image !~ '^[A-Za-z0-9._:/-]+@sha256:[0-9a-f]{64}$' OR pg_catalog.length(p_image) > 1024
        OR p_release !~ '^sha256:[0-9a-f]{64}$' OR pg_catalog.right(p_image, 72) IS DISTINCT FROM '@' || p_release
        OR p_cpu NOT BETWEEN 100 AND 64000 OR p_memory NOT BETWEEN 134217728 AND 1099511627776
        OR p_key !~ '^[A-Za-z0-9._~-]{16,128}$' OR p_digest !~ '^sha256:[0-9a-f]{64}$'
        OR NOT cloud_agents.is_valid_identifier(p_request) OR p_subject !~ '^sha256:[0-9a-f]{64}$'
    THEN RAISE EXCEPTION USING ERRCODE = '22023', MESSAGE = 'runtime profile input is invalid'; END IF;

    SELECT profile.* INTO existing FROM cloud_agents.runtime_profiles AS profile
    WHERE profile.tenant_id = p_tenant AND profile.project_uid = p_project
        AND profile.create_idempotency_key = p_key FOR UPDATE;
    IF FOUND THEN
        IF existing.create_request_digest IS DISTINCT FROM p_digest THEN
            RAISE EXCEPTION USING ERRCODE = '23505', MESSAGE = 'runtime profile idempotency conflict';
        END IF;
    ELSE
        PERFORM 1 FROM cloud_agents.projects AS project
        WHERE project.tenant_id = p_tenant AND project.project_uid = p_project AND project.state = 'active' FOR KEY SHARE;
        IF NOT FOUND THEN RAISE EXCEPTION USING ERRCODE = '23503', MESSAGE = 'runtime profile project unavailable'; END IF;
        PERFORM 1 FROM cloud_agents.deployment_targets AS target
        WHERE target.tenant_id = p_tenant AND target.project_uid = p_project
            AND target.target_uid = p_target AND target.target_kind = 'docker' FOR KEY SHARE;
        IF NOT FOUND THEN RAISE EXCEPTION USING ERRCODE = '23503', MESSAGE = 'runtime profile target unavailable'; END IF;
        SELECT profile.* INTO previous FROM cloud_agents.runtime_profiles AS profile
        WHERE profile.tenant_id = p_tenant AND profile.project_uid = p_project AND profile.profile_uid = p_profile
        ORDER BY profile.profile_version DESC LIMIT 1 FOR UPDATE;
        IF FOUND THEN
            IF previous.profile_name IS DISTINCT FROM p_name OR p_version <> previous.profile_version + 1 THEN
                RAISE EXCEPTION USING ERRCODE = '23505', MESSAGE = 'runtime profile version conflict';
            END IF;
        ELSIF p_version <> 1 THEN
            RAISE EXCEPTION USING ERRCODE = '23505', MESSAGE = 'runtime profile version conflict';
        END IF;
        INSERT INTO cloud_agents.runtime_profiles (
            tenant_id, tenant_ref_id, project_uid, profile_version_uid, profile_uid, profile_name,
            profile_version, description, status, target_uid, image_uri, release_digest,
            cpu_millis, memory_bytes, resource_version, create_idempotency_key,
            create_request_digest, created_at, updated_at
        ) VALUES (
            p_tenant, p_tenant, p_project,
            'rp-' || pg_catalog.md5(p_tenant || '|' || p_project || '|' || p_profile || '|' || p_version::text),
            p_profile, p_name, p_version, p_description, 'draft', p_target, p_image, p_release,
            p_cpu, p_memory, 1, p_key, p_digest, mutation_at, mutation_at
        ) RETURNING runtime_profiles.* INTO existing;
        operation_value := 'op-' || pg_catalog.md5(p_tenant || '|' || p_project || '|' || existing.profile_version_uid || '|create|' || p_key);
        INSERT INTO cloud_agents.runtime_profile_activity (
            tenant_id, project_uid, profile_version_uid, event_uid, operation_uid, action,
            idempotency_key, request_id, request_digest, subject_digest, profile_version, result, occurred_at
        ) VALUES (
            p_tenant, p_project, existing.profile_version_uid, operation_value || '-done', operation_value,
            'runtime-profile.create', p_key, p_request, p_digest, p_subject, p_version, 'succeeded', mutation_at
        );
    END IF;
    RETURN QUERY SELECT existing.profile_version_uid, existing.profile_uid, existing.profile_name,
        existing.profile_version, existing.description, existing.status, existing.target_uid,
        existing.image_uri, existing.release_digest, existing.cpu_millis, existing.memory_bytes,
        existing.resource_version, existing.created_at, existing.updated_at,
        existing.published_at, existing.disabled_at;
END;
$$;

CREATE FUNCTION cloud_agents.transition_runtime_profile_v1(
    p_tenant text, p_project text, p_profile text, p_version bigint,
    p_expected_resource_version bigint, p_action text, p_key text,
    p_digest text, p_request text, p_subject text
)
RETURNS TABLE (
    profile_version_uid text, profile_uid text, profile_name text, profile_version bigint,
    description text, status text, target_uid text, image_uri text, release_digest text,
    cpu_millis bigint, memory_bytes bigint, resource_version bigint,
    created_at timestamptz, updated_at timestamptz, published_at timestamptz, disabled_at timestamptz
)
LANGUAGE plpgsql SECURITY DEFINER SET search_path = pg_catalog, cloud_agents
AS $$
DECLARE
    ignored text;
    existing cloud_agents.runtime_profiles%ROWTYPE;
    mutation_at timestamptz := transaction_timestamp();
    operation_value text;
BEGIN
    ignored := cloud_agents.require_runtime_mutation_principal();
    IF p_tenant IS DISTINCT FROM cloud_agents.require_tenant_id()
        OR NOT cloud_agents.is_valid_identifier(p_project) OR NOT cloud_agents.is_valid_identifier(p_profile)
        OR p_version NOT BETWEEN 1 AND 2147483647 OR p_expected_resource_version < 1
        OR p_action NOT IN ('publish', 'disable') OR p_key !~ '^[A-Za-z0-9._~-]{16,128}$'
        OR p_digest !~ '^sha256:[0-9a-f]{64}$' OR NOT cloud_agents.is_valid_identifier(p_request)
        OR p_subject !~ '^sha256:[0-9a-f]{64}$'
    THEN RAISE EXCEPTION USING ERRCODE = '22023', MESSAGE = 'runtime profile transition input is invalid'; END IF;
    SELECT profile.* INTO existing FROM cloud_agents.runtime_profiles AS profile
    WHERE profile.tenant_id = p_tenant AND profile.project_uid = p_project
        AND profile.profile_uid = p_profile AND profile.profile_version = p_version FOR UPDATE;
    IF NOT FOUND THEN RAISE EXCEPTION USING ERRCODE = '23503', MESSAGE = 'runtime profile was not found'; END IF;

    IF p_action = 'publish' AND existing.publish_idempotency_key IS NOT NULL THEN
        IF existing.publish_idempotency_key IS DISTINCT FROM p_key OR existing.publish_request_digest IS DISTINCT FROM p_digest THEN
            RAISE EXCEPTION USING ERRCODE = '23505', MESSAGE = 'runtime profile idempotency conflict';
        END IF;
    ELSIF p_action = 'disable' AND existing.disable_idempotency_key IS NOT NULL THEN
        IF existing.disable_idempotency_key IS DISTINCT FROM p_key OR existing.disable_request_digest IS DISTINCT FROM p_digest THEN
            RAISE EXCEPTION USING ERRCODE = '23505', MESSAGE = 'runtime profile idempotency conflict';
        END IF;
    ELSE
        IF existing.resource_version <> p_expected_resource_version
            OR p_action = 'publish' AND existing.status <> 'draft'
            OR p_action = 'disable' AND existing.status <> 'published'
        THEN RAISE EXCEPTION USING ERRCODE = '23505', MESSAGE = 'runtime profile transition conflict'; END IF;
        IF p_action = 'publish' THEN
            PERFORM 1 FROM cloud_agents.deployment_targets AS target
            WHERE target.tenant_id = p_tenant AND target.project_uid = p_project
                AND target.target_uid = existing.target_uid AND target.target_kind = 'docker'
                AND target.observed_phase = 'ready' AND target.scheduling_state = 'active' FOR KEY SHARE;
            IF NOT FOUND THEN RAISE EXCEPTION USING ERRCODE = '23503', MESSAGE = 'runtime profile target unavailable'; END IF;
            UPDATE cloud_agents.runtime_profiles AS profile SET status = 'published',
                resource_version = profile.resource_version + 1, updated_at = mutation_at, published_at = mutation_at,
                publish_idempotency_key = p_key, publish_request_digest = p_digest
            WHERE profile.tenant_id = p_tenant AND profile.project_uid = p_project
                AND profile.profile_uid = p_profile AND profile.profile_version = p_version
            RETURNING profile.* INTO existing;
        ELSE
            UPDATE cloud_agents.runtime_profiles AS profile SET status = 'disabled',
                resource_version = profile.resource_version + 1, updated_at = mutation_at, disabled_at = mutation_at,
                disable_idempotency_key = p_key, disable_request_digest = p_digest
            WHERE profile.tenant_id = p_tenant AND profile.project_uid = p_project
                AND profile.profile_uid = p_profile AND profile.profile_version = p_version
            RETURNING profile.* INTO existing;
        END IF;
        operation_value := 'op-' || pg_catalog.md5(p_tenant || '|' || p_project || '|' || existing.profile_version_uid || '|' || p_action || '|' || p_key);
        INSERT INTO cloud_agents.runtime_profile_activity (
            tenant_id, project_uid, profile_version_uid, event_uid, operation_uid, action,
            idempotency_key, request_id, request_digest, subject_digest, profile_version, result, occurred_at
        ) VALUES (
            p_tenant, p_project, existing.profile_version_uid, operation_value || '-done', operation_value,
            'runtime-profile.' || p_action, p_key, p_request, p_digest, p_subject, p_version, 'succeeded', mutation_at
        );
    END IF;
    RETURN QUERY SELECT existing.profile_version_uid, existing.profile_uid, existing.profile_name,
        existing.profile_version, existing.description, existing.status, existing.target_uid,
        existing.image_uri, existing.release_digest, existing.cpu_millis, existing.memory_bytes,
        existing.resource_version, existing.created_at, existing.updated_at,
        existing.published_at, existing.disabled_at;
END;
$$;

CREATE FUNCTION cloud_agents.accept_foundation_sandbox_v1(
    p_project text, p_workspace text, p_workspace_name text, p_sandbox text,
    p_profile text, p_profile_version bigint, p_subject text, p_key text, p_digest text
)
RETURNS TABLE (
    operation_uid text, workspace_uid text, sandbox_uid text, profile_uid text,
    profile_version bigint, generation bigint, desired_state text, observed_state text
)
LANGUAGE plpgsql SECURITY DEFINER SET search_path = pg_catalog, cloud_agents
AS $$
DECLARE
    ignored text;
    selected cloud_agents.runtime_profiles%ROWTYPE;
    operation_value text;
    event_value text;
    prior_digest text;
    replay boolean := false;
BEGIN
    ignored := cloud_agents.require_runtime_mutation_principal();
    IF NOT cloud_agents.is_valid_identifier(p_project) OR NOT cloud_agents.is_valid_identifier(p_workspace)
        OR NOT cloud_agents.is_valid_identifier(p_workspace_name) OR NOT cloud_agents.is_valid_identifier(p_sandbox)
        OR NOT cloud_agents.is_valid_identifier(p_profile) OR p_profile_version NOT BETWEEN 1 AND 2147483647
        OR p_subject !~ '^sha256:[0-9a-f]{64}$' OR p_key !~ '^[A-Za-z0-9._~-]{16,128}$'
        OR p_digest !~ '^sha256:[0-9a-f]{64}$'
    THEN RAISE EXCEPTION USING ERRCODE = '22023', MESSAGE = 'foundation sandbox input is invalid'; END IF;
    SELECT record.request_digest INTO prior_digest FROM cloud_agents.idempotency_records AS record
    WHERE record.tenant_id = cloud_agents.require_tenant_id() AND record.subject_digest = p_subject
        AND record.profile_id = 'foundationSandboxLifecycle/v1alpha1'
        AND record.profile_digest = 'sha256:aabd6aa244c89a4850eba529dbe387132c3a0aff7ffba430f6115afb6e215a1b'
        AND record.idempotency_key = p_key;
    IF FOUND THEN
        replay := true;
        IF prior_digest IS DISTINCT FROM p_digest THEN
            RAISE EXCEPTION USING ERRCODE = '23505', MESSAGE = 'foundation sandbox idempotency conflict';
        END IF;
    END IF;
    SELECT profile.* INTO selected FROM cloud_agents.runtime_profiles AS profile
    WHERE profile.tenant_id = cloud_agents.require_tenant_id() AND profile.project_uid = p_project
        AND profile.profile_uid = p_profile AND profile.profile_version = p_profile_version FOR SHARE;
    IF NOT FOUND THEN RAISE EXCEPTION USING ERRCODE = '23503', MESSAGE = 'runtime profile was not found'; END IF;
    IF NOT replay AND selected.status <> 'published' THEN
        RAISE EXCEPTION USING ERRCODE = '23505', MESSAGE = 'runtime profile is not available';
    END IF;
    operation_value := 'op-' || pg_catalog.md5(cloud_agents.require_tenant_id() || '|' || p_project || '|' || p_sandbox || '|' || p_key);
    event_value := 'evt-' || pg_catalog.md5(cloud_agents.require_tenant_id() || '|' || p_project || '|' || p_sandbox || '|' || p_key);
    SELECT cloud_agents.accept_foundation_intent_v1(
        p_project, p_workspace, p_workspace_name, p_workspace, selected.target_uid, p_sandbox,
        selected.image_uri, selected.cpu_millis, selected.memory_bytes, operation_value,
        event_value, p_subject, p_key, p_digest
    ) INTO operation_value;
    UPDATE cloud_agents.sandbox_sessions AS sandbox
    SET runtime_profile_uid = p_profile, runtime_profile_version = p_profile_version
    WHERE sandbox.tenant_id = cloud_agents.require_tenant_id() AND sandbox.project_uid = p_project
        AND sandbox.sandbox_uid = p_sandbox
        AND (sandbox.runtime_profile_uid IS NULL AND sandbox.runtime_profile_version IS NULL
            OR sandbox.runtime_profile_uid = p_profile AND sandbox.runtime_profile_version = p_profile_version);
    IF NOT FOUND THEN RAISE EXCEPTION USING ERRCODE = '23505', MESSAGE = 'foundation sandbox profile conflict'; END IF;
    RETURN QUERY SELECT operation_value, p_workspace, p_sandbox, p_profile, p_profile_version,
        sandbox.generation, sandbox.desired_state, sandbox.observed_state
    FROM cloud_agents.sandbox_sessions AS sandbox
    WHERE sandbox.tenant_id = cloud_agents.require_tenant_id() AND sandbox.project_uid = p_project
        AND sandbox.sandbox_uid = p_sandbox;
END;
$$;

ALTER FUNCTION cloud_agents.create_runtime_profile_draft_v1(text,text,text,text,bigint,text,text,text,text,bigint,bigint,text,text,text,text)
    OWNER TO cloud_agents_migration_owner;
ALTER FUNCTION cloud_agents.transition_runtime_profile_v1(text,text,text,bigint,bigint,text,text,text,text,text)
    OWNER TO cloud_agents_migration_owner;
ALTER FUNCTION cloud_agents.accept_foundation_sandbox_v1(text,text,text,text,text,bigint,text,text,text)
    OWNER TO cloud_agents_migration_owner;
REVOKE ALL ON FUNCTION cloud_agents.create_runtime_profile_draft_v1(text,text,text,text,bigint,text,text,text,text,bigint,bigint,text,text,text,text) FROM PUBLIC;
REVOKE ALL ON FUNCTION cloud_agents.transition_runtime_profile_v1(text,text,text,bigint,bigint,text,text,text,text,text) FROM PUBLIC;
REVOKE ALL ON FUNCTION cloud_agents.accept_foundation_sandbox_v1(text,text,text,text,text,bigint,text,text,text) FROM PUBLIC;
REVOKE EXECUTE ON FUNCTION cloud_agents.accept_foundation_intent_v1(text,text,text,text,text,text,text,bigint,bigint,text,text,text,text,text) FROM cloud_agents_runtime;
GRANT EXECUTE ON FUNCTION cloud_agents.create_runtime_profile_draft_v1(text,text,text,text,bigint,text,text,text,text,bigint,bigint,text,text,text,text) TO cloud_agents_runtime;
GRANT EXECUTE ON FUNCTION cloud_agents.transition_runtime_profile_v1(text,text,text,bigint,bigint,text,text,text,text,text) TO cloud_agents_runtime;
GRANT EXECUTE ON FUNCTION cloud_agents.accept_foundation_sandbox_v1(text,text,text,text,text,bigint,text,text,text) TO cloud_agents_runtime;
