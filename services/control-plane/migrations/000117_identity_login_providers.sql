ALTER TABLE cloud_agents_identity.audit_events DROP CONSTRAINT audit_events_event_kind_check;
ALTER TABLE cloud_agents_identity.audit_events ADD CONSTRAINT audit_events_event_kind_check
    CHECK (event_kind IN ('realm_initialized', 'password_login_succeeded', 'password_login_denied', 'session_revoked', 'token_issued', 'policy_updated', 'invitation_created', 'invitation_revoked', 'invitation_accepted', 'account_disabled', 'password_reset_issued', 'password_reset_accepted', 'password_changed', 'password_change_denied', 'provider_config_updated', 'provider_login_succeeded', 'provider_reauth_succeeded', 'password_reauth_succeeded', 'login_method_linked', 'login_method_unlinked'));
ALTER TABLE cloud_agents_identity.audit_events DROP CONSTRAINT audit_events_reason_code_check;
ALTER TABLE cloud_agents_identity.audit_events ADD CONSTRAINT audit_events_reason_code_check
    CHECK (reason_code IN ('initialized', 'authenticated', 'invalid_credentials', 'locked', 'logged_out', 'issued', 'policy_updated', 'invitation_created', 'invitation_revoked', 'invitation_accepted', 'account_disabled', 'password_reset_issued', 'password_reset_accepted', 'password_changed', 'provider_config_updated', 'provider_authenticated', 'provider_reauthenticated', 'password_reauthenticated', 'login_method_linked', 'login_method_unlinked'));

CREATE TABLE cloud_agents_identity.provider_clients (
    id text NOT NULL CHECK (id ~ '^[A-Za-z0-9]([A-Za-z0-9._~-]{0,126}[A-Za-z0-9])?$'),
    application text NOT NULL CHECK (application IN ('admin', 'user')),
    display_name text NOT NULL CHECK (length(display_name) BETWEEN 1 AND 160),
    provider_kind text NOT NULL CHECK (provider_kind IN ('oidc', 'github', 'gitlab', 'feishu', 'dingtalk', 'wecom')),
    issuer text NOT NULL CHECK (octet_length(issuer) BETWEEN 1 AND 512),
    client_id text NOT NULL CHECK (octet_length(client_id) BETWEEN 1 AND 512),
    redirect_uri text NOT NULL CHECK (octet_length(redirect_uri) BETWEEN 1 AND 2048),
    secret_ref text NOT NULL CHECK (secret_ref ~ '^[A-Za-z0-9]([A-Za-z0-9._~-]{0,126}[A-Za-z0-9])?$'),
    root_ca_ref text CHECK (root_ca_ref ~ '^[A-Za-z0-9]([A-Za-z0-9._~-]{0,126}[A-Za-z0-9])?$'),
    agent_id text CHECK (octet_length(agent_id) BETWEEN 1 AND 255),
    scopes text[] NOT NULL DEFAULT ARRAY[]::text[] CHECK (cardinality(scopes) <= 32 AND array_position(scopes, NULL) IS NULL),
    trust_provider_email boolean NOT NULL DEFAULT false,
    allowed_organization_ids text[] NOT NULL DEFAULT ARRAY[]::text[] CHECK (cardinality(allowed_organization_ids) <= 32 AND array_position(allowed_organization_ids, NULL) IS NULL),
    enabled boolean NOT NULL DEFAULT true,
    resource_version bigint NOT NULL CHECK (resource_version BETWEEN 1 AND 9223372036854775807),
    created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    PRIMARY KEY (id, application),
    CHECK ((provider_kind = 'wecom') = (agent_id IS NOT NULL)),
    CHECK (trust_provider_email OR cardinality(allowed_organization_ids) = 0)
);

CREATE TABLE cloud_agents_identity.login_methods (
    id text PRIMARY KEY CHECK (id ~ '^[A-Za-z0-9]([A-Za-z0-9._~-]{0,126}[A-Za-z0-9])?$'),
    user_id text NOT NULL REFERENCES cloud_agents_identity.users (id),
    provider_id text NOT NULL CHECK (provider_id ~ '^[A-Za-z0-9]([A-Za-z0-9._~-]{0,126}[A-Za-z0-9])?$'),
    issuer text NOT NULL CHECK (octet_length(issuer) BETWEEN 1 AND 512),
    subject text NOT NULL CHECK (octet_length(subject) BETWEEN 1 AND 512),
    created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    revoked_at timestamptz,
    UNIQUE (issuer, subject)
);

CREATE TABLE cloud_agents_identity.reauth_grants (
    digest bytea PRIMARY KEY CHECK (octet_length(digest) = 32),
    user_id text NOT NULL REFERENCES cloud_agents_identity.users (id),
    session_digest bytea NOT NULL REFERENCES cloud_agents_identity.sessions (digest),
    created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    expires_at timestamptz NOT NULL,
    consumed_at timestamptz,
    CHECK (expires_at > created_at AND expires_at <= created_at + interval '5 minutes')
);

CREATE TABLE cloud_agents_identity.provider_flows (
    state_digest bytea PRIMARY KEY CHECK (octet_length(state_digest) = 32),
    verifier_digest bytea NOT NULL CHECK (octet_length(verifier_digest) = 32),
    nonce_digest bytea NOT NULL CHECK (octet_length(nonce_digest) = 32),
    application text NOT NULL CHECK (application IN ('admin', 'user')),
    provider_id text NOT NULL,
    provider_revision bigint NOT NULL CHECK (provider_revision BETWEEN 1 AND 9223372036854775807),
    purpose text NOT NULL CHECK (purpose IN ('login', 'invitation', 'reauth', 'link')),
    session_digest bytea REFERENCES cloud_agents_identity.sessions (digest),
    invitation_digest bytea REFERENCES cloud_agents_identity.invitations (code_digest),
    invitation_ip_bucket bytea CHECK (octet_length(invitation_ip_bucket) = 32),
    reauth_digest bytea REFERENCES cloud_agents_identity.reauth_grants (digest),
    display_name text CHECK (length(display_name) BETWEEN 1 AND 160),
    created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    expires_at timestamptz NOT NULL,
    consumed_at timestamptz,
    completed_at timestamptz,
    FOREIGN KEY (provider_id, application) REFERENCES cloud_agents_identity.provider_clients (id, application),
    CHECK (expires_at > created_at AND expires_at <= created_at + interval '10 minutes'),
    CHECK (
        (purpose = 'login' AND session_digest IS NULL AND invitation_digest IS NULL AND invitation_ip_bucket IS NULL AND reauth_digest IS NULL AND display_name IS NULL)
        OR (purpose = 'invitation' AND session_digest IS NULL AND invitation_digest IS NOT NULL AND invitation_ip_bucket IS NOT NULL AND reauth_digest IS NULL AND display_name IS NOT NULL)
        OR (purpose = 'reauth' AND session_digest IS NOT NULL AND invitation_digest IS NULL AND invitation_ip_bucket IS NULL AND reauth_digest IS NULL AND display_name IS NULL)
        OR (purpose = 'link' AND session_digest IS NOT NULL AND invitation_digest IS NULL AND invitation_ip_bucket IS NULL AND reauth_digest IS NOT NULL AND display_name IS NULL)
    )
);

