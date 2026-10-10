ALTER TABLE cloud_agents_identity.audit_events
    DROP CONSTRAINT audit_events_event_kind_check;
ALTER TABLE cloud_agents_identity.audit_events
    ADD CONSTRAINT audit_events_event_kind_check
    CHECK (event_kind IN ('realm_initialized', 'password_login_succeeded', 'password_login_denied', 'session_revoked', 'token_issued', 'policy_updated', 'invitation_created', 'invitation_revoked', 'invitation_accepted', 'account_disabled', 'password_reset_issued', 'password_reset_accepted', 'password_changed', 'password_change_denied'));
ALTER TABLE cloud_agents_identity.audit_events
    DROP CONSTRAINT audit_events_reason_code_check;
ALTER TABLE cloud_agents_identity.audit_events
    ADD CONSTRAINT audit_events_reason_code_check
    CHECK (reason_code IN ('initialized', 'authenticated', 'invalid_credentials', 'locked', 'logged_out', 'issued', 'policy_updated', 'invitation_created', 'invitation_revoked', 'invitation_accepted', 'account_disabled', 'password_reset_issued', 'password_reset_accepted', 'password_changed'));

CREATE TABLE cloud_agents_identity.password_reset_proofs (
    digest bytea PRIMARY KEY CHECK (octet_length(digest) = 32),
    user_id text NOT NULL REFERENCES cloud_agents_identity.users (id),
    issued_by_user_id text NOT NULL REFERENCES cloud_agents_identity.users (id),
    attempts integer NOT NULL DEFAULT 0 CHECK (attempts BETWEEN 0 AND 10),
    created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    expires_at timestamptz NOT NULL,
    consumed_at timestamptz,
    revoked_at timestamptz,
    CHECK (expires_at > created_at AND expires_at <= created_at + interval '30 minutes'),
    CHECK (consumed_at IS NULL OR revoked_at IS NULL),
    CHECK (consumed_at IS NULL OR consumed_at >= created_at),
    CHECK (revoked_at IS NULL OR revoked_at >= created_at)
);

CREATE TABLE cloud_agents_identity.password_reset_failures (
    ip_bucket bytea PRIMARY KEY CHECK (octet_length(ip_bucket) = 32),
    window_started_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    failures integer NOT NULL DEFAULT 0 CHECK (failures BETWEEN 0 AND 100),
    locked_until timestamptz
);

CREATE TABLE cloud_agents_identity.password_reauth_failures (
    session_digest bytea PRIMARY KEY REFERENCES cloud_agents_identity.sessions (digest),
    window_started_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    failures integer NOT NULL DEFAULT 0 CHECK (failures BETWEEN 0 AND 10),
    locked_until timestamptz
);

CREATE INDEX password_reset_proofs_expiry_idx
    ON cloud_agents_identity.password_reset_proofs (expires_at, digest);

ALTER TABLE cloud_agents_identity.password_reset_proofs OWNER TO cloud_agents_migration_owner;
ALTER TABLE cloud_agents_identity.password_reset_failures OWNER TO cloud_agents_migration_owner;
ALTER TABLE cloud_agents_identity.password_reauth_failures OWNER TO cloud_agents_migration_owner;
REVOKE ALL ON TABLE cloud_agents_identity.password_reset_proofs FROM PUBLIC;
REVOKE ALL ON TABLE cloud_agents_identity.password_reset_proofs FROM cloud_agents_identity_service;
REVOKE ALL ON TABLE cloud_agents_identity.password_reset_failures FROM PUBLIC;
REVOKE ALL ON TABLE cloud_agents_identity.password_reset_failures FROM cloud_agents_identity_service;
REVOKE ALL ON TABLE cloud_agents_identity.password_reauth_failures FROM PUBLIC;
REVOKE ALL ON TABLE cloud_agents_identity.password_reauth_failures FROM cloud_agents_identity_service;

CREATE FUNCTION cloud_agents_identity.require_platform_admin(p_session_digest bytea)
RETURNS text
LANGUAGE plpgsql SECURITY DEFINER
SET search_path = pg_catalog, cloud_agents_identity
AS $body$
DECLARE
    actor_id text;
    is_platform_admin boolean;
