ALTER TABLE cloud_agents_identity.audit_events
    DROP CONSTRAINT audit_events_event_kind_check;
ALTER TABLE cloud_agents_identity.audit_events
    ADD CONSTRAINT audit_events_event_kind_check
    CHECK (event_kind IN ('realm_initialized', 'password_login_succeeded', 'password_login_denied', 'session_revoked', 'token_issued', 'policy_updated', 'invitation_created', 'invitation_revoked', 'invitation_accepted'));
ALTER TABLE cloud_agents_identity.audit_events
    DROP CONSTRAINT audit_events_reason_code_check;
ALTER TABLE cloud_agents_identity.audit_events
    ADD CONSTRAINT audit_events_reason_code_check
    CHECK (reason_code IN ('initialized', 'authenticated', 'invalid_credentials', 'locked', 'logged_out', 'issued', 'policy_updated', 'invitation_created', 'invitation_revoked', 'invitation_accepted'));

CREATE TABLE cloud_agents_identity.invitations (
    id text PRIMARY KEY CHECK (cloud_agents.is_valid_identifier(id)),
    tenant_id text NOT NULL,
    tenant_uid text NOT NULL,
    email text NOT NULL CHECK (octet_length(email) BETWEEN 3 AND 254),
    email_domain text NOT NULL CHECK (octet_length(email_domain) BETWEEN 1 AND 253),
    role_name text NOT NULL,
    role_version bigint NOT NULL CHECK (role_version > 0),
    scope_level text NOT NULL CHECK (scope_level IN ('tenant', 'organization', 'project')),
    scope_id text NOT NULL CHECK (cloud_agents.is_valid_identifier(scope_id)),
    verification text NOT NULL CHECK (verification IN ('admin-attested', 'provider-required')),
    state text NOT NULL CHECK (state IN ('pending', 'accepted', 'revoked')),
    code_digest bytea NOT NULL UNIQUE CHECK (octet_length(code_digest) = 32),
    created_by_user_id text NOT NULL REFERENCES cloud_agents_identity.users (id),
    attestation_source text NOT NULL CHECK (attestation_source IN ('administrator', 'provider-pending')),
    attested_at timestamptz NOT NULL,
    accepted_by_user_id text REFERENCES cloud_agents_identity.users (id),
    accepted_at timestamptz,
    revoked_by_user_id text REFERENCES cloud_agents_identity.users (id),
    revoked_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    expires_at timestamptz NOT NULL,
    CONSTRAINT invitations_tenant_root CHECK (tenant_id = tenant_uid),
    CONSTRAINT invitations_email_domain CHECK (
        email = split_part(email, '@', 1) || '@' || email_domain
        AND split_part(email, '@', 1) <> ''
    ),
    CONSTRAINT invitations_tenant_fk FOREIGN KEY (tenant_id, tenant_uid)
        REFERENCES cloud_agents.platform_tenants (tenant_id, tenant_uid)
        ON UPDATE RESTRICT ON DELETE RESTRICT,
    CONSTRAINT invitations_role_fk FOREIGN KEY (role_name, role_version, scope_level)
        REFERENCES cloud_agents.builtin_roles (role_name, role_version, scope_level)
        ON UPDATE RESTRICT ON DELETE RESTRICT,
    CONSTRAINT invitations_scope CHECK (
        (scope_level = 'tenant' AND scope_id = tenant_id)
        OR scope_level IN ('organization', 'project')
    ),
    CONSTRAINT invitations_expiry CHECK (
        expires_at > created_at AND expires_at <= created_at + interval '24 hours'
    ),
    CONSTRAINT invitations_terminal_state CHECK (
        (state = 'pending' AND accepted_by_user_id IS NULL AND accepted_at IS NULL AND revoked_by_user_id IS NULL AND revoked_at IS NULL)
        OR (state = 'accepted' AND accepted_by_user_id IS NOT NULL AND accepted_at IS NOT NULL AND revoked_by_user_id IS NULL AND revoked_at IS NULL)
        OR (state = 'revoked' AND accepted_by_user_id IS NULL AND accepted_at IS NULL AND revoked_by_user_id IS NOT NULL AND revoked_at IS NOT NULL)
    )
);
CREATE INDEX invitations_tenant_page_idx
    ON cloud_agents_identity.invitations (tenant_id, id);
CREATE INDEX invitations_pending_expiry_idx
    ON cloud_agents_identity.invitations (expires_at, id);
ALTER TABLE cloud_agents_identity.invitations OWNER TO cloud_agents_migration_owner;
REVOKE ALL ON TABLE cloud_agents_identity.invitations FROM PUBLIC;
REVOKE ALL ON TABLE cloud_agents_identity.invitations FROM cloud_agents_identity_service;

