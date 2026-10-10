CREATE TABLE cloud_agents_identity.realm (
    singleton boolean PRIMARY KEY CHECK (singleton),
    issuer text NOT NULL CHECK (length(issuer) BETWEEN 1 AND 512),
    setup_digest bytea NOT NULL CHECK (octet_length(setup_digest) = 32),
    initialization_digest bytea NOT NULL CHECK (octet_length(initialization_digest) = 32),
    first_user_id text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT clock_timestamp()
);

CREATE TABLE cloud_agents_identity.users (
    id text PRIMARY KEY CHECK (id ~ '^[A-Za-z0-9]([A-Za-z0-9._~-]{0,126}[A-Za-z0-9])?$'),
    email text NOT NULL UNIQUE CHECK (octet_length(email) BETWEEN 3 AND 254),
    display_name text NOT NULL CHECK (length(display_name) BETWEEN 1 AND 160),
    email_verified_at timestamptz NOT NULL,
    disabled_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT clock_timestamp()
);

CREATE TABLE cloud_agents_identity.password_credentials (
    user_id text PRIMARY KEY REFERENCES cloud_agents_identity.users (id),
    password_hash text NOT NULL CHECK (octet_length(password_hash) BETWEEN 90 AND 256),
    updated_at timestamptz NOT NULL DEFAULT clock_timestamp()
);

CREATE TABLE cloud_agents_identity.platform_admins (
    user_id text PRIMARY KEY REFERENCES cloud_agents_identity.users (id),
    revoked_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT clock_timestamp()
);

CREATE TABLE cloud_agents_identity.sessions (
    digest bytea PRIMARY KEY CHECK (octet_length(digest) = 32),
    user_id text NOT NULL REFERENCES cloud_agents_identity.users (id),
    application text NOT NULL CHECK (application IN ('admin', 'user')),
    created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    last_seen_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    expires_at timestamptz NOT NULL,
    revoked_at timestamptz,
    CHECK (expires_at > created_at AND expires_at <= created_at + interval '12 hours')
);

CREATE TABLE cloud_agents_identity.login_failures (
    kind text NOT NULL CHECK (kind IN ('account', 'ip')),
    bucket bytea NOT NULL CHECK (octet_length(bucket) = 32),
    window_started_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    failures integer NOT NULL DEFAULT 0 CHECK (failures BETWEEN 0 AND 100),
    locked_until timestamptz,
    PRIMARY KEY (kind, bucket)
);

CREATE TABLE cloud_agents_identity.audit_events (
    id text PRIMARY KEY CHECK (id ~ '^[a-f0-9]{32}$'),
    event_kind text NOT NULL CHECK (event_kind IN ('realm_initialized', 'password_login_succeeded', 'password_login_denied', 'session_revoked')),
    user_id text REFERENCES cloud_agents_identity.users (id),
    actor_user_id text REFERENCES cloud_agents_identity.users (id),
    service_principal name NOT NULL DEFAULT SESSION_USER,
    target_user_id text REFERENCES cloud_agents_identity.users (id),
    tenant_id text,
    application text CHECK (application IN ('admin', 'user')),
    decision text NOT NULL CHECK (decision IN ('allow', 'deny')),
    reason_code text NOT NULL CHECK (reason_code IN ('initialized', 'authenticated', 'invalid_credentials', 'locked', 'logged_out')),
    correlation_id text NOT NULL CHECK (correlation_id ~ '^[A-Za-z0-9]([A-Za-z0-9._~-]{0,126}[A-Za-z0-9])?$'),
    occurred_at timestamptz NOT NULL DEFAULT clock_timestamp()
);

CREATE FUNCTION cloud_agents_identity.require_principal(p_expected_role text)
RETURNS void
LANGUAGE plpgsql SECURITY DEFINER
SET search_path = pg_catalog, cloud_agents_identity
AS $body$
DECLARE
    expected_role record;
    caller_role record;
    caller_membership record;
    incoming_membership record;
