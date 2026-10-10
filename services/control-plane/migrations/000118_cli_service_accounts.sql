ALTER TABLE cloud_agents_identity.audit_events DROP CONSTRAINT audit_events_event_kind_check;
ALTER TABLE cloud_agents_identity.audit_events ADD CONSTRAINT audit_events_event_kind_check
    CHECK (event_kind IN ('realm_initialized', 'password_login_succeeded', 'password_login_denied', 'session_revoked', 'token_issued', 'policy_updated', 'invitation_created', 'invitation_revoked', 'invitation_accepted', 'account_disabled', 'password_reset_issued', 'password_reset_accepted', 'password_changed', 'password_change_denied', 'provider_config_updated', 'provider_login_succeeded', 'provider_reauth_succeeded', 'password_reauth_succeeded', 'login_method_linked', 'login_method_unlinked', 'cli_authorization_approved', 'cli_grant_issued', 'cli_grant_revoked', 'cli_token_issued', 'service_account_created', 'service_account_create_denied', 'service_account_credential_rotated', 'service_account_credential_rotation_denied', 'service_account_disabled', 'service_account_disable_denied', 'service_account_token_issued'));
ALTER TABLE cloud_agents_identity.audit_events DROP CONSTRAINT audit_events_reason_code_check;
ALTER TABLE cloud_agents_identity.audit_events ADD CONSTRAINT audit_events_reason_code_check
    CHECK (reason_code IN ('initialized', 'authenticated', 'invalid_credentials', 'locked', 'logged_out', 'issued', 'policy_updated', 'invitation_created', 'invitation_revoked', 'invitation_accepted', 'account_disabled', 'password_reset_issued', 'password_reset_accepted', 'password_changed', 'provider_config_updated', 'provider_authenticated', 'provider_reauthenticated', 'password_reauthenticated', 'login_method_linked', 'login_method_unlinked', 'cli_authorization_approved', 'cli_grant_issued', 'cli_grant_revoked', 'cli_token_issued', 'service_account_created', 'service_account_credential_rotated', 'service_account_disabled', 'service_account_token_issued', 'authorization_denied', 'conflict'));

ALTER TABLE cloud_agents.admin_denied_writes
    DROP CONSTRAINT admin_denied_writes_action_check;
ALTER TABLE cloud_agents.admin_denied_writes
    ADD CONSTRAINT admin_denied_writes_action_check CHECK (action IN (
        'adminUpgradeEnvironmentLease', 'adminRollbackEnvironmentLease', 'adminRegisterWorkerRelease',
        'adminSetStoragePolicy', 'adminSetNetworkPolicy', 'adminSetProjectLeaseQuota',
        'adminCreateEnvironmentProfile', 'adminPublishEnvironmentProfile', 'adminDisableEnvironmentProfile',
        'adminCreateRuntimeProfile', 'adminPublishRuntimeProfile', 'adminDisableRuntimeProfile',
        'adminRegisterDeploymentTarget', 'adminProbeDeploymentTarget',
        'adminTransitionDeploymentTargetScheduling', 'adminCleanupDeploymentTarget',
        'adminStopSandboxSession', 'adminRebuildSandboxSession', 'adminCorrectSandboxUsage',
        'adminRevokeSandboxAccessGrant', 'adminCreateWorkspaceSnapshot', 'adminRestoreWorkspaceSnapshot',
        'adminCleanupWorkspaceSnapshot', 'adminCreateRemoteWorkerEnrollment',
        'adminRevokeRemoteWorkerEnrollment', 'adminTransitionRemoteWorkerScheduling',
        'adminCreateMcpServer', 'adminRevokeMcpServer',
        'adminCreateSkillBundle', 'adminRevokeSkillBundle'
    ));

CREATE TABLE cloud_agents_identity.cli_authorizations (
    id text PRIMARY KEY CHECK (cloud_agents.is_valid_identifier(id)),
    application text NOT NULL CHECK (application IN ('admin', 'user')),
    callback_port integer NOT NULL CHECK (callback_port BETWEEN 1024 AND 65535),
    state_sha256 bytea NOT NULL CHECK (octet_length(state_sha256) = 32),
    code_challenge text NOT NULL CHECK (code_challenge ~ '^[A-Za-z0-9_-]{43}$'),
    approved_user_id text REFERENCES cloud_agents_identity.users (id),
    approved_session_digest bytea REFERENCES cloud_agents_identity.sessions (digest),
    code_digest bytea UNIQUE CHECK (code_digest IS NULL OR octet_length(code_digest) = 32),
    created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    expires_at timestamptz NOT NULL,
    approved_at timestamptz,
    exchanged_at timestamptz,
    CHECK (expires_at > created_at AND expires_at <= created_at + interval '5 minutes'),
    CHECK ((approved_user_id IS NULL) = (approved_session_digest IS NULL)),
    CHECK ((approved_at IS NULL) = (code_digest IS NULL)),
    CHECK (exchanged_at IS NULL OR approved_at IS NOT NULL)
);

CREATE TABLE cloud_agents_identity.cli_grants (
    id text PRIMARY KEY CHECK (cloud_agents.is_valid_identifier(id)),
    secret_digest bytea NOT NULL UNIQUE CHECK (octet_length(secret_digest) = 32),
    authorization_id text NOT NULL UNIQUE REFERENCES cloud_agents_identity.cli_authorizations (id),
    user_id text NOT NULL REFERENCES cloud_agents_identity.users (id),
    application text NOT NULL CHECK (application IN ('admin', 'user')),
    created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    last_seen_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    expires_at timestamptz NOT NULL,
    revoked_at timestamptz,
    CHECK (expires_at > created_at AND expires_at <= created_at + interval '30 days'),
    CHECK (last_seen_at >= created_at)
);

CREATE INDEX cli_grants_user_idx
    ON cloud_agents_identity.cli_grants (user_id, application, expires_at);

CREATE TABLE cloud_agents_identity.cli_issued_tokens (
    token_sha256 bytea PRIMARY KEY CHECK (octet_length(token_sha256) = 32),
    jti text NOT NULL UNIQUE CHECK (jti ~ '^[A-Za-z0-9_-]{22}$'),
    grant_id text NOT NULL REFERENCES cloud_agents_identity.cli_grants (id),
    user_id text NOT NULL REFERENCES cloud_agents_identity.users (id),
    issuer text NOT NULL CHECK (length(issuer) BETWEEN 1 AND 512),
    application text NOT NULL CHECK (application IN ('admin', 'user')),
    client_id text NOT NULL CHECK (client_id = 'cloud-agents-cli'),
    tenant_id text NOT NULL CHECK (cloud_agents.is_valid_identifier(tenant_id)),
    project_id text CHECK (project_id IS NULL OR cloud_agents.is_valid_identifier(project_id)),
    issued_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    expires_at timestamptz NOT NULL,
    CHECK (expires_at > issued_at AND expires_at <= issued_at + interval '15 minutes')
);

CREATE INDEX cli_issued_tokens_expiry_idx
    ON cloud_agents_identity.cli_issued_tokens (expires_at, token_sha256);
CREATE INDEX cli_issued_tokens_grant_idx
    ON cloud_agents_identity.cli_issued_tokens (grant_id, expires_at);

CREATE TABLE cloud_agents_identity.cli_anonymous_attempts (
    kind text NOT NULL CHECK (kind IN ('start', 'exchange')),
    ip_bucket bytea NOT NULL CHECK (octet_length(ip_bucket) = 32),
    window_started_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    attempts integer NOT NULL CHECK (attempts BETWEEN 1 AND 101),
    PRIMARY KEY (kind, ip_bucket)
);

CREATE INDEX cli_anonymous_attempts_window_idx
    ON cloud_agents_identity.cli_anonymous_attempts
    (window_started_at, kind, ip_bucket);

CREATE FUNCTION cloud_agents_identity.revoke_cli_grants_after_password_change()
RETURNS trigger
LANGUAGE plpgsql SECURITY DEFINER
SET search_path = pg_catalog, cloud_agents_identity
AS $body$
BEGIN
    IF OLD.password_hash IS DISTINCT FROM NEW.password_hash THEN
        UPDATE cloud_agents_identity.cli_grants AS grant_row
            SET revoked_at = clock_timestamp()
            WHERE grant_row.user_id = NEW.user_id AND grant_row.revoked_at IS NULL;
    END IF;
    RETURN NEW;
END
$body$;

CREATE TRIGGER password_credentials_revoke_cli_grants
    AFTER UPDATE OF password_hash ON cloud_agents_identity.password_credentials
    FOR EACH ROW EXECUTE FUNCTION cloud_agents_identity.revoke_cli_grants_after_password_change();

CREATE FUNCTION cloud_agents_identity.revoke_cli_grants_after_account_disable()
RETURNS trigger
LANGUAGE plpgsql SECURITY DEFINER
SET search_path = pg_catalog, cloud_agents_identity
AS $body$
BEGIN
    IF OLD.disabled_at IS NULL AND NEW.disabled_at IS NOT NULL THEN
        UPDATE cloud_agents_identity.cli_grants AS grant_row
            SET revoked_at = clock_timestamp()
            WHERE grant_row.user_id = NEW.id AND grant_row.revoked_at IS NULL;
    END IF;
    RETURN NEW;
END
$body$;

CREATE TRIGGER users_revoke_cli_grants
    AFTER UPDATE OF disabled_at ON cloud_agents_identity.users
    FOR EACH ROW EXECUTE FUNCTION cloud_agents_identity.revoke_cli_grants_after_account_disable();

CREATE TABLE cloud_agents_identity.service_accounts (
    id text PRIMARY KEY CHECK (cloud_agents.is_valid_identifier(id)),
    tenant_id text NOT NULL,
    tenant_uid text NOT NULL,
    display_name text NOT NULL CHECK (length(display_name) BETWEEN 1 AND 160),
    application text NOT NULL CHECK (application IN ('admin', 'user')),
    management_scope_level text NOT NULL
        CHECK (management_scope_level IN ('tenant', 'organization', 'project')),
    management_scope_id text NOT NULL CHECK (cloud_agents.is_valid_identifier(management_scope_id)),
    membership_uid text NOT NULL CHECK (cloud_agents.is_valid_identifier(membership_uid)),
    role_binding_uid text NOT NULL CHECK (cloud_agents.is_valid_identifier(role_binding_uid)),
    role_name text NOT NULL CHECK (cloud_agents.is_valid_identifier(role_name)),
    role_version bigint NOT NULL CHECK (role_version > 0),
    state text NOT NULL CHECK (state IN ('active', 'disabled')),
    resource_version bigint NOT NULL CHECK (resource_version > 0),
    created_by_user_id text NOT NULL REFERENCES cloud_agents_identity.users (id),
    created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    CONSTRAINT service_accounts_tenant_root CHECK (tenant_id = tenant_uid),
    CONSTRAINT service_accounts_tenant_scope CHECK (
        management_scope_level <> 'tenant' OR management_scope_id = tenant_id
    ),
    CONSTRAINT service_accounts_tenant_fk FOREIGN KEY (tenant_id, tenant_uid)
        REFERENCES cloud_agents.platform_tenants (tenant_id, tenant_uid)
        ON UPDATE RESTRICT ON DELETE RESTRICT,
    CONSTRAINT service_accounts_membership_fk FOREIGN KEY (tenant_id, membership_uid)
        REFERENCES cloud_agents.memberships (tenant_id, membership_uid)
        ON UPDATE RESTRICT ON DELETE RESTRICT,
    CONSTRAINT service_accounts_role_binding_fk FOREIGN KEY (tenant_id, role_binding_uid)
        REFERENCES cloud_agents.role_bindings (tenant_id, role_binding_uid)
        ON UPDATE RESTRICT ON DELETE RESTRICT,
    UNIQUE (tenant_id, id),
    UNIQUE (tenant_id, id, application)
);

CREATE INDEX service_accounts_tenant_page_idx
    ON cloud_agents_identity.service_accounts (tenant_id, id);

CREATE TABLE cloud_agents_identity.service_account_credentials (
    digest bytea PRIMARY KEY CHECK (octet_length(digest) = 32),
    service_account_id text NOT NULL REFERENCES cloud_agents_identity.service_accounts (id),
    credential_version bigint NOT NULL CHECK (credential_version > 0),
    created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    expires_at timestamptz NOT NULL,
    revoked_at timestamptz,
    last_used_at timestamptz,
    CHECK (expires_at > created_at AND expires_at <= created_at + interval '365 days'),
    UNIQUE (service_account_id, credential_version)
);

CREATE INDEX service_account_credentials_account_idx
    ON cloud_agents_identity.service_account_credentials
    (service_account_id, expires_at, credential_version);

CREATE TABLE cloud_agents_identity.service_account_issued_tokens (
    token_sha256 bytea PRIMARY KEY CHECK (octet_length(token_sha256) = 32),
    jti text NOT NULL UNIQUE CHECK (jti ~ '^[A-Za-z0-9_-]{22}$'),
    credential_digest bytea NOT NULL REFERENCES cloud_agents_identity.service_account_credentials (digest),
    service_account_id text NOT NULL REFERENCES cloud_agents_identity.service_accounts (id),
    issuer text NOT NULL CHECK (length(issuer) BETWEEN 1 AND 512),
    application text NOT NULL CHECK (application IN ('admin', 'user')),
    client_id text NOT NULL CHECK (client_id = 'cloud-agents-automation'),
    tenant_id text NOT NULL CHECK (cloud_agents.is_valid_identifier(tenant_id)),
    project_id text CHECK (project_id IS NULL OR cloud_agents.is_valid_identifier(project_id)),
    issued_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    expires_at timestamptz NOT NULL,
    CHECK (expires_at > issued_at AND expires_at <= issued_at + interval '15 minutes')
);

