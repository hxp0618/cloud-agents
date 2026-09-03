ALTER TABLE cloud_agents.environment_profiles
    ADD COLUMN publish_idempotency_key text;
ALTER TABLE cloud_agents.environment_profiles
    ADD COLUMN publish_request_digest text;
ALTER TABLE cloud_agents.environment_profiles
    ADD COLUMN disable_idempotency_key text;
ALTER TABLE cloud_agents.environment_profiles
    ADD COLUMN disable_request_digest text;
ALTER TABLE cloud_agents.environment_profiles
    ADD CONSTRAINT environment_profiles_publish_idempotency CHECK (
        publish_idempotency_key IS NULL AND publish_request_digest IS NULL
        OR publish_idempotency_key IS NOT NULL AND publish_request_digest IS NOT NULL
            AND publish_idempotency_key ~ '^[A-Za-z0-9._~-]{16,128}$'
            AND publish_request_digest ~ '^sha256:[0-9a-f]{64}$'
    );
ALTER TABLE cloud_agents.environment_profiles
    ADD CONSTRAINT environment_profiles_disable_idempotency CHECK (
        disable_idempotency_key IS NULL AND disable_request_digest IS NULL
        OR disable_idempotency_key IS NOT NULL AND disable_request_digest IS NOT NULL
            AND disable_idempotency_key ~ '^[A-Za-z0-9._~-]{16,128}$'
            AND disable_request_digest ~ '^sha256:[0-9a-f]{64}$'
    );
ALTER TABLE cloud_agents.environment_profile_activity
    DROP CONSTRAINT environment_profile_activity_action;
ALTER TABLE cloud_agents.environment_profile_activity
    ADD CONSTRAINT environment_profile_activity_action CHECK (
        action IN ('profile.create', 'profile.publish', 'profile.disable')
    );