CREATE FUNCTION cloud_agents_identity.invitation_domain_allowed(
    p_tenant_id text, p_email_domain text, p_user_id text
) RETURNS boolean
LANGUAGE sql STABLE SECURITY DEFINER
SET search_path = pg_catalog, cloud_agents_identity
AS $body$
    SELECT
        NOT EXISTS (
            SELECT 1 FROM cloud_agents_identity.tenant_email_policies AS policy
            WHERE policy.tenant_id = p_tenant_id AND cardinality(policy.allowed_domains) > 0
        )
        OR EXISTS (
            SELECT 1 FROM cloud_agents_identity.tenant_email_policies AS policy
            WHERE policy.tenant_id = p_tenant_id AND p_email_domain = ANY(policy.allowed_domains)
        )
        OR (
            p_user_id IS NOT NULL
            AND EXISTS (
                SELECT 1
                FROM cloud_agents_identity.users AS account
                JOIN cloud_agents_identity.platform_admins AS admin ON admin.user_id = account.id
                WHERE account.id = p_user_id AND account.disabled_at IS NULL
                    AND admin.revoked_at IS NULL AND account.email LIKE '%@' || p_email_domain
            )
        )
$body$;
REVOKE ALL ON FUNCTION cloud_agents_identity.invitation_domain_allowed(text, text, text) FROM PUBLIC;

CREATE FUNCTION cloud_agents_identity.create_invitation(
    p_session_digest bytea, p_tenant_id text, p_invitation_id text,
    p_code_digest bytea, p_email text, p_email_domain text,
    p_role_name text, p_scope_level text, p_scope_id text, p_verification text,
    p_event_id text, p_correlation_id text
) RETURNS TABLE (
    id text, tenant_id text, email text, role_name text, scope_level text,
    scope_id text, verification text, state text, created_at timestamptz, expires_at timestamptz
)
LANGUAGE plpgsql SECURITY DEFINER
SET search_path = pg_catalog, cloud_agents_identity
AS $body$
DECLARE
    actor_id text;
    actor_platform_admin boolean;
    selected_role_version bigint;
    operation_time timestamptz := clock_timestamp();
BEGIN
    IF p_invitation_id IS NULL OR NOT cloud_agents.is_valid_identifier(p_invitation_id)
        OR p_code_digest IS NULL OR octet_length(p_code_digest) <> 32
        OR p_email IS NULL OR octet_length(p_email) NOT BETWEEN 3 AND 254
        OR p_email_domain IS NULL OR p_email_domain !~ '^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?(\.[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?)*$'
        OR p_email <> split_part(p_email, '@', 1) || '@' || p_email_domain
        OR split_part(p_email, '@', 1) = ''
        OR p_scope_level NOT IN ('tenant', 'organization', 'project')
        OR p_scope_id IS NULL OR NOT cloud_agents.is_valid_identifier(p_scope_id)
        OR (p_scope_level = 'tenant' AND p_scope_id IS DISTINCT FROM p_tenant_id)
        OR p_verification NOT IN ('admin-attested', 'provider-required')
        OR p_role_name = 'platform.admin'
        OR p_event_id IS NULL OR p_event_id !~ '^[a-f0-9]{32}$'
        OR p_correlation_id IS NULL OR NOT cloud_agents.is_valid_identifier(p_correlation_id)
    THEN
        RAISE EXCEPTION USING ERRCODE = '22023', MESSAGE = 'invalid invitation input';
    END IF;
    SELECT authority.actor_user_id, authority.platform_admin
        INTO STRICT actor_id, actor_platform_admin
        FROM cloud_agents_identity.require_tenant_admin(p_session_digest, p_tenant_id) AS authority;
    IF NOT cloud_agents_identity.invitation_domain_allowed(
        p_tenant_id, p_email_domain,
        CASE WHEN actor_platform_admin AND EXISTS (
            SELECT 1 FROM cloud_agents_identity.users AS actor
            WHERE actor.id = actor_id AND actor.email = p_email
        ) THEN actor_id END
    ) THEN
        RAISE EXCEPTION USING ERRCODE = '42501', MESSAGE = 'invitation email policy denied';
    END IF;
    SELECT role.role_version INTO selected_role_version
        FROM cloud_agents.builtin_roles AS role
        WHERE role.role_name = p_role_name AND role.scope_level = p_scope_level
            AND role.state = 'active' AND role.role_name <> 'platform.admin'
        ORDER BY role.catalog_revision DESC, role.role_version DESC LIMIT 1;
    IF selected_role_version IS NULL
        OR (p_scope_level = 'organization' AND NOT EXISTS (
            SELECT 1 FROM cloud_agents.organizations AS organization
            WHERE organization.tenant_id = p_tenant_id
                AND organization.organization_uid = p_scope_id AND organization.state = 'active'
        ))
        OR (p_scope_level = 'project' AND NOT EXISTS (
            SELECT 1 FROM cloud_agents.projects AS project
            JOIN cloud_agents.organizations AS organization
                ON organization.tenant_id = project.tenant_id
                AND organization.organization_uid = project.organization_uid
            WHERE project.tenant_id = p_tenant_id
                AND project.project_uid = p_scope_id AND project.state = 'active'
                AND organization.state = 'active'
        ))
    THEN
        RAISE EXCEPTION USING ERRCODE = '42501', MESSAGE = 'invitation scope denied';
    END IF;
    INSERT INTO cloud_agents_identity.invitations (
        id, tenant_id, tenant_uid, email, email_domain, role_name, role_version,
        scope_level, scope_id, verification, state, code_digest,
        created_by_user_id, attestation_source, attested_at, created_at, expires_at
    ) VALUES (
        p_invitation_id, p_tenant_id, p_tenant_id, p_email, p_email_domain,
        p_role_name, selected_role_version, p_scope_level, p_scope_id,
        p_verification, 'pending', p_code_digest, actor_id,
        CASE WHEN p_verification = 'admin-attested' THEN 'administrator' ELSE 'provider-pending' END,
        operation_time, operation_time, operation_time + interval '24 hours'
    );
    INSERT INTO cloud_agents_identity.audit_events (
        id, event_kind, user_id, actor_user_id, tenant_id, application,
        decision, reason_code, correlation_id
    ) VALUES (
        p_event_id, 'invitation_created', actor_id, actor_id, p_tenant_id, 'admin',
        'allow', 'invitation_created', p_correlation_id
    );
    RETURN QUERY SELECT invitation.id, invitation.tenant_id, invitation.email,
        invitation.role_name, invitation.scope_level, invitation.scope_id,
        invitation.verification, invitation.state, invitation.created_at, invitation.expires_at
        FROM cloud_agents_identity.invitations AS invitation
        WHERE invitation.id = p_invitation_id;