BEGIN
    IF p_expected_role NOT IN (
        'cloud_agents_bootstrap_admin',
        'cloud_agents_identity_service',
        'cloud_agents_runtime'
    ) THEN
        RAISE EXCEPTION USING ERRCODE = '42501', MESSAGE = 'identity authority group is invalid';
    END IF;

    SELECT
        role_row.oid,
        role_row.rolcanlogin,
        role_row.rolinherit,
        role_row.rolsuper,
        role_row.rolcreatedb,
        role_row.rolcreaterole,
        role_row.rolreplication,
        role_row.rolbypassrls
    INTO STRICT expected_role
    FROM pg_catalog.pg_roles AS role_row
    WHERE role_row.rolname = p_expected_role;

    IF expected_role.rolcanlogin
        OR expected_role.rolinherit
        OR expected_role.rolsuper
        OR expected_role.rolcreatedb
        OR expected_role.rolcreaterole
        OR expected_role.rolreplication
        OR expected_role.rolbypassrls
        OR EXISTS (
            SELECT 1
            FROM pg_catalog.pg_auth_members AS membership
            WHERE membership.member = expected_role.oid
        )
    THEN
        RAISE EXCEPTION USING ERRCODE = '42501', MESSAGE = 'identity authority group drift';
    END IF;

    SELECT
        role_row.oid,
        role_row.rolcanlogin,
        role_row.rolinherit,
        role_row.rolsuper,
        role_row.rolcreatedb,
        role_row.rolcreaterole,
        role_row.rolreplication,
        role_row.rolbypassrls
    INTO STRICT caller_role
    FROM pg_catalog.pg_roles AS role_row
    WHERE role_row.rolname = SESSION_USER;

    IF SESSION_USER IN (
        'cloud_agents_migration_owner',
        'cloud_agents_runtime',
        'cloud_agents_bootstrap_admin',
        'cloud_agents_identity_service'
    )
        OR NOT caller_role.rolcanlogin
        OR NOT caller_role.rolinherit
        OR caller_role.rolsuper
        OR caller_role.rolcreatedb
        OR caller_role.rolcreaterole
        OR caller_role.rolreplication
        OR caller_role.rolbypassrls
        OR EXISTS (
            SELECT 1
            FROM pg_catalog.pg_auth_members AS membership
            WHERE membership.roleid = caller_role.oid
        )
        OR (
            SELECT pg_catalog.count(*)
            FROM pg_catalog.pg_auth_members AS membership
            WHERE membership.member = caller_role.oid
        ) <> 1
        OR EXISTS (
            SELECT 1
            FROM pg_catalog.pg_roles AS authority_role
            WHERE authority_role.rolname IN (
                'cloud_agents_migration_owner',
                'cloud_agents_runtime',
                'cloud_agents_bootstrap_admin',
                'cloud_agents_identity_service'
            )
                AND authority_role.rolname <> p_expected_role
                AND pg_catalog.pg_has_role(SESSION_USER, authority_role.oid, 'MEMBER')
        )
    THEN
        RAISE EXCEPTION USING ERRCODE = '42501', MESSAGE = 'session user lacks the direct identity authority boundary';
    END IF;

    SELECT
        membership.admin_option,
        coalesce((pg_catalog.to_jsonb(membership)->>'inherit_option')::boolean, true) AS membership_inherits,
        coalesce((pg_catalog.to_jsonb(membership)->>'set_option')::boolean, true) AS membership_is_settable,
        grantor_role.oid AS grantor_oid,
        grantor_role.rolname AS grantor_name,
        grantor_role.rolsuper AS grantor_is_superuser
    INTO caller_membership
    FROM pg_catalog.pg_auth_members AS membership
    JOIN pg_catalog.pg_roles AS grantor_role ON grantor_role.oid = membership.grantor
    WHERE membership.roleid = expected_role.oid
        AND membership.member = caller_role.oid;

    IF NOT FOUND
        OR caller_membership.admin_option
        OR NOT caller_membership.membership_inherits
        OR NOT caller_membership.membership_is_settable
        OR NOT caller_membership.grantor_is_superuser
        OR caller_membership.grantor_name IN (
            'cloud_agents_migration_owner',
            'cloud_agents_runtime',
            'cloud_agents_bootstrap_admin',
            'cloud_agents_identity_service'
        )
        OR NOT pg_catalog.pg_has_role(SESSION_USER, expected_role.oid, 'USAGE')
        OR EXISTS (
            WITH RECURSIVE grantor_memberships (roleid) AS (
                SELECT membership.roleid
                FROM pg_catalog.pg_auth_members AS membership
                WHERE membership.member = caller_membership.grantor_oid

                UNION

                SELECT membership.roleid
                FROM pg_catalog.pg_auth_members AS membership
                JOIN grantor_memberships ON grantor_memberships.roleid = membership.member
            )
            SELECT 1
            FROM grantor_memberships
            JOIN pg_catalog.pg_roles AS inherited_role ON inherited_role.oid = grantor_memberships.roleid
            WHERE inherited_role.rolname IN (
                'cloud_agents_migration_owner',
                'cloud_agents_runtime',
                'cloud_agents_bootstrap_admin',
                'cloud_agents_identity_service'
            )
        )
    THEN
        RAISE EXCEPTION USING ERRCODE = '42501', MESSAGE = 'session user identity membership has untrusted provenance';
    END IF;

    FOR incoming_membership IN
        SELECT
            membership.admin_option,
            coalesce((pg_catalog.to_jsonb(membership)->>'inherit_option')::boolean, true) AS membership_inherits,
            coalesce((pg_catalog.to_jsonb(membership)->>'set_option')::boolean, true) AS membership_is_settable,
            member_role.oid AS member_oid,
            member_role.rolname AS member_name,
            member_role.rolcanlogin AS member_can_login,
            member_role.rolinherit AS member_inherits,
            member_role.rolsuper AS member_is_superuser,
            member_role.rolcreatedb AS member_can_create_database,
            member_role.rolcreaterole AS member_can_create_role,
            member_role.rolreplication AS member_can_replicate,
            member_role.rolbypassrls AS member_can_bypass_rls,
            pg_catalog.pg_has_role(member_role.oid, expected_role.oid, 'USAGE') AS member_uses_authority,
            grantor_role.oid AS grantor_oid,
            grantor_role.rolname AS grantor_name,
            grantor_role.rolsuper AS grantor_is_superuser
        FROM pg_catalog.pg_auth_members AS membership
        JOIN pg_catalog.pg_roles AS member_role ON member_role.oid = membership.member
        JOIN pg_catalog.pg_roles AS grantor_role ON grantor_role.oid = membership.grantor
        WHERE membership.roleid = expected_role.oid
        ORDER BY membership.member
    LOOP
        IF incoming_membership.admin_option
            OR NOT incoming_membership.membership_inherits
            OR NOT incoming_membership.membership_is_settable
            OR NOT incoming_membership.member_can_login
            OR NOT incoming_membership.member_inherits
            OR NOT incoming_membership.member_uses_authority
            OR incoming_membership.member_is_superuser
            OR incoming_membership.member_can_create_database
            OR incoming_membership.member_can_create_role
            OR incoming_membership.member_can_replicate
            OR incoming_membership.member_can_bypass_rls
            OR EXISTS (
                SELECT 1
                FROM pg_catalog.pg_auth_members AS membership
                WHERE membership.roleid = incoming_membership.member_oid
            )
            OR (
                SELECT pg_catalog.count(*)
                FROM pg_catalog.pg_auth_members AS membership
                WHERE membership.member = incoming_membership.member_oid
            ) <> 1
            OR NOT incoming_membership.grantor_is_superuser
            OR incoming_membership.grantor_name IN (
                'cloud_agents_migration_owner',
                'cloud_agents_runtime',
                'cloud_agents_bootstrap_admin',
                'cloud_agents_identity_service'
            )
            OR EXISTS (
                WITH RECURSIVE grantor_memberships (roleid) AS (
                    SELECT membership.roleid
                    FROM pg_catalog.pg_auth_members AS membership
                    WHERE membership.member = incoming_membership.grantor_oid

                    UNION

                    SELECT membership.roleid
                    FROM pg_catalog.pg_auth_members AS membership
                    JOIN grantor_memberships ON grantor_memberships.roleid = membership.member
                )
                SELECT 1
                FROM grantor_memberships
                JOIN pg_catalog.pg_roles AS inherited_role ON inherited_role.oid = grantor_memberships.roleid
                WHERE inherited_role.rolname IN (
                    'cloud_agents_migration_owner',
                    'cloud_agents_runtime',
                    'cloud_agents_bootstrap_admin',
                    'cloud_agents_identity_service'
                )
            )
        THEN
            RAISE EXCEPTION USING
                ERRCODE = '42501',
                MESSAGE = pg_catalog.format('identity authority group has unsafe member %I', incoming_membership.member_name);
        END IF;
    END LOOP;