CREATE INDEX service_account_issued_tokens_expiry_idx
    ON cloud_agents_identity.service_account_issued_tokens (expires_at, token_sha256);
CREATE INDEX service_account_issued_tokens_credential_idx
    ON cloud_agents_identity.service_account_issued_tokens (credential_digest, expires_at);

ALTER TABLE cloud_agents_identity.audit_events
    ADD COLUMN actor_service_account_id text;
ALTER TABLE cloud_agents_identity.audit_events
    ADD COLUMN target_service_account_id text;
ALTER TABLE cloud_agents_identity.audit_events
    ADD CONSTRAINT audit_events_actor_service_account_fk
        FOREIGN KEY (tenant_id, actor_service_account_id)
        REFERENCES cloud_agents_identity.service_accounts (tenant_id, id)
        ON UPDATE RESTRICT ON DELETE RESTRICT;
ALTER TABLE cloud_agents_identity.audit_events
    ADD CONSTRAINT audit_events_target_service_account_fk
        FOREIGN KEY (tenant_id, target_service_account_id)
        REFERENCES cloud_agents_identity.service_accounts (tenant_id, id)
        ON UPDATE RESTRICT ON DELETE RESTRICT;
ALTER TABLE cloud_agents_identity.audit_events
    ADD CONSTRAINT audit_events_actor_identity_check CHECK (
        actor_user_id IS NULL OR actor_service_account_id IS NULL
    );

-- Online roles receive only the narrow functions granted at the end of this migration.
-- identity_service:
--   start_cli_authorization(id, application, callback_port, state_sha256, challenge)
--   approve_cli_authorization(session_digest, id, state_sha256, code_digest)
--   exchange_cli_authorization(id, code_digest, challenge, grant_id, grant_digest)
--   revoke_cli_grant(grant_digest)
--   list_cli_tenants(grant_digest, application, after_tenant_id, page_size)
--   record_cli_issued_token(...)
--   record_service_account_issued_token(...)
--   token_is_active(token_digest, application, tenant, project, client_id) [successor signature]
-- cloud_agents_runtime:
--   read_cli_grant_subject(grant_digest, application)
--   read_service_account_subject(credential_digest, application, tenant)
--   create_service_account_record(...), rotate_service_account_credential(...),
--   disable_service_account_record(...), record_service_account_management_denial(...),
--   list_service_account_records(...)
-- identity_service audit projection:
--   list_control_plane_audit_facts(session_digest, tenant, after_occurred_at,
--       after_audit_uid, page_size), which first calls require_tenant_admin and
--       filters audit_facts.tenant_id exactly before bounded keyset pagination.

-- CP creation is one SERIALIZABLE tenant-bound transaction:
-- verified memberships.create -> create_service_account_record -> create_membership_v2;
-- verified role-bindings.bind -> bind_role. Raw 32-byte credential is generated in Go,
-- only its SHA-256 reaches SQL, and raw bytes are returned only after commit.

CREATE FUNCTION cloud_agents_identity.record_cli_anonymous_attempt(
    p_kind text,
    p_ip_bucket bytea
) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER
SET search_path = pg_catalog, cloud_agents_identity
AS $body$
DECLARE
    operation_time timestamptz := clock_timestamp();
    current_attempts integer;
BEGIN
    PERFORM cloud_agents_identity.require_principal('cloud_agents_identity_service');
    IF p_kind NOT IN ('start', 'exchange')
        OR p_ip_bucket IS NULL OR octet_length(p_ip_bucket) <> 32
    THEN
        RAISE EXCEPTION USING ERRCODE = '22023', MESSAGE = 'invalid CLI anonymous attempt';
    END IF;

    WITH expired AS (
        SELECT attempt.kind, attempt.ip_bucket
        FROM cloud_agents_identity.cli_anonymous_attempts AS attempt
        WHERE attempt.window_started_at <= operation_time - interval '15 minutes'
            AND (attempt.kind, attempt.ip_bucket) <> (p_kind, p_ip_bucket)
        ORDER BY attempt.window_started_at, attempt.kind, attempt.ip_bucket
        LIMIT 128 FOR UPDATE SKIP LOCKED
    )
    DELETE FROM cloud_agents_identity.cli_anonymous_attempts AS attempt
    USING expired
    WHERE attempt.kind = expired.kind AND attempt.ip_bucket = expired.ip_bucket;

    INSERT INTO cloud_agents_identity.cli_anonymous_attempts AS attempt (
        kind, ip_bucket, window_started_at, attempts
    ) VALUES (p_kind, p_ip_bucket, operation_time, 1)
    ON CONFLICT (kind, ip_bucket) DO UPDATE
        SET window_started_at = CASE
                WHEN attempt.window_started_at <= operation_time - interval '15 minutes'
                    THEN operation_time
                ELSE attempt.window_started_at
            END,
            attempts = CASE
                WHEN attempt.window_started_at <= operation_time - interval '15 minutes'
                    THEN 1
                ELSE LEAST(attempt.attempts + 1, 101)
            END
    RETURNING attempts INTO current_attempts;
    RETURN current_attempts <= 100;
END
$body$;

CREATE FUNCTION cloud_agents_identity.start_cli_authorization(
    p_id text,
    p_application text,
    p_callback_port integer,
    p_state_sha256 bytea,
    p_code_challenge text
) RETURNS timestamptz
LANGUAGE plpgsql SECURITY DEFINER
SET search_path = pg_catalog, cloud_agents_identity
AS $body$
DECLARE
    operation_time timestamptz := clock_timestamp();
    authorization_expiry timestamptz := operation_time + interval '5 minutes';
BEGIN
    PERFORM cloud_agents_identity.require_principal('cloud_agents_identity_service');
    IF p_id IS NULL OR NOT cloud_agents.is_valid_identifier(p_id)
        OR p_application NOT IN ('admin', 'user')
        OR p_callback_port IS NULL OR p_callback_port NOT BETWEEN 1024 AND 65535
        OR p_state_sha256 IS NULL OR octet_length(p_state_sha256) <> 32
        OR p_code_challenge IS NULL OR p_code_challenge !~ '^[A-Za-z0-9_-]{43}$'
    THEN
        RAISE EXCEPTION USING ERRCODE = '22023', MESSAGE = 'invalid CLI authorization';
    END IF;
    WITH expired AS (
        SELECT auth_row.id
        FROM cloud_agents_identity.cli_authorizations AS auth_row
        WHERE auth_row.expires_at <= operation_time
            AND auth_row.exchanged_at IS NULL
        ORDER BY auth_row.expires_at, auth_row.id
        LIMIT 128 FOR UPDATE SKIP LOCKED
    )
    DELETE FROM cloud_agents_identity.cli_authorizations AS auth_row
    USING expired WHERE auth_row.id = expired.id;

    INSERT INTO cloud_agents_identity.cli_authorizations (
        id, application, callback_port, state_sha256, code_challenge,
        created_at, expires_at
    ) VALUES (
        p_id, p_application, p_callback_port, p_state_sha256, p_code_challenge,
        operation_time, authorization_expiry
    );
    RETURN authorization_expiry;
END
$body$;

CREATE FUNCTION cloud_agents_identity.approve_cli_authorization(
    p_session_digest bytea,
    p_application text,
    p_authorization_id text,
    p_state_sha256 bytea,
    p_code_digest bytea,
    p_event_id text,
    p_correlation_id text
) RETURNS TABLE (callback_port integer, application text, expires_at timestamptz)
LANGUAGE plpgsql SECURITY DEFINER
SET search_path = pg_catalog, cloud_agents_identity
AS $body$
DECLARE
    operation_time timestamptz := clock_timestamp();
    authorization_application text;
    authorization_port integer;
    authorization_expiry timestamptz;
    account_id text;
BEGIN
    PERFORM cloud_agents_identity.require_principal('cloud_agents_identity_service');
    IF p_session_digest IS NULL OR octet_length(p_session_digest) <> 32
        OR p_application NOT IN ('admin', 'user')
        OR p_authorization_id IS NULL OR NOT cloud_agents.is_valid_identifier(p_authorization_id)
        OR p_state_sha256 IS NULL OR octet_length(p_state_sha256) <> 32
        OR p_code_digest IS NULL OR octet_length(p_code_digest) <> 32
        OR p_event_id IS NULL OR p_event_id !~ '^[a-f0-9]{32}$'
        OR p_correlation_id IS NULL OR NOT cloud_agents.is_valid_identifier(p_correlation_id)
    THEN
        RAISE EXCEPTION USING ERRCODE = '22023', MESSAGE = 'invalid CLI authorization approval';
    END IF;

    SELECT auth_row.application, auth_row.callback_port, auth_row.expires_at
        INTO authorization_application, authorization_port, authorization_expiry
    FROM cloud_agents_identity.cli_authorizations AS auth_row
    WHERE auth_row.id = p_authorization_id
        AND auth_row.application = p_application
        AND auth_row.state_sha256 = p_state_sha256
        AND auth_row.expires_at > operation_time
        AND auth_row.approved_at IS NULL
        AND auth_row.exchanged_at IS NULL
    FOR UPDATE;
    IF authorization_application IS NULL THEN
        RAISE EXCEPTION USING ERRCODE = '28000', MESSAGE = 'CLI authorization unavailable';
    END IF;

    SELECT session.user_id INTO account_id
    FROM cloud_agents_identity.read_session(
        p_session_digest,
        p_application
    ) AS session;
    IF account_id IS NULL THEN
        RAISE EXCEPTION USING ERRCODE = '28000', MESSAGE = 'identity session unavailable';
    END IF;

    UPDATE cloud_agents_identity.cli_authorizations AS auth_row
    SET approved_user_id = account_id,
        approved_session_digest = p_session_digest,
        code_digest = p_code_digest,
        approved_at = operation_time
    WHERE auth_row.id = p_authorization_id;

    INSERT INTO cloud_agents_identity.audit_events (
        id, event_kind, user_id, actor_user_id, target_user_id,
        application, decision, reason_code, correlation_id
    ) VALUES (
        p_event_id, 'cli_authorization_approved', account_id, account_id, account_id,
        authorization_application, 'allow', 'cli_authorization_approved', p_correlation_id
    );
    RETURN QUERY SELECT authorization_port, authorization_application, authorization_expiry;
END
$body$;

CREATE FUNCTION cloud_agents_identity.exchange_cli_authorization(
    p_application text,
    p_authorization_id text,
    p_code_digest bytea,
    p_code_challenge text,
    p_grant_id text,
    p_grant_digest bytea,
    p_event_id text,
    p_correlation_id text
) RETURNS TABLE (user_id text, application text, expires_at timestamptz)
LANGUAGE plpgsql SECURITY DEFINER
SET search_path = pg_catalog, cloud_agents_identity
AS $body$
DECLARE
    operation_time timestamptz := clock_timestamp();
    account_id text;
    authorization_application text;
    authorization_session bytea;
    grant_expiry timestamptz := operation_time + interval '30 days';
BEGIN
    PERFORM cloud_agents_identity.require_principal('cloud_agents_identity_service');
    IF p_application NOT IN ('admin', 'user')
        OR p_authorization_id IS NULL OR NOT cloud_agents.is_valid_identifier(p_authorization_id)
        OR p_code_digest IS NULL OR octet_length(p_code_digest) <> 32
        OR p_code_challenge IS NULL OR p_code_challenge !~ '^[A-Za-z0-9_-]{43}$'
        OR p_grant_id IS NULL OR NOT cloud_agents.is_valid_identifier(p_grant_id)
        OR p_grant_digest IS NULL OR octet_length(p_grant_digest) <> 32
        OR p_event_id IS NULL OR p_event_id !~ '^[a-f0-9]{32}$'
        OR p_correlation_id IS NULL OR NOT cloud_agents.is_valid_identifier(p_correlation_id)
    THEN
        RAISE EXCEPTION USING ERRCODE = '22023', MESSAGE = 'invalid CLI authorization exchange';
    END IF;
    SELECT auth_row.approved_user_id, auth_row.application,
        auth_row.approved_session_digest
        INTO account_id, authorization_application, authorization_session
    FROM cloud_agents_identity.cli_authorizations AS auth_row
    JOIN cloud_agents_identity.users AS account
        ON account.id = auth_row.approved_user_id
    WHERE auth_row.id = p_authorization_id
        AND auth_row.application = p_application
        AND auth_row.code_digest = p_code_digest
        AND auth_row.code_challenge = p_code_challenge
        AND auth_row.approved_at IS NOT NULL
        AND auth_row.exchanged_at IS NULL
        AND auth_row.expires_at > operation_time
        AND account.disabled_at IS NULL
    FOR UPDATE OF auth_row;
    IF account_id IS NULL OR NOT EXISTS (
        SELECT 1
        FROM cloud_agents_identity.read_session(
            authorization_session,
            authorization_application
        ) AS session
        WHERE session.user_id = account_id
    ) THEN
        RAISE EXCEPTION USING ERRCODE = '28000', MESSAGE = 'CLI authorization unavailable';
    END IF;

    INSERT INTO cloud_agents_identity.cli_grants (
        id, secret_digest, authorization_id, user_id, application,
        created_at, last_seen_at, expires_at
    ) VALUES (
        p_grant_id, p_grant_digest, p_authorization_id, account_id,
        authorization_application, operation_time, operation_time, grant_expiry
    );
    UPDATE cloud_agents_identity.cli_authorizations
        SET exchanged_at = operation_time
        WHERE id = p_authorization_id;
    INSERT INTO cloud_agents_identity.audit_events (
        id, event_kind, user_id, actor_user_id, target_user_id,
        application, decision, reason_code, correlation_id
    ) VALUES (
        p_event_id, 'cli_grant_issued', account_id, account_id, account_id,
        authorization_application, 'allow', 'cli_grant_issued', p_correlation_id
    );
    RETURN QUERY SELECT account_id, authorization_application, grant_expiry;
