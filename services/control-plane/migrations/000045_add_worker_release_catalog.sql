CREATE TABLE cloud_agents.worker_releases (
    tenant_id text NOT NULL,
    project_uid text NOT NULL,
    release_uid text NOT NULL,
    release_name text NOT NULL,
    image_repository text NOT NULL,
    release_digest text NOT NULL,
    platform_version text NOT NULL,
    runtime_version text NOT NULL,
    codex_version text NOT NULL,
    claude_code_version text NOT NULL,
    architectures text[] NOT NULL,
    status text NOT NULL,
    verification_state text NOT NULL,
    verification_evidence_digest text NOT NULL,
    resource_version bigint NOT NULL,
    register_idempotency_key text NOT NULL,
    register_request_digest text NOT NULL,
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL,
    approved_at timestamptz NOT NULL,
    PRIMARY KEY (tenant_id, project_uid, release_uid),
    UNIQUE (tenant_id, project_uid, release_digest),
    UNIQUE (tenant_id, project_uid, register_idempotency_key),
    CONSTRAINT worker_releases_project_fk FOREIGN KEY (tenant_id, project_uid)
        REFERENCES cloud_agents.projects (tenant_id, project_uid) ON UPDATE RESTRICT ON DELETE RESTRICT,
    CONSTRAINT worker_releases_uid CHECK (cloud_agents.is_valid_identifier(release_uid)),
    CONSTRAINT worker_releases_name CHECK (cloud_agents.is_valid_identifier(release_name)),
    CONSTRAINT worker_releases_repository CHECK (
        pg_catalog.char_length(image_repository) BETWEEN 3 AND 512
        AND image_repository ~ '^[a-z0-9]+(?:[.-][a-z0-9]+)*(?::[0-9]{1,5})?(?:/[a-z0-9]+(?:[._-][a-z0-9]+)*)+$'
    ),
    CONSTRAINT worker_releases_digest CHECK (release_digest ~ '^sha256:[0-9a-f]{64}$'),
    CONSTRAINT worker_releases_platform_version CHECK (cloud_agents.is_valid_identifier(platform_version)),
    CONSTRAINT worker_releases_runtime_version CHECK (cloud_agents.is_valid_identifier(runtime_version)),
    CONSTRAINT worker_releases_codex_version CHECK (cloud_agents.is_valid_identifier(codex_version)),
    CONSTRAINT worker_releases_claude_version CHECK (cloud_agents.is_valid_identifier(claude_code_version)),
    CONSTRAINT worker_releases_architectures CHECK (
        pg_catalog.cardinality(architectures) BETWEEN 1 AND 2
        AND architectures <@ ARRAY['linux/amd64', 'linux/arm64']::text[]
    ),
    CONSTRAINT worker_releases_status CHECK (status = 'approved'),
    CONSTRAINT worker_releases_verification_state CHECK (verification_state = 'attested'),
    CONSTRAINT worker_releases_evidence_digest CHECK (verification_evidence_digest ~ '^sha256:[0-9a-f]{64}$'),
    CONSTRAINT worker_releases_resource_version CHECK (resource_version = 1),
    CONSTRAINT worker_releases_register_key CHECK (register_idempotency_key ~ '^[A-Za-z0-9._~-]{16,128}$'),
    CONSTRAINT worker_releases_register_digest CHECK (register_request_digest ~ '^sha256:[0-9a-f]{64}$'),
    CONSTRAINT worker_releases_time CHECK (
        updated_at >= created_at AND approved_at >= created_at
    )
);

CREATE INDEX worker_releases_page_idx
    ON cloud_agents.worker_releases (tenant_id, project_uid, release_uid);