END
$body$;

CREATE FUNCTION cloud_agents_identity.initialize_realm(
    p_issuer text, p_setup_digest bytea, p_user_id text, p_email text,
    p_display_name text, p_password_hash text, p_event_id text, p_correlation_id text
) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER
SET search_path = pg_catalog, cloud_agents_identity
AS $body$
DECLARE
    existing cloud_agents_identity.realm%ROWTYPE;
    initialization bytea := pg_catalog.sha256(pg_catalog.convert_to(
        pg_catalog.jsonb_build_array(p_issuer, pg_catalog.encode(p_setup_digest, 'hex'), p_user_id,
            p_email, p_display_name, p_password_hash)::text, 'UTF8'));
BEGIN
    PERFORM cloud_agents_identity.require_principal('cloud_agents_bootstrap_admin');
    PERFORM pg_catalog.pg_advisory_xact_lock(5736179203699110521);
    SELECT * INTO existing FROM cloud_agents_identity.realm WHERE singleton;
    IF FOUND THEN
        IF existing.initialization_digest IS DISTINCT FROM initialization
            OR NOT EXISTS (SELECT 1 FROM cloud_agents_identity.users WHERE id = p_user_id AND email = p_email)
            OR NOT EXISTS (SELECT 1 FROM cloud_agents_identity.password_credentials WHERE user_id = p_user_id)
            OR NOT EXISTS (SELECT 1 FROM cloud_agents_identity.platform_admins WHERE user_id = p_user_id)
        THEN
            RAISE EXCEPTION USING ERRCODE = '22023', MESSAGE = 'identity installation conflicts with existing realm';
        END IF;
        RETURN false;
    END IF;
    INSERT INTO cloud_agents_identity.realm (singleton, issuer, setup_digest, initialization_digest, first_user_id)
        VALUES (true, p_issuer, p_setup_digest, initialization, p_user_id);
    INSERT INTO cloud_agents_identity.users (id, email, display_name, email_verified_at)
        VALUES (p_user_id, p_email, p_display_name, clock_timestamp());
    INSERT INTO cloud_agents_identity.password_credentials (user_id, password_hash)
        VALUES (p_user_id, p_password_hash);
    INSERT INTO cloud_agents_identity.platform_admins (user_id) VALUES (p_user_id);
    INSERT INTO cloud_agents_identity.audit_events (id, event_kind, user_id, actor_user_id, target_user_id, decision, reason_code, correlation_id)
        VALUES (p_event_id, 'realm_initialized', p_user_id, NULL, p_user_id, 'allow', 'initialized', p_correlation_id);
    RETURN true;