END
$body$;

CREATE FUNCTION cloud_agents_identity.revoke_cli_grant(
    p_grant_digest bytea,
    p_application text,
    p_event_id text,
    p_correlation_id text
) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER
SET search_path = pg_catalog, cloud_agents_identity
AS $body$
DECLARE
    operation_time timestamptz := clock_timestamp();
    account_id text;
    grant_application text;
BEGIN
    PERFORM cloud_agents_identity.require_principal('cloud_agents_identity_service');
    IF p_grant_digest IS NULL OR octet_length(p_grant_digest) <> 32
        OR p_application NOT IN ('admin', 'user')
        OR p_event_id IS NULL OR p_event_id !~ '^[a-f0-9]{32}$'
        OR p_correlation_id IS NULL OR NOT cloud_agents.is_valid_identifier(p_correlation_id)
    THEN
        RAISE EXCEPTION USING ERRCODE = '22023', MESSAGE = 'invalid CLI grant revocation';
    END IF;
    UPDATE cloud_agents_identity.cli_grants AS grant_row
        SET revoked_at = operation_time
        WHERE grant_row.secret_digest = p_grant_digest
            AND grant_row.application = p_application
            AND grant_row.revoked_at IS NULL
        RETURNING grant_row.user_id, grant_row.application
        INTO account_id, grant_application;
    IF account_id IS NULL THEN
        RAISE EXCEPTION USING ERRCODE = '28000', MESSAGE = 'CLI grant unavailable';
    END IF;
    INSERT INTO cloud_agents_identity.audit_events (
        id, event_kind, user_id, actor_user_id, target_user_id,
        application, decision, reason_code, correlation_id
    ) VALUES (
        p_event_id, 'cli_grant_revoked', account_id, account_id, account_id,
        grant_application, 'allow', 'cli_grant_revoked', p_correlation_id
    );
    RETURN true;
END
$body$;

CREATE FUNCTION cloud_agents_identity.list_cli_tenants(
    p_grant_digest bytea,
    p_application text,
    p_after text,
    p_limit integer
) RETURNS TABLE (tenant_id text, tenant_name text, tenant_admin boolean)
LANGUAGE plpgsql SECURITY DEFINER
SET search_path = pg_catalog, cloud_agents_identity
AS $body$
DECLARE
    account_id text;
    realm_issuer text;
    account_subject text;
    account_digest text;
    is_platform_admin boolean;
    operation_time timestamptz := clock_timestamp();
BEGIN
    PERFORM cloud_agents_identity.require_principal('cloud_agents_identity_service');
    IF p_grant_digest IS NULL OR octet_length(p_grant_digest) <> 32
        OR p_application NOT IN ('admin', 'user')
        OR p_after IS NULL OR (p_after <> '' AND NOT cloud_agents.is_valid_identifier(p_after))
        OR p_limit IS NULL OR p_limit NOT BETWEEN 1 AND 201
    THEN
        RAISE EXCEPTION USING ERRCODE = '22023', MESSAGE = 'invalid CLI tenant page';
    END IF;
    UPDATE cloud_agents_identity.cli_grants AS grant_row
        SET last_seen_at = operation_time
        FROM cloud_agents_identity.users AS account
        WHERE grant_row.secret_digest = p_grant_digest
            AND grant_row.application = p_application
            AND grant_row.user_id = account.id
            AND grant_row.revoked_at IS NULL
            AND grant_row.expires_at > operation_time
            AND grant_row.last_seen_at > operation_time - interval '7 days'
            AND account.disabled_at IS NULL
        RETURNING grant_row.user_id INTO account_id;
    IF account_id IS NULL THEN
        RAISE EXCEPTION USING ERRCODE = '28000', MESSAGE = 'CLI grant unavailable';
    END IF;
    SELECT realm.issuer INTO STRICT realm_issuer
        FROM cloud_agents_identity.realm AS realm WHERE realm.singleton;
    account_subject := 'user-' || account_id;
    account_digest := cloud_agents.subject_ref_digest('user', realm_issuer, account_subject);
    SELECT EXISTS (
        SELECT 1 FROM cloud_agents_identity.platform_admins AS administrator
        WHERE administrator.user_id = account_id AND administrator.revoked_at IS NULL
    ) INTO is_platform_admin;

    IF p_application = 'admin' AND is_platform_admin THEN
        RETURN QUERY SELECT tenant.tenant_id, tenant.display_name, false
        FROM cloud_agents.platform_tenants AS tenant
        WHERE tenant.tenant_id = tenant.tenant_uid AND tenant.state = 'active'
            AND tenant.tenant_id > p_after
        ORDER BY tenant.tenant_id LIMIT p_limit;
        RETURN;
    END IF;
    RETURN QUERY
        SELECT tenant.tenant_id, tenant.display_name, p_application = 'admin'
        FROM cloud_agents.platform_tenants AS tenant
        WHERE tenant.tenant_id = tenant.tenant_uid AND tenant.state = 'active'
            AND tenant.tenant_id > p_after
            AND EXISTS (
                SELECT 1 FROM cloud_agents.memberships AS membership
                WHERE membership.tenant_id = tenant.tenant_id
                    AND membership.subject_kind = 'user'
                    AND membership.subject_issuer = realm_issuer
                    AND membership.subject_value = account_subject
                    AND membership.subject_digest = account_digest
                    AND membership.state = 'active'
                    AND (membership.expires_at IS NULL OR membership.expires_at > operation_time)
                    AND (p_application = 'user' OR (
                        membership.scope_level = 'tenant'
                        AND membership.scope_tenant_uid = tenant.tenant_id
                        AND EXISTS (
                            SELECT 1
                            FROM cloud_agents.role_bindings AS binding
                            JOIN cloud_agents.builtin_roles AS role
                                ON role.role_name = binding.role_name
                                AND role.role_version = binding.role_version
                            JOIN cloud_agents.resource_changes AS admission
                                ON admission.tenant_id = membership.tenant_id
                                AND admission.resource_kind = 'membership'
                                AND admission.resource_uid = membership.membership_uid
                                AND admission.change_kind = 'created'
                                AND admission.resource_version < binding.resource_version
                            WHERE binding.tenant_id = membership.tenant_id
                                AND binding.subject_kind = membership.subject_kind
                                AND binding.subject_issuer = membership.subject_issuer
                                AND binding.subject_value = membership.subject_value
                                AND binding.subject_digest = membership.subject_digest
                                AND binding.role_name = 'tenant.admin'
                                AND binding.role_version = 1
                                AND binding.scope_level = 'tenant'
                                AND binding.scope_tenant_uid = tenant.tenant_id
                                AND binding.state = 'active'
                                AND (binding.expires_at IS NULL OR binding.expires_at > operation_time)
                                AND role.scope_level = 'tenant' AND role.state = 'active'
                        )
                    ))
            )
        ORDER BY tenant.tenant_id LIMIT p_limit;
END
$body$;

CREATE FUNCTION cloud_agents_identity.read_cli_grant_subject(
    p_grant_digest bytea,
    p_application text
) RETURNS TABLE (
    user_id text, subject_kind text, subject_issuer text,
    subject_value text, subject_digest text
)
LANGUAGE plpgsql SECURITY DEFINER
SET search_path = pg_catalog, cloud_agents_identity
AS $body$
DECLARE operation_time timestamptz := clock_timestamp();
BEGIN
    PERFORM cloud_agents_identity.require_principal('cloud_agents_runtime');
    IF p_grant_digest IS NULL OR octet_length(p_grant_digest) <> 32
        OR p_application NOT IN ('admin', 'user')
    THEN
        RAISE EXCEPTION USING ERRCODE = '22023', MESSAGE = 'invalid CLI grant context';
    END IF;
    RETURN QUERY
        SELECT account.id, 'user'::text, realm.issuer, 'user-' || account.id,
            cloud_agents.subject_ref_digest('user', realm.issuer, 'user-' || account.id)
        FROM cloud_agents_identity.cli_grants AS grant_row
        JOIN cloud_agents_identity.users AS account ON account.id = grant_row.user_id
        CROSS JOIN cloud_agents_identity.realm AS realm
        WHERE grant_row.secret_digest = p_grant_digest
            AND grant_row.application = p_application
            AND grant_row.revoked_at IS NULL
            AND grant_row.expires_at > operation_time
            AND grant_row.last_seen_at > operation_time - interval '7 days'
            AND account.disabled_at IS NULL AND realm.singleton
            AND EXISTS (
                SELECT 1 FROM cloud_agents.platform_tenants AS tenant
                WHERE tenant.tenant_id = cloud_agents.require_tenant_id()
                    AND tenant.tenant_uid = tenant.tenant_id AND tenant.state = 'active'
            );
END
$body$;

CREATE FUNCTION cloud_agents_identity.record_cli_issued_token(
    p_grant_digest bytea, p_user_id text, p_issuer text,
    p_application text, p_tenant_id text, p_project_id text,
    p_jti text, p_token_sha256 bytea, p_expires_at timestamptz,
    p_event_id text, p_correlation_id text
) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER
SET search_path = pg_catalog, cloud_agents_identity
AS $body$
DECLARE operation_time timestamptz := clock_timestamp();
BEGIN
    PERFORM cloud_agents_identity.require_principal('cloud_agents_identity_service');
    IF p_grant_digest IS NULL OR octet_length(p_grant_digest) <> 32
        OR p_user_id IS NULL OR NOT cloud_agents.is_valid_identifier(p_user_id)
        OR p_issuer IS NULL OR octet_length(p_issuer) NOT BETWEEN 1 AND 512
        OR p_application NOT IN ('admin', 'user')
        OR p_tenant_id IS NULL OR NOT cloud_agents.is_valid_identifier(p_tenant_id)
        OR (p_project_id IS NOT NULL AND NOT cloud_agents.is_valid_identifier(p_project_id))
        OR p_jti IS NULL OR p_jti !~ '^[A-Za-z0-9_-]{22}$'
        OR p_token_sha256 IS NULL OR octet_length(p_token_sha256) <> 32
        OR p_expires_at IS NULL OR p_expires_at <= operation_time
        OR p_expires_at > operation_time + interval '15 minutes'
        OR p_event_id IS NULL OR p_event_id !~ '^[a-f0-9]{32}$'
        OR p_correlation_id IS NULL OR NOT cloud_agents.is_valid_identifier(p_correlation_id)
    THEN
        RAISE EXCEPTION USING ERRCODE = '22023', MESSAGE = 'invalid CLI issued token';
    END IF;
    IF NOT EXISTS (
        SELECT 1
        FROM cloud_agents_identity.cli_grants AS grant_row
        JOIN cloud_agents_identity.users AS account ON account.id = grant_row.user_id
        CROSS JOIN cloud_agents_identity.realm AS realm
        WHERE grant_row.secret_digest = p_grant_digest
            AND grant_row.user_id = p_user_id
            AND grant_row.application = p_application
            AND grant_row.revoked_at IS NULL AND grant_row.expires_at > operation_time
            AND grant_row.last_seen_at > operation_time - interval '7 days'
            AND account.disabled_at IS NULL AND realm.singleton AND realm.issuer = p_issuer
    ) OR NOT EXISTS (
        SELECT 1 FROM cloud_agents.platform_tenants AS tenant
        WHERE tenant.tenant_id = p_tenant_id AND tenant.tenant_uid = p_tenant_id
            AND tenant.state = 'active'
    ) OR (p_project_id IS NOT NULL AND NOT EXISTS (
        SELECT 1 FROM cloud_agents.projects AS project
        JOIN cloud_agents.organizations AS organization
            ON organization.tenant_id = project.tenant_id
            AND organization.organization_uid = project.organization_uid
        WHERE project.tenant_id = p_tenant_id AND project.project_uid = p_project_id
            AND project.state = 'active' AND organization.state = 'active'
    )) THEN
        RETURN false;
    END IF;

    UPDATE cloud_agents_identity.cli_grants SET last_seen_at = operation_time
        WHERE secret_digest = p_grant_digest;
    INSERT INTO cloud_agents_identity.cli_issued_tokens (
        token_sha256, jti, grant_id, user_id, issuer, application,
        client_id, tenant_id, project_id, issued_at, expires_at
    ) SELECT p_token_sha256, p_jti, grant_row.id, p_user_id, p_issuer,
        p_application, 'cloud-agents-cli', p_tenant_id, p_project_id,
        operation_time, p_expires_at
        FROM cloud_agents_identity.cli_grants AS grant_row
        WHERE grant_row.secret_digest = p_grant_digest;
    INSERT INTO cloud_agents_identity.audit_events (
        id, event_kind, user_id, actor_user_id, target_user_id, tenant_id,
        application, decision, reason_code, correlation_id
    ) VALUES (
        p_event_id, 'cli_token_issued', p_user_id, p_user_id, p_user_id,
        p_tenant_id, p_application, 'allow', 'cli_token_issued', p_correlation_id
    );
    RETURN true;
