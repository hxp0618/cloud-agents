CREATE INDEX memberships_identity_subject_idx
    ON cloud_agents.memberships (subject_digest, subject_kind, subject_issuer, subject_value, tenant_id);

CREATE FUNCTION cloud_agents_identity.list_session_tenants(
    p_digest bytea, p_application text, p_after text, p_limit integer
) RETURNS TABLE (tenant_id text, tenant_name text, tenant_admin boolean)
LANGUAGE plpgsql SECURITY DEFINER
SET search_path = pg_catalog, cloud_agents_identity
AS $body$
DECLARE
    account_id text;
    is_platform_admin boolean;
    realm_issuer text;
    account_subject text;
    account_digest text;
    operation_time timestamptz;
BEGIN
    PERFORM cloud_agents_identity.require_principal('cloud_agents_identity_service');
    IF p_application IS NULL OR p_application NOT IN ('admin', 'user')
        OR p_limit IS NULL OR p_limit < 1 OR p_limit > 201
        OR p_after IS NULL OR (p_after <> '' AND NOT cloud_agents.is_valid_identifier(p_after))
    THEN
        RAISE EXCEPTION USING ERRCODE = '22023', MESSAGE = 'invalid tenant page';
    END IF;
    SELECT s.user_id, s.platform_admin INTO account_id, is_platform_admin
        FROM cloud_agents_identity.read_session(p_digest, p_application) AS s;
    IF account_id IS NULL THEN
        RAISE EXCEPTION USING ERRCODE = '28000', MESSAGE = 'identity session unavailable';
    END IF;
    SELECT r.issuer INTO STRICT realm_issuer FROM cloud_agents_identity.realm AS r WHERE r.singleton;
    account_subject := 'user-' || account_id;
    account_digest := cloud_agents.subject_ref_digest('user', realm_issuer, account_subject);
    operation_time := clock_timestamp();
    IF p_application = 'admin' AND is_platform_admin THEN
        RETURN QUERY SELECT t.tenant_id, t.display_name, false
            FROM cloud_agents.platform_tenants AS t
            WHERE t.tenant_id = t.tenant_uid AND t.state = 'active' AND t.tenant_id > p_after
            ORDER BY t.tenant_id LIMIT p_limit;
        RETURN;
    END IF;
    RETURN QUERY
        SELECT t.tenant_id, t.display_name, p_application = 'admin'
        FROM cloud_agents.platform_tenants AS t
        WHERE t.tenant_id = t.tenant_uid AND t.state = 'active' AND t.tenant_id > p_after
            AND EXISTS (
                SELECT 1 FROM cloud_agents.memberships AS m
                WHERE m.tenant_id = t.tenant_id AND m.subject_kind = 'user'
                    AND m.subject_issuer = realm_issuer AND m.subject_value = account_subject
                    AND m.subject_digest = account_digest
                    AND m.state = 'active' AND (m.expires_at IS NULL OR m.expires_at > operation_time)
                    AND (p_application = 'user' OR (m.scope_level = 'tenant' AND EXISTS (
                    SELECT 1 FROM cloud_agents.role_bindings AS b
                    JOIN cloud_agents.builtin_roles AS r
                        ON r.role_name = b.role_name AND r.role_version = b.role_version
                    JOIN cloud_agents.resource_changes AS admission
                        ON admission.tenant_id = m.tenant_id AND admission.resource_kind = 'membership'
                        AND admission.resource_uid = m.membership_uid AND admission.change_kind = 'created'
                        AND admission.resource_version < b.resource_version
                    WHERE b.tenant_id = m.tenant_id AND b.subject_kind = m.subject_kind
                        AND b.subject_issuer = m.subject_issuer AND b.subject_value = m.subject_value
                        AND b.subject_digest = m.subject_digest
                        AND b.role_name = 'tenant.admin' AND b.role_version = 1 AND r.state = 'active'
                        AND b.scope_level = 'tenant' AND b.scope_tenant_uid = t.tenant_id
                        AND b.state = 'active' AND (b.expires_at IS NULL OR b.expires_at > operation_time)
                    )))
            )
        ORDER BY t.tenant_id
        LIMIT p_limit;
END
$body$;

REVOKE ALL ON FUNCTION cloud_agents_identity.list_session_tenants(bytea, text, text, integer) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION cloud_agents_identity.list_session_tenants(bytea, text, text, integer) TO cloud_agents_identity_service;
