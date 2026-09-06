CREATE FUNCTION cloud_agents.is_valid_network_egress_target_v1(value text)
RETURNS boolean
LANGUAGE plpgsql IMMUTABLE PARALLEL SAFE SET search_path = pg_catalog
AS $$
BEGIN
    IF value IS NULL OR pg_catalog.char_length(value) NOT BETWEEN 1 AND 253 OR value <> pg_catalog.lower(value)
        OR value ~ '[[:space:][:cntrl:]]' THEN
        RETURN false;
    END IF;
    IF value ~ '^([*][.])?([a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?[.])+[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$' THEN
        RETURN true;
    END IF;
    BEGIN
        IF pg_catalog.strpos(value, '/') > 0 THEN
            RETURN value = (value::pg_catalog.cidr)::text;
        END IF;
        RETURN value = pg_catalog.host(value::pg_catalog.inet);
    EXCEPTION WHEN invalid_text_representation THEN
        RETURN false;
    END;
END;
$$;

CREATE FUNCTION cloud_agents.is_valid_network_egress_list_v1(p_values text[], default_egress text, allowlist_ref text)
RETURNS boolean
LANGUAGE plpgsql IMMUTABLE PARALLEL SAFE SET search_path = pg_catalog, cloud_agents
AS $$
DECLARE target text; previous text;
BEGIN
    IF p_values IS NULL OR pg_catalog.cardinality(p_values) > 64
        OR default_egress NOT IN ('public', 'restricted', 'deny') THEN
        RETURN false;
    END IF;
    FOREACH target IN ARRAY p_values LOOP
        IF NOT cloud_agents.is_valid_network_egress_target_v1(target)
            OR previous IS NOT NULL AND target <= previous THEN
            RETURN false;
        END IF;
        previous := target;
    END LOOP;
    RETURN (default_egress = 'restricted' OR pg_catalog.cardinality(p_values) = 0)
        AND (default_egress <> 'restricted' OR pg_catalog.cardinality(p_values) > 0 OR allowlist_ref IS NOT NULL);
END;
$$;

ALTER TABLE cloud_agents.network_policies
    ADD COLUMN allowed_egress text[] NOT NULL DEFAULT ARRAY[]::text[];
ALTER TABLE cloud_agents.network_policies
    ADD CONSTRAINT network_policies_allowed_egress CHECK (
        cloud_agents.is_valid_network_egress_list_v1(allowed_egress, default_egress, allowlist_policy_ref)
    );

ALTER TABLE cloud_agents.network_policy_activity
    ADD COLUMN allowed_egress text[] NOT NULL DEFAULT ARRAY[]::text[];

ALTER TABLE cloud_agents.runtime_profiles ADD COLUMN network_policy_ref text;
ALTER TABLE cloud_agents.runtime_profiles
    ADD CONSTRAINT runtime_profiles_network_policy_ref CHECK (
        network_policy_ref IS NULL OR cloud_agents.is_valid_identifier(network_policy_ref)
    );
ALTER TABLE cloud_agents.runtime_profiles
    ADD CONSTRAINT runtime_profiles_network_policy_fk
    FOREIGN KEY (tenant_id, project_uid, network_policy_ref)
    REFERENCES cloud_agents.network_policies (tenant_id, project_uid, policy_uid)
    ON UPDATE RESTRICT ON DELETE RESTRICT;
CREATE INDEX runtime_profiles_network_policy_idx ON cloud_agents.runtime_profiles
    (tenant_id, project_uid, network_policy_ref);

CREATE FUNCTION cloud_agents.set_network_policy_v2(
    p_tenant_id text, p_project_uid text, p_policy_uid text, p_policy_name text,
    p_user_summary text, p_default_egress text, p_allowed_egress text[], p_allowlist_policy_ref text,
    p_ingress_enabled boolean, p_preview_enabled boolean,
    p_dns_policy_ref text, p_proxy_policy_ref text,
    p_expected_resource_version bigint, p_idempotency_key text, p_request_digest text,
    p_request_id text, p_subject_digest text
)
RETURNS TABLE (
    policy_uid text, policy_name text, user_summary text, default_egress text,
    allowed_egress text[], allowlist_policy_ref text, ingress_enabled boolean,
    preview_enabled boolean, dns_policy_ref text, proxy_policy_ref text,
    resource_version bigint, created_at timestamptz, updated_at timestamptz
)
LANGUAGE plpgsql SECURITY DEFINER SET search_path = pg_catalog, cloud_agents
AS $$
DECLARE
    replay cloud_agents.network_policy_activity%ROWTYPE;
    written record;
    delegated_allowlist text;