BEGIN
    PERFORM cloud_agents_identity.require_principal('cloud_agents_identity_service');
    IF p_session_digest IS NULL OR octet_length(p_session_digest) <> 32 THEN
        RAISE EXCEPTION USING ERRCODE = '22023', MESSAGE = 'invalid platform administrator context';
    END IF;
    SELECT session.user_id, session.platform_admin INTO actor_id, is_platform_admin
        FROM cloud_agents_identity.read_session(p_session_digest, 'admin') AS session;
    IF actor_id IS NULL THEN
        RAISE EXCEPTION USING ERRCODE = '28000', MESSAGE = 'identity session unavailable';
    END IF;
    IF NOT is_platform_admin THEN
        RAISE EXCEPTION USING ERRCODE = '42501', MESSAGE = 'platform administrator authority denied';
    END IF;
    RETURN actor_id;
END
$body$;

CREATE FUNCTION cloud_agents_identity.list_accounts(
    p_session_digest bytea, p_after text, p_limit integer
) RETURNS TABLE (
    id text, subject_issuer text, email text, display_name text, state text,
    platform_admin boolean, email_verified_at timestamptz, created_at timestamptz
)
LANGUAGE plpgsql SECURITY DEFINER
SET search_path = pg_catalog, cloud_agents_identity
AS $body$
BEGIN
    PERFORM cloud_agents_identity.require_platform_admin(p_session_digest);
    IF p_after IS NULL OR (p_after <> '' AND NOT cloud_agents.is_valid_identifier(p_after))
        OR p_limit IS NULL OR p_limit NOT BETWEEN 1 AND 201
    THEN
        RAISE EXCEPTION USING ERRCODE = '22023', MESSAGE = 'invalid account page';
    END IF;
    RETURN QUERY
        SELECT account.id, realm.issuer, account.email, account.display_name,
            CASE WHEN account.disabled_at IS NULL THEN 'active' ELSE 'disabled' END,
            EXISTS (
                SELECT 1 FROM cloud_agents_identity.platform_admins AS admin
                WHERE admin.user_id = account.id AND admin.revoked_at IS NULL
            ),
            account.email_verified_at, account.created_at
        FROM cloud_agents_identity.users AS account
        CROSS JOIN cloud_agents_identity.realm AS realm
        WHERE realm.singleton AND (p_after = '' OR account.id > p_after)
        ORDER BY account.id
        LIMIT p_limit;
END
$body$;

CREATE FUNCTION cloud_agents_identity.list_tenant_accounts(
    p_session_digest bytea, p_tenant_id text, p_after text, p_limit integer
) RETURNS TABLE (
    id text, subject_issuer text, email text, display_name text, state text,
    platform_admin boolean, email_verified_at timestamptz, created_at timestamptz
)
LANGUAGE plpgsql SECURITY DEFINER
SET search_path = pg_catalog, cloud_agents_identity
AS $body$
DECLARE
    realm_issuer text;
BEGIN
    PERFORM 1 FROM cloud_agents_identity.require_tenant_admin(p_session_digest, p_tenant_id);
    IF p_after IS NULL OR (p_after <> '' AND NOT cloud_agents.is_valid_identifier(p_after))
        OR p_limit IS NULL OR p_limit NOT BETWEEN 1 AND 201
    THEN
        RAISE EXCEPTION USING ERRCODE = '22023', MESSAGE = 'invalid tenant account page';
    END IF;
    SELECT realm.issuer INTO STRICT realm_issuer
        FROM cloud_agents_identity.realm AS realm WHERE realm.singleton;
    RETURN QUERY
        SELECT account.id, realm_issuer, account.email, account.display_name,
            CASE WHEN account.disabled_at IS NULL THEN 'active' ELSE 'disabled' END,
            EXISTS (
                SELECT 1 FROM cloud_agents_identity.platform_admins AS admin
                WHERE admin.user_id = account.id AND admin.revoked_at IS NULL
            ),
            account.email_verified_at, account.created_at
        FROM cloud_agents_identity.users AS account
        WHERE (p_after = '' OR account.id > p_after)
            AND EXISTS (
                SELECT 1 FROM cloud_agents.memberships AS membership
                WHERE membership.tenant_id = p_tenant_id
                    AND membership.subject_kind = 'user'
                    AND membership.subject_issuer = realm_issuer
                    AND membership.subject_value = 'user-' || account.id
                    AND membership.subject_digest = cloud_agents.subject_ref_digest('user', realm_issuer, 'user-' || account.id)
                    AND membership.state IN ('active', 'suspended')
            )
        ORDER BY account.id
        LIMIT p_limit;
