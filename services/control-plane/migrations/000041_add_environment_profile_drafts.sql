CREATE TABLE cloud_agents.environment_profiles (
    tenant_id text NOT NULL,
    tenant_ref_id text NOT NULL,
    project_uid text NOT NULL,
    profile_version_uid text NOT NULL,
    profile_uid text NOT NULL,
    profile_name text NOT NULL,
    profile_version bigint NOT NULL,
    description text NOT NULL,
    status text NOT NULL,
    provider_kinds text[] NOT NULL,
    cpu_limit_millis bigint NOT NULL,
    memory_limit_bytes bigint NOT NULL,
    storage_policy_ref text NOT NULL,
    network_policy_ref text NOT NULL,
    release_digest text NOT NULL,
    target_refs text[] NOT NULL,
    provider_credential_ref text NOT NULL,
    resource_version bigint NOT NULL,
    create_idempotency_key text NOT NULL,
    create_request_digest text NOT NULL,
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL,
    published_at timestamptz,
    disabled_at timestamptz,
    PRIMARY KEY (tenant_id, project_uid, profile_uid, profile_version),
    UNIQUE (tenant_id, project_uid, profile_version_uid),
    UNIQUE (tenant_id, project_uid, create_idempotency_key),
    CONSTRAINT environment_profiles_tenant_ref CHECK (tenant_id = tenant_ref_id),
    CONSTRAINT environment_profiles_project_fk FOREIGN KEY (tenant_id, project_uid)
        REFERENCES cloud_agents.projects (tenant_id, project_uid) ON UPDATE RESTRICT ON DELETE RESTRICT,
    CONSTRAINT environment_profiles_tenant_fk FOREIGN KEY (tenant_id, tenant_ref_id)
        REFERENCES cloud_agents.platform_tenants (tenant_id, tenant_uid) ON UPDATE RESTRICT ON DELETE RESTRICT,
    CONSTRAINT environment_profiles_version_uid CHECK (cloud_agents.is_valid_identifier(profile_version_uid)),
    CONSTRAINT environment_profiles_uid CHECK (cloud_agents.is_valid_identifier(profile_uid)),
    CONSTRAINT environment_profiles_name CHECK (cloud_agents.is_valid_identifier(profile_name)),
    CONSTRAINT environment_profiles_version CHECK (profile_version BETWEEN 1 AND 2147483647),
    CONSTRAINT environment_profiles_description CHECK (
        pg_catalog.char_length(description) BETWEEN 1 AND 1024 AND description !~ '[[:cntrl:]]'
    ),
    CONSTRAINT environment_profiles_status CHECK (status IN ('draft', 'published', 'disabled')),
    CONSTRAINT environment_profiles_provider_kinds CHECK (
        pg_catalog.cardinality(provider_kinds) BETWEEN 1 AND 2
        AND provider_kinds <@ ARRAY['codex', 'claudeAgent']::text[]
    ),
    CONSTRAINT environment_profiles_cpu CHECK (cpu_limit_millis BETWEEN 100 AND 64000),
    CONSTRAINT environment_profiles_memory CHECK (memory_limit_bytes BETWEEN 134217728 AND 1099511627776),
    CONSTRAINT environment_profiles_storage_policy CHECK (cloud_agents.is_valid_identifier(storage_policy_ref)),
    CONSTRAINT environment_profiles_network_policy CHECK (cloud_agents.is_valid_identifier(network_policy_ref)),
    CONSTRAINT environment_profiles_release CHECK (release_digest ~ '^sha256:[0-9a-f]{64}$'),
    CONSTRAINT environment_profiles_targets CHECK (
        pg_catalog.cardinality(target_refs) BETWEEN 1 AND 32
    ),
    CONSTRAINT environment_profiles_provider_credential CHECK (cloud_agents.is_valid_identifier(provider_credential_ref)),
    CONSTRAINT environment_profiles_resource_version CHECK (resource_version > 0),
    CONSTRAINT environment_profiles_create_key CHECK (create_idempotency_key ~ '^[A-Za-z0-9._~-]{16,128}$'),
    CONSTRAINT environment_profiles_create_digest CHECK (create_request_digest ~ '^sha256:[0-9a-f]{64}$'),
    CONSTRAINT environment_profiles_lifecycle CHECK (
        (status = 'draft' AND published_at IS NULL AND disabled_at IS NULL)
        OR (status = 'published' AND published_at IS NOT NULL AND disabled_at IS NULL)
        OR (status = 'disabled' AND published_at IS NOT NULL AND disabled_at IS NOT NULL AND disabled_at >= published_at)
    ),
    CONSTRAINT environment_profiles_time CHECK (
        updated_at >= created_at AND (published_at IS NULL OR published_at >= created_at)
    )
);