BEGIN
    PERFORM cloud_agents.require_runtime_mutation_principal();
    IF NOT cloud_agents.is_valid_network_egress_list_v1(
        p_allowed_egress, p_default_egress, NULLIF(p_allowlist_policy_ref, ''))
    THEN RAISE EXCEPTION USING ERRCODE = '22023', MESSAGE = 'network policy input is invalid'; END IF;

    SELECT activity.* INTO replay FROM cloud_agents.network_policy_activity AS activity
    WHERE activity.tenant_id = p_tenant_id AND activity.project_uid = p_project_uid
        AND activity.idempotency_key = p_idempotency_key FOR SHARE;
    IF FOUND THEN
        IF replay.request_digest IS DISTINCT FROM p_request_digest THEN
            RAISE EXCEPTION USING ERRCODE = '23505', MESSAGE = 'network policy idempotency conflict';
        END IF;
        RETURN QUERY SELECT replay.policy_uid, replay.policy_name, replay.user_summary,
            replay.default_egress, replay.allowed_egress, COALESCE(replay.allowlist_policy_ref, ''),
            replay.ingress_enabled, replay.preview_enabled, COALESCE(replay.dns_policy_ref, ''),
            COALESCE(replay.proxy_policy_ref, ''), replay.policy_resource_version,
            replay.policy_created_at, replay.policy_updated_at;
        RETURN;
    END IF;

    PERFORM 1 FROM cloud_agents.runtime_profiles AS profile
    WHERE profile.tenant_id = p_tenant_id AND profile.project_uid = p_project_uid
        AND profile.network_policy_ref = p_policy_uid LIMIT 1;
    IF FOUND THEN RAISE EXCEPTION USING ERRCODE = '23505', MESSAGE = 'network policy is referenced'; END IF;

    PERFORM 1 FROM cloud_agents.environment_profiles AS profile
    WHERE profile.tenant_id = p_tenant_id AND profile.project_uid = p_project_uid
        AND profile.network_policy_ref = p_policy_uid LIMIT 1;
    IF FOUND THEN RAISE EXCEPTION USING ERRCODE = '23505', MESSAGE = 'network policy is referenced'; END IF;

    UPDATE cloud_agents.network_policies AS policy
    SET default_egress = 'public', allowed_egress = ARRAY[]::text[], allowlist_policy_ref = NULL
    WHERE policy.tenant_id = p_tenant_id AND policy.project_uid = p_project_uid
        AND policy.policy_uid = p_policy_uid;

    delegated_allowlist := p_allowlist_policy_ref;
    IF p_default_egress = 'restricted' AND pg_catalog.cardinality(p_allowed_egress) > 0
        AND NULLIF(p_allowlist_policy_ref, '') IS NULL THEN
        delegated_allowlist := p_policy_uid;
    END IF;

    SELECT * INTO written FROM cloud_agents.set_network_policy_v1(
        p_tenant_id, p_project_uid, p_policy_uid, p_policy_name, p_user_summary,
        p_default_egress, delegated_allowlist, p_ingress_enabled, p_preview_enabled,
        p_dns_policy_ref, p_proxy_policy_ref, p_expected_resource_version,
        p_idempotency_key, p_request_digest, p_request_id, p_subject_digest
    );
    UPDATE cloud_agents.network_policies AS policy
    SET allowed_egress = p_allowed_egress, allowlist_policy_ref = NULLIF(p_allowlist_policy_ref, '')
    WHERE policy.tenant_id = p_tenant_id AND policy.project_uid = p_project_uid
        AND policy.policy_uid = p_policy_uid AND policy.resource_version = written.resource_version;
    IF NOT FOUND THEN RAISE EXCEPTION USING ERRCODE = '40001', MESSAGE = 'network policy authority drifted'; END IF;
    UPDATE cloud_agents.network_policy_activity AS activity
    SET allowed_egress = p_allowed_egress, allowlist_policy_ref = NULLIF(p_allowlist_policy_ref, '')
    WHERE activity.tenant_id = p_tenant_id AND activity.project_uid = p_project_uid
        AND activity.idempotency_key = p_idempotency_key;
    IF NOT FOUND THEN RAISE EXCEPTION USING ERRCODE = '40001', MESSAGE = 'network policy audit drifted'; END IF;
    RETURN QUERY SELECT policy.policy_uid, policy.policy_name, policy.user_summary,
        policy.default_egress, policy.allowed_egress, COALESCE(policy.allowlist_policy_ref, ''),
        policy.ingress_enabled, policy.preview_enabled, COALESCE(policy.dns_policy_ref, ''),
        COALESCE(policy.proxy_policy_ref, ''), policy.resource_version, policy.created_at, policy.updated_at
    FROM cloud_agents.network_policies AS policy
    WHERE policy.tenant_id = p_tenant_id AND policy.project_uid = p_project_uid
        AND policy.policy_uid = p_policy_uid;