END
$body$;

CREATE FUNCTION cloud_agents_identity.prepare_password_login(p_email text, p_account_bucket bytea, p_ip_bucket bytea)
RETURNS TABLE (user_id text, password_hash text, locked boolean)
LANGUAGE plpgsql SECURITY DEFINER
SET search_path = pg_catalog, cloud_agents_identity
AS $body$
DECLARE
    operation_time timestamptz;
    matched_user text;
    matched_password text;
    denied boolean;
BEGIN
    PERFORM cloud_agents_identity.require_principal('cloud_agents_identity_service');
    INSERT INTO cloud_agents_identity.login_failures (kind, bucket)
        VALUES ('account', p_account_bucket), ('ip', p_ip_bucket)
        ON CONFLICT (kind, bucket) DO NOTHING;
    PERFORM 1 FROM cloud_agents_identity.login_failures
        WHERE (kind = 'account' AND bucket = p_account_bucket) OR (kind = 'ip' AND bucket = p_ip_bucket)
        ORDER BY kind FOR UPDATE;
    operation_time := clock_timestamp();
    UPDATE cloud_agents_identity.login_failures
        SET failures = 0, window_started_at = operation_time, locked_until = NULL
        WHERE ((kind = 'account' AND bucket = p_account_bucket) OR (kind = 'ip' AND bucket = p_ip_bucket))
            AND window_started_at <= operation_time - interval '15 minutes'
            AND (locked_until IS NULL OR locked_until <= operation_time);
    SELECT EXISTS (
        SELECT 1 FROM cloud_agents_identity.login_failures
        WHERE ((kind = 'account' AND bucket = p_account_bucket) OR (kind = 'ip' AND bucket = p_ip_bucket))
            AND locked_until > operation_time
    ) INTO denied;
    SELECT u.id, p.password_hash INTO matched_user, matched_password
        FROM cloud_agents_identity.users AS u
        JOIN cloud_agents_identity.password_credentials AS p ON p.user_id = u.id
        WHERE u.email = p_email AND u.disabled_at IS NULL
        FOR SHARE OF u, p;
    RETURN QUERY SELECT matched_user, matched_password, denied;