END
$body$;

CREATE FUNCTION cloud_agents.accept_identity_invitation_v1(
    p_invitation_id text, p_user_id text,
    p_membership_uid text, p_role_binding_uid text,
    p_membership_audit_id text, p_role_binding_audit_id text
) RETURNS TABLE (membership_uid text, role_binding_uid text)
LANGUAGE plpgsql SECURITY DEFINER
SET search_path = pg_catalog, cloud_agents
AS $body$
DECLARE
    invitation cloud_agents_identity.invitations%ROWTYPE;
    realm_issuer text;
    account_subject text;
    account_digest text;
    target_organization_uid text;
    selected_membership_uid text;
    selected_membership_admission bigint;
    selected_binding_uid text;
    selected_binding_version bigint;
    allocated_revision bigint;
    operation_time timestamptz := clock_timestamp();
BEGIN
    IF p_invitation_id IS NULL OR NOT cloud_agents.is_valid_identifier(p_invitation_id)
        OR p_user_id IS NULL OR p_user_id !~ '^[A-Za-z0-9]([A-Za-z0-9._~-]{0,126}[A-Za-z0-9])?$'
        OR p_membership_uid IS NULL OR NOT cloud_agents.is_valid_identifier(p_membership_uid)
        OR p_role_binding_uid IS NULL OR NOT cloud_agents.is_valid_identifier(p_role_binding_uid)
        OR p_membership_audit_id IS NULL OR NOT cloud_agents.is_valid_identifier(p_membership_audit_id)
        OR p_role_binding_audit_id IS NULL OR NOT cloud_agents.is_valid_identifier(p_role_binding_audit_id)
    THEN
        RAISE EXCEPTION USING ERRCODE = '22023', MESSAGE = 'invalid invitation admission input';
    END IF;

    SELECT candidate.* INTO invitation
        FROM cloud_agents_identity.invitations AS candidate
        WHERE candidate.id = p_invitation_id
        FOR UPDATE;
    IF NOT FOUND OR invitation.state <> 'pending' OR invitation.expires_at <= operation_time THEN
        RAISE EXCEPTION USING ERRCODE = '40001', MESSAGE = 'invitation state conflict';
    END IF;
    IF NOT EXISTS (
        SELECT 1 FROM cloud_agents_identity.users AS account
        WHERE account.id = p_user_id AND account.email = invitation.email
            AND account.disabled_at IS NULL
    ) OR NOT EXISTS (
        SELECT 1 FROM cloud_agents.platform_tenants AS tenant
        WHERE tenant.tenant_id = invitation.tenant_id
            AND tenant.tenant_uid = invitation.tenant_id AND tenant.state = 'active'
    ) OR NOT EXISTS (
        SELECT 1 FROM cloud_agents.builtin_roles AS role
        WHERE role.role_name = invitation.role_name
            AND role.role_version = invitation.role_version
            AND role.scope_level = invitation.scope_level
            AND role.state = 'active' AND role.role_name <> 'platform.admin'
    ) THEN
        RAISE EXCEPTION USING ERRCODE = '42501', MESSAGE = 'invitation admission denied';
    END IF;

    IF invitation.scope_level = 'organization' THEN
        SELECT organization.organization_uid INTO target_organization_uid
            FROM cloud_agents.organizations AS organization
            WHERE organization.tenant_id = invitation.tenant_id
                AND organization.organization_uid = invitation.scope_id
                AND organization.state = 'active';
    ELSIF invitation.scope_level = 'project' THEN
        SELECT project.organization_uid INTO target_organization_uid
            FROM cloud_agents.projects AS project
            JOIN cloud_agents.organizations AS organization
                ON organization.tenant_id = project.tenant_id
                AND organization.organization_uid = project.organization_uid
            WHERE project.tenant_id = invitation.tenant_id
                AND project.project_uid = invitation.scope_id
                AND project.state = 'active' AND organization.state = 'active';
    END IF;
    IF invitation.scope_level IN ('organization', 'project') AND target_organization_uid IS NULL THEN
        RAISE EXCEPTION USING ERRCODE = '42501', MESSAGE = 'invitation scope denied';
    END IF;

    SELECT realm.issuer INTO STRICT realm_issuer
        FROM cloud_agents_identity.realm AS realm WHERE realm.singleton;
    account_subject := 'user-' || p_user_id;
    account_digest := cloud_agents.subject_ref_digest('user', realm_issuer, account_subject);

    SELECT membership.membership_uid, admission.resource_version
        INTO selected_membership_uid, selected_membership_admission
        FROM cloud_agents.memberships AS membership
        JOIN cloud_agents.resource_changes AS admission
            ON admission.tenant_id = membership.tenant_id
            AND admission.resource_kind = 'membership'
            AND admission.resource_uid = membership.membership_uid
            AND admission.change_kind = 'created'
        LEFT JOIN cloud_agents.projects AS membership_project
            ON membership.scope_level = 'project'
            AND membership_project.tenant_id = membership.tenant_id
            AND membership_project.project_uid = membership.scope_project_uid
        WHERE membership.tenant_id = invitation.tenant_id
            AND membership.subject_kind = 'user'
            AND membership.subject_issuer = realm_issuer
            AND membership.subject_value = account_subject
            AND membership.subject_digest = account_digest
            AND membership.state = 'active'
            AND (membership.expires_at IS NULL OR membership.expires_at > operation_time)
            AND (
                membership.scope_level = 'tenant'
                OR (membership.scope_level = 'organization'
                    AND membership.scope_organization_uid = target_organization_uid)
                OR (membership.scope_level = 'project'
                    AND invitation.scope_level = 'project'
                    AND membership.scope_project_uid = invitation.scope_id
                    AND membership_project.organization_uid = target_organization_uid)
            )
        ORDER BY CASE membership.scope_level
            WHEN 'tenant' THEN 1 WHEN 'organization' THEN 2 ELSE 3 END,
            membership.membership_uid
        LIMIT 1;

    IF selected_membership_uid IS NULL AND EXISTS (
        SELECT 1
        FROM cloud_agents.memberships AS membership
        LEFT JOIN cloud_agents.projects AS membership_project
            ON membership.scope_level = 'project'
            AND membership_project.tenant_id = membership.tenant_id
            AND membership_project.project_uid = membership.scope_project_uid
        WHERE membership.tenant_id = invitation.tenant_id
            AND membership.subject_kind = 'user'
            AND membership.subject_issuer = realm_issuer
            AND membership.subject_value = account_subject
            AND membership.subject_digest = account_digest
            AND membership.state <> 'revoked'
            AND (
                membership.scope_level = 'tenant'
                OR (membership.scope_level = 'organization'
                    AND membership.scope_organization_uid = target_organization_uid)
                OR (membership.scope_level = 'project'
                    AND invitation.scope_level = 'project'
                    AND membership.scope_project_uid = invitation.scope_id
                    AND membership_project.organization_uid = target_organization_uid)
            )
    ) THEN
        RAISE EXCEPTION USING ERRCODE = '40001', MESSAGE = 'invitation membership conflict';
    END IF;

    SELECT revision.current_revision INTO allocated_revision
        FROM cloud_agents.tenant_resource_versions AS revision
        JOIN cloud_agents.platform_tenants AS tenant
            ON tenant.tenant_id = revision.tenant_id
            AND tenant.tenant_uid = revision.tenant_uid
        WHERE revision.tenant_id = invitation.tenant_id
            AND revision.tenant_uid = invitation.tenant_id AND tenant.state = 'active'
        FOR UPDATE OF revision;
    IF allocated_revision IS NULL OR allocated_revision > 9223372036854775805 THEN
        RAISE EXCEPTION USING ERRCODE = '22003', MESSAGE = 'tenant resource revision unavailable';
    END IF;

    IF selected_membership_uid IS NULL THEN
        allocated_revision := allocated_revision + 1;
        UPDATE cloud_agents.tenant_resource_versions AS revision
            SET current_revision = allocated_revision, updated_at = operation_time
            WHERE revision.tenant_id = invitation.tenant_id
                AND revision.tenant_uid = invitation.tenant_id;
        INSERT INTO cloud_agents.resource_changes (
            tenant_id, tenant_uid, resource_version, resource_kind,
            resource_uid, change_kind, actor_database_principal, occurred_at
        ) VALUES (
            invitation.tenant_id, invitation.tenant_id, allocated_revision, 'membership',
            p_membership_uid, 'created', SESSION_USER, operation_time
        );
        INSERT INTO cloud_agents.memberships (
            tenant_id, tenant_ref_id, membership_uid, membership_name,
            subject_kind, subject_issuer, subject_value, subject_digest,
            scope_level, scope_tenant_uid, scope_organization_uid, scope_project_uid,
            state, resource_version, created_at, updated_at
        ) VALUES (
            invitation.tenant_id, invitation.tenant_id, p_membership_uid, p_membership_uid,
            'user', realm_issuer, account_subject, account_digest,
            invitation.scope_level,
            CASE WHEN invitation.scope_level = 'tenant' THEN invitation.scope_id END,
            CASE WHEN invitation.scope_level = 'organization' THEN invitation.scope_id END,
            CASE WHEN invitation.scope_level = 'project' THEN invitation.scope_id END,
            'active', allocated_revision, operation_time, operation_time
        );
        INSERT INTO cloud_agents.audit_facts (
            tenant_id, tenant_uid, audit_fact_uid, resource_version, action,
            resource_kind, resource_uid, actor_database_principal, reason_code, occurred_at
        ) VALUES (
            invitation.tenant_id, invitation.tenant_id, p_membership_audit_id,
            allocated_revision, 'membership.create', 'membership', p_membership_uid,
            SESSION_USER, 'identity.invitation.accept', operation_time
        );
        selected_membership_uid := p_membership_uid;
        selected_membership_admission := allocated_revision;
    END IF;

    SELECT binding.role_binding_uid, binding.resource_version
        INTO selected_binding_uid, selected_binding_version
        FROM cloud_agents.role_bindings AS binding
        WHERE binding.tenant_id = invitation.tenant_id
            AND binding.subject_kind = 'user'
            AND binding.subject_issuer = realm_issuer
            AND binding.subject_value = account_subject
            AND binding.subject_digest = account_digest
            AND binding.role_name = invitation.role_name
            AND binding.role_version = invitation.role_version
            AND binding.scope_level = invitation.scope_level
            AND CASE invitation.scope_level
                WHEN 'tenant' THEN binding.scope_tenant_uid = invitation.scope_id
                WHEN 'organization' THEN binding.scope_organization_uid = invitation.scope_id
                WHEN 'project' THEN binding.scope_project_uid = invitation.scope_id
            END
            AND binding.state = 'active'
            AND (binding.expires_at IS NULL OR binding.expires_at > operation_time)
        ORDER BY binding.role_binding_uid LIMIT 1;
    IF selected_binding_uid IS NOT NULL
        AND selected_membership_admission >= selected_binding_version
    THEN
        RAISE EXCEPTION USING ERRCODE = '40001', MESSAGE = 'invitation admission ordering conflict';
    END IF;
    IF selected_binding_uid IS NULL AND EXISTS (
        SELECT 1 FROM cloud_agents.role_bindings AS binding
        WHERE binding.tenant_id = invitation.tenant_id
            AND binding.subject_kind = 'user'
            AND binding.subject_issuer = realm_issuer
            AND binding.subject_value = account_subject
            AND binding.subject_digest = account_digest
            AND binding.role_name = invitation.role_name
            AND binding.role_version = invitation.role_version
            AND binding.scope_level = invitation.scope_level
            AND CASE invitation.scope_level
                WHEN 'tenant' THEN binding.scope_tenant_uid = invitation.scope_id
                WHEN 'organization' THEN binding.scope_organization_uid = invitation.scope_id
                WHEN 'project' THEN binding.scope_project_uid = invitation.scope_id
            END
            AND binding.state <> 'revoked'
    ) THEN
        RAISE EXCEPTION USING ERRCODE = '40001', MESSAGE = 'invitation role binding conflict';
    END IF;

    IF selected_binding_uid IS NULL THEN
        allocated_revision := allocated_revision + 1;
        UPDATE cloud_agents.tenant_resource_versions AS revision
            SET current_revision = allocated_revision, updated_at = operation_time
            WHERE revision.tenant_id = invitation.tenant_id
                AND revision.tenant_uid = invitation.tenant_id;
        INSERT INTO cloud_agents.resource_changes (
            tenant_id, tenant_uid, resource_version, resource_kind,
            resource_uid, change_kind, actor_database_principal, occurred_at
        ) VALUES (
            invitation.tenant_id, invitation.tenant_id, allocated_revision, 'role_binding',
            p_role_binding_uid, 'created', SESSION_USER, operation_time
        );
        INSERT INTO cloud_agents.role_bindings (
            tenant_id, tenant_ref_id, role_binding_uid, role_binding_name,
            subject_kind, subject_issuer, subject_value, subject_digest,
            role_name, role_version, scope_level,
            scope_tenant_uid, scope_organization_uid, scope_project_uid,
            state, resource_version, created_at, updated_at
        ) VALUES (
            invitation.tenant_id, invitation.tenant_id,
            p_role_binding_uid, p_role_binding_uid,
            'user', realm_issuer, account_subject, account_digest,
            invitation.role_name, invitation.role_version, invitation.scope_level,
            CASE WHEN invitation.scope_level = 'tenant' THEN invitation.scope_id END,
            CASE WHEN invitation.scope_level = 'organization' THEN invitation.scope_id END,
            CASE WHEN invitation.scope_level = 'project' THEN invitation.scope_id END,
            'active', allocated_revision, operation_time, operation_time
        );
        INSERT INTO cloud_agents.audit_facts (
            tenant_id, tenant_uid, audit_fact_uid, resource_version, action,
            resource_kind, resource_uid, actor_database_principal, reason_code, occurred_at
        ) VALUES (
            invitation.tenant_id, invitation.tenant_id, p_role_binding_audit_id,
            allocated_revision, 'role_binding.bind', 'role_binding', p_role_binding_uid,
            SESSION_USER, 'identity.invitation.accept', operation_time
        );
        selected_binding_uid := p_role_binding_uid;
    END IF;

    RETURN QUERY SELECT selected_membership_uid, selected_binding_uid;