CREATE FUNCTION cloud_agents_identity.lock_password_credential_user()
RETURNS trigger
LANGUAGE plpgsql SECURITY DEFINER
SET search_path = pg_catalog, cloud_agents_identity
AS $body$
BEGIN
    PERFORM 1 FROM cloud_agents_identity.users AS account
        WHERE account.id = NEW.user_id FOR UPDATE;
    IF NOT FOUND THEN
        RAISE EXCEPTION USING ERRCODE = '23503', MESSAGE = 'identity account unavailable';
    END IF;
    RETURN NEW;
END
$body$;

CREATE TRIGGER password_credentials_lock_user
    BEFORE INSERT OR UPDATE OF password_hash ON cloud_agents_identity.password_credentials
    FOR EACH ROW EXECUTE FUNCTION cloud_agents_identity.lock_password_credential_user();

CREATE FUNCTION cloud_agents_identity.list_public_provider_clients(p_application text)
RETURNS TABLE (provider_id text, provider_kind text, display_name text)
LANGUAGE plpgsql SECURITY DEFINER
SET search_path = pg_catalog, cloud_agents_identity
AS $body$
BEGIN
    PERFORM cloud_agents_identity.require_principal('cloud_agents_identity_service');
    IF p_application NOT IN ('admin', 'user') THEN
        RAISE EXCEPTION USING ERRCODE = '22023', MESSAGE = 'invalid provider application';
    END IF;
    RETURN QUERY SELECT client.id, client.provider_kind, client.display_name
        FROM cloud_agents_identity.provider_clients AS client
        WHERE client.application = p_application AND client.enabled
        ORDER BY client.id LIMIT 64;
END
$body$;

CREATE FUNCTION cloud_agents_identity.list_provider_clients(p_session_digest bytea)
RETURNS TABLE (
    provider_id text, application text, display_name text, provider_kind text,
    issuer text, client_id text, redirect_uri text, secret_ref text,
    root_ca_ref text, agent_id text, scopes text[], trust_provider_email boolean,
    allowed_organization_ids text[], enabled boolean, resource_version bigint
)
LANGUAGE plpgsql SECURITY DEFINER
SET search_path = pg_catalog, cloud_agents_identity
AS $body$
BEGIN
    PERFORM cloud_agents_identity.require_principal('cloud_agents_identity_service');
    PERFORM cloud_agents_identity.require_platform_admin(p_session_digest);
    RETURN QUERY SELECT client.id, client.application, client.display_name,
        client.provider_kind, client.issuer, client.client_id, client.redirect_uri,
        client.secret_ref, client.root_ca_ref, client.agent_id, client.scopes,
        client.trust_provider_email, client.allowed_organization_ids,
        client.enabled, client.resource_version
        FROM cloud_agents_identity.provider_clients AS client
        ORDER BY client.id, client.application LIMIT 128;
END
$body$;

CREATE FUNCTION cloud_agents_identity.upsert_provider_client(
    p_session_digest bytea, p_provider_id text, p_application text,
    p_display_name text, p_provider_kind text, p_issuer text, p_client_id text,
    p_redirect_uri text, p_secret_ref text, p_root_ca_ref text, p_agent_id text,
    p_scopes text[], p_trust_provider_email boolean, p_allowed_organization_ids text[],
    p_enabled boolean, p_expected_resource_version bigint,
    p_event_id text, p_correlation_id text
) RETURNS bigint
LANGUAGE plpgsql SECURITY DEFINER
SET search_path = pg_catalog, cloud_agents_identity
AS $body$
DECLARE
    actor_id text;
    next_version bigint;
    existing_version bigint;
BEGIN
    PERFORM cloud_agents_identity.require_principal('cloud_agents_identity_service');
    IF p_provider_id IS NULL OR NOT cloud_agents.is_valid_identifier(p_provider_id)
        OR p_application NOT IN ('admin', 'user')
        OR p_display_name IS NULL OR length(p_display_name) NOT BETWEEN 1 AND 160
        OR p_provider_kind NOT IN ('oidc', 'github', 'gitlab', 'feishu', 'dingtalk', 'wecom')
        OR p_issuer IS NULL OR octet_length(p_issuer) NOT BETWEEN 1 AND 512
        OR p_client_id IS NULL OR octet_length(p_client_id) NOT BETWEEN 1 AND 512
        OR p_redirect_uri IS NULL OR octet_length(p_redirect_uri) NOT BETWEEN 1 AND 2048
        OR p_secret_ref IS NULL OR NOT cloud_agents.is_valid_identifier(p_secret_ref)
        OR (p_root_ca_ref IS NOT NULL AND NOT cloud_agents.is_valid_identifier(p_root_ca_ref))
        OR ((p_provider_kind = 'wecom') <> (p_agent_id IS NOT NULL))
        OR (p_agent_id IS NOT NULL AND octet_length(p_agent_id) NOT BETWEEN 1 AND 255)
        OR p_scopes IS NULL OR cardinality(p_scopes) > 32 OR array_position(p_scopes, NULL) IS NOT NULL
        OR p_trust_provider_email IS NULL
        OR p_allowed_organization_ids IS NULL OR cardinality(p_allowed_organization_ids) > 32
        OR array_position(p_allowed_organization_ids, NULL) IS NOT NULL
        OR (NOT p_trust_provider_email AND cardinality(p_allowed_organization_ids) <> 0)
        OR p_enabled IS NULL OR p_expected_resource_version IS NULL OR p_expected_resource_version < 0
        OR p_event_id IS NULL OR p_event_id !~ '^[a-f0-9]{32}$'
        OR p_correlation_id IS NULL OR NOT cloud_agents.is_valid_identifier(p_correlation_id)
    THEN
        RAISE EXCEPTION USING ERRCODE = '22023', MESSAGE = 'invalid provider configuration';
    END IF;
    actor_id := cloud_agents_identity.require_platform_admin(p_session_digest);
    SELECT client.resource_version INTO existing_version
        FROM cloud_agents_identity.provider_clients AS client
        WHERE client.id = p_provider_id AND client.application = p_application
        FOR UPDATE;
    IF existing_version IS NULL THEN
        IF p_expected_resource_version <> 0 THEN
            RAISE EXCEPTION USING ERRCODE = '40001', MESSAGE = 'provider configuration conflict';
        END IF;
        next_version := 1;
        INSERT INTO cloud_agents_identity.provider_clients (
            id, application, display_name, provider_kind, issuer, client_id,
            redirect_uri, secret_ref, root_ca_ref, agent_id, scopes,
            trust_provider_email, allowed_organization_ids, enabled, resource_version
        ) VALUES (
            p_provider_id, p_application, p_display_name, p_provider_kind, p_issuer,
            p_client_id, p_redirect_uri, p_secret_ref, p_root_ca_ref, p_agent_id,
            p_scopes, p_trust_provider_email, p_allowed_organization_ids,
            p_enabled, next_version
        );
    ELSE
        IF existing_version <> p_expected_resource_version OR existing_version = 9223372036854775807 THEN
            RAISE EXCEPTION USING ERRCODE = '40001', MESSAGE = 'provider configuration conflict';
        END IF;
        next_version := existing_version + 1;
        UPDATE cloud_agents_identity.provider_clients AS client SET
            display_name = p_display_name, provider_kind = p_provider_kind,
            issuer = p_issuer, client_id = p_client_id, redirect_uri = p_redirect_uri,
            secret_ref = p_secret_ref, root_ca_ref = p_root_ca_ref, agent_id = p_agent_id,
            scopes = p_scopes, trust_provider_email = p_trust_provider_email,
            allowed_organization_ids = p_allowed_organization_ids,
            enabled = p_enabled, resource_version = next_version, updated_at = clock_timestamp()
            WHERE client.id = p_provider_id AND client.application = p_application;
    END IF;
    INSERT INTO cloud_agents_identity.audit_events (
        id, event_kind, user_id, actor_user_id, target_user_id,
        application, decision, reason_code, correlation_id
    ) VALUES (
        p_event_id, 'provider_config_updated', actor_id, actor_id, actor_id,
        p_application, 'allow', 'provider_config_updated', p_correlation_id
    );
    RETURN next_version;
