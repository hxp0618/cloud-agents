ALTER TABLE cloud_agents_identity.audit_events
    DROP CONSTRAINT audit_events_event_kind_check;
ALTER TABLE cloud_agents_identity.audit_events
    ADD CONSTRAINT audit_events_event_kind_check
    CHECK (event_kind IN ('realm_initialized', 'password_login_succeeded', 'password_login_denied', 'session_revoked', 'token_issued', 'policy_updated'));
ALTER TABLE cloud_agents_identity.audit_events
    DROP CONSTRAINT audit_events_reason_code_check;
ALTER TABLE cloud_agents_identity.audit_events
    ADD CONSTRAINT audit_events_reason_code_check
    CHECK (reason_code IN ('initialized', 'authenticated', 'invalid_credentials', 'locked', 'logged_out', 'issued', 'policy_updated'));

CREATE FUNCTION cloud_agents_identity.email_policy_domains_valid(p_domains text[])
RETURNS boolean
LANGUAGE plpgsql IMMUTABLE SECURITY DEFINER
SET search_path = pg_catalog, cloud_agents_identity
AS $body$
DECLARE
    domain text;
    previous text;
BEGIN
    IF p_domains IS NULL OR cardinality(p_domains) > 64
        OR coalesce(array_ndims(p_domains), 1) <> 1
    THEN
        RETURN false;
    END IF;
    FOREACH domain IN ARRAY p_domains LOOP
        IF domain IS NULL OR octet_length(domain) NOT BETWEEN 1 AND 253
            OR domain !~ '^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?(\.[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?)*$'
            OR (previous IS NOT NULL AND previous >= domain)
        THEN
            RETURN false;
        END IF;
        previous := domain;
    END LOOP;
    RETURN true;
END
$body$;

CREATE TABLE cloud_agents_identity.tenant_email_policies (
    tenant_id text PRIMARY KEY,
    tenant_uid text NOT NULL,
    resource_version bigint NOT NULL CHECK (resource_version > 0),
    allowed_domains text[] NOT NULL,
    updated_by_user_id text NOT NULL REFERENCES cloud_agents_identity.users (id),
    updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    CONSTRAINT tenant_email_policies_tenant_root CHECK (tenant_id = tenant_uid),
    CONSTRAINT tenant_email_policies_tenant_fk
        FOREIGN KEY (tenant_id, tenant_uid)
        REFERENCES cloud_agents.platform_tenants (tenant_id, tenant_uid)
        ON UPDATE RESTRICT ON DELETE RESTRICT,
    CONSTRAINT tenant_email_policies_domains
        CHECK (cloud_agents_identity.email_policy_domains_valid(allowed_domains))
);

REVOKE ALL ON TABLE cloud_agents_identity.tenant_email_policies FROM PUBLIC;
REVOKE ALL ON TABLE cloud_agents_identity.tenant_email_policies FROM cloud_agents_identity_service;
REVOKE ALL ON FUNCTION cloud_agents_identity.email_policy_domains_valid(text[]) FROM PUBLIC;

CREATE FUNCTION cloud_agents_identity.require_tenant_admin(p_session_digest bytea, p_tenant_id text)
RETURNS TABLE (actor_user_id text, platform_admin boolean)
LANGUAGE plpgsql SECURITY DEFINER
SET search_path = pg_catalog, cloud_agents_identity
AS $body$
DECLARE
    account_id text;
    is_platform_admin boolean;
    realm_issuer text;
    account_subject text;
    account_digest text;
    operation_time timestamptz := clock_timestamp();
BEGIN
    PERFORM cloud_agents_identity.require_principal('cloud_agents_identity_service');
    IF p_session_digest IS NULL OR octet_length(p_session_digest) <> 32
        OR p_tenant_id IS NULL OR NOT cloud_agents.is_valid_identifier(p_tenant_id)
    THEN
        RAISE EXCEPTION USING ERRCODE = '22023', MESSAGE = 'invalid tenant administrator context';
    END IF;
    IF NOT EXISTS (
        SELECT 1 FROM cloud_agents.platform_tenants AS tenant
        WHERE tenant.tenant_id = p_tenant_id AND tenant.tenant_uid = p_tenant_id AND tenant.state = 'active'
    ) THEN
        RAISE EXCEPTION USING ERRCODE = '42501', MESSAGE = 'tenant administrator authority denied';
    END IF;
    SELECT session.user_id, session.platform_admin INTO account_id, is_platform_admin
        FROM cloud_agents_identity.read_session(p_session_digest, 'admin') AS session;
    IF account_id IS NULL THEN
        RAISE EXCEPTION USING ERRCODE = '28000', MESSAGE = 'identity session unavailable';
    END IF;
    IF is_platform_admin THEN
        RETURN QUERY SELECT account_id, true;
        RETURN;
    END IF;
    SELECT realm.issuer INTO STRICT realm_issuer
        FROM cloud_agents_identity.realm AS realm WHERE realm.singleton;
    account_subject := 'user-' || account_id;
    account_digest := cloud_agents.subject_ref_digest('user', realm_issuer, account_subject);
    IF NOT EXISTS (
        SELECT 1
        FROM cloud_agents.memberships AS membership
        JOIN cloud_agents.role_bindings AS binding
            ON binding.tenant_id = membership.tenant_id
            AND binding.subject_kind = membership.subject_kind
            AND binding.subject_issuer = membership.subject_issuer
            AND binding.subject_value = membership.subject_value
            AND binding.subject_digest = membership.subject_digest
        JOIN cloud_agents.builtin_roles AS role
            ON role.role_name = binding.role_name AND role.role_version = binding.role_version
        JOIN cloud_agents.resource_changes AS admission
            ON admission.tenant_id = membership.tenant_id
            AND admission.resource_kind = 'membership'
            AND admission.resource_uid = membership.membership_uid
            AND admission.change_kind = 'created'
            AND admission.resource_version < binding.resource_version
        WHERE membership.tenant_id = p_tenant_id
            AND membership.subject_kind = 'user'
            AND membership.subject_issuer = realm_issuer
            AND membership.subject_value = account_subject
            AND membership.subject_digest = account_digest
            AND membership.scope_level = 'tenant'
            AND membership.scope_tenant_uid = p_tenant_id
            AND membership.state = 'active'
            AND (membership.expires_at IS NULL OR membership.expires_at > operation_time)
            AND binding.role_name = 'tenant.admin' AND binding.role_version = 1
            AND binding.scope_level = 'tenant' AND binding.scope_tenant_uid = p_tenant_id
            AND binding.state = 'active'
            AND (binding.expires_at IS NULL OR binding.expires_at > operation_time)
            AND role.scope_level = 'tenant' AND role.state = 'active'
    ) THEN
        RAISE EXCEPTION USING ERRCODE = '42501', MESSAGE = 'tenant administrator authority denied';
    END IF;
    RETURN QUERY SELECT account_id, false;