CREATE TABLE cloud_agents.worker_release_activity (
    tenant_id text NOT NULL,
    project_uid text NOT NULL,
    release_uid text NOT NULL,
    event_uid text NOT NULL,
    action text NOT NULL,
    idempotency_key text NOT NULL,
    request_id text NOT NULL,
    request_digest text NOT NULL,
    subject_digest text NOT NULL,
    result text NOT NULL,
    occurred_at timestamptz NOT NULL,
    PRIMARY KEY (tenant_id, project_uid, release_uid, event_uid),
    CONSTRAINT worker_release_activity_release_fk FOREIGN KEY (tenant_id, project_uid, release_uid)
        REFERENCES cloud_agents.worker_releases (tenant_id, project_uid, release_uid)
        ON UPDATE RESTRICT ON DELETE RESTRICT,
    CONSTRAINT worker_release_activity_event_uid CHECK (cloud_agents.is_valid_identifier(event_uid)),
    CONSTRAINT worker_release_activity_action CHECK (action = 'release.register'),
    CONSTRAINT worker_release_activity_key CHECK (idempotency_key ~ '^[A-Za-z0-9._~-]{16,128}$'),
    CONSTRAINT worker_release_activity_request_id CHECK (cloud_agents.is_valid_identifier(request_id)),
    CONSTRAINT worker_release_activity_request_digest CHECK (request_digest ~ '^sha256:[0-9a-f]{64}$'),
    CONSTRAINT worker_release_activity_subject_digest CHECK (subject_digest ~ '^sha256:[0-9a-f]{64}$'),
    CONSTRAINT worker_release_activity_result CHECK (result = 'succeeded')
);

ALTER TABLE cloud_agents.worker_releases OWNER TO cloud_agents_migration_owner;
ALTER TABLE cloud_agents.worker_release_activity OWNER TO cloud_agents_migration_owner;
ALTER TABLE cloud_agents.worker_releases ENABLE ROW LEVEL SECURITY;
ALTER TABLE cloud_agents.worker_releases FORCE ROW LEVEL SECURITY;
ALTER TABLE cloud_agents.worker_release_activity ENABLE ROW LEVEL SECURITY;
ALTER TABLE cloud_agents.worker_release_activity FORCE ROW LEVEL SECURITY;
CREATE POLICY worker_releases_runtime_tenant ON cloud_agents.worker_releases
    TO cloud_agents_runtime USING (tenant_id = cloud_agents.require_tenant_id())
    WITH CHECK (tenant_id = cloud_agents.require_tenant_id());
CREATE POLICY worker_releases_migration_owner ON cloud_agents.worker_releases
    TO cloud_agents_migration_owner USING (true) WITH CHECK (true);
CREATE POLICY worker_release_activity_runtime_tenant ON cloud_agents.worker_release_activity
    TO cloud_agents_runtime USING (tenant_id = cloud_agents.require_tenant_id())
    WITH CHECK (tenant_id = cloud_agents.require_tenant_id());
CREATE POLICY worker_release_activity_migration_owner ON cloud_agents.worker_release_activity
    TO cloud_agents_migration_owner USING (true) WITH CHECK (true);
REVOKE ALL ON TABLE cloud_agents.worker_releases FROM PUBLIC;
REVOKE ALL ON TABLE cloud_agents.worker_release_activity FROM PUBLIC;
GRANT SELECT ON TABLE cloud_agents.worker_releases TO cloud_agents_runtime;
GRANT SELECT ON TABLE cloud_agents.worker_release_activity TO cloud_agents_runtime;

CREATE FUNCTION cloud_agents.register_worker_release_v1(
    p_tenant_id text, p_project_uid text, p_release_uid text, p_release_name text,
    p_image_repository text, p_release_digest text, p_platform_version text,
    p_runtime_version text, p_codex_version text, p_claude_code_version text,
    p_architectures_csv text, p_verification_evidence_digest text,
    p_idempotency_key text, p_request_digest text, p_request_id text, p_subject_digest text
)
RETURNS TABLE (
    release_uid text, release_name text, image_repository text, release_digest text,
    platform_version text, runtime_version text, codex_version text, claude_code_version text,
    architectures text[], status text, verification_state text,
    verification_evidence_digest text, resource_version bigint,
    created_at timestamptz, updated_at timestamptz, approved_at timestamptz
)
LANGUAGE plpgsql SECURITY DEFINER SET search_path = pg_catalog, cloud_agents
AS $cloud_agents_function$
DECLARE
    ignored_principal text;
    mutation_at timestamptz;
    existing cloud_agents.worker_releases%ROWTYPE;
    architectures_value text[];
    event_uid_value text;
