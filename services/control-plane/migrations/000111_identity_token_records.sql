ALTER TABLE cloud_agents_identity.audit_events
    DROP CONSTRAINT audit_events_event_kind_check;
ALTER TABLE cloud_agents_identity.audit_events
    ADD CONSTRAINT audit_events_event_kind_check
    CHECK (event_kind IN ('realm_initialized', 'password_login_succeeded', 'password_login_denied', 'session_revoked', 'token_issued'));
ALTER TABLE cloud_agents_identity.audit_events
    DROP CONSTRAINT audit_events_reason_code_check;
ALTER TABLE cloud_agents_identity.audit_events
    ADD CONSTRAINT audit_events_reason_code_check
    CHECK (reason_code IN ('initialized', 'authenticated', 'invalid_credentials', 'locked', 'logged_out', 'issued'));

CREATE TABLE cloud_agents_identity.issued_tokens (
    token_sha256 bytea PRIMARY KEY CHECK (octet_length(token_sha256) = 32),
    jti text NOT NULL UNIQUE CHECK (jti ~ '^[A-Za-z0-9_-]{22}$'),
    session_digest bytea NOT NULL REFERENCES cloud_agents_identity.sessions (digest),
    user_id text NOT NULL REFERENCES cloud_agents_identity.users (id),
    issuer text NOT NULL CHECK (length(issuer) BETWEEN 1 AND 512),
    application text NOT NULL CHECK (application IN ('admin', 'user')),
    tenant_id text NOT NULL CHECK (tenant_id ~ '^[A-Za-z0-9]([A-Za-z0-9._~-]{0,126}[A-Za-z0-9])?$'),
    project_id text CHECK (project_id ~ '^[A-Za-z0-9]([A-Za-z0-9._~-]{0,126}[A-Za-z0-9])?$'),
    issued_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    expires_at timestamptz NOT NULL,
    CHECK (expires_at > issued_at AND expires_at <= issued_at + interval '15 minutes')
);

CREATE INDEX issued_tokens_expiry_idx
    ON cloud_agents_identity.issued_tokens (expires_at, token_sha256);
CREATE INDEX issued_tokens_session_idx
    ON cloud_agents_identity.issued_tokens (session_digest, expires_at);

REVOKE ALL ON TABLE cloud_agents_identity.issued_tokens FROM PUBLIC;
REVOKE ALL ON TABLE cloud_agents_identity.issued_tokens FROM cloud_agents_identity_service;

CREATE FUNCTION cloud_agents_identity.record_issued_token(
    p_session_digest bytea,
    p_user_id text,
    p_issuer text,
    p_application text,
    p_tenant_id text,
    p_project_id text,
    p_jti text,
    p_token_sha256 bytea,
    p_expires_at timestamptz,
    p_event_id text,
    p_correlation_id text
) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER
SET search_path = pg_catalog, cloud_agents_identity
AS $body$
DECLARE
    operation_time timestamptz := clock_timestamp();
    account_id text;