END
$body$;

CREATE FUNCTION cloud_agents_identity.prepare_invitation_accept(
    p_code_digest bytea, p_application text, p_session_digest bytea
) RETURNS TABLE (invitation_id text, email text, existing_user_id text)
LANGUAGE plpgsql SECURITY DEFINER
SET search_path = pg_catalog, cloud_agents_identity
AS $body$
DECLARE
    invitation cloud_agents_identity.invitations%ROWTYPE;
    account cloud_agents_identity.users%ROWTYPE;
    session_user_id text;
    session_email text;
BEGIN
    PERFORM cloud_agents_identity.require_principal('cloud_agents_identity_service');
    IF p_code_digest IS NULL OR octet_length(p_code_digest) <> 32
        OR p_application NOT IN ('admin', 'user')
        OR (p_session_digest IS NOT NULL AND octet_length(p_session_digest) <> 32)
    THEN
        RAISE EXCEPTION USING ERRCODE = '22023', MESSAGE = 'invalid invitation acceptance';
    END IF;
    SELECT candidate.* INTO invitation
        FROM cloud_agents_identity.invitations AS candidate
        WHERE candidate.code_digest = p_code_digest
        FOR UPDATE;
    IF NOT FOUND THEN
        RAISE EXCEPTION USING ERRCODE = '28000', MESSAGE = 'invitation unavailable';
    END IF;
    IF invitation.state <> 'pending' OR invitation.expires_at <= clock_timestamp() THEN
        RAISE EXCEPTION USING ERRCODE = '40001', MESSAGE = 'invitation state conflict';
    END IF;
    IF invitation.verification <> 'admin-attested' THEN
        RAISE EXCEPTION USING ERRCODE = '42501', MESSAGE = 'invitation verification unavailable';
    END IF;
    SELECT candidate.* INTO account
        FROM cloud_agents_identity.users AS candidate
        WHERE candidate.email = invitation.email;
    IF FOUND AND account.disabled_at IS NOT NULL THEN
        RAISE EXCEPTION USING ERRCODE = '42501', MESSAGE = 'invitation account unavailable';
    END IF;
    IF p_session_digest IS NULL THEN
        IF account.id IS NOT NULL THEN
            RAISE EXCEPTION USING ERRCODE = '28000', MESSAGE = 'authenticated invitation acceptance required';
        END IF;
    ELSE
        SELECT session.user_id, session.email INTO session_user_id, session_email
            FROM cloud_agents_identity.read_session(p_session_digest, p_application) AS session;
        IF session_user_id IS NULL OR session_email IS DISTINCT FROM invitation.email
            OR account.id IS DISTINCT FROM session_user_id
        THEN
            RAISE EXCEPTION USING ERRCODE = '28000', MESSAGE = 'identity session unavailable';
        END IF;
    END IF;
    IF NOT cloud_agents_identity.invitation_domain_allowed(
        invitation.tenant_id, invitation.email_domain, account.id
    ) THEN
        RAISE EXCEPTION USING ERRCODE = '42501', MESSAGE = 'invitation email policy denied';
    END IF;
    RETURN QUERY SELECT invitation.id, invitation.email, account.id;