CREATE INDEX environment_profiles_page_idx
    ON cloud_agents.environment_profiles (tenant_id, project_uid, profile_version_uid);

CREATE TABLE cloud_agents.environment_profile_activity (
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
    CONSTRAINT environment_profile_activity_profile_fk FOREIGN KEY (tenant_id, project_uid, profile_version_uid)
        REFERENCES cloud_agents.environment_profiles (tenant_id, project_uid, profile_version_uid)
        ON UPDATE RESTRICT ON DELETE RESTRICT,
    CONSTRAINT environment_profile_activity_event_uid CHECK (cloud_agents.is_valid_identifier(event_uid)),
    CONSTRAINT environment_profile_activity_operation_uid CHECK (cloud_agents.is_valid_identifier(operation_uid)),
    CONSTRAINT environment_profile_activity_action CHECK (action = 'profile.create'),
    CONSTRAINT environment_profile_activity_key CHECK (idempotency_key ~ '^[A-Za-z0-9._~-]{16,128}$'),
    CONSTRAINT environment_profile_activity_request_id CHECK (cloud_agents.is_valid_identifier(request_id)),
    CONSTRAINT environment_profile_activity_request_digest CHECK (request_digest ~ '^sha256:[0-9a-f]{64}$'),
    CONSTRAINT environment_profile_activity_subject_digest CHECK (subject_digest ~ '^sha256:[0-9a-f]{64}$'),
    CONSTRAINT environment_profile_activity_version CHECK (profile_version BETWEEN 1 AND 2147483647),
    CONSTRAINT environment_profile_activity_result CHECK (result = 'succeeded')
);

CREATE INDEX environment_profile_activity_audit_idx
    ON cloud_agents.environment_profile_activity
    (tenant_id, project_uid, profile_version_uid, occurred_at DESC, event_uid DESC);

ALTER TABLE cloud_agents.environment_profiles OWNER TO cloud_agents_migration_owner;
ALTER TABLE cloud_agents.environment_profile_activity OWNER TO cloud_agents_migration_owner;
ALTER TABLE cloud_agents.environment_profiles ENABLE ROW LEVEL SECURITY;
ALTER TABLE cloud_agents.environment_profiles FORCE ROW LEVEL SECURITY;
ALTER TABLE cloud_agents.environment_profile_activity ENABLE ROW LEVEL SECURITY;
ALTER TABLE cloud_agents.environment_profile_activity FORCE ROW LEVEL SECURITY;
CREATE POLICY environment_profiles_runtime_tenant ON cloud_agents.environment_profiles
    TO cloud_agents_runtime USING (tenant_id = cloud_agents.require_tenant_id())
    WITH CHECK (tenant_id = cloud_agents.require_tenant_id());
CREATE POLICY environment_profiles_migration_owner ON cloud_agents.environment_profiles
    TO cloud_agents_migration_owner USING (true) WITH CHECK (true);
CREATE POLICY environment_profile_activity_runtime_tenant ON cloud_agents.environment_profile_activity
    TO cloud_agents_runtime USING (tenant_id = cloud_agents.require_tenant_id())
    WITH CHECK (tenant_id = cloud_agents.require_tenant_id());
CREATE POLICY environment_profile_activity_migration_owner ON cloud_agents.environment_profile_activity
    TO cloud_agents_migration_owner USING (true) WITH CHECK (true);
REVOKE ALL ON TABLE cloud_agents.environment_profiles FROM PUBLIC;
REVOKE ALL ON TABLE cloud_agents.environment_profile_activity FROM PUBLIC;
GRANT SELECT ON TABLE cloud_agents.environment_profiles TO cloud_agents_runtime;
GRANT SELECT ON TABLE cloud_agents.environment_profile_activity TO cloud_agents_runtime;

