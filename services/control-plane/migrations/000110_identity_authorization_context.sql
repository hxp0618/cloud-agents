CREATE FUNCTION cloud_agents_identity.read_platform_admin(p_kind text, p_issuer text, p_subject text)
RETURNS TABLE (user_id text, subject_kind text, subject_issuer text, subject_value text, subject_digest text)
LANGUAGE plpgsql SECURITY DEFINER
SET search_path = pg_catalog, cloud_agents_identity
AS $body$
BEGIN
    PERFORM cloud_agents_identity.require_principal('cloud_agents_runtime');
    RETURN QUERY
        SELECT u.id, 'user'::text, r.issuer, 'user-' || u.id,
            cloud_agents.subject_ref_digest('user', r.issuer, 'user-' || u.id)
        FROM cloud_agents_identity.platform_admins AS a
        JOIN cloud_agents_identity.users AS u ON u.id = a.user_id
        CROSS JOIN cloud_agents_identity.realm AS r
        WHERE p_kind = 'user' AND r.singleton AND r.issuer = p_issuer AND p_subject = 'user-' || u.id
            AND a.revoked_at IS NULL AND u.disabled_at IS NULL
            AND EXISTS (SELECT 1 FROM cloud_agents.platform_tenants AS t
                WHERE t.tenant_id = cloud_agents.require_tenant_id() AND t.tenant_uid = t.tenant_id AND t.state = 'active');
END
$body$;

CREATE FUNCTION cloud_agents_identity.read_session_subject(p_digest bytea, p_application text)
RETURNS TABLE (user_id text, subject_kind text, subject_issuer text, subject_value text, subject_digest text)
LANGUAGE plpgsql SECURITY DEFINER
SET search_path = pg_catalog, cloud_agents_identity
AS $body$
DECLARE
    operation_time timestamptz := clock_timestamp();
BEGIN
    PERFORM cloud_agents_identity.require_principal('cloud_agents_runtime');
    IF p_application IS NULL OR p_application NOT IN ('admin', 'user')
        OR p_digest IS NULL OR octet_length(p_digest) <> 32
    THEN
        RAISE EXCEPTION USING ERRCODE = '22023', MESSAGE = 'invalid identity session context';
    END IF;
    RETURN QUERY
        SELECT u.id, 'user'::text, r.issuer, 'user-' || u.id,
            cloud_agents.subject_ref_digest('user', r.issuer, 'user-' || u.id)
        FROM cloud_agents_identity.sessions AS s
        JOIN cloud_agents_identity.users AS u ON u.id = s.user_id
        CROSS JOIN cloud_agents_identity.realm AS r
        WHERE s.digest = p_digest AND s.application = p_application AND r.singleton
            AND s.revoked_at IS NULL AND u.disabled_at IS NULL
            AND s.expires_at > operation_time AND s.last_seen_at > operation_time - interval '30 minutes'
            AND EXISTS (SELECT 1 FROM cloud_agents.platform_tenants AS t
                WHERE t.tenant_id = cloud_agents.require_tenant_id() AND t.tenant_uid = t.tenant_id AND t.state = 'active');
END
$body$;

REVOKE ALL ON FUNCTION cloud_agents_identity.read_platform_admin(text, text, text) FROM PUBLIC;
REVOKE ALL ON FUNCTION cloud_agents_identity.read_session_subject(bytea, text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION cloud_agents_identity.read_platform_admin(text, text, text) TO cloud_agents_runtime;
GRANT EXECUTE ON FUNCTION cloud_agents_identity.read_session_subject(bytea, text) TO cloud_agents_runtime;