END
$body$;

CREATE FUNCTION cloud_agents_identity.disable_account(
    p_session_digest bytea, p_target_user_id text, p_event_id text, p_correlation_id text
) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER
SET search_path = pg_catalog, cloud_agents_identity
AS $body$
DECLARE
    actor_id text;
    target_disabled timestamptz;
    target_is_admin boolean;
BEGIN
    IF p_target_user_id IS NULL OR NOT cloud_agents.is_valid_identifier(p_target_user_id)
        OR p_event_id IS NULL OR p_event_id !~ '^[a-f0-9]{32}$'
        OR p_correlation_id IS NULL OR NOT cloud_agents.is_valid_identifier(p_correlation_id)
    THEN
        RAISE EXCEPTION USING ERRCODE = '22023', MESSAGE = 'invalid account disable request';
    END IF;
    PERFORM 1 FROM cloud_agents_identity.realm WHERE singleton FOR UPDATE;
    actor_id := cloud_agents_identity.require_platform_admin(p_session_digest);
    SELECT account.disabled_at,
        EXISTS (SELECT 1 FROM cloud_agents_identity.platform_admins AS admin
            WHERE admin.user_id = account.id AND admin.revoked_at IS NULL)
        INTO target_disabled, target_is_admin
        FROM cloud_agents_identity.users AS account
        WHERE account.id = p_target_user_id
        FOR UPDATE;
    IF NOT FOUND THEN
        RAISE EXCEPTION USING ERRCODE = 'P0002', MESSAGE = 'identity account unavailable';
    END IF;
    IF target_disabled IS NOT NULL THEN
        RETURN true;
    END IF;
    IF target_is_admin AND (
        SELECT count(*)
        FROM cloud_agents_identity.platform_admins AS admin
        JOIN cloud_agents_identity.users AS account ON account.id = admin.user_id
        WHERE admin.revoked_at IS NULL AND account.disabled_at IS NULL
    ) <= 1 THEN
        RAISE EXCEPTION USING ERRCODE = '40001', MESSAGE = 'last active platform administrator cannot be disabled';
    END IF;
    UPDATE cloud_agents_identity.users SET disabled_at = clock_timestamp()
        WHERE id = p_target_user_id;
    UPDATE cloud_agents_identity.sessions SET revoked_at = clock_timestamp()
        WHERE user_id = p_target_user_id AND revoked_at IS NULL;
    UPDATE cloud_agents_identity.password_reset_proofs SET revoked_at = clock_timestamp()
        WHERE user_id = p_target_user_id AND consumed_at IS NULL AND revoked_at IS NULL;
    INSERT INTO cloud_agents_identity.audit_events (
        id, event_kind, user_id, actor_user_id, target_user_id,
        application, decision, reason_code, correlation_id
    ) VALUES (
        p_event_id, 'account_disabled', p_target_user_id, actor_id, p_target_user_id,
        'admin', 'allow', 'account_disabled', p_correlation_id
    );
    RETURN true;
END
$body$;

CREATE FUNCTION cloud_agents_identity.issue_password_reset(
    p_session_digest bytea, p_target_user_id text, p_reset_digest bytea,
    p_event_id text, p_correlation_id text
) RETURNS timestamptz
LANGUAGE plpgsql SECURITY DEFINER
SET search_path = pg_catalog, cloud_agents_identity
AS $body$
DECLARE
    actor_id text;
    reset_expiry timestamptz := clock_timestamp() + interval '30 minutes';
