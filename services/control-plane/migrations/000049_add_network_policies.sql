CREATE TABLE cloud_agents.network_policies (
    tenant_id text NOT NULL,
    project_uid text NOT NULL,
    policy_uid text NOT NULL,
    policy_name text NOT NULL,
    user_summary text NOT NULL,
    default_egress text NOT NULL,
    allowlist_policy_ref text,
    ingress_enabled boolean NOT NULL,
    preview_enabled boolean NOT NULL,
    dns_policy_ref text,
    proxy_policy_ref text,
    resource_version bigint NOT NULL,
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL,
    PRIMARY KEY (tenant_id, project_uid, policy_uid),
    CONSTRAINT network_policies_project_fk FOREIGN KEY (tenant_id, project_uid)
        REFERENCES cloud_agents.projects (tenant_id, project_uid) ON UPDATE RESTRICT ON DELETE RESTRICT,
    CONSTRAINT network_policies_uid CHECK (cloud_agents.is_valid_identifier(policy_uid)),
    CONSTRAINT network_policies_name CHECK (cloud_agents.is_valid_identifier(policy_name)),
    CONSTRAINT network_policies_summary CHECK (
        pg_catalog.char_length(user_summary) BETWEEN 1 AND 256 AND user_summary !~ '[[:cntrl:]]'
    ),
    CONSTRAINT network_policies_egress CHECK (default_egress IN ('public', 'restricted', 'deny')),
    CONSTRAINT network_policies_allowlist CHECK (allowlist_policy_ref IS NULL OR cloud_agents.is_valid_identifier(allowlist_policy_ref)),
    CONSTRAINT network_policies_dns_ref CHECK (
        dns_policy_ref IS NULL OR cloud_agents.is_valid_identifier(dns_policy_ref)
    ),
    CONSTRAINT network_policies_proxy_ref CHECK (
        proxy_policy_ref IS NULL OR cloud_agents.is_valid_identifier(proxy_policy_ref)
    ),
    CONSTRAINT network_policies_resource_version CHECK (resource_version > 0),
    CONSTRAINT network_policies_time CHECK (updated_at >= created_at)
);

INSERT INTO cloud_agents.network_policies (
    tenant_id, project_uid, policy_uid, policy_name, user_summary,
    default_egress, allowlist_policy_ref, ingress_enabled,
    preview_enabled, dns_policy_ref, proxy_policy_ref,
    resource_version, created_at, updated_at
)
SELECT DISTINCT profile.tenant_id, profile.project_uid, profile.network_policy_ref,
    profile.network_policy_ref, 'Public internet access', 'public',
    NULL, false, false, NULL, NULL, 1,
    pg_catalog.transaction_timestamp(), pg_catalog.transaction_timestamp()
FROM cloud_agents.environment_profiles AS profile;

ALTER TABLE cloud_agents.environment_profiles
    ADD CONSTRAINT environment_profiles_network_policy_fk
    FOREIGN KEY (tenant_id, project_uid, network_policy_ref)
    REFERENCES cloud_agents.network_policies (tenant_id, project_uid, policy_uid)
    ON UPDATE RESTRICT ON DELETE RESTRICT;