END
$body$;

CREATE FUNCTION cloud_agents_identity.read_provider_client(p_application text, p_provider_id text)
RETURNS TABLE (
    provider_id text, application text, display_name text, provider_kind text,
    issuer text, client_id text, redirect_uri text, secret_ref text,
    root_ca_ref text, agent_id text, scopes text[], trust_provider_email boolean,
    allowed_organization_ids text[], resource_version bigint
)
LANGUAGE plpgsql SECURITY DEFINER
SET search_path = pg_catalog, cloud_agents_identity
AS $body$
BEGIN
    PERFORM cloud_agents_identity.require_principal('cloud_agents_identity_service');
    IF p_application NOT IN ('admin', 'user')
        OR p_provider_id IS NULL OR NOT cloud_agents.is_valid_identifier(p_provider_id)
    THEN
        RAISE EXCEPTION USING ERRCODE = '22023', MESSAGE = 'invalid provider selection';
    END IF;
    RETURN QUERY SELECT client.id, client.application, client.display_name,
        client.provider_kind, client.issuer, client.client_id, client.redirect_uri,
        client.secret_ref, client.root_ca_ref, client.agent_id, client.scopes,
        client.trust_provider_email, client.allowed_organization_ids,
        client.resource_version
        FROM cloud_agents_identity.provider_clients AS client
        WHERE client.id = p_provider_id AND client.application = p_application
            AND client.enabled;
END
$body$;

CREATE FUNCTION cloud_agents_identity.create_provider_flow(
    p_state_digest bytea, p_verifier_digest bytea, p_nonce_digest bytea,
    p_application text, p_provider_id text, p_provider_revision bigint,
    p_purpose text, p_session_digest bytea, p_invitation_digest bytea,
    p_invitation_ip_bucket bytea, p_reauth_digest bytea,
    p_display_name text, p_expires_at timestamptz
) RETURNS text
LANGUAGE plpgsql SECURITY DEFINER
SET search_path = pg_catalog, cloud_agents_identity
AS $body$
DECLARE
    account_id text;
    invitation_permitted boolean;
    invitation_rate_limited boolean;
    operation_time timestamptz := clock_timestamp();
BEGIN
    PERFORM cloud_agents_identity.require_principal('cloud_agents_identity_service');
    IF p_state_digest IS NULL OR octet_length(p_state_digest) <> 32
        OR p_verifier_digest IS NULL OR octet_length(p_verifier_digest) <> 32
        OR p_nonce_digest IS NULL OR octet_length(p_nonce_digest) <> 32
        OR p_application NOT IN ('admin', 'user')
        OR p_provider_id IS NULL OR NOT cloud_agents.is_valid_identifier(p_provider_id)
        OR p_provider_revision IS NULL OR p_provider_revision < 1
        OR p_purpose NOT IN ('login', 'invitation', 'reauth', 'link')
        OR p_expires_at <= operation_time OR p_expires_at > operation_time + interval '10 minutes 5 seconds'
        OR NOT (
            (p_purpose = 'login' AND p_session_digest IS NULL AND p_invitation_digest IS NULL AND p_invitation_ip_bucket IS NULL AND p_reauth_digest IS NULL AND p_display_name IS NULL)
            OR (p_purpose = 'invitation' AND p_session_digest IS NULL AND p_invitation_digest IS NOT NULL AND octet_length(p_invitation_ip_bucket) = 32 AND p_reauth_digest IS NULL AND p_display_name IS NOT NULL AND length(p_display_name) BETWEEN 1 AND 160)
            OR (p_purpose = 'reauth' AND p_session_digest IS NOT NULL AND p_invitation_digest IS NULL AND p_invitation_ip_bucket IS NULL AND p_reauth_digest IS NULL AND p_display_name IS NULL)
            OR (p_purpose = 'link' AND p_session_digest IS NOT NULL AND p_invitation_digest IS NULL AND p_invitation_ip_bucket IS NULL AND p_reauth_digest IS NOT NULL AND p_display_name IS NULL)
        )
    THEN
        RAISE EXCEPTION USING ERRCODE = '22023', MESSAGE = 'invalid provider flow';
    END IF;
    PERFORM 1 FROM cloud_agents_identity.provider_clients AS client
        WHERE client.id = p_provider_id AND client.application = p_application
            AND client.resource_version = p_provider_revision AND client.enabled
        FOR SHARE;
    IF NOT FOUND THEN
        RAISE EXCEPTION USING ERRCODE = '40001', MESSAGE = 'provider configuration changed';
    END IF;
    IF p_purpose = 'invitation' THEN
        SELECT attempt.permitted, attempt.rate_limited
            INTO invitation_permitted, invitation_rate_limited
            FROM cloud_agents_identity.record_invitation_accept_attempt(
                p_invitation_digest, p_invitation_ip_bucket
            ) AS attempt;
        IF invitation_rate_limited THEN
            RETURN 'rate_limited';
        END IF;
        IF NOT invitation_permitted THEN
            RETURN 'denied';
        END IF;
        PERFORM 1 FROM cloud_agents_identity.invitations AS invitation
            WHERE invitation.code_digest = p_invitation_digest
                AND invitation.state = 'pending' AND invitation.expires_at > operation_time
                AND invitation.verification = 'provider-required'
            FOR UPDATE;
        IF NOT FOUND THEN
            RAISE EXCEPTION USING ERRCODE = '28000', MESSAGE = 'invitation unavailable';
        END IF;
    ELSIF p_purpose IN ('reauth', 'link') THEN
        SELECT session.user_id INTO account_id
            FROM cloud_agents_identity.sessions AS session
            JOIN cloud_agents_identity.users AS account ON account.id = session.user_id
            WHERE session.digest = p_session_digest AND session.application = p_application
                AND session.revoked_at IS NULL AND session.expires_at > operation_time
                AND session.last_seen_at > operation_time - interval '30 minutes'
                AND account.disabled_at IS NULL
            FOR UPDATE OF account;
        IF account_id IS NULL THEN
            RAISE EXCEPTION USING ERRCODE = '28000', MESSAGE = 'identity session unavailable';
        END IF;
        IF p_purpose = 'link' THEN
            UPDATE cloud_agents_identity.reauth_grants AS grant_row
                SET consumed_at = operation_time
                WHERE grant_row.digest = p_reauth_digest
                    AND grant_row.user_id = account_id
                    AND grant_row.session_digest = p_session_digest
                    AND grant_row.consumed_at IS NULL AND grant_row.expires_at > operation_time;
            IF NOT FOUND THEN
                RAISE EXCEPTION USING ERRCODE = '28000', MESSAGE = 'reauthentication unavailable';
            END IF;
        END IF;
    END IF;
    INSERT INTO cloud_agents_identity.provider_flows (
        state_digest, verifier_digest, nonce_digest, application,
        provider_id, provider_revision, purpose, session_digest,
        invitation_digest, invitation_ip_bucket, reauth_digest, display_name, expires_at
    ) VALUES (
        p_state_digest, p_verifier_digest, p_nonce_digest, p_application,
        p_provider_id, p_provider_revision, p_purpose, p_session_digest,
        p_invitation_digest, p_invitation_ip_bucket, p_reauth_digest,
        p_display_name, p_expires_at
    );
    DELETE FROM cloud_agents_identity.provider_flows
        WHERE state_digest IN (
            SELECT flow.state_digest FROM cloud_agents_identity.provider_flows AS flow
            WHERE flow.expires_at <= operation_time OR flow.completed_at IS NOT NULL
            ORDER BY flow.expires_at, flow.state_digest LIMIT 128
        );
    RETURN 'created';