END;
$$;

CREATE FUNCTION cloud_agents.create_runtime_profile_draft_v2(
    p_tenant text, p_project text, p_profile text, p_name text, p_version bigint,
    p_description text, p_target text, p_network_policy text, p_image text, p_release text,
    p_cpu bigint, p_memory bigint, p_key text, p_digest text, p_request text, p_subject text
)
RETURNS TABLE (
    profile_version_uid text, profile_uid text, profile_name text, profile_version bigint,
    description text, status text, target_uid text, network_policy_ref text,
    image_uri text, release_digest text, cpu_millis bigint, memory_bytes bigint,
    resource_version bigint, created_at timestamptz, updated_at timestamptz,
    published_at timestamptz, disabled_at timestamptz
)
LANGUAGE plpgsql SECURITY DEFINER SET search_path = pg_catalog, cloud_agents
AS $$
DECLARE written record;
BEGIN
    PERFORM cloud_agents.require_runtime_mutation_principal();
    IF NOT cloud_agents.is_valid_identifier(p_network_policy) THEN
        RAISE EXCEPTION USING ERRCODE = '22023', MESSAGE = 'runtime profile input is invalid';
    END IF;
    PERFORM 1 FROM cloud_agents.network_policies AS policy
    WHERE policy.tenant_id = p_tenant AND policy.project_uid = p_project
        AND policy.policy_uid = p_network_policy FOR KEY SHARE;
    IF NOT FOUND THEN RAISE EXCEPTION USING ERRCODE = '23503', MESSAGE = 'runtime profile network policy unavailable'; END IF;
    SELECT * INTO written FROM cloud_agents.create_runtime_profile_draft_v1(
        p_tenant, p_project, p_profile, p_name, p_version, p_description, p_target,
        p_image, p_release, p_cpu, p_memory, p_key, p_digest, p_request, p_subject
    );
    UPDATE cloud_agents.runtime_profiles AS profile SET network_policy_ref = p_network_policy
    WHERE profile.tenant_id = p_tenant AND profile.project_uid = p_project
        AND profile.profile_uid = p_profile AND profile.profile_version = p_version
        AND (profile.network_policy_ref IS NULL OR profile.network_policy_ref = p_network_policy);
    IF NOT FOUND THEN RAISE EXCEPTION USING ERRCODE = '23505', MESSAGE = 'runtime profile network policy conflict'; END IF;
    RETURN QUERY SELECT profile.profile_version_uid, profile.profile_uid, profile.profile_name,
        profile.profile_version, profile.description, profile.status, profile.target_uid,
        profile.network_policy_ref, profile.image_uri, profile.release_digest, profile.cpu_millis,
        profile.memory_bytes, profile.resource_version, profile.created_at, profile.updated_at,
        profile.published_at, profile.disabled_at
    FROM cloud_agents.runtime_profiles AS profile
    WHERE profile.tenant_id = p_tenant AND profile.project_uid = p_project
        AND profile.profile_uid = p_profile AND profile.profile_version = p_version;
END;
$$;