BEGIN
    ignored_principal := cloud_agents.require_runtime_mutation_principal();
    mutation_at := pg_catalog.transaction_timestamp();
    architectures_value := pg_catalog.string_to_array(p_architectures_csv, ',');
    IF p_tenant_id IS DISTINCT FROM cloud_agents.require_tenant_id()
        OR NOT cloud_agents.is_valid_identifier(p_project_uid)
        OR NOT cloud_agents.is_valid_identifier(p_release_uid)
        OR NOT cloud_agents.is_valid_identifier(p_release_name)
        OR pg_catalog.char_length(p_image_repository) NOT BETWEEN 3 AND 512
        OR p_image_repository !~ '^[a-z0-9]+(?:[.-][a-z0-9]+)*(?::[0-9]{1,5})?(?:/[a-z0-9]+(?:[._-][a-z0-9]+)*)+$'
        OR p_release_digest !~ '^sha256:[0-9a-f]{64}$'
        OR NOT cloud_agents.is_valid_identifier(p_platform_version)
        OR NOT cloud_agents.is_valid_identifier(p_runtime_version)
        OR NOT cloud_agents.is_valid_identifier(p_codex_version)
        OR NOT cloud_agents.is_valid_identifier(p_claude_code_version)
        OR pg_catalog.cardinality(architectures_value) NOT BETWEEN 1 AND 2
        OR EXISTS (SELECT 1 FROM pg_catalog.unnest(architectures_value) AS architecture
            WHERE architecture NOT IN ('linux/amd64', 'linux/arm64'))
        OR pg_catalog.cardinality(architectures_value) <>
            (SELECT pg_catalog.count(DISTINCT architecture) FROM pg_catalog.unnest(architectures_value) AS architecture)
        OR p_verification_evidence_digest !~ '^sha256:[0-9a-f]{64}$'
        OR p_idempotency_key !~ '^[A-Za-z0-9._~-]{16,128}$'
        OR p_request_digest !~ '^sha256:[0-9a-f]{64}$'
        OR NOT cloud_agents.is_valid_identifier(p_request_id)
        OR p_subject_digest !~ '^sha256:[0-9a-f]{64}$'
    THEN RAISE EXCEPTION USING ERRCODE = '22023', MESSAGE = 'worker release input is invalid'; END IF;

    SELECT release.* INTO existing
    FROM cloud_agents.worker_releases AS release
    WHERE release.tenant_id = p_tenant_id AND release.project_uid = p_project_uid
        AND release.register_idempotency_key = p_idempotency_key
    FOR UPDATE;
    IF FOUND THEN
        IF existing.register_request_digest IS DISTINCT FROM p_request_digest THEN
            RAISE EXCEPTION USING ERRCODE = '23505', MESSAGE = 'worker release idempotency conflict';
        END IF;
    ELSE
        PERFORM 1 FROM cloud_agents.projects AS project
        WHERE project.tenant_id = p_tenant_id AND project.project_uid = p_project_uid
            AND project.state = 'active'
        FOR KEY SHARE;
        IF NOT FOUND THEN
            RAISE EXCEPTION USING ERRCODE = '23503', MESSAGE = 'project is absent or inactive';
        END IF;

        IF EXISTS (
            SELECT 1 FROM cloud_agents.worker_releases AS release
            WHERE release.tenant_id = p_tenant_id AND release.project_uid = p_project_uid
                AND (release.release_uid = p_release_uid OR release.release_digest = p_release_digest)
        ) THEN RAISE EXCEPTION USING ERRCODE = '23505', MESSAGE = 'worker release conflict'; END IF;

        INSERT INTO cloud_agents.worker_releases (
            tenant_id, project_uid, release_uid, release_name, image_repository, release_digest,
            platform_version, runtime_version, codex_version, claude_code_version, architectures,
            status, verification_state, verification_evidence_digest, resource_version,
            register_idempotency_key, register_request_digest, created_at, updated_at, approved_at
        ) VALUES (
            p_tenant_id, p_project_uid, p_release_uid, p_release_name, p_image_repository, p_release_digest,
            p_platform_version, p_runtime_version, p_codex_version, p_claude_code_version, architectures_value,
            'approved', 'attested', p_verification_evidence_digest, 1,
            p_idempotency_key, p_request_digest, mutation_at, mutation_at, mutation_at
        ) RETURNING worker_releases.* INTO existing;

        event_uid_value := 'event-' || pg_catalog.md5(
            p_tenant_id || '|' || p_project_uid || '|' || p_release_uid || '|release.register|' || p_idempotency_key
        );
        INSERT INTO cloud_agents.worker_release_activity (
            tenant_id, project_uid, release_uid, event_uid, action, idempotency_key,
            request_id, request_digest, subject_digest, result, occurred_at
        ) VALUES (
            p_tenant_id, p_project_uid, p_release_uid, event_uid_value, 'release.register',
            p_idempotency_key, p_request_id, p_request_digest, p_subject_digest, 'succeeded', mutation_at
        );
    END IF;

    release_uid := existing.release_uid; release_name := existing.release_name;
    image_repository := existing.image_repository; release_digest := existing.release_digest;
    platform_version := existing.platform_version; runtime_version := existing.runtime_version;
    codex_version := existing.codex_version; claude_code_version := existing.claude_code_version;
    architectures := existing.architectures; status := existing.status;
    verification_state := existing.verification_state;
    verification_evidence_digest := existing.verification_evidence_digest;
    resource_version := existing.resource_version; created_at := existing.created_at;
    updated_at := existing.updated_at; approved_at := existing.approved_at;
    RETURN NEXT;