END
$body$;

CREATE FUNCTION cloud_agents_identity.consume_provider_flow(
    p_application text, p_state_digest bytea,
    p_verifier_digest bytea, p_nonce_digest bytea
) RETURNS TABLE (
    state_digest bytea, provider_id text, application text, display_name text,
    provider_kind text, issuer text, client_id text, redirect_uri text,
    secret_ref text, root_ca_ref text, agent_id text, scopes text[],
    trust_provider_email boolean, allowed_organization_ids text[],
    resource_version bigint, purpose text, session_digest bytea,
    invitation_digest bytea, reauth_digest bytea, invitation_display_name text
)
LANGUAGE plpgsql SECURITY DEFINER
SET search_path = pg_catalog, cloud_agents_identity
AS $body$
DECLARE
    operation_time timestamptz := clock_timestamp();
BEGIN
    PERFORM cloud_agents_identity.require_principal('cloud_agents_identity_service');
    RETURN QUERY
        WITH consumed AS (
            UPDATE cloud_agents_identity.provider_flows AS flow
                SET consumed_at = operation_time
                FROM cloud_agents_identity.provider_clients AS client
                WHERE flow.state_digest = p_state_digest
                    AND flow.application = p_application
                    AND flow.verifier_digest = p_verifier_digest
                    AND flow.nonce_digest = p_nonce_digest
                    AND flow.consumed_at IS NULL AND flow.completed_at IS NULL
                    AND flow.expires_at > operation_time
                    AND client.id = flow.provider_id
                    AND client.application = flow.application
                    AND client.resource_version = flow.provider_revision
                    AND client.enabled
                RETURNING flow.*
        )
        SELECT consumed.state_digest, client.id, client.application, client.display_name,
            client.provider_kind, client.issuer, client.client_id, client.redirect_uri,
            client.secret_ref, client.root_ca_ref, client.agent_id, client.scopes,
            client.trust_provider_email, client.allowed_organization_ids,
            client.resource_version, consumed.purpose, consumed.session_digest,
            consumed.invitation_digest, consumed.reauth_digest, consumed.display_name
        FROM consumed
        JOIN cloud_agents_identity.provider_clients AS client
            ON client.id = consumed.provider_id
            AND client.application = consumed.application
            AND client.resource_version = consumed.provider_revision;
END
$body$;

CREATE FUNCTION cloud_agents_identity.finish_provider_callback(
    p_state_digest bytea, p_application text,
    p_asserted_issuer text, p_asserted_subject text,
    p_asserted_email text, p_email_verified boolean,
    p_session_digest bytea, p_new_user_id text, p_login_method_id text,
    p_reauth_digest bytea, p_membership_id text, p_role_binding_id text,
    p_membership_audit_id text, p_role_binding_audit_id text,
    p_event_id text, p_correlation_id text
) RETURNS TABLE (
    purpose text, user_id text, email text, display_name text,
    platform_admin boolean, login_method_id text,
    login_method_provider_id text, login_method_issuer text,
    login_method_subject text, login_method_created_at timestamptz,
    reauth_expires_at timestamptz
)
LANGUAGE plpgsql SECURITY DEFINER
SET search_path = pg_catalog, cloud_agents_identity
AS $body$
DECLARE
    flow cloud_agents_identity.provider_flows%ROWTYPE;
    client cloud_agents_identity.provider_clients%ROWTYPE;
    invitation cloud_agents_identity.invitations%ROWTYPE;
    method cloud_agents_identity.login_methods%ROWTYPE;
    account cloud_agents_identity.users%ROWTYPE;
    active_session cloud_agents_identity.sessions%ROWTYPE;
    session_user_id text;
    admitted_membership text;
    admitted_binding text;
    method_active boolean := false;
    operation_time timestamptz := clock_timestamp();