CREATE FUNCTION cloud_agents.transition_runtime_profile_v2(
    p_tenant text, p_project text, p_profile text, p_version bigint,
    p_expected_resource_version bigint, p_action text, p_key text,
    p_digest text, p_request text, p_subject text
)
RETURNS TABLE (
    profile_version_uid text, profile_uid text, profile_name text, profile_version bigint,
    description text, status text, target_uid text, network_policy_ref text,
    image_uri text, release_digest text, cpu_millis bigint, memory_bytes bigint,
    resource_version bigint, created_at timestamptz, updated_at timestamptz,
    published_at timestamptz, disabled_at timestamptz
)
LANGUAGE plpgsql SECURITY DEFINER SET search_path = pg_catalog, cloud_agents
AS $$
DECLARE transitioned record;
BEGIN
    PERFORM cloud_agents.require_runtime_mutation_principal();
    IF p_action = 'publish' THEN
        PERFORM 1 FROM cloud_agents.runtime_profiles AS profile
        JOIN cloud_agents.network_policies AS policy
          ON policy.tenant_id = profile.tenant_id AND policy.project_uid = profile.project_uid
         AND policy.policy_uid = profile.network_policy_ref
        WHERE profile.tenant_id = p_tenant AND profile.project_uid = p_project
            AND profile.profile_uid = p_profile AND profile.profile_version = p_version
            AND policy.default_egress IN ('restricted', 'deny')
            AND (policy.default_egress = 'restricted' AND pg_catalog.cardinality(policy.allowed_egress) > 0
                OR policy.default_egress = 'deny' AND pg_catalog.cardinality(policy.allowed_egress) = 0)
            AND policy.allowlist_policy_ref IS NULL AND policy.dns_policy_ref IS NULL
            AND policy.proxy_policy_ref IS NULL AND NOT policy.ingress_enabled
        FOR KEY SHARE OF profile, policy;
        IF NOT FOUND THEN RAISE EXCEPTION USING ERRCODE = '23503', MESSAGE = 'runtime profile network policy unavailable'; END IF;
    END IF;
    SELECT * INTO transitioned FROM cloud_agents.transition_runtime_profile_v1(
        p_tenant, p_project, p_profile, p_version, p_expected_resource_version,
        p_action, p_key, p_digest, p_request, p_subject
    );
    RETURN QUERY SELECT profile.profile_version_uid, profile.profile_uid, profile.profile_name,
        profile.profile_version, profile.description, profile.status, profile.target_uid,
        COALESCE(profile.network_policy_ref, ''), profile.image_uri, profile.release_digest,
        profile.cpu_millis, profile.memory_bytes, profile.resource_version, profile.created_at,
        profile.updated_at, profile.published_at, profile.disabled_at
    FROM cloud_agents.runtime_profiles AS profile
    WHERE profile.tenant_id = p_tenant AND profile.project_uid = p_project
        AND profile.profile_uid = p_profile AND profile.profile_version = p_version;
END;
$$;