END;
$cloud_agents_function$;

CREATE FUNCTION cloud_agents.create_environment_profile_draft_v2(
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
    existing_profile cloud_agents.environment_profiles%ROWTYPE;
BEGIN
    ignored_principal := cloud_agents.require_runtime_mutation_principal();
    SELECT profile.* INTO existing_profile
    FROM cloud_agents.environment_profiles AS profile
    WHERE profile.tenant_id = p_tenant_id AND profile.project_uid = p_project_uid
        AND profile.create_idempotency_key = p_idempotency_key
    FOR UPDATE;
    IF NOT FOUND THEN
        PERFORM 1 FROM cloud_agents.worker_releases AS release
        WHERE release.tenant_id = p_tenant_id AND release.project_uid = p_project_uid
            AND release.release_digest = p_release_digest
            AND release.status = 'approved' AND release.verification_state = 'attested'
        FOR SHARE;
        IF NOT FOUND THEN
            RAISE EXCEPTION USING ERRCODE = '23505', MESSAGE = 'worker release is not approved';
        END IF;
    END IF;
    RETURN QUERY SELECT * FROM cloud_agents.create_environment_profile_draft_v1(
        p_tenant_id, p_project_uid, p_profile_uid, p_profile_name, p_profile_version,
        p_description, p_provider_kinds_csv, p_cpu_limit_millis, p_memory_limit_bytes,
        p_storage_policy_ref, p_network_policy_ref, p_release_digest, p_target_refs_csv,
        p_provider_credential_ref, p_idempotency_key, p_request_digest, p_request_id, p_subject_digest
    );
END;
$cloud_agents_function$;

CREATE FUNCTION cloud_agents.transition_environment_profile_v2(
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
DECLARE
    ignored_principal text;
    existing_profile cloud_agents.environment_profiles%ROWTYPE;
BEGIN
    ignored_principal := cloud_agents.require_runtime_mutation_principal();
    IF p_action = 'publish' THEN
        SELECT profile.* INTO existing_profile
        FROM cloud_agents.environment_profiles AS profile
        WHERE profile.tenant_id = p_tenant_id AND profile.project_uid = p_project_uid
            AND profile.profile_uid = p_profile_uid AND profile.profile_version = p_profile_version
        FOR SHARE;
        IF FOUND AND existing_profile.status = 'draft' AND existing_profile.publish_idempotency_key IS NULL THEN
            PERFORM 1 FROM cloud_agents.worker_releases AS release
            WHERE release.tenant_id = p_tenant_id AND release.project_uid = p_project_uid
                AND release.release_digest = existing_profile.release_digest
                AND release.status = 'approved' AND release.verification_state = 'attested'
            FOR SHARE;
            IF NOT FOUND THEN
                RAISE EXCEPTION USING ERRCODE = '23505', MESSAGE = 'worker release is not approved';
            END IF;
        END IF;
    END IF;
    RETURN QUERY SELECT * FROM cloud_agents.transition_environment_profile_v1(
        p_tenant_id, p_project_uid, p_profile_uid, p_profile_version,
        p_expected_resource_version, p_action, p_idempotency_key,
        p_request_digest, p_request_id, p_subject_digest
    );
END;
$cloud_agents_function$;

CREATE FUNCTION cloud_agents.create_user_environment_v3(
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
    existing_lease cloud_agents.managed_host_environment_leases%ROWTYPE;
BEGIN
    ignored_principal := cloud_agents.require_runtime_mutation_principal();
    SELECT lease.* INTO existing_lease
    FROM cloud_agents.managed_host_environment_leases AS lease
    WHERE lease.tenant_id = p_tenant_id AND lease.project_uid = p_project_uid
        AND lease.create_idempotency_key = p_idempotency_key
    FOR UPDATE;
    IF NOT FOUND THEN
        PERFORM 1
        FROM cloud_agents.environment_profiles AS profile
        JOIN cloud_agents.worker_releases AS release
          ON release.tenant_id = profile.tenant_id AND release.project_uid = profile.project_uid
         AND release.release_digest = profile.release_digest
         AND release.status = 'approved' AND release.verification_state = 'attested'
        WHERE profile.tenant_id = p_tenant_id AND profile.project_uid = p_project_uid
            AND profile.profile_uid = p_profile_uid AND profile.profile_version = p_profile_version
            AND profile.status = 'published'
        FOR SHARE OF profile, release;
        IF NOT FOUND THEN
            RAISE EXCEPTION USING ERRCODE = '23505', MESSAGE = 'environment profile is not available';
        END IF;
    END IF;
    RETURN QUERY SELECT * FROM cloud_agents.create_user_environment_v2(
        p_tenant_id, p_project_uid, p_environment_uid, p_profile_uid,
        p_profile_version, p_ttl_seconds, p_idempotency_key, p_request_digest
    );
END;
$cloud_agents_function$;

ALTER FUNCTION cloud_agents.register_worker_release_v1(text, text, text, text, text, text, text, text, text, text, text, text, text, text, text, text)
    OWNER TO cloud_agents_migration_owner;
ALTER FUNCTION cloud_agents.create_environment_profile_draft_v2(text, text, text, text, bigint, text, text, bigint, bigint, text, text, text, text, text, text, text, text, text)
    OWNER TO cloud_agents_migration_owner;
ALTER FUNCTION cloud_agents.transition_environment_profile_v2(text, text, text, bigint, bigint, text, text, text, text, text)
    OWNER TO cloud_agents_migration_owner;
ALTER FUNCTION cloud_agents.create_user_environment_v3(text, text, text, text, bigint, bigint, text, text)
    OWNER TO cloud_agents_migration_owner;

REVOKE EXECUTE ON FUNCTION cloud_agents.create_environment_profile_draft_v1(text, text, text, text, bigint, text, text, bigint, bigint, text, text, text, text, text, text, text, text, text)
    FROM cloud_agents_runtime;
REVOKE EXECUTE ON FUNCTION cloud_agents.transition_environment_profile_v1(text, text, text, bigint, bigint, text, text, text, text, text)
    FROM cloud_agents_runtime;
REVOKE EXECUTE ON FUNCTION cloud_agents.create_user_environment_v2(text, text, text, text, bigint, bigint, text, text)
    FROM cloud_agents_runtime;
REVOKE ALL ON FUNCTION cloud_agents.register_worker_release_v1(text, text, text, text, text, text, text, text, text, text, text, text, text, text, text, text)
    FROM PUBLIC;
REVOKE ALL ON FUNCTION cloud_agents.create_environment_profile_draft_v2(text, text, text, text, bigint, text, text, bigint, bigint, text, text, text, text, text, text, text, text, text)
    FROM PUBLIC;
REVOKE ALL ON FUNCTION cloud_agents.transition_environment_profile_v2(text, text, text, bigint, bigint, text, text, text, text, text)
    FROM PUBLIC;
REVOKE ALL ON FUNCTION cloud_agents.create_user_environment_v3(text, text, text, text, bigint, bigint, text, text)
    FROM PUBLIC;
GRANT EXECUTE ON FUNCTION cloud_agents.register_worker_release_v1(text, text, text, text, text, text, text, text, text, text, text, text, text, text, text, text)
    TO cloud_agents_runtime;
GRANT EXECUTE ON FUNCTION cloud_agents.create_environment_profile_draft_v2(text, text, text, text, bigint, text, text, bigint, bigint, text, text, text, text, text, text, text, text, text)
    TO cloud_agents_runtime;
GRANT EXECUTE ON FUNCTION cloud_agents.transition_environment_profile_v2(text, text, text, bigint, bigint, text, text, text, text, text)
    TO cloud_agents_runtime;
GRANT EXECUTE ON FUNCTION cloud_agents.create_user_environment_v3(text, text, text, text, bigint, bigint, text, text)
    TO cloud_agents_runtime;