BEGIN
    PERFORM cloud_agents_identity.require_principal('cloud_agents_identity_service');
    IF p_state_digest IS NULL OR octet_length(p_state_digest) <> 32
        OR p_application NOT IN ('admin', 'user')
        OR p_asserted_issuer IS NULL OR octet_length(p_asserted_issuer) NOT BETWEEN 1 AND 512
        OR p_asserted_subject IS NULL OR octet_length(p_asserted_subject) NOT BETWEEN 1 AND 512
        OR p_email_verified IS NULL
        OR (p_asserted_email IS NOT NULL AND octet_length(p_asserted_email) NOT BETWEEN 3 AND 254)
        OR p_new_user_id IS NULL OR NOT cloud_agents.is_valid_identifier(p_new_user_id)
        OR p_login_method_id IS NULL OR NOT cloud_agents.is_valid_identifier(p_login_method_id)
        OR p_event_id IS NULL OR p_event_id !~ '^[a-f0-9]{32}$'
        OR p_correlation_id IS NULL OR NOT cloud_agents.is_valid_identifier(p_correlation_id)
    THEN
        RAISE EXCEPTION USING ERRCODE = '22023', MESSAGE = 'invalid provider callback';
    END IF;
    SELECT candidate.* INTO flow
        FROM cloud_agents_identity.provider_flows AS candidate
        WHERE candidate.state_digest = p_state_digest
        FOR UPDATE;
    IF NOT FOUND OR flow.application <> p_application OR flow.consumed_at IS NULL
        OR flow.completed_at IS NOT NULL OR flow.expires_at <= operation_time
    THEN
        RAISE EXCEPTION USING ERRCODE = '28000', MESSAGE = 'provider flow unavailable';
    END IF;
    SELECT candidate.* INTO client
        FROM cloud_agents_identity.provider_clients AS candidate
        WHERE candidate.id = flow.provider_id
            AND candidate.application = flow.application
            AND candidate.resource_version = flow.provider_revision
            AND candidate.enabled
        FOR SHARE;
    IF NOT FOUND OR client.issuer <> p_asserted_issuer THEN
        RAISE EXCEPTION USING ERRCODE = '40001', MESSAGE = 'provider configuration changed';
    END IF;

	IF (flow.purpose IN ('login', 'invitation', 'reauth') AND (p_session_digest IS NULL OR octet_length(p_session_digest) <> 32))
		OR (flow.purpose = 'link' AND p_session_digest IS NOT NULL)
	THEN
		RAISE EXCEPTION USING ERRCODE = '22023', MESSAGE = 'invalid provider session rotation';
	END IF;

    SELECT candidate.* INTO method
        FROM cloud_agents_identity.login_methods AS candidate
        WHERE candidate.issuer = p_asserted_issuer
            AND candidate.subject = p_asserted_subject
        FOR UPDATE;
    method_active := method.id IS NOT NULL AND method.revoked_at IS NULL;
    IF method_active THEN
        SELECT candidate.* INTO account
            FROM cloud_agents_identity.users AS candidate
            WHERE candidate.id = method.user_id FOR UPDATE;
        IF account.disabled_at IS NOT NULL THEN
            RAISE EXCEPTION USING ERRCODE = '28000', MESSAGE = 'identity account unavailable';
        END IF;
    END IF;

    IF flow.purpose = 'login' THEN
        IF NOT method_active THEN
            RAISE EXCEPTION USING ERRCODE = '28000', MESSAGE = 'provider login unavailable';
        END IF;
        INSERT INTO cloud_agents_identity.sessions (digest, user_id, application, expires_at)
            VALUES (p_session_digest, account.id, p_application, operation_time + interval '12 hours');
        INSERT INTO cloud_agents_identity.audit_events (
            id, event_kind, user_id, actor_user_id, target_user_id,
            application, decision, reason_code, correlation_id
        ) VALUES (
            p_event_id, 'provider_login_succeeded', account.id, account.id, account.id,
            p_application, 'allow', 'provider_authenticated', p_correlation_id
        );
    ELSIF flow.purpose = 'invitation' THEN
        SELECT candidate.* INTO invitation
            FROM cloud_agents_identity.invitations AS candidate
            WHERE candidate.code_digest = flow.invitation_digest FOR UPDATE;
        IF NOT FOUND OR invitation.state <> 'pending' OR invitation.expires_at <= operation_time
            OR invitation.verification <> 'provider-required'
            OR NOT p_email_verified OR p_asserted_email IS DISTINCT FROM invitation.email
        THEN
            RAISE EXCEPTION USING ERRCODE = '42501', MESSAGE = 'invitation provider identity denied';
        END IF;
        IF method.user_id IS NULL THEN
            IF flow.display_name IS NULL OR EXISTS (
                SELECT 1 FROM cloud_agents_identity.users AS existing
                WHERE existing.email = invitation.email
            ) THEN
                RAISE EXCEPTION USING ERRCODE = '40001', MESSAGE = 'explicit identity link required';
            END IF;
            INSERT INTO cloud_agents_identity.users (
                id, email, display_name, email_verified_at
            ) VALUES (
                p_new_user_id, invitation.email, flow.display_name, operation_time
            ) RETURNING * INTO account;
            INSERT INTO cloud_agents_identity.login_methods (
                id, user_id, provider_id, issuer, subject
            ) VALUES (
                p_login_method_id, account.id, flow.provider_id,
                p_asserted_issuer, p_asserted_subject
            ) ON CONFLICT (issuer, subject) DO UPDATE SET
                id = excluded.id, user_id = excluded.user_id,
                provider_id = excluded.provider_id, created_at = operation_time,
                revoked_at = NULL
                WHERE cloud_agents_identity.login_methods.revoked_at IS NOT NULL
            RETURNING * INTO method;
        ELSIF account.email IS DISTINCT FROM invitation.email THEN
            RAISE EXCEPTION USING ERRCODE = '42501', MESSAGE = 'invitation account mismatch';
        END IF;
        IF NOT cloud_agents_identity.invitation_domain_allowed(
            invitation.tenant_id, invitation.email_domain, account.id
        ) THEN
            RAISE EXCEPTION USING ERRCODE = '42501', MESSAGE = 'invitation email policy denied';
        END IF;
        SELECT admission.membership_uid, admission.role_binding_uid
            INTO admitted_membership, admitted_binding
            FROM cloud_agents.accept_identity_invitation_v1(
                invitation.id, account.id, p_membership_id, p_role_binding_id,
                p_membership_audit_id, p_role_binding_audit_id
            ) AS admission;
        IF admitted_membership IS NULL OR admitted_binding IS NULL THEN
            RAISE EXCEPTION USING ERRCODE = '40001', MESSAGE = 'invitation admission conflict';
        END IF;
        UPDATE cloud_agents_identity.invitations AS accepted
            SET state = 'accepted', accepted_by_user_id = account.id, accepted_at = operation_time
            WHERE accepted.id = invitation.id AND accepted.state = 'pending';
        IF NOT FOUND THEN
            RAISE EXCEPTION USING ERRCODE = '40001', MESSAGE = 'invitation state conflict';
        END IF;
        PERFORM cloud_agents_identity.clear_invitation_accept_attempt(
            flow.invitation_digest, flow.invitation_ip_bucket
        );
        INSERT INTO cloud_agents_identity.sessions (digest, user_id, application, expires_at)
            VALUES (p_session_digest, account.id, p_application, operation_time + interval '12 hours');
        INSERT INTO cloud_agents_identity.audit_events (
            id, event_kind, user_id, actor_user_id, target_user_id,
            tenant_id, application, decision, reason_code, correlation_id
        ) VALUES (
            p_event_id, 'invitation_accepted', account.id, account.id, account.id,
            invitation.tenant_id, p_application, 'allow', 'invitation_accepted', p_correlation_id
        );
    ELSIF flow.purpose IN ('reauth', 'link') THEN
        SELECT session.* INTO active_session
            FROM cloud_agents_identity.sessions AS session
            JOIN cloud_agents_identity.users AS current_account ON current_account.id = session.user_id
            WHERE session.digest = flow.session_digest
                AND session.application = p_application
                AND session.revoked_at IS NULL AND session.expires_at > operation_time
                AND session.last_seen_at > operation_time - interval '30 minutes'
                AND current_account.disabled_at IS NULL
            FOR UPDATE OF current_account;
		session_user_id := active_session.user_id;
        IF session_user_id IS NULL THEN
            RAISE EXCEPTION USING ERRCODE = '28000', MESSAGE = 'identity session unavailable';
        END IF;
        IF flow.purpose = 'reauth' THEN
            IF NOT method_active OR method.user_id IS DISTINCT FROM session_user_id THEN
                RAISE EXCEPTION USING ERRCODE = '28000', MESSAGE = 'provider reauthentication unavailable';
            END IF;
			UPDATE cloud_agents_identity.sessions AS old_session
				SET revoked_at = operation_time
				WHERE old_session.digest = active_session.digest AND old_session.revoked_at IS NULL;
			IF NOT FOUND THEN
				RAISE EXCEPTION USING ERRCODE = '40001', MESSAGE = 'identity session rotation conflict';
			END IF;
			INSERT INTO cloud_agents_identity.sessions (
				digest, user_id, application, created_at, last_seen_at, expires_at
			) VALUES (
				p_session_digest, session_user_id, p_application,
				active_session.created_at, operation_time, active_session.expires_at
			);
			INSERT INTO cloud_agents_identity.reauth_grants (
                digest, user_id, session_digest, expires_at
            ) VALUES (
				p_reauth_digest, session_user_id, p_session_digest,
                operation_time + interval '5 minutes'
            );
            SELECT current_account.* INTO account
                FROM cloud_agents_identity.users AS current_account
                WHERE current_account.id = session_user_id;
            INSERT INTO cloud_agents_identity.audit_events (
                id, event_kind, user_id, actor_user_id, target_user_id,
                application, decision, reason_code, correlation_id
            ) VALUES (
                p_event_id, 'provider_reauth_succeeded', session_user_id, session_user_id, session_user_id,
                p_application, 'allow', 'provider_reauthenticated', p_correlation_id
            );
        ELSE
            IF method_active THEN
                RAISE EXCEPTION USING ERRCODE = '23505', MESSAGE = 'provider subject already linked';
            END IF;
            INSERT INTO cloud_agents_identity.login_methods (
                id, user_id, provider_id, issuer, subject
            ) VALUES (
                p_login_method_id, session_user_id, flow.provider_id,
                p_asserted_issuer, p_asserted_subject
            ) ON CONFLICT (issuer, subject) DO UPDATE SET
                id = excluded.id, user_id = excluded.user_id,
                provider_id = excluded.provider_id, created_at = operation_time,
                revoked_at = NULL
                WHERE cloud_agents_identity.login_methods.revoked_at IS NOT NULL
            RETURNING * INTO method;
            SELECT current_account.* INTO account
                FROM cloud_agents_identity.users AS current_account
                WHERE current_account.id = session_user_id;
            INSERT INTO cloud_agents_identity.audit_events (
                id, event_kind, user_id, actor_user_id, target_user_id,
                application, decision, reason_code, correlation_id
            ) VALUES (
                p_event_id, 'login_method_linked', session_user_id, session_user_id, session_user_id,
                p_application, 'allow', 'login_method_linked', p_correlation_id
            );
        END IF;
    ELSE
        RAISE EXCEPTION USING ERRCODE = '22023', MESSAGE = 'invalid provider flow purpose';
    END IF;

    UPDATE cloud_agents_identity.provider_flows AS completed
        SET completed_at = operation_time
        WHERE completed.state_digest = flow.state_digest AND completed.completed_at IS NULL;
    IF NOT FOUND THEN
        RAISE EXCEPTION USING ERRCODE = '40001', MESSAGE = 'provider flow conflict';
    END IF;
    RETURN QUERY SELECT flow.purpose, account.id, account.email, account.display_name,
        EXISTS (
            SELECT 1 FROM cloud_agents_identity.platform_admins AS admin
            WHERE admin.user_id = account.id AND admin.revoked_at IS NULL
        ), method.id, method.provider_id, method.issuer, method.subject, method.created_at,
        CASE WHEN flow.purpose = 'reauth' THEN operation_time + interval '5 minutes' ELSE NULL::timestamptz END;