END
$body$;

CREATE FUNCTION cloud_agents_identity.read_email_suffix_policy(p_session_digest bytea, p_tenant_id text)
RETURNS TABLE (resource_version bigint, allowed_domains text[])
LANGUAGE plpgsql SECURITY DEFINER
SET search_path = pg_catalog, cloud_agents_identity
AS $body$
BEGIN
    PERFORM 1 FROM cloud_agents_identity.require_tenant_admin(p_session_digest, p_tenant_id);
    RETURN QUERY
        SELECT policy.resource_version, policy.allowed_domains
        FROM cloud_agents_identity.tenant_email_policies AS policy
        WHERE policy.tenant_id = p_tenant_id;
    IF NOT FOUND THEN
        RETURN QUERY SELECT 0::bigint, ARRAY[]::text[];
    END IF;
END
$body$;

CREATE FUNCTION cloud_agents_identity.update_email_suffix_policy(
    p_session_digest bytea, p_tenant_id text, p_expected_resource_version bigint,
    p_allowed_domains text[], p_event_id text, p_correlation_id text
) RETURNS TABLE (resource_version bigint, allowed_domains text[])
LANGUAGE plpgsql SECURITY DEFINER
SET search_path = pg_catalog, cloud_agents_identity
AS $body$
DECLARE
    actor_id text;
    next_version bigint;
BEGIN
    IF p_expected_resource_version IS NULL OR p_expected_resource_version < 0
        OR NOT cloud_agents_identity.email_policy_domains_valid(p_allowed_domains)
        OR p_event_id IS NULL OR p_event_id !~ '^[a-f0-9]{32}$'
        OR p_correlation_id IS NULL OR NOT cloud_agents.is_valid_identifier(p_correlation_id)
    THEN
        RAISE EXCEPTION USING ERRCODE = '22023', MESSAGE = 'invalid email suffix policy update';
    END IF;
    SELECT authority.actor_user_id INTO STRICT actor_id
        FROM cloud_agents_identity.require_tenant_admin(p_session_digest, p_tenant_id) AS authority;
    next_version := p_expected_resource_version + 1;
    IF p_expected_resource_version = 0 THEN
        BEGIN
            INSERT INTO cloud_agents_identity.tenant_email_policies (
                tenant_id, tenant_uid, resource_version, allowed_domains, updated_by_user_id
            ) VALUES (p_tenant_id, p_tenant_id, next_version, p_allowed_domains, actor_id);
        EXCEPTION WHEN unique_violation THEN
            RAISE EXCEPTION USING ERRCODE = '40001', MESSAGE = 'email suffix policy version conflict';
        END;
    ELSE
        UPDATE cloud_agents_identity.tenant_email_policies AS policy
        SET resource_version = next_version, allowed_domains = p_allowed_domains,
            updated_by_user_id = actor_id, updated_at = clock_timestamp()
        WHERE policy.tenant_id = p_tenant_id
            AND policy.resource_version = p_expected_resource_version;
        IF NOT FOUND THEN
            RAISE EXCEPTION USING ERRCODE = '40001', MESSAGE = 'email suffix policy version conflict';
        END IF;
    END IF;
    INSERT INTO cloud_agents_identity.audit_events (
        id, event_kind, user_id, actor_user_id, tenant_id,
        application, decision, reason_code, correlation_id
    ) VALUES (
        p_event_id, 'policy_updated', actor_id, actor_id, p_tenant_id,
        'admin', 'allow', 'policy_updated', p_correlation_id
    );
    RETURN QUERY SELECT next_version, p_allowed_domains;
END
$body$;

REVOKE ALL ON FUNCTION cloud_agents_identity.require_tenant_admin(bytea, text) FROM PUBLIC;
REVOKE ALL ON FUNCTION cloud_agents_identity.read_email_suffix_policy(bytea, text) FROM PUBLIC;
REVOKE ALL ON FUNCTION cloud_agents_identity.update_email_suffix_policy(bytea, text, bigint, text[], text, text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION cloud_agents_identity.read_email_suffix_policy(bytea, text) TO cloud_agents_identity_service;
GRANT EXECUTE ON FUNCTION cloud_agents_identity.update_email_suffix_policy(bytea, text, bigint, text[], text, text) TO cloud_agents_identity_service;