END
$body$;

CREATE FUNCTION cloud_agents_identity.finish_invitation_accept(
    p_code_digest bytea, p_application text, p_session_digest bytea,
    p_user_id text, p_password_hash text, p_display_name text,
    p_membership_uid text, p_role_binding_uid text,
    p_membership_audit_id text, p_role_binding_audit_id text,
    p_event_id text, p_correlation_id text
) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER
SET search_path = pg_catalog, cloud_agents_identity
AS $body$
DECLARE
    invitation cloud_agents_identity.invitations%ROWTYPE;
    existing_user_id text;
    session_user_id text;
    session_email text;
    admitted_membership text;
    admitted_binding text;
BEGIN
    PERFORM cloud_agents_identity.require_principal('cloud_agents_identity_service');
    IF p_code_digest IS NULL OR octet_length(p_code_digest) <> 32
        OR p_application NOT IN ('admin', 'user')
        OR (p_session_digest IS NOT NULL AND octet_length(p_session_digest) <> 32)
        OR p_user_id IS NULL OR p_user_id !~ '^[A-Za-z0-9]([A-Za-z0-9._~-]{0,126}[A-Za-z0-9])?$'
        OR p_event_id IS NULL OR p_event_id !~ '^[a-f0-9]{32}$'
        OR p_correlation_id IS NULL OR NOT cloud_agents.is_valid_identifier(p_correlation_id)
    THEN
        RAISE EXCEPTION USING ERRCODE = '22023', MESSAGE = 'invalid invitation acceptance';
    END IF;
    SELECT candidate.* INTO invitation
        FROM cloud_agents_identity.invitations AS candidate
        WHERE candidate.code_digest = p_code_digest
        FOR UPDATE;
    IF NOT FOUND THEN
        RAISE EXCEPTION USING ERRCODE = '28000', MESSAGE = 'invitation unavailable';
    END IF;
    IF invitation.state <> 'pending' OR invitation.expires_at <= clock_timestamp() THEN
        RAISE EXCEPTION USING ERRCODE = '40001', MESSAGE = 'invitation state conflict';
    END IF;
    IF invitation.verification <> 'admin-attested'
        OR NOT cloud_agents_identity.invitation_domain_allowed(
            invitation.tenant_id, invitation.email_domain,
            CASE WHEN p_session_digest IS NOT NULL THEN p_user_id END
        )
    THEN
        RAISE EXCEPTION USING ERRCODE = '42501', MESSAGE = 'invitation acceptance denied';
    END IF;

    SELECT account.id INTO existing_user_id
        FROM cloud_agents_identity.users AS account
        WHERE account.email = invitation.email AND account.disabled_at IS NULL;
    IF p_session_digest IS NULL THEN
        IF existing_user_id IS NOT NULL
            OR p_password_hash IS NULL
            OR p_password_hash !~ '^\$argon2id\$v=19\$m=19456,t=2,p=1\$[A-Za-z0-9+/]{22}\$[A-Za-z0-9+/]{43}$'
            OR p_display_name IS NULL OR length(p_display_name) NOT BETWEEN 1 AND 160
        THEN
            RAISE EXCEPTION USING ERRCODE = '40001', MESSAGE = 'invitation account conflict';
        END IF;
        INSERT INTO cloud_agents_identity.users (
            id, email, display_name, email_verified_at
        ) VALUES (
            p_user_id, invitation.email, p_display_name, invitation.attested_at
        );
        INSERT INTO cloud_agents_identity.password_credentials (user_id, password_hash)
            VALUES (p_user_id, p_password_hash);
    ELSE
        IF p_password_hash IS NOT NULL OR p_display_name IS NOT NULL THEN
            RAISE EXCEPTION USING ERRCODE = '22023', MESSAGE = 'existing account credentials are immutable';
        END IF;
        SELECT session.user_id, session.email INTO session_user_id, session_email
            FROM cloud_agents_identity.read_session(p_session_digest, p_application) AS session;
        IF session_user_id IS NULL OR session_user_id IS DISTINCT FROM p_user_id
            OR session_email IS DISTINCT FROM invitation.email
            OR existing_user_id IS DISTINCT FROM p_user_id
        THEN
            RAISE EXCEPTION USING ERRCODE = '28000', MESSAGE = 'identity session unavailable';
        END IF;
    END IF;

    SELECT admission.membership_uid, admission.role_binding_uid
        INTO admitted_membership, admitted_binding
        FROM cloud_agents.accept_identity_invitation_v1(
            invitation.id, p_user_id, p_membership_uid, p_role_binding_uid,
            p_membership_audit_id, p_role_binding_audit_id
        ) AS admission;
    IF admitted_membership IS NULL OR admitted_binding IS NULL THEN
        RAISE EXCEPTION USING ERRCODE = '40001', MESSAGE = 'invitation admission conflict';
    END IF;
    UPDATE cloud_agents_identity.invitations AS accepted
        SET state = 'accepted', accepted_by_user_id = p_user_id, accepted_at = clock_timestamp()
        WHERE accepted.id = invitation.id AND accepted.state = 'pending';
    IF NOT FOUND THEN
        RAISE EXCEPTION USING ERRCODE = '40001', MESSAGE = 'invitation state conflict';
    END IF;
    INSERT INTO cloud_agents_identity.audit_events (
        id, event_kind, user_id, actor_user_id, target_user_id, tenant_id,
        application, decision, reason_code, correlation_id
    ) VALUES (
        p_event_id, 'invitation_accepted', p_user_id, p_user_id, p_user_id,
        invitation.tenant_id, p_application, 'allow', 'invitation_accepted', p_correlation_id
    );
    RETURN true;