END
$body$;

CREATE FUNCTION cloud_agents_identity.list_login_methods(
    p_session_digest bytea, p_application text
) RETURNS TABLE (
    password_enabled boolean, login_method_id text, provider_id text, issuer text,
    subject text, created_at timestamptz
)
LANGUAGE plpgsql SECURITY DEFINER
SET search_path = pg_catalog, cloud_agents_identity
AS $body$
DECLARE
    account_id text;
BEGIN
    PERFORM cloud_agents_identity.require_principal('cloud_agents_identity_service');
    SELECT session.user_id INTO account_id
        FROM cloud_agents_identity.read_session(p_session_digest, p_application) AS session;
    IF account_id IS NULL THEN
        RAISE EXCEPTION USING ERRCODE = '28000', MESSAGE = 'identity session unavailable';
    END IF;
    RETURN QUERY SELECT
        EXISTS (SELECT 1 FROM cloud_agents_identity.password_credentials AS credential WHERE credential.user_id = account_id),
        method.id, method.provider_id, method.issuer, method.subject, method.created_at
        FROM (SELECT account_id AS id) AS current_account
        LEFT JOIN LATERAL (
            SELECT candidate.id, candidate.provider_id, candidate.issuer,
                candidate.subject, candidate.created_at
            FROM cloud_agents_identity.login_methods AS candidate
            WHERE candidate.user_id = current_account.id AND candidate.revoked_at IS NULL
            ORDER BY candidate.id LIMIT 64
        ) AS method ON true
        ORDER BY method.id;
END
$body$;

CREATE FUNCTION cloud_agents_identity.prepare_password_reauth(
    p_session_digest bytea, p_application text
) RETURNS TABLE (user_id text, password_hash text, locked boolean)
LANGUAGE plpgsql SECURITY DEFINER
SET search_path = pg_catalog, cloud_agents_identity
AS $body$
DECLARE
    account_id text;
    operation_time timestamptz := clock_timestamp();
BEGIN
    PERFORM cloud_agents_identity.require_principal('cloud_agents_identity_service');
    SELECT session.user_id INTO account_id
        FROM cloud_agents_identity.sessions AS session
        JOIN cloud_agents_identity.users AS account ON account.id = session.user_id
        WHERE session.digest = p_session_digest AND session.application = p_application
            AND session.revoked_at IS NULL AND session.expires_at > operation_time
            AND session.last_seen_at > operation_time - interval '30 minutes'
            AND account.disabled_at IS NULL
        FOR UPDATE OF account;
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
    RETURN QUERY SELECT account_id,
        (SELECT credential.password_hash
            FROM cloud_agents_identity.password_credentials AS credential
            WHERE credential.user_id = account_id),
        failure.locked_until IS NOT NULL AND failure.locked_until > operation_time
        FROM cloud_agents_identity.password_reauth_failures AS failure
        WHERE failure.session_digest = p_session_digest;
END
$body$;

CREATE FUNCTION cloud_agents_identity.finish_password_reauth(
    p_session_digest bytea, p_application text, p_expected_password_hash text,
	p_success boolean, p_reauth_digest bytea, p_new_session_digest bytea,
    p_event_id text, p_correlation_id text
) RETURNS TABLE (outcome text, expires_at timestamptz)
LANGUAGE plpgsql SECURITY DEFINER
SET search_path = pg_catalog, cloud_agents_identity
AS $body$
DECLARE
    account_id text;
    operation_time timestamptz := clock_timestamp();
    failure_count integer;
	original_created_at timestamptz;
	original_expires_at timestamptz;