CREATE FUNCTION cloud_agents.accept_foundation_sandbox_v3(
    p_project text, p_workspace text, p_workspace_name text, p_sandbox text,
    p_profile text, p_profile_version bigint, p_subject text, p_key text, p_digest text,
    p_ttl_seconds integer
)
RETURNS TABLE (
    operation_uid text, workspace_uid text, sandbox_uid text, profile_uid text,
    profile_version bigint, generation bigint, desired_state text, observed_state text,
    ttl_seconds integer, expires_at timestamptz
)
LANGUAGE plpgsql SECURITY DEFINER SET search_path = pg_catalog, cloud_agents
AS $$
DECLARE prior_digest text; accepted record;
BEGIN
    PERFORM cloud_agents.require_runtime_mutation_principal();
    SELECT record.request_digest INTO prior_digest FROM cloud_agents.idempotency_records AS record
    WHERE record.tenant_id = cloud_agents.require_tenant_id() AND record.subject_digest = p_subject
        AND record.profile_id = 'foundationSandboxLifecycle/v1alpha1'
        AND record.profile_digest = 'sha256:aabd6aa244c89a4850eba529dbe387132c3a0aff7ffba430f6115afb6e215a1b'
        AND record.idempotency_key = p_key;
    IF FOUND THEN
        IF prior_digest IS DISTINCT FROM p_digest THEN
            RAISE EXCEPTION USING ERRCODE = '23505', MESSAGE = 'foundation sandbox idempotency conflict';
        END IF;
    ELSE
        PERFORM 1 FROM cloud_agents.runtime_profiles AS profile
        JOIN cloud_agents.network_policies AS policy
          ON policy.tenant_id = profile.tenant_id AND policy.project_uid = profile.project_uid
         AND policy.policy_uid = profile.network_policy_ref
        WHERE profile.tenant_id = cloud_agents.require_tenant_id() AND profile.project_uid = p_project
            AND profile.profile_uid = p_profile AND profile.profile_version = p_profile_version
            AND profile.status = 'published' AND policy.default_egress IN ('restricted', 'deny')
            AND (policy.default_egress = 'restricted' AND pg_catalog.cardinality(policy.allowed_egress) > 0
                OR policy.default_egress = 'deny' AND pg_catalog.cardinality(policy.allowed_egress) = 0)
            AND policy.allowlist_policy_ref IS NULL AND policy.dns_policy_ref IS NULL
            AND policy.proxy_policy_ref IS NULL AND NOT policy.ingress_enabled
        FOR KEY SHARE OF profile, policy;
        IF NOT FOUND THEN RAISE EXCEPTION USING ERRCODE = '23505', MESSAGE = 'runtime profile is not available'; END IF;
    END IF;
    SELECT * INTO accepted FROM cloud_agents.accept_foundation_sandbox_v2(
        p_project, p_workspace, p_workspace_name, p_sandbox, p_profile, p_profile_version,
        p_subject, p_key, p_digest, p_ttl_seconds
    );
    RETURN QUERY SELECT accepted.operation_uid, accepted.workspace_uid, accepted.sandbox_uid,
        accepted.profile_uid, accepted.profile_version, accepted.generation, accepted.desired_state,
        accepted.observed_state, accepted.ttl_seconds, accepted.expires_at;
END;
$$;

CREATE FUNCTION cloud_agents.transition_foundation_sandbox_v3(
    p_project text, p_sandbox text, p_expected_generation bigint, p_expected_resource_version bigint,
    p_action text, p_confirmed_sandbox text, p_compute_disposition text, p_workspace_disposition text,
    p_subject text, p_key text, p_digest text, p_request text
)
RETURNS TABLE (
    operation_uid text, idempotency_key text, action text, sandbox_uid text,
    sandbox_generation bigint, requested_by text, request_id text, requested_at timestamptz,
    updated_at timestamptz, operation_state text, cleanup_phase text, stable_error_code text,
    compute_disposition text, workspace_disposition text
)
LANGUAGE plpgsql SECURITY DEFINER SET search_path = pg_catalog, cloud_agents
AS $$
DECLARE prior_operation text; transitioned record;
BEGIN
    PERFORM cloud_agents.require_runtime_mutation_principal();
    SELECT record.operation_id INTO prior_operation FROM cloud_agents.idempotency_records AS record
    WHERE record.tenant_id = cloud_agents.require_tenant_id() AND record.subject_digest = p_subject
        AND record.profile_id = 'foundationSandboxLifecycle/v1alpha1'
        AND record.profile_digest = 'sha256:aabd6aa244c89a4850eba529dbe387132c3a0aff7ffba430f6115afb6e215a1b'
        AND record.idempotency_key = p_key AND record.request_digest = p_digest;
    IF NOT FOUND AND p_action = 'rebuild' THEN
        PERFORM 1 FROM cloud_agents.sandbox_sessions AS sandbox
        JOIN cloud_agents.runtime_profiles AS profile
          ON profile.tenant_id = sandbox.tenant_id AND profile.project_uid = sandbox.project_uid
         AND profile.profile_uid = sandbox.runtime_profile_uid AND profile.profile_version = sandbox.runtime_profile_version
        JOIN cloud_agents.network_policies AS policy
          ON policy.tenant_id = profile.tenant_id AND policy.project_uid = profile.project_uid
         AND policy.policy_uid = profile.network_policy_ref
        WHERE sandbox.tenant_id = cloud_agents.require_tenant_id() AND sandbox.project_uid = p_project
            AND sandbox.sandbox_uid = p_sandbox AND policy.default_egress IN ('restricted', 'deny')
            AND (policy.default_egress = 'restricted' AND pg_catalog.cardinality(policy.allowed_egress) > 0
                OR policy.default_egress = 'deny' AND pg_catalog.cardinality(policy.allowed_egress) = 0)
            AND policy.allowlist_policy_ref IS NULL AND policy.dns_policy_ref IS NULL
            AND policy.proxy_policy_ref IS NULL AND NOT policy.ingress_enabled
        FOR KEY SHARE OF sandbox, profile, policy;
        IF NOT FOUND THEN RAISE EXCEPTION USING ERRCODE = '23503', MESSAGE = 'runtime profile network policy unavailable'; END IF;
    END IF;
    SELECT * INTO transitioned FROM cloud_agents.transition_foundation_sandbox_v2(
        p_project, p_sandbox, p_expected_generation, p_expected_resource_version, p_action,
        p_confirmed_sandbox, p_compute_disposition, p_workspace_disposition, p_subject,
        p_key, p_digest, p_request
    );
    RETURN QUERY SELECT transitioned.operation_uid, transitioned.idempotency_key, transitioned.action,
        transitioned.sandbox_uid, transitioned.sandbox_generation, transitioned.requested_by,
        transitioned.request_id, transitioned.requested_at, transitioned.updated_at,
        transitioned.operation_state, transitioned.cleanup_phase, transitioned.stable_error_code,
        transitioned.compute_disposition, transitioned.workspace_disposition;