END
$body$;

CREATE FUNCTION cloud_agents_identity.create_service_account_record(
    p_tenant_id text, p_service_account_id text, p_display_name text,
    p_application text, p_management_scope_level text, p_management_scope_id text,
    p_membership_uid text, p_role_binding_uid text,
    p_role_name text, p_role_version bigint,
    p_credential_digest bytea, p_credential_expires_at timestamptz,
    p_actor_kind text, p_actor_issuer text, p_actor_subject text,
    p_event_id text, p_correlation_id text
) RETURNS TABLE (
    service_account_id text, credential_version bigint,
    subject_kind text, subject_issuer text, subject_value text,
    created_at timestamptz, updated_at timestamptz
)
LANGUAGE plpgsql SECURITY DEFINER
SET search_path = pg_catalog, cloud_agents_identity
AS $body$
DECLARE
    operation_time timestamptz := clock_timestamp();
    actor_id text;
    realm_issuer text;
BEGIN
    PERFORM cloud_agents_identity.require_principal('cloud_agents_runtime');
    IF p_tenant_id IS DISTINCT FROM cloud_agents.require_tenant_id()
        OR p_service_account_id IS NULL OR NOT cloud_agents.is_valid_identifier(p_service_account_id)
        OR p_display_name IS NULL OR length(p_display_name) NOT BETWEEN 1 AND 160
        OR p_application NOT IN ('admin', 'user')
        OR p_management_scope_level NOT IN ('tenant', 'organization', 'project')
        OR p_management_scope_id IS NULL
        OR NOT cloud_agents.is_valid_identifier(p_management_scope_id)
        OR (p_management_scope_level = 'tenant' AND p_management_scope_id <> p_tenant_id)
        OR p_membership_uid IS NULL OR NOT cloud_agents.is_valid_identifier(p_membership_uid)
        OR p_role_binding_uid IS NULL OR NOT cloud_agents.is_valid_identifier(p_role_binding_uid)
        OR p_role_name IS NULL OR NOT cloud_agents.is_valid_identifier(p_role_name)
        OR p_role_name = 'platform.admin'
        OR p_role_version IS NULL OR p_role_version < 1
        OR p_credential_digest IS NULL OR octet_length(p_credential_digest) <> 32
        OR p_credential_expires_at IS NULL OR p_credential_expires_at <= operation_time
        OR p_credential_expires_at > operation_time + interval '365 days'
        OR p_actor_kind <> 'user'
        OR p_actor_issuer IS NULL OR octet_length(p_actor_issuer) NOT BETWEEN 1 AND 512
        OR p_actor_subject IS NULL OR octet_length(p_actor_subject) NOT BETWEEN 1 AND 256
        OR p_event_id IS NULL OR p_event_id !~ '^[a-f0-9]{32}$'
        OR p_correlation_id IS NULL OR NOT cloud_agents.is_valid_identifier(p_correlation_id)
    THEN
        RAISE EXCEPTION USING ERRCODE = '22023', MESSAGE = 'invalid service account create';
    END IF;
    SELECT realm.issuer INTO STRICT realm_issuer
        FROM cloud_agents_identity.realm AS realm WHERE realm.singleton;
    IF p_actor_issuer <> realm_issuer OR p_actor_subject !~ '^user-[A-Za-z0-9._~-]+$' THEN
        RAISE EXCEPTION USING ERRCODE = '42501', MESSAGE = 'service account actor unavailable';
    END IF;
    actor_id := substr(p_actor_subject, 6);
    IF NOT EXISTS (
        SELECT 1 FROM cloud_agents_identity.users AS account
        WHERE account.id = actor_id AND account.disabled_at IS NULL
    ) THEN
        RAISE EXCEPTION USING ERRCODE = '42501', MESSAGE = 'service account actor unavailable';
    END IF;
    IF NOT EXISTS (
        SELECT 1 FROM cloud_agents.platform_tenants AS tenant
        WHERE tenant.tenant_id = p_tenant_id AND tenant.tenant_uid = p_tenant_id
            AND tenant.state = 'active'
    ) THEN
        RAISE EXCEPTION USING ERRCODE = '42501', MESSAGE = 'service account tenant unavailable';
    END IF;
    IF (p_management_scope_level = 'organization' AND NOT EXISTS (
        SELECT 1 FROM cloud_agents.organizations AS organization
        WHERE organization.tenant_id = p_tenant_id
            AND organization.organization_uid = p_management_scope_id
            AND organization.state = 'active'
    )) OR (p_management_scope_level = 'project' AND NOT EXISTS (
        SELECT 1 FROM cloud_agents.projects AS project
        JOIN cloud_agents.organizations AS organization
            ON organization.tenant_id = project.tenant_id
            AND organization.organization_uid = project.organization_uid
        WHERE project.tenant_id = p_tenant_id
            AND project.project_uid = p_management_scope_id
            AND project.state = 'active' AND organization.state = 'active'
    )) THEN
        RAISE EXCEPTION USING ERRCODE = '42501', MESSAGE = 'service account scope unavailable';
    END IF;
    IF NOT EXISTS (
        SELECT 1
        FROM cloud_agents.memberships AS membership
        JOIN cloud_agents.role_bindings AS binding
            ON binding.tenant_id = membership.tenant_id
            AND binding.role_binding_uid = p_role_binding_uid
            AND binding.subject_kind = membership.subject_kind
            AND binding.subject_issuer = membership.subject_issuer
            AND binding.subject_value = membership.subject_value
            AND binding.subject_digest = membership.subject_digest
        JOIN cloud_agents.builtin_roles AS role
            ON role.role_name = binding.role_name
            AND role.role_version = binding.role_version
        JOIN cloud_agents.resource_changes AS admission
            ON admission.tenant_id = membership.tenant_id
            AND admission.resource_kind = 'membership'
            AND admission.resource_uid = membership.membership_uid
            AND admission.change_kind = 'created'
            AND admission.resource_version < binding.resource_version
        WHERE membership.tenant_id = p_tenant_id
            AND membership.membership_uid = p_membership_uid
            AND membership.subject_kind = 'serviceAccount'
            AND membership.subject_issuer = realm_issuer
            AND membership.subject_value = 'service-' || p_service_account_id
            AND membership.subject_digest = cloud_agents.subject_ref_digest(
                'serviceAccount', realm_issuer, 'service-' || p_service_account_id
            )
            AND membership.scope_level = p_management_scope_level
            AND CASE p_management_scope_level
                WHEN 'tenant' THEN membership.scope_tenant_uid
                WHEN 'organization' THEN membership.scope_organization_uid
                WHEN 'project' THEN membership.scope_project_uid
            END = p_management_scope_id
            AND membership.state = 'active'
            AND (membership.expires_at IS NULL OR membership.expires_at > operation_time)
            AND binding.role_name = p_role_name
            AND binding.role_version = p_role_version
            AND binding.scope_level = membership.scope_level
            AND binding.scope_key = membership.scope_key
            AND binding.state = 'active'
            AND (binding.expires_at IS NULL OR binding.expires_at > operation_time)
            AND role.state = 'active'
            AND role.scope_level = p_management_scope_level
    ) THEN
        RAISE EXCEPTION USING ERRCODE = '42501', MESSAGE = 'service account binding unavailable';
    END IF;

    INSERT INTO cloud_agents_identity.service_accounts (
        id, tenant_id, tenant_uid, display_name, application,
        management_scope_level, management_scope_id,
        membership_uid, role_binding_uid, role_name, role_version,
        state, resource_version, created_by_user_id, created_at, updated_at
    ) VALUES (
        p_service_account_id, p_tenant_id, p_tenant_id, p_display_name,
        p_application, p_management_scope_level, p_management_scope_id,
        p_membership_uid, p_role_binding_uid, p_role_name, p_role_version,
        'active', 1, actor_id, operation_time, operation_time
    );
    INSERT INTO cloud_agents_identity.service_account_credentials (
        digest, service_account_id, credential_version, created_at, expires_at
    ) VALUES (
        p_credential_digest, p_service_account_id, 1, operation_time,
        p_credential_expires_at
    );
    INSERT INTO cloud_agents_identity.audit_events (
        id, event_kind, user_id, actor_user_id, target_service_account_id,
        tenant_id, application, decision, reason_code, correlation_id
    ) VALUES (
        p_event_id, 'service_account_created', actor_id, actor_id,
        p_service_account_id, p_tenant_id, p_application, 'allow',
        'service_account_created', p_correlation_id
    );
    RETURN QUERY SELECT p_service_account_id, 1::bigint, 'serviceAccount'::text,
        realm_issuer, 'service-' || p_service_account_id,
        operation_time, operation_time;
END
$body$;

CREATE FUNCTION cloud_agents_identity.lock_service_account_tenant_revision(
    p_tenant_id text
) RETURNS bigint
LANGUAGE plpgsql SECURITY DEFINER
SET search_path = pg_catalog, cloud_agents_identity
AS $body$
DECLARE current_revision bigint;
BEGIN
    PERFORM cloud_agents_identity.require_principal('cloud_agents_runtime');
    IF p_tenant_id IS NULL OR NOT cloud_agents.is_valid_identifier(p_tenant_id)
        OR p_tenant_id <> cloud_agents.require_tenant_id()
        OR NOT EXISTS (
            SELECT 1 FROM cloud_agents.platform_tenants AS tenant
            WHERE tenant.tenant_id = p_tenant_id
                AND tenant.tenant_uid = p_tenant_id
                AND tenant.state = 'active'
        )
    THEN
        RAISE EXCEPTION USING ERRCODE = '42501', MESSAGE = 'service account tenant unavailable';
    END IF;
    SELECT revision.current_revision INTO current_revision
    FROM cloud_agents.tenant_resource_versions AS revision
    WHERE revision.tenant_id = p_tenant_id
    FOR UPDATE;
    IF current_revision IS NULL THEN
        RAISE EXCEPTION USING ERRCODE = '42501', MESSAGE = 'service account tenant unavailable';
    END IF;
    RETURN current_revision;
END
$body$;

CREATE FUNCTION cloud_agents_identity.lock_service_account_management(
    p_tenant_id text, p_service_account_id text
) RETURNS TABLE (
    service_account_id text, display_name text, application text,
    management_scope_level text, management_scope_id text,
    membership_uid text, role_binding_uid text, role_name text, role_version bigint,
    state text, resource_version bigint,
    subject_kind text, subject_issuer text, subject_value text
)
LANGUAGE plpgsql SECURITY DEFINER
SET search_path = pg_catalog, cloud_agents_identity
AS $body$
BEGIN
    PERFORM cloud_agents_identity.require_principal('cloud_agents_runtime');
    IF p_tenant_id IS DISTINCT FROM cloud_agents.require_tenant_id()
        OR p_service_account_id IS NULL
        OR NOT cloud_agents.is_valid_identifier(p_service_account_id)
    THEN
        RAISE EXCEPTION USING ERRCODE = '22023', MESSAGE = 'invalid service account management context';
    END IF;
    RETURN QUERY
        SELECT account.id, account.display_name, account.application,
            account.management_scope_level, account.management_scope_id,
            account.membership_uid, account.role_binding_uid,
            account.role_name, account.role_version,
            account.state, account.resource_version,
            'serviceAccount'::text, realm.issuer, 'service-' || account.id
        FROM cloud_agents_identity.service_accounts AS account
        CROSS JOIN cloud_agents_identity.realm AS realm
        WHERE account.tenant_id = p_tenant_id
            AND account.id = p_service_account_id
            AND realm.singleton
        FOR UPDATE OF account;
END
$body$;

CREATE FUNCTION cloud_agents_identity.rotate_service_account_credential(
    p_tenant_id text, p_service_account_id text,
    p_expected_resource_version bigint, p_credential_digest bytea,
    p_credential_expires_at timestamptz,
    p_actor_kind text, p_actor_issuer text, p_actor_subject text,
    p_event_id text, p_correlation_id text
) RETURNS TABLE (resource_version bigint, credential_version bigint)
LANGUAGE plpgsql SECURITY DEFINER
SET search_path = pg_catalog, cloud_agents_identity
AS $body$
DECLARE
    operation_time timestamptz := clock_timestamp();
    current_version bigint;
    next_credential_version bigint;
    actor_id text;
    realm_issuer text;