EXCEPTION
    WHEN unique_violation THEN
        RAISE EXCEPTION USING ERRCODE = '40001', MESSAGE = 'invitation acceptance conflict';
END
$body$;

CREATE FUNCTION cloud_agents_identity.list_invitations(
    p_session_digest bytea, p_tenant_id text, p_after text, p_limit integer
) RETURNS TABLE (
    id text, tenant_id text, email text, role_name text, scope_level text,
    scope_id text, verification text, state text, created_at timestamptz, expires_at timestamptz
)
LANGUAGE plpgsql SECURITY DEFINER
SET search_path = pg_catalog, cloud_agents_identity
AS $body$
BEGIN
    PERFORM 1 FROM cloud_agents_identity.require_tenant_admin(p_session_digest, p_tenant_id);
    IF p_after IS NULL OR (p_after <> '' AND NOT cloud_agents.is_valid_identifier(p_after))
        OR p_limit IS NULL OR p_limit NOT BETWEEN 1 AND 201
    THEN
        RAISE EXCEPTION USING ERRCODE = '22023', MESSAGE = 'invalid invitation page';
    END IF;
    RETURN QUERY SELECT invitation.id, invitation.tenant_id, invitation.email,
        invitation.role_name, invitation.scope_level, invitation.scope_id,
        invitation.verification,
        CASE WHEN invitation.state = 'pending' AND invitation.expires_at <= clock_timestamp()
            THEN 'expired' ELSE invitation.state END,
        invitation.created_at, invitation.expires_at
        FROM cloud_agents_identity.invitations AS invitation
        WHERE invitation.tenant_id = p_tenant_id AND invitation.id > p_after
        ORDER BY invitation.id LIMIT p_limit;