CREATE TABLE cloud_agents.network_policy_activity (
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
    default_egress text NOT NULL,
    allowlist_policy_ref text,
    ingress_enabled boolean NOT NULL,
    preview_enabled boolean NOT NULL,
    dns_policy_ref text,
    proxy_policy_ref text,
    policy_created_at timestamptz NOT NULL,
    policy_updated_at timestamptz NOT NULL,
    result text NOT NULL,
    occurred_at timestamptz NOT NULL,
    PRIMARY KEY (tenant_id, project_uid, policy_uid, event_uid),
    UNIQUE (tenant_id, project_uid, idempotency_key),
    CONSTRAINT network_policy_activity_policy_fk FOREIGN KEY (tenant_id, project_uid, policy_uid)
        REFERENCES cloud_agents.network_policies (tenant_id, project_uid, policy_uid)
        ON UPDATE RESTRICT ON DELETE RESTRICT,
    CONSTRAINT network_policy_activity_event_uid CHECK (cloud_agents.is_valid_identifier(event_uid)),
    CONSTRAINT network_policy_activity_operation_uid CHECK (cloud_agents.is_valid_identifier(operation_uid)),
    CONSTRAINT network_policy_activity_action CHECK (action = 'network-policy.set'),
    CONSTRAINT network_policy_activity_key CHECK (idempotency_key ~ '^[A-Za-z0-9._~-]{16,128}$'),
    CONSTRAINT network_policy_activity_request_id CHECK (cloud_agents.is_valid_identifier(request_id)),
    CONSTRAINT network_policy_activity_request_digest CHECK (request_digest ~ '^sha256:[0-9a-f]{64}$'),
    CONSTRAINT network_policy_activity_subject_digest CHECK (subject_digest ~ '^sha256:[0-9a-f]{64}$'),
    CONSTRAINT network_policy_activity_version CHECK (policy_resource_version > 0),
    CONSTRAINT network_policy_activity_name CHECK (cloud_agents.is_valid_identifier(policy_name)),
    CONSTRAINT network_policy_activity_summary CHECK (
        pg_catalog.char_length(user_summary) BETWEEN 1 AND 256 AND user_summary !~ '[[:cntrl:]]'
    ),
    CONSTRAINT network_policy_activity_egress CHECK (default_egress IN ('public', 'restricted', 'deny')),
    CONSTRAINT network_policy_activity_allowlist CHECK (allowlist_policy_ref IS NULL OR cloud_agents.is_valid_identifier(allowlist_policy_ref)),
    CONSTRAINT network_policy_activity_dns_ref CHECK (
        dns_policy_ref IS NULL OR cloud_agents.is_valid_identifier(dns_policy_ref)
    ),
    CONSTRAINT network_policy_activity_proxy_ref CHECK (
        proxy_policy_ref IS NULL OR cloud_agents.is_valid_identifier(proxy_policy_ref)
    ),
    CONSTRAINT network_policy_activity_result CHECK (result = 'succeeded'),
    CONSTRAINT network_policy_activity_time CHECK (
        policy_updated_at >= policy_created_at AND occurred_at = policy_updated_at
    )
);

CREATE INDEX network_policies_page_idx
    ON cloud_agents.network_policies (tenant_id, project_uid, policy_uid);
CREATE INDEX network_policy_activity_audit_idx
    ON cloud_agents.network_policy_activity
    (tenant_id, project_uid, policy_uid, occurred_at DESC, event_uid DESC);

ALTER TABLE cloud_agents.network_policies OWNER TO cloud_agents_migration_owner;
ALTER TABLE cloud_agents.network_policy_activity OWNER TO cloud_agents_migration_owner;
ALTER TABLE cloud_agents.network_policies ENABLE ROW LEVEL SECURITY;
ALTER TABLE cloud_agents.network_policies FORCE ROW LEVEL SECURITY;
ALTER TABLE cloud_agents.network_policy_activity ENABLE ROW LEVEL SECURITY;
ALTER TABLE cloud_agents.network_policy_activity FORCE ROW LEVEL SECURITY;
CREATE POLICY network_policies_runtime_tenant ON cloud_agents.network_policies
    TO cloud_agents_runtime USING (tenant_id = cloud_agents.require_tenant_id())
    WITH CHECK (tenant_id = cloud_agents.require_tenant_id());
CREATE POLICY network_policies_migration_owner ON cloud_agents.network_policies
    TO cloud_agents_migration_owner USING (true) WITH CHECK (true);
CREATE POLICY network_policy_activity_runtime_tenant ON cloud_agents.network_policy_activity
    TO cloud_agents_runtime USING (tenant_id = cloud_agents.require_tenant_id())
    WITH CHECK (tenant_id = cloud_agents.require_tenant_id());
