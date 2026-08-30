ALTER TABLE cloud_agents.audit_facts
    DROP CONSTRAINT audit_facts_action;

ALTER TABLE cloud_agents.audit_facts
    DROP CONSTRAINT audit_facts_resource_kind;

ALTER TABLE cloud_agents.audit_facts
    DROP CONSTRAINT audit_facts_action_resource;

ALTER TABLE cloud_agents.audit_facts
    ADD CONSTRAINT audit_facts_action
    CHECK (
        action IN (
            'platform_tenant.bootstrap',
            'organization.create',
            'membership.create',
            'membership.suspend',
            'membership.revoke',
            'role_binding.bind',
            'role_binding.revoke'
        )
    );

ALTER TABLE cloud_agents.audit_facts
    ADD CONSTRAINT audit_facts_resource_kind
    CHECK (resource_kind IN ('platform_tenant', 'organization', 'membership', 'role_binding'));

ALTER TABLE cloud_agents.audit_facts
    ADD CONSTRAINT audit_facts_action_resource
    CHECK (
        (action = 'platform_tenant.bootstrap' AND resource_kind = 'platform_tenant')
        OR (action = 'organization.create' AND resource_kind = 'organization')
        OR (action LIKE 'membership.%' AND resource_kind = 'membership')
        OR (action LIKE 'role_binding.%' AND resource_kind = 'role_binding')
    );

CREATE FUNCTION cloud_agents.create_organization(
    p_tenant_id text,
    p_expected_tenant_revision bigint,
    p_organization_uid text,
    p_organization_name text,
    p_display_name text,
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
BEGIN
    actor_principal := cloud_agents.require_runtime_mutation_principal();
    mutation_at := pg_catalog.clock_timestamp();

    IF p_tenant_id IS DISTINCT FROM cloud_agents.require_tenant_id()
        OR NOT cloud_agents.is_valid_identifier(p_organization_uid)
        OR NOT cloud_agents.is_valid_identifier(p_organization_name)
        OR pg_catalog.char_length(p_display_name) NOT BETWEEN 1 AND 160
        OR NOT cloud_agents.is_valid_identifier(p_audit_fact_uid)
        OR NOT cloud_agents.is_valid_identifier(p_reason_code)
    THEN
        RAISE EXCEPTION USING
            ERRCODE = '22023',
            MESSAGE = 'organization create input is invalid';
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
        'organization',
        p_organization_uid,
        'created',
        actor_principal,
        mutation_at
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
        p_tenant_id,
        p_tenant_id,
        p_organization_uid,
        p_organization_name,
        p_display_name,
        'active',
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
        'organization.create',
        'organization',
        p_organization_uid,
        actor_principal,
        p_reason_code,
        mutation_at
    );

    RETURN QUERY SELECT p_organization_uid, next_revision, 'active'::text;
END
$cloud_agents_function$;

REVOKE ALL ON FUNCTION cloud_agents.create_organization(
    text, bigint, text, text, text, text, text
) FROM PUBLIC;

GRANT EXECUTE ON FUNCTION cloud_agents.create_organization(
    text, bigint, text, text, text, text, text
) TO cloud_agents_runtime;