END
$body$;

CREATE FUNCTION cloud_agents_identity.finish_password_login(
    p_email text, p_account_bucket bytea, p_ip_bucket bytea, p_password_hash text,
    p_success boolean, p_application text, p_session_digest bytea, p_event_id text, p_correlation_id text
) RETURNS text
LANGUAGE plpgsql SECURITY DEFINER
SET search_path = pg_catalog, cloud_agents_identity
AS $body$
DECLARE
    operation_time timestamptz;
    matched_user text;
    denied boolean;
BEGIN
    PERFORM cloud_agents_identity.require_principal('cloud_agents_identity_service');
    IF p_application NOT IN ('admin', 'user') OR p_application IS NULL OR p_success IS NULL THEN
        RAISE EXCEPTION USING ERRCODE = '22023', MESSAGE = 'invalid identity application';
    END IF;
    PERFORM 1 FROM cloud_agents_identity.login_failures
        WHERE (kind = 'account' AND bucket = p_account_bucket) OR (kind = 'ip' AND bucket = p_ip_bucket)
        ORDER BY kind FOR UPDATE;
    operation_time := clock_timestamp();
    SELECT EXISTS (
        SELECT 1 FROM cloud_agents_identity.login_failures
        WHERE ((kind = 'account' AND bucket = p_account_bucket) OR (kind = 'ip' AND bucket = p_ip_bucket))
            AND locked_until > operation_time
    ) INTO denied;
    SELECT u.id INTO matched_user FROM cloud_agents_identity.users AS u
        JOIN cloud_agents_identity.password_credentials AS p ON p.user_id = u.id
        WHERE u.email = p_email AND u.disabled_at IS NULL AND p.password_hash = p_password_hash
        FOR SHARE OF u, p;
    IF p_success AND NOT denied AND matched_user IS NOT NULL THEN
        INSERT INTO cloud_agents_identity.sessions (digest, user_id, application, created_at, last_seen_at, expires_at)
            VALUES (p_session_digest, matched_user, p_application, operation_time, operation_time, operation_time + interval '12 hours');
        UPDATE cloud_agents_identity.login_failures SET failures = 0, window_started_at = operation_time, locked_until = NULL
            WHERE kind = 'account' AND bucket = p_account_bucket;
        INSERT INTO cloud_agents_identity.audit_events (id, event_kind, user_id, actor_user_id, target_user_id, application, decision, reason_code, correlation_id)
            VALUES (p_event_id, 'password_login_succeeded', matched_user, matched_user, matched_user, p_application, 'allow', 'authenticated', p_correlation_id);
        RETURN 'authenticated';
    END IF;
    IF NOT denied THEN
        UPDATE cloud_agents_identity.login_failures
            SET failures = LEAST(failures + 1, CASE WHEN kind = 'account' THEN 10 ELSE 100 END),
                locked_until = CASE WHEN failures + 1 >= CASE WHEN kind = 'account' THEN 10 ELSE 100 END
                    THEN operation_time + interval '15 minutes' ELSE locked_until END
            WHERE (kind = 'account' AND bucket = p_account_bucket) OR (kind = 'ip' AND bucket = p_ip_bucket);
    END IF;
    INSERT INTO cloud_agents_identity.audit_events (id, event_kind, user_id, target_user_id, application, decision, reason_code, correlation_id)
        VALUES (p_event_id, 'password_login_denied', matched_user, matched_user, p_application, 'deny', CASE WHEN denied THEN 'locked' ELSE 'invalid_credentials' END, p_correlation_id);
    RETURN CASE WHEN denied THEN 'locked' ELSE 'denied' END;