CREATE FUNCTION cloud_agents.create_environment_profile_draft_v1(
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
DECLARE
    ignored_principal text;
    mutation_at timestamptz;
    existing cloud_agents.environment_profiles%ROWTYPE;
    previous cloud_agents.environment_profiles%ROWTYPE;
    operation_uid_value text;
    provider_kinds_value text[];
    target_refs_value text[];
BEGIN
    ignored_principal := cloud_agents.require_runtime_mutation_principal();
    mutation_at := pg_catalog.transaction_timestamp();
    provider_kinds_value := pg_catalog.string_to_array(p_provider_kinds_csv, ',');
    target_refs_value := pg_catalog.string_to_array(p_target_refs_csv, ',');
    IF p_tenant_id IS DISTINCT FROM cloud_agents.require_tenant_id()
        OR NOT cloud_agents.is_valid_identifier(p_project_uid)
        OR NOT cloud_agents.is_valid_identifier(p_profile_uid)
        OR NOT cloud_agents.is_valid_identifier(p_profile_name)
        OR p_profile_version NOT BETWEEN 1 AND 2147483647
        OR pg_catalog.char_length(p_description) NOT BETWEEN 1 AND 1024
        OR p_description ~ '[[:cntrl:]]'
        OR pg_catalog.cardinality(provider_kinds_value) NOT BETWEEN 1 AND 2
        OR EXISTS (SELECT 1 FROM pg_catalog.unnest(provider_kinds_value) AS provider_kind WHERE provider_kind NOT IN ('codex', 'claudeAgent'))
        OR pg_catalog.cardinality(provider_kinds_value) <> (SELECT pg_catalog.count(DISTINCT provider_kind) FROM pg_catalog.unnest(provider_kinds_value) AS provider_kind)
        OR p_cpu_limit_millis NOT BETWEEN 100 AND 64000
        OR p_memory_limit_bytes NOT BETWEEN 134217728 AND 1099511627776
        OR NOT cloud_agents.is_valid_identifier(p_storage_policy_ref)
        OR NOT cloud_agents.is_valid_identifier(p_network_policy_ref)
        OR p_release_digest !~ '^sha256:[0-9a-f]{64}$'
        OR pg_catalog.cardinality(target_refs_value) NOT BETWEEN 1 AND 32
        OR EXISTS (SELECT 1 FROM pg_catalog.unnest(target_refs_value) AS target_ref WHERE NOT cloud_agents.is_valid_identifier(target_ref))
        OR pg_catalog.cardinality(target_refs_value) <> (SELECT pg_catalog.count(DISTINCT target_ref) FROM pg_catalog.unnest(target_refs_value) AS target_ref)
        OR NOT cloud_agents.is_valid_identifier(p_provider_credential_ref)
        OR p_idempotency_key !~ '^[A-Za-z0-9._~-]{16,128}$'
        OR p_request_digest !~ '^sha256:[0-9a-f]{64}$'
        OR NOT cloud_agents.is_valid_identifier(p_request_id)
        OR p_subject_digest !~ '^sha256:[0-9a-f]{64}$'
    THEN RAISE EXCEPTION USING ERRCODE = '22023', MESSAGE = 'environment profile input is invalid'; END IF;

    SELECT profile.* INTO existing
    FROM cloud_agents.environment_profiles AS profile
    WHERE profile.tenant_id = p_tenant_id AND profile.project_uid = p_project_uid
        AND profile.create_idempotency_key = p_idempotency_key
    FOR UPDATE;
    IF FOUND THEN
        IF existing.create_request_digest IS DISTINCT FROM p_request_digest THEN
            RAISE EXCEPTION USING ERRCODE = '23505', MESSAGE = 'environment profile idempotency conflict';
        END IF;
    ELSE
        PERFORM 1 FROM cloud_agents.projects AS project
        WHERE project.tenant_id = p_tenant_id AND project.project_uid = p_project_uid AND project.state = 'active'
        FOR KEY SHARE;
        IF NOT FOUND THEN RAISE EXCEPTION USING ERRCODE = '23503', MESSAGE = 'project is absent or inactive'; END IF;

        SELECT profile.* INTO previous
        FROM cloud_agents.environment_profiles AS profile
        WHERE profile.tenant_id = p_tenant_id AND profile.project_uid = p_project_uid
            AND profile.profile_uid = p_profile_uid
        ORDER BY profile.profile_version DESC
        LIMIT 1 FOR UPDATE;
        IF FOUND THEN
            IF previous.profile_name IS DISTINCT FROM p_profile_name THEN
                RAISE EXCEPTION USING ERRCODE = '23505', MESSAGE = 'environment profile name conflict';
            END IF;
            IF p_profile_version <> previous.profile_version + 1 THEN
                RAISE EXCEPTION USING ERRCODE = '23505', MESSAGE = 'environment profile version conflict';
            END IF;
        ELSIF p_profile_version <> 1 THEN
            RAISE EXCEPTION USING ERRCODE = '23505', MESSAGE = 'environment profile version conflict';
        END IF;

        INSERT INTO cloud_agents.environment_profiles (
            tenant_id, tenant_ref_id, project_uid, profile_version_uid, profile_uid,
            profile_name, profile_version, description, status, provider_kinds,
            cpu_limit_millis, memory_limit_bytes, storage_policy_ref, network_policy_ref,
            release_digest, target_refs, provider_credential_ref, resource_version,
            create_idempotency_key, create_request_digest, created_at, updated_at
        ) VALUES (
            p_tenant_id, p_tenant_id, p_project_uid,
            'ep-' || pg_catalog.md5(p_tenant_id || '|' || p_project_uid || '|' || p_profile_uid || '|' || p_profile_version::text),
            p_profile_uid, p_profile_name, p_profile_version, p_description, 'draft', provider_kinds_value,
            p_cpu_limit_millis, p_memory_limit_bytes, p_storage_policy_ref, p_network_policy_ref,
            p_release_digest, target_refs_value, p_provider_credential_ref, 1,
            p_idempotency_key, p_request_digest, mutation_at, mutation_at
        ) RETURNING environment_profiles.* INTO existing;

        operation_uid_value := 'op-' || pg_catalog.md5(
            p_tenant_id || '|' || p_project_uid || '|' || existing.profile_version_uid || '|profile.create|' || p_idempotency_key
        );
        INSERT INTO cloud_agents.environment_profile_activity (
            tenant_id, project_uid, profile_version_uid, event_uid, operation_uid, action,
            idempotency_key, request_id, request_digest, subject_digest, profile_version,
            result, occurred_at
        ) VALUES (
            p_tenant_id, p_project_uid, existing.profile_version_uid,
            operation_uid_value || '-succeeded', operation_uid_value, 'profile.create',
            p_idempotency_key, p_request_id, p_request_digest, p_subject_digest,
            existing.profile_version, 'succeeded', mutation_at
        );
    END IF;

    profile_version_uid := existing.profile_version_uid;
    profile_uid := existing.profile_uid;
    profile_name := existing.profile_name;
    profile_version := existing.profile_version;
    description := existing.description;
    status := existing.status;
    provider_kinds := existing.provider_kinds;
    cpu_limit_millis := existing.cpu_limit_millis;
    memory_limit_bytes := existing.memory_limit_bytes;
    storage_policy_ref := existing.storage_policy_ref;
    network_policy_ref := existing.network_policy_ref;
    release_digest := existing.release_digest;
    target_refs := existing.target_refs;
    provider_credential_ref := existing.provider_credential_ref;
    resource_version := existing.resource_version;
    created_at := existing.created_at;
    updated_at := existing.updated_at;
    published_at := existing.published_at;
    disabled_at := existing.disabled_at;
    RETURN NEXT;
END;
$cloud_agents_function$;

ALTER FUNCTION cloud_agents.create_environment_profile_draft_v1(text, text, text, text, bigint, text, text, bigint, bigint, text, text, text, text, text, text, text, text, text)
    OWNER TO cloud_agents_migration_owner;
REVOKE ALL ON FUNCTION cloud_agents.create_environment_profile_draft_v1(text, text, text, text, bigint, text, text, bigint, bigint, text, text, text, text, text, text, text, text, text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION cloud_agents.create_environment_profile_draft_v1(text, text, text, text, bigint, text, text, bigint, bigint, text, text, text, text, text, text, text, text, text) TO cloud_agents_runtime;