CREATE POLICY network_policy_activity_migration_owner ON cloud_agents.network_policy_activity
    TO cloud_agents_migration_owner USING (true) WITH CHECK (true);
REVOKE ALL ON TABLE cloud_agents.network_policies FROM PUBLIC;
REVOKE ALL ON TABLE cloud_agents.network_policy_activity FROM PUBLIC;
GRANT SELECT ON TABLE cloud_agents.network_policies TO cloud_agents_runtime;
GRANT SELECT ON TABLE cloud_agents.network_policy_activity TO cloud_agents_runtime;

CREATE FUNCTION cloud_agents.set_network_policy_v1(
    p_tenant_id text, p_project_uid text, p_policy_uid text, p_policy_name text,
    p_user_summary text, p_default_egress text, p_allowlist_policy_ref text,
    p_ingress_enabled boolean, p_preview_enabled boolean,
    p_dns_policy_ref text, p_proxy_policy_ref text,
    p_expected_resource_version bigint, p_idempotency_key text, p_request_digest text,
    p_request_id text, p_subject_digest text
)
RETURNS TABLE (
    policy_uid text, policy_name text, user_summary text, default_egress text,
    allowlist_policy_ref text, ingress_enabled boolean,
    preview_enabled boolean, dns_policy_ref text,
    proxy_policy_ref text,
    resource_version bigint, created_at timestamptz, updated_at timestamptz
)
LANGUAGE plpgsql SECURITY DEFINER SET search_path = pg_catalog, cloud_agents
AS $cloud_agents_function$
DECLARE
    ignored_principal text;
    mutation_at timestamptz;
    existing cloud_agents.network_policies%ROWTYPE;
    replay cloud_agents.network_policy_activity%ROWTYPE;
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
        OR p_default_egress IS NULL OR p_default_egress NOT IN ('public', 'restricted', 'deny')
        OR NULLIF(p_allowlist_policy_ref, '') IS NOT NULL AND NOT cloud_agents.is_valid_identifier(p_allowlist_policy_ref)
        OR p_ingress_enabled IS NULL OR p_preview_enabled IS NULL
        OR NULLIF(p_dns_policy_ref, '') IS NOT NULL
            AND NOT cloud_agents.is_valid_identifier(p_dns_policy_ref)
        OR NULLIF(p_proxy_policy_ref, '') IS NOT NULL
            AND NOT cloud_agents.is_valid_identifier(p_proxy_policy_ref)
        OR p_expected_resource_version < 0
        OR p_idempotency_key !~ '^[A-Za-z0-9._~-]{16,128}$'
        OR p_request_digest !~ '^sha256:[0-9a-f]{64}$'
        OR NOT cloud_agents.is_valid_identifier(p_request_id)
        OR p_subject_digest !~ '^sha256:[0-9a-f]{64}$'
    THEN RAISE EXCEPTION USING ERRCODE = '22023', MESSAGE = 'network policy input is invalid'; END IF;

    SELECT activity.* INTO replay
    FROM cloud_agents.network_policy_activity AS activity
    WHERE activity.tenant_id = p_tenant_id AND activity.project_uid = p_project_uid
        AND activity.idempotency_key = p_idempotency_key
    FOR SHARE;
    IF FOUND THEN
        IF replay.request_digest IS DISTINCT FROM p_request_digest THEN
            RAISE EXCEPTION USING ERRCODE = '23505', MESSAGE = 'network policy idempotency conflict';
        END IF;
        policy_uid := replay.policy_uid; policy_name := replay.policy_name;
        user_summary := replay.user_summary; default_egress := replay.default_egress;
        allowlist_policy_ref := COALESCE(replay.allowlist_policy_ref, '');
        ingress_enabled := replay.ingress_enabled;
        preview_enabled := replay.preview_enabled;
        dns_policy_ref := COALESCE(replay.dns_policy_ref, '');
        proxy_policy_ref := COALESCE(replay.proxy_policy_ref, '');
        resource_version := replay.policy_resource_version;
        created_at := replay.policy_created_at; updated_at := replay.policy_updated_at;
    ELSE
        PERFORM 1 FROM cloud_agents.projects AS project
        WHERE project.tenant_id = p_tenant_id AND project.project_uid = p_project_uid
            AND project.state = 'active'
        FOR KEY SHARE;
        IF NOT FOUND THEN RAISE EXCEPTION USING ERRCODE = '23503', MESSAGE = 'project is absent or inactive'; END IF;

        SELECT policy.* INTO existing
        FROM cloud_agents.network_policies AS policy
        WHERE policy.tenant_id = p_tenant_id AND policy.project_uid = p_project_uid
            AND policy.policy_uid = p_policy_uid
        FOR UPDATE;
        IF FOUND THEN
            IF existing.resource_version <> p_expected_resource_version THEN
                RAISE EXCEPTION USING ERRCODE = '23505', MESSAGE = 'network policy resource version conflict';
            END IF;
            PERFORM 1 FROM cloud_agents.environment_profiles AS profile
            WHERE profile.tenant_id = p_tenant_id AND profile.project_uid = p_project_uid
                AND profile.network_policy_ref = p_policy_uid
            LIMIT 1;
            IF FOUND THEN RAISE EXCEPTION USING ERRCODE = '23505', MESSAGE = 'network policy is referenced'; END IF;
            UPDATE cloud_agents.network_policies AS policy
            SET policy_name = p_policy_name, user_summary = p_user_summary,
                default_egress = p_default_egress,
                allowlist_policy_ref = NULLIF(p_allowlist_policy_ref, ''),
                ingress_enabled = p_ingress_enabled,
                preview_enabled = p_preview_enabled,
                dns_policy_ref = NULLIF(p_dns_policy_ref, ''),
                proxy_policy_ref = NULLIF(p_proxy_policy_ref, ''),
                resource_version = policy.resource_version + 1, updated_at = mutation_at
            WHERE policy.tenant_id = p_tenant_id AND policy.project_uid = p_project_uid
                AND policy.policy_uid = p_policy_uid
            RETURNING policy.* INTO existing;
        ELSE
            IF p_expected_resource_version <> 0 THEN
                RAISE EXCEPTION USING ERRCODE = '23505', MESSAGE = 'network policy resource version conflict';
            END IF;
            INSERT INTO cloud_agents.network_policies (
                tenant_id, project_uid, policy_uid, policy_name, user_summary,
                default_egress, allowlist_policy_ref, ingress_enabled,
                preview_enabled, dns_policy_ref, proxy_policy_ref,
                resource_version, created_at, updated_at
            ) VALUES (
                p_tenant_id, p_project_uid, p_policy_uid, p_policy_name, p_user_summary,
                p_default_egress, NULLIF(p_allowlist_policy_ref, ''), p_ingress_enabled,
                p_preview_enabled, NULLIF(p_dns_policy_ref, ''),
                NULLIF(p_proxy_policy_ref, ''),
                1, mutation_at, mutation_at
            ) RETURNING network_policies.* INTO existing;
        END IF;

        operation_uid_value := 'op-' || pg_catalog.md5(
            p_tenant_id || '|' || p_project_uid || '|' || p_policy_uid || '|network-policy.set|' || p_idempotency_key
        );
        INSERT INTO cloud_agents.network_policy_activity (
            tenant_id, project_uid, policy_uid, event_uid, operation_uid, action,
            idempotency_key, request_id, request_digest, subject_digest,
            policy_resource_version, policy_name, user_summary, default_egress,
            allowlist_policy_ref, ingress_enabled, preview_enabled,
            dns_policy_ref, proxy_policy_ref,
            policy_created_at, policy_updated_at, result, occurred_at
        ) VALUES (
            p_tenant_id, p_project_uid, p_policy_uid,
            operation_uid_value || '-succeeded', operation_uid_value, 'network-policy.set',
            p_idempotency_key, p_request_id, p_request_digest, p_subject_digest,
            existing.resource_version, existing.policy_name, existing.user_summary,
            existing.default_egress, existing.allowlist_policy_ref, existing.ingress_enabled,
            existing.preview_enabled, existing.dns_policy_ref,
            existing.proxy_policy_ref,
            existing.created_at, existing.updated_at, 'succeeded', mutation_at
        );

        policy_uid := existing.policy_uid; policy_name := existing.policy_name;
        user_summary := existing.user_summary; default_egress := existing.default_egress;
        allowlist_policy_ref := COALESCE(existing.allowlist_policy_ref, '');
        ingress_enabled := existing.ingress_enabled;
        preview_enabled := existing.preview_enabled;
        dns_policy_ref := COALESCE(existing.dns_policy_ref, '');
        proxy_policy_ref := COALESCE(existing.proxy_policy_ref, '');
        resource_version := existing.resource_version;
        created_at := existing.created_at; updated_at := existing.updated_at;
    END IF;
    RETURN NEXT;