END
$body$;

CREATE FUNCTION cloud_agents_identity.read_session(p_digest bytea, p_application text)
RETURNS TABLE (user_id text, email text, display_name text, platform_admin boolean)
LANGUAGE plpgsql SECURITY DEFINER
SET search_path = pg_catalog, cloud_agents_identity
AS $body$
DECLARE
    operation_time timestamptz := clock_timestamp();
BEGIN
    PERFORM cloud_agents_identity.require_principal('cloud_agents_identity_service');
    RETURN QUERY
        WITH touched AS (
            UPDATE cloud_agents_identity.sessions AS s SET last_seen_at = operation_time
            FROM cloud_agents_identity.users AS u
            WHERE s.digest = p_digest AND s.application = p_application AND s.user_id = u.id
                AND s.revoked_at IS NULL AND u.disabled_at IS NULL
                AND s.expires_at > operation_time AND s.last_seen_at > operation_time - interval '30 minutes'
            RETURNING u.id, u.email, u.display_name
        )
        SELECT t.id, t.email, t.display_name,
            EXISTS (SELECT 1 FROM cloud_agents_identity.platform_admins AS a WHERE a.user_id = t.id AND a.revoked_at IS NULL)
        FROM touched AS t;
END
$body$;

CREATE FUNCTION cloud_agents_identity.revoke_session(p_digest bytea, p_application text, p_event_id text, p_correlation_id text)
RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER
SET search_path = pg_catalog, cloud_agents_identity
AS $body$
DECLARE
    matched_user text;
BEGIN
    PERFORM cloud_agents_identity.require_principal('cloud_agents_identity_service');
    UPDATE cloud_agents_identity.sessions SET revoked_at = clock_timestamp()
        WHERE digest = p_digest AND application = p_application AND revoked_at IS NULL
        RETURNING user_id INTO matched_user;
    IF NOT FOUND THEN RETURN false; END IF;
    INSERT INTO cloud_agents_identity.audit_events (id, event_kind, user_id, actor_user_id, target_user_id, application, decision, reason_code, correlation_id)
        VALUES (p_event_id, 'session_revoked', matched_user, matched_user, matched_user, p_application, 'allow', 'logged_out', p_correlation_id);
    RETURN true;
END
$body$;

REVOKE ALL ON FUNCTION cloud_agents_identity.require_principal(text) FROM PUBLIC;
GRANT USAGE ON SCHEMA cloud_agents_identity TO cloud_agents_bootstrap_admin;
GRANT EXECUTE ON FUNCTION cloud_agents_identity.initialize_realm(text, bytea, text, text, text, text, text, text) TO cloud_agents_bootstrap_admin;
GRANT EXECUTE ON FUNCTION cloud_agents_identity.prepare_password_login(text, bytea, bytea) TO cloud_agents_identity_service;
GRANT EXECUTE ON FUNCTION cloud_agents_identity.finish_password_login(text, bytea, bytea, text, boolean, text, bytea, text, text) TO cloud_agents_identity_service;
GRANT EXECUTE ON FUNCTION cloud_agents_identity.read_session(bytea, text) TO cloud_agents_identity_service;
GRANT EXECUTE ON FUNCTION cloud_agents_identity.revoke_session(bytea, text, text, text) TO cloud_agents_identity_service;