BEGIN
    PERFORM cloud_agents_identity.require_principal('cloud_agents_runtime');
    IF p_tenant_id IS DISTINCT FROM cloud_agents.require_tenant_id()
        OR p_service_account_id IS NULL OR NOT cloud_agents.is_valid_identifier(p_service_account_id)
        OR p_expected_resource_version IS NULL OR p_expected_resource_version < 1
        OR p_credential_digest IS NULL OR octet_length(p_credential_digest) <> 32
        OR p_credential_expires_at IS NULL OR p_credential_expires_at <= operation_time
        OR p_credential_expires_at > operation_time + interval '365 days'
        OR p_actor_kind <> 'user'
        OR p_event_id IS NULL OR p_event_id !~ '^[a-f0-9]{32}$'
        OR p_correlation_id IS NULL OR NOT cloud_agents.is_valid_identifier(p_correlation_id)
    THEN
        RAISE EXCEPTION USING ERRCODE = '22023', MESSAGE = 'invalid service account rotation';
    END IF;
    SELECT realm.issuer INTO STRICT realm_issuer
        FROM cloud_agents_identity.realm AS realm WHERE realm.singleton;
    IF p_actor_issuer <> realm_issuer OR p_actor_subject !~ '^user-[A-Za-z0-9._~-]+$' THEN
        RAISE EXCEPTION USING ERRCODE = '42501', MESSAGE = 'service account actor unavailable';
    END IF;
    actor_id := substr(p_actor_subject, 6);
    IF NOT EXISTS (
        SELECT 1 FROM cloud_agents_identity.users AS account
        WHERE account.id = actor_id AND account.disabled_at IS NULL
    ) THEN
        RAISE EXCEPTION USING ERRCODE = '42501', MESSAGE = 'service account actor unavailable';
    END IF;
    SELECT account.resource_version INTO current_version
    FROM cloud_agents_identity.service_accounts AS account
    WHERE account.tenant_id = p_tenant_id AND account.id = p_service_account_id
        AND account.state = 'active'
    FOR UPDATE;
    IF current_version IS NULL THEN
        RAISE EXCEPTION USING ERRCODE = 'P0002', MESSAGE = 'service account unavailable';
    END IF;
    IF current_version <> p_expected_resource_version OR current_version = 9223372036854775807 THEN
        RAISE EXCEPTION USING ERRCODE = '40001', MESSAGE = 'service account version conflict';
    END IF;
    SELECT coalesce(max(credential.credential_version), 0) + 1
        INTO next_credential_version
    FROM cloud_agents_identity.service_account_credentials AS credential
    WHERE credential.service_account_id = p_service_account_id;
    UPDATE cloud_agents_identity.service_account_credentials
        SET revoked_at = operation_time
        WHERE service_account_id = p_service_account_id AND revoked_at IS NULL;
    INSERT INTO cloud_agents_identity.service_account_credentials (
        digest, service_account_id, credential_version, created_at, expires_at
    ) VALUES (
        p_credential_digest, p_service_account_id, next_credential_version,
        operation_time, p_credential_expires_at
    );
    UPDATE cloud_agents_identity.service_accounts
        SET resource_version = current_version + 1, updated_at = operation_time
        WHERE tenant_id = p_tenant_id AND id = p_service_account_id;
    INSERT INTO cloud_agents_identity.audit_events (
        id, event_kind, user_id, actor_user_id, target_service_account_id,
        tenant_id, application, decision, reason_code, correlation_id
    ) SELECT p_event_id, 'service_account_credential_rotated', actor_id, actor_id,
        account.id, account.tenant_id, account.application, 'allow',
        'service_account_credential_rotated', p_correlation_id
        FROM cloud_agents_identity.service_accounts AS account
        WHERE account.tenant_id = p_tenant_id AND account.id = p_service_account_id;
    RETURN QUERY SELECT current_version + 1, next_credential_version;
END
$body$;

CREATE FUNCTION cloud_agents_identity.record_service_account_management_denial(
    p_tenant_id text, p_service_account_id text,
    p_action text, p_reason text, p_application text,
    p_actor_kind text, p_actor_issuer text, p_actor_subject text,
    p_event_id text, p_correlation_id text
) RETURNS text
LANGUAGE plpgsql SECURITY DEFINER
SET search_path = pg_catalog, cloud_agents_identity
AS $body$
DECLARE
    actor_service_account_id text;
    actor_user_id text;
    audit_application text;
    event_kind text;
    realm_issuer text;
    target_id text;
BEGIN
    PERFORM cloud_agents_identity.require_principal('cloud_agents_runtime');
    IF p_tenant_id IS DISTINCT FROM cloud_agents.require_tenant_id()
        OR p_service_account_id IS NULL OR NOT cloud_agents.is_valid_identifier(p_service_account_id)
        OR p_action NOT IN ('create', 'rotate', 'disable')
        OR p_reason NOT IN ('authorization_denied', 'conflict')
        OR (p_action = 'create' AND p_application NOT IN ('admin', 'user'))
        OR (p_action <> 'create' AND p_application NOT IN ('', 'admin', 'user'))
        OR p_actor_kind NOT IN ('user', 'serviceAccount')
        OR p_actor_issuer IS NULL OR octet_length(p_actor_issuer) NOT BETWEEN 1 AND 512
        OR p_actor_subject IS NULL OR octet_length(p_actor_subject) NOT BETWEEN 1 AND 256
        OR p_event_id IS NULL OR p_event_id !~ '^[a-f0-9]{32}$'
        OR p_correlation_id IS NULL OR NOT cloud_agents.is_valid_identifier(p_correlation_id)
    THEN
        RAISE EXCEPTION USING ERRCODE = '22023', MESSAGE = 'invalid service account denial audit';
    END IF;
    SELECT realm.issuer INTO STRICT realm_issuer
        FROM cloud_agents_identity.realm AS realm WHERE realm.singleton;
    IF p_actor_issuer <> realm_issuer THEN
        RAISE EXCEPTION USING ERRCODE = '42501', MESSAGE = 'service account denial actor unavailable';
    END IF;
    IF p_actor_kind = 'user' THEN
        IF p_actor_subject !~ '^user-[A-Za-z0-9._~-]+$' THEN
            RAISE EXCEPTION USING ERRCODE = '42501', MESSAGE = 'service account denial actor unavailable';
        END IF;
        actor_user_id := substr(p_actor_subject, 6);
        IF NOT EXISTS (
            SELECT 1 FROM cloud_agents_identity.users AS account
            WHERE account.id = actor_user_id AND account.disabled_at IS NULL
        ) THEN
            RAISE EXCEPTION USING ERRCODE = '42501', MESSAGE = 'service account denial actor unavailable';
        END IF;
    ELSE
        SELECT account.id INTO actor_service_account_id
            FROM cloud_agents_identity.service_accounts AS account
            WHERE account.tenant_id = p_tenant_id
                AND account.state = 'active'
                AND account.application = 'admin'
                AND account.id = substr(p_actor_subject, 9)
                AND p_actor_subject = 'service-' || account.id;
        IF actor_service_account_id IS NULL
            OR p_actor_subject !~ '^service-[A-Za-z0-9._~-]+$'
        THEN
            RAISE EXCEPTION USING ERRCODE = '42501', MESSAGE = 'service account denial actor unavailable';
        END IF;
    END IF;
    IF NOT EXISTS (
        SELECT 1 FROM cloud_agents.platform_tenants AS tenant
        WHERE tenant.tenant_id = p_tenant_id AND tenant.tenant_uid = p_tenant_id
            AND tenant.state = 'active'
    ) THEN
        RAISE EXCEPTION USING ERRCODE = '42501', MESSAGE = 'service account denial authority unavailable';
    END IF;
    SELECT account.id, account.application INTO target_id, audit_application
        FROM cloud_agents_identity.service_accounts AS account
        WHERE account.tenant_id = p_tenant_id AND account.id = p_service_account_id;
    IF p_action <> 'create' AND target_id IS NULL THEN
        RAISE EXCEPTION USING ERRCODE = 'P0002', MESSAGE = 'service account denial target unavailable';
    END IF;
    IF p_action = 'create' THEN
        audit_application := p_application;
    ELSIF p_application <> '' AND p_application <> audit_application THEN
        RAISE EXCEPTION USING ERRCODE = '22023', MESSAGE = 'service account denial application mismatch';
    END IF;
    event_kind := CASE p_action
        WHEN 'create' THEN 'service_account_create_denied'
        WHEN 'rotate' THEN 'service_account_credential_rotation_denied'
        WHEN 'disable' THEN 'service_account_disable_denied'
    END;
    INSERT INTO cloud_agents_identity.audit_events (
        id, event_kind, user_id, actor_user_id, actor_service_account_id, target_service_account_id,
        tenant_id, application, decision, reason_code, correlation_id
    ) VALUES (
        p_event_id, event_kind, actor_user_id, actor_user_id, actor_service_account_id, target_id,
        p_tenant_id, audit_application, 'deny', p_reason, p_correlation_id
    );
    RETURN p_event_id;
END
$body$;

CREATE FUNCTION cloud_agents_identity.disable_service_account_record(
    p_tenant_id text, p_service_account_id text, p_expected_resource_version bigint,
    p_actor_kind text, p_actor_issuer text, p_actor_subject text,
    p_event_id text, p_correlation_id text
) RETURNS bigint
LANGUAGE plpgsql SECURITY DEFINER
SET search_path = pg_catalog, cloud_agents_identity
AS $body$
DECLARE
    operation_time timestamptz := clock_timestamp();
    next_version bigint;
    actor_id text;
    realm_issuer text;
    account_application text;
BEGIN
    PERFORM cloud_agents_identity.require_principal('cloud_agents_runtime');
    IF p_tenant_id IS DISTINCT FROM cloud_agents.require_tenant_id()
        OR p_service_account_id IS NULL OR NOT cloud_agents.is_valid_identifier(p_service_account_id)
        OR p_expected_resource_version IS NULL OR p_expected_resource_version < 1
        OR p_actor_kind <> 'user'
        OR p_event_id IS NULL OR p_event_id !~ '^[a-f0-9]{32}$'
        OR p_correlation_id IS NULL OR NOT cloud_agents.is_valid_identifier(p_correlation_id)
    THEN
        RAISE EXCEPTION USING ERRCODE = '22023', MESSAGE = 'invalid service account disable';
    END IF;
    SELECT realm.issuer INTO STRICT realm_issuer
        FROM cloud_agents_identity.realm AS realm WHERE realm.singleton;
    IF p_actor_issuer <> realm_issuer OR p_actor_subject !~ '^user-[A-Za-z0-9._~-]+$' THEN
        RAISE EXCEPTION USING ERRCODE = '42501', MESSAGE = 'service account actor unavailable';
    END IF;
    actor_id := substr(p_actor_subject, 6);
    IF NOT EXISTS (
        SELECT 1 FROM cloud_agents_identity.users AS account
        WHERE account.id = actor_id AND account.disabled_at IS NULL
    ) THEN
        RAISE EXCEPTION USING ERRCODE = '42501', MESSAGE = 'service account actor unavailable';
    END IF;
    UPDATE cloud_agents_identity.service_accounts AS account
        SET state = 'disabled', resource_version = account.resource_version + 1,
            updated_at = operation_time
        WHERE account.tenant_id = p_tenant_id AND account.id = p_service_account_id
            AND account.state = 'active'
            AND account.resource_version = p_expected_resource_version
            AND account.resource_version < 9223372036854775807
        RETURNING account.resource_version, account.application
        INTO next_version, account_application;
    IF next_version IS NULL THEN
        RAISE EXCEPTION USING ERRCODE = '40001', MESSAGE = 'service account version conflict';
    END IF;
    UPDATE cloud_agents_identity.service_account_credentials
        SET revoked_at = operation_time
        WHERE service_account_id = p_service_account_id AND revoked_at IS NULL;
    INSERT INTO cloud_agents_identity.audit_events (
        id, event_kind, user_id, actor_user_id, target_service_account_id,
        tenant_id, application, decision, reason_code, correlation_id
    ) VALUES (
        p_event_id, 'service_account_disabled', actor_id, actor_id,
        p_service_account_id, p_tenant_id, account_application, 'allow',
        'service_account_disabled', p_correlation_id
    );
    RETURN next_version;
END
$body$;

CREATE FUNCTION cloud_agents_identity.list_service_account_records(
    p_tenant_id text, p_after text, p_limit integer
) RETURNS TABLE (
    service_account_id text, display_name text, application text,
    management_scope_level text, management_scope_id text,
    membership_uid text, role_binding_uid text, role_name text, role_version bigint,
    state text, resource_version bigint,
    subject_kind text, subject_issuer text, subject_value text,
    created_at timestamptz, updated_at timestamptz
)
LANGUAGE plpgsql SECURITY DEFINER
SET search_path = pg_catalog, cloud_agents_identity
AS $body$
BEGIN
    PERFORM cloud_agents_identity.require_principal('cloud_agents_runtime');
    IF p_tenant_id IS DISTINCT FROM cloud_agents.require_tenant_id()
        OR p_after IS NULL OR (p_after <> '' AND NOT cloud_agents.is_valid_identifier(p_after))
        OR p_limit IS NULL OR p_limit NOT BETWEEN 1 AND 201
    THEN
        RAISE EXCEPTION USING ERRCODE = '22023', MESSAGE = 'invalid service account page';
    END IF;
    RETURN QUERY SELECT account.id, account.display_name, account.application,
        account.management_scope_level, account.management_scope_id,
        account.membership_uid, account.role_binding_uid,
        account.role_name, account.role_version,
        account.state, account.resource_version,
        'serviceAccount'::text, realm.issuer, 'service-' || account.id,
        account.created_at, account.updated_at
    FROM cloud_agents_identity.service_accounts AS account
    CROSS JOIN cloud_agents_identity.realm AS realm
    WHERE account.tenant_id = p_tenant_id AND account.id > p_after
        AND realm.singleton
    ORDER BY account.id LIMIT p_limit;