BEGIN
    IF p_target_user_id IS NULL OR NOT cloud_agents.is_valid_identifier(p_target_user_id)
        OR p_reset_digest IS NULL OR octet_length(p_reset_digest) <> 32
        OR p_event_id IS NULL OR p_event_id !~ '^[a-f0-9]{32}$'
        OR p_correlation_id IS NULL OR NOT cloud_agents.is_valid_identifier(p_correlation_id)
    THEN
        RAISE EXCEPTION USING ERRCODE = '22023', MESSAGE = 'invalid password reset request';
    END IF;
    actor_id := cloud_agents_identity.require_platform_admin(p_session_digest);
    PERFORM 1 FROM cloud_agents_identity.users AS account
        WHERE account.id = p_target_user_id AND account.disabled_at IS NULL FOR UPDATE;
    IF NOT FOUND THEN
        RAISE EXCEPTION USING ERRCODE = 'P0002', MESSAGE = 'identity account unavailable';
    END IF;
    UPDATE cloud_agents_identity.password_reset_proofs SET revoked_at = clock_timestamp()
        WHERE user_id = p_target_user_id AND consumed_at IS NULL AND revoked_at IS NULL;
    INSERT INTO cloud_agents_identity.password_reset_proofs (
        digest, user_id, issued_by_user_id, expires_at
    ) VALUES (p_reset_digest, p_target_user_id, actor_id, reset_expiry);
    DELETE FROM cloud_agents_identity.password_reset_proofs
        WHERE digest IN (
            SELECT proof.digest FROM cloud_agents_identity.password_reset_proofs AS proof
            WHERE proof.expires_at <= clock_timestamp()
                OR proof.consumed_at IS NOT NULL OR proof.revoked_at IS NOT NULL
            ORDER BY proof.expires_at, proof.digest LIMIT 128
        );
    INSERT INTO cloud_agents_identity.audit_events (
        id, event_kind, user_id, actor_user_id, target_user_id,
        application, decision, reason_code, correlation_id
    ) VALUES (
        p_event_id, 'password_reset_issued', p_target_user_id, actor_id, p_target_user_id,
        'admin', 'allow', 'password_reset_issued', p_correlation_id
    );
    RETURN reset_expiry;
END
$body$;

CREATE FUNCTION cloud_agents_identity.prepare_password_change(
    p_session_digest bytea, p_application text
) RETURNS TABLE (user_id text, password_hash text, locked boolean)
LANGUAGE plpgsql SECURITY DEFINER
SET search_path = pg_catalog, cloud_agents_identity
AS $body$
DECLARE
    account_id text;
    operation_time timestamptz := clock_timestamp();
BEGIN
    IF p_application NOT IN ('admin', 'user') THEN
        RAISE EXCEPTION USING ERRCODE = '22023', MESSAGE = 'invalid password change application';
    END IF;
    SELECT session.user_id INTO account_id
        FROM cloud_agents_identity.read_session(p_session_digest, p_application) AS session;
    IF account_id IS NULL THEN
        RAISE EXCEPTION USING ERRCODE = '28000', MESSAGE = 'identity session unavailable';
    END IF;
    INSERT INTO cloud_agents_identity.password_reauth_failures (session_digest)
        VALUES (p_session_digest) ON CONFLICT (session_digest) DO NOTHING;
    PERFORM 1 FROM cloud_agents_identity.password_reauth_failures AS failure
        WHERE failure.session_digest = p_session_digest FOR UPDATE;
    UPDATE cloud_agents_identity.password_reauth_failures AS failure
        SET failures = 0, window_started_at = operation_time, locked_until = NULL
        WHERE failure.session_digest = p_session_digest
            AND failure.window_started_at <= operation_time - interval '15 minutes';
    RETURN QUERY
        SELECT credential.user_id, credential.password_hash,
            failure.locked_until IS NOT NULL AND failure.locked_until > operation_time
        FROM cloud_agents_identity.password_credentials AS credential
        JOIN cloud_agents_identity.password_reauth_failures AS failure
            ON failure.session_digest = p_session_digest
        WHERE credential.user_id = account_id
        FOR UPDATE OF credential;
END
$body$;

CREATE FUNCTION cloud_agents_identity.finish_password_change(
    p_session_digest bytea, p_application text, p_expected_password_hash text,
    p_success boolean, p_new_password_hash text, p_event_id text, p_correlation_id text
) RETURNS text
LANGUAGE plpgsql SECURITY DEFINER
SET search_path = pg_catalog, cloud_agents_identity
AS $body$
DECLARE
    account_id text;
    operation_time timestamptz := clock_timestamp();
    failure_count integer;
