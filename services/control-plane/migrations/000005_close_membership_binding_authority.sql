CREATE OR REPLACE FUNCTION cloud_agents.bind_role(
    p_tenant_id text,
    p_expected_tenant_revision bigint,
    p_role_binding_uid text,
    p_role_binding_name text,
    p_subject_kind text,
    p_subject_issuer text,
    p_subject_value text,
    p_role_name text,
    p_role_version bigint,
    p_scope_level text,
    p_scope_uid text,
    p_expires_at timestamptz,
    p_audit_fact_uid text,
    p_reason_code text
)
RETURNS TABLE (resource_uid text, resource_version bigint, resource_state text)
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, cloud_agents
AS $cloud_agents_function$
DECLARE
    actor_principal text;
    mutation_at timestamptz;
    next_revision bigint;
    computed_subject_digest text;
BEGIN
    actor_principal := cloud_agents.require_runtime_mutation_principal();
    mutation_at := pg_catalog.clock_timestamp();

    IF p_tenant_id IS DISTINCT FROM cloud_agents.require_tenant_id()
        OR NOT cloud_agents.is_valid_identifier(p_role_binding_uid)
        OR NOT cloud_agents.is_valid_identifier(p_role_binding_name)
        OR NOT cloud_agents.is_valid_identifier(p_audit_fact_uid)
        OR NOT cloud_agents.is_valid_identifier(p_reason_code)
        OR p_role_version < 1
        OR p_scope_level NOT IN ('tenant', 'organization', 'project')
        OR NOT cloud_agents.is_valid_identifier(p_scope_uid)
        OR (p_scope_level = 'tenant' AND p_scope_uid IS DISTINCT FROM p_tenant_id)
        OR (p_expires_at IS NOT NULL AND p_expires_at <= mutation_at)
        OR p_role_name = 'platform.admin'
        OR NOT EXISTS (
            SELECT 1
            FROM cloud_agents.builtin_roles AS role
            WHERE role.role_name = p_role_name
                AND role.role_version = p_role_version
                AND role.catalog_revision = 1
                AND role.scope_level = p_scope_level
                AND role.state = 'active'
        )
    THEN
        RAISE EXCEPTION USING
            ERRCODE = '22023',
            MESSAGE = 'role binding input is invalid';
    END IF;

    computed_subject_digest := cloud_agents.subject_ref_digest(
        p_subject_kind,
        p_subject_issuer,
        p_subject_value
    );

    -- Membership is admission authority. A RoleBinding cannot be staged for a
    -- subject without an active, unexpired Membership covering its scope.
    IF NOT EXISTS (
        SELECT 1
        FROM cloud_agents.memberships AS membership
        WHERE membership.tenant_id = p_tenant_id
            AND membership.subject_kind = p_subject_kind
            AND membership.subject_issuer = p_subject_issuer
            AND membership.subject_value = p_subject_value
            AND membership.subject_digest = computed_subject_digest
            AND membership.state = 'active'
            AND (membership.expires_at IS NULL OR mutation_at < membership.expires_at)
            AND (
                membership.scope_level = 'tenant'
                OR (
                    membership.scope_level = 'organization'
                    AND (
                        (
                            p_scope_level = 'organization'
                            AND membership.scope_organization_uid = p_scope_uid
                        )
                        OR (
                            p_scope_level = 'project'
                            AND EXISTS (
                                SELECT 1
                                FROM cloud_agents.projects AS binding_project
                                WHERE binding_project.tenant_id = p_tenant_id
                                    AND binding_project.project_uid = p_scope_uid
                                    AND binding_project.organization_uid =
                                        membership.scope_organization_uid
                            )
                        )
                    )
                )
                OR (
                    membership.scope_level = 'project'
                    AND p_scope_level = 'project'
                    AND membership.scope_project_uid = p_scope_uid
                )
            )
    ) THEN
        RAISE EXCEPTION USING
            ERRCODE = '40001',
            MESSAGE = 'role binding requires an eligible membership';
    END IF;
    next_revision := cloud_agents.allocate_tenant_revision(
        p_tenant_id,
        p_expected_tenant_revision,
        mutation_at
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
        p_tenant_id,
        p_tenant_id,
        next_revision,
        'role_binding',
        p_role_binding_uid,
        'created',
        actor_principal,
        mutation_at
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
        scope_organization_uid,
        scope_project_uid,
        state,
        expires_at,
        resource_version,
        created_at,
        updated_at
    ) VALUES (
        p_tenant_id,
        p_tenant_id,
        p_role_binding_uid,
        p_role_binding_name,
        p_subject_kind,
        p_subject_issuer,
        p_subject_value,
        computed_subject_digest,
        p_role_name,
        p_role_version,
        p_scope_level,
        CASE WHEN p_scope_level = 'tenant' THEN p_scope_uid END,
        CASE WHEN p_scope_level = 'organization' THEN p_scope_uid END,
        CASE WHEN p_scope_level = 'project' THEN p_scope_uid END,
        'active',
        p_expires_at,
        next_revision,
        mutation_at,
        mutation_at
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
        p_tenant_id,
        p_tenant_id,
        p_audit_fact_uid,
        next_revision,
        'role_binding.bind',
        'role_binding',
        p_role_binding_uid,
        actor_principal,
        p_reason_code,
        mutation_at
    );

    RETURN QUERY SELECT p_role_binding_uid, next_revision, 'active'::text;
END
$cloud_agents_function$;
