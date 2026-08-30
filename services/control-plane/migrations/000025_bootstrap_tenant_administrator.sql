CREATE FUNCTION cloud_agents.bootstrap_tenant_administrator_v1(
    p_tenant_uid text,
    p_tenant_name text,
    p_tenant_display_name text,
    p_organization_uid text,
    p_organization_name text,
    p_organization_display_name text,
    p_subject_kind text,
    p_subject_issuer text,
    p_subject_value text,
    p_membership_uid text,
    p_membership_name text,
    p_role_binding_uid text,
    p_role_binding_name text,
    p_tenant_audit_fact_uid text,
    p_membership_audit_fact_uid text,
    p_role_binding_audit_fact_uid text,
    p_reason_code text
)
RETURNS TABLE (
    tenant_id text,
    organization_uid text,
    membership_uid text,
    role_binding_uid text,
    resource_version bigint
)
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, cloud_agents
AS $cloud_agents_function$
DECLARE
    bootstrap_at timestamptz;
    computed_subject_digest text;
    full_intent_matches boolean;
    tenant_intent_matches boolean;
    next_revision bigint;
BEGIN
    IF NOT cloud_agents.is_valid_identifier(p_organization_uid)
        OR NOT cloud_agents.is_valid_identifier(p_organization_name)
        OR NOT cloud_agents.is_valid_identifier(p_membership_uid)
        OR NOT cloud_agents.is_valid_identifier(p_membership_name)
        OR NOT cloud_agents.is_valid_identifier(p_role_binding_uid)
        OR NOT cloud_agents.is_valid_identifier(p_role_binding_name)
        OR NOT cloud_agents.is_valid_identifier(p_membership_audit_fact_uid)
        OR NOT cloud_agents.is_valid_identifier(p_role_binding_audit_fact_uid)
        OR NOT cloud_agents.is_valid_identifier(p_reason_code)
        OR p_organization_display_name IS NULL
        OR pg_catalog.char_length(p_organization_display_name) NOT BETWEEN 1 AND 160
    THEN
        RAISE EXCEPTION USING
            ERRCODE = '22023',
            MESSAGE = 'tenant administrator bootstrap input is invalid';
    END IF;

    computed_subject_digest := cloud_agents.subject_ref_digest(
        p_subject_kind,
        p_subject_issuer,
        p_subject_value
    );

    BEGIN
        PERFORM 1
        FROM cloud_agents.bootstrap_platform_tenant(
            p_tenant_uid,
            p_tenant_name,
            p_tenant_display_name,
            p_tenant_audit_fact_uid,
            p_reason_code
        );
    EXCEPTION
        WHEN unique_violation THEN
            NULL;
    END;

    SELECT EXISTS (
        SELECT 1
        FROM cloud_agents.platform_tenants AS tenant
        JOIN cloud_agents.tenant_resource_versions AS revision
            ON revision.tenant_id = tenant.tenant_id
            AND revision.tenant_uid = tenant.tenant_uid
        JOIN cloud_agents.resource_changes AS tenant_change
            ON tenant_change.tenant_id = tenant.tenant_id
            AND tenant_change.resource_version = 1
            AND tenant_change.resource_kind = 'platform_tenant'
            AND tenant_change.resource_uid = tenant.tenant_uid
        JOIN cloud_agents.audit_facts AS tenant_audit
            ON tenant_audit.tenant_id = tenant.tenant_id
            AND tenant_audit.resource_version = 1
            AND tenant_audit.resource_kind = 'platform_tenant'
            AND tenant_audit.resource_uid = tenant.tenant_uid
        JOIN cloud_agents.organizations AS organization
            ON organization.tenant_id = tenant.tenant_id
            AND organization.organization_uid = p_organization_uid
        JOIN cloud_agents.resource_changes AS organization_change
            ON organization_change.tenant_id = tenant.tenant_id
            AND organization_change.resource_version = 2
            AND organization_change.resource_kind = 'organization'
            AND organization_change.resource_uid = organization.organization_uid
        JOIN cloud_agents.memberships AS membership
            ON membership.tenant_id = tenant.tenant_id
            AND membership.membership_uid = p_membership_uid
        JOIN cloud_agents.resource_changes AS membership_change
            ON membership_change.tenant_id = tenant.tenant_id
            AND membership_change.resource_version = 3
            AND membership_change.resource_kind = 'membership'
            AND membership_change.resource_uid = membership.membership_uid
        JOIN cloud_agents.audit_facts AS membership_audit
            ON membership_audit.tenant_id = tenant.tenant_id
            AND membership_audit.resource_version = 3
            AND membership_audit.resource_kind = 'membership'
            AND membership_audit.resource_uid = membership.membership_uid
        JOIN cloud_agents.role_bindings AS binding
            ON binding.tenant_id = tenant.tenant_id
            AND binding.role_binding_uid = p_role_binding_uid
        JOIN cloud_agents.resource_changes AS binding_change
            ON binding_change.tenant_id = tenant.tenant_id
            AND binding_change.resource_version = 4
            AND binding_change.resource_kind = 'role_binding'
            AND binding_change.resource_uid = binding.role_binding_uid
        JOIN cloud_agents.audit_facts AS binding_audit
            ON binding_audit.tenant_id = tenant.tenant_id
            AND binding_audit.resource_version = 4
            AND binding_audit.resource_kind = 'role_binding'
            AND binding_audit.resource_uid = binding.role_binding_uid
        WHERE tenant.tenant_id = p_tenant_uid
            AND tenant.tenant_uid = p_tenant_uid
            AND tenant.tenant_name = p_tenant_name
            AND pg_catalog.convert_to(tenant.display_name, 'UTF8')
                = pg_catalog.convert_to(p_tenant_display_name, 'UTF8')
            AND tenant.state = 'active'
            AND tenant.resource_version = 1
            AND tenant.created_at = tenant.updated_at
            AND revision.current_revision = 4
            AND revision.updated_at = organization.updated_at
            AND tenant_change.change_kind = 'created'
            AND tenant_change.actor_database_principal = SESSION_USER
            AND tenant_change.occurred_at = tenant.created_at
            AND tenant_audit.audit_fact_uid = p_tenant_audit_fact_uid
            AND tenant_audit.action = 'platform_tenant.bootstrap'
            AND tenant_audit.actor_database_principal = SESSION_USER
            AND tenant_audit.reason_code = p_reason_code
            AND tenant_audit.occurred_at = tenant.created_at
            AND organization.organization_name = p_organization_name
            AND pg_catalog.convert_to(organization.display_name, 'UTF8')
                = pg_catalog.convert_to(p_organization_display_name, 'UTF8')
            AND organization.state = 'active'
            AND organization.resource_version = 2
            AND organization.created_at = organization.updated_at
            AND organization.created_at = membership.created_at
            AND organization_change.change_kind = 'created'
            AND organization_change.actor_database_principal = SESSION_USER
            AND organization_change.occurred_at = organization.created_at
            AND membership.membership_name = p_membership_name
            AND membership.subject_kind = p_subject_kind
            AND membership.subject_issuer = p_subject_issuer
            AND membership.subject_value = p_subject_value
            AND membership.subject_digest = computed_subject_digest
            AND membership.scope_level = 'tenant'
            AND membership.scope_tenant_uid = p_tenant_uid
            AND membership.scope_organization_uid IS NULL
            AND membership.scope_project_uid IS NULL
            AND membership.state = 'active'
            AND membership.expires_at IS NULL
            AND membership.resource_version = 3
            AND membership.created_at = membership.updated_at
            AND membership.created_at = binding.created_at
            AND membership_change.change_kind = 'created'
            AND membership_change.actor_database_principal = SESSION_USER
            AND membership_change.occurred_at = membership.created_at
            AND membership_audit.audit_fact_uid = p_membership_audit_fact_uid
            AND membership_audit.action = 'membership.create'
            AND membership_audit.actor_database_principal = SESSION_USER
            AND membership_audit.reason_code = p_reason_code
            AND membership_audit.occurred_at = membership.created_at
            AND binding.role_binding_name = p_role_binding_name
            AND binding.subject_kind = p_subject_kind
            AND binding.subject_issuer = p_subject_issuer
            AND binding.subject_value = p_subject_value
            AND binding.subject_digest = computed_subject_digest
            AND binding.role_name = 'tenant.admin'
            AND binding.role_version = 1
            AND binding.scope_level = 'tenant'
            AND binding.scope_tenant_uid = p_tenant_uid
            AND binding.scope_organization_uid IS NULL
            AND binding.scope_project_uid IS NULL
            AND binding.state = 'active'
            AND binding.expires_at IS NULL
            AND binding.resource_version = 4
            AND binding.created_at = binding.updated_at
            AND binding_change.change_kind = 'created'
            AND binding_change.actor_database_principal = SESSION_USER
            AND binding_change.occurred_at = binding.created_at
            AND binding_audit.audit_fact_uid = p_role_binding_audit_fact_uid
            AND binding_audit.action = 'role_binding.bind'
            AND binding_audit.actor_database_principal = SESSION_USER
            AND binding_audit.reason_code = p_reason_code
            AND binding_audit.occurred_at = binding.created_at
    ) INTO full_intent_matches;

    IF full_intent_matches THEN
        RETURN QUERY SELECT
            p_tenant_uid,
            p_organization_uid,
            p_membership_uid,
            p_role_binding_uid,
            4::bigint;
        RETURN;
    END IF;

    SELECT EXISTS (
        SELECT 1
        FROM cloud_agents.platform_tenants AS tenant
        JOIN cloud_agents.tenant_resource_versions AS revision
            ON revision.tenant_id = tenant.tenant_id
            AND revision.tenant_uid = tenant.tenant_uid
        JOIN cloud_agents.resource_changes AS change
            ON change.tenant_id = tenant.tenant_id
            AND change.resource_version = 1
            AND change.resource_kind = 'platform_tenant'
            AND change.resource_uid = tenant.tenant_uid
        JOIN cloud_agents.audit_facts AS audit
            ON audit.tenant_id = tenant.tenant_id
            AND audit.resource_version = 1
            AND audit.resource_kind = 'platform_tenant'
            AND audit.resource_uid = tenant.tenant_uid
        WHERE tenant.tenant_id = p_tenant_uid
            AND tenant.tenant_uid = p_tenant_uid
            AND tenant.tenant_name = p_tenant_name
            AND pg_catalog.convert_to(tenant.display_name, 'UTF8')
                = pg_catalog.convert_to(p_tenant_display_name, 'UTF8')
            AND tenant.state = 'active'
            AND tenant.resource_version = 1
            AND tenant.created_at = tenant.updated_at
            AND revision.current_revision = 1
            AND revision.updated_at = tenant.created_at
            AND change.change_kind = 'created'
            AND change.actor_database_principal = SESSION_USER
            AND change.occurred_at = tenant.created_at
            AND audit.audit_fact_uid = p_tenant_audit_fact_uid
            AND audit.action = 'platform_tenant.bootstrap'
            AND audit.actor_database_principal = SESSION_USER
            AND audit.reason_code = p_reason_code
            AND audit.occurred_at = tenant.created_at
    ) INTO tenant_intent_matches;

    IF NOT tenant_intent_matches THEN
        RAISE EXCEPTION USING
            ERRCODE = '23505',
            MESSAGE = 'cloud_agents tenant administrator bootstrap intent conflicts with existing state',
            CONSTRAINT = 'bootstrap_tenant_administrator_intent';
    END IF;

    bootstrap_at := pg_catalog.clock_timestamp();

    BEGIN
        UPDATE cloud_agents.tenant_resource_versions AS revision
        SET
            current_revision = 4,
            updated_at = bootstrap_at
        WHERE revision.tenant_id = p_tenant_uid
            AND revision.tenant_uid = p_tenant_uid
            AND revision.current_revision = 1
        RETURNING revision.current_revision INTO next_revision;

        IF NOT FOUND THEN
            RAISE EXCEPTION USING
                ERRCODE = '40001',
                MESSAGE = 'tenant resource revision compare-and-swap failed';
        END IF;

        INSERT INTO cloud_agents.resource_changes (
            tenant_id,
            tenant_uid,
            resource_version,
            resource_kind,
            resource_uid,
            change_kind,
            actor_database_principal,
            occurred_at
        ) VALUES (
            p_tenant_uid,
            p_tenant_uid,
            2,
            'organization',
            p_organization_uid,
            'created',
            SESSION_USER,
            bootstrap_at
        );

        INSERT INTO cloud_agents.organizations (
            tenant_id,
            tenant_ref_id,
            organization_uid,
            organization_name,
            display_name,
            state,
            resource_version,
            created_at,
            updated_at
        ) VALUES (
            p_tenant_uid,
            p_tenant_uid,
            p_organization_uid,
            p_organization_name,
            p_organization_display_name,
            'active',
            2,
            bootstrap_at,
            bootstrap_at
        );

        INSERT INTO cloud_agents.resource_changes (
            tenant_id,
            tenant_uid,
            resource_version,
            resource_kind,
            resource_uid,
            change_kind,
            actor_database_principal,
            occurred_at
        ) VALUES (
            p_tenant_uid,
            p_tenant_uid,
            3,
            'membership',
            p_membership_uid,
            'created',
            SESSION_USER,
            bootstrap_at
        );

        INSERT INTO cloud_agents.memberships (
            tenant_id,
            tenant_ref_id,
            membership_uid,
            membership_name,
            subject_kind,
            subject_issuer,
            subject_value,
            subject_digest,
            scope_level,
            scope_tenant_uid,
            state,
            resource_version,
            created_at,
            updated_at
        ) VALUES (
            p_tenant_uid,
            p_tenant_uid,
            p_membership_uid,
            p_membership_name,
            p_subject_kind,
            p_subject_issuer,
            p_subject_value,
            computed_subject_digest,
            'tenant',
            p_tenant_uid,
            'active',
            3,
            bootstrap_at,
            bootstrap_at
        );

        INSERT INTO cloud_agents.audit_facts (
            tenant_id,
            tenant_uid,
            audit_fact_uid,
            resource_version,
            action,
            resource_kind,
            resource_uid,
            actor_database_principal,
            reason_code,
            occurred_at
        ) VALUES (
            p_tenant_uid,
            p_tenant_uid,
            p_membership_audit_fact_uid,
            3,
            'membership.create',
            'membership',
            p_membership_uid,
            SESSION_USER,
            p_reason_code,
            bootstrap_at
        );

        INSERT INTO cloud_agents.resource_changes (
            tenant_id,
            tenant_uid,
            resource_version,
            resource_kind,
            resource_uid,
            change_kind,
            actor_database_principal,
            occurred_at
        ) VALUES (
            p_tenant_uid,
            p_tenant_uid,
            4,
            'role_binding',
            p_role_binding_uid,
            'created',
            SESSION_USER,
            bootstrap_at
        );

        INSERT INTO cloud_agents.role_bindings (
            tenant_id,
            tenant_ref_id,
            role_binding_uid,
            role_binding_name,
            subject_kind,
            subject_issuer,
            subject_value,
            subject_digest,
            role_name,
            role_version,
            scope_level,
            scope_tenant_uid,
            state,
            resource_version,
            created_at,
            updated_at
        ) VALUES (
            p_tenant_uid,
            p_tenant_uid,
            p_role_binding_uid,
            p_role_binding_name,
            p_subject_kind,
            p_subject_issuer,
            p_subject_value,
            computed_subject_digest,
            'tenant.admin',
            1,
            'tenant',
            p_tenant_uid,
            'active',
            4,
            bootstrap_at,
            bootstrap_at
        );

        INSERT INTO cloud_agents.audit_facts (
            tenant_id,
            tenant_uid,
            audit_fact_uid,
            resource_version,
            action,
            resource_kind,
            resource_uid,
            actor_database_principal,
            reason_code,
            occurred_at
        ) VALUES (
            p_tenant_uid,
            p_tenant_uid,
            p_role_binding_audit_fact_uid,
            4,
            'role_binding.bind',
            'role_binding',
            p_role_binding_uid,
            SESSION_USER,
            p_reason_code,
            bootstrap_at
        );
    EXCEPTION
        WHEN unique_violation THEN
            RAISE EXCEPTION USING
                ERRCODE = '23505',
                MESSAGE = 'cloud_agents tenant administrator bootstrap intent conflicts with existing state',
                CONSTRAINT = 'bootstrap_tenant_administrator_intent';
    END;

    RETURN QUERY SELECT
        p_tenant_uid,
        p_organization_uid,
        p_membership_uid,
        p_role_binding_uid,
        next_revision;
END
$cloud_agents_function$;

ALTER FUNCTION cloud_agents.bootstrap_tenant_administrator_v1(
    text, text, text, text, text, text, text, text, text,
    text, text, text, text, text, text, text, text
) OWNER TO cloud_agents_migration_owner;
REVOKE ALL ON FUNCTION cloud_agents.bootstrap_tenant_administrator_v1(
    text, text, text, text, text, text, text, text, text,
    text, text, text, text, text, text, text, text
) FROM PUBLIC;
REVOKE ALL ON FUNCTION cloud_agents.bootstrap_tenant_administrator_v1(
    text, text, text, text, text, text, text, text, text,
    text, text, text, text, text, text, text, text
) FROM cloud_agents_runtime;
GRANT EXECUTE ON FUNCTION cloud_agents.bootstrap_tenant_administrator_v1(
    text, text, text, text, text, text, text, text, text,
    text, text, text, text, text, text, text, text
) TO cloud_agents_bootstrap_admin;