END;
$cloud_agents_function$;

CREATE FUNCTION cloud_agents.create_environment_profile_draft_v4(
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
        PERFORM 1 FROM cloud_agents.network_policies AS policy
        WHERE policy.tenant_id = p_tenant_id AND policy.project_uid = p_project_uid
            AND policy.policy_uid = p_network_policy_ref
        FOR SHARE;
        IF NOT FOUND THEN
            RAISE EXCEPTION USING ERRCODE = '23503', MESSAGE = 'network policy is not available';
        END IF;
    END IF;
    RETURN QUERY SELECT * FROM cloud_agents.create_environment_profile_draft_v3(
        p_tenant_id, p_project_uid, p_profile_uid, p_profile_name, p_profile_version,
        p_description, p_provider_kinds_csv, p_cpu_limit_millis, p_memory_limit_bytes,
        p_storage_policy_ref, p_network_policy_ref, p_release_digest, p_target_refs_csv,
        p_provider_credential_ref, p_idempotency_key, p_request_digest, p_request_id, p_subject_digest
    );
END;
$cloud_agents_function$;

CREATE FUNCTION cloud_agents.transition_environment_profile_v4(
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
        SELECT profile.network_policy_ref INTO referenced_policy
        FROM cloud_agents.environment_profiles AS profile
        WHERE profile.tenant_id = p_tenant_id AND profile.project_uid = p_project_uid
            AND profile.profile_uid = p_profile_uid AND profile.profile_version = p_profile_version
        FOR SHARE;
        IF FOUND THEN
            PERFORM 1 FROM cloud_agents.network_policies AS policy
            WHERE policy.tenant_id = p_tenant_id AND policy.project_uid = p_project_uid
                AND policy.policy_uid = referenced_policy
                -- Fail closed until target adapters consume non-default policy semantics.
                AND policy.default_egress = 'public'
                AND policy.allowlist_policy_ref IS NULL AND policy.dns_policy_ref IS NULL
                AND policy.proxy_policy_ref IS NULL
                AND NOT policy.ingress_enabled AND NOT policy.preview_enabled
            FOR SHARE;
            IF NOT FOUND THEN
                RAISE EXCEPTION USING ERRCODE = '23503', MESSAGE = 'network policy is not available';
            END IF;
        END IF;
    END IF;
    RETURN QUERY SELECT * FROM cloud_agents.transition_environment_profile_v3(
        p_tenant_id, p_project_uid, p_profile_uid, p_profile_version,
        p_expected_resource_version, p_action, p_idempotency_key,
        p_request_digest, p_request_id, p_subject_digest
    );
END;
$cloud_agents_function$;

CREATE FUNCTION cloud_agents.create_user_environment_v6(
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
        JOIN cloud_agents.network_policies AS policy
          ON policy.tenant_id = profile.tenant_id AND policy.project_uid = profile.project_uid
         AND policy.policy_uid = profile.network_policy_ref
        WHERE profile.tenant_id = p_tenant_id AND profile.project_uid = p_project_uid
            AND profile.profile_uid = p_profile_uid AND profile.profile_version = p_profile_version
            AND profile.status = 'published'
            AND policy.default_egress = 'public'
            AND policy.allowlist_policy_ref IS NULL AND policy.dns_policy_ref IS NULL
            AND policy.proxy_policy_ref IS NULL
            AND NOT policy.ingress_enabled AND NOT policy.preview_enabled
        FOR SHARE OF profile, policy;
        IF NOT FOUND THEN
            RAISE EXCEPTION USING ERRCODE = '23505', MESSAGE = 'environment profile is not available';
        END IF;
    END IF;
    RETURN QUERY SELECT * FROM cloud_agents.create_user_environment_v5(
        p_tenant_id, p_project_uid, p_environment_uid, p_profile_uid,
        p_profile_version, p_ttl_seconds, p_idempotency_key, p_request_digest
    );
END;
$cloud_agents_function$;

ALTER FUNCTION cloud_agents.set_network_policy_v1(text, text, text, text, text, text, text, boolean, boolean, text, text, bigint, text, text, text, text)
    OWNER TO cloud_agents_migration_owner;
ALTER FUNCTION cloud_agents.create_environment_profile_draft_v4(text, text, text, text, bigint, text, text, bigint, bigint, text, text, text, text, text, text, text, text, text)
    OWNER TO cloud_agents_migration_owner;
ALTER FUNCTION cloud_agents.transition_environment_profile_v4(text, text, text, bigint, bigint, text, text, text, text, text)
    OWNER TO cloud_agents_migration_owner;
ALTER FUNCTION cloud_agents.create_user_environment_v6(text, text, text, text, bigint, bigint, text, text)
    OWNER TO cloud_agents_migration_owner;
REVOKE ALL ON FUNCTION cloud_agents.set_network_policy_v1(text, text, text, text, text, text, text, boolean, boolean, text, text, bigint, text, text, text, text)
    FROM PUBLIC;
REVOKE ALL ON FUNCTION cloud_agents.create_environment_profile_draft_v4(text, text, text, text, bigint, text, text, bigint, bigint, text, text, text, text, text, text, text, text, text)
    FROM PUBLIC;
REVOKE ALL ON FUNCTION cloud_agents.transition_environment_profile_v4(text, text, text, bigint, bigint, text, text, text, text, text)
    FROM PUBLIC;
REVOKE ALL ON FUNCTION cloud_agents.create_user_environment_v6(text, text, text, text, bigint, bigint, text, text)
    FROM PUBLIC;
REVOKE EXECUTE ON FUNCTION cloud_agents.create_environment_profile_draft_v3(text, text, text, text, bigint, text, text, bigint, bigint, text, text, text, text, text, text, text, text, text)
    FROM cloud_agents_runtime;
REVOKE EXECUTE ON FUNCTION cloud_agents.transition_environment_profile_v3(text, text, text, bigint, bigint, text, text, text, text, text)
    FROM cloud_agents_runtime;
REVOKE EXECUTE ON FUNCTION cloud_agents.create_user_environment_v5(text, text, text, text, bigint, bigint, text, text)
    FROM cloud_agents_runtime;
GRANT EXECUTE ON FUNCTION cloud_agents.set_network_policy_v1(text, text, text, text, text, text, text, boolean, boolean, text, text, bigint, text, text, text, text)
    TO cloud_agents_runtime;
GRANT EXECUTE ON FUNCTION cloud_agents.create_environment_profile_draft_v4(text, text, text, text, bigint, text, text, bigint, bigint, text, text, text, text, text, text, text, text, text)
    TO cloud_agents_runtime;
GRANT EXECUTE ON FUNCTION cloud_agents.transition_environment_profile_v4(text, text, text, bigint, bigint, text, text, text, text, text)
    TO cloud_agents_runtime;
GRANT EXECUTE ON FUNCTION cloud_agents.create_user_environment_v6(text, text, text, text, bigint, bigint, text, text)
    TO cloud_agents_runtime;