END
$body$;

CREATE FUNCTION cloud_agents_identity.read_service_account_subject(
    p_credential_digest bytea, p_application text, p_tenant_id text
) RETURNS TABLE (
    service_account_id text, subject_kind text, subject_issuer text,
    subject_value text, subject_digest text
)
LANGUAGE plpgsql SECURITY DEFINER
SET search_path = pg_catalog, cloud_agents_identity
AS $body$
DECLARE operation_time timestamptz := clock_timestamp();
BEGIN
    PERFORM cloud_agents_identity.require_principal('cloud_agents_runtime');
    IF p_credential_digest IS NULL OR octet_length(p_credential_digest) <> 32
        OR p_application NOT IN ('admin', 'user')
        OR p_tenant_id IS DISTINCT FROM cloud_agents.require_tenant_id()
    THEN
        RAISE EXCEPTION USING ERRCODE = '22023', MESSAGE = 'invalid service account context';
    END IF;
    RETURN QUERY SELECT account.id, 'serviceAccount'::text, realm.issuer,
        'service-' || account.id,
        cloud_agents.subject_ref_digest(
            'serviceAccount', realm.issuer, 'service-' || account.id
        )
    FROM cloud_agents_identity.service_account_credentials AS credential
    JOIN cloud_agents_identity.service_accounts AS account
        ON account.id = credential.service_account_id
    CROSS JOIN cloud_agents_identity.realm AS realm
    WHERE credential.digest = p_credential_digest
        AND account.tenant_id = p_tenant_id
        AND account.application = p_application
        AND account.state = 'active'
        AND credential.revoked_at IS NULL
        AND credential.expires_at > operation_time
        AND realm.singleton
        AND EXISTS (
            SELECT 1 FROM cloud_agents.platform_tenants AS tenant
            WHERE tenant.tenant_id = p_tenant_id AND tenant.tenant_uid = p_tenant_id
                AND tenant.state = 'active'
        );
END
$body$;

CREATE FUNCTION cloud_agents_identity.record_service_account_issued_token(
    p_credential_digest bytea, p_service_account_id text, p_issuer text,
    p_application text, p_tenant_id text, p_project_id text,
    p_jti text, p_token_sha256 bytea, p_expires_at timestamptz,
    p_event_id text, p_correlation_id text
) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER
SET search_path = pg_catalog, cloud_agents_identity
AS $body$
DECLARE operation_time timestamptz := clock_timestamp();
BEGIN
    PERFORM cloud_agents_identity.require_principal('cloud_agents_identity_service');
    IF p_credential_digest IS NULL OR octet_length(p_credential_digest) <> 32
        OR p_service_account_id IS NULL OR NOT cloud_agents.is_valid_identifier(p_service_account_id)
        OR p_issuer IS NULL OR octet_length(p_issuer) NOT BETWEEN 1 AND 512
        OR p_application NOT IN ('admin', 'user')
        OR p_tenant_id IS NULL OR NOT cloud_agents.is_valid_identifier(p_tenant_id)
        OR (p_project_id IS NOT NULL AND NOT cloud_agents.is_valid_identifier(p_project_id))
        OR p_jti IS NULL OR p_jti !~ '^[A-Za-z0-9_-]{22}$'
        OR p_token_sha256 IS NULL OR octet_length(p_token_sha256) <> 32
        OR p_expires_at IS NULL OR p_expires_at <= operation_time
        OR p_expires_at > operation_time + interval '15 minutes'
        OR p_event_id IS NULL OR p_event_id !~ '^[a-f0-9]{32}$'
        OR p_correlation_id IS NULL OR NOT cloud_agents.is_valid_identifier(p_correlation_id)
    THEN
        RAISE EXCEPTION USING ERRCODE = '22023', MESSAGE = 'invalid service account issued token';
    END IF;
    IF NOT EXISTS (
        SELECT 1
        FROM cloud_agents_identity.service_account_credentials AS credential
        JOIN cloud_agents_identity.service_accounts AS account
            ON account.id = credential.service_account_id
        CROSS JOIN cloud_agents_identity.realm AS realm
        WHERE credential.digest = p_credential_digest
            AND account.id = p_service_account_id
            AND account.tenant_id = p_tenant_id
            AND account.application = p_application
            AND account.state = 'active'
            AND credential.revoked_at IS NULL AND credential.expires_at > operation_time
            AND realm.singleton AND realm.issuer = p_issuer
    ) OR NOT EXISTS (
        SELECT 1 FROM cloud_agents.platform_tenants AS tenant
        WHERE tenant.tenant_id = p_tenant_id AND tenant.tenant_uid = p_tenant_id
            AND tenant.state = 'active'
    ) OR (p_project_id IS NOT NULL AND NOT EXISTS (
        SELECT 1 FROM cloud_agents.projects AS project
        JOIN cloud_agents.organizations AS organization
            ON organization.tenant_id = project.tenant_id
            AND organization.organization_uid = project.organization_uid
        WHERE project.tenant_id = p_tenant_id AND project.project_uid = p_project_id
            AND project.state = 'active' AND organization.state = 'active'
    )) THEN
        RETURN false;
    END IF;
    INSERT INTO cloud_agents_identity.service_account_issued_tokens (
        token_sha256, jti, credential_digest, service_account_id,
        issuer, application, client_id, tenant_id, project_id, issued_at, expires_at
    ) VALUES (
        p_token_sha256, p_jti, p_credential_digest, p_service_account_id,
        p_issuer, p_application, 'cloud-agents-automation', p_tenant_id,
        p_project_id, operation_time, p_expires_at
    );
    UPDATE cloud_agents_identity.service_account_credentials
        SET last_used_at = operation_time WHERE digest = p_credential_digest;
    INSERT INTO cloud_agents_identity.audit_events (
        id, event_kind, actor_service_account_id, target_service_account_id,
        tenant_id, application, decision, reason_code, correlation_id
    ) VALUES (
        p_event_id, 'service_account_token_issued', p_service_account_id,
        p_service_account_id, p_tenant_id, p_application, 'allow',
        'service_account_token_issued', p_correlation_id
    );
    RETURN true;
END
$body$;

CREATE FUNCTION cloud_agents_identity.access_token_is_active_v2(
    p_token_sha256 bytea, p_expected_client_id text,
    p_expected_application text, p_expected_tenant_id text,
    p_expected_project_id text
) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER
SET search_path = pg_catalog, cloud_agents_identity
AS $body$
DECLARE operation_time timestamptz := clock_timestamp();
BEGIN
    PERFORM cloud_agents_identity.require_principal('cloud_agents_identity_service');
    IF p_token_sha256 IS NULL OR octet_length(p_token_sha256) <> 32
        OR p_expected_client_id NOT IN (
            'cloud-agents-admin-web', 'cloud-agents-user-web',
            'cloud-agents-cli', 'cloud-agents-automation'
        )
        OR p_expected_application NOT IN ('admin', 'user')
        OR p_expected_tenant_id IS NULL OR NOT cloud_agents.is_valid_identifier(p_expected_tenant_id)
        OR (p_expected_project_id IS NOT NULL AND NOT cloud_agents.is_valid_identifier(p_expected_project_id))
    THEN
        RAISE EXCEPTION USING ERRCODE = '22023', MESSAGE = 'invalid token status input';
    END IF;
    IF p_expected_client_id IN ('cloud-agents-admin-web', 'cloud-agents-user-web') THEN
        IF (p_expected_client_id = 'cloud-agents-admin-web') <> (p_expected_application = 'admin') THEN
            RETURN false;
        END IF;
        RETURN cloud_agents_identity.token_is_active(
            p_token_sha256, p_expected_application,
            p_expected_tenant_id, p_expected_project_id
        );
    END IF;
    IF p_expected_client_id = 'cloud-agents-cli' THEN
        RETURN EXISTS (
            SELECT 1
            FROM cloud_agents_identity.cli_issued_tokens AS issued
            JOIN cloud_agents_identity.cli_grants AS grant_row ON grant_row.id = issued.grant_id
            JOIN cloud_agents_identity.users AS account ON account.id = issued.user_id
            JOIN cloud_agents_identity.realm AS realm
                ON realm.singleton AND realm.issuer = issued.issuer
            WHERE issued.token_sha256 = p_token_sha256
                AND issued.client_id = p_expected_client_id
                AND issued.application = p_expected_application
                AND issued.tenant_id = p_expected_tenant_id
                AND (issued.project_id IS NULL OR issued.project_id = p_expected_project_id)
                AND issued.expires_at > operation_time
                AND grant_row.revoked_at IS NULL AND grant_row.expires_at > operation_time
                AND grant_row.last_seen_at > operation_time - interval '7 days'
                AND account.disabled_at IS NULL
        );
    END IF;
    RETURN EXISTS (
        SELECT 1
        FROM cloud_agents_identity.service_account_issued_tokens AS issued
        JOIN cloud_agents_identity.service_account_credentials AS credential
            ON credential.digest = issued.credential_digest
        JOIN cloud_agents_identity.service_accounts AS account
            ON account.id = issued.service_account_id
        JOIN cloud_agents_identity.realm AS realm
            ON realm.singleton AND realm.issuer = issued.issuer
        WHERE issued.token_sha256 = p_token_sha256
            AND issued.client_id = p_expected_client_id
            AND issued.application = p_expected_application
            AND issued.tenant_id = p_expected_tenant_id
            AND (issued.project_id IS NULL OR issued.project_id = p_expected_project_id)
            AND issued.expires_at > operation_time
            AND credential.revoked_at IS NULL AND credential.expires_at > operation_time
            AND account.state = 'active'
    );
END
$body$;


-- Historical facts stay explicitly unattributed. New membership/role facts are
-- required to fill this complete group inside the same tenant transaction.
ALTER TABLE cloud_agents.audit_facts
    ADD COLUMN actor_subject_kind text,
    ADD COLUMN actor_subject_issuer text,
    ADD COLUMN actor_subject_value text,
    ADD COLUMN actor_subject_digest text,
    ADD COLUMN application text,
    ADD COLUMN decision text NOT NULL DEFAULT 'allow',
    ADD COLUMN correlation_id text;
ALTER TABLE cloud_agents.audit_facts
    ADD CONSTRAINT audit_facts_actor_context_check CHECK (
        (actor_subject_kind IS NULL
            AND actor_subject_issuer IS NULL
            AND actor_subject_value IS NULL
            AND actor_subject_digest IS NULL
            AND application IS NULL
            AND correlation_id IS NULL)
        OR
        (actor_subject_kind IN ('user', 'serviceAccount', 'workload')
            AND char_length(actor_subject_issuer) BETWEEN 1 AND 512
            AND actor_subject_issuer ~ '^[A-Za-z][A-Za-z0-9+.-]*:'
            AND char_length(actor_subject_value) BETWEEN 1 AND 256
            AND actor_subject_digest = cloud_agents.subject_ref_digest(
                actor_subject_kind, actor_subject_issuer, actor_subject_value
            )
            AND application IN ('admin', 'user')
            AND cloud_agents.is_valid_identifier(correlation_id))
    ),
    ADD CONSTRAINT audit_facts_decision_check CHECK (decision = 'allow');

CREATE FUNCTION cloud_agents_identity.list_control_plane_audit_facts(
    p_session_digest bytea, p_tenant_id text,
    p_after_at timestamptz, p_after_id text, p_limit integer
) RETURNS TABLE (
    id text, action text,
    actor_subject_kind text, actor_subject_issuer text,
    actor_subject_value text, actor_subject_digest text,
    application text, resource_kind text, resource_id text,
    tenant_id text, decision text, reason_code text,
    correlation_id text, occurred_at timestamptz
)
LANGUAGE plpgsql SECURITY DEFINER
SET search_path = pg_catalog, cloud_agents_identity
AS $body$
BEGIN
    PERFORM 1
        FROM cloud_agents_identity.require_tenant_admin(
            p_session_digest, p_tenant_id
        );
    IF p_limit IS NULL OR p_limit NOT BETWEEN 1 AND 201
        OR (p_after_at IS NULL) <> (p_after_id IS NULL)
        OR (p_after_id IS NOT NULL AND NOT cloud_agents.is_valid_identifier(p_after_id))
    THEN
        RAISE EXCEPTION USING ERRCODE = '22023', MESSAGE = 'invalid control plane audit page';
    END IF;
    RETURN QUERY
        SELECT audit.audit_fact_uid, audit.action,
            audit.actor_subject_kind, audit.actor_subject_issuer,
            audit.actor_subject_value, audit.actor_subject_digest,
            audit.application, audit.resource_kind, audit.resource_uid,
            audit.tenant_id, audit.decision, audit.reason_code,
            audit.correlation_id, audit.occurred_at
        FROM cloud_agents.audit_facts AS audit
        WHERE audit.tenant_id = p_tenant_id
            AND audit.tenant_uid = p_tenant_id
            AND (
                (audit.resource_kind = 'membership'
                    AND audit.action IN (
                        'membership.create', 'membership.resume',
                        'membership.suspend', 'membership.revoke'
                    ))
                OR
                (audit.resource_kind = 'role_binding'
                    AND audit.action IN (
                        'role_binding.bind', 'role_binding.revoke'
                    ))
            )
            AND (p_after_at IS NULL OR
                (audit.occurred_at, audit.audit_fact_uid) < (p_after_at, p_after_id))
        ORDER BY audit.occurred_at DESC, audit.audit_fact_uid DESC
        LIMIT p_limit;