END
$body$;

CREATE FUNCTION cloud_agents_identity.revoke_invitation(
    p_session_digest bytea, p_tenant_id text, p_invitation_id text,
    p_event_id text, p_correlation_id text
) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER
SET search_path = pg_catalog, cloud_agents_identity
AS $body$
DECLARE
    actor_id text;
    current_state text;
    current_expiry timestamptz;
BEGIN
    IF p_invitation_id IS NULL OR NOT cloud_agents.is_valid_identifier(p_invitation_id)
        OR p_event_id IS NULL OR p_event_id !~ '^[a-f0-9]{32}$'
        OR p_correlation_id IS NULL OR NOT cloud_agents.is_valid_identifier(p_correlation_id)
    THEN
        RAISE EXCEPTION USING ERRCODE = '22023', MESSAGE = 'invalid invitation revocation';
    END IF;
    SELECT authority.actor_user_id INTO STRICT actor_id
        FROM cloud_agents_identity.require_tenant_admin(p_session_digest, p_tenant_id) AS authority;
    SELECT invitation.state, invitation.expires_at INTO current_state, current_expiry
        FROM cloud_agents_identity.invitations AS invitation
        WHERE invitation.tenant_id = p_tenant_id AND invitation.id = p_invitation_id
        FOR UPDATE;
    IF NOT FOUND THEN
        RAISE EXCEPTION USING ERRCODE = '42501', MESSAGE = 'invitation unavailable';
    END IF;
    IF current_state = 'revoked' THEN RETURN true; END IF;
    IF current_state <> 'pending' OR current_expiry <= clock_timestamp() THEN
        RAISE EXCEPTION USING ERRCODE = '40001', MESSAGE = 'invitation state conflict';
    END IF;
    UPDATE cloud_agents_identity.invitations AS invitation
        SET state = 'revoked', revoked_by_user_id = actor_id, revoked_at = clock_timestamp()
        WHERE invitation.tenant_id = p_tenant_id AND invitation.id = p_invitation_id;
    INSERT INTO cloud_agents_identity.audit_events (
        id, event_kind, user_id, actor_user_id, target_user_id, tenant_id,
        application, decision, reason_code, correlation_id
    ) VALUES (
        p_event_id, 'invitation_revoked', actor_id, actor_id, NULL, p_tenant_id,
        'admin', 'allow', 'invitation_revoked', p_correlation_id
    );
    RETURN true;