BEGIN
    PERFORM cloud_agents_identity.require_principal('cloud_agents_identity_service');
    SELECT credential.user_id INTO account_id
        FROM cloud_agents_identity.password_credentials AS credential
        JOIN cloud_agents_identity.sessions AS session ON session.user_id = credential.user_id
        JOIN cloud_agents_identity.users AS account ON account.id = session.user_id
        WHERE session.digest = p_session_digest AND session.application = p_application
            AND session.revoked_at IS NULL AND account.disabled_at IS NULL
            AND session.expires_at > operation_time
            AND credential.password_hash = p_expected_password_hash
        FOR UPDATE OF credential;
    IF account_id IS NULL THEN
        RAISE EXCEPTION USING ERRCODE = '28000', MESSAGE = 'identity session unavailable';
    END IF;
    IF p_event_id IS NULL OR p_event_id !~ '^[a-f0-9]{32}$'
        OR p_correlation_id IS NULL OR NOT cloud_agents.is_valid_identifier(p_correlation_id)
        OR p_success IS NULL
        OR (p_success AND (p_new_password_hash IS NULL OR octet_length(p_new_password_hash) NOT BETWEEN 90 AND 256))
        OR (NOT p_success AND p_new_password_hash IS NOT NULL)
    THEN
        RAISE EXCEPTION USING ERRCODE = '22023', MESSAGE = 'invalid password change completion';
    END IF;
    IF NOT p_success THEN
        UPDATE cloud_agents_identity.password_reauth_failures AS failure
            SET failures = LEAST(failure.failures + 1, 10),
                locked_until = CASE WHEN failure.failures + 1 >= 10
                    THEN operation_time + interval '15 minutes' ELSE failure.locked_until END
            WHERE failure.session_digest = p_session_digest
            RETURNING failures INTO failure_count;
        INSERT INTO cloud_agents_identity.audit_events (
            id, event_kind, user_id, actor_user_id, target_user_id,
            application, decision, reason_code, correlation_id
        ) VALUES (
            p_event_id, 'password_change_denied', account_id, account_id, account_id,
            p_application, 'deny', CASE WHEN failure_count >= 10 THEN 'locked' ELSE 'invalid_credentials' END,
            p_correlation_id
        );
        RETURN CASE WHEN failure_count >= 10 THEN 'locked' ELSE 'denied' END;
    END IF;
    UPDATE cloud_agents_identity.password_credentials
        SET password_hash = p_new_password_hash, updated_at = operation_time
        WHERE user_id = account_id;
    UPDATE cloud_agents_identity.sessions SET revoked_at = operation_time
        WHERE user_id = account_id AND revoked_at IS NULL;
    DELETE FROM cloud_agents_identity.password_reauth_failures WHERE session_digest = p_session_digest;
    INSERT INTO cloud_agents_identity.audit_events (
        id, event_kind, user_id, actor_user_id, target_user_id,
        application, decision, reason_code, correlation_id
    ) VALUES (
        p_event_id, 'password_changed', account_id, account_id, account_id,
        p_application, 'allow', 'password_changed', p_correlation_id
    );
    RETURN 'changed';
END
$body$;

CREATE FUNCTION cloud_agents_identity.prepare_password_reset(
    p_reset_digest bytea, p_ip_bucket bytea
) RETURNS TABLE (user_id text, permitted boolean)
LANGUAGE plpgsql SECURITY DEFINER
SET search_path = pg_catalog, cloud_agents_identity
AS $body$
DECLARE
    operation_time timestamptz := clock_timestamp();
    account_id text;
    ip_locked boolean;
BEGIN
    PERFORM cloud_agents_identity.require_principal('cloud_agents_identity_service');
    IF p_reset_digest IS NULL OR octet_length(p_reset_digest) <> 32
        OR p_ip_bucket IS NULL OR octet_length(p_ip_bucket) <> 32
    THEN
        RAISE EXCEPTION USING ERRCODE = '22023', MESSAGE = 'invalid password reset proof';
    END IF;
    INSERT INTO cloud_agents_identity.password_reset_failures (ip_bucket)
        VALUES (p_ip_bucket) ON CONFLICT (ip_bucket) DO NOTHING;
    PERFORM 1 FROM cloud_agents_identity.password_reset_failures AS failure
        WHERE failure.ip_bucket = p_ip_bucket FOR UPDATE;
    UPDATE cloud_agents_identity.password_reset_failures AS failure
        SET failures = 0, window_started_at = operation_time, locked_until = NULL
        WHERE failure.ip_bucket = p_ip_bucket
            AND failure.window_started_at <= operation_time - interval '15 minutes';
    SELECT failure.locked_until IS NOT NULL AND failure.locked_until > operation_time INTO ip_locked
        FROM cloud_agents_identity.password_reset_failures AS failure
        WHERE failure.ip_bucket = p_ip_bucket;
    IF ip_locked THEN
        RAISE EXCEPTION USING ERRCODE = '53300', MESSAGE = 'password reset rate limited';
    END IF;
    UPDATE cloud_agents_identity.password_reset_proofs AS proof
        SET attempts = proof.attempts + 1
        FROM cloud_agents_identity.users AS account
        WHERE proof.digest = p_reset_digest AND proof.user_id = account.id
            AND proof.consumed_at IS NULL AND proof.revoked_at IS NULL
            AND proof.expires_at > operation_time AND proof.attempts < 10
            AND account.disabled_at IS NULL
        RETURNING proof.user_id INTO account_id;
    IF account_id IS NULL THEN
        UPDATE cloud_agents_identity.password_reset_failures AS failure
            SET failures = LEAST(failure.failures + 1, 100),
                locked_until = CASE WHEN failure.failures + 1 >= 100
                    THEN operation_time + interval '15 minutes' ELSE failure.locked_until END
            WHERE failure.ip_bucket = p_ip_bucket;
        RETURN QUERY SELECT NULL::text, false;
        RETURN;
    END IF;
    RETURN QUERY SELECT account_id, true;