END
$body$;


CREATE FUNCTION cloud_agents.record_rbac_audit_context_v1(
    p_tenant_id text,
    p_audit_fact_uid text,
    p_resource_version bigint,
    p_action text,
    p_resource_kind text,
    p_resource_uid text,
    p_actor_subject_kind text,
    p_actor_subject_issuer text,
    p_actor_subject_value text,
    p_application text,
    p_correlation_id text
) RETURNS void
LANGUAGE plpgsql SECURITY DEFINER
SET search_path = pg_catalog, cloud_agents
AS $body$
DECLARE actor_digest text;
BEGIN
    PERFORM cloud_agents.require_runtime_mutation_principal();
    IF p_tenant_id IS DISTINCT FROM cloud_agents.require_tenant_id()
        OR p_audit_fact_uid IS NULL OR NOT cloud_agents.is_valid_identifier(p_audit_fact_uid)
        OR p_resource_version IS NULL OR p_resource_version < 1
        OR p_action NOT IN (
            'membership.create', 'membership.resume', 'membership.suspend',
            'membership.revoke', 'role_binding.bind', 'role_binding.revoke'
        )
        OR p_resource_kind NOT IN ('membership', 'role_binding')
        OR (p_action LIKE 'membership.%') <> (p_resource_kind = 'membership')
        OR NOT cloud_agents.is_valid_identifier(p_resource_uid)
        OR p_application NOT IN ('admin', 'user')
        OR p_correlation_id IS NULL OR NOT cloud_agents.is_valid_identifier(p_correlation_id)
    THEN
        RAISE EXCEPTION USING ERRCODE = '22023', MESSAGE = 'invalid RBAC audit context';
    END IF;
    actor_digest := cloud_agents.subject_ref_digest(
        p_actor_subject_kind, p_actor_subject_issuer, p_actor_subject_value
    );
    UPDATE cloud_agents.audit_facts AS audit
    SET actor_subject_kind = p_actor_subject_kind,
        actor_subject_issuer = p_actor_subject_issuer,
        actor_subject_value = p_actor_subject_value,
        actor_subject_digest = actor_digest,
        application = p_application,
        correlation_id = p_correlation_id
    WHERE audit.tenant_id = p_tenant_id
        AND audit.tenant_uid = p_tenant_id
        AND audit.audit_fact_uid = p_audit_fact_uid
        AND audit.resource_version = p_resource_version
        AND audit.action = p_action
        AND audit.resource_kind = p_resource_kind
        AND audit.resource_uid = p_resource_uid
        AND audit.actor_database_principal = SESSION_USER
        AND audit.actor_subject_kind IS NULL
        AND audit.actor_subject_issuer IS NULL
        AND audit.actor_subject_value IS NULL
        AND audit.actor_subject_digest IS NULL
        AND audit.application IS NULL
        AND audit.correlation_id IS NULL;
    IF NOT FOUND THEN
        RAISE EXCEPTION USING ERRCODE = '42501', MESSAGE = 'RBAC audit fact context unavailable';
    END IF;
END
$body$;

CREATE FUNCTION cloud_agents.create_membership_v3(
    p_tenant_id text, p_expected_tenant_revision bigint,
    p_membership_uid text, p_membership_name text,
    p_subject_kind text, p_subject_issuer text, p_subject_value text,
    p_scope_level text, p_scope_uid text, p_expires_at timestamptz,
    p_audit_fact_uid text, p_reason_code text,
    p_actor_subject_kind text, p_actor_subject_issuer text,
    p_actor_subject_value text, p_application text, p_correlation_id text
) RETURNS TABLE (resource_uid text, resource_version bigint, resource_state text)
LANGUAGE plpgsql SECURITY DEFINER
SET search_path = pg_catalog, cloud_agents
AS $body$
DECLARE result record;
BEGIN
    SELECT * INTO STRICT result FROM cloud_agents.create_membership_v2(
        p_tenant_id, p_expected_tenant_revision, p_membership_uid,
        p_membership_name, p_subject_kind, p_subject_issuer, p_subject_value,
        p_scope_level, p_scope_uid, p_expires_at, p_audit_fact_uid, p_reason_code
    );
    PERFORM cloud_agents.record_rbac_audit_context_v1(
        p_tenant_id, p_audit_fact_uid, result.resource_version,
        'membership.create', 'membership', p_membership_uid,
        p_actor_subject_kind, p_actor_subject_issuer, p_actor_subject_value,
        p_application, p_correlation_id
    );
    RETURN QUERY SELECT result.resource_uid::text, result.resource_version::bigint,
        result.resource_state::text;
END
$body$;

CREATE FUNCTION cloud_agents.transition_membership_v2(
    p_tenant_id text, p_expected_tenant_revision bigint,
    p_membership_uid text, p_expected_resource_version bigint,
    p_target_state text, p_audit_fact_uid text, p_reason_code text,
    p_actor_subject_kind text, p_actor_subject_issuer text,
    p_actor_subject_value text, p_application text, p_correlation_id text
) RETURNS TABLE (resource_uid text, resource_version bigint, resource_state text)
LANGUAGE plpgsql SECURITY DEFINER
SET search_path = pg_catalog, cloud_agents
AS $body$
DECLARE result record; audit_action text;
BEGIN
    SELECT * INTO STRICT result FROM cloud_agents.transition_membership(
        p_tenant_id, p_expected_tenant_revision, p_membership_uid,
        p_expected_resource_version, p_target_state, p_audit_fact_uid, p_reason_code
    );
    audit_action := CASE p_target_state
        WHEN 'active' THEN 'membership.resume'
        WHEN 'suspended' THEN 'membership.suspend'
        WHEN 'revoked' THEN 'membership.revoke'
    END;
    IF audit_action IS NULL THEN
        RAISE EXCEPTION USING ERRCODE = '22023', MESSAGE = 'invalid membership transition';
    END IF;
    PERFORM cloud_agents.record_rbac_audit_context_v1(
        p_tenant_id, p_audit_fact_uid, result.resource_version,
        audit_action, 'membership', p_membership_uid,
        p_actor_subject_kind, p_actor_subject_issuer, p_actor_subject_value,
        p_application, p_correlation_id
    );
    RETURN QUERY SELECT result.resource_uid::text, result.resource_version::bigint,
        result.resource_state::text;
END
$body$;

CREATE FUNCTION cloud_agents.bind_role_v2(
    p_tenant_id text, p_expected_tenant_revision bigint,
    p_role_binding_uid text, p_role_binding_name text,
    p_subject_kind text, p_subject_issuer text, p_subject_value text,
    p_role_name text, p_role_version bigint,
    p_scope_level text, p_scope_uid text, p_expires_at timestamptz,
    p_audit_fact_uid text, p_reason_code text,
    p_actor_subject_kind text, p_actor_subject_issuer text,
    p_actor_subject_value text, p_application text, p_correlation_id text
) RETURNS TABLE (resource_uid text, resource_version bigint, resource_state text)
LANGUAGE plpgsql SECURITY DEFINER
SET search_path = pg_catalog, cloud_agents
AS $body$
DECLARE result record;
BEGIN
    SELECT * INTO STRICT result FROM cloud_agents.bind_role(
        p_tenant_id, p_expected_tenant_revision, p_role_binding_uid,
        p_role_binding_name, p_subject_kind, p_subject_issuer, p_subject_value,
        p_role_name, p_role_version, p_scope_level, p_scope_uid, p_expires_at,
        p_audit_fact_uid, p_reason_code
    );
    PERFORM cloud_agents.record_rbac_audit_context_v1(
        p_tenant_id, p_audit_fact_uid, result.resource_version,
        'role_binding.bind', 'role_binding', p_role_binding_uid,
        p_actor_subject_kind, p_actor_subject_issuer, p_actor_subject_value,
        p_application, p_correlation_id
    );
    RETURN QUERY SELECT result.resource_uid::text, result.resource_version::bigint,
        result.resource_state::text;
END
$body$;

CREATE FUNCTION cloud_agents.revoke_role_binding_v2(
    p_tenant_id text, p_expected_tenant_revision bigint,
    p_role_binding_uid text, p_expected_resource_version bigint,
    p_audit_fact_uid text, p_reason_code text,
    p_actor_subject_kind text, p_actor_subject_issuer text,
    p_actor_subject_value text, p_application text, p_correlation_id text
) RETURNS TABLE (resource_uid text, resource_version bigint, resource_state text)
LANGUAGE plpgsql SECURITY DEFINER
SET search_path = pg_catalog, cloud_agents
AS $body$
DECLARE result record;
BEGIN
    SELECT * INTO STRICT result FROM cloud_agents.revoke_role_binding(
        p_tenant_id, p_expected_tenant_revision, p_role_binding_uid,
        p_expected_resource_version, p_audit_fact_uid, p_reason_code
    );
    PERFORM cloud_agents.record_rbac_audit_context_v1(
        p_tenant_id, p_audit_fact_uid, result.resource_version,
        'role_binding.revoke', 'role_binding', p_role_binding_uid,
        p_actor_subject_kind, p_actor_subject_issuer, p_actor_subject_value,
        p_application, p_correlation_id
    );
    RETURN QUERY SELECT result.resource_uid::text, result.resource_version::bigint,
        result.resource_state::text;
END
$body$;

CREATE FUNCTION cloud_agents.suspend_membership_v2(
    p_tenant_id text, p_expected_tenant_revision bigint,
    p_membership_uid text, p_expected_resource_version bigint,
    p_audit_fact_uid text, p_reason_code text,
    p_actor_subject_kind text, p_actor_subject_issuer text,
    p_actor_subject_value text, p_application text, p_correlation_id text
) RETURNS TABLE (resource_uid text, resource_version bigint, resource_state text)
LANGUAGE sql SECURITY DEFINER
SET search_path = pg_catalog, cloud_agents
AS $body$
    SELECT * FROM cloud_agents.transition_membership_v2(
        p_tenant_id, p_expected_tenant_revision, p_membership_uid,
        p_expected_resource_version, 'suspended', p_audit_fact_uid, p_reason_code,
        p_actor_subject_kind, p_actor_subject_issuer, p_actor_subject_value,
        p_application, p_correlation_id
    )
$body$;

CREATE FUNCTION cloud_agents.resume_membership_v2(
    p_tenant_id text, p_expected_tenant_revision bigint,
    p_membership_uid text, p_expected_resource_version bigint,
    p_audit_fact_uid text, p_reason_code text,
    p_actor_subject_kind text, p_actor_subject_issuer text,
    p_actor_subject_value text, p_application text, p_correlation_id text
) RETURNS TABLE (resource_uid text, resource_version bigint, resource_state text)
LANGUAGE sql SECURITY DEFINER
SET search_path = pg_catalog, cloud_agents
AS $body$
    SELECT * FROM cloud_agents.transition_membership_v2(
        p_tenant_id, p_expected_tenant_revision, p_membership_uid,
        p_expected_resource_version, 'active', p_audit_fact_uid, p_reason_code,
        p_actor_subject_kind, p_actor_subject_issuer, p_actor_subject_value,
        p_application, p_correlation_id
    )
$body$;

CREATE FUNCTION cloud_agents.revoke_membership_v2(
    p_tenant_id text, p_expected_tenant_revision bigint,
    p_membership_uid text, p_expected_resource_version bigint,
    p_audit_fact_uid text, p_reason_code text,
    p_actor_subject_kind text, p_actor_subject_issuer text,
    p_actor_subject_value text, p_application text, p_correlation_id text
) RETURNS TABLE (resource_uid text, resource_version bigint, resource_state text)
LANGUAGE sql SECURITY DEFINER
SET search_path = pg_catalog, cloud_agents
AS $body$
    SELECT * FROM cloud_agents.transition_membership_v2(
        p_tenant_id, p_expected_tenant_revision, p_membership_uid,
        p_expected_resource_version, 'revoked', p_audit_fact_uid, p_reason_code,
        p_actor_subject_kind, p_actor_subject_issuer, p_actor_subject_value,
        p_application, p_correlation_id
    )
$body$;