END
$body$;

REVOKE ALL ON FUNCTION cloud_agents.accept_identity_invitation_v1(text, text, text, text, text, text) FROM PUBLIC;
REVOKE ALL ON FUNCTION cloud_agents_identity.create_invitation(bytea, text, text, bytea, text, text, text, text, text, text, text, text) FROM PUBLIC;
REVOKE ALL ON FUNCTION cloud_agents_identity.list_invitations(bytea, text, text, integer) FROM PUBLIC;
REVOKE ALL ON FUNCTION cloud_agents_identity.revoke_invitation(bytea, text, text, text, text) FROM PUBLIC;
REVOKE ALL ON FUNCTION cloud_agents_identity.prepare_invitation_accept(bytea, text, bytea) FROM PUBLIC;
REVOKE ALL ON FUNCTION cloud_agents_identity.finish_invitation_accept(bytea, text, bytea, text, text, text, text, text, text, text, text, text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION cloud_agents_identity.create_invitation(bytea, text, text, bytea, text, text, text, text, text, text, text, text) TO cloud_agents_identity_service;
GRANT EXECUTE ON FUNCTION cloud_agents_identity.list_invitations(bytea, text, text, integer) TO cloud_agents_identity_service;
GRANT EXECUTE ON FUNCTION cloud_agents_identity.revoke_invitation(bytea, text, text, text, text) TO cloud_agents_identity_service;
GRANT EXECUTE ON FUNCTION cloud_agents_identity.prepare_invitation_accept(bytea, text, bytea) TO cloud_agents_identity_service;
GRANT EXECUTE ON FUNCTION cloud_agents_identity.finish_invitation_accept(bytea, text, bytea, text, text, text, text, text, text, text, text, text) TO cloud_agents_identity_service;