BEGIN
    PERFORM cloud_agents_identity.require_principal('cloud_agents_identity_service');
	SELECT credential.user_id, session.created_at, session.expires_at
		INTO account_id, original_created_at, original_expires_at
        FROM cloud_agents_identity.password_credentials AS credential
        JOIN cloud_agents_identity.sessions AS session ON session.user_id = credential.user_id
        JOIN cloud_agents_identity.users AS account ON account.id = session.user_id
        WHERE session.digest = p_session_digest AND session.application = p_application
            AND session.revoked_at IS NULL AND session.expires_at > operation_time
            AND session.last_seen_at > operation_time - interval '30 minutes'
            AND account.disabled_at IS NULL
            AND credential.password_hash = p_expected_password_hash
        FOR UPDATE OF account, credential;
    IF account_id IS NULL OR p_success IS NULL
        OR p_event_id IS NULL OR p_event_id !~ '^[a-f0-9]{32}$'
        OR p_correlation_id IS NULL OR NOT cloud_agents.is_valid_identifier(p_correlation_id)
		OR (p_success AND (p_reauth_digest IS NULL OR octet_length(p_reauth_digest) <> 32
			OR p_new_session_digest IS NULL OR octet_length(p_new_session_digest) <> 32))
		OR (NOT p_success AND (p_reauth_digest IS NOT NULL OR p_new_session_digest IS NOT NULL))
    THEN
        RAISE EXCEPTION USING ERRCODE = '28000', MESSAGE = 'password reauthentication unavailable';
    END IF;
    IF NOT p_success THEN
        UPDATE cloud_agents_identity.password_reauth_failures AS failure
            SET failures = LEAST(failure.failures + 1, 10),
                locked_until = CASE WHEN failure.failures + 1 >= 10
                    THEN operation_time + interval '15 minutes' ELSE failure.locked_until END
            WHERE failure.session_digest = p_session_digest
            RETURNING failures INTO failure_count;
        RETURN QUERY SELECT CASE WHEN failure_count >= 10 THEN 'locked' ELSE 'denied' END, NULL::timestamptz;
        RETURN;
    END IF;
	DELETE FROM cloud_agents_identity.password_reauth_failures WHERE session_digest = p_session_digest;
	UPDATE cloud_agents_identity.sessions AS old_session SET revoked_at = operation_time
		WHERE old_session.digest = p_session_digest AND old_session.revoked_at IS NULL;
	IF NOT FOUND THEN
		RAISE EXCEPTION USING ERRCODE = '40001', MESSAGE = 'identity session rotation conflict';
	END IF;
	INSERT INTO cloud_agents_identity.sessions (
		digest, user_id, application, created_at, last_seen_at, expires_at
	) VALUES (
		p_new_session_digest, account_id, p_application,
		original_created_at, operation_time, original_expires_at
	);
	INSERT INTO cloud_agents_identity.reauth_grants (
        digest, user_id, session_digest, expires_at
    ) VALUES (
		p_reauth_digest, account_id, p_new_session_digest, operation_time + interval '5 minutes'
    );
    INSERT INTO cloud_agents_identity.audit_events (
        id, event_kind, user_id, actor_user_id, target_user_id,
        application, decision, reason_code, correlation_id
    ) VALUES (
        p_event_id, 'password_reauth_succeeded', account_id, account_id, account_id,
        p_application, 'allow', 'password_reauthenticated', p_correlation_id
    );
    RETURN QUERY SELECT 'reauthenticated'::text, operation_time + interval '5 minutes';
END
$body$;

CREATE FUNCTION cloud_agents_identity.enable_password(
    p_session_digest bytea, p_application text,
    p_reauth_digest bytea, p_password_hash text,
    p_event_id text, p_correlation_id text
) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER
SET search_path = pg_catalog, cloud_agents_identity
AS $body$
DECLARE
    account_id text;
    operation_time timestamptz := clock_timestamp();
BEGIN
    PERFORM cloud_agents_identity.require_principal('cloud_agents_identity_service');
    IF p_reauth_digest IS NULL OR octet_length(p_reauth_digest) <> 32
        OR p_password_hash IS NULL OR octet_length(p_password_hash) NOT BETWEEN 1 AND 256
        OR p_event_id IS NULL OR p_event_id !~ '^[a-f0-9]{32}$'
        OR p_correlation_id IS NULL OR NOT cloud_agents.is_valid_identifier(p_correlation_id)
    THEN
        RAISE EXCEPTION USING ERRCODE = '22023', MESSAGE = 'invalid password enable';
    END IF;
    SELECT session.user_id INTO account_id
        FROM cloud_agents_identity.sessions AS session
        JOIN cloud_agents_identity.users AS account ON account.id = session.user_id
        WHERE session.digest = p_session_digest AND session.application = p_application
            AND session.revoked_at IS NULL AND session.expires_at > operation_time
            AND session.last_seen_at > operation_time - interval '30 minutes'
            AND account.disabled_at IS NULL
        FOR UPDATE OF account;
    IF account_id IS NULL THEN
        RAISE EXCEPTION USING ERRCODE = '28000', MESSAGE = 'identity session unavailable';
    END IF;
    UPDATE cloud_agents_identity.reauth_grants AS grant_row SET consumed_at = operation_time
        WHERE grant_row.digest = p_reauth_digest AND grant_row.user_id = account_id
            AND grant_row.session_digest = p_session_digest
            AND grant_row.consumed_at IS NULL AND grant_row.expires_at > operation_time;
    IF NOT FOUND THEN
        RAISE EXCEPTION USING ERRCODE = '28000', MESSAGE = 'reauthentication unavailable';
    END IF;
    INSERT INTO cloud_agents_identity.password_credentials (user_id, password_hash)
        VALUES (account_id, p_password_hash)
        ON CONFLICT (user_id) DO NOTHING;
    IF NOT FOUND THEN
        RAISE EXCEPTION USING ERRCODE = '40001', MESSAGE = 'password credential already enabled';
    END IF;
    UPDATE cloud_agents_identity.sessions AS session
        SET revoked_at = operation_time
        WHERE session.user_id = account_id AND session.revoked_at IS NULL;
    INSERT INTO cloud_agents_identity.audit_events (
        id, event_kind, user_id, actor_user_id, target_user_id,
        application, decision, reason_code, correlation_id
    ) VALUES (
        p_event_id, 'password_changed', account_id, account_id, account_id,
        p_application, 'allow', 'password_changed', p_correlation_id
    );
    RETURN true;
END
$body$;

CREATE FUNCTION cloud_agents_identity.unlink_login_method(
    p_session_digest bytea, p_application text,
    p_login_method_id text, p_reauth_digest bytea,
    p_event_id text, p_correlation_id text
) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER
SET search_path = pg_catalog, cloud_agents_identity
AS $body$
DECLARE
    account_id text;
    operation_time timestamptz := clock_timestamp();