END;
$$;

CREATE FUNCTION cloud_agents.claim_foundation_sandbox_v4(
    p_holder text, p_incarnation text, p_token text, p_lease_seconds integer,
    p_subject text, p_audit text
)
RETURNS TABLE (
    tenant_id text, event_id text, delivery_attempts integer, claim_expires_at timestamptz,
    operation_id text, operation_generation bigint, action text,
    project_uid text, workspace_uid text, workspace_name text, volume_uid text,
    physical_volume_uid text, target_uid text, target_generation bigint, target_endpoint text,
    credential_ref text, sandbox_uid text, sandbox_generation bigint, image_uri text,
    runtime_profile_uid text, runtime_profile_version bigint, cpu_millis bigint, memory_bytes bigint,
    spec_digest text, runtime_uid text, runtime_state text, runtime_operation_uid text,
    runtime_generation bigint, runtime_spec_digest text, ttl_seconds integer, expires_at timestamptz,
    network_policy_uid text, network_default_egress text, network_allowed_egress text[],
    network_preview_enabled boolean
)
LANGUAGE plpgsql SECURITY DEFINER SET search_path = pg_catalog, cloud_agents
AS $$
DECLARE claimed record;
BEGIN
    SELECT * INTO claimed FROM cloud_agents.claim_foundation_sandbox_v3(
        p_holder, p_incarnation, p_token, p_lease_seconds, p_subject, p_audit
    );
    IF NOT FOUND THEN RETURN; END IF;
    RETURN QUERY SELECT claimed.tenant_id, claimed.event_id, claimed.delivery_attempts,
        claimed.claim_expires_at, claimed.operation_id, claimed.operation_generation, claimed.action,
        claimed.project_uid, claimed.workspace_uid, claimed.workspace_name, claimed.volume_uid,
        claimed.physical_volume_uid, claimed.target_uid, claimed.target_generation,
        claimed.target_endpoint, claimed.credential_ref, claimed.sandbox_uid,
        claimed.sandbox_generation, claimed.image_uri, claimed.runtime_profile_uid,
        claimed.runtime_profile_version, claimed.cpu_millis, claimed.memory_bytes,
        claimed.spec_digest, claimed.runtime_uid, claimed.runtime_state,
        claimed.runtime_operation_uid, claimed.runtime_generation, claimed.runtime_spec_digest,
        claimed.ttl_seconds, claimed.expires_at, COALESCE(profile.network_policy_ref, ''),
        COALESCE(policy.default_egress, ''), COALESCE(policy.allowed_egress, ARRAY[]::text[]),
        COALESCE(policy.preview_enabled, false)
    FROM cloud_agents.runtime_profiles AS profile
    LEFT JOIN cloud_agents.network_policies AS policy
      ON policy.tenant_id = profile.tenant_id AND policy.project_uid = profile.project_uid
     AND policy.policy_uid = profile.network_policy_ref
    WHERE profile.tenant_id = claimed.tenant_id AND profile.project_uid = claimed.project_uid
        AND profile.profile_uid = claimed.runtime_profile_uid
        AND profile.profile_version = claimed.runtime_profile_version;
    IF NOT FOUND THEN RAISE EXCEPTION USING ERRCODE = '40001', MESSAGE = 'foundation network policy claim drifted'; END IF;