END
$body$;

CREATE FUNCTION cloud_agents_identity.finish_password_reset(
    p_reset_digest bytea, p_ip_bucket bytea, p_user_id text,
    p_new_password_hash text, p_event_id text, p_correlation_id text
) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER
SET search_path = pg_catalog, cloud_agents_identity
AS $body$
DECLARE
    operation_time timestamptz := clock_timestamp();
BEGIN
    PERFORM cloud_agents_identity.require_principal('cloud_agents_identity_service');
    IF p_user_id IS NULL OR NOT cloud_agents.is_valid_identifier(p_user_id)
        OR p_new_password_hash IS NULL OR octet_length(p_new_password_hash) NOT BETWEEN 90 AND 256
        OR p_event_id IS NULL OR p_event_id !~ '^[a-f0-9]{32}$'
        OR p_correlation_id IS NULL OR NOT cloud_agents.is_valid_identifier(p_correlation_id)
    THEN
        RAISE EXCEPTION USING ERRCODE = '22023', MESSAGE = 'invalid password reset completion';
    END IF;
    UPDATE cloud_agents_identity.password_reset_proofs AS proof
        SET consumed_at = operation_time
        FROM cloud_agents_identity.users AS account
        WHERE proof.digest = p_reset_digest AND proof.user_id = p_user_id AND proof.user_id = account.id
            AND proof.consumed_at IS NULL AND proof.revoked_at IS NULL
            AND proof.expires_at > operation_time AND proof.attempts BETWEEN 1 AND 10
            AND account.disabled_at IS NULL;
    IF NOT FOUND THEN
        RAISE EXCEPTION USING ERRCODE = '28000', MESSAGE = 'password reset proof unavailable';
    END IF;
    INSERT INTO cloud_agents_identity.password_credentials (user_id, password_hash, updated_at)
        VALUES (p_user_id, p_new_password_hash, operation_time)
        ON CONFLICT (user_id) DO UPDATE
            SET password_hash = excluded.password_hash, updated_at = excluded.updated_at;
    UPDATE cloud_agents_identity.sessions SET revoked_at = operation_time
        WHERE user_id = p_user_id AND revoked_at IS NULL;
    UPDATE cloud_agents_identity.password_reset_proofs SET revoked_at = operation_time
        WHERE user_id = p_user_id AND digest <> p_reset_digest
            AND consumed_at IS NULL AND revoked_at IS NULL;
    DELETE FROM cloud_agents_identity.password_reset_failures WHERE ip_bucket = p_ip_bucket;
    INSERT INTO cloud_agents_identity.audit_events (
        id, event_kind, user_id, actor_user_id, target_user_id,
        decision, reason_code, correlation_id
    ) VALUES (
        p_event_id, 'password_reset_accepted', p_user_id, p_user_id, p_user_id,
        'allow', 'password_reset_accepted', p_correlation_id
    );
    RETURN true;
END
$body$;