BEGIN
    PERFORM cloud_agents_identity.require_principal('cloud_agents_identity_service');
    IF p_login_method_id IS NULL OR NOT cloud_agents.is_valid_identifier(p_login_method_id)
        OR p_reauth_digest IS NULL OR octet_length(p_reauth_digest) <> 32
        OR p_event_id IS NULL OR p_event_id !~ '^[a-f0-9]{32}$'
        OR p_correlation_id IS NULL OR NOT cloud_agents.is_valid_identifier(p_correlation_id)
    THEN
        RAISE EXCEPTION USING ERRCODE = '22023', MESSAGE = 'invalid login method unlink';
    END IF;
    SELECT session.user_id INTO account_id
        FROM cloud_agents_identity.sessions AS session
        JOIN cloud_agents_identity.users AS account ON account.id = session.user_id
        WHERE session.digest = p_session_digest AND session.application = p_application
            AND session.revoked_at IS NULL AND session.expires_at > operation_time
            AND session.last_seen_at > operation_time - interval '30 minutes'
            AND account.disabled_at IS NULL
        FOR UPDATE OF account;
    IF account_id IS NULL THEN
        RAISE EXCEPTION USING ERRCODE = '28000', MESSAGE = 'identity session unavailable';
    END IF;
    UPDATE cloud_agents_identity.reauth_grants AS grant_row SET consumed_at = operation_time
        WHERE grant_row.digest = p_reauth_digest AND grant_row.user_id = account_id
            AND grant_row.session_digest = p_session_digest
            AND grant_row.consumed_at IS NULL AND grant_row.expires_at > operation_time;
    IF NOT FOUND THEN
        RAISE EXCEPTION USING ERRCODE = '28000', MESSAGE = 'reauthentication unavailable';
    END IF;
    PERFORM 1 FROM cloud_agents_identity.login_methods AS method
        WHERE method.id = p_login_method_id AND method.user_id = account_id
            AND method.revoked_at IS NULL FOR UPDATE;
    IF NOT FOUND THEN
        RAISE EXCEPTION USING ERRCODE = 'P0002', MESSAGE = 'login method unavailable';
    END IF;
    IF NOT EXISTS (
        SELECT 1 FROM cloud_agents_identity.password_credentials AS credential
        WHERE credential.user_id = account_id
    ) AND NOT EXISTS (
        SELECT 1 FROM cloud_agents_identity.login_methods AS other
        WHERE other.user_id = account_id AND other.revoked_at IS NULL
            AND other.id <> p_login_method_id
    ) THEN
        RAISE EXCEPTION USING ERRCODE = '40001', MESSAGE = 'last login method cannot be unlinked';
    END IF;
    UPDATE cloud_agents_identity.login_methods SET revoked_at = operation_time
        WHERE id = p_login_method_id AND user_id = account_id AND revoked_at IS NULL;
    INSERT INTO cloud_agents_identity.audit_events (
        id, event_kind, user_id, actor_user_id, target_user_id,
        application, decision, reason_code, correlation_id
    ) VALUES (
        p_event_id, 'login_method_unlinked', account_id, account_id, account_id,
        p_application, 'allow', 'login_method_unlinked', p_correlation_id
    );
    RETURN true;
END
$body$;

REVOKE ALL ON TABLE cloud_agents_identity.provider_clients FROM PUBLIC;
REVOKE ALL ON TABLE cloud_agents_identity.login_methods FROM PUBLIC;
REVOKE ALL ON TABLE cloud_agents_identity.reauth_grants FROM PUBLIC;
REVOKE ALL ON TABLE cloud_agents_identity.provider_flows FROM PUBLIC;
REVOKE ALL ON FUNCTION cloud_agents_identity.lock_password_credential_user() FROM PUBLIC;
REVOKE ALL ON FUNCTION cloud_agents_identity.list_public_provider_clients(text) FROM PUBLIC;
REVOKE ALL ON FUNCTION cloud_agents_identity.list_provider_clients(bytea) FROM PUBLIC;
REVOKE ALL ON FUNCTION cloud_agents_identity.upsert_provider_client(bytea, text, text, text, text, text, text, text, text, text, text, text[], boolean, text[], boolean, bigint, text, text) FROM PUBLIC;
REVOKE ALL ON FUNCTION cloud_agents_identity.read_provider_client(text, text) FROM PUBLIC;
REVOKE ALL ON FUNCTION cloud_agents_identity.create_provider_flow(bytea, bytea, bytea, text, text, bigint, text, bytea, bytea, bytea, bytea, text, timestamptz) FROM PUBLIC;
REVOKE ALL ON FUNCTION cloud_agents_identity.consume_provider_flow(text, bytea, bytea, bytea) FROM PUBLIC;
REVOKE ALL ON FUNCTION cloud_agents_identity.finish_provider_callback(bytea, text, text, text, text, boolean, bytea, text, text, bytea, text, text, text, text, text, text) FROM PUBLIC;
REVOKE ALL ON FUNCTION cloud_agents_identity.list_login_methods(bytea, text) FROM PUBLIC;
REVOKE ALL ON FUNCTION cloud_agents_identity.prepare_password_reauth(bytea, text) FROM PUBLIC;
REVOKE ALL ON FUNCTION cloud_agents_identity.finish_password_reauth(bytea, text, text, boolean, bytea, bytea, text, text) FROM PUBLIC;
REVOKE ALL ON FUNCTION cloud_agents_identity.unlink_login_method(bytea, text, text, bytea, text, text) FROM PUBLIC;
REVOKE ALL ON FUNCTION cloud_agents_identity.enable_password(bytea, text, bytea, text, text, text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION cloud_agents_identity.list_public_provider_clients(text) TO cloud_agents_identity_service;
GRANT EXECUTE ON FUNCTION cloud_agents_identity.list_provider_clients(bytea) TO cloud_agents_identity_service;
GRANT EXECUTE ON FUNCTION cloud_agents_identity.upsert_provider_client(bytea, text, text, text, text, text, text, text, text, text, text, text[], boolean, text[], boolean, bigint, text, text) TO cloud_agents_identity_service;
GRANT EXECUTE ON FUNCTION cloud_agents_identity.read_provider_client(text, text) TO cloud_agents_identity_service;
GRANT EXECUTE ON FUNCTION cloud_agents_identity.create_provider_flow(bytea, bytea, bytea, text, text, bigint, text, bytea, bytea, bytea, bytea, text, timestamptz) TO cloud_agents_identity_service;
GRANT EXECUTE ON FUNCTION cloud_agents_identity.consume_provider_flow(text, bytea, bytea, bytea) TO cloud_agents_identity_service;
GRANT EXECUTE ON FUNCTION cloud_agents_identity.finish_provider_callback(bytea, text, text, text, text, boolean, bytea, text, text, bytea, text, text, text, text, text, text) TO cloud_agents_identity_service;
GRANT EXECUTE ON FUNCTION cloud_agents_identity.list_login_methods(bytea, text) TO cloud_agents_identity_service;
GRANT EXECUTE ON FUNCTION cloud_agents_identity.prepare_password_reauth(bytea, text) TO cloud_agents_identity_service;
GRANT EXECUTE ON FUNCTION cloud_agents_identity.finish_password_reauth(bytea, text, text, boolean, bytea, bytea, text, text) TO cloud_agents_identity_service;
GRANT EXECUTE ON FUNCTION cloud_agents_identity.unlink_login_method(bytea, text, text, bytea, text, text) TO cloud_agents_identity_service;
GRANT EXECUTE ON FUNCTION cloud_agents_identity.enable_password(bytea, text, bytea, text, text, text) TO cloud_agents_identity_service;