REVOKE ALL ON FUNCTION cloud_agents.record_rbac_audit_context_v1(
    text, text, bigint, text, text, text, text, text, text, text, text
) FROM PUBLIC;
REVOKE ALL ON FUNCTION cloud_agents.create_membership_v3(
    text, bigint, text, text, text, text, text, text, text, timestamptz,
    text, text, text, text, text, text, text
) FROM PUBLIC;
REVOKE ALL ON FUNCTION cloud_agents.transition_membership_v2(
    text, bigint, text, bigint, text, text, text, text, text, text, text, text
) FROM PUBLIC;
REVOKE ALL ON FUNCTION cloud_agents.suspend_membership_v2(
    text, bigint, text, bigint, text, text, text, text, text, text, text
) FROM PUBLIC;
REVOKE ALL ON FUNCTION cloud_agents.resume_membership_v2(
    text, bigint, text, bigint, text, text, text, text, text, text, text
) FROM PUBLIC;
REVOKE ALL ON FUNCTION cloud_agents.revoke_membership_v2(
    text, bigint, text, bigint, text, text, text, text, text, text, text
) FROM PUBLIC;
REVOKE ALL ON FUNCTION cloud_agents.bind_role_v2(
    text, bigint, text, text, text, text, text, text, bigint, text, text,
    timestamptz, text, text, text, text, text, text, text
) FROM PUBLIC;
REVOKE ALL ON FUNCTION cloud_agents.revoke_role_binding_v2(
    text, bigint, text, bigint, text, text, text, text, text, text, text
) FROM PUBLIC;

REVOKE EXECUTE ON FUNCTION cloud_agents.create_membership_v2(
    text, bigint, text, text, text, text, text, text, text, timestamptz, text, text
) FROM cloud_agents_runtime;
REVOKE EXECUTE ON FUNCTION cloud_agents.suspend_membership(
    text, bigint, text, bigint, text, text
) FROM cloud_agents_runtime;
REVOKE EXECUTE ON FUNCTION cloud_agents.resume_membership(
    text, bigint, text, bigint, text, text
) FROM cloud_agents_runtime;
REVOKE EXECUTE ON FUNCTION cloud_agents.revoke_membership(
    text, bigint, text, bigint, text, text
) FROM cloud_agents_runtime;
REVOKE EXECUTE ON FUNCTION cloud_agents.bind_role(
    text, bigint, text, text, text, text, text, text, bigint, text, text,
    timestamptz, text, text
) FROM cloud_agents_runtime;
REVOKE EXECUTE ON FUNCTION cloud_agents.revoke_role_binding(
    text, bigint, text, bigint, text, text
) FROM cloud_agents_runtime;

GRANT EXECUTE ON FUNCTION cloud_agents.create_membership_v3(
    text, bigint, text, text, text, text, text, text, text, timestamptz,
    text, text, text, text, text, text, text
) TO cloud_agents_runtime;
GRANT EXECUTE ON FUNCTION cloud_agents.suspend_membership_v2(
    text, bigint, text, bigint, text, text, text, text, text, text, text
) TO cloud_agents_runtime;
GRANT EXECUTE ON FUNCTION cloud_agents.resume_membership_v2(
    text, bigint, text, bigint, text, text, text, text, text, text, text
) TO cloud_agents_runtime;
GRANT EXECUTE ON FUNCTION cloud_agents.revoke_membership_v2(
    text, bigint, text, bigint, text, text, text, text, text, text, text
) TO cloud_agents_runtime;
GRANT EXECUTE ON FUNCTION cloud_agents.bind_role_v2(
    text, bigint, text, text, text, text, text, text, bigint, text, text,
    timestamptz, text, text, text, text, text, text, text
) TO cloud_agents_runtime;
GRANT EXECUTE ON FUNCTION cloud_agents.revoke_role_binding_v2(
    text, bigint, text, bigint, text, text, text, text, text, text, text
) TO cloud_agents_runtime;

ALTER TABLE cloud_agents_identity.cli_authorizations OWNER TO cloud_agents_migration_owner;
ALTER TABLE cloud_agents_identity.cli_grants OWNER TO cloud_agents_migration_owner;
ALTER TABLE cloud_agents_identity.cli_issued_tokens OWNER TO cloud_agents_migration_owner;
ALTER TABLE cloud_agents_identity.cli_anonymous_attempts OWNER TO cloud_agents_migration_owner;
ALTER TABLE cloud_agents_identity.service_accounts OWNER TO cloud_agents_migration_owner;
ALTER TABLE cloud_agents_identity.service_account_credentials OWNER TO cloud_agents_migration_owner;
ALTER TABLE cloud_agents_identity.service_account_issued_tokens OWNER TO cloud_agents_migration_owner;

REVOKE ALL ON TABLE cloud_agents_identity.cli_authorizations FROM PUBLIC;
REVOKE ALL ON TABLE cloud_agents_identity.cli_authorizations FROM cloud_agents_identity_service;
REVOKE ALL ON TABLE cloud_agents_identity.cli_authorizations FROM cloud_agents_runtime;
REVOKE ALL ON TABLE cloud_agents_identity.cli_grants FROM PUBLIC;
REVOKE ALL ON TABLE cloud_agents_identity.cli_grants FROM cloud_agents_identity_service;
REVOKE ALL ON TABLE cloud_agents_identity.cli_grants FROM cloud_agents_runtime;
REVOKE ALL ON TABLE cloud_agents_identity.cli_issued_tokens FROM PUBLIC;
REVOKE ALL ON TABLE cloud_agents_identity.cli_issued_tokens FROM cloud_agents_identity_service;
REVOKE ALL ON TABLE cloud_agents_identity.cli_issued_tokens FROM cloud_agents_runtime;
REVOKE ALL ON TABLE cloud_agents_identity.cli_anonymous_attempts FROM PUBLIC;
REVOKE ALL ON TABLE cloud_agents_identity.cli_anonymous_attempts FROM cloud_agents_identity_service;
REVOKE ALL ON TABLE cloud_agents_identity.cli_anonymous_attempts FROM cloud_agents_runtime;
REVOKE ALL ON TABLE cloud_agents_identity.service_accounts FROM PUBLIC;
REVOKE ALL ON TABLE cloud_agents_identity.service_accounts FROM cloud_agents_identity_service;
REVOKE ALL ON TABLE cloud_agents_identity.service_accounts FROM cloud_agents_runtime;
REVOKE ALL ON TABLE cloud_agents_identity.service_account_credentials FROM PUBLIC;
REVOKE ALL ON TABLE cloud_agents_identity.service_account_credentials FROM cloud_agents_identity_service;
REVOKE ALL ON TABLE cloud_agents_identity.service_account_credentials FROM cloud_agents_runtime;
REVOKE ALL ON TABLE cloud_agents_identity.service_account_issued_tokens FROM PUBLIC;
REVOKE ALL ON TABLE cloud_agents_identity.service_account_issued_tokens FROM cloud_agents_identity_service;
REVOKE ALL ON TABLE cloud_agents_identity.service_account_issued_tokens FROM cloud_agents_runtime;

REVOKE ALL ON FUNCTION cloud_agents_identity.record_cli_anonymous_attempt(text, bytea) FROM PUBLIC;
REVOKE ALL ON FUNCTION cloud_agents_identity.revoke_cli_grants_after_password_change() FROM PUBLIC;
REVOKE ALL ON FUNCTION cloud_agents_identity.revoke_cli_grants_after_account_disable() FROM PUBLIC;
REVOKE ALL ON FUNCTION cloud_agents_identity.start_cli_authorization(text, text, integer, bytea, text) FROM PUBLIC;
REVOKE ALL ON FUNCTION cloud_agents_identity.approve_cli_authorization(bytea, text, text, bytea, bytea, text, text) FROM PUBLIC;
REVOKE ALL ON FUNCTION cloud_agents_identity.exchange_cli_authorization(text, text, bytea, text, text, bytea, text, text) FROM PUBLIC;
REVOKE ALL ON FUNCTION cloud_agents_identity.revoke_cli_grant(bytea, text, text, text) FROM PUBLIC;
REVOKE ALL ON FUNCTION cloud_agents_identity.list_cli_tenants(bytea, text, text, integer) FROM PUBLIC;
REVOKE ALL ON FUNCTION cloud_agents_identity.read_cli_grant_subject(bytea, text) FROM PUBLIC;
REVOKE ALL ON FUNCTION cloud_agents_identity.record_cli_issued_token(bytea, text, text, text, text, text, text, bytea, timestamptz, text, text) FROM PUBLIC;
REVOKE ALL ON FUNCTION cloud_agents_identity.create_service_account_record(text, text, text, text, text, text, text, text, text, bigint, bytea, timestamptz, text, text, text, text, text) FROM PUBLIC;
REVOKE ALL ON FUNCTION cloud_agents_identity.lock_service_account_tenant_revision(text) FROM PUBLIC;
REVOKE ALL ON FUNCTION cloud_agents_identity.lock_service_account_management(text, text) FROM PUBLIC;
REVOKE ALL ON FUNCTION cloud_agents_identity.rotate_service_account_credential(text, text, bigint, bytea, timestamptz, text, text, text, text, text) FROM PUBLIC;
REVOKE ALL ON FUNCTION cloud_agents_identity.disable_service_account_record(text, text, bigint, text, text, text, text, text) FROM PUBLIC;
REVOKE ALL ON FUNCTION cloud_agents_identity.record_service_account_management_denial(text, text, text, text, text, text, text, text, text, text) FROM PUBLIC;
REVOKE ALL ON FUNCTION cloud_agents_identity.list_service_account_records(text, text, integer) FROM PUBLIC;
REVOKE ALL ON FUNCTION cloud_agents_identity.read_service_account_subject(bytea, text, text) FROM PUBLIC;
REVOKE ALL ON FUNCTION cloud_agents_identity.record_service_account_issued_token(bytea, text, text, text, text, text, text, bytea, timestamptz, text, text) FROM PUBLIC;
REVOKE ALL ON FUNCTION cloud_agents_identity.access_token_is_active_v2(bytea, text, text, text, text) FROM PUBLIC;
REVOKE ALL ON FUNCTION cloud_agents_identity.list_control_plane_audit_facts(bytea, text, timestamptz, text, integer) FROM PUBLIC;

GRANT EXECUTE ON FUNCTION cloud_agents_identity.record_cli_anonymous_attempt(text, bytea) TO cloud_agents_identity_service;
GRANT EXECUTE ON FUNCTION cloud_agents_identity.start_cli_authorization(text, text, integer, bytea, text) TO cloud_agents_identity_service;
GRANT EXECUTE ON FUNCTION cloud_agents_identity.approve_cli_authorization(bytea, text, text, bytea, bytea, text, text) TO cloud_agents_identity_service;
GRANT EXECUTE ON FUNCTION cloud_agents_identity.exchange_cli_authorization(text, text, bytea, text, text, bytea, text, text) TO cloud_agents_identity_service;
GRANT EXECUTE ON FUNCTION cloud_agents_identity.revoke_cli_grant(bytea, text, text, text) TO cloud_agents_identity_service;
GRANT EXECUTE ON FUNCTION cloud_agents_identity.list_cli_tenants(bytea, text, text, integer) TO cloud_agents_identity_service;
GRANT EXECUTE ON FUNCTION cloud_agents_identity.record_cli_issued_token(bytea, text, text, text, text, text, text, bytea, timestamptz, text, text) TO cloud_agents_identity_service;
GRANT EXECUTE ON FUNCTION cloud_agents_identity.record_service_account_issued_token(bytea, text, text, text, text, text, text, bytea, timestamptz, text, text) TO cloud_agents_identity_service;
GRANT EXECUTE ON FUNCTION cloud_agents_identity.access_token_is_active_v2(bytea, text, text, text, text) TO cloud_agents_identity_service;
GRANT EXECUTE ON FUNCTION cloud_agents_identity.list_control_plane_audit_facts(bytea, text, timestamptz, text, integer) TO cloud_agents_identity_service;

GRANT EXECUTE ON FUNCTION cloud_agents_identity.read_cli_grant_subject(bytea, text) TO cloud_agents_runtime;
GRANT EXECUTE ON FUNCTION cloud_agents_identity.create_service_account_record(text, text, text, text, text, text, text, text, text, bigint, bytea, timestamptz, text, text, text, text, text) TO cloud_agents_runtime;
GRANT EXECUTE ON FUNCTION cloud_agents_identity.lock_service_account_tenant_revision(text) TO cloud_agents_runtime;
GRANT EXECUTE ON FUNCTION cloud_agents_identity.lock_service_account_management(text, text) TO cloud_agents_runtime;
GRANT EXECUTE ON FUNCTION cloud_agents_identity.rotate_service_account_credential(text, text, bigint, bytea, timestamptz, text, text, text, text, text) TO cloud_agents_runtime;
GRANT EXECUTE ON FUNCTION cloud_agents_identity.disable_service_account_record(text, text, bigint, text, text, text, text, text) TO cloud_agents_runtime;
GRANT EXECUTE ON FUNCTION cloud_agents_identity.record_service_account_management_denial(text, text, text, text, text, text, text, text, text, text) TO cloud_agents_runtime;
GRANT EXECUTE ON FUNCTION cloud_agents_identity.list_service_account_records(text, text, integer) TO cloud_agents_runtime;
GRANT EXECUTE ON FUNCTION cloud_agents_identity.read_service_account_subject(bytea, text, text) TO cloud_agents_runtime;