END;
$$;

CREATE FUNCTION cloud_agents.register_sandbox_preview_port_v2(
    p_project text, p_grant text, p_port integer, p_token_digest text
)
RETURNS TABLE (port integer, sandbox_uid text, sandbox_generation bigint, registered_at timestamptz)
LANGUAGE plpgsql SECURITY DEFINER SET search_path = pg_catalog, cloud_agents
AS $$
BEGIN
    PERFORM cloud_agents.require_runtime_mutation_principal();
    PERFORM 1 FROM cloud_agents.sandbox_access_grants AS access_grant
    JOIN cloud_agents.sandbox_sessions AS sandbox
      ON sandbox.tenant_id = access_grant.tenant_id AND sandbox.project_uid = access_grant.project_uid
     AND sandbox.sandbox_uid = access_grant.sandbox_uid
    JOIN cloud_agents.runtime_profiles AS profile
      ON profile.tenant_id = sandbox.tenant_id AND profile.project_uid = sandbox.project_uid
     AND profile.profile_uid = sandbox.runtime_profile_uid AND profile.profile_version = sandbox.runtime_profile_version
    JOIN cloud_agents.network_policies AS policy
      ON policy.tenant_id = profile.tenant_id AND policy.project_uid = profile.project_uid
     AND policy.policy_uid = profile.network_policy_ref
    WHERE access_grant.tenant_id = cloud_agents.require_tenant_id()
        AND access_grant.project_uid = p_project AND access_grant.grant_uid = p_grant
        AND access_grant.token_digest = p_token_digest AND policy.preview_enabled;
    IF NOT FOUND THEN RAISE EXCEPTION USING ERRCODE = '28000', MESSAGE = 'sandbox Preview is disabled by network policy'; END IF;
    RETURN QUERY SELECT * FROM cloud_agents.register_sandbox_preview_port_v1(
        p_project, p_grant, p_port, p_token_digest
    );
END;
$$;

ALTER FUNCTION cloud_agents.is_valid_network_egress_target_v1(text) OWNER TO cloud_agents_migration_owner;
ALTER FUNCTION cloud_agents.is_valid_network_egress_list_v1(text[],text,text) OWNER TO cloud_agents_migration_owner;
ALTER FUNCTION cloud_agents.set_network_policy_v2(text,text,text,text,text,text,text[],text,boolean,boolean,text,text,bigint,text,text,text,text) OWNER TO cloud_agents_migration_owner;
ALTER FUNCTION cloud_agents.create_runtime_profile_draft_v2(text,text,text,text,bigint,text,text,text,text,text,bigint,bigint,text,text,text,text) OWNER TO cloud_agents_migration_owner;
ALTER FUNCTION cloud_agents.transition_runtime_profile_v2(text,text,text,bigint,bigint,text,text,text,text,text) OWNER TO cloud_agents_migration_owner;
ALTER FUNCTION cloud_agents.accept_foundation_sandbox_v3(text,text,text,text,text,bigint,text,text,text,integer) OWNER TO cloud_agents_migration_owner;
ALTER FUNCTION cloud_agents.transition_foundation_sandbox_v3(text,text,bigint,bigint,text,text,text,text,text,text,text,text) OWNER TO cloud_agents_migration_owner;
ALTER FUNCTION cloud_agents.claim_foundation_sandbox_v4(text,text,text,integer,text,text) OWNER TO cloud_agents_migration_owner;
ALTER FUNCTION cloud_agents.register_sandbox_preview_port_v2(text,text,integer,text) OWNER TO cloud_agents_migration_owner;