BEGIN
    PERFORM cloud_agents_identity.require_principal('cloud_agents_identity_service');
    IF p_session_digest IS NULL OR octet_length(p_session_digest) <> 32
        OR p_user_id IS NULL OR NOT cloud_agents.is_valid_identifier(p_user_id)
        OR p_issuer IS NULL OR length(p_issuer) NOT BETWEEN 1 AND 512
        OR p_application IS NULL OR p_application NOT IN ('admin', 'user')
        OR p_tenant_id IS NULL OR NOT cloud_agents.is_valid_identifier(p_tenant_id)
        OR (p_project_id IS NOT NULL AND NOT cloud_agents.is_valid_identifier(p_project_id))
        OR p_jti IS NULL OR p_jti !~ '^[A-Za-z0-9_-]{22}$'
        OR p_token_sha256 IS NULL OR octet_length(p_token_sha256) <> 32
        OR p_expires_at IS NULL OR p_expires_at <= operation_time
        OR p_expires_at > operation_time + interval '15 minutes'
        OR p_event_id IS NULL OR p_event_id !~ '^[a-f0-9]{32}$'
        OR p_correlation_id IS NULL OR NOT cloud_agents.is_valid_identifier(p_correlation_id)
    THEN
        RAISE EXCEPTION USING ERRCODE = '22023', MESSAGE = 'invalid issued token input';
    END IF;

    IF NOT EXISTS (
        SELECT 1
        FROM cloud_agents.platform_tenants AS tenant
        WHERE tenant.tenant_id = p_tenant_id
            AND tenant.tenant_uid = p_tenant_id
            AND tenant.state = 'active'
    ) OR (p_project_id IS NOT NULL AND NOT EXISTS (
        SELECT 1
        FROM cloud_agents.projects AS project
        JOIN cloud_agents.organizations AS organization
            ON organization.tenant_id = project.tenant_id
            AND organization.organization_uid = project.organization_uid
        WHERE project.tenant_id = p_tenant_id
            AND project.project_uid = p_project_id
            AND organization.state = 'active'
            AND project.state = 'active'
    )) THEN
        RETURN false;
    END IF;

    UPDATE cloud_agents_identity.sessions AS session
    SET last_seen_at = operation_time
    FROM cloud_agents_identity.users AS account, cloud_agents_identity.realm AS realm
    WHERE session.digest = p_session_digest
        AND session.user_id = p_user_id
        AND session.application = p_application
        AND session.user_id = account.id
        AND realm.singleton
        AND realm.issuer = p_issuer
        AND session.revoked_at IS NULL
        AND account.disabled_at IS NULL
        AND session.expires_at > operation_time
        AND session.last_seen_at > operation_time - interval '30 minutes'
    RETURNING session.user_id INTO account_id;
    IF account_id IS NULL THEN
        RETURN false;
    END IF;

    WITH expired AS (
        SELECT issued.token_sha256
        FROM cloud_agents_identity.issued_tokens AS issued
        WHERE issued.expires_at <= operation_time
        ORDER BY issued.expires_at, issued.token_sha256
        LIMIT 128
        FOR UPDATE SKIP LOCKED
    )
    DELETE FROM cloud_agents_identity.issued_tokens AS issued
    USING expired
    WHERE issued.token_sha256 = expired.token_sha256;

    INSERT INTO cloud_agents_identity.issued_tokens (
        token_sha256, jti, session_digest, user_id, issuer,
        application, tenant_id, project_id, issued_at, expires_at
    ) VALUES (
        p_token_sha256, p_jti, p_session_digest, p_user_id, p_issuer,
        p_application, p_tenant_id, p_project_id, operation_time, p_expires_at
    );
    INSERT INTO cloud_agents_identity.audit_events (
        id, event_kind, user_id, actor_user_id, target_user_id,
        tenant_id, application, decision, reason_code, correlation_id
    ) VALUES (
        p_event_id, 'token_issued', p_user_id, p_user_id, p_user_id,
        p_tenant_id, p_application, 'allow', 'issued', p_correlation_id
    );
    RETURN true;
END
$body$;

CREATE FUNCTION cloud_agents_identity.token_is_active(
    p_token_sha256 bytea,
    p_expected_application text,
    p_expected_tenant_id text,
    p_expected_project_id text
) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER
SET search_path = pg_catalog, cloud_agents_identity
AS $body$
DECLARE
    operation_time timestamptz := clock_timestamp();
BEGIN
    PERFORM cloud_agents_identity.require_principal('cloud_agents_identity_service');
    IF p_token_sha256 IS NULL OR octet_length(p_token_sha256) <> 32
        OR p_expected_application IS NULL OR p_expected_application NOT IN ('admin', 'user')
        OR p_expected_tenant_id IS NULL OR NOT cloud_agents.is_valid_identifier(p_expected_tenant_id)
        OR (p_expected_project_id IS NOT NULL AND NOT cloud_agents.is_valid_identifier(p_expected_project_id))
    THEN
        RAISE EXCEPTION USING ERRCODE = '22023', MESSAGE = 'invalid token status input';
    END IF;
    RETURN EXISTS (
        SELECT 1
        FROM cloud_agents_identity.issued_tokens AS issued
        JOIN cloud_agents_identity.sessions AS session
            ON session.digest = issued.session_digest
            AND session.user_id = issued.user_id
            AND session.application = issued.application
        JOIN cloud_agents_identity.users AS account ON account.id = issued.user_id
        JOIN cloud_agents_identity.realm AS realm ON realm.singleton AND realm.issuer = issued.issuer
        WHERE issued.token_sha256 = p_token_sha256
            AND issued.application = p_expected_application
            AND issued.tenant_id = p_expected_tenant_id
            AND (issued.project_id IS NULL OR issued.project_id = p_expected_project_id)
            AND issued.expires_at > operation_time
            AND session.revoked_at IS NULL
            AND session.expires_at > operation_time
            AND session.last_seen_at > operation_time - interval '30 minutes'
            AND account.disabled_at IS NULL
    );
END
$body$;

REVOKE ALL ON FUNCTION cloud_agents_identity.record_issued_token(bytea, text, text, text, text, text, text, bytea, timestamptz, text, text) FROM PUBLIC;
REVOKE ALL ON FUNCTION cloud_agents_identity.token_is_active(bytea, text, text, text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION cloud_agents_identity.record_issued_token(bytea, text, text, text, text, text, text, bytea, timestamptz, text, text) TO cloud_agents_identity_service;
GRANT EXECUTE ON FUNCTION cloud_agents_identity.token_is_active(bytea, text, text, text) TO cloud_agents_identity_service;