CREATE FUNCTION cloud_agents.transition_environment_profile_v1(
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
    mutation_at timestamptz;
    existing cloud_agents.environment_profiles%ROWTYPE;
    operation_uid_value text;
BEGIN
    ignored_principal := cloud_agents.require_runtime_mutation_principal();
    mutation_at := pg_catalog.transaction_timestamp();
    IF p_tenant_id IS DISTINCT FROM cloud_agents.require_tenant_id()
        OR NOT cloud_agents.is_valid_identifier(p_project_uid)
        OR NOT cloud_agents.is_valid_identifier(p_profile_uid)
        OR p_profile_version NOT BETWEEN 1 AND 2147483647
        OR p_expected_resource_version IS NULL OR p_expected_resource_version < 1
        OR p_action IS NULL OR p_action NOT IN ('publish', 'disable')
        OR p_idempotency_key IS NULL OR p_idempotency_key !~ '^[A-Za-z0-9._~-]{16,128}$'
        OR p_request_digest IS NULL OR p_request_digest !~ '^sha256:[0-9a-f]{64}$'
        OR p_request_id IS NULL OR NOT cloud_agents.is_valid_identifier(p_request_id)
        OR p_subject_digest IS NULL OR p_subject_digest !~ '^sha256:[0-9a-f]{64}$'
    THEN RAISE EXCEPTION USING ERRCODE = '22023', MESSAGE = 'environment profile transition input is invalid'; END IF;

    SELECT profile.* INTO existing
    FROM cloud_agents.environment_profiles AS profile
    WHERE profile.tenant_id = p_tenant_id AND profile.project_uid = p_project_uid
        AND profile.profile_uid = p_profile_uid AND profile.profile_version = p_profile_version
    FOR UPDATE;
    IF NOT FOUND THEN
        RAISE EXCEPTION USING ERRCODE = '23503', MESSAGE = 'environment profile was not found';
    END IF;

    IF p_action = 'publish' THEN
        IF existing.publish_idempotency_key IS NOT NULL THEN
            IF existing.publish_idempotency_key = p_idempotency_key
                AND existing.publish_request_digest IS DISTINCT FROM p_request_digest
            THEN
                RAISE EXCEPTION USING ERRCODE = '23505', MESSAGE = 'environment profile idempotency conflict';
            ELSIF existing.publish_idempotency_key IS DISTINCT FROM p_idempotency_key THEN
                RAISE EXCEPTION USING ERRCODE = '23505', MESSAGE = 'environment profile transition conflict';
            END IF;
        ELSE
            IF existing.status <> 'draft' OR existing.resource_version <> p_expected_resource_version
                OR EXISTS (
                    SELECT 1
                    FROM pg_catalog.unnest(existing.target_refs) AS target_ref
                    WHERE NOT EXISTS (
                        SELECT 1 FROM cloud_agents.deployment_targets AS target
                        WHERE target.tenant_id = p_tenant_id AND target.project_uid = p_project_uid
                            AND target.target_uid = target_ref
                    )
                )
            THEN
                RAISE EXCEPTION USING ERRCODE = '23505', MESSAGE = 'environment profile transition conflict';
            END IF;
            UPDATE cloud_agents.environment_profiles AS profile
            SET status = 'published', published_at = mutation_at, updated_at = mutation_at,
                resource_version = profile.resource_version + 1,
                publish_idempotency_key = p_idempotency_key,
                publish_request_digest = p_request_digest
            WHERE profile.tenant_id = p_tenant_id AND profile.project_uid = p_project_uid
                AND profile.profile_uid = p_profile_uid AND profile.profile_version = p_profile_version
            RETURNING profile.* INTO existing;
            operation_uid_value := 'op-' || pg_catalog.md5(
                p_tenant_id || '|' || p_project_uid || '|' || existing.profile_version_uid || '|profile.publish|' || p_idempotency_key
            );
            INSERT INTO cloud_agents.environment_profile_activity (
                tenant_id, project_uid, profile_version_uid, event_uid, operation_uid, action,
                idempotency_key, request_id, request_digest, subject_digest, profile_version,
                result, occurred_at
            ) VALUES (
                p_tenant_id, p_project_uid, existing.profile_version_uid,
                operation_uid_value || '-succeeded', operation_uid_value, 'profile.publish',
                p_idempotency_key, p_request_id, p_request_digest, p_subject_digest,
                existing.profile_version, 'succeeded', mutation_at
            );
        END IF;
    ELSE
        IF existing.disable_idempotency_key IS NOT NULL THEN
            IF existing.disable_idempotency_key = p_idempotency_key
                AND existing.disable_request_digest IS DISTINCT FROM p_request_digest
            THEN
                RAISE EXCEPTION USING ERRCODE = '23505', MESSAGE = 'environment profile idempotency conflict';
            ELSIF existing.disable_idempotency_key IS DISTINCT FROM p_idempotency_key THEN
                RAISE EXCEPTION USING ERRCODE = '23505', MESSAGE = 'environment profile transition conflict';
            END IF;
        ELSE
            IF existing.status <> 'published' OR existing.resource_version <> p_expected_resource_version THEN
                RAISE EXCEPTION USING ERRCODE = '23505', MESSAGE = 'environment profile transition conflict';
            END IF;
            UPDATE cloud_agents.environment_profiles AS profile
            SET status = 'disabled', disabled_at = mutation_at, updated_at = mutation_at,
                resource_version = profile.resource_version + 1,
                disable_idempotency_key = p_idempotency_key,
                disable_request_digest = p_request_digest
            WHERE profile.tenant_id = p_tenant_id AND profile.project_uid = p_project_uid
                AND profile.profile_uid = p_profile_uid AND profile.profile_version = p_profile_version
            RETURNING profile.* INTO existing;
            operation_uid_value := 'op-' || pg_catalog.md5(
                p_tenant_id || '|' || p_project_uid || '|' || existing.profile_version_uid || '|profile.disable|' || p_idempotency_key
            );
            INSERT INTO cloud_agents.environment_profile_activity (
                tenant_id, project_uid, profile_version_uid, event_uid, operation_uid, action,
                idempotency_key, request_id, request_digest, subject_digest, profile_version,
                result, occurred_at
            ) VALUES (
                p_tenant_id, p_project_uid, existing.profile_version_uid,
                operation_uid_value || '-succeeded', operation_uid_value, 'profile.disable',
                p_idempotency_key, p_request_id, p_request_digest, p_subject_digest,
                existing.profile_version, 'succeeded', mutation_at
            );
        END IF;
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

ALTER FUNCTION cloud_agents.transition_environment_profile_v1(text, text, text, bigint, bigint, text, text, text, text, text)
    OWNER TO cloud_agents_migration_owner;
REVOKE ALL ON FUNCTION cloud_agents.transition_environment_profile_v1(text, text, text, bigint, bigint, text, text, text, text, text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION cloud_agents.transition_environment_profile_v1(text, text, text, bigint, bigint, text, text, text, text, text) TO cloud_agents_runtime;