REVOKE ALL ON FUNCTION cloud_agents.is_valid_network_egress_target_v1(text) FROM PUBLIC;
REVOKE ALL ON FUNCTION cloud_agents.is_valid_network_egress_list_v1(text[],text,text) FROM PUBLIC;
REVOKE ALL ON FUNCTION cloud_agents.set_network_policy_v2(text,text,text,text,text,text,text[],text,boolean,boolean,text,text,bigint,text,text,text,text) FROM PUBLIC;
REVOKE ALL ON FUNCTION cloud_agents.create_runtime_profile_draft_v2(text,text,text,text,bigint,text,text,text,text,text,bigint,bigint,text,text,text,text) FROM PUBLIC;
REVOKE ALL ON FUNCTION cloud_agents.transition_runtime_profile_v2(text,text,text,bigint,bigint,text,text,text,text,text) FROM PUBLIC;
REVOKE ALL ON FUNCTION cloud_agents.accept_foundation_sandbox_v3(text,text,text,text,text,bigint,text,text,text,integer) FROM PUBLIC;
REVOKE ALL ON FUNCTION cloud_agents.transition_foundation_sandbox_v3(text,text,bigint,bigint,text,text,text,text,text,text,text,text) FROM PUBLIC;
REVOKE ALL ON FUNCTION cloud_agents.claim_foundation_sandbox_v4(text,text,text,integer,text,text) FROM PUBLIC;
REVOKE ALL ON FUNCTION cloud_agents.register_sandbox_preview_port_v2(text,text,integer,text) FROM PUBLIC;

REVOKE EXECUTE ON FUNCTION cloud_agents.set_network_policy_v1(text,text,text,text,text,text,text,boolean,boolean,text,text,bigint,text,text,text,text) FROM cloud_agents_runtime;
REVOKE EXECUTE ON FUNCTION cloud_agents.create_runtime_profile_draft_v1(text,text,text,text,bigint,text,text,text,text,bigint,bigint,text,text,text,text) FROM cloud_agents_runtime;
REVOKE EXECUTE ON FUNCTION cloud_agents.transition_runtime_profile_v1(text,text,text,bigint,bigint,text,text,text,text,text) FROM cloud_agents_runtime;
REVOKE EXECUTE ON FUNCTION cloud_agents.accept_foundation_sandbox_v2(text,text,text,text,text,bigint,text,text,text,integer) FROM cloud_agents_runtime;
REVOKE EXECUTE ON FUNCTION cloud_agents.transition_foundation_sandbox_v2(text,text,bigint,bigint,text,text,text,text,text,text,text,text) FROM cloud_agents_runtime;
REVOKE EXECUTE ON FUNCTION cloud_agents.claim_foundation_sandbox_v3(text,text,text,integer,text,text) FROM cloud_agents_runtime;
REVOKE EXECUTE ON FUNCTION cloud_agents.register_sandbox_preview_port_v1(text,text,integer,text) FROM cloud_agents_runtime;

GRANT EXECUTE ON FUNCTION cloud_agents.set_network_policy_v2(text,text,text,text,text,text,text[],text,boolean,boolean,text,text,bigint,text,text,text,text) TO cloud_agents_runtime;
GRANT EXECUTE ON FUNCTION cloud_agents.create_runtime_profile_draft_v2(text,text,text,text,bigint,text,text,text,text,text,bigint,bigint,text,text,text,text) TO cloud_agents_runtime;
GRANT EXECUTE ON FUNCTION cloud_agents.transition_runtime_profile_v2(text,text,text,bigint,bigint,text,text,text,text,text) TO cloud_agents_runtime;
GRANT EXECUTE ON FUNCTION cloud_agents.accept_foundation_sandbox_v3(text,text,text,text,text,bigint,text,text,text,integer) TO cloud_agents_runtime;
GRANT EXECUTE ON FUNCTION cloud_agents.transition_foundation_sandbox_v3(text,text,bigint,bigint,text,text,text,text,text,text,text,text) TO cloud_agents_runtime;
GRANT EXECUTE ON FUNCTION cloud_agents.claim_foundation_sandbox_v4(text,text,text,integer,text,text) TO cloud_agents_runtime;
GRANT EXECUTE ON FUNCTION cloud_agents.register_sandbox_preview_port_v2(text,text,integer,text) TO cloud_agents_runtime;