CREATE FUNCTION cloud_agents_identity.list_audit_events(
    p_session_digest bytea, p_tenant_id text, p_after_at timestamptz,
    p_after_id text, p_limit integer
) RETURNS TABLE (
    id text, event_kind text, actor_user_id text, target_user_id text,
    tenant_id text, application text, decision text, reason_code text,
    correlation_id text, occurred_at timestamptz
)
LANGUAGE plpgsql SECURITY DEFINER
SET search_path = pg_catalog, cloud_agents_identity
AS $body$
BEGIN
    IF p_tenant_id IS NULL THEN
        PERFORM cloud_agents_identity.require_platform_admin(p_session_digest);
    ELSE
        PERFORM 1 FROM cloud_agents_identity.require_tenant_admin(p_session_digest, p_tenant_id);
    END IF;
    IF p_limit IS NULL OR p_limit NOT BETWEEN 1 AND 201
        OR (p_after_at IS NULL) <> (p_after_id IS NULL)
        OR (p_after_id IS NOT NULL AND p_after_id !~ '^[a-f0-9]{32}$')
    THEN
        RAISE EXCEPTION USING ERRCODE = '22023', MESSAGE = 'invalid identity audit page';
    END IF;
    RETURN QUERY
        SELECT event.id, event.event_kind, event.actor_user_id, event.target_user_id,
            event.tenant_id, event.application, event.decision, event.reason_code,
            event.correlation_id, event.occurred_at
        FROM cloud_agents_identity.audit_events AS event
        WHERE (p_tenant_id IS NULL OR event.tenant_id = p_tenant_id)
            AND (p_after_at IS NULL OR (event.occurred_at, event.id) < (p_after_at, p_after_id))
        ORDER BY event.occurred_at DESC, event.id DESC
        LIMIT p_limit;
END
$body$;

REVOKE ALL ON FUNCTION cloud_agents_identity.require_platform_admin(bytea) FROM PUBLIC;
REVOKE ALL ON FUNCTION cloud_agents_identity.list_accounts(bytea, text, integer) FROM PUBLIC;
REVOKE ALL ON FUNCTION cloud_agents_identity.list_tenant_accounts(bytea, text, text, integer) FROM PUBLIC;
REVOKE ALL ON FUNCTION cloud_agents_identity.disable_account(bytea, text, text, text) FROM PUBLIC;
REVOKE ALL ON FUNCTION cloud_agents_identity.issue_password_reset(bytea, text, bytea, text, text) FROM PUBLIC;
REVOKE ALL ON FUNCTION cloud_agents_identity.prepare_password_change(bytea, text) FROM PUBLIC;
REVOKE ALL ON FUNCTION cloud_agents_identity.finish_password_change(bytea, text, text, boolean, text, text, text) FROM PUBLIC;
REVOKE ALL ON FUNCTION cloud_agents_identity.prepare_password_reset(bytea, bytea) FROM PUBLIC;
REVOKE ALL ON FUNCTION cloud_agents_identity.finish_password_reset(bytea, bytea, text, text, text, text) FROM PUBLIC;
REVOKE ALL ON FUNCTION cloud_agents_identity.list_audit_events(bytea, text, timestamptz, text, integer) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION cloud_agents_identity.list_accounts(bytea, text, integer) TO cloud_agents_identity_service;
GRANT EXECUTE ON FUNCTION cloud_agents_identity.list_tenant_accounts(bytea, text, text, integer) TO cloud_agents_identity_service;
GRANT EXECUTE ON FUNCTION cloud_agents_identity.disable_account(bytea, text, text, text) TO cloud_agents_identity_service;
GRANT EXECUTE ON FUNCTION cloud_agents_identity.issue_password_reset(bytea, text, bytea, text, text) TO cloud_agents_identity_service;
GRANT EXECUTE ON FUNCTION cloud_agents_identity.prepare_password_change(bytea, text) TO cloud_agents_identity_service;
GRANT EXECUTE ON FUNCTION cloud_agents_identity.finish_password_change(bytea, text, text, boolean, text, text, text) TO cloud_agents_identity_service;
GRANT EXECUTE ON FUNCTION cloud_agents_identity.prepare_password_reset(bytea, bytea) TO cloud_agents_identity_service;
GRANT EXECUTE ON FUNCTION cloud_agents_identity.finish_password_reset(bytea, bytea, text, text, text, text) TO cloud_agents_identity_service;
GRANT EXECUTE ON FUNCTION cloud_agents_identity.list_audit_events(bytea, text, timestamptz, text, integer) TO cloud_agents_identity_service;
